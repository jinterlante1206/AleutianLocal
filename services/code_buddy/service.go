// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package code_buddy provides the Code Buddy HTTP service for code analysis.
//
// The service exposes endpoints for:
//   - Initializing and caching code graphs
//   - Querying symbols and relationships
//   - Assembling context for LLM prompts
//   - Seeding library documentation
package code_buddy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	cbcontext "github.com/AleutianAI/AleutianFOSS/services/code_buddy/context"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

// ServiceConfig configures the Code Buddy service.
type ServiceConfig struct {
	// MaxInitDuration is the maximum time allowed for init operations.
	// Default: 30s
	MaxInitDuration time.Duration

	// MaxProjectFiles is the maximum number of files to parse.
	// Default: 10000
	MaxProjectFiles int

	// MaxProjectSize is the maximum total size of files in bytes.
	// Default: 100MB
	MaxProjectSize int64

	// MaxCachedGraphs is the maximum number of graphs to cache.
	// Default: 5
	MaxCachedGraphs int

	// GraphTTL is how long graphs are cached before expiry.
	// Default: 0 (no expiry)
	GraphTTL time.Duration

	// AllowedRoots is an optional list of allowed project root prefixes.
	// If empty, all paths are allowed. Security feature.
	AllowedRoots []string
}

// DefaultServiceConfig returns sensible defaults.
func DefaultServiceConfig() ServiceConfig {
	return ServiceConfig{
		MaxInitDuration: 30 * time.Second,
		MaxProjectFiles: 10000,
		MaxProjectSize:  100 * 1024 * 1024, // 100MB
		MaxCachedGraphs: 5,
		GraphTTL:        0, // No expiry
	}
}

// Service is the Code Buddy service.
//
// Thread Safety:
//
//	Service is safe for concurrent use. Multiple goroutines can call
//	any combination of methods simultaneously.
type Service struct {
	config    ServiceConfig
	graphs    map[string]*CachedGraph
	mu        sync.RWMutex
	initLocks sync.Map // projectRoot -> *sync.Mutex

	// registry holds parser instances
	registry *ast.ParserRegistry

	// libDocProvider is optional library documentation provider
	libDocProvider cbcontext.LibraryDocProvider
}

// NewService creates a new Code Buddy service.
//
// Description:
//
//	Creates a service with the given configuration. The service starts
//	with no cached graphs and a default parser registry.
//
// Inputs:
//
//	config - Service configuration
//
// Outputs:
//
//	*Service - The configured service
func NewService(config ServiceConfig) *Service {
	svc := &Service{
		config:   config,
		graphs:   make(map[string]*CachedGraph),
		registry: ast.NewParserRegistry(),
	}

	// Register default parsers
	svc.registry.Register(ast.NewGoParser())
	svc.registry.Register(ast.NewPythonParser())
	svc.registry.Register(ast.NewTypeScriptParser())
	svc.registry.Register(ast.NewJavaScriptParser())

	return svc
}

// SetLibraryDocProvider sets the library documentation provider.
func (s *Service) SetLibraryDocProvider(p cbcontext.LibraryDocProvider) {
	s.libDocProvider = p
}

