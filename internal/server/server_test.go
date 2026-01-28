package server

import (
	"encoding/json"
	"testing"

	"github.com/valyala/fasthttp"

	"github.com/jamengual/the-redirector/internal/config"
	"github.com/jamengual/the-redirector/internal/router"
	"github.com/jamengual/the-redirector/internal/stats"
	"github.com/jamengual/the-redirector/internal/versioning"
)

// newTestServer creates a minimal server suitable for handler-level tests.
func newTestServer(t *testing.T, rules []config.Rule) *Server {
	t.Helper()

	r, err := router.New(rules)
	if err != nil {
		t.Fatalf("creating router: %v", err)
	}

	cfg := &config.Config{
		Version: "test",
		Rules:   rules,
	}

	return &Server{
		cfg:          cfg,
		router:       r,
		stats:        stats.NewCollector(stats.Config{Enabled: true, BufferSize: 100, SamplingRate: 1.0}),
		versionStore: versioning.NewStore(10),
		auditLog:     versioning.NewAuditLog(100),
	}
}

// executeHandler calls a fasthttp handler and returns the response context.
func executeHandler(handler func(*fasthttp.RequestCtx), method, path string) *fasthttp.RequestCtx {
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI(path)
	ctx.Request.Header.SetMethod(method)
	handler(ctx)
	return ctx
}

func TestHandleRedirect_ExactMatch(t *testing.T) {
	s := newTestServer(t, []config.Rule{
		{
			ID:       "exact-1",
			Match:    config.Match{Type: config.MatchTypeExact, Path: "/foo"},
			Redirect: config.Redirect{To: "https://example.com/foo", Status: 301},
		},
	})

	ctx := executeHandler(s.handleRedirect, "GET", "/foo")

	if ctx.Response.StatusCode() != 301 {
		t.Errorf("Expected 301, got %d", ctx.Response.StatusCode())
	}
	location := string(ctx.Response.Header.Peek("Location"))
	if location != "https://example.com/foo" {
		t.Errorf("Expected Location https://example.com/foo, got %q", location)
	}
}

func TestHandleRedirect_PrefixMatch(t *testing.T) {
	preservePath := true
	s := newTestServer(t, []config.Rule{
		{
			ID:       "prefix-1",
			Match:    config.Match{Type: config.MatchTypePrefix, Path: "/docs/"},
			Redirect: config.Redirect{To: "https://docs.example.com/", Status: 302, PreservePath: preservePath},
		},
	})

	ctx := executeHandler(s.handleRedirect, "GET", "/docs/getting-started")

	if ctx.Response.StatusCode() != 302 {
		t.Errorf("Expected 302, got %d", ctx.Response.StatusCode())
	}
	location := string(ctx.Response.Header.Peek("Location"))
	if location != "https://docs.example.com/getting-started" {
		t.Errorf("Expected Location https://docs.example.com/getting-started, got %q", location)
	}
}

func TestHandleRedirect_NoMatch(t *testing.T) {
	s := newTestServer(t, []config.Rule{
		{
			ID:       "exact-1",
			Match:    config.Match{Type: config.MatchTypeExact, Path: "/foo"},
			Redirect: config.Redirect{To: "https://example.com/foo", Status: 301},
		},
	})

	ctx := executeHandler(s.handleRedirect, "GET", "/nonexistent")

	if ctx.Response.StatusCode() != 404 {
		t.Errorf("Expected 404, got %d", ctx.Response.StatusCode())
	}
}

func TestHandleRedirect_CustomHeaders(t *testing.T) {
	s := newTestServer(t, []config.Rule{
		{
			ID:    "headers-1",
			Match: config.Match{Type: config.MatchTypeExact, Path: "/with-headers"},
			Redirect: config.Redirect{
				To:      "https://example.com/dest",
				Status:  301,
				Headers: map[string]string{"X-Custom": "test-value"},
			},
		},
	})

	ctx := executeHandler(s.handleRedirect, "GET", "/with-headers")

	if ctx.Response.StatusCode() != 301 {
		t.Errorf("Expected 301, got %d", ctx.Response.StatusCode())
	}
	custom := string(ctx.Response.Header.Peek("X-Custom"))
	if custom != "test-value" {
		t.Errorf("Expected X-Custom header 'test-value', got %q", custom)
	}
	redirectedBy := string(ctx.Response.Header.Peek("X-Redirected-By"))
	if redirectedBy != "the-redirector" {
		t.Errorf("Expected X-Redirected-By header 'the-redirector', got %q", redirectedBy)
	}
}

func TestHandleRedirect_NonRedirectStatus(t *testing.T) {
	s := newTestServer(t, []config.Rule{
		{
			ID:    "gone-1",
			Match: config.Match{Type: config.MatchTypeExact, Path: "/old-page"},
			Redirect: config.Redirect{
				Status: 410,
				Body:   "This page has been permanently removed",
			},
		},
	})

	ctx := executeHandler(s.handleRedirect, "GET", "/old-page")

	if ctx.Response.StatusCode() != 410 {
		t.Errorf("Expected 410, got %d", ctx.Response.StatusCode())
	}
	body := string(ctx.Response.Body())
	if body != "This page has been permanently removed" {
		t.Errorf("Expected body 'This page has been permanently removed', got %q", body)
	}
	matchedBy := string(ctx.Response.Header.Peek("X-Matched-By"))
	if matchedBy != "the-redirector" {
		t.Errorf("Expected X-Matched-By header 'the-redirector', got %q", matchedBy)
	}
}

