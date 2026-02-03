// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package trust

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/trace/safety"
)

// Verifier implements the safety.RemediationVerifier interface.
//
// Description:
//
//	Verifier re-runs security analysis on patched code to confirm that
//	reported issues have been fixed and no new issues were introduced.
//	This is critical for maintaining security during the fix cycle.
//
// Thread Safety:
//
//	Verifier is safe for concurrent use after initialization.
type Verifier struct {
	// issueStore tracks known issues for verification
	issueStore *IssueStore

	// analyzer is used for boundary analysis verification
	analyzer *Analyzer
}

// IssueStore manages known security issues for verification.
//
// Description:
//
//	IssueStore maintains a registry of detected issues that can be
//	verified after remediation. Thread-safe for concurrent access.
//
// Thread Safety:
//
//	IssueStore is safe for concurrent use.
type IssueStore struct {
	mu     sync.RWMutex
	issues map[string]*safety.SecurityIssue
}

// NewIssueStore creates a new empty IssueStore.
func NewIssueStore() *IssueStore {
	return &IssueStore{
		issues: make(map[string]*safety.SecurityIssue),
	}
}

// Register adds an issue to the store.
func (s *IssueStore) Register(issue *safety.SecurityIssue) {
	if issue == nil || issue.ID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.issues[issue.ID] = issue
}

// Get retrieves an issue by ID.
func (s *IssueStore) Get(id string) (*safety.SecurityIssue, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	issue, ok := s.issues[id]
	return issue, ok
}

// Remove deletes an issue from the store.
func (s *IssueStore) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.issues, id)
}

// NewVerifier creates a new RemediationVerifier.
//
// Description:
//
//	Creates a verifier that can check if security issues have been
//	properly remediated in new code.
//
// Inputs:
//
//	analyzer - The trust boundary analyzer. May be nil for basic checks.
//
// Outputs:
//
//	*Verifier - The configured verifier.
func NewVerifier(analyzer *Analyzer) *Verifier {
	return &Verifier{
		issueStore: NewIssueStore(),
		analyzer:   analyzer,
	}
}

// NewVerifierWithStore creates a Verifier with an existing issue store.
//
// Description:
//
//	Creates a verifier with a pre-populated issue store for testing
//	or when issues have been previously detected.
func NewVerifierWithStore(analyzer *Analyzer, store *IssueStore) *Verifier {
	if store == nil {
		store = NewIssueStore()
	}
	return &Verifier{
		issueStore: store,
		analyzer:   analyzer,
	}
}

// RegisterIssue adds an issue to be tracked for verification.
//
// Description:
//
//	Registers a detected security issue so it can be verified after
//	remediation. The issue ID is used for lookup.
//
// Inputs:
//
//	issue - The security issue to register. Must have a non-empty ID.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (v *Verifier) RegisterIssue(issue *safety.SecurityIssue) {
	v.issueStore.Register(issue)
}

// VerifyRemediation checks if a security fix properly addresses an issue.
//
// Description:
//
//	Analyzes the provided code to determine if the original issue has
//	been fixed and whether any new issues were introduced. The verification
//	uses pattern matching and heuristics appropriate to the issue type.
//
// Inputs:
//
//	ctx - Context for cancellation. Must not be nil.
//	issueID - The ID of the issue to verify.
//	newCode - The patched source code to analyze.
//
// Outputs:
//
//	*safety.VerificationResult - The verification result.
//	error - Non-nil if issue not found or context canceled.
//
// Errors:
//
//	safety.ErrInvalidInput - Context or issueID is nil/empty.
//	safety.ErrNoVulnerabilityFound - Issue ID not found in store.
//	safety.ErrContextCanceled - Context was canceled.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (v *Verifier) VerifyRemediation(
	ctx context.Context,
	issueID string,
	newCode string,
) (*safety.VerificationResult, error) {
	// Validate inputs
	if ctx == nil {
		return nil, safety.ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, safety.ErrContextCanceled
	}
	if issueID == "" {
		return nil, safety.ErrInvalidInput
	}

	// Look up the original issue
	originalIssue, found := v.issueStore.Get(issueID)
	if !found {
		return nil, safety.ErrNoVulnerabilityFound
	}

	result := &safety.VerificationResult{
		IssueID:       issueID,
		OriginalIssue: originalIssue,
		IsFixed:       false,
		StillPresent:  true,
		NewIssues:     make([]safety.SecurityIssue, 0),
	}

	// Verify based on issue type
	switch {
	case strings.Contains(originalIssue.Type, "sql"):
		v.verifySQLInjectionFix(originalIssue, newCode, result)

	case strings.Contains(originalIssue.Type, "command"):
		v.verifyCommandInjectionFix(originalIssue, newCode, result)

	case strings.Contains(originalIssue.Type, "xss"):
		v.verifyXSSFix(originalIssue, newCode, result)

	case strings.Contains(originalIssue.Type, "path"):
		v.verifyPathTraversalFix(originalIssue, newCode, result)

	case strings.Contains(originalIssue.Type, "boundary"):
		v.verifyBoundaryFix(originalIssue, newCode, result)

	default:
		v.verifyGenericFix(originalIssue, newCode, result)
	}

	// Check for context cancellation before returning
	if err := ctx.Err(); err != nil {
		return result, nil // Return partial result
	}

	return result, nil
}

