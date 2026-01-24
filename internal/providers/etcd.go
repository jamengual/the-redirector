// Package providers contains configuration source implementations.
// This file implements etcd KV integration.
package providers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/jamengual/the-redirector/internal/config"
)

func init() {
	Registry.Register("etcd", NewEtcdSourceFromMap)
}

// EtcdSourceConfig configures the etcd source.
type EtcdSourceConfig struct {
	// Key is the path to the configuration in etcd
	Key string `yaml:"key" json:"key"`

	// Endpoints is a list of etcd server addresses
	// Default: ["127.0.0.1:2379"]
	Endpoints []string `yaml:"endpoints" json:"endpoints"`

	// Username for authentication (optional)
	Username string `yaml:"username" json:"username"`

	// Password for authentication (optional)
	Password string `yaml:"password" json:"password"`

	// TLS configuration
	TLSConfig *EtcdTLSConfig `yaml:"tls" json:"tls"`

	// DialTimeout is the timeout for establishing connection (default: 5s)
	DialTimeout time.Duration `yaml:"dial_timeout" json:"dial_timeout"`

	// RequestTimeout for individual requests (default: 10s)
	RequestTimeout time.Duration `yaml:"request_timeout" json:"request_timeout"`
}

// EtcdTLSConfig configures TLS for etcd connection.
type EtcdTLSConfig struct {
	// CertFile is the client certificate file
	CertFile string `yaml:"cert_file" json:"cert_file"`

	// KeyFile is the client key file
	KeyFile string `yaml:"key_file" json:"key_file"`

	// CAFile is the trusted CA certificate file
	CAFile string `yaml:"ca_file" json:"ca_file"`

	// InsecureSkipVerify disables TLS verification (NOT recommended)
	InsecureSkipVerify bool `yaml:"insecure_skip_verify" json:"insecure_skip_verify"`
}

// EtcdSource fetches configuration from etcd KV store.
type EtcdSource struct {
	cfg          EtcdSourceConfig
	client       *clientv3.Client
	lastRevision int64 // Revision for change detection
	mu           sync.RWMutex
	stopCh       chan struct{}
	options      SourceOptions
}

// NewEtcdSourceFromMap creates a new etcd source from a config map.
func NewEtcdSourceFromMap(cfg map[string]interface{}) (Source, error) {
	key, ok := cfg["key"].(string)
	if !ok || key == "" {
		return nil, fmt.Errorf("etcd source requires 'key'")
	}

	sourceCfg := EtcdSourceConfig{
		Key:            key,
		Endpoints:      []string{"127.0.0.1:2379"},
		DialTimeout:    5 * time.Second,
		RequestTimeout: 10 * time.Second,
	}

	if endpoints, ok := cfg["endpoints"].([]interface{}); ok {
		sourceCfg.Endpoints = make([]string, 0, len(endpoints))
		for _, ep := range endpoints {
			if epStr, ok := ep.(string); ok {
				sourceCfg.Endpoints = append(sourceCfg.Endpoints, epStr)
			}
		}
	}

	if username, ok := cfg["username"].(string); ok {
		sourceCfg.Username = username
	}

	if password, ok := cfg["password"].(string); ok {
		sourceCfg.Password = password
	}

	if dialTimeout, ok := cfg["dial_timeout"].(string); ok {
		if d, err := time.ParseDuration(dialTimeout); err == nil {
			sourceCfg.DialTimeout = d
		}
	}

	if requestTimeout, ok := cfg["request_timeout"].(string); ok {
		if d, err := time.ParseDuration(requestTimeout); err == nil {
			sourceCfg.RequestTimeout = d
		}
	}

	// Parse TLS config
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

	return NewEtcdSource(context.Background(), sourceCfg)
}

// NewEtcdSource creates a new etcd KV configuration source.
func NewEtcdSource(ctx context.Context, cfg EtcdSourceConfig) (*EtcdSource, error) {
	if cfg.Key == "" {
		return nil, fmt.Errorf("key is required")
	}
	if len(cfg.Endpoints) == 0 {
		cfg.Endpoints = []string{"127.0.0.1:2379"}
	}
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = 5 * time.Second
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = 10 * time.Second
	}

	// Build etcd client config
	etcdCfg := clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: cfg.DialTimeout,
		Username:    cfg.Username,
		Password:    cfg.Password,
	}

	// Configure TLS
	if cfg.TLSConfig != nil {
		tlsConfig, err := buildEtcdTLSConfig(cfg.TLSConfig)
		if err != nil {
			return nil, fmt.Errorf("building TLS config: %w", err)
		}
		etcdCfg.TLS = tlsConfig
	}

	client, err := clientv3.New(etcdCfg)
	if err != nil {
		return nil, fmt.Errorf("creating etcd client: %w", err)
	}

	return &EtcdSource{
		cfg:     cfg,
		client:  client,
		stopCh:  make(chan struct{}),
		options: DefaultSourceOptions(),
	}, nil
}

