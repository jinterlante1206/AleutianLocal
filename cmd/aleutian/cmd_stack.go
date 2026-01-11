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
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/config"
	"github.com/spf13/cobra"
)

// =============================================================================
// ENVIRONMENT CONFIGURATION
// =============================================================================

// calculateOptimizedEnv determines optimal environment variables based on hardware.
//
// # Description
//
// Analyzes available compute memory (VRAM) and selects an appropriate hardware
// profile from the centralized config. Returns environment variables that
// configure Ollama models, token limits, and query parameters.
//
// # Inputs
//
//   - totalRAM_MB: Total compute memory in megabytes (VRAM for GPU systems).
//
// # Outputs
//
//   - map[string]string: Environment variables keyed by name, including:
//   - OLLAMA_MODEL: Selected LLM model for the hardware tier
//   - LLM_DEFAULT_MAX_TOKENS: Maximum token limit for generation
//   - RERANKER_MODEL: Model for reranking search results
//   - WEAVIATE_QUERY_DEFAULTS_LIMIT: Default query limit for vector DB
//   - RERANK_FINAL_K: Number of results after reranking (if applicable)
//
// # Examples
//
//	env := calculateOptimizedEnv(8192)
//	// Returns profile for 8GB VRAM systems
//	fmt.Println(env["OLLAMA_MODEL"]) // e.g., "gpt-oss:latest"
//
// # Limitations
//
//   - Profiles are predefined; no dynamic scaling between tiers.
//   - VRAM detection depends on accurate hardware reporting.
//
// # Assumptions
//
//   - config.BuiltInHardwareProfiles is populated with valid profiles.
//   - totalRAM_MB reflects actual available compute memory.
func calculateOptimizedEnv(totalRAM_MB int) map[string]string {
	env := make(map[string]string)
	fmt.Printf("Optimization Engine: Detected %d MB Compute Memory (VRAM)\n", totalRAM_MB)

	// Use centralized config for profile selection
	profileName := config.GetProfileForRAM(totalRAM_MB)
	profile := config.BuiltInHardwareProfiles[profileName]

	fmt.Printf("   -> Profile: %s\n", profile.Description)

	env["OLLAMA_MODEL"] = profile.OllamaModel
	env["LLM_DEFAULT_MAX_TOKENS"] = strconv.Itoa(profile.MaxTokens)
	env["RERANKER_MODEL"] = profile.RerankerModel
	env["WEAVIATE_QUERY_DEFAULTS_LIMIT"] = strconv.Itoa(profile.WeaviateQueryLimit)
	if profile.RerankFinalK > 0 {
		env["RERANK_FINAL_K"] = strconv.Itoa(profile.RerankFinalK)
	}

	return env
}

// =============================================================================
// CLI HANDLERS
// =============================================================================

// runStart handles the "stack start" CLI command.
//
// # Description
//
// Initializes and starts the Aleutian stack by:
// 1. Creating a StackManager with production dependencies
// 2. Parsing CLI flags into StartOptions
// 3. Delegating to StackManager.Start() for orchestration
//
// The StackManager handles infrastructure setup, secrets provisioning,
// container orchestration, and service health verification.
//
// # Inputs
//
//   - cmd: Cobra command containing parsed flags
//   - args: Positional arguments (not used)
//
// # Outputs
//
// None (exits process on failure via log.Fatalf).
//
// # Examples
//
//	// Called by Cobra when user runs: aleutian stack start
//	// With flags: aleutian stack start --force-recreate --skip-model-check
//
// # Limitations
//
//   - Exits process on any error; no partial recovery.
//   - Blocks until stack is fully started.
//
// # Assumptions
//
//   - config.Global is loaded and valid.
//   - rootCmd.Version contains valid CLI version.
//   - Required infrastructure (Podman) is installed.
func runStart(cmd *cobra.Command, _ []string) {
	ctx := context.Background()

	// Get CLI flags
	forceRecreate, _ := cmd.Flags().GetBool("force-recreate")
	fixMounts, _ := cmd.Flags().GetBool("fix-mounts")
	skipModelCheck, _ := cmd.Flags().GetBool("skip-model-check")

	// Get stack directory
	cliVersion := rootCmd.Version
	stackDir, err := ensureStackDir(cliVersion)
	if err != nil {
		log.Fatalf("Failed to prepare stack directory: %v", err)
	}

	// Create StackManager with all production dependencies
	mgr, err := CreateProductionStackManager(&config.Global, stackDir, cliVersion)
	if err != nil {
		log.Fatalf("Failed to create stack manager: %v", err)
	}

	// Configure start options from CLI flags
	opts := StartOptions{
		ForceRecreate:   forceRecreate,
		FixMounts:       fixMounts,
		ForceBuild:      forceBuild,
		SkipModelCheck:  skipModelCheck,
		Profile:         profile,
		BackendOverride: backendType,
		ForecastMode:    forecastMode,
	}

	// Start the stack
	if err := mgr.Start(ctx, opts); err != nil {
		log.Fatalf("Failed to start stack: %v", err)
	}
}

