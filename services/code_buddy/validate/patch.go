// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package validate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
	"github.com/sourcegraph/go-diff/diff"
)

// PatchValidator validates patches for safety.
//
// Thread Safety: Individual Validate calls are safe for concurrent use.
// The validator maintains no state between calls. Tree-sitter parsers
// are created per-call to avoid sharing issues.
type PatchValidator struct {
	config        ValidatorConfig
	astScanner    *ASTScanner
	secretScanner *SecretScanner
}

// NewPatchValidator creates a new patch validator.
//
// Description:
//
//	Creates a PatchValidator with the given configuration. The validator
//	checks patches for size limits, syntax errors, dangerous patterns,
//	and hardcoded secrets.
//
// Inputs:
//
//	config - Validator configuration
//
// Outputs:
//
//	*PatchValidator - The configured validator
//	error - Non-nil if configuration is invalid
//
// Thread Safety: Safe to share between goroutines.
func NewPatchValidator(config ValidatorConfig) (*PatchValidator, error) {
	secretScanner, err := NewSecretScanner(config)
	if err != nil {
		return nil, fmt.Errorf("creating secret scanner: %w", err)
	}

	return &PatchValidator{
		config:        config,
		astScanner:    NewASTScanner(),
		secretScanner: secretScanner,
	}, nil
}

// Validate validates a patch for safety.
//
// Description:
//
//	Runs a multi-stage validation pipeline on the patch:
//	1. Size check - reject patches over maxLines
//	2. Diff parsing - validate diff format
//	3. Syntax validation - parse full file after applying patch
//	4. Pattern scanning - AST-based dangerous pattern detection
//	5. Secret scanning - check for hardcoded secrets
//	6. Permission check - verify files are writable
//
// Inputs:
//
//	ctx - Context for cancellation
//	patchContent - The patch content (unified diff format)
//	projectRoot - Project root directory for file resolution
//
// Outputs:
//
//	*ValidationResult - Validation result with errors and warnings
//	error - Non-nil if validation pipeline itself fails
//
// Thread Safety: Safe for concurrent use. Parser created per-call.
func (v *PatchValidator) Validate(ctx context.Context, patchContent, projectRoot string) (*ValidationResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	result := &ValidationResult{
		Valid:          true,
		Errors:         make([]ValidationError, 0),
		Warnings:       make([]ValidationWarning, 0),
		Permissions:    make([]PermissionIssue, 0),
		Stats:          PatchStats{},
		PatternVersion: PatternVersion,
		ValidatedAt:    time.Now(),
	}

	// 1. Size check
	if err := v.checkSize(patchContent, result); err != nil {
		return result, nil // Size error is in result, not a pipeline failure
	}

	// Check context
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// 2. Parse diff
	fileDiffs, err := v.parseDiff(patchContent)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Type:    ErrorTypeDiffParse,
			Message: fmt.Sprintf("Invalid diff format: %v", err),
		})
		return result, nil
	}

	// Calculate stats
	result.Stats = v.calculateStats(fileDiffs)

	// Check context
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Process each file in the diff
	for _, fileDiff := range fileDiffs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		filePath := fileDiff.NewName
		if filePath == "" || filePath == "/dev/null" {
			filePath = fileDiff.OrigName
		}

		// Strip a/ or b/ prefix from git diffs
		filePath = strings.TrimPrefix(filePath, "a/")
		filePath = strings.TrimPrefix(filePath, "b/")

		absPath := filepath.Join(projectRoot, filePath)
		language := detectLanguage(filePath)

		// 3. Syntax validation (full file)
		if err := v.checkSyntax(ctx, absPath, fileDiff, language, result); err != nil {
			// Internal error, not validation error
			return nil, fmt.Errorf("checking syntax for %s: %w", filePath, err)
		}

		// 4. Pattern scanning
		if err := v.scanPatterns(ctx, absPath, fileDiff, language, filePath, result); err != nil {
			return nil, fmt.Errorf("scanning patterns for %s: %w", filePath, err)
		}

		// 5. Secret scanning
		v.scanSecrets(absPath, fileDiff, filePath, result)

		// 6. Permission check
		v.checkPermissions(absPath, filePath, fileDiff, result)
	}

	// Determine final validity
	v.determineValidity(result)

	return result, nil
}

// checkSize validates the patch size.
func (v *PatchValidator) checkSize(patch string, result *ValidationResult) error {
	lines := strings.Count(patch, "\n")
	if lines > v.config.MaxLines {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Type:    ErrorTypeSizeLimit,
			Message: fmt.Sprintf("Patch exceeds maximum size: %d lines (max %d)", lines, v.config.MaxLines),
		})
	}
	return nil
}

