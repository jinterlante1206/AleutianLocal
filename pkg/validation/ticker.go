// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package validation provides input validation utilities for security-critical operations.
//
// This package contains validators for user-provided inputs that are used in
// database queries, file paths, or subprocess calls. Using these validators
// prevents injection attacks (SQL/Flux injection, command injection, path traversal).
package validation

import (
	"fmt"
	"regexp"
	"strings"
)

// tickerPattern matches valid stock ticker symbols.
// Allows: uppercase letters, digits, dots (BRK.A), hyphens (BF-B)
// Max length: 10 characters (covers most exchanges)
var tickerPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.\-]{0,9}$`)

// ValidateTicker validates a stock ticker symbol to prevent Flux injection.
//
// Valid tickers:
//   - 1-10 characters
//   - Uppercase letters A-Z
//   - Digits 0-9
//   - Dots (.) for class shares like BRK.A
//   - Hyphens (-) for class shares like BF-B
//
// Returns an error if the ticker is invalid.
//
// Example:
//
//	if err := validation.ValidateTicker(ticker); err != nil {
//	    return nil, fmt.Errorf("invalid ticker: %w", err)
//	}
//	// Safe to use in Flux query
func ValidateTicker(ticker string) error {
	if ticker == "" {
		return fmt.Errorf("ticker cannot be empty")
	}

	if !tickerPattern.MatchString(ticker) {
		return fmt.Errorf("invalid ticker format: %q (must be 1-10 uppercase alphanumeric chars, dots, or hyphens)", ticker)
	}

	return nil
}

// ValidateTickers validates multiple ticker symbols.
// Returns an error listing all invalid tickers if any fail validation.
func ValidateTickers(tickers []string) error {
	var invalid []string
	for _, t := range tickers {
		if err := ValidateTicker(t); err != nil {
			invalid = append(invalid, t)
		}
	}

	if len(invalid) > 0 {
		return fmt.Errorf("invalid tickers: %v", invalid)
	}
	return nil
}

// SanitizeTicker normalizes and validates a ticker symbol.
// Returns the uppercase ticker if valid, or an error if invalid.
//
// Use this when you need both validation and normalization:
//
//	safeTicker, err := validation.SanitizeTicker(userInput)
//	if err != nil {
//	    return err
//	}
//	// safeTicker is uppercase and validated
func SanitizeTicker(ticker string) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(ticker))
	if err := ValidateTicker(normalized); err != nil {
		return "", err
	}
	return normalized, nil
}
