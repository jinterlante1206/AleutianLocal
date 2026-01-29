// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package safety

import (
	"context"
	"testing"
)

func TestDefaultGate_Check_BlockedPaths(t *testing.T) {
	gate := NewDefaultGate(nil)

	tests := []struct {
		name       string
		change     ProposedChange
		wantPassed bool
		wantCrit   int
	}{
		{
			name: "allowed path",
			change: ProposedChange{
				Type:   "file_write",
				Target: "/project/src/main.go",
			},
			wantPassed: true,
			wantCrit:   0,
		},
		{
			name: "blocked .git path",
			change: ProposedChange{
				Type:   "file_write",
				Target: "/project/.git/config",
			},
			wantPassed: false,
			wantCrit:   1,
		},
		{
			name: "blocked .env path",
			change: ProposedChange{
				Type:   "file_write",
				Target: "/project/.env",
			},
			wantPassed: false,
			wantCrit:   1,
		},
		{
			name: "blocked secrets directory",
			change: ProposedChange{
				Type:   "file_delete",
				Target: "/project/secrets/api_key.txt",
			},
			wantPassed: false,
			wantCrit:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := gate.Check(context.Background(), []ProposedChange{tt.change})
			if err != nil {
				t.Fatalf("Check failed: %v", err)
			}
			if result.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v", result.Passed, tt.wantPassed)
			}
			if result.CriticalCount != tt.wantCrit {
				t.Errorf("CriticalCount = %d, want %d", result.CriticalCount, tt.wantCrit)
			}
		})
	}
}

func TestDefaultGate_Check_BlockedCommands(t *testing.T) {
	gate := NewDefaultGate(nil)

	tests := []struct {
		name       string
		change     ProposedChange
		wantPassed bool
		wantCrit   int
	}{
		{
			name: "allowed command",
			change: ProposedChange{
				Type:   "shell_command",
				Target: "go build ./...",
			},
			wantPassed: true,
			wantCrit:   0,
		},
		{
			name: "blocked rm -rf command",
			change: ProposedChange{
				Type:   "shell_command",
				Target: "rm -rf /",
			},
			wantPassed: false,
			wantCrit:   1,
		},
		{
			name: "blocked chmod 777 command",
			change: ProposedChange{
				Type:   "shell_command",
				Target: "chmod 777 /etc/passwd",
			},
			wantPassed: false,
			wantCrit:   1,
		},
		{
			name: "blocked write to /dev",
			change: ProposedChange{
				Type:   "shell_command",
				Target: "echo test > /dev/sda",
			},
			wantPassed: false,
			wantCrit:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := gate.Check(context.Background(), []ProposedChange{tt.change})
			if err != nil {
				t.Fatalf("Check failed: %v", err)
			}
			if result.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v", result.Passed, tt.wantPassed)
			}
			if result.CriticalCount != tt.wantCrit {
				t.Errorf("CriticalCount = %d, want %d", result.CriticalCount, tt.wantCrit)
			}
		})
	}
}

func TestDefaultGate_Check_FileSize(t *testing.T) {
	config := DefaultGateConfig()
	config.MaxFileSize = 100 // 100 bytes for testing
	gate := NewDefaultGate(&config)

	tests := []struct {
		name        string
		change      ProposedChange
		wantPassed  bool
		wantWarning int
	}{
		{
			name: "small file",
			change: ProposedChange{
				Type:    "file_write",
				Target:  "/project/small.txt",
				Content: "hello world",
			},
			wantPassed:  true,
			wantWarning: 0,
		},
		{
			name: "large file",
			change: ProposedChange{
				Type:    "file_write",
				Target:  "/project/large.txt",
				Content: string(make([]byte, 200)), // 200 bytes
			},
			wantPassed:  true, // Warnings don't block by default
			wantWarning: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := gate.Check(context.Background(), []ProposedChange{tt.change})
			if err != nil {
				t.Fatalf("Check failed: %v", err)
			}
			if result.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v", result.Passed, tt.wantPassed)
			}
			if result.WarningCount != tt.wantWarning {
				t.Errorf("WarningCount = %d, want %d", result.WarningCount, tt.wantWarning)
			}
		})
	}
}

func TestDefaultGate_Check_MultipleChanges(t *testing.T) {
	gate := NewDefaultGate(nil)

	changes := []ProposedChange{
		{Type: "file_write", Target: "/project/main.go", Content: "package main"},
		{Type: "file_write", Target: "/project/.git/hooks/pre-commit"},
		{Type: "shell_command", Target: "go build"},
	}

	result, err := gate.Check(context.Background(), changes)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	if result.Passed {
		t.Error("expected check to fail due to .git path")
	}
	if result.CriticalCount != 1 {
		t.Errorf("CriticalCount = %d, want 1", result.CriticalCount)
	}
}

