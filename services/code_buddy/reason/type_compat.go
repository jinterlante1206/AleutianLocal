// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package reason

import (
	"context"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

// TypeCompatibilityChecker checks if types are compatible.
//
// Description:
//
//	TypeCompatibilityChecker analyzes whether one type can be used where
//	another is expected. This includes interface satisfaction, structural
//	compatibility, and possible conversions.
//
// Thread Safety:
//
//	TypeCompatibilityChecker is safe for concurrent use.
type TypeCompatibilityChecker struct {
	graph *graph.Graph
	index *index.SymbolIndex
}

// NewTypeCompatibilityChecker creates a new TypeCompatibilityChecker.
//
// Description:
//
//	Creates a checker that can analyze type compatibility using the
//	code graph and symbol index.
//
// Inputs:
//
//	g - The code graph. Must be frozen.
//	idx - The symbol index.
//
// Outputs:
//
//	*TypeCompatibilityChecker - The configured checker.
func NewTypeCompatibilityChecker(g *graph.Graph, idx *index.SymbolIndex) *TypeCompatibilityChecker {
	return &TypeCompatibilityChecker{
		graph: g,
		index: idx,
	}
}

// CheckCompatibility checks if sourceType can be used where targetType is expected.
//
// Description:
//
//	Analyzes whether a source type is compatible with a target type.
//	Checks for direct assignment compatibility, interface satisfaction,
//	and suggests conversions when applicable.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	sourceType - The type you have (type name or full path).
//	targetType - The type you need (type name or full path).
//
// Outputs:
//
//	*TypeCompatibility - Compatibility analysis result.
//	error - Non-nil if the analysis fails.
//
// Example:
//
//	result, err := checker.CheckCompatibility(ctx, "*MyStruct", "io.Reader")
//	if result.Compatible {
//	    fmt.Println("Types are compatible")
//	} else {
//	    fmt.Printf("Incompatible: %s\n", result.Reason)
//	}
//
// Limitations:
//
//   - Performs structural analysis only, not full semantic type checking
//   - May not detect all interface implementations
//   - Requires types to be present in the index
func (c *TypeCompatibilityChecker) CheckCompatibility(
	ctx context.Context,
	sourceType string,
	targetType string,
) (*TypeCompatibility, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}
	if sourceType == "" || targetType == "" {
		return nil, ErrInvalidInput
	}

	result := &TypeCompatibility{
		SourceType:  sourceType,
		TargetType:  targetType,
		Conversions: make([]string, 0),
	}

	// Normalize type names
	sourceNorm := normalizeTypeName(sourceType)
	targetNorm := normalizeTypeName(targetType)

	// Check for exact match
	if sourceNorm == targetNorm {
		result.Compatible = true
		result.Reason = "Exact type match"
		result.Confidence = 1.0
		return result, nil
	}

	// Check for builtin compatibility
	if compatible, reason := checkBuiltinCompatibility(sourceNorm, targetNorm); compatible {
		result.Compatible = true
		result.Reason = reason
		result.Confidence = 0.95
		return result, nil
	}

	// Check for interface satisfaction
	if c.graph != nil && c.index != nil {
		if satisfied, reason := c.checkInterfaceSatisfaction(sourceNorm, targetNorm); satisfied {
			result.Compatible = true
			result.Reason = reason
			result.Confidence = 0.9
			return result, nil
		}
	}

	// Check for pointer/value compatibility
	if compatible, reason, confidence := checkPointerCompatibility(sourceNorm, targetNorm); compatible {
		result.Compatible = true
		result.Reason = reason
		result.Confidence = confidence
		return result, nil
	}

	// Check for slice/array compatibility
	if compatible, reason := checkSliceCompatibility(sourceNorm, targetNorm); compatible {
		result.Compatible = true
		result.Reason = reason
		result.Confidence = 0.85
		return result, nil
	}

	// Suggest conversions
	result.Conversions = suggestConversions(sourceNorm, targetNorm)

	// Not compatible
	result.Compatible = false
	result.Reason = "Types are not compatible"
	result.Confidence = 0.8

	return result, nil
}

