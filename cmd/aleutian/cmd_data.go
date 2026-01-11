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
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/jinterlante1206/AleutianLocal/pkg/ux"
	"github.com/jinterlante1206/AleutianLocal/services/policy_engine"
	"github.com/spf13/cobra"
)

// SessionInfo represents metadata about a chat session.
//
// # Description
//
// SessionInfo is returned by the session list endpoint and contains
// summary information about active or historical chat sessions.
type SessionInfo struct {
	SessionId string `json:"session_id"`
	Summary   string `json:"summary"`
	Timestamp int64  `json:"timestamp"`
}

// logFindingsToFile writes policy scan findings to a timestamped JSON file.
//
// # Description
//
// Creates a JSON file with all policy violations and user decisions for
// compliance record-keeping. The filename includes a UTC timestamp for
// easy chronological ordering.
//
// # Inputs
//
//   - findings: Policy scan results with user decisions attached
//
// # Outputs
//
// None (writes file and prints confirmation to stdout)
//
// # Limitations
//
//   - Writes to current working directory
//   - Silently logs errors and continues (non-fatal)
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

// scanResult holds the result of scanning a single file for policy violations.
// Includes file content for subsequent ingestion if approved.
type scanResult struct {
	FilePath string
	Content  []byte
	Findings []policy_engine.ScanFinding
	Error    error
}

// ingestResult tracks the outcome of ingesting a single file to the vector database.
// Includes chunk count on success or error details on failure.
type ingestResult struct {
	FilePath string
	Chunks   int
	Error    error
}

// IngestAuditContext captures identity and metadata for audit logging.
//
// # Description
//
// Every IngestPipeline operation logs an audit event with this context.
// This enables compliance reporting (GDPR, HIPAA, CCPA) and incident
// investigation by tracking who initiated ingestion, when, and from where.
//
// # Fields
//
//   - Caller: Identity of the user or service initiating ingestion
//   - CallerType: Origin type - "cli" for command line, "api" for HTTP API,
//     "automated" for scheduled jobs or pipelines
//   - SessionID: Unique identifier (UUID) for this ingestion operation
//   - Timestamp: When the operation started (UTC)
//   - SourceIP: For API calls, the originating IP address (empty for CLI)
//
// # Examples
//
//	// CLI usage
//	ctx := IngestAuditContext{
//	    Caller:     "jin",
//	    CallerType: "cli",
//	    SessionID:  uuid.New().String(),
//	    Timestamp:  time.Now().UTC(),
//	}
//
//	// API usage
//	ctx := IngestAuditContext{
//	    Caller:     userID,
//	    CallerType: "api",
//	    SessionID:  requestID,
//	    Timestamp:  time.Now().UTC(),
//	    SourceIP:   clientIP,
//	}
//
// # Limitations
//
//   - Caller identity is not cryptographically verified in CLI mode
//   - SourceIP may be empty or a proxy address for API calls
type IngestAuditContext struct {
	Caller     string    `json:"caller"`
	CallerType string    `json:"caller_type"`
	SessionID  string    `json:"session_id"`
	Timestamp  time.Time `json:"timestamp"`
	SourceIP   string    `json:"source_ip,omitempty"`
}

// IngestConfig holds configuration for the ingestion pipeline.
//
// # Description
//
// IngestConfig contains all settings needed to run an ingestion operation.
// It is passed to NewIngestPipeline to configure the pipeline behavior.
//
// # Fields
//
//   - OrchestratorURL: Base URL of the orchestrator service (e.g., "http://localhost:8080")
//   - DataSpace: Logical namespace for documents (e.g., "default", "project-alpha")
//   - VersionTag: Version label for this ingestion batch (e.g., "v1.0", "2024-01-15")
//   - ForceMode: If true, bypasses interactive approval for files with secrets
//
// # Examples
//
//	config := IngestConfig{
//	    OrchestratorURL: "http://localhost:8080",
//	    DataSpace:       "codebase",
//	    VersionTag:      "v2.1.0",
//	    ForceMode:       false,
//	}
//
// # Limitations
//
//   - ForceMode should only be used in automated pipelines with pre-approved content
//   - DataSpace and VersionTag are not validated against any registry
type IngestConfig struct {
	OrchestratorURL string
	DataSpace       string
	VersionTag      string
	ForceMode       bool
}

// IngestPipelineVersion is the current version of the ingestion pipeline.
// This is logged in audit events for compliance provenance tracking.
const IngestPipelineVersion = "1.0.0"

