// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package datatypes

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Period Tests
// =============================================================================

func TestPeriod_IsValid_AllConstants(t *testing.T) {
	validPeriods := []Period{
		Period1m, Period5m, Period15m, Period30m,
		Period1h, Period4h, Period1d, Period1w, Period1M,
	}

	for _, p := range validPeriods {
		t.Run(string(p), func(t *testing.T) {
			assert.True(t, p.IsValid(), "Period %s should be valid", p)
		})
	}
}

func TestPeriod_IsValid_InvalidValues(t *testing.T) {
	invalidPeriods := []Period{
		"",
		"invalid",
		"2d",
		"1D", // Case sensitive
		"1hr",
		"daily",
	}

	for _, p := range invalidPeriods {
		t.Run(string(p), func(t *testing.T) {
			assert.False(t, p.IsValid(), "Period %s should be invalid", p)
		})
	}
}

// =============================================================================
// DataSource Tests
// =============================================================================

func TestDataSource_AllConstants_Serialize(t *testing.T) {
	// Verify all DataSource constants serialize to expected JSON values
	testCases := []struct {
		source   DataSource
		expected string
	}{
		{SourceYahoo, "yahoo"},
		{SourceInfluxDB, "influxdb"},
		{SourceAlpaca, "alpaca"},
		{SourceBinance, "binance"},
		{SourcePolygon, "polygon"},
		{SourceCoinbase, "coinbase"},
		{SourceKraken, "kraken"},
		{SourceSynthetic, "synthetic"},
		{SourceUnknown, "unknown"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, string(tc.source))
		})
	}
}

// =============================================================================
// JSON Round-Trip Tests
// =============================================================================

func TestInferenceRequest_JSON_RoundTrip(t *testing.T) {
	original := InferenceRequest{
		RequestID: "test-uuid-123",
		Timestamp: time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC),
		Ticker:    "SPY",
		Model:     "amazon/chronos-t5-tiny",
		Context: ContextData{
			Values:    []float64{100.0, 101.5, 99.8, 102.0},
			Period:    Period1d,
			Source:    SourceInfluxDB,
			StartDate: "2025-01-01",
			EndDate:   "2025-01-04",
			Field:     FieldClose,
		},
		Horizon: HorizonSpec{
			Length: 10,
			Period: Period1d,
		},
		Params: &ModelParams{
			NumSamples:  20,
			Temperature: 1.0,
			Quantiles:   []float64{0.1, 0.5, 0.9},
		},
	}

	// Serialize
	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Deserialize
	var restored InferenceRequest
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	// Verify all fields
	assert.Equal(t, original.RequestID, restored.RequestID)
	assert.Equal(t, original.Timestamp.UTC(), restored.Timestamp.UTC())
	assert.Equal(t, original.Ticker, restored.Ticker)
	assert.Equal(t, original.Model, restored.Model)
	assert.Equal(t, original.Context.Values, restored.Context.Values)
	assert.Equal(t, original.Context.Period, restored.Context.Period)
	assert.Equal(t, original.Context.Source, restored.Context.Source)
	assert.Equal(t, original.Context.StartDate, restored.Context.StartDate)
	assert.Equal(t, original.Context.EndDate, restored.Context.EndDate)
	assert.Equal(t, original.Context.Field, restored.Context.Field)
	assert.Equal(t, original.Horizon.Length, restored.Horizon.Length)
	assert.Equal(t, original.Horizon.Period, restored.Horizon.Period)
	require.NotNil(t, restored.Params)
	assert.Equal(t, original.Params.NumSamples, restored.Params.NumSamples)
	assert.Equal(t, original.Params.Temperature, restored.Params.Temperature)
	assert.Equal(t, original.Params.Quantiles, restored.Params.Quantiles)
}

func TestInferenceRequest_JSON_RoundTrip_NilParams(t *testing.T) {
	original := InferenceRequest{
		RequestID: "test-uuid-456",
		Timestamp: time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC),
		Ticker:    "AAPL",
		Model:     "google/timesfm-2.0-500m-pytorch",
		Context: ContextData{
			Values:    []float64{150.0, 151.0},
			Period:    Period1d,
			Source:    SourceYahoo,
			StartDate: "2025-12-01",
			EndDate:   "2025-12-02",
			Field:     FieldAdjClose,
		},
		Horizon: HorizonSpec{
			Length: 5,
			Period: Period1d,
		},
		Params: nil,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Verify params is omitted
	var raw map[string]interface{}
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)
	_, hasParams := raw["params"]
	assert.False(t, hasParams, "params should be omitted when nil")

	// Round trip
	var restored InferenceRequest
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)
	assert.Nil(t, restored.Params)
}

