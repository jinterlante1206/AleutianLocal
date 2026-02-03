// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package tui

import (
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/trace/diff"
	tea "github.com/charmbracelet/bubbletea"
)

func createTestChanges() []*diff.ProposedChange {
	return []*diff.ProposedChange{
		{
			FilePath:  "file1.go",
			Rationale: "First change",
			Risk:      diff.RiskLow,
			Hunks: []*diff.Hunk{
				{
					OldStart: 1,
					OldCount: 3,
					NewStart: 1,
					NewCount: 4,
					Status:   diff.HunkPending,
					Lines: []diff.DiffLine{
						{Type: diff.LineContext, Content: "context", OldNum: 1, NewNum: 1},
						{Type: diff.LineRemoved, Content: "old", OldNum: 2},
						{Type: diff.LineAdded, Content: "new", NewNum: 2},
						{Type: diff.LineContext, Content: "more", OldNum: 3, NewNum: 3},
					},
				},
			},
		},
		{
			FilePath:  "file2.go",
			Rationale: "Second change",
			Risk:      diff.RiskMedium,
			Hunks: []*diff.Hunk{
				{
					OldStart: 10,
					OldCount: 2,
					NewStart: 10,
					NewCount: 3,
					Status:   diff.HunkPending,
					Lines: []diff.DiffLine{
						{Type: diff.LineContext, Content: "ctx", OldNum: 10, NewNum: 10},
						{Type: diff.LineAdded, Content: "added", NewNum: 11},
						{Type: diff.LineContext, Content: "ctx2", OldNum: 11, NewNum: 12},
					},
				},
			},
		},
	}
}

func TestNewDiffReviewModel(t *testing.T) {
	changes := createTestChanges()
	config := DefaultDiffReviewConfig()

	model := NewDiffReviewModel(changes, config)

	if len(model.changes) != 2 {
		t.Errorf("Expected 2 changes, got %d", len(model.changes))
	}

	if len(model.decisions) != 2 {
		t.Errorf("Expected 2 decisions, got %d", len(model.decisions))
	}

	// Check initial state
	if model.currentFile != 0 {
		t.Errorf("Expected currentFile = 0, got %d", model.currentFile)
	}
	if model.currentHunk != 0 {
		t.Errorf("Expected currentHunk = 0, got %d", model.currentHunk)
	}
	if model.viewMode != ViewFile {
		t.Errorf("Expected viewMode = ViewFile, got %v", model.viewMode)
	}
	if model.result == nil {
		t.Error("Expected result to be initialized")
	}
}

func TestDefaultDiffReviewConfig(t *testing.T) {
	config := DefaultDiffReviewConfig()

	if config.ConfirmAcceptAll != true {
		t.Error("Expected ConfirmAcceptAll = true")
	}
	if config.ShowLineNumbers != true {
		t.Error("Expected ShowLineNumbers = true")
	}
	if config.ContextLines != 3 {
		t.Errorf("Expected ContextLines = 3, got %d", config.ContextLines)
	}
}

func TestDiffReviewModel_AcceptCurrent(t *testing.T) {
	changes := createTestChanges()
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())

	// Initialize viewport (simulating window size)
	model.width = 80
	model.height = 24
	model.ready = true

	// Accept current file
	model.acceptCurrent()

	decision := model.decisions["file1.go"]
	if decision.Action != diff.DecisionAccept {
		t.Errorf("Expected Action = DecisionAccept, got %v", decision.Action)
	}

	// Check hunks are marked accepted
	for i, hunk := range changes[0].Hunks {
		if hunk.Status != diff.HunkAccepted {
			t.Errorf("Hunk %d status = %v, want HunkAccepted", i, hunk.Status)
		}
	}
}

func TestDiffReviewModel_RejectCurrent(t *testing.T) {
	changes := createTestChanges()
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())
	model.ready = true

	// Reject current file
	model.rejectCurrent()

	decision := model.decisions["file1.go"]
	if decision.Action != diff.DecisionReject {
		t.Errorf("Expected Action = DecisionReject, got %v", decision.Action)
	}

	// Check hunks are marked rejected
	for i, hunk := range changes[0].Hunks {
		if hunk.Status != diff.HunkRejected {
			t.Errorf("Hunk %d status = %v, want HunkRejected", i, hunk.Status)
		}
	}
}

func TestDiffReviewModel_SkipCurrent(t *testing.T) {
	changes := createTestChanges()
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())
	model.ready = true

	model.skipCurrent()

	decision := model.decisions["file1.go"]
	if decision.Action != diff.DecisionSkip {
		t.Errorf("Expected Action = DecisionSkip, got %v", decision.Action)
	}
}

func TestDiffReviewModel_AcceptAllRemaining(t *testing.T) {
	changes := createTestChanges()
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())
	model.ready = true

	// Reject first file, then accept all remaining
	model.rejectCurrent()
	model.acceptAllRemaining()

	// First file should still be rejected
	if model.decisions["file1.go"].Action != diff.DecisionReject {
		t.Error("First file should remain rejected")
	}

	// Second file should be accepted
	if model.decisions["file2.go"].Action != diff.DecisionAccept {
		t.Error("Second file should be accepted")
	}
}

