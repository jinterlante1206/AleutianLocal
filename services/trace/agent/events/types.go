// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package events provides event types and handling for the agent loop.
//
// Events allow external systems to observe agent behavior, collect metrics,
// and implement logging without coupling to the agent implementation.
//
// Thread Safety:
//
//	All types in this package are designed for concurrent use.
package events

import (
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
)

// Type identifies the kind of event.
type Type string

const (
	// TypeStateTransition is emitted when the agent changes state.
	TypeStateTransition Type = "state_transition"

	// TypeToolInvocation is emitted when a tool is about to be invoked.
	TypeToolInvocation Type = "tool_invocation"

	// TypeToolResult is emitted when a tool returns a result.
	TypeToolResult Type = "tool_result"

	// TypeContextUpdate is emitted when context is modified.
	TypeContextUpdate Type = "context_update"

	// TypeLLMRequest is emitted when sending a request to the LLM.
	TypeLLMRequest Type = "llm_request"

	// TypeLLMResponse is emitted when receiving a response from the LLM.
	TypeLLMResponse Type = "llm_response"

	// TypeSafetyCheck is emitted when a safety check is performed.
	TypeSafetyCheck Type = "safety_check"

	// TypeReflection is emitted during reflection phase.
	TypeReflection Type = "reflection"

	// TypeError is emitted when an error occurs.
	TypeError Type = "error"

	// TypeSessionStart is emitted when a session begins.
	TypeSessionStart Type = "session_start"

	// TypeSessionEnd is emitted when a session ends.
	TypeSessionEnd Type = "session_end"

	// TypeStepComplete is emitted when a step is completed.
	TypeStepComplete Type = "step_complete"

	// TypeToolForcing is emitted when tool usage is being forced for an analytical query.
	TypeToolForcing Type = "tool_forcing"
)

// Event represents an agent event.
//
// Description:
//
//	Events are the primary mechanism for observing agent behavior.
//	Each event has a type that determines the structure of its Data field.
//	Use the appropriate typed data struct (StateTransitionData, ToolResultData, etc.)
//	when setting the Data field.
//
// Thread Safety:
//
//	Event structs should be treated as immutable after creation.
type Event struct {
	// ID is a unique identifier for this event.
	ID string `json:"id"`

	// Type identifies the kind of event.
	Type Type `json:"type"`

	// SessionID links the event to a session.
	SessionID string `json:"session_id"`

	// Timestamp is when the event occurred (Unix milliseconds UTC).
	Timestamp int64 `json:"timestamp"`

	// Step is the agent step number when this event occurred.
	Step int `json:"step"`

	// Data contains event-specific data. Should be one of the typed
	// data structs: StateTransitionData, ToolInvocationData, ToolResultData,
	// ContextUpdateData, LLMRequestData, LLMResponseData, SafetyCheckData,
	// ReflectionData, ErrorData, SessionStartData, SessionEndData, or StepCompleteData.
	Data any `json:"data,omitempty"`

	// Metadata contains typed additional context for the event.
	Metadata *EventMetadata `json:"metadata,omitempty"`
}

// StateTransitionData is the data for state transition events.
type StateTransitionData struct {
	// FromState is the previous state.
	FromState agent.AgentState `json:"from_state"`

	// ToState is the new state.
	ToState agent.AgentState `json:"to_state"`

	// Reason explains why the transition occurred.
	Reason string `json:"reason,omitempty"`
}

// ToolInvocationParameters contains typed parameters for tool invocation events.
type ToolInvocationParameters struct {
	// StringParams contains string parameter values.
	StringParams map[string]string `json:"string_params,omitempty"`

	// IntParams contains integer parameter values.
	IntParams map[string]int `json:"int_params,omitempty"`

	// BoolParams contains boolean parameter values.
	BoolParams map[string]bool `json:"bool_params,omitempty"`

	// RawJSON contains the original JSON if parsing into typed maps failed.
	RawJSON []byte `json:"raw_json,omitempty"`
}

