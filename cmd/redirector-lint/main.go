package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/jamengual/the-redirector/internal/config"
	"github.com/jamengual/the-redirector/internal/lint"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorBold   = "\033[1m"
)

func main() {
	// Flags
	jsonOutput := flag.Bool("json", false, "Output in JSON format")
	quiet := flag.Bool("quiet", false, "Only output errors")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: redirector-lint [options] <config-path>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	configPath := flag.Arg(0)

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
	if *jsonOutput {
		outputJSON(result)
	} else {
		outputText(result, *quiet)
	}

	// Exit with error code if there are errors
	if result.HasErrors() {
		os.Exit(1)
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
