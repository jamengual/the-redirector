package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"

	"github.com/jamengual/the-redirector/internal/config"
	"github.com/jamengual/the-redirector/internal/lint"
	"github.com/jamengual/the-redirector/internal/metrics"
	"github.com/jamengual/the-redirector/internal/providers"
)

// SyncerConfig configures the config syncer service.
type SyncerConfig struct {
	// SyncInterval is how often to check for config updates
	SyncInterval time.Duration `yaml:"sync_interval"`

	// Sources defines the config sources in priority order (failover)
	Sources []SourceConfig `yaml:"sources"`

	// Output defines where to write the fetched config
	Output OutputConfig `yaml:"output"`

	// Targets for multi-target push (alternative to Output.API)
	Targets []TargetConfig `yaml:"targets"`

	// Webhook server configuration
	Webhook WebhookConfig `yaml:"webhook"`

	// Retry configuration
	Retry RetryConfig `yaml:"retry"`

	// Logging configuration
	LogLevel  string `yaml:"log_level"`
	LogFormat string `yaml:"log_format"`
}

// SourceConfig defines a configuration source.
type SourceConfig struct {
	// Name is a human-readable name for this source
	Name string `yaml:"name"`

	// Type is the source type (file, s3, azure, github, http, parameterstore)
	Type string `yaml:"type"`

	// Priority determines failover order (higher = tried first)
	Priority int `yaml:"priority"`

	// Prefix is automatically prepended to all rule IDs from this source
	Prefix string `yaml:"prefix"`

	// Enabled allows disabling a source without removing it
	Enabled bool `yaml:"enabled"`

	// Source-specific configuration
	File           *FileSourceConfig     `yaml:"file,omitempty"`
	S3             *S3SourceConfig       `yaml:"s3,omitempty"`
	Azure          *AzureSourceConfig    `yaml:"azure,omitempty"`
	GitHub         *GitHubSourceConfig   `yaml:"github,omitempty"`
	HTTP           *HTTPSourceConfig     `yaml:"http,omitempty"`
	ParameterStore *ParameterStoreConfig `yaml:"parameter_store,omitempty"`
}

// FileSourceConfig for local file sources.
type FileSourceConfig struct {
	Path string `yaml:"path"`
}

// S3SourceConfig for AWS S3 sources.
type S3SourceConfig struct {
	Bucket   string `yaml:"bucket"`
	Key      string `yaml:"key"`
	Region   string `yaml:"region"`
	RoleARN  string `yaml:"role_arn,omitempty"`
	Endpoint string `yaml:"endpoint,omitempty"` // For S3-compatible services
}

// AzureSourceConfig for Azure Blob Storage.
type AzureSourceConfig struct {
	AccountName   string `yaml:"account_name"`
	ContainerName string `yaml:"container_name"`
	BlobName      string `yaml:"blob_name"`
	// Auth can be connection string, managed identity, or SAS token
	ConnectionString   string `yaml:"connection_string,omitempty"`
	UseManagedIdentity bool   `yaml:"use_managed_identity,omitempty"`
}

// GitHubSourceConfig for GitHub repository sources.
type GitHubSourceConfig struct {
	Owner    string `yaml:"owner"`
	Repo     string `yaml:"repo"`
	Path     string `yaml:"path"`     // Path to config file in repo
	Ref      string `yaml:"ref"`      // Branch, tag, or commit
	Strategy string `yaml:"strategy"` // release, tag, branch, commit

	// GitHub App authentication (recommended)
	AppID          int64  `yaml:"app_id,omitempty"`
	InstallationID int64  `yaml:"installation_id,omitempty"`
	PrivateKeyPath string `yaml:"private_key_path,omitempty"`

	// Personal Access Token (alternative, less secure)
	Token string `yaml:"token,omitempty"`
}

// HTTPSourceConfig for generic HTTP/HTTPS sources.
type HTTPSourceConfig struct {
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers,omitempty"`
	Timeout time.Duration     `yaml:"timeout"`
	// Auth options
	BasicAuth   *BasicAuthConfig `yaml:"basic_auth,omitempty"`
	BearerToken string           `yaml:"bearer_token,omitempty"`
}

// BasicAuthConfig for HTTP basic authentication.
type BasicAuthConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// ParameterStoreConfig for AWS Systems Manager Parameter Store.
type ParameterStoreConfig struct {
	Name           string `yaml:"name"` // Parameter name
	Region         string `yaml:"region"`
	WithDecryption bool   `yaml:"with_decryption"`
}

