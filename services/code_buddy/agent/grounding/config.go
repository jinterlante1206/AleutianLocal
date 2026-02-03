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

import "time"

// Config configures the grounding validation behavior.
type Config struct {
	// Enabled determines if grounding checks are enabled.
	Enabled bool

	// RejectOnCritical rejects responses with critical violations.
	RejectOnCritical bool

	// AddFootnoteOnWarning adds warning footnotes to responses.
	AddFootnoteOnWarning bool

	// MaxViolationsBeforeReject triggers rejection at this threshold.
	MaxViolationsBeforeReject int

	// MinConfidence is the minimum confidence to be considered grounded.
	MinConfidence float64

	// MaxResponseScanLength limits how much of the response to scan.
	MaxResponseScanLength int

	// Timeout is the maximum time for grounding checks.
	Timeout time.Duration

	// ShortCircuitOnCritical stops checking after first critical violation.
	ShortCircuitOnCritical bool

	// MaxHallucinationRetries is the circuit breaker limit.
	MaxHallucinationRetries int

	// LanguageCheckerConfig configures the language consistency checker.
	LanguageCheckerConfig *LanguageCheckerConfig

	// CitationCheckerConfig configures the citation checker.
	CitationCheckerConfig *CitationCheckerConfig

	// GroundingCheckerConfig configures the grounding (claim validation) checker.
	GroundingCheckerConfig *GroundingCheckerConfig

	// TMSVerifierConfig configures the TMS-based claim verifier.
	TMSVerifierConfig *TMSVerifierConfig

	// StructuredOutputConfig configures the structured output validator.
	StructuredOutputConfig *StructuredOutputConfig

	// ChainOfVerificationConfig configures the chain-of-verification verifier.
	ChainOfVerificationConfig *ChainOfVerificationConfig

	// MultiSampleConfig configures the multi-sample consistency verifier.
	MultiSampleConfig *MultiSampleConfig

	// StructuralClaimCheckerConfig configures the structural claim checker.
	StructuralClaimCheckerConfig *StructuralClaimCheckerConfig

	// PhantomCheckerConfig configures the phantom file checker.
	PhantomCheckerConfig *PhantomCheckerConfig

	// PostSynthesisConfig configures post-synthesis verification.
	PostSynthesisConfig *PostSynthesisConfig

	// AnchoredSynthesisConfig configures tool-anchored synthesis prompts.
	AnchoredSynthesisConfig *AnchoredSynthesisConfig

	// PhantomSymbolCheckerConfig configures the phantom symbol checker.
	PhantomSymbolCheckerConfig *PhantomSymbolCheckerConfig

	// SemanticDriftCheckerConfig configures the semantic drift checker.
	SemanticDriftCheckerConfig *SemanticDriftCheckerConfig

	// AttributeCheckerConfig configures the attribute hallucination checker.
	AttributeCheckerConfig *AttributeCheckerConfig

	// LineNumberCheckerConfig configures the line number fabrication checker.
	LineNumberCheckerConfig *LineNumberCheckerConfig

	// RelationshipCheckerConfig configures the relationship hallucination checker.
	RelationshipCheckerConfig *RelationshipCheckerConfig

	// BehavioralCheckerConfig configures the behavioral hallucination checker.
	BehavioralCheckerConfig *BehavioralCheckerConfig

	// QuantitativeCheckerConfig configures the quantitative hallucination checker.
	QuantitativeCheckerConfig *QuantitativeCheckerConfig

	// CodeSnippetCheckerConfig configures the fabricated code snippet checker.
	CodeSnippetCheckerConfig *CodeSnippetCheckerConfig

	// APILibraryCheckerConfig configures the API/library hallucination checker.
	APILibraryCheckerConfig *APILibraryCheckerConfig

	// TemporalCheckerConfig configures the temporal hallucination checker.
	TemporalCheckerConfig *TemporalCheckerConfig

	// CrossContextCheckerConfig configures the cross-context confusion checker.
	CrossContextCheckerConfig *CrossContextCheckerConfig

	// ConfidenceCheckerConfig configures the confidence fabrication checker.
	ConfidenceCheckerConfig *ConfidenceCheckerConfig
}

