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
	"fmt"
	"time"
)

// SimulationTier defines the depth of simulation.
type SimulationTier int

const (
	// SimTierQuick runs only fast checks (syntax, complexity).
	// Use for initial exploration. ~5ms.
	SimTierQuick SimulationTier = iota

	// SimTierStandard adds linter checks.
	// Use for promising nodes. ~100ms.
	SimTierStandard

	// SimTierFull adds blast radius and test execution.
	// Use only for best path candidates. ~500ms+.
	SimTierFull
)

// String returns a human-readable tier name.
func (t SimulationTier) String() string {
	switch t {
	case SimTierQuick:
		return "quick"
	case SimTierStandard:
		return "standard"
	case SimTierFull:
		return "full"
	default:
		return "unknown"
	}
}

// SignalWeights defines weights for each signal in score calculation.
type SignalWeights struct {
	Syntax      float64
	Complexity  float64
	Lint        float64
	BlastRadius float64
	Tests       float64
	Security    float64
}

// SimulatorConfig configures the simulator behavior.
type SimulatorConfig struct {
	// Score thresholds for tier promotion
	QuickScoreThreshold    float64 // Score needed to run standard tier (default: 0.5)
	StandardScoreThreshold float64 // Score needed to run full tier (default: 0.7)

	// Timeouts per tier
	QuickTimeout    time.Duration
	StandardTimeout time.Duration
	FullTimeout     time.Duration

	// Signal weights by tier
	QuickWeights    SignalWeights
	StandardWeights SignalWeights
	FullWeights     SignalWeights
}

// DefaultSimulatorConfig returns sensible defaults.
func DefaultSimulatorConfig() SimulatorConfig {
	return SimulatorConfig{
		QuickScoreThreshold:    0.5,
		StandardScoreThreshold: 0.7,
		QuickTimeout:           5 * time.Second,
		StandardTimeout:        30 * time.Second,
		FullTimeout:            2 * time.Minute,
		QuickWeights: SignalWeights{
			Syntax:     0.6,
			Complexity: 0.4,
		},
		StandardWeights: SignalWeights{
			Syntax:     0.3,
			Complexity: 0.2,
			Lint:       0.5,
		},
		FullWeights: SignalWeights{
			Syntax:      0.15,
			Complexity:  0.10,
			Lint:        0.20,
			BlastRadius: 0.15,
			Tests:       0.30,
			Security:    0.10,
		},
	}
}

// Note: SimulationResult is defined in node.go with fields:
// - Score, Signals, Errors, Warnings, Duration, Tier, PromoteToNext

// Interfaces for simulation providers.
// Implementations should be lightweight and respect context cancellation.

// PatchValidator validates code syntax.
//
// Contract:
//   - CheckSyntax must be fast (< 100ms) and not make network calls
//   - Return true if the code is syntactically valid for the language
//   - Return false for syntax errors; do not panic
type PatchValidator interface {
	CheckSyntax(code, language string) bool
}

// LintRunner runs linting on code.
//
// Contract:
//   - Must respect ctx for cancellation
//   - Should complete within reasonable time (< 30s)
//   - Return error only for infrastructure failures, not lint findings
type LintRunner interface {
	LintContent(ctx context.Context, content []byte, language string) (*LintResult, error)
}

// LintResult contains linter output.
type LintResult struct {
	Valid    bool
	Errors   []string
	Warnings []string
}

// BlastRadiusAnalyzer analyzes change impact.
//
// Contract:
//   - Must respect ctx for cancellation
//   - Return TotalAffected as count of files/functions affected by change
//   - Return error only for infrastructure failures
type BlastRadiusAnalyzer interface {
	Analyze(ctx context.Context, filePath string, includeTests bool) (*BlastRadiusResult, error)
}

// BlastRadiusResult contains impact analysis.
type BlastRadiusResult struct {
	TotalAffected int      // Number of files/symbols affected
	AffectedFiles []string // Paths of affected files
}