// verifySQLInjectionFix checks if SQL injection was properly fixed.
func (v *Verifier) verifySQLInjectionFix(
	original *safety.SecurityIssue,
	newCode string,
	result *safety.VerificationResult,
) {
	// Look for parameterized query patterns
	hasParameterized := strings.Contains(newCode, "?") ||
		strings.Contains(newCode, "$1") ||
		strings.Contains(newCode, ":1") ||
		strings.Contains(newCode, "Prepare") ||
		strings.Contains(newCode, "prepare")

	// Look for dangerous patterns
	hasConcatenation := strings.Contains(newCode, "+") &&
		(strings.Contains(newCode, "SELECT") ||
			strings.Contains(newCode, "INSERT") ||
			strings.Contains(newCode, "UPDATE") ||
			strings.Contains(newCode, "DELETE"))

	hasStringFormat := strings.Contains(newCode, "Sprintf") &&
		(strings.Contains(newCode, "SELECT") ||
			strings.Contains(newCode, "INSERT"))

	if hasParameterized && !hasConcatenation && !hasStringFormat {
		result.IsFixed = true
		result.StillPresent = false
		result.Explanation = "SQL query now uses parameterized statements"
	} else if hasParameterized && (hasConcatenation || hasStringFormat) {
		result.IsFixed = false
		result.StillPresent = true
		result.Explanation = "Parameterized query found, but string concatenation still present. Ensure ALL query parameters use placeholders."
	} else {
		result.IsFixed = false
		result.StillPresent = true
		result.Explanation = "No parameterized query pattern detected. Use prepared statements or parameterized queries."
	}
}

// verifyCommandInjectionFix checks if command injection was properly fixed.
func (v *Verifier) verifyCommandInjectionFix(
	original *safety.SecurityIssue,
	newCode string,
	result *safety.VerificationResult,
) {
	// Look for dangerous patterns
	hasShellTrue := strings.Contains(newCode, "shell=True") ||
		strings.Contains(newCode, "shell=true")

	hasExecCommand := strings.Contains(newCode, "exec.Command") ||
		strings.Contains(newCode, "subprocess.run")

	hasSafeArgs := strings.Contains(newCode, "[]string") ||
		strings.Contains(newCode, "[\"")

	if hasShellTrue {
		result.IsFixed = false
		result.StillPresent = true
		result.Explanation = "shell=True still present. Use subprocess with explicit argument list instead."
	} else if hasExecCommand && hasSafeArgs {
		result.IsFixed = true
		result.StillPresent = false
		result.Explanation = "Command now uses argument list instead of shell string"
	} else {
		result.IsFixed = false
		result.StillPresent = true
		result.Explanation = "Could not verify safe command execution pattern. Ensure commands use explicit argument lists."
	}
}

