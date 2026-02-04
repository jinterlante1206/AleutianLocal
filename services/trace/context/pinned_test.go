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
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockTokenCounter provides predictable token counting for tests.
type mockTokenCounter struct {
	tokensPerChar float64
}

func (m *mockTokenCounter) Count(text string) int {
	return int(float64(len(text)) * m.tokensPerChar)
}

func newMockTokenCounter() *mockTokenCounter {
	return &mockTokenCounter{tokensPerChar: 0.25} // 4 chars per token
}

func TestNewPinnedInstructions(t *testing.T) {
	t.Run("default configuration", func(t *testing.T) {
		p := NewPinnedInstructions()

		if p == nil {
			t.Fatal("expected non-nil PinnedInstructions")
		}
		if p.maxTokenBudget != DefaultPinnedBudget {
			t.Errorf("expected default budget %d, got %d", DefaultPinnedBudget, p.maxTokenBudget)
		}
		if p.tokenCounter == nil {
			t.Error("expected non-nil token counter")
		}
		if !p.IsEmpty() {
			t.Error("expected new PinnedInstructions to be empty")
		}
	})

	t.Run("with custom budget", func(t *testing.T) {
		p := NewPinnedInstructions(WithTokenBudget(3000))

		if p.maxTokenBudget != 3000 {
			t.Errorf("expected budget 3000, got %d", p.maxTokenBudget)
		}
	})

	t.Run("with budget below minimum", func(t *testing.T) {
		p := NewPinnedInstructions(WithTokenBudget(100))

		if p.maxTokenBudget != MinPinnedBudget {
			t.Errorf("expected minimum budget %d, got %d", MinPinnedBudget, p.maxTokenBudget)
		}
	})

	t.Run("with custom token counter", func(t *testing.T) {
		tc := newMockTokenCounter()
		p := NewPinnedInstructions(WithTokenCounter(tc))

		if p.tokenCounter != tc {
			t.Error("expected custom token counter to be set")
		}
	})

	t.Run("nil token counter uses default", func(t *testing.T) {
		p := NewPinnedInstructions(WithTokenCounter(nil))

		if p.tokenCounter == nil {
			t.Error("expected non-nil token counter")
		}
	})
}

func TestPinnedInstructions_SetOriginalQuery(t *testing.T) {
	t.Run("set query successfully", func(t *testing.T) {
		p := NewPinnedInstructions()
		query := "Fix the auth bug in login.go"

		err := p.SetOriginalQuery(query)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if p.OriginalQuery() != query {
			t.Errorf("expected query %q, got %q", query, p.OriginalQuery())
		}
	})

	t.Run("immutable after first set", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetOriginalQuery("first query")

		err := p.SetOriginalQuery("second query")

		if err != ErrQueryAlreadySet {
			t.Errorf("expected ErrQueryAlreadySet, got %v", err)
		}
		if p.OriginalQuery() != "first query" {
			t.Error("query should not have changed")
		}
	})

	t.Run("empty query returns error", func(t *testing.T) {
		p := NewPinnedInstructions()

		err := p.SetOriginalQuery("")

		if err != ErrEmptyQuery {
			t.Errorf("expected ErrEmptyQuery, got %v", err)
		}
	})

	t.Run("whitespace-only query returns error", func(t *testing.T) {
		p := NewPinnedInstructions()

		err := p.SetOriginalQuery("   \n\t  ")

		if err != ErrEmptyQuery {
			t.Errorf("expected ErrEmptyQuery, got %v", err)
		}
	})

	t.Run("long query is truncated", func(t *testing.T) {
		p := NewPinnedInstructions()
		longQuery := strings.Repeat("x", MaxOriginalQueryLen+100)

		err := p.SetOriginalQuery(longQuery)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(p.OriginalQuery()) > MaxOriginalQueryLen {
			t.Errorf("query should be truncated to %d, got %d", MaxOriginalQueryLen, len(p.OriginalQuery()))
		}
		if !strings.HasSuffix(p.OriginalQuery(), "...") {
			t.Error("truncated query should end with ...")
		}
	})

	t.Run("nil receiver returns error", func(t *testing.T) {
		var p *PinnedInstructions

		err := p.SetOriginalQuery("query")

		if err != ErrNilPinnedInstructions {
			t.Errorf("expected ErrNilPinnedInstructions, got %v", err)
		}
	})

	t.Run("invalidates cache", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.Render() // Populate cache
		p.cacheValid = true

		_ = p.SetOriginalQuery("new query")

		if p.cacheValid {
			t.Error("cache should be invalidated after setting query")
		}
	})
}

