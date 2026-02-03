// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package lint

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// LINT RUNNER
// =============================================================================

// LintRunner executes linters and processes their output.
//
// Description:
//
//	Manages linter execution, output parsing, and policy application.
//	Detects available linters at startup and provides graceful fallback
//	when linters are not installed.
//
// Thread Safety: Safe for concurrent use.
type LintRunner struct {
	configs    *ConfigRegistry
	policies   *PolicyRegistry
	available  map[string]bool
	availMu    sync.RWMutex
	workingDir string
}

// Option configures the LintRunner.
type Option func(*LintRunner)

// WithWorkingDir sets the working directory for linter execution.
func WithWorkingDir(dir string) Option {
	return func(r *LintRunner) {
		r.workingDir = dir
	}
}

// WithConfigs sets a custom config registry.
func WithConfigs(configs *ConfigRegistry) Option {
	return func(r *LintRunner) {
		r.configs = configs
	}
}

// WithPolicies sets a custom policy registry.
func WithPolicies(policies *PolicyRegistry) Option {
	return func(r *LintRunner) {
		r.policies = policies
	}
}

// NewLintRunner creates a new lint runner.
//
// Description:
//
//	Creates a runner with default or custom configurations.
//	Call DetectAvailableLinters to check which linters are installed.
//
// Inputs:
//
//	opts - Optional configuration options
//
// Outputs:
//
//	*LintRunner - The configured runner
func NewLintRunner(opts ...Option) *LintRunner {
	r := &LintRunner{
		configs:   NewConfigRegistry(),
		policies:  NewPolicyRegistry(),
		available: make(map[string]bool),
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// DetectAvailableLinters checks which linters are installed.
//
// Description:
//
//	Probes the system PATH for each configured linter binary.
//	Updates the Available flag in configurations and returns
//	a map of language to availability.
//
// Outputs:
//
//	map[string]bool - Map of language to whether linter is available
//
// Thread Safety: Safe for concurrent use.
func (r *LintRunner) DetectAvailableLinters() map[string]bool {
	r.availMu.Lock()
	defer r.availMu.Unlock()

	result := make(map[string]bool)

	for _, lang := range r.configs.Languages() {
		config := r.configs.Get(lang)
		if config == nil {
			continue
		}

		_, err := exec.LookPath(config.Command)
		available := err == nil

		r.available[lang] = available
		r.configs.SetAvailable(lang, available)
		result[lang] = available

		if available {
			slog.Info("Linter available",
				slog.String("language", lang),
				slog.String("command", config.Command),
			)
		} else {
			slog.Warn("Linter not installed",
				slog.String("language", lang),
				slog.String("command", config.Command),
			)
		}
	}

	return result
}

// IsAvailable returns whether a linter is available for a language.
//
// Description:
//
//	Checks if the linter for the given language has been detected
//	as available. Returns false if language is unknown or linter
//	is not installed.
//
// Inputs:
//
//	language - The language identifier
//
// Outputs:
//
//	bool - True if linter is available
//
// Thread Safety: Safe for concurrent use.
func (r *LintRunner) IsAvailable(language string) bool {
	r.availMu.RLock()
	defer r.availMu.RUnlock()
	return r.available[language]
}

// Lint runs the linter on a file.
//
// Description:
//
//	Detects the language from the file path, runs the appropriate
//	linter, parses output, and applies policy to categorize issues.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout
//	filePath - Path to the file to lint (absolute or relative to workingDir)
//
// Outputs:
//
//	*LintResult - The lint result with categorized issues
//	error - Non-nil if the linter failed to execute
//
// Errors:
//
//	ErrUnsupportedLanguage - No linter for the file type
//	ErrLinterNotInstalled - Linter not found in PATH
//	ErrLinterTimeout - Linter exceeded timeout
//	ErrLinterFailed - Linter process failed
//
// Thread Safety: Safe for concurrent use.
func (r *LintRunner) Lint(ctx context.Context, filePath string) (*LintResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: ctx must not be nil", ErrInvalidInput)
	}

	// Detect language
	language := LanguageFromPath(filePath)
	if language == "" {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedLanguage, filepath.Ext(filePath))
	}

	return r.LintWithLanguage(ctx, filePath, language)
}

