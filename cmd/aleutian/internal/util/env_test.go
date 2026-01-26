// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package util

import (
	"errors"
	"testing"
)

// =============================================================================
// EnvVar.String() Tests
// =============================================================================

// TestEnvVar_String verifies KEY=VALUE format.
func TestEnvVar_String(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{"simple", "FOO", "bar", "FOO=bar"},
		{"empty value", "FOO", "", "FOO="},
		{"spaces in value", "FOO", "hello world", "FOO=hello world"},
		{"equals in value", "FOO", "a=b=c", "FOO=a=b=c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := EnvVar{Key: tt.key, Value: tt.value}
			if got := ev.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// =============================================================================
// EnvVar.Redacted() Tests
// =============================================================================

// TestEnvVar_Redacted verifies sensitive values are masked.
func TestEnvVar_Redacted(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		value     string
		sensitive bool
		want      string
	}{
		{"not sensitive", "FOO", "bar", false, "FOO=bar"},
		{"sensitive", "API_TOKEN", "secret123", true, "API_TOKEN=[REDACTED]"},
		{"sensitive empty value", "KEY", "", true, "KEY=[REDACTED]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := EnvVar{Key: tt.key, Value: tt.value, Sensitive: tt.sensitive}
			if got := ev.Redacted(); got != tt.want {
				t.Errorf("Redacted() = %q, want %q", got, tt.want)
			}
		})
	}
}

// =============================================================================
// EnvVar.Validate() Tests
// =============================================================================

// TestEnvVar_Validate_ValidKeys verifies valid key patterns.
func TestEnvVar_Validate_ValidKeys(t *testing.T) {
	validKeys := []string{
		"FOO",
		"foo",
		"FOO_BAR",
		"_FOO",
		"FOO123",
		"a",
		"A",
		"_",
		"__FOO__",
		"FOO_BAR_BAZ_123",
	}

	for _, key := range validKeys {
		t.Run(key, func(t *testing.T) {
			ev := EnvVar{Key: key, Value: "test"}
			if err := ev.Validate(); err != nil {
				t.Errorf("Validate() returned error for valid key %q: %v", key, err)
			}
		})
	}
}

// TestEnvVar_Validate_InvalidKeys verifies invalid key patterns are rejected.
func TestEnvVar_Validate_InvalidKeys(t *testing.T) {
	invalidKeys := []string{
		"",        // empty
		"1FOO",    // starts with number
		"FOO-BAR", // contains hyphen
		"FOO.BAR", // contains dot
		"FOO BAR", // contains space
		"FOO=BAR", // contains equals
		"FOO$BAR", // contains dollar
		"FOO@BAR", // contains at
		"foo bar", // spaces
		"123",     // all numbers
		"-FOO",    // starts with hyphen
		".FOO",    // starts with dot
	}

	for _, key := range invalidKeys {
		t.Run(key, func(t *testing.T) {
			ev := EnvVar{Key: key, Value: "test"}
			err := ev.Validate()
			if err == nil {
				t.Errorf("Validate() should return error for invalid key %q", key)
			}
			if !errors.Is(err, ErrInvalidEnvVarKey) {
				t.Errorf("Validate() error should wrap ErrInvalidEnvVarKey, got: %v", err)
			}
		})
	}
}

// =============================================================================
// NewEnvVars Tests
// =============================================================================

// TestNewEnvVars_Valid verifies creation with valid variables.
func TestNewEnvVars_Valid(t *testing.T) {
	envs, err := NewEnvVars(
		EnvVar{Key: "FOO", Value: "bar"},
		EnvVar{Key: "BAZ", Value: "qux", Sensitive: true},
	)

	if err != nil {
		t.Fatalf("NewEnvVars() returned error: %v", err)
	}
	if envs == nil {
		t.Fatal("NewEnvVars() returned nil")
	}
	if envs.Len() != 2 {
		t.Errorf("Len() = %d, want 2", envs.Len())
	}
}

// TestNewEnvVars_Empty verifies creation with no variables.
func TestNewEnvVars_Empty(t *testing.T) {
	envs, err := NewEnvVars()

	if err != nil {
		t.Fatalf("NewEnvVars() returned error: %v", err)
	}
	if envs.Len() != 0 {
		t.Errorf("Len() = %d, want 0", envs.Len())
	}
}

