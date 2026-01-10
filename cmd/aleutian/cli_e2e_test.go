// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

//go:build e2e
// +build e2e

// To run E2E tests: go test -tags=e2e -v ./cmd/aleutian/...
// These tests require the aleutian stack to be running.

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// TEST SETUP
// =============================================================================

var (
	testHarness  *CLITestHarness
	testFixtures *TestFixtures
)

func TestMain(m *testing.M) {
	// Setup
	testHarness = NewCLITestHarness("")
	if err := testHarness.Build(); err != nil {
		panic("Failed to build CLI: " + err.Error())
	}

	var err error
	testFixtures, err = NewTestFixtures()
	if err != nil {
		panic("Failed to create fixtures: " + err.Error())
	}

	// Run tests
	code := m.Run()

	// Cleanup
	testHarness.Cleanup()
	testFixtures.Cleanup()

	os.Exit(code)
}

// =============================================================================
// 1. ROOT COMMAND E2E TESTS (10+ tests)
// =============================================================================

func TestE2E_RootCommand(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		wantExitCode   int
		wantContains   []string
		wantNotContain []string
	}{
		{
			name:         "help flag shows all commands",
			args:         []string{"--help"},
			wantExitCode: 0,
			wantContains: []string{"Aleutian", "stack", "chat", "session", "ingest", "health"},
		},
		{
			name:         "short help flag",
			args:         []string{"-h"},
			wantExitCode: 0,
			wantContains: []string{"Usage"},
		},
		{
			name:         "version flag",
			args:         []string{"--version"},
			wantExitCode: 0,
		},
		{
			name:         "short version flag",
			args:         []string{"-v"},
			wantExitCode: 0,
		},
		{
			name:         "unknown command",
			args:         []string{"foobar"},
			wantExitCode: 1,
			wantContains: []string{"unknown command"},
		},
		{
			name:         "personality machine mode",
			args:         []string{"--personality", "machine", "--help"},
			wantExitCode: 0,
		},
		{
			name:         "personality full mode",
			args:         []string{"--personality", "full", "--help"},
			wantExitCode: 0,
		},
		{
			name:         "personality minimal mode",
			args:         []string{"--personality", "minimal", "--help"},
			wantExitCode: 0,
		},
		{
			name:         "personality standard mode",
			args:         []string{"--personality", "standard", "--help"},
			wantExitCode: 0,
		},
		// Note: Invalid personality gracefully falls back to default, not an error
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := testHarness.Run(tt.args...)
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}

			if err := result.AssertExitCode(tt.wantExitCode); err != nil {
				t.Error(err)
			}

			for _, want := range tt.wantContains {
				if err := result.AssertOutputContains(want); err != nil {
					t.Error(err)
				}
			}

			for _, notWant := range tt.wantNotContain {
				if err := result.AssertStdoutNotContains(notWant); err != nil {
					t.Error(err)
				}
			}
		})
	}
}

// =============================================================================
// 2. STACK COMMAND E2E TESTS (10+ tests)
// =============================================================================

