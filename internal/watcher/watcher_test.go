package watcher

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	reloadCalled := false
	w, err := New(Config{
		Paths:    []string{"."},
		Debounce: 100 * time.Millisecond,
		ReloadFunc: func(path string) error {
			reloadCalled = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer w.Stop()

	if w == nil {
		t.Fatal("New returned nil")
	}
	if reloadCalled {
		t.Error("ReloadFunc should not be called during New")
	}
}

func TestWatcher_StartStop(t *testing.T) {
	w, err := New(Config{
		Paths:      []string{"."},
		Debounce:   100 * time.Millisecond,
		ReloadFunc: func(path string) error { return nil },
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	cancel()
	time.Sleep(50 * time.Millisecond) // Allow goroutine to stop

	if err := w.Stop(); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

func TestWatcher_FileChange(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "watcher-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create initial config file
	configFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configFile, []byte("version: 1"), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	var reloadCount int32
	var lastPath string

	w, err := New(Config{
		Paths:    []string{tmpDir},
		Debounce: 50 * time.Millisecond,
		ReloadFunc: func(path string) error {
			atomic.AddInt32(&reloadCount, 1)
			lastPath = path
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer w.Stop()

	// Give watcher time to start
	time.Sleep(100 * time.Millisecond)

	// Modify the config file
	if err := os.WriteFile(configFile, []byte("version: 2"), 0644); err != nil {
		t.Fatalf("Failed to modify config: %v", err)
	}

	// Wait for debounce and reload
	time.Sleep(200 * time.Millisecond)

	if atomic.LoadInt32(&reloadCount) == 0 {
		t.Error("ReloadFunc was not called after file change")
	}
	if lastPath != tmpDir {
		t.Errorf("Expected path %s, got %s", tmpDir, lastPath)
	}
}

func TestWatcher_Debounce(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "watcher-debounce-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configFile, []byte("version: 1"), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	var reloadCount int32

	w, err := New(Config{
		Paths:    []string{tmpDir},
		Debounce: 200 * time.Millisecond,
		ReloadFunc: func(path string) error {
			atomic.AddInt32(&reloadCount, 1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer w.Stop()

	time.Sleep(100 * time.Millisecond)

	// Make multiple rapid changes
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(configFile, []byte("version: "+string(rune('2'+i))), 0644); err != nil {
			t.Fatalf("Failed to modify config: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Wait for debounce
	time.Sleep(400 * time.Millisecond)

	// Should only reload once due to debouncing
	count := atomic.LoadInt32(&reloadCount)
	if count > 2 {
		t.Errorf("Expected at most 2 reloads due to debouncing, got %d", count)
	}
}

func TestWatcher_IgnoreNonYAML(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "watcher-ignore-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create YAML file for initial watch
	yamlFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(yamlFile, []byte("version: 1"), 0644); err != nil {
		t.Fatalf("Failed to write yaml: %v", err)
	}

	var reloadCount int32

	w, err := New(Config{
		Paths:    []string{tmpDir},
		Debounce: 50 * time.Millisecond,
		ReloadFunc: func(path string) error {
			atomic.AddInt32(&reloadCount, 1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer w.Stop()

	time.Sleep(100 * time.Millisecond)

	// Create non-YAML files
	txtFile := filepath.Join(tmpDir, "notes.txt")
	jsonFile := filepath.Join(tmpDir, "data.json")

	if err := os.WriteFile(txtFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("Failed to write txt: %v", err)
	}
	if err := os.WriteFile(jsonFile, []byte("{}"), 0644); err != nil {
		t.Fatalf("Failed to write json: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Should not trigger reload for non-YAML files
	if atomic.LoadInt32(&reloadCount) > 0 {
		t.Error("ReloadFunc should not be called for non-YAML files")
	}
}

func TestWatcher_AddRemovePath(t *testing.T) {
	w, err := New(Config{
		Paths:      []string{"."},
		Debounce:   100 * time.Millisecond,
		ReloadFunc: func(path string) error { return nil },
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer w.Stop()

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "watcher-addremove-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Add path
	if err := w.AddPath(tmpDir); err != nil {
		t.Errorf("AddPath failed: %v", err)
	}

	if len(w.paths) != 2 {
		t.Errorf("Expected 2 paths, got %d", len(w.paths))
	}

	// Remove path
	if err := w.RemovePath(tmpDir); err != nil {
		t.Errorf("RemovePath failed: %v", err)
	}

	if len(w.paths) != 1 {
		t.Errorf("Expected 1 path after remove, got %d", len(w.paths))
	}
}
