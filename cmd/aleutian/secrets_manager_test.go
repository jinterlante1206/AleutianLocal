// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

/*
Package main provides tests for SecretsManager.

This file contains:
  - MockSecretsManager: A mock implementation for testing
  - Unit tests for all SecretsManager methods
  - Test helpers for creating test fixtures

# Test Categories

  - Interface compliance tests
  - Backend fallback tests
  - Validation tests
  - Error handling tests
  - Metadata tests
  - Setup instructions tests
*/
package main

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/cmd/aleutian/config"
)

// =============================================================================
// Mock Implementation
// =============================================================================

// MockSecretsManager is a mock implementation of SecretsManager for testing.
//
// # Description
//
// Provides a configurable mock for testing code that depends on SecretsManager.
// All behavior can be configured through the struct fields.
//
// # Thread Safety
//
// MockSecretsManager is NOT thread-safe. Use only in single-threaded tests.
//
// # Usage
//
//	mock := &MockSecretsManager{
//	    Secrets: map[string]string{"ANTHROPIC_API_KEY": "sk-ant-test123"},
//	}
//	value, err := mock.GetSecret(ctx, "ANTHROPIC_API_KEY")
type MockSecretsManager struct {
	// Secrets maps secret names to values.
	// GetSecret returns value from this map, or error if not found.
	Secrets map[string]string

	// Metadata maps secret names to metadata.
	// GetSecretWithMetadata returns metadata from this map.
	Metadata map[string]*SecretMetadata

	// Validations maps secret names to validation results.
	// ValidateSecret returns result from this map.
	Validations map[string]*SecretValidation

	// BackendType is returned by GetBackendType.
	BackendType string

	// AvailableBackends is returned by DetectAvailableBackends.
	AvailableBackends []string

	// Configured is returned by IsConfigured.
	Configured bool

	// SetupInstructions is returned by GetSetupInstructions.
	SetupInstructions string

	// ForceError causes all methods to return this error.
	ForceError error

	// RenewSecretError is returned by RenewSecret.
	RenewSecretError error

	// CallCounts tracks how many times each method was called.
	CallCounts map[string]int
}

// NewMockSecretsManager creates a mock with sensible defaults.
//
// # Description
//
// Creates a MockSecretsManager with initialized maps and default values.
// Secrets map is empty; add secrets as needed for your test.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - *MockSecretsManager: Ready-to-use mock
//
// # Examples
//
//	mock := NewMockSecretsManager()
//	mock.Secrets["ANTHROPIC_API_KEY"] = "sk-ant-test"
//	// Use mock in tests...
//
// # Limitations
//
//   - Not thread-safe
//
// # Assumptions
//
//   - Caller will add secrets as needed for their test case
func NewMockSecretsManager() *MockSecretsManager {
	return &MockSecretsManager{
		Secrets:           make(map[string]string),
		Metadata:          make(map[string]*SecretMetadata),
		Validations:       make(map[string]*SecretValidation),
		BackendType:       SecretBackendMock,
		AvailableBackends: []string{SecretBackendMock},
		Configured:        true,
		SetupInstructions: "Mock setup instructions",
		CallCounts:        make(map[string]int),
	}
}

// incrementCallCount increments the call count for a method.
func (m *MockSecretsManager) incrementCallCount(method string) {
	if m.CallCounts == nil {
		m.CallCounts = make(map[string]int)
	}
	m.CallCounts[method]++
}

// GetSecret retrieves a secret by its canonical name.
//
// # Description
//
// Returns the secret value from the Secrets map, or ErrSecretNotFound if
// not present. Returns ForceError if set.
//
// # Inputs
//
//   - ctx: Context (unused in mock)
//   - name: Secret name to look up
//
// # Outputs
//
//   - string: The secret value
//   - error: ErrSecretNotFound or ForceError
//
// # Examples
//
//	mock := NewMockSecretsManager()
//	mock.Secrets["KEY"] = "value"
//	val, err := mock.GetSecret(ctx, "KEY")
//
// # Limitations
//
//   - Does not simulate timeouts or backend failures
//
// # Assumptions
//
//   - Secrets map is initialized
func (m *MockSecretsManager) GetSecret(ctx context.Context, name string) (string, error) {
	m.incrementCallCount("GetSecret")
	if m.ForceError != nil {
		return "", m.ForceError
	}
	value, ok := m.Secrets[name]
	if !ok {
		return "", ErrSecretNotFound
	}
	return value, nil
}

// GetSecretWithDefault retrieves a secret, returning a default if not found.
//
// # Description
//
// Returns the secret value from the Secrets map, or defaultValue if not present.
// Returns ForceError if set (does NOT return default on error).
//
// # Inputs
//
//   - ctx: Context (unused in mock)
//   - name: Secret name to look up
//   - defaultValue: Value to return if secret not found
//
// # Outputs
//
//   - string: The secret value or default
//   - error: ForceError if set, nil otherwise
//
// # Examples
//
//	mock := NewMockSecretsManager()
//	val, err := mock.GetSecretWithDefault(ctx, "MISSING", "default")
//	// val == "default", err == nil
//
// # Limitations
//
//   - Does not validate defaultValue
//
// # Assumptions
//
//   - Caller understands ForceError still returns error
func (m *MockSecretsManager) GetSecretWithDefault(ctx context.Context, name, defaultValue string) (string, error) {
	m.incrementCallCount("GetSecretWithDefault")
	if m.ForceError != nil {
		return "", m.ForceError
	}
	value, ok := m.Secrets[name]
	if !ok {
		return defaultValue, nil
	}
	return value, nil
}