func TestPinnedInstructions_SetPlan(t *testing.T) {
	t.Run("set plan successfully", func(t *testing.T) {
		p := NewPinnedInstructions()
		steps := []PlanStep{
			{Description: "Read login.go", Status: StepPending},
			{Description: "Find bug", Status: StepPending},
		}

		err := p.SetPlan(steps)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		plan := p.GetPlan()
		if len(plan) != 2 {
			t.Errorf("expected 2 steps, got %d", len(plan))
		}
	})

	t.Run("too many steps returns error", func(t *testing.T) {
		p := NewPinnedInstructions()
		steps := make([]PlanStep, MaxPlanSteps+1)

		err := p.SetPlan(steps)

		if err != ErrPlanStepsLimitReached {
			t.Errorf("expected ErrPlanStepsLimitReached, got %v", err)
		}
	})

	t.Run("replaces existing plan", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetPlan([]PlanStep{{Description: "old step"}})

		_ = p.SetPlan([]PlanStep{{Description: "new step"}})

		plan := p.GetPlan()
		if len(plan) != 1 || plan[0].Description != "new step" {
			t.Error("plan should be replaced")
		}
	})

	t.Run("returns copy not reference", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetPlan([]PlanStep{{Description: "original"}})

		plan := p.GetPlan()
		plan[0].Description = "modified"

		if p.GetPlan()[0].Description != "original" {
			t.Error("GetPlan should return a copy")
		}
	})

	t.Run("nil receiver returns error", func(t *testing.T) {
		var p *PinnedInstructions

		err := p.SetPlan([]PlanStep{})

		if err != ErrNilPinnedInstructions {
			t.Errorf("expected ErrNilPinnedInstructions, got %v", err)
		}
	})

	t.Run("truncates long step descriptions", func(t *testing.T) {
		p := NewPinnedInstructions()
		longDesc := strings.Repeat("x", MaxStepDescriptionLen+50)

		err := p.SetPlan([]PlanStep{{Description: longDesc}})

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		plan := p.GetPlan()
		if len(plan[0].Description) > MaxStepDescriptionLen {
			t.Errorf("description should be truncated to %d, got %d", MaxStepDescriptionLen, len(plan[0].Description))
		}
		if !strings.HasSuffix(plan[0].Description, "...") {
			t.Error("truncated description should end with ...")
		}
	})
}

func TestPinnedInstructions_UpdateStepStatus(t *testing.T) {
	t.Run("update status successfully", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetPlan([]PlanStep{{Description: "step 1", Status: StepPending}})

		err := p.UpdateStepStatus(0, StepDone)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if p.GetPlan()[0].Status != StepDone {
			t.Error("status should be updated")
		}
	})

	t.Run("sets StartedAt on InProgress", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetPlan([]PlanStep{{Description: "step 1", Status: StepPending}})
		before := time.Now()

		_ = p.UpdateStepStatus(0, StepInProgress)

		step := p.GetPlan()[0]
		if step.StartedAt.Before(before) {
			t.Error("StartedAt should be set")
		}
	})

	t.Run("sets CompletedAt on Done", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetPlan([]PlanStep{{Description: "step 1", Status: StepPending}})
		before := time.Now()

		_ = p.UpdateStepStatus(0, StepDone)

		step := p.GetPlan()[0]
		if step.CompletedAt.Before(before) {
			t.Error("CompletedAt should be set")
		}
	})

	t.Run("sets CompletedAt on Skipped", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetPlan([]PlanStep{{Description: "step 1", Status: StepPending}})
		before := time.Now()

		_ = p.UpdateStepStatus(0, StepSkipped)

		step := p.GetPlan()[0]
		if step.CompletedAt.Before(before) {
			t.Error("CompletedAt should be set on skip")
		}
	})

	t.Run("invalid index returns error", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetPlan([]PlanStep{{Description: "step 1"}})

		err := p.UpdateStepStatus(5, StepDone)

		if err != ErrInvalidStepIndex {
			t.Errorf("expected ErrInvalidStepIndex, got %v", err)
		}
	})

	t.Run("negative index returns error", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetPlan([]PlanStep{{Description: "step 1"}})

		err := p.UpdateStepStatus(-1, StepDone)

		if err != ErrInvalidStepIndex {
			t.Errorf("expected ErrInvalidStepIndex, got %v", err)
		}
	})

	t.Run("invalid status returns error", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetPlan([]PlanStep{{Description: "step 1"}})

		err := p.UpdateStepStatus(0, StepStatus("invalid"))

		if err == nil {
			t.Error("expected error for invalid status")
		}
	})

	t.Run("nil receiver returns error", func(t *testing.T) {
		var p *PinnedInstructions

		err := p.UpdateStepStatus(0, StepDone)

		if err != ErrNilPinnedInstructions {
			t.Errorf("expected ErrNilPinnedInstructions, got %v", err)
		}
	})
}

