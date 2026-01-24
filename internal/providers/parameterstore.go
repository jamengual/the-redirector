// Package providers contains configuration source implementations.
// This file implements AWS Systems Manager Parameter Store integration.
package providers

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/rs/zerolog/log"

	"github.com/jamengual/the-redirector/internal/config"
)

func init() {
	Registry.Register("parameterstore", NewParameterStoreSourceFromMap)
}

// ParameterStoreSourceConfig configures the Parameter Store source.
type ParameterStoreSourceConfig struct {
	// Path to the parameter or parameter hierarchy
	// Single parameter: /myapp/config
	// Hierarchy: /myapp/redirector/ (trailing slash indicates hierarchy)
	Path string `yaml:"path" json:"path"`

	// Region (optional, uses SDK default if not set)
	Region string `yaml:"region" json:"region"`

	// RoleARN for cross-account access (optional)
	RoleARN string `yaml:"role_arn" json:"role_arn"`

	// WithDecryption enables decryption of SecureString parameters
	// Default: true
	WithDecryption bool `yaml:"with_decryption" json:"with_decryption"`

	// PollInterval for checking changes (default: 1m)
	PollInterval time.Duration `yaml:"poll_interval" json:"poll_interval"`

	// Recursive enables fetching all parameters under the path
	// Only applies when Path ends with /
	Recursive bool `yaml:"recursive" json:"recursive"`
}

// ParameterStoreSource fetches configuration from AWS Parameter Store.
// Supports two modes:
//  1. Single parameter: The parameter value contains the full configuration (YAML/JSON)
//  2. Parameter hierarchy: Multiple parameters are merged into a configuration
type ParameterStoreSource struct {
	cfg          ParameterStoreSourceConfig
	client       *ssm.Client
	lastVersions map[string]int64 // Track parameter versions for change detection
	mu           sync.RWMutex
	stopCh       chan struct{}
	options      SourceOptions
}

// NewParameterStoreSourceFromMap creates a new Parameter Store source from a config map.
// This is the factory function registered with the Registry.
func NewParameterStoreSourceFromMap(cfg map[string]interface{}) (Source, error) {
	path, ok := cfg["path"].(string)
	if !ok || path == "" {
		return nil, fmt.Errorf("parameterstore source requires 'path'")
	}

	sourceCfg := ParameterStoreSourceConfig{
		Path:           path,
		WithDecryption: true, // Default to true
		PollInterval:   time.Minute,
	}

	if region, ok := cfg["region"].(string); ok {
		sourceCfg.Region = region
	}

	if roleARN, ok := cfg["role_arn"].(string); ok {
		sourceCfg.RoleARN = roleARN
	}

	if decrypt, ok := cfg["with_decryption"].(bool); ok {
		sourceCfg.WithDecryption = decrypt
	}

	if interval, ok := cfg["poll_interval"].(string); ok {
		if d, err := time.ParseDuration(interval); err == nil {
			sourceCfg.PollInterval = d
		}
	}

	if recursive, ok := cfg["recursive"].(bool); ok {
		sourceCfg.Recursive = recursive
	}

	return NewParameterStoreSource(context.Background(), sourceCfg)
}

// NewParameterStoreSource creates a new Parameter Store configuration source.
func NewParameterStoreSource(ctx context.Context, cfg ParameterStoreSourceConfig) (*ParameterStoreSource, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = time.Minute
	}

	// Load AWS config
	var opts []func(*awsconfig.LoadOptions) error

	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	// Create SSM client options
	var ssmOpts []func(*ssm.Options)

	// Assume role if specified
	if cfg.RoleARN != "" {
		stsClient := sts.NewFromConfig(awsCfg)
		creds := stscreds.NewAssumeRoleProvider(stsClient, cfg.RoleARN)
		ssmOpts = append(ssmOpts, func(o *ssm.Options) {
			o.Credentials = aws.NewCredentialsCache(creds)
		})
	}

	client := ssm.NewFromConfig(awsCfg, ssmOpts...)

	return &ParameterStoreSource{
		cfg:          cfg,
		client:       client,
		lastVersions: make(map[string]int64),
		stopCh:       make(chan struct{}),
		options:      DefaultSourceOptions(),
	}, nil
}

// Name returns the source name.
func (p *ParameterStoreSource) Name() string {
	return "parameterstore"
}

// Fetch retrieves the configuration from Parameter Store.
func (p *ParameterStoreSource) Fetch(ctx context.Context) (*config.Config, error) {
	var data []byte
	var err error

	if p.isHierarchy() {
		data, err = p.fetchHierarchy(ctx)
	} else {
		data, err = p.fetchSingle(ctx)
	}

	if err != nil {
		return nil, err
	}

	cfg, err := config.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return cfg, nil
}

// isHierarchy returns true if the path represents a parameter hierarchy.
func (p *ParameterStoreSource) isHierarchy() bool {
	return strings.HasSuffix(p.cfg.Path, "/")
}

