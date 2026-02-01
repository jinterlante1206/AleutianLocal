// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// ErrorRecovery provides error analysis and fix suggestions for tool failures.
//
// Description:
//
//	Analyzes tool execution errors and suggests fixes that can be
//	relayed to the LLM to help it recover from failures.
//
// Thread Safety: ErrorRecovery is safe for concurrent use.
type ErrorRecovery struct {
	mu sync.RWMutex

	// customSuggestions maps error substrings to suggestions.
	customSuggestions map[string]string
}

// NewErrorRecovery creates a new error recovery helper.
//
// Outputs:
//
//	*ErrorRecovery - The configured recovery helper
func NewErrorRecovery() *ErrorRecovery {
	return &ErrorRecovery{
		customSuggestions: make(map[string]string),
	}
}

// AddCustomSuggestion adds a custom error pattern and suggestion.
//
// Description:
//
//	Registers a custom suggestion for errors matching a pattern.
//	The pattern is matched as a substring of the error message.
//
// Inputs:
//
//	pattern - Substring to match in error messages
//	suggestion - Suggestion to return when pattern matches
//
// Thread Safety: This method is safe for concurrent use.
func (r *ErrorRecovery) AddCustomSuggestion(pattern, suggestion string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.customSuggestions[pattern] = suggestion
}

// matchCustomSuggestion checks for a matching custom suggestion.
// Returns the suggestion if found, empty string otherwise.
func (r *ErrorRecovery) matchCustomSuggestion(errMsg string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for pattern, suggestion := range r.customSuggestions {
		if strings.Contains(errMsg, pattern) {
			return suggestion
		}
	}
	return ""
}

// SuggestFix analyzes an error and suggests a recovery action.
//
// Description:
//
//	Examines the error and tool call context to suggest fixes.
//	Returns an empty string if no suggestion is available.
//
// Inputs:
//
//	err - The error that occurred
//	call - The tool call that failed
//
// Outputs:
//
//	string - A suggested fix, or empty string if none available
//
// Thread Safety: This method is safe for concurrent use.
func (r *ErrorRecovery) SuggestFix(err error, call ToolCall) string {
	if err == nil {
		return ""
	}

	errMsg := err.Error()

	// Check custom suggestions first
	if suggestion := r.matchCustomSuggestion(errMsg); suggestion != "" {
		return suggestion
	}

	// File not found errors
	if errors.Is(err, os.ErrNotExist) || strings.Contains(errMsg, "no such file") ||
		strings.Contains(errMsg, "file not found") || strings.Contains(errMsg, "does not exist") {
		return r.suggestFileNotFound(call)
	}

	// Permission errors
	if errors.Is(err, os.ErrPermission) || strings.Contains(errMsg, "permission denied") ||
		strings.Contains(errMsg, "access denied") {
		return r.suggestPermissionDenied(call)
	}

	// Timeout errors
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(errMsg, "timeout") ||
		strings.Contains(errMsg, "deadline exceeded") {
		return r.suggestTimeout(call)
	}

	// Context cancelled
	if errors.Is(err, context.Canceled) || strings.Contains(errMsg, "context canceled") {
		return "The operation was cancelled. You may retry if the user hasn't explicitly asked to stop."
	}

	// Validation errors
	if errors.Is(err, ErrValidationFailed) || strings.Contains(errMsg, "validation") {
		return r.suggestValidation(call, errMsg)
	}

	// Tool not found
	if errors.Is(err, ErrToolNotFound) || strings.Contains(errMsg, "tool not found") ||
		strings.Contains(errMsg, "unknown tool") {
		return r.suggestToolNotFound(call)
	}

	// Requirement not met
	if errors.Is(err, ErrRequirementNotMet) || strings.Contains(errMsg, "requirement not met") {
		return r.suggestRequirementNotMet(call, errMsg)
	}

	// Network errors
	if strings.Contains(errMsg, "connection refused") || strings.Contains(errMsg, "network") ||
		strings.Contains(errMsg, "dial tcp") {
		return "Network error occurred. The service may be unavailable. Try again later or check connectivity."
	}

	// Memory/resource errors
	if strings.Contains(errMsg, "out of memory") || strings.Contains(errMsg, "too large") {
		return "Resource limit exceeded. Try operating on smaller data or fewer items at once."
	}

	// Parse errors
	if strings.Contains(errMsg, "parse error") || strings.Contains(errMsg, "syntax error") ||
		strings.Contains(errMsg, "unexpected token") {
		return "The input contains syntax errors. Check for proper formatting and escaping."
	}

	// JSON errors
	if strings.Contains(errMsg, "json") || strings.Contains(errMsg, "unmarshal") ||
		strings.Contains(errMsg, "marshal") {
		return "JSON parsing error. Ensure parameters are valid JSON with proper escaping and data types."
	}

	return ""
}

func (r *ErrorRecovery) suggestFileNotFound(call ToolCall) string {
	params, err := call.ParamsMap()
	if err != nil {
		return "File does not exist. Use a file listing or glob tool to find similar files."
	}

	// Try to extract the file path from common parameter names
	var path string
	for _, key := range []string{"path", "file_path", "file", "filepath", "filename"} {
		if v, ok := params[key].(string); ok {
			path = v
			break
		}
	}

	if path != "" {
		return fmt.Sprintf("File '%s' does not exist. Try:\n"+
			"1. Use a glob pattern to find similar files (e.g., *%s* or similar)\n"+
			"2. List the directory contents to see available files\n"+
			"3. Check if the path is relative or absolute\n"+
			"4. The file may have a different extension", path, extractFilename(path))
	}

	return "File does not exist. Use a file listing or glob tool to find similar files."
}

