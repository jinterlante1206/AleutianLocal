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
	"encoding/json"
	"fmt"
	"strings"
)

// StructuredOutputConfig configures the structured output validator.
type StructuredOutputConfig struct {
	// Enabled determines if structured output validation is active.
	Enabled bool

	// RequireJSON requires the response to be valid JSON.
	RequireJSON bool

	// RequireEvidenceQuotes requires claims to have evidence quotes.
	RequireEvidenceQuotes bool

	// MinConfidence is the minimum confidence to accept a claim.
	MinConfidence float64

	// ValidateEvidenceExists checks that evidence quotes exist in file content.
	ValidateEvidenceExists bool
}

// DefaultStructuredOutputConfig returns sensible defaults.
func DefaultStructuredOutputConfig() *StructuredOutputConfig {
	return &StructuredOutputConfig{
		Enabled:                false, // Opt-in since it changes response format
		RequireJSON:            false, // Start with relaxed requirements
		RequireEvidenceQuotes:  true,
		MinConfidence:          0.5,
		ValidateEvidenceExists: true,
	}
}

// StructuredResponse is the required JSON output format for grounded responses.
//
// When structured output is enabled, the LLM is instructed to respond in this
// JSON format, making claims machine-parseable and easier to validate.
type StructuredResponse struct {
	// Summary is a brief answer to the user's question.
	Summary string `json:"summary"`

	// Claims are the factual claims made in the response.
	Claims []StructuredClaim `json:"claims"`

	// FilesExamined lists the files the LLM saw in context.
	FilesExamined []string `json:"files_examined"`

	// ToolsUsed lists the tools the LLM used.
	ToolsUsed []string `json:"tools_used"`

	// Uncertainty describes what the LLM is not sure about.
	Uncertainty string `json:"uncertainty,omitempty"`
}

// StructuredClaim represents a single factual claim with evidence.
type StructuredClaim struct {
	// Statement is the factual claim being made.
	Statement string `json:"statement"`

	// File is the file that supports this claim.
	File string `json:"file"`

	// LineStart is the starting line number.
	LineStart int `json:"line_start"`

	// LineEnd is the ending line number.
	LineEnd int `json:"line_end"`

	// EvidenceQuote is the exact text from the file.
	EvidenceQuote string `json:"evidence_quote"`

	// Confidence is how certain the LLM is (0.0-1.0).
	Confidence float64 `json:"confidence"`
}

// StructuredOutputChecker validates structured JSON responses.
//
// This is Layer 2 of the anti-hallucination defense system. It parses
// JSON responses and validates that claims have proper evidence.
//
// Thread Safety: Safe for concurrent use after construction.
type StructuredOutputChecker struct {
	config *StructuredOutputConfig
}

// NewStructuredOutputChecker creates a new structured output checker.
//
// Inputs:
//
//	config - Configuration for the checker. If nil, defaults are used.
//
// Outputs:
//
//	*StructuredOutputChecker - The configured checker.
func NewStructuredOutputChecker(config *StructuredOutputConfig) *StructuredOutputChecker {
	if config == nil {
		config = DefaultStructuredOutputConfig()
	}
	return &StructuredOutputChecker{config: config}
}

// Name returns the checker name for logging and metrics.
func (c *StructuredOutputChecker) Name() string {
	return "structured_output"
}

// Check validates a structured response against evidence.
//
// Description:
//
//	Attempts to parse the response as JSON. If successful, validates
//	each claim's file reference and evidence quote. If parsing fails,
//	handles gracefully based on configuration.
//
// Thread Safety: Safe for concurrent use.
func (c *StructuredOutputChecker) Check(ctx context.Context, input *CheckInput) []Violation {
	if !c.config.Enabled || input == nil || input.Response == "" {
		return nil
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return nil
	default:
	}

	// Try to parse as JSON
	resp, err := c.parseResponse(input.Response)
	if err != nil {
		// Response is not JSON - this may be acceptable
		if c.config.RequireJSON {
			return []Violation{{
				Type:     ViolationUngrounded,
				Severity: SeverityWarning,
				Code:     "STRUCTURED_PARSE_FAILED",
				Message:  fmt.Sprintf("Response is not valid JSON: %v", err),
			}}
		}
		// Non-JSON response is OK, skip structured validation
		return nil
	}

	// Validate the structured response
	return c.validateStructuredResponse(resp, input)
}

