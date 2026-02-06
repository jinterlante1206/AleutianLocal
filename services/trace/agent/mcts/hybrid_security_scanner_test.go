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
	"context"
	"testing"
	"time"
)

func TestHybridSecurityScanner_IgnoresComments_Go(t *testing.T) {
	scanner := NewHybridSecurityScanner()
	ctx := context.Background()

	// This code has exec.Command in a comment - should NOT trigger
	code := `package main

// TODO: Remove exec.Command usage before release
// exec.Command is dangerous, see security docs
func safe() {
	// Don't use exec.Command with user input
	println("hello")
}
`
	result, err := scanner.ScanCode(ctx, code, "go", "main.go")
	if err != nil {
		t.Fatalf("ScanCode failed: %v", err)
	}

	if len(result.Issues) > 0 {
		t.Errorf("Expected no issues for comments, got %d: %+v", len(result.Issues), result.Issues)
	}

	if result.Score != 1.0 {
		t.Errorf("Expected score 1.0 for clean code, got %f", result.Score)
	}
}

func TestHybridSecurityScanner_IgnoresStrings_Go(t *testing.T) {
	scanner := NewHybridSecurityScanner()
	ctx := context.Background()

	// This code has exec.Command in a string literal - should NOT trigger
	code := `package main

func main() {
	errMsg := "Do not use exec.Command with user input"
	helpText := "Use exec.Command(\"ls\", \"-la\") for listing"
	println(errMsg, helpText)
}
`
	result, err := scanner.ScanCode(ctx, code, "go", "main.go")
	if err != nil {
		t.Fatalf("ScanCode failed: %v", err)
	}

	if len(result.Issues) > 0 {
		t.Errorf("Expected no issues for string literals, got %d: %+v", len(result.Issues), result.Issues)
	}
}

func TestHybridSecurityScanner_DetectsRealDangerousCode_Go(t *testing.T) {
	scanner := NewHybridSecurityScanner()
	ctx := context.Background()

	// This code has ACTUAL exec.Command usage - SHOULD trigger
	code := `package main

import "os/exec"

func dangerous() {
	cmd := exec.Command("rm", "-rf", userInput)
	cmd.Run()
}
`
	result, err := scanner.ScanCode(ctx, code, "go", "main.go")
	if err != nil {
		t.Fatalf("ScanCode failed: %v", err)
	}

	if len(result.Issues) == 0 {
		t.Error("Expected issues for dangerous code, got none")
	}

	// Verify the issue is about exec.Command
	found := false
	for _, issue := range result.Issues {
		if issue.Pattern == "exec.Command" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected to find exec.Command pattern, got: %+v", result.Issues)
	}
}

func TestHybridSecurityScanner_IgnoresComments_Python(t *testing.T) {
	scanner := NewHybridSecurityScanner()
	ctx := context.Background()

	code := `# WARNING: Don't use eval() - it's dangerous
# eval can execute arbitrary code

def safe_function():
    # Never eval user input!
    print("safe")
`
	result, err := scanner.ScanCode(ctx, code, "python", "main.py")
	if err != nil {
		t.Fatalf("ScanCode failed: %v", err)
	}

	if len(result.Issues) > 0 {
		t.Errorf("Expected no issues for Python comments, got %d: %+v", len(result.Issues), result.Issues)
	}
}

func TestHybridSecurityScanner_DetectsRealDangerousCode_Python(t *testing.T) {
	scanner := NewHybridSecurityScanner()
	ctx := context.Background()

	code := `import pickle

def dangerous():
    data = pickle.loads(untrusted_data)
    return data
`
	result, err := scanner.ScanCode(ctx, code, "python", "main.py")
	if err != nil {
		t.Fatalf("ScanCode failed: %v", err)
	}

	if len(result.Issues) == 0 {
		t.Error("Expected issues for dangerous Python code (pickle.loads), got none")
	}
}

