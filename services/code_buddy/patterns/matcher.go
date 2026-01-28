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
	"context"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

// PatternMatcher defines how to detect a specific design pattern.
//
// # Description
//
// PatternMatcher contains the logic for detecting one design pattern.
// It has two phases: structural detection (find candidates based on shape)
// and idiomatic validation (check if the implementation follows best practices).
type PatternMatcher struct {
	// Name is the pattern name (singleton, factory, etc.).
	Name PatternType

	// Description explains the pattern.
	Description string

	// Languages supported (empty = all).
	Languages []string

	// StructuralCheck finds candidates based on code structure.
	StructuralCheck func(ctx context.Context, g *graph.Graph, idx *index.SymbolIndex, scope string) []PatternCandidate

	// IdiomaticCheck validates if a candidate is idiomatically implemented.
	// Returns (isIdiomatic, warnings).
	IdiomaticCheck func(candidate PatternCandidate, idx *index.SymbolIndex) (bool, []string)
}

// Match runs the pattern matcher and returns detected patterns.
func (m *PatternMatcher) Match(
	ctx context.Context,
	g *graph.Graph,
	idx *index.SymbolIndex,
	scope string,
) []DetectedPattern {
	results := make([]DetectedPattern, 0)

	// Find structural candidates
	candidates := m.StructuralCheck(ctx, g, idx, scope)

	// Validate each candidate
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			break
		}

		// Check if idiomatic
		idiomatic := true
		var warnings []string
		if m.IdiomaticCheck != nil {
			idiomatic, warnings = m.IdiomaticCheck(candidate, idx)
		}

		// Calculate confidence
		confidence := m.calculateConfidence(candidate, idiomatic)

		results = append(results, DetectedPattern{
			Type:       m.Name,
			Location:   candidate.Location,
			Components: candidate.SymbolIDs,
			Confidence: confidence,
			Idiomatic:  idiomatic,
			Warnings:   warnings,
		})
	}

	return results
}

// calculateConfidence computes confidence based on match quality.
func (m *PatternMatcher) calculateConfidence(candidate PatternCandidate, idiomatic bool) float64 {
	if idiomatic {
		return IdiomaticMatchBase
	}
	return StructuralMatchBase
}

// SingletonMatcher detects singleton patterns.
//
// # Structural Indicators
//
// - Package-level variable of a type
// - sync.Once or mutex for initialization
// - Accessor function (GetInstance, Instance, etc.)
//
// # Idiomatic Check
//
// - MUST use sync.Once or mutex for thread-safety
// - Non-idiomatic: no synchronization mechanism
var SingletonMatcher = &PatternMatcher{
	Name:        PatternSingleton,
	Description: "Single instance pattern with thread-safe lazy initialization",
	Languages:   []string{"go"},

	StructuralCheck: func(ctx context.Context, g *graph.Graph, idx *index.SymbolIndex, scope string) []PatternCandidate {
		candidates := make([]PatternCandidate, 0)

		// Find package-level variables
		variables := idx.GetByKind(ast.SymbolKindVariable)

		for _, v := range variables {
			if ctx.Err() != nil {
				break
			}

			// Skip non-matching scope
			if scope != "" && !strings.HasPrefix(v.FilePath, scope) {
				continue
			}

			// Skip unexported (though singletons are usually private)
			// Look for associated accessor function
			accessorNames := []string{
				"Get" + v.Name,
				v.Name + "Instance",
				"Instance",
				"Get" + strings.TrimPrefix(v.Name, "default"),
			}

			for _, name := range accessorNames {
				functions := idx.GetByName(name)
				for _, fn := range functions {
					if fn.Kind == ast.SymbolKindFunction &&
						strings.HasPrefix(fn.FilePath, strings.TrimSuffix(v.FilePath, ".go")) {

						candidates = append(candidates, PatternCandidate{
							SymbolIDs: []string{v.ID, fn.ID},
							Location:  v.FilePath,
							Metadata: map[string]interface{}{
								"variable":       v.ID,
								"accessor":       fn.ID,
								"variable_name":  v.Name,
								"variable_file":  v.FilePath,
								"accessor_name":  fn.Name,
								"accessor_file":  fn.FilePath,
								"accessor_start": fn.StartLine,
								"accessor_end":   fn.EndLine,
							},
						})
					}
				}
			}
		}

		return candidates
	},

	IdiomaticCheck: func(candidate PatternCandidate, idx *index.SymbolIndex) (bool, []string) {
		warnings := make([]string, 0)

		// Check for sync.Once usage in accessor
		accessorID, ok := candidate.Metadata["accessor"].(string)
		if !ok {
			return false, []string{"Could not validate accessor function"}
		}

		sym, found := idx.GetByID(accessorID)
		if !found {
			return false, []string{"Accessor function not found"}
		}

		// Check signature for sync.Once (simplified check)
		hasSyncOnce := strings.Contains(sym.Signature, "sync.Once") ||
			strings.Contains(sym.Signature, "sync.Mutex")

		if !hasSyncOnce {
			warnings = append(warnings, "Singleton without sync.Once is not thread-safe")
			return false, warnings
		}

		return true, warnings
	},
}

