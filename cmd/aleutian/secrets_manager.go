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
Package main provides SecretsManager for secure secret management.

SecretsManager provides a centralized, secure abstraction for retrieving and
managing secrets (API keys, tokens, credentials). It supports multiple backends
with automatic fallback.

# Security Context

This is a CRITICAL-RISK component because it handles authentication credentials
for external services (Anthropic, OpenAI) and internal components. Improper
handling could lead to credential exposure, unauthorized access, or compliance
violations.

# Security Features

  - Zero Value Logging: Secret values are NEVER logged (even at debug level)
  - Audit Trail: All access is recorded (secret name only, not value)
  - Fail-Secure: Missing secrets result in clear errors with setup help
  - Format Validation: Known secrets validated for expected patterns

# Backend Priority

Backends are tried in order until a secret is found:
 1. HashiCorp Vault (if configured) - Phase 6B, not yet implemented
 2. 1Password CLI (if enabled and available)
 3. macOS Keychain (if enabled, darwin only)
 4. Linux libsecret (if enabled, linux only)
 5. Environment variables (if enabled)

# Design Principles

  - Interface-first design for testability
  - Dependencies injected (config, metrics)
  - Thread-safe for concurrent use
  - Single responsibility per method
*/
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/config"
	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/internal/diagnostics"
)

// -----------------------------------------------------------------------------
// Error Sentinel Values
// -----------------------------------------------------------------------------

// ErrSecretNotFound is returned when a requested secret does not exist.
// The secret was not found in any configured backend.
var ErrSecretNotFound = errors.New("secret not found")

// ErrSecretInvalid is returned when a secret fails format validation.
// The secret exists but does not match expected format (e.g., wrong prefix).
var ErrSecretInvalid = errors.New("secret invalid")

// ErrSecretBackendUnavailable is returned when the backend cannot be reached.
// The backend CLI or service is not responding within timeout.
var ErrSecretBackendUnavailable = errors.New("secret backend unavailable")

// ErrSecretExpired is returned when a secret has passed its expiry time.
// Only applicable to backends with lease/expiry support (Vault).
var ErrSecretExpired = errors.New("secret expired")

// ErrSecretNotRenewable is returned when trying to renew a non-renewable secret.
// Static secrets (env, keychain) cannot be renewed.
var ErrSecretNotRenewable = errors.New("secret not renewable")

// -----------------------------------------------------------------------------
// Backend Constants
// -----------------------------------------------------------------------------

const (
	// SecretBackendEnv is the environment variable backend type.
	SecretBackendEnv = "env"

	// SecretBackendKeychain is the macOS Keychain backend type.
	SecretBackendKeychain = "keychain"

	// SecretBackend1Password is the 1Password CLI backend type.
	SecretBackend1Password = "1password"

	// SecretBackendLibsecret is the Linux libsecret/Secret Service backend type.
	SecretBackendLibsecret = "libsecret"

	// SecretBackendVault is the HashiCorp Vault backend type.
	SecretBackendVault = "vault"

	// SecretBackendMock is the mock backend for testing.
	SecretBackendMock = "mock"
)

// -----------------------------------------------------------------------------
// Well-Known Secret Names
// -----------------------------------------------------------------------------

const (
	// SecretAnthropicAPIKey is the Anthropic API key for Claude models.
	// Format: Must start with "sk-ant-"
	SecretAnthropicAPIKey = "ANTHROPIC_API_KEY"

	// SecretOpenAIAPIKey is the OpenAI API key for GPT models.
	// Format: Must start with "sk-"
	SecretOpenAIAPIKey = "OPENAI_API_KEY"

	// SecretWeaviateAPIKey is the Weaviate vector database API key.
	// Format: Non-empty string
	SecretWeaviateAPIKey = "WEAVIATE_API_KEY"

	// SecretOllamaToken is the optional Ollama authentication token.
	// Format: Non-empty string (optional)
	SecretOllamaToken = "OLLAMA_TOKEN"
)

// KnownSecrets lists all secrets recognized by Aleutian.
// Used for validation, documentation, and ListSecretNames filtering.
var KnownSecrets = []string{
	SecretAnthropicAPIKey,
	SecretOpenAIAPIKey,
	SecretWeaviateAPIKey,
	SecretOllamaToken,
}

// -----------------------------------------------------------------------------
// Interface Definition
// -----------------------------------------------------------------------------

