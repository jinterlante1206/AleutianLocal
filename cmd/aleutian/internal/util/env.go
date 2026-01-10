// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package util

import (
	"fmt"
	"regexp"
	"strings"
)

// =============================================================================
// Package-level Variables
// =============================================================================

// envVarKeyPattern validates environment variable key names.
// Keys must:
//   - Start with a letter or underscore
//   - Contain only alphanumeric characters and underscores
//   - Not be empty
//
// This follows POSIX naming conventions and prevents shell metacharacter
// injection attacks.
var envVarKeyPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ErrInvalidEnvVarKey is returned when an environment variable key is invalid.
var ErrInvalidEnvVarKey = fmt.Errorf("invalid environment variable key")

// =============================================================================
// EnvVar Type
// =============================================================================

// EnvVar represents a single environment variable.
//
// # Description
//
// A typed representation of an environment variable with validation
// and sensitivity marking for secure logging. Keys are validated
// against POSIX naming conventions.
//
// # Thread Safety
//
// EnvVar is safe for concurrent reads. Do not modify after creation.
//
// # Example
//
//	ev := EnvVar{Key: "API_TOKEN", Value: "secret123", Sensitive: true}
//	fmt.Println(ev.Redacted()) // API_TOKEN=[REDACTED]
//
// # Limitations
//
//   - Value is not validated (can be empty or contain any characters)
//   - Key validation only happens when Validate() is called explicitly
type EnvVar struct {
	// Key is the environment variable name.
	// Must match pattern: ^[a-zA-Z_][a-zA-Z0-9_]*$
	Key string

	// Value is the environment variable value.
	// May be empty string (valid in POSIX).
	Value string

	// Sensitive indicates this value should be redacted in logs.
	Sensitive bool
}

// =============================================================================
// EnvVar Methods
// =============================================================================

// String returns the KEY=VALUE format.
//
// # Description
//
// Returns the environment variable in standard KEY=VALUE format
// suitable for shell usage or exec.Cmd.Env.
//
// # Inputs
//
//   - e: The EnvVar receiver
//
// # Outputs
//
//   - string: Formatted as "KEY=VALUE"
//
// # Example
//
//	ev := EnvVar{Key: "FOO", Value: "bar"}
//	fmt.Println(ev.String()) // "FOO=bar"
func (e EnvVar) String() string {
	return fmt.Sprintf("%s=%s", e.Key, e.Value)
}

// Redacted returns KEY=[REDACTED] for sensitive vars, otherwise String().
//
// # Description
//
// Returns a log-safe representation of the environment variable.
// Sensitive values are replaced with [REDACTED] to prevent
// accidental exposure in logs.
//
// # Inputs
//
//   - e: The EnvVar receiver
//
// # Outputs
//
//   - string: Formatted as "KEY=[REDACTED]" if sensitive, else "KEY=VALUE"
//
// # Example
//
//	secret := EnvVar{Key: "TOKEN", Value: "abc123", Sensitive: true}
//	fmt.Println(secret.Redacted()) // "TOKEN=[REDACTED]"
func (e EnvVar) Redacted() string {
	if e.Sensitive {
		return fmt.Sprintf("%s=[REDACTED]", e.Key)
	}
	return e.String()
}

// Validate checks if the key is valid.
//
// # Description
//
// Validates the environment variable key against POSIX naming conventions.
// Keys must start with a letter or underscore and contain only
// alphanumeric characters and underscores.
//
// # Inputs
//
//   - e: The EnvVar receiver
//
// # Outputs
//
//   - error: ErrInvalidEnvVarKey wrapped with details if key is invalid
//
// # Example
//
//	ev := EnvVar{Key: "invalid-key", Value: "test"}
//	if err := ev.Validate(); err != nil {
//	    // Handle invalid key
//	}
//
// # Limitations
//
//   - Does not validate value content
func (e EnvVar) Validate() error {
	if !envVarKeyPattern.MatchString(e.Key) {
		return fmt.Errorf("%w: %q must match pattern [a-zA-Z_][a-zA-Z0-9_]*", ErrInvalidEnvVarKey, e.Key)
	}
	return nil
}

