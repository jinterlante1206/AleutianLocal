// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// generate_tool_docs generates a comprehensive markdown reference table from tool_registry.yaml.
//
// Usage:
//
//	go run scripts/generate_tool_docs.go > docs/opensource/trace/mcts/14_tool_reference.md
//
// The generated documentation includes:
//   - Full tool inventory with categories
//   - Keywords and routing guidance
//   - Prerequisites and substitution patterns
//   - Summary statistics
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ToolRegistryYAML is the root structure for YAML deserialization.
type ToolRegistryYAML struct {
	Tools []ToolEntryYAML `yaml:"tools"`
}

// ToolEntryYAML represents a single tool entry in the YAML file.
type ToolEntryYAML struct {
	Name      string                 `yaml:"name"`
	Keywords  []string               `yaml:"keywords"`
	UseWhen   string                 `yaml:"use_when"`
	AvoidWhen string                 `yaml:"avoid_when,omitempty"`
	InsteadOf []ToolSubstitutionYAML `yaml:"instead_of,omitempty"`
	Requires  []string               `yaml:"requires,omitempty"`
}

// ToolSubstitutionYAML represents a tool substitution in YAML.
type ToolSubstitutionYAML struct {
	Tool string `yaml:"tool"`
	When string `yaml:"when"`
}

// ToolCategory represents a category of tools.
type ToolCategory struct {
	Name        string
	Description string
	Tools       []ToolEntryYAML
}

func main() {
	// Read the YAML file
	data, err := os.ReadFile("services/trace/config/tool_registry.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading tool_registry.yaml: %v\n", err)
		os.Exit(1)
	}

	var registry ToolRegistryYAML
	if err := yaml.Unmarshal(data, &registry); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing YAML: %v\n", err)
		os.Exit(1)
	}

	// Categorize tools
	categories := categorizeTools(registry.Tools)

	// Generate markdown
	generateMarkdown(categories, registry.Tools)
}

// categorizeTools groups tools into categories based on their names.
func categorizeTools(tools []ToolEntryYAML) []ToolCategory {
	categoryMap := map[string]*ToolCategory{
		"graph_query": {
			Name:        "Graph Query Tools",
			Description: "Tools for querying the code graph (callers, callees, implementations). These provide structural understanding of code relationships.",
		},
		"file": {
			Name:        "File Tools",
			Description: "Tools for reading, writing, and searching files. Basic file system operations.",
		},
		"bash": {
			Name:        "Bash Tools",
			Description: "Tools for executing shell commands (tests, builds, git).",
		},
		"graph_exploration": {
			Name:        "Graph Exploration Tools",
			Description: "Tools for exploring the codebase structure (entry points, data flow, error flow).",
		},
		"graph_reasoning": {
			Name:        "Graph Reasoning Tools",
			Description: "Tools for reasoning about code (breaking changes, side effects, test coverage).",
		},
		"graph_coordination": {
			Name:        "Graph Coordination Tools",
			Description: "Tools for coordinating multi-file changes and validating plans.",
		},
		"graph_patterns": {
			Name:        "Graph Pattern Tools",
			Description: "Tools for detecting patterns, code smells, and code quality issues.",
		},
		"special": {
			Name:        "Special Tools",
			Description: "Meta-tools that control routing behavior.",
		},
	}

	// Categorization rules
	for _, tool := range tools {
		switch {
		case tool.Name == "find_callers" || tool.Name == "find_callees" ||
			tool.Name == "find_implementations" || tool.Name == "find_references" ||
			tool.Name == "find_symbol" || tool.Name == "get_call_chain" ||
			tool.Name == "find_path":
			categoryMap["graph_query"].Tools = append(categoryMap["graph_query"].Tools, tool)

		case tool.Name == "Read" || tool.Name == "Grep" || tool.Name == "Glob" ||
			tool.Name == "Edit" || tool.Name == "Write" || tool.Name == "Diff" ||
			tool.Name == "Tree" || tool.Name == "JSON":
			categoryMap["file"].Tools = append(categoryMap["file"].Tools, tool)

		case tool.Name == "Bash":
			categoryMap["bash"].Tools = append(categoryMap["bash"].Tools, tool)

		case tool.Name == "find_entry_points" || tool.Name == "trace_data_flow" ||
			tool.Name == "trace_error_flow" || tool.Name == "find_config_usage" ||
			tool.Name == "find_similar_code" || tool.Name == "build_minimal_context" ||
			tool.Name == "summarize_file" || tool.Name == "summarize_package" ||
			tool.Name == "analyze_change_impact":
			categoryMap["graph_exploration"].Tools = append(categoryMap["graph_exploration"].Tools, tool)

		case tool.Name == "check_breaking_changes" || tool.Name == "simulate_change" ||
			tool.Name == "validate_change" || tool.Name == "find_test_coverage" ||
			tool.Name == "detect_side_effects" || tool.Name == "suggest_refactor":
			categoryMap["graph_reasoning"].Tools = append(categoryMap["graph_reasoning"].Tools, tool)

		case tool.Name == "plan_multi_file_change" || tool.Name == "validate_plan" ||
			tool.Name == "preview_changes":
			categoryMap["graph_coordination"].Tools = append(categoryMap["graph_coordination"].Tools, tool)

		case tool.Name == "detect_patterns" || tool.Name == "find_code_smells" ||
			tool.Name == "find_duplication" || tool.Name == "find_circular_deps" ||
			tool.Name == "extract_conventions" || tool.Name == "find_dead_code" ||
			tool.Name == "find_hotspots" || tool.Name == "find_cycles":
			categoryMap["graph_patterns"].Tools = append(categoryMap["graph_patterns"].Tools, tool)

		case tool.Name == "answer":
			categoryMap["special"].Tools = append(categoryMap["special"].Tools, tool)
		}
	}

	// Convert to sorted slice
	order := []string{
		"graph_query",
		"file",
		"bash",
		"graph_exploration",
		"graph_reasoning",
		"graph_coordination",
		"graph_patterns",
		"special",
	}

	var result []ToolCategory
	for _, key := range order {
		if cat, ok := categoryMap[key]; ok && len(cat.Tools) > 0 {
			result = append(result, *cat)
		}
	}

	return result
}