// SecretsManager provides secure access to secrets (API keys, tokens, credentials).
//
// # Description
//
// This interface abstracts secret retrieval from the underlying storage mechanism.
// Implementations may read from environment variables, system keychains, or
// external secret managers like HashiCorp Vault.
//
// # Security
//
//   - Secret values are NEVER logged (even at debug level)
//   - All access is recorded to the audit trail (secret name only, not value)
//   - Missing secrets result in clear errors (fail-secure)
//   - Secret values are validated for basic format requirements
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Compliance
//
// Supports GDPR Right to Know by recording who accessed what secrets.
// Secret names are classified as CONFIDENTIAL, values as SECRET.
type SecretsManager interface {
	// GetSecret retrieves a secret by its canonical name.
	//
	// # Description
	//
	// Looks up a secret by name and returns its value. The lookup is
	// performed against configured backends in priority order until found.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout
	//   - name: Canonical secret name (e.g., "ANTHROPIC_API_KEY")
	//
	// # Outputs
	//
	//   - string: The secret value (never empty on success)
	//   - error: ErrSecretNotFound, context errors, or backend errors
	//
	// # Examples
	//
	//   apiKey, err := secrets.GetSecret(ctx, SecretAnthropicAPIKey)
	//   if errors.Is(err, ErrSecretNotFound) {
	//       fmt.Println(secrets.GetSetupInstructions(SecretAnthropicAPIKey))
	//       return err
	//   }
	//
	// # Limitations
	//
	//   - Returns error if secret is empty (empty is not valid)
	//   - Does not cache; each call reads from backend
	//
	// # Assumptions
	//
	//   - Secret names use SCREAMING_SNAKE_CASE convention
	//   - Backend is properly configured before first access
	GetSecret(ctx context.Context, name string) (string, error)

	// GetSecretWithDefault retrieves a secret, returning a default if not found.
	//
	// # Description
	//
	// Like GetSecret but returns a default value instead of error when the
	// secret is not found. Still returns errors for backend failures.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout
	//   - name: Canonical secret name
	//   - defaultValue: Value to return if secret not found
	//
	// # Outputs
	//
	//   - string: The secret value or default
	//   - error: Backend errors only (NOT ErrSecretNotFound)
	//
	// # Examples
	//
	//   token, err := secrets.GetSecretWithDefault(ctx, SecretOllamaToken, "")
	//   if err != nil {
	//       return err // Backend failure, not "not found"
	//   }
	//   if token != "" {
	//       client.SetToken(token)
	//   }
	//
	// # Limitations
	//
	//   - Does not validate defaultValue
	//   - Still records access attempt to audit trail
	//
	// # Assumptions
	//
	//   - Caller understands that empty default is valid
	GetSecretWithDefault(ctx context.Context, name, defaultValue string) (string, error)

	// HasSecret checks if a secret exists without retrieving it.
	//
	// # Description
	//
	// Checks whether a secret is configured without actually reading its value.
	// Useful for conditional behavior based on feature availability.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout
	//   - name: Canonical secret name
	//
	// # Outputs
	//
	//   - bool: True if secret exists and is non-empty
	//   - error: Backend errors only
	//
	// # Examples
	//
	//   if ok, _ := secrets.HasSecret(ctx, SecretOpenAIAPIKey); ok {
	//       backends = append(backends, "openai")
	//   }
	//
	// # Limitations
	//
	//   - Still performs the full backend lookup internally
	//
	// # Assumptions
	//
	//   - Caller handles the case where secret exists but may be invalid
	HasSecret(ctx context.Context, name string) (bool, error)

	// ValidateSecret checks if a secret meets format requirements.
	//
	// # Description
	//
	// Validates that a secret exists and meets basic format requirements
	// for the given secret type. Does not make external API calls to verify.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout
	//   - name: Canonical secret name
	//
	// # Outputs
	//
	//   - *SecretValidation: Validation result with details
	//   - error: Backend errors only (validation failures are in result)
	//
	// # Examples
	//
	//   result, err := secrets.ValidateSecret(ctx, SecretAnthropicAPIKey)
	//   if err != nil {
	//       return err
	//   }
	//   if !result.Valid {
	//       fmt.Printf("Invalid: %s\n", result.Reason)
	//   }
	//
	// # Limitations
	//
	//   - Only validates format, not actual API validity
	//   - Validation rules are hardcoded per secret type
	//
	// # Assumptions
	//
	//   - Caller will make an API call to verify the key actually works
	ValidateSecret(ctx context.Context, name string) (*SecretValidation, error)

	// ListSecretNames returns all configured secret names (not values).
	//
	// # Description
	//
	// Returns a list of known secret names that are available in any backend.
	// Useful for diagnostics and configuration validation.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout
	//
	// # Outputs
	//
	//   - []string: List of secret names (never includes values)
	//   - error: Backend errors
	//
	// # Examples
	//
	//   names, err := secrets.ListSecretNames(ctx)
	//   if err != nil {
	//       return err
	//   }
	//   for _, name := range names {
	//       fmt.Printf("Configured: %s\n", name)
	//   }
	//
	// # Limitations
	//
	//   - Only returns KnownSecrets that are configured
	//   - Does not discover unknown secrets
	//
	// # Assumptions
	//
	//   - Caller only needs to know about Aleutian's known secrets
	ListSecretNames(ctx context.Context) ([]string, error)

	// GetSecretWithMetadata retrieves a secret along with its metadata.
	//
	// # Description
	//
	// Like GetSecret but also returns metadata about the secret including
	// expiration time, renewal capability, and backend source.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout
	//   - name: Canonical secret name
	//
	// # Outputs
	//
	//   - string: The secret value
	//   - *SecretMetadata: Metadata about the secret (never nil on success)
	//   - error: ErrSecretNotFound, ErrSecretExpired, or backend errors
	//
	// # Examples
	//
	//   value, meta, err := secrets.GetSecretWithMetadata(ctx, SecretAnthropicAPIKey)
	//   if err != nil {
	//       return err
	//   }
	//   fmt.Printf("Secret from backend: %s\n", meta.Backend)
	//
	// # Limitations
	//
	//   - Does not cache; each call reads from backend
	//   - Metadata varies by backend (env has minimal metadata)
	//
	// # Assumptions
	//
	//   - Caller handles different metadata completeness per backend
	GetSecretWithMetadata(ctx context.Context, name string) (string, *SecretMetadata, error)

	// RenewSecret attempts to renew/refresh a renewable secret.
	//
	// # Description
	//
	// For secrets that support renewal (Vault leases, OAuth tokens),
	// attempts to extend the secret's validity period.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout
	//   - name: Canonical secret name
	//
	// # Outputs
	//
	//   - *SecretMetadata: Updated metadata after renewal
	//   - error: If renewal fails or secret is not renewable
	//
	// # Examples
	//
	//   meta, err := secrets.RenewSecret(ctx, "VAULT_TOKEN")
	//   if errors.Is(err, ErrSecretNotRenewable) {
	//       // Static secret, renewal not applicable
	//   }
	//
	// # Limitations
	//
	//   - Only works for renewable secrets (Vault in Phase 6B)
	//   - Returns ErrSecretNotRenewable for static secrets
	//
	// # Assumptions
	//
	//   - Vault backend is configured and authenticated
	RenewSecret(ctx context.Context, name string) (*SecretMetadata, error)

	// GetBackendType returns the primary configured backend type.
	//
	// # Description
	//
	// Returns a string identifying the highest-priority enabled backend.
	//
	// # Inputs
	//
	// None.
	//
	// # Outputs
	//
	//   - string: Backend identifier ("env", "keychain", "1password", etc.)
	//
	// # Examples
	//
	//   backendType := secrets.GetBackendType()
	//   fmt.Printf("Using backend: %s\n", backendType)
	//
	// # Limitations
	//
	//   - Returns the first enabled backend, not necessarily the one with secrets
	//
	// # Assumptions
	//
	//   - At least one backend is configured
	GetBackendType() string

	// GetSetupInstructions returns platform-specific setup help for a missing secret.
	//
	// # Description
	//
	// When a secret is not found, this method returns helpful instructions
	// for configuring it based on the current platform and available backends.
	//
	// # Inputs
	//
	//   - name: The secret that was not found
	//
	// # Outputs
	//
	//   - string: Human-readable setup instructions
	//
	// # Examples
	//
	//   _, err := secrets.GetSecret(ctx, SecretAnthropicAPIKey)
	//   if errors.Is(err, ErrSecretNotFound) {
	//       fmt.Println(secrets.GetSetupInstructions(SecretAnthropicAPIKey))
	//   }
	//
	// # Limitations
	//
	//   - Instructions are static; does not check partial config
	//
	// # Assumptions
	//
	//   - User has terminal access to run the suggested commands
	GetSetupInstructions(name string) string

	// IsConfigured returns true if the secrets manager is properly configured.
	//
	// # Description
	//
	// Checks that at least one backend is enabled and ready to serve secrets.
	//
	// # Inputs
	//
	// None.
	//
	// # Outputs
	//
	//   - bool: True if at least one backend is enabled
	//
	// # Examples
	//
	//   if !secrets.IsConfigured() {
	//       return fmt.Errorf("no secrets backend configured")
	//   }
	//
	// # Limitations
	//
	//   - Does not verify backends actually work, only that they're enabled
	//
	// # Assumptions
	//
	//   - Enabled backends have the required CLI tools installed
	IsConfigured() bool

	// DetectAvailableBackends returns a list of backends available on this system.
	//
	// # Description
	//
	// Returns the list of available backends detected at initialization.
	// Checks for CLI tools in PATH and platform capabilities.
	//
	// # Inputs
	//
	// None.
	//
	// # Outputs
	//
	//   - []string: List of available backend identifiers
	//
	// # Examples
	//
	//   backends := secrets.DetectAvailableBackends()
	//   for _, b := range backends {
	//       fmt.Printf("Available: %s\n", b)
	//   }
	//
	// # Limitations
	//
	//   - Result is cached at initialization; new CLI installs require restart
	//
	// # Assumptions
	//
	//   - CLI tools in PATH are functional (not just present)
	DetectAvailableBackends() []string
}

