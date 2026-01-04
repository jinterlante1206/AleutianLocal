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
Package main contains ollama_client.go which provides model management operations
for the Ollama local model server.

# Problem Statement

When users run `aleutian stack start`, we need to ensure required models are
available before starting containers. This requires:

 1. Listing models currently available in Ollama
 2. Detecting which models need to be downloaded
 3. Pulling models with progress feedback
 4. Distinguishing between registry models and user-created custom models

Previously, users had to manually run `ollama pull <model>` before using Aleutian,
which created friction and confusion, especially for first-time users.

# Solution

OllamaModelManager provides a clean interface for model management:

	┌─────────────────────────────────────────────────────────────────┐
	│                    aleutian stack start                         │
	├─────────────────────────────────────────────────────────────────┤
	│                                                                 │
	│  1. SystemChecker.IsOllamaInstalled()  ← Verify Ollama ready    │
	│                                                                 │
	│  2. OllamaClient.ListModels()          ← Get available models   │
	│     └─ Cached for performance                                   │
	│                                                                 │
	│  3. OllamaClient.HasModel(embedModel)  ← Check if we need pull  │
	│     OllamaClient.HasModel(llmModel)                             │
	│                                                                 │
	│  4. IF models missing:                                          │
	│     ├─ OllamaClient.GetModelSize()     ← For disk space check   │
	│     └─ OllamaClient.PullModel()        ← Download with progress │
	│                                                                 │
	└─────────────────────────────────────────────────────────────────┘

# Custom Model Detection

Aleutian supports user-created GGUF models that are imported into Ollama.
These models cannot be "pulled" from the registry - they only exist locally.

Detection strategy: Models created via `ollama create` or imported from GGUF
have a `template` field in their metadata (from Modelfile). Registry-only models
like embedding models don't have templates.

	// Detect custom model
	isCustom, _ := client.IsCustomModel(ctx, "my-fine-tuned-llm")
	if isCustom {
	    fmt.Println("This is a locally-created model")
	}

# Progress Callback

Model pulls report progress via callback, using Ollama's native progress:

	err := client.PullModel(ctx, "nomic-embed-text-v2-moe", func(status string, completed, total int64) {
	    if total > 0 {
	        percent := float64(completed) / float64(total) * 100
	        fmt.Printf("\r  %s: %.1f%% (%s/%s)", status, percent,
	            formatBytes(completed), formatBytes(total))
	    } else {
	        fmt.Printf("\r  %s...", status)
	    }
	})

# Model Caching

ListModels results are cached for 30 seconds to avoid redundant API calls.
Use RefreshModelCache to force an update if needed.

# Usage

	client := NewOllamaClient("http://localhost:11434")

	// List all models
	models, err := client.ListModels(ctx)

	// Check if model exists
	exists, _ := client.HasModel(ctx, "nomic-embed-text-v2-moe")

	// Pull missing model with progress
	if !exists {
	    err = client.PullModel(ctx, "nomic-embed-text-v2-moe", progressCallback)
	}

# Configuration

