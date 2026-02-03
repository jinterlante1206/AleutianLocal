// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package risk

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/internal/impact"
	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/internal/initializer"
	"github.com/AleutianAI/AleutianFOSS/services/policy_engine"
)

// SignalCollector collects risk signals from various sources.
//
// # Thread Safety
//
// SignalCollector is safe for concurrent use.
type SignalCollector struct {
	index       *initializer.MemoryIndex
	projectRoot string
}

// NewSignalCollector creates a new SignalCollector.
//
// # Inputs
//
//   - index: The memory index for symbol lookups. Must not be nil.
//   - projectRoot: The root directory of the project.
//
// # Outputs
//
//   - *SignalCollector: The new collector.
func NewSignalCollector(index *initializer.MemoryIndex, projectRoot string) *SignalCollector {
	return &SignalCollector{
		index:       index,
		projectRoot: projectRoot,
	}
}

// CollectSignals collects all risk signals in parallel.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout. Must not be nil.
//   - cfg: Configuration specifying which signals to collect.
//   - changedFiles: Files that have changed.
//
// # Outputs
//
//   - *Signals: Collected signals (nil signals were skipped or failed).
//   - []error: Errors encountered during collection.
func (c *SignalCollector) CollectSignals(
	ctx context.Context,
	cfg Config,
	changedFiles []ChangedFile,
) (*Signals, []error) {
	if ctx == nil {
		return nil, []error{fmt.Errorf("ctx must not be nil")}
	}

	var (
		signals Signals
		errors  []error
		mu      sync.Mutex
		wg      sync.WaitGroup
	)

	// Set up per-signal timeout
	signalTimeout := time.Duration(cfg.SignalTimeout) * time.Second

	// Collect impact signal
	if !cfg.SkipImpact {
		wg.Add(1)
		go func() {
			defer wg.Done()
			signalCtx, cancel := context.WithTimeout(ctx, signalTimeout)
			defer cancel()

			result, err := c.collectImpactSignal(signalCtx, cfg, changedFiles)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errors = append(errors, fmt.Errorf("impact: %w", err))
			} else {
				signals.Impact = result
			}
		}()
	}

	// Collect policy signal
	if !cfg.SkipPolicy {
		wg.Add(1)
		go func() {
			defer wg.Done()
			signalCtx, cancel := context.WithTimeout(ctx, signalTimeout)
			defer cancel()

			result, err := c.collectPolicySignal(signalCtx, changedFiles)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errors = append(errors, fmt.Errorf("policy: %w", err))
			} else {
				signals.Policy = result
			}
		}()
	}

	// Collect complexity signal
	if !cfg.SkipComplexity {
		wg.Add(1)
		go func() {
			defer wg.Done()
			signalCtx, cancel := context.WithTimeout(ctx, signalTimeout)
			defer cancel()

			result, err := c.collectComplexitySignal(signalCtx, changedFiles)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errors = append(errors, fmt.Errorf("complexity: %w", err))
			} else {
				signals.Complexity = result
			}
		}()
	}

	wg.Wait()
	return &signals, errors
}

// collectImpactSignal runs impact analysis and converts to signal.
func (c *SignalCollector) collectImpactSignal(
	ctx context.Context,
	cfg Config,
	changedFiles []ChangedFile,
) (*ImpactSignal, error) {
	if c.index == nil {
		return nil, fmt.Errorf("no index available")
	}

	// Convert to file paths
	files := make([]string, len(changedFiles))
	for i, f := range changedFiles {
		files[i] = f.Path
	}

	// Create impact analyzer
	analyzer := impact.NewAnalyzer(c.index, c.projectRoot)

	// Configure impact analysis
	impactCfg := impact.DefaultConfig()
	impactCfg.Mode = impact.ChangeModeFiles
	impactCfg.Files = files

	// Run analysis
	result, err := analyzer.Analyze(ctx, impactCfg)
	if err != nil {
		return nil, fmt.Errorf("analyze: %w", err)
	}

	// Calculate impact score
	score := calculateImpactScore(result)

	// Build reasons
	reasons := make([]string, 0)
	if result.RiskFactors.DirectCallers > 0 {
		reasons = append(reasons, fmt.Sprintf("%d direct callers", result.RiskFactors.DirectCallers))
	}
	if result.RiskFactors.TransitiveCallers > 0 {
		reasons = append(reasons, fmt.Sprintf("%d transitive consumers", result.RiskFactors.TransitiveCallers))
	}
	if result.RiskFactors.IsSecurityPath {
		reasons = append(reasons, "Security-sensitive path")
	}
	if result.RiskFactors.IsPublicAPI {
		reasons = append(reasons, "Public API")
	}
	if result.RiskFactors.HasDBOperations {
		reasons = append(reasons, "Database operations")
	}
	if result.RiskFactors.HasIOOperations {
		reasons = append(reasons, "IO operations")
	}

	return &ImpactSignal{
		Score:             score,
		DirectCallers:     result.RiskFactors.DirectCallers,
		TransitiveCallers: result.RiskFactors.TransitiveCallers,
		IsSecurityPath:    result.RiskFactors.IsSecurityPath,
		IsPublicAPI:       result.RiskFactors.IsPublicAPI,
		HasDBOperations:   result.RiskFactors.HasDBOperations,
		HasIOOperations:   result.RiskFactors.HasIOOperations,
		AffectedPackages:  result.RiskFactors.AffectedPackages,
		AffectedTests:     result.RiskFactors.AffectedTests,
		Reasons:           reasons,
	}, nil
}

