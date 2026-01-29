package router

import (
	"sync"
	"testing"

	"github.com/jamengual/the-redirector/internal/config"
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
		name    string
		path    string
		wantID  string
		wantNil bool
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
	_ = cfg.Validate()

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
			ID:       "exact-1",
			Match:    config.Match{Type: config.MatchTypeExact, Path: "/a"},
			Redirect: config.Redirect{To: "https://example.com/a", Status: 301, PreserveQuery: &preserveQuery},
		},
		{
			ID:       "exact-2",
			Match:    config.Match{Type: config.MatchTypeExact, Path: "/b"},
			Redirect: config.Redirect{To: "https://example.com/b", Status: 301, PreserveQuery: &preserveQuery},
		},
		{
			ID:       "prefix-1",
			Match:    config.Match{Type: config.MatchTypePrefix, Path: "/api/"},
			Redirect: config.Redirect{To: "https://api.example.com/", Status: 301, PreserveQuery: &preserveQuery},
		},
		{
			ID:       "regex-1",
			Match:    config.Match{Type: config.MatchTypeRegex, Pattern: `^/product/\d+$`},
			Redirect: config.Redirect{To: "https://shop.example.com/", Status: 301, PreserveQuery: &preserveQuery},
		},
	}

	// Validate
	cfg := &config.Config{Rules: rules}
	_ = cfg.Validate()

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

func TestRouter_ConcurrentReads(t *testing.T) {
	preserveQuery := true
	rules := []config.Rule{
		{ID: "exact-1", Match: config.Match{Type: config.MatchTypeExact, Path: "/a"}, Redirect: config.Redirect{To: "https://example.com/a", Status: 301, PreserveQuery: &preserveQuery}},
		{ID: "exact-2", Match: config.Match{Type: config.MatchTypeExact, Path: "/b"}, Redirect: config.Redirect{To: "https://example.com/b", Status: 301, PreserveQuery: &preserveQuery}},
		{ID: "prefix-1", Match: config.Match{Type: config.MatchTypePrefix, Path: "/api/"}, Redirect: config.Redirect{To: "https://api.example.com/", Status: 301, PreserveQuery: &preserveQuery}},
	}

	cfg := &config.Config{Rules: rules}
	_ = cfg.Validate()

	r, err := New(cfg.Rules)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	paths := []string{"/a", "/b", "/api/v1/users", "/nonexistent"}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				path := paths[(idx+j)%len(paths)]
				rule, _ := r.Match("", path)
				// Verify results are consistent
				switch path {
				case "/a":
					if rule == nil || rule.ID != "exact-1" {
						t.Errorf("Expected exact-1 for /a")
					}
				case "/b":
					if rule == nil || rule.ID != "exact-2" {
						t.Errorf("Expected exact-2 for /b")
					}
				case "/api/v1/users":
					if rule == nil || rule.ID != "prefix-1" {
						t.Errorf("Expected prefix-1 for /api/v1/users")
					}
				case "/nonexistent":
					if rule != nil {
						t.Errorf("Expected nil for /nonexistent")
					}
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestRouter_ConcurrentGetStats(t *testing.T) {
	rules := []config.Rule{
		{ID: "exact-1", Match: config.Match{Type: config.MatchTypeExact, Path: "/a"}},
		{ID: "prefix-1", Match: config.Match{Type: config.MatchTypePrefix, Path: "/api/"}},
	}

	r, err := New(rules)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				stats := r.GetStats()
				if stats.TotalRules != 2 {
					t.Errorf("Expected 2 total rules, got %d", stats.TotalRules)
				}
			}
		}()
	}
	wg.Wait()
}

// Benchmarks

