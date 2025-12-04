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
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
	"github.com/spf13/cobra"
)

// Updated signature: Replaced 'Config' struct with string URL
func fileWorker(
	id int,
	wg *sync.WaitGroup,
	jobs <-chan string,
	orchestratorURL string,
	dataSpace string,
	versionTag string,
) {
	defer wg.Done()
	client := &http.Client{Timeout: 5 * time.Minute}

	for file := range jobs {
		fmt.Printf("[Worker %d] Ingesting: %s\n", id, file)
		content, err := os.ReadFile(file)
		if err != nil {
			log.Printf("[Worker %d] Could not read file %s: %v", id, file, err)
			continue
		}

		postBody, err := json.Marshal(map[string]string{
			"source":      file,
			"content":     string(content),
			"data_space":  dataSpace,
			"version_tag": versionTag,
		})
		if err != nil {
			log.Printf("[Worker %d] could not create request for file %s: %v", id, file, err)
			continue
		}

		resp, err := client.Post(orchestratorURL, "application/json", bytes.NewBuffer(postBody))
		if err != nil {
			log.Printf("[Worker %d] Failed to send data for %s to orchestrator: %v", id, file, err)
			continue
		}

		bodyBytes, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 400 {
			log.Printf("[Worker %d] Orchestrator error for %s, status %d: %s\n", id,
				file, resp.StatusCode, string(bodyBytes))
		} else {
			var ingestResp map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &ingestResp); err == nil {
				log.Printf("[Worker %d] Ingested %s (chunks: %.0f)\n", id,
					ingestResp["source"], ingestResp["chunks_processed"])
			} else {
				log.Printf("[Worker %d] Ingested %s (response unclear)\n", id, file)
			}
		}
		resp.Body.Close()
	}
}

type SessionInfo struct {
	SessionId string `json:"session_id"`
	Summary   string `json:"summary"`
	Timestamp int64  `json:"timestamp"`
}

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

func populateVectorDB(cmd *cobra.Command, args []string) {
	// FIX: Use helper instead of loading missing config file
	baseURL := getOrchestratorBaseURL()
	orchestratorURL := fmt.Sprintf("%s/v1/documents", baseURL)

	fmt.Println("Initializing the VectorDB population process... Finding all files...")
	var allFiles []string
	for _, path := range args {
		err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if blockedDirs[info.Name()] {
					log.Printf("Skipping blocked directory: %s\n", p)
					return filepath.SkipDir
				}
				return nil
			}
			if !info.IsDir() {
				ext := filepath.Ext(p)
				if !allowedFileExts[ext] {
					return nil
				}
				allFiles = append(allFiles, p)
			}
			return nil
		})
		if err != nil {
			log.Printf("Error walking path %s: %v", path, err)
		}
	}
	if len(allFiles) == 0 {
		fmt.Println("No valid files found to process.")
		return
	}
	fmt.Printf("Found %d files. Starting policy scan...\n", len(allFiles))

	policyEngine, err := policy_engine.NewPolicyEngine()
	if err != nil {
		log.Fatalf("FATAL: Could not initialize the policy engine: %v", err)
	}

	var approvedFiles []string
	var allFindings []policy_engine.ScanFinding
	reader := bufio.NewReader(os.Stdin)
	dataSpace, _ := cmd.Flags().GetString("data-space")
	versionTag, _ := cmd.Flags().GetString("version")

	for _, file := range allFiles {
		fmt.Printf("\nScanning file: %s\n", file)
		content, err := os.ReadFile(file)
		if err != nil {
			log.Printf("Could not read file %s: %v", file, err)
			continue
		}

		findings := policyEngine.ScanFileContent(string(content))

		currentUser, err := user.Current()
		reviewer := "John Doe"
		if err == nil {
			reviewer = currentUser.Username
		}
		decision := "accepted"
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
			fmt.Print("Do you want to proceed with this file? (yes/no): ")
			input, _ := reader.ReadString('\n')
			input = strings.ToLower(strings.TrimSpace(input))

			if input != "yes" && input != "y" {
				decision = "rejected"
				proceed = false
				fmt.Println("Skipping file based on user decision.")
			} else {
				decision = "accepted (user override)"
				fmt.Println("Proceeding with file based on user decision.")
			}
		} else {
			fmt.Println("No issues found.")
		}

		for i := range findings {
			findings[i].FilePath = file
			findings[i].ReviewTimestamp = time.Now().UnixMilli()
			findings[i].UserDecision = decision
			findings[i].Reviewer = reviewer
		}
		allFindings = append(allFindings, findings...)

		if proceed {
			approvedFiles = append(approvedFiles, file)
		}
	}

	if len(allFindings) > 0 {
		logFindingsToFile(allFindings)
	}

	if len(approvedFiles) == 0 {
		fmt.Println("\nNo files were approved for ingestion. Process complete.")
		return
	}

	fmt.Printf("\nScan complete. %d files approved. Starting parallel ingestion with 10 workers...\n", len(approvedFiles))
	numWorkers := 10
	var wg sync.WaitGroup
	jobs := make(chan string, len(approvedFiles))

	for w := 1; w <= numWorkers; w++ {
		wg.Add(1)
		// Updated to match new signature
		go fileWorker(w, &wg, jobs, orchestratorURL, dataSpace, versionTag)
	}

	for _, file := range approvedFiles {
		jobs <- file
	}
	close(jobs)

	wg.Wait()
	fmt.Println("\nWeaviate population process complete.")
}

