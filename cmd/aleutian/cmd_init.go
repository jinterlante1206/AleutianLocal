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
	"path/filepath"
	"time"

	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/internal/initializer"
	"github.com/spf13/cobra"
)

// =============================================================================
// COMMAND FLAGS
// =============================================================================

var (
	initForce      bool     // Rebuild index even if exists
	initLanguages  []string // Limit to specific languages
	initExcludes   []string // Glob patterns to exclude
	initJSONOutput bool     // Output as JSON
	initQuiet      bool     // Suppress progress output
	initVerbose    bool     // Show detailed output
	initDryRun     bool     // Show what would be indexed without writing
	initMaxWorkers int      // Maximum parallel workers
)

// =============================================================================
// COMMAND DEFINITION
// =============================================================================

// initCmd is the main init command.
//
// # Description
//
// Initializes Aleutian Trace index for a codebase by parsing source files,
// building a symbol index and call graph, and storing results in .aleutian/.
//
// # Examples
//
//	aleutian init                        # Initialize current directory
//	aleutian init ./myproject            # Initialize specific path
//	aleutian init --languages go,python  # Limit to specific languages
//	aleutian init --json                 # JSON output for scripting
//	aleutian init --dry-run              # Show what would be indexed
//
// # Exit Codes
//
//	0 - Success
//	1 - Initialization failed
//	2 - Invalid arguments
//
// # Limitations
//
//   - Only one init can run per project at a time (file lock)
//   - Requires write permission on project root
//
// # Assumptions
//
//   - Supported languages: go, python, typescript, javascript, java, rust
var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Initialize Aleutian Trace index for a codebase",
	Long: `Initialize Aleutian Trace index for a codebase.

This command parses source files, builds a symbol index and call graph,
and stores results in a .aleutian/ directory for fast code analysis.

The initialization process:
  1. Detects programming languages in the project
  2. Scans for source files (excluding vendor, node_modules, etc.)
  3. Parses files in parallel to extract symbols and relationships
  4. Builds call graph with caller/callee relationships
  5. Stores index in .aleutian/ directory

Supported languages: go, python, typescript, javascript, java, rust

Examples:
  aleutian init                        # Initialize current directory
  aleutian init ./myproject            # Initialize specific path
  aleutian init --languages go,python  # Limit to specific languages
  aleutian init --exclude "test/**"    # Exclude test files
  aleutian init --json                 # JSON output for scripting
  aleutian init --dry-run              # Show what would be indexed`,
	Args: cobra.MaximumNArgs(1),
	Run:  runInitCommand,
}

// =============================================================================
// COMMAND INITIALIZATION
// =============================================================================

func init() {
	initCmd.Flags().BoolVar(&initForce, "force", false,
		"Rebuild index even if .aleutian/ exists")
	initCmd.Flags().StringSliceVar(&initLanguages, "languages", nil,
		"Limit to specific languages (e.g., go,python)")
	initCmd.Flags().StringSliceVar(&initExcludes, "exclude", nil,
		"Glob patterns to exclude (e.g., vendor/**,*_test.go)")
	initCmd.Flags().BoolVar(&initJSONOutput, "json", false,
		"Output as JSON for scripting")
	initCmd.Flags().BoolVar(&initQuiet, "quiet", false,
		"Suppress progress output")
	initCmd.Flags().BoolVarP(&initVerbose, "verbose", "v", false,
		"Show detailed per-file progress")
	initCmd.Flags().BoolVar(&initDryRun, "dry-run", false,
		"Show what would be indexed without writing")
	initCmd.Flags().IntVar(&initMaxWorkers, "max-workers", initializer.OptimalWorkerCount(),
		"Maximum parallel workers for parsing")
}

// =============================================================================
// COMMAND IMPLEMENTATION
// =============================================================================

