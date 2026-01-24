package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseShortForm_Simple(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantSrc    string
		wantDest   string
		wantStatus int
	}{
		{
			name:       "simple redirect",
			input:      "/old -> https://new.com/path",
			wantSrc:    "/old",
			wantDest:   "https://new.com/path",
			wantStatus: 301,
		},
		{
			name:       "with status code",
			input:      "/old -> https://new.com [302]",
			wantSrc:    "/old",
			wantDest:   "https://new.com",
			wantStatus: 302,
		},
		{
			name:       "with spaces",
			input:      "  /old   ->   https://new.com   ",
			wantSrc:    "/old",
			wantDest:   "https://new.com",
			wantStatus: 301,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule, err := ParseShortForm(tt.input)
			if err != nil {
				t.Fatalf("ParseShortForm failed: %v", err)
			}
			if rule == nil {
				t.Fatal("Expected rule, got nil")
			}
			if rule.Source != tt.wantSrc {
				t.Errorf("Source = %q, want %q", rule.Source, tt.wantSrc)
			}
			if rule.Destination != tt.wantDest {
				t.Errorf("Destination = %q, want %q", rule.Destination, tt.wantDest)
			}
			if rule.Status != tt.wantStatus {
				t.Errorf("Status = %d, want %d", rule.Status, tt.wantStatus)
			}
		})
	}
}

func TestParseShortForm_Options(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		wantPreservePath  bool
		wantPreserveQuery bool
		wantStatus        int
	}{
		{
			name:             "preserve_path",
			input:            "/old/* -> https://new.com [preserve_path]",
			wantPreservePath: true,
			wantStatus:       301,
		},
		{
			name:              "multiple options",
			input:             "/old/* -> https://new.com [302, preserve_path, preserve_query]",
			wantPreservePath:  true,
			wantPreserveQuery: true,
			wantStatus:        302,
		},
		{
			name:             "options with spaces",
			input:            "/old -> https://new.com [ 307 , preserve_path ]",
			wantPreservePath: true,
			wantStatus:       307,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule, err := ParseShortForm(tt.input)
			if err != nil {
				t.Fatalf("ParseShortForm failed: %v", err)
			}
			if rule.PreservePath != tt.wantPreservePath {
				t.Errorf("PreservePath = %v, want %v", rule.PreservePath, tt.wantPreservePath)
			}
			if rule.PreserveQuery != tt.wantPreserveQuery {
				t.Errorf("PreserveQuery = %v, want %v", rule.PreserveQuery, tt.wantPreserveQuery)
			}
			if rule.Status != tt.wantStatus {
				t.Errorf("Status = %d, want %d", rule.Status, tt.wantStatus)
			}
		})
	}
}

func TestParseShortForm_Host(t *testing.T) {
	rule, err := ParseShortForm("old.example.com:/path -> https://new.com")
	if err != nil {
		t.Fatalf("ParseShortForm failed: %v", err)
	}
	if rule.Host != "old.example.com" {
		t.Errorf("Host = %q, want %q", rule.Host, "old.example.com")
	}
	if rule.Source != "/path" {
		t.Errorf("Source = %q, want %q", rule.Source, "/path")
	}
}

func TestParseShortForm_SkipsCommentsAndEmpty(t *testing.T) {
	tests := []string{
		"",
		"   ",
		"# comment",
		"  # indented comment",
	}

	for _, input := range tests {
		rule, err := ParseShortForm(input)
		if err != nil {
			t.Errorf("ParseShortForm(%q) returned error: %v", input, err)
		}
		if rule != nil {
			t.Errorf("ParseShortForm(%q) returned non-nil rule", input)
		}
	}
}

func TestParseCSVLine(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantSrc    string
		wantDest   string
		wantStatus int
		wantPP     bool
	}{
		{
			name:       "simple two columns",
			input:      "/old,https://new.com",
			wantSrc:    "/old",
			wantDest:   "https://new.com",
			wantStatus: 301,
		},
		{
			name:       "with status",
			input:      "/old,https://new.com,302",
			wantSrc:    "/old",
			wantDest:   "https://new.com",
			wantStatus: 302,
		},
		{
			name:       "with options",
			input:      "/blog/*,https://blog.com/,301,preserve_path",
			wantSrc:    "/blog/*",
			wantDest:   "https://blog.com/",
			wantStatus: 301,
			wantPP:     true,
		},
		{
			name:       "with spaces",
			input:      "  /old  ,  https://new.com  ,  307  ",
			wantSrc:    "/old",
			wantDest:   "https://new.com",
			wantStatus: 307,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule, err := ParseCSVLine(tt.input)
			if err != nil {
				t.Fatalf("ParseCSVLine failed: %v", err)
			}
			if rule.Source != tt.wantSrc {
				t.Errorf("Source = %q, want %q", rule.Source, tt.wantSrc)
			}
			if rule.Destination != tt.wantDest {
				t.Errorf("Destination = %q, want %q", rule.Destination, tt.wantDest)
			}
			if rule.Status != tt.wantStatus {
				t.Errorf("Status = %d, want %d", rule.Status, tt.wantStatus)
			}
			if rule.PreservePath != tt.wantPP {
				t.Errorf("PreservePath = %v, want %v", rule.PreservePath, tt.wantPP)
			}
		})
	}
}

