// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/internal/impact"
	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/internal/initializer"
	"github.com/spf13/cobra"
)

// =============================================================================
// COMMAND FLAGS
// =============================================================================

var (
	// Change detection flags
	impactDiff   bool
	impactStaged bool
	impactCommit string
	impactBranch string
	impactFiles  []string

	// Analysis flags
	impactThreshold    string
	impactMaxDepth     int
	impactMaxFiles     int
	impactIncludeTests bool
	impactExclude      []string

	// Output flags
	impactJSON   bool
	impactFormat string
	impactQuiet  bool
)

// =============================================================================
// COMMAND DEFINITION
// =============================================================================

var impactCmd = &cobra.Command{
	Use:   "impact [files...]",
	Short: "Analyze the impact of code changes",
	Long: `Analyze the impact of code changes to understand blast radius and risk.

This command detects changed files, maps them to code symbols, and computes
the transitive impact through the call graph. It reports affected symbols,
packages, tests, and provides a risk assessment.

Change Detection Modes:
  --diff       Analyze uncommitted changes (default)
  --staged     Analyze staged changes only
  --commit     Analyze a specific commit
  --branch     Analyze changes since branch point
  [files...]   Analyze specific files

Examples:
  aleutian impact --diff
  aleutian impact --staged
  aleutian impact --branch main
  aleutian impact --commit abc123
  aleutian impact src/auth/validator.go src/auth/types.go

CI/CD Integration:
  aleutian impact --branch main --threshold medium --json
  (exits 1 if risk exceeds threshold)`,
	Args: cobra.ArbitraryArgs,
	Run:  runImpact,
}

func init() {
	// Change detection flags
	impactCmd.Flags().BoolVar(&impactDiff, "diff", false,
		"Analyze uncommitted changes (git diff)")
	impactCmd.Flags().BoolVar(&impactStaged, "staged", false,
		"Analyze staged changes (git diff --cached)")
	impactCmd.Flags().StringVar(&impactCommit, "commit", "",
		"Analyze a specific commit")
	impactCmd.Flags().StringVar(&impactBranch, "branch", "",
		"Analyze changes since branch point (e.g., main)")

	// Analysis flags
	impactCmd.Flags().StringVar(&impactThreshold, "threshold", "high",
		"Risk threshold for exit code: low, medium, high, critical")
	impactCmd.Flags().IntVar(&impactMaxDepth, "max-depth", 0,
		"Maximum transitive depth (0 = default 10)")
	impactCmd.Flags().IntVar(&impactMaxFiles, "max-files", 0,
		"Maximum files to analyze (0 = default 500)")
	impactCmd.Flags().BoolVar(&impactIncludeTests, "include-tests", false,
		"Include test files in analysis")
	impactCmd.Flags().StringSliceVar(&impactExclude, "exclude", nil,
		"Patterns to exclude from analysis")

	// Output flags
	impactCmd.Flags().BoolVar(&impactJSON, "json", false,
		"Output as JSON for scripting")
	impactCmd.Flags().StringVar(&impactFormat, "format", "summary",
		"Output format: summary, full")
	impactCmd.Flags().BoolVar(&impactQuiet, "quiet", false,
		"Only exit code, no output")
}

// =============================================================================
// COMMAND IMPLEMENTATION
// =============================================================================

func runImpact(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Build config
	cfg := impact.DefaultConfig()

	// Determine change mode
	modeCount := 0
	if impactDiff {
		cfg.Mode = impact.ChangeModeDiff
		modeCount++
	}
	if impactStaged {
		cfg.Mode = impact.ChangeModeStaged
		modeCount++
	}
	if impactCommit != "" {
		cfg.Mode = impact.ChangeModeCommit
		cfg.CommitHash = impactCommit
		modeCount++
	}
	if impactBranch != "" {
		cfg.Mode = impact.ChangeModeBranch
		cfg.BaseBranch = impactBranch
		modeCount++
	}
	if len(args) > 0 {
		cfg.Mode = impact.ChangeModeFiles
		cfg.Files = args
		modeCount++
	}

	// Validate only one mode specified (or default to diff)
	if modeCount > 1 {
		outputImpactError("Multiple change modes specified; use only one of --diff, --staged, --commit, --branch, or [files...]", nil)
		os.Exit(impact.ExitError)
	}
	if modeCount == 0 {
		cfg.Mode = impact.ChangeModeDiff // Default
	}

	// Apply other config
	cfg.Threshold = impact.ParseRiskLevel(impactThreshold)
	if impactMaxDepth > 0 {
		cfg.MaxDepth = impactMaxDepth
	}
	if impactMaxFiles > 0 {
		cfg.MaxFiles = impactMaxFiles
	}
	cfg.IncludeTests = impactIncludeTests
	cfg.ExcludePatterns = impactExclude
	cfg.Quiet = impactQuiet

	// Load index
	cwd, err := os.Getwd()
	if err != nil {
		outputImpactError("Failed to get working directory", err)
		os.Exit(impact.ExitError)
	}

	storage := initializer.NewStorage(cwd)
	if !storage.Exists() {
		outputImpactError("Index not found", fmt.Errorf("run 'aleutian init' first"))
		os.Exit(impact.ExitError)
	}

	index, err := storage.LoadIndex(ctx)
	if err != nil {
		outputImpactError("Failed to load index", err)
		os.Exit(impact.ExitError)
	}

	// Run analysis
	analyzer := impact.NewAnalyzer(index, cwd)
	result, err := analyzer.Analyze(ctx, cfg)
	if err != nil {
		outputImpactError("Analysis failed", err)
		os.Exit(impact.ExitError)
	}

	// Output
	if !cfg.Quiet {
		if impactJSON {
			outputImpactJSON(result)
		} else {
			outputImpactText(result, impactFormat)
		}
	}

	// Exit code based on threshold
	if result.RiskLevel.Exceeds(cfg.Threshold) {
		os.Exit(impact.ExitRiskFound)
	}
	os.Exit(impact.ExitSuccess)
}

