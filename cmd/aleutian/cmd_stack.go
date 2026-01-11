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
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/config"
	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/internal/health"
	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/internal/infra"
	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/internal/infra/process"
	"github.com/spf13/cobra"
)

var alpineImagePulled bool // Optimization: Cache the pull status across retries

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

func checkAndFixPodmanMachine(cfg config.MachineConfig, forceRecreate bool) error {
	return checkAndFixPodmanMachineWithDepth(cfg, 0, forceRecreate)
}

// verifyMachineMounts checks if the Podman machine's actual mounts match the config.
func verifyMachineMounts(machineName string, expectedDrives []string) (bool, error) {
	cmd := exec.Command("podman", "machine", "inspect", machineName, "--format", "json")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to inspect machine: %w", err)
	}

	// CRITICAL FIX: Strip any non-JSON prefix (warnings, journalctl messages, etc.)
	outputStr := string(output)

	// Find the first '[' or '{' character (start of JSON)
	jsonStart := strings.IndexAny(outputStr, "[{")
	if jsonStart == -1 {
		return false, fmt.Errorf("no JSON found in inspect output")
	}

	// Parse only the JSON portion
	var machines []map[string]interface{}
	if err := json.Unmarshal([]byte(outputStr[jsonStart:]), &machines); err != nil {
		return false, fmt.Errorf("failed to parse machine inspect output: %w", err)
	}

	if len(machines) == 0 {
		return false, fmt.Errorf("no machine data returned")
	}

	machine := machines[0]

	// Extract the Mounts array
	mountsInterface, ok := machine["Mounts"]
	if !ok {
		if len(expectedDrives) > 0 {
			return false, nil
		}
		return true, nil
	}

	mounts, ok := mountsInterface.([]interface{})
	if !ok {
		return false, fmt.Errorf("unexpected Mounts format")
	}

	// Build a set of actual mounted paths
	actualMounts := make(map[string]bool)
	for _, mountInterface := range mounts {
		mount, ok := mountInterface.(map[string]interface{})
		if !ok {
			continue
		}
		if source, ok := mount["Source"].(string); ok {
			actualMounts[source] = true
		}
	}

	// Check if all expected drives are mounted
	for _, expectedDrive := range expectedDrives {
		if _, err := os.Stat(expectedDrive); err == nil {
			if !actualMounts[expectedDrive] {
				return false, nil
			}
		}
	}

	return true, nil
}

// provisionPodmanMachine creates a new machine with the specified config
func provisionPodmanMachine(machineName string, cpuCount int, memAmount int, drives []string) error {
	args := []string{"machine", "init", machineName,
		"--cpus", strconv.Itoa(cpuCount),
		"--memory", strconv.Itoa(memAmount),
	}

	validDrives := make([]string, 0, len(drives))
	for _, drive := range drives {
		if _, err := os.Stat(drive); os.IsNotExist(err) {
			slog.Warn("Configured drive path not found on host, skipping mount", "path", drive)
			continue
		}
		validDrives = append(validDrives, drive)
		mountStr := fmt.Sprintf("%s:%s", drive, drive)
		fmt.Printf("   - Mounting: %s\n", mountStr)
		args = append(args, "-v", mountStr)
	}

	if len(validDrives) == 0 {
		fmt.Println("   ⚠️  Warning: No valid mount paths found. Creating machine without mounts.")
	}

	initCmd := exec.Command("podman", args...)
	initCmd.Stdout = os.Stdout
	initCmd.Stderr = os.Stderr
	if err := initCmd.Run(); err != nil {
		return fmt.Errorf("failed to provision podman machine: %w", err)
	}
	fmt.Println("Infrastructure provisioned.")
	return nil
}

