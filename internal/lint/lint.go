// Package lint provides configuration validation and analysis.
package lint

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/jamengual/the-redirector/internal/config"
)

// Severity indicates the severity of an issue.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Issue represents a linting issue.
type Issue struct {
	Severity   Severity `json:"severity"`
	RuleID     string   `json:"rule_id,omitempty"`
	File       string   `json:"file,omitempty"`
	Line       int      `json:"line,omitempty"`
	Message    string   `json:"message"`
	Suggestion string   `json:"suggestion,omitempty"`
}

// Result contains all linting results.
type Result struct {
	Issues     []Issue `json:"issues"`
	RulesCount int     `json:"rules_count"`
	FilesCount int     `json:"files_count"`
}

// HasErrors returns true if there are any error-severity issues.
func (r *Result) HasErrors() bool {
	for _, issue := range r.Issues {
		if issue.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Errors returns only error-severity issues.
func (r *Result) Errors() []Issue {
	var errors []Issue
	for _, issue := range r.Issues {
		if issue.Severity == SeverityError {
			errors = append(errors, issue)
		}
	}
	return errors
}

// Warnings returns only warning-severity issues.
func (r *Result) Warnings() []Issue {
	var warnings []Issue
	for _, issue := range r.Issues {
		if issue.Severity == SeverityWarning {
			warnings = append(warnings, issue)
		}
	}
	return warnings
}

// Linter validates and analyzes configuration.
type Linter struct {
	cfg *config.Config
}

// New creates a new linter.
func New(cfg *config.Config) *Linter {
	return &Linter{cfg: cfg}
}

// Lint runs all checks and returns the result.
func (l *Linter) Lint() *Result {
	result := &Result{
		RulesCount: len(l.cfg.Rules),
	}

	// Run all checks
	result.Issues = append(result.Issues, l.checkDuplicateIDs()...)
	result.Issues = append(result.Issues, l.checkOverlappingPatterns()...)
	result.Issues = append(result.Issues, l.checkGreedyPatterns()...)
	result.Issues = append(result.Issues, l.checkRegexPerformance()...)
	result.Issues = append(result.Issues, l.checkUnreachableRules()...)
	result.Issues = append(result.Issues, l.checkMissingDefaults()...)
	result.Issues = append(result.Issues, l.checkCircularRedirects()...)

	// Sort issues by severity
	sort.SliceStable(result.Issues, func(i, j int) bool {
		severityOrder := map[Severity]int{
			SeverityError:   0,
			SeverityWarning: 1,
			SeverityInfo:    2,
		}
		return severityOrder[result.Issues[i].Severity] < severityOrder[result.Issues[j].Severity]
	})

	return result
}

// checkDuplicateIDs finds rules with duplicate IDs.
func (l *Linter) checkDuplicateIDs() []Issue {
	var issues []Issue
	seen := make(map[string]int) // ID -> first occurrence index

	for i, rule := range l.cfg.Rules {
		if firstIdx, exists := seen[rule.ID]; exists {
			issues = append(issues, Issue{
				Severity: SeverityError,
				RuleID:   rule.ID,
				Message:  fmt.Sprintf("Duplicate rule ID '%s' (first seen at index %d)", rule.ID, firstIdx),
			})
		} else {
			seen[rule.ID] = i
		}
	}

	return issues
}

// checkOverlappingPatterns finds rules that may match the same paths.
func (l *Linter) checkOverlappingPatterns() []Issue {
	var issues []Issue

	for i, rule1 := range l.cfg.Rules {
		for j, rule2 := range l.cfg.Rules {
			if i >= j {
				continue
			}

			if overlap := l.detectOverlap(rule1, rule2); overlap != "" {
				// Only warn if priorities are the same
				if rule1.Priority == rule2.Priority {
					issues = append(issues, Issue{
						Severity:   SeverityWarning,
						RuleID:     rule1.ID,
						Message:    fmt.Sprintf("Rule '%s' may overlap with '%s': %s", rule1.ID, rule2.ID, overlap),
						Suggestion: "Consider setting different priorities to control matching order",
					})
				}
			}
		}
	}

	return issues
}

// detectOverlap checks if two rules may match the same paths.
func (l *Linter) detectOverlap(r1, r2 config.Rule) string {
	// Exact matches don't overlap unless identical
	if r1.Match.Type == config.MatchTypeExact && r2.Match.Type == config.MatchTypeExact {
		if r1.Match.Path == r2.Match.Path {
			return fmt.Sprintf("Both match exact path '%s'", r1.Match.Path)
		}
		return ""
	}

	// Prefix overlaps
	if r1.Match.Type == config.MatchTypePrefix && r2.Match.Type == config.MatchTypePrefix {
		if strings.HasPrefix(r1.Match.Path, r2.Match.Path) {
			return fmt.Sprintf("Prefix '%s' is contained in '%s'", r2.Match.Path, r1.Match.Path)
		}
		if strings.HasPrefix(r2.Match.Path, r1.Match.Path) {
			return fmt.Sprintf("Prefix '%s' is contained in '%s'", r1.Match.Path, r2.Match.Path)
		}
	}

	// Prefix vs exact
	if r1.Match.Type == config.MatchTypePrefix && r2.Match.Type == config.MatchTypeExact {
		if strings.HasPrefix(r2.Match.Path, r1.Match.Path) {
			return fmt.Sprintf("Prefix '%s' matches exact path '%s'", r1.Match.Path, r2.Match.Path)
		}
	}
	if r2.Match.Type == config.MatchTypePrefix && r1.Match.Type == config.MatchTypeExact {
		if strings.HasPrefix(r1.Match.Path, r2.Match.Path) {
			return fmt.Sprintf("Prefix '%s' matches exact path '%s'", r2.Match.Path, r1.Match.Path)
		}
	}

	// Glob catch-all overlaps
	if r1.Match.Type == config.MatchTypeGlob && strings.Contains(r1.Match.Pattern, "**") {
		return fmt.Sprintf("Glob pattern '%s' may catch paths meant for other rules", r1.Match.Pattern)
	}
	if r2.Match.Type == config.MatchTypeGlob && strings.Contains(r2.Match.Pattern, "**") {
		return fmt.Sprintf("Glob pattern '%s' may catch paths meant for other rules", r2.Match.Pattern)
	}

	return ""
}

// checkGreedyPatterns finds overly greedy patterns.
func (l *Linter) checkGreedyPatterns() []Issue {
	var issues []Issue

	for _, rule := range l.cfg.Rules {
		switch rule.Match.Type {
		case config.MatchTypeGlob:
			if rule.Match.Pattern == "/**" || rule.Match.Pattern == "/*" {
				if rule.Priority >= 0 {
					issues = append(issues, Issue{
						Severity:   SeverityWarning,
						RuleID:     rule.ID,
						Message:    fmt.Sprintf("Greedy glob pattern '%s' will match many paths", rule.Match.Pattern),
						Suggestion: "Set a negative priority (e.g., -100) to ensure it's evaluated last",
					})
				}
			}

		case config.MatchTypeRegex:
			if strings.HasPrefix(rule.Match.Pattern, ".*") || rule.Match.Pattern == ".*" {
				issues = append(issues, Issue{
					Severity:   SeverityWarning,
					RuleID:     rule.ID,
					Message:    "Pattern starting with '.*' is greedy and slow",
					Suggestion: "Anchor with ^ or use a more specific prefix",
				})
			}

		case config.MatchTypePrefix:
			if rule.Match.Path == "/" && rule.Priority >= 0 {
				issues = append(issues, Issue{
					Severity:   SeverityWarning,
					RuleID:     rule.ID,
					Message:    "Prefix '/' matches all paths",
					Suggestion: "Set a negative priority to ensure it's evaluated last",
				})
			}
		}
	}

	return issues
}

// checkRegexPerformance analyzes regex patterns for performance issues.
func (l *Linter) checkRegexPerformance() []Issue {
	var issues []Issue

	for _, rule := range l.cfg.Rules {
		if rule.Match.Type != config.MatchTypeRegex {
			continue
		}

		pattern := rule.Match.Pattern

		// Check for multiple .* (exponential backtracking)
		if strings.Count(pattern, ".*") >= 2 {
			issues = append(issues, Issue{
				Severity:   SeverityWarning,
				RuleID:     rule.ID,
				Message:    "Multiple '.*' in pattern can cause exponential backtracking",
				Suggestion: "Use non-greedy '.*?' or be more specific with character classes",
			})
		}

		// Check for nested quantifiers (catastrophic backtracking)
		nestedQuantifier := regexp.MustCompile(`\([^)]*[+*][^)]*\)[+*]`)
		if nestedQuantifier.MatchString(pattern) {
			issues = append(issues, Issue{
				Severity:   SeverityError,
				RuleID:     rule.ID,
				Message:    "Nested quantifiers can cause catastrophic backtracking",
				Suggestion: "Restructure the pattern to avoid nested repetition",
			})
		}

		// Check for unanchored patterns
		if !strings.HasPrefix(pattern, "^") && !strings.HasPrefix(pattern, "/") {
			issues = append(issues, Issue{
				Severity:   SeverityInfo,
				RuleID:     rule.ID,
				Message:    "Pattern is not anchored to start",
				Suggestion: "Add ^ to anchor pattern and improve performance",
			})
		}

		// Check for inefficient character classes
		if strings.Contains(pattern, "[^/]*") {
			// This is actually good, but let's suggest it for .*
			continue
		}
		if strings.Contains(pattern, ".*") && !strings.Contains(pattern, "[^/]") {
			issues = append(issues, Issue{
				Severity:   SeverityInfo,
				RuleID:     rule.ID,
				Message:    "Pattern uses '.*' which matches across path segments",
				Suggestion: "Use '[^/]*' to match within a single path segment",
			})
		}

		// Check pattern complexity
		if len(pattern) > 100 {
			issues = append(issues, Issue{
				Severity:   SeverityInfo,
				RuleID:     rule.ID,
				Message:    fmt.Sprintf("Pattern is very long (%d chars)", len(pattern)),
				Suggestion: "Consider breaking into multiple simpler rules",
			})
		}
	}

	return issues
}

// checkUnreachableRules finds rules that will never be matched.
func (l *Linter) checkUnreachableRules() []Issue {
	var issues []Issue

	// Sort rules by priority to simulate actual matching order
	sortedRules := make([]config.Rule, len(l.cfg.Rules))
	copy(sortedRules, l.cfg.Rules)
	sort.SliceStable(sortedRules, func(i, j int) bool {
		return sortedRules[i].Priority > sortedRules[j].Priority
	})

	// Check if earlier rules shadow later ones
	for i, rule := range sortedRules {
		if rule.Match.Type == config.MatchTypeExact {
			continue // Exact matches are fine
		}

		// Check if this rule shadows any later exact matches
		for j := i + 1; j < len(sortedRules); j++ {
			later := sortedRules[j]
			if later.Match.Type != config.MatchTypeExact {
				continue
			}

			// Check if the prefix/glob/regex would match the exact path
			if l.wouldMatch(rule, later.Match.Path) {
				issues = append(issues, Issue{
					Severity:   SeverityWarning,
					RuleID:     later.ID,
					Message:    fmt.Sprintf("Rule '%s' may be unreachable - shadowed by '%s'", later.ID, rule.ID),
					Suggestion: fmt.Sprintf("Rule '%s' matches path '%s' first", rule.ID, later.Match.Path),
				})
			}
		}
	}

	return issues
}

// wouldMatch checks if a rule would match a given path.
func (l *Linter) wouldMatch(rule config.Rule, path string) bool {
	switch rule.Match.Type {
	case config.MatchTypePrefix:
		return strings.HasPrefix(path, rule.Match.Path)
	case config.MatchTypeGlob:
		// Simple glob check for /**
		if strings.HasSuffix(rule.Match.Pattern, "/**") {
			prefix := strings.TrimSuffix(rule.Match.Pattern, "/**")
			return strings.HasPrefix(path, prefix)
		}
	case config.MatchTypeRegex:
		if re := rule.CompiledRegex(); re != nil {
			return re.MatchString(path)
		}
	}
	return false
}

// checkMissingDefaults finds rules that might benefit from defaults.
func (l *Linter) checkMissingDefaults() []Issue {
	var issues []Issue

	// Count rules without explicit status
	noStatus := 0
	for _, rule := range l.cfg.Rules {
		if rule.Redirect.Status == 0 {
			noStatus++
		}
	}

	if noStatus > 0 && l.cfg.Defaults.StatusCode == 0 {
		issues = append(issues, Issue{
			Severity:   SeverityInfo,
			Message:    fmt.Sprintf("%d rules have no explicit status code and no default is set", noStatus),
			Suggestion: "Set defaults.status_code in your config",
		})
	}

	return issues
}

// checkCircularRedirects detects redirect loops by building a directed graph
// and running DFS cycle detection. Exact and prefix rules produce error-severity
// findings; regex/glob rules produce warnings (best-effort sample tracing).
func (l *Linter) checkCircularRedirects() []Issue {
	var issues []Issue

	// Only consider redirect rules (3xx status)
	redirectRules := make([]config.Rule, 0, len(l.cfg.Rules))
	for _, rule := range l.cfg.Rules {
		if rule.Redirect.IsRedirect() {
			redirectRules = append(redirectRules, rule)
		}
	}

	if len(redirectRules) == 0 {
		return nil
	}

	// Collect all hosts from rules to determine which destinations are "local"
	localHosts := l.collectLocalHosts()

	// Build adjacency list: rule index -> list of rule indices it redirects to
	adj := l.buildRedirectGraph(redirectRules, localHosts)

	// DFS cycle detection with three-color marking
	chains := l.findCycles(redirectRules, adj)

	for _, chain := range chains {
		ids := make([]string, len(chain))
		for i, idx := range chain {
			ids[i] = redirectRules[idx].ID
		}
		chainStr := strings.Join(ids, " -> ")

		issues = append(issues, Issue{
			Severity:   SeverityError,
			RuleID:     ids[0],
			Message:    fmt.Sprintf("Circular redirect detected: %s", chainStr),
			Suggestion: "Remove one rule from the chain or change a destination to break the cycle",
		})
	}

	// Check prefix self-loops (PreservePath redirecting back into own match space)
	issues = append(issues, l.checkPrefixSelfLoops(redirectRules, localHosts)...)

	// Best-effort regex/glob sample tracing
	issues = append(issues, l.checkRegexGlobCycles(redirectRules, localHosts)...)

	return issues
}

// collectLocalHosts returns the set of hosts defined in rules' match.host fields.
// Destinations pointing to these hosts are considered "local" (could loop back).
// If no rules specify a host, all relative-path destinations are local.
func (l *Linter) collectLocalHosts() map[string]bool {
	hosts := make(map[string]bool)
	for _, rule := range l.cfg.Rules {
		if rule.Match.Host != "" {
			hosts[rule.Match.Host] = true
		}
	}
	return hosts
}

// extractDestinationPath parses a redirect destination URL and returns the
// local path if the destination points back to this service, or "" if external.
func (l *Linter) extractDestinationPath(dest string, localHosts map[string]bool) string {
	// Relative paths are always local
	if strings.HasPrefix(dest, "/") {
		return dest
	}

	parsed, err := url.Parse(dest)
	if err != nil {
		return ""
	}

	// If we have no local hosts defined, we can't determine locality from absolute URLs
	if len(localHosts) == 0 {
		return ""
	}

	// Check if the destination host is one of our local hosts
	if localHosts[parsed.Hostname()] {
		path := parsed.Path
		if path == "" {
			path = "/"
		}
		return path
	}

	return ""
}

// buildRedirectGraph creates an adjacency list mapping each rule index to
// the indices of rules that would match the redirect destination.
func (l *Linter) buildRedirectGraph(rules []config.Rule, localHosts map[string]bool) map[int][]int {
	adj := make(map[int][]int)

	for i, rule := range rules {
		dest := rule.Redirect.GetLocation()
		destPath := l.extractDestinationPath(dest, localHosts)
		if destPath == "" {
			continue
		}

		// Find which rules would match this destination path
		for j, target := range rules {
			if i == j {
				continue // Self-loops handled separately in checkPrefixSelfLoops
			}

			if l.pathMatchesRule(destPath, target) {
				adj[i] = append(adj[i], j)
			}
		}
	}

	return adj
}

// pathMatchesRule checks if a given path would be matched by a rule.
func (l *Linter) pathMatchesRule(path string, rule config.Rule) bool {
	switch rule.Match.Type {
	case config.MatchTypeExact:
		return path == rule.Match.Path
	case config.MatchTypePrefix:
		return strings.HasPrefix(path, rule.Match.Path)
	case config.MatchTypeRegex:
		if re := rule.CompiledRegex(); re != nil {
			return re.MatchString(path)
		}
		// Try compiling the pattern for lint-time checking
		re, err := regexp.Compile(rule.Match.Pattern)
		if err != nil {
			return false
		}
		return re.MatchString(path)
	case config.MatchTypeGlob:
		return l.globMatchesPath(rule.Match.Pattern, path)
	}
	return false
}

// globMatchesPath does a simple glob match for lint purposes.
func (l *Linter) globMatchesPath(pattern, path string) bool {
	// Handle common glob patterns
	if pattern == "/**" || pattern == "/*" {
		return true
	}
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return strings.HasPrefix(path, prefix)
	}
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return strings.HasPrefix(path, prefix+"/")
	}
	return false
}

