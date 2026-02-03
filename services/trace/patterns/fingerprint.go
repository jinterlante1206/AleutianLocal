// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package patterns

import (
	"hash/fnv"
	"strings"
	"unicode"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
)

// FingerprintConfig configures code fingerprinting behavior.
type FingerprintConfig struct {
	// KGramSize is the size of k-grams for token hashing.
	KGramSize int

	// NumHashFuncs is the number of hash functions for MinHash.
	NumHashFuncs int

	// NormalizeIdentifiers removes identifier differences.
	NormalizeIdentifiers bool
}

// DefaultFingerprintConfig returns sensible defaults.
func DefaultFingerprintConfig() FingerprintConfig {
	return FingerprintConfig{
		KGramSize:            5,
		NumHashFuncs:         100,
		NormalizeIdentifiers: true,
	}
}

// CodeFingerprint represents a fingerprint for duplication detection.
//
// # Description
//
// CodeFingerprint captures the structural essence of code for efficient
// similarity comparison. It uses k-gram hashing for exact matching and
// MinHash signatures for locality-sensitive hashing.
//
// # Thread Safety
//
// This type is immutable after creation.
type CodeFingerprint struct {
	// SymbolID is the identifier of the source symbol.
	SymbolID string

	// FilePath is the source file path.
	FilePath string

	// LineStart is the starting line of the code.
	LineStart int

	// LineEnd is the ending line of the code.
	LineEnd int

	// TokenHashes contains k-gram hashes of normalized tokens.
	TokenHashes []uint64

	// MinHashSig is the MinHash signature for LSH.
	MinHashSig []uint64

	// ASTStructure is an abstracted AST pattern string.
	ASTStructure string

	// TokenCount is the number of tokens in the code.
	TokenCount int

	// LineCount is the number of lines in the code.
	LineCount int

	// Complexity is a cyclomatic complexity estimate.
	Complexity int
}

// Fingerprinter creates code fingerprints from symbols.
//
// # Description
//
// Fingerprinter processes code symbols to create fingerprints suitable
// for duplication detection. It normalizes code, extracts k-grams,
// and computes MinHash signatures.
//
// # Thread Safety
//
// This type is safe for concurrent use.
type Fingerprinter struct {
	config     FingerprintConfig
	hashSeeds  []uint64
	stopTokens map[string]bool
}

// NewFingerprinter creates a new fingerprinter with the given config.
//
// # Description
//
// Creates a fingerprinter with pre-computed hash seeds for MinHash.
//
// # Inputs
//
//   - config: Fingerprinting configuration.
//
// # Outputs
//
//   - *Fingerprinter: Configured fingerprinter.
func NewFingerprinter(config FingerprintConfig) *Fingerprinter {
	// Generate deterministic hash seeds
	seeds := make([]uint64, config.NumHashFuncs)
	for i := range seeds {
		seeds[i] = uint64(i*31 + 17)
	}

	return &Fingerprinter{
		config:     config,
		hashSeeds:  seeds,
		stopTokens: defaultStopTokens(),
	}
}

// defaultStopTokens returns common tokens to ignore in fingerprinting.
func defaultStopTokens() map[string]bool {
	return map[string]bool{
		// Common keywords that don't affect structure
		"public":    true,
		"private":   true,
		"protected": true,
		"static":    true,
		"final":     true,
		"const":     true,
		"var":       true,
		"let":       true,
		// Noise tokens
		"{": true,
		"}": true,
		"(": true,
		")": true,
		"[": true,
		"]": true,
		";": true,
		",": true,
	}
}

