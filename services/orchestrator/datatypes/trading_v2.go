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
// This file contains version 2 trading signal types that include full inference
// traceability via request/response ID linking. These types align with the
// backtest scenario YAML structure (strategies/*.yaml) and enable audit trails
// from forecast to trade.
package datatypes

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// =============================================================================
// INTERFACES
// =============================================================================

// TradingSignalRequester defines the contract for trading signal requests.
//
// Description:
//
//	TradingSignalRequester provides a common interface for both legacy (V1)
//	and V2 trading signal requests, enabling polymorphic handling in the
//	evaluator without knowing which version is being used.
//
// Implementations:
//   - TradingSignalRequest (V1, legacy - no tracing)
//   - TradingSignalRequestV2 (V2, new - with inference tracing)
//
// Example:
//
//	func processSignal(req TradingSignalRequester) {
//	    ticker := req.GetTicker()
//	    forecast := req.GetForecastPrice()
//	    // Process regardless of V1 or V2
//	}
//
// Limitations:
//   - Does not expose all fields (use type assertion for V2-specific fields)
//   - GetCurrentPrice may return nil for V1 if not set
//
// Assumptions:
//   - All implementations provide valid, non-empty ticker
//   - Prices are positive values
type TradingSignalRequester interface {
	GetTicker() string
	GetStrategyType() string
	GetForecastPrice() float64
	GetCurrentPrice() float64
	GetCurrentPosition() float64
	GetAvailableCash() float64
	GetInitialCapital() float64
	GetStrategyParams() map[string]interface{}
}

// =============================================================================
// STRUCTS - Supporting Types
// =============================================================================

// InferenceRef links a trading signal to its originating inference request.
//
// Description:
//
//	InferenceRef provides traceability from trading decisions back to the
//	forecast that informed them. This enables audit trails, debugging of
//	the full decision pipeline, and compliance reporting.
//
// Fields:
//   - RequestID: The inference request UUID (from InferenceRequest.RequestID)
//   - ResponseID: The inference response UUID (from InferenceResponse.ResponseID)
//
// Example:
//
//	// After calling inference service
//	ref := InferenceRef{
//	    RequestID:  inferenceReq.RequestID,
//	    ResponseID: inferenceResp.ResponseID,
//	}
//
// Limitations:
//   - Both IDs must be valid UUIDs from a prior inference call
//   - Empty IDs indicate legacy mode (no tracing available)
//   - Does not validate that the referenced inference exists
//
// Assumptions:
//   - The referenced inference has already completed successfully
//   - IDs are set by the caller after receiving inference response
type InferenceRef struct {
	RequestID  string `json:"request_id"`
	ResponseID string `json:"response_id"`
}

// IsSet returns true if both inference IDs are populated.
//
// Description:
//
//	IsSet checks whether this reference has valid inference tracing IDs.
//	A reference is considered set only when both RequestID and ResponseID
//	are non-empty strings.
//
// Inputs:
//
//	None (operates on receiver)
//
// Outputs:
//   - bool: true if both IDs are non-empty, false otherwise
//
// Example:
//
//	if ref.IsSet() {
//	    slog.Info("Linked to inference", "req_id", ref.RequestID)
//	}
//
// Limitations:
//   - Does not validate UUID format
//   - Does not verify IDs exist in any external system
//
// Assumptions:
//   - Empty string means "not set"
func (r *InferenceRef) IsSet() bool {
	return r.RequestID != "" && r.ResponseID != ""
}

// PriceInfo holds current and forecast prices with metadata.
//
// Description:
//
//	PriceInfo encapsulates price data used for trading decisions, including
//	provenance information for audit purposes. This structure aligns with
//	the inference response format.
//
// Fields:
//   - Current: The current market price at decision time
//   - Forecast: The model's predicted price (first value from forecast)
//   - Period: Time period of the forecast (e.g., Period1d)
//   - Source: Origin of the price data (e.g., SourceInfluxDB)
//   - AsOfDate: Reference date for the prices (YYYY-MM-DD format)
//
// Example:
//
//	prices := PriceInfo{
//	    Current:   450.00,
//	    Forecast:  455.00,
//	    Period:    Period1d,
//	    Source:    SourceInfluxDB,
//	    AsOfDate:  "2026-01-20",
//	}
//
// Limitations:
//   - Does not include bid/ask spread
//   - Single price point, not full OHLCV
//   - Does not include confidence intervals
//
// Assumptions:
//   - Prices are in the same currency
//   - Current price is from the same source as historical data
//   - Forecast is the median/mean prediction (quantile 0.5)
type PriceInfo struct {
	Current  float64    `json:"current"`
	Forecast float64    `json:"forecast"`
	Period   Period     `json:"period"`
	Source   DataSource `json:"source"`
	AsOfDate string     `json:"as_of_date"`
}

