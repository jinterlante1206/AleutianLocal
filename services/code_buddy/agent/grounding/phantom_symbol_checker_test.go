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

func TestPhantomSymbolChecker_Name(t *testing.T) {
	checker := NewPhantomSymbolChecker(nil)
	if name := checker.Name(); name != "phantom_symbol_checker" {
		t.Errorf("Name() = %q, want %q", name, "phantom_symbol_checker")
	}
}

func TestPhantomSymbolChecker_FunctionReference_Exists(t *testing.T) {
	checker := NewPhantomSymbolChecker(nil)
	ctx := context.Background()

	knownSymbols := map[string]bool{
		"ValidateUserToken": true,
		"ProcessRequest":    true,
		"HandleError":       true,
	}

	tests := []struct {
		name          string
		response      string
		wantViolation bool
	}{
		{
			name:          "function with backticks exists",
			response:      "The `ValidateUserToken()` function handles authentication",
			wantViolation: false,
		},
		{
			name:          "function call pattern exists",
			response:      "The system calls ProcessRequest to handle incoming data",
			wantViolation: false,
		},
		{
			name:          "function keyword pattern exists",
			response:      "The function HandleError handles all error cases",
			wantViolation: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response:     tt.response,
				KnownSymbols: knownSymbols,
			}
			violations := checker.Check(ctx, input)

			if tt.wantViolation && len(violations) == 0 {
				t.Error("expected violation but got none")
			}
			if !tt.wantViolation && len(violations) > 0 {
				t.Errorf("unexpected violation: %+v", violations[0])
			}
		})
	}
}

func TestPhantomSymbolChecker_FunctionReference_Missing(t *testing.T) {
	checker := NewPhantomSymbolChecker(nil)
	ctx := context.Background()

	knownSymbols := map[string]bool{
		"ExistingFunction": true,
	}

	tests := []struct {
		name         string
		response     string
		wantEvidence string
	}{
		{
			name:         "backtick function doesn't exist",
			response:     "The `ValidateUserToken()` function handles authentication",
			wantEvidence: "ValidateUserToken",
		},
		{
			name:         "calls pattern function doesn't exist",
			response:     "The code calls ProcessData to transform input",
			wantEvidence: "ProcessData",
		},
		{
			name:         "function keyword pattern doesn't exist",
			response:     "function HandleWebhook processes events",
			wantEvidence: "HandleWebhook",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response:     tt.response,
				KnownSymbols: knownSymbols,
			}
			violations := checker.Check(ctx, input)

			if len(violations) == 0 {
				t.Fatal("expected violation for non-existent function")
			}

			v := violations[0]
			if v.Type != ViolationPhantomSymbol {
				t.Errorf("type = %v, want %v", v.Type, ViolationPhantomSymbol)
			}
			if v.Evidence != tt.wantEvidence {
				t.Errorf("evidence = %q, want %q", v.Evidence, tt.wantEvidence)
			}
		})
	}
}

func TestPhantomSymbolChecker_TypeReference_Exists(t *testing.T) {
	checker := NewPhantomSymbolChecker(nil)
	ctx := context.Background()

	knownSymbols := map[string]bool{
		"UserConfig":   true,
		"ServerOption": true,
		"Database":     true,
	}

	tests := []struct {
		name          string
		response      string
		wantViolation bool
	}{
		{
			name:          "struct reference exists",
			response:      "The `UserConfig` struct contains all settings",
			wantViolation: false,
		},
		{
			name:          "type keyword exists",
			response:      "type ServerOption configures the server",
			wantViolation: false,
		},
		{
			name:          "implements pattern exists",
			response:      "This class implements Database",
			wantViolation: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response:     tt.response,
				KnownSymbols: knownSymbols,
			}
			violations := checker.Check(ctx, input)

			if tt.wantViolation && len(violations) == 0 {
				t.Error("expected violation but got none")
			}
			if !tt.wantViolation && len(violations) > 0 {
				t.Errorf("unexpected violation: %+v", violations[0])
			}
		})
	}
}

func TestPhantomSymbolChecker_TypeReference_Missing(t *testing.T) {
	checker := NewPhantomSymbolChecker(nil)
	ctx := context.Background()

	knownSymbols := map[string]bool{
		"RealType": true,
	}

	tests := []struct {
		name         string
		response     string
		wantEvidence string
	}{
		{
			name:         "struct doesn't exist",
			response:     "The `FakeConfig` struct handles configuration",
			wantEvidence: "FakeConfig",
		},
		{
			name:         "type keyword doesn't exist",
			response:     "type NonExistent is the main interface",
			wantEvidence: "NonExistent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response:     tt.response,
				KnownSymbols: knownSymbols,
			}
			violations := checker.Check(ctx, input)

			if len(violations) == 0 {
				t.Fatal("expected violation for non-existent type")
			}

			v := violations[0]
			if v.Type != ViolationPhantomSymbol {
				t.Errorf("type = %v, want %v", v.Type, ViolationPhantomSymbol)
			}
			if v.Evidence != tt.wantEvidence {
				t.Errorf("evidence = %q, want %q", v.Evidence, tt.wantEvidence)
			}
		})
	}
}

