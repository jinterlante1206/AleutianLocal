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
	"hash/fnv"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
)

// Default configuration for fingerprinting.
const (
	// DefaultNumHashes is the default number of MinHash signatures.
	DefaultNumHashes = 128

	// DefaultShingleSize is the default size of shingles for MinHash.
	DefaultShingleSize = 3

	// maxUint64 is the maximum uint64 value for MinHash.
	maxUint64 = ^uint64(0)
)

// FingerprintOptions configures fingerprint computation.
type FingerprintOptions struct {
	// NumHashes is the number of MinHash signatures to compute.
	NumHashes int

	// ShingleSize is the size of k-shingles for set representation.
	ShingleSize int

	// IncludeCallPattern determines whether to include call patterns.
	IncludeCallPattern bool
}

// DefaultFingerprintOptions returns sensible defaults.
func DefaultFingerprintOptions() FingerprintOptions {
	return FingerprintOptions{
		NumHashes:          DefaultNumHashes,
		ShingleSize:        DefaultShingleSize,
		IncludeCallPattern: true,
	}
}

// FingerprintOption is a functional option for fingerprint configuration.
type FingerprintOption func(*FingerprintOptions)

// WithNumHashes sets the number of MinHash signatures.
func WithNumHashes(n int) FingerprintOption {
	return func(o *FingerprintOptions) {
		if n > 0 {
			o.NumHashes = n
		}
	}
}

// WithShingleSize sets the shingle size.
func WithShingleSize(s int) FingerprintOption {
	return func(o *FingerprintOptions) {
		if s > 0 {
			o.ShingleSize = s
		}
	}
}

// FingerprintBuilder builds AST fingerprints from symbols.
//
// Description:
//
//	Extracts structural features from code symbols to enable similarity
//	comparison. Features include control flow patterns, call patterns,
//	and signature characteristics.
//
// Thread Safety:
//
//	This type is safe for concurrent use. All methods perform read-only
//	operations on the graph.
type FingerprintBuilder struct {
	graph *graph.Graph
}

// NewFingerprintBuilder creates a new FingerprintBuilder.
//
// Description:
//
//	Creates a builder that can compute fingerprints for symbols.
//	The graph is used to extract call patterns.
//
// Inputs:
//
//	g - The code graph. May be nil for standalone fingerprinting.
//
// Example:
//
//	builder := NewFingerprintBuilder(graph)
//	fp := builder.ComputeFingerprint(symbol)
func NewFingerprintBuilder(g *graph.Graph) *FingerprintBuilder {
	return &FingerprintBuilder{
		graph: g,
	}
}

// ComputeFingerprint extracts structural features from a symbol.
//
// Description:
//
//	Analyzes the symbol's signature and relationships to extract features
//	that can be used for similarity comparison. Features are language-agnostic
//	where possible.
//
// Inputs:
//
//	sym - The symbol to fingerprint. Must not be nil.
//	opts - Optional configuration.
//
// Outputs:
//
//	*ASTFingerprint - The computed fingerprint, never nil.
//
// Example:
//
//	fp := builder.ComputeFingerprint(funcSymbol)
//	fmt.Printf("Complexity: %d, Params: %d\n", fp.Complexity, fp.ParamCount)
func (b *FingerprintBuilder) ComputeFingerprint(sym *ast.Symbol, opts ...FingerprintOption) *ASTFingerprint {
	if sym == nil {
		return &ASTFingerprint{}
	}

	options := DefaultFingerprintOptions()
	for _, opt := range opts {
		opt(&options)
	}

	fp := &ASTFingerprint{
		SymbolID:    sym.ID,
		NodeTypes:   make([]string, 0),
		CallPattern: make([]string, 0),
	}

	// Extract features from signature
	fp.ParamCount, fp.ReturnCount = extractParamReturnCounts(sym.Signature)

	// Extract control flow pattern from signature and kind
	fp.ControlFlow = extractControlFlowPattern(sym)

	// Build node types based on symbol characteristics
	fp.NodeTypes = buildNodeTypes(sym)

	// Extract call patterns from graph
	if options.IncludeCallPattern && b.graph != nil {
		fp.CallPattern = b.extractCallPattern(sym.ID)
	}

	// Estimate complexity based on line count and control flow
	fp.Complexity = estimateComplexity(sym, fp.ControlFlow)

	// Compute MinHash signature
	fp.MinHash = b.computeMinHash(fp, options.NumHashes, options.ShingleSize)

	return fp
}

