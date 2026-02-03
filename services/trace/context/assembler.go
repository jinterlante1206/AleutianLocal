// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package context

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// Assembler combines graph traversal, symbol index, and library docs
// to produce focused context for LLM prompts.
//
// Thread Safety:
//
//	Assembler is safe for concurrent use after construction.
//	The underlying graph and index must be frozen/read-only.
type Assembler struct {
	graph       *graph.Graph
	index       *index.SymbolIndex
	libDocs     LibraryDocProvider
	options     AssembleOptions
	projectRoot string

	// fileCache caches file contents to avoid repeated disk reads.
	// Key is file path, value is file lines.
	fileCache   map[string][]string
	fileCacheMu sync.RWMutex
}

// NewAssembler creates a new context assembler.
//
// Description:
//
//	Creates an assembler with the given graph, symbol index, and optional
//	library documentation provider.
//
// Inputs:
//
//	g - The code graph (must be frozen for queries)
//	idx - The symbol index
//	opts - Functional options for configuration
//
// Outputs:
//
//	*Assembler - The configured assembler
//
// Example:
//
//	assembler := NewAssembler(graph, index,
//	    WithGraphDepth(3),
//	    WithLibraryDocs(true),
//	)
func NewAssembler(g *graph.Graph, idx *index.SymbolIndex, opts ...AssembleOption) *Assembler {
	options := DefaultAssembleOptions()
	for _, opt := range opts {
		opt(&options)
	}

	// Get project root from graph if available
	projectRoot := ""
	if g != nil {
		projectRoot = g.ProjectRoot
	}

	return &Assembler{
		graph:       g,
		index:       idx,
		options:     options,
		projectRoot: projectRoot,
		fileCache:   make(map[string][]string),
	}
}

// WithLibraryDocProvider sets the library documentation provider.
//
// Description:
//
//	Enables library documentation lookup during context assembly.
//	If not set, library docs are skipped gracefully.
func (a *Assembler) WithLibraryDocProvider(p LibraryDocProvider) *Assembler {
	a.libDocs = p
	return a
}

