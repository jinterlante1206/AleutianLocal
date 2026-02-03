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
	"fmt"
	"sort"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// Default configuration for similarity search.
const (
	// DefaultSimilarityLimit is the default number of similar results to return.
	DefaultSimilarityLimit = 10

	// DefaultLSHBands is the default number of LSH bands.
	DefaultLSHBands = 16

	// DefaultLSHBandSize is the default size of each LSH band.
	DefaultLSHBandSize = 8

	// MinSimilarityThreshold is the minimum similarity score to include in results.
	MinSimilarityThreshold = 0.3
)

// SimilarityEngine finds similar code using fingerprint-based comparison.
//
// Description:
//
//	Uses AST fingerprinting with MinHash LSH to efficiently find structurally
//	similar code. Pre-computes fingerprints during initialization for O(n log n)
//	similarity queries.
//
// Thread Safety:
//
//	This type is safe for concurrent queries after Build() is called.
//	Build() itself is NOT safe for concurrent use.
type SimilarityEngine struct {
	graph        *graph.Graph
	index        *index.SymbolIndex
	fpBuilder    *FingerprintBuilder
	fpIndex      *FingerprintIndex
	fingerprints map[string]*ASTFingerprint
	built        bool
}

// NewSimilarityEngine creates a new SimilarityEngine.
//
// Description:
//
//	Creates an engine that can find similar code. Call Build() to
//	pre-compute fingerprints before querying.
//
// Inputs:
//
//	g - The code graph. Must be frozen.
//	idx - The symbol index.
//
// Example:
//
//	engine := NewSimilarityEngine(graph, index)
//	if err := engine.Build(ctx); err != nil {
//	    return err
//	}
//	similar, err := engine.FindSimilarCode(ctx, "pkg.Function", 5)
func NewSimilarityEngine(g *graph.Graph, idx *index.SymbolIndex) *SimilarityEngine {
	return &SimilarityEngine{
		graph:        g,
		index:        idx,
		fpBuilder:    NewFingerprintBuilder(g),
		fpIndex:      NewFingerprintIndex(DefaultLSHBands, DefaultLSHBandSize),
		fingerprints: make(map[string]*ASTFingerprint),
	}
}

// Build pre-computes fingerprints for all function symbols.
//
// Description:
//
//	Iterates through all function and method symbols in the index,
//	computes their fingerprints, and indexes them for efficient similarity
//	queries. This is a one-time operation that should be called before
//	any FindSimilarCode queries.
//
// Inputs:
//
//	ctx - Context for cancellation.
//
// Outputs:
//
//	error - Non-nil if context is cancelled or graph is not ready.
//
// Performance:
//
//	O(n) where n is the number of functions/methods.
//	Typical: ~1ms per 100 functions.
func (e *SimilarityEngine) Build(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidInput
	}
	if !e.graph.IsFrozen() {
		return ErrGraphNotReady
	}

	// Get all functions and methods
	functions := e.index.GetByKind(ast.SymbolKindFunction)
	methods := e.index.GetByKind(ast.SymbolKindMethod)

	allSymbols := make([]*ast.Symbol, 0, len(functions)+len(methods))
	allSymbols = append(allSymbols, functions...)
	allSymbols = append(allSymbols, methods...)

	// Compute fingerprints
	checkInterval := 100
	for i, sym := range allSymbols {
		if i%checkInterval == 0 {
			if err := ctx.Err(); err != nil {
				return ErrContextCanceled
			}
		}

		fp := e.fpBuilder.ComputeFingerprint(sym)
		e.fingerprints[sym.ID] = fp
		e.fpIndex.Add(fp)
	}

	e.built = true
	return nil
}

// IsBuilt returns true if the engine has been built.
func (e *SimilarityEngine) IsBuilt() bool {
	return e.built
}