func TestRouter_IsAllowedHost_WithHosts(t *testing.T) {
	rules := []config.Rule{
		{
			ID:       "host-a",
			Match:    config.Match{Type: config.MatchTypeExact, Host: "a.example.com", Path: "/"},
			Redirect: config.Redirect{To: "https://a.example.com/", Status: 301},
		},
		{
			ID:       "host-b",
			Match:    config.Match{Type: config.MatchTypePrefix, Host: "b.example.com", Path: "/api/"},
			Redirect: config.Redirect{To: "https://b.example.com/api/", Status: 301},
		},
	}

	r, err := New(rules)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	if !r.IsAllowedHost("a.example.com") {
		t.Error("Expected a.example.com to be allowed")
	}
	if !r.IsAllowedHost("b.example.com") {
		t.Error("Expected b.example.com to be allowed")
	}
	if r.IsAllowedHost("unknown.example.com") {
		t.Error("Expected unknown.example.com to be rejected")
	}
}

func TestRouter_IsAllowedHost_WildcardRule(t *testing.T) {
	rules := []config.Rule{
		{
			ID:       "host-a",
			Match:    config.Match{Type: config.MatchTypeExact, Host: "a.example.com", Path: "/"},
			Redirect: config.Redirect{To: "https://a.example.com/", Status: 301},
		},
		{
			// Rule without host — matches any host
			ID:       "catch-all",
			Match:    config.Match{Type: config.MatchTypePrefix, Path: "/"},
			Redirect: config.Redirect{To: "https://default.example.com/", Status: 301},
		},
	}

	r, err := New(rules)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	// With a wildcard rule, all hosts should be allowed
	if !r.IsAllowedHost("a.example.com") {
		t.Error("Expected a.example.com to be allowed")
	}
	if !r.IsAllowedHost("anything.example.com") {
		t.Error("Expected any host to be allowed when wildcard rule exists")
	}
}

func TestRouter_IsAllowedHost_AllWildcard(t *testing.T) {
	rules := []config.Rule{
		{
			ID:       "no-host-1",
			Match:    config.Match{Type: config.MatchTypeExact, Path: "/foo"},
			Redirect: config.Redirect{To: "https://example.com/foo", Status: 301},
		},
		{
			ID:       "no-host-2",
			Match:    config.Match{Type: config.MatchTypePrefix, Path: "/bar/"},
			Redirect: config.Redirect{To: "https://example.com/bar/", Status: 301},
		},
	}

	r, err := New(rules)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	// All rules lack hosts, so allowAnyHost should be true
	if !r.IsAllowedHost("literally-anything.com") {
		t.Error("Expected any host to be allowed when no rules specify a host")
	}
}

func TestRouter_IsAllowedHost_PortStripping(t *testing.T) {
	rules := []config.Rule{
		{
			ID:       "host-with-port",
			Match:    config.Match{Type: config.MatchTypeExact, Host: "example.com", Path: "/"},
			Redirect: config.Redirect{To: "https://example.com/", Status: 301},
		},
	}

	r, err := New(rules)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	tests := []struct {
		name    string
		host    string
		allowed bool
	}{
		{"exact host", "example.com", true},
		{"host with port", "example.com:8080", true},
		{"host with https port", "example.com:443", true},
		{"wrong host with port", "other.com:8080", false},
		{"ipv6 with port", "[::1]:8080", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.IsAllowedHost(tt.host); got != tt.allowed {
				t.Errorf("IsAllowedHost(%q) = %v, want %v", tt.host, got, tt.allowed)
			}
		})
	}
}

func TestRouter_IsAllowedHost_Empty(t *testing.T) {
	rules := []config.Rule{
		{
			ID:       "host-a",
			Match:    config.Match{Type: config.MatchTypeExact, Host: "example.com", Path: "/"},
			Redirect: config.Redirect{To: "https://example.com/", Status: 301},
		},
	}

	r, err := New(rules)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	if r.IsAllowedHost("") {
		t.Error("Expected empty host to be rejected")
	}
}