The client respects these environment variables:

  - OLLAMA_HOST: Override default Ollama URL (http://localhost:11434)

# Related Files

  - system_checker.go: Pre-flight system checks
  - cmd_stack.go: Integration point (ensureOllamaModels function)
  - docs/designs/pending/ollama_model_management.md: Full architecture
*/
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// -----------------------------------------------------------------------------
// Error Types
// -----------------------------------------------------------------------------

// ModelErrorType categorizes model operation failures for programmatic handling.
type ModelErrorType int

const (
	// ModelErrorNotFound indicates the model does not exist in the registry.
	ModelErrorNotFound ModelErrorType = iota

	// ModelErrorPullFailed indicates the model download failed.
	ModelErrorPullFailed

	// ModelErrorConnectionFailed indicates Ollama server is not reachable.
	ModelErrorConnectionFailed

	// ModelErrorInvalidResponse indicates Ollama returned unexpected data.
	ModelErrorInvalidResponse

	// ModelErrorContextCancelled indicates the operation was cancelled.
	ModelErrorContextCancelled
)

// String returns the error type as a string for logging.
func (t ModelErrorType) String() string {
	switch t {
	case ModelErrorNotFound:
		return "MODEL_NOT_FOUND"
	case ModelErrorPullFailed:
		return "PULL_FAILED"
	case ModelErrorConnectionFailed:
		return "CONNECTION_FAILED"
	case ModelErrorInvalidResponse:
		return "INVALID_RESPONSE"
	case ModelErrorContextCancelled:
		return "CONTEXT_CANCELLED"
	default:
		return "UNKNOWN"
	}
}

// ModelError provides structured error information for model operations.
type ModelError struct {
	// Type categorizes the error for programmatic handling.
	Type ModelErrorType

	// Model is the name of the model that caused the error.
	Model string

	// Message is a human-readable error description.
	Message string

	// Detail provides technical information for debugging.
	Detail string

	// Remediation suggests how to fix the issue.
	Remediation string
}

// Error implements the error interface.
func (e *ModelError) Error() string {
	return e.Message
}

// FullError returns a detailed error message including remediation.
func (e *ModelError) FullError() string {
	var buf bytes.Buffer
	buf.WriteString(e.Message)
	if e.Model != "" {
		buf.WriteString(fmt.Sprintf(" (model: %s)", e.Model))
	}
	if e.Detail != "" {
		buf.WriteString("\n\nDetails: ")
		buf.WriteString(e.Detail)
	}
	if e.Remediation != "" {
		buf.WriteString("\n\nTo fix:\n")
		buf.WriteString(e.Remediation)
	}
	return buf.String()
}

// -----------------------------------------------------------------------------
// Data Types
// -----------------------------------------------------------------------------

// OllamaModel represents a model available in Ollama.
type OllamaModel struct {
	// Name is the model identifier (e.g., "nomic-embed-text-v2-moe").
	Name string

	// Size is the model file size in bytes.
	Size int64

	// ModifiedAt is when the model was last modified.
	ModifiedAt time.Time

	// IsCustom is true if this is a locally-created model (has template).
	IsCustom bool

	// Digest is the model's content hash.
	Digest string

	// Family is the model family (e.g., "llama", "nomic").
	Family string

	// ParameterSize is the human-readable parameter count (e.g., "7B").
	ParameterSize string

	// QuantizationLevel is the quantization type (e.g., "Q4_K_M").
	QuantizationLevel string
}

// PullProgressCallback is called during download to report progress.
//
// # Description
//
// The callback receives progress updates during model pulls.
// Ollama streams progress as the download proceeds.
//
// # Inputs
//
//   - status: Current operation (e.g., "pulling manifest", "pulling sha256:...")
//   - completed: Bytes downloaded so far
//   - total: Total bytes to download (0 if unknown)
type PullProgressCallback func(status string, completed, total int64)

// -----------------------------------------------------------------------------
// Interface Definition
// -----------------------------------------------------------------------------

// OllamaModelManager defines the contract for managing Ollama models.
// This interface enables testing with mocks and provides a clean API
// for model listing, checking, and pulling operations.
//
// Implementations must be safe for concurrent use.
type OllamaModelManager interface {
	// ListModels returns all models currently available in Ollama.
	// Results are cached; use RefreshModelCache to update.
	ListModels(ctx context.Context) ([]OllamaModel, error)

	// RefreshModelCache forces a refresh of the cached model list.
	RefreshModelCache(ctx context.Context) error

	// HasModel checks if a specific model is available locally.
	// Handles model name variations (with/without :latest tag).
	HasModel(ctx context.Context, modelName string) (bool, error)

	// IsCustomModel checks if a model was locally created.
	// Uses template field presence as detection heuristic.
	IsCustomModel(ctx context.Context, modelName string) (bool, error)

	// PullModel downloads a model, reporting progress via callback.
	// The callback receives real-time progress from Ollama's streaming API.
	PullModel(ctx context.Context, modelName string, progress PullProgressCallback) error

	// GetModelSize returns the download size for a model.
	// For local models, returns the stored size. For remote models, queries registry.
	GetModelSize(ctx context.Context, modelName string) (int64, error)

	// GetBaseURL returns the Ollama server URL.
	GetBaseURL() string
}

// -----------------------------------------------------------------------------
// Struct Definition
// -----------------------------------------------------------------------------

// OllamaClient implements OllamaModelManager for the Ollama API.
type OllamaClient struct {
	// baseURL is the Ollama server URL.
	baseURL string

	// httpClient is used for API requests.
	httpClient *http.Client

	// Cache for model list
	cacheMu        sync.RWMutex
	modelCache     []OllamaModel
	cacheTime      time.Time
	cacheTTL       time.Duration
	customModelMap map[string]bool // Caches IsCustomModel results
}

// -----------------------------------------------------------------------------
// Constructor
// -----------------------------------------------------------------------------

// NewOllamaClient creates a new Ollama client.
//
// # Description
//
// Creates an OllamaModelManager configured for the specified server URL.
// The client caches model lists for 30 seconds to reduce API calls.
//
// # Inputs
//
//   - baseURL: Ollama server URL (e.g., "http://localhost:11434")
//
// # Outputs
//
//   - *OllamaClient: Configured client instance
//
// # Examples
//
//	client := NewOllamaClient("http://localhost:11434")
//	models, err := client.ListModels(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for _, m := range models {
//	    fmt.Printf("%s (%s)\n", m.Name, formatBytes(m.Size))
//	}
//
// # Assumptions
//
//   - Ollama server is running at the specified URL
//   - Network access to the server is available
func NewOllamaClient(baseURL string) *OllamaClient {
	// Normalize URL
	baseURL = strings.TrimSuffix(baseURL, "/")

	return &OllamaClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute, // Long timeout for model pulls
		},
		cacheTTL:       30 * time.Second,
		customModelMap: make(map[string]bool),
	}
}

