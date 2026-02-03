// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package tdg

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// =============================================================================
// MOCK IMPLEMENTATIONS
// =============================================================================

// fullMockLLMClient implements the full LLMClient interface.
type fullMockLLMClient struct {
	generateResp       string
	generateSystemResp string
	err                error
	callCount          int
}

func (m *fullMockLLMClient) Generate(ctx context.Context, prompt string) (string, error) {
	m.callCount++
	if m.err != nil {
		return "", m.err
	}
	return m.generateResp, nil
}

func (m *fullMockLLMClient) GenerateWithSystem(ctx context.Context, system, prompt string) (string, error) {
	m.callCount++
	if m.err != nil {
		return "", m.err
	}
	return m.generateSystemResp, nil
}

// mockCtxAssembler implements ContextAssembler.
type mockCtxAssembler struct {
	context string
	err     error
}

func (m *mockCtxAssembler) AssembleContext(ctx context.Context, query string, tokenBudget int) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.context, nil
}

// =============================================================================
// TEST GENERATOR TESTS
// =============================================================================

func TestNewTestGenerator(t *testing.T) {
	t.Run("with all parameters", func(t *testing.T) {
		llm := &fullMockLLMClient{}
		assembler := &mockCtxAssembler{}
		gen := NewTestGenerator(llm, assembler, nil)

		if gen.llm != llm {
			t.Error("llm not set")
		}
		if gen.context != assembler {
			t.Error("context not set")
		}
		if gen.logger == nil {
			t.Error("logger should default to slog.Default")
		}
	})

	t.Run("with nil parameters", func(t *testing.T) {
		gen := NewTestGenerator(nil, nil, nil)

		if gen.logger == nil {
			t.Error("logger should default to slog.Default")
		}
	})
}

func TestTestGenerator_CallCount(t *testing.T) {
	gen := NewTestGenerator(nil, nil, nil)

	if gen.CallCount() != 0 {
		t.Errorf("CallCount() = %d, want 0", gen.CallCount())
	}

	gen.callCount.Store(5)
	if gen.CallCount() != 5 {
		t.Errorf("CallCount() = %d, want 5", gen.CallCount())
	}
}

func TestTestGenerator_ResetCallCount(t *testing.T) {
	gen := NewTestGenerator(nil, nil, nil)
	gen.callCount.Store(10)

	gen.ResetCallCount()

	if gen.CallCount() != 0 {
		t.Errorf("CallCount() after reset = %d, want 0", gen.CallCount())
	}
}

func TestTestGenerator_GenerateReproducerTest_Validation(t *testing.T) {
	llm := &fullMockLLMClient{}
	gen := NewTestGenerator(llm, nil, nil)

	t.Run("nil context returns error", func(t *testing.T) {
		req := &Request{
			BugDescription: "test bug",
			ProjectRoot:    "/project",
			Language:       "go",
		}
		_, err := gen.GenerateReproducerTest(nil, req)
		if err != ErrNilContext {
			t.Errorf("expected ErrNilContext, got %v", err)
		}
	})
}

func TestTestGenerator_GenerateReproducerTest(t *testing.T) {
	llmResp := `Here's the test:

TEST_FILE: auth_test.go

` + "```go" + `
package auth

import "testing"

func TestValidateToken_NilClaims(t *testing.T) {
    // Arrange
    token := &Token{Claims: nil}

    // Act
    err := ValidateToken(token)

    // Assert
    if err == nil {
        t.Error("expected error for nil claims")
    }
}
` + "```"

	llm := &fullMockLLMClient{generateSystemResp: llmResp}
	gen := NewTestGenerator(llm, nil, nil)

	req := &Request{
		BugDescription: "ValidateToken panics on nil claims",
		ProjectRoot:    "/project",
		Language:       "go",
	}

	tc, err := gen.GenerateReproducerTest(context.Background(), req)
	if err != nil {
		t.Fatalf("GenerateReproducerTest() error = %v", err)
	}

	if tc.Name != "TestValidateToken_NilClaims" {
		t.Errorf("Name = %q, want TestValidateToken_NilClaims", tc.Name)
	}
	if tc.FilePath != "auth_test.go" {
		t.Errorf("FilePath = %q, want auth_test.go", tc.FilePath)
	}
	if tc.Language != "go" {
		t.Errorf("Language = %q, want go", tc.Language)
	}
	if gen.CallCount() != 1 {
		t.Errorf("CallCount() = %d, want 1", gen.CallCount())
	}
}

