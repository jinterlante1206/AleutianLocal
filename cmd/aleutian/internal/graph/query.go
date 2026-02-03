// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package graph

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/internal/initializer"
)

// Querier provides graph query operations.
//
// # Description
//
// Querier wraps a MemoryIndex and provides callers/callees/path queries
// using BFS traversal with depth limits and cycle detection.
//
// # Thread Safety
//
// Querier is safe for concurrent use.
type Querier struct {
	index *initializer.MemoryIndex
}

// NewQuerier creates a new Querier with the given index.
//
// # Description
//
// Creates a Querier for executing graph queries against the provided index.
//
// # Inputs
//
//   - index: The MemoryIndex to query. Must not be nil.
//
// # Outputs
//
//   - *Querier: The querier instance.
//
// # Thread Safety
//
// The returned Querier is safe for concurrent use.
func NewQuerier(index *initializer.MemoryIndex) *Querier {
	return &Querier{index: index}
}

// FindCallers finds all symbols that call the target symbol.
//
// # Description
//
// Uses BFS traversal to find all callers up to the specified depth.
// Direct callers are at depth 1, their callers at depth 2, etc.
//
// # Inputs
//
//   - ctx: Context for cancellation. Must not be nil.
//   - symbolInput: Symbol name/ID/file:line to find callers for.
//   - cfg: Query configuration.
//
// # Outputs
//
//   - *QueryResult: Query results with callers.
//   - error: Non-nil if symbol resolution fails.
//
// # Thread Safety
//
// Safe for concurrent use.
func (q *Querier) FindCallers(ctx context.Context, symbolInput string, cfg QueryConfig) (*QueryResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}

	result := NewQueryResult("callers", symbolInput)

	// Resolve symbol
	sym, err := q.resolveSymbol(symbolInput, cfg.Exact)
	if err != nil {
		return nil, err
	}

	maxDepth := cfg.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}
	result.MaxDepthUsed = maxDepth

	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = DefaultMaxResults
	}

	// BFS to find callers using slice-based queue
	visited := make(map[string]bool)
	visited[sym.ID] = true

	type queueItem struct {
		symbolID string
		depth    int
		path     []string
	}

	queue := []queueItem{{symbolID: sym.ID, depth: 0, path: []string{sym.Name}}}
	directCount := 0

	for len(queue) > 0 && len(result.Results) < maxResults {
		select {
		case <-ctx.Done():
			result.Warnings = append(result.Warnings, "query cancelled")
			result.Truncated = true
			return result, nil
		default:
		}

		// Dequeue
		item := queue[0]
		queue = queue[1:]

		if item.depth >= maxDepth {
			continue
		}

		// Get callers (edges where ToID = symbolID)
		edges := q.index.GetCallers(item.symbolID, 0)

		for _, edge := range edges {
			if visited[edge.FromID] {
				continue
			}
			visited[edge.FromID] = true

			// Get caller symbol info
			callerSym := q.index.GetByID(edge.FromID)
			if callerSym == nil {
				continue
			}

			// Filter tests if needed
			if !cfg.IncludeTests && isTestFile(callerSym.FilePath) {
				continue
			}

			newPath := append(append([]string{}, item.path...), callerSym.Name)
			depth := item.depth + 1

			result.Results = append(result.Results, CallResult{
				SymbolID:   callerSym.ID,
				SymbolName: callerSym.Name,
				Kind:       callerSym.Kind,
				FilePath:   callerSym.FilePath,
				Line:       callerSym.StartLine,
				Depth:      depth,
				Path:       newPath,
			})

			if depth == 1 {
				directCount++
			}

			// Add to queue for further traversal
			if depth < maxDepth && len(result.Results) < maxResults {
				queue = append(queue, queueItem{symbolID: callerSym.ID, depth: depth, path: newPath})
			}
		}
	}

	result.DirectCount = directCount
	result.TransitiveCount = len(result.Results) - directCount
	result.TotalCount = len(result.Results)
	result.Truncated = len(result.Results) >= maxResults

	return result, nil
}

