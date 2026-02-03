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

// extensionToLanguage maps file extensions to language names.
var extensionToLanguage = map[string]string{
	".go":    "go",
	".py":    "python",
	".js":    "javascript",
	".jsx":   "javascript",
	".ts":    "typescript",
	".tsx":   "typescript",
	".java":  "java",
	".rs":    "rust",
	".c":     "c",
	".cpp":   "cpp",
	".h":     "c",
	".hpp":   "cpp",
	".rb":    "ruby",
	".php":   "php",
	".swift": "swift",
	".kt":    "kotlin",
	".scala": "scala",
}

// frameworkBlocklists maps project languages to frameworks that should NOT appear.
// If a Go project mentions Flask, that's a hallucination.
var frameworkBlocklists = map[string]map[string]bool{
	"go": {
		// Python frameworks
		"flask": true, "django": true, "fastapi": true, "pyramid": true,
		"tornado": true, "bottle": true, "falcon": true, "starlette": true,
		// Python tooling
		"pip": true, "virtualenv": true, "conda": true, "poetry": true,
		"requirements.txt": true, "__init__.py": true, "setup.py": true,
		// JavaScript/Node frameworks
		"express": true, "koa": true, "hapi": true, "nest.js": true, "nestjs": true,
		"next.js": true, "nextjs": true, "nuxt": true, "gatsby": true,
		// JavaScript tooling
		"npm": true, "yarn": true, "webpack": true, "package.json": true,
		// Ruby frameworks
		"rails": true, "sinatra": true,
		// Java frameworks
		"spring": true, "springboot": true, "spring boot": true,
	},
	"python": {
		// Go frameworks
		"gin": true, "echo": true, "fiber": true, "chi": true, "gorilla": true,
		"beego": true, "revel": true, "buffalo": true,
		// Go tooling
		"go.mod": true, "go.sum": true, "go get": true, "go build": true,
		// JavaScript frameworks
		"express": true, "react": true, "vue": true, "angular": true,
		// Ruby frameworks
		"rails": true, "sinatra": true,
	},
	"javascript": {
		// Go frameworks
		"gin": true, "echo": true, "fiber": true, "chi": true, "gorilla": true,
		// Go tooling
		"go.mod": true, "go.sum": true,
		// Python frameworks
		"flask": true, "django": true, "fastapi": true,
		// Python tooling
		"pip": true, "requirements.txt": true,
	},
	"typescript": {
		// Go frameworks
		"gin": true, "echo": true, "fiber": true, "chi": true, "gorilla": true,
		// Go tooling
		"go.mod": true, "go.sum": true,
		// Python frameworks
		"flask": true, "django": true, "fastapi": true,
		// Python tooling
		"pip": true, "requirements.txt": true,
	},
}

// languageSuggestions provides correct alternatives when wrong language is detected.
var languageSuggestions = map[string]map[string]string{
	"go": {
		"flask":       "Use net/http or Gin/Echo/Chi for HTTP routing",
		"django":      "Use net/http with gorilla/mux or Chi for request handling",
		"fastapi":     "Use Gin or Echo for fast HTTP APIs with validation",
		"@app.route":  "Use http.HandleFunc or gin.GET/POST for route handlers",
		"pip install": "Use 'go get' for dependencies",
		"import from": "Use 'import' statement without 'from' keyword",
		"def ":        "Use 'func' for function definitions",
		"self.":       "Use receiver type (s *Service) for methods",
	},
	"python": {
		"gin":           "Use Flask or FastAPI for HTTP routing",
		"echo":          "Use Flask or FastAPI for HTTP routing",
		"http.Handle":   "Use @app.route decorator for Flask or @router for FastAPI",
		"go.mod":        "Use requirements.txt or pyproject.toml for dependencies",
		"func ":         "Use 'def' for function definitions",
		":=":            "Use '=' for variable assignment",
		"if err != nil": "Use try/except for error handling",
	},
	"javascript": {
		"flask":         "Use Express.js for HTTP routing",
		"django":        "Use Express.js or Nest.js for web framework",
		"fastapi":       "Use Express.js or Fastify for fast HTTP APIs",
		"gin":           "Use Express.js for HTTP routing",
		"echo":          "Use Express.js or Koa for HTTP routing",
		"go.mod":        "Use package.json for dependencies",
		"pip":           "Use npm or yarn for package management",
		"if err != nil": "Use try/catch or .catch() for error handling",
		"func ":         "Use 'function' or arrow functions for function definitions",
	},
	"typescript": {
		"flask":         "Use Express.js with TypeScript for HTTP routing",
		"django":        "Use Nest.js for TypeScript web framework",
		"fastapi":       "Use Fastify or Express.js with TypeScript for APIs",
		"gin":           "Use Express.js with TypeScript for HTTP routing",
		"echo":          "Use Express.js or Koa with TypeScript for HTTP routing",
		"go.mod":        "Use package.json for dependencies",
		"pip":           "Use npm or yarn for package management",
		"if err != nil": "Use try/catch for error handling",
		"func ":         "Use 'function' or arrow functions with type annotations",
	},
}

