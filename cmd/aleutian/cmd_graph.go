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
	"strings"
	"time"

	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/internal/graph"
	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/internal/initializer"
	"github.com/spf13/cobra"
)

// =============================================================================
// COMMAND FLAGS
// =============================================================================

var (
	// Shared flags
	graphJSONOutput   bool
	graphFormat       string
	graphDepth        int
	graphLimit        int
	graphExact        bool
	graphFailIfEmpty  bool
	graphIncludeTests bool

	// Callees-specific
	graphIncludeStdlib bool

	// Path-specific
	graphAllPaths bool
	graphMaxPaths int
)

// =============================================================================
// COMMAND DEFINITIONS
// =============================================================================

// graphCmd is the parent graph command.
var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Query the code dependency graph",
	Long: `Commands for querying callers, callees, and paths in the call graph.

Prerequisites:
  Run 'aleutian init' first to build the code index.

Subcommands:
  callers  - Find all functions that call a symbol
  callees  - Find all functions called by a symbol
  path     - Find call path between two symbols

Examples:
  aleutian graph callers "auth.ValidateToken"
  aleutian graph callees "main.HandleRequest" --depth 2
  aleutian graph path "main.main" "db.Close"`,
}

// graphCallersCmd finds callers of a symbol.
var graphCallersCmd = &cobra.Command{
	Use:   "callers SYMBOL",
	Short: "Find all functions that call a symbol",
	Long: `Find all functions that call the specified symbol.

Symbol formats:
  - Function name: "ValidateToken"
  - Package.Function: "auth.ValidateToken"
  - Full path: "github.com/pkg/auth.ValidateToken"
  - File:Line: "auth.go:42"

Examples:
  aleutian graph callers "auth.ValidateToken"
  aleutian graph callers "db.Query" --depth 5
  aleutian graph callers "ProcessPayment" --json
  aleutian graph callers "handler.go:42" --direct`,
	Args: cobra.ExactArgs(1),
	Run:  runGraphCallers,
}

// graphCalleesCmd finds callees of a symbol.
var graphCalleesCmd = &cobra.Command{
	Use:   "callees SYMBOL",
	Short: "Find all functions called by a symbol",
	Long: `Find all functions called by the specified symbol.

Symbol formats:
  - Function name: "HandleRequest"
  - Package.Function: "main.HandleRequest"
  - File:Line: "handler.go:42"

Examples:
  aleutian graph callees "main.HandleRequest"
  aleutian graph callees "ProcessData" --depth 3
  aleutian graph callees "Handler" --include-stdlib
  aleutian graph callees "service.Run" --json`,
	Args: cobra.ExactArgs(1),
	Run:  runGraphCallees,
}

// graphPathCmd finds path between two symbols.
var graphPathCmd = &cobra.Command{
	Use:   "path FROM TO",
	Short: "Find call path between two symbols",
	Long: `Find the call path between two symbols.

Uses bidirectional BFS for efficient path finding.

Examples:
  aleutian graph path "main.main" "db.Close"
  aleutian graph path "Handler" "Logger" --all
  aleutian graph path "Start" "Shutdown" --json`,
	Args: cobra.ExactArgs(2),
	Run:  runGraphPath,
}

// =============================================================================
// COMMAND INITIALIZATION
// =============================================================================

func init() {
	// Parent command flags (inherited by subcommands)
	graphCmd.PersistentFlags().BoolVar(&graphJSONOutput, "json", false,
		"Output as JSON for scripting")
	graphCmd.PersistentFlags().StringVar(&graphFormat, "format", "tree",
		"Output format: tree, flat, columns")
	graphCmd.PersistentFlags().IntVar(&graphDepth, "depth", 0,
		"Maximum traversal depth (0 = default)")
	graphCmd.PersistentFlags().IntVar(&graphLimit, "limit", 0,
		"Maximum results (0 = default 1000)")
	graphCmd.PersistentFlags().BoolVar(&graphExact, "exact", false,
		"Require exact symbol match")
	graphCmd.PersistentFlags().BoolVar(&graphFailIfEmpty, "fail-if-empty", false,
		"Exit with error if no results found")

	// Callers-specific flags
	graphCallersCmd.Flags().BoolVar(&graphIncludeTests, "include-tests", false,
		"Include test files in results")
	graphCallersCmd.Flags().Bool("direct", false,
		"Only show direct callers (depth=1)")

	// Callees-specific flags
	graphCalleesCmd.Flags().BoolVar(&graphIncludeStdlib, "include-stdlib", false,
		"Include standard library calls")
	graphCalleesCmd.Flags().BoolVar(&graphIncludeTests, "include-tests", false,
		"Include test files in results")

	// Path-specific flags
	graphPathCmd.Flags().BoolVar(&graphAllPaths, "all", false,
		"Find all paths, not just shortest")
	graphPathCmd.Flags().IntVar(&graphMaxPaths, "max-paths", 10,
		"Maximum number of paths to return")

	// Add subcommands
	graphCmd.AddCommand(graphCallersCmd)
	graphCmd.AddCommand(graphCalleesCmd)
	graphCmd.AddCommand(graphPathCmd)
}