func TestPhantomSymbolChecker_WithFileAssociation(t *testing.T) {
	config := &PhantomSymbolCheckerConfig{
		Enabled:                true,
		RequireFileAssociation: true,
		MinSymbolLength:        3,
		MaxSymbolsToCheck:      100,
		IgnoredSymbols:         []string{},
	}
	checker := NewPhantomSymbolChecker(config)
	ctx := context.Background()

	// Symbol exists in auth.go but not in server.go
	symbolDetails := map[string][]SymbolInfo{
		"ValidateToken": {
			{Name: "ValidateToken", Kind: "function", File: "auth.go", Line: 10},
		},
	}

	evidenceIndex := &EvidenceIndex{
		SymbolDetails: symbolDetails,
	}

	tests := []struct {
		name          string
		response      string
		wantViolation bool
	}{
		{
			name:          "symbol in correct file - no violation",
			response:      "The `ValidateToken()` function in auth.go handles tokens",
			wantViolation: false,
		},
		{
			name:          "symbol in wrong file - violation",
			response:      "The `ValidateToken()` function in server.go handles tokens",
			wantViolation: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response:      tt.response,
				EvidenceIndex: evidenceIndex,
			}
			violations := checker.Check(ctx, input)

			if tt.wantViolation && len(violations) == 0 {
				t.Error("expected violation but got none")
			}
			if !tt.wantViolation && len(violations) > 0 {
				t.Errorf("unexpected violation: %+v", violations[0])
			}
		})
	}
}

func TestPhantomSymbolChecker_GlobalSearch(t *testing.T) {
	checker := NewPhantomSymbolChecker(nil)
	ctx := context.Background()

	knownSymbols := map[string]bool{
		"GlobalFunction": true,
	}

	tests := []struct {
		name          string
		response      string
		wantViolation bool
		wantSeverity  Severity
	}{
		{
			name:          "global symbol exists - no violation",
			response:      "The `GlobalFunction()` handles requests",
			wantViolation: false,
		},
		{
			name:          "global symbol missing - high severity (no file context)",
			response:      "The `MissingFunction()` handles errors",
			wantViolation: true,
			wantSeverity:  SeverityHigh,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response:     tt.response,
				KnownSymbols: knownSymbols,
			}
			violations := checker.Check(ctx, input)

			if tt.wantViolation && len(violations) == 0 {
				t.Error("expected violation but got none")
			}
			if !tt.wantViolation && len(violations) > 0 {
				t.Errorf("unexpected violation: %+v", violations[0])
			}
			if tt.wantViolation && len(violations) > 0 && violations[0].Severity != tt.wantSeverity {
				t.Errorf("severity = %v, want %v", violations[0].Severity, tt.wantSeverity)
			}
		})
	}
}

func TestPhantomSymbolChecker_MinSymbolLength(t *testing.T) {
	config := &PhantomSymbolCheckerConfig{
		Enabled:           true,
		MinSymbolLength:   5, // Higher threshold
		MaxSymbolsToCheck: 100,
		IgnoredSymbols:    []string{},
	}
	checker := NewPhantomSymbolChecker(config)
	ctx := context.Background()

	input := &CheckInput{
		Response:     "The `Foo()` function is short", // "Foo" is only 3 chars
		KnownSymbols: map[string]bool{"Other": true},
	}
	violations := checker.Check(ctx, input)

	// Should not flag "Foo" because it's below MinSymbolLength
	if len(violations) > 0 {
		t.Errorf("expected no violation for short symbol, got: %+v", violations[0])
	}
}

func TestPhantomSymbolChecker_IgnoredSymbols(t *testing.T) {
	checker := NewPhantomSymbolChecker(nil) // Default config ignores Context, Error, etc.
	ctx := context.Background()

	input := &CheckInput{
		Response: "Uses context.Context and returns Error",
		KnownSymbols: map[string]bool{
			"SomeOther": true,
		},
	}
	violations := checker.Check(ctx, input)

	// Should not flag ignored symbols
	for _, v := range violations {
		if v.Evidence == "Context" || v.Evidence == "Error" {
			t.Errorf("should not flag ignored symbol: %s", v.Evidence)
		}
	}
}

