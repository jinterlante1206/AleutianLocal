// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package explore

import (
	"context"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

// ErrorFlowTracer traces error propagation through function calls.
//
// Thread Safety:
//
//	ErrorFlowTracer is safe for concurrent use. It performs read-only
//	operations on the graph and index.
type ErrorFlowTracer struct {
	graph *graph.Graph
	index *index.SymbolIndex
}

// NewErrorFlowTracer creates a new ErrorFlowTracer.
//
// Description:
//
//	Creates a tracer that can follow error propagation through function
//	calls, identifying error origins, handlers, and escape points.
//
// Inputs:
//
//	g - The code graph. Must be frozen.
//	idx - The symbol index.
//
// Outputs:
//
//	*ErrorFlowTracer - The configured tracer.
//
// Example:
//
//	tracer := NewErrorFlowTracer(graph, index)
//	flow, err := tracer.TraceErrorFlow(ctx, "handlers.HandleRequest", opts...)
func NewErrorFlowTracer(g *graph.Graph, idx *index.SymbolIndex) *ErrorFlowTracer {
	return &ErrorFlowTracer{
		graph: g,
		index: idx,
	}
}

// TraceErrorFlow traces error propagation from a function.
//
// Description:
//
//	Performs BFS traversal from the starting symbol, analyzing each
//	function's error handling behavior to identify:
//	- Origins: Where errors are created (errors.New, fmt.Errorf)
//	- Handlers: Where errors are caught/handled (if err != nil with handling)
//	- Escapes: Where errors propagate up without handling
//
// Inputs:
//
//	ctx - Context for cancellation.
//	symbolID - The symbol ID to start tracing from.
//	opts - Optional configuration (MaxNodes, MaxHops).
//
// Outputs:
//
//	*ErrorFlow - The traced error flow with origins, handlers, and escapes.
//	error - Non-nil if the symbol is not found or operation was canceled.
//
// Errors:
//
//	ErrSymbolNotFound - Symbol not found in the graph.
//	ErrContextCanceled - Context was canceled.
//	ErrGraphNotReady - Graph is not frozen.
//
// Performance:
//
//	Target latency: < 500ms for typical call graphs.
//
// Limitations:
//
//   - Cannot track errors through interface calls reliably
//   - May miss custom error types that don't follow naming conventions
//   - Does not analyze error wrapping chains in detail
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (t *ErrorFlowTracer) TraceErrorFlow(ctx context.Context, symbolID string, opts ...ExploreOption) (*ErrorFlow, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	if !t.graph.IsFrozen() {
		return nil, ErrGraphNotReady
	}

	options := applyOptions(opts)
	if options.MaxNodes <= 0 {
		options.MaxNodes = 1000
	}

	// Verify starting node exists
	_, exists := t.graph.GetNode(symbolID)
	if !exists {
		return nil, ErrSymbolNotFound
	}

	flow := &ErrorFlow{
		Origins:  make([]ErrorPoint, 0),
		Handlers: make([]ErrorPoint, 0),
		Escapes:  make([]ErrorPoint, 0),
	}

	// BFS traversal
	visited := make(map[string]bool)
	type queueItem struct {
		nodeID string
		depth  int
	}
	queue := []queueItem{{symbolID, 0}}
	visited[symbolID] = true
	nodesVisited := 0

	for len(queue) > 0 {
		// Check context periodically
		if nodesVisited%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, ErrContextCanceled
			}
		}

		// Check node limit
		if nodesVisited >= options.MaxNodes {
			break
		}

		item := queue[0]
		queue = queue[1:]
		nodesVisited++

		node, exists := t.graph.GetNode(item.nodeID)
		if !exists {
			continue
		}

		// Analyze this node for error handling patterns
		t.analyzeErrorHandling(node, flow)

		// Check depth limit
		if item.depth >= options.MaxHops {
			continue
		}

		// Follow outgoing CALLS edges
		for _, edge := range node.Outgoing {
			if edge.Type != graph.EdgeTypeCalls {
				continue
			}

			if visited[edge.ToID] {
				continue
			}
			visited[edge.ToID] = true

			// Add to queue for further traversal
			queue = append(queue, queueItem{edge.ToID, item.depth + 1})
		}
	}

	return flow, nil
}

