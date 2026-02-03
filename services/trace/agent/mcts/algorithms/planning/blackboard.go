// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package planning

import (
	"context"
	"reflect"
	"sort"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
	"github.com/AleutianAI/AleutianFOSS/services/trace/eval"
)

// -----------------------------------------------------------------------------
// Blackboard Architecture Algorithm
// -----------------------------------------------------------------------------

// Blackboard implements a blackboard-based problem-solving architecture.
//
// Description:
//
//	The blackboard architecture enables multiple knowledge sources (personas)
//	to cooperatively solve problems by contributing to a shared workspace.
//	A scheduler determines which source to activate based on the current state.
//
//	Key Concepts:
//	- Blackboard: Shared workspace with structured data (levels)
//	- Knowledge Source (KS): A specialist that can contribute knowledge
//	- Trigger: Conditions under which a KS can contribute
//	- Action: The contribution a KS makes to the blackboard
//	- Scheduler: Determines which KS to activate
//
//	Use Cases:
//	- Multi-persona code review (Security, Performance, Style experts)
//	- Complex debugging (different analysis strategies)
//	- Incremental refinement of solutions
//
// Thread Safety: Safe for concurrent use.
type Blackboard struct {
	config *BlackboardConfig
}

// BlackboardConfig configures the blackboard algorithm.
type BlackboardConfig struct {
	// MaxIterations limits the number of scheduling cycles.
	MaxIterations int

	// MaxContributions limits total contributions per run.
	MaxContributions int

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// ProgressInterval is how often to report progress.
	ProgressInterval time.Duration
}

// DefaultBlackboardConfig returns the default configuration.
func DefaultBlackboardConfig() *BlackboardConfig {
	return &BlackboardConfig{
		MaxIterations:    50,
		MaxContributions: 100,
		Timeout:          5 * time.Second,
		ProgressInterval: 1 * time.Second,
	}
}

// NewBlackboard creates a new Blackboard algorithm.
func NewBlackboard(config *BlackboardConfig) *Blackboard {
	if config == nil {
		config = DefaultBlackboardConfig()
	}
	return &Blackboard{config: config}
}

// -----------------------------------------------------------------------------
// Input/Output Types
// -----------------------------------------------------------------------------

// BlackboardInput is the input for blackboard processing.
type BlackboardInput struct {
	// InitialData is the starting blackboard content.
	InitialData map[string]BlackboardEntry

	// KnowledgeSources are the available specialists.
	KnowledgeSources []KnowledgeSource

	// GoalConditions define when processing is complete.
	GoalConditions []BlackboardCondition

	// Source indicates where the request originated.
	Source crs.SignalSource
}

// BlackboardEntry represents data on the blackboard.
type BlackboardEntry struct {
	// Level is the abstraction level (e.g., "raw", "analyzed", "solution").
	Level string

	// Key is the entry identifier within the level.
	Key string

	// Value is the entry data.
	Value string

	// Confidence is how confident we are in this entry (0-1).
	Confidence float64

	// Source identifies which knowledge source created this.
	Source string

	// Timestamp is when this entry was created.
	Timestamp time.Time
}

// KnowledgeSource represents a specialist that can contribute.
type KnowledgeSource struct {
	// ID uniquely identifies this knowledge source.
	ID string

	// Name is a human-readable name.
	Name string

	// Triggers are conditions that activate this source.
	Triggers []BlackboardCondition

	// Actions are contributions this source can make.
	Actions []BlackboardAction

	// Priority influences scheduling (higher = prefer).
	Priority int

	// CooldownMS is minimum time between activations (milliseconds).
	CooldownMS int
}

// BlackboardCondition represents a condition on the blackboard state.
type BlackboardCondition struct {
	// Level is the blackboard level to check.
	Level string

	// Key is the entry key to check (empty means any key at level).
	Key string

	// Operator is the comparison operator (exists, not_exists, equals, contains).
	Operator string

	// Value is the value to compare against (for equals, contains).
	Value string

	// MinConfidence is the minimum confidence required.
	MinConfidence float64
}

