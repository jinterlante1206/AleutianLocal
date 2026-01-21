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
	"testing"
	"time"

	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Fixtures
// =============================================================================

func validBacktestScenario() *datatypes.BacktestScenario {
	scenario := &datatypes.BacktestScenario{}
	scenario.Metadata.ID = "test-scenario"
	scenario.Metadata.Version = "1.0"
	scenario.Evaluation.Ticker = "SPY"
	scenario.Evaluation.StartDate = "2025-01-01"
	scenario.Evaluation.EndDate = "2025-01-31"
	scenario.Forecast.Model = "amazon/chronos-t5-tiny"
	scenario.Forecast.ContextSize = 252
	scenario.Forecast.HorizonSize = 10
	scenario.Forecast.ComputeMode = ""
	scenario.Trading.InitialCapital = 100000.0
	scenario.Trading.InitialCash = 100000.0
	scenario.Trading.StrategyType = "threshold"
	return scenario
}

func validForecastJob() forecastJob {
	return forecastJob{
		index:       100,
		date:        time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC),
		price:       450.50,
		contextData: []float64{440.0, 442.5, 445.0, 443.0, 447.5, 450.0, 448.5, 451.0, 449.0, 450.50},
	}
}

// =============================================================================
// buildInferenceRequestFromJob Tests
// =============================================================================

func TestBuildInferenceRequestFromJob_Basic(t *testing.T) {
	job := validForecastJob()
	scenario := validBacktestScenario()
	runID := "run-2025-06-15"

	req := buildInferenceRequestFromJob(job, scenario, runID)

	require.NotNil(t, req)
	assert.Contains(t, req.RequestID, runID)
	assert.Contains(t, req.RequestID, "SPY")
	assert.Contains(t, req.RequestID, "20250615")
	assert.Equal(t, "SPY", req.Ticker)
	assert.Equal(t, "amazon/chronos-t5-tiny", req.Model)
	assert.Equal(t, job.contextData, req.Context.Values)
	assert.Equal(t, datatypes.Period1d, req.Context.Period)
	assert.Equal(t, datatypes.SourceInfluxDB, req.Context.Source)
	assert.Equal(t, datatypes.FieldClose, req.Context.Field)
	assert.Equal(t, 10, req.Horizon.Length)
	assert.Equal(t, datatypes.Period1d, req.Horizon.Period)
	assert.Nil(t, req.Params) // No quantiles specified
}

func TestBuildInferenceRequestFromJob_WithQuantiles(t *testing.T) {
	job := validForecastJob()
	scenario := validBacktestScenario()
	scenario.Forecast.Quantiles = []float64{0.1, 0.5, 0.9}
	runID := "run-2025-06-15"

	req := buildInferenceRequestFromJob(job, scenario, runID)

	require.NotNil(t, req)
	require.NotNil(t, req.Params)
	assert.Equal(t, []float64{0.1, 0.5, 0.9}, req.Params.Quantiles)
}

func TestBuildInferenceRequestFromJob_ContextDates(t *testing.T) {
	job := forecastJob{
		index:       100,
		date:        time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC),
		price:       450.50,
		contextData: make([]float64, 10), // 10 days of context
	}
	scenario := validBacktestScenario()
	runID := "run-test"

	req := buildInferenceRequestFromJob(job, scenario, runID)

	// EndDate should be the job date
	assert.Equal(t, "2025-06-15", req.Context.EndDate)

	// StartDate should be 9 days before (10 days total)
	assert.Equal(t, "2025-06-06", req.Context.StartDate)
}

func TestBuildInferenceRequestFromJob_RequestIDFormat(t *testing.T) {
	job := validForecastJob()
	scenario := validBacktestScenario()
	runID := "backtest-abc123"

	req := buildInferenceRequestFromJob(job, scenario, runID)

	// Request ID format: {runID}-{ticker}-{date}-{nanos}
	assert.Regexp(t, `^backtest-abc123-SPY-20250615-\d+$`, req.RequestID)
}

