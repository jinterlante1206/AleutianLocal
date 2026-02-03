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
	"testing"
)

func TestNewTMSVerifier(t *testing.T) {
	t.Run("with nil config uses defaults", func(t *testing.T) {
		verifier := NewTMSVerifier(nil)
		if verifier == nil {
			t.Fatal("expected non-nil verifier")
		}
		if !verifier.config.Enabled {
			t.Error("expected Enabled to be true by default")
		}
	})

	t.Run("with custom config", func(t *testing.T) {
		config := &TMSVerifierConfig{
			Enabled:       false,
			MaxIterations: 50,
		}
		verifier := NewTMSVerifier(config)
		if verifier.config.Enabled {
			t.Error("expected Enabled to be false")
		}
		if verifier.config.MaxIterations != 50 {
			t.Errorf("expected MaxIterations 50, got %d", verifier.config.MaxIterations)
		}
	})
}

func TestTMSVerifier_Name(t *testing.T) {
	verifier := NewTMSVerifier(nil)
	if name := verifier.Name(); name != "tms_verifier" {
		t.Errorf("expected name 'tms_verifier', got %q", name)
	}
}

func TestTMSVerifier_VerifyClaims_Disabled(t *testing.T) {
	config := &TMSVerifierConfig{Enabled: false}
	verifier := NewTMSVerifier(config)

	result, err := verifier.VerifyClaims(context.Background(), nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result when disabled")
	}
}

func TestTMSVerifier_VerifyClaims_FileClaims(t *testing.T) {
	verifier := NewTMSVerifier(nil)
	ctx := context.Background()

	t.Run("file in evidence is supported", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Files["main.go"] = true
		evidence.FileBasenames["main.go"] = true

		claims := []Claim{
			{Type: ClaimFile, Value: "main.go", RawText: "in main.go"},
		}

		result, err := verifier.VerifyClaims(ctx, claims, evidence)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.SupportedClaims) != 1 {
			t.Errorf("expected 1 supported claim, got %d", len(result.SupportedClaims))
		}
		if len(result.UnsupportedClaims) != 0 {
			t.Errorf("expected 0 unsupported claims, got %d", len(result.UnsupportedClaims))
		}
	})

	t.Run("file not in evidence is unsupported", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Files["server.go"] = true

		claims := []Claim{
			{Type: ClaimFile, Value: "app.py", RawText: "in app.py"},
		}

		result, err := verifier.VerifyClaims(ctx, claims, evidence)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.SupportedClaims) != 0 {
			t.Errorf("expected 0 supported claims, got %d", len(result.SupportedClaims))
		}
		if len(result.UnsupportedClaims) != 1 {
			t.Errorf("expected 1 unsupported claim, got %d", len(result.UnsupportedClaims))
		}
		if len(result.UnsupportedClaims) > 0 && result.UnsupportedClaims[0].Claim.Value != "app.py" {
			t.Errorf("expected unsupported claim for app.py")
		}
	})

	t.Run("file basename match is supported", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Files["cmd/server/main.go"] = true
		evidence.FileBasenames["main.go"] = true

		claims := []Claim{
			{Type: ClaimFile, Value: "main.go", RawText: "the main.go file"},
		}

		result, err := verifier.VerifyClaims(ctx, claims, evidence)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.SupportedClaims) != 1 {
			t.Errorf("expected 1 supported claim, got %d", len(result.SupportedClaims))
		}
	})
}