// calculateImpactScore converts impact result to normalized score (0-1).
func calculateImpactScore(result *impact.Result) float64 {
	score := 0.0

	// Fan-in scoring (up to 0.3)
	if result.RiskFactors.DirectCallers > 10 {
		score += 0.15
	} else if result.RiskFactors.DirectCallers > 5 {
		score += 0.1
	} else if result.RiskFactors.DirectCallers > 0 {
		score += 0.05
	}

	if result.RiskFactors.TransitiveCallers > 50 {
		score += 0.15
	} else if result.RiskFactors.TransitiveCallers > 20 {
		score += 0.1
	} else if result.RiskFactors.TransitiveCallers > 0 {
		score += 0.05
	}

	// Sensitivity scoring (up to 0.7)
	if result.RiskFactors.IsSecurityPath {
		score += 0.3
	}
	if result.RiskFactors.IsPublicAPI {
		score += 0.2
	}
	if result.RiskFactors.HasDBOperations {
		score += 0.1
	}
	if result.RiskFactors.HasIOOperations {
		score += 0.1
	}

	return math.Min(score, 1.0)
}

// collectPolicySignal runs policy check and converts to signal.
func (c *SignalCollector) collectPolicySignal(
	ctx context.Context,
	changedFiles []ChangedFile,
) (*PolicySignal, error) {
	// Initialize policy engine
	engine, err := policy_engine.NewPolicyEngine()
	if err != nil {
		return nil, fmt.Errorf("create policy engine: %w", err)
	}

	var (
		criticalCount int
		highCount     int
		mediumCount   int
		lowCount      int
		reasons       []string
	)

	// Scan each changed file
	for _, f := range changedFiles {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Skip deleted files
		if f.ChangeType == "D" {
			continue
		}

		// Read file content
		absPath := f.Path
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(c.projectRoot, f.Path)
		}

		// Scan content
		content, err := readFileContent(absPath)
		if err != nil {
			continue // Skip files we can't read
		}

		findings := engine.ScanFileContent(string(content))
		for _, finding := range findings {
			severity := classifyFinding(finding)
			switch severity {
			case "critical":
				criticalCount++
			case "high":
				highCount++
			case "medium":
				mediumCount++
			case "low":
				lowCount++
			}
		}
	}

	totalFound := criticalCount + highCount + mediumCount + lowCount

	if criticalCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d critical violations", criticalCount))
	}
	if highCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d high severity violations", highCount))
	}
	if mediumCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d medium severity violations", mediumCount))
	}

	// Calculate policy score
	score := calculatePolicyScore(criticalCount, highCount, mediumCount, lowCount)

	return &PolicySignal{
		Score:         score,
		TotalFound:    totalFound,
		CriticalCount: criticalCount,
		HighCount:     highCount,
		MediumCount:   mediumCount,
		LowCount:      lowCount,
		HasCritical:   criticalCount > 0,
		Reasons:       reasons,
	}, nil
}

// classifyFinding maps a policy finding to severity.
func classifyFinding(f policy_engine.ScanFinding) string {
	classLower := strings.ToLower(f.ClassificationName)

	if strings.Contains(classLower, "secret") ||
		strings.Contains(classLower, "credential") ||
		strings.Contains(classLower, "password") {
		if f.Confidence == policy_engine.High {
			return "critical"
		}
		return "high"
	}

	if strings.Contains(classLower, "pii") ||
		strings.Contains(classLower, "personal") {
		if f.Confidence == policy_engine.High {
			return "high"
		}
		return "medium"
	}

	switch f.Confidence {
	case policy_engine.High:
		return "high"
	case policy_engine.Medium:
		return "medium"
	default:
		return "low"
	}
}

// calculatePolicyScore converts policy counts to normalized score (0-1).
func calculatePolicyScore(critical, high, medium, low int) float64 {
	// Weighted sum with diminishing returns
	score := 0.0

	// Critical violations have maximum impact
	if critical > 0 {
		score += 0.6 + math.Min(float64(critical-1)*0.1, 0.4)
	}

	// High violations contribute significantly
	score += math.Min(float64(high)*0.15, 0.3)

	// Medium violations contribute moderately
	score += math.Min(float64(medium)*0.05, 0.2)

	// Low violations contribute minimally
	score += math.Min(float64(low)*0.02, 0.1)

	return math.Min(score, 1.0)
}

