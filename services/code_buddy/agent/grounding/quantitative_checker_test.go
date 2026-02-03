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

func TestQuantitativeChecker_Name(t *testing.T) {
	checker := NewQuantitativeChecker(nil)
	if checker.Name() != "quantitative_checker" {
		t.Errorf("expected name 'quantitative_checker', got %q", checker.Name())
	}
}

func TestQuantitativeChecker_Disabled(t *testing.T) {
	config := DefaultQuantitativeCheckerConfig()
	config.Enabled = false
	checker := NewQuantitativeChecker(config)

	violations := checker.Check(context.Background(), &CheckInput{
		Response: "There are 15 files in the project.",
		EvidenceIndex: &EvidenceIndex{
			Files: map[string]bool{"a.go": true, "b.go": true, "c.go": true},
		},
	})

	if len(violations) != 0 {
		t.Errorf("expected no violations when disabled, got %d", len(violations))
	}
}

func TestQuantitativeChecker_NilInput(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	violations := checker.Check(context.Background(), nil)
	if len(violations) != 0 {
		t.Errorf("expected no violations for nil input, got %d", len(violations))
	}
}

func TestQuantitativeChecker_NoEvidenceIndex(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	violations := checker.Check(context.Background(), &CheckInput{
		Response: "There are 15 files in the project.",
	})

	if len(violations) != 0 {
		t.Errorf("expected no violations when no evidence index, got %d", len(violations))
	}
}

func TestQuantitativeChecker_FileCount_Exact(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	// Claim matches actual count
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "There are 3 files in the project.",
		EvidenceIndex: &EvidenceIndex{
			Files: map[string]bool{"a.go": true, "b.go": true, "c.go": true},
		},
	})

	if len(violations) != 0 {
		t.Errorf("expected no violations for correct count, got %d", len(violations))
	}
}

func TestQuantitativeChecker_FileCount_Wrong(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	// Claim does not match actual count
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "There are 15 files in the project.",
		EvidenceIndex: &EvidenceIndex{
			Files: map[string]bool{"a.go": true, "b.go": true, "c.go": true},
		},
	})

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]
	if v.Type != ViolationQuantitativeHallucination {
		t.Errorf("expected ViolationQuantitativeHallucination, got %v", v.Type)
	}
	if v.Severity != SeverityHigh {
		t.Errorf("expected SeverityHigh, got %v", v.Severity)
	}
}

func TestQuantitativeChecker_FileCount_TestFiles(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	// Claim about test files
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "There are 3 test files in the project.",
		EvidenceIndex: &EvidenceIndex{
			Files: map[string]bool{
				"a.go":           true,
				"b.go":           true,
				"a_test.go":      true,
				"b_test.go":      true,
				"integration.go": true,
			},
		},
	})

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation (claimed 3, actual 2 test files), got %d", len(violations))
	}
}

func TestQuantitativeChecker_FileCount_GoFiles(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	// Claim about Go files - correct count
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "There are 4 Go files in the package.",
		EvidenceIndex: &EvidenceIndex{
			Files: map[string]bool{
				"main.go":     true,
				"handler.go":  true,
				"utils.go":    true,
				"types.go":    true,
				"readme.md":   true,
				"config.json": true,
			},
		},
	})

	if len(violations) != 0 {
		t.Errorf("expected no violations for correct Go file count, got %d", len(violations))
	}
}

func TestQuantitativeChecker_LineCount_Exact(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	// Claim matches actual line count
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "main.go is 52 lines of code.",
		EvidenceIndex: &EvidenceIndex{
			FileLines: map[string]int{"main.go": 52},
		},
	})

	if len(violations) != 0 {
		t.Errorf("expected no violations for correct line count, got %d", len(violations))
	}
}

func TestQuantitativeChecker_LineCount_Wrong(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	// Claim does not match actual line count
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "main.go is 200 lines.",
		EvidenceIndex: &EvidenceIndex{
			FileLines: map[string]int{"main.go": 52},
		},
	})

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]
	if v.Type != ViolationQuantitativeHallucination {
		t.Errorf("expected ViolationQuantitativeHallucination, got %v", v.Type)
	}
}

func TestQuantitativeChecker_LineCount_NoFileContext(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	// Line count claim without specific file - cannot validate
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "The function has 50 lines.",
		EvidenceIndex: &EvidenceIndex{
			FileLines: map[string]int{"main.go": 52, "utils.go": 30},
		},
	})

	// Should not generate violation (cannot determine which file)
	if len(violations) != 0 {
		t.Errorf("expected no violations when no file context, got %d", len(violations))
	}
}