// analyzeErrorHandling analyzes a node for error handling patterns.
func (t *ErrorFlowTracer) analyzeErrorHandling(node *graph.Node, flow *ErrorFlow) {
	if node == nil || node.Symbol == nil {
		return
	}

	sym := node.Symbol
	errorPoint := ErrorPoint{
		Function: sym.Name,
		FilePath: sym.FilePath,
		Line:     sym.StartLine,
	}

	// Check for error origins
	if t.isErrorOrigin(sym) {
		errorPoint.Type = "origin"
		flow.Origins = append(flow.Origins, errorPoint)
	}

	// Check for error handlers
	if t.isErrorHandler(sym) {
		errorPoint.Type = "handler"
		flow.Handlers = append(flow.Handlers, errorPoint)
	}

	// Check for error escapes (functions that return error but don't handle it)
	if t.isErrorEscape(sym) {
		errorPoint.Type = "escape"
		flow.Escapes = append(flow.Escapes, errorPoint)
	}
}

// isErrorOrigin checks if a symbol creates errors.
func (t *ErrorFlowTracer) isErrorOrigin(sym *ast.Symbol) bool {
	if sym == nil {
		return false
	}

	// Check by name for common error creation functions
	errorCreators := []string{
		"New",    // errors.New
		"Errorf", // fmt.Errorf
		"Wrap",   // pkg/errors.Wrap
		"Wrapf",  // pkg/errors.Wrapf
		"WithMessage",
		"WithStack",
	}

	for _, creator := range errorCreators {
		if sym.Name == creator {
			// Check if it's from an error-related package
			if strings.Contains(sym.Package, "errors") ||
				strings.Contains(sym.Package, "fmt") ||
				strings.Contains(sym.Package, "xerrors") {
				return true
			}
		}
	}

	// Check for custom error type constructors (functions that return error)
	if sym.Kind == ast.SymbolKindFunction || sym.Kind == ast.SymbolKindMethod {
		sig := strings.ToLower(sym.Signature)
		name := strings.ToLower(sym.Name)

		// Functions named New*Error or *Error that return error
		if (strings.HasPrefix(name, "new") && strings.Contains(name, "error")) ||
			(strings.HasSuffix(name, "error") && strings.Contains(sig, "error")) {
			return true
		}
	}

	return false
}

// isErrorHandler checks if a symbol handles errors.
func (t *ErrorFlowTracer) isErrorHandler(sym *ast.Symbol) bool {
	if sym == nil {
		return false
	}

	// Check by name for common error handling patterns
	errorHandlers := []string{
		"handleError",
		"HandleError",
		"handle",
		"Handle",
		"recover",
		"Recover",
		"errorHandler",
		"ErrorHandler",
		"logError",
		"LogError",
	}

	name := sym.Name
	for _, handler := range errorHandlers {
		if strings.Contains(name, handler) || name == handler {
			return true
		}
	}

	// Functions with 'Error' in the name that don't return error (they handle it)
	if (sym.Kind == ast.SymbolKindFunction || sym.Kind == ast.SymbolKindMethod) &&
		strings.Contains(strings.ToLower(name), "error") &&
		!strings.Contains(sym.Signature, "error") {
		return true
	}

	return false
}

// isErrorEscape checks if a symbol propagates errors without handling.
func (t *ErrorFlowTracer) isErrorEscape(sym *ast.Symbol) bool {
	if sym == nil {
		return false
	}

	// A function that returns error is a potential escape point
	if sym.Kind == ast.SymbolKindFunction || sym.Kind == ast.SymbolKindMethod {
		sig := strings.ToLower(sym.Signature)

		// Check if the function returns error
		if strings.Contains(sig, "error") {
			// But exclude error handlers and error creators
			if !t.isErrorHandler(sym) && !t.isErrorOrigin(sym) {
				return true
			}
		}
	}

	return false
}