// IngestPipeline coordinates document ingestion with privacy audit logging.
//
// # Description
//
// IngestPipeline orchestrates file discovery, policy scanning, user approval,
// and parallel ingestion into the vector database. All operations are logged
// with audit context for compliance (GDPR, HIPAA, CCPA).
//
// The pipeline follows the "Watchtower" philosophy: observe and log all
// decisions without blocking, enabling post-hoc compliance review.
//
// # Workflow
//
//  1. discoverFiles() - Find files matching allowed extensions
//  2. scanFilesForSecrets() - Parallel policy engine scanning
//  3. promptForApproval() - User decisions on flagged files
//  4. ingestFiles() - Parallel upload to orchestrator
//
// # Programmatic Usage
//
//	auditCtx := IngestAuditContext{
//	    Caller:     "data-sync-service",
//	    CallerType: "automated",
//	    SessionID:  uuid.New().String(),
//	    Timestamp:  time.Now().UTC(),
//	}
//
//	config := IngestConfig{
//	    OrchestratorURL: "http://localhost:8080",
//	    DataSpace:       "codebase",
//	    VersionTag:      "v1.0",
//	    ForceMode:       true, // Automated pipeline, pre-approved content
//	}
//
//	pipeline, err := NewIngestPipeline(config, auditCtx)
//	if err != nil {
//	    return err
//	}
//
//	// Use pipeline methods...
//
// # Thread Safety
//
// The pipeline is NOT safe for concurrent use. Create one pipeline per
// ingestion operation.
//
// # Audit Trail
//
// All operations emit structured log events via slog:
//   - ingest.started: Pipeline initialization with version
//   - ingest.discovery.complete: Files found
//   - ingest.scan.complete: Policy scan results
//   - ingest.approval.decision: Per-file user decisions (critical for compliance)
//   - ingest.file.success/error: Ingestion outcomes
//   - ingest.complete: Final summary
//
// # Version
//
// The pipeline version (IngestPipelineVersion) is included in all audit
// events to enable compliance log schema tracking across releases.
type IngestPipeline struct {
	config          IngestConfig
	auditCtx        IngestAuditContext
	policyEngine    *policy_engine.PolicyEngine
	httpClient      *http.Client
	pipelineVersion string
}

// NewIngestPipeline creates an ingestion pipeline with audit context.
//
// # Description
//
// Initializes a new IngestPipeline with the provided configuration and
// audit context. The pipeline is ready to use immediately after creation.
// The current IngestPipelineVersion is automatically set.
//
// # Inputs
//
//   - config: Ingestion configuration (URLs, data space, version, force mode)
//   - auditCtx: Caller identity and session info for audit logging
//
// # Outputs
//
//   - *IngestPipeline: Configured pipeline ready to run
//   - error: If policy engine initialization fails
//
// # Examples
//
//	pipeline, err := NewIngestPipeline(config, auditCtx)
//	if err != nil {
//	    return fmt.Errorf("pipeline init failed: %w", err)
//	}
//
// # Limitations
//
//   - HTTP client uses a fixed 5-minute timeout per request
//   - Policy engine is initialized fresh each time (no caching)
//
// # Assumptions
//
//   - OrchestratorURL is reachable
//   - Policy engine patterns are valid (will error if not)
func NewIngestPipeline(config IngestConfig, auditCtx IngestAuditContext) (*IngestPipeline, error) {
	policyEngine, err := policy_engine.NewPolicyEngine()
	if err != nil {
		return nil, fmt.Errorf("policy engine initialization failed: %w", err)
	}

	return &IngestPipeline{
		config:          config,
		auditCtx:        auditCtx,
		policyEngine:    policyEngine,
		httpClient:      &http.Client{Timeout: 5 * time.Minute},
		pipelineVersion: IngestPipelineVersion,
	}, nil
}

// discoverFiles walks the given paths and returns files matching allowed extensions.
//
// # Description
//
// Recursively walks each path, filtering by allowed file extensions and
// skipping blocked directories (node_modules, .git, vendor, etc.).
// Emits an audit log event upon completion.
//
// # Inputs
//
//   - paths: Directories or individual files to scan
//
// # Outputs
//
//   - []string: Absolute paths to discovered files
//   - error: Only returned for critical failures (currently logs warnings and continues)
//
// # Examples
//
//	files, err := pipeline.discoverFiles([]string{"./src", "./docs"})
//	if err != nil {
//	    return fmt.Errorf("discovery failed: %w", err)
//	}
//	fmt.Printf("Found %d files\n", len(files))
//
// # Limitations
//
//   - Does not follow symlinks (filepath.Walk behavior)
//   - Silently skips unreadable directories with a warning
//   - File list is not sorted
//
// # Assumptions
//
//   - allowedFileExts and blockedDirs maps are initialized
//   - Paths provided are valid filesystem paths
func (p *IngestPipeline) discoverFiles(paths []string) ([]string, error) {
	var discoveredFiles []string

	for _, path := range paths {
		err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if blockedDirs[info.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			ext := filepath.Ext(filePath)
			if allowedFileExts[ext] {
				discoveredFiles = append(discoveredFiles, filePath)
			}
			return nil
		})
		if err != nil {
			// Log warning but continue with other paths
			slog.Warn("ingest.discovery.path_error",
				"session_id", p.auditCtx.SessionID,
				"path", path,
				"error", err.Error(),
			)
		}
	}

	// Audit log: discovery complete
	slog.Info("ingest.discovery.complete",
		"session_id", p.auditCtx.SessionID,
		"caller", p.auditCtx.Caller,
		"caller_type", p.auditCtx.CallerType,
		"pipeline_version", p.pipelineVersion,
		"paths_scanned", paths,
		"file_count", len(discoveredFiles),
	)

	return discoveredFiles, nil
}

