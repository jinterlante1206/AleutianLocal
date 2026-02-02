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

func TestPhantomFileChecker_Name(t *testing.T) {
	checker := NewPhantomFileChecker(nil)
	if name := checker.Name(); name != "phantom_file_checker" {
		t.Errorf("Name() = %q, want %q", name, "phantom_file_checker")
	}
}

func TestPhantomFileChecker_NonExistentFile(t *testing.T) {
	checker := NewPhantomFileChecker(nil)
	ctx := context.Background()

	knownFiles := map[string]bool{
		"main.go":        true,
		"pkg/server.go":  true,
		"internal/db.go": true,
	}

	tests := []struct {
		name          string
		response      string
		wantViolation bool
		wantEvidence  string
	}{
		{
			name:          "references non-existent file",
			response:      "The handler is in pkg/handler/handler.go",
			wantViolation: true,
			wantEvidence:  "pkg/handler/handler.go",
		},
		{
			name:          "references non-existent test file",
			response:      "Tests are in `pkg/server/server_test.go`",
			wantViolation: true,
			wantEvidence:  "pkg/server/server_test.go",
		},
		{
			name:          "references existing file - no violation",
			response:      "The main entry point is in main.go",
			wantViolation: false,
		},
		{
			name:          "references existing nested file - no violation",
			response:      "See the server code in pkg/server.go",
			wantViolation: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response:   tt.response,
				KnownFiles: knownFiles,
			}
			violations := checker.Check(ctx, input)

			if tt.wantViolation && len(violations) == 0 {
				t.Error("expected violation for non-existent file reference")
			}
			if !tt.wantViolation && len(violations) > 0 {
				t.Errorf("unexpected violation: %+v", violations[0])
			}

			if tt.wantViolation && len(violations) > 0 {
				v := violations[0]
				if v.Type != ViolationPhantomFile {
					t.Errorf("violation type = %v, want %v", v.Type, ViolationPhantomFile)
				}
				if v.Severity != SeverityCritical {
					t.Errorf("severity = %v, want %v", v.Severity, SeverityCritical)
				}
				if v.Evidence != tt.wantEvidence {
					t.Errorf("evidence = %q, want %q", v.Evidence, tt.wantEvidence)
				}
			}
		})
	}
}

func TestPhantomFileChecker_ExistingFile(t *testing.T) {
	checker := NewPhantomFileChecker(nil)
	ctx := context.Background()

	knownFiles := map[string]bool{
		"main.go":                     true,
		"pkg/server/server.go":        true,
		"internal/handler/handler.go": true,
		"README.md":                   true,
	}

	tests := []struct {
		name     string
		response string
	}{
		{
			name:     "exact path match",
			response: "Check out the code in pkg/server/server.go",
		},
		{
			name:     "basename match",
			response: "The main function is in main.go",
		},
		{
			name:     "with leading ./",
			response: "Look at ./main.go for the entry point",
		},
		{
			name:     "in backticks",
			response: "The handler is implemented in `internal/handler/handler.go`",
		},
		{
			name:     "markdown file",
			response: "See the README.md for documentation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response:   tt.response,
				KnownFiles: knownFiles,
			}
			violations := checker.Check(ctx, input)

			if len(violations) > 0 {
				t.Errorf("expected no violations for existing file, got: %+v", violations[0])
			}
		})
	}
}

func TestPhantomFileChecker_EarlyExitNoFileRefs(t *testing.T) {
	checker := NewPhantomFileChecker(nil)
	ctx := context.Background()

	tests := []struct {
		name     string
		response string
	}{
		{
			name:     "no file references",
			response: "The server handles HTTP requests and returns JSON responses.",
		},
		{
			name:     "code without file paths",
			response: "```go\nfunc main() {\n    fmt.Println(\"hello\")\n}\n```",
		},
		{
			name:     "mentions a function",
			response: "The handleRequest function processes incoming requests.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response:   tt.response,
				KnownFiles: map[string]bool{"main.go": true},
			}
			violations := checker.Check(ctx, input)

			if len(violations) > 0 {
				t.Errorf("expected no violations for response without file refs, got: %+v", violations)
			}
		})
	}
}

