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
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
)

// ConfidenceCalculator calculates analysis confidence scores.
//
// # Description
//
// Evaluates how reliable the blast radius analysis is based on factors
// that can cause incomplete or inaccurate results. Lower confidence
// indicates the agent should be extra careful with changes.
//
// # Confidence Reducers
//
//   - Reflection usage in callers (-20%)
//   - Interface with external implementers (-15%)
//   - Dynamic dispatch patterns (-10%)
//   - Plugin/callback patterns (-10%)
//   - Truncated results (-10% per truncation)
//   - Enricher failures (-5% each)
//
// # Thread Safety
//
// Safe for concurrent use (stateless).
type ConfidenceCalculator struct{}

// Verify interface compliance at compile time
var _ Enricher = (*ConfidenceCalculator)(nil)

// NewConfidenceCalculator creates a new confidence calculator.
func NewConfidenceCalculator() *ConfidenceCalculator {
	return &ConfidenceCalculator{}
}

// Name returns the enricher identifier.
func (c *ConfidenceCalculator) Name() string {
	return "confidence"
}

// Priority returns 3 (runs last, aggregates other results).
func (c *ConfidenceCalculator) Priority() int {
	return 3
}

// Enrich calculates the confidence score for the analysis.
//
// # Description
//
// Examines the analysis results to determine confidence level.
// Starts at 100% and reduces based on detected uncertainty factors.
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
func (c *ConfidenceCalculator) Enrich(
	ctx context.Context,
	target *EnrichmentTarget,
	result *EnhancedBlastRadius,
) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	score := 100
	var reasons []string

	// Check for reflection usage in callers
	reflectionPenalty := c.checkReflectionUsage(target, result)
	if reflectionPenalty > 0 {
		score -= reflectionPenalty
		reasons = append(reasons, "Reflection usage detected in call chain")
	}

	// Check for interface with potential external implementers
	if len(result.Implementers) > 0 {
		if target.Symbol != nil && target.Symbol.Kind == ast.SymbolKindInterface {
			// Check if interface might have external implementers
			if c.hasExternalImplementers(target, result) {
				score -= 15
				reasons = append(reasons, "Interface may have external implementers")
			}
		}
	}

	// Check for dynamic dispatch patterns
	if c.hasDynamicDispatch(target, result) {
		score -= 10
		reasons = append(reasons, "Dynamic dispatch pattern detected")
	}

	// Check for plugin/callback patterns
	if c.hasCallbackPattern(target, result) {
		score -= 10
		reasons = append(reasons, "Plugin/callback pattern detected")
	}

	// Check for truncated results
	if result.Truncated {
		score -= 10
		reasons = append(reasons, "Results were truncated: "+result.TruncatedReason)
	}

	// Check for enricher failures
	failedEnrichers := 0
	for _, er := range result.EnricherResults {
		if !er.Success && !er.Skipped {
			failedEnrichers++
		}
	}
	if failedEnrichers > 0 {
		penalty := failedEnrichers * 5
		if penalty > 20 {
			penalty = 20 // Cap at 20%
		}
		score -= penalty
		reasons = append(reasons, "Some analysis enrichers failed")
	}

	// Check for skipped enrichers (timeout)
	skippedEnrichers := 0
	for _, er := range result.EnricherResults {
		if er.Skipped {
			skippedEnrichers++
		}
	}
	if skippedEnrichers > 0 {
		penalty := skippedEnrichers * 3
		if penalty > 15 {
			penalty = 15 // Cap at 15%
		}
		score -= penalty
		reasons = append(reasons, "Some analysis enrichers were skipped (timeout)")
	}

	// Ensure score doesn't go negative
	if score < 0 {
		score = 0
	}

	cs := NewConfidenceScore(score, reasons)
	result.Confidence = &cs
	return nil
}

// checkReflectionUsage looks for reflection patterns in callers.
func (c *ConfidenceCalculator) checkReflectionUsage(target *EnrichmentTarget, result *EnhancedBlastRadius) int {
	reflectionPatterns := []string{
		"reflect",
		"Reflect",
		"invoke",
		"Invoke",
		"CallMethod",
		"MethodByName",
		"ValueOf",
		"TypeOf",
	}

	reflectionCount := 0

	// Check caller names for reflection patterns
	for _, caller := range result.DirectCallers {
		callerName := extractSymbolName(caller.ID)
		for _, pattern := range reflectionPatterns {
			if strings.Contains(callerName, pattern) {
				reflectionCount++
				break
			}
		}
	}

	// Also check file paths for reflection packages
	for _, caller := range result.DirectCallers {
		if strings.Contains(caller.FilePath, "reflect") {
			reflectionCount++
		}
	}

	// Return penalty based on reflection usage
	if reflectionCount > 5 {
		return 20 // High reflection usage
	}
	if reflectionCount > 0 {
		return 10 + reflectionCount // Moderate reflection usage
	}
	return 0
}

