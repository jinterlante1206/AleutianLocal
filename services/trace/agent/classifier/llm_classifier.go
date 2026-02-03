// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package classifier

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/llm"
	"github.com/AleutianAI/AleutianFOSS/services/trace/cli/tools"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"
)

// classificationPromptTemplate is the optimized prompt for classification.
// Uses only tool names and brief descriptions to minimize tokens (~150 input tokens).
const classificationPromptTemplate = `You are a query classifier for a code analysis agent.

Analyze the user's question and determine:
1. Is this an ANALYTICAL query requiring codebase exploration with tools?
2. If yes, which tool should be called FIRST and with what parameters?

ANALYTICAL queries ask about code structure, behavior, quality, or location.
NON-ANALYTICAL: greetings, general concepts, requests to write new code.

Available tools:
{{range .Tools}}- {{.Name}}: {{.Brief}}
{{end}}

Respond with ONLY valid JSON (no markdown, no preamble):
{"is_analytical":bool,"tool":"name","parameters":{},"search_patterns":[],"reasoning":"brief","confidence":0.0-1.0}`

// toolBrief holds a tool name and brief description for the prompt template.
type toolBrief struct {
	Name  string
	Brief string
}

// LLMClassifier implements QueryClassifier using the LLM client.
//
// Description:
//
//	Uses an LLM to classify queries with higher accuracy than regex patterns.
//	Implements caching, request coalescing, retry logic with exponential backoff,
//	and falls back to RegexClassifier on errors.
//
// Thread Safety: This type is safe for concurrent use after initialization.
type LLMClassifier struct {
	client          llm.Client
	toolDefinitions []tools.ToolDefinition
	toolDefsMap     map[string]tools.ToolDefinition
	toolNames       []string
	toolsHash       string
	config          ClassifierConfig
	cache           *ClassificationCache
	regexFallback   *RegexClassifier
	promptTemplate  *template.Template
	inflight        singleflight.Group
	semaphore       chan struct{}
}

