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
	"strings"
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

// =============================================================================
// Supply Chain Checker Tests
// =============================================================================

func TestSupplyChainChecker_BlocksInstallCommands(t *testing.T) {
	gate := NewDefaultGate(nil) // AllowPackageInstall is false by default

	tests := []struct {
		name       string
		command    string
		wantPassed bool
		wantCrit   int
	}{
		// NPM
		{"npm install blocked", "npm install express", false, 1},
		{"npm i blocked", "npm i express", false, 1},
		{"npm add blocked", "npm add express", false, 1},
		{"npm ci blocked", "npm ci", false, 1},
		{"npm run allowed", "npm run build", true, 0},
		{"npm test allowed", "npm test", true, 0},
		{"npm start allowed", "npm start", true, 0},
		{"npm audit allowed", "npm audit", true, 0},

		// Yarn
		{"yarn add blocked", "yarn add lodash", false, 1},
		{"yarn install blocked", "yarn install", false, 1},
		{"yarn run allowed", "yarn run test", true, 0},
		{"yarn test allowed", "yarn test", true, 0},

		// pnpm
		{"pnpm add blocked", "pnpm add react", false, 1},
		{"pnpm install blocked", "pnpm install", false, 1},
		{"pnpm i blocked", "pnpm i", false, 1},
		{"pnpm run allowed", "pnpm run build", true, 0},

		// pip
		{"pip install blocked", "pip install requests", false, 1},
		{"pip3 install blocked", "pip3 install numpy", false, 1},
		{"pip list allowed", "pip list", true, 0},
		{"pip freeze allowed", "pip freeze", true, 0},

		// poetry
		{"poetry add blocked", "poetry add django", false, 1},
		{"poetry install blocked", "poetry install", false, 1},
		{"poetry run allowed", "poetry run pytest", true, 0},

		// cargo
		{"cargo install blocked", "cargo install ripgrep", false, 1},
		{"cargo build allowed", "cargo build", true, 0},
		{"cargo test allowed", "cargo test", true, 0},
		{"cargo run allowed", "cargo run", true, 0},
		{"cargo check allowed", "cargo check", true, 0},

		// go
		{"go get blocked", "go get github.com/pkg/errors", false, 1},
		{"go install blocked", "go install github.com/golangci/golangci-lint", false, 1},
		{"go build allowed", "go build ./...", true, 0},
		{"go test allowed", "go test ./...", true, 0},
		{"go fmt allowed", "go fmt ./...", true, 0},
		{"go vet allowed", "go vet ./...", true, 0},
		{"go mod tidy allowed", "go mod tidy", true, 0},

		// gem
		{"gem install blocked", "gem install rails", false, 1},
		{"gem list allowed", "gem list", true, 0},

		// bundle
		{"bundle install blocked", "bundle install", false, 1},
		{"bundle add blocked", "bundle add rspec", false, 1},
		{"bundle exec allowed", "bundle exec rspec", true, 0},

		// composer
		{"composer install blocked", "composer install", false, 1},
		{"composer require blocked", "composer require laravel/laravel", false, 1},
		{"composer run allowed", "composer run test", true, 0},

		// dotnet
		{"dotnet add blocked", "dotnet add package Newtonsoft.Json", false, 1},
		{"dotnet build allowed", "dotnet build", true, 0},
		{"dotnet test allowed", "dotnet test", true, 0},

		// System package managers
		{"apt install blocked", "apt install vim", false, 1},
		{"apt-get install blocked", "apt-get install curl", false, 1},
		{"brew install blocked", "brew install jq", false, 1},
		{"yum install blocked", "yum install gcc", false, 1},
		{"dnf install blocked", "dnf install python3", false, 1},
		{"pacman -S blocked", "pacman -S firefox", false, 1},

		// Non-package manager commands are allowed
		{"ls allowed", "ls -la", true, 0},
		{"cat allowed", "cat README.md", true, 0},
		{"git status allowed", "git status", true, 0},
		{"make allowed", "make build", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			change := ProposedChange{
				Type:   "shell_command",
				Target: tt.command,
			}

			result, err := gate.Check(context.Background(), []ProposedChange{change})
			if err != nil {
				t.Fatalf("Check failed: %v", err)
			}
			if result.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v for command %q", result.Passed, tt.wantPassed, tt.command)
			}
			if result.CriticalCount != tt.wantCrit {
				t.Errorf("CriticalCount = %d, want %d for command %q", result.CriticalCount, tt.wantCrit, tt.command)
			}
		})
	}
}

func TestSupplyChainChecker_AllowPackageInstall(t *testing.T) {
	config := DefaultGateConfig()
	config.AllowPackageInstall = true
	gate := NewDefaultGate(&config)

	// When AllowPackageInstall is true, install commands should pass
	tests := []struct {
		name    string
		command string
	}{
		{"npm install", "npm install express"},
		{"pip install", "pip install requests"},
		{"cargo install", "cargo install ripgrep"},
		{"go get", "go get github.com/pkg/errors"},
		{"yarn add", "yarn add lodash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			change := ProposedChange{
				Type:   "shell_command",
				Target: tt.command,
			}

			result, err := gate.Check(context.Background(), []ProposedChange{change})
			if err != nil {
				t.Fatalf("Check failed: %v", err)
			}
			if !result.Passed {
				t.Errorf("expected command %q to pass with AllowPackageInstall=true", tt.command)
			}
		})
	}
}

