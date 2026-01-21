// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

// Package main contains the Aleutian CLI chat runner interfaces and implementations.
//
// This file defines the ChatRunner interface for abstracting chat loop execution,
// enabling different implementations for RAG and direct chat modes. It follows
// the interface-first pattern from claude.md Section 3.2.
//
// Architecture:
//
//	cmd_chat.go → ChatRunner Interface → RAGChatRunner / DirectChatRunner
//	                                     ↓
//	                                     ChatService (from chat_service.go)
//	                                     InputReader (stdin abstraction)
//	                                     ChatUI (from pkg/ux)
//
// See docs/designs/pending/streaming_chat_integration.md for full design.
package main

import (
	"bufio"
	"context"
	"io"
	"os"
	"strings"

	"github.com/jinterlante1206/AleutianLocal/pkg/ux"
)

// =============================================================================
// ChatRunner Interface
// =============================================================================

// ChatRunner defines the contract for running interactive chat sessions.
//
// # Description
//
// ChatRunner abstracts the chat loop execution, enabling different
// implementations for RAG and direct chat modes. Implementations handle
// user input, service communication, and output rendering.
//
// ChatRunner embeds io.Closer for resource cleanup. Callers MUST call
// Close() when done, typically via defer.
//
// # Inputs
//
// Run accepts a context for cancellation support. When context is
// cancelled, Run performs graceful shutdown:
//   - Stops accepting new input
//   - Completes in-flight request (if possible)
//   - Saves conversation state
//   - Cleans up resources
//
// # Outputs
//
// Run returns an error if the chat session failed to start or
// encountered an unrecoverable error. Normal exit (user types "exit")
// returns nil. Context cancellation returns context.Canceled.
//
// # Examples
//
//	runner := NewRAGChatRunner(config)
//	defer runner.Close()
//
//	ctx, cancel := context.WithCancel(context.Background())
//	// Set up signal handler to call cancel() on SIGINT/SIGTERM
//
//	if err := runner.Run(ctx); err != nil && err != context.Canceled {
//	    log.Fatal(err)
//	}
//
// # Limitations
//
//   - Implementations are not reusable after Run() returns
//   - In-flight requests may timeout on shutdown
//
// # Assumptions
//
//   - Underlying service is properly configured
//   - Terminal supports the configured personality mode
//   - Caller sets up signal handling for graceful shutdown
type ChatRunner interface {
	// Run executes the interactive chat loop until exit, error, or context cancellation.
	//
	// # Description
	//
	// Runs the main chat loop. Exits when:
	//   - User types "exit" or "quit" (returns nil)
	//   - Context is cancelled (returns context.Canceled)
	//   - Fatal error occurs (returns error)
	//
	// # Inputs
	//
	//   - ctx: Context for cancellation. Cancel to trigger graceful shutdown.
	//
	// # Outputs
	//
	//   - error: nil on normal exit, context.Canceled on shutdown, or error
	//
	// # Limitations
	//
	//   - Blocks until exit condition
	//
	// # Assumptions
	//
	//   - Service is ready to accept messages
	Run(ctx context.Context) error

	// Close releases all resources held by the runner.
	//
	// # Description
	//
	// Closes the underlying service and performs cleanup.
	// Safe to call multiple times. Must be called after Run() returns.
	//
	// # Inputs
	//
	// None.
	//
	// # Outputs
	//
	//   - error: Non-nil if cleanup failed
	//
	// # Limitations
	//
	//   - Does not interrupt Run() if still executing
	//
	// # Assumptions
	//
	//   - Run() has returned before Close() is called
	Close() error
}

// =============================================================================
// InputReader Interface
// =============================================================================

// InputReader abstracts user input reading for testability.
//
// # Description
//
// InputReader enables mocking of stdin in unit tests. Production
// implementation wraps bufio.Reader; test implementation returns
// predetermined inputs.
//
// # Inputs
//
// ReadLine takes no parameters.
//
// # Outputs
//
// Returns the line read (trimmed) and any error.
// Returns io.EOF when input is exhausted.
//
// # Examples
//
//	reader := NewStdinReader()
//	line, err := reader.ReadLine()
//	if err == io.EOF {
//	    // Input exhausted
//	}
//
// # Limitations
//
//   - Does not support multi-line input
//   - No line editing support (no readline/linenoise)
//
// # Assumptions
//
//   - Input source is line-oriented
//   - Lines are newline-terminated
type InputReader interface {
	// ReadLine reads a single line of input.
	//
	// # Description
	//
	// Reads until newline and returns the trimmed line.
	// Blocks until input is available.
	//
	// # Inputs
	//
	// None.
	//
	// # Outputs
	//
	//   - string: The line read, with leading/trailing whitespace trimmed
	//   - error: io.EOF when input exhausted, or other read error
	//
	// # Limitations
	//
	//   - Blocks until newline received
	//
	// # Assumptions
	//
	//   - Input is newline-terminated
	ReadLine() (string, error)
}

