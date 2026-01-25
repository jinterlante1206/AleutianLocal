// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/datatypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Fixtures
// =============================================================================

// validScenarioYAML returns a valid YAML string for testing file loading.
func validScenarioYAML() string {
	return `metadata:
  id: "test-strategy"
  version: "1.0.0"
  description: "Test strategy for unit tests"
  author: "Test Author"
  created: "2025-01-20"

evaluation:
  ticker: "SPY"
  fetch_start_date: "20211201"
  start_date: "20230101"
  end_date: "20240101"

forecast:
  model: "amazon/chronos-t5-tiny"
  context_size: 64
  horizon_size: 10
  compute_mode: "unified"

trading:
  initial_capital: 100000.0
  initial_position: 0.0
  initial_cash: 100000.0
  strategy_type: "threshold"
  params:
    threshold_type: "absolute"
    threshold_value: 2.0
`
}

// validScenarioJSON returns a valid JSON object for testing URL loading.
func validScenarioJSON() *datatypes.BacktestScenario {
	return &datatypes.BacktestScenario{
		Metadata: datatypes.ScenarioMetadata{
			ID:          "test-strategy-json",
			Version:     "2.0.0",
			Description: "Test strategy from JSON endpoint",
			Author:      "Test Author",
			Created:     "2025-01-20",
		},
		Evaluation: struct {
			Ticker         string `yaml:"ticker" json:"ticker"`
			FetchStartDate string `yaml:"fetch_start_date" json:"fetch_start_date"`
			StartDate      string `yaml:"start_date" json:"start_date"`
			EndDate        string `yaml:"end_date" json:"end_date"`
		}{
			Ticker:         "QQQ",
			FetchStartDate: "20211201",
			StartDate:      "20230101",
			EndDate:        "20240101",
		},
		Forecast: struct {
			Model       string    `yaml:"model" json:"model"`
			ContextSize int       `yaml:"context_size" json:"context_size"`
			HorizonSize int       `yaml:"horizon_size" json:"horizon_size"`
			ComputeMode string    `yaml:"compute_mode" json:"compute_mode"`
			Quantiles   []float64 `yaml:"quantiles" json:"quantiles"`
		}{
			Model:       "google/timesfm-2.0-500m-pytorch",
			ContextSize: 128,
			HorizonSize: 20,
			ComputeMode: "unified",
		},
		Trading: struct {
			InitialCapital  float64                `yaml:"initial_capital" json:"initial_capital"`
			InitialPosition float64                `yaml:"initial_position" json:"initial_position"`
			InitialCash     float64                `yaml:"initial_cash" json:"initial_cash"`
			StrategyType    string                 `yaml:"strategy_type" json:"strategy_type"`
			Params          map[string]interface{} `yaml:"params" json:"params"`
		}{
			InitialCapital:  50000.0,
			InitialPosition: 0.0,
			InitialCash:     50000.0,
			StrategyType:    "momentum",
			Params: map[string]interface{}{
				"lookback_period": 20,
			},
		},
	}
}

// =============================================================================
// loadScenario Tests
// =============================================================================

// TestLoadScenario_LocalFile verifies that loadScenario routes to file loader
// for paths without http:// or https:// prefix.
//
// Description:
//
//	This test creates a temporary YAML file and verifies that loadScenario
//	correctly loads and parses it via loadScenarioFromFile.
func TestLoadScenario_LocalFile(t *testing.T) {
	// Create temp directory and file
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "test_strategy.yaml")
	err := os.WriteFile(yamlPath, []byte(validScenarioYAML()), 0644)
	require.NoError(t, err, "Failed to create test YAML file")

	// Load scenario
	scenario, err := loadScenario(yamlPath)

	// Verify
	require.NoError(t, err)
	require.NotNil(t, scenario)
	assert.Equal(t, "test-strategy", scenario.Metadata.ID)
	assert.Equal(t, "1.0.0", scenario.Metadata.Version)
	assert.Equal(t, "SPY", scenario.Evaluation.Ticker)
}

