// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package agent provides a state-machine-driven agent orchestration system.
//
// The agent loop coordinates exploration tools, safety checks, and LLM
// interactions to answer user queries about codebases. It implements a
// finite state machine with phases: IDLE, INIT, PLAN, EXECUTE, REFLECT,
// CLARIFY, DEGRADED, COMPLETE, and ERROR.
//
// Thread Safety:
//
//	All exported types in this package are designed for concurrent use.
//	Sessions are protected by internal synchronization.
package agent

import "time"

// AgentState represents a state in the agent loop state machine.
//
// Valid state transitions are enforced by the state machine. Invalid
// transitions return ErrInvalidTransition.
type AgentState string

const (
	// StateIdle is the initial state before any query is received.
	StateIdle AgentState = "IDLE"

	// StateInit initializes the Code Buddy graph for the project.
	StateInit AgentState = "INIT"

	// StatePlan assembles initial context and classifies the query.
	StatePlan AgentState = "PLAN"

	// StateExecute is the main tool-use loop.
	StateExecute AgentState = "EXECUTE"

	// StateReflect enables self-correction after hitting step limits.
	StateReflect AgentState = "REFLECT"

	// StateClarify requests additional input from the user.
	StateClarify AgentState = "CLARIFY"

	// StateDegraded operates with limited tools when Code Buddy unavailable.
	StateDegraded AgentState = "DEGRADED"

	// StateComplete indicates successful completion.
	StateComplete AgentState = "COMPLETE"

	// StateError indicates an unrecoverable error occurred.
	StateError AgentState = "ERROR"
)

// String returns the string representation of the state.
func (s AgentState) String() string {
	return string(s)
}

// IsTerminal returns true if the state is a terminal state (COMPLETE or ERROR).
func (s AgentState) IsTerminal() bool {
	return s == StateComplete || s == StateError
}

// IsActive returns true if the state allows continued execution.
func (s AgentState) IsActive() bool {
	switch s {
	case StateInit, StatePlan, StateExecute, StateReflect, StateDegraded:
		return true
	default:
		return false
	}
}

// AllStates returns all valid agent states.
func AllStates() []AgentState {
	return []AgentState{
		StateIdle,
		StateInit,
		StatePlan,
		StateExecute,
		StateReflect,
		StateClarify,
		StateDegraded,
		StateComplete,
		StateError,
	}
}

// HistoryEntry records a single step in the agent's execution history.
type HistoryEntry struct {
	// Step is the 0-indexed step number.
	Step int `json:"step"`

	// Type describes what happened (plan, tool_call, reflection, etc.).
	Type string `json:"type"`

	// State is the agent state at this step.
	State AgentState `json:"state"`

	// Query is the user's query (for plan entries).
	Query string `json:"query,omitempty"`

	// ToolName is the tool that was invoked (for tool_call entries).
	ToolName string `json:"tool_name,omitempty"`

	// Input contains the tool input or prompt.
	Input string `json:"input,omitempty"`

	// Output contains the tool result or LLM response.
	Output string `json:"output,omitempty"`

	// TokensUsed is the tokens consumed in this step.
	TokensUsed int `json:"tokens_used,omitempty"`

	// DurationMs is how long this step took in milliseconds.
	DurationMs int64 `json:"duration_ms"`

	// Timestamp is when this step occurred.
	Timestamp time.Time `json:"timestamp"`

	// Error contains any error message from this step.
	Error string `json:"error,omitempty"`
}

// SessionMetrics tracks metrics for a session.
type SessionMetrics struct {
	// TotalSteps is the number of steps executed.
	TotalSteps int `json:"total_steps"`

	// TotalTokens is the total tokens consumed.
	TotalTokens int `json:"total_tokens"`

	// TotalDurationMs is the total execution time in milliseconds.
	TotalDurationMs int64 `json:"total_duration_ms"`

	// ToolCalls is the number of tool invocations.
	ToolCalls int `json:"tool_calls"`

	// ToolErrors is the number of failed tool calls.
	ToolErrors int `json:"tool_errors"`

	// LLMCalls is the number of LLM API calls.
	LLMCalls int `json:"llm_calls"`

	// CacheHits is the number of cache hits.
	CacheHits int `json:"cache_hits"`

	// DegradedMode indicates if running in degraded mode.
	DegradedMode bool `json:"degraded_mode"`

	// GraphStats contains Code Buddy graph statistics.
	GraphStats *GraphStats `json:"graph_stats,omitempty"`
}

// GraphStats contains statistics about the Code Buddy graph.
type GraphStats struct {
	// FilesParsed is the number of files in the graph.
	FilesParsed int `json:"files_parsed"`

	// SymbolsExtracted is the number of symbols extracted.
	SymbolsExtracted int `json:"symbols_extracted"`

	// EdgesBuilt is the number of edges in the graph.
	EdgesBuilt int `json:"edges_built"`

	// ParseTimeMs is how long graph construction took.
	ParseTimeMs int64 `json:"parse_time_ms"`
}

// RunResult contains the outcome of a Run or Continue call.
type RunResult struct {
	// State is the current agent state after execution.
	State AgentState `json:"state"`

	// Response is the final response text (for COMPLETE state).
	Response string `json:"response,omitempty"`

	// NeedsClarify contains clarification details (for CLARIFY state).
	NeedsClarify *ClarifyRequest `json:"needs_clarify,omitempty"`

	// ToolsUsed lists all tool invocations made.
	ToolsUsed []ToolInvocation `json:"tools_used"`

	// TokensUsed is the total tokens consumed.
	TokensUsed int `json:"tokens_used"`

	// StepsTaken is the number of steps executed.
	StepsTaken int `json:"steps_taken"`

	// SafetyChecks contains any safety analysis results.
	SafetyChecks []SafetyResult `json:"safety_checks,omitempty"`

	// Error contains error details (for ERROR state).
	Error *AgentError `json:"error,omitempty"`
}

