// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package tui provides terminal user interface components for interactive review.
//
// # Description
//
// This package implements the interactive diff review TUI using bubbletea.
// It allows users to review, accept, reject, and edit proposed code changes
// before they are applied.
//
// # Thread Safety
//
// TUI components are designed for single-threaded use within the bubbletea
// event loop. Do not access TUI state from multiple goroutines.
package tui

import (
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/diff"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// =============================================================================
// View Mode
// =============================================================================

// ViewMode determines how changes are displayed.
type ViewMode int

const (
	// ViewFile shows entire file diff.
	ViewFile ViewMode = iota

	// ViewHunk shows individual hunks.
	ViewHunk

	// ViewSummary shows review summary.
	ViewSummary
)

// =============================================================================
// Messages
// =============================================================================

// DoneMsg signals the review is complete.
type DoneMsg struct {
	Result *diff.ReviewResult
}

// EditorResultMsg contains the result of external editor invocation.
type EditorResultMsg struct {
	FilePath string
	Content  string
	Err      error
}

// =============================================================================
// Config
// =============================================================================

// DiffReviewConfig configures the diff review TUI.
type DiffReviewConfig struct {
	// Editor is the command to invoke for editing (default: $EDITOR or "vi").
	Editor string

	// ConfirmAcceptAll requires typing "yes" for Accept All (safety).
	ConfirmAcceptAll bool

	// ShowLineNumbers shows line numbers in diff output.
	ShowLineNumbers bool

	// ContextLines is the number of context lines to show around changes.
	ContextLines int

	// Width overrides terminal width (0 = auto-detect).
	Width int

	// Height overrides terminal height (0 = auto-detect).
	Height int
}

// DefaultDiffReviewConfig returns sensible defaults.
func DefaultDiffReviewConfig() DiffReviewConfig {
	return DiffReviewConfig{
		Editor:           "",
		ConfirmAcceptAll: true,
		ShowLineNumbers:  true,
		ContextLines:     3,
	}
}

// =============================================================================
// Model
// =============================================================================

// DiffReviewModel is the bubbletea model for interactive diff review.
//
// # Description
//
// Manages the state of the diff review session, including navigation,
// decisions, and rendering.
type DiffReviewModel struct {
	// Configuration
	config DiffReviewConfig

	// Changes being reviewed
	changes []*diff.ProposedChange

	// Current navigation state
	currentFile int
	currentHunk int
	viewMode    ViewMode

	// Viewport for scrolling
	viewport viewport.Model

	// Terminal dimensions
	width  int
	height int

	// User decisions
	decisions map[string]*diff.FileDecision

	// State flags
	ready        bool
	confirmInput string
	showConfirm  bool
	showHelp     bool
	quitting     bool

	// Result
	result *diff.ReviewResult
}

// NewDiffReviewModel creates a new diff review model.
//
// # Inputs
//
//   - changes: The proposed changes to review.
//   - config: Configuration options.
//
// # Outputs
//
//   - DiffReviewModel: Ready-to-use model for tea.NewProgram.
func NewDiffReviewModel(changes []*diff.ProposedChange, config DiffReviewConfig) DiffReviewModel {
	decisions := make(map[string]*diff.FileDecision, len(changes))
	for _, change := range changes {
		decisions[change.FilePath] = &diff.FileDecision{
			FilePath:      change.FilePath,
			Action:        diff.DecisionPending,
			HunkDecisions: make(map[int]diff.HunkStatus),
		}
	}

	return DiffReviewModel{
		config:    config,
		changes:   changes,
		decisions: decisions,
		viewMode:  ViewFile,
		result:    diff.NewReviewResult(),
	}
}

// Init implements tea.Model.
func (m DiffReviewModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m DiffReviewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		headerHeight := 3
		footerHeight := 3
		viewportHeight := m.height - headerHeight - footerHeight

		if !m.ready {
			m.viewport = viewport.New(m.width, viewportHeight)
			m.viewport.YPosition = headerHeight
			m.ready = true
		} else {
			m.viewport.Width = m.width
			m.viewport.Height = viewportHeight
		}

		m.updateViewportContent()

	case tea.KeyMsg:
		// Handle confirmation input mode
		if m.showConfirm {
			return m.handleConfirmInput(msg)
		}

		// Handle help overlay
		if m.showHelp {
			if msg.String() == "q" || msg.String() == "?" || msg.String() == "esc" {
				m.showHelp = false
			}
			return m, nil
		}

		// Normal key handling
		switch msg.String() {
		case "y", "Y":
			m.acceptCurrent()
			return m.advanceFile()

		case "n", "N":
			m.rejectCurrent()
			return m.advanceFile()

		case "e", "E":
			return m, m.openEditor()

		case "s", "S":
			m.skipCurrent()
			return m.advanceFile()

		case "?":
			m.showHelp = true

		case "a", "A":
			if m.config.ConfirmAcceptAll {
				m.showConfirm = true
				m.confirmInput = ""
			} else {
				m.acceptAllRemaining()
				return m.finish()
			}

		case "q", "Q", "ctrl+c":
			m.result.Cancelled = true
			m.result.CancelReason = "user cancelled"
			m.quitting = true
			return m, tea.Quit

		case "left", "h":
			return m.prevFile()

		case "right", "l":
			return m.nextFile()

		case "j", "down":
			m.viewport.LineDown(1)

		case "k", "up":
			m.viewport.LineUp(1)

		case "ctrl+d":
			m.viewport.HalfViewDown()

		case "ctrl+u":
			m.viewport.HalfViewUp()

		case "g", "home":
			m.viewport.GotoTop()

		case "G", "end":
			m.viewport.GotoBottom()

		case "tab":
			m.toggleViewMode()
			m.updateViewportContent()

		case "enter":
			if m.viewMode == ViewSummary {
				return m.finish()
			}
		}

	case EditorResultMsg:
		if msg.Err == nil {
			m.applyEdit(msg.FilePath, msg.Content)
		}
		return m, nil
	}

	// Update viewport
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// View implements tea.Model.
func (m DiffReviewModel) View() string {
	if m.quitting {
		return "Review cancelled.\n"
	}

	if !m.ready || len(m.changes) == 0 {
		return "Loading...\n"
	}

	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Main content
	if m.showHelp {
		b.WriteString(m.renderHelp())
	} else if m.showConfirm {
		b.WriteString(m.renderConfirm())
	} else {
		b.WriteString(m.viewport.View())
	}

	// Footer
	b.WriteString("\n")
	b.WriteString(m.renderFooter())

	return b.String()
}

// =============================================================================
// Navigation
// =============================================================================

func (m *DiffReviewModel) advanceFile() (DiffReviewModel, tea.Cmd) {
	// Find next pending file
	for i := m.currentFile + 1; i < len(m.changes); i++ {
		decision := m.decisions[m.changes[i].FilePath]
		if !decision.Action.IsTerminal() {
			m.currentFile = i
			m.currentHunk = 0
			m.updateViewportContent()
			return *m, nil
		}
	}

	// No more pending files - show summary
	m.viewMode = ViewSummary
	m.updateViewportContent()
	return *m, nil
}

func (m *DiffReviewModel) prevFile() (DiffReviewModel, tea.Cmd) {
	if m.currentFile > 0 {
		m.currentFile--
		m.currentHunk = 0
		m.updateViewportContent()
	}
	return *m, nil
}

func (m *DiffReviewModel) nextFile() (DiffReviewModel, tea.Cmd) {
	if m.currentFile < len(m.changes)-1 {
		m.currentFile++
		m.currentHunk = 0
		m.updateViewportContent()
	}
	return *m, nil
}

func (m *DiffReviewModel) toggleViewMode() {
	switch m.viewMode {
	case ViewFile:
		m.viewMode = ViewHunk
	case ViewHunk:
		m.viewMode = ViewSummary
	case ViewSummary:
		m.viewMode = ViewFile
	}
}

// =============================================================================
// Actions
// =============================================================================

func (m *DiffReviewModel) acceptCurrent() {
	if m.currentFile >= len(m.changes) {
		return
	}

	change := m.changes[m.currentFile]
	decision := m.decisions[change.FilePath]

	if m.viewMode == ViewHunk {
		// Accept current hunk only
		if m.currentHunk < len(change.Hunks) {
			change.Hunks[m.currentHunk].Status = diff.HunkAccepted
			decision.HunkDecisions[m.currentHunk] = diff.HunkAccepted
		}
	} else {
		// Accept entire file
		decision.Action = diff.DecisionAccept
		for i, hunk := range change.Hunks {
			hunk.Status = diff.HunkAccepted
			decision.HunkDecisions[i] = diff.HunkAccepted
		}
	}
}

func (m *DiffReviewModel) rejectCurrent() {
	if m.currentFile >= len(m.changes) {
		return
	}

	change := m.changes[m.currentFile]
	decision := m.decisions[change.FilePath]

	if m.viewMode == ViewHunk {
		// Reject current hunk only
		if m.currentHunk < len(change.Hunks) {
			change.Hunks[m.currentHunk].Status = diff.HunkRejected
			decision.HunkDecisions[m.currentHunk] = diff.HunkRejected
		}
	} else {
		// Reject entire file
		decision.Action = diff.DecisionReject
		for i, hunk := range change.Hunks {
			hunk.Status = diff.HunkRejected
			decision.HunkDecisions[i] = diff.HunkRejected
		}
	}
}

func (m *DiffReviewModel) skipCurrent() {
	if m.currentFile >= len(m.changes) {
		return
	}

	change := m.changes[m.currentFile]
	decision := m.decisions[change.FilePath]
	decision.Action = diff.DecisionSkip
}

func (m *DiffReviewModel) applyEdit(filePath, content string) {
	for i, change := range m.changes {
		if change.FilePath == filePath {
			decision := m.decisions[filePath]
			decision.Action = diff.DecisionEdit
			decision.EditedContent = content

			// Mark all hunks as edited
			for j := range change.Hunks {
				change.Hunks[j].Status = diff.HunkEdited
				decision.HunkDecisions[j] = diff.HunkEdited
			}

			// Move to next file
			m.currentFile = i
			*m, _ = m.advanceFile()
			return
		}
	}
}

func (m *DiffReviewModel) acceptAllRemaining() {
	for _, change := range m.changes {
		decision := m.decisions[change.FilePath]
		if !decision.Action.IsTerminal() {
			decision.Action = diff.DecisionAccept
			for i, hunk := range change.Hunks {
				if hunk.Status == diff.HunkPending {
					hunk.Status = diff.HunkAccepted
					decision.HunkDecisions[i] = diff.HunkAccepted
				}
			}
		}
	}
}

func (m *DiffReviewModel) openEditor() tea.Cmd {
	if m.currentFile >= len(m.changes) {
		return nil
	}

	// Editor functionality requires temp file handling
	// For now, editor invocation is a no-op
	// Future: write proposed content to temp file, invoke $EDITOR, read result
	_ = m.changes[m.currentFile] // Acknowledge we have the change
	_ = m.config.Editor          // Acknowledge we have the editor config

	return nil
}

func (m DiffReviewModel) finish() (DiffReviewModel, tea.Cmd) {
	// Build final result
	m.result.Decisions = m.decisions
	m.quitting = true

	return m, tea.Sequence(
		func() tea.Msg { return DoneMsg{Result: m.result} },
		tea.Quit,
	)
}

// =============================================================================
// Confirmation Handling
// =============================================================================

func (m DiffReviewModel) handleConfirmInput(msg tea.KeyMsg) (DiffReviewModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if strings.ToLower(m.confirmInput) == "yes" {
			m.showConfirm = false
			m.acceptAllRemaining()
			return m.finish()
		}
		m.showConfirm = false
		m.confirmInput = ""

	case "esc":
		m.showConfirm = false
		m.confirmInput = ""

	case "backspace":
		if len(m.confirmInput) > 0 {
			m.confirmInput = m.confirmInput[:len(m.confirmInput)-1]
		}

	default:
		if len(msg.String()) == 1 {
			m.confirmInput += msg.String()
		}
	}

	return m, nil
}

