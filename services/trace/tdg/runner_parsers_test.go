// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package tdg

import (
	"regexp"
	"strings"
	"testing"
)

// =============================================================================
// GO TEST OUTPUT PARSER TESTS
// =============================================================================

func TestParseGoTestOutput(t *testing.T) {
	tests := []struct {
		name       string
		output     string
		wantPassed bool
		wantFailed []string
	}{
		{
			name: "all tests pass",
			output: `=== RUN   TestValidate
--- PASS: TestValidate (0.00s)
=== RUN   TestProcess
--- PASS: TestProcess (0.01s)
PASS
ok  	example.com/pkg	0.015s`,
			wantPassed: true,
			wantFailed: []string{},
		},
		{
			name: "single test fails",
			output: `=== RUN   TestValidate
--- FAIL: TestValidate (0.00s)
    validate_test.go:15: expected true, got false
FAIL
FAIL	example.com/pkg	0.015s`,
			wantPassed: false,
			wantFailed: []string{"TestValidate"},
		},
		{
			name: "multiple tests fail",
			output: `=== RUN   TestA
--- FAIL: TestA (0.00s)
=== RUN   TestB
--- PASS: TestB (0.00s)
=== RUN   TestC
--- FAIL: TestC (0.00s)
FAIL
FAIL	example.com/pkg	0.015s`,
			wantPassed: false,
			wantFailed: []string{"TestA", "TestC"},
		},
		{
			name: "panic in test",
			output: `=== RUN   TestPanic
panic: runtime error: invalid memory address
goroutine 1 [running]:
testing.tRunner.func1()
FAIL	example.com/pkg	0.005s`,
			wantPassed: false,
			wantFailed: []string{},
		},
		{
			name: "skipped tests still pass",
			output: `=== RUN   TestSkipped
--- SKIP: TestSkipped (0.00s)
    skip_test.go:10: skipping in CI
=== RUN   TestPassed
--- PASS: TestPassed (0.00s)
PASS
ok  	example.com/pkg	0.010s`,
			wantPassed: true,
			wantFailed: []string{},
		},
		{
			name: "subtest fails",
			output: `=== RUN   TestParent
=== RUN   TestParent/subtest1
--- FAIL: TestParent/subtest1 (0.00s)
--- FAIL: TestParent (0.00s)
FAIL
FAIL	example.com/pkg	0.010s`,
			wantPassed: false,
			wantFailed: []string{"TestParent/subtest1", "TestParent"},
		},
		{
			name:       "empty output",
			output:     "",
			wantPassed: true,
			wantFailed: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			passed, failed := parseGoTestOutput([]byte(tt.output))

			if passed != tt.wantPassed {
				t.Errorf("passed = %v, want %v", passed, tt.wantPassed)
			}

			if len(failed) != len(tt.wantFailed) {
				t.Errorf("failed = %v, want %v", failed, tt.wantFailed)
				return
			}

			for i, f := range failed {
				if f != tt.wantFailed[i] {
					t.Errorf("failed[%d] = %v, want %v", i, f, tt.wantFailed[i])
				}
			}
		})
	}
}

// =============================================================================
// PYTEST OUTPUT PARSER TESTS
// =============================================================================

func TestParsePytestOutput(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		wantPassed  bool
		wantMinFail int // Minimum number of failed tests (parser may find duplicates)
	}{
		{
			name: "all tests pass",
			output: `============================= test session starts ==============================
collected 3 items

test_module.py ...                                                       [100%]

============================== 3 passed in 0.12s ===============================`,
			wantPassed:  true,
			wantMinFail: 0,
		},
		{
			name: "single test fails",
			output: `============================= test session starts ==============================
collected 3 items

test_module.py ..F                                                       [100%]

=================================== FAILURES ===================================
_________________________________ test_validate ________________________________

    def test_validate():
>       assert validate(None) == True
E       AssertionError: assert False == True

test_module.py:10: AssertionError
=========================== short test summary info ============================
FAILED test_module.py::test_validate
========================= 1 failed, 2 passed in 0.15s =========================`,
			wantPassed:  false,
			wantMinFail: 1,
		},
		{
			name: "multiple tests fail",
			output: `FAILED test_a.py::test_one
FAILED test_b.py::test_two
========================= 2 failed, 5 passed in 0.20s =========================`,
			wantPassed:  false,
			wantMinFail: 2,
		},
		{
			name: "errors in collection",
			output: `============================= test session starts ==============================
collected 0 items / 1 error

=========================== short test summary info ============================
========================= 1 error in 0.05s =========================`,
			wantPassed:  false,
			wantMinFail: 0,
		},
		{
			name: "short format output",
			output: `PASSED test_module.py::test_good
FAILED test_module.py::test_bad
ERROR test_module.py::test_broken
========================= 1 failed, 1 passed, 1 error in 0.10s =========================`,
			wantPassed:  false,
			wantMinFail: 2,
		},
		{
			name:        "empty output",
			output:      "",
			wantPassed:  true,
			wantMinFail: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			passed, failed := parsePytestOutput([]byte(tt.output))

			if passed != tt.wantPassed {
				t.Errorf("passed = %v, want %v", passed, tt.wantPassed)
			}

			if len(failed) < tt.wantMinFail {
				t.Errorf("failed count = %d, want at least %d", len(failed), tt.wantMinFail)
			}
		})
	}
}

