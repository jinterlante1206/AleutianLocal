// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package trust provides trust boundary analysis for security scanning.
//
// # Description
//
// This package implements a formal trust zone model that detects where
// untrusted data crosses into trusted zones without proper validation.
// It builds on data flow analysis to provide Aleutian's unique security
// differentiator: proactive detection of trust boundary violations.
//
// # Trust Zones
//
// The model recognizes four trust levels:
//   - Untrusted: External input (HTTP requests, CLI args, file uploads)
//   - Boundary: Validation/sanitization points
//   - Internal: Business logic that should only receive validated data
//   - Privileged: Admin/system functionality requiring authorization
//
// # Thread Safety
//
// All types in this package are safe for concurrent use after initialization.
package trust

import (
	"regexp"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/safety"
)

// ZonePatterns defines patterns for detecting trust zones.
type ZonePatterns struct {
	// PathPatterns match file/directory paths to zones
	PathPatterns map[safety.TrustLevel][]*regexp.Regexp

	// FunctionPatterns match function names to zones
	FunctionPatterns map[safety.TrustLevel][]*regexp.Regexp

	// ReceiverPatterns match receiver types to zones
	ReceiverPatterns map[safety.TrustLevel][]*regexp.Regexp

	// PackagePatterns match package names to zones
	PackagePatterns map[safety.TrustLevel][]*regexp.Regexp
}

