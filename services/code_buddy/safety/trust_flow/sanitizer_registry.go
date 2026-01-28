// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package trust_flow provides trust flow analysis for security scanning.
//
// # Description
//
// This package implements taint tracking and trust flow analysis to detect
// vulnerabilities where untrusted data reaches sensitive sinks without
// proper sanitization. It builds on top of the explore package's data flow
// tracing with security-specific enhancements.
//
// # Thread Safety
//
// All types in this package are safe for concurrent use after initialization.
package trust_flow

import (
	"regexp"
	"strings"
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
)

// SanitizerPattern defines a pattern for identifying sanitization functions.
//
// Description:
//
//	SanitizerPattern matches functions that make untrusted data safe for
//	specific sink categories. Some sanitizers are complete (fully safe),
//	while others require additional context to be effective.
//
// Thread Safety:
//
//	SanitizerPattern is safe for concurrent use after initialization.
//	Uses sync.Once for thread-safe lazy regex compilation.
type SanitizerPattern struct {
	// Category is the sanitizer category (sql, xss, path, validation, etc.).
	Category string

	// Language is the programming language this pattern applies to.
	Language string

	// FunctionName is a pattern for the function/method name.
	FunctionName string

	// Receiver is a pattern for the receiver type (for methods).
	Receiver string

	// Package is a pattern for the package containing the function.
	Package string

	// MakesSafeFor lists sink categories this sanitizer protects against.
	MakesSafeFor []string

	// RequiresContext specifies additional validation requirements.
	// If nil, the sanitizer is complete on its own.
	RequiresContext *SanitizerContext

	// Description explains what this sanitizer does.
	Description string

	// Confidence is the base confidence level for matches (0.0-1.0).
	Confidence float64

	// nameRegex is the compiled regex for function name matching.
	nameRegex *regexp.Regexp

	// nameRegexOnce ensures thread-safe lazy compilation.
	nameRegexOnce sync.Once
}

// SanitizerContext specifies requirements for a sanitizer to be effective.
//
// Description:
//
//	Some sanitizers are not complete on their own. For example, filepath.Clean
//	does not prevent path traversal by itself - it must be combined with
//	base directory validation. SanitizerContext captures these requirements.
type SanitizerContext struct {
	// AdditionalCheck describes what else must be verified.
	AdditionalCheck string `json:"additional_check,omitempty"`

	// MustFollowWith is a pattern that must follow the sanitizer call.
	MustFollowWith string `json:"must_follow_with,omitempty"`

	// MustPrecedeWith is a pattern that must precede the sanitizer call.
	MustPrecedeWith string `json:"must_precede_with,omitempty"`

	// EffectiveOnlyWhen describes conditions for effectiveness.
	EffectiveOnlyWhen string `json:"effective_only_when,omitempty"`
}

// SanitizerRegistry holds patterns for identifying sanitization functions.
//
// Thread Safety:
//
//	SanitizerRegistry is safe for concurrent read access after initialization.
//	Do not modify patterns after construction.
type SanitizerRegistry struct {
	patterns map[string][]SanitizerPattern
}

// NewSanitizerRegistry creates a new SanitizerRegistry with default patterns.
func NewSanitizerRegistry() *SanitizerRegistry {
	r := &SanitizerRegistry{
		patterns: make(map[string][]SanitizerPattern),
	}

	r.patterns["go"] = DefaultGoSanitizerPatterns()
	r.patterns["python"] = DefaultPythonSanitizerPatterns()
	r.patterns["typescript"] = DefaultTypeScriptSanitizerPatterns()
	r.patterns["javascript"] = DefaultTypeScriptSanitizerPatterns()

	return r
}

// GetPatterns returns sanitizer patterns for a language.
func (r *SanitizerRegistry) GetPatterns(language string) []SanitizerPattern {
	return r.patterns[strings.ToLower(language)]
}

// RegisterPatterns adds patterns for a language.
func (r *SanitizerRegistry) RegisterPatterns(language string, patterns []SanitizerPattern) {
	r.patterns[strings.ToLower(language)] = patterns
}

// MatchSanitizer checks if a symbol matches any sanitizer pattern.
//
// Description:
//
//	Returns the matching pattern and categories it sanitizes for.
//	Also returns whether the sanitizer requires additional context.
//
// Outputs:
//
//	*SanitizerPattern - The matched pattern, or nil if no match.
//	bool - True if a match was found.
func (r *SanitizerRegistry) MatchSanitizer(sym *ast.Symbol) (*SanitizerPattern, bool) {
	if sym == nil {
		return nil, false
	}

	patterns := r.GetPatterns(sym.Language)
	for i := range patterns {
		if patterns[i].Match(sym) {
			return &patterns[i], true
		}
	}
	return nil, false
}