// CheckInterfaceSatisfaction checks if a type satisfies an interface.
//
// Description:
//
//	Determines whether a concrete type implements all methods required
//	by an interface. Uses the code graph to find method definitions.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	typeName - The concrete type to check.
//	interfaceName - The interface to check against.
//
// Outputs:
//
//	*InterfaceSatisfaction - Detailed satisfaction analysis.
//	error - Non-nil if the analysis fails.
func (c *TypeCompatibilityChecker) CheckInterfaceSatisfaction(
	ctx context.Context,
	typeName string,
	interfaceName string,
) (*InterfaceSatisfaction, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}
	if typeName == "" || interfaceName == "" {
		return nil, ErrInvalidInput
	}

	result := &InterfaceSatisfaction{
		TypeName:       typeName,
		InterfaceName:  interfaceName,
		MissingMethods: make([]string, 0),
		MatchedMethods: make([]MethodMatch, 0),
	}

	// Find interface symbol
	interfaceSymbol := c.findTypeSymbol(interfaceName)
	if interfaceSymbol == nil {
		result.Reason = "Interface not found in index"
		result.Confidence = 0.3
		return result, nil
	}

	// Find type symbol
	typeSymbol := c.findTypeSymbol(typeName)
	if typeSymbol == nil {
		result.Reason = "Type not found in index"
		result.Confidence = 0.3
		return result, nil
	}

	// Get interface methods
	interfaceMethods := c.getInterfaceMethods(interfaceSymbol)
	if len(interfaceMethods) == 0 {
		// Empty interface - everything satisfies it
		result.Satisfied = true
		result.Reason = "Empty interface is satisfied by all types"
		result.Confidence = 1.0
		return result, nil
	}

	// Get type methods
	typeMethods := c.getTypeMethods(typeSymbol)

	// Check each interface method
	for _, ifaceMethod := range interfaceMethods {
		matched := false
		for _, typeMethod := range typeMethods {
			if methodsMatch(ifaceMethod, typeMethod) {
				result.MatchedMethods = append(result.MatchedMethods, MethodMatch{
					InterfaceMethod: ifaceMethod.Name,
					TypeMethod:      typeMethod.Name,
					SignatureMatch:  true,
				})
				matched = true
				break
			}
		}
		if !matched {
			result.MissingMethods = append(result.MissingMethods, ifaceMethod.Name)
		}
	}

	result.Satisfied = len(result.MissingMethods) == 0
	if result.Satisfied {
		result.Reason = "Type implements all interface methods"
		result.Confidence = 0.9
	} else {
		result.Reason = "Type is missing required methods"
		result.Confidence = 0.85
	}

	return result, nil
}

// InterfaceSatisfaction represents the result of interface satisfaction check.
type InterfaceSatisfaction struct {
	// TypeName is the type being checked.
	TypeName string `json:"type_name"`

	// InterfaceName is the interface being checked against.
	InterfaceName string `json:"interface_name"`

	// Satisfied indicates if the type satisfies the interface.
	Satisfied bool `json:"satisfied"`

	// MissingMethods lists methods the type doesn't implement.
	MissingMethods []string `json:"missing_methods,omitempty"`

	// MatchedMethods lists methods that match.
	MatchedMethods []MethodMatch `json:"matched_methods,omitempty"`

	// Reason explains the result.
	Reason string `json:"reason"`

	// Confidence is how confident we are (0.0-1.0).
	Confidence float64 `json:"confidence"`
}

// MethodMatch represents a matched method between type and interface.
type MethodMatch struct {
	// InterfaceMethod is the method name from the interface.
	InterfaceMethod string `json:"interface_method"`

	// TypeMethod is the matching method from the type.
	TypeMethod string `json:"type_method"`

	// SignatureMatch indicates if signatures match exactly.
	SignatureMatch bool `json:"signature_match"`
}

// checkInterfaceSatisfaction is an internal helper for basic interface check.
func (c *TypeCompatibilityChecker) checkInterfaceSatisfaction(sourceType, targetType string) (bool, string) {
	// Check if target is an interface
	targetSymbol := c.findTypeSymbol(targetType)
	if targetSymbol == nil || targetSymbol.Kind != ast.SymbolKindInterface {
		return false, ""
	}

	// Check if source implements target interface
	sourceSymbol := c.findTypeSymbol(sourceType)
	if sourceSymbol == nil {
		return false, ""
	}

	// Check implements relationship in graph
	if c.graph != nil {
		node, found := c.graph.GetNode(sourceSymbol.ID)
		if found && node != nil {
			for _, edge := range node.Outgoing {
				if edge.Type == graph.EdgeTypeImplements && edge.ToID == targetSymbol.ID {
					return true, "Type implements interface"
				}
			}
		}
	}

	return false, ""
}

