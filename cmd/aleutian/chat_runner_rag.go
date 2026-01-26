// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package main contains the RAGChatRunner implementation.
//
// This file implements the ChatRunner interface for RAG-enabled chat mode.
// It coordinates between the ChatService (HTTP communication), ChatUI (display),
// and InputReader (user input) to provide an interactive chat experience.
//
// See docs/designs/pending/streaming_chat_integration.md for full design.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/pkg/ux"
)

// =============================================================================
// RAGChatRunner Implementation
// =============================================================================

// RAGChatRunner implements ChatRunner for RAG-enabled streaming chat.
//
// # Description
//
// RAGChatRunner manages the interactive chat loop for RAG mode.
// It coordinates between the chat service (HTTP/SSE), the UI
// (headers, prompts, errors), and user input.
//
// The runner follows a single-responsibility pattern:
//   - Input reading is delegated to InputReader
//   - Service communication is delegated to ChatService
//   - Display formatting is delegated to ChatUI
//   - Runner only handles coordination and control flow
//
// # Fields
//
//   - service: ChatService for RAG communication (from chat_service.go)
//   - ui: ChatUI for display formatting (from pkg/ux)
//   - input: InputReader for user input (injectable for testing)
//   - pipeline: RAG pipeline name for display in header
//   - initialSessionID: Session ID provided at creation (for resume)
//   - sessionTTL: Configured session TTL (e.g., "5m", "24h")
//   - dataSpace: Dataspace being queried (e.g., "wheat")
//   - sessionStartTime: When the session started (for duration tracking)
//   - sessionStats: Accumulated statistics for the session
//   - uniqueSources: Set of unique source names referenced
//   - closed: Flag to ensure Close() is idempotent
//   - mu: Mutex protecting closed flag
//
// # Thread Safety
//
// The runner itself is not designed for concurrent Run() calls.
// However, Close() is thread-safe and can be called from any goroutine.
//
// # Limitations
//
//   - Single use: cannot restart after Run() completes
//   - No input history or editing (depends on InputReader implementation)
//   - Stdin reads cannot be interrupted mid-line (OS limitation)
//
// # Assumptions
//
//   - Service is properly initialized before Run() is called
//   - UI is ready for output (terminal is available)
//   - Context cancellation is set up by caller for graceful shutdown
type RAGChatRunner struct {
	service          StreamingChatService
	ui               ux.ChatUI
	input            InputReader
	pipeline         string
	initialSessionID string
	sessionTTL       string          // Configured TTL (e.g., "5m", "24h")
	dataSpace        string          // Dataspace being queried
	spellCorrector   *SpellCorrector // Typo correction (optional)
	sessionStartTime time.Time
	sessionStats     ux.SessionStats
	uniqueSources    map[string]bool
	strictMode       bool // Strict RAG mode: only answer from docs (no LLM fallback)
	closed           bool
	mu               sync.Mutex
}