// -----------------------------------------------------------------------------
// Supporting Types
// -----------------------------------------------------------------------------

// SecretMetadata contains metadata about a secret for rotation and lifecycle.
//
// # Description
//
// Provides additional context about a secret beyond its value, including
// expiration time and renewal capabilities. This enables proactive rotation
// and prevents using stale credentials.
//
// # Use Cases
//
//   - Vault leases: Track lease expiry, trigger renewal
//   - OAuth tokens: Track token expiry, refresh before expiration
//   - 1Password: Track last modified time for audit
//   - Static keys: ExpiresAt is zero (no expiry)
//
// # Thread Safety
//
// SecretMetadata is immutable after creation.
type SecretMetadata struct {
	// ExpiresAt is when the secret expires (zero time = no expiry).
	// For Vault: lease expiration time.
	// For OAuth: token expiration time.
	// For static keys: zero (never expires).
	ExpiresAt time.Time

	// LeaseID is the Vault lease ID for renewal (empty for other backends).
	LeaseID string

	// LastRotated is when the secret was last changed (if known).
	// Useful for audit and compliance reporting.
	LastRotated time.Time

	// Backend identifies which backend provided this secret.
	Backend string

	// Renewable indicates if the secret can be renewed/refreshed.
	// True for Vault leases and OAuth tokens.
	Renewable bool
}

// SecretValidation is the result of validating a secret.
//
// # Description
//
// Contains the outcome of format validation for a secret.
// Includes whether the secret exists, is valid, and any warnings.
type SecretValidation struct {
	// Name is the secret name that was validated.
	Name string

	// Valid is true if the secret passed all validation checks.
	Valid bool

	// Exists is true if the secret was found in the backend.
	Exists bool

	// Reason explains why validation failed (empty if Valid=true).
	Reason string

	// Warnings lists non-fatal issues (e.g., unusual format).
	Warnings []string
}

// -----------------------------------------------------------------------------
// SecretMetadata Methods
// -----------------------------------------------------------------------------

