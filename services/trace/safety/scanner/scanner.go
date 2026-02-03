// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package scanner

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
	"github.com/AleutianAI/AleutianFOSS/services/trace/safety"
	"github.com/AleutianAI/AleutianFOSS/services/trace/safety/trust_flow"
)

// SecurityScannerImpl implements the safety.SecurityScanner interface.
//
// Description:
//
//	SecurityScannerImpl performs SAST-lite scanning, detecting common
//	vulnerability patterns like SQL injection, XSS, command injection.
//	It integrates with trust flow analysis for higher confidence findings.
//
// Thread Safety:
//
//	SecurityScannerImpl is safe for concurrent use after initialization.
type SecurityScannerImpl struct {
	graph        *graph.Graph
	idx          *index.SymbolIndex
	patternDB    *SecurityPatternDB
	confidence   *ConfidenceCalculator
	inputTracer  *trust_flow.InputTracerImpl
	issueCounter uint64

	// File content cache for repeated access
	fileCache   map[string]string
	fileCacheMu sync.RWMutex
}

// NewSecurityScanner creates a new security scanner.
//
// Description:
//
//	Creates a scanner with default patterns and confidence calculator.
//	The scanner integrates with trust flow analysis for proof of
//	exploitability.
//
// Inputs:
//
//	g - The code graph. Should be frozen.
//	idx - The symbol index.
//
// Outputs:
//
//	*SecurityScannerImpl - The configured scanner.
//
// Example:
//
//	scanner := NewSecurityScanner(graph, index)
//	result, err := scanner.ScanForSecurityIssues(ctx, "pkg/handlers")
func NewSecurityScanner(g *graph.Graph, idx *index.SymbolIndex) *SecurityScannerImpl {
	return &SecurityScannerImpl{
		graph:       g,
		idx:         idx,
		patternDB:   NewSecurityPatternDB(),
		confidence:  NewConfidenceCalculator(),
		inputTracer: trust_flow.NewInputTracer(g, idx),
		fileCache:   make(map[string]string),
	}
}

// NewSecurityScannerWithTracer creates a scanner with a custom input tracer.
//
// Description:
//
//	Creates a scanner with a custom input tracer for dependency injection.
func NewSecurityScannerWithTracer(
	g *graph.Graph,
	idx *index.SymbolIndex,
	tracer *trust_flow.InputTracerImpl,
) *SecurityScannerImpl {
	return &SecurityScannerImpl{
		graph:       g,
		idx:         idx,
		patternDB:   NewSecurityPatternDB(),
		confidence:  NewConfidenceCalculator(),
		inputTracer: tracer,
		fileCache:   make(map[string]string),
	}
}

// ScanForSecurityIssues scans a scope for security vulnerabilities.
//
// Description:
//
//	Scans the specified scope (package, file, or function) for security
//	issues using pattern matching and optional trust flow analysis.
//	Issues are filtered by minimum severity and confidence.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout.
//	scope - The scope to scan (package path, file path, or symbol ID).
//	opts - Optional configuration (min severity, min confidence, etc.).
//
// Outputs:
//
//	*safety.ScanResult - The scan result with issues found.
//	error - Non-nil if scope not found or operation canceled.
//
// Errors:
//
//	safety.ErrInvalidInput - Scope is empty.
//	safety.ErrGraphNotReady - Graph is not frozen.
//	safety.ErrContextCanceled - Context was canceled.
//
// Performance:
//
//	Target latency: < 500ms for single package.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (s *SecurityScannerImpl) ScanForSecurityIssues(
	ctx context.Context,
	scope string,
	opts ...safety.ScanOption,
) (*safety.ScanResult, error) {
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

	if !s.graph.IsFrozen() {
		return nil, safety.ErrGraphNotReady
	}

	// Apply options
	config := safety.DefaultScanConfig()
	config.ApplyOptions(opts...)

	// Find nodes matching the scope
	nodes := s.findNodesInScope(scope)
	if len(nodes) == 0 {
		return &safety.ScanResult{
			Scope:           scope,
			Issues:          []safety.SecurityIssue{},
			Summary:         safety.ScanSummary{},
			Confidence:      1.0,
			CoveragePercent: 0.0,
			Duration:        time.Since(start),
		}, nil
	}

	// Scan each node
	var issues []safety.SecurityIssue
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Use parallelism if configured
	parallelism := config.Parallelism
	if parallelism <= 0 {
		parallelism = 1
	}
	semaphore := make(chan struct{}, parallelism)

	filesScanned := make(map[string]bool)
	partialFailures := make([]safety.PartialFailure, 0)

	for _, node := range nodes {
		if err := ctx.Err(); err != nil {
			break
		}

		wg.Add(1)
		semaphore <- struct{}{}

		go func(n *graph.Node) {
			defer wg.Done()
			defer func() { <-semaphore }()

			nodeIssues, failures := s.scanNode(ctx, n, config)

			mu.Lock()
			issues = append(issues, nodeIssues...)
			partialFailures = append(partialFailures, failures...)
			if n.Symbol != nil && n.Symbol.FilePath != "" {
				filesScanned[n.Symbol.FilePath] = true
			}
			mu.Unlock()
		}(node)
	}

	wg.Wait()

	// Build result
	result := &safety.ScanResult{
		Scope:           scope,
		Issues:          issues,
		PartialFailures: partialFailures,
		Duration:        time.Since(start),
	}

	// Calculate summary
	result.Summary = s.calculateSummary(issues, len(filesScanned))

	// Calculate overall confidence
	if len(issues) > 0 {
		var totalConf float64
		for _, issue := range issues {
			totalConf += issue.Confidence
		}
		result.Confidence = totalConf / float64(len(issues))
	} else {
		result.Confidence = 1.0
	}

	// Estimate coverage (rough heuristic)
	result.CoveragePercent = float64(len(filesScanned)) / float64(max(len(nodes), 1)) * 100

	return result, nil
}

