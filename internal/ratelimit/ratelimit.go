// Package ratelimit provides rate limiting middleware for the redirector.
package ratelimit

import (
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/valyala/fasthttp"
	"golang.org/x/time/rate"
)

// Config configures rate limiting.
type Config struct {
	// Enabled turns rate limiting on/off
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Global rate limit (requests per second across all clients)
	GlobalRPS float64 `yaml:"global_rps" json:"global_rps"`
	GlobalBurst int   `yaml:"global_burst" json:"global_burst"`

	// Per-IP rate limit
	PerIPRPS   float64 `yaml:"per_ip_rps" json:"per_ip_rps"`
	PerIPBurst int     `yaml:"per_ip_burst" json:"per_ip_burst"`

	// Per-path rate limits (for specific paths)
	PathLimits []PathLimit `yaml:"path_limits" json:"path_limits"`

	// CleanupInterval for removing stale IP limiters
	CleanupInterval time.Duration `yaml:"cleanup_interval" json:"cleanup_interval"`

	// IPTTLfor removing inactive IP limiters
	IPTTL time.Duration `yaml:"ip_ttl" json:"ip_ttl"`

	// TrustProxy trusts X-Forwarded-For header for client IP
	TrustProxy bool `yaml:"trust_proxy" json:"trust_proxy"`

	// ExemptIPs are IPs that bypass rate limiting
	ExemptIPs []string `yaml:"exempt_ips" json:"exempt_ips"`
}