// FindSimilarCode finds code similar to a target function.
//
// Description:
//
//	Uses LSH to find candidate similar functions, then computes exact
//	similarity scores and returns the top matches with explanations.
//
// Inputs:
//
//	ctx - Context for cancellation. Must not be nil.
//	symbolID - ID of the target function to find similar code for.
//	opts - Optional configuration (limit via WithMaxNodes).
//
// Outputs:
//
//	*SimilarCode - Results including matched traits explaining similarity.
//	error - Non-nil on validation failure or if symbol not found.
//
// Errors:
//
//	ErrInvalidInput - Context is nil or empty symbolID
//	ErrSymbolNotFound - Symbol not found in index
//	ErrGraphNotReady - Engine not built
//
// Example:
//
//	result, err := engine.FindSimilarCode(ctx, "pkg/handlers.HandleRequest")
//	for _, match := range result.Results {
//	    fmt.Printf("%s: %.2f similar (%v)\n", match.ID, match.Similarity, match.MatchedTraits)
//	}
func (e *SimilarityEngine) FindSimilarCode(ctx context.Context, symbolID string, opts ...ExploreOption) (*SimilarCode, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}
	if symbolID == "" {
		return nil, fmt.Errorf("%w: symbolID is empty", ErrInvalidInput)
	}
	if !e.built {
		return nil, ErrGraphNotReady
	}

	options := applyOptions(opts)
	limit := options.MaxNodes
	if limit <= 0 {
		limit = DefaultSimilarityLimit
	}

	// Get target fingerprint
	targetFP, exists := e.fingerprints[symbolID]
	if !exists {
		// Try to compute it on the fly
		sym, found := e.index.GetByID(symbolID)
		if !found {
			return nil, ErrSymbolNotFound
		}
		targetFP = e.fpBuilder.ComputeFingerprint(sym)
	}

	result := &SimilarCode{
		Query:   symbolID,
		Results: make([]SimilarResult, 0),
		Method:  "structural",
	}

	// Get candidates from LSH index
	candidateIDs := e.fpIndex.FindSimilar(targetFP, limit*3) // Get more candidates for filtering

	// Compute exact similarity for candidates
	type scored struct {
		id         string
		similarity float64
		traits     []string
	}
	scoredResults := make([]scored, 0, len(candidateIDs))

	for _, candID := range candidateIDs {
		if err := ctx.Err(); err != nil {
			return nil, ErrContextCanceled
		}

		candFP, exists := e.fingerprints[candID]
		if !exists {
			continue
		}

		sim, traits := ComputeSimilarity(targetFP, candFP)
		if sim >= MinSimilarityThreshold {
			scoredResults = append(scoredResults, scored{candID, sim, traits})
		}
	}

	// If LSH didn't return enough candidates, fall back to brute force for small graphs
	if len(scoredResults) < limit && e.fpIndex.Size() < 1000 {
		for id, fp := range e.fingerprints {
			if id == symbolID {
				continue
			}
			// Skip if already scored
			alreadyScored := false
			for _, sr := range scoredResults {
				if sr.id == id {
					alreadyScored = true
					break
				}
			}
			if alreadyScored {
				continue
			}

			sim, traits := ComputeSimilarity(targetFP, fp)
			if sim >= MinSimilarityThreshold {
				scoredResults = append(scoredResults, scored{id, sim, traits})
			}
		}
	}

	// Sort by similarity (descending)
	sort.Slice(scoredResults, func(i, j int) bool {
		return scoredResults[i].similarity > scoredResults[j].similarity
	})

	// Take top results
	if len(scoredResults) > limit {
		scoredResults = scoredResults[:limit]
	}

	// Build result
	for _, sr := range scoredResults {
		sym, found := e.index.GetByID(sr.id)
		if !found {
			continue
		}

		similarResult := SimilarResult{
			ID:            sr.id,
			Similarity:    sr.similarity,
			FilePath:      sym.FilePath,
			MatchedTraits: sr.traits,
			Why:           buildSimilarityExplanation(sr.traits),
		}

		if options.IncludeCode {
			// Code would be fetched from source files
			similarResult.Code = ""
		}

		result.Results = append(result.Results, similarResult)
	}

	return result, nil
}