// Assemble creates context for a query within a token budget.
//
// Description:
//
//	Finds entry point symbols from the query, walks the graph to find
//	related code, ranks results by relevance, and packs into the token
//	budget with code, types, and optionally library docs.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout
//	query - The user's query or task description
//	budget - Maximum tokens to use (must be positive)
//
// Outputs:
//
//	*ContextResult - Assembled context with metadata
//	error - Non-nil if validation fails or fatal error occurs
//
// Errors:
//
//	ErrGraphNotInitialized - Graph is nil or not frozen
//	ErrEmptyQuery - Query is empty or whitespace
//	ErrQueryTooLong - Query exceeds MaxQueryLength
//	ErrInvalidBudget - Budget is not positive
//
// Example:
//
//	result, err := assembler.Assemble(ctx, "Add authentication to HandleAgent", 8000)
//	if err != nil {
//	    return err
//	}
//	fmt.Println(result.Context)
func (a *Assembler) Assemble(ctx context.Context, query string, budget int) (*ContextResult, error) {
	// Start tracing span
	ctx, span := startAssembleSpan(ctx, len(query), budget)
	defer span.End()
	start := time.Now()

	// Validate inputs
	if err := a.validateInputs(query, budget); err != nil {
		recordAssembleMetrics(ctx, time.Since(start), 0, 0, false)
		return nil, err
	}

	// Apply timeout from options
	if a.options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.options.Timeout)
		defer cancel()
	}

	result := &ContextResult{
		SymbolsIncluded:     make([]string, 0),
		LibraryDocsIncluded: make([]string, 0),
		Suggestions:         make([]string, 0),
	}

	// Step 1: Find entry points from query
	entryPoints, err := a.findEntryPoints(ctx, query)
	if err != nil {
		recordAssembleMetrics(ctx, time.Since(start), 0, 0, false)
		return nil, err
	}
	recordEntryPointsMetrics(ctx, len(entryPoints))

	if len(entryPoints) == 0 {
		// No matches - return helpful suggestions
		result.Suggestions = append(result.Suggestions,
			"No symbols found matching the query. Try using more specific terms.",
			"Use function or type names that exist in the codebase.",
		)
		result.AssemblyDurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// Step 2: Graph walk to find related symbols
	relatedSymbols, err := a.walkGraph(ctx, entryPoints)
	if err != nil {
		recordAssembleMetrics(ctx, time.Since(start), 0, 0, false)
		return nil, err
	}
	recordGraphWalkMetrics(ctx, len(relatedSymbols))

	// Step 3: Score and rank symbols
	scoredSymbols := a.scoreSymbols(query, relatedSymbols)
	sort.Slice(scoredSymbols, func(i, j int) bool {
		return scoredSymbols[i].Score > scoredSymbols[j].Score
	})

	// Step 4: Calculate budget allocation
	effectiveBudget := int(float64(budget) * (1.0 - TokenSafetyBuffer))
	codeBudget := effectiveBudget * a.options.BudgetAllocation.CodePercent / 100
	typesBudget := effectiveBudget * a.options.BudgetAllocation.TypesPercent / 100
	libDocsBudget := effectiveBudget * a.options.BudgetAllocation.LibDocsPercent / 100

	// Step 5: Pack context within budget
	var contextBuilder strings.Builder

	// Pack primary code
	codeSection, codeTokens, includedSymbols := a.packCodeSection(ctx, scoredSymbols, codeBudget)
	if codeSection != "" {
		contextBuilder.WriteString("## Relevant Code\n\n")
		contextBuilder.WriteString(codeSection)
		result.SymbolsIncluded = includedSymbols
		result.TokensUsed += codeTokens
	}

	// Pack type definitions
	typeSection, typeTokens := a.packTypesSection(ctx, scoredSymbols, typesBudget)
	if typeSection != "" {
		contextBuilder.WriteString("\n## Type Definitions\n\n")
		contextBuilder.WriteString(typeSection)
		result.TokensUsed += typeTokens
	}

	// Pack library docs (if enabled and provider available)
	if a.options.IncludeLibraryDocs && a.libDocs != nil {
		libSection, libTokens, libDocIDs := a.packLibraryDocs(ctx, query, libDocsBudget)
		if libSection != "" {
			contextBuilder.WriteString("\n## Library Reference\n\n")
			contextBuilder.WriteString(libSection)
			result.LibraryDocsIncluded = libDocIDs
			result.TokensUsed += libTokens
		}
	}

	result.Context = contextBuilder.String()
	result.Truncated = len(scoredSymbols) > len(result.SymbolsIncluded)
	result.AssemblyDurationMs = time.Since(start).Milliseconds()

	// Add suggestions for symbols that didn't fit
	if result.Truncated && len(scoredSymbols) > len(result.SymbolsIncluded) {
		for i := len(result.SymbolsIncluded); i < len(scoredSymbols) && i < len(result.SymbolsIncluded)+3; i++ {
			if scoredSymbols[i].Symbol != nil {
				result.Suggestions = append(result.Suggestions,
					fmt.Sprintf("Consider also: %s", scoredSymbols[i].Symbol.FilePath))
			}
		}
	}

	// Record successful assembly metrics
	setAssembleSpanResult(span, result.TokensUsed, len(result.SymbolsIncluded), result.Truncated)
	recordAssembleMetrics(ctx, time.Since(start), result.TokensUsed, len(result.SymbolsIncluded), true)

	return result, nil
}

// validateInputs checks that inputs are valid before assembly.
func (a *Assembler) validateInputs(query string, budget int) error {
	if a.graph == nil || !a.graph.IsFrozen() {
		return ErrGraphNotInitialized
	}

	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		return ErrEmptyQuery
	}

	if len(query) > MaxQueryLength {
		return ErrQueryTooLong
	}

	if budget <= 0 {
		return ErrInvalidBudget
	}

	return nil
}

