// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

// Package phases implements the agent state machine phases.
package phases

import (
	"context"
	"log/slog"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/classifier"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ForcingRequest contains the context for a tool forcing decision.
//
// Thread Safety: This type is immutable and safe for concurrent read access.
type ForcingRequest struct {
	// Query is the user's original query.
	Query string

	// StepNumber is the current execution step.
	StepNumber int

	// ForcingRetries is the number of tool forcing retries so far.
	ForcingRetries int

	// MaxRetries is the maximum allowed forcing retries.
	MaxRetries int

	// MaxStepForForcing is the maximum step number where forcing applies.
	MaxStepForForcing int

	// AvailableTools is the list of available tool names.
	AvailableTools []string
}

// ToolForcingPolicy determines when and how to force tool usage.
//
// Description:
//
//	The policy is responsible for deciding when an LLM response should be
//	rejected in favor of forcing tool usage. This is used when the LLM
//	answers analytical questions without exploring the codebase first.
//
// Thread Safety: Implementations must be safe for concurrent use.
type ToolForcingPolicy interface {
	// ShouldForce determines if tool usage should be forced.
	//
	// Description:
	//   Evaluates the request context to determine if the LLM should be
	//   prompted to use tools instead of answering directly.
	//
	// Inputs:
	//   ctx - Context for tracing and cancellation. Must not be nil.
	//   req - The forcing request context. Must not be nil.
	//
	// Outputs:
	//   bool - True if tool usage should be forced.
	//
	// Example:
	//   if policy.ShouldForce(ctx, &ForcingRequest{Query: "What tests exist?"}) {
	//       // Inject forcing hint
	//   }
	//
	// Thread Safety: This method is safe for concurrent use.
	ShouldForce(ctx context.Context, req *ForcingRequest) bool

	// BuildHint creates the forcing hint message to inject into conversation.
	//
	// Description:
	//   Generates a prompt that encourages the LLM to use tools before
	//   answering. The hint should include the suggested tool if available.
	//
	// Inputs:
	//   ctx - Context for tracing. Must not be nil.
	//   req - The forcing request context. Must not be nil.
	//
	// Outputs:
	//   string - The forcing hint to inject as a user message.
	//
	// Example:
	//   hint := policy.BuildHint(ctx, &ForcingRequest{
	//       Query: "What tests exist?",
	//       AvailableTools: []string{"find_entry_points"},
	//   })
	//
	// Thread Safety: This method is safe for concurrent use.
	BuildHint(ctx context.Context, req *ForcingRequest) string
}

// DefaultForcingPolicy implements ToolForcingPolicy with standard behavior.
//
// Description:
//
//	Uses a QueryClassifier to identify analytical queries that require
//	tool exploration. Forces tool usage when:
//	- The query is analytical (requires codebase exploration)
//	- We haven't exceeded max retries
//	- We're within the early steps where forcing applies
//
// Thread Safety: This type is safe for concurrent use after initialization.
type DefaultForcingPolicy struct {
	// classifier classifies queries for tool forcing decisions.
	classifier classifier.QueryClassifier
}

// NewDefaultForcingPolicy creates a policy with a regex-based classifier.
//
// Description:
//
//	Creates a DefaultForcingPolicy using RegexClassifier for query
//	classification. The classifier uses word-boundary patterns to
//	minimize false positives.
//
// Outputs:
//
//	*DefaultForcingPolicy - A ready-to-use forcing policy.
//
// Example:
//
//	policy := NewDefaultForcingPolicy()
//	if policy.ShouldForce(ctx, req) { ... }
//
// Thread Safety: The returned policy is safe for concurrent use.
func NewDefaultForcingPolicy() *DefaultForcingPolicy {
	return &DefaultForcingPolicy{
		classifier: classifier.NewRegexClassifier(),
	}
}

// NewDefaultForcingPolicyWithClassifier creates a policy with a custom classifier.
//
// Description:
//
//	Allows injecting a custom QueryClassifier for testing or
//	specialized classification logic.
//
// Inputs:
//
//	c - The query classifier to use. Must not be nil.
//
// Outputs:
//
//	*DefaultForcingPolicy - A policy using the provided classifier.
//
// Example:
//
//	mockClassifier := &MockClassifier{isAnalytical: true}
//	policy := NewDefaultForcingPolicyWithClassifier(mockClassifier)
//
// Thread Safety: The returned policy is safe for concurrent use if the
// classifier is safe for concurrent use.
func NewDefaultForcingPolicyWithClassifier(c classifier.QueryClassifier) *DefaultForcingPolicy {
	if c == nil {
		c = classifier.NewRegexClassifier()
	}
	return &DefaultForcingPolicy{
		classifier: c,
	}
}

// ShouldForce determines if tool usage should be forced.
//
// Description:
//
//	Returns true when all of the following are true:
//	- The query is analytical (identified by classifier)
//	- StepNumber <= MaxStepForForcing (early enough to force)
//	- ForcingRetries < MaxRetries (haven't hit circuit breaker)
//
// Inputs:
//
//	ctx - Context for tracing. Must not be nil.
//	req - The forcing request. Must not be nil.
//
// Outputs:
//
//	bool - True if tool usage should be forced.
//
// Example:
//
//	policy := NewDefaultForcingPolicy()
//	shouldForce := policy.ShouldForce(ctx, &ForcingRequest{
//	    Query: "What tests exist?",
//	    StepNumber: 1,
//	    MaxStepForForcing: 2,
//	    ForcingRetries: 0,
//	    MaxRetries: 2,
//	})
//	// shouldForce == true
//
// Thread Safety: This method is safe for concurrent use.
func (p *DefaultForcingPolicy) ShouldForce(ctx context.Context, req *ForcingRequest) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if req == nil {
		return false
	}

	ctx, span := otel.Tracer("phases").Start(ctx, "phases.DefaultForcingPolicy.ShouldForce",
		trace.WithAttributes(
			attribute.Int("step_number", req.StepNumber),
			attribute.Int("forcing_retries", req.ForcingRetries),
			attribute.Int("max_retries", req.MaxRetries),
			attribute.Int("max_step_for_forcing", req.MaxStepForForcing),
		),
	)
	defer span.End()

	// Check step threshold - only force on early steps
	if req.StepNumber > req.MaxStepForForcing {
		span.SetAttributes(
			attribute.Bool("should_force", false),
			attribute.String("reason", "step_threshold_exceeded"),
		)
		slog.Debug("Tool forcing skipped - step threshold exceeded",
			slog.Int("step", req.StepNumber),
			slog.Int("max_step", req.MaxStepForForcing),
		)
		return false
	}

	// Check circuit breaker
	if req.ForcingRetries >= req.MaxRetries {
		span.SetAttributes(
			attribute.Bool("should_force", false),
			attribute.String("reason", "circuit_breaker"),
		)
		slog.Debug("Tool forcing skipped - circuit breaker triggered",
			slog.Int("retries", req.ForcingRetries),
			slog.Int("max_retries", req.MaxRetries),
		)
		return false
	}

	// Check if query is analytical
	isAnalytical := p.classifier.IsAnalytical(ctx, req.Query)
	span.SetAttributes(
		attribute.Bool("is_analytical", isAnalytical),
		attribute.Bool("should_force", isAnalytical),
		attribute.String("reason", func() string {
			if isAnalytical {
				return "analytical_query"
			}
			return "not_analytical"
		}()),
	)

	if isAnalytical {
		slog.Info("Tool forcing triggered for analytical query",
			slog.String("query", req.Query),
			slog.Int("step", req.StepNumber),
			slog.Int("retry", req.ForcingRetries),
		)
	}

	return isAnalytical
}

