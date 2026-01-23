package router

import (
	"testing"

	"github.com/your-org/the-redirector/internal/config"
)

func TestRouter_ExactMatch(t *testing.T) {
	rules := []config.Rule{
		{
			ID: "exact-1",
			Match: config.Match{
				Type: config.MatchTypeExact,
				Path: "/foo",
			},
			Redirect: config.Redirect{
				To:     "https://example.com/foo",
				Status: 301,
			},
		},
		{
			ID: "exact-2",
			Match: config.Match{
				Type: config.MatchTypeExact,
				Path: "/bar",
			},
			Redirect: config.Redirect{
				To:     "https://example.com/bar",
				Status: 302,
			},
		},
	}

	r, err := New(rules)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	tests := []struct {
		name     string
		path     string
		wantID   string
		wantNil  bool
	}{
		{"exact match /foo", "/foo", "exact-1", false},
		{"exact match /bar", "/bar", "exact-2", false},
		{"no match", "/baz", "", true},
		{"partial match", "/foo/bar", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule, _ := r.Match("", tt.path)

			if tt.wantNil {
				if rule != nil {
					t.Errorf("Expected nil, got rule %s", rule.ID)
				}
				return
			}

			if rule == nil {
				t.Errorf("Expected rule %s, got nil", tt.wantID)
				return
			}

			if rule.ID != tt.wantID {
				t.Errorf("Expected rule %s, got %s", tt.wantID, rule.ID)
			}
		})
	}
}

func TestRouter_PrefixMatch(t *testing.T) {
	rules := []config.Rule{
		{
			ID: "prefix-long",
			Match: config.Match{
				Type: config.MatchTypePrefix,
				Path: "/api/v1/users/",
			},
			Redirect: config.Redirect{
				To:     "https://api.example.com/v2/users/",
				Status: 301,
			},
		},
		{
			ID: "prefix-short",
			Match: config.Match{
				Type: config.MatchTypePrefix,
				Path: "/api/",
			},
			Redirect: config.Redirect{
				To:     "https://api.example.com/",
				Status: 301,
			},
		},
	}

	r, err := New(rules)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	tests := []struct {
		name   string
		path   string
		wantID string
	}{
		{"longer prefix wins", "/api/v1/users/123", "prefix-long"},
		{"shorter prefix match", "/api/v2/products", "prefix-short"},
		{"exact prefix match", "/api/", "prefix-short"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule, _ := r.Match("", tt.path)

			if rule == nil {
				t.Errorf("Expected rule %s, got nil", tt.wantID)
				return
			}

			if rule.ID != tt.wantID {
				t.Errorf("Expected rule %s, got %s", tt.wantID, rule.ID)
			}
		})
	}
}

func TestRouter_RegexMatch(t *testing.T) {
	preserveQuery := true
	rules := []config.Rule{
		{
			ID: "product-regex",
			Match: config.Match{
				Type:    config.MatchTypeRegex,
				Pattern: `^/product/(\d+)$`,
			},
			Redirect: config.Redirect{
				To:            "https://shop.example.com/item/$1",
				Status:        301,
				PreserveQuery: &preserveQuery,
			},
		},
		{
			ID: "user-regex",
			Match: config.Match{
				Type:    config.MatchTypeRegex,
				Pattern: `^/user/([a-z]+)/profile$`,
			},
			Redirect: config.Redirect{
				To:            "https://profiles.example.com/$1",
				Status:        301,
				PreserveQuery: &preserveQuery,
			},
		},
	}

	// Validate and compile patterns
	cfg := &config.Config{Rules: rules}
	cfg.Validate()

	r, err := New(cfg.Rules)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	tests := []struct {
		name         string
		path         string
		wantID       string
		wantCaptures []string
	}{
		{"product match", "/product/12345", "product-regex", []string{"12345"}},
		{"user match", "/user/john/profile", "user-regex", []string{"john"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule, captures := r.Match("", tt.path)

			if rule == nil {
				t.Errorf("Expected rule %s, got nil", tt.wantID)
				return
			}

			if rule.ID != tt.wantID {
				t.Errorf("Expected rule %s, got %s", tt.wantID, rule.ID)
			}

			if len(captures) != len(tt.wantCaptures) {
				t.Errorf("Expected %d captures, got %d", len(tt.wantCaptures), len(captures))
				return
			}

			for i, c := range captures {
				if c != tt.wantCaptures[i] {
					t.Errorf("Capture %d: expected %s, got %s", i, tt.wantCaptures[i], c)
				}
			}
		})
	}
}

