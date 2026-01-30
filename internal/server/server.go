package server

import (
	"context"
	"encoding/json"
	"expvar"
	"fmt"
	"net/http/pprof"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/jamengual/the-redirector/internal/auth"
	"github.com/jamengual/the-redirector/internal/config"
	"github.com/jamengual/the-redirector/internal/metrics"
	"github.com/jamengual/the-redirector/internal/ratelimit"
	"github.com/jamengual/the-redirector/internal/router"
	"github.com/jamengual/the-redirector/internal/stats"
	"github.com/jamengual/the-redirector/internal/tracing"
	"github.com/jamengual/the-redirector/internal/versioning"
)

// Header and content-type constants used throughout the server.
const (
	headerRedirectedBy = "X-Redirected-By"
	headerMatchedBy    = "X-Matched-By"
	headerRuleID       = "X-Rule-ID"
	headerValueApp     = "the-redirector"
	contentTypeJSON    = "application/json"
)

// writeJSONError writes a JSON error response with the given status code and message.
func writeJSONError(ctx *fasthttp.RequestCtx, status int, message string) {
	ctx.SetStatusCode(status)
	ctx.SetContentType(contentTypeJSON)
	ctx.SetBodyString(fmt.Sprintf(`{"error":"%s"}`, message))
}

// Server handles HTTP requests and redirects.
type Server struct {
	cfg             *config.Config
	configPath      string
	router          *router.Router
	stats           *stats.Collector
	metrics         *metrics.Metrics
	authMiddleware  *auth.Middleware
	versionStore    *versioning.Store
	auditLog        *versioning.AuditLog
	tracingProvider *tracing.Provider
	rateLimiter     *ratelimit.Limiter

	httpServer       *fasthttp.Server
	managementServer *fasthttp.Server

	promHandler fasthttp.RequestHandler
	mu          sync.RWMutex
}

