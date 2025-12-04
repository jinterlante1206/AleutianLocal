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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func runFetchData(cmd *cobra.Command, args []string) {
	tickers := strings.Split(args[0], ",")
	// Calculate start date based on days
	startDate := time.Now().AddDate(0, 0, -fetchDays).Format("2006-01-02")

	fmt.Printf("Fetching data for %v (starting %s)...\n", tickers, startDate)

	payload := map[string]interface{}{
		"names":      tickers,
		"start_date": startDate,
		"interval":   "1d",
	}

	baseURL := getOrchestratorBaseURL()
	url := fmt.Sprintf("%s/v1/data/fetch", baseURL)
	resp := sendPostRequest(url, payload)
	fmt.Println(resp)
}

func runForecast(cmd *cobra.Command, args []string) {
	ticker := args[0]
	fmt.Printf("Forecasting %s using %s...\n", ticker, forecastModel)

	payload := map[string]interface{}{
		"name":                 ticker,
		"context_period_size":  forecastContext,
		"forecast_period_size": forecastHorizon,
		"model":                forecastModel,
	}

	baseURL := getOrchestratorBaseURL()
	url := fmt.Sprintf("%s/v1/timeseries/forecast", baseURL)
	resp := sendPostRequest(url, payload)

	// Pretty print the forecast
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(resp), &result); err == nil {
		if forecast, ok := result["forecast"].([]interface{}); ok {
			fmt.Printf("\nForecast for %s:\n", ticker)
			fmt.Printf("%v\n", forecast)
		} else {
			fmt.Println("Response:", resp)
		}
	} else {
		fmt.Println("Response:", resp)
	}
}
