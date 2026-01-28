// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package validation

import (
	"bufio"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// DiffParser extracts changed symbols from unified diff format.
//
// # Description
//
// Parses unified diff patches and identifies which symbols (functions,
// types, etc.) were modified. This is used by patch validation to
// analyze the blast radius of proposed changes.
//
// # Supported Formats
//
//   - Standard unified diff (diff -u)
//   - Git diff format
//
// # Thread Safety
//
// Safe for concurrent use (stateless).
type DiffParser struct {
	validator *InputValidator
}

// ChangedSymbol represents a symbol that was modified in a patch.
type ChangedSymbol struct {
	// FilePath is the file containing the changed symbol.
	FilePath string `json:"file_path"`

	// SymbolName is the function/type name if detected.
	// Empty if not determinable from context.
	SymbolName string `json:"symbol_name,omitempty"`

	// ChangeType indicates what happened to the symbol.
	// One of: "added", "modified", "deleted".
	ChangeType string `json:"change_type"`

	// StartLine is the starting line number of the change.
	StartLine int `json:"start_line"`

	// EndLine is the ending line number of the change.
	EndLine int `json:"end_line"`

	// Context contains the hunk header context (e.g., function name).
	Context string `json:"context,omitempty"`

	// AddedLines is the count of lines added.
	AddedLines int `json:"added_lines"`

	// DeletedLines is the count of lines deleted.
	DeletedLines int `json:"deleted_lines"`
}

// Change type constants.
const (
	ChangeTypeAdded    = "added"
	ChangeTypeModified = "modified"
	ChangeTypeDeleted  = "deleted"
)

// DiffFile represents a single file in a diff.
type DiffFile struct {
	OldPath   string
	NewPath   string
	Hunks     []DiffHunk
	IsNew     bool
	IsDeleted bool
}

// DiffHunk represents a single hunk in a diff.
type DiffHunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Context  string // Function/method context from hunk header
	Lines    []DiffLine
}

// DiffLine represents a single line in a diff.
type DiffLine struct {
	Type    string // "+", "-", or " "
	Content string
	OldLine int // 0 if not applicable
	NewLine int // 0 if not applicable
}

// NewDiffParser creates a new diff parser.
//
// # Inputs
//
//   - validator: Input validator. If nil, a default is created.
//
// # Outputs
//
//   - *DiffParser: Ready-to-use parser.
func NewDiffParser(validator *InputValidator) *DiffParser {
	if validator == nil {
		validator = NewInputValidator(nil)
	}
	return &DiffParser{
		validator: validator,
	}
}

// ExtractChangedSymbols parses a patch and returns changed symbols.
//
// # Description
//
// Parses the unified diff and identifies changed lines, then attempts
// to determine which symbols were affected based on the hunk context.
//
// # Inputs
//
//   - patch: The unified diff string.
//
// # Outputs
//
//   - []ChangedSymbol: List of changed symbols.
//   - error: Non-nil if patch is invalid.
//
// # Example
//
//	parser := NewDiffParser(validator)
//	symbols, err := parser.ExtractChangedSymbols(patchContent)
//	if err != nil {
//	    return fmt.Errorf("failed to parse patch: %w", err)
//	}
//
//	for _, sym := range symbols {
//	    fmt.Printf("Changed: %s in %s\n", sym.SymbolName, sym.FilePath)
//	}
func (p *DiffParser) ExtractChangedSymbols(patch string) ([]ChangedSymbol, error) {
	// Validate patch first
	if err := p.validator.ValidateDiffPatch(patch); err != nil {
		return nil, err
	}

	// Parse the diff
	files, err := p.ParseDiff(patch)
	if err != nil {
		return nil, fmt.Errorf("failed to parse diff: %w", err)
	}

	// Extract changed symbols from each file
	var symbols []ChangedSymbol
	for _, file := range files {
		fileSymbols := p.extractSymbolsFromFile(file)
		symbols = append(symbols, fileSymbols...)
	}

	return symbols, nil
}

