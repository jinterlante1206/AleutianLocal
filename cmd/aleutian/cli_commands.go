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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/jinterlante1206/AleutianLocal/cmd/aleutian/gcs"
	"github.com/jinterlante1206/AleutianLocal/services/orchestrator/datatypes"
	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
	"github.com/spf13/cobra"
)

type SourceInfo struct {
	Source   string  `json:"source"`
	Distance float64 `json:"distance,omitempty"`
	Score    float64 `json:"score,omitempty"`
}

type RAGResponse struct {
	Answer    string       `json:"answer"`
	SessionId string       `json:"session_id"`
	Sources   []SourceInfo `json:"sources,omitempty"`
}

type ConvertResponse struct {
	Status     string `json:"status"`
	Message    string `json:"message"`
	OutputPath string `json:"output_path"`
	Logs       string `json:"logs"`
}

type DirectChatRequest struct {
	Messages []datatypes.Message `json:"messages"`
}
type DirectChatResponse struct {
	Answer string `json:"answer"`
}

var (
	rootCmd = &cobra.Command{
		Use:   "aleutian",
		Short: "A CLI to manage the Aleutian Private AI Appliance",
		Long: `Aleutian is a tool for deploying and managing a complete, 
				offline AI stack on your own infrastructure.`,
	}
	askCmd = &cobra.Command{
		Use:   "ask [question]",
		Short: "Asks a question to the RAG system using the documents in the VectorDB",
		Long:  `Sends a question to the orchestrator, which uses Retrieval-Augmented Generation (RAG) to find relevant context from Weaviate and generate an answer with the VLLM.`,
		Args:  cobra.MinimumNArgs(1),
		Run:   runAskCommand,
	}
	pipelineType string
	noRag        bool

	populateCmd = &cobra.Command{
		Use:   "populate",
		Short: "Populate data stores with scanned and approved content.",
	}
	populateVectorDBCmd = &cobra.Command{
		Use:   "vectordb [file or directory path]",
		Short: "Scans local files/directories for secrets before populating the VectorDB",
		Long:  `Scans one or more files or directories for sensitive data`,
		Args:  cobra.MinimumNArgs(1),
		Run:   populateVectorDB,
	}

	// --- Utilities (convert to GGUF to start) ---
	convertCmd = &cobra.Command{
		Use:   "convert [model-id]",
		Short: "Converts a huggingface model to GGUF format",
		Long:  `Calls the converter service to download and convert a model to gguf`,
		Args:  cobra.ExactArgs(1),
		Run:   runConvertCommand,
	}
	quantizeType string
	isLocalPath  bool

	stackCmd = &cobra.Command{
		Use:   "stack",
		Short: "Manage the local Aleutian application on your machine",
		Long:  `Start, stop, and manage the local appliance using podman-compose.`,
	}
	deployCmd = &cobra.Command{
		Use:   "start",
		Short: "Start all local Aleutian services",
		Run:   runDeploy,
	}
	stopCmd = &cobra.Command{
		Use:   "stop",
		Short: "Stop all local Aleutian services",
		Run:   runStop,
	}
	destroyCmd = &cobra.Command{
		Use:   "destroy",
		Short: "DANGER: Stops and deletes all local containers AND data",
		Run:   runDestroy,
	}
	logsCmd = &cobra.Command{
		Use:   "logs [service_name]",
		Short: "Stream logs from a local service container (names are in podman ps -a)",
		Long:  `Runs 'podman-compose logs -f' for the specified service. If no service is specified, it will stream logs for all services.`,
		Run:   runLogsCommand,
	}

	// Session Administration commands
	sessionCmd = &cobra.Command{
		Use:   "session",
		Short: "Manage conversation sessions",
		Long:  `List, delete, or inspect conversation sessions stored in Weaviate.`,
	}
	listSessionsCmd = &cobra.Command{
		Use:   "list",
		Short: "List all conversation sessions",
		Run:   runListSessions,
	}
	deleteSessionCmd = &cobra.Command{
		Use:   "delete [session_id]",
		Short: "Delete a specific conversation session and its history",
		Args:  cobra.MinimumNArgs(1),
		Run:   runDeleteSession,
	}

	// Chat command
	chatCmd = &cobra.Command{
		Use:   "chat",
		Short: "Starts an interactive chat session",
		Long:  `Initiates a persistent, interactive chat session. A session ID is created and used for all subsequent messages to maintain conversation context.`,
		Run:   runChatCommand,
	}

	// Weaviate Administration commands
	weaviateCmd = &cobra.Command{
		Use:   "weaviate",
		Short: "Perform administrative tasks on the Weaviate vector database",
	}
	weaviateBackupCmd = &cobra.Command{
		Use:   "backup [backupID]",
		Short: "Create a new backup of the Weaviate database",
		Args:  cobra.ExactArgs(1),
		Run:   runWeaviateBackup,
	}
	weaviateRestoreCmd = &cobra.Command{
		Use:   "restore [backup-id]",
		Short: "Restore the Weaviate database from a backup",
		Args:  cobra.ExactArgs(1),
		Run:   runWeaviateRestore,
	}
	weaviateSummaryCmd = &cobra.Command{
		Use:   "summary",
		Short: "Get a summary of the Weaviate schema and data",
		Run:   runWeaviateSummary,
	}
	weaviateWipeoutCmd = &cobra.Command{
		Use:   "wipeout",
		Short: "DANGER: Deletes all data and schemas from Weaviate",
		Run:   runWeaviateWipeout,
	}

	// GCS data commands
	uploadCmd = &cobra.Command{
		Use:   "upload",
		Short: "Upload data to Google Cloud Storage (GCS)",
	}
	uploadLogsCmd = &cobra.Command{
		Use:   "logs [local_directory]",
		Short: "Uploads log files from a local directory to GCS",
		Args:  cobra.ExactArgs(1),
		Run:   runUploadLogs,
	}
	uploadBackupsCmd = &cobra.Command{
		Use:   "backups [local_directory]",
		Short: "Uploads Weaviate backups from a local directory to GCS",
		Args:  cobra.ExactArgs(1),
		Run:   runUploadBackups,
	}
)

