// Package providers defines interfaces for pluggable configuration sources.
// Contributors can implement the Source interface to add support for new
// configuration backends (VCS, cloud storage, databases, etc.)
package providers

import (
	"context"
	"time"

	"github.com/jamengual/the-redirector/internal/config"
)

// Source defines the interface for configuration sources.
// Implement this interface to add support for new configuration backends.
//
// Example implementations:
//   - FileSource: Local filesystem (YAML, JSON, TOML)
//   - S3Source: AWS S3 buckets
//   - GitHubSource: GitHub repositories
//   - ConsulSource: HashiCorp Consul
//
// To contribute a new source:
//  1. Create a new file in internal/providers/
//  2. Implement the Source interface
//  3. Register in the source registry
//  4. Add tests following existing patterns
type Source interface {
	// Name returns a unique identifier for this source type.
	// Examples: "file", "s3", "github", "consul"
	Name() string

	// Fetch retrieves the current configuration from the source.
	// This should be idempotent and safe to call repeatedly.
	// Returns an error if the configuration cannot be retrieved or parsed.
	Fetch(ctx context.Context) (*config.Config, error)

	// Watch returns a channel that emits new configurations when changes
	// are detected. The channel should be closed when the context is canceled.
	// Return nil if the source doesn't support watching (use polling instead).
	Watch(ctx context.Context) (<-chan *config.Config, error)

	// SupportsWatch returns true if this source supports real-time updates
	// via the Watch method. If false, the syncer will use polling.
	SupportsWatch() bool

	// Validate checks if the source is properly configured and accessible.
	// This is called during initialization to fail fast on misconfigurations.
	Validate(ctx context.Context) error

	// Close releases any resources held by the source.
	// Called when the syncer is shutting down.
	Close() error
}

// Metadata contains information about a fetched configuration.
type Metadata struct {
	// Version is a unique identifier for this config version.
	// Examples: Git commit SHA, S3 ETag, file modification time
	Version string

	// Source identifies where the config came from.
	// Example: "s3://bucket/key", "github:owner/repo:path"
	Source string

	// FetchedAt is when the config was retrieved.
	FetchedAt time.Time

	// Extra contains source-specific metadata.
	Extra map[string]string
}

// FetchResult wraps a configuration with metadata.
type FetchResult struct {
	Config   *config.Config
	Metadata Metadata
}

// SourceWithMetadata extends Source with metadata support.
// Implement this for sources that provide version tracking.
type SourceWithMetadata interface {
	Source

	// FetchWithMetadata retrieves config with version/source metadata.
	FetchWithMetadata(ctx context.Context) (*FetchResult, error)
}

// SourceOptions contains common configuration for sources.
type SourceOptions struct {
	// PollInterval is how often to check for changes (for non-watching sources).
	PollInterval time.Duration

	// RetryAttempts is how many times to retry on failure.
	RetryAttempts int

	// RetryDelay is the initial delay between retries (exponential backoff).
	RetryDelay time.Duration

	// Timeout is the maximum time for a single fetch operation.
	Timeout time.Duration
}

// DefaultSourceOptions returns sensible defaults for source options.
func DefaultSourceOptions() SourceOptions {
	return SourceOptions{
		PollInterval:  60 * time.Second,
		RetryAttempts: 3,
		RetryDelay:    1 * time.Second,
		Timeout:       30 * time.Second,
	}
}

// Registry manages available configuration sources.
// Use this to register custom sources at application startup.
var Registry = &SourceRegistry{
	sources: make(map[string]SourceFactory),
}

// SourceFactory creates a new source instance from configuration.
type SourceFactory func(cfg map[string]interface{}) (Source, error)

// SourceRegistry holds registered source factories.
type SourceRegistry struct {
	sources map[string]SourceFactory
}

// Register adds a new source type to the registry.
// Call this in an init() function to register custom sources.
//
// Example:
//
//	func init() {
//	    providers.Registry.Register("mycloud", NewMyCloudSource)
//	}
func (r *SourceRegistry) Register(name string, factory SourceFactory) {
	r.sources[name] = factory
}

// Get returns a factory for the given source type.
func (r *SourceRegistry) Get(name string) (SourceFactory, bool) {
	factory, ok := r.sources[name]
	return factory, ok
}

// List returns all registered source type names.
func (r *SourceRegistry) List() []string {
	names := make([]string, 0, len(r.sources))
	for name := range r.sources {
		names = append(names, name)
	}
	return names
}

// Create instantiates a source from configuration.
func (r *SourceRegistry) Create(sourceType string, cfg map[string]interface{}) (Source, error) {
	factory, ok := r.Get(sourceType)
	if !ok {
		return nil, &UnknownSourceError{Type: sourceType}
	}
	return factory(cfg)
}

// UnknownSourceError is returned when an unregistered source type is requested.
type UnknownSourceError struct {
	Type string
}

func (e *UnknownSourceError) Error() string {
	return "unknown source type: " + e.Type
}
