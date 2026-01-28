// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package error_audit

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/safety"
)

// ErrorAuditorImpl implements the safety.ErrorAuditor interface.
//
// Description:
//
//	ErrorAuditorImpl detects error handling issues that could lead to
//	security vulnerabilities, including fail-open patterns, information
//	leaks, and swallowed errors.
//
// Thread Safety:
//
//	ErrorAuditorImpl is safe for concurrent use after initialization.
type ErrorAuditorImpl struct {
	graph            *graph.Graph
	idx              *index.SymbolIndex
	infoLeakPatterns []*InfoLeakPattern
	failOpenPatterns map[string]*FailOpenPattern

	// File content cache
	fileCache   map[string]string
	fileCacheMu sync.RWMutex
}

// NewErrorAuditor creates a new error auditor.
//
// Description:
//
//	Creates an auditor with default patterns for detecting error
//	handling issues across multiple languages.
//
// Inputs:
//
//	g - The code graph.
//	idx - The symbol index.
//
// Outputs:
//
//	*ErrorAuditorImpl - The configured auditor.
func NewErrorAuditor(g *graph.Graph, idx *index.SymbolIndex) *ErrorAuditorImpl {
	return &ErrorAuditorImpl{
		graph:            g,
		idx:              idx,
		infoLeakPatterns: DefaultInfoLeakPatterns,
		failOpenPatterns: DefaultFailOpenPatterns,
		fileCache:        make(map[string]string),
	}
}

// AuditErrorHandling audits error handling in a scope.
//
// Description:
//
//	Analyzes error handling patterns to detect:
//	  - Fail-open conditions (missing returns after error checks)
//	  - Information leaks (stack traces, internal paths in responses)
//	  - Swallowed errors (empty catch blocks)
//
// Inputs:
//
//	ctx - Context for cancellation and timeout.
//	scope - The scope to audit (package path or file path).
//	opts - Optional configuration (focus area, etc.).
//
// Outputs:
//
//	*safety.ErrorAudit - The audit result with issues found.
//	error - Non-nil if scope not found or operation canceled.
//
// Errors:
//
//	safety.ErrInvalidInput - Scope is empty.
//	safety.ErrContextCanceled - Context was canceled.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (a *ErrorAuditorImpl) AuditErrorHandling(
	ctx context.Context,
	scope string,
	opts ...safety.AuditOption,
) (*safety.ErrorAudit, error) {
	start := time.Now()

	if ctx == nil {
		return nil, safety.ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, safety.ErrContextCanceled
	}

	if scope == "" {
		return nil, safety.ErrInvalidInput
	}

	// Apply options
	config := safety.DefaultAuditConfig()
	config.ApplyOptions(opts...)

	// Find files in scope
	files := a.findFilesInScope(scope)
	if len(files) == 0 {
		return &safety.ErrorAudit{
			Scope:    scope,
			Issues:   []safety.ErrorIssue{},
			Summary:  safety.ErrorSummary{},
			Duration: time.Since(start),
		}, nil
	}

	var allIssues []safety.ErrorIssue
	var mu sync.Mutex
	var wg sync.WaitGroup

	summary := safety.ErrorSummary{}

	for filePath, lang := range files {
		if ctx.Err() != nil {
			break
		}

		wg.Add(1)
		go func(fp, language string) {
			defer wg.Done()

			content := a.getFileContent(fp)
			if content == "" {
				return
			}

			// Run detection methods in parallel within each file
			var issues []safety.ErrorIssue
			var innerWg sync.WaitGroup
			var innerMu sync.Mutex

			// Fail-open detection
			if config.Focus == "all" || config.Focus == "fail_open" {
				innerWg.Add(1)
				go func() {
					defer innerWg.Done()
					failOpenIssues := a.detectFailOpen(fp, content, language)
					if len(failOpenIssues) > 0 {
						innerMu.Lock()
						issues = append(issues, failOpenIssues...)
						innerMu.Unlock()
					}
				}()
			}

			// Info leak detection
			if config.Focus == "all" || config.Focus == "info_leak" {
				innerWg.Add(1)
				go func() {
					defer innerWg.Done()
					infoLeakIssues := a.detectInfoLeaks(fp, content)
					if len(infoLeakIssues) > 0 {
						innerMu.Lock()
						issues = append(issues, infoLeakIssues...)
						innerMu.Unlock()
					}
				}()
			}

			// Swallowed error detection
			if config.Focus == "all" {
				innerWg.Add(1)
				go func() {
					defer innerWg.Done()
					swallowedIssues := a.detectSwallowedErrors(fp, content, language)
					if len(swallowedIssues) > 0 {
						innerMu.Lock()
						issues = append(issues, swallowedIssues...)
						innerMu.Unlock()
					}
				}()
			}

			innerWg.Wait()

			mu.Lock()
			allIssues = append(allIssues, issues...)

			// Update summary
			for _, issue := range issues {
				summary.TotalErrors++
				switch issue.Type {
				case "fail_open":
					summary.FailOpenPaths++
				case "swallow":
					summary.Swallowed++
				case "stack_trace", "db_error", "verbose_error", "sensitive_field":
					summary.InfoLeaks++
				}
			}
			mu.Unlock()
		}(filePath, lang)
	}

	wg.Wait()

	// Calculate handled paths (rough estimate)
	summary.Handled = summary.TotalErrors - summary.FailOpenPaths
	summary.FailClosedPaths = summary.Handled

	return &safety.ErrorAudit{
		Scope:    scope,
		Issues:   allIssues,
		Summary:  summary,
		Duration: time.Since(start),
	}, nil
}