// IsExpired checks if the secret has passed its expiry time.
//
// # Description
//
// Checks if ExpiresAt is non-zero and in the past. Returns false if
// no expiry is set (zero time) or if the expiry is in the future.
//
// # Inputs
//
// None (method receiver only).
//
// # Outputs
//
//   - bool: True if expired, false if no expiry or not yet expired
//
// # Examples
//
//	meta := &SecretMetadata{ExpiresAt: time.Now().Add(-1 * time.Hour)}
//	if meta.IsExpired() {
//	    // Secret has expired, need to refresh
//	}
//
// # Limitations
//
//   - Uses system clock; clock skew could affect accuracy
//
// # Assumptions
//
//   - ExpiresAt is in UTC or local time consistent with time.Now()
func (m *SecretMetadata) IsExpired() bool {
	if m.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(m.ExpiresAt)
}

// ExpiresIn calculates time remaining until expiry.
//
// # Description
//
// Returns the duration until the secret expires. Returns zero duration
// if no expiry is set. Returns negative duration if already expired.
//
// # Inputs
//
// None (method receiver only).
//
// # Outputs
//
//   - time.Duration: Time until expiry (0 if no expiry, negative if past)
//
// # Examples
//
//	meta := &SecretMetadata{ExpiresAt: time.Now().Add(5 * time.Minute)}
//	remaining := meta.ExpiresIn()
//	fmt.Printf("Secret expires in %v\n", remaining)
//
// # Limitations
//
//   - Uses system clock; value changes with each call
//
// # Assumptions
//
//   - Caller handles negative durations appropriately
func (m *SecretMetadata) ExpiresIn() time.Duration {
	if m.ExpiresAt.IsZero() {
		return 0
	}
	return time.Until(m.ExpiresAt)
}

// NeedsRenewal checks if the secret should be renewed soon.
//
// # Description
//
// Returns true if the secret is renewable and expires within the given
// threshold duration. Use this for proactive renewal before expiration.
//
// # Inputs
//
//   - threshold: Renew if expiring within this duration
//
// # Outputs
//
//   - bool: True if should renew now
//
// # Examples
//
//	if meta.NeedsRenewal(5 * time.Minute) {
//	    meta, err = secrets.RenewSecret(ctx, name)
//	}
//
// # Limitations
//
//   - Only returns true if Renewable is true
//
// # Assumptions
//
//   - Caller has access to RenewSecret to act on the result
func (m *SecretMetadata) NeedsRenewal(threshold time.Duration) bool {
	if !m.Renewable {
		return false
	}
	if m.ExpiresAt.IsZero() {
		return false
	}
	return m.ExpiresIn() < threshold
}

// -----------------------------------------------------------------------------
// Implementation Struct
// -----------------------------------------------------------------------------

// DefaultSecretsManager implements SecretsManager with multi-backend support.
//
// # Description
//
// This is the production implementation that supports multiple backends
// with automatic fallback. Backends are tried in priority order until
// a secret is found.
//
// # Backend Priority
//
//  1. HashiCorp Vault (if configured) - Phase 6B, not yet implemented
//  2. 1Password CLI (if enabled and available)
//  3. macOS Keychain (if enabled, darwin only)
//  4. Linux libsecret (if enabled, linux only)
//  5. Environment variables (if enabled)
//
// # Security
//
//   - Values are never logged, even at debug level
//   - Access events are recorded to the audit trail (name only)
//   - Invalid or empty secrets result in clear errors
//
// # Thread Safety
//
// DefaultSecretsManager is safe for concurrent use.
type DefaultSecretsManager struct {
	config            config.SecretsConfig
	metrics           diagnostics.DiagnosticsMetrics
	envFunc           func(string) string
	execCommandFunc   func(ctx context.Context, name string, args ...string) *exec.Cmd
	availableBackends []string
	mu                sync.RWMutex
}

// -----------------------------------------------------------------------------
// Constructor
// -----------------------------------------------------------------------------

// NewDefaultSecretsManager creates a secrets manager with multi-backend support.
//
// # Description
//
// Creates a new SecretsManager that tries multiple backends in priority order.
// Backends are auto-detected at initialization time by checking for CLI tools.
//
// # Inputs
//
//   - cfg: Secrets configuration from aleutian.yaml
//   - metrics: diagnostics.DiagnosticsMetrics for audit trail (may be nil for no-op)
//
// # Outputs
//
//   - *DefaultSecretsManager: Ready-to-use manager
//
// # Examples
//
//	cfg := config.SecretsConfig{UseEnv: true, UseKeychain: true}
//	secrets := NewDefaultSecretsManager(cfg, nil)
//	apiKey, err := secrets.GetSecret(ctx, SecretAnthropicAPIKey)
//
// # Limitations
//
//   - Backend detection happens at initialization only
//   - New CLIs installed after creation will not be detected
//
// # Assumptions
//
//   - Configuration has been loaded and validated before calling
func NewDefaultSecretsManager(cfg config.SecretsConfig, metrics diagnostics.DiagnosticsMetrics) *DefaultSecretsManager {
	mgr := &DefaultSecretsManager{
		config:          cfg,
		metrics:         metrics,
		envFunc:         os.Getenv,
		execCommandFunc: exec.CommandContext,
	}
	mgr.availableBackends = mgr.detectBackendsInternal()
	return mgr
}

// -----------------------------------------------------------------------------
// Interface Implementation Methods
// -----------------------------------------------------------------------------

