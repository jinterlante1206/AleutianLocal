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
	"regexp"
	"strings"
	"sync"
)

// MultiSampleConfig configures the multi-sample consistency verifier.
type MultiSampleConfig struct {
	// Enabled determines if multi-sample verification is active.
	Enabled bool

	// NumSamples is the number of samples to generate (default: 3).
	NumSamples int

	// Temperature is the sampling temperature (default: 0.7).
	Temperature float64

	// ConsensusThreshold is the minimum samples for consensus (default: 2).
	ConsensusThreshold int
}

// DefaultMultiSampleConfig returns sensible defaults.
func DefaultMultiSampleConfig() *MultiSampleConfig {
	return &MultiSampleConfig{
		Enabled:            false, // Opt-in due to cost (N LLM calls)
		NumSamples:         3,
		Temperature:        0.7,
		ConsensusThreshold: 2,
	}
}

// MultiSampleVerifier generates multiple responses and finds consensus.
//
// This is Layer 7 of the anti-hallucination defense system. It generates
// N responses at non-zero temperature and only keeps claims that appear
// consistently across samples. Inconsistent claims indicate hallucination.
//
// Key principle: Hallucinations are inconsistent across samples.
// True facts tend to appear in multiple samples.
//
// Cost: N LLM calls per verification (use sparingly for high-stakes questions)
//
// Thread Safety: Safe for concurrent use after construction.
type MultiSampleVerifier struct {
	config *MultiSampleConfig
}

// NewMultiSampleVerifier creates a new multi-sample verifier.
//
// Inputs:
//
//	config - Configuration for the verifier. If nil, defaults are used.
//
// Outputs:
//
//	*MultiSampleVerifier - The configured verifier.
func NewMultiSampleVerifier(config *MultiSampleConfig) *MultiSampleVerifier {
	if config == nil {
		config = DefaultMultiSampleConfig()
	}
	return &MultiSampleVerifier{config: config}
}

// Name returns the verifier name for logging and metrics.
func (v *MultiSampleVerifier) Name() string {
	return "multi_sample"
}

// SampleResponse represents a single LLM response sample.
type SampleResponse struct {
	// Index is the sample number (0-based).
	Index int

	// Content is the response text.
	Content string

	// Claims are the extracted claims from this sample.
	Claims []NormalizedClaim
}

// NormalizedClaim is a claim normalized for comparison.
type NormalizedClaim struct {
	// Original is the original claim text.
	Original string

	// Normalized is the normalized form for comparison.
	Normalized string

	// Type is the claim type (file, symbol, framework).
	Type ClaimType

	// Value is the extracted value.
	Value string

	// SampleIndices tracks which samples contained this claim.
	SampleIndices []int
}

// ConsensusResult contains the outcome of multi-sample analysis.
type ConsensusResult struct {
	// ConsistentClaims are claims that appeared in threshold+ samples.
	ConsistentClaims []NormalizedClaim

	// InconsistentClaims are claims that appeared in fewer samples.
	InconsistentClaims []NormalizedClaim

	// TotalSamples is the number of samples analyzed.
	TotalSamples int

	// ConsensusRate is the percentage of claims that are consistent.
	ConsensusRate float64
}

// Check implements Checker interface for integration with grounding pipeline.
//
// Note: This checker doesn't call LLM directly. It provides tools for
// analyzing multiple pre-generated samples. Use AnalyzeSamples to perform
// the actual consistency check.
//
// Thread Safety: Safe for concurrent use.
func (v *MultiSampleVerifier) Check(ctx context.Context, input *CheckInput) []Violation {
	if !v.config.Enabled || input == nil || input.Response == "" {
		return nil
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return nil
	default:
	}

	// This checker doesn't run on single responses.
	// Use AnalyzeSamples for multi-sample analysis.
	return nil
}

// AnalyzeSamples performs multi-sample consistency analysis.
//
// Description:
//
//	Extracts claims from each sample, normalizes them for comparison,
//	and identifies which claims appear consistently across samples.
//
// Inputs:
//
//	samples - The response samples to analyze.
//
// Outputs:
//
//	*ConsensusResult - The consensus analysis result.
func (v *MultiSampleVerifier) AnalyzeSamples(samples []string) *ConsensusResult {
	if len(samples) == 0 {
		return &ConsensusResult{
			TotalSamples:  0,
			ConsensusRate: 1.0,
		}
	}

	// Extract claims from each sample
	sampleResponses := make([]SampleResponse, len(samples))
	checker := NewGroundingChecker(nil)

	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, content := range samples {
		wg.Add(1)
		go func(idx int, text string) {
			defer wg.Done()

			claims := checker.extractClaims(text)
			normalized := make([]NormalizedClaim, len(claims))

			for j, claim := range claims {
				normalized[j] = NormalizedClaim{
					Original:      claim.RawText,
					Normalized:    normalizeClaim(claim),
					Type:          claim.Type,
					Value:         claim.Value,
					SampleIndices: []int{idx},
				}
			}

			mu.Lock()
			sampleResponses[idx] = SampleResponse{
				Index:   idx,
				Content: text,
				Claims:  normalized,
			}
			mu.Unlock()
		}(i, content)
	}
	wg.Wait()

	// Find consensus across samples
	return v.findConsensus(sampleResponses)
}

