// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package analysis

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/validation"
)

// ChurnAnalyzer analyzes git history for code churn.
//
// # Description
//
// Examines git history to determine how often code changes, which can
// indicate instability or active development. High churn code may need
// extra attention during modifications.
//
// # Security
//
// Uses exec.Command with explicit arguments (no shell). Validates all
// file paths before passing to git. Enforces timeouts and output limits.
//
// # Thread Safety
//
// Safe for concurrent use. Git log results are cached with TTL.
type ChurnAnalyzer struct {
	repoPath  string
	cache     sync.Map      // file path -> *gitLogCache
	cacheTTL  time.Duration // Default: 1 minute
	validator *validation.InputValidator

	// Git command configuration
	gitTimeout    time.Duration // Default: 10 seconds
	maxOutputSize int           // Default: 1MB
}

// gitLogCache stores cached git log results.
type gitLogCache struct {
	commits     []gitCommit
	lastChecked time.Time
}

// gitCommit represents a parsed git commit.
type gitCommit struct {
	Hash      string
	Author    string
	Date      time.Time
	Message   string
	IsBugFix  bool
	IssueRefs []string
}

// Verify interface compliance at compile time
var _ Enricher = (*ChurnAnalyzer)(nil)

// NewChurnAnalyzer creates a churn analyzer for the given repository.
//
// # Description
//
// Creates a ChurnAnalyzer that queries git history for code churn metrics.
// If git is not available or the path is not a git repository, the analyzer
// will return gracefully with nil ChurnScore.
//
// # Inputs
//
//   - repoPath: Absolute path to the repository root.
//   - validator: Input validator. If nil, a default is created.
//
// # Outputs
//
//   - *ChurnAnalyzer: Ready-to-use analyzer.
//
// # Example
//
//	analyzer := NewChurnAnalyzer("/path/to/repo", validator)
func NewChurnAnalyzer(repoPath string, validator *validation.InputValidator) *ChurnAnalyzer {
	if validator == nil {
		validator = validation.NewInputValidator(nil)
	}

	return &ChurnAnalyzer{
		repoPath:      repoPath,
		cacheTTL:      1 * time.Minute,
		validator:     validator,
		gitTimeout:    10 * time.Second,
		maxOutputSize: 1 << 20, // 1MB
	}
}

// Name returns the enricher identifier.
func (a *ChurnAnalyzer) Name() string {
	return "churn"
}

// Priority returns 2 (secondary analysis).
func (a *ChurnAnalyzer) Priority() int {
	return 2
}

// Enrich analyzes git history for the target symbol's file.
//
// # Description
//
// Queries git log to count commits, identify bug fixes, and determine
// code stability. Populates result.ChurnScore with findings.
//
// # Graceful Degradation
//
// Returns nil ChurnScore (no error) if:
//   - Git is not available
//   - Path is not in a git repository
//   - Git command fails for any reason
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - target: The symbol to analyze.
//   - result: The result to enrich.
//
// # Outputs
//
//   - error: Non-nil only on context cancellation.
func (a *ChurnAnalyzer) Enrich(
	ctx context.Context,
	target *EnrichmentTarget,
	result *EnhancedBlastRadius,
) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Get file path from target
	filePath := ""
	if target.Symbol != nil {
		filePath = target.Symbol.FilePath
	} else {
		// Extract from symbol ID
		filePath = extractFileFromSymbolID(target.SymbolID)
	}

	if filePath == "" {
		return nil // Can't analyze without file path
	}

	// Validate file path
	if err := a.validator.ValidateFilePath(filePath); err != nil {
		return nil // Invalid path - graceful degradation
	}

	// Check cache first
	if cached, ok := a.getFromCache(filePath); ok {
		result.ChurnScore = a.calculateChurnScore(cached.commits)
		return nil
	}

	// Query git
	commits, err := a.getGitLog(ctx, filePath)
	if err != nil {
		// Graceful degradation - don't fail on git errors
		return nil
	}

	// Cache results
	a.setCache(filePath, commits)

	// Calculate and set churn score
	result.ChurnScore = a.calculateChurnScore(commits)

	return nil
}

// getGitLog queries git for commit history of a file.
func (a *ChurnAnalyzer) getGitLog(ctx context.Context, filePath string) ([]gitCommit, error) {
	// Prepare git arguments
	// We want commits from last 90 days affecting this file
	since := time.Now().AddDate(0, 0, -90).Format("2006-01-02")
	args := []string{
		"log",
		"--since=" + since,
		"--format=%H|%an|%ai|%s",
		"--follow",
		"--",
		filePath,
	}

	// Execute git command
	output, err := a.executeGit(ctx, args...)
	if err != nil {
		return nil, err
	}

	// Parse output
	return a.parseGitLog(output)
}

