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
	"github.com/spf13/cobra"
)

func calculateOptimizedEnv(totalRAM_MB int) map[string]string {
	env := make(map[string]string)
	fmt.Printf("Optimization Engine: Detected %d MB Compute Memory (VRAM)\n", totalRAM_MB)
	const (
		LOW_RAM  = 16384
		MID_RAM  = 32768
		HIGH_RAM = 65536
	)
	env["OLLAMA_MODEL"] = "gemma3:4b"
	env["LLM_DEFAULT_MAX_TOKENS"] = "2048"
	env["RERANKER_MODEL"] = "cross-encoder/ms-marco-TinyBERT-L-2-v2"
	env["WEAVIATE_QUERY_DEFAULTS_LIMIT"] = "5"

	if totalRAM_MB >= LOW_RAM && totalRAM_MB < MID_RAM {
		fmt.Println("    -> Profile: Standard (16GB to 32GB of VRAM)")
		env["OLLAMA_MODEL"] = "gemma3:12b"
		env["LLM_DEFAULT_MAX_TOKENS"] = "4096"
		env["RERANKER_MODEL"] = "cross-encoder/ms-marco-MiniLM-L-6-v2"
	} else if totalRAM_MB >= MID_RAM && totalRAM_MB < HIGH_RAM {
		fmt.Println("   -> Profile: Performance (32GB+)")
		//env["OLLAMA_MODEL"] = "gemma3:27b"     // Powerful model
		env["LLM_DEFAULT_MAX_TOKENS"] = "8192" // Larger context
		env["OLLAMA_MODEL"] = "gpt-oss:20b"
		env["RERANKER_MODEL"] = "cross-encoder/ms-marco-MiniLM-L-6-v2"
		env["RERANK_FINAL_K"] = "10" // Re-rank more results
	} else if totalRAM_MB >= HIGH_RAM {
		fmt.Println("   -> Profile: Ultra (64GB+)")
		env["OLLAMA_MODEL"] = "llama3:70b" // Enterprise grade
		env["RERANKER_MODEL"] = "cross-encoder/ms-marco-MiniLM-L-6-v2"
		env["LLM_DEFAULT_MAX_TOKENS"] = "32768" // Massive context
	}
	return env
}