// findEntryPoints searches the index for symbols matching the query.
func (a *Assembler) findEntryPoints(ctx context.Context, query string) ([]*ast.Symbol, error) {
	if a.index == nil {
		return nil, nil
	}

	// Extract potential symbol names from query
	terms := extractQueryTerms(query)

	seen := make(map[string]bool)
	var entryPoints []*ast.Symbol

	for _, term := range terms {
		if err := ctx.Err(); err != nil {
			break
		}

		// Search for matching symbols
		matches, err := a.index.Search(ctx, term, 10)
		if err != nil {
			continue // Don't fail on search errors
		}

		for _, sym := range matches {
			if !seen[sym.ID] {
				seen[sym.ID] = true
				entryPoints = append(entryPoints, sym)
			}
		}
	}

	return entryPoints, nil
}

// extractQueryTerms splits a query into potential symbol name terms.
func extractQueryTerms(query string) []string {
	// Split on whitespace and common punctuation
	query = strings.ReplaceAll(query, ".", " ")
	query = strings.ReplaceAll(query, ",", " ")
	query = strings.ReplaceAll(query, "(", " ")
	query = strings.ReplaceAll(query, ")", " ")
	query = strings.ReplaceAll(query, "\"", " ")
	query = strings.ReplaceAll(query, "'", " ")

	parts := strings.Fields(query)

	// Filter out common words and short terms
	var terms []string
	commonWords := map[string]bool{
		"the": true, "a": true, "an": true, "to": true, "for": true,
		"in": true, "on": true, "at": true, "by": true, "with": true,
		"add": true, "remove": true, "update": true, "fix": true, "implement": true,
		"create": true, "delete": true, "change": true, "modify": true,
		"function": true, "method": true, "type": true, "struct": true,
		"how": true, "what": true, "where": true, "when": true, "why": true,
	}

	for _, part := range parts {
		lower := strings.ToLower(part)
		if len(part) >= 3 && !commonWords[lower] {
			terms = append(terms, part)
		}
	}

	return terms
}

// walkGraph performs BFS traversal from entry points to find related symbols.
func (a *Assembler) walkGraph(ctx context.Context, entryPoints []*ast.Symbol) (map[string]*ScoredSymbol, error) {
	result := make(map[string]*ScoredSymbol)

	// Add entry points at depth 0
	for _, sym := range entryPoints {
		result[sym.ID] = &ScoredSymbol{
			Symbol: sym,
			Depth:  0,
		}
	}

	// BFS queue
	type queueItem struct {
		symbolID string
		depth    int
	}

	visited := make(map[string]bool)
	var queue []queueItem

	for _, sym := range entryPoints {
		queue = append(queue, queueItem{sym.ID, 0})
		visited[sym.ID] = true
	}

	for len(queue) > 0 && len(result) < a.options.MaxSymbols {
		if err := ctx.Err(); err != nil {
			break
		}

		item := queue[0]
		queue = queue[1:]

		if item.depth >= a.options.GraphDepth {
			continue
		}

		node, ok := a.graph.GetNode(item.symbolID)
		if !ok {
			continue
		}

		// Follow outgoing edges (what this symbol calls/references)
		for _, edge := range node.Outgoing {
			if visited[edge.ToID] {
				continue
			}
			visited[edge.ToID] = true

			targetNode, ok := a.graph.GetNode(edge.ToID)
			if ok && targetNode.Symbol != nil {
				result[edge.ToID] = &ScoredSymbol{
					Symbol: targetNode.Symbol,
					Depth:  item.depth + 1,
				}
				queue = append(queue, queueItem{edge.ToID, item.depth + 1})
			}

			if len(result) >= a.options.MaxSymbols {
				break
			}
		}

		// Follow incoming edges (what calls/references this symbol)
		for _, edge := range node.Incoming {
			if visited[edge.FromID] {
				continue
			}
			visited[edge.FromID] = true

			sourceNode, ok := a.graph.GetNode(edge.FromID)
			if ok && sourceNode.Symbol != nil {
				result[edge.FromID] = &ScoredSymbol{
					Symbol: sourceNode.Symbol,
					Depth:  item.depth + 1,
				}
				queue = append(queue, queueItem{edge.FromID, item.depth + 1})
			}

			if len(result) >= a.options.MaxSymbols {
				break
			}
		}
	}

	return result, nil
}

