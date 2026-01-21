// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package extensions

import "time"

// =============================================================================
// Metadata Type
// =============================================================================

// Metadata stores arbitrary key-value pairs for context and logging.
//
// Using a defined type rather than map[string]any provides:
//   - Clearer intent in function signatures
//   - Ability to add methods for type-safe access
//   - Compile-time distinction from arbitrary maps
//   - Self-documenting code
//
// # Common Keys
//
// While Metadata is flexible, these keys are commonly used:
//   - "session_id": Conversation session identifier
//   - "request_id": Request correlation ID
//   - "user_id": User performing the action
//   - "model": AI model used
//   - "error": Error message if applicable
//   - "ip_address": Client IP address
//   - "user_agent": Client identifier
//   - "duration_ms": Operation duration
//   - "turn_number": Conversation turn count
//
// # Thread Safety
//
// Metadata is NOT thread-safe. Do not share a single Metadata instance
// across goroutines without external synchronization.
//
// Example:
//
//	meta := extensions.NewMetadata().
//	    Set("session_id", sessionID).
//	    Set("model", "claude-3").
//	    Set("duration_ms", 150)
//
//	// Type-safe access
//	if sessionID, ok := meta.GetString("session_id"); ok {
//	    log.Info("session", "id", sessionID)
//	}
type Metadata map[string]any

// NewMetadata creates an empty Metadata instance.
//
// # Description
//
// Returns an initialized Metadata map ready for use.
// This is the preferred way to create Metadata instances.
//
// # Outputs
//
//   - Metadata: An empty, initialized map.
//
// # Examples
//
//	meta := NewMetadata()
//	meta["key"] = "value"
//
// # Thread Safety
//
// The returned Metadata is not thread-safe.
func NewMetadata() Metadata {
	return make(Metadata)
}

// Set adds or updates a key-value pair and returns the Metadata for chaining.
//
// # Description
//
// Provides a fluent interface for building Metadata instances.
// Returns the same Metadata instance for method chaining.
//
// # Inputs
//
//   - key: The metadata key (string).
//   - value: The metadata value (any type).
//
// # Outputs
//
//   - Metadata: The same instance (for chaining).
//
// # Examples
//
//	meta := NewMetadata().
//	    Set("session_id", "sess-123").
//	    Set("user_id", "user-456").
//	    Set("timestamp", time.Now())
//
// # Thread Safety
//
// Not thread-safe. Do not call concurrently.
func (m Metadata) Set(key string, value any) Metadata {
	m[key] = value
	return m
}

// Get retrieves a value by key.
//
// # Description
//
// Returns the value associated with the key and a boolean indicating
// whether the key exists.
//
// # Inputs
//
//   - key: The metadata key to retrieve.
//
// # Outputs
//
//   - any: The value, or nil if not found.
//   - bool: True if the key exists, false otherwise.
//
// # Examples
//
//	if value, ok := meta.Get("session_id"); ok {
//	    // value exists
//	}
//
// # Thread Safety
//
// Not thread-safe if modified concurrently.
func (m Metadata) Get(key string) (any, bool) {
	value, ok := m[key]
	return value, ok
}

// GetString retrieves a string value by key.
//
// # Description
//
// Type-safe accessor for string values. Returns the string and true
// if the key exists and the value is a string, otherwise returns
// empty string and false.
//
// # Inputs
//
//   - key: The metadata key to retrieve.
//
// # Outputs
//
//   - string: The value, or "" if not found or not a string.
//   - bool: True if the key exists and value is a string.
//
// # Examples
//
//	if sessionID, ok := meta.GetString("session_id"); ok {
//	    log.Info("session", "id", sessionID)
//	}
//
// # Thread Safety
//
// Not thread-safe if modified concurrently.
func (m Metadata) GetString(key string) (string, bool) {
	value, ok := m[key]
	if !ok {
		return "", false
	}
	str, ok := value.(string)
	return str, ok
}

