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
Package config provides configuration types and loading for the Aleutian CLI.

# Overview

This package defines the configuration schema for Aleutian, including:
  - Machine configuration for Podman VM
  - Model backend settings (Ollama, OpenAI, Anthropic)
  - Feature toggles and extensions
  - Forecast module configuration
  - Custom optimization profiles

# Configuration File

The configuration is stored at ~/.aleutian/aleutian.yaml and is created
automatically on first run with sensible defaults.

# Example

	model_backend:
	  type: ollama
	  ollama:
	    embedding_model: nomic-embed-text-v2-moe
	    disk_limit_gb: 50
*/
package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// -----------------------------------------------------------------------------
// Constants
// -----------------------------------------------------------------------------

const (
	// DefaultEmbeddingModel is the default model for document embeddings.
	DefaultEmbeddingModel = "nomic-embed-text-v2-moe"

	// DefaultLLMModel is the default model for chat/generation.
	DefaultLLMModel = "gpt-oss"

	// DefaultDiskLimitGB is the default maximum disk space for models.
	DefaultDiskLimitGB = 50

	// DefaultOllamaHostURL is the Ollama URL when accessed from the host.
	DefaultOllamaHostURL = "http://localhost:11434"

	// DefaultOllamaContainerURL is the Ollama URL when accessed from containers.
	DefaultOllamaContainerURL = "http://host.containers.internal:11434"
)

// -----------------------------------------------------------------------------
// Enums
// -----------------------------------------------------------------------------

// ForecastMode defines how the forecast service is deployed.
//
// # Description
//
// ForecastMode determines whether Aleutian runs its own forecast service
// or connects to an external Sapheneia instance.
//
// # Values
//
//   - ForecastModeStandalone: Runs Aleutian's containerized forecast service
//   - ForecastModeSapheneia: Connects to external Sapheneia containers
type ForecastMode string

const (
	// ForecastModeStandalone runs Aleutian's own forecast service.
	ForecastModeStandalone ForecastMode = "standalone"

	// ForecastModeSapheneia connects to external Sapheneia containers.
	ForecastModeSapheneia ForecastMode = "sapheneia"
)

// IsValid checks if the mode is a known value.
//
// # Description
//
// Returns true if the ForecastMode is one of the defined constants.
//
// # Outputs
//
//   - bool: True if valid, false otherwise
func (m ForecastMode) IsValid() bool {
	switch m {
	case ForecastModeStandalone, ForecastModeSapheneia:
		return true
	}
	return false
}

// -----------------------------------------------------------------------------
// Primary Configuration Types
// -----------------------------------------------------------------------------

// AleutianConfig is the root configuration structure for the Aleutian CLI.
//
// # Description
//
// Contains all configuration sections for the Aleutian system, including
// infrastructure, model backend, features, and optional modules.
//
// # Fields
//
//   - Machine: Podman machine configuration (macOS only)
//   - Extensions: Paths to custom compose files
//   - Secrets: Secret storage configuration
//   - Features: Feature toggle flags
//   - ModelBackend: LLM backend configuration
//   - Forecast: Timeseries forecasting module
//   - Profiles: Custom optimization profiles
//
// # Example
//
//	cfg := config.DefaultConfig()
//	cfg.ModelBackend.Type = "ollama"
//	cfg.ModelBackend.Ollama.EmbeddingModel = "nomic-embed-text-v2-moe"
type AleutianConfig struct {
	// Meta contains versioning and audit information.
	// Required for compliance with GDPR, HIPAA, and CCPA.
	Meta ConfigMeta `yaml:"meta"`

	// Machine configures the Podman virtual machine (macOS only).
	Machine MachineConfig `yaml:"machine"`

	// Extensions lists paths to custom podman-compose files.
	Extensions []string `yaml:"extensions"`

	// Secrets configures secret storage (env vars or keychain).
	Secrets SecretsConfig `yaml:"secrets"`

	// Features toggles optional system services.
	Features FeatureConfig `yaml:"features"`

	// ModelBackend configures the LLM backend (ollama, openai, etc.).
	ModelBackend BackendConfig `yaml:"model_backend"`

	// Forecast configures the optional timeseries forecast module.
	Forecast ForecastConfig `yaml:"forecast"`

	// ModelManagement configures model downloads, verification, and governance.
	// Includes version pinning, auto-selection, fallback chains, and audit logging.
	ModelManagement ModelManagementConfig `yaml:"model_management"`

	// Profiles defines custom optimization profiles.
	// These extend the built-in profiles (low, standard, performance, ultra).
	Profiles []ProfileConfig `yaml:"profiles,omitempty"`
}