func TestInferenceResponse_JSON_RoundTrip(t *testing.T) {
	original := InferenceResponse{
		RequestID:  "req-123",
		ResponseID: "resp-456",
		Timestamp:  time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC),
		Ticker:     "SPY",
		Model:      "amazon/chronos-t5-tiny",
		Forecast: ForecastData{
			Values:    []float64{101.5, 102.0, 101.8},
			Period:    Period1d,
			StartDate: "2026-01-21",
			EndDate:   "2026-01-23",
		},
		ContextSummary: ContextSummary{
			Length:    252,
			Period:    Period1d,
			Source:    SourceInfluxDB,
			StartDate: "2025-01-01",
			EndDate:   "2025-12-31",
			Field:     FieldClose,
		},
		Quantiles: []QuantileForecast{
			{Quantile: 0.1, Values: []float64{99.5, 100.0, 99.8}},
			{Quantile: 0.5, Values: []float64{101.5, 102.0, 101.8}},
			{Quantile: 0.9, Values: []float64{103.5, 104.0, 103.8}},
		},
		Metadata: InferenceMetadata{
			InferenceTimeMs: 245,
			ModelVersion:    "1.0.0",
			Device:          "cpu",
			ModelFamily:     "chronos",
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored InferenceResponse
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, original.RequestID, restored.RequestID)
	assert.Equal(t, original.ResponseID, restored.ResponseID)
	assert.Equal(t, original.Ticker, restored.Ticker)
	assert.Equal(t, original.Model, restored.Model)
	assert.Equal(t, original.Forecast.Values, restored.Forecast.Values)
	assert.Equal(t, original.Forecast.Period, restored.Forecast.Period)
	assert.Equal(t, original.ContextSummary.Length, restored.ContextSummary.Length)
	assert.Len(t, restored.Quantiles, 3)
	assert.Equal(t, original.Metadata.InferenceTimeMs, restored.Metadata.InferenceTimeMs)
}

func TestInferenceResponse_JSON_RoundTrip_EmptyQuantiles(t *testing.T) {
	original := InferenceResponse{
		RequestID:  "req-789",
		ResponseID: "resp-012",
		Timestamp:  time.Now().UTC(),
		Ticker:     "QQQ",
		Model:      "ibm/granite-ttm-r1",
		Forecast: ForecastData{
			Values:    []float64{400.0, 401.0},
			Period:    Period1d,
			StartDate: "2026-01-21",
			EndDate:   "2026-01-22",
		},
		ContextSummary: ContextSummary{
			Length: 100,
			Period: Period1d,
			Source: SourceInfluxDB,
		},
		Quantiles: nil,
		Metadata: InferenceMetadata{
			InferenceTimeMs: 150,
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Verify quantiles is omitted
	var raw map[string]interface{}
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)
	_, hasQuantiles := raw["quantiles"]
	assert.False(t, hasQuantiles, "quantiles should be omitted when nil")
}

// =============================================================================
// Validation Tests
// =============================================================================

func TestInferenceRequest_Validate_Success(t *testing.T) {
	req := validInferenceRequest()
	err := req.Validate()
	assert.NoError(t, err)
}

func TestInferenceRequest_Validate_EmptyRequestID(t *testing.T) {
	req := validInferenceRequest()
	req.RequestID = ""

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request_id")
}

func TestInferenceRequest_Validate_EmptyTicker(t *testing.T) {
	req := validInferenceRequest()
	req.Ticker = ""

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ticker")
}

func TestInferenceRequest_Validate_InvalidTicker(t *testing.T) {
	req := validInferenceRequest()
	req.Ticker = "INVALID<>TICKER"

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ticker")
}

func TestInferenceRequest_Validate_EmptyModel(t *testing.T) {
	req := validInferenceRequest()
	req.Model = ""

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model")
}

func TestInferenceRequest_Validate_ModelWithoutSlash(t *testing.T) {
	req := validInferenceRequest()
	req.Model = "chronos-t5-tiny" // Missing vendor prefix

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "full name")
	assert.Contains(t, err.Error(), "vendor prefix")
}

func TestInferenceRequest_Validate_EmptyContextValues(t *testing.T) {
	req := validInferenceRequest()
	req.Context.Values = []float64{}

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context.values")
}

func TestInferenceRequest_Validate_NilContextValues(t *testing.T) {
	req := validInferenceRequest()
	req.Context.Values = nil

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context.values")
}

func TestInferenceRequest_Validate_InvalidContextPeriod(t *testing.T) {
	req := validInferenceRequest()
	req.Context.Period = "invalid"

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context.period")
}

