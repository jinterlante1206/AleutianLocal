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
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

// Default configuration for minimal context.
const (
	// DefaultTokenBudget is the default maximum token budget.
	DefaultTokenBudget = 4000

	// DefaultMaxCallees is the default maximum number of key callees to include.
	DefaultMaxCallees = 5

	// DefaultMaxTypes is the default maximum number of types to include.
	DefaultMaxTypes = 10

	// DefaultMaxInterfaces is the default maximum number of interfaces to include.
	DefaultMaxInterfaces = 3

	// tokensPerChar is an approximation of tokens per character.
	// For code, it's roughly 1 token per 4 characters.
	tokensPerChar = 0.25

	// minTokensPerBlock is the minimum tokens to estimate for any block.
	minTokensPerBlock = 10
)

// MinimalContextBuilder builds minimal context for understanding a function.
//
// Description:
//
//	Extracts the minimum amount of code needed to understand a target function,
//	including required types, implemented interfaces, and key dependencies.
//	All results respect the configured token budget.
//
// Thread Safety:
//
//	This type is safe for concurrent use. All methods perform read-only
//	operations on the graph and index.
type MinimalContextBuilder struct {
	graph *graph.Graph
	index *index.SymbolIndex
}

// NewMinimalContextBuilder creates a new MinimalContextBuilder.
//
// Description:
//
//	Creates a builder that can extract minimal context for functions.
//	The graph must be frozen before use.
//
// Inputs:
//
//	g - The code graph. Must not be nil and must be frozen.
//	idx - The symbol index. Must not be nil.
//
// Example:
//
//	builder := NewMinimalContextBuilder(graph, index)
//	ctx, err := builder.BuildMinimalContext(ctx, "pkg.FunctionName", WithTokenBudget(4000))
func NewMinimalContextBuilder(g *graph.Graph, idx *index.SymbolIndex) *MinimalContextBuilder {
	return &MinimalContextBuilder{
		graph: g,
		index: idx,
	}
}

// BuildMinimalContext extracts the minimum context needed to understand a function.
//
// Description:
//
//	Collects the target function, required types (parameters and returns),
//	implemented interfaces, and key callees (sorted by call frequency).
//	Results are trimmed to fit within the token budget, prioritizing the
//	target function and essential types over callees.
//
// Inputs:
//
//	ctx - Context for cancellation. Must not be nil.
//	symbolID - ID of the target function/method.
//	opts - Optional configuration (TokenBudget, IncludeCode).
//
// Outputs:
//
//	*MinimalContext - The extracted context, never nil on success.
//	error - Non-nil on validation failure or if symbol not found.
//
// Errors:
//
//	ErrInvalidInput - Context is nil or empty symbolID
//	ErrSymbolNotFound - Symbol not found in index
//	ErrGraphNotReady - Graph is not frozen
//	ErrContextCanceled - Context was canceled
//
// Limitations:
//
//   - Token estimates are approximate (based on character count)
//   - Does not include transitive dependencies
//   - Code content requires source access (may be empty)
//
// Example:
//
//	result, err := builder.BuildMinimalContext(ctx, "pkg/handlers.HandleRequest")
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("Total tokens: %d\n", result.TotalTokens)
func (b *MinimalContextBuilder) BuildMinimalContext(ctx context.Context, symbolID string, opts ...ExploreOption) (*MinimalContext, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}
	if symbolID == "" {
		return nil, fmt.Errorf("%w: symbolID is empty", ErrInvalidInput)
	}
	if !b.graph.IsFrozen() {
		return nil, ErrGraphNotReady
	}

	options := applyOptions(opts)
	if options.TokenBudget <= 0 {
		options.TokenBudget = DefaultTokenBudget
	}

	// Get target symbol
	targetSym, found := b.index.GetByID(symbolID)
	if !found {
		return nil, ErrSymbolNotFound
	}

	result := &MinimalContext{
		Types:      make([]CodeBlock, 0),
		Interfaces: make([]CodeBlock, 0),
		KeyCallees: make([]CodeBlock, 0),
	}

	// Build target code block
	result.Target = b.symbolToCodeBlock(targetSym, options.IncludeCode)
	result.TotalTokens = result.Target.TokenEstimate

	remainingBudget := options.TokenBudget - result.TotalTokens

	// Check context
	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	// Get required types (parameters and returns)
	types := b.findRequiredTypes(ctx, symbolID)
	for _, typeSym := range types {
		if remainingBudget <= 0 {
			break
		}
		block := b.symbolToCodeBlock(typeSym, options.IncludeCode)
		if block.TokenEstimate <= remainingBudget {
			result.Types = append(result.Types, block)
			result.TotalTokens += block.TokenEstimate
			remainingBudget -= block.TokenEstimate
		}
		if len(result.Types) >= DefaultMaxTypes {
			break
		}
	}

	// Check context
	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	// Get implemented interfaces (for methods)
	interfaces := b.findImplementedInterfaces(ctx, targetSym)
	for _, ifaceSym := range interfaces {
		if remainingBudget <= 0 {
			break
		}
		block := b.symbolToCodeBlock(ifaceSym, options.IncludeCode)
		if block.TokenEstimate <= remainingBudget {
			result.Interfaces = append(result.Interfaces, block)
			result.TotalTokens += block.TokenEstimate
			remainingBudget -= block.TokenEstimate
		}
		if len(result.Interfaces) >= DefaultMaxInterfaces {
			break
		}
	}

	// Check context
	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	// Get key callees (sorted by call frequency)
	callees := b.findKeyCallees(ctx, symbolID)
	for _, calleeSym := range callees {
		if remainingBudget <= 0 {
			break
		}
		block := b.symbolToCodeBlock(calleeSym, options.IncludeCode)
		if block.TokenEstimate <= remainingBudget {
			result.KeyCallees = append(result.KeyCallees, block)
			result.TotalTokens += block.TokenEstimate
			remainingBudget -= block.TokenEstimate
		}
		if len(result.KeyCallees) >= DefaultMaxCallees {
			break
		}
	}

	return result, nil
}

