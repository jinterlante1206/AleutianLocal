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

// ChainOfVerificationConfig configures the chain-of-verification verifier.
type ChainOfVerificationConfig struct {
	// Enabled determines if chain-of-verification is active.
	Enabled bool

	// VerifyThreshold is the minimum confidence to keep a claim.
	// Values: "HIGH", "MEDIUM", "LOW"
	VerifyThreshold string

	// MaxClaimsToVerify limits how many claims to verify per response.
	MaxClaimsToVerify int
}

// DefaultChainOfVerificationConfig returns sensible defaults.
func DefaultChainOfVerificationConfig() *ChainOfVerificationConfig {
	return &ChainOfVerificationConfig{
		Enabled:           false, // Opt-in due to extra LLM call cost
		VerifyThreshold:   "MEDIUM",
		MaxClaimsToVerify: 10,
	}
}

// ChainOfVerification performs self-verification on LLM responses.
//
// This is Layer 8 of the anti-hallucination defense system. After the LLM
// generates a response, it asks the LLM to verify its own claims against
// the context it was shown.
//
// The verification step helps catch claims the LLM "knows" it made up,
// since LLMs are often better at verification than generation.
//
// Thread Safety: Safe for concurrent use after construction.
type ChainOfVerification struct {
	config *ChainOfVerificationConfig
}

// NewChainOfVerification creates a new chain-of-verification verifier.
//
// Inputs:
//
//	config - Configuration for the verifier. If nil, defaults are used.
//
// Outputs:
//
//	*ChainOfVerification - The configured verifier.
func NewChainOfVerification(config *ChainOfVerificationConfig) *ChainOfVerification {
	if config == nil {
		config = DefaultChainOfVerificationConfig()
	}
	return &ChainOfVerification{config: config}
}

// Name returns the verifier name for logging and metrics.
func (v *ChainOfVerification) Name() string {
	return "chain_of_verification"
}

// VerificationRequest contains the data needed for verification.
type VerificationRequest struct {
	// Claims are the claims extracted from the response.
	Claims []ExtractedClaim

	// OriginalResponse is the LLM response being verified.
	OriginalResponse string

	// Evidence is the evidence index from context.
	Evidence *EvidenceIndex
}

// ExtractedClaim represents a claim extracted for verification.
type ExtractedClaim struct {
	// ID is a unique identifier for this claim.
	ID int

	// Statement is the text of the claim.
	Statement string

	// File is the file referenced (if any).
	File string

	// Line is the line number referenced (if any).
	Line int

	// RawText is the original text containing this claim.
	RawText string
}

// VerificationResponse is the expected response from the verification LLM call.
type VerificationResponse struct {
	// Verifications contains the result for each claim.
	Verifications []ClaimVerification `json:"verifications"`
}

// ClaimVerification contains the verification result for a single claim.
type ClaimVerification struct {
	// ClaimID is the ID of the claim being verified.
	ClaimID int `json:"claim_id"`

	// Verified is true if the LLM could verify the claim.
	Verified bool `json:"verified"`

	// Confidence is HIGH, MEDIUM, LOW, or CANNOT_VERIFY.
	Confidence string `json:"confidence"`

	// Citation is the file:line reference (if verified).
	Citation string `json:"citation,omitempty"`

	// Reason explains why the claim couldn't be verified.
	Reason string `json:"reason,omitempty"`
}

// VerificationResult contains the outcome of chain-of-verification.
type VerificationResult struct {
	// AllVerified is true if all claims were verified.
	AllVerified bool

	// VerifiedClaims are claims that passed verification.
	VerifiedClaims []VerifiedClaimResult

	// UnverifiedClaims are claims that failed verification.
	UnverifiedClaims []VerifiedClaimResult

	// RawResponse is the raw verification response for debugging.
	RawResponse string
}

// VerifiedClaimResult contains verification details for a claim.
type VerifiedClaimResult struct {
	// ClaimID is the ID of the claim.
	ClaimID int

	// Statement is the claim text.
	Statement string

	// Verified is true if verified.
	Verified bool

	// Confidence is the confidence level.
	Confidence string

	// Citation is the supporting citation.
	Citation string

	// Reason explains lack of verification.
	Reason string
}

