// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package models

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// ModelInfoProvider Interface
// =============================================================================

// ModelInfoProvider retrieves model metadata before download.
//
// # Description
//
// This interface abstracts model metadata retrieval, enabling size estimation,
// version pinning verification, and integrity checking. Used to query Ollama
// for model information without triggering a download.
//
// # Security
//
//   - Model names must be validated before queries
//   - Digest information is security-sensitive (version pinning)
//   - All network requests respect context cancellation
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
type ModelInfoProvider interface {
	// GetModelInfo retrieves metadata for a model from the registry.
	//
	// # Description
	//
	// Queries the model registry for size, digest, and other metadata.
	// Does NOT download the model, only retrieves manifest information.
	// For remote models, this may require a network request.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation/timeout
	//   - model: Model name (e.g., "llama3:8b", "nomic-embed-text-v2-moe")
	//
	// # Outputs
	//
	//   - *ModelInfo: Model metadata (nil if model not found)
	//   - error: Network errors, invalid model name, etc.
	//
	// # Examples
	//
	//   info, err := provider.GetModelInfo(ctx, "llama3:8b")
	//   if err != nil {
	//       return fmt.Errorf("failed to get model info: %w", err)
	//   }
	//   fmt.Printf("Model size: %s\n", formatBytes(info.Size))
	//
	// # Limitations
	//
	//   - Requires network connectivity for remote models
	//   - Model must exist in registry
	//   - Size may differ from actual download size (shared layers)
	//
	// # Assumptions
	//
	//   - Model name has been validated
	//   - Ollama server is reachable
	GetModelInfo(ctx context.Context, model string) (*ModelInfo, error)

	// GetLocalModelInfo retrieves metadata for a locally installed model.
	//
	// # Description
	//
	// Gets information about a model already present on disk.
	// Used for integrity verification and version checking.
	// Does not require network connectivity.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - model: Model name
	//
	// # Outputs
	//
	//   - *ModelInfo: Local model metadata (nil if not installed)
	//   - error: Filesystem errors, corrupt model, etc.
	//
	// # Examples
	//
	//   info, err := provider.GetLocalModelInfo(ctx, "llama3:8b")
	//   if errors.Is(err, ErrModelNotFound) {
	//       fmt.Println("Model not installed locally")
	//   }
	//
	// # Limitations
	//
	//   - Only works for locally installed models
	//   - Returns ErrModelNotFound if model not present
	//
	// # Assumptions
	//
	//   - Ollama is running and accessible
	GetLocalModelInfo(ctx context.Context, model string) (*ModelInfo, error)

	// GetMultipleModelInfo retrieves metadata for multiple models.
	//
	// # Description
	//
	// Batch operation for getting info on multiple models. More efficient
	// than individual GetModelInfo calls when checking many models.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - models: List of model names
	//
	// # Outputs
	//
	//   - map[string]*ModelInfo: Model name to info (nil entry if not found)
	//   - error: General failures (individual model failures are nil entries)
	//
	// # Examples
	//
	//   infos, err := provider.GetMultipleModelInfo(ctx, []string{"llama3:8b", "phi3"})
	//   for name, info := range infos {
	//       if info == nil {
	//           fmt.Printf("%s: not found\n", name)
	//       }
	//   }
	//
	// # Limitations
	//
	//   - Individual model failures result in nil map entries, not errors
	//
	// # Assumptions
	//
	//   - Model names have been validated
	GetMultipleModelInfo(ctx context.Context, models []string) (map[string]*ModelInfo, error)

	// EstimateTotalSize calculates total download size for a list of models.
	//
	// # Description
	//
	// Sums the sizes of all models in the list, accounting for models
	// that are already installed (their size is not included).
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - models: List of model names to estimate
	//
	// # Outputs
	//
	//   - int64: Total bytes to download
	//   - error: If size estimation fails
	//
	// # Examples
	//
	//   totalSize, err := provider.EstimateTotalSize(ctx, []string{"llama3:8b", "phi3"})
	//   if err != nil {
	//       return err
	//   }
	//   fmt.Printf("Will download approximately %s\n", formatBytes(totalSize))
	//
	// # Limitations
	//
	//   - Estimate may be inaccurate due to shared layers between models
	//   - Uses size from manifest which may differ from actual download
	//
	// # Assumptions
	//
	//   - Models that exist locally are not counted in the total
	EstimateTotalSize(ctx context.Context, models []string) (int64, error)
}

