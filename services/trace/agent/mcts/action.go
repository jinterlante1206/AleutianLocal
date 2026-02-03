// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package mcts

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ActionType represents valid action types.
type ActionType string

const (
	ActionTypeEdit    ActionType = "edit"
	ActionTypeCreate  ActionType = "create"
	ActionTypeDelete  ActionType = "delete"
	ActionTypeRunTest ActionType = "run_test"
)

// String returns the string representation of the action type.
func (a ActionType) String() string {
	return string(a)
}

// ValidActionTypes is the exhaustive list of valid action types.
var ValidActionTypes = map[ActionType]bool{
	ActionTypeEdit:    true,
	ActionTypeCreate:  true,
	ActionTypeDelete:  true,
	ActionTypeRunTest: true,
}

// PlannedAction represents a validated action from the LLM.
//
// Thread Safety: Designed for validate-once-then-use pattern.
// After Validate() returns successfully, the action is immutable and safe
// for concurrent reads. Do not call Validate() concurrently or modify
// fields after validation. Clone() creates an unvalidated copy that must
// be validated separately before use.
type PlannedAction struct {
	Type        ActionType `json:"type"`
	FilePath    string     `json:"file_path,omitempty"`
	Description string     `json:"description"`
	CodeDiff    string     `json:"code_diff,omitempty"`
	Language    string     `json:"language,omitempty"`

	// Validation metadata (not serialized)
	validated   bool
	projectRoot string
}

// ActionValidationConfig contains validation limits.
type ActionValidationConfig struct {
	MaxPathLength    int      // Default: 500
	MaxCodeDiffBytes int      // Default: 100KB
	AllowedLanguages []string // Default: common languages
}

// DefaultActionValidationConfig returns sensible defaults.
func DefaultActionValidationConfig() ActionValidationConfig {
	return ActionValidationConfig{
		MaxPathLength:    500,
		MaxCodeDiffBytes: 100 * 1024, // 100KB
		AllowedLanguages: []string{"go", "python", "typescript", "javascript", "java", "rust", "c", "cpp"},
	}
}

// unsafePathChars matches shell metacharacters that could enable injection.
var unsafePathChars = regexp.MustCompile(`[;&|$` + "`" + `(){}\[\]<>*?!\\]`)

// Validate checks the PlannedAction for safety and correctness.
//
// Inputs:
//   - projectRoot: Absolute path to project root. All file paths must be within this.
//   - config: Validation configuration with limits.
//
// Outputs:
//   - error: Non-nil if validation fails. Contains specific error type.
//
// Thread Safety: Safe for concurrent use (read-only on receiver until validated flag set).
func (a *PlannedAction) Validate(projectRoot string, config ActionValidationConfig) error {
	if a == nil {
		return fmt.Errorf("action is nil")
	}

	// 1. Action type validation
	if !ValidActionTypes[a.Type] {
		return fmt.Errorf("%w: %q", ErrInvalidActionType, a.Type)
	}

	// 2. Description must not be empty
	if strings.TrimSpace(a.Description) == "" {
		return ErrEmptyDescription
	}

	// 3. File path validation (if present)
	if a.FilePath != "" {
		if err := a.validateFilePath(projectRoot, config); err != nil {
			return err
		}
	}

	// 4. Code diff validation (if present)
	if a.CodeDiff != "" {
		if err := a.validateCodeDiff(config); err != nil {
			return err
		}
	}

	a.validated = true
	a.projectRoot = projectRoot
	return nil
}

// validateFilePath checks the file path for safety.
func (a *PlannedAction) validateFilePath(projectRoot string, config ActionValidationConfig) error {
	// Length check
	if len(a.FilePath) > config.MaxPathLength {
		return fmt.Errorf("%w: %d > %d", ErrPathTooLong, len(a.FilePath), config.MaxPathLength)
	}

	// Shell metacharacter check
	if unsafePathChars.MatchString(a.FilePath) {
		return fmt.Errorf("%w: contains shell metacharacters", ErrUnsafePath)
	}

	// Null byte check (path truncation attack)
	if strings.Contains(a.FilePath, "\x00") {
		return fmt.Errorf("%w: contains null byte", ErrUnsafePath)
	}

	// Path traversal check
	cleanPath := filepath.Clean(a.FilePath)
	if strings.HasPrefix(cleanPath, "..") || strings.Contains(cleanPath, "/../") {
		return fmt.Errorf("%w: path traversal detected", ErrPathOutsideBoundary)
	}

	// Absolute path must be within project root
	absPath := a.FilePath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(projectRoot, a.FilePath)
	}
	absPath = filepath.Clean(absPath)

	// Ensure the resolved path is still within project root
	projectRoot = filepath.Clean(projectRoot)
	if !strings.HasPrefix(absPath, projectRoot+string(filepath.Separator)) && absPath != projectRoot {
		return fmt.Errorf("%w: %s is not within %s", ErrPathOutsideBoundary, absPath, projectRoot)
	}

	return nil
}

// validateCodeDiff checks the code diff for safety.
func (a *PlannedAction) validateCodeDiff(config ActionValidationConfig) error {
	// Size check
	if len(a.CodeDiff) > config.MaxCodeDiffBytes {
		return fmt.Errorf("%w: %d > %d bytes", ErrCodeDiffTooLarge, len(a.CodeDiff), config.MaxCodeDiffBytes)
	}

	// UTF-8 validity check
	if !utf8.ValidString(a.CodeDiff) {
		return ErrInvalidUTF8
	}

	return nil
}

// IsValidated returns whether this action has been validated.
func (a *PlannedAction) IsValidated() bool {
	return a.validated
}

// MustValidate panics if the action hasn't been validated.
// Use this in code paths that require validation.
func (a *PlannedAction) MustValidate() {
	if !a.validated {
		panic("PlannedAction used without validation - call Validate() first")
	}
}

// ProjectRoot returns the project root this action was validated against.
// Returns empty string if not validated.
func (a *PlannedAction) ProjectRoot() string {
	return a.projectRoot
}

// AbsolutePath returns the absolute file path within the project.
// Panics if not validated. Returns empty string if no file path.
func (a *PlannedAction) AbsolutePath() string {
	a.MustValidate()
	if a.FilePath == "" {
		return ""
	}
	if filepath.IsAbs(a.FilePath) {
		return filepath.Clean(a.FilePath)
	}
	return filepath.Clean(filepath.Join(a.projectRoot, a.FilePath))
}

// Clone creates a deep copy of the action (without validation state).
func (a *PlannedAction) Clone() *PlannedAction {
	if a == nil {
		return nil
	}
	return &PlannedAction{
		Type:        a.Type,
		FilePath:    a.FilePath,
		Description: a.Description,
		CodeDiff:    a.CodeDiff,
		Language:    a.Language,
		// validated and projectRoot are not copied - clone needs re-validation
	}
}
