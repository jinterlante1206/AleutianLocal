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

	// Layer 9: Phantom Package Checker (validates package path references)
	// Detects conformity hallucination where model assumes standard packages exist.
	if config.PhantomPackageCheckerConfig != nil && config.PhantomPackageCheckerConfig.Enabled {
		checkers = append(checkers, NewPhantomPackageChecker(config.PhantomPackageCheckerConfig))
	}

	// Layer 10: Semantic Drift Checker (detects query-response mismatch)
	// CRITICAL: This checker catches responses that don't address the original query.
	// Previously not registered despite having config - fixed in cb_30a.
	if config.SemanticDriftCheckerConfig != nil && config.SemanticDriftCheckerConfig.Enabled {
		checkers = append(checkers, NewSemanticDriftChecker(config.SemanticDriftCheckerConfig))
	}

	// Layer 11: Structural Claim Checker (validates structural claims about codebase)
	if config.StructuralClaimCheckerConfig != nil && config.StructuralClaimCheckerConfig.Enabled {
		checkers = append(checkers, NewStructuralClaimChecker(config.StructuralClaimCheckerConfig))
	}

	// Layer 12: Phantom File Checker (detects references to non-existent files)
	if config.PhantomCheckerConfig != nil && config.PhantomCheckerConfig.Enabled {
		checkers = append(checkers, NewPhantomFileChecker(config.PhantomCheckerConfig))
	}

	// Layer 13: Phantom Symbol Checker (detects references to non-existent symbols)
	if config.PhantomSymbolCheckerConfig != nil && config.PhantomSymbolCheckerConfig.Enabled {
		checkers = append(checkers, NewPhantomSymbolChecker(config.PhantomSymbolCheckerConfig))
	}

	// Layer 14: Attribute Checker (validates attribute claims about symbols)
	if config.AttributeCheckerConfig != nil && config.AttributeCheckerConfig.Enabled {
		checkers = append(checkers, NewAttributeChecker(config.AttributeCheckerConfig))
	}

	// Layer 15: Line Number Checker (validates line number citations)
	if config.LineNumberCheckerConfig != nil && config.LineNumberCheckerConfig.Enabled {
		checkers = append(checkers, NewLineNumberChecker(config.LineNumberCheckerConfig))
	}

	// Layer 16: Relationship Checker (validates import/call relationship claims)
	if config.RelationshipCheckerConfig != nil && config.RelationshipCheckerConfig.Enabled {
		checkers = append(checkers, NewRelationshipChecker(config.RelationshipCheckerConfig))
	}

	// Layer 17: Behavioral Checker (validates behavioral claims about code)
	if config.BehavioralCheckerConfig != nil && config.BehavioralCheckerConfig.Enabled {
		checkers = append(checkers, NewBehavioralChecker(config.BehavioralCheckerConfig))
	}

	// Layer 18: Quantitative Checker (validates numeric claims)
	if config.QuantitativeCheckerConfig != nil && config.QuantitativeCheckerConfig.Enabled {
		checkers = append(checkers, NewQuantitativeChecker(config.QuantitativeCheckerConfig))
	}

	// Layer 19: Code Snippet Checker (validates code snippets match actual code)
	if config.CodeSnippetCheckerConfig != nil && config.CodeSnippetCheckerConfig.Enabled {
		checkers = append(checkers, NewCodeSnippetChecker(config.CodeSnippetCheckerConfig))
	}

	// Layer 20: API Library Checker (validates API/library usage claims)
	if config.APILibraryCheckerConfig != nil && config.APILibraryCheckerConfig.Enabled {
		checkers = append(checkers, NewAPILibraryChecker(config.APILibraryCheckerConfig))
	}

	// Layer 21: Temporal Checker (flags unverifiable temporal claims)
	if config.TemporalCheckerConfig != nil && config.TemporalCheckerConfig.Enabled {
		checkers = append(checkers, NewTemporalChecker(config.TemporalCheckerConfig))
	}

	// Layer 22: Cross Context Checker (detects context confusion)
	if config.CrossContextCheckerConfig != nil && config.CrossContextCheckerConfig.Enabled {
		checkers = append(checkers, NewCrossContextChecker(config.CrossContextCheckerConfig))
	}

	// Layer 23: Confidence Checker (detects overconfident claims without evidence)
	if config.ConfidenceCheckerConfig != nil && config.ConfidenceCheckerConfig.Enabled {
		checkers = append(checkers, NewConfidenceChecker(config.ConfidenceCheckerConfig))
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
