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

// NewGrounder creates a fully configured Grounder with all detection checkers.
//
// Inputs:
//
//	config - Configuration for grounding behavior. Nil uses defaults.
//
// Outputs:
//
//	Grounder - A configured grounder ready for use.
//
// Example:
//
//	grounder := grounding.NewGrounder(nil) // use defaults
//	result, err := grounder.Validate(ctx, response, assembledCtx)
//	if grounder.ShouldReject(result) {
//	    return ErrHallucination
//	}
func NewGrounder(config *Config) Grounder {
	if config == nil {
		defaultConfig := DefaultConfig()
		config = &defaultConfig
	}

	// Create checkers based on configuration
	var checkers []Checker

	// Layer 2: Structured Output Checker (validates JSON responses with evidence)
	if config.StructuredOutputConfig != nil && config.StructuredOutputConfig.Enabled {
		checkers = append(checkers, NewStructuredOutputChecker(config.StructuredOutputConfig))
	}

	// Layer 3: Citation Checker
	checkers = append(checkers, NewCitationChecker(config.CitationCheckerConfig))

	// Layer 4: Grounding Checker (claim extraction and validation)
	checkers = append(checkers, NewGroundingChecker(config.GroundingCheckerConfig))

	// Layer 5: Language Checker
	checkers = append(checkers, NewLanguageChecker(config.LanguageCheckerConfig))

	// Layer 6: TMS Verifier (uses TMS propagation for claim verification)
	if config.TMSVerifierConfig != nil && config.TMSVerifierConfig.Enabled {
		checkers = append(checkers, NewTMSVerifier(config.TMSVerifierConfig))
	}

	// Layer 7: Multi-Sample Verifier (consistency across multiple samples)
	// Note: This is opt-in due to cost (N LLM calls). The actual multi-sample
	// generation is done by the consumer; this checker provides analysis tools.
	if config.MultiSampleConfig != nil && config.MultiSampleConfig.Enabled {
		checkers = append(checkers, NewMultiSampleVerifier(config.MultiSampleConfig))
	}

	// Layer 8: Chain-of-Verification (self-verification step)
	// Note: This checker is opt-in and requires external LLM client integration.
	// The actual verification prompt/response handling is done by the consumer.
	if config.ChainOfVerificationConfig != nil && config.ChainOfVerificationConfig.Enabled {
		checkers = append(checkers, NewChainOfVerification(config.ChainOfVerificationConfig))
	}

	return NewDefaultGrounder(*config, checkers...)
}

// NewGrounderWithCheckers creates a Grounder with custom checkers.
//
// Use this when you need to add custom checkers or want fine-grained
// control over which checkers are used.
//
// Inputs:
//
//	config - Configuration for grounding behavior.
//	checkers - The checkers to use.
//
// Outputs:
//
//	Grounder - A configured grounder.
func NewGrounderWithCheckers(config Config, checkers ...Checker) Grounder {
	return NewDefaultGrounder(config, checkers...)
}