// BlackboardAction represents a contribution a KS can make.
type BlackboardAction struct {
	// Type is the action type (add, update, remove).
	Type string

	// Level is the target blackboard level.
	Level string

	// Key is the entry key to affect.
	Key string

	// ValueTemplate is a template for the new value.
	// Can reference other blackboard entries with {level.key} syntax.
	ValueTemplate string

	// Confidence is the confidence for this contribution.
	Confidence float64
}

// BlackboardOutput is the output from blackboard processing.
type BlackboardOutput struct {
	// FinalState is the blackboard state after processing.
	FinalState map[string]BlackboardEntry

	// Contributions are all contributions made.
	Contributions []BlackboardContribution

	// GoalReached indicates if goal conditions were met.
	GoalReached bool

	// Iterations is the number of scheduling cycles.
	Iterations int

	// ActivationsPerSource counts activations per KS.
	ActivationsPerSource map[string]int

	// StoppedReason explains why processing stopped.
	StoppedReason string
}

// BlackboardContribution records a single contribution.
type BlackboardContribution struct {
	// SourceID identifies which KS contributed.
	SourceID string

	// Action describes what was done.
	Action BlackboardAction

	// Iteration is when this contribution was made.
	Iteration int

	// Timestamp is when this contribution was made.
	Timestamp time.Time

	// TriggeredBy lists the conditions that triggered this.
	TriggeredBy []string
}

// -----------------------------------------------------------------------------
// Algorithm Interface Implementation
// -----------------------------------------------------------------------------

// Name returns the algorithm name.
func (b *Blackboard) Name() string {
	return "blackboard"
}

// Process performs blackboard-based problem solving.
//
// Description:
//
//	Runs a scheduling loop that activates knowledge sources based on
//	trigger conditions, allowing them to contribute to the shared blackboard.
//
// Thread Safety: Safe for concurrent use.
func (b *Blackboard) Process(ctx context.Context, snapshot crs.Snapshot, input any) (any, crs.Delta, error) {
	in, ok := input.(*BlackboardInput)
	if !ok {
		return nil, nil, &AlgorithmError{
			Algorithm: "blackboard",
			Operation: "Process",
			Err:       ErrInvalidInput,
		}
	}

	// Check for cancellation
	select {
	case <-ctx.Done():
		return &BlackboardOutput{StoppedReason: "cancelled"}, nil, ctx.Err()
	default:
	}

	output := &BlackboardOutput{
		FinalState:           make(map[string]BlackboardEntry),
		Contributions:        make([]BlackboardContribution, 0),
		ActivationsPerSource: make(map[string]int),
	}

	// Initialize blackboard state
	state := make(map[string]BlackboardEntry, len(in.InitialData))
	for k, v := range in.InitialData {
		state[k] = v
	}

	// Track last activation time for cooldowns
	lastActivation := make(map[string]time.Time)

	// Main scheduling loop
	for output.Iterations < b.config.MaxIterations {
		// Check for cancellation
		select {
		case <-ctx.Done():
			output.FinalState = state
			output.StoppedReason = "cancelled"
			return output, nil, ctx.Err()
		default:
		}

		// Check contribution limit
		if len(output.Contributions) >= b.config.MaxContributions {
			output.FinalState = state
			output.StoppedReason = "max contributions reached"
			return output, nil, nil
		}

		// Check goal conditions
		if b.checkConditions(in.GoalConditions, state) {
			output.GoalReached = true
			output.FinalState = state
			output.StoppedReason = "goal reached"
			return output, nil, nil
		}

		output.Iterations++

		// Find triggered knowledge sources
		triggered := b.findTriggeredSources(in.KnowledgeSources, state, lastActivation)
		if len(triggered) == 0 {
			output.FinalState = state
			output.StoppedReason = "no triggered sources"
			return output, nil, nil
		}

		// Sort by priority
		sort.Slice(triggered, func(i, j int) bool {
			return triggered[i].Priority > triggered[j].Priority
		})

		// Activate highest priority source
		ks := triggered[0]
		lastActivation[ks.ID] = time.Now()
		output.ActivationsPerSource[ks.ID]++

		// Execute actions
		triggeredConditions := b.getTriggeredConditionStrings(ks.Triggers, state)
		for _, action := range ks.Actions {
			contribution := b.executeAction(action, ks.ID, state, output.Iterations, triggeredConditions)
			output.Contributions = append(output.Contributions, contribution)

			// Apply action to state
			b.applyAction(action, ks.ID, state)
		}
	}

	output.FinalState = state
	output.StoppedReason = "max iterations reached"

	return output, nil, nil
}

