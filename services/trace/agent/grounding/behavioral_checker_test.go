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
	"strings"
	"testing"
	"time"
)

func TestBehavioralChecker_Name(t *testing.T) {
	checker := NewBehavioralChecker(nil)
	if checker.Name() != "behavioral_checker" {
		t.Errorf("expected name 'behavioral_checker', got %q", checker.Name())
	}
}

func TestBehavioralChecker_Disabled(t *testing.T) {
	config := &BehavioralCheckerConfig{
		Enabled: false,
	}
	checker := NewBehavioralChecker(config)

	violations := checker.Check(context.Background(), &CheckInput{
		Response: "ProcessData logs errors gracefully",
		EvidenceIndex: &EvidenceIndex{
			Symbols: map[string]bool{"ProcessData": true},
		},
	})

	if len(violations) != 0 {
		t.Errorf("expected 0 violations when disabled, got %d", len(violations))
	}
}

func TestBehavioralChecker_NilInput(t *testing.T) {
	checker := NewBehavioralChecker(nil)

	violations := checker.Check(context.Background(), nil)
	if violations != nil {
		t.Errorf("expected nil violations for nil input, got %v", violations)
	}
}

func TestBehavioralChecker_EmptyResponse(t *testing.T) {
	checker := NewBehavioralChecker(nil)

	violations := checker.Check(context.Background(), &CheckInput{
		Response:      "",
		EvidenceIndex: NewEvidenceIndex(),
	})

	if violations != nil {
		t.Errorf("expected nil violations for empty response, got %v", violations)
	}
}

func TestBehavioralChecker_NilEvidenceIndex(t *testing.T) {
	checker := NewBehavioralChecker(nil)

	violations := checker.Check(context.Background(), &CheckInput{
		Response:      "ProcessData logs errors",
		EvidenceIndex: nil,
	})

	if violations != nil {
		t.Errorf("expected nil violations for nil evidence index, got %v", violations)
	}
}

