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

	"github.com/spf13/cobra"
)

// =============================================================================
// COMMAND FLAGS
// =============================================================================

var (
	healthIncludeLLM bool   // Include LLM-generated summary
	healthJSONOutput bool   // Output as JSON
	healthTimeWindow string // Time window for analysis (e.g., "5m", "1h")
	healthVerbose    bool   // Show detailed per-service information
)

// =============================================================================
// COMMAND DEFINITION
// =============================================================================

// healthCmd is the main health command.
//
// # Description
//
// Provides AI-native health analysis for the Aleutian stack, combining
// basic health checks with intelligent analysis including log anomalies,
// metric trends, code freshness, and optional LLM-generated summaries.
//
// # Examples
//
//	aleutian health              # Basic health report
//	aleutian health --ai         # Include LLM summary
//	aleutian health --json       # JSON output for scripting
//	aleutian health --verbose    # Detailed per-service info
//
// # Limitations
//
//   - LLM summary requires Ollama running with gemma3:1b
//   - Metric trends require prior data collection
//
// # Assumptions
//
//   - Stack services are deployed via podman-compose
//   - Network is accessible on localhost
var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Display intelligent health analysis of the Aleutian stack",
	Long: `Performs comprehensive health analysis of the Aleutian stack.

This command provides AI-native observability beyond simple up/down checks:
  - Service health with latency and error metrics
  - Log anomaly detection
  - Code freshness checking (stale containers)
  - Optional LLM-generated health summary

Examples:
  aleutian health              # Basic health report
  aleutian health --ai         # Include AI-generated summary
  aleutian health --json       # JSON output for automation
  aleutian health --verbose    # Detailed per-service information
  aleutian health -w 15m       # Analyze last 15 minutes`,
	Run: runHealthCommand,
}

// =============================================================================
// COMMAND INITIALIZATION
// =============================================================================

func init() {
	healthCmd.Flags().BoolVar(&healthIncludeLLM, "ai", false,
		"Include AI-generated health summary (requires Ollama)")
	healthCmd.Flags().BoolVar(&healthJSONOutput, "json", false,
		"Output as JSON for scripting")
	healthCmd.Flags().StringVarP(&healthTimeWindow, "window", "w", "5m",
		"Time window for analysis (e.g., 5m, 15m, 1h)")
	healthCmd.Flags().BoolVarP(&healthVerbose, "verbose", "v", false,
		"Show detailed per-service information")
}

// =============================================================================
// COMMAND IMPLEMENTATION
// =============================================================================

