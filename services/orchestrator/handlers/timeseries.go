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
	"regexp"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/pkg/validation"
	"github.com/gin-gonic/gin"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"go.opentelemetry.io/otel"
)

// Create a new tracer
var timeseriesTracer = otel.Tracer("aleutian.orchestrator.handlers")

// Request structure to inspect and augment the request
type TimeSeriesRequest struct {
	Model              string    `json:"model"`
	Name               string    `json:"name,omitempty"`                 // Ticker, e.g. "SPY"
	Data               []float64 `json:"data,omitempty"`                 // Actual history
	RecentData         []float64 `json:"recent_data,omitempty"`          // Alias for Data
	ContextPeriodSize  int       `json:"context_period_size,omitempty"`  // How much history to fetch
	ForecastPeriodSize int       `json:"forecast_period_size,omitempty"` // Horizon (days to forecast)
	Horizon            int       `json:"horizon,omitempty"`              // Alias for ForecastPeriodSize (standalone mode)
	NumSamples         int       `json:"num_samples,omitempty"`          // Number of sample paths
	AsOfDate           string    `json:"as_of_date,omitempty"`           // Date for forecast reference
}

// normalizeModelName converts a display name or huggingface ID to a standard "slug"
func normalizeModelName(input string) string {
	s := strings.ToLower(input)
	if idx := strings.LastIndex(s, "/"); idx != -1 {
		s = s[idx+1:]
	}
	reg := regexp.MustCompile("[^a-z0-9]+")
	s = reg.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// Helper to resolve the target URL based on the model name
func getSerivceURL(modelName string) (string, error) {
	defaultURL := os.Getenv("ALEUTIAN_TIMESERIES_TOOL")
	if defaultURL == "" {
		defaultURL = "http://forecast-service:8000"
	}

	forecastMode := os.Getenv("ALEUTIAN_FORECAST_MODE")
	if forecastMode == "standalone" {
		// Standalone mode: all models go to the unified service
		return defaultURL, nil
	}

	if modelName == "" {
		slog.Error("Model Name was not set")
		return defaultURL, nil
	}
	slug := normalizeModelName(modelName)

	envVarKey := fmt.Sprintf("TIMESERIES_SERVICE_%s", strings.ReplaceAll(strings.ToUpper(slug), "-", "_"))
	if override := os.Getenv(envVarKey); override != "" {
		return override, nil
	}

	// Dynamic Routing Logic (Sapheneia mode)
	switch slug {
	// --- AMAZON CHRONOS T5 ---
	case "chronos-t5-tiny":
		return "http://forecast-chronos-t5-tiny:8000", nil
	case "chronos-t5-mini":
		return "http://forecast-chronos-t5-mini:8000", nil
	case "chronos-t5-small":
		return "http://forecast-chronos-t5-small:8000", nil
	case "chronos-t5-base":
		return "http://forecast-chronos-t5-base:8000", nil
	case "chronos-t5-large":
		return "http://forecast-chronos-t5-large:8000", nil

	// --- AMAZON CHRONOS BOLT ---
	case "chronos-bolt-mini":
		return "http://forecast-chronos-bolt-mini:8000", nil
	case "chronos-bolt-small":
		return "http://forecast-chronos-bolt-small:8000", nil
	case "chronos-bolt-base":
		return "http://forecast-chronos-bolt-base:8000", nil

	// --- GOOGLE TIMESFM ---
	case "timesfm-1-0":
		return "http://forecast-timesfm-1-0:8000", nil
	case "timesfm-2-0":
		return "http://forecast-timesfm-2-0:8000", nil
	case "timesfm-2-5":
		return "http://forecast-timesfm-2-5:8000", nil

	// --- SALESFORCE MOIRAI ---
	case "moirai-1-1-small":
		return "http://forecast-moirai-1-1-small:8000", nil
	case "moirai-1-1-base":
		return "http://forecast-moirai-1-1-base:8000", nil
	case "moirai-1-1-large":
		return "http://forecast-moirai-1-1-large:8000", nil
	case "moirai-2-0-small":
		return "http://forecast-moirai-2-0-small:8000", nil
	case "moirai-1-0-small":
		return "http://forecast-moirai-1-0-small:8000", nil

	// --- IBM GRANITE ---
	case "granite-ttm-r1":
		return "http://forecast-granite-ttm-r1:8000", nil
	case "granite-ttm-r2":
		return "http://forecast-granite-ttm-r2:8000", nil
	case "granite-flowstate":
		return "http://forecast-granite-flowstate:8000", nil
	case "granite-patchtsmixer":
		return "http://forecast-granite-patchtsmixer:8000", nil
	case "granite-patchtst":
		return "http://forecast-granite-patchtst:8000", nil

	// --- AUTONLAB MOMENT ---
	case "moment-small":
		return "http://forecast-moment-small:8000", nil
	case "moment-base":
		return "http://forecast-moment-base:8000", nil
	case "moment-large":
		return "http://forecast-moment-large:8000", nil

	// --- ALIBABA YINGLONG ---
	case "yinglong-6m":
		return "http://forecast-yinglong-6m:8000", nil
	case "yinglong-50m":
		return "http://forecast-yinglong-50m:8000", nil
	case "yinglong-110m":
		return "http://forecast-yinglong-110m:8000", nil
	case "yinglong-300m":
		return "http://forecast-yinglong-300m:8000", nil

	// --- MISC / SINGLE MODELS ---
	case "lag-llama":
		return "http://forecast-lag-llama:8000", nil
	case "kairos-10m":
		return "http://forecast-kairos-10m:8000", nil
	case "kairos-50m":
		return "http://forecast-kairos-50m:8000", nil
	case "timemoe-200m":
		return "http://forecast-timemoe-200m:8000", nil
	case "timer":
		return "http://forecast-timer:8000", nil
	case "sundial":
		return "http://forecast-sundial:8000", nil
	case "toto":
		return "http://forecast-toto:8000", nil
	case "falcon-tst":
		return "http://forecast-falcon-tst:8000", nil
	case "tempopfn":
		return "http://forecast-tempopfn:8000", nil
	case "forecastpfn":
		return "http://forecast-forecastpfn:8000", nil
	case "chattime":
		return "http://forecast-chattime:8000", nil
	case "opencity":
		return "http://forecast-opencity:8000", nil
	case "units":
		return "http://forecast-units:8000", nil

	// --- EARTH / WEATHER ---
	case "prithvi-2-0-eo":
		return "http://forecast-prithvi-2-0-eo:8000", nil
	case "atmorep":
		return "http://forecast-atmorep:8000", nil
	case "earthpt":
		return "http://forecast-earthpt:8000", nil
	case "graphcast":
		return "http://forecast-graphcast:8000", nil
	case "fourcastnet":
		return "http://forecast-fourcastnet:8000", nil
	case "pangu-weather":
		return "http://forecast-pangu-weather:8000", nil
	case "climax":
		return "http://forecast-climax:8000", nil

	default:
		slog.Warn("Unknown model requested, falling back to default", "model", modelName, "slug", slug)
		return defaultURL, nil
	}
}

// fetchHistoryForForecast retrieves close prices from InfluxDB
func fetchHistoryForForecast(ctx context.Context, ticker string, count int) ([]float64, error) {
	// Validate ticker to prevent Flux injection
	if err := validation.ValidateTicker(ticker); err != nil {
		return nil, fmt.Errorf("invalid ticker: %w", err)
	}

	// InfluxDB connection setup
	influxURL := os.Getenv("INFLUXDB_URL")
	if influxURL == "" {
		influxURL = "http://influxdb:8086" // Internal default
	}
	influxToken := os.Getenv("INFLUXDB_TOKEN")
	influxOrg := os.Getenv("INFLUXDB_ORG")
	influxBucket := os.Getenv("INFLUXDB_BUCKET")

	if influxToken == "" || influxOrg == "" || influxBucket == "" {
		return nil, fmt.Errorf("InfluxDB credentials not fully configured")
	}

	client := influxdb2.NewClient(influxURL, influxToken)
	defer client.Close()
	queryAPI := client.QueryAPI(influxOrg)

	// Calculate fetch range: context * 2 to account for weekends/holidays
	calendarDays := int(float64(count) * 2.0)
	if calendarDays < 14 {
		calendarDays = 14 // Minimum fetch
	}

	query := fmt.Sprintf(`
		from(bucket: "%s")
		  |> range(start: -%dd)
		  |> filter(fn: (r) => r._measurement == "stock_prices")
		  |> filter(fn: (r) => r.ticker == "%s")
		  |> filter(fn: (r) => r._field == "close")
		  |> sort(columns: ["_time"], desc: false)
		  |> tail(n: %d)
	`, influxBucket, calendarDays, ticker, count)

	slog.Info("Fetching history for forecast injection", "ticker", ticker, "days_needed", count)

	result, err := queryAPI.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("InfluxDB query error: %w", err)
	}

	var prices []float64
	for result.Next() {
		if val, ok := result.Record().Value().(float64); ok {
			prices = append(prices, val)
		}
	}

	if result.Err() != nil {
		return nil, result.Err()
	}

	if len(prices) == 0 {
		return nil, fmt.Errorf("no data found for ticker %s", ticker)
	}
	return prices, nil
}

