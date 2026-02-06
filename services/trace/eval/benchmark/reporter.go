// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package benchmark

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// -----------------------------------------------------------------------------
// Reporter Interface
// -----------------------------------------------------------------------------

// Reporter formats and outputs benchmark results.
//
// Description:
//
//	Reporter provides a standardized interface for outputting benchmark
//	results in various formats. Implementations include console (human-readable)
//	and JSON (machine-readable) formats.
//
// Thread Safety: Implementations must be safe for concurrent use.
type Reporter interface {
	// Report writes a single benchmark result to the output.
	//
	// Inputs:
	//   - result: The benchmark result to report. Must not be nil.
	//
	// Outputs:
	//   - error: Non-nil if writing failed.
	Report(result *Result) error

	// ReportComparison writes a comparison result to the output.
	//
	// Inputs:
	//   - comparison: The comparison result to report. Must not be nil.
	//
	// Outputs:
	//   - error: Non-nil if writing failed.
	ReportComparison(comparison *ComparisonResult) error

	// ReportAll writes multiple results to the output.
	//
	// Inputs:
	//   - results: The benchmark results to report.
	//
	// Outputs:
	//   - error: Non-nil if writing failed.
	ReportAll(results []*Result) error
}

// -----------------------------------------------------------------------------
// Console Reporter
// -----------------------------------------------------------------------------

// ConsoleReporter outputs benchmark results to the console in human-readable format.
//
// Description:
//
//	ConsoleReporter formats benchmark results as human-readable text with
//	tables and statistics. Supports verbose mode for detailed memory stats.
//
// Thread Safety: Safe for concurrent use if the underlying Writer is safe.
type ConsoleReporter struct {
	out     io.Writer
	verbose bool
}

// NewConsoleReporter creates a new console reporter.
//
// Description:
//
//	Creates a reporter that outputs human-readable benchmark results
//	to the specified writer. Verbose mode includes memory statistics.
//
// Inputs:
//   - out: Writer to output to. Must not be nil.
//   - verbose: Whether to include detailed statistics.
//
// Outputs:
//   - *ConsoleReporter: The new reporter. Never nil.
//
// Example:
//
//	reporter := benchmark.NewConsoleReporter(os.Stdout, true)
//	reporter.Report(result)
//
// Assumptions:
//   - The writer is ready to accept output and will not be nil.
func NewConsoleReporter(out io.Writer, verbose bool) *ConsoleReporter {
	return &ConsoleReporter{
		out:     out,
		verbose: verbose,
	}
}

// Report writes a benchmark result to the console.
//
// Description:
//
//	Formats and writes the benchmark result including iterations,
//	latency statistics, percentiles, throughput, and optionally memory stats.
//
// Inputs:
//   - result: The benchmark result to report. Must not be nil.
//
// Outputs:
//   - error: Non-nil if writing failed.
//
// Thread Safety: Safe for concurrent use if the underlying Writer is safe.
func (r *ConsoleReporter) Report(result *Result) error {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Benchmark: %s\n", result.Name))
	sb.WriteString(strings.Repeat("─", 60) + "\n")
	sb.WriteString(fmt.Sprintf("  Iterations: %d\n", result.Iterations))
	sb.WriteString(fmt.Sprintf("  Errors:     %d (%.2f%%)\n", result.Errors, result.ErrorRate*100))
	sb.WriteString(fmt.Sprintf("  Total Time: %v\n", result.TotalDuration))
	sb.WriteString("\n")

	sb.WriteString("  Latency:\n")
	sb.WriteString(fmt.Sprintf("    Min:    %v\n", result.Latency.Min))
	sb.WriteString(fmt.Sprintf("    Mean:   %v\n", result.Latency.Mean))
	sb.WriteString(fmt.Sprintf("    Median: %v\n", result.Latency.Median))
	sb.WriteString(fmt.Sprintf("    Max:    %v\n", result.Latency.Max))
	sb.WriteString(fmt.Sprintf("    StdDev: %v\n", result.Latency.StdDev))
	sb.WriteString("\n")

	sb.WriteString("  Percentiles:\n")
	sb.WriteString(fmt.Sprintf("    P50:  %v\n", result.Latency.P50))
	sb.WriteString(fmt.Sprintf("    P90:  %v\n", result.Latency.P90))
	sb.WriteString(fmt.Sprintf("    P95:  %v\n", result.Latency.P95))
	sb.WriteString(fmt.Sprintf("    P99:  %v\n", result.Latency.P99))
	sb.WriteString(fmt.Sprintf("    P999: %v\n", result.Latency.P999))
	sb.WriteString("\n")

	sb.WriteString("  Throughput:\n")
	sb.WriteString(fmt.Sprintf("    Ops/sec: %.2f\n", result.Throughput.OpsPerSecond))

	if result.Memory != nil && r.verbose {
		sb.WriteString("\n")
		sb.WriteString("  Memory:\n")
		sb.WriteString(fmt.Sprintf("    Heap Before: %s\n", formatBytes(result.Memory.HeapAllocBefore)))
		sb.WriteString(fmt.Sprintf("    Heap After:  %s\n", formatBytes(result.Memory.HeapAllocAfter)))
		sb.WriteString(fmt.Sprintf("    Heap Delta:  %s\n", formatBytesDelta(result.Memory.HeapAllocDelta)))
		sb.WriteString(fmt.Sprintf("    GC Pauses:   %d\n", result.Memory.GCPauses))
		sb.WriteString(fmt.Sprintf("    GC Total:    %v\n", result.Memory.GCPauseTotal))
	}

	sb.WriteString("\n")
	_, err := io.WriteString(r.out, sb.String())
	return err
}

