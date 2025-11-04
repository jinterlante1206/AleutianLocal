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
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
)

// Create a new tracer
var timeseriesTracer = otel.Tracer("aleutian.orchestrator.handlers")

// HandleTimeSeriesForecast proxies requests to the Python timeseries-analysis-service
func HandleTimeSeriesForecast() gin.HandlerFunc { // <-- RENAMED
	return func(c *gin.Context) {
		ctx, span := timeseriesTracer.Start(c.Request.Context(), "HandleTimeSeriesForecast")
		defer span.End()

		// 1. Get the URL from the environment
		// --- READ THE NEW ENV VAR ---
		serviceURL := os.Getenv("ALEUTIAN_TIMESERIES_TOOL")
		if serviceURL == "" {
			slog.Error("ALEUTIAN_TIMESERIES_TOOL env var not set")
			span.RecordError(fmt.Errorf("ALEUTIAN_TIMESERIES_TOOL not set"))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Time Series service not configured"})
			return
		}
		targetURL := fmt.Sprintf("%s/v1/timeseries/forecast", serviceURL)

		// 2. Read the raw request body
		reqBodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			slog.Error("Failed to read request body", "error", err)
			span.RecordError(err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}

		// 3. Create and send the proxy request
		slog.Info("Proxying time series forecast request", "target_url", targetURL)
		httpReq, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewBuffer(reqBodyBytes))
		// ... (rest of the function is identical) ...
		if err != nil {
			slog.Error("Failed to create request for time series service", "error", err)
			span.RecordError(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create time series service request"})
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")

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