// NewRAGChatRunner creates a RAG chat runner with production dependencies.
//
// # Description
//
// Creates a fully configured RAGChatRunner for production use.
// Initializes the RAG chat service, terminal UI, and stdin reader.
//
// Default values applied:
//   - Pipeline defaults to "reranking" if empty
//   - Personality defaults to ux.GetPersonality().Level if zero
//
// # Inputs
//
//   - config: RAGChatRunnerConfig with baseURL, pipeline, sessionID, personality
//
// # Outputs
//
//   - ChatRunner: Ready to run chat session (returns interface type)
//
// # Examples
//
//	runner := NewRAGChatRunner(RAGChatRunnerConfig{
//	    BaseURL:   "http://localhost:8080",
//	    Pipeline:  "reranking",
//	    SessionID: "",  // New session
//	})
//	defer runner.Close()
//
//	ctx := context.Background()
//	if err := runner.Run(ctx); err != nil {
//	    log.Fatal(err)
//	}
//
// # Limitations
//
//   - Creates real HTTP client and stdin reader (not for unit tests)
//   - Use NewRAGChatRunnerWithDeps for testing
//
// # Assumptions
//
//   - BaseURL is valid and orchestrator is reachable
//   - Terminal is available for UI output
func NewRAGChatRunner(config RAGChatRunnerConfig) ChatRunner {
	// Apply defaults
	pipeline := config.Pipeline
	if pipeline == "" {
		pipeline = "reranking"
	}

	personality := config.Personality
	if personality == "" {
		personality = ux.GetPersonality().Level
	}

	// Create production dependencies - streaming service for real-time output
	service := NewRAGStreamingChatService(RAGStreamingChatServiceConfig{
		BaseURL:     config.BaseURL,
		SessionID:   config.SessionID,
		Pipeline:    pipeline,
		Writer:      os.Stdout,
		Personality: personality,
		StrictMode:  config.StrictMode,
		Verbosity:   config.Verbosity,
		DataSpace:   config.DataSpace,
		DocVersion:  config.DocVersion,
		SessionTTL:  config.SessionTTL,
		RecencyBias: config.RecencyBias,
	})

	ui := ux.NewChatUI()
	input := NewInteractiveInputReader(50) // Keep last 50 prompts in history

	// Initialize spell corrector with dataspace name as a known term
	// Also include common English words (5000 most common) to avoid false corrections
	var corrector *SpellCorrector
	if config.DataSpace != "" {
		// Start with common English words from embedded word list
		terms := GetCommonWords()
		// Add dataspace name with highest priority (will override if in common words)
		terms[config.DataSpace] = 0             // 0 = highest priority (lowest rank number)
		corrector = NewSpellCorrector(terms, 2) // Max 2 edit distance
	}

	return &RAGChatRunner{
		service:          service,
		ui:               ui,
		input:            input,
		pipeline:         pipeline,
		initialSessionID: config.SessionID,
		sessionTTL:       config.SessionTTL,
		dataSpace:        config.DataSpace,
		spellCorrector:   corrector,
		uniqueSources:    make(map[string]bool),
		strictMode:       config.StrictMode,
		closed:           false,
	}
}

// RAGChatRunnerTestConfig holds configuration for creating RAGChatRunner in tests.
type RAGChatRunnerTestConfig struct {
	Service   StreamingChatService
	UI        ux.ChatUI
	Input     InputReader
	Pipeline  string
	SessionID string
	TTL       string
	DataSpace string
}

// NewRAGChatRunnerWithDeps creates a RAG chat runner with injected dependencies.
//
// # Description
//
// Creates a RAGChatRunner with injected dependencies for testing.
// Allows mocking of service, UI, and input reader for unit tests.
//
// # Inputs
//
//   - service: StreamingChatService implementation (real or mock)
//   - ui: ChatUI instance (can use NewChatUIWithWriter for testing)
//   - input: InputReader implementation (use MockInputReader for testing)
//   - pipeline: Pipeline name for display in header
//
// # Outputs
//
//   - *RAGChatRunner: Ready to run chat session (returns concrete type for testing)
//
// # Examples
//
//	// Test setup
//	mockService := &mockStreamingChatService{
//	    sendMessageFunc: func(ctx context.Context, msg string) (*ux.StreamResult, error) {
//	        return &ux.StreamResult{Answer: "Hello!"}, nil
//	    },
//	}
//	mockInput := NewMockInputReader([]string{"hello", "exit"})
//	var buf bytes.Buffer
//	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)
//
//	runner := NewRAGChatRunnerWithDeps(mockService, ui, mockInput, "test-pipeline")
//	err := runner.Run(context.Background())
//
// # Limitations
//
//   - Caller is responsible for dependency lifecycle
//   - Dependencies must be properly initialized before passing
//
// # Assumptions
//
//   - All dependencies are non-nil and properly initialized
//   - Service is ready to accept messages
func NewRAGChatRunnerWithDeps(
	service StreamingChatService,
	ui ux.ChatUI,
	input InputReader,
	pipeline string,
) *RAGChatRunner {
	return &RAGChatRunner{
		service:          service,
		ui:               ui,
		input:            input,
		pipeline:         pipeline,
		initialSessionID: "",
		sessionTTL:       "",
		dataSpace:        "",
		uniqueSources:    make(map[string]bool),
		closed:           false,
	}
}

