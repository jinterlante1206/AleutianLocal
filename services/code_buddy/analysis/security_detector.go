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
	"fmt"
	"regexp"
	"strings"
)

// SecurityPathDetector identifies security-sensitive code paths.
//
// # Description
//
// Analyzes symbol names and call chains to detect security-sensitive paths
// such as authentication, authorization, cryptography, and PII handling.
// Uses pre-compiled regex patterns for performance.
//
// # Detection Methods
//
//  1. Pattern matching on symbol name
//  2. Call chain analysis (if target is called BY security code)
//
// # Thread Safety
//
// Safe for concurrent use. Patterns are pre-compiled at construction.
type SecurityPathDetector struct {
	// compiledPatterns maps PathType -> compiled regex patterns
	compiledPatterns map[string][]*regexp.Regexp

	// rawPatterns stores original patterns for debugging
	rawPatterns map[string][]string
}

// Verify interface compliance at compile time
var _ Enricher = (*SecurityPathDetector)(nil)

// DefaultSecurityPatterns provides standard patterns for security detection.
//
// # Pattern Categories
//
//   - AUTH: Authentication (login, logout, token validation, sessions)
//   - AUTHZ: Authorization (permissions, roles, access control)
//   - PII: Personal identifiable information handling
//   - CRYPTO: Cryptographic operations
//   - SECRETS: Secret and credential handling
var DefaultSecurityPatterns = map[string][]string{
	SecurityPathAuth: {
		"(?i)login",
		"(?i)logout",
		"(?i)authenticate",
		"(?i)validatetoken",
		"(?i)verifytoken",
		"(?i)session",
		"(?i)signin",
		"(?i)signout",
		"(?i)oauth",
		"(?i)oidc",
		"(?i)saml",
		"(?i)jwt",
		"(?i)refreshtoken",
		"(?i)accesstoken",
	},
	SecurityPathAuthz: {
		"(?i)authorize",
		"(?i)permission",
		"(?i)role",
		"(?i)access",
		"(?i)canuser",
		"(?i)isallowed",
		"(?i)hasperm",
		"(?i)checkaccess",
		"(?i)acl",
		"(?i)rbac",
		"(?i)policy",
	},
	SecurityPathPII: {
		"(?i)email",
		"(?i)phone",
		"(?i)address",
		"(?i)ssn",
		"(?i)creditcard",
		"(?i)password",
		"(?i)secret",
		"(?i)birthdate",
		"(?i)socialsecurity",
		"(?i)personalinfo",
		"(?i)userdata",
		"(?i)pii",
	},
	SecurityPathCrypto: {
		"(?i)encrypt",
		"(?i)decrypt",
		"(?i)hash",
		"(?i)sign",
		"(?i)verify",
		"(?i)hmac",
		"(?i)aes",
		"(?i)rsa",
		"(?i)sha256",
		"(?i)sha512",
		"(?i)bcrypt",
		"(?i)argon",
		"(?i)pbkdf",
		"(?i)cipher",
	},
	SecurityPathSecrets: {
		"(?i)apikey",
		"(?i)token",
		"(?i)credential",
		"(?i)secret",
		"(?i)private",
		"(?i)privatekey",
		"(?i)secretkey",
		"(?i)masterkey",
		"(?i)passphrase",
		"(?i)vault",
	},
}

// NewSecurityPathDetector creates a detector with the given patterns.
//
// # Description
//
// Creates a SecurityPathDetector with pre-compiled regex patterns.
// Invalid patterns are logged but don't cause construction to fail.
//
// # Inputs
//
//   - patterns: Map of PathType -> regex patterns. Use DefaultSecurityPatterns
//     for standard patterns, or provide custom patterns.
//
// # Outputs
//
//   - *SecurityPathDetector: Ready-to-use detector.
//   - error: Non-nil if no valid patterns could be compiled.
//
// # Example
//
//	detector, err := NewSecurityPathDetector(DefaultSecurityPatterns)
//	if err != nil {
//	    return fmt.Errorf("failed to create detector: %w", err)
//	}
func NewSecurityPathDetector(patterns map[string][]string) (*SecurityPathDetector, error) {
	compiled := make(map[string][]*regexp.Regexp)
	validCount := 0

	for pathType, patternList := range patterns {
		compiledList := make([]*regexp.Regexp, 0, len(patternList))
		for _, pattern := range patternList {
			re, err := regexp.Compile(pattern)
			if err != nil {
				// Log warning but continue - don't fail on one bad pattern
				continue
			}
			compiledList = append(compiledList, re)
			validCount++
		}
		if len(compiledList) > 0 {
			compiled[pathType] = compiledList
		}
	}

	if validCount == 0 {
		return nil, fmt.Errorf("no valid patterns compiled")
	}

	return &SecurityPathDetector{
		compiledPatterns: compiled,
		rawPatterns:      patterns,
	}, nil
}

