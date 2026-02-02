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
}

// DefaultConfig returns sensible defaults for grounding configuration.
//
// Outputs:
//
//	Config - The default configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:                   true,
		RejectOnCritical:          true,
		AddFootnoteOnWarning:      true,
		MaxViolationsBeforeReject: 3,
		MinConfidence:             0.5,
		MaxResponseScanLength:     10000,
		Timeout:                   5 * time.Second,
		ShortCircuitOnCritical:    false,
		MaxHallucinationRetries:   3,
		LanguageCheckerConfig:     DefaultLanguageCheckerConfig(),
		CitationCheckerConfig:     DefaultCitationCheckerConfig(),
		GroundingCheckerConfig:    DefaultGroundingCheckerConfig(),
		TMSVerifierConfig:         DefaultTMSVerifierConfig(),
		StructuredOutputConfig:    DefaultStructuredOutputConfig(),
		ChainOfVerificationConfig: DefaultChainOfVerificationConfig(),
		MultiSampleConfig:         DefaultMultiSampleConfig(),
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
