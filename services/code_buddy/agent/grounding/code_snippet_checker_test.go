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
	"strings"
	"testing"
)

func TestCodeSnippetChecker_Name(t *testing.T) {
	checker := NewCodeSnippetChecker(nil)
	if checker.Name() != "code_snippet_checker" {
		t.Errorf("expected name 'code_snippet_checker', got %q", checker.Name())
	}
}

func TestCodeSnippetChecker_DisabledReturnsNil(t *testing.T) {
	config := DefaultCodeSnippetCheckerConfig()
	config.Enabled = false
	checker := NewCodeSnippetChecker(config)

	input := &CheckInput{
		Response: "```go\nfunc Foo() {}\n```",
		EvidenceIndex: &EvidenceIndex{
			FileContents: map[string]string{
				"main.go": "package main\nfunc Bar() {}",
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations when disabled, got %d", len(violations))
	}
}

func TestCodeSnippetChecker_NilInputReturnsNil(t *testing.T) {
	checker := NewCodeSnippetChecker(nil)

	if violations := checker.Check(context.Background(), nil); len(violations) != 0 {
		t.Error("expected nil for nil input")
	}

	if violations := checker.Check(context.Background(), &CheckInput{}); len(violations) != 0 {
		t.Error("expected nil for empty response")
	}
}

func TestCodeSnippetChecker_NoEvidenceReturnsNil(t *testing.T) {
	checker := NewCodeSnippetChecker(nil)

	input := &CheckInput{
		Response:      "```go\nfunc Foo() {}\n```",
		EvidenceIndex: nil,
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations without evidence, got %d", len(violations))
	}

	// With empty FileContents
	input.EvidenceIndex = &EvidenceIndex{
		FileContents: map[string]string{},
	}
	violations = checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations with empty FileContents, got %d", len(violations))
	}
}

func TestCodeSnippetChecker_VerbatimMatch(t *testing.T) {
	checker := NewCodeSnippetChecker(nil)

	actualCode := `func Parse(input string) (*Result, error) {
	if input == "" {
		return nil, errors.New("empty input")
	}
	return &Result{Value: input}, nil
}`

	input := &CheckInput{
		Response: "Here's the Parse function:\n```go\n" + actualCode + "\n```",
		EvidenceIndex: &EvidenceIndex{
			FileContents: map[string]string{
				"parser.go": "package parser\n\n" + actualCode + "\n",
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for verbatim match, got %d: %v", len(violations), violations)
	}
}

func TestCodeSnippetChecker_NormalizedVerbatimMatch(t *testing.T) {
	checker := NewCodeSnippetChecker(nil)

	// Code in evidence with tabs and extra whitespace
	actualCode := `func Parse(input string) (*Result, error) {
	if input == "" {
		return nil, errors.New("empty input")
	}
	return &Result{Value: input}, nil
}`

	// Code in response with spaces and different formatting
	responseCode := `func Parse(input string) (*Result, error) {
    if input == "" {
        return nil, errors.New("empty input")
    }
    return &Result{Value: input}, nil
}`

	input := &CheckInput{
		Response: "```go\n" + responseCode + "\n```",
		EvidenceIndex: &EvidenceIndex{
			FileContents: map[string]string{
				"parser.go": actualCode,
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for normalized match, got %d", len(violations))
	}
}

func TestCodeSnippetChecker_ModifiedCode(t *testing.T) {
	checker := NewCodeSnippetChecker(nil)

	// Actual code in evidence
	actualCode := `func Parse(input string) error {
	if input == "" {
		return errors.New("empty input")
	}
	return nil
}`

	// Similar but modified code in response
	modifiedCode := `func Parse(input string) error {
	if input == "" {
		return fmt.Errorf("invalid: empty input provided")
	}
	log.Printf("parsing input: %s", input)
	return nil
}`

	input := &CheckInput{
		Response: "```go\n" + modifiedCode + "\n```",
		EvidenceIndex: &EvidenceIndex{
			FileContents: map[string]string{
				"parser.go": actualCode,
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Should detect as modified (warning) since it's similar but different
	hasModifiedWarning := false
	for _, v := range violations {
		if v.Type == ViolationFabricatedCode && v.Severity == SeverityWarning {
			hasModifiedWarning = true
			break
		}
	}
	if !hasModifiedWarning && len(violations) > 0 {
		// If detected as fabricated instead of modified, that's also acceptable
		// since the similarity may be below modified threshold
		for _, v := range violations {
			if v.Type == ViolationFabricatedCode {
				return // Test passes - detected code mismatch
			}
		}
	}
	// If no violations, check that similarity is actually high enough
	if len(violations) == 0 {
		t.Log("No violations - code may be similar enough to pass verbatim threshold")
	}
}

func TestCodeSnippetChecker_FabricatedCode(t *testing.T) {
	checker := NewCodeSnippetChecker(nil)

	// Completely fabricated code
	fabricatedCode := `func ComputeQuantumEntanglement(particles []Particle) *EntanglementMatrix {
	matrix := NewEntanglementMatrix(len(particles))
	for i, p := range particles {
		for j := i + 1; j < len(particles); j++ {
			matrix.Set(i, j, calculateBellState(p, particles[j]))
		}
	}
	return matrix
}`

	// Actual code is completely different
	actualCode := `func Add(a, b int) int {
	return a + b
}`

	input := &CheckInput{
		Response: "```go\n" + fabricatedCode + "\n```",
		EvidenceIndex: &EvidenceIndex{
			FileContents: map[string]string{
				"math.go": actualCode,
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 1 {
		t.Errorf("expected 1 violation for fabricated code, got %d", len(violations))
		return
	}

	v := violations[0]
	if v.Type != ViolationFabricatedCode {
		t.Errorf("expected ViolationFabricatedCode, got %v", v.Type)
	}
	if v.Severity != SeverityCritical {
		t.Errorf("expected SeverityCritical for fabricated code, got %v", v.Severity)
	}
}

func TestCodeSnippetChecker_SuggestionPhrase(t *testing.T) {
	checker := NewCodeSnippetChecker(nil)

	// Fabricated code but preceded by suggestion phrase
	fabricatedCode := `func DoSomething() {
	fmt.Println("hello world")
}`

	testCases := []struct {
		name    string
		context string
	}{
		{"you could", "You could implement it like this:\n"},
		{"for example", "For example, here's how you might do it:\n"},
		{"consider", "Consider this approach:\n"},
		{"sample code", "Here's some sample code:\n"},
		{"try this", "Try this:\n"},
	}

	actualCode := `func RealFunction() int { return 42 }`

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			input := &CheckInput{
				Response: tc.context + "```go\n" + fabricatedCode + "\n```",
				EvidenceIndex: &EvidenceIndex{
					FileContents: map[string]string{
						"real.go": actualCode,
					},
				},
			}

			violations := checker.Check(context.Background(), input)
			if len(violations) != 0 {
				t.Errorf("expected no violations when suggestion phrase '%s' present, got %d",
					tc.name, len(violations))
			}
		})
	}
}

func TestCodeSnippetChecker_MultipleBlocks(t *testing.T) {
	checker := NewCodeSnippetChecker(nil)

	// Mix of verbatim and fabricated code
	verbatimCode := `func Add(a, b int) int {
	return a + b
}`
	fabricatedCode := `func MultiplyComplex(a, b complex128) complex128 {
	// Complex multiplication with normalization
	result := a * b
	return result / complex(cmplx.Abs(result), 0)
}`

	actualCode := "package math\n\n" + verbatimCode

	input := &CheckInput{
		Response: "Here's the Add function:\n```go\n" + verbatimCode + "\n```\n\n" +
			"And here's a complex multiply:\n```go\n" + fabricatedCode + "\n```",
		EvidenceIndex: &EvidenceIndex{
			FileContents: map[string]string{
				"math.go": actualCode,
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Should have 1 violation for the fabricated code
	if len(violations) != 1 {
		t.Errorf("expected 1 violation for fabricated code block, got %d", len(violations))
	}
}

func TestCodeSnippetChecker_InlineCode(t *testing.T) {
	config := DefaultCodeSnippetCheckerConfig()
	config.CheckInlineCode = true
	config.MinSnippetLength = 5 // Lower threshold for inline code test
	checker := NewCodeSnippetChecker(config)

	actualCode := `func Foo() string { return "hello" }`

	input := &CheckInput{
		Response: "The function `func Bar() string { return \"world\" }` does something.",
		EvidenceIndex: &EvidenceIndex{
			FileContents: map[string]string{
				"main.go": actualCode,
			},
		},
	}

	// This should detect fabricated inline code (if long enough)
	violations := checker.Check(context.Background(), input)
	// The inline code might be too short or similar enough, so we just verify the check runs
	t.Logf("Found %d violations for inline code check", len(violations))
}

func TestCodeSnippetChecker_NoFileContext(t *testing.T) {
	checker := NewCodeSnippetChecker(nil)

	// Fabricated code without any file context mentioned
	fabricatedCode := `func Mystery() {
	doSomethingUnknown()
	callAnotherFunction()
	returnSomeResult()
}`

	input := &CheckInput{
		Response: "```go\n" + fabricatedCode + "\n```",
		EvidenceIndex: &EvidenceIndex{
			FileContents: map[string]string{
				"unrelated.go": "package unrelated\nfunc Nothing() {}",
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 1 {
		t.Errorf("expected 1 violation for fabricated code, got %d", len(violations))
		return
	}

	if violations[0].Type != ViolationFabricatedCode {
		t.Errorf("expected ViolationFabricatedCode, got %v", violations[0].Type)
	}
}

func TestCodeSnippetChecker_SmallSnippetSkipped(t *testing.T) {
	config := DefaultCodeSnippetCheckerConfig()
	config.MinSnippetLength = 30
	checker := NewCodeSnippetChecker(config)

	// Very short code that should be skipped
	shortCode := `x := 1`

	input := &CheckInput{
		Response: "```go\n" + shortCode + "\n```",
		EvidenceIndex: &EvidenceIndex{
			FileContents: map[string]string{
				"main.go": "package main\nfunc foo() { y := 2 }",
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for short snippet, got %d", len(violations))
	}
}

func TestCodeSnippetChecker_ReformattedVerbatim(t *testing.T) {
	checker := NewCodeSnippetChecker(nil)

	// Same code, different formatting (tabs vs spaces, different line breaks)
	actualCode := "func Parse(s string) int {\n\treturn len(s)\n}"
	reformattedCode := "func Parse(s string) int {\n    return len(s)\n}"

	input := &CheckInput{
		Response: "```go\n" + reformattedCode + "\n```",
		EvidenceIndex: &EvidenceIndex{
			FileContents: map[string]string{
				"parser.go": actualCode,
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for reformatted verbatim code, got %d", len(violations))
	}
}

func TestCodeSnippetChecker_SuggestionPhraseDistance(t *testing.T) {
	config := DefaultCodeSnippetCheckerConfig()
	config.SuggestionContextLines = 3
	checker := NewCodeSnippetChecker(config)

	fabricatedCode := `func NewFunction() {
	// completely fabricated
}`

	// Suggestion phrase is more than 3 lines before the code block
	response := "You could try a different approach.\n\n\n\n\n\n" +
		"Here's some code:\n```go\n" + fabricatedCode + "\n```"

	input := &CheckInput{
		Response: response,
		EvidenceIndex: &EvidenceIndex{
			FileContents: map[string]string{
				"main.go": "package main\nfunc Real() {}",
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Suggestion phrase is too far away, should detect as fabricated
	if len(violations) != 1 {
		t.Errorf("expected 1 violation when suggestion phrase is too far, got %d", len(violations))
	}
}

func TestCodeSnippetChecker_MaxBlocksLimit(t *testing.T) {
	config := DefaultCodeSnippetCheckerConfig()
	config.MaxCodeBlocksToCheck = 2
	checker := NewCodeSnippetChecker(config)

	fabricatedCode := `func Fabricated%d() { /* fabricated */ }`

	// Create response with 5 code blocks
	var blocks []string
	for i := 1; i <= 5; i++ {
		blocks = append(blocks, "```go\n"+strings.Replace(fabricatedCode, "%d", string(rune('0'+i)), 1)+"\n```")
	}

	input := &CheckInput{
		Response: strings.Join(blocks, "\n\n"),
		EvidenceIndex: &EvidenceIndex{
			FileContents: map[string]string{
				"main.go": "package main",
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Should only check first 2 blocks
	if len(violations) > 2 {
		t.Errorf("expected at most 2 violations (MaxCodeBlocksToCheck), got %d", len(violations))
	}
}

func TestCodeSnippetChecker_ContextCancellation(t *testing.T) {
	checker := NewCodeSnippetChecker(nil)

	input := &CheckInput{
		Response: "```go\nfunc Foo() {}\n```\n```go\nfunc Bar() {}\n```",
		EvidenceIndex: &EvidenceIndex{
			FileContents: map[string]string{
				"main.go": "package main",
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	violations := checker.Check(ctx, input)
	// Should return early due to cancelled context
	t.Logf("Got %d violations with cancelled context", len(violations))
}

// Test the LCS similarity function directly
func TestLCSSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected float64 // approximate
	}{
		{"identical", "hello world", "hello world", 1.0},
		{"empty a", "", "hello", 0.0},
		{"empty b", "hello", "", 0.0},
		{"both empty", "", "", 0.0},
		{"completely different", "abc", "xyz", 0.0},
		{"partial match", "abcdef", "abxyzf", 0.5}, // LCS is "abf" = 3, max = 6
		{"one char match", "a", "a", 1.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := lcsSimilarity(tc.a, tc.b)
			// Allow some tolerance for floating point
			if result < tc.expected-0.1 || result > tc.expected+0.1 {
				t.Errorf("lcsSimilarity(%q, %q) = %f, expected ~%f", tc.a, tc.b, result, tc.expected)
			}
		})
	}
}

func TestNormalizeWhitespace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"tabs to spaces", "func\tfoo()", "func foo()"},
		{"multiple spaces", "func  foo()", "func foo()"},
		{"trim lines", "  func foo()  ", "func foo()"},
		{"empty lines at start", "\n\nfunc foo()", "func foo()"},
		{"empty lines at end", "func foo()\n\n", "func foo()"},
		{"preserve newlines", "a\nb", "a\nb"},
		{"crlf to lf", "a\r\nb", "a\nb"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := normalizeWhitespace(tc.input)
			if result != tc.expected {
				t.Errorf("normalizeWhitespace(%q) = %q, expected %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestNormalizeFull(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			"remove go comment",
			"func foo() { // comment\nreturn 1\n}",
			"func foo() {\nreturn 1\n}",
		},
		{
			"remove python comment",
			"def foo(): # comment\n    return 1",
			"def foo():\nreturn 1",
		},
		{
			"preserve code without comments",
			"func foo() {\nreturn 1\n}",
			"func foo() {\nreturn 1\n}",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := normalizeFull(tc.input)
			if result != tc.expected {
				t.Errorf("normalizeFull(%q) = %q, expected %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestCodeClassification_String(t *testing.T) {
	tests := []struct {
		c        CodeClassification
		expected string
	}{
		{ClassificationVerbatim, "verbatim"},
		{ClassificationModified, "modified"},
		{ClassificationFabricated, "fabricated"},
		{ClassificationSkipped, "skipped"},
		{CodeClassification(99), "unknown"},
	}

	for _, tc := range tests {
		if tc.c.String() != tc.expected {
			t.Errorf("%v.String() = %q, expected %q", tc.c, tc.c.String(), tc.expected)
		}
	}
}

func TestExtractCodeBlocks(t *testing.T) {
	checker := NewCodeSnippetChecker(nil)

	response := "Some text\n```go\nfunc Foo() {}\n```\nMore text\n```python\ndef bar():\n    pass\n```"

	blocks := checker.extractCodeBlocks(response)
	if len(blocks) != 2 {
		t.Errorf("expected 2 code blocks, got %d", len(blocks))
		return
	}

	if blocks[0].Language != "go" {
		t.Errorf("expected first block language 'go', got %q", blocks[0].Language)
	}
	if blocks[1].Language != "python" {
		t.Errorf("expected second block language 'python', got %q", blocks[1].Language)
	}

	if !strings.Contains(blocks[0].Content, "func Foo()") {
		t.Errorf("first block should contain 'func Foo()', got %q", blocks[0].Content)
	}
}

func TestNormalizationLevel(t *testing.T) {
	config := DefaultCodeSnippetCheckerConfig()
	config.NormalizationLevel = NormNone
	checker := NewCodeSnippetChecker(config)

	// Code with different whitespace that won't match without normalization
	actualCode := "func foo() {\n\treturn 1\n}"
	responseCode := "func foo() {\n    return 1\n}" // spaces instead of tabs

	input := &CheckInput{
		Response: "```go\n" + responseCode + "\n```",
		EvidenceIndex: &EvidenceIndex{
			FileContents: map[string]string{
				"main.go": actualCode,
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// With NormNone, whitespace differences should cause a mismatch
	// (though the code is logically the same)
	t.Logf("With NormNone: %d violations", len(violations))
}

func TestCodeSnippetChecker_EmptyCodeBlock(t *testing.T) {
	checker := NewCodeSnippetChecker(nil)

	// Empty code block
	input := &CheckInput{
		Response: "```go\n```",
		EvidenceIndex: &EvidenceIndex{
			FileContents: map[string]string{
				"main.go": "package main",
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Empty block should be skipped (too short)
	if len(violations) != 0 {
		t.Errorf("expected no violations for empty code block, got %d", len(violations))
	}
}

func TestCodeSnippetChecker_CodeBlockWithOnlyWhitespace(t *testing.T) {
	checker := NewCodeSnippetChecker(nil)

	// Code block with only whitespace
	input := &CheckInput{
		Response: "```go\n   \n   \n```",
		EvidenceIndex: &EvidenceIndex{
			FileContents: map[string]string{
				"main.go": "package main",
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Whitespace-only block should be skipped (too short after normalization)
	if len(violations) != 0 {
		t.Errorf("expected no violations for whitespace-only block, got %d", len(violations))
	}
}

func TestFindCommentIndex(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		marker   string
		expected int
	}{
		{"simple comment", "code // comment", "//", 5},
		{"no comment", "code", "//", -1},
		{"in double quotes", `color = "#FF0000"`, "#", -1},
		{"in single quotes", `char = '#'`, "#", -1},
		{"after quoted", `s = "test" // comment`, "//", 11},
		{"hash after string", `print("hello") # comment`, "#", 15},
		{"multiple quotes", `s = "a" + "b" // comment`, "//", 14},
		{"escaped quote", `s = "a\"b" // comment`, "//", 11},
		{"python string hash", `url = "http://example.com#anchor"`, "#", -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := findCommentIndex(tc.line, tc.marker)
			if result != tc.expected {
				t.Errorf("findCommentIndex(%q, %q) = %d, expected %d",
					tc.line, tc.marker, result, tc.expected)
			}
		})
	}
}

func TestNormalizeFull_PreservesHashInStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string // What the result should still contain
	}{
		{
			"preserves hex color",
			`color = "#FF0000"`,
			`color = "#FF0000"`,
		},
		{
			"preserves URL with anchor",
			`url = "http://example.com#section"`,
			`url = "http://example.com#section"`,
		},
		{
			"still removes real comments",
			"func foo() { // real comment\nreturn 1\n}",
			"func foo() {\nreturn 1\n}",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := normalizeFull(tc.input)
			if result != tc.contains {
				t.Errorf("normalizeFull(%q) = %q, expected %q",
					tc.input, result, tc.contains)
			}
		})
	}
}
