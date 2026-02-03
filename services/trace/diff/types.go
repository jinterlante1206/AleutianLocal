// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package diff provides diff parsing, rendering, and application for code review.
//
// # Description
//
// This package implements unified diff parsing and application, enabling the
// interactive diff review workflow. It supports hunk-level granularity for
// accepting or rejecting individual changes.
//
// # Thread Safety
//
// Types in this package are not safe for concurrent modification.
// However, they can be safely read concurrently after creation.
package diff

import (
	"fmt"
	"strings"
)

// =============================================================================
// Line Types
// =============================================================================

// LineType categorizes diff lines.
type LineType string

const (
	// LineContext represents unchanged context lines.
	LineContext LineType = " "

	// LineAdded represents added lines.
	LineAdded LineType = "+"

	// LineRemoved represents removed lines.
	LineRemoved LineType = "-"
)

// String returns the string representation of the line type.
func (lt LineType) String() string {
	return string(lt)
}

// =============================================================================
// Change Risk
// =============================================================================

// ChangeRisk categorizes how risky a change is.
type ChangeRisk string

const (
	// RiskLow indicates pure additions, comments, formatting.
	RiskLow ChangeRisk = "low"

	// RiskMedium indicates modifications, refactors.
	RiskMedium ChangeRisk = "medium"

	// RiskHigh indicates deletions, core logic changes.
	RiskHigh ChangeRisk = "high"

	// RiskCritical indicates security-sensitive changes.
	RiskCritical ChangeRisk = "critical"
)

// String returns the string representation of the risk level.
func (r ChangeRisk) String() string {
	return string(r)
}

// =============================================================================
// Hunk Status
// =============================================================================

// HunkStatus tracks review state of a hunk.
type HunkStatus string

const (
	// HunkPending indicates the hunk has not been reviewed.
	HunkPending HunkStatus = "pending"

	// HunkAccepted indicates the hunk was accepted.
	HunkAccepted HunkStatus = "accepted"

	// HunkRejected indicates the hunk was rejected.
	HunkRejected HunkStatus = "rejected"

	// HunkEdited indicates the hunk was modified by the user.
	HunkEdited HunkStatus = "edited"
)

// String returns the string representation of the status.
func (s HunkStatus) String() string {
	return string(s)
}

// IsTerminal returns true if the status is a final decision.
func (s HunkStatus) IsTerminal() bool {
	return s == HunkAccepted || s == HunkRejected || s == HunkEdited
}

// =============================================================================
// Diff Line
// =============================================================================

// DiffLine represents a single line in a diff.
//
// # Description
//
// Each line tracks its type (context, added, removed), content,
// and line numbers in both the old and new versions.
type DiffLine struct {
	// Type is the line type (context, added, removed).
	Type LineType

	// Content is the line content without the prefix.
	Content string

	// OldNum is the line number in the old file (0 if added).
	OldNum int

	// NewNum is the line number in the new file (0 if removed).
	NewNum int
}

// String returns a formatted representation of the line.
func (l DiffLine) String() string {
	return string(l.Type) + l.Content
}

// IsAddition returns true if this line was added.
func (l DiffLine) IsAddition() bool {
	return l.Type == LineAdded
}

// IsDeletion returns true if this line was removed.
func (l DiffLine) IsDeletion() bool {
	return l.Type == LineRemoved
}

// IsContext returns true if this line is context (unchanged).
func (l DiffLine) IsContext() bool {
	return l.Type == LineContext
}

// =============================================================================
// Hunk
// =============================================================================

// Hunk represents a single diff hunk (contiguous change region).
//
// # Description
//
// A hunk is the atomic unit of a diff - a contiguous region of changes
// surrounded by context lines. Users can accept/reject at the hunk level.
type Hunk struct {
	// OldStart is the starting line number in the old file.
	OldStart int

	// OldCount is the number of lines from the old file.
	OldCount int

	// NewStart is the starting line number in the new file.
	NewStart int

	// NewCount is the number of lines in the new file.
	NewCount int

	// Lines contains all lines in this hunk.
	Lines []DiffLine

	// Status is the review status of this hunk.
	Status HunkStatus

	// EditedLines contains user modifications (if Status == HunkEdited).
	EditedLines []DiffLine
}