// GetInt retrieves an int value by key.
//
// # Description
//
// Type-safe accessor for int values. Returns the int and true
// if the key exists and the value is an int, otherwise returns
// 0 and false.
//
// # Inputs
//
//   - key: The metadata key to retrieve.
//
// # Outputs
//
//   - int: The value, or 0 if not found or not an int.
//   - bool: True if the key exists and value is an int.
//
// # Examples
//
//	if turnNum, ok := meta.GetInt("turn_number"); ok {
//	    fmt.Printf("Turn %d\n", turnNum)
//	}
//
// # Thread Safety
//
// Not thread-safe if modified concurrently.
func (m Metadata) GetInt(key string) (int, bool) {
	value, ok := m[key]
	if !ok {
		return 0, false
	}
	i, ok := value.(int)
	return i, ok
}

// GetInt64 retrieves an int64 value by key.
//
// # Description
//
// Type-safe accessor for int64 values. Returns the int64 and true
// if the key exists and the value is an int64, otherwise returns
// 0 and false.
//
// # Inputs
//
//   - key: The metadata key to retrieve.
//
// # Outputs
//
//   - int64: The value, or 0 if not found or not an int64.
//   - bool: True if the key exists and value is an int64.
//
// # Examples
//
//	if durationMs, ok := meta.GetInt64("duration_ms"); ok {
//	    fmt.Printf("Duration: %dms\n", durationMs)
//	}
//
// # Thread Safety
//
// Not thread-safe if modified concurrently.
func (m Metadata) GetInt64(key string) (int64, bool) {
	value, ok := m[key]
	if !ok {
		return 0, false
	}
	i, ok := value.(int64)
	return i, ok
}

// GetFloat64 retrieves a float64 value by key.
//
// # Description
//
// Type-safe accessor for float64 values. Returns the float64 and true
// if the key exists and the value is a float64, otherwise returns
// 0 and false.
//
// # Inputs
//
//   - key: The metadata key to retrieve.
//
// # Outputs
//
//   - float64: The value, or 0 if not found or not a float64.
//   - bool: True if the key exists and value is a float64.
//
// # Examples
//
//	if score, ok := meta.GetFloat64("confidence_score"); ok {
//	    fmt.Printf("Confidence: %.2f\n", score)
//	}
//
// # Thread Safety
//
// Not thread-safe if modified concurrently.
func (m Metadata) GetFloat64(key string) (float64, bool) {
	value, ok := m[key]
	if !ok {
		return 0, false
	}
	f, ok := value.(float64)
	return f, ok
}

// GetBool retrieves a bool value by key.
//
// # Description
//
// Type-safe accessor for bool values. Returns the bool and true
// if the key exists and the value is a bool, otherwise returns
// false and false.
//
// # Inputs
//
//   - key: The metadata key to retrieve.
//
// # Outputs
//
//   - bool: The value, or false if not found or not a bool.
//   - bool: True if the key exists and value is a bool.
//
// # Examples
//
//	if isAdmin, ok := meta.GetBool("is_admin"); ok && isAdmin {
//	    // User is admin
//	}
//
// # Thread Safety
//
// Not thread-safe if modified concurrently.
func (m Metadata) GetBool(key string) (bool, bool) {
	value, ok := m[key]
	if !ok {
		return false, false
	}
	b, ok := value.(bool)
	return b, ok
}

// GetTime retrieves a time.Time value by key.
//
// # Description
//
// Type-safe accessor for time.Time values. Returns the time and true
// if the key exists and the value is a time.Time, otherwise returns
// zero time and false.
//
// # Inputs
//
//   - key: The metadata key to retrieve.
//
// # Outputs
//
//   - time.Time: The value, or zero time if not found or not a time.Time.
//   - bool: True if the key exists and value is a time.Time.
//
// # Examples
//
//	if timestamp, ok := meta.GetTime("created_at"); ok {
//	    fmt.Printf("Created: %s\n", timestamp.Format(time.RFC3339))
//	}
//
// # Thread Safety
//
// Not thread-safe if modified concurrently.
func (m Metadata) GetTime(key string) (time.Time, bool) {
	value, ok := m[key]
	if !ok {
		return time.Time{}, false
	}
	t, ok := value.(time.Time)
	return t, ok
}

