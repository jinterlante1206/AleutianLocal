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
	"fmt"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/trace/diff"
	"github.com/charmbracelet/lipgloss"
)

// =============================================================================
// Header Rendering
// =============================================================================

func (m DiffReviewModel) renderHeader() string {
	if len(m.changes) == 0 {
		return titleStyle.Render("No changes to review")
	}

	var b strings.Builder

	// Title bar with file count
	title := fmt.Sprintf("Proposed Changes (%d files)", len(m.changes))
	b.WriteString(titleStyle.Render(title))

	// Progress indicator
	if m.viewMode != ViewSummary {
		progress := fmt.Sprintf("  [%d/%d]", m.currentFile+1, len(m.changes))
		b.WriteString(statsStyle.Render(progress))
	}

	return b.String()
}

// =============================================================================
// Footer Rendering
// =============================================================================

func (m DiffReviewModel) renderFooter() string {
	var keys []string

	switch m.viewMode {
	case ViewFile:
		keys = []string{
			"[Y] Accept", "[N] Reject", "[E] Edit", "[S] Skip",
			"[A] Accept all", "[←→] Navigate", "[?] Help", "[Q] Cancel",
		}
	case ViewHunk:
		keys = []string{
			"[Y] Accept hunk", "[N] Reject hunk", "[J/K] Navigate hunks",
			"[Tab] File view", "[?] Help", "[Q] Cancel",
		}
	case ViewSummary:
		keys = []string{
			"[Enter] Apply accepted", "[←→] Review files", "[Q] Cancel",
		}
	}

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render(strings.Join(keys, "  "))
}

// =============================================================================
// File Diff Rendering
// =============================================================================

func (m DiffReviewModel) renderFileDiff() string {
	if m.currentFile >= len(m.changes) {
		return "No file selected"
	}

	change := m.changes[m.currentFile]
	decision := m.decisions[change.FilePath]

	var b strings.Builder

	// File header with status badge
	b.WriteString(m.renderFileHeader(change, decision))
	b.WriteString("\n\n")

	// Render all hunks
	for i, hunk := range change.Hunks {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.renderHunk(hunk, i))
	}

	// Rationale if present
	if change.Rationale != "" {
		b.WriteString("\n\n")
		b.WriteString(rationaleStyle.Render("Rationale: " + change.Rationale))
	}

	return b.String()
}

func (m DiffReviewModel) renderFileHeader(change *diff.ProposedChange, decision *diff.FileDecision) string {
	var b strings.Builder

	// File path
	b.WriteString(filePathStyle.Render(change.FilePath))

	// Stats
	b.WriteString("  ")
	b.WriteString(m.renderStats(change))

	// Status badge
	b.WriteString("  ")
	switch decision.Action {
	case diff.DecisionAccept:
		b.WriteString(acceptedBadge.Render("ACCEPTED"))
	case diff.DecisionReject:
		b.WriteString(rejectedBadge.Render("REJECTED"))
	case diff.DecisionEdit:
		b.WriteString(acceptedBadge.Render("EDITED"))
	case diff.DecisionSkip:
		b.WriteString(pendingBadge.Render("SKIPPED"))
	default:
		b.WriteString(pendingBadge.Render("PENDING"))
	}

	// Risk indicator
	if change.Risk != "" {
		b.WriteString("  ")
		b.WriteString(m.renderRisk(change.Risk))
	}

	return b.String()
}

func (m DiffReviewModel) renderStats(change *diff.ProposedChange) string {
	added, removed := change.LineStats()

	addedStr := addedStyle.Render(fmt.Sprintf("+%d", added))
	removedStr := removedStyle.Render(fmt.Sprintf("-%d", removed))

	return fmt.Sprintf("%s %s", addedStr, removedStr)
}