// -----------------------------------------------------------------------------
// Model Listing
// -----------------------------------------------------------------------------

// ollamaTagsResponse is the response from /api/tags.
type ollamaTagsResponse struct {
	Models []ollamaModelInfo `json:"models"`
}

// ollamaModelInfo is a model entry from /api/tags.
//
// NOTE: The Details struct may be empty or partially populated depending
// on the Ollama version. Older versions may not include all fields.
// The code handles this gracefully by leaving fields at zero values.
type ollamaModelInfo struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	Digest     string    `json:"digest"`
	ModifiedAt time.Time `json:"modified_at"`
	Details    struct {
		Family            string `json:"family"`
		ParameterSize     string `json:"parameter_size"`
		QuantizationLevel string `json:"quantization_level"`
	} `json:"details"`
}

// ListModels returns all models currently available in Ollama.
//
// # Description
//
// Queries Ollama's /api/tags endpoint to get the list of locally available
// models. Results are cached for 30 seconds.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//
// # Outputs
//
//   - []OllamaModel: List of available models
//   - error: ModelError if the operation fails
//
// # Examples
//
//	models, err := client.ListModels(ctx)
//	if err != nil {
//	    if me, ok := err.(*ModelError); ok && me.Type == ModelErrorConnectionFailed {
//	        fmt.Println("Ollama is not running")
//	    }
//	    return
//	}
//	fmt.Printf("Found %d models\n", len(models))
//
// # Limitations
//
//   - Returns cached results if cache is still valid
//   - Does not refresh custom model status automatically
func (c *OllamaClient) ListModels(ctx context.Context) ([]OllamaModel, error) {
	// Check cache
	c.cacheMu.RLock()
	if time.Since(c.cacheTime) < c.cacheTTL && c.modelCache != nil {
		models := c.modelCache
		c.cacheMu.RUnlock()
		return models, nil
	}
	c.cacheMu.RUnlock()

	// Fetch from API
	return c.fetchModels(ctx)
}

// RefreshModelCache forces a refresh of the cached model list.
//
// # Description
//
// Clears the cache and fetches fresh data from Ollama.
// Use after pulling new models or when stale data is suspected.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//
// # Outputs
//
//   - error: ModelError if the refresh fails
func (c *OllamaClient) RefreshModelCache(ctx context.Context) error {
	c.cacheMu.Lock()
	c.modelCache = nil
	c.cacheTime = time.Time{}
	c.customModelMap = make(map[string]bool)
	c.cacheMu.Unlock()

	_, err := c.fetchModels(ctx)
	return err
}