// LintWithLanguage runs the linter for a specific language on a file.
//
// Description:
//
//	Like Lint, but with explicit language specification.
//	Useful when the file extension doesn't match the language.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout
//	filePath - Path to the file to lint
//	language - The language identifier
//
// Outputs:
//
//	*LintResult - The lint result with categorized issues
//	error - Non-nil if the linter failed to execute
//
// Thread Safety: Safe for concurrent use.
func (r *LintRunner) LintWithLanguage(ctx context.Context, filePath, language string) (*LintResult, error) {
	// Start tracing span
	ctx, span := startLintSpan(ctx, language, filePath)
	defer span.End()
	start := time.Now()

	// Get config
	config := r.configs.Get(language)
	if config == nil {
		recordLintMetrics(ctx, language, time.Since(start), 0, 0, false)
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedLanguage, language)
	}

	// Check availability
	if !r.IsAvailable(language) {
		// Return empty result with flag indicating linter unavailable
		setLintSpanResult(span, 0, 0, false)
		recordLintMetrics(ctx, language, time.Since(start), 0, 0, true)
		return &LintResult{
			Valid:           true, // Don't block when linter unavailable
			Errors:          make([]LintIssue, 0),
			Warnings:        make([]LintIssue, 0),
			Duration:        time.Since(start),
			Linter:          config.Command,
			Language:        language,
			FilePath:        filePath,
			LinterAvailable: false,
		}, nil
	}

	// Resolve file path
	absPath := filePath
	if !filepath.IsAbs(filePath) {
		if r.workingDir != "" {
			absPath = filepath.Join(r.workingDir, filePath)
		} else {
			var err error
			absPath, err = filepath.Abs(filePath)
			if err != nil {
				return nil, fmt.Errorf("resolving path: %w", err)
			}
		}
	}

	// Execute linter
	output, err := r.executeLinter(ctx, config, absPath)
	if err != nil {
		recordLintMetrics(ctx, language, time.Since(start), 0, 0, false)
		return nil, err
	}

	// Parse output
	issues, err := r.parseOutput(language, output)
	if err != nil {
		recordLintMetrics(ctx, language, time.Since(start), 0, 0, false)
		return nil, fmt.Errorf("%w: %v", ErrParseOutput, err)
	}

	// Apply policy
	policy := r.policies.Get(language)
	errors, warnings, infos := ApplyPolicy(issues, policy)

	result := &LintResult{
		Valid:           len(errors) == 0,
		Errors:          errors,
		Warnings:        warnings,
		Infos:           infos,
		Duration:        time.Since(start),
		Linter:          config.Command,
		Language:        language,
		FilePath:        filePath,
		LinterAvailable: true,
	}

	// Record successful lint metrics
	setLintSpanResult(span, len(errors), len(warnings), true)
	recordLintMetrics(ctx, language, time.Since(start), len(errors), len(warnings), true)

	slog.Debug("Lint completed",
		slog.String("file", filePath),
		slog.String("linter", config.Command),
		slog.Duration("duration", result.Duration),
		slog.Int("errors", len(errors)),
		slog.Int("warnings", len(warnings)),
	)

	return result, nil
}

// LintContent runs the linter on content directly.
//
// Description:
//
//	Writes content to a temp file, runs the linter, then cleans up.
//	File paths in results are remapped to indicate temp file origin.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout
//	content - The source code to lint
//	language - The language identifier (e.g., "go", "python")
//
// Outputs:
//
//	*LintResult - The lint result with categorized issues
//	error - Non-nil if the linter failed
//
// Thread Safety: Safe for concurrent use.
func (r *LintRunner) LintContent(ctx context.Context, content []byte, language string) (*LintResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: ctx must not be nil", ErrInvalidInput)
	}
	if len(content) == 0 {
		return &LintResult{
			Valid:           true,
			Errors:          make([]LintIssue, 0),
			Warnings:        make([]LintIssue, 0),
			Language:        language,
			LinterAvailable: r.IsAvailable(language),
		}, nil
	}

	// Get extension for temp file
	ext := ExtensionForLanguage(language)
	if ext == "" {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedLanguage, language)
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "lint-*"+ext)
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Write content
	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return nil, fmt.Errorf("writing temp file: %w", err)
	}
	tmpFile.Close()

	// Run linter
	result, err := r.LintWithLanguage(ctx, tmpPath, language)
	if err != nil {
		return nil, err
	}

	// Remap file paths in results
	result.FilePath = "<content>"
	for i := range result.Errors {
		if result.Errors[i].File == tmpPath {
			result.Errors[i].File = "<content>"
		}
	}
	for i := range result.Warnings {
		if result.Warnings[i].File == tmpPath {
			result.Warnings[i].File = "<content>"
		}
	}
	for i := range result.Infos {
		if result.Infos[i].File == tmpPath {
			result.Infos[i].File = "<content>"
		}
	}

	return result, nil
}

