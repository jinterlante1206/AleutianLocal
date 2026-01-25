// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package ttl

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// Constants
// =============================================================================

const (
	// maxRetentionDays is the maximum allowed retention period (10 years).
	maxRetentionDays = 3650

	// minRetentionMinutes is the minimum allowed retention period (1 minute).
	minRetentionMinutes = 1

	// daysPerMonth is the assumed days per month for ISO 8601 parsing.
	daysPerMonth = 30

	// daysPerYear is the assumed days per year for ISO 8601 parsing.
	daysPerYear = 365

	// daysPerWeek is the days per week.
	daysPerWeek = 7
)

// =============================================================================
// Regular Expressions
// =============================================================================

var (
	// simpleFormatRegex matches simple duration formats like "30d", "24h", "1w".
	// Supported units: m (minutes), h (hours), d (days), w (weeks), M (months), y (years).
	simpleFormatRegex = regexp.MustCompile(`^(\d+)(m|h|d|w|M|y)$`)

	// iso8601Regex matches ISO 8601 duration formats like "P30D", "PT24H", "P1Y2M3D".
	// Format: P[n]Y[n]M[n]W[n]DT[n]H[n]M[n]S
	iso8601Regex = regexp.MustCompile(
		`^P(?:(\d+)Y)?(?:(\d+)M)?(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`,
	)
)

// =============================================================================
// Public Functions
// =============================================================================

// ParseTTLDuration parses a TTL string supporting both simple and ISO 8601 formats.
//
// # Description
//
// Accepts human-friendly formats (30d, 24h, 1w, 3M, 1y) and ISO 8601 duration
// format (P30D, PT24H, P1W, P3M, P1Y). Returns the duration, expiration timestamp,
// and a human-readable description.
//
// # Inputs
//
//   - ttlString: Duration string in either format. Empty string returns zero values.
//
// # Outputs
//
//   - TTLParseResult: Parsed result with Duration, ExpiresAt, Description, Format.
//   - error: Non-nil if parsing fails.
//
// # Examples
//
//	ParseTTLDuration("30d")    // 30 days, "30 days", TTLFormatSimple
//	ParseTTLDuration("P30D")   // 30 days, "30 days", TTLFormatISO8601
//	ParseTTLDuration("24h")    // 24 hours, "24 hours", TTLFormatSimple
//	ParseTTLDuration("PT24H")  // 24 hours, "24 hours", TTLFormatISO8601
//	ParseTTLDuration("")       // 0, 0, "", nil (no TTL = never expires)
//
// # Limitations
//
//   - Maximum retention: 10 years (3650 days).
//   - Minimum retention: 1 minute.
//   - Months in ISO 8601 (P1M) assumed to be 30 days.
//   - Years in ISO 8601 (P1Y) assumed to be 365 days.
//   - Weeks in ISO 8601 (P1W) is 7 days.
//
// # Assumptions
//
//   - Input is a single duration value, not a timestamp.
//   - The system clock is used to calculate ExpiresAt.
func ParseTTLDuration(ttlString string) (TTLParseResult, error) {
	// Empty string means no TTL (never expires)
	if ttlString == "" {
		return TTLParseResult{
			Duration:    0,
			ExpiresAt:   0,
			Description: "",
			Format:      TTLFormatSimple,
		}, nil
	}

	ttlString = strings.TrimSpace(ttlString)

	// Try ISO 8601 format first (starts with P)
	if strings.HasPrefix(strings.ToUpper(ttlString), "P") {
		return parseISO8601Duration(ttlString)
	}

	// Try simple format
	return parseSimpleDuration(ttlString)
}

// =============================================================================
// Private Functions
// =============================================================================

// parseSimpleDuration parses simple duration formats like "30d", "24h", "1w".
//
// # Description
//
// Parses human-friendly duration strings with a numeric value followed by a
// single-character unit. Validates that the resulting duration is within
// allowed bounds.
//
// # Inputs
//
//   - s: Duration string in simple format.
//
// # Outputs
//
//   - TTLParseResult: Parsed result.
//   - error: Non-nil if format is invalid or duration is out of bounds.
func parseSimpleDuration(s string) (TTLParseResult, error) {
	matches := simpleFormatRegex.FindStringSubmatch(s)
	if matches == nil {
		return TTLParseResult{}, fmt.Errorf("invalid TTL format: %q (expected format like '30d', '24h', '1w')", s)
	}

	value, err := strconv.Atoi(matches[1])
	if err != nil {
		return TTLParseResult{}, fmt.Errorf("invalid TTL value: %q", matches[1])
	}

	unit := matches[2]
	duration, description, err := calculateDuration(value, unit)
	if err != nil {
		return TTLParseResult{}, err
	}

	if err := validateDuration(duration); err != nil {
		return TTLParseResult{}, err
	}

	return TTLParseResult{
		Duration:    duration,
		ExpiresAt:   time.Now().Add(duration).UnixMilli(),
		Description: description,
		Format:      TTLFormatSimple,
	}, nil
}

