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

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
)

func TestCitationChecker_Name(t *testing.T) {
	checker := NewCitationChecker(nil)
	if checker.Name() != "citation_checker" {
		t.Errorf("expected name 'citation_checker', got '%s'", checker.Name())
	}
}

func TestCitationChecker_Check_ValidCitation(t *testing.T) {
	checker := NewCitationChecker(&CitationCheckerConfig{
		RequireCitations:   true,
		ValidateFileExists: true,
		ValidateInContext:  true,
		ValidateLineRange:  true,
	})
	ctx := context.Background()

	fileContent := `package main

func main() {
    fmt.Println("Hello")
}
`
	input := &CheckInput{
		Response: `The main function is defined at [main.go:3]. It prints a greeting.`,
		KnownFiles: map[string]bool{
			"main.go": true,
		},
		CodeContext: []agent.CodeEntry{
			{
				FilePath: "main.go",
				Content:  fileContent,
			},
		},
		EvidenceIndex: &EvidenceIndex{
			Files:         map[string]bool{"main.go": true},
			FileBasenames: map[string]bool{"main.go": true},
			FileContents:  map[string]string{"main.go": fileContent},
		},
	}

	violations := checker.Check(ctx, input)

	for _, v := range violations {
		if v.Type == ViolationCitationInvalid {
			t.Errorf("unexpected citation violation: %s", v.Message)
		}
	}
}

func TestCitationChecker_Check_FileNotFound(t *testing.T) {
	checker := NewCitationChecker(&CitationCheckerConfig{
		RequireCitations:   true,
		ValidateFileExists: true,
		ValidateInContext:  false,
		ValidateLineRange:  false,
	})
	ctx := context.Background()

	input := &CheckInput{
		Response: `Check the implementation in [nonexistent.go:10].`,
		KnownFiles: map[string]bool{
			"main.go": true,
		},
	}

	violations := checker.Check(ctx, input)

	found := false
	for _, v := range violations {
		if v.Code == "CITATION_FILE_NOT_FOUND" {
			found = true
			if v.Severity != SeverityCritical {
				t.Errorf("expected critical severity for missing file, got %s", v.Severity)
			}
		}
	}

	if !found {
		t.Error("expected CITATION_FILE_NOT_FOUND violation")
	}
}

func TestCitationChecker_Check_FileNotInContext(t *testing.T) {
	checker := NewCitationChecker(&CitationCheckerConfig{
		RequireCitations:   true,
		ValidateFileExists: true,
		ValidateInContext:  true,
		ValidateLineRange:  false,
	})
	ctx := context.Background()

	input := &CheckInput{
		Response: `Check the implementation in [utils.go:10].`,
		KnownFiles: map[string]bool{
			"main.go":  true,
			"utils.go": true, // File exists but not in context
		},
		CodeContext: []agent.CodeEntry{
			{
				FilePath: "main.go",
				Content:  "package main",
			},
		},
		EvidenceIndex: &EvidenceIndex{
			Files:         map[string]bool{"main.go": true},
			FileBasenames: map[string]bool{"main.go": true},
		},
	}

	violations := checker.Check(ctx, input)

	found := false
	for _, v := range violations {
		if v.Code == "CITATION_NOT_IN_CONTEXT" {
			found = true
			if v.Severity != SeverityWarning {
				t.Errorf("expected warning severity for file not in context, got %s", v.Severity)
			}
		}
	}

	if !found {
		t.Error("expected CITATION_NOT_IN_CONTEXT violation")
	}
}

