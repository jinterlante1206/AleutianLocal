// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package crs

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// Clause Tests
// -----------------------------------------------------------------------------

func TestClause_IsSatisfied(t *testing.T) {
	tests := []struct {
		name       string
		clause     Clause
		assignment map[string]bool
		want       bool
	}{
		{
			name: "clause satisfied by positive literal",
			clause: Clause{
				ID: "c1",
				Literals: []Literal{
					{Variable: "tool:list_packages", Negated: false},
				},
			},
			assignment: map[string]bool{"tool:list_packages": true},
			want:       true,
		},
		{
			name: "clause satisfied by negated literal",
			clause: Clause{
				ID: "c2",
				Literals: []Literal{
					{Variable: "tool:list_packages", Negated: true},
				},
			},
			assignment: map[string]bool{"tool:list_packages": false},
			want:       true,
		},
		{
			name: "clause not satisfied",
			clause: Clause{
				ID: "c3",
				Literals: []Literal{
					{Variable: "tool:list_packages", Negated: true},
				},
			},
			assignment: map[string]bool{"tool:list_packages": true},
			want:       false,
		},
		{
			name: "disjunction - one literal true satisfies",
			clause: Clause{
				ID: "c4",
				Literals: []Literal{
					{Variable: "tool:A", Negated: true},
					{Variable: "tool:B", Negated: false},
				},
			},
			assignment: map[string]bool{"tool:A": true, "tool:B": true},
			want:       true, // tool:B is true, so clause is satisfied
		},
		{
			name: "unassigned variable doesn't satisfy",
			clause: Clause{
				ID: "c5",
				Literals: []Literal{
					{Variable: "tool:unknown", Negated: false},
				},
			},
			assignment: map[string]bool{},
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.clause.IsSatisfied(tt.assignment)
			if got != tt.want {
				t.Errorf("IsSatisfied() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClause_IsViolated(t *testing.T) {
	tests := []struct {
		name       string
		clause     Clause
		assignment map[string]bool
		want       bool
	}{
		{
			name: "clause violated when all literals false",
			clause: Clause{
				ID: "c1",
				Literals: []Literal{
					{Variable: "tool:A", Negated: true},  // ¬A - needs A=false to be true
					{Variable: "tool:B", Negated: false}, // B - needs B=true to be true
				},
			},
			assignment: map[string]bool{"tool:A": true, "tool:B": false},
			want:       true, // ¬A is false (A=true), B is false, so clause is violated
		},
		{
			name: "clause not violated when one literal true",
			clause: Clause{
				ID: "c2",
				Literals: []Literal{
					{Variable: "tool:A", Negated: true},
					{Variable: "tool:B", Negated: false},
				},
			},
			assignment: map[string]bool{"tool:A": true, "tool:B": true},
			want:       false, // B is true, so clause is not violated
		},
		{
			name: "clause not violated when variable unassigned",
			clause: Clause{
				ID: "c3",
				Literals: []Literal{
					{Variable: "tool:A", Negated: true},
				},
			},
			assignment: map[string]bool{}, // A not assigned
			want:       false,             // Cannot be violated yet
		},
		{
			name: "empty clause is always violated",
			clause: Clause{
				ID:       "c4",
				Literals: []Literal{},
			},
			assignment: map[string]bool{},
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.clause.IsViolated(tt.assignment)
			if got != tt.want {
				t.Errorf("IsViolated() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClause_String(t *testing.T) {
	clause := Clause{
		ID: "c1",
		Literals: []Literal{
			{Variable: "tool:A", Negated: true},
			{Variable: "outcome:success", Negated: false},
		},
	}

	got := clause.String()
	if got != "(¬tool:A ∨ outcome:success)" {
		t.Errorf("String() = %q, want %q", got, "(¬tool:A ∨ outcome:success)")
	}
}

func TestClause_Validate(t *testing.T) {
	tests := []struct {
		name    string
		clause  Clause
		wantErr bool
	}{
		{
			name: "valid clause",
			clause: Clause{
				ID:       "c1",
				Literals: []Literal{{Variable: "tool:A", Negated: true}},
				Source:   SignalSourceHard,
			},
			wantErr: false,
		},
		{
			name: "empty ID fails",
			clause: Clause{
				ID:       "",
				Literals: []Literal{{Variable: "tool:A", Negated: true}},
				Source:   SignalSourceHard,
			},
			wantErr: true,
		},
		{
			name: "no literals fails",
			clause: Clause{
				ID:       "c1",
				Literals: []Literal{},
				Source:   SignalSourceHard,
			},
			wantErr: true,
		},
		{
			name: "soft source fails",
			clause: Clause{
				ID:       "c1",
				Literals: []Literal{{Variable: "tool:A", Negated: true}},
				Source:   SignalSourceSoft,
			},
			wantErr: true,
		},
		{
			name: "empty literal variable fails (CR-4)",
			clause: Clause{
				ID:       "c1",
				Literals: []Literal{{Variable: "", Negated: true}},
				Source:   SignalSourceHard,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.clause.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// FailureEvent Tests
// -----------------------------------------------------------------------------

func TestFailureEvent_Validate(t *testing.T) {
	tests := []struct {
		name    string
		event   FailureEvent
		wantErr bool
	}{
		{
			name: "valid event",
			event: FailureEvent{
				SessionID:   "session-1",
				FailureType: FailureTypeCycleDetected,
				Source:      SignalSourceHard,
			},
			wantErr: false,
		},
		{
			name: "empty session ID fails",
			event: FailureEvent{
				SessionID:   "",
				FailureType: FailureTypeCycleDetected,
				Source:      SignalSourceHard,
			},
			wantErr: true,
		},
		{
			name: "invalid failure type fails",
			event: FailureEvent{
				SessionID:   "session-1",
				FailureType: "invalid",
				Source:      SignalSourceHard,
			},
			wantErr: true,
		},
		{
			name: "invalid source fails",
			event: FailureEvent{
				SessionID:   "session-1",
				FailureType: FailureTypeCycleDetected,
				Source:      SignalSourceUnknown,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// CRS ClauseIndex Tests
// -----------------------------------------------------------------------------

func TestCRS_AddClause(t *testing.T) {
	ctx := context.Background()
	crsInstance := New(nil)

	// Add a valid clause
	clause := &Clause{
		ID:          "test-clause-1",
		Literals:    []Literal{{Variable: "tool:A", Negated: true}},
		Source:      SignalSourceHard,
		FailureType: FailureTypeCycleDetected,
	}

	err := crsInstance.AddClause(ctx, clause)
	if err != nil {
		t.Errorf("AddClause() error = %v", err)
	}

	// Verify clause was added
	snapshot := crsInstance.Snapshot()
	if snapshot.ConstraintIndex().ClauseCount() != 1 {
		t.Errorf("ClauseCount() = %d, want 1", snapshot.ConstraintIndex().ClauseCount())
	}
}

func TestCRS_AddClause_Duplicate(t *testing.T) {
	ctx := context.Background()
	crsInstance := New(nil)

	// Add a clause
	clause1 := &Clause{
		ID:          "test-clause-1",
		Literals:    []Literal{{Variable: "tool:A", Negated: true}},
		Source:      SignalSourceHard,
		FailureType: FailureTypeCycleDetected,
	}
	err := crsInstance.AddClause(ctx, clause1)
	if err != nil {
		t.Fatalf("AddClause(1) error = %v", err)
	}

	// Add a semantic duplicate (same literals, different ID)
	clause2 := &Clause{
		ID:          "test-clause-2",
		Literals:    []Literal{{Variable: "tool:A", Negated: true}},
		Source:      SignalSourceHard,
		FailureType: FailureTypeCycleDetected,
	}
	err = crsInstance.AddClause(ctx, clause2)
	if err != nil {
		t.Fatalf("AddClause(2) error = %v", err)
	}

	// Should still have only 1 clause (duplicate detection)
	snapshot := crsInstance.Snapshot()
	if snapshot.ConstraintIndex().ClauseCount() != 1 {
		t.Errorf("ClauseCount() = %d, want 1 (duplicate should be merged)", snapshot.ConstraintIndex().ClauseCount())
	}
}

func TestCRS_AddClause_InvalidClause(t *testing.T) {
	ctx := context.Background()
	crsInstance := New(nil)

	// Try to add an invalid clause (soft source)
	clause := &Clause{
		ID:          "test-clause-1",
		Literals:    []Literal{{Variable: "tool:A", Negated: true}},
		Source:      SignalSourceSoft, // Invalid for CDCL
		FailureType: FailureTypeCycleDetected,
	}

	err := crsInstance.AddClause(ctx, clause)
	if err == nil {
		t.Error("AddClause() should fail for soft source clause")
	}
}

func TestCRS_CheckDecisionAllowed(t *testing.T) {
	ctx := context.Background()
	crsInstance := New(nil)

	sessionID := "test-session"

	// Add some step history
	for i := 1; i <= 3; i++ {
		_ = crsInstance.RecordStep(ctx, StepRecord{
			StepNumber: i,
			SessionID:  sessionID,
			Actor:      ActorMainAgent,
			Decision:   DecisionExecuteTool,
			Tool:       "tool_A",
			Outcome:    OutcomeSuccess,
			Timestamp:  time.Now().UnixMilli(),
		})
	}

	// Add a clause that blocks tool_A after prev_tool:tool_A
	clause := &Clause{
		ID: "block-repeat",
		Literals: []Literal{
			{Variable: "tool:tool_A", Negated: true},
			{Variable: "prev_tool:tool_A", Negated: true},
		},
		Source:      SignalSourceHard,
		FailureType: FailureTypeCycleDetected,
	}
	err := crsInstance.AddClause(ctx, clause)
	if err != nil {
		t.Fatalf("AddClause() error = %v", err)
	}

	// Check if tool_A is allowed (should be blocked because prev_tool is tool_A)
	allowed, reason := crsInstance.CheckDecisionAllowed(sessionID, "tool_A")
	if allowed {
		t.Error("CheckDecisionAllowed() should block tool_A due to learned clause")
	}
	if reason == "" {
		t.Error("CheckDecisionAllowed() should provide a reason when blocked")
	}

	// Check if tool_B is allowed (should be allowed)
	allowed, reason = crsInstance.CheckDecisionAllowed(sessionID, "tool_B")
	if !allowed {
		t.Errorf("CheckDecisionAllowed() should allow tool_B, blocked with reason: %s", reason)
	}
}

func TestCRS_GarbageCollectClauses(t *testing.T) {
	// Create CRS with short TTL for testing
	crsInstance := New(nil).(*crsImpl)
	crsInstance.clauseConfig = ClausePersistence{
		Scope:      ClauseScopeSession,
		TTL:        1 * time.Millisecond, // Very short TTL
		MaxClauses: 1000,
	}

	// Add a clause
	clause := &Clause{
		ID:          "test-clause-1",
		Literals:    []Literal{{Variable: "tool:A", Negated: true}},
		Source:      SignalSourceHard,
		FailureType: FailureTypeCycleDetected,
		LearnedAt:   time.Now().Add(-1 * time.Hour).UnixMilli(), // Old clause
	}

	// Directly add to clauseData to bypass LearnedAt update
	crsInstance.mu.Lock()
	crsInstance.clauseData[clause.ID] = clause
	crsInstance.mu.Unlock()

	// Wait for TTL
	time.Sleep(10 * time.Millisecond)

	// Garbage collect
	removed := crsInstance.GarbageCollectClauses()
	if removed != 1 {
		t.Errorf("GarbageCollectClauses() = %d, want 1", removed)
	}

	// Verify clause was removed
	snapshot := crsInstance.Snapshot()
	if snapshot.ConstraintIndex().ClauseCount() != 0 {
		t.Errorf("ClauseCount() = %d, want 0 after GC", snapshot.ConstraintIndex().ClauseCount())
	}
}

func TestCRS_ClauseLRUEviction(t *testing.T) {
	ctx := context.Background()

	// Create CRS with small MaxClauses for testing
	crsInstance := New(nil).(*crsImpl)
	crsInstance.clauseConfig = ClausePersistence{
		Scope:      ClauseScopeSession,
		TTL:        24 * time.Hour,
		MaxClauses: 2, // Only allow 2 clauses
	}

	// Add 3 clauses (should trigger LRU eviction)
	// CR-9: Use fmt.Sprintf for correct ID generation
	for i := 1; i <= 3; i++ {
		clause := &Clause{
			ID:          fmt.Sprintf("test-clause-%d", i),
			Literals:    []Literal{{Variable: fmt.Sprintf("tool:%c", 'A'+i-1), Negated: true}},
			Source:      SignalSourceHard,
			FailureType: FailureTypeCycleDetected,
		}
		err := crsInstance.AddClause(ctx, clause)
		if err != nil {
			t.Fatalf("AddClause(%d) error = %v", i, err)
		}
		// Small delay to ensure different LearnedAt times
		time.Sleep(1 * time.Millisecond)
	}

	// Should have only 2 clauses due to LRU eviction
	snapshot := crsInstance.Snapshot()
	if snapshot.ConstraintIndex().ClauseCount() != 2 {
		t.Errorf("ClauseCount() = %d, want 2 (LRU eviction)", snapshot.ConstraintIndex().ClauseCount())
	}
}

// -----------------------------------------------------------------------------
// ConstraintIndexView Clause Tests
// -----------------------------------------------------------------------------

func TestConstraintIndexView_CheckAssignment(t *testing.T) {
	crsInstance := New(nil)
	ctx := context.Background()

	// Add a clause: ¬tool:A ∨ ¬outcome:success
	// This is violated when tool:A=true AND outcome:success=true
	clause := &Clause{
		ID: "test-clause",
		Literals: []Literal{
			{Variable: "tool:A", Negated: true},
			{Variable: "outcome:success", Negated: true},
		},
		Source:      SignalSourceHard,
		FailureType: FailureTypeCircuitBreaker,
	}
	err := crsInstance.AddClause(ctx, clause)
	if err != nil {
		t.Fatalf("AddClause() error = %v", err)
	}

	snapshot := crsInstance.Snapshot()

	// Test 1: Assignment that violates the clause
	violatingAssignment := map[string]bool{
		"tool:A":          true,
		"outcome:success": true,
	}
	result := snapshot.ConstraintIndex().CheckAssignment(violatingAssignment)
	if !result.Conflict {
		t.Error("CheckAssignment() should detect conflict for violating assignment")
	}
	if result.ViolatedClause == nil {
		t.Error("CheckAssignment() should return the violated clause")
	}

	// Test 2: Assignment that doesn't violate
	safeAssignment := map[string]bool{
		"tool:A":          false, // ¬tool:A is true, so clause is satisfied
		"outcome:success": true,
	}
	result = snapshot.ConstraintIndex().CheckAssignment(safeAssignment)
	if result.Conflict {
		t.Error("CheckAssignment() should not detect conflict for safe assignment")
	}
}

// -----------------------------------------------------------------------------
// Literal Tests
// -----------------------------------------------------------------------------

func TestLiteral_String(t *testing.T) {
	tests := []struct {
		literal Literal
		want    string
	}{
		{Literal{Variable: "tool:A", Negated: false}, "tool:A"},
		{Literal{Variable: "tool:A", Negated: true}, "¬tool:A"},
		{Literal{Variable: "outcome:success", Negated: false}, "outcome:success"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.literal.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// FailureType Tests
// -----------------------------------------------------------------------------

func TestFailureType_IsValid(t *testing.T) {
	validTypes := []FailureType{
		FailureTypeToolError,
		FailureTypeCycleDetected,
		FailureTypeCircuitBreaker,
		FailureTypeTimeout,
		FailureTypeInvalidOutput,
		FailureTypeSafety,
	}

	for _, ft := range validTypes {
		if !ft.IsValid() {
			t.Errorf("FailureType %q should be valid", ft)
		}
	}

	invalidType := FailureType("invalid")
	if invalidType.IsValid() {
		t.Error("FailureType 'invalid' should not be valid")
	}
}

// -----------------------------------------------------------------------------
// Checkpoint/Restore Tests (CR-11)
// -----------------------------------------------------------------------------

func TestCRS_ClauseRestoreFromCheckpoint(t *testing.T) {
	ctx := context.Background()
	crsInstance := New(nil)

	// Add a clause
	clause := &Clause{
		ID:          "checkpoint-test-clause",
		Literals:    []Literal{{Variable: "tool:A", Negated: true}},
		Source:      SignalSourceHard,
		FailureType: FailureTypeCycleDetected,
	}
	err := crsInstance.AddClause(ctx, clause)
	if err != nil {
		t.Fatalf("AddClause() error = %v", err)
	}

	// Create checkpoint
	checkpoint, err := crsInstance.Checkpoint(ctx)
	if err != nil {
		t.Fatalf("Checkpoint() error = %v", err)
	}

	// Verify clause exists before modification
	snapshot1 := crsInstance.Snapshot()
	if snapshot1.ConstraintIndex().ClauseCount() != 1 {
		t.Errorf("ClauseCount() before = %d, want 1", snapshot1.ConstraintIndex().ClauseCount())
	}

	// Add another clause
	clause2 := &Clause{
		ID:          "post-checkpoint-clause",
		Literals:    []Literal{{Variable: "tool:B", Negated: true}},
		Source:      SignalSourceHard,
		FailureType: FailureTypeToolError,
	}
	err = crsInstance.AddClause(ctx, clause2)
	if err != nil {
		t.Fatalf("AddClause(2) error = %v", err)
	}

	// Verify 2 clauses now
	snapshot2 := crsInstance.Snapshot()
	if snapshot2.ConstraintIndex().ClauseCount() != 2 {
		t.Errorf("ClauseCount() after = %d, want 2", snapshot2.ConstraintIndex().ClauseCount())
	}

	// Restore to checkpoint
	err = crsInstance.Restore(ctx, checkpoint)
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}

	// Verify only 1 clause after restore
	snapshot3 := crsInstance.Snapshot()
	if snapshot3.ConstraintIndex().ClauseCount() != 1 {
		t.Errorf("ClauseCount() after restore = %d, want 1", snapshot3.ConstraintIndex().ClauseCount())
	}

	// Verify it's the original clause
	restoredClause, ok := snapshot3.ConstraintIndex().GetClause("checkpoint-test-clause")
	if !ok {
		t.Error("GetClause() should find the restored clause")
	}
	if restoredClause == nil {
		t.Error("restored clause should not be nil")
	}
	if restoredClause.ID != "checkpoint-test-clause" {
		t.Errorf("restored clause ID = %q, want %q", restoredClause.ID, "checkpoint-test-clause")
	}
}