func TestPhantomFileChecker_MultiplePhantomFiles(t *testing.T) {
	checker := NewPhantomFileChecker(nil)
	ctx := context.Background()

	input := &CheckInput{
		Response: `The project structure is:
- main.go (entry point)
- pkg/handler/handler.go (request handling)
- pkg/models/user.go (data models)
- config/config.go (configuration)`,
		KnownFiles: map[string]bool{
			"main.go": true,
			// All others don't exist
		},
	}

	violations := checker.Check(ctx, input)

	// Should have multiple violations
	if len(violations) < 2 {
		t.Errorf("expected at least 2 violations, got %d", len(violations))
	}

	// All should be phantom file violations
	for _, v := range violations {
		if v.Type != ViolationPhantomFile {
			t.Errorf("expected ViolationPhantomFile, got %v", v.Type)
		}
	}
}

func TestPhantomFileChecker_NormalizedPaths(t *testing.T) {
	checker := NewPhantomFileChecker(nil)
	ctx := context.Background()

	knownFiles := map[string]bool{
		"pkg/server/server.go": true,
	}

	tests := []struct {
		name          string
		response      string
		wantViolation bool
	}{
		{
			name:          "forward slashes - match",
			response:      "In pkg/server/server.go",
			wantViolation: false,
		},
		{
			name:          "leading ./ - match",
			response:      "In ./pkg/server/server.go",
			wantViolation: false,
		},
		{
			name:          "partial path suffix match",
			response:      "Check server.go for the implementation",
			wantViolation: false, // basename match
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response:   tt.response,
				KnownFiles: knownFiles,
			}
			violations := checker.Check(ctx, input)

			if tt.wantViolation && len(violations) == 0 {
				t.Error("expected violation")
			}
			if !tt.wantViolation && len(violations) > 0 {
				t.Errorf("unexpected violation: %+v", violations[0])
			}
		})
	}
}

func TestPhantomFileChecker_Test15Scenario(t *testing.T) {
	// This test reproduces the exact scenario from Test 15 that motivated this checker
	checker := NewPhantomFileChecker(nil)
	ctx := context.Background()

	// The actual project has these files
	knownFiles := map[string]bool{
		"main.go":        true,
		"pkg/api/api.go": true,
		// Note: pkg/server/server_test.go does NOT exist
	}

	// The LLM claimed this:
	input := &CheckInput{
		Response:   `The tests are in pkg/server/server_test.go which tests the server functionality.`,
		KnownFiles: knownFiles,
	}

	violations := checker.Check(ctx, input)

	if len(violations) == 0 {
		t.Fatal("Test 15 scenario should have been caught - phantom file reference")
	}

	v := violations[0]
	if v.Type != ViolationPhantomFile {
		t.Errorf("expected ViolationPhantomFile, got %v", v.Type)
	}
	if v.Severity != SeverityCritical {
		t.Errorf("expected SeverityCritical, got %v", v.Severity)
	}
	if v.Evidence != "pkg/server/server_test.go" {
		t.Errorf("evidence = %q, want %q", v.Evidence, "pkg/server/server_test.go")
	}
}

func TestPhantomFileChecker_Disabled(t *testing.T) {
	config := &PhantomCheckerConfig{
		Enabled: false,
	}
	checker := NewPhantomFileChecker(config)
	ctx := context.Background()

	input := &CheckInput{
		Response:   "Check pkg/fake/fake.go for details",
		KnownFiles: map[string]bool{"main.go": true},
	}

	violations := checker.Check(ctx, input)
	if len(violations) > 0 {
		t.Error("expected no violations when checker is disabled")
	}
}

