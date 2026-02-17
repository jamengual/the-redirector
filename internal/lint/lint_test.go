package lint

import (
	"strings"
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
	_ = cfg.Validate()

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

	_ = cfg.Validate()

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

// Multi-source linting tests

func TestMultiSourceLinter_NoConflicts(t *testing.T) {
	sources := []SourceInput{
		{
			Name:   "marketing",
			Prefix: "marketing",
			Config: &config.Config{
				Rules: []config.Rule{
					{ID: "promo", Match: config.Match{Type: config.MatchTypeExact, Path: "/promo/spring"}, Redirect: config.Redirect{Status: 301, To: "http://marketing.com"}},
				},
			},
		},
		{
			Name:   "engineering",
			Prefix: "eng",
			Config: &config.Config{
				Rules: []config.Rule{
					{ID: "docs", Match: config.Match{Type: config.MatchTypeExact, Path: "/docs/v1"}, Redirect: config.Redirect{Status: 301, To: "http://docs.com"}},
				},
			},
		},
	}

	linter := NewMultiSource(sources)
	result := linter.Lint()

	if result.HasConflicts() {
		t.Errorf("Expected no conflicts, got %d", len(result.Conflicts))
	}

	if result.TotalRules != 2 {
		t.Errorf("Expected 2 total rules, got %d", result.TotalRules)
	}
}

func TestMultiSourceLinter_ExactPathConflict(t *testing.T) {
	sources := []SourceInput{
		{
			Name:   "team-a",
			Prefix: "team-a",
			Config: &config.Config{
				Rules: []config.Rule{
					{ID: "rule-1", Match: config.Match{Type: config.MatchTypeExact, Path: "/same-path"}, Redirect: config.Redirect{Status: 301, To: "http://a.com"}},
				},
			},
		},
		{
			Name:   "team-b",
			Prefix: "team-b",
			Config: &config.Config{
				Rules: []config.Rule{
					{ID: "rule-2", Match: config.Match{Type: config.MatchTypeExact, Path: "/same-path"}, Redirect: config.Redirect{Status: 301, To: "http://b.com"}},
				},
			},
		},
	}

	linter := NewMultiSource(sources)
	result := linter.Lint()

	if !result.HasConflicts() {
		t.Error("Expected conflict for same path from different teams")
	}

	if len(result.Conflicts) != 1 {
		t.Errorf("Expected 1 conflict, got %d", len(result.Conflicts))
	}

	conflict := result.Conflicts[0]
	if conflict.MatchType != "exact" {
		t.Errorf("Expected 'exact' match type, got %s", conflict.MatchType)
	}
	if conflict.Path != "/same-path" {
		t.Errorf("Expected '/same-path' path, got %s", conflict.Path)
	}
}

func TestMultiSourceLinter_PrefixOverlapConflict(t *testing.T) {
	sources := []SourceInput{
		{
			Name:   "team-a",
			Prefix: "team-a",
			Config: &config.Config{
				Rules: []config.Rule{
					{ID: "prefix", Match: config.Match{Type: config.MatchTypePrefix, Path: "/api/"}, Redirect: config.Redirect{Status: 301, To: "http://a.com"}},
				},
			},
		},
		{
			Name:   "team-b",
			Prefix: "team-b",
			Config: &config.Config{
				Rules: []config.Rule{
					{ID: "exact", Match: config.Match{Type: config.MatchTypeExact, Path: "/api/users"}, Redirect: config.Redirect{Status: 301, To: "http://b.com"}},
				},
			},
		},
	}

	linter := NewMultiSource(sources)
	result := linter.Lint()

	if !result.HasConflicts() {
		t.Error("Expected conflict for prefix overlapping exact path")
	}

	// Check that the conflict description mentions the overlap
	found := false
	for _, c := range result.Conflicts {
		if c.MatchType == "overlap" {
			found = true
		}
	}
	if !found {
		t.Error("Expected overlap conflict type")
	}
}

func TestMultiSourceLinter_RulesPerSource(t *testing.T) {
	sources := []SourceInput{
		{
			Name:   "source-1",
			Prefix: "s1",
			Config: &config.Config{
				Rules: []config.Rule{
					{ID: "a", Match: config.Match{Type: config.MatchTypeExact, Path: "/a"}, Redirect: config.Redirect{Status: 301, To: "http://a.com"}},
					{ID: "b", Match: config.Match{Type: config.MatchTypeExact, Path: "/b"}, Redirect: config.Redirect{Status: 301, To: "http://b.com"}},
				},
			},
		},
		{
			Name:   "source-2",
			Prefix: "s2",
			Config: &config.Config{
				Rules: []config.Rule{
					{ID: "c", Match: config.Match{Type: config.MatchTypeExact, Path: "/c"}, Redirect: config.Redirect{Status: 301, To: "http://c.com"}},
				},
			},
		},
	}

	linter := NewMultiSource(sources)
	result := linter.Lint()

	if result.RulesPerSource["source-1"] != 2 {
		t.Errorf("Expected 2 rules for source-1, got %d", result.RulesPerSource["source-1"])
	}
	if result.RulesPerSource["source-2"] != 1 {
		t.Errorf("Expected 1 rule for source-2, got %d", result.RulesPerSource["source-2"])
	}
}

// --- Circular Redirect Detection Tests ---

func TestLinter_CheckCircularRedirects_DirectCycle(t *testing.T) {
	// A -> B -> A (exact rules redirecting to relative paths)
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				ID:       "rule-a",
				Match:    config.Match{Type: config.MatchTypeExact, Path: "/page-a"},
				Redirect: config.Redirect{Status: 301, To: "/page-b"},
			},
			{
				ID:       "rule-b",
				Match:    config.Match{Type: config.MatchTypeExact, Path: "/page-b"},
				Redirect: config.Redirect{Status: 301, To: "/page-a"},
			},
		},
	}

	linter := New(cfg)
	result := linter.Lint()

	errors := result.Errors()
	found := false
	for _, e := range errors {
		if e.RuleID == "rule-a" || e.RuleID == "rule-b" {
			if contains(e.Message, "Circular redirect detected") {
				found = true
			}
		}
	}
	if !found {
		t.Error("Expected error for direct circular redirect (A -> B -> A)")
		for _, e := range errors {
			t.Logf("  Error: %s (rule: %s)", e.Message, e.RuleID)
		}
	}
}

