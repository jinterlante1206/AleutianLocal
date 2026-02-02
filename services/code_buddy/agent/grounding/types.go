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
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent"
)

// Severity indicates the severity of a grounding violation.
type Severity string

const (
	// SeverityInfo is for informational messages.
	SeverityInfo Severity = "info"

	// SeverityWarning is for warnings that should be reviewed.
	SeverityWarning Severity = "warning"

	// SeverityHigh is for high-severity issues requiring attention.
	SeverityHigh Severity = "high"

	// SeverityCritical is for critical issues that indicate hallucination.
	SeverityCritical Severity = "critical"
)

// ViolationType categorizes the kind of grounding failure.
type ViolationType string

const (
	// ViolationWrongLanguage indicates response contains wrong language patterns.
	ViolationWrongLanguage ViolationType = "wrong_language"

	// ViolationFileNotFound indicates referenced file doesn't exist.
	ViolationFileNotFound ViolationType = "file_not_found"

	// ViolationSymbolNotFound indicates referenced symbol doesn't exist.
	ViolationSymbolNotFound ViolationType = "symbol_not_found"

	// ViolationCitationInvalid indicates a [file:line] citation is invalid.
	ViolationCitationInvalid ViolationType = "citation_invalid"

	// ViolationNoCitations indicates response makes claims without citations.
	ViolationNoCitations ViolationType = "no_citations"

	// ViolationUngrounded indicates a claim is not grounded in evidence.
	ViolationUngrounded ViolationType = "ungrounded"

	// ViolationContradiction indicates response contradicts its context.
	ViolationContradiction ViolationType = "contradiction"

	// ViolationEvidenceMismatch indicates quoted evidence doesn't match file.
	ViolationEvidenceMismatch ViolationType = "evidence_mismatch"

	// ViolationPhantomFile indicates reference to a file that doesn't exist.
	// This is the highest priority violation as it invalidates other claims.
	ViolationPhantomFile ViolationType = "phantom_file"

	// ViolationStructuralClaim indicates fabricated directory/file structure.
	// Structural claims about project layout without supporting evidence.
	ViolationStructuralClaim ViolationType = "structural_claim"

	// ViolationLanguageConfusion indicates wrong-language patterns in response.
	// E.g., describing Flask patterns in a Go codebase.
	ViolationLanguageConfusion ViolationType = "language_confusion"

	// ViolationGenericPattern indicates generic descriptions without grounding.
	// Response describes common patterns instead of project-specific findings.
	ViolationGenericPattern ViolationType = "generic_pattern"
)

// ViolationPriority defines processing order for violation types.
// Lower values = higher priority (processed first).
// This is used to sort violations and deduplicate cascade violations.
type ViolationPriority int

const (
	// PriorityPhantomFile is highest priority - file doesn't exist at all.
	// Always process first because it invalidates any other claims about that file.
	PriorityPhantomFile ViolationPriority = 1

	// PriorityStructuralClaim is high priority - fabricated directory structure.
	// Process after phantom because a phantom file in a structure claim should
	// be reported as PhantomFile, not StructuralClaim.
	PriorityStructuralClaim ViolationPriority = 2

	// PriorityLanguageConfusion is medium priority - wrong language patterns.
	// Lower priority because language confusion is often a symptom of phantom
	// file references (referencing imaginary Python files in a Go project).
	PriorityLanguageConfusion ViolationPriority = 3

	// PriorityGenericPattern is low priority - generic descriptions.
	// Lowest priority as it's the least severe form of hallucination.
	PriorityGenericPattern ViolationPriority = 4

	// PriorityOther is for existing violation types not in the hierarchy.
	PriorityOther ViolationPriority = 5
)

// Violation represents a single grounding failure.
type Violation struct {
	// Type is the kind of violation.
	Type ViolationType `json:"type"`

	// Severity indicates how serious the violation is.
	Severity Severity `json:"severity"`

	// Code is a machine-readable issue code.
	Code string `json:"code"`

	// Message is a human-readable description.
	Message string `json:"message"`

	// Evidence is what triggered this violation.
	Evidence string `json:"evidence,omitempty"`

	// Expected is what should exist instead.
	Expected string `json:"expected,omitempty"`

	// Location is where in the response this occurred.
	Location string `json:"location,omitempty"`

	// Suggestion provides guidance on how to fix the violation.
	Suggestion string `json:"suggestion,omitempty"`

	// Phase indicates when this violation was detected ("pre_synthesis" or "post_synthesis").
	Phase string `json:"phase,omitempty"`

	// RetryCount indicates which retry attempt detected this violation.
	RetryCount int `json:"retry_count,omitempty"`

	// LocationOffset is the character position in the response (for sorting).
	LocationOffset int `json:"location_offset,omitempty"`
}

// Result contains the outcome of grounding validation.
type Result struct {
	// Grounded is true if the response is sufficiently grounded.
	Grounded bool `json:"grounded"`

	// Confidence is a score from 0.0 to 1.0 indicating grounding confidence.
	Confidence float64 `json:"confidence"`

	// Violations contains all violations found during checking.
	Violations []Violation `json:"violations,omitempty"`

	// CriticalCount is the number of critical violations.
	CriticalCount int `json:"critical_count"`

	// WarningCount is the number of warnings.
	WarningCount int `json:"warning_count"`

	// ChecksRun is the number of grounding checks that were executed.
	ChecksRun int `json:"checks_run"`

	// CheckDuration is how long the grounding check took.
	CheckDuration time.Duration `json:"check_duration"`

	// CitationsFound is the number of citations found in the response.
	CitationsFound int `json:"citations_found"`

	// CitationsValid is the number of valid citations.
	CitationsValid int `json:"citations_valid"`
}