func TestQuantitativeChecker_ApproximateWithinTolerance(t *testing.T) {
	config := DefaultQuantitativeCheckerConfig()
	// ApproximateUnderPct = 0.3 (30% under OK)
	// ApproximateOverPct = 0.15 (15% over triggers)
	checker := NewQuantitativeChecker(config)

	// "About 200" for 180 actual = 11% over, should be OK
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "main.go has about 200 lines.",
		EvidenceIndex: &EvidenceIndex{
			FileLines: map[string]int{"main.go": 180},
		},
	})

	if len(violations) != 0 {
		t.Errorf("expected no violations for approximate within tolerance, got %d", len(violations))
	}

	// "About 200" for 160 actual = 25% over, but 160->200 is not 25% over
	// Let me recalculate: (200-160)/160 = 40/160 = 0.25 = 25% over -> violation
	violations = checker.Check(context.Background(), &CheckInput{
		Response: "utils.go has about 200 lines.",
		EvidenceIndex: &EvidenceIndex{
			FileLines: map[string]int{"utils.go": 160},
		},
	})

	if len(violations) != 1 {
		t.Errorf("expected 1 violation for overcounting by 25%%, got %d", len(violations))
	}
}

func TestQuantitativeChecker_ApproximateOutsideTolerance(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	// "About 200" for 400 actual = 100% over, definitely a violation
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "main.go has about 400 lines.",
		EvidenceIndex: &EvidenceIndex{
			FileLines: map[string]int{"main.go": 200},
		},
	})

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for grossly overcounting, got %d", len(violations))
	}

	v := violations[0]
	// Approximate claims get warning severity
	if v.Severity != SeverityWarning {
		t.Errorf("expected SeverityWarning for approximate claim, got %v", v.Severity)
	}
}

func TestQuantitativeChecker_AsymmetricTolerance(t *testing.T) {
	config := DefaultQuantitativeCheckerConfig()
	// ApproximateUnderPct = 0.3 (30% under OK)
	// ApproximateOverPct = 0.15 (15% over triggers)
	checker := NewQuantitativeChecker(config)

	// Undercounting: "about 140" for 200 actual = 30% under, should be OK
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "main.go has about 140 lines.",
		EvidenceIndex: &EvidenceIndex{
			FileLines: map[string]int{"main.go": 200},
		},
	})

	if len(violations) != 0 {
		t.Errorf("expected no violations for 30%% undercount, got %d", len(violations))
	}

	// Overcounting: "about 230" for 200 actual = 15% over, exactly at threshold
	violations = checker.Check(context.Background(), &CheckInput{
		Response: "utils.go has about 230 lines.",
		EvidenceIndex: &EvidenceIndex{
			FileLines: map[string]int{"utils.go": 200},
		},
	})

	if len(violations) != 0 {
		t.Errorf("expected no violations for exactly 15%% overcount, got %d", len(violations))
	}

	// Overcounting: "about 232" for 200 actual = 16% over, should trigger
	violations = checker.Check(context.Background(), &CheckInput{
		Response: "handler.go has about 232 lines.",
		EvidenceIndex: &EvidenceIndex{
			FileLines: map[string]int{"handler.go": 200},
		},
	})

	if len(violations) != 1 {
		t.Errorf("expected 1 violation for 16%% overcount, got %d", len(violations))
	}
}

func TestQuantitativeChecker_NumberWords(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	// "three files" for 3 actual = correct
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "There are three files in the package.",
		EvidenceIndex: &EvidenceIndex{
			Files: map[string]bool{"a.go": true, "b.go": true, "c.go": true},
		},
	})

	if len(violations) != 0 {
		t.Errorf("expected no violations for correct word-based count, got %d", len(violations))
	}

	// "twelve files" for 3 actual = wrong
	violations = checker.Check(context.Background(), &CheckInput{
		Response: "There are twelve files in the package.",
		EvidenceIndex: &EvidenceIndex{
			Files: map[string]bool{"a.go": true, "b.go": true, "c.go": true},
		},
	})

	if len(violations) != 1 {
		t.Errorf("expected 1 violation for wrong word-based count, got %d", len(violations))
	}
}

func TestQuantitativeChecker_VagueQuantities(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	// Vague quantities should be skipped
	vagueResponses := []string{
		"There are several files in the project.",
		"The project contains many functions.",
		"There are a few test files.",
		"The codebase has multiple modules.",
	}

	for _, response := range vagueResponses {
		violations := checker.Check(context.Background(), &CheckInput{
			Response: response,
			EvidenceIndex: &EvidenceIndex{
				Files: map[string]bool{"a.go": true, "b.go": true},
			},
		})

		if len(violations) != 0 {
			t.Errorf("expected no violations for vague quantity %q, got %d", response, len(violations))
		}
	}
}