// GetContextForFunction is an alias for BuildMinimalContext.
//
// Deprecated: Use BuildMinimalContext instead.
func (b *MinimalContextBuilder) GetContextForFunction(ctx context.Context, symbolID string, opts ...ExploreOption) (*MinimalContext, error) {
	return b.BuildMinimalContext(ctx, symbolID, opts...)
}

// findRequiredTypes finds types used in function parameters and return values.
func (b *MinimalContextBuilder) findRequiredTypes(ctx context.Context, symbolID string) []*ast.Symbol {
	node, exists := b.graph.GetNode(symbolID)
	if !exists {
		return nil
	}

	typeIDs := make(map[string]bool)
	var types []*ast.Symbol

	// Look for EdgeTypeParameters and EdgeTypeReturns edges
	for _, edge := range node.Outgoing {
		if err := ctx.Err(); err != nil {
			return types
		}

		if edge.Type == graph.EdgeTypeParameters || edge.Type == graph.EdgeTypeReturns {
			if !typeIDs[edge.ToID] {
				typeIDs[edge.ToID] = true
				if typeSym, found := b.index.GetByID(edge.ToID); found {
					// Only include type definitions (struct, interface, alias)
					if isTypeSymbol(typeSym) {
						types = append(types, typeSym)
					}
				}
			}
		}
	}

	// Also check for type references in the signature
	sym, _ := b.index.GetByID(symbolID)
	if sym != nil && sym.Signature != "" {
		// Parse types from signature and look them up
		typeNames := extractTypeNamesFromSignature(sym.Signature)
		for _, typeName := range typeNames {
			if err := ctx.Err(); err != nil {
				return types
			}

			matches := b.index.GetByName(typeName)
			for _, match := range matches {
				if !typeIDs[match.ID] && isTypeSymbol(match) {
					// Prefer types from same package
					if match.Package == sym.Package {
						typeIDs[match.ID] = true
						types = append(types, match)
					}
				}
			}
		}
	}

	return types
}