// BuildHint creates the forcing hint message with targeted search instructions.
//
// Description:
//
//	Generates a user message that prompts the LLM to use tools.
//	Uses SuggestToolWithHint to provide specific search instructions,
//	not just which tool to use but WHAT patterns to search for.
//	This implements Fix 2 (Targeted Exploration) from CB-28d-6.
//
// Inputs:
//
//	ctx - Context for tracing. Must not be nil.
//	req - The forcing request. Must not be nil.
//
// Outputs:
//
//	string - The hint message to inject with specific search instructions.
//
// Example:
//
//	hint := policy.BuildHint(ctx, &ForcingRequest{
//	    Query: "What tests exist?",
//	    AvailableTools: []string{"find_entry_points"},
//	})
//	// hint contains "find_entry_points with type='test'" and search patterns
//
// Thread Safety: This method is safe for concurrent use.
func (p *DefaultForcingPolicy) BuildHint(ctx context.Context, req *ForcingRequest) string {
	if ctx == nil {
		ctx = context.Background()
	}
	if req == nil {
		return "Please use a tool to explore the codebase before answering."
	}

	ctx, span := otel.Tracer("phases").Start(ctx, "phases.DefaultForcingPolicy.BuildHint",
		trace.WithAttributes(
			attribute.Int("available_tools", len(req.AvailableTools)),
		),
	)
	defer span.End()

	// Try to get enhanced suggestion with search instructions
	suggestion, hasSuggestion := p.classifier.SuggestToolWithHint(ctx, req.Query, req.AvailableTools)

	if hasSuggestion && suggestion != nil {
		span.SetAttributes(
			attribute.Bool("has_suggestion", true),
			attribute.String("suggested_tool", suggestion.ToolName),
			attribute.Bool("has_search_hint", suggestion.SearchHint != ""),
			attribute.Int("search_pattern_count", len(suggestion.SearchPatterns)),
		)

		// Build hint with specific search instructions
		var hint strings.Builder
		hint.WriteString("Your response didn't include tool usage. For this type of question, you MUST use tools to explore the codebase first.\n\n")

		// Add the targeted search hint
		if suggestion.SearchHint != "" {
			hint.WriteString("**Recommended approach:**\n")
			hint.WriteString(suggestion.SearchHint)
			hint.WriteString("\n\n")
		}

		// Add specific search patterns if available
		if len(suggestion.SearchPatterns) > 0 {
			hint.WriteString("**Search patterns to look for:**\n")
			for _, pattern := range suggestion.SearchPatterns {
				hint.WriteString("- `")
				hint.WriteString(pattern)
				hint.WriteString("`\n")
			}
			hint.WriteString("\n")
		}

		hint.WriteString("Call the tool now, then provide your answer based on what you find.")

		return hint.String()
	}

	span.SetAttributes(
		attribute.Bool("has_suggestion", false),
		attribute.String("suggestion_type", "generic"),
	)

	// Generic hint when no specific suggestion available
	return `Your response didn't include tool usage. For this type of question, you MUST use tools to explore the codebase first.

Please call a tool now to gather information, then provide your answer based on what you find.`
}

// Ensure DefaultForcingPolicy implements ToolForcingPolicy.
var _ ToolForcingPolicy = (*DefaultForcingPolicy)(nil)
