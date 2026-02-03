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
	"fmt"
	"path/filepath"
	"strings"

	godiff "github.com/sourcegraph/go-diff/diff"
)

// =============================================================================
// Diff Generation
// =============================================================================

// GenerateDiff creates a ProposedChange from old and new content.
//
// # Description
//
// Computes the unified diff between old and new content, parsing it into
// hunks for interactive review. Automatically detects file creation/deletion.
//
// # Inputs
//
//   - filePath: Path to the file being changed.
//   - oldContent: Original file content (empty string for new files).
//   - newContent: Proposed new content (empty string for deletions).
//   - rationale: Explanation for the change.
//
// # Outputs
//
//   - *ProposedChange: Parsed change with hunks.
//   - error: Non-nil if diff generation fails.
//
// # Example
//
//	change, err := GenerateDiff("auth/handler.go", oldCode, newCode, "Added error handling")
func GenerateDiff(filePath, oldContent, newContent, rationale string) (*ProposedChange, error) {
	change := &ProposedChange{
		FilePath:   filePath,
		OldContent: oldContent,
		NewContent: newContent,
		Rationale:  rationale,
		Language:   detectLanguage(filePath),
		IsNew:      oldContent == "",
		IsDelete:   newContent == "",
	}

	// Generate unified diff
	unifiedDiff, err := generateUnifiedDiff(filePath, oldContent, newContent)
	if err != nil {
		return nil, fmt.Errorf("generating unified diff: %w", err)
	}

	// Parse the unified diff into hunks
	hunks, err := parseUnifiedDiff(unifiedDiff)
	if err != nil {
		return nil, fmt.Errorf("parsing unified diff: %w", err)
	}

	change.Hunks = hunks
	change.Risk = assessRisk(change)

	return change, nil
}

// generateUnifiedDiff creates a unified diff string.
func generateUnifiedDiff(filePath, oldContent, newContent string) (string, error) {
	// Use go-diff library for unified diff generation
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)

	edits := computeEdits(oldLines, newLines)
	return formatUnifiedDiff(filePath, oldLines, newLines, edits), nil
}

// splitLines splits content into lines, preserving empty trailing lines.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	// Remove trailing empty string if content doesn't end with newline
	if len(lines) > 0 && lines[len(lines)-1] == "" && !strings.HasSuffix(content, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// =============================================================================
// Edit Computation (Myers Diff Algorithm)
// =============================================================================

// editOp represents a single edit operation.
type editOp struct {
	kind    editKind
	oldLine int
	newLine int
	text    string
}

type editKind int

const (
	editEqual editKind = iota
	editInsert
	editDelete
)

// maxLCSMatrixSize limits memory usage for very large files.
// Beyond this threshold, we use a simpler line-by-line diff.
const maxLCSMatrixSize = 100_000_000 // 100M cells max (~800MB for int64)

// computeEdits computes the minimal edit sequence using Myers diff.
//
// # Description
//
// Uses LCS-based diff for small files, falls back to simpler
// line-by-line comparison for very large files to avoid memory exhaustion.
func computeEdits(oldLines, newLines []string) []editOp {
	m, n := len(oldLines), len(newLines)
	if m == 0 && n == 0 {
		return nil
	}

	// Check if LCS matrix would be too large
	matrixSize := int64(m+1) * int64(n+1)
	if matrixSize > maxLCSMatrixSize {
		return computeEditsLinear(oldLines, newLines)
	}

	// Simple LCS-based diff (Myers optimization for common cases)
	var edits []editOp

	// Build LCS matrix
	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}

	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}

	// Trace back to get edits
	i, j := 0, 0
	for i < m || j < n {
		if i < m && j < n && oldLines[i] == newLines[j] {
			edits = append(edits, editOp{kind: editEqual, oldLine: i + 1, newLine: j + 1, text: oldLines[i]})
			i++
			j++
		} else if j < n && (i >= m || lcs[i][j+1] >= lcs[i+1][j]) {
			edits = append(edits, editOp{kind: editInsert, newLine: j + 1, text: newLines[j]})
			j++
		} else if i < m {
			edits = append(edits, editOp{kind: editDelete, oldLine: i + 1, text: oldLines[i]})
			i++
		}
	}

	return edits
}