// init() runs when the Go program starts
func init() {
	rootCmd.AddCommand(askCmd)
	askCmd.Flags().StringVarP(&pipelineType, "pipeline", "p", "reranking",
		"RAG pipeline to use(e.g., standard, reranking, raptor, graph, rig, semantic")
	askCmd.Flags().BoolVar(&noRag, "no-rag", false, "Skip the RAG pipeline and ask the LLM directly.")
	rootCmd.AddCommand(populateCmd)
	populateCmd.AddCommand(populateVectorDBCmd)

	// --- Local Commands ---
	rootCmd.AddCommand(stackCmd)
	stackCmd.AddCommand(deployCmd)
	stackCmd.AddCommand(stopCmd)
	stackCmd.AddCommand(destroyCmd)
	stackCmd.AddCommand(logsCmd)

	// --- Utility Commands ---
	rootCmd.AddCommand(convertCmd)
	convertCmd.Flags().StringVar(&quantizeType, "quantize", "q8_0", "Quantization type (f32, q8_0, bf16, f16)")
	convertCmd.Flags().BoolVar(&isLocalPath, "is-local-path", false, "Treat the model-id as a local path inside the container")
	convertCmd.Flags().Bool("register", false, "Register the model with ollama")

	// session commands
	rootCmd.AddCommand(sessionCmd)
	sessionCmd.AddCommand(listSessionsCmd)
	sessionCmd.AddCommand(deleteSessionCmd)

	// chat command
	rootCmd.AddCommand(chatCmd)
	chatCmd.Flags().String("resume", "", "Resume a conversation using a specific session ID.")

	// weaviate administration commands
	rootCmd.AddCommand(weaviateCmd)
	weaviateCmd.AddCommand(weaviateBackupCmd)
	weaviateCmd.AddCommand(weaviateRestoreCmd)
	weaviateCmd.AddCommand(weaviateSummaryCmd)
	weaviateCmd.AddCommand(weaviateWipeoutCmd)
	weaviateWipeoutCmd.Flags().Bool("force", false, "Required to confirm the deletion of all data.")

	// GCS data commands
	rootCmd.AddCommand(uploadCmd)
	uploadCmd.AddCommand(uploadLogsCmd)
	uploadCmd.AddCommand(uploadBackupsCmd)

}

