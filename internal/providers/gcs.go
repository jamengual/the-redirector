// Package providers contains configuration source implementations.
// This file implements Google Cloud Storage integration.
package providers

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"github.com/rs/zerolog/log"
	"google.golang.org/api/option"

	"github.com/jamengual/the-redirector/internal/config"
)

func init() {
	Registry.Register("gcs", NewGCSSourceFromMap)
}

// GCSSourceConfig configures the GCS source.
type GCSSourceConfig struct {
	// Bucket is the GCS bucket name
	Bucket string `yaml:"bucket" json:"bucket"`

	// Object is the object (file) path in the bucket
	Object string `yaml:"object" json:"object"`

	// Project is the GCP project ID (optional, uses default if not set)
	Project string `yaml:"project" json:"project"`

	// CredentialsFile is the path to a service account JSON key file (optional)
	// If not set, uses Application Default Credentials
	CredentialsFile string `yaml:"credentials_file" json:"credentials_file"`

	// CredentialsJSON is the raw service account JSON (optional, alternative to file)
	// Use environment variable: ${GCS_CREDENTIALS}
	CredentialsJSON string `yaml:"credentials_json" json:"credentials_json"`

	// PollInterval for checking changes (default: 5m)
	PollInterval time.Duration `yaml:"poll_interval" json:"poll_interval"`
}

// GCSSource fetches configuration from Google Cloud Storage.
type GCSSource struct {
	cfg            GCSSourceConfig
	client         *storage.Client
	lastGeneration int64 // GCS object generation for change detection
	mu             sync.RWMutex
	stopCh         chan struct{}
	options        SourceOptions
}

// NewGCSSourceFromMap creates a new GCS source from a config map.
func NewGCSSourceFromMap(cfg map[string]interface{}) (Source, error) {
	bucket, ok := cfg["bucket"].(string)
	if !ok || bucket == "" {
		return nil, fmt.Errorf("gcs source requires 'bucket'")
	}

	object, ok := cfg["object"].(string)
	if !ok || object == "" {
		return nil, fmt.Errorf("gcs source requires 'object'")
	}

	sourceCfg := GCSSourceConfig{
		Bucket:       bucket,
		Object:       object,
		PollInterval: 5 * time.Minute,
	}

	if project, ok := cfg["project"].(string); ok {
		sourceCfg.Project = project
	}

	if credsFile, ok := cfg["credentials_file"].(string); ok {
		sourceCfg.CredentialsFile = credsFile
	}

	if credsJSON, ok := cfg["credentials_json"].(string); ok {
		sourceCfg.CredentialsJSON = credsJSON
	}

	if interval, ok := cfg["poll_interval"].(string); ok {
		if d, err := time.ParseDuration(interval); err == nil {
			sourceCfg.PollInterval = d
		}
	}

	return NewGCSSource(context.Background(), sourceCfg)
}

// NewGCSSource creates a new Google Cloud Storage configuration source.
func NewGCSSource(ctx context.Context, cfg GCSSourceConfig) (*GCSSource, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("bucket is required")
	}
	if cfg.Object == "" {
		return nil, fmt.Errorf("object is required")
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 5 * time.Minute
	}

	// Build client options
	var opts []option.ClientOption

	if cfg.CredentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(cfg.CredentialsFile))
	} else if cfg.CredentialsJSON != "" {
		opts = append(opts, option.WithCredentialsJSON([]byte(cfg.CredentialsJSON)))
	}
	// If no credentials specified, uses Application Default Credentials

	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating GCS client: %w", err)
	}

	return &GCSSource{
		cfg:     cfg,
		client:  client,
		stopCh:  make(chan struct{}),
		options: DefaultSourceOptions(),
	}, nil
}

// Name returns the source name.
func (g *GCSSource) Name() string {
	return "gcs"
}

// Fetch retrieves the configuration from GCS.
func (g *GCSSource) Fetch(ctx context.Context) (*config.Config, error) {
	data, generation, err := g.download(ctx)
	if err != nil {
		return nil, err
	}

	cfg, err := config.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Store generation for change detection
	g.mu.Lock()
	g.lastGeneration = generation
	g.mu.Unlock()

	return cfg, nil
}

// download fetches the object content.
func (g *GCSSource) download(ctx context.Context) ([]byte, int64, error) {
	obj := g.client.Bucket(g.cfg.Bucket).Object(g.cfg.Object)

	reader, err := obj.NewReader(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("opening object: %w", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, 0, fmt.Errorf("reading object: %w", err)
	}

	// Get object attributes for generation
	attrs, err := obj.Attrs(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("getting object attributes: %w", err)
	}

	log.Debug().
		Str("bucket", g.cfg.Bucket).
		Str("object", g.cfg.Object).
		Int64("generation", attrs.Generation).
		Int("bytes", len(data)).
		Msg("Downloaded config from GCS")

	return data, attrs.Generation, nil
}

// Watch polls GCS for changes and returns configs on a channel.
func (g *GCSSource) Watch(ctx context.Context) (<-chan *config.Config, error) {
	ch := make(chan *config.Config, 1)

	go func() {
		defer close(ch)

		ticker := time.NewTicker(g.cfg.PollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-g.stopCh:
				return
			case <-ticker.C:
				changed, err := g.hasChanged(ctx)
				if err != nil {
					log.Error().Err(err).Msg("Error checking GCS for changes")
					continue
				}

				if !changed {
					continue
				}

				cfg, err := g.Fetch(ctx)
				if err != nil {
					log.Error().Err(err).Msg("Error fetching config from GCS")
					continue
				}

				select {
				case ch <- cfg:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch, nil
}

// hasChanged checks if the object has changed since last fetch.
func (g *GCSSource) hasChanged(ctx context.Context) (bool, error) {
	g.mu.RLock()
	lastGeneration := g.lastGeneration
	g.mu.RUnlock()

	if lastGeneration == 0 {
		return true, nil // Never fetched
	}

	// Get object attributes without downloading content
	attrs, err := g.client.Bucket(g.cfg.Bucket).Object(g.cfg.Object).Attrs(ctx)
	if err != nil {
		return false, fmt.Errorf("getting object attributes: %w", err)
	}

	changed := attrs.Generation != lastGeneration

	if changed {
		log.Debug().
			Str("bucket", g.cfg.Bucket).
			Str("object", g.cfg.Object).
			Int64("old_generation", lastGeneration).
			Int64("new_generation", attrs.Generation).
			Msg("GCS config changed")
	}

	return changed, nil
}

// SupportsWatch returns true - GCS uses polling.
func (g *GCSSource) SupportsWatch() bool {
	return true
}

// Validate checks that the object is accessible.
func (g *GCSSource) Validate(ctx context.Context) error {
	_, err := g.client.Bucket(g.cfg.Bucket).Object(g.cfg.Object).Attrs(ctx)
	if err != nil {
		return fmt.Errorf("validating GCS access: %w", err)
	}
	return nil
}

// Close releases any resources.
func (g *GCSSource) Close() error {
	close(g.stopCh)
	return g.client.Close()
}