func TestSupplyChainChecker_HandlesPathPrefixes(t *testing.T) {
	gate := NewDefaultGate(nil)

	// Commands invoked with full path should still be caught
	tests := []struct {
		name       string
		command    string
		wantPassed bool
	}{
		{"/usr/bin/npm install blocked", "/usr/bin/npm install express", false},
		{"/usr/local/bin/pip install blocked", "/usr/local/bin/pip install requests", false},
		{"/home/user/.cargo/bin/cargo install blocked", "/home/user/.cargo/bin/cargo install ripgrep", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			change := ProposedChange{
				Type:   "shell_command",
				Target: tt.command,
			}

			result, err := gate.Check(context.Background(), []ProposedChange{change})
			if err != nil {
				t.Fatalf("Check failed: %v", err)
			}
			if result.Passed != tt.wantPassed {
				t.Errorf("Passed = %v, want %v for command %q", result.Passed, tt.wantPassed, tt.command)
			}
		})
	}
}

func TestSupplyChainChecker_IssueDetails(t *testing.T) {
	gate := NewDefaultGate(nil)

	change := ProposedChange{
		Type:   "shell_command",
		Target: "npm install malicious-package",
	}

	result, err := gate.Check(context.Background(), []ProposedChange{change})
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	if len(result.Issues) == 0 {
		t.Fatal("expected at least one issue")
	}

	// Find the supply chain issue
	var supplyChainIssue *Issue
	for i := range result.Issues {
		if result.Issues[i].Code == "SUPPLY_CHAIN_INSTALL" {
			supplyChainIssue = &result.Issues[i]
			break
		}
	}

	if supplyChainIssue == nil {
		t.Fatal("expected SUPPLY_CHAIN_INSTALL issue")
	}

	if supplyChainIssue.Severity != SeverityCritical {
		t.Errorf("Severity = %s, want %s", supplyChainIssue.Severity, SeverityCritical)
	}

	if !strings.Contains(supplyChainIssue.Message, "postinstall") {
		t.Errorf("Message should mention postinstall scripts: %s", supplyChainIssue.Message)
	}

	if !strings.Contains(supplyChainIssue.Suggestion, "--allow-install") {
		t.Errorf("Suggestion should mention --allow-install flag: %s", supplyChainIssue.Suggestion)
	}
}

func TestSupplyChainChecker_IgnoresNonShellCommands(t *testing.T) {
	gate := NewDefaultGate(nil)

	// File writes should not trigger supply chain checker
	change := ProposedChange{
		Type:    "file_write",
		Target:  "/project/package.json",
		Content: `{"dependencies": {"express": "^4.0.0"}}`,
	}

	result, err := gate.Check(context.Background(), []ProposedChange{change})
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	// Should pass (supply chain checker only looks at shell_command)
	if !result.Passed {
		t.Error("file_write should not trigger supply chain checker")
	}
}

