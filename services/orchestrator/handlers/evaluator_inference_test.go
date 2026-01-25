// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/datatypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Fixtures
// =============================================================================

func validInferenceTestRequest() *datatypes.InferenceRequest {
	return &datatypes.InferenceRequest{
		RequestID: "test-request-123",
		Timestamp: time.Now().UTC(),
		Ticker:    "SPY",
		Model:     "amazon/chronos-t5-tiny",
		Context: datatypes.ContextData{
			Values:    []float64{100.0, 101.5, 99.8, 102.0},
			Period:    datatypes.Period1d,
			Source:    datatypes.SourceInfluxDB,
			StartDate: "2025-01-01",
			EndDate:   "2025-01-04",
			Field:     datatypes.FieldClose,
		},
		Horizon: datatypes.HorizonSpec{
			Length: 10,
			Period: datatypes.Period1d,
		},
		Params: nil,
	}
}

func validInferenceTestResponse() *datatypes.InferenceResponse {
	return &datatypes.InferenceResponse{
		RequestID:  "test-request-123",
		ResponseID: "test-response-456",
		Timestamp:  time.Now().UTC(),
		Ticker:     "SPY",
		Model:      "amazon/chronos-t5-tiny",
		Forecast: datatypes.ForecastData{
			Values:    []float64{103.0, 104.5, 102.8, 105.0, 106.2, 104.9, 107.1, 108.0, 106.5, 109.0},
			Period:    datatypes.Period1d,
			StartDate: "2025-01-05",
			EndDate:   "2025-01-14",
		},
		ContextSummary: datatypes.ContextSummary{
			Length:    4,
			Period:    datatypes.Period1d,
			Source:    datatypes.SourceInfluxDB,
			StartDate: "2025-01-01",
			EndDate:   "2025-01-04",
			Field:     datatypes.FieldClose,
		},
		Quantiles: nil,
		Metadata: datatypes.InferenceMetadata{
			InferenceTimeMs: 245,
			ModelVersion:    "1.0.0",
			Device:          "cpu",
			ModelFamily:     "chronos",
		},
	}
}

// createInferenceTestEvaluator creates an Evaluator with a mock HTTP server for inference tests.
func createInferenceTestEvaluator(handler http.HandlerFunc) (*Evaluator, *httptest.Server) {
	mockServer := httptest.NewServer(handler)
	evaluator := &Evaluator{
		httpClient:      &http.Client{Timeout: 5 * time.Second},
		orchestratorURL: mockServer.URL,
	}
	return evaluator, mockServer
}

// =============================================================================
// buildInferenceURL Tests
// =============================================================================