// =============================================================================
// StdinReader Implementation
// =============================================================================

// StdinReader implements InputReader for production stdin reading.
//
// # Description
//
// StdinReader wraps bufio.Reader to read lines from os.Stdin.
// This is the production implementation; tests use MockInputReader.
//
// # Fields
//
//   - reader: Underlying bufio.Reader wrapping os.Stdin
//
// # Thread Safety
//
// Not thread-safe. Single reader per stdin. Do not share across goroutines.
//
// # Limitations
//
//   - Blocks until input available
//   - No line editing support (no up-arrow history, no tab completion)
//   - Cannot be cancelled mid-read (stdin blocking is OS-level)
//
// # Assumptions
//
//   - os.Stdin is available and readable
//   - Input is line-oriented (newline-terminated)
type StdinReader struct {
	reader *bufio.Reader
}

// NewStdinReader creates a new StdinReader wrapping os.Stdin.
//
// # Description
//
// Creates and returns a StdinReader ready for reading user input.
// Uses bufio.Reader for efficient line-based reading.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - *StdinReader: Ready to read from stdin
//
// # Examples
//
//	reader := NewStdinReader()
//	for {
//	    line, err := reader.ReadLine()
//	    if err == io.EOF {
//	        break
//	    }
//	    fmt.Println("You said:", line)
//	}
//
// # Limitations
//
//   - Creates new bufio.Reader each time (don't call repeatedly)
//
// # Assumptions
//
//   - os.Stdin is valid
func NewStdinReader() *StdinReader {
	return &StdinReader{
		reader: bufio.NewReader(os.Stdin),
	}
}

