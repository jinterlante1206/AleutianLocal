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

func TestAPILibraryChecker_Name(t *testing.T) {
	checker := NewAPILibraryChecker(nil)
	if checker.Name() != "api_library_checker" {
		t.Errorf("expected name 'api_library_checker', got %q", checker.Name())
	}
}

func TestAPILibraryChecker_DisabledReturnsNil(t *testing.T) {
	config := DefaultAPILibraryCheckerConfig()
	config.Enabled = false
	checker := NewAPILibraryChecker(config)

	input := &CheckInput{
		Response: "The code uses gorm for database operations",
		EvidenceIndex: &EvidenceIndex{
			Imports: map[string][]ImportInfo{
				"main.go": {{Path: "github.com/jmoiron/sqlx"}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations when disabled, got %d", len(violations))
	}
}

func TestAPILibraryChecker_NilInputReturnsNil(t *testing.T) {
	checker := NewAPILibraryChecker(nil)

	if violations := checker.Check(context.Background(), nil); len(violations) != 0 {
		t.Error("expected nil for nil input")
	}

	if violations := checker.Check(context.Background(), &CheckInput{}); len(violations) != 0 {
		t.Error("expected nil for empty response")
	}
}

func TestAPILibraryChecker_NoImportsReturnsNil(t *testing.T) {
	checker := NewAPILibraryChecker(nil)

	input := &CheckInput{
		Response:      "The code uses gorm for database operations",
		EvidenceIndex: &EvidenceIndex{},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations without imports, got %d", len(violations))
	}
}

func TestAPILibraryChecker_LibraryExists(t *testing.T) {
	checker := NewAPILibraryChecker(nil)

	input := &CheckInput{
		Response: "The code uses sqlx for database operations",
		EvidenceIndex: &EvidenceIndex{
			Imports: map[string][]ImportInfo{
				"main.go": {{Path: "github.com/jmoiron/sqlx"}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations when library exists, got %d", len(violations))
	}
}

func TestAPILibraryChecker_LibraryMissing(t *testing.T) {
	checker := NewAPILibraryChecker(nil)

	input := &CheckInput{
		Response: "The code uses redis for caching",
		EvidenceIndex: &EvidenceIndex{
			Imports: map[string][]ImportInfo{
				"main.go": {{Path: "github.com/jmoiron/sqlx"}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Should detect missing library
	found := false
	for _, v := range violations {
		if v.Type == ViolationAPIHallucination && v.Code == "API_LIBRARY_NOT_IMPORTED" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected violation for missing library, got %d violations: %v", len(violations), violations)
	}
}

func TestAPILibraryChecker_WrongLibrary(t *testing.T) {
	checker := NewAPILibraryChecker(nil)

	// Response claims gorm but evidence shows sqlx
	input := &CheckInput{
		Response: "The project uses gorm.Open() for database connections",
		EvidenceIndex: &EvidenceIndex{
			Imports: map[string][]ImportInfo{
				"main.go": {{Path: "github.com/jmoiron/sqlx"}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Should detect library confusion
	found := false
	for _, v := range violations {
		if v.Type == ViolationAPIHallucination && v.Code == "API_LIBRARY_CONFUSION" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected confusion violation, got %d violations: %v", len(violations), violations)
	}
}

func TestAPILibraryChecker_APICallExists(t *testing.T) {
	checker := NewAPILibraryChecker(nil)

	input := &CheckInput{
		Response: "The code calls sqlx.Open() for database connections",
		EvidenceIndex: &EvidenceIndex{
			Imports: map[string][]ImportInfo{
				"main.go": {{Path: "github.com/jmoiron/sqlx"}},
			},
			FileContents: map[string]string{
				"main.go": `package main
import "github.com/jmoiron/sqlx"
func main() {
    db, err := sqlx.Open("postgres", connStr)
}`,
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations when API call exists, got %d", len(violations))
	}
}

func TestAPILibraryChecker_APICallFabricated(t *testing.T) {
	checker := NewAPILibraryChecker(nil)

	input := &CheckInput{
		Response: "The code calls sqlx.Connect() for database connections",
		EvidenceIndex: &EvidenceIndex{
			Imports: map[string][]ImportInfo{
				"main.go": {{Path: "github.com/jmoiron/sqlx"}},
			},
			FileContents: map[string]string{
				"main.go": `package main
import "github.com/jmoiron/sqlx"
func main() {
    db, err := sqlx.Open("postgres", connStr)
}`,
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Should detect API call not in evidence
	found := false
	for _, v := range violations {
		if v.Type == ViolationAPIHallucination && v.Code == "API_CALL_NOT_IN_EVIDENCE" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected API call not found violation, got %d violations: %v", len(violations), violations)
	}
}

func TestAPILibraryChecker_CommonConfusion_GormSqlx(t *testing.T) {
	checker := NewAPILibraryChecker(nil)

	testCases := []struct {
		name       string
		response   string
		imports    []ImportInfo
		expectCode string
		expectViol bool
	}{
		{
			name:       "claims gorm, has sqlx",
			response:   "The code uses gorm for ORM",
			imports:    []ImportInfo{{Path: "github.com/jmoiron/sqlx"}},
			expectCode: "API_LIBRARY_CONFUSION",
			expectViol: true,
		},
		{
			name:       "claims sqlx, has gorm",
			response:   "The code uses sqlx for database",
			imports:    []ImportInfo{{Path: "gorm.io/gorm"}},
			expectCode: "API_LIBRARY_CONFUSION",
			expectViol: true,
		},
		{
			name:       "claims gorm, has gorm",
			response:   "The code uses gorm for ORM",
			imports:    []ImportInfo{{Path: "gorm.io/gorm"}},
			expectCode: "",
			expectViol: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			input := &CheckInput{
				Response: tc.response,
				EvidenceIndex: &EvidenceIndex{
					Imports: map[string][]ImportInfo{
						"main.go": tc.imports,
					},
				},
			}

			violations := checker.Check(context.Background(), input)

			if tc.expectViol {
				found := false
				for _, v := range violations {
					if v.Code == tc.expectCode {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected %s violation, got %v", tc.expectCode, violations)
				}
			} else {
				if len(violations) != 0 {
					t.Errorf("expected no violations, got %d: %v", len(violations), violations)
				}
			}
		})
	}
}

func TestAPILibraryChecker_FromGoMod(t *testing.T) {
	checker := NewAPILibraryChecker(nil)

	// Response mentions gin, evidence shows gin is imported
	input := &CheckInput{
		Response: "The HTTP server uses gin framework",
		EvidenceIndex: &EvidenceIndex{
			Imports: map[string][]ImportInfo{
				"main.go": {
					{Path: "github.com/gin-gonic/gin"},
				},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations when gin is imported, got %d", len(violations))
	}
}

func TestAPILibraryChecker_NoImportEvidence(t *testing.T) {
	checker := NewAPILibraryChecker(nil)

	input := &CheckInput{
		Response: "The code uses gorm for database",
		EvidenceIndex: &EvidenceIndex{
			Imports: map[string][]ImportInfo{}, // Empty imports
		},
	}

	violations := checker.Check(context.Background(), input)
	// Should return no violations when we can't validate
	if len(violations) != 0 {
		t.Errorf("expected no violations with empty imports, got %d", len(violations))
	}
}

func TestAPILibraryChecker_AliasedImport(t *testing.T) {
	checker := NewAPILibraryChecker(nil)

	input := &CheckInput{
		Response: "The code uses sqldb.Open() for database connections",
		EvidenceIndex: &EvidenceIndex{
			Imports: map[string][]ImportInfo{
				"main.go": {
					{Path: "database/sql", Alias: "sqldb"},
				},
			},
			FileContents: map[string]string{
				"main.go": `import sqldb "database/sql"
func main() {
    db, err := sqldb.Open("postgres", connStr)
}`,
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations for aliased import, got %d: %v", len(violations), violations)
	}
}

func TestAPILibraryChecker_VersionedModule(t *testing.T) {
	checker := NewAPILibraryChecker(nil)

	input := &CheckInput{
		Response: "The project uses gorm for ORM",
		EvidenceIndex: &EvidenceIndex{
			Imports: map[string][]ImportInfo{
				"main.go": {
					{Path: "gorm.io/gorm/v2"},
				},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Should recognize gorm even with version suffix
	if len(violations) != 0 {
		t.Errorf("expected no violations for versioned module, got %d: %v", len(violations), violations)
	}
}

func TestAPILibraryChecker_SameLanguageConfusion(t *testing.T) {
	checker := NewAPILibraryChecker(nil)

	testCases := []struct {
		name       string
		response   string
		imports    []ImportInfo
		shouldFlag bool
	}{
		{
			name:       "gin vs echo confusion",
			response:   "The server uses gin.New() for routing",
			imports:    []ImportInfo{{Path: "github.com/labstack/echo/v4"}},
			shouldFlag: true,
		},
		{
			name:       "logrus vs zap confusion",
			response:   "Logging uses logrus.Info() calls",
			imports:    []ImportInfo{{Path: "go.uber.org/zap"}},
			shouldFlag: true,
		},
		{
			name:       "correct library",
			response:   "Logging uses zap.NewProduction()",
			imports:    []ImportInfo{{Path: "go.uber.org/zap"}},
			shouldFlag: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			input := &CheckInput{
				Response: tc.response,
				EvidenceIndex: &EvidenceIndex{
					Imports: map[string][]ImportInfo{
						"main.go": tc.imports,
					},
				},
			}

			violations := checker.Check(context.Background(), input)

			if tc.shouldFlag {
				if len(violations) == 0 {
					t.Error("expected violation for library confusion")
				}
			} else {
				if len(violations) != 0 {
					t.Errorf("expected no violations, got %d: %v", len(violations), violations)
				}
			}
		})
	}
}

func TestExtractLibraryName(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"github.com/gin-gonic/gin", "gin"},
		{"github.com/jmoiron/sqlx", "sqlx"},
		{"gorm.io/gorm", "gorm"},
		{"gorm.io/gorm/v2", "gorm"},
		{"database/sql", "sql"},
		{"net/http", "http"},
		{"go.uber.org/zap", "zap"},
		{"github.com/labstack/echo/v4", "echo"},
		{"example.com/pkg/v10", "pkg"},         // multi-digit version
		{"example.com/lib/v123", "lib"},        // larger multi-digit version
		{"example.com/viper/subpkg", "subpkg"}, // viper is not a version
		{"", ""},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			result := extractLibraryName(tc.path)
			if result != tc.expected {
				t.Errorf("extractLibraryName(%q) = %q, expected %q", tc.path, result, tc.expected)
			}
		})
	}
}

func TestLibraryClaimType_String(t *testing.T) {
	tests := []struct {
		c        LibraryClaimType
		expected string
	}{
		{ClaimLibraryUsage, "library_usage"},
		{ClaimAPICall, "api_call"},
		{LibraryClaimType(99), "unknown"},
	}

	for _, tc := range tests {
		if tc.c.String() != tc.expected {
			t.Errorf("%v.String() = %q, expected %q", tc.c, tc.c.String(), tc.expected)
		}
	}
}

func TestIsCommonWord(t *testing.T) {
	tests := []struct {
		word     string
		expected bool
	}{
		{"the", true},
		{"function", true},
		{"gorm", false},
		{"gin", false},
		{"http", true}, // http is common in prose
		{"sqlx", false},
	}

	for _, tc := range tests {
		t.Run(tc.word, func(t *testing.T) {
			result := isCommonLibraryWord(tc.word)
			if result != tc.expected {
				t.Errorf("isCommonLibraryWord(%q) = %v, expected %v", tc.word, result, tc.expected)
			}
		})
	}
}

func TestIsBuiltinPackage(t *testing.T) {
	tests := []struct {
		pkg      string
		expected bool
	}{
		{"fmt", true},
		{"strings", true},
		{"context", true},
		{"gorm", false},
		{"gin", false},
		{"sqlx", false},
	}

	for _, tc := range tests {
		t.Run(tc.pkg, func(t *testing.T) {
			result := isBuiltinPackage(tc.pkg)
			if result != tc.expected {
				t.Errorf("isBuiltinPackage(%q) = %v, expected %v", tc.pkg, result, tc.expected)
			}
		})
	}
}

func TestAPILibraryChecker_ContextCancellation(t *testing.T) {
	checker := NewAPILibraryChecker(nil)

	input := &CheckInput{
		Response: "uses gorm, uses sqlx, uses redis, uses postgres",
		EvidenceIndex: &EvidenceIndex{
			Imports: map[string][]ImportInfo{
				"main.go": {{Path: "github.com/other/lib"}},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	violations := checker.Check(ctx, input)
	// Should return early due to cancelled context
	t.Logf("Got %d violations with cancelled context", len(violations))
}

func TestAPILibraryChecker_MaxClaimsLimit(t *testing.T) {
	config := DefaultAPILibraryCheckerConfig()
	config.MaxClaimsToCheck = 2
	checker := NewAPILibraryChecker(config)

	// Response with many library claims
	input := &CheckInput{
		Response: "uses lib1, uses lib2, uses lib3, uses lib4, uses lib5",
		EvidenceIndex: &EvidenceIndex{
			Imports: map[string][]ImportInfo{
				"main.go": {{Path: "github.com/other/pkg"}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	// Should only check first 2 claims
	if len(violations) > 2 {
		t.Errorf("expected at most 2 violations (MaxClaimsToCheck), got %d", len(violations))
	}
}

func TestAPILibraryChecker_MultipleImportFiles(t *testing.T) {
	checker := NewAPILibraryChecker(nil)

	input := &CheckInput{
		Response: "The project uses gin for HTTP and gorm for database",
		EvidenceIndex: &EvidenceIndex{
			Imports: map[string][]ImportInfo{
				"server.go": {{Path: "github.com/gin-gonic/gin"}},
				"db.go":     {{Path: "gorm.io/gorm"}},
			},
		},
	}

	violations := checker.Check(context.Background(), input)
	if len(violations) != 0 {
		t.Errorf("expected no violations when libraries are in different files, got %d: %v",
			len(violations), violations)
	}
}
