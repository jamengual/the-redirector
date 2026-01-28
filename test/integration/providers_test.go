//go:build integration
// +build integration

// Integration tests for configuration providers.
// Run with: go test -tags=integration ./test/integration/...
//
// Prerequisites:
//   docker-compose -f test/integration/docker-compose.yml up -d
//   ./test/integration/setup.sh

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/jamengual/the-redirector/internal/providers"
)

// Test configuration used across all providers
const testConfigYAML = `version: "1.0"
rules:
  - id: test-redirect
    match:
      type: exact
      path: /old
    redirect:
      to: https://new.example.com/
      status: 301
`

func TestS3Source_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Configure AWS SDK to use LocalStack
	os.Setenv("AWS_ACCESS_KEY_ID", "test")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "test")

	source, err := providers.NewS3Source(ctx, providers.S3SourceConfig{
		Bucket:   "test-config",
		Key:      "redirector.yaml",
		Region:   "us-east-1",
		Endpoint: "http://localhost:4566",
	})
	if err != nil {
		t.Fatalf("failed to create S3 source: %v", err)
	}
	defer source.Close()

	// Test Validate
	if err := source.Validate(ctx); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Test Fetch
	cfg, err := source.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("expected config, got nil")
	}

	if len(cfg.Rules) == 0 {
		t.Error("expected at least one rule")
	}
}

func TestParameterStoreSource_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Configure to use LocalStack
	os.Setenv("AWS_ACCESS_KEY_ID", "test")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	os.Setenv("AWS_ENDPOINT_URL", "http://localhost:4566")

	source, err := providers.NewParameterStoreSourceFromMap(map[string]interface{}{
		"path":   "/redirector/config",
		"region": "us-east-1",
	})
	if err != nil {
		t.Fatalf("failed to create Parameter Store source: %v", err)
	}
	defer source.Close()

	// Test Fetch
	cfg, err := source.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("expected config, got nil")
	}

	if len(cfg.Rules) == 0 {
		t.Error("expected at least one rule")
	}
}

func TestSecretsManagerSource_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	os.Setenv("AWS_ACCESS_KEY_ID", "test")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	os.Setenv("AWS_ENDPOINT_URL", "http://localhost:4566")

	source, err := providers.NewSecretsManagerSourceFromMap(map[string]interface{}{
		"secret_id": "redirector/config",
		"region":    "us-east-1",
	})
	if err != nil {
		t.Fatalf("failed to create Secrets Manager source: %v", err)
	}
	defer source.Close()

	// Test Fetch
	cfg, err := source.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("expected config, got nil")
	}

	if len(cfg.Rules) == 0 {
		t.Error("expected at least one rule")
	}
}

func TestAzureBlobSource_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Azurite default connection string
	connStr := "DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;"

	source, err := providers.NewAzureBlobSourceFromMap(map[string]interface{}{
		"container":         "configs",
		"blob_name":         "redirector.yaml",
		"connection_string": connStr,
	})
	if err != nil {
		t.Fatalf("failed to create Azure Blob source: %v", err)
	}
	defer source.Close()

	// Test Fetch
	cfg, err := source.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("expected config, got nil")
	}

	if len(cfg.Rules) == 0 {
		t.Error("expected at least one rule")
	}
}

func TestGCSSource_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use endpoint with full path as recommended by fake-gcs-server docs
	source, err := providers.NewGCSSourceFromMap(map[string]interface{}{
		"bucket":   "test-config",
		"object":   "redirector.yaml",
		"endpoint": "http://localhost:4443/storage/v1/",
	})
	if err != nil {
		t.Fatalf("failed to create GCS source: %v", err)
	}
	defer source.Close()

	// Test Fetch
	cfg, err := source.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("expected config, got nil")
	}

	if len(cfg.Rules) == 0 {
		t.Error("expected at least one rule")
	}
}

func TestConsulSource_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	source, err := providers.NewConsulSourceFromMap(map[string]interface{}{
		"key":     "redirector/config",
		"address": "127.0.0.1:8500",
	})
	if err != nil {
		t.Fatalf("failed to create Consul source: %v", err)
	}
	defer source.Close()

	// Test Validate
	if err := source.Validate(ctx); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Test Fetch
	cfg, err := source.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("expected config, got nil")
	}

	if len(cfg.Rules) == 0 {
		t.Error("expected at least one rule")
	}
}

func TestEtcdSource_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	source, err := providers.NewEtcdSourceFromMap(map[string]interface{}{
		"key":       "/redirector/config",
		"endpoints": []interface{}{"127.0.0.1:2379"},
	})
	if err != nil {
		t.Fatalf("failed to create etcd source: %v", err)
	}
	defer source.Close()

	// Test Validate
	if err := source.Validate(ctx); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Test Fetch
	cfg, err := source.Fetch(ctx)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("expected config, got nil")
	}

	if len(cfg.Rules) == 0 {
		t.Error("expected at least one rule")
	}
}