// buildEtcdTLSConfig creates a tls.Config from EtcdTLSConfig.
func buildEtcdTLSConfig(cfg *EtcdTLSConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	}

	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("loading client cert: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	if cfg.CAFile != "" {
		caCert, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("reading CA file: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
	}

	return tlsConfig, nil
}

// Name returns the source name.
func (e *EtcdSource) Name() string {
	return "etcd"
}

// Fetch retrieves the configuration from etcd.
func (e *EtcdSource) Fetch(ctx context.Context) (*config.Config, error) {
	ctx, cancel := context.WithTimeout(ctx, e.cfg.RequestTimeout)
	defer cancel()

	resp, err := e.client.Get(ctx, e.cfg.Key)
	if err != nil {
		return nil, fmt.Errorf("getting key %s: %w", e.cfg.Key, err)
	}

	if len(resp.Kvs) == 0 {
		return nil, fmt.Errorf("key %s not found", e.cfg.Key)
	}

	kv := resp.Kvs[0]
	cfg, err := config.ParseBytes(kv.Value)
	if err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Store revision for change detection
	e.mu.Lock()
	e.lastRevision = resp.Header.Revision
	e.mu.Unlock()

	log.Debug().
		Str("key", e.cfg.Key).
		Int64("revision", resp.Header.Revision).
		Int("bytes", len(kv.Value)).
		Msg("Fetched config from etcd")

	return cfg, nil
}

// Watch uses etcd's native watch for real-time updates.
func (e *EtcdSource) Watch(ctx context.Context) (<-chan *config.Config, error) {
	ch := make(chan *config.Config, 1)

	go func() {
		defer close(ch)

		for {
			select {
			case <-ctx.Done():
				return
			case <-e.stopCh:
				return
			default:
			}

			e.mu.RLock()
			startRevision := e.lastRevision
			e.mu.RUnlock()

			// Start watching from the next revision
			watchOpts := []clientv3.OpOption{}
			if startRevision > 0 {
				watchOpts = append(watchOpts, clientv3.WithRev(startRevision+1))
			}

			watchCh := e.client.Watch(ctx, e.cfg.Key, watchOpts...)

		watchLoop:
			for {
				select {
				case <-ctx.Done():
					return
				case <-e.stopCh:
					return
				case wresp, ok := <-watchCh:
					if !ok {
						// Watch channel closed, restart
						break watchLoop
					}

					if wresp.Canceled {
						log.Warn().Msg("etcd watch canceled, restarting")
						break watchLoop
					}

					if wresp.Err() != nil {
						log.Error().Err(wresp.Err()).Msg("etcd watch error")
						break watchLoop
					}

					for _, ev := range wresp.Events {
						if ev.Type == clientv3.EventTypePut {
							// Key was updated
							cfg, err := config.ParseBytes(ev.Kv.Value)
							if err != nil {
								log.Error().Err(err).Msg("Error parsing config from etcd")
								continue
							}

							e.mu.Lock()
							e.lastRevision = wresp.Header.Revision
							e.mu.Unlock()

							log.Debug().
								Str("key", e.cfg.Key).
								Int64("revision", wresp.Header.Revision).
								Msg("etcd config changed")

							select {
							case ch <- cfg:
							case <-ctx.Done():
								return
							}
						} else if ev.Type == clientv3.EventTypeDelete {
							log.Warn().Str("key", e.cfg.Key).Msg("etcd key deleted")
						}
					}
				}
			}

			// Brief delay before restarting watch
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return
			case <-e.stopCh:
				return
			}
		}
	}()

	return ch, nil
}

// SupportsWatch returns true - etcd natively supports watches.
func (e *EtcdSource) SupportsWatch() bool {
	return true
}

// Validate checks that the key is accessible.
func (e *EtcdSource) Validate(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, e.cfg.RequestTimeout)
	defer cancel()

	resp, err := e.client.Get(ctx, e.cfg.Key)
	if err != nil {
		return fmt.Errorf("validating etcd access: %w", err)
	}

	if len(resp.Kvs) == 0 {
		return fmt.Errorf("key %s not found", e.cfg.Key)
	}

	return nil
}

// Close releases any resources.
func (e *EtcdSource) Close() error {
	close(e.stopCh)
	return e.client.Close()
}
