// Package providers contains configuration source implementations.
// This file implements HashiCorp Consul KV integration.
package providers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/rs/zerolog/log"

	"github.com/jamengual/the-redirector/internal/config"
)

func init() {
	Registry.Register("consul", NewConsulSourceFromMap)
}

// ConsulSourceConfig configures the Consul source.
type ConsulSourceConfig struct {
	// Key is the path to the configuration in Consul KV
	Key string `yaml:"key" json:"key"`

	// Address is the Consul HTTP address (default: 127.0.0.1:8500)
	Address string `yaml:"address" json:"address"`

	// Datacenter to query (optional, uses agent's default)
	Datacenter string `yaml:"datacenter" json:"datacenter"`

	// Token is the ACL token for authentication (optional)
	Token string `yaml:"token" json:"token"`

	// Namespace for Consul Enterprise (optional)
	Namespace string `yaml:"namespace" json:"namespace"`

	// Partition for Consul Enterprise (optional)
	Partition string `yaml:"partition" json:"partition"`

	// TLS configuration
	TLSConfig *ConsulTLSConfig `yaml:"tls" json:"tls"`

	// WaitTime for blocking queries (default: 5m)
	// This is how long a watch query will wait before returning if no changes
	WaitTime time.Duration `yaml:"wait_time" json:"wait_time"`
}

// ConsulTLSConfig configures TLS for Consul connection.
type ConsulTLSConfig struct {
	// Address is used for SNI and verification (if different from Address)
	Address string `yaml:"address" json:"address"`

	// CAFile is the path to CA certificate
	CAFile string `yaml:"ca_file" json:"ca_file"`

	// CAPath is the directory of CA certificates
	CAPath string `yaml:"ca_path" json:"ca_path"`

	// CertFile is the client certificate file
	CertFile string `yaml:"cert_file" json:"cert_file"`

	// KeyFile is the client key file
	KeyFile string `yaml:"key_file" json:"key_file"`

	// InsecureSkipVerify disables TLS verification (NOT recommended for production)
	InsecureSkipVerify bool `yaml:"insecure_skip_verify" json:"insecure_skip_verify"`
}

// ConsulSource fetches configuration from Consul KV store.
type ConsulSource struct {
	cfg         ConsulSourceConfig
	client      *api.Client
	lastIndex   uint64 // ModifyIndex for change detection
	mu          sync.RWMutex
	stopCh      chan struct{}
	options     SourceOptions
	queryOpts   *api.QueryOptions
}

