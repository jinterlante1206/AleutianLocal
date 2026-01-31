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
	"sync"
	"sync/atomic"
)

// Default pricing (based on Claude Haiku 2024 pricing).
// These can be overridden via CostConfig.
const (
	// DefaultInputPricePerMillion is the default cost per million input tokens.
	DefaultInputPricePerMillion = 0.25 // $0.25 per 1M tokens

	// DefaultOutputPricePerMillion is the default cost per million output tokens.
	DefaultOutputPricePerMillion = 1.25 // $1.25 per 1M tokens
)

// CostEstimate contains estimated costs for summary generation.
type CostEstimate struct {
	// Entity counts by level
	ProjectCount  int `json:"project_count"`
	PackageCount  int `json:"package_count"`
	FileCount     int `json:"file_count"`
	FunctionCount int `json:"function_count,omitempty"` // Usually not summarized individually

	// Total estimated tokens
	EstimatedInputTokens  int `json:"estimated_input_tokens"`
	EstimatedOutputTokens int `json:"estimated_output_tokens"`
	EstimatedTotalTokens  int `json:"estimated_total_tokens"`

	// Cost in USD
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`

	// Breakdown by hierarchy level
	LevelBreakdown map[HierarchyLevel]LevelCost `json:"level_breakdown"`
}

// LevelCost contains cost breakdown for a single hierarchy level.
type LevelCost struct {
	EntityCount  int     `json:"entity_count"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// CostLimits configures cost thresholds and controls.
type CostLimits struct {
	// MaxTotalTokens is the hard limit on total tokens.
	// Default: 1,000,000
	MaxTotalTokens int `json:"max_total_tokens"`

	// MaxCostUSD is the budget cap in USD.
	// Default: $10.00
	MaxCostUSD float64 `json:"max_cost_usd"`

	// RequireConfirmation enables confirmation prompts for expensive operations.
	RequireConfirmation bool `json:"require_confirmation"`

	// ConfirmationThresholdUSD is the cost threshold for confirmation.
	// Operations exceeding this require confirmation if RequireConfirmation is true.
	// Default: $1.00
	ConfirmationThresholdUSD float64 `json:"confirmation_threshold_usd"`
}

// DefaultCostLimits returns sensible default cost limits.
func DefaultCostLimits() CostLimits {
	return CostLimits{
		MaxTotalTokens:           1_000_000,
		MaxCostUSD:               10.00,
		RequireConfirmation:      true,
		ConfirmationThresholdUSD: 1.00,
	}
}

// CostConfig configures cost calculation.
type CostConfig struct {
	// InputPricePerMillion is the cost per million input tokens.
	InputPricePerMillion float64 `json:"input_price_per_million"`

	// OutputPricePerMillion is the cost per million output tokens.
	OutputPricePerMillion float64 `json:"output_price_per_million"`
}

// DefaultCostConfig returns default pricing configuration.
func DefaultCostConfig() CostConfig {
	return CostConfig{
		InputPricePerMillion:  DefaultInputPricePerMillion,
		OutputPricePerMillion: DefaultOutputPricePerMillion,
	}
}

// CostEstimator estimates and tracks LLM costs.
//
// Thread Safety: Safe for concurrent use.
type CostEstimator struct {
	config CostConfig
	limits CostLimits

	// Running totals (atomic for thread safety)
	totalInputTokens  int64
	totalOutputTokens int64

	mu sync.RWMutex
}

// NewCostEstimator creates a new cost estimator with the given configuration.
func NewCostEstimator(config CostConfig, limits CostLimits) *CostEstimator {
	return &CostEstimator{
		config: config,
		limits: limits,
	}
}

// EstimateForProject estimates costs for summarizing an entire project.
//
// Inputs:
//   - projectCount: Number of projects (typically 1).
//   - packageCount: Number of packages.
//   - fileCount: Number of files.
//
// Outputs:
//   - *CostEstimate: The cost estimate with breakdowns.
func (e *CostEstimator) EstimateForProject(projectCount, packageCount, fileCount int) *CostEstimate {
	estimate := &CostEstimate{
		ProjectCount:   projectCount,
		PackageCount:   packageCount,
		FileCount:      fileCount,
		LevelBreakdown: make(map[HierarchyLevel]LevelCost),
	}

	// Calculate for each level
	levels := []struct {
		level HierarchyLevel
		count int
	}{
		{LevelProject, projectCount},
		{LevelPackage, packageCount},
		{LevelFile, fileCount},
	}

	for _, l := range levels {
		maxInput := l.level.MaxInputTokens()
		maxOutput := l.level.MaxOutputTokens()

		inputTokens := l.count * maxInput
		outputTokens := l.count * maxOutput
		cost := e.calculateCost(inputTokens, outputTokens)

		estimate.LevelBreakdown[l.level] = LevelCost{
			EntityCount:  l.count,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			CostUSD:      cost,
		}

		estimate.EstimatedInputTokens += inputTokens
		estimate.EstimatedOutputTokens += outputTokens
	}

	estimate.EstimatedTotalTokens = estimate.EstimatedInputTokens + estimate.EstimatedOutputTokens
	estimate.EstimatedCostUSD = e.calculateCost(estimate.EstimatedInputTokens, estimate.EstimatedOutputTokens)

	return estimate
}

// EstimateForLevel estimates costs for summarizing entities at a specific level.
//
// Inputs:
//   - level: The hierarchy level.
//   - count: Number of entities to summarize.
//
// Outputs:
//   - *LevelCost: The cost breakdown for this level.
func (e *CostEstimator) EstimateForLevel(level HierarchyLevel, count int) *LevelCost {
	maxInput := level.MaxInputTokens()
	maxOutput := level.MaxOutputTokens()

	inputTokens := count * maxInput
	outputTokens := count * maxOutput

	return &LevelCost{
		EntityCount:  count,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		CostUSD:      e.calculateCost(inputTokens, outputTokens),
	}
}

// calculateCost calculates the USD cost for token usage.
func (e *CostEstimator) calculateCost(inputTokens, outputTokens int) float64 {
	inputCost := float64(inputTokens) * e.config.InputPricePerMillion / 1_000_000
	outputCost := float64(outputTokens) * e.config.OutputPricePerMillion / 1_000_000
	return inputCost + outputCost
}

// RecordUsage records actual token usage.
//
// Thread Safety: Safe for concurrent use.
func (e *CostEstimator) RecordUsage(inputTokens, outputTokens int) {
	atomic.AddInt64(&e.totalInputTokens, int64(inputTokens))
	atomic.AddInt64(&e.totalOutputTokens, int64(outputTokens))
}

// GetUsage returns the current usage totals.
//
// Thread Safety: Safe for concurrent use.
func (e *CostEstimator) GetUsage() (inputTokens, outputTokens int64, costUSD float64) {
	input := atomic.LoadInt64(&e.totalInputTokens)
	output := atomic.LoadInt64(&e.totalOutputTokens)
	cost := e.calculateCost(int(input), int(output))
	return input, output, cost
}

// ResetUsage resets the usage counters.
//
// Thread Safety: Safe for concurrent use.
func (e *CostEstimator) ResetUsage() {
	atomic.StoreInt64(&e.totalInputTokens, 0)
	atomic.StoreInt64(&e.totalOutputTokens, 0)
}

// CheckLimits checks if an operation would exceed limits.
//
// Inputs:
//   - estimate: The cost estimate for the operation.
//
// Outputs:
//   - error: Non-nil if limits would be exceeded.
//   - ErrTokenLimitExceeded: Token limit would be exceeded.
//   - ErrCostLimitExceeded: Cost limit would be exceeded.
//   - ErrConfirmationRequired: Operation needs confirmation.
func (e *CostEstimator) CheckLimits(estimate *CostEstimate) error {
	// Get current usage
	currentInput, currentOutput, _ := e.GetUsage()
	currentTotal := currentInput + currentOutput

	// Check token limit
	projectedTotal := currentTotal + int64(estimate.EstimatedTotalTokens)
	if projectedTotal > int64(e.limits.MaxTotalTokens) {
		return ErrTokenLimitExceeded
	}

	// Check cost limit
	currentCost := e.calculateCost(int(currentInput), int(currentOutput))
	projectedCost := currentCost + estimate.EstimatedCostUSD
	if projectedCost > e.limits.MaxCostUSD {
		return ErrCostLimitExceeded
	}

	// Check confirmation threshold
	if e.limits.RequireConfirmation && estimate.EstimatedCostUSD > e.limits.ConfirmationThresholdUSD {
		return ErrConfirmationRequired
	}

	return nil
}

// EnforceLimits is a convenience function that checks limits and returns
// a detailed error if exceeded.
func (e *CostEstimator) EnforceLimits(estimate *CostEstimate) error {
	return e.CheckLimits(estimate)
}

// WouldExceedTokenLimit checks if additional tokens would exceed the limit.
//
// Thread Safety: Safe for concurrent use.
func (e *CostEstimator) WouldExceedTokenLimit(additionalTokens int) bool {
	currentInput, currentOutput, _ := e.GetUsage()
	currentTotal := currentInput + currentOutput
	return currentTotal+int64(additionalTokens) > int64(e.limits.MaxTotalTokens)
}

// WouldExceedCostLimit checks if additional cost would exceed the limit.
//
// Thread Safety: Safe for concurrent use.
func (e *CostEstimator) WouldExceedCostLimit(additionalCostUSD float64) bool {
	_, _, currentCost := e.GetUsage()
	return currentCost+additionalCostUSD > e.limits.MaxCostUSD
}

// RemainingBudget returns the remaining token and cost budget.
//
// Thread Safety: Safe for concurrent use.
func (e *CostEstimator) RemainingBudget() (tokensRemaining int, costRemaining float64) {
	currentInput, currentOutput, currentCost := e.GetUsage()
	currentTotal := int(currentInput + currentOutput)

	tokensRemaining = e.limits.MaxTotalTokens - currentTotal
	if tokensRemaining < 0 {
		tokensRemaining = 0
	}

	costRemaining = e.limits.MaxCostUSD - currentCost
	if costRemaining < 0 {
		costRemaining = 0
	}

	return tokensRemaining, costRemaining
}

// CostSummary returns a human-readable cost summary.
func (e *CostEstimator) CostSummary() CostSummaryReport {
	input, output, cost := e.GetUsage()
	tokensRemaining, costRemaining := e.RemainingBudget()

	return CostSummaryReport{
		InputTokensUsed:   int(input),
		OutputTokensUsed:  int(output),
		TotalTokensUsed:   int(input + output),
		CostUSD:           cost,
		TokensRemaining:   tokensRemaining,
		CostRemainingUSD:  costRemaining,
		TokenLimitPercent: float64(input+output) / float64(e.limits.MaxTotalTokens) * 100,
		CostLimitPercent:  cost / e.limits.MaxCostUSD * 100,
	}
}

// CostSummaryReport contains a summary of cost usage.
type CostSummaryReport struct {
	InputTokensUsed   int     `json:"input_tokens_used"`
	OutputTokensUsed  int     `json:"output_tokens_used"`
	TotalTokensUsed   int     `json:"total_tokens_used"`
	CostUSD           float64 `json:"cost_usd"`
	TokensRemaining   int     `json:"tokens_remaining"`
	CostRemainingUSD  float64 `json:"cost_remaining_usd"`
	TokenLimitPercent float64 `json:"token_limit_percent"`
	CostLimitPercent  float64 `json:"cost_limit_percent"`
}