// findCycles runs DFS on the redirect graph and returns all cycles found.
// Each cycle is a slice of rule indices forming the loop.
func (l *Linter) findCycles(rules []config.Rule, adj map[int][]int) [][]int {
	const (
		white = 0 // unvisited
		gray  = 1 // in progress (on current DFS stack)
		black = 2 // done
	)

	color := make([]int, len(rules))
	parent := make([]int, len(rules))
	for i := range parent {
		parent[i] = -1
	}

	var cycles [][]int
	seen := make(map[string]bool) // Deduplicate cycles

	var dfs func(u int, stack []int)
	dfs = func(u int, stack []int) {
		color[u] = gray
		stack = append(stack, u)

		for _, v := range adj[u] {
			switch color[v] {
			case gray:
				// Found a cycle — extract the cycle from the stack
				cycle := extractCycle(stack, v)
				if cycle != nil {
					key := cycleKey(cycle, rules)
					if !seen[key] {
						seen[key] = true
						cycles = append(cycles, cycle)
					}
				}
			case white:
				dfs(v, stack)
			}
		}

		color[u] = black
	}

	for i := range rules {
		if color[i] == white {
			dfs(i, nil)
		}
	}

	return cycles
}

// extractCycle extracts the cycle portion from a DFS stack.
// The cycle starts at the node 'start' and includes everything
// after it on the stack, plus 'start' again to close the loop.
func extractCycle(stack []int, start int) []int {
	for i, node := range stack {
		if node == start {
			cycle := make([]int, len(stack)-i+1)
			copy(cycle, stack[i:])
			cycle[len(cycle)-1] = start // Close the loop
			return cycle
		}
	}
	return nil
}

