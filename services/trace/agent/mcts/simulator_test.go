// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package mcts

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSimulationTier_String(t *testing.T) {
	tests := []struct {
		tier SimulationTier
		want string
	}{
		{SimTierQuick, "quick"},
		{SimTierStandard, "standard"},
		{SimTierFull, "full"},
		{SimulationTier(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.tier.String(); got != tt.want {
				t.Errorf("String() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestDefaultSimulatorConfig(t *testing.T) {
	config := DefaultSimulatorConfig()

	if config.QuickScoreThreshold != 0.5 {
		t.Errorf("QuickScoreThreshold = %f, want 0.5", config.QuickScoreThreshold)
	}
	if config.StandardScoreThreshold != 0.7 {
		t.Errorf("StandardScoreThreshold = %f, want 0.7", config.StandardScoreThreshold)
	}
	if config.QuickTimeout != 5*time.Second {
		t.Errorf("QuickTimeout = %v, want 5s", config.QuickTimeout)
	}
	if config.QuickWeights.Syntax != 0.6 {
		t.Errorf("QuickWeights.Syntax = %f, want 0.6", config.QuickWeights.Syntax)
	}
}

func TestNewSimulator(t *testing.T) {
	config := DefaultSimulatorConfig()
	sim := NewSimulator(config)

	if sim == nil {
		t.Fatal("NewSimulator returned nil")
	}
}

// Mock implementations for testing

type mockValidator struct {
	valid bool
}

func (m *mockValidator) CheckSyntax(code, language string) bool {
	return m.valid
}

type mockLinter struct {
	result *LintResult
	err    error
}

func (m *mockLinter) LintContent(ctx context.Context, content []byte, language string) (*LintResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

type mockBlastRadius struct {
	result *BlastRadiusResult
	err    error
}

func (m *mockBlastRadius) Analyze(ctx context.Context, filePath string, includeTests bool) (*BlastRadiusResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

type mockTestRunner struct {
	result *TestResult
	err    error
}

func (m *mockTestRunner) RunTest(ctx context.Context, testFile, testName string) (*TestResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

type mockSecurityScanner struct {
	result *SecurityScanResult
	err    error
}

func (m *mockSecurityScanner) ScanCode(ctx context.Context, code string) (*SecurityScanResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func TestSimulator_Simulate_NoAction(t *testing.T) {
	sim := NewSimulator(DefaultSimulatorConfig())

	node := NewPlanNode("1", "Test node")
	// No action set

	result, err := sim.Simulate(context.Background(), node, SimTierQuick)
	if err != nil {
		t.Fatalf("Simulate error: %v", err)
	}

	if result.Score != 0.5 {
		t.Errorf("Score = %f, want 0.5 for no action", result.Score)
	}
}

func TestSimulator_Simulate_UnvalidatedAction(t *testing.T) {
	sim := NewSimulator(DefaultSimulatorConfig())

	node := NewPlanNode("1", "Test node")
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "test.go",
		Description: "Test action",
	}
	node.SetAction(action)
	// Action not validated

	result, err := sim.Simulate(context.Background(), node, SimTierQuick)
	if err != nil {
		t.Fatalf("Simulate error: %v", err)
	}

	if result.Score != 0 {
		t.Errorf("Score = %f, want 0 for unvalidated action", result.Score)
	}
	if len(result.Errors) == 0 {
		t.Error("Expected error for unvalidated action")
	}
}

func TestSimulator_Simulate_QuickTier(t *testing.T) {
	sim := NewSimulator(DefaultSimulatorConfig(),
		WithValidator(&mockValidator{valid: true}))

	node := NewPlanNode("1", "Test node")
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "test.go",
		Description: "Test action",
		CodeDiff:    "package main\n\nfunc main() {}",
		Language:    "go",
	}
	if err := action.Validate("/project", DefaultActionValidationConfig()); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	node.SetAction(action)

	result, err := sim.Simulate(context.Background(), node, SimTierQuick)
	if err != nil {
		t.Fatalf("Simulate error: %v", err)
	}

	if result.Tier != "quick" {
		t.Errorf("Tier = %v, want quick", result.Tier)
	}
	if _, ok := result.Signals["syntax"]; !ok {
		t.Error("Missing syntax signal")
	}
	if _, ok := result.Signals["complexity"]; !ok {
		t.Error("Missing complexity signal")
	}
}

func TestSimulator_Simulate_StandardTier(t *testing.T) {
	sim := NewSimulator(DefaultSimulatorConfig(),
		WithValidator(&mockValidator{valid: true}),
		WithLinter(&mockLinter{result: &LintResult{Valid: true}}))

	node := NewPlanNode("1", "Test node")
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "test.go",
		Description: "Test action",
		CodeDiff:    "package main\n\nfunc main() {}",
		Language:    "go",
	}
	if err := action.Validate("/project", DefaultActionValidationConfig()); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	node.SetAction(action)

	result, err := sim.Simulate(context.Background(), node, SimTierStandard)
	if err != nil {
		t.Fatalf("Simulate error: %v", err)
	}

	if result.Tier != "standard" {
		t.Errorf("Tier = %v, want standard", result.Tier)
	}
	if _, ok := result.Signals["lint"]; !ok {
		t.Error("Missing lint signal")
	}
}

func TestSimulator_Simulate_FullTier(t *testing.T) {
	sim := NewSimulator(DefaultSimulatorConfig(),
		WithValidator(&mockValidator{valid: true}),
		WithLinter(&mockLinter{result: &LintResult{Valid: true}}),
		WithBlastRadius(&mockBlastRadius{result: &BlastRadiusResult{TotalAffected: 5}}),
		WithSecurityScanner(&mockSecurityScanner{result: &SecurityScanResult{Score: 0.9}}))

	node := NewPlanNode("1", "Test node")
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "test.go",
		Description: "Test action",
		CodeDiff:    "package main\n\nfunc main() {}",
		Language:    "go",
	}
	if err := action.Validate("/project", DefaultActionValidationConfig()); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	node.SetAction(action)

	result, err := sim.Simulate(context.Background(), node, SimTierFull)
	if err != nil {
		t.Fatalf("Simulate error: %v", err)
	}

	if result.Tier != "full" {
		t.Errorf("Tier = %v, want full", result.Tier)
	}
	if _, ok := result.Signals["blast_radius"]; !ok {
		t.Error("Missing blast_radius signal")
	}
	if _, ok := result.Signals["security"]; !ok {
		t.Error("Missing security signal")
	}
}

func TestSimulator_Simulate_LinterError(t *testing.T) {
	sim := NewSimulator(DefaultSimulatorConfig(),
		WithValidator(&mockValidator{valid: true}),
		WithLinter(&mockLinter{err: errors.New("lint failed")}))

	node := NewPlanNode("1", "Test node")
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "test.go",
		Description: "Test action",
		CodeDiff:    "package main",
		Language:    "go",
	}
	if err := action.Validate("/project", DefaultActionValidationConfig()); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	node.SetAction(action)

	result, err := sim.Simulate(context.Background(), node, SimTierStandard)
	if err != nil {
		t.Fatalf("Simulate error: %v", err)
	}

	// Should have warning about linter error
	if len(result.Warnings) == 0 {
		t.Error("Expected warning for linter error")
	}
	// Lint signal should be 0.5 (unknown)
	if result.Signals["lint"] != 0.5 {
		t.Errorf("Lint signal = %f, want 0.5", result.Signals["lint"])
	}
}

func TestSimulator_Simulate_SyntaxError(t *testing.T) {
	sim := NewSimulator(DefaultSimulatorConfig(),
		WithValidator(&mockValidator{valid: false}))

	node := NewPlanNode("1", "Test node")
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "test.go",
		Description: "Test action",
		CodeDiff:    "invalid{{{syntax",
		Language:    "go",
	}
	if err := action.Validate("/project", DefaultActionValidationConfig()); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	node.SetAction(action)

	result, err := sim.Simulate(context.Background(), node, SimTierQuick)
	if err != nil {
		t.Fatalf("Simulate error: %v", err)
	}

	if result.Signals["syntax"] != 0.0 {
		t.Errorf("Syntax signal = %f, want 0.0", result.Signals["syntax"])
	}
	if len(result.Errors) == 0 {
		t.Error("Expected error for syntax failure")
	}
}

func TestSimulator_Simulate_PromoteToNext(t *testing.T) {
	config := DefaultSimulatorConfig()
	config.QuickScoreThreshold = 0.5

	sim := NewSimulator(config,
		WithValidator(&mockValidator{valid: true}))

	node := NewPlanNode("1", "Test node")
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "test.go",
		Description: "Test action",
		CodeDiff:    "x", // Short code = high complexity score
		Language:    "go",
	}
	if err := action.Validate("/project", DefaultActionValidationConfig()); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	node.SetAction(action)

	result, err := sim.Simulate(context.Background(), node, SimTierQuick)
	if err != nil {
		t.Fatalf("Simulate error: %v", err)
	}

	// High score should promote
	if !result.PromoteToNext {
		t.Error("Should promote to next tier with high score")
	}
}

func TestSimulator_Simulate_NoPromote(t *testing.T) {
	config := DefaultSimulatorConfig()
	config.QuickScoreThreshold = 0.99

	sim := NewSimulator(config,
		WithValidator(&mockValidator{valid: false})) // Will get low score

	node := NewPlanNode("1", "Test node")
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "test.go",
		Description: "Test action",
		CodeDiff:    "bad code",
		Language:    "go",
	}
	if err := action.Validate("/project", DefaultActionValidationConfig()); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	node.SetAction(action)

	result, err := sim.Simulate(context.Background(), node, SimTierQuick)
	if err != nil {
		t.Fatalf("Simulate error: %v", err)
	}

	// Low score should not promote
	if result.PromoteToNext {
		t.Error("Should not promote with low score")
	}
}

func TestSimulator_SimulateProgressive(t *testing.T) {
	config := DefaultSimulatorConfig()
	config.QuickScoreThreshold = 0.3
	config.StandardScoreThreshold = 0.3

	sim := NewSimulator(config,
		WithValidator(&mockValidator{valid: true}),
		WithLinter(&mockLinter{result: &LintResult{Valid: true}}),
		WithSecurityScanner(&mockSecurityScanner{result: &SecurityScanResult{Score: 0.9}}))

	node := NewPlanNode("1", "Test node")
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "test.go",
		Description: "Test action",
		CodeDiff:    "package main",
		Language:    "go",
	}
	if err := action.Validate("/project", DefaultActionValidationConfig()); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	node.SetAction(action)

	result, err := sim.SimulateProgressive(context.Background(), node)
	if err != nil {
		t.Fatalf("SimulateProgressive error: %v", err)
	}

	// Should have progressed to full tier
	if result.Tier != "full" {
		t.Errorf("Tier = %v, want full", result.Tier)
	}
}