func TestTestGenerator_GenerateReproducerTest_LLMError(t *testing.T) {
	llm := &fullMockLLMClient{err: errors.New("rate limited")}
	gen := NewTestGenerator(llm, nil, nil)

	req := &Request{
		BugDescription: "test bug",
		ProjectRoot:    "/project",
		Language:       "go",
	}

	_, err := gen.GenerateReproducerTest(context.Background(), req)
	if err == nil {
		t.Error("expected error")
	}
	if !errors.Is(err, ErrLLMGenerationFailed) {
		t.Errorf("expected ErrLLMGenerationFailed, got %v", err)
	}
}

func TestTestGenerator_GenerateFix(t *testing.T) {
	llmResp := `Here's the fix:

FIX_FILE: auth.go

` + "```go" + `
package auth

func ValidateToken(token *Token) error {
    if token == nil || token.Claims == nil {
        return ErrInvalidToken
    }
    // ... rest of validation
    return nil
}
` + "```"

	llm := &fullMockLLMClient{generateSystemResp: llmResp}
	gen := NewTestGenerator(llm, nil, nil)

	req := &Request{
		BugDescription: "ValidateToken panics on nil claims",
		ProjectRoot:    "/project",
		Language:       "go",
	}

	tc := &TestCase{
		Name:     "TestValidateToken_NilClaims",
		FilePath: "auth_test.go",
		Content:  "test content",
		Language: "go",
	}

	patch, err := gen.GenerateFix(context.Background(), req, tc, "panic: nil pointer")
	if err != nil {
		t.Fatalf("GenerateFix() error = %v", err)
	}

	if patch.FilePath != "auth.go" {
		t.Errorf("FilePath = %q, want auth.go", patch.FilePath)
	}
	if patch.NewContent == "" {
		t.Error("NewContent should not be empty")
	}
}

func TestTestGenerator_GenerateFix_Validation(t *testing.T) {
	gen := NewTestGenerator(nil, nil, nil)

	t.Run("nil context returns error", func(t *testing.T) {
		_, err := gen.GenerateFix(nil, &Request{}, &TestCase{}, "output")
		if err != ErrNilContext {
			t.Errorf("expected ErrNilContext, got %v", err)
		}
	})
}

func TestTestGenerator_RefineTest_Validation(t *testing.T) {
	gen := NewTestGenerator(nil, nil, nil)

	t.Run("nil context returns error", func(t *testing.T) {
		_, err := gen.RefineTest(nil, &Request{}, &TestCase{}, "output")
		if err != ErrNilContext {
			t.Errorf("expected ErrNilContext, got %v", err)
		}
	})
}

func TestTestGenerator_RefineFix_Validation(t *testing.T) {
	gen := NewTestGenerator(nil, nil, nil)

	t.Run("nil context returns error", func(t *testing.T) {
		_, err := gen.RefineFix(nil, &Request{}, &TestCase{}, &Patch{}, "output")
		if err != ErrNilContext {
			t.Errorf("expected ErrNilContext, got %v", err)
		}
	})
}

// =============================================================================
// PARSING HELPER TESTS
// =============================================================================