func (m DiffReviewModel) renderRisk(risk diff.ChangeRisk) string {
	var style lipgloss.Style
	var label string

	switch risk {
	case diff.RiskCritical:
		style = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)
		label = "CRITICAL"
	case diff.RiskHigh:
		style = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))
		label = "HIGH RISK"
	case diff.RiskMedium:
		style = lipgloss.NewStyle().
			Foreground(lipgloss.Color("226"))
		label = "MEDIUM"
	default:
		style = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))
		label = "LOW"
	}

	return style.Render("[" + label + "]")
}

// =============================================================================
// Hunk Rendering
// =============================================================================

func (m DiffReviewModel) renderHunk(hunk *diff.Hunk, index int) string {
	var b strings.Builder

	// Hunk header
	header := hunk.Header()
	b.WriteString(hunkHeaderStyle.Render(header))

	// Hunk status indicator
	switch hunk.Status {
	case diff.HunkAccepted:
		b.WriteString("  " + acceptedBadge.Render("✓"))
	case diff.HunkRejected:
		b.WriteString("  " + rejectedBadge.Render("✗"))
	case diff.HunkEdited:
		b.WriteString("  " + acceptedBadge.Render("✎"))
	}

	b.WriteString("\n")

	// Render lines
	for _, line := range hunk.Lines {
		b.WriteString(m.renderLine(line))
		b.WriteString("\n")
	}

	return b.String()
}

func (m DiffReviewModel) renderLine(line diff.DiffLine) string {
	var b strings.Builder

	// Line numbers (if enabled)
	if m.config.ShowLineNumbers {
		oldNum := "   "
		newNum := "   "

		if line.OldNum > 0 {
			oldNum = fmt.Sprintf("%3d", line.OldNum)
		}
		if line.NewNum > 0 {
			newNum = fmt.Sprintf("%3d", line.NewNum)
		}

		b.WriteString(lineNumStyle.Render(oldNum))
		b.WriteString(" ")
		b.WriteString(lineNumStyle.Render(newNum))
		b.WriteString(" ")
	}

	// Prefix and content
	prefix := string(line.Type)
	content := line.Content

	var style lipgloss.Style
	switch line.Type {
	case diff.LineAdded:
		style = addedStyle
	case diff.LineRemoved:
		style = removedStyle
	default:
		style = contextStyle
	}

	b.WriteString(style.Render(prefix + content))

	return b.String()
}

// =============================================================================
// Hunk View Mode
// =============================================================================

func (m DiffReviewModel) renderHunkDiff() string {
	if m.currentFile >= len(m.changes) {
		return "No file selected"
	}

	change := m.changes[m.currentFile]
	if m.currentHunk >= len(change.Hunks) {
		return "No hunk selected"
	}

	hunk := change.Hunks[m.currentHunk]

	var b strings.Builder

	// Header showing file and hunk position
	header := fmt.Sprintf("%s  Hunk %d/%d  %s",
		filePathStyle.Render(change.FilePath),
		m.currentHunk+1,
		len(change.Hunks),
		m.renderStats(change),
	)
	b.WriteString(header)
	b.WriteString("\n\n")

	// Render the current hunk
	b.WriteString(m.renderHunk(hunk, m.currentHunk))

	// Rationale if present
	if change.Rationale != "" {
		b.WriteString("\n")
		b.WriteString(rationaleStyle.Render("Rationale: " + change.Rationale))
	}

	return b.String()
}

// =============================================================================
// Summary Rendering
// =============================================================================

