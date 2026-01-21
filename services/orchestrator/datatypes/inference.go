// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package datatypes provides type definitions for the Aleutian orchestrator.
//
// This file contains types for the unified inference API that communicates
// with Sapheneia's /orchestration/v1/predict endpoint.
package datatypes

import (
	"fmt"
	"strings"
	"time"

	"github.com/jinterlante1206/AleutianLocal/pkg/validation"
)

// =============================================================================
// ENUMS
// =============================================================================

// Period represents the time frequency of time-series data points.
//
// Description:
//
//	Period defines the interval between consecutive data points in a time series.
//	Common periods include daily (1d), hourly (1h), and various intraday intervals.
//
// Valid Values:
//   - "1m": One minute
//   - "5m": Five minutes
//   - "15m": Fifteen minutes
//   - "30m": Thirty minutes
//   - "1h": One hour
//   - "4h": Four hours
//   - "1d": One day (trading day)
//   - "1w": One week
//   - "1M": One month
//
// Example:
//
//	period := datatypes.Period1d
//	if period == datatypes.Period1d {
//	    log.Println("Daily data")
//	}
//
// Limitations:
//   - Does not support arbitrary intervals (e.g., 7m, 3d)
//   - "1M" represents calendar months, not 30-day periods
//
// Assumptions:
//   - Trading days exclude weekends and holidays
//   - Intraday periods assume continuous trading (24h for crypto)
type Period string

const (
	Period1m  Period = "1m"
	Period5m  Period = "5m"
	Period15m Period = "15m"
	Period30m Period = "30m"
	Period1h  Period = "1h"
	Period4h  Period = "4h"
	Period1d  Period = "1d"
	Period1w  Period = "1w"
	Period1M  Period = "1M"
)

// validPeriods contains all valid Period values for validation.
var validPeriods = map[Period]bool{
	Period1m:  true,
	Period5m:  true,
	Period15m: true,
	Period30m: true,
	Period1h:  true,
	Period4h:  true,
	Period1d:  true,
	Period1w:  true,
	Period1M:  true,
}

// IsValid checks if the Period is a valid value.
//
// Description:
//
//	IsValid returns true if the Period is one of the defined constants.
//
// Inputs:
//
//	None (operates on receiver)
//
// Outputs:
//   - bool: true if valid, false otherwise
//
// Example:
//
//	p := Period("invalid")
//	if !p.IsValid() {
//	    log.Println("Invalid period")
//	}
func (p Period) IsValid() bool {
	return validPeriods[p]
}

// DataSource indicates the origin of time-series data.
//
// Description:
//
//	DataSource tracks where historical data was obtained from, enabling
//	data quality assessment and reproducibility.
//
// Valid Values:
//   - "yahoo": Yahoo Finance API
//   - "influxdb": Local InfluxDB storage
//   - "alpaca": Alpaca Markets API
//   - "binance": Binance exchange API
//   - "polygon": Polygon.io API
//   - "coinbase": Coinbase exchange API
//   - "kraken": Kraken exchange API
//   - "synthetic": Generated/simulated data
//   - "unknown": Source not specified
//
// Example:
//
//	context := ContextData{
//	    Source: datatypes.SourceInfluxDB,
//	}
//
// Limitations:
//   - Cannot express hybrid sources (data from multiple providers)
//   - New providers require code changes to add constants
//
// Assumptions:
//   - Each data point comes from a single source
//   - Source is set by the data fetching layer, not the user
type DataSource string

const (
	SourceYahoo     DataSource = "yahoo"
	SourceInfluxDB  DataSource = "influxdb"
	SourceAlpaca    DataSource = "alpaca"
	SourceBinance   DataSource = "binance"
	SourcePolygon   DataSource = "polygon"  // Polygon.io API
	SourceCoinbase  DataSource = "coinbase" // Coinbase exchange API
	SourceKraken    DataSource = "kraken"   // Kraken exchange API
	SourceSynthetic DataSource = "synthetic"
	SourceUnknown   DataSource = "unknown"
)