// runStatus handles the "stack status" CLI command.
//
// # Description
//
// Queries the current state of the Aleutian stack and displays:
// - Podman machine state (macOS only)
// - Overall stack state and container counts
// - Individual service states with health indicators
//
// # Inputs
//
//   - cmd: Cobra command (not used)
//   - args: Positional arguments (not used)
//
// # Outputs
//
// None (prints status to stdout, exits on failure via log.Fatalf).
//
// # Examples
//
//	// Called by Cobra when user runs: aleutian stack status
//	// Output:
//	// === Aleutian Stack Status ===
//	// Podman Machine: running
//	// State: healthy (Running: 5, Stopped: 0)
//	// Services:
//	//    weaviate: running
//
// # Limitations
//
//   - Status is a point-in-time snapshot.
//   - Health indicators require health check endpoints.
//
// # Assumptions
//
//   - Stack directory exists.
func runStatus(_ *cobra.Command, _ []string) {
	ctx := context.Background()

	// Get stack directory
	cliVersion := rootCmd.Version
	stackDir, err := ensureStackDir(cliVersion)
	if err != nil {
		log.Printf("Warning: Could not determine stack directory (%v), using current dir.", err)
		stackDir = "."
	}

	// Create StackManager
	mgr, err := CreateProductionStackManager(&config.Global, stackDir, cliVersion)
	if err != nil {
		log.Fatalf("Failed to create stack manager: %v", err)
	}

	// Get status
	status, err := mgr.Status(ctx)
	if err != nil {
		log.Fatalf("Failed to get stack status: %v", err)
	}

	// Print status
	printStackStatus(status)
}

// runStop handles the "stack stop" CLI command.
//
// # Description
//
// Gracefully stops all running Aleutian stack containers without removing them.
// Containers can be restarted later without re-creating.
//
// # Inputs
//
//   - cmd: Cobra command (not used)
//   - args: Positional arguments (not used)
//
// # Outputs
//
// None (exits on failure via log.Fatalf).
//
// # Examples
//
//	// Called by Cobra when user runs: aleutian stack stop
//
// # Limitations
//
//   - Does not stop Podman machine (use "podman machine stop" separately).
//   - Does not wait for containers to fully stop before returning.
//
// # Assumptions
//
//   - Stack was previously started.
func runStop(_ *cobra.Command, _ []string) {
	ctx := context.Background()

	// Get stack directory
	cliVersion := rootCmd.Version
	stackDir, err := ensureStackDir(cliVersion)
	if err != nil {
		log.Printf("Warning: Could not determine stack directory (%v), using current dir.", err)
		stackDir = "."
	}

	// Create StackManager
	mgr, err := CreateProductionStackManager(&config.Global, stackDir, cliVersion)
	if err != nil {
		log.Fatalf("Failed to create stack manager: %v", err)
	}

	// Stop the stack
	if err := mgr.Stop(ctx); err != nil {
		log.Fatalf("Failed to stop stack: %v", err)
	}
}

// runLogsCommand handles the "stack logs" CLI command.
//
// # Description
//
// Streams container logs to stdout in real-time. Can follow all containers
// or filter to specific services.
//
// # Inputs
//
//   - cmd: Cobra command (not used)
//   - args: Optional service names to filter logs (empty = all services)
//
// # Outputs
//
// None (streams logs to stdout).
//
// # Examples
//
//	// All services: aleutian stack logs
//	// Specific service: aleutian stack logs orchestrator
//	// Multiple services: aleutian stack logs orchestrator weaviate
//
// # Limitations
//
//   - Blocks until user terminates (Ctrl+C).
//   - Log buffering may cause slight delays.
//
// # Assumptions
//
//   - Stack containers are running.
func runLogsCommand(_ *cobra.Command, args []string) {
	ctx := context.Background()

	// Get stack directory
	cliVersion := rootCmd.Version
	stackDir, err := ensureStackDir(cliVersion)
	if err != nil {
		log.Printf("Warning: Could not determine stack directory (%v), using current dir.", err)
		stackDir = "."
	}

	// Create StackManager
	mgr, err := CreateProductionStackManager(&config.Global, stackDir, cliVersion)
	if err != nil {
		log.Fatalf("Failed to create stack manager: %v", err)
	}

	// Stream logs
	if err := mgr.Logs(ctx, args); err != nil {
		fmt.Println("\nLog streaming stopped or encountered an error")
	}
}

