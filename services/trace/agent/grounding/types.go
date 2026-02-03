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

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
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

	// ViolationPhantomSymbol indicates reference to a symbol that doesn't exist.
	// The file exists but the referenced function/type/variable does not.
	// This is distinct from ViolationSymbolNotFound which is for cited symbols
	// that should exist based on citation format but don't.
	ViolationPhantomSymbol ViolationType = "phantom_symbol"

	// ViolationSemanticDrift indicates response doesn't address the original question.
	// The response may be internally consistent but completely irrelevant to what was asked.
	// This is the highest priority violation as the entire response is invalid.
	ViolationSemanticDrift ViolationType = "semantic_drift"

	// ViolationAttributeHallucination indicates wrong attributes about real code elements.
	// The referenced element exists but the properties described are fabricated
	// (wrong return types, wrong parameter counts, wrong field names).
	ViolationAttributeHallucination ViolationType = "attribute_hallucination"

	// ViolationLineNumberFabrication indicates fabricated or incorrect line numbers.
	// The file exists and may be in context, but the cited line number is wrong.
	// This includes lines beyond file length and symbol location mismatches.
	ViolationLineNumberFabrication ViolationType = "line_number_fabrication"

	// ViolationRelationshipHallucination indicates fabricated relationships between code elements.
	// Claims about function calls, imports, or interface implementations that don't exist.
	// E.g., "A calls B" when A doesn't call B, or "X imports Y" when X doesn't import Y.
	ViolationRelationshipHallucination ViolationType = "relationship_hallucination"

	// ViolationBehavioralHallucination indicates fabricated claims about code behavior.
	// Claims about what code does (validates, logs, encrypts) that are contradicted by the code.
	// E.g., "validates input" when there's no validation, "logs errors" when they're swallowed.
	ViolationBehavioralHallucination ViolationType = "behavioral_hallucination"

	// ViolationQuantitativeHallucination indicates incorrect numeric claims.
	// Wrong file counts, line counts, function counts, etc.
	// E.g., "15 test files" when there are 3, "200 lines" when file has 52.
	ViolationQuantitativeHallucination ViolationType = "quantitative_hallucination"

	// ViolationFabricatedCode indicates code snippet not found in evidence.
	// Model shows code blocks that don't exist in the codebase,
	// inventing implementations or showing "improved" code as original.
	// This is a critical violation - actively misleading users.
	ViolationFabricatedCode ViolationType = "fabricated_code"

	// ViolationAPIHallucination indicates claims about libraries/APIs not in evidence.
	// Model claims usage of libraries not imported, or confuses similar libraries
	// (e.g., claims "uses gorm" when project actually uses sqlx).
	ViolationAPIHallucination ViolationType = "api_hallucination"

	// ViolationTemporalHallucination indicates unverifiable claims about code history.
	// Model makes claims about when code was added, changed, refactored, or versioned
	// without access to git history. Examples: "recently refactored", "added in v2.0",
	// "originally implemented for X", "was changed because Y".
	ViolationTemporalHallucination ViolationType = "temporal_hallucination"

	// ViolationCrossContextConfusion indicates mixing information from different code locations.
	// Model conflates attributes, fields, or behaviors from different instances of
	// similarly-named symbols. E.g., describing Config in pkg/server with fields
	// that actually belong to Config in pkg/client.
	ViolationCrossContextConfusion ViolationType = "cross_context_confusion"

	// ViolationConfidenceFabrication indicates overconfident claims without supporting evidence.
	// Model asserts certainty ("always", "never", "all", "none") when evidence is weak
	// or absent. E.g., "all inputs are validated" when only one validation was seen,
	// or "there is no error logging" after searching only 3 files.
	ViolationConfidenceFabrication ViolationType = "confidence_fabrication"

	// ViolationPhantomPackage indicates reference to a package path that doesn't exist.
	// The model mentions pkg/config, cmd/database, etc. that are not in the codebase.
	// This is conformity hallucination - assuming standard patterns exist.
	// E.g., claiming "pkg/config" handles configuration when no such package exists.
	ViolationPhantomPackage ViolationType = "phantom_package"
)