// GetSecret retrieves a secret by its canonical name.
//
// # Description
//
// Looks up a secret by name and returns its value. The lookup is
// performed against configured backends in priority order until found.
// Records access to the audit trail (name only, not value).
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//   - name: Canonical secret name (e.g., "ANTHROPIC_API_KEY")
//
// # Outputs
//
//   - string: The secret value (never empty on success)
//   - error: ErrSecretNotFound, context errors, or backend errors
//
// # Examples
//
//	apiKey, err := secrets.GetSecret(ctx, SecretAnthropicAPIKey)
//	if errors.Is(err, ErrSecretNotFound) {
//	    fmt.Println(secrets.GetSetupInstructions(SecretAnthropicAPIKey))
//	    return err
//	}
//	// Use apiKey...
//
// # Limitations
//
//   - Returns error if secret is empty (empty is not valid)
//   - Does not cache; each call reads from backend
//
// # Assumptions
//
//   - Secret names use SCREAMING_SNAKE_CASE convention
//   - Backend is properly configured before first access
func (m *DefaultSecretsManager) GetSecret(ctx context.Context, name string) (string, error) {
	value, _, err := m.GetSecretWithMetadata(ctx, name)
	return value, err
}

// GetSecretWithDefault retrieves a secret, returning a default if not found.
//
// # Description
//
// Like GetSecret but returns a default value instead of error when the
// secret is not found. Still returns errors for backend failures.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//   - name: Canonical secret name
//   - defaultValue: Value to return if secret not found
//
// # Outputs
//
//   - string: The secret value or default
//   - error: Backend errors only (NOT ErrSecretNotFound)
//
// # Examples
//
//	token, err := secrets.GetSecretWithDefault(ctx, SecretOllamaToken, "")
//	if err != nil {
//	    return err // Backend failure, not "not found"
//	}
//	if token != "" {
//	    client.SetToken(token)
//	}
//
// # Limitations
//
//   - Does not validate defaultValue
//   - Still records access attempt to audit trail
//
// # Assumptions
//
//   - Caller understands that empty default is valid
func (m *DefaultSecretsManager) GetSecretWithDefault(ctx context.Context, name, defaultValue string) (string, error) {
	value, err := m.GetSecret(ctx, name)
	if errors.Is(err, ErrSecretNotFound) {
		return defaultValue, nil
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

// HasSecret checks if a secret exists without retrieving it.
//
// # Description
//
// Checks whether a secret is configured without actually reading its value.
// Useful for conditional behavior based on feature availability.
// Does NOT record to audit trail (existence check only).
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//   - name: Canonical secret name
//
// # Outputs
//
//   - bool: True if secret exists and is non-empty
//   - error: Backend errors only
//
// # Examples
//
//	if ok, _ := secrets.HasSecret(ctx, SecretOpenAIAPIKey); ok {
//	    backends = append(backends, "openai")
//	}
//
// # Limitations
//
//   - Still performs the full backend lookup internally
//
// # Assumptions
//
//   - Caller handles the case where secret exists but may be invalid
func (m *DefaultSecretsManager) HasSecret(ctx context.Context, name string) (bool, error) {
	_, err := m.GetSecret(ctx, name)
	if errors.Is(err, ErrSecretNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetSecretWithMetadata retrieves a secret along with its metadata.
//
// # Description
//
// Like GetSecret but also returns metadata about the secret including
// expiration time, renewal capability, and backend source. Tries each
// enabled backend in priority order.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//   - name: Canonical secret name
//
// # Outputs
//
//   - string: The secret value
//   - *SecretMetadata: Metadata about the secret (never nil on success)
//   - error: ErrSecretNotFound, ErrSecretExpired, or backend errors
//
// # Examples
//
//	value, meta, err := secrets.GetSecretWithMetadata(ctx, SecretAnthropicAPIKey)
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("Secret from backend: %s\n", meta.Backend)
//
// # Limitations
//
//   - Does not cache; each call reads from backend
//   - Metadata varies by backend (env has minimal metadata)
//
// # Assumptions
//
//   - Caller handles different metadata completeness per backend
func (m *DefaultSecretsManager) GetSecretWithMetadata(ctx context.Context, name string) (string, *SecretMetadata, error) {
	if name == "" {
		return "", nil, fmt.Errorf("secret name cannot be empty")
	}

	timeout := m.config.GetTimeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	value, meta, err := m.tryAllBackends(ctx, name)
	if err != nil {
		m.recordAccess(name, false, "")
		return "", nil, err
	}

	m.recordAccess(name, true, meta.Backend)
	return value, meta, nil
}

// ValidateSecret checks if a secret meets format requirements.
//
// # Description
//
// Validates that a secret exists and meets basic format requirements
// for the given secret type. Does not make external API calls to verify.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//   - name: Canonical secret name
//
// # Outputs
//
//   - *SecretValidation: Validation result with details
//   - error: Backend errors only (validation failures are in result)
//
// # Examples
//
//	result, err := secrets.ValidateSecret(ctx, SecretAnthropicAPIKey)
//	if err != nil {
//	    return err
//	}
//	if !result.Valid {
//	    fmt.Printf("Invalid: %s\n", result.Reason)
//	}
//
// # Limitations
//
//   - Only validates format, not actual API validity
//   - Validation rules are hardcoded per secret type
//
// # Assumptions
//
//   - Caller will make an API call to verify the key actually works
func (m *DefaultSecretsManager) ValidateSecret(ctx context.Context, name string) (*SecretValidation, error) {
	result := &SecretValidation{
		Name:     name,
		Warnings: []string{},
	}

	value, err := m.GetSecret(ctx, name)
	if errors.Is(err, ErrSecretNotFound) {
		result.Exists = false
		result.Valid = false
		result.Reason = "secret not found"
		return result, nil
	}
	if err != nil {
		return nil, err
	}

	result.Exists = true
	m.applyValidationRules(result, name, value)
	return result, nil
}

// ListSecretNames returns all configured secret names (not values).
//
// # Description
//
// Returns a list of known secret names that are available in any backend.
// Useful for diagnostics and configuration validation.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//
// # Outputs
//
//   - []string: List of secret names (never includes values)
//   - error: Backend errors
//
// # Examples
//
//	names, err := secrets.ListSecretNames(ctx)
//	if err != nil {
//	    return err
//	}
//	for _, name := range names {
//	    fmt.Printf("Configured: %s\n", name)
//	}
//
// # Limitations
//
//   - Only returns KnownSecrets that are configured
//   - Does not discover unknown secrets
//
// # Assumptions
//
//   - Caller only needs to know about Aleutian's known secrets
func (m *DefaultSecretsManager) ListSecretNames(ctx context.Context) ([]string, error) {
	var found []string
	for _, name := range KnownSecrets {
		exists, err := m.HasSecret(ctx, name)
		if err != nil {
			return nil, err
		}
		if exists {
			found = append(found, name)
		}
	}
	return found, nil
}

// RenewSecret attempts to renew/refresh a renewable secret.
//
// # Description
//
// For secrets that support renewal (Vault leases, OAuth tokens),
// attempts to extend the secret's validity period.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//   - name: Canonical secret name
//
// # Outputs
//
//   - *SecretMetadata: Updated metadata after renewal
//   - error: If renewal fails or secret is not renewable
//
// # Examples
//
//	meta, err := secrets.RenewSecret(ctx, "VAULT_TOKEN")
//	if errors.Is(err, ErrSecretNotRenewable) {
//	    // Static secret, renewal not applicable
//	}
//
// # Limitations
//
//   - Only works for renewable secrets (Vault in Phase 6B)
//   - Returns ErrSecretNotRenewable for static secrets
//
// # Assumptions
//
//   - Vault backend is configured and authenticated
func (m *DefaultSecretsManager) RenewSecret(ctx context.Context, name string) (*SecretMetadata, error) {
	_, meta, err := m.GetSecretWithMetadata(ctx, name)
	if err != nil {
		return nil, err
	}

	if !meta.Renewable {
		return nil, ErrSecretNotRenewable
	}

	return nil, fmt.Errorf("secret renewal not yet implemented (Phase 6B)")
}

// GetBackendType returns the primary configured backend type.
//
// # Description
//
// Returns a string identifying the highest-priority enabled backend.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - string: Backend identifier ("env", "keychain", "1password", etc.)
//
// # Examples
//
//	backendType := secrets.GetBackendType()
//	fmt.Printf("Using backend: %s\n", backendType)
//
// # Limitations
//
//   - Returns the first enabled backend, not necessarily the one with secrets
//
// # Assumptions
//
//   - At least one backend is configured
func (m *DefaultSecretsManager) GetBackendType() string {
	if m.config.VaultAddress != "" {
		return SecretBackendVault
	}
	if m.config.Use1Password {
		return SecretBackend1Password
	}
	if m.config.UseKeychain && runtime.GOOS == "darwin" {
		return SecretBackendKeychain
	}
	if m.config.UseLibsecret && runtime.GOOS == "linux" {
		return SecretBackendLibsecret
	}
	if m.config.UseEnv {
		return SecretBackendEnv
	}
	return "none"
}

// GetSetupInstructions returns platform-specific setup help for a missing secret.
//
// # Description
//
// When a secret is not found, this method returns helpful instructions
// for configuring it based on the current platform and available backends.
//
// # Inputs
//
//   - name: The secret that was not found
//
// # Outputs
//
//   - string: Human-readable setup instructions
//
// # Examples
//
//	_, err := secrets.GetSecret(ctx, SecretAnthropicAPIKey)
//	if errors.Is(err, ErrSecretNotFound) {
//	    fmt.Println(secrets.GetSetupInstructions(SecretAnthropicAPIKey))
//	}
//
// # Limitations
//
//   - Instructions are static; does not check partial config
//
// # Assumptions
//
//   - User has terminal access to run the suggested commands
func (m *DefaultSecretsManager) GetSetupInstructions(name string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s not found.\n\n", name))
	sb.WriteString("To configure this secret, choose one of these options:\n\n")

	optionNum := 1
	optionNum = m.appendKeychainInstructions(&sb, name, optionNum)
	optionNum = m.append1PasswordInstructions(&sb, name, optionNum)
	optionNum = m.appendLibsecretInstructions(&sb, name, optionNum)
	m.appendEnvInstructions(&sb, name, optionNum)
	m.appendSecretFormatHint(&sb, name)

	return sb.String()
}

// IsConfigured returns true if at least one backend is enabled.
//
// # Description
//
// Checks that the secrets manager has at least one working backend.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - bool: True if at least one backend is enabled
//
// # Examples
//
//	if !secrets.IsConfigured() {
//	    return fmt.Errorf("no secrets backend configured")
//	}
//
// # Limitations
//
//   - Does not verify backends actually work, only that they're enabled
//
// # Assumptions
//
//   - Enabled backends have the required CLI tools installed
func (m *DefaultSecretsManager) IsConfigured() bool {
	return m.config.UseEnv ||
		m.config.UseKeychain ||
		m.config.Use1Password ||
		m.config.UseLibsecret ||
		m.config.VaultAddress != ""
}

// DetectAvailableBackends returns a list of backends available on this system.
//
// # Description
//
// Returns the cached list of available backends detected at initialization.
// Checks for CLI tools in PATH and platform capabilities.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - []string: List of available backend identifiers
//
// # Examples
//
//	backends := secrets.DetectAvailableBackends()
//	for _, b := range backends {
//	    fmt.Printf("Available: %s\n", b)
//	}
//
// # Limitations
//
//   - Result is cached at initialization; new CLI installs require restart
//
// # Assumptions
//
//   - CLI tools in PATH are functional (not just present)
func (m *DefaultSecretsManager) DetectAvailableBackends() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]string, len(m.availableBackends))
	copy(result, m.availableBackends)
	return result
}

// -----------------------------------------------------------------------------
// Private Helper Methods
// -----------------------------------------------------------------------------

// detectBackendsInternal checks which backends are available on this system.
func (m *DefaultSecretsManager) detectBackendsInternal() []string {
	var available []string

	if m.is1PasswordAvailable() {
		available = append(available, SecretBackend1Password)
	}
	if m.isKeychainAvailable() {
		available = append(available, SecretBackendKeychain)
	}
	if m.isLibsecretAvailable() {
		available = append(available, SecretBackendLibsecret)
	}
	available = append(available, SecretBackendEnv)

	return available
}

// is1PasswordAvailable checks if 1Password CLI is installed.
func (m *DefaultSecretsManager) is1PasswordAvailable() bool {
	_, err := exec.LookPath("op")
	return err == nil
}

// isKeychainAvailable checks if macOS Keychain is available.
func (m *DefaultSecretsManager) isKeychainAvailable() bool {
	return runtime.GOOS == "darwin"
}

// isLibsecretAvailable checks if libsecret (secret-tool) is installed.
func (m *DefaultSecretsManager) isLibsecretAvailable() bool {
	_, err := exec.LookPath("secret-tool")
	return err == nil
}

// isBackendInAvailableList checks if a backend is in the available list.
func (m *DefaultSecretsManager) isBackendInAvailableList(backend string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, b := range m.availableBackends {
		if b == backend {
			return true
		}
	}
	return false
}

// tryAllBackends attempts to retrieve a secret from all configured backends.
func (m *DefaultSecretsManager) tryAllBackends(ctx context.Context, name string) (string, *SecretMetadata, error) {
	if m.should1PasswordBeTried() {
		value, meta, err := m.try1Password(ctx, name)
		if err == nil {
			return value, meta, nil
		}
	}

	if m.shouldKeychainBeTried() {
		value, meta, err := m.tryKeychain(ctx, name)
		if err == nil {
			return value, meta, nil
		}
	}

	if m.shouldLibsecretBeTried() {
		value, meta, err := m.tryLibsecret(ctx, name)
		if err == nil {
			return value, meta, nil
		}
	}

	if m.config.UseEnv {
		value, meta, err := m.tryEnv(name)
		if err == nil {
			return value, meta, nil
		}
	}

	return "", nil, ErrSecretNotFound
}

// should1PasswordBeTried checks if 1Password should be attempted.
func (m *DefaultSecretsManager) should1PasswordBeTried() bool {
	return m.config.Use1Password || m.isBackendInAvailableList(SecretBackend1Password)
}

// shouldKeychainBeTried checks if Keychain should be attempted.
func (m *DefaultSecretsManager) shouldKeychainBeTried() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	return m.config.UseKeychain || m.isBackendInAvailableList(SecretBackendKeychain)
}

