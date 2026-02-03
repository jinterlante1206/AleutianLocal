// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package context

import (
	"context"
	"time"
)

// Token limits for summary generation by hierarchy level.
// These limits ensure summaries stay concise and cost-effective.
const (
	// ProjectMaxInputTokens is the maximum input tokens for project summaries.
	ProjectMaxInputTokens = 4000

	// ProjectMaxOutputTokens is the maximum output tokens for project summaries.
	ProjectMaxOutputTokens = 500

	// PackageMaxInputTokens is the maximum input tokens for package summaries.
	PackageMaxInputTokens = 2000

	// PackageMaxOutputTokens is the maximum output tokens for package summaries.
	PackageMaxOutputTokens = 300

	// FileMaxInputTokens is the maximum input tokens for file summaries.
	FileMaxInputTokens = 1000

	// FileMaxOutputTokens is the maximum output tokens for file summaries.
	FileMaxOutputTokens = 150

	// FunctionMaxInputTokens is the maximum input tokens for function summaries.
	FunctionMaxInputTokens = 500

	// FunctionMaxOutputTokens is the maximum output tokens for function summaries.
	FunctionMaxOutputTokens = 100

	// DefaultLLMTemperature is the default temperature for summary generation.
	// Lower temperature produces more focused, deterministic summaries.
	DefaultLLMTemperature = 0.3

	// DefaultLLMTimeout is the default timeout for LLM requests.
	DefaultLLMTimeout = 30 * time.Second
)

// HierarchyLevel represents a level in the code hierarchy.
type HierarchyLevel int

const (
	// LevelProject is the project root level (Level 0).
	LevelProject HierarchyLevel = iota

	// LevelPackage is the package/module level (Level 1).
	LevelPackage

	// LevelFile is the file level (Level 2).
	LevelFile

	// LevelFunction is the function/symbol level (Level 3).
	LevelFunction
)

// String returns the human-readable name for the hierarchy level.
func (l HierarchyLevel) String() string {
	switch l {
	case LevelProject:
		return "project"
	case LevelPackage:
		return "package"
	case LevelFile:
		return "file"
	case LevelFunction:
		return "function"
	default:
		return "unknown"
	}
}

// MaxInputTokens returns the maximum input tokens for this hierarchy level.
func (l HierarchyLevel) MaxInputTokens() int {
	switch l {
	case LevelProject:
		return ProjectMaxInputTokens
	case LevelPackage:
		return PackageMaxInputTokens
	case LevelFile:
		return FileMaxInputTokens
	case LevelFunction:
		return FunctionMaxInputTokens
	default:
		return FunctionMaxInputTokens
	}
}

// MaxOutputTokens returns the maximum output tokens for this hierarchy level.
func (l HierarchyLevel) MaxOutputTokens() int {
	switch l {
	case LevelProject:
		return ProjectMaxOutputTokens
	case LevelPackage:
		return PackageMaxOutputTokens
	case LevelFile:
		return FileMaxOutputTokens
	case LevelFunction:
		return FunctionMaxOutputTokens
	default:
		return FunctionMaxOutputTokens
	}
}

// LLMClient abstracts LLM operations for summary generation.
//
// Implementations must handle:
// - Rate limiting with appropriate backoff
// - Request timeouts
// - Context cancellation
//
// Thread Safety: Implementations must be safe for concurrent use.
type LLMClient interface {
	// Complete sends a prompt and returns the LLM response.
	//
	// Inputs:
	//   - ctx: Context for cancellation and timeout. Must not be nil.
	//   - prompt: The prompt text to send. Must not be empty.
	//   - opts: Optional parameters (max tokens, temperature, timeout).
	//
	// Outputs:
	//   - *LLMResponse: The completion result. Never nil on success.
	//   - error: Non-nil on failure (rate limit, timeout, invalid input, etc.).
	//
	// Errors:
	//   - ErrLLMRateLimited: Rate limit exceeded (retryable)
	//   - ErrLLMTimeout: Request timed out (retryable)
	//   - ErrLLMServerError: Server error 5xx (retryable)
	//   - ErrLLMInvalidRequest: Invalid request (not retryable)
	//
	// Example:
	//   resp, err := client.Complete(ctx, "Summarize this package...",
	//       WithMaxTokens(300),
	//       WithTemperature(0.3),
	//   )
	Complete(ctx context.Context, prompt string, opts ...LLMOption) (*LLMResponse, error)

	// EstimateTokens returns approximate token count for text.
	//
	// Used for budget management before sending requests.
	// Implementations should use the model's tokenizer or a reasonable
	// approximation (e.g., ~4 chars per token for English).
	//
	// Inputs:
	//   - text: The text to estimate tokens for.
	//
	// Outputs:
	//   - int: Estimated token count. Always >= 0.
	EstimateTokens(text string) int
}