// scanNode scans a single node for security issues.
func (s *SecurityScannerImpl) scanNode(
	ctx context.Context,
	node *graph.Node,
	config *safety.ScanConfig,
) ([]safety.SecurityIssue, []safety.PartialFailure) {
	if node.Symbol == nil {
		return nil, nil
	}

	// Get patterns for this language
	patterns := s.patternDB.GetPatternsForLanguage(node.Symbol.Language)
	if len(patterns) == 0 {
		return nil, nil
	}

	// Get file content
	content := s.getFileContent(node.Symbol.FilePath)
	if content == "" {
		return nil, []safety.PartialFailure{{
			Scope:    node.Symbol.FilePath,
			Reason:   "file content not available",
			Impact:   "could not scan for pattern-based vulnerabilities",
			Severity: safety.SeverityLow,
		}}
	}

	// Build scan context
	scanCtx := &ScanContext{
		FilePath:           node.Symbol.FilePath,
		IsTestFile:         IsTestFile(node.Symbol.FilePath),
		InSecurityFunction: IsSecurityFunction(node.Symbol.Name),
	}

	var issues []safety.SecurityIssue

	// Match each pattern against the content
	for _, pattern := range patterns {
		if ctx.Err() != nil {
			break
		}

		// Filter by minimum severity
		if !severityMeetsMinimum(pattern.Severity, config.MinSeverity) {
			continue
		}

		// Match pattern
		matches := pattern.Detection.Match(content)
		for _, match := range matches {
			// Get the line number
			lineNum := countLines(content[:match[0]]) + 1

			// Check for suppression comment
			hasSuppression, suppressionNote := HasSuppressionComment(content, match[0], match[1])
			scanCtx.HasNoSecComment = hasSuppression
			scanCtx.SuppressionNote = suppressionNote

			// Calculate confidence
			confidence := s.confidence.Calculate(pattern, scanCtx)

			// Filter by minimum confidence
			if confidence < config.MinConfidence {
				continue
			}

			// Try to prove with data flow if pattern has trust flow rule
			dataFlowProven := false
			if pattern.Detection.TrustFlowRule != nil && s.inputTracer != nil {
				dataFlowProven = s.proveWithDataFlow(ctx, node, pattern.Detection.TrustFlowRule)
				if dataFlowProven {
					scanCtx.DataFlowProven = true
					confidence = s.confidence.Calculate(pattern, scanCtx)
				}
			}

			// Extract code snippet
			codeSnippet := extractCodeSnippet(content, match[0], match[1])

			issue := safety.SecurityIssue{
				ID:              s.generateIssueID(),
				Type:            pattern.Name,
				Severity:        pattern.Severity,
				Confidence:      confidence,
				Location:        fmt.Sprintf("%s:%d", node.Symbol.FilePath, lineNum),
				Line:            lineNum,
				Code:            codeSnippet,
				Description:     pattern.Description,
				Remediation:     pattern.Remediation,
				CWE:             pattern.CWE,
				DataFlowProven:  dataFlowProven,
				Suppressed:      hasSuppression,
				SuppressionNote: suppressionNote,
			}

			issues = append(issues, issue)
		}
	}

	return issues, nil
}