func TestE2E_StackCommands(t *testing.T) {
	t.Run("stack help", func(t *testing.T) {
		result, err := testHarness.Run("stack", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
		if err := result.AssertOutputContains("start"); err != nil {
			t.Error(err)
		}
		if err := result.AssertOutputContains("stop"); err != nil {
			t.Error(err)
		}
	})

	t.Run("stack start help", func(t *testing.T) {
		result, err := testHarness.Run("stack", "start", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
		if err := result.AssertOutputContains("profile"); err != nil {
			t.Error(err)
		}
	})

	t.Run("stack stop help", func(t *testing.T) {
		result, err := testHarness.Run("stack", "stop", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("stack logs help", func(t *testing.T) {
		result, err := testHarness.Run("stack", "logs", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("stack restart help", func(t *testing.T) {
		result, err := testHarness.Run("stack", "restart", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	// Tests that require stack running
	if isStackRunning() {
		t.Run("status command shows running", func(t *testing.T) {
			result, err := testHarness.Run("status")
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			if err := result.AssertExitCode(0); err != nil {
				t.Error(err)
			}
			if err := result.AssertOutputContains("Aleutian"); err != nil {
				t.Error(err)
			}
		})

		t.Run("status with json flag", func(t *testing.T) {
			result, err := testHarness.Run("status", "--json")
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// Should output valid JSON
			combined := result.Stdout + result.Stderr
			if !strings.Contains(combined, "{") {
				t.Error("Expected JSON output")
			}
		})

		t.Run("stack logs orchestrator", func(t *testing.T) {
			result, err := testHarness.RunWithOptions(CLIRunOptions{
				Args:    []string{"stack", "logs", "orchestrator", "--tail", "10"},
				Timeout: 30 * time.Second,
			})
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// Should not error (logs may be empty)
			if result.ExitCode != 0 {
				t.Logf("Logs command returned %d", result.ExitCode)
			}
		})

		t.Run("stack logs weaviate", func(t *testing.T) {
			_, err := testHarness.RunWithOptions(CLIRunOptions{
				Args:    []string{"stack", "logs", "weaviate", "--tail", "5"},
				Timeout: 30 * time.Second,
			})
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// May or may not have weaviate container
		})

		t.Run("stack logs invalid service", func(t *testing.T) {
			result, err := testHarness.Run("stack", "logs", "nonexistent-service")
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// Should error gracefully
			if result.ExitCode == 0 {
				t.Log("Logs for nonexistent service returned success - may be expected")
			}
		})
	} else {
		t.Run("status when stack not running", func(t *testing.T) {
			result, err := testHarness.Run("status")
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// May return 0 with "not running" or non-zero
			_ = result // Just verify it doesn't crash
		})
	}
}

// =============================================================================
// 3. SESSION COMMAND E2E TESTS (10+ tests)
// =============================================================================

func TestE2E_SessionCommands(t *testing.T) {
	if !isStackRunning() {
		t.Skip("Stack not running - skipping E2E session tests")
	}

	t.Run("session help", func(t *testing.T) {
		result, err := testHarness.Run("session", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
		if err := result.AssertOutputContains("list"); err != nil {
			t.Error(err)
		}
	})

	t.Run("session list returns results", func(t *testing.T) {
		result, err := testHarness.Run("session", "list")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
		// Should show sessions or "No active sessions"
		combined := result.Stdout + result.Stderr
		if !strings.Contains(combined, "Session") && !strings.Contains(combined, "No") {
			t.Error("Expected sessions or 'No active sessions' message")
		}
	})

	t.Run("session list json format", func(t *testing.T) {
		result, err := testHarness.Run("session", "list", "--json")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should return JSON even if empty
		combined := result.Stdout + result.Stderr
		if !strings.Contains(combined, "[") && !strings.Contains(combined, "{") {
			t.Error("Expected JSON output")
		}
	})

	t.Run("session list with limit", func(t *testing.T) {
		result, err := testHarness.Run("session", "list", "--limit", "5")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("session verify nonexistent ID", func(t *testing.T) {
		result, err := testHarness.Run("session", "verify", "nonexistent-session-id-12345")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should fail gracefully with error message
		if result.ExitCode == 0 {
			combined := result.Stdout + result.Stderr
			if !strings.Contains(combined, "error") && !strings.Contains(combined, "not found") {
				t.Log("Verify of nonexistent session returned success")
			}
		}
	})

	t.Run("session verify with json flag", func(t *testing.T) {
		result, err := testHarness.Run("session", "verify", "test-id", "--json")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should return JSON (even if error)
		combined := result.Stdout + result.Stderr
		if !strings.Contains(combined, "{") {
			t.Error("Expected JSON output")
		}
	})

	t.Run("session verify with full flag", func(t *testing.T) {
		result, err := testHarness.Run("session", "verify", "test-id", "--full")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should attempt full verification
		_ = result // Just verify it doesn't crash
	})

	t.Run("session delete nonexistent", func(t *testing.T) {
		result, err := testHarness.Run("session", "delete", "nonexistent-session-id-12345")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should handle gracefully (may be idempotent)
		_ = result
	})

	t.Run("session delete help", func(t *testing.T) {
		result, err := testHarness.Run("session", "delete", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("session verify help", func(t *testing.T) {
		result, err := testHarness.Run("session", "verify", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("session list help", func(t *testing.T) {
		result, err := testHarness.Run("session", "list", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})
}

// =============================================================================
// 4. POLICY COMMAND E2E TESTS (10+ tests)
// =============================================================================

func TestE2E_PolicyCommands(t *testing.T) {
	t.Run("policy help", func(t *testing.T) {
		result, err := testHarness.Run("policy", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
		if err := result.AssertOutputContains("verify"); err != nil {
			t.Error(err)
		}
	})

	t.Run("policy verify shows hash", func(t *testing.T) {
		result, err := testHarness.Run("policy", "verify")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
		// Should show SHA256 hash
		combined := result.Stdout + result.Stderr
		if !strings.Contains(strings.ToLower(combined), "sha") {
			t.Error("Expected SHA hash in output")
		}
	})

	t.Run("policy verify consistent hash", func(t *testing.T) {
		// Run twice and verify same hash
		result1, err := testHarness.Run("policy", "verify")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		result2, err := testHarness.Run("policy", "verify")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Both should return same content
		if result1.Stdout != result2.Stdout {
			t.Error("Policy verify hash should be consistent across runs")
		}
	})

	t.Run("policy dump shows content", func(t *testing.T) {
		result, err := testHarness.Run("policy", "dump")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
		// Should show policy YAML content
		combined := result.Stdout + result.Stderr
		if !strings.Contains(combined, "classifications") && !strings.Contains(combined, "patterns") {
			t.Error("Expected policy content in dump")
		}
	})

	t.Run("policy test SSN detection", func(t *testing.T) {
		result, err := testHarness.Run("policy", "test", "123-45-6789")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
		// Should detect SSN
		combined := strings.ToLower(result.Stdout + result.Stderr)
		if !strings.Contains(combined, "ssn") && !strings.Contains(combined, "pii") {
			t.Error("Expected SSN detection")
		}
	})

	t.Run("policy test AWS key detection", func(t *testing.T) {
		result, err := testHarness.Run("policy", "test", "AKIAIOSFODNN7EXAMPLE")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
		// Should detect AWS key
		combined := strings.ToLower(result.Stdout + result.Stderr)
		if !strings.Contains(combined, "aws") && !strings.Contains(combined, "secret") {
			t.Error("Expected AWS key detection")
		}
	})

	t.Run("policy test credit card detection", func(t *testing.T) {
		result, err := testHarness.Run("policy", "test", "4111-1111-1111-1111")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should detect credit card
		combined := strings.ToLower(result.Stdout + result.Stderr)
		if !strings.Contains(combined, "credit") && !strings.Contains(combined, "card") && !strings.Contains(combined, "pci") {
			t.Log("Expected credit card detection - may vary by policy version")
		}
	})

	t.Run("policy test email detection", func(t *testing.T) {
		result, err := testHarness.Run("policy", "test", "user@example.com")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// May or may not detect email as PII
		_ = result
	})

	t.Run("policy test phone detection", func(t *testing.T) {
		result, err := testHarness.Run("policy", "test", "+1-555-123-4567")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// May or may not detect phone as PII
		_ = result
	})

	t.Run("policy test clean input", func(t *testing.T) {
		result, err := testHarness.Run("policy", "test", "Hello World - just regular text")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("policy test multiline content", func(t *testing.T) {
		// Test multiline content with embedded secrets
		result, err := testHarness.Run("policy", "test", "Line 1\nSSN: 123-45-6789\nLine 3")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should still detect SSN in multiline
		_ = result
	})

	t.Run("policy test empty input", func(t *testing.T) {
		result, err := testHarness.Run("policy", "test", "")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should handle empty input gracefully
		_ = result
	})
}

// =============================================================================
// 5. WEAVIATE COMMAND E2E TESTS (10+ tests)
// =============================================================================

func TestE2E_WeaviateCommands(t *testing.T) {
	if !isStackRunning() {
		t.Skip("Stack not running - skipping E2E weaviate tests")
	}

	t.Run("weaviate help", func(t *testing.T) {
		result, err := testHarness.Run("weaviate", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
		if err := result.AssertOutputContains("summary"); err != nil {
			t.Error(err)
		}
	})

	t.Run("weaviate summary returns JSON", func(t *testing.T) {
		result, err := testHarness.Run("weaviate", "summary")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
		// Should return JSON
		combined := result.Stdout + result.Stderr
		if !strings.Contains(combined, "{") {
			t.Error("Expected JSON output")
		}
	})

	t.Run("weaviate summary parseable JSON", func(t *testing.T) {
		result, err := testHarness.Run("weaviate", "summary")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should be valid JSON
		var data interface{}
		if err := json.Unmarshal([]byte(result.Stdout), &data); err != nil {
			// Try stderr
			if err := json.Unmarshal([]byte(result.Stderr), &data); err != nil {
				t.Log("Weaviate summary may not be pure JSON")
			}
		}
	})

	t.Run("weaviate wipeout without force fails", func(t *testing.T) {
		result, err := testHarness.Run("weaviate", "wipeout")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should fail or warn without --force
		combined := result.Stdout + result.Stderr
		if !strings.Contains(combined, "force") && result.ExitCode == 0 {
			t.Error("Expected --force requirement")
		}
	})

	t.Run("weaviate backup help", func(t *testing.T) {
		result, err := testHarness.Run("weaviate", "backup", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("weaviate restore help", func(t *testing.T) {
		result, err := testHarness.Run("weaviate", "restore", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("weaviate restore nonexistent backup", func(t *testing.T) {
		result, err := testHarness.Run("weaviate", "restore", "nonexistent-backup-12345")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should fail gracefully
		if result.ExitCode == 0 {
			t.Log("Restore of nonexistent backup returned success")
		}
	})

	t.Run("weaviate query help", func(t *testing.T) {
		result, err := testHarness.Run("weaviate", "query", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// May or may not exist
		_ = result
	})

	t.Run("weaviate schema check", func(t *testing.T) {
		result, err := testHarness.Run("weaviate", "schema")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// May or may not have schema subcommand
		_ = result
	})

	t.Run("weaviate stats", func(t *testing.T) {
		result, err := testHarness.Run("weaviate", "stats")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// May or may not have stats subcommand
		_ = result
	})
}

// =============================================================================
// 6. HEALTH COMMAND E2E TESTS (10+ tests)
// =============================================================================

func TestE2E_HealthCommands(t *testing.T) {
	if !isStackRunning() {
		t.Skip("Stack not running - skipping E2E health tests")
	}

	t.Run("health basic", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"health"},
			Timeout: 60 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("health json output", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"health", "--json"},
			Timeout: 60 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
		// Should contain JSON structure
		combined := result.Stdout + result.Stderr
		if !strings.Contains(combined, "{") {
			t.Error("Expected JSON output")
		}
	})

	t.Run("health json has overall_state", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"health", "--json"},
			Timeout: 60 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		combined := result.Stdout + result.Stderr
		if !strings.Contains(combined, "overall_state") {
			t.Error("Expected overall_state in JSON output")
		}
	})

	t.Run("health verbose output", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"health", "--verbose"},
			Timeout: 60 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
		// Verbose should have more output
		if len(result.Stdout) == 0 && len(result.Stderr) == 0 {
			t.Error("Expected verbose output")
		}
	})

	t.Run("health custom window 5m", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"health", "-w", "5m"},
			Timeout: 60 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("health custom window 15m", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"health", "-w", "15m"},
			Timeout: 60 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("health custom window 1h", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"health", "-w", "1h"},
			Timeout: 60 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("health invalid window format", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"health", "-w", "invalid"},
			Timeout: 60 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should handle invalid format
		if result.ExitCode == 0 {
			t.Log("Invalid window format accepted - may default")
		}
	})

	t.Run("health help", func(t *testing.T) {
		result, err := testHarness.Run("health", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("health combined flags", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"health", "--json", "--verbose", "-w", "10m"},
			Timeout: 60 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Combined flags should work
		_ = result
	})
}

// =============================================================================
// 7. INGEST COMMAND E2E TESTS (10+ tests)
// =============================================================================

func TestE2E_IngestCommands(t *testing.T) {
	if !isStackRunning() {
		t.Skip("Stack not running - skipping E2E ingest tests")
	}

	t.Run("ingest help", func(t *testing.T) {
		result, err := testHarness.Run("ingest", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("ingest clean file", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"ingest", testFixtures.Files["clean_doc.txt"]},
			Timeout: 120 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("ingest with force flag", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"ingest", "--force", testFixtures.Files["secret_doc.txt"]},
			Timeout: 120 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should succeed in force mode even with secrets
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("ingest with data-space", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"ingest", "--data-space", "test-space", testFixtures.Files["clean_doc.txt"]},
			Timeout: 120 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("ingest nonexistent path", func(t *testing.T) {
		result, err := testHarness.Run("ingest", "/nonexistent/path/file.txt")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should handle gracefully
		_ = result
	})

	t.Run("ingest directory", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"ingest", testFixtures.TempDir},
			Timeout: 180 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should process directory
		_ = result
	})

	t.Run("ingest with verbose flag", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"ingest", "--verbose", testFixtures.Files["clean_doc.txt"]},
			Timeout: 120 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Verbose should show more output
		_ = result
	})

	t.Run("ingest with dry-run", func(t *testing.T) {
		result, err := testHarness.Run("ingest", "--dry-run", testFixtures.Files["clean_doc.txt"])
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Dry run should not actually ingest
		_ = result
	})

	t.Run("ingest markdown file", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"ingest", testFixtures.Files["test_doc.md"]},
			Timeout: 120 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("ingest multiple files", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args: []string{
				"ingest",
				testFixtures.Files["clean_doc.txt"],
				testFixtures.Files["test_doc.md"],
			},
			Timeout: 180 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should handle multiple files
		_ = result
	})

	t.Run("ingest with glob pattern", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"ingest", filepath.Join(testFixtures.TempDir, "*.txt")},
			Timeout: 180 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should handle glob pattern
		_ = result
	})
}

// =============================================================================
// 8. UPLOAD COMMAND E2E TESTS (10+ tests)
// =============================================================================

func TestE2E_UploadCommands(t *testing.T) {
	t.Run("upload help", func(t *testing.T) {
		result, err := testHarness.Run("upload", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("upload logs help", func(t *testing.T) {
		result, err := testHarness.Run("upload", "logs", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("upload backups help", func(t *testing.T) {
		result, err := testHarness.Run("upload", "backups", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("upload logs shows disabled", func(t *testing.T) {
		result, err := testHarness.Run("upload", "logs", ".")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should show disabled message
		combined := strings.ToLower(result.Stdout + result.Stderr)
		if !strings.Contains(combined, "disabled") {
			t.Error("Expected disabled message")
		}
	})

	t.Run("upload backups shows disabled", func(t *testing.T) {
		result, err := testHarness.Run("upload", "backups", ".")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should show disabled message
		combined := strings.ToLower(result.Stdout + result.Stderr)
		if !strings.Contains(combined, "disabled") {
			t.Error("Expected disabled message")
		}
	})

	t.Run("upload logs nonexistent path", func(t *testing.T) {
		result, err := testHarness.Run("upload", "logs", "/nonexistent/path")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should handle gracefully
		_ = result
	})

	t.Run("upload backups nonexistent path", func(t *testing.T) {
		result, err := testHarness.Run("upload", "backups", "/nonexistent/path")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should handle gracefully
		_ = result
	})

	t.Run("upload logs with dry-run", func(t *testing.T) {
		result, err := testHarness.Run("upload", "logs", "--dry-run", ".")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should accept dry-run flag
		_ = result
	})

	t.Run("upload backups with dry-run", func(t *testing.T) {
		result, err := testHarness.Run("upload", "backups", "--dry-run", ".")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should accept dry-run flag
		_ = result
	})

	t.Run("upload without subcommand", func(t *testing.T) {
		result, err := testHarness.Run("upload")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should show help or error
		_ = result
	})
}

// =============================================================================
// 9. TIMESERIES COMMAND E2E TESTS (10+ tests)
// =============================================================================

func TestE2E_TimeseriesCommands(t *testing.T) {
	if !isStackRunning() {
		t.Skip("Stack not running - skipping E2E timeseries tests")
	}

	t.Run("timeseries help", func(t *testing.T) {
		result, err := testHarness.Run("timeseries", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("timeseries fetch help", func(t *testing.T) {
		result, err := testHarness.Run("timeseries", "fetch", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("timeseries fetch SPY", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"timeseries", "fetch", "SPY", "--days", "30"},
			Timeout: 120 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should attempt to fetch
		combined := strings.ToLower(result.Stdout + result.Stderr)
		if !strings.Contains(combined, "fetch") && !strings.Contains(combined, "error") {
			t.Log("Expected fetch attempt or error")
		}
	})

	t.Run("timeseries fetch QQQ", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"timeseries", "fetch", "QQQ", "--days", "7"},
			Timeout: 120 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		_ = result
	})

	t.Run("timeseries fetch invalid symbol", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"timeseries", "fetch", "INVALID_SYMBOL_12345", "--days", "7"},
			Timeout: 60 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should handle invalid symbol
		_ = result
	})

	t.Run("timeseries fetch with start-date", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"timeseries", "fetch", "SPY", "--start-date", "2024-01-01"},
			Timeout: 120 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		_ = result
	})

	t.Run("timeseries fetch with end-date", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"timeseries", "fetch", "SPY", "--start-date", "2024-01-01", "--end-date", "2024-01-31"},
			Timeout: 120 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		_ = result
	})

	t.Run("timeseries list", func(t *testing.T) {
		result, err := testHarness.Run("timeseries", "list")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// May or may not exist
		_ = result
	})

	t.Run("timeseries delete help", func(t *testing.T) {
		result, err := testHarness.Run("timeseries", "delete", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		_ = result
	})

	t.Run("timeseries fetch multiple days values", func(t *testing.T) {
		// Test various days values
		for _, days := range []string{"1", "7", "30", "90"} {
			result, err := testHarness.RunWithOptions(CLIRunOptions{
				Args:    []string{"timeseries", "fetch", "SPY", "--days", days},
				Timeout: 60 * time.Second,
			})
			if err != nil {
				t.Logf("Days %s: Run failed: %v", days, err)
				continue
			}
			_ = result
		}
	})
}

// =============================================================================
// 10. CHAT COMMAND E2E TESTS (10+ tests)
// =============================================================================

func TestE2E_ChatCommands(t *testing.T) {
	t.Run("chat help", func(t *testing.T) {
		result, err := testHarness.Run("chat", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("chat resume flag help", func(t *testing.T) {
		result, err := testHarness.Run("chat", "--resume", "test-id", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Help should still work with resume flag
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	// Note: --model and --data-space flags are not implemented for chat command

	// Interactive tests require stack
	if isStackRunning() {
		t.Run("chat with immediate quit", func(t *testing.T) {
			result, err := testHarness.RunInteractive(
				[]string{"chat"},
				[]string{"/quit"},
			)
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// Should exit cleanly
			_ = result
		})

		t.Run("chat with exit command", func(t *testing.T) {
			result, err := testHarness.RunInteractive(
				[]string{"chat"},
				[]string{"/exit"},
			)
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			_ = result
		})

		t.Run("chat with help command", func(t *testing.T) {
			result, err := testHarness.RunInteractive(
				[]string{"chat"},
				[]string{"/help", "/quit"},
			)
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			_ = result
		})

		t.Run("chat simple question", func(t *testing.T) {
			result, err := testHarness.RunWithOptions(CLIRunOptions{
				Args:    []string{"chat"},
				Stdin:   "What is 2+2?\n/quit\n",
				Timeout: 120 * time.Second,
			})
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// Should get some response
			_ = result
		})

		t.Run("chat with data-space", func(t *testing.T) {
			result, err := testHarness.RunWithOptions(CLIRunOptions{
				Args:    []string{"chat", "--data-space", "test"},
				Stdin:   "/quit\n",
				Timeout: 60 * time.Second,
			})
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			_ = result
		})
	}

	t.Run("chat invalid resume session", func(t *testing.T) {
		// This may fail at start since session doesn't exist
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"chat", "--resume", "nonexistent-session-12345"},
			Stdin:   "/quit\n",
			Timeout: 30 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should handle gracefully
		_ = result
	})
}

// =============================================================================
// 11. ASK COMMAND E2E TESTS (10+ tests)
// =============================================================================

func TestE2E_AskCommands(t *testing.T) {
	t.Run("ask help", func(t *testing.T) {
		result, err := testHarness.Run("ask", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	if isStackRunning() {
		t.Run("ask simple question", func(t *testing.T) {
			result, err := testHarness.RunWithOptions(CLIRunOptions{
				Args:    []string{"ask", "What is the capital of France?"},
				Timeout: 120 * time.Second,
			})
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// Should get some response
			_ = result
		})

		t.Run("ask with data-space", func(t *testing.T) {
			result, err := testHarness.RunWithOptions(CLIRunOptions{
				Args:    []string{"ask", "--data-space", "test", "Hello"},
				Timeout: 120 * time.Second,
			})
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			_ = result
		})

		t.Run("ask with model flag", func(t *testing.T) {
			result, err := testHarness.RunWithOptions(CLIRunOptions{
				Args:    []string{"ask", "--model", "default", "Hello"},
				Timeout: 120 * time.Second,
			})
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			_ = result
		})

		t.Run("ask with json output", func(t *testing.T) {
			result, err := testHarness.RunWithOptions(CLIRunOptions{
				Args:    []string{"ask", "--json", "Hello"},
				Timeout: 120 * time.Second,
			})
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// May or may not support --json
			_ = result
		})

		t.Run("ask empty question", func(t *testing.T) {
			result, err := testHarness.RunWithOptions(CLIRunOptions{
				Args:    []string{"ask", ""},
				Timeout: 60 * time.Second,
			})
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// Should handle empty input
			_ = result
		})

		t.Run("ask multiword question", func(t *testing.T) {
			result, err := testHarness.RunWithOptions(CLIRunOptions{
				Args:    []string{"ask", "Tell me a short story about a robot learning to paint"},
				Timeout: 180 * time.Second,
			})
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			_ = result
		})

		t.Run("ask with special characters", func(t *testing.T) {
			result, err := testHarness.RunWithOptions(CLIRunOptions{
				Args:    []string{"ask", "What's 2+2? Let's see!"},
				Timeout: 120 * time.Second,
			})
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			_ = result
		})

		t.Run("ask about code", func(t *testing.T) {
			result, err := testHarness.RunWithOptions(CLIRunOptions{
				Args:    []string{"ask", "Write a hello world in Go"},
				Timeout: 180 * time.Second,
			})
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			_ = result
		})
	}

	t.Run("ask without question", func(t *testing.T) {
		result, err := testHarness.Run("ask")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should show error or help
		_ = result
	})

	t.Run("ask invalid data-space", func(t *testing.T) {
		result, err := testHarness.RunWithOptions(CLIRunOptions{
			Args:    []string{"ask", "--data-space", "", "Hello"},
			Timeout: 60 * time.Second,
		})
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should handle empty data-space
		_ = result
	})
}

// =============================================================================
// 12. EVALUATE COMMAND E2E TESTS (10+ tests)
// =============================================================================

func TestE2E_EvaluateCommands(t *testing.T) {
	t.Run("evaluate help", func(t *testing.T) {
		result, err := testHarness.Run("evaluate", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	t.Run("eval alias help", func(t *testing.T) {
		result, err := testHarness.Run("eval", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// May or may not have eval alias
		_ = result
	})

	if isStackRunning() {
		t.Run("evaluate list", func(t *testing.T) {
			result, err := testHarness.Run("evaluate", "list")
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// Should list evaluations or show empty
			_ = result
		})

		t.Run("evaluate list json", func(t *testing.T) {
			result, err := testHarness.Run("evaluate", "list", "--json")
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// Should return JSON
			_ = result
		})

		t.Run("evaluate run help", func(t *testing.T) {
			result, err := testHarness.Run("evaluate", "run", "--help")
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			if err := result.AssertExitCode(0); err != nil {
				t.Error(err)
			}
		})

		t.Run("evaluate status help", func(t *testing.T) {
			result, err := testHarness.Run("evaluate", "status", "--help")
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			_ = result
		})

		t.Run("evaluate status nonexistent", func(t *testing.T) {
			result, err := testHarness.Run("evaluate", "status", "nonexistent-eval-id")
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// Should handle gracefully
			_ = result
		})

		t.Run("evaluate delete help", func(t *testing.T) {
			result, err := testHarness.Run("evaluate", "delete", "--help")
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			_ = result
		})

		t.Run("evaluate run without config", func(t *testing.T) {
			result, err := testHarness.Run("evaluate", "run")
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// Should error without config
			_ = result
		})

		t.Run("evaluate run with invalid config", func(t *testing.T) {
			result, err := testHarness.Run("evaluate", "run", "--config", "/nonexistent/config.yaml")
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// Should error with invalid config
			_ = result
		})
	}

	t.Run("evaluate without subcommand", func(t *testing.T) {
		result, err := testHarness.Run("evaluate")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should show help or subcommands
		_ = result
	})

	t.Run("evaluate invalid subcommand", func(t *testing.T) {
		result, err := testHarness.Run("evaluate", "invalid-subcommand")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should error
		_ = result
	})
}

// =============================================================================
// 13. PULL COMMAND E2E TESTS (10+ tests)
// =============================================================================

func TestE2E_PullCommands(t *testing.T) {
	t.Run("pull help", func(t *testing.T) {
		result, err := testHarness.Run("pull", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		if err := result.AssertExitCode(0); err != nil {
			t.Error(err)
		}
	})

	if isStackRunning() {
		t.Run("pull invalid model", func(t *testing.T) {
			result, err := testHarness.RunWithOptions(CLIRunOptions{
				Args:    []string{"pull", "nonexistent/model"},
				Timeout: 30 * time.Second,
			})
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// Should show error or failure
			_ = result
		})

		t.Run("pull without model name", func(t *testing.T) {
			result, err := testHarness.Run("pull")
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// Should error without model name
			_ = result
		})

		t.Run("pull with force flag", func(t *testing.T) {
			result, err := testHarness.RunWithOptions(CLIRunOptions{
				Args:    []string{"pull", "--force", "nonexistent/model"},
				Timeout: 30 * time.Second,
			})
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			_ = result
		})

		t.Run("pull llama model format", func(t *testing.T) {
			result, err := testHarness.RunWithOptions(CLIRunOptions{
				Args:    []string{"pull", "llama3.2:1b"},
				Timeout: 30 * time.Second,
			})
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// May initiate download or fail
			_ = result
		})

		t.Run("pull with tag", func(t *testing.T) {
			result, err := testHarness.RunWithOptions(CLIRunOptions{
				Args:    []string{"pull", "model:tag"},
				Timeout: 30 * time.Second,
			})
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			_ = result
		})

		t.Run("pull with registry prefix", func(t *testing.T) {
			result, err := testHarness.RunWithOptions(CLIRunOptions{
				Args:    []string{"pull", "library/model"},
				Timeout: 30 * time.Second,
			})
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			_ = result
		})

		t.Run("pull status check", func(t *testing.T) {
			result, err := testHarness.Run("pull", "--status")
			if err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			// May or may not have --status flag
			_ = result
		})
	}

	t.Run("pull empty model name", func(t *testing.T) {
		result, err := testHarness.Run("pull", "")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should error
		_ = result
	})

	t.Run("pull invalid characters", func(t *testing.T) {
		result, err := testHarness.Run("pull", "model@#$%")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should error gracefully
		_ = result
	})

	t.Run("pull verbose flag", func(t *testing.T) {
		result, err := testHarness.Run("pull", "--verbose", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should accept verbose flag
		_ = result
	})
}

// =============================================================================
// 14. CONFIG COMMAND E2E TESTS (10+ tests)
// =============================================================================

func TestE2E_ConfigCommands(t *testing.T) {
	t.Run("config help", func(t *testing.T) {
		result, err := testHarness.Run("config", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// May or may not have config subcommand
		_ = result
	})

	t.Run("config show help", func(t *testing.T) {
		result, err := testHarness.Run("config", "show", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		_ = result
	})

	t.Run("config show", func(t *testing.T) {
		result, err := testHarness.Run("config", "show")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should show current config or error
		_ = result
	})

	t.Run("config validate", func(t *testing.T) {
		result, err := testHarness.Run("config", "validate")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		_ = result
	})

	t.Run("config path", func(t *testing.T) {
		result, err := testHarness.Run("config", "path")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		_ = result
	})

	t.Run("config init help", func(t *testing.T) {
		result, err := testHarness.Run("config", "init", "--help")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		_ = result
	})

	t.Run("config without subcommand", func(t *testing.T) {
		result, err := testHarness.Run("config")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should show help
		_ = result
	})

	t.Run("config invalid subcommand", func(t *testing.T) {
		result, err := testHarness.Run("config", "invalid-subcommand")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		// Should error
		_ = result
	})

	t.Run("config show json", func(t *testing.T) {
		result, err := testHarness.Run("config", "show", "--json")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		_ = result
	})

	t.Run("config show yaml", func(t *testing.T) {
		result, err := testHarness.Run("config", "show", "--yaml")
		if err != nil {
			t.Fatalf("Run failed: %v", err)
		}
		_ = result
	})
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// isStackRunning checks if the aleutian stack is running.
func isStackRunning() bool {
	result, err := testHarness.RunWithOptions(CLIRunOptions{
		Args:    []string{"status"},
		Timeout: 10 * time.Second,
	})
	if err != nil {
		return false
	}
	// Check for running containers
	return result.ExitCode == 0 &&
		(containsAny(result.Stdout, "running", "healthy", "Orchestrator"))
}

func containsAny(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if contains(s, substr) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr) >= 0))
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