// FindCallees finds all symbols called by the target symbol.
//
// # Description
//
// Uses BFS traversal to find all callees up to the specified depth.
// Direct callees are at depth 1, their callees at depth 2, etc.
//
// # Inputs
//
//   - ctx: Context for cancellation. Must not be nil.
//   - symbolInput: Symbol name/ID/file:line to find callees for.
//   - cfg: Query configuration.
//
// # Outputs
//
//   - *QueryResult: Query results with callees.
//   - error: Non-nil if symbol resolution fails.
//
// # Thread Safety
//
// Safe for concurrent use.
func (q *Querier) FindCallees(ctx context.Context, symbolInput string, cfg QueryConfig) (*QueryResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}

	result := NewQueryResult("callees", symbolInput)

	// Resolve symbol
	sym, err := q.resolveSymbol(symbolInput, cfg.Exact)
	if err != nil {
		return nil, err
	}

	maxDepth := cfg.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 1 // Default to direct callees only
	}
	result.MaxDepthUsed = maxDepth

	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = DefaultMaxResults
	}

	// BFS to find callees using slice-based queue
	visited := make(map[string]bool)
	visited[sym.ID] = true

	type queueItem struct {
		symbolID string
		depth    int
		path     []string
	}

	queue := []queueItem{{symbolID: sym.ID, depth: 0, path: []string{sym.Name}}}
	directCount := 0

	for len(queue) > 0 && len(result.Results) < maxResults {
		select {
		case <-ctx.Done():
			result.Warnings = append(result.Warnings, "query cancelled")
			result.Truncated = true
			return result, nil
		default:
		}

		// Dequeue
		item := queue[0]
		queue = queue[1:]

		if item.depth >= maxDepth {
			continue
		}

		// Get callees (edges where FromID = symbolID)
		edges := q.index.GetCallees(item.symbolID, 0)

		for _, edge := range edges {
			if visited[edge.ToID] {
				continue
			}
			visited[edge.ToID] = true

			// Get callee symbol info
			calleeSym := q.index.GetByID(edge.ToID)
			if calleeSym == nil {
				continue
			}

			// Filter stdlib if needed
			if !cfg.IncludeStdlib && isStdlibSymbol(calleeSym) {
				continue
			}

			newPath := append(append([]string{}, item.path...), calleeSym.Name)
			depth := item.depth + 1

			result.Results = append(result.Results, CallResult{
				SymbolID:   calleeSym.ID,
				SymbolName: calleeSym.Name,
				Kind:       calleeSym.Kind,
				FilePath:   calleeSym.FilePath,
				Line:       calleeSym.StartLine,
				Depth:      depth,
				Path:       newPath,
			})

			if depth == 1 {
				directCount++
			}

			// Add to queue for further traversal
			if depth < maxDepth && len(result.Results) < maxResults {
				queue = append(queue, queueItem{symbolID: calleeSym.ID, depth: depth, path: newPath})
			}
		}
	}

	result.DirectCount = directCount
	result.TransitiveCount = len(result.Results) - directCount
	result.TotalCount = len(result.Results)
	result.Truncated = len(result.Results) >= maxResults

	return result, nil
}

// FindPath finds paths between two symbols using bidirectional BFS.
//
// # Description
//
// Uses bidirectional BFS for efficient path finding. Expands from both
// the source and target simultaneously until they meet.
//
// # Inputs
//
//   - ctx: Context for cancellation. Must not be nil.
//   - fromInput: Source symbol name/ID.
//   - toInput: Target symbol name/ID.
//   - cfg: Query configuration.
//   - findAll: If true, find all paths (not just shortest).
//   - maxPaths: Maximum number of paths to return.
//
// # Outputs
//
//   - *PathQueryResult: Query results with paths.
//   - error: Non-nil if symbol resolution fails.
//
// # Thread Safety
//
// Safe for concurrent use.
func (q *Querier) FindPath(ctx context.Context, fromInput, toInput string, cfg QueryConfig, findAll bool, maxPaths int) (*PathQueryResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}

	result := NewPathQueryResult(fromInput, toInput)

	// Resolve symbols
	fromSym, err := q.resolveSymbol(fromInput, cfg.Exact)
	if err != nil {
		return nil, fmt.Errorf("resolving source symbol: %w", err)
	}

	toSym, err := q.resolveSymbol(toInput, cfg.Exact)
	if err != nil {
		return nil, fmt.Errorf("resolving target symbol: %w", err)
	}

	// Same symbol = trivial path
	if fromSym.ID == toSym.ID {
		result.PathFound = true
		result.PathCount = 1
		result.Paths = []PathResult{{
			Symbols: []PathNode{{
				SymbolID:   fromSym.ID,
				SymbolName: fromSym.Name,
				FilePath:   fromSym.FilePath,
				Line:       fromSym.StartLine,
			}},
			Length: 0,
		}}
		return result, nil
	}

	maxDepth := cfg.MaxDepth
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}
	result.MaxDepth = maxDepth

	if maxPaths <= 0 {
		maxPaths = 10
	}

	// Bidirectional BFS
	type bfsNode struct {
		id   string
		path []string
	}

	// Forward from source
	forwardVisited := make(map[string][]string) // id -> path from source
	forwardVisited[fromSym.ID] = []string{fromSym.ID}
	forwardFrontier := []bfsNode{{id: fromSym.ID, path: []string{fromSym.ID}}}

	// Backward from target
	backwardVisited := make(map[string][]string) // id -> path to target
	backwardVisited[toSym.ID] = []string{toSym.ID}
	backwardFrontier := []bfsNode{{id: toSym.ID, path: []string{toSym.ID}}}

	var paths [][]string

	for depth := 0; depth < maxDepth && len(paths) < maxPaths; depth++ {
		select {
		case <-ctx.Done():
			result.Warnings = append(result.Warnings, "query cancelled")
			break
		default:
		}

		// Expand smaller frontier
		if len(forwardFrontier) <= len(backwardFrontier) && len(forwardFrontier) > 0 {
			newFrontier := []bfsNode{}
			for _, node := range forwardFrontier {
				edges := q.index.GetCallees(node.id, 0)
				for _, edge := range edges {
					if _, visited := forwardVisited[edge.ToID]; visited {
						continue
					}
					newPath := append(append([]string{}, node.path...), edge.ToID)
					forwardVisited[edge.ToID] = newPath
					newFrontier = append(newFrontier, bfsNode{id: edge.ToID, path: newPath})

					// Check for intersection
					if backPath, found := backwardVisited[edge.ToID]; found {
						fullPath := append(newPath[:len(newPath)-1], reverseSlice(backPath)...)
						paths = append(paths, fullPath)
						if !findAll || len(paths) >= maxPaths {
							goto done
						}
					}
				}
			}
			forwardFrontier = newFrontier
		} else if len(backwardFrontier) > 0 {
			newFrontier := []bfsNode{}
			for _, node := range backwardFrontier {
				edges := q.index.GetCallers(node.id, 0)
				for _, edge := range edges {
					if _, visited := backwardVisited[edge.FromID]; visited {
						continue
					}
					newPath := append([]string{edge.FromID}, node.path...)
					backwardVisited[edge.FromID] = newPath
					newFrontier = append(newFrontier, bfsNode{id: edge.FromID, path: newPath})

					// Check for intersection
					if forwardPath, found := forwardVisited[edge.FromID]; found {
						fullPath := append(forwardPath[:len(forwardPath)-1], newPath...)
						paths = append(paths, fullPath)
						if !findAll || len(paths) >= maxPaths {
							goto done
						}
					}
				}
			}
			backwardFrontier = newFrontier
		}

		if len(forwardFrontier) == 0 && len(backwardFrontier) == 0 {
			break
		}
	}

