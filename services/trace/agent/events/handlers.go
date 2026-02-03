// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package events

import (
	"log/slog"
	"sync"
	"time"
)

// LoggingHandler creates a handler that logs events.
//
// Inputs:
//
//	logger - The slog logger to use.
//	level - The log level for events.
//
// Outputs:
//
//	Handler - A handler function that logs events.
func LoggingHandler(logger *slog.Logger, level slog.Level) Handler {
	return func(event *Event) {
		attrs := []any{
			slog.String("event_id", event.ID),
			slog.String("event_type", string(event.Type)),
			slog.String("session_id", event.SessionID),
			slog.Int("step", event.Step),
			slog.Time("timestamp", event.Timestamp),
		}

		// Add type-specific attributes
		switch data := event.Data.(type) {
		case *StateTransitionData:
			attrs = append(attrs,
				slog.String("from_state", string(data.FromState)),
				slog.String("to_state", string(data.ToState)),
			)
			if data.Reason != "" {
				attrs = append(attrs, slog.String("reason", data.Reason))
			}

		case *ToolInvocationData:
			attrs = append(attrs,
				slog.String("tool_name", data.ToolName),
				slog.String("invocation_id", data.InvocationID),
			)

		case *ToolResultData:
			attrs = append(attrs,
				slog.String("tool_name", data.ToolName),
				slog.String("invocation_id", data.InvocationID),
				slog.Bool("success", data.Success),
				slog.Duration("duration", data.Duration),
			)
			if data.Error != "" {
				attrs = append(attrs, slog.String("error", data.Error))
			}

		case *LLMRequestData:
			attrs = append(attrs,
				slog.String("model", data.Model),
				slog.Int("tokens_in", data.TokensIn),
				slog.Bool("has_tools", data.HasTools),
			)

		case *LLMResponseData:
			attrs = append(attrs,
				slog.String("model", data.Model),
				slog.Int("tokens_out", data.TokensOut),
				slog.Duration("duration", data.Duration),
				slog.String("stop_reason", data.StopReason),
			)

		case *SafetyCheckData:
			attrs = append(attrs,
				slog.Int("changes_checked", data.ChangesChecked),
				slog.Bool("passed", data.Passed),
				slog.Int("critical_count", data.CriticalCount),
				slog.Int("warning_count", data.WarningCount),
				slog.Bool("blocked", data.Blocked),
			)

		case *ErrorData:
			attrs = append(attrs,
				slog.String("error", data.Error),
				slog.Bool("recoverable", data.Recoverable),
			)
			if data.Code != "" {
				attrs = append(attrs, slog.String("code", data.Code))
			}
		}

		logger.Log(nil, level, "agent event", attrs...)
	}
}

// MetricsCollector collects metrics from events.
//
// Thread Safety: MetricsCollector is safe for concurrent use.
type MetricsCollector struct {
	mu sync.RWMutex

	// Counters
	sessionCount         int64
	stepCount            int64
	toolInvocations      int64
	llmRequests          int64
	safetyChecks         int64
	errorCount           int64
	blockedBySefetyCount int64

	// Gauges
	activeSession string

	// Histograms (simple approximation - use proper histogram for production)
	toolDurations    []time.Duration
	llmDurations     []time.Duration
	stepDurations    []time.Duration
	sessionDurations []time.Duration

	// Token tracking
	totalInputTokens  int64
	totalOutputTokens int64
}

// NewMetricsCollector creates a new metrics collector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		toolDurations:    make([]time.Duration, 0),
		llmDurations:     make([]time.Duration, 0),
		stepDurations:    make([]time.Duration, 0),
		sessionDurations: make([]time.Duration, 0),
	}
}

// Handler returns an event handler for the collector.
func (c *MetricsCollector) Handler() Handler {
	return func(event *Event) {
		c.mu.Lock()
		defer c.mu.Unlock()

		switch data := event.Data.(type) {
		case *SessionStartData:
			c.sessionCount++
			c.activeSession = event.SessionID

		case *SessionEndData:
			c.sessionDurations = append(c.sessionDurations, data.TotalDuration)
			c.totalInputTokens += int64(data.TotalTokens)
			if c.activeSession == event.SessionID {
				c.activeSession = ""
			}

		case *StepCompleteData:
			c.stepCount++
			c.stepDurations = append(c.stepDurations, data.Duration)

		case *ToolInvocationData:
			c.toolInvocations++

		case *ToolResultData:
			c.toolDurations = append(c.toolDurations, data.Duration)

		case *LLMRequestData:
			c.llmRequests++
			c.totalInputTokens += int64(data.TokensIn)

		case *LLMResponseData:
			c.llmDurations = append(c.llmDurations, data.Duration)
			c.totalOutputTokens += int64(data.TokensOut)

		case *SafetyCheckData:
			c.safetyChecks++
			if data.Blocked {
				c.blockedBySefetyCount++
			}

		case *ErrorData:
			c.errorCount++
		}
	}
}