// scanFilesForSecrets scans files in parallel for policy violations.
//
// # Description
//
// Uses a worker pool (runtime.NumCPU() workers) to scan files against the
// policy engine. Returns scan results categorized as clean or containing
// findings. Emits an audit log event upon completion.
//
// # Inputs
//
//   - ctx: Context for cancellation (respected by workers)
//   - files: Paths to files to scan
//
// # Outputs
//
//   - cleanFiles: Files with no policy violations (includes content)
//   - flaggedFiles: Files containing secrets/PII (includes content and findings)
//   - error: Currently always nil (errors logged per-file as warnings)
//
// # Examples
//
//	clean, flagged, err := pipeline.scanFilesForSecrets(ctx, files)
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("Clean: %d, Flagged: %d\n", len(clean), len(flagged))
//
// # Limitations
//
//   - Reads entire file into memory (not suitable for very large files)
//   - Worker count is fixed to runtime.NumCPU()
//   - Unreadable files are logged and skipped, not returned as errors
//
// # Assumptions
//
//   - Policy engine is initialized and valid
//   - Files exist and are readable (best-effort for unreadable)
func (p *IngestPipeline) scanFilesForSecrets(ctx context.Context, files []string) (
	cleanFiles []scanResult, flaggedFiles []scanResult, err error) {

	numScanners := runtime.NumCPU()
	fileChan := make(chan string, len(files))
	resultChan := make(chan scanResult, len(files))

	// Start scanner workers
	var scanWg sync.WaitGroup
	for i := 0; i < numScanners; i++ {
		scanWg.Add(1)
		go func() {
			defer scanWg.Done()
			for filePath := range fileChan {
				select {
				case <-ctx.Done():
					return
				default:
				}

				content, readErr := os.ReadFile(filePath)
				if readErr != nil {
					resultChan <- scanResult{FilePath: filePath, Error: readErr}
					continue
				}
				findings := p.policyEngine.ScanFileContent(string(content))
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
		for _, f := range files {
			select {
			case <-ctx.Done():
				close(fileChan)
				return
			case fileChan <- f:
			}
		}
		close(fileChan)
	}()

	// Collect results in background
	go func() {
		scanWg.Wait()
		close(resultChan)
	}()

	// Categorize results
	var readErrors int
	for result := range resultChan {
		if result.Error != nil {
			readErrors++
			slog.Warn("ingest.scan.file_error",
				"session_id", p.auditCtx.SessionID,
				"file_path", result.FilePath,
				"error", result.Error.Error(),
			)
			continue
		}
		if len(result.Findings) > 0 {
			flaggedFiles = append(flaggedFiles, result)
		} else {
			cleanFiles = append(cleanFiles, result)
		}
	}

	// Audit log: scan complete
	slog.Info("ingest.scan.complete",
		"session_id", p.auditCtx.SessionID,
		"caller", p.auditCtx.Caller,
		"pipeline_version", p.pipelineVersion,
		"files_scanned", len(files),
		"clean_count", len(cleanFiles),
		"flagged_count", len(flaggedFiles),
		"read_errors", readErrors,
	)

	return cleanFiles, flaggedFiles, nil
}

// promptForApproval handles user decisions for files with detected secrets.
//
// # Description
//
// Processes clean and flagged files to determine which should be ingested.
// Clean files are auto-approved. Flagged files are handled based on ForceMode:
//   - ForceMode=true: Auto-approve all flagged files with "accepted (forced)" decision
//   - ForceMode=false: Interactive prompt for each flagged file
//
// All decisions are logged for compliance audit trail.
//
// # Inputs
//
//   - cleanFiles: Files with no policy violations (auto-approved)
//   - flaggedFiles: Files containing secrets/PII requiring decision
//
// # Outputs
//
//   - approved: Files approved for ingestion (includes content)
//   - findings: All findings with user decisions recorded for audit log
//
// # Examples
//
//	approved, findings := pipeline.promptForApproval(clean, flagged)
//	if len(findings) > 0 {
//	    logFindingsToFile(findings)
//	}
//
// # Limitations
//
//   - Interactive mode requires TTY (will fail with EOF in non-interactive)
//   - Prompt errors result in file being skipped (fail-safe)
//
// # Assumptions
//
//   - p.config.ForceMode indicates whether to bypass prompts
//   - p.auditCtx.Caller is used as the reviewer identity
func (p *IngestPipeline) promptForApproval(
	cleanFiles []scanResult, flaggedFiles []scanResult,
) (approved []scanResult, findings []policy_engine.ScanFinding) {

	// Auto-approve clean files
	for _, f := range cleanFiles {
		approved = append(approved, f)

		// Audit log: clean file auto-approved
		slog.Debug("ingest.approval.decision",
			"session_id", p.auditCtx.SessionID,
			"file_path", f.FilePath,
			"decision", "approved (clean)",
			"reviewer", "system",
			"findings_count", 0,
		)
	}

	if len(flaggedFiles) == 0 {
		return approved, findings
	}

	// Use audit context caller as reviewer
	reviewer := p.auditCtx.Caller

	if p.config.ForceMode {
		// Force mode: approve all flagged files with warning
		for _, f := range flaggedFiles {
			approved = append(approved, f)

			// Record findings with forced decision
			for i := range f.Findings {
				f.Findings[i].FilePath = f.FilePath
				f.Findings[i].ReviewTimestamp = time.Now().UnixMilli()
				f.Findings[i].UserDecision = "accepted (forced)"
				f.Findings[i].Reviewer = reviewer
			}
			findings = append(findings, f.Findings...)

			// Audit log: forced approval (critical for compliance)
			findingTypes := make([]string, len(f.Findings))
			for i, finding := range f.Findings {
				findingTypes[i] = finding.PatternId
			}
			slog.Warn("ingest.approval.decision",
				"session_id", p.auditCtx.SessionID,
				"file_path", f.FilePath,
				"decision", "approved (forced)",
				"reviewer", reviewer,
				"findings_count", len(f.Findings),
				"finding_types", findingTypes,
				"pipeline_version", p.pipelineVersion,
			)
		}
		return approved, findings
	}

	// Interactive mode: prompt for each flagged file
	for _, f := range flaggedFiles {
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
			// Fail-safe: skip file on prompt error
			action = ux.SecretActionSkip
			slog.Warn("ingest.approval.prompt_error",
				"session_id", p.auditCtx.SessionID,
				"file_path", f.FilePath,
				"error", err.Error(),
			)
		}

		decision := "rejected"
		switch action {
		case ux.SecretActionProceed:
			decision = "accepted (user override)"
			approved = append(approved, f)
		case ux.SecretActionSkip:
			decision = "rejected"
		}

		// Record findings with user decision
		for i := range f.Findings {
			f.Findings[i].FilePath = f.FilePath
			f.Findings[i].ReviewTimestamp = time.Now().UnixMilli()
			f.Findings[i].UserDecision = decision
			f.Findings[i].Reviewer = reviewer
		}
		findings = append(findings, f.Findings...)

		// Audit log: user decision (critical for compliance)
		findingTypes := make([]string, len(f.Findings))
		for i, finding := range f.Findings {
			findingTypes[i] = finding.PatternId
		}
		slog.Info("ingest.approval.decision",
			"session_id", p.auditCtx.SessionID,
			"file_path", f.FilePath,
			"decision", decision,
			"reviewer", reviewer,
			"findings_count", len(f.Findings),
			"finding_types", findingTypes,
			"pipeline_version", p.pipelineVersion,
		)
	}

	return approved, findings
}

// ingestFiles sends approved files to the orchestrator in parallel.
//
// # Description
//
// Uses a worker pool (10 workers by default, capped at file count) to POST
// files to the /v1/documents endpoint. Each file is sent with its content,
// data space, and version tag. Emits audit log events for each file and
// a summary upon completion.
//
// # Inputs
//
//   - ctx: Context for cancellation (respected by workers and HTTP requests)
//   - files: Approved files with content to ingest
//
// # Outputs
//
//   - ingested: Count of successfully ingested files
//   - chunks: Total chunks created across all files
//   - errors: List of error messages for failed files
//
// # Examples
//
//	ingested, chunks, errors := pipeline.ingestFiles(ctx, approved)
//	if len(errors) > 0 {
//	    fmt.Printf("Failures: %v\n", errors)
//	}
//	fmt.Printf("Ingested %d files, %d chunks\n", ingested, chunks)
//
// # Limitations
//
//   - Fixed 10 worker limit (or file count, whichever is smaller)
//   - 5 minute timeout per file (from pipeline's HTTP client)
//   - Failed files do not retry
//
// # Assumptions
//
//   - Orchestrator is running and reachable at config.OrchestratorURL
//   - Files have valid content (already scanned and approved)
func (p *IngestPipeline) ingestFiles(ctx context.Context, files []scanResult) (
	ingested int, chunks int, errors []string) {

	if len(files) == 0 {
		return 0, 0, nil
	}

	orchestratorURL := fmt.Sprintf("%s/v1/documents", p.config.OrchestratorURL)

	// Smart worker count: min(10, file count)
	numWorkers := 10
	if len(files) < numWorkers {
		numWorkers = len(files)
	}

	jobChan := make(chan int, len(files))
	resultChan := make(chan ingestResult, len(files))

	// Start ingestion workers
	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobChan {
				select {
				case <-ctx.Done():
					return
				default:
				}

				file := files[idx]
				postBody, marshalErr := json.Marshal(map[string]string{
					"source":      file.FilePath,
					"content":     string(file.Content),
					"data_space":  p.config.DataSpace,
					"version_tag": p.config.VersionTag,
				})
				if marshalErr != nil {
					resultChan <- ingestResult{
						FilePath: file.FilePath,
						Error:    fmt.Errorf("marshal error: %w", marshalErr),
					}
					continue
				}

				req, reqErr := http.NewRequestWithContext(ctx, "POST", orchestratorURL, bytes.NewBuffer(postBody))
				if reqErr != nil {
					resultChan <- ingestResult{
						FilePath: file.FilePath,
						Error:    fmt.Errorf("request creation error: %w", reqErr),
					}
					continue
				}
				req.Header.Set("Content-Type", "application/json")

				resp, doErr := p.httpClient.Do(req)
				if doErr != nil {
					resultChan <- ingestResult{FilePath: file.FilePath, Error: doErr}
					continue
				}

				bodyBytes, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if readErr != nil {
					resultChan <- ingestResult{
						FilePath: file.FilePath,
						Error:    fmt.Errorf("read response error: %w", readErr),
					}
					continue
				}

				if resp.StatusCode >= 400 {
					resultChan <- ingestResult{
						FilePath: file.FilePath,
						Error:    fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes)),
					}
					continue
				}

				var ingestResp map[string]interface{}
				fileChunks := 0
				if jsonErr := json.Unmarshal(bodyBytes, &ingestResp); jsonErr == nil {
					if c, ok := ingestResp["chunks_processed"].(float64); ok {
						fileChunks = int(c)
					}
				}
				resultChan <- ingestResult{FilePath: file.FilePath, Chunks: fileChunks}
			}
		}()
	}

	// Feed jobs
	go func() {
		for i := range files {
			select {
			case <-ctx.Done():
				close(jobChan)
				return
			case jobChan <- i:
			}
		}
		close(jobChan)
	}()

	// Collect results in background
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Process results
	for result := range resultChan {
		if result.Error != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", result.FilePath, result.Error))

			// Audit log: file error
			slog.Error("ingest.file.error",
				"session_id", p.auditCtx.SessionID,
				"file_path", result.FilePath,
				"error", result.Error.Error(),
			)
		} else {
			ingested++
			chunks += result.Chunks

			// Audit log: file success
			slog.Info("ingest.file.success",
				"session_id", p.auditCtx.SessionID,
				"file_path", result.FilePath,
				"chunks", result.Chunks,
			)
		}
	}

	// Audit log: ingestion complete
	slog.Info("ingest.complete",
		"session_id", p.auditCtx.SessionID,
		"caller", p.auditCtx.Caller,
		"caller_type", p.auditCtx.CallerType,
		"pipeline_version", p.pipelineVersion,
		"data_space", p.config.DataSpace,
		"version_tag", p.config.VersionTag,
		"files_ingested", ingested,
		"total_chunks", chunks,
		"files_failed", len(errors),
	)

	return ingested, chunks, errors
}