func TestSimulator_SimulateProgressive_StopsEarly(t *testing.T) {
	config := DefaultSimulatorConfig()
	config.QuickScoreThreshold = 0.99 // Very high threshold

	sim := NewSimulator(config,
		WithValidator(&mockValidator{valid: false})) // Low score

	node := NewPlanNode("1", "Test node")
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "test.go",
		Description: "Test action",
		CodeDiff:    "bad",
		Language:    "go",
	}
	if err := action.Validate("/project", DefaultActionValidationConfig()); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	node.SetAction(action)

	result, err := sim.SimulateProgressive(context.Background(), node)
	if err != nil {
		t.Fatalf("SimulateProgressive error: %v", err)
	}

	// Should have stopped at quick tier
	if result.Tier != "quick" {
		t.Errorf("Tier = %v, want quick (should stop early)", result.Tier)
	}
}

func TestSimulator_EstimateComplexity(t *testing.T) {
	sim := NewSimulator(DefaultSimulatorConfig())

	// Lines estimated as bytes / 40
	// < 5 lines (< 200 bytes) = 0.9
	// < 20 lines (< 800 bytes) = 0.7
	// < 50 lines (< 2000 bytes) = 0.5
	// >= 50 lines (>= 2000 bytes) = 0.3
	tests := []struct {
		code string
		want float64
	}{
		{"x", 0.9},                        // ~0 lines
		{string(make([]byte, 100)), 0.9},  // ~2 lines
		{string(make([]byte, 400)), 0.7},  // ~10 lines
		{string(make([]byte, 1000)), 0.5}, // ~25 lines
		{string(make([]byte, 3000)), 0.3}, // ~75 lines
	}

	for _, tt := range tests {
		got := sim.estimateComplexity(tt.code)
		if got != tt.want {
			t.Errorf("estimateComplexity(%d bytes) = %f, want %f", len(tt.code), got, tt.want)
		}
	}
}

