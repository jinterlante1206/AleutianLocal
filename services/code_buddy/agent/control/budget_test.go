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
	"sync"
	"testing"
)

func TestStepBudget_DefaultConfig(t *testing.T) {
	config := DefaultBudgetConfig()

	if config.TotalSteps != 15 {
		t.Errorf("TotalSteps = %d, want 15", config.TotalSteps)
	}
	if config.DefaultSynthesisRatio != 0.20 {
		t.Errorf("DefaultSynthesisRatio = %f, want 0.20", config.DefaultSynthesisRatio)
	}
	if config.SimpleQueryRatio != 0.50 {
		t.Errorf("SimpleQueryRatio = %f, want 0.50", config.SimpleQueryRatio)
	}
	if config.MediumQueryRatio != 0.70 {
		t.Errorf("MediumQueryRatio = %f, want 0.70", config.MediumQueryRatio)
	}
	if config.ComplexQueryRatio != 0.85 {
		t.Errorf("ComplexQueryRatio = %f, want 0.85", config.ComplexQueryRatio)
	}
}

func TestStepBudget_CanExplore(t *testing.T) {
	config := DefaultBudgetConfig()
	budget := NewStepBudget(config)

	// With default 20% synthesis reserve, exploration budget = 80% of 15 = 12 steps
	for i := 0; i < 12; i++ {
		if !budget.CanExplore() {
			t.Errorf("Step %d: CanExplore() = false, want true", i)
		}
		budget.IncrementStep()
	}

	// Step 12 should be at exploration limit
	if budget.CanExplore() {
		t.Error("Step 12: CanExplore() = true, want false (synthesis phase)")
	}
}

func TestStepBudget_MustSynthesize(t *testing.T) {
	config := DefaultBudgetConfig()
	budget := NewStepBudget(config)

	// Not in synthesis phase initially
	if budget.MustSynthesize() {
		t.Error("Initial MustSynthesize() = true, want false")
	}

	// Step through exploration budget
	for i := 0; i < 12; i++ {
		budget.IncrementStep()
	}

	// Now must synthesize
	if !budget.MustSynthesize() {
		t.Error("After exploration exhausted: MustSynthesize() = false, want true")
	}
}

func TestStepBudget_EnterSynthesis(t *testing.T) {
	config := DefaultBudgetConfig()
	budget := NewStepBudget(config)

	// Force synthesis early
	budget.EnterSynthesis()

	if !budget.IsInSynthesis() {
		t.Error("After EnterSynthesis(): IsInSynthesis() = false, want true")
	}

	if budget.CanExplore() {
		t.Error("After EnterSynthesis(): CanExplore() = true, want false")
	}
}

func TestStepBudget_RemainingSteps(t *testing.T) {
	config := DefaultBudgetConfig()
	budget := NewStepBudget(config)

	if budget.RemainingSteps() != 15 {
		t.Errorf("Initial RemainingSteps() = %d, want 15", budget.RemainingSteps())
	}

	budget.IncrementStep()
	if budget.RemainingSteps() != 14 {
		t.Errorf("After 1 step: RemainingSteps() = %d, want 14", budget.RemainingSteps())
	}
}

func TestStepBudget_RemainingExplorationSteps(t *testing.T) {
	config := DefaultBudgetConfig()
	budget := NewStepBudget(config)

	// With default 20% synthesis reserve, exploration budget = 12 steps
	if budget.RemainingExplorationSteps() != 12 {
		t.Errorf("Initial RemainingExplorationSteps() = %d, want 12", budget.RemainingExplorationSteps())
	}

	budget.IncrementStep()
	if budget.RemainingExplorationSteps() != 11 {
		t.Errorf("After 1 step: RemainingExplorationSteps() = %d, want 11", budget.RemainingExplorationSteps())
	}

	// Exhaust exploration
	for i := 0; i < 12; i++ {
		budget.IncrementStep()
	}

	if budget.RemainingExplorationSteps() != 0 {
		t.Errorf("After exhaustion: RemainingExplorationSteps() = %d, want 0", budget.RemainingExplorationSteps())
	}
}

func TestStepBudget_DetectComplexity_Simple(t *testing.T) {
	config := DefaultBudgetConfig()
	budget := NewStepBudget(config)

	simpleQueries := []string{
		"What is the main function?",
		"Where is the config defined?",
		"Show me the error handling",
		"Find the database connection",
		"List all exported functions",
		"Which file contains the router?",
		"Which function handles authentication?",
	}

	for _, query := range simpleQueries {
		budget.Reset()
		budget.DetectComplexity(query)
		if budget.GetComplexity() != ComplexitySimple {
			t.Errorf("Query %q: complexity = %s, want simple", query, budget.GetComplexity())
		}
	}
}