// populateVectorDB orchestrates the complete document ingestion workflow.
//
// # Description
//
// Main entry point for the 'aleutian data populate' command. Coordinates
// file discovery, policy scanning, user approval, and parallel ingestion
// using the IngestPipeline. All operations are logged with audit context
// for compliance (GDPR, HIPAA, CCPA).
//
// # Workflow
//
//  1. Parse command flags (data-space, version, force)
//  2. Create audit context with current user identity
//  3. Discover files matching allowed extensions
//  4. Scan files for secrets/PII in parallel
//  5. Prompt user for approval of flagged files (or auto-approve in force mode)
//  6. Ingest approved files in parallel
//  7. Display summary
//
// # Inputs
//
//   - cmd: Cobra command with flags (data-space, version, force)
//   - args: Directory paths to ingest
//
// # Outputs
//
// None (displays results to stdout, logs to slog)
//
// # Limitations
//
//   - Requires orchestrator to be running
//   - Interactive prompts require TTY (use --force for non-interactive)
//
// # Assumptions
//
//   - getOrchestratorBaseURL() returns a valid URL
//   - User has read access to the specified paths
func populateVectorDB(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Parse flags
	dataSpace, err := cmd.Flags().GetString("data-space")
	if err != nil {
		ux.Error(fmt.Sprintf("Failed to read 'data-space' flag: %v", err))
		return
	}
	versionTag, err := cmd.Flags().GetString("version")
	if err != nil {
		ux.Error(fmt.Sprintf("Failed to read 'version' flag: %v", err))
		return
	}
	force, err := cmd.Flags().GetBool("force")
	if err != nil {
		ux.Error(fmt.Sprintf("Failed to read 'force' flag: %v", err))
		return
	}

	// Build configuration
	config := IngestConfig{
		OrchestratorURL: getOrchestratorBaseURL(),
		DataSpace:       dataSpace,
		VersionTag:      versionTag,
		ForceMode:       force,
	}

	// Build audit context with current user
	currentUser, _ := user.Current()
	caller := "unknown"
	if currentUser != nil {
		caller = currentUser.Username
	}

	auditCtx := IngestAuditContext{
		Caller:     caller,
		CallerType: "cli",
		SessionID:  fmt.Sprintf("ingest-%d", time.Now().UnixNano()),
		Timestamp:  time.Now().UTC(),
	}

	// Create pipeline
	pipeline, err := NewIngestPipeline(config, auditCtx)
	if err != nil {
		ux.Error(fmt.Sprintf("Failed to initialize pipeline: %v", err))
		return
	}

	// Log pipeline start
	slog.Info("ingest.started",
		"session_id", auditCtx.SessionID,
		"caller", auditCtx.Caller,
		"caller_type", auditCtx.CallerType,
		"pipeline_version", pipeline.pipelineVersion,
		"data_space", config.DataSpace,
		"version_tag", config.VersionTag,
		"force_mode", config.ForceMode,
		"paths", args,
	)

	// Phase 1: Discover files
	ux.Title("Aleutian Ingest")
	spin := ux.NewSpinner("Scanning for files...")
	spin.Start()

	files, err := pipeline.discoverFiles(args)
	if err != nil {
		spin.StopWithWarning("Discovery failed")
		ux.Error(err.Error())
		return
	}
	if len(files) == 0 {
		spin.StopWithWarning("No valid files found")
		return
	}
	spin.StopWithSuccess(fmt.Sprintf("Found %d files", len(files)))

	// Phase 2: Scan for secrets
	ux.Info("Scanning for secrets and policy violations...")
	cleanFiles, flaggedFiles, err := pipeline.scanFilesForSecrets(ctx, files)
	if err != nil {
		ux.Error(fmt.Sprintf("Scan failed: %v", err))
		return
	}

	// Display clean files
	for _, f := range cleanFiles {
		ux.FileStatus(f.FilePath, ux.IconSuccess, "")
	}

	// Phase 3: Approval
	if len(flaggedFiles) > 0 && config.ForceMode {
		ux.Warning(fmt.Sprintf("Force mode: approving %d files with detected secrets", len(flaggedFiles)))
	}

	approved, findings := pipeline.promptForApproval(cleanFiles, flaggedFiles)

	// Display flagged file decisions (UX feedback)
	for _, f := range flaggedFiles {
		// Check if this file was approved
		wasApproved := false
		for _, a := range approved {
			if a.FilePath == f.FilePath {
				wasApproved = true
				break
			}
		}
		if wasApproved {
			ux.FileStatus(f.FilePath, ux.IconWarning, "approved with secrets")
		} else {
			ux.FileStatus(f.FilePath, ux.IconError, "skipped")
		}
	}

	// Log findings to file
	if len(findings) > 0 {
		logFindingsToFile(findings)
	}

	// Summary before ingestion
	skipped := len(files) - len(approved)
	ux.Summary(len(approved), skipped, len(files))

	if len(approved) == 0 {
		ux.Warning("No files approved for ingestion")
		return
	}

	// Phase 4: Ingest
	ux.Info(fmt.Sprintf("Ingesting %d files...", len(approved)))
	progressSpin := ux.NewProgressSpinner("Ingesting", len(approved))
	progressSpin.Start()

	ingested, totalChunks, errors := pipeline.ingestFiles(ctx, approved)

	progressSpin.Stop()

	// Final summary
	if len(errors) > 0 {
		ux.Warning(fmt.Sprintf("%d files failed to ingest", len(errors)))
		for _, e := range errors {
			ux.Info("  " + e)
		}
	}

	ux.Success(fmt.Sprintf("Ingestion complete: %d files → %d chunks", ingested, totalChunks))
}