// Match checks if a symbol matches this sanitizer pattern.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (p *SanitizerPattern) Match(sym *ast.Symbol) bool {
	if sym == nil {
		return false
	}

	// Match function name with thread-safe lazy compilation
	if p.FunctionName != "" && !p.matchFunctionName(sym.Name) {
		return false
	}

	// Match receiver (no caching needed - simple patterns)
	if p.Receiver != "" && !matchPatternSimple(p.Receiver, sym.Receiver) {
		return false
	}

	// Match package (no caching needed - simple patterns)
	if p.Package != "" && !matchPatternSimple(p.Package, sym.Package) {
		return false
	}

	return true
}

// matchFunctionName matches the function name using thread-safe cached regex.
func (p *SanitizerPattern) matchFunctionName(name string) bool {
	if p.FunctionName == "*" {
		return true
	}

	if !strings.Contains(p.FunctionName, "*") {
		return p.FunctionName == name
	}

	// Lazy compile regex with sync.Once
	p.nameRegexOnce.Do(func() {
		regexPattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(p.FunctionName), "\\*", ".*") + "$"
		p.nameRegex, _ = regexp.Compile(regexPattern)
	})

	if p.nameRegex == nil {
		return false
	}
	return p.nameRegex.MatchString(name)
}

// IsComplete returns true if this sanitizer fully protects without context.
func (p *SanitizerPattern) IsComplete() bool {
	return p.RequiresContext == nil
}

// matchPatternSimple matches a pattern against a value, supporting glob patterns.
// This version does not cache compiled regex - use for patterns that are rarely matched.
func matchPatternSimple(pattern, value string) bool {
	if pattern == "*" {
		return true
	}

	if strings.Contains(pattern, "*") {
		regexPattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), "\\*", ".*") + "$"
		regex, err := regexp.Compile(regexPattern)
		if err != nil {
			return false
		}
		return regex.MatchString(value)
	}

	return pattern == value
}

