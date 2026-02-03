// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package grounding

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Package-level compiled regexes for symbol reference extraction (compiled once).
var (
	// functionRefPattern matches function references.
	// Matches: `FunctionName()`, `FunctionName(args)`, calls FunctionName, the FunctionName function
	functionRefPattern = regexp.MustCompile(
		"(?i)" + // case insensitive for keywords
			"(?:" +
			"`([A-Z][a-zA-Z0-9_]*)\\(\\)`" + // backtick function call `Foo()`
			"|" +
			"`([A-Z][a-zA-Z0-9_]*)\\([^)]*\\)`" + // backtick function with args `Foo(x, y)`
			"|" +
			"(?:calls?|invokes?|executes?)\\s+`?([A-Z][a-zA-Z0-9_]*)\\(?`?" + // calls Foo
			"|" +
			"(?:the|a)\\s+`?([A-Z][a-zA-Z0-9_]*)\\(\\)`?\\s+(?:function|method)" + // the Foo() function
			"|" +
			"(?:function|method)\\s+`?([A-Z][a-zA-Z0-9_]*)\\(?`?" + // function Foo
			")",
	)

	// typeRefPattern matches type references.
	// Matches: `TypeName` struct, type TypeName, TypeName interface, implements TypeName
	typeRefPattern = regexp.MustCompile(
		"(?i)" +
			"(?:" +
			"`([A-Z][a-zA-Z0-9_]*)`\\s+(?:struct|interface|type)" + // `Type` struct
			"|" +
			"(?:the|a)\\s+`([A-Z][a-zA-Z0-9_]*)`\\s+(?:struct|interface|type)" + // the `Type` struct
			"|" +
			"type\\s+`?([A-Z][a-zA-Z0-9_]*)`?" + // type Type
			"|" +
			"struct\\s+`?([A-Z][a-zA-Z0-9_]*)`?" + // struct Type
			"|" +
			"(?:implements|extends)\\s+`?([A-Z][a-zA-Z0-9_]*)`?" + // implements Type
			")",
	)

	// variableRefPattern matches variable/constant references.
	// Matches: `varName` variable, constant MaxRetries, the varName constant
	variableRefPattern = regexp.MustCompile(
		"(?i)" +
			"(?:" +
			"`([A-Z][a-zA-Z0-9_]*)`\\s+(?:variable|constant|const|var)" + // `Var` variable
			"|" +
			"(?:variable|constant|const|var)\\s+`?([A-Z][a-zA-Z0-9_]*)`?" + // variable Var
			"|" +
			"(?:the)\\s+`([A-Z][a-zA-Z0-9_]*)`\\s+(?:value|setting)" + // the `Value` setting
			")",
	)

	// methodRefPattern matches method references with receiver.
	// Matches: Type.Method(), (*Type).Method()
	methodRefPattern = regexp.MustCompile(
		"`?\\(?\\*?([A-Z][a-zA-Z0-9_]*)\\)?\\." +
			"([A-Z][a-zA-Z0-9_]*)\\(?\\)?`?",
	)

	// fileContextPattern extracts file associations near symbol references.
	// Matches: in file.go, at path/file.go, from file.py
	fileContextPattern = regexp.MustCompile(
		"(?:in|at|from)\\s+" +
			"`?([a-zA-Z_][a-zA-Z0-9_\\-./]*\\." +
			"(?:go|py|js|ts|jsx|tsx|java|rs|c|cpp|h|hpp|rb))`?",
	)
)

// symbolReference represents an extracted symbol reference from the response.
type symbolReference struct {
	// Name is the symbol name.
	Name string

	// Kind is the symbol kind: "function", "type", "variable", "method".
	Kind string

	// File is the associated file (if any).
	File string

	// Position is where in the response this reference was found.
	Position int
}

// PhantomSymbolChecker detects references to symbols that don't exist.
//
// This checker identifies when the LLM references functions, types, variables,
// or constants that are not present in the codebase. Unlike PhantomFileChecker
// which validates file existence, this validates symbol existence within files.
//
// Thread Safety: Safe for concurrent use (stateless after construction).
type PhantomSymbolChecker struct {
	config        *PhantomSymbolCheckerConfig
	ignoredLookup map[string]bool
}

// NewPhantomSymbolChecker creates a new phantom symbol checker.
//
// Description:
//
//	Creates a checker that detects references to non-existent symbols.
//	Uses CheckInput.KnownSymbols and EvidenceIndex.SymbolDetails for validation.
//
// Inputs:
//   - config: Configuration for the checker (nil uses defaults).
//
// Outputs:
//   - *PhantomSymbolChecker: The configured checker.
//
// Thread Safety: Safe for concurrent use.
func NewPhantomSymbolChecker(config *PhantomSymbolCheckerConfig) *PhantomSymbolChecker {
	if config == nil {
		config = DefaultPhantomSymbolCheckerConfig()
	}

	// Build lookup map for ignored symbols
	ignoredLookup := make(map[string]bool)
	for _, s := range config.IgnoredSymbols {
		ignoredLookup[s] = true
	}

	return &PhantomSymbolChecker{
		config:        config,
		ignoredLookup: ignoredLookup,
	}
}