done:
	// Convert paths to PathResult
	for _, path := range paths {
		pathResult := PathResult{
			Symbols: make([]PathNode, 0, len(path)),
			Length:  len(path) - 1,
		}

		for _, id := range path {
			sym := q.index.GetByID(id)
			if sym == nil {
				continue
			}
			pathResult.Symbols = append(pathResult.Symbols, PathNode{
				SymbolID:   sym.ID,
				SymbolName: sym.Name,
				FilePath:   sym.FilePath,
				Line:       sym.StartLine,
			})
		}

		result.Paths = append(result.Paths, pathResult)
	}

	result.PathFound = len(result.Paths) > 0
	result.PathCount = len(result.Paths)
	result.Truncated = len(paths) >= maxPaths

	return result, nil
}

// resolveSymbol resolves a symbol input string to a Symbol.
func (q *Querier) resolveSymbol(input string, exact bool) (*initializer.Symbol, error) {
	// Try exact ID match first
	if sym := q.index.GetByID(input); sym != nil {
		return sym, nil
	}

	// Try file:line format
	if file, line, ok := parseFileLine(input); ok {
		syms := q.index.GetByFile(file)
		for _, sym := range syms {
			if sym.StartLine <= line && line <= sym.EndLine {
				return sym, nil
			}
		}
		// Try partial file match
		for _, sym := range q.index.Symbols {
			if strings.HasSuffix(sym.FilePath, file) && sym.StartLine <= line && line <= sym.EndLine {
				return &sym, nil
			}
		}
	}

	// Try name match
	matches := q.index.GetByName(input)
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		if exact {
			ambErr := &AmbiguousSymbolError{Input: input}
			for _, m := range matches {
				ambErr.Matches = append(ambErr.Matches, SymbolMatch{
					ID:       m.ID,
					Name:     m.Name,
					FilePath: m.FilePath,
					Line:     m.StartLine,
				})
			}
			return nil, ambErr
		}
		// Return first match if not exact
		return matches[0], nil
	}

	// Try partial name match (package.function format)
	parts := strings.Split(input, ".")
	if len(parts) >= 2 {
		funcName := parts[len(parts)-1]
		matches := q.index.GetByName(funcName)
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 && !exact {
			return matches[0], nil
		}
	}

	return nil, &SymbolNotFoundError{Input: input}
}

// parseFileLine parses "file:line" format.
func parseFileLine(input string) (file string, line int, ok bool) {
	idx := strings.LastIndex(input, ":")
	if idx < 0 {
		return "", 0, false
	}

	file = input[:idx]
	lineStr := input[idx+1:]

	l, err := strconv.Atoi(lineStr)
	if err != nil {
		return "", 0, false
	}

	return file, l, true
}

// isTestFile checks if a file path looks like a test file.
func isTestFile(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, "_test.go") ||
		strings.HasPrefix(base, "test_") ||
		strings.Contains(base, "_test_") ||
		strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".test.js") ||
		strings.HasSuffix(base, ".spec.ts") ||
		strings.HasSuffix(base, ".spec.js")
}

// isStdlibSymbol checks if a symbol looks like it's from the standard library.
func isStdlibSymbol(sym *initializer.Symbol) bool {
	// Simple heuristic: stdlib paths don't have project-like prefixes
	return !strings.Contains(sym.FilePath, "/") && !strings.Contains(sym.FilePath, "\\")
}

// reverseSlice reverses a string slice.
func reverseSlice(s []string) []string {
	result := make([]string, len(s))
	for i, v := range s {
		result[len(s)-1-i] = v
	}
	return result
}
