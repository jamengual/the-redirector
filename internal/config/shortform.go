package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ShortFormRule represents a compact rule definition.
// Supports formats:
//   - "/old -> https://new.com"
//   - "/old -> https://new.com [301]"
//   - "/old -> https://new.com [301, preserve_path]"
//   - "/old -> https://new.com [preserve_path, preserve_query]"
type ShortFormRule struct {
	Source        string
	Destination   string
	Status        int
	PreservePath  bool
	PreserveQuery bool
	Host          string
}

// shortFormRegex parses: /path -> https://dest [options]
var shortFormRegex = regexp.MustCompile(`^(.+?)\s*->\s*(\S+)(?:\s*\[([^\]]+)\])?$`)

// ParseShortForm parses a short-form rule string.
// Examples:
//   - "/old -> https://new.com"
//   - "/old -> https://new.com [301]"
//   - "/old -> https://new.com [301, preserve_path]"
//   - "old.com:/path -> https://new.com [301]"
func ParseShortForm(line string) (*ShortFormRule, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil, nil // Skip empty lines and comments
	}

	matches := shortFormRegex.FindStringSubmatch(line)
	if matches == nil {
		return nil, fmt.Errorf("invalid short-form rule: %s", line)
	}

	rule := &ShortFormRule{
		Source:      strings.TrimSpace(matches[1]),
		Destination: strings.TrimSpace(matches[2]),
		Status:      301, // Default
	}

	// Parse host:path format
	if idx := strings.Index(rule.Source, ":"); idx > 0 && !strings.HasPrefix(rule.Source, "/") {
		// Check if it looks like host:path (not just a path with colon)
		potentialHost := rule.Source[:idx]
		if !strings.Contains(potentialHost, "/") {
			rule.Host = potentialHost
			rule.Source = rule.Source[idx+1:]
		}
	}

	// Parse options if present
	if matches[3] != "" {
		options := strings.Split(matches[3], ",")
		for _, opt := range options {
			opt = strings.TrimSpace(strings.ToLower(opt))
			switch {
			case opt == "preserve_path" || opt == "preservepath":
				rule.PreservePath = true
			case opt == "preserve_query" || opt == "preservequery":
				rule.PreserveQuery = true
			default:
				// Try to parse as status code
				if status, err := strconv.Atoi(opt); err == nil && status >= 100 && status < 600 {
					rule.Status = status
				}
			}
		}
	}

	return rule, nil
}

// ToRule converts a ShortFormRule to a full Rule.
func (s *ShortFormRule) ToRule(id string) Rule {
	// Determine match type from source pattern
	matchType := MatchTypeExact
	path := s.Source
	pattern := ""

	if strings.Contains(s.Source, "*") {
		// Glob pattern
		matchType = MatchTypeGlob
		pattern = s.Source
		path = ""
	} else if strings.HasPrefix(s.Source, "^") || strings.Contains(s.Source, "(") {
		// Regex pattern
		matchType = MatchTypeRegex
		pattern = s.Source
		path = ""
	} else if strings.HasSuffix(s.Source, "/") && len(s.Source) > 1 {
		// Prefix match (ends with /)
		matchType = MatchTypePrefix
	}

	preserveQuery := s.PreserveQuery
	return Rule{
		ID: id,
		Match: Match{
			Type:    matchType,
			Path:    path,
			Pattern: pattern,
			Host:    s.Host,
		},
		Redirect: Redirect{
			Status:        s.Status,
			To:            s.Destination,
			PreservePath:  s.PreservePath,
			PreserveQuery: &preserveQuery,
		},
	}
}

// ParseCSVLine parses a CSV rule line.
// Formats:
//   - "origin,destination"
//   - "origin,destination,status"
//   - "origin,destination,status,options..."
//
// Options can be: preserve_path, preserve_query
// Example: "/old,https://new.com,301,preserve_path"
func ParseCSVLine(line string) (*ShortFormRule, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil, nil // Skip empty lines and comments
	}

	// Split by comma, but be careful with URLs containing commas (rare but possible)
	parts := strings.Split(line, ",")
	if len(parts) < 2 {
		return nil, fmt.Errorf("CSV line must have at least origin and destination: %s", line)
	}

	rule := &ShortFormRule{
		Source:      strings.TrimSpace(parts[0]),
		Destination: strings.TrimSpace(parts[1]),
		Status:      301, // Default
	}

	// Parse host:path format in source
	if idx := strings.Index(rule.Source, ":"); idx > 0 && !strings.HasPrefix(rule.Source, "/") {
		potentialHost := rule.Source[:idx]
		if !strings.Contains(potentialHost, "/") {
			rule.Host = potentialHost
			rule.Source = rule.Source[idx+1:]
		}
	}

	// Parse remaining fields
	for i := 2; i < len(parts); i++ {
		opt := strings.TrimSpace(strings.ToLower(parts[i]))
		switch {
		case opt == "preserve_path" || opt == "preservepath":
			rule.PreservePath = true
		case opt == "preserve_query" || opt == "preservequery":
			rule.PreserveQuery = true
		default:
			// Try to parse as status code
			if status, err := strconv.Atoi(opt); err == nil && status >= 100 && status < 600 {
				rule.Status = status
			}
		}
	}

	return rule, nil
}