// DefaultConfig returns sensible defaults for grounding configuration.
//
// Outputs:
//
//	Config - The default configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:                      true,
		RejectOnCritical:             true,
		AddFootnoteOnWarning:         true,
		MaxViolationsBeforeReject:    3,
		MinConfidence:                0.5,
		MaxResponseScanLength:        10000,
		Timeout:                      5 * time.Second,
		ShortCircuitOnCritical:       false,
		MaxHallucinationRetries:      3,
		LanguageCheckerConfig:        DefaultLanguageCheckerConfig(),
		CitationCheckerConfig:        DefaultCitationCheckerConfig(),
		GroundingCheckerConfig:       DefaultGroundingCheckerConfig(),
		TMSVerifierConfig:            DefaultTMSVerifierConfig(),
		StructuredOutputConfig:       DefaultStructuredOutputConfig(),
		ChainOfVerificationConfig:    DefaultChainOfVerificationConfig(),
		MultiSampleConfig:            DefaultMultiSampleConfig(),
		StructuralClaimCheckerConfig: DefaultStructuralClaimCheckerConfig(),
		PhantomCheckerConfig:         DefaultPhantomCheckerConfig(),
		PostSynthesisConfig:          DefaultPostSynthesisConfig(),
		AnchoredSynthesisConfig:      DefaultAnchoredSynthesisConfig(),
		PhantomSymbolCheckerConfig:   DefaultPhantomSymbolCheckerConfig(),
		SemanticDriftCheckerConfig:   DefaultSemanticDriftCheckerConfig(),
		AttributeCheckerConfig:       DefaultAttributeCheckerConfig(),
		LineNumberCheckerConfig:      DefaultLineNumberCheckerConfig(),
		RelationshipCheckerConfig:    DefaultRelationshipCheckerConfig(),
		BehavioralCheckerConfig:      DefaultBehavioralCheckerConfig(),
		QuantitativeCheckerConfig:    DefaultQuantitativeCheckerConfig(),
		CodeSnippetCheckerConfig:     DefaultCodeSnippetCheckerConfig(),
		APILibraryCheckerConfig:      DefaultAPILibraryCheckerConfig(),
		TemporalCheckerConfig:        DefaultTemporalCheckerConfig(),
		CrossContextCheckerConfig:    DefaultCrossContextCheckerConfig(),
		ConfidenceCheckerConfig:      DefaultConfidenceCheckerConfig(),
	}
}

// LanguageCheckerConfig configures the language consistency checker.
type LanguageCheckerConfig struct {
	// WeightThreshold is the accumulated weight needed to trigger a violation.
	WeightThreshold float64

	// EnablePython enables Python pattern detection.
	EnablePython bool

	// EnableJavaScript enables JavaScript/TypeScript pattern detection.
	EnableJavaScript bool

	// EnableGo enables Go pattern detection.
	EnableGo bool
}

// DefaultLanguageCheckerConfig returns default language checker config.
func DefaultLanguageCheckerConfig() *LanguageCheckerConfig {
	return &LanguageCheckerConfig{
		WeightThreshold:  1.5,
		EnablePython:     true,
		EnableJavaScript: true,
		EnableGo:         true,
	}
}

// CitationCheckerConfig configures the citation checker.
type CitationCheckerConfig struct {
	// RequireCitations requires citations for claims.
	RequireCitations bool

	// ValidateFileExists checks that cited files exist.
	ValidateFileExists bool

	// ValidateInContext checks that cited files were in context.
	ValidateInContext bool

	// ValidateLineRange checks that line numbers are valid.
	ValidateLineRange bool
}

// DefaultCitationCheckerConfig returns default citation checker config.
func DefaultCitationCheckerConfig() *CitationCheckerConfig {
	return &CitationCheckerConfig{
		RequireCitations:   true,
		ValidateFileExists: true,
		ValidateInContext:  true,
		ValidateLineRange:  true,
	}
}

// StructuralClaimCheckerConfig configures the structural claim checker.
type StructuralClaimCheckerConfig struct {
	// Enabled determines if structural claim checking is active.
	Enabled bool

	// RequireToolEvidence requires tool output (ls/find/tree) for structural claims.
	RequireToolEvidence bool

	// MaxPathsToExtract limits the number of paths extracted from tree structures.
	MaxPathsToExtract int
}

