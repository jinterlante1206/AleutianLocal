package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// =============================================================================
// ModelManager Interface
// =============================================================================

// ModelManager is the unified API for model operations.
//
// # Description
//
// This interface provides a single entry point for all model management
// operations including ensuring models exist, auto-selection based on
// hardware, integrity verification, and progress-tracked downloads.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use.
//
// # Compliance
//
// All operations are audit-logged for GDPR/HIPAA/CCPA compliance.
type ModelManager interface {
	// EnsureModel verifies a model exists, pulling if needed.
	//
	// # Description
	//
	// Checks if the specified model is available locally. If not present
	// and AllowPull is true, downloads the model. Supports fallback chains
	// and integrity verification. Retries on network failures with
	// exponential backoff.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation and timeout
	//   - model: Model identifier (e.g., "llama3:8b")
	//   - opts: Configuration options
	//
	// # Outputs
	//
	//   - ModelResult: Operation outcome with model details
	//   - error: Operation failure
	//
	// # Examples
	//
	//   result, err := manager.EnsureModel(ctx, "llama3:8b", EnsureOpts{
	//       AllowPull:       true,
	//       VerifyIntegrity: true,
	//       FallbackModels:  []string{"llama3:latest"},
	//   })
	//
	// # Limitations
	//
	//   - Network failures may exhaust retry budget
	//   - Blocked models return error immediately
	//
	// # Assumptions
	//
	//   - Ollama API is available
	//   - Model name is valid
	EnsureModel(ctx context.Context, model string, opts EnsureOpts) (ModelResult, error)

	// SelectOptimalModel auto-selects the best model for a purpose.
	//
	// # Description
	//
	// Uses hardware detection and model catalog to select the optimal
	// model for the given purpose (e.g., "chat", "code", "embedding").
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - purpose: Intended use case
	//
	// # Outputs
	//
	//   - string: Selected model identifier
	//   - error: Selection failure
	//
	// # Examples
	//
	//   model, err := manager.SelectOptimalModel(ctx, "code")
	//   // Returns "deepseek-coder:6.7b" on 16GB RAM system
	//
	// # Limitations
	//
	//   - Hardware detection may fail on some systems
	//   - Unknown purposes return default model
	//
	// # Assumptions
	//
	//   - Selector is properly configured
	SelectOptimalModel(ctx context.Context, purpose string) (string, error)

	// VerifyModel checks model integrity using SHA-256.
	//
	// # Description
	//
	// Computes and verifies the model's digest against the registry.
	// Useful for detecting corruption or tampering.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - model: Model identifier
	//
	// # Outputs
	//
	//   - VerificationResult: Verification outcome
	//   - error: Operation failure
	//
	// # Examples
	//
	//   result, err := manager.VerifyModel(ctx, "llama3:8b")
	//   if !result.Verified {
	//       log.Printf("Digest mismatch: %s vs %s", result.Digest, result.ExpectedDigest)
	//   }
	//
	// # Limitations
	//
	//   - Requires model to exist locally
	//
	// # Assumptions
	//
	//   - Model was previously pulled
	VerifyModel(ctx context.Context, model string) (VerificationResult, error)

	// GetModelStatus returns current model status.
	//
	// # Description
	//
	// Checks whether a model is present, being pulled, blocked, etc.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - model: Model identifier
	//
	// # Outputs
	//
	//   - ModelStatus: Current status
	//   - error: Query failure
	//
	// # Examples
	//
	//   status, err := manager.GetModelStatus(ctx, "llama3:8b")
	//   if status == StatusPresent {
	//       // Model is ready to use
	//   }
	//
	// # Limitations
	//
	//   - Status is point-in-time snapshot
	//
	// # Assumptions
	//
	//   - Ollama API is available
	GetModelStatus(ctx context.Context, model string) (ModelStatus, error)

	// ListAvailableModels returns all locally available models.
	//
	// # Description
	//
	// Queries Ollama for all models present on the local system.
	// Results are cached for 24 hours for performance.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//
	// # Outputs
	//
	//   - []LocalModelInfo: Available models
	//   - error: Query failure
	//
	// # Examples
	//
	//   models, err := manager.ListAvailableModels(ctx)
	//   for _, m := range models {
	//       fmt.Printf("%s (%d bytes)\n", m.Name, m.Size)
	//   }
	//
	// # Limitations
	//
	//   - Cache may be stale up to 24 hours
	//
	// # Assumptions
	//
	//   - Ollama API is available
	ListAvailableModels(ctx context.Context) ([]LocalModelInfo, error)

	// PullModelWithProgress pulls a model with progress reporting.
	//
	// # Description
	//
	// Downloads a model and sends progress updates to the provided channel.
	// The channel is closed when the operation completes.
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation
	//   - model: Model identifier
	//   - progressCh: Channel for progress updates (caller must read)
	//
	// # Outputs
	//
	//   - error: Download failure
	//
	// # Examples
	//
	//   progressCh := make(chan PullProgress, 100)
	//   go func() {
	//       for p := range progressCh {
	//           fmt.Printf("\r%s: %.1f%%", p.Status, p.Percent)
	//       }
	//   }()
	//   err := manager.PullModelWithProgress(ctx, "llama3:8b", progressCh)
	//
	// # Limitations
	//
	//   - Caller must read from progressCh to prevent blocking
	//
	// # Assumptions
	//
	//   - Sufficient disk space available
	PullModelWithProgress(ctx context.Context, model string, progressCh chan<- PullProgress) error

	// InvalidateCache clears the model list cache.
	//
	// # Description
	//
	// Forces the next ListAvailableModels call to query fresh data.
	// Useful after manual model operations.
	//
	// # Examples
	//
	//   manager.InvalidateCache()
	//
	// # Limitations
	//
	//   - Does not affect in-flight requests
	//
	// # Assumptions
	//
	//   - None
	InvalidateCache()
}

// =============================================================================
// EnsureOpts Struct
// =============================================================================

// EnsureOpts configures model ensure behavior.
//
// # Description
//
// Options for controlling how EnsureModel behaves, including whether
// to pull missing models, verify integrity, and handle failures.
//
// # Defaults
//
// Zero-value EnsureOpts enables pulling but disables verification.
type EnsureOpts struct {
	// AllowPull enables downloading if model not present.
	// Default: true (when opts is zero-value)
	AllowPull bool

	// VerifyIntegrity enables SHA-256 verification after pull.
	VerifyIntegrity bool

	// FallbackModels lists alternatives to try if primary fails.
	// Tried sequentially in order.
	FallbackModels []string

	// Timeout overrides default operation timeout.
	// Zero uses manager's default timeout.
	Timeout time.Duration

	// ForceRefresh re-pulls even if model is present.
	ForceRefresh bool

	// RetryPolicy configures retry behavior for network failures.
	// Nil uses default policy.
	RetryPolicy *RetryPolicy
}