// ReportComparison writes a comparison result to the console.
//
// Description:
//
//	Formats and writes the comparison result including a table of all
//	results, statistical analysis, and winner determination.
//
// Inputs:
//   - comparison: The comparison result to report. Must not be nil.
//
// Outputs:
//   - error: Non-nil if writing failed.
//
// Thread Safety: Safe for concurrent use if the underlying Writer is safe.
func (r *ConsoleReporter) ReportComparison(comparison *ComparisonResult) error {
	var sb strings.Builder

	sb.WriteString("Benchmark Comparison\n")
	sb.WriteString(strings.Repeat("═", 60) + "\n\n")

	// Results table
	sb.WriteString("Results:\n")
	sb.WriteString("┌" + strings.Repeat("─", 20) + "┬" + strings.Repeat("─", 15) + "┬" + strings.Repeat("─", 15) + "┐\n")
	sb.WriteString(fmt.Sprintf("│ %-18s │ %13s │ %13s │\n", "Component", "Mean", "P99"))
	sb.WriteString("├" + strings.Repeat("─", 20) + "┼" + strings.Repeat("─", 15) + "┼" + strings.Repeat("─", 15) + "┤\n")

	for _, name := range comparison.Ranking {
		result := comparison.Results[name]
		marker := ""
		if name == comparison.Winner {
			marker = " *"
		}
		sb.WriteString(fmt.Sprintf("│ %-18s │ %13v │ %13v │%s\n",
			truncate(name, 18), result.Latency.Mean, result.Latency.P99, marker))
	}
	sb.WriteString("└" + strings.Repeat("─", 20) + "┴" + strings.Repeat("─", 15) + "┴" + strings.Repeat("─", 15) + "┘\n\n")

	// Statistical analysis
	sb.WriteString("Statistical Analysis:\n")
	sb.WriteString(fmt.Sprintf("  Speedup:      %.2fx\n", comparison.Speedup))
	sb.WriteString(fmt.Sprintf("  P-Value:      %.4f\n", comparison.PValue))
	sb.WriteString(fmt.Sprintf("  Effect Size:  %.2f (%s)\n", comparison.EffectSize, comparison.EffectSizeCategory))
	sb.WriteString(fmt.Sprintf("  Significant:  %v (alpha=%.2f)\n", comparison.Significant, 1-comparison.ConfidenceLevel))

	if comparison.Winner != "" {
		sb.WriteString(fmt.Sprintf("\n  Winner: %s (%.2fx faster)\n", comparison.Winner, comparison.Speedup))
	} else {
		sb.WriteString("\n  No statistically significant winner.\n")
	}

	sb.WriteString("\n")
	_, err := io.WriteString(r.out, sb.String())
	return err
}

