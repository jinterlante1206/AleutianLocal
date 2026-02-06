// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package activities

import (
	"context"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/algorithms"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/algorithms/search"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/eval"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// -----------------------------------------------------------------------------
// Learning Activity
// -----------------------------------------------------------------------------

// LearningActivity orchestrates learning algorithms: CDCL, Watched.
//
// Description:
//
//	LearningActivity coordinates learning from failures using:
//	- CDCL: Conflict-Driven Clause Learning for extracting conflict clauses
//	- Watched: Watched literals for efficient unit propagation
//
//	IMPORTANT: CDCL clauses are ONLY learned from hard signals (compiler errors,
//	test failures). Soft signals from LLM cannot be used to learn conflict clauses.
//
// Thread Safety: Safe for concurrent use.
type LearningActivity struct {
	*BaseActivity
	config *LearningConfig

	// Algorithms
	cdcl    *search.CDCL
	watched *search.WatchedLiterals
}

// LearningConfig configures the learning activity.
type LearningConfig struct {
	*ActivityConfig

	// CDCLConfig configures the CDCL algorithm.
	CDCLConfig *search.CDCLConfig

	// WatchedConfig configures the watched literals algorithm.
	WatchedConfig *search.WatchedConfig
}

// DefaultLearningConfig returns the default learning configuration.
func DefaultLearningConfig() *LearningConfig {
	return &LearningConfig{
		ActivityConfig: DefaultActivityConfig(),
		CDCLConfig:     search.DefaultCDCLConfig(),
		WatchedConfig:  search.DefaultWatchedConfig(),
	}
}

// NewLearningActivity creates a new learning activity.
//
// Inputs:
//   - config: Learning configuration. Uses defaults if nil.
//
// Outputs:
//   - *LearningActivity: The new activity.
func NewLearningActivity(config *LearningConfig) *LearningActivity {
	if config == nil {
		config = DefaultLearningConfig()
	}

	cdcl := search.NewCDCL(config.CDCLConfig)
	watched := search.NewWatchedLiterals(config.WatchedConfig)

	return &LearningActivity{
		BaseActivity: NewBaseActivity(
			"learning",
			config.Timeout,
			cdcl,
			watched,
		),
		config:  config,
		cdcl:    cdcl,
		watched: watched,
	}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// LearningInput is the input for the learning activity.
type LearningInput struct {
	BaseInput

	// ConflictNodeID is the node where conflict was detected.
	ConflictNodeID string

	// ConflictingAssignments are the assignments that led to conflict.
	ConflictingAssignments []string

	// ErrorMessage is the error that caused the conflict.
	ErrorMessage string

	// ErrorType classifies the error (compile, test, lint, etc.).
	ErrorType string
}

// Type returns the input type name.
func (i *LearningInput) Type() string {
	return "learning"
}

// NewLearningInput creates a new learning input.
//
// IMPORTANT: Learning input MUST have a hard signal source. Soft signals
// cannot be used to learn conflict clauses.
func NewLearningInput(requestID, conflictNodeID string, source crs.SignalSource) *LearningInput {
	return &LearningInput{
		BaseInput:      NewBaseInput(requestID, source),
		ConflictNodeID: conflictNodeID,
	}
}

// SetErrorInfo sets the error information for learning.
//
// Description:
//
//	Adds error details that can be used by CDCL to generate more specific
//	conflict clauses. The error category helps classify the type of failure.
//
// Inputs:
//
//	errorMsg - The error message.
//	category - The error category for classification.
//
// Thread Safety: Not safe for concurrent use - call before passing to activity.
func (i *LearningInput) SetErrorInfo(errorMsg string, category crs.ErrorCategory) {
	i.ErrorMessage = errorMsg
	i.ErrorType = string(category)
}

// -----------------------------------------------------------------------------
// Activity Implementation
// -----------------------------------------------------------------------------

// Execute runs the learning algorithms.
//
// Description:
//
//	Runs CDCL and Watched in parallel to learn from conflicts.
//	CRITICAL: Only hard signals can trigger clause learning. If the input
//	has a soft signal source, clause learning is skipped.
//
// Thread Safety: Safe for concurrent calls.
func (a *LearningActivity) Execute(
	ctx context.Context,
	snapshot crs.Snapshot,
	input ActivityInput,
) (ActivityResult, crs.Delta, error) {
	// Create OTel span for activity execution
	ctx, span := otel.Tracer("activities").Start(ctx, "activities.LearningActivity.Execute",
		trace.WithAttributes(
			attribute.String("activity", a.Name()),
		),
	)
	defer span.End()

	learningInput, ok := input.(*LearningInput)
	if !ok {
		span.RecordError(ErrNilInput)
		return ActivityResult{}, nil, &ActivityError{
			Activity:  a.Name(),
			Operation: "Execute",
			Err:       ErrNilInput,
		}
	}

	span.SetAttributes(
		attribute.String("conflict_node", learningInput.ConflictNodeID),
		attribute.Bool("is_hard_signal", learningInput.Source().IsHard()),
	)

	// CRITICAL: Only learn from hard signals
	if !learningInput.Source().IsHard() {
		span.AddEvent("skipping_soft_signal")
		// Skip clause learning for soft signals, but still update watched literals
		return a.executeWatchedOnly(ctx, snapshot, learningInput)
	}

	// Create algorithm-specific inputs
	makeInput := func(algo algorithms.Algorithm) any {
		switch algo.Name() {
		case "cdcl":
			return &search.CDCLInput{
				Conflict: search.CDCLConflict{
					NodeID:           learningInput.ConflictNodeID,
					Source:           learningInput.Source(),
					ConflictingNodes: learningInput.ConflictingAssignments,
					Reason:           learningInput.ErrorMessage,
				},
			}
		case "watched_literals":
			return &search.WatchedInput{}
		default:
			return nil
		}
	}

	return a.RunAlgorithms(ctx, snapshot, makeInput)
}

// executeWatchedOnly runs only the watched literals algorithm.
func (a *LearningActivity) executeWatchedOnly(
	ctx context.Context,
	snapshot crs.Snapshot,
	input *LearningInput,
) (ActivityResult, crs.Delta, error) {
	startTime := time.Now()

	result := ActivityResult{
		ActivityName: a.Name(),
		StartTime:    startTime,
		Metrics:      make(map[string]float64),
	}

	// Only run watched literals
	watchedInput := &search.WatchedInput{}

	ctx, cancel := context.WithTimeout(ctx, a.Timeout())
	defer cancel()

	delta, algoResults, err := algorithms.RunParallel(ctx, snapshot,
		algorithms.NewExecution(a.watched, watchedInput),
	)

	result.AlgorithmResults = algoResults
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(startTime)

	if err != nil {
		return result, nil, &ActivityError{
			Activity:  a.Name(),
			Operation: "executeWatchedOnly",
			Err:       err,
		}
	}

	result.Success = result.SuccessCount() > 0
	result.Metrics["soft_signal_skip"] = 1

	return result, delta, nil
}

// ShouldRun decides if learning should run.
//
// Description:
//
//	Learning should run when there are conflicts to analyze,
//	indicated by disproven nodes from hard signals.
func (a *LearningActivity) ShouldRun(snapshot crs.Snapshot) (bool, Priority) {
	proofIndex := snapshot.ProofIndex()

	// Check for disproven nodes from hard signals
	for _, proof := range proofIndex.All() {
		if proof.Status == crs.ProofStatusDisproven && proof.Source.IsHard() {
			return true, PriorityHigh
		}
	}

	return false, PriorityLow
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (a *LearningActivity) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "hard_signal_required",
			Description: "CDCL only runs on hard signals",
			Check: func(input, output any) error {
				learningInput, ok := input.(*LearningInput)
				if !ok {
					return nil
				}
				result, ok := output.(ActivityResult)
				if !ok {
					return nil
				}

				// If soft signal, CDCL should not have run
				if !learningInput.Source().IsHard() {
					for _, ar := range result.AlgorithmResults {
						if ar.Name == "cdcl" && ar.Success() {
							return &ActivityError{
								Activity:  a.Name(),
								Operation: "Property.hard_signal_required",
								Err:       eval.ErrPropertyFailed,
							}
						}
					}
				}
				return nil
			},
		},
	}
}

// Metrics returns the metrics this activity exposes.
func (a *LearningActivity) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "learning_activity_executions_total",
			Type:        eval.MetricCounter,
			Description: "Total learning activity executions",
		},
		{
			Name:        "learning_clauses_learned_total",
			Type:        eval.MetricCounter,
			Description: "Total conflict clauses learned",
		},
		{
			Name:        "learning_soft_signal_skips_total",
			Type:        eval.MetricCounter,
			Description: "Times CDCL was skipped due to soft signal",
		},
	}
}

// HealthCheck verifies the activity is functioning.
func (a *LearningActivity) HealthCheck(ctx context.Context) error {
	if a.config == nil {
		return &ActivityError{
			Activity:  a.Name(),
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}

	for _, algo := range a.Algorithms() {
		if err := algo.HealthCheck(ctx); err != nil {
			return &ActivityError{
				Activity:  a.Name(),
				Operation: "HealthCheck",
				Err:       err,
			}
		}
	}

	return nil
}