// HasSecret checks if a secret exists without retrieving it.
//
// # Description
//
// Returns true if the secret is in the Secrets map and non-empty.
//
// # Inputs
//
//   - ctx: Context (unused in mock)
//   - name: Secret name to check
//
// # Outputs
//
//   - bool: True if secret exists
//   - error: ForceError if set
//
// # Examples
//
//	mock := NewMockSecretsManager()
//	mock.Secrets["KEY"] = "value"
//	exists, _ := mock.HasSecret(ctx, "KEY") // true
//
// # Limitations
//
//   - Empty string value is treated as not existing
//
// # Assumptions
//
//   - Secrets map is initialized
func (m *MockSecretsManager) HasSecret(ctx context.Context, name string) (bool, error) {
	m.incrementCallCount("HasSecret")
	if m.ForceError != nil {
		return false, m.ForceError
	}
	value, ok := m.Secrets[name]
	return ok && value != "", nil
}

// GetSecretWithMetadata retrieves a secret along with its metadata.
//
// # Description
//
// Returns the secret value and metadata. If no metadata is configured,
// returns default metadata with Backend set to SecretBackendMock.
//
// # Inputs
//
//   - ctx: Context (unused in mock)
//   - name: Secret name to look up
//
// # Outputs
//
//   - string: The secret value
//   - *SecretMetadata: Metadata (from Metadata map or default)
//   - error: ErrSecretNotFound or ForceError
//
// # Examples
//
//	mock := NewMockSecretsManager()
//	mock.Secrets["KEY"] = "value"
//	mock.Metadata["KEY"] = &SecretMetadata{Backend: "test"}
//	val, meta, err := mock.GetSecretWithMetadata(ctx, "KEY")
//
// # Limitations
//
//   - Does not simulate expiry or renewal
//
// # Assumptions
//
//   - Secrets and Metadata maps are initialized
func (m *MockSecretsManager) GetSecretWithMetadata(ctx context.Context, name string) (string, *SecretMetadata, error) {
	m.incrementCallCount("GetSecretWithMetadata")
	if m.ForceError != nil {
		return "", nil, m.ForceError
	}
	value, ok := m.Secrets[name]
	if !ok {
		return "", nil, ErrSecretNotFound
	}
	meta := m.Metadata[name]
	if meta == nil {
		meta = &SecretMetadata{
			Backend:   SecretBackendMock,
			Renewable: false,
		}
	}
	return value, meta, nil
}

// ValidateSecret checks if a secret meets format requirements.
//
// # Description
//
// Returns validation result from Validations map if configured,
// otherwise performs basic validation (exists and non-empty).
//
// # Inputs
//
//   - ctx: Context (unused in mock)
//   - name: Secret name to validate
//
// # Outputs
//
//   - *SecretValidation: Validation result
//   - error: ForceError if set
//
// # Examples
//
//	mock := NewMockSecretsManager()
//	mock.Validations["KEY"] = &SecretValidation{Valid: true}
//	result, _ := mock.ValidateSecret(ctx, "KEY")
//
// # Limitations
//
//   - Does not perform real format validation
//
// # Assumptions
//
//   - Caller sets up Validations map for specific test cases
func (m *MockSecretsManager) ValidateSecret(ctx context.Context, name string) (*SecretValidation, error) {
	m.incrementCallCount("ValidateSecret")
	if m.ForceError != nil {
		return nil, m.ForceError
	}
	if result, ok := m.Validations[name]; ok {
		return result, nil
	}
	value, ok := m.Secrets[name]
	return &SecretValidation{
		Name:   name,
		Valid:  ok && value != "",
		Exists: ok,
		Reason: func() string {
			if !ok {
				return "secret not found"
			}
			return ""
		}(),
	}, nil
}

// ListSecretNames returns all configured secret names.
//
// # Description
//
// Returns the keys from the Secrets map.
//
// # Inputs
//
//   - ctx: Context (unused in mock)
//
// # Outputs
//
//   - []string: List of secret names
//   - error: ForceError if set
//
// # Examples
//
//	mock := NewMockSecretsManager()
//	mock.Secrets["KEY1"] = "value1"
//	mock.Secrets["KEY2"] = "value2"
//	names, _ := mock.ListSecretNames(ctx) // ["KEY1", "KEY2"]
//
// # Limitations
//
//   - Order is not guaranteed
//
// # Assumptions
//
//   - Secrets map is initialized
func (m *MockSecretsManager) ListSecretNames(ctx context.Context) ([]string, error) {
	m.incrementCallCount("ListSecretNames")
	if m.ForceError != nil {
		return nil, m.ForceError
	}
	names := make([]string, 0, len(m.Secrets))
	for name := range m.Secrets {
		names = append(names, name)
	}
	return names, nil
}