func TestHandleHealth(t *testing.T) {
	s := newTestServer(t, nil)

	ctx := executeHandler(s.handleHealth, "GET", "/health")

	if ctx.Response.StatusCode() != 200 {
		t.Errorf("Expected 200, got %d", ctx.Response.StatusCode())
	}

	var resp map[string]string
	if err := json.Unmarshal(ctx.Response.Body(), &resp); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}
	if resp["status"] != "healthy" {
		t.Errorf("Expected status 'healthy', got %q", resp["status"])
	}
}

func TestHandleReady(t *testing.T) {
	s := newTestServer(t, nil)

	ctx := executeHandler(s.handleReady, "GET", "/ready")

	if ctx.Response.StatusCode() != 200 {
		t.Errorf("Expected 200, got %d", ctx.Response.StatusCode())
	}

	var resp map[string]string
	if err := json.Unmarshal(ctx.Response.Body(), &resp); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}
	if resp["status"] != "ready" {
		t.Errorf("Expected status 'ready', got %q", resp["status"])
	}
}

func TestHandleConfig(t *testing.T) {
	s := newTestServer(t, []config.Rule{
		{ID: "r1", Match: config.Match{Type: config.MatchTypeExact, Path: "/a"}},
		{ID: "r2", Match: config.Match{Type: config.MatchTypeExact, Path: "/b"}},
	})

	ctx := executeHandler(s.handleConfig, "GET", "/api/v1/config")

	if ctx.Response.StatusCode() != 200 {
		t.Errorf("Expected 200, got %d", ctx.Response.StatusCode())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(ctx.Response.Body(), &resp); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}
	if resp["rules_count"] != float64(2) {
		t.Errorf("Expected rules_count 2, got %v", resp["rules_count"])
	}
}

func TestHandleStats_Enabled(t *testing.T) {
	s := newTestServer(t, nil)

	ctx := executeHandler(s.handleStats, "GET", "/stats")

	if ctx.Response.StatusCode() != 200 {
		t.Errorf("Expected 200, got %d", ctx.Response.StatusCode())
	}
}

func TestHandleStats_Disabled(t *testing.T) {
	s := newTestServer(t, nil)
	s.stats = nil // Simulate stats disabled

	ctx := executeHandler(s.handleStats, "GET", "/stats")

	if ctx.Response.StatusCode() != 503 {
		t.Errorf("Expected 503, got %d", ctx.Response.StatusCode())
	}
}

func TestHandleManagement_NotFound(t *testing.T) {
	s := newTestServer(t, nil)

	ctx := executeHandler(s.handleManagement, "GET", "/nonexistent-endpoint")

	if ctx.Response.StatusCode() != 404 {
		t.Errorf("Expected 404, got %d", ctx.Response.StatusCode())
	}
}

func TestHandleManagement_PublicEndpoints(t *testing.T) {
	s := newTestServer(t, nil)

	tests := []struct {
		name string
		path string
		want int
	}{
		{"health", "/health", 200},
		{"ready", "/ready", 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := executeHandler(s.handleManagement, "GET", tt.path)
			if ctx.Response.StatusCode() != tt.want {
				t.Errorf("Expected %d, got %d", tt.want, ctx.Response.StatusCode())
			}
		})
	}
}

func TestHandleReload_RequiresPost(t *testing.T) {
	s := newTestServer(t, nil)

	ctx := executeHandler(s.handleReload, "GET", "/api/v1/reload")

	if ctx.Response.StatusCode() != 405 {
		t.Errorf("Expected 405 for GET request, got %d", ctx.Response.StatusCode())
	}
}

func TestHandleReload_NoConfigPath(t *testing.T) {
	s := newTestServer(t, nil)
	s.configPath = ""

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/api/v1/reload")
	ctx.Request.Header.SetMethod("POST")
	s.handleReload(ctx)

	if ctx.Response.StatusCode() != 503 {
		t.Errorf("Expected 503, got %d", ctx.Response.StatusCode())
	}
}

func TestHandleVersions(t *testing.T) {
	s := newTestServer(t, []config.Rule{
		{ID: "r1", Match: config.Match{Type: config.MatchTypeExact, Path: "/a"}},
	})
	// Add a version so there's something to return
	s.versionStore.Add(s.cfg, "test-config.yaml")

	ctx := executeHandler(s.handleVersions, "GET", "/api/v1/versions")

	if ctx.Response.StatusCode() != 200 {
		t.Errorf("Expected 200, got %d", ctx.Response.StatusCode())
	}

	var versions []map[string]interface{}
	if err := json.Unmarshal(ctx.Response.Body(), &versions); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}
	if len(versions) == 0 {
		t.Error("Expected at least one version")
	}
}

func TestHandleCurrentVersion(t *testing.T) {
	s := newTestServer(t, []config.Rule{
		{ID: "r1", Match: config.Match{Type: config.MatchTypeExact, Path: "/a"}},
	})
	s.versionStore.Add(s.cfg, "test-config.yaml")

	ctx := executeHandler(s.handleCurrentVersion, "GET", "/api/v1/versions/current")

	if ctx.Response.StatusCode() != 200 {
		t.Errorf("Expected 200, got %d", ctx.Response.StatusCode())
	}
}

func TestRequirePermission_AuthDisabled(t *testing.T) {
	s := newTestServer(t, nil)
	// authMiddleware is nil - all permissions should be granted

	ctx := &fasthttp.RequestCtx{}
	if !s.requirePermission(ctx, "read") {
		t.Error("Expected permission granted when auth is disabled")
	}
}
