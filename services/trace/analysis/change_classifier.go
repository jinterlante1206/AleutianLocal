// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package analysis

import (
	"context"
	"fmt"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
)

// ChangeClassifier classifies potential change impacts.
//
// # Description
//
// Analyzes the target symbol to determine what kinds of changes would
// be breaking vs compatible. Helps agents understand the consequences
// of different modification strategies.
//
// # Change Classifications
//
// For Functions:
//   - add_param: BREAKING (all callers must update)
//   - remove_param: BREAKING (all callers must update)
//   - change_return_type: BREAKING (all callers must update)
//   - add_return_value: COMPATIBLE (callers can ignore in Go)
//   - internal_logic: SAFE (no signature change)
//
// For Types:
//   - add_field: COMPATIBLE (existing code works)
//   - remove_field: BREAKING (code using field breaks)
//   - change_field_type: BREAKING
//   - rename_field: BREAKING
//
// For Interfaces:
//   - add_method: BREAKING (all implementers must add)
//   - remove_method: BREAKING (callers of method break)
//   - change_signature: BREAKING (all implementers + callers)
//
// # Thread Safety
//
// Safe for concurrent use (stateless).
type ChangeClassifier struct{}

// Verify interface compliance at compile time
var _ Enricher = (*ChangeClassifier)(nil)

// NewChangeClassifier creates a new change classifier.
func NewChangeClassifier() *ChangeClassifier {
	return &ChangeClassifier{}
}

// Name returns the enricher identifier.
func (c *ChangeClassifier) Name() string {
	return "change_classifier"
}

// Priority returns 2 (secondary analysis).
func (c *ChangeClassifier) Priority() int {
	return 2
}

// Enrich classifies potential change impacts for the target.
//
// # Description
//
// Analyzes the target symbol and generates a list of potential change
// types with their impact classifications. This helps agents understand
// what kinds of modifications are safe vs breaking.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - target: The symbol to analyze.
//   - result: The result to enrich.
//
// # Outputs
//
//   - error: Non-nil on context cancellation.
func (c *ChangeClassifier) Enrich(
	ctx context.Context,
	target *EnrichmentTarget,
	result *EnhancedBlastRadius,
) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Ensure we have a slice to append to
	if result.ChangeImpacts == nil {
		result.ChangeImpacts = make([]ChangeImpact, 0)
	}

	// Determine symbol kind and generate appropriate classifications
	if target.Symbol == nil {
		// Without symbol info, generate generic classifications
		result.ChangeImpacts = append(result.ChangeImpacts, c.genericImpacts(target, result)...)
		return nil
	}

	switch target.Symbol.Kind {
	case ast.SymbolKindFunction, ast.SymbolKindMethod:
		result.ChangeImpacts = append(result.ChangeImpacts, c.functionImpacts(target, result)...)
	case ast.SymbolKindType, ast.SymbolKindStruct:
		result.ChangeImpacts = append(result.ChangeImpacts, c.typeImpacts(target, result)...)
	case ast.SymbolKindInterface:
		result.ChangeImpacts = append(result.ChangeImpacts, c.interfaceImpacts(target, result)...)
	default:
		result.ChangeImpacts = append(result.ChangeImpacts, c.genericImpacts(target, result)...)
	}

	return nil
}