func TestHybridSecurityScanner_IgnoresComments_JavaScript(t *testing.T) {
	scanner := NewHybridSecurityScanner()
	ctx := context.Background()

	code := `// Don't use eval() in production
// eval is evil: eval("code")

function safe() {
    /*
     * Never do: eval(userInput)
     * Always sanitize input first
     */
    console.log("safe");
}
`
	result, err := scanner.ScanCode(ctx, code, "javascript", "main.js")
	if err != nil {
		t.Fatalf("ScanCode failed: %v", err)
	}

	if len(result.Issues) > 0 {
		t.Errorf("Expected no issues for JavaScript comments, got %d: %+v", len(result.Issues), result.Issues)
	}
}

func TestHybridSecurityScanner_DetectsRealDangerousCode_JavaScript(t *testing.T) {
	scanner := NewHybridSecurityScanner()
	ctx := context.Background()

	code := `function dangerous(userInput) {
    const result = eval(userInput);
    return result;
}
`
	result, err := scanner.ScanCode(ctx, code, "javascript", "main.js")
	if err != nil {
		t.Fatalf("ScanCode failed: %v", err)
	}

	if len(result.Issues) == 0 {
		t.Error("Expected issues for dangerous JavaScript code (eval), got none")
	}
}

func TestHybridSecurityScanner_ExcludesTestFiles(t *testing.T) {
	scanner := NewHybridSecurityScanner(WithExcludeTests(true))
	ctx := context.Background()

	// Even dangerous code in test files should be excluded
	code := `package main

import "os/exec"

func TestCommand(t *testing.T) {
	cmd := exec.Command("echo", "test")
	cmd.Run()
}
`
	result, err := scanner.ScanCode(ctx, code, "go", "main_test.go")
	if err != nil {
		t.Fatalf("ScanCode failed: %v", err)
	}

	if len(result.Issues) > 0 {
		t.Errorf("Expected test file to be skipped, got %d issues", len(result.Issues))
	}

	metrics := scanner.Metrics()
	if metrics.TestsExcluded != 1 {
		t.Errorf("Expected TestsExcluded=1, got %d", metrics.TestsExcluded)
	}
}

func TestHybridSecurityScanner_TestFilePatterns(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		isTest   bool
	}{
		{"Go test file", "foo_test.go", true},
		{"Go regular file", "foo.go", false},
		{"Python test prefix", "test_foo.py", true},
		{"Python test suffix", "foo_test.py", true},
		{"Python regular file", "foo.py", false},
		{"JS test file", "foo.test.js", true},
		{"JS spec file", "foo.spec.js", true},
		{"JS regular file", "foo.js", false},
		{"TS test file", "foo.test.ts", true},
		{"TS spec file", "foo.spec.ts", true},
		{"TS regular file", "foo.ts", false},
		{"Testdata directory", "testdata/foo.go", true},
		{"__tests__ directory", "__tests__/foo.js", true},
		{"fixtures directory", "fixtures/foo.py", true},
		{"test/ directory", "test/helper.go", true},
		{"Regular directory", "src/main.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTestFile(tt.filePath)
			if got != tt.isTest {
				t.Errorf("isTestFile(%q) = %v, want %v", tt.filePath, got, tt.isTest)
			}
		})
	}
}

func TestHybridSecurityScanner_UsesASTForSupportedLanguages(t *testing.T) {
	scanner := NewHybridSecurityScanner()
	ctx := context.Background()

	// Clean code in Go
	code := `package main

func main() {
	println("hello")
}
`
	result, mode, err := scanner.ScanCodeWithMode(ctx, code, "go", "main.go")
	if err != nil {
		t.Fatalf("ScanCodeWithMode failed: %v", err)
	}

	if mode != ScanModeAST {
		t.Errorf("Expected AST mode for Go, got %s", mode)
	}

	if result.Score != 1.0 {
		t.Errorf("Expected score 1.0 for clean code, got %f", result.Score)
	}

	metrics := scanner.Metrics()
	if metrics.ASTScans != 1 {
		t.Errorf("Expected ASTScans=1, got %d", metrics.ASTScans)
	}
}

