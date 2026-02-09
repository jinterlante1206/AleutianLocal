// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package classifier

import (
	"fmt"
	"strings"
	"time"
)

// ClassificationResult contains the LLM's analysis of a query.
//
// Thread Safety: This type is immutable after creation and safe for concurrent read.
type ClassificationResult struct {
	// IsAnalytical indicates if the query requires tool exploration.
	IsAnalytical bool `json:"is_analytical"`

	// Tool is the recommended first tool to call.
	// Empty if IsAnalytical is false.
	// VALIDATED: Must exist in available tools list.
	Tool string `json:"tool,omitempty"`

	// Parameters are the recommended parameters for the tool.
	// Keys match the tool's parameter schema.
	// VALIDATED: Types and enum values checked against ParamDef.
	Parameters map[string]any `json:"parameters,omitempty"`

	// SearchPatterns are specific patterns to search for.
	// Used for grep/glob operations within tools.
	SearchPatterns []string `json:"search_patterns,omitempty"`

	// Reasoning explains the classification decision.
	// Useful for debugging and observability.
	Reasoning string `json:"reasoning,omitempty"`

	// Confidence is the model's confidence in this classification (0.0-1.0).
	// Below ConfidenceThreshold triggers regex fallback.
	Confidence float64 `json:"confidence,omitempty"`

	// Cached indicates this result came from cache.
	Cached bool `json:"-"`

	// Duration is how long classification took.
	Duration time.Duration `json:"-"`

	// FallbackUsed indicates regex fallback was used.
	FallbackUsed bool `json:"-"`

	// ValidationWarnings contains any parameter validation warnings.
	ValidationWarnings []string `json:"-"`
}

// ToToolSuggestion converts the classification result to a ToolSuggestion.
//
// Description:
//
//	Converts the rich ClassificationResult into the simpler ToolSuggestion
//	format used by the existing ToolForcingPolicy interface.
//
// Outputs:
//
//	*ToolSuggestion - The tool suggestion, or nil if not analytical.
//
// Thread Safety: This method is safe for concurrent use.
func (r *ClassificationResult) ToToolSuggestion() *ToolSuggestion {
	if !r.IsAnalytical || r.Tool == "" {
		return nil
	}

	// Build search hint from parameters if available
	var searchHint string
	if len(r.Parameters) > 0 {
		var hints []string
		for k, v := range r.Parameters {
			hints = append(hints, fmt.Sprintf("%s=%v", k, v))
		}
		searchHint = fmt.Sprintf("Call %s with %s", r.Tool, strings.Join(hints, ", "))
	} else {
		searchHint = fmt.Sprintf("Use %s to explore the codebase", r.Tool)
	}

	return &ToolSuggestion{
		ToolName:       r.Tool,
		SearchHint:     searchHint,
		SearchPatterns: r.SearchPatterns,
	}
}

// ClassifierConfig configures the LLM classifier behavior.
//
// Thread Safety: This type should not be modified after passing to NewLLMClassifier.
type ClassifierConfig struct {
	// Temperature for classification (0.0 = deterministic).
	// Must be >= 0.0 and <= 1.0.
	Temperature float64

	// MaxTokens limits classification response length.
	// Must be > 0.
	MaxTokens int

	// Timeout for each classification attempt.
	// Must be > 0.
	Timeout time.Duration

	// MaxRetries before fallback to regex.
	// 0 = no retries, fall back immediately on first failure.
	MaxRetries int

	// RetryBackoff is the base duration for exponential backoff.
	// Retry N waits RetryBackoff * 2^N.
	RetryBackoff time.Duration

	// CacheTTL is how long to cache classification results.
	// 0 = no caching.
	CacheTTL time.Duration

	// CacheMaxSize is maximum cache entries before LRU eviction.
	// Must be > 0 if CacheTTL > 0.
	CacheMaxSize int

	// ConfidenceThreshold below which regex fallback is used.
	// Must be >= 0.0 and <= 1.0.
	ConfidenceThreshold float64

	// FallbackToRegex enables regex fallback on LLM errors.
	FallbackToRegex bool

	// MaxConcurrent limits simultaneous classification calls.
	// 0 = unlimited.
	MaxConcurrent int
}

// Validate checks that config values are within valid ranges.
//
// Description:
//
//	Validates all configuration fields and returns an error describing
//	all invalid fields if any are out of range.
//
// Outputs:
//
//	error - Non-nil if any field is invalid, describing all issues.
//
// Thread Safety: This method is safe for concurrent use.
func (c ClassifierConfig) Validate() error {
	var errs []string

	if c.Temperature < 0 || c.Temperature > 1 {
		errs = append(errs, "Temperature must be between 0.0 and 1.0")
	}
	if c.MaxTokens <= 0 {
		errs = append(errs, "MaxTokens must be positive")
	}
	if c.Timeout <= 0 {
		errs = append(errs, "Timeout must be positive")
	}
	if c.MaxRetries < 0 {
		errs = append(errs, "MaxRetries must be non-negative")
	}
	if c.RetryBackoff < 0 {
		errs = append(errs, "RetryBackoff must be non-negative")
	}
	if c.CacheTTL > 0 && c.CacheMaxSize <= 0 {
		errs = append(errs, "CacheMaxSize must be positive when CacheTTL > 0")
	}
	if c.ConfidenceThreshold < 0 || c.ConfidenceThreshold > 1 {
		errs = append(errs, "ConfidenceThreshold must be between 0.0 and 1.0")
	}
	if c.MaxConcurrent < 0 {
		errs = append(errs, "MaxConcurrent must be non-negative")
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid classifier config: %s", strings.Join(errs, "; "))
	}
	return nil
}

// DefaultClassifierConfig returns production defaults.
//
// Description:
//
//	Returns a ClassifierConfig with sensible defaults for production use:
//	- Temperature 0.0 for deterministic classification
//	- 5 second timeout with 2 retries (100ms exponential backoff)
//	- 10 minute cache TTL with 1000 max entries
//	- 0.7 confidence threshold for regex fallback
//
// Outputs:
//
//	ClassifierConfig - Ready-to-use configuration.
//
// Thread Safety: This function is safe for concurrent use.
func DefaultClassifierConfig() ClassifierConfig {
	return ClassifierConfig{
		// GR-Phase1: Use Temperature=0.1 instead of 0.0.
		// Some models (e.g., glm-4.7-flash) return empty responses with Temperature=0.0.
		// A small non-zero temperature ensures the model generates output while
		// remaining mostly deterministic for classification tasks.
		Temperature:         0.1,
		MaxTokens:           256,
		Timeout:             5 * time.Second,
		MaxRetries:          2,
		RetryBackoff:        100 * time.Millisecond,
		CacheTTL:            10 * time.Minute,
		CacheMaxSize:        1000,
		ConfidenceThreshold: 0.7,
		FallbackToRegex:     true,
		MaxConcurrent:       10,
	}
}