func TestCitationChecker_Check_LineOutOfRange(t *testing.T) {
	checker := NewCitationChecker(&CitationCheckerConfig{
		RequireCitations:   true,
		ValidateFileExists: true,
		ValidateInContext:  true,
		ValidateLineRange:  true,
	})
	ctx := context.Background()

	// File has only 5 lines
	fileContent := "line1\nline2\nline3\nline4\nline5"

	input := &CheckInput{
		Response: `The error handler is at [main.go:100].`,
		KnownFiles: map[string]bool{
			"main.go": true,
		},
		CodeContext: []agent.CodeEntry{
			{
				FilePath: "main.go",
				Content:  fileContent,
			},
		},
		EvidenceIndex: &EvidenceIndex{
			Files:         map[string]bool{"main.go": true},
			FileBasenames: map[string]bool{"main.go": true},
			FileContents:  map[string]string{"main.go": fileContent},
		},
	}

	violations := checker.Check(ctx, input)

	found := false
	for _, v := range violations {
		if v.Code == "CITATION_LINE_OUT_OF_RANGE" {
			found = true
			if v.Severity != SeverityCritical {
				t.Errorf("expected critical severity for line out of range, got %s", v.Severity)
			}
		}
	}

	if !found {
		t.Error("expected CITATION_LINE_OUT_OF_RANGE violation")
	}
}

func TestCitationChecker_Check_NoCitationsWithClaims(t *testing.T) {
	checker := NewCitationChecker(&CitationCheckerConfig{
		RequireCitations:   true,
		ValidateFileExists: false,
		ValidateInContext:  false,
		ValidateLineRange:  false,
	})
	ctx := context.Background()

	input := &CheckInput{
		Response: `The main function handles authentication.
The handleLogin function validates credentials.
This code is in auth.go.`,
	}

	violations := checker.Check(ctx, input)

	found := false
	for _, v := range violations {
		if v.Code == "NO_CITATIONS" {
			found = true
			if v.Severity != SeverityWarning {
				t.Errorf("expected warning severity for no citations, got %s", v.Severity)
			}
		}
	}

	if !found {
		t.Error("expected NO_CITATIONS violation when claims made without citations")
	}
}

func TestCitationChecker_Check_NoCitationsNoClaims(t *testing.T) {
	checker := NewCitationChecker(&CitationCheckerConfig{
		RequireCitations:   true,
		ValidateFileExists: false,
		ValidateInContext:  false,
		ValidateLineRange:  false,
	})
	ctx := context.Background()

	// Response without code claims shouldn't trigger NO_CITATIONS
	input := &CheckInput{
		Response: `Sure, I can help you with that. Let me know what you'd like to do next.`,
	}

	violations := checker.Check(ctx, input)

	for _, v := range violations {
		if v.Code == "NO_CITATIONS" {
			t.Error("should not require citations for non-code responses")
		}
	}
}

func TestCitationChecker_Check_MultipleCitations(t *testing.T) {
	checker := NewCitationChecker(&CitationCheckerConfig{
		RequireCitations:   true,
		ValidateFileExists: true,
		ValidateInContext:  true,
		ValidateLineRange:  true,
	})
	ctx := context.Background()

	fileContent := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10"

	input := &CheckInput{
		Response: `The main function [main.go:3] calls process [main.go:7-10].
Also see [missing.go:5] for details.`,
		KnownFiles: map[string]bool{
			"main.go": true,
		},
		CodeContext: []agent.CodeEntry{
			{
				FilePath: "main.go",
				Content:  fileContent,
			},
		},
		EvidenceIndex: &EvidenceIndex{
			Files:         map[string]bool{"main.go": true},
			FileBasenames: map[string]bool{"main.go": true},
			FileContents:  map[string]string{"main.go": fileContent},
		},
	}

	violations := checker.Check(ctx, input)

	// Should have violation for missing.go
	foundMissing := false
	for _, v := range violations {
		if v.Code == "CITATION_FILE_NOT_FOUND" && v.Evidence == "[missing.go:5]" {
			foundMissing = true
		}
	}

	if !foundMissing {
		t.Error("expected violation for missing.go citation")
	}
}

