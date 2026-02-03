// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package lint

import (
	"testing"
)

func TestSeverity_String(t *testing.T) {
	tests := []struct {
		severity Severity
		want     string
	}{
		{SeverityInfo, "info"},
		{SeverityWarning, "warning"},
		{SeverityError, "error"},
		{Severity(99), "unknown"},
	}

	for _, tt := range tests {
		got := tt.severity.String()
		if got != tt.want {
			t.Errorf("Severity(%d).String() = %q, want %q", tt.severity, got, tt.want)
		}
	}
}

func TestSeverityFromString(t *testing.T) {
	tests := []struct {
		input string
		want  Severity
	}{
		{"error", SeverityError},
		{"err", SeverityError},
		{"fatal", SeverityError},
		{"critical", SeverityError},
		{"warning", SeverityWarning},
		{"warn", SeverityWarning},
		{"info", SeverityInfo},
		{"note", SeverityInfo},
		{"style", SeverityInfo},
		{"hint", SeverityInfo},
		{"unknown", SeverityWarning}, // default
		{"", SeverityWarning},        // default
	}

	for _, tt := range tests {
		got := SeverityFromString(tt.input)
		if got != tt.want {
			t.Errorf("SeverityFromString(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestLinterConfig_Clone(t *testing.T) {
	original := &LinterConfig{
		Language:      "go",
		Command:       "golangci-lint",
		Args:          []string{"run", "--out-format=json"},
		Extensions:    []string{".go"},
		Timeout:       30,
		Available:     true,
		SupportsStdin: false,
		FixArgs:       []string{"run", "--fix"},
	}

	clone := original.Clone()

	// Verify values are copied
	if clone.Language != original.Language {
		t.Errorf("Language not cloned")
	}
	if clone.Command != original.Command {
		t.Errorf("Command not cloned")
	}

	// Modify clone and verify original is unchanged
	clone.Args[0] = "check"
	if original.Args[0] != "run" {
		t.Errorf("Modifying clone affected original Args")
	}

	clone.Extensions[0] = ".py"
	if original.Extensions[0] != ".go" {
		t.Errorf("Modifying clone affected original Extensions")
	}
}

func TestLintResult_HasErrors(t *testing.T) {
	t.Run("no errors", func(t *testing.T) {
		r := &LintResult{Errors: []LintIssue{}}
		if r.HasErrors() {
			t.Error("HasErrors() should return false for empty errors")
		}
	})

	t.Run("with errors", func(t *testing.T) {
		r := &LintResult{Errors: []LintIssue{{Rule: "test"}}}
		if !r.HasErrors() {
			t.Error("HasErrors() should return true when errors exist")
		}
	})
}

func TestLintResult_HasWarnings(t *testing.T) {
	t.Run("no warnings", func(t *testing.T) {
		r := &LintResult{Warnings: []LintIssue{}}
		if r.HasWarnings() {
			t.Error("HasWarnings() should return false for empty warnings")
		}
	})

	t.Run("with warnings", func(t *testing.T) {
		r := &LintResult{Warnings: []LintIssue{{Rule: "test"}}}
		if !r.HasWarnings() {
			t.Error("HasWarnings() should return true when warnings exist")
		}
	})
}

func TestLintResult_HasIssues(t *testing.T) {
	t.Run("no issues", func(t *testing.T) {
		r := &LintResult{}
		if r.HasIssues() {
			t.Error("HasIssues() should return false when no issues")
		}
	})

	t.Run("with errors only", func(t *testing.T) {
		r := &LintResult{Errors: []LintIssue{{Rule: "test"}}}
		if !r.HasIssues() {
			t.Error("HasIssues() should return true when errors exist")
		}
	})

	t.Run("with warnings only", func(t *testing.T) {
		r := &LintResult{Warnings: []LintIssue{{Rule: "test"}}}
		if !r.HasIssues() {
			t.Error("HasIssues() should return true when warnings exist")
		}
	})

	t.Run("with infos only", func(t *testing.T) {
		r := &LintResult{Infos: []LintIssue{{Rule: "test"}}}
		if !r.HasIssues() {
			t.Error("HasIssues() should return true when infos exist")
		}
	})
}

func TestLintResult_AllIssues(t *testing.T) {
	r := &LintResult{
		Errors:   []LintIssue{{Rule: "error1"}, {Rule: "error2"}},
		Warnings: []LintIssue{{Rule: "warn1"}},
		Infos:    []LintIssue{{Rule: "info1"}, {Rule: "info2"}, {Rule: "info3"}},
	}

	all := r.AllIssues()
	if len(all) != 6 {
		t.Errorf("AllIssues() returned %d issues, want 6", len(all))
	}

	// Verify order: errors, warnings, infos
	if all[0].Rule != "error1" {
		t.Error("First issue should be error1")
	}
	if all[2].Rule != "warn1" {
		t.Error("Third issue should be warn1")
	}
	if all[3].Rule != "info1" {
		t.Error("Fourth issue should be info1")
	}
}

func TestLintResult_IssueCount(t *testing.T) {
	r := &LintResult{
		Errors:   []LintIssue{{Rule: "e1"}, {Rule: "e2"}},
		Warnings: []LintIssue{{Rule: "w1"}},
		Infos:    []LintIssue{{Rule: "i1"}, {Rule: "i2"}},
	}

	if r.IssueCount() != 5 {
		t.Errorf("IssueCount() = %d, want 5", r.IssueCount())
	}
}

func TestLintResult_AutoFixableCount(t *testing.T) {
	r := &LintResult{
		Errors:   []LintIssue{{CanAutoFix: true}, {CanAutoFix: false}},
		Warnings: []LintIssue{{CanAutoFix: true}, {CanAutoFix: true}},
	}

	if r.AutoFixableCount() != 3 {
		t.Errorf("AutoFixableCount() = %d, want 3", r.AutoFixableCount())
	}
}

func TestLintIssue_Location(t *testing.T) {
	t.Run("with column", func(t *testing.T) {
		i := LintIssue{File: "test.go", Line: 10, Column: 5}
		want := "test.go:10:5"
		if i.Location() != want {
			t.Errorf("Location() = %q, want %q", i.Location(), want)
		}
	})

	t.Run("without column", func(t *testing.T) {
		i := LintIssue{File: "test.go", Line: 10, Column: 0}
		want := "test.go:10"
		if i.Location() != want {
			t.Errorf("Location() = %q, want %q", i.Location(), want)
		}
	})
}

func TestItoa(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{10, "10"},
		{123, "123"},
		{-1, "-1"},
		{-123, "-123"},
	}

	for _, tt := range tests {
		got := itoa(tt.input)
		if got != tt.want {
			t.Errorf("itoa(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
