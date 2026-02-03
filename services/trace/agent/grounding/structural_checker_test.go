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

func TestStructuralClaimChecker_Name(t *testing.T) {
	checker := NewStructuralClaimChecker(nil)
	if name := checker.Name(); name != "structural_claim_checker" {
		t.Errorf("Name() = %q, want %q", name, "structural_claim_checker")
	}
}

func TestStructuralClaimChecker_UnicodeTreeMarkers(t *testing.T) {
	checker := NewStructuralClaimChecker(nil)
	ctx := context.Background()

	tests := []struct {
		name          string
		response      string
		wantViolation bool
	}{
		{
			name: "unicode tree with box drawing chars",
			response: `The project structure is:
├── main.go
├── config/
│   └── config.go
└── handler/
    └── handler.go`,
			wantViolation: true,
		},
		{
			name: "unicode tree without supporting evidence",
			response: `
├── cmd/
│   └── server/
│       └── main.go
└── internal/
    ├── api/
    └── models/`,
			wantViolation: true,
		},
		{
			name: "simple unicode markers",
			response: `Files:
├── app.go
└── util.go`,
			wantViolation: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response:    tt.response,
				ToolResults: nil, // No tool evidence
			}
			violations := checker.Check(ctx, input)

			if tt.wantViolation && len(violations) == 0 {
				t.Error("expected violation for unicode tree without evidence")
			}
			if !tt.wantViolation && len(violations) > 0 {
				t.Errorf("unexpected violation: %+v", violations[0])
			}

			if len(violations) > 0 {
				if violations[0].Type != ViolationStructuralClaim {
					t.Errorf("violation type = %v, want %v", violations[0].Type, ViolationStructuralClaim)
				}
				if violations[0].Severity != SeverityHigh {
					t.Errorf("severity = %v, want %v", violations[0].Severity, SeverityHigh)
				}
			}
		})
	}
}

func TestStructuralClaimChecker_ASCIITreeMarkers(t *testing.T) {
	checker := NewStructuralClaimChecker(nil)
	ctx := context.Background()

	tests := []struct {
		name          string
		response      string
		wantViolation bool
	}{
		{
			name: "ASCII tree with plus markers",
			response: `Project layout:
+-- main.go
+-- config/
|   +-- config.go
+-- handler/
    +-- handler.go`,
			wantViolation: true,
		},
		{
			name: "ASCII tree with backtick",
			response: `Structure:
+-- src/
|   +-- app.go
` + "`-- tests/",
			wantViolation: true,
		},
		{
			name: "pipe-based tree",
			response: `
|-- cmd/
|   |-- main.go
|-- pkg/
    |-- util.go`,
			wantViolation: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response:    tt.response,
				ToolResults: nil,
			}
			violations := checker.Check(ctx, input)

			if tt.wantViolation && len(violations) == 0 {
				t.Error("expected violation for ASCII tree without evidence")
			}
			if !tt.wantViolation && len(violations) > 0 {
				t.Errorf("unexpected violation: %+v", violations[0])
			}
		})
	}
}

func TestStructuralClaimChecker_WithLsCitation(t *testing.T) {
	checker := NewStructuralClaimChecker(nil)
	ctx := context.Background()

	t.Run("has ls output in tool results", func(t *testing.T) {
		input := &CheckInput{
			Response: `Based on the ls output, the project structure is:
├── main.go
├── config/
└── handler/`,
			ToolResults: []ToolResult{
				{
					InvocationID: "1",
					Output:       "$ ls -la\nmain.go\nconfig/\nhandler/\n",
				},
			},
		}

		violations := checker.Check(ctx, input)
		if len(violations) > 0 {
			t.Errorf("expected no violation when ls evidence exists, got: %+v", violations[0])
		}
	})

	t.Run("mentions ls in response", func(t *testing.T) {
		input := &CheckInput{
			Response: `From ls output:
├── main.go
└── util.go`,
			ToolResults: nil,
		}

		violations := checker.Check(ctx, input)
		if len(violations) > 0 {
			t.Errorf("expected no violation when ls is cited in response, got: %+v", violations[0])
		}
	})
}

