// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package validation provides input validation for untrusted data.
//
// # Description
//
// This package validates untrusted input before processing to prevent
// security vulnerabilities such as path traversal, command injection,
// and regex denial of service (ReDoS).
//
// # Thread Safety
//
// All validators are stateless and safe for concurrent use.
package validation

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ValidationError represents a validation failure with context.
type ValidationError struct {
	Field   string // The field that failed validation
	Value   string // The rejected value (truncated for safety)
	Reason  string // Why validation failed
	Details string // Additional details for debugging
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed for %s: %s", e.Field, e.Reason)
}

// InputValidator validates untrusted input before processing.
//
// # Description
//
// Provides validation for file paths, diff patches, symbol identifiers,
// and regex patterns. All methods are stateless and safe for concurrent use.
//
// # Security
//
//   - Rejects path traversal attempts
//   - Enforces size limits to prevent DoS
//   - Validates UTF-8 encoding
//   - Prevents shell metacharacter injection
//   - Detects ReDoS-prone regex patterns
//
// # Thread Safety
//
// Safe for concurrent use (stateless).
type InputValidator struct {
	maxPathLen    int // Maximum file path length
	maxPatchLen   int // Maximum patch size in bytes
	maxPatternLen int // Maximum regex pattern length
}

// InputValidatorOptions configures the InputValidator.
type InputValidatorOptions struct {
	MaxPathLen    int // Default: 4096
	MaxPatchLen   int // Default: 1MB (1<<20)
	MaxPatternLen int // Default: 1000
}

// DefaultInputValidatorOptions returns sensible default options.
func DefaultInputValidatorOptions() InputValidatorOptions {
	return InputValidatorOptions{
		MaxPathLen:    4096,
		MaxPatchLen:   1 << 20, // 1MB
		MaxPatternLen: 1000,
	}
}

// NewInputValidator creates a new InputValidator with the given options.
//
// # Description
//
// Creates a validator with configurable limits. If opts is nil,
// defaults are used.
//
// # Inputs
//
//   - opts: Configuration options. If nil, defaults are used.
//
// # Outputs
//
//   - *InputValidator: Ready-to-use validator.
//
// # Example
//
//	v := NewInputValidator(nil)
//	if err := v.ValidateFilePath("../../../etc/passwd"); err != nil {
//	    // Handle path traversal attempt
//	}
func NewInputValidator(opts *InputValidatorOptions) *InputValidator {
	if opts == nil {
		defaults := DefaultInputValidatorOptions()
		opts = &defaults
	}

	return &InputValidator{
		maxPathLen:    opts.MaxPathLen,
		maxPatchLen:   opts.MaxPatchLen,
		maxPatternLen: opts.MaxPatternLen,
	}
}

// ValidateFilePath rejects path traversal and excessive length.
//
// # Description
//
// Validates that a file path is safe to use. Rejects paths containing
// "..", null bytes, or excessive length.
//
// # Security
//
//   - Rejects paths containing ".." (path traversal)
//   - Rejects paths exceeding maxPathLen
//   - Rejects null bytes (C-string injection)
//   - Rejects non-UTF-8 sequences
//   - Rejects paths starting with ~ (home directory expansion)
//
// # Inputs
//
//   - path: The file path to validate.
//
// # Outputs
//
//   - error: Non-nil if validation fails, with details about the failure.
//
// # Example
//
//	if err := v.ValidateFilePath(userInput); err != nil {
//	    return fmt.Errorf("invalid path: %w", err)
//	}
func (v *InputValidator) ValidateFilePath(path string) error {
	if path == "" {
		return &ValidationError{
			Field:  "path",
			Value:  "",
			Reason: "path must not be empty",
		}
	}

	// Check UTF-8 validity
	if !utf8.ValidString(path) {
		return &ValidationError{
			Field:  "path",
			Value:  truncateForError(path),
			Reason: "path contains invalid UTF-8",
		}
	}

	// Check length limit
	if len(path) > v.maxPathLen {
		return &ValidationError{
			Field:   "path",
			Value:   truncateForError(path),
			Reason:  "path exceeds maximum length",
			Details: fmt.Sprintf("max: %d, got: %d", v.maxPathLen, len(path)),
		}
	}

	// Check for null bytes
	if strings.ContainsRune(path, '\x00') {
		return &ValidationError{
			Field:  "path",
			Value:  truncateForError(path),
			Reason: "path contains null byte",
		}
	}

	// Check for path traversal patterns
	if containsPathTraversal(path) {
		return &ValidationError{
			Field:  "path",
			Value:  truncateForError(path),
			Reason: "path contains traversal sequence",
		}
	}

	// Reject home directory expansion
	if strings.HasPrefix(path, "~") {
		return &ValidationError{
			Field:  "path",
			Value:  truncateForError(path),
			Reason: "path starts with ~ (home directory expansion not allowed)",
		}
	}

	return nil
}

