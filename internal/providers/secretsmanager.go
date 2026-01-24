// Package providers contains configuration source implementations.
// This file implements AWS Secrets Manager integration.
package providers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/rs/zerolog/log"

	"github.com/jamengual/the-redirector/internal/config"
)

func init() {
	Registry.Register("secretsmanager", NewSecretsManagerSourceFromMap)
}

// SecretsManagerSourceConfig configures the Secrets Manager source.
type SecretsManagerSourceConfig struct {
	// SecretID is the ARN or name of the secret
	SecretID string `yaml:"secret_id" json:"secret_id"`

	// Region (optional, uses SDK default if not set)
	Region string `yaml:"region" json:"region"`

	// RoleARN for cross-account access (optional)
	RoleARN string `yaml:"role_arn" json:"role_arn"`

	// VersionID to retrieve a specific version (optional)
	// If not set, retrieves the current version (AWSCURRENT)
	VersionID string `yaml:"version_id" json:"version_id"`

	// VersionStage to retrieve a version by staging label
	// Common values: AWSCURRENT (default), AWSPREVIOUS, AWSPENDING
	// Custom staging labels are also supported
	VersionStage string `yaml:"version_stage" json:"version_stage"`

	// CacheTTL is how long to cache the secret before re-fetching (default: 5m)
	// Set to 0 to disable caching
	CacheTTL time.Duration `yaml:"cache_ttl" json:"cache_ttl"`

	// PollInterval for checking changes (default: 5m)
	// Only relevant when watching for changes
	PollInterval time.Duration `yaml:"poll_interval" json:"poll_interval"`
}

// SecretsManagerSource fetches configuration from AWS Secrets Manager.
// The secret value should contain the full configuration in YAML or JSON format.
type SecretsManagerSource struct {
	cfg          SecretsManagerSourceConfig
	client       *secretsmanager.Client
	lastVersion  string    // Track version for change detection
	cachedConfig []byte    // Cached configuration
	cacheExpiry  time.Time // When the cache expires
	mu           sync.RWMutex
	stopCh       chan struct{}
	options      SourceOptions
}

// NewSecretsManagerSourceFromMap creates a new Secrets Manager source from a config map.
func NewSecretsManagerSourceFromMap(cfg map[string]interface{}) (Source, error) {
	secretID, ok := cfg["secret_id"].(string)
	if !ok || secretID == "" {
		return nil, fmt.Errorf("secretsmanager source requires 'secret_id'")
	}

	sourceCfg := SecretsManagerSourceConfig{
		SecretID:     secretID,
		CacheTTL:     5 * time.Minute,
		PollInterval: 5 * time.Minute,
	}

	if region, ok := cfg["region"].(string); ok {
		sourceCfg.Region = region
	}

	if roleARN, ok := cfg["role_arn"].(string); ok {
		sourceCfg.RoleARN = roleARN
	}

	if versionID, ok := cfg["version_id"].(string); ok {
		sourceCfg.VersionID = versionID
	}

	if versionStage, ok := cfg["version_stage"].(string); ok {
		sourceCfg.VersionStage = versionStage
	}

	if cacheTTL, ok := cfg["cache_ttl"].(string); ok {
		if d, err := time.ParseDuration(cacheTTL); err == nil {
			sourceCfg.CacheTTL = d
		}
	}

	if interval, ok := cfg["poll_interval"].(string); ok {
		if d, err := time.ParseDuration(interval); err == nil {
			sourceCfg.PollInterval = d
		}
	}

	return NewSecretsManagerSource(context.Background(), sourceCfg)
}

// NewSecretsManagerSource creates a new Secrets Manager configuration source.
func NewSecretsManagerSource(ctx context.Context, cfg SecretsManagerSourceConfig) (*SecretsManagerSource, error) {
	if cfg.SecretID == "" {
		return nil, fmt.Errorf("secret_id is required")
	}
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 5 * time.Minute
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 5 * time.Minute
	}

	// Load AWS config
	var opts []func(*awsconfig.LoadOptions) error

	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	// Create Secrets Manager client options
	var smOpts []func(*secretsmanager.Options)

	// Assume role if specified
	if cfg.RoleARN != "" {
		stsClient := sts.NewFromConfig(awsCfg)
		creds := stscreds.NewAssumeRoleProvider(stsClient, cfg.RoleARN)
		smOpts = append(smOpts, func(o *secretsmanager.Options) {
			o.Credentials = aws.NewCredentialsCache(creds)
		})
	}

	client := secretsmanager.NewFromConfig(awsCfg, smOpts...)

	return &SecretsManagerSource{
		cfg:     cfg,
		client:  client,
		stopCh:  make(chan struct{}),
		options: DefaultSourceOptions(),
	}, nil
}

// Name returns the source name.
func (s *SecretsManagerSource) Name() string {
	return "secretsmanager"
}

// Fetch retrieves the configuration from Secrets Manager.
func (s *SecretsManagerSource) Fetch(ctx context.Context) (*config.Config, error) {
	data, err := s.getSecret(ctx)
	if err != nil {
		return nil, err
	}

	cfg, err := config.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return cfg, nil
}

