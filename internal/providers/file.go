package providers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jamengual/the-redirector/internal/config"
)

func init() {
	// Register the file source at package initialization
	Registry.Register("file", NewFileSource)
}

// FileSource reads configuration from local files.
// Supports YAML, JSON, and TOML formats (detected by extension).
type FileSource struct {
	path    string
	options SourceOptions

	mu       sync.Mutex
	lastMod  time.Time
	watcher  chan struct{}
	stopOnce sync.Once
}

// FileSourceConfig configures the file source.
type FileSourceConfig struct {
	// Path is the path to the configuration file.
	Path string `yaml:"path" json:"path"`

	// Watch enables file system watching for changes.
	Watch bool `yaml:"watch" json:"watch"`

	// Options contains common source options.
	Options SourceOptions `yaml:"options" json:"options"`
}

// NewFileSource creates a new file-based configuration source.
func NewFileSource(cfg map[string]interface{}) (Source, error) {
	path, ok := cfg["path"].(string)
	if !ok || path == "" {
		return nil, fmt.Errorf("file source requires 'path' configuration")
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}

	options := DefaultSourceOptions()
	if pollInterval, ok := cfg["poll_interval"].(string); ok {
		if d, err := time.ParseDuration(pollInterval); err == nil {
			options.PollInterval = d
		}
	}

	return &FileSource{
		path:    absPath,
		options: options,
	}, nil
}

// Name returns the source type identifier.
func (f *FileSource) Name() string {
	return "file"
}

// Fetch reads the configuration from the file.
func (f *FileSource) Fetch(ctx context.Context) (*config.Config, error) {
	cfg, err := config.Load(f.path)
	if err != nil {
		return nil, fmt.Errorf("loading config from %s: %w", f.path, err)
	}

	// Update last modified time
	if info, err := os.Stat(f.path); err == nil {
		f.mu.Lock()
		f.lastMod = info.ModTime()
		f.mu.Unlock()
	}

	return cfg, nil
}

// Watch returns a channel that emits configs when the file changes.
// Note: For production use, consider using fsnotify for efficient watching.
func (f *FileSource) Watch(ctx context.Context) (<-chan *config.Config, error) {
	updates := make(chan *config.Config, 1)
	f.watcher = make(chan struct{})

	go func() {
		ticker := time.NewTicker(f.options.PollInterval)
		defer ticker.Stop()
		defer close(updates)

		for {
			select {
			case <-ctx.Done():
				return
			case <-f.watcher:
				return
			case <-ticker.C:
				info, err := os.Stat(f.path)
				if err != nil {
					continue
				}

				f.mu.Lock()
				changed := info.ModTime().After(f.lastMod)
				f.mu.Unlock()

				if changed {
					cfg, err := f.Fetch(ctx)
					if err == nil {
						select {
						case updates <- cfg:
						default:
							// Channel full, skip this update
						}
					}
				}
			}
		}
	}()

	return updates, nil
}

// SupportsWatch returns true - file source supports polling-based watching.
func (f *FileSource) SupportsWatch() bool {
	return true
}

// Validate checks if the file exists and is readable.
func (f *FileSource) Validate(ctx context.Context) error {
	info, err := os.Stat(f.path)
	if err != nil {
		return fmt.Errorf("cannot access file %s: %w", f.path, err)
	}

	if info.IsDir() {
		return fmt.Errorf("path %s is a directory, expected file", f.path)
	}

	// Try to read the file
	file, err := os.Open(f.path)
	if err != nil {
		return fmt.Errorf("cannot open file %s: %w", f.path, err)
	}
	file.Close()

	return nil
}

// Close stops the file watcher.
func (f *FileSource) Close() error {
	f.stopOnce.Do(func() {
		if f.watcher != nil {
			close(f.watcher)
		}
	})
	return nil
}
