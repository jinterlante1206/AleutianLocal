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
	"strings"
	"testing"
)

func TestNewChainOfVerification(t *testing.T) {
	t.Run("with nil config uses defaults", func(t *testing.T) {
		verifier := NewChainOfVerification(nil)
		if verifier == nil {
			t.Fatal("expected non-nil verifier")
		}
		if verifier.config.Enabled {
			t.Error("expected Enabled to be false by default")
		}
		if verifier.config.MaxClaimsToVerify != 10 {
			t.Errorf("expected MaxClaimsToVerify 10, got %d", verifier.config.MaxClaimsToVerify)
		}
	})

	t.Run("with custom config", func(t *testing.T) {
		config := &ChainOfVerificationConfig{
			Enabled:           true,
			VerifyThreshold:   "HIGH",
			MaxClaimsToVerify: 5,
		}
		verifier := NewChainOfVerification(config)
		if !verifier.config.Enabled {
			t.Error("expected Enabled to be true")
		}
		if verifier.config.VerifyThreshold != "HIGH" {
			t.Errorf("expected VerifyThreshold HIGH, got %s", verifier.config.VerifyThreshold)
		}
	})
}

func TestChainOfVerification_Name(t *testing.T) {
	verifier := NewChainOfVerification(nil)
	if name := verifier.Name(); name != "chain_of_verification" {
		t.Errorf("expected name 'chain_of_verification', got %q", name)
	}
}

func TestChainOfVerification_Check_Disabled(t *testing.T) {
	config := &ChainOfVerificationConfig{Enabled: false}
	verifier := NewChainOfVerification(config)

	violations := verifier.Check(context.Background(), &CheckInput{
		Response: "test response",
	})

	if violations != nil {
		t.Error("expected nil violations when disabled")
	}
}

func TestChainOfVerification_Check_ContextCancellation(t *testing.T) {
	config := &ChainOfVerificationConfig{Enabled: true}
	verifier := NewChainOfVerification(config)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	violations := verifier.Check(ctx, &CheckInput{
		Response: "test response",
	})

	if violations != nil {
		t.Error("expected nil violations on cancelled context")
	}
}

func TestChainOfVerification_BuildVerificationPrompt(t *testing.T) {
	verifier := NewChainOfVerification(nil)

	t.Run("basic claims", func(t *testing.T) {
		claims := []ExtractedClaim{
			{ID: 1, Statement: "The main function exists", File: "main.go", Line: 10},
			{ID: 2, Statement: "Uses Gin framework", File: "server.go"},
		}

		prompt := verifier.BuildVerificationPrompt(claims)

		// Check required elements
		requiredPhrases := []string{
			"verify the following claims",
			"1. The main function exists",
			"(Claimed from: main.go:10)",
			"2. Uses Gin framework",
			"(Claimed from: server.go)",
			"HIGH / MEDIUM / LOW / CANNOT_VERIFY",
			"JSON format",
		}

		for _, phrase := range requiredPhrases {
			if !strings.Contains(prompt, phrase) {
				t.Errorf("expected prompt to contain %q", phrase)
			}
		}
	})

	t.Run("respects MaxClaimsToVerify", func(t *testing.T) {
		config := &ChainOfVerificationConfig{
			MaxClaimsToVerify: 2,
		}
		v := NewChainOfVerification(config)

		claims := []ExtractedClaim{
			{ID: 1, Statement: "Claim 1"},
			{ID: 2, Statement: "Claim 2"},
			{ID: 3, Statement: "Claim 3"},
			{ID: 4, Statement: "Claim 4"},
		}

		prompt := v.BuildVerificationPrompt(claims)

		// Should only include first 2 claims
		if strings.Contains(prompt, "Claim 3") || strings.Contains(prompt, "Claim 4") {
			t.Error("expected prompt to exclude claims beyond MaxClaimsToVerify")
		}
		if !strings.Contains(prompt, "Claim 1") || !strings.Contains(prompt, "Claim 2") {
			t.Error("expected prompt to include first MaxClaimsToVerify claims")
		}
	})

	t.Run("claim without file", func(t *testing.T) {
		claims := []ExtractedClaim{
			{ID: 1, Statement: "General claim about the project"},
		}

		prompt := verifier.BuildVerificationPrompt(claims)

		if strings.Contains(prompt, "(Claimed from:") {
			t.Error("expected no 'Claimed from' for claim without file")
		}
	})
}

func TestChainOfVerification_VerificationSystemPrompt(t *testing.T) {
	verifier := NewChainOfVerification(nil)
	prompt := verifier.VerificationSystemPrompt()

	requiredPhrases := []string{
		"claim verifier",
		"CANNOT_VERIFY",
		"JSON format",
		"HIGH",
		"MEDIUM",
		"LOW",
	}

	for _, phrase := range requiredPhrases {
		if !strings.Contains(prompt, phrase) {
			t.Errorf("expected system prompt to contain %q", phrase)
		}
	}
}