// scoreSymbols calculates relevance scores for collected symbols.
func (a *Assembler) scoreSymbols(query string, symbols map[string]*ScoredSymbol) []*ScoredSymbol {
	queryLower := strings.ToLower(query)
	queryTerms := extractQueryTerms(query)

	var result []*ScoredSymbol

	for _, scored := range symbols {
		if scored.Symbol == nil {
			continue
		}

		// Calculate query similarity (0.0-1.0)
		querySim := calculateQuerySimilarity(scored.Symbol.Name, queryLower, queryTerms)

		// Calculate graph distance score (1.0/(depth+1))
		graphDist := 1.0 / float64(scored.Depth+1)

		// Get symbol importance
		importance := SymbolImportance(scored.Symbol.Kind)

		// Combined score: (querySim * 0.4) + (graphDist * 0.3) + (importance * 0.3)
		scored.Score = (querySim * 0.4) + (graphDist * 0.3) + (importance * 0.3)

		result = append(result, scored)
	}

	return result
}

// calculateQuerySimilarity computes how well a symbol name matches the query.
func calculateQuerySimilarity(name, queryLower string, queryTerms []string) float64 {
	nameLower := strings.ToLower(name)

	// Exact match
	if nameLower == queryLower {
		return 1.0
	}

	// Check if name matches any query term exactly
	for _, term := range queryTerms {
		termLower := strings.ToLower(term)
		if nameLower == termLower {
			return 0.95
		}
	}

	// Prefix match with any term
	for _, term := range queryTerms {
		termLower := strings.ToLower(term)
		if strings.HasPrefix(nameLower, termLower) || strings.HasPrefix(termLower, nameLower) {
			return 0.8
		}
	}

	// Contains match
	for _, term := range queryTerms {
		termLower := strings.ToLower(term)
		if strings.Contains(nameLower, termLower) || strings.Contains(termLower, nameLower) {
			return 0.6
		}
	}

	// No match
	return 0.0
}

// estimateTokens estimates token count from text length.
func estimateTokens(text string) int {
	return int(float64(len(text)) / CharsPerToken)
}

// packCodeSection formats code symbols into markdown within budget.
func (a *Assembler) packCodeSection(ctx context.Context, symbols []*ScoredSymbol, budget int) (string, int, []string) {
	var builder strings.Builder
	var included []string
	tokensUsed := 0

	for _, scored := range symbols {
		if err := ctx.Err(); err != nil {
			break
		}

		if scored.Symbol == nil {
			continue
		}

		// Skip non-code symbols for this section
		kind := scored.Symbol.Kind
		if kind == ast.SymbolKindImport || kind == ast.SymbolKindPackage {
			continue
		}

		// Format symbol as markdown code block with full source
		section := a.formatCodeSymbol(scored.Symbol)
		sectionTokens := estimateTokens(section)

		if tokensUsed+sectionTokens > budget {
			break
		}

		builder.WriteString(section)
		builder.WriteString("\n")
		tokensUsed += sectionTokens
		included = append(included, scored.Symbol.ID)
	}

	return builder.String(), tokensUsed, included
}

