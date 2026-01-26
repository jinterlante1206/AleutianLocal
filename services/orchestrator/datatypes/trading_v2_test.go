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
// InferenceRef Tests
// =============================================================================

func TestInferenceRef_IsSet_BothPopulated(t *testing.T) {
	ref := InferenceRef{
		RequestID:  "req-123",
		ResponseID: "resp-456",
	}

	assert.True(t, ref.IsSet())
}

func TestInferenceRef_IsSet_EmptyRequestID(t *testing.T) {
	ref := InferenceRef{
		RequestID:  "",
		ResponseID: "resp-456",
	}

	assert.False(t, ref.IsSet())
}

func TestInferenceRef_IsSet_EmptyResponseID(t *testing.T) {
	ref := InferenceRef{
		RequestID:  "req-123",
		ResponseID: "",
	}

	assert.False(t, ref.IsSet())
}

func TestInferenceRef_IsSet_BothEmpty(t *testing.T) {
	ref := InferenceRef{}

	assert.False(t, ref.IsSet())
}

// =============================================================================
// TradingSignalRequestV2 Constructor Tests
// =============================================================================

func TestNewTradingSignalRequestV2_GeneratesUUID(t *testing.T) {
	req := NewTradingSignalRequestV2(
		"SPY",
		"threshold",
		PriceInfo{Current: 450.0, Forecast: 455.0, Period: Period1d, Source: SourceInfluxDB},
		PortfolioState{Position: 0, Cash: 100000, InitialCapital: 100000},
		map[string]interface{}{"threshold_type": "absolute"},
		InferenceRef{},
	)

	assert.NotEmpty(t, req.RequestID)
	assert.Len(t, req.RequestID, 36) // UUID format
	assert.False(t, req.Timestamp.IsZero())
}

func TestNewTradingSignalRequestV2_SetsAllFields(t *testing.T) {
	prices := PriceInfo{
		Current:  450.0,
		Forecast: 455.0,
		Period:   Period1d,
		Source:   SourceInfluxDB,
		AsOfDate: "2026-01-20",
	}
	portfolio := PortfolioState{
		Position:       100,
		Cash:           50000,
		InitialCapital: 100000,
	}
	params := map[string]interface{}{
		"threshold_type":  "absolute",
		"threshold_value": 2.0,
		"execution_size":  10.0,
	}
	inferenceRef := InferenceRef{
		RequestID:  "inf-req-123",
		ResponseID: "inf-resp-456",
	}

	req := NewTradingSignalRequestV2("SPY", "threshold", prices, portfolio, params, inferenceRef)

	assert.Equal(t, "SPY", req.Ticker)
	assert.Equal(t, "threshold", req.StrategyType)
	assert.Equal(t, 450.0, req.Prices.Current)
	assert.Equal(t, 455.0, req.Prices.Forecast)
	assert.Equal(t, Period1d, req.Prices.Period)
	assert.Equal(t, SourceInfluxDB, req.Prices.Source)
	assert.Equal(t, 100.0, req.Portfolio.Position)
	assert.Equal(t, 50000.0, req.Portfolio.Cash)
	assert.Equal(t, 100000.0, req.Portfolio.InitialCapital)
	assert.Equal(t, "absolute", req.StrategyParams["threshold_type"])
	assert.True(t, req.HasInferenceRef())
}

// =============================================================================
// Interface Implementation Tests
// =============================================================================

func TestTradingSignalRequestV2_ImplementsInterface(t *testing.T) {
	var _ TradingSignalRequester = (*TradingSignalRequestV2)(nil)
}

func TestTradingSignalRequestV2_GetTicker(t *testing.T) {
	req := &TradingSignalRequestV2{Ticker: "QQQ"}
	assert.Equal(t, "QQQ", req.GetTicker())
}

func TestTradingSignalRequestV2_GetStrategyType(t *testing.T) {
	req := &TradingSignalRequestV2{StrategyType: "return"}
	assert.Equal(t, "return", req.GetStrategyType())
}

func TestTradingSignalRequestV2_GetForecastPrice(t *testing.T) {
	req := &TradingSignalRequestV2{Prices: PriceInfo{Forecast: 500.0}}
	assert.Equal(t, 500.0, req.GetForecastPrice())
}

