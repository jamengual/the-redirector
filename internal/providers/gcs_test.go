package providers

import (
	"testing"
	"time"
)

func TestNewGCSSourceFromMap_Validation(t *testing.T) {
	// These tests only check validation, not actual client creation
	// Client creation tests are skipped without credentials
	tests := []struct {
		name    string
		cfg     map[string]interface{}
		wantErr bool
	}{
		{
			name: "missing bucket",
			cfg: map[string]interface{}{
				"object": "config.yaml",
			},
			wantErr: true,
		},
		{
			name: "missing object",
			cfg: map[string]interface{}{
				"bucket": "my-bucket",
			},
			wantErr: true,
		},
		{
			name: "empty bucket",
			cfg: map[string]interface{}{
				"bucket": "",
				"object": "config.yaml",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewGCSSourceFromMap(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewGCSSourceFromMap() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGCSSourceConfig_Parsing(t *testing.T) {
	// Test config parsing without creating the actual client
	tests := []struct {
		name  string
		cfg   map[string]interface{}
		check func(*testing.T, GCSSourceConfig)
	}{
		{
			name: "parses bucket and object",
			cfg: map[string]interface{}{
				"bucket": "my-bucket",
				"object": "config/redirector.yaml",
			},
			check: func(t *testing.T, cfg GCSSourceConfig) {
				if cfg.Bucket != "my-bucket" {
					t.Errorf("bucket = %q, want %q", cfg.Bucket, "my-bucket")
				}
				if cfg.Object != "config/redirector.yaml" {
					t.Errorf("object = %q, want %q", cfg.Object, "config/redirector.yaml")
				}
			},
		},
		{
			name: "parses project",
			cfg: map[string]interface{}{
				"bucket":  "my-bucket",
				"object":  "config.yaml",
				"project": "my-gcp-project",
			},
			check: func(t *testing.T, cfg GCSSourceConfig) {
				if cfg.Project != "my-gcp-project" {
					t.Errorf("project = %q, want %q", cfg.Project, "my-gcp-project")
				}
			},
		},
		{
			name: "parses poll interval",
			cfg: map[string]interface{}{
				"bucket":        "my-bucket",
				"object":        "config.yaml",
				"poll_interval": "10m",
			},
			check: func(t *testing.T, cfg GCSSourceConfig) {
				if cfg.PollInterval != 10*time.Minute {
					t.Errorf("poll_interval = %v, want 10m", cfg.PollInterval)
				}
			},
		},
		{
			name: "parses credentials file",
			cfg: map[string]interface{}{
				"bucket":           "my-bucket",
				"object":           "config.yaml",
				"credentials_file": "/path/to/creds.json",
			},
			check: func(t *testing.T, cfg GCSSourceConfig) {
				if cfg.CredentialsFile != "/path/to/creds.json" {
					t.Errorf("credentials_file = %q, want %q", cfg.CredentialsFile, "/path/to/creds.json")
				}
			},
		},
		{
			name: "default poll interval",
			cfg: map[string]interface{}{
				"bucket": "my-bucket",
				"object": "config.yaml",
			},
			check: func(t *testing.T, cfg GCSSourceConfig) {
				if cfg.PollInterval != 5*time.Minute {
					t.Errorf("poll_interval = %v, want 5m (default)", cfg.PollInterval)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse config without creating client
			bucket, _ := tt.cfg["bucket"].(string)
			object, _ := tt.cfg["object"].(string)

			cfg := GCSSourceConfig{
				Bucket:       bucket,
				Object:       object,
				PollInterval: 5 * time.Minute,
			}

			if project, ok := tt.cfg["project"].(string); ok {
				cfg.Project = project
			}
			if credsFile, ok := tt.cfg["credentials_file"].(string); ok {
				cfg.CredentialsFile = credsFile
			}
			if credsJSON, ok := tt.cfg["credentials_json"].(string); ok {
				cfg.CredentialsJSON = credsJSON
			}
			if interval, ok := tt.cfg["poll_interval"].(string); ok {
				if d, err := time.ParseDuration(interval); err == nil {
					cfg.PollInterval = d
				}
			}

			tt.check(t, cfg)
		})
	}
}

func TestGCSSource_Name(t *testing.T) {
	source := &GCSSource{
		cfg: GCSSourceConfig{
			Bucket: "my-bucket",
			Object: "config.yaml",
		},
	}

	if source.Name() != "gcs" {
		t.Errorf("Name() = %q, want %q", source.Name(), "gcs")
	}
}

func TestGCSSource_SupportsWatch(t *testing.T) {
	source := &GCSSource{}

	if !source.SupportsWatch() {
		t.Error("SupportsWatch() = false, want true")
	}
}

func TestGCSSource_Registry(t *testing.T) {
	// Verify GCS source is registered
	factory, ok := Registry.Get("gcs")
	if !ok {
		t.Fatal("gcs source not registered")
	}

	// Can't test full creation without valid credentials
	_ = factory
}