// DataField indicates which OHLCV field the values represent.
//
// Description:
//
//	DataField specifies which component of price data is being used.
//	Time-series forecasting typically uses adjusted close prices.
//
// Valid Values:
//   - "open": Opening price
//   - "high": Highest price in period
//   - "low": Lowest price in period
//   - "close": Closing price
//   - "adj_close": Split/dividend-adjusted close
//   - "volume": Trading volume
//
// Example:
//
//	context := ContextData{
//	    Field: datatypes.FieldAdjClose,
//	}
//
// Limitations:
//   - Does not support derived fields (e.g., VWAP, returns)
//   - Cannot represent multi-field inputs
//
// Assumptions:
//   - Forecasting models expect single-field input
//   - adj_close is preferred for equity forecasting
type DataField string

const (
	FieldOpen     DataField = "open"
	FieldHigh     DataField = "high"
	FieldLow      DataField = "low"
	FieldClose    DataField = "close"
	FieldAdjClose DataField = "adj_close"
	FieldVolume   DataField = "volume"
)

// =============================================================================
// REQUEST STRUCTURES
// =============================================================================

// ContextData holds historical time-series data with full provenance.
//
// Description:
//
//	ContextData encapsulates the input context window for a forecast,
//	including the actual values and metadata about their origin.
//
// Fields:
//   - Values: The time-series values (most recent last)
//   - Period: Time interval between points
//   - Source: Where the data came from
//   - StartDate: First date in the series (YYYY-MM-DD)
//   - EndDate: Last date in the series (YYYY-MM-DD)
//   - Field: Which OHLCV field the values represent
//
// JSON Tags:
//
//	All fields are serialized with snake_case names.
//
// Example:
//
//	context := ContextData{
//	    Values:    []float64{100.0, 101.5, 99.8, 102.0},
//	    Period:    Period1d,
//	    Source:    SourceInfluxDB,
//	    StartDate: "2025-01-01",
//	    EndDate:   "2025-01-04",
//	    Field:     FieldClose,
//	}
//
// Limitations:
//   - Values must be in chronological order
//   - Does not support missing values (NaN handling)
//   - Date format must be YYYY-MM-DD
//
// Assumptions:
//   - len(Values) matches the number of periods between dates
//   - All values are valid (no NaN or Inf)
type ContextData struct {
	Values    []float64  `json:"values"`
	Period    Period     `json:"period"`
	Source    DataSource `json:"source"`
	StartDate string     `json:"start_date"`
	EndDate   string     `json:"end_date"`
	Field     DataField  `json:"field"`
}

// HorizonSpec defines the forecast horizon parameters.
//
// Description:
//
//	HorizonSpec specifies how far into the future the model should predict.
//
// Fields:
//   - Length: Number of periods to forecast
//   - Period: Time interval for forecast points
//
// Example:
//
//	horizon := HorizonSpec{
//	    Length: 10,
//	    Period: Period1d,
//	}
//
// Limitations:
//   - Maximum horizon depends on the model
//   - Forecast period should match context period
//
// Assumptions:
//   - Length > 0
//   - Period matches the context period (models don't interpolate)
type HorizonSpec struct {
	Length int    `json:"length"`
	Period Period `json:"period"`
}

