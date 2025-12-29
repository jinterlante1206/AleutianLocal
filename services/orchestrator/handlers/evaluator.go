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

	// Precheck and autofill data
	adjustedFetchStart, err := e.EnsureDataAvailability(ctx, scenario)
	if err != nil {
		return fmt.Errorf("data availability check failed: %w", err)
	}
	fetchStartDate := adjustedFetchStart

	// Parse end_date (use from config or default to now)
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

	// Fetch OHLC data using absolute date range
	fullHistory, _, err := fetchOHLCFromInfluxByDateRange(ctx, ticker, fetchStartDate, fetchEndDate)
	if err != nil {
		return fmt.Errorf("failed to fetch history: %w", err)
	}
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
		currentSimulatedPrice := fullHistory.Close[i]
		currentDate := fullHistory.Time[i]

		// SLICE HISTORY: Grab exactly the N days leading up to and including today
		// This guarantees the model ONLY sees data up to 'currentDate'
		sliceStart := i - scenario.Forecast.ContextSize + 1
		if sliceStart < 0 {
			sliceStart = 0 // Safety clamp, though validation above prevents this
		}

		// Create the explicit context slice to send to the model
		contextSlice := fullHistory.Close[sliceStart : i+1]

		// Pass the slice directly to the service
		forecast, err := e.CallForecastServiceAsOf(ctx, ticker, scenario.Forecast.Model,
			scenario.Forecast.ContextSize, scenario.Forecast.HorizonSize, &currentDate, contextSlice)

		if err != nil {
			slog.Error("Forecast failed", "date", currentDate.Format("2006-01-02"), "error", err)
			continue
		}

		// Use the first forecast value (1-day ahead prediction)
		var predictedPrice float64
		if len(forecast.Forecast) > 0 {
			predictedPrice = forecast.Forecast[0]
		} else {
			slog.Warn("Empty forecast returned, skipping", "date", currentDate.Format("2006-01-02"))
			continue
		}

		// --- Execute Trade ---
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

		signal, err := e.CallTradingService(ctx, tradingReq)
		if err != nil {
			slog.Error("Trade signal failed", "date", currentDate, "error", err)
			continue
		}

		// Update State
		currentPosition = signal.PositionAfter
		currentCash = signal.AvailableCash

		// Store Result
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
			slog.Info("Progress", "date", currentDate.Format("20060102"), "cash", currentCash)
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

// --- HTTP Helper Methods ---the

func (e *Evaluator) CallForecastService(ctx context.Context, ticker, model string, contextSize, horizonSize int) (*datatypes.ForecastResult, error) {
	// Pass nil for asOfDate (implies "now")
	// Pass nil for contextData (implies "fetch from DB")
	return e.CallForecastServiceAsOf(ctx, ticker, model, contextSize, horizonSize, nil, nil)
}