// cycleKey produces a canonical string key for deduplication.
// Rotates the cycle so the smallest rule ID comes first.
func cycleKey(cycle []int, rules []config.Rule) string {
	if len(cycle) <= 1 {
		return ""
	}
	// Exclude the closing element (duplicate of first)
	nodes := cycle[:len(cycle)-1]

	// Find the minimum ID position
	minIdx := 0
	for i := 1; i < len(nodes); i++ {
		if rules[nodes[i]].ID < rules[nodes[minIdx]].ID {
			minIdx = i
		}
	}

	// Rotate to start at minIdx
	rotated := make([]string, len(nodes))
	for i := range nodes {
		rotated[i] = rules[nodes[(i+minIdx)%len(nodes)]].ID
	}
	return strings.Join(rotated, "->")
}

// checkPrefixSelfLoops detects prefix rules with PreservePath that redirect
// back into their own match space, creating implicit self-loops.
func (l *Linter) checkPrefixSelfLoops(rules []config.Rule, localHosts map[string]bool) []Issue {
	var issues []Issue

	for _, rule := range rules {
		if rule.Match.Type != config.MatchTypePrefix || !rule.Redirect.PreservePath {
			continue
		}

		dest := rule.Redirect.GetLocation()
		destPath := l.extractDestinationPath(dest, localHosts)
		if destPath == "" {
			continue
		}

		// A prefix rule with PreservePath creates a self-loop when the
		// destination path starts with (or equals) the match prefix.
		// Example: match "/old/" with PreservePath, redirect to "/old/new/"
		// Request for /old/foo → /old/new/foo → /old/new/new/foo → ...
		if strings.HasPrefix(destPath, rule.Match.Path) {
			issues = append(issues, Issue{
				Severity: SeverityError,
				RuleID:   rule.ID,
				Message: fmt.Sprintf(
					"Prefix rule '%s' with preserve_path creates a self-loop: "+
						"match '%s' redirects to '%s' which is within the same prefix",
					rule.ID, rule.Match.Path, destPath),
				Suggestion: "Change the destination to a path outside the match prefix, or disable preserve_path",
			})
		}
	}

	return issues
}

