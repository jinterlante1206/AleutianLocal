// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package reason provides tools for reasoning about code changes.
//
// This package implements the "think before act" principle by providing
// analysis tools that help understand the impact of proposed changes:
//
//   - Breaking change detection (will callers break?)
//   - Change simulation (what needs to be updated?)
//   - Type compatibility checking (can this type be used here?)
//   - Test coverage analysis (what tests cover this?)
//   - Side effect detection (what external effects does this have?)
//   - Refactoring suggestions (how can this code be improved?)
//
// All tools in this package are READ-ONLY and do not modify code.
// They analyze the existing codebase and proposed changes to provide
// actionable insights before changes are made.
package reason

// TypeInfo represents type information extracted from signatures.
type TypeInfo struct {
	// Name is the type name (e.g., "string", "*User", "[]byte").
	Name string `json:"name"`

	// Package is the package path for qualified types.
	// Empty for builtin types.
	Package string `json:"package,omitempty"`

	// IsPointer indicates if this is a pointer type.
	IsPointer bool `json:"is_pointer,omitempty"`

	// IsSlice indicates if this is a slice type.
	IsSlice bool `json:"is_slice,omitempty"`

	// IsMap indicates if this is a map type.
	IsMap bool `json:"is_map,omitempty"`

	// IsChannel indicates if this is a channel type.
	IsChannel bool `json:"is_channel,omitempty"`

	// IsVariadic indicates if this is a variadic parameter (...T).
	IsVariadic bool `json:"is_variadic,omitempty"`

	// ElementType is the element type for slices, arrays, maps, channels.
	ElementType *TypeInfo `json:"element_type,omitempty"`

	// KeyType is the key type for maps.
	KeyType *TypeInfo `json:"key_type,omitempty"`

	// TypeParams lists generic type parameters if any.
	TypeParams []string `json:"type_params,omitempty"`
}

// ParameterInfo represents a function parameter.
type ParameterInfo struct {
	// Name is the parameter name. May be empty for unnamed parameters.
	Name string `json:"name"`

	// Type is the parameter type information.
	Type TypeInfo `json:"type"`

	// Optional indicates if the parameter has a default value (Python/TS).
	Optional bool `json:"optional,omitempty"`

	// Default is the default value expression if optional.
	Default string `json:"default,omitempty"`
}

// TypeParamInfo represents a generic type parameter.
type TypeParamInfo struct {
	// Name is the type parameter name (e.g., "T", "K").
	Name string `json:"name"`

	// Constraint is the type constraint (e.g., "any", "comparable").
	Constraint string `json:"constraint,omitempty"`
}

// ParsedSignature represents a parsed function/method signature.
type ParsedSignature struct {
	// Name is the function/method name.
	Name string `json:"name"`

	// Receiver is the receiver type for methods. Nil for functions.
	Receiver *TypeInfo `json:"receiver,omitempty"`

	// Parameters lists all parameters in order.
	Parameters []ParameterInfo `json:"parameters"`

	// Returns lists all return types in order.
	Returns []TypeInfo `json:"returns"`

	// TypeParams lists generic type parameters if any.
	TypeParams []TypeParamInfo `json:"type_params,omitempty"`

	// Variadic indicates if the last parameter is variadic.
	Variadic bool `json:"variadic,omitempty"`

	// Language is the source language.
	Language string `json:"language"`
}

// Severity represents the severity level of a change or issue.
type Severity string

const (
	// SeverityCritical indicates a critical issue that will cause failures.
	SeverityCritical Severity = "CRITICAL"

	// SeverityHigh indicates a high-priority issue likely to cause problems.
	SeverityHigh Severity = "HIGH"

	// SeverityMedium indicates a medium-priority issue that may cause problems.
	SeverityMedium Severity = "MEDIUM"

	// SeverityLow indicates a low-priority issue or minor inconvenience.
	SeverityLow Severity = "LOW"
)

// BreakingChangeType categorizes the type of breaking change.
type BreakingChangeType string

const (
	// BreakingChangeSignature indicates a signature change (params, returns).
	BreakingChangeSignature BreakingChangeType = "signature"

	// BreakingChangeReturn indicates a return type change.
	BreakingChangeReturn BreakingChangeType = "return"

	// BreakingChangeBehavior indicates a behavior change inferred from usage.
	BreakingChangeBehavior BreakingChangeType = "behavior"

	// BreakingChangeVisibility indicates a visibility change (exported -> unexported).
	BreakingChangeVisibility BreakingChangeType = "visibility"

	// BreakingChangeType indicates a type change (e.g., struct field type).
	BreakingChangeTypeChange BreakingChangeType = "type"

	// BreakingChangeRemoval indicates a symbol was removed.
	BreakingChangeRemoval BreakingChangeType = "removal"
)

// BreakingChange describes a single breaking change detected.
type BreakingChange struct {
	// Type categorizes the breaking change.
	Type BreakingChangeType `json:"type"`

	// Description explains what changed.
	Description string `json:"description"`

	// Affected lists the IDs of affected callers/users.
	Affected []string `json:"affected"`

	// Severity indicates how serious the break is.
	Severity Severity `json:"severity"`

	// AutoFixable indicates if an automatic fix can be generated.
	AutoFixable bool `json:"auto_fixable"`

	// SuggestedFix is the suggested fix if auto-fixable.
	SuggestedFix string `json:"suggested_fix,omitempty"`
}

// BreakingAnalysis is the result of breaking change analysis.
type BreakingAnalysis struct {
	// TargetID is the symbol being analyzed.
	TargetID string `json:"target_id"`

	// IsBreaking indicates if any breaking changes were detected.
	IsBreaking bool `json:"is_breaking"`

	// BreakingChanges lists all detected breaking changes.
	BreakingChanges []BreakingChange `json:"breaking_changes"`

	// SafeChanges lists changes that are safe (non-breaking).
	SafeChanges []string `json:"safe_changes"`

	// CallersAffected is the count of affected callers.
	CallersAffected int `json:"callers_affected"`

	// Confidence is how confident we are in the analysis (0.0-1.0).
	Confidence float64 `json:"confidence"`

	// Limitations lists what we couldn't check.
	Limitations []string `json:"limitations"`
}

// TypeCompatibility is the result of type compatibility checking.
type TypeCompatibility struct {
	// SourceType is the type we have.
	SourceType string `json:"source_type"`

	// TargetType is the type we need.
	TargetType string `json:"target_type"`

	// Compatible indicates if the types are compatible.
	Compatible bool `json:"compatible"`

	// Reason explains why types are or aren't compatible.
	Reason string `json:"reason"`

	// Conversions lists possible conversions if not directly compatible.
	Conversions []string `json:"conversions,omitempty"`

	// Confidence is how confident we are (0.0-1.0).
	Confidence float64 `json:"confidence"`
}
