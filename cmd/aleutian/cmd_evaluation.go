package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/handlers"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func runEvaluation(cmd *cobra.Command, _ []string) {
	// 1. Get the config file path from flags
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		slog.Error("Please provide a configuration file using --config (e.g., --config strategies/spy_threshold_v1.yaml)")
		return
	}

	// 2. Read and Parse the Scenario File (supports local files and URLs)
	scenario, err := loadScenario(configPath)
	if err != nil {
		slog.Error("Failed to load scenario", "source", configPath, "error", err)
		return
	}

	// 2b. Apply CLI overrides
	// Priority: --api-version (new) > --compute-mode (deprecated) > YAML config > default
	computeMode, _ := cmd.Flags().GetString("compute-mode")
	if computeMode != "" {
		slog.Warn("--compute-mode is deprecated, use --api-version instead")
	}

	// Use evalAPIVersion if set to non-default, otherwise fall back to compute-mode
	effectiveAPIVersion := evalAPIVersion
	if effectiveAPIVersion == "" || effectiveAPIVersion == "legacy" {
		// Check if deprecated compute-mode was explicitly set
		if computeMode != "" && computeMode != "legacy" {
			effectiveAPIVersion = computeMode
		}
	}

	// Validate API version
	if effectiveAPIVersion != "" && effectiveAPIVersion != "legacy" && effectiveAPIVersion != "unified" {
		slog.Error("Invalid --api-version. Must be 'legacy' or 'unified'", "value", effectiveAPIVersion)
		return
	}
	if effectiveAPIVersion != "" {
		scenario.Forecast.ComputeMode = effectiveAPIVersion
		slog.Info("CLI override applied", "api-version", effectiveAPIVersion)
	}

	// 2c. Configure service URLs based on deployment mode
	// This sets environment variables that NewEvaluator() reads
	if evalDeploymentMode != "" {
		if evalDeploymentMode != "standalone" && evalDeploymentMode != "distributed" {
			slog.Error("Invalid --deployment-mode. Must be 'standalone' or 'distributed'", "value", evalDeploymentMode)
			return
		}
		slog.Info("Deployment mode configured", "mode", evalDeploymentMode)

		// Set orchestration URL based on deployment mode if not already set
		if os.Getenv("SAPHENEIA_ORCHESTRATION_URL") == "" {
			switch evalDeploymentMode {
			case "standalone":
				_ = os.Setenv("SAPHENEIA_ORCHESTRATION_URL", "http://localhost:12210")
			case "distributed":
				_ = os.Setenv("SAPHENEIA_ORCHESTRATION_URL", "http://sapheneia-orchestration:8000")
			}
		}

		// Set trading URL based on deployment mode if not already set
		if os.Getenv("SAPHENEIA_TRADING_URL") == "" {
			switch evalDeploymentMode {
			case "standalone":
				_ = os.Setenv("SAPHENEIA_TRADING_URL", "http://localhost:12132")
			case "distributed":
				_ = os.Setenv("SAPHENEIA_TRADING_URL", "http://sapheneia-trading:8000")
			}
		}
	}

	// 3. Generate a Unique Run ID
	// Format: {ScenarioID}_v{Version}_{Timestamp}
	timestamp := time.Now().Format("20060102_150405")
	runID := fmt.Sprintf("%s_v%s_%s", scenario.Metadata.ID, scenario.Metadata.Version, timestamp)

	// Determine effective modes for display
	effectiveMode := scenario.Forecast.ComputeMode
	if effectiveMode == "" {
		effectiveMode = "legacy"
	}
	effectiveDeployment := evalDeploymentMode
	if effectiveDeployment == "" {
		effectiveDeployment = "standalone"
	}

	fmt.Printf("\nStarting Evaluation Run: %s\n", runID)
	fmt.Printf("   Strategy:       %s (v%s)\n", scenario.Metadata.ID, scenario.Metadata.Version)
	fmt.Printf("   Model:          %s\n", scenario.Forecast.Model)
	fmt.Printf("   Ticker:         %s\n", scenario.Evaluation.Ticker)
	fmt.Printf("   Range:          %s to %s\n", scenario.Evaluation.StartDate, scenario.Evaluation.EndDate)
	fmt.Printf("   API Version:    %s\n", effectiveMode)
	fmt.Printf("   Deployment:     %s\n", effectiveDeployment)
	fmt.Println("---------------------------------------------------")

	// 4. Ensure required environment variables are set
	if os.Getenv("SAPHENEIA_TRADING_API_KEY") == "" {
		_ = os.Setenv("SAPHENEIA_TRADING_API_KEY", "default_trading_api_key_please_change")
		slog.Warn("SAPHENEIA_TRADING_API_KEY not set, using default")
	}
	if os.Getenv("INFLUXDB_TOKEN") == "" {
		// Default token matches podman-compose.timeseries.yml default
		_ = os.Setenv("INFLUXDB_TOKEN", "aleutian-dev-token-2026")
		slog.Info("Using default InfluxDB token for local development")
	}

	// 5. Initialize Evaluator
	evaluator, err := handlers.NewEvaluator()
	if err != nil {
		slog.Error("Failed to create evaluator", "error", err)
		return
	}
	defer func() {
		if closeErr := evaluator.Close(); closeErr != nil {
			slog.Warn("Failed to close evaluator", "error", closeErr)
		}
	}()

	// 6. Execute the Run using RunScenario
	ctx := context.Background()
	if err := evaluator.RunScenario(ctx, scenario, runID); err != nil {
		slog.Error("Evaluation failed", "error", err)
		return
	}

	fmt.Printf("\n✅ Evaluation completed successfully.\n")
	fmt.Printf("   Run ID: %s\n", runID)
}