// Header returns the unified diff header for this hunk.
func (h *Hunk) Header() string {
	return fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.OldStart, h.OldCount, h.NewStart, h.NewCount)
}

// AddedCount returns the number of added lines.
func (h *Hunk) AddedCount() int {
	count := 0
	for _, line := range h.Lines {
		if line.IsAddition() {
			count++
		}
	}
	return count
}

// RemovedCount returns the number of removed lines.
func (h *Hunk) RemovedCount() int {
	count := 0
	for _, line := range h.Lines {
		if line.IsDeletion() {
			count++
		}
	}
	return count
}

// EffectiveLines returns the lines to apply based on status.
//
// # Description
//
// If the hunk was edited, returns EditedLines.
// Otherwise returns the original Lines.
func (h *Hunk) EffectiveLines() []DiffLine {
	if h.Status == HunkEdited && len(h.EditedLines) > 0 {
		return h.EditedLines
	}
	return h.Lines
}

// =============================================================================
// Proposed Change
// =============================================================================

// ProposedChange represents a single file change proposed by the agent.
//
// # Description
//
// Encapsulates all information about a proposed change to a file,
// including the diff hunks, risk assessment, and rationale.
type ProposedChange struct {
	// FilePath is the path to the file being changed.
	FilePath string

	// OldContent is the original file content (empty for new files).
	OldContent string

	// NewContent is the proposed new content.
	NewContent string

	// Hunks contains the parsed diff hunks.
	Hunks []*Hunk

	// Rationale explains why this change was proposed.
	Rationale string

	// Risk is the assessed risk level of this change.
	Risk ChangeRisk

	// Related lists related file changes (by path).
	Related []string

	// IsNew indicates this is a new file being created.
	IsNew bool

	// IsDelete indicates this file is being deleted.
	IsDelete bool

	// Language is the programming language (for syntax highlighting).
	Language string
}

// Stats returns a formatted stats string like "+12 -3".
func (c *ProposedChange) Stats() string {
	added, removed := c.LineStats()
	return fmt.Sprintf("+%d -%d", added, removed)
}

// LineStats returns the total lines added and removed.
func (c *ProposedChange) LineStats() (added, removed int) {
	for _, hunk := range c.Hunks {
		added += hunk.AddedCount()
		removed += hunk.RemovedCount()
	}
	return
}

// HunkCount returns the number of hunks.
func (c *ProposedChange) HunkCount() int {
	return len(c.Hunks)
}

// AllAccepted returns true if all hunks are accepted or edited.
func (c *ProposedChange) AllAccepted() bool {
	for _, hunk := range c.Hunks {
		if hunk.Status == HunkPending || hunk.Status == HunkRejected {
			return false
		}
	}
	return true
}

// AllRejected returns true if all hunks are rejected.
func (c *ProposedChange) AllRejected() bool {
	for _, hunk := range c.Hunks {
		if hunk.Status != HunkRejected {
			return false
		}
	}
	return true
}

// AnyPending returns true if any hunk is pending review.
func (c *ProposedChange) AnyPending() bool {
	for _, hunk := range c.Hunks {
		if hunk.Status == HunkPending {
			return true
		}
	}
	return false
}

// PendingCount returns the number of pending hunks.
func (c *ProposedChange) PendingCount() int {
	count := 0
	for _, hunk := range c.Hunks {
		if hunk.Status == HunkPending {
			count++
		}
	}
	return count
}

// AcceptedCount returns the number of accepted hunks.
func (c *ProposedChange) AcceptedCount() int {
	count := 0
	for _, hunk := range c.Hunks {
		if hunk.Status == HunkAccepted || hunk.Status == HunkEdited {
			count++
		}
	}
	return count
}

