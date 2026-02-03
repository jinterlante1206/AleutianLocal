// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package crs

import (
	"sync"
	"time"
)

// -----------------------------------------------------------------------------
// Trace Types
// -----------------------------------------------------------------------------

// TraceStep represents one step in the reasoning process.
//
// Description:
//
//	Captures what action was taken, what was found, and how CRS was updated.
//	Steps are recorded in order and can be exported for audit/debugging.
type TraceStep struct {
	// Step is the 1-indexed step number (assigned by recorder).
	Step int `json:"step"`

	// Timestamp is when this step occurred.
	Timestamp time.Time `json:"timestamp"`

	// Action describes what was done (e.g., "explore", "analyze", "trace_flow").
	Action string `json:"action"`

	// Target is the file or symbol being operated on.
	Target string `json:"target"`

	// Tool is the tool that triggered this action (optional).
	Tool string `json:"tool,omitempty"`

	// Duration is how long this step took.
	Duration time.Duration `json:"duration_ms"`

	// SymbolsFound lists symbols discovered in this step.
	SymbolsFound []string `json:"symbols_found,omitempty"`

	// ProofUpdates lists proof status changes.
	ProofUpdates []ProofUpdate `json:"proof_updates,omitempty"`

	// ConstraintsAdded lists new constraints added.
	ConstraintsAdded []ConstraintUpdate `json:"constraints_added,omitempty"`

	// DependenciesFound lists new dependency edges found.
	DependenciesFound []DependencyEdge `json:"dependencies_found,omitempty"`

	// Error contains any error that occurred.
	Error string `json:"error,omitempty"`

	// Metadata contains additional step context.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ProofUpdate represents a proof status change.
type ProofUpdate struct {
	// NodeID is the node whose proof status changed.
	NodeID string `json:"node_id"`

	// Status is the new status: "proven", "disproven", "expanded", "unknown".
	Status string `json:"status"`

	// Reason explains why the status changed.
	Reason string `json:"reason,omitempty"`

	// Source indicates signal source: "hard", "soft".
	Source string `json:"source,omitempty"`
}

// ConstraintUpdate represents a constraint being added.
type ConstraintUpdate struct {
	// ID is the constraint ID.
	ID string `json:"id"`

	// Type is the constraint type.
	Type string `json:"type"`

	// Nodes are the affected nodes.
	Nodes []string `json:"nodes"`
}

// DependencyEdge represents a dependency relationship.
type DependencyEdge struct {
	// From is the dependent node.
	From string `json:"from"`

	// To is the dependency target.
	To string `json:"to"`
}

// ReasoningTrace is the exportable trace format.
type ReasoningTrace struct {
	// SessionID identifies the session this trace belongs to.
	SessionID string `json:"session_id"`

	// TotalSteps is the number of steps recorded.
	TotalSteps int `json:"total_steps"`

	// Duration is the total time from first to last step.
	Duration string `json:"total_duration"`

	// StartTime is when the first step occurred.
	StartTime time.Time `json:"start_time,omitempty"`

	// EndTime is when the last step occurred.
	EndTime time.Time `json:"end_time,omitempty"`

	// Trace contains all recorded steps.
	Trace []TraceStep `json:"trace"`
}

// -----------------------------------------------------------------------------
// TraceConfig
// -----------------------------------------------------------------------------

// TraceConfig configures trace recording behavior.
type TraceConfig struct {
	// MaxSteps limits trace size to prevent unbounded growth.
	// When exceeded, oldest steps are evicted.
	// Default: 1000.
	MaxSteps int

	// RecordSymbols enables recording of discovered symbols.
	// Default: true.
	RecordSymbols bool

	// RecordMetadata enables recording of step metadata.
	// Default: true.
	RecordMetadata bool
}

// DefaultTraceConfig returns sensible defaults.
func DefaultTraceConfig() TraceConfig {
	return TraceConfig{
		MaxSteps:       1000,
		RecordSymbols:  true,
		RecordMetadata: true,
	}
}

// -----------------------------------------------------------------------------
// TraceRecorder
// -----------------------------------------------------------------------------

// TraceRecorder captures reasoning steps for audit and debugging.
//
// Description:
//
//	Records each reasoning action and its effects on CRS state.
//	Steps are stored in order and can be exported as a complete trace.
//
// Thread Safety: Safe for concurrent use.
type TraceRecorder struct {
	mu          sync.Mutex
	steps       []TraceStep
	config      TraceConfig
	nextStepNum int // Monotonically increasing step counter
}

// NewTraceRecorder creates a new trace recorder.
//
// Inputs:
//
//	config - Configuration for the recorder. Uses defaults if zero-valued.
//
// Outputs:
//
//	*TraceRecorder - The configured recorder.
func NewTraceRecorder(config TraceConfig) *TraceRecorder {
	if config.MaxSteps <= 0 {
		config.MaxSteps = DefaultTraceConfig().MaxSteps
	}
	return &TraceRecorder{
		steps:       make([]TraceStep, 0, min(config.MaxSteps, 100)),
		config:      config,
		nextStepNum: 1, // Step numbers start at 1
	}
}

// RecordStep adds a step to the trace.
//
// Description:
//
//	Called after each reasoning action to capture what was done,
//	what was found, and how CRS was updated. Automatically assigns
//	step numbers and timestamps.
//
// Inputs:
//
//	step - The trace step to record. Step number and timestamp
//	       will be overwritten by the recorder.
//
// Thread Safety: Safe for concurrent use.
func (r *TraceRecorder) RecordStep(step TraceStep) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Evict oldest step if at capacity
	if len(r.steps) >= r.config.MaxSteps {
		r.steps = r.steps[1:]
	}

	// Assign step number using monotonically increasing counter
	step.Step = r.nextStepNum
	r.nextStepNum++

	// Set timestamp if not provided
	if step.Timestamp.IsZero() {
		step.Timestamp = time.Now()
	}

	// Apply config filters
	if !r.config.RecordSymbols {
		step.SymbolsFound = nil
	}
	if !r.config.RecordMetadata {
		step.Metadata = nil
	}

	r.steps = append(r.steps, step)
}