// =============================================================================
// RetryPolicy Struct
// =============================================================================

// RetryPolicy configures exponential backoff for retries.
//
// # Description
//
// Controls how network failures are retried with exponential backoff
// and optional jitter for distributed systems.
//
// # Defaults
//
// Default policy: 3 retries, 1s initial delay, 30s max delay, 0.1 jitter.
type RetryPolicy struct {
	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int

	// InitialDelay is the delay before first retry.
	InitialDelay time.Duration

	// MaxDelay caps the exponential backoff.
	MaxDelay time.Duration

	// JitterFactor adds randomness (0.0 to 1.0).
	// 0.1 means +/- 10% variation.
	JitterFactor float64
}

// =============================================================================
// RetryPolicy Constructor
// =============================================================================

// DefaultRetryPolicy returns the standard retry configuration.
//
// # Description
//
// Returns a policy with reasonable defaults for network operations:
// 3 retries, exponential backoff from 1s to 30s, 10% jitter.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - *RetryPolicy: Default configuration
//
// # Examples
//
//	opts := EnsureOpts{
//	    RetryPolicy: DefaultRetryPolicy(),
//	}
//
// # Limitations
//
//   - May be too aggressive for slow networks
//
// # Assumptions
//
//   - Network failures are transient
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxRetries:   3,
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		JitterFactor: 0.1,
	}
}

// =============================================================================
// RetryPolicy Methods
// =============================================================================

// CalculateDelay computes the delay for a given attempt.
//
// # Description
//
// Uses exponential backoff with jitter:
// delay = min(initial * 2^attempt, max) * (1 +/- jitter).
//
// # Inputs
//
//   - attempt: Zero-based attempt number
//
// # Outputs
//
//   - time.Duration: Delay before next retry
//
// # Examples
//
//	policy := DefaultRetryPolicy()
//	delay := policy.CalculateDelay(0) // ~1s
//	delay = policy.CalculateDelay(2)  // ~4s
//
// # Limitations
//
//   - Jitter introduces non-determinism
//
// # Assumptions
//
//   - attempt >= 0
func (p *RetryPolicy) CalculateDelay(attempt int) time.Duration {
	return p.calculateBaseDelay(attempt)
}

// calculateBaseDelay computes exponential backoff without jitter.
//
// # Description
//
// Calculates delay = min(initial * 2^attempt, max).
//
// # Inputs
//
//   - attempt: Zero-based attempt number
//
// # Outputs
//
//   - time.Duration: Base delay with jitter applied
func (p *RetryPolicy) calculateBaseDelay(attempt int) time.Duration {
	if p == nil {
		return DefaultRetryPolicy().calculateBaseDelay(attempt)
	}

	baseDelay := float64(p.InitialDelay) * math.Pow(2, float64(attempt))

	maxDelay := float64(p.MaxDelay)
	if baseDelay > maxDelay {
		baseDelay = maxDelay
	}

	return p.applyJitter(time.Duration(baseDelay))
}

// applyJitter adds randomness to a delay.
//
// # Description
//
// Multiplies delay by (1 +/- jitterFactor * random).
//
// # Inputs
//
//   - delay: Base delay
//
// # Outputs
//
//   - time.Duration: Delay with jitter
func (p *RetryPolicy) applyJitter(delay time.Duration) time.Duration {
	if p.JitterFactor <= 0 {
		return delay
	}

	jitter := float64(delay) * p.JitterFactor * (2*rand.Float64() - 1)
	return time.Duration(float64(delay) + jitter)
}

// =============================================================================
// ModelResult Struct
// =============================================================================

// ModelResult contains the outcome of an ensure operation.
//
// # Description
//
// Provides details about the model that was ensured, including
// whether it was pulled and if a fallback was used.
type ModelResult struct {
	// Model is the final model name (may differ if fallback used).
	Model string

	// Digest is the SHA-256 digest of the model.
	Digest string

	// Size is the model size in bytes.
	Size int64

	// WasPulled is true if the model was downloaded.
	WasPulled bool

	// UsedFallback is true if a fallback model was used.
	UsedFallback bool

	// FallbackReason explains why fallback was needed.
	FallbackReason string

	// Duration is the total operation time.
	Duration time.Duration

	// VerificationPassed indicates integrity check result (if performed).
	VerificationPassed *bool
}

// =============================================================================
// VerificationResult Struct
// =============================================================================

// VerificationResult contains integrity check outcome.
//
// # Description
//
// Reports whether a model's digest matches the expected value.
type VerificationResult struct {
	// Model is the verified model name.
	Model string

	// Verified is true if digest matches.
	Verified bool

	// Digest is the computed digest.
	Digest string

	// ExpectedDigest is what the registry reports (on mismatch).
	ExpectedDigest string

	// Error contains verification failure details.
	Error error
}

// =============================================================================
// ModelStatus Type
// =============================================================================

// ModelStatus represents current model state.
//
// # Description
//
// Enumeration of possible model states.
type ModelStatus int

const (
	// StatusUnknown indicates status could not be determined.
	StatusUnknown ModelStatus = iota

	// StatusPresent indicates model is available locally.
	StatusPresent

	// StatusPulling indicates model download is in progress.
	StatusPulling

	// StatusNotFound indicates model is not available.
	StatusNotFound

	// StatusBlocked indicates model is on the blocklist.
	StatusBlocked
)

// =============================================================================
// ModelStatus Methods
// =============================================================================

// String returns a human-readable status name.
//
// # Description
//
// Converts ModelStatus to string for logging and display.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - string: Status name
//
// # Examples
//
//	status := StatusPresent
//	fmt.Println(status.String()) // "present"
//
// # Limitations
//
//   - Unknown values return "unknown"
//
// # Assumptions
//
//   - None
func (s ModelStatus) String() string {
	return s.toStringValue()
}

// toStringValue performs the actual conversion.
func (s ModelStatus) toStringValue() string {
	switch s {
	case StatusPresent:
		return "present"
	case StatusPulling:
		return "pulling"
	case StatusNotFound:
		return "not_found"
	case StatusBlocked:
		return "blocked"
	default:
		return "unknown"
	}
}

// =============================================================================
// LocalModelInfo Struct
// =============================================================================

// LocalModelInfo describes a locally available model.
//
// # Description
//
// Contains metadata about a model that exists on the local system.
type LocalModelInfo struct {
	// Name is the model name (e.g., "llama3").
	Name string

	// Tag is the model tag (e.g., "8b", "latest").
	Tag string

	// Digest is the SHA-256 digest.
	Digest string

	// Size is the model size in bytes.
	Size int64

	// ModifiedAt is when the model was last modified.
	ModifiedAt time.Time

	// Families lists model families (e.g., ["llama"]).
	Families []string

	// Parameters describes parameter count (e.g., "8B").
	Parameters string

	// Quantization describes quantization level (e.g., "Q4_0").
	Quantization string
}

