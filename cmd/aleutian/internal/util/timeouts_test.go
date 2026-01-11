// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package util

import (
	"testing"
	"time"
)

// =============================================================================
// EnforceMinTimeout Tests
// =============================================================================

// TestEnforceMinTimeout_ValidAboveMinimum verifies that values above minimum
// are returned unchanged.
//
// # Description
//
// When the requested timeout exceeds the minimum, the requested value
// should be returned as-is.
//
// # Inputs
//
//   - requested: Various durations above the minimum
//   - minimum: A baseline minimum duration
//
// # Outputs
//
//   - The requested duration unchanged
func TestEnforceMinTimeout_ValidAboveMinimum(t *testing.T) {
	tests := []struct {
		name      string
		requested time.Duration
		minimum   time.Duration
		want      time.Duration
	}{
		{
			name:      "requested equals minimum",
			requested: 5 * time.Second,
			minimum:   5 * time.Second,
			want:      5 * time.Second,
		},
		{
			name:      "requested above minimum",
			requested: 10 * time.Second,
			minimum:   5 * time.Second,
			want:      10 * time.Second,
		},
		{
			name:      "large requested value",
			requested: 5 * time.Minute,
			minimum:   1 * time.Second,
			want:      5 * time.Minute,
		},
		{
			name:      "millisecond precision",
			requested: 1500 * time.Millisecond,
			minimum:   1000 * time.Millisecond,
			want:      1500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnforceMinTimeout(tt.requested, tt.minimum)
			if got != tt.want {
				t.Errorf("EnforceMinTimeout(%v, %v) = %v, want %v",
					tt.requested, tt.minimum, got, tt.want)
			}
		})
	}
}

// TestEnforceMinTimeout_BelowMinimum verifies that values below minimum
// are raised to the minimum.
//
// # Description
//
// When the requested timeout is below the minimum, the minimum value
// should be returned instead.
func TestEnforceMinTimeout_BelowMinimum(t *testing.T) {
	tests := []struct {
		name      string
		requested time.Duration
		minimum   time.Duration
		want      time.Duration
	}{
		{
			name:      "requested below minimum",
			requested: 1 * time.Second,
			minimum:   5 * time.Second,
			want:      5 * time.Second,
		},
		{
			name:      "requested is zero",
			requested: 0,
			minimum:   5 * time.Second,
			want:      5 * time.Second,
		},
		{
			name:      "requested is negative",
			requested: -1 * time.Second,
			minimum:   5 * time.Second,
			want:      5 * time.Second,
		},
		{
			name:      "very small requested",
			requested: 1 * time.Nanosecond,
			minimum:   1 * time.Millisecond,
			want:      1 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnforceMinTimeout(tt.requested, tt.minimum)
			if got != tt.want {
				t.Errorf("EnforceMinTimeout(%v, %v) = %v, want %v",
					tt.requested, tt.minimum, got, tt.want)
			}
		})
	}
}

// =============================================================================
// EnforceDefaultTimeout Tests
// =============================================================================

// TestEnforceDefaultTimeout_ValidPositive verifies that positive values
// are returned unchanged.
//
// # Description
//
// When the requested timeout is positive, it should be returned as-is
// regardless of the default value.
func TestEnforceDefaultTimeout_ValidPositive(t *testing.T) {
	tests := []struct {
		name       string
		requested  time.Duration
		defaultVal time.Duration
		want       time.Duration
	}{
		{
			name:       "small positive value",
			requested:  1 * time.Millisecond,
			defaultVal: 30 * time.Second,
			want:       1 * time.Millisecond,
		},
		{
			name:       "requested equals default",
			requested:  30 * time.Second,
			defaultVal: 30 * time.Second,
			want:       30 * time.Second,
		},
		{
			name:       "requested above default",
			requested:  5 * time.Minute,
			defaultVal: 30 * time.Second,
			want:       5 * time.Minute,
		},
		{
			name:       "requested below default but positive",
			requested:  1 * time.Second,
			defaultVal: 30 * time.Second,
			want:       1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnforceDefaultTimeout(tt.requested, tt.defaultVal)
			if got != tt.want {
				t.Errorf("EnforceDefaultTimeout(%v, %v) = %v, want %v",
					tt.requested, tt.defaultVal, got, tt.want)
			}
		})
	}
}

// TestEnforceDefaultTimeout_InvalidValues verifies that zero and negative
// values are replaced with the default.
//
// # Description
//
// When the requested timeout is zero or negative, the default value
// should be returned.
func TestEnforceDefaultTimeout_InvalidValues(t *testing.T) {
	tests := []struct {
		name       string
		requested  time.Duration
		defaultVal time.Duration
		want       time.Duration
	}{
		{
			name:       "zero requested",
			requested:  0,
			defaultVal: 30 * time.Second,
			want:       30 * time.Second,
		},
		{
			name:       "negative requested",
			requested:  -5 * time.Second,
			defaultVal: 30 * time.Second,
			want:       30 * time.Second,
		},
		{
			name:       "large negative requested",
			requested:  -1 * time.Hour,
			defaultVal: 1 * time.Minute,
			want:       1 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnforceDefaultTimeout(tt.requested, tt.defaultVal)
			if got != tt.want {
				t.Errorf("EnforceDefaultTimeout(%v, %v) = %v, want %v",
					tt.requested, tt.defaultVal, got, tt.want)
			}
		})
	}
}

