// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

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
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/pkg/ux"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
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

// PromptingInputReader extends InputReader with prompt display capability.
//
// # Description
//
// PromptingInputReader is implemented by input readers that handle their own
// prompt display (like interactive readers with bubbletea). The chat runner
// checks for this interface to avoid double-prompting.
//
// # Usage
//
//	if p, ok := reader.(PromptingInputReader); ok {
//	    p.SetPrompt(promptString)
//	    // Reader will display prompt
//	} else {
//	    fmt.Print(promptString)
//	    // Manually display prompt
//	}
//	line, err := reader.ReadLine()
type PromptingInputReader interface {
	InputReader
	// SetPrompt sets the prompt string to display before input.
	SetPrompt(prompt string)
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
// InteractiveInputReader Implementation (with history)
// =============================================================================

// InteractiveInputReader implements InputReader with history navigation.
//
// # Description
//
// InteractiveInputReader uses charmbracelet/bubbletea to provide an interactive
// input experience with:
//   - Up/down arrow history navigation
//   - Line editing (Ctrl+A, Ctrl+E, etc.)
//   - Proper terminal handling
//
// Falls back to StdinReader for non-TTY environments (piped input, CI/CD).
//
// # Fields
//
//   - history: Slice of previous inputs (most recent last)
//   - historyIndex: Current position when navigating history (-1 = new input)
//   - maxHistory: Maximum number of history entries to keep
//   - prompt: The prompt string to display
//
// # Thread Safety
//
// Not thread-safe. Single reader per stdin. Do not share across goroutines.
//
// # Limitations
//
//   - History is in-memory only (not persisted across sessions)
//   - Maximum history size is configurable (default: 50)
//
// # Assumptions
//
//   - Terminal supports ANSI escape codes
//   - os.Stdin is a TTY for interactive mode
type InteractiveInputReader struct {
	history      []string
	historyIndex int
	maxHistory   int
	prompt       string
}

// inputModel is the bubbletea model for interactive input.
type inputModel struct {
	textInput    textinput.Model
	history      []string
	historyIndex int
	currentInput string // Stores current input when navigating history
	done         bool
	cancelled    bool
}

// NewInteractiveInputReader creates an interactive input reader with history.
//
// # Description
//
// Creates an InteractiveInputReader that provides up-arrow history navigation
// and line editing. If stdin is not a TTY, returns a StdinReader instead.
//
// Note: This reader does NOT display a prompt - the caller (chat runner)
// is responsible for displaying the prompt before calling ReadLine().
//
// # Inputs
//
//   - maxHistory: Maximum number of history entries to keep
//
// # Outputs
//
//   - InputReader: Interactive reader if TTY, StdinReader otherwise
//
// # Examples
//
//	reader := NewInteractiveInputReader(50)
//	for {
//	    fmt.Print("> ") // Caller displays prompt
//	    line, err := reader.ReadLine()
//	    if err == io.EOF {
//	        break
//	    }
//	    fmt.Println("You said:", line)
//	}
//
// # Limitations
//
//   - Falls back to basic input for non-TTY
//
// # Assumptions
//
//   - Terminal supports ANSI escape codes for interactive mode
func NewInteractiveInputReader(maxHistory int) InputReader {
	// Fall back to basic stdin reader for non-TTY (piped input, CI/CD)
	if !isatty.IsTerminal(os.Stdin.Fd()) && !isatty.IsCygwinTerminal(os.Stdin.Fd()) {
		return NewStdinReader()
	}

	return &InteractiveInputReader{
		history:      make([]string, 0, maxHistory),
		historyIndex: -1,
		maxHistory:   maxHistory,
		prompt:       "> ", // Default prompt, can be overridden via SetPrompt
	}
}

// SetPrompt sets the prompt string to display before input.
//
// # Description
//
// Implements PromptingInputReader interface. The prompt will be displayed
// by the bubbletea textinput component.
//
// # Inputs
//
//   - prompt: The prompt string (e.g., "> ")
func (r *InteractiveInputReader) SetPrompt(prompt string) {
	r.prompt = prompt
}

// ReadLine reads a single line with interactive history support.
//
// # Description
//
// Displays a prompt and reads user input with support for:
//   - Up arrow: Previous history entry
//   - Down arrow: Next history entry
//   - Enter: Submit input
//   - Ctrl+C: Cancel current input (returns empty string)
//   - Ctrl+D: EOF (returns io.EOF)
//
// Successfully submitted non-empty inputs are added to history.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - string: The line read, with leading/trailing whitespace trimmed
//   - error: io.EOF on Ctrl+D, or other error
//
// # Limitations
//
//   - Blocks until input is submitted
//
// # Assumptions
//
//   - Terminal is in a usable state
func (r *InteractiveInputReader) ReadLine() (string, error) {
	// Create text input model
	ti := textinput.New()
	ti.Prompt = r.prompt
	ti.Focus()
	ti.CharLimit = 4096
	ti.Width = 80

	m := inputModel{
		textInput:    ti,
		history:      r.history,
		historyIndex: -1,
		currentInput: "",
		done:         false,
		cancelled:    false,
	}

	// Run the bubbletea program
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	// Defensive type assertion - finalModel should never be nil when err is nil,
	// but we check anyway to prevent potential panic
	result, ok := finalModel.(inputModel)
	if !ok {
		return "", fmt.Errorf("unexpected model type from bubbletea: %T", finalModel)
	}

	// Handle Ctrl+D (EOF)
	if result.cancelled && result.textInput.Value() == "" {
		return "", io.EOF
	}

	// Get the final input value
	input := strings.TrimSpace(result.textInput.Value())

	// Add non-empty inputs to history
	if input != "" {
		r.addToHistory(input)
	}

	return input, nil
}

// addToHistory adds an input to the history buffer.
func (r *InteractiveInputReader) addToHistory(input string) {
	// Don't add duplicates of the most recent entry
	if len(r.history) > 0 && r.history[len(r.history)-1] == input {
		return
	}

	r.history = append(r.history, input)

	// Trim history if it exceeds max size
	if len(r.history) > r.maxHistory {
		r.history = r.history[1:]
	}
}

// Init initializes the bubbletea model.
func (m inputModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles input events for the bubbletea model.
func (m inputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			m.done = true
			return m, tea.Quit

		case tea.KeyCtrlC:
			// Clear input and return empty
			m.textInput.SetValue("")
			m.done = true
			return m, tea.Quit

		case tea.KeyCtrlD:
			// EOF - signal to exit
			m.cancelled = true
			m.textInput.SetValue("")
			m.done = true
			return m, tea.Quit

		case tea.KeyUp:
			// Navigate to previous history entry
			if len(m.history) == 0 {
				return m, nil
			}

			// Save current input when first entering history
			if m.historyIndex == -1 {
				m.currentInput = m.textInput.Value()
				m.historyIndex = len(m.history) - 1
			} else if m.historyIndex > 0 {
				m.historyIndex--
			}

			m.textInput.SetValue(m.history[m.historyIndex])
			m.textInput.CursorEnd()
			return m, nil

		case tea.KeyDown:
			// Navigate to next history entry
			if m.historyIndex == -1 {
				return m, nil
			}

			if m.historyIndex < len(m.history)-1 {
				m.historyIndex++
				m.textInput.SetValue(m.history[m.historyIndex])
			} else {
				// Return to current input
				m.historyIndex = -1
				m.textInput.SetValue(m.currentInput)
			}
			m.textInput.CursorEnd()
			return m, nil
		}
	}

	// Handle other input
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// View renders the input prompt.
func (m inputModel) View() string {
	if m.done {
		return ""
	}
	return m.textInput.View()
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
	SessionTTL  string              // Session TTL (optional, e.g., "24h", "7d"). Resets on each message.
	RecencyBias string              // Recency bias preset (optional): none, gentle, moderate, aggressive
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
	SessionTTL     string              // Session TTL (optional, e.g., "24h", "7d"). Not used in direct mode.
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