// ClarifyRequest contains details when agent needs clarification.
type ClarifyRequest struct {
	// Question is what the agent needs to know.
	Question string `json:"question"`

	// Options are suggested clarification options.
	Options []string `json:"options,omitempty"`

	// Context explains why clarification is needed.
	Context string `json:"context,omitempty"`
}

// ToolInvocation represents a single tool call.
type ToolInvocation struct {
	// ID is a unique identifier for this invocation.
	ID string `json:"id"`

	// Tool is the tool name.
	Tool string `json:"tool"`

	// Parameters are the tool input parameters.
	Parameters map[string]any `json:"parameters"`

	// Reason explains why the agent chose this tool.
	Reason string `json:"reason,omitempty"`

	// StepNumber is the step when this was invoked.
	StepNumber int `json:"step_number"`

	// Result contains the tool execution result.
	Result *ToolResult `json:"result,omitempty"`
}

// ToolResult contains the outcome of a tool execution.
type ToolResult struct {
	// InvocationID links back to the invocation.
	InvocationID string `json:"invocation_id"`

	// Success indicates if the tool succeeded.
	Success bool `json:"success"`

	// Output is the tool's output.
	Output string `json:"output"`

	// Error contains any error message.
	Error string `json:"error,omitempty"`

	// Duration is how long execution took.
	Duration time.Duration `json:"duration"`

	// TokensUsed is the estimated tokens in the output.
	TokensUsed int `json:"tokens_used"`

	// Cached indicates if result came from cache.
	Cached bool `json:"cached"`

	// Truncated indicates if output was truncated.
	Truncated bool `json:"truncated"`
}

// SafetyResult contains security analysis findings.
type SafetyResult struct {
	// Passed indicates if all safety checks passed.
	Passed bool `json:"passed"`

	// Issues contains security issues found.
	Issues []SecurityIssue `json:"issues"`

	// Blocked indicates if the operation was blocked.
	Blocked bool `json:"blocked"`

	// BlockReason explains why the operation was blocked.
	BlockReason string `json:"block_reason,omitempty"`
}

// SecurityIssue represents a single security finding.
type SecurityIssue struct {
	// ID is a unique identifier for this issue type.
	ID string `json:"id"`

	// Title is a short description.
	Title string `json:"title"`

	// Description provides details.
	Description string `json:"description"`

	// Severity is the issue severity (critical, high, medium, low).
	Severity string `json:"severity"`

	// Category is the issue category (injection, secrets, etc.).
	Category string `json:"category"`

	// FilePath is where the issue was found.
	FilePath string `json:"file_path,omitempty"`

	// Line is the line number.
	Line int `json:"line,omitempty"`

	// Code is the problematic code snippet.
	Code string `json:"code,omitempty"`
}

// AgentError contains error information for the ERROR state.
type AgentError struct {
	// Code is a machine-readable error code.
	Code string `json:"code"`

	// Message is a human-readable error message.
	Message string `json:"message"`

	// Details contains additional error context.
	Details string `json:"details,omitempty"`

	// Recoverable indicates if the error might be resolved by retry.
	Recoverable bool `json:"recoverable"`

	// Step is the step where the error occurred.
	Step int `json:"step,omitempty"`
}

// QueryIntent represents the classified intent of a user query.
type QueryIntent struct {
	// Type is the query type (explore, modify, explain, debug, refactor).
	Type string `json:"type"`

	// Scope is the query scope (file, function, package, project).
	Scope string `json:"scope"`

	// Targets are specific symbols/files mentioned.
	Targets []string `json:"targets"`

	// Confidence is how certain we are about the classification (0.0-1.0).
	Confidence float64 `json:"confidence"`
}

// ProposedChange represents a code change the agent wants to make.
type ProposedChange struct {
	// FilePath is the file to modify.
	FilePath string `json:"file_path"`

	// ChangeType is create, modify, or delete.
	ChangeType string `json:"change_type"`

	// OldContent is the original content (for modify).
	OldContent string `json:"old_content,omitempty"`

	// NewContent is the new content.
	NewContent string `json:"new_content"`

	// Reason explains why this change is needed.
	Reason string `json:"reason"`
}

// SessionState contains the externally visible state of a session.
type SessionState struct {
	// ID is the session identifier.
	ID string `json:"id"`

	// ProjectRoot is the project being analyzed.
	ProjectRoot string `json:"project_root"`

	// GraphID is the Code Buddy graph ID.
	GraphID string `json:"graph_id,omitempty"`

	// State is the current agent state.
	State AgentState `json:"state"`

	// StepCount is the number of steps executed.
	StepCount int `json:"step_count"`

	// TokensUsed is the total tokens consumed.
	TokensUsed int `json:"tokens_used"`

	// CreatedAt is when the session started.
	CreatedAt time.Time `json:"created_at"`

	// LastActiveAt is when the session was last active.
	LastActiveAt time.Time `json:"last_active_at"`

	// DegradedMode indicates if running with limited tools.
	DegradedMode bool `json:"degraded_mode"`
}