// findTypeSymbol finds a type symbol by name.
func (c *TypeCompatibilityChecker) findTypeSymbol(typeName string) *ast.Symbol {
	if c.index == nil {
		return nil
	}

	// Normalize: remove pointer/slice markers
	cleanName := strings.TrimPrefix(typeName, "*")
	cleanName = strings.TrimPrefix(cleanName, "[]")

	// Try exact match first
	symbols := c.index.GetByName(cleanName)
	for _, sym := range symbols {
		if sym.Kind == ast.SymbolKindStruct ||
			sym.Kind == ast.SymbolKindInterface ||
			sym.Kind == ast.SymbolKindType ||
			sym.Kind == ast.SymbolKindClass {
			return sym
		}
	}

	// Try with package prefix
	if strings.Contains(cleanName, ".") {
		parts := strings.Split(cleanName, ".")
		shortName := parts[len(parts)-1]
		symbols = c.index.GetByName(shortName)
		for _, sym := range symbols {
			if strings.HasSuffix(sym.Package, parts[0]) {
				return sym
			}
		}
	}

	return nil
}

// getInterfaceMethods gets all methods of an interface.
func (c *TypeCompatibilityChecker) getInterfaceMethods(iface *ast.Symbol) []*ast.Symbol {
	methods := make([]*ast.Symbol, 0)

	// Check children first
	if iface.Children != nil {
		for _, child := range iface.Children {
			if child.Kind == ast.SymbolKindMethod || child.Kind == ast.SymbolKindFunction {
				methods = append(methods, child)
			}
		}
	}

	// Check graph for method definitions
	if c.graph != nil {
		node, found := c.graph.GetNode(iface.ID)
		if found && node != nil {
			for _, edge := range node.Outgoing {
				if edge.Type == graph.EdgeTypeDefines {
					methodNode, methodFound := c.graph.GetNode(edge.ToID)
					if methodFound && methodNode != nil && methodNode.Symbol != nil {
						if methodNode.Symbol.Kind == ast.SymbolKindMethod {
							methods = append(methods, methodNode.Symbol)
						}
					}
				}
			}
		}
	}

	return methods
}

// getTypeMethods gets all methods of a type.
func (c *TypeCompatibilityChecker) getTypeMethods(typeSymbol *ast.Symbol) []*ast.Symbol {
	methods := make([]*ast.Symbol, 0)

	// Check children
	if typeSymbol.Children != nil {
		for _, child := range typeSymbol.Children {
			if child.Kind == ast.SymbolKindMethod {
				methods = append(methods, child)
			}
		}
	}

	// Check graph for methods with this receiver
	if c.graph != nil && c.index != nil {
		// Find methods by receiver type
		allMethods := c.index.GetByKind(ast.SymbolKindMethod)
		typeName := typeSymbol.Name
		for _, method := range allMethods {
			if method.Receiver != "" {
				// Check if receiver matches type
				receiverClean := strings.TrimPrefix(method.Receiver, "*")
				if receiverClean == typeName {
					methods = append(methods, method)
				}
			}
		}
	}

	return methods
}

// methodsMatch checks if two methods have compatible signatures.
func methodsMatch(ifaceMethod, typeMethod *ast.Symbol) bool {
	if ifaceMethod.Name != typeMethod.Name {
		return false
	}

	// Compare signatures (simplified - just check if they look similar)
	// Full comparison would need signature parsing
	if ifaceMethod.Signature != "" && typeMethod.Signature != "" {
		// Normalize signatures for comparison
		ifaceSig := normalizeSignature(ifaceMethod.Signature)
		typeSig := normalizeSignature(typeMethod.Signature)

		// Remove receiver from type method signature for comparison
		typeSig = removeReceiver(typeSig)

		return signaturesCompatible(ifaceSig, typeSig)
	}

	// If no signature info, assume match by name
	return true
}

// Helper functions

func normalizeTypeName(name string) string {
	name = strings.TrimSpace(name)
	// Remove leading/trailing whitespace and normalize
	return name
}

