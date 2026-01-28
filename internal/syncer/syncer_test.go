package syncer

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jamengual/the-redirector/internal/config"
	"github.com/jamengual/the-redirector/internal/providers"
)

// mockSource is a test source that returns a fixed config.
type mockSource struct {
	name   string
	config *config.Config
	err    error
}

func (m *mockSource) Name() string                                             { return m.name }
func (m *mockSource) Fetch(ctx context.Context) (*config.Config, error)        { return m.config, m.err }
func (m *mockSource) Validate(ctx context.Context) error                       { return nil }
func (m *mockSource) Close() error                                             { return nil }
func (m *mockSource) SupportsWatch() bool                                      { return false }
func (m *mockSource) Watch(ctx context.Context) (<-chan *config.Config, error) { return nil, nil }

func init() {
	// Register mock source type
	providers.Registry.Register("mock", func(cfg map[string]interface{}) (providers.Source, error) {
		return &mockSource{name: "mock"}, nil
	})
}

func TestSyncer_MergeWithPrefixes(t *testing.T) {
	// Create syncer with two sources
	s := &Syncer{
		cfg: &Config{
			Merge: MergeConfig{
				ConflictResolution: "error",
			},
		},
		sourceConfigs: []SourceConfig{
			{Name: "marketing", Prefix: "marketing", Priority: 10},
			{Name: "engineering", Prefix: "eng", Priority: 20},
		},
		sources: []providers.Source{
			&mockSource{
				name: "marketing",
				config: &config.Config{
					Rules: []config.Rule{
						{ID: "campaign", Match: config.Match{Path: "/promo/spring"}},
					},
				},
			},
			&mockSource{
				name: "engineering",
				config: &config.Config{
					Rules: []config.Rule{
						{ID: "docs-v2", Match: config.Match{Path: "/docs/v1/"}},
					},
				},
			},
		},
	}

	cfg, err := s.fetchAndMerge(context.Background())
	if err != nil {
		t.Fatalf("fetchAndMerge failed: %v", err)
	}

	if len(cfg.Rules) != 2 {
		t.Errorf("Expected 2 rules, got %d", len(cfg.Rules))
	}

	// Check rule ID prefixing
	ruleIDs := make(map[string]bool)
	for _, r := range cfg.Rules {
		ruleIDs[r.ID] = true
	}

	if !ruleIDs["marketing/campaign"] {
		t.Error("Expected rule ID 'marketing/campaign'")
	}
	if !ruleIDs["eng/docs-v2"] {
		t.Error("Expected rule ID 'eng/docs-v2'")
	}
}

func TestSyncer_ConflictDetection(t *testing.T) {
	// Create syncer with conflicting rules
	s := &Syncer{
		cfg: &Config{
			Merge: MergeConfig{
				ConflictResolution: "error",
			},
		},
		sourceConfigs: []SourceConfig{
			{Name: "source1", Prefix: "s1", Priority: 10},
			{Name: "source2", Prefix: "s2", Priority: 20},
		},
		sources: []providers.Source{
			&mockSource{
				name: "source1",
				config: &config.Config{
					Rules: []config.Rule{
						{ID: "rule1", Match: config.Match{Path: "/same-path"}},
					},
				},
			},
			&mockSource{
				name: "source2",
				config: &config.Config{
					Rules: []config.Rule{
						{ID: "rule2", Match: config.Match{Path: "/same-path"}},
					},
				},
			},
		},
	}

	_, err := s.fetchAndMerge(context.Background())
	if err == nil {
		t.Error("Expected conflict error, got nil")
	}

	// Check merge report has the conflict
	report := s.GetLastReport()
	if report == nil {
		t.Fatal("Expected merge report")
	}
	if len(report.Conflicts) != 1 {
		t.Errorf("Expected 1 conflict, got %d", len(report.Conflicts))
	}
}

func TestSyncer_ConflictResolutionPriority(t *testing.T) {
	// Create syncer with priority-based conflict resolution
	s := &Syncer{
		cfg: &Config{
			Merge: MergeConfig{
				ConflictResolution: "priority",
			},
		},
		sourceConfigs: []SourceConfig{
			{Name: "low-priority", Prefix: "low", Priority: 10},
			{Name: "high-priority", Prefix: "high", Priority: 100},
		},
		sources: []providers.Source{
			&mockSource{
				name: "low-priority",
				config: &config.Config{
					Rules: []config.Rule{
						{ID: "rule", Match: config.Match{Path: "/conflict"}, Redirect: config.Redirect{To: "https://low.example.com"}},
					},
				},
			},
			&mockSource{
				name: "high-priority",
				config: &config.Config{
					Rules: []config.Rule{
						{ID: "rule", Match: config.Match{Path: "/conflict"}, Redirect: config.Redirect{To: "https://high.example.com"}},
					},
				},
			},
		},
	}

	cfg, err := s.fetchAndMerge(context.Background())
	if err != nil {
		t.Fatalf("fetchAndMerge failed: %v", err)
	}

	// Should have 1 rule (high priority wins)
	if len(cfg.Rules) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(cfg.Rules))
	}

	// Check high priority rule won
	if cfg.Rules[0].Redirect.To != "https://high.example.com" {
		t.Errorf("Expected high priority rule, got %s", cfg.Rules[0].Redirect.To)
	}
}

