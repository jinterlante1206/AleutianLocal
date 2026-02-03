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
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewEmbeddingClient(t *testing.T) {
	client := NewEmbeddingClient("http://localhost:8000")

	if client == nil {
		t.Fatal("expected client to be non-nil")
	}
	if client.baseURL != "http://localhost:8000" {
		t.Errorf("expected baseURL 'http://localhost:8000', got %s", client.baseURL)
	}
	if client.httpClient == nil {
		t.Error("expected httpClient to be non-nil")
	}
}

func TestEmbeddingClient_WithTimeout(t *testing.T) {
	client := NewEmbeddingClient("http://localhost:8000").WithTimeout(5 * time.Second)

	if client.timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", client.timeout)
	}
}

func TestEmbeddingClient_Embed_NilContext(t *testing.T) {
	client := NewEmbeddingClient("http://localhost:8000")

	_, err := client.Embed(nil, "test text")

	if err != ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestEmbeddingClient_Embed_EmptyText(t *testing.T) {
	client := NewEmbeddingClient("http://localhost:8000")
	ctx := context.Background()

	_, err := client.Embed(ctx, "")

	if err == nil {
		t.Error("expected error for empty text")
	}
}

func TestEmbeddingClient_BatchEmbed_EmptyTexts(t *testing.T) {
	client := NewEmbeddingClient("http://localhost:8000")
	ctx := context.Background()

	_, err := client.BatchEmbed(ctx, []string{})

	if err == nil {
		t.Error("expected error for empty texts")
	}
}

func TestEmbeddingClient_Embed_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/batch_embed" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// Return mock embedding
		resp := embeddingResponse{
			ID:        "test-id",
			Timestamp: time.Now().Unix(),
			Model:     "test-model",
			Vectors:   [][]float32{{0.1, 0.2, 0.3, 0.4}},
			Dim:       4,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewEmbeddingClient(server.URL)
	ctx := context.Background()

	vector, err := client.Embed(ctx, "test text")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vector) != 4 {
		t.Errorf("expected vector length 4, got %d", len(vector))
	}
	if vector[0] != 0.1 {
		t.Errorf("expected first element 0.1, got %f", vector[0])
	}
}

func TestEmbeddingClient_BatchEmbed_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := embeddingResponse{
			ID:        "test-id",
			Timestamp: time.Now().Unix(),
			Model:     "test-model",
			Vectors: [][]float32{
				{0.1, 0.2, 0.3},
				{0.4, 0.5, 0.6},
			},
			Dim: 3,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewEmbeddingClient(server.URL)
	ctx := context.Background()

	vectors, err := client.BatchEmbed(ctx, []string{"text1", "text2"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vectors) != 2 {
		t.Errorf("expected 2 vectors, got %d", len(vectors))
	}
}

func TestEmbeddingClient_Embed_ServiceError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"detail": "Model not ready"}`))
	}))
	defer server.Close()

	client := NewEmbeddingClient(server.URL)
	ctx := context.Background()

	_, err := client.Embed(ctx, "test text")

	if err == nil {
		t.Error("expected error for service error")
	}
}

func TestEmbeddingClient_Health_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := healthResponse{
			Status: "ok",
			Model:  "test-model",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewEmbeddingClient(server.URL)
	ctx := context.Background()

	err := client.Health(ctx)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEmbeddingClient_Health_Unhealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status": "initializing"}`))
	}))
	defer server.Close()

	client := NewEmbeddingClient(server.URL)
	ctx := context.Background()

	err := client.Health(ctx)

	if err == nil {
		t.Error("expected error for unhealthy service")
	}
}

func TestEmbeddingClient_Health_NilContext(t *testing.T) {
	client := NewEmbeddingClient("http://localhost:8000")

	err := client.Health(nil)

	if err != ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float64
		delta    float64
	}{
		{
			name:     "identical vectors",
			a:        []float32{1.0, 0.0, 0.0},
			b:        []float32{1.0, 0.0, 0.0},
			expected: 1.0,
			delta:    0.001,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1.0, 0.0, 0.0},
			b:        []float32{0.0, 1.0, 0.0},
			expected: 0.0,
			delta:    0.001,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1.0, 0.0, 0.0},
			b:        []float32{-1.0, 0.0, 0.0},
			expected: -1.0,
			delta:    0.001,
		},
		{
			name:     "similar vectors",
			a:        []float32{1.0, 1.0, 0.0},
			b:        []float32{1.0, 0.0, 0.0},
			expected: 0.707, // cos(45°)
			delta:    0.01,
		},
		{
			name:     "empty vectors",
			a:        []float32{},
			b:        []float32{},
			expected: 0.0,
			delta:    0.001,
		},
		{
			name:     "different lengths",
			a:        []float32{1.0, 2.0},
			b:        []float32{1.0, 2.0, 3.0},
			expected: 0.0,
			delta:    0.001,
		},
		{
			name:     "zero vector",
			a:        []float32{0.0, 0.0, 0.0},
			b:        []float32{1.0, 2.0, 3.0},
			expected: 0.0,
			delta:    0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CosineSimilarity(tt.a, tt.b)

			if math.Abs(result-tt.expected) > tt.delta {
				t.Errorf("expected %f, got %f", tt.expected, result)
			}
		})
	}
}

func TestSqrt(t *testing.T) {
	tests := []struct {
		input    float64
		expected float64
		delta    float64
	}{
		{4.0, 2.0, 0.001},
		{9.0, 3.0, 0.001},
		{2.0, 1.414, 0.01},
		{0.0, 0.0, 0.001},
		{-1.0, 0.0, 0.001}, // Negative returns 0
	}

	for _, tt := range tests {
		result := sqrt(tt.input)
		if math.Abs(result-tt.expected) > tt.delta {
			t.Errorf("sqrt(%f) = %f, expected %f", tt.input, result, tt.expected)
		}
	}
}

func TestEmbeddingClient_BaseURL(t *testing.T) {
	client := NewEmbeddingClient("http://test:8000")

	if client.BaseURL() != "http://test:8000" {
		t.Errorf("expected 'http://test:8000', got %s", client.BaseURL())
	}
}