// =============================================================================
// Sentinel Errors
// =============================================================================

// ErrModelNotFound indicates the requested model does not exist.
var ErrModelNotFound = errors.New("model not found")

// ErrModelDigestMismatch indicates the model digest doesn't match expected.
var ErrModelDigestMismatch = errors.New("model digest mismatch")

// ErrModelInfoUnavailable indicates model info could not be retrieved.
var ErrModelInfoUnavailable = errors.New("model info unavailable")

// ErrInvalidModelName indicates the model name is malformed.
var ErrInvalidModelName = errors.New("invalid model name")

// =============================================================================
// ModelInfo Struct
// =============================================================================

// ModelInfo contains metadata about a model.
//
// # Description
//
// Holds all metadata needed for size estimation, version pinning,
// and integrity verification. This type is used by both the provider
// and the selector components.
//
// # Thread Safety
//
// ModelInfo is immutable after creation and safe for concurrent read access.
//
// # Security
//
// The Digest field contains security-critical information for version
// pinning. Ensure digests are validated before trusting them.
type ModelInfo struct {
	// Name is the model identifier (e.g., "llama3:8b")
	// Format: name[:tag] where tag defaults to "latest"
	Name string

	// Size is the total size in bytes on disk
	// This is the uncompressed size, actual download may be smaller
	Size int64

	// Digest is the SHA-256 hash of the model (for pinning/verification)
	// Format: "sha256:abc123..."
	// Empty string indicates digest is unknown
	Digest string

	// ModifiedAt is when the model was last updated in the registry
	ModifiedAt time.Time

	// Quantization is the quantization method (e.g., "Q4_K_M", "Q8_0", "F16")
	// Empty string indicates unknown or full precision
	Quantization string

	// ParameterCount is the number of model parameters (e.g., 8_000_000_000)
	// Zero indicates unknown
	ParameterCount int64

	// ContextLength is the maximum context window size in tokens
	// Zero indicates unknown
	ContextLength int

	// Family is the model family (e.g., "llama", "phi", "mistral")
	// Empty string indicates unknown
	Family string

	// IsLocal indicates if this model is installed locally
	IsLocal bool
}

// =============================================================================
// ModelInfo Methods
// =============================================================================

// SizeGB returns the size in gigabytes as a float.
//
// # Description
//
// Convenience method for displaying size in human-readable format.
// Uses 1 GB = 1,073,741,824 bytes (binary gigabyte).
//
// # Outputs
//
//   - float64: Size in GB
//
// # Examples
//
//	info := &ModelInfo{Size: 4_294_967_296}
//	fmt.Printf("%.1f GB\n", info.SizeGB()) // "4.0 GB"
func (m *ModelInfo) SizeGB() float64 {
	return float64(m.Size) / (1024 * 1024 * 1024)
}

// HasDigest returns true if a digest is available.
//
// # Description
//
// Checks if the digest field is populated. Used to determine
// if integrity verification is possible.
//
// # Outputs
//
//   - bool: True if digest is available
//
// # Examples
//
//	if info.HasDigest() {
//	    // Can verify integrity
//	}
func (m *ModelInfo) HasDigest() bool {
	return m.Digest != ""
}

// MatchesDigest checks if this model's digest matches the expected digest.
//
// # Description
//
// Compares digests in a case-insensitive manner. Both digests
// are normalized to lowercase before comparison.
//
// # Inputs
//
//   - expected: The expected digest string
//
// # Outputs
//
//   - bool: True if digests match
//
// # Examples
//
//	if !info.MatchesDigest(pinned.Digest) {
//	    return ErrModelDigestMismatch
//	}
//
// # Limitations
//
//   - Returns false if either digest is empty
func (m *ModelInfo) MatchesDigest(expected string) bool {
	if m.Digest == "" || expected == "" {
		return false
	}
	return strings.EqualFold(m.Digest, expected)
}