// getSecret retrieves the secret value, using cache if valid.
func (s *SecretsManagerSource) getSecret(ctx context.Context) ([]byte, error) {
	// Check cache
	s.mu.RLock()
	if s.cachedConfig != nil && time.Now().Before(s.cacheExpiry) {
		data := s.cachedConfig
		s.mu.RUnlock()
		return data, nil
	}
	s.mu.RUnlock()

	// Fetch from Secrets Manager
	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(s.cfg.SecretID),
	}

	// Add version specifier if provided
	if s.cfg.VersionID != "" {
		input.VersionId = aws.String(s.cfg.VersionID)
	} else if s.cfg.VersionStage != "" {
		input.VersionStage = aws.String(s.cfg.VersionStage)
	}

	result, err := s.client.GetSecretValue(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("getting secret %s: %w", s.cfg.SecretID, err)
	}

	var data []byte
	if result.SecretString != nil {
		data = []byte(*result.SecretString)
	} else if result.SecretBinary != nil {
		data = result.SecretBinary
	} else {
		return nil, fmt.Errorf("secret %s has no value", s.cfg.SecretID)
	}

	// Update cache
	s.mu.Lock()
	s.cachedConfig = data
	s.cacheExpiry = time.Now().Add(s.cfg.CacheTTL)
	if result.VersionId != nil {
		s.lastVersion = *result.VersionId
	}
	s.mu.Unlock()

	log.Debug().
		Str("secret_id", s.cfg.SecretID).
		Str("version", s.lastVersion).
		Int("bytes", len(data)).
		Msg("Fetched secret from Secrets Manager")

	return data, nil
}

// Watch polls Secrets Manager for changes and returns configs on a channel.
func (s *SecretsManagerSource) Watch(ctx context.Context) (<-chan *config.Config, error) {
	ch := make(chan *config.Config, 1)

	go func() {
		defer close(ch)

		ticker := time.NewTicker(s.cfg.PollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			case <-ticker.C:
				changed, err := s.hasChanged(ctx)
				if err != nil {
					log.Error().Err(err).Msg("Error checking Secrets Manager for changes")
					continue
				}

				if !changed {
					continue
				}

				// Invalidate cache to force re-fetch
				s.mu.Lock()
				s.cachedConfig = nil
				s.cacheExpiry = time.Time{}
				s.mu.Unlock()

				cfg, err := s.Fetch(ctx)
				if err != nil {
					log.Error().Err(err).Msg("Error fetching config from Secrets Manager")
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

// hasChanged checks if the secret has changed since last fetch.
func (s *SecretsManagerSource) hasChanged(ctx context.Context) (bool, error) {
	s.mu.RLock()
	lastVersion := s.lastVersion
	s.mu.RUnlock()

	if lastVersion == "" {
		return true, nil // Never fetched
	}

	// Use DescribeSecret to get metadata without retrieving the value
	input := &secretsmanager.DescribeSecretInput{
		SecretId: aws.String(s.cfg.SecretID),
	}

	result, err := s.client.DescribeSecret(ctx, input)
	if err != nil {
		return false, fmt.Errorf("describing secret: %w", err)
	}

	// Check if the current version has changed
	// VersionIdsToStages maps version IDs to their staging labels
	for versionID, stages := range result.VersionIdsToStages {
		for _, stage := range stages {
			// Check if this version is the current one
			if (s.cfg.VersionStage != "" && stage == s.cfg.VersionStage) ||
				(s.cfg.VersionStage == "" && stage == "AWSCURRENT") {
				if versionID != lastVersion {
					log.Debug().
						Str("secret_id", s.cfg.SecretID).
						Str("old_version", lastVersion).
						Str("new_version", versionID).
						Str("stage", stage).
						Msg("Secret version changed")
					return true, nil
				}
				return false, nil
			}
		}
	}

	return false, nil
}

// SupportsWatch returns true - Secrets Manager uses polling.
func (s *SecretsManagerSource) SupportsWatch() bool {
	return true
}

// Validate checks that the secret is accessible.
func (s *SecretsManagerSource) Validate(ctx context.Context) error {
	input := &secretsmanager.DescribeSecretInput{
		SecretId: aws.String(s.cfg.SecretID),
	}

	result, err := s.client.DescribeSecret(ctx, input)
	if err != nil {
		return fmt.Errorf("validating Secrets Manager access: %w", err)
	}

	// Check if secret is marked for deletion
	if result.DeletedDate != nil {
		return fmt.Errorf("secret %s is marked for deletion", s.cfg.SecretID)
	}

	return nil
}

// InvalidateCache forces the next Fetch to retrieve from Secrets Manager.
func (s *SecretsManagerSource) InvalidateCache() {
	s.mu.Lock()
	s.cachedConfig = nil
	s.cacheExpiry = time.Time{}
	s.mu.Unlock()
}

// Close releases any resources.
func (s *SecretsManagerSource) Close() error {
	close(s.stopCh)
	return nil
}
