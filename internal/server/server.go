package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"
	"github.com/your-org/the-redirector/internal/config"
	"github.com/your-org/the-redirector/internal/metrics"
	"github.com/your-org/the-redirector/internal/router"
	"github.com/your-org/the-redirector/internal/stats"
)

// Server handles HTTP requests and redirects.
type Server struct {
	cfg        *config.Config
	configPath string
	router     *router.Router
	stats      *stats.Collector
	metrics    *metrics.Metrics

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

	s := &Server{
		cfg:         cfg,
		configPath:  configPath,
		router:      r,
		stats:       stats.NewCollector(statsCfg),
		metrics:     m,
		promHandler: promHandler,
	}

	// Configure main HTTP server
	s.httpServer = &fasthttp.Server{
		Handler:            s.handleRedirect,
		Name:               "the-redirector",
		ReadTimeout:        5 * time.Second,
		WriteTimeout:       5 * time.Second,
		MaxConnsPerIP:      1000,
		MaxRequestsPerConn: 10000,
		DisableKeepalive:   false,
	}

	// Configure management server
	s.managementServer = &fasthttp.Server{
		Handler:      s.handleManagement,
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

	// Track in-flight requests
	if s.metrics != nil {
		s.metrics.IncrementInFlight()
		defer s.metrics.DecrementInFlight()
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
		ctx.SetStatusCode(fasthttp.StatusNotFound)
		ctx.SetBodyString("Not Found")
		return
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
		ctx.Response.Header.Set("X-Matched-By", "the-redirector")
		ctx.Response.Header.Set("X-Rule-ID", rule.ID)
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
	ctx.Response.Header.Set("X-Redirected-By", "the-redirector")
	ctx.Response.Header.Set("X-Rule-ID", rule.ID)

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
	case path == "/health":
		s.handleHealth(ctx)
	case path == "/ready":
		s.handleReady(ctx)
	case path == "/metrics":
		s.handleMetrics(ctx)
	case path == "/api/v1/config":
		s.handleConfig(ctx)
	case path == "/api/v1/rules":
		s.handleRules(ctx)
	case path == "/stats":
		s.handleStats(ctx)
	case strings.HasPrefix(path, "/stats/live"):
		s.handleStatsLive(ctx)
	case strings.HasPrefix(path, "/stats/rule/"):
		s.handleStatsRule(ctx)
	case path == "/stats/enable":
		s.handleStatsEnable(ctx)
	case path == "/stats/disable":
		s.handleStatsDisable(ctx)
	case path == "/stats/reset":
		s.handleStatsReset(ctx)
	case path == "/api/v1/reload":
		s.handleReload(ctx)
	default:
		ctx.SetStatusCode(fasthttp.StatusNotFound)
		ctx.SetBodyString("Not Found")
	}
}

func (s *Server) handleHealth(ctx *fasthttp.RequestCtx) {
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(`{"status":"healthy"}`)
	ctx.SetContentType("application/json")
}

func (s *Server) handleReady(ctx *fasthttp.RequestCtx) {
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(`{"status":"ready"}`)
	ctx.SetContentType("application/json")
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
	ctx.SetContentType("application/json")
}

func (s *Server) handleRules(ctx *fasthttp.RequestCtx) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Simple JSON output of rules
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(fmt.Sprintf(`{"count":%d}`, len(s.cfg.Rules)))
	ctx.SetContentType("application/json")
}

// ReloadConfig reloads configuration from the given path (file or directory).
func (s *Server) ReloadConfig(path string) error {
	start := time.Now()

	// LoadDirectory handles both files and directories
	cfg, err := config.LoadDirectory(path)
	if err != nil {
		if s.metrics != nil {
			s.metrics.RecordConfigReload(false, 0, time.Since(start).Seconds())
		}
		return fmt.Errorf("loading config: %w", err)
	}

	r, err := router.New(cfg.Rules)
	if err != nil {
		if s.metrics != nil {
			s.metrics.RecordConfigReload(false, 0, time.Since(start).Seconds())
		}
		return fmt.Errorf("creating router: %w", err)
	}

	s.mu.Lock()
	s.cfg = cfg
	s.router = r
	s.mu.Unlock()

	// Record successful reload
	if s.metrics != nil {
		s.metrics.RecordConfigReload(true, len(cfg.Rules), time.Since(start).Seconds())
	}

	log.Info().Int("rules", len(cfg.Rules)).Msg("Configuration reloaded")
	return nil
}