func TestPinnedInstructions_AddFinding(t *testing.T) {
	t.Run("add finding successfully", func(t *testing.T) {
		p := NewPinnedInstructions()
		finding := Finding{
			Summary: "Auth check missing nil guard",
			Source:  "login.go:142",
		}

		err := p.AddFinding(context.Background(), finding)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		findings := p.GetFindings()
		if len(findings) != 1 {
			t.Errorf("expected 1 finding, got %d", len(findings))
		}
	})

	t.Run("sets timestamp if not set", func(t *testing.T) {
		p := NewPinnedInstructions()
		finding := Finding{Summary: "test"}

		_ = p.AddFinding(context.Background(), finding)

		if p.GetFindings()[0].Timestamp.IsZero() {
			t.Error("timestamp should be set")
		}
	})

	t.Run("preserves provided timestamp", func(t *testing.T) {
		p := NewPinnedInstructions()
		ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		finding := Finding{Summary: "test", Timestamp: ts}

		_ = p.AddFinding(context.Background(), finding)

		if !p.GetFindings()[0].Timestamp.Equal(ts) {
			t.Error("provided timestamp should be preserved")
		}
	})

	t.Run("truncates long summary", func(t *testing.T) {
		p := NewPinnedInstructions()
		finding := Finding{Summary: strings.Repeat("x", MaxFindingSummaryLen+50)}

		_ = p.AddFinding(context.Background(), finding)

		if len(p.GetFindings()[0].Summary) > MaxFindingSummaryLen {
			t.Error("summary should be truncated")
		}
	})

	t.Run("truncates long detail", func(t *testing.T) {
		p := NewPinnedInstructions()
		finding := Finding{
			Summary: "test",
			Detail:  strings.Repeat("x", MaxFindingDetailLen+50),
		}

		_ = p.AddFinding(context.Background(), finding)

		if len(p.GetFindings()[0].Detail) > MaxFindingDetailLen {
			t.Error("detail should be truncated")
		}
	})

	t.Run("limit reached returns error", func(t *testing.T) {
		p := NewPinnedInstructions()
		for i := 0; i < MaxFindings; i++ {
			_ = p.AddFinding(context.Background(), Finding{Summary: "test"})
		}

		err := p.AddFinding(context.Background(), Finding{Summary: "one more"})

		if err != ErrFindingsLimitReached {
			t.Errorf("expected ErrFindingsLimitReached, got %v", err)
		}
	})

	t.Run("nil receiver returns error", func(t *testing.T) {
		var p *PinnedInstructions

		err := p.AddFinding(context.Background(), Finding{})

		if err != ErrNilPinnedInstructions {
			t.Errorf("expected ErrNilPinnedInstructions, got %v", err)
		}
	})
}