func populateVectorDB(cmd *cobra.Command, args []string) {
	fmt.Println("Initializing the VectorDB population process")
	var allFiles []string
	var allFindings []policy_engine.ScanFinding
	// 1. Initialize the Policy Engine
	policyEngine, err := policy_engine.NewPolicyEngine(
		"internal/policy_engine/enforcement/data_classification_patterns.yaml")
	if err != nil {
		log.Fatalf("FATAL: Could not initialize the policy engine: %v", err)
	}
	// 2. Collect all files from the provided paths
	for _, path := range args {
		err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				allFiles = append(allFiles, p)
			}
			return nil
		})
		if err != nil {
			log.Printf("Error walking path %s: %v", path, err)
		}
	}

	// 3. Process each file individually for user review
	for _, file := range allFiles {
		fmt.Printf("\n🔍 Scanning file: %s\n", file)
		content, err := os.ReadFile(file)
		if err != nil {
			log.Printf("Could not read file %s: %v", file, err)
			continue
		}

		findings := policyEngine.ScanFileContent(string(content))

		// Get current user for logging
		currentUser, err := user.Current()
		reviewer := "unknown"
		if err == nil {
			reviewer = currentUser.Username
		}

		decision := "accepted" // Default decision if no findings
		proceed := true

		if len(findings) > 0 {
			fmt.Printf("Found %d potential issue(s) in '%s':\n", len(findings), file)
			fmt.Println("-------------------------------------------------")
			for _, f := range findings {
				fmt.Printf("  [L%d] %s Confidence | %s | %s\n", f.LineNumber, f.Confidence,
					f.ClassificationName, f.PatternId)
				fmt.Printf("    Reason: %s\n", f.PatternDescription)
				fmt.Printf("    Match:  '%s'\n\n", f.MatchedContent)
			}

			// 5. Per-file review and defaulting to "stop"
			fmt.Print("Do you want to proceed with this file? (yes/no): ")
			reader := bufio.NewReader(os.Stdin)
			input, _ := reader.ReadString('\n')
			input = strings.ToLower(strings.TrimSpace(input))

			if input != "yes" && input != "y" {
				decision = "rejected"
				proceed = false
				fmt.Println("Skipping file based on user decision.")
			} else {
				decision = "accepted"
				fmt.Println("Proceeding with file based on user decision.")
			}
		} else {
			fmt.Println("No issues found.")
		}

		// 4. Record the human's decision for each finding in the file
		for i := range findings {
			findings[i].FilePath = file
			findings[i].ReviewTimestamp = time.Now().UnixMilli()
			findings[i].UserDecision = decision
			findings[i].Reviewer = reviewer
		}
		allFindings = append(allFindings, findings...)

		if proceed {
			// Prepare the request body for the orchestrator
			postBody, err := json.Marshal(map[string]string{
				"source":  file,
				"content": string(content),
			})
			if err != nil {
				log.Printf("could not create the request for file %s: %v", file, err)
				continue
			}
			var host string
			if config.Target == "local" {
				host = "localhost"
			} else {
				host = config.ServerHost
			}
			// Send the request to the orchestrator
			orchestratorURL := fmt.Sprintf(
				"http://%s:%d/v1/documents",
				host,
				config.Services["orchestrator"].Port)
			resp, err := http.Post(orchestratorURL, "application/json", bytes.NewBuffer(postBody))
			if err != nil {
				log.Printf("Failed to send data for %s to the orchestrator: %v", file, err)
				continue
			}
			defer func(Body io.ReadCloser) {
				err := Body.Close()
				if err != nil {
					log.Println("Failed to close the orchestrator request")
				}
			}(resp.Body)
			if resp.StatusCode >= 400 {
				log.Printf("The orchestrator returned an error for %s, status %d\n", file,
					resp.StatusCode)
			} else {
				log.Printf("Successfully sent %s for population to the vectorDB\n", file)
			}

		}
	}

	// 6. Log all findings to a file
	if len(allFindings) > 0 {
		logFindingsToFile(allFindings)
	}
	fmt.Println("\n✨ Weaviate population process complete.")
}