func TestPhantomSymbolChecker_Disabled(t *testing.T) {
	config := &PhantomSymbolCheckerConfig{
		Enabled: false,
	}
	checker := NewPhantomSymbolChecker(config)
	ctx := context.Background()

	input := &CheckInput{
		Response:     "The `FakeFunction()` handles everything",
		KnownSymbols: map[string]bool{"Real": true},
	}
	violations := checker.Check(ctx, input)

	if len(violations) > 0 {
		t.Error("expected no violations when checker is disabled")
	}
}

func TestPhantomSymbolChecker_NoSymbolData(t *testing.T) {
	checker := NewPhantomSymbolChecker(nil)
	ctx := context.Background()

	tests := []struct {
		name         string
		knownSymbols map[string]bool
		evidence     *EvidenceIndex
	}{
		{"nil KnownSymbols", nil, nil},
		{"empty KnownSymbols", map[string]bool{}, nil},
		{"nil EvidenceIndex", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response:      "The `SomeFunction()` does work",
				KnownSymbols:  tt.knownSymbols,
				EvidenceIndex: tt.evidence,
			}
			violations := checker.Check(ctx, input)

			// Should return nil without symbol data (can't validate)
			if violations != nil {
				t.Errorf("expected nil violations without symbol data, got: %+v", violations)
			}
		})
	}
}

func TestPhantomSymbolChecker_ViolationPriority(t *testing.T) {
	checker := NewPhantomSymbolChecker(nil)
	ctx := context.Background()

	input := &CheckInput{
		Response:     "The `FakeFunc()` function does work",
		KnownSymbols: map[string]bool{"Real": true},
	}
	violations := checker.Check(ctx, input)

	if len(violations) == 0 {
		t.Fatal("expected violation")
	}

	priority := violations[0].Priority()
	if priority != PriorityPhantomSymbol {
		t.Errorf("violation priority = %v, want %v", priority, PriorityPhantomSymbol)
	}
}

func TestPhantomSymbolChecker_ContextCancellation(t *testing.T) {
	checker := NewPhantomSymbolChecker(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	input := &CheckInput{
		Response: `Multiple symbols:
The FakeFunc1() function
The FakeFunc2() function
The FakeFunc3() function`,
		KnownSymbols: map[string]bool{"Real": true},
	}

	// Should return early without panicking
	violations := checker.Check(ctx, input)
	_ = violations
}

func TestPhantomSymbolChecker_MaxSymbolsToCheck(t *testing.T) {
	config := &PhantomSymbolCheckerConfig{
		Enabled:           true,
		MinSymbolLength:   3,
		MaxSymbolsToCheck: 2, // Only check 2 symbols
		IgnoredSymbols:    []string{},
	}
	checker := NewPhantomSymbolChecker(config)
	ctx := context.Background()

	input := &CheckInput{
		Response: `Symbols:
The FakeFunc1() function
The FakeFunc2() function
The FakeFunc3() function
The FakeFunc4() function
The FakeFunc5() function`,
		KnownSymbols: map[string]bool{"Real": true},
	}

	violations := checker.Check(ctx, input)

	// Should be limited by MaxSymbolsToCheck
	if len(violations) > 2 {
		t.Errorf("expected at most 2 violations (MaxSymbolsToCheck), got %d", len(violations))
	}
}

func TestPhantomSymbolChecker_MethodReference(t *testing.T) {
	checker := NewPhantomSymbolChecker(nil)
	ctx := context.Background()

	knownSymbols := map[string]bool{
		"Server":   true,
		"Start":    true,
		"Database": true,
		"Connect":  true,
	}

	tests := []struct {
		name          string
		response      string
		wantViolation bool
	}{
		{
			name:          "method on known type - no violation",
			response:      "Call Server.Start() to begin",
			wantViolation: false,
		},
		{
			name:          "method on known type with pointer - no violation",
			response:      "Call (*Database).Connect() to connect",
			wantViolation: false,
		},
		{
			name:          "method on unknown type - violation",
			response:      "Call UnknownType.DoWork() to process",
			wantViolation: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response:     tt.response,
				KnownSymbols: knownSymbols,
			}
			violations := checker.Check(ctx, input)

			if tt.wantViolation && len(violations) == 0 {
				t.Error("expected violation but got none")
			}
			if !tt.wantViolation && len(violations) > 0 {
				t.Errorf("unexpected violation: %+v", violations[0])
			}
		})
	}
}