// RejectedCount returns the number of rejected hunks.
func (c *ProposedChange) RejectedCount() int {
	count := 0
	for _, hunk := range c.Hunks {
		if hunk.Status == HunkRejected {
			count++
		}
	}
	return count
}

// =============================================================================
// Decision Types
// =============================================================================

// DecisionAction represents the user's decision for a file or hunk.
type DecisionAction string

const (
	// DecisionPending indicates no decision has been made.
	DecisionPending DecisionAction = "pending"

	// DecisionAccept indicates acceptance without modification.
	DecisionAccept DecisionAction = "accept"

	// DecisionReject indicates rejection of the change.
	DecisionReject DecisionAction = "reject"

	// DecisionEdit indicates the change was edited.
	DecisionEdit DecisionAction = "edit"

	// DecisionSkip indicates the change was skipped for later.
	DecisionSkip DecisionAction = "skip"
)

// String returns the string representation of the action.
func (d DecisionAction) String() string {
	return string(d)
}

// IsTerminal returns true if this is a final decision.
func (d DecisionAction) IsTerminal() bool {
	return d == DecisionAccept || d == DecisionReject || d == DecisionEdit
}

// FileDecision captures the user's decision for a file.
type FileDecision struct {
	// FilePath identifies the file.
	FilePath string

	// Action is the overall file decision.
	Action DecisionAction

	// EditedContent contains user modifications (if Action == DecisionEdit).
	EditedContent string

	// HunkDecisions contains per-hunk decisions (for granular review).
	HunkDecisions map[int]HunkStatus
}

// =============================================================================
// Review Result
// =============================================================================

// ReviewResult contains the outcomes of an interactive diff review session.
//
// # Description
//
// Aggregates all user decisions and provides methods for applying
// the accepted changes.
type ReviewResult struct {
	// Decisions maps file paths to decisions.
	Decisions map[string]*FileDecision

	// Cancelled indicates the user cancelled the entire review.
	Cancelled bool

	// CancelReason explains why the review was cancelled.
	CancelReason string
}

// NewReviewResult creates a new empty review result.
func NewReviewResult() *ReviewResult {
	return &ReviewResult{
		Decisions: make(map[string]*FileDecision),
	}
}

// AcceptedFiles returns the list of files that were accepted or edited.
func (r *ReviewResult) AcceptedFiles() []string {
	var files []string
	for path, decision := range r.Decisions {
		if decision.Action == DecisionAccept || decision.Action == DecisionEdit {
			files = append(files, path)
		}
	}
	return files
}

// RejectedFiles returns the list of files that were rejected.
func (r *ReviewResult) RejectedFiles() []string {
	var files []string
	for path, decision := range r.Decisions {
		if decision.Action == DecisionReject {
			files = append(files, path)
		}
	}
	return files
}

// PendingFiles returns the list of files still pending decision.
func (r *ReviewResult) PendingFiles() []string {
	var files []string
	for path, decision := range r.Decisions {
		if decision.Action == DecisionPending || decision.Action == DecisionSkip {
			files = append(files, path)
		}
	}
	return files
}

// Summary returns a human-readable summary of the review.
func (r *ReviewResult) Summary() string {
	if r.Cancelled {
		return fmt.Sprintf("Review cancelled: %s", r.CancelReason)
	}

	accepted := len(r.AcceptedFiles())
	rejected := len(r.RejectedFiles())
	pending := len(r.PendingFiles())

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Accepted: %d files\n", accepted))
	sb.WriteString(fmt.Sprintf("Rejected: %d files\n", rejected))
	if pending > 0 {
		sb.WriteString(fmt.Sprintf("Pending:  %d files\n", pending))
	}
	return sb.String()
}

// AllDecided returns true if all files have terminal decisions.
func (r *ReviewResult) AllDecided() bool {
	for _, decision := range r.Decisions {
		if !decision.Action.IsTerminal() {
			return false
		}
	}
	return true
}