// =============================================================================
// TimeoutConfig Tests
// =============================================================================

// TestNewTimeoutConfig_Defaults verifies that NewTimeoutConfig returns
// sensible default values.
//
// # Description
//
// The constructor should return a TimeoutConfig with all fields set to
// their documented default values.
func TestNewTimeoutConfig_Defaults(t *testing.T) {
	cfg := NewTimeoutConfig()

	tests := []struct {
		name     string
		got      time.Duration
		want     time.Duration
		wantMin  time.Duration
		wantName string
	}{
		{
			name:     "HTTP timeout",
			got:      cfg.HTTP,
			want:     DefaultHTTPTimeout,
			wantMin:  MinHTTPTimeout,
			wantName: "HTTP",
		},
		{
			name:     "TCP timeout",
			got:      cfg.TCP,
			want:     DefaultTCPTimeout,
			wantMin:  MinTCPTimeout,
			wantName: "TCP",
		},
		{
			name:     "Process timeout",
			got:      cfg.Process,
			want:     DefaultProcessTimeout,
			wantMin:  MinProcessTimeout,
			wantName: "Process",
		},
		{
			name:     "Compose timeout",
			got:      cfg.Compose,
			want:     DefaultComposeTimeout,
			wantMin:  MinProcessTimeout,
			wantName: "Compose",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("NewTimeoutConfig().%s = %v, want %v",
					tt.wantName, tt.got, tt.want)
			}
			// Also verify defaults are at least the minimum
			if tt.got < tt.wantMin {
				t.Errorf("NewTimeoutConfig().%s = %v, below minimum %v",
					tt.wantName, tt.got, tt.wantMin)
			}
		})
	}
}

// TestTimeoutConfig_Validated_EnforcesMinimums verifies that Validated
// raises all values to at least their minimums.
//
// # Description
//
// When any timeout value is below its minimum, Validated should return
// a copy with that value raised to the minimum.
func TestTimeoutConfig_Validated_EnforcesMinimums(t *testing.T) {
	tests := []struct {
		name string
		cfg  *TimeoutConfig
		want TimeoutConfig
	}{
		{
			name: "all zeros become minimums",
			cfg: &TimeoutConfig{
				HTTP:    0,
				TCP:     0,
				Process: 0,
				Compose: 0,
			},
			want: TimeoutConfig{
				HTTP:    MinHTTPTimeout,
				TCP:     MinTCPTimeout,
				Process: MinProcessTimeout,
				Compose: MinProcessTimeout, // Compose uses MinProcessTimeout
			},
		},
		{
			name: "negative values become minimums",
			cfg: &TimeoutConfig{
				HTTP:    -1 * time.Second,
				TCP:     -1 * time.Second,
				Process: -1 * time.Second,
				Compose: -1 * time.Second,
			},
			want: TimeoutConfig{
				HTTP:    MinHTTPTimeout,
				TCP:     MinTCPTimeout,
				Process: MinProcessTimeout,
				Compose: MinProcessTimeout,
			},
		},
		{
			name: "values above minimum unchanged",
			cfg: &TimeoutConfig{
				HTTP:    1 * time.Minute,
				TCP:     30 * time.Second,
				Process: 5 * time.Minute,
				Compose: 10 * time.Minute,
			},
			want: TimeoutConfig{
				HTTP:    1 * time.Minute,
				TCP:     30 * time.Second,
				Process: 5 * time.Minute,
				Compose: 10 * time.Minute,
			},
		},
		{
			name: "mixed valid and invalid",
			cfg: &TimeoutConfig{
				HTTP:    0,                // Below minimum
				TCP:     30 * time.Second, // Valid
				Process: -1 * time.Second, // Below minimum
				Compose: 10 * time.Minute, // Valid
			},
			want: TimeoutConfig{
				HTTP:    MinHTTPTimeout,
				TCP:     30 * time.Second,
				Process: MinProcessTimeout,
				Compose: 10 * time.Minute,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.Validated()

			if got.HTTP != tt.want.HTTP {
				t.Errorf("Validated().HTTP = %v, want %v", got.HTTP, tt.want.HTTP)
			}
			if got.TCP != tt.want.TCP {
				t.Errorf("Validated().TCP = %v, want %v", got.TCP, tt.want.TCP)
			}
			if got.Process != tt.want.Process {
				t.Errorf("Validated().Process = %v, want %v", got.Process, tt.want.Process)
			}
			if got.Compose != tt.want.Compose {
				t.Errorf("Validated().Compose = %v, want %v", got.Compose, tt.want.Compose)
			}
		})
	}
}