// OutputConfig defines where to write the config.
type OutputConfig struct {
	// Type is the output type (file, api, both)
	Type string `yaml:"type"`

	// File output configuration
	File *FileOutputConfig `yaml:"file,omitempty"`

	// API output configuration (push to redirector)
	API *APIOutputConfig `yaml:"api,omitempty"`
}

// FileOutputConfig for writing config to a local file.
type FileOutputConfig struct {
	Path   string      `yaml:"path"`
	Mode   os.FileMode `yaml:"mode"`
	Atomic bool        `yaml:"atomic"` // Use atomic write (tmp + rename)
}

// APIOutputConfig for pushing config to the redirector API.
type APIOutputConfig struct {
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers,omitempty"`
	Timeout time.Duration     `yaml:"timeout"`
	APIKey  string            `yaml:"api_key,omitempty"`
}

// TargetConfig configures a redirector target for multi-target push.
type TargetConfig struct {
	Name    string        `yaml:"name"`
	URL     string        `yaml:"url"`
	APIKey  string        `yaml:"api_key,omitempty"`
	Timeout time.Duration `yaml:"timeout"`
	Healthy bool          `yaml:"-"`
}

// WebhookConfig configures the webhook server.
type WebhookConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	Secret  string `yaml:"secret"`
}

// RetryConfig configures retry behavior.
type RetryConfig struct {
	Attempts int           `yaml:"attempts"`
	Delay    time.Duration `yaml:"delay"`
}

// ANSI color codes for lint output.
const (
	colorReset   = "\033[0m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorBold    = "\033[1m"
)

func main() {
	// Flags
	configPath := flag.String("config", "syncer.yaml", "Path to syncer configuration")
	oneShot := flag.Bool("one-shot", false, "Run once and exit")
	dryRun := flag.Bool("dry-run", false, "Fetch config but don't write output")

	// Lint flags
	lintMode := flag.Bool("lint", false, "Lint mode: validate config and exit")
	lintJSON := flag.Bool("lint-json", false, "Output lint results as JSON")
	lintQuiet := flag.Bool("lint-quiet", false, "Only show lint errors (no warnings)")
	lintConfig := flag.String("lint-config", "", "Path to redirector config file to lint directly (no source fetching)")
	flag.Parse()

	// Setup logging
	zerolog.TimeFieldFormat = time.RFC3339
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"})

	// Lint mode: lint a local config file directly (no syncer config needed)
	if *lintMode && *lintConfig != "" {
		runSingleLint(*lintConfig, *lintJSON, *lintQuiet)
		return
	}

	// Load syncer config
	cfg, err := loadSyncerConfig(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load syncer config")
	}

	// Set log level
	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	// Lint mode: fetch all sources from syncer config, lint each + detect cross-source conflicts
	if *lintMode {
		runSyncerLint(cfg, *lintJSON, *lintQuiet)
		return
	}

	log.Info().
		Int("sources", len(cfg.Sources)).
		Str("interval", cfg.SyncInterval.String()).
		Bool("one_shot", *oneShot).
		Msg("redirector-sync starting")

	// Create syncer
	syncer := NewSyncer(cfg)

	// Setup context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Info().Str("signal", sig.String()).Msg("Received signal, shutting down")
		cancel()
	}()

	// Start webhook server if enabled
	if cfg.Webhook.Enabled && cfg.Webhook.Port > 0 {
		go startWebhookServer(ctx, cfg.Webhook.Port, cfg.Webhook.Secret, syncer)
	}

	// Run syncer
	if *oneShot {
		if err := syncer.SyncOnce(ctx, *dryRun); err != nil {
			log.Fatal().Err(err).Msg("Sync failed")
		}
		log.Info().Msg("Sync completed successfully")
		return
	}

	// Run sync loop
	syncer.Run(ctx, *dryRun)
}

// startWebhookServer starts the webhook HTTP server.
func startWebhookServer(ctx context.Context, port int, secret string, syncer *Syncer) {
	mux := http.NewServeMux()

	mux.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Log webhook event
		eventType := ""
		if gh := r.Header.Get("X-GitHub-Event"); gh != "" {
			eventType = "github:" + gh
		} else if gl := r.Header.Get("X-Gitlab-Event"); gl != "" {
			eventType = "gitlab:" + gl
		}

		log.Info().Str("event", eventType).Msg("Received webhook")

		// Trigger sync
		syncCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := syncer.SyncOnce(syncCtx, false); err != nil {
			log.Error().Err(err).Msg("Webhook-triggered sync failed")

			// Return lint issues in the response body when available
			var lintErr *LintError
			if errors.As(err, &lintErr) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status":  "lint_failed",
					"source":  lintErr.Source,
					"message": lintErr.Error(),
					"issues":  lintErr.Result.Issues,
				})
				return
			}

			http.Error(w, "Sync failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		status := syncer.GetStatus()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	// Prometheus metrics endpoint
	if syncer.promRegistry != nil {
		mux.Handle("/metrics", promhttp.HandlerFor(syncer.promRegistry, promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		}))
	}

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	log.Info().Int("port", port).Msg("Starting webhook server")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error().Err(err).Msg("Webhook server error")
	}
}