// New creates a new server instance.
// configPath is stored for reload operations.
func New(cfg *config.Config, configPath string) (*Server, error) {
	r, err := router.New(cfg.Rules)
	if err != nil {
		return nil, fmt.Errorf("creating router: %w", err)
	}

	// Create stats collector (enabled via config or environment)
	statsCfg := stats.DefaultConfig()
	if cfg.Stats != nil {
		statsCfg.Enabled = cfg.Stats.Enabled
		if cfg.Stats.BufferSize > 0 {
			statsCfg.BufferSize = cfg.Stats.BufferSize
		}
		if cfg.Stats.SamplingRate > 0 {
			statsCfg.SamplingRate = cfg.Stats.SamplingRate
		}
	}

	// Create Prometheus metrics
	registry := prometheus.NewRegistry()
	m := metrics.NewWithRuntimeMetrics(registry)

	// Set initial config metrics
	m.ConfigRulesCount.Set(float64(len(cfg.Rules)))
	m.ConfigLastReloadTime.SetToCurrentTime()

	// Create Prometheus HTTP handler adapted for fasthttp
	promHandler := fasthttpadaptor.NewFastHTTPHandler(
		promhttp.HandlerFor(registry, promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		}),
	)

	// Create auth middleware if configured
	var authMiddleware *auth.Middleware
	if cfg.Auth != nil {
		authCfg := &auth.Config{
			Enabled:  cfg.Auth.Enabled,
			AllowIPs: cfg.Auth.AllowIPs,
		}

		// Convert API key configs
		for _, key := range cfg.Auth.APIKeys {
			permissions := make([]auth.Permission, len(key.Permissions))
			for i, p := range key.Permissions {
				permissions[i] = auth.Permission(p)
			}
			authCfg.APIKeys = append(authCfg.APIKeys, auth.APIKeyEntry{
				Name:        key.Name,
				Key:         key.Key,
				Permissions: permissions,
			})
		}

		// Convert JWT config
		if cfg.Auth.JWT != nil {
			authCfg.JWT = &auth.JWTConfig{
				Enabled:   cfg.Auth.JWT.Enabled,
				Secret:    cfg.Auth.JWT.Secret,
				PublicKey: cfg.Auth.JWT.PublicKey,
				Issuer:    cfg.Auth.JWT.Issuer,
				Audience:  cfg.Auth.JWT.Audience,
			}
		}

		var err error
		authMiddleware, err = auth.NewMiddleware(authCfg)
		if err != nil {
			return nil, fmt.Errorf("creating auth middleware: %w", err)
		}

		if authCfg.Enabled {
			log.Info().
				Int("api_keys", len(authCfg.APIKeys)).
				Bool("jwt", authCfg.JWT != nil && authCfg.JWT.Enabled).
				Int("allow_ips", len(authCfg.AllowIPs)).
				Msg("Authentication enabled for management API")
		}
	}

	// Create version store and audit log
	versionStore := versioning.NewStore(10) // Keep last 10 versions
	auditLog := versioning.NewAuditLog(1000)

	// Create rate limiter
	var rateLimiter *ratelimit.Limiter
	if cfg.RateLimit != nil && cfg.RateLimit.Enabled {
		rateLimitCfg := &ratelimit.Config{
			Enabled:     cfg.RateLimit.Enabled,
			GlobalRPS:   cfg.RateLimit.GlobalRPS,
			GlobalBurst: cfg.RateLimit.GlobalBurst,
			PerIPRPS:    cfg.RateLimit.PerIPRPS,
			PerIPBurst:  cfg.RateLimit.PerIPBurst,
			TrustProxy:  cfg.RateLimit.TrustProxy,
			ExemptIPs:   cfg.RateLimit.ExemptIPs,
		}
		for _, pl := range cfg.RateLimit.PathLimits {
			rateLimitCfg.PathLimits = append(rateLimitCfg.PathLimits, ratelimit.PathLimit{
				Path:  pl.Path,
				RPS:   pl.RPS,
				Burst: pl.Burst,
			})
		}
		rateLimiter = ratelimit.New(rateLimitCfg)
		log.Info().
			Float64("global_rps", rateLimitCfg.GlobalRPS).
			Float64("per_ip_rps", rateLimitCfg.PerIPRPS).
			Msg("Rate limiting enabled")
	}

	// Create tracing provider
	var tracingProvider *tracing.Provider
	if cfg.Tracing != nil && cfg.Tracing.Enabled {
		tracingCfg := &tracing.Config{
			Enabled:      cfg.Tracing.Enabled,
			Endpoint:     cfg.Tracing.Endpoint,
			ServiceName:  cfg.Tracing.ServiceName,
			Environment:  cfg.Tracing.Environment,
			SamplingRate: cfg.Tracing.SamplingRate,
			Insecure:     cfg.Tracing.Insecure,
		}
		var err error
		tracingProvider, err = tracing.NewProvider(context.Background(), tracingCfg)
		if err != nil {
			return nil, fmt.Errorf("creating tracing provider: %w", err)
		}
		log.Info().
			Str("endpoint", tracingCfg.Endpoint).
			Float64("sampling_rate", tracingCfg.SamplingRate).
			Msg("OpenTelemetry tracing enabled")
	} else {
		// Create disabled provider for no-op tracing
		var providerErr error
		tracingProvider, providerErr = tracing.NewProvider(context.Background(), nil)
		if providerErr != nil {
			log.Warn().Err(providerErr).Msg("Failed to create no-op tracing provider")
		}
	}

	s := &Server{
		cfg:             cfg,
		configPath:      configPath,
		router:          r,
		stats:           stats.NewCollector(statsCfg),
		metrics:         m,
		authMiddleware:  authMiddleware,
		versionStore:    versionStore,
		auditLog:        auditLog,
		tracingProvider: tracingProvider,
		rateLimiter:     rateLimiter,
		promHandler:     promHandler,
	}

	// Record initial config version
	initialVersion := versionStore.Add(cfg, configPath)
	auditLog.LogConfigChange(versioning.AuditEventConfigLoaded, initialVersion, "system", "startup")
	log.Info().Int("version", initialVersion.Version).Str("hash", initialVersion.Hash).Msg("Initial config version recorded")

	// Configure main HTTP server handler with optional rate limiting
	redirectHandler := s.handleRedirect
	if rateLimiter != nil {
		redirectHandler = rateLimiter.Middleware(redirectHandler)
	}

	s.httpServer = &fasthttp.Server{
		Handler:            redirectHandler,
		Name:               "the-redirector",
		ReadTimeout:        5 * time.Second,
		WriteTimeout:       5 * time.Second,
		MaxConnsPerIP:      1000,
		MaxRequestsPerConn: 10000,
		DisableKeepalive:   false,
	}

	// Configure management server with auth middleware
	managementHandler := s.handleManagement
	if authMiddleware != nil && authMiddleware.IsEnabled() {
		managementHandler = authMiddleware.Wrap(s.handleManagement)
	}

	s.managementServer = &fasthttp.Server{
		Handler:      managementHandler,
		Name:         "the-redirector-mgmt",
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return s, nil
}

// Start begins serving HTTP requests.
func (s *Server) Start(ctx context.Context) error {
	errChan := make(chan error, 2)

	// Start main HTTP server
	go func() {
		addr := fmt.Sprintf(":%d", s.cfg.Server.Port)
		log.Info().Str("addr", addr).Msg("Starting redirect server")
		if err := s.httpServer.ListenAndServe(addr); err != nil {
			errChan <- fmt.Errorf("redirect server: %w", err)
		}
	}()

	// Start management server
	go func() {
		addr := fmt.Sprintf(":%d", s.cfg.Server.ManagementPort)
		log.Info().Str("addr", addr).Msg("Starting management server")
		if err := s.managementServer.ListenAndServe(addr); err != nil {
			errChan <- fmt.Errorf("management server: %w", err)
		}
	}()

	// Wait for context cancellation or error
	select {
	case <-ctx.Done():
		log.Info().Msg("Shutting down servers")
		s.httpServer.Shutdown()
		s.managementServer.Shutdown()
		// Shutdown tracing provider
		if s.tracingProvider != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.tracingProvider.Shutdown(shutdownCtx); err != nil {
				log.Error().Err(err).Msg("Failed to shutdown tracing provider")
			}
		}
		// Shutdown rate limiter
		if s.rateLimiter != nil {
			s.rateLimiter.Close()
		}
		return nil
	case err := <-errChan:
		return err
	}
}

