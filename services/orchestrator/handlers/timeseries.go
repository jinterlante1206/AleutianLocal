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
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
)

// Create a new tracer
var timeseriesTracer = otel.Tracer("aleutian.orchestrator.handlers")

// Request structure to inspect just the routing key
type TimeSeriesRoutingRequest struct {
	Model string `json:"model"`
}

// normalizeModelName converts a display name or huggingface ID to a standard "slug"
// e.g.: "Chronos T5 (Tiny)" -> "chronos-t5-tiny"
func normalizeModelName(input string) string {
	// Lowercase everything
	s := strings.ToLower(input)
	// Remove the prefix
	if idx := strings.LastIndex(s, "/"); idx != -1 {
		s = s[idx+1:]
	}
	// Remove non-alphanumeric characters except hyphens
	reg := regexp.MustCompile("[^a-z0-9]+")
	s = reg.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// Helper to resolve the target URL based on the model name
func getSerivceURL(modelName string) (string, error) {
	// Default URL (the primary container)
	defaultURL := os.Getenv("ALEUTIAN_TIMESERIES_TOOL")
	if defaultURL == "" {
		defaultURL = "http://forecast-service:8000"
	}

	// Check forecast mode - standalone uses unified service, sapheneia uses per-model routing
	forecastMode := os.Getenv("ALEUTIAN_FORECAST_MODE")
	if forecastMode == "standalone" {
		// In standalone mode, all models go to the unified forecast service
		// The Python service handles model loading internally
		slog.Info("Standalone mode: routing to unified forecast service", "model", modelName, "url", defaultURL)
		return defaultURL, nil
	}

	// Sapheneia mode (or unset) - use per-model routing
	if modelName == "" {
		slog.Error("Model Name was not set")
		return defaultURL, nil
	}
	// normalize the model name
	slug := normalizeModelName(modelName)

	// Check for a specific Env Variable Override for the IP for a specific model
	envVarKey := fmt.Sprintf("TIMESERIES_SERVICE_%s", strings.ReplaceAll(strings.ToUpper(slug),
		"-", "_"))
	if override := os.Getenv(envVarKey); override != "" {
		slog.Info("Using environment override for model", "model", modelName, "url", override)
		return override, nil
	}

	// Dynamic Routing Logic (Sapheneia mode)
	// Model compatibility status (see docs/model_compatibility.md):
	//   [VERIFIED]  = Tested and confirmed working
	//   [UNTESTED]  = Listed but not yet verified
	//   [BROKEN]    = Known issues, do not use
	//
	// In sapheneia mode, each model routes to its dedicated container.
	switch slug {
	// --- AMAZON CHRONOS T5 --- [VERIFIED]
	case "chronos-t5-tiny": // [VERIFIED] 0.5GB VRAM
		return "http://forecast-chronos-t5-tiny:8000", nil
	case "chronos-t5-mini": // [VERIFIED] 1.0GB VRAM
		return "http://forecast-chronos-t5-mini:8000", nil
	case "chronos-t5-small": // [VERIFIED] 2.0GB VRAM
		return "http://forecast-chronos-t5-small:8000", nil
	case "chronos-t5-base": // [VERIFIED] 4.0GB VRAM
		return "http://forecast-chronos-t5-base:8000", nil
	case "chronos-t5-large": // [VERIFIED] 8.0GB VRAM
		return "http://forecast-chronos-t5-large:8000", nil

	// --- AMAZON CHRONOS BOLT --- [BROKEN]
	case "chronos-bolt-mini": // [BROKEN] Do not use
		return "http://forecast-chronos-bolt-mini:8000", nil
	case "chronos-bolt-small": // [BROKEN] Do not use
		return "http://forecast-chronos-bolt-small:8000", nil
	case "chronos-bolt-base": // [BROKEN] Do not use
		return "http://forecast-chronos-bolt-base:8000", nil

	// --- GOOGLE TIMESFM --- [UNTESTED]
	case "timesfm-1-0": // [UNTESTED] Priority for testing
		return "http://forecast-timesfm-1-0:8000", nil
	case "timesfm-2-0": // [UNTESTED]
		return "http://forecast-timesfm-2-0:8000", nil
	case "timesfm-2-5": // [UNTESTED]
		return "http://forecast-timesfm-2-5:8000", nil

	// --- SALESFORCE MOIRAI --- [UNTESTED]
	case "moirai-1-1-small": // [UNTESTED]
		return "http://forecast-moirai-1-1-small:8000", nil
	case "moirai-1-1-base": // [UNTESTED]
		return "http://forecast-moirai-1-1-base:8000", nil
	case "moirai-1-1-large": // [UNTESTED]
		return "http://forecast-moirai-1-1-large:8000", nil
	case "moirai-2-0-small": // [UNTESTED]
		return "http://forecast-moirai-2-0-small:8000", nil
	case "moirai-1-0-small": // [UNTESTED] (legacy slug)
		return "http://forecast-moirai-1-0-small:8000", nil

	// --- IBM GRANITE --- [UNTESTED]
	case "granite-ttm-r1": // [UNTESTED]
		return "http://forecast-granite-ttm-r1:8000", nil
	case "granite-ttm-r2": // [UNTESTED]
		return "http://forecast-granite-ttm-r2:8000", nil
	case "granite-flowstate": // [UNTESTED]
		return "http://forecast-granite-flowstate:8000", nil
	case "granite-patchtsmixer": // [UNTESTED]
		return "http://forecast-granite-patchtsmixer:8000", nil
	case "granite-patchtst": // [UNTESTED]
		return "http://forecast-granite-patchtst:8000", nil

	// --- AUTONLAB MOMENT --- [UNTESTED]
	case "moment-small": // [UNTESTED]
		return "http://forecast-moment-small:8000", nil
	case "moment-base": // [UNTESTED]
		return "http://forecast-moment-base:8000", nil
	case "moment-large": // [UNTESTED]
		return "http://forecast-moment-large:8000", nil

	// --- ALIBABA YINGLONG --- [UNTESTED]
	case "yinglong-6m": // [UNTESTED]
		return "http://forecast-yinglong-6m:8000", nil
	case "yinglong-50m": // [UNTESTED]
		return "http://forecast-yinglong-50m:8000", nil
	case "yinglong-110m": // [UNTESTED]
		return "http://forecast-yinglong-110m:8000", nil
	case "yinglong-300m": // [UNTESTED]
		return "http://forecast-yinglong-300m:8000", nil

	// --- MISC / SINGLE MODELS --- [UNTESTED]
	case "lag-llama": // [UNTESTED]
		return "http://forecast-lag-llama:8000", nil
	case "kairos-10m": // [UNTESTED]
		return "http://forecast-kairos-10m:8000", nil
	case "kairos-50m": // [UNTESTED]
		return "http://forecast-kairos-50m:8000", nil
	case "timemoe-200m": // [UNTESTED]
		return "http://forecast-timemoe-200m:8000", nil
	case "timer": // [UNTESTED]
		return "http://forecast-timer:8000", nil
	case "sundial": // [UNTESTED]
		return "http://forecast-sundial:8000", nil
	case "toto": // [UNTESTED]
		return "http://forecast-toto:8000", nil
	case "falcon-tst": // [UNTESTED]
		return "http://forecast-falcon-tst:8000", nil
	case "tempopfn": // [UNTESTED]
		return "http://forecast-tempopfn:8000", nil
	case "forecastpfn": // [UNTESTED]
		return "http://forecast-forecastpfn:8000", nil
	case "chattime": // [UNTESTED]
		return "http://forecast-chattime:8000", nil
	case "opencity": // [UNTESTED]
		return "http://forecast-opencity:8000", nil
	case "units": // [UNTESTED]
		return "http://forecast-units:8000", nil

	// --- EARTH / WEATHER --- [UNTESTED]
	case "prithvi-2-0-eo": // [UNTESTED]
		return "http://forecast-prithvi-2-0-eo:8000", nil
	case "atmorep": // [UNTESTED]
		return "http://forecast-atmorep:8000", nil
	case "earthpt": // [UNTESTED]
		return "http://forecast-earthpt:8000", nil
	case "graphcast": // [UNTESTED]
		return "http://forecast-graphcast:8000", nil
	case "fourcastnet": // [UNTESTED]
		return "http://forecast-fourcastnet:8000", nil
	case "pangu-weather": // [UNTESTED]
		return "http://forecast-pangu-weather:8000", nil
	case "climax": // [UNTESTED]
		return "http://forecast-climax:8000", nil

	default:
		slog.Warn("Unknown model requested, falling back to default", "model", modelName,
			"slug", slug)
		return defaultURL, nil
	}
}

// HandleTimeSeriesForecast proxies requests to the Python timeseries-analysis-service
func HandleTimeSeriesForecast() gin.HandlerFunc { // <-- RENAMED
	return func(c *gin.Context) {
		ctx, span := timeseriesTracer.Start(c.Request.Context(), "HandleTimeSeriesForecast")
		defer span.End()

		// Read the raw request body
		reqBodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			slog.Error("Failed to read request body", "error", err)
			span.RecordError(err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}

		// Peek at the "model" field
		var routingReq TimeSeriesRoutingRequest
		_ = json.Unmarshal(reqBodyBytes, &routingReq)
		baseURL, err := getSerivceURL(routingReq.Model)
		if err != nil {
			slog.Error("Routing error", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Service configuration error"})
			return
		}
		targetURL := fmt.Sprintf("%s/v1/timeseries/forecast", baseURL)
		slog.Info("Proxying time series forecast request", "target_url", targetURL)

		// 3. Create and send the proxy request
		slog.Info("Proxying time series forecast request", "target_url", targetURL)
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

		// 4. Stream the response
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