// DefaultZonePatterns returns the default patterns for zone detection.
//
// Description:
//
//	Creates patterns based on common naming conventions:
//	- Untrusted: handlers, controllers, api, routes
//	- Boundary: validators, sanitizers, middleware
//	- Internal: services, domain, core, models
//	- Privileged: admin, internal, system
func DefaultZonePatterns() *ZonePatterns {
	return &ZonePatterns{
		PathPatterns: map[safety.TrustLevel][]*regexp.Regexp{
			// UNTRUSTED - External input entry points
			safety.TrustExternal: {
				regexp.MustCompile(`(?i)handlers?/`),
				regexp.MustCompile(`(?i)controllers?/`),
				regexp.MustCompile(`(?i)api/`),
				regexp.MustCompile(`(?i)routes?/`),
				regexp.MustCompile(`(?i)endpoints?/`),
				regexp.MustCompile(`(?i)views?/`),
				regexp.MustCompile(`(?i)resources?/`),
				regexp.MustCompile(`(?i)web/`),
				regexp.MustCompile(`(?i)http/`),
				regexp.MustCompile(`(?i)grpc/`),
				regexp.MustCompile(`(?i)graphql/`),
				regexp.MustCompile(`(?i)websocket/`),
				regexp.MustCompile(`(?i)cli/`),
				regexp.MustCompile(`(?i)cmd/`),
			},
			// BOUNDARY - Validation and sanitization
			safety.TrustValidation: {
				regexp.MustCompile(`(?i)validators?/`),
				regexp.MustCompile(`(?i)sanitizers?/`),
				regexp.MustCompile(`(?i)middleware/`),
				regexp.MustCompile(`(?i)filters?/`),
				regexp.MustCompile(`(?i)interceptors?/`),
				regexp.MustCompile(`(?i)guards?/`),
				regexp.MustCompile(`(?i)auth/`),
				regexp.MustCompile(`(?i)security/`),
			},
			// INTERNAL - Business logic
			safety.TrustInternal: {
				regexp.MustCompile(`(?i)services?/`),
				regexp.MustCompile(`(?i)domain/`),
				regexp.MustCompile(`(?i)core/`),
				regexp.MustCompile(`(?i)business/`),
				regexp.MustCompile(`(?i)logic/`),
				regexp.MustCompile(`(?i)usecases?/`),
				regexp.MustCompile(`(?i)application/`),
				regexp.MustCompile(`(?i)models?/`),
				regexp.MustCompile(`(?i)entities?/`),
				regexp.MustCompile(`(?i)repository/`),
				regexp.MustCompile(`(?i)repositories/`),
				regexp.MustCompile(`(?i)store/`),
				regexp.MustCompile(`(?i)storage/`),
			},
			// PRIVILEGED - Admin and system
			safety.TrustPrivileged: {
				regexp.MustCompile(`(?i)admin/`),
				regexp.MustCompile(`(?i)internal/`),
				regexp.MustCompile(`(?i)system/`),
				regexp.MustCompile(`(?i)management/`),
				regexp.MustCompile(`(?i)ops/`),
				regexp.MustCompile(`(?i)debug/`),
				regexp.MustCompile(`(?i)metrics/`),
				regexp.MustCompile(`(?i)monitor/`),
			},
		},
		FunctionPatterns: map[safety.TrustLevel][]*regexp.Regexp{
			// UNTRUSTED - Handler functions
			safety.TrustExternal: {
				regexp.MustCompile(`(?i)^Handle`),
				regexp.MustCompile(`(?i)Handler$`),
				regexp.MustCompile(`(?i)^Serve`),
				regexp.MustCompile(`(?i)^Process.*Request`),
				regexp.MustCompile(`(?i)^On.*Event`),
				regexp.MustCompile(`(?i)^Receive`),
				regexp.MustCompile(`(?i)^Accept`),
				regexp.MustCompile(`(?i)^Parse.*Input`),
			},
			// BOUNDARY - Validation functions
			safety.TrustValidation: {
				regexp.MustCompile(`(?i)^Validate`),
				regexp.MustCompile(`(?i)^Sanitize`),
				regexp.MustCompile(`(?i)^Check`),
				regexp.MustCompile(`(?i)^Verify`),
				regexp.MustCompile(`(?i)^Clean`),
				regexp.MustCompile(`(?i)^Filter`),
				regexp.MustCompile(`(?i)^Escape`),
				regexp.MustCompile(`(?i)^Normalize`),
				regexp.MustCompile(`(?i)^Authenticate`),
				regexp.MustCompile(`(?i)^Authorize`),
			},
			// INTERNAL - Service functions
			safety.TrustInternal: {
				regexp.MustCompile(`(?i)^Create`),
				regexp.MustCompile(`(?i)^Update`),
				regexp.MustCompile(`(?i)^Delete`),
				regexp.MustCompile(`(?i)^Get`),
				regexp.MustCompile(`(?i)^Find`),
				regexp.MustCompile(`(?i)^List`),
				regexp.MustCompile(`(?i)^Save`),
				regexp.MustCompile(`(?i)^Process`),
				regexp.MustCompile(`(?i)^Execute`),
				regexp.MustCompile(`(?i)^Compute`),
			},
			// PRIVILEGED - Admin functions
			safety.TrustPrivileged: {
				regexp.MustCompile(`(?i)^Admin`),
				regexp.MustCompile(`(?i)Admin$`),
				regexp.MustCompile(`(?i)^System`),
				regexp.MustCompile(`(?i)^Internal`),
				regexp.MustCompile(`(?i)^Debug`),
				regexp.MustCompile(`(?i)^Manage`),
				regexp.MustCompile(`(?i)^Configure`),
			},
		},
		ReceiverPatterns: map[safety.TrustLevel][]*regexp.Regexp{
			safety.TrustExternal: {
				regexp.MustCompile(`(?i)Handler`),
				regexp.MustCompile(`(?i)Controller`),
				regexp.MustCompile(`(?i)Server`),
				regexp.MustCompile(`(?i)Endpoint`),
			},
			safety.TrustValidation: {
				regexp.MustCompile(`(?i)Validator`),
				regexp.MustCompile(`(?i)Sanitizer`),
				regexp.MustCompile(`(?i)Middleware`),
				regexp.MustCompile(`(?i)Guard`),
				regexp.MustCompile(`(?i)Interceptor`),
			},
			safety.TrustInternal: {
				regexp.MustCompile(`(?i)Service`),
				regexp.MustCompile(`(?i)Repository`),
				regexp.MustCompile(`(?i)Store`),
				regexp.MustCompile(`(?i)Manager`),
				regexp.MustCompile(`(?i)Provider`),
			},
			safety.TrustPrivileged: {
				regexp.MustCompile(`(?i)Admin`),
				regexp.MustCompile(`(?i)System`),
			},
		},
		PackagePatterns: map[safety.TrustLevel][]*regexp.Regexp{
			safety.TrustExternal: {
				regexp.MustCompile(`(?i)/handlers?$`),
				regexp.MustCompile(`(?i)/api$`),
				regexp.MustCompile(`(?i)/http$`),
				regexp.MustCompile(`(?i)/web$`),
			},
			safety.TrustValidation: {
				regexp.MustCompile(`(?i)/middleware$`),
				regexp.MustCompile(`(?i)/validators?$`),
				regexp.MustCompile(`(?i)/auth$`),
			},
			safety.TrustInternal: {
				regexp.MustCompile(`(?i)/services?$`),
				regexp.MustCompile(`(?i)/domain$`),
				regexp.MustCompile(`(?i)/core$`),
			},
			safety.TrustPrivileged: {
				regexp.MustCompile(`(?i)/admin$`),
				regexp.MustCompile(`(?i)/internal$`),
			},
		},
	}
}