// TestNewEnvVars_InvalidKey verifies error on invalid key.
func TestNewEnvVars_InvalidKey(t *testing.T) {
	_, err := NewEnvVars(
		EnvVar{Key: "VALID", Value: "ok"},
		EnvVar{Key: "invalid-key", Value: "bad"},
	)

	if err == nil {
		t.Fatal("NewEnvVars() should return error for invalid key")
	}
	if !errors.Is(err, ErrInvalidEnvVarKey) {
		t.Errorf("error should wrap ErrInvalidEnvVarKey, got: %v", err)
	}
}

// =============================================================================
// MustNewEnvVars Tests
// =============================================================================

// TestMustNewEnvVars_Valid verifies creation with valid variables.
func TestMustNewEnvVars_Valid(t *testing.T) {
	envs := MustNewEnvVars(
		EnvVar{Key: "FOO", Value: "bar"},
	)

	if envs.Len() != 1 {
		t.Errorf("Len() = %d, want 1", envs.Len())
	}
}

// TestMustNewEnvVars_Panics verifies panic on invalid key.
func TestMustNewEnvVars_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustNewEnvVars should panic on invalid key")
		}
	}()

	MustNewEnvVars(EnvVar{Key: "invalid-key", Value: "bad"})
}

// =============================================================================
// EmptyEnvVars Tests
// =============================================================================

// TestEmptyEnvVars verifies empty collection creation.
func TestEmptyEnvVars(t *testing.T) {
	envs := EmptyEnvVars()

	if envs == nil {
		t.Fatal("EmptyEnvVars() returned nil")
	}
	if envs.Len() != 0 {
		t.Errorf("Len() = %d, want 0", envs.Len())
	}
}

// =============================================================================
// EnvVars.Add() Tests
// =============================================================================

// TestEnvVars_Add_Valid verifies adding valid variables.
func TestEnvVars_Add_Valid(t *testing.T) {
	envs := EmptyEnvVars()

	err := envs.Add("FOO", "bar", false)
	if err != nil {
		t.Fatalf("Add() returned error: %v", err)
	}

	err = envs.Add("SECRET", "hidden", true)
	if err != nil {
		t.Fatalf("Add() returned error: %v", err)
	}

	if envs.Len() != 2 {
		t.Errorf("Len() = %d, want 2", envs.Len())
	}
	if envs.Get("FOO") != "bar" {
		t.Errorf("Get(FOO) = %q, want %q", envs.Get("FOO"), "bar")
	}
}

// TestEnvVars_Add_InvalidKey verifies error on invalid key.
func TestEnvVars_Add_InvalidKey(t *testing.T) {
	envs := EmptyEnvVars()

	err := envs.Add("invalid-key", "value", false)
	if err == nil {
		t.Error("Add() should return error for invalid key")
	}

	// Should not add the invalid key
	if envs.Len() != 0 {
		t.Errorf("Len() = %d, want 0 (invalid key should not be added)", envs.Len())
	}
}

// =============================================================================
// EnvVars.MustAdd() Tests
// =============================================================================

// TestEnvVars_MustAdd_Valid verifies adding valid variables.
func TestEnvVars_MustAdd_Valid(t *testing.T) {
	envs := EmptyEnvVars()
	envs.MustAdd("FOO", "bar", false)

	if envs.Get("FOO") != "bar" {
		t.Errorf("Get(FOO) = %q, want %q", envs.Get("FOO"), "bar")
	}
}

// TestEnvVars_MustAdd_Panics verifies panic on invalid key.
func TestEnvVars_MustAdd_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustAdd should panic on invalid key")
		}
	}()

	envs := EmptyEnvVars()
	envs.MustAdd("invalid-key", "value", false)
}

// =============================================================================
// EnvVars.Get() Tests
// =============================================================================