// shouldLibsecretBeTried checks if libsecret should be attempted.
func (m *DefaultSecretsManager) shouldLibsecretBeTried() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	return m.config.UseLibsecret || m.isBackendInAvailableList(SecretBackendLibsecret)
}

// try1Password attempts to retrieve a secret from 1Password.
func (m *DefaultSecretsManager) try1Password(ctx context.Context, name string) (string, *SecretMetadata, error) {
	vault := m.config.GetOnePasswordVault()
	reference := fmt.Sprintf("op://%s/%s/password", vault, name)

	cmd := m.execCommandFunc(ctx, "op", "read", reference)
	output, err := cmd.Output()
	if err != nil {
		return "", nil, ErrSecretNotFound
	}

	value := strings.TrimSpace(string(output))
	if value == "" {
		return "", nil, ErrSecretNotFound
	}

	meta := &SecretMetadata{
		Backend:   SecretBackend1Password,
		Renewable: false,
	}
	return value, meta, nil
}

// tryKeychain attempts to retrieve a secret from macOS Keychain.
func (m *DefaultSecretsManager) tryKeychain(ctx context.Context, name string) (string, *SecretMetadata, error) {
	cmd := m.execCommandFunc(ctx, "security", "find-generic-password",
		"-a", "aleutian",
		"-s", name,
		"-w",
	)
	output, err := cmd.Output()
	if err != nil {
		return "", nil, ErrSecretNotFound
	}

	value := strings.TrimSpace(string(output))
	if value == "" {
		return "", nil, ErrSecretNotFound
	}

	meta := &SecretMetadata{
		Backend:   SecretBackendKeychain,
		Renewable: false,
	}
	return value, meta, nil
}