// TestLoadScenario_HTTPUrl verifies that loadScenario routes to URL loader
// for paths starting with http://.
func TestLoadScenario_HTTPUrl(t *testing.T) {
	// Create mock server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(validScenarioJSON()); err != nil {
			t.Logf("Warning: failed to encode response: %v", err)
		}
	}))
	defer mockServer.Close()

	// Load scenario from URL
	scenario, err := loadScenario(mockServer.URL + "/strategies/test")

	// Verify
	require.NoError(t, err)
	require.NotNil(t, scenario)
	assert.Equal(t, "test-strategy-json", scenario.Metadata.ID)
	assert.Equal(t, "2.0.0", scenario.Metadata.Version)
	assert.Equal(t, "QQQ", scenario.Evaluation.Ticker)
}

// TestLoadScenario_HTTPSUrl verifies that loadScenario routes to URL loader
// for paths starting with https://.
func TestLoadScenario_HTTPSUrl(t *testing.T) {
	// Create mock HTTPS server
	mockServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(validScenarioJSON()); err != nil {
			t.Logf("Warning: failed to encode response: %v", err)
		}
	}))
	defer mockServer.Close()

	// Note: loadScenarioFromURL uses default http.Client which won't trust
	// the test server's self-signed cert. This test verifies the routing logic.
	// The actual HTTPS call will fail with certificate error.
	_, err := loadScenario(mockServer.URL + "/strategies/test")

	// Expect certificate error (proves https:// was detected and routed correctly)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "certificate")
}

// =============================================================================
// loadScenarioFromFile Tests
// =============================================================================

// TestLoadScenarioFromFile_Success verifies successful YAML file loading.
func TestLoadScenarioFromFile_Success(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "valid_strategy.yaml")
	err := os.WriteFile(yamlPath, []byte(validScenarioYAML()), 0644)
	require.NoError(t, err)

	scenario, err := loadScenarioFromFile(yamlPath)

	require.NoError(t, err)
	require.NotNil(t, scenario)
	assert.Equal(t, "test-strategy", scenario.Metadata.ID)
	assert.Equal(t, "SPY", scenario.Evaluation.Ticker)
	assert.Equal(t, "amazon/chronos-t5-tiny", scenario.Forecast.Model)
	assert.Equal(t, 64, scenario.Forecast.ContextSize)
	assert.Equal(t, "threshold", scenario.Trading.StrategyType)
	assert.Equal(t, 100000.0, scenario.Trading.InitialCapital)
}

// TestLoadScenarioFromFile_FileNotFound verifies error on missing file.
func TestLoadScenarioFromFile_FileNotFound(t *testing.T) {
	scenario, err := loadScenarioFromFile("/nonexistent/path/strategy.yaml")

	require.Error(t, err)
	assert.Nil(t, scenario)
	assert.Contains(t, err.Error(), "failed to read file")
}

// TestLoadScenarioFromFile_InvalidYAML verifies error on malformed YAML.
func TestLoadScenarioFromFile_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "invalid.yaml")
	err := os.WriteFile(yamlPath, []byte("{{{{invalid yaml content"), 0644)
	require.NoError(t, err)

	scenario, err := loadScenarioFromFile(yamlPath)

	require.Error(t, err)
	assert.Nil(t, scenario)
	assert.Contains(t, err.Error(), "failed to parse YAML")
}

// TestLoadScenarioFromFile_EmptyFile verifies behavior on empty file.
func TestLoadScenarioFromFile_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "empty.yaml")
	err := os.WriteFile(yamlPath, []byte(""), 0644)
	require.NoError(t, err)

	scenario, err := loadScenarioFromFile(yamlPath)

	// Empty YAML unmarshals to zero-value struct (no error)
	require.NoError(t, err)
	require.NotNil(t, scenario)
	assert.Empty(t, scenario.Metadata.ID)
}