// ParameterSizeString returns a human-readable parameter count.
//
// # Description
//
// Formats the parameter count with appropriate suffix (K, M, B).
// Returns "unknown" if parameter count is zero.
//
// # Outputs
//
//   - string: Human-readable parameter count
//
// # Examples
//
//	info := &ModelInfo{ParameterCount: 8_000_000_000}
//	fmt.Println(info.ParameterSizeString()) // "8B"
func (m *ModelInfo) ParameterSizeString() string {
	if m.ParameterCount == 0 {
		return "unknown"
	}

	count := float64(m.ParameterCount)
	switch {
	case count >= 1_000_000_000:
		return fmt.Sprintf("%.0fB", count/1_000_000_000)
	case count >= 1_000_000:
		return fmt.Sprintf("%.0fM", count/1_000_000)
	case count >= 1_000:
		return fmt.Sprintf("%.0fK", count/1_000)
	default:
		return fmt.Sprintf("%.0f", count)
	}
}

// =============================================================================
// FallbackChain Struct
// =============================================================================

// FallbackChain defines an ordered list of models to try.
//
// # Description
//
// When the primary model fails (download error, OOM, etc.),
// the system tries each fallback in order until one succeeds.
// This provides resilience for model availability.
//
// # Thread Safety
//
// FallbackChain is immutable after creation and safe for concurrent read access.
//
// # Configuration
//
// Fallback chains are configured in YAML:
//
//	fallback_chains:
//	  llm:
//	    primary: "llama3:70b"
//	    fallbacks:
//	      - "llama3:8b"
//	      - "phi3:mini"
type FallbackChain struct {
	// Primary is the preferred model to use
	Primary string

	// Fallbacks are tried in order if primary fails
	Fallbacks []string

	// StopOnFirst stops at first successful model
	// When false, verifies all models are available before proceeding
	StopOnFirst bool
}

// =============================================================================
// FallbackChain Methods
// =============================================================================

// Models returns all models in the chain (primary + fallbacks).
//
// # Description
//
// Returns a slice containing the primary model followed by all fallbacks.
// The returned slice is a new allocation and can be safely modified.
//
// # Outputs
//
//   - []string: All models in priority order
//
// # Examples
//
//	chain := &FallbackChain{Primary: "llama3:8b", Fallbacks: []string{"phi3"}}
//	models := chain.Models() // ["llama3:8b", "phi3"]
func (c *FallbackChain) Models() []string {
	if c == nil {
		return nil
	}
	result := make([]string, 0, 1+len(c.Fallbacks))
	if c.Primary != "" {
		result = append(result, c.Primary)
	}
	result = append(result, c.Fallbacks...)
	return result
}

// Len returns the total number of models in the chain.
//
// # Description
//
// Returns count of primary (if set) plus all fallbacks.
//
// # Outputs
//
//   - int: Total number of models
//
// # Examples
//
//	chain := &FallbackChain{Primary: "llama3:8b", Fallbacks: []string{"phi3", "tinyllama"}}
//	fmt.Println(chain.Len()) // 3
func (c *FallbackChain) Len() int {
	if c == nil {
		return 0
	}
	count := len(c.Fallbacks)
	if c.Primary != "" {
		count++
	}
	return count
}

// IsEmpty returns true if the chain has no models.
//
// # Description
//
// Checks if both primary and fallbacks are empty.
//
// # Outputs
//
//   - bool: True if no models configured
//
// # Examples
//
//	chain := &FallbackChain{}
//	fmt.Println(chain.IsEmpty()) // true
func (c *FallbackChain) IsEmpty() bool {
	if c == nil {
		return true
	}
	return c.Primary == "" && len(c.Fallbacks) == 0
}

// Validate checks if the chain configuration is valid.
//
// # Description
//
// Validates that:
//   - At least one model is configured
//   - All model names are valid format
//   - No duplicate models in the chain
//
// # Outputs
//
//   - error: Validation error, nil if valid
//
// # Examples
//
//	if err := chain.Validate(); err != nil {
//	    return fmt.Errorf("invalid fallback chain: %w", err)
//	}
func (c *FallbackChain) Validate() error {
	if c.IsEmpty() {
		return errors.New("fallback chain must have at least one model")
	}

	seen := make(map[string]bool)
	models := c.Models()

	for _, model := range models {
		if err := ValidateModelName(model); err != nil {
			return fmt.Errorf("invalid model name %q: %w", model, err)
		}
		normalized := strings.ToLower(model)
		if seen[normalized] {
			return fmt.Errorf("duplicate model in chain: %s", model)
		}
		seen[normalized] = true
	}

	return nil
}