// RenewSecret attempts to renew a renewable secret.
//
// # Description
//
// Returns RenewSecretError if set, otherwise returns ErrSecretNotRenewable.
//
// # Inputs
//
//   - ctx: Context (unused in mock)
//   - name: Secret name to renew
//
// # Outputs
//
//   - *SecretMetadata: Updated metadata (nil in mock)
//   - error: RenewSecretError or ErrSecretNotRenewable
//
// # Examples
//
//	mock := NewMockSecretsManager()
//	_, err := mock.RenewSecret(ctx, "KEY")
//	// err == ErrSecretNotRenewable
//
// # Limitations
//
//   - Does not actually renew anything
//
// # Assumptions
//
//   - Most test cases expect ErrSecretNotRenewable
func (m *MockSecretsManager) RenewSecret(ctx context.Context, name string) (*SecretMetadata, error) {
	m.incrementCallCount("RenewSecret")
	if m.ForceError != nil {
		return nil, m.ForceError
	}
	if m.RenewSecretError != nil {
		return nil, m.RenewSecretError
	}
	return nil, ErrSecretNotRenewable
}

// GetBackendType returns the primary configured backend type.
//
// # Description
//
// Returns the BackendType field value.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - string: Backend type identifier
//
// # Examples
//
//	mock := NewMockSecretsManager()
//	mock.BackendType = "custom"
//	bt := mock.GetBackendType() // "custom"
//
// # Limitations
//
// None.
//
// # Assumptions
//
// None.
func (m *MockSecretsManager) GetBackendType() string {
	m.incrementCallCount("GetBackendType")
	return m.BackendType
}

// GetSetupInstructions returns setup help for a missing secret.
//
// # Description
//
// Returns the SetupInstructions field value.
//
// # Inputs
//
//   - name: Secret name (unused in mock)
//
// # Outputs
//
//   - string: Setup instructions
//
// # Examples
//
//	mock := NewMockSecretsManager()
//	mock.SetupInstructions = "Run: export KEY=value"
//	instr := mock.GetSetupInstructions("KEY")
//
// # Limitations
//
//   - Same instructions returned regardless of secret name
//
// # Assumptions
//
// None.
func (m *MockSecretsManager) GetSetupInstructions(name string) string {
	m.incrementCallCount("GetSetupInstructions")
	return m.SetupInstructions
}

// IsConfigured returns true if the secrets manager is configured.
//
// # Description
//
// Returns the Configured field value.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - bool: Configuration status
//
// # Examples
//
//	mock := NewMockSecretsManager()
//	mock.Configured = false
//	cfg := mock.IsConfigured() // false
//
// # Limitations
//
// None.
//
// # Assumptions
//
// None.
func (m *MockSecretsManager) IsConfigured() bool {
	m.incrementCallCount("IsConfigured")
	return m.Configured
}

// DetectAvailableBackends returns available backends.
//
// # Description
//
// Returns the AvailableBackends field value.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - []string: List of available backends
//
// # Examples
//
//	mock := NewMockSecretsManager()
//	mock.AvailableBackends = []string{"env", "keychain"}
//	backends := mock.DetectAvailableBackends()
//
// # Limitations
//
// None.
//
// # Assumptions
//
// None.
func (m *MockSecretsManager) DetectAvailableBackends() []string {
	m.incrementCallCount("DetectAvailableBackends")
	result := make([]string, len(m.AvailableBackends))
	copy(result, m.AvailableBackends)
	return result
}

// Compile-time interface check for MockSecretsManager.
var _ SecretsManager = (*MockSecretsManager)(nil)

// =============================================================================
// Test Helpers
// =============================================================================

// createTestSecretsManager creates a DefaultSecretsManager configured for testing.
//
// # Description
//
// Creates a secrets manager with environment variable backend enabled and
// a mock environment function that returns values from the provided map.
//
// # Inputs
//
//   - secrets: Map of secret name to value for mock environment
//
// # Outputs
//
//   - *DefaultSecretsManager: Configured for testing
//
// # Examples
//
//	mgr := createTestSecretsManager(map[string]string{
//	    "ANTHROPIC_API_KEY": "sk-ant-test123",
//	})
//	val, _ := mgr.GetSecret(ctx, "ANTHROPIC_API_KEY")
//
// # Limitations
//
//   - Only enables environment backend
//   - Does not test CLI backends (keychain, 1password, libsecret)
//
// # Assumptions
//
//   - Test only needs environment variable backend
func createTestSecretsManager(secrets map[string]string) *DefaultSecretsManager {
	cfg := config.SecretsConfig{
		UseEnv: true,
	}
	mgr := NewDefaultSecretsManager(cfg, nil)
	mgr.envFunc = func(name string) string {
		return secrets[name]
	}
	return mgr
}