// FindErrorOrigins finds all error creation points in a file.
//
// Description:
//
//	Scans all symbols in a file and identifies which ones create errors.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	filePath - The relative path to the file.
//
// Outputs:
//
//	[]ErrorPoint - All error origins found in the file.
//	error - Non-nil if the file is not found or operation was canceled.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (t *ErrorFlowTracer) FindErrorOrigins(ctx context.Context, filePath string) ([]ErrorPoint, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	symbols := t.index.GetByFile(filePath)
	if len(symbols) == 0 {
		return nil, ErrFileNotFound
	}

	origins := make([]ErrorPoint, 0)
	for _, sym := range symbols {
		if err := ctx.Err(); err != nil {
			return origins, ErrContextCanceled
		}

		if t.isErrorOrigin(sym) {
			origins = append(origins, ErrorPoint{
				Function: sym.Name,
				FilePath: sym.FilePath,
				Line:     sym.StartLine,
				Type:     "origin",
			})
		}
	}

	return origins, nil
}

// FindUnhandledErrors finds functions that return errors but might not handle them.
//
// Description:
//
//	Analyzes the call graph to find functions that receive errors from
//	callees but might not handle them (potential escape points).
//
// Inputs:
//
//	ctx - Context for cancellation.
//	symbolID - The symbol ID to start analyzing from.
//	opts - Optional configuration (MaxNodes, MaxHops).
//
// Outputs:
//
//	[]ErrorPoint - Functions that might have unhandled errors.
//	error - Non-nil if the symbol is not found or operation was canceled.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (t *ErrorFlowTracer) FindUnhandledErrors(ctx context.Context, symbolID string, opts ...ExploreOption) ([]ErrorPoint, error) {
	flow, err := t.TraceErrorFlow(ctx, symbolID, opts...)
	if err != nil {
		return nil, err
	}

	// Return escape points - these are functions that propagate errors without handling
	return flow.Escapes, nil
}

// FindErrorHandlers finds all error handling points in a file.
//
// Description:
//
//	Scans all symbols in a file and identifies which ones handle errors.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	filePath - The relative path to the file.
//
// Outputs:
//
//	[]ErrorPoint - All error handlers found in the file.
//	error - Non-nil if the file is not found or operation was canceled.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (t *ErrorFlowTracer) FindErrorHandlers(ctx context.Context, filePath string) ([]ErrorPoint, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	symbols := t.index.GetByFile(filePath)
	if len(symbols) == 0 {
		return nil, ErrFileNotFound
	}

	handlers := make([]ErrorPoint, 0)
	for _, sym := range symbols {
		if err := ctx.Err(); err != nil {
			return handlers, ErrContextCanceled
		}

		if t.isErrorHandler(sym) {
			handlers = append(handlers, ErrorPoint{
				Function: sym.Name,
				FilePath: sym.FilePath,
				Line:     sym.StartLine,
				Type:     "handler",
			})
		}
	}

	return handlers, nil
}

// GetErrorFlowSummary provides a summary of error handling in a file.
//
// Description:
//
//	Provides a quick overview of error handling patterns in a file,
//	including counts of origins, handlers, and potential escapes.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	filePath - The relative path to the file.
//
// Outputs:
//
//	*ErrorFlow - Summary of error handling in the file.
//	error - Non-nil if the file is not found or operation was canceled.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (t *ErrorFlowTracer) GetErrorFlowSummary(ctx context.Context, filePath string) (*ErrorFlow, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	symbols := t.index.GetByFile(filePath)
	if len(symbols) == 0 {
		return nil, ErrFileNotFound
	}

	flow := &ErrorFlow{
		Origins:  make([]ErrorPoint, 0),
		Handlers: make([]ErrorPoint, 0),
		Escapes:  make([]ErrorPoint, 0),
	}

	for _, sym := range symbols {
		if err := ctx.Err(); err != nil {
			return flow, ErrContextCanceled
		}

		errorPoint := ErrorPoint{
			Function: sym.Name,
			FilePath: sym.FilePath,
			Line:     sym.StartLine,
		}

		if t.isErrorOrigin(sym) {
			errorPoint.Type = "origin"
			flow.Origins = append(flow.Origins, errorPoint)
		}

		if t.isErrorHandler(sym) {
			errorPoint.Type = "handler"
			flow.Handlers = append(flow.Handlers, errorPoint)
		}

		if t.isErrorEscape(sym) {
			errorPoint.Type = "escape"
			flow.Escapes = append(flow.Escapes, errorPoint)
		}
	}

	return flow, nil
}