// -----------------------------------------------------------------------------
// Infrastructure Configuration
// -----------------------------------------------------------------------------

// MachineConfig configures the Podman virtual machine.
//
// # Description
//
// On macOS, containers run inside a Linux VM managed by Podman.
// This configuration controls the VM's resources and mount points.
//
// # Fields
//
//   - Id: Machine name (default: "podman-machine-default")
//   - CPUCount: Number of CPU cores allocated
//   - MemoryAmount: RAM in MB allocated to the VM
//   - Drives: Host paths to mount into the VM
//
// # Limitations
//
// This configuration is only used on macOS. On Linux, containers
// run natively without a VM.
type MachineConfig struct {
	// Id is the Podman machine name.
	Id string `yaml:"id"`

	// CPUCount is the number of CPU cores for the VM.
	CPUCount int `yaml:"cpu_count"`

	// MemoryAmount is the RAM allocation in MB.
	MemoryAmount int `yaml:"memory_amount"`

	// Drives lists host paths to mount into the VM.
	Drives []string `yaml:"drives"`
}

// SecretsConfig configures how secrets are stored and accessed.
//
// # Description
//
// Configures secret management with support for multiple backends.
// Backends are tried in priority order until a secret is found.
//
// # Backend Priority Order
//
//  1. HashiCorp Vault (if VaultAddress is set) - Enterprise/production
//  2. 1Password CLI (if Use1Password is true) - Cross-platform, recommended
//  3. macOS Keychain (if UseKeychain is true) - macOS only, built-in
//  4. Linux libsecret (if UseLibsecret is true) - Linux only, GNOME/KDE
//  5. Environment variables (if UseEnv is true) - Fallback, CI/Docker
//
// # Auto-Detection
//
// By default, backends are auto-detected based on platform and available
// CLI tools. Explicit configuration overrides auto-detection.
//
// # Example YAML
//
//	secrets:
//	  use_env: true            # Fallback for CI/Docker
//	  use_keychain: true       # macOS (auto-detected)
//	  use_1password: true      # Cross-platform (auto-detected if `op` in PATH)
//	  onepassword_vault: "Aleutian"
//	  timeout: 10s
//	  required:
//	    - ANTHROPIC_API_KEY
//
// # Limitations
//
// Vault support is planned for Phase 6B and is not yet implemented.
type SecretsConfig struct {
	// UseEnv enables reading secrets from environment variables.
	// This is the fallback backend, always checked last.
	// Recommended: true (for CI/CD and Docker compatibility).
	UseEnv bool `yaml:"use_env"`

	// UseKeychain enables reading secrets from macOS Keychain.
	// Ignored on non-macOS platforms.
	// The Keychain is accessed via: security find-generic-password
	// Recommended: true on macOS (auto-detected).
	UseKeychain bool `yaml:"use_keychain,omitempty"`

	// Use1Password enables reading secrets from 1Password CLI.
	// Requires: 1Password CLI (`op`) installed and authenticated.
	// Works on: macOS, Linux, Windows.
	// Access pattern: op read "op://Vault/ItemName/password"
	// Recommended: true for teams using 1Password (auto-detected if `op` in PATH).
	Use1Password bool `yaml:"use_1password,omitempty"`

	// OnePasswordVault is the 1Password vault name for Aleutian secrets.
	// Default: "Aleutian"
	OnePasswordVault string `yaml:"onepassword_vault,omitempty"`

	// UseLibsecret enables reading secrets from Linux Secret Service.
	// Works with GNOME Keyring, KDE Wallet, and other providers.
	// Requires: libsecret installed, secret-tool CLI available.
	// Access pattern: secret-tool lookup service aleutian key SECRET_NAME
	// Recommended: true on Linux desktops (auto-detected if `secret-tool` in PATH).
	UseLibsecret bool `yaml:"use_libsecret,omitempty"`

	// VaultAddress is the HashiCorp Vault server address.
	// If set, enables Vault backend (highest priority).
	// Example: "https://vault.example.com:8200"
	// NOTE: Vault support is planned for Phase 6B.
	VaultAddress string `yaml:"vault_address,omitempty"`

	// VaultPath is the path prefix for secrets in Vault.
	// Default: "secret/data/aleutian"
	VaultPath string `yaml:"vault_path,omitempty"`

	// Timeout is the maximum time to wait for CLI backends.
	// Default: 10 seconds (allows time for biometric prompts).
	// Set to 0 to use the default timeout.
	Timeout time.Duration `yaml:"timeout,omitempty"`

	// Required lists secrets that must be present for startup.
	// If any are missing, initialization fails with clear error.
	Required []string `yaml:"required,omitempty"`

	// Redact lists additional secret names to redact from logs.
	// Well-known secrets (ANTHROPIC_API_KEY, etc.) are always redacted.
	Redact []string `yaml:"redact,omitempty"`
}