// FactoryMatcher detects factory patterns.
//
// # Structural Indicators
//
// - Function named New* or Create*
// - Returns a concrete type or interface
// - May include validation or configuration
//
// # Idiomatic Check
//
// - Better: returns interface, not concrete type
// - Better: includes validation
var FactoryMatcher = &PatternMatcher{
	Name:        PatternFactory,
	Description: "Creates objects without exposing instantiation logic",
	Languages:   []string{"go"},

	StructuralCheck: func(ctx context.Context, g *graph.Graph, idx *index.SymbolIndex, scope string) []PatternCandidate {
		candidates := make([]PatternCandidate, 0)

		functions := idx.GetByKind(ast.SymbolKindFunction)

		for _, fn := range functions {
			if ctx.Err() != nil {
				break
			}

			// Skip non-matching scope
			if scope != "" && !strings.HasPrefix(fn.FilePath, scope) {
				continue
			}

			// Factory pattern: New* or Create* functions
			if strings.HasPrefix(fn.Name, "New") || strings.HasPrefix(fn.Name, "Create") {
				// Skip test files
				if strings.Contains(fn.FilePath, "_test.go") {
					continue
				}

				candidates = append(candidates, PatternCandidate{
					SymbolIDs: []string{fn.ID},
					Location:  fn.FilePath,
					Metadata: map[string]interface{}{
						"function":        fn.ID,
						"function_name":   fn.Name,
						"function_file":   fn.FilePath,
						"signature":       fn.Signature,
						"returns_pointer": strings.Contains(fn.Signature, "*"),
						"returns_error":   strings.Contains(fn.Signature, "error"),
					},
				})
			}
		}

		return candidates
	},

	IdiomaticCheck: func(candidate PatternCandidate, idx *index.SymbolIndex) (bool, []string) {
		warnings := make([]string, 0)

		sig, ok := candidate.Metadata["signature"].(string)
		if !ok {
			return false, []string{"Could not validate signature"}
		}

		returnsError, ok := candidate.Metadata["returns_error"].(bool)
		if !ok {
			returnsError = false
		}

		// Idiomatic factories return error for validation
		if !returnsError {
			warnings = append(warnings, "Factory doesn't return error - no validation possible")
		}

		// Idiomatic factories return interface (harder to detect without type info)
		// For now, just check if it's a pointer return
		if !strings.Contains(sig, "*") && !strings.Contains(sig, "interface") {
			warnings = append(warnings, "Consider returning pointer or interface for flexibility")
		}

		return len(warnings) == 0, warnings
	},
}

// BuilderMatcher detects builder patterns.
//
// # Structural Indicators
//
// - Type with method chaining (methods return same type)
// - With* or Set* methods
// - Build() method that returns final type
//
// # Idiomatic Check
//
// - Build() validates before returning
// - With* methods are chainable
var BuilderMatcher = &PatternMatcher{
	Name:        PatternBuilder,
	Description: "Constructs complex objects step by step",
	Languages:   []string{"go"},

	StructuralCheck: func(ctx context.Context, g *graph.Graph, idx *index.SymbolIndex, scope string) []PatternCandidate {
		candidates := make([]PatternCandidate, 0)

		// Find types that might be builders
		structs := idx.GetByKind(ast.SymbolKindStruct)

		for _, s := range structs {
			if ctx.Err() != nil {
				break
			}

			// Skip non-matching scope
			if scope != "" && !strings.HasPrefix(s.FilePath, scope) {
				continue
			}

			// Skip if name doesn't suggest builder
			if !strings.HasSuffix(s.Name, "Builder") && !strings.HasSuffix(s.Name, "Config") {
				continue
			}

			// Find methods on this type
			methods := findMethodsForType(idx, s.Name)

			withMethods := make([]string, 0)
			hasBuild := false
			chainable := 0

			for _, m := range methods {
				// Check for With* or Set* methods
				if strings.HasPrefix(m.Name, "With") || strings.HasPrefix(m.Name, "Set") {
					withMethods = append(withMethods, m.ID)
					// Check if chainable (returns *Builder)
					if strings.Contains(m.Signature, "*"+s.Name) {
						chainable++
					}
				}
				// Check for Build method
				if m.Name == "Build" {
					hasBuild = true
				}
			}

			// Needs at least some With methods and preferably Build
			if len(withMethods) >= 2 {
				symbolIDs := append([]string{s.ID}, withMethods...)

				candidates = append(candidates, PatternCandidate{
					SymbolIDs: symbolIDs,
					Location:  s.FilePath,
					Metadata: map[string]interface{}{
						"type":         s.ID,
						"type_name":    s.Name,
						"with_count":   len(withMethods),
						"chainable":    chainable,
						"has_build":    hasBuild,
						"with_methods": withMethods,
					},
				})
			}
		}

		return candidates
	},

	IdiomaticCheck: func(candidate PatternCandidate, idx *index.SymbolIndex) (bool, []string) {
		warnings := make([]string, 0)

		hasBuild, ok := candidate.Metadata["has_build"].(bool)
		if ok && !hasBuild {
			warnings = append(warnings, "Builder lacks Build() method - validation point missing")
		}

		chainable, ok := candidate.Metadata["chainable"].(int)
		withCount, ok2 := candidate.Metadata["with_count"].(int)
		if ok && ok2 && chainable < withCount {
			warnings = append(warnings, "Some With* methods aren't chainable (don't return *Builder)")
		}

		return len(warnings) == 0, warnings
	},
}