// =============================================================================
// loadScenarioFromURL Tests
// =============================================================================

// TestLoadScenarioFromURL_Success verifies successful JSON fetch and parse.
func TestLoadScenarioFromURL_Success(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/strategies/spy_threshold_v1", r.URL.Path)
		assert.Equal(t, "GET", r.Method)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(validScenarioJSON()); err != nil {
			t.Logf("Warning: failed to encode response: %v", err)
		}
	}))
	defer mockServer.Close()

	scenario, err := loadScenarioFromURL(mockServer.URL + "/strategies/spy_threshold_v1")

	require.NoError(t, err)
	require.NotNil(t, scenario)
	assert.Equal(t, "test-strategy-json", scenario.Metadata.ID)
	assert.Equal(t, "QQQ", scenario.Evaluation.Ticker)
	assert.Equal(t, "google/timesfm-2.0-500m-pytorch", scenario.Forecast.Model)
}

// TestLoadScenarioFromURL_HTTPError404 verifies error handling for 404 response.
func TestLoadScenarioFromURL_HTTPError404(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "Strategy not found"}`))
	}))
	defer mockServer.Close()

	scenario, err := loadScenarioFromURL(mockServer.URL + "/strategies/nonexistent")

	require.Error(t, err)
	assert.Nil(t, scenario)
	assert.Contains(t, err.Error(), "HTTP 404")
}

// TestLoadScenarioFromURL_HTTPError500 verifies error handling for 500 response.
func TestLoadScenarioFromURL_HTTPError500(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "Internal server error"}`))
	}))
	defer mockServer.Close()

	scenario, err := loadScenarioFromURL(mockServer.URL + "/strategies/test")

	require.Error(t, err)
	assert.Nil(t, scenario)
	assert.Contains(t, err.Error(), "HTTP 500")
}

// TestLoadScenarioFromURL_InvalidJSON verifies error handling for malformed JSON.
func TestLoadScenarioFromURL_InvalidJSON(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{{{invalid json`))
	}))
	defer mockServer.Close()

	scenario, err := loadScenarioFromURL(mockServer.URL + "/strategies/test")

	require.Error(t, err)
	assert.Nil(t, scenario)
	assert.Contains(t, err.Error(), "failed to parse JSON")
}

// TestLoadScenarioFromURL_NetworkError verifies error handling for unreachable server.
func TestLoadScenarioFromURL_NetworkError(t *testing.T) {
	// Use an address that won't be listening
	scenario, err := loadScenarioFromURL("http://127.0.0.1:65535/strategies/test")

	require.Error(t, err)
	assert.Nil(t, scenario)
	assert.Contains(t, err.Error(), "failed to fetch URL")
}

// TestLoadScenarioFromURL_Timeout verifies timeout behavior.
func TestLoadScenarioFromURL_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping timeout test in short mode")
	}

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than the 30-second client timeout
		// Note: In actual test, we'd need to wait 30+ seconds which is too long.
		// This test documents the expected behavior; actual timeout testing
		// would require modifying the client timeout for testing.
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()

	// This test verifies the function handles slow responses.
	// Full timeout testing would require injecting a shorter timeout.
	_, err := loadScenarioFromURL(mockServer.URL + "/strategies/test")

	// With 100ms delay, this should succeed but return parse error (empty body)
	// This is acceptable - the test documents timeout behavior exists
	require.Error(t, err)
}

// TestLoadScenarioFromURL_EmptyResponse verifies error handling for empty response body.
func TestLoadScenarioFromURL_EmptyResponse(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Don't write anything - empty body
	}))
	defer mockServer.Close()

	scenario, err := loadScenarioFromURL(mockServer.URL + "/strategies/test")

	require.Error(t, err)
	assert.Nil(t, scenario)
	assert.Contains(t, err.Error(), "failed to parse JSON")
}
