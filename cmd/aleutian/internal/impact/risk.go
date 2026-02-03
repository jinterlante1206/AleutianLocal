// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package impact

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/internal/initializer"
)

// Risk scoring weights (configurable in future via config file).
const (
	weightHighFanIn     = 0.3
	weightVeryHighFanIn = 0.2
	weightSecurityPath  = 0.4
	weightPublicAPI     = 0.2
	weightDBOperations  = 0.15
	weightIOOperations  = 0.1
)

// Risk thresholds.
const (
	thresholdCritical = 0.7
	thresholdHigh     = 0.5
	thresholdMedium   = 0.3
)

// High fan-in thresholds.
const (
	highFanInThreshold     = 10
	veryHighFanInThreshold = 50
)

// SecurityPaths are paths that indicate security-sensitive code.
var SecurityPaths = []string{
	"auth", "authentication", "authorization",
	"crypto", "cryptography", "encryption",
	"security", "secure",
	"password", "credential", "secret",
	"token", "jwt", "oauth",
	"permission", "access", "acl",
}

// PublicAPIPaths are paths that indicate public API code.
var PublicAPIPaths = []string{
	"api", "handler", "controller",
	"endpoint", "route", "router",
	"server", "http", "grpc",
	"cmd", "main",
}

// DBOperationPatterns are symbol name patterns that indicate database operations.
var DBOperationPatterns = []string{
	"Query", "Exec", "Execute",
	"Insert", "Update", "Delete", "Select",
	"Create", "Drop", "Alter",
	"Transaction", "Commit", "Rollback",
	"Connect", "Close", "Pool",
}

// IOOperationPatterns are symbol name patterns that indicate IO operations.
var IOOperationPatterns = []string{
	"Read", "Write", "Open", "Close",
	"File", "Directory", "Path",
	"Stream", "Buffer", "Pipe",
	"Socket", "Connection", "Dial",
	"HTTP", "Request", "Response",
}

// RiskAssessor calculates risk levels for impact analysis.
//
// # Thread Safety
//
// RiskAssessor is safe for concurrent use.
type RiskAssessor struct{}

// NewRiskAssessor creates a new RiskAssessor.
func NewRiskAssessor() *RiskAssessor {
	return &RiskAssessor{}
}

// CalculateRisk computes the risk level and factors for the given impact data.
//
// # Inputs
//
//   - changedSymbols: Symbols that were directly changed.
//   - affectedSymbols: Symbols in the blast radius.
//   - affectedTests: Test files affected by the changes.
//   - affectedPackages: Packages containing affected symbols.
//
// # Outputs
//
//   - RiskLevel: The computed risk level.
//   - RiskFactors: Detailed factors that contributed to the risk.
func (r *RiskAssessor) CalculateRisk(
	changedSymbols []ChangedSymbol,
	affectedSymbols []AffectedSymbol,
	affectedTests []string,
	affectedPackages []string,
) (RiskLevel, RiskFactors) {
	factors := RiskFactors{
		AffectedPackages: len(affectedPackages),
		AffectedTests:    len(affectedTests),
		Reasons:          make([]string, 0),
	}

	// Count direct vs transitive callers
	for _, sym := range affectedSymbols {
		if sym.Depth == 1 {
			factors.DirectCallers++
		} else {
			factors.TransitiveCallers++
		}
	}

	// Check for security-sensitive paths
	for _, cs := range changedSymbols {
		path := cs.FilePath
		if path == "" {
			path = cs.Symbol.FilePath
		}
		if isSecurityPath(path) {
			factors.IsSecurityPath = true
			factors.Reasons = append(factors.Reasons, "Changes affect security-sensitive code path")
			break
		}
	}

	// Check for public API paths
	for _, cs := range changedSymbols {
		path := cs.FilePath
		if path == "" {
			path = cs.Symbol.FilePath
		}
		if isPublicAPIPath(path) {
			factors.IsPublicAPI = true
			factors.Reasons = append(factors.Reasons, "Changes affect public API")
			break
		}
	}

	// Check for DB operations in changed symbols
	for _, cs := range changedSymbols {
		if isDBOperation(cs.Symbol.Name) {
			factors.HasDBOperations = true
			factors.Reasons = append(factors.Reasons, "Changes affect database operations")
			break
		}
	}

	// Check for IO operations in changed symbols
	for _, cs := range changedSymbols {
		if isIOOperation(cs.Symbol.Name) {
			factors.HasIOOperations = true
			factors.Reasons = append(factors.Reasons, "Changes affect IO operations")
			break
		}
	}

	// Add fan-in reasons
	if factors.DirectCallers > highFanInThreshold {
		factors.Reasons = append(factors.Reasons,
			"High fan-in: "+formatInt(factors.DirectCallers)+" direct callers")
	}
	if factors.TransitiveCallers > veryHighFanInThreshold {
		factors.Reasons = append(factors.Reasons,
			"Large blast radius: "+formatInt(factors.TransitiveCallers)+" transitive consumers")
	}

	// Calculate score
	score := r.calculateScore(factors)

	// Determine risk level
	var level RiskLevel
	switch {
	case score >= thresholdCritical:
		level = RiskCritical
	case score >= thresholdHigh:
		level = RiskHigh
	case score >= thresholdMedium:
		level = RiskMedium
	default:
		level = RiskLow
	}

	return level, factors
}