// generateMarkdown outputs the full markdown documentation.
func generateMarkdown(categories []ToolCategory, allTools []ToolEntryYAML) {
	fmt.Println("# Tool Reference")
	fmt.Println()
	fmt.Println("## Overview")
	fmt.Println()
	fmt.Println("This document provides a comprehensive reference for all tools available in Aleutian Trace.")
	fmt.Println("The tool registry is defined in `services/trace/config/tool_registry.yaml` and loaded at startup.")
	fmt.Println()
	fmt.Printf("**Generated:** %s\n", time.Now().Format("2006-01-02 15:04:05 UTC"))
	fmt.Println()

	// Statistics
	totalKeywords := 0
	requiresGraph := 0
	hasSubstitutions := 0
	for _, tool := range allTools {
		totalKeywords += len(tool.Keywords)
		if len(tool.Requires) > 0 {
			requiresGraph++
		}
		if len(tool.InsteadOf) > 0 {
			hasSubstitutions++
		}
	}

	fmt.Println("## Summary Statistics")
	fmt.Println()
	fmt.Println("| Metric | Count |")
	fmt.Println("|--------|-------|")
	fmt.Printf("| Total Tools | %d |\n", len(allTools))
	fmt.Printf("| Total Keywords | %d |\n", totalKeywords)
	fmt.Printf("| Tools Requiring Graph | %d |\n", requiresGraph)
	fmt.Printf("| Tools with Substitutions | %d |\n", hasSubstitutions)
	fmt.Printf("| Tool Categories | %d |\n", len(categories))
	fmt.Println()

	// Table of contents
	fmt.Println("## Table of Contents")
	fmt.Println()
	for i, cat := range categories {
		fmt.Printf("%d. [%s](#%s)\n", i+1, cat.Name, strings.ToLower(strings.ReplaceAll(cat.Name, " ", "-")))
	}
	fmt.Println()

	// Quick reference table (all tools)
	fmt.Println("---")
	fmt.Println()
	fmt.Println("## Quick Reference")
	fmt.Println()
	fmt.Println("| Tool | Category | Keywords (Top 3) | Requires Graph | Has Substitution |")
	fmt.Println("|------|----------|------------------|----------------|------------------|")

	for _, cat := range categories {
		for _, tool := range cat.Tools {
			keywords := tool.Keywords
			if len(keywords) > 3 {
				keywords = keywords[:3]
			}
			keywordStr := strings.Join(keywords, ", ")
			if len(tool.Keywords) > 3 {
				keywordStr += ", ..."
			}

			requiresGraphStr := "No"
			if len(tool.Requires) > 0 && containsGraphInit(tool.Requires) {
				requiresGraphStr = "Yes"
			}

			hasSubstitution := "No"
			if len(tool.InsteadOf) > 0 {
				hasSubstitution = "Yes"
			}

			fmt.Printf("| `%s` | %s | %s | %s | %s |\n",
				tool.Name,
				cat.Name,
				keywordStr,
				requiresGraphStr,
				hasSubstitution,
			)
		}
	}
	fmt.Println()

	// Detailed sections per category
	fmt.Println("---")
	fmt.Println()
	for _, cat := range categories {
		fmt.Printf("## %s\n", cat.Name)
		fmt.Println()
		fmt.Println(cat.Description)
		fmt.Println()

		for _, tool := range cat.Tools {
			printToolDetails(tool)
		}
	}

	// Keyword index
	fmt.Println("---")
	fmt.Println()
	fmt.Println("## Keyword Index")
	fmt.Println()
	fmt.Println("This index maps keywords to the tools they trigger. Use this to understand how")
	fmt.Println("the router will respond to different query patterns.")
	fmt.Println()

	keywordIndex := buildKeywordIndex(allTools)
	keywords := make([]string, 0, len(keywordIndex))
	for k := range keywordIndex {
		keywords = append(keywords, k)
	}
	sort.Strings(keywords)

	fmt.Println("| Keyword | Triggers Tools |")
	fmt.Println("|---------|----------------|")
	for _, kw := range keywords {
		tools := keywordIndex[kw]
		toolStr := strings.Join(tools, ", ")
		fmt.Printf("| `%s` | %s |\n", kw, toolStr)
	}
	fmt.Println()

	// Substitution patterns
	fmt.Println("---")
	fmt.Println()
	fmt.Println("## Substitution Patterns")
	fmt.Println()
	fmt.Println("Substitution patterns tell the router when to prefer one tool over another.")
	fmt.Println("This helps avoid suboptimal tool selection (e.g., using Grep when find_callers is better).")
	fmt.Println()

	fmt.Println("| Preferred Tool | Instead Of | When |")
	fmt.Println("|----------------|------------|------|")
	for _, tool := range allTools {
		for _, sub := range tool.InsteadOf {
			fmt.Printf("| `%s` | `%s` | %s |\n", tool.Name, sub.Tool, sub.When)
		}
	}
	fmt.Println()

	// Prerequisites reference
	fmt.Println("---")
	fmt.Println()
	fmt.Println("## Prerequisites Reference")
	fmt.Println()
	fmt.Println("Some tools require certain conditions to be met before they can be used.")
	fmt.Println()

	fmt.Println("| Prerequisite | Description | Tools Requiring |")
	fmt.Println("|--------------|-------------|-----------------|")

	prereqTools := make(map[string][]string)
	for _, tool := range allTools {
		for _, req := range tool.Requires {
			prereqTools[req] = append(prereqTools[req], tool.Name)
		}
	}

	prereqDescs := map[string]string{
		"graph_initialized": "Code graph must be built before using graph query tools",
		"file_read":         "File must be read before it can be edited",
	}

	for prereq, tools := range prereqTools {
		desc := prereqDescs[prereq]
		if desc == "" {
			desc = "Prerequisite"
		}
		fmt.Printf("| `%s` | %s | %s |\n", prereq, desc, strings.Join(tools, ", "))
	}
	fmt.Println()

	// Footer
	fmt.Println("---")
	fmt.Println()
	fmt.Println("*This document is auto-generated from `services/trace/config/tool_registry.yaml`.*")
	fmt.Println()
	fmt.Println("*To regenerate: `go run scripts/generate_tool_docs.go > docs/opensource/trace/mcts/14_tool_reference.md`*")
}