// ComputeFingerprintFromSignature creates a fingerprint from just a signature.
//
// Description:
//
//	Creates a fingerprint without needing a full symbol, useful for
//	comparing against signatures extracted from code.
//
// Inputs:
//
//	signature - The function signature string.
//	kind - The symbol kind (function, method, etc.).
//	language - The programming language.
//
// Outputs:
//
//	*ASTFingerprint - The computed fingerprint.
func ComputeFingerprintFromSignature(signature string, kind ast.SymbolKind, language string) *ASTFingerprint {
	fp := &ASTFingerprint{
		NodeTypes:   make([]string, 0),
		CallPattern: make([]string, 0),
	}

	fp.ParamCount, fp.ReturnCount = extractParamReturnCounts(signature)

	// Build node types from kind
	switch kind {
	case ast.SymbolKindFunction:
		fp.NodeTypes = append(fp.NodeTypes, "function")
	case ast.SymbolKindMethod:
		fp.NodeTypes = append(fp.NodeTypes, "method")
	}

	// Add type indicators
	if hasErrorReturn(signature) {
		fp.NodeTypes = append(fp.NodeTypes, "returns_error")
	}
	if hasContextParam(signature) {
		fp.NodeTypes = append(fp.NodeTypes, "takes_context")
	}

	fp.Complexity = 1 // Minimum complexity

	return fp
}

// extractCallPattern gets the call pattern for a symbol from the graph.
func (b *FingerprintBuilder) extractCallPattern(symbolID string) []string {
	if b.graph == nil {
		return nil
	}

	node, exists := b.graph.GetNode(symbolID)
	if !exists {
		return nil
	}

	// Collect unique callees and their abstracted signatures
	callees := make(map[string]bool)
	for _, edge := range node.Outgoing {
		if edge.Type == graph.EdgeTypeCalls {
			calleeNode, exists := b.graph.GetNode(edge.ToID)
			if exists && calleeNode.Symbol != nil {
				// Abstract the callee: use kind + param count
				abstract := abstractCallee(calleeNode.Symbol)
				callees[abstract] = true
			}
		}
	}

	// Convert to sorted slice for consistent fingerprint
	patterns := make([]string, 0, len(callees))
	for pattern := range callees {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)

	return patterns
}

// abstractCallee creates an abstracted representation of a callee.
func abstractCallee(sym *ast.Symbol) string {
	if sym == nil {
		return "unknown"
	}

	paramCount, returnCount := extractParamReturnCounts(sym.Signature)
	hasErr := hasErrorReturn(sym.Signature)

	parts := []string{sym.Kind.String()}

	if paramCount > 0 {
		parts = append(parts, "params")
	}
	if returnCount > 1 {
		parts = append(parts, "multi_return")
	}
	if hasErr {
		parts = append(parts, "error")
	}

	return strings.Join(parts, "_")
}

// computeMinHash computes the MinHash signature for a fingerprint.
func (b *FingerprintBuilder) computeMinHash(fp *ASTFingerprint, numHashes, shingleSize int) []uint64 {
	// Build set of shingles from all features
	shingles := buildShingleSet(fp, shingleSize)

	if len(shingles) == 0 {
		return make([]uint64, numHashes)
	}

	return MinHashSignature(shingles, numHashes)
}

// buildShingleSet creates a set of k-shingles from fingerprint features.
func buildShingleSet(fp *ASTFingerprint, k int) []string {
	// Combine all features into a single string representation
	var features []string

	// Add node types as features
	features = append(features, fp.NodeTypes...)

	// Add call patterns
	features = append(features, fp.CallPattern...)

	// Add control flow
	if fp.ControlFlow != "" {
		features = append(features, fp.ControlFlow)
	}

	// Add numeric features as strings
	features = append(features, strings.Repeat("P", fp.ParamCount))  // Param indicator
	features = append(features, strings.Repeat("R", fp.ReturnCount)) // Return indicator
	features = append(features, strings.Repeat("C", fp.Complexity))  // Complexity indicator

	// Join and create shingles
	combined := strings.Join(features, "|")
	return createShingles(combined, k)
}

