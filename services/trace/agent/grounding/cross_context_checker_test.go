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

func TestCrossContextChecker_Name(t *testing.T) {
	checker := NewCrossContextChecker(nil)
	if checker.Name() != "cross_context_checker" {
		t.Errorf("expected 'cross_context_checker', got '%s'", checker.Name())
	}
}

func TestCrossContextChecker_Disabled(t *testing.T) {
	config := &CrossContextCheckerConfig{
		Enabled: false,
	}
	checker := NewCrossContextChecker(config)

	input := &CheckInput{
		Response: "Config in server has MaxRetries",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config": {
					{Name: "Config", File: "pkg/server/config.go", Kind: "type", Fields: []string{"Name", "Timeout"}},
					{Name: "Config", File: "pkg/client/config.go", Kind: "type", Fields: []string{"Name", "MaxRetries"}},
				},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations when disabled, got %d", len(violations))
	}
}

func TestCrossContextChecker_NilInput(t *testing.T) {
	checker := NewCrossContextChecker(nil)
	violations := checker.Check(context.Background(), nil)
	if violations != nil {
		t.Errorf("expected nil for nil input, got %v", violations)
	}
}

func TestCrossContextChecker_EmptyResponse(t *testing.T) {
	checker := NewCrossContextChecker(nil)
	input := &CheckInput{
		Response: "",
	}
	violations := checker.Check(context.Background(), input)
	if violations != nil {
		t.Errorf("expected nil for empty response, got %v", violations)
	}
}

func TestCrossContextChecker_NoSymbolDetails(t *testing.T) {
	checker := NewCrossContextChecker(nil)
	input := &CheckInput{
		Response:      "Config in server has MaxRetries",
		EvidenceIndex: NewEvidenceIndex(),
	}
	violations := checker.Check(context.Background(), input)
	if violations != nil {
		t.Errorf("expected nil when no symbol details, got %v", violations)
	}
}

func TestCrossContextChecker_SingleLocation_Correct(t *testing.T) {
	checker := NewCrossContextChecker(nil)

	input := &CheckInput{
		Response: "Config in server has Name and Timeout fields.",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config": {
					{Name: "Config", File: "pkg/server/config.go", Kind: "type", Fields: []string{"Name", "Timeout"}},
				},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for correct claim, got %d: %v", len(violations), violations)
	}
}

func TestCrossContextChecker_LocationMismatch(t *testing.T) {
	checker := NewCrossContextChecker(nil)

	input := &CheckInput{
		Response: "Config in database has Name field.",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config": {
					{Name: "Config", File: "pkg/server/config.go", Kind: "type", Fields: []string{"Name", "Timeout"}},
				},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) == 0 {
		t.Errorf("expected at least 1 violation for location mismatch, got 0")
		return
	}

	found := false
	for _, v := range violations {
		if v.Code == "CROSS_CONTEXT_LOCATION_MISMATCH" {
			found = true
			if v.Type != ViolationCrossContextConfusion {
				t.Errorf("expected type %s, got %s", ViolationCrossContextConfusion, v.Type)
			}
			if v.Severity != SeverityHigh {
				t.Errorf("expected severity %s, got %s", SeverityHigh, v.Severity)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected code CROSS_CONTEXT_LOCATION_MISMATCH, got %v", violations)
	}
}

func TestCrossContextChecker_AttributeConfusion(t *testing.T) {
	checker := NewCrossContextChecker(nil)

	// Config in server claimed to have MaxRetries, but MaxRetries is in client
	input := &CheckInput{
		Response: "The Config struct in server has MaxRetries for connection handling.",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config": {
					{Name: "Config", File: "pkg/server/config.go", Kind: "type", Fields: []string{"Name", "Timeout"}},
					{Name: "Config", File: "pkg/client/config.go", Kind: "type", Fields: []string{"Name", "MaxRetries"}},
				},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) == 0 {
		t.Errorf("expected at least 1 violation for attribute confusion, got 0")
		return
	}

	found := false
	for _, v := range violations {
		if v.Code == "CROSS_CONTEXT_ATTRIBUTE_CONFUSION" {
			found = true
			if v.Severity != SeverityHigh {
				t.Errorf("expected severity %s, got %s", SeverityHigh, v.Severity)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected code CROSS_CONTEXT_ATTRIBUTE_CONFUSION, got %v", violations)
	}
}

func TestCrossContextChecker_CorrectAttributeAtLocation(t *testing.T) {
	checker := NewCrossContextChecker(nil)

	// Config in server correctly has Timeout (not MaxRetries)
	input := &CheckInput{
		Response: "The Config struct in server has Timeout for connection handling.",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config": {
					{Name: "Config", File: "pkg/server/config.go", Kind: "type", Fields: []string{"Name", "Timeout"}},
					{Name: "Config", File: "pkg/client/config.go", Kind: "type", Fields: []string{"Name", "MaxRetries"}},
				},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for correct attribute at location, got %d: %v", len(violations), violations)
	}
}

func TestCrossContextChecker_AmbiguousReference(t *testing.T) {
	config := &CrossContextCheckerConfig{
		Enabled:                 true,
		FlagAmbiguousReferences: true,
		AmbiguityThreshold:      2,
		CheckLocationClaims:     true,
		CheckAttributeConfusion: true,
	}
	checker := NewCrossContextChecker(config)

	// "Config has Name" without specifying which Config
	input := &CheckInput{
		Response: "Config has Name field.",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config": {
					{Name: "Config", File: "pkg/server/config.go", Kind: "type", Fields: []string{"Name", "Timeout"}},
					{Name: "Config", File: "pkg/client/config.go", Kind: "type", Fields: []string{"Name", "MaxRetries"}},
				},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) == 0 {
		t.Errorf("expected at least 1 violation for ambiguous reference, got 0")
		return
	}

	found := false
	for _, v := range violations {
		if v.Code == "CROSS_CONTEXT_AMBIGUOUS_ATTRIBUTE" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected code CROSS_CONTEXT_AMBIGUOUS_ATTRIBUTE, got codes: %v", violationCodes(violations))
	}
}

