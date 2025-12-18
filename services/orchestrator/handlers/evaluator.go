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

// RunEvaluation is the main entry point called by the CLI
func (e *Evaluator) RunEvaluation(ctx context.Context, config *datatypes.EvaluationConfig) error {
	slog.Info("Starting evaluation run",
		"run_id", config.RunID,
		"tickers", len(config.Tickers),
		"models", len(config.Models))

	successCount := 0
	errorCount := 0

	for _, tickerInfo := range config.Tickers {
		ticker := tickerInfo.Ticker

		// 1. Get current price (needed for the simulation baseline)
		currentPrice, err := e.GetCurrentPrice(ctx, ticker)
		if err != nil {
			slog.Error("Failed to get price", "ticker", ticker, "error", err)
			errorCount++
			continue
		}

		for _, model := range config.Models {
			// 2. Evaluate specific ticker/model combo
			err := e.EvaluateTickerModel(ctx, ticker, model, config, currentPrice)
			if err != nil {
				slog.Error("Evaluation failed", "ticker", ticker, "model", model, "error", err)
				errorCount++
			} else {
				successCount++
			}
		}
	}

	slog.Info("Evaluation run complete", "successes", successCount, "errors", errorCount)
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
		tradingReq := TradingSignalRequest{
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

func (e *Evaluator) CallTradingService(ctx context.Context, req TradingSignalRequest) (*datatypes.TradingSignalResponse, error) {
	url := fmt.Sprintf("%s/trading/execute", e.tradingServiceURL)
	reqBody, _ := json.Marshal(req)
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