// findTriggeredSources returns knowledge sources whose triggers are satisfied.
func (b *Blackboard) findTriggeredSources(sources []KnowledgeSource, state map[string]BlackboardEntry, lastActivation map[string]time.Time) []KnowledgeSource {
	triggered := make([]KnowledgeSource, 0)
	now := time.Now()

	for _, ks := range sources {
		// Check cooldown
		if lastTime, ok := lastActivation[ks.ID]; ok {
			elapsed := now.Sub(lastTime)
			if elapsed.Milliseconds() < int64(ks.CooldownMS) {
				continue
			}
		}

		// Check triggers
		if b.checkConditions(ks.Triggers, state) {
			triggered = append(triggered, ks)
		}
	}

	return triggered
}

// checkConditions verifies all conditions are met.
func (b *Blackboard) checkConditions(conditions []BlackboardCondition, state map[string]BlackboardEntry) bool {
	if len(conditions) == 0 {
		return false // No conditions means never triggered
	}

	for _, cond := range conditions {
		if !b.checkCondition(cond, state) {
			return false
		}
	}
	return true
}

// checkCondition verifies a single condition.
func (b *Blackboard) checkCondition(cond BlackboardCondition, state map[string]BlackboardEntry) bool {
	// Build the full key (level.key)
	fullKey := cond.Level
	if cond.Key != "" {
		fullKey = cond.Level + "." + cond.Key
	}

	entry, exists := state[fullKey]

	switch cond.Operator {
	case "exists":
		return exists && entry.Confidence >= cond.MinConfidence
	case "not_exists":
		return !exists
	case "equals":
		return exists && entry.Value == cond.Value && entry.Confidence >= cond.MinConfidence
	case "contains":
		return exists && containsSubstring(entry.Value, cond.Value) && entry.Confidence >= cond.MinConfidence
	default:
		// For empty key, check if any entry at level exists
		if cond.Key == "" {
			for k, e := range state {
				if len(k) >= len(cond.Level) && k[:len(cond.Level)] == cond.Level {
					if e.Confidence >= cond.MinConfidence {
						return true
					}
				}
			}
		}
		return false
	}
}

// containsSubstring checks if s contains substr.
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// getTriggeredConditionStrings returns human-readable strings for triggered conditions.
func (b *Blackboard) getTriggeredConditionStrings(conditions []BlackboardCondition, state map[string]BlackboardEntry) []string {
	result := make([]string, 0, len(conditions))
	for _, cond := range conditions {
		if b.checkCondition(cond, state) {
			result = append(result, cond.Level+"."+cond.Key+" "+cond.Operator)
		}
	}
	return result
}

// executeAction creates a contribution record for an action.
func (b *Blackboard) executeAction(action BlackboardAction, sourceID string, state map[string]BlackboardEntry, iteration int, triggeredBy []string) BlackboardContribution {
	return BlackboardContribution{
		SourceID:    sourceID,
		Action:      action,
		Iteration:   iteration,
		Timestamp:   time.Now(),
		TriggeredBy: triggeredBy,
	}
}

// applyAction applies an action to the blackboard state.
func (b *Blackboard) applyAction(action BlackboardAction, sourceID string, state map[string]BlackboardEntry) {
	fullKey := action.Level + "." + action.Key

	switch action.Type {
	case "add", "update":
		state[fullKey] = BlackboardEntry{
			Level:      action.Level,
			Key:        action.Key,
			Value:      action.ValueTemplate, // In real impl, would expand template
			Confidence: action.Confidence,
			Source:     sourceID,
			Timestamp:  time.Now(),
		}
	case "remove":
		delete(state, fullKey)
	}
}

// Timeout returns the maximum execution time.
func (b *Blackboard) Timeout() time.Duration {
	return b.config.Timeout
}