func loadSyncerConfig(path string) (*SyncerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	var cfg SyncerConfig
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Apply defaults
	if cfg.SyncInterval == 0 {
		cfg.SyncInterval = 5 * time.Minute
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.Retry.Attempts == 0 {
		cfg.Retry.Attempts = 3
	}
	if cfg.Retry.Delay == 0 {
		cfg.Retry.Delay = 1 * time.Second
	}
	for i := range cfg.Targets {
		if cfg.Targets[i].Timeout == 0 {
			cfg.Targets[i].Timeout = 10 * time.Second
		}
	}

	return &cfg, nil
}

// LintError is returned when a sync fails due to lint issues.
// It carries the full lint result so callers (like webhook handlers) can
// surface the actual issues to the user instead of a generic "sync failed".
type LintError struct {
	Source string
	Result *lint.Result
}

func (e *LintError) Error() string {
	return fmt.Sprintf("lint errors found in config from source %s (%d errors)",
		e.Source, len(e.Result.Errors()))
}

// Syncer handles config synchronization.
type Syncer struct {
	cfg          *SyncerConfig
	sources      []ConfigSource
	metrics      *metrics.SyncerMetrics
	promRegistry *prometheus.Registry

	mu           sync.RWMutex
	syncCount    int64
	syncErrors   int64
	lastSyncTime time.Time
}

// SyncerStatus represents the syncer's current state.
type SyncerStatus struct {
	SyncCount    int64          `json:"sync_count"`
	SyncErrors   int64          `json:"sync_errors"`
	LastSyncTime time.Time      `json:"last_sync_time"`
	Targets      []TargetStatus `json:"targets"`
}

// TargetStatus represents a target's health.
type TargetStatus struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Healthy bool   `json:"healthy"`
}

// GetStatus returns the current syncer status.
func (s *Syncer) GetStatus() SyncerStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	targets := make([]TargetStatus, len(s.cfg.Targets))
	for i, t := range s.cfg.Targets {
		targets[i] = TargetStatus{
			Name:    t.Name,
			URL:     t.URL,
			Healthy: t.Healthy,
		}
	}

	return SyncerStatus{
		SyncCount:    s.syncCount,
		SyncErrors:   s.syncErrors,
		LastSyncTime: s.lastSyncTime,
		Targets:      targets,
	}
}

// ConfigSource is the interface for config sources.
type ConfigSource interface {
	Name() string
	Priority() int
	Fetch(ctx context.Context) ([]byte, error)
	Validate(ctx context.Context) error
}

// NewSyncer creates a new syncer from configuration.
func NewSyncer(cfg *SyncerConfig) *Syncer {
	registry := prometheus.NewRegistry()
	syncerMetrics := metrics.NewSyncerMetrics(registry)

	s := &Syncer{cfg: cfg, metrics: syncerMetrics, promRegistry: registry}

	// Initialize sources
	for _, srcCfg := range cfg.Sources {
		if !srcCfg.Enabled {
			continue
		}

		src := s.createSource(srcCfg)
		if src != nil {
			s.sources = append(s.sources, src)
		}
	}

	// Sort by priority (descending)
	for i := 0; i < len(s.sources); i++ {
		for j := i + 1; j < len(s.sources); j++ {
			if s.sources[j].Priority() > s.sources[i].Priority() {
				s.sources[i], s.sources[j] = s.sources[j], s.sources[i]
			}
		}
	}

	return s
}

func (s *Syncer) createSource(cfg SourceConfig) ConfigSource {
	// Build the config map for the provider Registry
	configMap := sourceConfigToMap(cfg)
	if configMap == nil {
		log.Warn().Str("type", cfg.Type).Str("name", cfg.Name).Msg("No configuration for source type")
		return nil
	}

	log.Debug().Str("type", cfg.Type).Str("name", cfg.Name).Interface("config_map", redactSecrets(configMap)).Msg("Creating source from config map")

	// Map source type names to registry names where they differ
	registryType := cfg.Type
	switch cfg.Type {
	case "azure":
		registryType = "azureblob"
	case "parameter_store":
		registryType = "parameterstore"
	case "secrets_manager":
		registryType = "secretsmanager"
	}

	source, err := providers.Registry.Create(registryType, configMap)
	if err != nil {
		log.Error().Err(err).Str("type", cfg.Type).Str("name", cfg.Name).Msg("Failed to create source from Registry")
		return nil
	}

	log.Debug().Str("type", cfg.Type).Str("name", cfg.Name).Str("source_name", source.Name()).Msg("Source created successfully")

	return &providerAdapter{
		source:   source,
		name:     cfg.Name,
		priority: cfg.Priority,
	}
}

