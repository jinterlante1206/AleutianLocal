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
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Exit codes for CLI commands.
const (
	CLIExitSuccess  = 0 // Operation completed successfully
	CLIExitFindings = 1 // Operation completed with findings/violations
	CLIExitError    = 2 // Operation failed
)

// OutputConfig controls output behavior.
type OutputConfig struct {
	JSON    bool // Output as JSON
	Compact bool // No indentation
	Quiet   bool // No output, exit code only
}

// CommandResult wraps command output with metadata.
type CommandResult struct {
	APIVersion string      `json:"api_version"`
	Command    string      `json:"command"`
	Timestamp  time.Time   `json:"timestamp"`
	DurationMs int64       `json:"duration_ms"`
	Success    bool        `json:"success"`
	Data       interface{} `json:"data,omitempty"`
	Error      string      `json:"error,omitempty"`
}

// OutputJSON writes structured data as JSON to stdout.
//
// # Inputs
//
//   - data: The data to encode. Must be JSON-serializable.
//   - compact: If true, output without indentation.
//
// # Outputs
//
//   - error: Non-nil if encoding fails.
func OutputJSON(data interface{}, compact bool) error {
	encoder := json.NewEncoder(os.Stdout)
	if !compact {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(data)
}

// OutputError writes an error in the appropriate format.
//
// # Inputs
//
//   - jsonMode: If true, output as JSON to stdout.
//   - msg: Human-readable error message.
//   - err: The underlying error.
func OutputError(jsonMode bool, msg string, err error) {
	if jsonMode {
		result := CommandResult{
			APIVersion: "1.0",
			Timestamp:  time.Now(),
			Success:    false,
			Error:      fmt.Sprintf("%s: %v", msg, err),
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		encoder.Encode(result)
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s: %v\n", msg, err)
	}
}

// OutputResult handles all output scenarios with proper formatting.
//
// # Inputs
//
//   - cfg: Output configuration.
//   - cmd: Command name for metadata.
//   - start: Start time for duration calculation.
//   - data: The data to output.
//   - hasFindings: Whether the operation found issues (for exit code).
//   - err: Any error that occurred.
//
// # Outputs
//
//   - int: The exit code to use.
func OutputResult(cfg OutputConfig, cmd string, start time.Time, data interface{}, hasFindings bool, err error) int {
	if cfg.Quiet {
		if err != nil {
			return CLIExitError
		}
		if hasFindings {
			return CLIExitFindings
		}
		return CLIExitSuccess
	}

	if err != nil {
		OutputError(cfg.JSON, "Command failed", err)
		return CLIExitError
	}

	if cfg.JSON {
		result := CommandResult{
			APIVersion: "1.0",
			Command:    cmd,
			Timestamp:  time.Now(),
			DurationMs: time.Since(start).Milliseconds(),
			Success:    true,
			Data:       data,
		}
		if encErr := OutputJSON(result, cfg.Compact); encErr != nil {
			fmt.Fprintf(os.Stderr, "Failed to encode JSON: %v\n", encErr)
			return CLIExitError
		}
	}

	if hasFindings {
		return CLIExitFindings
	}
	return CLIExitSuccess
}

// PolicyVerifyResult holds policy verification output.
type PolicyVerifyResult struct {
	Valid      bool     `json:"valid"`
	Hash       string   `json:"hash"`
	ByteSize   int      `json:"byte_size"`
	RuleCount  int      `json:"rule_count,omitempty"`
	Categories []string `json:"categories,omitempty"`
	Version    string   `json:"version,omitempty"`
}

// PolicyTestResult holds policy test output.
type PolicyTestResult struct {
	Input   string            `json:"input"`
	Matches []PolicyTestMatch `json:"matches"`
	Matched bool              `json:"matched"`
}

// PolicyTestMatch represents a single match in policy test.
type PolicyTestMatch struct {
	Rule           string `json:"rule"`
	Severity       string `json:"severity"`
	Match          string `json:"match,omitempty"`
	Classification string `json:"classification"`
	Confidence     string `json:"confidence"`
	LineNumber     int    `json:"line_number"`
}

// SessionListResult holds session list output.
type SessionListResult struct {
	Sessions []SessionSummary `json:"sessions"`
	Count    int              `json:"count"`
}

// SessionSummary represents a session in list output.
type SessionSummary struct {
	ID           string `json:"id"`
	Summary      string `json:"summary,omitempty"`
	MessageCount int    `json:"message_count,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
}

// WeaviateSummaryResult holds weaviate summary output.
type WeaviateSummaryResult struct {
	Classes       []WeaviateClassInfo `json:"classes"`
	TotalObjects  int                 `json:"total_objects"`
	SchemaVersion string              `json:"schema_version,omitempty"`
}

// WeaviateClassInfo represents a Weaviate class.
type WeaviateClassInfo struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}
