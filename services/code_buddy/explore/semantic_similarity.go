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
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

// SimilarityMethod specifies the similarity algorithm to use.
type SimilarityMethod string

const (
	// SimilarityStructural uses AST fingerprinting with MinHash LSH.
	// Fast, works offline, focuses on code structure.
	SimilarityStructural SimilarityMethod = "structural"

	// SimilaritySemantic uses embeddings for semantic similarity.
	// Finds functionally similar code even with different structure.
	// Requires embeddings service.
	SimilaritySemantic SimilarityMethod = "semantic"

	// SimilarityHybrid combines structural and semantic methods.
	// Weights: 0.6 structural + 0.4 semantic by default.
	SimilarityHybrid SimilarityMethod = "hybrid"
)

// HybridWeights configures the balance between structural and semantic.
type HybridWeights struct {
	Structural float64 // Weight for structural similarity (default 0.6)
	Semantic   float64 // Weight for semantic similarity (default 0.4)
}

// DefaultHybridWeights returns the default hybrid weighting.
func DefaultHybridWeights() HybridWeights {
	return HybridWeights{
		Structural: 0.6,
		Semantic:   0.4,
	}
}

// SemanticSimilarityEngine extends SimilarityEngine with embedding support.
//
// # Description
//
// SemanticSimilarityEngine wraps the structural SimilarityEngine and adds
// optional semantic similarity via embeddings. It supports three modes:
// structural (default), semantic, and hybrid.
//
// # Thread Safety
//
// This type is safe for concurrent queries after Build() is called.
type SemanticSimilarityEngine struct {
	// Structural similarity (delegates to base engine)
	base *SimilarityEngine

	// Embedding support
	embedClient  *EmbeddingClient
	embeddings   map[string][]float32
	embedMu      sync.RWMutex
	embedBuilt   bool
	embedEnabled bool

	// Configuration
	hybridWeights HybridWeights
	defaultMethod SimilarityMethod
}

// NewSemanticSimilarityEngine creates a new engine with optional embedding support.
//
// # Description
//
// Creates an engine that supports structural, semantic, and hybrid similarity.
// If embedClient is nil, only structural similarity is available.
//
// # Inputs
//
//   - g: Code graph. Must be frozen before Build().
//   - idx: Symbol index.
//   - embedClient: Optional embedding client. May be nil.
//
// # Example
//
//	// Without embeddings (structural only)
//	engine := NewSemanticSimilarityEngine(graph, index, nil)
//
//	// With embeddings (all methods available)
//	client := NewEmbeddingClient("http://localhost:8000")
//	engine := NewSemanticSimilarityEngine(graph, index, client)
func NewSemanticSimilarityEngine(
	g *graph.Graph,
	idx *index.SymbolIndex,
	embedClient *EmbeddingClient,
) *SemanticSimilarityEngine {
	return &SemanticSimilarityEngine{
		base:          NewSimilarityEngine(g, idx),
		embedClient:   embedClient,
		embeddings:    make(map[string][]float32),
		embedEnabled:  embedClient != nil,
		hybridWeights: DefaultHybridWeights(),
		defaultMethod: SimilarityStructural,
	}
}

// WithDefaultMethod sets the default similarity method.
func (e *SemanticSimilarityEngine) WithDefaultMethod(m SimilarityMethod) *SemanticSimilarityEngine {
	e.defaultMethod = m
	return e
}

// WithHybridWeights sets custom hybrid weights.
func (e *SemanticSimilarityEngine) WithHybridWeights(w HybridWeights) *SemanticSimilarityEngine {
	e.hybridWeights = w
	return e
}

// Build pre-computes fingerprints and optionally embeddings.
//
// # Description
//
// Builds the structural fingerprint index. If embedding client is available
// and the service is healthy, also pre-computes embeddings for all functions.
// Embedding computation failures are non-fatal - the engine falls back to
// structural similarity.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//
// # Outputs
//
//   - error: Non-nil only for structural build failures.
func (e *SemanticSimilarityEngine) Build(ctx context.Context) error {
	// Build structural index first
	if err := e.base.Build(ctx); err != nil {
		return err
	}

	// Build embeddings if client available
	if e.embedClient != nil {
		if err := e.buildEmbeddings(ctx); err != nil {
			// Non-fatal - log and continue with structural only
			e.embedEnabled = false
		} else {
			e.embedBuilt = true
		}
	}

	return nil
}