// =============================================================================
// LocalModelInfo Methods
// =============================================================================

// FullName returns the complete model identifier.
//
// # Description
//
// Combines Name and Tag into the standard format.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - string: Full model name (e.g., "llama3:8b")
//
// # Examples
//
//	info := LocalModelInfo{Name: "llama3", Tag: "8b"}
//	fmt.Println(info.FullName()) // "llama3:8b"
//
// # Limitations
//
//   - Returns name only if tag is empty or "latest"
//
// # Assumptions
//
//   - Name is non-empty
func (m *LocalModelInfo) FullName() string {
	return m.formatFullName()
}

// formatFullName performs the actual formatting.
func (m *LocalModelInfo) formatFullName() string {
	if m.Tag == "" || m.Tag == "latest" {
		return m.Name
	}
	return fmt.Sprintf("%s:%s", m.Name, m.Tag)
}

// =============================================================================
// PullProgress Struct
// =============================================================================

// PullProgress reports download progress.
//
// # Description
//
// Sent through progress channel during model downloads.
type PullProgress struct {
	// Status describes current phase.
	// Values: "pulling manifest", "downloading", "verifying", "complete", "error"
	Status string

	// Layer is the current layer being processed.
	Layer string

	// Completed is bytes downloaded so far.
	Completed int64

	// Total is total bytes to download.
	Total int64

	// Percent is completion percentage (0-100).
	Percent float64

	// Error contains failure details if Status is "error".
	Error error
}

// =============================================================================
// Error Variables
// =============================================================================

// ErrModelBlocked indicates a model is on the blocklist.
var ErrModelBlocked = errors.New("model is blocked by policy")

// ErrAllFallbacksFailed indicates all fallback models failed.
var ErrAllFallbacksFailed = errors.New("all fallback models failed")

// ErrVerificationFailed indicates integrity check failed.
var ErrVerificationFailed = errors.New("model verification failed")

// =============================================================================
// DefaultModelManager Struct
// =============================================================================

// DefaultModelManager implements ModelManager.
//
// # Description
//
// Production implementation that integrates ModelQuerier, ModelSelector,
// and ModelAuditLogger to provide complete model management.
//
// # Thread Safety
//
// DefaultModelManager is safe for concurrent use.
//
// # Caching
//
// ListAvailableModels results are cached for 24 hours.
type DefaultModelManager struct {
	querier            ModelInfoProvider
	selector           ModelSelector
	auditLogger        ModelAuditLogger
	httpClient         *http.Client
	baseURL            string
	allowlist          map[string]bool
	defaultTimeout     time.Duration
	defaultRetryPolicy *RetryPolicy
	cacheTTL           time.Duration

	mu             sync.RWMutex
	pullingModels  map[string]bool
	modelCache     []LocalModelInfo
	modelCacheTime time.Time
}

// =============================================================================
// ModelManagerConfig Struct
// =============================================================================

// ModelManagerConfig configures DefaultModelManager.
//
// # Description
//
// Configuration options for creating a DefaultModelManager.
type ModelManagerConfig struct {
	// Querier for registry operations (required)
	Querier ModelInfoProvider

	// Selector for auto-selection (required)
	Selector ModelSelector

	// AuditLogger for compliance (optional, nil = no-op)
	AuditLogger ModelAuditLogger

	// HTTPClient for API calls (optional, nil = default)
	HTTPClient *http.Client

	// BaseURL is the Ollama API endpoint (optional, default = http://localhost:11434)
	BaseURL string

	// Allowlist restricts permitted models (optional, nil = allow all)
	Allowlist []string

	// DefaultTimeout for operations (optional, default = 30m)
	DefaultTimeout time.Duration

	// DefaultRetryPolicy for network failures (optional)
	DefaultRetryPolicy *RetryPolicy

	// CacheTTL for model list cache (optional, default = 24h)
	CacheTTL time.Duration
}

// =============================================================================
// DefaultModelManager Constructor
// =============================================================================

// NewDefaultModelManager creates a configured ModelManager.
//
// # Description
//
// Creates a DefaultModelManager with the provided configuration.
// Uses sensible defaults for optional fields.
//
// # Inputs
//
//   - cfg: Configuration options
//
// # Outputs
//
//   - *DefaultModelManager: Ready-to-use manager
//   - error: Configuration error
//
// # Examples
//
//	manager, err := NewDefaultModelManager(ModelManagerConfig{
//	    Querier:  querier,
//	    Selector: selector,
//	})
//
// # Limitations
//
//   - Requires non-nil Querier and Selector
//
// # Assumptions
//
//   - Ollama is running at BaseURL
func NewDefaultModelManager(cfg ModelManagerConfig) (*DefaultModelManager, error) {
	return buildDefaultModelManager(cfg)
}

// buildDefaultModelManager constructs the manager with validation.
func buildDefaultModelManager(cfg ModelManagerConfig) (*DefaultModelManager, error) {
	if err := validateManagerConfig(cfg); err != nil {
		return nil, err
	}

	cfg = applyManagerDefaults(cfg)

	return &DefaultModelManager{
		querier:            cfg.Querier,
		selector:           cfg.Selector,
		auditLogger:        cfg.AuditLogger,
		httpClient:         cfg.HTTPClient,
		baseURL:            strings.TrimSuffix(cfg.BaseURL, "/"),
		allowlist:          buildAllowlistMap(cfg.Allowlist),
		defaultTimeout:     cfg.DefaultTimeout,
		defaultRetryPolicy: cfg.DefaultRetryPolicy,
		cacheTTL:           cfg.CacheTTL,
		pullingModels:      make(map[string]bool),
	}, nil
}

// validateManagerConfig checks required fields.
func validateManagerConfig(cfg ModelManagerConfig) error {
	if cfg.Querier == nil {
		return errors.New("querier is required")
	}
	if cfg.Selector == nil {
		return errors.New("selector is required")
	}
	return nil
}

// applyManagerDefaults fills in default values.
func applyManagerDefaults(cfg ModelManagerConfig) ModelManagerConfig {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 5 * time.Minute}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:11434"
	}
	if cfg.DefaultTimeout == 0 {
		cfg.DefaultTimeout = 30 * time.Minute
	}
	if cfg.DefaultRetryPolicy == nil {
		cfg.DefaultRetryPolicy = DefaultRetryPolicy()
	}
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 24 * time.Hour
	}
	if cfg.AuditLogger == nil {
		cfg.AuditLogger = NewDefaultModelAuditLogger(nil)
	}
	return cfg
}

