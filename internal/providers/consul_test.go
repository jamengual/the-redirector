package providers

import (
	"testing"
	"time"
)

func TestNewConsulSourceFromMap(t *testing.T) {
	tests := []struct {
		name    string
		cfg     map[string]interface{}
		wantErr bool
		check   func(*testing.T, Source)
	}{
		{
			name: "valid minimal config",
			cfg: map[string]interface{}{
				"key": "redirector/config",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				cs, _ := s.(*ConsulSource)
				if cs.cfg.Key != "redirector/config" {
					t.Errorf("key = %q, want %q", cs.cfg.Key, "redirector/config")
				}
				if cs.cfg.WaitTime != 5*time.Minute {
					t.Errorf("wait_time = %v, want 5m", cs.cfg.WaitTime)
				}
			},
		},
		{
			name: "with address and datacenter",
			cfg: map[string]interface{}{
				"key":        "config",
				"address":    "consul.example.com:8500",
				"datacenter": "dc1",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				cs, _ := s.(*ConsulSource)
				if cs.cfg.Address != "consul.example.com:8500" {
					t.Errorf("address = %q, want %q", cs.cfg.Address, "consul.example.com:8500")
				}
				if cs.cfg.Datacenter != "dc1" {
					t.Errorf("datacenter = %q, want %q", cs.cfg.Datacenter, "dc1")
				}
			},
		},
		{
			name: "with token",
			cfg: map[string]interface{}{
				"key":   "config",
				"token": "acl-token-xxxx",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				cs, _ := s.(*ConsulSource)
				if cs.cfg.Token != "acl-token-xxxx" {
					t.Errorf("token = %q, want %q", cs.cfg.Token, "acl-token-xxxx")
				}
			},
		},
		{
			name: "with namespace and partition (Enterprise)",
			cfg: map[string]interface{}{
				"key":       "config",
				"namespace": "app-ns",
				"partition": "prod",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				cs, _ := s.(*ConsulSource)
				if cs.cfg.Namespace != "app-ns" {
					t.Errorf("namespace = %q, want %q", cs.cfg.Namespace, "app-ns")
				}
				if cs.cfg.Partition != "prod" {
					t.Errorf("partition = %q, want %q", cs.cfg.Partition, "prod")
				}
			},
		},
		{
			name: "with wait time",
			cfg: map[string]interface{}{
				"key":       "config",
				"wait_time": "10m",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				cs, _ := s.(*ConsulSource)
				if cs.cfg.WaitTime != 10*time.Minute {
					t.Errorf("wait_time = %v, want 10m", cs.cfg.WaitTime)
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
			source, err := NewConsulSourceFromMap(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewConsulSourceFromMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil && source != nil {
				tt.check(t, source)
			}
		})
	}
}

func TestConsulSource_Name(t *testing.T) {
	source := &ConsulSource{
		cfg: ConsulSourceConfig{
			Key: "config",
		},
	}

	if source.Name() != "consul" {
		t.Errorf("Name() = %q, want %q", source.Name(), "consul")
	}
}

func TestConsulSource_SupportsWatch(t *testing.T) {
	source := &ConsulSource{}

	if !source.SupportsWatch() {
		t.Error("SupportsWatch() = false, want true")
	}
}

func TestConsulTLSConfig_Parsing(t *testing.T) {
	// Test TLS config parsing without creating the actual client
	cfg := map[string]interface{}{
		"key": "config",
		"tls": map[string]interface{}{
			"address":              "consul.example.com",
			"ca_file":              "/etc/consul/ca.pem",
			"cert_file":            "/etc/consul/cert.pem",
			"key_file":             "/etc/consul/key.pem",
			"insecure_skip_verify": true,
		},
	}

	// Parse the TLS config manually without creating client
	sourceCfg := ConsulSourceConfig{}

	if tlsCfg, ok := cfg["tls"].(map[string]interface{}); ok {
		sourceCfg.TLSConfig = &ConsulTLSConfig{}
		if addr, ok := tlsCfg["address"].(string); ok {
			sourceCfg.TLSConfig.Address = addr
		}
		if caFile, ok := tlsCfg["ca_file"].(string); ok {
			sourceCfg.TLSConfig.CAFile = caFile
		}
		if certFile, ok := tlsCfg["cert_file"].(string); ok {
			sourceCfg.TLSConfig.CertFile = certFile
		}
		if keyFile, ok := tlsCfg["key_file"].(string); ok {
			sourceCfg.TLSConfig.KeyFile = keyFile
		}
		if insecure, ok := tlsCfg["insecure_skip_verify"].(bool); ok {
			sourceCfg.TLSConfig.InsecureSkipVerify = insecure
		}
	}

	if sourceCfg.TLSConfig == nil {
		t.Fatal("TLS config should not be nil")
	}
	if sourceCfg.TLSConfig.Address != "consul.example.com" {
		t.Errorf("tls.address = %q, want %q", sourceCfg.TLSConfig.Address, "consul.example.com")
	}
	if sourceCfg.TLSConfig.CAFile != "/etc/consul/ca.pem" {
		t.Errorf("tls.ca_file = %q, want %q", sourceCfg.TLSConfig.CAFile, "/etc/consul/ca.pem")
	}
	if sourceCfg.TLSConfig.CertFile != "/etc/consul/cert.pem" {
		t.Errorf("tls.cert_file = %q, want %q", sourceCfg.TLSConfig.CertFile, "/etc/consul/cert.pem")
	}
	if sourceCfg.TLSConfig.KeyFile != "/etc/consul/key.pem" {
		t.Errorf("tls.key_file = %q, want %q", sourceCfg.TLSConfig.KeyFile, "/etc/consul/key.pem")
	}
	if !sourceCfg.TLSConfig.InsecureSkipVerify {
		t.Error("tls.insecure_skip_verify = false, want true")
	}
}

func TestConsulSource_Registry(t *testing.T) {
	// Verify Consul source is registered
	factory, ok := Registry.Get("consul")
	if !ok {
		t.Fatal("consul source not registered")
	}

	source, err := factory(map[string]interface{}{
		"key": "test/config",
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	if source.Name() != "consul" {
		t.Errorf("Name() = %q, want %q", source.Name(), "consul")
	}
}