func TestBuildInferenceURL(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		expected string
	}{
		{
			name:     "localhost",
			baseURL:  "http://localhost:12700",
			expected: "http://localhost:12700/orchestration/v1/predict",
		},
		{
			name:     "container name",
			baseURL:  "http://sapheneia-forecast:8000",
			expected: "http://sapheneia-forecast:8000/orchestration/v1/predict",
		},
		{
			name:     "production URL",
			baseURL:  "https://api.sapheneia.example.com",
			expected: "https://api.sapheneia.example.com/orchestration/v1/predict",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildInferenceURL(tt.baseURL)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// CallInferenceService Tests
// =============================================================================

func TestCallInferenceService_Success(t *testing.T) {
	var receivedReq datatypes.InferenceRequest

	handler := func(w http.ResponseWriter, r *http.Request) {
		// Verify endpoint
		assert.Equal(t, "/orchestration/v1/predict", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Parse request body
		err := json.NewDecoder(r.Body).Decode(&receivedReq)
		require.NoError(t, err)

		// Return success response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(validInferenceTestResponse()); err != nil {
			t.Logf("Warning: failed to encode response: %v", err)
		}
	}

	evaluator, mockServer := createInferenceTestEvaluator(handler)
	defer mockServer.Close()

	ctx := context.Background()
	req := validInferenceTestRequest()

	resp, err := evaluator.CallInferenceService(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "test-request-123", resp.RequestID)
	assert.Equal(t, "test-response-456", resp.ResponseID)
	assert.Equal(t, "SPY", resp.Ticker)
	assert.Equal(t, "amazon/chronos-t5-tiny", resp.Model)
	assert.Len(t, resp.Forecast.Values, 10)
	assert.Equal(t, 245, resp.Metadata.InferenceTimeMs)

	// Verify request was sent correctly
	assert.Equal(t, "test-request-123", receivedReq.RequestID)
	assert.Equal(t, "SPY", receivedReq.Ticker)
	assert.Equal(t, "amazon/chronos-t5-tiny", receivedReq.Model)
	assert.Equal(t, []float64{100.0, 101.5, 99.8, 102.0}, receivedReq.Context.Values)
}

func TestCallInferenceService_ValidationFailure(t *testing.T) {
	callCount := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}

	evaluator, mockServer := createInferenceTestEvaluator(handler)
	defer mockServer.Close()

	ctx := context.Background()
	req := validInferenceTestRequest()
	req.RequestID = "" // Invalid: empty request ID

	resp, err := evaluator.CallInferenceService(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid inference request")
	assert.Contains(t, err.Error(), "request_id")

	// Verify no HTTP call was made
	assert.Equal(t, 0, callCount)
}

func TestCallInferenceService_ModelWithoutSlash(t *testing.T) {
	callCount := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}

	evaluator, mockServer := createInferenceTestEvaluator(handler)
	defer mockServer.Close()

	ctx := context.Background()
	req := validInferenceTestRequest()
	req.Model = "chronos-t5-tiny" // Missing vendor prefix

	resp, err := evaluator.CallInferenceService(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "full name")

	// Verify no HTTP call was made
	assert.Equal(t, 0, callCount)
}

func TestCallInferenceService_RetryOn5xx(t *testing.T) {
	var callCount int32

	handler := func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		if count < 3 {
			// First two calls return 500
			w.WriteHeader(http.StatusInternalServerError)
			if _, err := w.Write([]byte(`{"error": "internal error"}`)); err != nil {
				t.Logf("Warning: failed to write response: %v", err)
			}
			return
		}
		// Third call succeeds
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(validInferenceTestResponse()); err != nil {
			t.Logf("Warning: failed to encode response: %v", err)
		}
	}

	evaluator, mockServer := createInferenceTestEvaluator(handler)
	defer mockServer.Close()

	ctx := context.Background()
	req := validInferenceTestRequest()

	resp, err := evaluator.CallInferenceService(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "test-request-123", resp.RequestID)

	// Verify all 3 attempts were made
	assert.Equal(t, int32(3), atomic.LoadInt32(&callCount))
}

func TestCallInferenceService_4xxError(t *testing.T) {
	// NOTE: The current retryWithBackoff implementation retries on ALL errors,
	// including 4xx. The "(not retryable)" message is informational only.
	// This test verifies the error is returned correctly after all retries.
	var callCount int32

	handler := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusBadRequest)
		if _, err := w.Write([]byte(`{"error": "invalid model"}`)); err != nil {
			t.Logf("Warning: failed to write response: %v", err)
		}
	}

	evaluator, mockServer := createInferenceTestEvaluator(handler)
	defer mockServer.Close()

	ctx := context.Background()
	req := validInferenceTestRequest()

	resp, err := evaluator.CallInferenceService(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "not retryable")
	assert.Contains(t, err.Error(), "400")

	// Current behavior: retries happen even on 4xx (maxRetries = 3)
	assert.Equal(t, int32(3), atomic.LoadInt32(&callCount))
}

func TestCallInferenceService_ContextCancelled(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}

	evaluator, mockServer := createInferenceTestEvaluator(handler)
	defer mockServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	req := validInferenceTestRequest()

	resp, err := evaluator.CallInferenceService(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
	// Error should indicate context deadline or cancellation
	assert.True(t, err != nil)
}

