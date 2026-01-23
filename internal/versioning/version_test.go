package versioning

import (
	"testing"

	"github.com/jamengual/the-redirector/internal/config"
)

func TestStore(t *testing.T) {
	store := NewStore(5)

	// Add first config
	cfg1 := &config.Config{
		Version: "1.0",
		Server:  config.ServerConfig{Port: 8080},
		Rules: []config.Rule{
			{ID: "rule-1", Match: config.Match{Type: "exact", Path: "/foo"}},
		},
	}
	v1 := store.Add(cfg1, "file:config.yaml")

	if v1.Version != 1 {
		t.Errorf("Expected version 1, got %d", v1.Version)
	}
	if v1.RulesCount != 1 {
		t.Errorf("Expected 1 rule, got %d", v1.RulesCount)
	}
	if v1.Changes != nil {
		t.Error("First version should have no changes")
	}

	// Add second config
	cfg2 := &config.Config{
		Version: "1.0",
		Server:  config.ServerConfig{Port: 8080},
		Rules: []config.Rule{
			{ID: "rule-1", Match: config.Match{Type: "exact", Path: "/foo"}},
			{ID: "rule-2", Match: config.Match{Type: "exact", Path: "/bar"}},
		},
	}
	v2 := store.Add(cfg2, "file:config.yaml")

	if v2.Version != 2 {
		t.Errorf("Expected version 2, got %d", v2.Version)
	}
	if v2.Changes == nil {
		t.Fatal("Second version should have changes")
	}
	if len(v2.Changes.RulesAdded) != 1 || v2.Changes.RulesAdded[0] != "rule-2" {
		t.Errorf("Expected rule-2 added, got %v", v2.Changes.RulesAdded)
	}

	// Test Current()
	current := store.Current()
	if current.Version != 2 {
		t.Errorf("Current should be version 2, got %d", current.Version)
	}

	// Test Get()
	v1Retrieved := store.Get(1)
	if v1Retrieved == nil || v1Retrieved.Version != 1 {
		t.Error("Get(1) should return version 1")
	}

	// Test List()
	list := store.List()
	if len(list) != 2 {
		t.Errorf("Expected 2 versions, got %d", len(list))
	}
	if list[0].Version != 2 || list[1].Version != 1 {
		t.Error("List should return newest first")
	}

	// Test Previous()
	prev := store.Previous()
	if prev == nil || prev.Version != 1 {
		t.Error("Previous should return version 1")
	}
}

func TestStoreRollback(t *testing.T) {
	store := NewStore(5)

	// Add configs
	cfg1 := &config.Config{
		Version: "1.0",
		Rules:   []config.Rule{{ID: "rule-1"}},
	}
	store.Add(cfg1, "file:config.yaml")

	cfg2 := &config.Config{
		Version: "1.0",
		Rules:   []config.Rule{{ID: "rule-1"}, {ID: "rule-2"}},
	}
	store.Add(cfg2, "file:config.yaml")

	cfg3 := &config.Config{
		Version: "1.0",
		Rules:   []config.Rule{{ID: "rule-3"}},
	}
	store.Add(cfg3, "file:config.yaml")

	// Rollback to version 1
	rolled := store.Rollback(1)
	if rolled == nil {
		t.Fatal("Rollback should succeed")
	}
	if rolled.Version != 4 { // New version number
		t.Errorf("Rolled back version should be 4, got %d", rolled.Version)
	}
	if rolled.RulesCount != 1 {
		t.Errorf("Rolled back should have 1 rule, got %d", rolled.RulesCount)
	}
	if rolled.Source != "rollback:file:config.yaml" {
		t.Errorf("Source should indicate rollback, got %s", rolled.Source)
	}
}

func TestStoreRingBuffer(t *testing.T) {
	store := NewStore(3)

	// Add more than max versions
	for i := 1; i <= 5; i++ {
		cfg := &config.Config{
			Version: "1.0",
			Rules:   []config.Rule{{ID: "rule-" + string(rune('0'+i))}},
		}
		store.Add(cfg, "test")
	}

	// Should only have last 3 versions
	list := store.List()
	if len(list) != 3 {
		t.Errorf("Expected 3 versions, got %d", len(list))
	}

	// Oldest should be gone
	v1 := store.Get(1)
	if v1 != nil {
		t.Error("Version 1 should be evicted")
	}

	// Newest should exist
	v5 := store.Get(5)
	if v5 == nil {
		t.Error("Version 5 should exist")
	}
}

func TestAuditLog(t *testing.T) {
	log := NewAuditLog(10)

	// Log some events
	log.Log(AuditEventConfigLoaded, "system", "127.0.0.1", nil)
	log.Log(AuditEventAuthSuccess, "admin", "10.0.0.1", map[string]string{"method": "apikey"})
	log.Log(AuditEventConfigReloaded, "deploy-key", "10.0.0.2", nil)

	// Test Recent()
	recent := log.Recent(2)
	if len(recent) != 2 {
		t.Errorf("Expected 2 events, got %d", len(recent))
	}
	if recent[0].Type != AuditEventConfigReloaded {
		t.Error("Most recent should be config_reloaded")
	}

	// Test Query()
	authEvents := log.Query(AuditFilter{Type: AuditEventAuthSuccess})
	if len(authEvents) != 1 {
		t.Errorf("Expected 1 auth event, got %d", len(authEvents))
	}

	// Test Count()
	if log.Count() != 3 {
		t.Errorf("Expected 3 events, got %d", log.Count())
	}

	// Test handler
	handlerCalled := false
	log.AddHandler(func(e *AuditEvent) {
		handlerCalled = true
	})
	log.Log(AuditEventAPICall, "test", "test", nil)
	if !handlerCalled {
		t.Error("Handler should be called")
	}
}

func TestConfigChanges(t *testing.T) {
	store := NewStore(5)

	cfg1 := &config.Config{
		Server: config.ServerConfig{Port: 8080},
		Rules: []config.Rule{
			{ID: "rule-1", Match: config.Match{Path: "/a"}},
			{ID: "rule-2", Match: config.Match{Path: "/b"}},
		},
	}
	store.Add(cfg1, "test")

	cfg2 := &config.Config{
		Server: config.ServerConfig{Port: 8081}, // Changed
		Rules: []config.Rule{
			{ID: "rule-1", Match: config.Match{Path: "/a-modified"}}, // Modified
			{ID: "rule-3", Match: config.Match{Path: "/c"}},          // Added
			// rule-2 removed
		},
	}
	v2 := store.Add(cfg2, "test")

	if v2.Changes == nil {
		t.Fatal("Changes should not be nil")
	}
	if !v2.Changes.ServerChanged {
		t.Error("Server should be marked as changed")
	}
	if len(v2.Changes.RulesAdded) != 1 || v2.Changes.RulesAdded[0] != "rule-3" {
		t.Errorf("Expected rule-3 added, got %v", v2.Changes.RulesAdded)
	}
	if len(v2.Changes.RulesRemoved) != 1 || v2.Changes.RulesRemoved[0] != "rule-2" {
		t.Errorf("Expected rule-2 removed, got %v", v2.Changes.RulesRemoved)
	}
	if len(v2.Changes.RulesModified) != 1 || v2.Changes.RulesModified[0] != "rule-1" {
		t.Errorf("Expected rule-1 modified, got %v", v2.Changes.RulesModified)
	}
}