// redactSecrets returns a copy of the map with sensitive fields masked.
func redactSecrets(m map[string]interface{}) map[string]interface{} {
	redacted := make(map[string]interface{}, len(m))
	for k, v := range m {
		switch k {
		case "token", "bearer_token", "secret", "private_key", "connection_string":
			redacted[k] = "***"
		default:
			redacted[k] = v
		}
	}
	return redacted
}

// providerAdapter wraps a providers.Source to implement the cmd's ConfigSource interface.
// providers.Source.Fetch returns *config.Config; ConfigSource.Fetch returns []byte.
type providerAdapter struct {
	source   providers.Source
	name     string
	priority int
}

func (a *providerAdapter) Name() string  { return a.name }
func (a *providerAdapter) Priority() int { return a.priority }

func (a *providerAdapter) Fetch(ctx context.Context) ([]byte, error) {
	cfg, err := a.source.Fetch(ctx)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(cfg)
}

func (a *providerAdapter) Validate(ctx context.Context) error {
	return a.source.Validate(ctx)
}

// sourceConfigToMap converts a typed SourceConfig into a map[string]interface{}
// suitable for the provider Registry.
func sourceConfigToMap(cfg SourceConfig) map[string]interface{} {
	switch cfg.Type {
	case "file":
		if cfg.File == nil {
			return nil
		}
		return map[string]interface{}{
			"path": cfg.File.Path,
		}
	case "s3":
		if cfg.S3 == nil {
			return nil
		}
		m := map[string]interface{}{
			"bucket": cfg.S3.Bucket,
			"key":    cfg.S3.Key,
		}
		if cfg.S3.Region != "" {
			m["region"] = cfg.S3.Region
		}
		if cfg.S3.RoleARN != "" {
			m["role_arn"] = cfg.S3.RoleARN
		}
		if cfg.S3.Endpoint != "" {
			m["endpoint"] = cfg.S3.Endpoint
		}
		return m
	case "azure":
		if cfg.Azure == nil {
			return nil
		}
		m := map[string]interface{}{
			"container": cfg.Azure.ContainerName,
			"blob_name": cfg.Azure.BlobName,
		}
		if cfg.Azure.AccountName != "" {
			m["storage_account"] = cfg.Azure.AccountName
		}
		if cfg.Azure.ConnectionString != "" {
			m["connection_string"] = cfg.Azure.ConnectionString
		}
		if cfg.Azure.UseManagedIdentity {
			m["use_default_credential"] = true
		}
		return m
	case "github":
		if cfg.GitHub == nil {
			return nil
		}
		m := map[string]interface{}{
			"repository": cfg.GitHub.Owner + "/" + cfg.GitHub.Repo,
		}
		if cfg.GitHub.Path != "" {
			m["path"] = cfg.GitHub.Path
		}
		if cfg.GitHub.Strategy != "" {
			m["strategy"] = cfg.GitHub.Strategy
		}
		if cfg.GitHub.Ref != "" {
			m["environment"] = cfg.GitHub.Ref
			// Default to branch strategy when ref is set but strategy isn't
			if cfg.GitHub.Strategy == "" {
				m["strategy"] = "branch"
			}
		}
		if cfg.GitHub.Token != "" {
			m["token"] = cfg.GitHub.Token
		}
		if cfg.GitHub.AppID != 0 {
			m["app"] = map[string]interface{}{
				"app_id":           float64(cfg.GitHub.AppID),
				"installation_id":  float64(cfg.GitHub.InstallationID),
				"private_key_path": cfg.GitHub.PrivateKeyPath,
			}
		}
		return m
	case "http":
		if cfg.HTTP == nil {
			return nil
		}
		m := map[string]interface{}{
			"url": cfg.HTTP.URL,
		}
		if cfg.HTTP.Headers != nil {
			m["headers"] = cfg.HTTP.Headers
		}
		if cfg.HTTP.Timeout != 0 {
			m["timeout"] = cfg.HTTP.Timeout.String()
		}
		if cfg.HTTP.BearerToken != "" {
			m["bearer_token"] = cfg.HTTP.BearerToken
		}
		return m
	case "parameter_store", "parameterstore":
		if cfg.ParameterStore == nil {
			return nil
		}
		m := map[string]interface{}{
			"name": cfg.ParameterStore.Name,
		}
		if cfg.ParameterStore.Region != "" {
			m["region"] = cfg.ParameterStore.Region
		}
		if cfg.ParameterStore.WithDecryption {
			m["with_decryption"] = true
		}
		return m
	default:
		return nil
	}
}