// createShingles creates k-shingles from a string.
func createShingles(s string, k int) []string {
	if len(s) < k {
		return []string{s}
	}

	shingles := make([]string, 0, len(s)-k+1)
	for i := 0; i <= len(s)-k; i++ {
		shingles = append(shingles, s[i:i+k])
	}

	return shingles
}

// MinHashSignature computes the MinHash signature for a set of strings.
//
// Description:
//
//	Computes a locality-sensitive hash that can be used for efficient
//	similarity estimation. Similar sets will have similar MinHash signatures.
//
// Inputs:
//
//	set - The set of strings to hash.
//	numHashes - The number of hash functions to use.
//
// Outputs:
//
//	[]uint64 - The MinHash signature (length = numHashes).
//
// Example:
//
//	sig := MinHashSignature([]string{"a", "b", "c"}, 128)
func MinHashSignature(set []string, numHashes int) []uint64 {
	if numHashes <= 0 {
		numHashes = DefaultNumHashes
	}

	signature := make([]uint64, numHashes)
	for i := range signature {
		signature[i] = maxUint64
	}

	if len(set) == 0 {
		return signature
	}

	// For each element in the set
	for _, elem := range set {
		// Compute hashes with different seeds
		for i := 0; i < numHashes; i++ {
			h := hashWithSeed(elem, uint64(i))
			if h < signature[i] {
				signature[i] = h
			}
		}
	}

	return signature
}

// hashWithSeed computes a hash of a string with a seed.
func hashWithSeed(s string, seed uint64) uint64 {
	h := fnv.New64a()
	// Write seed first
	seedBytes := []byte{
		byte(seed >> 56), byte(seed >> 48), byte(seed >> 40), byte(seed >> 32),
		byte(seed >> 24), byte(seed >> 16), byte(seed >> 8), byte(seed),
	}
	h.Write(seedBytes)
	h.Write([]byte(s))
	return h.Sum64()
}

// JaccardSimilarity computes the estimated Jaccard similarity from MinHash signatures.
//
// Description:
//
//	Estimates the Jaccard similarity (intersection/union) of two sets from
//	their MinHash signatures. Accuracy improves with more hash functions.
//
// Inputs:
//
//	a, b - MinHash signatures (must be same length).
//
// Outputs:
//
//	float64 - Estimated Jaccard similarity (0.0 to 1.0).
//
// Example:
//
//	sim := JaccardSimilarity(sig1, sig2)
//	if sim > 0.8 {
//	    fmt.Println("Very similar!")
//	}
func JaccardSimilarity(a, b []uint64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	if len(a) != len(b) {
		// Use shorter length
		minLen := len(a)
		if len(b) < minLen {
			minLen = len(b)
		}
		a = a[:minLen]
		b = b[:minLen]
	}

	matches := 0
	for i := range a {
		if a[i] == b[i] {
			matches++
		}
	}

	return float64(matches) / float64(len(a))
}

// ComputeSimilarity computes the similarity between two fingerprints.
//
// Description:
//
//	Computes overall similarity using both MinHash (if available) and
//	direct feature comparison. Returns a score from 0.0 to 1.0.
//
// Inputs:
//
//	a, b - The fingerprints to compare.
//
// Outputs:
//
//	float64 - Similarity score (0.0 to 1.0).
//	[]string - List of matched traits explaining similarity.
func ComputeSimilarity(a, b *ASTFingerprint) (float64, []string) {
	if a == nil || b == nil {
		return 0.0, nil
	}

	var score float64
	var traits []string
	weights := 0.0

	// MinHash similarity (weight: 0.4)
	if len(a.MinHash) > 0 && len(b.MinHash) > 0 {
		mhSim := JaccardSimilarity(a.MinHash, b.MinHash)
		score += mhSim * 0.4
		weights += 0.4
		if mhSim > 0.5 {
			traits = append(traits, "structural_overlap")
		}
	}

	// Parameter count similarity (weight: 0.15)
	if a.ParamCount == b.ParamCount {
		score += 0.15
		traits = append(traits, "same_param_count")
	} else if abs(a.ParamCount-b.ParamCount) <= 1 {
		score += 0.075
	}
	weights += 0.15

	// Return count similarity (weight: 0.15)
	if a.ReturnCount == b.ReturnCount {
		score += 0.15
		traits = append(traits, "same_return_count")
	} else if abs(a.ReturnCount-b.ReturnCount) <= 1 {
		score += 0.075
	}
	weights += 0.15

	// Complexity similarity (weight: 0.15)
	complexityDiff := abs(a.Complexity - b.Complexity)
	if complexityDiff == 0 {
		score += 0.15
		traits = append(traits, "same_complexity")
	} else if complexityDiff <= 2 {
		score += 0.15 * (1.0 - float64(complexityDiff)/5.0)
	}
	weights += 0.15

	// Control flow similarity (weight: 0.15)
	if a.ControlFlow == b.ControlFlow && a.ControlFlow != "" {
		score += 0.15
		traits = append(traits, "same_control_flow")
	} else if controlFlowOverlap(a.ControlFlow, b.ControlFlow) > 0.5 {
		score += 0.075
	}
	weights += 0.15

	// Normalize by weights
	if weights > 0 {
		score = score / weights
	}

	// Clamp to [0, 1]
	if score > 1.0 {
		score = 1.0
	}
	if score < 0.0 {
		score = 0.0
	}

	return score, traits
}