// checkRegexGlobCycles uses sample URLs to detect potential cycles involving
// regex and glob rules. These are reported as warnings since they're best-effort.
func (l *Linter) checkRegexGlobCycles(rules []config.Rule, localHosts map[string]bool) []Issue {
	var issues []Issue

	for _, rule := range rules {
		if rule.Match.Type != config.MatchTypeRegex && rule.Match.Type != config.MatchTypeGlob {
			continue
		}

		// Generate sample paths for this rule
		samples := l.generateSamplePaths(rule)
		dest := rule.Redirect.GetLocation()

		for _, sample := range samples {
			// Simulate what the destination would be for this sample
			resolvedDest := dest
			if rule.Match.Type == config.MatchTypeRegex {
				re := rule.CompiledRegex()
				if re == nil {
					var err error
					re, err = regexp.Compile(rule.Match.Pattern)
					if err != nil {
						continue
					}
				}
				if re.MatchString(sample) {
					resolvedDest = re.ReplaceAllString(sample, dest)
				}
			}

			destPath := l.extractDestinationPath(resolvedDest, localHosts)
			if destPath == "" {
				continue
			}

			// Check if the resolved destination would match the same rule
			if l.pathMatchesRule(destPath, rule) {
				issues = append(issues, Issue{
					Severity: SeverityWarning,
					RuleID:   rule.ID,
					Message: fmt.Sprintf(
						"Potential circular redirect: rule '%s' destination '%s' "+
							"may match the same rule (sample path: '%s' -> '%s')",
						rule.ID, dest, sample, destPath),
					Suggestion: "Verify the destination does not fall within the rule's match pattern",
				})
				break // One warning per rule is enough
			}

			// Check if destination matches any other regex/glob rule that
			// could redirect back
			for _, other := range rules {
				if other.ID == rule.ID {
					continue
				}
				if l.pathMatchesRule(destPath, other) {
					otherDest := other.Redirect.GetLocation()
					otherDestPath := l.extractDestinationPath(otherDest, localHosts)
					if otherDestPath != "" && l.pathMatchesRule(otherDestPath, rule) {
						issues = append(issues, Issue{
							Severity: SeverityWarning,
							RuleID:   rule.ID,
							Message: fmt.Sprintf(
								"Potential circular redirect: '%s' -> '%s' -> '%s' "+
									"(sample: '%s' -> '%s' -> '%s')",
								rule.ID, other.ID, rule.ID,
								sample, destPath, otherDestPath),
							Suggestion: "Verify these rules don't create a redirect loop",
						})
						break
					}
				}
			}
		}
	}

	return issues
}