// functionImpacts generates change impacts for a function/method.
func (c *ChangeClassifier) functionImpacts(target *EnrichmentTarget, result *EnhancedBlastRadius) []ChangeImpact {
	callerCount := len(result.DirectCallers)

	return []ChangeImpact{
		{
			ChangeType:    "add_param",
			Impact:        ImpactBreaking,
			AffectedSites: callerCount,
			Description:   fmt.Sprintf("Adding a parameter requires updating all %d callers", callerCount),
			Example:       "func Foo(a int) -> func Foo(a int, b string)",
		},
		{
			ChangeType:    "remove_param",
			Impact:        ImpactBreaking,
			AffectedSites: callerCount,
			Description:   fmt.Sprintf("Removing a parameter requires updating all %d callers", callerCount),
			Example:       "func Foo(a, b int) -> func Foo(a int)",
		},
		{
			ChangeType:    "change_return_type",
			Impact:        ImpactBreaking,
			AffectedSites: callerCount,
			Description:   fmt.Sprintf("Changing return type requires updating all %d callers", callerCount),
			Example:       "func Foo() int -> func Foo() string",
		},
		{
			ChangeType:    "add_return_value",
			Impact:        ImpactCompatible,
			AffectedSites: 0,
			Description:   "Adding a return value is compatible (callers can ignore extra values)",
			Example:       "func Foo() int -> func Foo() (int, error)",
		},
		{
			ChangeType:    "internal_logic",
			Impact:        ImpactSafe,
			AffectedSites: 0,
			Description:   "Internal implementation changes don't affect callers",
			Example:       "Changing algorithm or optimization without signature change",
		},
	}
}

// typeImpacts generates change impacts for a struct/type.
func (c *ChangeClassifier) typeImpacts(target *EnrichmentTarget, result *EnhancedBlastRadius) []ChangeImpact {
	// For types, we estimate usage from indirect references
	usageCount := len(result.DirectCallers) + len(result.IndirectCallers)

	return []ChangeImpact{
		{
			ChangeType:    "add_field",
			Impact:        ImpactCompatible,
			AffectedSites: 0,
			Description:   "Adding a field is compatible (existing code continues to work)",
			Example:       "type Foo struct { A int } -> type Foo struct { A int; B string }",
		},
		{
			ChangeType:    "remove_field",
			Impact:        ImpactBreaking,
			AffectedSites: usageCount,
			Description:   fmt.Sprintf("Removing a field may break up to %d usages", usageCount),
			Example:       "type Foo struct { A, B int } -> type Foo struct { A int }",
		},
		{
			ChangeType:    "change_field_type",
			Impact:        ImpactBreaking,
			AffectedSites: usageCount,
			Description:   fmt.Sprintf("Changing field type may break up to %d usages", usageCount),
			Example:       "type Foo struct { A int } -> type Foo struct { A string }",
		},
		{
			ChangeType:    "rename_field",
			Impact:        ImpactBreaking,
			AffectedSites: usageCount,
			Description:   fmt.Sprintf("Renaming a field may break up to %d usages", usageCount),
			Example:       "type Foo struct { OldName int } -> type Foo struct { NewName int }",
		},
		{
			ChangeType:    "add_method",
			Impact:        ImpactCompatible,
			AffectedSites: 0,
			Description:   "Adding a method is compatible",
			Example:       "func (f *Foo) NewMethod()",
		},
	}
}

// interfaceImpacts generates change impacts for an interface.
func (c *ChangeClassifier) interfaceImpacts(target *EnrichmentTarget, result *EnhancedBlastRadius) []ChangeImpact {
	implementerCount := len(result.Implementers)
	callerCount := len(result.DirectCallers)

	return []ChangeImpact{
		{
			ChangeType:    "add_method",
			Impact:        ImpactBreaking,
			AffectedSites: implementerCount,
			Description:   fmt.Sprintf("Adding a method requires updating all %d implementers", implementerCount),
			Example:       "type Reader interface { Read() } -> type Reader interface { Read(); Close() }",
		},
		{
			ChangeType:    "remove_method",
			Impact:        ImpactBreaking,
			AffectedSites: callerCount,
			Description:   fmt.Sprintf("Removing a method may break %d callers", callerCount),
			Example:       "type ReadWriter interface { Read(); Write() } -> type ReadWriter interface { Read() }",
		},
		{
			ChangeType:    "change_signature",
			Impact:        ImpactBreaking,
			AffectedSites: implementerCount + callerCount,
			Description:   fmt.Sprintf("Changing method signature affects %d implementers and %d callers", implementerCount, callerCount),
			Example:       "Read() []byte -> Read(n int) []byte",
		},
		{
			ChangeType:    "rename_method",
			Impact:        ImpactBreaking,
			AffectedSites: implementerCount + callerCount,
			Description:   fmt.Sprintf("Renaming a method affects %d implementers and %d callers", implementerCount, callerCount),
			Example:       "Read() -> Fetch()",
		},
	}
}

