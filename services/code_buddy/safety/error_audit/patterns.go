// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package error_audit

import (
	"regexp"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/safety"
)

// SecurityFunctions are function name patterns that are security-sensitive.
// Fail-open in these functions is CRITICAL.
var SecurityFunctions = []string{
	// Authentication
	"checkAuth", "validateAuth", "authenticate", "verifyAuth",
	"ValidateToken", "VerifyToken", "CheckToken", "ParseToken",
	"login", "signin", "signIn", "Login", "SignIn",
	"verifyPassword", "checkPassword", "ValidatePassword",

	// Authorization
	"authorize", "Authorize", "checkPermission", "CheckPermission",
	"hasPermission", "HasPermission", "isAllowed", "IsAllowed",
	"checkAccess", "CheckAccess", "verifyAccess", "VerifyAccess",
	"canAccess", "CanAccess", "isAuthorized", "IsAuthorized",

	// Validation
	"validate", "Validate", "sanitize", "Sanitize",
	"verify", "Verify", "check", "Check",

	// Security middleware
	"authMiddleware", "AuthMiddleware", "requireAuth", "RequireAuth",
	"ensureAuth", "EnsureAuth", "mustAuth", "MustAuth",
}

// InfoLeakPattern defines a pattern for detecting information leaks.
type InfoLeakPattern struct {
	// Type categorizes the leak (stack_trace, internal_path, db_error, etc.)
	Type string

	// Pattern is the regex to match.
	Pattern string

	// CompiledPattern is the compiled regex.
	compiledPattern *regexp.Regexp

	// Severity indicates how serious this leak is.
	Severity safety.Severity

	// Description explains the risk.
	Description string

	// CWE is the Common Weakness Enumeration ID.
	CWE string
}

// Match checks if content matches this pattern.
func (p *InfoLeakPattern) Match(content string) [][]int {
	if p.compiledPattern == nil {
		p.compiledPattern = regexp.MustCompile(p.Pattern)
	}
	return p.compiledPattern.FindAllStringIndex(content, -1)
}

// DefaultInfoLeakPatterns contains patterns for detecting information leaks.
var DefaultInfoLeakPatterns = []*InfoLeakPattern{
	// Stack traces in responses
	{
		Type:        "stack_trace",
		Pattern:     `(?:runtime/debug\.Stack|debug\.PrintStack|traceback\.format_exc|traceback\.print_exc)`,
		Severity:    safety.SeverityHigh,
		Description: "Stack trace exposed to users",
		CWE:         "CWE-209",
	},
	{
		Type:        "stack_trace",
		Pattern:     `(?:\.stack|Error\.stack|err\.stack)\s*[,)]`,
		Severity:    safety.SeverityMedium,
		Description: "Error stack property exposed",
		CWE:         "CWE-209",
	},

	// Internal paths revealed
	{
		Type:        "internal_path",
		Pattern:     `(?:"/home/|"/var/|"/usr/|"/opt/|"C:\\|"/root/)`,
		Severity:    safety.SeverityLow,
		Description: "Internal file path exposed in string",
		CWE:         "CWE-200",
	},

	// Database errors exposed
	{
		Type:        "db_error",
		Pattern:     `(?:pq:|mysql:|sqlite3:|SQLSTATE|ORA-\d+).*(?:Write|Response|Send|json)`,
		Severity:    safety.SeverityHigh,
		Description: "Database error message exposed to user",
		CWE:         "CWE-209",
	},

	// Configuration details
	{
		Type:        "config_detail",
		Pattern:     `(?:connection.*refused|ECONNREFUSED|timeout|host.*not.*found).*(?:Write|Response|Send)`,
		Severity:    safety.SeverityMedium,
		Description: "Infrastructure error exposed to user",
		CWE:         "CWE-200",
	},

	// Verbose error messages
	{
		Type:        "verbose_error",
		Pattern:     `(?:Write|Response|Send|json|fmt\.Fprint).*(?:err\.Error\(\)|error\.Error\(\))`,
		Severity:    safety.SeverityMedium,
		Description: "Full error message sent to client",
		CWE:         "CWE-209",
	},

	// Debug information
	{
		Type:        "debug_info",
		Pattern:     `(?:debug|Debug|DEBUG).*(?:true|True|TRUE|enabled|Enabled).*(?:prod|Prod|PROD|production|Production)`,
		Severity:    safety.SeverityHigh,
		Description: "Debug mode enabled in production context",
		CWE:         "CWE-489",
	},

	// Sensitive field names in errors
	{
		Type:        "sensitive_field",
		Pattern:     `(?:password|secret|token|key|credential|auth).*(?:invalid|incorrect|wrong|failed).*(?:Write|Response|Send)`,
		Severity:    safety.SeverityMedium,
		Description: "Sensitive field name in error response",
		CWE:         "CWE-209",
	},
}

