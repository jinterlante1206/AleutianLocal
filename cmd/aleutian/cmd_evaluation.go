package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/handlers"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func runEvaluation(cmd *cobra.Command, args []string) {
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

	// 3. Generate a Unique Run ID
	// Format: {ScenarioID}_v{Version}_{Timestamp}
	timestamp := time.Now().Format("20060102_150405")
	runID := fmt.Sprintf("%s_v%s_%s", scenario.Metadata.ID, scenario.Metadata.Version, timestamp)

	fmt.Printf("\n🚀 Starting Evaluation Run: %s\n", runID)
	fmt.Printf("   Strategy: %s (v%s)\n", scenario.Metadata.ID, scenario.Metadata.Version)
	fmt.Printf("   Ticker:   %s\n", scenario.Evaluation.Ticker)
	fmt.Printf("   Range:    %s to %s\n", scenario.Evaluation.StartDate, scenario.Evaluation.EndDate)
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
	defer evaluator.Close()

	// 6. Execute the Run using RunScenario
	ctx := context.Background()
	if err := evaluator.RunScenario(ctx, &scenario, runID); err != nil {
		slog.Error("Evaluation failed", "error", err)
		return
	}

	fmt.Printf("\n✅ Evaluation completed successfully.\n")
	fmt.Printf("   Run ID: %s\n", runID)
}
