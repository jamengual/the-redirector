package lint

import (
	"testing"

	"github.com/jamengual/the-redirector/internal/config"
)

func boolPtr(b bool) *bool { return &b }

func TestLinter_CheckDuplicateIDs(t *testing.T) {
	cfg := &config.Config{
		Rules: []config.Rule{
			{ID: "rule-1", Match: config.Match{Type: config.MatchTypeExact, Path: "/a"}, Redirect: config.Redirect{Status: 301, To: "http://a.com"}},
			{ID: "rule-2", Match: config.Match{Type: config.MatchTypeExact, Path: "/b"}, Redirect: config.Redirect{Status: 301, To: "http://b.com"}},
			{ID: "rule-1", Match: config.Match{Type: config.MatchTypeExact, Path: "/c"}, Redirect: config.Redirect{Status: 301, To: "http://c.com"}}, // Duplicate
		},
	}

	linter := New(cfg)
	result := linter.Lint()

	errors := result.Errors()
	if len(errors) != 1 {
		t.Errorf("Expected 1 error for duplicate ID, got %d", len(errors))
	}

	if len(errors) > 0 && errors[0].RuleID != "rule-1" {
		t.Errorf("Expected error for rule-1, got %s", errors[0].RuleID)
	}
}

func TestLinter_CheckOverlappingPatterns_Exact(t *testing.T) {
	cfg := &config.Config{
		Rules: []config.Rule{
			{ID: "rule-1", Match: config.Match{Type: config.MatchTypeExact, Path: "/same"}, Redirect: config.Redirect{Status: 301, To: "http://a.com"}},
			{ID: "rule-2", Match: config.Match{Type: config.MatchTypeExact, Path: "/same"}, Redirect: config.Redirect{Status: 301, To: "http://b.com"}}, // Same exact path
		},
	}

	linter := New(cfg)
	result := linter.Lint()

	warnings := result.Warnings()
	if len(warnings) != 1 {
		t.Errorf("Expected 1 warning for overlapping exact paths, got %d", len(warnings))
	}
}

func TestLinter_CheckOverlappingPatterns_Prefix(t *testing.T) {
	cfg := &config.Config{
		Rules: []config.Rule{
			{ID: "rule-1", Match: config.Match{Type: config.MatchTypePrefix, Path: "/api/"}, Redirect: config.Redirect{Status: 301, To: "http://a.com"}},
			{ID: "rule-2", Match: config.Match{Type: config.MatchTypePrefix, Path: "/api/v1/"}, Redirect: config.Redirect{Status: 301, To: "http://b.com"}}, // Contained
		},
	}

	linter := New(cfg)
	result := linter.Lint()

	warnings := result.Warnings()
	if len(warnings) != 1 {
		t.Errorf("Expected 1 warning for overlapping prefixes, got %d", len(warnings))
	}
}

func TestLinter_CheckGreedyPatterns_Glob(t *testing.T) {
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				ID:       "greedy",
				Match:    config.Match{Type: config.MatchTypeGlob, Pattern: "/**"},
				Redirect: config.Redirect{Status: 301, To: "http://fallback.com"},
				Priority: 0, // Default priority
			},
		},
	}

	linter := New(cfg)
	result := linter.Lint()

	warnings := result.Warnings()
	found := false
	for _, w := range warnings {
		if w.RuleID == "greedy" {
			found = true
		}
	}
	if !found {
		t.Error("Expected warning for greedy glob pattern")
	}
}

func TestLinter_CheckGreedyPatterns_NegativePriority(t *testing.T) {
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				ID:       "fallback",
				Match:    config.Match{Type: config.MatchTypeGlob, Pattern: "/**"},
				Redirect: config.Redirect{Status: 301, To: "http://fallback.com"},
				Priority: -100, // Negative priority - OK
			},
		},
	}

	linter := New(cfg)
	result := linter.Lint()

	// Should not warn about greedy pattern with negative priority
	for _, w := range result.Warnings() {
		if w.RuleID == "fallback" && w.Message == "Greedy glob pattern '/**' will match many paths" {
			t.Error("Should not warn about greedy pattern with negative priority")
		}
	}
}