func TestSyncer_AllowedPaths(t *testing.T) {
	s := &Syncer{
		cfg: &Config{
			Merge: MergeConfig{
				ConflictResolution: "error",
			},
		},
		sourceConfigs: []SourceConfig{
			{
				Name:         "restricted",
				Prefix:       "restricted",
				Priority:     10,
				AllowedPaths: []string{"/allowed/"},
			},
		},
		sources: []providers.Source{
			&mockSource{
				name: "restricted",
				config: &config.Config{
					Rules: []config.Rule{
						{ID: "allowed-rule", Match: config.Match{Path: "/allowed/page"}},
						{ID: "forbidden-rule", Match: config.Match{Path: "/forbidden/page"}},
					},
				},
			},
		},
	}

	cfg, err := s.fetchAndMerge(context.Background())
	if err != nil {
		t.Fatalf("fetchAndMerge failed: %v", err)
	}

	// Should have only 1 rule (the allowed one)
	if len(cfg.Rules) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(cfg.Rules))
	}

	// Check it's the allowed rule
	if cfg.Rules[0].ID != "restricted/allowed-rule" {
		t.Errorf("Expected 'restricted/allowed-rule', got %s", cfg.Rules[0].ID)
	}

	// Check warning was generated
	report := s.GetLastReport()
	if len(report.Warnings) != 1 {
		t.Errorf("Expected 1 warning about skipped rule, got %d", len(report.Warnings))
	}
}

func TestSyncer_RequirePrefix(t *testing.T) {
	cfg := &Config{
		Merge: MergeConfig{
			RequirePrefix: true,
		},
		Sources: []SourceConfig{
			{Name: "no-prefix", Type: "mock", Priority: 10},
		},
	}

	_, err := New(cfg)
	if err == nil {
		t.Error("Expected error for missing prefix when require_prefix is true")
	}
}

func TestSyncer_MergeReport(t *testing.T) {
	s := &Syncer{
		cfg: &Config{
			Merge: MergeConfig{
				ConflictResolution: "error",
			},
		},
		sourceConfigs: []SourceConfig{
			{Name: "source1", Prefix: "s1", Priority: 10},
			{Name: "source2", Prefix: "s2", Priority: 20},
		},
		sources: []providers.Source{
			&mockSource{
				name: "source1",
				config: &config.Config{
					Rules: []config.Rule{
						{ID: "rule1", Match: config.Match{Path: "/path1"}},
						{ID: "rule2", Match: config.Match{Path: "/path2"}},
					},
				},
			},
			&mockSource{
				name: "source2",
				config: &config.Config{
					Rules: []config.Rule{
						{ID: "rule3", Match: config.Match{Path: "/path3"}},
					},
				},
			},
		},
	}

	_, err := s.fetchAndMerge(context.Background())
	if err != nil {
		t.Fatalf("fetchAndMerge failed: %v", err)
	}

	report := s.GetLastReport()
	if report == nil {
		t.Fatal("Expected merge report")
	}

	if report.TotalRules != 3 {
		t.Errorf("Expected 3 total rules, got %d", report.TotalRules)
	}

	if len(report.Sources) != 2 {
		t.Errorf("Expected 2 sources in report, got %d", len(report.Sources))
	}

	// Check source contributions
	for _, src := range report.Sources {
		if src.Name == "source1" && src.RuleCount != 2 {
			t.Errorf("Expected source1 to have 2 rules, got %d", src.RuleCount)
		}
		if src.Name == "source2" && src.RuleCount != 1 {
			t.Errorf("Expected source2 to have 1 rule, got %d", src.RuleCount)
		}
	}
}

