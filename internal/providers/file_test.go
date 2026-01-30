package providers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewFileSource(t *testing.T) {
	tests := []struct {
		name    string
		cfg     map[string]interface{}
		wantErr bool
		check   func(*testing.T, Source)
	}{
		{
			name: "valid path",
			cfg: map[string]interface{}{
				"path": "/tmp/test-config.yaml",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				fs, ok := s.(*FileSource)
				if !ok {
					t.Fatal("expected *FileSource")
				}
				if !filepath.IsAbs(fs.path) {
					t.Errorf("path should be absolute, got %q", fs.path)
				}
			},
		},
		{
			name: "with poll interval",
			cfg: map[string]interface{}{
				"path":          "/tmp/test-config.yaml",
				"poll_interval": "10s",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				fs, _ := s.(*FileSource)
				if fs.options.PollInterval != 10*time.Second {
					t.Errorf("PollInterval = %v, want 10s", fs.options.PollInterval)
				}
			},
		},
		{
			name:    "missing path",
			cfg:     map[string]interface{}{},
			wantErr: true,
		},
		{
			name: "empty path",
			cfg: map[string]interface{}{
				"path": "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := NewFileSource(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewFileSource() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil && source != nil {
				tt.check(t, source)
			}
		})
	}
}

func TestFileSource_Name(t *testing.T) {
	source, _ := NewFileSource(map[string]interface{}{
		"path": "/tmp/config.yaml",
	})
	if source.Name() != "file" {
		t.Errorf("Name() = %q, want %q", source.Name(), "file")
	}
}

func TestFileSource_SupportsWatch(t *testing.T) {
	source, _ := NewFileSource(map[string]interface{}{
		"path": "/tmp/config.yaml",
	})
	if !source.SupportsWatch() {
		t.Error("SupportsWatch() = false, want true")
	}
}

func TestFileSource_Registry(t *testing.T) {
	factory, ok := Registry.Get("file")
	if !ok {
		t.Fatal("file source not registered")
	}

	source, err := factory(map[string]interface{}{
		"path": "/tmp/config.yaml",
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	if source.Name() != "file" {
		t.Errorf("Name() = %q, want %q", source.Name(), "file")
	}
}

func TestFileSource_Validate(t *testing.T) {
	// Create a valid temp file
	tmpFile, err := os.CreateTemp("", "test-config-*.yaml")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	tmpFile.Write([]byte(validTestConfig))
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "test-dir-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name        string
		path        string
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid file",
			path:    tmpFile.Name(),
			wantErr: false,
		},
		{
			name:        "non-existent file",
			path:        "/tmp/nonexistent-config-12345.yaml",
			wantErr:     true,
			errContains: "cannot access",
		},
		{
			name:        "directory instead of file",
			path:        tmpDir,
			wantErr:     true,
			errContains: "is a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &FileSource{path: tt.path}
			err := source.Validate(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errContains != "" && err != nil {
				if !contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
			}
		})
	}
}

func TestFileSource_Fetch(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-config-*.yaml")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	tmpFile.Write([]byte(validTestConfig))
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	source := &FileSource{
		path:    tmpFile.Name(),
		options: DefaultSourceOptions(),
	}

	cfg, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("Fetch() returned nil config")
	}
}

func TestFileSource_Fetch_NonExistent(t *testing.T) {
	source := &FileSource{
		path:    "/tmp/nonexistent-config-12345.yaml",
		options: DefaultSourceOptions(),
	}

	_, err := source.Fetch(context.Background())
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestFileSource_Close(t *testing.T) {
	source := &FileSource{}
	if err := source.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Double close should not panic
	if err := source.Close(); err != nil {
		t.Errorf("Close() second call error = %v", err)
	}
}

func TestFileSource_Close_WithWatcher(t *testing.T) {
	source := &FileSource{
		watcher: make(chan struct{}),
	}
	if err := source.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestFileSource_DefaultPollInterval(t *testing.T) {
	source, err := NewFileSource(map[string]interface{}{
		"path": "/tmp/config.yaml",
	})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}
	fs, _ := source.(*FileSource)
	defaults := DefaultSourceOptions()
	if fs.options.PollInterval != defaults.PollInterval {
		t.Errorf("PollInterval = %v, want default %v", fs.options.PollInterval, defaults.PollInterval)
	}
}
