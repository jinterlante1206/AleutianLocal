package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/spf13/cobra"
)

func TestForecastCommandPayload(t *testing.T) {
	// 1. Setup Mock
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/timeseries/forecast" {
			t.Errorf("Expected /v1/timeseries/forecast, got %s", r.URL.Path)
		}

		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)

		if body["name"] != "AAPL" {
			t.Errorf("Expected ticker AAPL, got %v", body["name"])
		}
		if body["context_period_size"] != float64(90) { // JSON numbers are floats
			t.Errorf("Expected context 90, got %v", body["context_period_size"])
		}

		// Return mock forecast
		json.NewEncoder(w).Encode(map[string]interface{}{
			"forecast": []float64{100.1, 100.2, 100.3},
		})
	}))
	defer mockServer.Close()

	// 2. Inject Mock URL via Env Var
	os.Setenv("ALEUTIAN_ORCHESTRATOR_URL", mockServer.URL)
	defer os.Unsetenv("ALEUTIAN_ORCHESTRATOR_URL")

	// 3. Set global flags (simulating cobra flags)
	// In your real code, these are globals in main.go. We modify them for the test.
	forecastModel = "test-model"
	forecastHorizon = 10
	forecastContext = 90

	// 4. Capture Stdout (Optional, or just ensure no panic)
	// For now, we just run it to ensure the request is valid
	cmd := &cobra.Command{}
	runForecast(cmd, []string{"AAPL"})
}