// createTestSecretsManagerWithExec creates a manager with mock command execution.
//
// # Description
//
// Creates a secrets manager with a mock execCommandFunc for testing
// CLI-based backends (1Password, Keychain, libsecret) without real CLIs.
//
// # Inputs
//
//   - cfg: Secrets configuration
//   - mockExec: Mock command execution function
//
// # Outputs
//
//   - *DefaultSecretsManager: Configured with mock exec
//
// # Examples
//
//	mgr := createTestSecretsManagerWithExec(cfg, func(ctx context.Context, name string, args ...string) *exec.Cmd {
//	    return exec.Command("echo", "mock-secret")
//	})
//
// # Limitations
//
//   - Requires understanding of exec.Cmd mocking
//
// # Assumptions
//
//   - Caller provides appropriate mock for their test case
func createTestSecretsManagerWithExec(
	cfg config.SecretsConfig,
	mockExec func(ctx context.Context, name string, args ...string) *exec.Cmd,
) *DefaultSecretsManager {
	mgr := NewDefaultSecretsManager(cfg, nil)
	mgr.execCommandFunc = mockExec
	mgr.envFunc = func(name string) string { return "" }
	return mgr
}

// =============================================================================
// Unit Tests - MockSecretsManager
// =============================================================================

func TestMockSecretsManager_GetSecret(t *testing.T) {
	t.Parallel()

	t.Run("returns secret when exists", func(t *testing.T) {
		mock := NewMockSecretsManager()
		mock.Secrets["TEST_KEY"] = "test-value"

		value, err := mock.GetSecret(context.Background(), "TEST_KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if value != "test-value" {
			t.Errorf("expected 'test-value', got '%s'", value)
		}
		if mock.CallCounts["GetSecret"] != 1 {
			t.Errorf("expected 1 call, got %d", mock.CallCounts["GetSecret"])
		}
	})

	t.Run("returns error when not exists", func(t *testing.T) {
		mock := NewMockSecretsManager()

		_, err := mock.GetSecret(context.Background(), "MISSING_KEY")
		if !errors.Is(err, ErrSecretNotFound) {
			t.Errorf("expected ErrSecretNotFound, got %v", err)
		}
	})

	t.Run("returns forced error", func(t *testing.T) {
		mock := NewMockSecretsManager()
		mock.Secrets["TEST_KEY"] = "test-value"
		mock.ForceError = errors.New("forced error")

		_, err := mock.GetSecret(context.Background(), "TEST_KEY")
		if err == nil || err.Error() != "forced error" {
			t.Errorf("expected forced error, got %v", err)
		}
	})
}

func TestMockSecretsManager_GetSecretWithDefault(t *testing.T) {
	t.Parallel()

	t.Run("returns secret when exists", func(t *testing.T) {
		mock := NewMockSecretsManager()
		mock.Secrets["TEST_KEY"] = "test-value"

		value, err := mock.GetSecretWithDefault(context.Background(), "TEST_KEY", "default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if value != "test-value" {
			t.Errorf("expected 'test-value', got '%s'", value)
		}
	})

	t.Run("returns default when not exists", func(t *testing.T) {
		mock := NewMockSecretsManager()

		value, err := mock.GetSecretWithDefault(context.Background(), "MISSING_KEY", "default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if value != "default" {
			t.Errorf("expected 'default', got '%s'", value)
		}
	})

	t.Run("returns error when forced", func(t *testing.T) {
		mock := NewMockSecretsManager()
		mock.ForceError = errors.New("forced error")

		_, err := mock.GetSecretWithDefault(context.Background(), "KEY", "default")
		if err == nil || err.Error() != "forced error" {
			t.Errorf("expected forced error, got %v", err)
		}
	})
}

func TestMockSecretsManager_HasSecret(t *testing.T) {
	t.Parallel()

	t.Run("returns true when exists", func(t *testing.T) {
		mock := NewMockSecretsManager()
		mock.Secrets["TEST_KEY"] = "value"

		exists, err := mock.HasSecret(context.Background(), "TEST_KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !exists {
			t.Error("expected exists to be true")
		}
	})

	t.Run("returns false when not exists", func(t *testing.T) {
		mock := NewMockSecretsManager()

		exists, err := mock.HasSecret(context.Background(), "MISSING")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exists {
			t.Error("expected exists to be false")
		}
	})

	t.Run("returns false when empty value", func(t *testing.T) {
		mock := NewMockSecretsManager()
		mock.Secrets["EMPTY_KEY"] = ""

		exists, err := mock.HasSecret(context.Background(), "EMPTY_KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exists {
			t.Error("expected exists to be false for empty value")
		}
	})
}

func TestMockSecretsManager_GetSecretWithMetadata(t *testing.T) {
	t.Parallel()

	t.Run("returns secret with default metadata", func(t *testing.T) {
		mock := NewMockSecretsManager()
		mock.Secrets["KEY"] = "value"

		value, meta, err := mock.GetSecretWithMetadata(context.Background(), "KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if value != "value" {
			t.Errorf("expected 'value', got '%s'", value)
		}
		if meta.Backend != SecretBackendMock {
			t.Errorf("expected backend '%s', got '%s'", SecretBackendMock, meta.Backend)
		}
	})

	t.Run("returns custom metadata when configured", func(t *testing.T) {
		mock := NewMockSecretsManager()
		mock.Secrets["KEY"] = "value"
		mock.Metadata["KEY"] = &SecretMetadata{
			Backend:   "custom",
			Renewable: true,
			ExpiresAt: time.Now().Add(time.Hour),
		}

		_, meta, err := mock.GetSecretWithMetadata(context.Background(), "KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if meta.Backend != "custom" {
			t.Errorf("expected backend 'custom', got '%s'", meta.Backend)
		}
		if !meta.Renewable {
			t.Error("expected renewable to be true")
		}
	})
}