// detectFailOpen detects fail-open error handling patterns.
func (a *ErrorAuditorImpl) detectFailOpen(filePath, content, language string) []safety.ErrorIssue {
	pattern, ok := a.failOpenPatterns[language]
	if !ok {
		return nil
	}

	blocks := pattern.FindErrorChecks(content)
	var issues []safety.ErrorIssue

	for _, block := range blocks {
		if !block.IsFailOpen {
			continue
		}

		// Check if this is in a security function
		funcName := a.findContainingFunction(content, block.Start)
		inSecurityFn := IsSecurityFunction(funcName)

		severity := safety.SeverityMedium
		if inSecurityFn {
			severity = safety.SeverityCritical
		}

		// Extract code snippet
		codeSnippet := extractSnippet(block.Content, 100)

		issue := safety.ErrorIssue{
			Type:       "fail_open",
			Severity:   severity,
			Location:   fmt.Sprintf("%s:%d", filePath, block.Line),
			Line:       block.Line,
			Code:       codeSnippet,
			Context:    funcName,
			Risk:       "Error handling does not stop execution, allowing code to continue even on failure",
			Suggestion: "Add a return statement after handling the error to prevent fail-open behavior",
			CWE:        "CWE-755",
		}

		if inSecurityFn {
			issue.Risk = "CRITICAL: Fail-open in security function allows bypass of security controls"
			issue.Suggestion = "Add return statement immediately after error handling in security-sensitive functions"
		}

		issues = append(issues, issue)
	}

	return issues
}

// detectInfoLeaks detects information leak patterns.
func (a *ErrorAuditorImpl) detectInfoLeaks(filePath, content string) []safety.ErrorIssue {
	var issues []safety.ErrorIssue

	for _, pattern := range a.infoLeakPatterns {
		matches := pattern.Match(content)

		for _, match := range matches {
			lineNum := strings.Count(content[:match[0]], "\n") + 1
			codeSnippet := extractSnippetAround(content, match[0], match[1], 50)

			issues = append(issues, safety.ErrorIssue{
				Type:       pattern.Type,
				Severity:   pattern.Severity,
				Location:   fmt.Sprintf("%s:%d", filePath, lineNum),
				Line:       lineNum,
				Code:       codeSnippet,
				Context:    pattern.Description,
				Risk:       fmt.Sprintf("Information disclosure: %s", pattern.Description),
				Suggestion: "Return a generic error message to users; log detailed errors internally",
				CWE:        pattern.CWE,
			})
		}
	}

	return issues
}

// detectSwallowedErrors detects swallowed (empty) error handling.
func (a *ErrorAuditorImpl) detectSwallowedErrors(filePath, content, language string) []safety.ErrorIssue {
	matches := FindSwallowedErrors(content, language)
	var issues []safety.ErrorIssue

	for _, match := range matches {
		lineNum := strings.Count(content[:match[0]], "\n") + 1
		codeSnippet := extractSnippetAround(content, match[0], match[1], 20)

		issues = append(issues, safety.ErrorIssue{
			Type:       "swallow",
			Severity:   safety.SeverityMedium,
			Location:   fmt.Sprintf("%s:%d", filePath, lineNum),
			Line:       lineNum,
			Code:       codeSnippet,
			Context:    "Empty error handler",
			Risk:       "Error is silently ignored, hiding potential problems",
			Suggestion: "Log the error or handle it explicitly; avoid empty error handlers",
			CWE:        "CWE-390",
		})
	}

	return issues
}