// PortfolioState captures the current portfolio position.
//
// Description:
//
//	PortfolioState provides a snapshot of the portfolio at decision time,
//	used by trading strategies to determine position sizing and available
//	capital. This maps directly to the trading section in strategy YAML.
//
// Fields:
//   - Position: Current number of shares/units held (from scenario.Trading.InitialPosition)
//   - Cash: Available cash for trading (from scenario.Trading.InitialCash)
//   - InitialCapital: Starting capital for P&L calculation (from scenario.Trading.InitialCapital)
//
// Example:
//
//	// From strategy YAML trading section
//	state := PortfolioState{
//	    Position:       scenario.Trading.InitialPosition,
//	    Cash:           scenario.Trading.InitialCash,
//	    InitialCapital: scenario.Trading.InitialCapital,
//	}
//
// Limitations:
//   - Single asset portfolio only
//   - Does not track unrealized P&L
//   - Does not include margin or leverage
//
// Assumptions:
//   - Position is non-negative (long-only strategies per Sapheneia)
//   - Cash is non-negative
//   - InitialCapital > 0
type PortfolioState struct {
	Position       float64 `json:"position"`
	Cash           float64 `json:"cash"`
	InitialCapital float64 `json:"initial_capital"`
}

// =============================================================================
// STRUCTS - Main Request Type
// =============================================================================

// TradingSignalRequestV2 is the new trading signal request with full traceability.
//
// Description:
//
//	TradingSignalRequestV2 extends the legacy V1 request with inference tracing
//	and structured price/portfolio data. This enables full audit trails from
//	forecast to trade, which is required for compliance and debugging.
//
//	The structure aligns with the backtest scenario YAML format:
//	  - Prices.Forecast comes from inference response
//	  - Portfolio maps to scenario.Trading section
//	  - StrategyType/Params map to scenario.Trading.strategy_type/params
//
// Fields:
//   - RequestID: Unique ID for this trading request (UUID)
//   - Timestamp: When the request was created (UTC)
//   - Ticker: Asset symbol (from scenario.Evaluation.ticker)
//   - StrategyType: Trading strategy (from scenario.Trading.strategy_type)
//   - Prices: Current and forecast price info
//   - Portfolio: Current portfolio state
//   - StrategyParams: Strategy-specific parameters (from scenario.Trading.params)
//   - InferenceRef: Link to originating inference request/response
//
// Example:
//
//	// Building V2 request after inference
//	req := TradingSignalRequestV2{
//	    RequestID:    uuid.New().String(),
//	    Timestamp:    time.Now().UTC(),
//	    Ticker:       scenario.Evaluation.Ticker,
//	    StrategyType: scenario.Trading.StrategyType,
//	    Prices: PriceInfo{
//	        Current:  currentPrice,
//	        Forecast: inferenceResp.Forecast.Values[0],
//	        Period:   Period1d,
//	        Source:   SourceInfluxDB,
//	        AsOfDate: currentDate,
//	    },
//	    Portfolio: PortfolioState{
//	        Position:       currentPosition,
//	        Cash:           availableCash,
//	        InitialCapital: scenario.Trading.InitialCapital,
//	    },
//	    StrategyParams: scenario.Trading.Params,
//	    InferenceRef: InferenceRef{
//	        RequestID:  inferenceReq.RequestID,
//	        ResponseID: inferenceResp.ResponseID,
//	    },
//	}
//
// Limitations:
//   - Requires prior inference call for InferenceRef
//   - Sapheneia trading service doesn't support V2 yet (must convert to V1)
//   - Not backwards compatible with V1 trading service
//
// Assumptions:
//   - InferenceRef is set when using unified API mode
//   - Trading service supports the strategy type specified
//   - StrategyParams contains all required parameters for the strategy
type TradingSignalRequestV2 struct {
	RequestID string    `json:"request_id"`
	Timestamp time.Time `json:"timestamp"`

	Ticker       string `json:"ticker"`
	StrategyType string `json:"strategy_type"`

	Prices    PriceInfo      `json:"prices"`
	Portfolio PortfolioState `json:"portfolio"`

	StrategyParams map[string]interface{} `json:"strategy_params"`

	InferenceRef InferenceRef `json:"inference_ref"`
}

