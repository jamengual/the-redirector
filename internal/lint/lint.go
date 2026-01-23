// Package lint provides configuration validation and analysis.
package lint

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/your-org/the-redirector/internal/config"
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
						Severity: SeverityWarning,
						RuleID:   rule1.ID,
						Message:  fmt.Sprintf("Rule '%s' may overlap with '%s': %s", rule1.ID, rule2.ID, overlap),
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
						Severity: SeverityWarning,
						RuleID:   rule.ID,
						Message:  fmt.Sprintf("Greedy glob pattern '%s' will match many paths", rule.Match.Pattern),
						Suggestion: "Set a negative priority (e.g., -100) to ensure it's evaluated last",
					})
				}
			}

		case config.MatchTypeRegex:
			if strings.HasPrefix(rule.Match.Pattern, ".*") || rule.Match.Pattern == ".*" {
				issues = append(issues, Issue{
					Severity: SeverityWarning,
					RuleID:   rule.ID,
					Message:  fmt.Sprintf("Pattern starting with '.*' is greedy and slow"),
					Suggestion: "Anchor with ^ or use a more specific prefix",
				})
			}

		case config.MatchTypePrefix:
			if rule.Match.Path == "/" && rule.Priority >= 0 {
				issues = append(issues, Issue{
					Severity: SeverityWarning,
					RuleID:   rule.ID,
					Message:  "Prefix '/' matches all paths",
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
					Severity: SeverityWarning,
					RuleID:   later.ID,
					Message:  fmt.Sprintf("Rule '%s' may be unreachable - shadowed by '%s'", later.ID, rule.ID),
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