func TestPhantomSymbolChecker_SymbolInDifferentFile(t *testing.T) {
	// Symbol exists but in a different file than claimed
	config := &PhantomSymbolCheckerConfig{
		Enabled:                true,
		RequireFileAssociation: true,
		MinSymbolLength:        3,
		MaxSymbolsToCheck:      100,
		IgnoredSymbols:         []string{},
	}
	checker := NewPhantomSymbolChecker(config)
	ctx := context.Background()

	symbolDetails := map[string][]SymbolInfo{
		"ProcessData": {
			{Name: "ProcessData", Kind: "function", File: "pkg/processor/processor.go", Line: 25},
		},
	}

	input := &CheckInput{
		Response: "The `ProcessData()` function in pkg/handler/handler.go transforms input",
		EvidenceIndex: &EvidenceIndex{
			SymbolDetails: symbolDetails,
		},
	}

	violations := checker.Check(ctx, input)

	if len(violations) == 0 {
		t.Fatal("expected violation for symbol in wrong file")
	}

	v := violations[0]
	if v.Type != ViolationPhantomSymbol {
		t.Errorf("type = %v, want %v", v.Type, ViolationPhantomSymbol)
	}
}

func TestPhantomSymbolChecker_ExtractSymbolReferences(t *testing.T) {
	checker := NewPhantomSymbolChecker(nil)

	tests := []struct {
		name         string
		response     string
		wantContains []string
		wantMinCount int
	}{
		{
			name:         "backtick function",
			response:     "The `HandleRequest()` function",
			wantContains: []string{"HandleRequest"},
			wantMinCount: 1,
		},
		{
			name:         "calls pattern",
			response:     "calls ProcessData to transform",
			wantContains: []string{"ProcessData"},
			wantMinCount: 1,
		},
		{
			name:         "type struct",
			response:     "The `UserConfig` struct",
			wantContains: []string{"UserConfig"},
			wantMinCount: 1,
		},
		{
			name:         "method call",
			response:     "Call Database.Connect() to begin",
			wantContains: []string{"Database", "Connect"},
			wantMinCount: 2,
		},
		{
			name:         "no symbols",
			response:     "This response has no symbol references.",
			wantMinCount: 0,
		},
		{
			name:         "deduplicates",
			response:     "The `Foo()` function and also Foo() again",
			wantContains: []string{"Foo"},
			wantMinCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs := checker.extractSymbolReferences(tt.response)

			if len(refs) < tt.wantMinCount {
				t.Errorf("extractSymbolReferences() returned %d refs, want at least %d", len(refs), tt.wantMinCount)
			}

			for _, want := range tt.wantContains {
				found := false
				for _, ref := range refs {
					if ref.Name == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected ref %q not found in %+v", want, refs)
				}
			}
		})
	}
}

func TestPhantomSymbolChecker_UsesEvidenceIndexSymbols(t *testing.T) {
	// Test that checker works with EvidenceIndex.Symbols (simple map)
	checker := NewPhantomSymbolChecker(nil)
	ctx := context.Background()

	evidenceIndex := &EvidenceIndex{
		Symbols: map[string]bool{
			"ExistingFunc": true,
			"ExistingType": true,
		},
	}

	tests := []struct {
		name          string
		response      string
		wantViolation bool
	}{
		{
			name:          "symbol in EvidenceIndex.Symbols - no violation",
			response:      "The `ExistingFunc()` function handles requests",
			wantViolation: false,
		},
		{
			name:          "symbol not in EvidenceIndex.Symbols - violation",
			response:      "The `MissingFunc()` function processes data",
			wantViolation: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response:      tt.response,
				EvidenceIndex: evidenceIndex,
			}
			violations := checker.Check(ctx, input)

			if tt.wantViolation && len(violations) == 0 {
				t.Error("expected violation but got none")
			}
			if !tt.wantViolation && len(violations) > 0 {
				t.Errorf("unexpected violation: %+v", violations[0])
			}
		})
	}
}

func TestPhantomSymbolChecker_GoKeywordsIgnored(t *testing.T) {
	config := &PhantomSymbolCheckerConfig{
		Enabled:           true,
		MinSymbolLength:   2, // Low to catch short keywords
		MaxSymbolsToCheck: 100,
		IgnoredSymbols:    []string{},
	}
	checker := NewPhantomSymbolChecker(config)
	ctx := context.Background()

	// Response mentions Go keywords
	input := &CheckInput{
		Response: "The function uses if, for, and return statements",
		KnownSymbols: map[string]bool{
			"SomeFunc": true,
		},
	}

	violations := checker.Check(ctx, input)

	// Should not flag Go keywords
	for _, v := range violations {
		if v.Evidence == "if" || v.Evidence == "for" || v.Evidence == "return" {
			t.Errorf("should not flag Go keyword: %s", v.Evidence)
		}
	}
}