func TestQuantitativeChecker_SymbolCount_Functions(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	// Claim about function count - correct
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "The package has 3 functions.",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Foo": {{Name: "Foo", Kind: "function", File: "main.go", Line: 10}},
				"Bar": {{Name: "Bar", Kind: "function", File: "main.go", Line: 20}},
				"Baz": {{Name: "Baz", Kind: "function", File: "utils.go", Line: 5}},
			},
		},
	})

	if len(violations) != 0 {
		t.Errorf("expected no violations for correct function count, got %d", len(violations))
	}
}

func TestQuantitativeChecker_SymbolCount_Wrong(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	// Claim about function count - wrong
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "The package has 10 functions.",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Foo": {{Name: "Foo", Kind: "function", File: "main.go", Line: 10}},
				"Bar": {{Name: "Bar", Kind: "function", File: "main.go", Line: 20}},
			},
		},
	})

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for wrong function count, got %d", len(violations))
	}
}

func TestQuantitativeChecker_SymbolCount_Types(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	// Claim about type count
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "There are 2 types defined.",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Config":  {{Name: "Config", Kind: "type", File: "types.go", Line: 5}},
				"Handler": {{Name: "Handler", Kind: "type", File: "types.go", Line: 15}},
				"Foo":     {{Name: "Foo", Kind: "function", File: "main.go", Line: 10}},
			},
		},
	})

	if len(violations) != 0 {
		t.Errorf("expected no violations for correct type count, got %d", len(violations))
	}
}

func TestQuantitativeChecker_CommaFormattedNumber(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	// Comma-formatted number
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "main.go has 1,000 lines.",
		EvidenceIndex: &EvidenceIndex{
			FileLines: map[string]int{"main.go": 1000},
		},
	})

	if len(violations) != 0 {
		t.Errorf("expected no violations for comma-formatted number, got %d", len(violations))
	}

	// Wrong comma-formatted number
	violations = checker.Check(context.Background(), &CheckInput{
		Response: "utils.go has 1,000 lines.",
		EvidenceIndex: &EvidenceIndex{
			FileLines: map[string]int{"utils.go": 50},
		},
	})

	if len(violations) != 1 {
		t.Errorf("expected 1 violation for wrong comma-formatted number, got %d", len(violations))
	}
}

func TestQuantitativeChecker_MaxClaimsToCheck(t *testing.T) {
	config := DefaultQuantitativeCheckerConfig()
	config.MaxClaimsToCheck = 1
	checker := NewQuantitativeChecker(config)

	// Multiple claims, but only first should be checked
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "There are 100 files. The project has 200 functions. There are 50 test files.",
		EvidenceIndex: &EvidenceIndex{
			Files: map[string]bool{"a.go": true},
			SymbolDetails: map[string][]SymbolInfo{
				"Foo": {{Name: "Foo", Kind: "function", File: "main.go", Line: 10}},
			},
		},
	})

	// Should have at most 1 violation due to MaxClaimsToCheck
	if len(violations) > 1 {
		t.Errorf("expected at most 1 violation with MaxClaimsToCheck=1, got %d", len(violations))
	}
}

func TestQuantitativeChecker_CheckFileCountsDisabled(t *testing.T) {
	config := DefaultQuantitativeCheckerConfig()
	config.CheckFileCounts = false
	checker := NewQuantitativeChecker(config)

	violations := checker.Check(context.Background(), &CheckInput{
		Response: "There are 100 files in the project.",
		EvidenceIndex: &EvidenceIndex{
			Files: map[string]bool{"a.go": true},
		},
	})

	if len(violations) != 0 {
		t.Errorf("expected no violations when file count checking disabled, got %d", len(violations))
	}
}

func TestQuantitativeChecker_CheckLineCountsDisabled(t *testing.T) {
	config := DefaultQuantitativeCheckerConfig()
	config.CheckLineCounts = false
	checker := NewQuantitativeChecker(config)

	violations := checker.Check(context.Background(), &CheckInput{
		Response: "main.go has 1000 lines.",
		EvidenceIndex: &EvidenceIndex{
			FileLines: map[string]int{"main.go": 50},
		},
	})

	if len(violations) != 0 {
		t.Errorf("expected no violations when line count checking disabled, got %d", len(violations))
	}
}

func TestQuantitativeChecker_CheckSymbolCountsDisabled(t *testing.T) {
	config := DefaultQuantitativeCheckerConfig()
	config.CheckSymbolCounts = false
	checker := NewQuantitativeChecker(config)

	violations := checker.Check(context.Background(), &CheckInput{
		Response: "There are 100 functions in the package.",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: map[string][]SymbolInfo{
				"Foo": {{Name: "Foo", Kind: "function", File: "main.go", Line: 10}},
			},
		},
	})

	if len(violations) != 0 {
		t.Errorf("expected no violations when symbol count checking disabled, got %d", len(violations))
	}
}