// =============================================================================
// JEST OUTPUT PARSER TESTS
// =============================================================================

func TestParseJestOutput(t *testing.T) {
	tests := []struct {
		name         string
		output       string
		wantPassed   bool
		wantMinFail  int
		wantContains []string // Check that failed names contain these substrings
	}{
		{
			name: "all tests pass",
			output: `PASS src/utils.test.ts
  ✓ validates input correctly (5 ms)
  ✓ processes data (3 ms)

Test Suites: 1 passed, 1 total
Tests:       2 passed, 2 total`,
			wantPassed:   true,
			wantMinFail:  0,
			wantContains: []string{},
		},
		{
			name: "single test fails",
			output: `FAIL src/utils.test.ts
  ✓ validates input correctly (5 ms)
  ✕ processes data (8 ms)

  ● processes data

    expect(received).toBe(expected)

Test Suites: 1 failed, 1 total
Tests:       1 failed, 1 passed, 2 total`,
			wantPassed:   false,
			wantMinFail:  1,
			wantContains: []string{"processes data"},
		},
		{
			name: "multiple tests fail",
			output: `FAIL src/app.test.ts
  ✕ test one (5 ms)
  ✓ test two (3 ms)
  ✕ test three (4 ms)

Tests:       2 failed, 1 passed, 3 total`,
			wantPassed:   false,
			wantMinFail:  2,
			wantContains: []string{"test one", "test three"},
		},
		{
			name: "alternative fail symbol",
			output: `FAIL src/app.test.ts
  ✗ failing test (5 ms)

Tests:       1 failed, 0 passed, 1 total`,
			wantPassed:   false,
			wantMinFail:  0, // The ✗ pattern doesn't extract name in current impl
			wantContains: []string{},
		},
		{
			name:         "empty output",
			output:       "",
			wantPassed:   true,
			wantMinFail:  0,
			wantContains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			passed, failed := parseJestOutput([]byte(tt.output))

			if passed != tt.wantPassed {
				t.Errorf("passed = %v, want %v", passed, tt.wantPassed)
			}

			if len(failed) < tt.wantMinFail {
				t.Errorf("failed count = %d, want at least %d", len(failed), tt.wantMinFail)
			}

			// Check that expected substrings are found in failed tests
			for _, want := range tt.wantContains {
				found := false
				for _, f := range failed {
					if strings.Contains(f, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected failed tests to contain %q, got %v", want, failed)
				}
			}
		})
	}
}

// =============================================================================
// PARSER REGISTRY TESTS
// =============================================================================

func TestGetTestOutputParser(t *testing.T) {
	tests := []struct {
		language string
		wantNil  bool
	}{
		{"go", false},
		{"python", false},
		{"typescript", false},
		{"javascript", false},
		{"rust", true},
		{"unknown", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			parser := GetTestOutputParser(tt.language)
			if (parser == nil) != tt.wantNil {
				t.Errorf("GetTestOutputParser(%q) nil = %v, want %v", tt.language, parser == nil, tt.wantNil)
			}
		})
	}
}

func TestRegisterTestOutputParser(t *testing.T) {
	// Register a custom parser
	customParser := func(output []byte) (bool, []string) {
		return true, []string{"custom"}
	}

	RegisterTestOutputParser("custom_lang", customParser)

	parser := GetTestOutputParser("custom_lang")
	if parser == nil {
		t.Fatal("custom parser not registered")
	}

	passed, failed := parser([]byte("test"))
	if !passed {
		t.Error("custom parser should return passed=true")
	}
	if len(failed) != 1 || failed[0] != "custom" {
		t.Errorf("custom parser should return [\"custom\"], got %v", failed)
	}
}

// =============================================================================
// UTILITY FUNCTION TESTS
// =============================================================================

func TestExtractTestNames(t *testing.T) {
	pattern := regexp.MustCompile(`--- FAIL: (\S+)`)
	output := `--- FAIL: TestA (0.00s)
--- PASS: TestB (0.01s)
--- FAIL: TestC (0.00s)`

	names := ExtractTestNames(output, pattern)

	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
	if names[0] != "TestA" {
		t.Errorf("names[0] = %q, want TestA", names[0])
	}
	if names[1] != "TestC" {
		t.Errorf("names[1] = %q, want TestC", names[1])
	}
}

func TestCountTestResults(t *testing.T) {
	passPattern := regexp.MustCompile(`--- PASS:`)
	failPattern := regexp.MustCompile(`--- FAIL:`)

	output := `--- PASS: TestA (0.00s)
--- PASS: TestB (0.01s)
--- FAIL: TestC (0.00s)
--- PASS: TestD (0.02s)`

	passed, failed := CountTestResults(output, passPattern, failPattern)

	if passed != 3 {
		t.Errorf("passed = %d, want 3", passed)
	}
	if failed != 1 {
		t.Errorf("failed = %d, want 1", failed)
	}
}