func runDeleteSession(cmd *cobra.Command, args []string) {
	baseURL := getOrchestratorBaseURL()
	sessionId := args[0]
	orchestratorURL := fmt.Sprintf("%s/v1/sessions/%s", baseURL, sessionId)

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

func runListSessions(cmd *cobra.Command, args []string) {
	baseURL := getOrchestratorBaseURL()
	orchestratorURL := fmt.Sprintf("%s/v1/sessions", baseURL)

	resp, err := http.Get(orchestratorURL)
	if err != nil {
		log.Fatalf("Failed to connect to orchestrator: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Orchestrator returned an error: %s", resp.Status)
	}

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

func runWeaviateBackup(cmd *cobra.Command, args []string) {
	baseURL := getOrchestratorBaseURL()
	backupId := args[0]
	fmt.Printf("Starting Weaviate backup with ID: %s\n", backupId)
	postBody, _ := json.Marshal(map[string]string{"id": backupId, "action": "create"})
	orchestratorURL := fmt.Sprintf("%s/v1/weaviate/backups", baseURL)

	resp, err := http.Post(orchestratorURL, "application/json", bytes.NewBuffer(postBody))
	if err != nil {
		log.Fatalf("Failed to send backup request: %v", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	fmt.Println("Orchestrator Response:", string(bodyBytes))
}

func runWeaviateDeleteDoc(cmd *cobra.Command, args []string) {
	baseURL := getOrchestratorBaseURL()
	sourceName := args[0]
	fmt.Printf("Submitting request to delete all chunks for: %s\n", sourceName)

	encodedSourceName := url.QueryEscape(sourceName)
	orchestratorURL := fmt.Sprintf("%s/v1/document?source=%s", baseURL, encodedSourceName)
	req, err := http.NewRequest(http.MethodDelete, orchestratorURL, nil)
	if err != nil {
		log.Fatalf("failed to create the delete request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Failed to send delete request to orchestrator: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Orchestrator returned an error: (Status %d) %s", resp.StatusCode, string(bodyBytes))
	}
	var deleteResp map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &deleteResp); err != nil {
		log.Fatalf("Failed to parse success response from orchestrator: %v", err)
	}
	fmt.Printf("\nSuccess: %s\n", deleteResp["status"])
	fmt.Printf("Source Deleted: %s\n", deleteResp["source_deleted"])
	fmt.Printf("Chunks Removed: %.0f\n", deleteResp["chunks_deleted"])
}

func runWeaviateRestore(cmd *cobra.Command, args []string) {
	baseURL := getOrchestratorBaseURL()
	backupId := args[0]
	fmt.Printf("Restoring Weaviate from backup ID: %s\n", backupId)
	postBody, _ := json.Marshal(map[string]string{"id": backupId, "action": "restore"})
	orchestratorURL := fmt.Sprintf("%s/v1/weaviate/backups", baseURL)

	resp, err := http.Post(orchestratorURL, "application/json", bytes.NewBuffer(postBody))
	if err != nil {
		log.Fatalf("Failed to send restore request: %v", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	fmt.Println("Orchestrator Response:", string(bodyBytes))
}

func runWeaviateSummary(cmd *cobra.Command, args []string) {
	baseURL := getOrchestratorBaseURL()
	fmt.Println("Fetching Weaviate summary...")
	orchestratorURL := fmt.Sprintf("%s/v1/weaviate/summary", baseURL)
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

func runWeaviateWipeout(cmd *cobra.Command, args []string) {
	baseURL := getOrchestratorBaseURL()
	force, _ := cmd.Flags().GetBool("force")
	if !force {
		fmt.Println("Error: the --force flag is required to proceed with this destructive operation.")
		fmt.Println("Example: ./aleutian weaviate wipe --force")
		return
	}

	fmt.Println("DANGER: This will permanently delete all data and schemas from Weaviate.")
	fmt.Print("Are you sure you want to continue? (yes/no): ")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	if strings.TrimSpace(input) != "yes" {
		fmt.Println("Aborted.")
		return
	}

	fmt.Println("Proceeding with deletion...")
	orchestratorURL := fmt.Sprintf("%s/v1/weaviate/data", baseURL)
	req, _ := http.NewRequest(http.MethodDelete, orchestratorURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Failed to send wipe request: %v", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	fmt.Println("Orchestrator Response:", string(bodyBytes))
}

func runUploadLogs(cmd *cobra.Command, args []string) {
	// Temporarily disabled until GCS config is migrated to aleutian.yaml
	fmt.Println("GCS Uploads are temporarily disabled in v0.3.0 pending config migration.")
	// NOTE: The previous code was removed to allow compilation because Config.Cloud is gone.
}

func runUploadBackups(cmd *cobra.Command, args []string) {
	fmt.Println("GCS Uploads are temporarily disabled in v0.3.0 pending config migration.")
}
