// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package integration

import (
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
)

// =============================================================================
// Agent Events (CRS-06)
// =============================================================================

// AgentEvent represents an event in the agent execution lifecycle.
//
// Description:
//
//	Events are emitted by the agent execution loop and trigger
//	coordinated activity execution. Each event maps to a set of
//	activities that should run in response.
type AgentEvent string

const (
	// EventSessionStart is emitted when a new session begins.
	// Triggers: Memory (initialize history), Streaming (initialize sketches)
	EventSessionStart AgentEvent = "session_start"

	// EventQueryReceived is emitted when a user query is received.
	// Triggers: Similarity (find similar queries), Planning (decompose task), Search (select first tool)
	EventQueryReceived AgentEvent = "query_received"

	// EventToolSelected is emitted after a tool is selected for execution.
	// Triggers: Constraint (check constraints), Awareness (check for cycles)
	EventToolSelected AgentEvent = "tool_selected"

	// EventToolExecuted is emitted after a tool executes successfully.
	// Triggers: Memory (record step), Streaming (update statistics), Search (select next tool)
	EventToolExecuted AgentEvent = "tool_executed"

	// EventToolFailed is emitted when a tool execution fails.
	// Triggers: Learning (learn from failure), Constraint (propagate constraints), Search (backtrack)
	EventToolFailed AgentEvent = "tool_failed"

	// EventCycleDetected is emitted when a cycle is detected in tool usage.
	// Triggers: Learning (learn cycle avoidance), Awareness (mark cycle disproven)
	EventCycleDetected AgentEvent = "cycle_detected"

	// EventCircuitBreaker is emitted when circuit breaker trips.
	// Triggers: Learning (learn from repeated failures), Constraint (add blocking constraint)
	EventCircuitBreaker AgentEvent = "circuit_breaker"

	// EventSynthesisStart is emitted when answer synthesis begins.
	// Triggers: Similarity (find similar syntheses), Memory (get relevant history)
	EventSynthesisStart AgentEvent = "synthesis_start"

	// EventSessionEnd is emitted when a session ends.
	// Triggers: Streaming (finalize statistics), Memory (persist learned clauses)
	EventSessionEnd AgentEvent = "session_end"
)

// EventData provides context for event handling.
//
// Description:
//
//	Contains the data associated with an agent event. Different fields
//	are populated depending on the event type.
type EventData struct {
	// SessionID is the session identifier.
	SessionID string

	// StepNumber is the current step number (for step-related events).
	StepNumber int

	// Tool is the tool name (for tool-related events).
	Tool string

	// Query is the user query (for query events).
	Query string

	// Error is the error message (for failure events).
	Error string

	// ErrorCategory categorizes the error for learning.
	ErrorCategory crs.ErrorCategory

	// ProofStatus contains proof index state if available.
	ProofStatus map[string]crs.ProofNumber

	// Assignment contains the current variable assignment.
	Assignment map[string]bool

	// Metadata contains additional event-specific data.
	Metadata map[string]any
}

// =============================================================================
// Event-to-Activity Mapping
// =============================================================================

// ActivityName identifies an activity type.
type ActivityName string

const (
	ActivitySearch     ActivityName = "search"
	ActivityLearning   ActivityName = "learning"
	ActivityConstraint ActivityName = "constraint"
	ActivityPlanning   ActivityName = "planning"
	ActivityAwareness  ActivityName = "awareness"
	ActivitySimilarity ActivityName = "similarity"
	ActivityStreaming  ActivityName = "streaming"
	ActivityMemory     ActivityName = "memory"
)

// EventActivityMapping defines the default mapping from events to activities.
//
// Description:
//
//	This mapping determines which activities run in response to each event.
//	Activities are listed in priority order (first activity runs first).
//	The coordinator may skip activities not registered or disabled.
var EventActivityMapping = map[AgentEvent][]ActivityName{
	EventSessionStart: {
		ActivityMemory,    // Initialize history
		ActivityStreaming, // Initialize sketches
	},
	EventQueryReceived: {
		ActivitySimilarity, // Find similar past queries
		ActivityPlanning,   // Decompose task (HTN)
		ActivitySearch,     // Select first tool
	},
	EventToolSelected: {
		ActivityConstraint, // Check constraints (AC-3)
		ActivityAwareness,  // Check for cycles (Tarjan)
	},
	EventToolExecuted: {
		ActivityMemory,    // Record step
		ActivityStreaming, // Update statistics
		ActivitySearch,    // Select next tool
	},
	EventToolFailed: {
		ActivityLearning,   // Learn from failure (CDCL)
		ActivityConstraint, // Propagate constraints
		ActivitySearch,     // Backtrack and reselect
	},
	EventCycleDetected: {
		ActivityLearning,  // Learn cycle avoidance
		ActivityAwareness, // Mark cycle disproven
	},
	EventCircuitBreaker: {
		ActivityLearning,   // Learn from repeated calls
		ActivityConstraint, // Add blocking constraint
	},
	EventSynthesisStart: {
		ActivitySimilarity, // Find similar successful syntheses
		ActivityMemory,     // Get relevant history
	},
	EventSessionEnd: {
		ActivityStreaming, // Finalize statistics
		ActivityMemory,    // Persist learned clauses
	},
}

// =============================================================================
// Activity Configuration
// =============================================================================