// LoadCSVFile loads rules from a CSV file.
func LoadCSVFile(path string) ([]Rule, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening CSV file: %w", err)
	}
	defer file.Close()

	var rules []Rule
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		shortForm, err := ParseCSVLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}
		if shortForm == nil {
			continue // Skip empty/comment lines
		}

		// Generate ID from source path
		id := generateRuleID(shortForm.Source, lineNum)
		rules = append(rules, shortForm.ToRule(id))
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading CSV file: %w", err)
	}

	return rules, nil
}

// LoadRulesFile loads rules from either CSV or YAML file based on extension.
func LoadRulesFile(path string) ([]Rule, error) {
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".csv":
		return LoadCSVFile(path)
	case ".yaml", ".yml":
		cfg, err := Load(path)
		if err != nil {
			return nil, err
		}
		return cfg.Rules, nil
	default:
		return nil, fmt.Errorf("unsupported file extension: %s (use .csv, .yaml, or .yml)", ext)
	}
}

// generateRuleID creates a rule ID from the source path.
func generateRuleID(source string, lineNum int) string {
	// Clean up the source to make a reasonable ID
	id := strings.TrimPrefix(source, "/")
	id = strings.ReplaceAll(id, "/", "-")
	id = strings.ReplaceAll(id, "*", "x")
	id = strings.ReplaceAll(id, "^", "")
	id = strings.ReplaceAll(id, "$", "")
	id = strings.ReplaceAll(id, "(", "")
	id = strings.ReplaceAll(id, ")", "")

	if id == "" {
		id = fmt.Sprintf("rule-%d", lineNum)
	}

	return id
}

// FlexibleRule can unmarshal from either a full Rule object or a short-form string.
type FlexibleRule struct {
	Rule
}

// UnmarshalYAML implements custom YAML unmarshaling for FlexibleRule.
// It accepts both full rule objects and short-form strings.
func (f *FlexibleRule) UnmarshalYAML(value *yaml.Node) error {
	// Try as string first (short-form)
	if value.Kind == yaml.ScalarNode {
		shortForm, err := ParseShortForm(value.Value)
		if err != nil {
			return err
		}
		if shortForm == nil {
			return fmt.Errorf("empty rule")
		}
		f.Rule = shortForm.ToRule(generateRuleID(shortForm.Source, value.Line))
		return nil
	}

	// Otherwise, parse as full Rule object
	type rawRule Rule // Avoid recursion
	var r rawRule
	if err := value.Decode(&r); err != nil {
		return err
	}
	f.Rule = Rule(r)
	return nil
}

// FlexibleConfig is like Config but supports flexible rule formats.
type FlexibleConfig struct {
	Version      string          `yaml:"version"`
	Server       ServerConfig    `yaml:"server"`
	Defaults     DefaultConfig   `yaml:"defaults"`
	Stats        *StatsConfig    `yaml:"stats"`
	Auth         *AuthConfig     `yaml:"auth"`
	Tracing      *TracingConfig  `yaml:"tracing"`
	RateLimit    *RateLimitConfig `yaml:"rate_limit"`
	Rules        []FlexibleRule  `yaml:"rules"`
	RulesInclude []string        `yaml:"rules_include"`
}

// ToConfig converts FlexibleConfig to regular Config.
func (f *FlexibleConfig) ToConfig() *Config {
	rules := make([]Rule, len(f.Rules))
	for i, fr := range f.Rules {
		rules[i] = fr.Rule
	}

	return &Config{
		Version:   f.Version,
		Server:    f.Server,
		Defaults:  f.Defaults,
		Stats:     f.Stats,
		Auth:      f.Auth,
		Tracing:   f.Tracing,
		RateLimit: f.RateLimit,
		Rules:     rules,
	}
}

// LoadFlexible loads a config file with support for short-form rules and includes.
func LoadFlexible(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	var fcfg FlexibleConfig
	if err := yaml.Unmarshal([]byte(expanded), &fcfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Get directory of config file for relative paths
	baseDir := filepath.Dir(path)

	// Load included rule files
	for _, includePath := range fcfg.RulesInclude {
		// Resolve relative paths
		if !filepath.IsAbs(includePath) {
			includePath = filepath.Join(baseDir, includePath)
		}

		includedRules, err := LoadRulesFile(includePath)
		if err != nil {
			return nil, fmt.Errorf("loading included rules from %s: %w", includePath, err)
		}

		// Append as FlexibleRules
		for _, r := range includedRules {
			fcfg.Rules = append(fcfg.Rules, FlexibleRule{Rule: r})
		}
	}

	cfg := fcfg.ToConfig()

	// Apply defaults and validate
	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

// RulesConfig extends Config to support rule includes.
type RulesConfig struct {
	Config       `yaml:",inline"`
	RulesInclude []string `yaml:"rules_include"` // Paths to CSV or YAML files to include
}

// LoadWithIncludes loads a config file and processes any rule includes.
func LoadWithIncludes(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	// First, try to parse as RulesConfig to check for includes
	var rcfg RulesConfig
	if err := yaml.Unmarshal([]byte(expanded), &rcfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Get directory of config file for relative paths
	baseDir := filepath.Dir(path)

	// Load included rule files
	for _, includePath := range rcfg.RulesInclude {
		// Resolve relative paths
		if !filepath.IsAbs(includePath) {
			includePath = filepath.Join(baseDir, includePath)
		}

		includedRules, err := LoadRulesFile(includePath)
		if err != nil {
			return nil, fmt.Errorf("loading included rules from %s: %w", includePath, err)
		}

		rcfg.Rules = append(rcfg.Rules, includedRules...)
	}

	// Apply defaults and validate
	rcfg.Config.applyDefaults()

	if err := rcfg.Config.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &rcfg.Config, nil
}