// genericImpacts generates impacts when symbol type is unknown.
func (c *ChangeClassifier) genericImpacts(target *EnrichmentTarget, result *EnhancedBlastRadius) []ChangeImpact {
	totalAffected := len(result.DirectCallers) + len(result.IndirectCallers)

	return []ChangeImpact{
		{
			ChangeType:    "signature_change",
			Impact:        ImpactBreaking,
			AffectedSites: totalAffected,
			Description:   fmt.Sprintf("Signature changes may affect up to %d usages", totalAffected),
		},
		{
			ChangeType:    "internal_change",
			Impact:        ImpactSafe,
			AffectedSites: 0,
			Description:   "Internal changes without signature modification are safe",
		},
		{
			ChangeType:    "rename",
			Impact:        ImpactBreaking,
			AffectedSites: totalAffected,
			Description:   fmt.Sprintf("Renaming requires updating all %d references", totalAffected),
		},
		{
			ChangeType:    "delete",
			Impact:        ImpactBreaking,
			AffectedSites: totalAffected,
			Description:   fmt.Sprintf("Deletion breaks all %d usages", totalAffected),
		},
	}
}

// ClassifyChangeType analyzes a before/after diff to classify the change.
//
// # Description
//
// Given the old and new versions of a symbol, determines what kind of
// change was made. This is used by patch validation to automatically
// detect the impact of proposed changes.
//
// # Inputs
//
//   - oldSymbol: The symbol before changes (nil if new).
//   - newSymbol: The symbol after changes (nil if deleted).
//
// # Outputs
//
//   - string: The change type (e.g., "add_param", "internal_logic").
//   - string: The impact level (BREAKING, COMPATIBLE, SAFE).
//
// # Limitations
//
// This is heuristic-based and may not catch all breaking changes,
// especially semantic changes that don't affect the signature.
func (c *ChangeClassifier) ClassifyChangeType(oldSymbol, newSymbol *ast.Symbol) (changeType string, impact string) {
	if oldSymbol == nil && newSymbol != nil {
		return "add", ImpactCompatible
	}
	if oldSymbol != nil && newSymbol == nil {
		return "delete", ImpactBreaking
	}
	if oldSymbol == nil || newSymbol == nil {
		return "unknown", ImpactSafe
	}

	// Name change
	if oldSymbol.Name != newSymbol.Name {
		return "rename", ImpactBreaking
	}

	// For functions, check signature
	if oldSymbol.Kind == ast.SymbolKindFunction || oldSymbol.Kind == ast.SymbolKindMethod {
		return c.classifyFunctionChange(oldSymbol, newSymbol)
	}

	// Default to internal change if we can't detect specifics
	return "internal_logic", ImpactSafe
}

// classifyFunctionChange compares two function symbols.
func (c *ChangeClassifier) classifyFunctionChange(oldSym, newSym *ast.Symbol) (string, string) {
	// Compare signature string if available
	if oldSym.Signature != newSym.Signature {
		// Try to determine what changed using metadata
		oldReturn := ""
		newReturn := ""
		if oldSym.Metadata != nil {
			oldReturn = oldSym.Metadata.ReturnType
		}
		if newSym.Metadata != nil {
			newReturn = newSym.Metadata.ReturnType
		}

		if oldReturn != newReturn && newReturn != "" && oldReturn != "" {
			return "change_return_type", ImpactBreaking
		}

		// Signature changed but we can't determine specifics - generic breaking change
		return "change_signature", ImpactBreaking
	}

	// Signature unchanged - internal logic change
	return "internal_logic", ImpactSafe
}