// =============================================================================
// PinnedModel Struct
// =============================================================================

// PinnedModel represents a model locked to a specific version.
//
// # Description
//
// Version pinning ensures reproducible deployments by locking
// to a specific model digest. When enabled, downloads fail if
// the registry digest doesn't match the pinned digest.
//
// # Thread Safety
//
// PinnedModel is immutable after creation and safe for concurrent read access.
//
// # Security
//
// Version pinning is a security feature. The digest provides
// cryptographic assurance that the model hasn't changed.
//
// # Configuration
//
// Pinned models are configured in YAML:
//
//	version_pinning:
//	  enabled: true
//	  models:
//	    embedding:
//	      name: "nomic-embed-text-v2-moe"
//	      digest: "sha256:abc123..."
//	      allow_upgrade: false
type PinnedModel struct {
	// Name is the model identifier
	Name string

	// Digest is the required SHA-256 hash
	// If set, download will fail if digest doesn't match
	// Format: "sha256:abc123..."
	Digest string

	// AllowUpgrade permits newer versions if pinned version unavailable
	// When true: warn on mismatch but proceed
	// When false: fail on mismatch
	AllowUpgrade bool
}

// =============================================================================
// PinnedModel Methods
// =============================================================================

// IsPinned returns true if a specific digest is required.
//
// # Description
//
// Checks if version pinning is active for this model.
// Pinning is active when a non-empty digest is configured.
//
// # Outputs
//
//   - bool: True if digest is specified
//
// # Examples
//
//	if pinned.IsPinned() {
//	    // Verify digest before using model
//	}
func (p *PinnedModel) IsPinned() bool {
	if p == nil {
		return false
	}
	return p.Digest != ""
}

// Verify checks if the provided info matches the pinned specification.
//
// # Description
//
// Compares the provided model info against the pinned requirements.
// Returns nil if verification passes, error otherwise.
//
// # Inputs
//
//   - info: Model info to verify against pin
//
// # Outputs
//
//   - error: nil if matches, ErrModelDigestMismatch if not
//
// # Examples
//
//	if err := pinned.Verify(downloadedInfo); err != nil {
//	    if pinned.AllowUpgrade {
//	        log.Warn("Digest mismatch, proceeding anyway")
//	    } else {
//	        return err
//	    }
//	}
//
// # Limitations
//
//   - Only verifies digest, not other fields
func (p *PinnedModel) Verify(info *ModelInfo) error {
	if p == nil || !p.IsPinned() {
		return nil // Not pinned, always passes
	}

	if info == nil {
		return fmt.Errorf("%w: model info is nil", ErrModelDigestMismatch)
	}

	if !info.MatchesDigest(p.Digest) {
		return fmt.Errorf("%w: expected %s, got %s",
			ErrModelDigestMismatch, p.Digest, info.Digest)
	}

	return nil
}

// =============================================================================
// Model Name Validation
// =============================================================================