// handleRedirect processes incoming requests and returns responses.
// Supports any HTTP status code, not just redirects.
func (s *Server) handleRedirect(ctx *fasthttp.RequestCtx) {
	start := time.Now()
	path := string(ctx.Path())
	host := string(ctx.Host())
	method := string(ctx.Method())

	// Start tracing span if enabled
	var span trace.Span
	if s.tracingProvider != nil && s.tracingProvider.IsEnabled() {
		_, span = s.tracingProvider.StartRequestSpan(context.Background(), method, path, host)
		defer span.End()
		span.SetAttributes(tracing.AttrClientIP.String(ctx.RemoteIP().String()))
	}

	// Track in-flight requests
	if s.metrics != nil {
		s.metrics.IncrementInFlight()
		defer s.metrics.DecrementInFlight()
	}

	// Early host rejection — avoid processing rules for unknown domains
	s.mu.RLock()
	allowed := s.router.IsAllowedHost(host)
	s.mu.RUnlock()

	if !allowed {
		if s.metrics != nil {
			s.metrics.RecordHostRejected()
		}
		log.Debug().Str("host", host).Msg("Rejected unknown host")
		ctx.SetStatusCode(421) // Misdirected Request
		ctx.SetBodyString("Misdirected Request")
		return
	}

	s.mu.RLock()
	rule, captures := s.router.Match(host, path)
	s.mu.RUnlock()

	// Defer stats and metrics recording
	defer func() {
		duration := time.Since(start)
		status := ctx.Response.StatusCode()
		ruleID := ""
		matchType := ""
		if rule != nil {
			ruleID = rule.ID
			matchType = string(rule.Match.Type)
		}

		// Record Prometheus metrics
		if s.metrics != nil {
			s.metrics.RecordRequest(method, status, ruleID, duration.Seconds(), len(ctx.Response.Body()))
			if rule != nil {
				s.metrics.RecordRuleMatch(ruleID, matchType)
			}
		}

		// Record stats for TUI
		if s.stats != nil {
			destination := ""
			if rule != nil {
				destination = rule.Redirect.GetLocation()
			}
			s.stats.Record(stats.RequestRecord{
				Timestamp:   start,
				Path:        path,
				Host:        host,
				Status:      status,
				RuleID:      ruleID,
				Destination: destination,
				LatencyUs:   duration.Microseconds(),
				ClientIP:    ctx.RemoteIP().String(),
			})
		}
	}()

	if rule == nil {
		if span != nil {
			tracing.RecordNoMatch(span)
		}
		ctx.SetStatusCode(fasthttp.StatusNotFound)
		ctx.SetBodyString("Not Found")
		return
	}

	// Record rule match in tracing span
	if span != nil {
		tracing.RecordRuleMatch(span, rule.ID, string(rule.Match.Type), rule.Redirect.GetLocation(), rule.Redirect.Status)
	}

	// Set custom headers first
	for key, value := range rule.Redirect.Headers {
		ctx.Response.Header.Set(key, value)
	}

	// Handle non-redirect responses (4xx, 5xx, etc.)
	if !rule.Redirect.IsRedirect() {
		ctx.SetStatusCode(rule.Redirect.Status)
		if rule.Redirect.Body != "" {
			ctx.SetBodyString(rule.Redirect.Body)
		}
		// Set default headers
		ctx.Response.Header.Set(headerMatchedBy, headerValueApp)
		ctx.Response.Header.Set(headerRuleID, rule.ID)
		return
	}

	// Handle redirect responses (3xx)
	destination := rule.Redirect.GetLocation()

	// Apply regex captures if any
	if len(captures) > 0 && rule.CompiledRegex() != nil {
		destination = rule.CompiledRegex().ReplaceAllString(path, destination)
	}

	// Preserve path suffix for prefix matches
	if rule.Redirect.PreservePath && rule.Match.Type == config.MatchTypePrefix {
		suffix := path[len(rule.Match.Path):]
		destination += suffix
	}

	// Preserve query string
	if rule.Redirect.PreserveQuery != nil && *rule.Redirect.PreserveQuery {
		queryString := ctx.QueryArgs().String()
		if queryString != "" {
			if len(destination) > 0 && destination[len(destination)-1] != '?' {
				destination += "?"
			}
			destination += queryString
		}
	}

	// Set default headers
	ctx.Response.Header.Set(headerRedirectedBy, headerValueApp)
	ctx.Response.Header.Set(headerRuleID, rule.ID)

	// Log the redirect
	log.Debug().
		Str("path", path).
		Str("destination", destination).
		Str("rule_id", rule.ID).
		Int("status", rule.Redirect.Status).
		Msg("Redirect")

	// Send redirect response
	ctx.Redirect(destination, rule.Redirect.Status)
}

