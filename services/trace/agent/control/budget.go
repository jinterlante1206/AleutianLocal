// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package control

import (
	"regexp"
	"strings"
	"sync"
)

// QueryComplexity categorizes query complexity for budget allocation.
type QueryComplexity int

const (
	// ComplexitySimple is for direct, single-entity questions.
	ComplexitySimple QueryComplexity = iota

	// ComplexityMedium is for multi-entity or explanation questions.
	ComplexityMedium

	// ComplexityComplex is for architecture or cross-cutting questions.
	ComplexityComplex
)

// String returns the string representation of complexity.
func (c QueryComplexity) String() string {
	switch c {
	case ComplexitySimple:
		return "simple"
	case ComplexityMedium:
		return "medium"
	case ComplexityComplex:
		return "complex"
	default:
		return "unknown"
	}
}

// BudgetConfig allows configuring budget behavior.
type BudgetConfig struct {
	// TotalSteps is the total step budget (default: 15).
	TotalSteps int

	// DefaultSynthesisRatio is default synthesis reserve (default: 0.20).
	DefaultSynthesisRatio float64

	// SimpleQueryRatio is exploration ratio for simple queries (default: 0.50).
	SimpleQueryRatio float64

	// MediumQueryRatio is exploration ratio for medium queries (default: 0.70).
	MediumQueryRatio float64

	// ComplexQueryRatio is exploration ratio for complex queries (default: 0.85).
	ComplexQueryRatio float64
}

// DefaultBudgetConfig returns production defaults.
func DefaultBudgetConfig() BudgetConfig {
	return BudgetConfig{
		TotalSteps:            15,
		DefaultSynthesisRatio: 0.20,
		SimpleQueryRatio:      0.50,
		MediumQueryRatio:      0.70,
		ComplexQueryRatio:     0.85,
	}
}

// StepBudget manages step allocation between exploration and synthesis.
//
// Description:
//
//	Reserves a configurable percentage of the step budget for the synthesis
//	phase, ensuring the agent has steps available to synthesize a response
//	even when exploration is exhaustive.
//
// Thread Safety: Safe for concurrent use via mutex.
type StepBudget struct {
	mu               sync.Mutex
	totalSteps       int
	currentStep      int
	synthesisPercent float64 // Percentage reserved for synthesis (0.0-1.0)
	complexity       QueryComplexity
	inSynthesis      bool
	config           BudgetConfig
}

// BudgetStatus contains budget state for logging/debugging.
type BudgetStatus struct {
	// CurrentStep is the current step number.
	CurrentStep int

	// TotalSteps is the total step budget.
	TotalSteps int

	// ExplorationBudget is steps available for exploration.
	ExplorationBudget int

	// SynthesisBudget is steps reserved for synthesis.
	SynthesisBudget int

	// InSynthesisPhase indicates if we're in synthesis phase.
	InSynthesisPhase bool

	// Complexity is the detected query complexity.
	Complexity QueryComplexity

	// RemainingSteps is steps remaining in current phase.
	RemainingSteps int
}

// NewStepBudget creates a new budget with the given config.
//
// Inputs:
//
//	config - Configuration for budget behavior.
//
// Outputs:
//
//	*StepBudget - The configured budget manager.
func NewStepBudget(config BudgetConfig) *StepBudget {
	// Validate and apply defaults for invalid values
	if config.TotalSteps <= 0 {
		config.TotalSteps = 15
	}
	if config.DefaultSynthesisRatio <= 0 || config.DefaultSynthesisRatio >= 1.0 {
		config.DefaultSynthesisRatio = 0.20
	}

	return &StepBudget{
		totalSteps:       config.TotalSteps,
		synthesisPercent: config.DefaultSynthesisRatio,
		complexity:       ComplexityMedium, // Default until detected
		config:           config,
	}
}

// Package-level compiled patterns for complexity detection.
var (
	complexPatterns = []string{
		`architecture`,
		`how do .* interact`,
		`relationship between`,
		`all the .* that`,
		`across`,
		`entire`,
		`overview`,
		`design`,
	}

	simplePatterns = []string{
		"what is",
		"where is",
		"show me",
		"find the",
		"list",
		"which file",
		"which function",
	}

	complexRegexes []*regexp.Regexp
	complexOnce    sync.Once
)

// initComplexRegexes compiles complex patterns once.
func initComplexRegexes() {
	complexOnce.Do(func() {
		complexRegexes = make([]*regexp.Regexp, len(complexPatterns))
		for i, p := range complexPatterns {
			complexRegexes[i] = regexp.MustCompile(`(?i)` + p)
		}
	})
}