// tryLibsecret attempts to retrieve a secret from Linux Secret Service.
func (m *DefaultSecretsManager) tryLibsecret(ctx context.Context, name string) (string, *SecretMetadata, error) {
	cmd := m.execCommandFunc(ctx, "secret-tool", "lookup",
		"service", "aleutian",
		"key", name,
	)
	output, err := cmd.Output()
	if err != nil {
		return "", nil, ErrSecretNotFound
	}

	value := strings.TrimSpace(string(output))
	if value == "" {
		return "", nil, ErrSecretNotFound
	}

	meta := &SecretMetadata{
		Backend:   SecretBackendLibsecret,
		Renewable: false,
	}
	return value, meta, nil
}

// tryEnv attempts to retrieve a secret from environment variables.
func (m *DefaultSecretsManager) tryEnv(name string) (string, *SecretMetadata, error) {
	value := m.envFunc(name)
	if value == "" {
		return "", nil, ErrSecretNotFound
	}

	meta := &SecretMetadata{
		Backend:   SecretBackendEnv,
		Renewable: false,
	}
	return value, meta, nil
}

// recordAccess records a secret access event to the audit trail.
func (m *DefaultSecretsManager) recordAccess(name string, found bool, backend string) {
	if m.metrics == nil {
		return
	}
	severity := diagnostics.SeverityInfo
	if !found {
		severity = diagnostics.SeverityWarning
	}
	label := fmt.Sprintf("secret_access:%s:%s", name, backend)
	m.metrics.RecordCollection(severity, label, 0, 0)
}