func TestTradingSignalRequestV2_GetCurrentPrice(t *testing.T) {
	req := &TradingSignalRequestV2{Prices: PriceInfo{Current: 495.0}}
	assert.Equal(t, 495.0, req.GetCurrentPrice())
}

func TestTradingSignalRequestV2_GetCurrentPosition(t *testing.T) {
	req := &TradingSignalRequestV2{Portfolio: PortfolioState{Position: 50}}
	assert.Equal(t, 50.0, req.GetCurrentPosition())
}

func TestTradingSignalRequestV2_GetAvailableCash(t *testing.T) {
	req := &TradingSignalRequestV2{Portfolio: PortfolioState{Cash: 75000}}
	assert.Equal(t, 75000.0, req.GetAvailableCash())
}

func TestTradingSignalRequestV2_GetInitialCapital(t *testing.T) {
	req := &TradingSignalRequestV2{Portfolio: PortfolioState{InitialCapital: 100000}}
	assert.Equal(t, 100000.0, req.GetInitialCapital())
}

func TestTradingSignalRequestV2_GetStrategyParams(t *testing.T) {
	params := map[string]interface{}{"key": "value"}
	req := &TradingSignalRequestV2{StrategyParams: params}
	assert.Equal(t, params, req.GetStrategyParams())
}

// =============================================================================
// V1 Interface Implementation Tests
// =============================================================================

func TestTradingSignalRequest_ImplementsInterface(t *testing.T) {
	var _ TradingSignalRequester = (*TradingSignalRequest)(nil)
}

func TestTradingSignalRequest_GetCurrentPrice_Nil(t *testing.T) {
	req := &TradingSignalRequest{CurrentPrice: nil}
	assert.Equal(t, 0.0, req.GetCurrentPrice())
}

func TestTradingSignalRequest_GetCurrentPrice_Set(t *testing.T) {
	price := 500.0
	req := &TradingSignalRequest{CurrentPrice: &price}
	assert.Equal(t, 500.0, req.GetCurrentPrice())
}

// =============================================================================
// ToV1 Conversion Tests
// =============================================================================

func TestTradingSignalRequestV2_ToV1(t *testing.T) {
	v2Req := &TradingSignalRequestV2{
		RequestID:    "v2-req-123",
		Timestamp:    time.Now().UTC(),
		Ticker:       "SPY",
		StrategyType: "threshold",
		Prices: PriceInfo{
			Current:  450.0,
			Forecast: 455.0,
			Period:   Period1d,
			Source:   SourceInfluxDB,
		},
		Portfolio: PortfolioState{
			Position:       100,
			Cash:           50000,
			InitialCapital: 100000,
		},
		StrategyParams: map[string]interface{}{
			"threshold_type":  "absolute",
			"threshold_value": 2.0,
		},
		InferenceRef: InferenceRef{
			RequestID:  "inf-req",
			ResponseID: "inf-resp",
		},
	}

	v1Req := v2Req.ToV1()

	assert.Equal(t, "SPY", v1Req.Ticker)
	assert.Equal(t, 455.0, v1Req.ForecastPrice)
	require.NotNil(t, v1Req.CurrentPrice)
	assert.Equal(t, 450.0, *v1Req.CurrentPrice)
	assert.Equal(t, 100.0, v1Req.CurrentPosition)
	assert.Equal(t, 50000.0, v1Req.AvailableCash)
	assert.Equal(t, 100000.0, v1Req.InitialCapital)
	assert.Equal(t, "threshold", v1Req.StrategyType)
	assert.Equal(t, "absolute", v1Req.StrategyParams["threshold_type"])
}

// =============================================================================
// Validation Tests
// =============================================================================

func TestTradingSignalRequestV2_Validate_Success(t *testing.T) {
	req := validTradingSignalRequestV2()
	err := req.Validate()
	assert.NoError(t, err)
}

func TestTradingSignalRequestV2_Validate_EmptyRequestID(t *testing.T) {
	req := validTradingSignalRequestV2()
	req.RequestID = ""

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request_id")
}

