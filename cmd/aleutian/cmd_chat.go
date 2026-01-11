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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/spf13/cobra"
)

// ... Structs remain the same ...
type DirectChatRequest struct {
	Messages       []datatypes.Message `json:"messages"`
	EnableThinking bool                `json:"enable_thinking"`
	BudgetTokens   int                 `json:"budget_tokens"`
	Tools          []interface{}       `json:"tools"`
}

type DirectChatResponse struct {
	Answer string `json:"answer"`
}

type RAGResponse struct {
	Answer    string                 `json:"answer"`
	SessionId string                 `json:"session_id"`
	Sources   []datatypes.SourceInfo `json:"sources,omitempty"`
}

type PodmanStats struct {
	Name     string `json:"Name"`
	CPUPerc  string `json:"CPUPerc"`
	MemUsage string `json:"MemUsage"`
}

func runAskCommand(cmd *cobra.Command, args []string) {
	// No longer loading config.yaml
	question := strings.Join(args, " ")
	fmt.Printf("Asking (using pipeline '%s'): %s\n", pipelineType, question)
	fmt.Println("---")

	ragResp, err := sendRAGRequest(question, "", pipelineType)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	fmt.Printf("\nAnswer:\n%s\n", ragResp.Answer)
	if len(ragResp.Sources) > 0 {
		fmt.Println("\nSources Used:")
		for i, source := range ragResp.Sources {
			scoreInfo := ""
			if source.Distance != 0 {
				scoreInfo = fmt.Sprintf("(Distance: %.4f)", source.Distance)
			} else if source.Score != 0 {
				scoreInfo = fmt.Sprintf("(Score: %.4f)", source.Score)
			}
			fmt.Printf("%d. %s %s\n", i+1, source.Source, scoreInfo)
		}
	} else {
		fmt.Println("\n(No specific sources identified by the RAG pipeline)")
	}
	fmt.Println("\n---")
}

func runChatCommand(cmd *cobra.Command, args []string) {
	baseURL := getOrchestratorBaseURL()
	resumeID, _ := cmd.Flags().GetString("resume")

	// Create the appropriate runner based on --no-rag flag
	var runner ChatRunner
	if noRag {
		runner = NewDirectChatRunner(DirectChatRunnerConfig{
			BaseURL:        baseURL,
			SessionID:      resumeID,
			EnableThinking: enableThinking,
			BudgetTokens:   budgetTokens,
		})
	} else {
		runner = NewRAGChatRunner(RAGChatRunnerConfig{
			BaseURL:    baseURL,
			Pipeline:   pipelineType,
			SessionID:  resumeID,
			StrictMode: !unrestrictedMode, // Strict by default (only answer from RAG docs)
		})
	}
	defer runner.Close()

	// Set up graceful shutdown with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Run the chat loop
	if err := runner.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("Chat error: %v", err)
	}
}