func TestRouter_AllowedHosts(t *testing.T) {
	rules := []config.Rule{
		{
			ID:       "host-a",
			Match:    config.Match{Type: config.MatchTypeExact, Host: "a.example.com", Path: "/"},
			Redirect: config.Redirect{To: "https://a.example.com/", Status: 301},
		},
		{
			ID:       "host-b",
			Match:    config.Match{Type: config.MatchTypePrefix, Host: "b.example.com", Path: "/"},
			Redirect: config.Redirect{To: "https://b.example.com/", Status: 301},
		},
	}

	r, err := New(rules)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	hosts := r.AllowedHosts()
	if len(hosts) != 2 {
		t.Fatalf("Expected 2 allowed hosts, got %d", len(hosts))
	}

	hostSet := make(map[string]bool)
	for _, h := range hosts {
		hostSet[h] = true
	}
	if !hostSet["a.example.com"] {
		t.Error("Expected a.example.com in allowed hosts")
	}
	if !hostSet["b.example.com"] {
		t.Error("Expected b.example.com in allowed hosts")
	}
}

func TestRouter_Stats_HostInfo(t *testing.T) {
	rules := []config.Rule{
		{
			ID:       "host-a",
			Match:    config.Match{Type: config.MatchTypeExact, Host: "a.example.com", Path: "/"},
			Redirect: config.Redirect{To: "https://a.example.com/", Status: 301},
		},
		{
			ID:       "no-host",
			Match:    config.Match{Type: config.MatchTypeExact, Path: "/foo"},
			Redirect: config.Redirect{To: "https://example.com/foo", Status: 301},
		},
	}

	r, err := New(rules)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	stats := r.GetStats()
	if stats.AllowedHostsCount != 1 {
		t.Errorf("Expected AllowedHostsCount=1, got %d", stats.AllowedHostsCount)
	}
	if !stats.AllowAnyHost {
		t.Error("Expected AllowAnyHost=true because one rule has no host")
	}
}

func BenchmarkRouter_IsAllowedHost(b *testing.B) {
	rules := make([]config.Rule, 100)
	for i := range rules {
		rules[i] = config.Rule{
			ID:       "rule-" + string(rune('a'+i)),
			Match:    config.Match{Type: config.MatchTypeExact, Host: "host-" + string(rune('a'+i)) + ".example.com", Path: "/"},
			Redirect: config.Redirect{To: "https://example.com/", Status: 301},
		}
	}

	r, _ := New(rules)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.IsAllowedHost("host-a.example.com")
	}
}

func BenchmarkRouter_ExactMatch(b *testing.B) {
	rules := make([]config.Rule, 1000)
	for i := 0; i < 1000; i++ {
		preserveQuery := true
		rules[i] = config.Rule{
			ID:       string(rune(i)),
			Match:    config.Match{Type: config.MatchTypeExact, Path: "/path/" + string(rune(i))},
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
			ID:       string(rune(i)),
			Match:    config.Match{Type: config.MatchTypePrefix, Path: "/prefix/" + string(rune(i)) + "/"},
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
	_ = cfg.Validate()

	r, _ := New(cfg.Rules)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Match("", "/product/12345")
	}
}

func BenchmarkRouter_GlobMatch(b *testing.B) {
	preserveQuery := true
	rules := []config.Rule{
		{
			ID:       "glob-1",
			Match:    config.Match{Type: config.MatchTypeGlob, Pattern: `/docs/*/guide`},
			Redirect: config.Redirect{To: "https://docs.example.com/", Status: 301, PreserveQuery: &preserveQuery},
		},
		{
			ID:       "glob-2",
			Match:    config.Match{Type: config.MatchTypeGlob, Pattern: `/api/**`},
			Redirect: config.Redirect{To: "https://api.example.com/", Status: 301, PreserveQuery: &preserveQuery},
		},
	}

	cfg := &config.Config{Rules: rules}
	_ = cfg.Validate()

	r, _ := New(cfg.Rules)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Match("", "/docs/v2/guide")
	}
}

func BenchmarkRouter_GlobDeepPath(b *testing.B) {
	preserveQuery := true
	rules := []config.Rule{
		{
			ID:       "glob-deep",
			Match:    config.Match{Type: config.MatchTypeGlob, Pattern: `/legacy/**`},
			Redirect: config.Redirect{To: "https://new.example.com/", Status: 301, PreserveQuery: &preserveQuery},
		},
	}

	cfg := &config.Config{Rules: rules}
	_ = cfg.Validate()

	r, _ := New(cfg.Rules)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Match("", "/legacy/a/b/c/d/e/f/g")
	}
}