// DefaultStructuralClaimCheckerConfig returns default structural claim checker config.
func DefaultStructuralClaimCheckerConfig() *StructuralClaimCheckerConfig {
	return &StructuralClaimCheckerConfig{
		Enabled:             true,
		RequireToolEvidence: true,
		MaxPathsToExtract:   100,
	}
}

// PhantomCheckerConfig configures the phantom file checker.
type PhantomCheckerConfig struct {
	// Enabled determines if phantom file checking is active.
	Enabled bool

	// Extensions are the file extensions to check (e.g., ".go", ".py").
	// Empty means check all common code extensions.
	Extensions []string

	// MaxRefsToCheck limits how many file references to check per response.
	// Prevents excessive checking on large responses.
	MaxRefsToCheck int
}

// DefaultPhantomCheckerConfig returns default phantom checker config.
func DefaultPhantomCheckerConfig() *PhantomCheckerConfig {
	return &PhantomCheckerConfig{
		Enabled:        true,
		Extensions:     nil, // nil means all common code extensions
		MaxRefsToCheck: 100,
	}
}

// PhantomSymbolCheckerConfig configures the phantom symbol checker.
type PhantomSymbolCheckerConfig struct {
	// Enabled determines if phantom symbol checking is active.
	Enabled bool

	// RequireFileAssociation requires symbol to be associated with a specific file.
	// If true, only validates symbols that have file context.
	// If false, validates against all known symbols globally.
	RequireFileAssociation bool

	// MinSymbolLength filters out short symbols that may be false positives.
	// Symbols shorter than this are ignored.
	MinSymbolLength int

	// MaxSymbolsToCheck limits how many symbol references to check per response.
	// Prevents excessive checking on large responses.
	MaxSymbolsToCheck int

	// IgnoredSymbols contains common type names to ignore (e.g., "Context", "Error").
	// These are too common to validate reliably.
	IgnoredSymbols []string
}

// DefaultPhantomSymbolCheckerConfig returns default phantom symbol checker config.
func DefaultPhantomSymbolCheckerConfig() *PhantomSymbolCheckerConfig {
	return &PhantomSymbolCheckerConfig{
		Enabled:                true,
		RequireFileAssociation: false,
		MinSymbolLength:        3,
		MaxSymbolsToCheck:      100,
		IgnoredSymbols: []string{
			// Common Go types
			"Context", "Error", "String", "Reader", "Writer",
			"Handler", "Request", "Response", "Client", "Server",
			// Standard library types commonly referenced
			"Buffer", "File", "Mutex", "Duration", "Time",
			"Logger", "Marshal", "Unmarshal", "Parse", "Format",
			// Common single-letter or short names
			"T", "S", "R", "W", "ID",
			// Common interface names
			"Stringer", "Closer", "Formatter",
			// Common patterns that cause false positives
			"New", "Get", "Set", "Init", "Close", "Open",
		},
	}
}

// SemanticDriftCheckerConfig configures the semantic drift checker.
type SemanticDriftCheckerConfig struct {
	// Enabled determines if semantic drift checking is active.
	Enabled bool

	// CriticalThreshold is the drift score above which violations are critical.
	// Default 0.7 - response completely off-topic.
	CriticalThreshold float64

	// HighThreshold is the drift score above which violations are high severity.
	// Default 0.5 - significant drift from the question.
	HighThreshold float64

	// WarningThreshold is the drift score above which warnings are generated.
	// Default 0.3 - partial drift detected.
	WarningThreshold float64

	// KeywordWeight is the weight for keyword overlap scoring.
	// Default 0.4 - how much keywords from question appear in response.
	KeywordWeight float64

	// TopicWeight is the weight for topic coherence scoring.
	// Default 0.4 - whether response discusses the right topic.
	TopicWeight float64

	// TypeWeight is the weight for question type matching.
	// Default 0.2 - whether response format matches question type.
	TypeWeight float64

	// MinKeywords is the minimum keywords needed to perform drift check.
	// Questions with fewer keywords are skipped (too ambiguous).
	MinKeywords int

	// MinResponseLength is the minimum response length to check.
	// Very short responses are skipped (can't meaningfully assess).
	MinResponseLength int
}