// =============================================================================
// CONSTRUCTORS
// =============================================================================

// NewTradingSignalRequestV2 creates a new V2 request with generated UUID.
//
// Description:
//
//	NewTradingSignalRequestV2 is a convenience constructor that generates
//	a new request ID and sets the timestamp. Use this when building a
//	V2 request from backtest scenario data.
//
// Inputs:
//   - ticker: Asset symbol (e.g., "SPY")
//   - strategyType: Strategy name (e.g., "threshold", "return", "quantile")
//   - prices: Price information with current and forecast
//   - portfolio: Current portfolio state
//   - params: Strategy-specific parameters from YAML
//   - inferenceRef: Link to originating inference (can be empty)
//
// Outputs:
//   - *TradingSignalRequestV2: Fully constructed request ready for use
//
// Example:
//
//	req := NewTradingSignalRequestV2(
//	    "SPY",
//	    "threshold",
//	    PriceInfo{Current: 450, Forecast: 455, Period: Period1d, Source: SourceInfluxDB},
//	    PortfolioState{Position: 0, Cash: 100000, InitialCapital: 100000},
//	    map[string]interface{}{"threshold_type": "absolute", "threshold_value": 2.0},
//	    InferenceRef{RequestID: req.RequestID, ResponseID: resp.ResponseID},
//	)
//
// Limitations:
//   - Does not validate inputs (caller must ensure validity)
//
// Assumptions:
//   - UUID generation does not fail
//   - Caller provides valid strategy parameters
func NewTradingSignalRequestV2(
	ticker string,
	strategyType string,
	prices PriceInfo,
	portfolio PortfolioState,
	params map[string]interface{},
	inferenceRef InferenceRef,
) *TradingSignalRequestV2 {
	return &TradingSignalRequestV2{
		RequestID:      uuid.New().String(),
		Timestamp:      time.Now().UTC(),
		Ticker:         ticker,
		StrategyType:   strategyType,
		Prices:         prices,
		Portfolio:      portfolio,
		StrategyParams: params,
		InferenceRef:   inferenceRef,
	}
}

// =============================================================================
// METHODS - Interface Implementation
// =============================================================================

// GetTicker returns the asset ticker symbol.
//
// Description:
//
//	GetTicker implements TradingSignalRequester interface.
//
// Inputs:
//
//	None (operates on receiver)
//
// Outputs:
//   - string: The ticker symbol (e.g., "SPY")
//
// Example:
//
//	ticker := req.GetTicker() // "SPY"
//
// Limitations:
//
//	None
//
// Assumptions:
//   - Ticker was set during construction
func (r *TradingSignalRequestV2) GetTicker() string {
	return r.Ticker
}

// GetStrategyType returns the trading strategy type.
//
// Description:
//
//	GetStrategyType implements TradingSignalRequester interface.
//	Returns the strategy type as specified in the YAML (threshold, return, quantile).
//
// Inputs:
//
//	None (operates on receiver)
//
// Outputs:
//   - string: The strategy type (e.g., "threshold")
//
// Example:
//
//	stratType := req.GetStrategyType() // "threshold"
//
// Limitations:
//
//	None
//
// Assumptions:
//   - StrategyType matches one of the supported Sapheneia strategies
func (r *TradingSignalRequestV2) GetStrategyType() string {
	return r.StrategyType
}

// GetForecastPrice returns the model's forecast price.
//
// Description:
//
//	GetForecastPrice implements TradingSignalRequester interface.
//	Returns the predicted price from the inference response.
//
// Inputs:
//
//	None (operates on receiver)
//
// Outputs:
//   - float64: The forecast price
//
// Example:
//
//	forecast := req.GetForecastPrice() // 455.00
//
// Limitations:
//
//	None
//
// Assumptions:
//   - Forecast price is positive
func (r *TradingSignalRequestV2) GetForecastPrice() float64 {
	return r.Prices.Forecast
}

// GetCurrentPrice returns the current market price.
//
// Description:
//
//	GetCurrentPrice implements TradingSignalRequester interface.
//	Returns the current price at decision time.
//
// Inputs:
//
//	None (operates on receiver)
//
// Outputs:
//   - float64: The current price
//
// Example:
//
//	current := req.GetCurrentPrice() // 450.00
//
// Limitations:
//
//	None
//
// Assumptions:
//   - Current price is positive
func (r *TradingSignalRequestV2) GetCurrentPrice() float64 {
	return r.Prices.Current
}