func TestTradingSignalRequestV2_Validate_EmptyTicker(t *testing.T) {
	req := validTradingSignalRequestV2()
	req.Ticker = ""

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ticker")
}

func TestTradingSignalRequestV2_Validate_EmptyStrategyType(t *testing.T) {
	req := validTradingSignalRequestV2()
	req.StrategyType = ""

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strategy_type")
}

func TestTradingSignalRequestV2_Validate_InvalidStrategyType(t *testing.T) {
	req := validTradingSignalRequestV2()
	req.StrategyType = "invalid"

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strategy_type")
	assert.Contains(t, err.Error(), "threshold")
}

func TestTradingSignalRequestV2_Validate_AllStrategyTypes(t *testing.T) {
	validTypes := []string{"threshold", "return", "quantile"}

	for _, stratType := range validTypes {
		t.Run(stratType, func(t *testing.T) {
			req := validTradingSignalRequestV2()
			req.StrategyType = stratType

			err := req.Validate()
			assert.NoError(t, err)
		})
	}
}

func TestTradingSignalRequestV2_Validate_ZeroCurrentPrice(t *testing.T) {
	req := validTradingSignalRequestV2()
	req.Prices.Current = 0

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prices.current")
}

func TestTradingSignalRequestV2_Validate_NegativeCurrentPrice(t *testing.T) {
	req := validTradingSignalRequestV2()
	req.Prices.Current = -100

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prices.current")
}

func TestTradingSignalRequestV2_Validate_ZeroForecastPrice(t *testing.T) {
	req := validTradingSignalRequestV2()
	req.Prices.Forecast = 0

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prices.forecast")
}

func TestTradingSignalRequestV2_Validate_NegativePosition(t *testing.T) {
	req := validTradingSignalRequestV2()
	req.Portfolio.Position = -10

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "portfolio.position")
}

func TestTradingSignalRequestV2_Validate_ZeroPositionAllowed(t *testing.T) {
	req := validTradingSignalRequestV2()
	req.Portfolio.Position = 0

	err := req.Validate()
	assert.NoError(t, err)
}

func TestTradingSignalRequestV2_Validate_NegativeCash(t *testing.T) {
	req := validTradingSignalRequestV2()
	req.Portfolio.Cash = -1000

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "portfolio.cash")
}

func TestTradingSignalRequestV2_Validate_ZeroCashAllowed(t *testing.T) {
	req := validTradingSignalRequestV2()
	req.Portfolio.Cash = 0

	err := req.Validate()
	assert.NoError(t, err)
}

func TestTradingSignalRequestV2_Validate_ZeroInitialCapital(t *testing.T) {
	req := validTradingSignalRequestV2()
	req.Portfolio.InitialCapital = 0

	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "portfolio.initial_capital")
}

// =============================================================================
// JSON Serialization Tests
// =============================================================================

func TestTradingSignalRequestV2_JSON_RoundTrip(t *testing.T) {
	original := &TradingSignalRequestV2{
		RequestID:    "test-uuid-123",
		Timestamp:    time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC),
		Ticker:       "SPY",
		StrategyType: "threshold",
		Prices: PriceInfo{
			Current:  450.0,
			Forecast: 455.0,
			Period:   Period1d,
			Source:   SourceInfluxDB,
			AsOfDate: "2026-01-20",
		},
		Portfolio: PortfolioState{
			Position:       100,
			Cash:           50000,
			InitialCapital: 100000,
		},
		StrategyParams: map[string]interface{}{
			"threshold_type":  "absolute",
			"threshold_value": 2.0,
		},
		InferenceRef: InferenceRef{
			RequestID:  "inf-req-123",
			ResponseID: "inf-resp-456",
		},
	}

	// Serialize
	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Deserialize
	var restored TradingSignalRequestV2
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	// Verify all fields
	assert.Equal(t, original.RequestID, restored.RequestID)
	assert.Equal(t, original.Timestamp.UTC(), restored.Timestamp.UTC())
	assert.Equal(t, original.Ticker, restored.Ticker)
	assert.Equal(t, original.StrategyType, restored.StrategyType)
	assert.Equal(t, original.Prices.Current, restored.Prices.Current)
	assert.Equal(t, original.Prices.Forecast, restored.Prices.Forecast)
	assert.Equal(t, original.Prices.Period, restored.Prices.Period)
	assert.Equal(t, original.Prices.Source, restored.Prices.Source)
	assert.Equal(t, original.Portfolio.Position, restored.Portfolio.Position)
	assert.Equal(t, original.Portfolio.Cash, restored.Portfolio.Cash)
	assert.Equal(t, original.Portfolio.InitialCapital, restored.Portfolio.InitialCapital)
	assert.Equal(t, original.InferenceRef.RequestID, restored.InferenceRef.RequestID)
	assert.Equal(t, original.InferenceRef.ResponseID, restored.InferenceRef.ResponseID)
}

