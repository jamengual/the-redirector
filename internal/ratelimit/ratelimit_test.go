package ratelimit

import (
	"net"
	"testing"
	"time"

	"github.com/valyala/fasthttp"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled {
		t.Error("Default should be disabled")
	}
	if cfg.GlobalRPS != 10000 {
		t.Errorf("Expected global RPS 10000, got %f", cfg.GlobalRPS)
	}
	if cfg.PerIPRPS != 100 {
		t.Errorf("Expected per-IP RPS 100, got %f", cfg.PerIPRPS)
	}
}

func TestLimiterDisabled(t *testing.T) {
	cfg := &Config{Enabled: false}
	l := New(cfg)
	defer l.Close()

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/test")

	// Should always allow when disabled
	for i := 0; i < 1000; i++ {
		if !l.Allow(ctx) {
			t.Fatal("Should allow all requests when disabled")
		}
	}
}

func TestGlobalRateLimit(t *testing.T) {
	cfg := &Config{
		Enabled:     true,
		GlobalRPS:   10,
		GlobalBurst: 10,
		PerIPRPS:    0, // Disable per-IP
	}
	l := New(cfg)
	defer l.Close()

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/test")

	// First burst should be allowed
	allowed := 0
	for i := 0; i < 20; i++ {
		if l.Allow(ctx) {
			allowed++
		}
	}

	// Should have allowed about burst size
	if allowed < 5 || allowed > 15 {
		t.Errorf("Expected around 10 allowed, got %d", allowed)
	}
}

func TestPerIPRateLimit(t *testing.T) {
	cfg := &Config{
		Enabled:     true,
		GlobalRPS:   10000, // High global limit
		GlobalBurst: 20000,
		PerIPRPS:    5,
		PerIPBurst:  5,
	}
	l := New(cfg)
	defer l.Close()

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/test")
	// Simulate remote IP
	ctx.SetRemoteAddr(&net.TCPAddr{IP: net.ParseIP("192.168.1.1"), Port: 12345})

	// First burst should be allowed
	allowed := 0
	for i := 0; i < 20; i++ {
		if l.Allow(ctx) {
			allowed++
		}
	}

	if allowed < 3 || allowed > 7 {
		t.Errorf("Expected around 5 allowed for IP, got %d", allowed)
	}
}

func TestExemptIPs(t *testing.T) {
	cfg := &Config{
		Enabled:     true,
		GlobalRPS:   1,
		GlobalBurst: 1,
		PerIPRPS:    1,
		PerIPBurst:  1,
		ExemptIPs:   []string{"10.0.0.0/8", "192.168.1.100"},
	}
	l := New(cfg)
	defer l.Close()

	// Exempt IP in CIDR range
	ctx1 := &fasthttp.RequestCtx{}
	ctx1.Request.SetRequestURI("/test")
	ctx1.SetRemoteAddr(&net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 12345})

	// Should always be allowed
	for i := 0; i < 100; i++ {
		if !l.Allow(ctx1) {
			t.Fatal("Exempt IP should always be allowed")
		}
	}

	// Exempt single IP
	ctx2 := &fasthttp.RequestCtx{}
	ctx2.Request.SetRequestURI("/test")
	ctx2.SetRemoteAddr(&net.TCPAddr{IP: net.ParseIP("192.168.1.100"), Port: 12345})

	for i := 0; i < 100; i++ {
		if !l.Allow(ctx2) {
			t.Fatal("Exempt single IP should always be allowed")
		}
	}
}

func TestPathLimits(t *testing.T) {
	cfg := &Config{
		Enabled:     true,
		GlobalRPS:   10000,
		GlobalBurst: 20000,
		PerIPRPS:    10000,
		PerIPBurst:  20000,
		PathLimits: []PathLimit{
			{Path: "/api/expensive", RPS: 2, Burst: 2},
			{Path: "/api/*", RPS: 5, Burst: 5},
		},
	}
	l := New(cfg)
	defer l.Close()

	// Test exact path limit
	ctx1 := &fasthttp.RequestCtx{}
	ctx1.Request.SetRequestURI("/api/expensive")

	allowed := 0
	for i := 0; i < 10; i++ {
		if l.Allow(ctx1) {
			allowed++
		}
	}
	if allowed > 4 {
		t.Errorf("Path limit should restrict to ~2, got %d", allowed)
	}
}

func TestTrustProxy(t *testing.T) {
	cfg := &Config{
		Enabled:    true,
		GlobalRPS:  10000,
		PerIPRPS:   5,
		PerIPBurst: 5,
		TrustProxy: true,
	}
	l := New(cfg)
	defer l.Close()

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/test")
	ctx.Request.Header.Set("X-Forwarded-For", "203.0.113.1, 10.0.0.1")
	ctx.SetRemoteAddr(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345})

	// Get the client IP (should be from X-Forwarded-For)
	ip := l.getClientIP(ctx)
	if ip.String() != "203.0.113.1" {
		t.Errorf("Expected IP from X-Forwarded-For, got %s", ip.String())
	}
}

func TestMiddleware(t *testing.T) {
	cfg := &Config{
		Enabled:     true,
		GlobalRPS:   5,
		GlobalBurst: 5,
	}
	l := New(cfg)
	defer l.Close()

	handlerCalled := 0
	handler := func(ctx *fasthttp.RequestCtx) {
		handlerCalled++
	}

	wrapped := l.Middleware(handler)

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetRequestURI("/test")

	// Call multiple times
	for i := 0; i < 20; i++ {
		wrapped(ctx)
	}

	// Handler should have been called burst times
	if handlerCalled < 3 || handlerCalled > 7 {
		t.Errorf("Expected handler called ~5 times, got %d", handlerCalled)
	}
}

func TestCleanup(t *testing.T) {
	cfg := &Config{
		Enabled:         true,
		PerIPRPS:        100,
		PerIPBurst:      100,
		CleanupInterval: 50 * time.Millisecond,
		IPTTL:           100 * time.Millisecond,
	}
	l := New(cfg)
	defer l.Close()

	// Create some IP limiters
	for i := 0; i < 10; i++ {
		ctx := &fasthttp.RequestCtx{}
		ctx.SetRemoteAddr(&net.TCPAddr{IP: net.ParseIP("192.168.1." + string(rune('0'+i))), Port: 12345})
		l.Allow(ctx)
	}

	// Check we have limiters
	stats := l.Stats()
	if stats.ActiveIPLimiters != 10 {
		t.Errorf("Expected 10 IP limiters, got %d", stats.ActiveIPLimiters)
	}

	// Wait for cleanup
	time.Sleep(200 * time.Millisecond)

	// Check limiters were cleaned up
	stats = l.Stats()
	if stats.ActiveIPLimiters != 0 {
		t.Errorf("Expected 0 IP limiters after cleanup, got %d", stats.ActiveIPLimiters)
	}
}

func TestStats(t *testing.T) {
	cfg := &Config{
		Enabled:    true,
		PerIPRPS:   100,
		PerIPBurst: 100,
		PathLimits: []PathLimit{
			{Path: "/a", RPS: 10, Burst: 10},
			{Path: "/b", RPS: 10, Burst: 10},
		},
	}
	l := New(cfg)
	defer l.Close()

	stats := l.Stats()
	if stats.PathLimiters != 2 {
		t.Errorf("Expected 2 path limiters, got %d", stats.PathLimiters)
	}
}