// runDeleteSession deletes a chat session from the orchestrator.
//
// # Inputs
//
//   - args[0]: Session ID to delete
//
// # Limitations
//
//   - Requires orchestrator to be running
//   - Exits fatally on error
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

// VerifySessionResponse represents the verification result from the orchestrator.
//
// # Description
//
// Contains the result of hash chain verification for a session.
// Mirrors the API response from POST /v1/sessions/:sessionId/verify.
//
// # Fields
//
//   - SessionID: The session that was verified
//   - Verified: Whether the integrity check passed
//   - TurnCount: Number of conversation turns verified
//   - ChainHash: Hash of all turn content combined
//   - VerifiedAt: Timestamp when verification was performed
//   - TurnHashes: Hash of each individual Q&A turn
//   - ErrorDetails: If verification failed, details about the failure
type VerifySessionResponse struct {
	SessionID    string         `json:"session_id"`
	Verified     bool           `json:"verified"`
	TurnCount    int            `json:"turn_count"`
	ChainHash    string         `json:"chain_hash,omitempty"`
	VerifiedAt   int64          `json:"verified_at"`
	TurnHashes   map[int]string `json:"turn_hashes,omitempty"`
	ErrorDetails string         `json:"error_details,omitempty"`
}

// runVerifySession verifies the integrity of a session's hash chain.
//
// # Description
//
// Calls the orchestrator's verify endpoint to check that a session's
// conversation history has not been tampered with. The verification
// checks the cryptographic hash chain linking each turn.
//
// # Inputs
//
//   - args[0]: Session ID to verify
//
// # Flags
//
//   - --full: Perform full verification (recompute all hashes from content)
//   - --json: Output result as JSON for scripting
//
// # Outputs
//
// Prints verification result to stdout. Exit code 0 if verified,
// exit code 1 if verification failed or tampered.
//
// # Examples
//
//	aleutian session verify sess-abc123
//	aleutian session verify sess-abc123 --full
//	aleutian session verify sess-abc123 --json
//
// # Limitations
//
//   - Requires orchestrator to be running
//   - Only verifies data currently in Weaviate
func runVerifySession(cmd *cobra.Command, args []string) {
	baseURL := getOrchestratorBaseURL()
	sessionID := args[0]

	fullVerify, _ := cmd.Flags().GetBool("full")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	// Build URL with optional query params
	orchestratorURL := fmt.Sprintf("%s/v1/sessions/%s/verify", baseURL, sessionID)
	if fullVerify {
		orchestratorURL += "?mode=full"
	}

	// Make POST request
	req, err := http.NewRequest(http.MethodPost, orchestratorURL, nil)
	if err != nil {
		if jsonOutput {
			fmt.Printf(`{"error": "failed to create request: %s"}`, err.Error())
		} else {
			log.Fatalf("Failed to create verify request: %v", err)
		}
		os.Exit(1)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if jsonOutput {
			fmt.Printf(`{"error": "failed to connect to orchestrator: %s"}`, err.Error())
		} else {
			log.Fatalf("Failed to connect to orchestrator: %v", err)
		}
		os.Exit(1)
	}
	defer resp.Body.Close()

	// Parse response
	var result VerifySessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		if jsonOutput {
			fmt.Printf(`{"error": "failed to parse response: %s"}`, err.Error())
		} else {
			log.Fatalf("Failed to parse verification response: %v", err)
		}
		os.Exit(1)
	}

	// Handle JSON output mode
	if jsonOutput {
		output, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(output))
		if !result.Verified {
			os.Exit(1)
		}
		return
	}

	// Human-readable output using UX personality
	personality := ux.GetPersonality().Level

	if result.Verified {
		switch personality {
		case ux.PersonalityFull:
			if fullVerify {
				// Full Phase 13 design output
				printVerifyFullOutput(result, sessionID, baseURL)
			} else {
				// Basic verification output
				fmt.Println()
				fmt.Println(ux.Styles.Success.Render("╔══════════════════════════════════════════════════════════════╗"))
				fmt.Println(ux.Styles.Success.Render("║           INTEGRITY VERIFICATION SUCCESSFUL                  ║"))
				fmt.Println(ux.Styles.Success.Render("╚══════════════════════════════════════════════════════════════╝"))
				fmt.Println()
				fmt.Printf("  Session:    %s\n", result.SessionID)
				fmt.Printf("  Status:     %s\n", ux.Styles.Success.Render("✓ VERIFIED"))
				fmt.Printf("  Turns:      %d conversation turns verified\n", result.TurnCount)
				if result.ChainHash != "" {
					fmt.Printf("  Chain Hash: %s...%s\n", result.ChainHash[:8], result.ChainHash[len(result.ChainHash)-4:])
				}
				fmt.Printf("  Verified:   %s\n", time.UnixMilli(result.VerifiedAt).Format(time.RFC3339))
				fmt.Println()
				fmt.Println(ux.Styles.Muted.Render("  The hash chain is intact. No tampering detected."))
				fmt.Println(ux.Styles.Muted.Render("  Use --full for detailed output with Weaviate queries."))
				fmt.Println()
			}

		case ux.PersonalityStandard:
			fmt.Printf("✓ Session %s verified (%d turns)\n", result.SessionID, result.TurnCount)
			if result.ChainHash != "" {
				fmt.Printf("  Chain: %s...%s\n", result.ChainHash[:8], result.ChainHash[len(result.ChainHash)-4:])
			}

		case ux.PersonalityMinimal:
			fmt.Printf("VERIFIED: %s (%d turns)\n", result.SessionID, result.TurnCount)

		case ux.PersonalityMachine:
			fmt.Printf("verified=true session_id=%s turn_count=%d chain_hash=%s\n",
				result.SessionID, result.TurnCount, result.ChainHash)
		}
	} else {
		switch personality {
		case ux.PersonalityFull:
			fmt.Println()
			fmt.Println(ux.Styles.Error.Render("╔══════════════════════════════════════════════════════════════╗"))
			fmt.Println(ux.Styles.Error.Render("║           INTEGRITY VERIFICATION FAILED                      ║"))
			fmt.Println(ux.Styles.Error.Render("╚══════════════════════════════════════════════════════════════╝"))
			fmt.Println()
			fmt.Printf("  Session:    %s\n", result.SessionID)
			fmt.Printf("  Status:     %s\n", ux.Styles.Error.Render("✗ FAILED"))
			if result.ErrorDetails != "" {
				fmt.Printf("  Error:      %s\n", ux.Styles.Error.Render(result.ErrorDetails))
			}
			fmt.Println()
			fmt.Println(ux.Styles.Warning.Render("  ⚠ WARNING: The hash chain may have been tampered with."))
			fmt.Println(ux.Styles.Warning.Render("  Please investigate this session's history."))
			fmt.Println()

		case ux.PersonalityStandard:
			fmt.Printf("✗ Session %s FAILED verification\n", result.SessionID)
			if result.ErrorDetails != "" {
				fmt.Printf("  Error: %s\n", result.ErrorDetails)
			}

		case ux.PersonalityMinimal:
			fmt.Printf("FAILED: %s\n", result.SessionID)

		case ux.PersonalityMachine:
			fmt.Printf("verified=false session_id=%s error=%s\n",
				result.SessionID, result.ErrorDetails)
		}
		os.Exit(1)
	}
}