// MatchPath returns the trust level for a file path.
func (p *ZonePatterns) MatchPath(path string) (safety.TrustLevel, bool) {
	// Check in order of specificity (privileged first)
	levels := []safety.TrustLevel{
		safety.TrustPrivileged,
		safety.TrustValidation,
		safety.TrustExternal,
		safety.TrustInternal,
	}

	for _, level := range levels {
		patterns, ok := p.PathPatterns[level]
		if !ok {
			continue
		}
		for _, pattern := range patterns {
			if pattern.MatchString(path) {
				return level, true
			}
		}
	}

	return safety.TrustInternal, false // Default to internal
}

// MatchFunction returns the trust level for a function name.
func (p *ZonePatterns) MatchFunction(name string) (safety.TrustLevel, bool) {
	levels := []safety.TrustLevel{
		safety.TrustPrivileged,
		safety.TrustValidation,
		safety.TrustExternal,
		safety.TrustInternal,
	}

	for _, level := range levels {
		patterns, ok := p.FunctionPatterns[level]
		if !ok {
			continue
		}
		for _, pattern := range patterns {
			if pattern.MatchString(name) {
				return level, true
			}
		}
	}

	return safety.TrustInternal, false
}

// MatchReceiver returns the trust level for a receiver type.
func (p *ZonePatterns) MatchReceiver(receiver string) (safety.TrustLevel, bool) {
	levels := []safety.TrustLevel{
		safety.TrustPrivileged,
		safety.TrustValidation,
		safety.TrustExternal,
		safety.TrustInternal,
	}

	for _, level := range levels {
		patterns, ok := p.ReceiverPatterns[level]
		if !ok {
			continue
		}
		for _, pattern := range patterns {
			if pattern.MatchString(receiver) {
				return level, true
			}
		}
	}

	return safety.TrustInternal, false
}

// CrossingRequirements specifies what validation is required for zone crossings.
type CrossingRequirements struct {
	// From → To → Requirements
	Requirements map[safety.TrustLevel]map[safety.TrustLevel][]string
	// From → To → CWE
	CWEMapping map[safety.TrustLevel]map[safety.TrustLevel]string
	// From → To → Severity
	SeverityMapping map[safety.TrustLevel]map[safety.TrustLevel]safety.Severity
}