func checkAndFixPodmanMachineWithDepth(cfg config.MachineConfig, recursionDepth int, forceRecreate bool) error {
	const MAX_HEALING_ATTEMPTS = 2

	defer func() {
		if r := recover(); r != nil {
			collectDiagnostics("panic", fmt.Sprintf("%v", r))
			panic(r)
		}
	}()

	if recursionDepth > MAX_HEALING_ATTEMPTS {
		err := fmt.Errorf("self-healing failed after %d attempts", MAX_HEALING_ATTEMPTS)
		collectDiagnostics("Self-Healing Exhausted", err.Error())
		return fmt.Errorf("%w - manual intervention required", err)
	}

	if runtime.GOOS != "darwin" {
		return nil
	}

	if isRunning, pid := isPodmanDesktopRunning(); isRunning {
		fmt.Printf("⚠️  Podman Desktop is running (PID %d)\n", pid)
		fmt.Println("   It creates a conflicting VM that breaks CLI tools.")
		fmt.Println("   Recommendation: Quit Podman Desktop via the Menu Bar.")
		fmt.Print("\n   Try to proceed anyway? (yes/no): ")

		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(input)) != "yes" {
			return fmt.Errorf("startup cancelled due to Podman Desktop conflict")
		}
	}

	machineName := cfg.Id
	if machineName == "" {
		machineName = "podman-machine-default"
	}
	cpuCount := cfg.CPUCount
	if cpuCount == 0 {
		cpuCount = 6
	}
	memAmount := cfg.MemoryAmount
	if memAmount == 0 {
		memAmount = 20480
	}

	if recursionDepth == 0 {
		fmt.Println("Aleutian Infrastructure Check...")
		fmt.Printf("   Target Machine: %s (CPUs: %d, Mem: %d MB)\n", machineName, cpuCount, memAmount)
	} else {
		fmt.Printf("   ⚠️  Self-Healing attempt %d/%d...\n", recursionDepth, MAX_HEALING_ATTEMPTS)
	}

	// 1. Check if machine exists
	checkCmd := exec.Command("podman", "machine", "inspect", machineName)
	machineExists := checkCmd.Run() == nil

	if !machineExists {
		fmt.Printf("Machine not found. Provisioning Infrastructure...\n")
		return provisionPodmanMachine(machineName, cpuCount, memAmount, cfg.Drives)
	}

	// 2. Machine exists - Verify Mount Configuration (Safe Drift Detection)
	fmt.Print("🔍 Verifying machine configuration... ")
	mountsMatch, err := verifyMachineMounts(machineName, cfg.Drives)
	if err != nil {
		fmt.Printf("WARN (couldn't verify: %v)\n", err)
	} else if !mountsMatch {
		fmt.Println("DRIFT DETECTED.")
		fmt.Println()
		fmt.Println("╔═══════════════════════════════════════════════════════════════════╗")
		fmt.Println("║              ⚠️  MOUNT CONFIGURATION MISMATCH                      ║")
		fmt.Println("╚═══════════════════════════════════════════════════════════════════╝")
		fmt.Println("\nYour Podman machine has different volume mounts than your config.")

		// Visualize Diff (Simple)
		fmt.Println("\nExpected config mounts:")
		for _, drive := range cfg.Drives {
			fmt.Printf("   - %s\n", drive)
		}

		fmt.Println("\n┌─────────────────────────────────────────────────────────────────┐")
		fmt.Println("│ WHY THIS MATTERS:                                               │")
		fmt.Println("│ - Containers won't be able to access missing mount paths        │")
		fmt.Println("│ - You'll see 'statfs: not a directory' errors                   │")
		fmt.Println("└─────────────────────────────────────────────────────────────────┘")
		fmt.Println("\n┌─────────────────────────────────────────────────────────────────┐")
		fmt.Println("│ TO FIX:                                                         │")
		fmt.Println("│ 1. Stop services:    aleutian stack stop                        │")
		fmt.Printf("│ 2. Remove machine:   podman machine rm -f %s\n", machineName)
		fmt.Println("│ 3. Restart:          aleutian stack start                       │")
		fmt.Println("│                                                                 │")
		fmt.Println("│ NOTE: This only removes the VM. Your data (volumes, models)     │")
		fmt.Println("│       will be preserved.                                        │")
		fmt.Println("└─────────────────────────────────────────────────────────────────┘")
		fmt.Println()

		shouldRecreate := false

		// Check Flag
		if forceRecreate {
			fmt.Println("🛠️  --force-recreate flag detected. Automatically fixing...")
			shouldRecreate = true
		} else {
			// Check for Foreign Workloads before asking
			hasForeign, _, _ := hasForeignWorkloads()
			if hasForeign {
				fmt.Println("⚠️  Foreign containers detected. Auto-fix is disabled to protect your other work.")
				fmt.Println("   Please fix manually using the commands above.")
				return nil // Continue and let them fail or work partially
			}

			// Interactive Prompt
			fmt.Print("Would you like to automatically fix this now? (yes/no): ")
			reader := bufio.NewReader(os.Stdin)
			input, _ := reader.ReadString('\n')
			input = strings.ToLower(strings.TrimSpace(input))
			if input == "yes" || input == "y" {
				shouldRecreate = true
			}
		}

		if shouldRecreate {
			fmt.Println("\n🛠️  Recreating machine with correct mounts...")
			fmt.Print("   Stopping machine... ")
			exec.Command("podman", "machine", "stop", machineName).Run()
			fmt.Println("done.")

			fmt.Print("   Removing old machine... ")
			rmCmd := exec.Command("podman", "machine", "rm", "-f", machineName)
			if err := rmCmd.Run(); err != nil {
				return fmt.Errorf("failed to remove machine: %w", err)
			}
			fmt.Println("done.")

			fmt.Println("   Provisioning new machine:")
			if err := provisionPodmanMachine(machineName, cpuCount, memAmount, cfg.Drives); err != nil {
				return err
			}
			fmt.Println("\n   ✅ Machine recreated successfully!")
			return nil // Success
		} else {
			fmt.Println("\n⚠️  Proceeding with mismatched mounts. Services may fail to start.")
		}
	} else {
		fmt.Println("OK.")
	}

	// 3. Runtime Verification (Is it running?)
	inspectCmd := exec.Command("podman", "machine", "inspect", machineName)
	output, _ := inspectCmd.Output()

	if !strings.Contains(string(output), "\"State\": \"running\"") {
		fmt.Println("Machine is stopped. Booting up...")
		startCmd := exec.Command("podman", "machine", "start", machineName)
		output, err := startCmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to start infrastructure: %w\nOutput: %s", err, string(output))
		}
		fmt.Println("Infrastructure ready.")
	}

	return nil
}

func isPodmanDesktopRunning() (bool, int) {
	cmd := exec.Command("pgrep", "-f", "Podman Desktop")
	out, err := cmd.Output()
	if err != nil {
		return false, 0
	}

	pidStr := strings.TrimSpace(string(out))
	if pidStr == "" {
		return false, 0
	}

	lines := strings.Split(pidStr, "\n")
	if len(lines) > 0 {
		if pid, err := strconv.Atoi(lines[0]); err == nil {
			return true, pid
		}
	}
	return false, 0
}