// printToolDetails prints detailed information for a single tool.
func printToolDetails(tool ToolEntryYAML) {
	fmt.Printf("### `%s`\n", tool.Name)
	fmt.Println()

	// Main table
	fmt.Println("| Property | Value |")
	fmt.Println("|----------|-------|")
	fmt.Printf("| **Use When** | %s |\n", tool.UseWhen)
	if tool.AvoidWhen != "" {
		fmt.Printf("| **Avoid When** | %s |\n", tool.AvoidWhen)
	}
	if len(tool.Requires) > 0 {
		fmt.Printf("| **Requires** | %s |\n", strings.Join(tool.Requires, ", "))
	}
	fmt.Println()

	// Keywords
	fmt.Println("**Keywords:**")
	fmt.Println()
	fmt.Print("`")
	fmt.Print(strings.Join(tool.Keywords, "`, `"))
	fmt.Println("`")
	fmt.Println()

	// Substitutions
	if len(tool.InsteadOf) > 0 {
		fmt.Println("**Substitutes for:**")
		fmt.Println()
		for _, sub := range tool.InsteadOf {
			fmt.Printf("- `%s` when: %s\n", sub.Tool, sub.When)
		}
		fmt.Println()
	}
}

// buildKeywordIndex creates a map of keyword -> tool names.
func buildKeywordIndex(tools []ToolEntryYAML) map[string][]string {
	index := make(map[string][]string)
	for _, tool := range tools {
		for _, kw := range tool.Keywords {
			index[kw] = append(index[kw], tool.Name)
		}
	}
	return index
}

// containsGraphInit checks if requires list contains graph_initialized.
func containsGraphInit(requires []string) bool {
	for _, r := range requires {
		if r == "graph_initialized" {
			return true
		}
	}
	return false
}