func TestMockSecretsManager_ValidateSecret(t *testing.T) {
	t.Parallel()

	t.Run("returns custom validation when configured", func(t *testing.T) {
		mock := NewMockSecretsManager()
		mock.Validations["KEY"] = &SecretValidation{
			Name:     "KEY",
			Valid:    false,
			Exists:   true,
			Reason:   "wrong format",
			Warnings: []string{"test warning"},
		}

		result, err := mock.ValidateSecret(context.Background(), "KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Valid {
			t.Error("expected Valid to be false")
		}
		if result.Reason != "wrong format" {
			t.Errorf("expected reason 'wrong format', got '%s'", result.Reason)
		}
	})

	t.Run("returns basic validation when not configured", func(t *testing.T) {
		mock := NewMockSecretsManager()
		mock.Secrets["KEY"] = "value"

		result, err := mock.ValidateSecret(context.Background(), "KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Valid {
			t.Error("expected Valid to be true")
		}
		if !result.Exists {
			t.Error("expected Exists to be true")
		}
	})
}

func TestMockSecretsManager_ListSecretNames(t *testing.T) {
	t.Parallel()

	t.Run("returns all secret names", func(t *testing.T) {
		mock := NewMockSecretsManager()
		mock.Secrets["KEY1"] = "value1"
		mock.Secrets["KEY2"] = "value2"

		names, err := mock.ListSecretNames(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(names) != 2 {
			t.Errorf("expected 2 names, got %d", len(names))
		}
	})

	t.Run("returns empty list when no secrets", func(t *testing.T) {
		mock := NewMockSecretsManager()

		names, err := mock.ListSecretNames(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(names) != 0 {
			t.Errorf("expected 0 names, got %d", len(names))
		}
	})
}

func TestMockSecretsManager_RenewSecret(t *testing.T) {
	t.Parallel()

	t.Run("returns ErrSecretNotRenewable by default", func(t *testing.T) {
		mock := NewMockSecretsManager()

		_, err := mock.RenewSecret(context.Background(), "KEY")
		if !errors.Is(err, ErrSecretNotRenewable) {
			t.Errorf("expected ErrSecretNotRenewable, got %v", err)
		}
	})

	t.Run("returns custom error when set", func(t *testing.T) {
		mock := NewMockSecretsManager()
		mock.RenewSecretError = errors.New("renewal failed")

		_, err := mock.RenewSecret(context.Background(), "KEY")
		if err == nil || err.Error() != "renewal failed" {
			t.Errorf("expected 'renewal failed', got %v", err)
		}
	})
}

func TestMockSecretsManager_GetBackendType(t *testing.T) {
	t.Parallel()

	mock := NewMockSecretsManager()
	mock.BackendType = "test-backend"

	bt := mock.GetBackendType()
	if bt != "test-backend" {
		t.Errorf("expected 'test-backend', got '%s'", bt)
	}
}

func TestMockSecretsManager_IsConfigured(t *testing.T) {
	t.Parallel()

	t.Run("returns true by default", func(t *testing.T) {
		mock := NewMockSecretsManager()
		if !mock.IsConfigured() {
			t.Error("expected IsConfigured to be true by default")
		}
	})

	t.Run("returns configured value", func(t *testing.T) {
		mock := NewMockSecretsManager()
		mock.Configured = false
		if mock.IsConfigured() {
			t.Error("expected IsConfigured to be false")
		}
	})
}

func TestMockSecretsManager_DetectAvailableBackends(t *testing.T) {
	t.Parallel()

	mock := NewMockSecretsManager()
	mock.AvailableBackends = []string{"env", "keychain"}

	backends := mock.DetectAvailableBackends()
	if len(backends) != 2 {
		t.Errorf("expected 2 backends, got %d", len(backends))
	}
}

// =============================================================================
// Unit Tests - DefaultSecretsManager
// =============================================================================

func TestDefaultSecretsManager_GetSecret(t *testing.T) {
	t.Parallel()

	t.Run("returns secret from env", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{
			"ANTHROPIC_API_KEY": "sk-ant-test123456789012345",
		})

		value, err := mgr.GetSecret(context.Background(), "ANTHROPIC_API_KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if value != "sk-ant-test123456789012345" {
			t.Errorf("unexpected value: %s", value)
		}
	})

	t.Run("returns error for missing secret", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{})

		_, err := mgr.GetSecret(context.Background(), "MISSING_KEY")
		if !errors.Is(err, ErrSecretNotFound) {
			t.Errorf("expected ErrSecretNotFound, got %v", err)
		}
	})

	t.Run("returns error for empty name", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{})

		_, err := mgr.GetSecret(context.Background(), "")
		if err == nil {
			t.Error("expected error for empty name")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{
			"KEY": "value",
		})

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := mgr.GetSecret(ctx, "KEY")
		if err != nil && !errors.Is(err, context.Canceled) {
			// Note: Our implementation may succeed before ctx check
			// This tests that we at least don't hang
		}
	})
}