// TestRunner executes tests.
//
// Contract:
//   - Must respect ctx for cancellation and timeout
//   - Return TestResult.Passed = true only if test passes
//   - Return error only for infrastructure failures (test failure is not an error)
type TestRunner interface {
	RunTest(ctx context.Context, testFile, testName string) (*TestResult, error)
}

// TestResult contains test execution output.
type TestResult struct {
	Passed   bool          // True if test passed
	Output   string        // Test output (stdout/stderr)
	Duration time.Duration // How long the test took
}

// SecurityScanner scans code for vulnerabilities.
//
// Contract:
//   - Must respect ctx for cancellation
//   - Return Score in range [0, 1] where 1 = no issues
//   - Return error only for infrastructure failures
type SecurityScanner interface {
	ScanCode(ctx context.Context, code string) (*SecurityScanResult, error)
}

// SecurityScanResult contains security scan output.
type SecurityScanResult struct {
	Score  float64
	Issues []SecurityIssue
}

// SecurityIssue represents a security finding.
type SecurityIssue struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Pattern  string `json:"pattern,omitempty"`
}

// Simulator provides tiered simulation for plan nodes.
//
// Thread Safety: Safe for concurrent use.
type Simulator struct {
	config SimulatorConfig

	// Optional providers (nil = skip that signal)
	validator       PatchValidator
	linter          LintRunner
	blastRadius     BlastRadiusAnalyzer
	testRunner      TestRunner
	securityScanner SecurityScanner
}

// NewSimulator creates a simulator with the given providers.
//
// Inputs:
//   - config: Simulator configuration.
//   - opts: Optional configuration via SimulatorOption functions.
//
// Outputs:
//   - *Simulator: Ready to use simulator.
func NewSimulator(config SimulatorConfig, opts ...SimulatorOption) *Simulator {
	s := &Simulator{config: config}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// SimulatorOption configures the simulator.
type SimulatorOption func(*Simulator)

// WithValidator sets the patch validator.
func WithValidator(v PatchValidator) SimulatorOption {
	return func(s *Simulator) { s.validator = v }
}

// WithLinter sets the lint runner.
func WithLinter(l LintRunner) SimulatorOption {
	return func(s *Simulator) { s.linter = l }
}

// WithBlastRadius sets the blast radius analyzer.
func WithBlastRadius(b BlastRadiusAnalyzer) SimulatorOption {
	return func(s *Simulator) { s.blastRadius = b }
}

// WithTestRunner sets the test runner.
func WithTestRunner(t TestRunner) SimulatorOption {
	return func(s *Simulator) { s.testRunner = t }
}

// WithSecurityScanner sets the security scanner.
func WithSecurityScanner(ss SecurityScanner) SimulatorOption {
	return func(s *Simulator) { s.securityScanner = ss }
}

// Simulate runs simulation at the specified tier.
//
// Inputs:
//   - ctx: Context for cancellation and timeout.
//   - node: The plan node to simulate.
//   - tier: The simulation tier to run.
//
// Outputs:
//   - *SimulationResult: Simulation results with score.
//   - error: Non-nil on context cancellation only.
func (s *Simulator) Simulate(ctx context.Context, node *PlanNode, tier SimulationTier) (*SimulationResult, error) {
	start := time.Now()

	result := &SimulationResult{
		Tier:    tier.String(),
		Signals: make(map[string]float64),
	}

	action := node.Action()
	if action == nil {
		result.Score = 0.5 // Neutral score for nodes without actions
		result.Duration = time.Since(start)
		return result, nil
	}

	// Validate action first
	if !action.IsValidated() {
		result.Errors = append(result.Errors, "action not validated")
		result.Score = 0
		result.Duration = time.Since(start)
		return result, nil
	}

	// Apply timeout based on tier
	timeout := s.timeoutForTier(tier)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Run tier-appropriate checks
	switch tier {
	case SimTierQuick:
		s.runQuickChecks(ctx, action, result)
	case SimTierStandard:
		s.runQuickChecks(ctx, action, result)
		s.runStandardChecks(ctx, action, result)
	case SimTierFull:
		s.runQuickChecks(ctx, action, result)
		s.runStandardChecks(ctx, action, result)
		s.runFullChecks(ctx, node, action, result)
	}

	// Calculate weighted score
	result.Score = s.calculateScore(result.Signals, tier)
	result.Duration = time.Since(start)

	// Determine if next tier should run
	result.PromoteToNext = s.shouldPromote(result.Score, tier)

	return result, nil
}

func (s *Simulator) timeoutForTier(tier SimulationTier) time.Duration {
	switch tier {
	case SimTierQuick:
		return s.config.QuickTimeout
	case SimTierStandard:
		return s.config.StandardTimeout
	case SimTierFull:
		return s.config.FullTimeout
	default:
		return s.config.QuickTimeout
	}
}

func (s *Simulator) runQuickChecks(ctx context.Context, action *PlannedAction, result *SimulationResult) {
	if ctx.Err() != nil {
		return
	}

	// Syntax check
	if s.validator != nil && action.CodeDiff != "" {
		if s.validator.CheckSyntax(action.CodeDiff, action.Language) {
			result.Signals["syntax"] = 1.0
		} else {
			result.Signals["syntax"] = 0.0
			result.Errors = append(result.Errors, "Syntax error in proposed code")
		}
	} else {
		result.Signals["syntax"] = 0.5 // Unknown
	}

	// Complexity estimation (fast)
	result.Signals["complexity"] = s.estimateComplexity(action.CodeDiff)
}

func (s *Simulator) runStandardChecks(ctx context.Context, action *PlannedAction, result *SimulationResult) {
	if ctx.Err() != nil {
		return
	}

	// Linter check
	if s.linter != nil && action.CodeDiff != "" {
		lintResult, err := s.linter.LintContent(ctx, []byte(action.CodeDiff), action.Language)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("linter error: %v", err))
			result.Signals["lint"] = 0.5 // Unknown on error
		} else if lintResult.Valid {
			result.Signals["lint"] = 1.0
		} else {
			result.Signals["lint"] = 0.3
			result.Warnings = append(result.Warnings, lintResult.Warnings...)
			result.Errors = append(result.Errors, lintResult.Errors...)
		}
	}
}