// proveWithDataFlow attempts to prove exploitability using trust flow analysis.
//
// Description:
//
//	Checks if untrusted data can reach this potential sink by finding
//	source nodes and tracing forward to see if they reach this sink.
//	Traces from multiple sources in parallel for faster results.
//
// Algorithm:
//
//  1. Find all recognized source nodes in the same file/package
//  2. Trace from sources in parallel with early exit on first proof
//  3. If any trace reaches this sink node, return true
func (s *SecurityScannerImpl) proveWithDataFlow(
	ctx context.Context,
	sinkNode *graph.Node,
	rule *TrustFlowRule,
) bool {
	if s.inputTracer == nil {
		return false
	}

	if sinkNode.Symbol == nil {
		return false
	}

	// Find potential source nodes in the same file/package
	sourceNodes := s.findSourceNodesNear(sinkNode)
	if len(sourceNodes) == 0 {
		return false
	}

	// For small number of sources, run sequentially to avoid overhead
	if len(sourceNodes) <= 2 {
		return s.proveWithDataFlowSequential(ctx, sinkNode, sourceNodes, rule)
	}

	// Trace from sources in parallel with early exit
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	resultCh := make(chan bool, len(sourceNodes))
	var wg sync.WaitGroup

	for _, sourceNode := range sourceNodes {
		wg.Add(1)
		go func(sn *graph.Node) {
			defer wg.Done()

			// Check if already proven by another goroutine
			select {
			case <-ctx.Done():
				return
			default:
			}

			trace, err := s.inputTracer.TraceUserInput(ctx, sn.ID,
				safety.WithMaxDepth(5),
				safety.WithSinkCategories(rule.SinkCategory),
			)

			if err != nil {
				return
			}

			// Check if any of the sinks in the trace match our target node
			for _, sink := range trace.Sinks {
				if sink.ID == sinkNode.ID {
					resultCh <- true
					cancel() // Signal other goroutines to stop
					return
				}
			}

			// Also check vulnerabilities since they reference the sink
			for _, vuln := range trace.Vulnerabilities {
				if strings.Contains(vuln.Location, sinkNode.Symbol.FilePath) &&
					vuln.Line == sinkNode.Symbol.StartLine {
					resultCh <- true
					cancel() // Signal other goroutines to stop
					return
				}
			}
		}(sourceNode)
	}

	// Wait for all goroutines in background, close channel when done
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Return true on first proof, or false if all complete without proof
	for result := range resultCh {
		if result {
			return true
		}
	}
	return false
}

// proveWithDataFlowSequential traces sources sequentially for small source counts.
func (s *SecurityScannerImpl) proveWithDataFlowSequential(
	ctx context.Context,
	sinkNode *graph.Node,
	sourceNodes []*graph.Node,
	rule *TrustFlowRule,
) bool {
	for _, sourceNode := range sourceNodes {
		if ctx.Err() != nil {
			return false
		}

		trace, err := s.inputTracer.TraceUserInput(ctx, sourceNode.ID,
			safety.WithMaxDepth(5),
			safety.WithSinkCategories(rule.SinkCategory),
		)

		if err != nil {
			continue
		}

		// Check if any of the sinks in the trace match our target node
		for _, sink := range trace.Sinks {
			if sink.ID == sinkNode.ID {
				return true
			}
		}

		// Also check vulnerabilities since they reference the sink
		for _, vuln := range trace.Vulnerabilities {
			if strings.Contains(vuln.Location, sinkNode.Symbol.FilePath) &&
				vuln.Line == sinkNode.Symbol.StartLine {
				return true
			}
		}
	}

	return false
}

// findSourceNodesNear finds recognized source nodes near a sink node.
//
// Description:
//
//	Looks for HTTP handlers, request parameter access, and other
//	recognized input sources in the same file or calling the same
//	package as the sink.
func (s *SecurityScannerImpl) findSourceNodesNear(sinkNode *graph.Node) []*graph.Node {
	var sources []*graph.Node

	// Look through all nodes for sources in the same file or package
	for _, node := range s.graph.Nodes() {
		if node.Symbol == nil {
			continue
		}

		// Check if this is a recognized source
		if !s.isRecognizedSource(node) {
			continue
		}

		// Prefer sources in the same file
		if node.Symbol.FilePath == sinkNode.Symbol.FilePath {
			sources = append(sources, node)
			continue
		}

		// Also include sources in the same package
		if node.Symbol.Package == sinkNode.Symbol.Package {
			sources = append(sources, node)
		}
	}

	// Limit to avoid expensive traversals
	if len(sources) > 10 {
		sources = sources[:10]
	}

	return sources
}