// formatCodeSymbol formats a symbol as a markdown code block with full source.
//
// Description:
//
//	Reads the actual source code from disk using the symbol's file path
//	and line range (StartLine to EndLine). Falls back to signature-only
//	if the file cannot be read.
//
// Thread Safety: This method is safe for concurrent use.
func (a *Assembler) formatCodeSymbol(sym *ast.Symbol) string {
	var builder strings.Builder

	// Header with file path and lines
	builder.WriteString(fmt.Sprintf("### %s (lines %d-%d)\n",
		sym.FilePath, sym.StartLine, sym.EndLine))

	// Code block with language
	lang := sym.Language
	if lang == "" {
		lang = "go" // Default to Go for this codebase
	}
	builder.WriteString(fmt.Sprintf("```%s\n", lang))

	// Try to read the actual source code
	sourceCode, err := a.readSourceLines(sym.FilePath, sym.StartLine, sym.EndLine)
	if err == nil && sourceCode != "" {
		// Include doc comment if it's not already part of the source
		if sym.DocComment != "" && !strings.Contains(sourceCode, sym.DocComment) {
			builder.WriteString(sym.DocComment)
			builder.WriteString("\n")
		}
		builder.WriteString(sourceCode)
	} else {
		// Fallback to signature-only if file read fails
		if sym.DocComment != "" {
			builder.WriteString(sym.DocComment)
			builder.WriteString("\n")
		}
		if sym.Signature != "" {
			builder.WriteString(sym.Signature)
		} else {
			builder.WriteString(fmt.Sprintf("%s %s", sym.Kind.String(), sym.Name))
		}
	}

	builder.WriteString("\n```\n")

	return builder.String()
}

// GetSymbolSourceCode returns the source code for a symbol.
//
// Description:
//
//	Reads the actual source code from disk using the symbol's file path
//	and line range. Returns the signature as fallback if file read fails.
//
// Inputs:
//
//	sym - The symbol to get source code for.
//
// Outputs:
//
//	string - The source code (or signature as fallback).
//
// Thread Safety: This method is safe for concurrent use.
func (a *Assembler) GetSymbolSourceCode(sym *ast.Symbol) string {
	if sym == nil {
		return ""
	}

	// Try to read the actual source code
	sourceCode, err := a.readSourceLines(sym.FilePath, sym.StartLine, sym.EndLine)
	if err == nil && sourceCode != "" {
		return sourceCode
	}

	// Fallback to signature
	if sym.Signature != "" {
		return sym.Signature
	}

	return fmt.Sprintf("%s %s", sym.Kind.String(), sym.Name)
}

// readSourceLines reads lines from a source file, using cache when available.
//
// Description:
//
//	Reads the specified line range from a source file. Uses an internal
//	cache to avoid repeated disk reads for the same file.
//
// Inputs:
//
//	filePath - Relative path to the source file (from project root)
//	startLine - First line to read (1-indexed, inclusive)
//	endLine - Last line to read (1-indexed, inclusive)
//
// Outputs:
//
//	string - The source code lines joined with newlines
//	error - Non-nil if file cannot be read
//
// Thread Safety: This method is safe for concurrent use.
func (a *Assembler) readSourceLines(filePath string, startLine, endLine int) (string, error) {
	if a.projectRoot == "" {
		return "", fmt.Errorf("project root not set")
	}

	// Get file lines from cache or read from disk
	lines, err := a.getFileLines(filePath)
	if err != nil {
		return "", err
	}

	// Validate line range
	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	if startLine > endLine || startLine > len(lines) {
		return "", fmt.Errorf("invalid line range: %d-%d (file has %d lines)", startLine, endLine, len(lines))
	}

	// Extract the requested lines (convert to 0-indexed)
	selectedLines := lines[startLine-1 : endLine]
	return strings.Join(selectedLines, "\n"), nil
}

