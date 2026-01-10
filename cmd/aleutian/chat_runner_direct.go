// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

// Package main contains the DirectChatRunner implementation.
//
// This file implements the ChatRunner interface for direct LLM chat mode
// (no RAG). It coordinates between the DirectChatService, ChatUI, and
// InputReader to provide an interactive chat experience without knowledge
// base retrieval.
//
// See docs/designs/pending/streaming_chat_integration.md for full design.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/jinterlante1206/AleutianLocal/pkg/ux"
)

// =============================================================================
// DirectChatRunner Implementation
// =============================================================================

// DirectChatRunner implements ChatRunner for direct LLM streaming chat.
//
// # Description
//
// DirectChatRunner manages the interactive chat loop for direct mode
// (no RAG). It coordinates between the direct chat service, the UI,
// and user input. Supports session resume via LoadSessionHistory.
//
// Unlike RAGChatRunner, DirectChatRunner:
//   - Does not display sources (no knowledge base retrieval)
//   - Maintains client-side conversation history via the service
//   - Supports loading previous session history for resume
//   - Supports extended thinking mode (Claude only)
//
// # Fields
//
//   - service: *directChatService for direct LLM communication
//     (concrete type to access LoadSessionHistory method)
//   - ui: ChatUI for display formatting
//   - input: InputReader for user input (injectable for testing)
//   - sessionID: Session ID for resume (empty for new sessions)
//   - sessionStartTime: When the session started (for duration tracking)
//   - sessionStats: Accumulated statistics for the session
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
//   - Session history loading requires valid session ID
//   - Stdin reads cannot be interrupted mid-line (OS limitation)
//
// # Assumptions
//
//   - Service is properly initialized before Run() is called
//   - Session ID (if provided) exists on server
//   - UI is ready for output
type DirectChatRunner struct {
	service          *directStreamingChatService // Concrete type for LoadSessionHistory access
	ui               ux.ChatUI
	input            InputReader
	sessionID        string
	sessionStartTime time.Time
	sessionStats     ux.SessionStats
	closed           bool
	mu               sync.Mutex
}

// NewDirectChatRunner creates a direct chat runner with production dependencies.
//
// # Description
//
// Creates a fully configured DirectChatRunner for production use.
// Initializes the direct chat service, terminal UI, and stdin reader.
//
// Default values applied:
//   - Personality defaults to ux.GetPersonality().Level if zero
//   - BudgetTokens defaults to 2048 if EnableThinking is true and BudgetTokens is 0
//
// # Inputs
//
//   - config: DirectChatRunnerConfig with baseURL, sessionID, thinking settings
//
// # Outputs
//
//   - ChatRunner: Ready to run chat session (returns interface type)
//
// # Examples
//
//	// Basic usage
//	runner := NewDirectChatRunner(DirectChatRunnerConfig{
//	    BaseURL: "http://localhost:8080",
//	})
//	defer runner.Close()
//	runner.Run(context.Background())
//
//	// With extended thinking
//	runner := NewDirectChatRunner(DirectChatRunnerConfig{
//	    BaseURL:        "http://localhost:8080",
//	    EnableThinking: true,
//	    BudgetTokens:   4096,
//	})
//
//	// Resume existing session
//	runner := NewDirectChatRunner(DirectChatRunnerConfig{
//	    BaseURL:   "http://localhost:8080",
//	    SessionID: "sess-abc123",
//	})
//
// # Limitations
//
//   - Creates real HTTP client and stdin reader (not for unit tests)
//   - Use NewDirectChatRunnerWithDeps for testing
//
// # Assumptions
//
//   - BaseURL is valid and orchestrator is reachable
//   - Terminal is available for UI output
//   - Extended thinking requires Claude model on backend
func NewDirectChatRunner(config DirectChatRunnerConfig) ChatRunner {
	// Apply defaults
	personality := config.Personality
	if personality == "" {
		personality = ux.GetPersonality().Level
	}

	budgetTokens := config.BudgetTokens
	if config.EnableThinking && budgetTokens == 0 {
		budgetTokens = 2048
	}

	// Create production dependencies - streaming service for real-time output
	service := NewDirectStreamingChatService(DirectStreamingChatServiceConfig{
		BaseURL:        config.BaseURL,
		SessionID:      config.SessionID,
		EnableThinking: config.EnableThinking,
		BudgetTokens:   budgetTokens,
		Writer:         os.Stdout,
		Personality:    personality,
	})

	ui := ux.NewChatUI()
	input := NewStdinReader()

	return &DirectChatRunner{
		service:   service,
		ui:        ui,
		input:     input,
		sessionID: config.SessionID,
		closed:    false,
	}
}