// handleManagement handles management API requests.
func (s *Server) handleManagement(ctx *fasthttp.RequestCtx) {
	path := string(ctx.Path())

	switch {
	// Public endpoints (no auth required)
	case path == "/health":
		s.handleHealth(ctx)
	case path == "/ready":
		s.handleReady(ctx)
	case path == "/metrics":
		s.handleMetrics(ctx)

	// Read endpoints (require read permission if auth enabled)
	case path == "/api/v1/config":
		if !s.requirePermission(ctx, auth.PermissionRead) {
			return
		}
		s.handleConfig(ctx)
	case path == "/api/v1/rules":
		if !s.requirePermission(ctx, auth.PermissionRead) {
			return
		}
		s.handleRules(ctx)
	case path == "/stats":
		if !s.requirePermission(ctx, auth.PermissionStatsRead) {
			return
		}
		s.handleStats(ctx)
	case strings.HasPrefix(path, "/stats/live"):
		if !s.requirePermission(ctx, auth.PermissionStatsRead) {
			return
		}
		s.handleStatsLive(ctx)
	case strings.HasPrefix(path, "/stats/rule/"):
		if !s.requirePermission(ctx, auth.PermissionStatsRead) {
			return
		}
		s.handleStatsRule(ctx)

	// Write endpoints (require specific permissions)
	case path == "/stats/enable":
		if !s.requirePermission(ctx, auth.PermissionStatsWrite) {
			return
		}
		s.handleStatsEnable(ctx)
	case path == "/stats/disable":
		if !s.requirePermission(ctx, auth.PermissionStatsWrite) {
			return
		}
		s.handleStatsDisable(ctx)
	case path == "/stats/reset":
		if !s.requirePermission(ctx, auth.PermissionStatsWrite) {
			return
		}
		s.handleStatsReset(ctx)
	case path == "/api/v1/reload":
		if !s.requirePermission(ctx, auth.PermissionReload) {
			return
		}
		s.handleReload(ctx)

	// Version endpoints
	case path == "/api/v1/versions":
		if !s.requirePermission(ctx, auth.PermissionRead) {
			return
		}
		s.handleVersions(ctx)
	case path == "/api/v1/versions/current":
		if !s.requirePermission(ctx, auth.PermissionRead) {
			return
		}
		s.handleCurrentVersion(ctx)
	case path == "/api/v1/rollback":
		if !s.requirePermission(ctx, auth.PermissionWrite) {
			return
		}
		s.handleRollback(ctx)

	// Audit endpoint
	case path == "/api/v1/audit":
		if !s.requirePermission(ctx, auth.PermissionRead) {
			return
		}
		s.handleAudit(ctx)

	// Debug endpoints (require admin permission)
	case path == "/debug/pprof/":
		if !s.requirePermission(ctx, auth.PermissionAdmin) {
			return
		}
		s.handlePprofIndex(ctx)
	case path == "/debug/pprof/cmdline":
		if !s.requirePermission(ctx, auth.PermissionAdmin) {
			return
		}
		s.handlePprofCmdline(ctx)
	case path == "/debug/pprof/profile":
		if !s.requirePermission(ctx, auth.PermissionAdmin) {
			return
		}
		s.handlePprofProfile(ctx)
	case path == "/debug/pprof/symbol":
		if !s.requirePermission(ctx, auth.PermissionAdmin) {
			return
		}
		s.handlePprofSymbol(ctx)
	case path == "/debug/pprof/trace":
		if !s.requirePermission(ctx, auth.PermissionAdmin) {
			return
		}
		s.handlePprofTrace(ctx)
	case strings.HasPrefix(path, "/debug/pprof/"):
		if !s.requirePermission(ctx, auth.PermissionAdmin) {
			return
		}
		s.handlePprofHandler(ctx)
	case path == "/debug/vars":
		if !s.requirePermission(ctx, auth.PermissionAdmin) {
			return
		}
		s.handleDebugVars(ctx)
	case path == "/debug/config":
		if !s.requirePermission(ctx, auth.PermissionAdmin) {
			return
		}
		s.handleDebugConfig(ctx)
	case path == "/debug/rules":
		if !s.requirePermission(ctx, auth.PermissionAdmin) {
			return
		}
		s.handleDebugRules(ctx)
	case path == "/debug/runtime":
		if !s.requirePermission(ctx, auth.PermissionAdmin) {
			return
		}
		s.handleDebugRuntime(ctx)

	default:
		ctx.SetStatusCode(fasthttp.StatusNotFound)
		ctx.SetBodyString("Not Found")
	}
}

// requirePermission checks if the request has the required permission.
// Returns true if permission granted, false if denied (and sends 403).
func (s *Server) requirePermission(ctx *fasthttp.RequestCtx, perm auth.Permission) bool {
	if s.authMiddleware == nil || !s.authMiddleware.IsEnabled() {
		return true // Auth disabled, allow all
	}

	principal, ok := ctx.UserValue("principal").(*auth.Principal)
	if !ok {
		// This shouldn't happen if middleware ran, but handle gracefully
		ctx.SetStatusCode(fasthttp.StatusUnauthorized)
		ctx.SetContentType(contentTypeJSON)
		ctx.SetBodyString(`{"error":"unauthorized","message":"authentication required"}`)
		return false
	}

	if !principal.HasPermission(perm) {
		ctx.SetStatusCode(fasthttp.StatusForbidden)
		ctx.SetContentType(contentTypeJSON)
		ctx.SetBodyString(`{"error":"forbidden","message":"permission denied: ` + string(perm) + `"}`)
		return false
	}

	return true
}

func (s *Server) handleHealth(ctx *fasthttp.RequestCtx) {
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(`{"status":"healthy"}`)
	ctx.SetContentType(contentTypeJSON)
}

func (s *Server) handleReady(ctx *fasthttp.RequestCtx) {
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(`{"status":"ready"}`)
	ctx.SetContentType(contentTypeJSON)
}

func (s *Server) handleMetrics(ctx *fasthttp.RequestCtx) {
	if s.promHandler != nil {
		s.promHandler(ctx)
		return
	}
	// Fallback if metrics not initialized
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString("# HELP redirector_up Whether the redirector is up\n# TYPE redirector_up gauge\nredirector_up 1\n")
	ctx.SetContentType("text/plain")
}