func (s *Simulator) runFullChecks(ctx context.Context, node *PlanNode, action *PlannedAction, result *SimulationResult) {
	if ctx.Err() != nil {
		return
	}

	// Blast radius analysis
	if s.blastRadius != nil && action.FilePath != "" {
		br, err := s.blastRadius.Analyze(ctx, action.FilePath, false)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("blast radius error: %v", err))
			result.Signals["blast_radius"] = 0.5
		} else {
			// Lower score for higher impact
			impactScore := 1.0 - float64(br.TotalAffected)/100.0
			if impactScore < 0 {
				impactScore = 0
			}
			result.Signals["blast_radius"] = impactScore
		}
	}

	// Test execution (if applicable)
	if s.testRunner != nil {
		if testFile, testName := s.getTestInfo(node); testFile != "" {
			testResult, err := s.testRunner.RunTest(ctx, testFile, testName)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("test error: %v", err))
				result.Signals["tests"] = 0.0
			} else if testResult.Passed {
				result.Signals["tests"] = 1.0
			} else {
				result.Signals["tests"] = 0.0
				result.Errors = append(result.Errors, "Test failed")
			}
		}
	}

	// Security scanning
	if s.securityScanner != nil && action.CodeDiff != "" {
		secResult, err := s.securityScanner.ScanCode(ctx, action.CodeDiff)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("security scan error: %v", err))
			result.Signals["security"] = 0.5
		} else {
			result.Signals["security"] = secResult.Score
			for _, issue := range secResult.Issues {
				if issue.Severity == "critical" || issue.Severity == "high" {
					result.Errors = append(result.Errors, fmt.Sprintf("security: %s", issue.Message))
				} else {
					result.Warnings = append(result.Warnings, fmt.Sprintf("security: %s", issue.Message))
				}
			}
		}
	}
}