// TestTimeoutConfig_Validated_DoesNotMutateOriginal verifies that Validated
// returns a copy without modifying the original.
//
// # Description
//
// Validated should return a new TimeoutConfig, leaving the original unchanged.
func TestTimeoutConfig_Validated_DoesNotMutateOriginal(t *testing.T) {
	original := &TimeoutConfig{
		HTTP:    0,
		TCP:     0,
		Process: 0,
		Compose: 0,
	}

	// Store original values
	originalHTTP := original.HTTP
	originalTCP := original.TCP
	originalProcess := original.Process
	originalCompose := original.Compose

	// Call Validated
	_ = original.Validated()

	// Verify original unchanged
	if original.HTTP != originalHTTP {
		t.Errorf("Original.HTTP was mutated: got %v, want %v", original.HTTP, originalHTTP)
	}
	if original.TCP != originalTCP {
		t.Errorf("Original.TCP was mutated: got %v, want %v", original.TCP, originalTCP)
	}
	if original.Process != originalProcess {
		t.Errorf("Original.Process was mutated: got %v, want %v", original.Process, originalProcess)
	}
	if original.Compose != originalCompose {
		t.Errorf("Original.Compose was mutated: got %v, want %v", original.Compose, originalCompose)
	}
}

// =============================================================================
// Interface Satisfaction Tests
// =============================================================================

// TestTimeoutConfig_ImplementsTimeoutValidator verifies interface satisfaction.
//
// # Description
//
// TimeoutConfig must implement the TimeoutValidator interface.
func TestTimeoutConfig_ImplementsTimeoutValidator(t *testing.T) {
	var _ TimeoutValidator = (*TimeoutConfig)(nil)

	// Also test that we can use it through the interface
	cfg := &TimeoutConfig{HTTP: 0}
	var validator TimeoutValidator = cfg

	validated := validator.Validated()
	if validated.HTTP != MinHTTPTimeout {
		t.Errorf("Interface call: Validated().HTTP = %v, want %v",
			validated.HTTP, MinHTTPTimeout)
	}
}

// =============================================================================
// Constants Tests
// =============================================================================

// TestConstants_Positive verifies all timeout constants are positive.
//
// # Description
//
// All minimum and default timeout constants must be positive durations
// to prevent infinite hangs or invalid configurations.
func TestConstants_Positive(t *testing.T) {
	tests := []struct {
		name  string
		value time.Duration
	}{
		{"MinHTTPTimeout", MinHTTPTimeout},
		{"MinTCPTimeout", MinTCPTimeout},
		{"MinProcessTimeout", MinProcessTimeout},
		{"DefaultHTTPTimeout", DefaultHTTPTimeout},
		{"DefaultTCPTimeout", DefaultTCPTimeout},
		{"DefaultProcessTimeout", DefaultProcessTimeout},
		{"DefaultComposeTimeout", DefaultComposeTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value <= 0 {
				t.Errorf("%s = %v, want positive duration", tt.name, tt.value)
			}
		})
	}
}

// TestConstants_DefaultsExceedMinimums verifies defaults are at least minimums.
//
// # Description
//
// Each default timeout should be greater than or equal to its corresponding
// minimum to ensure NewTimeoutConfig always returns valid values.
func TestConstants_DefaultsExceedMinimums(t *testing.T) {
	tests := []struct {
		name       string
		defaultVal time.Duration
		minimum    time.Duration
	}{
		{
			name:       "HTTP",
			defaultVal: DefaultHTTPTimeout,
			minimum:    MinHTTPTimeout,
		},
		{
			name:       "TCP",
			defaultVal: DefaultTCPTimeout,
			minimum:    MinTCPTimeout,
		},
		{
			name:       "Process",
			defaultVal: DefaultProcessTimeout,
			minimum:    MinProcessTimeout,
		},
		{
			name:       "Compose",
			defaultVal: DefaultComposeTimeout,
			minimum:    MinProcessTimeout, // Compose uses Process minimum
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.defaultVal < tt.minimum {
				t.Errorf("Default%sTimeout (%v) < Min%sTimeout (%v)",
					tt.name, tt.defaultVal, tt.name, tt.minimum)
			}
		})
	}
}

// =============================================================================
// Edge Case Tests
// =============================================================================

// TestEnforceMinTimeout_MaxDuration verifies behavior with maximum duration.
//
// # Description
//
// The function should handle the maximum time.Duration value correctly.
func TestEnforceMinTimeout_MaxDuration(t *testing.T) {
	maxDuration := time.Duration(1<<63 - 1) // Max int64 nanoseconds

	got := EnforceMinTimeout(maxDuration, 1*time.Second)
	if got != maxDuration {
		t.Errorf("EnforceMinTimeout(max, 1s) = %v, want %v", got, maxDuration)
	}
}

// TestEnforceDefaultTimeout_MaxDuration verifies behavior with maximum duration.
func TestEnforceDefaultTimeout_MaxDuration(t *testing.T) {
	maxDuration := time.Duration(1<<63 - 1)

	got := EnforceDefaultTimeout(maxDuration, 1*time.Second)
	if got != maxDuration {
		t.Errorf("EnforceDefaultTimeout(max, 1s) = %v, want %v", got, maxDuration)
	}
}