// =============================================================================
// EnvVars Type
// =============================================================================

// EnvVars is a validated collection of environment variables.
//
// # Description
//
// Provides a type-safe container for environment variables with
// validation, merging, and redaction capabilities. Replaces raw
// map[string]string for better type safety and security.
//
// # Thread Safety
//
// EnvVars is NOT thread-safe. Do not modify concurrently.
// For concurrent access, use external synchronization or Clone().
//
// # Example
//
//	envs, err := NewEnvVars(
//	    EnvVar{Key: "OLLAMA_MODEL", Value: "llama2"},
//	    EnvVar{Key: "API_TOKEN", Value: "secret", Sensitive: true},
//	)
//	if err != nil {
//	    return err
//	}
//	fmt.Println(envs.RedactedSlice()) // Safe for logging
//
// # Limitations
//
//   - Duplicate keys are allowed (last wins in ToMap/Get)
//   - Not thread-safe for concurrent modifications
type EnvVars struct {
	vars []EnvVar
}

// =============================================================================
// EnvVars Constructor Functions
// =============================================================================

// NewEnvVars creates a validated EnvVars collection.
//
// # Description
//
// Creates a new EnvVars after validating all provided variables.
// Returns an error if any key is invalid.
//
// # Inputs
//
//   - vars: Environment variables to include
//
// # Outputs
//
//   - *EnvVars: Validated collection
//   - error: Non-nil if any key is invalid
//
// # Example
//
//	envs, err := NewEnvVars(
//	    EnvVar{Key: "FOO", Value: "bar"},
//	)
//
// # Limitations
//
//   - Duplicate keys are allowed (last wins in ToMap)
//
// # Assumptions
//
//   - Caller handles validation errors appropriately
func NewEnvVars(vars ...EnvVar) (*EnvVars, error) {
	for _, v := range vars {
		if err := v.Validate(); err != nil {
			return nil, err
		}
	}
	return &EnvVars{vars: vars}, nil
}

// MustNewEnvVars creates EnvVars or panics.
//
// # Description
//
// Like NewEnvVars but panics on validation error. Use only for
// compile-time constants where you're certain the keys are valid.
//
// # Inputs
//
//   - vars: Environment variables to include
//
// # Outputs
//
//   - *EnvVars: Validated collection
//
// # Example
//
//	var defaultEnvs = MustNewEnvVars(
//	    EnvVar{Key: "LOG_LEVEL", Value: "info"},
//	)
//
// # Assumptions
//
//   - Keys are known valid at compile time
func MustNewEnvVars(vars ...EnvVar) *EnvVars {
	ev, err := NewEnvVars(vars...)
	if err != nil {
		panic(err)
	}
	return ev
}

// EmptyEnvVars returns an empty EnvVars.
//
// # Description
//
// Creates an empty EnvVars collection that can be populated
// using Add() method calls.
//
// # Outputs
//
//   - *EnvVars: Empty collection
//
// # Example
//
//	envs := EmptyEnvVars()
//	envs.Add("FOO", "bar", false)
func EmptyEnvVars() *EnvVars {
	return &EnvVars{vars: []EnvVar{}}
}

// =============================================================================
// EnvVars Methods
// =============================================================================

// Add appends a validated environment variable.
//
// # Description
//
// Adds a new environment variable to the collection after validation.
// The variable is appended to the end. If the key is invalid,
// returns an error and does not add the variable.
//
// # Inputs
//
//   - key: Environment variable name
//   - value: Environment variable value
//   - sensitive: Whether to redact in logs
//
// # Outputs
//
//   - error: Non-nil if key is invalid
//
// # Example
//
//	envs := EmptyEnvVars()
//	envs.Add("FOO", "bar", false)
//	envs.Add("SECRET", "hidden", true)
//
// # Assumptions
//
//   - Receiver is not nil
func (e *EnvVars) Add(key, value string, sensitive bool) error {
	ev := EnvVar{Key: key, Value: value, Sensitive: sensitive}
	if err := ev.Validate(); err != nil {
		return err
	}
	e.vars = append(e.vars, ev)
	return nil
}