func TestPhantomSymbolChecker_StandardLibraryTypes(t *testing.T) {
	// Standard library types like Context, Error, etc. should be ignored
	checker := NewPhantomSymbolChecker(nil)
	ctx := context.Background()

	input := &CheckInput{
		Response: `The function takes a Context parameter and may return an Error.
It uses time.Duration for timeouts and bytes.Buffer for buffering.`,
		KnownSymbols: map[string]bool{
			"SomeFunc": true, // Only this exists
		},
	}

	violations := checker.Check(ctx, input)

	// Should not flag standard library types (Context, Error, Duration, Buffer)
	for _, v := range violations {
		ignored := []string{"Context", "Error", "Duration", "Buffer"}
		for _, ig := range ignored {
			if v.Evidence == ig {
				t.Errorf("should not flag standard library type: %s", v.Evidence)
			}
		}
	}
}

func TestPhantomSymbolChecker_LowercaseSymbolsNotMatched(t *testing.T) {
	// Unexported symbols (lowercase first letter) should not be matched
	// because LLM responses typically reference exported symbols
	config := &PhantomSymbolCheckerConfig{
		Enabled:           true,
		MinSymbolLength:   2,
		MaxSymbolsToCheck: 100,
		IgnoredSymbols:    []string{},
	}
	checker := NewPhantomSymbolChecker(config)
	ctx := context.Background()

	input := &CheckInput{
		Response: "The internal helper function in the code",
		KnownSymbols: map[string]bool{
			"ExportedFunc": true,
		},
	}

	violations := checker.Check(ctx, input)

	// Should not flag "internal" or "helper" as they are lowercase
	for _, v := range violations {
		if v.Evidence == "internal" || v.Evidence == "helper" || v.Evidence == "function" {
			t.Errorf("should not match lowercase word: %s", v.Evidence)
		}
	}
}

func TestPhantomSymbolChecker_MultipleViolations(t *testing.T) {
	checker := NewPhantomSymbolChecker(nil)
	ctx := context.Background()

	input := &CheckInput{
		Response: "The system uses:\n" +
			"- The `FakeHandler()` function for HTTP handling\n" +
			"- The `NonExistentProcessor` struct for data processing\n" +
			"- It calls MissingValidator to check inputs",
		KnownSymbols: map[string]bool{
			"RealFunc": true,
		},
	}

	violations := checker.Check(ctx, input)

	// Should have multiple violations
	if len(violations) < 2 {
		t.Errorf("expected at least 2 violations, got %d", len(violations))
	}

	// All should be phantom symbol violations
	for _, v := range violations {
		if v.Type != ViolationPhantomSymbol {
			t.Errorf("expected ViolationPhantomSymbol, got %v", v.Type)
		}
	}
}

func TestPhantomSymbolChecker_FileContextExtraction(t *testing.T) {
	checker := NewPhantomSymbolChecker(nil)

	tests := []struct {
		name        string
		response    string
		wantFile    string
		wantHasFile bool
		symbolName  string
	}{
		{
			name:        "in file.go pattern",
			response:    "The `HandleRequest()` function in handler.go processes requests",
			wantFile:    "handler.go",
			wantHasFile: true,
			symbolName:  "HandleRequest",
		},
		{
			name:        "at path pattern",
			response:    "See `ProcessData()` at pkg/processor.go",
			wantFile:    "pkg/processor.go",
			wantHasFile: true,
			symbolName:  "ProcessData",
		},
		{
			name:        "from file pattern",
			response:    "Imported from utils.py",
			wantFile:    "utils.py",
			wantHasFile: true,
			symbolName:  "",
		},
		{
			name:        "no file context",
			response:    "The `SomeFunc()` function does work",
			wantHasFile: false,
			symbolName:  "SomeFunc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs := checker.extractSymbolReferences(tt.response)

			if tt.symbolName != "" {
				var found bool
				for _, ref := range refs {
					if ref.Name == tt.symbolName {
						found = true
						if tt.wantHasFile && ref.File == "" {
							t.Errorf("expected file context for %s, got none", tt.symbolName)
						}
						if tt.wantHasFile && ref.File != "" && ref.File != tt.wantFile {
							t.Errorf("file = %q, want %q", ref.File, tt.wantFile)
						}
						break
					}
				}
				if !found && tt.symbolName != "" {
					t.Errorf("symbol %s not found in refs", tt.symbolName)
				}
			}
		})
	}
}