// ReadLine reads a single line from stdin.
//
// # Description
//
// Reads until newline character and returns the trimmed result.
// Blocks until input is available or stdin is closed.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - string: The line read, with leading/trailing whitespace trimmed
//   - error: io.EOF when stdin closed, or other read error
//
// # Limitations
//
//   - Blocks until newline received or EOF
//   - Cannot be interrupted (stdin read is OS-level blocking)
//
// # Assumptions
//
//   - Stdin is open and readable
func (r *StdinReader) ReadLine() (string, error) {
	line, err := r.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// =============================================================================
// MockInputReader Implementation (for testing)
// =============================================================================

// MockInputReader implements InputReader for testing.
//
// # Description
//
// MockInputReader returns predetermined inputs for unit testing
// chat runners without requiring actual user input. Each call to
// ReadLine returns the next input in sequence.
//
// # Fields
//
//   - inputs: Slice of inputs to return in order
//   - index: Current position in inputs slice
//
// # Thread Safety
//
// Not thread-safe. Designed for single-threaded tests.
//
// # Limitations
//
//   - Fixed input sequence (cannot add inputs after creation)
//   - Returns io.EOF after all inputs consumed
//
// # Assumptions
//
//   - Caller provides sufficient inputs for test scenario
//   - Test will handle io.EOF appropriately
type MockInputReader struct {
	inputs []string
	index  int
}

// NewMockInputReader creates a MockInputReader with predetermined inputs.
//
// # Description
//
// Creates a MockInputReader that will return the given inputs in order.
// After all inputs are consumed, ReadLine returns io.EOF.
//
// # Inputs
//
//   - inputs: Slice of strings to return from ReadLine calls
//
// # Outputs
//
//   - *MockInputReader: Ready for use in tests
//
// # Examples
//
//	mock := NewMockInputReader([]string{"hello", "how are you", "exit"})
//	line1, _ := mock.ReadLine() // "hello"
//	line2, _ := mock.ReadLine() // "how are you"
//	line3, _ := mock.ReadLine() // "exit"
//	_, err := mock.ReadLine()   // io.EOF
//
// # Limitations
//
//   - Inputs are fixed at creation time
//
// # Assumptions
//
//   - Inputs slice is not modified after creation
func NewMockInputReader(inputs []string) *MockInputReader {
	return &MockInputReader{
		inputs: inputs,
		index:  0,
	}
}

// ReadLine returns the next predetermined input.
//
// # Description
//
// Returns inputs in order, then io.EOF when exhausted.
// Does not block (returns immediately).
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - string: Next input from the sequence
//   - error: io.EOF when all inputs consumed
//
// # Limitations
//
//   - Cannot add more inputs after creation
//
// # Assumptions
//
//   - Called sequentially (not thread-safe)
func (m *MockInputReader) ReadLine() (string, error) {
	if m.index >= len(m.inputs) {
		return "", io.EOF
	}
	line := m.inputs[m.index]
	m.index++
	return line, nil
}

// =============================================================================
// Configuration Structs
// =============================================================================

// RAGChatRunnerConfig holds configuration for creating RAGChatRunner.
//
// # Description
//
// Groups all configuration needed to create a RAGChatRunner instance.
// Required fields must be provided; optional fields have sensible defaults.
//
// # Fields
//
//   - BaseURL: Required. Orchestrator URL (e.g., "http://localhost:8080").
//     Must not include trailing slash.
//   - Pipeline: Optional. RAG pipeline name. Default: "reranking".
//     Valid values: "standard", "reranking", "raptor", "graph", "rig", "semantic".
//   - SessionID: Optional. Resume existing session by providing its ID.
//     If empty, a new session is created on first message.
//   - Personality: Optional. Output styling level. Default: from ux.GetPersonality().
//     Controls verbosity and formatting of CLI output.
//
// # Examples
//
//	config := RAGChatRunnerConfig{
//	    BaseURL:   "http://localhost:8080",
//	    Pipeline:  "reranking",
//	    SessionID: "",  // New session
//	}
//
//	// Resume existing session
//	config := RAGChatRunnerConfig{
//	    BaseURL:   "http://localhost:8080",
//	    SessionID: "sess-abc123",
//	}
//
// # Limitations
//
//   - No validation performed on struct creation (validated on runner creation)
//
// # Assumptions
//
//   - BaseURL points to a running orchestrator
//   - Pipeline name is valid if provided
type RAGChatRunnerConfig struct {
	BaseURL     string              // Orchestrator URL (required)
	Pipeline    string              // RAG pipeline name (optional, default: "reranking")
	SessionID   string              // Session ID to resume (optional)
	Personality ux.PersonalityLevel // Output styling (optional)
	StrictMode  bool                // Strict RAG mode: only answer from docs (default: true)
	Verbosity   int                 // Verified pipeline verbosity: 0=silent, 1=summary, 2=detailed (optional)
	DataSpace   string              // Data space to filter queries by (optional, e.g., "work", "personal")
	DocVersion  string              // Specific document version to query (optional, e.g., "v1", "report.md:v3")
}

// DirectChatRunnerConfig holds configuration for creating DirectChatRunner.
//
// # Description
//
// Groups all configuration needed to create a DirectChatRunner instance.
// Required fields must be provided; optional fields have sensible defaults.
//
// # Fields
//
//   - BaseURL: Required. Orchestrator URL (e.g., "http://localhost:8080").
//     Must not include trailing slash.
//   - SessionID: Optional. Session ID for resume. Unlike RAG, this is
//     client-side tracking only; server doesn't maintain session state.
//   - EnableThinking: Optional. Enable Claude extended thinking mode.
//     When true, Claude will show its reasoning process.
//   - BudgetTokens: Optional. Token budget for thinking mode.
//     Only used when EnableThinking is true. Default: 2048.
//   - Personality: Optional. Output styling level. Default: from ux.GetPersonality().
//
// # Examples
//
//	config := DirectChatRunnerConfig{
//	    BaseURL:        "http://localhost:8080",
//	    EnableThinking: true,
//	    BudgetTokens:   4096,
//	}
//
// # Limitations
//
//   - No validation performed on struct creation
//
// # Assumptions
//
//   - BaseURL points to a running orchestrator
//   - Extended thinking requires Claude model on backend
type DirectChatRunnerConfig struct {
	BaseURL        string              // Orchestrator URL (required)
	SessionID      string              // Session ID for resume (optional)
	EnableThinking bool                // Enable extended thinking (optional)
	BudgetTokens   int                 // Token budget for thinking (optional)
	Personality    ux.PersonalityLevel // Output styling (optional)
}

// =============================================================================
// Helper Functions
// =============================================================================

// isExitCommand checks if the input is an exit command.
//
// # Description
//
// Returns true if the input matches "exit" or "quit" (case-sensitive).
// Used by both RAGChatRunner and DirectChatRunner.
//
// # Inputs
//
//   - input: The user input string (should be trimmed)
//
// # Outputs
//
//   - bool: true if input is "exit" or "quit"
//
// # Examples
//
//	isExitCommand("exit")  // true
//	isExitCommand("quit")  // true
//	isExitCommand("EXIT")  // false (case-sensitive)
//	isExitCommand("hello") // false
//
// # Limitations
//
//   - Case-sensitive comparison
//
// # Assumptions
//
//   - Input is already trimmed
func isExitCommand(input string) bool {
	return input == "exit" || input == "quit"
}
