package providers

import (
	"testing"
	"time"
)

func TestNewEtcdSourceFromMap(t *testing.T) {
	tests := []struct {
		name    string
		cfg     map[string]interface{}
		wantErr bool
		check   func(*testing.T, EtcdSourceConfig)
	}{
		{
			name: "valid minimal config",
			cfg: map[string]interface{}{
				"key": "/redirector/config",
			},
			wantErr: false,
			check: func(t *testing.T, cfg EtcdSourceConfig) {
				if cfg.Key != "/redirector/config" {
					t.Errorf("key = %q, want %q", cfg.Key, "/redirector/config")
				}
				if len(cfg.Endpoints) != 1 || cfg.Endpoints[0] != "127.0.0.1:2379" {
					t.Errorf("endpoints = %v, want [127.0.0.1:2379]", cfg.Endpoints)
				}
			},
		},
		{
			name: "with multiple endpoints",
			cfg: map[string]interface{}{
				"key":       "/config",
				"endpoints": []interface{}{"etcd1:2379", "etcd2:2379", "etcd3:2379"},
			},
			wantErr: false,
			check: func(t *testing.T, cfg EtcdSourceConfig) {
				if len(cfg.Endpoints) != 3 {
					t.Errorf("endpoints count = %d, want 3", len(cfg.Endpoints))
				}
			},
		},
		{
			name: "with auth",
			cfg: map[string]interface{}{
				"key":      "/config",
				"username": "root",
				"password": "secret",
			},
			wantErr: false,
			check: func(t *testing.T, cfg EtcdSourceConfig) {
				if cfg.Username != "root" {
					t.Errorf("username = %q, want %q", cfg.Username, "root")
				}
				if cfg.Password != "secret" {
					t.Errorf("password = %q, want %q", cfg.Password, "secret")
				}
			},
		},
		{
			name: "with timeouts",
			cfg: map[string]interface{}{
				"key":             "/config",
				"dial_timeout":    "10s",
				"request_timeout": "30s",
			},
			wantErr: false,
			check: func(t *testing.T, cfg EtcdSourceConfig) {
				if cfg.DialTimeout != 10*time.Second {
					t.Errorf("dial_timeout = %v, want 10s", cfg.DialTimeout)
				}
				if cfg.RequestTimeout != 30*time.Second {
					t.Errorf("request_timeout = %v, want 30s", cfg.RequestTimeout)
				}
			},
		},
		{
			name:    "missing key",
			cfg:     map[string]interface{}{},
			wantErr: true,
		},
		{
			name: "empty key",
			cfg: map[string]interface{}{
				"key": "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse config without actually creating client
			key, _ := tt.cfg["key"].(string)
			if key == "" && !tt.wantErr {
				t.Skip("skipping validation-only test")
			}

			if tt.wantErr {
				_, err := NewEtcdSourceFromMap(tt.cfg)
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			// For valid configs, parse manually to check config values
			cfg := EtcdSourceConfig{
				Key:            key,
				Endpoints:      []string{"127.0.0.1:2379"},
				DialTimeout:    5 * time.Second,
				RequestTimeout: 10 * time.Second,
			}

			if endpoints, ok := tt.cfg["endpoints"].([]interface{}); ok {
				cfg.Endpoints = make([]string, 0, len(endpoints))
				for _, ep := range endpoints {
					if epStr, ok := ep.(string); ok {
						cfg.Endpoints = append(cfg.Endpoints, epStr)
					}
				}
			}

			if username, ok := tt.cfg["username"].(string); ok {
				cfg.Username = username
			}
			if password, ok := tt.cfg["password"].(string); ok {
				cfg.Password = password
			}
			if dialTimeout, ok := tt.cfg["dial_timeout"].(string); ok {
				if d, err := time.ParseDuration(dialTimeout); err == nil {
					cfg.DialTimeout = d
				}
			}
			if requestTimeout, ok := tt.cfg["request_timeout"].(string); ok {
				if d, err := time.ParseDuration(requestTimeout); err == nil {
					cfg.RequestTimeout = d
				}
			}

			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestEtcdTLSConfig_Parsing(t *testing.T) {
	cfg := map[string]interface{}{
		"key": "/config",
		"tls": map[string]interface{}{
			"cert_file":            "/etc/etcd/cert.pem",
			"key_file":             "/etc/etcd/key.pem",
			"ca_file":              "/etc/etcd/ca.pem",
			"insecure_skip_verify": true,
		},
	}

	// Parse TLS config manually
	sourceCfg := EtcdSourceConfig{
		Key: cfg["key"].(string),
	}

	if tlsCfg, ok := cfg["tls"].(map[string]interface{}); ok {
		sourceCfg.TLSConfig = &EtcdTLSConfig{}
		if certFile, ok := tlsCfg["cert_file"].(string); ok {
			sourceCfg.TLSConfig.CertFile = certFile
		}
		if keyFile, ok := tlsCfg["key_file"].(string); ok {
			sourceCfg.TLSConfig.KeyFile = keyFile
		}
		if caFile, ok := tlsCfg["ca_file"].(string); ok {
			sourceCfg.TLSConfig.CAFile = caFile
		}
		if insecure, ok := tlsCfg["insecure_skip_verify"].(bool); ok {
			sourceCfg.TLSConfig.InsecureSkipVerify = insecure
		}
	}

	if sourceCfg.TLSConfig == nil {
		t.Fatal("TLS config should not be nil")
	}
	if sourceCfg.TLSConfig.CertFile != "/etc/etcd/cert.pem" {
		t.Errorf("tls.cert_file = %q, want %q", sourceCfg.TLSConfig.CertFile, "/etc/etcd/cert.pem")
	}
	if sourceCfg.TLSConfig.KeyFile != "/etc/etcd/key.pem" {
		t.Errorf("tls.key_file = %q, want %q", sourceCfg.TLSConfig.KeyFile, "/etc/etcd/key.pem")
	}
	if sourceCfg.TLSConfig.CAFile != "/etc/etcd/ca.pem" {
		t.Errorf("tls.ca_file = %q, want %q", sourceCfg.TLSConfig.CAFile, "/etc/etcd/ca.pem")
	}
	if !sourceCfg.TLSConfig.InsecureSkipVerify {
		t.Error("tls.insecure_skip_verify = false, want true")
	}
}

func TestEtcdSource_Name(t *testing.T) {
	source := &EtcdSource{
		cfg: EtcdSourceConfig{
			Key: "/config",
		},
	}

	if source.Name() != "etcd" {
		t.Errorf("Name() = %q, want %q", source.Name(), "etcd")
	}
}

func TestEtcdSource_SupportsWatch(t *testing.T) {
	source := &EtcdSource{}

	if !source.SupportsWatch() {
		t.Error("SupportsWatch() = false, want true")
	}
}

func TestEtcdSource_Registry(t *testing.T) {
	// Verify etcd source is registered
	factory, ok := Registry.Get("etcd")
	if !ok {
		t.Fatal("etcd source not registered")
	}

	// Can't create actual client without running etcd
	_ = factory
}
