// Package versioning provides configuration version tracking, audit logging, and rollback.
package versioning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/jamengual/the-redirector/internal/config"
)

// ConfigVersion represents a specific version of the configuration.
type ConfigVersion struct {
	Version    int             `json:"version"`      // Monotonically increasing version number
	Hash       string          `json:"hash"`         // SHA256 hash of config content
	LoadedAt   time.Time       `json:"loaded_at"`    // When this version was loaded
	Source     string          `json:"source"`       // Source of config (file path, s3, github)
	RulesCount int             `json:"rules_count"`  // Number of rules in this version
	Changes    *ConfigChanges  `json:"changes"`      // Changes from previous version
	Config     *config.Config  `json:"-"`            // The actual config (not serialized)
}

// ConfigChanges describes what changed between versions.
type ConfigChanges struct {
	RulesAdded    []string `json:"rules_added,omitempty"`
	RulesRemoved  []string `json:"rules_removed,omitempty"`
	RulesModified []string `json:"rules_modified,omitempty"`
	ServerChanged bool     `json:"server_changed,omitempty"`
	StatsChanged  bool     `json:"stats_changed,omitempty"`
	AuthChanged   bool     `json:"auth_changed,omitempty"`
}

// Store manages configuration versions with bounded history.
type Store struct {
	versions    []*ConfigVersion
	maxVersions int
	current     int // Index of current version in ring buffer
	count       int // Total versions stored
	nextVersion int // Next version number to assign
	mu          sync.RWMutex
}

// NewStore creates a new version store with the specified max history.
func NewStore(maxVersions int) *Store {
	if maxVersions < 1 {
		maxVersions = 10
	}
	return &Store{
		versions:    make([]*ConfigVersion, maxVersions),
		maxVersions: maxVersions,
		nextVersion: 1,
	}
}

// Add stores a new configuration version.
// Returns the version number assigned.
func (s *Store) Add(cfg *config.Config, source string) *ConfigVersion {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Calculate hash of config
	hash := s.calculateHash(cfg)

	// Get previous version for diff
	var changes *ConfigChanges
	if prev := s.getCurrentLocked(); prev != nil {
		changes = s.calculateChanges(prev.Config, cfg)
	}

	version := &ConfigVersion{
		Version:    s.nextVersion,
		Hash:       hash,
		LoadedAt:   time.Now(),
		Source:     source,
		RulesCount: len(cfg.Rules),
		Changes:    changes,
		Config:     cfg,
	}

	// Store in ring buffer
	s.current = (s.current + 1) % s.maxVersions
	s.versions[s.current] = version

	if s.count < s.maxVersions {
		s.count++
	}
	s.nextVersion++

	return version
}

// Current returns the current configuration version.
func (s *Store) Current() *ConfigVersion {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getCurrentLocked()
}

func (s *Store) getCurrentLocked() *ConfigVersion {
	if s.count == 0 {
		return nil
	}
	return s.versions[s.current]
}

// Get returns a specific version by version number.
func (s *Store) Get(version int) *ConfigVersion {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i := 0; i < s.count; i++ {
		idx := (s.current - i + s.maxVersions) % s.maxVersions
		if s.versions[idx] != nil && s.versions[idx].Version == version {
			return s.versions[idx]
		}
	}
	return nil
}

// List returns all stored versions, newest first.
func (s *Store) List() []*ConfigVersion {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*ConfigVersion, 0, s.count)
	for i := 0; i < s.count; i++ {
		idx := (s.current - i + s.maxVersions) % s.maxVersions
		if s.versions[idx] != nil {
			result = append(result, s.versions[idx])
		}
	}
	return result
}

// Previous returns the previous version (for rollback).
func (s *Store) Previous() *ConfigVersion {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.count < 2 {
		return nil
	}

	prevIdx := (s.current - 1 + s.maxVersions) % s.maxVersions
	return s.versions[prevIdx]
}

