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
	"github.com/jinterlante1206/AleutianLocal/pkg/ux"
	"github.com/spf13/cobra"
)

// --- Global Command Variables ---
var (
	backendType      string
	profile          string
	forceBuild       bool
	forecastMode     string // CLI override for forecast.mode (standalone/sapheneia)
	pipelineType     string
	noRag            bool
	enableThinking   bool
	budgetTokens     int
	quantizeType     string
	isLocalPath      bool
	fetchDays        int
	forecastModel    string
	forecastHorizon  int
	forecastContext  int
	personalityLevel string // UX personality level (full/standard/minimal/machine)

	rootCmd = &cobra.Command{
		Use:   "aleutian",
		Short: "A cli to manage the Aleutian FOSS private AI appliance",
		Long: `Aleutian is a tool for deploying and managing a complete,
				offline AI stack on your own infrastructure.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Initialize UX personality from flag or environment
			if personalityLevel != "" {
				ux.SetPersonalityLevel(ux.ParsePersonalityLevel(personalityLevel))
			} else {
				ux.InitPersonality()
			}
		},
	}

	// ingestCmd is a simplified alias for populate vectordb
	ingestCmd = &cobra.Command{
		Use:     "ingest [path...]",
		Short:   "Ingest documents into the knowledge base (alias for populate vectordb)",
		Aliases: []string{"i"},
		Run:     populateVectorDB,
	}
	// --- RAG / Ask ---
	askCmd = &cobra.Command{
		Use:   "ask [question]",
		Short: "Asks a question to the RAG system using the documents in the VectorDB",
		Run:   runAskCommand, // Defined in cmd_chat.go
	}

	// --- Data / Populate ---
	populateCmd = &cobra.Command{
		Use:   "populate",
		Short: "Populate data stores with scanned and approved content.",
	}
	populateVectorDBCmd = &cobra.Command{
		Use:   "vectordb [file or directory path]",
		Short: "Scans local files/directories for secrets before populating the VectorDB",
		Run:   populateVectorDB, // Defined in cmd_data.go
	}

	// --- Stack Management ---
	stackCmd = &cobra.Command{
		Use:   "stack",
		Short: "Manage the local Aleutian application on your machine",
	}
	deployCmd = &cobra.Command{
		Use:   "start",
		Short: "Start all local Aleutian services",
		Run:   runStart, // Defined in cmd_stack.go
	}

	stopCmd = &cobra.Command{
		Use:   "stop",
		Short: "Stop all local Aleutian services",
		Run:   runStop, // Defined in cmd_stack.go
	}
	destroyCmd = &cobra.Command{
		Use:   "destroy",
		Short: "DANGER: Stops and deletes all local containers AND data",
		Run:   runDestroy, // Defined in cmd_stack.go
	}
	logsCmd = &cobra.Command{
		Use:   "logs [service_name]",
		Short: "Stream logs from a local service container",
		Run:   runLogsCommand, // Defined in cmd_stack.go
	}

	// --- Utilities ---
	convertCmd = &cobra.Command{
		Use:   "convert [model-id]",
		Short: "Converts a huggingface model to GGUF format",
		Run:   runConvertCommand, // Defined in cmd_utils.go
	}

	pullModelCmd = &cobra.Command{
		Use:   "pull [model_id]",
		Short: "Instructs the Orchestrator to download a specific model",
		Run:   runPullModel, // Defined in cmd_utils.go
	}

	cacheAllCmd = &cobra.Command{
		Use:   "cache-all [json_file]",
		Short: "Iterates through a JSON list and requests the Orchestrator to cache them all",
		Run:   runCacheAll, // Defined in cmd_utils.go
	}

	// --- Sessions ---
	sessionCmd = &cobra.Command{
		Use:   "session",
		Short: "Manage conversation sessions",
	}
	listSessionsCmd = &cobra.Command{
		Use:   "list",
		Short: "List all conversation sessions",
		Run:   runListSessions, // Defined in cmd_data.go
	}
	deleteSessionCmd = &cobra.Command{
		Use:   "delete [session_id]",
		Short: "Delete a specific conversation session",
		Run:   runDeleteSession, // Defined in cmd_data.go
	}

	// --- Chat ---
	chatCmd = &cobra.Command{
		Use:   "chat",
		Short: "Starts an interactive chat session",
		Run:   runChatCommand, // Defined in cmd_chat.go
	}

	traceCmd = &cobra.Command{
		Use:   "trace [query]",
		Short: "Analyze codebase using autonomous agent",
		Run:   runTraceCommand, // Defined in cmd_chat.go
	}

	// --- Weaviate Admin ---
	weaviateCmd = &cobra.Command{
		Use:   "weaviate",
		Short: "Perform administrative tasks on the Weaviate vector database",
	}
	weaviateBackupCmd = &cobra.Command{
		Use:   "backup [backupID]",
		Short: "Create a new backup of the Weaviate database",
		Run:   runWeaviateBackup, // Defined in cmd_data.go
	}
	weaviateRestoreCmd = &cobra.Command{
		Use:   "restore [backup-id]",
		Short: "Restore the Weaviate database from a backup",
		Run:   runWeaviateRestore, // Defined in cmd_data.go
	}
	weaviateSummaryCmd = &cobra.Command{
		Use:   "summary",
		Short: "Get a summary of the Weaviate schema and data",
		Run:   runWeaviateSummary, // Defined in cmd_data.go
	}
	weaviateWipeoutCmd = &cobra.Command{
		Use:   "wipeout",
		Short: "DANGER: Deletes all data and schemas from Weaviate",
		Run:   runWeaviateWipeout, // Defined in cmd_data.go
	}
	weaviateDeleteDocCmd = &cobra.Command{
		Use:   "delete [source-name]",
		Short: "Deletes a document and all its chunks",
		Run:   runWeaviateDeleteDoc, // Defined in cmd_data.go
	}

	// --- GCS ---
	uploadCmd = &cobra.Command{
		Use:   "upload",
		Short: "Upload data to Google Cloud Storage (GCS)",
	}
	uploadLogsCmd = &cobra.Command{
		Use:   "logs [local_directory]",
		Short: "Uploads log files from a local directory to GCS",
		Run:   runUploadLogs, // Defined in cmd_data.go
	}
	uploadBackupsCmd = &cobra.Command{
		Use:   "backups [local_directory]",
		Short: "Uploads Weaviate backups from a local directory to GCS",
		Run:   runUploadBackups, // Defined in cmd_data.go
	}

	// --- Time Series ---
	timeseriesCmd = &cobra.Command{
		Use:   "timeseries",
		Short: "timeseries data and forecasting commands",
	}
	fetchDataCmd = &cobra.Command{
		Use:   "fetch [tickers]",
		Short: "Fetch historical data for tickers",
		Run:   runFetchData, // Defined in cmd_timeseries.go
	}
	forecastCmd = &cobra.Command{
		Use:   "forecast [ticker]",
		Short: "Run a time-series forecast on a ticker",
		Run:   runForecast, // Defined in cmd_timeseries.go
	}
	evaluationCmd = &cobra.Command{
		Use:   "evaluate",
		Short: "Run forecast evaluation across models and tickers",
	}

	runEvaluationCmd = &cobra.Command{
		Use:   "run",
		Short: "Run evaluation for specified date, tickers, and models",
		Run:   runEvaluation, // Defined in cmd_evaluation.go
	}

	// Policies
	policyCmd = &cobra.Command{
		Use:   "policy",
		Short: "Base command to interact with the privacy policies",
		Long: `Use policy + subcommands to interact with the privacy policies that are embedded
				in the aleutian binary. You can define new versions as long as you rebuild the
				binary.`,
	}

	verifyPolicyCmd = &cobra.Command{
		Use:   "verify",
		Short: "Verify the integrity of the embedded policy rules",
		Long:  `Calculates the SHA256 hash of the compiled-in policy definitions. Use this to verify that the binary is running the expected version of your governance rules.`,
		Run:   verifyPolicies,
	}

	dumpPolicyCmd = &cobra.Command{
		Use:   "dump",
		Short: "Prints out the whole policy file to stdout",
		Long: `policy dump prints out the policies you've configured in the
				data_classification_patterns.yaml file.'`,
		Run: dumpPolicies,
	}

	testPolicyCmd = &cobra.Command{
		Use:   "test",
		Short: "Allows you to enter a test string see if the policies catch it",
		Long: `policy test allows you to enter a test string to see if that individual string
				gets caught by the policies you have in place'`,
		Run: testPolicyString,
	}

	statusCmd = &cobra.Command{
		Use:   "status",
		Short: "Show resource usage and health of running services.",
		Run:   runStatus,
	}

	exportEvaluationCmd = &cobra.Command{
		Use:   "export [run_id]",
		Short: "Export evaluation results to CSV",
		Args:  cobra.ExactArgs(1),
		Run:   runExport, // Points to the function we just made
	}
)