// GetTimeout returns the configured timeout or the default (10 seconds).
//
// # Description
//
// Returns the timeout duration for CLI backend operations.
// Uses 10 seconds as default to allow time for biometric prompts.
//
// # Outputs
//
//   - time.Duration: The timeout duration
func (c *SecretsConfig) GetTimeout() time.Duration {
	if c == nil || c.Timeout <= 0 {
		return 10 * time.Second
	}
	return c.Timeout
}

// GetOnePasswordVault returns the 1Password vault name or default.
//
// # Description
//
// Returns the vault name for 1Password lookups.
// Uses "Aleutian" as the default vault name.
//
// # Outputs
//
//   - string: The vault name
func (c *SecretsConfig) GetOnePasswordVault() string {
	if c == nil || c.OnePasswordVault == "" {
		return "Aleutian"
	}
	return c.OnePasswordVault
}

// GetVaultPath returns the Vault path prefix or default.
//
// # Description
//
// Returns the path prefix for HashiCorp Vault secret lookups.
// Uses "secret/data/aleutian" as the default path.
//
// # Outputs
//
//   - string: The Vault path prefix
func (c *SecretsConfig) GetVaultPath() string {
	if c == nil || c.VaultPath == "" {
		return "secret/data/aleutian"
	}
	return c.VaultPath
}

// FeatureConfig toggles optional system features.
//
// # Description
//
// Controls which optional services are enabled in the stack.
type FeatureConfig struct {
	// Observability enables metrics, tracing, and logging services.
	Observability bool `yaml:"observability"`

	// RagEngine enables the RAG (Retrieval-Augmented Generation) pipeline.
	RagEngine bool `yaml:"rag_engine"`
}

// -----------------------------------------------------------------------------
// Model Backend Configuration
// -----------------------------------------------------------------------------

// BackendConfig configures the LLM backend.
//
// # Description
//
// Determines which LLM provider is used and its configuration.
// Supports local (Ollama) and cloud (OpenAI, Anthropic) backends.
//
// # Fields
//
//   - Type: Backend type ("ollama", "openai", "anthropic", "remote_tgi")
//   - BaseURL: API endpoint for cloud backends
//   - Ollama: Ollama-specific configuration (when Type is "ollama")
//
// # Example
//
//	backend := BackendConfig{
//	    Type: "ollama",
//	    Ollama: OllamaConfig{
//	        EmbeddingModel: "nomic-embed-text-v2-moe",
//	        DiskLimitGB:    50,
//	    },
//	}
type BackendConfig struct {
	// Type specifies the backend: "ollama", "openai", "anthropic", "remote_tgi".
	Type string `yaml:"type"`

	// BaseURL is the API endpoint for cloud backends.
	BaseURL string `yaml:"base_url,omitempty"`

	// Ollama contains Ollama-specific settings.
	// Only used when Type is "ollama".
	Ollama OllamaConfig `yaml:"ollama,omitempty"`
}