// SyncOnce performs a single sync operation.
func (s *Syncer) SyncOnce(ctx context.Context, dryRun bool) error {
	syncStart := time.Now()

	// Try sources in priority order
	var lastErr error
	for _, src := range s.sources {
		log.Info().Str("source", src.Name()).Msg("Attempting to fetch config")

		fetchStart := time.Now()
		data, err := src.Fetch(ctx)
		fetchDuration := time.Since(fetchStart).Seconds()

		if err != nil {
			log.Warn().Err(err).Str("source", src.Name()).Msg("Failed to fetch from source")
			if s.metrics != nil {
				s.metrics.RecordFetch(src.Name(), false, fetchDuration)
			}
			lastErr = err
			continue
		}

		if s.metrics != nil {
			s.metrics.RecordFetch(src.Name(), true, fetchDuration)
		}

		log.Info().
			Str("source", src.Name()).
			Int("bytes", len(data)).
			Msg("Successfully fetched config")

		// Lint the fetched config before writing
		parsedCfg, parseErr := config.ParseBytes(data)
		if parseErr != nil {
			log.Error().Err(parseErr).Str("source", src.Name()).Msg("Failed to parse fetched config")
			lastErr = parseErr
			continue
		}

		if s.metrics != nil {
			s.metrics.RulesFetched.Set(float64(len(parsedCfg.Rules)))
		}

		linter := lint.New(parsedCfg)
		lintResult := linter.Lint()

		if lintResult.HasErrors() {
			for _, issue := range lintResult.Errors() {
				log.Error().Str("rule_id", issue.RuleID).Str("source", src.Name()).Msg(issue.Message)
			}
			if s.metrics != nil {
				s.metrics.RecordLintError(src.Name())
				s.metrics.RecordSync(false, time.Since(syncStart).Seconds())
			}
			s.mu.Lock()
			s.syncErrors++
			s.mu.Unlock()
			return &LintError{Source: src.Name(), Result: lintResult}
		}

		for _, issue := range lintResult.Warnings() {
			log.Warn().Str("rule_id", issue.RuleID).Str("source", src.Name()).Msg(issue.Message)
		}

		if dryRun {
			log.Info().Msg("Dry run - not writing output")
			if s.metrics != nil {
				s.metrics.RecordSync(true, time.Since(syncStart).Seconds())
			}
			return nil
		}

		// Write output
		if err := s.writeOutput(ctx, data); err != nil {
			if s.metrics != nil {
				s.metrics.RecordSync(false, time.Since(syncStart).Seconds())
			}
			s.mu.Lock()
			s.syncErrors++
			s.mu.Unlock()
			return fmt.Errorf("writing output: %w", err)
		}

		s.mu.Lock()
		s.syncCount++
		s.lastSyncTime = time.Now()
		s.mu.Unlock()

		if s.metrics != nil {
			s.metrics.RecordSync(true, time.Since(syncStart).Seconds())
		}
		return nil
	}

	if s.metrics != nil {
		s.metrics.RecordSync(false, time.Since(syncStart).Seconds())
	}

	s.mu.Lock()
	s.syncErrors++
	s.mu.Unlock()

	if lastErr != nil {
		return fmt.Errorf("all sources failed, last error: %w", lastErr)
	}
	return fmt.Errorf("no sources configured")
}

// Run starts the sync loop.
func (s *Syncer) Run(ctx context.Context, dryRun bool) {
	// Initial sync
	if err := s.SyncOnce(ctx, dryRun); err != nil {
		log.Error().Err(err).Msg("Initial sync failed")
	}

	ticker := time.NewTicker(s.cfg.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Syncer shutting down")
			return
		case <-ticker.C:
			if err := s.SyncOnce(ctx, dryRun); err != nil {
				log.Error().Err(err).Msg("Sync failed")
			}
		}
	}
}

func (s *Syncer) writeOutput(ctx context.Context, data []byte) error {
	switch s.cfg.Output.Type {
	case "file":
		return s.writeFileOutput(data)
	case "api":
		return s.writeAPIOutput(ctx, data)
	case "both":
		if err := s.writeFileOutput(data); err != nil {
			return err
		}
		return s.writeAPIOutput(ctx, data)
	default:
		return fmt.Errorf("unknown output type: %s", s.cfg.Output.Type)
	}
}