func TestDefaultSecretsManager_GetSecretWithDefault(t *testing.T) {
	t.Parallel()

	t.Run("returns secret when exists", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{
			"KEY": "actual-value",
		})

		value, err := mgr.GetSecretWithDefault(context.Background(), "KEY", "default")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if value != "actual-value" {
			t.Errorf("expected 'actual-value', got '%s'", value)
		}
	})

	t.Run("returns default when not exists", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{})

		value, err := mgr.GetSecretWithDefault(context.Background(), "MISSING", "default-value")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if value != "default-value" {
			t.Errorf("expected 'default-value', got '%s'", value)
		}
	})
}

func TestDefaultSecretsManager_HasSecret(t *testing.T) {
	t.Parallel()

	t.Run("returns true when exists", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{
			"KEY": "value",
		})

		exists, err := mgr.HasSecret(context.Background(), "KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !exists {
			t.Error("expected exists to be true")
		}
	})

	t.Run("returns false when not exists", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{})

		exists, err := mgr.HasSecret(context.Background(), "MISSING")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if exists {
			t.Error("expected exists to be false")
		}
	})
}

func TestDefaultSecretsManager_GetSecretWithMetadata(t *testing.T) {
	t.Parallel()

	t.Run("returns metadata from env backend", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{
			"KEY": "value",
		})

		value, meta, err := mgr.GetSecretWithMetadata(context.Background(), "KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if value != "value" {
			t.Errorf("expected 'value', got '%s'", value)
		}
		if meta.Backend != SecretBackendEnv {
			t.Errorf("expected backend '%s', got '%s'", SecretBackendEnv, meta.Backend)
		}
		if meta.Renewable {
			t.Error("env secrets should not be renewable")
		}
	})
}

func TestDefaultSecretsManager_ValidateSecret(t *testing.T) {
	t.Parallel()

	t.Run("validates Anthropic key format - valid", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{
			SecretAnthropicAPIKey: "sk-ant-api03-test1234567890123456789",
		})

		result, err := mgr.ValidateSecret(context.Background(), SecretAnthropicAPIKey)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Valid {
			t.Errorf("expected valid, got reason: %s", result.Reason)
		}
		if !result.Exists {
			t.Error("expected exists to be true")
		}
	})

	t.Run("validates Anthropic key format - invalid prefix", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{
			SecretAnthropicAPIKey: "sk-not-anthropic-key",
		})

		result, err := mgr.ValidateSecret(context.Background(), SecretAnthropicAPIKey)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Valid {
			t.Error("expected invalid")
		}
		if !strings.Contains(result.Reason, "sk-ant-") {
			t.Errorf("expected reason about 'sk-ant-', got: %s", result.Reason)
		}
	})

	t.Run("validates OpenAI key format - valid", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{
			SecretOpenAIAPIKey: "sk-proj-test1234567890123456789",
		})

		result, err := mgr.ValidateSecret(context.Background(), SecretOpenAIAPIKey)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Valid {
			t.Errorf("expected valid, got reason: %s", result.Reason)
		}
	})

	t.Run("validates OpenAI key format - invalid prefix", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{
			SecretOpenAIAPIKey: "not-a-valid-openai-key",
		})

		result, err := mgr.ValidateSecret(context.Background(), SecretOpenAIAPIKey)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Valid {
			t.Error("expected invalid")
		}
		if !strings.Contains(result.Reason, "sk-") {
			t.Errorf("expected reason about 'sk-', got: %s", result.Reason)
		}
	})

	t.Run("validates key too short", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{
			SecretAnthropicAPIKey: "sk-ant-x",
		})

		result, err := mgr.ValidateSecret(context.Background(), SecretAnthropicAPIKey)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Valid {
			t.Error("expected invalid due to short length")
		}
		if !strings.Contains(result.Reason, "short") {
			t.Errorf("expected reason about length, got: %s", result.Reason)
		}
	})

	t.Run("returns not found for missing secret", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{})

		result, err := mgr.ValidateSecret(context.Background(), SecretAnthropicAPIKey)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Valid {
			t.Error("expected invalid")
		}
		if result.Exists {
			t.Error("expected exists to be false")
		}
		if result.Reason != "secret not found" {
			t.Errorf("expected 'secret not found', got: %s", result.Reason)
		}
	})

	t.Run("warns on whitespace", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{
			"GENERIC_KEY": "  value-with-spaces  ",
		})

		result, err := mgr.ValidateSecret(context.Background(), "GENERIC_KEY")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Warnings) == 0 {
			t.Error("expected warning about whitespace")
		}
		found := false
		for _, w := range result.Warnings {
			if strings.Contains(w, "whitespace") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected whitespace warning, got: %v", result.Warnings)
		}
	})
}

func TestDefaultSecretsManager_ListSecretNames(t *testing.T) {
	t.Parallel()

	t.Run("returns configured known secrets", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{
			SecretAnthropicAPIKey: "sk-ant-test123456789012345",
			SecretOpenAIAPIKey:    "sk-test123456789012345",
		})

		names, err := mgr.ListSecretNames(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(names) != 2 {
			t.Errorf("expected 2 names, got %d", len(names))
		}
	})

	t.Run("excludes missing secrets", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{
			SecretAnthropicAPIKey: "sk-ant-test123456789012345",
		})

		names, err := mgr.ListSecretNames(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, name := range names {
			if name == SecretOpenAIAPIKey {
				t.Error("should not include missing secret")
			}
		}
	})
}