func (s *Server) handleConfig(ctx *fasthttp.RequestCtx) {
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(fmt.Sprintf(`{"version":"%s","rules_count":%d}`, s.cfg.Version, len(s.cfg.Rules)))
	ctx.SetContentType(contentTypeJSON)
}

func (s *Server) handleRules(ctx *fasthttp.RequestCtx) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Simple JSON output of rules
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(fmt.Sprintf(`{"count":%d}`, len(s.cfg.Rules)))
	ctx.SetContentType(contentTypeJSON)
}

// ReloadConfig reloads configuration from the given path (file or directory).
func (s *Server) ReloadConfig(path string) error {
	start := time.Now()

	// Start tracing span if enabled
	var span trace.Span
	if s.tracingProvider != nil && s.tracingProvider.IsEnabled() {
		_, span = s.tracingProvider.StartConfigReloadSpan(context.Background(), path)
		defer span.End()
	}

	// LoadDirectory handles both files and directories
	cfg, err := config.LoadDirectory(path)
	if err != nil {
		if s.metrics != nil {
			s.metrics.RecordConfigReload(false, 0, time.Since(start).Seconds())
		}
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "config load failed")
		}
		return fmt.Errorf("loading config: %w", err)
	}

	r, err := router.New(cfg.Rules)
	if err != nil {
		if s.metrics != nil {
			s.metrics.RecordConfigReload(false, 0, time.Since(start).Seconds())
		}
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "router creation failed")
		}
		return fmt.Errorf("creating router: %w", err)
	}

	s.mu.Lock()
	s.cfg = cfg
	s.router = r
	s.mu.Unlock()

	// Record config version
	version := s.versionStore.Add(cfg, path)
	s.auditLog.LogConfigChange(versioning.AuditEventConfigReloaded, version, "system", "reload")

	// Record successful reload
	if s.metrics != nil {
		s.metrics.RecordConfigReload(true, len(cfg.Rules), time.Since(start).Seconds())
	}

	// Record in tracing span
	if span != nil {
		tracing.RecordConfigReload(span, version.Version, len(cfg.Rules), time.Since(start))
		span.SetStatus(codes.Ok, "config reloaded successfully")
	}

	log.Info().
		Int("rules", len(cfg.Rules)).
		Int("version", version.Version).
		Str("hash", version.Hash).
		Msg("Configuration reloaded")
	return nil
}

// Stats handler functions

func (s *Server) handleStats(ctx *fasthttp.RequestCtx) {
	if s.stats == nil {
		writeJSONError(ctx, fasthttp.StatusServiceUnavailable, "stats not enabled")
		return
	}

	summary := s.stats.GetSummary()
	data, err := json.Marshal(summary)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString(fmt.Sprintf(`{"error":"%s"}`, err.Error()))
		ctx.SetContentType(contentTypeJSON)
		return
	}

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(data)
	ctx.SetContentType(contentTypeJSON)
}

func (s *Server) handleStatsLive(ctx *fasthttp.RequestCtx) {
	if s.stats == nil {
		writeJSONError(ctx, fasthttp.StatusServiceUnavailable, "stats not enabled")
		return
	}

	// Parse limit from query string
	limit := 100
	if limitStr := string(ctx.QueryArgs().Peek("limit")); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	requests := s.stats.GetRecentRequests(limit)
	data, err := json.Marshal(requests)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString(fmt.Sprintf(`{"error":"%s"}`, err.Error()))
		ctx.SetContentType(contentTypeJSON)
		return
	}

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(data)
	ctx.SetContentType(contentTypeJSON)
}

func (s *Server) handleStatsRule(ctx *fasthttp.RequestCtx) {
	if s.stats == nil {
		writeJSONError(ctx, fasthttp.StatusServiceUnavailable, "stats not enabled")
		return
	}

	// Extract rule ID from path
	path := string(ctx.Path())
	ruleID := strings.TrimPrefix(path, "/stats/rule/")
	if ruleID == "" {
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		ctx.SetBodyString(`{"error":"rule ID required"}`)
		ctx.SetContentType(contentTypeJSON)
		return
	}

	ruleStats, found := s.stats.GetRuleStats(ruleID)
	if !found {
		ctx.SetStatusCode(fasthttp.StatusNotFound)
		ctx.SetBodyString(`{"error":"rule not found"}`)
		ctx.SetContentType(contentTypeJSON)
		return
	}

	data, err := json.Marshal(ruleStats)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString(fmt.Sprintf(`{"error":"%s"}`, err.Error()))
		ctx.SetContentType(contentTypeJSON)
		return
	}

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(data)
	ctx.SetContentType(contentTypeJSON)
}

func (s *Server) handleStatsEnable(ctx *fasthttp.RequestCtx) {
	if s.stats == nil {
		writeJSONError(ctx, fasthttp.StatusServiceUnavailable, "stats collector not initialized")
		return
	}

	s.stats.Enable()
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(`{"status":"enabled"}`)
	ctx.SetContentType(contentTypeJSON)
}

func (s *Server) handleStatsDisable(ctx *fasthttp.RequestCtx) {
	if s.stats == nil {
		writeJSONError(ctx, fasthttp.StatusServiceUnavailable, "stats collector not initialized")
		return
	}

	s.stats.Disable()
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(`{"status":"disabled"}`)
	ctx.SetContentType(contentTypeJSON)
}