// DefaultGoSanitizerPatterns returns default sanitizer patterns for Go.
func DefaultGoSanitizerPatterns() []SanitizerPattern {
	return []SanitizerPattern{
		// SQL sanitizers (complete - prepared statements)
		{
			Category:     "sql",
			FunctionName: "Prepare",
			Receiver:     "*sql.DB",
			MakesSafeFor: []string{"sql"},
			Description:  "Prepared statement prevents SQL injection",
			Confidence:   0.95,
		},
		{
			Category:     "sql",
			FunctionName: "NamedExec",
			Package:      "*sqlx*",
			MakesSafeFor: []string{"sql"},
			Description:  "Named parameters prevent SQL injection",
			Confidence:   0.95,
		},
		{
			Category:     "sql",
			FunctionName: "*",
			Package:      "*squirrel*",
			MakesSafeFor: []string{"sql"},
			Description:  "Query builder prevents SQL injection",
			Confidence:   0.9,
		},

		// XSS sanitizers (complete)
		{
			Category:     "xss",
			FunctionName: "EscapeString",
			Package:      "html",
			MakesSafeFor: []string{"xss"},
			Description:  "HTML escaping prevents XSS",
			Confidence:   0.95,
		},
		{
			Category:     "xss",
			FunctionName: "HTMLEscapeString",
			Package:      "*template*",
			MakesSafeFor: []string{"xss"},
			Description:  "Template escaping prevents XSS",
			Confidence:   0.95,
		},
		{
			Category:     "xss",
			FunctionName: "*",
			Package:      "*bluemonday*",
			MakesSafeFor: []string{"xss"},
			Description:  "Bluemonday HTML sanitizer prevents XSS",
			Confidence:   0.95,
		},

		// Path sanitizers (require context)
		{
			Category:     "path",
			FunctionName: "Clean",
			Package:      "filepath",
			MakesSafeFor: []string{"path"},
			RequiresContext: &SanitizerContext{
				AdditionalCheck:   "result must be verified to stay within base directory",
				MustFollowWith:    "strings.HasPrefix|filepath.Rel|filepath.Match",
				EffectiveOnlyWhen: "combined with base directory validation",
			},
			Description: "filepath.Clean normalizes but does NOT prevent traversal alone",
			Confidence:  0.5, // Lower confidence because incomplete
		},
		{
			Category:     "path",
			FunctionName: "Base",
			Package:      "filepath",
			MakesSafeFor: []string{"path"},
			RequiresContext: &SanitizerContext{
				EffectiveOnlyWhen: "only filename needed and directory traversal not possible",
			},
			Description: "filepath.Base strips directory but may not prevent all attacks",
			Confidence:  0.7,
		},
		{
			Category:     "path",
			FunctionName: "SecureJoin",
			Package:      "*securejoin*",
			MakesSafeFor: []string{"path"},
			Description:  "SecureJoin prevents path traversal",
			Confidence:   0.95,
		},

		// Validation sanitizers (complete for type conversion)
		{
			Category:     "validation",
			FunctionName: "Atoi",
			Package:      "strconv",
			MakesSafeFor: []string{"sql", "command", "path"},
			Description:  "Integer parsing ensures numeric input",
			Confidence:   0.95,
		},
		{
			Category:     "validation",
			FunctionName: "ParseInt",
			Package:      "strconv",
			MakesSafeFor: []string{"sql", "command", "path"},
			Description:  "Integer parsing ensures numeric input",
			Confidence:   0.95,
		},
		{
			Category:     "validation",
			FunctionName: "ParseFloat",
			Package:      "strconv",
			MakesSafeFor: []string{"sql", "command", "path"},
			Description:  "Float parsing ensures numeric input",
			Confidence:   0.95,
		},
		{
			Category:     "validation",
			FunctionName: "Parse",
			Package:      "*uuid*",
			MakesSafeFor: []string{"sql", "command", "path"},
			Description:  "UUID parsing ensures valid UUID format",
			Confidence:   0.95,
		},
		{
			Category:     "validation",
			FunctionName: "MatchString",
			Package:      "regexp",
			MakesSafeFor: []string{"sql", "command", "path", "xss"},
			RequiresContext: &SanitizerContext{
				EffectiveOnlyWhen: "pattern is a strict allowlist (not blacklist)",
			},
			Description: "Regex validation can sanitize if pattern is strict allowlist",
			Confidence:  0.7,
		},

		// URL sanitizers
		{
			Category:     "url",
			FunctionName: "Parse",
			Package:      "net/url",
			MakesSafeFor: []string{"ssrf"},
			RequiresContext: &SanitizerContext{
				MustFollowWith:    "Host check|scheme check|allowlist",
				EffectiveOnlyWhen: "URL host is validated against allowlist",
			},
			Description: "URL parsing alone doesn't prevent SSRF",
			Confidence:  0.5,
		},

		// Command sanitizers
		{
			Category:     "command",
			FunctionName: "QuoteArg",
			Package:      "*",
			MakesSafeFor: []string{"command"},
			Description:  "Argument quoting prevents command injection",
			Confidence:   0.9,
		},
	}
}

// DefaultPythonSanitizerPatterns returns default sanitizer patterns for Python.
func DefaultPythonSanitizerPatterns() []SanitizerPattern {
	return []SanitizerPattern{
		// SQL sanitizers
		{
			Category:     "sql",
			FunctionName: "execute",
			MakesSafeFor: []string{"sql"},
			RequiresContext: &SanitizerContext{
				EffectiveOnlyWhen: "using parameterized query (tuple as second arg)",
			},
			Description: "Parameterized queries prevent SQL injection",
			Confidence:  0.8,
		},

		// Command sanitizers
		{
			Category:     "command",
			FunctionName: "quote",
			Package:      "shlex",
			MakesSafeFor: []string{"command"},
			Description:  "shlex.quote escapes shell arguments",
			Confidence:   0.95,
		},
		{
			Category:     "command",
			FunctionName: "split",
			Package:      "shlex",
			MakesSafeFor: []string{"command"},
			Description:  "shlex.split safely parses shell commands",
			Confidence:   0.9,
		},

		// XSS sanitizers
		{
			Category:     "xss",
			FunctionName: "clean",
			Package:      "bleach",
			MakesSafeFor: []string{"xss"},
			Description:  "Bleach sanitizes HTML",
			Confidence:   0.95,
		},
		{
			Category:     "xss",
			FunctionName: "escape",
			Package:      "html",
			MakesSafeFor: []string{"xss"},
			Description:  "HTML escape prevents XSS",
			Confidence:   0.95,
		},
		{
			Category:     "xss",
			FunctionName: "escape",
			Package:      "markupsafe",
			MakesSafeFor: []string{"xss"},
			Description:  "Markupsafe escape prevents XSS",
			Confidence:   0.95,
		},

		// Path sanitizers
		{
			Category:     "path",
			FunctionName: "resolve",
			Package:      "pathlib",
			MakesSafeFor: []string{"path"},
			RequiresContext: &SanitizerContext{
				MustFollowWith:    "relative_to|is_relative_to",
				EffectiveOnlyWhen: "validated to be within base directory",
			},
			Description: "Path resolution alone doesn't prevent traversal",
			Confidence:  0.5,
		},
	}
}