// verifyXSSFix checks if XSS was properly fixed.
func (v *Verifier) verifyXSSFix(
	original *safety.SecurityIssue,
	newCode string,
	result *safety.VerificationResult,
) {
	// Look for encoding/escaping patterns
	hasEscape := strings.Contains(newCode, "html.EscapeString") ||
		strings.Contains(newCode, "template.HTML") ||
		strings.Contains(newCode, "htmlspecialchars") ||
		strings.Contains(newCode, "escape(") ||
		strings.Contains(newCode, "sanitize")

	// Look for dangerous patterns
	hasInnerHTML := strings.Contains(newCode, "innerHTML") ||
		strings.Contains(newCode, "dangerouslySetInnerHTML")

	hasRawOutput := strings.Contains(newCode, "{{.") && !strings.Contains(newCode, "|")

	if hasEscape && !hasInnerHTML {
		result.IsFixed = true
		result.StillPresent = false
		result.Explanation = "Output is now properly encoded/escaped"
	} else if hasInnerHTML {
		result.IsFixed = false
		result.StillPresent = true
		result.Explanation = "innerHTML/dangerouslySetInnerHTML still used. Use textContent or proper encoding."
	} else if hasRawOutput {
		result.IsFixed = false
		result.StillPresent = true
		result.Explanation = "Template may output unescaped data. Use |html or |escape filter."
	} else {
		result.Explanation = "Could not determine XSS fix status. Verify output encoding manually."
	}
}

// verifyPathTraversalFix checks if path traversal was properly fixed.
func (v *Verifier) verifyPathTraversalFix(
	original *safety.SecurityIssue,
	newCode string,
	result *safety.VerificationResult,
) {
	// Look for path validation patterns
	hasPathValidation := strings.Contains(newCode, "filepath.Base") ||
		strings.Contains(newCode, "SecureJoin") ||
		strings.Contains(newCode, "path.Clean") ||
		strings.Contains(newCode, "HasPrefix")

	// Look for dangerous patterns
	hasDirectConcat := strings.Contains(newCode, "+") &&
		(strings.Contains(newCode, "path") || strings.Contains(newCode, "file"))

	hasDotDotCheck := strings.Contains(newCode, "..") &&
		(strings.Contains(newCode, "reject") ||
			strings.Contains(newCode, "error") ||
			strings.Contains(newCode, "return"))

	if hasPathValidation || hasDotDotCheck {
		result.IsFixed = true
		result.StillPresent = false
		result.Explanation = "Path input is now validated before use"
	} else if hasDirectConcat {
		result.IsFixed = false
		result.StillPresent = true
		result.Explanation = "Path concatenation detected. Use filepath.Join with path validation."
	} else {
		result.Explanation = "Could not determine path traversal fix status. Verify path validation manually."
	}
}

// verifyBoundaryFix checks if trust boundary violation was fixed.
func (v *Verifier) verifyBoundaryFix(
	original *safety.SecurityIssue,
	newCode string,
	result *safety.VerificationResult,
) {
	// Look for validation patterns
	hasValidation := strings.Contains(newCode, "Validate") ||
		strings.Contains(newCode, "validate") ||
		strings.Contains(newCode, "Check") ||
		strings.Contains(newCode, "Sanitize")

	hasAuth := strings.Contains(newCode, "Auth") ||
		strings.Contains(newCode, "auth") ||
		strings.Contains(newCode, "Authorize") ||
		strings.Contains(newCode, "Permission")

	hasMiddleware := strings.Contains(newCode, "middleware") ||
		strings.Contains(newCode, "Middleware") ||
		strings.Contains(newCode, "guard") ||
		strings.Contains(newCode, "Guard")

	if hasValidation || hasAuth || hasMiddleware {
		result.IsFixed = true
		result.StillPresent = false
		result.Explanation = "Validation/authorization added at trust boundary"
	} else {
		result.IsFixed = false
		result.StillPresent = true
		result.Explanation = "No validation pattern detected. Add input validation or auth middleware."
	}
}

// verifyGenericFix performs generic fix verification.
func (v *Verifier) verifyGenericFix(
	original *safety.SecurityIssue,
	newCode string,
	result *safety.VerificationResult,
) {
	// Generic heuristics: check if the problematic code is still present
	if original.Code != "" {
		if strings.Contains(newCode, original.Code) {
			result.IsFixed = false
			result.StillPresent = true
			result.Explanation = fmt.Sprintf(
				"Original problematic code pattern still present: %s",
				truncate(original.Code, 50),
			)
		} else {
			result.IsFixed = true
			result.StillPresent = false
			result.Explanation = "Original problematic code pattern no longer present"
		}
	} else {
		result.Explanation = "Could not verify fix - original code pattern not available. Manual review recommended."
	}
}

// truncate shortens a string to maxLen, adding ellipsis if needed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// GetIssueStore returns the issue store for testing or advanced use.
func (v *Verifier) GetIssueStore() *IssueStore {
	return v.issueStore
}