// GetCurrentPosition returns the current position size.
//
// Description:
//
//	GetCurrentPosition implements TradingSignalRequester interface.
//	Returns the number of shares/units currently held.
//
// Inputs:
//
//	None (operates on receiver)
//
// Outputs:
//   - float64: The current position (shares/units)
//
// Example:
//
//	position := req.GetCurrentPosition() // 100.0
//
// Limitations:
//
//	None
//
// Assumptions:
//   - Position is non-negative (long-only)
func (r *TradingSignalRequestV2) GetCurrentPosition() float64 {
	return r.Portfolio.Position
}

// GetAvailableCash returns the available cash for trading.
//
// Description:
//
//	GetAvailableCash implements TradingSignalRequester interface.
//	Returns the cash available for new purchases.
//
// Inputs:
//
//	None (operates on receiver)
//
// Outputs:
//   - float64: The available cash
//
// Example:
//
//	cash := req.GetAvailableCash() // 50000.00
//
// Limitations:
//
//	None
//
// Assumptions:
//   - Cash is non-negative
func (r *TradingSignalRequestV2) GetAvailableCash() float64 {
	return r.Portfolio.Cash
}

// GetInitialCapital returns the initial capital for P&L calculation.
//
// Description:
//
//	GetInitialCapital implements TradingSignalRequester interface.
//	Returns the starting capital from the backtest scenario.
//
// Inputs:
//
//	None (operates on receiver)
//
// Outputs:
//   - float64: The initial capital
//
// Example:
//
//	capital := req.GetInitialCapital() // 100000.00
//
// Limitations:
//
//	None
//
// Assumptions:
//   - InitialCapital is positive
func (r *TradingSignalRequestV2) GetInitialCapital() float64 {
	return r.Portfolio.InitialCapital
}

// GetStrategyParams returns the strategy-specific parameters.
//
// Description:
//
//	GetStrategyParams implements TradingSignalRequester interface.
//	Returns the parameters from the YAML trading.params section.
//
// Inputs:
//
//	None (operates on receiver)
//
// Outputs:
//   - map[string]interface{}: Strategy parameters (e.g., threshold_type, threshold_value)
//
// Example:
//
//	params := req.GetStrategyParams()
//	// {"threshold_type": "absolute", "threshold_value": 2.0, "execution_size": 10.0}
//
// Limitations:
//   - Returns nil if no params were set
//
// Assumptions:
//   - Params match the strategy type's requirements
func (r *TradingSignalRequestV2) GetStrategyParams() map[string]interface{} {
	return r.StrategyParams
}

// =============================================================================
// METHODS - V2 Specific
// =============================================================================

// HasInferenceRef returns true if inference tracing is available.
//
// Description:
//
//	HasInferenceRef checks whether this request has valid inference reference
//	IDs for traceability purposes. This is true when using the unified API mode.
//
// Inputs:
//
//	None (operates on receiver)
//
// Outputs:
//   - bool: true if both RequestID and ResponseID are non-empty
//
// Example:
//
//	if req.HasInferenceRef() {
//	    slog.Info("Request linked to inference",
//	        "trading_id", req.RequestID,
//	        "inference_id", req.InferenceRef.RequestID,
//	    )
//	}
//
// Limitations:
//   - Does not validate UUID format
//   - Does not verify IDs exist in any system
//
// Assumptions:
//   - Empty InferenceRef means legacy mode was used
func (r *TradingSignalRequestV2) HasInferenceRef() bool {
	return r.InferenceRef.IsSet()
}

// ToV1 converts the V2 request to V1 format for Sapheneia compatibility.
//
// Description:
//
//	ToV1 converts this V2 request to the legacy V1 TradingSignalRequest format.
//	This is required because Sapheneia's trading service doesn't support V2 yet.
//	The conversion preserves all trading-relevant fields but drops the inference
//	tracing (which should be logged separately).
//
// Inputs:
//
//	None (operates on receiver)
//
// Outputs:
//   - TradingSignalRequest: V1 format request for Sapheneia
//
// Example:
//
//	v2Req := NewTradingSignalRequestV2(...)
//	v1Req := v2Req.ToV1()
//	response, err := evaluator.CallTradingService(ctx, v1Req)
//
// Limitations:
//   - InferenceRef is lost in conversion (log it before converting)
//   - V2-specific RequestID/Timestamp are not preserved
//
// Assumptions:
//   - Sapheneia trading service accepts V1 format
//   - All required V1 fields can be derived from V2
func (r *TradingSignalRequestV2) ToV1() TradingSignalRequest {
	currentPrice := r.Prices.Current
	return TradingSignalRequest{
		Ticker:          r.Ticker,
		ForecastPrice:   r.Prices.Forecast,
		CurrentPrice:    &currentPrice,
		CurrentPosition: r.Portfolio.Position,
		AvailableCash:   r.Portfolio.Cash,
		InitialCapital:  r.Portfolio.InitialCapital,
		StrategyType:    r.StrategyType,
		StrategyParams:  r.StrategyParams,
	}
}