func TestStructuralClaimChecker_WithToolEvidence(t *testing.T) {
	checker := NewStructuralClaimChecker(nil)
	ctx := context.Background()

	t.Run("find command output", func(t *testing.T) {
		input := &CheckInput{
			Response: `The project has:
├── cmd/main.go
└── pkg/util.go`,
			ToolResults: []ToolResult{
				{
					InvocationID: "1",
					Output:       "$ find . -name '*.go'\n./cmd/main.go\n./pkg/util.go\n",
				},
			},
		}

		violations := checker.Check(ctx, input)
		if len(violations) > 0 {
			t.Errorf("expected no violation with find evidence, got: %+v", violations[0])
		}
	})

	t.Run("tree command output", func(t *testing.T) {
		input := &CheckInput{
			Response: `Structure:
├── main.go
└── lib/`,
			ToolResults: []ToolResult{
				{
					InvocationID: "1",
					Output:       "$ tree\nmain.go\nlib/\n",
				},
			},
		}

		violations := checker.Check(ctx, input)
		if len(violations) > 0 {
			t.Errorf("expected no violation with tree evidence, got: %+v", violations[0])
		}
	})

	t.Run("evidence index has files", func(t *testing.T) {
		input := &CheckInput{
			Response: `The project contains:
├── main.go
├── config.go
└── handler.go`,
			EvidenceIndex: &EvidenceIndex{
				Files: map[string]bool{
					"main.go":    true,
					"config.go":  true,
					"handler.go": true,
				},
			},
		}

		violations := checker.Check(ctx, input)
		if len(violations) > 0 {
			t.Errorf("expected no violation when evidence index has files, got: %+v", violations[0])
		}
	})
}

func TestStructuralClaimChecker_NoStructuralClaims(t *testing.T) {
	checker := NewStructuralClaimChecker(nil)
	ctx := context.Background()

	tests := []struct {
		name     string
		response string
	}{
		{
			name:     "simple code explanation",
			response: "The main function initializes the server and starts listening on port 8080.",
		},
		{
			name:     "code block without tree",
			response: "```go\nfunc main() {\n    fmt.Println(\"hello\")\n}\n```",
		},
		{
			name:     "mentions a single file",
			response: "The configuration is in config.go which handles environment variables.",
		},
		{
			name:     "path in explanation",
			response: "You can find the handler at internal/api/handler.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &CheckInput{
				Response: tt.response,
			}
			violations := checker.Check(ctx, input)

			if len(violations) > 0 {
				t.Errorf("expected no violation for non-structural response, got: %+v", violations[0])
			}
		})
	}
}

func TestStructuralClaimChecker_DirectoryListPattern(t *testing.T) {
	checker := NewStructuralClaimChecker(nil)
	ctx := context.Background()

	t.Run("multiple paths in list without evidence", func(t *testing.T) {
		input := &CheckInput{
			Response: `The directory structure is:

- cmd/
- pkg/
- internal/
- main.go
- go.mod`,
			ToolResults: nil,
		}

		violations := checker.Check(ctx, input)
		if len(violations) == 0 {
			t.Error("expected violation for path list without evidence")
		}
	})

	t.Run("structural phrase with path list", func(t *testing.T) {
		input := &CheckInput{
			Response: `The project layout includes:
config/
handler/
model/
database/`,
			ToolResults: nil,
		}

		violations := checker.Check(ctx, input)
		if len(violations) == 0 {
			t.Error("expected violation for structural phrase with paths")
		}
	})
}

func TestStructuralClaimChecker_Disabled(t *testing.T) {
	config := &StructuralClaimCheckerConfig{
		Enabled: false,
	}
	checker := NewStructuralClaimChecker(config)
	ctx := context.Background()

	input := &CheckInput{
		Response: `├── main.go
└── config/`,
	}

	violations := checker.Check(ctx, input)
	if len(violations) > 0 {
		t.Error("expected no violations when checker is disabled")
	}
}