func TestLinter_CheckCircularRedirects_TransitiveCycle(t *testing.T) {
	// A -> B -> C -> A
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				ID:       "rule-a",
				Match:    config.Match{Type: config.MatchTypeExact, Path: "/a"},
				Redirect: config.Redirect{Status: 301, To: "/b"},
			},
			{
				ID:       "rule-b",
				Match:    config.Match{Type: config.MatchTypeExact, Path: "/b"},
				Redirect: config.Redirect{Status: 302, To: "/c"},
			},
			{
				ID:       "rule-c",
				Match:    config.Match{Type: config.MatchTypeExact, Path: "/c"},
				Redirect: config.Redirect{Status: 301, To: "/a"},
			},
		},
	}

	linter := New(cfg)
	result := linter.Lint()

	errors := result.Errors()
	found := false
	for _, e := range errors {
		if contains(e.Message, "Circular redirect detected") &&
			contains(e.Message, "rule-a") &&
			contains(e.Message, "rule-b") &&
			contains(e.Message, "rule-c") {
			found = true
		}
	}
	if !found {
		t.Error("Expected error for transitive circular redirect (A -> B -> C -> A)")
		for _, e := range errors {
			t.Logf("  Error: %s (rule: %s)", e.Message, e.RuleID)
		}
	}
}

