// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package mcts

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// TreeBudgetConfig contains configuration for tree exploration limits.
type TreeBudgetConfig struct {
	MaxNodes      int           // Maximum nodes to explore
	MaxDepth      int           // Maximum plan depth
	MaxExpansions int           // Maximum alternatives per node
	TimeLimit     time.Duration // Wall clock limit
	LLMCallLimit  int           // Maximum LLM calls
	LLMTokenLimit int           // Maximum tokens across all LLM calls
	CostLimitUSD  float64       // Maximum cost in USD
}

// DefaultTreeBudgetConfig returns sensible defaults.
func DefaultTreeBudgetConfig() TreeBudgetConfig {
	return TreeBudgetConfig{
		MaxNodes:      20,
		MaxDepth:      5,
		MaxExpansions: 3,
		TimeLimit:     30 * time.Second,
		LLMCallLimit:  50,
		LLMTokenLimit: 100000,
		CostLimitUSD:  1.0, // $1 max
	}
}

// TreeBudget tracks resource consumption during MCTS exploration.
//
// Thread Safety: Safe for concurrent use.
type TreeBudget struct {
	config    TreeBudgetConfig
	startTime time.Time

	// Atomic counters
	nodesExplored int64
	llmCalls      int64
	tokensUsed    int64

	// Cost tracking (protected by mu)
	mu          sync.RWMutex
	costUSD     float64
	exhausted   bool
	exhaustedBy string // Which limit was hit
}

// NewTreeBudget creates a new budget tracker.
//
// Inputs:
//   - config: Budget configuration
//
// Outputs:
//   - *TreeBudget: Budget tracker, ready to use
//
// Thread Safety: The returned budget is safe for concurrent use.
func NewTreeBudget(config TreeBudgetConfig) *TreeBudget {
	return &TreeBudget{
		config:    config,
		startTime: time.Now(),
	}
}

// Config returns the budget configuration.
func (b *TreeBudget) Config() TreeBudgetConfig {
	return b.config
}

// NodesExplored returns the number of nodes explored.
func (b *TreeBudget) NodesExplored() int64 {
	return atomic.LoadInt64(&b.nodesExplored)
}

// RecordNodeExplored records a node exploration.
func (b *TreeBudget) RecordNodeExplored() int64 {
	return atomic.AddInt64(&b.nodesExplored, 1)
}

// LLMCalls returns the number of LLM calls made.
func (b *TreeBudget) LLMCalls() int64 {
	return atomic.LoadInt64(&b.llmCalls)
}

// RecordLLMCall records an LLM call with token usage and cost.
//
// Inputs:
//   - tokens: Number of tokens used
//   - costUSD: Cost in USD for this call
//
// Outputs:
//   - error: Non-nil if budget is exhausted by this call
func (b *TreeBudget) RecordLLMCall(tokens int64, costUSD float64) error {
	atomic.AddInt64(&b.llmCalls, 1)
	atomic.AddInt64(&b.tokensUsed, tokens)

	b.mu.Lock()
	b.costUSD += costUSD
	b.mu.Unlock()

	return b.checkLimits()
}

// TokensUsed returns the total tokens used.
func (b *TreeBudget) TokensUsed() int64 {
	return atomic.LoadInt64(&b.tokensUsed)
}

// CostUSD returns the total cost in USD.
func (b *TreeBudget) CostUSD() float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.costUSD
}

// Elapsed returns time elapsed since budget was created.
func (b *TreeBudget) Elapsed() time.Duration {
	return time.Since(b.startTime)
}

// Remaining returns the remaining budget as a struct.
func (b *TreeBudget) Remaining() BudgetRemaining {
	return BudgetRemaining{
		Nodes:    int(b.config.MaxNodes) - int(b.NodesExplored()),
		Time:     b.config.TimeLimit - b.Elapsed(),
		LLMCalls: b.config.LLMCallLimit - int(b.LLMCalls()),
		Tokens:   b.config.LLMTokenLimit - int(b.TokensUsed()),
		CostUSD:  b.config.CostLimitUSD - b.CostUSD(),
	}
}

// BudgetRemaining contains remaining budget values.
type BudgetRemaining struct {
	Nodes    int           `json:"nodes"`
	Time     time.Duration `json:"time"`
	LLMCalls int           `json:"llm_calls"`
	Tokens   int           `json:"tokens"`
	CostUSD  float64       `json:"cost_usd"`
}

// Exhausted returns whether the budget has been exhausted.
func (b *TreeBudget) Exhausted() bool {
	b.mu.RLock()
	if b.exhausted {
		b.mu.RUnlock()
		return true
	}
	b.mu.RUnlock()

	// Check all limits
	return b.checkLimits() != nil
}

// ExhaustedBy returns which limit caused exhaustion (empty if not exhausted).
func (b *TreeBudget) ExhaustedBy() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.exhaustedBy
}