// GetSteps returns a copy of all recorded steps.
//
// Outputs:
//
//	[]TraceStep - Copy of recorded steps in order.
//
// Thread Safety: Safe for concurrent use.
func (r *TraceRecorder) GetSteps() []TraceStep {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]TraceStep, len(r.steps))
	copy(result, r.steps)
	return result
}

// StepCount returns the number of recorded steps.
//
// Thread Safety: Safe for concurrent use.
func (r *TraceRecorder) StepCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.steps)
}

// Clear removes all recorded steps.
//
// Thread Safety: Safe for concurrent use.
func (r *TraceRecorder) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = r.steps[:0]
	r.nextStepNum = 1
}

// Export returns the trace in exportable format.
//
// Description:
//
//	Creates a ReasoningTrace containing all recorded steps,
//	suitable for JSON serialization.
//
// Inputs:
//
//	sessionID - Session identifier for the export.
//
// Outputs:
//
//	ReasoningTrace - The exportable trace.
//
// Thread Safety: Safe for concurrent use.
func (r *TraceRecorder) Export(sessionID string) ReasoningTrace {
	steps := r.GetSteps()

	trace := ReasoningTrace{
		SessionID:  sessionID,
		TotalSteps: len(steps),
		Trace:      steps,
	}

	if len(steps) > 0 {
		trace.StartTime = steps[0].Timestamp
		trace.EndTime = steps[len(steps)-1].Timestamp
		duration := trace.EndTime.Sub(trace.StartTime)
		trace.Duration = duration.String()
	} else {
		trace.Duration = "0s"
	}

	return trace
}

// LastStep returns the most recently recorded step, or nil if empty.
//
// Thread Safety: Safe for concurrent use.
func (r *TraceRecorder) LastStep() *TraceStep {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.steps) == 0 {
		return nil
	}

	// Return a copy
	step := r.steps[len(r.steps)-1]
	return &step
}

// -----------------------------------------------------------------------------
// Builder Pattern for TraceStep
// -----------------------------------------------------------------------------

// TraceStepBuilder helps construct TraceStep instances.
type TraceStepBuilder struct {
	step TraceStep
}

// NewTraceStepBuilder creates a new builder.
func NewTraceStepBuilder() *TraceStepBuilder {
	return &TraceStepBuilder{
		step: TraceStep{
			Timestamp: time.Now(),
		},
	}
}

// WithAction sets the action.
func (b *TraceStepBuilder) WithAction(action string) *TraceStepBuilder {
	b.step.Action = action
	return b
}

// WithTarget sets the target.
func (b *TraceStepBuilder) WithTarget(target string) *TraceStepBuilder {
	b.step.Target = target
	return b
}

// WithTool sets the tool.
func (b *TraceStepBuilder) WithTool(tool string) *TraceStepBuilder {
	b.step.Tool = tool
	return b
}

// WithDuration sets the duration.
func (b *TraceStepBuilder) WithDuration(d time.Duration) *TraceStepBuilder {
	b.step.Duration = d
	return b
}

// WithSymbolsFound sets the symbols found.
func (b *TraceStepBuilder) WithSymbolsFound(symbols []string) *TraceStepBuilder {
	b.step.SymbolsFound = symbols
	return b
}

// WithProofUpdate adds a proof update.
func (b *TraceStepBuilder) WithProofUpdate(nodeID, status, reason, source string) *TraceStepBuilder {
	b.step.ProofUpdates = append(b.step.ProofUpdates, ProofUpdate{
		NodeID: nodeID,
		Status: status,
		Reason: reason,
		Source: source,
	})
	return b
}

// WithConstraint adds a constraint update.
func (b *TraceStepBuilder) WithConstraint(id, constraintType string, nodes []string) *TraceStepBuilder {
	b.step.ConstraintsAdded = append(b.step.ConstraintsAdded, ConstraintUpdate{
		ID:    id,
		Type:  constraintType,
		Nodes: nodes,
	})
	return b
}

// WithDependency adds a dependency edge.
func (b *TraceStepBuilder) WithDependency(from, to string) *TraceStepBuilder {
	b.step.DependenciesFound = append(b.step.DependenciesFound, DependencyEdge{
		From: from,
		To:   to,
	})
	return b
}

// WithError sets the error.
func (b *TraceStepBuilder) WithError(err string) *TraceStepBuilder {
	b.step.Error = err
	return b
}

// WithMetadata adds a metadata key-value pair.
func (b *TraceStepBuilder) WithMetadata(key, value string) *TraceStepBuilder {
	if b.step.Metadata == nil {
		b.step.Metadata = make(map[string]string)
	}
	b.step.Metadata[key] = value
	return b
}

// Build returns the constructed TraceStep.
func (b *TraceStepBuilder) Build() TraceStep {
	return b.step
}

// min returns the smaller of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