// Init initializes a code graph for a project.
//
// Description:
//
//	Parses the project, builds the code graph and symbol index, and
//	caches the result. If a graph already exists for the project, it
//	is replaced.
//
// Inputs:
//
//	ctx - Context for cancellation
//	projectRoot - Absolute path to the project root
//	languages - Languages to parse (default: ["go"])
//	excludes - Glob patterns to exclude (default: ["vendor/*", "*_test.go"])
//
// Outputs:
//
//	*InitResponse - Graph statistics and metadata
//	error - Non-nil if validation fails or parsing fails
//
// Errors:
//
//	ErrRelativePath - Project root is not absolute
//	ErrPathTraversal - Project root contains .. sequences
//	ErrProjectTooLarge - Project exceeds configured limits
//	ErrInitInProgress - Another init is running for this project
//	ErrInitTimeout - Init took too long
func (s *Service) Init(ctx context.Context, projectRoot string, languages, excludes []string) (*InitResponse, error) {
	// Validate project root
	if err := s.validateProjectRoot(projectRoot); err != nil {
		return nil, err
	}

	// Apply defaults
	if len(languages) == 0 {
		languages = []string{"go"}
	}
	if len(excludes) == 0 {
		excludes = []string{"vendor/*", "*_test.go"}
	}

	// Get init lock for this project to prevent concurrent inits
	lock := s.getInitLock(projectRoot)
	if !lock.TryLock() {
		return nil, ErrInitInProgress
	}
	defer lock.Unlock()

	// Apply timeout
	if s.config.MaxInitDuration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.config.MaxInitDuration)
		defer cancel()
	}

	start := time.Now()

	// Generate graph ID
	graphID := s.generateGraphID(projectRoot)

	// Check if we're replacing an existing graph
	s.mu.RLock()
	existing, isRefresh := s.graphs[graphID]
	var previousID string
	if isRefresh && existing != nil {
		previousID = graphID
	}
	s.mu.RUnlock()

	// Create new graph and index
	g := graph.NewGraph(projectRoot)
	idx := index.NewSymbolIndex()

	// Parse files
	result, err := s.parseProject(ctx, projectRoot, languages, excludes, g, idx)
	if err != nil {
		return nil, err
	}

	// Freeze the graph
	g.Freeze()

	// Create assembler
	assembler := cbcontext.NewAssembler(g, idx)
	if s.libDocProvider != nil {
		assembler = assembler.WithLibraryDocProvider(s.libDocProvider)
	}

	// Cache the graph
	cached := &CachedGraph{
		Graph:        g,
		Index:        idx,
		Assembler:    assembler,
		BuiltAtMilli: time.Now().UnixMilli(),
		ProjectRoot:  projectRoot,
	}

	if s.config.GraphTTL > 0 {
		cached.ExpiresAtMilli = time.Now().Add(s.config.GraphTTL).UnixMilli()
	}

	s.mu.Lock()
	s.graphs[graphID] = cached
	s.evictIfNeeded()
	s.mu.Unlock()

	return &InitResponse{
		GraphID:          graphID,
		IsRefresh:        isRefresh,
		PreviousID:       previousID,
		FilesParsed:      result.FilesParsed,
		SymbolsExtracted: result.SymbolsExtracted,
		EdgesBuilt:       g.EdgeCount(),
		ParseTimeMs:      time.Since(start).Milliseconds(),
		Errors:           result.Errors,
	}, nil
}

// parseResult holds intermediate parsing results.
type parseResult struct {
	FilesParsed      int
	SymbolsExtracted int
	Errors           []string
}

// parseProject parses project files and builds the graph.
func (s *Service) parseProject(ctx context.Context, projectRoot string, languages, excludes []string, g *graph.Graph, idx *index.SymbolIndex) (*parseResult, error) {
	result := &parseResult{
		Errors: make([]string, 0),
	}

	// Walk the project directory
	fileCount := 0
	var totalSize int64

	err := filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		// Check context
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Skip directories
		if d.IsDir() {
			// Skip excluded directories
			relPath, _ := filepath.Rel(projectRoot, path)
			for _, pattern := range excludes {
				if matched, _ := filepath.Match(pattern, relPath); matched {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(projectRoot, path)
		if err != nil {
			return nil
		}

		// Check exclusions
		for _, pattern := range excludes {
			if matched, _ := filepath.Match(pattern, relPath); matched {
				return nil
			}
		}

		// Check file extension matches languages
		ext := filepath.Ext(path)
		if !s.isLanguageFile(ext, languages) {
			return nil
		}

		// Check limits
		info, err := d.Info()
		if err != nil {
			return nil
		}
		totalSize += info.Size()
		if totalSize > s.config.MaxProjectSize {
			return ErrProjectTooLarge
		}

		fileCount++
		if fileCount > s.config.MaxProjectFiles {
			return ErrProjectTooLarge
		}

		// Parse file
		parseErr := s.parseFile(ctx, path, relPath, g, idx)
		if parseErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", relPath, parseErr))
		} else {
			result.FilesParsed++
		}

		return nil
	})

	if err != nil && err != ErrProjectTooLarge {
		return nil, fmt.Errorf("walking project: %w", err)
	}
	if err == ErrProjectTooLarge {
		return nil, err
	}

	result.SymbolsExtracted = idx.Stats().TotalSymbols

	return result, nil
}