// DefaultSemanticDriftCheckerConfig returns default semantic drift checker config.
func DefaultSemanticDriftCheckerConfig() *SemanticDriftCheckerConfig {
	return &SemanticDriftCheckerConfig{
		Enabled:           true,
		CriticalThreshold: 0.7,
		HighThreshold:     0.5,
		WarningThreshold:  0.3,
		KeywordWeight:     0.4,
		TopicWeight:       0.4,
		TypeWeight:        0.2,
		MinKeywords:       2,
		MinResponseLength: 20,
	}
}

// AttributeCheckerConfig configures the attribute hallucination checker.
type AttributeCheckerConfig struct {
	// Enabled determines if attribute hallucination checking is active.
	Enabled bool

	// MaxClaimsToCheck limits how many attribute claims to check per response.
	// Prevents excessive checking on large responses.
	MaxClaimsToCheck int

	// IgnorePartialClaims if true, only validates exact count claims.
	// If false, also validates claims like "has fields X and Y".
	IgnorePartialClaims bool

	// MinSymbolLength filters out short symbol names that may be false positives.
	MinSymbolLength int
}

// DefaultAttributeCheckerConfig returns default attribute checker config.
func DefaultAttributeCheckerConfig() *AttributeCheckerConfig {
	return &AttributeCheckerConfig{
		Enabled:             true,
		MaxClaimsToCheck:    50,
		IgnorePartialClaims: false,
		MinSymbolLength:     3,
	}
}

// LineNumberCheckerConfig configures the line number fabrication checker.
type LineNumberCheckerConfig struct {
	// Enabled determines if line number checking is active.
	Enabled bool

	// LineTolerance is the base tolerance for single line citations.
	// A citation is considered valid if within ±LineTolerance of actual line.
	LineTolerance int

	// RangeTolerance is the base tolerance for range citations.
	// Range start/end are each allowed ±RangeTolerance variance.
	RangeTolerance int

	// StrictMode requires exact line matches when true (tolerance = 0).
	StrictMode bool

	// ScaleTolerance adjusts tolerance based on file size.
	// Large files (>500 lines) get 2x tolerance.
	// Small files (<100 lines) get 0.5x tolerance.
	ScaleTolerance bool

	// MaxCitationsToCheck limits how many citations to check per response.
	// Prevents excessive checking on responses with many citations.
	MaxCitationsToCheck int
}

// DefaultLineNumberCheckerConfig returns default line number checker config.
func DefaultLineNumberCheckerConfig() *LineNumberCheckerConfig {
	return &LineNumberCheckerConfig{
		Enabled:             true,
		LineTolerance:       5,
		RangeTolerance:      10,
		StrictMode:          false,
		ScaleTolerance:      true,
		MaxCitationsToCheck: 100,
	}
}

// RelationshipCheckerConfig configures the relationship hallucination checker.
type RelationshipCheckerConfig struct {
	// Enabled determines if relationship checking is active.
	Enabled bool

	// ValidateImports enables import relationship checking.
	// Validates claims like "X imports Y" against actual imports.
	ValidateImports bool

	// ValidateCalls enables function call relationship checking.
	// Validates claims like "A calls B" against call graph.
	ValidateCalls bool

	// ValidateImplements enables interface implementation checking.
	// Validates claims like "T implements I" against type methods.
	// NOTE: Currently disabled by default - requires type resolution.
	ValidateImplements bool

	// MaxRelationshipsToCheck limits how many relationship claims to check.
	// Prevents excessive checking on responses with many claims.
	MaxRelationshipsToCheck int
}

// DefaultRelationshipCheckerConfig returns default relationship checker config.
func DefaultRelationshipCheckerConfig() *RelationshipCheckerConfig {
	return &RelationshipCheckerConfig{
		Enabled:                 true,
		ValidateImports:         true,
		ValidateCalls:           true,
		ValidateImplements:      false, // Requires type resolution, deferred
		MaxRelationshipsToCheck: 50,
	}
}