// Fingerprint creates a fingerprint for a symbol's code.
//
// # Description
//
// Extracts code from the symbol, normalizes it, and computes
// k-gram hashes and MinHash signature.
//
// # Inputs
//
//   - symbol: The symbol to fingerprint.
//   - code: The source code content for the symbol.
//
// # Outputs
//
//   - *CodeFingerprint: The computed fingerprint.
func (f *Fingerprinter) Fingerprint(symbol *ast.Symbol, code string) *CodeFingerprint {
	if symbol == nil || code == "" {
		return nil
	}

	// Normalize code
	tokens := f.tokenize(code)
	if f.config.NormalizeIdentifiers {
		tokens = f.normalizeIdentifiers(tokens)
	}

	// Compute k-gram hashes
	kgrams := f.computeKGrams(tokens, f.config.KGramSize)
	tokenHashes := make([]uint64, len(kgrams))
	for i, kg := range kgrams {
		tokenHashes[i] = hashKGram(kg)
	}

	// Compute MinHash signature
	minHashSig := f.computeMinHash(tokenHashes)

	// Compute AST structure pattern
	astStructure := f.computeASTStructure(symbol.Kind, tokens)

	// Estimate complexity
	complexity := f.estimateComplexity(tokens)

	return &CodeFingerprint{
		SymbolID:     symbol.ID,
		FilePath:     symbol.FilePath,
		LineStart:    symbol.StartLine,
		LineEnd:      symbol.EndLine,
		TokenHashes:  tokenHashes,
		MinHashSig:   minHashSig,
		ASTStructure: astStructure,
		TokenCount:   len(tokens),
		LineCount:    symbol.EndLine - symbol.StartLine + 1,
		Complexity:   complexity,
	}
}

// tokenize splits code into tokens.
func (f *Fingerprinter) tokenize(code string) []string {
	var tokens []string
	var current strings.Builder

	for _, r := range code {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				token := current.String()
				if !f.stopTokens[token] {
					tokens = append(tokens, token)
				}
				current.Reset()
			}
			// Include operator tokens
			if !unicode.IsSpace(r) && !f.stopTokens[string(r)] {
				tokens = append(tokens, string(r))
			}
		}
	}

	if current.Len() > 0 {
		token := current.String()
		if !f.stopTokens[token] {
			tokens = append(tokens, token)
		}
	}

	return tokens
}

// normalizeIdentifiers replaces identifiers with placeholders.
func (f *Fingerprinter) normalizeIdentifiers(tokens []string) []string {
	identMap := make(map[string]string)
	counter := 0
	keywords := goKeywords()

	result := make([]string, len(tokens))
	for i, token := range tokens {
		// Keep keywords as-is
		if keywords[token] {
			result[i] = token
			continue
		}

		// Keep numbers as-is
		if isNumeric(token) {
			result[i] = "NUM"
			continue
		}

		// Keep strings as placeholder
		if strings.HasPrefix(token, "\"") || strings.HasPrefix(token, "'") {
			result[i] = "STR"
			continue
		}

		// Normalize identifier
		if normalized, ok := identMap[token]; ok {
			result[i] = normalized
		} else {
			normalized := identifierPlaceholder(counter)
			identMap[token] = normalized
			counter++
			result[i] = normalized
		}
	}

	return result
}

// goKeywords returns Go language keywords.
func goKeywords() map[string]bool {
	return map[string]bool{
		"break": true, "case": true, "chan": true, "const": true,
		"continue": true, "default": true, "defer": true, "else": true,
		"fallthrough": true, "for": true, "func": true, "go": true,
		"goto": true, "if": true, "import": true, "interface": true,
		"map": true, "package": true, "range": true, "return": true,
		"select": true, "struct": true, "switch": true, "type": true,
		"var": true, "nil": true, "true": true, "false": true,
		"error": true, "string": true, "int": true, "bool": true,
		"byte": true, "float64": true, "int64": true, "uint64": true,
	}
}

// isNumeric checks if a token is a number.
func isNumeric(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) && r != '.' && r != 'e' && r != 'E' && r != '-' && r != '+' {
			return false
		}
	}
	return true
}

// identifierPlaceholder creates a normalized identifier name.
func identifierPlaceholder(index int) string {
	return "ID" + string(rune('A'+index%26))
}

// computeKGrams extracts k-grams from tokens.
func (f *Fingerprinter) computeKGrams(tokens []string, k int) []string {
	if len(tokens) < k {
		// Return single "gram" of all tokens
		return []string{strings.Join(tokens, " ")}
	}

	kgrams := make([]string, 0, len(tokens)-k+1)
	for i := 0; i <= len(tokens)-k; i++ {
		kgram := strings.Join(tokens[i:i+k], " ")
		kgrams = append(kgrams, kgram)
	}

	return kgrams
}