// FailOpenPattern defines patterns for detecting fail-open error handling.
type FailOpenPattern struct {
	// Language is the programming language.
	Language string

	// ErrorCheckPattern matches error check statements.
	ErrorCheckPattern string

	// compiledErrorCheck is the compiled regex.
	compiledErrorCheck *regexp.Regexp

	// ReturnPattern matches return statements.
	ReturnPattern string

	// compiledReturn is the compiled regex.
	compiledReturn *regexp.Regexp

	// PanicPattern matches panic/raise statements.
	PanicPattern string

	// compiledPanic is the compiled regex.
	compiledPanic *regexp.Regexp
}

// DefaultFailOpenPatterns contains language-specific fail-open patterns.
var DefaultFailOpenPatterns = map[string]*FailOpenPattern{
	"go": {
		Language:          "go",
		ErrorCheckPattern: `if\s+(?:\w+\s*[:=].*)?(?:err\w*)\s*!=\s*nil\s*\{`,
		ReturnPattern:     `\breturn\b`,
		PanicPattern:      `\bpanic\s*\(`,
	},
	"python": {
		Language:          "python",
		ErrorCheckPattern: `except\s+(?:\w+(?:\s*,\s*\w+)*)?(?:\s+as\s+\w+)?\s*:`,
		ReturnPattern:     `\breturn\b|\braise\b`,
		PanicPattern:      `\braise\b`,
	},
	"typescript": {
		Language:          "typescript",
		ErrorCheckPattern: `catch\s*\(\s*\w*\s*\)\s*\{`,
		ReturnPattern:     `\breturn\b|\bthrow\b`,
		PanicPattern:      `\bthrow\b`,
	},
	"java": {
		Language:          "java",
		ErrorCheckPattern: `catch\s*\(\s*\w+\s+\w+\s*\)\s*\{`,
		ReturnPattern:     `\breturn\b|\bthrow\b`,
		PanicPattern:      `\bthrow\b`,
	},
}

// CompilePatterns compiles all regex patterns.
func (p *FailOpenPattern) CompilePatterns() {
	if p.compiledErrorCheck == nil && p.ErrorCheckPattern != "" {
		p.compiledErrorCheck = regexp.MustCompile(p.ErrorCheckPattern)
	}
	if p.compiledReturn == nil && p.ReturnPattern != "" {
		p.compiledReturn = regexp.MustCompile(p.ReturnPattern)
	}
	if p.compiledPanic == nil && p.PanicPattern != "" {
		p.compiledPanic = regexp.MustCompile(p.PanicPattern)
	}
}

// FindErrorChecks finds all error check blocks in content.
func (p *FailOpenPattern) FindErrorChecks(content string) []ErrorCheckBlock {
	p.CompilePatterns()

	if p.compiledErrorCheck == nil {
		return nil
	}

	matches := p.compiledErrorCheck.FindAllStringIndex(content, -1)
	var blocks []ErrorCheckBlock

	for _, match := range matches {
		// Find the block end (matching brace)
		blockEnd := findBlockEnd(content, match[1])
		if blockEnd < 0 {
			continue
		}

		blockContent := content[match[0]:blockEnd]
		lineNum := strings.Count(content[:match[0]], "\n") + 1

		// Check if block has return/panic
		hasReturn := p.compiledReturn != nil && p.compiledReturn.MatchString(blockContent)
		hasPanic := p.compiledPanic != nil && p.compiledPanic.MatchString(blockContent)

		blocks = append(blocks, ErrorCheckBlock{
			Start:      match[0],
			End:        blockEnd,
			Line:       lineNum,
			Content:    blockContent,
			HasReturn:  hasReturn,
			HasPanic:   hasPanic,
			IsFailOpen: !hasReturn && !hasPanic,
		})
	}

	return blocks
}

// ErrorCheckBlock represents an error handling block.
type ErrorCheckBlock struct {
	Start      int
	End        int
	Line       int
	Content    string
	HasReturn  bool
	HasPanic   bool
	IsFailOpen bool
}

// findBlockEnd finds the end of a block starting at startIdx.
func findBlockEnd(content string, startIdx int) int {
	depth := 1
	i := startIdx

	for i < len(content) && depth > 0 {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
		}
		i++
	}

	if depth == 0 {
		return i
	}
	return -1
}

// IsSecurityFunction checks if a function name is security-sensitive.
func IsSecurityFunction(funcName string) bool {
	nameLower := strings.ToLower(funcName)

	for _, pattern := range SecurityFunctions {
		if strings.Contains(nameLower, strings.ToLower(pattern)) {
			return true
		}
	}

	return false
}

// ErrorSwallowPattern matches empty error handling blocks.
var ErrorSwallowPatterns = map[string]*regexp.Regexp{
	"go":         regexp.MustCompile(`if\s+err\s*!=\s*nil\s*\{\s*\}`),
	"python":     regexp.MustCompile(`except.*:\s*pass\s*$`),
	"typescript": regexp.MustCompile(`catch\s*\([^)]*\)\s*\{\s*\}`),
	"java":       regexp.MustCompile(`catch\s*\([^)]*\)\s*\{\s*\}`),
}

// FindSwallowedErrors finds empty error handling blocks.
func FindSwallowedErrors(content, language string) [][]int {
	pattern, ok := ErrorSwallowPatterns[language]
	if !ok {
		return nil
	}
	return pattern.FindAllStringIndex(content, -1)
}