func (s *Syncer) writeFileOutput(data []byte) error {
	if s.cfg.Output.File == nil {
		return fmt.Errorf("file output not configured")
	}

	path := s.cfg.Output.File.Path
	mode := s.cfg.Output.File.Mode
	if mode == 0 {
		mode = 0644
	}

	if s.cfg.Output.File.Atomic {
		// Atomic write: write to temp file, then rename
		tmpPath := path + ".tmp"
		if err := os.WriteFile(tmpPath, data, mode); err != nil {
			return fmt.Errorf("writing temp file: %w", err)
		}
		if err := os.Rename(tmpPath, path); err != nil {
			_ = os.Remove(tmpPath) // Clean up on failure
			return fmt.Errorf("renaming temp file: %w", err)
		}
	} else {
		if err := os.WriteFile(path, data, mode); err != nil {
			return fmt.Errorf("writing file: %w", err)
		}
	}

	log.Info().Str("path", path).Msg("Wrote config to file")
	return nil
}

func (s *Syncer) writeAPIOutput(ctx context.Context, data []byte) error {
	// If targets are configured, use multi-target push
	if len(s.cfg.Targets) > 0 {
		return s.pushToTargets(ctx, data)
	}

	// Fall back to single API output
	if s.cfg.Output.API == nil {
		return fmt.Errorf("API output not configured")
	}

	return s.pushToSingleAPI(ctx, s.cfg.Output.API, data)
}

// pushToTargets pushes config to all targets concurrently.
func (s *Syncer) pushToTargets(ctx context.Context, data []byte) error {
	var wg sync.WaitGroup
	errors := make(chan error, len(s.cfg.Targets))

	for i := range s.cfg.Targets {
		target := &s.cfg.Targets[i]
		wg.Add(1)
		go func(t *TargetConfig) {
			defer wg.Done()
			if err := s.pushToTargetWithRetry(ctx, t, data); err != nil {
				errors <- fmt.Errorf("target %s: %w", t.Name, err)
				t.Healthy = false
			} else {
				t.Healthy = true
				log.Info().Str("target", t.Name).Msg("Config pushed successfully")
			}
		}(target)
	}

	wg.Wait()
	close(errors)

	// Collect errors
	var errs []error
	for err := range errors {
		errs = append(errs, err)
		log.Error().Err(err).Msg("Target push failed")
	}

	if len(errs) > 0 {
		s.mu.Lock()
		s.syncErrors += int64(len(errs))
		s.mu.Unlock()
		return fmt.Errorf("%d targets failed", len(errs))
	}

	return nil
}

// pushToTargetWithRetry pushes to a single target with exponential backoff.
func (s *Syncer) pushToTargetWithRetry(ctx context.Context, target *TargetConfig, data []byte) error {
	attempts := s.cfg.Retry.Attempts
	if attempts == 0 {
		attempts = 3
	}
	delay := s.cfg.Retry.Delay
	if delay == 0 {
		delay = 1 * time.Second
	}

	pushStart := time.Now()
	var lastErr error
	for attempt := 0; attempt <= attempts; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := delay * time.Duration(1<<(attempt-1))
			log.Debug().
				Str("target", target.Name).
				Int("attempt", attempt).
				Dur("delay", backoff).
				Msg("Retrying push")

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		err := s.doPush(ctx, target, data)
		if err == nil {
			if s.metrics != nil {
				s.metrics.RecordPush(target.Name, true, time.Since(pushStart).Seconds())
			}
			return nil
		}
		lastErr = err
		log.Warn().Err(err).Str("target", target.Name).Int("attempt", attempt).Msg("Push attempt failed")
	}

	if s.metrics != nil {
		s.metrics.RecordPush(target.Name, false, time.Since(pushStart).Seconds())
	}
	return lastErr
}