func collectDiagnostics(reason string, details string) {
	timestamp := time.Now().Format("20060102-150405")
	diagFile := filepath.Join(os.TempDir(), fmt.Sprintf("aleutian-diag-%s.log", timestamp))

	f, err := os.Create(diagFile)
	if err != nil {
		log.Printf("Failed to create diagnostics file: %v", err)
		return
	}
	defer f.Close()

	fmt.Fprintf(f, "=== Aleutian Diagnostics ===\n")
	fmt.Fprintf(f, "Timestamp: %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(f, "Reason: %s\n", reason)
	fmt.Fprintf(f, "Details: %s\n\n", details)

	fmt.Fprintf(f, "=== System Info ===\n")
	fmt.Fprintf(f, "OS: %s\n", runtime.GOOS)
	fmt.Fprintf(f, "Arch: %s\n", runtime.GOARCH)

	fmt.Fprintf(f, "\n=== Podman Version ===\n")
	versionCmd := exec.Command("podman", "version")
	versionOut, _ := versionCmd.CombinedOutput()
	f.Write(versionOut)

	if runtime.GOOS == "darwin" {
		fmt.Fprintf(f, "\n=== Podman Machine List ===\n")
		listCmd := exec.Command("podman", "machine", "list")
		listOut, _ := listCmd.CombinedOutput()
		f.Write(listOut)
	}

	fmt.Fprintf(f, "\n=== Container Logs (last 50 lines) ===\n")
	containers := []string{"aleutian-go-orchestrator", "aleutian-weaviate", "aleutian-rag-engine"}
	for _, container := range containers {
		fmt.Fprintf(f, "\n--- %s ---\n", container)
		logsCmd := exec.Command("podman", "logs", "--tail", "50", container)
		logsOut, _ := logsCmd.CombinedOutput()
		f.Write(logsOut)
	}

	log.Printf("Diagnostics saved to: %s", diagFile)
	log.Printf("   Please include this file when reporting issues.")
}

func runStart(cmd *cobra.Command, args []string) {
	cfg := config.Global
	if backendType != "" {
		cfg.ModelBackend.Type = backendType
		fmt.Printf("Overriding backend to %s\n", backendType)
	}

	// Override forecast mode if specified via CLI flag
	if forecastMode != "" {
		switch forecastMode {
		case "standalone":
			config.Global.Forecast.Mode = config.ForecastModeStandalone
			config.Global.Forecast.Enabled = true
			fmt.Println("Overriding forecast mode to: standalone")
		case "sapheneia":
			config.Global.Forecast.Mode = config.ForecastModeSapheneia
			config.Global.Forecast.Enabled = true
			fmt.Println("Overriding forecast mode to: sapheneia")
		default:
			log.Printf("Warning: Unknown forecast mode '%s', valid options are 'standalone' or 'sapheneia'", forecastMode)
		}
	}

	// 1. Get Force Flag
	forceRecreate, _ := cmd.Flags().GetBool("force-recreate")

	// 2. Run Safe Drift Detection
	machineConfig := config.Global.Machine
	if err := checkAndFixPodmanMachine(machineConfig, forceRecreate); err != nil {
		log.Fatalf("Infrastructure check failed: %v", err)
	}

	if cfg.ModelBackend.Type == "ollama" {
		ensureOllamaRunning()

		// Model verification using ModelEnsurer (Phase 11)
		skipModelCheck, _ := cmd.Flags().GetBool("skip-model-check")
		if !skipModelCheck {
			if err := ensureRequiredModels(context.Background(), cfg); err != nil {
				log.Printf("⚠️  Warning: Model verification issue: %v", err)
				log.Println("   Use --skip-model-check to bypass model verification")
			}
		} else {
			fmt.Println("⏭️  Skipping model verification (--skip-model-check)")
		}
	} else {
		fmt.Println("Running in Cloud/Remote mode. Skipping local AI infrastructure.")
	}

	cliVersion := rootCmd.Version
	stackDir, err := ensureStackDir(cliVersion)
	if err != nil {
		log.Fatalf("Failed to prepare stack directory: %v", err)
	}
	if err := ensureEssentialDirs(stackDir); err != nil {
		log.Fatalf("Failed to create essential directories: %v", err)
	}

	// Note: We don't strictly need to mkdir here anymore because the smart logic below handles it,
	// but keeping it doesn't hurt as a fallback.
	modelsCachePath := filepath.Join(stackDir, "models_cache")
	if _, err := os.Stat(modelsCachePath); os.IsNotExist(err) {
		os.MkdirAll(modelsCachePath, 0755)
	}

	fmt.Println("--- Checking Secrets ---")
	for _, secret := range requiredSecrets {
		if !ensureSecretExists(secret) {
			log.Fatalf("Failed to verify secret: %s. Cannot proceed.", secret.Name)
		}
	}
	fmt.Println("------------------------")

	var dynamicEnv map[string]string
	switch profile {
	case "manual":
		fmt.Println("🛠️ Manual Profile selected.")
		dynamicEnv = make(map[string]string)
	case "low", "standard", "performance", "ultra":
		var fakeRam int
		switch profile {
		case "low":
			fakeRam = 8192
		case "standard":
			fakeRam = 24000
		case "performance":
			fakeRam = 48000
		case "ultra":
			fakeRam = 90000
		}
		dynamicEnv = calculateOptimizedEnv(fakeRam)
	default:
		if cfg.ModelBackend.Type == "ollama" {
			ram, err := getSystemTotalMemory()
			if err != nil {
				slog.Warn("Failed to detect system RAM/VRAM, defaulting to safe mode")
				ram = 8192
			}
			dynamicEnv = calculateOptimizedEnv(ram)
		} else {
			dynamicEnv = make(map[string]string)
			dynamicEnv["LLM_BACKEND_TYPE"] = cfg.ModelBackend.Type
			if cfg.ModelBackend.Type == "openai" && !ensureSecretExists(
				SecretDefinition{Name: "openai_api_key"}) {
				log.Fatalf("OpenAI selected, but no API key found")
			}
		}
	}

	// --- SMART CACHE PATH SELECTION ---
	// 1. Default to local stack directory (Safe fallback for Linux/Windows)
	finalCachePath := filepath.Join(stackDir, "models_cache")

	// 2. Check for manual override via Environment Variable
	if envPath := os.Getenv("ALEUTIAN_MODELS_CACHE"); envPath != "" {
		finalCachePath = envPath
	} else {
		// 3. Auto-Discovery: Check configured drives for an existing cache
		// This prioritizes your external drive (/Volumes/ai_models) if it exists
		for _, drive := range cfg.Machine.Drives {
			// Skip the user's home dir and root /Volumes (too generic)
			home, _ := os.UserHomeDir()
			if strings.HasPrefix(drive, home) || drive == "/Volumes" {
				continue
			}

			// Check for the standard Aleutian data structure on this drive
			// Structure: /Volumes/ai_models/aleutian_data/models_cache
			candidate := filepath.Join(drive, "aleutian_data", "models_cache")
			if _, err := os.Stat(candidate); err == nil {
				fmt.Printf("📦 Auto-detected external model cache: %s\n", candidate)
				finalCachePath = candidate
				break
			}
		}
	}

	// 4. Ensure the directory exists (Prevent statfs errors)
	if _, err := os.Stat(finalCachePath); os.IsNotExist(err) {
		// If it's an external drive that doesn't have the folder yet, we create it.
		// If it's the local fallback, we create it.
		if err := os.MkdirAll(finalCachePath, 0755); err != nil {
			log.Printf("⚠️ Warning: Failed to create model cache at %s: %v", finalCachePath, err)
			// Fallback to local if external creation fails
			finalCachePath = filepath.Join(stackDir, "models_cache")
			os.MkdirAll(finalCachePath, 0755)
		}
	}

	// 5. Verify external cache is accessible from containers (macOS VM issue)
	machineName := config.Global.Machine.Id
	if machineName == "" {
		machineName = "podman-machine-default"
	}
	finalCachePath, err = verifyAndFixExternalCache(finalCachePath, stackDir, machineName, forceRecreate)
	if err != nil {
		log.Fatalf("Cache configuration failed: %v", err)
	}

	// 6. Inject into the map that gets passed to Podman Compose
	if dynamicEnv == nil {
		dynamicEnv = make(map[string]string)
	}
	dynamicEnv["ALEUTIAN_MODELS_CACHE"] = finalCachePath
	// ----------------------------------

	// --- INFLUXDB TOKEN MANAGEMENT ---
	// Auto-generate and store InfluxDB token as a podman secret
	// This is used for both InfluxDB initialization and client authentication
	influxToken, err := ensureAutoGeneratedSecret(SecretDefinition{
		Name:        "influxdb_token",
		DisplayName: "InfluxDB Admin Token",
		Description: "Authentication token for InfluxDB (auto-generated)",
	})
	if err != nil {
		log.Fatalf("Failed to ensure InfluxDB token: %v", err)
	}
	// Pass token to compose via environment variables
	// These are used by InfluxDB for init and by other services for auth
	dynamicEnv["INFLUXDB_TOKEN"] = influxToken
	dynamicEnv["DOCKER_INFLUXDB_INIT_ADMIN_TOKEN"] = influxToken
	// ----------------------------------

	printStartupSummary(stackDir, dynamicEnv)

	// composeArgs: only add EXTRA files here (extensions, forecast)
	// Base compose + override are handled by runPodmanCompose
	var composeArgs []string

	// Extensions Loading
	for _, extPath := range config.Global.Extensions {
		if _, err := os.Stat(extPath); err == nil {
			fmt.Printf("Loading Extension: %s\n", extPath)
			composeArgs = append(composeArgs, "-f", extPath)
		} else {
			log.Printf("Warning: Extension file not found: %s", extPath)
		}
	}

	// Forecast module configuration
	if config.Global.Forecast.Enabled {
		forecastComposePath := filepath.Join(stackDir, "podman-compose.forecast.yml")

		switch config.Global.Forecast.Mode {
		case config.ForecastModeStandalone:
			if _, err := os.Stat(forecastComposePath); err == nil {
				fmt.Println("Loading standalone forecast service")
				composeArgs = append(composeArgs, "-f", forecastComposePath)
			}
			dynamicEnv["ALEUTIAN_TIMESERIES_TOOL"] = "http://forecast-service:8000"
			dynamicEnv["ALEUTIAN_FORECAST_MODE"] = "standalone"

		case config.ForecastModeSapheneia:
			fmt.Println("Using external Sapheneia forecast service")
			dynamicEnv["ALEUTIAN_TIMESERIES_TOOL"] = "http://host.containers.internal:8000"
			dynamicEnv["ALEUTIAN_FORECAST_MODE"] = "sapheneia"

		default:
			if !config.Global.Forecast.Mode.IsValid() {
				log.Printf("Warning: Unknown forecast mode '%s', defaulting to standalone",
					config.Global.Forecast.Mode)
				dynamicEnv["ALEUTIAN_TIMESERIES_TOOL"] = "http://forecast-service:8000"
				dynamicEnv["ALEUTIAN_FORECAST_MODE"] = "standalone"
			}
		}
	}

	composeArgs = append(composeArgs, "up", "-d")

	// Auto-detect if orchestrator rebuild is needed based on source file timestamps.
	// Uses ImageChecker to compare file modification times against image creation time.
	if !forceBuild {
		imageChecker := infra.NewDefaultImageChecker()
		orchestratorDir := filepath.Join(stackDir, "services", "orchestrator")
		needsRebuild, err := imageChecker.NeedsRebuild(
			"aleutian-go-orchestrator",
			orchestratorDir,
			[]string{".go", "Dockerfile", "go.mod", "go.sum"},
		)
		if err != nil {
			slog.Debug("Image check failed, skipping auto-rebuild", "error", err)
		} else if needsRebuild {
			fmt.Println("🔄 Detected code changes in orchestrator, triggering rebuild...")
			forceBuild = true
		}
	}

	if forceBuild {
		fmt.Println("Force build enabled: Recompiling containers")
		composeArgs = append(composeArgs, "--build")
	}

	err = runPodmanCompose(stackDir, dynamicEnv, composeArgs...)
	if err != nil {
		collectDiagnostics("Startup Failed", err.Error())
		log.Fatalf("Failed to start services: %v", err)
	}

	// Clean up dangling images after build to prevent disk space accumulation.
	// This is a non-critical operation; errors are logged but not fatal.
	if forceBuild {
		pruneChecker := infra.NewDefaultImageChecker()
		if pruneErr := pruneChecker.PruneDanglingImages(); pruneErr != nil {
			slog.Debug("Image prune failed (non-fatal)", "error", pruneErr)
		}
	}

	fmt.Println("\n⏳ Waiting for services to initialize...")
	if err := waitForServicesReady(); err != nil {
		collectDiagnostics("Health Check Failed", err.Error())
		log.Printf("⚠️  Warning: Some services may not be fully ready: %v", err)
		log.Println("   Check logs with: aleutian stack logs [service-name]")
	} else {
		fmt.Println("✅ All services are healthy")
	}

	fmt.Println("\nLocal Aleutian appliance started.")
	fmt.Printf("Orchestrator available at %s\n", getOrchestratorBaseURL())
	fmt.Println("Check 'podman ps' for exposed host ports.")
}

// ensureRequiredModels verifies that required AI models are available.
//
// # Description
//
// Creates a ModelEnsurer configured from the global config and ensures
// embedding and LLM models are available locally or can be pulled.
// This integrates the Phase 11 ModelEnsurer system.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - cfg: AleutianConfig with model backend settings
//
// # Outputs
//
//   - error: Non-nil if model verification fails
//
// # Examples
//
//	if err := ensureRequiredModels(ctx, cfg); err != nil {
//	    log.Printf("Warning: %v", err)
//	}
//
// # Limitations
//
//   - Requires Ollama to be running at the configured URL
//   - Network required for pulling missing models
//
// # Assumptions
//
//   - ensureOllamaRunning() has been called
//   - Config has valid ModelBackend settings
func ensureRequiredModels(ctx context.Context, cfg config.AleutianConfig) error {
	fmt.Println("--- Checking Required Models ---")

	// Build ModelEnsurerConfig from AleutianConfig
	// Note: EmbeddingModel/LLMModel are in cfg.ModelBackend.Ollama
	ensurerCfg := ModelEnsurerConfig{
		EmbeddingModel: cfg.ModelBackend.Ollama.EmbeddingModel,
		LLMModel:       cfg.ModelBackend.Ollama.LLMModel,
		OllamaBaseURL:  cfg.ModelBackend.Ollama.BaseURL,
		BackendType:    cfg.ModelBackend.Type,
		DiskLimitGB:    cfg.ModelManagement.DiskLimitGB,
	}

	// Apply defaults if not set
	if ensurerCfg.EmbeddingModel == "" {
		ensurerCfg.EmbeddingModel = DefaultEmbeddingModel
	}
	if ensurerCfg.LLMModel == "" && ensurerCfg.BackendType == "ollama" {
		ensurerCfg.LLMModel = DefaultLLMModel
	}
	if ensurerCfg.OllamaBaseURL == "" {
		ensurerCfg.OllamaBaseURL = config.DefaultOllamaHostURL
	}

	ensurer := NewDefaultModelEnsurer(ensurerCfg)

	// Set up progress callback for TTY-friendly output
	// PullProgressCallback signature: func(status string, completed, total int64)
	ensurer.SetProgressCallback(func(status string, completed, total int64) {
		if total > 0 {
			percent := float64(completed) / float64(total) * 100
			fmt.Printf("\r📥 %s: %.1f%%", status, percent)
		} else {
			fmt.Printf("\r📥 %s...", status)
		}
		if completed == total && total > 0 {
			fmt.Printf("\n")
		}
	})

	result, err := ensurer.EnsureModels(ctx)
	if err != nil {
		return fmt.Errorf("model verification failed: %w", err)
	}

	// Log warnings
	for _, warning := range result.Warnings {
		fmt.Printf("⚠️  %s\n", warning)
	}

	// Log pulled models
	for _, pulled := range result.ModelsPulled {
		fmt.Printf("📥 Downloaded: %s\n", pulled)
	}

	if !result.CanProceed {
		return fmt.Errorf("required models unavailable")
	}

	fmt.Println("✅ All required models verified")
	fmt.Println("--------------------------------")
	return nil
}

func ensureOllamaRunning() {
	client := http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get("http://localhost:11434")
	if err == nil {
		resp.Body.Close()
		return
	}

	if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "connect") {
		fmt.Println("Ollama is not running. Attempting to start ollama in background...")
		cmd := exec.Command("ollama", "serve")
		if err := cmd.Start(); err != nil {
			log.Fatalf("Failed to start ollama. Is it installed? %v", err)
		}
		fmt.Printf("Ollama service started with PID: %d\n", cmd.Process.Pid)
		time.Sleep(3 * time.Second)
	} else {
		log.Fatalf("Failed to connect to Ollama (http://localhost:11434): %v. Is it installed?", err)
	}
}