// generateSamplePaths creates representative sample paths for a regex or glob rule.
func (l *Linter) generateSamplePaths(rule config.Rule) []string {
	switch rule.Match.Type {
	case config.MatchTypeRegex:
		return generateRegexSamples(rule.Match.Pattern)
	case config.MatchTypeGlob:
		return generateGlobSamples(rule.Match.Pattern)
	}
	return nil
}

// generateRegexSamples produces sample paths from a regex pattern by
// replacing common capture groups with representative values.
func generateRegexSamples(pattern string) []string {
	// Strip anchors for replacement
	p := strings.TrimPrefix(pattern, "^")
	p = strings.TrimSuffix(p, "$")

	// Replace common capture group patterns with sample values
	replacements := []struct {
		re   *regexp.Regexp
		repl string
	}{
		{regexp.MustCompile(`\(\\d\+\)`), "123"},
		{regexp.MustCompile(`\(\[^/\]\+\)`), "sample"},
		{regexp.MustCompile(`\(\[^/\]\*\)`), "sample"},
		{regexp.MustCompile(`\(\.\+\)`), "test/path"},
		{regexp.MustCompile(`\(\.\*\)`), "test"},
		{regexp.MustCompile(`\(\.\+\?\)`), "t"},
		{regexp.MustCompile(`\(\.\*\?\)`), "t"},
		{regexp.MustCompile(`\\d\+`), "123"},
		{regexp.MustCompile(`\[^/\]\+`), "sample"},
		{regexp.MustCompile(`\[^/\]\*`), "sample"},
		{regexp.MustCompile(`\.\+`), "test/path"},
		{regexp.MustCompile(`\.\*`), "test"},
	}

	sample := p
	for _, r := range replacements {
		sample = r.re.ReplaceAllString(sample, r.repl)
	}

	// Ensure it starts with /
	if !strings.HasPrefix(sample, "/") {
		sample = "/" + sample
	}

	return []string{sample}
}