func TestCrossContextChecker_UniqueAttribute_NoAmbiguity(t *testing.T) {
	config := &CrossContextCheckerConfig{
		Enabled:                 true,
		FlagAmbiguousReferences: true,
		AmbiguityThreshold:      2,
		CheckLocationClaims:     true,
		CheckAttributeConfusion: true,
	}
	checker := NewCrossContextChecker(config)

	// "Config has Timeout" - Timeout only exists in server Config
	input := &CheckInput{
		Response: "Config has Timeout field.",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config": {
					{Name: "Config", File: "pkg/server/config.go", Kind: "type", Fields: []string{"Name", "Timeout"}},
					{Name: "Config", File: "pkg/client/config.go", Kind: "type", Fields: []string{"Name", "MaxRetries"}},
				},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations when attribute is unique to one location, got %d: %v", len(violations), violations)
	}
}

func TestCrossContextChecker_LocationDotSymbol(t *testing.T) {
	checker := NewCrossContextChecker(nil)

	input := &CheckInput{
		Response: "The utils.ProcessData function formats strings.",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"ProcessData": {
					{Name: "ProcessData", File: "pkg/utils/processor.go", Kind: "function"},
					{Name: "ProcessData", File: "pkg/service/handler.go", Kind: "function"},
				},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Should not flag - utils.ProcessData correctly matches pkg/utils/processor.go
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for correct location.Symbol, got %d: %v", len(violations), violations)
	}
}

func TestCrossContextChecker_LocationDotSymbol_Mismatch(t *testing.T) {
	checker := NewCrossContextChecker(nil)

	input := &CheckInput{
		Response: "The database.ProcessData function formats strings.",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"ProcessData": {
					{Name: "ProcessData", File: "pkg/utils/processor.go", Kind: "function"},
					{Name: "ProcessData", File: "pkg/service/handler.go", Kind: "function"},
				},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) == 0 {
		t.Errorf("expected at least 1 violation for location mismatch, got 0")
		return
	}

	found := false
	for _, v := range violations {
		if v.Code == "CROSS_CONTEXT_LOCATION_MISMATCH" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected code CROSS_CONTEXT_LOCATION_MISMATCH, got codes: %v", violationCodes(violations))
	}
}

func TestCrossContextChecker_BuiltinPackagesIgnored(t *testing.T) {
	checker := NewCrossContextChecker(nil)

	// fmt.Println should not be flagged as a location claim
	input := &CheckInput{
		Response: "The function uses fmt.Println for logging.",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Println": {
					{Name: "Println", File: "pkg/logger/logger.go", Kind: "function"},
				},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Should not flag - fmt is a builtin package
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for builtin package reference, got %d: %v", len(violations), violations)
	}
}

func TestCrossContextChecker_StructWithFields(t *testing.T) {
	checker := NewCrossContextChecker(nil)

	// This claims Config has both Name (exists in both) and MaxRetries (only in client)
	// Without location, this should flag if FlagAmbiguousReferences is on
	config := &CrossContextCheckerConfig{
		Enabled:                 true,
		FlagAmbiguousReferences: true,
		AmbiguityThreshold:      2,
		CheckLocationClaims:     true,
		CheckAttributeConfusion: true,
	}
	checker = NewCrossContextChecker(config)

	input := &CheckInput{
		Response: "The Config struct has fields Name, MaxRetries.",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config": {
					{Name: "Config", File: "pkg/server/config.go", Kind: "type", Fields: []string{"Name", "Timeout"}},
					{Name: "Config", File: "pkg/client/config.go", Kind: "type", Fields: []string{"Name", "MaxRetries"}},
				},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Name exists in both - ambiguous
	// MaxRetries exists in only one - not ambiguous
	// Should flag Name as ambiguous
	found := false
	for _, v := range violations {
		if v.Code == "CROSS_CONTEXT_AMBIGUOUS_ATTRIBUTE" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected CROSS_CONTEXT_AMBIGUOUS_ATTRIBUTE for field Name, got codes: %v", violationCodes(violations))
	}
}

