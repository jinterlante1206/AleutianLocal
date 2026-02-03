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
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
)

// RefreshConfig configures refresh behavior.
type RefreshConfig struct {
	// MaxFilesToRefresh limits incremental refresh size.
	// If exceeded, returns error suggesting full rebuild.
	// Default: 50
	MaxFilesToRefresh int

	// ParallelParsing enables concurrent file parsing.
	// Default: true
	ParallelParsing bool

	// ParallelWorkers is the number of parallel parse workers.
	// Default: 4
	ParallelWorkers int

	// Timeout is the maximum time for a refresh operation.
	// Default: 30s
	Timeout time.Duration
}

// DefaultRefreshConfig returns sensible defaults.
func DefaultRefreshConfig() RefreshConfig {
	return RefreshConfig{
		MaxFilesToRefresh: 50,
		ParallelParsing:   true,
		ParallelWorkers:   4,
		Timeout:           30 * time.Second,
	}
}

// RefreshResult contains statistics about an incremental refresh.
type RefreshResult struct {
	// FilesRefreshed is the number of files processed.
	FilesRefreshed int

	// NodesRemoved is the count of nodes removed from old files.
	NodesRemoved int

	// NodesAdded is the count of nodes added from new parses.
	NodesAdded int

	// Duration is how long the refresh took.
	Duration time.Duration

	// ParseErrors contains errors for files that failed to parse.
	ParseErrors []FileParseError
}

// FileParseError represents a parse error for a single file.
type FileParseError struct {
	FilePath string
	Err      error
}

// Refresher handles incremental graph updates.
//
// Description:
//
//	Provides incremental refresh capability for the code graph.
//	When files are modified by agent tools, the Refresher can
//	update only the affected portions of the graph without
//	requiring a full rebuild.
//
// Thread Safety:
//
//	The Refresher is safe for concurrent use. During refresh,
//	a clone of the graph is modified, then atomically swapped.
type Refresher struct {
	// graph is a pointer to the pointer, allowing atomic swap
	graph   **Graph
	graphMu sync.RWMutex

	// parsers is the registry of language parsers
	parsers *ast.ParserRegistry

	// logger for refresh operations
	logger *slog.Logger

	// config holds the refresh configuration
	config RefreshConfig
}

// RefresherOption configures a Refresher.
type RefresherOption func(*Refresher)

// WithRefresherLogger sets the logger.
func WithRefresherLogger(logger *slog.Logger) RefresherOption {
	return func(r *Refresher) {
		r.logger = logger
	}
}

// WithRefresherConfig sets the refresh configuration.
func WithRefresherConfig(config RefreshConfig) RefresherOption {
	return func(r *Refresher) {
		r.config = config
	}
}