func BenchmarkRouter_MixedRules(b *testing.B) {
	preserveQuery := true
	rules := []config.Rule{
		// Exact matches
		{ID: "exact-1", Match: config.Match{Type: config.MatchTypeExact, Path: "/home"}, Redirect: config.Redirect{To: "https://example.com/", Status: 301, PreserveQuery: &preserveQuery}},
		{ID: "exact-2", Match: config.Match{Type: config.MatchTypeExact, Path: "/about"}, Redirect: config.Redirect{To: "https://example.com/about", Status: 301, PreserveQuery: &preserveQuery}},
		// Prefix matches
		{ID: "prefix-1", Match: config.Match{Type: config.MatchTypePrefix, Path: "/blog/"}, Redirect: config.Redirect{To: "https://blog.example.com/", Status: 301, PreserveQuery: &preserveQuery}},
		{ID: "prefix-2", Match: config.Match{Type: config.MatchTypePrefix, Path: "/api/v1/"}, Redirect: config.Redirect{To: "https://api.example.com/v1/", Status: 301, PreserveQuery: &preserveQuery}},
		// Regex matches
		{ID: "regex-1", Match: config.Match{Type: config.MatchTypeRegex, Pattern: `^/product/(\d+)$`}, Redirect: config.Redirect{To: "https://shop.example.com/item/$1", Status: 301, PreserveQuery: &preserveQuery}},
		// Glob matches
		{ID: "glob-1", Match: config.Match{Type: config.MatchTypeGlob, Pattern: `/docs/**`}, Redirect: config.Redirect{To: "https://docs.example.com/", Status: 301, PreserveQuery: &preserveQuery}},
	}

	cfg := &config.Config{Rules: rules}
	_ = cfg.Validate()

	r, _ := New(cfg.Rules)

	paths := []string{
		"/home",                    // exact
		"/blog/post-123",           // prefix
		"/product/99999",           // regex
		"/docs/v1/getting-started", // glob
		"/unknown/path",            // no match
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Match("", paths[i%len(paths)])
	}
}

func BenchmarkRouter_LargeRuleSet(b *testing.B) {
	preserveQuery := true
	rules := make([]config.Rule, 10000)

	// Create a large rule set with mixed types
	for i := 0; i < 10000; i++ {
		switch i % 4 {
		case 0:
			rules[i] = config.Rule{
				ID:       "exact-" + string(rune(i)),
				Match:    config.Match{Type: config.MatchTypeExact, Path: "/exact/" + string(rune(i))},
				Redirect: config.Redirect{To: "https://example.com/", Status: 301, PreserveQuery: &preserveQuery},
			}
		case 1:
			rules[i] = config.Rule{
				ID:       "prefix-" + string(rune(i)),
				Match:    config.Match{Type: config.MatchTypePrefix, Path: "/prefix/" + string(rune(i)) + "/"},
				Redirect: config.Redirect{To: "https://example.com/", Status: 301, PreserveQuery: &preserveQuery},
			}
		case 2:
			rules[i] = config.Rule{
				ID:       "regex-" + string(rune(i)),
				Match:    config.Match{Type: config.MatchTypeRegex, Pattern: `^/regex/` + string(rune(i)) + `/(\d+)$`},
				Redirect: config.Redirect{To: "https://example.com/$1", Status: 301, PreserveQuery: &preserveQuery},
			}
		case 3:
			rules[i] = config.Rule{
				ID:       "glob-" + string(rune(i)),
				Match:    config.Match{Type: config.MatchTypeGlob, Pattern: "/glob/" + string(rune(i)) + "/**"},
				Redirect: config.Redirect{To: "https://example.com/", Status: 301, PreserveQuery: &preserveQuery},
			}
		}
	}

	cfg := &config.Config{Rules: rules}
	_ = cfg.Validate()

	r, _ := New(cfg.Rules)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Match("", "/exact/a")
	}
}