// findContainingFunction finds the function containing a position.
func (a *ErrorAuditorImpl) findContainingFunction(content string, pos int) string {
	// Simple heuristic: find the most recent "func" declaration before pos
	funcPatterns := []string{
		`func\s+(\w+)\s*\(`,                               // Go
		`def\s+(\w+)\s*\(`,                                // Python
		`function\s+(\w+)\s*\(`,                           // JavaScript/TypeScript
		`(?:public|private|protected)\s+\w+\s+(\w+)\s*\(`, // Java
	}

	beforePos := content[:pos]
	lastMatch := ""

	for _, pattern := range funcPatterns {
		re := newRegexp(pattern)
		if re == nil {
			continue
		}

		matches := re.FindAllStringSubmatch(beforePos, -1)
		if len(matches) > 0 {
			lastFunc := matches[len(matches)-1]
			if len(lastFunc) > 1 {
				lastMatch = lastFunc[1]
			}
		}
	}

	return lastMatch
}

// findFilesInScope finds all files in a scope with their languages.
func (a *ErrorAuditorImpl) findFilesInScope(scope string) map[string]string {
	filesMap := make(map[string]string)

	for _, node := range a.graph.Nodes() {
		if node.Symbol == nil || node.Symbol.FilePath == "" {
			continue
		}

		// Match by package
		if node.Symbol.Package == scope {
			filesMap[node.Symbol.FilePath] = node.Symbol.Language
			continue
		}

		// Match by file path prefix
		if strings.HasPrefix(node.Symbol.FilePath, scope) {
			filesMap[node.Symbol.FilePath] = node.Symbol.Language
			continue
		}

		// Match by package prefix
		if strings.HasPrefix(node.Symbol.Package, scope) {
			filesMap[node.Symbol.FilePath] = node.Symbol.Language
			continue
		}

		// Match exact file path
		if node.Symbol.FilePath == scope {
			filesMap[node.Symbol.FilePath] = node.Symbol.Language
		}
	}

	return filesMap
}

// getFileContent retrieves file content from cache.
func (a *ErrorAuditorImpl) getFileContent(filePath string) string {
	a.fileCacheMu.RLock()
	content, ok := a.fileCache[filePath]
	a.fileCacheMu.RUnlock()

	if ok {
		return content
	}

	return ""
}

// SetFileContent sets file content for auditing.
func (a *ErrorAuditorImpl) SetFileContent(filePath, content string) {
	a.fileCacheMu.Lock()
	a.fileCache[filePath] = content
	a.fileCacheMu.Unlock()
}

// ClearFileCache clears the file content cache.
func (a *ErrorAuditorImpl) ClearFileCache() {
	a.fileCacheMu.Lock()
	a.fileCache = make(map[string]string)
	a.fileCacheMu.Unlock()
}

// extractSnippet extracts a code snippet of max length.
func extractSnippet(content string, maxLen int) string {
	if len(content) <= maxLen {
		return strings.TrimSpace(content)
	}
	return strings.TrimSpace(content[:maxLen]) + "..."
}

// extractSnippetAround extracts a snippet around a match.
func extractSnippetAround(content string, start, end, padding int) string {
	snippetStart := max(0, start-padding)
	snippetEnd := min(len(content), end+padding)

	snippet := content[snippetStart:snippetEnd]
	return strings.TrimSpace(snippet)
}

// max returns the larger of two integers.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// min returns the smaller of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// regexpCache caches compiled regular expressions.
var regexpCache = make(map[string]*regexp.Regexp)
var regexpCacheMu sync.RWMutex

// newRegexp returns a compiled regexp, caching for performance.
func newRegexp(pattern string) *regexp.Regexp {
	regexpCacheMu.RLock()
	re, ok := regexpCache[pattern]
	regexpCacheMu.RUnlock()

	if ok {
		return re
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}

	regexpCacheMu.Lock()
	regexpCache[pattern] = re
	regexpCacheMu.Unlock()

	return re
}