// NewRefresher creates a new Refresher.
//
// Description:
//
//	Creates a refresher bound to a graph pointer. The graph pointer
//	can be swapped atomically during refresh operations.
//
// Inputs:
//
//	graph - Pointer to the graph pointer (allows atomic swap).
//	parsers - Parser registry for re-parsing files.
//	opts - Optional configuration.
//
// Outputs:
//
//	*Refresher - The configured refresher.
func NewRefresher(graph **Graph, parsers *ast.ParserRegistry, opts ...RefresherOption) *Refresher {
	r := &Refresher{
		graph:   graph,
		parsers: parsers,
		logger:  slog.Default(),
		config:  DefaultRefreshConfig(),
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// RefreshFiles incrementally updates the graph for modified files.
//
// Description:
//
//	Re-parses only the specified files and updates graph nodes.
//	Uses a copy-on-write pattern: clones the graph, modifies the
//	clone, then atomically swaps if successful.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout.
//	paths - Files to refresh (absolute paths).
//
// Outputs:
//
//	*RefreshResult - Statistics about the refresh.
//	error - Non-nil if refresh failed completely.
//
// Errors:
//
//	Returns error if:
//	  - Too many files (exceeds MaxFilesToRefresh)
//	  - Context cancelled or timed out
//	  - Graph is nil
//
// Thread Safety:
//
//	Safe for concurrent use. Uses copy-on-write to avoid
//	blocking readers during refresh.
func (r *Refresher) RefreshFiles(ctx context.Context, paths []string) (*RefreshResult, error) {
	if len(paths) == 0 {
		return &RefreshResult{}, nil
	}

	// Apply timeout if configured
	if r.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.config.Timeout)
		defer cancel()
	}

	// Check limit
	if len(paths) > r.config.MaxFilesToRefresh {
		return nil, fmt.Errorf("too many files to refresh (%d > %d), consider full rebuild",
			len(paths), r.config.MaxFilesToRefresh)
	}

	startTime := time.Now()
	result := &RefreshResult{
		FilesRefreshed: len(paths),
		ParseErrors:    make([]FileParseError, 0),
	}

	r.logger.Info("starting incremental refresh",
		slog.Int("file_count", len(paths)),
	)

	// Get current graph
	r.graphMu.RLock()
	currentGraph := *r.graph
	r.graphMu.RUnlock()

	if currentGraph == nil {
		return nil, fmt.Errorf("graph is nil")
	}

	// Step 1: Clone the graph for safe modification
	clonedGraph := currentGraph.Clone()

	// Step 2: Parse modified files
	parseResults, parseErrors := r.parseFiles(ctx, paths)
	result.ParseErrors = parseErrors

	// Check for context cancellation
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("refresh cancelled: %w", err)
	}

	// Step 3: Remove old nodes for these files from clone
	for _, path := range paths {
		removed, err := clonedGraph.RemoveFile(path)
		if err != nil {
			r.logger.Warn("failed to remove file from graph",
				slog.String("path", path),
				slog.String("error", err.Error()),
			)
			continue
		}
		result.NodesRemoved += removed
	}

	// Step 4: Add new nodes from parse results
	for _, pr := range parseResults {
		if pr == nil {
			continue
		}

		added, err := clonedGraph.MergeParseResult(pr)
		if err != nil {
			r.logger.Warn("failed to merge parse result",
				slog.String("path", pr.FilePath),
				slog.String("error", err.Error()),
			)
			continue
		}
		result.NodesAdded += added
	}

	// Step 5: Freeze the cloned graph
	clonedGraph.Freeze()

	// Step 6: Atomic swap
	r.graphMu.Lock()
	*r.graph = clonedGraph
	r.graphMu.Unlock()

	result.Duration = time.Since(startTime)

	r.logger.Info("incremental refresh complete",
		slog.Int("nodes_removed", result.NodesRemoved),
		slog.Int("nodes_added", result.NodesAdded),
		slog.Duration("duration", result.Duration),
		slog.Int("parse_errors", len(result.ParseErrors)),
	)

	return result, nil
}

// parseFiles parses the given file paths.
func (r *Refresher) parseFiles(ctx context.Context, paths []string) ([]*ast.ParseResult, []FileParseError) {
	if !r.config.ParallelParsing || len(paths) == 1 {
		return r.parseFilesSequential(ctx, paths)
	}
	return r.parseFilesParallel(ctx, paths)
}

// parseFilesSequential parses files one at a time.
func (r *Refresher) parseFilesSequential(ctx context.Context, paths []string) ([]*ast.ParseResult, []FileParseError) {
	results := make([]*ast.ParseResult, 0, len(paths))
	errors := make([]FileParseError, 0)

	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			break
		}

		pr, err := r.parseFile(ctx, path)
		if err != nil {
			errors = append(errors, FileParseError{FilePath: path, Err: err})
			continue
		}
		results = append(results, pr)
	}

	return results, errors
}

