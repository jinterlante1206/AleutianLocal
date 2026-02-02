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
	"regexp"
	"strings"
)

// LanguageMarker defines a pattern that indicates a specific language.
type LanguageMarker struct {
	// Pattern is the regex to match.
	Pattern *regexp.Regexp

	// Description is a human-readable description.
	Description string

	// Weight is how strongly this indicates the language (0.0-1.0).
	Weight float64
}

// LanguageChecker detects wrong-language content in responses.
//
// This checker identifies when the LLM describes code in a language
// different from the project's actual language. For example, detecting
// Python/Flask patterns in a Go project response.
//
// Thread Safety: Safe for concurrent use (stateless after construction).
type LanguageChecker struct {
	patterns        map[string][]LanguageMarker
	weightThreshold float64
}

// NewLanguageChecker creates a new language consistency checker.
//
// Inputs:
//
//	config - Configuration for the checker (nil uses defaults).
//
// Outputs:
//
//	*LanguageChecker - The configured checker.
func NewLanguageChecker(config *LanguageCheckerConfig) *LanguageChecker {
	if config == nil {
		config = DefaultLanguageCheckerConfig()
	}

	lc := &LanguageChecker{
		patterns:        make(map[string][]LanguageMarker),
		weightThreshold: config.WeightThreshold,
	}

	if config.EnablePython {
		lc.patterns["python"] = pythonMarkers()
	}
	if config.EnableJavaScript {
		lc.patterns["javascript"] = javascriptMarkers()
	}
	if config.EnableGo {
		lc.patterns["go"] = goMarkers()
	}

	return lc
}

// Name implements Checker.
func (c *LanguageChecker) Name() string {
	return "language_checker"
}

// Check implements Checker.
//
// Description:
//
//	Scans the response for language-specific patterns and flags
//	any patterns that don't match the project's language.
//
//	IMPORTANT: If a language is present in the EvidenceIndex (meaning files of that
//	language were actually shown to the LLM), we don't flag that language as a
//	violation. This supports hybrid repos (e.g., Go backend with Python scripts).
//
// Thread Safety: Safe for concurrent use.
func (c *LanguageChecker) Check(ctx context.Context, input *CheckInput) []Violation {
	var violations []Violation
	projectLang := strings.ToLower(input.ProjectLang)

	// Limit response size for performance
	response := input.Response
	if len(response) > 10000 {
		response = response[:10000]
	}

	for lang, markers := range c.patterns {
		// Skip markers for the correct language
		if lang == projectLang {
			continue
		}

		// HYBRID REPO SUPPORT: Skip if this language is in the EvidenceIndex.
		// This means files of this language were actually shown in context,
		// so discussing them is legitimate, not hallucination.
		if input.EvidenceIndex != nil && input.EvidenceIndex.Languages[lang] {
			continue
		}

		totalWeight := 0.0
		var matchedMarkers []string

		for _, marker := range markers {
			select {
			case <-ctx.Done():
				return violations
			default:
			}

			if marker.Pattern.MatchString(response) {
				totalWeight += marker.Weight
				matchedMarkers = append(matchedMarkers, marker.Description)
			}
		}

		// If accumulated weight exceeds threshold, it's a hallucination
		if totalWeight >= c.weightThreshold {
			violations = append(violations, Violation{
				Type:     ViolationWrongLanguage,
				Severity: SeverityCritical,
				Code:     "WRONG_LANGUAGE_" + strings.ToUpper(lang),
				Message: fmt.Sprintf("Response contains %s patterns in %s project: %s",
					lang, projectLang, strings.Join(matchedMarkers, ", ")),
				Evidence: strings.Join(matchedMarkers, ", "),
				Expected: projectLang + " patterns only",
			})
		}
	}

	return violations
}