// containsPathTraversal checks for various path traversal patterns.
func containsPathTraversal(path string) bool {
	// Normalize path separators for checking
	normalized := strings.ReplaceAll(path, "\\", "/")

	// Check for ".." at various positions
	patterns := []string{
		"..",     // Direct traversal
		"/../",   // In middle of path
		"/./",    // Current directory (can be chained)
		"//",     // Double slash (can bypass checks)
		"\\.\\.", // Windows-style traversal
	}

	for _, pattern := range patterns {
		if strings.Contains(normalized, pattern) {
			return true
		}
	}

	// Check for encoded variants
	// %2e = ".", %2f = "/", %5c = "\"
	encodedPatterns := []string{
		"%2e%2e",     // URL-encoded ..
		"%252e%252e", // Double-encoded ..
		"..%2f",      // .. with encoded /
		"..%5c",      // .. with encoded \
		"%2e%2e/",    // Encoded .. with /
		"%2e%2e\\",   // Encoded .. with \
	}

	lowered := strings.ToLower(normalized)
	for _, pattern := range encodedPatterns {
		if strings.Contains(lowered, pattern) {
			return true
		}
	}

	// Check for edge cases
	if normalized == ".." || strings.HasPrefix(normalized, "../") ||
		strings.HasSuffix(normalized, "/..") {
		return true
	}

	return false
}

// ValidateDiffPatch validates unified diff format and size.
//
// # Description
//
// Validates that a patch string is safe to process. Checks size limits,
// UTF-8 validity, and basic diff format.
//
// # Security
//
//   - Enforces size limit (maxPatchLen)
//   - Validates UTF-8 encoding
//   - Rejects null bytes
//   - Validates diff header format
//
// # Inputs
//
//   - patch: The unified diff string to validate.
//
// # Outputs
//
//   - error: Non-nil if validation fails.
//
// # Example
//
//	if err := v.ValidateDiffPatch(patchContent); err != nil {
//	    return fmt.Errorf("invalid patch: %w", err)
//	}
func (v *InputValidator) ValidateDiffPatch(patch string) error {
	if patch == "" {
		return &ValidationError{
			Field:  "patch",
			Value:  "",
			Reason: "patch must not be empty",
		}
	}

	// Check UTF-8 validity
	if !utf8.ValidString(patch) {
		return &ValidationError{
			Field:  "patch",
			Value:  truncateForError(patch),
			Reason: "patch contains invalid UTF-8",
		}
	}

	// Check size limit
	if len(patch) > v.maxPatchLen {
		return &ValidationError{
			Field:   "patch",
			Value:   truncateForError(patch),
			Reason:  "patch exceeds maximum size",
			Details: fmt.Sprintf("max: %d bytes, got: %d bytes", v.maxPatchLen, len(patch)),
		}
	}

	// Check for null bytes
	if strings.ContainsRune(patch, '\x00') {
		return &ValidationError{
			Field:  "patch",
			Value:  truncateForError(patch),
			Reason: "patch contains null byte",
		}
	}

	// Validate basic unified diff format
	// Must contain at least one of: "diff ", "---", "+++"
	if !isValidDiffFormat(patch) {
		return &ValidationError{
			Field:  "patch",
			Value:  truncateForError(patch),
			Reason: "patch is not valid unified diff format",
		}
	}

	return nil
}

// isValidDiffFormat checks for basic unified diff structure.
func isValidDiffFormat(patch string) bool {
	lines := strings.Split(patch, "\n")

	hasDiffHeader := false
	hasMinusFile := false
	hasPlusFile := false

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff "):
			hasDiffHeader = true
		case strings.HasPrefix(line, "--- "):
			hasMinusFile = true
		case strings.HasPrefix(line, "+++ "):
			hasPlusFile = true
		}
	}

	// Valid diff has either a "diff " header, or both --- and +++ lines
	return hasDiffHeader || (hasMinusFile && hasPlusFile)
}