// Name implements Checker.
func (c *PhantomSymbolChecker) Name() string {
	return "phantom_symbol_checker"
}

// Check implements Checker.
//
// Description:
//
//	Extracts symbol references from the response and validates they exist
//	in KnownSymbols or EvidenceIndex.SymbolDetails. Non-existent symbol
//	references are flagged as ViolationPhantomSymbol.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - input: The check input containing response and symbol data.
//
// Outputs:
//   - []Violation: Any violations found.
//
// Thread Safety: Safe for concurrent use.
func (c *PhantomSymbolChecker) Check(ctx context.Context, input *CheckInput) []Violation {
	if !c.config.Enabled {
		return nil
	}

	// Need symbol data to validate against
	hasKnownSymbols := input.KnownSymbols != nil && len(input.KnownSymbols) > 0
	hasSymbolDetails := input.EvidenceIndex != nil &&
		input.EvidenceIndex.SymbolDetails != nil &&
		len(input.EvidenceIndex.SymbolDetails) > 0
	hasSimpleSymbols := input.EvidenceIndex != nil &&
		input.EvidenceIndex.Symbols != nil &&
		len(input.EvidenceIndex.Symbols) > 0

	if !hasKnownSymbols && !hasSymbolDetails && !hasSimpleSymbols {
		return nil
	}

	var violations []Violation

	// Limit response size for performance
	response := input.Response
	if len(response) > 15000 {
		response = response[:15000]
	}

	// Extract symbol references from response
	refs := c.extractSymbolReferences(response)

	// Early exit if no symbol references found
	if len(refs) == 0 {
		return nil
	}

	// Limit number of references to check
	maxRefs := c.config.MaxSymbolsToCheck
	if maxRefs > 0 && len(refs) > maxRefs {
		refs = refs[:maxRefs]
	}

	// Check each reference against known symbols
	for _, ref := range refs {
		select {
		case <-ctx.Done():
			return violations
		default:
		}

		if !c.symbolExists(ref, input) {
			severity := SeverityCritical
			if ref.File == "" {
				// No file context - lower severity as it's a global check
				severity = SeverityHigh
			}

			violations = append(violations, Violation{
				Type:     ViolationPhantomSymbol,
				Severity: severity,
				Code:     "PHANTOM_SYMBOL",
				Message:  c.formatViolationMessage(ref),
				Evidence: ref.Name,
				Expected: "Symbol should exist in the project",
				Suggestion: fmt.Sprintf(
					"Verify the %s name is correct. Use code exploration tools "+
						"to discover actual symbols before referencing them.",
					ref.Kind,
				),
				LocationOffset: ref.Position,
			})

			// Record metric
			RecordPhantomSymbol(ctx, ref.Kind, ref.File != "")
		}
	}

	return violations
}

// extractSymbolReferences extracts symbol references from the response.
//
// Description:
//
//	Uses multiple regex patterns to find symbol references in different
//	contexts (functions, types, variables, methods). Deduplicates results.
//
// Inputs:
//   - response: The LLM response text.
//
// Outputs:
//   - []symbolReference: Unique symbol references found.
func (c *PhantomSymbolChecker) extractSymbolReferences(response string) []symbolReference {
	seen := make(map[string]bool)
	var refs []symbolReference

	addRef := func(name, kind string, pos int, file string) {
		// Skip short symbols
		if len(name) < c.config.MinSymbolLength {
			return
		}

		// Skip ignored symbols
		if c.ignoredLookup[name] {
			return
		}

		// Skip common Go keywords that might be misdetected
		if isGoKeyword(name) {
			return
		}

		// Deduplicate by name+kind
		key := name + ":" + kind
		if seen[key] {
			return
		}
		seen[key] = true

		refs = append(refs, symbolReference{
			Name:     name,
			Kind:     kind,
			File:     file,
			Position: pos,
		})
	}

	// Extract function references
	matches := functionRefPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		name := c.extractNameFromMatch(response, match, 2, 4, 6, 8, 10)
		if name != "" {
			file := c.findNearbyFileContext(response, match[0])
			addRef(name, "function", match[0], file)
		}
	}

	// Extract type references
	matches = typeRefPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		name := c.extractNameFromMatch(response, match, 2, 4, 6, 8, 10)
		if name != "" {
			file := c.findNearbyFileContext(response, match[0])
			addRef(name, "type", match[0], file)
		}
	}

	// Extract variable/constant references
	matches = variableRefPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		name := c.extractNameFromMatch(response, match, 2, 4, 6)
		if name != "" {
			file := c.findNearbyFileContext(response, match[0])
			addRef(name, "variable", match[0], file)
		}
	}

	// Extract method references (Type.Method)
	matches = methodRefPattern.FindAllStringSubmatchIndex(response, -1)
	for _, match := range matches {
		if match[2] >= 0 && match[3] > match[2] && match[4] >= 0 && match[5] > match[4] {
			typeName := response[match[2]:match[3]]
			methodName := response[match[4]:match[5]]
			file := c.findNearbyFileContext(response, match[0])
			// Add both type and method
			addRef(typeName, "type", match[0], file)
			addRef(methodName, "method", match[0], file)
		}
	}

	return refs
}