// Stats handler functions

func (s *Server) handleStats(ctx *fasthttp.RequestCtx) {
	if s.stats == nil {
		ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
		ctx.SetBodyString(`{"error":"stats not enabled"}`)
		ctx.SetContentType("application/json")
		return
	}

	summary := s.stats.GetSummary()
	data, err := json.Marshal(summary)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString(fmt.Sprintf(`{"error":"%s"}`, err.Error()))
		ctx.SetContentType("application/json")
		return
	}

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(data)
	ctx.SetContentType("application/json")
}

func (s *Server) handleStatsLive(ctx *fasthttp.RequestCtx) {
	if s.stats == nil {
		ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
		ctx.SetBodyString(`{"error":"stats not enabled"}`)
		ctx.SetContentType("application/json")
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
		ctx.SetContentType("application/json")
		return
	}

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(data)
	ctx.SetContentType("application/json")
}

func (s *Server) handleStatsRule(ctx *fasthttp.RequestCtx) {
	if s.stats == nil {
		ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
		ctx.SetBodyString(`{"error":"stats not enabled"}`)
		ctx.SetContentType("application/json")
		return
	}

	// Extract rule ID from path
	path := string(ctx.Path())
	ruleID := strings.TrimPrefix(path, "/stats/rule/")
	if ruleID == "" {
		ctx.SetStatusCode(fasthttp.StatusBadRequest)
		ctx.SetBodyString(`{"error":"rule ID required"}`)
		ctx.SetContentType("application/json")
		return
	}

	ruleStats, found := s.stats.GetRuleStats(ruleID)
	if !found {
		ctx.SetStatusCode(fasthttp.StatusNotFound)
		ctx.SetBodyString(`{"error":"rule not found"}`)
		ctx.SetContentType("application/json")
		return
	}

	data, err := json.Marshal(ruleStats)
	if err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString(fmt.Sprintf(`{"error":"%s"}`, err.Error()))
		ctx.SetContentType("application/json")
		return
	}

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(data)
	ctx.SetContentType("application/json")
}

func (s *Server) handleStatsEnable(ctx *fasthttp.RequestCtx) {
	if s.stats == nil {
		ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
		ctx.SetBodyString(`{"error":"stats collector not initialized"}`)
		ctx.SetContentType("application/json")
		return
	}

	s.stats.Enable()
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(`{"status":"enabled"}`)
	ctx.SetContentType("application/json")
}

func (s *Server) handleStatsDisable(ctx *fasthttp.RequestCtx) {
	if s.stats == nil {
		ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
		ctx.SetBodyString(`{"error":"stats collector not initialized"}`)
		ctx.SetContentType("application/json")
		return
	}

	s.stats.Disable()
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(`{"status":"disabled"}`)
	ctx.SetContentType("application/json")
}

func (s *Server) handleStatsReset(ctx *fasthttp.RequestCtx) {
	if s.stats == nil {
		ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
		ctx.SetBodyString(`{"error":"stats collector not initialized"}`)
		ctx.SetContentType("application/json")
		return
	}

	s.stats.Reset()
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(`{"status":"reset"}`)
	ctx.SetContentType("application/json")
}

func (s *Server) handleReload(ctx *fasthttp.RequestCtx) {
	// Only allow POST
	if !ctx.IsPost() {
		ctx.SetStatusCode(fasthttp.StatusMethodNotAllowed)
		ctx.SetBodyString(`{"error":"method not allowed, use POST"}`)
		ctx.SetContentType("application/json")
		return
	}

	if s.configPath == "" {
		ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
		ctx.SetBodyString(`{"error":"config path not set"}`)
		ctx.SetContentType("application/json")
		return
	}

	start := time.Now()
	if err := s.ReloadConfig(s.configPath); err != nil {
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString(fmt.Sprintf(`{"error":"reload failed: %s"}`, err.Error()))
		ctx.SetContentType("application/json")
		return
	}

	s.mu.RLock()
	rulesCount := len(s.cfg.Rules)
	s.mu.RUnlock()

	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(fmt.Sprintf(`{"status":"reloaded","rules_count":%d,"duration_ms":%d}`,
		rulesCount, time.Since(start).Milliseconds()))
	ctx.SetContentType("application/json")
}