func TestGitHubSource_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Mock GitHub API server
	mux := http.NewServeMux()

	// Repository endpoint (Validate)
	mux.HandleFunc("/repos/test-org/test-repo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"full_name": "test-org/test-repo",
			"private":   false,
		})
	})

	// Releases endpoint (Fetch with release strategy)
	mux.HandleFunc("/repos/test-org/test-repo/releases", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{
				"tag_name":   "v1.2.0",
				"prerelease": false,
				"draft":      false,
			},
			{
				"tag_name":   "v1.1.0-rc1",
				"prerelease": true,
				"draft":      false,
			},
		})
	})

	// Branches endpoint (Fetch with branch strategy)
	mux.HandleFunc("/repos/test-org/test-repo/branches/main", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"commit": map[string]interface{}{
				"sha": "abc123def456",
			},
		})
	})

	// Tags endpoint
	mux.HandleFunc("/repos/test-org/test-repo/tags", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"name": "v1.2.0", "commit": map[string]interface{}{"sha": "abc123"}},
			{"name": "config-3.0", "commit": map[string]interface{}{"sha": "def456"}},
		})
	})

	// Contents endpoint (config file)
	mux.HandleFunc("/repos/test-org/test-repo/contents/config.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.github.raw")
		w.Header().Set("ETag", `"etag-integration-test"`)
		w.Write([]byte(testConfigYAML))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	t.Run("validate", func(t *testing.T) {
		source, err := providers.Registry.Create("github", map[string]interface{}{
			"repository": "test-org/test-repo",
			"base_url":   server.URL,
			"token":      "test-pat-token",
		})
		if err != nil {
			t.Fatalf("failed to create GitHub source: %v", err)
		}
		defer source.Close()

		if err := source.Validate(ctx); err != nil {
			t.Fatalf("Validate failed: %v", err)
		}
	})

	t.Run("fetch with release strategy", func(t *testing.T) {
		source, err := providers.Registry.Create("github", map[string]interface{}{
			"repository":  "test-org/test-repo",
			"base_url":    server.URL,
			"token":       "test-pat-token",
			"strategy":    "release",
			"environment": "production",
		})
		if err != nil {
			t.Fatalf("failed to create GitHub source: %v", err)
		}
		defer source.Close()

		cfg, err := source.Fetch(ctx)
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}
		if cfg == nil {
			t.Fatal("expected config, got nil")
		}
		if len(cfg.Rules) == 0 {
			t.Error("expected at least one rule")
		}
	})

	t.Run("fetch with branch strategy", func(t *testing.T) {
		source, err := providers.Registry.Create("github", map[string]interface{}{
			"repository":  "test-org/test-repo",
			"base_url":    server.URL,
			"token":       "test-pat-token",
			"strategy":    "branch",
			"environment": "production",
		})
		if err != nil {
			t.Fatalf("failed to create GitHub source: %v", err)
		}
		defer source.Close()

		cfg, err := source.Fetch(ctx)
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}
		if cfg == nil {
			t.Fatal("expected config, got nil")
		}
		if len(cfg.Rules) == 0 {
			t.Error("expected at least one rule")
		}
	})

	t.Run("fetch with tag strategy and pattern", func(t *testing.T) {
		source, err := providers.Registry.Create("github", map[string]interface{}{
			"repository":  "test-org/test-repo",
			"base_url":    server.URL,
			"token":       "test-pat-token",
			"strategy":    "tag",
			"tag_pattern": "v*",
		})
		if err != nil {
			t.Fatalf("failed to create GitHub source: %v", err)
		}
		defer source.Close()

		cfg, err := source.Fetch(ctx)
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}
		if cfg == nil {
			t.Fatal("expected config, got nil")
		}
	})
}

// TestWatch_Integration tests the watch functionality for providers that support it
func TestConsulSource_Watch_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	source, err := providers.NewConsulSourceFromMap(map[string]interface{}{
		"key":       "redirector/watch-test",
		"address":   "127.0.0.1:8500",
		"wait_time": "5s",
	})
	if err != nil {
		t.Fatalf("failed to create Consul source: %v", err)
	}
	defer source.Close()

	// Initial fetch to establish baseline
	_, err = source.Fetch(ctx)
	if err != nil {
		t.Skipf("Skipping watch test - initial fetch failed: %v", err)
	}

	// Start watching
	watchCh, err := source.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}

	// Verify watch channel is created
	if watchCh == nil {
		t.Fatal("expected watch channel, got nil")
	}

	// Note: Actually testing the watch would require updating the value
	// in Consul and verifying we receive the update on the channel.
	// This is a basic smoke test.
}

// loadLocalStackAWSConfig returns an AWS config configured for LocalStack
func loadLocalStackAWSConfig(ctx context.Context) (aws.Config, error) {
	return awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
		awsconfig.WithEndpointResolverWithOptions(
			aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL:           "http://localhost:4566",
					SigningRegion: "us-east-1",
				}, nil
			}),
		),
	)
}