// MustAdd adds a variable or panics.
//
// # Description
//
// Like Add but panics on validation error. Use only when you're
// certain the key is valid.
//
// # Inputs
//
//   - key: Environment variable name
//   - value: Environment variable value
//   - sensitive: Whether to redact in logs
//
// # Assumptions
//
//   - Receiver is not nil
//   - Key is known valid
func (e *EnvVars) MustAdd(key, value string, sensitive bool) {
	if err := e.Add(key, value, sensitive); err != nil {
		panic(err)
	}
}

// Get returns the value for a key, or empty string if not found.
//
// # Description
//
// Retrieves the value associated with a key. If there are duplicate
// keys, returns the last value (matching shell semantics).
// Returns empty string if the key is not found or receiver is nil.
//
// # Inputs
//
//   - key: Environment variable name to look up
//
// # Outputs
//
//   - string: Value if found, empty string otherwise
//
// # Example
//
//	value := envs.Get("FOO")
//	if value == "" {
//	    // Key not found or has empty value
//	}
func (e *EnvVars) Get(key string) string {
	if e == nil {
		return ""
	}
	// Return last value for key (in case of duplicates)
	for i := len(e.vars) - 1; i >= 0; i-- {
		if e.vars[i].Key == key {
			return e.vars[i].Value
		}
	}
	return ""
}

// Has returns true if the key exists.
//
// # Description
//
// Checks whether a key exists in the collection. Returns false
// if the receiver is nil.
//
// # Inputs
//
//   - key: Environment variable name to check
//
// # Outputs
//
//   - bool: true if key exists, false otherwise
func (e *EnvVars) Has(key string) bool {
	if e == nil {
		return false
	}
	for _, v := range e.vars {
		if v.Key == key {
			return true
		}
	}
	return false
}

// Len returns the number of environment variables.
//
// # Description
//
// Returns the count of environment variables in the collection.
// Returns 0 if the receiver is nil.
//
// # Outputs
//
//   - int: Number of variables
func (e *EnvVars) Len() int {
	if e == nil {
		return 0
	}
	return len(e.vars)
}

// ToSlice converts to []string format for exec.Cmd.Env.
//
// # Description
//
// Returns environment variables in KEY=VALUE format suitable
// for passing to exec.Cmd.Env. Returns nil if receiver is nil.
//
// # Outputs
//
//   - []string: Variables in KEY=VALUE format
//
// # Example
//
//	cmd.Env = envs.ToSlice()
func (e *EnvVars) ToSlice() []string {
	if e == nil {
		return nil
	}
	result := make([]string, len(e.vars))
	for i, v := range e.vars {
		result[i] = v.String()
	}
	return result
}

// ToMap converts to map[string]string.
//
// # Description
//
// Returns environment variables as a map. If there are duplicate
// keys, the last value wins. Useful for compatibility with code
// that expects map[string]string. Returns nil if receiver is nil.
//
// # Outputs
//
//   - map[string]string: Variables as key-value map
func (e *EnvVars) ToMap() map[string]string {
	if e == nil {
		return nil
	}
	result := make(map[string]string, len(e.vars))
	for _, v := range e.vars {
		result[v.Key] = v.Value
	}
	return result
}

// RedactedSlice returns []string with sensitive values masked.
//
// # Description
//
// Like ToSlice but replaces sensitive values with [REDACTED].
// Safe for logging. Returns nil if receiver is nil.
//
// # Outputs
//
//   - []string: Variables with sensitive values redacted
//
// # Example
//
//	log.Printf("Starting with env: %v", envs.RedactedSlice())
func (e *EnvVars) RedactedSlice() []string {
	if e == nil {
		return nil
	}
	result := make([]string, len(e.vars))
	for i, v := range e.vars {
		result[i] = v.Redacted()
	}
	return result
}

