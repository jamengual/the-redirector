// Package providers contains configuration source implementations.
package providers

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/rs/zerolog/log"
	"github.com/your-org/the-redirector/internal/config"
)

// S3SourceConfig configures the S3 source.
type S3SourceConfig struct {
	// Bucket name
	Bucket string `yaml:"bucket" json:"bucket"`

	// Key (object path) in the bucket
	Key string `yaml:"key" json:"key"`

	// Region (optional, uses SDK default if not set)
	Region string `yaml:"region" json:"region"`

	// RoleARN for cross-account access (optional)
	RoleARN string `yaml:"role_arn" json:"role_arn"`

	// Endpoint for S3-compatible services like MinIO (optional)
	Endpoint string `yaml:"endpoint" json:"endpoint"`

	// PollInterval for checking changes (default: 5m)
	PollInterval time.Duration `yaml:"poll_interval" json:"poll_interval"`
}

// S3Source fetches configuration from AWS S3.
type S3Source struct {
	cfg      S3SourceConfig
	client   *s3.Client
	lastETag string
	mu       sync.RWMutex
}

// NewS3Source creates a new S3 configuration source.
func NewS3Source(ctx context.Context, cfg S3SourceConfig) (*S3Source, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("bucket is required")
	}
	if cfg.Key == "" {
		return nil, fmt.Errorf("key is required")
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

	// Create S3 client options
	var s3Opts []func(*s3.Options)

	// Custom endpoint for S3-compatible services
	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true // Required for most S3-compatible services
		})
	}

	// Assume role if specified
	if cfg.RoleARN != "" {
		stsClient := sts.NewFromConfig(awsCfg)
		creds := stscreds.NewAssumeRoleProvider(stsClient, cfg.RoleARN)
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.Credentials = aws.NewCredentialsCache(creds)
		})
	}

	client := s3.NewFromConfig(awsCfg, s3Opts...)

	return &S3Source{
		cfg:    cfg,
		client: client,
	}, nil
}

// Name returns the source name.
func (s *S3Source) Name() string {
	return fmt.Sprintf("s3://%s/%s", s.cfg.Bucket, s.cfg.Key)
}

// Fetch downloads and parses the configuration from S3.
func (s *S3Source) Fetch(ctx context.Context) (*config.Config, error) {
	data, etag, err := s.download(ctx)
	if err != nil {
		return nil, err
	}

	cfg, err := config.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Store ETag for change detection
	s.mu.Lock()
	s.lastETag = etag
	s.mu.Unlock()

	return cfg, nil
}

// FetchRaw downloads the raw configuration bytes from S3.
func (s *S3Source) FetchRaw(ctx context.Context) ([]byte, error) {
	data, etag, err := s.download(ctx)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.lastETag = etag
	s.mu.Unlock()

	return data, nil
}

// download fetches the object from S3.
func (s *S3Source) download(ctx context.Context) ([]byte, string, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(s.cfg.Key),
	}

	result, err := s.client.GetObject(ctx, input)
	if err != nil {
		return nil, "", fmt.Errorf("getting object from S3: %w", err)
	}
	defer result.Body.Close()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading S3 object: %w", err)
	}

	etag := ""
	if result.ETag != nil {
		etag = *result.ETag
	}

	log.Debug().
		Str("bucket", s.cfg.Bucket).
		Str("key", s.cfg.Key).
		Str("etag", etag).
		Int("bytes", len(data)).
		Msg("Downloaded config from S3")

	return data, etag, nil
}

// HasChanged checks if the S3 object has changed since last fetch.
func (s *S3Source) HasChanged(ctx context.Context) (bool, error) {
	s.mu.RLock()
	lastETag := s.lastETag
	s.mu.RUnlock()

	if lastETag == "" {
		// Never fetched, consider it changed
		return true, nil
	}

	// Use HeadObject to get metadata without downloading
	input := &s3.HeadObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(s.cfg.Key),
	}

	result, err := s.client.HeadObject(ctx, input)
	if err != nil {
		return false, fmt.Errorf("head object: %w", err)
	}

	currentETag := ""
	if result.ETag != nil {
		currentETag = *result.ETag
	}

	changed := currentETag != lastETag

	if changed {
		log.Debug().
			Str("bucket", s.cfg.Bucket).
			Str("key", s.cfg.Key).
			Str("old_etag", lastETag).
			Str("new_etag", currentETag).
			Msg("S3 config changed")
	}

	return changed, nil
}

// Watch polls S3 for changes and returns configs on a channel.
func (s *S3Source) Watch(ctx context.Context) (<-chan *config.Config, error) {
	ch := make(chan *config.Config)

	go func() {
		defer close(ch)

		ticker := time.NewTicker(s.cfg.PollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				changed, err := s.HasChanged(ctx)
				if err != nil {
					log.Error().Err(err).Msg("Error checking S3 for changes")
					continue
				}

				if !changed {
					continue
				}

				cfg, err := s.Fetch(ctx)
				if err != nil {
					log.Error().Err(err).Msg("Error fetching config from S3")
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

// Validate checks that the S3 bucket and key are accessible.
func (s *S3Source) Validate(ctx context.Context) error {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(s.cfg.Key),
	}

	_, err := s.client.HeadObject(ctx, input)
	if err != nil {
		return fmt.Errorf("validating S3 access: %w", err)
	}

	return nil
}

// PollInterval returns the configured poll interval.
func (s *S3Source) PollInterval() time.Duration {
	return s.cfg.PollInterval
}

// Close releases any resources.
func (s *S3Source) Close() error {
	return nil
}