// Metrics returns a snapshot of collected metrics.
type Metrics struct {
	SessionCount         int64         `json:"session_count"`
	StepCount            int64         `json:"step_count"`
	ToolInvocations      int64         `json:"tool_invocations"`
	LLMRequests          int64         `json:"llm_requests"`
	SafetyChecks         int64         `json:"safety_checks"`
	ErrorCount           int64         `json:"error_count"`
	BlockedBySefetyCount int64         `json:"blocked_by_safety_count"`
	TotalInputTokens     int64         `json:"total_input_tokens"`
	TotalOutputTokens    int64         `json:"total_output_tokens"`
	AvgToolDuration      time.Duration `json:"avg_tool_duration"`
	AvgLLMDuration       time.Duration `json:"avg_llm_duration"`
	AvgStepDuration      time.Duration `json:"avg_step_duration"`
	AvgSessionDuration   time.Duration `json:"avg_session_duration"`
	ActiveSession        string        `json:"active_session,omitempty"`
}

// GetMetrics returns a snapshot of collected metrics.
func (c *MetricsCollector) GetMetrics() Metrics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return Metrics{
		SessionCount:         c.sessionCount,
		StepCount:            c.stepCount,
		ToolInvocations:      c.toolInvocations,
		LLMRequests:          c.llmRequests,
		SafetyChecks:         c.safetyChecks,
		ErrorCount:           c.errorCount,
		BlockedBySefetyCount: c.blockedBySefetyCount,
		TotalInputTokens:     c.totalInputTokens,
		TotalOutputTokens:    c.totalOutputTokens,
		AvgToolDuration:      c.avgDuration(c.toolDurations),
		AvgLLMDuration:       c.avgDuration(c.llmDurations),
		AvgStepDuration:      c.avgDuration(c.stepDurations),
		AvgSessionDuration:   c.avgDuration(c.sessionDurations),
		ActiveSession:        c.activeSession,
	}
}

// avgDuration calculates the average duration. Caller must hold lock.
func (c *MetricsCollector) avgDuration(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}

	var total time.Duration
	for _, d := range durations {
		total += d
	}
	return total / time.Duration(len(durations))
}

// Reset clears all collected metrics.
func (c *MetricsCollector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.sessionCount = 0
	c.stepCount = 0
	c.toolInvocations = 0
	c.llmRequests = 0
	c.safetyChecks = 0
	c.errorCount = 0
	c.blockedBySefetyCount = 0
	c.totalInputTokens = 0
	c.totalOutputTokens = 0
	c.toolDurations = make([]time.Duration, 0)
	c.llmDurations = make([]time.Duration, 0)
	c.stepDurations = make([]time.Duration, 0)
	c.sessionDurations = make([]time.Duration, 0)
	c.activeSession = ""
}

// ChannelHandler creates a handler that sends events to a channel.
//
// Inputs:
//
//	ch - The channel to send events to.
//	dropOnFull - If true, drops events when channel is full; if false, blocks.
//
// Outputs:
//
//	Handler - A handler function that sends events to the channel.
func ChannelHandler(ch chan<- Event, dropOnFull bool) Handler {
	return func(event *Event) {
		if dropOnFull {
			select {
			case ch <- *event:
			default:
				// Channel full, drop event
			}
		} else {
			ch <- *event
		}
	}
}

// MultiHandler creates a handler that calls multiple handlers.
//
// Inputs:
//
//	handlers - The handlers to call.
//
// Outputs:
//
//	Handler - A handler that calls all provided handlers.
func MultiHandler(handlers ...Handler) Handler {
	return func(event *Event) {
		for _, h := range handlers {
			h(event)
		}
	}
}

// FilteredHandler creates a handler that only processes events matching a filter.
//
// Inputs:
//
//	handler - The underlying handler.
//	filter - The filter function.
//
// Outputs:
//
//	Handler - A handler that filters events before processing.
func FilteredHandler(handler Handler, filter Filter) Handler {
	return func(event *Event) {
		if filter(event) {
			handler(event)
		}
	}
}

// TypeFilter creates a filter that matches specific event types.
func TypeFilter(types ...Type) Filter {
	typeSet := make(map[Type]bool, len(types))
	for _, t := range types {
		typeSet[t] = true
	}

	return func(event *Event) bool {
		return typeSet[event.Type]
	}
}

// SessionFilter creates a filter that matches a specific session.
func SessionFilter(sessionID string) Filter {
	return func(event *Event) bool {
		return event.SessionID == sessionID
	}
}

// ErrorFilter creates a filter that only passes error events.
func ErrorFilter() Filter {
	return TypeFilter(TypeError)
}

// PerformanceFilter creates a filter for performance-related events.
func PerformanceFilter() Filter {
	return TypeFilter(TypeToolResult, TypeLLMResponse, TypeStepComplete, TypeSessionEnd)
}