func runExport(cmd *cobra.Command, args []string) {
	runID := args[0]

	outputFlag, _ := cmd.Flags().GetString("output")

	// Default filename
	defaultName := fmt.Sprintf("backtest_%s.csv", runID)
	var outputFile string

	if outputFlag == "" {
		outputFile = defaultName
	} else {
		// Check if the provided path is an existing directory
		info, err := os.Stat(outputFlag)
		if err == nil && info.IsDir() {
			// User provided a folder (e.g., ~/Desktop/), so append the filename
			outputFile = filepath.Join(outputFlag, defaultName)
		} else {
			// User provided a full file path (e.g., ~/Desktop/my_results.csv)
			outputFile = outputFlag
		}
	}

	fmt.Printf("Exporting results for Run ID: %s to %s...\n", runID, outputFile)

	// 1. Connect to InfluxDB (Localhost from CLI)
	// Default token matches podman-compose.timeseries.yml default
	token := os.Getenv("INFLUXDB_TOKEN")
	if token == "" {
		token = "aleutian-dev-token-2026"
		slog.Info("Using default InfluxDB token for local development")
	}
	client := influxdb2.NewClient("http://localhost:12130", token)
	defer client.Close()

	queryAPI := client.QueryAPI("aleutian-finance")

	// 2. Query Data
	// Pivot fields so we get a proper table structure
	query := fmt.Sprintf(`
		from(bucket: "financial-data")
		  |> range(start: -10y) 
		  |> filter(fn: (r) => r["_measurement"] == "forecast_evaluations")
		  |> filter(fn: (r) => r["run_id"] == "%s")
		  |> pivot(rowKey:["_time"], columnKey: ["_field"], valueColumn: "_value")
		  |> sort(columns: ["_time"])
	`, runID)

	result, err := queryAPI.Query(context.Background(), query)
	if err != nil {
		slog.Error("InfluxDB query failed", "error", err)
		return
	}

	// 3. Create CSV
	f, err := os.Create(outputFile)
	if err != nil {
		slog.Error("Failed to create output file", "error", err)
		return
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.Warn("Failed to close output file", "error", closeErr)
		}
	}()

	writer := csv.NewWriter(f)
	defer writer.Flush()

	// 4. Write Header
	header := []string{
		"Time", "Ticker", "Model", "Action", "Price", "Forecast",
		"Shares_Traded", "Position_Size", "Cash", "Portfolio_Value", "Reason",
	}
	if err := writer.Write(header); err != nil {
		slog.Error("Failed to write CSV header", "error", err)
		return
	}

	// 5. Write Rows
	count := 0
	for result.Next() {
		r := result.Record()

		// Helpers for safe value extraction
		getFloat := func(k string) string {
			if v, ok := r.ValueByKey(k).(float64); ok {
				return fmt.Sprintf("%.2f", v)
			}
			return "0.00"
		}
		getString := func(k string) string {
			if v, ok := r.ValueByKey(k).(string); ok {
				return v
			}
			return ""
		}

		row := []string{
			r.Time().Format(time.RFC3339),
			getString("ticker"),
			getString("model"),
			getString("action"),
			getFloat("current_price"),
			getFloat("forecast_price"),
			getFloat("size"),
			getFloat("position_after"),
			getFloat("available_cash"),
			// Calculate Portfolio Value (Cash + Stock Value)
			fmt.Sprintf("%.2f", r.ValueByKey("available_cash").(float64)+(r.ValueByKey("position_after").(float64)*r.ValueByKey("current_price").(float64))),
			getString("reason"),
		}
		if err := writer.Write(row); err != nil {
			slog.Error("Failed to write CSV row", "error", err)
			return
		}
		count++
	}

	if result.Err() != nil {
		slog.Error("Error reading query results", "error", result.Err())
		return
	}

	fmt.Printf("✅ Export complete: %d rows written to %s\n", count, outputFile)
}