// parseISO8601Duration parses ISO 8601 duration formats like "P30D", "PT24H".
//
// # Description
//
// Parses durations in the ISO 8601 format: P[n]Y[n]M[n]W[n]DT[n]H[n]M[n]S.
// Converts months to 30 days and years to 365 days.
//
// # Inputs
//
//   - s: Duration string in ISO 8601 format.
//
// # Outputs
//
//   - TTLParseResult: Parsed result.
//   - error: Non-nil if format is invalid or duration is out of bounds.
func parseISO8601Duration(s string) (TTLParseResult, error) {
	upper := strings.ToUpper(s)
	matches := iso8601Regex.FindStringSubmatch(upper)
	if matches == nil {
		return TTLParseResult{}, fmt.Errorf("invalid ISO 8601 duration format: %q (expected format like 'P30D', 'PT24H')", s)
	}

	// Parse each component: years, months, weeks, days, hours, minutes, seconds
	years := parseIntOrZero(matches[1])
	months := parseIntOrZero(matches[2])
	weeks := parseIntOrZero(matches[3])
	days := parseIntOrZero(matches[4])
	hours := parseIntOrZero(matches[5])
	minutes := parseIntOrZero(matches[6])
	seconds := parseIntOrZero(matches[7])

	// Check for empty duration (just "P" or "PT")
	if years == 0 && months == 0 && weeks == 0 && days == 0 &&
		hours == 0 && minutes == 0 && seconds == 0 {
		return TTLParseResult{}, fmt.Errorf("empty ISO 8601 duration: %q", s)
	}

	// Convert to total duration
	totalDays := years*daysPerYear + months*daysPerMonth + weeks*daysPerWeek + days
	totalHours := hours
	totalMinutes := minutes
	totalSeconds := seconds

	duration := time.Duration(totalDays)*24*time.Hour +
		time.Duration(totalHours)*time.Hour +
		time.Duration(totalMinutes)*time.Minute +
		time.Duration(totalSeconds)*time.Second

	if err := validateDuration(duration); err != nil {
		return TTLParseResult{}, err
	}

	description := buildISO8601Description(years, months, weeks, days, hours, minutes, seconds)

	return TTLParseResult{
		Duration:    duration,
		ExpiresAt:   time.Now().Add(duration).UnixMilli(),
		Description: description,
		Format:      TTLFormatISO8601,
	}, nil
}

// calculateDuration calculates the time.Duration from a numeric value and unit.
//
// # Inputs
//
//   - value: Numeric value.
//   - unit: Unit character (m, h, d, w, M, y).
//
// # Outputs
//
//   - time.Duration: The calculated duration.
//   - string: Human-readable description.
//   - error: Non-nil if unit is unknown.
func calculateDuration(value int, unit string) (time.Duration, string, error) {
	switch unit {
	case "m":
		return time.Duration(value) * time.Minute,
			pluralize(value, "minute"),
			nil
	case "h":
		return time.Duration(value) * time.Hour,
			pluralize(value, "hour"),
			nil
	case "d":
		return time.Duration(value) * 24 * time.Hour,
			pluralize(value, "day"),
			nil
	case "w":
		return time.Duration(value) * 7 * 24 * time.Hour,
			pluralize(value, "week"),
			nil
	case "M":
		return time.Duration(value) * daysPerMonth * 24 * time.Hour,
			pluralize(value, "month"),
			nil
	case "y":
		return time.Duration(value) * daysPerYear * 24 * time.Hour,
			pluralize(value, "year"),
			nil
	default:
		return 0, "", fmt.Errorf("unknown TTL unit: %q", unit)
	}
}

// validateDuration checks that a duration is within allowed bounds.
//
// # Inputs
//
//   - d: Duration to validate.
//
// # Outputs
//
//   - error: Non-nil if duration is out of bounds.
func validateDuration(d time.Duration) error {
	minDuration := time.Duration(minRetentionMinutes) * time.Minute
	maxDuration := time.Duration(maxRetentionDays) * 24 * time.Hour

	if d < minDuration {
		return fmt.Errorf("TTL too short: minimum is %d minute(s)", minRetentionMinutes)
	}
	if d > maxDuration {
		return fmt.Errorf("TTL too long: maximum is %d days (10 years)", maxRetentionDays)
	}
	return nil
}

// parseIntOrZero parses a string to int, returning 0 if empty or invalid.
//
// # Inputs
//
//   - s: String to parse.
//
// # Outputs
//
//   - int: Parsed value or 0.
func parseIntOrZero(s string) int {
	if s == "" {
		return 0
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}

// pluralize returns a human-readable string with proper pluralization.
//
// # Inputs
//
//   - value: Numeric value.
//   - unit: Unit name (singular form).
//
// # Outputs
//
//   - string: Formatted string like "1 day" or "30 days".
func pluralize(value int, unit string) string {
	if value == 1 {
		return fmt.Sprintf("%d %s", value, unit)
	}
	return fmt.Sprintf("%d %ss", value, unit)
}

// buildISO8601Description builds a human-readable description from ISO 8601 components.
//
// # Inputs
//
//   - years, months, weeks, days, hours, minutes, seconds: Parsed values.
//
// # Outputs
//
//   - string: Human-readable description like "1 year, 2 months, 3 days".
func buildISO8601Description(years, months, weeks, days, hours, minutes, seconds int) string {
	var parts []string

	if years > 0 {
		parts = append(parts, pluralize(years, "year"))
	}
	if months > 0 {
		parts = append(parts, pluralize(months, "month"))
	}
	if weeks > 0 {
		parts = append(parts, pluralize(weeks, "week"))
	}
	if days > 0 {
		parts = append(parts, pluralize(days, "day"))
	}
	if hours > 0 {
		parts = append(parts, pluralize(hours, "hour"))
	}
	if minutes > 0 {
		parts = append(parts, pluralize(minutes, "minute"))
	}
	if seconds > 0 {
		parts = append(parts, pluralize(seconds, "second"))
	}

	return strings.Join(parts, ", ")
}