func (c *OllamaClient) fetchModels(ctx context.Context) ([]OllamaModel, error) {
	url := c.baseURL + "/api/tags"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &ModelError{
			Type:        ModelErrorConnectionFailed,
			Message:     "Failed to create request",
			Detail:      err.Error(),
			Remediation: "Check that Ollama is running: ollama serve",
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &ModelError{
				Type:        ModelErrorContextCancelled,
				Message:     "Request cancelled",
				Detail:      ctx.Err().Error(),
				Remediation: "Try again or increase timeout",
			}
		}
		return nil, &ModelError{
			Type:        ModelErrorConnectionFailed,
			Message:     "Cannot connect to Ollama",
			Detail:      err.Error(),
			Remediation: fmt.Sprintf("Ensure Ollama is running at %s", c.baseURL),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, &ModelError{
			Type:        ModelErrorInvalidResponse,
			Message:     fmt.Sprintf("Ollama returned status %d", resp.StatusCode),
			Detail:      string(body),
			Remediation: "Check Ollama logs for errors",
		}
	}

	var tagsResp ollamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		return nil, &ModelError{
			Type:        ModelErrorInvalidResponse,
			Message:     "Failed to parse Ollama response",
			Detail:      err.Error(),
			Remediation: "This may indicate an Ollama version mismatch",
		}
	}

	models := make([]OllamaModel, 0, len(tagsResp.Models))
	for _, m := range tagsResp.Models {
		models = append(models, OllamaModel{
			Name:              m.Name,
			Size:              m.Size,
			ModifiedAt:        m.ModifiedAt,
			Digest:            m.Digest,
			Family:            m.Details.Family,
			ParameterSize:     m.Details.ParameterSize,
			QuantizationLevel: m.Details.QuantizationLevel,
		})
	}

	// Update cache
	c.cacheMu.Lock()
	c.modelCache = models
	c.cacheTime = time.Now()
	c.cacheMu.Unlock()

	slog.Debug("Fetched model list from Ollama", "count", len(models))
	return models, nil
}

// -----------------------------------------------------------------------------
// Model Checking
// -----------------------------------------------------------------------------

// HasModel checks if a specific model is available locally.
//
// # Description
//
// Checks if the specified model exists in Ollama. Handles model name
// variations - both "model" and "model:latest" will match.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//   - modelName: Model name to check (e.g., "nomic-embed-text-v2-moe")
//
// # Outputs
//
//   - bool: true if the model is available locally
//   - error: ModelError if the check fails
//
// # Examples
//
//	exists, err := client.HasModel(ctx, "nomic-embed-text-v2-moe")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	if !exists {
//	    fmt.Println("Model needs to be pulled")
//	}
//
// # Limitations
//
//   - Uses cached model list if available
func (c *OllamaClient) HasModel(ctx context.Context, modelName string) (bool, error) {
	models, err := c.ListModels(ctx)
	if err != nil {
		return false, err
	}

	// Normalize the search name
	searchName := normalizeModelName(modelName)

	for _, m := range models {
		if normalizeModelName(m.Name) == searchName {
			return true, nil
		}
	}

	return false, nil
}

// normalizeModelName removes the :latest tag if present for comparison.
func normalizeModelName(name string) string {
	// Lowercase first so we can match :latest regardless of case
	name = strings.ToLower(name)
	// Remove :latest suffix for comparison
	return strings.TrimSuffix(name, ":latest")
}

// -----------------------------------------------------------------------------
// Custom Model Detection
// -----------------------------------------------------------------------------

// ollamaShowResponse is the response from /api/show.
type ollamaShowResponse struct {
	Template   string `json:"template"`
	Modelfile  string `json:"modelfile"`
	Parameters string `json:"parameters"`
	License    string `json:"license"`
	Details    struct {
		Family            string   `json:"family"`
		Families          []string `json:"families"`
		ParameterSize     string   `json:"parameter_size"`
		QuantizationLevel string   `json:"quantization_level"`
	} `json:"details"`
}