// NewRAGChatRunnerWithTestConfig creates a RAG chat runner with full test configuration.
//
// # Description
//
// Creates a RAGChatRunner with all configurable fields for comprehensive testing.
// Use this when testing TTL and dataspace header display.
//
// # Inputs
//
//   - config: RAGChatRunnerTestConfig with all test dependencies and settings
//
// # Outputs
//
//   - *RAGChatRunner: Ready to run chat session
func NewRAGChatRunnerWithTestConfig(config RAGChatRunnerTestConfig) *RAGChatRunner {
	return &RAGChatRunner{
		service:          config.Service,
		ui:               config.UI,
		input:            config.Input,
		pipeline:         config.Pipeline,
		initialSessionID: config.SessionID,
		sessionTTL:       config.TTL,
		dataSpace:        config.DataSpace,
		uniqueSources:    make(map[string]bool),
		closed:           false,
	}
}

// Run executes the interactive RAG chat loop.
//
// # Description
//
// Runs the main chat loop for RAG mode. The loop:
//  1. Displays chat header with mode and pipeline info
//  2. Prompts for user input
//  3. Checks for exit commands ("exit", "quit")
//  4. Sends message to RAG service with spinner
//  5. Displays response and sources via UI
//  6. Repeats until exit or context cancellation
//
// Graceful shutdown:
//   - On context cancellation, saves conversation state and returns
//   - In-flight requests are given 5 seconds to complete
//   - Session ID is logged for potential resume
//
// # Inputs
//
//   - ctx: Context for cancellation. Cancel to trigger graceful shutdown.
//
// # Outputs
//
//   - error: nil on normal exit ("exit"/"quit"), context.Canceled on shutdown,
//     or error if fatal failure occurs
//
// # Examples
//
//	runner := NewRAGChatRunner(config)
//	defer runner.Close()
//
//	// Simple usage
//	err := runner.Run(context.Background())
//
//	// With graceful shutdown
//	ctx, cancel := context.WithCancel(context.Background())
//	go func() {
//	    sigCh := make(chan os.Signal, 1)
//	    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
//	    <-sigCh
//	    cancel()
//	}()
//	err := runner.Run(ctx)
//
// # Limitations
//
//   - Blocks until exit condition
//   - Stdin reads cannot be interrupted mid-line
//   - Runner cannot be reused after Run() returns
//
// # Assumptions
//
//   - Service is ready to accept messages
//   - Terminal is available for UI output
//   - Input source provides newline-terminated lines
func (r *RAGChatRunner) Run(ctx context.Context) error {
	// Record session start time for duration tracking
	r.sessionStartTime = time.Now()

	// Fetch dataspace stats if available (non-blocking, errors are ignored)
	var stats *ux.DataSpaceStats
	if r.dataSpace != "" {
		var err error
		stats, err = r.service.GetDataSpaceStats(ctx)
		if err != nil {
			slog.Warn("failed to fetch dataspace stats, continuing without",
				"dataspace", r.dataSpace,
				"error", err,
			)
		}
	}

	// Display header with TTL, dataspace, and stats
	r.ui.HeaderWithConfig(ux.HeaderConfig{
		Mode:           ux.ChatModeRAG,
		Pipeline:       r.pipeline,
		SessionID:      r.initialSessionID,
		TTL:            r.sessionTTL,
		DataSpace:      r.dataSpace,
		DataSpaceStats: stats,
	})

	// Display RAG mode notice
	if r.strictMode {
		fmt.Println()
		fmt.Println(ux.Styles.Muted.Render("  Strict RAG Mode: Only answers from your documents."))
		fmt.Println(ux.Styles.Muted.Render("  Use --unrestricted to allow LLM fallback when no relevant docs found."))
		fmt.Println()
	} else {
		fmt.Println()
		fmt.Println(ux.Styles.Warning.Render("  Unrestricted Mode: LLM may answer without document context."))
		fmt.Println()
	}

	// Main chat loop
	for {
		// Check for context cancellation before blocking on input
		select {
		case <-ctx.Done():
			return r.handleShutdown(ctx)
		default:
			// Continue to read input
		}

		// Display prompt and read input
		// If the reader handles prompts (interactive mode), set it; otherwise print manually
		if p, ok := r.input.(PromptingInputReader); ok {
			p.SetPrompt(r.ui.Prompt())
		} else {
			fmt.Print(r.ui.Prompt())
		}
		input, err := r.input.ReadLine()
		if err != nil {
			if err == io.EOF {
				// Input exhausted (e.g., piped input ended)
				r.displaySessionEndWithStats()
				return nil
			}
			slog.Error("failed to read input", "error", err)
			return fmt.Errorf("read input: %w", err)
		}

		// Skip empty input
		if input == "" {
			continue
		}

		// Echo the user's input for interactive readers
		// Bubbletea clears its rendering area on exit, so we restore the visual line
		if _, isInteractive := r.input.(*InteractiveInputReader); isInteractive {
			fmt.Printf("%s%s\n", r.ui.Prompt(), input)
		}

		// Check for exit command
		if isExitCommand(input) {
			r.displaySessionEndWithStats()
			return nil
		}

		// Check for typos and apply corrections
		input = r.applySpellCorrection(input)

		// Process the message
		if err := r.handleMessage(ctx, input); err != nil {
			// Check if error is due to context cancellation
			if ctx.Err() != nil {
				return r.handleShutdown(ctx)
			}
			// Non-fatal error: display and continue
			r.ui.Error(err)
			continue
		}
	}
}