// ToolInvocationData is the data for tool invocation events.
type ToolInvocationData struct {
	// ToolName is the name of the tool being invoked.
	ToolName string `json:"tool_name"`

	// InvocationID uniquely identifies this invocation.
	InvocationID string `json:"invocation_id"`

	// Parameters are the typed tool parameters.
	Parameters *ToolInvocationParameters `json:"parameters,omitempty"`
}

// ToolResultData is the data for tool result events.
type ToolResultData struct {
	// ToolName is the name of the tool.
	ToolName string `json:"tool_name"`

	// InvocationID links to the invocation.
	InvocationID string `json:"invocation_id"`

	// Success indicates if the tool succeeded.
	Success bool `json:"success"`

	// Duration is how long the tool took.
	Duration time.Duration `json:"duration"`

	// TokensUsed is the output token count.
	TokensUsed int `json:"tokens_used,omitempty"`

	// Cached indicates if the result was cached.
	Cached bool `json:"cached,omitempty"`

	// Error is set if the tool failed.
	Error string `json:"error,omitempty"`
}

// ContextUpdateData is the data for context update events.
type ContextUpdateData struct {
	// Action is what happened (e.g., "add", "evict", "update").
	Action string `json:"action"`

	// EntriesAffected is the number of entries affected.
	EntriesAffected int `json:"entries_affected"`

	// TokensBefore is the token count before the update.
	TokensBefore int `json:"tokens_before"`

	// TokensAfter is the token count after the update.
	TokensAfter int `json:"tokens_after"`
}

// LLMRequestData is the data for LLM request events.
type LLMRequestData struct {
	// Model is the model being used.
	Model string `json:"model"`

	// TokensIn is the input token count.
	TokensIn int `json:"tokens_in"`

	// HasTools indicates if tools were included.
	HasTools bool `json:"has_tools"`

	// ToolCount is the number of tools available.
	ToolCount int `json:"tool_count,omitempty"`

	// === Prompt Hierarchy (CRS enhancement for debugging) ===

	// SystemPromptLen is the length of the system prompt in characters.
	// Full system prompt is not included to avoid bloating logs.
	SystemPromptLen int `json:"system_prompt_len,omitempty"`

	// MessageCount is the number of messages in the conversation history.
	MessageCount int `json:"message_count,omitempty"`

	// MessageSummary shows role distribution: "user:3,assistant:2,tool:5"
	MessageSummary string `json:"message_summary,omitempty"`

	// LastUserMessage is the last user message (truncated to 500 chars).
	// This is the query being answered - critical for debugging.
	LastUserMessage string `json:"last_user_message,omitempty"`

	// ToolNames lists the available tools (for understanding what agent can do).
	ToolNames []string `json:"tool_names,omitempty"`
}

// LLMResponseData is the data for LLM response events.
type LLMResponseData struct {
	// Model is the model that responded.
	Model string `json:"model"`

	// TokensOut is the output token count.
	TokensOut int `json:"tokens_out"`

	// Duration is how long the request took.
	Duration time.Duration `json:"duration"`

	// StopReason is why generation stopped.
	StopReason string `json:"stop_reason"`

	// HasToolCalls indicates if the response includes tool calls.
	HasToolCalls bool `json:"has_tool_calls"`

	// ToolCallCount is the number of tool calls.
	ToolCallCount int `json:"tool_call_count,omitempty"`

	// === Response Content (CRS enhancement for debugging) ===

	// ContentLen is the length of the text content in characters.
	ContentLen int `json:"content_len,omitempty"`

	// ContentPreview is the first 500 chars of the text content.
	// Critical for debugging empty response issues like CB-31/Test 30.
	ContentPreview string `json:"content_preview,omitempty"`

	// ToolCallsPreview summarizes tool calls: "Grep(query=main),ReadFile(path=main.go)"
	ToolCallsPreview string `json:"tool_calls_preview,omitempty"`
}