// findImplementedInterfaces finds interfaces that the method's receiver type implements.
func (b *MinimalContextBuilder) findImplementedInterfaces(ctx context.Context, sym *ast.Symbol) []*ast.Symbol {
	if sym == nil || sym.Receiver == "" {
		return nil
	}

	// Get the receiver type
	receiverTypeName := cleanReceiverType(sym.Receiver)
	var interfaces []*ast.Symbol

	// Find the receiver type in the index
	typeMatches := b.index.GetByName(receiverTypeName)
	for _, typeSym := range typeMatches {
		if err := ctx.Err(); err != nil {
			return interfaces
		}

		// Look for IMPLEMENTS edges from this type
		node, exists := b.graph.GetNode(typeSym.ID)
		if !exists {
			continue
		}

		for _, edge := range node.Outgoing {
			if edge.Type == graph.EdgeTypeImplements {
				if ifaceSym, found := b.index.GetByID(edge.ToID); found {
					if ifaceSym.Kind == ast.SymbolKindInterface {
						interfaces = append(interfaces, ifaceSym)
					}
				}
			}
		}
	}

	return interfaces
}

// findKeyCallees finds the most important callees of a function.
func (b *MinimalContextBuilder) findKeyCallees(ctx context.Context, symbolID string) []*ast.Symbol {
	node, exists := b.graph.GetNode(symbolID)
	if !exists {
		return nil
	}

	// Count call frequency per callee
	callCounts := make(map[string]int)
	for _, edge := range node.Outgoing {
		if err := ctx.Err(); err != nil {
			break
		}
		if edge.Type == graph.EdgeTypeCalls {
			callCounts[edge.ToID]++
		}
	}

	// Sort by call frequency (descending)
	type calleeCount struct {
		id    string
		count int
	}
	counts := make([]calleeCount, 0, len(callCounts))
	for id, count := range callCounts {
		counts = append(counts, calleeCount{id, count})
	}
	sort.Slice(counts, func(i, j int) bool {
		return counts[i].count > counts[j].count
	})

	// Get symbols for top callees
	var callees []*ast.Symbol
	for _, cc := range counts {
		if calleeSym, found := b.index.GetByID(cc.id); found {
			// Exclude standard library and external dependencies
			if !isStdLibOrExternal(calleeSym) {
				callees = append(callees, calleeSym)
			}
		}
		if len(callees) >= DefaultMaxCallees*2 {
			break // Get more than needed, we'll trim by budget
		}
	}

	return callees
}

// symbolToCodeBlock converts a symbol to a CodeBlock.
func (b *MinimalContextBuilder) symbolToCodeBlock(sym *ast.Symbol, includeCode bool) CodeBlock {
	block := CodeBlock{
		ID:        sym.ID,
		Name:      sym.Name,
		Kind:      sym.Kind.String(),
		FilePath:  sym.FilePath,
		StartLine: sym.StartLine,
		EndLine:   sym.EndLine,
		Signature: sym.Signature,
	}

	// Estimate tokens based on line count and signature
	lineCount := sym.EndLine - sym.StartLine + 1
	if lineCount < 1 {
		lineCount = 1
	}

	// Rough estimate: ~50 chars per line, ~0.25 tokens per char
	block.TokenEstimate = int(float64(lineCount*50) * tokensPerChar)
	if block.TokenEstimate < minTokensPerBlock {
		block.TokenEstimate = minTokensPerBlock
	}

	// Add signature tokens
	if sym.Signature != "" {
		block.TokenEstimate += int(float64(len(sym.Signature)) * tokensPerChar)
	}

	if includeCode {
		// Code content would be retrieved from source files
		// For now, we leave it empty (would require file access)
		block.Code = ""
	}

	return block
}

// EstimateTokens estimates the token count for a piece of code.
//
// Description:
//
//	Provides a rough estimate of OpenAI/Anthropic token count based on
//	character count. Actual token count varies by model and content.
//
// Inputs:
//
//	code - The code string to estimate.
//
// Outputs:
//
//	int - Estimated token count.
func EstimateTokens(code string) int {
	if code == "" {
		return 0
	}
	tokens := int(float64(len(code)) * tokensPerChar)
	if tokens < minTokensPerBlock {
		return minTokensPerBlock
	}
	return tokens
}

// EstimateTokensForSymbol estimates tokens for a symbol.
func EstimateTokensForSymbol(sym *ast.Symbol) int {
	if sym == nil {
		return 0
	}

	lineCount := sym.EndLine - sym.StartLine + 1
	if lineCount < 1 {
		lineCount = 1
	}

	// Rough estimate: ~50 chars per line
	estimate := int(float64(lineCount*50) * tokensPerChar)
	if estimate < minTokensPerBlock {
		estimate = minTokensPerBlock
	}

	// Add signature
	if sym.Signature != "" {
		estimate += int(float64(len(sym.Signature)) * tokensPerChar)
	}

	return estimate
}