func TestTMSVerifier_VerifyClaims_SymbolClaims(t *testing.T) {
	verifier := NewTMSVerifier(nil)
	ctx := context.Background()

	t.Run("symbol in evidence is supported", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Symbols["HandleRequest"] = true

		claims := []Claim{
			{Type: ClaimSymbol, Value: "HandleRequest", RawText: "the HandleRequest function"},
		}

		result, err := verifier.VerifyClaims(ctx, claims, evidence)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.SupportedClaims) != 1 {
			t.Errorf("expected 1 supported claim, got %d", len(result.SupportedClaims))
		}
	})

	t.Run("symbol case insensitive match", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Symbols["handleRequest"] = true

		claims := []Claim{
			{Type: ClaimSymbol, Value: "HandleRequest", RawText: "the HandleRequest function"},
		}

		result, err := verifier.VerifyClaims(ctx, claims, evidence)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.SupportedClaims) != 1 {
			t.Errorf("expected 1 supported claim (case insensitive), got %d", len(result.SupportedClaims))
		}
	})

	t.Run("symbol not in evidence is unsupported", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Symbols["HandleRequest"] = true

		claims := []Claim{
			{Type: ClaimSymbol, Value: "ProcessData", RawText: "the ProcessData function"},
		}

		result, err := verifier.VerifyClaims(ctx, claims, evidence)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.UnsupportedClaims) != 1 {
			t.Errorf("expected 1 unsupported claim, got %d", len(result.UnsupportedClaims))
		}
	})
}

func TestTMSVerifier_VerifyClaims_FrameworkClaims(t *testing.T) {
	verifier := NewTMSVerifier(nil)
	ctx := context.Background()

	t.Run("framework in evidence is supported", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Frameworks["gin"] = true

		claims := []Claim{
			{Type: ClaimFramework, Value: "gin", RawText: "uses Gin"},
		}

		result, err := verifier.VerifyClaims(ctx, claims, evidence)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.SupportedClaims) != 1 {
			t.Errorf("expected 1 supported claim, got %d", len(result.SupportedClaims))
		}
	})

	t.Run("framework not in evidence is unsupported", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Frameworks["gin"] = true

		claims := []Claim{
			{Type: ClaimFramework, Value: "flask", RawText: "uses Flask"},
		}

		result, err := verifier.VerifyClaims(ctx, claims, evidence)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.UnsupportedClaims) != 1 {
			t.Errorf("expected 1 unsupported claim, got %d", len(result.UnsupportedClaims))
		}
	})
}

func TestTMSVerifier_VerifyClaims_MultipleClaims(t *testing.T) {
	verifier := NewTMSVerifier(nil)
	ctx := context.Background()

	evidence := NewEvidenceIndex()
	evidence.Files["main.go"] = true
	evidence.FileBasenames["main.go"] = true
	evidence.Symbols["HandleRequest"] = true
	evidence.Frameworks["gin"] = true

	claims := []Claim{
		{Type: ClaimFile, Value: "main.go", RawText: "in main.go"},            // Supported
		{Type: ClaimFile, Value: "app.py", RawText: "in app.py"},              // Unsupported
		{Type: ClaimSymbol, Value: "HandleRequest", RawText: "HandleRequest"}, // Supported
		{Type: ClaimSymbol, Value: "ProcessData", RawText: "ProcessData"},     // Unsupported
		{Type: ClaimFramework, Value: "gin", RawText: "uses Gin"},             // Supported
		{Type: ClaimFramework, Value: "flask", RawText: "uses Flask"},         // Unsupported
	}

	result, err := verifier.VerifyClaims(ctx, claims, evidence)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TotalClaims != 6 {
		t.Errorf("expected 6 total claims, got %d", result.TotalClaims)
	}

	if len(result.SupportedClaims) != 3 {
		t.Errorf("expected 3 supported claims, got %d", len(result.SupportedClaims))
	}

	if len(result.UnsupportedClaims) != 3 {
		t.Errorf("expected 3 unsupported claims, got %d", len(result.UnsupportedClaims))
	}
}

func TestTMSVerifier_VerifyClaims_EmptyClaims(t *testing.T) {
	verifier := NewTMSVerifier(nil)
	ctx := context.Background()

	evidence := NewEvidenceIndex()
	evidence.Files["main.go"] = true

	result, err := verifier.VerifyClaims(ctx, []Claim{}, evidence)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TotalClaims != 0 {
		t.Errorf("expected 0 total claims, got %d", result.TotalClaims)
	}
}