// runHealthCommand executes the health analysis and displays results.
//
// # Description
//
// Creates HealthIntelligence instance and performs comprehensive analysis,
// then formats and displays the results based on output mode.
//
// # Inputs
//
//   - cmd: Cobra command (unused)
//   - args: Command arguments (unused)
//
// # Outputs
//
// Prints formatted health report to stdout.
//
// # Limitations
//
//   - Exits with code 1 on critical health issues
//
// # Assumptions
//
//   - Stack directory is configured
func runHealthCommand(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Parse time window
	window, err := time.ParseDuration(healthTimeWindow)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid time window %q: %v\n", healthTimeWindow, err)
		os.Exit(1)
	}

	// Get stack directory
	stackDir := getAleutianStackDir()

	// Create OTel exporter for tracing health analysis
	otelExporter, err := NewHealthOTelExporter(ctx, DefaultHealthOTelConfig())
	if err != nil {
		// Non-fatal: continue without OTel export
		otelExporter = NewNoOpHealthOTelExporter(DefaultHealthOTelConfig())
	}

	// Start OTel span for the health analysis
	ctx, finishSpan := otelExporter.StartHealthAnalysisSpan(ctx, "cli_health_command")
	defer finishSpan(nil)

	// Create dependencies
	proc := NewDefaultProcessManager()
	checker := NewDefaultHealthChecker(proc, DefaultHealthCheckerConfig())
	metricsStore := createMetricsStore(stackDir)
	sanitizer := NewDefaultLogSanitizer(DefaultSanitizationPatterns())

	// Create text generator if LLM summary requested
	var textGen HealthTextGenerator
	if healthIncludeLLM {
		textGen = NewDefaultHealthTextGenerator("http://localhost:11434")
	}

	// Create intelligence instance
	config := DefaultIntelligenceConfig(stackDir)
	intel := NewDefaultHealthIntelligence(checker, proc, textGen, metricsStore, sanitizer, config)

	// Configure analysis options
	opts := AnalysisOptions{
		ID:                GenerateID(),
		TimeWindow:        window,
		Services:          DefaultServiceDefinitions(),
		IncludeLLMSummary: healthIncludeLLM,
		MaxLogLines:       1000,
		CreatedAt:         time.Now(),
	}

	// Perform analysis
	report, err := intel.AnalyzeHealth(ctx, opts)
	if err != nil {
		finishSpan(err)
		fmt.Fprintf(os.Stderr, "Health analysis failed: %v\n", err)
		os.Exit(1)
	}

	// Export to OTel (if configured)
	if exportErr := otelExporter.ExportHealthReport(ctx, report); exportErr != nil {
		// Non-fatal: log but continue
		if healthVerbose {
			fmt.Fprintf(os.Stderr, "OTel export warning: %v\n", exportErr)
		}
	}

	// Include trace ID in report for correlation
	report.TraceID = otelExporter.GetTraceID(ctx)

	// Output based on format
	if healthJSONOutput {
		outputHealthJSON(report)
	} else {
		outputHealthReport(report)
	}

	// Exit with non-zero if critical
	if report.OverallState == IntelligentStateCritical {
		os.Exit(1)
	}
}

// =============================================================================
// OUTPUT FORMATTING
// =============================================================================

// outputHealthJSON outputs the report as JSON.
//
// # Description
//
// Marshals the health report to JSON for scripting and automation.
//
// # Inputs
//
//   - report: Health report to output
//
// # Outputs
//
// Prints JSON to stdout.
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - report is non-nil
func outputHealthJSON(report *IntelligentHealthReport) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to encode JSON: %v\n", err)
		os.Exit(1)
	}
}