func (m DiffReviewModel) renderSummary() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Review Summary"))
	b.WriteString("\n\n")

	// Categorize files
	var accepted, rejected, edited, pending []*diff.ProposedChange

	for _, change := range m.changes {
		decision := m.decisions[change.FilePath]
		switch decision.Action {
		case diff.DecisionAccept:
			accepted = append(accepted, change)
		case diff.DecisionReject:
			rejected = append(rejected, change)
		case diff.DecisionEdit:
			edited = append(edited, change)
		default:
			pending = append(pending, change)
		}
	}

	// Accepted files
	if len(accepted) > 0 {
		b.WriteString(addedStyle.Render(fmt.Sprintf("✓ Accepted (%d files):", len(accepted))))
		b.WriteString("\n")
		for _, change := range accepted {
			b.WriteString(fmt.Sprintf("  • %s  %s\n", change.FilePath, m.renderStats(change)))
		}
		b.WriteString("\n")
	}

	// Edited files
	if len(edited) > 0 {
		b.WriteString(addedStyle.Render(fmt.Sprintf("✎ Edited (%d files):", len(edited))))
		b.WriteString("\n")
		for _, change := range edited {
			b.WriteString(fmt.Sprintf("  • %s  %s\n", change.FilePath, m.renderStats(change)))
		}
		b.WriteString("\n")
	}

	// Rejected files
	if len(rejected) > 0 {
		b.WriteString(removedStyle.Render(fmt.Sprintf("✗ Rejected (%d files):", len(rejected))))
		b.WriteString("\n")
		for _, change := range rejected {
			b.WriteString(fmt.Sprintf("  • %s  %s\n", change.FilePath, m.renderStats(change)))
		}
		b.WriteString("\n")
	}

	// Pending files
	if len(pending) > 0 {
		b.WriteString(pendingBadge.Render(fmt.Sprintf("? Pending (%d files):", len(pending))))
		b.WriteString("\n")
		for _, change := range pending {
			b.WriteString(fmt.Sprintf("  • %s  %s\n", change.FilePath, m.renderStats(change)))
		}
		b.WriteString("\n")
	}

	// Totals
	totalAdded := 0
	totalRemoved := 0
	for _, change := range m.changes {
		decision := m.decisions[change.FilePath]
		if decision.Action == diff.DecisionAccept || decision.Action == diff.DecisionEdit {
			added, removed := change.LineStats()
			totalAdded += added
			totalRemoved += removed
		}
	}

	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("Total to apply: %s %s across %d files\n",
		addedStyle.Render(fmt.Sprintf("+%d", totalAdded)),
		removedStyle.Render(fmt.Sprintf("-%d", totalRemoved)),
		len(accepted)+len(edited),
	))

	if len(pending) > 0 {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Render("⚠ Some files are still pending review"))
	}

	return b.String()
}

// =============================================================================
// Help Rendering
// =============================================================================

func (m DiffReviewModel) renderHelp() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Keyboard Shortcuts"))
	b.WriteString("\n\n")

	helpItems := []struct {
		key  string
		desc string
	}{
		{"Y", "Accept current file/hunk"},
		{"N", "Reject current file/hunk"},
		{"E", "Edit in external editor"},
		{"S", "Skip for later"},
		{"A", "Accept all remaining"},
		{"Q", "Cancel review"},
		{"", ""},
		{"←/→ or H/L", "Navigate between files"},
		{"↑/↓ or J/K", "Scroll content"},
		{"Ctrl+D/U", "Page down/up"},
		{"G / Shift+G", "Go to top/bottom"},
		{"Tab", "Toggle view mode (file/hunk/summary)"},
		{"?", "Toggle this help"},
	}

	for _, item := range helpItems {
		if item.key == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			helpKeyStyle.Render(fmt.Sprintf("%-15s", item.key)),
			helpDescStyle.Render(item.desc),
		))
	}

	b.WriteString("\n")
	b.WriteString(helpDescStyle.Render("Press ? or Q to close help"))

	return b.String()
}

// =============================================================================
// Confirm Dialog Rendering
// =============================================================================

func (m DiffReviewModel) renderConfirm() string {
	var b strings.Builder

	pending := 0
	for _, change := range m.changes {
		decision := m.decisions[change.FilePath]
		if !decision.Action.IsTerminal() {
			pending++
		}
	}

	b.WriteString(titleStyle.Render("Confirm Accept All"))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("This will accept %d remaining file(s).\n\n", pending))
	b.WriteString("Type 'yes' to confirm: ")
	b.WriteString(lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		Bold(true).
		Render(m.confirmInput))
	b.WriteString("▌")

	b.WriteString("\n\n")
	b.WriteString(helpDescStyle.Render("Press Enter to confirm, Esc to cancel"))

	return b.String()
}
