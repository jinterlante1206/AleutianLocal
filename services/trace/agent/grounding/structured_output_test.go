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

func TestNewStructuredOutputChecker(t *testing.T) {
	t.Run("with nil config uses defaults", func(t *testing.T) {
		checker := NewStructuredOutputChecker(nil)
		if checker == nil {
			t.Fatal("expected non-nil checker")
		}
		if checker.config.Enabled {
			t.Error("expected Enabled to be false by default")
		}
	})

	t.Run("with custom config", func(t *testing.T) {
		config := &StructuredOutputConfig{
			Enabled:       true,
			RequireJSON:   true,
			MinConfidence: 0.8,
		}
		checker := NewStructuredOutputChecker(config)
		if !checker.config.Enabled {
			t.Error("expected Enabled to be true")
		}
		if checker.config.MinConfidence != 0.8 {
			t.Errorf("expected MinConfidence 0.8, got %f", checker.config.MinConfidence)
		}
	})
}

func TestStructuredOutputChecker_Name(t *testing.T) {
	checker := NewStructuredOutputChecker(nil)
	if name := checker.Name(); name != "structured_output" {
		t.Errorf("expected name 'structured_output', got %q", name)
	}
}

func TestStructuredOutputChecker_Check_Disabled(t *testing.T) {
	config := &StructuredOutputConfig{Enabled: false}
	checker := NewStructuredOutputChecker(config)

	violations := checker.Check(context.Background(), &CheckInput{
		Response: `{"summary": "test"}`,
	})

	if violations != nil {
		t.Error("expected nil violations when disabled")
	}
}

func TestStructuredOutputChecker_Check_EmptyInput(t *testing.T) {
	config := &StructuredOutputConfig{Enabled: true}
	checker := NewStructuredOutputChecker(config)

	t.Run("nil input", func(t *testing.T) {
		violations := checker.Check(context.Background(), nil)
		if violations != nil {
			t.Error("expected nil violations for nil input")
		}
	})

	t.Run("empty response", func(t *testing.T) {
		violations := checker.Check(context.Background(), &CheckInput{Response: ""})
		if violations != nil {
			t.Error("expected nil violations for empty response")
		}
	})
}

func TestStructuredOutputChecker_Check_ValidJSON(t *testing.T) {
	config := &StructuredOutputConfig{
		Enabled:                true,
		RequireEvidenceQuotes:  true,
		ValidateEvidenceExists: true,
		MinConfidence:          0.5,
	}
	checker := NewStructuredOutputChecker(config)

	t.Run("valid response with all fields", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Files["main.go"] = true
		evidence.FileBasenames["main.go"] = true
		evidence.FileContents["main.go"] = "func main() {\n\tfmt.Println(\"hello\")\n}"

		response := `{
			"summary": "The main function prints hello",
			"claims": [
				{
					"statement": "main prints hello",
					"file": "main.go",
					"line_start": 2,
					"line_end": 2,
					"evidence_quote": "fmt.Println(\"hello\")",
					"confidence": 0.9
				}
			],
			"files_examined": ["main.go"],
			"tools_used": ["read_file"]
		}`

		violations := checker.Check(context.Background(), &CheckInput{
			Response:      response,
			EvidenceIndex: evidence,
		})

		if len(violations) > 0 {
			t.Errorf("expected no violations for valid response, got %d: %v", len(violations), violations)
		}
	})

	t.Run("valid response in markdown code block", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Files["main.go"] = true
		evidence.FileBasenames["main.go"] = true
		evidence.FileContents["main.go"] = "func main() {}"

		response := "Here's my analysis:\n```json\n" + `{
			"summary": "Main function exists",
			"claims": [
				{
					"statement": "main function exists",
					"file": "main.go",
					"line_start": 1,
					"line_end": 1,
					"evidence_quote": "func main()",
					"confidence": 0.95
				}
			],
			"files_examined": ["main.go"],
			"tools_used": []
		}` + "\n```"

		violations := checker.Check(context.Background(), &CheckInput{
			Response:      response,
			EvidenceIndex: evidence,
		})

		if len(violations) > 0 {
			t.Errorf("expected no violations for valid response in code block, got %d", len(violations))
		}
	})
}

func TestStructuredOutputChecker_Check_InvalidJSON(t *testing.T) {
	t.Run("non-JSON response with RequireJSON=true", func(t *testing.T) {
		config := &StructuredOutputConfig{
			Enabled:     true,
			RequireJSON: true,
		}
		checker := NewStructuredOutputChecker(config)

		violations := checker.Check(context.Background(), &CheckInput{
			Response: "This is not JSON, just plain text.",
		})

		if len(violations) != 1 {
			t.Fatalf("expected 1 violation, got %d", len(violations))
		}
		if violations[0].Code != "STRUCTURED_PARSE_FAILED" {
			t.Errorf("expected code STRUCTURED_PARSE_FAILED, got %s", violations[0].Code)
		}
	})

	t.Run("non-JSON response with RequireJSON=false", func(t *testing.T) {
		config := &StructuredOutputConfig{
			Enabled:     true,
			RequireJSON: false,
		}
		checker := NewStructuredOutputChecker(config)

		violations := checker.Check(context.Background(), &CheckInput{
			Response: "This is not JSON, just plain text.",
		})

		if violations != nil {
			t.Error("expected nil violations when RequireJSON is false")
		}
	})
}