func TestBehavioralChecker_ErrorHandling_WithEvidence(t *testing.T) {
	checker := NewBehavioralChecker(nil)

	// Code shows proper error handling
	code := `
func ProcessData(data []byte) error {
    result, err := parse(data)
    if err != nil {
        log.Error("failed to parse", slog.Any("error", err))
        return fmt.Errorf("parse failed: %w", err)
    }
    return nil
}
`

	input := &CheckInput{
		Response: "ProcessData logs errors and returns them to the caller.",
		EvidenceIndex: &EvidenceIndex{
			Symbols:      map[string]bool{"ProcessData": true},
			RawContent:   code,
			FileContents: map[string]string{"handler.go": code},
			SymbolDetails: map[string][]SymbolInfo{
				"ProcessData": {{Name: "ProcessData", File: "handler.go"}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations (error handling evidence found), got %d: %+v",
			len(violations), violations)
	}
}

func TestBehavioralChecker_ErrorHandling_WithCounterEvidence(t *testing.T) {
	checker := NewBehavioralChecker(nil)

	// Code shows swallowed errors
	code := `
func ProcessData(data []byte) {
    result, err := parse(data)
    _ = result  // ignored
    catch {}    // empty catch
}
`

	input := &CheckInput{
		Response: "ProcessData handles errors gracefully.",
		EvidenceIndex: &EvidenceIndex{
			Symbols:      map[string]bool{"ProcessData": true},
			RawContent:   code,
			FileContents: map[string]string{"handler.go": code},
			SymbolDetails: map[string][]SymbolInfo{
				"ProcessData": {{Name: "ProcessData", File: "handler.go"}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation (counter-evidence found), got %d", len(violations))
	}

	v := violations[0]
	if v.Type != ViolationBehavioralHallucination {
		t.Errorf("expected type ViolationBehavioralHallucination, got %v", v.Type)
	}
	if v.Code != "BEHAVIORAL_ERROR_HANDLING" {
		t.Errorf("expected code BEHAVIORAL_ERROR_HANDLING, got %q", v.Code)
	}
	if v.Severity != SeverityHigh {
		t.Errorf("expected severity High, got %v", v.Severity)
	}
}

func TestBehavioralChecker_Validation_WithEvidence(t *testing.T) {
	checker := NewBehavioralChecker(nil)

	// Code shows validation
	code := `
func ValidateUser(user *User) error {
    if len(user.Name) == 0 {
        return errors.New("name required")
    }
    if user.Email == "" {
        return errors.New("email required")
    }
    return nil
}
`

	input := &CheckInput{
		Response: "ValidateUser validates the input data before processing.",
		EvidenceIndex: &EvidenceIndex{
			Symbols:      map[string]bool{"ValidateUser": true},
			RawContent:   code,
			FileContents: map[string]string{"user.go": code},
			SymbolDetails: map[string][]SymbolInfo{
				"ValidateUser": {{Name: "ValidateUser", File: "user.go"}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations (validation evidence found), got %d: %+v",
			len(violations), violations)
	}
}

func TestBehavioralChecker_Validation_NoEvidence_RequireCounterEvidence(t *testing.T) {
	// Default config requires counter-evidence to flag
	checker := NewBehavioralChecker(nil)

	// Code shows no validation
	code := `
func ProcessInput(input string) string {
    return strings.ToUpper(input)
}
`

	input := &CheckInput{
		Response: "ProcessInput validates the input before processing.",
		EvidenceIndex: &EvidenceIndex{
			Symbols:      map[string]bool{"ProcessInput": true},
			RawContent:   code,
			FileContents: map[string]string{"process.go": code},
			SymbolDetails: map[string][]SymbolInfo{
				"ProcessInput": {{Name: "ProcessInput", File: "process.go"}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)

	// With RequireCounterEvidence=true (default), no counter-evidence = no violation
	if len(violations) != 0 {
		t.Errorf("expected 0 violations (RequireCounterEvidence=true, no counter-evidence), got %d",
			len(violations))
	}
}

func TestBehavioralChecker_Validation_NoEvidence_NoRequireCounterEvidence(t *testing.T) {
	// Config that doesn't require counter-evidence
	config := &BehavioralCheckerConfig{
		Enabled:                true,
		CheckValidation:        true,
		RequireCounterEvidence: false, // Flag even without counter-evidence
		MaxClaimsToCheck:       50,
	}
	checker := NewBehavioralChecker(config)

	// Code shows no validation
	code := `
func ProcessInput(input string) string {
    return strings.ToUpper(input)
}
`

	input := &CheckInput{
		Response: "ProcessInput validates the input before processing.",
		EvidenceIndex: &EvidenceIndex{
			Symbols:      map[string]bool{"ProcessInput": true},
			RawContent:   code,
			FileContents: map[string]string{"process.go": code},
			SymbolDetails: map[string][]SymbolInfo{
				"ProcessInput": {{Name: "ProcessInput", File: "process.go"}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)

	// With RequireCounterEvidence=false, no evidence = warning
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation (no evidence, RequireCounterEvidence=false), got %d",
			len(violations))
	}

	if violations[0].Severity != SeverityWarning {
		t.Errorf("expected severity Warning, got %v", violations[0].Severity)
	}
}

func TestBehavioralChecker_Security_WithEvidence(t *testing.T) {
	checker := NewBehavioralChecker(nil)

	// Code shows encryption
	code := `
func StorePassword(password string) (string, error) {
    hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    if err != nil {
        return "", err
    }
    return string(hash), nil
}
`

	input := &CheckInput{
		Response: "StorePassword hashes the password securely.",
		EvidenceIndex: &EvidenceIndex{
			Symbols:      map[string]bool{"StorePassword": true},
			RawContent:   code,
			FileContents: map[string]string{"auth.go": code},
			SymbolDetails: map[string][]SymbolInfo{
				"StorePassword": {{Name: "StorePassword", File: "auth.go"}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations (security evidence found), got %d: %+v",
			len(violations), violations)
	}
}

func TestBehavioralChecker_Security_WithCounterEvidence(t *testing.T) {
	checker := NewBehavioralChecker(nil)

	// Code shows plaintext password storage
	code := `
func StorePassword(password string) error {
    // TODO encrypt this later
    plaintext := password
    db.Save("password", plaintext)
    return nil
}
`

	input := &CheckInput{
		Response: "StorePassword encrypts the password before storage.",
		EvidenceIndex: &EvidenceIndex{
			Symbols:      map[string]bool{"StorePassword": true},
			RawContent:   code,
			FileContents: map[string]string{"auth.go": code},
			SymbolDetails: map[string][]SymbolInfo{
				"StorePassword": {{Name: "StorePassword", File: "auth.go"}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation (security counter-evidence found), got %d", len(violations))
	}

	v := violations[0]
	if v.Type != ViolationBehavioralHallucination {
		t.Errorf("expected type ViolationBehavioralHallucination, got %v", v.Type)
	}
	if v.Code != "BEHAVIORAL_SECURITY" {
		t.Errorf("expected code BEHAVIORAL_SECURITY, got %q", v.Code)
	}
}

func TestBehavioralChecker_SubjectNotInEvidence(t *testing.T) {
	checker := NewBehavioralChecker(nil)

	code := `
func OtherFunction() {
    // Different function
}
`

	input := &CheckInput{
		Response: "ProcessData logs errors gracefully.",
		EvidenceIndex: &EvidenceIndex{
			Symbols:      map[string]bool{"OtherFunction": true}, // ProcessData not in evidence
			RawContent:   code,
			FileContents: map[string]string{"other.go": code},
		},
	}

	violations := checker.Check(context.Background(), input)

	// Subject not in evidence = skip validation (not a violation)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations (subject not in evidence), got %d", len(violations))
	}
}

func TestBehavioralChecker_SubjectInRawContent(t *testing.T) {
	// When subject is found via function definition in RawContent
	config := &BehavioralCheckerConfig{
		Enabled:                true,
		CheckErrorHandling:     true,
		RequireCounterEvidence: true,
		MaxClaimsToCheck:       50,
	}
	checker := NewBehavioralChecker(config)

	code := `
func ProcessData(data []byte) {
    catch {}    // counter-evidence
}
`

	input := &CheckInput{
		Response: "ProcessData handles errors gracefully.",
		EvidenceIndex: &EvidenceIndex{
			Symbols:    map[string]bool{}, // Not in Symbols map
			RawContent: code,              // But defined in code
		},
	}

	violations := checker.Check(context.Background(), input)

	// Should find the subject via RawContent regex and flag counter-evidence
	if len(violations) != 1 {
		t.Errorf("expected 1 violation (subject found in RawContent), got %d", len(violations))
	}
}

func TestBehavioralChecker_CategoryDisabled(t *testing.T) {
	tests := []struct {
		name     string
		config   *BehavioralCheckerConfig
		response string
	}{
		{
			name: "error handling disabled",
			config: &BehavioralCheckerConfig{
				Enabled:            true,
				CheckErrorHandling: false,
				CheckValidation:    true,
				CheckSecurity:      true,
			},
			response: "ProcessData logs errors gracefully.",
		},
		{
			name: "validation disabled",
			config: &BehavioralCheckerConfig{
				Enabled:            true,
				CheckErrorHandling: true,
				CheckValidation:    false,
				CheckSecurity:      true,
			},
			response: "ProcessData validates the input.",
		},
		{
			name: "security disabled",
			config: &BehavioralCheckerConfig{
				Enabled:            true,
				CheckErrorHandling: true,
				CheckValidation:    true,
				CheckSecurity:      false,
			},
			response: "ProcessData encrypts the data.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := NewBehavioralChecker(tt.config)

			// Code with counter-evidence
			code := `
func ProcessData() {
    catch {}
    // TODO encrypt
    plaintext := "data"
}
`

			input := &CheckInput{
				Response: tt.response,
				EvidenceIndex: &EvidenceIndex{
					Symbols:    map[string]bool{"ProcessData": true},
					RawContent: code,
				},
			}

			violations := checker.Check(context.Background(), input)

			if len(violations) != 0 {
				t.Errorf("expected 0 violations (category disabled), got %d", len(violations))
			}
		})
	}
}

func TestBehavioralChecker_MaxClaimsLimit(t *testing.T) {
	config := &BehavioralCheckerConfig{
		Enabled:                true,
		CheckErrorHandling:     true,
		CheckValidation:        true,
		CheckSecurity:          true,
		RequireCounterEvidence: false, // Flag even without counter-evidence
		MaxClaimsToCheck:       2,
	}
	checker := NewBehavioralChecker(config)

	code := `
func FuncA() {}
func FuncB() {}
func FuncC() {}
func FuncD() {}
`

	// Response with 4 claims
	response := `FuncA logs errors. FuncB validates input. FuncC encrypts data. FuncD handles errors.`

	input := &CheckInput{
		Response: response,
		EvidenceIndex: &EvidenceIndex{
			Symbols:    map[string]bool{"FuncA": true, "FuncB": true, "FuncC": true, "FuncD": true},
			RawContent: code,
		},
	}

	violations := checker.Check(context.Background(), input)

	// Should only check 2 claims (MaxClaimsToCheck)
	if len(violations) > 2 {
		t.Errorf("expected at most 2 violations (MaxClaimsToCheck=2), got %d", len(violations))
	}
}

func TestBehavioralChecker_ContextCancellation(t *testing.T) {
	config := &BehavioralCheckerConfig{
		Enabled:                true,
		CheckErrorHandling:     true,
		RequireCounterEvidence: false,
		MaxClaimsToCheck:       100,
	}
	checker := NewBehavioralChecker(config)

	code := `
func FuncA() {}
func FuncB() {}
func FuncC() {}
`

	response := `FuncA logs errors. FuncB handles errors. FuncC returns errors.`

	input := &CheckInput{
		Response: response,
		EvidenceIndex: &EvidenceIndex{
			Symbols:    map[string]bool{"FuncA": true, "FuncB": true, "FuncC": true},
			RawContent: code,
		},
	}

	// Create already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	violations := checker.Check(ctx, input)

	// Should return early with partial/no results
	if len(violations) >= 3 {
		t.Errorf("expected fewer than 3 violations due to cancellation, got %d", len(violations))
	}
}

func TestBehavioralChecker_CommonWordsFiltered(t *testing.T) {
	checker := NewBehavioralChecker(nil)

	code := `
func RealFunction() {
    catch {}
}
`

	// "The", "This", "Function" should be filtered out as subjects
	response := `The function logs errors. This method validates input. Function handles errors.`

	input := &CheckInput{
		Response: response,
		EvidenceIndex: &EvidenceIndex{
			Symbols:    map[string]bool{"RealFunction": true},
			RawContent: code,
		},
	}

	violations := checker.Check(context.Background(), input)

	// Common words should be filtered, so no violations
	if len(violations) != 0 {
		t.Errorf("expected 0 violations (common words filtered), got %d: %+v",
			len(violations), violations)
	}
}

func TestBehavioralChecker_MultipleClaims(t *testing.T) {
	config := &BehavioralCheckerConfig{
		Enabled:                true,
		CheckErrorHandling:     true,
		CheckValidation:        true,
		CheckSecurity:          true,
		RequireCounterEvidence: false,
		MaxClaimsToCheck:       50,
	}
	checker := NewBehavioralChecker(config)

	// Code with no evidence for any category
	code := `
func ProcessData(data []byte) {
    return data
}
func ValidateData(data []byte) {}
func EncryptData(data []byte) {}
`

	// Each claim must have a proper subject immediately before the verb
	response := `ProcessData logs errors. ValidateData validates the input. EncryptData encrypts the data.`

	input := &CheckInput{
		Response: response,
		EvidenceIndex: &EvidenceIndex{
			Symbols:    map[string]bool{"ProcessData": true, "ValidateData": true, "EncryptData": true},
			RawContent: code,
		},
	}

	violations := checker.Check(context.Background(), input)

	// Should find 3 violations (one for each category)
	if len(violations) != 3 {
		t.Errorf("expected 3 violations (one per category), got %d: %+v",
			len(violations), violations)
	}

	// Verify all three categories are represented
	categories := make(map[string]bool)
	for _, v := range violations {
		categories[v.Code] = true
	}

	expectedCodes := []string{
		"BEHAVIORAL_ERROR_HANDLING",
		"BEHAVIORAL_VALIDATION",
		"BEHAVIORAL_SECURITY",
	}

	for _, code := range expectedCodes {
		if !categories[code] {
			t.Errorf("expected violation code %q not found", code)
		}
	}
}

func TestBehavioralChecker_Deduplication(t *testing.T) {
	config := &BehavioralCheckerConfig{
		Enabled:                true,
		CheckErrorHandling:     true,
		RequireCounterEvidence: false,
		MaxClaimsToCheck:       50,
	}
	checker := NewBehavioralChecker(config)

	code := `
func ProcessData() {}
`

	// Same claim repeated multiple times
	response := `ProcessData logs errors. ProcessData returns errors. ProcessData handles errors gracefully.`

	input := &CheckInput{
		Response: response,
		EvidenceIndex: &EvidenceIndex{
			Symbols:    map[string]bool{"ProcessData": true},
			RawContent: code,
		},
	}

	violations := checker.Check(context.Background(), input)

	// Should only have 1 violation (deduplicated by subject+category)
	if len(violations) != 1 {
		t.Errorf("expected 1 violation (deduplicated), got %d", len(violations))
	}
}

func TestBehavioralClaimCategory_String(t *testing.T) {
	tests := []struct {
		category BehavioralClaimCategory
		expected string
	}{
		{ClaimErrorHandling, "error_handling"},
		{ClaimValidation, "validation"},
		{ClaimSecurity, "security"},
		{BehavioralClaimCategory(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.category.String(); got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestBehavioralChecker_ViolationPriority(t *testing.T) {
	// Verify that behavioral hallucination has the correct priority
	v := Violation{
		Type: ViolationBehavioralHallucination,
	}

	priority := v.Priority()
	if priority != PriorityBehavioralHallucination {
		t.Errorf("expected priority %d, got %d",
			PriorityBehavioralHallucination, priority)
	}

	// Behavioral is P2 (same as structural claim)
	if priority != PriorityStructuralClaim {
		t.Errorf("expected behavioral priority to equal structural claim priority")
	}
}

func TestBehavioralChecker_Integration(t *testing.T) {
	// Full integration test with realistic scenario
	checker := NewBehavioralChecker(nil)

	// Realistic code that swallows errors
	code := `
package handler

import "fmt"

func HandleRequest(r *Request) *Response {
    data, err := fetchData(r.ID)
    _ = err  // Error swallowed!

    result := processData(data)
    return &Response{Data: result}
}

func processData(data []byte) []byte {
    // No validation here
    return data
}
`

	// Use pattern that matches the regex: "FunctionName logs/returns/handles errors"
	response := `HandleRequest logs errors and returns errors to the caller for proper handling.`

	input := &CheckInput{
		Response: response,
		EvidenceIndex: &EvidenceIndex{
			Symbols: map[string]bool{
				"HandleRequest": true,
				"processData":   true,
			},
			RawContent:   code,
			FileContents: map[string]string{"handler.go": code},
			SymbolDetails: map[string][]SymbolInfo{
				"HandleRequest": {{Name: "HandleRequest", File: "handler.go", Kind: "function"}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)

	// Should find at least 1 violation for error handling (counter-evidence: _ = err)
	if len(violations) < 1 {
		t.Errorf("expected at least 1 violation, got %d", len(violations))
	}

	// Verify error handling violation found
	foundErrorHandling := false
	for _, v := range violations {
		if v.Code == "BEHAVIORAL_ERROR_HANDLING" {
			foundErrorHandling = true
			break
		}
	}

	if !foundErrorHandling {
		t.Error("expected BEHAVIORAL_ERROR_HANDLING violation")
	}
}

func TestBehavioralChecker_Performance(t *testing.T) {
	checker := NewBehavioralChecker(nil)

	// Large code sample - use uppercase function names that match pattern
	var codeBuilder strings.Builder
	for i := 0; i < 100; i++ {
		codeBuilder.WriteString(fmt.Sprintf("func FuncHandler%d() { _ = err }\n", i))
	}
	code := codeBuilder.String()

	// Many claims - must start with uppercase letter
	var responseBuilder strings.Builder
	for i := 0; i < 50; i++ {
		responseBuilder.WriteString(fmt.Sprintf("FuncHandler%d handles errors. ", i))
	}
	response := responseBuilder.String()

	// Build symbols map
	symbols := make(map[string]bool)
	for i := 0; i < 100; i++ {
		symbols[fmt.Sprintf("FuncHandler%d", i)] = true
	}

	input := &CheckInput{
		Response: response,
		EvidenceIndex: &EvidenceIndex{
			Symbols:    symbols,
			RawContent: code,
		},
	}

	start := time.Now()
	violations := checker.Check(context.Background(), input)
	duration := time.Since(start)

	// Should complete in reasonable time (< 1 second for 50 claims)
	if duration > time.Second {
		t.Errorf("check took too long: %v", duration)
	}

	// Should find violations (all have counter-evidence)
	if len(violations) == 0 {
		t.Error("expected violations to be found")
	}

	t.Logf("Checked %d claims, found %d violations in %v", 50, len(violations), duration)
}