// printVerifyFullOutput displays the full Phase 13 design output for session verification.
//
// # Description
//
// Shows comprehensive verification details including:
//   - Integrity & Hash Chain section with turn hashes
//   - Query Your Data section with REST API and Weaviate GraphQL examples
//   - Weaviate Storage section (counts)
//   - Logs & Debugging section
//
// # Inputs
//
//   - result: The verification response from orchestrator
//   - sessionID: The session ID being verified
//   - baseURL: The orchestrator base URL for building curl commands
//
// # Outputs
//
// Prints formatted output to stdout.
func printVerifyFullOutput(result VerifySessionResponse, sessionID, baseURL string) {
	divider := "═══════════════════════════════════════════════════════════════"
	sectionDivider := "───────────────────────────────────────────────────────────────"

	fmt.Println()
	fmt.Println(divider)
	fmt.Println("INTEGRITY & HASH CHAIN")
	fmt.Println(divider)
	fmt.Println()
	fmt.Printf("  %s  Chain Verification:      %s (all %d turns verified)\n",
		ux.Styles.Success.Render("🔐"),
		ux.Styles.Success.Render("✓ PASSED"),
		result.TurnCount)
	fmt.Printf("  🔗  Chain Length:            %d events\n", result.TurnCount)
	fmt.Println()

	// Final Chain Hash
	fmt.Println("  Final Chain Hash:")
	if result.ChainHash != "" {
		fmt.Printf("    %s\n", result.ChainHash)
	} else {
		fmt.Println("    (not available)")
	}
	fmt.Println()

	// Turn Hashes
	if len(result.TurnHashes) > 0 {
		fmt.Println("  Turn Hashes:")
		for i := 1; i <= len(result.TurnHashes); i++ {
			if hash, ok := result.TurnHashes[i]; ok {
				fmt.Printf("    Turn %d (Q&A):  %s\n", i, hash)
			}
		}
		fmt.Println()
	}

	// Verify command
	fmt.Println("  Verify command:")
	fmt.Printf("    curl -X POST %s/v1/sessions/%s/verify\n", baseURL, sessionID)
	fmt.Println()

	fmt.Println(sectionDivider)
	fmt.Println("QUERY YOUR DATA")
	fmt.Println(sectionDivider)
	fmt.Println()

	// REST API
	fmt.Println("  REST API:")
	fmt.Printf("    curl %s/v1/sessions/%s\n", baseURL, sessionID)
	fmt.Println()

	// Weaviate GraphQL Console
	fmt.Println("  Weaviate GraphQL Console:")
	fmt.Println("    http://localhost:8081/v1/graphql")
	fmt.Println()

	// Session Query
	fmt.Println("  Session Query:")
	fmt.Println("    {")
	fmt.Println("      Get {")
	fmt.Println("        Session(where: {path: [\"session_id\"], operator: Equal, valueString: \"" + sessionID + "\"}) {")
	fmt.Println("          session_id")
	fmt.Println("          created_at")
	fmt.Println("          conversation_count")
	fmt.Println("        }")
	fmt.Println("      }")
	fmt.Println("    }")
	fmt.Println()

	fmt.Println(sectionDivider)
	fmt.Println("WEAVIATE STORAGE")
	fmt.Println(sectionDivider)
	fmt.Println()
	fmt.Printf("  📊  Session Records:         1\n")
	fmt.Printf("  💬  Conversation Turns:      %d\n", result.TurnCount)
	fmt.Printf("  📄  Document Chunks:         (query Weaviate for count)\n")
	fmt.Println()

	fmt.Println(sectionDivider)
	fmt.Println("LOGS & DEBUGGING")
	fmt.Println(sectionDivider)
	fmt.Println()
	fmt.Println("  Find logs:")
	fmt.Println("    docker logs aleutian-orchestrator-1 2>&1 | grep \"" + sessionID + "\"")
	fmt.Println()

	fmt.Println(divider)
	fmt.Println()
}