func TestCitationChecker_Check_LineRangeCitation(t *testing.T) {
	checker := NewCitationChecker(&CitationCheckerConfig{
		RequireCitations:   true,
		ValidateFileExists: true,
		ValidateInContext:  true,
		ValidateLineRange:  true,
	})
	ctx := context.Background()

	fileContent := "line1\nline2\nline3\nline4\nline5"

	input := &CheckInput{
		Response: `Check the code at [main.go:2-4].`,
		KnownFiles: map[string]bool{
			"main.go": true,
		},
		CodeContext: []agent.CodeEntry{
			{
				FilePath: "main.go",
				Content:  fileContent,
			},
		},
		EvidenceIndex: &EvidenceIndex{
			Files:         map[string]bool{"main.go": true},
			FileBasenames: map[string]bool{"main.go": true},
			FileContents:  map[string]string{"main.go": fileContent},
		},
	}

	violations := checker.Check(ctx, input)

	// Should have no violations for valid range
	for _, v := range violations {
		if v.Type == ViolationCitationInvalid {
			t.Errorf("unexpected citation violation: %s", v.Message)
		}
	}
}

func TestCitationChecker_Check_ContextCancellation(t *testing.T) {
	checker := NewCitationChecker(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	input := &CheckInput{
		Response: `Check [main.go:1] and [utils.go:2] and [config.go:3].`,
		KnownFiles: map[string]bool{
			"main.go":   true,
			"utils.go":  true,
			"config.go": true,
		},
	}

	// Should return early due to context cancellation
	violations := checker.Check(ctx, input)

	// May have partial results, but should not panic
	_ = violations
}

func TestCitationChecker_Check_DifferentFileExtensions(t *testing.T) {
	checker := NewCitationChecker(&CitationCheckerConfig{
		RequireCitations:   true,
		ValidateFileExists: true,
		ValidateInContext:  false,
		ValidateLineRange:  false,
	})
	ctx := context.Background()

	testCases := []struct {
		citation string
		valid    bool
	}{
		{"[main.go:10]", true},
		{"[script.py:5]", true},
		{"[app.js:20]", true},
		{"[component.tsx:15]", true},
		{"[App.java:100]", true},
		{"[lib.rs:50]", true},
		{"[config.yaml:3]", true},
		{"[data.json:1]", true},
		{"[readme.md:10]", true},
		{"[header.h:25]", true},
		{"[source.cpp:30]", true},
	}

	for _, tc := range testCases {
		t.Run(tc.citation, func(t *testing.T) {
			input := &CheckInput{
				Response:   "Check " + tc.citation,
				KnownFiles: map[string]bool{tc.citation[1 : len(tc.citation)-1]: true}, // Remove brackets and line number
			}

			// Should parse without panic
			_ = checker.Check(ctx, input)
		})
	}
}

func TestCitationChecker_Check_NormalizedPaths(t *testing.T) {
	checker := NewCitationChecker(&CitationCheckerConfig{
		RequireCitations:   true,
		ValidateFileExists: true,
		ValidateInContext:  false,
		ValidateLineRange:  false,
	})
	ctx := context.Background()

	// Test that citation [./main.go:10] matches known file "main.go"
	// and citation [main.go:10] matches known file "./main.go"
	t.Run("normalized citation matches raw known file", func(t *testing.T) {
		input := &CheckInput{
			Response: `See [./main.go:10] for details.`,
			KnownFiles: map[string]bool{
				"main.go": true,
			},
		}

		violations := checker.Check(ctx, input)

		for _, v := range violations {
			if v.Code == "CITATION_FILE_NOT_FOUND" {
				t.Error("should have matched: ./main.go normalized to main.go")
			}
		}
	})

	t.Run("raw citation matches normalized known file", func(t *testing.T) {
		input := &CheckInput{
			Response: `See [main.go:10] for details.`,
			KnownFiles: map[string]bool{
				"main.go": true, // Directly matches citation
			},
		}

		violations := checker.Check(ctx, input)

		for _, v := range violations {
			if v.Code == "CITATION_FILE_NOT_FOUND" {
				t.Error("should have matched directly")
			}
		}
	})

	t.Run("basename matching", func(t *testing.T) {
		input := &CheckInput{
			Response: `See [main.go:10] for details.`,
			KnownFiles: map[string]bool{
				"src/main.go": true, // Basename is main.go
			},
		}

		// This should fail because we check normalizedPath, filePath, and basename of the citation
		// but not the basename of KnownFiles entries
		violations := checker.Check(ctx, input)

		// Currently the implementation doesn't iterate through KnownFiles to check basenames
		// This is expected behavior - we match against what the caller provides
		foundViolation := false
		for _, v := range violations {
			if v.Code == "CITATION_FILE_NOT_FOUND" {
				foundViolation = true
			}
		}
		if !foundViolation {
			t.Error("expected violation: citation main.go doesn't match known file src/main.go")
		}
	})
}

func TestCitationChecker_extractCitations(t *testing.T) {
	checker := NewCitationChecker(nil)

	testCases := []struct {
		name     string
		response string
		expected int
	}{
		{
			name:     "single citation",
			response: "See [main.go:10]",
			expected: 1,
		},
		{
			name:     "multiple citations",
			response: "See [main.go:10] and [utils.go:20]",
			expected: 2,
		},
		{
			name:     "range citation",
			response: "Lines [main.go:10-20]",
			expected: 1,
		},
		{
			name:     "no citations",
			response: "No code references here",
			expected: 0,
		},
		{
			name:     "nested brackets",
			response: "Check [[main.go:10]] extra",
			expected: 1, // Should still find inner citation
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			citations := checker.extractCitations(tc.response)
			if len(citations) != tc.expected {
				t.Errorf("expected %d citations, got %d", tc.expected, len(citations))
			}
		})
	}
}

