// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package crs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
)

// ProjectHashLength is the expected length of a project hash (SHA256 truncated).
const ProjectHashLength = 16 // 64 bits = 16 hex chars

// projectHashRegex validates project hash format.
var projectHashRegex = regexp.MustCompile(`^[a-f0-9]{8,64}$`)

// ValidateProjectHash checks if a hash is valid.
//
// Description:
//
//	Validates that the project hash is a hex string of 8-64 characters.
//	Used by GR-33, GR-34, and GR-36 for consistent validation.
//
// Inputs:
//   - hash: The project hash to validate.
//
// Outputs:
//   - error: Non-nil if validation fails.
//
// Thread Safety: Safe for concurrent use (stateless).
func ValidateProjectHash(hash string) error {
	if hash == "" {
		return fmt.Errorf("project hash must not be empty")
	}
	if !projectHashRegex.MatchString(hash) {
		return fmt.Errorf("invalid project hash format: must be 8-64 hex characters, got %q", hash)
	}
	return nil
}

// ComputeProjectHash generates a project hash from a path.
//
// Description:
//
//	Computes a SHA256 hash of the project path and returns the first
//	16 hex characters. This provides consistent project identification
//	across sessions.
//
// Inputs:
//   - projectPath: Absolute path to the project root.
//
// Outputs:
//   - string: 16-character hex hash.
//
// Thread Safety: Safe for concurrent use (stateless).
func ComputeProjectHash(projectPath string) string {
	h := sha256.Sum256([]byte(projectPath))
	return hex.EncodeToString(h[:])[:ProjectHashLength]
}