// DefaultCrossingRequirements returns default requirements for zone crossings.
func DefaultCrossingRequirements() *CrossingRequirements {
	return &CrossingRequirements{
		Requirements: map[safety.TrustLevel]map[safety.TrustLevel][]string{
			// From UNTRUSTED
			safety.TrustExternal: {
				safety.TrustInternal: {
					"Input validation required",
					"Type checking recommended",
					"Length/range validation recommended",
				},
				safety.TrustPrivileged: {
					"Authentication REQUIRED",
					"Authorization REQUIRED",
					"Input validation REQUIRED",
				},
			},
			// From BOUNDARY (already validated) - less strict
			safety.TrustValidation: {
				safety.TrustPrivileged: {
					"Authorization REQUIRED",
				},
			},
			// From INTERNAL
			safety.TrustInternal: {
				safety.TrustPrivileged: {
					"Authorization check REQUIRED",
				},
			},
		},
		CWEMapping: map[safety.TrustLevel]map[safety.TrustLevel]string{
			safety.TrustExternal: {
				safety.TrustInternal:   "CWE-20",  // Improper Input Validation
				safety.TrustPrivileged: "CWE-284", // Improper Access Control
			},
			safety.TrustValidation: {
				safety.TrustPrivileged: "CWE-285", // Improper Authorization
			},
			safety.TrustInternal: {
				safety.TrustPrivileged: "CWE-285", // Improper Authorization
			},
		},
		SeverityMapping: map[safety.TrustLevel]map[safety.TrustLevel]safety.Severity{
			safety.TrustExternal: {
				safety.TrustInternal:   safety.SeverityMedium,
				safety.TrustPrivileged: safety.SeverityCritical,
			},
			safety.TrustValidation: {
				safety.TrustPrivileged: safety.SeverityHigh,
			},
			safety.TrustInternal: {
				safety.TrustPrivileged: safety.SeverityHigh,
			},
		},
	}
}

// GetRequirements returns requirements for a crossing.
func (r *CrossingRequirements) GetRequirements(from, to safety.TrustLevel) []string {
	if toMap, ok := r.Requirements[from]; ok {
		if reqs, ok := toMap[to]; ok {
			return reqs
		}
	}
	return nil
}

// GetCWE returns the CWE for a violation.
func (r *CrossingRequirements) GetCWE(from, to safety.TrustLevel) string {
	if toMap, ok := r.CWEMapping[from]; ok {
		if cwe, ok := toMap[to]; ok {
			return cwe
		}
	}
	return "CWE-20" // Default: Improper Input Validation
}

// GetSeverity returns the severity for a violation.
func (r *CrossingRequirements) GetSeverity(from, to safety.TrustLevel) safety.Severity {
	if toMap, ok := r.SeverityMapping[from]; ok {
		if sev, ok := toMap[to]; ok {
			return sev
		}
	}
	return safety.SeverityMedium // Default
}

// RequiresValidation returns true if the crossing requires validation.
func (r *CrossingRequirements) RequiresValidation(from, to safety.TrustLevel) bool {
	// Any crossing from less trusted to more trusted requires validation
	return from < to
}

// TrustLevelName returns a human-readable name for a trust level.
func TrustLevelName(level safety.TrustLevel) string {
	switch level {
	case safety.TrustExternal:
		return "Untrusted"
	case safety.TrustValidation:
		return "Boundary"
	case safety.TrustInternal:
		return "Internal"
	case safety.TrustPrivileged:
		return "Privileged"
	default:
		return "Unknown"
	}
}

// GenerateZoneID creates a unique zone ID from level and name.
func GenerateZoneID(level safety.TrustLevel, name string) string {
	// Normalize name
	normalized := strings.ToLower(name)
	normalized = strings.ReplaceAll(normalized, "/", "_")
	normalized = strings.ReplaceAll(normalized, "\\", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")

	return strings.ToLower(TrustLevelName(level)) + "_" + normalized
}

// GenerateCrossingID creates a unique crossing ID.
func GenerateCrossingID(fromZone, toZone, crossingAt string) string {
	return fromZone + "_to_" + toZone + "_at_" + strings.ReplaceAll(crossingAt, ".", "_")
}