// computeEditsLinear is a memory-efficient fallback for very large files.
// It uses O(m+n) memory instead of O(m*n) but produces less optimal diffs.
func computeEditsLinear(oldLines, newLines []string) []editOp {
	// Build a map of old lines for O(1) lookup
	oldMap := make(map[string][]int, len(oldLines))
	for i, line := range oldLines {
		oldMap[line] = append(oldMap[line], i)
	}

	var edits []editOp
	usedOld := make([]bool, len(oldLines))
	oldIdx := 0

	for newIdx, newLine := range newLines {
		// Check if this line exists in old
		if indices, ok := oldMap[newLine]; ok {
			// Find first unused match
			matched := false
			for _, idx := range indices {
				if !usedOld[idx] {
					// Emit deletes for skipped old lines
					for oldIdx < idx {
						if !usedOld[oldIdx] {
							edits = append(edits, editOp{
								kind:    editDelete,
								oldLine: oldIdx + 1,
								text:    oldLines[oldIdx],
							})
							usedOld[oldIdx] = true
						}
						oldIdx++
					}
					// Emit equal
					edits = append(edits, editOp{
						kind:    editEqual,
						oldLine: idx + 1,
						newLine: newIdx + 1,
						text:    newLine,
					})
					usedOld[idx] = true
					if idx >= oldIdx {
						oldIdx = idx + 1
					}
					matched = true
					break
				}
			}
			if !matched {
				// All matches used, treat as insert
				edits = append(edits, editOp{
					kind:    editInsert,
					newLine: newIdx + 1,
					text:    newLine,
				})
			}
		} else {
			// Line doesn't exist in old, it's an insert
			edits = append(edits, editOp{
				kind:    editInsert,
				newLine: newIdx + 1,
				text:    newLine,
			})
		}
	}

	// Emit remaining deletes
	for i := oldIdx; i < len(oldLines); i++ {
		if !usedOld[i] {
			edits = append(edits, editOp{
				kind:    editDelete,
				oldLine: i + 1,
				text:    oldLines[i],
			})
		}
	}

	return edits
}

// formatUnifiedDiff formats edits as a unified diff.
func formatUnifiedDiff(filePath string, oldLines, newLines []string, edits []editOp) string {
	if len(edits) == 0 {
		return ""
	}

	var sb strings.Builder

	// Write header
	sb.WriteString(fmt.Sprintf("--- a/%s\n", filePath))
	sb.WriteString(fmt.Sprintf("+++ b/%s\n", filePath))

	// Group edits into hunks (with context)
	const contextLines = 3
	hunks := groupIntoHunks(edits, contextLines, len(oldLines), len(newLines))

	for _, hunk := range hunks {
		sb.WriteString(hunk)
	}

	return sb.String()
}