func TestSimulator_CalculateScore(t *testing.T) {
	sim := NewSimulator(DefaultSimulatorConfig())

	// Quick tier with only syntax and complexity
	signals := map[string]float64{
		"syntax":     1.0,
		"complexity": 0.7,
	}

	score := sim.calculateScore(signals, SimTierQuick)
	expected := (1.0*0.6 + 0.7*0.4) / (0.6 + 0.4)
	if score != expected {
		t.Errorf("calculateScore = %f, want %f", score, expected)
	}
}

func TestSimulator_CalculateScore_NoSignals(t *testing.T) {
	sim := NewSimulator(DefaultSimulatorConfig())

	signals := map[string]float64{}

	score := sim.calculateScore(signals, SimTierQuick)
	if score != 0.5 {
		t.Errorf("calculateScore = %f, want 0.5 for no signals", score)
	}
}

func TestSimulator_SecurityIssues(t *testing.T) {
	sim := NewSimulator(DefaultSimulatorConfig(),
		WithValidator(&mockValidator{valid: true}),
		WithLinter(&mockLinter{result: &LintResult{Valid: true}}),
		WithSecurityScanner(&mockSecurityScanner{
			result: &SecurityScanResult{
				Score: 0.5,
				Issues: []SecurityIssue{
					{Severity: "high", Message: "SQL injection"},
					{Severity: "low", Message: "Weak hash"},
				},
			},
		}))

	node := NewPlanNode("1", "Test node")
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "test.go",
		Description: "Test action",
		CodeDiff:    "package main",
		Language:    "go",
	}
	if err := action.Validate("/project", DefaultActionValidationConfig()); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	node.SetAction(action)

	result, err := sim.Simulate(context.Background(), node, SimTierFull)
	if err != nil {
		t.Fatalf("Simulate error: %v", err)
	}

	// Should have error for high severity
	foundHighError := false
	for _, e := range result.Errors {
		if e == "security: SQL injection" {
			foundHighError = true
			break
		}
	}
	if !foundHighError {
		t.Error("Expected error for high severity security issue")
	}

	// Should have warning for low severity
	foundLowWarning := false
	for _, w := range result.Warnings {
		if w == "security: Weak hash" {
			foundLowWarning = true
			break
		}
	}
	if !foundLowWarning {
		t.Error("Expected warning for low severity security issue")
	}
}