// collectComplexitySignal calculates complexity metrics.
func (c *SignalCollector) collectComplexitySignal(
	ctx context.Context,
	changedFiles []ChangedFile,
) (*ComplexitySignal, error) {
	var (
		linesAdded   int
		linesRemoved int
		filesChanged int
	)

	filesChanged = len(changedFiles)

	for _, f := range changedFiles {
		linesAdded += f.LinesAdded
		linesRemoved += f.LinesRemoved
	}

	// Build reasons
	reasons := make([]string, 0)
	totalLines := linesAdded + linesRemoved
	if totalLines > 500 {
		reasons = append(reasons, fmt.Sprintf("Large change: %d lines", totalLines))
	}
	if filesChanged > 10 {
		reasons = append(reasons, fmt.Sprintf("Many files: %d files changed", filesChanged))
	}
	if linesRemoved > linesAdded*2 {
		reasons = append(reasons, "Significant code removal (potential refactoring)")
	}

	// Calculate complexity score
	score := calculateComplexityScore(linesAdded, linesRemoved, filesChanged, 0)

	return &ComplexitySignal{
		Score:           score,
		LinesAdded:      linesAdded,
		LinesRemoved:    linesRemoved,
		FilesChanged:    filesChanged,
		CyclomaticDelta: 0, // Not calculated in this version
		Reasons:         reasons,
	}, nil
}

// calculateComplexityScore converts complexity metrics to normalized score (0-1).
func calculateComplexityScore(added, removed, files, cyclomatic int) float64 {
	// Large changes (>500 lines) are higher risk
	lineScore := math.Min(float64(added+removed)/MaxLinesForScore, 1.0)

	// Many files (>10) are higher risk
	fileScore := math.Min(float64(files)/MaxFilesForScore, 1.0)

	// Increased complexity is higher risk
	complexityScore := 0.0
	if cyclomatic > 0 {
		complexityScore = math.Min(float64(cyclomatic)/MaxComplexityForScore, 1.0)
	}

	// Weighted combination
	return lineScore*0.3 + fileScore*0.3 + complexityScore*0.4
}

// GetChangedFiles gets the list of changed files using git.
//
// # Inputs
//
//   - ctx: Context for cancellation. Must not be nil.
//   - cfg: Configuration specifying change mode.
//   - projectRoot: Root directory of the project.
//
// # Outputs
//
//   - []ChangedFile: List of changed files with metadata.
//   - error: Non-nil on failure.
func GetChangedFiles(ctx context.Context, cfg Config, projectRoot string) ([]ChangedFile, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}

	switch cfg.Mode {
	case ChangeModeFiles:
		files := make([]ChangedFile, len(cfg.Files))
		for i, f := range cfg.Files {
			files[i] = ChangedFile{Path: f, ChangeType: "M"}
		}
		return files, nil
	case ChangeModeDiff:
		return getGitDiff(ctx, projectRoot, []string{"diff", "--numstat"})
	case ChangeModeStaged:
		return getGitDiff(ctx, projectRoot, []string{"diff", "--cached", "--numstat"})
	case ChangeModeCommit:
		if cfg.CommitHash == "" {
			return nil, fmt.Errorf("commit hash required for commit mode")
		}
		return getGitDiff(ctx, projectRoot, []string{"diff", "--numstat", cfg.CommitHash + "^", cfg.CommitHash})
	case ChangeModeBranch:
		baseBranch := cfg.BaseBranch
		if baseBranch == "" {
			baseBranch = "main"
		}
		mergeBase, err := getGitMergeBase(ctx, projectRoot, baseBranch)
		if err != nil {
			return nil, fmt.Errorf("get merge base: %w", err)
		}
		return getGitDiff(ctx, projectRoot, []string{"diff", "--numstat", mergeBase})
	default:
		return nil, fmt.Errorf("unknown change mode: %s", cfg.Mode)
	}
}

// getGitDiff runs git diff and parses numstat output.
func getGitDiff(ctx context.Context, projectRoot string, args []string) ([]ChangedFile, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = projectRoot

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return parseNumstat(output)
}

// parseNumstat parses git diff --numstat output.
func parseNumstat(output []byte) ([]ChangedFile, error) {
	var files []ChangedFile

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}

		added, _ := strconv.Atoi(parts[0])
		removed, _ := strconv.Atoi(parts[1])
		path := parts[2]

		// Handle renames (path contains => )
		if strings.Contains(path, "=>") {
			// Format: old_path => new_path or {old_path => new_path}
			parts := strings.Split(path, " => ")
			if len(parts) == 2 {
				path = strings.Trim(parts[1], "{}")
			}
		}

		changeType := "M"
		if added > 0 && removed == 0 {
			changeType = "A"
		} else if added == 0 && removed > 0 {
			changeType = "D"
		}

		files = append(files, ChangedFile{
			Path:         path,
			ChangeType:   changeType,
			LinesAdded:   added,
			LinesRemoved: removed,
		})
	}

	return files, scanner.Err()
}

// getGitMergeBase finds the merge base between HEAD and a branch.
func getGitMergeBase(ctx context.Context, projectRoot, branch string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "merge-base", branch, "HEAD")
	cmd.Dir = projectRoot

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git merge-base: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// readFileContent reads file content, returning empty on error.
func readFileContent(path string) ([]byte, error) {
	cmd := exec.Command("cat", path)
	return cmd.Output()
}