func TestStepBudget_DetectComplexity_Complex(t *testing.T) {
	config := DefaultBudgetConfig()
	budget := NewStepBudget(config)

	complexQueries := []string{
		"What is the architecture of this system?",
		"How do the services interact with each other?",
		"What is the relationship between the packages?",
		"Show me all the functions that handle errors across the codebase",
		"Give me an overview of the entire system",
		"Explain the design of the API",
	}

	for _, query := range complexQueries {
		budget.Reset()
		budget.DetectComplexity(query)
		if budget.GetComplexity() != ComplexityComplex {
			t.Errorf("Query %q: complexity = %s, want complex", query, budget.GetComplexity())
		}
	}
}

func TestStepBudget_DetectComplexity_Medium(t *testing.T) {
	config := DefaultBudgetConfig()
	budget := NewStepBudget(config)

	mediumQueries := []string{
		"How does the authentication work?",
		"Explain the error handling approach",
		"What happens when a request is processed?",
	}

	for _, query := range mediumQueries {
		budget.Reset()
		budget.DetectComplexity(query)
		if budget.GetComplexity() != ComplexityMedium {
			t.Errorf("Query %q: complexity = %s, want medium", query, budget.GetComplexity())
		}
	}
}

func TestStepBudget_ComplexityAffectsBudget(t *testing.T) {
	config := DefaultBudgetConfig()

	tests := []struct {
		query              string
		expectedComplexity QueryComplexity
		expectedExplBudget int
	}{
		{
			query:              "What is the main function?",
			expectedComplexity: ComplexitySimple,
			expectedExplBudget: 7, // 50% of 15
		},
		{
			query:              "How does this work?",
			expectedComplexity: ComplexityMedium,
			expectedExplBudget: 10, // 70% of 15
		},
		{
			query:              "What is the architecture?",
			expectedComplexity: ComplexityComplex,
			expectedExplBudget: 12, // 85% of 15
		},
	}

	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			budget := NewStepBudget(config)
			budget.DetectComplexity(tc.query)

			if budget.GetComplexity() != tc.expectedComplexity {
				t.Errorf("Complexity = %s, want %s", budget.GetComplexity(), tc.expectedComplexity)
			}

			status := budget.Status()
			if status.ExplorationBudget != tc.expectedExplBudget {
				t.Errorf("ExplorationBudget = %d, want %d", status.ExplorationBudget, tc.expectedExplBudget)
			}
		})
	}
}

func TestStepBudget_Status(t *testing.T) {
	config := DefaultBudgetConfig()
	budget := NewStepBudget(config)

	status := budget.Status()

	if status.CurrentStep != 0 {
		t.Errorf("CurrentStep = %d, want 0", status.CurrentStep)
	}
	if status.TotalSteps != 15 {
		t.Errorf("TotalSteps = %d, want 15", status.TotalSteps)
	}
	if status.InSynthesisPhase {
		t.Error("InSynthesisPhase = true, want false")
	}
	if status.RemainingSteps != 15 {
		t.Errorf("RemainingSteps = %d, want 15", status.RemainingSteps)
	}
}

func TestStepBudget_Reset(t *testing.T) {
	config := DefaultBudgetConfig()
	budget := NewStepBudget(config)

	// Modify state
	budget.IncrementStep()
	budget.IncrementStep()
	budget.EnterSynthesis()
	budget.DetectComplexity("What is the architecture?")

	// Reset
	budget.Reset()

	status := budget.Status()
	if status.CurrentStep != 0 {
		t.Errorf("After reset: CurrentStep = %d, want 0", status.CurrentStep)
	}
	if status.InSynthesisPhase {
		t.Error("After reset: InSynthesisPhase = true, want false")
	}
	if budget.GetComplexity() != ComplexityMedium {
		t.Errorf("After reset: complexity = %s, want medium", budget.GetComplexity())
	}
}

func TestStepBudget_SetTotalSteps(t *testing.T) {
	config := DefaultBudgetConfig()
	budget := NewStepBudget(config)

	budget.SetTotalSteps(20)

	if budget.RemainingSteps() != 20 {
		t.Errorf("After SetTotalSteps(20): RemainingSteps() = %d, want 20", budget.RemainingSteps())
	}
}

func TestStepBudget_ThreadSafety(t *testing.T) {
	config := DefaultBudgetConfig()
	budget := NewStepBudget(config)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			budget.IncrementStep()
			budget.CanExplore()
			budget.MustSynthesize()
			budget.RemainingSteps()
			budget.RemainingExplorationSteps()
			budget.Status()
			budget.IsInSynthesis()
			budget.GetComplexity()
		}()
	}
	wg.Wait()

	// Should not panic and should have consistent state
	status := budget.Status()
	if status.CurrentStep != 100 {
		t.Errorf("After 100 concurrent increments: CurrentStep = %d, want 100", status.CurrentStep)
	}
}

func TestQueryComplexity_String(t *testing.T) {
	tests := []struct {
		complexity QueryComplexity
		want       string
	}{
		{ComplexitySimple, "simple"},
		{ComplexityMedium, "medium"},
		{ComplexityComplex, "complex"},
		{QueryComplexity(99), "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.complexity.String(); got != tc.want {
				t.Errorf("Complexity(%d).String() = %q, want %q", tc.complexity, got, tc.want)
			}
		})
	}
}
