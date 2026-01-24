package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jamengual/the-redirector/internal/config"
	"github.com/jamengual/the-redirector/internal/lint"
)

// ANSI color codes
const (
	colorReset   = "\033[0m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorBold    = "\033[1m"
)

func main() {
	// Flags
	jsonOutput := flag.Bool("json", false, "Output in JSON format")
	quiet := flag.Bool("quiet", false, "Only output errors")
	multiSource := flag.Bool("multi-source", false, "Multi-source mode: detect conflicts between team configs")
	flag.Parse()

	if flag.NArg() < 1 {
		printUsage()
		os.Exit(1)
	}

	if *multiSource {
		runMultiSourceLint(flag.Args(), *jsonOutput, *quiet)
	} else {
		runSingleLint(flag.Arg(0), *jsonOutput, *quiet)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: redirector-lint [options] <config-path>")
	fmt.Fprintln(os.Stderr, "       redirector-lint --multi-source <source1> <source2> ...")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Multi-source format: name:prefix:priority:path")
	fmt.Fprintln(os.Stderr, "  Example: marketing:marketing:10:/path/to/marketing/rules.yaml")
	fmt.Fprintln(os.Stderr, "  Example: engineering:eng:20:/path/to/eng/rules.yaml")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Options:")
	flag.PrintDefaults()
}

func runSingleLint(configPath string, jsonOut, quiet bool) {
	// Load configuration
	cfg, err := config.LoadDirectory(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s Failed to load config: %v\n", colorRed, colorReset, err)
		os.Exit(1)
	}

	// Run linter
	linter := lint.New(cfg)
	result := linter.Lint()

	// Output results
	if jsonOut {
		outputJSON(result)
	} else {
		outputText(result, quiet)
	}

	// Exit with error code if there are errors
	if result.HasErrors() {
		os.Exit(1)
	}
}

func runMultiSourceLint(args []string, jsonOut, quiet bool) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "Error: Multi-source mode requires at least 2 sources")
		printUsage()
		os.Exit(1)
	}

	// Parse source arguments
	sources := make([]lint.SourceInput, 0, len(args))
	for _, arg := range args {
		src, err := parseSourceArg(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s %v\n", colorRed, colorReset, err)
			os.Exit(1)
		}
		sources = append(sources, src)
	}

	// Run multi-source linter
	linter := lint.NewMultiSource(sources)
	result := linter.Lint()

	// Output results
	if jsonOut {
		outputMultiSourceJSON(result)
	} else {
		outputMultiSourceText(result, quiet)
	}

	// Exit with error code if there are conflicts or errors
	if result.HasErrors() {
		os.Exit(1)
	}
}

func parseSourceArg(arg string) (lint.SourceInput, error) {
	// Format: name:prefix:priority:path
	parts := strings.SplitN(arg, ":", 4)
	if len(parts) != 4 {
		return lint.SourceInput{}, fmt.Errorf("invalid source format '%s', expected name:prefix:priority:path", arg)
	}

	name := parts[0]
	prefix := parts[1]
	priority, err := strconv.Atoi(parts[2])
	if err != nil {
		return lint.SourceInput{}, fmt.Errorf("invalid priority '%s': %w", parts[2], err)
	}
	path := parts[3]

	// Load the config
	cfg, err := config.LoadDirectory(path)
	if err != nil {
		return lint.SourceInput{}, fmt.Errorf("failed to load config from '%s': %w", path, err)
	}

	return lint.SourceInput{
		Name:     name,
		Prefix:   prefix,
		Priority: priority,
		Config:   cfg,
	}, nil
}

func outputMultiSourceJSON(result *lint.MultiSourceResult) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.Encode(result)
}