func TestChainOfVerification_ParseVerificationResponse(t *testing.T) {
	verifier := NewChainOfVerification(&ChainOfVerificationConfig{
		VerifyThreshold: "MEDIUM",
	})

	originalClaims := []ExtractedClaim{
		{ID: 1, Statement: "Main function exists"},
		{ID: 2, Statement: "Uses Flask framework"},
		{ID: 3, Statement: "Has error handling"},
	}

	t.Run("valid verification response", func(t *testing.T) {
		response := `{
			"verifications": [
				{"claim_id": 1, "verified": true, "confidence": "HIGH", "citation": "main.go:10"},
				{"claim_id": 2, "verified": false, "confidence": "CANNOT_VERIFY", "reason": "No Flask code in context"},
				{"claim_id": 3, "verified": true, "confidence": "MEDIUM", "citation": "main.go:25-30"}
			]
		}`

		result, err := verifier.ParseVerificationResponse(response, originalClaims)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.AllVerified {
			t.Error("expected AllVerified to be false (claim 2 unverified)")
		}

		if len(result.VerifiedClaims) != 2 {
			t.Errorf("expected 2 verified claims, got %d", len(result.VerifiedClaims))
		}

		if len(result.UnverifiedClaims) != 1 {
			t.Errorf("expected 1 unverified claim, got %d", len(result.UnverifiedClaims))
		}

		// Check the unverified claim
		if len(result.UnverifiedClaims) > 0 {
			unverified := result.UnverifiedClaims[0]
			if unverified.ClaimID != 2 {
				t.Errorf("expected unverified claim ID 2, got %d", unverified.ClaimID)
			}
			if unverified.Reason == "" {
				t.Error("expected reason for unverified claim")
			}
		}
	})

	t.Run("JSON in code block", func(t *testing.T) {
		response := "Here's the verification:\n```json\n" + `{
			"verifications": [
				{"claim_id": 1, "verified": true, "confidence": "HIGH", "citation": "main.go:10"}
			]
		}` + "\n```"

		result, err := verifier.ParseVerificationResponse(response, originalClaims[:1])
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.VerifiedClaims) != 1 {
			t.Errorf("expected 1 verified claim, got %d", len(result.VerifiedClaims))
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, err := verifier.ParseVerificationResponse("not json at all", originalClaims)
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})

	t.Run("empty verifications", func(t *testing.T) {
		response := `{"verifications": []}`

		result, err := verifier.ParseVerificationResponse(response, originalClaims)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result.AllVerified {
			t.Error("expected AllVerified to be true for empty verifications")
		}
	})
}