func TestPinnedInstructions_AddConstraint(t *testing.T) {
	t.Run("add constraint successfully", func(t *testing.T) {
		p := NewPinnedInstructions()

		err := p.AddConstraint("Must not modify Session interface")

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		constraints := p.GetConstraints()
		if len(constraints) != 1 {
			t.Errorf("expected 1 constraint, got %d", len(constraints))
		}
	})

	t.Run("empty constraint is ignored", func(t *testing.T) {
		p := NewPinnedInstructions()

		err := p.AddConstraint("")

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(p.GetConstraints()) != 0 {
			t.Error("empty constraint should be ignored")
		}
	})

	t.Run("whitespace-only constraint is ignored", func(t *testing.T) {
		p := NewPinnedInstructions()

		err := p.AddConstraint("   \n\t  ")

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(p.GetConstraints()) != 0 {
			t.Error("whitespace-only constraint should be ignored")
		}
	})

	t.Run("truncates long constraint", func(t *testing.T) {
		p := NewPinnedInstructions()

		_ = p.AddConstraint(strings.Repeat("x", MaxConstraintLen+50))

		if len(p.GetConstraints()[0]) > MaxConstraintLen {
			t.Error("constraint should be truncated")
		}
	})

	t.Run("limit reached returns error", func(t *testing.T) {
		p := NewPinnedInstructions()
		for i := 0; i < MaxConstraints; i++ {
			_ = p.AddConstraint("constraint")
		}

		err := p.AddConstraint("one more")

		if err != ErrConstraintsLimitReached {
			t.Errorf("expected ErrConstraintsLimitReached, got %v", err)
		}
	})

	t.Run("nil receiver returns error", func(t *testing.T) {
		var p *PinnedInstructions

		err := p.AddConstraint("test")

		if err != ErrNilPinnedInstructions {
			t.Errorf("expected ErrNilPinnedInstructions, got %v", err)
		}
	})
}

func TestPinnedInstructions_Render(t *testing.T) {
	t.Run("empty block renders minimal header", func(t *testing.T) {
		p := NewPinnedInstructions()

		rendered := p.Render()

		if !strings.Contains(rendered, "Session Context") {
			t.Error("should contain header")
		}
	})

	t.Run("includes original query", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetOriginalQuery("Fix the auth bug")

		rendered := p.Render()

		if !strings.Contains(rendered, "Original Request") {
			t.Error("should contain Original Request section")
		}
		if !strings.Contains(rendered, "Fix the auth bug") {
			t.Error("should contain the query")
		}
	})

	t.Run("includes plan with status symbols", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetPlan([]PlanStep{
			{Description: "Step 1", Status: StepDone},
			{Description: "Step 2", Status: StepInProgress},
			{Description: "Step 3", Status: StepPending},
		})

		rendered := p.Render()

		if !strings.Contains(rendered, "Current Plan") {
			t.Error("should contain Current Plan section")
		}
		if !strings.Contains(rendered, "[x] Step 1") {
			t.Error("should show done symbol")
		}
		if !strings.Contains(rendered, "[>] Step 2") {
			t.Error("should show in-progress symbol")
		}
		if !strings.Contains(rendered, "[ ] Step 3") {
			t.Error("should show pending symbol")
		}
	})

	t.Run("includes findings with source", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.AddFinding(context.Background(), Finding{
			Summary: "Bug found",
			Source:  "file.go:42",
		})

		rendered := p.Render()

		if !strings.Contains(rendered, "Key Findings") {
			t.Error("should contain Key Findings section")
		}
		if !strings.Contains(rendered, "`file.go:42`") {
			t.Error("should format source as code")
		}
	})

	t.Run("includes constraints", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.AddConstraint("Must maintain backward compatibility")

		rendered := p.Render()

		if !strings.Contains(rendered, "Constraints") {
			t.Error("should contain Constraints section")
		}
		if !strings.Contains(rendered, "backward compatibility") {
			t.Error("should contain constraint text")
		}
	})

	t.Run("uses cache on second call", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetOriginalQuery("test")

		first := p.Render()
		second := p.Render()

		if first != second {
			t.Error("cached render should be identical")
		}
		if !p.cacheValid {
			t.Error("cache should be valid after render")
		}
	})

	t.Run("nil receiver returns empty string", func(t *testing.T) {
		var p *PinnedInstructions

		rendered := p.Render()

		if rendered != "" {
			t.Error("nil receiver should return empty string")
		}
	})
}

