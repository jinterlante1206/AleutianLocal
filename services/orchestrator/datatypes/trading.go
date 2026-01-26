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