// parseFile parses a single file and adds symbols to graph and index.
func (s *Service) parseFile(ctx context.Context, absPath, relPath string, g *graph.Graph, idx *index.SymbolIndex) error {
	content, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}

	// Determine language from extension
	ext := filepath.Ext(relPath)

	// Get parser for file extension
	parser, ok := s.registry.GetByExtension(ext)
	if !ok {
		return fmt.Errorf("no parser for extension: %s", ext)
	}

	// Parse the file
	parseResult, err := parser.Parse(ctx, content, relPath)
	if err != nil {
		return err
	}

	// Add symbols to graph and index
	for _, sym := range parseResult.Symbols {
		if sym == nil {
			continue
		}

		// Add to graph
		if _, err := g.AddNode(sym); err != nil {
			// Skip duplicates
			continue
		}

		// Add to index
		if err := idx.Add(sym); err != nil {
			// Skip duplicates
			continue
		}
	}

	return nil
}

// isLanguageFile checks if a file extension matches any of the specified languages.
func (s *Service) isLanguageFile(ext string, languages []string) bool {
	// Check if we have a parser for this extension
	parser, ok := s.registry.GetByExtension(ext)
	if !ok {
		return false
	}

	// Check if the parser's language matches any of the requested languages
	parserLang := parser.Language()
	for _, lang := range languages {
		if parserLang == lang {
			return true
		}
	}

	return false
}

// GetContext assembles context for a query.
//
// Description:
//
//	Uses the cached graph to assemble relevant context for an LLM prompt.
//
// Inputs:
//
//	ctx - Context for cancellation
//	graphID - ID of the graph to query
//	query - Search query or task description
//	budget - Maximum tokens to use
//
// Outputs:
//
//	*ContextResponse - Assembled context with metadata
//	error - Non-nil if graph not found or assembly fails
func (s *Service) GetContext(ctx context.Context, graphID, query string, budget int) (*ContextResponse, error) {
	cached, err := s.GetGraph(graphID)
	if err != nil {
		return nil, err
	}

	result, err := cached.Assembler.Assemble(ctx, query, budget)
	if err != nil {
		return nil, err
	}

	return &ContextResponse{
		Context:             result.Context,
		TokensUsed:          result.TokensUsed,
		SymbolsIncluded:     result.SymbolsIncluded,
		LibraryDocsIncluded: result.LibraryDocsIncluded,
		Suggestions:         result.Suggestions,
	}, nil
}

// FindCallers returns all symbols that call the given function.
//
// Description:
//
//	Searches the graph for functions that call the named function.
//
// Inputs:
//
//	ctx - Context for cancellation
//	graphID - ID of the graph to query
//	functionName - Name of the function to find callers for
//	limit - Maximum number of results (0 = default)
//
// Outputs:
//
//	[]*SymbolInfo - List of caller symbols
//	error - Non-nil if graph not found
func (s *Service) FindCallers(ctx context.Context, graphID, functionName string, limit int) ([]*SymbolInfo, error) {
	cached, err := s.GetGraph(graphID)
	if err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 50
	}

	results, err := cached.Graph.FindCallersByName(ctx, functionName, graph.WithLimit(limit))
	if err != nil {
		return nil, err
	}

	var callers []*SymbolInfo
	for _, queryResult := range results {
		for _, sym := range queryResult.Symbols {
			callers = append(callers, SymbolInfoFromAST(sym))
		}
	}

	return callers, nil
}