// Has checks if a key exists in the Metadata.
//
// # Description
//
// Returns true if the key exists, regardless of its value (including nil).
//
// # Inputs
//
//   - key: The metadata key to check.
//
// # Outputs
//
//   - bool: True if the key exists.
//
// # Examples
//
//	if meta.Has("error") {
//	    // Handle error case
//	}
//
// # Thread Safety
//
// Not thread-safe if modified concurrently.
func (m Metadata) Has(key string) bool {
	_, ok := m[key]
	return ok
}

// Delete removes a key from the Metadata.
//
// # Description
//
// Removes the specified key and its value. Safe to call even if
// the key doesn't exist.
//
// # Inputs
//
//   - key: The metadata key to delete.
//
// # Outputs
//
//   - Metadata: The same instance (for chaining).
//
// # Examples
//
//	meta.Delete("sensitive_data")
//
// # Thread Safety
//
// Not thread-safe. Do not call concurrently.
func (m Metadata) Delete(key string) Metadata {
	delete(m, key)
	return m
}

// Clone creates a shallow copy of the Metadata.
//
// # Description
//
// Creates a new Metadata instance with the same key-value pairs.
// Note: Values themselves are not deep-copied.
//
// # Outputs
//
//   - Metadata: A new instance with copied key-value pairs.
//
// # Examples
//
//	original := NewMetadata().Set("key", "value")
//	copy := original.Clone()
//	copy.Set("key", "modified")  // original unchanged
//
// # Limitations
//
// This is a shallow copy. If values are pointers or references,
// they will point to the same underlying data.
//
// # Thread Safety
//
// The returned copy is independent but not thread-safe.
func (m Metadata) Clone() Metadata {
	clone := make(Metadata, len(m))
	for k, v := range m {
		clone[k] = v
	}
	return clone
}

// Merge copies all key-value pairs from another Metadata into this one.
//
// # Description
//
// Adds all entries from the other Metadata. Existing keys are overwritten.
//
// # Inputs
//
//   - other: The Metadata to merge from. Can be nil (no-op).
//
// # Outputs
//
//   - Metadata: The same instance (for chaining).
//
// # Examples
//
//	base := NewMetadata().Set("env", "prod")
//	extra := NewMetadata().Set("version", "1.0")
//	base.Merge(extra)
//	// base now has both "env" and "version"
//
// # Thread Safety
//
// Not thread-safe. Do not call concurrently.
func (m Metadata) Merge(other Metadata) Metadata {
	if other == nil {
		return m
	}
	for k, v := range other {
		m[k] = v
	}
	return m
}

// Keys returns all keys in the Metadata.
//
// # Description
//
// Returns a slice of all keys. Order is not guaranteed.
//
// # Outputs
//
//   - []string: All keys in the Metadata.
//
// # Examples
//
//	keys := meta.Keys()
//	for _, key := range keys {
//	    fmt.Printf("Key: %s\n", key)
//	}
//
// # Thread Safety
//
// Not thread-safe if modified concurrently.
func (m Metadata) Keys() []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// Len returns the number of key-value pairs.
//
// # Description
//
// Returns the count of entries in the Metadata.
//
// # Outputs
//
//   - int: Number of key-value pairs.
//
// # Examples
//
//	if meta.Len() == 0 {
//	    fmt.Println("No metadata")
//	}
//
// # Thread Safety
//
// Not thread-safe if modified concurrently.
func (m Metadata) Len() int {
	return len(m)
}