// calculateScore computes a numeric risk score from factors.
func (r *RiskAssessor) calculateScore(factors RiskFactors) float64 {
	score := 0.0

	// Fan-in scoring
	if factors.DirectCallers > highFanInThreshold {
		score += weightHighFanIn
	}
	if factors.TransitiveCallers > veryHighFanInThreshold {
		score += weightVeryHighFanIn
	}

	// Sensitivity scoring
	if factors.IsSecurityPath {
		score += weightSecurityPath
	}
	if factors.IsPublicAPI {
		score += weightPublicAPI
	}
	if factors.HasDBOperations {
		score += weightDBOperations
	}
	if factors.HasIOOperations {
		score += weightIOOperations
	}

	return score
}

// isSecurityPath checks if a file path is security-sensitive.
func isSecurityPath(path string) bool {
	lower := strings.ToLower(path)
	for _, p := range SecurityPaths {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// isPublicAPIPath checks if a file path is in a public API location.
func isPublicAPIPath(path string) bool {
	lower := strings.ToLower(path)
	// Check directory components
	dir := filepath.Dir(lower)
	for _, p := range PublicAPIPaths {
		if strings.Contains(dir, p) {
			return true
		}
	}
	return false
}

// isDBOperation checks if a symbol name indicates a database operation.
func isDBOperation(name string) bool {
	for _, pattern := range DBOperationPatterns {
		if strings.Contains(name, pattern) {
			return true
		}
	}
	return false
}

// isIOOperation checks if a symbol name indicates an IO operation.
func isIOOperation(name string) bool {
	for _, pattern := range IOOperationPatterns {
		if strings.Contains(name, pattern) {
			return true
		}
	}
	return false
}

// isTestFile checks if a file path is a test file.
func isTestFile(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, "_test.go") ||
		strings.HasPrefix(base, "test_") ||
		strings.Contains(base, "_test_") ||
		strings.HasSuffix(base, ".test.ts") ||
		strings.HasSuffix(base, ".test.js") ||
		strings.HasSuffix(base, ".spec.ts") ||
		strings.HasSuffix(base, ".spec.js")
}

// isSupportedFile checks if a file should be analyzed.
func isSupportedFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".java", ".rs", ".c", ".cpp", ".h", ".hpp":
		return true
	default:
		return false
	}
}

// GetAffectedPackages extracts unique packages from affected symbols.
func GetAffectedPackages(symbols []AffectedSymbol) []string {
	packages := make(map[string]bool)
	for _, s := range symbols {
		pkg := filepath.Dir(s.Symbol.FilePath)
		if pkg != "" && pkg != "." {
			packages[pkg] = true
		}
	}

	result := make([]string, 0, len(packages))
	for pkg := range packages {
		result = append(result, pkg)
	}
	return result
}

// GetAffectedTests finds test files that test any of the affected symbols.
func GetAffectedTests(affected []AffectedSymbol, index *initializer.MemoryIndex) []string {
	tests := make(map[string]bool)

	// For each affected symbol, find test files in the same package
	for _, sym := range affected {
		dir := filepath.Dir(sym.Symbol.FilePath)

		// Look for test files in the same directory
		for _, s := range index.Symbols {
			if filepath.Dir(s.FilePath) == dir && isTestFile(s.FilePath) {
				tests[s.FilePath] = true
			}
		}
	}

	result := make([]string, 0, len(tests))
	for t := range tests {
		result = append(result, t)
	}
	return result
}

// formatInt formats an integer for display.
func formatInt(n int) string {
	return strconv.Itoa(n)
}
