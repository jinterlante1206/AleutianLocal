package datatypes

import "time"

// ========== REQUEST/RESPONSE STRUCTURES ==========

// TradingSignalRequest is the request structure for the trading signal endpoint
type TradingSignalRequest struct {
	// Ticker and forecast info
	Ticker        string   `json:"ticker" binding:"required"`
	ForecastPrice float64  `json:"forecast_price" binding:"required,gt=0"`
	CurrentPrice  *float64 `json:"current_price,omitempty"` // Optional, will fetch from InfluxDB if not provided

	// Portfolio state
	CurrentPosition float64 `json:"current_position" binding:"required,gte=0"`
	AvailableCash   float64 `json:"available_cash" binding:"required,gte=0"`
	InitialCapital  float64 `json:"initial_capital" binding:"required,gt=0"`

	// Strategy configuration
	StrategyType   string                 `json:"strategy_type" binding:"required,oneof=threshold return quantile"`
	StrategyParams map[string]interface{} `json:"strategy_params" binding:"required"`

	// Historical data parameters
	HistoryDays int `json:"history_days,omitempty"` // Default: 252 (1 year)
}

// OHLCData holds historical OHLC price data
type OHLCData struct {
	Time     []time.Time `json:"time"`
	Open     []float64   `json:"open_history"`
	High     []float64   `json:"high_history"`
	Low      []float64   `json:"low_history"`
	Close    []float64   `json:"close_history"`
	AdjClose []float64   `json:"adj_close_history"`
	Volume   []float64   `json:"volume_history"`
}