func (r *ErrorRecovery) suggestPermissionDenied(call ToolCall) string {
	return "Permission denied. Possible causes:\n" +
		"1. The file or directory is read-only\n" +
		"2. The operation requires elevated privileges\n" +
		"3. The file is owned by another user\n" +
		"Check file permissions with a file info tool if available."
}

func (r *ErrorRecovery) suggestTimeout(call ToolCall) string {
	switch call.Name {
	case "find_entry_points", "trace_data_flow", "trace_error_flow":
		return "The analysis timed out. Try:\n" +
			"1. Narrow the search scope with more specific parameters\n" +
			"2. Reduce max_hops or limit parameters\n" +
			"3. Target a specific package instead of the whole codebase"
	case "find_similar_code":
		return "Similarity search timed out. Try:\n" +
			"1. Reduce the limit parameter\n" +
			"2. Use a higher min_similarity threshold\n" +
			"3. Target a specific symbol rather than searching broadly"
	default:
		return "Operation timed out. Try a smaller scope or simpler operation."
	}
}

func (r *ErrorRecovery) suggestValidation(call ToolCall, errMsg string) string {
	// Extract parameter name if present
	if strings.Contains(errMsg, "required parameter") {
		return "A required parameter is missing. Check the tool definition for required parameters."
	}

	if strings.Contains(errMsg, "expected string") {
		return "A parameter has the wrong type. It should be a string value."
	}

	if strings.Contains(errMsg, "expected integer") || strings.Contains(errMsg, "expected number") {
		return "A parameter has the wrong type. It should be a numeric value."
	}

	if strings.Contains(errMsg, "expected boolean") {
		return "A parameter has the wrong type. It should be true or false."
	}

	if strings.Contains(errMsg, "not in allowed enum") {
		return "The parameter value is not one of the allowed options. Check the tool definition for valid values."
	}

	return "Parameter validation failed. Check parameter types and required fields."
}

func (r *ErrorRecovery) suggestToolNotFound(call ToolCall) string {
	return fmt.Sprintf("Tool '%s' not found. Possible issues:\n"+
		"1. Check spelling - tool names are case-sensitive\n"+
		"2. The tool may be in a different category that's not enabled\n"+
		"3. List available tools to see what's accessible", call.Name)
}

func (r *ErrorRecovery) suggestRequirementNotMet(call ToolCall, errMsg string) string {
	if strings.Contains(errMsg, "graph_initialized") {
		return "This tool requires the code graph to be initialized. " +
			"The graph may not be ready yet or graph initialization may have failed. " +
			"Try using a basic file read tool instead."
	}

	return "A tool requirement is not satisfied. Some tools have prerequisites that must be met first."
}

// extractFilename extracts just the filename from a path.
func extractFilename(path string) string {
	// Handle both Unix and Windows paths
	idx := strings.LastIndexAny(path, "/\\")
	if idx >= 0 {
		return path[idx+1:]
	}
	return path
}

// RecoveryResult contains analysis of an error with suggestions.
type RecoveryResult struct {
	// OriginalError is the error that was analyzed.
	OriginalError string `json:"original_error"`

	// Category is the type of error (e.g., "file_not_found", "timeout").
	Category string `json:"category"`

	// Suggestion is the recovery suggestion.
	Suggestion string `json:"suggestion"`

	// Retryable indicates if the operation might succeed on retry.
	Retryable bool `json:"retryable"`
}

// Analyze performs full error analysis and returns structured results.
//
// Description:
//
//	Provides detailed analysis including error category and retryability.
//
// Inputs:
//
//	err - The error to analyze
//	call - The tool call that failed
//
// Outputs:
//
//	*RecoveryResult - Analysis results
//
// Thread Safety: This method is safe for concurrent use.
func (r *ErrorRecovery) Analyze(err error, call ToolCall) *RecoveryResult {
	if err == nil {
		return nil
	}

	result := &RecoveryResult{
		OriginalError: err.Error(),
		Suggestion:    r.SuggestFix(err, call),
	}

	errMsg := err.Error()

	// Categorize the error
	// Note: Check specific errors before string matching to avoid false positives
	switch {
	case errors.Is(err, ErrToolNotFound):
		result.Category = "tool_not_found"
		result.Retryable = false

	case errors.Is(err, ErrValidationFailed):
		result.Category = "validation"
		result.Retryable = false

	case errors.Is(err, os.ErrNotExist) || strings.Contains(errMsg, "file not found") ||
		strings.Contains(errMsg, "does not exist") || strings.Contains(errMsg, "no such file"):
		result.Category = "file_not_found"
		result.Retryable = false

	case errors.Is(err, os.ErrPermission) || strings.Contains(errMsg, "permission"):
		result.Category = "permission_denied"
		result.Retryable = false

	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(errMsg, "timeout"):
		result.Category = "timeout"
		result.Retryable = true

	case errors.Is(err, context.Canceled):
		result.Category = "cancelled"
		result.Retryable = true

	case strings.Contains(errMsg, "network") || strings.Contains(errMsg, "connection"):
		result.Category = "network"
		result.Retryable = true

	default:
		result.Category = "unknown"
		result.Retryable = false
	}

	return result
}