// isTypeSymbol returns true if the symbol is a type definition.
func isTypeSymbol(sym *ast.Symbol) bool {
	switch sym.Kind {
	case ast.SymbolKindStruct, ast.SymbolKindInterface, ast.SymbolKindType:
		return true
	default:
		return false
	}
}

// isStdLibOrExternal returns true if the symbol appears to be from stdlib or external.
func isStdLibOrExternal(sym *ast.Symbol) bool {
	if sym == nil {
		return true
	}

	// Check for common stdlib packages
	pkg := sym.Package
	if pkg == "" {
		return false
	}

	// Standard library packages typically don't have dots before the first segment
	// and are single words like "fmt", "os", "net", "http", etc.
	stdLibPrefixes := []string{
		"fmt", "os", "io", "net", "http", "strings", "bytes", "bufio",
		"context", "errors", "sync", "time", "path", "filepath", "encoding",
		"crypto", "database", "regexp", "sort", "strconv", "testing", "log",
		"math", "reflect", "runtime", "syscall", "unicode",
	}

	for _, prefix := range stdLibPrefixes {
		if pkg == prefix || strings.HasPrefix(pkg, prefix+"/") {
			return true
		}
	}

	return false
}

// cleanReceiverType extracts the type name from a receiver string.
// For example, "*http.Request" becomes "Request".
func cleanReceiverType(receiver string) string {
	// Remove pointer
	receiver = strings.TrimPrefix(receiver, "*")

	// Remove package prefix
	if idx := strings.LastIndex(receiver, "."); idx >= 0 {
		receiver = receiver[idx+1:]
	}

	return receiver
}

// extractTypeNamesFromSignature extracts type names from a function signature.
// This is a simplified parser for Go-style signatures.
func extractTypeNamesFromSignature(sig string) []string {
	if sig == "" {
		return nil
	}

	var types []string
	seen := make(map[string]bool)

	// Simple tokenization: extract words that look like type names
	// Type names typically start with uppercase (exported) or are preceded by * or []
	words := tokenizeSignature(sig)

	for _, word := range words {
		// Skip keywords and builtins
		if isGoKeyword(word) || isGoBuiltin(word) {
			continue
		}

		// Clean the word
		word = strings.TrimPrefix(word, "*")
		word = strings.TrimPrefix(word, "[]")
		word = strings.TrimPrefix(word, "map[")
		word = strings.TrimSuffix(word, "]")

		// Extract just the type name (after last dot for qualified names)
		if idx := strings.LastIndex(word, "."); idx >= 0 {
			word = word[idx+1:]
		}

		// Must be a valid identifier starting with uppercase (exported)
		if len(word) > 0 && word[0] >= 'A' && word[0] <= 'Z' && !seen[word] {
			seen[word] = true
			types = append(types, word)
		}
	}

	return types
}

// tokenizeSignature splits a signature into words/tokens.
func tokenizeSignature(sig string) []string {
	var words []string
	var current strings.Builder

	for _, r := range sig {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' {
			current.WriteRune(r)
		} else {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
		}
	}

	if current.Len() > 0 {
		words = append(words, current.String())
	}

	return words
}

// isGoKeyword returns true if the word is a Go keyword.
func isGoKeyword(word string) bool {
	keywords := map[string]bool{
		"func": true, "return": true, "var": true, "const": true,
		"type": true, "struct": true, "interface": true, "map": true,
		"chan": true, "if": true, "else": true, "for": true,
		"range": true, "switch": true, "case": true, "default": true,
		"select": true, "go": true, "defer": true, "break": true,
		"continue": true, "fallthrough": true, "goto": true, "package": true,
		"import": true,
	}
	return keywords[word]
}

// isGoBuiltin returns true if the word is a Go builtin type.
func isGoBuiltin(word string) bool {
	builtins := map[string]bool{
		"bool": true, "byte": true, "complex64": true, "complex128": true,
		"error": true, "float32": true, "float64": true, "int": true,
		"int8": true, "int16": true, "int32": true, "int64": true,
		"rune": true, "string": true, "uint": true, "uint8": true,
		"uint16": true, "uint32": true, "uint64": true, "uintptr": true,
		"any": true, "comparable": true,
	}
	return builtins[word]
}