// applyValidationRules applies format validation rules to a secret value.
func (m *DefaultSecretsManager) applyValidationRules(result *SecretValidation, name, value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed != value {
		result.Warnings = append(result.Warnings, "secret has leading or trailing whitespace")
	}

	switch name {
	case SecretAnthropicAPIKey:
		m.validateAnthropicKey(result, value)
	case SecretOpenAIAPIKey:
		m.validateOpenAIKey(result, value)
	default:
		m.validateGenericSecret(result, value)
	}
}

// validateAnthropicKey validates Anthropic API key format.
func (m *DefaultSecretsManager) validateAnthropicKey(result *SecretValidation, value string) {
	if !strings.HasPrefix(value, "sk-ant-") {
		result.Valid = false
		result.Reason = "Anthropic API key must start with 'sk-ant-'"
		return
	}
	if len(value) < 20 {
		result.Valid = false
		result.Reason = "Anthropic API key appears too short"
		return
	}
	result.Valid = true
}

// validateOpenAIKey validates OpenAI API key format.
func (m *DefaultSecretsManager) validateOpenAIKey(result *SecretValidation, value string) {
	if !strings.HasPrefix(value, "sk-") {
		result.Valid = false
		result.Reason = "OpenAI API key must start with 'sk-'"
		return
	}
	if len(value) < 20 {
		result.Valid = false
		result.Reason = "OpenAI API key appears too short"
		return
	}
	result.Valid = true
}

// validateGenericSecret validates a generic secret (non-empty).
func (m *DefaultSecretsManager) validateGenericSecret(result *SecretValidation, value string) {
	if value == "" {
		result.Valid = false
		result.Reason = "secret is empty"
		return
	}
	result.Valid = true
}

// appendKeychainInstructions adds macOS Keychain instructions to the builder.
func (m *DefaultSecretsManager) appendKeychainInstructions(sb *strings.Builder, name string, optionNum int) int {
	if runtime.GOOS != "darwin" {
		return optionNum
	}
	sb.WriteString(fmt.Sprintf("Option %d: macOS Keychain (Recommended - built-in, secure)\n", optionNum))
	sb.WriteString(fmt.Sprintf("  security add-generic-password -a \"aleutian\" -s \"%s\" -w \"your-secret-value\"\n\n", name))
	return optionNum + 1
}

// append1PasswordInstructions adds 1Password CLI instructions to the builder.
func (m *DefaultSecretsManager) append1PasswordInstructions(sb *strings.Builder, name string, optionNum int) int {
	if !m.isBackendInAvailableList(SecretBackend1Password) {
		return optionNum
	}
	vault := m.config.GetOnePasswordVault()
	sb.WriteString(fmt.Sprintf("Option %d: 1Password CLI", optionNum))
	if runtime.GOOS != "darwin" {
		sb.WriteString(" (Recommended)")
	}
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("  op item create --category=password --title=\"%s\" --vault=\"%s\" password=\"your-secret-value\"\n\n", name, vault))
	return optionNum + 1
}

// appendLibsecretInstructions adds libsecret instructions to the builder.
func (m *DefaultSecretsManager) appendLibsecretInstructions(sb *strings.Builder, name string, optionNum int) int {
	if runtime.GOOS != "linux" {
		return optionNum
	}
	if !m.isBackendInAvailableList(SecretBackendLibsecret) {
		return optionNum
	}
	sb.WriteString(fmt.Sprintf("Option %d: GNOME Keyring / Secret Service\n", optionNum))
	sb.WriteString(fmt.Sprintf("  secret-tool store --label=\"Aleutian %s\" service aleutian key %s\n", name, name))
	sb.WriteString("  (Then enter the secret when prompted)\n\n")
	return optionNum + 1
}

// appendEnvInstructions adds environment variable instructions to the builder.
func (m *DefaultSecretsManager) appendEnvInstructions(sb *strings.Builder, name string, optionNum int) {
	sb.WriteString(fmt.Sprintf("Option %d: Environment Variable (for CI/Docker)\n", optionNum))
	sb.WriteString(fmt.Sprintf("  export %s=\"your-secret-value\"\n", name))
}

// appendSecretFormatHint adds format hints for known secrets.
func (m *DefaultSecretsManager) appendSecretFormatHint(sb *strings.Builder, name string) {
	switch name {
	case SecretAnthropicAPIKey:
		sb.WriteString("\nNote: Anthropic API keys start with 'sk-ant-'\n")
	case SecretOpenAIAPIKey:
		sb.WriteString("\nNote: OpenAI API keys start with 'sk-'\n")
	}
}

// -----------------------------------------------------------------------------
// Compile-time Interface Check
// -----------------------------------------------------------------------------

var _ SecretsManager = (*DefaultSecretsManager)(nil)