// executeLinter runs the linter subprocess.
func (r *LintRunner) executeLinter(ctx context.Context, config *LinterConfig, filePath string) ([]byte, error) {
	// Build command
	args := make([]string, len(config.Args))
	copy(args, config.Args)
	args = append(args, filePath)

	// Create command with timeout
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, config.Command, args...)

	// Set working directory
	if r.workingDir != "" {
		cmd.Dir = r.workingDir
	} else {
		cmd.Dir = filepath.Dir(filePath)
	}

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run
	err := cmd.Run()

	// Check for timeout
	if cmdCtx.Err() == context.DeadlineExceeded {
		return nil, NewLinterError(config.Command, config.Language, ErrLinterTimeout).
			WithOutput(stderr.String())
	}

	// Check for context cancellation
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Some linters exit with non-zero when they find issues
	// Only fail if there's no stdout output (actual failure)
	if err != nil && stdout.Len() == 0 {
		return nil, NewLinterError(config.Command, config.Language, ErrLinterFailed).
			WithOutput(stderr.String())
	}

	return stdout.Bytes(), nil
}

// parseOutput parses linter JSON output based on language.
func (r *LintRunner) parseOutput(language string, output []byte) ([]LintIssue, error) {
	// Skip empty output
	if len(bytes.TrimSpace(output)) == 0 {
		return nil, nil
	}

	parser := GetParser(language)
	if parser == nil {
		return nil, fmt.Errorf("no parser for language: %s", language)
	}

	return parser(output)
}

// Configs returns the config registry for customization.
func (r *LintRunner) Configs() *ConfigRegistry {
	return r.configs
}

// Policies returns the policy registry for customization.
func (r *LintRunner) Policies() *PolicyRegistry {
	return r.policies
}

// =============================================================================
// BATCH OPERATIONS
// =============================================================================

// LintFiles runs the linter on multiple files concurrently.
//
// Description:
//
//	Lints multiple files in parallel using goroutines.
//	Results are returned in the same order as input files.
//
// Inputs:
//
//	ctx - Context for cancellation
//	filePaths - Paths to files to lint
//
// Outputs:
//
//	[]*LintResult - Results in same order as input
//	error - Non-nil if any file failed to lint
//
// Thread Safety: Safe for concurrent use.
func (r *LintRunner) LintFiles(ctx context.Context, filePaths []string) ([]*LintResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: ctx must not be nil", ErrInvalidInput)
	}

	results := make([]*LintResult, len(filePaths))
	errs := make([]error, len(filePaths))

	var wg sync.WaitGroup
	for i, path := range filePaths {
		wg.Add(1)
		go func(idx int, filePath string) {
			defer wg.Done()
			result, err := r.Lint(ctx, filePath)
			results[idx] = result
			errs[idx] = err
		}(i, path)
	}
	wg.Wait()

	// Check for first error
	for i, err := range errs {
		if err != nil {
			return results, fmt.Errorf("linting %s: %w", filePaths[i], err)
		}
	}

	return results, nil
}

// LintDirectory runs the linter on all supported files in a directory.
//
// Description:
//
//	Recursively finds all files with supported extensions and lints them.
//	Skips vendor, node_modules, and hidden directories.
//
// Inputs:
//
//	ctx - Context for cancellation
//	dirPath - Path to directory to lint
//
// Outputs:
//
//	[]*LintResult - Results for each file
//	error - Non-nil if directory walk or linting failed
//
// Thread Safety: Safe for concurrent use.
func (r *LintRunner) LintDirectory(ctx context.Context, dirPath string) ([]*LintResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: ctx must not be nil", ErrInvalidInput)
	}

	// Collect files
	var files []string
	err := filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip hidden and common vendor directories
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		// Check if we have a linter for this file type
		language := LanguageFromPath(path)
		if language != "" && r.IsAvailable(language) {
			files = append(files, path)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}

	if len(files) == 0 {
		return nil, nil
	}

	return r.LintFiles(ctx, files)
}