// DetectComplexity analyzes the query to determine complexity.
//
// Description:
//
//	Examines the query for complexity indicators and adjusts the
//	synthesis budget accordingly. More complex queries get more
//	exploration budget.
//
// Inputs:
//
//	query - The user's query string.
//
// Thread Safety: Safe for concurrent use.
func (b *StepBudget) DetectComplexity(query string) {
	initComplexRegexes()

	b.mu.Lock()
	defer b.mu.Unlock()

	lower := strings.ToLower(query)

	// Check complex patterns first (they override simple)
	for _, regex := range complexRegexes {
		if regex.MatchString(lower) {
			b.complexity = ComplexityComplex
			b.synthesisPercent = 1.0 - b.config.ComplexQueryRatio
			return
		}
	}

	// Check simple patterns
	for _, p := range simplePatterns {
		if strings.Contains(lower, p) {
			b.complexity = ComplexitySimple
			b.synthesisPercent = 1.0 - b.config.SimpleQueryRatio
			return
		}
	}

	// Default to medium
	b.complexity = ComplexityMedium
	b.synthesisPercent = 1.0 - b.config.MediumQueryRatio
}

// CanExplore returns true if exploration steps remain.
//
// Description:
//
//	Checks if the current step is within the exploration budget.
//	Once exploration budget is exhausted, the agent must synthesize.
//
// Outputs:
//
//	bool - True if more exploration steps are available.
//
// Thread Safety: Safe for concurrent use.
func (b *StepBudget) CanExplore() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.inSynthesis {
		return false
	}

	explorationBudget := int(float64(b.totalSteps) * (1.0 - b.synthesisPercent))
	return b.currentStep < explorationBudget
}

// MustSynthesize returns true if we're in the synthesis phase.
//
// Description:
//
//	Returns true when exploration budget is exhausted and the agent
//	must synthesize a response from gathered information.
//
// Outputs:
//
//	bool - True if synthesis is required.
//
// Thread Safety: Safe for concurrent use.
func (b *StepBudget) MustSynthesize() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.inSynthesis {
		return true
	}

	explorationBudget := int(float64(b.totalSteps) * (1.0 - b.synthesisPercent))
	return b.currentStep >= explorationBudget
}

// IncrementStep increments the step counter.
//
// Thread Safety: Safe for concurrent use.
func (b *StepBudget) IncrementStep() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentStep++
}

// RemainingSteps returns steps remaining in total budget.
//
// Thread Safety: Safe for concurrent use.
func (b *StepBudget) RemainingSteps() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.totalSteps - b.currentStep
}

// RemainingExplorationSteps returns steps remaining for exploration.
//
// Thread Safety: Safe for concurrent use.
func (b *StepBudget) RemainingExplorationSteps() int {
	b.mu.Lock()
	defer b.mu.Unlock()

	explorationBudget := int(float64(b.totalSteps) * (1.0 - b.synthesisPercent))
	remaining := explorationBudget - b.currentStep
	if remaining < 0 {
		return 0
	}
	return remaining
}

// EnterSynthesis forces entry into synthesis phase.
//
// Description:
//
//	Explicitly enters synthesis phase regardless of current step.
//	Called when the agent decides it has enough information to synthesize.
//
// Thread Safety: Safe for concurrent use.
func (b *StepBudget) EnterSynthesis() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.inSynthesis = true
}

// IsInSynthesis returns true if explicitly in synthesis phase.
//
// Thread Safety: Safe for concurrent use.
func (b *StepBudget) IsInSynthesis() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.inSynthesis
}

// Status returns current budget status for logging.
//
// Thread Safety: Safe for concurrent use.
func (b *StepBudget) Status() BudgetStatus {
	b.mu.Lock()
	defer b.mu.Unlock()

	explorationBudget := int(float64(b.totalSteps) * (1.0 - b.synthesisPercent))
	synthesisBudget := b.totalSteps - explorationBudget

	remaining := b.totalSteps - b.currentStep
	if remaining < 0 {
		remaining = 0
	}

	return BudgetStatus{
		CurrentStep:       b.currentStep,
		TotalSteps:        b.totalSteps,
		ExplorationBudget: explorationBudget,
		SynthesisBudget:   synthesisBudget,
		InSynthesisPhase:  b.currentStep >= explorationBudget || b.inSynthesis,
		Complexity:        b.complexity,
		RemainingSteps:    remaining,
	}
}

// Reset resets the budget for a new session.
//
// Thread Safety: Safe for concurrent use.
func (b *StepBudget) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.currentStep = 0
	b.inSynthesis = false
	b.complexity = ComplexityMedium
	b.synthesisPercent = b.config.DefaultSynthesisRatio
}

// SetTotalSteps updates the total step budget.
//
// Thread Safety: Safe for concurrent use.
func (b *StepBudget) SetTotalSteps(steps int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.totalSteps = steps
}

// GetComplexity returns the detected query complexity.
//
// Thread Safety: Safe for concurrent use.
func (b *StepBudget) GetComplexity() QueryComplexity {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.complexity
}
