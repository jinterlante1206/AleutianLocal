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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// ParseTTLDuration Tests - Simple Format
// =============================================================================

func TestParseTTLDuration_SimpleFormat_Minutes(t *testing.T) {
	result, err := ParseTTLDuration("30m")

	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, result.Duration)
	assert.Equal(t, "30 minutes", result.Description)
	assert.Equal(t, TTLFormatSimple, result.Format)
	assert.Greater(t, result.ExpiresAt, int64(0))
}

func TestParseTTLDuration_SimpleFormat_Hours(t *testing.T) {
	result, err := ParseTTLDuration("24h")

	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, result.Duration)
	assert.Equal(t, "24 hours", result.Description)
	assert.Equal(t, TTLFormatSimple, result.Format)
}

func TestParseTTLDuration_SimpleFormat_Days(t *testing.T) {
	result, err := ParseTTLDuration("30d")

	require.NoError(t, err)
	assert.Equal(t, 30*24*time.Hour, result.Duration)
	assert.Equal(t, "30 days", result.Description)
	assert.Equal(t, TTLFormatSimple, result.Format)
}

func TestParseTTLDuration_SimpleFormat_Weeks(t *testing.T) {
	result, err := ParseTTLDuration("2w")

	require.NoError(t, err)
	assert.Equal(t, 14*24*time.Hour, result.Duration)
	assert.Equal(t, "2 weeks", result.Description)
	assert.Equal(t, TTLFormatSimple, result.Format)
}

func TestParseTTLDuration_SimpleFormat_Months(t *testing.T) {
	result, err := ParseTTLDuration("3M")

	require.NoError(t, err)
	assert.Equal(t, 90*24*time.Hour, result.Duration) // 3 * 30 days
	assert.Equal(t, "3 months", result.Description)
	assert.Equal(t, TTLFormatSimple, result.Format)
}

func TestParseTTLDuration_SimpleFormat_Years(t *testing.T) {
	result, err := ParseTTLDuration("1y")

	require.NoError(t, err)
	assert.Equal(t, 365*24*time.Hour, result.Duration)
	assert.Equal(t, "1 year", result.Description)
	assert.Equal(t, TTLFormatSimple, result.Format)
}

func TestParseTTLDuration_SimpleFormat_SingularPlural(t *testing.T) {
	tests := []struct {
		input       string
		description string
	}{
		{"1m", "1 minute"},
		{"2m", "2 minutes"},
		{"1h", "1 hour"},
		{"2h", "2 hours"},
		{"1d", "1 day"},
		{"2d", "2 days"},
		{"1w", "1 week"},
		{"2w", "2 weeks"},
		{"1M", "1 month"},
		{"2M", "2 months"},
		{"1y", "1 year"},
		{"2y", "2 years"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseTTLDuration(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.description, result.Description)
		})
	}
}

// =============================================================================
// ParseTTLDuration Tests - ISO 8601 Format
// =============================================================================

func TestParseTTLDuration_ISO8601_Days(t *testing.T) {
	result, err := ParseTTLDuration("P30D")

	require.NoError(t, err)
	assert.Equal(t, 30*24*time.Hour, result.Duration)
	assert.Equal(t, "30 days", result.Description)
	assert.Equal(t, TTLFormatISO8601, result.Format)
}

func TestParseTTLDuration_ISO8601_Hours(t *testing.T) {
	result, err := ParseTTLDuration("PT24H")

	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, result.Duration)
	assert.Equal(t, "24 hours", result.Description)
	assert.Equal(t, TTLFormatISO8601, result.Format)
}

func TestParseTTLDuration_ISO8601_Weeks(t *testing.T) {
	result, err := ParseTTLDuration("P2W")

	require.NoError(t, err)
	assert.Equal(t, 14*24*time.Hour, result.Duration)
	assert.Equal(t, "2 weeks", result.Description)
	assert.Equal(t, TTLFormatISO8601, result.Format)
}

func TestParseTTLDuration_ISO8601_Months(t *testing.T) {
	result, err := ParseTTLDuration("P3M")

	require.NoError(t, err)
	assert.Equal(t, 90*24*time.Hour, result.Duration) // 3 * 30 days
	assert.Equal(t, "3 months", result.Description)
	assert.Equal(t, TTLFormatISO8601, result.Format)
}

func TestParseTTLDuration_ISO8601_Years(t *testing.T) {
	result, err := ParseTTLDuration("P1Y")

	require.NoError(t, err)
	assert.Equal(t, 365*24*time.Hour, result.Duration)
	assert.Equal(t, "1 year", result.Description)
	assert.Equal(t, TTLFormatISO8601, result.Format)
}

