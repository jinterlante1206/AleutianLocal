// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package lint

import (
	"strings"
	"sync"
)

// =============================================================================
// RULE POLICY
// =============================================================================

// RulePolicy defines how to handle specific linter rules.
//
// Description:
//
//	Rules are matched by prefix. For example, "errcheck" matches
//	"errcheck", "errcheck/assert", etc.
//
// Thread Safety: Treat as immutable after creation.
type RulePolicy struct {
	// BlockOn are rules that block patches (treat as errors).
	BlockOn []string

	// WarnOn are rules that warn but allow patches.
	WarnOn []string

	// Ignore are rules to completely ignore.
	Ignore []string
}

// ShouldBlock returns true if the rule should block patches.
//
// Description:
//
//	Checks if the rule matches any BlockOn patterns.
//	Matching is done by prefix.
//
// Inputs:
//
//	rule - The rule identifier from the linter
//
// Outputs:
//
//	bool - True if the rule should block
func (p *RulePolicy) ShouldBlock(rule string) bool {
	rule = strings.ToLower(rule)
	for _, pattern := range p.BlockOn {
		if matchesRule(rule, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// ShouldWarn returns true if the rule should produce a warning.
//
// Description:
//
//	Checks if the rule matches any WarnOn patterns.
//	Matching is done by prefix.
//
// Inputs:
//
//	rule - The rule identifier from the linter
//
// Outputs:
//
//	bool - True if the rule should warn
func (p *RulePolicy) ShouldWarn(rule string) bool {
	rule = strings.ToLower(rule)
	for _, pattern := range p.WarnOn {
		if matchesRule(rule, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// ShouldIgnore returns true if the rule should be ignored.
//
// Description:
//
//	Checks if the rule matches any Ignore patterns.
//	Matching is done by prefix.
//
// Inputs:
//
//	rule - The rule identifier from the linter
//
// Outputs:
//
//	bool - True if the rule should be ignored
func (p *RulePolicy) ShouldIgnore(rule string) bool {
	rule = strings.ToLower(rule)
	for _, pattern := range p.Ignore {
		if matchesRule(rule, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// GetSeverity returns the severity for a rule based on policy.
//
// Description:
//
//	Determines the severity based on which policy list the rule matches.
//	Ignore takes precedence, then BlockOn, then WarnOn.
//	Default is SeverityWarning.
//
// Inputs:
//
//	rule - The rule identifier from the linter
//
// Outputs:
//
//	Severity - The severity level for the rule
func (p *RulePolicy) GetSeverity(rule string) Severity {
	if p.ShouldIgnore(rule) {
		return SeverityInfo
	}
	if p.ShouldBlock(rule) {
		return SeverityError
	}
	if p.ShouldWarn(rule) {
		return SeverityWarning
	}
	// Default: treat as warning
	return SeverityWarning
}

// matchesRule checks if a rule matches a pattern.
// Pattern matching is by prefix or exact match.
// Examples:
//   - "errcheck" matches "errcheck"
//   - "SA1000" matches "SA" (prefix)
//   - "errcheck/assert" matches "errcheck" (hierarchy)
func matchesRule(rule, pattern string) bool {
	if rule == pattern {
		return true
	}
	// Check hierarchy match (e.g., "errcheck/assert" matches "errcheck")
	if strings.HasPrefix(rule, pattern+"/") {
		return true
	}
	// Check prefix match for codes like SA1000 matching SA
	// The pattern must be followed by a digit for this to match
	if strings.HasPrefix(rule, pattern) && len(rule) > len(pattern) {
		next := rule[len(pattern)]
		if next >= '0' && next <= '9' {
			return true
		}
	}
	return false
}

// =============================================================================
// DEFAULT POLICIES
// =============================================================================

// DefaultGoPolicy is the default policy for Go linting.
//
// Description:
//
//	Blocks on correctness and security issues that indicate bugs.
//	Warns on code quality issues that don't affect correctness.
//	Ignores pure style issues that are subjective.
var DefaultGoPolicy = RulePolicy{
	BlockOn: []string{
		// Error handling
		"errcheck",
		// Type safety
		"typecheck",
		// Static analysis
		"staticcheck",
		"SA", // staticcheck SA* rules
		// Security
		"gosec",
		"G", // gosec G* rules
		// Nil safety
		"nilness",
		"nilerr",
		// Race conditions
		"datarace",
	},
	WarnOn: []string{
		// Code quality
		"ineffassign",
		"unused",
		"deadcode",
		"govet",
		"shadow",
		// Performance
		"prealloc",
		"copylock",
		// Best practices
		"unconvert",
		"unparam",
	},
	Ignore: []string{
		// Style - subjective and auto-fixable
		"lll",
		"gofmt",
		"goimports",
		"whitespace",
		"wsl",
		// Complexity metrics - subjective
		"gocyclo",
		"gocognit",
		"funlen",
		// Naming - subjective
		"revive/var-naming",
		"stylecheck/ST1003",
	},
}

// DefaultPythonPolicy is the default policy for Python linting (Ruff).
//
// Description:
//
//	Ruff uses rule codes like E501, F401, etc.
//	E = pycodestyle errors
//	W = pycodestyle warnings
//	F = Pyflakes (unused imports, undefined names)
//	C = complexity
//	S = security (bandit)
var DefaultPythonPolicy = RulePolicy{
	BlockOn: []string{
		// Pyflakes - logic errors
		"F", // F401 (unused import), F811 (redefinition), etc.
		// Security
		"S", // bandit security rules
		// Type errors (if using mypy)
		"PGH", // pygrep-hooks
	},
	WarnOn: []string{
		// Style errors
		"E",
		// Warnings
		"W",
		// Complexity
		"C90", // mccabe complexity
		// Import order
		"I",
	},
	Ignore: []string{
		// Line length - often breaks diffs
		"E501",
		// Trailing whitespace - auto-fixable
		"W291",
		"W293",
		// Blank lines - auto-fixable
		"E302",
		"E303",
		// Documentation (optional)
		"D",
	},
}

// DefaultTSPolicy is the default policy for TypeScript/JavaScript linting.
//
// Description:
//
//	ESLint uses severity levels (0=off, 1=warn, 2=error).
//	We map ESLint severity directly, with overrides for specific rules.
var DefaultTSPolicy = RulePolicy{
	BlockOn: []string{
		// Type errors
		"@typescript-eslint/no-unsafe",
		"@typescript-eslint/no-explicit-any",
		// Correctness
		"no-undef",
		"no-unused-vars",
		// Security
		"no-eval",
		"no-implied-eval",
	},
	WarnOn: []string{
		// Best practices
		"eqeqeq",
		"no-console",
		"prefer-const",
		// Complexity
		"complexity",
	},
	Ignore: []string{
		// Formatting - handled by prettier
		"indent",
		"semi",
		"quotes",
		"comma-dangle",
		// Style preferences
		"max-len",
	},
}

// =============================================================================
// POLICY REGISTRY
// =============================================================================

// PolicyRegistry manages policies for different languages.
//
// Thread Safety: Safe for concurrent use after initialization.
type PolicyRegistry struct {
	mu       sync.RWMutex
	policies map[string]*RulePolicy
}

// NewPolicyRegistry creates a new registry with default policies.
func NewPolicyRegistry() *PolicyRegistry {
	r := &PolicyRegistry{
		policies: make(map[string]*RulePolicy),
	}
	r.registerDefaults()
	return r
}

// registerDefaults adds the default policies.
func (r *PolicyRegistry) registerDefaults() {
	r.policies["go"] = &DefaultGoPolicy
	r.policies["python"] = &DefaultPythonPolicy
	r.policies["typescript"] = &DefaultTSPolicy
	r.policies["javascript"] = &DefaultTSPolicy // Same as TS
}

// Get returns the policy for a language.
//
// Description:
//
//	Returns the policy for the given language.
//	Returns nil if no policy is registered for the language.
//
// Inputs:
//
//	language - The language identifier (e.g., "go", "python")
//
// Outputs:
//
//	*RulePolicy - The policy, or nil if not found
//
// Thread Safety: Safe for concurrent use.
func (r *PolicyRegistry) Get(language string) *RulePolicy {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.policies[language]
}

// Register adds or updates a policy for a language.
//
// Description:
//
//	Registers a custom policy for a language, overwriting any existing policy.
//
// Inputs:
//
//	language - The language identifier
//	policy - The policy to register
//
// Thread Safety: Safe for concurrent use.
func (r *PolicyRegistry) Register(language string, policy *RulePolicy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.policies[language] = policy
}

// Languages returns all registered language names.
//
// Thread Safety: Safe for concurrent use.
func (r *PolicyRegistry) Languages() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	langs := make([]string, 0, len(r.policies))
	for lang := range r.policies {
		langs = append(langs, lang)
	}
	return langs
}

// ApplyPolicy applies a policy to lint issues, setting appropriate severities.
//
// Description:
//
//	Takes raw lint issues from a linter and categorizes them based
//	on the policy into errors, warnings, and infos.
//
// Inputs:
//
//	issues - Raw issues from the linter
//	policy - The policy to apply
//
// Outputs:
//
//	errors - Issues that should block
//	warnings - Issues that should warn
//	infos - Issues that are informational
func ApplyPolicy(issues []LintIssue, policy *RulePolicy) (errors, warnings, infos []LintIssue) {
	if policy == nil {
		// No policy - treat all as warnings
		return nil, issues, nil
	}

	errors = make([]LintIssue, 0)
	warnings = make([]LintIssue, 0)
	infos = make([]LintIssue, 0)

	for _, issue := range issues {
		if policy.ShouldIgnore(issue.Rule) {
			continue // Skip ignored rules entirely
		}

		// Apply policy severity
		severity := policy.GetSeverity(issue.Rule)
		issue.Severity = severity

		switch severity {
		case SeverityError:
			errors = append(errors, issue)
		case SeverityWarning:
			warnings = append(warnings, issue)
		case SeverityInfo:
			infos = append(infos, issue)
		}
	}

	return errors, warnings, infos
}