func TestShortFormRule_ToRule(t *testing.T) {
	tests := []struct {
		name          string
		shortForm     ShortFormRule
		wantMatchType MatchType
	}{
		{
			name: "exact match",
			shortForm: ShortFormRule{
				Source:      "/exact/path",
				Destination: "https://new.com",
			},
			wantMatchType: MatchTypeExact,
		},
		{
			name: "prefix match (trailing slash)",
			shortForm: ShortFormRule{
				Source:      "/prefix/",
				Destination: "https://new.com",
			},
			wantMatchType: MatchTypePrefix,
		},
		{
			name: "glob match",
			shortForm: ShortFormRule{
				Source:      "/glob/*",
				Destination: "https://new.com",
			},
			wantMatchType: MatchTypeGlob,
		},
		{
			name: "glob double wildcard",
			shortForm: ShortFormRule{
				Source:      "/glob/**",
				Destination: "https://new.com",
			},
			wantMatchType: MatchTypeGlob,
		},
		{
			name: "regex match",
			shortForm: ShortFormRule{
				Source:      "^/regex/(\\d+)$",
				Destination: "https://new.com/$1",
			},
			wantMatchType: MatchTypeRegex,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := tt.shortForm.ToRule("test-id")
			if rule.Match.Type != tt.wantMatchType {
				t.Errorf("Match.Type = %q, want %q", rule.Match.Type, tt.wantMatchType)
			}
		})
	}
}

func TestLoadCSVFile(t *testing.T) {
	// Create temp CSV file
	content := `# Header comment
/old,https://new.com
/blog/*,https://blog.com/,301,preserve_path
/api/v1/*,https://api.com/v2/,307,preserve_path,preserve_query

# Another comment
/legacy/**,https://new.com/,301
`
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "rules.csv")
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test CSV: %v", err)
	}

	rules, err := LoadCSVFile(csvPath)
	if err != nil {
		t.Fatalf("LoadCSVFile failed: %v", err)
	}

	if len(rules) != 4 {
		t.Errorf("Expected 4 rules, got %d", len(rules))
	}

	// Check first rule
	if rules[0].Match.Path != "/old" {
		t.Errorf("First rule path = %q, want %q", rules[0].Match.Path, "/old")
	}
	if rules[0].Redirect.To != "https://new.com" {
		t.Errorf("First rule destination = %q, want %q", rules[0].Redirect.To, "https://new.com")
	}

	// Check rule with preserve_path
	if !rules[1].Redirect.PreservePath {
		t.Error("Second rule should have preserve_path=true")
	}
}

func TestLoadFlexible_ShortFormYAML(t *testing.T) {
	// Create temp YAML file with short-form rules
	content := `version: "1.0"

defaults:
  status_code: 301

rules:
  # Short-form rules
  - /old -> https://new.com
  - /blog/* -> https://blog.com/ [preserve_path]
  - /api/v1/* -> https://api.com/v2/ [307, preserve_path]

  # Full-form rule
  - id: full-form
    match:
      type: exact
      path: /full
    redirect:
      to: https://full.com
      status: 302
`
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test YAML: %v", err)
	}

	cfg, err := LoadFlexible(yamlPath)
	if err != nil {
		t.Fatalf("LoadFlexible failed: %v", err)
	}

	if len(cfg.Rules) != 4 {
		t.Errorf("Expected 4 rules, got %d", len(cfg.Rules))
	}

	// Check full-form rule
	var foundFullForm bool
	for _, r := range cfg.Rules {
		if r.ID == "full-form" {
			foundFullForm = true
			if r.Redirect.Status != 302 {
				t.Errorf("Full-form rule status = %d, want 302", r.Redirect.Status)
			}
		}
	}
	if !foundFullForm {
		t.Error("Full-form rule not found")
	}
}

func TestLoadFlexible_WithIncludes(t *testing.T) {
	tmpDir := t.TempDir()

	// Create CSV rules file
	csvContent := `/csv-rule,https://csv.com
/csv-blog/*,https://csv-blog.com/,302,preserve_path
`
	csvPath := filepath.Join(tmpDir, "extra-rules.csv")
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("Failed to write CSV: %v", err)
	}

	// Create main config with include
	yamlContent := `version: "1.0"

defaults:
  status_code: 301

rules:
  - /main -> https://main.com

rules_include:
  - extra-rules.csv
`
	yamlPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write YAML: %v", err)
	}

	cfg, err := LoadFlexible(yamlPath)
	if err != nil {
		t.Fatalf("LoadFlexible failed: %v", err)
	}

	// Should have 1 from main + 2 from CSV = 3 rules
	if len(cfg.Rules) != 3 {
		t.Errorf("Expected 3 rules, got %d", len(cfg.Rules))
	}

	// Verify CSV rules were loaded
	foundCSV := false
	for _, r := range cfg.Rules {
		if r.Redirect.To == "https://csv.com" {
			foundCSV = true
		}
	}
	if !foundCSV {
		t.Error("CSV rules not loaded")
	}
}
