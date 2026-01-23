// Package syncer coordinates configuration synchronization between sources and targets.
package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/jamengual/the-redirector/internal/config"
	"github.com/jamengual/the-redirector/internal/providers"
)

// TargetConfig configures a redirector target.
type TargetConfig struct {
	// Name is a friendly identifier for this target
	Name string `yaml:"name" json:"name"`

	// URL is the management API endpoint (e.g., "http://redirector:8081")
	URL string `yaml:"url" json:"url"`

	// APIKey for authentication (optional)
	APIKey string `yaml:"api_key" json:"api_key"`

	// Timeout for requests to this target
	Timeout time.Duration `yaml:"timeout" json:"timeout"`

	// Healthy tracks if this target is reachable
	Healthy bool `yaml:"-" json:"-"`
}

// Config configures the syncer service.
type Config struct {
	// Sources to fetch configuration from
	Sources []SourceConfig `yaml:"sources" json:"sources"`

	// Targets to push configuration to
	Targets []TargetConfig `yaml:"targets" json:"targets"`

	// SyncInterval is how often to sync (if not using watch)
	SyncInterval time.Duration `yaml:"sync_interval" json:"sync_interval"`

	// RetryAttempts on failed pushes
	RetryAttempts int `yaml:"retry_attempts" json:"retry_attempts"`

	// RetryDelay between retries (exponential backoff applied)
	RetryDelay time.Duration `yaml:"retry_delay" json:"retry_delay"`

	// WebhookPort for receiving GitHub/GitLab webhooks (0 = disabled)
	WebhookPort int `yaml:"webhook_port" json:"webhook_port"`

	// WebhookSecret for validating webhook payloads
	WebhookSecret string `yaml:"webhook_secret" json:"webhook_secret"`
}

