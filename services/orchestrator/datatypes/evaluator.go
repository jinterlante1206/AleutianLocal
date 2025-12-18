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

// TickerInfo represents a ticker to evaluate
type TickerInfo struct {
	Ticker      string `json:"ticker"`
	Description string `json:"description"`
}

// EvaluationConfig configures the evaluation run
type EvaluationConfig struct {
	Tickers        []TickerInfo
	Models         []string
	EvaluationDate string // "20250117"
	RunID          string

	// Strategy configuration
	StrategyType   string
	StrategyParams map[string]interface{}

	// Forecast configuration
	ContextSize int
	HorizonSize int

	// Portfolio configuration
	InitialCapital  float64
	InitialPosition float64
	InitialCash     float64
}

// ForecastResult matches the JSON response from /v1/timeseries/forecast
type ForecastResult struct {
	Name     string    `json:"name"`
	Forecast []float64 `json:"forecast"`
	Message  string    `json:"message"`
}

// TradingSignalResponse matches the JSON response from Sapheneia /trading/execute
type TradingSignalResponse struct {
	Action        string  `json:"action"`
	Size          float64 `json:"size"`
	Value         float64 `json:"value"`
	Reason        string  `json:"reason"`
	AvailableCash float64 `json:"available_cash"`
	PositionAfter float64 `json:"position_after"`
	Stopped       bool    `json:"stopped"`
}

// EvaluationResult is the final data point stored in InfluxDB
type EvaluationResult struct {
	Ticker          string
	Model           string
	EvaluationDate  string
	RunID           string
	ForecastHorizon int
	StrategyType    string

	ForecastPrice  float64
	CurrentPrice   float64
	Action         string
	Size           float64
	Value          float64
	Reason         string
	AvailableCash  float64
	PositionAfter  float64
	Stopped        bool
	ThresholdValue float64
	ExecutionSize  float64

	Timestamp time.Time
}

// --- Defaults ---

var DefaultTickers = []TickerInfo{
	{Ticker: "SPY", Description: "SPDR S&P 500"},
	{Ticker: "QQQ", Description: "Invesco QQQ Trust"},
	{Ticker: "IWM", Description: "iShares Russell 2000"},
	{Ticker: "BTCUSDT", Description: "BTC Spot"},
	{Ticker: "ETHUSD", Description: "Ethereum Spot"},
	{Ticker: "GLD", Description: "SPDR Gold Trust"},
	{Ticker: "TLT", Description: "iShares 20+ Year Treasury Bond"},
	{Ticker: "XLE", Description: "Energy"},
	{Ticker: "XLF", Description: "Financials"},
	{Ticker: "XLK", Description: "Technology"},
}

var DefaultModels = []string{
	// --- Google TimesFM ---
	"google/timesfm-1.0-200m",
	"google/timesfm-2.0-500m-pytorch", // Primary Recommendation

	// --- Amazon Chronos (T5) ---
	"amazon/chronos-t5-tiny",
	"amazon/chronos-t5-mini",
	"amazon/chronos-t5-small",
	"amazon/chronos-t5-base",
	"amazon/chronos-t5-large",

	// --- Amazon Chronos (Bolt) ---
	"amazon/chronos-bolt-mini",
	"amazon/chronos-bolt-small",
	"amazon/chronos-bolt-base",

	// --- Salesforce Moirai ---
	"salesforce/moirai-1.1-R-small",
	"salesforce/moirai-1.1-R-base",
	"salesforce/moirai-1.1-R-large",

	// --- IBM Granite ---
	"ibm/granite-ttm-r1",
	"ibm/granite-ttm-r2",

	// --- AutoLab Moment ---
	"autonlab/moment-1-small",
	"autonlab/moment-1-base",
	"autonlab/moment-1-large",

	// --- Alibaba Yinglong ---
	"alibaba/yinglong-6m",
	"alibaba/yinglong-50m",
	"alibaba/yinglong-300m",

	// --- Specialized / Single Models ---
	"lag-llama",
	"unity/kairos-10m",
	"microsoft/timemoe-200m",
	"thuml/timer-large",
}