func TestPinnedInstructions_Render_Truncation(t *testing.T) {
	t.Run("truncates findings when over budget", func(t *testing.T) {
		// Use a very small budget and mock token counter that counts 1 token per char
		tc := &mockTokenCounter{tokensPerChar: 1.0} // 1 token per char
		p := NewPinnedInstructions(
			WithTokenBudget(MinPinnedBudget), // Use minimum budget (500)
			WithTokenCounter(tc),
		)
		_ = p.SetOriginalQuery("This is the original query")

		// Add 10 findings with long content to exceed the budget
		for i := 0; i < 10; i++ {
			_ = p.AddFinding(context.Background(), Finding{
				Summary: strings.Repeat("x", MaxFindingSummaryLen-3), // ~97 chars each
				Detail:  strings.Repeat("y", MaxFindingDetailLen-3),  // ~497 chars each
				Source:  "file.go",
			})
		}

		rendered := p.Render()

		// With 500 token budget and ~600 chars per finding (at 1 token/char),
		// we should have fewer than 10 findings
		count := strings.Count(rendered, "file.go")
		if count >= 10 {
			t.Errorf("expected fewer than 10 findings after truncation, got %d (rendered len: %d)", count, len(rendered))
		}
	})

	t.Run("preserves query and plan when truncating findings", func(t *testing.T) {
		tc := &mockTokenCounter{tokensPerChar: 1.0}
		p := NewPinnedInstructions(
			WithTokenBudget(MinPinnedBudget),
			WithTokenCounter(tc),
		)
		_ = p.SetOriginalQuery("Important query")
		_ = p.SetPlan([]PlanStep{{Description: "Step 1", Status: StepPending}})

		// Add many findings to trigger truncation
		for i := 0; i < MaxFindings; i++ {
			_ = p.AddFinding(context.Background(), Finding{
				Summary: strings.Repeat("x", MaxFindingSummaryLen-3),
				Source:  "file.go",
			})
		}

		rendered := p.Render()

		// Query and plan should always be present
		if !strings.Contains(rendered, "Important query") {
			t.Error("original query should always be preserved")
		}
		if !strings.Contains(rendered, "Step 1") {
			t.Error("plan should always be preserved")
		}
	})
}

func TestPinnedInstructions_TokenCount(t *testing.T) {
	t.Run("counts tokens accurately", func(t *testing.T) {
		tc := newMockTokenCounter()
		p := NewPinnedInstructions(WithTokenCounter(tc))
		_ = p.SetOriginalQuery("test query")

		count := p.TokenCount()

		if count <= 0 {
			t.Error("token count should be positive")
		}
	})

	t.Run("uses cache when valid", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetOriginalQuery("test")
		first := p.TokenCount()
		second := p.TokenCount()

		if first != second {
			t.Error("cached count should be consistent")
		}
	})

	t.Run("nil receiver returns 0", func(t *testing.T) {
		var p *PinnedInstructions

		count := p.TokenCount()

		if count != 0 {
			t.Error("nil receiver should return 0")
		}
	})
}

func TestPinnedInstructions_Stats(t *testing.T) {
	t.Run("returns correct counts", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetOriginalQuery("test")
		_ = p.SetPlan([]PlanStep{{Description: "step"}})
		_ = p.AddFinding(context.Background(), Finding{Summary: "finding"})
		_ = p.AddConstraint("constraint")

		stats := p.Stats()

		if stats.PlanStepsCount != 1 {
			t.Errorf("expected 1 plan step, got %d", stats.PlanStepsCount)
		}
		if stats.FindingsCount != 1 {
			t.Errorf("expected 1 finding, got %d", stats.FindingsCount)
		}
		if stats.ConstraintsCount != 1 {
			t.Errorf("expected 1 constraint, got %d", stats.ConstraintsCount)
		}
		if stats.TokenBudget != DefaultPinnedBudget {
			t.Errorf("expected budget %d, got %d", DefaultPinnedBudget, stats.TokenBudget)
		}
	})

	t.Run("nil receiver returns empty stats", func(t *testing.T) {
		var p *PinnedInstructions

		stats := p.Stats()

		if stats.TotalTokens != 0 {
			t.Error("nil receiver should return empty stats")
		}
	})
}

func TestPinnedInstructions_Clear(t *testing.T) {
	t.Run("clears all content", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetOriginalQuery("query")
		_ = p.SetPlan([]PlanStep{{Description: "step"}})
		_ = p.AddFinding(context.Background(), Finding{Summary: "finding"})
		_ = p.AddConstraint("constraint")

		p.Clear()

		if !p.IsEmpty() {
			t.Error("should be empty after clear")
		}
		if p.OriginalQuery() != "" {
			t.Error("query should be cleared")
		}
	})

	t.Run("nil receiver is safe", func(t *testing.T) {
		var p *PinnedInstructions

		// Should not panic
		p.Clear()
	})
}