// ReportAll writes multiple results to the console.
//
// Description:
//
//	Writes each result sequentially using Report.
//
// Inputs:
//   - results: The benchmark results to report.
//
// Outputs:
//   - error: Non-nil if writing any result failed.
//
// Thread Safety: Safe for concurrent use if the underlying Writer is safe.
func (r *ConsoleReporter) ReportAll(results []*Result) error {
	for _, result := range results {
		if err := r.Report(result); err != nil {
			return fmt.Errorf("reporting result %s: %w", result.Name, err)
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// JSON Reporter
// -----------------------------------------------------------------------------

// JSONReporter outputs benchmark results as JSON.
//
// Description:
//
//	JSONReporter formats benchmark results as JSON for machine consumption.
//	Supports pretty-printing for human readability.
//
// Thread Safety: Safe for concurrent use if the underlying Writer is safe.
type JSONReporter struct {
	out    io.Writer
	pretty bool
}

// NewJSONReporter creates a new JSON reporter.
//
// Description:
//
//	Creates a reporter that outputs benchmark results as JSON
//	to the specified writer. Pretty mode adds indentation.
//
// Inputs:
//   - out: Writer to output to. Must not be nil.
//   - pretty: Whether to pretty-print the JSON with indentation.
//
// Outputs:
//   - *JSONReporter: The new reporter. Never nil.
//
// Example:
//
//	reporter := benchmark.NewJSONReporter(file, true)
//	reporter.Report(result)
//
// Assumptions:
//   - The writer is ready to accept output and will not be nil.
func NewJSONReporter(out io.Writer, pretty bool) *JSONReporter {
	return &JSONReporter{
		out:    out,
		pretty: pretty,
	}
}

// jsonResult is the JSON-serializable form of Result.
type jsonResult struct {
	Name          string           `json:"name"`
	Iterations    int              `json:"iterations"`
	Errors        int              `json:"errors"`
	ErrorRate     float64          `json:"error_rate"`
	TotalDuration string           `json:"total_duration"`
	Latency       jsonLatencyStats `json:"latency"`
	Throughput    jsonThroughput   `json:"throughput"`
	Memory        *jsonMemoryStats `json:"memory,omitempty"`
	Timestamp     time.Time        `json:"timestamp"`
}

type jsonLatencyStats struct {
	Min    string `json:"min"`
	Max    string `json:"max"`
	Mean   string `json:"mean"`
	Median string `json:"median"`
	StdDev string `json:"stddev"`
	P50    string `json:"p50"`
	P90    string `json:"p90"`
	P95    string `json:"p95"`
	P99    string `json:"p99"`
	P999   string `json:"p999"`
}

type jsonThroughput struct {
	OpsPerSecond   float64 `json:"ops_per_second"`
	BytesPerSecond float64 `json:"bytes_per_second,omitempty"`
	ItemsPerSecond float64 `json:"items_per_second,omitempty"`
}

type jsonMemoryStats struct {
	HeapAllocBefore uint64 `json:"heap_alloc_before"`
	HeapAllocAfter  uint64 `json:"heap_alloc_after"`
	HeapAllocDelta  int64  `json:"heap_alloc_delta"`
	GCPauses        uint32 `json:"gc_pauses"`
	GCPauseTotal    string `json:"gc_pause_total"`
}

// Report writes a benchmark result as JSON.
//
// Description:
//
//	Converts the result to JSON-serializable form and writes it.
//
// Inputs:
//   - result: The benchmark result to report. Must not be nil.
//
// Outputs:
//   - error: Non-nil if conversion or writing failed.
//
// Thread Safety: Safe for concurrent use if the underlying Writer is safe.
func (r *JSONReporter) Report(result *Result) error {
	jr := r.convertResult(result)
	return r.writeJSON(jr)
}

// ReportComparison writes a comparison result as JSON.
//
// Description:
//
//	Converts the comparison to JSON-serializable form and writes it.
//
// Inputs:
//   - comparison: The comparison result to report. Must not be nil.
//
// Outputs:
//   - error: Non-nil if conversion or writing failed.
//
// Thread Safety: Safe for concurrent use if the underlying Writer is safe.
func (r *JSONReporter) ReportComparison(comparison *ComparisonResult) error {
	type jsonComparison struct {
		Results            map[string]jsonResult `json:"results"`
		Winner             string                `json:"winner,omitempty"`
		Speedup            float64               `json:"speedup"`
		Significant        bool                  `json:"significant"`
		PValue             float64               `json:"p_value"`
		ConfidenceLevel    float64               `json:"confidence_level"`
		EffectSize         float64               `json:"effect_size"`
		EffectSizeCategory string                `json:"effect_size_category"`
		Ranking            []string              `json:"ranking"`
	}

	jc := jsonComparison{
		Results:            make(map[string]jsonResult),
		Winner:             comparison.Winner,
		Speedup:            comparison.Speedup,
		Significant:        comparison.Significant,
		PValue:             comparison.PValue,
		ConfidenceLevel:    comparison.ConfidenceLevel,
		EffectSize:         comparison.EffectSize,
		EffectSizeCategory: comparison.EffectSizeCategory.String(),
		Ranking:            comparison.Ranking,
	}

	for name, result := range comparison.Results {
		jc.Results[name] = r.convertResult(result)
	}

	return r.writeJSON(jc)
}

// ReportAll writes multiple results as a JSON array.
//
// Description:
//
//	Converts all results to JSON-serializable form and writes as an array.
//
// Inputs:
//   - results: The benchmark results to report.
//
// Outputs:
//   - error: Non-nil if conversion or writing failed.
//
// Thread Safety: Safe for concurrent use if the underlying Writer is safe.
func (r *JSONReporter) ReportAll(results []*Result) error {
	jrs := make([]jsonResult, 0, len(results))
	for _, result := range results {
		jrs = append(jrs, r.convertResult(result))
	}
	return r.writeJSON(jrs)
}

// convertResult converts a Result to its JSON-serializable form.
func (r *JSONReporter) convertResult(result *Result) jsonResult {
	jr := jsonResult{
		Name:          result.Name,
		Iterations:    result.Iterations,
		Errors:        result.Errors,
		ErrorRate:     result.ErrorRate,
		TotalDuration: result.TotalDuration.String(),
		Latency: jsonLatencyStats{
			Min:    result.Latency.Min.String(),
			Max:    result.Latency.Max.String(),
			Mean:   result.Latency.Mean.String(),
			Median: result.Latency.Median.String(),
			StdDev: result.Latency.StdDev.String(),
			P50:    result.Latency.P50.String(),
			P90:    result.Latency.P90.String(),
			P95:    result.Latency.P95.String(),
			P99:    result.Latency.P99.String(),
			P999:   result.Latency.P999.String(),
		},
		Throughput: jsonThroughput{
			OpsPerSecond:   result.Throughput.OpsPerSecond,
			BytesPerSecond: result.Throughput.BytesPerSecond,
			ItemsPerSecond: result.Throughput.ItemsPerSecond,
		},
		Timestamp: time.UnixMilli(result.Timestamp),
	}

	if result.Memory != nil {
		jr.Memory = &jsonMemoryStats{
			HeapAllocBefore: result.Memory.HeapAllocBefore,
			HeapAllocAfter:  result.Memory.HeapAllocAfter,
			HeapAllocDelta:  result.Memory.HeapAllocDelta,
			GCPauses:        result.Memory.GCPauses,
			GCPauseTotal:    result.Memory.GCPauseTotal.String(),
		}
	}

	return jr
}

// writeJSON encodes and writes the value as JSON.
func (r *JSONReporter) writeJSON(v any) error {
	encoder := json.NewEncoder(r.out)
	if r.pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(v)
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// formatBytes formats a byte count as a human-readable string.
func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// formatBytesDelta formats a byte delta as a signed human-readable string.
func formatBytesDelta(b int64) string {
	if b < 0 {
		return "-" + formatBytes(uint64(-b))
	}
	return "+" + formatBytes(uint64(b))
}

// abs returns the absolute value of n.
func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// truncate truncates a string to max characters with ellipsis.
func truncate(s string, max int) string {
	if max <= 3 {
		return s[:max]
	}
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