// runDestroy handles the "stack destroy" CLI command.
//
// # Description
//
// Permanently destroys all stack resources after user confirmation:
// 1. Prompts for destruction confirmation
// 2. Optionally prompts to remove stack files from ~/.aleutian/stack
// 3. Stops and removes all containers and volumes
// 4. Removes stack files if requested
//
// # Inputs
//
//   - cmd: Cobra command (not used)
//   - args: Positional arguments (not used)
//
// # Outputs
//
// None (exits on failure via log.Fatalf).
//
// # Examples
//
//	// Called by Cobra when user runs: aleutian stack destroy
//	// Prompts:
//	//   "Are you sure you want to continue? (yes/no):"
//	//   "Do you want to remove the downloaded stack files? (yes/no):"
//
// # Limitations
//
//   - Destruction is irreversible.
//   - Requires interactive terminal for confirmation.
//
// # Assumptions
//
//   - User can respond to stdin prompts.
func runDestroy(_ *cobra.Command, _ []string) {
	ctx := context.Background()

	// User confirmation prompt (keep in CLI layer)
	fmt.Println("WARNING: You are about to permanently delete all local data and containers" +
		" associated with your aleutian run, including wiping the database you have spun up. " +
		"If you want to save your Aleutian DB data please cancel this command and back it up.")
	fmt.Println("Are you sure you want to continue? (yes/no): ")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	if input != "yes" && input != "y" {
		fmt.Println("Aborted. No changes were made")
		return
	}

	// Get stack directory
	cliVersion := rootCmd.Version
	stackDir, err := ensureStackDir(cliVersion)
	if err != nil {
		log.Printf("Warning: Could not determine stack directory (%v), using current dir.", err)
		stackDir = "."
	}

	// Ask about file removal
	fmt.Print("Do you want to remove the downloaded stack files from ~/.aleutian/stack? (yes/no): ")
	reader = bufio.NewReader(os.Stdin)
	input, _ = reader.ReadString('\n')
	removeFiles := strings.TrimSpace(strings.ToLower(input)) == "yes"

	// Create StackManager
	mgr, err := CreateProductionStackManager(&config.Global, stackDir, cliVersion)
	if err != nil {
		log.Fatalf("Failed to create stack manager: %v", err)
	}

	// Destroy the stack
	if err := mgr.Destroy(ctx, removeFiles); err != nil {
		log.Fatalf("Failed to destroy stack: %v", err)
	}
}

// =============================================================================
// OUTPUT FORMATTING
// =============================================================================

// printStackStatus formats and prints the stack status to stdout.
//
// # Description
//
// Formats a StackStatus struct into human-readable output with:
// - Podman machine state (macOS only, with emoji indicator)
// - Overall state with running/stopped container counts
// - Uptime if available
// - Per-service status with health indicators
//
// Health indicators:
// - (green heart): Running and healthy
// - (checkmark): Running
// - (x): Stopped or unhealthy
//
// # Inputs
//
//   - status: StackStatus struct containing machine and service states.
//
// # Outputs
//
// None (prints to stdout).
//
// # Examples
//
//	status := &StackStatus{
//	    State: "healthy",
//	    MachineState: "running",
//	    RunningCount: 5,
//	    StoppedCount: 0,
//	}
//	printStackStatus(status)
//	// Output:
//	// === Aleutian Stack Status ===
//	// (checkmark) Podman Machine: running
//	// State: healthy (Running: 5, Stopped: 0)
//
// # Limitations
//
//   - Output format is not machine-readable.
//   - Emoji display depends on terminal support.
//
// # Assumptions
//
//   - Terminal supports UTF-8 for emoji display.
func printStackStatus(status *StackStatus) {
	fmt.Println("=== Aleutian Stack Status ===")

	// Machine status (macOS)
	if runtime.GOOS == "darwin" && status.MachineState != "" {
		stateEmoji := "X"
		if status.MachineState == "running" {
			stateEmoji = "OK"
		}
		fmt.Printf("%s Podman Machine: %s\n", stateEmoji, status.MachineState)
	}

	// Overall state
	fmt.Printf("\nState: %s (Running: %d, Stopped: %d)\n",
		status.State, status.RunningCount, status.StoppedCount)

	if status.Uptime > 0 {
		fmt.Printf("Uptime: %s\n", status.Uptime.Round(time.Second))
	}

	// Services
	if len(status.Services) > 0 {
		fmt.Println("\nServices:")
		for _, svc := range status.Services {
			stateEmoji := "X"
			if svc.State == "running" {
				stateEmoji = "OK"
				if svc.Healthy != nil && *svc.Healthy {
					stateEmoji = "HEALTHY"
				}
			}
			fmt.Printf("   %s %s: %s\n", stateEmoji, svc.Name, svc.State)
		}
	}
}