func (s *Simulator) estimateComplexity(codeDiff string) float64 {
	// Simple complexity estimation based on approximate line count.
	// We estimate ~40 bytes per line as a reasonable average for code.
	//
	// Complexity scoring (lower complexity = higher score):
	//   < 5 lines   (~200 bytes)  → 0.9 (trivial change)
	//   < 20 lines  (~800 bytes)  → 0.7 (small change)
	//   < 50 lines  (~2000 bytes) → 0.5 (medium change)
	//   >= 50 lines              → 0.3 (large change)
	//
	// These thresholds are tuned for typical code editing scenarios
	// where smaller changes are generally safer and easier to review.
	lines := len([]byte(codeDiff)) / 40 // Approximate lines

	if lines < 5 {
		return 0.9
	} else if lines < 20 {
		return 0.7
	} else if lines < 50 {
		return 0.5
	}
	return 0.3
}

func (s *Simulator) getTestInfo(node *PlanNode) (testFile, testName string) {
	// Extract test information from node metadata
	// This would be populated during expansion if tests are available
	return "", ""
}

func (s *Simulator) calculateScore(signals map[string]float64, tier SimulationTier) float64 {
	var weights SignalWeights
	switch tier {
	case SimTierQuick:
		weights = s.config.QuickWeights
	case SimTierStandard:
		weights = s.config.StandardWeights
	case SimTierFull:
		weights = s.config.FullWeights
	}

	totalWeight := 0.0
	totalScore := 0.0

	if w := weights.Syntax; w > 0 {
		if v, ok := signals["syntax"]; ok {
			totalScore += v * w
			totalWeight += w
		}
	}
	if w := weights.Complexity; w > 0 {
		if v, ok := signals["complexity"]; ok {
			totalScore += v * w
			totalWeight += w
		}
	}
	if w := weights.Lint; w > 0 {
		if v, ok := signals["lint"]; ok {
			totalScore += v * w
			totalWeight += w
		}
	}
	if w := weights.BlastRadius; w > 0 {
		if v, ok := signals["blast_radius"]; ok {
			totalScore += v * w
			totalWeight += w
		}
	}
	if w := weights.Tests; w > 0 {
		if v, ok := signals["tests"]; ok {
			totalScore += v * w
			totalWeight += w
		}
	}
	if w := weights.Security; w > 0 {
		if v, ok := signals["security"]; ok {
			totalScore += v * w
			totalWeight += w
		}
	}

	if totalWeight == 0 {
		return 0.5 // Neutral if no signals
	}
	return totalScore / totalWeight
}

func (s *Simulator) shouldPromote(score float64, tier SimulationTier) bool {
	switch tier {
	case SimTierQuick:
		return score >= s.config.QuickScoreThreshold
	case SimTierStandard:
		return score >= s.config.StandardScoreThreshold
	default:
		return false
	}
}

// SimulateProgressive runs simulation progressively through tiers.
// Stops early if score is too low to warrant further analysis.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - node: The plan node to simulate.
//
// Outputs:
//   - *SimulationResult: Final simulation results.
//   - error: Non-nil on context cancellation.
func (s *Simulator) SimulateProgressive(ctx context.Context, node *PlanNode) (*SimulationResult, error) {
	// Start with quick tier
	result, err := s.Simulate(ctx, node, SimTierQuick)
	if err != nil {
		return nil, err
	}

	// Promote if score is good enough
	if result.PromoteToNext {
		result, err = s.Simulate(ctx, node, SimTierStandard)
		if err != nil {
			return nil, err
		}
	}

	if result.PromoteToNext {
		result, err = s.Simulate(ctx, node, SimTierFull)
		if err != nil {
			return nil, err
		}
	}

	return result, nil
}

// Config returns the simulator configuration.
func (s *Simulator) Config() SimulatorConfig {
	return s.config
}