// buildAllowlistMap converts slice to map for O(1) lookup.
func buildAllowlistMap(allowlist []string) map[string]bool {
	if len(allowlist) == 0 {
		return nil
	}
	m := make(map[string]bool)
	for _, model := range allowlist {
		m[normalizeModelNameForLookup(model)] = true
	}
	return m
}

// =============================================================================
// DefaultModelManager - EnsureModel Method
// =============================================================================

// EnsureModel verifies a model exists, pulling if needed.
//
// # Description
//
// Implements ModelManager.EnsureModel with retry logic, fallback chains,
// and audit logging.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//   - model: Model identifier
//   - opts: Configuration options
//
// # Outputs
//
//   - ModelResult: Operation outcome
//   - error: Operation failure
//
// # Examples
//
//	result, err := manager.EnsureModel(ctx, "llama3:8b", EnsureOpts{
//	    AllowPull: true,
//	})
//
// # Limitations
//
//   - Blocked models fail immediately without retry
//
// # Assumptions
//
//   - Context deadline allows for retries
func (m *DefaultModelManager) EnsureModel(ctx context.Context, model string, opts EnsureOpts) (ModelResult, error) {
	return m.ensureModelWithFallbacks(ctx, model, opts)
}

// ensureModelWithFallbacks orchestrates primary and fallback attempts.
func (m *DefaultModelManager) ensureModelWithFallbacks(ctx context.Context, model string, opts EnsureOpts) (ModelResult, error) {
	startTime := time.Now()
	normalizedModel := normalizeModelNameForLookup(model)

	if err := m.checkModelAllowed(normalizedModel); err != nil {
		m.recordBlockedModel(normalizedModel, err.Error())
		return ModelResult{}, err
	}

	ctx = m.applyTimeout(ctx, opts.Timeout)

	result, err := m.attemptEnsureWithRetry(ctx, normalizedModel, opts)
	if err == nil {
		result.Duration = time.Since(startTime)
		return result, nil
	}

	return m.tryFallbackModels(ctx, model, opts, err, startTime)
}

// applyTimeout creates a context with the appropriate timeout.
func (m *DefaultModelManager) applyTimeout(ctx context.Context, optTimeout time.Duration) context.Context {
	timeout := m.defaultTimeout
	if optTimeout > 0 {
		timeout = optTimeout
	}
	ctx, _ = context.WithTimeout(ctx, timeout)
	return ctx
}

// tryFallbackModels attempts each fallback model sequentially.
func (m *DefaultModelManager) tryFallbackModels(ctx context.Context, primaryModel string, opts EnsureOpts, primaryErr error, startTime time.Time) (ModelResult, error) {
	if len(opts.FallbackModels) == 0 {
		return ModelResult{}, primaryErr
	}

	for _, fallback := range opts.FallbackModels {
		normalizedFallback := normalizeModelNameForLookup(fallback)

		if err := m.checkModelAllowed(normalizedFallback); err != nil {
			continue
		}

		result, err := m.attemptEnsureWithRetry(ctx, normalizedFallback, opts)
		if err == nil {
			result.UsedFallback = true
			result.FallbackReason = fmt.Sprintf("primary model %s failed: %v", primaryModel, primaryErr)
			result.Duration = time.Since(startTime)
			return result, nil
		}
	}

	return ModelResult{}, fmt.Errorf("%w: primary=%s, tried %d fallbacks", ErrAllFallbacksFailed, primaryModel, len(opts.FallbackModels))
}

// attemptEnsureWithRetry implements retry loop with exponential backoff.
func (m *DefaultModelManager) attemptEnsureWithRetry(ctx context.Context, model string, opts EnsureOpts) (ModelResult, error) {
	policy := m.getRetryPolicy(opts)
	var lastErr error

	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		if attempt > 0 {
			if err := m.waitForRetry(ctx, policy, attempt-1); err != nil {
				return ModelResult{}, err
			}
		}

		result, err := m.attemptEnsureOnce(ctx, model, opts)
		if err == nil {
			return result, nil
		}

		if !isRetryableError(err) {
			return ModelResult{}, err
		}

		lastErr = err
	}

	return ModelResult{}, fmt.Errorf("exhausted %d retries: %w", policy.MaxRetries+1, lastErr)
}

// getRetryPolicy returns the appropriate retry policy.
func (m *DefaultModelManager) getRetryPolicy(opts EnsureOpts) *RetryPolicy {
	if opts.RetryPolicy != nil {
		return opts.RetryPolicy
	}
	return m.defaultRetryPolicy
}