func TestPhantomFileChecker_NoKnownFiles(t *testing.T) {
	checker := NewPhantomFileChecker(nil)
	ctx := context.Background()

	tests := []struct {
		name       string
		knownFiles map[string]bool
	}{
		{"nil KnownFiles", nil},
		{"empty KnownFiles", map[string]bool{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response:   "Check pkg/server/server.go",
				KnownFiles: tt.knownFiles,
			}
			violations := checker.Check(ctx, input)

			// Should return nil without KnownFiles (can't validate)
			if violations != nil {
				t.Errorf("expected nil violations without KnownFiles, got: %+v", violations)
			}
		})
	}
}

func TestPhantomFileChecker_ViolationPriority(t *testing.T) {
	checker := NewPhantomFileChecker(nil)
	ctx := context.Background()

	input := &CheckInput{
		Response:   "See pkg/fake.go for details",
		KnownFiles: map[string]bool{"main.go": true},
	}

	violations := checker.Check(ctx, input)
	if len(violations) == 0 {
		t.Fatal("expected violation")
	}

	// Verify the violation has correct priority
	priority := violations[0].Priority()
	if priority != PriorityPhantomFile {
		t.Errorf("violation priority = %v, want %v", priority, PriorityPhantomFile)
	}
}

func TestPhantomFileChecker_ContextCancellation(t *testing.T) {
	checker := NewPhantomFileChecker(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	input := &CheckInput{
		Response: `Multiple files:
- pkg/a.go
- pkg/b.go
- pkg/c.go`,
		KnownFiles: map[string]bool{"main.go": true},
	}

	// Should return early without panicking
	violations := checker.Check(ctx, input)
	_ = violations
}

func TestPhantomFileChecker_MaxRefsToCheck(t *testing.T) {
	config := &PhantomCheckerConfig{
		Enabled:        true,
		MaxRefsToCheck: 2,
	}
	checker := NewPhantomFileChecker(config)
	ctx := context.Background()

	input := &CheckInput{
		Response: `Files:
- pkg/a.go
- pkg/b.go
- pkg/c.go
- pkg/d.go
- pkg/e.go`,
		KnownFiles: map[string]bool{"main.go": true},
	}

	violations := checker.Check(ctx, input)

	// Should be limited by MaxRefsToCheck
	if len(violations) > 2 {
		t.Errorf("expected at most 2 violations (MaxRefsToCheck), got %d", len(violations))
	}
}

func TestPhantomFileChecker_ExtractFileReferences(t *testing.T) {
	checker := NewPhantomFileChecker(nil)

	tests := []struct {
		name         string
		response     string
		wantContains []string
		wantMinCount int
	}{
		{
			name:         "path in prose",
			response:     "The handler is in pkg/handler/handler.go",
			wantContains: []string{"pkg/handler/handler.go"},
			wantMinCount: 1,
		},
		{
			name:         "path in backticks",
			response:     "Check `internal/db/db.go` for database code",
			wantContains: []string{"internal/db/db.go"},
			wantMinCount: 1,
		},
		{
			name:         "multiple paths",
			response:     "See main.go and pkg/server.go",
			wantContains: []string{"main.go", "pkg/server.go"},
			wantMinCount: 2,
		},
		{
			name:         "in file pattern",
			response:     "The code in file utils.go handles this",
			wantContains: []string{"utils.go"},
			wantMinCount: 1,
		},
		{
			name:         "no paths",
			response:     "This function handles HTTP requests.",
			wantMinCount: 0,
		},
		{
			name:         "deduplicates",
			response:     "See main.go and also main.go again",
			wantContains: []string{"main.go"},
			wantMinCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs := checker.extractFileReferences(tt.response)

			if len(refs) < tt.wantMinCount {
				t.Errorf("extractFileReferences() returned %d refs, want at least %d", len(refs), tt.wantMinCount)
			}

			for _, want := range tt.wantContains {
				found := false
				for _, ref := range refs {
					if ref == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected ref %q not found in %v", want, refs)
				}
			}
		})
	}
}