// OllamaConfig configures the Ollama model backend.
//
// # Description
//
// Contains settings specific to running Ollama locally, including
// which models to use for embeddings and LLM, and resource limits.
//
// # Fields
//
//   - EmbeddingModel: Model for document embeddings (default: nomic-embed-text-v2-moe)
//   - LLMModel: Model for chat/generation (default: determined by profile)
//   - DiskLimitGB: Maximum disk space for models (default: 50)
//   - BaseURL: Ollama API endpoint (default: http://localhost:11434)
//
// # Example
//
//	ollama := OllamaConfig{
//	    EmbeddingModel: "nomic-embed-text-v2-moe",
//	    LLMModel:       "gpt-oss:7b",
//	    DiskLimitGB:    100,
//	}
type OllamaConfig struct {
	// EmbeddingModel is the model used for document embeddings.
	// Default: "nomic-embed-text-v2-moe"
	EmbeddingModel string `yaml:"embedding_model,omitempty"`

	// LLMModel is the model used for chat/generation.
	// If empty, the model is determined by the optimization profile.
	LLMModel string `yaml:"llm_model,omitempty"`

	// DiskLimitGB is the maximum disk space (GB) for storing models.
	// Default: 50
	DiskLimitGB int64 `yaml:"disk_limit_gb,omitempty"`

	// BaseURL is the Ollama API endpoint.
	// Default: "http://localhost:11434" for host access.
	BaseURL string `yaml:"base_url,omitempty"`
}

// GetEmbeddingModel returns the configured embedding model or the default.
//
// # Description
//
// Returns the embedding model from configuration, falling back to
// DefaultEmbeddingModel if not configured.
//
// # Outputs
//
//   - string: The embedding model name
func (c *OllamaConfig) GetEmbeddingModel() string {
	if c.EmbeddingModel == "" {
		return DefaultEmbeddingModel
	}
	return c.EmbeddingModel
}

// GetDiskLimitGB returns the configured disk limit or the default.
//
// # Description
//
// Returns the disk limit from configuration, falling back to
// DefaultDiskLimitGB if not configured or zero.
//
// # Outputs
//
//   - int64: The disk limit in gigabytes
func (c *OllamaConfig) GetDiskLimitGB() int64 {
	if c.DiskLimitGB <= 0 {
		return DefaultDiskLimitGB
	}
	return c.DiskLimitGB
}

// GetBaseURL returns the configured base URL or the default.
//
// # Description
//
// Returns the Ollama API URL from configuration, falling back to
// DefaultOllamaHostURL if not configured.
//
// # Outputs
//
//   - string: The Ollama API base URL
func (c *OllamaConfig) GetBaseURL() string {
	if c.BaseURL == "" {
		return DefaultOllamaHostURL
	}
	return c.BaseURL
}

// -----------------------------------------------------------------------------
// Model Management Configuration
// -----------------------------------------------------------------------------