func (s *Server) handleStatsReset(ctx *fasthttp.RequestCtx) {
	if s.stats == nil {
		writeJSONError(ctx, fasthttp.StatusServiceUnavailable, "stats collector not initialized")
		return
	}

	s.stats.Reset()
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(`{"status":"reset"}`)
	ctx.SetContentType(contentTypeJSON)
}

func (s *Server) handleReload(ctx *fasthttp.RequestCtx) {
	// Only allow POST
	if !ctx.IsPost() {
		ctx.SetStatusCode(fasthttp.StatusMethodNotAllowed)
		ctx.SetBodyString(`{"error":"method not allowed, use POST"}`)
		ctx.SetContentType(contentTypeJSON)
		return
	}

	if s.configPath == "" {
		ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
		ctx.SetBodyString(`{"error":"config path not set"}`)
		ctx.SetContentType(contentTypeJSON)
		return
	}

	start := time.Now()
	if err := s.ReloadConfig(s.configPath); err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString(fmt.Sprintf(`{"error":"reload failed: %s"}`, err.Error()))
		ctx.SetContentType(contentTypeJSON)
		return
	}

	s.mu.RLock()
	rulesCount := len(s.cfg.Rules)
	s.mu.RUnlock()

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(fmt.Sprintf(`{"status":"reloaded","rules_count":%d,"duration_ms":%d}`,
		rulesCount, time.Since(start).Milliseconds()))
	ctx.SetContentType(contentTypeJSON)
}

// Version management handlers

func (s *Server) handleVersions(ctx *fasthttp.RequestCtx) {
	versions := s.versionStore.List()

	// Create response without full config
	type versionInfo struct {
		Version    int                       `json:"version"`
		Hash       string                    `json:"hash"`
		LoadedAt   time.Time                 `json:"loaded_at"`
		Source     string                    `json:"source"`
		RulesCount int                       `json:"rules_count"`
		Changes    *versioning.ConfigChanges `json:"changes,omitempty"`
	}

	result := make([]versionInfo, len(versions))
	for i, v := range versions {
		result[i] = versionInfo{
			Version:    v.Version,
			Hash:       v.Hash,
			LoadedAt:   v.LoadedAt,
			Source:     v.Source,
			RulesCount: v.RulesCount,
			Changes:    v.Changes,
		}
	}

	data, err := json.Marshal(result)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString(fmt.Sprintf(`{"error":"%s"}`, err.Error()))
		ctx.SetContentType(contentTypeJSON)
		return
	}

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(data)
	ctx.SetContentType(contentTypeJSON)
}

func (s *Server) handleCurrentVersion(ctx *fasthttp.RequestCtx) {
	current := s.versionStore.Current()
	if current == nil {
		ctx.SetStatusCode(fasthttp.StatusNotFound)
		ctx.SetBodyString(`{"error":"no version available"}`)
		ctx.SetContentType(contentTypeJSON)
		return
	}

	response := struct {
		Version    int                       `json:"version"`
		Hash       string                    `json:"hash"`
		LoadedAt   time.Time                 `json:"loaded_at"`
		Source     string                    `json:"source"`
		RulesCount int                       `json:"rules_count"`
		Changes    *versioning.ConfigChanges `json:"changes,omitempty"`
	}{
		Version:    current.Version,
		Hash:       current.Hash,
		LoadedAt:   current.LoadedAt,
		Source:     current.Source,
		RulesCount: current.RulesCount,
		Changes:    current.Changes,
	}

	data, err := json.Marshal(response)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString(fmt.Sprintf(`{"error":"%s"}`, err.Error()))
		ctx.SetContentType(contentTypeJSON)
		return
	}

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(data)
	ctx.SetContentType(contentTypeJSON)
}

func (s *Server) handleRollback(ctx *fasthttp.RequestCtx) {
	// Only allow POST
	if !ctx.IsPost() {
		ctx.SetStatusCode(fasthttp.StatusMethodNotAllowed)
		ctx.SetBodyString(`{"error":"method not allowed, use POST"}`)
		ctx.SetContentType(contentTypeJSON)
		return
	}

	// Parse version from request body
	var req struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(ctx.PostBody(), &req); err != nil {
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		ctx.SetBodyString(`{"error":"invalid request body, expected {\"version\": N}"}`)
		ctx.SetContentType(contentTypeJSON)
		return
	}

	if req.Version < 1 {
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		ctx.SetBodyString(`{"error":"version must be >= 1"}`)
		ctx.SetContentType(contentTypeJSON)
		return
	}

	// Find the version to rollback to
	targetVersion := s.versionStore.Get(req.Version)
	if targetVersion == nil {
		ctx.SetStatusCode(fasthttp.StatusNotFound)
		ctx.SetBodyString(fmt.Sprintf(`{"error":"version %d not found"}`, req.Version))
		ctx.SetContentType(contentTypeJSON)
		return
	}

	// Perform rollback
	rolledBack := s.versionStore.Rollback(req.Version)
	if rolledBack == nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString(`{"error":"rollback failed"}`)
		ctx.SetContentType(contentTypeJSON)
		return
	}

	// Update the server with the rolled back config
	r, err := router.New(rolledBack.Config.Rules)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString(fmt.Sprintf(`{"error":"failed to create router: %s"}`, err.Error()))
		ctx.SetContentType(contentTypeJSON)
		return
	}

	s.mu.Lock()
	s.cfg = rolledBack.Config
	s.router = r
	s.mu.Unlock()

	// Get actor from auth principal if available
	actor := "anonymous"
	if principal, ok := ctx.UserValue("principal").(*auth.Principal); ok {
		actor = principal.ID
	}

	// Log the rollback
	s.auditLog.LogConfigChange(versioning.AuditEventConfigRollback, rolledBack, actor, ctx.RemoteIP().String())
	log.Info().
		Int("from_version", req.Version).
		Int("new_version", rolledBack.Version).
		Str("actor", actor).
		Msg("Config rolled back")

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(fmt.Sprintf(`{"status":"rolled_back","from_version":%d,"new_version":%d,"rules_count":%d}`,
		req.Version, rolledBack.Version, rolledBack.RulesCount))
	ctx.SetContentType(contentTypeJSON)
}