// handleMessage processes a single user message.
//
// # Description
//
// Sends the message to the RAG streaming service. The response is
// rendered in real-time as tokens arrive via the StreamRenderer.
// No spinner is needed since tokens appear immediately.
// Accumulates statistics from the result for session summary.
//
// # Inputs
//
//   - ctx: Context for cancellation
//   - message: User's input message
//
// # Outputs
//
//   - error: Non-nil if service call failed
//
// # Limitations
//
//   - Streaming requires server SSE support
//
// # Assumptions
//
//   - Message is non-empty (caller validates)
func (r *RAGChatRunner) handleMessage(ctx context.Context, message string) error {
	// Streaming service renders tokens in real-time via StreamRenderer
	// No spinner needed - user sees tokens as they arrive
	result, err := r.service.SendMessage(ctx, message)
	if err != nil {
		return err
	}

	// Accumulate session statistics from this exchange
	r.accumulateStats(result)

	// Response and sources already displayed during streaming
	// via StreamRenderer.OnToken(), OnSources(), OnDone() callbacks
	fmt.Println()

	return nil
}

// accumulateStats updates session statistics from a stream result.
//
// # Description
//
// Aggregates metrics from a single message exchange into the session
// totals. Called after each successful message for the session summary.
//
// # Inputs
//
//   - result: Stream result from the message exchange
//
// # Outputs
//
// None. Updates r.sessionStats and r.uniqueSources in place.
//
// # Limitations
//
//   - Only tracks unique sources by name (not by full path)
//
// # Assumptions
//
//   - Result is non-nil (caller validates)
func (r *RAGChatRunner) accumulateStats(result *ux.StreamResult) {
	r.sessionStats.MessageCount++
	r.sessionStats.TotalTokens += result.TotalTokens
	r.sessionStats.ThinkingTokens += result.ThinkingTokens

	// Track unique sources
	for _, src := range result.Sources {
		r.uniqueSources[src.Source] = true
	}
	r.sessionStats.SourcesUsed = len(r.uniqueSources)

	// Track first response latency (only for first message)
	if r.sessionStats.MessageCount == 1 {
		r.sessionStats.FirstResponseLatency = result.TimeToFirstToken()
	}
}

// displaySessionEndWithStats displays session end with accumulated statistics.
//
// # Description
//
// Finalizes session statistics and displays the rich session end
// summary. Calculates session duration from start time.
//
// # Inputs
//
// None. Uses r.sessionStartTime, r.sessionStats, and service session ID.
//
// # Outputs
//
// None. Writes to UI.
//
// # Limitations
//
//   - Duration is approximate (wall clock time)
//
// # Assumptions
//
//   - Session start time was recorded
func (r *RAGChatRunner) displaySessionEndWithStats() {
	// Finalize duration
	r.sessionStats.Duration = time.Since(r.sessionStartTime)

	// Display rich session end
	r.ui.SessionEndRich(r.service.GetSessionID(), &r.sessionStats)
}