// ModelManagementConfig contains model storage and verification settings.
//
// # Description
//
// Configuration for controlling model downloads, storage limits,
// verification behavior, and advanced features like version pinning,
// auto-selection, and fallback chains.
//
// # YAML Example
//
//	model_management:
//	  disk_limit_gb: 100
//	  verify_on_start: true
//	  allowed_models:
//	    - nomic-embed-text-v2-moe
//	    - llama3:8b
//	  version_pinning:
//	    enabled: true
//	  fallback_chains:
//	    llm:
//	      primary: "llama3:8b"
//	      fallbacks: ["phi3:mini", "tinyllama"]
type ModelManagementConfig struct {
	// DiskLimitGB is maximum disk space for model storage.
	// Set to 0 for no limit.
	// Default: 50
	DiskLimitGB int64 `yaml:"disk_limit_gb"`

	// AllowedModels restricts which models can be downloaded.
	// Empty slice means all models are allowed.
	// Enterprise feature for governance.
	AllowedModels []string `yaml:"allowed_models,omitempty"`

	// VerifyOnStart controls whether models are checked on stack start.
	// Default: true
	VerifyOnStart bool `yaml:"verify_on_start"`

	// OfflineModeAllowed permits operation without network.
	// When true, missing models generate warnings not errors.
	// Default: true
	OfflineModeAllowed bool `yaml:"offline_mode_allowed"`

	// VersionPinning configures model version locking.
	VersionPinning VersionPinningConfig `yaml:"version_pinning,omitempty"`

	// Integrity configures model integrity verification.
	Integrity IntegrityConfig `yaml:"integrity,omitempty"`

	// Parallel configures parallel download behavior.
	Parallel ParallelDownloadConfig `yaml:"parallel,omitempty"`

	// AutoSelection configures automatic model selection.
	AutoSelection AutoSelectionConfig `yaml:"auto_selection,omitempty"`

	// FallbackChains defines ordered fallback lists for model categories.
	FallbackChains map[string]FallbackChain `yaml:"fallback_chains,omitempty"`

	// SizeEstimation configures download size warnings.
	SizeEstimation SizeEstimationConfig `yaml:"size_estimation,omitempty"`

	// Audit configures compliance audit logging.
	Audit AuditLoggingConfig `yaml:"audit,omitempty"`
}

// VersionPinningConfig configures model version locking.
//
// # Description
//
// Enables locking models to specific SHA-256 digests for reproducible
// deployments and security compliance.
type VersionPinningConfig struct {
	// Enabled toggles version pinning.
	// Default: false
	Enabled bool `yaml:"enabled"`

	// Models maps model categories to pinned versions.
	// Keys: "embedding", "llm", etc.
	Models map[string]PinnedModel `yaml:"models,omitempty"`
}

// PinnedModel represents a model locked to a specific version.
type PinnedModel struct {
	// Name is the model identifier.
	Name string `yaml:"name"`

	// Digest is the required SHA-256 hash.
	// If set, download will fail if digest doesn't match.
	// Format: "sha256:abc123..."
	Digest string `yaml:"digest,omitempty"`

	// AllowUpgrade permits newer versions if pinned version unavailable.
	AllowUpgrade bool `yaml:"allow_upgrade"`
}

// IntegrityConfig configures model integrity verification.
type IntegrityConfig struct {
	// VerifyAfterDownload enables SHA-256 verification after pull.
	// Default: true
	VerifyAfterDownload bool `yaml:"verify_after_download"`

	// VerifyOnStartup enables verification on stack start.
	// Default: true
	VerifyOnStartup bool `yaml:"verify_on_startup"`

	// FailOnMismatch fails startup if verification fails.
	// Default: true
	FailOnMismatch bool `yaml:"fail_on_mismatch"`
}

// ParallelDownloadConfig configures concurrent model downloads.
type ParallelDownloadConfig struct {
	// Enabled toggles parallel downloads.
	// Default: true
	Enabled bool `yaml:"enabled"`

	// MaxConcurrent limits simultaneous downloads.
	// Default: 3
	MaxConcurrent int `yaml:"max_concurrent"`

	// BandwidthLimitMbps limits download speed (0 = unlimited).
	// Default: 0
	BandwidthLimitMbps int `yaml:"bandwidth_limit_mbps"`
}