func TestHybridSecurityScanner_FallsBackToRegexForUnknownLanguage(t *testing.T) {
	scanner := NewHybridSecurityScanner()
	ctx := context.Background()

	// Unknown language
	code := `some code with eval(x)`

	result, mode, err := scanner.ScanCodeWithMode(ctx, code, "unknown", "file.xyz")
	if err != nil {
		t.Fatalf("ScanCodeWithMode failed: %v", err)
	}

	if mode != ScanModeRegex {
		t.Errorf("Expected Regex mode for unknown language, got %s", mode)
	}

	// Regex scanner should detect eval
	if len(result.Issues) == 0 {
		t.Error("Expected regex scanner to detect eval pattern")
	}

	metrics := scanner.Metrics()
	if metrics.RegexScans != 1 {
		t.Errorf("Expected RegexScans=1, got %d", metrics.RegexScans)
	}
}

func TestHybridSecurityScanner_DetectsLanguageFromExtension(t *testing.T) {
	scanner := NewHybridSecurityScanner()
	ctx := context.Background()

	code := `func main() { println("hello") }
`
	// Empty language but .go extension
	result, mode, err := scanner.ScanCodeWithMode(ctx, code, "", "main.go")
	if err != nil {
		t.Fatalf("ScanCodeWithMode failed: %v", err)
	}

	if mode != ScanModeAST {
		t.Errorf("Expected AST mode from extension detection, got %s", mode)
	}

	if result == nil {
		t.Error("Expected non-nil result")
	}
}

func TestHybridSecurityScanner_ContextCancellation(t *testing.T) {
	scanner := NewHybridSecurityScanner()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	code := `package main`

	_, err := scanner.ScanCode(ctx, code, "go", "main.go")
	if err == nil {
		t.Error("Expected error on cancelled context")
	}

	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

func TestHybridSecurityScanner_MetricsReset(t *testing.T) {
	scanner := NewHybridSecurityScanner()
	ctx := context.Background()

	// Do some scans
	_, _ = scanner.ScanCode(ctx, `package main`, "go", "main.go")
	_, _ = scanner.ScanCode(ctx, `foo`, "unknown", "file.xyz")

	metrics := scanner.Metrics()
	if metrics.ASTScans == 0 && metrics.RegexScans == 0 {
		t.Error("Expected some scans recorded")
	}

	scanner.ResetMetrics()

	metrics = scanner.Metrics()
	if metrics.ASTScans != 0 || metrics.RegexScans != 0 ||
		metrics.ASTFallbacks != 0 || metrics.TestsExcluded != 0 {
		t.Errorf("Expected all metrics to be 0 after reset, got %+v", metrics)
	}
}

func TestHybridSecurityScanner_ConcurrentAccess(t *testing.T) {
	scanner := NewHybridSecurityScanner()
	ctx := context.Background()

	// Run 10 concurrent scans
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(idx int) {
			defer func() { done <- true }()

			var code string
			var lang string
			var path string

			if idx%2 == 0 {
				code = `package main
func main() { println("hello") }`
				lang = "go"
				path = "main.go"
			} else {
				code = `def foo(): print("hello")`
				lang = "python"
				path = "main.py"
			}

			_, err := scanner.ScanCode(ctx, code, lang, path)
			if err != nil {
				t.Errorf("Concurrent scan %d failed: %v", idx, err)
			}
		}(i)
	}

	// Wait for all goroutines with timeout
	timeout := time.After(5 * time.Second)
	for i := 0; i < 10; i++ {
		select {
		case <-done:
			// OK
		case <-timeout:
			t.Fatal("Concurrent test timed out")
		}
	}

	// Verify metrics were properly tracked
	metrics := scanner.Metrics()
	if metrics.ASTScans != 10 {
		t.Errorf("Expected 10 AST scans, got %d", metrics.ASTScans)
	}
}