// logFindingsToFile handles writing the final log.
func logFindingsToFile(findings []policy_engine.ScanFinding) {
	logFileName := fmt.Sprintf("scan_log_%s.json", time.Now().UTC().Format("20060102T150405Z"))

	file, err := json.MarshalIndent(findings, "", "  ")
	if err != nil {
		log.Printf("Could not marshal findings to JSON: %v", err)
		return
	}

	err = os.WriteFile(logFileName, file, 0644)
	if err != nil {
		log.Printf("Could not write log file %s: %v", logFileName, err)
		return
	}

	fmt.Printf("\nScan log with all decisions written to %s\n", logFileName)
}

func runListSessions(cmd *cobra.Command, args []string) {
	var host string
	if config.Target == "local" {
		host = "localhost"
	} else {
		host = config.ServerHost
	}
	orchestratorURL := fmt.Sprintf(
		"http://%s:%d/v1/sessions",
		host,
		config.Services["orchestrator"].Port,
	)

	resp, err := http.Get(orchestratorURL)
	if err != nil {
		log.Fatalf("Failed to connect to orchestrator: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Orchestrator returned an error: %s", resp.Status)
	}

	// The result from Weaviate is nested, so we decode it into a generic map
	var result map[string]map[string][]SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Fatalf("Failed to parse response from orchestrator: %v", err)
	}

	sessions := result["Get"]["Session"]
	if len(sessions) == 0 {
		fmt.Println("No active sessions found.")
		return
	}

	fmt.Println("Active Sessions:")
	fmt.Println("------------------------------------------------------------------")
	for _, s := range sessions {
		fmt.Printf("ID: %s\nSummary: %s\n\n", s.SessionId, s.Summary)
	}
}

func runDeleteSession(cmd *cobra.Command, args []string) {
	var host string
	if config.Target == "local" {
		host = "localhost"
	} else {
		host = config.ServerHost
	}
	sessionId := args[0]
	orchestratorURL := fmt.Sprintf(
		"http://%s:%d/v1/sessions/%s",
		host,
		config.Services["orchestrator"].Port,
		sessionId,
	)

	req, err := http.NewRequest(http.MethodDelete, orchestratorURL, nil)
	if err != nil {
		log.Fatalf("Failed to create delete request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Failed to send delete request to orchestrator: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Orchestrator returned an error: %s", resp.Status)
	}

	fmt.Printf("Successfully deleted session: %s\n", sessionId)
}