// checkLimits checks all limits and returns an error if any is exceeded.
func (b *TreeBudget) checkLimits() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.exhausted {
		return ErrBudgetExhausted
	}

	// Time limit
	if b.config.TimeLimit > 0 && time.Since(b.startTime) >= b.config.TimeLimit {
		b.exhausted = true
		b.exhaustedBy = "time"
		return ErrTimeLimitExceeded
	}

	// Node limit
	if b.config.MaxNodes > 0 && atomic.LoadInt64(&b.nodesExplored) >= int64(b.config.MaxNodes) {
		b.exhausted = true
		b.exhaustedBy = "nodes"
		return ErrNodeLimitExceeded
	}

	// LLM call limit
	if b.config.LLMCallLimit > 0 && atomic.LoadInt64(&b.llmCalls) >= int64(b.config.LLMCallLimit) {
		b.exhausted = true
		b.exhaustedBy = "llm_calls"
		return ErrLLMCallLimitExceeded
	}

	// Token limit
	if b.config.LLMTokenLimit > 0 && atomic.LoadInt64(&b.tokensUsed) >= int64(b.config.LLMTokenLimit) {
		b.exhausted = true
		b.exhaustedBy = "tokens"
		return ErrBudgetExhausted
	}

	// Cost limit
	if b.config.CostLimitUSD > 0 && b.costUSD >= b.config.CostLimitUSD {
		b.exhausted = true
		b.exhaustedBy = "cost"
		return ErrCostLimitExceeded
	}

	return nil
}

// CheckDepth checks if the given depth is within limits.
//
// Inputs:
//   - depth: The depth to check
//
// Outputs:
//   - error: Non-nil if depth exceeds MaxDepth
func (b *TreeBudget) CheckDepth(depth int) error {
	if b.config.MaxDepth > 0 && depth >= b.config.MaxDepth {
		return ErrDepthLimitExceeded
	}
	return nil
}

// CanExpand checks if we can expand (create children) at the current state.
//
// Inputs:
//   - currentChildCount: Number of children the node already has
//
// Outputs:
//   - bool: True if expansion is allowed
func (b *TreeBudget) CanExpand(currentChildCount int) bool {
	if b.Exhausted() {
		return false
	}
	if b.config.MaxExpansions > 0 && currentChildCount >= b.config.MaxExpansions {
		return false
	}
	return true
}

// String returns a human-readable budget status.
func (b *TreeBudget) String() string {
	exhaustedStatus := ""
	if b.Exhausted() {
		exhaustedStatus = fmt.Sprintf(" [EXHAUSTED by %s]", b.ExhaustedBy())
	}

	return fmt.Sprintf("Budget{nodes=%d/%d, time=%v/%v, llm=%d/%d, tokens=%d/%d, cost=$%.4f/$%.2f}%s",
		b.NodesExplored(), b.config.MaxNodes,
		b.Elapsed().Round(time.Second), b.config.TimeLimit,
		b.LLMCalls(), b.config.LLMCallLimit,
		b.TokensUsed(), b.config.LLMTokenLimit,
		b.CostUSD(), b.config.CostLimitUSD,
		exhaustedStatus)
}

// UsageReport returns a detailed usage report.
type UsageReport struct {
	Elapsed       time.Duration   `json:"elapsed"`
	NodesExplored int64           `json:"nodes_explored"`
	LLMCalls      int64           `json:"llm_calls"`
	TokensUsed    int64           `json:"tokens_used"`
	CostUSD       float64         `json:"cost_usd"`
	Exhausted     bool            `json:"exhausted"`
	ExhaustedBy   string          `json:"exhausted_by,omitempty"`
	Remaining     BudgetRemaining `json:"remaining"`
}

// Report generates a usage report.
func (b *TreeBudget) Report() UsageReport {
	return UsageReport{
		Elapsed:       b.Elapsed(),
		NodesExplored: b.NodesExplored(),
		LLMCalls:      b.LLMCalls(),
		TokensUsed:    b.TokensUsed(),
		CostUSD:       b.CostUSD(),
		Exhausted:     b.Exhausted(),
		ExhaustedBy:   b.ExhaustedBy(),
		Remaining:     b.Remaining(),
	}
}

// Reset resets the budget counters but keeps the same configuration.
// Useful for restarting exploration with fresh budget.
func (b *TreeBudget) Reset() {
	atomic.StoreInt64(&b.nodesExplored, 0)
	atomic.StoreInt64(&b.llmCalls, 0)
	atomic.StoreInt64(&b.tokensUsed, 0)

	b.mu.Lock()
	b.costUSD = 0
	b.exhausted = false
	b.exhaustedBy = ""
	b.startTime = time.Now()
	b.mu.Unlock()
}
