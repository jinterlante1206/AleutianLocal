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

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
)

// Evaluator handles the logic of running forecasts and checking trading signals
type Evaluator struct {
	httpClient        *http.Client
	orchestratorURL   string
	tradingServiceURL string
	storage           *InfluxDBStorage
}

// NewEvaluator creates a new evaluator instance.
// Note: We default to localhost ports because this is usually run from the CLI on the Host.
func NewEvaluator() (*Evaluator, error) {
	orchestratorURL := os.Getenv("ORCHESTRATOR_URL")
	if orchestratorURL == "" {
		orchestratorURL = "http://localhost:12210"
	}

	tradingURL := os.Getenv("SAPHENEIA_TRADING_SERVICE_URL")
	if tradingURL == "" {
		// Default to the external port mapped in podman-compose (12132->9000)
		tradingURL = "http://localhost:12132"
	}

	storage, err := NewInfluxDBStorage()
	if err != nil {
		return nil, fmt.Errorf("failed to create storage: %w", err)
	}

	return &Evaluator{
		httpClient:        &http.Client{Timeout: 5 * time.Minute},
		orchestratorURL:   orchestratorURL,
		tradingServiceURL: tradingURL,
		storage:           storage,
	}, nil
}

// RunScenario executes a backtest based on the YAML scenario file
func (e *Evaluator) RunScenario(ctx context.Context, scenario *datatypes.BacktestScenario, runID string) error {
	ticker := scenario.Evaluation.Ticker
	slog.Info("Starting backtest loop", "run_id", runID, "ticker", ticker)

	// --- Parse Date Range for Data Fetch ---
	// 1. Parse fetch_start_date (required)
	fetchStartStr := scenario.Evaluation.FetchStartDate
	if fetchStartStr == "" {
		return fmt.Errorf("fetch_start_date is required in evaluation config")
	}

	// Support both YYYY-MM-DD and YYYYMMDD formats
	layout := "2006-01-02"
	if len(fetchStartStr) == 8 {
		layout = "20060102"
	}

	fetchStartDate, err := time.Parse(layout, fetchStartStr)
	if err != nil {
		return fmt.Errorf("invalid fetch_start_date format: %w", err)
	}

	// 2. Parse end_date (use from config or default to now)
	var fetchEndDate time.Time
	endDateStr := scenario.Evaluation.EndDate
	if endDateStr == "" {
		fetchEndDate = time.Now()
	} else {
		endLayout := "2006-01-02"
		if len(endDateStr) == 8 {
			endLayout = "20060102"
		}
		fetchEndDate, err = time.Parse(endLayout, endDateStr)
		if err != nil {
			return fmt.Errorf("invalid end_date format: %w", err)
		}
	}

	slog.Info("Fetching data from absolute date range", "fetch_start", fetchStartDate.Format("2006-01-02"), "fetch_end", fetchEndDate.Format("2006-01-02"))

	// 3. Fetch OHLC data using absolute date range
	fullHistory, _, err := fetchOHLCFromInfluxByDateRange(ctx, ticker, fetchStartDate, fetchEndDate)
	if err != nil {
		return fmt.Errorf("failed to fetch history: %w", err)
	}
	// -------------------------------------

	if len(fullHistory.Close) == 0 {
		return fmt.Errorf("no historical data found for %s", ticker)
	}

	slog.Info("Data fetch complete",
		"total_points", len(fullHistory.Close),
		"date_range", fmt.Sprintf("%s to %s", fullHistory.Time[0].Format("2006-01-02"), fullHistory.Time[len(fullHistory.Time)-1].Format("2006-01-02")))

	// 2. Determine Start Index
	// (Parse EvaluationStartDate)
	evalStartLayout := "2006-01-02"
	if len(scenario.Evaluation.StartDate) == 8 {
		evalStartLayout = "20060102"
	}
	evalStart, err := time.Parse(evalStartLayout, scenario.Evaluation.StartDate)
	if err != nil {
		return fmt.Errorf("invalid start date format: %w", err)
	}

	startIndex := -1
	for i, t := range fullHistory.Time {
		// Compare dates (ignoring time)
		if !t.Before(evalStart) {
			startIndex = i
			break
		}
	}

	if startIndex == -1 {
		return fmt.Errorf("start date %s not found in loaded history (oldest data: %s, latest data: %s)",
			scenario.Evaluation.StartDate, fullHistory.Time[0].Format("2006-01-02"), fullHistory.Time[len(fullHistory.Time)-1].Format("2006-01-02"))
	}

	// Ensure we have enough context *before* the start index
	if startIndex < scenario.Forecast.ContextSize {
		return fmt.Errorf("insufficient history before start date. Need %d days context, but only have %d days available (start_date=%s is at index %d, oldest_data=%s)",
			scenario.Forecast.ContextSize, startIndex, scenario.Evaluation.StartDate, startIndex, fullHistory.Time[0].Format("2006-01-02"))
	}

	slog.Info("Evaluation date range validated",
		"start_date", scenario.Evaluation.StartDate,
		"start_index", startIndex,
		"context_days_available", startIndex,
		"context_days_required", scenario.Forecast.ContextSize)

	endIndex := len(fullHistory.Close) - 1
	slog.Info("Backtest range found", "start_date", fullHistory.Time[startIndex], "start_index", startIndex, "days_to_test", endIndex-startIndex)

	// 3. Initialize Portfolio
	currentPosition := scenario.Trading.InitialPosition
	currentCash := scenario.Trading.InitialCash

	// 4. The Backtest Loop
	for i := startIndex; i <= endIndex; i++ {
		// A. Get "Current" Price
		currentSimulatedPrice := fullHistory.Close[i]
		currentDate := fullHistory.Time[i]

		// B. Simulate Forecast (Step 3)
		// MVP: Simulate a forecast. Later, call e.CallForecastService with sliced history.
		// simulatedForecast := currentSimulatedPrice * 1.002 // 0.2% predicted gain

		// REAL CALL (If services support it):
		// For now, we will stick to simulation or simple call to ensure pipeline works
		// Since standard forecast API doesn't support "as of date" yet, we use a placeholder:
		predictedPrice := currentSimulatedPrice * 1.005

		// C. Execute Trade (Step 4)
		tradingReq := datatypes.TradingSignalRequest{
			Ticker:          ticker,
			StrategyType:    scenario.Trading.StrategyType,
			ForecastPrice:   predictedPrice,
			CurrentPrice:    &currentSimulatedPrice,
			CurrentPosition: currentPosition,
			AvailableCash:   currentCash,
			InitialCapital:  scenario.Trading.InitialCapital,
			StrategyParams:  scenario.Trading.Params,
		}

		// Flatten the request for the Python API (required for StrategyParams)
		signal, err := e.CallTradingService(ctx, tradingReq)
		if err != nil {
			slog.Error("Trade signal failed", "date", currentDate, "error", err)
			continue
		}

		// D. Update State
		currentPosition = signal.PositionAfter
		currentCash = signal.AvailableCash

		// E. Store Result
		result := datatypes.EvaluationResult{
			Ticker:          ticker,
			Model:           scenario.Forecast.Model,
			EvaluationDate:  currentDate.Format("20060102"),
			RunID:           runID,
			ForecastHorizon: scenario.Forecast.HorizonSize,
			StrategyType:    scenario.Trading.StrategyType,
			ForecastPrice:   predictedPrice,
			CurrentPrice:    currentSimulatedPrice,
			Action:          signal.Action,
			Size:            signal.Size,
			Value:           signal.Value,
			Reason:          signal.Reason,
			AvailableCash:   signal.AvailableCash,
			PositionAfter:   signal.PositionAfter,
			Timestamp:       currentDate,
		}

		if err := e.storage.StoreResult(ctx, &result); err != nil {
			slog.Error("Failed to store result", "error", err)
		}

		if i%20 == 0 {
			slog.Info("Progress", "date", currentDate.Format("20060102"), "cash", currentCash,
				"pos", currentPosition)
		}
	}

	return nil
}