// =============================================================================
// COMMAND IMPLEMENTATIONS
// =============================================================================

// runGraphCallers executes the callers query.
func runGraphCallers(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	symbol := args[0]

	// Load index
	index, err := loadGraphIndex()
	if err != nil {
		outputGraphError("Failed to load index", err)
		os.Exit(graph.ExitError)
	}

	// Build config
	cfg := graph.DefaultQueryConfig()
	if graphDepth > 0 {
		cfg.MaxDepth = graphDepth
	}
	if direct, _ := cmd.Flags().GetBool("direct"); direct {
		cfg.MaxDepth = 1
	}
	if graphLimit > 0 {
		cfg.MaxResults = graphLimit
	}
	cfg.IncludeTests = graphIncludeTests
	cfg.Exact = graphExact
	cfg.FailIfEmpty = graphFailIfEmpty

	// Execute query
	querier := graph.NewQuerier(index)
	result, err := querier.FindCallers(ctx, symbol, cfg)
	if err != nil {
		outputGraphError("Query failed", err)
		os.Exit(graph.ExitError)
	}

	// Check for empty results
	if cfg.FailIfEmpty && result.TotalCount == 0 {
		outputGraphError("No callers found", graph.ErrNoResults)
		os.Exit(graph.ExitError)
	}

	// Output
	if graphJSONOutput {
		outputGraphJSON(result)
	} else {
		outputCallersText(result, graphFormat)
	}

	os.Exit(graph.ExitSuccess)
}

// runGraphCallees executes the callees query.
func runGraphCallees(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	symbol := args[0]

	// Load index
	index, err := loadGraphIndex()
	if err != nil {
		outputGraphError("Failed to load index", err)
		os.Exit(graph.ExitError)
	}

	// Build config
	cfg := graph.DefaultQueryConfig()
	if graphDepth > 0 {
		cfg.MaxDepth = graphDepth
	} else {
		cfg.MaxDepth = 1 // Default to direct callees
	}
	if graphLimit > 0 {
		cfg.MaxResults = graphLimit
	}
	cfg.IncludeStdlib = graphIncludeStdlib
	cfg.IncludeTests = graphIncludeTests
	cfg.Exact = graphExact
	cfg.FailIfEmpty = graphFailIfEmpty

	// Execute query
	querier := graph.NewQuerier(index)
	result, err := querier.FindCallees(ctx, symbol, cfg)
	if err != nil {
		outputGraphError("Query failed", err)
		os.Exit(graph.ExitError)
	}

	// Check for empty results
	if cfg.FailIfEmpty && result.TotalCount == 0 {
		outputGraphError("No callees found", graph.ErrNoResults)
		os.Exit(graph.ExitError)
	}

	// Output
	if graphJSONOutput {
		outputGraphJSON(result)
	} else {
		outputCalleesText(result, graphFormat)
	}

	os.Exit(graph.ExitSuccess)
}

// runGraphPath executes the path query.
func runGraphPath(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fromSymbol := args[0]
	toSymbol := args[1]

	// Load index
	index, err := loadGraphIndex()
	if err != nil {
		outputGraphError("Failed to load index", err)
		os.Exit(graph.ExitError)
	}

	// Build config
	cfg := graph.DefaultQueryConfig()
	if graphDepth > 0 {
		cfg.MaxDepth = graphDepth
	}
	cfg.Exact = graphExact
	cfg.FailIfEmpty = graphFailIfEmpty

	// Execute query
	querier := graph.NewQuerier(index)
	result, err := querier.FindPath(ctx, fromSymbol, toSymbol, cfg, graphAllPaths, graphMaxPaths)
	if err != nil {
		outputGraphError("Query failed", err)
		os.Exit(graph.ExitError)
	}

	// Check for empty results
	if cfg.FailIfEmpty && !result.PathFound {
		outputGraphError("No path found", graph.ErrNoResults)
		os.Exit(graph.ExitError)
	}

	// Output
	if graphJSONOutput {
		outputGraphJSON(result)
	} else {
		outputPathText(result)
	}

	os.Exit(graph.ExitSuccess)
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// loadGraphIndex loads the index from .aleutian/
func loadGraphIndex() (*initializer.MemoryIndex, error) {
	// Find project root
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting working directory: %w", err)
	}

	storage := initializer.NewStorage(cwd)
	if !storage.Exists() {
		return nil, graph.ErrIndexNotFound
	}

	ctx := context.Background()
	index, err := storage.LoadIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading index: %w", err)
	}

	return index, nil
}