func TestPinnedInstructions_IsEmpty(t *testing.T) {
	t.Run("true when new", func(t *testing.T) {
		p := NewPinnedInstructions()

		if !p.IsEmpty() {
			t.Error("should be empty when new")
		}
	})

	t.Run("false when has query", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetOriginalQuery("query")

		if p.IsEmpty() {
			t.Error("should not be empty with query")
		}
	})

	t.Run("false when has plan", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetPlan([]PlanStep{{Description: "step"}})

		if p.IsEmpty() {
			t.Error("should not be empty with plan")
		}
	})

	t.Run("nil receiver returns true", func(t *testing.T) {
		var p *PinnedInstructions

		if !p.IsEmpty() {
			t.Error("nil receiver should be empty")
		}
	})
}

func TestPinnedInstructions_JSON(t *testing.T) {
	t.Run("marshal and unmarshal round trip", func(t *testing.T) {
		p := NewPinnedInstructions(WithTokenBudget(3000))
		_ = p.SetOriginalQuery("Fix the bug")
		_ = p.SetPlan([]PlanStep{
			{Description: "Step 1", Status: StepDone},
			{Description: "Step 2", Status: StepPending},
		})
		_ = p.AddFinding(context.Background(), Finding{
			Summary: "Found issue",
			Source:  "file.go:10",
		})
		_ = p.AddConstraint("Must be fast")

		data, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		p2 := NewPinnedInstructions()
		err = json.Unmarshal(data, p2)
		if err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if p2.OriginalQuery() != "Fix the bug" {
			t.Error("query not preserved")
		}
		if len(p2.GetPlan()) != 2 {
			t.Error("plan not preserved")
		}
		if len(p2.GetFindings()) != 1 {
			t.Error("findings not preserved")
		}
		if len(p2.GetConstraints()) != 1 {
			t.Error("constraints not preserved")
		}
		if p2.maxTokenBudget != 3000 {
			t.Error("budget not preserved")
		}
	})

	t.Run("nil marshal returns null", func(t *testing.T) {
		var p *PinnedInstructions

		data, err := p.MarshalJSON()

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if string(data) != "null" {
			t.Errorf("expected null, got %s", data)
		}
	})

	t.Run("nil unmarshal returns error", func(t *testing.T) {
		var p *PinnedInstructions

		err := p.UnmarshalJSON([]byte(`{}`))

		if err != ErrNilPinnedInstructions {
			t.Errorf("expected ErrNilPinnedInstructions, got %v", err)
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		p := NewPinnedInstructions()

		err := p.UnmarshalJSON([]byte(`{invalid`))

		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

func TestPinnedInstructions_ThreadSafety(t *testing.T) {
	t.Run("concurrent reads and writes", func(t *testing.T) {
		p := NewPinnedInstructions()
		_ = p.SetOriginalQuery("initial query")

		var wg sync.WaitGroup
		done := make(chan struct{})

		// Writers
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for {
					select {
					case <-done:
						return
					default:
						_ = p.SetPlan([]PlanStep{{Description: "step"}})
						_ = p.AddFinding(context.Background(), Finding{Summary: "finding"})
						_ = p.AddConstraint("constraint")
						_ = p.UpdateStepStatus(0, StepDone)
					}
				}
			}(i)
		}

		// Readers
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for {
					select {
					case <-done:
						return
					default:
						_ = p.OriginalQuery()
						_ = p.GetPlan()
						_ = p.GetFindings()
						_ = p.GetConstraints()
						_ = p.Render()
						_ = p.TokenCount()
						_ = p.Stats()
						_ = p.IsEmpty()
					}
				}
			}(i)
		}

		// Run for a short time
		time.Sleep(100 * time.Millisecond)
		close(done)
		wg.Wait()
	})
}