func checkBuiltinCompatibility(source, target string) (bool, string) {
	// Check common Go builtin conversions
	builtinCompatible := map[string][]string{
		"int":     {"int", "int64", "int32"},
		"int64":   {"int64", "int"},
		"int32":   {"int32", "int"},
		"float64": {"float64", "float32"},
		"float32": {"float32", "float64"},
		"string":  {"string"},
		"bool":    {"bool"},
		"byte":    {"byte", "uint8"},
		"rune":    {"rune", "int32"},
		"error":   {"error"},
	}

	// interface{} / any accepts everything
	if target == "interface{}" || target == "any" {
		return true, "Any type satisfies empty interface"
	}

	if targets, ok := builtinCompatible[source]; ok {
		for _, t := range targets {
			if t == target {
				return true, "Builtin type compatibility"
			}
		}
	}

	return false, ""
}

func checkPointerCompatibility(source, target string) (bool, string, float64) {
	// *T and T interoperability (with caveats)
	sourceIsPtr := strings.HasPrefix(source, "*")
	targetIsPtr := strings.HasPrefix(target, "*")

	sourceBase := strings.TrimPrefix(source, "*")
	targetBase := strings.TrimPrefix(target, "*")

	if sourceBase == targetBase {
		if sourceIsPtr && !targetIsPtr {
			return true, "Pointer can be dereferenced", 0.7
		}
		if !sourceIsPtr && targetIsPtr {
			return true, "Address can be taken (if addressable)", 0.6
		}
	}

	return false, "", 0
}

func checkSliceCompatibility(source, target string) (bool, string) {
	sourceIsSlice := strings.HasPrefix(source, "[]")
	targetIsSlice := strings.HasPrefix(target, "[]")

	if sourceIsSlice && targetIsSlice {
		sourceElem := strings.TrimPrefix(source, "[]")
		targetElem := strings.TrimPrefix(target, "[]")
		if sourceElem == targetElem {
			return true, "Slice types match"
		}
	}

	return false, ""
}

func suggestConversions(source, target string) []string {
	conversions := make([]string, 0)

	// Numeric conversions
	numericTypes := map[string]bool{
		"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
		"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
		"float32": true, "float64": true,
	}

	if numericTypes[source] && numericTypes[target] {
		conversions = append(conversions, target+"(value)")
	}

	// String conversions
	if source == "[]byte" && target == "string" {
		conversions = append(conversions, "string(value)")
	}
	if source == "string" && target == "[]byte" {
		conversions = append(conversions, "[]byte(value)")
	}
	if source == "[]rune" && target == "string" {
		conversions = append(conversions, "string(value)")
	}
	if source == "string" && target == "[]rune" {
		conversions = append(conversions, "[]rune(value)")
	}

	// Pointer conversions
	if !strings.HasPrefix(source, "*") && strings.HasPrefix(target, "*") {
		conversions = append(conversions, "&value (if addressable)")
	}
	if strings.HasPrefix(source, "*") && !strings.HasPrefix(target, "*") {
		conversions = append(conversions, "*value (dereference)")
	}

	return conversions
}

func normalizeSignature(sig string) string {
	// Remove whitespace variations
	sig = strings.TrimSpace(sig)
	sig = strings.ReplaceAll(sig, "\n", " ")
	sig = strings.ReplaceAll(sig, "\t", " ")
	// Collapse multiple spaces
	for strings.Contains(sig, "  ") {
		sig = strings.ReplaceAll(sig, "  ", " ")
	}
	return sig
}

func removeReceiver(sig string) string {
	// Remove receiver from method signature
	// "func (r *T) Method(...)" -> "func Method(...)"
	if strings.HasPrefix(sig, "func (") {
		// Find end of receiver
		depth := 0
		for i, r := range sig {
			switch r {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					// Found end of receiver
					rest := strings.TrimSpace(sig[i+1:])
					return "func " + rest
				}
			}
		}
	}
	return sig
}

func signaturesCompatible(sig1, sig2 string) bool {
	// Simplified comparison - in production would use parsed signatures
	sig1 = strings.TrimPrefix(sig1, "func ")
	sig2 = strings.TrimPrefix(sig2, "func ")

	// Extract just the parameter and return types
	// This is a simplified check
	return sig1 == sig2
}