// ModelParams holds model-specific inference parameters.
//
// Description:
//
//	ModelParams contains optional tuning parameters that affect how
//	the model generates predictions. Not all models support all parameters.
//
// Fields:
//   - NumSamples: Number of samples for probabilistic models (must be > 0, server default: 20)
//   - Temperature: Sampling temperature (must be > 0, server default: 1.0)
//   - TopK: Top-K sampling parameter
//   - TopP: Top-P (nucleus) sampling parameter
//   - Quantiles: Which quantiles to return (e.g., [0.1, 0.5, 0.9])
//
// Example:
//
//	// With explicit parameters
//	params := &ModelParams{
//	    NumSamples:  100,
//	    Temperature: 0.8,
//	    Quantiles:   []float64{0.1, 0.5, 0.9},
//	}
//
//	// For server defaults, use nil Params in InferenceRequest
//	req := InferenceRequest{Params: nil} // Sapheneia uses num_samples=20, temperature=1.0
//
// Limitations:
//   - Not all models respect all parameters
//   - Chronos models ignore temperature
//   - Zero values are NOT allowed (use nil Params for server defaults)
//
// Assumptions:
//   - NumSamples > 0 (required by Sapheneia gt=0 constraint)
//   - Temperature > 0 (required by Sapheneia gt=0 constraint)
//   - Quantiles are in range [0, 1]
type ModelParams struct {
	NumSamples  int       `json:"num_samples,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	TopK        int       `json:"top_k,omitempty"`
	TopP        float64   `json:"top_p,omitempty"`
	Quantiles   []float64 `json:"quantiles,omitempty"`
}

// InferenceRequest is the unified request for all forecasting models.
//
// Description:
//
//	InferenceRequest contains all information needed to generate a forecast,
//	including the input context, model selection, and inference parameters.
//	The RequestID enables end-to-end request tracing.
//
// Fields:
//   - RequestID: UUID for tracing (generated by caller)
//   - Timestamp: When the request was created
//   - Ticker: Asset symbol (e.g., "SPY", "BTCUSD")
//   - Model: Model identifier (e.g., "amazon/chronos-t5-tiny")
//   - Context: Historical data and metadata
//   - Horizon: Forecast horizon specification
//   - Params: Optional model parameters (nil for defaults)
//
// Example:
//
//	req := InferenceRequest{
//	    RequestID: uuid.New().String(),
//	    Timestamp: time.Now().UTC(),
//	    Ticker:    "SPY",
//	    Model:     "amazon/chronos-t5-tiny",
//	    Context: ContextData{
//	        Values:    closeHistory,
//	        Period:    Period1d,
//	        Source:    SourceInfluxDB,
//	        StartDate: "2025-01-01",
//	        EndDate:   "2025-12-31",
//	        Field:     FieldClose,
//	    },
//	    Horizon: HorizonSpec{
//	        Length: 10,
//	        Period: Period1d,
//	    },
//	    Params: &ModelParams{
//	        NumSamples: 20,
//	    },
//	}
//
// Limitations:
//   - Ticker validation is the caller's responsibility
//   - Model name must match Sapheneia's supported models
//   - Context.Values cannot be empty
//
// Assumptions:
//   - RequestID is unique per request
//   - Timestamp is in UTC
//   - Context.Period matches Horizon.Period
type InferenceRequest struct {
	RequestID string    `json:"request_id"`
	Timestamp time.Time `json:"timestamp"`

	Ticker  string       `json:"ticker"`
	Model   string       `json:"model"`
	Context ContextData  `json:"context"`
	Horizon HorizonSpec  `json:"horizon"`
	Params  *ModelParams `json:"params,omitempty"`
}

// Validate checks that the InferenceRequest is valid before sending.
//
// Description:
//
//	Validate performs runtime validation of the request, checking that
//	all required fields are present and have valid values. Should be
//	called before sending the request to avoid unnecessary network calls.
//
// Inputs:
//
//	None (operates on receiver)
//
// Outputs:
//   - error: Non-nil if validation fails, with descriptive message
//
// Validations Performed:
//   - RequestID is non-empty
//   - Ticker is non-empty and passes ticker validation
//   - Model is non-empty and contains "/" (full name format)
//   - Context.Values is non-empty
//   - Context.Period is valid
//   - Context.StartDate and EndDate are non-empty
//   - Horizon.Length > 0
//   - Horizon.Period is valid
//   - If Params != nil, NumSamples >= 0, Temperature > 0 (if set)
//   - If Params.Quantiles provided, all values in [0, 1]
//
// Example:
//
//	req := &InferenceRequest{
//	    RequestID: "",  // Invalid!
//	}
//	if err := req.Validate(); err != nil {
//	    log.Fatalf("Invalid request: %v", err)
//	}
//
// Limitations:
//   - Does not validate that dates are parseable
//   - Does not validate model exists in Sapheneia
//
// Assumptions:
//   - Full model names contain "/" (e.g., "amazon/chronos-t5-tiny")
func (r *InferenceRequest) Validate() error {
	if r.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}

	if r.Ticker == "" {
		return fmt.Errorf("ticker is required")
	}
	if err := validation.ValidateTicker(r.Ticker); err != nil {
		return fmt.Errorf("invalid ticker: %w", err)
	}

	if r.Model == "" {
		return fmt.Errorf("model is required")
	}
	if !strings.Contains(r.Model, "/") {
		return fmt.Errorf("model must be full name with vendor prefix (e.g., 'amazon/chronos-t5-tiny'), got: %s", r.Model)
	}

	if len(r.Context.Values) == 0 {
		return fmt.Errorf("context.values cannot be empty")
	}
	if !r.Context.Period.IsValid() {
		return fmt.Errorf("context.period is invalid: %s", r.Context.Period)
	}
	if r.Context.StartDate == "" {
		return fmt.Errorf("context.start_date is required")
	}
	if r.Context.EndDate == "" {
		return fmt.Errorf("context.end_date is required")
	}

	if r.Horizon.Length <= 0 {
		return fmt.Errorf("horizon.length must be positive, got: %d", r.Horizon.Length)
	}
	if !r.Horizon.Period.IsValid() {
		return fmt.Errorf("horizon.period is invalid: %s", r.Horizon.Period)
	}

	if r.Params != nil {
		// NumSamples: if set (non-zero), must be positive.
		// Zero is allowed because omitempty will omit it and Sapheneia uses its default.
		// Negative values are rejected as invalid.
		if r.Params.NumSamples < 0 {
			return fmt.Errorf("params.num_samples must be non-negative, got: %d", r.Params.NumSamples)
		}
		// Temperature: if set (non-zero), must be positive.
		// Zero is allowed because omitempty will omit it and Sapheneia uses its default.
		// Negative values are rejected as invalid.
		if r.Params.Temperature < 0 {
			return fmt.Errorf("params.temperature must be non-negative, got: %f", r.Params.Temperature)
		}
		for i, q := range r.Params.Quantiles {
			if q < 0 || q > 1 {
				return fmt.Errorf("params.quantiles[%d] must be in [0, 1], got: %f", i, q)
			}
		}
	}

	return nil
}

// =============================================================================
// RESPONSE STRUCTURES
// =============================================================================

// ForecastData holds the forecast output.
//
// Description:
//
//	ForecastData contains the predicted values and their temporal metadata.
//
// Fields:
//   - Values: Predicted values (chronological order)
//   - Period: Time interval between predictions
//   - StartDate: First forecast date (YYYY-MM-DD)
//   - EndDate: Last forecast date (YYYY-MM-DD)
//
// Example:
//
//	forecast := ForecastData{
//	    Values:    []float64{101.5, 102.0, 101.8},
//	    Period:    Period1d,
//	    StartDate: "2026-01-01",
//	    EndDate:   "2026-01-03",
//	}
//
// Limitations:
//   - Values are point estimates (median of distribution)
//   - Does not include confidence intervals (see Quantiles)
//
// Assumptions:
//   - len(Values) == horizon.Length from request
type ForecastData struct {
	Values    []float64 `json:"values"`
	Period    Period    `json:"period"`
	StartDate string    `json:"start_date"`
	EndDate   string    `json:"end_date"`
}

// ContextSummary echoes back what context was used.
//
// Description:
//
//	ContextSummary confirms the context that was actually used for the
//	forecast, which may differ from the request if truncation occurred.
//
// Fields:
//   - Length: Number of context points used
//   - Period: Time interval of context
//   - Source: Data source
//   - StartDate: First context date
//   - EndDate: Last context date
//   - Field: OHLCV field used
//
// Example:
//
//	summary := ContextSummary{
//	    Length:    252,
//	    Period:    Period1d,
//	    Source:    SourceInfluxDB,
//	    StartDate: "2025-01-01",
//	    EndDate:   "2025-12-31",
//	    Field:     FieldClose,
//	}
//
// Limitations:
//   - Does not include the actual values (to save bandwidth)
//
// Assumptions:
//   - Matches the request context unless truncation occurred
type ContextSummary struct {
	Length    int        `json:"length"`
	Period    Period     `json:"period"`
	Source    DataSource `json:"source"`
	StartDate string     `json:"start_date"`
	EndDate   string     `json:"end_date"`
	Field     DataField  `json:"field"`
}

// InferenceMetadata provides execution details.
//
// Description:
//
//	InferenceMetadata contains information about how the inference was
//	performed, useful for debugging and performance monitoring.
//
// Fields:
//   - InferenceTimeMs: Time taken for model inference in milliseconds
//   - ModelVersion: Specific model version used
//   - Device: Compute device (cpu, cuda:0, mps)
//   - ModelFamily: Model family (chronos, timesfm, moirai)
//
// Example:
//
//	metadata := InferenceMetadata{
//	    InferenceTimeMs: 245,
//	    ModelVersion:    "1.0.0",
//	    Device:          "cpu",
//	    ModelFamily:     "chronos",
//	}
//
// Assumptions:
//   - InferenceTimeMs does not include network latency
type InferenceMetadata struct {
	InferenceTimeMs int    `json:"inference_time_ms"`
	ModelVersion    string `json:"model_version,omitempty"`
	Device          string `json:"device,omitempty"`
	ModelFamily     string `json:"model_family,omitempty"`
}

// QuantileForecast holds a single quantile forecast.
//
// Description:
//
//	QuantileForecast contains the predicted values at a specific quantile
//	level, enabling uncertainty estimation.
//
// Fields:
//   - Quantile: The quantile level (0.0 to 1.0)
//   - Values: Predicted values at this quantile
//
// Example:
//
//	q90 := QuantileForecast{
//	    Quantile: 0.9,
//	    Values:   []float64{103.0, 104.5, 103.8},
//	}
//
// Limitations:
//   - Quantile must be in [0, 1]
//
// Assumptions:
//   - len(Values) matches the forecast horizon
type QuantileForecast struct {
	Quantile float64   `json:"quantile"`
	Values   []float64 `json:"values"`
}

// InferenceResponse is the unified response from all forecasting models.
//
// Description:
//
//	InferenceResponse contains the forecast results along with tracing IDs
//	and metadata. The ResponseID links back to the RequestID for tracing.
//
// Fields:
//   - RequestID: Echoed from the request
//   - ResponseID: UUID generated by the inference service
//   - Timestamp: When the response was created
//   - Ticker: Echoed from request
//   - Model: Echoed from request
//   - Forecast: Point forecast results
//   - ContextSummary: Context that was used
//   - Quantiles: Optional quantile forecasts
//   - Metadata: Execution metadata
//
// Example:
//
//	resp := InferenceResponse{
//	    RequestID:  "req-123",
//	    ResponseID: "resp-456",
//	    Timestamp:  time.Now().UTC(),
//	    Ticker:     "SPY",
//	    Model:      "amazon/chronos-t5-tiny",
//	    Forecast: ForecastData{
//	        Values:    []float64{101.5, 102.0},
//	        Period:    Period1d,
//	        StartDate: "2026-01-01",
//	        EndDate:   "2026-01-02",
//	    },
//	    ContextSummary: ContextSummary{
//	        Length:    252,
//	        Period:    Period1d,
//	        Source:    SourceInfluxDB,
//	        StartDate: "2025-01-01",
//	        EndDate:   "2025-12-31",
//	        Field:     FieldClose,
//	    },
//	    Quantiles: []QuantileForecast{
//	        {Quantile: 0.1, Values: []float64{99.5, 100.0}},
//	        {Quantile: 0.9, Values: []float64{103.5, 104.0}},
//	    },
//	    Metadata: InferenceMetadata{
//	        InferenceTimeMs: 245,
//	        Device:          "cpu",
//	    },
//	}
//
// Limitations:
//   - Quantiles may be empty if not requested or model doesn't support
//
// Assumptions:
//   - RequestID matches the request's RequestID
//   - ResponseID is unique per response
type InferenceResponse struct {
	RequestID  string    `json:"request_id"`
	ResponseID string    `json:"response_id"`
	Timestamp  time.Time `json:"timestamp"`

	Ticker string `json:"ticker"`
	Model  string `json:"model"`

	Forecast       ForecastData       `json:"forecast"`
	ContextSummary ContextSummary     `json:"context_summary"`
	Quantiles      []QuantileForecast `json:"quantiles,omitempty"`
	Metadata       InferenceMetadata  `json:"metadata"`
}