func outputMultiSourceText(result *lint.MultiSourceResult, quiet bool) {
	// Header
	fmt.Printf("%s%sThe Redirector - Multi-Team Config Linter%s\n", colorBold, colorBlue, colorReset)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	// Sources summary
	fmt.Printf("Sources: %d | Total Rules: %d\n", len(result.Sources), result.TotalRules)
	for _, src := range result.Sources {
		fmt.Printf("  • %s%s%s: %d rules\n", colorCyan, src, colorReset, result.RulesPerSource[src])
	}
	fmt.Println()

	// Conflicts (most important for multi-team)
	if len(result.Conflicts) > 0 {
		fmt.Printf("%s%s⚠ TEAM CONFLICTS (%d)%s\n", colorBold, colorRed, len(result.Conflicts), colorReset)
		fmt.Println("─────────────────────────────────────────────────────────────────")
		fmt.Println("These rules from different teams may conflict at runtime:")
		fmt.Println()

		for i, conflict := range result.Conflicts {
			// Conflict header
			fmt.Printf("  %s%d. %s%s\n", colorYellow, i+1, conflict.Description, colorReset)

			// Details
			fmt.Printf("     Path: %s%s%s\n", colorMagenta, conflict.Path, colorReset)
			fmt.Printf("     Teams: %s\n", strings.Join(conflict.Sources, " vs "))
			fmt.Printf("     Rules: %s\n", strings.Join(conflict.RuleIDs, ", "))
			fmt.Printf("     Type: %s\n", conflict.MatchType)
			fmt.Println()
		}
	}

	// Per-source issues
	errors := make([]lint.Issue, 0)
	warnings := make([]lint.Issue, 0)
	for _, issue := range result.Issues {
		switch issue.Severity {
		case lint.SeverityError:
			errors = append(errors, issue)
		case lint.SeverityWarning:
			warnings = append(warnings, issue)
		}
	}

	if len(errors) > 0 {
		fmt.Printf("%s%s✗ ERRORS (%d)%s\n", colorBold, colorRed, len(errors), colorReset)
		fmt.Println("─────────────────────────────────────────────────────────────────")
		for _, issue := range errors {
			printIssue(issue)
		}
		fmt.Println()
	}

	if len(warnings) > 0 && !quiet {
		fmt.Printf("%s%s⚠ WARNINGS (%d)%s\n", colorBold, colorYellow, len(warnings), colorReset)
		fmt.Println("─────────────────────────────────────────────────────────────────")
		for _, issue := range warnings {
			printIssue(issue)
		}
		fmt.Println()
	}

	// Summary
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	if len(result.Conflicts) == 0 && len(errors) == 0 && len(warnings) == 0 {
		fmt.Printf("%s%s✓ No conflicts or issues found between teams!%s\n", colorBold, colorGreen, colorReset)
	} else {
		fmt.Printf("Found: %s%d conflicts%s, %s%d errors%s, %s%d warnings%s\n",
			colorRed, len(result.Conflicts), colorReset,
			colorRed, len(errors), colorReset,
			colorYellow, len(warnings), colorReset)

		if len(result.Conflicts) > 0 {
			fmt.Printf("\n%sRecommendation:%s Teams should coordinate on conflicting paths or use\n", colorBold, colorReset)
			fmt.Printf("different path prefixes to avoid runtime conflicts.\n")
		}
	}
}

func outputJSON(result *lint.Result) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.Encode(result)
}

func outputText(result *lint.Result, quiet bool) {
	// Header
	fmt.Printf("%s%sThe Redirector - Config Linter%s\n", colorBold, colorBlue, colorReset)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("Loaded %d rules\n\n", result.RulesCount)

	// Group issues by severity
	errors := result.Errors()
	warnings := result.Warnings()
	var infos []lint.Issue
	for _, issue := range result.Issues {
		if issue.Severity == lint.SeverityInfo {
			infos = append(infos, issue)
		}
	}

	// Print errors
	if len(errors) > 0 {
		fmt.Printf("%s%s✗ ERRORS (%d)%s\n", colorBold, colorRed, len(errors), colorReset)
		fmt.Println("─────────────────────────────────────────────────────────────────")
		for _, issue := range errors {
			printIssue(issue)
		}
		fmt.Println()
	}

	// Print warnings
	if len(warnings) > 0 && !quiet {
		fmt.Printf("%s%s⚠ WARNINGS (%d)%s\n", colorBold, colorYellow, len(warnings), colorReset)
		fmt.Println("─────────────────────────────────────────────────────────────────")
		for _, issue := range warnings {
			printIssue(issue)
		}
		fmt.Println()
	}

	// Print info
	if len(infos) > 0 && !quiet {
		fmt.Printf("%s%sℹ SUGGESTIONS (%d)%s\n", colorBold, colorBlue, len(infos), colorReset)
		fmt.Println("─────────────────────────────────────────────────────────────────")
		for _, issue := range infos {
			printIssue(issue)
		}
		fmt.Println()
	}

	// Summary
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	if len(errors) == 0 && len(warnings) == 0 {
		fmt.Printf("%s%s✓ No issues found!%s\n", colorBold, colorGreen, colorReset)
	} else {
		fmt.Printf("Found: %s%d errors%s, %s%d warnings%s, %d suggestions\n",
			colorRed, len(errors), colorReset,
			colorYellow, len(warnings), colorReset,
			len(infos))
	}
}

func printIssue(issue lint.Issue) {
	// Color based on severity
	var color string
	switch issue.Severity {
	case lint.SeverityError:
		color = colorRed
	case lint.SeverityWarning:
		color = colorYellow
	case lint.SeverityInfo:
		color = colorBlue
	}

	// Rule ID if present
	ruleInfo := ""
	if issue.RuleID != "" {
		ruleInfo = fmt.Sprintf("[%s] ", issue.RuleID)
	}

	fmt.Printf("  %s%s%s%s\n", color, ruleInfo, issue.Message, colorReset)

	if issue.Suggestion != "" {
		fmt.Printf("    %s→ %s%s\n", colorGreen, issue.Suggestion, colorReset)
	}
}
