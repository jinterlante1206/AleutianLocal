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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/jinterlante1206/AleutianLocal/pkg/validation"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
)

// ========== INFLUXDB HELPERS ==========

// fetchOHLCFromInfluxByDateRange retrieves OHLC historical data from InfluxDB using absolute date ranges
func fetchOHLCFromInfluxByDateRange(ctx context.Context, ticker string, startDate, endDate time.Time) (*datatypes.OHLCData, float64, error) {
	// Validate ticker to prevent Flux injection
	if err := validation.ValidateTicker(ticker); err != nil {
		return nil, 0, fmt.Errorf("invalid ticker: %w", err)
	}

	// Get InfluxDB configuration from environment OR use defaults
	// This allows the CLI (running on host) to connect to localhost:12130
	influxURL := os.Getenv("INFLUXDB_URL")
	if influxURL == "" {
		influxURL = "http://localhost:12130"
	}

	influxToken := os.Getenv("INFLUXDB_TOKEN")
	if influxToken == "" {
		influxToken = "your_super_secret_admin_token"
	}

	influxOrg := os.Getenv("INFLUXDB_ORG")
	if influxOrg == "" {
		influxOrg = "aleutian-finance"
	}

	influxBucket := os.Getenv("INFLUXDB_BUCKET")
	if influxBucket == "" {
		influxBucket = "financial-data"
	}

	if influxURL == "" || influxToken == "" || influxOrg == "" || influxBucket == "" {
		return nil, 0, fmt.Errorf("InfluxDB configuration not set in environment")
	}

	// Create InfluxDB client
	client := influxdb2.NewClient(influxURL, influxToken)
	defer client.Close()

	queryAPI := client.QueryAPI(influxOrg)

	// Format dates as RFC3339 for InfluxDB query
	startTimestamp := startDate.Format(time.RFC3339)
	endTimestamp := endDate.Format(time.RFC3339)

	// Query to fetch OHLC data using absolute date range
	query := fmt.Sprintf(`
		from(bucket: "%s")
		  |> range(start: %s, stop: %s)
		  |> filter(fn: (r) => r._measurement == "stock_prices")
		  |> filter(fn: (r) => r.ticker == "%s")
		  |> pivot(rowKey:["_time"], columnKey: ["_field"], valueColumn: "_value")
		  |> sort(columns: ["_time"], desc: false)
	`, influxBucket, startTimestamp, endTimestamp, ticker)

	slog.Info("Fetching OHLC data from InfluxDB", "ticker", ticker, "start", startDate.Format("2006-01-02"), "end", endDate.Format("2006-01-02"))

	result, err := queryAPI.Query(ctx, query)
	if err != nil {
		return nil, 0, fmt.Errorf("InfluxDB query failed: %w", err)
	}

	// Parse results
	ohlc := &datatypes.OHLCData{
		Time:     make([]time.Time, 0),
		Open:     make([]float64, 0),
		High:     make([]float64, 0),
		Low:      make([]float64, 0),
		Close:    make([]float64, 0),
		AdjClose: make([]float64, 0),
		Volume:   make([]float64, 0),
	}

	var latestPrice float64

	for result.Next() {
		record := result.Record()
		ohlc.Time = append(ohlc.Time, record.Time())

		// Extract OHLC values from the record
		if open, ok := record.ValueByKey("open").(float64); ok {
			ohlc.Open = append(ohlc.Open, open)
		}
		if high, ok := record.ValueByKey("high").(float64); ok {
			ohlc.High = append(ohlc.High, high)
		}
		if low, ok := record.ValueByKey("low").(float64); ok {
			ohlc.Low = append(ohlc.Low, low)
		}
		if close, ok := record.ValueByKey("close").(float64); ok {
			ohlc.Close = append(ohlc.Close, close)
			latestPrice = close // Keep updating to get the latest
		}
		if adjClose, ok := record.ValueByKey("adj_close").(float64); ok {
			ohlc.AdjClose = append(ohlc.AdjClose, adjClose)
		}
		if volume, ok := record.ValueByKey("volume").(float64); ok {
			ohlc.Volume = append(ohlc.Volume, volume)
		}
	}

	if result.Err() != nil {
		return nil, 0, fmt.Errorf("error reading InfluxDB results: %w", result.Err())
	}

	// Validate we got data
	if len(ohlc.Close) == 0 {
		return nil, 0, fmt.Errorf("no historical data found for ticker %s between %s and %s", ticker, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	}

	slog.Info("Successfully fetched OHLC data", "ticker", ticker, "points", len(ohlc.Close), "date_range", fmt.Sprintf("%s to %s", ohlc.Time[0].Format("2006-01-02"), ohlc.Time[len(ohlc.Time)-1].Format("2006-01-02")), "latest_price", latestPrice)

	return ohlc, latestPrice, nil
}

// fetchOHLCFromInflux retrieves OHLC historical data from InfluxDB using relative days (for real-time trading)
// If asOfDate is provided, data is fetched up to that date (for backtesting). Otherwise, fetches up to now.
func fetchOHLCFromInflux(ctx context.Context, ticker string, days int, asOfDate *time.Time) (*datatypes.OHLCData, float64, error) {
	// Validate ticker to prevent Flux injection
	if err := validation.ValidateTicker(ticker); err != nil {
		return nil, 0, fmt.Errorf("invalid ticker: %w", err)
	}

	// Get InfluxDB configuration from environment OR use defaults
	// This allows the CLI (running on host) to connect to localhost:12130
	influxURL := os.Getenv("INFLUXDB_URL")
	if influxURL == "" {
		influxURL = "http://localhost:12130"
	}

	influxToken := os.Getenv("INFLUXDB_TOKEN")
	if influxToken == "" {
		influxToken = "your_super_secret_admin_token"
	}

	influxOrg := os.Getenv("INFLUXDB_ORG")
	if influxOrg == "" {
		influxOrg = "aleutian-finance"
	}

	influxBucket := os.Getenv("INFLUXDB_BUCKET")
	if influxBucket == "" {
		influxBucket = "financial-data"
	}

	if influxURL == "" || influxToken == "" || influxOrg == "" || influxBucket == "" {
		return nil, 0, fmt.Errorf("InfluxDB configuration not set in environment")
	}

	// Create InfluxDB client
	client := influxdb2.NewClient(influxURL, influxToken)
	defer client.Close()

	queryAPI := client.QueryAPI(influxOrg)

	// Calculate calendar days (trading days * 1.6 to account for weekends/holidays)
	calendarDays := int(float64(days) * 1.6)

	// Determine stop time (for backtesting vs real-time)
	stopTime := "now()"
	if asOfDate != nil {
		stopTime = asOfDate.Format(time.RFC3339)
	}

	// Query to fetch OHLC data
	query := fmt.Sprintf(`
		from(bucket: "%s")
		  |> range(start: -%dd, stop: %s)
		  |> filter(fn: (r) => r._measurement == "stock_prices")
		  |> filter(fn: (r) => r.ticker == "%s")
		  |> pivot(rowKey:["_time"], columnKey: ["_field"], valueColumn: "_value")
		  |> sort(columns: ["_time"], desc: false)
		  |> tail(n: %d)
	`, influxBucket, calendarDays, stopTime, ticker, days)

	slog.Info("Fetching OHLC data from InfluxDB", "ticker", ticker, "days", days)

	result, err := queryAPI.Query(ctx, query)
	if err != nil {
		return nil, 0, fmt.Errorf("InfluxDB query failed: %w", err)
	}

	// Parse results
	ohlc := &datatypes.OHLCData{
		Time:     make([]time.Time, 0),
		Open:     make([]float64, 0),
		High:     make([]float64, 0),
		Low:      make([]float64, 0),
		Close:    make([]float64, 0),
		AdjClose: make([]float64, 0),
		Volume:   make([]float64, 0),
	}

	var latestPrice float64

	for result.Next() {
		record := result.Record()
		ohlc.Time = append(ohlc.Time, record.Time())

		// Extract OHLC values from the record
		if open, ok := record.ValueByKey("open").(float64); ok {
			ohlc.Open = append(ohlc.Open, open)
		}
		if high, ok := record.ValueByKey("high").(float64); ok {
			ohlc.High = append(ohlc.High, high)
		}
		if low, ok := record.ValueByKey("low").(float64); ok {
			ohlc.Low = append(ohlc.Low, low)
		}
		if close, ok := record.ValueByKey("close").(float64); ok {
			ohlc.Close = append(ohlc.Close, close)
			latestPrice = close // Keep updating to get the latest
		}
		if adjClose, ok := record.ValueByKey("adj_close").(float64); ok {
			ohlc.AdjClose = append(ohlc.AdjClose, adjClose)
		}
		if volume, ok := record.ValueByKey("volume").(float64); ok {
			ohlc.Volume = append(ohlc.Volume, volume)
		}
	}

	if result.Err() != nil {
		return nil, 0, fmt.Errorf("error reading InfluxDB results: %w", result.Err())
	}

	// Validate we got data
	if len(ohlc.Close) == 0 {
		return nil, 0, fmt.Errorf("no historical data found for ticker: %s", ticker)
	}

	slog.Info("Successfully fetched OHLC data", "ticker", ticker, "points", len(ohlc.Close), "latest_price", latestPrice)

	return ohlc, latestPrice, nil
}

// ========== HANDLER ==========

// HandleTradingSignal orchestrates the trading signal generation workflow
// 1. Fetch OHLC data from InfluxDB
// 2. Call the Sapheneia trading service with the data
// 3. Return the trading signal
func HandleTradingSignal() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// 1. Parse request
		var req datatypes.TradingSignalRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			slog.Error("Invalid trading signal request", "error", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body", "details": err.Error()})
			return
		}

		// Set default history days if not provided
		if req.HistoryDays == 0 {
			req.HistoryDays = 252 // Default to 1 year
		}

		slog.Info("Processing trading signal request",
			"ticker", req.Ticker,
			"strategy", req.StrategyType,
			"forecast_price", req.ForecastPrice)

		// 2. Fetch OHLC data from InfluxDB
		ohlcData, latestPrice, err := fetchOHLCFromInflux(ctx, req.Ticker, req.HistoryDays, nil)
		if err != nil {
			slog.Error("Failed to fetch OHLC data", "error", err, "ticker", req.Ticker)
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":   "Failed to fetch historical data",
				"details": err.Error(),
			})
			return
		}

		// 3. Use current price from request or use latest from InfluxDB
		currentPrice := latestPrice
		if req.CurrentPrice != nil {
			currentPrice = *req.CurrentPrice
		}

		// 4. Build request for Sapheneia trading service
		tradingServiceReq := buildTradingServiceRequest(req, currentPrice, ohlcData)

		// 5. Call the Sapheneia trading service
		tradingServiceURL := os.Getenv("SAPHENEIA_TRADING_SERVICE_URL")
		if tradingServiceURL == "" {
			tradingServiceURL = "http://sapheneia-trading-service:9000"
		}
		targetURL := fmt.Sprintf("%s/trading/execute", tradingServiceURL)

		slog.Info("Calling Sapheneia trading service", "url", targetURL)

		reqBody, err := json.Marshal(tradingServiceReq)
		if err != nil {
			slog.Error("Failed to marshal trading service request", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare trading service request"})
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewBuffer(reqBody))
		if err != nil {
			slog.Error("Failed to create trading service request", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create trading service request"})
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")

		// Add API key if configured
		apiKey := os.Getenv("SAPHENEIA_TRADING_API_KEY")
		if apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			slog.Error("Failed to call trading service", "url", targetURL, "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Failed to connect to trading service"})
			return
		}
		defer resp.Body.Close()

		// 6. Parse and enhance the response
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("Failed to read trading service response", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read trading service response"})
			return
		}

		if resp.StatusCode != http.StatusOK {
			slog.Error("Trading service returned error", "status", resp.StatusCode, "body", string(respBody))
			c.Data(resp.StatusCode, "application/json", respBody)
			return
		}

		// Parse the trading service response
		var tradingResp datatypes.TradingSignalResponse
		if err := json.Unmarshal(respBody, &tradingResp); err != nil {
			slog.Error("Failed to parse trading service response", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse trading service response"})
			return
		}

		// Enhance response with additional context
		tradingResp.Ticker = req.Ticker
		tradingResp.CurrentPrice = currentPrice
		tradingResp.ForecastPrice = req.ForecastPrice

		slog.Info("Trading signal generated",
			"ticker", req.Ticker,
			"action", tradingResp.Action,
			"size", tradingResp.Size,
			"value", tradingResp.Value)

		c.JSON(http.StatusOK, tradingResp)
	}
}

// buildTradingServiceRequest constructs the request payload for the Sapheneia trading service
func buildTradingServiceRequest(req datatypes.TradingSignalRequest, currentPrice float64,
	ohlc *datatypes.OHLCData) map[string]interface{} {
	// Start with base parameters
	payload := map[string]interface{}{
		"strategy_type":    req.StrategyType,
		"forecast_price":   req.ForecastPrice,
		"current_price":    currentPrice,
		"current_position": req.CurrentPosition,
		"available_cash":   req.AvailableCash,
		"initial_capital":  req.InitialCapital,

		// Add OHLC history
		"open_history":  ohlc.Open,
		"high_history":  ohlc.High,
		"low_history":   ohlc.Low,
		"close_history": ohlc.Close,
	}

	// Merge strategy-specific parameters
	for key, value := range req.StrategyParams {
		payload[key] = value
	}

	return payload
}