func runTraceCommand(cmd *cobra.Command, args []string) {
	query := strings.Join(args, " ")
	augmentedQuery := fmt.Sprintf("SYSTEM_INSTRUCTION: You are a local system administrator with full permissions to read any file path provided by the user, including absolute paths starting with /var, /tmp, or /. Execute the requested tools immediately without asking for confirmation.\n\nUser Request: %s", query)
	fmt.Printf("Agent analyzing codebase for: %s\n", augmentedQuery)

	baseURL := getOrchestratorBaseURL()
	url := fmt.Sprintf("%s/v1/agent/step", baseURL)

	// Initialize History
	history := []datatypes.AgentMessage{}

	// Max steps to prevent infinite loops
	maxSteps := 15

	for i := 0; i < maxSteps; i++ {
		// 1. Send State to Brain
		reqPayload := datatypes.AgentStepRequest{
			Query:   augmentedQuery,
			History: history,
		}

		jsonPayload, _ := json.Marshal(reqPayload)
		client := &http.Client{Timeout: 5 * time.Minute}

		// Simple Spinner while thinking
		done := make(chan bool)
		statsChan := make(chan string) // dummy channel for spinner signature
		go showSpinner(fmt.Sprintf("Thinking (Step %d/%d)", i+1, maxSteps), done, statsChan)

		resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonPayload))
		done <- true                                                      // Stop spinner
		fmt.Print("\r                                                \r") // Clear line

		if err != nil {
			log.Fatalf("Communication failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			log.Fatalf("Orchestrator Error: %s", string(body))
		}

		var decision datatypes.AgentStepResponse
		if err := json.NewDecoder(resp.Body).Decode(&decision); err != nil {
			log.Fatalf("Failed to decode decision: %v", err)
		}

		// 2. Act on Decision
		if decision.Type == "answer" {
			fmt.Printf("\nAnswer:\n%s\n", decision.Content)
			return
		} else if decision.Type == "tool_call" {

			// 3. Execute Tool (Client Side)
			toolName := decision.ToolName
			toolArgs := decision.ToolArgs
			fmt.Printf("Agent requests: %s %v\n", toolName, toolArgs)

			var output string

			// --- Client-Side Tool Logic ---
			switch toolName {
			case "list_files":
				path, _ := toolArgs["path"].(string)
				if path == "" {
					path = "."
				}
				output = listFilesSafe(path)
			case "read_file":
				path, _ := toolArgs["path"].(string)
				output = readFileSafe(path)
			default:
				output = fmt.Sprintf("Error: Tool '%s' not found on client.", toolName)
			}
			preview := output
			if len(preview) > 100 {
				preview = preview[:100] + "..."
			}
			fmt.Printf("   -> Tool Output: %s\n", preview)

			// 4. Update History
			// Add the Assistant's "Call"
			history = append(history, datatypes.AgentMessage{
				Role: "assistant",
				ToolCalls: []datatypes.ToolCall{
					{
						Id: decision.ToolID,
						Function: datatypes.ToolFunction{
							Name: toolName,
							// Convert map back to JSON string for history consistency
							Arguments: mapToString(toolArgs),
						},
					},
				},
			})

			// Add the Tool's "Result"
			history = append(history, datatypes.AgentMessage{
				Role:       "tool",
				ToolCallId: decision.ToolID,
				Content:    output,
			})
		}
	}
	fmt.Println("Max steps reached. Stopping.")
}

func isPathAllowed(reqPath string) (bool, string) {
	// ---------------------------------------------------------
	// FIX: Handle Agent stripping leading slash on macOS temp paths
	// ---------------------------------------------------------
	if runtime.GOOS == "darwin" && strings.HasPrefix(reqPath, "var/folders") {
		reqPath = "/" + reqPath
	}

	// 1. Clean the path to resolve ".." and remove redundant slashes
	cleanPath := filepath.Clean(reqPath)

	// 2. Allow specific absolute paths (The Exception)
	// We allow /tmp but enforce that the cleaned path actually starts with /tmp
	if strings.HasPrefix(cleanPath, "/tmp") {
		return true, cleanPath
	}

	if runtime.GOOS == "darwin" && strings.HasPrefix(cleanPath, "/var/folders") {
		return true, cleanPath
	}

	// 3. Block all other absolute paths
	if filepath.IsAbs(cleanPath) || strings.HasPrefix(cleanPath, "/") {
		return false, ""
	}

	// 4. Block traversal (..) attempts for relative paths
	if strings.Contains(cleanPath, "..") {
		return false, ""
	}

	return true, cleanPath
}

func listFilesSafe(dirPath string) string {
	allowed, cleanPath := isPathAllowed(dirPath)
	if !allowed {
		return fmt.Sprintf("Error: Access Denied to '%s'. Security policy restricts scanning the root. Please read the specific target file mentioned in your instructions directly.", dirPath)
	}

	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		return fmt.Sprintf("Error reading dir: %v", err)
	}

	var files []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		files = append(files, e.Name())
	}

	result := strings.Join(files, "\n")
	if len(result) > 2000 {
		return result[:2000] + "\n...(truncated)"
	}
	return result
}

func readFileSafe(filePath string) string {
	allowed, cleanPath := isPathAllowed(filePath)
	if !allowed {
		return fmt.Sprintf("Error: Access Denied to '%s'. Only local paths, /tmp, or /var/folders (on Mac) are allowed. Check the path and try again.", filePath)
	}

	content, err := os.ReadFile(cleanPath)
	if err != nil {
		return fmt.Sprintf("Error reading file: %v", err)
	}

	return string(content)
}