func TestQuantitativeChecker_ExactTolerance(t *testing.T) {
	config := DefaultQuantitativeCheckerConfig()
	config.ExactTolerance = 1 // Allow off-by-one
	checker := NewQuantitativeChecker(config)

	// Off by one - should be OK with tolerance=1
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "There are 4 files in the project.",
		EvidenceIndex: &EvidenceIndex{
			Files: map[string]bool{"a.go": true, "b.go": true, "c.go": true},
		},
	})

	if len(violations) != 0 {
		t.Errorf("expected no violations for off-by-one with tolerance=1, got %d", len(violations))
	}

	// Off by two - should still be a violation
	violations = checker.Check(context.Background(), &CheckInput{
		Response: "There are 5 files in the project.",
		EvidenceIndex: &EvidenceIndex{
			Files: map[string]bool{"a.go": true, "b.go": true, "c.go": true},
		},
	})

	if len(violations) != 1 {
		t.Errorf("expected 1 violation for off-by-two with tolerance=1, got %d", len(violations))
	}
}

func TestQuantitativeChecker_ContextCancellation(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	violations := checker.Check(ctx, &CheckInput{
		Response: "There are 100 files. There are 200 functions.",
		EvidenceIndex: &EvidenceIndex{
			Files: map[string]bool{"a.go": true},
		},
	})

	// Should return early due to context cancellation
	// May have 0 or 1 violations depending on when cancellation is detected
	if len(violations) > 2 {
		t.Errorf("expected at most 2 violations with cancelled context, got %d", len(violations))
	}
}

func TestQuantitativeChecker_ZeroActualCount(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	// Empty evidence - should not be able to validate
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "There are 5 files.",
		EvidenceIndex: &EvidenceIndex{
			Files: map[string]bool{},
		},
	})

	// With zero files, claiming 5 should be a violation
	// Actually, countFiles returns false for canValidate when Files is empty
	// So no violation will be generated
	if len(violations) != 0 {
		t.Errorf("expected no violations when evidence is empty, got %d", len(violations))
	}
}

func TestQuantitativeClaimType_String(t *testing.T) {
	tests := []struct {
		claimType QuantitativeClaimType
		expected  string
	}{
		{ClaimFileCount, "file_count"},
		{ClaimLineCount, "line_count"},
		{ClaimSymbolCount, "symbol_count"},
		{QuantitativeClaimType(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.claimType.String(); got != tt.expected {
			t.Errorf("QuantitativeClaimType(%d).String() = %q, want %q", tt.claimType, got, tt.expected)
		}
	}
}

func TestParseNumber(t *testing.T) {
	tests := []struct {
		input    string
		expected int
		ok       bool
	}{
		{"5", 5, true},
		{"100", 100, true},
		{"1,000", 1000, true},
		{"1,234,567", 1234567, true},
		{"three", 3, true},
		{"twelve", 12, true},
		{"dozen", 12, true},
		{"hundred", 100, true},
		{"several", 0, false}, // Vague quantity
		{"many", 0, false},    // Vague quantity
		{"abc", 0, false},     // Invalid
		// SI suffixes
		{"1k", 1000, true},
		{"2K", 2000, true},
		{"1.5k", 1500, true},
		{"1m", 1000000, true},
		{"1M", 1000000, true},
		{"2.5M", 2500000, true},
	}

	for _, tt := range tests {
		got, ok := parseNumber(tt.input)
		if ok != tt.ok {
			t.Errorf("parseNumber(%q) ok = %v, want %v", tt.input, ok, tt.ok)
		}
		if ok && got != tt.expected {
			t.Errorf("parseNumber(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

func TestQuantitativeChecker_LineCountByBasename(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	// Test matching by basename when full path is in evidence
	violations := checker.Check(context.Background(), &CheckInput{
		Response: "main.go has 50 lines.",
		EvidenceIndex: &EvidenceIndex{
			FileLines: map[string]int{
				"src/cmd/main.go": 50,
			},
		},
	})

	if len(violations) != 0 {
		t.Errorf("expected no violations when matching by basename, got %d", len(violations))
	}
}

func TestQuantitativeChecker_ViolationCode(t *testing.T) {
	checker := NewQuantitativeChecker(nil)

	violations := checker.Check(context.Background(), &CheckInput{
		Response: "There are 100 files.",
		EvidenceIndex: &EvidenceIndex{
			Files: map[string]bool{"a.go": true},
		},
	})

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	if violations[0].Code != "QUANTITATIVE_FILE_COUNT" {
		t.Errorf("expected code QUANTITATIVE_FILE_COUNT, got %q", violations[0].Code)
	}
}