func TestStructuredOutputChecker_Check_FilesExamined(t *testing.T) {
	config := &StructuredOutputConfig{
		Enabled:                true,
		RequireEvidenceQuotes:  false,
		ValidateEvidenceExists: false,
	}
	checker := NewStructuredOutputChecker(config)

	evidence := NewEvidenceIndex()
	evidence.Files["main.go"] = true
	evidence.FileBasenames["main.go"] = true

	response := `{
		"summary": "Analysis",
		"claims": [],
		"files_examined": ["main.go", "nonexistent.go"],
		"tools_used": []
	}`

	violations := checker.Check(context.Background(), &CheckInput{
		Response:      response,
		EvidenceIndex: evidence,
	})

	// Should have 1 violation for nonexistent.go
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Code != "STRUCTURED_FILE_NOT_SEEN" {
		t.Errorf("expected code STRUCTURED_FILE_NOT_SEEN, got %s", violations[0].Code)
	}
	if !strings.Contains(violations[0].Message, "nonexistent.go") {
		t.Errorf("expected message to mention nonexistent.go")
	}
}

func TestStructuredOutputChecker_Check_ClaimValidation(t *testing.T) {
	config := &StructuredOutputConfig{
		Enabled:                true,
		RequireEvidenceQuotes:  true,
		ValidateEvidenceExists: true,
		MinConfidence:          0.7,
	}
	checker := NewStructuredOutputChecker(config)

	t.Run("low confidence claim", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Files["main.go"] = true
		evidence.FileBasenames["main.go"] = true
		evidence.FileContents["main.go"] = "func main() {}"

		response := `{
			"summary": "Analysis",
			"claims": [
				{
					"statement": "Something might exist",
					"file": "main.go",
					"line_start": 1,
					"line_end": 1,
					"evidence_quote": "func main()",
					"confidence": 0.5
				}
			],
			"files_examined": ["main.go"],
			"tools_used": []
		}`

		violations := checker.Check(context.Background(), &CheckInput{
			Response:      response,
			EvidenceIndex: evidence,
		})

		hasLowConfidence := false
		for _, v := range violations {
			if v.Code == "STRUCTURED_LOW_CONFIDENCE" {
				hasLowConfidence = true
			}
		}
		if !hasLowConfidence {
			t.Error("expected STRUCTURED_LOW_CONFIDENCE violation")
		}
	})

	t.Run("file not in evidence", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Files["other.go"] = true

		response := `{
			"summary": "Analysis",
			"claims": [
				{
					"statement": "Something in main",
					"file": "main.go",
					"line_start": 1,
					"line_end": 1,
					"evidence_quote": "func main()",
					"confidence": 0.9
				}
			],
			"files_examined": [],
			"tools_used": []
		}`

		violations := checker.Check(context.Background(), &CheckInput{
			Response:      response,
			EvidenceIndex: evidence,
		})

		hasFileNotFound := false
		for _, v := range violations {
			if v.Code == "STRUCTURED_FILE_NOT_FOUND" {
				hasFileNotFound = true
			}
		}
		if !hasFileNotFound {
			t.Error("expected STRUCTURED_FILE_NOT_FOUND violation")
		}
	})

	t.Run("evidence quote not found in file", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Files["main.go"] = true
		evidence.FileBasenames["main.go"] = true
		evidence.FileContents["main.go"] = "func main() { fmt.Println(\"world\") }"

		response := `{
			"summary": "Analysis",
			"claims": [
				{
					"statement": "Prints hello",
					"file": "main.go",
					"line_start": 1,
					"line_end": 1,
					"evidence_quote": "fmt.Println(\"hello\")",
					"confidence": 0.9
				}
			],
			"files_examined": ["main.go"],
			"tools_used": []
		}`

		violations := checker.Check(context.Background(), &CheckInput{
			Response:      response,
			EvidenceIndex: evidence,
		})

		hasEvidenceMismatch := false
		for _, v := range violations {
			if v.Code == "STRUCTURED_EVIDENCE_MISMATCH" {
				hasEvidenceMismatch = true
			}
		}
		if !hasEvidenceMismatch {
			t.Error("expected STRUCTURED_EVIDENCE_MISMATCH violation")
		}
	})

	t.Run("missing evidence quote when required", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Files["main.go"] = true
		evidence.FileBasenames["main.go"] = true

		response := `{
			"summary": "Analysis",
			"claims": [
				{
					"statement": "Something exists",
					"file": "main.go",
					"line_start": 1,
					"line_end": 1,
					"evidence_quote": "",
					"confidence": 0.9
				}
			],
			"files_examined": ["main.go"],
			"tools_used": []
		}`

		violations := checker.Check(context.Background(), &CheckInput{
			Response:      response,
			EvidenceIndex: evidence,
		})

		hasNoEvidence := false
		for _, v := range violations {
			if v.Code == "STRUCTURED_NO_EVIDENCE_QUOTE" {
				hasNoEvidence = true
			}
		}
		if !hasNoEvidence {
			t.Error("expected STRUCTURED_NO_EVIDENCE_QUOTE violation")
		}
	})
}