// OptionsMatcher detects functional options pattern.
//
// # Structural Indicators
//
// - Type alias: type Option func(*Config)
// - Multiple With* functions returning Option
// - Constructor accepting ...Option
var OptionsMatcher = &PatternMatcher{
	Name:        PatternOptions,
	Description: "Functional options for configuration",
	Languages:   []string{"go"},

	StructuralCheck: func(ctx context.Context, g *graph.Graph, idx *index.SymbolIndex, scope string) []PatternCandidate {
		candidates := make([]PatternCandidate, 0)

		// Find type aliases that look like Option
		types := idx.GetByKind(ast.SymbolKindType)

		for _, t := range types {
			if ctx.Err() != nil {
				break
			}

			// Skip non-matching scope
			if scope != "" && !strings.HasPrefix(t.FilePath, scope) {
				continue
			}

			// Look for Option type pattern
			if !strings.Contains(t.Name, "Option") && !strings.Contains(t.Name, "Opt") {
				continue
			}

			// Check if it's a function type
			if !strings.Contains(t.Signature, "func") {
				continue
			}

			// Find functions returning this type
			functions := idx.GetByKind(ast.SymbolKindFunction)
			withFuncs := make([]string, 0)

			for _, fn := range functions {
				if strings.HasPrefix(fn.Name, "With") && strings.Contains(fn.Signature, t.Name) {
					withFuncs = append(withFuncs, fn.ID)
				}
			}

			if len(withFuncs) >= 2 {
				symbolIDs := append([]string{t.ID}, withFuncs...)

				candidates = append(candidates, PatternCandidate{
					SymbolIDs: symbolIDs,
					Location:  t.FilePath,
					Metadata: map[string]interface{}{
						"option_type": t.ID,
						"option_name": t.Name,
						"with_count":  len(withFuncs),
						"with_funcs":  withFuncs,
					},
				})
			}
		}

		return candidates
	},

	IdiomaticCheck: func(candidate PatternCandidate, idx *index.SymbolIndex) (bool, []string) {
		// Options pattern is generally idiomatic if it exists
		withCount, ok := candidate.Metadata["with_count"].(int)
		if ok && withCount < 3 {
			return true, []string{"Options pattern with few options - might be over-engineering"}
		}
		return true, nil
	},
}

// MiddlewareMatcher detects middleware patterns.
//
// # Structural Indicators
//
// - Function signature: func(Handler) Handler
// - http.Handler or similar handler types
var MiddlewareMatcher = &PatternMatcher{
	Name:        PatternMiddleware,
	Description: "Handler chain pattern for HTTP or similar",
	Languages:   []string{"go"},

	StructuralCheck: func(ctx context.Context, g *graph.Graph, idx *index.SymbolIndex, scope string) []PatternCandidate {
		candidates := make([]PatternCandidate, 0)

		functions := idx.GetByKind(ast.SymbolKindFunction)

		for _, fn := range functions {
			if ctx.Err() != nil {
				break
			}

			// Skip non-matching scope
			if scope != "" && !strings.HasPrefix(fn.FilePath, scope) {
				continue
			}

			// Middleware pattern: func(Handler) Handler
			sig := fn.Signature

			// Check for Handler in signature (input and output)
			if (strings.Contains(sig, "Handler") || strings.Contains(sig, "HandlerFunc")) &&
				strings.Count(sig, "Handler") >= 2 {

				candidates = append(candidates, PatternCandidate{
					SymbolIDs: []string{fn.ID},
					Location:  fn.FilePath,
					Metadata: map[string]interface{}{
						"function":  fn.ID,
						"name":      fn.Name,
						"signature": sig,
					},
				})
			}
		}

		return candidates
	},

	IdiomaticCheck: func(candidate PatternCandidate, idx *index.SymbolIndex) (bool, []string) {
		// Middleware is idiomatic if it follows the pattern
		return true, nil
	},
}

// findMethodsForType finds all methods for a given type name.
func findMethodsForType(idx *index.SymbolIndex, typeName string) []*ast.Symbol {
	methods := make([]*ast.Symbol, 0)

	allMethods := idx.GetByKind(ast.SymbolKindMethod)
	for _, m := range allMethods {
		// Check if receiver matches type
		if strings.Contains(m.Receiver, typeName) {
			methods = append(methods, m)
		}
	}

	return methods
}

// DefaultMatchers returns the standard set of pattern matchers.
func DefaultMatchers() map[PatternType]*PatternMatcher {
	return map[PatternType]*PatternMatcher{
		PatternSingleton:  SingletonMatcher,
		PatternFactory:    FactoryMatcher,
		PatternBuilder:    BuilderMatcher,
		PatternOptions:    OptionsMatcher,
		PatternMiddleware: MiddlewareMatcher,
	}
}
