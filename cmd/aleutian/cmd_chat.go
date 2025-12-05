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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
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
	Answer    string       `json:"answer"`
	SessionId string       `json:"session_id"`
	Sources   []SourceInfo `json:"sources,omitempty"`
}

type SourceInfo struct {
	Source   string  `json:"source"`
	Distance float64 `json:"distance,omitempty"`
	Score    float64 `json:"score,omitempty"`
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
	orchestratorURL := fmt.Sprintf("%s/v1/chat/direct", baseURL)

	resumeID, _ := cmd.Flags().GetString("resume")
	messages := []datatypes.Message{}

	if resumeID != "" {
		fmt.Printf("Resuming chat session: %s\n", resumeID)
		historyURL := fmt.Sprintf("%s/v1/sessions/%s/history", baseURL, resumeID)

		resp, err := http.Get(historyURL)
		if err != nil || resp.StatusCode != http.StatusOK {
			log.Fatalf("Failed to get history for session %s: %v", resumeID, err)
		}

		type HistoryTurn struct {
			Question string `json:"question"`
			Answer   string `json:"answer"`
		}
		var historyResp map[string]map[string][]HistoryTurn
		if err := json.NewDecoder(resp.Body).Decode(&historyResp); err != nil {
			resp.Body.Close()
			log.Fatalf("Failed to parse session history: %v", err)
		}
		resp.Body.Close()

		history, ok := historyResp["Get"]["Conversation"]
		if !ok {
			log.Fatalf("No 'Conversation' data found in history response.")
		}
		for _, turn := range history {
			messages = append(messages, datatypes.Message{Role: "user", Content: turn.Question})
			messages = append(messages, datatypes.Message{Role: "assistant", Content: turn.Answer})
		}
		fmt.Printf("Loaded %d previous turns. You can start chatting.\n", len(history))

	} else {
		fmt.Println("Starting a new chat session (no RAG). Type 'exit' or 'quit' to end.")
		messages = append(messages, datatypes.Message{
			Role:    "system",
			Content: "You are a helpful, technically gifted assistant",
		})
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "exit" || input == "quit" {
			fmt.Println("ending chat")
			break
		}
		if input == "" {
			continue
		}

		messages = append(messages, datatypes.Message{Role: "user", Content: input})
		reqBody := DirectChatRequest{
			Messages:       messages,
			EnableThinking: enableThinking,
			BudgetTokens:   budgetTokens,
		}
		postBody, err := json.Marshal(reqBody)
		if err != nil {
			fmt.Printf("Error: failed to create the chat request: %v", err)
			continue
		}

		client := &http.Client{Timeout: 3 * time.Minute}
		// Use the simple spinner for normal chat, reserve the heavy monitor for Trace
		done := make(chan bool)
		statsChan := make(chan string)
		go showSpinner("Thinking", done, statsChan)

		resp, err := client.Post(orchestratorURL, "application/json", bytes.NewBuffer(postBody))
		done <- true
		if err != nil {
			fmt.Printf("failed to send the request to the orchestrator: %v", err)
			if len(messages) > 0 {
				messages = messages[:len(messages)-1]
			}
			continue
		}
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != http.StatusOK {
			fmt.Printf("error: Orchestrator returned status %d: %s\n", resp.StatusCode, string(bodyBytes))
			if len(messages) > 0 {
				messages = messages[:len(messages)-1]
			}
			continue
		}

		var chatResp DirectChatResponse
		if err := json.Unmarshal(bodyBytes, &chatResp); err != nil {
			fmt.Printf("Failed to parse the chat response: %v", err)
			if len(messages) > 0 {
				messages = messages[:len(messages)-1]
			}
			continue
		}
		if chatResp.Answer == "" {
			fmt.Println("Error: Received empty response from Orchestrator.")
			if len(messages) > 0 {
				messages = messages[:len(messages)-1]
			}
			continue
		}
		messages = append(messages, datatypes.Message{Role: "assistant", Content: chatResp.Answer})
		fmt.Println(chatResp.Answer)
	}
}

func runTraceCommand(cmd *cobra.Command, args []string) {
	query := strings.Join(args, " ")
	fmt.Printf("Agent analyzing codebase for: %s\n", query)
	fmt.Println("(This may take a moment while the agent reads files...)")

	baseURL := getOrchestratorBaseURL()
	url := fmt.Sprintf("%s/v1/agent/trace", baseURL)
	payload, _ := json.Marshal(map[string]string{"query": query})
	stopMonitor := make(chan bool)
	statsChan := make(chan string)
	stopSpinner := make(chan bool)
	go monitorResources(stopMonitor, statsChan)
	go showSpinner("Agent is working", stopSpinner, statsChan)
	client := &http.Client{Timeout: 10 * time.Minute} // Long timeout for agents
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(payload))

	// Cleanup UI
	stopMonitor <- true
	stopSpinner <- true
	fmt.Print("\r                                                                            \r") // Clean line

	if err != nil {
		log.Fatalf("\nConnection Failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("\nError (Status %d):\n%s\n", resp.StatusCode, string(body))
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		log.Fatalf("\nFailed to parse JSON response: %s", string(body))
	}

	if ans, ok := result["answer"].(string); ok {
		fmt.Printf("\nAnswer:\n%s\n", ans)
		if strings.Contains(ans, "sk_live_") {
			fmt.Println("\nPrivacy alert: The agent tried to display a secret key.")
			fmt.Println("   [Content Redacted by Aleutian Policy Engine]")
			return
		}
	}

	if steps, ok := result["steps"].([]interface{}); ok {
		fmt.Println("\nSteps Taken:")
		for _, s := range steps {
			step := s.(map[string]interface{})
			fmt.Printf("- Called %s(%s)\n", step["tool"], step["args"])
		}
	}
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