// outputHealthReport outputs the formatted health report.
//
// # Description
//
// Formats and displays the health report in a human-readable format
// with box drawing characters for visual appeal.
//
// # Inputs
//
//   - report: Health report to output
//
// # Outputs
//
// Prints formatted report to stdout.
//
// # Limitations
//
//   - Uses Unicode box drawing characters
//
// # Assumptions
//
//   - Terminal supports Unicode
func outputHealthReport(report *IntelligentHealthReport) {
	width := 70

	// Header
	printBoxTop(width)
	printBoxCenter("ALEUTIAN HEALTH REPORT", width)
	printBoxCenter(fmt.Sprintf("Generated: %s", report.Timestamp.Format("2006-01-02 15:04:05")), width)
	printBoxSeparator(width)

	// Overall State
	stateIcon := getStateIcon(report.OverallState)
	stateColor := getStateColor(report.OverallState)
	printBoxLine(fmt.Sprintf("Overall State: %s%s %s%s",
		stateColor, stateIcon, strings.ToUpper(string(report.OverallState)), colorReset), width)

	// AI Summary (if available)
	if report.Summary != "" {
		printBoxSeparator(width)
		printBoxLine("AI Summary:", width)
		// Wrap summary text
		for _, line := range wrapText(report.Summary, width-4) {
			printBoxLine("  "+line, width)
		}
	}

	// Services
	printBoxSeparator(width)
	printBoxLine("Services:", width)
	for _, svc := range report.Services {
		svcIcon := getStateIcon(IntelligentHealthState(svc.BasicHealth.State))
		svcColor := getStateColor(svc.IntelligentState)

		// Build status line
		status := fmt.Sprintf("  %s%s %s%s", svcColor, svcIcon, colorReset, svc.Name)

		// Add state badge
		stateBadge := fmt.Sprintf("[%s]", strings.ToUpper(string(svc.IntelligentState)))
		status += fmt.Sprintf(" %s%s%s", svcColor, stateBadge, colorReset)

		// Add key metrics if available
		if svc.LatencyP99 > 0 {
			status += fmt.Sprintf(" latency: %v", svc.LatencyP99)
			if svc.LatencyTrend == TrendIncreasing {
				status += " (↑)"
			} else if svc.LatencyTrend == TrendDecreasing {
				status += " (↓)"
			}
		}
		if svc.RecentErrors > 0 {
			status += fmt.Sprintf(" errors: %d", svc.RecentErrors)
		}

		printBoxLine(status, width)

		// Verbose mode: show additional details
		if healthVerbose && len(svc.Insights) > 0 {
			for _, insight := range svc.Insights {
				printBoxLine(fmt.Sprintf("      → %s", insight), width)
			}
		}
	}

	// Alerts
	if len(report.Alerts) > 0 {
		printBoxSeparator(width)
		printBoxLine("Alerts:", width)
		for _, alert := range report.Alerts {
			alertIcon := getAlertIcon(alert.Severity)
			alertColor := getAlertColor(alert.Severity)
			printBoxLine(fmt.Sprintf("  %s%s [%s] %s: %s%s",
				alertColor, alertIcon, strings.ToUpper(string(alert.Severity)),
				alert.Service, alert.Title, colorReset), width)
		}
	}

	// Code Freshness
	staleCount := 0
	for _, fr := range report.FreshnessReports {
		if fr.IsStale {
			staleCount++
		}
	}
	printBoxSeparator(width)
	printBoxLine("Code Freshness:", width)
	if staleCount == 0 {
		printBoxLine("  "+colorGreen+"✓ All containers running latest code"+colorReset, width)
	} else {
		printBoxLine(fmt.Sprintf("  "+colorYellow+"⚠ %d container(s) running stale code"+colorReset, staleCount), width)
		if healthVerbose {
			for _, fr := range report.FreshnessReports {
				if fr.IsStale {
					printBoxLine(fmt.Sprintf("      → %s: stale by %v", fr.ServiceName, fr.StaleBy), width)
				}
			}
		}
	}

	// Recommendations
	if len(report.Recommendations) > 0 {
		printBoxSeparator(width)
		printBoxLine("Recommendations:", width)
		for _, rec := range report.Recommendations {
			printBoxLine(fmt.Sprintf("  • %s", rec), width)
		}
	}

	// Trace ID (for OTel correlation)
	if report.TraceID != "" && healthVerbose {
		printBoxSeparator(width)
		printBoxLine(fmt.Sprintf("Trace ID: %s", report.TraceID), width)
	}

	// Footer
	printBoxBottom(width)
	fmt.Printf("\nAnalysis completed in %v\n", report.Duration.Round(time.Millisecond))

	// Show trace ID outside box for easy copying
	if report.TraceID != "" {
		fmt.Printf("Trace ID: %s\n", report.TraceID)
	}
}

// =============================================================================
// BOX DRAWING HELPERS
// =============================================================================

const (
	boxTopLeft     = "╔"
	boxTopRight    = "╗"
	boxBottomLeft  = "╚"
	boxBottomRight = "╝"
	boxHorizontal  = "═"
	boxVertical    = "║"
	boxLeftT       = "╠"
	boxRightT      = "╣"

	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
)

func printBoxTop(width int) {
	fmt.Print(boxTopLeft)
	for i := 0; i < width-2; i++ {
		fmt.Print(boxHorizontal)
	}
	fmt.Println(boxTopRight)
}

func printBoxBottom(width int) {
	fmt.Print(boxBottomLeft)
	for i := 0; i < width-2; i++ {
		fmt.Print(boxHorizontal)
	}
	fmt.Println(boxBottomRight)
}

func printBoxSeparator(width int) {
	fmt.Print(boxLeftT)
	for i := 0; i < width-2; i++ {
		fmt.Print(boxHorizontal)
	}
	fmt.Println(boxRightT)
}