func TestDefaultGate_Check_Disabled(t *testing.T) {
	config := DefaultGateConfig()
	config.Enabled = false
	gate := NewDefaultGate(&config)

	changes := []ProposedChange{
		{Type: "shell_command", Target: "rm -rf /"},
	}

	result, err := gate.Check(context.Background(), changes)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	if !result.Passed {
		t.Error("expected check to pass when disabled")
	}
}

func TestDefaultGate_ShouldBlock(t *testing.T) {
	tests := []struct {
		name   string
		config GateConfig
		result *Result
		want   bool
	}{
		{
			name:   "nil result",
			config: DefaultGateConfig(),
			result: nil,
			want:   false,
		},
		{
			name:   "passed result",
			config: DefaultGateConfig(),
			result: &Result{Passed: true},
			want:   false,
		},
		{
			name:   "critical with block enabled",
			config: GateConfig{BlockOnCritical: true},
			result: &Result{Passed: false, CriticalCount: 1},
			want:   true,
		},
		{
			name:   "warning with block disabled",
			config: GateConfig{BlockOnCritical: true, BlockOnWarning: false},
			result: &Result{Passed: true, WarningCount: 1},
			want:   false,
		},
		{
			name:   "warning with block enabled",
			config: GateConfig{BlockOnCritical: true, BlockOnWarning: true},
			result: &Result{Passed: false, WarningCount: 1},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gate := NewDefaultGate(&tt.config)
			if got := gate.ShouldBlock(tt.result); got != tt.want {
				t.Errorf("ShouldBlock() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultGate_GenerateWarnings(t *testing.T) {
	gate := NewDefaultGate(nil)

	t.Run("nil result", func(t *testing.T) {
		warnings := gate.GenerateWarnings(nil)
		if len(warnings) != 0 {
			t.Errorf("expected 0 warnings, got %d", len(warnings))
		}
	})

	t.Run("no issues", func(t *testing.T) {
		result := &Result{Passed: true, Issues: nil}
		warnings := gate.GenerateWarnings(result)
		if len(warnings) != 0 {
			t.Errorf("expected 0 warnings, got %d", len(warnings))
		}
	})

	t.Run("with issues", func(t *testing.T) {
		result := &Result{
			Passed: false,
			Issues: []Issue{
				{Severity: SeverityCritical, Message: "Critical issue"},
				{Severity: SeverityWarning, Message: "Warning issue", Suggestion: "Fix it"},
				{Severity: SeverityInfo, Message: "Info message"},
			},
		}

		warnings := gate.GenerateWarnings(result)
		if len(warnings) != 3 {
			t.Fatalf("expected 3 warnings, got %d", len(warnings))
		}

		if warnings[0] != "[CRITICAL] Critical issue" {
			t.Errorf("unexpected warning[0]: %s", warnings[0])
		}
		if warnings[1] != "[WARNING] Warning issue Suggestion: Fix it" {
			t.Errorf("unexpected warning[1]: %s", warnings[1])
		}
		if warnings[2] != "[INFO] Info message" {
			t.Errorf("unexpected warning[2]: %s", warnings[2])
		}
	})
}

func TestResult_HasCritical(t *testing.T) {
	tests := []struct {
		name   string
		result Result
		want   bool
	}{
		{name: "no critical", result: Result{CriticalCount: 0}, want: false},
		{name: "has critical", result: Result{CriticalCount: 1}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.HasCritical(); got != tt.want {
				t.Errorf("HasCritical() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResult_HasWarnings(t *testing.T) {
	tests := []struct {
		name   string
		result Result
		want   bool
	}{
		{name: "no warnings", result: Result{WarningCount: 0}, want: false},
		{name: "has warnings", result: Result{WarningCount: 1}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.HasWarnings(); got != tt.want {
				t.Errorf("HasWarnings() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMockGate(t *testing.T) {
	mock := NewMockGate()

	t.Run("records calls", func(t *testing.T) {
		changes := []ProposedChange{{Type: "file_write", Target: "/test"}}
		_, _ = mock.Check(context.Background(), changes)

		if mock.CallCount() != 1 {
			t.Errorf("CallCount = %d, want 1", mock.CallCount())
		}
	})

	t.Run("custom check func", func(t *testing.T) {
		mock.CheckFunc = func(ctx context.Context, changes []ProposedChange) (*Result, error) {
			return &Result{Passed: false, CriticalCount: 5}, nil
		}

		result, _ := mock.Check(context.Background(), nil)
		if result.CriticalCount != 5 {
			t.Errorf("CriticalCount = %d, want 5", result.CriticalCount)
		}
	})

	t.Run("reset", func(t *testing.T) {
		mock.Reset()
		if mock.CallCount() != 0 {
			t.Errorf("CallCount after reset = %d, want 0", mock.CallCount())
		}
	})
}

func TestContextCancellation(t *testing.T) {
	gate := NewDefaultGate(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	changes := []ProposedChange{{Type: "file_write", Target: "/test"}}
	_, err := gate.Check(ctx, changes)

	if err == nil {
		t.Error("expected error for cancelled context")
	}
}