// parseResponse attempts to parse the response as a StructuredResponse.
func (c *StructuredOutputChecker) parseResponse(response string) (*StructuredResponse, error) {
	// Try direct parse
	var resp StructuredResponse
	if err := json.Unmarshal([]byte(response), &resp); err == nil {
		return &resp, nil
	}

	// Try to extract JSON from markdown code blocks
	cleaned := c.extractJSON(response)
	if cleaned != "" {
		if err := json.Unmarshal([]byte(cleaned), &resp); err == nil {
			return &resp, nil
		}
	}

	return nil, fmt.Errorf("unable to parse as JSON")
}

// extractJSON tries to extract JSON from markdown code blocks.
func (c *StructuredOutputChecker) extractJSON(response string) string {
	// Look for ```json blocks
	startMarkers := []string{"```json\n", "```json\r\n", "```\n", "```\r\n"}
	endMarker := "```"

	for _, startMarker := range startMarkers {
		startIdx := strings.Index(response, startMarker)
		if startIdx == -1 {
			continue
		}

		contentStart := startIdx + len(startMarker)
		remaining := response[contentStart:]
		endIdx := strings.Index(remaining, endMarker)
		if endIdx == -1 {
			continue
		}

		return strings.TrimSpace(remaining[:endIdx])
	}

	// Try to find bare JSON object
	startIdx := strings.Index(response, "{")
	endIdx := strings.LastIndex(response, "}")
	if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
		return response[startIdx : endIdx+1]
	}

	return ""
}

// validateStructuredResponse validates a parsed structured response.
func (c *StructuredOutputChecker) validateStructuredResponse(resp *StructuredResponse, input *CheckInput) []Violation {
	var violations []Violation

	evidence := input.EvidenceIndex
	if evidence == nil {
		evidence = NewEvidenceIndex()
	}

	// Validate files_examined
	for _, file := range resp.FilesExamined {
		if !evidence.Files[file] && !evidence.FileBasenames[getBasename(file)] {
			violations = append(violations, Violation{
				Type:     ViolationUngrounded,
				Severity: SeverityWarning,
				Code:     "STRUCTURED_FILE_NOT_SEEN",
				Message:  fmt.Sprintf("files_examined includes file not in evidence: %s", file),
				Evidence: file,
			})
		}
	}

	// Validate each claim
	for i, claim := range resp.Claims {
		claimViolations := c.validateClaim(i, claim, evidence)
		violations = append(violations, claimViolations...)
	}

	return violations
}

// validateClaim validates a single structured claim.
func (c *StructuredOutputChecker) validateClaim(index int, claim StructuredClaim, evidence *EvidenceIndex) []Violation {
	var violations []Violation
	claimRef := fmt.Sprintf("claim[%d]", index)

	// Check confidence threshold
	if claim.Confidence < c.config.MinConfidence {
		violations = append(violations, Violation{
			Type:     ViolationUngrounded,
			Severity: SeverityWarning,
			Code:     "STRUCTURED_LOW_CONFIDENCE",
			Message:  fmt.Sprintf("%s has low confidence: %.2f", claimRef, claim.Confidence),
			Evidence: claim.Statement,
		})
	}

	// Validate file reference
	if claim.File == "" {
		if c.config.RequireEvidenceQuotes {
			violations = append(violations, Violation{
				Type:     ViolationUngrounded,
				Severity: SeverityWarning,
				Code:     "STRUCTURED_NO_FILE",
				Message:  fmt.Sprintf("%s has no file reference", claimRef),
				Evidence: claim.Statement,
			})
		}
		return violations
	}

	// Check if file is in evidence
	basename := getBasename(claim.File)
	if !evidence.Files[claim.File] && !evidence.FileBasenames[basename] {
		violations = append(violations, Violation{
			Type:     ViolationFileNotFound,
			Severity: SeverityCritical,
			Code:     "STRUCTURED_FILE_NOT_FOUND",
			Message:  fmt.Sprintf("%s references file not in evidence: %s", claimRef, claim.File),
			Evidence: claim.File,
		})
		return violations
	}

	// Validate evidence quote if required
	if c.config.RequireEvidenceQuotes && claim.EvidenceQuote == "" {
		violations = append(violations, Violation{
			Type:     ViolationUngrounded,
			Severity: SeverityWarning,
			Code:     "STRUCTURED_NO_EVIDENCE_QUOTE",
			Message:  fmt.Sprintf("%s has no evidence quote", claimRef),
			Evidence: claim.Statement,
		})
		return violations
	}

	// Validate evidence quote exists in file content
	if c.config.ValidateEvidenceExists && claim.EvidenceQuote != "" {
		if !c.evidenceExistsInFile(claim, evidence) {
			violations = append(violations, Violation{
				Type:     ViolationEvidenceMismatch,
				Severity: SeverityCritical,
				Code:     "STRUCTURED_EVIDENCE_MISMATCH",
				Message:  fmt.Sprintf("%s evidence quote not found in file %s", claimRef, claim.File),
				Evidence: claim.EvidenceQuote,
			})
		}
	}

	return violations
}