// computeGitHubSignature generates a valid X-Hub-Signature-256 value.
func computeGitHubSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookHandler_ValidGitHubSignature(t *testing.T) {
	secret := "test-webhook-secret"
	body := []byte(`{"ref":"refs/heads/main"}`)
	sig := computeGitHubSignature(secret, body)

	s := &Syncer{
		cfg: &Config{
			Merge: MergeConfig{ConflictResolution: "error"},
		},
		sourceConfigs: []SourceConfig{
			{Name: "source1", Priority: 10},
		},
		sources: []providers.Source{
			&mockSource{
				name: "source1",
				config: &config.Config{
					Rules: []config.Rule{
						{ID: "rule1", Match: config.Match{Path: "/test"}},
					},
				},
			},
		},
	}

	handler := NewWebhookHandler(s, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", sig)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWebhookHandler_InvalidGitHubSignature(t *testing.T) {
	secret := "test-webhook-secret"
	body := []byte(`{"ref":"refs/heads/main"}`)

	handler := NewWebhookHandler(&Syncer{cfg: &Config{}}, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d", w.Code)
	}
}

func TestWebhookHandler_MissingGitHubSignature(t *testing.T) {
	secret := "test-webhook-secret"
	body := []byte(`{"ref":"refs/heads/main"}`)

	handler := NewWebhookHandler(&Syncer{cfg: &Config{}}, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	// No X-Hub-Signature-256 header
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d", w.Code)
	}
}

func TestWebhookHandler_ValidGitLabToken(t *testing.T) {
	secret := "test-gitlab-token"
	body := []byte(`{"object_kind":"push"}`)

	s := &Syncer{
		cfg: &Config{
			Merge: MergeConfig{ConflictResolution: "error"},
		},
		sourceConfigs: []SourceConfig{
			{Name: "source1", Priority: 10},
		},
		sources: []providers.Source{
			&mockSource{
				name: "source1",
				config: &config.Config{
					Rules: []config.Rule{
						{ID: "rule1", Match: config.Match{Path: "/test"}},
					},
				},
			},
		},
	}

	handler := NewWebhookHandler(s, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	req.Header.Set("X-Gitlab-Token", secret)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWebhookHandler_InvalidGitLabToken(t *testing.T) {
	secret := "test-gitlab-token"
	body := []byte(`{"object_kind":"push"}`)

	handler := NewWebhookHandler(&Syncer{cfg: &Config{}}, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	req.Header.Set("X-Gitlab-Token", "wrong-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d", w.Code)
	}
}

func TestWebhookHandler_NoSecretSkipsValidation(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)

	s := &Syncer{
		cfg: &Config{
			Merge: MergeConfig{ConflictResolution: "error"},
		},
		sourceConfigs: []SourceConfig{
			{Name: "source1", Priority: 10},
		},
		sources: []providers.Source{
			&mockSource{
				name: "source1",
				config: &config.Config{
					Rules: []config.Rule{
						{ID: "rule1", Match: config.Match{Path: "/test"}},
					},
				},
			},
		},
	}

	// No secret configured - validation should be skipped
	handler := NewWebhookHandler(s, "")

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 when no secret configured, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWebhookHandler_MethodNotAllowed(t *testing.T) {
	handler := NewWebhookHandler(&Syncer{cfg: &Config{}}, "secret")

	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", w.Code)
	}
}

func TestValidateGitHubSignature(t *testing.T) {
	secret := "my-secret"
	body := []byte("test payload")

	tests := []struct {
		name      string
		signature string
		wantErr   bool
	}{
		{
			name:      "valid signature",
			signature: computeGitHubSignature(secret, body),
			wantErr:   false,
		},
		{
			name:      "empty signature",
			signature: "",
			wantErr:   true,
		},
		{
			name:      "missing prefix",
			signature: "abc123",
			wantErr:   true,
		},
		{
			name:      "invalid hex",
			signature: "sha256=zzzz",
			wantErr:   true,
		},
		{
			name:      "wrong signature",
			signature: "sha256=" + hex.EncodeToString([]byte("wrong")),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGitHubSignature(secret, tt.signature, body)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateGitLabToken(t *testing.T) {
	secret := "my-gitlab-token"

	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{
			name:    "valid token",
			token:   secret,
			wantErr: false,
		},
		{
			name:    "empty token",
			token:   "",
			wantErr: true,
		},
		{
			name:    "wrong token",
			token:   "wrong-token",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGitLabToken(secret, tt.token)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestSyncer_NilConfigFromFetch(t *testing.T) {
	s := &Syncer{
		cfg: &Config{
			Merge: MergeConfig{
				ConflictResolution: "error",
			},
		},
		sourceConfigs: []SourceConfig{
			{Name: "nil-source", Priority: 10},
			{Name: "good-source", Prefix: "good", Priority: 20},
		},
		sources: []providers.Source{
			&mockSource{
				name:   "nil-source",
				config: nil, // Returns nil config with nil error
				err:    nil,
			},
			&mockSource{
				name: "good-source",
				config: &config.Config{
					Rules: []config.Rule{
						{ID: "rule1", Match: config.Match{Path: "/test"}},
					},
				},
			},
		},
	}

	cfg, err := s.fetchAndMerge(context.Background())
	if err != nil {
		t.Fatalf("fetchAndMerge should not fail, got: %v", err)
	}

	// Should have only the good source's rule
	if len(cfg.Rules) != 1 {
		t.Errorf("Expected 1 rule from good source, got %d", len(cfg.Rules))
	}

	// Check report recorded the nil-source error
	report := s.GetLastReport()
	if report == nil {
		t.Fatal("Expected merge report")
	}

	foundNilError := false
	for _, src := range report.Sources {
		if src.Name == "nil-source" && src.Error != "" {
			foundNilError = true
		}
	}
	if !foundNilError {
		t.Error("Expected nil-source to have an error in the merge report")
	}
}