// getFileLines returns the lines of a file, using cache when available.
func (a *Assembler) getFileLines(filePath string) ([]string, error) {
	// Check cache first
	a.fileCacheMu.RLock()
	if lines, ok := a.fileCache[filePath]; ok {
		a.fileCacheMu.RUnlock()
		return lines, nil
	}
	a.fileCacheMu.RUnlock()

	// Read file from disk
	fullPath := filepath.Join(a.projectRoot, filePath)
	file, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("open file %s: %w", filePath, err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read file %s: %w", filePath, err)
	}

	// Cache the result
	a.fileCacheMu.Lock()
	a.fileCache[filePath] = lines
	a.fileCacheMu.Unlock()

	return lines, nil
}

// packTypesSection extracts and formats type definitions within budget.
func (a *Assembler) packTypesSection(ctx context.Context, symbols []*ScoredSymbol, budget int) (string, int) {
	var builder strings.Builder
	tokensUsed := 0
	seen := make(map[string]bool)

	for _, scored := range symbols {
		if err := ctx.Err(); err != nil {
			break
		}

		if scored.Symbol == nil {
			continue
		}

		// Only include type-related symbols
		kind := scored.Symbol.Kind
		if kind != ast.SymbolKindStruct && kind != ast.SymbolKindInterface &&
			kind != ast.SymbolKindType && kind != ast.SymbolKindClass {
			continue
		}

		// Skip duplicates
		if seen[scored.Symbol.ID] {
			continue
		}
		seen[scored.Symbol.ID] = true

		section := a.formatTypeSymbol(scored.Symbol)
		sectionTokens := estimateTokens(section)

		if tokensUsed+sectionTokens > budget {
			break
		}

		builder.WriteString(section)
		builder.WriteString("\n")
		tokensUsed += sectionTokens
	}

	return builder.String(), tokensUsed
}

// formatTypeSymbol formats a type symbol as markdown with full definition.
//
// Thread Safety: This method is safe for concurrent use.
func (a *Assembler) formatTypeSymbol(sym *ast.Symbol) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("// From %s (lines %d-%d)\n", sym.FilePath, sym.StartLine, sym.EndLine))

	// Try to read the actual type definition from source
	sourceCode, err := a.readSourceLines(sym.FilePath, sym.StartLine, sym.EndLine)
	if err == nil && sourceCode != "" {
		builder.WriteString(sourceCode)
	} else {
		// Fallback to signature-only
		if sym.Signature != "" {
			builder.WriteString(sym.Signature)
		} else {
			builder.WriteString(fmt.Sprintf("type %s %s { /* ... */ }", sym.Name, sym.Kind.String()))
		}
	}

	builder.WriteString("\n")

	return builder.String()
}

// packLibraryDocs fetches and formats library documentation within budget.
func (a *Assembler) packLibraryDocs(ctx context.Context, query string, budget int) (string, int, []string) {
	if a.libDocs == nil {
		return "", 0, nil
	}

	docs, err := a.libDocs.Search(ctx, query, 10)
	if err != nil {
		return "", 0, nil // Graceful degradation
	}

	var builder strings.Builder
	var included []string
	tokensUsed := 0

	for _, doc := range docs {
		if err := ctx.Err(); err != nil {
			break
		}

		section := formatLibraryDoc(doc)
		sectionTokens := estimateTokens(section)

		if tokensUsed+sectionTokens > budget {
			break
		}

		builder.WriteString(section)
		builder.WriteString("\n")
		tokensUsed += sectionTokens
		included = append(included, doc.DocID)
	}

	return builder.String(), tokensUsed, included
}

// formatLibraryDoc formats a library doc entry as markdown.
func formatLibraryDoc(doc LibraryDoc) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("### %s (%s)\n", doc.SymbolPath, doc.Library))

	if doc.DocContent != "" {
		builder.WriteString(doc.DocContent)
		builder.WriteString("\n")
	}

	if doc.Signature != "" {
		builder.WriteString(fmt.Sprintf("`%s`\n", doc.Signature))
	}

	if doc.Example != "" {
		builder.WriteString("Example:\n```\n")
		builder.WriteString(doc.Example)
		builder.WriteString("\n```\n")
	}

	return builder.String()
}