// ParseDiff parses a unified diff into structured data.
//
// # Inputs
//
//   - patch: The unified diff string.
//
// # Outputs
//
//   - []DiffFile: Parsed diff files.
//   - error: Non-nil if patch format is invalid.
func (p *DiffParser) ParseDiff(patch string) ([]DiffFile, error) {
	var files []DiffFile
	var currentFile *DiffFile
	var currentHunk *DiffHunk
	var oldLine, newLine int

	scanner := bufio.NewScanner(strings.NewReader(patch))

	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, "diff "):
			// Start of new file
			if currentFile != nil {
				if currentHunk != nil {
					currentFile.Hunks = append(currentFile.Hunks, *currentHunk)
				}
				files = append(files, *currentFile)
			}
			currentFile = &DiffFile{}
			currentHunk = nil

		case strings.HasPrefix(line, "--- "):
			if currentFile != nil {
				currentFile.OldPath = parseFilePath(line[4:])
				if currentFile.OldPath == "/dev/null" {
					currentFile.IsNew = true
				}
			}

		case strings.HasPrefix(line, "+++ "):
			if currentFile != nil {
				currentFile.NewPath = parseFilePath(line[4:])
				if currentFile.NewPath == "/dev/null" {
					currentFile.IsDeleted = true
				}
			}

		case strings.HasPrefix(line, "@@ "):
			// Start of new hunk
			if currentFile != nil && currentHunk != nil {
				currentFile.Hunks = append(currentFile.Hunks, *currentHunk)
			}
			hunk, err := parseHunkHeader(line)
			if err != nil {
				continue // Skip malformed hunk headers
			}
			currentHunk = hunk
			oldLine = hunk.OldStart
			newLine = hunk.NewStart

		case currentHunk != nil && (strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, " ") || line == ""):
			// Line in hunk
			diffLine := DiffLine{Content: line}
			if len(line) > 0 {
				diffLine.Type = string(line[0])
				diffLine.Content = line[1:]
			} else {
				diffLine.Type = " "
			}

			switch diffLine.Type {
			case "+":
				diffLine.NewLine = newLine
				newLine++
			case "-":
				diffLine.OldLine = oldLine
				oldLine++
			case " ":
				diffLine.OldLine = oldLine
				diffLine.NewLine = newLine
				oldLine++
				newLine++
			}

			currentHunk.Lines = append(currentHunk.Lines, diffLine)
		}
	}

	// Don't forget the last file/hunk
	if currentFile != nil {
		if currentHunk != nil {
			currentFile.Hunks = append(currentFile.Hunks, *currentHunk)
		}
		files = append(files, *currentFile)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}

	return files, nil
}

// parseFilePath extracts the file path from a diff header line.
func parseFilePath(line string) string {
	// Remove common prefixes
	path := strings.TrimPrefix(line, "a/")
	path = strings.TrimPrefix(path, "b/")

	// Remove timestamp suffix if present (old diff format)
	if idx := strings.Index(path, "\t"); idx != -1 {
		path = path[:idx]
	}

	return strings.TrimSpace(path)
}