func TestSupplyChainChecker_EmptyCommand(t *testing.T) {
	checker := &SupplyChainChecker{config: DefaultGateConfig()}

	tests := []struct {
		name    string
		command string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
		{"single command no subcommand", "npm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			change := &ProposedChange{
				Type:   "shell_command",
				Target: tt.command,
			}

			issues := checker.Check(context.Background(), change)
			if len(issues) > 0 {
				t.Errorf("expected no issues for %q, got %v", tt.command, issues)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Safety-CDCL Integration Tests (Issue #6)
// -----------------------------------------------------------------------------

func TestIsSafetyError(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		expected bool
	}{
		{
			name:     "blocked by safety check prefix",
			errMsg:   "blocked by safety check",
			expected: true,
		},
		{
			name:     "blocked with details",
			errMsg:   "blocked by safety check: SUPPLY_CHAIN_INSTALL",
			expected: true,
		},
		{
			name:     "contains safety check",
			errMsg:   "operation failed due to safety check",
			expected: true,
		},
		{
			name:     "supply chain install code",
			errMsg:   "SUPPLY_CHAIN_INSTALL blocked",
			expected: true,
		},
		{
			name:     "path blocked code",
			errMsg:   "PATH_BLOCKED: .git/config",
			expected: true,
		},
		{
			name:     "command blocked code",
			errMsg:   "COMMAND_BLOCKED: rm -rf",
			expected: true,
		},
		{
			name:     "generic tool error",
			errMsg:   "file not found",
			expected: false,
		},
		{
			name:     "compilation error",
			errMsg:   "undefined: foo",
			expected: false,
		},
		{
			name:     "empty error",
			errMsg:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSafetyError(tt.errMsg)
			if got != tt.expected {
				t.Errorf("IsSafetyError(%q) = %v, want %v", tt.errMsg, got, tt.expected)
			}
		})
	}
}

func TestExtractConstraints(t *testing.T) {
	t.Run("nil result returns nil", func(t *testing.T) {
		constraints := ExtractConstraints(nil, "node1")
		if constraints != nil {
			t.Errorf("expected nil, got %v", constraints)
		}
	})

	t.Run("empty issues returns nil", func(t *testing.T) {
		result := &Result{Passed: true, Issues: nil}
		constraints := ExtractConstraints(result, "node1")
		if constraints != nil {
			t.Errorf("expected nil, got %v", constraints)
		}
	})

	t.Run("non-critical issues are skipped", func(t *testing.T) {
		result := &Result{
			Passed: false,
			Issues: []Issue{
				{
					Severity: SeverityWarning,
					Code:     "FILE_SIZE_WARNING",
					Message:  "File is large",
				},
			},
		}
		constraints := ExtractConstraints(result, "node1")
		if len(constraints) != 0 {
			t.Errorf("expected 0 constraints for warning, got %d", len(constraints))
		}
	})

	t.Run("critical issues create constraints", func(t *testing.T) {
		result := &Result{
			Passed: false,
			Issues: []Issue{
				{
					Severity: SeverityCritical,
					Code:     "SUPPLY_CHAIN_INSTALL",
					Message:  "npm install blocked",
					Change: &ProposedChange{
						Type:   "shell_command",
						Target: "npm install express",
					},
				},
			},
		}
		constraints := ExtractConstraints(result, "node1")
		if len(constraints) != 1 {
			t.Fatalf("expected 1 constraint, got %d", len(constraints))
		}
		if constraints[0].IssueCode != "SUPPLY_CHAIN_INSTALL" {
			t.Errorf("IssueCode = %q, want SUPPLY_CHAIN_INSTALL", constraints[0].IssueCode)
		}
		if constraints[0].Pattern != "command:npm install express" {
			t.Errorf("Pattern = %q, want command:npm install express", constraints[0].Pattern)
		}
		if constraints[0].ConflictingNodes[0] != "node1" {
			t.Errorf("ConflictingNodes[0] = %q, want node1", constraints[0].ConflictingNodes[0])
		}
	})

	t.Run("file write creates path pattern", func(t *testing.T) {
		result := &Result{
			Passed: false,
			Issues: []Issue{
				{
					Severity: SeverityCritical,
					Code:     "PATH_BLOCKED",
					Message:  ".git path blocked",
					Change: &ProposedChange{
						Type:   "file_write",
						Target: "/project/.git/config",
					},
				},
			},
		}
		constraints := ExtractConstraints(result, "test_node")
		if len(constraints) != 1 {
			t.Fatalf("expected 1 constraint, got %d", len(constraints))
		}
		if constraints[0].Pattern != "path:/project/.git/config" {
			t.Errorf("Pattern = %q, want path:/project/.git/config", constraints[0].Pattern)
		}
	})

	t.Run("multiple issues create multiple constraints", func(t *testing.T) {
		result := &Result{
			Passed: false,
			Issues: []Issue{
				{
					Severity: SeverityCritical,
					Code:     "PATH_BLOCKED",
					Message:  ".env blocked",
					Change: &ProposedChange{
						Type:   "file_write",
						Target: ".env",
					},
				},
				{
					Severity: SeverityCritical,
					Code:     "COMMAND_BLOCKED",
					Message:  "rm -rf blocked",
					Change: &ProposedChange{
						Type:   "shell_command",
						Target: "rm -rf /",
					},
				},
			},
		}
		constraints := ExtractConstraints(result, "multi_node")
		if len(constraints) != 2 {
			t.Fatalf("expected 2 constraints, got %d", len(constraints))
		}
	})
}

func TestResult_ToErrorMessage(t *testing.T) {
	t.Run("nil result returns base message", func(t *testing.T) {
		var r *Result
		msg := r.ToErrorMessage()
		if msg != SafetyBlockedError {
			t.Errorf("got %q, want %q", msg, SafetyBlockedError)
		}
	})

	t.Run("empty issues returns base message", func(t *testing.T) {
		r := &Result{Issues: nil}
		msg := r.ToErrorMessage()
		if msg != SafetyBlockedError {
			t.Errorf("got %q, want %q", msg, SafetyBlockedError)
		}
	})

	t.Run("includes critical issue codes", func(t *testing.T) {
		r := &Result{
			Issues: []Issue{
				{Severity: SeverityCritical, Code: "SUPPLY_CHAIN_INSTALL"},
				{Severity: SeverityWarning, Code: "FILE_SIZE_WARNING"}, // Should be excluded
				{Severity: SeverityCritical, Code: "PATH_BLOCKED"},
			},
		}
		msg := r.ToErrorMessage()
		if !strings.Contains(msg, "SUPPLY_CHAIN_INSTALL") {
			t.Errorf("expected message to contain SUPPLY_CHAIN_INSTALL, got %q", msg)
		}
		if !strings.Contains(msg, "PATH_BLOCKED") {
			t.Errorf("expected message to contain PATH_BLOCKED, got %q", msg)
		}
		if strings.Contains(msg, "FILE_SIZE_WARNING") {
			t.Errorf("expected message to NOT contain FILE_SIZE_WARNING, got %q", msg)
		}
	})
}