func TestSimulator_Config(t *testing.T) {
	config := DefaultSimulatorConfig()
	config.QuickTimeout = 99 * time.Second

	sim := NewSimulator(config)

	got := sim.Config()
	if got.QuickTimeout != 99*time.Second {
		t.Errorf("Config().QuickTimeout = %v, want 99s", got.QuickTimeout)
	}
}

func TestSimulator_Simulate_ContextCancellation(t *testing.T) {
	// Use a slow mock linter to ensure we can test cancellation
	slowLinter := &mockLinter{
		result: &LintResult{Valid: true},
	}

	config := DefaultSimulatorConfig()
	config.StandardTimeout = 5 * time.Second // Long enough that we can cancel

	sim := NewSimulator(config,
		WithValidator(&mockValidator{valid: true}),
		WithLinter(slowLinter))

	node := NewPlanNode("1", "Test node")
	action := &PlannedAction{
		Type:        ActionTypeEdit,
		FilePath:    "test.go",
		Description: "Test action",
		CodeDiff:    "package main",
		Language:    "go",
	}
	if err := action.Validate("/project", DefaultActionValidationConfig()); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	node.SetAction(action)

	// Create a pre-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// The simulation should respect the cancelled context
	// Note: The current implementation checks ctx at the start of each tier
	// so it may still complete if checks happen before ctx is checked
	result, err := sim.Simulate(ctx, node, SimTierQuick)

	// Either we get a context error, or we get a result
	// (depending on timing - the quick tier is very fast)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled, got: %v", err)
		}
	} else if result == nil {
		t.Error("Expected either error or result, got neither")
	}
}