func TestTMSVerifier_VerifyClaims_NilEvidence(t *testing.T) {
	verifier := NewTMSVerifier(nil)
	ctx := context.Background()

	claims := []Claim{
		{Type: ClaimFile, Value: "main.go", RawText: "in main.go"},
	}

	result, err := verifier.VerifyClaims(ctx, claims, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All claims should be unsupported with nil evidence
	if len(result.UnsupportedClaims) != 1 {
		t.Errorf("expected 1 unsupported claim with nil evidence, got %d", len(result.UnsupportedClaims))
	}
}

func TestTMSVerifier_Check_Integration(t *testing.T) {
	verifier := NewTMSVerifier(nil)
	ctx := context.Background()

	t.Run("detects ungrounded claims", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Files["main.go"] = true
		evidence.FileBasenames["main.go"] = true

		input := &CheckInput{
			Response:      "The app.py file uses Flask for routing.",
			EvidenceIndex: evidence,
		}

		violations := verifier.Check(ctx, input)

		// Should have violations for app.py and flask
		if len(violations) < 1 {
			t.Errorf("expected at least 1 violation, got %d", len(violations))
		}

		hasUngroundedViolation := false
		for _, v := range violations {
			if v.Code == "TMS_UNSUPPORTED_CLAIM" {
				hasUngroundedViolation = true
			}
		}
		if !hasUngroundedViolation {
			t.Error("expected TMS_UNSUPPORTED_CLAIM violation")
		}
	})

	t.Run("no violations for grounded claims", func(t *testing.T) {
		evidence := NewEvidenceIndex()
		evidence.Files["main.go"] = true
		evidence.FileBasenames["main.go"] = true
		evidence.Frameworks["gin"] = true

		input := &CheckInput{
			Response:      "The main.go file uses Gin for routing.",
			EvidenceIndex: evidence,
		}

		violations := verifier.Check(ctx, input)

		// Filter to only TMS violations
		tmsViolations := make([]Violation, 0)
		for _, v := range violations {
			if v.Code == "TMS_UNSUPPORTED_CLAIM" {
				tmsViolations = append(tmsViolations, v)
			}
		}

		if len(tmsViolations) > 0 {
			t.Errorf("expected no TMS violations for grounded claims, got %d", len(tmsViolations))
		}
	})

	t.Run("disabled returns nil", func(t *testing.T) {
		config := &TMSVerifierConfig{Enabled: false}
		disabledVerifier := NewTMSVerifier(config)

		input := &CheckInput{
			Response:      "The app.py file uses Flask.",
			EvidenceIndex: NewEvidenceIndex(),
		}

		violations := disabledVerifier.Check(ctx, input)
		if violations != nil {
			t.Error("expected nil violations when disabled")
		}
	})

	t.Run("empty response returns nil", func(t *testing.T) {
		input := &CheckInput{
			Response:      "",
			EvidenceIndex: NewEvidenceIndex(),
		}

		violations := verifier.Check(ctx, input)
		if violations != nil {
			t.Error("expected nil violations for empty response")
		}
	})
}

func TestTMSVerifier_ContextCancellation(t *testing.T) {
	verifier := NewTMSVerifier(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	evidence := NewEvidenceIndex()
	claims := []Claim{
		{Type: ClaimFile, Value: "main.go", RawText: "in main.go"},
	}

	_, err := verifier.VerifyClaims(ctx, claims, evidence)
	if err == nil {
		t.Error("expected error due to context cancellation")
	}
}

func TestSanitizeID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"main.go", "main_go"},
		{"cmd/server/main.go", "cmd_server_main_go"},
		{"HandleRequest", "HandleRequest"},
		{"my-file.py", "my_file_py"},
		{"file with spaces.go", "file_with_spaces_go"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeID(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGetBasename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"main.go", "main.go"},
		{"cmd/server/main.go", "main.go"},
		{"/absolute/path/file.py", "file.py"},
		{"path\\windows\\file.go", "file.go"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := getBasename(tt.input)
			if result != tt.expected {
				t.Errorf("getBasename(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
