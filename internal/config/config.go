package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the complete redirector configuration.
type Config struct {
	Version  string        `yaml:"version"`
	Server   ServerConfig  `yaml:"server"`
	Defaults DefaultConfig `yaml:"defaults"`
	Stats    *StatsConfig  `yaml:"stats"`
	Rules    []Rule        `yaml:"rules"`
}

// StatsConfig configures the statistics collector.
type StatsConfig struct {
	Enabled      bool    `yaml:"enabled"`
	BufferSize   int     `yaml:"buffer_size"`
	SamplingRate float64 `yaml:"sampling_rate"`
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	Port           int    `yaml:"port"`
	ManagementPort int    `yaml:"management_port"`
	ReadTimeout    string `yaml:"read_timeout"`
	WriteTimeout   string `yaml:"write_timeout"`
	IdleTimeout    string `yaml:"idle_timeout"`
	MaxConnections int    `yaml:"max_connections"`
}

// DefaultConfig contains default values for redirects.
type DefaultConfig struct {
	StatusCode    int               `yaml:"status_code"`
	PreserveQuery bool              `yaml:"preserve_query"`
	Headers       map[string]string `yaml:"headers"`
}

// Rule represents a single redirect rule.
type Rule struct {
	ID       string   `yaml:"id"`
	Match    Match    `yaml:"match"`
	Redirect Redirect `yaml:"redirect"`
	Priority int      `yaml:"priority"`

	// Compiled regex (populated during validation)
	compiledRegex *regexp.Regexp
}

// Match defines how to match incoming requests.
type Match struct {
	Type       MatchType         `yaml:"type"`
	Path       string            `yaml:"path"`
	Pattern    string            `yaml:"pattern"`
	Host       string            `yaml:"host"`
	Conditions map[string]string `yaml:"conditions"`
}

// MatchType defines the type of matching.
type MatchType string

const (
	MatchTypeExact  MatchType = "exact"
	MatchTypePrefix MatchType = "prefix"
	MatchTypeRegex  MatchType = "regex"
	MatchTypeGlob   MatchType = "glob"
)

// Response defines the response behavior. Supports any HTTP status code.
// For redirects (3xx), use Location field. For other statuses, use Body.
type Response struct {
	Status        int               `yaml:"status"`         // Any HTTP status (301, 302, 404, 503, etc.)
	Location      string            `yaml:"location"`       // Redirect destination (for 3xx)
	To            string            `yaml:"to"`             // Alias for Location (backwards compat)
	Body          string            `yaml:"body"`           // Response body (for non-redirects)
	PreservePath  bool              `yaml:"preserve_path"`  // Append matched path to destination
	PreserveQuery *bool             `yaml:"preserve_query"` // Forward query string
	Headers       map[string]string `yaml:"headers"`        // Custom response headers
}

// Redirect is an alias for Response for backwards compatibility.
type Redirect = Response

// GetLocation returns the redirect destination, checking both Location and To fields.
func (r *Response) GetLocation() string {
	if r.Location != "" {
		return r.Location
	}
	return r.To
}

// IsRedirect returns true if the status code is a redirect (3xx).
func (r *Response) IsRedirect() bool {
	return r.Status >= 300 && r.Status < 400
}

// Load reads and parses a configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Apply defaults
	cfg.applyDefaults()

	// Validate and compile patterns
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

// ParseBytes parses configuration from raw bytes.
// This is useful for sources that fetch config content directly (e.g., GitHub, S3).
func ParseBytes(data []byte) (*Config, error) {
	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Apply defaults
	cfg.applyDefaults()

	// Validate and compile patterns
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

// LoadDirectory loads all YAML files from a directory and merges them.
// Files are loaded in alphabetical order by full path.
// Later files can override rules with the same ID.
func LoadDirectory(path string) (*Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("accessing path: %w", err)
	}

	// If it's a file, just load it directly
	if !info.IsDir() {
		return Load(path)
	}

	var merged Config
	var files []string

	// Collect all YAML files
	err = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && (strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml")) {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}

	// Sort files for deterministic order
	sort.Strings(files)

	// Load and merge each file
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", file, err)
		}

		expanded := os.ExpandEnv(string(data))

		var cfg Config
		if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", file, err)
		}

		// Merge rules
		merged.Rules = append(merged.Rules, cfg.Rules...)

		// Take server config from first file that has it
		if merged.Server.Port == 0 && cfg.Server.Port != 0 {
			merged.Server = cfg.Server
		}

		// Merge defaults (later files override)
		if cfg.Defaults.StatusCode != 0 {
			merged.Defaults = cfg.Defaults
		}

		// Merge stats config (later files override)
		if cfg.Stats != nil {
			merged.Stats = cfg.Stats
		}

		// Take version from first file
		if merged.Version == "" && cfg.Version != "" {
			merged.Version = cfg.Version
		}
	}

	// Apply defaults and validate
	merged.applyDefaults()

	if err := merged.Validate(); err != nil {
		return nil, fmt.Errorf("validating merged config: %w", err)
	}

	return &merged, nil
}

