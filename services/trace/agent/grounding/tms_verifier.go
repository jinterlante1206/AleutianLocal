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
	"fmt"
	"log/slog"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/algorithms/constraints"
)

// TMSVerifierConfig configures the TMS verifier.
type TMSVerifierConfig struct {
	// Enabled determines if TMS verification is enabled.
	Enabled bool

	// MaxIterations limits TMS propagation iterations.
	MaxIterations int
}

// DefaultTMSVerifierConfig returns sensible defaults.
func DefaultTMSVerifierConfig() *TMSVerifierConfig {
	return &TMSVerifierConfig{
		Enabled:       true,
		MaxIterations: 100,
	}
}

// TMSVerifier uses TMS to verify claims against evidence.
//
// This is Layer 6 of the anti-hallucination defense system. It models
// claims and evidence as TMS beliefs, then uses propagation to determine
// which claims are supported by evidence.
//
// Key concepts:
//   - Evidence (files, symbols) are registered as base beliefs (always IN)
//   - Claims are registered as beliefs that depend on evidence
//   - TMS propagation determines if claims have supporting evidence
//   - Unsupported claims (status OUT) indicate potential hallucination
//
// Thread Safety: Safe for concurrent use after construction.
type TMSVerifier struct {
	config         *TMSVerifierConfig
	tms            *constraints.TMS
	claimExtractor *GroundingChecker // Cached for reuse
}

// NewTMSVerifier creates a new TMS-based claim verifier.
//
// Inputs:
//
//	config - Configuration for the verifier. If nil, defaults are used.
//
// Outputs:
//
//	*TMSVerifier - The configured verifier.
func NewTMSVerifier(config *TMSVerifierConfig) *TMSVerifier {
	if config == nil {
		config = DefaultTMSVerifierConfig()
	}

	tmsConfig := constraints.DefaultTMSConfig()
	tmsConfig.MaxIterations = config.MaxIterations

	return &TMSVerifier{
		config:         config,
		tms:            constraints.NewTMS(tmsConfig),
		claimExtractor: NewGroundingChecker(nil), // Cache for reuse
	}
}

// TMSVerificationResult contains the outcome of TMS-based verification.
type TMSVerificationResult struct {
	// SupportedClaims are claims with evidence support (status IN).
	SupportedClaims []VerifiedClaim

	// UnsupportedClaims are claims without evidence support (status OUT).
	UnsupportedClaims []VerifiedClaim

	// Contradictions are logical contradictions detected by TMS.
	Contradictions []string

	// PropagationIterations is how many TMS iterations were run.
	PropagationIterations int

	// TotalClaims is the total number of claims verified.
	TotalClaims int
}

// VerifiedClaim contains the verification result for a single claim.
type VerifiedClaim struct {
	// Index is the claim's position in the original list.
	Index int

	// Claim is the original claim.
	Claim Claim

	// Supported indicates if the claim is supported by evidence.
	Supported bool

	// Reason explains why the claim is or isn't supported.
	Reason string

	// MissingEvidence lists evidence that would be needed to support this claim.
	MissingEvidence []string
}

// VerifyClaims uses TMS to verify which claims are supported by evidence.
//
// Description:
//
//	Registers evidence as base beliefs (always IN), registers claims as
//	dependent beliefs, runs TMS propagation, and collects results.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	claims - The claims to verify.
//	evidence - The evidence index from context.
//
// Outputs:
//
//	*TMSVerificationResult - The verification result.
//	error - Non-nil if verification fails.
//
// Thread Safety: Safe for concurrent use.
func (v *TMSVerifier) VerifyClaims(ctx context.Context, claims []Claim, evidence *EvidenceIndex) (*TMSVerificationResult, error) {
	if !v.config.Enabled {
		return nil, nil
	}

	if evidence == nil {
		evidence = NewEvidenceIndex()
	}

	// Build TMS input
	input := &constraints.TMSInput{
		Beliefs:        make(map[string]constraints.TMSBelief),
		Justifications: make(map[string][]constraints.TMSJustification),
		Changes:        make([]constraints.TMSChange, 0),
	}

	// Register evidence as base beliefs (always IN)
	v.registerEvidence(input, evidence)

	// Register claims as dependent beliefs
	claimIDs := v.registerClaims(input, claims, evidence)

	// Run TMS propagation
	output, _, err := v.tms.Process(ctx, nil, input)
	if err != nil {
		return nil, fmt.Errorf("TMS propagation failed: %w", err)
	}

	// Parse TMS output
	tmsOutput, ok := output.(*constraints.TMSOutput)
	if !ok {
		return nil, fmt.Errorf("unexpected TMS output type: %T", output)
	}

	// Collect results
	result := v.collectResults(claims, claimIDs, input.Beliefs, tmsOutput, evidence)

	return result, nil
}