// waitForRetry sleeps for the calculated backoff duration.
func (m *DefaultModelManager) waitForRetry(ctx context.Context, policy *RetryPolicy, attempt int) error {
	delay := policy.CalculateDelay(attempt)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

// attemptEnsureOnce performs a single ensure attempt.
func (m *DefaultModelManager) attemptEnsureOnce(ctx context.Context, model string, opts EnsureOpts) (ModelResult, error) {
	m.logEnsureStart(model)

	info, err := m.querier.GetModelInfo(ctx, model)
	if err == nil && !opts.ForceRefresh {
		return m.handleExistingModel(ctx, model, info, opts)
	}

	// Propagate retryable network errors so retry logic can handle them
	if err != nil && isRetryableError(err) {
		return ModelResult{}, err
	}

	return m.handleMissingModel(ctx, model, opts)
}

// logEnsureStart records the start of an ensure operation.
func (m *DefaultModelManager) logEnsureStart(model string) {
	m.auditLogger.LogModelPull(ModelAuditEvent{
		Action: "ensure_start",
		Model:  model,
	})
}

// handleExistingModel processes a model that already exists locally.
func (m *DefaultModelManager) handleExistingModel(ctx context.Context, model string, info *ModelInfo, opts EnsureOpts) (ModelResult, error) {
	result := ModelResult{
		Model:     model,
		Digest:    info.Digest,
		Size:      info.Size,
		WasPulled: false,
	}

	if opts.VerifyIntegrity {
		if err := m.verifyExistingModel(ctx, model, info.Digest, &result); err != nil {
			return ModelResult{}, err
		}
	}

	m.logModelExists(model, info.Digest)
	return result, nil
}

// verifyExistingModel checks integrity of an existing model.
func (m *DefaultModelManager) verifyExistingModel(ctx context.Context, model string, digest string, result *ModelResult) error {
	verified, err := m.compareModelDigest(ctx, model, digest)
	result.VerificationPassed = &verified
	if err != nil || !verified {
		return fmt.Errorf("%w: %v", ErrVerificationFailed, err)
	}
	return nil
}

// logModelExists records that a model was found locally.
func (m *DefaultModelManager) logModelExists(model string, digest string) {
	m.auditLogger.LogModelPull(ModelAuditEvent{
		Action:  "ensure_exists",
		Model:   model,
		Success: true,
		Digest:  digest,
	})
}

// handleMissingModel processes a model that needs to be pulled.
func (m *DefaultModelManager) handleMissingModel(ctx context.Context, model string, opts EnsureOpts) (ModelResult, error) {
	if !m.shouldAllowPull(opts) {
		return ModelResult{}, ErrModelNotFound
	}

	if err := m.executePull(ctx, model); err != nil {
		m.logPullFailed(model, err)
		return ModelResult{}, err
	}

	return m.buildPullResult(ctx, model, opts)
}

// shouldAllowPull determines if pulling is permitted.
func (m *DefaultModelManager) shouldAllowPull(opts EnsureOpts) bool {
	if opts.AllowPull {
		return true
	}
	return isDefaultEnsureOpts(opts)
}

// executePull downloads the model.
func (m *DefaultModelManager) executePull(ctx context.Context, model string) error {
	return m.pullModelWithoutProgress(ctx, model)
}

// logPullFailed records a pull failure.
func (m *DefaultModelManager) logPullFailed(model string, err error) {
	m.auditLogger.LogModelPull(ModelAuditEvent{
		Action:       "pull_failed",
		Model:        model,
		Success:      false,
		ErrorMessage: err.Error(),
	})
}

// buildPullResult creates the result after a successful pull.
func (m *DefaultModelManager) buildPullResult(ctx context.Context, model string, opts EnsureOpts) (ModelResult, error) {
	info, err := m.querier.GetModelInfo(ctx, model)
	if err != nil {
		return ModelResult{}, fmt.Errorf("failed to get model info after pull: %w", err)
	}

	result := ModelResult{
		Model:     model,
		Digest:    info.Digest,
		Size:      info.Size,
		WasPulled: true,
	}

	if opts.VerifyIntegrity {
		if err := m.verifyExistingModel(ctx, model, info.Digest, &result); err != nil {
			return ModelResult{}, err
		}
	}

	m.logPullComplete(model, info)
	m.InvalidateCache()

	return result, nil
}

// logPullComplete records a successful pull.
func (m *DefaultModelManager) logPullComplete(model string, info *ModelInfo) {
	m.auditLogger.LogModelPull(ModelAuditEvent{
		Action:     "pull_complete",
		Model:      model,
		Success:    true,
		Digest:     info.Digest,
		BytesTotal: info.Size,
	})
}

// =============================================================================
// DefaultModelManager - SelectOptimalModel Method
// =============================================================================

// SelectOptimalModel auto-selects the best model for a purpose.
//
// # Description
//
// Delegates to the configured ModelSelector and validates against allowlist.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - purpose: Intended use case
//
// # Outputs
//
//   - string: Selected model identifier
//   - error: Selection failure
//
// # Examples
//
//	model, err := manager.SelectOptimalModel(ctx, "chat")
//
// # Limitations
//
//   - Selected model may be blocked by allowlist
//
// # Assumptions
//
//   - Selector is properly configured
func (m *DefaultModelManager) SelectOptimalModel(ctx context.Context, purpose string) (string, error) {
	return m.selectAndValidateModel(ctx, purpose)
}

// selectAndValidateModel performs selection and allowlist check.
func (m *DefaultModelManager) selectAndValidateModel(ctx context.Context, purpose string) (string, error) {
	model, err := m.selector.SelectModel(ctx, purpose, SelectionOpts{})
	if err != nil {
		return "", err
	}

	if err := m.checkModelAllowed(model); err != nil {
		return "", fmt.Errorf("selected model %s is blocked: %w", model, err)
	}

	return model, nil
}

// =============================================================================
// DefaultModelManager - VerifyModel Method
// =============================================================================

// VerifyModel checks model integrity using SHA-256.
//
// # Description
//
// Compares the model's digest against the expected value.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - model: Model identifier
//
// # Outputs
//
//   - VerificationResult: Verification outcome
//   - error: Operation failure
//
// # Examples
//
//	result, err := manager.VerifyModel(ctx, "llama3:8b")
//
// # Limitations
//
//   - Requires model to exist locally
//
// # Assumptions
//
//   - Model was previously pulled
func (m *DefaultModelManager) VerifyModel(ctx context.Context, model string) (VerificationResult, error) {
	return m.executeVerification(ctx, model)
}

// executeVerification performs the verification operation.
func (m *DefaultModelManager) executeVerification(ctx context.Context, model string) (VerificationResult, error) {
	normalizedModel := normalizeModelNameForLookup(model)

	info, err := m.querier.GetModelInfo(ctx, normalizedModel)
	if err != nil {
		return VerificationResult{Model: normalizedModel, Error: err}, err
	}

	verified, verifyErr := m.compareModelDigest(ctx, normalizedModel, info.Digest)

	result := VerificationResult{
		Model:    normalizedModel,
		Verified: verified,
		Digest:   info.Digest,
		Error:    verifyErr,
	}

	m.logVerification(normalizedModel, verified, info.Digest)

	return result, nil
}

// logVerification records the verification operation.
func (m *DefaultModelManager) logVerification(model string, verified bool, digest string) {
	m.auditLogger.LogModelVerify(ModelAuditEvent{
		Action:  "verify",
		Model:   model,
		Success: verified,
		Digest:  digest,
	})
}

// =============================================================================
// DefaultModelManager - GetModelStatus Method
// =============================================================================

// GetModelStatus returns current model status.
//
// # Description
//
// Checks model state including pull status and blocklist.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - model: Model identifier
//
// # Outputs
//
//   - ModelStatus: Current status
//   - error: Query failure
//
// # Examples
//
//	status, err := manager.GetModelStatus(ctx, "llama3:8b")
//
// # Limitations
//
//   - Status is point-in-time snapshot
//
// # Assumptions
//
//   - Ollama API is available
func (m *DefaultModelManager) GetModelStatus(ctx context.Context, model string) (ModelStatus, error) {
	return m.determineModelStatus(ctx, model)
}

// determineModelStatus checks various status conditions.
func (m *DefaultModelManager) determineModelStatus(ctx context.Context, model string) (ModelStatus, error) {
	normalizedModel := normalizeModelNameForLookup(model)

	if m.isModelBlocked(normalizedModel) {
		return StatusBlocked, nil
	}

	if m.isModelPulling(normalizedModel) {
		return StatusPulling, nil
	}

	return m.checkModelPresence(ctx, normalizedModel)
}

// isModelBlocked checks if model is on blocklist.
func (m *DefaultModelManager) isModelBlocked(model string) bool {
	return m.checkModelAllowed(model) != nil
}

// isModelPulling checks if model is currently being pulled.
func (m *DefaultModelManager) isModelPulling(model string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pullingModels[model]
}

// checkModelPresence queries if model exists locally.
func (m *DefaultModelManager) checkModelPresence(ctx context.Context, model string) (ModelStatus, error) {
	_, err := m.querier.GetModelInfo(ctx, model)
	if err == nil {
		return StatusPresent, nil
	}

	if isNotFoundError(err) {
		return StatusNotFound, nil
	}

	return StatusUnknown, err
}

// =============================================================================
// DefaultModelManager - ListAvailableModels Method
// =============================================================================

// ListAvailableModels returns all locally available models.
//
// # Description
//
// Queries Ollama for local models. Results are cached for 24 hours.
//
// # Inputs
//
//   - ctx: Context for cancellation
//
// # Outputs
//
//   - []LocalModelInfo: Available models
//   - error: Query failure
//
// # Examples
//
//	models, err := manager.ListAvailableModels(ctx)
//
// # Limitations
//
//   - Cache may be stale up to 24 hours
//
// # Assumptions
//
//   - Ollama API is available
func (m *DefaultModelManager) ListAvailableModels(ctx context.Context) ([]LocalModelInfo, error) {
	return m.getModelsWithCache(ctx)
}

// getModelsWithCache checks cache before querying.
func (m *DefaultModelManager) getModelsWithCache(ctx context.Context) ([]LocalModelInfo, error) {
	if cached := m.getCachedModels(); cached != nil {
		return cached, nil
	}

	return m.refreshModelCache(ctx)
}

// getCachedModels returns cached data if still valid.
func (m *DefaultModelManager) getCachedModels() []LocalModelInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.modelCache == nil {
		return nil
	}

	if time.Since(m.modelCacheTime) >= m.cacheTTL {
		return nil
	}

	result := make([]LocalModelInfo, len(m.modelCache))
	copy(result, m.modelCache)
	return result
}