func TestLinter_CheckCircularRedirects_NoCycle(t *testing.T) {
	// A -> B, C -> D — no cycles
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				ID:       "rule-a",
				Match:    config.Match{Type: config.MatchTypeExact, Path: "/a"},
				Redirect: config.Redirect{Status: 301, To: "/b"},
			},
			{
				ID:       "rule-b",
				Match:    config.Match{Type: config.MatchTypeExact, Path: "/b"},
				Redirect: config.Redirect{Status: 301, To: "/final"},
			},
			{
				ID:       "rule-c",
				Match:    config.Match{Type: config.MatchTypeExact, Path: "/c"},
				Redirect: config.Redirect{Status: 301, To: "/d"},
			},
		},
	}

	linter := New(cfg)
	result := linter.Lint()

	for _, e := range result.Errors() {
		if contains(e.Message, "Circular redirect") {
			t.Errorf("Expected no circular redirect errors, got: %s", e.Message)
		}
	}
}

func TestLinter_CheckCircularRedirects_ExternalDestination(t *testing.T) {
	// Rules redirect to external hosts — should not be flagged as cycles
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				ID:       "to-google",
				Match:    config.Match{Type: config.MatchTypeExact, Path: "/search"},
				Redirect: config.Redirect{Status: 301, To: "https://google.com/search"},
			},
			{
				ID:       "to-github",
				Match:    config.Match{Type: config.MatchTypeExact, Path: "/code"},
				Redirect: config.Redirect{Status: 301, To: "https://github.com/"},
			},
		},
	}

	linter := New(cfg)
	result := linter.Lint()

	for _, e := range result.Errors() {
		if contains(e.Message, "Circular redirect") {
			t.Errorf("Expected no circular redirect errors for external destinations, got: %s", e.Message)
		}
	}
}

func TestLinter_CheckCircularRedirects_CrossHostCycle(t *testing.T) {
	// Rules with match.host — redirect to the same host creating a cycle
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				ID:       "host-a",
				Match:    config.Match{Type: config.MatchTypeExact, Path: "/page1", Host: "example.com"},
				Redirect: config.Redirect{Status: 301, To: "https://example.com/page2"},
			},
			{
				ID:       "host-b",
				Match:    config.Match{Type: config.MatchTypeExact, Path: "/page2", Host: "example.com"},
				Redirect: config.Redirect{Status: 301, To: "https://example.com/page1"},
			},
		},
	}

	linter := New(cfg)
	result := linter.Lint()

	errors := result.Errors()
	found := false
	for _, e := range errors {
		if contains(e.Message, "Circular redirect detected") {
			found = true
		}
	}
	if !found {
		t.Error("Expected error for cross-host circular redirect")
		for _, e := range errors {
			t.Logf("  Error: %s (rule: %s)", e.Message, e.RuleID)
		}
	}
}

func TestLinter_CheckCircularRedirects_PrefixSelfLoop(t *testing.T) {
	// Prefix rule with preserve_path redirecting into its own match space
	// /old/ -> /old/new/ with preserve_path
	// Request for /old/foo -> /old/new/foo -> /old/new/new/foo -> infinite
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				ID:       "self-loop",
				Match:    config.Match{Type: config.MatchTypePrefix, Path: "/old/"},
				Redirect: config.Redirect{Status: 301, To: "/old/new/", PreservePath: true},
			},
		},
	}

	linter := New(cfg)
	result := linter.Lint()

	errors := result.Errors()
	found := false
	for _, e := range errors {
		if e.RuleID == "self-loop" && contains(e.Message, "self-loop") {
			found = true
		}
	}
	if !found {
		t.Error("Expected error for prefix self-loop with preserve_path")
		for _, e := range errors {
			t.Logf("  Error: %s (rule: %s)", e.Message, e.RuleID)
		}
	}
}

func TestLinter_CheckCircularRedirects_PrefixNoSelfLoop(t *testing.T) {
	// Prefix rule with preserve_path but destination is OUTSIDE the match space
	// /old/ -> /new/ with preserve_path — safe, no loop
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				ID:       "safe-prefix",
				Match:    config.Match{Type: config.MatchTypePrefix, Path: "/old/"},
				Redirect: config.Redirect{Status: 301, To: "/new/", PreservePath: true},
			},
		},
	}

	linter := New(cfg)
	result := linter.Lint()

	for _, e := range result.Errors() {
		if e.RuleID == "safe-prefix" && contains(e.Message, "self-loop") {
			t.Errorf("Expected no self-loop error for safe prefix rule, got: %s", e.Message)
		}
	}
}