// Merge combines two EnvVars, with other taking precedence.
//
// # Description
//
// Creates a new EnvVars containing all variables from both collections.
// If the same key exists in both, the value from 'other' is used.
// Handles nil receivers and nil other gracefully.
//
// # Inputs
//
//   - other: EnvVars to merge (takes precedence)
//
// # Outputs
//
//   - *EnvVars: New merged collection
//
// # Example
//
//	defaults := MustNewEnvVars(EnvVar{Key: "LOG_LEVEL", Value: "info"})
//	overrides := MustNewEnvVars(EnvVar{Key: "LOG_LEVEL", Value: "debug"})
//	merged := defaults.Merge(overrides)
//	// merged.Get("LOG_LEVEL") == "debug"
//
// # Limitations
//
//   - Order of keys in result is not guaranteed
func (e *EnvVars) Merge(other *EnvVars) *EnvVars {
	if other == nil {
		if e == nil {
			return EmptyEnvVars()
		}
		// Return a copy
		result := &EnvVars{vars: make([]EnvVar, len(e.vars))}
		copy(result.vars, e.vars)
		return result
	}
	if e == nil {
		result := &EnvVars{vars: make([]EnvVar, len(other.vars))}
		copy(result.vars, other.vars)
		return result
	}

	// Use map to handle duplicates (other takes precedence)
	merged := make(map[string]EnvVar)
	for _, v := range e.vars {
		merged[v.Key] = v
	}
	for _, v := range other.vars {
		merged[v.Key] = v
	}

	result := &EnvVars{vars: make([]EnvVar, 0, len(merged))}
	for _, v := range merged {
		result.vars = append(result.vars, v)
	}
	return result
}

// Clone returns a deep copy.
//
// # Description
//
// Creates a new EnvVars with copies of all variables.
// Modifications to the clone do not affect the original.
// Returns nil if receiver is nil.
//
// # Outputs
//
//   - *EnvVars: Deep copy of the collection
func (e *EnvVars) Clone() *EnvVars {
	if e == nil {
		return nil
	}
	result := &EnvVars{vars: make([]EnvVar, len(e.vars))}
	copy(result.vars, e.vars)
	return result
}

// =============================================================================
// Utility Functions
// =============================================================================

// FromMap creates EnvVars from a map[string]string.
//
// # Description
//
// Converts a map[string]string to EnvVars with validation.
// Keys containing common sensitive patterns (TOKEN, SECRET, KEY,
// PASSWORD, CREDENTIAL, API_KEY, AUTH) are automatically marked
// as sensitive. Additional sensitive keys can be specified.
//
// # Inputs
//
//   - m: Map of environment variables (may be nil)
//   - sensitiveKeys: Additional keys that should be marked as sensitive
//
// # Outputs
//
//   - *EnvVars: Validated collection
//   - error: Non-nil if any key is invalid
//
// # Example
//
//	m := map[string]string{"FOO": "bar", "SECRET": "hidden"}
//	envs, err := FromMap(m, []string{"SECRET"})
//
// # Limitations
//
//   - Order of keys in result is not guaranteed (map iteration order)
func FromMap(m map[string]string, sensitiveKeys []string) (*EnvVars, error) {
	if m == nil {
		return EmptyEnvVars(), nil
	}

	sensitiveSet := make(map[string]bool)
	for _, k := range sensitiveKeys {
		sensitiveSet[k] = true
	}

	vars := make([]EnvVar, 0, len(m))
	for k, v := range m {
		vars = append(vars, EnvVar{
			Key:       k,
			Value:     v,
			Sensitive: sensitiveSet[k] || isSensitiveKey(k),
		})
	}

	return NewEnvVars(vars...)
}

// isSensitiveKey detects common sensitive key patterns.
//
// # Description
//
// Checks if a key name contains common patterns that indicate
// sensitive data. Used by FromMap to automatically mark sensitive
// environment variables.
//
// # Inputs
//
//   - key: Environment variable name to check
//
// # Outputs
//
//   - bool: true if key matches sensitive patterns
func isSensitiveKey(key string) bool {
	upper := strings.ToUpper(key)
	return strings.Contains(upper, "TOKEN") ||
		strings.Contains(upper, "SECRET") ||
		strings.Contains(upper, "KEY") ||
		strings.Contains(upper, "PASSWORD") ||
		strings.Contains(upper, "CREDENTIAL") ||
		strings.Contains(upper, "API_KEY") ||
		strings.Contains(upper, "AUTH")
}