// AutoSelectionConfig configures automatic model selection based on hardware.
type AutoSelectionConfig struct {
	// Enabled toggles auto-selection.
	// Default: true
	Enabled bool `yaml:"enabled"`

	// PreferQuantized favors quantized models over full precision.
	// Default: true
	PreferQuantized bool `yaml:"prefer_quantized"`

	// MinContextWindow requires minimum context window size.
	// Default: 4096
	MinContextWindow int `yaml:"min_context_window"`

	// ExplicitLLM overrides auto-selection for LLM.
	ExplicitLLM string `yaml:"explicit_llm,omitempty"`

	// ExplicitEmbedding overrides auto-selection for embeddings.
	ExplicitEmbedding string `yaml:"explicit_embedding,omitempty"`
}

// FallbackChain defines an ordered list of models to try.
type FallbackChain struct {
	// Primary is the preferred model.
	Primary string `yaml:"primary"`

	// Fallbacks are tried in order if primary fails.
	Fallbacks []string `yaml:"fallbacks,omitempty"`
}

// Models returns all models in the chain (primary + fallbacks).
func (c *FallbackChain) Models() []string {
	if c == nil || c.Primary == "" {
		return nil
	}
	result := make([]string, 0, 1+len(c.Fallbacks))
	result = append(result, c.Primary)
	result = append(result, c.Fallbacks...)
	return result
}

// SizeEstimationConfig configures download size warnings.
type SizeEstimationConfig struct {
	// Enabled toggles size estimation.
	// Default: true
	Enabled bool `yaml:"enabled"`

	// WarnThresholdGB warns before large downloads.
	// Default: 10
	WarnThresholdGB int `yaml:"warn_threshold_gb"`

	// RequireConfirmationGB requires user confirmation for huge downloads.
	// Default: 50
	RequireConfirmationGB int `yaml:"require_confirmation_gb"`
}

// AuditLoggingConfig configures compliance audit logging.
type AuditLoggingConfig struct {
	// Enabled toggles audit logging.
	// Default: true
	Enabled bool `yaml:"enabled"`

	// LogPulls records model download attempts.
	// Default: true
	LogPulls bool `yaml:"log_pulls"`

	// LogVerifications records integrity checks.
	// Default: true
	LogVerifications bool `yaml:"log_verifications"`

	// LogBlocks records blocked model requests.
	// Default: true
	LogBlocks bool `yaml:"log_blocks"`

	// IncludeHostname adds hostname to audit events.
	// Default: true
	IncludeHostname bool `yaml:"include_hostname"`

	// IncludeUser adds username to audit events.
	// Default: true
	IncludeUser bool `yaml:"include_user"`
}

// DefaultModelManagementConfig returns sensible defaults.
//
// # Description
//
// Creates a ModelManagementConfig with production-ready defaults
// that enable verification, auto-selection, and parallel downloads.
//
// # Outputs
//
//   - ModelManagementConfig: Configuration with default values
func DefaultModelManagementConfig() ModelManagementConfig {
	return ModelManagementConfig{
		DiskLimitGB:        50,
		AllowedModels:      nil, // All allowed
		VerifyOnStart:      true,
		OfflineModeAllowed: true,
		Integrity: IntegrityConfig{
			VerifyAfterDownload: true,
			VerifyOnStartup:     true,
			FailOnMismatch:      true,
		},
		Parallel: ParallelDownloadConfig{
			Enabled:       true,
			MaxConcurrent: 3,
		},
		AutoSelection: AutoSelectionConfig{
			Enabled:          true,
			PreferQuantized:  true,
			MinContextWindow: 4096,
		},
		SizeEstimation: SizeEstimationConfig{
			Enabled:               true,
			WarnThresholdGB:       10,
			RequireConfirmationGB: 50,
		},
		Audit: AuditLoggingConfig{
			Enabled:          true,
			LogPulls:         true,
			LogVerifications: true,
			LogBlocks:        true,
			IncludeHostname:  true,
			IncludeUser:      true,
		},
	}
}

// -----------------------------------------------------------------------------
// Forecast Configuration
// -----------------------------------------------------------------------------