func TestRouter_Stats(t *testing.T) {
	preserveQuery := true
	rules := []config.Rule{
		{
			ID:    "exact-1",
			Match: config.Match{Type: config.MatchTypeExact, Path: "/a"},
			Redirect: config.Redirect{To: "https://example.com/a", Status: 301, PreserveQuery: &preserveQuery},
		},
		{
			ID:    "exact-2",
			Match: config.Match{Type: config.MatchTypeExact, Path: "/b"},
			Redirect: config.Redirect{To: "https://example.com/b", Status: 301, PreserveQuery: &preserveQuery},
		},
		{
			ID:    "prefix-1",
			Match: config.Match{Type: config.MatchTypePrefix, Path: "/api/"},
			Redirect: config.Redirect{To: "https://api.example.com/", Status: 301, PreserveQuery: &preserveQuery},
		},
		{
			ID:    "regex-1",
			Match: config.Match{Type: config.MatchTypeRegex, Pattern: `^/product/\d+$`},
			Redirect: config.Redirect{To: "https://shop.example.com/", Status: 301, PreserveQuery: &preserveQuery},
		},
	}

	// Validate
	cfg := &config.Config{Rules: rules}
	cfg.Validate()

	r, _ := New(cfg.Rules)
	stats := r.GetStats()

	if stats.ExactRules != 2 {
		t.Errorf("Expected 2 exact rules, got %d", stats.ExactRules)
	}
	if stats.PrefixRules != 1 {
		t.Errorf("Expected 1 prefix rule, got %d", stats.PrefixRules)
	}
	if stats.RegexRules != 1 {
		t.Errorf("Expected 1 regex rule, got %d", stats.RegexRules)
	}
	if stats.TotalRules != 4 {
		t.Errorf("Expected 4 total rules, got %d", stats.TotalRules)
	}
}

// Benchmarks

func BenchmarkRouter_ExactMatch(b *testing.B) {
	rules := make([]config.Rule, 1000)
	for i := 0; i < 1000; i++ {
		preserveQuery := true
		rules[i] = config.Rule{
			ID:    string(rune(i)),
			Match: config.Match{Type: config.MatchTypeExact, Path: "/path/" + string(rune(i))},
			Redirect: config.Redirect{To: "https://example.com/", Status: 301, PreserveQuery: &preserveQuery},
		}
	}

	r, _ := New(rules)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Match("", "/path/a")
	}
}

func BenchmarkRouter_PrefixMatch(b *testing.B) {
	rules := make([]config.Rule, 100)
	for i := 0; i < 100; i++ {
		preserveQuery := true
		rules[i] = config.Rule{
			ID:    string(rune(i)),
			Match: config.Match{Type: config.MatchTypePrefix, Path: "/prefix/" + string(rune(i)) + "/"},
			Redirect: config.Redirect{To: "https://example.com/", Status: 301, PreserveQuery: &preserveQuery},
		}
	}

	r, _ := New(rules)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Match("", "/prefix/a/some/path")
	}
}

func BenchmarkRouter_RegexMatch(b *testing.B) {
	preserveQuery := true
	rules := []config.Rule{
		{
			ID:       "regex-1",
			Match:    config.Match{Type: config.MatchTypeRegex, Pattern: `^/product/(\d+)$`},
			Redirect: config.Redirect{To: "https://example.com/$1", Status: 301, PreserveQuery: &preserveQuery},
		},
	}

	cfg := &config.Config{Rules: rules}
	cfg.Validate()

	r, _ := New(cfg.Rules)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Match("", "/product/12345")
	}
}
