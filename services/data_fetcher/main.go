// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/pkg/validation"
	"github.com/gin-gonic/gin"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
)

const (
	NUM_WORKERS = 8 // Number of parallel fetches per API request
)

// HTTPClient interface allows injecting mock HTTP clients for testing
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Server struct holds all dependencies
type Server struct {
	WriteAPI   api.WriteAPIBlocking
	QueryAPI   api.QueryAPI
	HTTPClient HTTPClient
}

// --- Yahoo Finance Structs ---
type YahooChartResponse struct {
	Chart struct {
		Result []YahooResult `json:"result"`
		Error  interface{}   `json:"error"`
	} `json:"chart"`
}

type YahooResult struct {
	Meta       YahooMeta       `json:"meta"`
	Timestamp  []int64         `json:"timestamp"`
	Indicators YahooIndicators `json:"indicators"`
}

type YahooMeta struct {
	Currency string `json:"currency"`
	Symbol   string `json:"symbol"`
}

type YahooIndicators struct {
	Quote []struct {
		Open   []float64 `json:"open"`
		High   []float64 `json:"high"`
		Low    []float64 `json:"low"`
		Close  []float64 `json:"close"`
		Volume []int64   `json:"volume"`
	} `json:"quote"`
	AdjClose []struct {
		AdjClose []float64 `json:"adjclose"`
	} `json:"adjclose"`
}

// --- API Request/Response Structs ---
type DataFetchRequest struct {
	Tickers   []string `json:"names"`
	StartDate string   `json:"start_date"` // e.g., "2020-01-01" or "20200101"
	Interval  string   `json:"interval"`   // e.g., "1d", "1h", "1m"
}

type DataFetchResponse struct {
	Status  string            `json:"status"`
	Message string            `json:"message"`
	Details map[string]string `json:"details"`
}

type DataQueryRequest struct {
	Ticker  string `json:"ticker"`
	Days    int    `json:"days"`     // Number of days to query
	EndDate string `json:"end_date"` // Optional: end date (defaults to now)
}

type DataPoint struct {
	Time     string  `json:"time"`
	Open     float64 `json:"open"`
	High     float64 `json:"high"`
	Low      float64 `json:"low"`
	Close    float64 `json:"close"`
	Volume   int64   `json:"volume"`
	AdjClose float64 `json:"adj_close"`
}

type DataQueryResponse struct {
	Ticker string      `json:"ticker"`
	Data   []DataPoint `json:"data"`
	Count  int         `json:"count"`
}

// InfluxDB configuration from environment
var (
	influxURL    = os.Getenv("INFLUXDB_URL")
	influxToken  = os.Getenv("INFLUXDB_TOKEN")
	influxOrg    = os.Getenv("INFLUXDB_ORG")
	influxBucket = os.Getenv("INFLUXDB_BUCKET")
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Set defaults if not provided
	if influxURL == "" {
		influxURL = "http://influxdb:8086"
	}
	if influxToken == "" {
		slog.Error("INFLUXDB_TOKEN environment variable is required")
		os.Exit(1)
	}
	if influxOrg == "" {
		influxOrg = "aleutian-finance"
	}
	if influxBucket == "" {
		influxBucket = "financial-data"
	}

	slog.Info("Starting Aleutian Data Fetcher",
		"influx_url", influxURL,
		"influx_org", influxOrg,
		"influx_bucket", influxBucket)

	// Create InfluxDB client
	influxClient := influxdb2.NewClient(influxURL, influxToken)
	defer influxClient.Close()

	// Wait for InfluxDB to be ready
	var influxReady bool
	slog.Info("Waiting for InfluxDB to be ready...")
	for i := 0; i < 10; i++ {
		health, err := influxClient.Health(context.Background())
		if err == nil && health.Status == "pass" {
			influxReady = true
			break
		}

		var errMsg string
		if err != nil {
			errMsg = err.Error()
		} else if health != nil && health.Message != nil {
			errMsg = *health.Message
		}
		slog.Warn("InfluxDB not ready, retrying...", "attempt", i+1, "error", errMsg)
		time.Sleep(3 * time.Second)
	}

	if !influxReady {
		slog.Error("Failed to connect to InfluxDB after all retries")
		os.Exit(1)
	}

	slog.Info("Successfully connected to InfluxDB")

	// Create Server instance
	server := &Server{
		WriteAPI:   influxClient.WriteAPIBlocking(influxOrg, influxBucket),
		QueryAPI:   influxClient.QueryAPI(influxOrg),
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}

	// Start Gin server
	router := gin.Default()

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "aleutian-data-fetcher"})
	})

	// Data endpoints
	router.POST("/v1/data/fetch", server.handleFetchData)
	router.POST("/v1/data/query", server.handleQueryData)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8001"
	}

	slog.Info("Starting data fetcher API server", "port", port)
	if err := router.Run(":" + port); err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}