func (s *Server) handleAudit(ctx *fasthttp.RequestCtx) {
	// Parse limit from query string
	limit := 100
	if limitStr := string(ctx.QueryArgs().Peek("limit")); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	events := s.auditLog.Recent(limit)

	data, err := json.Marshal(events)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString(fmt.Sprintf(`{"error":"%s"}`, err.Error()))
		ctx.SetContentType(contentTypeJSON)
		return
	}

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(data)
	ctx.SetContentType(contentTypeJSON)
}

// Debug endpoint handlers

func (s *Server) handlePprofIndex(ctx *fasthttp.RequestCtx) {
	fasthttpadaptor.NewFastHTTPHandlerFunc(pprof.Index)(ctx)
}

func (s *Server) handlePprofCmdline(ctx *fasthttp.RequestCtx) {
	fasthttpadaptor.NewFastHTTPHandlerFunc(pprof.Cmdline)(ctx)
}

func (s *Server) handlePprofProfile(ctx *fasthttp.RequestCtx) {
	fasthttpadaptor.NewFastHTTPHandlerFunc(pprof.Profile)(ctx)
}

func (s *Server) handlePprofSymbol(ctx *fasthttp.RequestCtx) {
	fasthttpadaptor.NewFastHTTPHandlerFunc(pprof.Symbol)(ctx)
}

func (s *Server) handlePprofTrace(ctx *fasthttp.RequestCtx) {
	fasthttpadaptor.NewFastHTTPHandlerFunc(pprof.Trace)(ctx)
}

func (s *Server) handlePprofHandler(ctx *fasthttp.RequestCtx) {
	// Extract the profile name from the path
	path := string(ctx.Path())
	name := strings.TrimPrefix(path, "/debug/pprof/")
	fasthttpadaptor.NewFastHTTPHandler(pprof.Handler(name))(ctx)
}

func (s *Server) handleDebugVars(ctx *fasthttp.RequestCtx) {
	// Use expvar handler adapted to fasthttp
	fasthttpadaptor.NewFastHTTPHandler(expvar.Handler())(ctx)
}

func (s *Server) handleDebugConfig(ctx *fasthttp.RequestCtx) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create a sanitized config view (mask sensitive values)
	type sanitizedConfig struct {
		Version string `json:"version"`
		Server  struct {
			Port           int    `json:"port"`
			ManagementPort int    `json:"management_port"`
			ReadTimeout    string `json:"read_timeout"`
			WriteTimeout   string `json:"write_timeout"`
			MaxConnections int    `json:"max_connections"`
		} `json:"server"`
		Defaults struct {
			StatusCode    int  `json:"status_code"`
			PreserveQuery bool `json:"preserve_query"`
		} `json:"defaults"`
		Stats struct {
			Enabled      bool    `json:"enabled"`
			BufferSize   int     `json:"buffer_size"`
			SamplingRate float64 `json:"sampling_rate"`
		} `json:"stats"`
		Auth struct {
			Enabled     bool     `json:"enabled"`
			APIKeyCount int      `json:"api_key_count"`
			JWTEnabled  bool     `json:"jwt_enabled"`
			AllowIPs    []string `json:"allow_ips"`
		} `json:"auth"`
		Tracing struct {
			Enabled      bool    `json:"enabled"`
			Endpoint     string  `json:"endpoint"`
			ServiceName  string  `json:"service_name"`
			SamplingRate float64 `json:"sampling_rate"`
		} `json:"tracing"`
		RateLimit struct {
			Enabled    bool    `json:"enabled"`
			GlobalRPS  float64 `json:"global_rps"`
			PerIPRPS   float64 `json:"per_ip_rps"`
			PathLimits int     `json:"path_limits_count"`
		} `json:"rate_limit"`
		RulesCount int `json:"rules_count"`
	}

	cfg := sanitizedConfig{}
	cfg.Version = s.cfg.Version
	cfg.Server.Port = s.cfg.Server.Port
	cfg.Server.ManagementPort = s.cfg.Server.ManagementPort
	cfg.Server.ReadTimeout = s.cfg.Server.ReadTimeout
	cfg.Server.WriteTimeout = s.cfg.Server.WriteTimeout
	cfg.Server.MaxConnections = s.cfg.Server.MaxConnections

	cfg.Defaults.StatusCode = s.cfg.Defaults.StatusCode
	cfg.Defaults.PreserveQuery = s.cfg.Defaults.PreserveQuery

	if s.cfg.Stats != nil {
		cfg.Stats.Enabled = s.cfg.Stats.Enabled
		cfg.Stats.BufferSize = s.cfg.Stats.BufferSize
		cfg.Stats.SamplingRate = s.cfg.Stats.SamplingRate
	}

	if s.cfg.Auth != nil {
		cfg.Auth.Enabled = s.cfg.Auth.Enabled
		cfg.Auth.APIKeyCount = len(s.cfg.Auth.APIKeys)
		cfg.Auth.JWTEnabled = s.cfg.Auth.JWT != nil && s.cfg.Auth.JWT.Enabled
		cfg.Auth.AllowIPs = s.cfg.Auth.AllowIPs
	}

	if s.cfg.Tracing != nil {
		cfg.Tracing.Enabled = s.cfg.Tracing.Enabled
		cfg.Tracing.Endpoint = s.cfg.Tracing.Endpoint
		cfg.Tracing.ServiceName = s.cfg.Tracing.ServiceName
		cfg.Tracing.SamplingRate = s.cfg.Tracing.SamplingRate
	}

	if s.cfg.RateLimit != nil {
		cfg.RateLimit.Enabled = s.cfg.RateLimit.Enabled
		cfg.RateLimit.GlobalRPS = s.cfg.RateLimit.GlobalRPS
		cfg.RateLimit.PerIPRPS = s.cfg.RateLimit.PerIPRPS
		cfg.RateLimit.PathLimits = len(s.cfg.RateLimit.PathLimits)
	}

	cfg.RulesCount = len(s.cfg.Rules)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString(fmt.Sprintf(`{"error":"%s"}`, err.Error()))
		ctx.SetContentType(contentTypeJSON)
		return
	}

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(data)
	ctx.SetContentType(contentTypeJSON)
}