// isRecognizedSource checks if a node is a recognized input source.
func (s *SecurityScannerImpl) isRecognizedSource(node *graph.Node) bool {
	if node.Symbol == nil {
		return false
	}

	// Check common source patterns
	name := strings.ToLower(node.Symbol.Name)
	receiver := strings.ToLower(node.Symbol.Receiver)

	// HTTP request access patterns
	httpPatterns := []string{
		"query", "param", "form", "body", "header", "cookie",
		"getparam", "getquery", "getform", "getbody",
		"request", "req", "r.url", "r.form", "r.body",
	}

	for _, pattern := range httpPatterns {
		if strings.Contains(name, pattern) || strings.Contains(receiver, pattern) {
			return true
		}
	}

	// Gin context patterns
	if strings.Contains(receiver, "context") || strings.Contains(receiver, "gin") {
		if name == "query" || name == "param" || name == "postform" ||
			name == "bind" || name == "shouldbind" || name == "getstring" {
			return true
		}
	}

	// FastAPI/Flask patterns
	if strings.Contains(name, "depends") || strings.Contains(name, "request") {
		return true
	}

	return false
}

// findNodesInScope finds all nodes matching a scope.
func (s *SecurityScannerImpl) findNodesInScope(scope string) []*graph.Node {
	var nodes []*graph.Node

	// Try as exact symbol ID first
	if node, exists := s.graph.GetNode(scope); exists {
		return []*graph.Node{node}
	}

	// Try as package or file path prefix
	for _, node := range s.graph.Nodes() {
		if node.Symbol == nil {
			continue
		}

		// Match by package
		if node.Symbol.Package == scope {
			nodes = append(nodes, node)
			continue
		}

		// Match by file path prefix
		if strings.HasPrefix(node.Symbol.FilePath, scope) {
			nodes = append(nodes, node)
			continue
		}

		// Match by package prefix
		if strings.HasPrefix(node.Symbol.Package, scope) {
			nodes = append(nodes, node)
			continue
		}
	}

	return nodes
}

// getFileContent retrieves file content from cache or graph.
func (s *SecurityScannerImpl) getFileContent(filePath string) string {
	s.fileCacheMu.RLock()
	content, ok := s.fileCache[filePath]
	s.fileCacheMu.RUnlock()

	if ok {
		return content
	}

	// Try to get from graph metadata
	// In a full implementation, this would read from disk or a file store
	// For now, we'll return empty and rely on AST analysis
	return ""
}

// SetFileContent sets file content for scanning.
//
// Description:
//
//	Sets the content for a file to enable pattern-based scanning.
//	This is typically called by the scan orchestrator after reading files.
//
// Inputs:
//
//	filePath - The file path.
//	content - The file content.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (s *SecurityScannerImpl) SetFileContent(filePath, content string) {
	s.fileCacheMu.Lock()
	s.fileCache[filePath] = content
	s.fileCacheMu.Unlock()
}

// ClearFileCache clears the file content cache.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (s *SecurityScannerImpl) ClearFileCache() {
	s.fileCacheMu.Lock()
	s.fileCache = make(map[string]string)
	s.fileCacheMu.Unlock()
}

// calculateSummary calculates the scan summary.
func (s *SecurityScannerImpl) calculateSummary(issues []safety.SecurityIssue, filesScanned int) safety.ScanSummary {
	summary := safety.ScanSummary{
		FilesScanned: filesScanned,
	}

	for _, issue := range issues {
		summary.TotalIssues++

		if issue.Suppressed {
			summary.Suppressed++
			continue
		}

		switch issue.Severity {
		case safety.SeverityCritical:
			summary.Critical++
		case safety.SeverityHigh:
			summary.High++
		case safety.SeverityMedium:
			summary.Medium++
		case safety.SeverityLow:
			summary.Low++
		}
	}

	return summary
}

// generateIssueID generates a unique issue ID.
func (s *SecurityScannerImpl) generateIssueID() string {
	id := atomic.AddUint64(&s.issueCounter, 1)
	return fmt.Sprintf("SCAN-%d", id)
}

// severityMeetsMinimum checks if severity meets the minimum threshold.
func severityMeetsMinimum(severity, minimum safety.Severity) bool {
	severityOrder := map[safety.Severity]int{
		safety.SeverityInfo:     0,
		safety.SeverityLow:      1,
		safety.SeverityMedium:   2,
		safety.SeverityHigh:     3,
		safety.SeverityCritical: 4,
	}

	return severityOrder[severity] >= severityOrder[minimum]
}

// countLines counts the number of newlines before a position.
func countLines(content string) int {
	return strings.Count(content, "\n")
}

// extractCodeSnippet extracts a code snippet around a match.
func extractCodeSnippet(content string, start, end int) string {
	// Extend to include full lines
	lineStart := start
	for lineStart > 0 && content[lineStart-1] != '\n' {
		lineStart--
	}

	lineEnd := end
	for lineEnd < len(content) && content[lineEnd] != '\n' {
		lineEnd++
	}

	snippet := content[lineStart:lineEnd]

	// Truncate if too long
	if len(snippet) > 200 {
		snippet = snippet[:200] + "..."
	}

	return strings.TrimSpace(snippet)
}