// init runs when the Go program starts
func init() {
	// Global UX personality flag
	rootCmd.PersistentFlags().StringVar(&personalityLevel, "personality", "",
		"Output style: full (default, rich nautical), standard, minimal, or machine (scripting)")

	// Simplified ingest alias
	rootCmd.AddCommand(ingestCmd)
	ingestCmd.Flags().Bool("force", false, "Force ingestion, skipping policy/secret checks.")
	ingestCmd.Flags().String("data-space", "default", "The logical data space to ingest into")
	ingestCmd.Flags().String("version", "latest", "A version tag for this ingestion")

	rootCmd.AddCommand(askCmd)
	askCmd.Flags().StringVarP(&pipelineType, "pipeline", "p", "reranking",
		"RAG pipeline to use(e.g., standard, reranking, raptor, graph, rig, semantic")
	askCmd.Flags().BoolVar(&noRag, "no-rag", false, "Skip the RAG pipeline and ask the LLM directly.")

	rootCmd.AddCommand(populateCmd)
	populateCmd.AddCommand(populateVectorDBCmd)
	populateVectorDBCmd.Flags().Bool("force", false, "Force ingestion, skipping policy/secret checks.")
	populateVectorDBCmd.Flags().String("data-space", "default", "The logical data space to ingest into (e.g., 'work', 'personal')")
	populateVectorDBCmd.Flags().String("version", "latest", "A version tag for this ingestion (e.g., 'v1.1', '2025-11-01')")

	// --- Local Commands ---
	rootCmd.AddCommand(stackCmd)
	stackCmd.AddCommand(deployCmd)
	stackCmd.AddCommand(stopCmd)
	stackCmd.AddCommand(destroyCmd)
	stackCmd.AddCommand(logsCmd)
	stackCmd.AddCommand(statusCmd)
	deployCmd.Flags().StringVar(&backendType, "backend", "", "Set LLM backend (ollama, "+
		"openai, anthropic). Skips local model checks if not 'ollama'.")
	deployCmd.Flags().StringVar(&profile, "profile", "auto", "Optimization profile: 'auto', 'low', 'standard', 'performance', 'ultra', or 'manual'")
	deployCmd.Flags().BoolVar(&forceBuild, "build", false, "Force rebuild of container images")
	deployCmd.Flags().Bool("force-recreate", false,
		"automatically recreates the podman machine if a drift is detected.")
	deployCmd.Flags().StringVar(&forecastMode, "forecast-mode", "", "Forecast service mode: 'standalone' (local) or 'sapheneia' (external)")
	// --- Utility Commands ---
	rootCmd.AddCommand(convertCmd)
	convertCmd.Flags().StringVar(&quantizeType, "quantize", "q8_0", "Quantization type (f32, q8_0, bf16, f16)")
	convertCmd.Flags().BoolVar(&isLocalPath, "is-local-path", false, "Treat the model-id as a local path inside the container")
	convertCmd.Flags().Bool("register", false, "Register the model with ollama")

	rootCmd.AddCommand(pullModelCmd)
	rootCmd.AddCommand(cacheAllCmd)

	// session commands
	rootCmd.AddCommand(sessionCmd)
	sessionCmd.AddCommand(listSessionsCmd)
	sessionCmd.AddCommand(deleteSessionCmd)

	// chat command
	rootCmd.AddCommand(chatCmd)
	chatCmd.Flags().String("resume", "", "Resume a conversation using a specific session ID.")
	chatCmd.Flags().BoolVar(&enableThinking, "thinking", false, "Enable Extended Thinking (Claude only)")
	chatCmd.Flags().IntVar(&budgetTokens, "budget", 2048, "Token budget for thinking (default 2048)")

	rootCmd.AddCommand(traceCmd)

	// weaviate administration commands
	rootCmd.AddCommand(weaviateCmd)
	weaviateCmd.AddCommand(weaviateBackupCmd)
	weaviateCmd.AddCommand(weaviateRestoreCmd)
	weaviateCmd.AddCommand(weaviateSummaryCmd)
	weaviateCmd.AddCommand(weaviateWipeoutCmd)
	weaviateWipeoutCmd.Flags().Bool("force", false, "Required to confirm the deletion of all data.")
	weaviateCmd.AddCommand(weaviateDeleteDocCmd)

	// GCS data commands
	rootCmd.AddCommand(uploadCmd)
	uploadCmd.AddCommand(uploadLogsCmd)
	uploadCmd.AddCommand(uploadBackupsCmd)

	// Time Series
	rootCmd.AddCommand(timeseriesCmd)
	timeseriesCmd.AddCommand(fetchDataCmd)
	fetchDataCmd.Flags().IntVar(&fetchDays, "days", 365, "Number of days of history to fetch")
	timeseriesCmd.AddCommand(forecastCmd)
	forecastCmd.Flags().StringVar(&forecastModel, "model", "google/timesfm-2.0-500m-pytorch", "Model ID to use")
	forecastCmd.Flags().IntVar(&forecastHorizon, "horizon", 20, "Forecast horizon (days)")
	forecastCmd.Flags().IntVar(&forecastContext, "context", 300, "Context window size (days)")

	rootCmd.AddCommand(evaluationCmd)
	evaluationCmd.AddCommand(runEvaluationCmd)
	runEvaluationCmd.Flags().String("config", "", "Path to scenario configuration file (YAML)")
	runEvaluationCmd.Flags().String("date", "", "Evaluation date (YYYYMMDD, default: today)")
	runEvaluationCmd.Flags().String("ticker", "", "Single ticker to evaluate (default: all)")
	runEvaluationCmd.Flags().String("model", "", "Single model to evaluate (default: all)")
	evaluationCmd.AddCommand(exportEvaluationCmd)
	exportEvaluationCmd.Flags().StringP("output", "o", "", "Output filename (default: backtest_{RunID}.csv)")

	// Policies
	rootCmd.AddCommand(policyCmd)
	policyCmd.AddCommand(verifyPolicyCmd)
	policyCmd.AddCommand(dumpPolicyCmd)
	policyCmd.AddCommand(testPolicyCmd)
}