func TestCrossContextChecker_NoConfusion_SingleSymbol(t *testing.T) {
	checker := NewCrossContextChecker(nil)

	input := &CheckInput{
		Response: "The Handler function processes requests.",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Handler": {
					{Name: "Handler", File: "pkg/server/handler.go", Kind: "function"},
				},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for single-location symbol, got %d: %v", len(violations), violations)
	}
}

func TestCrossContextChecker_MaxClaimsLimit(t *testing.T) {
	config := &CrossContextCheckerConfig{
		Enabled:                 true,
		CheckLocationClaims:     true,
		CheckAttributeConfusion: true,
		MaxClaimsToCheck:        2,
	}
	checker := NewCrossContextChecker(config)

	// Many location claims but only 2 should be checked
	input := &CheckInput{
		Response: "Config in a, Config in b, Config in c, Config in d",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config": {
					{Name: "Config", File: "pkg/server/config.go", Kind: "type"},
				},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// All 4 are location mismatches, but only 2 should be checked
	if len(violations) > 2 {
		t.Errorf("expected at most 2 violations with MaxClaimsToCheck=2, got %d", len(violations))
	}
}

func TestCrossContextChecker_ContextCancellation(t *testing.T) {
	checker := NewCrossContextChecker(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	input := &CheckInput{
		Response: "Config in a, Config in b, Config in c",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config": {
					{Name: "Config", File: "pkg/server/config.go", Kind: "type"},
				},
			},
		},
	}

	violations := checker.Check(ctx, input)
	// Should return early due to context cancellation
	if len(violations) > 3 {
		t.Errorf("expected limited violations due to cancellation, got %d", len(violations))
	}
}

func TestCrossContextChecker_Priority(t *testing.T) {
	// Verify priority mapping exists and is correct
	priority := ViolationTypeToPriority(ViolationCrossContextConfusion)
	if priority != PriorityCrossContextConfusion {
		t.Errorf("expected priority %d, got %d", PriorityCrossContextConfusion, priority)
	}
	if priority != 2 {
		t.Errorf("expected priority 2 (high), got %d", priority)
	}
}

func TestExtractDirectory(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"pkg/server/config.go", "pkg/server"},
		{"config.go", ""},
		{"a/b/c/d.go", "a/b/c"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractDirectory(tt.path)
			if got != tt.want {
				t.Errorf("extractDirectory(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestExtractFilename(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"pkg/server/config.go", "config.go"},
		{"config.go", "config.go"},
		{"a/b/c/d.go", "d.go"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractFilename(tt.path)
			if got != tt.want {
				t.Errorf("extractFilename(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestFormatLocations(t *testing.T) {
	tests := []struct {
		locations []string
		want      string
	}{
		{[]string{}, "unknown"},
		{[]string{"a.go"}, "a.go"},
		{[]string{"a.go", "b.go"}, "a.go and b.go"},
		{[]string{"a.go", "b.go", "c.go"}, "a.go, b.go, and c.go"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatLocations(tt.locations)
			if got != tt.want {
				t.Errorf("formatLocations(%v) = %q, want %q", tt.locations, got, tt.want)
			}
		})
	}
}

func TestParseFieldList(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"Name, Timeout, MaxRetries", 3},
		{"Name", 1},
		{"Name Timeout MaxRetries", 1}, // Only captures first word
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseFieldList(tt.input)
			if len(got) != tt.want {
				t.Errorf("parseFieldList(%q) returned %d fields, want %d", tt.input, len(got), tt.want)
			}
		})
	}
}

func TestIsValidCrossContextIdentifier(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"Config", true},
		{"config", true},
		{"_config", true},
		{"Config123", true},
		{"123Config", false},
		{"", false},
		{"Config-Name", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isValidCrossContextIdentifier(tt.input)
			if got != tt.want {
				t.Errorf("isValidCrossContextIdentifier(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsBuiltinPackagePrefix(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"fmt", true},
		{"Fmt", true}, // Case insensitive
		{"strings", true},
		{"mypackage", false},
		{"utils", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isBuiltinPackagePrefix(tt.input)
			if got != tt.want {
				t.Errorf("isBuiltinPackagePrefix(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