// parseFilesParallel parses files concurrently using a worker pool.
func (r *Refresher) parseFilesParallel(ctx context.Context, paths []string) ([]*ast.ParseResult, []FileParseError) {
	numWorkers := r.config.ParallelWorkers
	if numWorkers <= 0 {
		numWorkers = 4
	}
	if numWorkers > len(paths) {
		numWorkers = len(paths)
	}

	type parseJob struct {
		path string
		idx  int
	}

	type parseResultItem struct {
		result *ast.ParseResult
		err    error
		idx    int
	}

	jobs := make(chan parseJob, len(paths))
	resultsCh := make(chan parseResultItem, len(paths))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					resultsCh <- parseResultItem{idx: job.idx, err: ctx.Err()}
					continue
				}

				pr, err := r.parseFile(ctx, job.path)
				resultsCh <- parseResultItem{
					result: pr,
					err:    err,
					idx:    job.idx,
				}
			}
		}()
	}

	// Send jobs
	for i, path := range paths {
		jobs <- parseJob{path: path, idx: i}
	}
	close(jobs)

	// Wait for workers and close results
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	// Collect results in order
	orderedResults := make([]*ast.ParseResult, len(paths))
	var parseErrors []FileParseError
	var received int32

	for item := range resultsCh {
		atomic.AddInt32(&received, 1)
		if item.err != nil {
			parseErrors = append(parseErrors, FileParseError{
				FilePath: paths[item.idx],
				Err:      item.err,
			})
		} else {
			orderedResults[item.idx] = item.result
		}
	}

	// Compact results (remove nils)
	compacted := make([]*ast.ParseResult, 0, len(paths))
	for _, pr := range orderedResults {
		if pr != nil {
			compacted = append(compacted, pr)
		}
	}

	return compacted, parseErrors
}

// parseFile parses a single file.
func (r *Refresher) parseFile(ctx context.Context, path string) (*ast.ParseResult, error) {
	// Read file content
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	// Get parser for this file by extension
	ext := getFileExtension(path)
	parser, ok := r.parsers.GetByExtension(ext)
	if !ok {
		return nil, fmt.Errorf("no parser for extension %q (file: %s)", ext, path)
	}

	// Parse the file
	result, err := parser.Parse(ctx, content, path)
	if err != nil {
		return nil, fmt.Errorf("parsing file: %w", err)
	}

	return result, nil
}

// getFileExtension extracts the file extension including the dot.
func getFileExtension(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return path[i:]
		}
		if path[i] == '/' || path[i] == '\\' {
			break
		}
	}
	return ""
}

// GetGraph returns the current graph.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (r *Refresher) GetGraph() *Graph {
	r.graphMu.RLock()
	defer r.graphMu.RUnlock()
	return *r.graph
}

// SetGraph replaces the current graph.
//
// Description:
//
//	Used to set a new graph, for example after a full rebuild.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (r *Refresher) SetGraph(g *Graph) {
	r.graphMu.Lock()
	defer r.graphMu.Unlock()
	*r.graph = g
}

// GraphHolder provides thread-safe access to a mutable graph reference.
//
// Description:
//
//	Wraps a graph pointer with a mutex for thread-safe swapping.
//	Used by Refresher to atomically update the graph.
//
// Thread Safety:
//
//	All methods are safe for concurrent use.
type GraphHolder struct {
	graph *Graph
	mu    sync.RWMutex
}

// NewGraphHolder creates a new GraphHolder.
func NewGraphHolder(g *Graph) *GraphHolder {
	return &GraphHolder{graph: g}
}

// Get returns the current graph.
func (h *GraphHolder) Get() *Graph {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.graph
}

// Set replaces the current graph.
func (h *GraphHolder) Set(g *Graph) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.graph = g
}

// GetPtr returns a pointer to the graph pointer.
//
// Description:
//
//	Returns a pointer suitable for use with NewRefresher.
//	The returned pointer should only be used with the Refresher.
func (h *GraphHolder) GetPtr() **Graph {
	return &h.graph
}

// NullLogger returns a logger that discards all output.
func NullLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
