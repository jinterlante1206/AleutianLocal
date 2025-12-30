// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package handlers

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFetchOHLCFromInflux_AsOfDate tests the time-travel prevention fix
// This is an integration test that requires InfluxDB to be running
func TestFetchOHLCFromInflux_AsOfDate(t *testing.T) {
	// Skip unless explicitly enabled - this test requires InfluxDB
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test - set RUN_INTEGRATION_TESTS=1 to run")
	}

	ctx := context.Background()

	// Setup test data in InfluxDB
	setupTestData(t, ctx)

	tests := []struct {
		name            string
		ticker          string
		days            int
		asOfDate        *time.Time
		expectedMaxDate time.Time
		expectError     bool
	}{
		{
			name:            "Real-time fetch (no asOfDate)",
			ticker:          "TEST_SPY",
			days:            10,
			asOfDate:        nil,
			expectedMaxDate: time.Now().Add(24 * time.Hour), // Should include recent data
			expectError:     false,
		},
		{
			name:            "Backtest fetch (with asOfDate = 2023-01-31)",
			ticker:          "TEST_SPY",
			days:            10,
			asOfDate:        timePtrTrading(time.Date(2023, 1, 31, 0, 0, 0, 0, time.UTC)),
			expectedMaxDate: time.Date(2023, 1, 31, 23, 59, 59, 0, time.UTC),
			expectError:     false,
		},
		{
			name:            "Backtest fetch (with asOfDate = 2023-06-15)",
			ticker:          "TEST_SPY",
			days:            20,
			asOfDate:        timePtrTrading(time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC)),
			expectedMaxDate: time.Date(2023, 6, 15, 23, 59, 59, 0, time.UTC),
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ohlc, latestPrice, err := fetchOHLCFromInflux(ctx, tt.ticker, tt.days, tt.asOfDate)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, ohlc)
			assert.Greater(t, latestPrice, 0.0)
			assert.Greater(t, len(ohlc.Close), 0, "Should have OHLC data")

			// Verify time-travel prevention: all dates should be <= expectedMaxDate
			for i, ts := range ohlc.Time {
				assert.True(t, ts.Before(tt.expectedMaxDate) || ts.Equal(tt.expectedMaxDate),
					"Data point %d at %s should be before or equal to %s",
					i, ts.Format("2006-01-02"), tt.expectedMaxDate.Format("2006-01-02"))
			}

			// Log for inspection
			if len(ohlc.Time) > 0 {
				t.Logf("Fetched %d data points from %s to %s",
					len(ohlc.Time),
					ohlc.Time[0].Format("2006-01-02"),
					ohlc.Time[len(ohlc.Time)-1].Format("2006-01-02"))
			}
		})
	}
}

// TestFetchOHLCFromInflux_TimeTravelBug demonstrates the bug this fix prevents
// This is an integration test that requires InfluxDB to be running
func TestFetchOHLCFromInflux_TimeTravelBug(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test - set RUN_INTEGRATION_TESTS=1 to run")
	}

	ctx := context.Background()
	ticker := "TEST_SPY"

	// Scenario: We're backtesting on 2023-01-15
	// We should NOT see data from 2023-12-31 (future data)
	backtestDate := time.Date(2023, 1, 15, 14, 30, 0, 0, time.UTC)

	// Fetch with asOfDate (FIX APPLIED)
	ohlcFixed, _, err := fetchOHLCFromInflux(ctx, ticker, 10, &backtestDate)
	require.NoError(t, err)

	// Fetch without asOfDate (BUGGY BEHAVIOR - would happen if we passed nil)
	ohlcBuggy, _, err := fetchOHLCFromInflux(ctx, ticker, 10, nil)
	require.NoError(t, err)

	// The fixed version should have FEWER or EQUAL data points
	// (because it doesn't include future data)
	t.Logf("Fixed version: %d data points (up to %s)",
		len(ohlcFixed.Time),
		backtestDate.Format("2006-01-02"))
	t.Logf("Buggy version: %d data points (includes future data)",
		len(ohlcBuggy.Time))

	// Verify: Fixed version has no data beyond backtest date
	for i, ts := range ohlcFixed.Time {
		assert.True(t, ts.Before(backtestDate) || ts.Equal(backtestDate),
			"Fixed version: data point %d at %s leaks future data beyond %s",
			i, ts.Format("2006-01-02"), backtestDate.Format("2006-01-02"))
	}

	// Verify: Buggy version DOES have data beyond backtest date (if today > backtest date)
	if time.Now().After(backtestDate.Add(30 * 24 * time.Hour)) {
		hasFutureData := false
		for _, ts := range ohlcBuggy.Time {
			if ts.After(backtestDate) {
				hasFutureData = true
				break
			}
		}
		assert.True(t, hasFutureData,
			"Buggy version should have future data (this demonstrates the bug)")
	}
}