// registerEvidence registers evidence files and symbols as base beliefs.
func (v *TMSVerifier) registerEvidence(input *constraints.TMSInput, evidence *EvidenceIndex) {
	// Register files as evidence beliefs
	for file := range evidence.Files {
		beliefID := "evidence_file_" + sanitizeID(file)
		input.Beliefs[beliefID] = constraints.TMSBelief{
			NodeID: beliefID,
			Status: constraints.TMSStatusIn, // Evidence is always IN
		}
	}

	// Register file basenames
	for basename := range evidence.FileBasenames {
		beliefID := "evidence_basename_" + sanitizeID(basename)
		input.Beliefs[beliefID] = constraints.TMSBelief{
			NodeID: beliefID,
			Status: constraints.TMSStatusIn,
		}
	}

	// Register symbols as evidence beliefs
	for symbol := range evidence.Symbols {
		beliefID := "evidence_symbol_" + sanitizeID(symbol)
		input.Beliefs[beliefID] = constraints.TMSBelief{
			NodeID: beliefID,
			Status: constraints.TMSStatusIn,
		}
	}

	// Register frameworks as evidence beliefs
	for framework := range evidence.Frameworks {
		beliefID := "evidence_framework_" + sanitizeID(framework)
		input.Beliefs[beliefID] = constraints.TMSBelief{
			NodeID: beliefID,
			Status: constraints.TMSStatusIn,
		}
	}
}

// registerClaims registers claims as beliefs with justifications.
func (v *TMSVerifier) registerClaims(input *constraints.TMSInput, claims []Claim, evidence *EvidenceIndex) []string {
	claimIDs := make([]string, len(claims))

	for i, claim := range claims {
		claimID := fmt.Sprintf("claim_%d", i)
		claimIDs[i] = claimID

		// Claim starts as OUT (not believed until justified)
		input.Beliefs[claimID] = constraints.TMSBelief{
			NodeID: claimID,
			Status: constraints.TMSStatusOut,
		}

		// Create justification based on claim type
		justification := v.createJustification(claimID, claim, evidence)
		input.Justifications[claimID] = []constraints.TMSJustification{justification}

		// If justification is supported, trigger change to IN
		if v.isJustificationSupported(justification, input.Beliefs) {
			input.Changes = append(input.Changes, constraints.TMSChange{
				NodeID:    claimID,
				NewStatus: constraints.TMSStatusIn,
				Reason:    "evidence supports claim",
			})
		}
	}

	return claimIDs
}

// createJustification creates a TMS justification for a claim.
func (v *TMSVerifier) createJustification(claimID string, claim Claim, evidence *EvidenceIndex) constraints.TMSJustification {
	justification := constraints.TMSJustification{
		ID:      claimID + "_just",
		InList:  make([]string, 0),
		OutList: make([]string, 0),
	}

	switch claim.Type {
	case ClaimFile:
		// File claim requires file evidence
		filePath := claim.Value
		basename := getBasename(filePath)

		// Check if full path or basename exists in evidence
		fullPathID := "evidence_file_" + sanitizeID(filePath)
		basenameID := "evidence_basename_" + sanitizeID(basename)

		// Require either full path or basename
		if evidence.Files[filePath] || evidence.Files[normalizePath(filePath)] {
			justification.InList = append(justification.InList, fullPathID)
		} else if evidence.FileBasenames[basename] {
			justification.InList = append(justification.InList, basenameID)
		} else {
			// No evidence - add impossible requirement
			justification.InList = append(justification.InList, "evidence_file_"+sanitizeID(filePath))
		}

	case ClaimSymbol:
		// Symbol claim requires symbol evidence
		symbolID := "evidence_symbol_" + sanitizeID(claim.Value)

		// Check case-insensitive
		found := false
		for symbol := range evidence.Symbols {
			if strings.EqualFold(symbol, claim.Value) {
				justification.InList = append(justification.InList, "evidence_symbol_"+sanitizeID(symbol))
				found = true
				break
			}
		}
		if !found {
			justification.InList = append(justification.InList, symbolID)
		}

	case ClaimFramework:
		// Framework claim requires framework evidence
		framework := strings.ToLower(claim.Value)
		frameworkID := "evidence_framework_" + sanitizeID(framework)
		justification.InList = append(justification.InList, frameworkID)

	case ClaimLanguage:
		// Language claims don't use TMS - handled by LanguageChecker
		// For TMS purposes, we'll mark as unconditionally supported
		// (TMS focuses on file/symbol/framework claims)
	}

	return justification
}

// isJustificationSupported checks if a justification is currently supported.
func (v *TMSVerifier) isJustificationSupported(just constraints.TMSJustification, beliefs map[string]constraints.TMSBelief) bool {
	// All InList nodes must be IN
	for _, nodeID := range just.InList {
		if belief, ok := beliefs[nodeID]; !ok || belief.Status != constraints.TMSStatusIn {
			return false
		}
	}

	// All OutList nodes must be OUT
	for _, nodeID := range just.OutList {
		if belief, ok := beliefs[nodeID]; ok && belief.Status == constraints.TMSStatusIn {
			return false
		}
	}

	return true
}