// FindSimilarBySignature finds code similar to a given signature.
//
// Description:
//
//	Allows searching for similar code without having a specific symbol ID,
//	useful for finding implementations that match a desired signature pattern.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	signature - The function signature to search for (e.g., "func(ctx context.Context) error").
//	kind - The symbol kind (function or method).
//	limit - Maximum number of results.
//
// Outputs:
//
//	*SimilarCode - Results matching the signature pattern.
//	error - Non-nil on validation failure.
func (e *SimilarityEngine) FindSimilarBySignature(ctx context.Context, signature string, kind ast.SymbolKind, limit int) (*SimilarCode, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}
	if signature == "" {
		return nil, fmt.Errorf("%w: signature is empty", ErrInvalidInput)
	}
	if !e.built {
		return nil, ErrGraphNotReady
	}

	if limit <= 0 {
		limit = DefaultSimilarityLimit
	}

	// Create fingerprint from signature
	targetFP := ComputeFingerprintFromSignature(signature, kind, "go")
	targetFP.MinHash = MinHashSignature(buildShingleSet(targetFP, DefaultShingleSize), DefaultNumHashes)

	result := &SimilarCode{
		Query:   signature,
		Results: make([]SimilarResult, 0),
		Method:  "structural",
	}

	// Get candidates from LSH index
	candidateIDs := e.fpIndex.FindSimilar(targetFP, limit*3)

	// Compute exact similarity
	type scored struct {
		id         string
		similarity float64
		traits     []string
	}
	scoredResults := make([]scored, 0, len(candidateIDs))

	for _, candID := range candidateIDs {
		if err := ctx.Err(); err != nil {
			return nil, ErrContextCanceled
		}

		candFP, exists := e.fingerprints[candID]
		if !exists {
			continue
		}

		sim, traits := ComputeSimilarity(targetFP, candFP)
		if sim >= MinSimilarityThreshold {
			scoredResults = append(scoredResults, scored{candID, sim, traits})
		}
	}

	// Sort and limit
	sort.Slice(scoredResults, func(i, j int) bool {
		return scoredResults[i].similarity > scoredResults[j].similarity
	})

	if len(scoredResults) > limit {
		scoredResults = scoredResults[:limit]
	}

	// Build result
	for _, sr := range scoredResults {
		sym, found := e.index.GetByID(sr.id)
		if !found {
			continue
		}

		result.Results = append(result.Results, SimilarResult{
			ID:            sr.id,
			Similarity:    sr.similarity,
			FilePath:      sym.FilePath,
			MatchedTraits: sr.traits,
			Why:           buildSimilarityExplanation(sr.traits),
		})
	}

	return result, nil
}

// GetFingerprint returns the fingerprint for a symbol.
//
// Description:
//
//	Returns the pre-computed fingerprint for a symbol, or computes
//	it on demand if not in the index.
//
// Inputs:
//
//	symbolID - The symbol ID.
//
// Outputs:
//
//	*ASTFingerprint - The fingerprint, nil if symbol not found.
//	bool - True if fingerprint was found or computed.
func (e *SimilarityEngine) GetFingerprint(symbolID string) (*ASTFingerprint, bool) {
	if fp, exists := e.fingerprints[symbolID]; exists {
		return fp, true
	}

	sym, found := e.index.GetByID(symbolID)
	if !found {
		return nil, false
	}

	fp := e.fpBuilder.ComputeFingerprint(sym)
	return fp, true
}

// Stats returns statistics about the similarity engine.
func (e *SimilarityEngine) Stats() SimilarityEngineStats {
	return SimilarityEngineStats{
		TotalFingerprints: len(e.fingerprints),
		IndexSize:         e.fpIndex.Size(),
		Built:             e.built,
	}
}

// SimilarityEngineStats contains statistics about the engine.
type SimilarityEngineStats struct {
	TotalFingerprints int  `json:"total_fingerprints"`
	IndexSize         int  `json:"index_size"`
	Built             bool `json:"built"`
}