// handleShutdown performs graceful shutdown.
//
// # Description
//
// Called when context is cancelled. Performs cleanup:
//  1. Logs shutdown initiation
//  2. Saves conversation state (best effort)
//  3. Displays session end message with statistics
//  4. Returns context error
//
// applySpellCorrection checks for typos and applies corrections.
//
// # Description
//
// Uses Levenshtein distance to detect likely typos in the user input.
// For high-confidence matches (1 edit distance), auto-corrects and notifies.
// For lower-confidence matches (2 edit distance), suggests but doesn't change.
//
// # Inputs
//
//   - input: The user's input text
//
// # Outputs
//
//   - string: The possibly corrected input
//
// # Limitations
//
//   - Only corrects words that are similar to known terms
//   - Currently only knows the dataspace name
func (r *RAGChatRunner) applySpellCorrection(input string) string {
	if r.spellCorrector == nil {
		return input
	}

	suggestions := r.spellCorrector.Check(input)
	if len(suggestions) == 0 {
		return input
	}

	// Process the first suggestion (most likely typo)
	suggestion := suggestions[0]

	if suggestion.Distance == 1 {
		// High confidence - auto-correct
		r.ui.ShowAutoCorrection(suggestion.Original, suggestion.Suggested)
		return replaceWord(input, suggestion.Original, suggestion.Suggested)
	}

	// Lower confidence - just suggest, don't auto-correct
	r.ui.ShowCorrectionSuggestion(suggestion.Original, suggestion.Suggested)
	return input
}

// replaceWord replaces all occurrences of a word in text (case-insensitive).
//
// # Description
//
// Uses regex with word boundaries for robust, case-insensitive replacement
// of all occurrences of a word.
//
// # Inputs
//
//   - text: The text to search in
//   - oldWord: The word to replace (case-insensitive match)
//   - newWord: The replacement word
//
// # Outputs
//
//   - string: Text with all occurrences replaced
//
// # Examples
//
//	replaceWord("show me wheet and wheet data", "wheet", "wheat")
//	// Returns: "show me wheat and wheat data"
func replaceWord(text, oldWord, newWord string) string {
	// Use regex for robust, case-insensitive, whole-word replacement
	// (?i) makes it case-insensitive
	// \b ensures we match whole words only
	re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(oldWord) + `\b`)
	return re.ReplaceAllString(text, newWord)
}

// # Inputs
//
//   - ctx: The cancelled context
//
// # Outputs
//
//   - error: The context's error (typically context.Canceled)
//
// # Limitations
//
//   - State save is best-effort (may timeout)
//
// # Assumptions
//
//   - Context is already cancelled
func (r *RAGChatRunner) handleShutdown(ctx context.Context) error {
	slog.Info("graceful shutdown initiated",
		"session_id", r.service.GetSessionID(),
	)

	// Create a timeout context for shutdown operations
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Save conversation state (best effort)
	if err := r.saveConversationState(shutdownCtx); err != nil {
		slog.Warn("failed to save conversation state during shutdown",
			"error", err,
		)
	}

	// Display session end with statistics
	fmt.Println() // New line after interrupted input
	r.displaySessionEndWithStats()

	return ctx.Err()
}

// saveConversationState saves the current session state before shutdown.
//
// # Description
//
// Called during graceful shutdown to preserve conversation data.
// Currently logs session ID for potential resume. Server-side
// storage handles actual persistence after each message.
//
// # Inputs
//
//   - ctx: Context with timeout for save operation
//
// # Outputs
//
//   - error: Non-nil if save failed (currently always nil)
//
// # Limitations
//
//   - Currently only logs session ID
//   - Future: could write to ~/.aleutian/last_session
//
// # Assumptions
//
//   - Session data already persisted server-side
func (r *RAGChatRunner) saveConversationState(_ context.Context) error {
	sessionID := r.service.GetSessionID()
	if sessionID != "" {
		slog.Info("conversation state preserved",
			"session_id", sessionID,
			"note", "session can be resumed with --resume flag",
		)
	}
	// Server-side storage already handles persistence
	// Future: write session ID to ~/.aleutian/last_session
	return nil
}

// Close releases all resources held by the runner.
//
// # Description
//
// Closes the underlying chat service and marks the runner as closed.
// Safe to call multiple times (idempotent).
// Should be called after Run() returns, typically via defer.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - error: Non-nil if service Close() failed
//
// # Examples
//
//	runner := NewRAGChatRunner(config)
//	defer runner.Close()  // Always close, even on error
//	err := runner.Run(ctx)
//
// # Limitations
//
//   - Does not interrupt Run() if still executing
//   - Must wait for Run() to return before Close() has full effect
//
// # Assumptions
//
//   - Run() has returned (or was never called)
func (r *RAGChatRunner) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil // Already closed, idempotent
	}

	r.closed = true
	return r.service.Close()
}