func TestStepStatus(t *testing.T) {
	t.Run("String returns correct values", func(t *testing.T) {
		tests := []struct {
			status StepStatus
			want   string
		}{
			{StepPending, "pending"},
			{StepInProgress, "in_progress"},
			{StepDone, "done"},
			{StepSkipped, "skipped"},
		}

		for _, tt := range tests {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("StepStatus.String() = %v, want %v", got, tt.want)
			}
		}
	})

	t.Run("IsValid returns correct values", func(t *testing.T) {
		validStatuses := []StepStatus{StepPending, StepInProgress, StepDone, StepSkipped}
		for _, s := range validStatuses {
			if !s.IsValid() {
				t.Errorf("%s should be valid", s)
			}
		}

		if StepStatus("invalid").IsValid() {
			t.Error("invalid status should not be valid")
		}
	})

	t.Run("Symbol returns correct values", func(t *testing.T) {
		tests := []struct {
			status StepStatus
			want   string
		}{
			{StepPending, "[ ]"},
			{StepInProgress, "[>]"},
			{StepDone, "[x]"},
			{StepSkipped, "[-]"},
			{StepStatus("invalid"), "[?]"},
		}

		for _, tt := range tests {
			if got := tt.status.Symbol(); got != tt.want {
				t.Errorf("StepStatus.Symbol() = %v, want %v", got, tt.want)
			}
		}
	})
}

// Benchmarks

func BenchmarkPinnedInstructions_Render(b *testing.B) {
	p := NewPinnedInstructions()
	_ = p.SetOriginalQuery("Fix the authentication bug in login.go that causes users to be logged out")
	_ = p.SetPlan([]PlanStep{
		{Description: "Read login.go", Status: StepDone},
		{Description: "Read auth_middleware.go", Status: StepDone},
		{Description: "Identify bug location", Status: StepInProgress},
		{Description: "Implement fix", Status: StepPending},
		{Description: "Write tests", Status: StepPending},
	})
	for i := 0; i < 5; i++ {
		_ = p.AddFinding(context.Background(), Finding{
			Summary: "Auth check missing nil guard",
			Source:  "login.go:142",
		})
	}
	_ = p.AddConstraint("Must not modify Session interface")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = p.Render()
	}
}

func BenchmarkPinnedInstructions_Render_Cached(b *testing.B) {
	p := NewPinnedInstructions()
	_ = p.SetOriginalQuery("Fix the bug")
	_ = p.Render() // Warm cache

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = p.Render()
	}
}

func BenchmarkPinnedInstructions_TokenCount(b *testing.B) {
	p := NewPinnedInstructions()
	_ = p.SetOriginalQuery("Fix the authentication bug")
	_ = p.SetPlan([]PlanStep{{Description: "Step 1"}, {Description: "Step 2"}})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = p.TokenCount()
	}
}

// --- Compression Tests ---

func TestPinnedInstructions_Compress(t *testing.T) {
	t.Run("does nothing when under target", func(t *testing.T) {
		p := NewPinnedInstructions(WithTokenBudget(5000))
		_ = p.SetOriginalQuery("Short query")

		freed := p.Compress(1000)

		if freed != 0 {
			t.Errorf("expected 0 tokens freed, got %d", freed)
		}
	})

	t.Run("removes finding details first", func(t *testing.T) {
		tc := &mockTokenCounter{tokensPerChar: 1.0}
		p := NewPinnedInstructions(
			WithTokenBudget(5000),
			WithTokenCounter(tc),
		)
		_ = p.SetOriginalQuery("Query")

		// Add findings with long details
		for i := 0; i < 5; i++ {
			_ = p.AddFinding(context.Background(), Finding{
				Summary: "Summary",
				Detail:  strings.Repeat("detail", 50), // 300 chars
				Source:  "file.go",
			})
		}

		beforeTokens := p.TokenCount()
		freed := p.Compress(beforeTokens / 2)

		if freed <= 0 {
			t.Error("expected tokens to be freed")
		}

		// Verify details are removed
		for _, f := range p.GetFindings() {
			if f.Detail != "" {
				t.Error("details should be removed")
			}
		}
	})

	t.Run("aggregates findings when over 3", func(t *testing.T) {
		tc := &mockTokenCounter{tokensPerChar: 1.0}
		p := NewPinnedInstructions(
			WithTokenBudget(5000),
			WithTokenCounter(tc),
		)
		_ = p.SetOriginalQuery("Query")

		// Add many findings
		for i := 0; i < 8; i++ {
			_ = p.AddFinding(context.Background(), Finding{
				Summary: strings.Repeat("summary", 10),
				Source:  "file.go",
			})
		}

		// Target very low to trigger aggregation
		p.Compress(100)

		findings := p.GetFindings()
		if len(findings) > 3 {
			t.Errorf("expected at most 3 findings after aggregation, got %d", len(findings))
		}

		// Should have an aggregated finding
		hasAggregated := false
		for _, f := range findings {
			if strings.Contains(f.Summary, "Identified") {
				hasAggregated = true
				break
			}
		}
		if !hasAggregated && len(findings) > 0 {
			t.Error("expected aggregated finding")
		}
	})

	t.Run("shortens constraints", func(t *testing.T) {
		tc := &mockTokenCounter{tokensPerChar: 1.0}
		p := NewPinnedInstructions(
			WithTokenBudget(5000),
			WithTokenCounter(tc),
		)
		_ = p.SetOriginalQuery("Query")

		// Add long constraints
		for i := 0; i < 5; i++ {
			_ = p.AddConstraint(strings.Repeat("constraint", 20)) // 200 chars
		}

		p.Compress(200)

		for _, c := range p.GetConstraints() {
			if len(c) > 60 { // 50 + some buffer for "..."
				t.Errorf("constraint should be shortened, got length %d", len(c))
			}
		}
	})

	t.Run("nil receiver returns 0", func(t *testing.T) {
		var p *PinnedInstructions

		freed := p.Compress(100)

		if freed != 0 {
			t.Errorf("nil receiver should return 0, got %d", freed)
		}
	})
}