func TestCitationChecker_Check_ZeroLine(t *testing.T) {
	checker := NewCitationChecker(&CitationCheckerConfig{
		RequireCitations:   true,
		ValidateFileExists: true,
		ValidateInContext:  true,
		ValidateLineRange:  true,
	})
	ctx := context.Background()

	fileContent := "line1\nline2\nline3"

	input := &CheckInput{
		Response: `Invalid line zero reference [main.go:0].`,
		KnownFiles: map[string]bool{
			"main.go": true,
		},
		CodeContext: []agent.CodeEntry{
			{
				FilePath: "main.go",
				Content:  fileContent,
			},
		},
		EvidenceIndex: &EvidenceIndex{
			Files:         map[string]bool{"main.go": true},
			FileBasenames: map[string]bool{"main.go": true},
			FileContents:  map[string]string{"main.go": fileContent},
		},
	}

	violations := checker.Check(ctx, input)

	found := false
	for _, v := range violations {
		if v.Code == "CITATION_LINE_OUT_OF_RANGE" {
			found = true
		}
	}

	if !found {
		t.Error("expected violation for line 0 (lines start at 1)")
	}
}

func TestCitationChecker_fileInContext(t *testing.T) {
	checker := NewCitationChecker(nil)

	t.Run("file in evidence index", func(t *testing.T) {
		input := &CheckInput{
			EvidenceIndex: &EvidenceIndex{
				Files:         map[string]bool{"main.go": true},
				FileBasenames: map[string]bool{"main.go": true},
			},
		}

		if !checker.fileInContext("main.go", input) {
			t.Error("expected main.go to be in context via EvidenceIndex")
		}
	})

	t.Run("file in code context", func(t *testing.T) {
		input := &CheckInput{
			CodeContext: []agent.CodeEntry{
				{FilePath: "src/main.go", Content: "package main"},
			},
		}

		// Should match via basename
		if !checker.fileInContext("main.go", input) {
			t.Error("expected main.go to be in context via basename match")
		}
	})

	t.Run("file in tool results", func(t *testing.T) {
		input := &CheckInput{
			ToolResults: []ToolResult{
				{InvocationID: "read_1", Output: "Content of utils/helper.go:\npackage utils"},
			},
		}

		if !checker.fileInContext("helper.go", input) {
			t.Error("expected helper.go to be in context via tool result")
		}
	})

	t.Run("file not in context", func(t *testing.T) {
		input := &CheckInput{
			EvidenceIndex: &EvidenceIndex{
				Files:         map[string]bool{"main.go": true},
				FileBasenames: map[string]bool{"main.go": true},
			},
		}

		if checker.fileInContext("other.go", input) {
			t.Error("expected other.go to NOT be in context")
		}
	})

	t.Run("nil evidence index", func(t *testing.T) {
		input := &CheckInput{
			EvidenceIndex: nil,
			CodeContext: []agent.CodeEntry{
				{FilePath: "main.go", Content: "package main"},
			},
		}

		if !checker.fileInContext("main.go", input) {
			t.Error("expected main.go to be in context via CodeContext")
		}
	})
}