// Helper functions

// extractParamReturnCounts extracts parameter and return counts from a signature.
func extractParamReturnCounts(sig string) (params, returns int) {
	if sig == "" {
		return 0, 0
	}

	// Find parameter section (between first ( and matching ))
	paramStart := strings.Index(sig, "(")
	if paramStart < 0 {
		return 0, 0
	}

	// Count parameters by counting commas + 1, ignoring nested parens
	paramSection := extractParenSection(sig[paramStart:])
	if paramSection != "" && paramSection != "()" {
		params = countParams(paramSection)
	}

	// Find return section (after the parameter section)
	afterParams := sig[paramStart+len(paramSection):]
	if strings.TrimSpace(afterParams) == "" {
		return params, 0
	}

	// Check for multiple returns (parenthesized)
	returnSection := strings.TrimSpace(afterParams)
	if strings.HasPrefix(returnSection, "(") {
		retParens := extractParenSection(returnSection)
		if retParens != "" && retParens != "()" {
			returns = countParams(retParens)
		}
	} else if returnSection != "" {
		// Single return
		returns = 1
	}

	return params, returns
}

// extractParenSection extracts the content of the first balanced parentheses.
func extractParenSection(s string) string {
	if !strings.HasPrefix(s, "(") {
		return ""
	}

	depth := 0
	for i, r := range s {
		if r == '(' {
			depth++
		} else if r == ')' {
			depth--
			if depth == 0 {
				return s[:i+1]
			}
		}
	}
	return ""
}

// countParams counts parameters by analyzing the content between parentheses.
func countParams(section string) int {
	// Remove outer parens
	section = strings.TrimPrefix(section, "(")
	section = strings.TrimSuffix(section, ")")
	section = strings.TrimSpace(section)

	if section == "" {
		return 0
	}

	// Count by commas, but respect nested structures
	count := 1
	depth := 0
	for _, r := range section {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				count++
			}
		}
	}

	return count
}

// hasErrorReturn checks if a signature returns an error.
func hasErrorReturn(sig string) bool {
	// Simple check: look for "error" in return section
	return strings.Contains(sig, "error") ||
		strings.Contains(sig, "Error") ||
		strings.Contains(sig, "Exception")
}

// hasContextParam checks if a signature takes a context parameter.
func hasContextParam(sig string) bool {
	return strings.Contains(sig, "context.Context") ||
		strings.Contains(sig, "Context") && strings.Contains(sig, "(")
}

// extractControlFlowPattern extracts an abstracted control flow pattern.
func extractControlFlowPattern(sym *ast.Symbol) string {
	if sym == nil {
		return ""
	}

	var patterns []string

	// Analyze signature for patterns
	if hasErrorReturn(sym.Signature) {
		patterns = append(patterns, "error_handling")
	}
	if hasContextParam(sym.Signature) {
		patterns = append(patterns, "context_aware")
	}

	// Analyze kind
	switch sym.Kind {
	case ast.SymbolKindMethod:
		patterns = append(patterns, "method")
	case ast.SymbolKindFunction:
		patterns = append(patterns, "function")
	}

	// Check for common patterns in name
	nameLower := strings.ToLower(sym.Name)
	if strings.HasPrefix(nameLower, "get") || strings.HasPrefix(nameLower, "fetch") {
		patterns = append(patterns, "getter")
	} else if strings.HasPrefix(nameLower, "set") || strings.HasPrefix(nameLower, "update") {
		patterns = append(patterns, "setter")
	} else if strings.HasPrefix(nameLower, "is") || strings.HasPrefix(nameLower, "has") ||
		strings.HasPrefix(nameLower, "can") || strings.HasPrefix(nameLower, "should") {
		patterns = append(patterns, "predicate")
	} else if strings.HasPrefix(nameLower, "handle") || strings.HasPrefix(nameLower, "process") {
		patterns = append(patterns, "handler")
	} else if strings.HasPrefix(nameLower, "validate") || strings.HasPrefix(nameLower, "check") {
		patterns = append(patterns, "validator")
	} else if strings.HasPrefix(nameLower, "create") || strings.HasPrefix(nameLower, "new") {
		patterns = append(patterns, "constructor")
	}

	return strings.Join(patterns, ",")
}