func TestExtractMarkedPath(t *testing.T) {
	tests := []struct {
		name     string
		response string
		marker   string
		want     string
	}{
		{
			name:     "path on separate line",
			response: "Some text\nTEST_FILE: path/to/test.go\nMore text",
			marker:   "TEST_FILE:",
			want:     "path/to/test.go",
		},
		{
			name:     "path with spaces",
			response: "TEST_FILE:    path/to/test.go   ",
			marker:   "TEST_FILE:",
			want:     "path/to/test.go",
		},
		{
			name:     "no marker found",
			response: "Some text without marker",
			marker:   "TEST_FILE:",
			want:     "",
		},
		{
			name:     "FIX_FILE marker",
			response: "FIX_FILE: src/auth.go",
			marker:   "FIX_FILE:",
			want:     "src/auth.go",
		},
		{
			name:     "marker inside code block",
			response: "```\nTEST_FILE: test_example.py\n```",
			marker:   "TEST_FILE:",
			want:     "test_example.py",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractMarkedPath(tt.response, tt.marker)
			if got != tt.want {
				t.Errorf("extractMarkedPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractCodeBlock(t *testing.T) {
	tests := []struct {
		name         string
		response     string
		language     string
		wantContains string // Check content contains expected substring
		wantEmpty    bool
	}{
		{
			name:         "go code block",
			response:     "Here's the code:\n```go\npackage main\n\nfunc main() {}\n```",
			language:     "go",
			wantContains: "package main",
		},
		{
			name:         "generic code block",
			response:     "Code:\n```\nsome code\n```",
			language:     "go",
			wantContains: "some code",
		},
		{
			name:      "no code block",
			response:  "No code here",
			language:  "go",
			wantEmpty: true,
		},
		{
			name:         "python code block",
			response:     "```python\ndef test_foo():\n    pass\n```",
			language:     "python",
			wantContains: "def test_foo()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCodeBlock(tt.response, tt.language)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("extractCodeBlock() = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tt.wantContains) {
				t.Errorf("extractCodeBlock() = %q, want to contain %q", got, tt.wantContains)
			}
		})
	}
}

func TestExtractTestName(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		language string
		want     string
	}{
		{
			name:     "go test function",
			content:  "func TestValidateToken(t *testing.T) {\n}",
			language: "go",
			want:     "TestValidateToken",
		},
		{
			name:     "go test with underscores",
			content:  "func TestValidate_NilInput_ReturnsError(t *testing.T) {}",
			language: "go",
			want:     "TestValidate_NilInput_ReturnsError",
		},
		{
			name:     "python test function",
			content:  "def test_validate_token():\n    pass",
			language: "python",
			want:     "test_validate_token",
		},
		{
			name:     "jest test with single quotes",
			content:  "test('validates input correctly', () => {});",
			language: "typescript",
			want:     "validates input correctly",
		},
		{
			name:     "jest it with double quotes",
			content:  `it("should process data", async () => {});`,
			language: "javascript",
			want:     "should process data",
		},
		{
			name:     "no test found",
			content:  "package main\n\nfunc main() {}",
			language: "go",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTestName(tt.content, tt.language)
			if got != tt.want {
				t.Errorf("extractTestName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInferTestFilePath(t *testing.T) {
	tests := []struct {
		language string
		want     string
	}{
		{"go", "reproducer_test.go"},
		{"python", "test_reproducer.py"},
		{"typescript", "reproducer.test.ts"},
		{"javascript", "reproducer.test.js"},
		{"unknown", "reproducer_test"},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			got := inferTestFilePath(tt.language)
			if got != tt.want {
				t.Errorf("inferTestFilePath(%q) = %q, want %q", tt.language, got, tt.want)
			}
		})
	}
}

// =============================================================================
// LANGUAGE HELPER TESTS
// =============================================================================

func TestLanguageForFile(t *testing.T) {
	tests := []struct {
		filePath string
		want     string
	}{
		{"main.go", "go"},
		{"test.py", "python"},
		{"app.ts", "typescript"},
		{"app.tsx", "typescript"},
		{"script.js", "javascript"},
		{"script.jsx", "javascript"},
		{"unknown.xyz", ""},
	}

	for _, tt := range tests {
		t.Run(tt.filePath, func(t *testing.T) {
			got := LanguageForFile(tt.filePath)
			if got != tt.want {
				t.Errorf("LanguageForFile(%q) = %q, want %q", tt.filePath, got, tt.want)
			}
		})
	}
}

// =============================================================================
// CONTEXT ASSEMBLER TESTS
// =============================================================================

func TestTestGenerator_WithContextAssembler(t *testing.T) {
	llmResp := `TEST_FILE: test.go
` + "```go" + `
package main
func TestFoo(t *testing.T) {}
` + "```"

	llm := &fullMockLLMClient{generateSystemResp: llmResp}
	assembler := &mockCtxAssembler{context: "func ValidateToken() {}"}
	gen := NewTestGenerator(llm, assembler, nil)

	req := &Request{
		BugDescription: "test bug",
		ProjectRoot:    "/project",
		Language:       "go",
	}

	_, err := gen.GenerateReproducerTest(context.Background(), req)
	if err != nil {
		t.Fatalf("GenerateReproducerTest() error = %v", err)
	}

	// Call count should be 1
	if gen.CallCount() != 1 {
		t.Errorf("CallCount() = %d, want 1", gen.CallCount())
	}
}

func TestTestGenerator_ContextAssemblerError(t *testing.T) {
	// Even if context assembler fails, generation should continue
	llmResp := `TEST_FILE: test.go
` + "```go" + `
package main
func TestFoo(t *testing.T) {}
` + "```"

	llm := &fullMockLLMClient{generateSystemResp: llmResp}
	assembler := &mockCtxAssembler{err: errors.New("context failed")}
	gen := NewTestGenerator(llm, assembler, nil)

	req := &Request{
		BugDescription: "test bug",
		ProjectRoot:    "/project",
		Language:       "go",
	}

	tc, err := gen.GenerateReproducerTest(context.Background(), req)
	if err != nil {
		t.Fatalf("GenerateReproducerTest() should succeed despite context error, got %v", err)
	}

	if tc == nil {
		t.Error("TestCase should not be nil")
	}
}
