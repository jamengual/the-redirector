package providers

import (
	"testing"
	"time"
)

func TestNewSecretsManagerSourceFromMap(t *testing.T) {
	tests := []struct {
		name    string
		cfg     map[string]interface{}
		wantErr bool
		check   func(*testing.T, Source)
	}{
		{
			name: "valid minimal config",
			cfg: map[string]interface{}{
				"secret_id": "my-app/config",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				sm := s.(*SecretsManagerSource)
				if sm.cfg.SecretID != "my-app/config" {
					t.Errorf("secret_id = %q, want %q", sm.cfg.SecretID, "my-app/config")
				}
				if sm.cfg.CacheTTL != 5*time.Minute {
					t.Errorf("cache_ttl = %v, want 5m", sm.cfg.CacheTTL)
				}
			},
		},
		{
			name: "with region and role ARN",
			cfg: map[string]interface{}{
				"secret_id": "my-app/config",
				"region":    "eu-west-1",
				"role_arn":  "arn:aws:iam::123456789:role/SecretsRole",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				sm := s.(*SecretsManagerSource)
				if sm.cfg.Region != "eu-west-1" {
					t.Errorf("region = %q, want %q", sm.cfg.Region, "eu-west-1")
				}
				if sm.cfg.RoleARN != "arn:aws:iam::123456789:role/SecretsRole" {
					t.Errorf("role_arn = %q, want %q", sm.cfg.RoleARN, "arn:aws:iam::123456789:role/SecretsRole")
				}
			},
		},
		{
			name: "with version ID",
			cfg: map[string]interface{}{
				"secret_id":  "my-app/config",
				"version_id": "12345678-1234-1234-1234-123456789012",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				sm := s.(*SecretsManagerSource)
				if sm.cfg.VersionID != "12345678-1234-1234-1234-123456789012" {
					t.Errorf("version_id = %q, want %q", sm.cfg.VersionID, "12345678-1234-1234-1234-123456789012")
				}
			},
		},
		{
			name: "with version stage",
			cfg: map[string]interface{}{
				"secret_id":     "my-app/config",
				"version_stage": "AWSPREVIOUS",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				sm := s.(*SecretsManagerSource)
				if sm.cfg.VersionStage != "AWSPREVIOUS" {
					t.Errorf("version_stage = %q, want %q", sm.cfg.VersionStage, "AWSPREVIOUS")
				}
			},
		},
		{
			name: "with custom cache TTL",
			cfg: map[string]interface{}{
				"secret_id": "my-app/config",
				"cache_ttl": "10m",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				sm := s.(*SecretsManagerSource)
				if sm.cfg.CacheTTL != 10*time.Minute {
					t.Errorf("cache_ttl = %v, want 10m", sm.cfg.CacheTTL)
				}
			},
		},
		{
			name: "with poll interval",
			cfg: map[string]interface{}{
				"secret_id":     "my-app/config",
				"poll_interval": "1m",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				sm := s.(*SecretsManagerSource)
				if sm.cfg.PollInterval != time.Minute {
					t.Errorf("poll_interval = %v, want 1m", sm.cfg.PollInterval)
				}
			},
		},
		{
			name: "with secret ARN",
			cfg: map[string]interface{}{
				"secret_id": "arn:aws:secretsmanager:us-west-2:123456789:secret:my-app/config-AbCdEf",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				sm := s.(*SecretsManagerSource)
				if sm.cfg.SecretID != "arn:aws:secretsmanager:us-west-2:123456789:secret:my-app/config-AbCdEf" {
					t.Errorf("secret_id unexpected value")
				}
			},
		},
		{
			name:    "missing secret_id",
			cfg:     map[string]interface{}{},
			wantErr: true,
		},
		{
			name: "empty secret_id",
			cfg: map[string]interface{}{
				"secret_id": "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := NewSecretsManagerSourceFromMap(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSecretsManagerSourceFromMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil && source != nil {
				tt.check(t, source)
			}
		})
	}
}

func TestSecretsManagerSource_Name(t *testing.T) {
	source := &SecretsManagerSource{
		cfg: SecretsManagerSourceConfig{
			SecretID: "my-app/config",
		},
	}

	if source.Name() != "secretsmanager" {
		t.Errorf("Name() = %q, want %q", source.Name(), "secretsmanager")
	}
}

func TestSecretsManagerSource_SupportsWatch(t *testing.T) {
	source := &SecretsManagerSource{}

	if !source.SupportsWatch() {
		t.Error("SupportsWatch() = false, want true")
	}
}

func TestSecretsManagerSource_InvalidateCache(t *testing.T) {
	source := &SecretsManagerSource{
		cachedConfig: []byte("cached"),
		cacheExpiry:  time.Now().Add(time.Hour),
	}

	source.InvalidateCache()

	if source.cachedConfig != nil {
		t.Error("cachedConfig should be nil after InvalidateCache")
	}

	if !source.cacheExpiry.IsZero() {
		t.Error("cacheExpiry should be zero after InvalidateCache")
	}
}

func TestSecretsManagerSource_Registry(t *testing.T) {
	// Verify Secrets Manager source is registered
	factory, ok := Registry.Get("secretsmanager")
	if !ok {
		t.Fatal("secretsmanager source not registered")
	}

	source, err := factory(map[string]interface{}{
		"secret_id": "test/config",
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	if source.Name() != "secretsmanager" {
		t.Errorf("Name() = %q, want %q", source.Name(), "secretsmanager")
	}
}