// pythonMarkers returns patterns that indicate Python code.
func pythonMarkers() []LanguageMarker {
	return []LanguageMarker{
		{regexp.MustCompile(`from\s+\w+\s+import`), "Python import", 0.95},
		{regexp.MustCompile(`def\s+\w+\s*\([^)]*\)\s*:`), "Python function def", 0.99},
		{regexp.MustCompile(`class\s+\w+\s*(\([^)]*\))?\s*:`), "Python class", 0.95},
		{regexp.MustCompile(`if\s+__name__\s*==\s*["']__main__["']`), "Python main guard", 0.99},
		{regexp.MustCompile(`pip\s+install`), "pip command", 0.90},
		{regexp.MustCompile(`requirements\.txt`), "requirements.txt", 0.95},
		{regexp.MustCompile(`\.py\b`), "Python extension", 0.85},
		{regexp.MustCompile(`__init__\.py`), "Python init", 0.99},
		{regexp.MustCompile(`(?i)(flask|django|fastapi|pyramid|tornado)`), "Python framework", 0.95},
		{regexp.MustCompile(`@app\.route`), "Flask route decorator", 0.99},
		{regexp.MustCompile(`Blueprint\s*\(`), "Flask blueprint", 0.99},
		{regexp.MustCompile(`(?i)virtualenv|venv`), "Python venv", 0.70},
		{regexp.MustCompile(`self\.\w+`), "Python self", 0.60},
		{regexp.MustCompile(`except\s+\w+(\s+as\s+\w+)?:`), "Python except", 0.90},
	}
}

// javascriptMarkers returns patterns that indicate JavaScript/TypeScript code.
func javascriptMarkers() []LanguageMarker {
	return []LanguageMarker{
		{regexp.MustCompile(`require\s*\(\s*['"]`), "JS require", 0.90},
		{regexp.MustCompile(`import\s+.*\s+from\s+['"]`), "ES6 import", 0.90},
		{regexp.MustCompile(`export\s+(default\s+)?`), "ES6 export", 0.85},
		{regexp.MustCompile(`\.(js|jsx|ts|tsx)\b`), "JS extension", 0.85},
		{regexp.MustCompile(`package\.json`), "package.json", 0.95},
		{regexp.MustCompile(`node_modules`), "node_modules", 0.95},
		{regexp.MustCompile(`npm\s+(install|run)`), "npm command", 0.90},
		{regexp.MustCompile(`const\s+\w+\s*=\s*require`), "CommonJS", 0.95},
		{regexp.MustCompile(`(?i)(express|react|vue|angular|next\.js)`), "JS framework", 0.90},
		{regexp.MustCompile(`=>\s*\{`), "Arrow function", 0.60},
		{regexp.MustCompile(`async\s+function`), "Async function", 0.70},
		{regexp.MustCompile(`\.then\s*\(`), "Promise then", 0.70},
	}
}

// goMarkers returns patterns that indicate Go code.
func goMarkers() []LanguageMarker {
	return []LanguageMarker{
		{regexp.MustCompile(`package\s+\w+`), "Go package", 0.85},
		{regexp.MustCompile(`func\s+\(\w+\s+\*?\w+\)`), "Go method", 0.99},
		{regexp.MustCompile(`import\s+\(`), "Go import block", 0.95},
		{regexp.MustCompile(`\.go\b`), "Go extension", 0.85},
		{regexp.MustCompile(`go\.(mod|sum)`), "Go modules", 0.99},
		{regexp.MustCompile(`go\s+(get|build|test|run)`), "Go command", 0.85},
		{regexp.MustCompile(`func\s+\w+\s*\([^)]*\)\s*\(?[^)]*\)?\s*\{`), "Go function", 0.90},
		{regexp.MustCompile(`(?i)(gin|echo|fiber|chi|gorilla)`), "Go framework", 0.90},
		{regexp.MustCompile(`if\s+err\s*!=\s*nil`), "Go error check", 0.95},
		{regexp.MustCompile(`make\s*\(\s*(map|chan|slice|\[\])`), "Go make", 0.90},
		{regexp.MustCompile(`:=`), "Go short declaration", 0.70},
	}
}