// modelNamePattern validates Ollama model name format.
// Format: [namespace/]name[:tag]
// Examples: "llama3:8b", "library/llama3:latest", "nomic-embed-text-v2-moe"
var modelNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*(/[a-zA-Z0-9][a-zA-Z0-9_.-]*)?(:[a-zA-Z0-9][a-zA-Z0-9_.-]*)?$`)

// ValidateModelName checks if a model name is valid.
//
// # Description
//
// Validates that the model name follows Ollama naming conventions.
// Model names must:
//   - Start with alphanumeric character
//   - Contain only alphanumeric, dash, underscore, dot
//   - Optionally have namespace prefix (name/model)
//   - Optionally have tag suffix (:tag)
//   - Be at most 256 characters
//
// # Inputs
//
//   - name: Model name to validate
//
// # Outputs
//
//   - error: nil if valid, ErrInvalidModelName with details if not
//
// # Examples
//
//	if err := ValidateModelName("llama3:8b"); err != nil {
//	    return err
//	}
//
// # Security
//
// This validation prevents injection attacks through model names.
// Always validate before using model names in URLs or paths.
func ValidateModelName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name cannot be empty", ErrInvalidModelName)
	}

	if len(name) > 256 {
		return fmt.Errorf("%w: name exceeds 256 characters", ErrInvalidModelName)
	}

	if !modelNamePattern.MatchString(name) {
		return fmt.Errorf("%w: %q does not match pattern [namespace/]name[:tag]", ErrInvalidModelName, name)
	}

	return nil
}

// ParseParameterSize converts parameter size strings to numeric values.
//
// # Description
//
// Parses human-readable parameter sizes like "7B", "13B", "70B" into
// actual counts. Handles various formats from Ollama's API.
//
// # Inputs
//
//   - size: Parameter size string (e.g., "7B", "7.1B", "7000M")
//
// # Outputs
//
//   - int64: Parameter count (0 if parsing fails)
//
// # Examples
//
//	count := ParseParameterSize("7B")   // 7_000_000_000
//	count := ParseParameterSize("70M")  // 70_000_000
//	count := ParseParameterSize("1.5B") // 1_500_000_000
//
// # Limitations
//
//   - Returns 0 for unrecognized formats (does not error)
//   - Only handles K, M, B suffixes
func ParseParameterSize(size string) int64 {
	if size == "" {
		return 0
	}

	size = strings.TrimSpace(strings.ToUpper(size))

	// Extract numeric part and suffix
	var numStr string
	var multiplier int64 = 1

	for i, c := range size {
		if c >= '0' && c <= '9' || c == '.' {
			continue
		}
		numStr = size[:i]
		suffix := size[i:]
		switch strings.ToUpper(suffix) {
		case "B":
			multiplier = 1_000_000_000
		case "M":
			multiplier = 1_000_000
		case "K":
			multiplier = 1_000
		default:
			return 0
		}
		break
	}

	if numStr == "" {
		numStr = strings.TrimRight(size, "BMKbmk")
	}

	// Parse as float to handle "7.1B"
	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0
	}

	return int64(val * float64(multiplier))
}

// =============================================================================
// DefaultModelInfoProvider Struct
// =============================================================================

// OllamaModelLister defines the subset of OllamaModelManager needed for info.
//
// # Description
//
// This interface allows ModelInfoProvider to work with OllamaClient
// without depending on the full OllamaModelManager interface.
// This enables easier testing and looser coupling.
type OllamaModelLister interface {
	// ListModels returns all locally available models
	ListModels(ctx context.Context) ([]OllamaModelInfo, error)

	// GetModelSize returns the size for a model
	GetModelSize(ctx context.Context, modelName string) (int64, error)
}

// OllamaModelInfo represents model info from Ollama.
// This mirrors the fields needed from the main package's OllamaModel.
type OllamaModelInfo struct {
	Name              string
	Size              int64
	Digest            string
	ModifiedAt        time.Time
	Family            string
	ParameterSize     string
	QuantizationLevel string
}

// DefaultModelInfoProvider implements ModelInfoProvider using Ollama.
//
// # Description
//
// Production implementation that retrieves model information from
// a local Ollama server. Uses the /api/tags endpoint for local models.
//
// # Thread Safety
//
// DefaultModelInfoProvider is safe for concurrent use via internal mutex.
//
// # Security
//
//   - All model names are validated before use
//   - Network requests respect context cancellation
//   - No sensitive data is logged
type DefaultModelInfoProvider struct {
	// client provides access to Ollama API
	client OllamaModelLister

	// mu protects cache fields
	mu sync.RWMutex

	// localModels caches local model info
	localModels map[string]*ModelInfo

	// cacheTime is when localModels was last refreshed
	cacheTime time.Time

	// cacheTTL is how long to cache local model info
	cacheTTL time.Duration
}

// =============================================================================
// DefaultModelInfoProvider Constructor
// =============================================================================

// NewDefaultModelInfoProvider creates a ModelInfoProvider using Ollama.
//
// # Description
//
// Creates a provider that queries the local Ollama server for model
// information. Caches results for efficiency.
//
// # Inputs
//
//   - client: OllamaModelLister for API access
//
// # Outputs
//
//   - *DefaultModelInfoProvider: Ready-to-use provider
//
// # Examples
//
//	client := NewOllamaClient("http://localhost:11434")
//	// Wrap to implement OllamaModelLister
//	provider := NewDefaultModelInfoProvider(wrappedClient)
//
// # Assumptions
//
//   - Client is non-nil and configured
func NewDefaultModelInfoProvider(client OllamaModelLister) *DefaultModelInfoProvider {
	return &DefaultModelInfoProvider{
		client:      client,
		localModels: make(map[string]*ModelInfo),
		cacheTTL:    30 * time.Second,
	}
}

// =============================================================================
// DefaultModelInfoProvider Methods
// =============================================================================

// GetModelInfo retrieves metadata for a model.
//
// # Description
//
// First checks if the model is available locally, then returns its info.
// For remote-only models, this implementation returns ErrModelNotFound
// as it only has access to local model information.
//
// # Inputs
//
//   - ctx: Context for cancellation/timeout
//   - model: Model name (e.g., "llama3:8b")
//
// # Outputs
//
//   - *ModelInfo: Model metadata (nil if not found)
//   - error: ErrModelNotFound, ErrInvalidModelName, or other errors
//
// # Examples
//
//	info, err := provider.GetModelInfo(ctx, "llama3:8b")
//	if errors.Is(err, ErrModelNotFound) {
//	    fmt.Println("Model not available")
//	}
//
// # Limitations
//
//   - Only returns info for locally installed models
//   - Does not query remote registry
func (p *DefaultModelInfoProvider) GetModelInfo(ctx context.Context, model string) (*ModelInfo, error) {
	if err := ValidateModelName(model); err != nil {
		return nil, err
	}

	// Try local first
	info, err := p.GetLocalModelInfo(ctx, model)
	if err == nil {
		return info, nil
	}

	// For this implementation, we only have local info
	return nil, ErrModelNotFound
}

// GetLocalModelInfo retrieves metadata for a locally installed model.
//
// # Description
//
// Gets information about a model already present on disk by querying
// the Ollama server's model list.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - model: Model name
//
// # Outputs
//
//   - *ModelInfo: Local model metadata
//   - error: ErrModelNotFound if not installed, other errors
//
// # Examples
//
//	info, err := provider.GetLocalModelInfo(ctx, "llama3:8b")
//	if err != nil {
//	    fmt.Println("Model not installed locally")
//	}
//
// # Limitations
//
//   - Requires Ollama to be running
func (p *DefaultModelInfoProvider) GetLocalModelInfo(ctx context.Context, model string) (*ModelInfo, error) {
	if err := ValidateModelName(model); err != nil {
		return nil, err
	}

	// Check cache
	p.mu.RLock()
	if time.Since(p.cacheTime) < p.cacheTTL {
		// Normalize model name for lookup (case-insensitive)
		normalized := normalizeModelName(model)
		if info, ok := p.localModels[normalized]; ok {
			p.mu.RUnlock()
			return info, nil
		}
	}
	p.mu.RUnlock()

	// Refresh cache
	if err := p.refreshCache(ctx); err != nil {
		return nil, err
	}

	// Check again
	p.mu.RLock()
	defer p.mu.RUnlock()

	normalized := normalizeModelName(model)
	if info, ok := p.localModels[normalized]; ok {
		return info, nil
	}

	return nil, ErrModelNotFound
}

// GetMultipleModelInfo retrieves metadata for multiple models.
//
// # Description
//
// Batch operation for getting info on multiple models. Uses a single
// cache refresh for efficiency.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - models: List of model names
//
// # Outputs
//
//   - map[string]*ModelInfo: Model name to info (nil entry if not found)
//   - error: General failures (individual model failures are nil entries)
//
// # Examples
//
//	infos, err := provider.GetMultipleModelInfo(ctx, []string{"llama3:8b", "phi3"})
//	for name, info := range infos {
//	    if info == nil {
//	        fmt.Printf("%s: not found\n", name)
//	    }
//	}
func (p *DefaultModelInfoProvider) GetMultipleModelInfo(ctx context.Context, models []string) (map[string]*ModelInfo, error) {
	if len(models) == 0 {
		return make(map[string]*ModelInfo), nil
	}

	// Refresh cache once
	if err := p.refreshCache(ctx); err != nil {
		return nil, err
	}

	result := make(map[string]*ModelInfo, len(models))
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, model := range models {
		if err := ValidateModelName(model); err != nil {
			result[model] = nil
			continue
		}
		normalized := normalizeModelName(model)
		result[model] = p.localModels[normalized]
	}

	return result, nil
}

// EstimateTotalSize calculates total download size for models.
//
// # Description
//
// Sums the sizes of all models in the list, excluding models
// that are already installed locally.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - models: List of model names to estimate
//
// # Outputs
//
//   - int64: Total bytes to download
//   - error: If size estimation fails
//
// # Examples
//
//	totalSize, err := provider.EstimateTotalSize(ctx, []string{"llama3:8b", "phi3"})
//	fmt.Printf("Will download approximately %s\n", formatBytes(totalSize))
//
// # Limitations
//
//   - Returns 0 for unknown models (no error, assumes small)
func (p *DefaultModelInfoProvider) EstimateTotalSize(ctx context.Context, models []string) (int64, error) {
	if len(models) == 0 {
		return 0, nil
	}

	infos, err := p.GetMultipleModelInfo(ctx, models)
	if err != nil {
		return 0, err
	}

	var total int64
	for _, model := range models {
		info := infos[model]
		if info != nil && info.IsLocal {
			// Already installed, skip
			continue
		}

		// Get size from client
		size, err := p.client.GetModelSize(ctx, model)
		if err != nil {
			// Unknown size, continue with 0
			continue
		}
		total += size
	}

	return total, nil
}

// refreshCache updates the local model cache from Ollama.
func (p *DefaultModelInfoProvider) refreshCache(ctx context.Context) error {
	models, err := p.client.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("failed to list models: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.localModels = make(map[string]*ModelInfo, len(models))
	for _, m := range models {
		info := &ModelInfo{
			Name:           m.Name,
			Size:           m.Size,
			Digest:         m.Digest,
			ModifiedAt:     m.ModifiedAt,
			Quantization:   m.QuantizationLevel,
			ParameterCount: ParseParameterSize(m.ParameterSize),
			Family:         m.Family,
			IsLocal:        true,
		}
		p.localModels[normalizeModelName(m.Name)] = info
	}
	p.cacheTime = time.Now()

	return nil
}

// normalizeModelName normalizes a model name for comparison.
// Handles case insensitivity and default tag.
func normalizeModelName(name string) string {
	name = strings.ToLower(name)
	// Add :latest if no tag specified
	if !strings.Contains(name, ":") {
		name += ":latest"
	}
	return name
}

// =============================================================================
// MockModelInfoProvider Struct
// =============================================================================

// MockModelInfoProvider implements ModelInfoProvider for testing.
//
// # Description
//
// Test double that allows controlling return values and tracking calls.
// All methods can be overridden via function fields.
//
// # Thread Safety
//
// MockModelInfoProvider is safe for concurrent use via internal mutex.
type MockModelInfoProvider struct {
	// Function overrides (nil uses default behavior)
	GetModelInfoFunc         func(ctx context.Context, model string) (*ModelInfo, error)
	GetLocalModelInfoFunc    func(ctx context.Context, model string) (*ModelInfo, error)
	GetMultipleModelInfoFunc func(ctx context.Context, models []string) (map[string]*ModelInfo, error)
	EstimateTotalSizeFunc    func(ctx context.Context, models []string) (int64, error)

	// mu protects tracking fields
	mu sync.Mutex

	// Call tracking
	GetModelInfoCalls      []string
	GetLocalModelInfoCalls []string
	GetMultipleCalls       [][]string
	EstimateSizeCalls      [][]string

	// Preconfigured return values
	Models     map[string]*ModelInfo
	SizeByName map[string]int64
	DefaultErr error
}

// =============================================================================
// MockModelInfoProvider Constructor
// =============================================================================

// NewMockModelInfoProvider creates a mock for testing.
//
// # Description
//
// Creates a mock with empty maps. Add models using the Models field.
//
// # Outputs
//
//   - *MockModelInfoProvider: Ready-to-use mock
//
// # Examples
//
//	mock := NewMockModelInfoProvider()
//	mock.Models["llama3:8b"] = &ModelInfo{Name: "llama3:8b", Size: 4_000_000_000}
func NewMockModelInfoProvider() *MockModelInfoProvider {
	return &MockModelInfoProvider{
		Models:     make(map[string]*ModelInfo),
		SizeByName: make(map[string]int64),
	}
}

// =============================================================================
// MockModelInfoProvider Methods
// =============================================================================

// GetModelInfo implements ModelInfoProvider.
func (m *MockModelInfoProvider) GetModelInfo(ctx context.Context, model string) (*ModelInfo, error) {
	m.mu.Lock()
	m.GetModelInfoCalls = append(m.GetModelInfoCalls, model)
	m.mu.Unlock()

	if m.GetModelInfoFunc != nil {
		return m.GetModelInfoFunc(ctx, model)
	}

	if m.DefaultErr != nil {
		return nil, m.DefaultErr
	}

	normalized := normalizeModelName(model)
	if info, ok := m.Models[normalized]; ok {
		return info, nil
	}
	return nil, ErrModelNotFound
}

// GetLocalModelInfo implements ModelInfoProvider.
func (m *MockModelInfoProvider) GetLocalModelInfo(ctx context.Context, model string) (*ModelInfo, error) {
	m.mu.Lock()
	m.GetLocalModelInfoCalls = append(m.GetLocalModelInfoCalls, model)
	m.mu.Unlock()

	if m.GetLocalModelInfoFunc != nil {
		return m.GetLocalModelInfoFunc(ctx, model)
	}

	if m.DefaultErr != nil {
		return nil, m.DefaultErr
	}

	normalized := normalizeModelName(model)
	if info, ok := m.Models[normalized]; ok {
		if info.IsLocal {
			return info, nil
		}
	}
	return nil, ErrModelNotFound
}

// GetMultipleModelInfo implements ModelInfoProvider.
func (m *MockModelInfoProvider) GetMultipleModelInfo(ctx context.Context, models []string) (map[string]*ModelInfo, error) {
	m.mu.Lock()
	m.GetMultipleCalls = append(m.GetMultipleCalls, models)
	m.mu.Unlock()

	if m.GetMultipleModelInfoFunc != nil {
		return m.GetMultipleModelInfoFunc(ctx, models)
	}

	if m.DefaultErr != nil {
		return nil, m.DefaultErr
	}

	result := make(map[string]*ModelInfo, len(models))
	for _, model := range models {
		normalized := normalizeModelName(model)
		result[model] = m.Models[normalized]
	}
	return result, nil
}

// EstimateTotalSize implements ModelInfoProvider.
func (m *MockModelInfoProvider) EstimateTotalSize(ctx context.Context, models []string) (int64, error) {
	m.mu.Lock()
	m.EstimateSizeCalls = append(m.EstimateSizeCalls, models)
	m.mu.Unlock()

	if m.EstimateTotalSizeFunc != nil {
		return m.EstimateTotalSizeFunc(ctx, models)
	}

	if m.DefaultErr != nil {
		return 0, m.DefaultErr
	}

	var total int64
	for _, model := range models {
		normalized := normalizeModelName(model)
		if info, ok := m.Models[normalized]; ok && info.IsLocal {
			continue // Already installed
		}
		if size, ok := m.SizeByName[normalized]; ok {
			total += size
		}
	}
	return total, nil
}

// Reset clears all call tracking.
//
// # Description
//
// Clears all recorded calls while preserving configured return values.
// Use between test cases.
//
// # Examples
//
//	mock.Reset()
//	// Now all call slices are empty
func (m *MockModelInfoProvider) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.GetModelInfoCalls = nil
	m.GetLocalModelInfoCalls = nil
	m.GetMultipleCalls = nil
	m.EstimateSizeCalls = nil
}