// outputGraphError outputs an error message.
func outputGraphError(msg string, err error) {
	if graphJSONOutput {
		result := map[string]interface{}{
			"api_version": graph.APIVersion,
			"success":     false,
			"error":       err.Error(),
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		encoder.Encode(result)
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s: %v\n", msg, err)
	}
}

// outputGraphJSON outputs any result as JSON.
func outputGraphJSON(result interface{}) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to encode JSON: %v\n", err)
		os.Exit(graph.ExitError)
	}
}

// outputCallersText outputs callers result as text.
func outputCallersText(result *graph.QueryResult, format string) {
	fmt.Printf("Callers of %s:\n\n", result.Symbol)

	if result.TotalCount == 0 {
		fmt.Println("  No callers found.")
		return
	}

	switch format {
	case "flat":
		for _, r := range result.Results {
			fmt.Printf("  %s:%d  %s()\n", filepath.Base(r.FilePath), r.Line, r.SymbolName)
		}
	case "columns":
		for _, r := range result.Results {
			fmt.Printf("%s:%d:%s\n", r.FilePath, r.Line, r.SymbolName)
		}
	default: // tree
		outputTree(result.Results)
	}

	fmt.Printf("\nFound %d callers (%d direct, %d transitive)\n",
		result.TotalCount, result.DirectCount, result.TransitiveCount)

	if result.Truncated {
		fmt.Println("  (results truncated)")
	}
}

// outputCalleesText outputs callees result as text.
func outputCalleesText(result *graph.QueryResult, format string) {
	fmt.Printf("Callees of %s:\n\n", result.Symbol)

	if result.TotalCount == 0 {
		fmt.Println("  No callees found.")
		return
	}

	switch format {
	case "flat":
		for _, r := range result.Results {
			fmt.Printf("  %s:%d  %s()\n", filepath.Base(r.FilePath), r.Line, r.SymbolName)
		}
	case "columns":
		for _, r := range result.Results {
			fmt.Printf("%s:%d:%s\n", r.FilePath, r.Line, r.SymbolName)
		}
	default: // tree
		outputTree(result.Results)
	}

	fmt.Printf("\nFound %d callees (%d direct, %d at depth %d)\n",
		result.TotalCount, result.DirectCount, result.TransitiveCount, result.MaxDepthUsed)

	if result.Truncated {
		fmt.Println("  (results truncated)")
	}
}

// outputPathText outputs path result as text.
func outputPathText(result *graph.PathQueryResult) {
	fmt.Printf("Path from %s to %s:\n\n", result.From, result.To)

	if !result.PathFound {
		fmt.Println("  No path found.")
		return
	}

	for i, path := range result.Paths {
		if len(result.Paths) > 1 {
			fmt.Printf("Path %d (%d hops):\n", i+1, path.Length)
		} else {
			fmt.Printf("Path found (%d hops):\n", path.Length)
		}

		names := make([]string, 0, len(path.Symbols))
		for _, sym := range path.Symbols {
			names = append(names, sym.SymbolName)
		}
		fmt.Printf("  %s\n", strings.Join(names, " -> "))
		fmt.Println()
	}

	if result.Truncated {
		fmt.Println("  (more paths available, use --max-paths to see more)")
	}
}

// outputTree outputs results in tree format.
func outputTree(results []graph.CallResult) {
	// Group by depth for simple tree output
	byDepth := make(map[int][]graph.CallResult)
	for _, r := range results {
		byDepth[r.Depth] = append(byDepth[r.Depth], r)
	}

	// Output by depth
	for depth := 1; depth <= len(byDepth); depth++ {
		items := byDepth[depth]
		if len(items) == 0 {
			continue
		}

		indent := strings.Repeat("  ", depth-1)
		prefix := "├──"
		for i, r := range items {
			if i == len(items)-1 {
				prefix = "└──"
			}
			fmt.Printf("%s%s %s:%d  %s()\n", indent, prefix, filepath.Base(r.FilePath), r.Line, r.SymbolName)
		}
	}
}