func (e *Evaluator) EvaluateTickerModel(
	ctx context.Context,
	ticker string,
	model string,
	config *datatypes.EvaluationConfig,
	currentPrice float64,
) error {
	slog.Info("Evaluating", "ticker", ticker, "model", model)

	// 1. Generate forecast via Orchestrator
	forecast, err := e.CallForecastService(ctx, ticker, model, config.ContextSize, config.HorizonSize)
	if err != nil {
		return fmt.Errorf("forecast failed: %w", err)
	}

	if len(forecast.Forecast) == 0 {
		return fmt.Errorf("empty forecast received")
	}

	// 2. Track portfolio state
	currentPosition := config.InitialPosition
	availableCash := config.InitialCash

	// 3. For each forecast horizon (1 to N days ahead)
	for horizon := 1; horizon <= len(forecast.Forecast); horizon++ {
		forecastPrice := forecast.Forecast[horizon-1]

		// Build trading signal request
		tradingReq := datatypes.TradingSignalRequest{
			Ticker:          ticker,
			StrategyType:    config.StrategyType,
			ForecastPrice:   forecastPrice,
			CurrentPrice:    &currentPrice, // We use the initial price as the "current" for simulation
			CurrentPosition: currentPosition,
			AvailableCash:   availableCash,
			InitialCapital:  config.InitialCapital,
			StrategyParams:  config.StrategyParams,
		}

		// Generate trading signal via Sapheneia
		signal, err := e.CallTradingService(ctx, tradingReq)
		if err != nil {
			slog.Error("Trading signal failed", "horizon", horizon, "error", err)
			continue
		}

		// Update portfolio state for next iteration
		availableCash = signal.AvailableCash
		currentPosition = signal.PositionAfter

		// Prepare Result for Storage
		thresholdVal := 0.0
		executionSize := 0.0
		if v, ok := config.StrategyParams["threshold_value"].(float64); ok {
			thresholdVal = v
		}
		if v, ok := config.StrategyParams["execution_size"].(float64); ok {
			executionSize = v
		}

		result := datatypes.EvaluationResult{
			Ticker:          ticker,
			Model:           model,
			EvaluationDate:  config.EvaluationDate,
			RunID:           config.RunID,
			ForecastHorizon: horizon,
			StrategyType:    config.StrategyType,
			ForecastPrice:   forecastPrice,
			CurrentPrice:    currentPrice,
			Action:          signal.Action,
			Size:            signal.Size,
			Value:           signal.Value,
			Reason:          signal.Reason,
			AvailableCash:   signal.AvailableCash,
			PositionAfter:   signal.PositionAfter,
			Stopped:         signal.Stopped,
			ThresholdValue:  thresholdVal,
			ExecutionSize:   executionSize,
			Timestamp:       time.Now(),
		}

		if err := e.storage.StoreResult(ctx, &result); err != nil {
			slog.Error("Failed to store result", "error", err)
		}
	}
	return nil
}