// fetchSingle retrieves a single parameter containing the full configuration.
func (p *ParameterStoreSource) fetchSingle(ctx context.Context) ([]byte, error) {
	input := &ssm.GetParameterInput{
		Name:           aws.String(p.cfg.Path),
		WithDecryption: aws.Bool(p.cfg.WithDecryption),
	}

	result, err := p.client.GetParameter(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("getting parameter %s: %w", p.cfg.Path, err)
	}

	if result.Parameter == nil || result.Parameter.Value == nil {
		return nil, fmt.Errorf("parameter %s has no value", p.cfg.Path)
	}

	// Track version for change detection
	p.mu.Lock()
	p.lastVersions[p.cfg.Path] = result.Parameter.Version
	p.mu.Unlock()

	log.Debug().
		Str("path", p.cfg.Path).
		Int64("version", result.Parameter.Version).
		Msg("Fetched parameter from Parameter Store")

	return []byte(*result.Parameter.Value), nil
}

// fetchHierarchy retrieves all parameters under a path and merges them.
// Parameters are expected to be in format:
//
//	/myapp/config/rules -> YAML/JSON array of rules
//	/myapp/config/defaults -> YAML/JSON defaults object
//	/myapp/config/server -> YAML/JSON server config
//
// Or a flat structure:
//
//	/myapp/config/rule_1 -> single rule YAML
//	/myapp/config/rule_2 -> single rule YAML
func (p *ParameterStoreSource) fetchHierarchy(ctx context.Context) ([]byte, error) {
	input := &ssm.GetParametersByPathInput{
		Path:           aws.String(p.cfg.Path),
		Recursive:      aws.Bool(p.cfg.Recursive),
		WithDecryption: aws.Bool(p.cfg.WithDecryption),
	}

	var parameters []types.Parameter
	paginator := ssm.NewGetParametersByPathPaginator(p.client, input)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("getting parameters by path: %w", err)
		}
		parameters = append(parameters, page.Parameters...)
	}

	if len(parameters) == 0 {
		return nil, fmt.Errorf("no parameters found under path %s", p.cfg.Path)
	}

	// Track versions for change detection
	p.mu.Lock()
	for _, param := range parameters {
		if param.Name != nil {
			p.lastVersions[*param.Name] = param.Version
		}
	}
	p.mu.Unlock()

	// Build configuration from parameters
	return p.mergeParameters(parameters)
}

// mergeParameters combines multiple parameters into a single configuration.
// It looks for specific parameter names:
//   - "config" or "main": Full configuration (highest priority)
//   - "rules": Rules array to merge
//   - "defaults": Defaults object
//   - "server": Server configuration
//
// If none of these patterns match, it treats all parameters as rules.
func (p *ParameterStoreSource) mergeParameters(parameters []types.Parameter) ([]byte, error) {
	// Look for a main config parameter first
	for _, param := range parameters {
		if param.Name == nil || param.Value == nil {
			continue
		}

		name := paramName(*param.Name, p.cfg.Path)
		if name == "config" || name == "main" {
			log.Debug().
				Str("path", *param.Name).
				Msg("Using main config parameter")
			return []byte(*param.Value), nil
		}
	}

	// Build composite configuration from parts
	var builder strings.Builder
	builder.WriteString("version: \"1.0\"\n")

	// Collect configuration sections
	var rulesContent []string
	var serverContent, defaultsContent string

	for _, param := range parameters {
		if param.Name == nil || param.Value == nil {
			continue
		}

		name := paramName(*param.Name, p.cfg.Path)
		value := *param.Value

		switch name {
		case "server":
			serverContent = value
		case "defaults":
			defaultsContent = value
		case "rules":
			// Assume this is a YAML/JSON array of rules
			rulesContent = append(rulesContent, value)
		default:
			// Treat as individual rule content
			rulesContent = append(rulesContent, value)
		}

		log.Debug().
			Str("path", *param.Name).
			Str("name", name).
			Msg("Processed parameter")
	}

	// Write server section
	if serverContent != "" {
		builder.WriteString("\nserver:\n")
		for _, line := range strings.Split(serverContent, "\n") {
			if line != "" {
				builder.WriteString("  ")
				builder.WriteString(line)
				builder.WriteString("\n")
			}
		}
	}

	// Write defaults section
	if defaultsContent != "" {
		builder.WriteString("\ndefaults:\n")
		for _, line := range strings.Split(defaultsContent, "\n") {
			if line != "" {
				builder.WriteString("  ")
				builder.WriteString(line)
				builder.WriteString("\n")
			}
		}
	}

	// Write rules section
	if len(rulesContent) > 0 {
		builder.WriteString("\nrules:\n")
		for _, ruleContent := range rulesContent {
			// Each rule content might be a single rule or multiple rules
			lines := strings.Split(ruleContent, "\n")
			for _, line := range lines {
				if line != "" {
					builder.WriteString("  ")
					builder.WriteString(line)
					builder.WriteString("\n")
				}
			}
		}
	}

	return []byte(builder.String()), nil
}

// paramName extracts the parameter name relative to the base path.
func paramName(fullPath, basePath string) string {
	name := strings.TrimPrefix(fullPath, basePath)
	name = strings.TrimPrefix(name, "/")
	return name
}

