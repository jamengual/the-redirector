// Package watcher provides file system watching for configuration hot-reload.
package watcher

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

// ReloadFunc is called when configuration should be reloaded.
type ReloadFunc func(path string) error

// Watcher watches configuration files for changes.
type Watcher struct {
	watcher     *fsnotify.Watcher
	paths       []string
	reloadFunc  ReloadFunc
	debounce    time.Duration
	mu          sync.Mutex
	lastReload  time.Time
	pendingPath string
	timer       *time.Timer
}

// Config configures the file watcher.
type Config struct {
	// Paths to watch (files or directories)
	Paths []string

	// Debounce duration to prevent rapid reloads
	// Default: 500ms
	Debounce time.Duration

	// ReloadFunc is called when a reload is triggered
	ReloadFunc ReloadFunc
}

// New creates a new file watcher.
func New(cfg Config) (*Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	debounce := cfg.Debounce
	if debounce == 0 {
		debounce = 500 * time.Millisecond
	}

	w := &Watcher{
		watcher:    fsWatcher,
		paths:      cfg.Paths,
		reloadFunc: cfg.ReloadFunc,
		debounce:   debounce,
	}

	return w, nil
}

// Start begins watching for file changes.
func (w *Watcher) Start(ctx context.Context) error {
	// Add paths to watch
	for _, path := range w.paths {
		absPath, err := filepath.Abs(path)
		if err != nil {
			log.Warn().Err(err).Str("path", path).Msg("Failed to resolve path")
			continue
		}

		if err := w.watcher.Add(absPath); err != nil {
			log.Warn().Err(err).Str("path", absPath).Msg("Failed to watch path")
			continue
		}

		log.Info().Str("path", absPath).Msg("Watching for changes")
	}

	// Process events
	go w.processEvents(ctx)

	return nil
}

// processEvents handles file system events.
func (w *Watcher) processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Debug().Msg("Watcher context canceled")
			return

		case event, ok := <-w.watcher.Events:
			if !ok {
				log.Debug().Msg("Watcher events channel closed")
				return
			}

			// Only care about write and create events
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}

			// Skip non-YAML files
			ext := filepath.Ext(event.Name)
			if ext != ".yaml" && ext != ".yml" {
				continue
			}

			log.Debug().
				Str("path", event.Name).
				Str("op", event.Op.String()).
				Msg("File change detected")

			w.scheduleReload(event.Name)

		case err, ok := <-w.watcher.Errors:
			if !ok {
				log.Debug().Msg("Watcher errors channel closed")
				return
			}
			log.Error().Err(err).Msg("Watcher error")
		}
	}
}

// scheduleReload schedules a reload with debouncing.
func (w *Watcher) scheduleReload(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Store the path that triggered the change
	w.pendingPath = path

	// Cancel any existing timer
	if w.timer != nil {
		w.timer.Stop()
	}

	// Schedule reload after debounce period
	w.timer = time.AfterFunc(w.debounce, func() {
		w.mu.Lock()
		path := w.pendingPath
		w.mu.Unlock()

		w.doReload(path)
	})
}

// doReload performs the actual reload.
func (w *Watcher) doReload(path string) {
	w.mu.Lock()
	// Check if enough time has passed since last reload
	if time.Since(w.lastReload) < w.debounce {
		w.mu.Unlock()
		return
	}
	w.lastReload = time.Now()
	w.mu.Unlock()

	log.Info().Str("path", path).Msg("Reloading configuration")

	// Find the root config path (might be a directory)
	configPath := w.findConfigRoot(path)

	if err := w.reloadFunc(configPath); err != nil {
		log.Error().Err(err).Str("path", configPath).Msg("Failed to reload configuration")
		return
	}

	log.Info().Str("path", configPath).Msg("Configuration reloaded successfully")
}

// findConfigRoot finds the root config path from a changed file.
func (w *Watcher) findConfigRoot(changedPath string) string {
	// If we're watching a specific file, return its directory's first watched path
	dir := filepath.Dir(changedPath)

	for _, watchedPath := range w.paths {
		absWatched, _ := filepath.Abs(watchedPath)
		absDir, _ := filepath.Abs(dir)

		// If the changed file is in a watched directory, use that directory
		if absDir == absWatched || filepath.Dir(absWatched) == absDir {
			return watchedPath
		}

		// If the changed file matches a watched file exactly
		absChanged, _ := filepath.Abs(changedPath)
		if absChanged == absWatched {
			return watchedPath
		}
	}

	// Default to the first watched path
	if len(w.paths) > 0 {
		return w.paths[0]
	}

	return changedPath
}

// Stop stops the watcher.
func (w *Watcher) Stop() error {
	w.mu.Lock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.mu.Unlock()

	return w.watcher.Close()
}

// AddPath adds a path to watch.
func (w *Watcher) AddPath(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	if err := w.watcher.Add(absPath); err != nil {
		return err
	}

	w.mu.Lock()
	w.paths = append(w.paths, path)
	w.mu.Unlock()

	log.Info().Str("path", absPath).Msg("Added path to watch")
	return nil
}

// RemovePath removes a path from watching.
func (w *Watcher) RemovePath(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	if err := w.watcher.Remove(absPath); err != nil {
		return err
	}

	w.mu.Lock()
	for i, p := range w.paths {
		if p == path {
			w.paths = append(w.paths[:i], w.paths[i+1:]...)
			break
		}
	}
	w.mu.Unlock()

	log.Info().Str("path", absPath).Msg("Removed path from watch")
	return nil
}