// PathLimit configures rate limiting for a specific path.
type PathLimit struct {
	Path  string  `yaml:"path" json:"path"`   // Exact path or prefix (with *)
	RPS   float64 `yaml:"rps" json:"rps"`
	Burst int     `yaml:"burst" json:"burst"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Enabled:         false,
		GlobalRPS:       10000,
		GlobalBurst:     20000,
		PerIPRPS:        100,
		PerIPBurst:      200,
		CleanupInterval: 5 * time.Minute,
		IPTTL:           10 * time.Minute,
		TrustProxy:      false,
	}
}

// ipLimiter tracks a rate limiter and last access time.
type ipLimiter struct {
	limiter    *rate.Limiter
	lastAccess time.Time
}

// Limiter provides rate limiting functionality.
type Limiter struct {
	cfg           *Config
	globalLimiter *rate.Limiter
	ipLimiters    map[string]*ipLimiter
	pathLimiters  map[string]*rate.Limiter
	exemptNets    []*net.IPNet
	mu            sync.RWMutex
	stopCleanup   chan struct{}
}

// New creates a new rate limiter.
func New(cfg *Config) *Limiter {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	l := &Limiter{
		cfg:          cfg,
		ipLimiters:   make(map[string]*ipLimiter),
		pathLimiters: make(map[string]*rate.Limiter),
		stopCleanup:  make(chan struct{}),
	}

	// Global limiter
	if cfg.GlobalRPS > 0 {
		l.globalLimiter = rate.NewLimiter(rate.Limit(cfg.GlobalRPS), cfg.GlobalBurst)
	}

	// Path-specific limiters
	for _, pl := range cfg.PathLimits {
		l.pathLimiters[pl.Path] = rate.NewLimiter(rate.Limit(pl.RPS), pl.Burst)
	}

	// Parse exempt IPs
	for _, ipStr := range cfg.ExemptIPs {
		if _, ipNet, err := net.ParseCIDR(ipStr); err == nil {
			l.exemptNets = append(l.exemptNets, ipNet)
		} else if ip := net.ParseIP(ipStr); ip != nil {
			var mask net.IPMask
			if ip.To4() != nil {
				mask = net.CIDRMask(32, 32)
			} else {
				mask = net.CIDRMask(128, 128)
			}
			l.exemptNets = append(l.exemptNets, &net.IPNet{IP: ip, Mask: mask})
		}
	}

	// Start cleanup goroutine
	if cfg.CleanupInterval > 0 {
		go l.cleanupLoop()
	}

	return l
}

// cleanupLoop periodically removes stale IP limiters.
func (l *Limiter) cleanupLoop() {
	ticker := time.NewTicker(l.cfg.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-l.stopCleanup:
			return
		case <-ticker.C:
			l.cleanup()
		}
	}
}

// cleanup removes IP limiters that haven't been used recently.
func (l *Limiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	stale := 0
	for ip, limiter := range l.ipLimiters {
		if now.Sub(limiter.lastAccess) > l.cfg.IPTTL {
			delete(l.ipLimiters, ip)
			stale++
		}
	}

	if stale > 0 {
		log.Debug().Int("removed", stale).Int("remaining", len(l.ipLimiters)).Msg("Cleaned up stale IP limiters")
	}
}

// Allow checks if a request should be allowed.
func (l *Limiter) Allow(ctx *fasthttp.RequestCtx) bool {
	if !l.cfg.Enabled {
		return true
	}

	clientIP := l.getClientIP(ctx)

	// Check exempt IPs
	if l.isExempt(clientIP) {
		return true
	}

	// Check global limit
	if l.globalLimiter != nil && !l.globalLimiter.Allow() {
		log.Debug().Msg("Global rate limit exceeded")
		return false
	}

	// Check path-specific limit
	path := string(ctx.Path())
	if pathLimiter := l.getPathLimiter(path); pathLimiter != nil {
		if !pathLimiter.Allow() {
			log.Debug().Str("path", path).Msg("Path rate limit exceeded")
			return false
		}
	}

	// Check per-IP limit
	if l.cfg.PerIPRPS > 0 {
		ipLimiter := l.getIPLimiter(clientIP.String())
		if !ipLimiter.Allow() {
			log.Debug().Str("ip", clientIP.String()).Msg("IP rate limit exceeded")
			return false
		}
	}

	return true
}

// getClientIP extracts the client IP from the request.
func (l *Limiter) getClientIP(ctx *fasthttp.RequestCtx) net.IP {
	if l.cfg.TrustProxy {
		// Check X-Forwarded-For header
		if xff := ctx.Request.Header.Peek("X-Forwarded-For"); len(xff) > 0 {
			// Take the first IP in the list
			for i, b := range xff {
				if b == ',' {
					xff = xff[:i]
					break
				}
			}
			if ip := net.ParseIP(string(xff)); ip != nil {
				return ip
			}
		}
		// Check X-Real-IP header
		if xri := ctx.Request.Header.Peek("X-Real-IP"); len(xri) > 0 {
			if ip := net.ParseIP(string(xri)); ip != nil {
				return ip
			}
		}
	}

	return ctx.RemoteIP()
}

// isExempt checks if an IP is in the exempt list.
func (l *Limiter) isExempt(ip net.IP) bool {
	for _, ipNet := range l.exemptNets {
		if ipNet.Contains(ip) {
			return true
		}
	}
	return false
}

// getPathLimiter returns a limiter for a specific path.
func (l *Limiter) getPathLimiter(path string) *rate.Limiter {
	// Exact match first
	if limiter, ok := l.pathLimiters[path]; ok {
		return limiter
	}

	// Check prefix matches (paths ending with *)
	for pattern, limiter := range l.pathLimiters {
		if len(pattern) > 0 && pattern[len(pattern)-1] == '*' {
			prefix := pattern[:len(pattern)-1]
			if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
				return limiter
			}
		}
	}

	return nil
}

// getIPLimiter gets or creates a rate limiter for an IP.
func (l *Limiter) getIPLimiter(ip string) *rate.Limiter {
	l.mu.RLock()
	if limiter, ok := l.ipLimiters[ip]; ok {
		limiter.lastAccess = time.Now()
		l.mu.RUnlock()
		return limiter.limiter
	}
	l.mu.RUnlock()

	// Create new limiter
	l.mu.Lock()
	defer l.mu.Unlock()

	// Double-check after acquiring write lock
	if limiter, ok := l.ipLimiters[ip]; ok {
		limiter.lastAccess = time.Now()
		return limiter.limiter
	}

	newLimiter := rate.NewLimiter(rate.Limit(l.cfg.PerIPRPS), l.cfg.PerIPBurst)
	l.ipLimiters[ip] = &ipLimiter{
		limiter:    newLimiter,
		lastAccess: time.Now(),
	}

	return newLimiter
}

// Middleware wraps a handler with rate limiting.
func (l *Limiter) Middleware(handler fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		if !l.Allow(ctx) {
			l.sendTooManyRequests(ctx)
			return
		}
		handler(ctx)
	}
}

// sendTooManyRequests sends a 429 response.
func (l *Limiter) sendTooManyRequests(ctx *fasthttp.RequestCtx) {
	ctx.SetStatusCode(fasthttp.StatusTooManyRequests)
	ctx.SetContentType("application/json")
	ctx.Response.Header.Set("Retry-After", "1")
	ctx.Response.Header.Set("X-RateLimit-Limit", "rate limited")
	ctx.SetBodyString(`{"error":"too_many_requests","message":"Rate limit exceeded. Please try again later."}`)
}

// Close stops the cleanup goroutine.
func (l *Limiter) Close() {
	close(l.stopCleanup)
}

// Stats returns current rate limiter statistics.
func (l *Limiter) Stats() Stats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return Stats{
		ActiveIPLimiters: len(l.ipLimiters),
		PathLimiters:     len(l.pathLimiters),
	}
}

// Stats contains rate limiter statistics.
type Stats struct {
	ActiveIPLimiters int `json:"active_ip_limiters"`
	PathLimiters     int `json:"path_limiters"`
}