// --- HTTP Helper Methods ---

func (e *Evaluator) CallForecastService(ctx context.Context, ticker, model string, contextSize, horizonSize int) (*datatypes.ForecastResult, error) {
	url := fmt.Sprintf("%s/v1/timeseries/forecast", e.orchestratorURL)
	payload := map[string]interface{}{
		"name":                 ticker,
		"context_period_size":  contextSize,
		"forecast_period_size": horizonSize,
		"model":                model,
	}
	reqBody, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("forecast error status: %d", resp.StatusCode)
	}

	var result datatypes.ForecastResult
	err = json.NewDecoder(resp.Body).Decode(&result)
	return &result, err
}

func (e *Evaluator) CallTradingService(ctx context.Context, req datatypes.TradingSignalRequest) (*datatypes.TradingSignalResponse, error) {
	url := fmt.Sprintf("%s/trading/execute", e.tradingServiceURL)
	flatReq := map[string]interface{}{
		"ticker":           req.Ticker,
		"strategy_type":    req.StrategyType,
		"forecast_price":   req.ForecastPrice,
		"current_price":    req.CurrentPrice,
		"current_position": req.CurrentPosition,
		"available_cash":   req.AvailableCash,
		"initial_capital":  req.InitialCapital,
	}
	for key, value := range req.StrategyParams {
		flatReq[key] = value
	}
	reqBody, _ := json.Marshal(flatReq)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")

	apiKey := os.Getenv("SAPHENEIA_TRADING_API_KEY")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := e.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("trading error: %s", string(b))
	}

	var result datatypes.TradingSignalResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	return &result, err
}