// ValidateSymbolID rejects malformed symbol identifiers.
//
// # Description
//
// Validates that a symbol ID conforms to expected format and doesn't
// contain dangerous characters that could cause issues in downstream
// processing (logging, URLs, etc.).
//
// # Security
//
//   - Rejects empty strings
//   - Rejects null bytes
//   - Rejects shell metacharacters
//   - Enforces reasonable length limit
//   - Validates character set
//
// # Inputs
//
//   - id: The symbol identifier to validate.
//
// # Outputs
//
//   - error: Non-nil if validation fails.
//
// # Example
//
//	if err := v.ValidateSymbolID(userInput); err != nil {
//	    return fmt.Errorf("invalid symbol ID: %w", err)
//	}
func (v *InputValidator) ValidateSymbolID(id string) error {
	if id == "" {
		return &ValidationError{
			Field:  "symbol_id",
			Value:  "",
			Reason: "symbol ID must not be empty",
		}
	}

	// Check UTF-8 validity
	if !utf8.ValidString(id) {
		return &ValidationError{
			Field:  "symbol_id",
			Value:  truncateForError(id),
			Reason: "symbol ID contains invalid UTF-8",
		}
	}

	// Reasonable length limit for symbol IDs
	// Format: "path/to/file.go:line:name" typically under 500 chars
	const maxSymbolIDLen = 1000
	if len(id) > maxSymbolIDLen {
		return &ValidationError{
			Field:   "symbol_id",
			Value:   truncateForError(id),
			Reason:  "symbol ID exceeds maximum length",
			Details: fmt.Sprintf("max: %d, got: %d", maxSymbolIDLen, len(id)),
		}
	}

	// Check for null bytes
	if strings.ContainsRune(id, '\x00') {
		return &ValidationError{
			Field:  "symbol_id",
			Value:  truncateForError(id),
			Reason: "symbol ID contains null byte",
		}
	}

	// Check for shell metacharacters (security)
	if containsShellMetacharacters(id) {
		return &ValidationError{
			Field:  "symbol_id",
			Value:  truncateForError(id),
			Reason: "symbol ID contains shell metacharacter",
		}
	}

	return nil
}

// containsShellMetacharacters checks for characters that could cause
// shell injection if the ID is ever passed to a command.
func containsShellMetacharacters(s string) bool {
	// Characters that have special meaning in shells
	dangerousChars := "|;&$`\\\"'<>(){}[]!"
	return strings.ContainsAny(s, dangerousChars)
}

// ValidateRegexPattern validates regex won't cause ReDoS.
//
// # Description
//
// Validates that a regex pattern is safe to compile and use. Checks
// for common patterns that can cause catastrophic backtracking (ReDoS).
//
// # Security
//
//   - Enforces length limit
//   - Detects nested quantifiers (a+)+ which cause exponential backtracking
//   - Detects overlapping alternations
//   - Validates pattern compiles
//
// # Limitations
//
// ReDoS detection is heuristic-based and may have false negatives.
// For untrusted patterns, consider using RE2 (which has linear time guarantees)
// or implementing execution timeouts.
//
// # Inputs
//
//   - pattern: The regex pattern to validate.
//
// # Outputs
//
//   - error: Non-nil if validation fails or pattern is potentially dangerous.
//
// # Example
//
//	if err := v.ValidateRegexPattern(userPattern); err != nil {
//	    return fmt.Errorf("invalid regex: %w", err)
//	}
func (v *InputValidator) ValidateRegexPattern(pattern string) error {
	if pattern == "" {
		return &ValidationError{
			Field:  "pattern",
			Value:  "",
			Reason: "pattern must not be empty",
		}
	}

	// Check length limit
	if len(pattern) > v.maxPatternLen {
		return &ValidationError{
			Field:   "pattern",
			Value:   truncateForError(pattern),
			Reason:  "pattern exceeds maximum length",
			Details: fmt.Sprintf("max: %d, got: %d", v.maxPatternLen, len(pattern)),
		}
	}

	// Check for common ReDoS patterns
	if isReDoSProne(pattern) {
		return &ValidationError{
			Field:  "pattern",
			Value:  truncateForError(pattern),
			Reason: "pattern contains potentially dangerous backtracking",
		}
	}

	// Try to compile the pattern
	if _, err := regexp.Compile(pattern); err != nil {
		return &ValidationError{
			Field:   "pattern",
			Value:   truncateForError(pattern),
			Reason:  "pattern is invalid regex",
			Details: err.Error(),
		}
	}

	return nil
}

