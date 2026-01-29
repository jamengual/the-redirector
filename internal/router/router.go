package router

import (
	"net"
	"sort"
	"strings"

	"github.com/jamengual/the-redirector/internal/config"
)

// Router handles matching incoming requests to redirect rules.
// It uses a combination of exact match maps, prefix trees, and regex lists
// for efficient matching with different rule types.
type Router struct {
	// Exact path matches (fastest)
	exactMatches map[string]*config.Rule

	// Prefix matches sorted by length (longer first)
	prefixMatches []*config.Rule

	// Regex/glob matches (evaluated in order)
	regexMatches []*config.Rule

	// Host allowlist for early rejection of unknown hosts
	allowedHosts map[string]struct{}
	allowAnyHost bool
}

// New creates a new router from the given rules.
func New(rules []config.Rule) (*Router, error) {
	r := &Router{
		exactMatches:  make(map[string]*config.Rule),
		prefixMatches: make([]*config.Rule, 0),
		regexMatches:  make([]*config.Rule, 0),
		allowedHosts:  make(map[string]struct{}),
	}

	// Sort rules by priority (higher first), then by specificity
	sortedRules := make([]config.Rule, len(rules))
	copy(sortedRules, rules)
	sort.SliceStable(sortedRules, func(i, j int) bool {
		// Higher priority first
		if sortedRules[i].Priority != sortedRules[j].Priority {
			return sortedRules[i].Priority > sortedRules[j].Priority
		}
		// For prefix matches, longer paths first
		if sortedRules[i].Match.Type == config.MatchTypePrefix &&
			sortedRules[j].Match.Type == config.MatchTypePrefix {
			return len(sortedRules[i].Match.Path) > len(sortedRules[j].Match.Path)
		}
		return false
	})

	// Categorize rules by match type
	for i := range sortedRules {
		rule := &sortedRules[i]

		switch rule.Match.Type {
		case config.MatchTypeExact:
			// Use the path as the key, including host if specified
			key := buildMatchKey(rule.Match.Host, rule.Match.Path)
			r.exactMatches[key] = rule

		case config.MatchTypePrefix:
			r.prefixMatches = append(r.prefixMatches, rule)

		case config.MatchTypeRegex, config.MatchTypeGlob:
			r.regexMatches = append(r.regexMatches, rule)
		}
	}

	// Build host allowlist from rules
	for i := range sortedRules {
		if sortedRules[i].Match.Host == "" {
			r.allowAnyHost = true
		} else {
			r.allowedHosts[sortedRules[i].Match.Host] = struct{}{}
		}
	}

	return r, nil
}

// IsAllowedHost reports whether the given host is present in the allowlist.
// Returns true if allowAnyHost is set (a rule with empty host exists) or if the
// host (after stripping any port) appears in the allowedHosts map.
func (r *Router) IsAllowedHost(host string) bool {
	if r.allowAnyHost {
		return true
	}
	host = stripPort(host)
	_, ok := r.allowedHosts[host]
	return ok
}

// AllowedHosts returns the list of explicitly allowed hostnames.
func (r *Router) AllowedHosts() []string {
	hosts := make([]string, 0, len(r.allowedHosts))
	for h := range r.allowedHosts {
		hosts = append(hosts, h)
	}
	return hosts
}

// stripPort removes the port suffix from a host string.
// It handles both plain hosts ("example.com:8080") and IPv6 addresses ("[::1]:8080").
func stripPort(host string) string {
	// Use net.SplitHostPort which correctly handles IPv6.
	// It returns an error for hosts without a port, in which case we return as-is.
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		return host
	}
	return h
}

// Match finds the best matching rule for the given host and path.
// Returns the matched rule and any capture groups from regex matching.
func (r *Router) Match(host, path string) (*config.Rule, []string) {
	// 1. Try exact match first (O(1))
	if rule := r.matchExact(host, path); rule != nil {
		return rule, nil
	}

	// 2. Try prefix match (O(n) where n is number of prefix rules)
	if rule := r.matchPrefix(host, path); rule != nil {
		return rule, nil
	}

	// 3. Try regex/glob match (O(n) where n is number of regex rules)
	if rule, captures := r.matchRegex(host, path); rule != nil {
		return rule, captures
	}

	return nil, nil
}

// matchExact performs exact path matching.
func (r *Router) matchExact(host, path string) *config.Rule {
	// Try with host first
	if host != "" {
		key := buildMatchKey(host, path)
		if rule, ok := r.exactMatches[key]; ok {
			return rule
		}
	}

	// Try without host
	key := buildMatchKey("", path)
	if rule, ok := r.exactMatches[key]; ok {
		return rule
	}

	return nil
}

// matchPrefix performs prefix path matching.
// Rules are pre-sorted by length, so first match is the most specific.
func (r *Router) matchPrefix(host, path string) *config.Rule {
	for _, rule := range r.prefixMatches {
		// Check host if specified
		if rule.Match.Host != "" && rule.Match.Host != host {
			continue
		}

		// Check if path starts with the prefix
		if strings.HasPrefix(path, rule.Match.Path) {
			return rule
		}
	}

	return nil
}

// matchRegex performs regex/glob pattern matching.
// Returns the rule and any captured groups.
func (r *Router) matchRegex(host, path string) (*config.Rule, []string) {
	for _, rule := range r.regexMatches {
		// Check host if specified
		if rule.Match.Host != "" && rule.Match.Host != host {
			continue
		}

		regex := rule.CompiledRegex()
		if regex == nil {
			continue
		}

		matches := regex.FindStringSubmatch(path)
		if matches != nil {
			// Return captured groups (excluding full match)
			if len(matches) > 1 {
				return rule, matches[1:]
			}
			return rule, nil
		}
	}

	return nil, nil
}

// buildMatchKey creates a unique key for exact matching.
func buildMatchKey(host, path string) string {
	if host == "" {
		return path
	}
	return host + ":" + path
}

// Stats returns statistics about the router.
type Stats struct {
	ExactRules        int
	PrefixRules       int
	RegexRules        int
	TotalRules        int
	AllowedHostsCount int
	AllowAnyHost      bool
}

// GetStats returns statistics about the router's rule distribution.
func (r *Router) GetStats() Stats {
	return Stats{
		ExactRules:        len(r.exactMatches),
		PrefixRules:       len(r.prefixMatches),
		RegexRules:        len(r.regexMatches),
		TotalRules:        len(r.exactMatches) + len(r.prefixMatches) + len(r.regexMatches),
		AllowedHostsCount: len(r.allowedHosts),
		AllowAnyHost:      r.allowAnyHost,
	}
}