// findConsensus identifies consistent and inconsistent claims.
func (v *MultiSampleVerifier) findConsensus(samples []SampleResponse) *ConsensusResult {
	// Build claim frequency map
	claimCounts := make(map[string]*NormalizedClaim)

	for _, sample := range samples {
		// Track unique claims per sample (avoid double-counting)
		seenInSample := make(map[string]bool)

		for _, claim := range sample.Claims {
			key := claim.Normalized
			if seenInSample[key] {
				continue
			}
			seenInSample[key] = true

			if existing, ok := claimCounts[key]; ok {
				existing.SampleIndices = append(existing.SampleIndices, sample.Index)
			} else {
				claimCopy := claim
				claimCounts[key] = &claimCopy
			}
		}
	}

	// Separate consistent and inconsistent claims
	result := &ConsensusResult{
		TotalSamples:       len(samples),
		ConsistentClaims:   make([]NormalizedClaim, 0),
		InconsistentClaims: make([]NormalizedClaim, 0),
	}

	totalClaims := 0
	consistentCount := 0

	for _, claim := range claimCounts {
		totalClaims++
		count := len(claim.SampleIndices)

		if count >= v.config.ConsensusThreshold {
			result.ConsistentClaims = append(result.ConsistentClaims, *claim)
			consistentCount++
		} else {
			result.InconsistentClaims = append(result.InconsistentClaims, *claim)
		}
	}

	if totalClaims > 0 {
		result.ConsensusRate = float64(consistentCount) / float64(totalClaims)
	} else {
		result.ConsensusRate = 1.0
	}

	return result
}

// ConvertToViolations converts inconsistent claims to grounding violations.
//
// Inputs:
//
//	result - The consensus result.
//
// Outputs:
//
//	[]Violation - Violations for inconsistent claims.
func (v *MultiSampleVerifier) ConvertToViolations(result *ConsensusResult) []Violation {
	if result == nil {
		return nil
	}

	var violations []Violation

	for _, claim := range result.InconsistentClaims {
		// Determine severity based on how many samples contained the claim
		severity := SeverityWarning
		if len(claim.SampleIndices) == 1 {
			// Only appeared in 1 sample - more likely hallucination
			severity = SeverityCritical
		}

		violations = append(violations, Violation{
			Type:     ViolationUngrounded,
			Severity: severity,
			Code:     "MULTI_SAMPLE_INCONSISTENT",
			Message:  "Claim inconsistent across samples (appeared in " + formatSampleCount(claim.SampleIndices, result.TotalSamples) + ")",
			Evidence: claim.Original,
		})
	}

	return violations
}

// normalizeClaim creates a canonical representation for comparison.
func normalizeClaim(claim Claim) string {
	// Normalize: lowercase, remove punctuation, keep key entities
	normalized := strings.ToLower(claim.RawText)

	// Remove common filler words
	fillers := []string{"the ", "a ", "an ", "this ", "that ", "is ", "are ", "was ", "were "}
	for _, filler := range fillers {
		normalized = strings.ReplaceAll(normalized, filler, " ")
	}

	// Remove punctuation except for file extensions
	normalized = regexp.MustCompile(`[^\w\s.]`).ReplaceAllString(normalized, " ")

	// Collapse whitespace
	normalized = regexp.MustCompile(`\s+`).ReplaceAllString(normalized, " ")
	normalized = strings.TrimSpace(normalized)

	// Add type prefix for better disambiguation
	switch claim.Type {
	case ClaimFile:
		normalized = "file:" + claim.Value
	case ClaimSymbol:
		normalized = "symbol:" + strings.ToLower(claim.Value)
	case ClaimFramework:
		normalized = "framework:" + strings.ToLower(claim.Value)
	}

	return normalized
}

// formatSampleCount formats sample indices for display.
func formatSampleCount(indices []int, total int) string {
	return strings.TrimSpace(strings.Repeat(".", len(indices)) + "/" + strings.Repeat(".", total))
}

// GetConfig returns the verifier configuration.
func (v *MultiSampleVerifier) GetConfig() *MultiSampleConfig {
	return v.config
}

// IsConsistent checks if a consensus result indicates consistency.
//
// Inputs:
//
//	result - The consensus result.
//	minRate - Minimum consensus rate to be considered consistent (0.0-1.0).
//
// Outputs:
//
//	bool - True if the result is sufficiently consistent.
func IsConsistent(result *ConsensusResult, minRate float64) bool {
	if result == nil {
		return true
	}
	return result.ConsensusRate >= minRate
}