// Check implements Checker interface for integration with grounding pipeline.
//
// Note: This checker requires an LLM client to perform verification.
// If no LLM client is configured, it returns nil (no violations).
// The LLM call is handled externally via BuildVerificationPrompt and
// ParseVerificationResponse.
//
// Thread Safety: Safe for concurrent use.
func (v *ChainOfVerification) Check(ctx context.Context, input *CheckInput) []Violation {
	if !v.config.Enabled || input == nil || input.Response == "" {
		return nil
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return nil
	default:
	}

	// This checker doesn't call LLM directly - it provides tools for the
	// grounding pipeline to use. Return nil to indicate no violations found
	// at this stage. The actual verification happens in VerifyWithPrompt.
	return nil
}

// BuildVerificationPrompt creates the prompt to ask the LLM to verify claims.
//
// Inputs:
//
//	claims - The claims extracted from the response.
//
// Outputs:
//
//	string - The verification prompt to send to the LLM.
func (v *ChainOfVerification) BuildVerificationPrompt(claims []ExtractedClaim) string {
	var builder strings.Builder

	builder.WriteString("Please verify the following claims you made:\n\n")

	claimsToVerify := claims
	if len(claimsToVerify) > v.config.MaxClaimsToVerify {
		claimsToVerify = claimsToVerify[:v.config.MaxClaimsToVerify]
	}

	for _, claim := range claimsToVerify {
		builder.WriteString(fmt.Sprintf("%d. %s\n", claim.ID, claim.Statement))
		if claim.File != "" {
			if claim.Line > 0 {
				builder.WriteString(fmt.Sprintf("   (Claimed from: %s:%d)\n", claim.File, claim.Line))
			} else {
				builder.WriteString(fmt.Sprintf("   (Claimed from: %s)\n", claim.File))
			}
		}
	}

	builder.WriteString("\nFor each claim:\n")
	builder.WriteString("- Can you cite the EXACT file and line from context?\n")
	builder.WriteString("- Did you actually see this in the provided code?\n")
	builder.WriteString("- Rate confidence: HIGH / MEDIUM / LOW / CANNOT_VERIFY\n")
	builder.WriteString("\nBe honest. If you cannot verify a claim, say so.\n")
	builder.WriteString("\nRespond in JSON format:\n")
	builder.WriteString("{\n")
	builder.WriteString("  \"verifications\": [\n")
	builder.WriteString("    {\"claim_id\": 1, \"verified\": true, \"confidence\": \"HIGH\", \"citation\": \"file.go:45\"},\n")
	builder.WriteString("    {\"claim_id\": 2, \"verified\": false, \"confidence\": \"CANNOT_VERIFY\", \"reason\": \"why\"}\n")
	builder.WriteString("  ]\n")
	builder.WriteString("}\n")

	return builder.String()
}

// VerificationSystemPrompt returns the system prompt for verification calls.
func (v *ChainOfVerification) VerificationSystemPrompt() string {
	return `You are a claim verifier. For each claim, determine if it can be verified from the context you were shown. Be honest - if you cannot find evidence for a claim, say CANNOT_VERIFY.

Your job is to critically evaluate whether each claim is actually supported by the code context. Do not accept claims just because they sound plausible.

Respond ONLY in JSON format:
{
  "verifications": [
    {"claim_id": 1, "verified": true, "confidence": "HIGH", "citation": "file:line"},
    {"claim_id": 2, "verified": false, "confidence": "CANNOT_VERIFY", "reason": "why"}
  ]
}

Confidence levels:
- HIGH: You clearly saw this in the context with exact evidence
- MEDIUM: You saw related content that supports this claim
- LOW: You're not sure but it seems plausible
- CANNOT_VERIFY: You cannot find evidence for this claim`
}