// SourceConfig configures a configuration source.
type SourceConfig struct {
	// Type is the source type (file, s3, github)
	Type string `yaml:"type" json:"type"`

	// Priority determines merge order (higher = later, overrides)
	Priority int `yaml:"priority" json:"priority"`

	// Config contains source-specific configuration
	Config map[string]interface{} `yaml:"config" json:"config"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		SyncInterval:  60 * time.Second,
		RetryAttempts: 3,
		RetryDelay:    1 * time.Second,
	}
}

// Syncer coordinates configuration synchronization.
type Syncer struct {
	cfg     *Config
	sources []providers.Source
	client  *http.Client

	currentConfig *config.Config
	mu            sync.RWMutex

	// Metrics
	syncCount    int64
	syncErrors   int64
	lastSyncTime time.Time
}

// New creates a new Syncer instance.
func New(cfg *Config) (*Syncer, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	s := &Syncer{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	// Initialize sources
	for _, srcCfg := range cfg.Sources {
		source, err := s.createSource(srcCfg)
		if err != nil {
			return nil, fmt.Errorf("creating source %s: %w", srcCfg.Type, err)
		}
		s.sources = append(s.sources, source)
	}

	// Set default timeout for targets
	for i := range cfg.Targets {
		if cfg.Targets[i].Timeout == 0 {
			cfg.Targets[i].Timeout = 10 * time.Second
		}
	}

	return s, nil
}

// createSource instantiates a source from configuration.
func (s *Syncer) createSource(cfg SourceConfig) (providers.Source, error) {
	factory, ok := providers.Registry.Get(cfg.Type)
	if !ok {
		return nil, fmt.Errorf("unknown source type: %s", cfg.Type)
	}
	return factory(cfg.Config)
}

// Start begins the synchronization loop.
func (s *Syncer) Start(ctx context.Context) error {
	log.Info().
		Int("sources", len(s.sources)).
		Int("targets", len(s.cfg.Targets)).
		Msg("Starting config syncer")

	// Validate all sources
	for _, source := range s.sources {
		if err := source.Validate(ctx); err != nil {
			log.Warn().Err(err).Str("source", source.Name()).Msg("Source validation failed")
		}
	}

	// Initial sync
	if err := s.Sync(ctx); err != nil {
		log.Error().Err(err).Msg("Initial sync failed")
	}

	// Start watching sources that support it
	for _, source := range s.sources {
		if watcher, ok := source.(interface {
			Watch(context.Context) (<-chan *config.Config, error)
			SupportsWatch() bool
		}); ok && watcher.SupportsWatch() {
			go s.watchSource(ctx, source, watcher)
		}
	}

	// Periodic sync loop
	ticker := time.NewTicker(s.cfg.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Syncer shutting down")
			return s.Close()
		case <-ticker.C:
			if err := s.Sync(ctx); err != nil {
				log.Error().Err(err).Msg("Periodic sync failed")
			}
		}
	}
}

// watchSource monitors a source for changes.
func (s *Syncer) watchSource(ctx context.Context, source providers.Source, watcher interface {
	Watch(context.Context) (<-chan *config.Config, error)
}) {
	updates, err := watcher.Watch(ctx)
	if err != nil {
		log.Error().Err(err).Str("source", source.Name()).Msg("Failed to start watching source")
		return
	}

	log.Info().Str("source", source.Name()).Msg("Watching source for changes")

	for {
		select {
		case <-ctx.Done():
			return
		case cfg, ok := <-updates:
			if !ok {
				return
			}
			log.Info().Str("source", source.Name()).Msg("Source change detected")
			if err := s.pushConfig(ctx, cfg); err != nil {
				log.Error().Err(err).Msg("Failed to push config after source change")
			}
		}
	}
}

// Sync fetches from all sources and pushes to all targets.
func (s *Syncer) Sync(ctx context.Context) error {
	log.Debug().Msg("Starting sync")

	// Fetch from all sources
	cfg, err := s.fetchAndMerge(ctx)
	if err != nil {
		s.mu.Lock()
		s.syncErrors++
		s.mu.Unlock()
		return fmt.Errorf("fetching config: %w", err)
	}

	// Push to all targets
	if err := s.pushConfig(ctx, cfg); err != nil {
		return err
	}

	s.mu.Lock()
	s.currentConfig = cfg
	s.syncCount++
	s.lastSyncTime = time.Now()
	s.mu.Unlock()

	return nil
}

// fetchAndMerge fetches from all sources and merges them.
func (s *Syncer) fetchAndMerge(ctx context.Context) (*config.Config, error) {
	if len(s.sources) == 0 {
		return nil, fmt.Errorf("no sources configured")
	}

	// For now, use first source (TODO: implement priority-based merging)
	cfg, err := s.sources[0].Fetch(ctx)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// pushConfig sends configuration to all targets.
func (s *Syncer) pushConfig(ctx context.Context, cfg *config.Config) error {
	var wg sync.WaitGroup
	errors := make(chan error, len(s.cfg.Targets))

	for i := range s.cfg.Targets {
		target := &s.cfg.Targets[i]
		wg.Add(1)
		go func(t *TargetConfig) {
			defer wg.Done()
			if err := s.pushToTarget(ctx, t, cfg); err != nil {
				errors <- fmt.Errorf("target %s: %w", t.Name, err)
				t.Healthy = false
			} else {
				t.Healthy = true
			}
		}(target)
	}

	wg.Wait()
	close(errors)

	// Collect errors
	var errs []error
	for err := range errors {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		s.mu.Lock()
		s.syncErrors += int64(len(errs))
		s.mu.Unlock()
		return fmt.Errorf("%d targets failed: %v", len(errs), errs)
	}

	return nil
}

// pushToTarget sends configuration to a single target with retries.
func (s *Syncer) pushToTarget(ctx context.Context, target *TargetConfig, cfg *config.Config) error {
	var lastErr error

	for attempt := 0; attempt <= s.cfg.RetryAttempts; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			delay := s.cfg.RetryDelay * time.Duration(1<<(attempt-1))
			log.Debug().
				Str("target", target.Name).
				Int("attempt", attempt).
				Dur("delay", delay).
				Msg("Retrying push")

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		err := s.doPush(ctx, target, cfg)
		if err == nil {
			log.Info().
				Str("target", target.Name).
				Int("rules", len(cfg.Rules)).
				Msg("Config pushed successfully")
			return nil
		}
		lastErr = err
		log.Warn().Err(err).Str("target", target.Name).Int("attempt", attempt).Msg("Push failed")
	}

	return lastErr
}

// doPush performs the actual HTTP request to push config.
func (s *Syncer) doPush(ctx context.Context, target *TargetConfig, cfg *config.Config) error {
	// First, trigger a reload via the API
	url := target.URL + "/api/v1/reload"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	if target.APIKey != "" {
		req.Header.Set("X-API-Key", target.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: target.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// TriggerSync manually triggers a sync.
func (s *Syncer) TriggerSync(ctx context.Context) error {
	return s.Sync(ctx)
}

// GetStatus returns the current syncer status.
func (s *Syncer) GetStatus() Status {
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

	return Status{
		SyncCount:    s.syncCount,
		SyncErrors:   s.syncErrors,
		LastSyncTime: s.lastSyncTime,
		Targets:      targets,
	}
}

// Status represents the syncer's current state.
type Status struct {
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

// Close releases resources.
func (s *Syncer) Close() error {
	for _, source := range s.sources {
		if err := source.Close(); err != nil {
			log.Warn().Err(err).Str("source", source.Name()).Msg("Error closing source")
		}
	}
	return nil
}

// WebhookHandler handles incoming webhooks from GitHub/GitLab.
type WebhookHandler struct {
	syncer *Syncer
	secret string
}

// NewWebhookHandler creates a webhook handler.
func NewWebhookHandler(syncer *Syncer, secret string) *WebhookHandler {
	return &WebhookHandler{
		syncer: syncer,
		secret: secret,
	}
}

// ServeHTTP handles webhook requests.
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Detect webhook type
	eventType := ""
	if gh := r.Header.Get("X-GitHub-Event"); gh != "" {
		eventType = "github:" + gh
	} else if gl := r.Header.Get("X-Gitlab-Event"); gl != "" {
		eventType = "gitlab:" + gl
	}

	log.Info().
		Str("event", eventType).
		Int("body_size", len(body)).
		Msg("Received webhook")

	// Trigger sync
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := h.syncer.TriggerSync(ctx); err != nil {
		log.Error().Err(err).Msg("Webhook-triggered sync failed")
		http.Error(w, "Sync failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// StartWebhookServer starts the webhook HTTP server.
func StartWebhookServer(ctx context.Context, port int, handler http.Handler) error {
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: handler,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	log.Info().Int("port", port).Msg("Starting webhook server")
	return server.ListenAndServe()
}

// PushConfigDirectly pushes raw config to a target (for external use).
func (s *Syncer) PushConfigDirectly(ctx context.Context, target *TargetConfig, configData []byte) error {
	url := target.URL + "/api/v1/config"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(configData))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	if target.APIKey != "" {
		req.Header.Set("X-API-Key", target.APIKey)
	}
	req.Header.Set("Content-Type", "application/yaml")

	client := &http.Client{Timeout: target.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