// collectResults aggregates TMS output into verification results.
func (v *TMSVerifier) collectResults(claims []Claim, claimIDs []string, beliefs map[string]constraints.TMSBelief, tmsOutput *constraints.TMSOutput, evidence *EvidenceIndex) *TMSVerificationResult {
	result := &TMSVerificationResult{
		SupportedClaims:       make([]VerifiedClaim, 0),
		UnsupportedClaims:     make([]VerifiedClaim, 0),
		PropagationIterations: tmsOutput.Iterations,
		TotalClaims:           len(claims),
	}

	// Collect contradictions
	for _, c := range tmsOutput.Contradictions {
		result.Contradictions = append(result.Contradictions, c.Reason)
	}

	// Determine final status of each claim
	for i, claim := range claims {
		claimID := claimIDs[i]
		belief, exists := beliefs[claimID]

		verified := VerifiedClaim{
			Index: i,
			Claim: claim,
		}

		// Check if belief is IN (supported)
		if exists && belief.Status == constraints.TMSStatusIn {
			verified.Supported = true
			verified.Reason = "Evidence supports this claim"
			result.SupportedClaims = append(result.SupportedClaims, verified)
		} else {
			// Check updated beliefs from propagation
			isSupported := false
			for _, updated := range tmsOutput.UpdatedBeliefs {
				if updated.NodeID == claimID && updated.Status == constraints.TMSStatusIn {
					isSupported = true
					break
				}
			}

			if isSupported {
				verified.Supported = true
				verified.Reason = "Evidence supports this claim (via propagation)"
				result.SupportedClaims = append(result.SupportedClaims, verified)
			} else {
				verified.Supported = false
				verified.Reason = v.explainMissingEvidence(claim, evidence)
				verified.MissingEvidence = v.getMissingEvidence(claim, evidence)
				result.UnsupportedClaims = append(result.UnsupportedClaims, verified)
			}
		}
	}

	return result
}

// explainMissingEvidence creates a human-readable explanation.
func (v *TMSVerifier) explainMissingEvidence(claim Claim, evidence *EvidenceIndex) string {
	switch claim.Type {
	case ClaimFile:
		return fmt.Sprintf("File '%s' not found in evidence", claim.Value)
	case ClaimSymbol:
		return fmt.Sprintf("Symbol '%s' not found in evidence", claim.Value)
	case ClaimFramework:
		return fmt.Sprintf("Framework '%s' not mentioned in evidence", claim.Value)
	default:
		return "Claim not supported by evidence"
	}
}

// getMissingEvidence lists what evidence would be needed.
func (v *TMSVerifier) getMissingEvidence(claim Claim, evidence *EvidenceIndex) []string {
	switch claim.Type {
	case ClaimFile:
		return []string{claim.Value}
	case ClaimSymbol:
		return []string{claim.Value}
	case ClaimFramework:
		return []string{claim.Value}
	default:
		return nil
	}
}

// sanitizeID creates a safe ID from a string.
func sanitizeID(s string) string {
	// Replace non-alphanumeric characters with underscores
	var result strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			result.WriteRune(c)
		} else {
			result.WriteRune('_')
		}
	}
	return result.String()
}

// getBasename extracts the base filename from a path.
func getBasename(path string) string {
	// Find last slash
	lastSlash := strings.LastIndexAny(path, "/\\")
	if lastSlash >= 0 {
		return path[lastSlash+1:]
	}
	return path
}

// Name returns the verifier name for logging and metrics.
func (v *TMSVerifier) Name() string {
	return "tms_verifier"
}

// Check implements Checker interface for integration with grounding pipeline.
//
// Description:
//
//	Extracts claims from the response (using GroundingChecker's patterns),
//	verifies them using TMS, and returns violations for unsupported claims.
//
// Thread Safety: Safe for concurrent use.
func (v *TMSVerifier) Check(ctx context.Context, input *CheckInput) []Violation {
	if !v.config.Enabled || input == nil || input.Response == "" {
		return nil
	}

	// Extract claims using cached GroundingChecker
	claims := v.claimExtractor.extractClaims(input.Response)

	if len(claims) == 0 {
		return nil
	}

	// Get or build evidence index
	evidence := input.EvidenceIndex
	if evidence == nil {
		evidence = NewEvidenceIndex()
	}

	// Verify claims using TMS
	result, err := v.VerifyClaims(ctx, claims, evidence)
	if err != nil {
		// Log error for debugging but don't fail the check
		slog.Warn("TMS verification failed",
			slog.String("error", err.Error()),
			slog.Int("claims_count", len(claims)),
		)
		return nil
	}

	if result == nil {
		return nil
	}

	// Convert unsupported claims to violations
	var violations []Violation
	for _, uc := range result.UnsupportedClaims {
		// Skip if already caught by GroundingChecker
		// TMS provides additional validation, not duplication
		severity := SeverityWarning
		if uc.Claim.Type == ClaimFramework {
			severity = SeverityCritical
		}

		violations = append(violations, Violation{
			Type:     ViolationUngrounded,
			Severity: severity,
			Code:     "TMS_UNSUPPORTED_CLAIM",
			Message:  uc.Reason,
			Evidence: uc.Claim.RawText,
		})
	}

	return violations
}