// buildNodeTypes builds a list of node types based on symbol characteristics.
func buildNodeTypes(sym *ast.Symbol) []string {
	if sym == nil {
		return nil
	}

	var types []string

	// Add kind
	types = append(types, sym.Kind.String())

	// Add language if available
	if sym.Language != "" {
		types = append(types, sym.Language)
	}

	// Check for receiver (method vs function)
	if sym.Receiver != "" {
		types = append(types, "has_receiver")
	}

	// Check signature characteristics
	if hasErrorReturn(sym.Signature) {
		types = append(types, "returns_error")
	}
	if hasContextParam(sym.Signature) {
		types = append(types, "takes_context")
	}

	// Check for variadic
	if strings.Contains(sym.Signature, "...") {
		types = append(types, "variadic")
	}

	return types
}

// estimateComplexity estimates cyclomatic complexity from available information.
func estimateComplexity(sym *ast.Symbol, controlFlow string) int {
	if sym == nil {
		return 1
	}

	// Base complexity from line count (rough estimate)
	lineCount := sym.EndLine - sym.StartLine + 1
	if lineCount < 1 {
		lineCount = 1
	}

	// Estimate: 1 decision point per ~5 lines on average
	complexity := 1 + (lineCount / 5)

	// Add for error handling patterns
	if strings.Contains(controlFlow, "error_handling") {
		complexity++
	}

	// Cap at reasonable maximum
	if complexity > 50 {
		complexity = 50
	}

	return complexity
}

// controlFlowOverlap computes overlap between two control flow patterns.
func controlFlowOverlap(a, b string) float64 {
	if a == "" || b == "" {
		return 0.0
	}

	partsA := strings.Split(a, ",")
	partsB := strings.Split(b, ",")

	setA := make(map[string]bool)
	for _, p := range partsA {
		setA[p] = true
	}

	matches := 0
	for _, p := range partsB {
		if setA[p] {
			matches++
		}
	}

	total := len(partsA) + len(partsB) - matches
	if total == 0 {
		return 0.0
	}

	return float64(matches) / float64(total)
}

// abs returns the absolute value of an int.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// FingerprintIndex provides efficient similarity lookups using LSH.
//
// Description:
//
//	Uses Locality-Sensitive Hashing (LSH) to enable O(n log n) approximate
//	nearest neighbor queries instead of O(n) brute force.
//
// Thread Safety:
//
//	NOT safe for concurrent modification. Safe for concurrent queries
//	after Build() is called.
type FingerprintIndex struct {
	fingerprints map[string]*ASTFingerprint
	bandSize     int
	numBands     int
	buckets      []map[uint64][]string // band -> hash -> symbolIDs
}

// NewFingerprintIndex creates a new LSH index.
//
// Description:
//
//	Creates an index that can store fingerprints and efficiently find
//	similar ones using LSH. Configure bands and band size based on
//	desired similarity threshold.
//
// Inputs:
//
//	numBands - Number of hash bands for LSH (more = more candidates, lower threshold)
//	bandSize - Size of each band (signature length / numBands)
//
// Example:
//
//	// For ~0.5 similarity threshold with 128 hashes
//	index := NewFingerprintIndex(32, 4)
func NewFingerprintIndex(numBands, bandSize int) *FingerprintIndex {
	if numBands <= 0 {
		numBands = 16
	}
	if bandSize <= 0 {
		bandSize = DefaultNumHashes / numBands
	}

	buckets := make([]map[uint64][]string, numBands)
	for i := range buckets {
		buckets[i] = make(map[uint64][]string)
	}

	return &FingerprintIndex{
		fingerprints: make(map[string]*ASTFingerprint),
		bandSize:     bandSize,
		numBands:     numBands,
		buckets:      buckets,
	}
}