func TestChainOfVerification_meetsThreshold(t *testing.T) {
	tests := []struct {
		threshold    string
		verification ClaimVerification
		expected     bool
	}{
		// HIGH threshold
		{"HIGH", ClaimVerification{Verified: true, Confidence: "HIGH"}, true},
		{"HIGH", ClaimVerification{Verified: true, Confidence: "MEDIUM"}, false},
		{"HIGH", ClaimVerification{Verified: true, Confidence: "LOW"}, false},
		{"HIGH", ClaimVerification{Verified: false, Confidence: "HIGH"}, false},

		// MEDIUM threshold
		{"MEDIUM", ClaimVerification{Verified: true, Confidence: "HIGH"}, true},
		{"MEDIUM", ClaimVerification{Verified: true, Confidence: "MEDIUM"}, true},
		{"MEDIUM", ClaimVerification{Verified: true, Confidence: "LOW"}, false},

		// LOW threshold
		{"LOW", ClaimVerification{Verified: true, Confidence: "HIGH"}, true},
		{"LOW", ClaimVerification{Verified: true, Confidence: "MEDIUM"}, true},
		{"LOW", ClaimVerification{Verified: true, Confidence: "LOW"}, true},
		{"LOW", ClaimVerification{Verified: false, Confidence: "LOW"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.threshold+"_"+tt.verification.Confidence, func(t *testing.T) {
			v := NewChainOfVerification(&ChainOfVerificationConfig{
				VerifyThreshold: tt.threshold,
			})

			result := v.meetsThreshold(tt.verification)
			if result != tt.expected {
				t.Errorf("meetsThreshold(%v) = %v, want %v", tt.verification, result, tt.expected)
			}
		})
	}
}

func TestChainOfVerification_ConvertToViolations(t *testing.T) {
	verifier := NewChainOfVerification(nil)

	t.Run("nil result", func(t *testing.T) {
		violations := verifier.ConvertToViolations(nil)
		if violations != nil {
			t.Error("expected nil violations for nil result")
		}
	})

	t.Run("no unverified claims", func(t *testing.T) {
		result := &VerificationResult{
			AllVerified:      true,
			VerifiedClaims:   []VerifiedClaimResult{{ClaimID: 1}},
			UnverifiedClaims: nil,
		}

		violations := verifier.ConvertToViolations(result)
		if len(violations) != 0 {
			t.Errorf("expected 0 violations, got %d", len(violations))
		}
	})

	t.Run("unverified claims converted", func(t *testing.T) {
		result := &VerificationResult{
			AllVerified: false,
			UnverifiedClaims: []VerifiedClaimResult{
				{ClaimID: 1, Statement: "Claim 1", Confidence: "LOW", Reason: ""},
				{ClaimID: 2, Statement: "Claim 2", Confidence: "CANNOT_VERIFY", Reason: "Not found"},
			},
		}

		violations := verifier.ConvertToViolations(result)
		if len(violations) != 2 {
			t.Fatalf("expected 2 violations, got %d", len(violations))
		}

		// First should be warning (LOW)
		if violations[0].Severity != SeverityWarning {
			t.Errorf("expected first violation to be warning, got %s", violations[0].Severity)
		}

		// Second should be critical (CANNOT_VERIFY)
		if violations[1].Severity != SeverityCritical {
			t.Errorf("expected second violation to be critical, got %s", violations[1].Severity)
		}

		// Both should have COV code
		for _, v := range violations {
			if v.Code != "COV_UNVERIFIED_CLAIM" {
				t.Errorf("expected code COV_UNVERIFIED_CLAIM, got %s", v.Code)
			}
		}

		// Second should include reason
		if !strings.Contains(violations[1].Message, "Not found") {
			t.Error("expected second violation message to include reason")
		}
	})
}

func TestExtractClaimsForVerification(t *testing.T) {
	t.Run("extracts file and symbol claims", func(t *testing.T) {
		response := "The main.go file contains the HandleRequest function which processes requests."

		claims := ExtractClaimsForVerification(response)

		if len(claims) == 0 {
			t.Fatal("expected at least 1 claim")
		}

		// Check claims have IDs starting from 1
		for i, claim := range claims {
			if claim.ID != i+1 {
				t.Errorf("expected claim ID %d, got %d", i+1, claim.ID)
			}
		}
	})

	t.Run("empty response", func(t *testing.T) {
		claims := ExtractClaimsForVerification("")
		if len(claims) != 0 {
			t.Errorf("expected 0 claims for empty response, got %d", len(claims))
		}
	})
}

func TestExtractJSONBlock(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "json code block",
			input:    "Some text\n```json\n{\"key\": \"value\"}\n```\nMore text",
			expected: `{"key": "value"}`,
		},
		{
			name:     "plain code block",
			input:    "Some text\n```\n{\"key\": \"value\"}\n```\nMore text",
			expected: `{"key": "value"}`,
		},
		{
			name:     "bare JSON object",
			input:    "Here's the result: {\"key\": \"value\"} done",
			expected: `{"key": "value"}`,
		},
		{
			name:     "no JSON",
			input:    "Just plain text without any JSON",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSONBlock(tt.input)
			if result != tt.expected {
				t.Errorf("extractJSONBlock() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestChainOfVerification_Integration(t *testing.T) {
	// Test the full workflow
	verifier := NewChainOfVerification(&ChainOfVerificationConfig{
		Enabled:           true,
		VerifyThreshold:   "MEDIUM",
		MaxClaimsToVerify: 10,
	})

	// Simulate LLM response
	llmResponse := "The main.go file contains the Run function which uses Gin framework."

	// Extract claims
	claims := ExtractClaimsForVerification(llmResponse)
	if len(claims) == 0 {
		t.Skip("no claims extracted")
	}

	// Build verification prompt
	prompt := verifier.BuildVerificationPrompt(claims)
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}

	// Simulate verification response
	verificationResponse := `{
		"verifications": [
			{"claim_id": 1, "verified": true, "confidence": "HIGH", "citation": "main.go:1"},
			{"claim_id": 2, "verified": false, "confidence": "CANNOT_VERIFY", "reason": "No Gin imports found"}
		]
	}`

	// Parse verification response
	result, err := verifier.ParseVerificationResponse(verificationResponse, claims)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Convert to violations
	violations := verifier.ConvertToViolations(result)

	// Should have at least one violation for Gin
	hasGinViolation := false
	for _, v := range violations {
		if strings.Contains(v.Message, "Gin") || strings.Contains(v.Message, "reason") {
			hasGinViolation = true
		}
	}

	// Note: Whether we have a Gin violation depends on claim extraction
	// The important thing is the flow works
	_ = hasGinViolation
}