// ForecastConfig configures the optional timeseries forecast module.
//
// # Description
//
// Controls the forecast/timeseries functionality, including whether
// to run a local service or connect to external Sapheneia.
type ForecastConfig struct {
	// Enabled toggles the forecast module on/off.
	Enabled bool `yaml:"enabled"`

	// Mode determines deployment: "standalone" or "sapheneia".
	Mode ForecastMode `yaml:"mode"`
}

// -----------------------------------------------------------------------------
// Profile Configuration
// -----------------------------------------------------------------------------

// ProfileConfig defines a custom optimization profile.
//
// # Description
//
// Allows users to define custom profiles that extend or override
// the built-in profiles (low, standard, performance, ultra).
//
// # Fields
//
//   - Name: Unique identifier for the profile
//   - OllamaModel: LLM model to use with this profile
//   - MaxTokens: Context window size
//   - RerankerModel: Reranking model for RAG
//   - MinRAM_MB: Minimum RAM required to use this profile
//
// # Example YAML
//
//	profiles:
//	  - name: my-custom
//	    ollama_model: mixtral:8x7b
//	    max_tokens: 16384
//	    reranker_model: cross-encoder/ms-marco-MiniLM-L-6-v2
//	    min_ram_mb: 48000
type ProfileConfig struct {
	// Name is the unique identifier for this profile.
	Name string `yaml:"name"`

	// OllamaModel is the LLM model to use with this profile.
	OllamaModel string `yaml:"ollama_model"`

	// MaxTokens is the context window size for the LLM.
	MaxTokens int `yaml:"max_tokens"`

	// RerankerModel is the model used for reranking in RAG.
	RerankerModel string `yaml:"reranker_model"`

	// MinRAM_MB is the minimum RAM (in MB) required for this profile.
	MinRAM_MB int64 `yaml:"min_ram_mb"`
}

// -----------------------------------------------------------------------------
// Configuration Metadata (Versioning & Audit)
// -----------------------------------------------------------------------------

// ConfigMeta contains metadata for configuration versioning and auditing.
//
// # Description
//
// Tracks when and how the configuration was created or modified.
// Required for compliance with GDPR, HIPAA, and CCPA audit requirements.
//
// # Fields
//
//   - Version: Schema version for migration support
//   - CreatedAt: Unix millisecond timestamp when config was first created
//   - ModifiedAt: Unix millisecond timestamp when config was last modified
//   - ModifiedBy: Identifier of who/what modified the config
//
// # Timestamp Format
//
// All timestamps are stored as Unix milliseconds (int64) for precision
// and easy comparison. Use time.UnixMilli() to convert.
type ConfigMeta struct {
	// Version is the configuration schema version.
	// Used for migration when schema changes.
	Version string `yaml:"version"`

	// CreatedAt is the Unix millisecond timestamp when config was created.
	CreatedAt int64 `yaml:"created_at"`

	// ModifiedAt is the Unix millisecond timestamp when config was last modified.
	ModifiedAt int64 `yaml:"modified_at"`

	// ModifiedBy identifies who or what modified the config.
	// Examples: "user", "aleutian-cli", "migration-v2"
	ModifiedBy string `yaml:"modified_by"`
}

// CreatedAtTime returns the CreatedAt timestamp as a time.Time.
//
// # Description
//
// Converts the Unix millisecond timestamp to a Go time.Time value.
//
// # Outputs
//
//   - time.Time: The creation time
func (m *ConfigMeta) CreatedAtTime() time.Time {
	return time.UnixMilli(m.CreatedAt)
}

// ModifiedAtTime returns the ModifiedAt timestamp as a time.Time.
//
// # Description
//
// Converts the Unix millisecond timestamp to a Go time.Time value.
//
// # Outputs
//
//   - time.Time: The modification time
func (m *ConfigMeta) ModifiedAtTime() time.Time {
	return time.UnixMilli(m.ModifiedAt)
}

// CurrentConfigVersion is the current configuration schema version.
const CurrentConfigVersion = "1.0.0"

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

