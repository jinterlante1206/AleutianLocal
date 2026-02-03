// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package diff

import (
	"testing"
)

func TestLineType_String(t *testing.T) {
	tests := []struct {
		name string
		lt   LineType
		want string
	}{
		{"context", LineContext, " "},
		{"added", LineAdded, "+"},
		{"removed", LineRemoved, "-"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.lt.String(); got != tt.want {
				t.Errorf("LineType.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChangeRisk_String(t *testing.T) {
	tests := []struct {
		risk ChangeRisk
		want string
	}{
		{RiskLow, "low"},
		{RiskMedium, "medium"},
		{RiskHigh, "high"},
		{RiskCritical, "critical"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.risk.String(); got != tt.want {
				t.Errorf("ChangeRisk.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHunkStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		status   HunkStatus
		terminal bool
	}{
		{HunkPending, false},
		{HunkAccepted, true},
		{HunkRejected, true},
		{HunkEdited, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsTerminal(); got != tt.terminal {
				t.Errorf("HunkStatus.IsTerminal() = %v, want %v", got, tt.terminal)
			}
		})
	}
}

func TestDiffLine_TypeChecks(t *testing.T) {
	t.Run("addition", func(t *testing.T) {
		line := DiffLine{Type: LineAdded, Content: "new line"}
		if !line.IsAddition() {
			t.Error("Expected IsAddition() to be true")
		}
		if line.IsDeletion() {
			t.Error("Expected IsDeletion() to be false")
		}
		if line.IsContext() {
			t.Error("Expected IsContext() to be false")
		}
	})

	t.Run("deletion", func(t *testing.T) {
		line := DiffLine{Type: LineRemoved, Content: "old line"}
		if line.IsAddition() {
			t.Error("Expected IsAddition() to be false")
		}
		if !line.IsDeletion() {
			t.Error("Expected IsDeletion() to be true")
		}
		if line.IsContext() {
			t.Error("Expected IsContext() to be false")
		}
	})

	t.Run("context", func(t *testing.T) {
		line := DiffLine{Type: LineContext, Content: "unchanged"}
		if line.IsAddition() {
			t.Error("Expected IsAddition() to be false")
		}
		if line.IsDeletion() {
			t.Error("Expected IsDeletion() to be false")
		}
		if !line.IsContext() {
			t.Error("Expected IsContext() to be true")
		}
	})
}

func TestDiffLine_String(t *testing.T) {
	line := DiffLine{Type: LineAdded, Content: "hello world"}
	want := "+hello world"
	if got := line.String(); got != want {
		t.Errorf("DiffLine.String() = %q, want %q", got, want)
	}
}

func TestHunk_Header(t *testing.T) {
	hunk := &Hunk{
		OldStart: 10,
		OldCount: 5,
		NewStart: 12,
		NewCount: 8,
	}

	want := "@@ -10,5 +12,8 @@"
	if got := hunk.Header(); got != want {
		t.Errorf("Hunk.Header() = %q, want %q", got, want)
	}
}

func TestHunk_Counts(t *testing.T) {
	hunk := &Hunk{
		Lines: []DiffLine{
			{Type: LineContext, Content: "context1"},
			{Type: LineRemoved, Content: "old1"},
			{Type: LineRemoved, Content: "old2"},
			{Type: LineAdded, Content: "new1"},
			{Type: LineContext, Content: "context2"},
		},
	}

	if got := hunk.AddedCount(); got != 1 {
		t.Errorf("Hunk.AddedCount() = %d, want 1", got)
	}
	if got := hunk.RemovedCount(); got != 2 {
		t.Errorf("Hunk.RemovedCount() = %d, want 2", got)
	}
}

func TestHunk_EffectiveLines(t *testing.T) {
	t.Run("returns_original_when_not_edited", func(t *testing.T) {
		original := []DiffLine{{Type: LineAdded, Content: "line1"}}
		hunk := &Hunk{
			Lines:  original,
			Status: HunkAccepted,
		}

		got := hunk.EffectiveLines()
		if len(got) != len(original) {
			t.Errorf("Expected original lines, got different length")
		}
	})

	t.Run("returns_edited_when_edited", func(t *testing.T) {
		original := []DiffLine{{Type: LineAdded, Content: "line1"}}
		edited := []DiffLine{{Type: LineAdded, Content: "modified"}}
		hunk := &Hunk{
			Lines:       original,
			EditedLines: edited,
			Status:      HunkEdited,
		}

		got := hunk.EffectiveLines()
		if len(got) != len(edited) || got[0].Content != "modified" {
			t.Errorf("Expected edited lines, got %v", got)
		}
	})
}

func TestProposedChange_Stats(t *testing.T) {
	change := &ProposedChange{
		Hunks: []*Hunk{
			{
				Lines: []DiffLine{
					{Type: LineAdded, Content: "a"},
					{Type: LineAdded, Content: "b"},
					{Type: LineRemoved, Content: "c"},
				},
			},
			{
				Lines: []DiffLine{
					{Type: LineAdded, Content: "d"},
				},
			},
		},
	}

	want := "+3 -1"
	if got := change.Stats(); got != want {
		t.Errorf("ProposedChange.Stats() = %q, want %q", got, want)
	}
}

func TestProposedChange_StatusChecks(t *testing.T) {
	change := &ProposedChange{
		Hunks: []*Hunk{
			{Status: HunkAccepted},
			{Status: HunkPending},
			{Status: HunkRejected},
		},
	}

	t.Run("AllAccepted", func(t *testing.T) {
		if change.AllAccepted() {
			t.Error("Expected AllAccepted() to be false")
		}
	})

	t.Run("AllRejected", func(t *testing.T) {
		if change.AllRejected() {
			t.Error("Expected AllRejected() to be false")
		}
	})

	t.Run("AnyPending", func(t *testing.T) {
		if !change.AnyPending() {
			t.Error("Expected AnyPending() to be true")
		}
	})

	t.Run("Counts", func(t *testing.T) {
		if got := change.PendingCount(); got != 1 {
			t.Errorf("PendingCount() = %d, want 1", got)
		}
		if got := change.AcceptedCount(); got != 1 {
			t.Errorf("AcceptedCount() = %d, want 1", got)
		}
		if got := change.RejectedCount(); got != 1 {
			t.Errorf("RejectedCount() = %d, want 1", got)
		}
	})
}

func TestDecisionAction_IsTerminal(t *testing.T) {
	tests := []struct {
		action   DecisionAction
		terminal bool
	}{
		{DecisionPending, false},
		{DecisionSkip, false},
		{DecisionAccept, true},
		{DecisionReject, true},
		{DecisionEdit, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			if got := tt.action.IsTerminal(); got != tt.terminal {
				t.Errorf("DecisionAction.IsTerminal() = %v, want %v", got, tt.terminal)
			}
		})
	}
}

func TestReviewResult(t *testing.T) {
	result := NewReviewResult()

	result.Decisions["file1.go"] = &FileDecision{
		FilePath: "file1.go",
		Action:   DecisionAccept,
	}
	result.Decisions["file2.go"] = &FileDecision{
		FilePath: "file2.go",
		Action:   DecisionReject,
	}
	result.Decisions["file3.go"] = &FileDecision{
		FilePath: "file3.go",
		Action:   DecisionPending,
	}
	result.Decisions["file4.go"] = &FileDecision{
		FilePath: "file4.go",
		Action:   DecisionEdit,
	}

	t.Run("AcceptedFiles", func(t *testing.T) {
		accepted := result.AcceptedFiles()
		if len(accepted) != 2 {
			t.Errorf("Expected 2 accepted files, got %d", len(accepted))
		}
	})

	t.Run("RejectedFiles", func(t *testing.T) {
		rejected := result.RejectedFiles()
		if len(rejected) != 1 {
			t.Errorf("Expected 1 rejected file, got %d", len(rejected))
		}
	})

	t.Run("PendingFiles", func(t *testing.T) {
		pending := result.PendingFiles()
		if len(pending) != 1 {
			t.Errorf("Expected 1 pending file, got %d", len(pending))
		}
	})

	t.Run("AllDecided", func(t *testing.T) {
		if result.AllDecided() {
			t.Error("Expected AllDecided() to be false")
		}
	})

	t.Run("Summary", func(t *testing.T) {
		summary := result.Summary()
		if summary == "" {
			t.Error("Expected non-empty summary")
		}
	})
}

func TestReviewResult_Cancelled(t *testing.T) {
	result := NewReviewResult()
	result.Cancelled = true
	result.CancelReason = "user abort"

	summary := result.Summary()
	if summary != "Review cancelled: user abort" {
		t.Errorf("Unexpected summary: %q", summary)
	}
}