// DetectProjectLanguage determines the primary language from extension counts.
//
// Description:
//
//	Analyzes extension counts to determine the dominant programming language.
//	Returns the language with the most files, excluding config/docs extensions.
//
// Inputs:
//   - counts: Map of extensions to file counts (from FileManifest.ExtensionCounts).
//
// Outputs:
//   - string: The detected language (e.g., "go", "python"), or "" if undetermined.
//
// Thread Safety: Safe for concurrent use (pure function).
func DetectProjectLanguage(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}

	// Skip non-code extensions when determining language
	skipExtensions := map[string]bool{
		".md": true, ".yaml": true, ".yml": true,
		".json": true, ".toml": true, ".sh": true,
		".txt": true, ".html": true, ".css": true,
	}

	var maxCount int
	var primaryLang string

	for ext, count := range counts {
		if skipExtensions[ext] {
			continue
		}

		lang, ok := extensionToLanguage[ext]
		if !ok {
			continue
		}

		if count > maxCount {
			maxCount = count
			primaryLang = lang
		}
	}

	return primaryLang
}

// GetBlockedFrameworks returns frameworks that shouldn't appear for a project language.
//
// Description:
//
//	Returns the set of framework/tool names that indicate hallucination
//	if mentioned in a response for the given project language.
//
// Inputs:
//   - projectLang: The project's primary language (e.g., "go", "python").
//
// Outputs:
//   - map[string]bool: Set of blocked framework names (lowercase).
//
// Thread Safety: Safe for concurrent use (returns reference to package-level map).
func GetBlockedFrameworks(projectLang string) map[string]bool {
	blocklist := frameworkBlocklists[strings.ToLower(projectLang)]
	if blocklist == nil {
		return make(map[string]bool)
	}
	return blocklist
}

// GetLanguageSuggestion provides a helpful suggestion for a wrong-language pattern.
//
// Description:
//
//	Returns a suggestion for what to use instead of a wrong-language pattern.
//	For example, if Flask is mentioned in a Go project, suggests using net/http.
//
// Inputs:
//   - projectLang: The correct project language.
//   - wrongPattern: The pattern that was incorrectly mentioned.
//
// Outputs:
//   - string: A helpful suggestion, or "" if no specific suggestion available.
//
// Thread Safety: Safe for concurrent use (pure function).
func GetLanguageSuggestion(projectLang, wrongPattern string) string {
	suggestions := languageSuggestions[strings.ToLower(projectLang)]
	if suggestions == nil {
		return ""
	}

	// Try exact match first
	if suggestion, ok := suggestions[strings.ToLower(wrongPattern)]; ok {
		return suggestion
	}

	// Try partial match
	lowerPattern := strings.ToLower(wrongPattern)
	for pattern, suggestion := range suggestions {
		if strings.Contains(lowerPattern, pattern) {
			return suggestion
		}
	}

	return ""
}

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
//	Also checks for blocked frameworks - language-specific tools/frameworks
//	that should never appear in responses about the project language.
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

	responseLower := strings.ToLower(response)

	// Check blocked frameworks first (fast O(1) lookups)
	blockedFrameworks := GetBlockedFrameworks(projectLang)
	for framework := range blockedFrameworks {
		select {
		case <-ctx.Done():
			return violations
		default:
		}

		if strings.Contains(responseLower, framework) {
			// Get language-specific suggestion
			suggestion := GetLanguageSuggestion(projectLang, framework)
			if suggestion == "" {
				suggestion = fmt.Sprintf("This appears to be a %s project. Avoid references to %s.", projectLang, framework)
			}

			violations = append(violations, Violation{
				Type:       ViolationLanguageConfusion,
				Severity:   SeverityCritical,
				Code:       "LANGUAGE_CONFUSION_FRAMEWORK",
				Message:    fmt.Sprintf("Response mentions '%s' in a %s project", framework, projectLang),
				Evidence:   framework,
				Expected:   fmt.Sprintf("%s frameworks and tools only", projectLang),
				Suggestion: suggestion,
			})
		}
	}

	// Check language-specific patterns
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
		var primaryMarker string

		for _, marker := range markers {
			select {
			case <-ctx.Done():
				return violations
			default:
			}

			if marker.Pattern.MatchString(response) {
				totalWeight += marker.Weight
				matchedMarkers = append(matchedMarkers, marker.Description)
				if primaryMarker == "" || marker.Weight > 0.9 {
					primaryMarker = marker.Description
				}
			}
		}

		// If accumulated weight exceeds threshold, it's a hallucination
		if totalWeight >= c.weightThreshold {
			// Get suggestion based on the primary marker
			suggestion := GetLanguageSuggestion(projectLang, primaryMarker)
			if suggestion == "" {
				suggestion = fmt.Sprintf("This is a %s project. Use %s idioms and patterns instead of %s.",
					projectLang, projectLang, lang)
			}

			violations = append(violations, Violation{
				Type:     ViolationLanguageConfusion,
				Severity: SeverityCritical,
				Code:     "LANGUAGE_CONFUSION_" + strings.ToUpper(lang),
				Message: fmt.Sprintf("Response contains %s patterns in %s project: %s",
					lang, projectLang, strings.Join(matchedMarkers, ", ")),
				Evidence:   strings.Join(matchedMarkers, ", "),
				Expected:   projectLang + " patterns only",
				Suggestion: suggestion,
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