// generateGlobSamples produces sample paths from a glob pattern.
func generateGlobSamples(pattern string) []string {
	sample := pattern
	sample = strings.ReplaceAll(sample, "**", "sub/path")
	sample = strings.ReplaceAll(sample, "*", "sample")
	sample = strings.ReplaceAll(sample, "?", "x")

	if !strings.HasPrefix(sample, "/") {
		sample = "/" + sample
	}

	return []string{sample}
}

// SourceInput represents a config source for multi-source linting.
type SourceInput struct {
	Name     string         // e.g., "marketing", "engineering"
	Prefix   string         // e.g., "marketing", "eng"
	Priority int            // Higher priority wins in conflict resolution
	Config   *config.Config // The loaded config
}

// MultiSourceConflict represents a conflict between two sources.
type MultiSourceConflict struct {
	Path        string   `json:"path"`
	Sources     []string `json:"sources"`
	RuleIDs     []string `json:"rule_ids"`
	MatchType   string   `json:"match_type"` // "exact", "overlap", "potential"
	Description string   `json:"description"`
}

// MultiSourceResult contains the result of multi-source linting.
type MultiSourceResult struct {
	Sources        []string              `json:"sources"`
	TotalRules     int                   `json:"total_rules"`
	Conflicts      []MultiSourceConflict `json:"conflicts"`
	Issues         []Issue               `json:"issues"`
	RulesPerSource map[string]int        `json:"rules_per_source"`
}