// buildSimilarityExplanation creates a human-readable explanation of similarity.
func buildSimilarityExplanation(traits []string) string {
	if len(traits) == 0 {
		return "Similar overall structure"
	}

	explanations := make([]string, 0, len(traits))
	for _, trait := range traits {
		switch trait {
		case "structural_overlap":
			explanations = append(explanations, "similar code structure")
		case "same_param_count":
			explanations = append(explanations, "same number of parameters")
		case "same_return_count":
			explanations = append(explanations, "same number of return values")
		case "same_complexity":
			explanations = append(explanations, "similar complexity level")
		case "same_control_flow":
			explanations = append(explanations, "similar control flow patterns")
		default:
			explanations = append(explanations, trait)
		}
	}

	if len(explanations) == 1 {
		return explanations[0]
	}

	return fmt.Sprintf("%s and %s", explanations[0], explanations[len(explanations)-1])
}

// FindFunctionsLike finds functions that match specific characteristics.
//
// Description:
//
//	Searches for functions matching specified criteria like parameter count,
//	return types, or naming patterns. More flexible than signature-based search.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	criteria - Search criteria.
//
// Outputs:
//
//	[]SimilarResult - Matching functions.
//	error - Non-nil on validation failure.
func (e *SimilarityEngine) FindFunctionsLike(ctx context.Context, criteria FunctionCriteria) ([]SimilarResult, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}
	if !e.built {
		return nil, ErrGraphNotReady
	}

	var results []SimilarResult

	for id, fp := range e.fingerprints {
		if err := ctx.Err(); err != nil {
			return nil, ErrContextCanceled
		}

		if matchesCriteria(fp, criteria) {
			sym, found := e.index.GetByID(id)
			if !found {
				continue
			}

			results = append(results, SimilarResult{
				ID:       id,
				FilePath: sym.FilePath,
				Why:      "Matches criteria",
			})

			if criteria.Limit > 0 && len(results) >= criteria.Limit {
				break
			}
		}
	}

	return results, nil
}

// FunctionCriteria specifies criteria for function search.
type FunctionCriteria struct {
	// MinParams is the minimum parameter count (-1 to ignore).
	MinParams int

	// MaxParams is the maximum parameter count (-1 to ignore).
	MaxParams int

	// MinReturns is the minimum return count (-1 to ignore).
	MinReturns int

	// MaxReturns is the maximum return count (-1 to ignore).
	MaxReturns int

	// MinComplexity is the minimum complexity (-1 to ignore).
	MinComplexity int

	// MaxComplexity is the maximum complexity (-1 to ignore).
	MaxComplexity int

	// HasErrorReturn requires the function to return an error.
	HasErrorReturn *bool

	// HasContextParam requires the function to take a context parameter.
	HasContextParam *bool

	// Limit is the maximum number of results (0 for unlimited).
	Limit int
}

// matchesCriteria checks if a fingerprint matches the criteria.
func matchesCriteria(fp *ASTFingerprint, c FunctionCriteria) bool {
	if c.MinParams >= 0 && fp.ParamCount < c.MinParams {
		return false
	}
	if c.MaxParams >= 0 && fp.ParamCount > c.MaxParams {
		return false
	}
	if c.MinReturns >= 0 && fp.ReturnCount < c.MinReturns {
		return false
	}
	if c.MaxReturns >= 0 && fp.ReturnCount > c.MaxReturns {
		return false
	}
	if c.MinComplexity >= 0 && fp.Complexity < c.MinComplexity {
		return false
	}
	if c.MaxComplexity >= 0 && fp.Complexity > c.MaxComplexity {
		return false
	}

	// Check for error handling
	if c.HasErrorReturn != nil {
		hasError := containsNodeType(fp.NodeTypes, "returns_error")
		if *c.HasErrorReturn != hasError {
			return false
		}
	}

	// Check for context param
	if c.HasContextParam != nil {
		hasContext := containsNodeType(fp.NodeTypes, "takes_context")
		if *c.HasContextParam != hasContext {
			return false
		}
	}

	return true
}

// containsNodeType checks if a node type list contains a specific type.
func containsNodeType(nodeTypes []string, target string) bool {
	for _, nt := range nodeTypes {
		if nt == target {
			return true
		}
	}
	return false
}