func (e *Evaluator) CallForecastServiceAsOf(
	ctx context.Context,
	ticker, model string,
	contextSize, horizonSize int,
	asOfDate *time.Time,
	contextData []float64, // <--- New Parameter
) (*datatypes.ForecastResult, error) {

	url := fmt.Sprintf("%s/v1/timeseries/forecast", e.orchestratorURL)

	payload := map[string]interface{}{
		"name":                 ticker,
		"context_period_size":  contextSize,
		"forecast_period_size": horizonSize,
		"model":                model,
	}

	// Add as_of_date for metadata/logging
	if asOfDate != nil {
		payload["as_of_date"] = asOfDate.Format("2006-01-02")
	}

	// Add the explicit historical data (The Fix)
	if len(contextData) > 0 {
		payload["recent_data"] = contextData
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
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("forecast error status %d: %s", resp.StatusCode, string(body))
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

func (e *Evaluator) CheckDataCoverage(ctx context.Context,
	ticker string) (*datatypes.DataCoverageInfo, error) {

	query := fmt.Sprintf(`
		from(bucket: "%s")
            |> range(start: -20y)
            |> filter(fn: (r) => r._measurement == "stock_prices")
            |> filter(fn: (r) => r.ticker == "%s")
            |> filter(fn: (r) => r._field == "close")
            |> group()
            |> reduce(
                identity: {count: 0, first: time(v: "2100-01-01T00:00:00Z"), last: time(v: "1900-01-01T00:00:00Z")},
                fn: (r, accumulator) => ({
                  count: accumulator.count + 1,
                  first: if r._time < accumulator.first then r._time else accumulator.first,
                  last: if r._time > accumulator.last then r._time else accumulator.last
                })
            )
	`, e.storage.bucket, ticker)

	queryAPI := e.storage.client.QueryAPI(e.storage.org)
	result, err := queryAPI.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query data coverage: %w", err)
	}
	info := &datatypes.DataCoverageInfo{Ticker: ticker, HasData: false}
	if result.Next() {
		record := result.Record()
		if count, ok := record.ValueByKey("count").(int64); ok && count > 0 {
			info.PointCount = int(count)
			info.HasData = true
			if first, ok := record.ValueByKey("first").(time.Time); ok {
				info.OldestDate = first
			}
			if last, ok := record.ValueByKey("last").(time.Time); ok {
				info.NewestDate = last
			}
		}
	}
	return info, result.Err()
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

// FetchMissingData calls the data fetcher service to populate InfluxDB
func (e *Evaluator) FetchMissingData(ctx context.Context, ticker string, startDate,
	endDate time.Time) error {

	url := fmt.Sprintf("%s/v1/data/fetch", e.orchestratorURL)

	payload := map[string]interface{}{
		"names":      []string{ticker},
		"start_date": startDate.Format("2006-01-02"),
		"end_date":   endDate.Format("2006-01-02"),
		"interval":   "1d",
	}

	reqBody, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")

	slog.Info("Fetching missing data from external source",
		"ticker", ticker,
		"start", startDate.Format("2006-01-02"),
		"end", endDate.Format("2006-01-02"))

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("data fetch request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("data fetch failed with status %d: %s", resp.StatusCode, string(body))
	}

	slog.Info("Data fetch completed successfully", "ticker", ticker)
	return nil
}

// EnsureDataAvailability checks data coverage and fetches missing data if needed.
// Returns the adjusted fetch_start_date if data isn't available as far back as requested.
func (e *Evaluator) EnsureDataAvailability(ctx context.Context, scenario *datatypes.BacktestScenario) (adjustedFetchStart time.Time, err error) {
	ticker := scenario.Evaluation.Ticker
	contextSize := scenario.Forecast.ContextSize

	// Parse the requested dates
	layout := "2006-01-02"
	if len(scenario.Evaluation.FetchStartDate) == 8 {
		layout = "20060102"
	}
	requestedFetchStart, err := time.Parse(layout, scenario.Evaluation.FetchStartDate)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid fetch_start_date: %w", err)
	}

	endLayout := "2006-01-02"
	if len(scenario.Evaluation.EndDate) == 8 {
		endLayout = "20060102"
	}
	var requestedEnd time.Time
	if scenario.Evaluation.EndDate == "" {
		requestedEnd = time.Now()
	} else {
		requestedEnd, err = time.Parse(endLayout, scenario.Evaluation.EndDate)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid end_date: %w", err)
		}
	}

	// Step 1: Check current data coverage
	slog.Info("Checking data availability in InfluxDB", "ticker", ticker)
	coverage, err := e.CheckDataCoverage(ctx, ticker)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to check data coverage: %w", err)
	}

	if coverage.HasData {
		slog.Info("Current data coverage",
			"ticker", ticker,
			"points", coverage.PointCount,
			"oldest", coverage.OldestDate.Format("2006-01-02"),
			"newest", coverage.NewestDate.Format("2006-01-02"))
	} else {
		slog.Info("No existing data found for ticker", "ticker", ticker)
	}

	// Step 2: Determine what data we need to fetch
	needsFetch := false
	fetchStart := requestedFetchStart
	fetchEnd := requestedEnd

	if !coverage.HasData {
		// No data at all - fetch everything
		needsFetch = true
		slog.Warn("No data exists for ticker, will attempt to fetch",
			"ticker", ticker,
			"requested_start", requestedFetchStart.Format("2006-01-02"),
			"requested_end", requestedEnd.Format("2006-01-02"))
	} else {
		// Check if we need earlier data
		if requestedFetchStart.Before(coverage.OldestDate) {
			needsFetch = true
			fetchEnd = coverage.OldestDate.AddDate(0, 0, -1) // Fetch up to day before existing data
			slog.Info("Need earlier data",
				"requested_start", requestedFetchStart.Format("2006-01-02"),
				"oldest_available", coverage.OldestDate.Format("2006-01-02"))
		}
		// Check if we need later data
		if requestedEnd.After(coverage.NewestDate) {
			if needsFetch {
				// Need both earlier and later - fetch the whole range
				fetchStart = requestedFetchStart
				fetchEnd = requestedEnd
			} else {
				needsFetch = true
				fetchStart = coverage.NewestDate.AddDate(0, 0, 1) // Fetch from day after existing data
				fetchEnd = requestedEnd
			}
			slog.Info("Need later data",
				"requested_end", requestedEnd.Format("2006-01-02"),
				"newest_available", coverage.NewestDate.Format("2006-01-02"))
		}
	}

	// Step 3: Fetch missing data if needed
	if needsFetch {
		err = e.FetchMissingData(ctx, ticker, fetchStart, fetchEnd)
		if err != nil {
			slog.Warn("Failed to fetch missing data", "error", err)
			// Don't fail yet - we might still have enough data
		} else {
			// Re-check coverage after fetch
			coverage, err = e.CheckDataCoverage(ctx, ticker)
			if err != nil {
				return time.Time{}, fmt.Errorf("failed to re-check data coverage: %w", err)
			}
			slog.Info("Updated data coverage after fetch",
				"ticker", ticker,
				"points", coverage.PointCount,
				"oldest", coverage.OldestDate.Format("2006-01-02"),
				"newest", coverage.NewestDate.Format("2006-01-02"))
		}
	}

	// Step 4: Validate we have enough data and adjust if needed
	if !coverage.HasData {
		return time.Time{}, fmt.Errorf("no data available for ticker %s after fetch attempt", ticker)
	}

	adjustedFetchStart = requestedFetchStart

	// Check if oldest available is after what we requested
	if coverage.OldestDate.After(requestedFetchStart) {
		adjustedFetchStart = coverage.OldestDate
		slog.Warn("DATA AVAILABILITY ALERT: Requested start date is earlier than available data",
			"ticker", ticker,
			"requested_start", requestedFetchStart.Format("2006-01-02"),
			"oldest_available", coverage.OldestDate.Format("2006-01-02"),
			"adjusted_fetch_start", adjustedFetchStart.Format("2006-01-02"))

		fmt.Printf("\n⚠️  DATA AVAILABILITY WARNING\n")
		fmt.Printf("   Requested fetch_start_date: %s\n", requestedFetchStart.Format("2006-01-02"))
		fmt.Printf("   Oldest available data:      %s\n", coverage.OldestDate.Format("2006-01-02"))
		fmt.Printf("   → Using oldest available as fetch_start_date\n\n")
	}

	// Calculate if evaluation start_date needs adjustment based on context requirement
	evalLayout := "2006-01-02"
	if len(scenario.Evaluation.StartDate) == 8 {
		evalLayout = "20060102"
	}
	evalStart, _ := time.Parse(evalLayout, scenario.Evaluation.StartDate)

	// Minimum evaluation start = oldest data + context_size trading days
	// Approximate: context_size * 1.5 calendar days to account for weekends
	minCalendarDays := int(float64(contextSize) * 1.5)
	minEvalStart := coverage.OldestDate.AddDate(0, 0, minCalendarDays)

	if evalStart.Before(minEvalStart) {
		fmt.Printf("\n⚠️  CONTEXT REQUIREMENT WARNING\n")
		fmt.Printf("   Evaluation start_date: %s\n", evalStart.Format("2006-01-02"))
		fmt.Printf("   Required context:      %d trading days\n", contextSize)
		fmt.Printf("   Oldest available data: %s\n", coverage.OldestDate.Format("2006-01-02"))
		fmt.Printf("   Minimum eval start:    ~%s (oldest + %d calendar days)\n", minEvalStart.Format("2006-01-02"), minCalendarDays)
		fmt.Printf("   → The evaluation will skip dates until sufficient context is available\n\n")
	}

	return adjustedFetchStart, nil
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