// Validate checks if the V2 request is valid for trading.
//
// Description:
//
//	Validate performs validation on the V2 request to ensure all required
//	fields are present and have valid values. This should be called before
//	sending the request to the trading service.
//
// Inputs:
//
//	None (operates on receiver)
//
// Outputs:
//   - error: nil if valid, error describing the validation failure otherwise
//
// Example:
//
//	if err := req.Validate(); err != nil {
//	    return fmt.Errorf("invalid trading request: %w", err)
//	}
//
// Limitations:
//   - Does not validate strategy-specific parameters
//   - Does not verify InferenceRef IDs exist
//
// Assumptions:
//   - Caller wants basic validation before API call
func (r *TradingSignalRequestV2) Validate() error {
	if r.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	if r.Ticker == "" {
		return fmt.Errorf("ticker is required")
	}
	if r.StrategyType == "" {
		return fmt.Errorf("strategy_type is required")
	}
	if r.StrategyType != "threshold" && r.StrategyType != "return" && r.StrategyType != "quantile" {
		return fmt.Errorf("strategy_type must be 'threshold', 'return', or 'quantile', got: %s", r.StrategyType)
	}
	if r.Prices.Current <= 0 {
		return fmt.Errorf("prices.current must be positive, got: %f", r.Prices.Current)
	}
	if r.Prices.Forecast <= 0 {
		return fmt.Errorf("prices.forecast must be positive, got: %f", r.Prices.Forecast)
	}
	if r.Portfolio.Position < 0 {
		return fmt.Errorf("portfolio.position cannot be negative, got: %f", r.Portfolio.Position)
	}
	if r.Portfolio.Cash < 0 {
		return fmt.Errorf("portfolio.cash cannot be negative, got: %f", r.Portfolio.Cash)
	}
	if r.Portfolio.InitialCapital <= 0 {
		return fmt.Errorf("portfolio.initial_capital must be positive, got: %f", r.Portfolio.InitialCapital)
	}
	return nil
}

// =============================================================================
// V1 INTERFACE IMPLEMENTATION
// =============================================================================

// Ensure TradingSignalRequest implements TradingSignalRequester
var _ TradingSignalRequester = (*TradingSignalRequest)(nil)

// GetTicker returns the ticker symbol from V1 request.
func (r *TradingSignalRequest) GetTicker() string {
	return r.Ticker
}

// GetStrategyType returns the strategy type from V1 request.
func (r *TradingSignalRequest) GetStrategyType() string {
	return r.StrategyType
}

// GetForecastPrice returns the forecast price from V1 request.
func (r *TradingSignalRequest) GetForecastPrice() float64 {
	return r.ForecastPrice
}

// GetCurrentPrice returns the current price from V1 request.
// Returns 0 if CurrentPrice was not set.
func (r *TradingSignalRequest) GetCurrentPrice() float64 {
	if r.CurrentPrice == nil {
		return 0
	}
	return *r.CurrentPrice
}

// GetCurrentPosition returns the current position from V1 request.
func (r *TradingSignalRequest) GetCurrentPosition() float64 {
	return r.CurrentPosition
}

// GetAvailableCash returns the available cash from V1 request.
func (r *TradingSignalRequest) GetAvailableCash() float64 {
	return r.AvailableCash
}

// GetInitialCapital returns the initial capital from V1 request.
func (r *TradingSignalRequest) GetInitialCapital() float64 {
	return r.InitialCapital
}

// GetStrategyParams returns the strategy params from V1 request.
func (r *TradingSignalRequest) GetStrategyParams() map[string]interface{} {
	return r.StrategyParams
}

// =============================================================================
// TYPE ASSERTION COMPILE CHECK
// =============================================================================

// Ensure TradingSignalRequestV2 implements TradingSignalRequester
var _ TradingSignalRequester = (*TradingSignalRequestV2)(nil)