func TestHybridSecurityScanner_ScoreDeductions(t *testing.T) {
	scanner := NewHybridSecurityScanner()
	ctx := context.Background()

	// Code with multiple issues
	code := `package main

import (
	"os/exec"
	"unsafe"
)

func dangerous() {
	cmd := exec.Command("rm", "-rf", "/")
	cmd.Run()

	ptr := unsafe.Pointer(nil)
	_ = ptr
}
`
	result, err := scanner.ScanCode(ctx, code, "go", "main.go")
	if err != nil {
		t.Fatalf("ScanCode failed: %v", err)
	}

	// Should have multiple issues
	if len(result.Issues) < 2 {
		t.Errorf("Expected at least 2 issues, got %d", len(result.Issues))
	}

	// Score should be deducted
	if result.Score >= 1.0 {
		t.Errorf("Expected score < 1.0 for dangerous code, got %f", result.Score)
	}

	// Score should be clamped to 0
	if result.Score < 0 {
		t.Errorf("Score should not go below 0, got %f", result.Score)
	}
}

func TestHybridSecurityScanner_TypeScriptSupport(t *testing.T) {
	scanner := NewHybridSecurityScanner()
	ctx := context.Background()

	// TypeScript code with comment
	code := `// Don't use eval()
function safe(): void {
    console.log("hello");
}
`
	result, mode, err := scanner.ScanCodeWithMode(ctx, code, "typescript", "main.ts")
	if err != nil {
		t.Fatalf("ScanCodeWithMode failed: %v", err)
	}

	if mode != ScanModeAST {
		t.Errorf("Expected AST mode for TypeScript, got %s", mode)
	}

	// Comment should not trigger
	if len(result.Issues) > 0 {
		t.Errorf("Expected no issues for TS comments, got %d: %+v", len(result.Issues), result.Issues)
	}
}

func TestHybridSecurityScanner_LanguageNormalization(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"go", "go"},
		{"Go", "go"},
		{"GO", "go"},
		{"golang", "golang"},
		{"python", "python"},
		{"Python", "python"},
		{"python3", "python3"},
		{"javascript", "javascript"},
		{"JavaScript", "javascript"},
		{"js", "js"},
		{"typescript", "typescript"},
		{"TypeScript", "typescript"},
		{"ts", "ts"},
		{"unknown", "unknown"},
		{"  go  ", "go"},
	}

	scanner := NewHybridSecurityScanner()

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			normalized := scanner.normalizeLanguage(tt.input, "")
			if normalized != tt.expected {
				t.Errorf("normalizeLanguage(%q) = %q, want %q", tt.input, normalized, tt.expected)
			}
		})
	}
}

// Regression test: This was the original failing case from the ticket
func TestHybridSecurityScanner_RegressionTest_CommentsInGo(t *testing.T) {
	scanner := NewHybridSecurityScanner()
	ctx := context.Background()

	code := `package main

// TODO: Remove exec.Command usage before release
func safe() {}
`
	result, err := scanner.ScanCode(ctx, code, "go", "main.go")
	if err != nil {
		t.Fatalf("ScanCode failed: %v", err)
	}

	if len(result.Issues) > 0 {
		t.Fatalf("REGRESSION: exec.Command in comment triggered false positive: %+v", result.Issues)
	}
}

// Regression test: String literal case
func TestHybridSecurityScanner_RegressionTest_StringsInGo(t *testing.T) {
	scanner := NewHybridSecurityScanner()
	ctx := context.Background()

	code := `package main

func main() {
	errMsg := "Do not use exec.Command with user input"
	println(errMsg)
}
`
	result, err := scanner.ScanCode(ctx, code, "go", "main.go")
	if err != nil {
		t.Fatalf("ScanCode failed: %v", err)
	}

	if len(result.Issues) > 0 {
		t.Fatalf("REGRESSION: exec.Command in string triggered false positive: %+v", result.Issues)
	}
}