func TestStructuralClaimChecker_ContextCancellation(t *testing.T) {
	checker := NewStructuralClaimChecker(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	input := &CheckInput{
		Response: `├── main.go
└── config/`,
	}

	// Should return early without panicking
	violations := checker.Check(ctx, input)
	// May or may not have violations depending on when cancellation is checked
	_ = violations
}

func TestStructuralClaimChecker_ExtractClaimedPaths(t *testing.T) {
	checker := NewStructuralClaimChecker(nil)

	tests := []struct {
		name         string
		response     string
		wantPaths    []string
		wantMinCount int
	}{
		{
			name: "unicode tree",
			response: `├── main.go
├── config/
└── handler.go`,
			wantPaths:    []string{"main.go", "config/", "handler.go"},
			wantMinCount: 3,
		},
		{
			name: "ascii tree",
			response: `+-- app.go
+-- lib/
    +-- util.go`,
			wantMinCount: 2,
		},
		{
			name:         "no tree",
			response:     "Just some text about the code.",
			wantMinCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := checker.extractClaimedPaths(tt.response)

			if len(paths) < tt.wantMinCount {
				t.Errorf("extractClaimedPaths() returned %d paths, want at least %d", len(paths), tt.wantMinCount)
			}

			if tt.wantPaths != nil {
				for _, want := range tt.wantPaths {
					found := false
					for _, got := range paths {
						if got == want {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("expected path %q not found in %v", want, paths)
					}
				}
			}
		})
	}
}

func TestStructuralClaimChecker_ViolationPriority(t *testing.T) {
	checker := NewStructuralClaimChecker(nil)
	ctx := context.Background()

	input := &CheckInput{
		Response: `├── main.go
└── config/`,
	}

	violations := checker.Check(ctx, input)
	if len(violations) == 0 {
		t.Fatal("expected violation")
	}

	// Verify the violation has correct priority
	priority := violations[0].Priority()
	if priority != PriorityStructuralClaim {
		t.Errorf("violation priority = %v, want %v", priority, PriorityStructuralClaim)
	}
}

func TestStructuralClaimChecker_LooksLikeDirectoryListing(t *testing.T) {
	checker := NewStructuralClaimChecker(nil)

	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "file paths",
			output: "cmd/main.go\npkg/util.go\ninternal/handler.go",
			want:   true,
		},
		{
			name:   "mixed content",
			output: "main.go\nconfig.go\nREADME.md",
			want:   true,
		},
		{
			name:   "not a listing",
			output: "Error: file not found\nPlease check the path",
			want:   false,
		},
		{
			name:   "single file",
			output: "main.go",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checker.looksLikeDirectoryListing(tt.output)
			if got != tt.want {
				t.Errorf("looksLikeDirectoryListing() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStructuralClaimChecker_Test7Scenario(t *testing.T) {
	// This test reproduces the exact scenario from Test 7 that motivated this checker
	checker := NewStructuralClaimChecker(nil)
	ctx := context.Background()

	input := &CheckInput{
		Response: `The project directory structure is:
+-- main.go
+-- config/
|   +-- config.go
+-- handler/
|   +-- handler.go
+-- model/
|   +-- model.go
+-- database/
    +-- database.go`,
		ToolResults: nil, // No tool evidence - this is the hallucination
		KnownFiles: map[string]bool{
			"main.go": true,
			// Note: config/, handler/, model/, database/ do NOT exist
		},
	}

	violations := checker.Check(ctx, input)

	if len(violations) == 0 {
		t.Fatal("Test 7 scenario should have been caught - structural claim without evidence")
	}

	v := violations[0]
	if v.Type != ViolationStructuralClaim {
		t.Errorf("expected ViolationStructuralClaim, got %v", v.Type)
	}
	if v.Severity != SeverityHigh {
		t.Errorf("expected SeverityHigh, got %v", v.Severity)
	}
	if v.Code != "STRUCTURAL_CLAIM_NO_EVIDENCE" {
		t.Errorf("expected code STRUCTURAL_CLAIM_NO_EVIDENCE, got %v", v.Code)
	}
}