func TestCallInferenceService_InvalidJSONResponse(t *testing.T) {
	// NOTE: The current implementation returns empty struct on decode error
	// after retries, since json.Decoder doesn't return error for partial decode.
	// This test verifies the error message contains decode info.
	var callCount int32

	handler := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("not valid json{{{")); err != nil {
			t.Logf("Warning: failed to write response: %v", err)
		}
	}

	evaluator, mockServer := createInferenceTestEvaluator(handler)
	defer mockServer.Close()

	ctx := context.Background()
	req := validInferenceTestRequest()

	_, err := evaluator.CallInferenceService(ctx, req)

	// After max retries, the error is returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode")

	// Verify retries were attempted
	assert.Equal(t, int32(3), atomic.LoadInt32(&callCount))
}

func TestCallInferenceService_AuthHeader(t *testing.T) {
	// Set API key for this test
	t.Setenv("SAPHENEIA_API_KEY", "test-api-key-12345")

	var receivedAuthHeader string

	handler := func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(validInferenceTestResponse()); err != nil {
			t.Logf("Warning: failed to encode response: %v", err)
		}
	}

	evaluator, mockServer := createInferenceTestEvaluator(handler)
	defer mockServer.Close()

	ctx := context.Background()
	req := validInferenceTestRequest()

	resp, err := evaluator.CallInferenceService(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify Authorization header was set
	assert.Equal(t, "Bearer test-api-key-12345", receivedAuthHeader)
}

func TestCallInferenceService_NoAuthHeaderWhenKeyNotSet(t *testing.T) {
	// Ensure no API key is set
	t.Setenv("SAPHENEIA_API_KEY", "")

	var receivedAuthHeader string

	handler := func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(validInferenceTestResponse()); err != nil {
			t.Logf("Warning: failed to encode response: %v", err)
		}
	}

	evaluator, mockServer := createInferenceTestEvaluator(handler)
	defer mockServer.Close()

	ctx := context.Background()
	req := validInferenceTestRequest()

	resp, err := evaluator.CallInferenceService(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify no Authorization header
	assert.Empty(t, receivedAuthHeader)
}

func TestCallInferenceService_WithQuantilesResponse(t *testing.T) {
	responseWithQuantiles := validInferenceTestResponse()
	responseWithQuantiles.Quantiles = []datatypes.QuantileForecast{
		{Quantile: 0.1, Values: []float64{99.0, 100.0, 98.5}},
		{Quantile: 0.5, Values: []float64{103.0, 104.5, 102.8}},
		{Quantile: 0.9, Values: []float64{107.0, 109.0, 107.1}},
	}

	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(responseWithQuantiles); err != nil {
			t.Logf("Warning: failed to encode response: %v", err)
		}
	}

	evaluator, mockServer := createInferenceTestEvaluator(handler)
	defer mockServer.Close()

	ctx := context.Background()
	req := validInferenceTestRequest()
	req.Params = &datatypes.ModelParams{
		Quantiles: []float64{0.1, 0.5, 0.9},
	}

	resp, err := evaluator.CallInferenceService(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Quantiles, 3)
	assert.Equal(t, 0.1, resp.Quantiles[0].Quantile)
	assert.Equal(t, 0.5, resp.Quantiles[1].Quantile)
	assert.Equal(t, 0.9, resp.Quantiles[2].Quantile)
}

func TestCallInferenceService_EmptyContextValues(t *testing.T) {
	callCount := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}

	evaluator, mockServer := createInferenceTestEvaluator(handler)
	defer mockServer.Close()

	ctx := context.Background()
	req := validInferenceTestRequest()
	req.Context.Values = []float64{} // Empty values

	resp, err := evaluator.CallInferenceService(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "context.values")

	// Verify no HTTP call was made
	assert.Equal(t, 0, callCount)
}