// doPush performs the actual HTTP request.
func (s *Syncer) doPush(ctx context.Context, target *TargetConfig, data []byte) error {
	url := target.URL + "/api/v1/reload"

	timeout := target.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	if target.APIKey != "" {
		req.Header.Set("X-API-Key", target.APIKey)
	}
	req.Header.Set("Content-Type", "application/yaml")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// pushToSingleAPI pushes to a single API endpoint (legacy mode).
func (s *Syncer) pushToSingleAPI(ctx context.Context, cfg *APIOutputConfig, data []byte) error {
	url := cfg.URL + "/api/v1/reload"

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	if cfg.APIKey != "" {
		req.Header.Set("X-API-Key", cfg.APIKey)
	}
	req.Header.Set("Content-Type", "application/yaml")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("HTTP %d (failed to read body: %v)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	log.Info().Str("url", cfg.URL).Msg("Config pushed to API")
	return nil
}

// --- Lint mode functions (ported from cmd/redirector-lint) ---

func runSingleLint(configPath string, jsonOut, quiet bool) {
	cfg, err := config.LoadDirectory(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s Failed to load config: %v\n", colorRed, colorReset, err)
		os.Exit(1)
	}

	linter := lint.New(cfg)
	result := linter.Lint()

	if jsonOut {
		outputJSON(result)
	} else {
		outputText(result, quiet)
	}

	if result.HasErrors() {
		os.Exit(1)
	}
}

// runSyncerLint fetches all sources defined in the syncer config, lints each,
// and runs cross-source conflict detection when multiple sources exist.
func runSyncerLint(cfg *SyncerConfig, jsonOut, quiet bool) {
	syncer := NewSyncer(cfg)
	ctx := context.Background()

	log.Info().Int("sources", len(syncer.sources)).Msg("Lint mode: fetching all sources")

	type fetchedSource struct {
		name     string
		prefix   string
		priority int
		config   *config.Config
	}

	var fetched []fetchedSource
	for i, src := range syncer.sources {
		log.Info().Str("source", src.Name()).Msg("Fetching source for lint")

		data, err := src.Fetch(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s Failed to fetch source '%s': %v\n", colorRed, colorReset, src.Name(), err)
			os.Exit(1)
		}

		parsedCfg, err := config.ParseBytes(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s Failed to parse config from source '%s': %v\n", colorRed, colorReset, src.Name(), err)
			os.Exit(1)
		}

		// Look up prefix from the original SourceConfig
		prefix := ""
		for _, srcCfg := range cfg.Sources {
			if srcCfg.Name == src.Name() {
				prefix = srcCfg.Prefix
				break
			}
		}

		fetched = append(fetched, fetchedSource{
			name:     src.Name(),
			prefix:   prefix,
			priority: src.Priority(),
			config:   parsedCfg,
		})

		log.Info().
			Str("source", src.Name()).
			Int("rules", len(parsedCfg.Rules)).
			Int("index", i).
			Msg("Source fetched and parsed")
	}

	if len(fetched) == 0 {
		fmt.Fprintf(os.Stderr, "%sError:%s No sources configured or enabled\n", colorRed, colorReset)
		os.Exit(1)
	}

	// Single source: simple lint
	if len(fetched) == 1 {
		linter := lint.New(fetched[0].config)
		result := linter.Lint()
		if jsonOut {
			outputJSON(result)
		} else {
			outputText(result, quiet)
		}
		if result.HasErrors() {
			os.Exit(1)
		}
		return
	}

	// Multiple sources: multi-source lint with conflict detection
	sources := make([]lint.SourceInput, len(fetched))
	for i, f := range fetched {
		sources[i] = lint.SourceInput{
			Name:     f.name,
			Prefix:   f.prefix,
			Priority: f.priority,
			Config:   f.config,
		}
	}

	linter := lint.NewMultiSource(sources)
	result := linter.Lint()

	if jsonOut {
		outputMultiSourceJSON(result)
	} else {
		outputMultiSourceText(result, quiet)
	}

	if result.HasErrors() {
		os.Exit(1)
	}
}

func outputJSON(result *lint.Result) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.Encode(result)
}

func outputText(result *lint.Result, quiet bool) {
	fmt.Printf("%s%sThe Redirector - Config Linter%s\n", colorBold, colorBlue, colorReset)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("Loaded %d rules\n\n", result.RulesCount)

	errors := result.Errors()
	warnings := result.Warnings()
	var infos []lint.Issue
	for _, issue := range result.Issues {
		if issue.Severity == lint.SeverityInfo {
			infos = append(infos, issue)
		}
	}

	if len(errors) > 0 {
		fmt.Printf("%s%s✗ ERRORS (%d)%s\n", colorBold, colorRed, len(errors), colorReset)
		fmt.Println("─────────────────────────────────────────────────────────────────")
		for _, issue := range errors {
			printIssue(issue)
		}
		fmt.Println()
	}

	if len(warnings) > 0 && !quiet {
		fmt.Printf("%s%s⚠ WARNINGS (%d)%s\n", colorBold, colorYellow, len(warnings), colorReset)
		fmt.Println("─────────────────────────────────────────────────────────────────")
		for _, issue := range warnings {
			printIssue(issue)
		}
		fmt.Println()
	}

	if len(infos) > 0 && !quiet {
		fmt.Printf("%s%sℹ SUGGESTIONS (%d)%s\n", colorBold, colorBlue, len(infos), colorReset)
		fmt.Println("─────────────────────────────────────────────────────────────────")
		for _, issue := range infos {
			printIssue(issue)
		}
		fmt.Println()
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	if len(errors) == 0 && len(warnings) == 0 {
		fmt.Printf("%s%s✓ No issues found!%s\n", colorBold, colorGreen, colorReset)
	} else {
		fmt.Printf("Found: %s%d errors%s, %s%d warnings%s, %d suggestions\n",
			colorRed, len(errors), colorReset,
			colorYellow, len(warnings), colorReset,
			len(infos))
	}
}

func outputMultiSourceJSON(result *lint.MultiSourceResult) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.Encode(result)
}

func outputMultiSourceText(result *lint.MultiSourceResult, quiet bool) {
	fmt.Printf("%s%sThe Redirector - Multi-Team Config Linter%s\n", colorBold, colorBlue, colorReset)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	fmt.Printf("Sources: %d | Total Rules: %d\n", len(result.Sources), result.TotalRules)
	for _, src := range result.Sources {
		fmt.Printf("  • %s%s%s: %d rules\n", colorCyan, src, colorReset, result.RulesPerSource[src])
	}
	fmt.Println()

	if len(result.Conflicts) > 0 {
		fmt.Printf("%s%s⚠ TEAM CONFLICTS (%d)%s\n", colorBold, colorRed, len(result.Conflicts), colorReset)
		fmt.Println("─────────────────────────────────────────────────────────────────")
		fmt.Println("These rules from different teams may conflict at runtime:")
		fmt.Println()

		for i, conflict := range result.Conflicts {
			fmt.Printf("  %s%d. %s%s\n", colorYellow, i+1, conflict.Description, colorReset)
			fmt.Printf("     Path: %s%s%s\n", colorMagenta, conflict.Path, colorReset)
			fmt.Printf("     Teams: %s\n", strings.Join(conflict.Sources, " vs "))
			fmt.Printf("     Rules: %s\n", strings.Join(conflict.RuleIDs, ", "))
			fmt.Printf("     Type: %s\n", conflict.MatchType)
			fmt.Println()
		}
	}

	errors := make([]lint.Issue, 0)
	warnings := make([]lint.Issue, 0)
	for _, issue := range result.Issues {
		switch issue.Severity {
		case lint.SeverityError:
			errors = append(errors, issue)
		case lint.SeverityWarning:
			warnings = append(warnings, issue)
		}
	}

	if len(errors) > 0 {
		fmt.Printf("%s%s✗ ERRORS (%d)%s\n", colorBold, colorRed, len(errors), colorReset)
		fmt.Println("─────────────────────────────────────────────────────────────────")
		for _, issue := range errors {
			printIssue(issue)
		}
		fmt.Println()
	}

	if len(warnings) > 0 && !quiet {
		fmt.Printf("%s%s⚠ WARNINGS (%d)%s\n", colorBold, colorYellow, len(warnings), colorReset)
		fmt.Println("─────────────────────────────────────────────────────────────────")
		for _, issue := range warnings {
			printIssue(issue)
		}
		fmt.Println()
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	if len(result.Conflicts) == 0 && len(errors) == 0 && len(warnings) == 0 {
		fmt.Printf("%s%s✓ No conflicts or issues found between teams!%s\n", colorBold, colorGreen, colorReset)
	} else {
		fmt.Printf("Found: %s%d conflicts%s, %s%d errors%s, %s%d warnings%s\n",
			colorRed, len(result.Conflicts), colorReset,
			colorRed, len(errors), colorReset,
			colorYellow, len(warnings), colorReset)

		if len(result.Conflicts) > 0 {
			fmt.Printf("\n%sRecommendation:%s Teams should coordinate on conflicting paths or use\n", colorBold, colorReset)
			fmt.Printf("different path prefixes to avoid runtime conflicts.\n")
		}
	}
}

func printIssue(issue lint.Issue) {
	var color string
	switch issue.Severity {
	case lint.SeverityError:
		color = colorRed
	case lint.SeverityWarning:
		color = colorYellow
	case lint.SeverityInfo:
		color = colorBlue
	}

	ruleInfo := ""
	if issue.RuleID != "" {
		ruleInfo = fmt.Sprintf("[%s] ", issue.RuleID)
	}

	fmt.Printf("  %s%s%s%s\n", color, ruleInfo, issue.Message, colorReset)

	if issue.Suggestion != "" {
		fmt.Printf("    %s→ %s%s\n", colorGreen, issue.Suggestion, colorReset)
	}
}