// SafetyCheckData is the data for safety check events.
type SafetyCheckData struct {
	// ChangesChecked is the number of changes checked.
	ChangesChecked int `json:"changes_checked"`

	// Passed indicates if the check passed.
	Passed bool `json:"passed"`

	// CriticalCount is the number of critical issues.
	CriticalCount int `json:"critical_count"`

	// WarningCount is the number of warnings.
	WarningCount int `json:"warning_count"`

	// Blocked indicates if execution was blocked.
	Blocked bool `json:"blocked"`
}

// ReflectionData is the data for reflection events.
type ReflectionData struct {
	// StepsCompleted is the number of steps completed so far.
	StepsCompleted int `json:"steps_completed"`

	// TokensUsed is the total tokens used so far.
	TokensUsed int `json:"tokens_used"`

	// Decision is the reflection outcome.
	Decision string `json:"decision"`

	// Reason explains the decision.
	Reason string `json:"reason,omitempty"`
}

// ErrorContext contains typed context for error events.
type ErrorContext struct {
	// ToolName is the tool that caused the error, if applicable.
	ToolName string `json:"tool_name,omitempty"`

	// InvocationID links to the tool invocation that failed.
	InvocationID string `json:"invocation_id,omitempty"`

	// FilePath is the file involved in the error, if applicable.
	FilePath string `json:"file_path,omitempty"`

	// SymbolID is the symbol involved in the error, if applicable.
	SymbolID string `json:"symbol_id,omitempty"`

	// Phase is the agent phase where the error occurred.
	Phase string `json:"phase,omitempty"`

	// StepNumber is the step where the error occurred.
	StepNumber int `json:"step_number,omitempty"`

	// StackTrace is the stack trace, if available.
	StackTrace string `json:"stack_trace,omitempty"`
}

// ErrorData is the data for error events.
type ErrorData struct {
	// Error is the error message.
	Error string `json:"error"`

	// Code is a machine-readable error code.
	Code string `json:"code,omitempty"`

	// Recoverable indicates if the error can be recovered from.
	Recoverable bool `json:"recoverable"`

	// Context provides typed additional error context.
	Context *ErrorContext `json:"context,omitempty"`
}

// SessionStartData is the data for session start events.
type SessionStartData struct {
	// Query is the initial user query.
	Query string `json:"query"`

	// ProjectRoot is the project directory.
	ProjectRoot string `json:"project_root"`

	// GraphID is the code graph being used.
	GraphID string `json:"graph_id,omitempty"`
}

// SessionEndData is the data for session end events.
type SessionEndData struct {
	// FinalState is the state when the session ended.
	FinalState agent.AgentState `json:"final_state"`

	// TotalSteps is the number of steps executed.
	TotalSteps int `json:"total_steps"`

	// TotalDuration is how long the session lasted.
	TotalDuration time.Duration `json:"total_duration"`

	// TotalTokens is the total tokens used.
	TotalTokens int `json:"total_tokens"`

	// Success indicates if the session completed successfully.
	Success bool `json:"success"`

	// Error is set if the session ended with an error.
	Error string `json:"error,omitempty"`
}

// StepCompleteData is the data for step complete events.
type StepCompleteData struct {
	// StepNumber is the step that completed.
	StepNumber int `json:"step_number"`

	// Duration is how long the step took.
	Duration time.Duration `json:"duration"`

	// ToolsInvoked is the number of tools invoked in this step.
	ToolsInvoked int `json:"tools_invoked"`

	// TokensUsed is the tokens used in this step.
	TokensUsed int `json:"tokens_used"`
}

// ToolForcingData is the data for tool forcing events.
type ToolForcingData struct {
	// Query is the user's analytical query.
	Query string `json:"query"`

	// SuggestedTool is the tool being suggested in the forcing hint.
	SuggestedTool string `json:"suggested_tool,omitempty"`

	// RetryCount is the current tool forcing retry attempt.
	RetryCount int `json:"retry_count"`

	// MaxRetries is the maximum number of retries allowed.
	MaxRetries int `json:"max_retries"`

	// StepNumber is the step where forcing occurred.
	StepNumber int `json:"step_number"`

	// Reason explains why tool forcing was triggered.
	Reason string `json:"reason,omitempty"`
}