func sendRAGRequest(question string, sessionId string, pipeline string) (RAGResponse, error) {
	var host string
	if config.Target == "local" {
		host = "localhost"
	} else {
		host = config.ServerHost
	}
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

	orchestratorURL := fmt.Sprintf(
		"http://%s:%d/v1/rag",
		host,
		config.Services["orchestrator"].Port,
	)

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

func runAskCommand(cmd *cobra.Command, args []string) {
	question := strings.Join(args, " ")
	// Show pipeline being used
	fmt.Printf("Asking (using pipeline '%s'): %s\n", pipelineType, question)
	fmt.Println("---") // Separator
	// Pass the pipelineType flag value to sendRAGRequest
	ragResp, err := sendRAGRequest(question, "", pipelineType)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	// --- Enhanced Feedback: Answer + Sources ---
	fmt.Printf("\nAnswer:\n%s\n", ragResp.Answer) // Add newline for readability
	// Display sources if available
	if len(ragResp.Sources) > 0 {
		fmt.Println("\nSources Used:")
		for i, source := range ragResp.Sources {
			scoreInfo := ""
			if source.Distance != 0 { // Weaviate provides distance
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
	var host string
	if config.Target == "local" {
		host = "localhost"
	} else {
		host = config.ServerHost
	}
	orchestratorURL := fmt.Sprintf("http://%s:%d/v1/chat/direct", host,
		config.Services["orchestrator"].Port)
	messages := []datatypes.Message{
		{
			Role:    "system",
			Content: "You are a helpful, technically gifted assistant",
		},
	}
	// TODO: Add in session, --resume, and state here
	fmt.Println("starting a new chat session (no RAG). Type 'exit' or 'quit' to end")
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
		// ---- add the user's message to the history ----
		messages = append(messages, datatypes.Message{Role: "user", Content: input})
		reqBody := DirectChatRequest{Messages: messages}
		postBody, err := json.Marshal(reqBody)
		if err != nil {
			fmt.Printf("Error: failed to create the chat request: %v", err)
			continue
		}
		// --- send the request to the orchestrator ---
		client := &http.Client{Timeout: 3 * time.Minute}
		resp, err := client.Post(orchestratorURL, "application/json", bytes.NewBuffer(postBody))
		if err != nil {
			fmt.Printf("failed to send the request to the orchestrator: %v", err)
			continue
		}
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != http.StatusOK {
			fmt.Printf("error: Orchestrator returned status %d: %s\n", http.StatusOK,
				string(bodyBytes))
			messages = messages[:len(messages)-1] // remove the last failed message from the context
			continue
		}
		// ---- parse the response and add it to the chat history ----
		var chatResp DirectChatResponse
		if err := json.Unmarshal(bodyBytes, &chatResp); err != nil {
			fmt.Printf("Failed to parse the chat response: %v", err)
			messages = messages[:len(messages)-1]
			continue
		}
		messages = append(messages, datatypes.Message{Role: "assistant", Content: chatResp.Answer})
		fmt.Println(chatResp.Answer)
	}
}

//func runChatCommand(cmd *cobra.Command, args []string) {
//	var sessionId string
//	isFirstTurn := true
//	// Check if the --resume flag was used
//	resumeId, _ := cmd.Flags().GetString("resume")
//	if resumeId != "" {
//		sessionId = resumeId
//		isFirstTurn = false
//		fmt.Printf("Resuming session: %s\n", sessionId)
//	} else {
//		// Create a new session ID for this chat
//		fmt.Printf("Starting new chat session: %s\n", sessionId)
//	}
//	fmt.Println("Type 'exit' or 'quit' to end the session.")
//	reader := bufio.NewReader(os.Stdin)
//
//	for {
//		fmt.Print("> ")
//		input, _ := reader.ReadString('\n')
//		input = strings.TrimSpace(input)
//
//		if input == "exit" || input == "quit" {
//			fmt.Println("Ending chat session.")
//			break
//		}
//
//		if input == "" {
//			continue
//		}
//		currentSessionId := sessionId
//		if isFirstTurn {
//			currentSessionId = ""
//		}
//
//		ragResp, err := sendRAGRequest(input, currentSessionId, pipelineType)
//		if err != nil {
//			fmt.Printf("Error: %v\n", err)
//			continue
//		}
//		if isFirstTurn {
//			sessionId = ragResp.SessionId
//			isFirstTurn = false
//			fmt.Printf("(Session started: %s)\n", sessionId)
//		}
//
//		fmt.Println(ragResp.Answer)
//	}
//}

func runWeaviateWipeout(cmd *cobra.Command, args []string) {
	var host string
	if config.Target == "local" {
		host = "localhost"
	} else {
		host = config.ServerHost
	}
	// Check if the --force flag was provided.
	force, _ := cmd.Flags().GetBool("force")
	if !force {
		fmt.Println("Error: the --force flag is required to proceed with this destructive operation.")
		fmt.Println("Example: ./aleutian weaviate wipe --force")
		return
	}

	// The existing confirmation prompt provides a second layer of safety
	fmt.Println("DANGER: This will permanently delete all data and schemas from Weaviate.")
	fmt.Print("Are you sure you want to continue? (yes/no): ")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	if strings.TrimSpace(input) != "yes" {
		fmt.Println("Aborted.")
		return
	}

	fmt.Println("Proceeding with deletion...")
	orchestratorURL := fmt.Sprintf(
		"http://%s:%d/v1/weaviate/data",
		host,
		config.Services["orchestrator"].Port)
	req, _ := http.NewRequest(http.MethodDelete, orchestratorURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Failed to send wipe request: %v", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	fmt.Println("Orchestrator Response:", string(bodyBytes))
}

func runWeaviateBackup(cmd *cobra.Command, args []string) {
	var host string
	if config.Target == "local" {
		host = "localhost"
	} else {
		host = config.ServerHost
	}
	backupId := args[0]
	fmt.Printf("Starting Weaviate backup with ID: %s\n", backupId)
	postBody, _ := json.Marshal(map[string]string{"id": backupId, "action": "create"})
	orchestratorURL := fmt.Sprintf(
		"http://%s:%d/v1/weaviate/backups",
		host,
		config.Services["orchestrator"].Port)

	resp, err := http.Post(orchestratorURL, "application/json", bytes.NewBuffer(postBody))
	if err != nil {
		log.Fatalf("Failed to send backup request: %v", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	fmt.Println("Orchestrator Response:", string(bodyBytes))
}

func runWeaviateRestore(cmd *cobra.Command, args []string) {
	var host string
	if config.Target == "local" {
		host = "localhost"
	} else {
		host = config.ServerHost
	}
	backupId := args[0]
	fmt.Printf("Restoring Weaviate from backup ID: %s\n", backupId)
	postBody, _ := json.Marshal(map[string]string{"id": backupId, "action": "restore"})
	orchestratorURL := fmt.Sprintf("http://%s:%d/v1/weaviate/backups", host,
		config.Services["orchestrator"].Port)

	resp, err := http.Post(orchestratorURL, "application/json", bytes.NewBuffer(postBody))
	if err != nil {
		log.Fatalf("Failed to send restore request: %v", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	fmt.Println("Orchestrator Response:", string(bodyBytes))
}

func runWeaviateSummary(cmd *cobra.Command, args []string) {
	var host string
	if config.Target == "local" {
		host = "localhost"
	} else {
		host = config.ServerHost
	}
	fmt.Println("Fetching Weaviate summary...")
	orchestratorURL := fmt.Sprintf("http://%s:%d/v1/weaviate/summary", host,
		config.Services["orchestrator"].Port)
	resp, err := http.Get(orchestratorURL)
	if err != nil {
		log.Fatalf("Failed to send summary request: %v", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)

	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, bodyBytes, "", "  "); err != nil {
		log.Fatalf("Failed to format JSON: %v", err)
	}
	fmt.Println(prettyJSON.String())
}

func runUploadLogs(cmd *cobra.Command, args []string) {
	localDir := args[0]
	fmt.Printf("Preparing to upload logs from '%s' to GCS...\n", localDir)

	// Get GCS config from your global config object
	gcsConfig := config.Storage.GCS
	saKeyPath := "internal/ansible/secrets/gcp_keys/ansible_orchestrator_sa.json" // Path to your key

	ctx := context.Background()
	gcsClient, err := gcs.NewClient(ctx, config.Cloud.GCPProjectID, gcsConfig.BucketName, saKeyPath)
	if err != nil {
		log.Fatalf("Failed to create GCS client: %v", err)
	}

	fmt.Printf("Uploading contents of %s to gs://%s/%s\n", localDir, gcsClient.BucketName, gcsConfig.Logs.Code)
	if err := gcsClient.UploadDir(ctx, localDir, gcsConfig.Logs.Code); err != nil {
		log.Fatalf("Upload failed: %v", err)
	}

	fmt.Println("\nLog upload complete.")
}

func runUploadBackups(cmd *cobra.Command, args []string) {
	localDir := args[0]
	fmt.Printf("Preparing to upload backups from '%s' to GCS...\n", localDir)

	gcsConfig := config.Storage.GCS
	saKeyPath := "internal/ansible/secrets/gcp_keys/ansible_orchestrator_sa.json"

	ctx := context.Background()
	gcsClient, err := gcs.NewClient(ctx, config.Cloud.GCPProjectID, gcsConfig.BucketName, saKeyPath)
	if err != nil {
		log.Fatalf("Failed to create GCS client: %v", err)
	}

	gcsPath := filepath.Join(gcsConfig.Outputs.Code, "backups")
	fmt.Printf("Uploading contents of %s to gs://%s/%s\n", localDir, gcsClient.BucketName, gcsPath)
	if err := gcsClient.UploadDir(ctx, localDir, gcsPath); err != nil {
		log.Fatalf("Upload failed: %v", err)
	}

	fmt.Println("\nBackup upload complete.")
}

func runPodmanCompose(args ...string) {
	fmt.Printf("Executing: podman-compose %s\n", strings.Join(args, " "))
	cmd := exec.Command("podman-compose", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("podman-compose command failed: %v", err)
	}
}

func runDeploy(cmd *cobra.Command, args []string) {
	fmt.Println("Starting local Aleutian appliance")
	runPodmanCompose("up", "-d")
	fmt.Println("\nLocal Aleutian podman cluster started")
	fmt.Printf("Orchestrator is available at http://localhost:%d\n",
		config.Services["orchestrator"].Port)
}

func runStop(cmd *cobra.Command, args []string) {
	fmt.Println("Stopping the whole Aleutian system (all containers)")
	runPodmanCompose("down")
	fmt.Println("\nLocal appliance stopped")
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
	fmt.Println("Destroying local Aleutian instance and all associated data.")
	runPodmanCompose("down", "-v")
	fmt.Println("Local Aleutian instance and all associated data destroyed.")

}

func runConvertCommand(cmd *cobra.Command, args []string) {
	modelId := args[0]
	converterURL := "http://localhost:12140/convert"
	payload, _ := json.Marshal(map[string]interface{}{
		"model_id":      modelId,
		"quantize_type": quantizeType,
		"is_local_path": isLocalPath,
	})
	fmt.Printf("Sending the conversion request for %s (type: %s). This may take some time.\n",
		modelId, quantizeType)
	client := &http.Client{Timeout: 45 * time.Minute}
	resp, err := client.Post(converterURL, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		log.Fatalf("Failed to call the GGUF converter service: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("The GGUF Converter service failed and returned an error (%s)", string(body))
	}
	var convertResp ConvertResponse
	if err := json.NewDecoder(resp.Body).Decode(&convertResp); err != nil {
		log.Fatalf("Failed to parse response from converter: %v", err)
	}

	register, _ := cmd.Flags().GetBool("register")
	if register {
		fmt.Println("Registering the gguf model file with ollama")
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatalf("Failed to get current directory: %v", err)
		}
		hostGgufPath := filepath.Join(cwd, convertResp.OutputPath)
		if _, err := os.Stat(hostGgufPath); os.IsNotExist(err) {
			log.Fatalf("Could not find converted GGUF file on host at %s: %v", hostGgufPath, err)
		}
		modelFileContent := fmt.Sprintf("FROM %s", hostGgufPath)
		modelFileContent += fmt.Sprintf("\nPARAMETER stop %q", "</answer>") // Use %q for quoting
		modelFileContent += fmt.Sprintf("\nPARAMETER stop %q", "</s>")      // Common EOS token, good to include
		modelFileContent += fmt.Sprintf("\nPARAMETER stop %q", "Done")
		modelFileContent += fmt.Sprintf("\nPARAMETER stop %q", "End")
		modelFileContent += fmt.Sprintf("\nPARAMETER stop %q", "Response complete")

		osTmpFile, err := os.CreateTemp("", "Modelfile-*")
		if err != nil {
			log.Fatalf("Failed to create the temporary modelfile: %v", err)
		}
		defer osTmpFile.Close()
		defer os.Remove(osTmpFile.Name())
		_, err = osTmpFile.WriteString(modelFileContent)
		if err != nil {
			log.Fatalf("Failed to write to the tmpfile %v", err)
		}
		osTmpFile.Name()
		ollamaCreate := exec.Command("ollama", "create", modelId+"_local", "-f", osTmpFile.Name())
		ollamaCreate.Stdout = os.Stdout
		ollamaCreate.Stderr = os.Stderr
		if err = ollamaCreate.Run(); err != nil {
			log.Fatalf("Ollama failed to register your gguf model %s: %v", modelId, err)
		}
	}

	fmt.Printf("\n🎉 %s\n", convertResp.Message)
	fmt.Printf("   Output File: %s\n", convertResp.OutputPath)
	if register {
		fmt.Println("Registered the output file with Ollama")
	}
	fmt.Println("--- Conversion Logs ---")
	fmt.Println(convertResp.Logs)
	fmt.Println("-----------------------")
	fmt.Println("\nCheck the converter logs for full details: podman logs -f aleutian-gguf-converter")
}

func runLogsCommand(cmd *cobra.Command, args []string) {
	logArgs := []string{"logs", "-f"}
	if len(args) > 0 {
		logArgs = append(logArgs, args...)
		fmt.Printf("Streaming logs for %s\n", strings.Join(args, " "))
	} else {
		fmt.Println("Streaming the logs for all services")
	}
	fmt.Println("(press ctrl+c to exit)")
	runPodmanCompose(logArgs...)
	fmt.Println("\nLog streaming stopped.")
}

type SessionInfo struct {
	SessionId string `json:"session_id"`
	Summary   string `json:"summary"`
	Timestamp int64  `json:"timestamp"`
}