func TestLinter_CheckCircularRedirects_NonRedirectSkipped(t *testing.T) {
	// Non-redirect rules (404, 503) should be completely skipped
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				ID:       "not-found",
				Match:    config.Match{Type: config.MatchTypeExact, Path: "/missing"},
				Redirect: config.Redirect{Status: 404, Body: "Not Found"},
			},
			{
				ID:       "maintenance",
				Match:    config.Match{Type: config.MatchTypeExact, Path: "/api"},
				Redirect: config.Redirect{Status: 503, Body: "Maintenance"},
			},
		},
	}

	linter := New(cfg)
	result := linter.Lint()

	for _, e := range result.Errors() {
		if contains(e.Message, "Circular redirect") {
			t.Errorf("Expected no circular redirect errors for non-redirect rules, got: %s", e.Message)
		}
	}
}

func TestLinter_CheckCircularRedirects_PrefixCrossCycle(t *testing.T) {
	// Two prefix rules creating a cross-cycle
	// /foo/ -> /bar/  and  /bar/ -> /foo/
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				ID:       "foo-to-bar",
				Match:    config.Match{Type: config.MatchTypePrefix, Path: "/foo/"},
				Redirect: config.Redirect{Status: 301, To: "/bar/"},
			},
			{
				ID:       "bar-to-foo",
				Match:    config.Match{Type: config.MatchTypePrefix, Path: "/bar/"},
				Redirect: config.Redirect{Status: 301, To: "/foo/"},
			},
		},
	}

	linter := New(cfg)
	result := linter.Lint()

	errors := result.Errors()
	found := false
	for _, e := range errors {
		if contains(e.Message, "Circular redirect detected") {
			found = true
		}
	}
	if !found {
		t.Error("Expected error for prefix cross-cycle (/foo/ -> /bar/ -> /foo/)")
		for _, e := range errors {
			t.Logf("  Error: %s (rule: %s)", e.Message, e.RuleID)
		}
	}
}

func TestLinter_CheckCircularRedirects_RegexSelfCycle(t *testing.T) {
	// Regex rule that redirects back into its own match pattern
	// ^/api/v1/(.*) -> /api/v1/v2/$1
	// This destination /api/v1/v2/... still matches ^/api/v1/(.*)
	cfg := &config.Config{
		Rules: []config.Rule{
			{
				ID:       "api-loop",
				Match:    config.Match{Type: config.MatchTypeRegex, Pattern: "^/api/v1/(.*)"},
				Redirect: config.Redirect{Status: 301, To: "/api/v1/v2/$1"},
			},
		},
	}

	_ = cfg.Validate() // Compile regex

	linter := New(cfg)
	result := linter.Lint()

	warnings := result.Warnings()
	found := false
	for _, w := range warnings {
		if w.RuleID == "api-loop" && contains(w.Message, "Potential circular redirect") {
			found = true
		}
	}
	if !found {
		t.Error("Expected warning for regex self-cycle")
		for _, w := range warnings {
			t.Logf("  Warning: %s (rule: %s)", w.Message, w.RuleID)
		}
	}
}

// contains checks if a string contains a substring (test helper).
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestMultiSourceResult_HasErrors(t *testing.T) {
	// With conflicts
	result := &MultiSourceResult{
		Conflicts: []MultiSourceConflict{
			{Path: "/test"},
		},
	}
	if !result.HasErrors() {
		t.Error("Expected HasErrors() to return true when there are conflicts")
	}

	// Without conflicts, with error issue
	result = &MultiSourceResult{
		Issues: []Issue{
			{Severity: SeverityError, Message: "error"},
		},
	}
	if !result.HasErrors() {
		t.Error("Expected HasErrors() to return true when there are error issues")
	}

	// No errors
	result = &MultiSourceResult{}
	if result.HasErrors() {
		t.Error("Expected HasErrors() to return false when there are no conflicts or errors")
	}
}
