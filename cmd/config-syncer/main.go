package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

// SyncerConfig configures the config syncer service.
type SyncerConfig struct {
	// SyncInterval is how often to check for config updates
	SyncInterval time.Duration `yaml:"sync_interval"`

	// Sources defines the config sources in priority order (failover)
	Sources []SourceConfig `yaml:"sources"`

	// Output defines where to write the fetched config
	Output OutputConfig `yaml:"output"`

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

	// Enabled allows disabling a source without removing it
	Enabled bool `yaml:"enabled"`

	// Source-specific configuration
	File            *FileSourceConfig       `yaml:"file,omitempty"`
	S3              *S3SourceConfig         `yaml:"s3,omitempty"`
	Azure           *AzureSourceConfig      `yaml:"azure,omitempty"`
	GitHub          *GitHubSourceConfig     `yaml:"github,omitempty"`
	HTTP            *HTTPSourceConfig       `yaml:"http,omitempty"`
	ParameterStore  *ParameterStoreConfig   `yaml:"parameter_store,omitempty"`
}

// FileSourceConfig for local file sources.
type FileSourceConfig struct {
	Path string `yaml:"path"`
}

// S3SourceConfig for AWS S3 sources.
type S3SourceConfig struct {
	Bucket    string `yaml:"bucket"`
	Key       string `yaml:"key"`
	Region    string `yaml:"region"`
	RoleARN   string `yaml:"role_arn,omitempty"`
	Endpoint  string `yaml:"endpoint,omitempty"` // For S3-compatible services
}

// AzureSourceConfig for Azure Blob Storage.
type AzureSourceConfig struct {
	AccountName   string `yaml:"account_name"`
	ContainerName string `yaml:"container_name"`
	BlobName      string `yaml:"blob_name"`
	// Auth can be connection string, managed identity, or SAS token
	ConnectionString string `yaml:"connection_string,omitempty"`
	UseManagedIdentity bool `yaml:"use_managed_identity,omitempty"`
}

// GitHubSourceConfig for GitHub repository sources.
type GitHubSourceConfig struct {
	Owner      string `yaml:"owner"`
	Repo       string `yaml:"repo"`
	Path       string `yaml:"path"`          // Path to config file in repo
	Ref        string `yaml:"ref"`           // Branch, tag, or commit
	Strategy   string `yaml:"strategy"`      // release, tag, branch, commit

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
	BasicAuth *BasicAuthConfig `yaml:"basic_auth,omitempty"`
	BearerToken string         `yaml:"bearer_token,omitempty"`
}

// BasicAuthConfig for HTTP basic authentication.
type BasicAuthConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// ParameterStoreConfig for AWS Systems Manager Parameter Store.
type ParameterStoreConfig struct {
	Name      string `yaml:"name"`       // Parameter name
	Region    string `yaml:"region"`
	WithDecryption bool `yaml:"with_decryption"`
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
	Path     string      `yaml:"path"`
	Mode     os.FileMode `yaml:"mode"`
	Atomic   bool        `yaml:"atomic"` // Use atomic write (tmp + rename)
}

// APIOutputConfig for pushing config to the redirector API.
type APIOutputConfig struct {
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers,omitempty"`
	Timeout time.Duration     `yaml:"timeout"`
}

func main() {
	// Flags
	configPath := flag.String("config", "syncer.yaml", "Path to syncer configuration")
	oneShot := flag.Bool("one-shot", false, "Run once and exit")
	dryRun := flag.Bool("dry-run", false, "Fetch config but don't write output")
	flag.Parse()

	// Setup logging
	zerolog.TimeFieldFormat = time.RFC3339
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"})

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

	log.Info().
		Int("sources", len(cfg.Sources)).
		Str("interval", cfg.SyncInterval.String()).
		Bool("one_shot", *oneShot).
		Msg("Config syncer starting")

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

	return &cfg, nil
}

// Syncer handles config synchronization.
type Syncer struct {
	cfg     *SyncerConfig
	sources []ConfigSource
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
	s := &Syncer{cfg: cfg}

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
	switch cfg.Type {
	case "file":
		if cfg.File != nil {
			return &fileSource{
				name:     cfg.Name,
				priority: cfg.Priority,
				path:     cfg.File.Path,
			}
		}
	case "http":
		if cfg.HTTP != nil {
			return &httpSource{
				name:     cfg.Name,
				priority: cfg.Priority,
				url:      cfg.HTTP.URL,
				headers:  cfg.HTTP.Headers,
				timeout:  cfg.HTTP.Timeout,
			}
		}
	// Additional sources (S3, Azure, GitHub) would be implemented here
	// They are stubbed for now - see internal/providers for full implementations
	default:
		log.Warn().Str("type", cfg.Type).Msg("Unknown source type")
	}
	return nil
}

// SyncOnce performs a single sync operation.
func (s *Syncer) SyncOnce(ctx context.Context, dryRun bool) error {
	// Try sources in priority order
	var lastErr error
	for _, src := range s.sources {
		log.Info().Str("source", src.Name()).Msg("Attempting to fetch config")

		data, err := src.Fetch(ctx)
		if err != nil {
			log.Warn().Err(err).Str("source", src.Name()).Msg("Failed to fetch from source")
			lastErr = err
			continue
		}

		log.Info().
			Str("source", src.Name()).
			Int("bytes", len(data)).
			Msg("Successfully fetched config")

		if dryRun {
			log.Info().Msg("Dry run - not writing output")
			return nil
		}

		// Write output
		if err := s.writeOutput(ctx, data); err != nil {
			return fmt.Errorf("writing output: %w", err)
		}

		return nil
	}

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
			os.Remove(tmpPath) // Clean up on failure
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
	if s.cfg.Output.API == nil {
		return fmt.Errorf("API output not configured")
	}

	// TODO: Implement HTTP POST to redirector API
	// This would use net/http to POST the config to the redirector's management API
	log.Info().Str("url", s.cfg.Output.API.URL).Msg("Would push config to API (not implemented)")
	return nil
}

// fileSource implements ConfigSource for local files.
type fileSource struct {
	name     string
	priority int
	path     string
}

func (s *fileSource) Name() string     { return s.name }
func (s *fileSource) Priority() int    { return s.priority }

func (s *fileSource) Fetch(ctx context.Context) ([]byte, error) {
	return os.ReadFile(s.path)
}

func (s *fileSource) Validate(ctx context.Context) error {
	info, err := os.Stat(s.path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file")
	}
	return nil
}

// httpSource implements ConfigSource for HTTP/HTTPS URLs.
type httpSource struct {
	name     string
	priority int
	url      string
	headers  map[string]string
	timeout  time.Duration
}

func (s *httpSource) Name() string     { return s.name }
func (s *httpSource) Priority() int    { return s.priority }

func (s *httpSource) Fetch(ctx context.Context) ([]byte, error) {
	// TODO: Implement HTTP GET with timeout and headers
	// This is a stub - see internal/providers/http.go for full implementation
	return nil, fmt.Errorf("HTTP source not yet implemented in syncer")
}

func (s *httpSource) Validate(ctx context.Context) error {
	if s.url == "" {
		return fmt.Errorf("URL is required")
	}
	return nil
}