// BehavioralCheckerConfig configures the behavioral hallucination checker.
type BehavioralCheckerConfig struct {
	// Enabled determines if behavioral hallucination checking is active.
	Enabled bool

	// CheckErrorHandling enables checking claims about error handling.
	// E.g., "logs errors", "returns error to caller", "handles failures gracefully".
	CheckErrorHandling bool

	// CheckValidation enables checking claims about input validation.
	// E.g., "validates input", "sanitizes data", "checks parameters".
	CheckValidation bool

	// CheckSecurity enables checking claims about security behavior.
	// E.g., "encrypts data", "authenticates users", "authorizes access".
	// NOTE: Security claims use RequireCounterEvidence to avoid false positives.
	CheckSecurity bool

	// RequireCounterEvidence if true, only flags claims with explicit counter-evidence.
	// If false, also flags claims with no supporting evidence (more false positives).
	// Default: true (conservative, fewer false positives).
	RequireCounterEvidence bool

	// MaxClaimsToCheck limits how many behavioral claims to check per response.
	// Prevents excessive checking on large responses.
	MaxClaimsToCheck int
}

// DefaultBehavioralCheckerConfig returns default behavioral checker config.
func DefaultBehavioralCheckerConfig() *BehavioralCheckerConfig {
	return &BehavioralCheckerConfig{
		Enabled:                true,
		CheckErrorHandling:     true,
		CheckValidation:        true,
		CheckSecurity:          true,
		RequireCounterEvidence: true, // Conservative - only flag with counter-evidence
		MaxClaimsToCheck:       50,
	}
}

// QuantitativeCheckerConfig configures the quantitative hallucination checker.
type QuantitativeCheckerConfig struct {
	// Enabled determines if quantitative hallucination checking is active.
	Enabled bool

	// CheckFileCounts enables checking claims about file counts.
	// E.g., "15 test files", "3 Go files".
	CheckFileCounts bool

	// CheckLineCounts enables checking claims about line counts.
	// E.g., "main.go is 200 lines", "function has 50 lines".
	CheckLineCounts bool

	// CheckSymbolCounts enables checking claims about symbol counts.
	// E.g., "5 functions in package", "3 methods on type".
	CheckSymbolCounts bool

	// ExactTolerance is the tolerance for exact claims (not using "about").
	// Default 0 means exact claims must match exactly.
	// Value of 1 allows off-by-one errors.
	ExactTolerance int

	// ApproximateUnderPct is tolerance for undercounting with approximate claims.
	// "About 200" for 140 actual = 30% under, allowed by default.
	// Default 0.3 (30% under is acceptable).
	ApproximateUnderPct float64

	// ApproximateOverPct is tolerance for overcounting with approximate claims.
	// "About 200" for 230 actual = 15% over, triggers violation by default.
	// Default 0.15 (15% over triggers violation).
	// Asymmetric because overcounting is worse than undercounting.
	ApproximateOverPct float64

	// MaxClaimsToCheck limits how many quantitative claims to check per response.
	// Prevents excessive checking on large responses.
	MaxClaimsToCheck int
}

// DefaultQuantitativeCheckerConfig returns default quantitative checker config.
func DefaultQuantitativeCheckerConfig() *QuantitativeCheckerConfig {
	return &QuantitativeCheckerConfig{
		Enabled:             true,
		CheckFileCounts:     true,
		CheckLineCounts:     true,
		CheckSymbolCounts:   true,
		ExactTolerance:      0,    // Exact claims must match exactly
		ApproximateUnderPct: 0.3,  // 30% under OK for "about N"
		ApproximateOverPct:  0.15, // 15% over triggers violation
		MaxClaimsToCheck:    20,
	}
}

// NormalizationLevel specifies how code is normalized before comparison.
type NormalizationLevel int

const (
	// NormNone compares code as-is without any normalization.
	NormNone NormalizationLevel = iota

	// NormWhitespace normalizes whitespace only (tabs, spaces, newlines).
	// Default level - preserves comments which may be semantically important.
	NormWhitespace

	// NormFull removes comments and normalizes all whitespace.
	// Use sparingly as comments can be meaningful for doc queries.
	NormFull
)

