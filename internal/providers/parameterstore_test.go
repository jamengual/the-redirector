package providers

import (
	"testing"
)

func TestNewParameterStoreSourceFromMap(t *testing.T) {
	tests := []struct {
		name    string
		cfg     map[string]interface{}
		wantErr bool
		check   func(*testing.T, Source)
	}{
		{
			name: "valid minimal config",
			cfg: map[string]interface{}{
				"path": "/myapp/config",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				ps, _ := s.(*ParameterStoreSource)
				if ps.cfg.Path != "/myapp/config" {
					t.Errorf("path = %q, want %q", ps.cfg.Path, "/myapp/config")
				}
				if !ps.cfg.WithDecryption {
					t.Error("WithDecryption should default to true")
				}
			},
		},
		{
			name: "with region and role ARN",
			cfg: map[string]interface{}{
				"path":     "/myapp/config",
				"region":   "us-west-2",
				"role_arn": "arn:aws:iam::123456789:role/MyRole",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				ps, _ := s.(*ParameterStoreSource)
				if ps.cfg.Region != "us-west-2" {
					t.Errorf("region = %q, want %q", ps.cfg.Region, "us-west-2")
				}
				if ps.cfg.RoleARN != "arn:aws:iam::123456789:role/MyRole" {
					t.Errorf("role_arn = %q, want %q", ps.cfg.RoleARN, "arn:aws:iam::123456789:role/MyRole")
				}
			},
		},
		{
			name: "with decryption disabled",
			cfg: map[string]interface{}{
				"path":            "/myapp/secrets",
				"with_decryption": false,
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				ps, _ := s.(*ParameterStoreSource)
				if ps.cfg.WithDecryption {
					t.Error("WithDecryption should be false")
				}
			},
		},
		{
			name: "with poll interval",
			cfg: map[string]interface{}{
				"path":          "/myapp/config",
				"poll_interval": "5m",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				ps, _ := s.(*ParameterStoreSource)
				if ps.cfg.PollInterval.Minutes() != 5 {
					t.Errorf("poll_interval = %v, want 5m", ps.cfg.PollInterval)
				}
			},
		},
		{
			name: "hierarchy path",
			cfg: map[string]interface{}{
				"path":      "/myapp/config/",
				"recursive": true,
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				ps, _ := s.(*ParameterStoreSource)
				if !ps.cfg.Recursive {
					t.Error("recursive should be true")
				}
				if !ps.isHierarchy() {
					t.Error("isHierarchy() should return true for path ending with /")
				}
			},
		},
		{
			name:    "missing path",
			cfg:     map[string]interface{}{},
			wantErr: true,
		},
		{
			name: "empty path",
			cfg: map[string]interface{}{
				"path": "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := NewParameterStoreSourceFromMap(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewParameterStoreSourceFromMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil && source != nil {
				tt.check(t, source)
			}
		})
	}
}

func TestParameterStoreSource_Name(t *testing.T) {
	source := &ParameterStoreSource{
		cfg: ParameterStoreSourceConfig{
			Path: "/myapp/config",
		},
	}

	if source.Name() != "parameterstore" {
		t.Errorf("Name() = %q, want %q", source.Name(), "parameterstore")
	}
}

func TestParameterStoreSource_SupportsWatch(t *testing.T) {
	source := &ParameterStoreSource{}

	if !source.SupportsWatch() {
		t.Error("SupportsWatch() = false, want true")
	}
}

func TestParameterStoreSource_isHierarchy(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/myapp/config", false},
		{"/myapp/config/", true},
		{"/myapp/", true},
		{"/config", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			source := &ParameterStoreSource{
				cfg: ParameterStoreSourceConfig{Path: tt.path},
			}
			if got := source.isHierarchy(); got != tt.want {
				t.Errorf("isHierarchy() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParamName(t *testing.T) {
	tests := []struct {
		fullPath string
		basePath string
		want     string
	}{
		{"/myapp/config/rules", "/myapp/config/", "rules"},
		{"/myapp/config/server", "/myapp/config/", "server"},
		{"/myapp/config/defaults", "/myapp/config/", "defaults"},
		{"/myapp/config/nested/param", "/myapp/config/", "nested/param"},
		{"/myapp/config", "/myapp/", "config"},
	}

	for _, tt := range tests {
		t.Run(tt.fullPath, func(t *testing.T) {
			got := paramName(tt.fullPath, tt.basePath)
			if got != tt.want {
				t.Errorf("paramName(%q, %q) = %q, want %q", tt.fullPath, tt.basePath, got, tt.want)
			}
		})
	}
}

func TestParameterStoreSource_Registry(t *testing.T) {
	// Verify Parameter Store source is registered
	factory, ok := Registry.Get("parameterstore")
	if !ok {
		t.Fatal("parameterstore source not registered")
	}

	source, err := factory(map[string]interface{}{
		"path": "/test/config",
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	if source.Name() != "parameterstore" {
		t.Errorf("Name() = %q, want %q", source.Name(), "parameterstore")
	}
}