// loadScenario loads a BacktestScenario from either a local file path or a URL.
//
// Description:
//
//	loadScenario is the entry point for scenario loading that routes to the
//	appropriate loader based on the source prefix. It detects URLs by checking
//	for "http://" or "https://" prefixes.
//
// Inputs:
//   - source: Either a local file path (e.g., "strategies/spy.yaml") or
//     an HTTP(S) URL (e.g., "http://localhost:12210/strategies/spy")
//
// Outputs:
//   - *datatypes.BacktestScenario: Parsed scenario configuration on success
//   - error: Non-nil if loading or parsing fails
//
// Example:
//
//	// Load from local YAML file
//	scenario, err := loadScenario("strategies/spy_threshold_v1.yaml")
//
//	// Load from remote URL (JSON response)
//	scenario, err := loadScenario("http://localhost:12210/strategies/spy_threshold_v1")
//
// Limitations:
//   - URL detection is prefix-based only; malformed URLs pass to HTTP client
//   - No support for other schemes (file://, ftp://, etc.)
//
// Assumptions:
//   - Local files are YAML format
//   - Remote URLs return JSON format
//   - Source string is non-empty (caller should validate)
func loadScenario(source string) (*datatypes.BacktestScenario, error) {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		return loadScenarioFromURL(source)
	}
	return loadScenarioFromFile(source)
}

// loadScenarioFromFile reads and parses a YAML scenario file from the local filesystem.
//
// Description:
//
//	loadScenarioFromFile reads a YAML file from the local filesystem and
//	unmarshals it into a BacktestScenario struct. This is the default loading
//	method for locally-defined strategy configurations.
//
// Inputs:
//   - path: Local file path (relative or absolute) to a YAML scenario file
//
// Outputs:
//   - *datatypes.BacktestScenario: Parsed scenario configuration on success
//   - error: Wrapped os.ReadFile error if file cannot be read, or
//     wrapped yaml.Unmarshal error if YAML is invalid
//
// Example:
//
//	scenario, err := loadScenarioFromFile("strategies/spy_threshold_v1.yaml")
//	if err != nil {
//	    log.Fatalf("Failed to load scenario: %v", err)
//	}
//	fmt.Printf("Loaded strategy: %s v%s\n", scenario.Metadata.ID, scenario.Metadata.Version)
//
// Limitations:
//   - YAML format only; does not support JSON local files
//   - No path validation; relies on OS error handling
//   - No file size limits; large files loaded entirely into memory
//
// Assumptions:
//   - File exists and is readable by the current user
//   - File contains valid YAML matching the BacktestScenario schema
//   - File encoding is UTF-8
func loadScenarioFromFile(path string) (*datatypes.BacktestScenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var scenario datatypes.BacktestScenario
	if err := yaml.Unmarshal(data, &scenario); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return &scenario, nil
}

// loadScenarioFromURL fetches and parses a JSON scenario from a remote URL.
//
// Description:
//
//	loadScenarioFromURL makes an HTTP GET request to the specified URL and
//	parses the JSON response into a BacktestScenario struct. This enables
//	loading strategy configurations hosted by Sapheneia's /strategies endpoint
//	or any other service that returns compatible JSON.
//
// Inputs:
//   - url: HTTP or HTTPS URL to fetch (e.g., "http://localhost:12210/strategies/spy")
//
// Outputs:
//   - *datatypes.BacktestScenario: Parsed scenario configuration on success
//   - error: Network error, HTTP error (non-200), or JSON parse error
//
// Example:
//
//	scenario, err := loadScenarioFromURL("http://localhost:12210/strategies/spy_threshold_v1")
//	if err != nil {
//	    log.Fatalf("Failed to fetch scenario: %v", err)
//	}
//	fmt.Printf("Loaded strategy: %s v%s\n", scenario.Metadata.ID, scenario.Metadata.Version)
//
// Limitations:
//   - JSON format only; does not support YAML responses
//   - No retry logic; single attempt with 30-second timeout
//   - No authentication; cannot access protected endpoints
//   - No caching; fetches fresh on every call
//   - Timeout is hardcoded at 30 seconds
//
// Assumptions:
//   - URL is well-formed and reachable
//   - Server returns HTTP 200 with Content-Type: application/json
//   - Response body contains valid JSON matching BacktestScenario schema
//   - Network latency is acceptable within 30-second timeout
func loadScenarioFromURL(url string) (*datatypes.BacktestScenario, error) {
	slog.Info("Loading scenario from URL", "url", url)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			slog.Debug("Failed to close response body", "error", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			body = []byte("(failed to read body)")
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var scenario datatypes.BacktestScenario
	if err := json.NewDecoder(resp.Body).Decode(&scenario); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	slog.Info("Scenario loaded from URL",
		"id", scenario.Metadata.ID,
		"version", scenario.Metadata.Version,
		"ticker", scenario.Evaluation.Ticker)

	return &scenario, nil
}