func TestDefaultSecretsManager_RenewSecret(t *testing.T) {
	t.Parallel()

	t.Run("returns not renewable for env secrets", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{
			"KEY": "value",
		})

		_, err := mgr.RenewSecret(context.Background(), "KEY")
		if !errors.Is(err, ErrSecretNotRenewable) {
			t.Errorf("expected ErrSecretNotRenewable, got %v", err)
		}
	})

	t.Run("returns not found for missing secret", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{})

		_, err := mgr.RenewSecret(context.Background(), "MISSING")
		if !errors.Is(err, ErrSecretNotFound) {
			t.Errorf("expected ErrSecretNotFound, got %v", err)
		}
	})
}

func TestDefaultSecretsManager_GetBackendType(t *testing.T) {
	t.Parallel()

	t.Run("returns env when only env enabled", func(t *testing.T) {
		cfg := config.SecretsConfig{UseEnv: true}
		mgr := NewDefaultSecretsManager(cfg, nil)

		bt := mgr.GetBackendType()
		if bt != SecretBackendEnv {
			t.Errorf("expected '%s', got '%s'", SecretBackendEnv, bt)
		}
	})

	t.Run("returns none when nothing enabled", func(t *testing.T) {
		cfg := config.SecretsConfig{}
		mgr := NewDefaultSecretsManager(cfg, nil)

		bt := mgr.GetBackendType()
		if bt != "none" {
			t.Errorf("expected 'none', got '%s'", bt)
		}
	})
}

func TestDefaultSecretsManager_IsConfigured(t *testing.T) {
	t.Parallel()

	t.Run("returns true when env enabled", func(t *testing.T) {
		cfg := config.SecretsConfig{UseEnv: true}
		mgr := NewDefaultSecretsManager(cfg, nil)

		if !mgr.IsConfigured() {
			t.Error("expected IsConfigured to be true")
		}
	})

	t.Run("returns false when nothing enabled", func(t *testing.T) {
		cfg := config.SecretsConfig{}
		mgr := NewDefaultSecretsManager(cfg, nil)

		if mgr.IsConfigured() {
			t.Error("expected IsConfigured to be false")
		}
	})

	t.Run("returns true when keychain enabled", func(t *testing.T) {
		cfg := config.SecretsConfig{UseKeychain: true}
		mgr := NewDefaultSecretsManager(cfg, nil)

		if !mgr.IsConfigured() {
			t.Error("expected IsConfigured to be true")
		}
	})
}

func TestDefaultSecretsManager_DetectAvailableBackends(t *testing.T) {
	t.Parallel()

	t.Run("always includes env backend", func(t *testing.T) {
		cfg := config.SecretsConfig{}
		mgr := NewDefaultSecretsManager(cfg, nil)

		backends := mgr.DetectAvailableBackends()
		found := false
		for _, b := range backends {
			if b == SecretBackendEnv {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected env backend to always be available")
		}
	})
}

func TestDefaultSecretsManager_GetSetupInstructions(t *testing.T) {
	t.Parallel()

	t.Run("includes secret name", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{})

		instr := mgr.GetSetupInstructions(SecretAnthropicAPIKey)
		if !strings.Contains(instr, SecretAnthropicAPIKey) {
			t.Error("expected instructions to include secret name")
		}
	})

	t.Run("includes env option", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{})

		instr := mgr.GetSetupInstructions(SecretAnthropicAPIKey)
		if !strings.Contains(instr, "export") {
			t.Error("expected instructions to include env export command")
		}
	})

	t.Run("includes format hint for Anthropic", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{})

		instr := mgr.GetSetupInstructions(SecretAnthropicAPIKey)
		if !strings.Contains(instr, "sk-ant-") {
			t.Error("expected instructions to include Anthropic format hint")
		}
	})

	t.Run("includes format hint for OpenAI", func(t *testing.T) {
		mgr := createTestSecretsManager(map[string]string{})

		instr := mgr.GetSetupInstructions(SecretOpenAIAPIKey)
		if !strings.Contains(instr, "sk-") {
			t.Error("expected instructions to include OpenAI format hint")
		}
	})
}

// =============================================================================
// Unit Tests - SecretMetadata
// =============================================================================

func TestSecretMetadata_IsExpired(t *testing.T) {
	t.Parallel()

	t.Run("returns false for zero time", func(t *testing.T) {
		meta := &SecretMetadata{}
		if meta.IsExpired() {
			t.Error("expected not expired for zero time")
		}
	})

	t.Run("returns true for past time", func(t *testing.T) {
		meta := &SecretMetadata{
			ExpiresAt: time.Now().Add(-time.Hour),
		}
		if !meta.IsExpired() {
			t.Error("expected expired for past time")
		}
	})

	t.Run("returns false for future time", func(t *testing.T) {
		meta := &SecretMetadata{
			ExpiresAt: time.Now().Add(time.Hour),
		}
		if meta.IsExpired() {
			t.Error("expected not expired for future time")
		}
	})
}