// IsCustomModel checks if a model was locally created.
//
// # Description
//
// Detects custom models (created via `ollama create` or GGUF import) by
// checking for the presence of a template field in the model metadata.
// Registry-only models like embedding models don't have templates.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//   - modelName: Model name to check
//
// # Outputs
//
//   - bool: true if the model is locally created (has template)
//   - error: ModelError if the check fails
//
// # Examples
//
//	isCustom, err := client.IsCustomModel(ctx, "my-fine-tuned-llm")
//	if isCustom {
//	    fmt.Println("This is a custom local model - cannot be pulled from registry")
//	}
//
// # Limitations
//
//   - Requires the model to exist locally
//   - Result is cached per model name
func (c *OllamaClient) IsCustomModel(ctx context.Context, modelName string) (bool, error) {
	// Check cache
	c.cacheMu.RLock()
	if isCustom, ok := c.customModelMap[modelName]; ok {
		c.cacheMu.RUnlock()
		return isCustom, nil
	}
	c.cacheMu.RUnlock()

	// Query /api/show
	url := c.baseURL + "/api/show"
	reqBody := fmt.Sprintf(`{"name":"%s"}`, modelName)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(reqBody))
	if err != nil {
		return false, &ModelError{
			Type:        ModelErrorConnectionFailed,
			Model:       modelName,
			Message:     "Failed to create request",
			Detail:      err.Error(),
			Remediation: "Check that Ollama is running",
		}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, &ModelError{
			Type:        ModelErrorConnectionFailed,
			Model:       modelName,
			Message:     "Cannot connect to Ollama",
			Detail:      err.Error(),
			Remediation: fmt.Sprintf("Ensure Ollama is running at %s", c.baseURL),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return false, &ModelError{
			Type:        ModelErrorNotFound,
			Model:       modelName,
			Message:     fmt.Sprintf("Model '%s' not found", modelName),
			Remediation: fmt.Sprintf("Pull the model: ollama pull %s", modelName),
		}
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, &ModelError{
			Type:        ModelErrorInvalidResponse,
			Model:       modelName,
			Message:     fmt.Sprintf("Ollama returned status %d", resp.StatusCode),
			Detail:      string(body),
			Remediation: "Check Ollama logs for errors",
		}
	}

	var showResp ollamaShowResponse
	if err := json.NewDecoder(resp.Body).Decode(&showResp); err != nil {
		return false, &ModelError{
			Type:        ModelErrorInvalidResponse,
			Model:       modelName,
			Message:     "Failed to parse model info",
			Detail:      err.Error(),
			Remediation: "This may indicate an Ollama version mismatch",
		}
	}

	// Custom models have a template field from their Modelfile
	isCustom := showResp.Template != ""

	// Cache the result
	c.cacheMu.Lock()
	c.customModelMap[modelName] = isCustom
	c.cacheMu.Unlock()

	slog.Debug("Checked custom model status", "model", modelName, "isCustom", isCustom)
	return isCustom, nil
}

// -----------------------------------------------------------------------------
// Model Pulling
// -----------------------------------------------------------------------------

// ollamaPullRequest is the request body for /api/pull.
type ollamaPullRequest struct {
	Name   string `json:"name"`
	Stream bool   `json:"stream"`
}