// DefaultTypeScriptSanitizerPatterns returns default sanitizer patterns for TypeScript/JavaScript.
func DefaultTypeScriptSanitizerPatterns() []SanitizerPattern {
	return []SanitizerPattern{
		// XSS sanitizers
		{
			Category:     "xss",
			FunctionName: "sanitize",
			Package:      "*dompurify*",
			MakesSafeFor: []string{"xss"},
			Description:  "DOMPurify sanitizes HTML",
			Confidence:   0.95,
		},
		{
			Category:     "xss",
			FunctionName: "escape",
			Package:      "*lodash*",
			MakesSafeFor: []string{"xss"},
			Description:  "Lodash escape prevents XSS",
			Confidence:   0.9,
		},
		{
			Category:     "xss",
			FunctionName: "encodeURIComponent",
			MakesSafeFor: []string{"xss", "url"},
			Description:  "URL encoding prevents injection in URLs",
			Confidence:   0.9,
		},

		// SQL sanitizers
		{
			Category:     "sql",
			FunctionName: "escape",
			Package:      "*mysql*",
			MakesSafeFor: []string{"sql"},
			Description:  "MySQL escape prevents SQL injection",
			Confidence:   0.9,
		},

		// Path sanitizers
		{
			Category:     "path",
			FunctionName: "normalize",
			Package:      "path",
			MakesSafeFor: []string{"path"},
			RequiresContext: &SanitizerContext{
				MustFollowWith:    "startsWith|includes check",
				EffectiveOnlyWhen: "validated to be within base directory",
			},
			Description: "Path normalize alone doesn't prevent traversal",
			Confidence:  0.5,
		},
		{
			Category:     "path",
			FunctionName: "basename",
			Package:      "path",
			MakesSafeFor: []string{"path"},
			RequiresContext: &SanitizerContext{
				EffectiveOnlyWhen: "only filename needed",
			},
			Description: "Basename strips directory",
			Confidence:  0.7,
		},
	}
}

// CWEMapping maps sink categories to CWE IDs.
// Both "sql" and "database" map to SQL injection since both can be vulnerable.
var CWEMapping = map[string]string{
	"sql":                "CWE-89",   // SQL Injection
	"database":           "CWE-89",   // SQL Injection (database category from explore package)
	"command":            "CWE-78",   // OS Command Injection
	"xss":                "CWE-79",   // Cross-site Scripting
	"response":           "CWE-79",   // XSS via response (from explore package)
	"path":               "CWE-22",   // Path Traversal
	"file":               "CWE-22",   // Path Traversal (file category from explore package)
	"ssrf":               "CWE-918",  // Server-Side Request Forgery
	"network":            "CWE-918",  // SSRF (network category from explore package)
	"deserialize":        "CWE-502",  // Deserialization of Untrusted Data
	"log":                "CWE-532",  // Information Exposure Through Log Files
	"xxe":                "CWE-611",  // XXE
	"code_injection":     "CWE-94",   // Code Injection
	"template_injection": "CWE-1336", // Template Injection
}

// SeverityBySinkCategory maps sink categories to default severity.
var SeverityBySinkCategory = map[string]string{
	"sql":                "CRITICAL",
	"database":           "CRITICAL",
	"command":            "CRITICAL",
	"deserialize":        "CRITICAL",
	"code_injection":     "CRITICAL",
	"template_injection": "CRITICAL",
	"xss":                "HIGH",
	"response":           "HIGH",
	"path":               "HIGH",
	"file":               "HIGH",
	"ssrf":               "HIGH",
	"network":            "HIGH",
	"xxe":                "HIGH",
	"log":                "MEDIUM",
}