func (e *Evaluator) GetCurrentPrice(ctx context.Context, ticker string) (float64, error) {
	// Simple Influx query to get the last known close price
	query := fmt.Sprintf(`
		from(bucket: "%s")
		  |> range(start: -7d)
		  |> filter(fn: (r) => r._measurement == "stock_prices")
		  |> filter(fn: (r) => r.ticker == "%s")
		  |> filter(fn: (r) => r._field == "close")
		  |> last()
	`, e.storage.bucket, ticker)

	// Note: We use the storage client's QueryAPI
	queryAPI := e.storage.client.QueryAPI(e.storage.org)
	result, err := queryAPI.Query(ctx, query)
	if err != nil {
		return 0, err
	}
	if result.Next() {
		if val, ok := result.Record().Value().(float64); ok {
			return val, nil
		}
	}
	return 0, fmt.Errorf("no price data found for %s", ticker)
}

func (e *Evaluator) Close() error {
	e.storage.Close()
	return nil
}

// --- Internal Storage Implementation ---

type InfluxDBStorage struct {
	client   influxdb2.Client
	writeAPI api.WriteAPIBlocking
	bucket   string
	org      string
}

func NewInfluxDBStorage() (*InfluxDBStorage, error) {
	// Default to external port 12130 if not set
	url := os.Getenv("INFLUXDB_URL")
	if url == "" {
		url = "http://localhost:12130"
	}

	token := os.Getenv("INFLUXDB_TOKEN")
	if token == "" {
		// Try to fallback to the default dev token
		token = "your_super_secret_admin_token"
	}

	org := os.Getenv("INFLUXDB_ORG")
	if org == "" {
		org = "aleutian-finance"
	}

	bucket := os.Getenv("INFLUXDB_BUCKET")
	if bucket == "" {
		bucket = "financial-data"
	}

	client := influxdb2.NewClient(url, token)
	writeAPI := client.WriteAPIBlocking(org, bucket)

	return &InfluxDBStorage{
		client:   client,
		writeAPI: writeAPI,
		bucket:   bucket,
		org:      org,
	}, nil
}

func (s *InfluxDBStorage) StoreResult(ctx context.Context, result *datatypes.EvaluationResult) error {
	p := influxdb2.NewPointWithMeasurement("forecast_evaluations").
		AddTag("ticker", result.Ticker).
		AddTag("model", result.Model).
		AddTag("evaluation_date", result.EvaluationDate).
		AddTag("run_id", result.RunID).
		AddTag("forecast_horizon", fmt.Sprintf("%d", result.ForecastHorizon)).
		AddTag("strategy_type", result.StrategyType).
		AddField("forecast_price", result.ForecastPrice).
		AddField("current_price", result.CurrentPrice).
		AddField("action", result.Action).
		AddField("size", result.Size).
		AddField("value", result.Value).
		AddField("reason", result.Reason).
		AddField("available_cash", result.AvailableCash).
		AddField("position_after", result.PositionAfter).
		AddField("stopped", result.Stopped).
		AddField("threshold_value", result.ThresholdValue).
		AddField("execution_size", result.ExecutionSize).
		SetTime(result.Timestamp)

	return s.writeAPI.WritePoint(ctx, p)
}

func (s *InfluxDBStorage) Close() {
	s.client.Close()
}