// runListSessions lists all active chat sessions from the orchestrator.
//
// # Outputs
//
// Prints session IDs and summaries to stdout.
//
// # Limitations
//
//   - Requires orchestrator to be running
//   - Exits fatally on error
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

// runWeaviateBackup creates a backup of the Weaviate vector database.
//
// # Inputs
//
//   - args[0]: Backup ID to use for the backup
//
// # Limitations
//
//   - Requires orchestrator to be running
//   - Backup is stored in orchestrator's configured backup location
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

// runWeaviateDeleteDoc deletes all chunks for a document from Weaviate.
//
// # Inputs
//
//   - args[0]: Source name (file path) of the document to delete
//
// # Outputs
//
// Prints deletion status, source name, and chunk count removed.
//
// # Limitations
//
//   - Requires orchestrator to be running
//   - Exits fatally on error
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

// runWeaviateRestore restores the Weaviate database from a backup.
//
// # Inputs
//
//   - args[0]: Backup ID to restore from
//
// # Limitations
//
//   - Requires orchestrator to be running
//   - Backup must exist in configured backup location
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

// runWeaviateSummary displays statistics about the Weaviate vector database.
//
// # Outputs
//
// Prints JSON summary including document counts and schema info to stdout.
//
// # Limitations
//
//   - Requires orchestrator to be running
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

// runWeaviateWipeout permanently deletes all data and schemas from Weaviate.
//
// # Description
//
// DESTRUCTIVE OPERATION: Requires --force flag AND interactive confirmation
// before proceeding. This cannot be undone without a backup.
//
// # Inputs
//
//   - --force flag: Required to proceed
//   - Interactive "yes" confirmation
//
// # Limitations
//
//   - Requires orchestrator to be running
//   - Cannot be undone without a backup
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

// runUploadLogs uploads log files to Google Cloud Storage.
//
// # Status
//
// DISABLED: Temporarily disabled in v0.3.0 pending config migration to aleutian.yaml.
func runUploadLogs(cmd *cobra.Command, args []string) {
	fmt.Println("GCS Uploads are temporarily disabled in v0.3.0 pending config migration.")
}

// runUploadBackups uploads backup files to Google Cloud Storage.
//
// # Status
//
// DISABLED: Temporarily disabled in v0.3.0 pending config migration to aleutian.yaml.
func runUploadBackups(cmd *cobra.Command, args []string) {
	fmt.Println("GCS Uploads are temporarily disabled in v0.3.0 pending config migration.")
}