// Watch polls Parameter Store for changes and returns configs on a channel.
func (p *ParameterStoreSource) Watch(ctx context.Context) (<-chan *config.Config, error) {
	ch := make(chan *config.Config, 1)

	go func() {
		defer close(ch)

		ticker := time.NewTicker(p.cfg.PollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-p.stopCh:
				return
			case <-ticker.C:
				changed, err := p.hasChanged(ctx)
				if err != nil {
					log.Error().Err(err).Msg("Error checking Parameter Store for changes")
					continue
				}

				if !changed {
					continue
				}

				cfg, err := p.Fetch(ctx)
				if err != nil {
					log.Error().Err(err).Msg("Error fetching config from Parameter Store")
					continue
				}

				select {
				case ch <- cfg:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch, nil
}

// hasChanged checks if any parameters have changed since last fetch.
func (p *ParameterStoreSource) hasChanged(ctx context.Context) (bool, error) {
	p.mu.RLock()
	lastVersions := make(map[string]int64, len(p.lastVersions))
	for k, v := range p.lastVersions {
		lastVersions[k] = v
	}
	p.mu.RUnlock()

	if len(lastVersions) == 0 {
		return true, nil // Never fetched
	}

	if p.isHierarchy() {
		return p.hasHierarchyChanged(ctx, lastVersions)
	}

	return p.hasSingleChanged(ctx, lastVersions)
}

// hasSingleChanged checks if a single parameter has changed.
func (p *ParameterStoreSource) hasSingleChanged(ctx context.Context, lastVersions map[string]int64) (bool, error) {
	input := &ssm.GetParameterInput{
		Name:           aws.String(p.cfg.Path),
		WithDecryption: aws.Bool(false), // Don't need value, just metadata
	}

	result, err := p.client.GetParameter(ctx, input)
	if err != nil {
		return false, fmt.Errorf("getting parameter: %w", err)
	}

	if result.Parameter == nil {
		return false, nil
	}

	lastVersion, exists := lastVersions[p.cfg.Path]
	if !exists {
		return true, nil
	}

	changed := result.Parameter.Version != lastVersion

	if changed {
		log.Debug().
			Str("path", p.cfg.Path).
			Int64("old_version", lastVersion).
			Int64("new_version", result.Parameter.Version).
			Msg("Parameter Store config changed")
	}

	return changed, nil
}

// hasHierarchyChanged checks if any parameter in the hierarchy has changed.
func (p *ParameterStoreSource) hasHierarchyChanged(ctx context.Context, lastVersions map[string]int64) (bool, error) {
	input := &ssm.GetParametersByPathInput{
		Path:           aws.String(p.cfg.Path),
		Recursive:      aws.Bool(p.cfg.Recursive),
		WithDecryption: aws.Bool(false),
	}

	paginator := ssm.NewGetParametersByPathPaginator(p.client, input)

	currentParams := make(map[string]int64)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return false, fmt.Errorf("getting parameters by path: %w", err)
		}

		for _, param := range page.Parameters {
			if param.Name != nil {
				currentParams[*param.Name] = param.Version
			}
		}
	}

	// Check for new or modified parameters
	for name, version := range currentParams {
		lastVersion, exists := lastVersions[name]
		if !exists || version != lastVersion {
			log.Debug().
				Str("path", name).
				Int64("old_version", lastVersion).
				Int64("new_version", version).
				Bool("is_new", !exists).
				Msg("Parameter changed")
			return true, nil
		}
	}

	// Check for deleted parameters
	for name := range lastVersions {
		if _, exists := currentParams[name]; !exists {
			log.Debug().
				Str("path", name).
				Msg("Parameter deleted")
			return true, nil
		}
	}

	return false, nil
}

// SupportsWatch returns true - Parameter Store uses polling.
func (p *ParameterStoreSource) SupportsWatch() bool {
	return true
}

// Validate checks that the Parameter Store path is accessible.
func (p *ParameterStoreSource) Validate(ctx context.Context) error {
	if p.isHierarchy() {
		input := &ssm.GetParametersByPathInput{
			Path:           aws.String(p.cfg.Path),
			Recursive:      aws.Bool(false),
			MaxResults:     aws.Int32(1),
			WithDecryption: aws.Bool(false),
		}

		result, err := p.client.GetParametersByPath(ctx, input)
		if err != nil {
			return fmt.Errorf("validating Parameter Store access: %w", err)
		}

		if len(result.Parameters) == 0 {
			return fmt.Errorf("no parameters found under path %s", p.cfg.Path)
		}

		return nil
	}

	input := &ssm.GetParameterInput{
		Name:           aws.String(p.cfg.Path),
		WithDecryption: aws.Bool(false),
	}

	_, err := p.client.GetParameter(ctx, input)
	if err != nil {
		return fmt.Errorf("validating Parameter Store access: %w", err)
	}

	return nil
}

// Close releases any resources.
func (p *ParameterStoreSource) Close() error {
	close(p.stopCh)
	return nil
}