// handleFetchData fetches data from Yahoo Finance and stores in InfluxDB
func (s *Server) handleFetchData(c *gin.Context) {
	var req DataFetchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body", "details": err.Error()})
		return
	}

	if len(req.Tickers) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No tickers provided"})
		return
	}

	// Validate all tickers to prevent Flux injection
	for i, ticker := range req.Tickers {
		sanitized, err := validation.SanitizeTicker(ticker)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ticker", "details": err.Error()})
			return
		}
		req.Tickers[i] = sanitized
	}

	if req.Interval == "" {
		req.Interval = "1d"
	}

	slog.Info("Handling data fetch request", "tickers", req.Tickers, "interval", req.Interval, "start_date", req.StartDate)

	var wg sync.WaitGroup
	tickerJobs := make(chan string, len(req.Tickers))
	results := make(chan map[string]string, len(req.Tickers))

	// Create worker goroutines
	for i := 0; i < NUM_WORKERS; i++ {
		wg.Add(1)
		go s.fetchWorker(i, &wg, tickerJobs, results, req.StartDate, req.Interval)
	}

	// Send jobs
	for _, ticker := range req.Tickers {
		tickerJobs <- ticker
	}
	close(tickerJobs)

	// Wait for all workers
	wg.Wait()
	close(results)

	// Collect results
	finalDetails := make(map[string]string)
	for res := range results {
		for k, v := range res {
			finalDetails[k] = v
		}
	}

	c.JSON(http.StatusOK, DataFetchResponse{
		Status:  "success",
		Message: fmt.Sprintf("Data fetch completed for %d tickers", len(req.Tickers)),
		Details: finalDetails,
	})
}

// fetchWorker processes a single ticker
func (s *Server) fetchWorker(id int, wg *sync.WaitGroup,
	jobs <-chan string, results chan<- map[string]string,
	startDate string, interval string) {

	defer wg.Done()
	for ticker := range jobs {
		slog.Info("Worker processing", "worker_id", id, "ticker", ticker)

		// 1. Find latest timestamp
		latestTime, err := s.getLatestTimestamp(ticker, startDate)
		if err != nil {
			slog.Error("Failed to get latest timestamp", "worker_id", id, "ticker", ticker, "error", err)
			results <- map[string]string{ticker: "Error: " + err.Error()}
			continue
		}

		// 2. Fetch data from Yahoo
		points, err := s.fetchYahooData(ticker, latestTime, interval)
		if err != nil {
			slog.Error("Failed to fetch Yahoo data", "worker_id", id, "ticker", ticker, "error", err)
			results <- map[string]string{ticker: "Error: " + err.Error()}
			continue
		}

		// 3. Write to InfluxDB
		if len(points) > 0 {
			if err := s.WriteAPI.WritePoint(context.Background(), points...); err != nil {
				slog.Error("Failed to write to InfluxDB", "worker_id", id, "ticker", ticker, "error", err)
				results <- map[string]string{ticker: "Error: " + err.Error()}
				continue
			}
			results <- map[string]string{ticker: fmt.Sprintf("%d points written", len(points))}
		} else {
			slog.Info("No new data to write", "worker_id", id, "ticker", ticker)
			results <- map[string]string{ticker: "No new data"}
		}
	}
}