// HasConflicts returns true if there are any conflicts.
func (r *MultiSourceResult) HasConflicts() bool {
	return len(r.Conflicts) > 0
}

// HasErrors returns true if there are any error-severity issues.
func (r *MultiSourceResult) HasErrors() bool {
	for _, issue := range r.Issues {
		if issue.Severity == SeverityError {
			return true
		}
	}
	return r.HasConflicts()
}

// MultiSourceLinter validates multiple config sources for conflicts.
type MultiSourceLinter struct {
	sources []SourceInput
}

// NewMultiSource creates a linter for multiple config sources.
func NewMultiSource(sources []SourceInput) *MultiSourceLinter {
	return &MultiSourceLinter{sources: sources}
}

// Lint checks all sources for conflicts and issues.
func (m *MultiSourceLinter) Lint() *MultiSourceResult {
	result := &MultiSourceResult{
		Sources:        make([]string, 0, len(m.sources)),
		RulesPerSource: make(map[string]int),
	}

	// Collect source names and rule counts
	for _, src := range m.sources {
		result.Sources = append(result.Sources, src.Name)
		result.RulesPerSource[src.Name] = len(src.Config.Rules)
		result.TotalRules += len(src.Config.Rules)
	}

	// Lint each source individually
	for _, src := range m.sources {
		linter := New(src.Config)
		srcResult := linter.Lint()

		// Prefix issues with source name
		for _, issue := range srcResult.Issues {
			issue.Message = fmt.Sprintf("[%s] %s", src.Name, issue.Message)
			if issue.RuleID != "" && src.Prefix != "" {
				issue.RuleID = src.Prefix + "/" + issue.RuleID
			}
			result.Issues = append(result.Issues, issue)
		}
	}

	// Check for cross-source conflicts
	result.Conflicts = m.checkCrossSourceConflicts()

	return result
}