// groupIntoHunks groups edits into unified diff hunks.
func groupIntoHunks(edits []editOp, contextLines, oldLen, newLen int) []string {
	if len(edits) == 0 {
		return nil
	}

	var hunks []string
	var hunkEdits []editOp
	hunkStart := -1

	flushHunk := func() {
		if len(hunkEdits) == 0 {
			return
		}

		// Calculate hunk header
		oldStart, oldCount := 0, 0
		newStart, newCount := 0, 0

		for _, e := range hunkEdits {
			switch e.kind {
			case editEqual:
				if oldStart == 0 {
					oldStart = e.oldLine
				}
				if newStart == 0 {
					newStart = e.newLine
				}
				oldCount++
				newCount++
			case editDelete:
				if oldStart == 0 {
					oldStart = e.oldLine
				}
				oldCount++
			case editInsert:
				if newStart == 0 {
					newStart = e.newLine
				}
				newCount++
			}
		}

		if oldStart == 0 {
			oldStart = 1
		}
		if newStart == 0 {
			newStart = 1
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount))

		for _, e := range hunkEdits {
			switch e.kind {
			case editEqual:
				sb.WriteString(" " + e.text + "\n")
			case editDelete:
				sb.WriteString("-" + e.text + "\n")
			case editInsert:
				sb.WriteString("+" + e.text + "\n")
			}
		}

		hunks = append(hunks, sb.String())
		hunkEdits = nil
	}

	for i, edit := range edits {
		if edit.kind != editEqual {
			// Start or extend a hunk
			if hunkStart < 0 {
				// Include preceding context
				start := i - contextLines
				if start < 0 {
					start = 0
				}
				for j := start; j < i; j++ {
					if edits[j].kind == editEqual {
						hunkEdits = append(hunkEdits, edits[j])
					}
				}
			}
			hunkStart = i
			hunkEdits = append(hunkEdits, edit)
		} else {
			// Context line
			if hunkStart >= 0 {
				// Check if we should close the hunk
				remaining := len(edits) - i - 1
				hasMoreChanges := false
				lookahead := min(contextLines*2+1, remaining+1)
				for j := i + 1; j <= i+lookahead && j < len(edits); j++ {
					if edits[j].kind != editEqual {
						hasMoreChanges = true
						break
					}
				}

				if hasMoreChanges {
					// Continue the hunk
					hunkEdits = append(hunkEdits, edit)
				} else {
					// Close the hunk with trailing context
					contextAdded := 0
					for j := i; j < len(edits) && contextAdded < contextLines; j++ {
						if edits[j].kind == editEqual {
							hunkEdits = append(hunkEdits, edits[j])
							contextAdded++
						}
					}
					flushHunk()
					hunkStart = -1
				}
			}
		}
	}

	// Flush any remaining hunk
	flushHunk()

	return hunks
}

// =============================================================================
// Diff Parsing
// =============================================================================

// parseUnifiedDiff parses a unified diff string into hunks.
func parseUnifiedDiff(unifiedDiff string) ([]*Hunk, error) {
	if unifiedDiff == "" {
		return nil, nil
	}

	// Use go-diff library for parsing
	fileDiffs, err := godiff.ParseMultiFileDiff([]byte(unifiedDiff))
	if err != nil {
		return nil, fmt.Errorf("parsing diff: %w", err)
	}

	var hunks []*Hunk
	for _, fd := range fileDiffs {
		for _, h := range fd.Hunks {
			hunk := &Hunk{
				OldStart: int(h.OrigStartLine),
				OldCount: int(h.OrigLines),
				NewStart: int(h.NewStartLine),
				NewCount: int(h.NewLines),
				Status:   HunkPending,
			}

			// Parse hunk body into lines
			hunkLines := parseHunkBody(string(h.Body), hunk.OldStart, hunk.NewStart)
			hunk.Lines = hunkLines

			hunks = append(hunks, hunk)
		}
	}

	return hunks, nil
}

// parseHunkBody parses the body of a hunk into DiffLines.
func parseHunkBody(body string, oldStart, newStart int) []DiffLine {
	var lines []DiffLine
	oldNum := oldStart
	newNum := newStart

	for _, line := range strings.Split(body, "\n") {
		if line == "" {
			continue
		}

		if len(line) == 0 {
			continue
		}

		prefix := line[0]
		content := ""
		if len(line) > 1 {
			content = line[1:]
		}

		var dl DiffLine
		switch prefix {
		case '+':
			dl = DiffLine{
				Type:    LineAdded,
				Content: content,
				NewNum:  newNum,
			}
			newNum++
		case '-':
			dl = DiffLine{
				Type:    LineRemoved,
				Content: content,
				OldNum:  oldNum,
			}
			oldNum++
		case ' ':
			dl = DiffLine{
				Type:    LineContext,
				Content: content,
				OldNum:  oldNum,
				NewNum:  newNum,
			}
			oldNum++
			newNum++
		case '\\':
			// "\ No newline at end of file" - skip
			continue
		default:
			// Treat as context if no recognized prefix
			dl = DiffLine{
				Type:    LineContext,
				Content: line,
				OldNum:  oldNum,
				NewNum:  newNum,
			}
			oldNum++
			newNum++
		}

		lines = append(lines, dl)
	}

	return lines
}

