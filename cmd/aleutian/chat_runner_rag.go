// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

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
	"sync"
	"time"

	"github.com/jinterlante1206/AleutianLocal/pkg/ux"
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
	service          ChatService
	ui               ux.ChatUI
	input            InputReader
	pipeline         string
	initialSessionID string
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

	// Create production dependencies
	service := NewRAGChatService(RAGChatServiceConfig{
		BaseURL:   config.BaseURL,
		SessionID: config.SessionID,
		Pipeline:  pipeline,
	})

	ui := ux.NewChatUI()
	input := NewStdinReader()

	return &RAGChatRunner{
		service:          service,
		ui:               ui,
		input:            input,
		pipeline:         pipeline,
		initialSessionID: config.SessionID,
		closed:           false,
	}
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
//   - service: ChatService implementation (real or mock)
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
//	mockService := &mockChatService{
//	    sendMessageFunc: func(ctx context.Context, msg string) (*ChatServiceResponse, error) {
//	        return &ChatServiceResponse{Answer: "Hello!"}, nil
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
	service ChatService,
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
	// Display header
	r.ui.Header(ux.ChatModeRAG, r.pipeline, r.initialSessionID)

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
				r.ui.SessionEnd(r.service.GetSessionID())
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
			r.ui.SessionEnd(r.service.GetSessionID())
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

// handleMessage processes a single user message.
//
// # Description
//
// Sends the message to the RAG service and displays the response.
// Shows a spinner while waiting for the response.
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
//   - Spinner cannot be cancelled mid-animation
//
// # Assumptions
//
//   - Message is non-empty (caller validates)
func (r *RAGChatRunner) handleMessage(ctx context.Context, message string) error {
	// Show spinner during request
	spinner := ux.NewSpinner("Searching knowledge base...")
	spinner.Start()

	// Send message to service
	resp, err := r.service.SendMessage(ctx, message)
	spinner.Stop()

	if err != nil {
		return err
	}

	// Display response
	r.ui.Response(resp.Answer)

	// Display sources if available
	if len(resp.Sources) > 0 {
		r.ui.Sources(resp.Sources)
	} else {
		r.ui.NoSources()
	}
	fmt.Println()

	return nil
}

// handleShutdown performs graceful shutdown.
//
// # Description
//
// Called when context is cancelled. Performs cleanup:
//  1. Logs shutdown initiation
//  2. Saves conversation state (best effort)
//  3. Displays session end message
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

	// Display session end
	fmt.Println() // New line after interrupted input
	r.ui.SessionEnd(r.service.GetSessionID())

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
func (r *RAGChatRunner) saveConversationState(ctx context.Context) error {
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