func TestDiffReviewModel_ToggleViewMode(t *testing.T) {
	changes := createTestChanges()
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())

	if model.viewMode != ViewFile {
		t.Error("Initial view mode should be ViewFile")
	}

	model.toggleViewMode()
	if model.viewMode != ViewHunk {
		t.Error("After first toggle, should be ViewHunk")
	}

	model.toggleViewMode()
	if model.viewMode != ViewSummary {
		t.Error("After second toggle, should be ViewSummary")
	}

	model.toggleViewMode()
	if model.viewMode != ViewFile {
		t.Error("After third toggle, should be back to ViewFile")
	}
}

func TestDiffReviewModel_Navigation(t *testing.T) {
	changes := createTestChanges()
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())
	model.ready = true

	// Initial position
	if model.currentFile != 0 {
		t.Errorf("Initial currentFile = %d, want 0", model.currentFile)
	}

	// Navigate next
	model, _ = model.nextFile()
	if model.currentFile != 1 {
		t.Errorf("After nextFile, currentFile = %d, want 1", model.currentFile)
	}

	// Navigate past end (should stay)
	model, _ = model.nextFile()
	if model.currentFile != 1 {
		t.Errorf("After extra nextFile, currentFile = %d, want 1", model.currentFile)
	}

	// Navigate prev
	model, _ = model.prevFile()
	if model.currentFile != 0 {
		t.Errorf("After prevFile, currentFile = %d, want 0", model.currentFile)
	}

	// Navigate before start (should stay)
	model, _ = model.prevFile()
	if model.currentFile != 0 {
		t.Errorf("After extra prevFile, currentFile = %d, want 0", model.currentFile)
	}
}

func TestDiffReviewModel_AdvanceFile(t *testing.T) {
	changes := createTestChanges()
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())
	model.ready = true
	model.width = 80
	model.height = 24

	// Accept first file and advance
	model.acceptCurrent()
	model, _ = model.advanceFile()

	// Should move to next pending file
	if model.currentFile != 1 {
		t.Errorf("After advanceFile, currentFile = %d, want 1", model.currentFile)
	}

	// Accept second file and advance
	model.acceptCurrent()
	model, _ = model.advanceFile()

	// Should switch to summary view when no more pending
	if model.viewMode != ViewSummary {
		t.Errorf("After all files accepted, viewMode = %v, want ViewSummary", model.viewMode)
	}
}

func TestDiffReviewModel_HunkMode(t *testing.T) {
	changes := []*diff.ProposedChange{
		{
			FilePath: "multi_hunk.go",
			Hunks: []*diff.Hunk{
				{Status: diff.HunkPending, OldStart: 1, OldCount: 1, NewStart: 1, NewCount: 1},
				{Status: diff.HunkPending, OldStart: 10, OldCount: 1, NewStart: 10, NewCount: 1},
			},
		},
	}
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())
	model.ready = true
	model.viewMode = ViewHunk

	// Accept first hunk only
	model.acceptCurrent()

	if changes[0].Hunks[0].Status != diff.HunkAccepted {
		t.Error("First hunk should be accepted")
	}
	if changes[0].Hunks[1].Status != diff.HunkPending {
		t.Error("Second hunk should still be pending")
	}
}

func TestDiffReviewModel_Result(t *testing.T) {
	changes := createTestChanges()
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())
	model.ready = true

	// Accept first, reject second
	model.acceptCurrent()
	model.currentFile = 1
	model.rejectCurrent()

	result := model.Result()
	if result == nil {
		t.Fatal("Result should not be nil")
	}

	// Result should reflect decisions
	if result.Decisions["file1.go"].Action != diff.DecisionAccept {
		t.Error("file1.go should be accepted in result")
	}
	if result.Decisions["file2.go"].Action != diff.DecisionReject {
		t.Error("file2.go should be rejected in result")
	}
}

func TestDiffReviewModel_Changes(t *testing.T) {
	changes := createTestChanges()
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())

	returnedChanges := model.Changes()
	if len(returnedChanges) != len(changes) {
		t.Errorf("Expected %d changes, got %d", len(changes), len(returnedChanges))
	}
}

func TestDiffReviewModel_KeyMsg_Y(t *testing.T) {
	changes := createTestChanges()
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())
	model.ready = true
	model.width = 80
	model.height = 24

	// Simulate pressing 'y'
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m := newModel.(DiffReviewModel)

	// Should accept and advance
	if m.decisions["file1.go"].Action != diff.DecisionAccept {
		t.Error("Y key should accept current file")
	}
}

func TestDiffReviewModel_KeyMsg_N(t *testing.T) {
	changes := createTestChanges()
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())
	model.ready = true
	model.width = 80
	model.height = 24

	// Simulate pressing 'n'
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m := newModel.(DiffReviewModel)

	if m.decisions["file1.go"].Action != diff.DecisionReject {
		t.Error("N key should reject current file")
	}
}

