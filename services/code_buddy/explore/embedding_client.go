// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package explore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultEmbeddingTimeout is the default timeout for embedding requests.
const DefaultEmbeddingTimeout = 30 * time.Second

// EmbeddingClient wraps calls to the embeddings service.
//
// # Description
//
// EmbeddingClient provides a Go interface to the Python embeddings service,
// which runs transformer models (like BGE, Qwen) to generate vector embeddings
// for text. These embeddings enable semantic similarity search.
//
// # Thread Safety
//
// EmbeddingClient is safe for concurrent use.
type EmbeddingClient struct {
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
}

// NewEmbeddingClient creates a new embedding client.
//
// # Description
//
// Creates a client configured to connect to the embeddings service.
// The service should be running and accessible at the given URL.
//
// # Inputs
//
//   - baseURL: The base URL of the embeddings service (e.g., "http://localhost:8000").
//
// # Example
//
//	client := explore.NewEmbeddingClient("http://localhost:8000")
//	vector, err := client.Embed(ctx, "func ProcessData(ctx context.Context) error")
func NewEmbeddingClient(baseURL string) *EmbeddingClient {
	return &EmbeddingClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: DefaultEmbeddingTimeout,
		},
		timeout: DefaultEmbeddingTimeout,
	}
}

// WithTimeout sets a custom timeout for embedding requests.
func (c *EmbeddingClient) WithTimeout(timeout time.Duration) *EmbeddingClient {
	c.timeout = timeout
	c.httpClient.Timeout = timeout
	return c
}

// embeddingRequest is the request body for the /embed endpoint.
type embeddingRequest struct {
	Texts []string `json:"texts"`
}

// embeddingResponse is the response from the /embed endpoint.
type embeddingResponse struct {
	ID        string      `json:"id"`
	Timestamp int64       `json:"timestamp"`
	Model     string      `json:"model"`
	Vectors   [][]float32 `json:"vectors"`
	Dim       int         `json:"dim"`
}

// healthResponse is the response from the /health endpoint.
type healthResponse struct {
	Status string `json:"status"`
	Model  string `json:"model"`
}

// Embed computes a vector embedding for the given text.
//
// # Description
//
// Calls the embeddings service to convert text into a dense vector
// representation suitable for semantic similarity search.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - text: The text to embed (e.g., function signature, docstring).
//
// # Outputs
//
//   - []float32: The embedding vector.
//   - error: Non-nil if embedding fails.
//
// # Example
//
//	vector, err := client.Embed(ctx, "func ProcessData(ctx context.Context, data []byte) error")
//	if err != nil {
//	    // Handle error - may fall back to structural similarity
//	}
//
// # Limitations
//
//   - Text may be truncated if exceeding model's max input tokens (512 for BGE).
//   - Network latency depends on embedding service location.
func (c *EmbeddingClient) Embed(ctx context.Context, text string) ([]float32, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}
	if text == "" {
		return nil, fmt.Errorf("%w: text is empty", ErrInvalidInput)
	}

	vectors, err := c.BatchEmbed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("embedding service returned no vectors")
	}

	return vectors[0], nil
}

// BatchEmbed computes embeddings for multiple texts efficiently.
//
// # Description
//
// Batches multiple texts into a single request for efficiency.
// The service processes them together, reducing overhead.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - texts: The texts to embed.
//
// # Outputs
//
//   - [][]float32: The embedding vectors, one per input text.
//   - error: Non-nil if embedding fails.
//
// # Performance
//
// Batching is more efficient than individual Embed calls when
// embedding multiple texts. Target: < 50ms per text in batch.
func (c *EmbeddingClient) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}
	if len(texts) == 0 {
		return nil, fmt.Errorf("%w: texts is empty", ErrInvalidInput)
	}

	// Build request
	reqBody := embeddingRequest{Texts: texts}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Create HTTP request
	url := c.baseURL + "/batch_embed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	// Check status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding service returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var embResp embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return embResp.Vectors, nil
}

// Health checks if the embeddings service is available.
//
// # Description
//
// Performs a health check against the embeddings service to verify
// it is running and the model is loaded.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//
// # Outputs
//
//   - error: Non-nil if service is unavailable.
//
// # Example
//
//	if err := client.Health(ctx); err != nil {
//	    log.Warn("Embeddings service unavailable, falling back to structural similarity")
//	}
func (c *EmbeddingClient) Health(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidInput
	}

	url := c.baseURL + "/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("embeddings service unhealthy: status %d: %s", resp.StatusCode, string(body))
	}

	var health healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return fmt.Errorf("decode health response: %w", err)
	}

	if health.Status != "ok" {
		return fmt.Errorf("embeddings service not ready: %s", health.Status)
	}

	return nil
}

// BaseURL returns the configured base URL.
func (c *EmbeddingClient) BaseURL() string {
	return c.baseURL
}

// CosineSimilarity computes the cosine similarity between two vectors.
//
// # Description
//
// Computes the cosine of the angle between two vectors, which is a
// common similarity metric for embeddings. Returns a value between
// -1 (opposite) and 1 (identical).
//
// # Inputs
//
//   - a, b: The vectors to compare. Must have the same length.
//
// # Outputs
//
//   - float64: The cosine similarity score.
//
// # Performance
//
// O(n) where n is the vector dimension. Typical: < 1μs for 768-dim vectors.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0.0
	}

	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dot / (sqrt(normA) * sqrt(normB))
}

// sqrt is a simple square root implementation to avoid importing math.
func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	// Newton's method
	z := x / 2
	for i := 0; i < 10; i++ {
		z = z - (z*z-x)/(2*z)
	}
	return z
}