// parseDiff parses the unified diff format.
func (v *PatchValidator) parseDiff(patch string) ([]*diff.FileDiff, error) {
	reader := strings.NewReader(patch)
	return diff.NewMultiFileDiffReader(reader).ReadAllFiles()
}

// calculateStats calculates patch statistics.
func (v *PatchValidator) calculateStats(fileDiffs []*diff.FileDiff) PatchStats {
	stats := PatchStats{
		FilesAffected: len(fileDiffs),
	}

	for _, fd := range fileDiffs {
		for _, hunk := range fd.Hunks {
			for _, line := range strings.Split(string(hunk.Body), "\n") {
				if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
					stats.LinesAdded++
				} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
					stats.LinesRemoved++
				}
			}
		}
	}

	return stats
}

// checkSyntax validates that the patched file will have valid syntax.
func (v *PatchValidator) checkSyntax(ctx context.Context, absPath string, fileDiff *diff.FileDiff, language string, result *ValidationResult) error {
	if language == "" {
		return nil // Unknown language, skip syntax check
	}

	// Read original file
	var original []byte
	if _, err := os.Stat(absPath); err == nil {
		original, err = os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("reading file: %w", err)
		}
	}

	// Apply diff to get new content
	newContent, err := v.applyDiff(original, fileDiff)
	if err != nil {
		result.Errors = append(result.Errors, ValidationError{
			Type:    ErrorTypeDiffParse,
			File:    absPath,
			Message: fmt.Sprintf("Cannot apply diff: %v", err),
		})
		return nil
	}

	// Parse full file with tree-sitter
	parser := sitter.NewParser()
	defer parser.Close()

	var lang *sitter.Language
	switch language {
	case "go":
		lang = golang.GetLanguage()
	case "python":
		lang = python.GetLanguage()
	case "javascript":
		lang = javascript.GetLanguage()
	case "typescript":
		lang = typescript.GetLanguage()
	default:
		return nil // Unknown language
	}

	parser.SetLanguage(lang)

	tree, err := parser.ParseCtx(ctx, nil, newContent)
	if err != nil {
		return fmt.Errorf("parsing: %w", err)
	}
	defer tree.Close()

	// Check for syntax errors
	root := tree.RootNode()
	if hasSyntaxError(root) {
		result.Valid = false
		errNode := findFirstError(root)
		line := 0
		if errNode != nil {
			line = int(errNode.StartPoint().Row) + 1
		}
		result.Errors = append(result.Errors, ValidationError{
			Type:    ErrorTypeSyntax,
			File:    absPath,
			Line:    line,
			Message: "Syntax error in patched file",
		})
	}

	return nil
}

// applyDiff applies a file diff to the original content.
func (v *PatchValidator) applyDiff(original []byte, fileDiff *diff.FileDiff) ([]byte, error) {
	if fileDiff.NewName == "/dev/null" {
		// File deletion
		return nil, nil
	}

	if fileDiff.OrigName == "/dev/null" || len(original) == 0 {
		// New file - extract added lines from hunks
		var lines []string
		for _, hunk := range fileDiff.Hunks {
			for _, line := range strings.Split(string(hunk.Body), "\n") {
				if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
					lines = append(lines, strings.TrimPrefix(line, "+"))
				}
			}
		}
		return []byte(strings.Join(lines, "\n")), nil
	}

	// Apply hunks to original
	origLines := strings.Split(string(original), "\n")
	newLines := make([]string, 0, len(origLines))

	origIdx := 0
	for _, hunk := range fileDiff.Hunks {
		// Copy lines before this hunk
		hunkStart := int(hunk.OrigStartLine) - 1
		for origIdx < hunkStart && origIdx < len(origLines) {
			newLines = append(newLines, origLines[origIdx])
			origIdx++
		}

		// Process hunk
		for _, line := range strings.Split(string(hunk.Body), "\n") {
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				// Added line
				newLines = append(newLines, strings.TrimPrefix(line, "+"))
			} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
				// Removed line - skip in original
				origIdx++
			} else if strings.HasPrefix(line, " ") || line == "" {
				// Context line
				if origIdx < len(origLines) {
					newLines = append(newLines, origLines[origIdx])
					origIdx++
				}
			}
		}
	}

	// Copy remaining lines
	for origIdx < len(origLines) {
		newLines = append(newLines, origLines[origIdx])
		origIdx++
	}

	return []byte(strings.Join(newLines, "\n")), nil
}