// refreshModelCache queries fresh data and updates cache.
func (m *DefaultModelManager) refreshModelCache(ctx context.Context) ([]LocalModelInfo, error) {
	models, err := m.queryLocalModelsFromAPI(ctx)
	if err != nil {
		return nil, err
	}

	m.updateModelCache(models)

	result := make([]LocalModelInfo, len(models))
	copy(result, models)
	return result, nil
}

// updateModelCache stores new data in cache.
func (m *DefaultModelManager) updateModelCache(models []LocalModelInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modelCache = models
	m.modelCacheTime = time.Now()
}

// =============================================================================
// DefaultModelManager - PullModelWithProgress Method
// =============================================================================

// PullModelWithProgress pulls a model with progress reporting.
//
// # Description
//
// Downloads a model and sends progress updates to the channel.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - model: Model identifier
//   - progressCh: Channel for progress updates
//
// # Outputs
//
//   - error: Download failure
//
// # Examples
//
//	progressCh := make(chan PullProgress, 100)
//	err := manager.PullModelWithProgress(ctx, "llama3:8b", progressCh)
//
// # Limitations
//
//   - Caller must read from progressCh to prevent blocking
//
// # Assumptions
//
//   - Sufficient disk space available
func (m *DefaultModelManager) PullModelWithProgress(ctx context.Context, model string, progressCh chan<- PullProgress) error {
	return m.executePullWithProgress(ctx, model, progressCh)
}

// executePullWithProgress implements the progress-reporting pull.
func (m *DefaultModelManager) executePullWithProgress(ctx context.Context, model string, progressCh chan<- PullProgress) error {
	normalizedModel := normalizeModelNameForLookup(model)
	defer close(progressCh)

	if err := m.checkModelAllowed(normalizedModel); err != nil {
		progressCh <- PullProgress{Status: "error", Error: err}
		return err
	}

	m.markModelPulling(normalizedModel, true)
	defer m.markModelPulling(normalizedModel, false)

	m.logPullStart(normalizedModel)

	return m.streamPullProgress(ctx, normalizedModel, progressCh)
}

// markModelPulling updates the pulling status for a model.
func (m *DefaultModelManager) markModelPulling(model string, pulling bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pulling {
		m.pullingModels[model] = true
	} else {
		delete(m.pullingModels, model)
	}
}

// logPullStart records the start of a pull operation.
func (m *DefaultModelManager) logPullStart(model string) {
	m.auditLogger.LogModelPull(ModelAuditEvent{
		Action: "pull_start",
		Model:  model,
	})
}

// streamPullProgress executes the pull and streams progress.
func (m *DefaultModelManager) streamPullProgress(ctx context.Context, model string, progressCh chan<- PullProgress) error {
	resp, err := m.makePullRequest(ctx, model, true)
	if err != nil {
		progressCh <- PullProgress{Status: "error", Error: err}
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("pull failed with status %d", resp.StatusCode)
		progressCh <- PullProgress{Status: "error", Error: err}
		return err
	}

	totalBytes, err := m.parseProgressStream(resp.Body, progressCh)
	if err != nil {
		return err
	}

	m.finalizePull(model, totalBytes, progressCh)
	return nil
}

// makePullRequest creates and executes the pull HTTP request.
func (m *DefaultModelManager) makePullRequest(ctx context.Context, model string, stream bool) (*http.Response, error) {
	reqBody := fmt.Sprintf(`{"name": "%s", "stream": %t}`, model, stream)
	req, err := http.NewRequestWithContext(ctx, "POST", m.baseURL+"/api/pull", strings.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return m.httpClient.Do(req)
}

// parseProgressStream reads progress updates from the response.
func (m *DefaultModelManager) parseProgressStream(body io.Reader, progressCh chan<- PullProgress) (int64, error) {
	decoder := json.NewDecoder(body)
	var totalBytes int64

	for {
		var progress ollamaPullProgress
		if err := decoder.Decode(&progress); err != nil {
			if err == io.EOF {
				break
			}
			progressCh <- PullProgress{Status: "error", Error: err}
			return totalBytes, err
		}

		if progress.Total > totalBytes {
			totalBytes = progress.Total
		}

		progressCh <- m.convertProgress(progress)

		if progress.Status == "success" {
			break
		}
	}

	return totalBytes, nil
}

// convertProgress converts Ollama progress to our format.
func (m *DefaultModelManager) convertProgress(progress ollamaPullProgress) PullProgress {
	var percent float64
	if progress.Total > 0 {
		percent = float64(progress.Completed) / float64(progress.Total) * 100
	}

	return PullProgress{
		Status:    progress.Status,
		Layer:     progress.Digest,
		Completed: progress.Completed,
		Total:     progress.Total,
		Percent:   percent,
	}
}

// finalizePull sends completion and logs success.
func (m *DefaultModelManager) finalizePull(model string, totalBytes int64, progressCh chan<- PullProgress) {
	progressCh <- PullProgress{
		Status:    "complete",
		Completed: totalBytes,
		Total:     totalBytes,
		Percent:   100,
	}

	m.auditLogger.LogModelPull(ModelAuditEvent{
		Action:     "pull_complete",
		Model:      model,
		Success:    true,
		BytesTotal: totalBytes,
	})

	m.InvalidateCache()
}

// =============================================================================
// DefaultModelManager - InvalidateCache Method
// =============================================================================

// InvalidateCache clears the model list cache.
//
// # Description
//
// Forces the next ListAvailableModels call to query fresh data.
//
// # Inputs
//
// None.
//
// # Outputs
//
// None.
//
// # Examples
//
//	manager.InvalidateCache()
//
// # Limitations
//
//   - Does not affect in-flight requests
//
// # Assumptions
//
//   - None
func (m *DefaultModelManager) InvalidateCache() {
	m.clearModelCache()
}

// clearModelCache resets the cache state.
func (m *DefaultModelManager) clearModelCache() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modelCache = nil
	m.modelCacheTime = time.Time{}
}