// HasCritical returns true if there are critical violations.
func (r *Result) HasCritical() bool {
	return r.CriticalCount > 0
}

// HasWarnings returns true if there are warnings.
func (r *Result) HasWarnings() bool {
	return r.WarningCount > 0
}

// AddViolation adds a violation to the result.
func (r *Result) AddViolation(v Violation) {
	r.Violations = append(r.Violations, v)
	switch v.Severity {
	case SeverityCritical:
		r.CriticalCount++
		r.Confidence -= 0.3
	case SeverityHigh:
		r.CriticalCount++ // High severity counts as critical for rejection purposes
		r.Confidence -= 0.25
	case SeverityWarning:
		r.WarningCount++
		r.Confidence -= 0.1
	}
	if r.Confidence < 0 {
		r.Confidence = 0
	}
}

// Grounder validates LLM responses against project reality.
//
// Implementations validate that responses are grounded in actual code
// and do not contain hallucinated content.
//
// Thread Safety: Implementations must be safe for concurrent use.
type Grounder interface {
	// Validate checks if a response is grounded in project reality.
	//
	// Inputs:
	//   ctx - Context for cancellation.
	//   response - The LLM response content.
	//   assembledCtx - The context that was given to the LLM.
	//
	// Outputs:
	//   *Result - The validation result.
	//   error - Non-nil only if validation itself fails.
	Validate(ctx context.Context, response string, assembledCtx *agent.AssembledContext) (*Result, error)

	// ShouldReject determines if the result warrants rejection.
	//
	// Inputs:
	//   result - The validation result.
	//
	// Outputs:
	//   bool - True if the response should be rejected.
	ShouldReject(result *Result) bool

	// GenerateFootnote creates a warning footnote for questionable responses.
	//
	// Inputs:
	//   result - The validation result.
	//
	// Outputs:
	//   string - A footnote to append to the response, or empty if not needed.
	GenerateFootnote(result *Result) string
}

// Checker is a single grounding check.
//
// Each checker focuses on one aspect of grounding validation.
// Multiple checkers are composed to form the complete validation pipeline.
//
// Thread Safety: Implementations must be safe for concurrent use.
type Checker interface {
	// Name returns the checker name for logging and metrics.
	Name() string

	// Check runs the grounding check.
	//
	// Inputs:
	//   ctx - Context for cancellation.
	//   input - The input data for checking.
	//
	// Outputs:
	//   []Violation - Any violations found.
	Check(ctx context.Context, input *CheckInput) []Violation
}

// CheckInput provides all data needed for a grounding check.
type CheckInput struct {
	// Response is the LLM response text.
	Response string

	// ProjectRoot is the absolute path to the project root.
	ProjectRoot string

	// ProjectLang is the primary language of the project (e.g., "go", "python").
	ProjectLang string

	// KnownFiles maps file paths that exist in the project to true.
	KnownFiles map[string]bool

	// KnownSymbols maps symbol names that exist in the graph to true.
	KnownSymbols map[string]bool

	// CodeContext is the code that was shown to the LLM.
	CodeContext []agent.CodeEntry

	// ToolResults are the tool outputs the LLM saw.
	ToolResults []ToolResult

	// EvidenceIndex is the pre-built evidence index (optional).
	EvidenceIndex *EvidenceIndex
}

// ToolResult is a simplified tool result for grounding checks.
type ToolResult struct {
	// InvocationID links back to the invocation.
	InvocationID string

	// Output is the tool's output text.
	Output string
}

// EvidenceIndex tracks exactly what the LLM was shown.
//
// This is built BEFORE sending to LLM and used AFTER receiving response
// to validate that claims are grounded in actual evidence.
type EvidenceIndex struct {
	// Files that were shown (normalized paths).
	Files map[string]bool

	// FileBasenames maps base filenames for convenience.
	FileBasenames map[string]bool

	// Symbols (function/type names) that appeared in context.
	Symbols map[string]bool

	// Frameworks/libraries mentioned in shown code.
	Frameworks map[string]bool

	// Languages of shown code.
	Languages map[string]bool

	// FileContents maps file paths to their content.
	FileContents map[string]string

	// RawContent is concatenated content for substring matching.
	RawContent string
}

// NewEvidenceIndex creates a new empty evidence index.
func NewEvidenceIndex() *EvidenceIndex {
	return &EvidenceIndex{
		Files:         make(map[string]bool),
		FileBasenames: make(map[string]bool),
		Symbols:       make(map[string]bool),
		Frameworks:    make(map[string]bool),
		Languages:     make(map[string]bool),
		FileContents:  make(map[string]string),
	}
}

// Citation represents a parsed citation from LLM response.
type Citation struct {
	// Raw is the original text (e.g., "[main.go:45]").
	Raw string

	// FilePath is the extracted file path.
	FilePath string

	// StartLine is the starting line number.
	StartLine int

	// EndLine is the ending line number (same as StartLine for single line).
	EndLine int

	// Position is the character position in the response.
	Position int
}

// Claim represents a factual claim extracted from the response.
type Claim struct {
	// Type is the kind of claim.
	Type ClaimType

	// Value is the claimed value (file path, symbol name, etc.).
	Value string

	// RawText is the original text that contained this claim.
	RawText string

	// Position is where in the response this claim was found.
	Position int
}

// ClaimType categorizes claims.
type ClaimType int

const (
	// ClaimFile is a claim about a file.
	ClaimFile ClaimType = iota

	// ClaimSymbol is a claim about a symbol (function, type, etc.).
	ClaimSymbol

	// ClaimFramework is a claim about a framework or library.
	ClaimFramework

	// ClaimLanguage is a claim about a programming language.
	ClaimLanguage
)