// Rollback sets a previous version as current without changing it.
// Returns the version rolled back to, or nil if version not found.
func (s *Store) Rollback(version int) *ConfigVersion {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find the version
	for i := 0; i < s.count; i++ {
		idx := (s.current - i + s.maxVersions) % s.maxVersions
		if s.versions[idx] != nil && s.versions[idx].Version == version {
			// Create a new version entry for the rollback
			rolledBack := &ConfigVersion{
				Version:    s.nextVersion,
				Hash:       s.versions[idx].Hash,
				LoadedAt:   time.Now(),
				Source:     "rollback:" + s.versions[idx].Source,
				RulesCount: s.versions[idx].RulesCount,
				Changes:    s.calculateChanges(s.versions[s.current].Config, s.versions[idx].Config),
				Config:     s.versions[idx].Config,
			}

			// Store as new current
			s.current = (s.current + 1) % s.maxVersions
			s.versions[s.current] = rolledBack

			if s.count < s.maxVersions {
				s.count++
			}
			s.nextVersion++

			return rolledBack
		}
	}

	return nil
}

// calculateHash computes a SHA256 hash of the config.
func (s *Store) calculateHash(cfg *config.Config) string {
	// Create a deterministic representation
	data, err := json.Marshal(struct {
		Version string
		Rules   int
		Server  config.ServerConfig
	}{
		Version: cfg.Version,
		Rules:   len(cfg.Rules),
		Server:  cfg.Server,
	})
	if err != nil {
		return ""
	}

	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:8]) // Use first 8 bytes for readability
}

// calculateChanges computes the diff between two configs.
func (s *Store) calculateChanges(old, new *config.Config) *ConfigChanges {
	if old == nil || new == nil {
		return nil
	}

	changes := &ConfigChanges{}

	// Build maps of rule IDs
	oldRules := make(map[string]*config.Rule)
	for i := range old.Rules {
		oldRules[old.Rules[i].ID] = &old.Rules[i]
	}

	newRules := make(map[string]*config.Rule)
	for i := range new.Rules {
		newRules[new.Rules[i].ID] = &new.Rules[i]
	}

	// Find added and modified rules
	for id, newRule := range newRules {
		if oldRule, exists := oldRules[id]; !exists {
			changes.RulesAdded = append(changes.RulesAdded, id)
		} else if !rulesEqual(oldRule, newRule) {
			changes.RulesModified = append(changes.RulesModified, id)
		}
	}

	// Find removed rules
	for id := range oldRules {
		if _, exists := newRules[id]; !exists {
			changes.RulesRemoved = append(changes.RulesRemoved, id)
		}
	}

	// Check server config changes
	changes.ServerChanged = old.Server != new.Server

	// Check stats config changes
	changes.StatsChanged = !statsEqual(old.Stats, new.Stats)

	// Check auth config changes
	changes.AuthChanged = !authEqual(old.Auth, new.Auth)

	return changes
}

// rulesEqual compares two rules for equality.
func rulesEqual(a, b *config.Rule) bool {
	if a.ID != b.ID || a.Priority != b.Priority {
		return false
	}
	if a.Match.Type != b.Match.Type || a.Match.Path != b.Match.Path ||
		a.Match.Pattern != b.Match.Pattern || a.Match.Host != b.Match.Host {
		return false
	}
	if a.Redirect.Status != b.Redirect.Status ||
		a.Redirect.GetLocation() != b.Redirect.GetLocation() ||
		a.Redirect.Body != b.Redirect.Body {
		return false
	}
	return true
}

// statsEqual compares stats configs.
func statsEqual(a, b *config.StatsConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Enabled == b.Enabled &&
		a.BufferSize == b.BufferSize &&
		a.SamplingRate == b.SamplingRate
}

// authEqual compares auth configs.
func authEqual(a, b *config.AuthConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Enabled == b.Enabled
	// Note: Full comparison would check API keys and JWT config
}
