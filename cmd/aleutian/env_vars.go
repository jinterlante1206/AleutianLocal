package main

import (
	"fmt"
	"regexp"
	"strings"
)

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

// EnvVar represents a single environment variable.
//
// # Description
//
// A typed representation of an environment variable with validation
// and sensitivity marking for secure logging.
//
// # Example
//
//	ev := EnvVar{Key: "API_TOKEN", Value: "secret123", Sensitive: true}
//	fmt.Println(ev.Redacted()) // API_TOKEN=[REDACTED]
//
// # Limitations
//
//   - Value is not validated (can be empty or contain any characters)
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

// String returns the KEY=VALUE format.
func (e EnvVar) String() string {
	return fmt.Sprintf("%s=%s", e.Key, e.Value)
}

// Redacted returns KEY=[REDACTED] for sensitive vars, otherwise String().
func (e EnvVar) Redacted() string {
	if e.Sensitive {
		return fmt.Sprintf("%s=[REDACTED]", e.Key)
	}
	return e.String()
}

// Validate checks if the key is valid.
func (e EnvVar) Validate() error {
	if !envVarKeyPattern.MatchString(e.Key) {
		return fmt.Errorf("%w: %q must match pattern [a-zA-Z_][a-zA-Z0-9_]*", ErrInvalidEnvVarKey, e.Key)
	}
	return nil
}

// EnvVars is a validated collection of environment variables.
//
// # Description
//
// Provides a type-safe container for environment variables with
// validation, merging, and redaction capabilities. Replaces raw
// map[string]string for better type safety and security.
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
// # Thread Safety
//
// EnvVars is NOT thread-safe. Do not modify concurrently.
type EnvVars struct {
	vars []EnvVar
}

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
func MustNewEnvVars(vars ...EnvVar) *EnvVars {
	ev, err := NewEnvVars(vars...)
	if err != nil {
		panic(err)
	}
	return ev
}

// EmptyEnvVars returns an empty EnvVars.
func EmptyEnvVars() *EnvVars {
	return &EnvVars{vars: []EnvVar{}}
}

// Add appends a validated environment variable.
//
// # Description
//
// Adds a new environment variable to the collection after validation.
// The variable is appended to the end.
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
func (e *EnvVars) Add(key, value string, sensitive bool) error {
	ev := EnvVar{Key: key, Value: value, Sensitive: sensitive}
	if err := ev.Validate(); err != nil {
		return err
	}
	e.vars = append(e.vars, ev)
	return nil
}

// MustAdd adds a variable or panics.
func (e *EnvVars) MustAdd(key, value string, sensitive bool) {
	if err := e.Add(key, value, sensitive); err != nil {
		panic(err)
	}
}

// Get returns the value for a key, or empty string if not found.
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
// for passing to exec.Cmd.Env.
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
// that expects map[string]string.
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
// Safe for logging.
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
func (e *EnvVars) Clone() *EnvVars {
	if e == nil {
		return nil
	}
	result := &EnvVars{vars: make([]EnvVar, len(e.vars))}
	copy(result.vars, e.vars)
	return result
}

// FromMap creates EnvVars from a map[string]string.
//
// # Description
//
// Converts a map[string]string to EnvVars with validation.
// Useful for migrating from legacy map-based code.
//
// # Inputs
//
//   - m: Map of environment variables
//   - sensitiveKeys: Keys that should be marked as sensitive
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