func checkAndFixPodmanMachine(cfg config.MachineConfig) error {
	// 1. Only run this logic on macOS (Darwin)
	if runtime.GOOS != "darwin" {
		return nil
	}

	machineName := cfg.Id
	// Defaults if config is missing (unlikely given DefaultConfig)
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
	fmt.Println("Aleutian Infrastructure Check...")
	fmt.Printf("   Target Machine: %s (CPUs: %d, Mem: %d MB)\n", machineName, cpuCount, memAmount)

	// 2. Check if machine exists
	checkCmd := exec.Command("podman", "machine", "inspect", machineName)
	if err := checkCmd.Run(); err != nil {
		fmt.Printf("Machine not found. Provisioning Infrastructure...\n")

		args := []string{"machine", "init", machineName,
			"--cpus", strconv.Itoa(cpuCount),
			"--memory", strconv.Itoa(memAmount),
		}

		for _, drive := range cfg.Drives {
			// Mount it to the same path inside the VM
			mountStr := fmt.Sprintf("%s:%s", drive, drive)
			fmt.Printf("   - Mounting: %s\n", mountStr)
			args = append(args, "-v", mountStr)
		}

		initCmd := exec.Command("podman", args...)
		initCmd.Stdout = os.Stdout
		initCmd.Stderr = os.Stderr
		if err := initCmd.Run(); err != nil {
			return fmt.Errorf("failed to provision podman machine: %w", err)
		}
		fmt.Println("Infrastructure provisioned.")
	}

	// 3. Connectivity & Health Check
	inspectCmd := exec.Command("podman", "machine", "inspect", machineName)
	output, _ := inspectCmd.Output()

	needsStart := false
	isRestart := false

	if !strings.Contains(string(output), "\"State\": \"running\"") {
		fmt.Println("Machine is stopped. Booting up...")
		needsStart = true
	} else {
		// Machine is technically running. Test the mount to see if it's "stale".
		// We test the FIRST drive in the list as a canary.
		if len(cfg.Drives) > 0 {
			targetMount := cfg.Drives[0]
			fmt.Printf("🔍 Verifying connectivity to %s... ", targetMount)

			// We use a context with timeout to detect if the mount is hung
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Run 'ls' inside the VM using a tiny command.
			testCmd := exec.CommandContext(ctx, "podman", "run", "--rm",
				"-v", fmt.Sprintf("%s:%s", targetMount, targetMount),
				"alpine", "ls", targetMount)

			if err := testCmd.Run(); err != nil {
				fmt.Println("FAILED.")

				if ctx.Err() == context.DeadlineExceeded {
					// Case A: Timeout (Sleep Crash) -> Soft Reboot
					fmt.Println("   Reason: Drive access timed out (The 'Sleep Crash').")
					fmt.Println("️  Self-Healing: Restarting infrastructure to reconnect drives...")
					exec.Command("podman", "machine", "stop", machineName).Run()
					needsStart = true
					isRestart = true
				} else {
					// Case B: Immediate Error (Missing Mount) -> Factory Reset
					fmt.Printf("   Reason: Drive disconnected or unreadable.\n")
					fmt.Println("   Diagnosis: The Podman machine configuration is missing required mounts.")
					fmt.Println("🛠️  Self-Healing: Performing Factory Reset on Podman machine to fix configuration...")

					// 1. Destroy the broken machine
					rmCmd := exec.Command("podman", "machine", "rm", "-f", machineName)
					rmCmd.Stdout = os.Stdout
					rmCmd.Stderr = os.Stderr
					if err := rmCmd.Run(); err != nil {
						return fmt.Errorf("failed to remove broken machine during self-healing: %w", err)
					}

					// 2. Recurse to provision it correctly (Block A will now run)
					fmt.Println("   Machine removed. Re-provisioning with correct mounts...")
					return checkAndFixPodmanMachine(cfg)
				}
			} else {
				fmt.Println("OK.")
			}
		}
	}

	if needsStart {
		startCmd := exec.Command("podman", "machine", "start", machineName)
		startCmd.Stdout = os.Stdout
		startCmd.Stderr = os.Stderr

		if err := startCmd.Run(); err != nil {
			return fmt.Errorf("failed to start infrastructure: %w", err)
		}
		if isRestart {
			fmt.Println("Infrastructure rebooted. External drives reconnected.")
		} else {
			fmt.Println("Infrastructure ready.")
		}
	}

	return nil
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

	err = runPodmanCompose(stackDir, nil, "down", "-v") // Add -v flag
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
	fmt.Printf("Executing: podman-compose %s (in %s)\n", strings.Join(args, " "), stackDir)

	composeFilePath := filepath.Join(stackDir, "podman-compose.yml")
	overrideFilePath := filepath.Join(stackDir, "podman-compose.override.yml")
	fileArgs := []string{"-f", composeFilePath}

	// Add override file if it exists
	if _, err := os.Stat(overrideFilePath); err == nil {
		fileArgs = append(fileArgs, "-f", overrideFilePath)
		fmt.Println("    (Including podman-compose.override.yml)")
	}
	// If the args contain "-f", we assume caller knows what they are doing and don't prepend our defaults.
	hasFileFlag := false
	for _, arg := range args {
		if arg == "-f" {
			hasFileFlag = true
			break
		}
	}
	var cmdArgs []string
	if hasFileFlag {
		cmdArgs = args
	} else {
		cmdArgs = append(fileArgs, args...)
	}

	cmd := exec.Command("podman-compose", cmdArgs...)
	cmd.Dir = stackDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Injection Logic
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

func runStart(cmd *cobra.Command, args []string) {
	cfg := config.Global
	if backendType != "" {
		cfg.ModelBackend.Type = backendType
		fmt.Printf("Overriding backend to %s\n", backendType)
	}
	// Controller Logic: Ensure Infrastructure (VM) is correct
	//    This reads from ~/.aleutian/aleutian.yaml
	machineConfig := config.Global.Machine
	if err := checkAndFixPodmanMachine(machineConfig); err != nil {
		log.Fatalf("Infrastructure check failed: %v", err)
	}
	if cfg.ModelBackend.Type == "ollama" {
		ensureOllamaRunning()
	} else {
		fmt.Println("Running in Cloud/Remote mode. Skipping local AI infrastructure.")
	}

	cliVersion := rootCmd.Version
	// 2. Prepare Application Stack (Download/Update)
	stackDir, err := ensureStackDir(cliVersion)
	if err != nil {
		log.Fatalf("Failed to prepare stack directory: %v", err)
	}
	if err := ensureEssentialDirs(stackDir); err != nil {
		log.Fatalf("Failed to create essential directories in stack: %v", err)
	}

	// Secret Check
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
		fmt.Println("🛠️ Manual Profile selected: Skipping auto-optimization. Using podman-compose.override.yml values.")
		dynamicEnv = make(map[string]string) // Empty map, lets overrides take precedence

	case "low", "standard", "performance", "ultra":
		// Force a specific profile regardless of actual RAM
		// You might need to refactor calculateOptimizedEnv to accept a string,
		// or just simulate the RAM value here to trick it.
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
			} else if cfg.ModelBackend.Type == "anthropic" && !ensureSecretExists(
				SecretDefinition{Name: "anthropic_api_key"}) {
				log.Fatalf("Anthropic selected, but no API key found")
			} else if cfg.ModelBackend.Type == "gemini" && !ensureSecretExists(
				SecretDefinition{Name: "gemini_api_key"}) {
				log.Fatalf("Gemini selected, but no API key found")
			}
		}
	}

	// --- NEW: Print the Truth ---
	printStartupSummary(stackDir, dynamicEnv)
	// ----------------------------

	composeArgs := []string{"-f", filepath.Join(stackDir, "podman-compose.yml")}

	overridePath := filepath.Join(stackDir, "podman-compose.override.yml")
	if _, err := os.Stat(overridePath); err == nil {
		fmt.Println("🔌 Loading local override configuration")
		composeArgs = append(composeArgs, "-f", overridePath)
	}

	extensions := config.Global.Extensions
	if len(extensions) > 0 {
		fmt.Printf("Loading %d custom extensions:\n", len(extensions))
		for _, extPath := range extensions {
			if _, err := os.Stat(extPath); err == nil {
				fmt.Printf("    - %s\n", extPath)
				composeArgs = append(composeArgs, "-f", extPath)
			} else {
				log.Printf("Warning: Extension file not found: %s", extPath)
			}
		}
	}

	composeArgs = append(composeArgs, "up", "-d")
	if forceBuild {
		fmt.Println("Force build enabled: Recompiling containers")
		composeArgs = append(composeArgs, "--build")
	}

	err = runPodmanCompose(stackDir, dynamicEnv, composeArgs...)
	if err != nil {
		log.Fatalf("Failed to start services: %v", err)
	}

	fmt.Println("\nLocal Aleutian appliance started.")
	fmt.Printf("Orchestrator available at %s\n", getOrchestratorBaseURL())
	fmt.Println("Check 'podman ps' for exposed host ports.")
}