// ViolationPriority defines processing order for violation types.
// Lower values = higher priority (processed first).
// This is used to sort violations and deduplicate cascade violations.
type ViolationPriority int

const (
	// PrioritySemanticDrift is highest priority - response doesn't address the question.
	// Process first because if the entire response is off-topic, other violations are moot.
	PrioritySemanticDrift ViolationPriority = 0

	// PriorityPhantomFile is high priority - file doesn't exist at all.
	// Always process early because it invalidates any other claims about that file.
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

	// PriorityPhantomSymbol is high priority - symbol doesn't exist in file.
	// Process after phantom file (P1) but at same level as structural claim (P2).
	PriorityPhantomSymbol ViolationPriority = 2

	// PriorityAttributeHallucination is high priority - wrong attributes on real symbols.
	// Same level as phantom symbol (P2) as it's a factual error about existing code.
	PriorityAttributeHallucination ViolationPriority = 2

	// PriorityLineNumberFabrication is medium priority - incorrect line citations.
	// Less severe than phantom symbol since the file/symbol may exist, just wrong location.
	PriorityLineNumberFabrication ViolationPriority = 3

	// PriorityRelationshipHallucination is high priority - fabricated code relationships.
	// Wrong understanding of system architecture (calls, imports, dependencies).
	PriorityRelationshipHallucination ViolationPriority = 2

	// PriorityBehavioralHallucination is high priority - fabricated behavior claims.
	// Wrong claims about what code does (validation, logging, security).
	// Same level as relationship hallucination (P2) due to security implications.
	PriorityBehavioralHallucination ViolationPriority = 2

	// PriorityQuantitativeHallucination is medium priority - incorrect numeric claims.
	// Wrong counts are factual errors but less severe than structural hallucinations.
	// Same level as language confusion (P3).
	PriorityQuantitativeHallucination ViolationPriority = 3

	// PriorityFabricatedCode is critical priority - invented code shown as real.
	// Same level as PhantomFile (P1) - both are actively misleading.
	// Fabricated code is as dangerous as claiming a file exists when it doesn't.
	PriorityFabricatedCode ViolationPriority = 1

	// PriorityAPIHallucination is high priority - incorrect library/API claims.
	// Same level as structural claim (P2) - factual error about project dependencies.
	PriorityAPIHallucination ViolationPriority = 2

	// PriorityTemporalHallucination is low priority - unverifiable history claims.
	// Same level as generic pattern (P4) - often harmless filler.
	// Temporal claims are unverifiable without git access, not provably wrong.
	PriorityTemporalHallucination ViolationPriority = 4

	// PriorityCrossContextConfusion is high priority - mixing up different code locations.
	// Same level as structural claim (P2) - factual error about code structure.
	// Conflating information from different symbols is a serious hallucination.
	PriorityCrossContextConfusion ViolationPriority = 2

	// PriorityConfidenceFabrication is medium priority - overconfident claims.
	// Same level as line number fabrication (P3) - epistemically problematic but
	// not necessarily factually wrong. The claim might be true, just not supported.
	PriorityConfidenceFabrication ViolationPriority = 3

	// PriorityPhantomPackage is high priority - package path doesn't exist.
	// Same level as structural claim (P2) - factual error about project structure.
	// Model claims pkg/config exists when it doesn't - this is conformity hallucination.
	PriorityPhantomPackage ViolationPriority = 2
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

	// UserQuestion is the original user question being answered.
	// Used by SemanticDriftChecker to verify response addresses the question.
	UserQuestion string

	// ProjectRoot is the absolute path to the project root.
	ProjectRoot string

	// ProjectLang is the primary language of the project (e.g., "go", "python").
	ProjectLang string

	// KnownFiles maps file paths that exist in the project to true.
	KnownFiles map[string]bool

	// KnownSymbols maps symbol names that exist in the graph to true.
	KnownSymbols map[string]bool

	// KnownPackages maps package paths that exist in the project to true.
	// Derived from graph file paths (e.g., "pkg/calcs", "cmd/orchestrator").
	// Used by PhantomPackageChecker to validate package path references.
	KnownPackages map[string]bool

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

	// SymbolDetails maps symbol names to detailed information.
	// Key: symbol name, Value: list of SymbolInfo for all locations.
	// This provides richer information than Symbols map for validation.
	SymbolDetails map[string][]SymbolInfo

	// Frameworks/libraries mentioned in shown code.
	Frameworks map[string]bool

	// Languages of shown code.
	Languages map[string]bool

	// FileContents maps file paths to their content.
	FileContents map[string]string

	// FileLines maps file paths to their total line count.
	// Populated during evidence building by counting newlines in content.
	// Used by LineNumberChecker to validate cited line numbers.
	FileLines map[string]int

	// Imports maps file paths to their imported package paths.
	// Key: normalized file path, Value: list of import paths.
	// Used by RelationshipChecker to validate import claims.
	Imports map[string][]ImportInfo

	// CallsWithin tracks function calls within shown code.
	// Key: "FunctionName" or "pkg.FunctionName", Value: list of called functions.
	// Only populated for functions defined in evidence.
	// Used by RelationshipChecker to validate call relationship claims.
	CallsWithin map[string][]string

	// RawContent is concatenated content for substring matching.
	RawContent string
}