func TestBuildInferenceRequestFromJob_TimestampIsRecent(t *testing.T) {
	job := validForecastJob()
	scenario := validBacktestScenario()
	runID := "run-test"

	before := time.Now().UTC()
	req := buildInferenceRequestFromJob(job, scenario, runID)
	after := time.Now().UTC()

	assert.True(t, req.Timestamp.After(before) || req.Timestamp.Equal(before))
	assert.True(t, req.Timestamp.Before(after) || req.Timestamp.Equal(after))
}

func TestBuildInferenceRequestFromJob_DifferentTickers(t *testing.T) {
	tickers := []string{"AAPL", "BTCUSD", "QQQ", "GLD"}

	for _, ticker := range tickers {
		t.Run(ticker, func(t *testing.T) {
			job := validForecastJob()
			scenario := validBacktestScenario()
			scenario.Evaluation.Ticker = ticker
			runID := "run-test"

			req := buildInferenceRequestFromJob(job, scenario, runID)

			assert.Equal(t, ticker, req.Ticker)
			assert.Contains(t, req.RequestID, ticker)
		})
	}
}

func TestBuildInferenceRequestFromJob_DifferentModels(t *testing.T) {
	models := []string{
		"amazon/chronos-t5-tiny",
		"google/timesfm-2.0-500m-pytorch",
		"salesforce/moirai-1.1-R-base",
	}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			job := validForecastJob()
			scenario := validBacktestScenario()
			scenario.Forecast.Model = model
			runID := "run-test"

			req := buildInferenceRequestFromJob(job, scenario, runID)

			assert.Equal(t, model, req.Model)
		})
	}
}

// =============================================================================
// ForecastOutput Tests
// =============================================================================

func TestForecastOutput_FromLegacyAPI(t *testing.T) {
	// Simulates how legacy API results are converted
	legacyForecast := []float64{101.5, 102.0, 101.8, 103.0, 102.5}

	output := ForecastOutput{
		Values: legacyForecast,
	}

	assert.Equal(t, legacyForecast, output.Values)
	assert.Empty(t, output.RequestID)
	assert.Empty(t, output.ResponseID)
	assert.Nil(t, output.Metadata)
	assert.Nil(t, output.Quantiles)
}

func TestForecastOutput_FromUnifiedAPI(t *testing.T) {
	// Simulates how unified API results are converted
	forecastValues := []float64{101.5, 102.0, 101.8, 103.0, 102.5}
	metadata := &datatypes.InferenceMetadata{
		InferenceTimeMs: 245,
		ModelVersion:    "1.0.0",
		Device:          "cuda:0",
		ModelFamily:     "chronos",
	}
	quantiles := []datatypes.QuantileForecast{
		{Quantile: 0.1, Values: []float64{99.5, 100.0, 99.8}},
		{Quantile: 0.9, Values: []float64{103.5, 104.0, 103.8}},
	}

	output := ForecastOutput{
		Values:     forecastValues,
		RequestID:  "req-12345",
		ResponseID: "resp-67890",
		Metadata:   metadata,
		Quantiles:  quantiles,
	}

	assert.Equal(t, forecastValues, output.Values)
	assert.Equal(t, "req-12345", output.RequestID)
	assert.Equal(t, "resp-67890", output.ResponseID)
	assert.NotNil(t, output.Metadata)
	assert.Equal(t, 245, output.Metadata.InferenceTimeMs)
	assert.Equal(t, "cuda:0", output.Metadata.Device)
	assert.Len(t, output.Quantiles, 2)
}

// =============================================================================
// forecastJob Tests
// =============================================================================