func ensureOllamaRunning() {
	resp, err := http.Get("http://localhost:11434")
	if err == nil {
		resp.Body.Close()
		return
	}

	if strings.Contains(err.Error(), "Couldn't connect to server") {
		fmt.Println("Ollama is not running. Attempting to start ollama.")
		cmd := exec.Command("ollama", "serve", "nohup")
		if err := cmd.Start(); err != nil {
			log.Fatalf("Failed to start ollama. Please make sure it's installed: %v", err)
		}
		fmt.Printf("Ollama service started in the background with PID: %d\n", cmd.Process.Pid)
	} else {
		log.Fatalf("Failed to start ollama. Please make sure it's installed: %v", err)
	}
}

func runStop(cmd *cobra.Command, args []string) {
	// Pre-flight check: Is Podman responding?
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

	err = runPodmanCompose(stackDir, nil, "down")
	if err != nil {
		log.Printf("Warning: Failed to stop services: %v", err)
	} else {
		fmt.Println("\nLocal Aleutian services stopped.")
	}
}

// printStartupSummary inspects the final configuration to tell the user what is running
func printStartupSummary(stackDir string, dynamicEnv map[string]string) {
	fmt.Println("\n--- Aleutian Startup Configuration ---")

	// 1. Determine Backend
	backend := os.Getenv("LLM_BACKEND_TYPE")
	if val, ok := dynamicEnv["LLM_BACKEND_TYPE"]; ok {
		backend = val
	}
	if backend == "" {
		backend = "ollama (default)"
	}
	fmt.Printf("   Backend:   %s\n", backend)

	// 2. Determine Model (The Tricky Part)
	// Precedence: Dynamic Env (Profile) > Override File > Config Default
	model := ""
	source := "Unknown"

	// Check Dynamic Env (Auto profiles)
	if val, ok := dynamicEnv["OLLAMA_MODEL"]; ok {
		model = val
		source = "Auto-Profile"
	}

	// Check Override File (Manual profile)
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