// FindImplementations returns all types that implement the given interface.
//
// Description:
//
//	Searches the graph for types that implement the named interface.
//
// Inputs:
//
//	ctx - Context for cancellation
//	graphID - ID of the graph to query
//	interfaceName - Name of the interface to find implementations for
//	limit - Maximum number of results (0 = default)
//
// Outputs:
//
//	[]*SymbolInfo - List of implementing types
//	error - Non-nil if graph not found
func (s *Service) FindImplementations(ctx context.Context, graphID, interfaceName string, limit int) ([]*SymbolInfo, error) {
	cached, err := s.GetGraph(graphID)
	if err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 50
	}

	results, err := cached.Graph.FindImplementationsByName(ctx, interfaceName, graph.WithLimit(limit))
	if err != nil {
		return nil, err
	}

	var implementations []*SymbolInfo
	for _, queryResult := range results {
		for _, sym := range queryResult.Symbols {
			implementations = append(implementations, SymbolInfoFromAST(sym))
		}
	}

	return implementations, nil
}

// GetSymbol retrieves a symbol by its ID.
//
// Description:
//
//	Looks up a symbol in the graph by its unique ID.
//
// Inputs:
//
//	ctx - Context for cancellation
//	graphID - ID of the graph to query
//	symbolID - ID of the symbol to retrieve
//
// Outputs:
//
//	*SymbolInfo - The symbol if found
//	error - Non-nil if graph not found or symbol not found
func (s *Service) GetSymbol(ctx context.Context, graphID, symbolID string) (*SymbolInfo, error) {
	cached, err := s.GetGraph(graphID)
	if err != nil {
		return nil, err
	}

	sym, ok := cached.Index.GetByID(symbolID)
	if !ok {
		return nil, fmt.Errorf("symbol not found: %s", symbolID)
	}

	return SymbolInfoFromAST(sym), nil
}

// GetGraph retrieves a cached graph by ID.
//
// Description:
//
//	Returns the cached graph if it exists and hasn't expired.
//
// Inputs:
//
//	graphID - ID of the graph to retrieve
//
// Outputs:
//
//	*CachedGraph - The cached graph
//	error - ErrGraphNotInitialized if not found, ErrGraphExpired if expired
func (s *Service) GetGraph(graphID string) (*CachedGraph, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cached, ok := s.graphs[graphID]
	if !ok {
		return nil, ErrGraphNotInitialized
	}

	// Check expiry
	if cached.ExpiresAtMilli > 0 && time.Now().UnixMilli() > cached.ExpiresAtMilli {
		return nil, ErrGraphExpired
	}

	return cached, nil
}

// GraphCount returns the number of cached graphs.
func (s *Service) GraphCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.graphs)
}

// validateProjectRoot validates the project root path.
func (s *Service) validateProjectRoot(projectRoot string) error {
	// Must be absolute
	if !filepath.IsAbs(projectRoot) {
		return ErrRelativePath
	}

	// No path traversal
	if strings.Contains(projectRoot, "..") {
		return ErrPathTraversal
	}

	// Resolve symlinks and verify still within allowed roots
	resolved, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	// Check against allowlist if configured
	if len(s.config.AllowedRoots) > 0 {
		allowed := false
		for _, root := range s.config.AllowedRoots {
			if strings.HasPrefix(resolved, root) {
				allowed = true
				break
			}
		}
		if !allowed {
			return ErrPathTraversal
		}
	}

	return nil
}

// generateGraphID creates a deterministic ID for a project.
func (s *Service) generateGraphID(projectRoot string) string {
	hash := sha256.Sum256([]byte(projectRoot))
	return hex.EncodeToString(hash[:])[:16]
}

// getInitLock returns the init lock for a project.
func (s *Service) getInitLock(projectRoot string) *sync.Mutex {
	lock, _ := s.initLocks.LoadOrStore(projectRoot, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// evictIfNeeded removes graphs if over capacity. Caller must hold write lock.
func (s *Service) evictIfNeeded() {
	for len(s.graphs) > s.config.MaxCachedGraphs {
		// Find oldest graph
		var oldestID string
		var oldestTime int64 = time.Now().UnixMilli()
		for id, cached := range s.graphs {
			if cached.BuiltAtMilli < oldestTime {
				oldestTime = cached.BuiltAtMilli
				oldestID = id
			}
		}
		if oldestID != "" {
			delete(s.graphs, oldestID)
		}
	}
}