// hashKGram computes a hash for a k-gram string.
func hashKGram(kgram string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(kgram))
	return h.Sum64()
}

// computeMinHash computes the MinHash signature.
func (f *Fingerprinter) computeMinHash(hashes []uint64) []uint64 {
	if len(hashes) == 0 {
		return make([]uint64, f.config.NumHashFuncs)
	}

	sig := make([]uint64, f.config.NumHashFuncs)
	for i := range sig {
		sig[i] = ^uint64(0) // Max uint64
	}

	for _, h := range hashes {
		for i, seed := range f.hashSeeds {
			// Simple hash combination
			combined := h ^ (seed * 0x9e3779b97f4a7c15)
			if combined < sig[i] {
				sig[i] = combined
			}
		}
	}

	return sig
}

// computeASTStructure creates an abstracted structure pattern.
func (f *Fingerprinter) computeASTStructure(kind ast.SymbolKind, tokens []string) string {
	var structure strings.Builder
	structure.WriteString(kind.String())
	structure.WriteString(":")

	// Count structural elements
	ifCount := 0
	forCount := 0
	funcCount := 0

	for _, token := range tokens {
		switch token {
		case "if":
			ifCount++
		case "for", "range":
			forCount++
		case "func":
			funcCount++
		}
	}

	structure.WriteString("if=" + itoa(ifCount))
	structure.WriteString(",for=" + itoa(forCount))
	structure.WriteString(",fn=" + itoa(funcCount))

	return structure.String()
}

// itoa converts an int to string without importing strconv.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	if i < 0 {
		return "-" + itoa(-i)
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

// estimateComplexity estimates cyclomatic complexity.
func (f *Fingerprinter) estimateComplexity(tokens []string) int {
	complexity := 1 // Base complexity

	for _, token := range tokens {
		switch token {
		case "if", "else", "for", "case", "&&", "||":
			complexity++
		}
	}

	return complexity
}

// JaccardSimilarity computes the Jaccard similarity between two fingerprints.
//
// # Description
//
// Computes set intersection / set union of token hashes.
// Returns a value between 0.0 and 1.0.
//
// # Inputs
//
//   - other: The fingerprint to compare against.
//
// # Outputs
//
//   - float64: Similarity score (0.0 - 1.0).
func (fp *CodeFingerprint) JaccardSimilarity(other *CodeFingerprint) float64 {
	if fp == nil || other == nil {
		return 0.0
	}

	if len(fp.TokenHashes) == 0 || len(other.TokenHashes) == 0 {
		return 0.0
	}

	// Build sets
	set1 := make(map[uint64]bool)
	for _, h := range fp.TokenHashes {
		set1[h] = true
	}

	set2 := make(map[uint64]bool)
	for _, h := range other.TokenHashes {
		set2[h] = true
	}

	// Count intersection
	intersection := 0
	for h := range set1 {
		if set2[h] {
			intersection++
		}
	}

	// Union = |set1| + |set2| - intersection
	union := len(set1) + len(set2) - intersection

	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// EstimatedJaccard estimates Jaccard similarity using MinHash signatures.
//
// # Description
//
// Uses MinHash signatures for O(1) similarity estimation.
// Less accurate than exact Jaccard but much faster for large codebases.
//
// # Inputs
//
//   - other: The fingerprint to compare against.
//
// # Outputs
//
//   - float64: Estimated similarity score (0.0 - 1.0).
func (fp *CodeFingerprint) EstimatedJaccard(other *CodeFingerprint) float64 {
	if fp == nil || other == nil {
		return 0.0
	}

	if len(fp.MinHashSig) == 0 || len(other.MinHashSig) == 0 {
		return 0.0
	}

	if len(fp.MinHashSig) != len(other.MinHashSig) {
		return 0.0
	}

	matches := 0
	for i := range fp.MinHashSig {
		if fp.MinHashSig[i] == other.MinHashSig[i] {
			matches++
		}
	}

	return float64(matches) / float64(len(fp.MinHashSig))
}
