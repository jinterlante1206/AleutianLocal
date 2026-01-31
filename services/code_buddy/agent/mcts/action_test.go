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
	"errors"
	"strings"
	"testing"
)

func TestPlannedAction_Validate_ValidAction(t *testing.T) {
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "pkg/auth/handler.go",
		Description: "Add nil check before token access",
		CodeDiff:    "if token == nil { return ErrNilToken }",
		Language:    "go",
	}

	config := DefaultActionValidationConfig()
	err := action.Validate("/project", config)

	if err != nil {
		t.Errorf("expected valid action, got error: %v", err)
	}

	if !action.IsValidated() {
		t.Error("expected IsValidated() to return true")
	}

	if action.ProjectRoot() != "/project" {
		t.Errorf("expected project root /project, got %s", action.ProjectRoot())
	}
}

func TestPlannedAction_Validate_AllActionTypes(t *testing.T) {
	tests := []struct {
		name       string
		actionType ActionType
	}{
		{"edit", ActionTypeEdit},
		{"create", ActionTypeCreate},
		{"delete", ActionTypeDelete},
		{"run_test", ActionTypeRunTest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := &PlannedAction{
				Type:        tt.actionType,
				Description: "Test action",
			}

			err := action.Validate("/project", DefaultActionValidationConfig())
			if err != nil {
				t.Errorf("expected valid action type %s, got error: %v", tt.actionType, err)
			}
		})
	}
}

func TestPlannedAction_Validate_InvalidActionType(t *testing.T) {
	action := &PlannedAction{
		Type:        ActionType("invalid"),
		Description: "Test",
	}

	err := action.Validate("/project", DefaultActionValidationConfig())
	if err == nil {
		t.Error("expected error for invalid action type")
	}
	if !errors.Is(err, ErrInvalidActionType) {
		t.Errorf("expected ErrInvalidActionType, got: %v", err)
	}
}

func TestPlannedAction_Validate_EmptyDescription(t *testing.T) {
	tests := []struct {
		name        string
		description string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"tabs and newlines", "\t\n\t"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := &PlannedAction{
				Type:        ActionTypeEdit,
				Description: tt.description,
			}

			err := action.Validate("/project", DefaultActionValidationConfig())
			if err == nil {
				t.Error("expected error for empty description")
			}
			if !errors.Is(err, ErrEmptyDescription) {
				t.Errorf("expected ErrEmptyDescription, got: %v", err)
			}
		})
	}
}

func TestPlannedAction_Validate_PathTraversal(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
	}{
		{"double dot prefix", "../etc/passwd"},
		{"double dot middle", "pkg/../../../etc/passwd"},
		{"absolute outside", "/etc/passwd"},
		{"hidden traversal", "pkg/../../etc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := &PlannedAction{
				Type:        ActionTypeEdit,
				FilePath:    tt.filePath,
				Description: "Malicious action",
			}

			err := action.Validate("/project", DefaultActionValidationConfig())

			if err == nil {
				t.Error("expected error for path traversal")
			}
			if !errors.Is(err, ErrPathOutsideBoundary) {
				t.Errorf("expected ErrPathOutsideBoundary, got: %v", err)
			}
		})
	}
}

func TestPlannedAction_Validate_ShellMetacharacters(t *testing.T) {
	unsafePaths := []string{
		"file;rm -rf /",
		"file|cat /etc/passwd",
		"file$(whoami)",
		"file`id`",
		"file&cmd",
		"file{a,b}",
		"file[a]",
		"file<in",
		"file>out",
		"file*glob",
		"file?any",
		"file!not",
		"file\\escaped",
	}

	for _, path := range unsafePaths {
		t.Run(path, func(t *testing.T) {
			action := &PlannedAction{
				Type:        ActionTypeEdit,
				FilePath:    path,
				Description: "Test",
			}

			err := action.Validate("/project", DefaultActionValidationConfig())

			if err == nil {
				t.Errorf("expected error for shell metacharacter in path: %s", path)
			}
			if !errors.Is(err, ErrUnsafePath) {
				t.Errorf("expected ErrUnsafePath, got: %v", err)
			}
		})
	}
}

func TestPlannedAction_Validate_NullByte(t *testing.T) {
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "file.go\x00.txt",
		Description: "Null byte attack",
	}

	err := action.Validate("/project", DefaultActionValidationConfig())

	if err == nil {
		t.Error("expected error for null byte")
	}
	if !errors.Is(err, ErrUnsafePath) {
		t.Errorf("expected ErrUnsafePath, got: %v", err)
	}
}

func TestPlannedAction_Validate_PathTooLong(t *testing.T) {
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    strings.Repeat("a", 600),
		Description: "Long path",
	}

	err := action.Validate("/project", DefaultActionValidationConfig())

	if err == nil {
		t.Error("expected error for long path")
	}
	if !errors.Is(err, ErrPathTooLong) {
		t.Errorf("expected ErrPathTooLong, got: %v", err)
	}
}

func TestPlannedAction_Validate_CodeDiffTooLarge(t *testing.T) {
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		Description: "Large diff",
		CodeDiff:    strings.Repeat("x", 200*1024), // 200KB
	}

	err := action.Validate("/project", DefaultActionValidationConfig())

	if err == nil {
		t.Error("expected error for large code diff")
	}
	if !errors.Is(err, ErrCodeDiffTooLarge) {
		t.Errorf("expected ErrCodeDiffTooLarge, got: %v", err)
	}
}