// applyDefaults sets default values for missing configuration.
func (c *Config) applyDefaults() {
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Server.ManagementPort == 0 {
		c.Server.ManagementPort = 8081
	}
	if c.Defaults.StatusCode == 0 {
		c.Defaults.StatusCode = 301
	}

	for i := range c.Rules {
		rule := &c.Rules[i]

		// Generate ID if not provided
		if rule.ID == "" {
			rule.ID = fmt.Sprintf("rule-%d", i+1)
		}

		// Default match type
		if rule.Match.Type == "" {
			rule.Match.Type = MatchTypeExact
		}

		// Default status code
		if rule.Redirect.Status == 0 {
			rule.Redirect.Status = c.Defaults.StatusCode
		}

		// Inherit preserve_query from defaults if not explicitly set
		if rule.Redirect.PreserveQuery == nil {
			rule.Redirect.PreserveQuery = &c.Defaults.PreserveQuery
		}
	}
}

// Validate checks the configuration for errors and compiles patterns.
func (c *Config) Validate() error {
	if len(c.Rules) == 0 {
		return fmt.Errorf("no rules defined")
	}

	seenIDs := make(map[string]bool)

	for i := range c.Rules {
		rule := &c.Rules[i]

		// Check for duplicate IDs
		if seenIDs[rule.ID] {
			return fmt.Errorf("duplicate rule ID: %s", rule.ID)
		}
		seenIDs[rule.ID] = true

		// Validate status code (any valid HTTP status)
		if rule.Redirect.Status < 100 || rule.Redirect.Status >= 600 {
			return fmt.Errorf("rule %s: invalid HTTP status %d", rule.ID, rule.Redirect.Status)
		}

		// For redirect statuses (3xx), require a destination
		if rule.Redirect.IsRedirect() {
			if rule.Redirect.GetLocation() == "" {
				return fmt.Errorf("rule %s: redirect status %d requires 'to' or 'location'", rule.ID, rule.Redirect.Status)
			}
		}

		// Validate match configuration
		switch rule.Match.Type {
		case MatchTypeExact, MatchTypePrefix:
			if rule.Match.Path == "" {
				return fmt.Errorf("rule %s: path is required for %s match", rule.ID, rule.Match.Type)
			}

		case MatchTypeRegex:
			if rule.Match.Pattern == "" {
				return fmt.Errorf("rule %s: pattern is required for regex match", rule.ID)
			}
			regex, err := regexp.Compile(rule.Match.Pattern)
			if err != nil {
				return fmt.Errorf("rule %s: invalid regex pattern: %w", rule.ID, err)
			}
			rule.compiledRegex = regex

		case MatchTypeGlob:
			if rule.Match.Pattern == "" {
				return fmt.Errorf("rule %s: pattern is required for glob match", rule.ID)
			}
			// Convert glob to regex
			regexPattern := globToRegex(rule.Match.Pattern)
			regex, err := regexp.Compile(regexPattern)
			if err != nil {
				return fmt.Errorf("rule %s: invalid glob pattern: %w", rule.ID, err)
			}
			rule.compiledRegex = regex

		default:
			return fmt.Errorf("rule %s: unknown match type: %s", rule.ID, rule.Match.Type)
		}
	}

	return nil
}

// globToRegex converts a glob pattern to a regular expression.
func globToRegex(glob string) string {
	var result strings.Builder
	result.WriteString("^")

	i := 0
	for i < len(glob) {
		switch glob[i] {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				// ** matches any path segments
				result.WriteString(".*")
				i += 2
				// Skip trailing slash after **
				if i < len(glob) && glob[i] == '/' {
					i++
				}
			} else {
				// * matches single path segment (no slashes)
				result.WriteString("[^/]*")
				i++
			}
		case '?':
			result.WriteString("[^/]")
			i++
		case '.', '+', '^', '$', '(', ')', '{', '}', '[', ']', '|', '\\':
			result.WriteString("\\")
			result.WriteByte(glob[i])
			i++
		default:
			result.WriteByte(glob[i])
			i++
		}
	}

	result.WriteString("$")
	return result.String()
}

// CompiledRegex returns the compiled regex for regex/glob match types.
func (r *Rule) CompiledRegex() *regexp.Regexp {
	return r.compiledRegex
}