// ParseMultiFileDiff parses a multi-file unified diff.
//
// # Description
//
// Parses a complete unified diff that may contain changes to multiple files.
//
// # Inputs
//
//   - diffText: The unified diff text.
//
// # Outputs
//
//   - []*ProposedChange: Parsed changes for each file.
//   - error: Non-nil if parsing fails.
func ParseMultiFileDiff(diffText string) ([]*ProposedChange, error) {
	fileDiffs, err := godiff.ParseMultiFileDiff([]byte(diffText))
	if err != nil {
		return nil, fmt.Errorf("parsing multi-file diff: %w", err)
	}

	var changes []*ProposedChange
	for _, fd := range fileDiffs {
		change := &ProposedChange{
			FilePath: cleanDiffPath(fd.NewName),
			Language: detectLanguage(fd.NewName),
			IsNew:    fd.OrigName == "/dev/null",
			IsDelete: fd.NewName == "/dev/null",
		}

		for _, h := range fd.Hunks {
			hunk := &Hunk{
				OldStart: int(h.OrigStartLine),
				OldCount: int(h.OrigLines),
				NewStart: int(h.NewStartLine),
				NewCount: int(h.NewLines),
				Status:   HunkPending,
				Lines:    parseHunkBody(string(h.Body), int(h.OrigStartLine), int(h.NewStartLine)),
			}
			change.Hunks = append(change.Hunks, hunk)
		}

		change.Risk = assessRisk(change)
		changes = append(changes, change)
	}

	return changes, nil
}

// cleanDiffPath removes the a/ or b/ prefix from diff paths.
func cleanDiffPath(path string) string {
	if strings.HasPrefix(path, "a/") || strings.HasPrefix(path, "b/") {
		return path[2:]
	}
	return path
}

// =============================================================================
// Risk Assessment
// =============================================================================

// assessRisk determines the risk level of a proposed change.
func assessRisk(change *ProposedChange) ChangeRisk {
	if change.IsDelete {
		return RiskHigh
	}

	// Check for security-sensitive patterns
	if isSecuritySensitive(change.FilePath) {
		return RiskCritical
	}

	added, removed := change.LineStats()

	// Large deletions are high risk
	if removed > 20 {
		return RiskHigh
	}

	// Significant modifications are medium risk
	if removed > 5 || (added > 0 && removed > 0) {
		return RiskMedium
	}

	// Pure additions are low risk
	return RiskLow
}

// isSecuritySensitive checks if a file path suggests security sensitivity.
func isSecuritySensitive(filePath string) bool {
	sensitivePatterns := []string{
		"auth", "security", "credential", "password", "secret",
		"token", "key", "cert", "crypto", "encrypt", "permission",
		"access", "login", "session",
	}

	lower := strings.ToLower(filePath)
	for _, pattern := range sensitivePatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// languageMap provides O(1) lookup for file extension to language mapping.
var languageMap = map[string]string{
	".go":       "go",
	".py":       "python",
	".js":       "javascript",
	".ts":       "typescript",
	".tsx":      "typescriptreact",
	".jsx":      "javascriptreact",
	".java":     "java",
	".rs":       "rust",
	".rb":       "ruby",
	".c":        "c",
	".h":        "c",
	".cpp":      "cpp",
	".hpp":      "cpp",
	".cc":       "cpp",
	".cxx":      "cpp",
	".cs":       "csharp",
	".php":      "php",
	".swift":    "swift",
	".kt":       "kotlin",
	".kts":      "kotlin",
	".scala":    "scala",
	".sh":       "bash",
	".bash":     "bash",
	".yaml":     "yaml",
	".yml":      "yaml",
	".json":     "json",
	".xml":      "xml",
	".html":     "html",
	".htm":      "html",
	".css":      "css",
	".scss":     "scss",
	".sass":     "scss",
	".md":       "markdown",
	".markdown": "markdown",
	".sql":      "sql",
}

// detectLanguage infers the programming language from file extension.
func detectLanguage(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	if lang, ok := languageMap[ext]; ok {
		return lang
	}
	return "text"
}
