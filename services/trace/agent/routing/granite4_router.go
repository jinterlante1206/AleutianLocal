// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/llm"
	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/datatypes"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var tracer = otel.Tracer("aleutian.routing")

// =============================================================================
// Granite4 Router Implementation
// =============================================================================

// Granite4Router implements ToolRouter using granite4:micro-h via Ollama.
//
// # Description
//
// Uses IBM's Granite4 model with hybrid Mamba-2 architecture for ultra-fast
// tool routing. The Mamba-2 architecture has linear complexity (vs quadratic
// for attention), making it ideal for fast classification tasks.
//
// # Thread Safety
//
// Granite4Router is safe for concurrent use.
type Granite4Router struct {
	modelManager  *llm.MultiModelManager
	config        RouterConfig
	promptBuilder *PromptBuilder
	logger        *slog.Logger
}

// NewGranite4Router creates a new router with the specified configuration.
//
// # Description
//
// Creates a router that uses granite4:micro-h (or another small model) for
// fast tool selection. The router integrates with MultiModelManager to
// prevent model thrashing.
//
// # Inputs
//
//   - modelManager: MultiModelManager for model coordination.
//   - config: Router configuration.
//
// # Outputs
//
//   - *Granite4Router: Configured router.
//   - error: Non-nil if initialization fails.
//
// # Example
//
//	mgr := llm.NewMultiModelManager("http://localhost:11434")
//	config := routing.DefaultRouterConfig()
//	router, err := routing.NewGranite4Router(mgr, config)
func NewGranite4Router(modelManager *llm.MultiModelManager, config RouterConfig) (*Granite4Router, error) {
	if modelManager == nil {
		return nil, fmt.Errorf("modelManager must not be nil")
	}

	promptBuilder, err := NewPromptBuilder()
	if err != nil {
		return nil, fmt.Errorf("creating prompt builder: %w", err)
	}

	return &Granite4Router{
		modelManager:  modelManager,
		config:        config,
		promptBuilder: promptBuilder,
		logger:        slog.Default(),
	}, nil
}

// NewGranite4RouterWithDefaults creates a router with default configuration.
//
// # Description
//
// Convenience constructor that creates a MultiModelManager and uses
// default configuration. For more control, use NewGranite4Router.
//
// # Inputs
//
//   - ollamaEndpoint: Ollama server URL.
//
// # Outputs
//
//   - *Granite4Router: Configured router.
//   - error: Non-nil if initialization fails.
func NewGranite4RouterWithDefaults(ollamaEndpoint string) (*Granite4Router, error) {
	config := DefaultRouterConfig()
	config.OllamaEndpoint = ollamaEndpoint

	modelManager := llm.NewMultiModelManager(ollamaEndpoint)

	return NewGranite4Router(modelManager, config)
}

// SelectTool chooses the best tool for the given query.
//
// # Description
//
// Uses the micro LLM to analyze the query and select the most appropriate
// tool. Returns an error if the selection fails or confidence is too low.
//
// # Inputs
//
//   - ctx: Context for cancellation/timeout.
//   - query: The user's question or request.
//   - availableTools: Tools currently available.
//   - codeContext: Optional context about the codebase.
//
// # Outputs
//
//   - *ToolSelection: The selected tool with confidence.
//   - error: Non-nil if routing fails.
func (r *Granite4Router) SelectTool(ctx context.Context, query string, availableTools []ToolSpec, codeContext *CodeContext) (*ToolSelection, error) {
	ctx, span := tracer.Start(ctx, "Granite4Router.SelectTool")
	defer span.End()

	span.SetAttributes(
		attribute.String("router.model", r.config.Model),
		attribute.Int("router.num_tools", len(availableTools)),
		attribute.String("query_preview", truncate(query, 100)),
	)

	startTime := time.Now()

	// Validate inputs
	if len(availableTools) == 0 {
		return nil, NewRouterError(ErrCodeNoTools, "no tools available for selection", false)
	}

	// Apply timeout
	if r.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.config.Timeout)
		defer cancel()
	}

	// Build prompts
	systemPrompt, err := r.promptBuilder.BuildSystemPrompt(availableTools, codeContext)
	if err != nil {
		return nil, fmt.Errorf("building system prompt: %w", err)
	}

	userPrompt := r.promptBuilder.BuildUserPrompt(query)

	r.logger.Debug("Routing query",
		slog.String("model", r.config.Model),
		slog.Int("num_tools", len(availableTools)),
		slog.String("query_preview", truncate(query, 50)),
	)

	// Call the model
	messages := []datatypes.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	temp := float32(r.config.Temperature)
	maxTokens := r.config.MaxTokens
	params := llm.GenerationParams{
		Temperature:   &temp,
		MaxTokens:     &maxTokens,
		KeepAlive:     r.config.KeepAlive,
		ModelOverride: r.config.Model,
	}

	response, err := r.modelManager.Chat(ctx, r.config.Model, messages, params)
	if err != nil {
		duration := time.Since(startTime)
		if ctx.Err() == context.DeadlineExceeded {
			span.SetStatus(codes.Error, "timeout")
			RecordRoutingLatency(r.config.Model, "error", duration.Seconds())
			RecordRoutingError(r.config.Model, "timeout")
			return nil, NewRouterError(ErrCodeTimeout, "routing timed out", true)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "chat failed")
		RecordRoutingLatency(r.config.Model, "error", duration.Seconds())
		RecordRoutingError(r.config.Model, "chat_failed")
		return nil, fmt.Errorf("router chat failed: %w", err)
	}

	// Parse the response
	selection, err := r.parseResponse(response, availableTools)
	if err != nil {
		duration := time.Since(startTime)
		span.RecordError(err)
		span.SetStatus(codes.Error, "parse failed")
		RecordRoutingLatency(r.config.Model, "error", duration.Seconds())
		RecordRoutingError(r.config.Model, "parse_error")
		return nil, err
	}

	selection.Duration = time.Since(startTime)

	// Record confidence metric
	RecordRoutingConfidence(r.config.Model, selection.Confidence)

	// Check confidence threshold
	if selection.Confidence < r.config.ConfidenceThreshold {
		span.SetStatus(codes.Error, "low confidence")
		RecordRoutingLatency(r.config.Model, "low_confidence", selection.Duration.Seconds())
		RecordRoutingFallback(r.config.Model, "low_confidence")
		return selection, NewRouterError(
			ErrCodeLowConfidence,
			fmt.Sprintf("confidence %.2f below threshold %.2f", selection.Confidence, r.config.ConfidenceThreshold),
			false,
		)
	}

	// Record successful selection
	RecordRoutingLatency(r.config.Model, "success", selection.Duration.Seconds())
	RecordRoutingSelection(r.config.Model, selection.Tool)

	span.SetAttributes(
		attribute.String("selection.tool", selection.Tool),
		attribute.Float64("selection.confidence", selection.Confidence),
		attribute.Int64("selection.duration_ms", selection.Duration.Milliseconds()),
	)

	r.logger.Info("Tool selected",
		slog.String("tool", selection.Tool),
		slog.Float64("confidence", selection.Confidence),
		slog.Duration("duration", selection.Duration),
		slog.String("reasoning", selection.Reasoning),
	)

	return selection, nil
}