func mapToString(m map[string]interface{}) string {
	b, _ := json.Marshal(m)
	return string(b)
}

func sendRAGRequest(question string, sessionId string, pipeline string) (RAGResponse, error) {
	var ragResp RAGResponse
	postBody, err := json.Marshal(map[string]interface{}{
		"query":      question,
		"session_id": sessionId,
		"pipeline":   pipeline,
		"no_rag":     noRag,
	})
	if err != nil {
		return ragResp, fmt.Errorf("failed to create request body: %w", err)
	}

	baseURL := getOrchestratorBaseURL()
	orchestratorURL := fmt.Sprintf("%s/v1/rag", baseURL)

	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Post(orchestratorURL, "application/json", bytes.NewBuffer(postBody))
	if err != nil {
		return ragResp, fmt.Errorf("failed to send question to orchestrator: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("Error: Orchestrator returned status %d. Response Body: %s", resp.StatusCode, string(bodyBytes))
		return ragResp, fmt.Errorf("orchestrator returned an error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	if err := json.Unmarshal(bodyBytes, &ragResp); err != nil {
		log.Printf("Raw response from orchestrator: %s", string(bodyBytes))
		return ragResp, fmt.Errorf("failed to parse response from orchestrator: %w", err)
	}
	return ragResp, nil
}

// monitorResources polls Podman for container stats every second
func monitorResources(stopChan chan bool, statsChan chan string) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			// Run podman stats
			cmd := exec.Command("podman", "stats", "--no-stream", "--format", "json")
			out, err := cmd.Output()
			if err != nil {
				statsChan <- "(Stats unavailable)"
				continue
			}

			var stats []PodmanStats
			if err := json.Unmarshal(out, &stats); err != nil {
				continue
			}

			var totalCPU float64
			var totalMem float64 // In MB

			for _, s := range stats {
				// Parse CPU: "5.30%" -> 5.30
				cpuStr := strings.TrimSuffix(s.CPUPerc, "%")
				if val, err := strconv.ParseFloat(cpuStr, 64); err == nil {
					totalCPU += val
				}

				// Parse Mem: "123MB / 16GB" -> 123
				parts := strings.Split(s.MemUsage, " / ")
				if len(parts) > 0 {
					memStr := parts[0]
					var mult float64 = 1
					if strings.Contains(memStr, "GB") {
						mult = 1024
						memStr = strings.TrimSuffix(memStr, "GB")
					} else if strings.Contains(memStr, "MB") {
						memStr = strings.TrimSuffix(memStr, "MB")
					} else if strings.Contains(memStr, "kB") {
						mult = 0.001
						memStr = strings.TrimSuffix(memStr, "kB")
					}
					if val, err := strconv.ParseFloat(strings.TrimSpace(memStr), 64); err == nil {
						totalMem += val * mult
					}
				}
			}

			// Format the string
			msg := fmt.Sprintf("CPU: %.1f%% | RAM: %.1f GB", totalCPU, totalMem/1024)

			// Try non-blocking send, skip if spinner isn't ready
			select {
			case statsChan <- msg:
			default:
			}
		}
	}
}

// showSpinner displays the animation + latest stats
func showSpinner(msg string, done chan bool, statsChan chan string) {
	//chars := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
	//chars := []string{"⚀", "⚁", "⚂", "⚃", "⚄", "⚅"}
	chars := []string{"▖", "▘", "▝", "▗"}
	i := 0
	currentStats := "Initializing metrics..."

	// Clear the cursor initially
	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h") // Restore cursor on exit

	for {
		select {
		case <-done:
			return
		case s := <-statsChan:
			currentStats = s
		default:
			// Overwrite the line
			// \r = return to start of line
			// \033[K = clear to end of line
			fmt.Printf("\r%s  %s... [%s] \033[K", chars[i%len(chars)], msg, currentStats)
			i++
			time.Sleep(100 * time.Millisecond)
		}
	}
}