// ActivityConfig configures an individual activity for event handling.
//
// Description:
//
//	Per-activity configuration for priority, timeout, and dependency management.
type ActivityConfig struct {
	// Priority determines execution order (higher = runs first).
	// Default priorities are based on activity type.
	Priority int

	// Enabled allows disabling activities dynamically.
	Enabled bool

	// Optional marks activities that can fail without blocking.
	// Optional activities don't cause HandleEvent to return an error.
	Optional bool

	// DependsOn lists activities that must complete before this one.
	// This is for sequential dependency, not just priority ordering.
	DependsOn []ActivityName
}

// DefaultActivityConfigs returns the default configuration for all activities.
//
// Description:
//
//	Provides sensible defaults for activity priorities and dependencies.
//	Constraint and Awareness run first (safety checks), then Learning
//	and Search (using learned clauses), then Planning and Similarity
//	(optimization), finally Streaming and Memory (recording).
func DefaultActivityConfigs() map[ActivityName]*ActivityConfig {
	return map[ActivityName]*ActivityConfig{
		ActivityConstraint: {
			Priority:  100, // Check constraints first
			Enabled:   true,
			Optional:  false,
			DependsOn: nil,
		},
		ActivityAwareness: {
			Priority:  90, // Detect cycles early
			Enabled:   true,
			Optional:  true, // Analysis can fail without blocking
			DependsOn: nil,
		},
		ActivityLearning: {
			Priority:  80, // Learn before search
			Enabled:   true,
			Optional:  true,
			DependsOn: []ActivityName{ActivityAwareness},
		},
		ActivitySearch: {
			Priority:  70, // Search uses learned clauses
			Enabled:   true,
			Optional:  false,
			DependsOn: []ActivityName{ActivityLearning, ActivityConstraint},
		},
		ActivityPlanning: {
			Priority:  60,
			Enabled:   true,
			Optional:  true,
			DependsOn: []ActivityName{ActivitySearch},
		},
		ActivitySimilarity: {
			Priority:  50, // Optional optimization
			Enabled:   true,
			Optional:  true,
			DependsOn: nil,
		},
		ActivityStreaming: {
			Priority:  40, // Background statistics
			Enabled:   true,
			Optional:  true,
			DependsOn: nil,
		},
		ActivityMemory: {
			Priority:  30, // Recording is last
			Enabled:   true,
			Optional:  false, // Recording must succeed
			DependsOn: nil,
		},
	}
}

// =============================================================================
// Event Context
// =============================================================================

// EventContext provides context for dynamic activity filtering.
//
// Description:
//
//	Used by ActivityFilters to make runtime decisions about which
//	activities to run based on current session state.
type EventContext struct {
	// SessionID is the session identifier.
	SessionID string

	// StepCount is the number of steps taken so far.
	StepCount int

	// QueryType classifies the query (analytical, modification, etc.).
	QueryType string

	// ErrorRate is the recent error rate (0.0-1.0).
	ErrorRate float64

	// IsFirstStep indicates if this is the first step.
	IsFirstStep bool

	// IsSimpleQuery indicates if the query is simple.
	IsSimpleQuery bool

	// ProofStatus contains proof numbers for key variables.
	ProofStatus map[string]crs.ProofNumber
}

// ActivityFilter can dynamically enable/disable activities based on context.
//
// Description:
//
//	Filters are applied to the activity list before execution, allowing
//	runtime decisions about which activities to run.
type ActivityFilter interface {
	// Filter modifies the activity list based on context.
	//
	// Inputs:
	//   - event: The triggering event.
	//   - activities: The activities to filter.
	//   - ctx: The event context.
	//
	// Outputs:
	//   - []ActivityName: The filtered activities.
	Filter(event AgentEvent, activities []ActivityName, ctx *EventContext) []ActivityName
}

// SimpleQueryFilter skips expensive activities for simple queries.
//
// Description:
//
//	When IsSimpleQuery is true, skips Similarity and Planning activities
//	to reduce latency for straightforward requests.
type SimpleQueryFilter struct{}

// Filter implements ActivityFilter.
func (f *SimpleQueryFilter) Filter(event AgentEvent, acts []ActivityName, ctx *EventContext) []ActivityName {
	if ctx == nil || !ctx.IsSimpleQuery {
		return acts
	}

	// Skip expensive activities for simple queries
	expensive := map[ActivityName]bool{
		ActivitySimilarity: true,
		ActivityPlanning:   true,
	}

	result := make([]ActivityName, 0, len(acts))
	for _, a := range acts {
		if !expensive[a] {
			result = append(result, a)
		}
	}
	return result
}

// HighErrorRateFilter enables learning when error rate is high.
//
// Description:
//
//	When error rate exceeds threshold, ensures Learning activity runs
//	even if not normally scheduled for this event.
type HighErrorRateFilter struct {
	Threshold float64
}

// Filter implements ActivityFilter.
func (f *HighErrorRateFilter) Filter(event AgentEvent, acts []ActivityName, ctx *EventContext) []ActivityName {
	if ctx == nil || ctx.ErrorRate < f.Threshold {
		return acts
	}

	// High error rate - ensure learning activity is included
	hasLearning := false
	for _, a := range acts {
		if a == ActivityLearning {
			hasLearning = true
			break
		}
	}

	if !hasLearning {
		// Prepend learning for high priority
		acts = append([]ActivityName{ActivityLearning}, acts...)
	}
	return acts
}