// findExternalDrives automatically discovers mounted external drives on macOS.
//
// # Description
//
// Scans /Volumes for mounted external drives, filtering out system volumes
// like "Macintosh HD" and hidden directories.
//
// # Outputs
//
//   - []string: List of external drive paths, or nil on non-macOS or error
//
// # Limitations
//
// Only works on macOS. Returns nil on other platforms.
func findExternalDrives() []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	var externalDrives []string
	volumesDir := "/Volumes"
	entries, err := os.ReadDir(volumesDir)
	if err != nil {
		return nil
	}
	cmd := exec.Command("mount")
	output, err := cmd.Output()
	mountOutput := string(output)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "Macintosh HD" || strings.HasPrefix(name, ".") || name == "Recovery" {
			continue
		}

		fullPath := filepath.Join(volumesDir, name)
		if err == nil && strings.Contains(mountOutput, fullPath) {
			externalDrives = append(externalDrives, fullPath)
		}
	}
	return externalDrives
}

// buildDefaultDrives determines default mount paths based on the host OS.
//
// # Description
//
// Creates a list of default paths to mount into the Podman VM.
// Includes the user's home directory and platform-specific mount points.
//
// # Outputs
//
//   - []string: List of paths to mount
func buildDefaultDrives() []string {
	var defaultDrives []string

	// Always mount the user's home directory
	home, err := os.UserHomeDir()
	if err == nil {
		defaultDrives = append(defaultDrives, home)
	}

	if runtime.GOOS == "darwin" {
		if _, err := os.Stat("/Volumes"); err == nil {
			defaultDrives = append(defaultDrives, "/Volumes")
		}
		extDrives := findExternalDrives()
		defaultDrives = append(defaultDrives, extDrives...)
	} else if runtime.GOOS == "linux" {
		if _, err := os.Stat("/mnt"); err == nil {
			defaultDrives = append(defaultDrives, "/mnt")
		}
		if _, err := os.Stat("/media"); err == nil {
			defaultDrives = append(defaultDrives, "/media")
		}
	}

	return defaultDrives
}

// newConfigMeta creates a new ConfigMeta with current timestamp.
//
// # Description
//
// Initializes metadata for a new configuration file with the
// current schema version and creation timestamp.
//
// # Outputs
//
//   - ConfigMeta: Initialized metadata
func newConfigMeta() ConfigMeta {
	now := time.Now().UnixMilli()
	return ConfigMeta{
		Version:    CurrentConfigVersion,
		CreatedAt:  now,
		ModifiedAt: now,
		ModifiedBy: "aleutian-cli",
	}
}

// DefaultConfig returns the default Aleutian configuration.
//
// # Description
//
// Creates a new AleutianConfig with sensible defaults for all settings.
// This is used when no configuration file exists on first run.
//
// # Outputs
//
//   - AleutianConfig: Configuration with default values
//
// # Default Values
//
//   - Machine: 6 CPUs, 20GB RAM, auto-detected drives
//   - Backend: Ollama with default embedding model
//   - Features: Observability and RAG enabled
//   - Forecast: Standalone mode enabled
func DefaultConfig() AleutianConfig {
	return AleutianConfig{
		Meta: newConfigMeta(),
		Machine: MachineConfig{
			Id:           "podman-machine-default",
			CPUCount:     6,
			MemoryAmount: 20480,
			Drives:       buildDefaultDrives(),
		},
		Extensions: []string{},
		Secrets:    SecretsConfig{UseEnv: false},
		Features: FeatureConfig{
			Observability: true,
			RagEngine:     true,
		},
		ModelBackend: BackendConfig{
			Type:    "ollama",
			BaseURL: DefaultOllamaContainerURL,
			Ollama: OllamaConfig{
				EmbeddingModel: DefaultEmbeddingModel,
				DiskLimitGB:    DefaultDiskLimitGB,
				BaseURL:        DefaultOllamaHostURL,
			},
		},
		Forecast: ForecastConfig{
			Enabled: true,
			Mode:    ForecastModeStandalone,
		},
		ModelManagement: DefaultModelManagementConfig(),
		Profiles:        []ProfileConfig{},
	}
}