// parseResponse parses the JSON response from the router model.
//
// # Description
//
// Extracts the tool selection from the model's JSON response.
// Handles common variations in output format.
//
// # Inputs
//
//   - response: Raw response from the model.
//   - availableTools: Tools to validate against.
//
// # Outputs
//
//   - *ToolSelection: Parsed selection.
//   - error: Non-nil if parsing fails.
func (r *Granite4Router) parseResponse(response string, availableTools []ToolSpec) (*ToolSelection, error) {
	// Clean up the response - remove markdown code blocks if present
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	// Try to find JSON in the response
	startIdx := strings.Index(response, "{")
	endIdx := strings.LastIndex(response, "}")
	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		return nil, NewRouterError(
			ErrCodeParseError,
			"no JSON object found in response: "+truncate(response, 100),
			false,
		)
	}

	jsonStr := response[startIdx : endIdx+1]

	// Parse JSON
	var result struct {
		Tool       string  `json:"tool"`
		Confidence float64 `json:"confidence"`
		Reasoning  string  `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, NewRouterError(
			ErrCodeParseError,
			fmt.Sprintf("failed to parse JSON: %v, response: %s", err, truncate(jsonStr, 100)),
			false,
		)
	}

	// Validate tool exists
	toolValid := false
	for _, t := range availableTools {
		if t.Name == result.Tool {
			toolValid = true
			break
		}
	}

	if !toolValid {
		// Try to find closest match
		result.Tool = r.findClosestTool(result.Tool, availableTools)
		result.Confidence *= 0.8 // Reduce confidence for corrected tool
	}

	// Clamp confidence to valid range
	if result.Confidence < 0 {
		result.Confidence = 0
	}
	if result.Confidence > 1 {
		result.Confidence = 1
	}

	return &ToolSelection{
		Tool:       result.Tool,
		Confidence: result.Confidence,
		Reasoning:  result.Reasoning,
	}, nil
}

// findClosestTool finds the closest matching tool name.
//
// # Description
//
// Uses simple string matching to find a tool when the model returns
// an invalid tool name. This provides some resilience to typos.
func (r *Granite4Router) findClosestTool(name string, tools []ToolSpec) string {
	name = strings.ToLower(name)

	// Try exact match (case-insensitive)
	for _, t := range tools {
		if strings.ToLower(t.Name) == name {
			return t.Name
		}
	}

	// Try prefix match
	for _, t := range tools {
		if strings.HasPrefix(strings.ToLower(t.Name), name) {
			return t.Name
		}
	}

	// Try contains match
	for _, t := range tools {
		if strings.Contains(strings.ToLower(t.Name), name) ||
			strings.Contains(name, strings.ToLower(t.Name)) {
			return t.Name
		}
	}

	// Fall back to first tool
	if len(tools) > 0 {
		return tools[0].Name
	}

	return name
}

// Model returns the model being used for routing.
func (r *Granite4Router) Model() string {
	return r.config.Model
}

// Close releases any resources held by the router.
//
// # Description
//
// Currently a no-op since the MultiModelManager is shared.
// Future implementations might unload the router model here.
func (r *Granite4Router) Close() error {
	// No-op: MultiModelManager is shared and manages model lifecycle
	return nil
}

// WarmRouter pre-loads the routing model.
//
// # Description
//
// Warms up the router model so the first routing request doesn't incur
// model loading latency. Should be called during initialization.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//
// # Outputs
//
//   - error: Non-nil if warmup fails.
func (r *Granite4Router) WarmRouter(ctx context.Context) error {
	r.logger.Info("Warming router model", slog.String("model", r.config.Model))
	startTime := time.Now()
	err := r.modelManager.WarmModel(ctx, r.config.Model, r.config.KeepAlive)
	duration := time.Since(startTime)
	RecordModelWarmup(r.config.Model, duration.Seconds(), err == nil)
	return err
}

// =============================================================================
// Helpers
// =============================================================================

// truncate truncates a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