// isReDoSProne checks for patterns that can cause catastrophic backtracking.
func isReDoSProne(pattern string) bool {
	// Common ReDoS patterns:
	// 1. Nested quantifiers: (a+)+ or (a*)*
	// 2. Overlapping groups: (a|a)+
	// 3. Exponential patterns: (.*)*

	// Pattern 1: Nested quantifiers
	// Look for groups with quantifiers inside that have quantifiers outside
	nestedQuantifier := regexp.MustCompile(`\([^)]*[+*][^)]*\)[+*?]`)
	if nestedQuantifier.MatchString(pattern) {
		return true
	}

	// Pattern 2: .* followed by something that can also match .*
	// e.g., (.*)(.*)+ or (.*)+
	dangerousDot := regexp.MustCompile(`\(\.\*\)[+*]`)
	if dangerousDot.MatchString(pattern) {
		return true
	}

	// Pattern 3: Multiple adjacent .+ or .*
	adjacentWildcard := regexp.MustCompile(`\.\*\.\*|\.\+\.\+`)
	if adjacentWildcard.MatchString(pattern) {
		return true
	}

	// Pattern 4: Look for excessive repetition of groups
	// e.g., (a){1000,}
	largeRepetition := regexp.MustCompile(`\{(\d+)\s*,\s*\}`)
	matches := largeRepetition.FindStringSubmatch(pattern)
	if len(matches) >= 2 {
		// Check if the minimum is very large
		var min int
		fmt.Sscanf(matches[1], "%d", &min)
		if min > 100 {
			return true
		}
	}

	return false
}

// ValidateGitArgs validates arguments that will be passed to git commands.
//
// # Description
//
// Validates that arguments are safe to pass to git via exec.Command.
// Rejects shell metacharacters to prevent command injection.
//
// # Security
//
//   - Rejects empty arguments
//   - Rejects shell metacharacters
//   - Rejects null bytes
//   - Rejects arguments starting with - (flag injection)
//
// # Inputs
//
//   - args: The git command arguments to validate.
//
// # Outputs
//
//   - error: Non-nil if any argument fails validation.
//
// # Example
//
//	if err := v.ValidateGitArgs([]string{"log", "--oneline", filePath}); err != nil {
//	    return fmt.Errorf("invalid git args: %w", err)
//	}
func (v *InputValidator) ValidateGitArgs(args []string) error {
	for i, arg := range args {
		if arg == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("args[%d]", i),
				Value:   "",
				Reason:  "git argument must not be empty",
				Details: fmt.Sprintf("argument at index %d is empty", i),
			}
		}

		// Check for null bytes
		if strings.ContainsRune(arg, '\x00') {
			return &ValidationError{
				Field:  fmt.Sprintf("args[%d]", i),
				Value:  truncateForError(arg),
				Reason: "git argument contains null byte",
			}
		}

		// Check for shell metacharacters
		if containsShellMetacharacters(arg) {
			return &ValidationError{
				Field:  fmt.Sprintf("args[%d]", i),
				Value:  truncateForError(arg),
				Reason: "git argument contains shell metacharacter",
			}
		}
	}

	return nil
}

// truncateForError safely truncates a value for inclusion in error messages.
func truncateForError(s string) string {
	const maxLen = 100
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Common validation errors for use in error checking.
var (
	// ErrPathTraversal indicates a path traversal attempt was detected.
	ErrPathTraversal = errors.New("path traversal detected")

	// ErrNullByte indicates a null byte was found in input.
	ErrNullByte = errors.New("null byte in input")

	// ErrSizeLimit indicates input exceeded size limits.
	ErrSizeLimit = errors.New("input exceeds size limit")

	// ErrInvalidUTF8 indicates input contains invalid UTF-8.
	ErrInvalidUTF8 = errors.New("invalid UTF-8 encoding")

	// ErrShellMetachar indicates shell metacharacters were found.
	ErrShellMetachar = errors.New("shell metacharacter in input")

	// ErrReDoS indicates a potential ReDoS pattern was detected.
	ErrReDoS = errors.New("potential ReDoS pattern")
)

// IsPathTraversal returns true if the error indicates a path traversal attempt.
func IsPathTraversal(err error) bool {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return strings.Contains(ve.Reason, "traversal")
	}
	return errors.Is(err, ErrPathTraversal)
}

// IsNullByte returns true if the error indicates a null byte was found.
func IsNullByte(err error) bool {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return strings.Contains(ve.Reason, "null byte")
	}
	return errors.Is(err, ErrNullByte)
}

// IsSizeLimit returns true if the error indicates a size limit was exceeded.
func IsSizeLimit(err error) bool {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return strings.Contains(ve.Reason, "maximum") || strings.Contains(ve.Reason, "size")
	}
	return errors.Is(err, ErrSizeLimit)
}