// getLatestTimestamp gets the latest timestamp for a ticker from InfluxDB
func (s *Server) getLatestTimestamp(ticker string, defaultStartDate string) (time.Time, error) {
	// Parse the provided start date
	defaultStartTime, err := time.Parse("2006-01-02", defaultStartDate)
	if err != nil {
		defaultStartTime, err = time.Parse("20060102", defaultStartDate)
		if err != nil {
			// Fallback to 1 year ago
			defaultStartTime = time.Now().AddDate(-1, 0, 0)
		}
	}

	query := fmt.Sprintf(`
        from(bucket: "%s")
          |> range(start: -30d)
          |> filter(fn: (r) => r._measurement == "stock_prices")
          |> filter(fn: (r) => r.ticker == "%s")
          |> last()
    `, influxBucket, ticker)

	result, err := s.QueryAPI.Query(context.Background(), query)
	if err != nil {
		return defaultStartTime, err
	}

	// Guard against nil result (can happen with empty query results)
	if result != nil && result.Next() {
		// Use the later of record time or user's requested start time
		recordTime := result.Record().Time().Add(24 * time.Hour) // +1 day to avoid duplicates
		if recordTime.After(defaultStartTime) {
			return recordTime, nil
		}
	}
	if result != nil && result.Err() != nil {
		return defaultStartTime, result.Err()
	}

	return defaultStartTime, nil
}

// fetchYahooData fetches data from Yahoo Finance
func (s *Server) fetchYahooData(ticker string, startTime time.Time, interval string) ([]*write.Point, error) {
	start := startTime.Unix()
	end := time.Now().Unix()

	if start > end {
		return nil, nil // Start time is in the future
	}

	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?period1=%d&period2=%d&interval=%s&events=history",
		ticker, start, end, interval,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Yahoo API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Yahoo API returned status %s", resp.Status)
	}

	var chartData YahooChartResponse
	if err := json.NewDecoder(resp.Body).Decode(&chartData); err != nil {
		return nil, fmt.Errorf("failed to decode Yahoo JSON: %w", err)
	}

	if chartData.Chart.Error != nil {
		return nil, fmt.Errorf("Yahoo API error: %v", chartData.Chart.Error)
	}

	if len(chartData.Chart.Result) == 0 {
		return nil, fmt.Errorf("no results for ticker %s", ticker)
	}

	var points []*write.Point
	res := chartData.Chart.Result[0]

	if len(res.Indicators.AdjClose) == 0 || len(res.Indicators.Quote) == 0 {
		return nil, fmt.Errorf("incomplete indicators for ticker %s", ticker)
	}

	adjCloseData := res.Indicators.AdjClose[0].AdjClose
	quoteData := res.Indicators.Quote[0]

	for i, ts := range res.Timestamp {
		if len(adjCloseData) <= i ||
			len(quoteData.Close) <= i ||
			len(quoteData.Open) <= i ||
			len(quoteData.High) <= i ||
			len(quoteData.Low) <= i ||
			len(quoteData.Volume) <= i {
			continue
		}

		p := influxdb2.NewPoint(
			"stock_prices",
			map[string]string{
				"ticker": strings.ReplaceAll(ticker, "-USD", "USDT"),
			},
			map[string]interface{}{
				"open":      quoteData.Open[i],
				"high":      quoteData.High[i],
				"low":       quoteData.Low[i],
				"close":     quoteData.Close[i],
				"adj_close": adjCloseData[i],
				"volume":    quoteData.Volume[i],
			},
			time.Unix(ts, 0),
		)
		points = append(points, p)
	}
	return points, nil
}