func TestForecastJob_Fields(t *testing.T) {
	job := forecastJob{
		index:       42,
		date:        time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC),
		price:       123.45,
		contextData: []float64{120.0, 121.5, 122.0, 123.0, 123.45},
	}

	assert.Equal(t, 42, job.index)
	assert.Equal(t, time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC), job.date)
	assert.Equal(t, 123.45, job.price)
	assert.Len(t, job.contextData, 5)
}

// =============================================================================
// forecastResult Tests
// =============================================================================

func TestForecastResult_Success(t *testing.T) {
	result := forecastResult{
		index: 42,
		date:  time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC),
		price: 123.45,
		output: ForecastOutput{
			Values:     []float64{124.0, 125.0},
			RequestID:  "req-123",
			ResponseID: "resp-456",
		},
		err: nil,
	}

	assert.Equal(t, 42, result.index)
	assert.Nil(t, result.err)
	assert.Equal(t, []float64{124.0, 125.0}, result.output.Values)
	assert.Equal(t, "req-123", result.output.RequestID)
}

func TestForecastResult_Error(t *testing.T) {
	result := forecastResult{
		index:  42,
		date:   time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC),
		price:  123.45,
		output: ForecastOutput{},
		err:    assert.AnError,
	}

	assert.Equal(t, 42, result.index)
	assert.NotNil(t, result.err)
	assert.Empty(t, result.output.Values)
}

// =============================================================================
// ComputeMode Configuration Tests
// =============================================================================

func TestBacktestScenario_ComputeMode_Default(t *testing.T) {
	scenario := validBacktestScenario()

	// Empty string means legacy mode
	assert.Empty(t, scenario.Forecast.ComputeMode)
}

func TestBacktestScenario_ComputeMode_Legacy(t *testing.T) {
	scenario := validBacktestScenario()
	scenario.Forecast.ComputeMode = "legacy"

	assert.Equal(t, "legacy", scenario.Forecast.ComputeMode)
}

func TestBacktestScenario_ComputeMode_Unified(t *testing.T) {
	scenario := validBacktestScenario()
	scenario.Forecast.ComputeMode = "unified"

	assert.Equal(t, "unified", scenario.Forecast.ComputeMode)
}

func TestBacktestScenario_Quantiles(t *testing.T) {
	scenario := validBacktestScenario()
	scenario.Forecast.Quantiles = []float64{0.1, 0.5, 0.9}

	assert.Equal(t, []float64{0.1, 0.5, 0.9}, scenario.Forecast.Quantiles)
}

// =============================================================================
// EvaluationResult Metadata Fields Tests
// =============================================================================

func TestEvaluationResult_MetadataFields_Empty(t *testing.T) {
	// Legacy mode: metadata fields are empty
	result := datatypes.EvaluationResult{
		Ticker:         "SPY",
		Model:          "amazon/chronos-t5-tiny",
		EvaluationDate: "20250615",
		RunID:          "run-test",
	}

	assert.Empty(t, result.RequestID)
	assert.Empty(t, result.ResponseID)
	assert.Equal(t, 0, result.InferenceTimeMs)
	assert.Empty(t, result.Device)
	assert.Empty(t, result.ModelFamily)
}

func TestEvaluationResult_MetadataFields_Populated(t *testing.T) {
	// Unified mode: metadata fields are populated
	result := datatypes.EvaluationResult{
		Ticker:          "SPY",
		Model:           "amazon/chronos-t5-tiny",
		EvaluationDate:  "20250615",
		RunID:           "run-test",
		RequestID:       "run-test-SPY-20250615-123456",
		ResponseID:      "resp-789012",
		InferenceTimeMs: 245,
		Device:          "cuda:0",
		ModelFamily:     "chronos",
	}

	assert.Equal(t, "run-test-SPY-20250615-123456", result.RequestID)
	assert.Equal(t, "resp-789012", result.ResponseID)
	assert.Equal(t, 245, result.InferenceTimeMs)
	assert.Equal(t, "cuda:0", result.Device)
	assert.Equal(t, "chronos", result.ModelFamily)
}