// ollamaPullProgress is a single progress update from /api/pull streaming.
type ollamaPullProgress struct {
	Status    string `json:"status"`
	Digest    string `json:"digest,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Completed int64  `json:"completed,omitempty"`
	Error     string `json:"error,omitempty"`
}

// PullModel downloads a model, reporting progress via callback.
//
// # Description
//
// Downloads a model from the Ollama registry using Ollama's streaming API.
// Progress is reported via the callback in real-time.
//
// The callback receives:
//   - status: Current operation ("pulling manifest", "pulling sha256:...", etc.)
//   - completed: Bytes downloaded so far
//   - total: Total bytes to download (0 if unknown)
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//   - modelName: Model to download (e.g., "nomic-embed-text-v2-moe")
//   - progress: Callback for progress updates (can be nil to skip progress)
//
// # Outputs
//
//   - error: ModelError if the pull fails
//
// # Examples
//
//	err := client.PullModel(ctx, "nomic-embed-text-v2-moe", func(status string, completed, total int64) {
//	    if total > 0 {
//	        percent := float64(completed) / float64(total) * 100
//	        fmt.Printf("\r%s: %.1f%%", status, percent)
//	    } else {
//	        fmt.Printf("\r%s...", status)
//	    }
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println("\nDone!")
//
// # Limitations
//
//   - Requires network access to Ollama registry
//   - Large models may take significant time to download
//
// # Assumptions
//
//   - Ollama server is running and accessible
//   - Sufficient disk space is available (check with SystemChecker first)
func (c *OllamaClient) PullModel(ctx context.Context, modelName string, progress PullProgressCallback) error {
	url := c.baseURL + "/api/pull"

	reqBody := ollamaPullRequest{
		Name:   modelName,
		Stream: true,
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return &ModelError{
			Type:        ModelErrorPullFailed,
			Model:       modelName,
			Message:     "Failed to create pull request",
			Detail:      err.Error(),
			Remediation: "This is an internal error - please report it",
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBytes))
	if err != nil {
		return &ModelError{
			Type:        ModelErrorConnectionFailed,
			Model:       modelName,
			Message:     "Failed to create request",
			Detail:      err.Error(),
			Remediation: "Check that Ollama is running: ollama serve",
		}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return &ModelError{
				Type:        ModelErrorContextCancelled,
				Model:       modelName,
				Message:     "Pull cancelled",
				Detail:      ctx.Err().Error(),
				Remediation: "Try again to resume the download",
			}
		}
		return &ModelError{
			Type:        ModelErrorConnectionFailed,
			Model:       modelName,
			Message:     "Cannot connect to Ollama",
			Detail:      err.Error(),
			Remediation: fmt.Sprintf("Ensure Ollama is running at %s", c.baseURL),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &ModelError{
			Type:        ModelErrorPullFailed,
			Model:       modelName,
			Message:     fmt.Sprintf("Pull failed with status %d", resp.StatusCode),
			Detail:      string(body),
			Remediation: "Check if the model name is correct and registry is accessible",
		}
	}

	// Parse streaming response
	scanner := bufio.NewScanner(resp.Body)
	// Increase buffer for large progress lines
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return &ModelError{
				Type:        ModelErrorContextCancelled,
				Model:       modelName,
				Message:     "Pull cancelled",
				Detail:      ctx.Err().Error(),
				Remediation: "Try again to resume the download",
			}
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var prog ollamaPullProgress
		if err := json.Unmarshal(line, &prog); err != nil {
			slog.Debug("Failed to parse progress line", "line", string(line), "error", err)
			continue
		}

		// Check for error in progress
		if prog.Error != "" {
			return &ModelError{
				Type:        ModelErrorPullFailed,
				Model:       modelName,
				Message:     "Pull failed",
				Detail:      prog.Error,
				Remediation: "Check network connection and try again",
			}
		}

		// Report progress if callback provided
		if progress != nil {
			progress(prog.Status, prog.Completed, prog.Total)
		}
	}

	if err := scanner.Err(); err != nil {
		return &ModelError{
			Type:        ModelErrorPullFailed,
			Model:       modelName,
			Message:     "Error reading pull response",
			Detail:      err.Error(),
			Remediation: "Check network connection and try again",
		}
	}

	// Refresh cache after successful pull
	c.cacheMu.Lock()
	c.modelCache = nil
	c.cacheTime = time.Time{}
	c.cacheMu.Unlock()

	slog.Info("Model pulled successfully", "model", modelName)
	return nil
}

// -----------------------------------------------------------------------------
// Model Size Query
// -----------------------------------------------------------------------------

// GetModelSize returns the download size for a model.
//
// # Description
//
// Returns the size of a model in bytes. For locally available models,
// returns the stored size. For remote models, this would query the registry
// (not implemented - returns fallback estimate).
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//   - modelName: Model name to check
//
// # Outputs
//
//   - int64: Model size in bytes
//   - error: ModelError if the query fails
//
// # Examples
//
//	size, err := client.GetModelSize(ctx, "nomic-embed-text-v2-moe")
//	if err == nil {
//	    fmt.Printf("Model size: %s\n", formatBytes(size))
//	}
//
// # Limitations
//
//   - Returns fallback estimate (500MB) if model is not found locally
//   - Does not query remote registry for size
func (c *OllamaClient) GetModelSize(ctx context.Context, modelName string) (int64, error) {
	models, err := c.ListModels(ctx)
	if err != nil {
		return 0, err
	}

	searchName := normalizeModelName(modelName)

	for _, m := range models {
		if normalizeModelName(m.Name) == searchName {
			return m.Size, nil
		}
	}

	// Model not found locally - return fallback estimate
	// This is used when planning disk space for models not yet downloaded
	slog.Debug("Model not found locally, using fallback size estimate", "model", modelName)
	return 500 * 1024 * 1024, nil // 500 MB fallback
}

// GetBaseURL returns the Ollama server URL.
//
// # Description
//
// Returns the base URL configured for this client.
//
// # Outputs
//
//   - string: The Ollama server URL
func (c *OllamaClient) GetBaseURL() string {
	return c.baseURL
}