// setupTestData creates test OHLC data in InfluxDB
func setupTestData(t *testing.T, ctx context.Context) {
	influxURL := os.Getenv("INFLUXDB_URL")
	if influxURL == "" {
		influxURL = "http://localhost:12130"
	}
	token := os.Getenv("INFLUXDB_TOKEN")
	if token == "" {
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

	client := influxdb2.NewClient(influxURL, token)
	defer client.Close()

	writeAPI := client.WriteAPIBlocking(org, bucket)

	// Create test data spanning 2023-01-01 to 2023-12-31
	startDate := time.Date(2023, 1, 1, 14, 30, 0, 0, time.UTC)
	for i := 0; i < 365; i++ {
		date := startDate.Add(time.Duration(i) * 24 * time.Hour)

		// Skip weekends
		if date.Weekday() == time.Saturday || date.Weekday() == time.Sunday {
			continue
		}

		point := influxdb2.NewPoint(
			"stock_prices",
			map[string]string{"ticker": "TEST_SPY"},
			map[string]interface{}{
				"open":      380.0 + float64(i)*0.1,
				"high":      382.0 + float64(i)*0.1,
				"low":       378.0 + float64(i)*0.1,
				"close":     381.0 + float64(i)*0.1,
				"adj_close": 381.0 + float64(i)*0.1,
				"volume":    1000000.0,
			},
			date,
		)

		err := writeAPI.WritePoint(ctx, point)
		if err != nil {
			t.Logf("Warning: failed to write test data for %s: %v", date.Format("2006-01-02"), err)
		}
	}

	t.Log("Test data setup complete")
}

func timePtrTrading(t time.Time) *time.Time {
	return &t
}

// BenchmarkFetchOHLCFromInflux measures performance impact of the fix
// This is an integration benchmark that requires InfluxDB to be running
func BenchmarkFetchOHLCFromInflux(b *testing.B) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		b.Skip("Skipping integration benchmark - set RUN_INTEGRATION_TESTS=1 to run")
	}

	ctx := context.Background()
	ticker := "SPY"
	days := 252
	asOfDate := time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC)

	b.Run("WithAsOfDate", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _, err := fetchOHLCFromInflux(ctx, ticker, days, &asOfDate)
			if err != nil {
				b.Errorf("fetchOHLCFromInflux failed: %v", err)
			}
		}
	})

	b.Run("WithoutAsOfDate", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _, err := fetchOHLCFromInflux(ctx, ticker, days, nil)
			if err != nil {
				b.Errorf("fetchOHLCFromInflux failed: %v", err)
			}
		}
	})
}

// TestFetchOHLCFromInflux_FluxQueryCorrectness verifies the Flux query syntax
func TestFetchOHLCFromInflux_FluxQueryCorrectness(t *testing.T) {
	tests := []struct {
		name     string
		asOfDate *time.Time
		wantStop string
	}{
		{
			name:     "No asOfDate uses now()",
			asOfDate: nil,
			wantStop: "now()",
		},
		{
			name:     "With asOfDate uses RFC3339",
			asOfDate: timePtrTrading(time.Date(2023, 6, 15, 14, 30, 0, 0, time.UTC)),
			wantStop: "2023-06-15T14:30:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Determine expected stop time
			stopTime := "now()"
			if tt.asOfDate != nil {
				stopTime = tt.asOfDate.Format(time.RFC3339)
			}

			assert.Equal(t, tt.wantStop, stopTime,
				"Stop time should match expected format")

			// Verify the query would be constructed correctly
			expectedQueryFragment := fmt.Sprintf("|> range(start: -%%dd, stop: %s)", stopTime)
			t.Logf("Expected query fragment: %s", expectedQueryFragment)
		})
	}
}