// NewLLMClassifier creates a classifier using the provided LLM client.
//
// Description:
//
//	Creates an LLMClassifier with caching, request coalescing, and regex
//	fallback. The classifier uses the same LLM client as the main agent loop.
//
// Inputs:
//
//	client - LLM client for classification calls. Must not be nil.
//	toolDefs - Available tool definitions. Must not be empty.
//	config - Classifier configuration. Will be validated.
//
// Outputs:
//
//	*LLMClassifier - Ready-to-use classifier.
//	error - If client is nil, toolDefs empty, or config invalid.
//
// Example:
//
//	classifier, err := NewLLMClassifier(llmClient, toolDefs, DefaultClassifierConfig())
//	if err != nil {
//	    return err
//	}
//	result, err := classifier.Classify(ctx, "What tests exist?")
//
// Thread Safety: The returned classifier is safe for concurrent use.
func NewLLMClassifier(
	client llm.Client,
	toolDefs []tools.ToolDefinition,
	config ClassifierConfig,
) (*LLMClassifier, error) {
	if client == nil {
		return nil, errors.New("client must not be nil")
	}
	if len(toolDefs) == 0 {
		return nil, errors.New("toolDefs must not be empty")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Pre-compile prompt template
	tmpl, err := template.New("classify").Parse(classificationPromptTemplate)
	if err != nil {
		return nil, fmt.Errorf("compile prompt template: %w", err)
	}

	// Initialize regex fallback
	regexFallback := NewRegexClassifier()

	// Build tool maps and compute hash
	toolNames := make([]string, 0, len(toolDefs))
	toolDefsMap := make(map[string]tools.ToolDefinition, len(toolDefs))
	for _, td := range toolDefs {
		toolNames = append(toolNames, td.Name)
		toolDefsMap[td.Name] = td
	}
	sort.Strings(toolNames) // Stable hash
	toolsHash := computeToolsHashFromDefs(toolDefs)

	// Initialize cache if enabled
	var cache *ClassificationCache
	if config.CacheTTL > 0 {
		cache = NewClassificationCache(config.CacheTTL, config.CacheMaxSize)
	}

	// Initialize concurrency limiter if enabled
	var semaphore chan struct{}
	if config.MaxConcurrent > 0 {
		semaphore = make(chan struct{}, config.MaxConcurrent)
	}

	return &LLMClassifier{
		client:          client,
		toolDefinitions: toolDefs,
		toolDefsMap:     toolDefsMap,
		toolNames:       toolNames,
		toolsHash:       toolsHash,
		config:          config,
		cache:           cache,
		regexFallback:   regexFallback,
		promptTemplate:  tmpl,
		semaphore:       semaphore,
	}, nil
}

// Classify analyzes a query and returns a classification result.
//
// Description:
//
//	Performs LLM-based classification with caching, request coalescing,
//	retry logic, and validation. Falls back to regex on failure.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout. Must not be nil.
//	query - The user's question. Empty queries return non-analytical result.
//
// Outputs:
//
//	*ClassificationResult - Classification with tool suggestion.
//	error - Only if context cancelled; other errors trigger fallback.
//
// Thread Safety: This method is safe for concurrent use.
func (c *LLMClassifier) Classify(ctx context.Context, query string) (*ClassificationResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	startTime := time.Now()

	ctx, span := otel.Tracer("classifier").Start(ctx, "classifier.LLMClassifier.Classify",
		trace.WithAttributes(
			attribute.Int("query_length", len(query)),
		),
	)
	defer span.End()

	// Handle empty/whitespace queries
	query = strings.TrimSpace(query)
	if query == "" {
		span.SetAttributes(
			attribute.Bool("is_analytical", false),
			attribute.String("reason", "empty_query"),
		)
		return &ClassificationResult{
			IsAnalytical: false,
			Reasoning:    "empty query",
			Duration:     time.Since(startTime),
		}, nil
	}

	// Check cache
	if c.cache != nil {
		if cached, ok := c.cache.Get(query, c.toolsHash); ok {
			span.SetAttributes(
				attribute.Bool("cached", true),
				attribute.Bool("is_analytical", cached.IsAnalytical),
			)
			cached.Duration = time.Since(startTime)
			recordClassification(cached, true)
			return cached, nil
		}
		recordCacheMiss()
	}

	// Use singleflight for request coalescing
	key := c.computeCacheKey(query)
	resultInterface, err, _ := c.inflight.Do(key, func() (interface{}, error) {
		return c.classifyWithRetry(ctx, query, startTime)
	})

	if err != nil {
		// Context cancelled - don't cache
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			span.RecordError(err)
			span.SetStatus(codes.Error, "context cancelled")
			return nil, err
		}

		// Other errors - use fallback
		span.SetAttributes(
			attribute.Bool("fallback_used", true),
			attribute.String("fallback_reason", err.Error()),
		)
		return c.useFallback(ctx, query, startTime, err.Error())
	}

	result := resultInterface.(*ClassificationResult)

	// Cache successful results (not from cancelled requests)
	if c.cache != nil && !result.FallbackUsed {
		c.cache.Set(query, c.toolsHash, result)
	}

	span.SetAttributes(
		attribute.Bool("is_analytical", result.IsAnalytical),
		attribute.String("tool", result.Tool),
		attribute.Float64("confidence", result.Confidence),
		attribute.Bool("fallback_used", result.FallbackUsed),
	)

	recordClassification(result, false)
	return result, nil
}