// evidenceExistsInFile checks if the evidence quote exists in the file content.
func (c *StructuredOutputChecker) evidenceExistsInFile(claim StructuredClaim, evidence *EvidenceIndex) bool {
	// Normalize the evidence quote for matching
	normalizedQuote := normalizeForComparison(claim.EvidenceQuote)
	if normalizedQuote == "" {
		return false
	}

	// Check in specific file content
	if content, ok := evidence.FileContents[claim.File]; ok {
		if strings.Contains(normalizeForComparison(content), normalizedQuote) {
			return true
		}
	}

	// Check by basename
	basename := getBasename(claim.File)
	for filePath, content := range evidence.FileContents {
		if getBasename(filePath) == basename {
			if strings.Contains(normalizeForComparison(content), normalizedQuote) {
				return true
			}
		}
	}

	// Fallback: check raw content
	if evidence.RawContent != "" {
		if strings.Contains(normalizeForComparison(evidence.RawContent), normalizedQuote) {
			return true
		}
	}

	return false
}

// normalizeForComparison normalizes a string for fuzzy comparison.
func normalizeForComparison(s string) string {
	// Remove extra whitespace and normalize line endings
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\t", " ")

	// Collapse multiple spaces to single space
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}

	return strings.TrimSpace(s)
}

// ValidateStructuredResponse validates a StructuredResponse against evidence.
//
// This is a standalone function for use outside the checker pipeline.
//
// Inputs:
//
//	resp - The structured response to validate.
//	evidence - The evidence index from context.
//
// Outputs:
//
//	[]Violation - Any violations found.
func ValidateStructuredResponse(resp *StructuredResponse, evidence *EvidenceIndex) []Violation {
	checker := NewStructuredOutputChecker(&StructuredOutputConfig{
		Enabled:                true,
		RequireEvidenceQuotes:  true,
		ValidateEvidenceExists: true,
		MinConfidence:          0.5,
	})

	input := &CheckInput{
		EvidenceIndex: evidence,
	}

	return checker.validateStructuredResponse(resp, input)
}

// ParseStructuredResponse attempts to parse a response as StructuredResponse.
//
// Inputs:
//
//	response - The response text to parse.
//
// Outputs:
//
//	*StructuredResponse - The parsed response, or nil if parsing fails.
//	error - Non-nil if parsing fails.
func ParseStructuredResponse(response string) (*StructuredResponse, error) {
	checker := NewStructuredOutputChecker(nil)
	return checker.parseResponse(response)
}

// StructuredOutputSystemPrompt returns the system prompt addition for structured output.
//
// This prompt instructs the LLM to respond in the StructuredResponse JSON format.
func StructuredOutputSystemPrompt() string {
	return `RESPONSE FORMAT (MANDATORY):
You MUST respond in the following JSON format. Do NOT use free-form text.

{
  "summary": "Brief answer to the user's question",
  "claims": [
    {
      "statement": "A specific factual claim",
      "file": "path/to/file.go",
      "line_start": 10,
      "line_end": 20,
      "evidence_quote": "Exact text from the file that supports this claim",
      "confidence": 0.9
    }
  ],
  "files_examined": ["list", "of", "files", "you", "saw"],
  "tools_used": ["read_file", "search_symbol"],
  "uncertainty": "Things you're not sure about (optional)"
}

RULES:
1. Every claim MUST have a file and line reference
2. evidence_quote MUST be exact text from the file (copy-paste)
3. confidence should reflect how certain you are (0.0-1.0)
4. Only include files you actually saw in files_examined`
}