// InputType returns the expected input type.
func (b *Blackboard) InputType() reflect.Type {
	return reflect.TypeOf(&BlackboardInput{})
}

// OutputType returns the output type.
func (b *Blackboard) OutputType() reflect.Type {
	return reflect.TypeOf(&BlackboardOutput{})
}

// ProgressInterval returns how often to report progress.
func (b *Blackboard) ProgressInterval() time.Duration {
	return b.config.ProgressInterval
}

// SupportsPartialResults returns true.
func (b *Blackboard) SupportsPartialResults() bool {
	return true
}

// -----------------------------------------------------------------------------
// Evaluable Implementation
// -----------------------------------------------------------------------------

// Properties returns the correctness properties.
func (b *Blackboard) Properties() []eval.Property {
	return []eval.Property{
		{
			Name:        "contributions_from_valid_sources",
			Description: "All contributions come from registered knowledge sources",
			Check: func(input, output any) error {
				in, okIn := input.(*BlackboardInput)
				out, okOut := output.(*BlackboardOutput)
				if !okIn || !okOut {
					return nil
				}

				// Build set of valid source IDs
				validSources := make(map[string]bool)
				for _, ks := range in.KnowledgeSources {
					validSources[ks.ID] = true
				}

				// Verify all contributions come from valid sources
				for _, contrib := range out.Contributions {
					if !validSources[contrib.SourceID] {
						return &AlgorithmError{
							Algorithm: "blackboard",
							Operation: "Property.contributions_from_valid_sources",
							Err:       eval.ErrPropertyFailed,
						}
					}
				}
				return nil
			},
		},
		{
			Name:        "goal_implies_conditions_met",
			Description: "If goal is reached, all goal conditions are satisfied",
			Check: func(input, output any) error {
				in, okIn := input.(*BlackboardInput)
				out, okOut := output.(*BlackboardOutput)
				if !okIn || !okOut || !out.GoalReached {
					return nil
				}

				// Verify goal conditions are met in final state
				bb := &Blackboard{}
				if !bb.checkConditions(in.GoalConditions, out.FinalState) {
					return &AlgorithmError{
						Algorithm: "blackboard",
						Operation: "Property.goal_implies_conditions_met",
						Err:       eval.ErrPropertyFailed,
					}
				}
				return nil
			},
		},
		{
			Name:        "confidence_in_range",
			Description: "All confidence values are in [0, 1]",
			Check: func(input, output any) error {
				out, ok := output.(*BlackboardOutput)
				if !ok {
					return nil
				}

				for _, entry := range out.FinalState {
					if entry.Confidence < 0 || entry.Confidence > 1 {
						return &AlgorithmError{
							Algorithm: "blackboard",
							Operation: "Property.confidence_in_range",
							Err:       eval.ErrPropertyFailed,
						}
					}
				}
				return nil
			},
		},
	}
}

// Metrics returns the metrics this algorithm exposes.
func (b *Blackboard) Metrics() []eval.MetricDefinition {
	return []eval.MetricDefinition{
		{
			Name:        "blackboard_iterations_total",
			Type:        eval.MetricCounter,
			Description: "Total scheduling iterations",
		},
		{
			Name:        "blackboard_contributions_total",
			Type:        eval.MetricCounter,
			Description: "Total contributions made",
		},
		{
			Name:        "blackboard_goals_reached_total",
			Type:        eval.MetricCounter,
			Description: "Total times goal was reached",
		},
		{
			Name:        "blackboard_source_activations",
			Type:        eval.MetricCounter,
			Description: "Activations per knowledge source",
			Labels:      []string{"source_id"},
		},
	}
}

// HealthCheck verifies the algorithm is functioning.
func (b *Blackboard) HealthCheck(ctx context.Context) error {
	if b.config == nil {
		return &AlgorithmError{
			Algorithm: "blackboard",
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}
	if b.config.MaxIterations <= 0 {
		return &AlgorithmError{
			Algorithm: "blackboard",
			Operation: "HealthCheck",
			Err:       ErrInvalidConfig,
		}
	}
	return nil
}