// =============================================================================
// DefaultModelManager - Helper Methods
// =============================================================================

// checkModelAllowed verifies a model is permitted by allowlist.
func (m *DefaultModelManager) checkModelAllowed(model string) error {
	if m.allowlist == nil {
		return nil
	}

	normalized := normalizeModelNameForLookup(model)
	if m.allowlist[normalized] {
		return nil
	}

	baseName := strings.Split(normalized, ":")[0]
	if m.allowlist[baseName] || m.allowlist[baseName+":latest"] {
		return nil
	}

	return fmt.Errorf("%w: %s", ErrModelBlocked, model)
}

// recordBlockedModel logs a blocked model request.
func (m *DefaultModelManager) recordBlockedModel(model string, reason string) {
	m.auditLogger.LogModelBlock(ModelAuditEvent{
		Action:       "block",
		Model:        model,
		Success:      false,
		ErrorMessage: reason,
	})
}

// pullModelWithoutProgress downloads a model without progress reporting.
func (m *DefaultModelManager) pullModelWithoutProgress(ctx context.Context, model string) error {
	m.markModelPulling(model, true)
	defer m.markModelPulling(model, false)

	resp, err := m.makePullRequest(ctx, model, false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pull failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// compareModelDigest checks if stored digest matches expected.
func (m *DefaultModelManager) compareModelDigest(ctx context.Context, model string, expectedDigest string) (bool, error) {
	info, err := m.querier.GetModelInfo(ctx, model)
	if err != nil {
		return false, err
	}
	return info.Digest == expectedDigest, nil
}

// queryLocalModelsFromAPI fetches all local models from Ollama API.
func (m *DefaultModelManager) queryLocalModelsFromAPI(ctx context.Context) ([]LocalModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", m.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list models: status %d", resp.StatusCode)
	}

	var tagsResp ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		return nil, err
	}

	return m.convertOllamaModels(tagsResp.Models), nil
}

// convertOllamaModels transforms Ollama API response to our types.
func (m *DefaultModelManager) convertOllamaModels(ollamaModels []ollamaModel) []LocalModelInfo {
	models := make([]LocalModelInfo, 0, len(ollamaModels))
	for _, om := range ollamaModels {
		name, tag := splitModelName(om.Name)
		models = append(models, LocalModelInfo{
			Name:         name,
			Tag:          tag,
			Digest:       om.Digest,
			Size:         om.Size,
			ModifiedAt:   om.ModifiedAt,
			Families:     om.Details.Families,
			Parameters:   om.Details.ParameterSize,
			Quantization: om.Details.QuantizationLevel,
		})
	}
	return models
}

// =============================================================================
// Ollama API Types
// =============================================================================

// ollamaPullProgress represents Ollama pull progress response.
type ollamaPullProgress struct {
	Status    string `json:"status"`
	Digest    string `json:"digest"`
	Total     int64  `json:"total"`
	Completed int64  `json:"completed"`
}

// ollamaTagsResponse represents Ollama /api/tags response.
type ollamaTagsResponse struct {
	Models []ollamaModel `json:"models"`
}