func TestStructuredOutputChecker_ContextCancellation(t *testing.T) {
	config := &StructuredOutputConfig{Enabled: true}
	checker := NewStructuredOutputChecker(config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	violations := checker.Check(ctx, &CheckInput{
		Response: `{"summary": "test"}`,
	})

	if violations != nil {
		t.Error("expected nil violations on cancelled context")
	}
}

func TestParseStructuredResponse(t *testing.T) {
	t.Run("valid JSON", func(t *testing.T) {
		response := `{
			"summary": "Test summary",
			"claims": [],
			"files_examined": ["main.go"],
			"tools_used": ["read_file"]
		}`

		parsed, err := ParseStructuredResponse(response)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if parsed.Summary != "Test summary" {
			t.Errorf("expected summary 'Test summary', got %q", parsed.Summary)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, err := ParseStructuredResponse("not json")
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

func TestValidateStructuredResponse(t *testing.T) {
	evidence := NewEvidenceIndex()
	evidence.Files["main.go"] = true
	evidence.FileBasenames["main.go"] = true
	evidence.FileContents["main.go"] = "func main() {}"

	resp := &StructuredResponse{
		Summary: "Test",
		Claims: []StructuredClaim{
			{
				Statement:     "main exists",
				File:          "main.go",
				LineStart:     1,
				LineEnd:       1,
				EvidenceQuote: "func main()",
				Confidence:    0.9,
			},
		},
		FilesExamined: []string{"main.go"},
		ToolsUsed:     []string{},
	}

	violations := ValidateStructuredResponse(resp, evidence)
	if len(violations) > 0 {
		t.Errorf("expected no violations, got %d", len(violations))
	}
}

func TestStructuredOutputSystemPrompt(t *testing.T) {
	prompt := StructuredOutputSystemPrompt()

	requiredPhrases := []string{
		"JSON format",
		"summary",
		"claims",
		"evidence_quote",
		"confidence",
		"files_examined",
	}

	for _, phrase := range requiredPhrases {
		if !strings.Contains(prompt, phrase) {
			t.Errorf("expected prompt to contain %q", phrase)
		}
	}
}

func TestNormalizeForComparison(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello world", "hello world"},
		{"hello  world", "hello world"},
		{"hello\r\nworld", "hello\nworld"},
		{"  hello  ", "hello"},
		{"hello\t\tworld", "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeForComparison(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeForComparison(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestEvidenceExistsInFile(t *testing.T) {
	checker := NewStructuredOutputChecker(nil)

	t.Run("empty evidence quote", func(t *testing.T) {
		claim := StructuredClaim{
			File:          "main.go",
			EvidenceQuote: "",
		}
		evidence := NewEvidenceIndex()
		evidence.FileContents["main.go"] = "package main"

		if checker.evidenceExistsInFile(claim, evidence) {
			t.Error("expected false for empty evidence quote")
		}
	})

	t.Run("evidence in specific file", func(t *testing.T) {
		claim := StructuredClaim{
			File:          "main.go",
			EvidenceQuote: "package main",
		}
		evidence := NewEvidenceIndex()
		evidence.FileContents["main.go"] = "package main\n\nfunc main() {}"

		if !checker.evidenceExistsInFile(claim, evidence) {
			t.Error("expected true: evidence exists in file")
		}
	})

	t.Run("evidence not in file", func(t *testing.T) {
		claim := StructuredClaim{
			File:          "main.go",
			EvidenceQuote: "func nonexistent()",
		}
		evidence := NewEvidenceIndex()
		evidence.FileContents["main.go"] = "package main\n\nfunc main() {}"

		if checker.evidenceExistsInFile(claim, evidence) {
			t.Error("expected false: evidence does not exist in file")
		}
	})

	t.Run("evidence matched by basename", func(t *testing.T) {
		claim := StructuredClaim{
			File:          "main.go", // Just basename
			EvidenceQuote: "package main",
		}
		evidence := NewEvidenceIndex()
		evidence.FileContents["src/main.go"] = "package main\n\nfunc main() {}"

		if !checker.evidenceExistsInFile(claim, evidence) {
			t.Error("expected true: evidence matched by basename")
		}
	})

	t.Run("evidence in raw content fallback", func(t *testing.T) {
		claim := StructuredClaim{
			File:          "other.go",
			EvidenceQuote: "special code",
		}
		evidence := NewEvidenceIndex()
		evidence.FileContents["main.go"] = "package main"
		evidence.RawContent = "Some raw content with special code here"

		if !checker.evidenceExistsInFile(claim, evidence) {
			t.Error("expected true: evidence in raw content")
		}
	})
}
