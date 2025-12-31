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
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jinterlante1206/AleutianLocal/pkg/ux"
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

// scanResult holds the result of scanning a single file
type scanResult struct {
	FilePath string
	Content  []byte
	Findings []policy_engine.ScanFinding
	Error    error
}

// ingestResult tracks the outcome of ingesting a file
type ingestResult struct {
	FilePath string
	Chunks   int
	Error    error
}

func populateVectorDB(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	baseURL := getOrchestratorBaseURL()
	orchestratorURL := fmt.Sprintf("%s/v1/documents", baseURL)

	dataSpace, _ := cmd.Flags().GetString("data-space")
	versionTag, _ := cmd.Flags().GetString("version")
	force, _ := cmd.Flags().GetBool("force")

	// Phase 1: Discover files with spinner
	ux.Title("Aleutian Ingest")
	spin := ux.NewSpinner("Scanning for files...")
	spin.Start()

	var allFiles []string
	for _, path := range args {
		err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if blockedDirs[info.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			ext := filepath.Ext(p)
			if allowedFileExts[ext] {
				allFiles = append(allFiles, p)
			}
			return nil
		})
		if err != nil {
			ux.Warning(fmt.Sprintf("Error walking %s: %v", path, err))
		}
	}

	if len(allFiles) == 0 {
		spin.StopWithWarning("No valid files found")
		return
	}
	spin.StopWithSuccess(fmt.Sprintf("Found %d files", len(allFiles)))

	// Phase 2: Parallel policy scan
	policyEngine, err := policy_engine.NewPolicyEngine()
	if err != nil {
		ux.Error(fmt.Sprintf("Could not initialize policy engine: %v", err))
		return
	}

	ux.Info("Scanning for secrets and policy violations...")

	// Parallel scanning with worker pool
	numScanners := runtime.NumCPU()
	fileChan := make(chan string, len(allFiles))
	resultChan := make(chan scanResult, len(allFiles))

	// Start scanner workers
	var scanWg sync.WaitGroup
	for i := 0; i < numScanners; i++ {
		scanWg.Add(1)
		go func() {
			defer scanWg.Done()
			for filePath := range fileChan {
				content, err := os.ReadFile(filePath)
				if err != nil {
					resultChan <- scanResult{FilePath: filePath, Error: err}
					continue
				}
				findings := policyEngine.ScanFileContent(string(content))
				resultChan <- scanResult{
					FilePath: filePath,
					Content:  content,
					Findings: findings,
				}
			}
		}()
	}

	// Feed files to scanners
	go func() {
		for _, f := range allFiles {
			fileChan <- f
		}
		close(fileChan)
	}()

	// Collect results in background
	go func() {
		scanWg.Wait()
		close(resultChan)
	}()

	// Process scan results
	var approvedFiles []string
	var approvedContents [][]byte
	var allFindings []policy_engine.ScanFinding
	var filesWithSecrets []scanResult
	var cleanFiles []scanResult

	// Collect all results first
	var scannedCount int32
	progressDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-progressDone:
				return
			case <-ticker.C:
				current := atomic.LoadInt32(&scannedCount)
				if ux.ShouldShowProgress() {
					fmt.Printf("\r  Scanned %d/%d files...", current, len(allFiles))
				}
			}
		}
	}()

	for result := range resultChan {
		atomic.AddInt32(&scannedCount, 1)
		if result.Error != nil {
			ux.Warning(fmt.Sprintf("Could not read %s: %v", result.FilePath, result.Error))
			continue
		}
		if len(result.Findings) > 0 {
			filesWithSecrets = append(filesWithSecrets, result)
		} else {
			cleanFiles = append(cleanFiles, result)
		}
	}
	close(progressDone)
	fmt.Print("\r\033[K") // Clear progress line

	// Clean files are auto-approved
	for _, f := range cleanFiles {
		approvedFiles = append(approvedFiles, f.FilePath)
		approvedContents = append(approvedContents, f.Content)
		ux.FileStatus(f.FilePath, ux.IconSuccess, "")
	}

	// Handle files with secrets via bidirectional prompts
	if len(filesWithSecrets) > 0 {
		currentUser, _ := user.Current()
		reviewer := "unknown"
		if currentUser != nil {
			reviewer = currentUser.Username
		}

		if force {
			// Force mode: approve all with warning
			ux.Warning(fmt.Sprintf("Force mode: approving %d files with detected secrets", len(filesWithSecrets)))
			for _, f := range filesWithSecrets {
				approvedFiles = append(approvedFiles, f.FilePath)
				approvedContents = append(approvedContents, f.Content)
				for i := range f.Findings {
					f.Findings[i].FilePath = f.FilePath
					f.Findings[i].ReviewTimestamp = time.Now().UnixMilli()
					f.Findings[i].UserDecision = "accepted (forced)"
					f.Findings[i].Reviewer = reviewer
				}
				allFindings = append(allFindings, f.Findings...)
			}
		} else {
			// Interactive mode: ask about each file
			for _, f := range filesWithSecrets {
				// Convert findings to UX format
				uxFindings := make([]ux.SecretFinding, len(f.Findings))
				for i, finding := range f.Findings {
					uxFindings[i] = ux.SecretFinding{
						LineNumber:  finding.LineNumber,
						PatternID:   finding.PatternId,
						PatternName: finding.ClassificationName,
						Confidence:  string(finding.Confidence),
						Match:       finding.MatchedContent,
						Reason:      finding.PatternDescription,
					}
				}

				action, err := ux.AskSecretAction(ux.SecretPromptOptions{
					FilePath: f.FilePath,
					Findings: uxFindings,
				})
				if err != nil {
					ux.Warning(fmt.Sprintf("Prompt error, skipping %s", f.FilePath))
					action = ux.SecretActionSkip
				}

				decision := "rejected"
				switch action {
				case ux.SecretActionProceed:
					decision = "accepted (user override)"
					approvedFiles = append(approvedFiles, f.FilePath)
					approvedContents = append(approvedContents, f.Content)
					ux.FileStatus(f.FilePath, ux.IconWarning, "approved with secrets")
				case ux.SecretActionSkip:
					decision = "rejected"
					ux.FileStatus(f.FilePath, ux.IconError, "skipped")
				}

				for i := range f.Findings {
					f.Findings[i].FilePath = f.FilePath
					f.Findings[i].ReviewTimestamp = time.Now().UnixMilli()
					f.Findings[i].UserDecision = decision
					f.Findings[i].Reviewer = reviewer
				}
				allFindings = append(allFindings, f.Findings...)
			}
		}
	}

	// Log findings
	if len(allFindings) > 0 {
		logFindingsToFile(allFindings)
	}

	skipped := len(allFiles) - len(approvedFiles)
	ux.Summary(len(approvedFiles), skipped, len(allFiles))

	if len(approvedFiles) == 0 {
		ux.Warning("No files approved for ingestion")
		return
	}

	// Phase 3: Parallel ingestion
	ux.Info(fmt.Sprintf("Ingesting %d files...", len(approvedFiles)))

	numWorkers := 10
	jobChan := make(chan int, len(approvedFiles))
	ingestResultChan := make(chan ingestResult, len(approvedFiles))

	// Start ingestion workers
	var ingestWg sync.WaitGroup
	client := &http.Client{Timeout: 5 * time.Minute}

	for w := 0; w < numWorkers; w++ {
		ingestWg.Add(1)
		go func() {
			defer ingestWg.Done()
			for idx := range jobChan {
				select {
				case <-ctx.Done():
					return
				default:
				}

				filePath := approvedFiles[idx]
				content := approvedContents[idx]

				postBody, _ := json.Marshal(map[string]string{
					"source":      filePath,
					"content":     string(content),
					"data_space":  dataSpace,
					"version_tag": versionTag,
				})

				resp, err := client.Post(orchestratorURL, "application/json", bytes.NewBuffer(postBody))
				if err != nil {
					ingestResultChan <- ingestResult{FilePath: filePath, Error: err}
					continue
				}

				bodyBytes, _ := io.ReadAll(resp.Body)
				resp.Body.Close()

				if resp.StatusCode >= 400 {
					ingestResultChan <- ingestResult{
						FilePath: filePath,
						Error:    fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes)),
					}
					continue
				}

				var ingestResp map[string]interface{}
				chunks := 0
				if err := json.Unmarshal(bodyBytes, &ingestResp); err == nil {
					if c, ok := ingestResp["chunks_processed"].(float64); ok {
						chunks = int(c)
					}
				}
				ingestResultChan <- ingestResult{FilePath: filePath, Chunks: chunks}
			}
		}()
	}

	// Feed jobs
	go func() {
		for i := range approvedFiles {
			jobChan <- i
		}
		close(jobChan)
	}()

	// Collect ingestion results with progress
	go func() {
		ingestWg.Wait()
		close(ingestResultChan)
	}()

	var ingestedCount, totalChunks int
	var errors []string
	progressSpin := ux.NewProgressSpinner("Ingesting", len(approvedFiles))
	progressSpin.Start()

	for result := range ingestResultChan {
		ingestedCount++
		progressSpin.SetProgress(ingestedCount)
		if result.Error != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", result.FilePath, result.Error))
		} else {
			totalChunks += result.Chunks
		}
	}

	progressSpin.Stop()

	// Final summary
	if len(errors) > 0 {
		ux.Warning(fmt.Sprintf("%d files failed to ingest", len(errors)))
		for _, e := range errors {
			ux.Info("  " + e)
		}
	}

	ux.Success(fmt.Sprintf("Ingestion complete: %d files → %d chunks", ingestedCount-len(errors), totalChunks))
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