// handleQueryData queries data from InfluxDB
func (s *Server) handleQueryData(c *gin.Context) {
	var req DataQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body", "details": err.Error()})
		return
	}

	if req.Ticker == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Ticker is required"})
		return
	}

	// Validate ticker to prevent Flux injection
	sanitizedTicker, err := validation.SanitizeTicker(req.Ticker)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ticker", "details": err.Error()})
		return
	}
	req.Ticker = sanitizedTicker

	if req.Days <= 0 {
		req.Days = 252 // Default to 1 year of trading days
	}

	// Build Flux query with optional end_date (prevents look-ahead bias in backtests)
	var query string
	if req.EndDate != "" {
		// BACKTEST MODE: Use end_date as stop parameter
		stopTime := fmt.Sprintf("%sT23:59:59Z", req.EndDate)
		query = fmt.Sprintf(`
			from(bucket: "%s")
			  |> range(start: -%dd, stop: %s)
			  |> filter(fn: (r) => r._measurement == "stock_prices")
			  |> filter(fn: (r) => r.ticker == "%s")
			  |> pivot(rowKey:["_time"], columnKey: ["_field"], valueColumn: "_value")
			  |> sort(columns: ["_time"], desc: false)
		`, influxBucket, req.Days+10, stopTime, req.Ticker)
		slog.Info("Querying InfluxDB (backtest mode)", "ticker", req.Ticker, "days", req.Days, "end_date", req.EndDate)
	} else {
		// LIVE MODE: Query up to now
		query = fmt.Sprintf(`
			from(bucket: "%s")
			  |> range(start: -%dd)
			  |> filter(fn: (r) => r._measurement == "stock_prices")
			  |> filter(fn: (r) => r.ticker == "%s")
			  |> pivot(rowKey:["_time"], columnKey: ["_field"], valueColumn: "_value")
			  |> sort(columns: ["_time"], desc: false)
		`, influxBucket, req.Days+10, req.Ticker)
		slog.Info("Querying InfluxDB (live mode)", "ticker", req.Ticker, "days", req.Days)
	}

	result, err := s.QueryAPI.Query(context.Background(), query)
	if err != nil {
		slog.Error("Query failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Query failed", "details": err.Error()})
		return
	}

	// Guard against nil result (can happen with empty query results)
	if result == nil {
		slog.Warn("Query returned nil result", "ticker", req.Ticker)
		c.JSON(http.StatusOK, DataQueryResponse{Ticker: req.Ticker, Data: []DataPoint{}, Count: 0})
		return
	}

	var dataPoints []DataPoint
	for result.Next() {
		record := result.Record()

		dataPoint := DataPoint{
			Time: record.Time().Format("2006-01-02T15:04:05Z"),
		}

		if val, ok := record.ValueByKey("open").(float64); ok {
			dataPoint.Open = val
		}
		if val, ok := record.ValueByKey("high").(float64); ok {
			dataPoint.High = val
		}
		if val, ok := record.ValueByKey("low").(float64); ok {
			dataPoint.Low = val
		}
		if val, ok := record.ValueByKey("close").(float64); ok {
			dataPoint.Close = val
		}
		if val, ok := record.ValueByKey("adj_close").(float64); ok {
			dataPoint.AdjClose = val
		}
		if val, ok := record.ValueByKey("volume").(int64); ok {
			dataPoint.Volume = val
		}

		dataPoints = append(dataPoints, dataPoint)
	}

	if result.Err() != nil {
		slog.Error("Result iteration error", "error", result.Err())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Query result error", "details": result.Err().Error()})
		return
	}

	// Limit to requested number of days
	if len(dataPoints) > req.Days {
		dataPoints = dataPoints[len(dataPoints)-req.Days:]
	}

	response := DataQueryResponse{
		Ticker: req.Ticker,
		Data:   dataPoints,
		Count:  len(dataPoints),
	}

	slog.Info("Query complete", "ticker", req.Ticker, "points_returned", len(dataPoints))
	c.JSON(http.StatusOK, response)
}
