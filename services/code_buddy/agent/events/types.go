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

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent"
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
)

// Event represents an agent event.
type Event struct {
	// ID is a unique identifier for this event.
	ID string `json:"id"`

	// Type identifies the kind of event.
	Type Type `json:"type"`

	// SessionID links the event to a session.
	SessionID string `json:"session_id"`

	// Timestamp is when the event occurred.
	Timestamp time.Time `json:"timestamp"`

	// Step is the agent step number when this event occurred.
	Step int `json:"step"`

	// Data contains event-specific data.
	Data any `json:"data,omitempty"`

	// Metadata contains additional context.
	Metadata map[string]any `json:"metadata,omitempty"`
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

// ToolInvocationData is the data for tool invocation events.
type ToolInvocationData struct {
	// ToolName is the name of the tool being invoked.
	ToolName string `json:"tool_name"`

	// InvocationID uniquely identifies this invocation.
	InvocationID string `json:"invocation_id"`

	// Parameters are the tool parameters.
	Parameters map[string]any `json:"parameters,omitempty"`
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

// ErrorData is the data for error events.
type ErrorData struct {
	// Error is the error message.
	Error string `json:"error"`

	// Code is a machine-readable error code.
	Code string `json:"code,omitempty"`

	// Recoverable indicates if the error can be recovered from.
	Recoverable bool `json:"recoverable"`

	// Context provides additional error context.
	Context map[string]any `json:"context,omitempty"`
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