// ImportInfo tracks an import statement with optional alias.
type ImportInfo struct {
	// Path is the import path (e.g., "github.com/pkg/errors").
	Path string

	// Alias is the local name if aliased, or the package name from path.
	// For `import foo "pkg/bar"`, Alias is "foo".
	// For `import "pkg/bar"`, Alias is "bar".
	Alias string
}

// NewEvidenceIndex creates a new empty evidence index.
func NewEvidenceIndex() *EvidenceIndex {
	return &EvidenceIndex{
		Files:         make(map[string]bool),
		FileBasenames: make(map[string]bool),
		Symbols:       make(map[string]bool),
		SymbolDetails: make(map[string][]SymbolInfo),
		Frameworks:    make(map[string]bool),
		Languages:     make(map[string]bool),
		FileContents:  make(map[string]string),
		FileLines:     make(map[string]int),
		Imports:       make(map[string][]ImportInfo),
		CallsWithin:   make(map[string][]string),
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

// SymbolInfo contains detailed information about a symbol.
//
// This tracks where symbols are defined in the codebase, enabling
// validation that referenced symbols actually exist and have correct attributes.
type SymbolInfo struct {
	// Name is the symbol name (function, type, variable, constant name).
	Name string

	// Kind is the symbol kind: "function", "type", "variable", "constant", "method".
	Kind string

	// File is the file path where the symbol is defined.
	File string

	// Line is the line number where the symbol is defined.
	Line int

	// ReturnTypes lists the return types for functions/methods (in order).
	// For "func Parse() (*Result, error)", this is ["*Result", "error"].
	// Empty for non-functions.
	ReturnTypes []string

	// Parameters lists the parameter types for functions/methods (in order).
	// For "func Parse(ctx context.Context, data []byte)", this is ["context.Context", "[]byte"].
	// Empty for non-functions.
	Parameters []string

	// Fields lists the field names for structs.
	// For "type Config struct { Name string; Value int }", this is ["Name", "Value"].
	// Empty for non-structs.
	Fields []string

	// Methods lists the method names for interfaces.
	// For "type Reader interface { Read() }", this is ["Read"].
	// Empty for non-interfaces.
	Methods []string

	// Receiver is the receiver type for methods (e.g., "*Config" for pointer receiver).
	// Empty for functions and non-method types.
	Receiver string
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