// NewDirectChatRunnerWithDeps creates a direct chat runner with injected dependencies.
//
// # Description
//
// Creates a DirectChatRunner with injected dependencies for testing.
// Allows mocking of service, UI, and input reader for unit tests.
//
// # Inputs
//
//   - service: *directStreamingChatService implementation (concrete type for LoadSessionHistory)
//   - ui: ChatUI instance (can use NewChatUIWithWriter for testing)
//   - input: InputReader implementation (use MockInputReader for testing)
//   - sessionID: Session ID for resume (empty for new sessions)
//
// # Outputs
//
//   - *DirectChatRunner: Ready to run chat session (returns concrete type for testing)
//
// # Examples
//
//	// Test setup with mock service
//	mockService := NewDirectStreamingChatServiceWithClient(mockHTTPClient, config)
//	mockInput := NewMockInputReader([]string{"hello", "exit"})
//	var buf bytes.Buffer
//	ui := ux.NewChatUIWithWriter(&buf, ux.PersonalityStandard)
//
//	runner := NewDirectChatRunnerWithDeps(mockService, ui, mockInput, "")
//	err := runner.Run(context.Background())
//
// # Limitations
//
//   - Caller is responsible for dependency lifecycle
//   - Service must be concrete *directStreamingChatService type
//
// # Assumptions
//
//   - All dependencies are non-nil and properly initialized
//   - Service is ready to accept messages
func NewDirectChatRunnerWithDeps(
	service *directStreamingChatService,
	ui ux.ChatUI,
	input InputReader,
	sessionID string,
) *DirectChatRunner {
	return &DirectChatRunner{
		service:   service,
		ui:        ui,
		input:     input,
		sessionID: sessionID,
		closed:    false,
	}
}