func TestInferenceRequest_Validate_EmptyStartDate(t *testing.T) {
	req := validInferenceRequest()
	req.Context.StartDate = ""

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start_date")
}

func TestInferenceRequest_Validate_EmptyEndDate(t *testing.T) {
	req := validInferenceRequest()
	req.Context.EndDate = ""

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "end_date")
}

func TestInferenceRequest_Validate_ZeroHorizonLength(t *testing.T) {
	req := validInferenceRequest()
	req.Horizon.Length = 0

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "horizon.length")
}

func TestInferenceRequest_Validate_NegativeHorizonLength(t *testing.T) {
	req := validInferenceRequest()
	req.Horizon.Length = -5

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "horizon.length")
}

func TestInferenceRequest_Validate_InvalidHorizonPeriod(t *testing.T) {
	req := validInferenceRequest()
	req.Horizon.Period = "2d"

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "horizon.period")
}

func TestInferenceRequest_Validate_NegativeNumSamples(t *testing.T) {
	req := validInferenceRequest()
	req.Params = &ModelParams{
		NumSamples:  -1,  // Invalid
		Temperature: 1.0, // Valid
	}

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "num_samples")
}

func TestInferenceRequest_Validate_NegativeTemperature(t *testing.T) {
	req := validInferenceRequest()
	req.Params = &ModelParams{
		NumSamples:  20,   // Valid
		Temperature: -0.5, // Invalid
	}

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "temperature")
}

func TestInferenceRequest_Validate_QuantileBelowZero(t *testing.T) {
	req := validInferenceRequest()
	req.Params = &ModelParams{
		NumSamples:  20,  // Optional positive value
		Temperature: 1.0, // Optional positive value
		Quantiles:   []float64{-0.1, 0.5, 0.9},
	}

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quantiles")
}

func TestInferenceRequest_Validate_QuantileAboveOne(t *testing.T) {
	req := validInferenceRequest()
	req.Params = &ModelParams{
		NumSamples:  20,  // Optional positive value
		Temperature: 1.0, // Optional positive value
		Quantiles:   []float64{0.1, 0.5, 1.5},
	}

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "quantiles")
}

func TestInferenceRequest_Validate_ZeroTemperatureAllowed(t *testing.T) {
	// Temperature = 0 is allowed because omitempty will omit it from JSON.
	// Sapheneia will use its default (1.0) when the field is absent.
	req := validInferenceRequest()
	req.Params = &ModelParams{
		NumSamples:  20, // Valid
		Temperature: 0,  // Zero is allowed - will be omitted via omitempty
	}

	err := req.Validate()
	assert.NoError(t, err)
}

func TestInferenceRequest_Validate_ZeroNumSamplesAllowed(t *testing.T) {
	// NumSamples = 0 is allowed because omitempty will omit it from JSON.
	// Sapheneia will use its default (20) when the field is absent.
	req := validInferenceRequest()
	req.Params = &ModelParams{
		NumSamples:  0,   // Zero is allowed - will be omitted via omitempty
		Temperature: 1.0, // Valid
	}

	err := req.Validate()
	assert.NoError(t, err)
}

func TestInferenceRequest_Validate_NilParamsAllowed(t *testing.T) {
	// nil Params is valid - server will use defaults (num_samples=20, temperature=1.0)
	req := validInferenceRequest()
	req.Params = nil

	err := req.Validate()
	assert.NoError(t, err)
}

func TestInferenceRequest_Validate_BoundaryQuantiles(t *testing.T) {
	// Quantiles exactly at 0 and 1 should be valid
	req := validInferenceRequest()
	req.Params = &ModelParams{
		NumSamples:  20,  // Optional - zero allowed (omitted via omitempty)
		Temperature: 1.0, // Optional - zero allowed (omitted via omitempty)
		Quantiles:   []float64{0.0, 0.5, 1.0},
	}

	err := req.Validate()
	assert.NoError(t, err)
}

// =============================================================================
// Test Helpers
// =============================================================================

func validInferenceRequest() *InferenceRequest {
	return &InferenceRequest{
		RequestID: "test-uuid-valid",
		Timestamp: time.Now().UTC(),
		Ticker:    "SPY",
		Model:     "amazon/chronos-t5-tiny",
		Context: ContextData{
			Values:    []float64{100.0, 101.5, 99.8, 102.0},
			Period:    Period1d,
			Source:    SourceInfluxDB,
			StartDate: "2025-01-01",
			EndDate:   "2025-01-04",
			Field:     FieldClose,
		},
		Horizon: HorizonSpec{
			Length: 10,
			Period: Period1d,
		},
		Params: nil,
	}
}