// TestEnvVars_Get verifies value retrieval.
func TestEnvVars_Get(t *testing.T) {
	envs := MustNewEnvVars(
		EnvVar{Key: "FOO", Value: "bar"},
		EnvVar{Key: "BAZ", Value: "qux"},
	)

	tests := []struct {
		key  string
		want string
	}{
		{"FOO", "bar"},
		{"BAZ", "qux"},
		{"NONEXISTENT", ""},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := envs.Get(tt.key); got != tt.want {
				t.Errorf("Get(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

// TestEnvVars_Get_NilReceiver verifies nil safety.
func TestEnvVars_Get_NilReceiver(t *testing.T) {
	var envs *EnvVars

	if got := envs.Get("FOO"); got != "" {
		t.Errorf("Get() on nil = %q, want empty string", got)
	}
}

// TestEnvVars_Get_DuplicateKeys verifies last value wins.
func TestEnvVars_Get_DuplicateKeys(t *testing.T) {
	envs := MustNewEnvVars(
		EnvVar{Key: "FOO", Value: "first"},
		EnvVar{Key: "FOO", Value: "second"},
		EnvVar{Key: "FOO", Value: "third"},
	)

	if got := envs.Get("FOO"); got != "third" {
		t.Errorf("Get(FOO) = %q, want %q (last value)", got, "third")
	}
}

// =============================================================================
// EnvVars.Has() Tests
// =============================================================================

// TestEnvVars_Has verifies key existence check.
func TestEnvVars_Has(t *testing.T) {
	envs := MustNewEnvVars(
		EnvVar{Key: "FOO", Value: "bar"},
	)

	if !envs.Has("FOO") {
		t.Error("Has(FOO) = false, want true")
	}
	if envs.Has("NONEXISTENT") {
		t.Error("Has(NONEXISTENT) = true, want false")
	}
}

// TestEnvVars_Has_NilReceiver verifies nil safety.
func TestEnvVars_Has_NilReceiver(t *testing.T) {
	var envs *EnvVars

	if envs.Has("FOO") {
		t.Error("Has() on nil = true, want false")
	}
}

// =============================================================================
// EnvVars.Len() Tests
// =============================================================================

// TestEnvVars_Len verifies count.
func TestEnvVars_Len(t *testing.T) {
	tests := []struct {
		name string
		vars []EnvVar
		want int
	}{
		{"empty", []EnvVar{}, 0},
		{"one", []EnvVar{{Key: "A", Value: "1"}}, 1},
		{"three", []EnvVar{{Key: "A", Value: "1"}, {Key: "B", Value: "2"}, {Key: "C", Value: "3"}}, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envs := MustNewEnvVars(tt.vars...)
			if got := envs.Len(); got != tt.want {
				t.Errorf("Len() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestEnvVars_Len_NilReceiver verifies nil safety.
func TestEnvVars_Len_NilReceiver(t *testing.T) {
	var envs *EnvVars

	if got := envs.Len(); got != 0 {
		t.Errorf("Len() on nil = %d, want 0", got)
	}
}

// =============================================================================
// EnvVars.ToSlice() Tests
// =============================================================================

// TestEnvVars_ToSlice verifies slice conversion.
func TestEnvVars_ToSlice(t *testing.T) {
	envs := MustNewEnvVars(
		EnvVar{Key: "FOO", Value: "bar"},
		EnvVar{Key: "BAZ", Value: "qux"},
	)

	got := envs.ToSlice()
	if len(got) != 2 {
		t.Fatalf("ToSlice() len = %d, want 2", len(got))
	}

	// Check both are present (order may vary)
	found := make(map[string]bool)
	for _, s := range got {
		found[s] = true
	}
	if !found["FOO=bar"] || !found["BAZ=qux"] {
		t.Errorf("ToSlice() = %v, want to contain FOO=bar and BAZ=qux", got)
	}
}

// TestEnvVars_ToSlice_NilReceiver verifies nil safety.
func TestEnvVars_ToSlice_NilReceiver(t *testing.T) {
	var envs *EnvVars

	got := envs.ToSlice()
	if got != nil {
		t.Errorf("ToSlice() on nil = %v, want nil", got)
	}
}

// =============================================================================
// EnvVars.ToMap() Tests
// =============================================================================

// TestEnvVars_ToMap verifies map conversion.
func TestEnvVars_ToMap(t *testing.T) {
	envs := MustNewEnvVars(
		EnvVar{Key: "FOO", Value: "bar"},
		EnvVar{Key: "BAZ", Value: "qux"},
	)

	got := envs.ToMap()
	if len(got) != 2 {
		t.Fatalf("ToMap() len = %d, want 2", len(got))
	}
	if got["FOO"] != "bar" {
		t.Errorf("ToMap()[FOO] = %q, want %q", got["FOO"], "bar")
	}
	if got["BAZ"] != "qux" {
		t.Errorf("ToMap()[BAZ] = %q, want %q", got["BAZ"], "qux")
	}
}

// TestEnvVars_ToMap_NilReceiver verifies nil safety.
func TestEnvVars_ToMap_NilReceiver(t *testing.T) {
	var envs *EnvVars

	got := envs.ToMap()
	if got != nil {
		t.Errorf("ToMap() on nil = %v, want nil", got)
	}
}

// TestEnvVars_ToMap_DuplicateKeys verifies last value wins.
func TestEnvVars_ToMap_DuplicateKeys(t *testing.T) {
	envs := MustNewEnvVars(
		EnvVar{Key: "FOO", Value: "first"},
		EnvVar{Key: "FOO", Value: "second"},
	)

	got := envs.ToMap()
	if got["FOO"] != "second" {
		t.Errorf("ToMap()[FOO] = %q, want %q (last value)", got["FOO"], "second")
	}
}

// =============================================================================
// EnvVars.RedactedSlice() Tests
// =============================================================================

// TestEnvVars_RedactedSlice verifies redacted slice conversion.
func TestEnvVars_RedactedSlice(t *testing.T) {
	envs := MustNewEnvVars(
		EnvVar{Key: "PUBLIC", Value: "visible", Sensitive: false},
		EnvVar{Key: "SECRET", Value: "hidden", Sensitive: true},
	)

	got := envs.RedactedSlice()
	if len(got) != 2 {
		t.Fatalf("RedactedSlice() len = %d, want 2", len(got))
	}

	found := make(map[string]bool)
	for _, s := range got {
		found[s] = true
	}
	if !found["PUBLIC=visible"] {
		t.Error("RedactedSlice() should contain PUBLIC=visible")
	}
	if !found["SECRET=[REDACTED]"] {
		t.Error("RedactedSlice() should contain SECRET=[REDACTED]")
	}
	if found["SECRET=hidden"] {
		t.Error("RedactedSlice() should NOT contain SECRET=hidden")
	}
}

// TestEnvVars_RedactedSlice_NilReceiver verifies nil safety.
func TestEnvVars_RedactedSlice_NilReceiver(t *testing.T) {
	var envs *EnvVars

	got := envs.RedactedSlice()
	if got != nil {
		t.Errorf("RedactedSlice() on nil = %v, want nil", got)
	}
}

// =============================================================================
// EnvVars.Merge() Tests
// =============================================================================

// TestEnvVars_Merge verifies merging with precedence.
func TestEnvVars_Merge(t *testing.T) {
	base := MustNewEnvVars(
		EnvVar{Key: "FOO", Value: "base"},
		EnvVar{Key: "BAR", Value: "only_in_base"},
	)
	override := MustNewEnvVars(
		EnvVar{Key: "FOO", Value: "override"},
		EnvVar{Key: "BAZ", Value: "only_in_override"},
	)

	merged := base.Merge(override)

	if merged.Get("FOO") != "override" {
		t.Errorf("Merge() FOO = %q, want %q (override takes precedence)", merged.Get("FOO"), "override")
	}
	if merged.Get("BAR") != "only_in_base" {
		t.Errorf("Merge() BAR = %q, want %q", merged.Get("BAR"), "only_in_base")
	}
	if merged.Get("BAZ") != "only_in_override" {
		t.Errorf("Merge() BAZ = %q, want %q", merged.Get("BAZ"), "only_in_override")
	}
}

// TestEnvVars_Merge_NilOther verifies nil other returns copy of base.
func TestEnvVars_Merge_NilOther(t *testing.T) {
	base := MustNewEnvVars(EnvVar{Key: "FOO", Value: "bar"})

	merged := base.Merge(nil)

	if merged.Get("FOO") != "bar" {
		t.Errorf("Merge(nil) FOO = %q, want %q", merged.Get("FOO"), "bar")
	}
	// Verify it's a copy, not same reference
	if merged == base {
		t.Error("Merge(nil) should return a copy, not same reference")
	}
}

// TestEnvVars_Merge_NilBase verifies nil base returns copy of other.
func TestEnvVars_Merge_NilBase(t *testing.T) {
	var base *EnvVars
	other := MustNewEnvVars(EnvVar{Key: "FOO", Value: "bar"})

	merged := base.Merge(other)

	if merged.Get("FOO") != "bar" {
		t.Errorf("nil.Merge(other) FOO = %q, want %q", merged.Get("FOO"), "bar")
	}
}

// TestEnvVars_Merge_BothNil verifies both nil returns empty.
func TestEnvVars_Merge_BothNil(t *testing.T) {
	var base *EnvVars

	merged := base.Merge(nil)

	if merged == nil {
		t.Fatal("Merge() returned nil, want empty EnvVars")
	}
	if merged.Len() != 0 {
		t.Errorf("Merge() Len = %d, want 0", merged.Len())
	}
}

// =============================================================================
// EnvVars.Clone() Tests
// =============================================================================

// TestEnvVars_Clone verifies deep copy.
func TestEnvVars_Clone(t *testing.T) {
	original := MustNewEnvVars(EnvVar{Key: "FOO", Value: "bar"})

	clone := original.Clone()

	// Verify values are equal
	if clone.Get("FOO") != "bar" {
		t.Errorf("Clone() FOO = %q, want %q", clone.Get("FOO"), "bar")
	}

	// Verify it's a different object
	if clone == original {
		t.Error("Clone() should return different object")
	}

	// Modify clone, original should be unchanged
	clone.MustAdd("BAZ", "qux", false)
	if original.Has("BAZ") {
		t.Error("Modifying clone should not affect original")
	}
}

// TestEnvVars_Clone_NilReceiver verifies nil safety.
func TestEnvVars_Clone_NilReceiver(t *testing.T) {
	var envs *EnvVars

	clone := envs.Clone()
	if clone != nil {
		t.Errorf("Clone() on nil = %v, want nil", clone)
	}
}

// =============================================================================
// FromMap Tests
// =============================================================================

// TestFromMap_Valid verifies map conversion.
func TestFromMap_Valid(t *testing.T) {
	m := map[string]string{
		"FOO":    "bar",
		"SECRET": "hidden",
	}

	envs, err := FromMap(m, []string{"SECRET"})
	if err != nil {
		t.Fatalf("FromMap() returned error: %v", err)
	}

	if envs.Get("FOO") != "bar" {
		t.Errorf("Get(FOO) = %q, want %q", envs.Get("FOO"), "bar")
	}

	// Verify SECRET is marked sensitive
	slice := envs.RedactedSlice()
	foundRedacted := false
	for _, s := range slice {
		if s == "SECRET=[REDACTED]" {
			foundRedacted = true
		}
	}
	if !foundRedacted {
		t.Error("SECRET should be marked sensitive")
	}
}

// TestFromMap_NilMap verifies nil map handling.
func TestFromMap_NilMap(t *testing.T) {
	envs, err := FromMap(nil, nil)
	if err != nil {
		t.Fatalf("FromMap(nil) returned error: %v", err)
	}
	if envs.Len() != 0 {
		t.Errorf("FromMap(nil) Len = %d, want 0", envs.Len())
	}
}

// TestFromMap_InvalidKey verifies error on invalid key.
func TestFromMap_InvalidKey(t *testing.T) {
	m := map[string]string{
		"VALID":       "ok",
		"invalid-key": "bad",
	}

	_, err := FromMap(m, nil)
	if err == nil {
		t.Error("FromMap() should return error for invalid key")
	}
}

// TestFromMap_AutoDetectSensitive verifies automatic sensitivity detection.
func TestFromMap_AutoDetectSensitive(t *testing.T) {
	sensitivePatterns := []string{
		"API_TOKEN",
		"SECRET_KEY",
		"MY_PASSWORD",
		"AUTH_HEADER",
		"CREDENTIAL_FILE",
	}

	for _, key := range sensitivePatterns {
		t.Run(key, func(t *testing.T) {
			m := map[string]string{key: "value"}
			envs, err := FromMap(m, nil)
			if err != nil {
				t.Fatalf("FromMap() error: %v", err)
			}

			slice := envs.RedactedSlice()
			if len(slice) == 0 {
				t.Fatal("RedactedSlice() is empty")
			}
			if slice[0] != key+"=[REDACTED]" {
				t.Errorf("Key %q should be auto-detected as sensitive, got %q", key, slice[0])
			}
		})
	}
}

// =============================================================================
// Error Variable Tests
// =============================================================================

// TestErrInvalidEnvVarKey_Message verifies error message.
func TestErrInvalidEnvVarKey_Message(t *testing.T) {
	if ErrInvalidEnvVarKey == nil {
		t.Fatal("ErrInvalidEnvVarKey is nil")
	}
	if ErrInvalidEnvVarKey.Error() != "invalid environment variable key" {
		t.Errorf("ErrInvalidEnvVarKey = %q, unexpected", ErrInvalidEnvVarKey.Error())
	}
}
