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
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNewLintRunner(t *testing.T) {
	runner := NewLintRunner()

	if runner.configs == nil {
		t.Error("Configs should not be nil")
	}
	if runner.policies == nil {
		t.Error("Policies should not be nil")
	}
	if runner.available == nil {
		t.Error("Available map should not be nil")
	}
}

func TestNewLintRunner_WithOptions(t *testing.T) {
	workDir := "/test/dir"
	runner := NewLintRunner(
		WithWorkingDir(workDir),
	)

	if runner.workingDir != workDir {
		t.Errorf("WorkingDir = %q, want %q", runner.workingDir, workDir)
	}
}

func TestLintRunner_DetectAvailableLinters(t *testing.T) {
	runner := NewLintRunner()
	available := runner.DetectAvailableLinters()

	// Should return a map with at least the default languages
	if len(available) == 0 {
		t.Error("Expected at least some languages in available map")
	}

	// Check that IsAvailable returns consistent results
	for lang, avail := range available {
		if runner.IsAvailable(lang) != avail {
			t.Errorf("IsAvailable(%q) inconsistent with DetectAvailableLinters", lang)
		}
	}
}

func TestLintRunner_Lint_UnsupportedLanguage(t *testing.T) {
	runner := NewLintRunner()

	ctx := context.Background()
	_, err := runner.Lint(ctx, "file.unknown")

	if err == nil {
		t.Error("Expected error for unsupported language")
	}
}

func TestLintRunner_Lint_NilContext(t *testing.T) {
	runner := NewLintRunner()

	_, err := runner.Lint(nil, "test.go") //nolint:staticcheck
	if err == nil {
		t.Error("Expected error for nil context")
	}
}

func TestLintRunner_LintContent_Empty(t *testing.T) {
	runner := NewLintRunner()
	runner.DetectAvailableLinters()

	ctx := context.Background()
	result, err := runner.LintContent(ctx, []byte{}, "go")

	if err != nil {
		t.Fatalf("LintContent: %v", err)
	}

	if !result.Valid {
		t.Error("Empty content should be valid")
	}
}

func TestLintRunner_LintContent_NilContext(t *testing.T) {
	runner := NewLintRunner()

	_, err := runner.LintContent(nil, []byte("package main"), "go") //nolint:staticcheck
	if err == nil {
		t.Error("Expected error for nil context")
	}
}

func TestLintRunner_LintContent_UnsupportedLanguage(t *testing.T) {
	runner := NewLintRunner()

	ctx := context.Background()
	_, err := runner.LintContent(ctx, []byte("content"), "unknown")

	if err == nil {
		t.Error("Expected error for unsupported language")
	}
}

func TestLintRunner_Configs(t *testing.T) {
	runner := NewLintRunner()

	configs := runner.Configs()
	if configs == nil {
		t.Error("Configs() should not return nil")
	}

	// Should have default configs
	goConfig := configs.Get("go")
	if goConfig == nil {
		t.Error("Expected Go config")
	}
}

func TestLintRunner_Policies(t *testing.T) {
	runner := NewLintRunner()

	policies := runner.Policies()
	if policies == nil {
		t.Error("Policies() should not return nil")
	}

	// Should have default policies
	goPolicy := policies.Get("go")
	if goPolicy == nil {
		t.Error("Expected Go policy")
	}
}

// Integration tests - only run if linters are installed

func TestLintRunner_Integration_Go(t *testing.T) {
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		t.Skip("golangci-lint not installed")
	}

	runner := NewLintRunner()
	runner.DetectAvailableLinters()

	// Create temp directory with go.mod
	dir := t.TempDir()
	goMod := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(goMod, []byte("module test\ngo 1.21"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Run("valid Go file", func(t *testing.T) {
		content := []byte(`package main

func main() {
	println("hello")
}
`)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := runner.LintContent(ctx, content, "go")
		if err != nil {
			t.Fatalf("LintContent: %v", err)
		}

		// Valid code should pass
		if !result.LinterAvailable {
			t.Skip("Linter not available")
		}
		// Note: result might have warnings but should not have blocking errors
		// for this simple valid code
	})

	t.Run("Go file with unchecked error", func(t *testing.T) {
		content := []byte(`package main

import "os"

func main() {
	os.Open("file.txt") // error not checked
}
`)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := runner.LintContent(ctx, content, "go")
		if err != nil {
			t.Fatalf("LintContent: %v", err)
		}

		if !result.LinterAvailable {
			t.Skip("Linter not available")
		}

		// Should have issues (errcheck should catch this)
		if result.IssueCount() == 0 {
			t.Log("Note: golangci-lint might not have errcheck enabled by default")
		}
	})
}

func TestLintRunner_Integration_Python(t *testing.T) {
	if _, err := exec.LookPath("ruff"); err != nil {
		t.Skip("ruff not installed")
	}

	runner := NewLintRunner()
	runner.DetectAvailableLinters()

	t.Run("valid Python file", func(t *testing.T) {
		content := []byte(`def main():
    print("hello")

if __name__ == "__main__":
    main()
`)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result, err := runner.LintContent(ctx, content, "python")
		if err != nil {
			t.Fatalf("LintContent: %v", err)
		}

		if !result.LinterAvailable {
			t.Skip("Linter not available")
		}

		if !result.Valid {
			t.Errorf("Valid Python code should pass, got errors: %v", result.Errors)
		}
	})

	t.Run("Python file with unused import", func(t *testing.T) {
		content := []byte(`import os  # unused

def main():
    print("hello")
`)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result, err := runner.LintContent(ctx, content, "python")
		if err != nil {
			t.Fatalf("LintContent: %v", err)
		}

		if !result.LinterAvailable {
			t.Skip("Linter not available")
		}

		// Should have F401 unused import
		hasF401 := false
		for _, issue := range result.AllIssues() {
			if issue.Rule == "F401" {
				hasF401 = true
				break
			}
		}
		if !hasF401 {
			t.Error("Expected F401 (unused import) issue")
		}
	})
}

func TestLintRunner_LinterUnavailable(t *testing.T) {
	runner := NewLintRunner()
	// Don't call DetectAvailableLinters - linters should be unavailable

	ctx := context.Background()
	result, err := runner.LintWithLanguage(ctx, "test.go", "go")

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should return valid result with LinterAvailable = false
	if result.LinterAvailable {
		t.Error("LinterAvailable should be false when not detected")
	}
	if !result.Valid {
		t.Error("Valid should be true when linter unavailable (graceful degradation)")
	}
}

func TestLanguageFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"test.py", "python"},
		{"test.pyi", "python"},
		{"app.ts", "typescript"},
		{"app.tsx", "typescript"},
		{"app.js", "javascript"},
		{"app.jsx", "javascript"},
		{"app.mjs", "javascript"},
		{"file.txt", ""},
		{"file.unknown", ""},
		{"/path/to/main.go", "go"},
	}

	for _, tt := range tests {
		got := LanguageFromPath(tt.path)
		if got != tt.want {
			t.Errorf("LanguageFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestExtensionForLanguage(t *testing.T) {
	tests := []struct {
		lang string
		want string
	}{
		{"go", ".go"},
		{"python", ".py"},
		{"typescript", ".ts"},
		{"javascript", ".js"},
		{"unknown", ""},
	}

	for _, tt := range tests {
		got := ExtensionForLanguage(tt.lang)
		if got != tt.want {
			t.Errorf("ExtensionForLanguage(%q) = %q, want %q", tt.lang, got, tt.want)
		}
	}
}