func printBoxLine(content string, width int) {
	// Calculate visible length (excluding ANSI codes)
	visibleLen := visibleLength(content)
	padding := width - 4 - visibleLen
	if padding < 0 {
		padding = 0
	}

	fmt.Printf("%s %s%s %s\n", boxVertical, content, strings.Repeat(" ", padding), boxVertical)
}

func printBoxCenter(content string, width int) {
	visibleLen := visibleLength(content)
	totalPadding := width - 4 - visibleLen
	leftPad := totalPadding / 2
	rightPad := totalPadding - leftPad

	fmt.Printf("%s %s%s%s %s\n", boxVertical,
		strings.Repeat(" ", leftPad), content, strings.Repeat(" ", rightPad), boxVertical)
}

// visibleLength returns the visible length of a string, excluding ANSI escape codes.
func visibleLength(s string) int {
	// Simple ANSI code stripper
	inEscape := false
	visible := 0
	for _, r := range s {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		visible++
	}
	return visible
}

// wrapText wraps text to the specified width.
func wrapText(text string, width int) []string {
	var lines []string
	words := strings.Fields(text)
	if len(words) == 0 {
		return lines
	}

	currentLine := words[0]
	for _, word := range words[1:] {
		if len(currentLine)+1+len(word) <= width {
			currentLine += " " + word
		} else {
			lines = append(lines, currentLine)
			currentLine = word
		}
	}
	if currentLine != "" {
		lines = append(lines, currentLine)
	}
	return lines
}

// =============================================================================
// STATE/ALERT FORMATTING
// =============================================================================

func getStateIcon(state IntelligentHealthState) string {
	switch state {
	case IntelligentStateHealthy:
		return "✓"
	case IntelligentStateDegraded:
		return "◐"
	case IntelligentStateAtRisk:
		return "⚠"
	case IntelligentStateCritical:
		return "✗"
	default:
		return "?"
	}
}

func getStateColor(state IntelligentHealthState) string {
	switch state {
	case IntelligentStateHealthy:
		return colorGreen
	case IntelligentStateDegraded:
		return colorYellow
	case IntelligentStateAtRisk:
		return colorYellow
	case IntelligentStateCritical:
		return colorRed
	default:
		return colorCyan
	}
}

func getAlertIcon(severity AlertSeverity) string {
	switch severity {
	case AlertSeverityInfo:
		return "ℹ"
	case AlertSeverityWarning:
		return "⚠"
	case AlertSeverityError:
		return "✗"
	case AlertSeverityCritical:
		return "🔥"
	default:
		return "•"
	}
}

func getAlertColor(severity AlertSeverity) string {
	switch severity {
	case AlertSeverityInfo:
		return colorBlue
	case AlertSeverityWarning:
		return colorYellow
	case AlertSeverityError:
		return colorRed
	case AlertSeverityCritical:
		return colorRed
	default:
		return colorReset
	}
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// createMetricsStore creates an ephemeral metrics store.
//
// # Description
//
// Creates an in-memory metrics store for the health command.
// Does not persist to disk since health command is ephemeral.
//
// # Inputs
//
//   - stackDir: Stack directory (unused for in-memory store)
//
// # Outputs
//
//   - MetricsStore: In-memory metrics store
//
// # Limitations
//
//   - No persistence between runs
//
// # Assumptions
//
//   - None
func createMetricsStore(stackDir string) MetricsStore {
	config := MetricsStoreConfig{
		InMemoryOnly:       true,
		MaxPointsPerMetric: 100,
		RetentionPeriod:    1 * time.Hour,
	}
	store, _ := NewEphemeralMetricsStore(config)
	return store
}

// getAleutianStackDir returns the Aleutian stack directory.
//
// # Description
//
// Returns the current working directory as the stack directory.
// This assumes the command is run from the Aleutian project root.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - string: Stack directory path
//
// # Limitations
//
//   - Assumes CWD is correct
//
// # Assumptions
//
//   - Command is run from project root
func getAleutianStackDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}