func TestPriceInfo_JSON_RoundTrip(t *testing.T) {
	original := PriceInfo{
		Current:  450.0,
		Forecast: 455.0,
		Period:   Period1d,
		Source:   SourcePolygon, // Test new DataSource
		AsOfDate: "2026-01-20",
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored PriceInfo
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, original, restored)
}

func TestPortfolioState_JSON_RoundTrip(t *testing.T) {
	original := PortfolioState{
		Position:       100,
		Cash:           50000,
		InitialCapital: 100000,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored PortfolioState
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, original, restored)
}

func TestInferenceRef_JSON_RoundTrip(t *testing.T) {
	original := InferenceRef{
		RequestID:  "req-123",
		ResponseID: "resp-456",
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored InferenceRef
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, original, restored)
}

// =============================================================================
// Polymorphism Tests
// =============================================================================

func TestTradingSignalRequester_Polymorphism(t *testing.T) {
	// Create both V1 and V2 requests
	currentPrice := 450.0
	v1Req := &TradingSignalRequest{
		Ticker:          "SPY",
		ForecastPrice:   455.0,
		CurrentPrice:    &currentPrice,
		CurrentPosition: 100,
		AvailableCash:   50000,
		InitialCapital:  100000,
		StrategyType:    "threshold",
		StrategyParams:  map[string]interface{}{"threshold_type": "absolute"},
	}

	v2Req := &TradingSignalRequestV2{
		Ticker:       "SPY",
		StrategyType: "threshold",
		Prices: PriceInfo{
			Current:  450.0,
			Forecast: 455.0,
		},
		Portfolio: PortfolioState{
			Position:       100,
			Cash:           50000,
			InitialCapital: 100000,
		},
		StrategyParams: map[string]interface{}{"threshold_type": "absolute"},
	}

	// Test polymorphic handling
	requests := []TradingSignalRequester{v1Req, v2Req}

	for _, req := range requests {
		assert.Equal(t, "SPY", req.GetTicker())
		assert.Equal(t, "threshold", req.GetStrategyType())
		assert.Equal(t, 455.0, req.GetForecastPrice())
		assert.Equal(t, 450.0, req.GetCurrentPrice())
		assert.Equal(t, 100.0, req.GetCurrentPosition())
		assert.Equal(t, 50000.0, req.GetAvailableCash())
		assert.Equal(t, 100000.0, req.GetInitialCapital())
	}
}

// =============================================================================
// Test Helpers
// =============================================================================

func validTradingSignalRequestV2() *TradingSignalRequestV2 {
	return &TradingSignalRequestV2{
		RequestID:    "test-request-id",
		Timestamp:    time.Now().UTC(),
		Ticker:       "SPY",
		StrategyType: "threshold",
		Prices: PriceInfo{
			Current:  450.0,
			Forecast: 455.0,
			Period:   Period1d,
			Source:   SourceInfluxDB,
			AsOfDate: "2026-01-20",
		},
		Portfolio: PortfolioState{
			Position:       100,
			Cash:           50000,
			InitialCapital: 100000,
		},
		StrategyParams: map[string]interface{}{
			"threshold_type":  "absolute",
			"threshold_value": 2.0,
			"execution_size":  10.0,
		},
		InferenceRef: InferenceRef{
			RequestID:  "inf-req-123",
			ResponseID: "inf-resp-456",
		},
	}
}