func (s *Server) handleDebugRules(ctx *fasthttp.RequestCtx) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return detailed rule information
	type ruleInfo struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Path     string `json:"path,omitempty"`
		Pattern  string `json:"pattern,omitempty"`
		Host     string `json:"host,omitempty"`
		To       string `json:"to,omitempty"`
		Status   int    `json:"status"`
		Priority int    `json:"priority"`
	}

	rules := make([]ruleInfo, len(s.cfg.Rules))
	for i, r := range s.cfg.Rules {
		rules[i] = ruleInfo{
			ID:       r.ID,
			Type:     string(r.Match.Type),
			Path:     r.Match.Path,
			Pattern:  r.Match.Pattern,
			Host:     r.Match.Host,
			To:       r.Redirect.To,
			Status:   r.Redirect.Status,
			Priority: r.Priority,
		}
	}

	response := struct {
		Count       int         `json:"count"`
		RouterStats interface{} `json:"router_stats"`
		Rules       []ruleInfo  `json:"rules"`
	}{
		Count:       len(rules),
		RouterStats: s.router.GetStats(),
		Rules:       rules,
	}

	data, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString(fmt.Sprintf(`{"error":"%s"}`, err.Error()))
		ctx.SetContentType(contentTypeJSON)
		return
	}

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(data)
	ctx.SetContentType(contentTypeJSON)
}

func (s *Server) handleDebugRuntime(ctx *fasthttp.RequestCtx) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	response := struct {
		Go struct {
			Version    string `json:"version"`
			NumCPU     int    `json:"num_cpu"`
			GOMAXPROCS int    `json:"gomaxprocs"`
			Goroutines int    `json:"goroutines"`
		} `json:"go"`
		Memory struct {
			Alloc      uint64 `json:"alloc_bytes"`
			TotalAlloc uint64 `json:"total_alloc_bytes"`
			Sys        uint64 `json:"sys_bytes"`
			HeapAlloc  uint64 `json:"heap_alloc_bytes"`
			HeapSys    uint64 `json:"heap_sys_bytes"`
			HeapIdle   uint64 `json:"heap_idle_bytes"`
			HeapInuse  uint64 `json:"heap_inuse_bytes"`
			StackInuse uint64 `json:"stack_inuse_bytes"`
			NumGC      uint32 `json:"num_gc"`
			LastGC     uint64 `json:"last_gc_ns"`
		} `json:"memory"`
		Uptime string `json:"uptime"`
	}{}

	response.Go.Version = runtime.Version()
	response.Go.NumCPU = runtime.NumCPU()
	response.Go.GOMAXPROCS = runtime.GOMAXPROCS(0)
	response.Go.Goroutines = runtime.NumGoroutine()

	response.Memory.Alloc = memStats.Alloc
	response.Memory.TotalAlloc = memStats.TotalAlloc
	response.Memory.Sys = memStats.Sys
	response.Memory.HeapAlloc = memStats.HeapAlloc
	response.Memory.HeapSys = memStats.HeapSys
	response.Memory.HeapIdle = memStats.HeapIdle
	response.Memory.HeapInuse = memStats.HeapInuse
	response.Memory.StackInuse = memStats.StackInuse
	response.Memory.NumGC = memStats.NumGC
	response.Memory.LastGC = memStats.LastGC

	// Calculate uptime (approximate based on process start)
	response.Uptime = "N/A" // Would need to track start time

	data, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString(fmt.Sprintf(`{"error":"%s"}`, err.Error()))
		ctx.SetContentType(contentTypeJSON)
		return
	}

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(data)
	ctx.SetContentType(contentTypeJSON)
}