func TestLinter_CheckRegexPerformance_MultipleWildcard(t *testing.T) {
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				ID:       "slow-regex",
				Match:    config.Match{Type: config.MatchTypeRegex, Pattern: ".*foo.*bar.*"},
				Redirect: config.Redirect{Status: 301, To: "http://example.com"},
			},
		},
	}

	// Need to validate to compile the regex
	cfg.Validate()

	linter := New(cfg)
	result := linter.Lint()

	warnings := result.Warnings()
	found := false
	for _, w := range warnings {
		if w.RuleID == "slow-regex" && w.Message == "Multiple '.*' in pattern can cause exponential backtracking" {
			found = true
		}
	}
	if !found {
		t.Error("Expected warning for multiple .* in pattern")
	}
}

func TestLinter_CheckRegexPerformance_NestedQuantifiers(t *testing.T) {
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				ID:       "nested",
				Match:    config.Match{Type: config.MatchTypeRegex, Pattern: "(a+)+"},
				Redirect: config.Redirect{Status: 301, To: "http://example.com"},
			},
		},
	}

	cfg.Validate()

	linter := New(cfg)
	result := linter.Lint()

	errors := result.Errors()
	found := false
	for _, e := range errors {
		if e.RuleID == "nested" && e.Message == "Nested quantifiers can cause catastrophic backtracking" {
			found = true
		}
	}
	if !found {
		t.Error("Expected error for nested quantifiers")
	}
}

func TestLinter_CheckMissingDefaults(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.DefaultConfig{
			StatusCode: 0, // No default status
		},
		Rules: []config.Rule{
			{ID: "rule-1", Match: config.Match{Type: config.MatchTypeExact, Path: "/a"}, Redirect: config.Redirect{Status: 0, To: "http://a.com"}},
			{ID: "rule-2", Match: config.Match{Type: config.MatchTypeExact, Path: "/b"}, Redirect: config.Redirect{Status: 0, To: "http://b.com"}},
		},
	}

	linter := New(cfg)
	issues := linter.checkMissingDefaults()

	if len(issues) != 1 {
		t.Errorf("Expected 1 info issue for missing defaults, got %d", len(issues))
	}
}

func TestResult_HasErrors(t *testing.T) {
	tests := []struct {
		name     string
		issues   []Issue
		expected bool
	}{
		{
			name:     "no issues",
			issues:   nil,
			expected: false,
		},
		{
			name: "only warnings",
			issues: []Issue{
				{Severity: SeverityWarning, Message: "warning"},
			},
			expected: false,
		},
		{
			name: "has error",
			issues: []Issue{
				{Severity: SeverityWarning, Message: "warning"},
				{Severity: SeverityError, Message: "error"},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Result{Issues: tt.issues}
			if r.HasErrors() != tt.expected {
				t.Errorf("HasErrors() = %v, expected %v", r.HasErrors(), tt.expected)
			}
		})
	}
}

func TestLinter_RulesCount(t *testing.T) {
	cfg := &config.Config{
		Rules: []config.Rule{
			{ID: "rule-1", Match: config.Match{Type: config.MatchTypeExact, Path: "/a"}, Redirect: config.Redirect{Status: 301, To: "http://a.com"}},
			{ID: "rule-2", Match: config.Match{Type: config.MatchTypeExact, Path: "/b"}, Redirect: config.Redirect{Status: 301, To: "http://b.com"}},
			{ID: "rule-3", Match: config.Match{Type: config.MatchTypeExact, Path: "/c"}, Redirect: config.Redirect{Status: 301, To: "http://c.com"}},
		},
	}

	linter := New(cfg)
	result := linter.Lint()

	if result.RulesCount != 3 {
		t.Errorf("Expected RulesCount=3, got %d", result.RulesCount)
	}
}