func TestPinnedInstructions_CompressToFit(t *testing.T) {
	t.Run("does nothing when code budget is satisfied", func(t *testing.T) {
		p := NewPinnedInstructions(WithTokenBudget(1000))
		_ = p.SetOriginalQuery("Short query")

		// Total 100K, trace 50K, pinned ~50 tokens = plenty of code budget
		freed := p.CompressToFit(100000, 50000)

		if freed != 0 {
			t.Errorf("expected 0 tokens freed, got %d", freed)
		}
	})

	t.Run("compresses when code budget is squeezed", func(t *testing.T) {
		tc := &mockTokenCounter{tokensPerChar: 1.0}
		p := NewPinnedInstructions(
			WithTokenBudget(5000),
			WithTokenCounter(tc),
		)
		_ = p.SetOriginalQuery("Query")

		// Add a lot of content to the pinned block
		for i := 0; i < 10; i++ {
			_ = p.AddFinding(context.Background(), Finding{
				Summary: strings.Repeat("x", 90),
				Detail:  strings.Repeat("y", 400),
				Source:  "file.go",
			})
		}

		// Total 10K, trace 6K, pinned is ~5K = only 4K available
		// MinCodeBudget is 4096, so we're right at the edge
		initialTokens := p.TokenCount()

		// Total 8K, trace 4K, pinned ~5K = need to shrink pinned
		freed := p.CompressToFit(8000, 4000)

		if freed <= 0 && initialTokens > 1000 {
			t.Error("expected tokens to be freed when code budget is squeezed")
		}
	})

	t.Run("respects minimum pinned budget", func(t *testing.T) {
		tc := &mockTokenCounter{tokensPerChar: 1.0}
		p := NewPinnedInstructions(
			WithTokenBudget(5000),
			WithTokenCounter(tc),
		)
		_ = p.SetOriginalQuery("Query")
		_ = p.SetPlan([]PlanStep{{Description: "Step 1"}})

		// Very constrained: total 1K, trace 500
		// Can't compress pinned below minimum
		p.CompressToFit(1000, 500)

		// Should still have core content
		if p.OriginalQuery() == "" {
			t.Error("should preserve original query")
		}
	})

	t.Run("nil receiver returns 0", func(t *testing.T) {
		var p *PinnedInstructions

		freed := p.CompressToFit(100000, 50000)

		if freed != 0 {
			t.Errorf("nil receiver should return 0, got %d", freed)
		}
	})
}

func TestMinCodeBudget_Constant(t *testing.T) {
	// Verify the constant is reasonable
	if MinCodeBudget < 2048 {
		t.Errorf("MinCodeBudget should be at least 2048, got %d", MinCodeBudget)
	}
	if MinCodeBudget > 8192 {
		t.Errorf("MinCodeBudget should not exceed 8192, got %d", MinCodeBudget)
	}
}