// buildEmbeddings pre-computes embeddings for all function signatures.
func (e *SemanticSimilarityEngine) buildEmbeddings(ctx context.Context) error {
	// Check service health
	if err := e.embedClient.Health(ctx); err != nil {
		return fmt.Errorf("embedding service unavailable: %w", err)
	}

	// Get all functions and methods
	functions := e.base.index.GetByKind(ast.SymbolKindFunction)
	methods := e.base.index.GetByKind(ast.SymbolKindMethod)

	allSymbols := make([]*ast.Symbol, 0, len(functions)+len(methods))
	allSymbols = append(allSymbols, functions...)
	allSymbols = append(allSymbols, methods...)

	if len(allSymbols) == 0 {
		return nil
	}

	// Batch embeddings in chunks for efficiency
	const batchSize = 50
	for i := 0; i < len(allSymbols); i += batchSize {
		if err := ctx.Err(); err != nil {
			return ErrContextCanceled
		}

		end := i + batchSize
		if end > len(allSymbols) {
			end = len(allSymbols)
		}

		batch := allSymbols[i:end]
		texts := make([]string, len(batch))
		ids := make([]string, len(batch))

		for j, sym := range batch {
			// Use signature and doc comment for embedding
			text := sym.Signature
			if sym.DocComment != "" {
				text = sym.DocComment + " " + sym.Signature
			}
			texts[j] = text
			ids[j] = sym.ID
		}

		vectors, err := e.embedClient.BatchEmbed(ctx, texts)
		if err != nil {
			return fmt.Errorf("batch embed: %w", err)
		}

		e.embedMu.Lock()
		for j, id := range ids {
			if j < len(vectors) {
				e.embeddings[id] = vectors[j]
			}
		}
		e.embedMu.Unlock()
	}

	return nil
}

// IsBuilt returns true if the engine is ready for queries.
func (e *SemanticSimilarityEngine) IsBuilt() bool {
	return e.base.IsBuilt()
}

// IsEmbeddingEnabled returns true if semantic similarity is available.
func (e *SemanticSimilarityEngine) IsEmbeddingEnabled() bool {
	return e.embedEnabled && e.embedBuilt
}

// FindSimilarCode finds code similar to a target function.
//
// # Description
//
// Finds similar code using the specified method. Falls back to structural
// if semantic is requested but unavailable.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - symbolID: ID of the target function.
//   - method: Similarity method (structural, semantic, hybrid).
//   - opts: Optional configuration.
//
// # Outputs
//
//   - *SimilarCode: Results including similarity scores and explanations.
//   - error: Non-nil on validation failure or if symbol not found.
//
// # Example
//
//	// Structural similarity
//	result, err := engine.FindSimilarCode(ctx, "pkg.Function", SimilarityStructural)
//
//	// Semantic similarity (if available)
//	result, err := engine.FindSimilarCode(ctx, "pkg.Function", SimilaritySemantic)
//
//	// Hybrid (best of both)
//	result, err := engine.FindSimilarCode(ctx, "pkg.Function", SimilarityHybrid)
func (e *SemanticSimilarityEngine) FindSimilarCode(
	ctx context.Context,
	symbolID string,
	method SimilarityMethod,
	opts ...ExploreOption,
) (*SimilarCode, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}
	if symbolID == "" {
		return nil, fmt.Errorf("%w: symbolID is empty", ErrInvalidInput)
	}
	if !e.IsBuilt() {
		return nil, ErrGraphNotReady
	}

	// Use default method if not specified
	if method == "" {
		method = e.defaultMethod
	}

	// Fall back to structural if semantic unavailable
	if (method == SimilaritySemantic || method == SimilarityHybrid) && !e.IsEmbeddingEnabled() {
		method = SimilarityStructural
	}

	switch method {
	case SimilarityStructural:
		return e.base.FindSimilarCode(ctx, symbolID, opts...)

	case SimilaritySemantic:
		return e.findSemanticSimilar(ctx, symbolID, opts...)

	case SimilarityHybrid:
		return e.findHybridSimilar(ctx, symbolID, opts...)

	default:
		return nil, fmt.Errorf("%w: unknown similarity method %s", ErrInvalidInput, method)
	}
}

// findSemanticSimilar finds similar code using embeddings only.
func (e *SemanticSimilarityEngine) findSemanticSimilar(
	ctx context.Context,
	symbolID string,
	opts ...ExploreOption,
) (*SimilarCode, error) {
	options := applyOptions(opts)
	limit := options.MaxNodes
	if limit <= 0 {
		limit = DefaultSimilarityLimit
	}

	// Get target embedding
	e.embedMu.RLock()
	targetEmbed, exists := e.embeddings[symbolID]
	e.embedMu.RUnlock()

	if !exists {
		// Try to compute on the fly
		sym, found := e.base.index.GetByID(symbolID)
		if !found {
			return nil, ErrSymbolNotFound
		}

		text := sym.Signature
		if sym.DocComment != "" {
			text = sym.DocComment + " " + sym.Signature
		}

		embed, err := e.embedClient.Embed(ctx, text)
		if err != nil {
			// Fall back to structural
			return e.base.FindSimilarCode(ctx, symbolID, opts...)
		}
		targetEmbed = embed
	}

	result := &SimilarCode{
		Query:   symbolID,
		Results: make([]SimilarResult, 0),
		Method:  string(SimilaritySemantic),
	}

	// Compare against all embeddings
	type scored struct {
		id         string
		similarity float64
	}
	scoredResults := make([]scored, 0)

	e.embedMu.RLock()
	for id, embed := range e.embeddings {
		if id == symbolID {
			continue
		}

		sim := CosineSimilarity(targetEmbed, embed)
		if sim >= MinSimilarityThreshold {
			scoredResults = append(scoredResults, scored{id, sim})
		}
	}
	e.embedMu.RUnlock()

	// Sort by similarity
	sort.Slice(scoredResults, func(i, j int) bool {
		return scoredResults[i].similarity > scoredResults[j].similarity
	})

	// Take top results
	if len(scoredResults) > limit {
		scoredResults = scoredResults[:limit]
	}

	// Build result
	for _, sr := range scoredResults {
		sym, found := e.base.index.GetByID(sr.id)
		if !found {
			continue
		}

		result.Results = append(result.Results, SimilarResult{
			ID:            sr.id,
			Similarity:    sr.similarity,
			FilePath:      sym.FilePath,
			MatchedTraits: []string{"semantic_similarity"},
			Why:           "Semantically similar code",
		})
	}

	return result, nil
}