// extractNameFromMatch extracts a name from regex match indices.
func (c *PhantomSymbolChecker) extractNameFromMatch(response string, match []int, groups ...int) string {
	for _, g := range groups {
		startIdx := g
		endIdx := g + 1
		if startIdx < len(match) && endIdx < len(match) &&
			match[startIdx] >= 0 && match[endIdx] > match[startIdx] {
			return response[match[startIdx]:match[endIdx]]
		}
	}
	return ""
}

// findNearbyFileContext looks for file associations near a symbol reference.
func (c *PhantomSymbolChecker) findNearbyFileContext(response string, position int) string {
	// Look at text around the position (100 chars before and after)
	start := position - 100
	if start < 0 {
		start = 0
	}
	end := position + 100
	if end > len(response) {
		end = len(response)
	}

	context := response[start:end]
	matches := fileContextPattern.FindStringSubmatch(context)
	if len(matches) >= 2 {
		return normalizeFilePath(matches[1])
	}
	return ""
}

// symbolExists checks if a symbol reference exists in the evidence.
func (c *PhantomSymbolChecker) symbolExists(ref symbolReference, input *CheckInput) bool {
	name := ref.Name

	// Check SymbolDetails if available (most detailed check)
	if input.EvidenceIndex != nil && input.EvidenceIndex.SymbolDetails != nil {
		if infos, ok := input.EvidenceIndex.SymbolDetails[name]; ok && len(infos) > 0 {
			// If file context provided, verify symbol is in that file
			if ref.File != "" && c.config.RequireFileAssociation {
				for _, info := range infos {
					if c.fileMatches(ref.File, info.File) {
						return true
					}
				}
				// Symbol exists but not in the claimed file
				return false
			}
			// Symbol exists (no file constraint)
			return true
		}
	}

	// Check simple Symbols map in EvidenceIndex
	if input.EvidenceIndex != nil && input.EvidenceIndex.Symbols != nil {
		if input.EvidenceIndex.Symbols[name] {
			return true
		}
	}

	// Check KnownSymbols
	if input.KnownSymbols != nil {
		if input.KnownSymbols[name] {
			return true
		}
	}

	return false
}

// fileMatches checks if two file paths refer to the same file.
func (c *PhantomSymbolChecker) fileMatches(ref, known string) bool {
	// Normalize both paths
	ref = normalizeFilePath(ref)
	known = normalizeFilePath(known)

	// Exact match
	if ref == known {
		return true
	}

	// Check if ref is a suffix of known (partial path match)
	if strings.HasSuffix(known, "/"+ref) {
		return true
	}

	// Check basename match
	if filepath.Base(ref) == filepath.Base(known) {
		return true
	}

	return false
}

// formatViolationMessage creates a human-readable violation message.
func (c *PhantomSymbolChecker) formatViolationMessage(ref symbolReference) string {
	if ref.File != "" {
		return fmt.Sprintf("Reference to non-existent %s '%s' in %s",
			ref.Kind, ref.Name, ref.File)
	}
	return fmt.Sprintf("Reference to non-existent %s '%s'",
		ref.Kind, ref.Name)
}

// normalizeFilePath normalizes a file path for comparison.
func normalizeFilePath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "./")
	path = filepath.ToSlash(path)
	return path
}

// isGoKeyword returns true if the name is a Go keyword.
func isGoKeyword(name string) bool {
	keywords := map[string]bool{
		"break": true, "case": true, "chan": true, "const": true,
		"continue": true, "default": true, "defer": true, "else": true,
		"fallthrough": true, "for": true, "func": true, "go": true,
		"goto": true, "if": true, "import": true, "interface": true,
		"map": true, "package": true, "range": true, "return": true,
		"select": true, "struct": true, "switch": true, "type": true,
		"var": true,
	}
	return keywords[strings.ToLower(name)]
}