// =============================================================================
// OUTPUT FUNCTIONS
// =============================================================================

func outputImpactError(msg string, err error) {
	if impactJSON {
		result := map[string]interface{}{
			"api_version": impact.APIVersion,
			"success":     false,
			"error":       msg,
		}
		if err != nil {
			result["error"] = fmt.Sprintf("%s: %v", msg, err)
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		encoder.Encode(result)
	} else {
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s: %v\n", msg, err)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
		}
	}
}

func outputImpactJSON(result *impact.Result) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to encode JSON: %v\n", err)
		os.Exit(impact.ExitError)
	}
}

func outputImpactText(result *impact.Result, format string) {
	// Header
	fmt.Println("Impact Analysis")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println()

	// Changed files summary
	fmt.Printf("Changed Files: %d\n", len(result.ChangedFiles))
	if format == "full" && len(result.ChangedFiles) > 0 {
		for _, f := range result.ChangedFiles {
			fmt.Printf("  %s  %s\n", f.ChangeType, f.Path)
		}
		fmt.Println()
	}

	// Changed symbols
	fmt.Printf("Changed Symbols: %d\n", len(result.ChangedSymbols))
	if format == "full" && len(result.ChangedSymbols) > 0 {
		for _, s := range result.ChangedSymbols {
			fmt.Printf("  %s  %s:%d  %s()\n",
				s.ChangeType, s.FilePath, s.Symbol.StartLine, s.Symbol.Name)
		}
		fmt.Println()
	}

	// Blast radius
	fmt.Println()
	fmt.Println("Blast Radius:")
	fmt.Printf("  Direct callers:     %d\n", result.DirectCount)
	fmt.Printf("  Transitive impact:  %d\n", result.TransitiveCount)
	fmt.Printf("  Total affected:     %d symbols\n", result.TotalAffected)
	fmt.Printf("  Affected packages:  %d\n", len(result.AffectedPackages))
	fmt.Printf("  Affected tests:     %d files\n", len(result.AffectedTests))

	if format == "full" && len(result.AffectedPackages) > 0 {
		fmt.Println()
		fmt.Println("  Packages:")
		for _, pkg := range result.AffectedPackages {
			fmt.Printf("    %s\n", pkg)
		}
	}

	// Risk assessment
	fmt.Println()
	fmt.Println("Risk Assessment:")
	fmt.Printf("  Level: %s\n", result.RiskLevel)
	if len(result.RiskFactors.Reasons) > 0 {
		for _, reason := range result.RiskFactors.Reasons {
			fmt.Printf("  - %s\n", reason)
		}
	}

	// Affected tests
	if len(result.AffectedTests) > 0 {
		fmt.Println()
		fmt.Println("Affected Tests:")
		limit := 10
		if format == "full" {
			limit = len(result.AffectedTests)
		}
		for i, t := range result.AffectedTests {
			if i >= limit {
				fmt.Printf("  ... and %d more\n", len(result.AffectedTests)-limit)
				break
			}
			fmt.Printf("  %s\n", t)
		}
	}

	// Warnings
	if len(result.Warnings) > 0 {
		fmt.Println()
		fmt.Println("Warnings:")
		for _, w := range result.Warnings {
			fmt.Printf("  %s\n", w)
		}
	}

	// Footer
	fmt.Println()
	fmt.Printf("Analysis completed in %dms\n", result.DurationMs)

	if result.Truncated {
		fmt.Println("(Results truncated)")
	}
}
