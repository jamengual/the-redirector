# Contributing to The Redirector

Thank you for your interest in contributing! This guide will help you get started.

## Development Setup

### Prerequisites

- Go 1.21 or later
- Docker (optional, for container builds)
- k6 (optional, for load testing)
- golangci-lint (optional, for linting)

### Getting Started

```bash
# Clone the repository
git clone https://github.com/your-org/the-redirector.git
cd the-redirector

# Install dependencies
go mod download

# Build
make build

# Run tests
make test

# Run linter
make lint
```

## Code Style

- Follow standard Go conventions
- Use `gofmt` for formatting
- Keep functions small and focused (single responsibility)
- Write tests for all new functionality
- Use meaningful variable and function names

## Adding a New Configuration Source

One of the most valuable contributions is adding support for new configuration sources. Here's how to do it:

### 1. Create a New Source File

Create a new file in `internal/providers/`:

```go
// internal/providers/mycloud.go
package providers

import (
	"context"
	"fmt"

	"github.com/your-org/the-redirector/internal/config"
)

func init() {
	// Register your source at package initialization
	Registry.Register("mycloud", NewMyCloudSource)
}

// MyCloudSource reads configuration from MyCloud storage.
type MyCloudSource struct {
	bucket  string
	key     string
	client  *mycloud.Client
	options SourceOptions
}

// NewMyCloudSource creates a new MyCloud configuration source.
func NewMyCloudSource(cfg map[string]interface{}) (Source, error) {
	bucket, ok := cfg["bucket"].(string)
	if !ok || bucket == "" {
		return nil, fmt.Errorf("mycloud source requires 'bucket' configuration")
	}

	key, ok := cfg["key"].(string)
	if !ok || key == "" {
		return nil, fmt.Errorf("mycloud source requires 'key' configuration")
	}

	// Initialize your client
	client, err := mycloud.NewClient()
	if err != nil {
		return nil, fmt.Errorf("creating mycloud client: %w", err)
	}

	return &MyCloudSource{
		bucket:  bucket,
		key:     key,
		client:  client,
		options: DefaultSourceOptions(),
	}, nil
}

// Name returns the source type identifier.
func (s *MyCloudSource) Name() string {
	return "mycloud"
}

// Fetch retrieves the configuration from MyCloud.
func (s *MyCloudSource) Fetch(ctx context.Context) (*config.Config, error) {
	// Implement fetch logic
	data, err := s.client.GetObject(ctx, s.bucket, s.key)
	if err != nil {
		return nil, fmt.Errorf("fetching from mycloud: %w", err)
	}

	// Parse the configuration
	cfg, err := config.ParseYAML(data)
	if err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return cfg, nil
}

// Watch returns a channel for config updates.
// Return nil if your source doesn't support watching.
func (s *MyCloudSource) Watch(ctx context.Context) (<-chan *config.Config, error) {
	// Option 1: Return nil if watching not supported
	// return nil, nil

	// Option 2: Implement polling-based watching
	updates := make(chan *config.Config, 1)
	go func() {
		ticker := time.NewTicker(s.options.PollInterval)
		defer ticker.Stop()
		defer close(updates)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cfg, err := s.Fetch(ctx)
				if err == nil {
					updates <- cfg
				}
			}
		}
	}()
	return updates, nil
}

// SupportsWatch returns whether this source supports real-time updates.
func (s *MyCloudSource) SupportsWatch() bool {
	return true // or false if only polling
}

// Validate checks if the source is properly configured.
func (s *MyCloudSource) Validate(ctx context.Context) error {
	// Check if we can access the bucket/key
	_, err := s.client.HeadObject(ctx, s.bucket, s.key)
	return err
}

// Close releases resources.
func (s *MyCloudSource) Close() error {
	return s.client.Close()
}
```

### 2. Write Tests

Create a test file `internal/providers/mycloud_test.go`:

```go
package providers

import (
	"context"
	"testing"
)

func TestMyCloudSource_Name(t *testing.T) {
	src := &MyCloudSource{}
	if src.Name() != "mycloud" {
		t.Errorf("expected name 'mycloud', got %s", src.Name())
	}
}

func TestNewMyCloudSource_MissingBucket(t *testing.T) {
	_, err := NewMyCloudSource(map[string]interface{}{
		"key": "config.yaml",
	})
	if err == nil {
		t.Error("expected error for missing bucket")
	}
}

func TestMyCloudSource_Fetch(t *testing.T) {
	// Use a mock client or integration test
	// ...
}
```

### 3. Add Documentation

Update the README to document your new source:

```yaml
# Example configuration
sources:
  - type: mycloud
    bucket: my-config-bucket
    key: redirects/config.yaml
    region: us-east-1
```

### 4. Submit a Pull Request

1. Create a feature branch: `git checkout -b feature/mycloud-source`
2. Make your changes
3. Run tests: `make test`
4. Run linter: `make lint`
5. Commit with a clear message
6. Push and create a PR

## Source Interface Reference

```go
type Source interface {
	// Name returns a unique identifier for this source type.
	Name() string

	// Fetch retrieves the current configuration.
	Fetch(ctx context.Context) (*config.Config, error)

	// Watch returns a channel for config updates (nil if not supported).
	Watch(ctx context.Context) (<-chan *config.Config, error)

	// SupportsWatch indicates if real-time updates are available.
	SupportsWatch() bool

	// Validate checks if the source is properly configured.
	Validate(ctx context.Context) error

	// Close releases any resources.
	Close() error
}
```

## Currently Supported Sources

| Source | Status | Maintainer |
|--------|--------|------------|
| `file` | Stable | Core Team |
| `s3` | Planned | - |
| `github` | Planned | - |
| `consul` | Planned | - |
| `etcd` | Planned | - |

## Wanted Contributions

We're actively looking for contributors to implement:

- **AWS S3** - S3 bucket configuration with ETag-based change detection
- **GitHub/GitLab** - Git repository with webhook support
- **HashiCorp Consul** - Consul KV store with watch support
- **etcd** - etcd v3 with watch support
- **Azure Blob Storage** - Azure storage account
- **GCP Cloud Storage** - Google Cloud Storage
- **HTTP** - Generic HTTP endpoint with polling

## Pull Request Guidelines

1. **One feature per PR** - Keep PRs focused
2. **Tests required** - All new code must have tests
3. **Documentation** - Update relevant docs
4. **Clean history** - Squash commits if needed
5. **Descriptive title** - Summarize the change
6. **Link issues** - Reference related issues

## Testing

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# Run specific package tests
go test -v ./internal/providers/

# Run benchmarks
make bench
```

## Questions?

- Open an issue for bugs or feature requests
- Use discussions for questions
- Tag maintainers if you need help

Thank you for contributing!