func TestPlannedAction_Validate_InvalidUTF8(t *testing.T) {
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		Description: "Invalid UTF-8",
		CodeDiff:    "invalid \xff\xfe utf8",
	}

	err := action.Validate("/project", DefaultActionValidationConfig())

	if err == nil {
		t.Error("expected error for invalid UTF-8")
	}
	if !errors.Is(err, ErrInvalidUTF8) {
		t.Errorf("expected ErrInvalidUTF8, got: %v", err)
	}
}

func TestPlannedAction_Validate_NilAction(t *testing.T) {
	var action *PlannedAction

	err := action.Validate("/project", DefaultActionValidationConfig())
	if err == nil {
		t.Error("expected error for nil action")
	}
}

func TestPlannedAction_AbsolutePath(t *testing.T) {
	tests := []struct {
		name        string
		filePath    string
		projectRoot string
		want        string
	}{
		{
			name:        "relative path",
			filePath:    "pkg/auth/handler.go",
			projectRoot: "/project",
			want:        "/project/pkg/auth/handler.go",
		},
		{
			name:        "absolute path within project",
			filePath:    "/project/pkg/auth/handler.go",
			projectRoot: "/project",
			want:        "/project/pkg/auth/handler.go",
		},
		{
			name:        "path with dots",
			filePath:    "pkg/./auth/../auth/handler.go",
			projectRoot: "/project",
			want:        "/project/pkg/auth/handler.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := &PlannedAction{
				Type:        ActionTypeEdit,
				FilePath:    tt.filePath,
				Description: "Test",
			}

			err := action.Validate(tt.projectRoot, DefaultActionValidationConfig())
			if err != nil {
				t.Fatalf("validate failed: %v", err)
			}

			got := action.AbsolutePath()
			if got != tt.want {
				t.Errorf("AbsolutePath() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestPlannedAction_AbsolutePath_EmptyFilePath(t *testing.T) {
	action := &PlannedAction{
		Type:        ActionTypeRunTest,
		Description: "Run all tests",
	}

	err := action.Validate("/project", DefaultActionValidationConfig())
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}

	got := action.AbsolutePath()
	if got != "" {
		t.Errorf("AbsolutePath() = %s, want empty string", got)
	}
}

func TestPlannedAction_MustValidate_Panic(t *testing.T) {
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		Description: "Unvalidated",
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for unvalidated action")
		}
	}()

	action.MustValidate()
}

func TestPlannedAction_Clone(t *testing.T) {
	original := &PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "pkg/auth/handler.go",
		Description: "Add nil check",
		CodeDiff:    "if token == nil { return }",
		Language:    "go",
	}

	// Validate original
	err := original.Validate("/project", DefaultActionValidationConfig())
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}

	// Clone
	cloned := original.Clone()

	// Check fields are copied
	if cloned.Type != original.Type {
		t.Errorf("Type mismatch: got %s, want %s", cloned.Type, original.Type)
	}
	if cloned.FilePath != original.FilePath {
		t.Errorf("FilePath mismatch: got %s, want %s", cloned.FilePath, original.FilePath)
	}
	if cloned.Description != original.Description {
		t.Errorf("Description mismatch: got %s, want %s", cloned.Description, original.Description)
	}
	if cloned.CodeDiff != original.CodeDiff {
		t.Errorf("CodeDiff mismatch")
	}
	if cloned.Language != original.Language {
		t.Errorf("Language mismatch: got %s, want %s", cloned.Language, original.Language)
	}

	// Clone should not be validated
	if cloned.IsValidated() {
		t.Error("clone should not be validated")
	}
	if cloned.ProjectRoot() != "" {
		t.Error("clone should not have project root")
	}
}

func TestPlannedAction_Clone_Nil(t *testing.T) {
	var action *PlannedAction
	cloned := action.Clone()
	if cloned != nil {
		t.Error("clone of nil should be nil")
	}
}

func TestActionType_String(t *testing.T) {
	tests := []struct {
		actionType ActionType
		want       string
	}{
		{ActionTypeEdit, "edit"},
		{ActionTypeCreate, "create"},
		{ActionTypeDelete, "delete"},
		{ActionTypeRunTest, "run_test"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.actionType.String()
			if got != tt.want {
				t.Errorf("String() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestDefaultActionValidationConfig(t *testing.T) {
	config := DefaultActionValidationConfig()

	if config.MaxPathLength != 500 {
		t.Errorf("MaxPathLength = %d, want 500", config.MaxPathLength)
	}
	if config.MaxCodeDiffBytes != 100*1024 {
		t.Errorf("MaxCodeDiffBytes = %d, want 102400", config.MaxCodeDiffBytes)
	}
	if len(config.AllowedLanguages) == 0 {
		t.Error("AllowedLanguages should not be empty")
	}
}

func TestPlannedAction_Validate_ValidPathsWithinProject(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
	}{
		{"simple file", "main.go"},
		{"nested file", "pkg/auth/handler.go"},
		{"deep nesting", "internal/domain/models/user/user.go"},
		{"with dots in name", "pkg/.gitkeep"},
		{"underscores", "pkg/auth_test.go"},
		{"numbers", "pkg/v2/handler.go"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := &PlannedAction{
				Type:        ActionTypeEdit,
				FilePath:    tt.filePath,
				Description: "Test",
			}

			err := action.Validate("/project", DefaultActionValidationConfig())
			if err != nil {
				t.Errorf("expected valid path %q, got error: %v", tt.filePath, err)
			}
		})
	}
}
