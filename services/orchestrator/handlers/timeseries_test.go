// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Tests for timeseries.go handlers

package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// =============================================================================
// normalizeModelName Tests
// =============================================================================

func TestNormalizeModelName_StripOrgPrefix(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"amazon/chronos-t5-tiny", "chronos-t5-tiny"},
		{"google/timesfm-1.0-200m", "timesfm-1-0-200m"},
		{"salesforce/moirai-1.0-R-small", "moirai-1-0-r-small"},
	}

	for _, tc := range testCases {
		result := normalizeModelName(tc.input)
		if result != tc.expected {
			t.Errorf("normalizeModelName(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestNormalizeModelName_AlreadyNormalized(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"chronos-t5-tiny", "chronos-t5-tiny"},
		{"timesfm-1-0", "timesfm-1-0"},
	}

	for _, tc := range testCases {
		result := normalizeModelName(tc.input)
		if result != tc.expected {
			t.Errorf("normalizeModelName(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestNormalizeModelName_SpecialCharacters(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"Chronos T5 (Tiny)", "chronos-t5-tiny"},
		{"chronos_t5_tiny", "chronos-t5-tiny"},
		{"CHRONOS-T5-TINY", "chronos-t5-tiny"},
		{"model.with.dots", "model-with-dots"},
		{"model  with   spaces", "model-with-spaces"},
	}

	for _, tc := range testCases {
		result := normalizeModelName(tc.input)
		if result != tc.expected {
			t.Errorf("normalizeModelName(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestNormalizeModelName_EdgeCases(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"---", ""},
		{"a", "a"},
		{"123", "123"},
	}

	for _, tc := range testCases {
		result := normalizeModelName(tc.input)
		if result != tc.expected {
			t.Errorf("normalizeModelName(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

// =============================================================================
// getSerivceURL Tests
// =============================================================================

func TestGetServiceURL_StandaloneMode(t *testing.T) {
	// Save and restore env vars
	origMode := os.Getenv("ALEUTIAN_FORECAST_MODE")
	origURL := os.Getenv("ALEUTIAN_TIMESERIES_TOOL")
	defer func() {
		os.Setenv("ALEUTIAN_FORECAST_MODE", origMode)
		os.Setenv("ALEUTIAN_TIMESERIES_TOOL", origURL)
	}()

	os.Setenv("ALEUTIAN_FORECAST_MODE", "standalone")
	os.Setenv("ALEUTIAN_TIMESERIES_TOOL", "http://forecast-service:12000")

	// All models should go to the unified service in standalone mode
	testCases := []string{
		"chronos-t5-tiny",
		"timesfm-1-0",
		"unknown-model",
	}

	for _, model := range testCases {
		url, err := getSerivceURL(model)
		if err != nil {
			t.Errorf("getSerivceURL(%q) returned error: %v", model, err)
		}
		if url != "http://forecast-service:12000" {
			t.Errorf("getSerivceURL(%q) = %q, expected unified service URL", model, url)
		}
	}
}

func TestGetServiceURL_SapheneiaMode_KnownModels(t *testing.T) {
	// Save and restore env vars
	origMode := os.Getenv("ALEUTIAN_FORECAST_MODE")
	origURL := os.Getenv("ALEUTIAN_TIMESERIES_TOOL")
	defer func() {
		os.Setenv("ALEUTIAN_FORECAST_MODE", origMode)
		os.Setenv("ALEUTIAN_TIMESERIES_TOOL", origURL)
	}()

	os.Unsetenv("ALEUTIAN_FORECAST_MODE") // Default to Sapheneia mode
	os.Setenv("ALEUTIAN_TIMESERIES_TOOL", "http://default:8000")

	testCases := []struct {
		model    string
		expected string
	}{
		{"chronos-t5-tiny", "http://forecast-chronos-t5-tiny:8000"},
		{"chronos-t5-base", "http://forecast-chronos-t5-base:8000"},
		{"timesfm-1-0", "http://forecast-timesfm-1-0:8000"},
		{"moirai-1-1-small", "http://forecast-moirai-1-1-small:8000"},
		{"lag-llama", "http://forecast-lag-llama:8000"},
	}

	for _, tc := range testCases {
		url, err := getSerivceURL(tc.model)
		if err != nil {
			t.Errorf("getSerivceURL(%q) returned error: %v", tc.model, err)
		}
		if url != tc.expected {
			t.Errorf("getSerivceURL(%q) = %q, expected %q", tc.model, url, tc.expected)
		}
	}
}

func TestGetServiceURL_SapheneiaMode_UnknownModel(t *testing.T) {
	// Save and restore env vars
	origMode := os.Getenv("ALEUTIAN_FORECAST_MODE")
	origURL := os.Getenv("ALEUTIAN_TIMESERIES_TOOL")
	defer func() {
		os.Setenv("ALEUTIAN_FORECAST_MODE", origMode)
		os.Setenv("ALEUTIAN_TIMESERIES_TOOL", origURL)
	}()

	os.Unsetenv("ALEUTIAN_FORECAST_MODE")
	os.Setenv("ALEUTIAN_TIMESERIES_TOOL", "http://fallback:8000")

	url, err := getSerivceURL("unknown-future-model")
	if err != nil {
		t.Errorf("getSerivceURL returned error: %v", err)
	}
	if url != "http://fallback:8000" {
		t.Errorf("Expected fallback URL, got %q", url)
	}
}

func TestGetServiceURL_EnvOverride(t *testing.T) {
	// Save and restore env vars
	origMode := os.Getenv("ALEUTIAN_FORECAST_MODE")
	origOverride := os.Getenv("TIMESERIES_SERVICE_CHRONOS_T5_TINY")
	defer func() {
		os.Setenv("ALEUTIAN_FORECAST_MODE", origMode)
		if origOverride == "" {
			os.Unsetenv("TIMESERIES_SERVICE_CHRONOS_T5_TINY")
		} else {
			os.Setenv("TIMESERIES_SERVICE_CHRONOS_T5_TINY", origOverride)
		}
	}()

	os.Unsetenv("ALEUTIAN_FORECAST_MODE")
	os.Setenv("TIMESERIES_SERVICE_CHRONOS_T5_TINY", "http://custom-override:9999")

	url, err := getSerivceURL("chronos-t5-tiny")
	if err != nil {
		t.Errorf("getSerivceURL returned error: %v", err)
	}
	if url != "http://custom-override:9999" {
		t.Errorf("Expected env override URL, got %q", url)
	}
}

func TestGetServiceURL_EmptyModel(t *testing.T) {
	origURL := os.Getenv("ALEUTIAN_TIMESERIES_TOOL")
	defer os.Setenv("ALEUTIAN_TIMESERIES_TOOL", origURL)

	os.Setenv("ALEUTIAN_TIMESERIES_TOOL", "http://default:8000")

	url, err := getSerivceURL("")
	if err != nil {
		t.Errorf("getSerivceURL returned error: %v", err)
	}
	// Should return default URL for empty model
	if url != "http://default:8000" {
		t.Errorf("Expected default URL for empty model, got %q", url)
	}
}

// =============================================================================
// TimeSeriesRequest Tests
// =============================================================================

func TestTimeSeriesRequest_JSONParsing(t *testing.T) {
	jsonData := `{
		"model": "chronos-t5-tiny",
		"name": "SPY",
		"data": [100.0, 101.0, 102.0],
		"context_period_size": 252,
		"forecast_period_size": 5,
		"num_samples": 20,
		"as_of_date": "2024-01-15"
	}`

	var req TimeSeriesRequest
	err := json.Unmarshal([]byte(jsonData), &req)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if req.Model != "chronos-t5-tiny" {
		t.Errorf("Expected model 'chronos-t5-tiny', got %q", req.Model)
	}
	if req.Name != "SPY" {
		t.Errorf("Expected name 'SPY', got %q", req.Name)
	}
	if len(req.Data) != 3 {
		t.Errorf("Expected 3 data points, got %d", len(req.Data))
	}
	if req.ContextPeriodSize != 252 {
		t.Errorf("Expected context_period_size 252, got %d", req.ContextPeriodSize)
	}
	if req.ForecastPeriodSize != 5 {
		t.Errorf("Expected forecast_period_size 5, got %d", req.ForecastPeriodSize)
	}
}

func TestTimeSeriesRequest_RecentDataAlias(t *testing.T) {
	jsonData := `{
		"model": "chronos-t5-tiny",
		"recent_data": [100.0, 101.0, 102.0],
		"horizon": 10
	}`

	var req TimeSeriesRequest
	err := json.Unmarshal([]byte(jsonData), &req)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if len(req.RecentData) != 3 {
		t.Errorf("Expected 3 recent_data points, got %d", len(req.RecentData))
	}
	if req.Horizon != 10 {
		t.Errorf("Expected horizon 10, got %d", req.Horizon)
	}
}

// =============================================================================
// HandleTimeSeriesForecast Tests
// =============================================================================

func TestHandleTimeSeriesForecast_InvalidJSON(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/timeseries/forecast", strings.NewReader("{invalid json"))
	c.Request.Header.Set("Content-Type", "application/json")

	handler := HandleTimeSeriesForecast()
	handler(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleTimeSeriesForecast_ProxySuccess(t *testing.T) {
	// Create mock upstream service
	mockResponse := map[string]interface{}{
		"model":    "chronos-t5-tiny",
		"forecast": []float64{105.0, 106.0, 107.0},
		"horizon":  3,
	}
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer mockServer.Close()

	// Set env to point to mock server
	origURL := os.Getenv("ALEUTIAN_TIMESERIES_TOOL")
	origMode := os.Getenv("ALEUTIAN_FORECAST_MODE")
	defer func() {
		os.Setenv("ALEUTIAN_TIMESERIES_TOOL", origURL)
		os.Setenv("ALEUTIAN_FORECAST_MODE", origMode)
	}()
	os.Setenv("ALEUTIAN_TIMESERIES_TOOL", mockServer.URL)
	os.Setenv("ALEUTIAN_FORECAST_MODE", "standalone")

	// Create request
	reqBody := TimeSeriesRequest{
		Model: "chronos-t5-tiny",
		Data:  []float64{100.0, 101.0, 102.0, 103.0, 104.0, 105.0, 106.0, 107.0, 108.0, 109.0},
	}
	reqJSON, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/timeseries/forecast", bytes.NewReader(reqJSON))
	c.Request.Header.Set("Content-Type", "application/json")

	handler := HandleTimeSeriesForecast()
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	// Verify response was proxied
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["model"] != "chronos-t5-tiny" {
		t.Errorf("Expected model in response, got %v", resp)
	}
}

func TestHandleTimeSeriesForecast_ServiceUnavailable(t *testing.T) {
	// Set env to point to non-existent server
	origURL := os.Getenv("ALEUTIAN_TIMESERIES_TOOL")
	origMode := os.Getenv("ALEUTIAN_FORECAST_MODE")
	defer func() {
		os.Setenv("ALEUTIAN_TIMESERIES_TOOL", origURL)
		os.Setenv("ALEUTIAN_FORECAST_MODE", origMode)
	}()
	os.Setenv("ALEUTIAN_TIMESERIES_TOOL", "http://localhost:99999") // Non-existent
	os.Setenv("ALEUTIAN_FORECAST_MODE", "standalone")

	reqBody := TimeSeriesRequest{
		Model: "chronos-t5-tiny",
		Data:  []float64{100.0, 101.0, 102.0},
	}
	reqJSON, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/timeseries/forecast", bytes.NewReader(reqJSON))
	c.Request.Header.Set("Content-Type", "application/json")

	handler := HandleTimeSeriesForecast()
	handler(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

// =============================================================================
// HandleDataFetch Tests
// =============================================================================

func TestHandleDataFetch_MissingEnvVar(t *testing.T) {
	// Save and restore env var
	origURL := os.Getenv("ALEUTIAN_DATA_FETCHER_URL")
	defer os.Setenv("ALEUTIAN_DATA_FETCHER_URL", origURL)
	os.Unsetenv("ALEUTIAN_DATA_FETCHER_URL")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/data/fetch", strings.NewReader(`{"names": ["SPY"]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	handler := HandleDataFetch()
	handler(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.Contains(resp["error"], "not configured") {
		t.Errorf("Expected 'not configured' error, got %v", resp)
	}
}

func TestHandleDataFetch_ProxySuccess(t *testing.T) {
	// Create mock upstream service
	mockResponse := map[string]interface{}{
		"status":  "success",
		"message": "Data fetched",
	}
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request was forwarded correctly
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		if req["names"] == nil {
			t.Error("Expected 'names' in forwarded request")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer mockServer.Close()

	// Set env to point to mock server
	origURL := os.Getenv("ALEUTIAN_DATA_FETCHER_URL")
	defer os.Setenv("ALEUTIAN_DATA_FETCHER_URL", origURL)
	os.Setenv("ALEUTIAN_DATA_FETCHER_URL", mockServer.URL)

	reqBody := `{"names": ["SPY", "QQQ"], "start_date": "2024-01-01"}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/data/fetch", strings.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler := HandleDataFetch()
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "success" {
		t.Errorf("Expected success status, got %v", resp)
	}
}

func TestHandleDataFetch_ServiceUnavailable(t *testing.T) {
	// Set env to point to non-existent server
	origURL := os.Getenv("ALEUTIAN_DATA_FETCHER_URL")
	defer os.Setenv("ALEUTIAN_DATA_FETCHER_URL", origURL)
	os.Setenv("ALEUTIAN_DATA_FETCHER_URL", "http://localhost:99999")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/data/fetch", strings.NewReader(`{"names": ["SPY"]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	handler := HandleDataFetch()
	handler(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestHandleDataFetch_InvalidRequestBody(t *testing.T) {
	origURL := os.Getenv("ALEUTIAN_DATA_FETCHER_URL")
	defer os.Setenv("ALEUTIAN_DATA_FETCHER_URL", origURL)
	os.Setenv("ALEUTIAN_DATA_FETCHER_URL", "http://localhost:8000")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	// Create a request with a body that will fail to read
	c.Request = httptest.NewRequest("POST", "/v1/data/fetch", &errorReader{})
	c.Request.Header.Set("Content-Type", "application/json")

	handler := HandleDataFetch()
	handler(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// errorReader is a reader that always returns an error
type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}