// =============================================================================
// Viewport Content
// =============================================================================

func (m *DiffReviewModel) updateViewportContent() {
	if !m.ready {
		return
	}

	var content string
	switch m.viewMode {
	case ViewFile:
		content = m.renderFileDiff()
	case ViewHunk:
		content = m.renderHunkDiff()
	case ViewSummary:
		content = m.renderSummary()
	}

	m.viewport.SetContent(content)
}

// =============================================================================
// Result Access
// =============================================================================

// Result returns the review result after the TUI exits.
//
// # Description
//
// Returns the current state of all decisions. If the TUI hasn't
// finished yet, this returns the decisions made so far.
func (m DiffReviewModel) Result() *diff.ReviewResult {
	// Populate decisions from model state
	m.result.Decisions = m.decisions
	return m.result
}

// Changes returns the changes with updated hunk statuses.
func (m DiffReviewModel) Changes() []*diff.ProposedChange {
	return m.changes
}

// =============================================================================
// Styles
// =============================================================================

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39"))

	filePathStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212"))

	statsStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	addedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))

	removedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	contextStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("250"))

	lineNumStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Width(4)

	hunkHeaderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("75")).
			Bold(true)

	rationaleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Italic(true)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Bold(true)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("250"))

	acceptedBadge = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Background(lipgloss.Color("22")).
			Padding(0, 1)

	rejectedBadge = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Background(lipgloss.Color("52")).
			Padding(0, 1)

	pendingBadge = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Background(lipgloss.Color("58")).
			Padding(0, 1)
)