func TestDiffReviewModel_KeyMsg_S(t *testing.T) {
	changes := createTestChanges()
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())
	model.ready = true
	model.width = 80
	model.height = 24

	// Simulate pressing 's'
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m := newModel.(DiffReviewModel)

	if m.decisions["file1.go"].Action != diff.DecisionSkip {
		t.Error("S key should skip current file")
	}
}

func TestDiffReviewModel_KeyMsg_Q(t *testing.T) {
	changes := createTestChanges()
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())
	model.ready = true

	// Simulate pressing 'q'
	newModel, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m := newModel.(DiffReviewModel)

	if !m.result.Cancelled {
		t.Error("Q key should cancel review")
	}
	if m.result.CancelReason != "user cancelled" {
		t.Errorf("Cancel reason = %q, want %q", m.result.CancelReason, "user cancelled")
	}
	if cmd == nil {
		t.Error("Q key should return quit command")
	}
}

func TestDiffReviewModel_KeyMsg_Help(t *testing.T) {
	changes := createTestChanges()
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())
	model.ready = true

	// Simulate pressing '?'
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m := newModel.(DiffReviewModel)

	if !m.showHelp {
		t.Error("? key should show help")
	}

	// Press '?' again to close
	newModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = newModel.(DiffReviewModel)

	if m.showHelp {
		t.Error("? key again should hide help")
	}
}

func TestDiffReviewModel_KeyMsg_Tab(t *testing.T) {
	changes := createTestChanges()
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())
	model.ready = true
	model.width = 80
	model.height = 24

	initialMode := model.viewMode

	// Simulate pressing 'tab'
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	m := newModel.(DiffReviewModel)

	if m.viewMode == initialMode {
		t.Error("Tab key should toggle view mode")
	}
}

func TestDiffReviewModel_ConfirmAcceptAll(t *testing.T) {
	changes := createTestChanges()
	config := DefaultDiffReviewConfig()
	config.ConfirmAcceptAll = true
	model := NewDiffReviewModel(changes, config)
	model.ready = true

	// Simulate pressing 'a'
	newModel, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m := newModel.(DiffReviewModel)

	if !m.showConfirm {
		t.Error("A key should show confirmation dialog when ConfirmAcceptAll is true")
	}

	// Type "yes" and press enter
	m, _ = m.handleConfirmInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m, _ = m.handleConfirmInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m, _ = m.handleConfirmInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	if m.confirmInput != "yes" {
		t.Errorf("confirmInput = %q, want %q", m.confirmInput, "yes")
	}
}

func TestDiffReviewModel_ConfirmEscape(t *testing.T) {
	changes := createTestChanges()
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())
	model.ready = true
	model.showConfirm = true
	model.confirmInput = "ye"

	// Simulate pressing escape
	m, _ := model.handleConfirmInput(tea.KeyMsg{Type: tea.KeyEsc})

	if m.showConfirm {
		t.Error("Escape should close confirmation dialog")
	}
	if m.confirmInput != "" {
		t.Error("Escape should clear confirm input")
	}
}

func TestDiffReviewModel_ConfirmBackspace(t *testing.T) {
	changes := createTestChanges()
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())
	model.ready = true
	model.showConfirm = true
	model.confirmInput = "yes"

	// Simulate backspace
	m, _ := model.handleConfirmInput(tea.KeyMsg{Type: tea.KeyBackspace})

	if m.confirmInput != "ye" {
		t.Errorf("After backspace, confirmInput = %q, want %q", m.confirmInput, "ye")
	}
}

func TestDiffReviewModel_View_NotReady(t *testing.T) {
	model := NewDiffReviewModel(nil, DefaultDiffReviewConfig())
	model.ready = false

	view := model.View()
	if view != "Loading...\n" {
		t.Errorf("View when not ready = %q, want %q", view, "Loading...\n")
	}
}

func TestDiffReviewModel_View_NoChanges(t *testing.T) {
	model := NewDiffReviewModel([]*diff.ProposedChange{}, DefaultDiffReviewConfig())
	model.ready = true

	view := model.View()
	if view != "Loading...\n" {
		t.Errorf("View with no changes = %q, want %q", view, "Loading...\n")
	}
}

func TestDiffReviewModel_View_Quitting(t *testing.T) {
	model := NewDiffReviewModel(createTestChanges(), DefaultDiffReviewConfig())
	model.quitting = true

	view := model.View()
	if view != "Review cancelled.\n" {
		t.Errorf("View when quitting = %q, want %q", view, "Review cancelled.\n")
	}
}

func TestDiffReviewModel_WindowSizeMsg(t *testing.T) {
	changes := createTestChanges()
	model := NewDiffReviewModel(changes, DefaultDiffReviewConfig())

	// Simulate window size message
	msg := tea.WindowSizeMsg{Width: 120, Height: 40}
	newModel, _ := model.Update(msg)
	m := newModel.(DiffReviewModel)

	if m.width != 120 {
		t.Errorf("width = %d, want 120", m.width)
	}
	if m.height != 40 {
		t.Errorf("height = %d, want 40", m.height)
	}
	if !m.ready {
		t.Error("Model should be ready after WindowSizeMsg")
	}
}