// ParseVerificationResponse parses the LLM's verification response.
//
// Inputs:
//
//	response - The raw LLM response text.
//	originalClaims - The original claims being verified.
//
// Outputs:
//
//	*VerificationResult - The parsed verification result.
//	error - Non-nil if parsing fails.
func (v *ChainOfVerification) ParseVerificationResponse(
	response string,
	originalClaims []ExtractedClaim,
) (*VerificationResult, error) {
	// Try to parse the response
	var verifyResp VerificationResponse

	// Try direct parse
	if err := json.Unmarshal([]byte(response), &verifyResp); err != nil {
		// Try to extract JSON from response
		cleaned := extractJSONBlock(response)
		if cleaned == "" {
			return nil, fmt.Errorf("unable to parse verification response as JSON")
		}
		if err := json.Unmarshal([]byte(cleaned), &verifyResp); err != nil {
			return nil, fmt.Errorf("unable to parse extracted JSON: %w", err)
		}
	}

	// Build result
	result := &VerificationResult{
		AllVerified: true,
		RawResponse: response,
	}

	// Create claim lookup
	claimByID := make(map[int]ExtractedClaim)
	for _, claim := range originalClaims {
		claimByID[claim.ID] = claim
	}

	// Process verifications
	for _, ver := range verifyResp.Verifications {
		claim, ok := claimByID[ver.ClaimID]
		if !ok {
			continue
		}

		claimResult := VerifiedClaimResult{
			ClaimID:    ver.ClaimID,
			Statement:  claim.Statement,
			Verified:   ver.Verified,
			Confidence: ver.Confidence,
			Citation:   ver.Citation,
			Reason:     ver.Reason,
		}

		if v.meetsThreshold(ver) {
			result.VerifiedClaims = append(result.VerifiedClaims, claimResult)
		} else {
			result.AllVerified = false
			result.UnverifiedClaims = append(result.UnverifiedClaims, claimResult)
		}
	}

	return result, nil
}

// meetsThreshold checks if a verification meets the configured threshold.
func (v *ChainOfVerification) meetsThreshold(ver ClaimVerification) bool {
	if !ver.Verified {
		return false
	}

	threshold := strings.ToUpper(v.config.VerifyThreshold)
	confidence := strings.ToUpper(ver.Confidence)

	switch threshold {
	case "HIGH":
		return confidence == "HIGH"
	case "MEDIUM":
		return confidence == "HIGH" || confidence == "MEDIUM"
	case "LOW":
		return confidence == "HIGH" || confidence == "MEDIUM" || confidence == "LOW"
	default:
		// Unknown threshold, be permissive
		return ver.Verified
	}
}

// ConvertToViolations converts unverified claims to grounding violations.
//
// Inputs:
//
//	result - The verification result.
//
// Outputs:
//
//	[]Violation - Violations for unverified claims.
func (v *ChainOfVerification) ConvertToViolations(result *VerificationResult) []Violation {
	if result == nil {
		return nil
	}

	var violations []Violation

	for _, claim := range result.UnverifiedClaims {
		severity := SeverityWarning
		if claim.Confidence == "CANNOT_VERIFY" {
			severity = SeverityCritical
		}

		message := fmt.Sprintf("Claim could not be verified: %s", claim.Statement)
		if claim.Reason != "" {
			message = fmt.Sprintf("%s (reason: %s)", message, claim.Reason)
		}

		violations = append(violations, Violation{
			Type:     ViolationUngrounded,
			Severity: severity,
			Code:     "COV_UNVERIFIED_CLAIM",
			Message:  message,
			Evidence: claim.Statement,
		})
	}

	return violations
}

// ExtractClaimsForVerification extracts claims from a response for verification.
//
// This uses the same extraction logic as GroundingChecker but formats
// claims for the verification prompt.
//
// Inputs:
//
//	response - The LLM response text.
//
// Outputs:
//
//	[]ExtractedClaim - The extracted claims.
func ExtractClaimsForVerification(response string) []ExtractedClaim {
	// Use GroundingChecker for extraction
	checker := NewGroundingChecker(nil)
	claims := checker.extractClaims(response)

	var extracted []ExtractedClaim
	for i, claim := range claims {
		extracted = append(extracted, ExtractedClaim{
			ID:        i + 1,
			Statement: claim.RawText,
			File:      claim.Value,
			RawText:   claim.RawText,
		})
	}

	return extracted
}

// Note: extractJSONBlock is defined in structured_output.go as a shared utility.