// checkCrossSourceConflicts detects conflicts between different sources.
func (m *MultiSourceLinter) checkCrossSourceConflicts() []MultiSourceConflict {
	var conflicts []MultiSourceConflict

	// Build a map of path -> (source, rule) for conflict detection
	type pathEntry struct {
		source string
		ruleID string
		prefix string
		rule   config.Rule
	}

	pathMap := make(map[string][]pathEntry)

	for _, src := range m.sources {
		for _, rule := range src.Config.Rules {
			// Determine the path key
			pathKey := rule.Match.Path
			if pathKey == "" {
				pathKey = rule.Match.Pattern
			}
			if rule.Match.Host != "" {
				pathKey = rule.Match.Host + ":" + pathKey
			}

			prefixedID := rule.ID
			if src.Prefix != "" {
				prefixedID = src.Prefix + "/" + rule.ID
			}

			pathMap[pathKey] = append(pathMap[pathKey], pathEntry{
				source: src.Name,
				ruleID: prefixedID,
				prefix: src.Prefix,
				rule:   rule,
			})
		}
	}

	// Find exact path conflicts
	for path, entries := range pathMap {
		if len(entries) > 1 {
			// Check if entries are from different sources
			sourceSet := make(map[string]bool)
			for _, e := range entries {
				sourceSet[e.source] = true
			}

			if len(sourceSet) > 1 {
				var sources, ruleIDs []string
				for _, e := range entries {
					sources = append(sources, e.source)
					ruleIDs = append(ruleIDs, e.ruleID)
				}

				conflicts = append(conflicts, MultiSourceConflict{
					Path:        path,
					Sources:     sources,
					RuleIDs:     ruleIDs,
					MatchType:   "exact",
					Description: fmt.Sprintf("Multiple teams define rules for the same path '%s'", path),
				})
			}
		}
	}

	// Check for potential overlaps (prefix vs exact, glob patterns, etc.)
	for i, src1 := range m.sources {
		for j, src2 := range m.sources {
			if i >= j {
				continue
			}

			for _, rule1 := range src1.Config.Rules {
				for _, rule2 := range src2.Config.Rules {
					if overlap := detectCrossSourceOverlap(rule1, rule2); overlap != "" {
						id1 := rule1.ID
						id2 := rule2.ID
						if src1.Prefix != "" {
							id1 = src1.Prefix + "/" + id1
						}
						if src2.Prefix != "" {
							id2 = src2.Prefix + "/" + id2
						}

						// Skip if this is already detected as an exact conflict
						alreadyDetected := false
						for _, c := range conflicts {
							if c.MatchType == "exact" && containsAll(c.RuleIDs, []string{id1, id2}) {
								alreadyDetected = true
								break
							}
						}

						if !alreadyDetected {
							conflicts = append(conflicts, MultiSourceConflict{
								Path:        fmt.Sprintf("%s vs %s", rule1.Match.Path+rule1.Match.Pattern, rule2.Match.Path+rule2.Match.Pattern),
								Sources:     []string{src1.Name, src2.Name},
								RuleIDs:     []string{id1, id2},
								MatchType:   "overlap",
								Description: overlap,
							})
						}
					}
				}
			}
		}
	}

	return conflicts
}

// detectCrossSourceOverlap checks if two rules from different sources may conflict.
func detectCrossSourceOverlap(r1, r2 config.Rule) string {
	// Get paths
	path1 := r1.Match.Path
	if path1 == "" {
		path1 = r1.Match.Pattern
	}
	path2 := r2.Match.Path
	if path2 == "" {
		path2 = r2.Match.Pattern
	}

	// Exact matches don't overlap unless identical (already caught above)
	if r1.Match.Type == config.MatchTypeExact && r2.Match.Type == config.MatchTypeExact {
		return ""
	}

	// Prefix overlaps
	if r1.Match.Type == config.MatchTypePrefix && r2.Match.Type == config.MatchTypePrefix {
		if strings.HasPrefix(r1.Match.Path, r2.Match.Path) || strings.HasPrefix(r2.Match.Path, r1.Match.Path) {
			return fmt.Sprintf("Prefix rules overlap: '%s' and '%s' may match the same paths", r1.Match.Path, r2.Match.Path)
		}
	}

	// Prefix vs exact
	if r1.Match.Type == config.MatchTypePrefix && r2.Match.Type == config.MatchTypeExact {
		if strings.HasPrefix(r2.Match.Path, r1.Match.Path) {
			return fmt.Sprintf("Prefix '%s' would match exact path '%s'", r1.Match.Path, r2.Match.Path)
		}
	}
	if r2.Match.Type == config.MatchTypePrefix && r1.Match.Type == config.MatchTypeExact {
		if strings.HasPrefix(r1.Match.Path, r2.Match.Path) {
			return fmt.Sprintf("Prefix '%s' would match exact path '%s'", r2.Match.Path, r1.Match.Path)
		}
	}

	// Glob catch-all can overlap with anything
	if r1.Match.Type == config.MatchTypeGlob && strings.Contains(r1.Match.Pattern, "**") {
		pattern := strings.TrimSuffix(r1.Match.Pattern, "/**")
		if strings.HasPrefix(path2, pattern) {
			return fmt.Sprintf("Glob '%s' may catch paths meant for '%s'", r1.Match.Pattern, path2)
		}
	}
	if r2.Match.Type == config.MatchTypeGlob && strings.Contains(r2.Match.Pattern, "**") {
		pattern := strings.TrimSuffix(r2.Match.Pattern, "/**")
		if strings.HasPrefix(path1, pattern) {
			return fmt.Sprintf("Glob '%s' may catch paths meant for '%s'", r2.Match.Pattern, path1)
		}
	}

	return ""
}

// containsAll checks if slice a contains all elements of slice b.
func containsAll(a, b []string) bool {
	m := make(map[string]bool)
	for _, v := range a {
		m[v] = true
	}
	for _, v := range b {
		if !m[v] {
			return false
		}
	}
	return true
}