// findHybridSimilar combines structural and semantic similarity.
func (e *SemanticSimilarityEngine) findHybridSimilar(
	ctx context.Context,
	symbolID string,
	opts ...ExploreOption,
) (*SimilarCode, error) {
	options := applyOptions(opts)
	limit := options.MaxNodes
	if limit <= 0 {
		limit = DefaultSimilarityLimit
	}

	// Get structural results
	structuralResult, err := e.base.FindSimilarCode(ctx, symbolID, WithMaxNodes(limit*2))
	if err != nil {
		return nil, err
	}

	// Get semantic results
	semanticResult, err := e.findSemanticSimilar(ctx, symbolID, WithMaxNodes(limit*2))
	if err != nil {
		// If semantic fails, just use structural
		structuralResult.Method = string(SimilarityHybrid)
		return structuralResult, nil
	}

	// Combine scores
	scoreMap := make(map[string]struct {
		structural float64
		semantic   float64
		filePath   string
		traits     []string
	})

	for _, r := range structuralResult.Results {
		entry := scoreMap[r.ID]
		entry.structural = r.Similarity
		entry.filePath = r.FilePath
		entry.traits = append(entry.traits, r.MatchedTraits...)
		scoreMap[r.ID] = entry
	}

	for _, r := range semanticResult.Results {
		entry := scoreMap[r.ID]
		entry.semantic = r.Similarity
		entry.filePath = r.FilePath
		entry.traits = append(entry.traits, r.MatchedTraits...)
		scoreMap[r.ID] = entry
	}

	// Calculate hybrid scores
	type hybridScore struct {
		id       string
		score    float64
		filePath string
		traits   []string
	}
	hybridScores := make([]hybridScore, 0, len(scoreMap))

	for id, entry := range scoreMap {
		score := e.hybridWeights.Structural*entry.structural +
			e.hybridWeights.Semantic*entry.semantic
		hybridScores = append(hybridScores, hybridScore{
			id:       id,
			score:    score,
			filePath: entry.filePath,
			traits:   entry.traits,
		})
	}

	// Sort by hybrid score
	sort.Slice(hybridScores, func(i, j int) bool {
		return hybridScores[i].score > hybridScores[j].score
	})

	// Take top results
	if len(hybridScores) > limit {
		hybridScores = hybridScores[:limit]
	}

	// Build result
	result := &SimilarCode{
		Query:   symbolID,
		Results: make([]SimilarResult, 0, len(hybridScores)),
		Method:  string(SimilarityHybrid),
	}

	for _, hs := range hybridScores {
		result.Results = append(result.Results, SimilarResult{
			ID:            hs.id,
			Similarity:    hs.score,
			FilePath:      hs.filePath,
			MatchedTraits: uniqueStrings(hs.traits),
			Why:           "Combined structural and semantic similarity",
		})
	}

	return result, nil
}

// GetEmbedding returns the pre-computed embedding for a symbol.
func (e *SemanticSimilarityEngine) GetEmbedding(symbolID string) ([]float32, bool) {
	e.embedMu.RLock()
	defer e.embedMu.RUnlock()
	embed, exists := e.embeddings[symbolID]
	return embed, exists
}

// Stats returns statistics about the engine.
func (e *SemanticSimilarityEngine) Stats() SemanticSimilarityStats {
	e.embedMu.RLock()
	embedCount := len(e.embeddings)
	e.embedMu.RUnlock()

	return SemanticSimilarityStats{
		StructuralStats:  e.base.Stats(),
		EmbeddingCount:   embedCount,
		EmbeddingEnabled: e.embedEnabled,
		EmbeddingBuilt:   e.embedBuilt,
		DefaultMethod:    string(e.defaultMethod),
	}
}

// SemanticSimilarityStats contains statistics about the engine.
type SemanticSimilarityStats struct {
	StructuralStats  SimilarityEngineStats `json:"structural_stats"`
	EmbeddingCount   int                   `json:"embedding_count"`
	EmbeddingEnabled bool                  `json:"embedding_enabled"`
	EmbeddingBuilt   bool                  `json:"embedding_built"`
	DefaultMethod    string                `json:"default_method"`
}

// uniqueStrings returns unique strings from the input slice.
func uniqueStrings(strs []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(strs))
	for _, s := range strs {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	return result
}