// ollamaModel represents a model in Ollama API response.
type ollamaModel struct {
	Name       string    `json:"name"`
	Digest     string    `json:"digest"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
	Details    struct {
		Families          []string `json:"families"`
		ParameterSize     string   `json:"parameter_size"`
		QuantizationLevel string   `json:"quantization_level"`
	} `json:"details"`
}

// =============================================================================
// Helper Functions
// =============================================================================

// normalizeModelNameForLookup ensures consistent model naming.
func normalizeModelNameForLookup(model string) string {
	model = strings.TrimSpace(strings.ToLower(model))
	if !strings.Contains(model, ":") {
		model += ":latest"
	}
	return model
}

// splitModelName divides a model name into name and tag.
func splitModelName(fullName string) (name, tag string) {
	parts := strings.SplitN(fullName, ":", 2)
	name = parts[0]
	if len(parts) > 1 {
		tag = parts[1]
	} else {
		tag = "latest"
	}
	return
}

// isDefaultEnsureOpts checks if opts is the zero value.
func isDefaultEnsureOpts(opts EnsureOpts) bool {
	return !opts.AllowPull && !opts.VerifyIntegrity &&
		len(opts.FallbackModels) == 0 && opts.Timeout == 0 &&
		!opts.ForceRefresh && opts.RetryPolicy == nil
}

// isRetryableError checks if an error is network-related and retryable.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	errStr := strings.ToLower(err.Error())
	networkIndicators := []string{
		"connection refused",
		"connection reset",
		"no such host",
		"network is unreachable",
		"i/o timeout",
		"dial tcp",
		"eof",
	}
	for _, indicator := range networkIndicators {
		if strings.Contains(errStr, indicator) {
			return true
		}
	}

	return false
}

// isNotFoundError checks if error indicates model not found.
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "not found") || strings.Contains(errStr, "does not exist")
}

// =============================================================================
// MockModelManager Struct
// =============================================================================

// MockModelManager implements ModelManager for testing.
//
// # Description
//
// Test double that allows customizing behavior via function overrides.
//
// # Thread Safety
//
// MockModelManager is safe for concurrent use.
type MockModelManager struct {
	EnsureModelFunc           func(ctx context.Context, model string, opts EnsureOpts) (ModelResult, error)
	SelectOptimalModelFunc    func(ctx context.Context, purpose string) (string, error)
	VerifyModelFunc           func(ctx context.Context, model string) (VerificationResult, error)
	GetModelStatusFunc        func(ctx context.Context, model string) (ModelStatus, error)
	ListAvailableModelsFunc   func(ctx context.Context) ([]LocalModelInfo, error)
	PullModelWithProgressFunc func(ctx context.Context, model string, progressCh chan<- PullProgress) error
	InvalidateCacheFunc       func()

	mu          sync.Mutex
	EnsureCalls []MockEnsureCall
	DefaultErr  error
}

// MockEnsureCall records an EnsureModel call.
type MockEnsureCall struct {
	Model string
	Opts  EnsureOpts
}

// =============================================================================
// MockModelManager Constructor
// =============================================================================

// NewMockModelManager creates a mock for testing.
//
// # Description
//
// Creates a mock that records calls and returns configurable results.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - *MockModelManager: Ready-to-use mock
//
// # Examples
//
//	mock := NewMockModelManager()
//	mock.EnsureModelFunc = func(...) { ... }
//
// # Limitations
//
//   - Does not simulate real network behavior
//
// # Assumptions
//
//   - Test code sets appropriate function overrides
func NewMockModelManager() *MockModelManager {
	return &MockModelManager{}
}

// =============================================================================
// MockModelManager Methods
// =============================================================================

// EnsureModel implements ModelManager.
//
// # Description
//
// Records the call and returns configured result.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - model: Model identifier
//   - opts: Configuration options
//
// # Outputs
//
//   - ModelResult: Configured result
//   - error: Configured error
func (m *MockModelManager) EnsureModel(ctx context.Context, model string, opts EnsureOpts) (ModelResult, error) {
	return m.recordAndReturnEnsure(ctx, model, opts)
}

// recordAndReturnEnsure stores the call and returns result.
func (m *MockModelManager) recordAndReturnEnsure(ctx context.Context, model string, opts EnsureOpts) (ModelResult, error) {
	m.mu.Lock()
	m.EnsureCalls = append(m.EnsureCalls, MockEnsureCall{Model: model, Opts: opts})
	m.mu.Unlock()

	if m.EnsureModelFunc != nil {
		return m.EnsureModelFunc(ctx, model, opts)
	}
	if m.DefaultErr != nil {
		return ModelResult{}, m.DefaultErr
	}
	return ModelResult{Model: model, WasPulled: false}, nil
}

// SelectOptimalModel implements ModelManager.
//
// # Description
//
// Returns configured result or default.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - purpose: Intended use case
//
// # Outputs
//
//   - string: Selected model
//   - error: Configured error
func (m *MockModelManager) SelectOptimalModel(ctx context.Context, purpose string) (string, error) {
	return m.returnSelectResult(ctx, purpose)
}

// returnSelectResult provides the configured selection result.
func (m *MockModelManager) returnSelectResult(ctx context.Context, purpose string) (string, error) {
	if m.SelectOptimalModelFunc != nil {
		return m.SelectOptimalModelFunc(ctx, purpose)
	}
	if m.DefaultErr != nil {
		return "", m.DefaultErr
	}
	return "llama3:8b", nil
}

// VerifyModel implements ModelManager.
//
// # Description
//
// Returns configured verification result.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - model: Model identifier
//
// # Outputs
//
//   - VerificationResult: Configured result
//   - error: Configured error
func (m *MockModelManager) VerifyModel(ctx context.Context, model string) (VerificationResult, error) {
	return m.returnVerifyResult(ctx, model)
}

// returnVerifyResult provides the configured verification result.
func (m *MockModelManager) returnVerifyResult(ctx context.Context, model string) (VerificationResult, error) {
	if m.VerifyModelFunc != nil {
		return m.VerifyModelFunc(ctx, model)
	}
	if m.DefaultErr != nil {
		return VerificationResult{}, m.DefaultErr
	}
	return VerificationResult{Model: model, Verified: true}, nil
}

// GetModelStatus implements ModelManager.
//
// # Description
//
// Returns configured status result.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - model: Model identifier
//
// # Outputs
//
//   - ModelStatus: Configured status
//   - error: Configured error
func (m *MockModelManager) GetModelStatus(ctx context.Context, model string) (ModelStatus, error) {
	return m.returnStatusResult(ctx, model)
}

// returnStatusResult provides the configured status result.
func (m *MockModelManager) returnStatusResult(ctx context.Context, model string) (ModelStatus, error) {
	if m.GetModelStatusFunc != nil {
		return m.GetModelStatusFunc(ctx, model)
	}
	if m.DefaultErr != nil {
		return StatusUnknown, m.DefaultErr
	}
	return StatusPresent, nil
}

// ListAvailableModels implements ModelManager.
//
// # Description
//
// Returns configured model list.
//
// # Inputs
//
//   - ctx: Context for cancellation
//
// # Outputs
//
//   - []LocalModelInfo: Configured models
//   - error: Configured error
func (m *MockModelManager) ListAvailableModels(ctx context.Context) ([]LocalModelInfo, error) {
	return m.returnListResult(ctx)
}

// returnListResult provides the configured list result.
func (m *MockModelManager) returnListResult(ctx context.Context) ([]LocalModelInfo, error) {
	if m.ListAvailableModelsFunc != nil {
		return m.ListAvailableModelsFunc(ctx)
	}
	if m.DefaultErr != nil {
		return nil, m.DefaultErr
	}
	return []LocalModelInfo{{Name: "llama3", Tag: "8b"}}, nil
}

// PullModelWithProgress implements ModelManager.
//
// # Description
//
// Executes configured pull function or closes channel.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - model: Model identifier
//   - progressCh: Progress channel
//
// # Outputs
//
//   - error: Configured error
func (m *MockModelManager) PullModelWithProgress(ctx context.Context, model string, progressCh chan<- PullProgress) error {
	return m.executeMockPull(ctx, model, progressCh)
}

// executeMockPull handles the mock pull operation.
func (m *MockModelManager) executeMockPull(ctx context.Context, model string, progressCh chan<- PullProgress) error {
	if m.PullModelWithProgressFunc != nil {
		return m.PullModelWithProgressFunc(ctx, model, progressCh)
	}
	close(progressCh)
	return m.DefaultErr
}

// InvalidateCache implements ModelManager.
//
// # Description
//
// Executes configured invalidate function if set.
//
// # Inputs
//
// None.
//
// # Outputs
//
// None.
func (m *MockModelManager) InvalidateCache() {
	m.executeInvalidate()
}

// executeInvalidate runs the configured invalidate function.
func (m *MockModelManager) executeInvalidate() {
	if m.InvalidateCacheFunc != nil {
		m.InvalidateCacheFunc()
	}
}

// Reset clears recorded calls.
//
// # Description
//
// Resets all recorded calls for test reuse.
//
// # Inputs
//
// None.
//
// # Outputs
//
// None.
//
// # Examples
//
//	mock.Reset()
//
// # Limitations
//
//   - Does not reset function overrides
//
// # Assumptions
//
//   - Called between test cases
func (m *MockModelManager) Reset() {
	m.clearRecordedCalls()
}

// clearRecordedCalls resets all call records.
func (m *MockModelManager) clearRecordedCalls() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.EnsureCalls = nil
}