// runInitCommand executes the init command.
//
// # Description
//
// Initializes the code index for a project. Handles argument parsing,
// creates the initializer, runs initialization, and outputs results.
//
// # Inputs
//
//   - cmd: Cobra command (unused except for context)
//   - args: Optional path to initialize (default: current directory)
//
// # Outputs
//
// Prints results to stdout. Exits with appropriate code.
//
// # Exit Codes
//
//	0 - Success (including partial success with warnings)
//	1 - Initialization failed
//	2 - Invalid arguments
func runInitCommand(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Determine project path
	projectRoot := "."
	if len(args) > 0 {
		projectRoot = args[0]
	}

	// Convert to absolute path
	absPath, err := filepath.Abs(projectRoot)
	if err != nil {
		outputError("Invalid path", err)
		os.Exit(initializer.ExitBadArgs)
	}

	// Validate path exists
	info, err := os.Stat(absPath)
	if os.IsNotExist(err) {
		outputError("Path does not exist", fmt.Errorf("%s", absPath))
		os.Exit(initializer.ExitBadArgs)
	}
	if err != nil {
		outputError("Cannot access path", err)
		os.Exit(initializer.ExitBadArgs)
	}
	if !info.IsDir() {
		outputError("Path is not a directory", fmt.Errorf("%s", absPath))
		os.Exit(initializer.ExitBadArgs)
	}

	// Check if index already exists
	storage := initializer.NewStorage(absPath)
	if storage.Exists() && !initForce && !initDryRun {
		outputError("Index already exists", fmt.Errorf("use --force to rebuild"))
		os.Exit(initializer.ExitBadArgs)
	}

	// Build configuration
	cfg := initializer.DefaultConfig(absPath)
	cfg.Languages = initLanguages
	if len(initExcludes) > 0 {
		cfg.ExcludePatterns = append(cfg.ExcludePatterns, initExcludes...)
	}
	cfg.Force = initForce
	cfg.DryRun = initDryRun
	cfg.Quiet = initQuiet
	cfg.Verbose = initVerbose
	cfg.MaxWorkers = initMaxWorkers

	// Create progress callback
	var progressCb initializer.ProgressCallback
	if !initQuiet && !initJSONOutput {
		progressCb = func(p initializer.Progress) {
			switch p.Phase {
			case "detecting":
				fmt.Println("Detecting languages...")
			case "scanning":
				fmt.Println("Scanning for source files...")
			case "parsing":
				if initVerbose {
					fmt.Printf("\rParsing: %d/%d files (%d%%) - %s",
						p.FilesScanned, p.FilesTotal, p.Percent, filepath.Base(p.FilesCurrent))
				} else if p.FilesScanned%100 == 0 {
					fmt.Printf("\rParsing: %d/%d files (%d%%)",
						p.FilesScanned, p.FilesTotal, p.Percent)
				}
			case "writing":
				fmt.Println("\nWriting index...")
			case "complete":
				fmt.Println("Initialization complete.")
			}
		}
	}

	// Create initializer and run
	init := initializer.NewInitializer(storage)
	result, err := init.Init(ctx, cfg, progressCb)

	// Handle errors
	if err != nil {
		if initJSONOutput {
			outputErrorJSON(err)
		} else {
			outputError("Initialization failed", err)
		}
		os.Exit(initializer.ExitFailure)
	}

	// Output results
	if initJSONOutput {
		outputInitResultJSON(result)
	} else {
		outputInitResultText(result)
	}

	// Exit success (even with warnings)
	os.Exit(initializer.ExitSuccess)
}

// outputInitResultJSON outputs the result as JSON.
func outputInitResultJSON(result *initializer.Result) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to encode JSON: %v\n", err)
		os.Exit(initializer.ExitFailure)
	}
}

// outputInitResultText outputs the result as human-readable text.
func outputInitResultText(result *initializer.Result) {
	fmt.Println()
	fmt.Printf("╔══════════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║                    ALEUTIAN TRACE INITIALIZED                     ║\n")
	fmt.Printf("╠══════════════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║  Project:    %-50s  ║\n", truncateString(result.ProjectRoot, 50))
	fmt.Printf("║  Languages:  %-50s  ║\n", formatLanguages(result.Languages))
	fmt.Printf("╠══════════════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║  Files indexed:   %10d                                      ║\n", result.FilesIndexed)
	fmt.Printf("║  Symbols found:   %10d                                      ║\n", result.SymbolsFound)
	fmt.Printf("║  Call edges:      %10d                                      ║\n", result.EdgesBuilt)
	fmt.Printf("║  Duration:        %10.2fs                                     ║\n", float64(result.DurationMs)/1000)
	fmt.Printf("╠══════════════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║  Index path:  %-52s║\n", result.IndexPath)
	fmt.Printf("╚══════════════════════════════════════════════════════════════════╝\n")

	// Show warnings if any
	if len(result.Warnings) > 0 {
		fmt.Println()
		fmt.Println("Warnings:")
		for _, w := range result.Warnings {
			if len(w) > 70 {
				fmt.Printf("  ⚠ %s...\n", w[:67])
			} else {
				fmt.Printf("  ⚠ %s\n", w)
			}
		}
	}
}

// outputError outputs an error message.
func outputError(msg string, err error) {
	fmt.Fprintf(os.Stderr, "Error: %s: %v\n", msg, err)
}

// outputErrorJSON outputs an error as JSON.
func outputErrorJSON(err error) {
	result := map[string]interface{}{
		"api_version": initializer.APIVersion,
		"success":     false,
		"error":       err.Error(),
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.Encode(result)
}

// truncateString truncates a string to max length with ellipsis.
func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// formatLanguages formats a slice of languages for display.
func formatLanguages(languages []string) string {
	if len(languages) == 0 {
		return "none"
	}
	result := ""
	for i, lang := range languages {
		if i > 0 {
			result += ", "
		}
		result += lang
	}
	if len(result) > 50 {
		return result[:47] + "..."
	}
	return result
}