// CodeSnippetCheckerConfig configures the fabricated code snippet checker.
type CodeSnippetCheckerConfig struct {
	// Enabled determines if code snippet checking is active.
	Enabled bool

	// VerbatimThreshold is the minimum similarity for VERBATIM classification.
	// Code above this threshold is considered an exact or near-exact match.
	// Default 0.9 (90% similar = verbatim).
	VerbatimThreshold float64

	// ModifiedThreshold is the minimum similarity for MODIFIED classification.
	// Code between ModifiedThreshold and VerbatimThreshold is similar but differs.
	// Code below this threshold is classified as FABRICATED.
	// Default 0.5 (50-90% = modified, <50% = fabricated).
	ModifiedThreshold float64

	// MinSnippetLength is the minimum code length to validate.
	// Shorter snippets are skipped (too easy to match by chance).
	// Default 20 characters.
	MinSnippetLength int

	// MaxSnippetLength is the maximum code length to validate.
	// Longer snippets are truncated for performance.
	// Default 5000 characters.
	MaxSnippetLength int

	// MaxCodeBlocksToCheck limits how many code blocks to validate per response.
	// Prevents excessive checking on responses with many code blocks.
	// Default 10.
	MaxCodeBlocksToCheck int

	// NormalizationLevel specifies how to normalize code before comparison.
	// Default NormWhitespace (preserve comments).
	NormalizationLevel NormalizationLevel

	// SuggestionContextLines is how many lines before a code block to check
	// for suggestion phrases (e.g., "you could", "for example").
	// Default 5 lines.
	SuggestionContextLines int

	// CheckInlineCode enables checking inline code (single backticks).
	// Default false - inline code is often too short to validate reliably.
	CheckInlineCode bool
}

// DefaultCodeSnippetCheckerConfig returns default code snippet checker config.
func DefaultCodeSnippetCheckerConfig() *CodeSnippetCheckerConfig {
	return &CodeSnippetCheckerConfig{
		Enabled:                true,
		VerbatimThreshold:      0.9,  // 90% = verbatim
		ModifiedThreshold:      0.5,  // 50% = modified, <50% = fabricated
		MinSnippetLength:       20,   // Skip shorter snippets
		MaxSnippetLength:       5000, // Truncate longer snippets
		MaxCodeBlocksToCheck:   10,   // Limit blocks per response
		NormalizationLevel:     NormWhitespace,
		SuggestionContextLines: 5,
		CheckInlineCode:        false, // Inline code is often too short
	}
}

// APILibraryCheckerConfig configures the API/library hallucination checker.
type APILibraryCheckerConfig struct {
	// Enabled determines if API/library checking is active.
	Enabled bool

	// CheckLibraryExists validates that claimed libraries appear in imports.
	// E.g., "uses gorm" but gorm is not in any import statements.
	CheckLibraryExists bool

	// CheckLibraryConfusion detects similar library mixups.
	// E.g., claims "gorm" but evidence shows "sqlx" is imported.
	CheckLibraryConfusion bool

	// CheckAPIUsageInEvidence validates that claimed API patterns exist.
	// E.g., "gorm.Open()" but this call pattern doesn't appear in code.
	CheckAPIUsageInEvidence bool

	// MaxClaimsToCheck limits how many library claims to check per response.
	// Prevents excessive checking on large responses.
	MaxClaimsToCheck int

	// MinLibraryNameLength filters out short names that may be false positives.
	// Library names shorter than this are ignored.
	MinLibraryNameLength int
}

// DefaultAPILibraryCheckerConfig returns default API library checker config.
func DefaultAPILibraryCheckerConfig() *APILibraryCheckerConfig {
	return &APILibraryCheckerConfig{
		Enabled:                 true,
		CheckLibraryExists:      true,
		CheckLibraryConfusion:   true,
		CheckAPIUsageInEvidence: true,
		MaxClaimsToCheck:        50,
		MinLibraryNameLength:    3,
	}
}

// TemporalCheckerConfig configures the temporal hallucination checker.
type TemporalCheckerConfig struct {
	// Enabled determines if temporal hallucination checking is active.
	Enabled bool

	// StrictMode flags all temporal claims as warnings instead of info.
	// When false (default), temporal claims use SeverityInfo.
	// When true, temporal claims use SeverityWarning.
	StrictMode bool

	// CheckRecencyClaims enables checking "recently", "just", "new" claims.
	CheckRecencyClaims bool

	// CheckHistoricalClaims enables checking "was", "used to", "originally" claims.
	CheckHistoricalClaims bool

	// CheckVersionClaims enables checking "v1.0", "version 2", "since" claims.
	CheckVersionClaims bool

	// CheckReasonClaims enables checking "because", "due to", "in order to" claims.
	// These claims about WHY code was changed are always unverifiable.
	CheckReasonClaims bool

	// MaxClaimsToCheck limits how many temporal claims to check per response.
	// Prevents excessive checking on responses with many temporal references.
	MaxClaimsToCheck int

	// SkipCodeBlocks if true, ignores temporal words inside code blocks.
	// Default true - avoids flagging "new" in "new(Something)" etc.
	SkipCodeBlocks bool
}