func hasForeignWorkloads() (bool, []string, error) {
	// List all containers with their names and labels
	args := []string{"ps", "-a", "--format", "{{.Names}}", "--filter",
		"label!=io.podman.compose.project=aleutian"}
	cmd := exec.Command("podman", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, nil, fmt.Errorf("failed to inspect the container list %w", err)
	}
	outputStr := strings.TrimSpace(string(output))
	if outputStr == "" {
		return false, nil, nil
	}
	names := strings.Split(outputStr, "\n")
	return true, names, nil
}

// waitForServicesReady waits for all critical services to become healthy.
//
// # Description
//
// Uses the HealthChecker interface with exponential backoff to intelligently
// wait for services during stack startup. This replaces the naive fixed-delay
// implementation with smarter retry logic that prevents Heisenbug conditions.
//
// # Outputs
//
//   - error: Non-nil if services fail to become healthy within timeout
//
// # Examples
//
//	if err := waitForServicesReady(); err != nil {
//	    log.Printf("Services not ready: %v", err)
//	}
//
// # Limitations
//
//   - Only checks services that have running containers
//   - Uses default 60-second timeout
//
// # Assumptions
//
//   - Containers are already started via podman-compose
//   - Network is accessible on localhost
func waitForServicesReady() error {
	// Create HealthChecker with production dependencies
	proc := process.NewDefaultManager()
	checker := health.NewDefaultHealthChecker(proc, health.DefaultHealthCheckerConfig())

	// Build service definitions for critical services
	services := []health.ServiceDefinition{
		{
			ID:             health.GenerateID(),
			Name:           "Orchestrator",
			URL:            fmt.Sprintf("%s/health", getOrchestratorBaseURL()),
			ContainerName:  "aleutian-go-orchestrator",
			CheckType:      health.HealthCheckHTTP,
			Critical:       true,
			Timeout:        10 * time.Second,
			ExpectedStatus: 200,
			Version:        health.HealthCheckVersion,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
		{
			ID:             health.GenerateID(),
			Name:           "Data Fetcher",
			URL:            "http://localhost:12001/health",
			ContainerName:  "aleutian-data-fetcher",
			CheckType:      health.HealthCheckHTTP,
			Critical:       true,
			Timeout:        10 * time.Second,
			ExpectedStatus: 200,
			Version:        health.HealthCheckVersion,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
		{
			ID:             health.GenerateID(),
			Name:           "Weaviate",
			URL:            "http://localhost:8080/v1/.well-known/ready",
			ContainerName:  "weaviate-db",
			CheckType:      health.HealthCheckHTTP,
			Critical:       true,
			Timeout:        10 * time.Second,
			ExpectedStatus: 200,
			Version:        health.HealthCheckVersion,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		},
	}

	// Use default wait options with exponential backoff
	opts := health.DefaultWaitOptions()
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()

	// Wait for all services with intelligent retry logic
	result, err := checker.WaitForServices(ctx, services, opts)
	if err != nil {
		return err
	}

	// Print individual service statuses
	for _, status := range result.Services {
		if status.State == health.HealthStateHealthy {
			fmt.Printf("   Checking %s... ✓ (%.1fs)\n",
				status.Name,
				status.Latency.Seconds())
		} else if status.State == health.HealthStateSkipped {
			fmt.Printf("   Checking %s... - (skipped)\n", status.Name)
		} else {
			fmt.Printf("   Checking %s... ✗ (%s)\n",
				status.Name,
				status.Message)
		}
	}

	if !result.Success {
		return fmt.Errorf("some services failed health checks")
	}

	return nil
}

func runStatus(cmd *cobra.Command, args []string) {
	fmt.Println("=== Aleutian Stack Status ===")

	if runtime.GOOS == "darwin" {
		machineConfig := config.Global.Machine
		machineName := machineConfig.Id
		if machineName == "" {
			machineName = "podman-machine-default"
		}

		inspectCmd := exec.Command("podman", "machine", "inspect", machineName, "--format", "{{.State}}")
		out, err := inspectCmd.Output()
		if err == nil {
			state := strings.TrimSpace(string(out))
			stateEmoji := "❌"
			if state == "running" {
				stateEmoji = "✅"
			}
			fmt.Printf("%s Podman Machine: %s\n", stateEmoji, state)
		} else {
			fmt.Println("❌ Podman Machine: Not Found or Error")
		}
	}

	fmt.Println("\nContainers:")
	psCmd := exec.Command("podman", "ps", "--filter", "label=io.podman.compose.project=aleutian",
		"--format", "{{.Names}}\t{{.Status}}\t{{.Ports}}")
	psCmd.Stdout = os.Stdout
	psCmd.Stderr = os.Stderr
	psCmd.Run()

	fmt.Println("\nResource Usage:")
	statsCmd := exec.Command("podman", "stats", "--no-stream",
		"--format", "{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}")
	statsCmd.Stdout = os.Stdout
	statsCmd.Stderr = os.Stderr
	statsCmd.Run()

	fmt.Println("\nService Health:")
	healthChecks := []struct {
		name          string
		url           string
		containerName string // Optional: if set, only check if container is running
	}{
		{"Orchestrator", fmt.Sprintf("%s/health", getOrchestratorBaseURL()), "aleutian-go-orchestrator"},
		{"Data Fetcher", "http://localhost:12001/health", "aleutian-data-fetcher"},
		{"Weaviate", "http://localhost:12127/v1/.well-known/ready", "weaviate-db"},
		{"Forecast", "http://localhost:12000/health", "aleutian-forecast"},
		{"Ollama", "http://localhost:11434", ""}, // Ollama runs on host, always check
	}

	for _, hc := range healthChecks {
		// Skip container-based services that aren't running
		if hc.containerName != "" {
			checkCmd := exec.Command("podman", "ps", "--filter", fmt.Sprintf("name=%s", hc.containerName), "--format", "{{.Names}}")
			out, err := checkCmd.Output()
			if err != nil || strings.TrimSpace(string(out)) == "" {
				continue // Container not running, skip
			}
		}

		status := "Unreachable"
		client := http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(hc.url)
		if err == nil {
			if resp.StatusCode == 200 {
				status = "✅ Healthy"
			} else {
				status = fmt.Sprintf("⚠️  HTTP %d", resp.StatusCode)
			}
			resp.Body.Close()
		}
		fmt.Printf("   %s: %s\n", hc.name, status)
	}
}

// ... existing runStop, runLogsCommand, etc ...
func runStop(cmd *cobra.Command, args []string) {
	checkCmd := exec.Command("podman", "info")
	checkCmd.Stdout = io.Discard
	checkCmd.Stderr = io.Discard
	if err := checkCmd.Run(); err != nil {
		fmt.Println("⚠️  Podman is unreachable (Machine is likely stopped).")
		fmt.Println("   Skipping teardown as services are already offline.")
		return
	}

	fmt.Println("Stopping local Aleutian services...")
	stackDir, err := getStackDir()
	if err != nil {
		log.Printf("Warning: Could not determine stack directory (%v), attempting run from current dir.", err)
		stackDir = "."
	}

	// Step 1: Stop all Aleutian containers gracefully (with timeout)
	// Use name filter as it's more reliable than compose project label
	fmt.Println("   Stopping containers...")
	stopCmd := exec.Command("podman", "stop", "-t", "10", "-a", "--filter", "name=aleutian-")
	stopCmd.Stdout = io.Discard
	stopCmd.Stderr = io.Discard
	stopCmd.Run() // Ignore errors - some containers may already be stopped

	// Step 2: Force stop any that didn't stop gracefully
	forceStopCmd := exec.Command("podman", "stop", "-t", "0", "-a", "--filter", "name=aleutian-")
	forceStopCmd.Stdout = io.Discard
	forceStopCmd.Stderr = io.Discard
	forceStopCmd.Run()

	// Step 3: Now run compose down to clean up networks/pods
	err = runPodmanCompose(stackDir, nil, "down", "--remove-orphans")
	if err != nil {
		// If compose down still fails, try force cleanup
		log.Printf("Warning: Compose down had issues, attempting force cleanup...")
		forceCleanup()
	}

	fmt.Println("\nLocal Aleutian services stopped.")
}

// forceCleanup removes all Aleutian containers and pods when compose down fails
func forceCleanup() {
	// Remove containers by name pattern
	rmCmd := exec.Command("podman", "rm", "-f", "-a", "--filter", "name=aleutian-")
	rmCmd.Stdout = io.Discard
	rmCmd.Stderr = io.Discard
	rmCmd.Run()

	// Also try by compose project label (covers both naming conventions)
	rmCmd2 := exec.Command("podman", "rm", "-f", "-a", "--filter", "label=io.podman.compose.project=aleutianlocal")
	rmCmd2.Stdout = io.Discard
	rmCmd2.Stderr = io.Discard
	rmCmd2.Run()

	// Remove pods that match aleutian pattern
	// First list them, then remove
	listPodsCmd := exec.Command("podman", "pod", "ls", "-q", "--filter", "name=aleutian")
	output, err := listPodsCmd.Output()
	if err == nil && len(strings.TrimSpace(string(output))) > 0 {
		podIDs := strings.Fields(string(output))
		for _, podID := range podIDs {
			exec.Command("podman", "pod", "rm", "-f", podID).Run()
		}
	}
}

func printStartupSummary(stackDir string, dynamicEnv map[string]string) {
	fmt.Println("\n--- Aleutian Startup Configuration ---")

	backend := os.Getenv("LLM_BACKEND_TYPE")
	if val, ok := dynamicEnv["LLM_BACKEND_TYPE"]; ok {
		backend = val
	}
	if backend == "" {
		backend = "ollama (default)"
	}
	fmt.Printf("   Backend:   %s\n", backend)

	model := ""
	source := "Unknown"

	if val, ok := dynamicEnv["OLLAMA_MODEL"]; ok {
		model = val
		source = "Auto-Profile"
	}

	if model == "" {
		overridePath := filepath.Join(stackDir, "podman-compose.override.yml")
		if content, err := os.ReadFile(overridePath); err == nil {
			lines := strings.Split(string(content), "\n")
			for _, line := range lines {
				trim := strings.TrimSpace(line)
				if strings.Contains(trim, "OLLAMA_MODEL:") {
					parts := strings.SplitN(trim, ":", 2)
					if len(parts) == 2 {
						model = strings.TrimSpace(strings.ReplaceAll(parts[1], "\"", ""))
						source = "Override File"
						break
					}
				}
			}
		}
	}

	if model == "" {
		model = "gpt-oss:latest (default)"
		source = "Default"
	}

	fmt.Printf("   Model:     \x1b[32m%s\x1b[0m  [%s]\n", model, source)
	fmt.Println("-----------------------------------------")
}

func runLogsCommand(cmd *cobra.Command, args []string) {
	logArgs := []string{"logs", "-f"}
	if len(args) > 0 {
		logArgs = append(logArgs, args...)
		fmt.Printf("Streaming logs for %s\n", strings.Join(args, " "))
	} else {
		fmt.Println("Streaming the logs for all services")
	}
	stackDir, err := getStackDir()
	if err != nil {
		log.Printf("Warning: could not determine the stack directory %v", err)
		stackDir = "."
	}
	composeFilePath := filepath.Join(stackDir, "podman-compose.yml")
	if _, err := os.Stat(composeFilePath); os.IsNotExist(err) {
		log.Fatalf("stack files not found in %s: %v", composeFilePath, err)
	}
	err = runPodmanCompose(stackDir, nil, logArgs...)
	if err != nil {
		fmt.Println("\nLog streaming stopped or encountered an error")
	} else {
		fmt.Println("\nLog streaming finished")
	}
}

func runPodmanCompose(stackDir string, extraEnv map[string]string, args ...string) error {
	composeFilePath := filepath.Join(stackDir, "podman-compose.yml")
	overrideFilePath := filepath.Join(stackDir, "podman-compose.override.yml")

	// Always start with base compose file
	fileArgs := []string{"-f", composeFilePath}

	// Add override if it exists
	if _, err := os.Stat(overrideFilePath); err == nil {
		fileArgs = append(fileArgs, "-f", overrideFilePath)
		fmt.Println("    (Including podman-compose.override.yml)")
	}

	// Separate -f flags and other args from the input
	var extraFileArgs []string
	var otherArgs []string
	for i := 0; i < len(args); i++ {
		if args[i] == "-f" && i+1 < len(args) {
			extraFileArgs = append(extraFileArgs, "-f", args[i+1])
			i++ // skip the path
		} else {
			otherArgs = append(otherArgs, args[i])
		}
	}

	// Build final command: base files + extra files + other args
	cmdArgs := append(fileArgs, extraFileArgs...)
	cmdArgs = append(cmdArgs, otherArgs...)

	fmt.Printf("Executing: podman-compose %s (in %s)\n", strings.Join(cmdArgs, " "), stackDir)

	cmd := exec.Command("podman-compose", cmdArgs...)
	cmd.Dir = stackDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if len(extraEnv) > 0 {
		fmt.Println("Injecting dynamic configuration:")
		for k, v := range extraEnv {
			fmt.Printf("    - %s=%s\n", k, v)
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	return cmd.Run()
}

func runDestroy(cmd *cobra.Command, args []string) {
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
	fmt.Println("Destroying local Aleutian instance and data...")
	stackDir, err := getStackDir()
	if err != nil {
		log.Printf("Warning: Could not determine stack directory (%v), attempting run from current dir.", err)
		stackDir = "."
	}
	composeFilePath := filepath.Join(stackDir, "podman-compose.yml")
	if _, err := os.Stat(composeFilePath); os.IsNotExist(err) {
		log.Println("Stack files not found. Nothing to destroy.")
		return
	}

	err = runPodmanCompose(stackDir, nil, "down", "-v")
	if err != nil {
		log.Fatalf("Failed to destroy services and volumes: %v", err)
	}
	fmt.Print("Do you want to remove the downloaded stack files from ~/.aleutian/stack? (yes/no): ")
	reader = bufio.NewReader(os.Stdin)
	input, _ = reader.ReadString('\n')
	if strings.TrimSpace(strings.ToLower(input)) == "yes" {
		fmt.Printf("Removing %s...\n", stackDir)
		err := os.RemoveAll(stackDir)
		if err != nil {
			log.Printf("Warning: Failed to remove stack directory %s: %v\n", stackDir, err)
		}
	}

	fmt.Println("\nLocal Aleutian instance and data destroyed.")
}