// Add adds a fingerprint to the index.
func (idx *FingerprintIndex) Add(fp *ASTFingerprint) {
	if fp == nil || fp.SymbolID == "" || len(fp.MinHash) == 0 {
		return
	}

	idx.fingerprints[fp.SymbolID] = fp

	// Add to LSH buckets
	for band := 0; band < idx.numBands; band++ {
		start := band * idx.bandSize
		end := start + idx.bandSize
		if end > len(fp.MinHash) {
			end = len(fp.MinHash)
		}
		if start >= end {
			continue
		}

		bandHash := hashBand(fp.MinHash[start:end])
		idx.buckets[band][bandHash] = append(idx.buckets[band][bandHash], fp.SymbolID)
	}
}

// FindSimilar finds candidate similar fingerprints using LSH.
//
// Description:
//
//	Returns candidate symbol IDs that are likely similar based on LSH.
//	Should be followed by exact similarity computation for final ranking.
//
// Inputs:
//
//	query - The fingerprint to find similar ones for.
//	limit - Maximum number of candidates to return.
//
// Outputs:
//
//	[]string - Symbol IDs of candidate similar fingerprints.
func (idx *FingerprintIndex) FindSimilar(query *ASTFingerprint, limit int) []string {
	if query == nil || len(query.MinHash) == 0 {
		return nil
	}

	candidateSet := make(map[string]int) // symbolID -> number of bucket matches

	// Check each band
	for band := 0; band < idx.numBands; band++ {
		start := band * idx.bandSize
		end := start + idx.bandSize
		if end > len(query.MinHash) {
			end = len(query.MinHash)
		}
		if start >= end {
			continue
		}

		bandHash := hashBand(query.MinHash[start:end])
		for _, id := range idx.buckets[band][bandHash] {
			if id != query.SymbolID {
				candidateSet[id]++
			}
		}
	}

	// Sort candidates by number of bucket matches
	type candidate struct {
		id      string
		matches int
	}
	candidates := make([]candidate, 0, len(candidateSet))
	for id, matches := range candidateSet {
		candidates = append(candidates, candidate{id, matches})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].matches > candidates[j].matches
	})

	// Return top candidates
	if limit <= 0 || limit > len(candidates) {
		limit = len(candidates)
	}

	result := make([]string, limit)
	for i := 0; i < limit; i++ {
		result[i] = candidates[i].id
	}

	return result
}

// Get retrieves a fingerprint by symbol ID.
func (idx *FingerprintIndex) Get(symbolID string) (*ASTFingerprint, bool) {
	fp, exists := idx.fingerprints[symbolID]
	return fp, exists
}

// Size returns the number of fingerprints in the index.
func (idx *FingerprintIndex) Size() int {
	return len(idx.fingerprints)
}

// hashBand computes a hash for a band of MinHash values.
func hashBand(band []uint64) uint64 {
	h := fnv.New64a()
	for _, v := range band {
		bytes := []byte{
			byte(v >> 56), byte(v >> 48), byte(v >> 40), byte(v >> 32),
			byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v),
		}
		h.Write(bytes)
	}
	return h.Sum64()
}

// SimilarityThreshold estimates the similarity threshold for given LSH parameters.
//
// Description:
//
//	Returns the approximate similarity threshold where items have ~50%
//	probability of being candidates using the given LSH parameters.
//
// Inputs:
//
//	numBands - Number of hash bands.
//	bandSize - Size of each band.
//
// Outputs:
//
//	float64 - Approximate similarity threshold.
func SimilarityThreshold(numBands, bandSize int) float64 {
	// Probability of being candidate = 1 - (1 - s^bandSize)^numBands
	// At 50% probability: 0.5 = 1 - (1 - s^r)^b
	// Solving: s ≈ (1 - (1 - 0.5)^(1/b))^(1/r)

	b := float64(numBands)
	r := float64(bandSize)

	return math.Pow(1-math.Pow(0.5, 1/b), 1/r)
}

// Regex for extracting types from signatures
var typeExtractor = regexp.MustCompile(`[A-Z][a-zA-Z0-9_]*`)
