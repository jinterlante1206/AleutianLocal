// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package nodes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/dag"
)

// ParseFilesNode parses source files using the AST parser registry.
//
// Description:
//
//	Reads source files from disk and parses them using language-specific
//	parsers registered in the ParserRegistry. Supports parallel parsing
//	for improved performance on multi-file projects.
//
// Inputs (from map[string]any):
//
//	"project_root" (string): Absolute path to project root. Required.
//	"files" ([]string): List of file paths to parse. Required.
//	    Paths can be relative (to project_root) or absolute.
//
// Outputs:
//
//	*ParseFilesOutput containing:
//	  - Results: Parsed results for each file
//	  - Errors: Files that failed to parse
//	  - Duration: Total parsing time
//
// Thread Safety:
//
//	Safe for concurrent use.
type ParseFilesNode struct {
	dag.BaseNode
	registry    *ast.ParserRegistry
	parallelism int
}

// ParseFilesOutput contains the results of file parsing.
type ParseFilesOutput struct {
	// Results contains successfully parsed files.
	Results []*ast.ParseResult

	// Errors contains files that failed to parse.
	Errors []ParseError

	// FilesProcessed is the total number of files attempted.
	FilesProcessed int

	// Duration is the total parsing time.
	Duration time.Duration
}

// ParseError represents a single file parse failure.
type ParseError struct {
	FilePath string
	Error    string
}

// NewParseFilesNode creates a new parse files node.
//
// Inputs:
//
//	registry - The parser registry to use. Must not be nil.
//	deps - Names of nodes this node depends on.
//
// Outputs:
//
//	*ParseFilesNode - The configured node.
func NewParseFilesNode(registry *ast.ParserRegistry, deps []string) *ParseFilesNode {
	return &ParseFilesNode{
		BaseNode: dag.BaseNode{
			NodeName:         "PARSE_FILES",
			NodeDependencies: deps,
			NodeTimeout:      5 * time.Minute,
			NodeRetryable:    false,
		},
		registry:    registry,
		parallelism: 4, // Default parallelism
	}
}

// WithParallelism sets the number of parallel parsing goroutines.
func (n *ParseFilesNode) WithParallelism(p int) *ParseFilesNode {
	if p > 0 {
		n.parallelism = p
	}
	return n
}

// Execute parses the specified files.
//
// Description:
//
//	Parses source files in parallel using the configured parser registry.
//	Files are grouped by extension and parsed using the appropriate parser.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	inputs - Map containing "project_root" and "files".
//
// Outputs:
//
//	*ParseFilesOutput - The parse results.
//	error - Non-nil if critical failure (individual file errors in output).
//
// Thread Safety:
//
//	Safe for concurrent use.
func (n *ParseFilesNode) Execute(ctx context.Context, inputs map[string]any) (any, error) {
	if n.registry == nil {
		return nil, fmt.Errorf("%w: parser registry", ErrNilDependency)
	}

	// Extract inputs
	projectRoot, files, err := n.extractInputs(inputs)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return &ParseFilesOutput{
			Results:        make([]*ast.ParseResult, 0),
			Errors:         make([]ParseError, 0),
			FilesProcessed: 0,
		}, nil
	}

	start := time.Now()

	// Parse files in parallel
	results, parseErrors := n.parseParallel(ctx, projectRoot, files)

	return &ParseFilesOutput{
		Results:        results,
		Errors:         parseErrors,
		FilesProcessed: len(files),
		Duration:       time.Since(start),
	}, nil
}

// extractInputs validates and extracts inputs from the map.
func (n *ParseFilesNode) extractInputs(inputs map[string]any) (string, []string, error) {
	// Extract project root
	rootRaw, ok := inputs["project_root"]
	if !ok {
		// Try "root" as fallback
		rootRaw, ok = inputs["root"]
		if !ok {
			return "", nil, fmt.Errorf("%w: project_root", ErrMissingInput)
		}
	}

	projectRoot, ok := rootRaw.(string)
	if !ok {
		return "", nil, fmt.Errorf("%w: project_root must be string", ErrInvalidInputType)
	}

	// Extract files
	filesRaw, ok := inputs["files"]
	if !ok {
		return "", nil, fmt.Errorf("%w: files", ErrMissingInput)
	}

	files, ok := filesRaw.([]string)
	if !ok {
		// Try []any
		filesAny, ok := filesRaw.([]any)
		if !ok {
			return "", nil, fmt.Errorf("%w: files must be []string", ErrInvalidInputType)
		}
		files = make([]string, len(filesAny))
		for i, f := range filesAny {
			s, ok := f.(string)
			if !ok {
				return "", nil, fmt.Errorf("%w: files[%d] must be string", ErrInvalidInputType, i)
			}
			files[i] = s
		}
	}

	return projectRoot, files, nil
}

// parseParallel parses files using a worker pool.
func (n *ParseFilesNode) parseParallel(
	ctx context.Context,
	projectRoot string,
	files []string,
) ([]*ast.ParseResult, []ParseError) {
	type result struct {
		index  int
		result *ast.ParseResult
		err    *ParseError
	}

	results := make([]*ast.ParseResult, 0, len(files))
	parseErrors := make([]ParseError, 0)

	// Use semaphore for parallelism control
	sem := make(chan struct{}, n.parallelism)
	resultCh := make(chan result, len(files))

	var wg sync.WaitGroup

	for i, file := range files {
		wg.Add(1)
		go func(idx int, filePath string) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				resultCh <- result{index: idx, err: &ParseError{
					FilePath: filePath,
					Error:    ctx.Err().Error(),
				}}
				return
			}

			// Parse file
			parseResult, parseErr := n.parseFile(ctx, projectRoot, filePath)
			if parseErr != nil {
				resultCh <- result{index: idx, err: parseErr}
			} else {
				resultCh <- result{index: idx, result: parseResult}
			}
		}(i, file)
	}

	// Close result channel when all done
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect results
	for r := range resultCh {
		if r.err != nil {
			parseErrors = append(parseErrors, *r.err)
		} else if r.result != nil {
			results = append(results, r.result)
		}
	}

	return results, parseErrors
}

// parseFile parses a single file.
func (n *ParseFilesNode) parseFile(
	ctx context.Context,
	projectRoot string,
	filePath string,
) (*ast.ParseResult, *ParseError) {
	// Resolve path
	absPath := filePath
	if !filepath.IsAbs(filePath) {
		absPath = filepath.Join(projectRoot, filePath)
	}

	// Get parser for extension
	ext := filepath.Ext(absPath)
	parser, ok := n.registry.GetByExtension(ext)
	if !ok {
		return nil, &ParseError{
			FilePath: filePath,
			Error:    fmt.Sprintf("%s: %s", ErrParserNotFound.Error(), ext),
		}
	}

	// Read file
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, &ParseError{
			FilePath: filePath,
			Error:    fmt.Sprintf("read file: %v", err),
		}
	}

	// Parse
	result, err := parser.Parse(ctx, content, filePath)
	if err != nil {
		return nil, &ParseError{
			FilePath: filePath,
			Error:    fmt.Sprintf("parse: %v", err),
		}
	}

	return result, nil
}
