package config

import (
	"os"
	"testing"
)

func TestLoad_ValidConfig(t *testing.T) {
	content := `
version: "1.0"
server:
  port: 9090
  management_port: 9091
defaults:
  status_code: 302
  preserve_query: true
rules:
  - id: test-rule
    match:
      type: exact
      path: /test
    redirect:
      to: https://example.com/test
`

	// Write temp config file
	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, writeErr := tmpFile.WriteString(content); writeErr != nil {
		t.Fatalf("Failed to write temp file: %v", writeErr)
	}
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Version != "1.0" {
		t.Errorf("Expected version 1.0, got %s", cfg.Version)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("Expected port 9090, got %d", cfg.Server.Port)
	}

	if cfg.Server.ManagementPort != 9091 {
		t.Errorf("Expected management port 9091, got %d", cfg.Server.ManagementPort)
	}

	if len(cfg.Rules) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(cfg.Rules))
	}

	if cfg.Rules[0].ID != "test-rule" {
		t.Errorf("Expected rule ID test-rule, got %s", cfg.Rules[0].ID)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	content := `
invalid: yaml: content
  - not valid
`

	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, writeErr := tmpFile.WriteString(content); writeErr != nil {
		t.Fatalf("Failed to write temp file: %v", writeErr)
	}
	tmpFile.Close()

	_, err = Load(tmpFile.Name())
	if err == nil {
		t.Error("Expected error for invalid YAML, got nil")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("Expected error for missing file, got nil")
	}
}

func TestLoad_EnvironmentVariables(t *testing.T) {
	os.Setenv("TEST_DESTINATION", "https://test.example.com")
	defer os.Unsetenv("TEST_DESTINATION")

	content := `
version: "1.0"
rules:
  - id: env-test
    match:
      path: /test
    redirect:
      to: ${TEST_DESTINATION}
`

	tmpFile, err := os.CreateTemp("", "config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, writeErr := tmpFile.WriteString(content); writeErr != nil {
		t.Fatalf("Failed to write temp file: %v", writeErr)
	}
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Rules[0].Redirect.To != "https://test.example.com" {
		t.Errorf("Expected https://test.example.com, got %s", cfg.Rules[0].Redirect.To)
	}
}

func TestValidate_NoRules(t *testing.T) {
	cfg := &Config{
		Rules: []Rule{},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for no rules, got nil")
	}
}

func TestValidate_DuplicateIDs(t *testing.T) {
	cfg := &Config{
		Rules: []Rule{
			{ID: "duplicate", Match: Match{Path: "/a"}, Redirect: Redirect{To: "https://a.com", Status: 301}},
			{ID: "duplicate", Match: Match{Path: "/b"}, Redirect: Redirect{To: "https://b.com", Status: 301}},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for duplicate IDs, got nil")
	}
}

func TestValidate_InvalidStatusCode(t *testing.T) {
	cfg := &Config{
		Rules: []Rule{
			{ID: "invalid-status", Match: Match{Path: "/test"}, Redirect: Redirect{To: "https://example.com", Status: 200}},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for invalid status code, got nil")
	}
}

func TestValidate_InvalidRegex(t *testing.T) {
	cfg := &Config{
		Rules: []Rule{
			{ID: "bad-regex", Match: Match{Type: MatchTypeRegex, Pattern: "[invalid("}, Redirect: Redirect{To: "https://example.com", Status: 301}},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for invalid regex, got nil")
	}
}

func TestGlobToRegex(t *testing.T) {
	tests := []struct {
		glob     string
		expected string
	}{
		{"/api/*", "^/api/[^/]*$"},
		{"/docs/**", "^/docs/.*$"},
		{"/path/*/end", "^/path/[^/]*/end$"},
		{"/exact/path", "^/exact/path$"},
		{"/with.dot", "^/with\\.dot$"},
	}

	for _, tt := range tests {
		t.Run(tt.glob, func(t *testing.T) {
			result := globToRegex(tt.glob)
			if result != tt.expected {
				t.Errorf("globToRegex(%s) = %s, want %s", tt.glob, result, tt.expected)
			}
		})
	}
}

func TestApplyDefaults(t *testing.T) {
	cfg := &Config{
		Rules: []Rule{
			{Match: Match{Path: "/test"}, Redirect: Redirect{To: "https://example.com"}},
		},
	}

	cfg.applyDefaults()

	if cfg.Server.Port != 8080 {
		t.Errorf("Expected default port 8080, got %d", cfg.Server.Port)
	}

	if cfg.Server.ManagementPort != 8081 {
		t.Errorf("Expected default management port 8081, got %d", cfg.Server.ManagementPort)
	}

	if cfg.Defaults.StatusCode != 301 {
		t.Errorf("Expected default status 301, got %d", cfg.Defaults.StatusCode)
	}

	if cfg.Rules[0].ID == "" {
		t.Error("Expected auto-generated rule ID")
	}

	if cfg.Rules[0].Redirect.Status != 301 {
		t.Errorf("Expected rule to inherit default status 301, got %d", cfg.Rules[0].Redirect.Status)
	}
}