// executeGit runs a git command with safety guardrails.
//
// # Security
//
//   - Uses exec.Command with explicit args (NO shell=true)
//   - Enforces timeout via context (10s max)
//   - Validates file paths before use
//   - Limits output size (1MB max)
//   - Rejects args with shell metacharacters
func (a *ChurnAnalyzer) executeGit(ctx context.Context, args ...string) ([]byte, error) {
	// Validate args don't contain shell metacharacters
	if err := a.validator.ValidateGitArgs(args); err != nil {
		return nil, fmt.Errorf("invalid git argument: %w", err)
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, a.gitTimeout)
	defer cancel()

	// Create command
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = a.repoPath

	// Capture output with size limit
	var stdout, stderr bytes.Buffer
	stdout.Grow(a.maxOutputSize)
	cmd.Stdout = &limitedWriter{w: &stdout, limit: a.maxOutputSize}
	cmd.Stderr = &stderr

	// Run command
	err := cmd.Run()
	if err != nil {
		// Check for context cancellation
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("git command failed: %w: %s", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

// limitedWriter limits the amount of data that can be written.
type limitedWriter struct {
	w       *bytes.Buffer
	limit   int
	written int
}

func (lw *limitedWriter) Write(p []byte) (n int, err error) {
	remaining := lw.limit - lw.written
	if remaining <= 0 {
		return 0, nil // Silently discard
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	n, err = lw.w.Write(p)
	lw.written += n
	return n, err
}

// parseGitLog parses git log output.
func (a *ChurnAnalyzer) parseGitLog(output []byte) ([]gitCommit, error) {
	var commits []gitCommit

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}

		hash := parts[0]
		author := parts[1]
		dateStr := parts[2]
		message := parts[3]

		// Parse date
		date, err := time.Parse("2006-01-02 15:04:05 -0700", dateStr)
		if err != nil {
			// Try alternative format
			date, _ = time.Parse("2006-01-02", dateStr[:10])
		}

		commit := gitCommit{
			Hash:      hash,
			Author:    author,
			Date:      date,
			Message:   message,
			IsBugFix:  isBugFixCommit(message),
			IssueRefs: extractIssueRefs(message),
		}

		commits = append(commits, commit)
	}

	return commits, scanner.Err()
}

// isBugFixCommit checks if a commit message indicates a bug fix.
func isBugFixCommit(message string) bool {
	lower := strings.ToLower(message)
	bugPatterns := []string{"fix", "bug", "hotfix", "patch", "resolve", "issue"}
	for _, pattern := range bugPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// issueRefPattern matches issue references like #123, JIRA-123, etc.
var issueRefPattern = regexp.MustCompile(`(?i)(#\d+|[A-Z]+-\d+)`)

// extractIssueRefs extracts issue references from commit message.
func extractIssueRefs(message string) []string {
	matches := issueRefPattern.FindAllString(message, -1)
	if len(matches) == 0 {
		return nil
	}
	return matches
}

// calculateChurnScore computes the churn score from commits.
func (a *ChurnAnalyzer) calculateChurnScore(commits []gitCommit) *ChurnScore {
	if len(commits) == 0 {
		return &ChurnScore{
			ChurnLevel: ChurnLevelLow,
		}
	}

	now := time.Now()
	thirtyDaysAgo := now.AddDate(0, 0, -30)
	ninetyDaysAgo := now.AddDate(0, 0, -90)

	var changes30 int
	var changes90 int
	var bugFixes int
	var lastModified int64
	contributors := make(map[string]bool)

	for _, commit := range commits {
		if commit.Date.After(thirtyDaysAgo) {
			changes30++
		}
		if commit.Date.After(ninetyDaysAgo) {
			changes90++
		}
		if commit.IsBugFix || len(commit.IssueRefs) > 0 {
			bugFixes++
		}
		if commit.Date.UnixMilli() > lastModified {
			lastModified = commit.Date.UnixMilli()
		}
		contributors[commit.Author] = true
	}

	contributorList := make([]string, 0, len(contributors))
	for author := range contributors {
		contributorList = append(contributorList, author)
	}

	return &ChurnScore{
		ChangesLast30Days: changes30,
		ChangesLast90Days: changes90,
		BugReportsLinked:  bugFixes,
		LastModified:      lastModified,
		ChurnLevel:        GetChurnLevel(changes30),
		Contributors:      contributorList,
	}
}

// getFromCache retrieves cached git log if still valid.
func (a *ChurnAnalyzer) getFromCache(filePath string) (*gitLogCache, bool) {
	val, ok := a.cache.Load(filePath)
	if !ok {
		return nil, false
	}

	cached := val.(*gitLogCache)
	if time.Since(cached.lastChecked) > a.cacheTTL {
		a.cache.Delete(filePath)
		return nil, false
	}

	return cached, true
}

// setCache stores git log results in cache.
func (a *ChurnAnalyzer) setCache(filePath string, commits []gitCommit) {
	a.cache.Store(filePath, &gitLogCache{
		commits:     commits,
		lastChecked: time.Now(),
	})
}

// extractFileFromSymbolID extracts file path from symbol ID.
// Symbol ID format: "path/to/file.go:line:name"
func extractFileFromSymbolID(symbolID string) string {
	// Find the first colon that's followed by a number
	for i := 0; i < len(symbolID); i++ {
		if symbolID[i] == ':' {
			// Check if next char is a digit
			if i+1 < len(symbolID) {
				_, err := strconv.Atoi(string(symbolID[i+1]))
				if err == nil {
					return symbolID[:i]
				}
			}
		}
	}
	return symbolID
}

// SetCacheTTL sets the cache time-to-live.
func (a *ChurnAnalyzer) SetCacheTTL(ttl time.Duration) {
	a.cacheTTL = ttl
}

// ClearCache clears all cached git log results.
func (a *ChurnAnalyzer) ClearCache() {
	a.cache.Range(func(key, value interface{}) bool {
		a.cache.Delete(key)
		return true
	})
}