// LLMResponse represents a completion response from the LLM.
type LLMResponse struct {
	// Content is the generated text content.
	Content string `json:"content"`

	// TokensUsed is the total tokens consumed (input + output).
	TokensUsed int `json:"tokens_used"`

	// InputTokens is the number of input tokens consumed.
	InputTokens int `json:"input_tokens"`

	// OutputTokens is the number of output tokens generated.
	OutputTokens int `json:"output_tokens"`

	// FinishReason indicates why generation stopped.
	// Values: "stop" (natural end), "length" (max tokens), "error"
	FinishReason string `json:"finish_reason"`

	// Model is the model identifier that generated this response.
	Model string `json:"model,omitempty"`
}

// llmOptions holds configuration for LLM requests.
type llmOptions struct {
	maxTokens   int
	temperature float64
	timeout     time.Duration
}

// defaultLLMOptions returns sensible defaults for LLM requests.
func defaultLLMOptions() *llmOptions {
	return &llmOptions{
		maxTokens:   PackageMaxOutputTokens,
		temperature: DefaultLLMTemperature,
		timeout:     DefaultLLMTimeout,
	}
}

// LLMOption is a functional option for configuring LLM requests.
type LLMOption func(*llmOptions)

// WithMaxTokens sets the maximum tokens for the response.
//
// Inputs:
//   - n: Maximum tokens. Must be positive.
//
// If n <= 0, this option is ignored.
func WithMaxTokens(n int) LLMOption {
	return func(o *llmOptions) {
		if n > 0 {
			o.maxTokens = n
		}
	}
}

// WithTemperature sets the sampling temperature.
//
// Inputs:
//   - t: Temperature value. Should be in range [0.0, 2.0].
//
// Lower values (0.0-0.3) produce more focused, deterministic output.
// Higher values (0.7-1.0) produce more creative, varied output.
// If t < 0, this option is ignored.
func WithTemperature(t float64) LLMOption {
	return func(o *llmOptions) {
		if t >= 0 {
			o.temperature = t
		}
	}
}

// WithLLMTimeout sets the timeout for the LLM request.
//
// Inputs:
//   - d: Timeout duration. Must be positive.
//
// If d <= 0, this option is ignored.
func WithLLMTimeout(d time.Duration) LLMOption {
	return func(o *llmOptions) {
		if d > 0 {
			o.timeout = d
		}
	}
}

// WithLevelTokenLimits sets token limits appropriate for the hierarchy level.
//
// This is a convenience option that sets MaxTokens based on the hierarchy level.
func WithLevelTokenLimits(level HierarchyLevel) LLMOption {
	return func(o *llmOptions) {
		o.maxTokens = level.MaxOutputTokens()
	}
}

// ApplyOptions applies the given options to default settings and returns
// the configured options. This is exported for use by LLM client implementations.
func ApplyOptions(opts ...LLMOption) (maxTokens int, temperature float64, timeout time.Duration) {
	o := defaultLLMOptions()
	for _, opt := range opts {
		opt(o)
	}
	return o.maxTokens, o.temperature, o.timeout
}

// TokenLimitsForLevel returns the input and output token limits for a hierarchy level.
func TokenLimitsForLevel(level HierarchyLevel) (maxInput, maxOutput int) {
	return level.MaxInputTokens(), level.MaxOutputTokens()
}