// NewConsulSourceFromMap creates a new Consul source from a config map.
func NewConsulSourceFromMap(cfg map[string]interface{}) (Source, error) {
	key, ok := cfg["key"].(string)
	if !ok || key == "" {
		return nil, fmt.Errorf("consul source requires 'key'")
	}

	sourceCfg := ConsulSourceConfig{
		Key:      key,
		WaitTime: 5 * time.Minute,
	}

	if address, ok := cfg["address"].(string); ok {
		sourceCfg.Address = address
	}

	if dc, ok := cfg["datacenter"].(string); ok {
		sourceCfg.Datacenter = dc
	}

	if token, ok := cfg["token"].(string); ok {
		sourceCfg.Token = token
	}

	if ns, ok := cfg["namespace"].(string); ok {
		sourceCfg.Namespace = ns
	}

	if partition, ok := cfg["partition"].(string); ok {
		sourceCfg.Partition = partition
	}

	if waitTime, ok := cfg["wait_time"].(string); ok {
		if d, err := time.ParseDuration(waitTime); err == nil {
			sourceCfg.WaitTime = d
		}
	}

	// Parse TLS config
	if tlsCfg, ok := cfg["tls"].(map[string]interface{}); ok {
		sourceCfg.TLSConfig = &ConsulTLSConfig{}
		if addr, ok := tlsCfg["address"].(string); ok {
			sourceCfg.TLSConfig.Address = addr
		}
		if caFile, ok := tlsCfg["ca_file"].(string); ok {
			sourceCfg.TLSConfig.CAFile = caFile
		}
		if caPath, ok := tlsCfg["ca_path"].(string); ok {
			sourceCfg.TLSConfig.CAPath = caPath
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

	return NewConsulSource(sourceCfg)
}

// NewConsulSource creates a new Consul KV configuration source.
func NewConsulSource(cfg ConsulSourceConfig) (*ConsulSource, error) {
	if cfg.Key == "" {
		return nil, fmt.Errorf("key is required")
	}
	if cfg.WaitTime == 0 {
		cfg.WaitTime = 5 * time.Minute
	}

	// Build Consul client config
	consulCfg := api.DefaultConfig()

	if cfg.Address != "" {
		consulCfg.Address = cfg.Address
	}

	if cfg.Datacenter != "" {
		consulCfg.Datacenter = cfg.Datacenter
	}

	if cfg.Token != "" {
		consulCfg.Token = cfg.Token
	}

	if cfg.Namespace != "" {
		consulCfg.Namespace = cfg.Namespace
	}

	if cfg.Partition != "" {
		consulCfg.Partition = cfg.Partition
	}

	// Configure TLS
	if cfg.TLSConfig != nil {
		consulCfg.TLSConfig = api.TLSConfig{
			Address:            cfg.TLSConfig.Address,
			CAFile:             cfg.TLSConfig.CAFile,
			CAPath:             cfg.TLSConfig.CAPath,
			CertFile:           cfg.TLSConfig.CertFile,
			KeyFile:            cfg.TLSConfig.KeyFile,
			InsecureSkipVerify: cfg.TLSConfig.InsecureSkipVerify,
		}
	}

	client, err := api.NewClient(consulCfg)
	if err != nil {
		return nil, fmt.Errorf("creating Consul client: %w", err)
	}

	// Build query options
	queryOpts := &api.QueryOptions{
		Datacenter: cfg.Datacenter,
		Namespace:  cfg.Namespace,
		Partition:  cfg.Partition,
		WaitTime:   cfg.WaitTime,
	}

	return &ConsulSource{
		cfg:       cfg,
		client:    client,
		stopCh:    make(chan struct{}),
		options:   DefaultSourceOptions(),
		queryOpts: queryOpts,
	}, nil
}

// Name returns the source name.
func (c *ConsulSource) Name() string {
	return "consul"
}

// Fetch retrieves the configuration from Consul KV.
func (c *ConsulSource) Fetch(ctx context.Context) (*config.Config, error) {
	kv := c.client.KV()

	pair, meta, err := kv.Get(c.cfg.Key, c.queryOpts.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("getting key %s: %w", c.cfg.Key, err)
	}

	if pair == nil {
		return nil, fmt.Errorf("key %s not found", c.cfg.Key)
	}

	cfg, err := config.ParseBytes(pair.Value)
	if err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Store ModifyIndex for change detection
	c.mu.Lock()
	c.lastIndex = meta.LastIndex
	c.mu.Unlock()

	log.Debug().
		Str("key", c.cfg.Key).
		Uint64("index", meta.LastIndex).
		Int("bytes", len(pair.Value)).
		Msg("Fetched config from Consul")

	return cfg, nil
}

// Watch uses Consul's blocking queries for real-time updates.
// This is more efficient than polling as it only returns when data changes.
func (c *ConsulSource) Watch(ctx context.Context) (<-chan *config.Config, error) {
	ch := make(chan *config.Config, 1)

	go func() {
		defer close(ch)

		kv := c.client.KV()

		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			default:
			}

			// Build query options with blocking query
			c.mu.RLock()
			lastIndex := c.lastIndex
			c.mu.RUnlock()

			opts := &api.QueryOptions{
				Datacenter: c.cfg.Datacenter,
				Namespace:  c.cfg.Namespace,
				Partition:  c.cfg.Partition,
				WaitIndex:  lastIndex, // Block until index changes
				WaitTime:   c.cfg.WaitTime,
			}

			pair, meta, err := kv.Get(c.cfg.Key, opts.WithContext(ctx))
			if err != nil {
				if ctx.Err() != nil {
					return // Context cancelled
				}
				log.Error().Err(err).Msg("Error watching Consul key")
				time.Sleep(time.Second) // Brief delay before retry
				continue
			}

			// Check if index changed (data was modified)
			if meta.LastIndex == lastIndex {
				continue // No change, blocking query timed out
			}

			// Update last index
			c.mu.Lock()
			c.lastIndex = meta.LastIndex
			c.mu.Unlock()

			if pair == nil {
				log.Warn().Str("key", c.cfg.Key).Msg("Consul key deleted")
				continue
			}

			cfg, err := config.ParseBytes(pair.Value)
			if err != nil {
				log.Error().Err(err).Msg("Error parsing config from Consul")
				continue
			}

			log.Debug().
				Str("key", c.cfg.Key).
				Uint64("index", meta.LastIndex).
				Msg("Consul config changed")

			select {
			case ch <- cfg:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// SupportsWatch returns true - Consul natively supports blocking queries.
func (c *ConsulSource) SupportsWatch() bool {
	return true
}

// Validate checks that the key is accessible.
func (c *ConsulSource) Validate(ctx context.Context) error {
	kv := c.client.KV()

	pair, _, err := kv.Get(c.cfg.Key, c.queryOpts.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("validating Consul access: %w", err)
	}

	if pair == nil {
		return fmt.Errorf("key %s not found", c.cfg.Key)
	}

	return nil
}

// Close releases any resources.
func (c *ConsulSource) Close() error {
	close(c.stopCh)
	return nil
}