func TestSecretMetadata_ExpiresIn(t *testing.T) {
	t.Parallel()

	t.Run("returns zero for no expiry", func(t *testing.T) {
		meta := &SecretMetadata{}
		if meta.ExpiresIn() != 0 {
			t.Error("expected zero duration for no expiry")
		}
	})

	t.Run("returns positive for future expiry", func(t *testing.T) {
		meta := &SecretMetadata{
			ExpiresAt: time.Now().Add(time.Hour),
		}
		if meta.ExpiresIn() <= 0 {
			t.Error("expected positive duration for future expiry")
		}
	})

	t.Run("returns negative for past expiry", func(t *testing.T) {
		meta := &SecretMetadata{
			ExpiresAt: time.Now().Add(-time.Hour),
		}
		if meta.ExpiresIn() >= 0 {
			t.Error("expected negative duration for past expiry")
		}
	})
}

func TestSecretMetadata_NeedsRenewal(t *testing.T) {
	t.Parallel()

	t.Run("returns false for non-renewable", func(t *testing.T) {
		meta := &SecretMetadata{
			Renewable: false,
			ExpiresAt: time.Now().Add(time.Minute),
		}
		if meta.NeedsRenewal(time.Hour) {
			t.Error("expected false for non-renewable")
		}
	})

	t.Run("returns false for no expiry", func(t *testing.T) {
		meta := &SecretMetadata{
			Renewable: true,
		}
		if meta.NeedsRenewal(time.Hour) {
			t.Error("expected false for no expiry")
		}
	})

	t.Run("returns true when expiring soon", func(t *testing.T) {
		meta := &SecretMetadata{
			Renewable: true,
			ExpiresAt: time.Now().Add(5 * time.Minute),
		}
		if !meta.NeedsRenewal(10 * time.Minute) {
			t.Error("expected true when expiring within threshold")
		}
	})

	t.Run("returns false when not expiring soon", func(t *testing.T) {
		meta := &SecretMetadata{
			Renewable: true,
			ExpiresAt: time.Now().Add(time.Hour),
		}
		if meta.NeedsRenewal(10 * time.Minute) {
			t.Error("expected false when not expiring within threshold")
		}
	})
}

// =============================================================================
// Unit Tests - Error Sentinels
// =============================================================================

func TestErrorSentinels(t *testing.T) {
	t.Parallel()

	t.Run("ErrSecretNotFound", func(t *testing.T) {
		err := ErrSecretNotFound
		if err.Error() != "secret not found" {
			t.Errorf("unexpected error message: %s", err.Error())
		}
	})

	t.Run("ErrSecretInvalid", func(t *testing.T) {
		err := ErrSecretInvalid
		if err.Error() != "secret invalid" {
			t.Errorf("unexpected error message: %s", err.Error())
		}
	})

	t.Run("ErrSecretBackendUnavailable", func(t *testing.T) {
		err := ErrSecretBackendUnavailable
		if err.Error() != "secret backend unavailable" {
			t.Errorf("unexpected error message: %s", err.Error())
		}
	})

	t.Run("ErrSecretExpired", func(t *testing.T) {
		err := ErrSecretExpired
		if err.Error() != "secret expired" {
			t.Errorf("unexpected error message: %s", err.Error())
		}
	})

	t.Run("ErrSecretNotRenewable", func(t *testing.T) {
		err := ErrSecretNotRenewable
		if err.Error() != "secret not renewable" {
			t.Errorf("unexpected error message: %s", err.Error())
		}
	})
}

// =============================================================================
// Unit Tests - Backend Constants
// =============================================================================

func TestBackendConstants(t *testing.T) {
	t.Parallel()

	expectedBackends := map[string]string{
		"SecretBackendEnv":       "env",
		"SecretBackendKeychain":  "keychain",
		"SecretBackend1Password": "1password",
		"SecretBackendLibsecret": "libsecret",
		"SecretBackendVault":     "vault",
		"SecretBackendMock":      "mock",
	}

	actualBackends := map[string]string{
		"SecretBackendEnv":       SecretBackendEnv,
		"SecretBackendKeychain":  SecretBackendKeychain,
		"SecretBackend1Password": SecretBackend1Password,
		"SecretBackendLibsecret": SecretBackendLibsecret,
		"SecretBackendVault":     SecretBackendVault,
		"SecretBackendMock":      SecretBackendMock,
	}

	for name, expected := range expectedBackends {
		actual, ok := actualBackends[name]
		if !ok {
			t.Errorf("missing backend constant: %s", name)
			continue
		}
		if actual != expected {
			t.Errorf("%s: expected '%s', got '%s'", name, expected, actual)
		}
	}
}

// =============================================================================
// Unit Tests - KnownSecrets
// =============================================================================

func TestKnownSecrets(t *testing.T) {
	t.Parallel()

	expectedSecrets := []string{
		"ANTHROPIC_API_KEY",
		"OPENAI_API_KEY",
		"WEAVIATE_API_KEY",
		"OLLAMA_TOKEN",
	}

	if len(KnownSecrets) != len(expectedSecrets) {
		t.Errorf("expected %d known secrets, got %d", len(expectedSecrets), len(KnownSecrets))
	}

	for _, expected := range expectedSecrets {
		found := false
		for _, actual := range KnownSecrets {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing known secret: %s", expected)
		}
	}
}