// hunkHeaderRegex matches hunk headers like "@@ -1,5 +1,7 @@ func name"
var hunkHeaderRegex = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)$`)

// parseHunkHeader parses a hunk header line.
func parseHunkHeader(line string) (*DiffHunk, error) {
	matches := hunkHeaderRegex.FindStringSubmatch(line)
	if matches == nil {
		return nil, fmt.Errorf("invalid hunk header: %s", line)
	}

	hunk := &DiffHunk{}

	hunk.OldStart, _ = strconv.Atoi(matches[1])
	if matches[2] != "" {
		hunk.OldCount, _ = strconv.Atoi(matches[2])
	} else {
		hunk.OldCount = 1
	}

	hunk.NewStart, _ = strconv.Atoi(matches[3])
	if matches[4] != "" {
		hunk.NewCount, _ = strconv.Atoi(matches[4])
	} else {
		hunk.NewCount = 1
	}

	hunk.Context = strings.TrimSpace(matches[5])

	return hunk, nil
}

// extractSymbolsFromFile extracts changed symbols from a parsed file.
func (p *DiffParser) extractSymbolsFromFile(file DiffFile) []ChangedSymbol {
	var symbols []ChangedSymbol

	// Determine file path (prefer new path, fall back to old)
	filePath := file.NewPath
	if filePath == "" || filePath == "/dev/null" {
		filePath = file.OldPath
	}

	// Handle whole-file changes
	if file.IsNew {
		symbols = append(symbols, ChangedSymbol{
			FilePath:   filePath,
			ChangeType: ChangeTypeAdded,
			StartLine:  1,
			EndLine:    p.countNewLines(file),
			Context:    "new file",
		})
		return symbols
	}

	if file.IsDeleted {
		symbols = append(symbols, ChangedSymbol{
			FilePath:   filePath,
			ChangeType: ChangeTypeDeleted,
			StartLine:  1,
			EndLine:    p.countOldLines(file),
			Context:    "deleted file",
		})
		return symbols
	}

	// Extract symbols from each hunk
	for _, hunk := range file.Hunks {
		symbol := p.extractSymbolFromHunk(filePath, hunk)
		if symbol != nil {
			symbols = append(symbols, *symbol)
		}
	}

	return symbols
}

// extractSymbolFromHunk creates a ChangedSymbol from a hunk.
func (p *DiffParser) extractSymbolFromHunk(filePath string, hunk DiffHunk) *ChangedSymbol {
	var added, deleted int
	var startLine, endLine int

	startLine = hunk.NewStart
	endLine = hunk.NewStart + hunk.NewCount - 1

	for _, line := range hunk.Lines {
		switch line.Type {
		case "+":
			added++
		case "-":
			deleted++
		}
	}

	// Skip hunks with no actual changes (context only)
	if added == 0 && deleted == 0 {
		return nil
	}

	changeType := ChangeTypeModified
	if added > 0 && deleted == 0 {
		changeType = ChangeTypeAdded
	} else if deleted > 0 && added == 0 {
		changeType = ChangeTypeDeleted
	}

	// Try to extract symbol name from context
	symbolName := extractSymbolNameFromContext(hunk.Context)

	return &ChangedSymbol{
		FilePath:     filePath,
		SymbolName:   symbolName,
		ChangeType:   changeType,
		StartLine:    startLine,
		EndLine:      endLine,
		Context:      hunk.Context,
		AddedLines:   added,
		DeletedLines: deleted,
	}
}

// extractSymbolNameFromContext tries to extract a symbol name from hunk context.
func extractSymbolNameFromContext(context string) string {
	if context == "" {
		return ""
	}

	// Go function/method: "func Name" or "func (r *Receiver) Name"
	goFuncRegex := regexp.MustCompile(`func\s+(?:\([^)]+\)\s+)?(\w+)`)
	if matches := goFuncRegex.FindStringSubmatch(context); len(matches) > 1 {
		return matches[1]
	}

	// Go type: "type Name"
	goTypeRegex := regexp.MustCompile(`type\s+(\w+)`)
	if matches := goTypeRegex.FindStringSubmatch(context); len(matches) > 1 {
		return matches[1]
	}

	// Python function/method: "def name"
	pyFuncRegex := regexp.MustCompile(`def\s+(\w+)`)
	if matches := pyFuncRegex.FindStringSubmatch(context); len(matches) > 1 {
		return matches[1]
	}

	// Python class: "class Name"
	pyClassRegex := regexp.MustCompile(`class\s+(\w+)`)
	if matches := pyClassRegex.FindStringSubmatch(context); len(matches) > 1 {
		return matches[1]
	}

	// JavaScript/TypeScript function: "function name" or "const name ="
	jsFuncRegex := regexp.MustCompile(`(?:function\s+|(?:const|let|var)\s+)(\w+)`)
	if matches := jsFuncRegex.FindStringSubmatch(context); len(matches) > 1 {
		return matches[1]
	}

	// JavaScript class
	jsClassRegex := regexp.MustCompile(`class\s+(\w+)`)
	if matches := jsClassRegex.FindStringSubmatch(context); len(matches) > 1 {
		return matches[1]
	}

	return ""
}

// countNewLines counts total new lines in a diff file.
func (p *DiffParser) countNewLines(file DiffFile) int {
	count := 0
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			if line.Type == "+" || line.Type == " " {
				count++
			}
		}
	}
	return count
}

// countOldLines counts total old lines in a diff file.
func (p *DiffParser) countOldLines(file DiffFile) int {
	count := 0
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			if line.Type == "-" || line.Type == " " {
				count++
			}
		}
	}
	return count
}

// GetFilesChanged returns the list of files affected by the patch.
func (p *DiffParser) GetFilesChanged(patch string) ([]string, error) {
	files, err := p.ParseDiff(patch)
	if err != nil {
		return nil, err
	}

	var paths []string
	seen := make(map[string]bool)

	for _, file := range files {
		path := file.NewPath
		if path == "" || path == "/dev/null" {
			path = file.OldPath
		}
		if path != "" && path != "/dev/null" && !seen[path] {
			paths = append(paths, path)
			seen[path] = true
		}
	}

	return paths, nil
}
