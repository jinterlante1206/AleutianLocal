package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/jinterlante1206/AleutianLocal/pkg/ux"
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

	// 2. Read and Parse the Scenario File
	data, err := os.ReadFile(configPath)
	if err != nil {
		slog.Error("Failed to read config file", "path", configPath, "error", err)
		return
	}

	var scenario datatypes.BacktestScenario
	if err := yaml.Unmarshal(data, &scenario); err != nil {
		slog.Error("Failed to parse YAML config", "error", err)
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

	// 4. Ensure API Key is set
	if os.Getenv("SAPHENEIA_TRADING_API_KEY") == "" {
		_ = os.Setenv("SAPHENEIA_TRADING_API_KEY", "default_trading_api_key_please_change")
		slog.Warn("SAPHENEIA_TRADING_API_KEY not set, using default")
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
	if err := evaluator.RunScenario(ctx, &scenario, runID); err != nil {
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
	// Token is auto-generated by 'aleutian stack start' and stored as podman secret
	token := os.Getenv("INFLUXDB_TOKEN")
	if token == "" {
		ux.Error("INFLUXDB_TOKEN not set. Run 'aleutian stack start' first to auto-generate it.")
		return
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
