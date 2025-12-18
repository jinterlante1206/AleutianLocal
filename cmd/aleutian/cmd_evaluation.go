package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/handlers"
	"github.com/spf13/cobra"
)

func runEvaluation(cmd *cobra.Command, args []string) {
	// Parse flags
	date, _ := cmd.Flags().GetString("date")
	ticker, _ := cmd.Flags().GetString("ticker")
	model, _ := cmd.Flags().GetString("model")

	// Set defaults
	if date == "" {
		date = time.Now().Format("20060102")
	}

	runID := fmt.Sprintf("%s_%s", date, uuid.New().String()[:8])

	// Select tickers
	tickers := datatypes.DefaultTickers
	if ticker != "" {
		tickers = []datatypes.TickerInfo{{Ticker: ticker, Description: ""}}
	}

	// Select models
	models := datatypes.DefaultModels
	if model != "" {
		models = []string{model}
	}

	// Build config
	config := &datatypes.EvaluationConfig{
		Tickers:        tickers,
		Models:         models,
		EvaluationDate: date,
		RunID:          runID,
		StrategyType:   "threshold",
		StrategyParams: map[string]interface{}{
			"threshold_type":  "absolute",
			"threshold_value": 2.0,
			"execution_size":  10.0,
		},
		ContextSize:     252,
		HorizonSize:     20,
		InitialCapital:  100000.0,
		InitialPosition: 0.0,
		InitialCash:     100000.0,
	}

	fmt.Printf("Starting Evaluation Run: %s\n", runID)
	fmt.Printf("Tickers: %d | Models: %d\n", len(tickers), len(models))

	// Create evaluator (Logic lives in handlers package)
	evaluator, err := handlers.NewEvaluator()
	if err != nil {
		slog.Error("Failed to create evaluator", "error", err)
		return
	}
	defer evaluator.Close()

	// Run evaluation
	ctx := context.Background()
	if err := evaluator.RunEvaluation(ctx, config); err != nil {
		slog.Error("Evaluation failed", "error", err)
		return
	}

	fmt.Printf("✅ Evaluation completed successfully.\n")
}