// Name returns the enricher identifier.
func (d *SecurityPathDetector) Name() string {
	return "security_path"
}

// Priority returns 1 (critical analysis).
func (d *SecurityPathDetector) Priority() int {
	return 1
}

// Enrich analyzes the target for security sensitivity.
//
// # Description
//
// Checks whether the target symbol or its callers are security-sensitive.
// Populates result.SecurityPath with findings.
//
// # Detection Logic
//
//  1. Check target symbol name against patterns
//  2. Check target file path against patterns
//  3. Check if any direct callers match security patterns (call chain)
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - target: The symbol to analyze.
//   - result: The result to enrich.
//
// # Outputs
//
//   - error: Non-nil on failure (should be rare for this enricher).
func (d *SecurityPathDetector) Enrich(
	ctx context.Context,
	target *EnrichmentTarget,
	result *EnhancedBlastRadius,
) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	securityPath := &SecurityPath{
		MatchedPatterns: make([]string, 0),
	}

	// Check target name
	symbolName := extractSymbolName(target.SymbolID)
	if pathType, patterns := d.matchPatterns(symbolName); pathType != "" {
		securityPath.IsSecuritySensitive = true
		securityPath.PathType = pathType
		securityPath.MatchedPatterns = append(securityPath.MatchedPatterns, patterns...)
		securityPath.Reason = fmt.Sprintf("Symbol name '%s' matches %s patterns", symbolName, pathType)
	}

	// Check file path
	if target.Symbol != nil && !securityPath.IsSecuritySensitive {
		filePath := target.Symbol.FilePath
		if pathType, patterns := d.matchPatterns(filePath); pathType != "" {
			securityPath.IsSecuritySensitive = true
			securityPath.PathType = pathType
			securityPath.MatchedPatterns = append(securityPath.MatchedPatterns, patterns...)
			securityPath.Reason = fmt.Sprintf("File path '%s' matches %s patterns", filePath, pathType)
		}
	}

	// Check context cancellation before expensive call chain analysis
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Check call chain (are we called BY security code?)
	if target.BaseResult != nil && len(target.BaseResult.DirectCallers) > 0 {
		for _, caller := range target.BaseResult.DirectCallers {
			callerName := extractSymbolName(caller.ID)
			if pathType, _ := d.matchPatterns(callerName); pathType != "" {
				securityPath.CallChainSecurity = true
				if !securityPath.IsSecuritySensitive {
					securityPath.IsSecuritySensitive = true
					securityPath.PathType = pathType
					securityPath.Reason = fmt.Sprintf("Called by security function '%s' (%s)", callerName, pathType)
				}
				break
			}

			// Check context periodically in loop
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}

	// Determine if review is required
	if securityPath.IsSecuritySensitive {
		securityPath.RequiresReview = true
	}

	// Only set result if we found something
	if securityPath.IsSecuritySensitive {
		result.SecurityPath = securityPath
	}

	return nil
}

// matchPatterns checks a string against all compiled patterns.
// Returns the matching PathType and the matched pattern strings.
func (d *SecurityPathDetector) matchPatterns(s string) (string, []string) {
	for pathType, patterns := range d.compiledPatterns {
		var matched []string
		for _, re := range patterns {
			if re.MatchString(s) {
				matched = append(matched, re.String())
			}
		}
		if len(matched) > 0 {
			return pathType, matched
		}
	}
	return "", nil
}

// extractSymbolName extracts the function/type name from a symbol ID.
// Symbol ID format: "path/to/file.go:line:name"
func extractSymbolName(symbolID string) string {
	parts := strings.Split(symbolID, ":")
	if len(parts) >= 3 {
		return parts[len(parts)-1]
	}
	if len(parts) >= 1 {
		return parts[len(parts)-1]
	}
	return symbolID
}

// GetPatterns returns the raw patterns for inspection/debugging.
func (d *SecurityPathDetector) GetPatterns() map[string][]string {
	// Return a copy to prevent mutation
	result := make(map[string][]string)
	for k, v := range d.rawPatterns {
		patternsCopy := make([]string, len(v))
		copy(patternsCopy, v)
		result[k] = patternsCopy
	}
	return result
}

// AddPattern adds a pattern to an existing detector.
// Note: This creates a new compiled pattern; the detector is NOT fully
// thread-safe for pattern modification during use.
func (d *SecurityPathDetector) AddPattern(pathType, pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid pattern %q: %w", pattern, err)
	}

	d.compiledPatterns[pathType] = append(d.compiledPatterns[pathType], re)
	d.rawPatterns[pathType] = append(d.rawPatterns[pathType], pattern)
	return nil
}