// classifyWithRetry performs classification with retry logic.
func (c *LLMClassifier) classifyWithRetry(ctx context.Context, query string, startTime time.Time) (*ClassificationResult, error) {
	var lastErr error

	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := c.config.RetryBackoff * time.Duration(1<<(attempt-1))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		result, err := c.doClassify(ctx, query, startTime, attempt)
		if err == nil {
			return result, nil
		}

		lastErr = err

		// Don't retry on context cancellation
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}

		slog.Debug("classification attempt failed, retrying",
			slog.Int("attempt", attempt+1),
			slog.Int("max_retries", c.config.MaxRetries),
			slog.String("error", err.Error()),
		)
		recordRetry()
	}

	return nil, fmt.Errorf("classification failed after %d retries: %w", c.config.MaxRetries+1, lastErr)
}

// doClassify performs a single classification attempt.
func (c *LLMClassifier) doClassify(ctx context.Context, query string, startTime time.Time, attempt int) (*ClassificationResult, error) {
	// Acquire semaphore if configured
	if c.semaphore != nil {
		select {
		case c.semaphore <- struct{}{}:
			defer func() { <-c.semaphore }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Build prompt
	prompt, err := c.buildPrompt()
	if err != nil {
		return nil, fmt.Errorf("build prompt: %w", err)
	}

	// Create request with timeout
	reqCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	request := &llm.Request{
		SystemPrompt: prompt,
		Messages: []llm.Message{
			{Role: "user", Content: query},
		},
		MaxTokens:   c.config.MaxTokens,
		Temperature: c.config.Temperature,
	}

	// Call LLM
	response, err := c.client.Complete(reqCtx, request)
	if err != nil {
		return nil, fmt.Errorf("llm call: %w", err)
	}

	// Parse response
	result, err := ParseClassificationResponse(response.Content)
	if err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// Validate result
	validated, valid := ValidateClassificationResult(result, c.toolDefsMap, c.toolNames)
	if !valid && c.config.FallbackToRegex {
		// Tool was hallucinated - log and fall back
		slog.Warn("LLM hallucinated tool name, using fallback",
			slog.String("hallucinated_tool", result.Tool),
			slog.Any("available_tools", c.toolNames),
		)
		return c.useFallback(ctx, query, startTime, "hallucinated tool: "+result.Tool)
	}

	// Check confidence threshold
	if validated.Confidence < c.config.ConfidenceThreshold && c.config.FallbackToRegex {
		slog.Debug("confidence below threshold, using fallback",
			slog.Float64("confidence", validated.Confidence),
			slog.Float64("threshold", c.config.ConfidenceThreshold),
		)
		return c.useFallback(ctx, query, startTime, fmt.Sprintf("low confidence: %.2f", validated.Confidence))
	}

	validated.Duration = time.Since(startTime)
	return validated, nil
}

// buildPrompt builds the classification prompt from the template.
func (c *LLMClassifier) buildPrompt() (string, error) {
	// Build tool briefs
	briefs := make([]toolBrief, 0, len(c.toolDefinitions))
	for _, td := range c.toolDefinitions {
		briefs = append(briefs, toolBrief{
			Name:  td.Name,
			Brief: truncateDescription(td.Description, 80),
		})
	}

	data := struct {
		Tools []toolBrief
	}{
		Tools: briefs,
	}

	var buf bytes.Buffer
	if err := c.promptTemplate.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// truncateDescription truncates a description to max characters.
func truncateDescription(desc string, maxLen int) string {
	if len(desc) <= maxLen {
		return desc
	}
	return desc[:maxLen-3] + "..."
}

// useFallback uses the regex classifier as fallback.
func (c *LLMClassifier) useFallback(ctx context.Context, query string, startTime time.Time, reason string) (*ClassificationResult, error) {
	recordFallback(reason)

	isAnalytical := c.regexFallback.IsAnalytical(ctx, query)

	result := &ClassificationResult{
		IsAnalytical: isAnalytical,
		FallbackUsed: true,
		Duration:     time.Since(startTime),
		Reasoning:    "regex fallback: " + reason,
	}

	if isAnalytical {
		if suggestion, ok := c.regexFallback.SuggestToolWithHint(ctx, query, c.toolNames); ok {
			result.Tool = suggestion.ToolName
			result.SearchPatterns = suggestion.SearchPatterns
		}
	}

	return result, nil
}

// computeCacheKey creates a cache key for singleflight.
func (c *LLMClassifier) computeCacheKey(query string) string {
	h := sha256.New()
	h.Write([]byte(query))
	h.Write([]byte("|"))
	h.Write([]byte(c.toolsHash))
	return hex.EncodeToString(h.Sum(nil))
}

// computeToolsHashFromDefs creates a stable hash of tool definitions.
func computeToolsHashFromDefs(toolDefs []tools.ToolDefinition) string {
	h := sha256.New()
	names := make([]string, 0, len(toolDefs))
	for _, td := range toolDefs {
		names = append(names, td.Name)
	}
	sort.Strings(names)
	for _, name := range names {
		h.Write([]byte(name))
		h.Write([]byte("|"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// IsAnalytical implements QueryClassifier interface.
//
// Description:
//
//	Classifies the query and returns whether it's analytical.
//	This is a convenience wrapper around Classify().
//
// Thread Safety: This method is safe for concurrent use.
func (c *LLMClassifier) IsAnalytical(ctx context.Context, query string) bool {
	result, err := c.Classify(ctx, query)
	if err != nil {
		// On error, fall back to regex
		return c.regexFallback.IsAnalytical(ctx, query)
	}
	return result.IsAnalytical
}

// SuggestTool implements QueryClassifier interface.
//
// Description:
//
//	Classifies the query and returns a suggested tool if the query
//	is analytical. Returns empty string if not analytical.
//
// Thread Safety: This method is safe for concurrent use.
func (c *LLMClassifier) SuggestTool(ctx context.Context, query string, available []string) (string, bool) {
	result, err := c.Classify(ctx, query)
	if err != nil {
		return c.regexFallback.SuggestTool(ctx, query, available)
	}

	if !result.IsAnalytical || result.Tool == "" {
		return "", false
	}

	// Validate tool is in available list
	for _, tool := range available {
		if tool == result.Tool {
			return result.Tool, true
		}
	}

	// Tool not available, fall back to regex
	return c.regexFallback.SuggestTool(ctx, query, available)
}

// SuggestToolWithHint implements QueryClassifier interface.
//
// Description:
//
//	Classifies the query and returns a ToolSuggestion with the suggested
//	tool and search hints. Falls back to regex if classification fails.
//
// Thread Safety: This method is safe for concurrent use.
func (c *LLMClassifier) SuggestToolWithHint(ctx context.Context, query string, available []string) (*ToolSuggestion, bool) {
	result, err := c.Classify(ctx, query)
	if err != nil {
		return c.regexFallback.SuggestToolWithHint(ctx, query, available)
	}

	if !result.IsAnalytical {
		return nil, false
	}

	// Validate tool is in available list
	toolAvailable := false
	for _, tool := range available {
		if tool == result.Tool {
			toolAvailable = true
			break
		}
	}

	if !toolAvailable && result.Tool != "" {
		// Tool not available, fall back to regex
		return c.regexFallback.SuggestToolWithHint(ctx, query, available)
	}

	suggestion := result.ToToolSuggestion()
	if suggestion == nil {
		return c.regexFallback.SuggestToolWithHint(ctx, query, available)
	}

	return suggestion, true
}

// CacheStats returns cache statistics.
//
// Description:
//
//	Returns the current cache hit rate and size. Returns zeros if
//	caching is disabled.
//
// Outputs:
//
//	hitRate - Cache hit rate (0.0-1.0).
//	size - Current number of cached entries.
//
// Thread Safety: This method is safe for concurrent use.
func (c *LLMClassifier) CacheStats() (hitRate float64, size int) {
	if c.cache == nil {
		return 0, 0
	}
	return c.cache.HitRate(), c.cache.Size()
}

// Ensure LLMClassifier implements QueryClassifier.
var _ QueryClassifier = (*LLMClassifier)(nil)