func BenchmarkRouter_NoMatch(b *testing.B) {
	preserveQuery := true
	rules := []config.Rule{
		{ID: "exact-1", Match: config.Match{Type: config.MatchTypeExact, Path: "/foo"}, Redirect: config.Redirect{To: "https://example.com/", Status: 301, PreserveQuery: &preserveQuery}},
		{ID: "prefix-1", Match: config.Match{Type: config.MatchTypePrefix, Path: "/api/"}, Redirect: config.Redirect{To: "https://example.com/", Status: 301, PreserveQuery: &preserveQuery}},
		{ID: "regex-1", Match: config.Match{Type: config.MatchTypeRegex, Pattern: `^/product/\d+$`}, Redirect: config.Redirect{To: "https://example.com/", Status: 301, PreserveQuery: &preserveQuery}},
	}

	cfg := &config.Config{Rules: rules}
	_ = cfg.Validate()

	r, _ := New(cfg.Rules)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Match("", "/completely/different/path")
	}
}

func BenchmarkRouter_HostMatch(b *testing.B) {
	preserveQuery := true
	rules := []config.Rule{
		{
			ID:       "host-1",
			Match:    config.Match{Type: config.MatchTypePrefix, Host: "old.example.com", Path: "/"},
			Redirect: config.Redirect{To: "https://new.example.com/", Status: 301, PreserveQuery: &preserveQuery},
		},
		{
			ID:       "host-2",
			Match:    config.Match{Type: config.MatchTypePrefix, Host: "api.example.com", Path: "/v1/"},
			Redirect: config.Redirect{To: "https://api.example.com/v2/", Status: 301, PreserveQuery: &preserveQuery},
		},
	}

	r, _ := New(rules)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Match("old.example.com", "/some/path")
	}
}

// BenchmarkRouter_NewRouter measures router initialization time
func BenchmarkRouter_NewRouter(b *testing.B) {
	preserveQuery := true
	rules := make([]config.Rule, 1000)
	for i := 0; i < 1000; i++ {
		rules[i] = config.Rule{
			ID:       "rule-" + string(rune(i)),
			Match:    config.Match{Type: config.MatchTypeExact, Path: "/path/" + string(rune(i))},
			Redirect: config.Redirect{To: "https://example.com/", Status: 301, PreserveQuery: &preserveQuery},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = New(rules)
	}
}

// BenchmarkRouter_NewRouterWithRegex measures router init with regex compilation
func BenchmarkRouter_NewRouterWithRegex(b *testing.B) {
	preserveQuery := true
	rules := make([]config.Rule, 100)
	for i := 0; i < 100; i++ {
		rules[i] = config.Rule{
			ID:       "regex-" + string(rune(i)),
			Match:    config.Match{Type: config.MatchTypeRegex, Pattern: `^/path/` + string(rune(i)) + `/(\d+)$`},
			Redirect: config.Redirect{To: "https://example.com/$1", Status: 301, PreserveQuery: &preserveQuery},
		}
	}

	cfg := &config.Config{Rules: rules}
	_ = cfg.Validate()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = New(cfg.Rules)
	}
}

// Memory allocation benchmarks

func BenchmarkRouter_ExactMatch_Allocs(b *testing.B) {
	preserveQuery := true
	rules := []config.Rule{
		{ID: "exact-1", Match: config.Match{Type: config.MatchTypeExact, Path: "/foo"}, Redirect: config.Redirect{To: "https://example.com/", Status: 301, PreserveQuery: &preserveQuery}},
	}

	r, _ := New(rules)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Match("", "/foo")
	}
}

func BenchmarkRouter_RegexMatch_Allocs(b *testing.B) {
	preserveQuery := true
	rules := []config.Rule{
		{ID: "regex-1", Match: config.Match{Type: config.MatchTypeRegex, Pattern: `^/product/(\d+)$`}, Redirect: config.Redirect{To: "https://example.com/$1", Status: 301, PreserveQuery: &preserveQuery}},
	}

	cfg := &config.Config{Rules: rules}
	_ = cfg.Validate()

	r, _ := New(cfg.Rules)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Match("", "/product/12345")
	}
}