func TestParseTTLDuration_ISO8601_Complex(t *testing.T) {
	result, err := ParseTTLDuration("P1Y2M3DT4H5M6S")

	require.NoError(t, err)
	// 1 year (365 days) + 2 months (60 days) + 3 days + 4 hours + 5 minutes + 6 seconds
	expected := time.Duration(428)*24*time.Hour +
		4*time.Hour + 5*time.Minute + 6*time.Second
	assert.Equal(t, expected, result.Duration)
	assert.Equal(t, "1 year, 2 months, 3 days, 4 hours, 5 minutes, 6 seconds", result.Description)
	assert.Equal(t, TTLFormatISO8601, result.Format)
}

func TestParseTTLDuration_ISO8601_LowercaseAccepted(t *testing.T) {
	result, err := ParseTTLDuration("p30d")

	require.NoError(t, err)
	assert.Equal(t, 30*24*time.Hour, result.Duration)
	assert.Equal(t, TTLFormatISO8601, result.Format)
}

// =============================================================================
// ParseTTLDuration Tests - Edge Cases
// =============================================================================

func TestParseTTLDuration_EmptyString(t *testing.T) {
	result, err := ParseTTLDuration("")

	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), result.Duration)
	assert.Equal(t, int64(0), result.ExpiresAt)
	assert.Equal(t, "", result.Description)
}

func TestParseTTLDuration_Whitespace(t *testing.T) {
	result, err := ParseTTLDuration("  30d  ")

	require.NoError(t, err)
	assert.Equal(t, 30*24*time.Hour, result.Duration)
}

func TestParseTTLDuration_MinimumDuration(t *testing.T) {
	result, err := ParseTTLDuration("1m")

	require.NoError(t, err)
	assert.Equal(t, 1*time.Minute, result.Duration)
}

func TestParseTTLDuration_MaximumDuration(t *testing.T) {
	result, err := ParseTTLDuration("3650d")

	require.NoError(t, err)
	assert.Equal(t, 3650*24*time.Hour, result.Duration)
}

func TestParseTTLDuration_ExpiresAtIsInFuture(t *testing.T) {
	before := time.Now().UnixMilli()
	result, err := ParseTTLDuration("1h")
	after := time.Now().UnixMilli()

	require.NoError(t, err)

	// ExpiresAt should be approximately 1 hour from now
	expectedMin := before + (1 * time.Hour).Milliseconds()
	expectedMax := after + (1 * time.Hour).Milliseconds()

	assert.GreaterOrEqual(t, result.ExpiresAt, expectedMin)
	assert.LessOrEqual(t, result.ExpiresAt, expectedMax)
}

// =============================================================================
// ParseTTLDuration Tests - Error Cases
// =============================================================================

func TestParseTTLDuration_InvalidFormat(t *testing.T) {
	tests := []string{
		"abc",
		"30",
		"30dd",
		"d30",
		"-30d",
		"30.5d",
		"30 d",
		"30D", // Uppercase D is not valid in simple format
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := ParseTTLDuration(input)
			assert.Error(t, err, "expected error for input: %q", input)
		})
	}
}

func TestParseTTLDuration_TooShort(t *testing.T) {
	// 0 minutes is too short (minimum is 1 minute)
	_, err := ParseTTLDuration("0m")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too short")
}

func TestParseTTLDuration_TooLong(t *testing.T) {
	// 3651 days exceeds maximum (3650 days)
	_, err := ParseTTLDuration("3651d")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too long")
}

func TestParseTTLDuration_ISO8601_Empty(t *testing.T) {
	_, err := ParseTTLDuration("P")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestParseTTLDuration_ISO8601_OnlyT(t *testing.T) {
	_, err := ParseTTLDuration("PT")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestParseTTLDuration_ISO8601_Invalid(t *testing.T) {
	tests := []string{
		"PABC",
		"P30",
		"P-30D",
		"PD30",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := ParseTTLDuration(input)
			assert.Error(t, err, "expected error for input: %q", input)
		})
	}
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestPluralize(t *testing.T) {
	tests := []struct {
		value    int
		unit     string
		expected string
	}{
		{0, "day", "0 days"},
		{1, "day", "1 day"},
		{2, "day", "2 days"},
		{1, "hour", "1 hour"},
		{24, "hour", "24 hours"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := pluralize(tt.value, tt.unit)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseIntOrZero(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"0", 0},
		{"1", 1},
		{"100", 100},
		{"abc", 0},
		{"-1", -1},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseIntOrZero(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateDuration(t *testing.T) {
	tests := []struct {
		name      string
		duration  time.Duration
		expectErr bool
	}{
		{"minimum valid", 1 * time.Minute, false},
		{"maximum valid", 3650 * 24 * time.Hour, false},
		{"too short", 30 * time.Second, true},
		{"too long", 3651 * 24 * time.Hour, true},
		{"normal", 30 * 24 * time.Hour, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDuration(tt.duration)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