// scanPatterns scans for dangerous patterns.
func (v *PatchValidator) scanPatterns(ctx context.Context, absPath string, fileDiff *diff.FileDiff, language, relPath string, result *ValidationResult) error {
	// Get added content for scanning
	var addedContent []byte
	for _, hunk := range fileDiff.Hunks {
		for _, line := range strings.Split(string(hunk.Body), "\n") {
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				addedContent = append(addedContent, []byte(strings.TrimPrefix(line, "+")+"\n")...)
			}
		}
	}

	if len(addedContent) == 0 {
		return nil
	}

	// AST-based pattern scanning
	warnings, err := v.astScanner.Scan(ctx, addedContent, language, relPath)
	if err != nil {
		// Log but don't fail
		return nil
	}
	result.Warnings = append(result.Warnings, warnings...)

	// SQL injection scanning
	sqlWarnings := v.astScanner.ScanForSQLInjection(ctx, addedContent, language, relPath)
	result.Warnings = append(result.Warnings, sqlWarnings...)

	return nil
}

// scanSecrets scans for hardcoded secrets.
func (v *PatchValidator) scanSecrets(absPath string, fileDiff *diff.FileDiff, relPath string, result *ValidationResult) {
	// Get added content
	var addedContent []byte
	for _, hunk := range fileDiff.Hunks {
		for _, line := range strings.Split(string(hunk.Body), "\n") {
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				addedContent = append(addedContent, []byte(strings.TrimPrefix(line, "+")+"\n")...)
			}
		}
	}

	if len(addedContent) == 0 {
		return
	}

	warnings := v.secretScanner.Scan(addedContent, relPath)
	result.Warnings = append(result.Warnings, warnings...)
}

// checkPermissions validates file permissions.
func (v *PatchValidator) checkPermissions(absPath, relPath string, fileDiff *diff.FileDiff, result *ValidationResult) {
	if fileDiff.NewName == "/dev/null" {
		// Deletion - need write permission on file
		info, err := os.Stat(absPath)
		if err != nil {
			if !os.IsNotExist(err) {
				result.Permissions = append(result.Permissions, PermissionIssue{
					File:  relPath,
					Issue: "access_error",
				})
			}
			return
		}
		if info.Mode().Perm()&0200 == 0 {
			result.Permissions = append(result.Permissions, PermissionIssue{
				File:  relPath,
				Issue: "not_writable",
			})
		}
		return
	}

	// Check if file exists
	info, err := os.Stat(absPath)
	if os.IsNotExist(err) {
		// New file - check if parent dir is writable
		parentDir := filepath.Dir(absPath)
		if !isDirWritable(parentDir) {
			result.Permissions = append(result.Permissions, PermissionIssue{
				File:  relPath,
				Issue: "parent_not_writable",
			})
		}
		return
	}
	if err != nil {
		result.Permissions = append(result.Permissions, PermissionIssue{
			File:  relPath,
			Issue: "access_error",
		})
		return
	}

	// Existing file - check if writable
	if info.Mode().Perm()&0200 == 0 {
		result.Permissions = append(result.Permissions, PermissionIssue{
			File:  relPath,
			Issue: "not_writable",
		})
	}
}

// determineValidity sets the final valid flag based on errors and blocking warnings.
func (v *PatchValidator) determineValidity(result *ValidationResult) {
	// Already invalid from errors
	if !result.Valid {
		return
	}

	// Check for blocking warnings
	if !v.config.WarnOnly && v.config.BlockDangerous {
		for _, w := range result.Warnings {
			if w.Blocking {
				result.Valid = false
				return
			}
		}
	}

	// Check for permission issues
	if len(result.Permissions) > 0 {
		result.Valid = false
	}
}

// hasSyntaxError checks if the AST has syntax errors.
func hasSyntaxError(node *sitter.Node) bool {
	if node == nil {
		return false
	}
	if node.IsError() || node.IsMissing() {
		return true
	}
	for i := uint32(0); i < node.ChildCount(); i++ {
		if hasSyntaxError(node.Child(int(i))) {
			return true
		}
	}
	return false
}

// findFirstError finds the first error node in the AST.
func findFirstError(node *sitter.Node) *sitter.Node {
	if node == nil {
		return nil
	}
	if node.IsError() || node.IsMissing() {
		return node
	}
	for i := uint32(0); i < node.ChildCount(); i++ {
		if err := findFirstError(node.Child(int(i))); err != nil {
			return err
		}
	}
	return nil
}

// detectLanguage detects the programming language from file extension.
func detectLanguage(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		return "go"
	case ".py", ".pyi":
		return "python"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx", ".mts", ".cts":
		return "typescript"
	default:
		return ""
	}
}

// isDirWritable checks if a directory is writable.
func isDirWritable(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		return false
	}
	// Check write permission
	return info.Mode().Perm()&0200 != 0
}