// DefaultTemporalCheckerConfig returns default temporal checker config.
func DefaultTemporalCheckerConfig() *TemporalCheckerConfig {
	return &TemporalCheckerConfig{
		Enabled:               true,
		StrictMode:            false, // Use SeverityInfo by default
		CheckRecencyClaims:    true,
		CheckHistoricalClaims: true,
		CheckVersionClaims:    true,
		CheckReasonClaims:     true,
		MaxClaimsToCheck:      20,
		SkipCodeBlocks:        true,
	}
}

// CrossContextCheckerConfig configures the cross-context confusion checker.
type CrossContextCheckerConfig struct {
	// Enabled determines if cross-context confusion checking is active.
	Enabled bool

	// CheckAttributeConfusion enables checking for attributes applied to wrong location.
	// E.g., field from pkg/server/Config attributed to pkg/client/Config.
	CheckAttributeConfusion bool

	// CheckLocationClaims enables checking explicit location claims.
	// E.g., "ProcessData in utils" validated against actual location.
	CheckLocationClaims bool

	// FlagAmbiguousReferences flags references to multi-location symbols without disambiguation.
	// E.g., "Config has..." when Config exists in multiple packages.
	FlagAmbiguousReferences bool

	// AmbiguityThreshold is the minimum number of locations before flagging ambiguity.
	// When a symbol exists in fewer than this many locations, no ambiguity warning.
	// Default 2 - flag when 2+ locations exist.
	AmbiguityThreshold int

	// MaxClaimsToCheck limits how many location claims to check per response.
	// Prevents excessive checking on large responses.
	MaxClaimsToCheck int
}

// DefaultCrossContextCheckerConfig returns default cross-context checker config.
func DefaultCrossContextCheckerConfig() *CrossContextCheckerConfig {
	return &CrossContextCheckerConfig{
		Enabled:                 true,
		CheckAttributeConfusion: true,
		CheckLocationClaims:     true,
		FlagAmbiguousReferences: false, // Disabled by default - can be noisy
		AmbiguityThreshold:      2,
		MaxClaimsToCheck:        30,
	}
}

// ConfidenceCheckerConfig configures the confidence fabrication checker.
type ConfidenceCheckerConfig struct {
	// Enabled determines if confidence fabrication checking is active.
	Enabled bool

	// AbsentEvidenceSeverity is the severity for claims with no supporting evidence.
	// Default SeverityCritical - making absolute claims with no search is very problematic.
	AbsentEvidenceSeverity Severity

	// PartialEvidenceSeverity is the severity for claims with partial/truncated evidence.
	// Default SeverityHigh - absolute claims with limited search should be flagged.
	PartialEvidenceSeverity Severity

	// AllowHedgedAbsolutes skips claims that have hedging language nearby.
	// E.g., "it appears that all..." - the hedge negates the absolute.
	// Default true.
	AllowHedgedAbsolutes bool

	// SkipTautologies skips claims that are definitionally true.
	// E.g., "all .go files have .go extension" - always true.
	// Default true.
	SkipTautologies bool

	// SkipCodeBlocks skips absolute language inside code blocks/quotes.
	// Avoids flagging quoted documentation or code comments.
	// Default true.
	SkipCodeBlocks bool

	// MaxClaimsToCheck limits how many absolute claims to check per response.
	// Prevents excessive checking on responses with many absolute words.
	MaxClaimsToCheck int
}

// DefaultConfidenceCheckerConfig returns default confidence checker config.
func DefaultConfidenceCheckerConfig() *ConfidenceCheckerConfig {
	return &ConfidenceCheckerConfig{
		Enabled:                 true,
		AbsentEvidenceSeverity:  SeverityCritical,
		PartialEvidenceSeverity: SeverityHigh,
		AllowHedgedAbsolutes:    true,
		SkipTautologies:         true,
		SkipCodeBlocks:          true,
		MaxClaimsToCheck:        20,
	}
}