// HandleTimeSeriesForecast proxies requests to the Python timeseries-analysis-service
// It also fetches historical data if only a ticker symbol is provided.
func HandleTimeSeriesForecast() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, span := timeseriesTracer.Start(c.Request.Context(), "HandleTimeSeriesForecast")
		defer span.End()

		// 1. Read the raw request body
		reqBodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			slog.Error("Failed to read request body", "error", err)
			span.RecordError(err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}

		// 2. Unmarshal to inspect
		var req TimeSeriesRequest
		if err := json.Unmarshal(reqBodyBytes, &req); err != nil {
			slog.Error("Invalid JSON", "error", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON format"})
			return
		}

		// 3. INTELLIGENT FETCH: If data is missing but Ticker/Name exists, fetch from Influx
		dataLen := len(req.Data)
		if dataLen == 0 && len(req.RecentData) > 0 {
			dataLen = len(req.RecentData)
			req.Data = req.RecentData
		}

		if dataLen == 0 && req.Name != "" {
			// Determine how much history to fetch
			ctxSize := req.ContextPeriodSize
			if ctxSize <= 0 {
				ctxSize = 252 // Default to 1 trading year if not specified
			}

			slog.Info("Injecting historical data for forecast", "ticker", req.Name, "points", ctxSize)
			history, err := fetchHistoryForForecast(ctx, req.Name, ctxSize)
			if err != nil {
				slog.Error("Failed to fetch history for forecast", "ticker", req.Name, "error", err)
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Failed to fetch history for %s: %v", req.Name, err)})
				return
			}
			// Update the request object
			req.Data = history
			req.RecentData = history // Set both for compatibility

			// Re-marshal the body to send to Python
			reqBodyBytes, err = json.Marshal(req)
			if err != nil {
				slog.Error("Failed to marshal updated request", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal error processing request"})
				return
			}
		}

		// 4. Resolve routing
		baseURL, err := getSerivceURL(req.Model)
		if err != nil {
			slog.Error("Routing error", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Service configuration error"})
			return
		}
		targetURL := fmt.Sprintf("%s/v1/timeseries/forecast", baseURL)

		// 5. Proxy the (potentially modified) request
		slog.Info("Proxying time series forecast request", "target_url", targetURL, "model", req.Model)
		httpReq, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewBuffer(reqBodyBytes))
		if err != nil {
			slog.Error("Failed to create request for time series service", "error", err)
			span.RecordError(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create time series service request"})
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")

		// Inject the API key so Sapheneia accepts the request
		apiKey := os.Getenv("SAPHENEIA_TRADING_API_KEY")
		if apiKey != "" {
			httpReq.Header.Set("X-API-Key", apiKey)
			httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			slog.Error("Failed to call time series service", "url", targetURL, "error", err)
			span.RecordError(err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Failed to connect to the time series service"})
			return
		}
		defer resp.Body.Close()

		// 6. Stream the response back
		c.Status(resp.StatusCode)
		for k, v := range resp.Header {
			c.Header(k, strings.Join(v, ","))
		}
		_, err = io.Copy(c.Writer, resp.Body)
		if err != nil {
			slog.Error("Failed to write time series service response to client", "error", err)
		}
	}
}

func HandleDataFetch() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. Get the URL from the environment
		serviceURL := os.Getenv("ALEUTIAN_DATA_FETCHER_URL")
		if serviceURL == "" {
			slog.Error("ALEUTIAN_DATA_FETCHER_URL env var not set")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Data fetching service not configured"})
			return
		}
		targetURL := fmt.Sprintf("%s/v1/data/fetch", serviceURL) // Points to the Gin handler in main.go

		// 2. Read the raw request body
		reqBodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			slog.Error("Failed to read request body", "error", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}

		// 3. Create and send the proxy request
		slog.Info("Proxying data fetch request", "target_url", targetURL)
		httpReq, err := http.NewRequestWithContext(c.Request.Context(), "POST", targetURL, bytes.NewBuffer(reqBodyBytes))
		if err != nil {
			slog.Error("Failed to create request for data fetch service", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create data fetch service request"})
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			slog.Error("Failed to call data fetch service", "url", targetURL, "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Failed to connect to the data fetch service"})
			return
		}
		defer resp.Body.Close()

		// 4. Stream the response
		c.Status(resp.StatusCode)
		for k, v := range resp.Header {
			c.Header(k, strings.Join(v, ","))
		}
		_, _ = io.Copy(c.Writer, resp.Body)
	}
}
