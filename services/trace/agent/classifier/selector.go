// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package classifier

import (
	"context"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/llm"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ToolChoiceSelector decides the tool_choice parameter based on query classification.
//
// It uses confidence thresholds to determine whether to:
// - Force a specific tool (high confidence)
// - Force any tool (medium confidence)
// - Let the model decide (low confidence)
//
// Thread Safety: This type is safe for concurrent use.
type ToolChoiceSelector struct {
	// classifier is used to analyze queries.
	classifier QueryClassifier

	// forceThreshold is the confidence above which to force a specific tool.
	// Default: 0.8 (80% confidence)
	forceThreshold float64

	// requireThreshold is the confidence above which to require some tool call.
	// Default: 0.4 (40% confidence)
	requireThreshold float64
}

// SelectorConfig configures the ToolChoiceSelector.
type SelectorConfig struct {
	// ForceThreshold sets the confidence level to force a specific tool.
	ForceThreshold float64

	// RequireThreshold sets the confidence level to require any tool.
	RequireThreshold float64
}

// DefaultSelectorConfig returns sensible default thresholds.
func DefaultSelectorConfig() SelectorConfig {
	return SelectorConfig{
		ForceThreshold:   0.8,
		RequireThreshold: 0.4,
	}
}

// NewToolChoiceSelector creates a selector with the given classifier and config.
//
// Description:
//
//	Creates a selector that uses the classifier to analyze queries
//	and select appropriate tool_choice values based on confidence.
//
// Inputs:
//
//	classifier - The query classifier to use. Must not be nil.
//	config - Configuration for thresholds. Use nil for defaults.
//
// Outputs:
//
//	*ToolChoiceSelector - Ready-to-use selector.
//
// Example:
//
//	classifier := NewRegexClassifier()
//	selector := NewToolChoiceSelector(classifier, nil)
//	choice := selector.SelectToolChoice(ctx, "What tests exist?", tools)
func NewToolChoiceSelector(classifier QueryClassifier, config *SelectorConfig) *ToolChoiceSelector {
	cfg := DefaultSelectorConfig()
	if config != nil {
		cfg = *config
	}

	return &ToolChoiceSelector{
		classifier:       classifier,
		forceThreshold:   cfg.ForceThreshold,
		requireThreshold: cfg.RequireThreshold,
	}
}

// SelectionResult contains the tool choice decision and reasoning.
type SelectionResult struct {
	// ToolChoice is the selected tool_choice parameter.
	ToolChoice *llm.ToolChoice

	// SuggestedTool is the tool that was suggested (if any).
	SuggestedTool string

	// IsAnalytical indicates if the query was classified as analytical.
	IsAnalytical bool

	// Confidence is the classification confidence (0.0-1.0).
	Confidence float64

	// Reason explains the selection decision.
	Reason string
}

// SelectToolChoice analyzes a query and returns the appropriate tool_choice.
//
// Description:
//
//	Classifies the query and selects tool_choice based on confidence:
//	- High confidence (>0.8) + valid tool → force specific tool
//	- Medium confidence (>0.4) → require any tool
//	- Low confidence → auto (model decides)
//	- Non-analytical query → none (no tools)
//
// Inputs:
//
//	ctx - Context for tracing. Must not be nil.
//	query - The user's question.
//	availableTools - List of available tool names.
//
// Outputs:
//
//	SelectionResult - Contains the tool_choice and reasoning.
//
// Example:
//
//	result := selector.SelectToolChoice(ctx, "What tests exist?",
//	    []string{"find_entry_points", "trace_data_flow"})
//	// result.ToolChoice.Type == "tool"
//	// result.ToolChoice.Name == "find_entry_points"
//
// Thread Safety: This method is safe for concurrent use.
func (s *ToolChoiceSelector) SelectToolChoice(
	ctx context.Context,
	query string,
	availableTools []string,
) SelectionResult {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, span := otel.Tracer("classifier").Start(ctx, "classifier.ToolChoiceSelector.SelectToolChoice",
		trace.WithAttributes(
			attribute.Int("query_length", len(query)),
			attribute.Int("available_tools", len(availableTools)),
		),
	)
	defer span.End()

	// Check if query is analytical
	isAnalytical := s.classifier.IsAnalytical(ctx, query)

	// Non-analytical queries don't need tools
	if !isAnalytical {
		span.SetAttributes(
			attribute.Bool("is_analytical", false),
			attribute.String("tool_choice_type", "auto"),
			attribute.String("reason", "non-analytical query"),
		)
		return SelectionResult{
			ToolChoice:   llm.ToolChoiceAuto(),
			IsAnalytical: false,
			Confidence:   0.0,
			Reason:       "non-analytical query",
		}
	}

	// Get tool suggestion and estimate confidence
	suggestedTool, found := s.classifier.SuggestTool(ctx, query, availableTools)

	// Estimate confidence based on suggestion match.
	// These are heuristics since the current classifier doesn't return confidence.
	// Values chosen based on empirical testing:
	//   - 0.85: Strong match (above forceThreshold 0.8) when specific tool suggested
	//   - 0.50: Moderate match (above requireThreshold 0.4) when fallback used
	//   - 0.30: Low match (below requireThreshold) when no tools available
	const (
		confidenceStrongMatch   = 0.85 // Specific tool suggested → force it
		confidenceModerateMatch = 0.50 // Fallback tool suggested → require any
		confidenceLowMatch      = 0.30 // No tools available → let model decide
	)

	var confidence float64
	if found && suggestedTool != "" {
		confidence = confidenceStrongMatch
	} else if found {
		confidence = confidenceModerateMatch
	} else {
		confidence = confidenceLowMatch
	}

	span.SetAttributes(
		attribute.Bool("is_analytical", true),
		attribute.Float64("confidence", confidence),
		attribute.String("suggested_tool", suggestedTool),
	)

	// Select tool_choice based on confidence
	if confidence >= s.forceThreshold && suggestedTool != "" {
		// High confidence: force specific tool
		span.SetAttributes(
			attribute.String("tool_choice_type", "tool"),
			attribute.String("reason", "high confidence match"),
		)
		return SelectionResult{
			ToolChoice:    llm.ToolChoiceRequired(suggestedTool),
			SuggestedTool: suggestedTool,
			IsAnalytical:  true,
			Confidence:    confidence,
			Reason:        "high confidence match, forcing " + suggestedTool,
		}
	}

	if confidence >= s.requireThreshold {
		// Medium confidence: require any tool
		span.SetAttributes(
			attribute.String("tool_choice_type", "any"),
			attribute.String("reason", "medium confidence, requiring any tool"),
		)
		return SelectionResult{
			ToolChoice:    llm.ToolChoiceAny(),
			SuggestedTool: suggestedTool,
			IsAnalytical:  true,
			Confidence:    confidence,
			Reason:        "medium confidence, requiring any tool",
		}
	}

	// Low confidence: let model decide
	span.SetAttributes(
		attribute.String("tool_choice_type", "auto"),
		attribute.String("reason", "low confidence, model decides"),
	)
	return SelectionResult{
		ToolChoice:    llm.ToolChoiceAuto(),
		SuggestedTool: suggestedTool,
		IsAnalytical:  true,
		Confidence:    confidence,
		Reason:        "low confidence, model decides",
	}
}