// Run executes the interactive direct chat loop.
//
// # Description
//
// Runs the main chat loop for direct mode. The loop:
//  1. Loads session history if resuming (sessionID provided)
//  2. Displays chat header with mode info
//  3. Prompts for user input
//  4. Checks for exit commands ("exit", "quit")
//  5. Sends message to direct chat service with spinner
//  6. Displays response via UI (no sources in direct mode)
//  7. Repeats until exit or context cancellation
//
// Session resume:
//   - If sessionID is provided, loads previous conversation history
//   - Displays number of turns loaded
//   - Fatal error if history load fails (user expects to resume)
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
//     or error if fatal failure occurs (e.g., history load fails)
//
// # Examples
//
//	runner := NewDirectChatRunner(config)
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
//   - Session history load failure is fatal
//   - Stdin reads cannot be interrupted mid-line
//   - Runner cannot be reused after Run() returns
//
// # Assumptions
//
//   - Service is ready to accept messages
//   - Terminal is available for UI output
//   - Session ID (if provided) exists on server
func (r *DirectChatRunner) Run(ctx context.Context) error {
	// Record session start time for duration tracking
	r.sessionStartTime = time.Now()

	// Load session history if resuming
	if r.sessionID != "" {
		if err := r.loadHistory(ctx); err != nil {
			// Fatal error: user expects to resume existing session
			log.Fatalf("Failed to load session history: %v", err)
		}
	}

	// Display header
	r.ui.Header(ux.ChatModeDirect, "", r.service.GetSessionID())

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
		fmt.Print(r.ui.Prompt())
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

		// Check for exit command
		if isExitCommand(input) {
			r.displaySessionEndWithStats()
			return nil
		}

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

// loadHistory loads session history for resume.
//
// # Description
//
// Fetches previous conversation turns from the server and populates
// the service's message history. Displays the number of turns loaded
// via the UI.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout
//
// # Outputs
//
//   - error: Non-nil if history load failed
//
// # Limitations
//
//   - Requires valid session ID
//   - Server must have session data
//
// # Assumptions
//
//   - r.sessionID is non-empty (caller validates)
func (r *DirectChatRunner) loadHistory(ctx context.Context) error {
	turns, err := r.service.LoadSessionHistory(ctx, r.sessionID)
	if err != nil {
		return err
	}
	r.ui.SessionResume(r.sessionID, turns)
	return nil
}

// handleMessage processes a single user message.
//
// # Description
//
// Sends the message to the direct streaming service. The response is
// rendered in real-time as tokens arrive via the StreamRenderer.
// No spinner is needed since tokens appear immediately.
// Unlike RAG mode, direct chat does not display sources.
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
func (r *DirectChatRunner) handleMessage(ctx context.Context, message string) error {
	// Streaming service renders tokens in real-time via StreamRenderer
	// No spinner needed - user sees tokens as they arrive
	result, err := r.service.SendMessage(ctx, message)
	if err != nil {
		return err
	}

	// Accumulate session statistics from this exchange
	r.accumulateStats(result)

	// Response already displayed during streaming
	// via StreamRenderer.OnToken(), OnDone() callbacks
	fmt.Println()

	return nil
}

// accumulateStats updates session statistics from a stream result.
//
// # Description
//
// Aggregates metrics from a single message exchange into the session
// totals. Called after each successful message for the session summary.
// Direct chat does not track sources (no RAG retrieval).
//
// # Inputs
//
//   - result: Stream result from the message exchange
//
// # Outputs
//
// None. Updates r.sessionStats in place.
//
// # Limitations
//
//   - Does not track sources (direct chat has no RAG)
//
// # Assumptions
//
//   - Result is non-nil (caller validates)
func (r *DirectChatRunner) accumulateStats(result *ux.StreamResult) {
	r.sessionStats.MessageCount++
	r.sessionStats.TotalTokens += result.TotalTokens
	r.sessionStats.ThinkingTokens += result.ThinkingTokens

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
func (r *DirectChatRunner) displaySessionEndWithStats() {
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
func (r *DirectChatRunner) handleShutdown(ctx context.Context) error {
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
// For direct chat, the conversation history is maintained client-side
// by the service. This method logs the session ID for potential resume.
//
// Note: Direct chat conversation history is NOT persisted server-side
// like RAG chat. The session ID is for client-side tracking only.
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
//   - Direct chat history is client-side only
//   - Currently only logs session ID
//   - Future: could serialize history to local file
//
// # Assumptions
//
//   - Service has valid session ID (may be empty for new sessions)
func (r *DirectChatRunner) saveConversationState(ctx context.Context) error {
	sessionID := r.service.GetSessionID()
	if sessionID != "" {
		slog.Info("direct chat session info preserved",
			"session_id", sessionID,
			"note", "direct chat history is client-side only",
		)
	}
	// Future: could serialize service.messages to ~/.aleutian/direct_history_{sessionID}.json
	return nil
}

// Close releases all resources held by the runner.
//
// # Description
//
// Closes the underlying direct chat service and marks the runner as closed.
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
//	runner := NewDirectChatRunner(config)
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
func (r *DirectChatRunner) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil // Already closed, idempotent
	}

	r.closed = true
	return r.service.Close()
}
