package providers

import (
	"testing"
	"time"
)

func TestNewS3SourceFromMap(t *testing.T) {
	tests := []struct {
		name    string
		cfg     map[string]interface{}
		wantErr bool
		check   func(*testing.T, Source)
	}{
		{
			name: "valid minimal config",
			cfg: map[string]interface{}{
				"bucket": "my-configs",
				"key":    "redirector/config.yaml",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				s3src, ok := s.(*S3Source)
				if !ok {
					t.Fatal("expected *S3Source")
				}
				if s3src.cfg.Bucket != "my-configs" {
					t.Errorf("Bucket = %q, want %q", s3src.cfg.Bucket, "my-configs")
				}
				if s3src.cfg.Key != "redirector/config.yaml" {
					t.Errorf("Key = %q, want %q", s3src.cfg.Key, "redirector/config.yaml")
				}
				if s3src.cfg.PollInterval != 5*time.Minute {
					t.Errorf("PollInterval = %v, want %v", s3src.cfg.PollInterval, 5*time.Minute)
				}
			},
		},
		{
			name: "with region and role ARN",
			cfg: map[string]interface{}{
				"bucket":   "my-configs",
				"key":      "config.yaml",
				"region":   "us-west-2",
				"role_arn": "arn:aws:iam::123456789012:role/config-reader",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				s3src, _ := s.(*S3Source)
				if s3src.cfg.Region != "us-west-2" {
					t.Errorf("Region = %q, want %q", s3src.cfg.Region, "us-west-2")
				}
				if s3src.cfg.RoleARN != "arn:aws:iam::123456789012:role/config-reader" {
					t.Errorf("RoleARN = %q, want %q", s3src.cfg.RoleARN, "arn:aws:iam::123456789012:role/config-reader")
				}
			},
		},
		{
			name: "with custom endpoint (MinIO)",
			cfg: map[string]interface{}{
				"bucket":   "my-configs",
				"key":      "config.yaml",
				"endpoint": "http://localhost:9000",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				s3src, _ := s.(*S3Source)
				if s3src.cfg.Endpoint != "http://localhost:9000" {
					t.Errorf("Endpoint = %q, want %q", s3src.cfg.Endpoint, "http://localhost:9000")
				}
			},
		},
		{
			name: "with poll interval",
			cfg: map[string]interface{}{
				"bucket":        "my-configs",
				"key":           "config.yaml",
				"poll_interval": "30s",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				s3src, _ := s.(*S3Source)
				if s3src.cfg.PollInterval != 30*time.Second {
					t.Errorf("PollInterval = %v, want %v", s3src.cfg.PollInterval, 30*time.Second)
				}
			},
		},
		{
			name: "missing bucket",
			cfg: map[string]interface{}{
				"key": "config.yaml",
			},
			wantErr: true,
		},
		{
			name: "missing key",
			cfg: map[string]interface{}{
				"bucket": "my-configs",
			},
			wantErr: true,
		},
		{
			name:    "empty config",
			cfg:     map[string]interface{}{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := NewS3SourceFromMap(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewS3SourceFromMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil && source != nil {
				tt.check(t, source)
			}
		})
	}
}

func TestS3Source_Name(t *testing.T) {
	source := &S3Source{
		cfg: S3SourceConfig{
			Bucket: "my-bucket",
			Key:    "configs/redirector.yaml",
		},
	}

	want := "s3://my-bucket/configs/redirector.yaml"
	if got := source.Name(); got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
}

func TestS3Source_SupportsWatch(t *testing.T) {
	source := &S3Source{}
	if !source.SupportsWatch() {
		t.Error("SupportsWatch() = false, want true")
	}
}

func TestS3Source_Registry(t *testing.T) {
	factory, ok := Registry.Get("s3")
	if !ok {
		t.Fatal("s3 source not registered in Registry")
	}
	_ = factory
}

func TestS3Source_PollInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		want     time.Duration
	}{
		{
			name:     "custom interval",
			interval: 30 * time.Second,
			want:     30 * time.Second,
		},
		{
			name:     "default interval",
			interval: 5 * time.Minute,
			want:     5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &S3Source{
				cfg: S3SourceConfig{
					PollInterval: tt.interval,
				},
			}
			if got := source.PollInterval(); got != tt.want {
				t.Errorf("PollInterval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestS3Source_Close(t *testing.T) {
	source := &S3Source{}
	if err := source.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}
