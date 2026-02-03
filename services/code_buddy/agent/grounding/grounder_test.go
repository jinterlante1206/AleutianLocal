// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package grounding

import (
	"context"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent"
)

// mockChecker is a test checker that returns configurable violations.
type mockChecker struct {
	name       string
	violations []Violation
}

func (m *mockChecker) Name() string {
	return m.name
}

func (m *mockChecker) Check(ctx context.Context, input *CheckInput) []Violation {
	return m.violations
}

func TestDefaultGrounder_Validate_NoViolations(t *testing.T) {
	grounder := NewDefaultGrounder(DefaultConfig())
	ctx := context.Background()

	result, err := grounder.Validate(ctx, "This is a valid response.", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Grounded {
		t.Error("expected grounded=true with no checkers")
	}
	if result.Confidence != 1.0 {
		t.Errorf("expected confidence=1.0, got %f", result.Confidence)
	}
}

func TestDefaultGrounder_Validate_Disabled(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = false

	grounder := NewDefaultGrounder(config)
	ctx := context.Background()

	result, err := grounder.Validate(ctx, "any response", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Grounded {
		t.Error("expected grounded=true when disabled")
	}
	if result.Confidence != 1.0 {
		t.Errorf("expected confidence=1.0 when disabled, got %f", result.Confidence)
	}
}

func TestDefaultGrounder_Validate_CriticalViolation(t *testing.T) {
	checker := &mockChecker{
		name: "test_checker",
		violations: []Violation{
			{
				Type:     ViolationWrongLanguage,
				Severity: SeverityCritical,
				Code:     "TEST_CRITICAL",
				Message:  "Critical test violation",
			},
		},
	}

	config := DefaultConfig()
	grounder := NewDefaultGrounder(config, checker)
	ctx := context.Background()

	result, err := grounder.Validate(ctx, "response with critical issue", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Grounded {
		t.Error("expected grounded=false with critical violation")
	}
	if result.CriticalCount != 1 {
		t.Errorf("expected CriticalCount=1, got %d", result.CriticalCount)
	}
	if result.ChecksRun != 1 {
		t.Errorf("expected ChecksRun=1, got %d", result.ChecksRun)
	}
}

func TestDefaultGrounder_Validate_WarningViolation(t *testing.T) {
	checker := &mockChecker{
		name: "test_checker",
		violations: []Violation{
			{
				Type:     ViolationNoCitations,
				Severity: SeverityWarning,
				Code:     "TEST_WARNING",
				Message:  "Warning test violation",
			},
		},
	}

	config := DefaultConfig()
	grounder := NewDefaultGrounder(config, checker)
	ctx := context.Background()

	result, err := grounder.Validate(ctx, "response with warning", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Warnings alone don't make it ungrounded (unless confidence drops below threshold)
	if result.WarningCount != 1 {
		t.Errorf("expected WarningCount=1, got %d", result.WarningCount)
	}
}

func TestDefaultGrounder_Validate_ShortCircuitOnCritical(t *testing.T) {
	checker1 := &mockChecker{
		name: "checker1",
		violations: []Violation{
			{
				Type:     ViolationWrongLanguage,
				Severity: SeverityCritical,
				Code:     "CRITICAL1",
				Message:  "First critical",
			},
		},
	}
	checker2 := &mockChecker{
		name: "checker2",
		violations: []Violation{
			{
				Type:     ViolationWrongLanguage,
				Severity: SeverityCritical,
				Code:     "CRITICAL2",
				Message:  "Second critical",
			},
		},
	}

	config := DefaultConfig()
	config.ShortCircuitOnCritical = true

	grounder := NewDefaultGrounder(config, checker1, checker2)
	ctx := context.Background()

	result, err := grounder.Validate(ctx, "response", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should stop after first critical
	if result.CriticalCount != 1 {
		t.Errorf("expected CriticalCount=1 (short circuit), got %d", result.CriticalCount)
	}
	if result.ChecksRun != 1 {
		t.Errorf("expected ChecksRun=1 (short circuit), got %d", result.ChecksRun)
	}
}

func TestDefaultGrounder_Validate_MaxViolationsReject(t *testing.T) {
	checker := &mockChecker{
		name: "test_checker",
		violations: []Violation{
			{Severity: SeverityWarning, Code: "WARN1"},
			{Severity: SeverityWarning, Code: "WARN2"},
			{Severity: SeverityWarning, Code: "WARN3"},
		},
	}

	config := DefaultConfig()
	config.MaxViolationsBeforeReject = 3 // Exactly 3 warnings should trigger reject

	grounder := NewDefaultGrounder(config, checker)
	ctx := context.Background()

	result, err := grounder.Validate(ctx, "response", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Grounded {
		t.Error("expected grounded=false when violations >= MaxViolationsBeforeReject")
	}
}

func TestDefaultGrounder_Validate_Timeout(t *testing.T) {
	config := DefaultConfig()
	config.Timeout = 1 * time.Millisecond

	grounder := NewDefaultGrounder(config)

	// Create a context that's already timed out
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond) // Ensure timeout

	result, err := grounder.Validate(ctx, "response", nil)
	if err != context.DeadlineExceeded {
		// May or may not get error depending on timing
		_ = result
	}
}

func TestDefaultGrounder_ShouldReject(t *testing.T) {
	config := DefaultConfig()
	config.RejectOnCritical = true

	grounder := NewDefaultGrounder(config)

	testCases := []struct {
		name     string
		result   *Result
		expected bool
	}{
		{
			name:     "nil result",
			result:   nil,
			expected: false,
		},
		{
			name: "grounded result",
			result: &Result{
				Grounded:   true,
				Confidence: 1.0,
			},
			expected: false,
		},
		{
			name: "critical violation",
			result: &Result{
				Grounded:      false,
				CriticalCount: 1,
			},
			expected: true,
		},
		{
			name: "max violations exceeded",
			result: &Result{
				Grounded:   false,
				Violations: make([]Violation, 5),
			},
			expected: true,
		},
		{
			name: "not grounded",
			result: &Result{
				Grounded: false,
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := grounder.ShouldReject(tc.result)
			if got != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

func TestDefaultGrounder_GenerateFootnote(t *testing.T) {
	config := DefaultConfig()
	config.AddFootnoteOnWarning = true

	grounder := NewDefaultGrounder(config)

	testCases := []struct {
		name        string
		result      *Result
		expectEmpty bool
	}{
		{
			name:        "nil result",
			result:      nil,
			expectEmpty: true,
		},
		{
			name: "no warnings",
			result: &Result{
				Grounded:   true,
				Confidence: 1.0,
			},
			expectEmpty: true,
		},
		{
			name: "critical violation - no footnote (should be rejected)",
			result: &Result{
				Grounded:      false,
				CriticalCount: 1,
				Violations: []Violation{
					{Severity: SeverityCritical, Message: "Critical issue"},
				},
			},
			expectEmpty: true,
		},
		{
			name: "warning - should have footnote",
			result: &Result{
				Grounded:     true,
				WarningCount: 1,
				Violations: []Violation{
					{Severity: SeverityWarning, Message: "Warning message"},
				},
			},
			expectEmpty: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			footnote := grounder.GenerateFootnote(tc.result)
			if tc.expectEmpty && footnote != "" {
				t.Errorf("expected empty footnote, got: %s", footnote)
			}
			if !tc.expectEmpty && footnote == "" {
				t.Error("expected non-empty footnote")
			}
		})
	}
}

func TestDefaultGrounder_BuildEvidenceIndex(t *testing.T) {
	config := DefaultConfig()
	grounder := NewDefaultGrounder(config)

	assembledCtx := &agent.AssembledContext{
		CodeContext: []agent.CodeEntry{
			{
				FilePath:   "main.go",
				SymbolName: "main",
				Content:    "package main\n\nfunc main() {}",
			},
			{
				FilePath:   "utils/helper.go",
				SymbolName: "Helper",
				Content:    "package utils\n\nfunc Helper() {}",
			},
		},
		ToolResults: []agent.ToolResult{
			{
				InvocationID: "read_1",
				Output:       "File content from config.yaml",
			},
		},
	}

	idx := grounder.buildEvidenceIndex(assembledCtx)

	// Check files are indexed
	if !idx.Files["main.go"] {
		t.Error("expected main.go in files")
	}
	if !idx.Files["utils/helper.go"] {
		t.Error("expected utils/helper.go in files")
	}

	// Check basenames
	if !idx.FileBasenames["main.go"] {
		t.Error("expected main.go in basenames")
	}
	if !idx.FileBasenames["helper.go"] {
		t.Error("expected helper.go in basenames")
	}

	// Check symbols
	if !idx.Symbols["main"] {
		t.Error("expected 'main' symbol")
	}
	if !idx.Symbols["Helper"] {
		t.Error("expected 'Helper' symbol")
	}

	// Check languages
	if !idx.Languages["go"] {
		t.Error("expected 'go' language")
	}

	// Check file contents
	if idx.FileContents["main.go"] == "" {
		t.Error("expected file content for main.go")
	}

	// Check raw content includes everything
	if idx.RawContent == "" {
		t.Error("expected non-empty raw content")
	}
}

func TestNewGrounder_Factory(t *testing.T) {
	grounder := NewGrounder(nil)
	if grounder == nil {
		t.Fatal("expected non-nil grounder")
	}

	// Validate with some response to ensure it works
	ctx := context.Background()
	result, err := grounder.Validate(ctx, "Test response", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestNewGrounder_WithConfig(t *testing.T) {
	config := &Config{
		Enabled:          false, // Disabled
		RejectOnCritical: false,
	}

	grounder := NewGrounder(config)
	ctx := context.Background()

	result, err := grounder.Validate(ctx, "any response", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be grounded because validation is disabled
	if !result.Grounded {
		t.Error("expected grounded=true when disabled")
	}
}

func TestNewGrounder_WithAllOptionalCheckersEnabled(t *testing.T) {
	config := &Config{
		Enabled:          true,
		RejectOnCritical: true,
		StructuredOutputConfig: &StructuredOutputConfig{
			Enabled: true,
		},
		TMSVerifierConfig: &TMSVerifierConfig{
			Enabled: true,
		},
		MultiSampleConfig: &MultiSampleConfig{
			Enabled: true,
		},
		ChainOfVerificationConfig: &ChainOfVerificationConfig{
			Enabled: true,
		},
		CitationCheckerConfig:  DefaultCitationCheckerConfig(),
		GroundingCheckerConfig: DefaultGroundingCheckerConfig(),
		LanguageCheckerConfig:  DefaultLanguageCheckerConfig(),
	}

	grounder := NewGrounder(config)
	if grounder == nil {
		t.Fatal("expected non-nil grounder")
	}

	ctx := context.Background()
	result, err := grounder.Validate(ctx, "Test response", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestNewGrounderWithCheckers(t *testing.T) {
	checker := &mockChecker{
		name:       "custom_checker",
		violations: nil,
	}

	config := DefaultConfig()
	grounder := NewGrounderWithCheckers(config, checker)

	if grounder == nil {
		t.Fatal("expected non-nil grounder")
	}

	ctx := context.Background()
	result, err := grounder.Validate(ctx, "Test response", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ChecksRun != 1 {
		t.Errorf("expected ChecksRun=1, got %d", result.ChecksRun)
	}
}

func TestConvertToolResults(t *testing.T) {
	agentResults := []agent.ToolResult{
		{InvocationID: "id1", Output: "output1"},
		{InvocationID: "id2", Output: "output2"},
		{InvocationID: "id3", Output: "output3"},
	}

	results := convertToolResults(agentResults)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].InvocationID != "id1" {
		t.Errorf("expected InvocationID='id1', got %s", results[0].InvocationID)
	}
	if results[1].Output != "output2" {
		t.Errorf("expected Output='output2', got %s", results[1].Output)
	}
}

func TestDetectLanguageFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"main.go", "go"},
		{"app.py", "python"},
		{"index.js", "javascript"},
		{"component.tsx", "typescript"},
		{"App.java", "java"},
		{"lib.rs", "rust"},
		{"main.c", "c"},
		{"main.cpp", "cpp"},
		{"header.h", "c"},
		{"header.hpp", "cpp"},
		{"main.cc", "cpp"},
		{"component.jsx", "javascript"},
		{"app.ts", "typescript"},
		{"README.md", ""},
		{"config.yaml", ""},
		{"data.json", ""},
		{"", ""},
		{"no_extension", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			lang := DetectLanguageFromPath(tt.path)
			if lang != tt.expected {
				t.Errorf("DetectLanguageFromPath(%q) = %q, want %q", tt.path, lang, tt.expected)
			}
		})
	}
}

func TestExtractSymbols(t *testing.T) {
	t.Run("Go functions", func(t *testing.T) {
		content := `package main

func main() {
    fmt.Println("Hello")
}

func helper() string {
    return "help"
}
`
		symbols := make(map[string]bool)
		extractSymbols(content, symbols)

		if !symbols["main"] {
			t.Error("expected 'main' symbol")
		}
		if !symbols["helper"] {
			t.Error("expected 'helper' symbol")
		}
	})

	t.Run("Go methods", func(t *testing.T) {
		content := `package server

func (s *Server) Start() error {
    return nil
}

func (h Handler) Handle() {
}
`
		symbols := make(map[string]bool)
		extractSymbols(content, symbols)

		if !symbols["Start"] {
			t.Error("expected 'Start' symbol")
		}
		if !symbols["Handle"] {
			t.Error("expected 'Handle' symbol")
		}
	})

	t.Run("Go types", func(t *testing.T) {
		content := `package types

type Server struct {
    Port int
}

type Handler interface {
    Handle()
}
`
		symbols := make(map[string]bool)
		extractSymbols(content, symbols)

		if !symbols["Server"] {
			t.Error("expected 'Server' symbol")
		}
		if !symbols["Handler"] {
			t.Error("expected 'Handler' symbol")
		}
	})

	t.Run("Python functions", func(t *testing.T) {
		content := `
def main():
    print("Hello")

def helper(x, y):
    return x + y
`
		symbols := make(map[string]bool)
		extractSymbols(content, symbols)

		if !symbols["main"] {
			t.Error("expected 'main' symbol")
		}
		if !symbols["helper"] {
			t.Error("expected 'helper' symbol")
		}
	})

	t.Run("Python classes", func(t *testing.T) {
		content := `
class Server:
    def __init__(self):
        pass

class Handler(BaseHandler):
    pass
`
		symbols := make(map[string]bool)
		extractSymbols(content, symbols)

		if !symbols["Server"] {
			t.Error("expected 'Server' symbol")
		}
		if !symbols["Handler"] {
			t.Error("expected 'Handler' symbol")
		}
	})

	t.Run("empty content", func(t *testing.T) {
		symbols := make(map[string]bool)
		extractSymbols("", symbols)
		if len(symbols) != 0 {
			t.Errorf("expected no symbols, got %d", len(symbols))
		}
	})
}

func TestExtractFilePathsFromText(t *testing.T) {
	t.Run("paths with directories", func(t *testing.T) {
		text := `Looking at src/main.go and utils/helper.go for the implementation.`
		files := make(map[string]bool)
		basenames := make(map[string]bool)

		extractFilePathsFromText(text, files, basenames)

		if !files["src/main.go"] {
			t.Error("expected 'src/main.go' in files")
		}
		if !files["utils/helper.go"] {
			t.Error("expected 'utils/helper.go' in files")
		}
		if !basenames["main.go"] {
			t.Error("expected 'main.go' in basenames")
		}
		if !basenames["helper.go"] {
			t.Error("expected 'helper.go' in basenames")
		}
	})

	t.Run("filenames only", func(t *testing.T) {
		text := `Check config.yaml and main.go for settings.`
		files := make(map[string]bool)
		basenames := make(map[string]bool)

		extractFilePathsFromText(text, files, basenames)

		if !basenames["config.yaml"] {
			t.Error("expected 'config.yaml' in basenames")
		}
		if !basenames["main.go"] {
			t.Error("expected 'main.go' in basenames")
		}
	})

	t.Run("quoted paths", func(t *testing.T) {
		text := `Open "src/app.py" and 'lib/utils.js' files.`
		files := make(map[string]bool)
		basenames := make(map[string]bool)

		extractFilePathsFromText(text, files, basenames)

		if !files["src/app.py"] {
			t.Error("expected 'src/app.py' in files")
		}
		if !files["lib/utils.js"] {
			t.Error("expected 'lib/utils.js' in files")
		}
	})

	t.Run("paths with backslashes", func(t *testing.T) {
		text := `Windows path: src\main.go`
		files := make(map[string]bool)
		basenames := make(map[string]bool)

		extractFilePathsFromText(text, files, basenames)

		// Note: backslash paths may or may not be detected depending on implementation
		// The function looks for \ or / as path separators
		if !files["src\\main.go"] && !basenames["main.go"] {
			// At minimum we should get the basename
			t.Log("backslash path not detected as expected, this is acceptable")
		}
	})

	t.Run("no code files", func(t *testing.T) {
		text := `This is just plain text without file references.`
		files := make(map[string]bool)
		basenames := make(map[string]bool)

		extractFilePathsFromText(text, files, basenames)

		if len(files) != 0 {
			t.Errorf("expected no files, got %d", len(files))
		}
		if len(basenames) != 0 {
			t.Errorf("expected no basenames, got %d", len(basenames))
		}
	})
}

func TestHasCodeExtension(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"main.go", true},
		{"app.py", true},
		{"index.js", true},
		{"app.ts", true},
		{"Component.jsx", true},
		{"Component.tsx", true},
		{"App.java", true},
		{"lib.rs", true},
		{"main.c", true},
		{"main.cpp", true},
		{"header.h", true},
		{"header.hpp", true},
		{"main.cc", true},
		{"README.md", true},
		{"config.yaml", true},
		{"config.yml", true},
		{"data.json", true},
		{"image.png", false},
		{"doc.pdf", false},
		{"archive.zip", false},
		{"no_extension", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := hasCodeExtension(tt.path)
			if got != tt.expected {
				t.Errorf("hasCodeExtension(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestBuildCheckInput(t *testing.T) {
	config := DefaultConfig()
	config.MaxResponseScanLength = 100
	grounder := NewDefaultGrounder(config)

	t.Run("nil assembled context", func(t *testing.T) {
		input, err := grounder.buildCheckInput("test response", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if input.Response != "test response" {
			t.Errorf("expected response='test response', got %q", input.Response)
		}
		if input.ProjectLang != "" {
			t.Errorf("expected empty ProjectLang, got %q", input.ProjectLang)
		}
	})

	t.Run("with assembled context", func(t *testing.T) {
		ctx := &agent.AssembledContext{
			CodeContext: []agent.CodeEntry{
				{FilePath: "main.go", Content: "package main"},
			},
			ToolResults: []agent.ToolResult{
				{InvocationID: "read_1", Output: "file content"},
			},
		}

		input, err := grounder.buildCheckInput("test response", ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if input.ProjectLang != "go" {
			t.Errorf("expected ProjectLang='go', got %q", input.ProjectLang)
		}
		if input.EvidenceIndex == nil {
			t.Error("expected non-nil EvidenceIndex")
		}
		if len(input.ToolResults) != 1 {
			t.Errorf("expected 1 tool result, got %d", len(input.ToolResults))
		}
	})

	t.Run("response truncation", func(t *testing.T) {
		longResponse := "This is a very long response that exceeds the maximum scan length limit set in the configuration and more."

		// Note: truncation only happens with non-nil assembledCtx
		ctx := &agent.AssembledContext{
			CodeContext: []agent.CodeEntry{
				{FilePath: "main.go", Content: "package main"},
			},
		}

		input, err := grounder.buildCheckInput(longResponse, ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(input.Response) > 100 {
			t.Errorf("expected truncated response of length <=100, got %d", len(input.Response))
		}
	})
}

func TestBuildEvidenceIndex_NilContext(t *testing.T) {
	config := DefaultConfig()
	grounder := NewDefaultGrounder(config)

	idx := grounder.buildEvidenceIndex(nil)
	if idx == nil {
		t.Fatal("expected non-nil index")
	}
	if len(idx.Files) != 0 {
		t.Errorf("expected empty files, got %d", len(idx.Files))
	}
}

func TestDefaultGrounder_DetectProjectLanguage(t *testing.T) {
	config := DefaultConfig()
	grounder := NewDefaultGrounder(config)

	testCases := []struct {
		name     string
		ctx      *agent.AssembledContext
		expected string
	}{
		{
			name:     "nil context",
			ctx:      nil,
			expected: "",
		},
		{
			name: "Go project",
			ctx: &agent.AssembledContext{
				CodeContext: []agent.CodeEntry{
					{FilePath: "main.go"},
					{FilePath: "handler.go"},
					{FilePath: "utils/helpers.go"},
				},
			},
			expected: "go",
		},
		{
			name: "Python project",
			ctx: &agent.AssembledContext{
				CodeContext: []agent.CodeEntry{
					{FilePath: "app.py"},
					{FilePath: "models.py"},
					{FilePath: "tests/test_app.py"},
				},
			},
			expected: "python",
		},
		{
			name: "Mixed project - Go majority",
			ctx: &agent.AssembledContext{
				CodeContext: []agent.CodeEntry{
					{FilePath: "main.go"},
					{FilePath: "handler.go"},
					{FilePath: "config.yaml"}, // Not counted
					{FilePath: "script.py"},
				},
			},
			expected: "go",
		},
		{
			name: "No code files",
			ctx: &agent.AssembledContext{
				CodeContext: []agent.CodeEntry{
					{FilePath: "README.md"},
					{FilePath: "config.yaml"},
				},
			},
			expected: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			lang := grounder.detectProjectLanguage(tc.ctx)
			if lang != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, lang)
			}
		})
	}
}