// hasExternalImplementers checks if interface might have external implementations.
func (c *ConfidenceCalculator) hasExternalImplementers(target *EnrichmentTarget, result *EnhancedBlastRadius) bool {
	if target.Symbol == nil {
		return false
	}

	// Check if interface is exported (starts with uppercase)
	if len(target.Symbol.Name) > 0 && target.Symbol.Name[0] >= 'A' && target.Symbol.Name[0] <= 'Z' {
		// Exported interface - might have external implementers
		// Check if any implementers are in different packages
		targetPkg := extractPackage(target.Symbol.FilePath)
		for _, impl := range result.Implementers {
			implPkg := extractPackage(impl.FilePath)
			if implPkg != targetPkg {
				return true // Already has cross-package implementation
			}
		}
		// No cross-package implementations found, but it's exported
		// so external implementations are possible
		return true
	}

	return false
}

// hasDynamicDispatch checks for dynamic dispatch patterns.
func (c *ConfidenceCalculator) hasDynamicDispatch(target *EnrichmentTarget, result *EnhancedBlastRadius) bool {
	dynamicPatterns := []string{
		"Handler",
		"handler",
		"Dispatch",
		"dispatch",
		"Route",
		"route",
		"Registry",
		"registry",
		"Map",
		"lookup",
		"Lookup",
	}

	// Check if target name suggests dynamic dispatch
	if target.Symbol != nil {
		for _, pattern := range dynamicPatterns {
			if strings.Contains(target.Symbol.Name, pattern) {
				return true
			}
		}
	}

	// Check callers for dynamic patterns
	for _, caller := range result.DirectCallers {
		callerName := extractSymbolName(caller.ID)
		for _, pattern := range dynamicPatterns {
			if strings.Contains(callerName, pattern) {
				return true
			}
		}
	}

	return false
}

// hasCallbackPattern checks for callback/plugin patterns.
func (c *ConfidenceCalculator) hasCallbackPattern(target *EnrichmentTarget, result *EnhancedBlastRadius) bool {
	callbackPatterns := []string{
		"Callback",
		"callback",
		"Hook",
		"hook",
		"Plugin",
		"plugin",
		"Middleware",
		"middleware",
		"OnEvent",
		"onEvent",
		"Subscribe",
		"subscribe",
		"Listener",
		"listener",
	}

	// Check target name
	if target.Symbol != nil {
		for _, pattern := range callbackPatterns {
			if strings.Contains(target.Symbol.Name, pattern) {
				return true
			}
		}
	}

	// Check target file path
	if target.Symbol != nil {
		for _, pattern := range callbackPatterns {
			if strings.Contains(strings.ToLower(target.Symbol.FilePath), strings.ToLower(pattern)) {
				return true
			}
		}
	}

	return false
}

// extractPackage extracts the package path from a file path.
func extractPackage(filePath string) string {
	// Find the last directory component before the file
	lastSlash := strings.LastIndex(filePath, "/")
	if lastSlash == -1 {
		return ""
	}
	return filePath[:lastSlash]
}

// ConfidenceThresholds defines threshold values for confidence levels.
var ConfidenceThresholds = struct {
	High   int
	Medium int
}{
	High:   90,
	Medium: 70,
}

// IsHighConfidence returns true if confidence is >= 90%.
func IsHighConfidence(score ConfidenceScore) bool {
	return score.Score >= ConfidenceThresholds.High
}

// IsMediumConfidence returns true if confidence is 70-89%.
func IsMediumConfidence(score ConfidenceScore) bool {
	return score.Score >= ConfidenceThresholds.Medium && score.Score < ConfidenceThresholds.High
}

// IsLowConfidence returns true if confidence is < 70%.
func IsLowConfidence(score ConfidenceScore) bool {
	return score.Score < ConfidenceThresholds.Medium
}