func TestCitationChecker_getFileContent(t *testing.T) {
	checker := NewCitationChecker(nil)

	t.Run("content from evidence index with normalized path", func(t *testing.T) {
		input := &CheckInput{
			EvidenceIndex: &EvidenceIndex{
				FileContents: map[string]string{
					"main.go": "package main\nfunc main() {}",
				},
			},
		}

		content := checker.getFileContent("main.go", input)
		if content != "package main\nfunc main() {}" {
			t.Errorf("expected content from evidence index, got %q", content)
		}
	})

	t.Run("content from evidence index with original path", func(t *testing.T) {
		input := &CheckInput{
			EvidenceIndex: &EvidenceIndex{
				FileContents: map[string]string{
					"./src/main.go": "package main",
				},
			},
		}

		content := checker.getFileContent("./src/main.go", input)
		if content != "package main" {
			t.Errorf("expected content from evidence index, got %q", content)
		}
	})

	t.Run("content from code context", func(t *testing.T) {
		input := &CheckInput{
			EvidenceIndex: &EvidenceIndex{
				FileContents: map[string]string{}, // Empty
			},
			CodeContext: []agent.CodeEntry{
				{FilePath: "utils/helper.go", Content: "package utils"},
			},
		}

		content := checker.getFileContent("helper.go", input)
		if content != "package utils" {
			t.Errorf("expected content from code context, got %q", content)
		}
	})

	t.Run("content not found", func(t *testing.T) {
		input := &CheckInput{
			EvidenceIndex: &EvidenceIndex{
				FileContents: map[string]string{"main.go": "package main"},
			},
		}

		content := checker.getFileContent("other.go", input)
		if content != "" {
			t.Errorf("expected empty content, got %q", content)
		}
	})

	t.Run("nil evidence index falls back to code context", func(t *testing.T) {
		input := &CheckInput{
			EvidenceIndex: nil,
			CodeContext: []agent.CodeEntry{
				{FilePath: "main.go", Content: "package main"},
			},
		}

		content := checker.getFileContent("main.go", input)
		if content != "package main" {
			t.Errorf("expected content from code context, got %q", content)
		}
	})
}

func TestCitationChecker_containsCodeClaims(t *testing.T) {
	checker := NewCitationChecker(nil)

	t.Run("response with function claim - the X function", func(t *testing.T) {
		response := "The main function initializes the server."
		if !checker.containsCodeClaims(response) {
			t.Error("expected to detect code claim")
		}
	})

	t.Run("response with function X pattern", func(t *testing.T) {
		response := "function handleRequest processes incoming data."
		if !checker.containsCodeClaims(response) {
			t.Error("expected to detect code claim")
		}
	})

	t.Run("response with file reference", func(t *testing.T) {
		response := "The implementation in main.go handles this."
		if !checker.containsCodeClaims(response) {
			t.Error("expected to detect code claim")
		}
	})

	t.Run("response with this code pattern", func(t *testing.T) {
		response := "This code initializes the server."
		if !checker.containsCodeClaims(response) {
			t.Error("expected to detect code claim")
		}
	})

	t.Run("response without code claims", func(t *testing.T) {
		response := "Sure, I can help you with that question."
		if checker.containsCodeClaims(response) {
			t.Error("expected no code claims")
		}
	})

	t.Run("response with the file X pattern", func(t *testing.T) {
		response := "The file config.yaml contains settings."
		if !checker.containsCodeClaims(response) {
			t.Error("expected to detect code claim")
		}
	})
}
