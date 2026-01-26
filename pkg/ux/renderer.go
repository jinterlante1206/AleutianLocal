// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package ux provides user experience components for the Aleutian CLI.
//
// This file contains stream renderers that display streaming events to
// various outputs (terminal, buffer, etc.).
//
// Single Responsibility:
//
//	Renderers ONLY render. They do not parse, read, or manage HTTP.
//	Each method handles exactly one event type, enabling clean composition.
//
// Renderer Types:
//
//   - TerminalStreamRenderer: Interactive terminal with spinners and colors
//   - MachineStreamRenderer: Machine-readable KEY: value format
//   - BufferStreamRenderer: In-memory buffer for testing
package ux

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// =============================================================================
// Helper Functions
// =============================================================================

// formatVersionInfo returns a formatted version string for a source document.
// Returns " v{N} (latest)" if current, " v{N}" if not current, or "" if no version info.
func formatVersionInfo(versionNumber *int, isCurrent *bool) string {
	if versionNumber == nil {
		return ""
	}
	if isCurrent != nil && *isCurrent {
		return fmt.Sprintf(" v%d (latest)", *versionNumber)
	}
	return fmt.Sprintf(" v%d", *versionNumber)
}

// =============================================================================
// Stream Renderer Interface
// =============================================================================

// StreamRenderer renders streaming events to an output destination.
//
// Each method handles exactly one event type. The renderer owns all
// output-related state (spinners, buffers, formatters). Callers should
// invoke methods in the order events are received.
//
// Thread Safety:
//
//	Implementations must be safe for concurrent calls. Multiple goroutines
//	may invoke methods simultaneously when processing events from channels.
//
// Lifecycle:
//
//  1. Create renderer with New*StreamRenderer()
//  2. Call On* methods as events arrive
//  3. Call Finalize() when stream ends (always, even on error)
//  4. Call Result() to get aggregated result
//
// Example:
//
//	renderer := NewTerminalStreamRenderer(os.Stdout, GetPersonality())
//	defer renderer.Finalize()
//
//	for event := range events {
//	    switch event.Type {
//	    case StreamEventToken:
//	        renderer.OnToken(ctx, event.Content)
//	    case StreamEventDone:
//	        renderer.OnDone(ctx, event.SessionID)
//	    }
//	}
//
//	result := renderer.Result()
type StreamRenderer interface {
	// OnStatus renders a status update (e.g., "Searching...").
	//
	// In interactive mode, may start or update a spinner.
	// In machine mode, prints "STATUS: message".
	//
	// Thread-safe. May be called concurrently with other methods.
	OnStatus(ctx context.Context, message string)

	// OnToken renders a single token from the LLM response.
	//
	// In interactive mode, prints immediately for streaming effect.
	// In machine mode, buffers until Finalize().
	//
	// Tokens should be rendered in order; out-of-order rendering
	// may produce garbled output.
	OnToken(ctx context.Context, token string)

	// OnThinking renders thinking/reasoning tokens (Claude extended thinking).
	//
	// May be styled differently (muted, collapsible) or hidden based on config.
	// In machine mode, buffers until Finalize().
	OnThinking(ctx context.Context, content string)

	// OnSources renders retrieved knowledge base sources inline.
	//
	// Called when sources event arrives (may be before, during, or after tokens).
	// In interactive mode, displays sources immediately for visibility.
	OnSources(ctx context.Context, sources []SourceInfo)

	// OnDone signals stream completion with optional session ID.
	//
	// Stops spinners, flushes buffers, prints final newlines.
	// This is typically the last On* method called (unless OnError).
	OnDone(ctx context.Context, sessionID string)

	// OnError renders an error that occurred during streaming.
	//
	// Stops spinners and displays error message.
	// After OnError, only Finalize() should be called.
	OnError(ctx context.Context, err error)

	// Finalize performs cleanup (stop spinners, flush output).
	//
	// MUST be called when streaming ends, even if abnormally.
	// Safe to call multiple times; subsequent calls are no-ops.
	// Typically called with defer immediately after creating renderer.
	Finalize()

	// Result returns the accumulated result after streaming completes.
	//
	// Contains the full answer, sources, session ID, and metadata.
	// May be called before Finalize() to get partial results.
	Result() *StreamResult
}

// =============================================================================
// Terminal Stream Renderer
// =============================================================================

// terminalStreamRenderer renders streaming events to an interactive terminal.
//
// This is the primary renderer for user-facing output. It provides a rich
// experience with spinners, colors, and real-time token streaming.
//
// Features:
//   - Spinners for status updates (stops automatically when tokens arrive)
//   - Real-time token streaming (each token printed as it arrives)
//   - Styled output based on personality level
//   - Inline source display for RAG responses
//   - Muted styling for thinking/reasoning content
//
// Personality Modes:
//
//   - PersonalityFull: Rich styling with colors, boxes, and icons
//   - PersonalityMinimal: Plain text with basic formatting
//   - PersonalityMachine: KEY: value format for scripting
//
// Thread Safety:
//
//	All methods are protected by a mutex. Safe for concurrent calls.
//
// Fields:
//   - writer: Output destination (typically os.Stdout)
//   - personality: Controls output styling
//   - spinner: Current spinner instance (nil if not spinning)
//   - result: Accumulated result with metrics
//   - answerBuilder: Accumulates token content
//   - thinkingBuilder: Accumulates thinking content
//   - hasWrittenToken: Tracks if first token has been written
//   - finalized: Prevents operations after Finalize()
type terminalStreamRenderer struct {
	writer      io.Writer
	personality PersonalityLevel
	spinner     *Spinner
	result      *StreamResult
	mu          sync.Mutex

	// State tracking
	answerBuilder   strings.Builder
	thinkingBuilder strings.Builder
	hasWrittenToken bool
	finalized       bool
}

// NewTerminalStreamRenderer creates a renderer for interactive terminal output.
//
// This constructor creates a renderer optimized for interactive terminal use.
// It supports spinners, colors, and real-time streaming based on personality.
//
// Parameters:
//   - w: The output writer. If nil, defaults to os.Stdout.
//   - personality: Controls output styling. Use GetPersonality().Level for
//     the user's configured personality, or hardcode for specific behavior.
//
// Returns:
//
//	A StreamRenderer that displays events interactively. The returned renderer
//	has an Id and CreatedAt already set on its internal result.
//
// Example:
//
//	// Use user's configured personality
//	renderer := NewTerminalStreamRenderer(os.Stdout, GetPersonality().Level)
//	defer renderer.Finalize()
//
//	// Force machine-readable output
//	renderer := NewTerminalStreamRenderer(os.Stdout, PersonalityMachine)
func NewTerminalStreamRenderer(w io.Writer, personality PersonalityLevel) StreamRenderer {
	if w == nil {
		w = os.Stdout
	}
	return &terminalStreamRenderer{
		writer:      w,
		personality: personality,
		result: &StreamResult{
			Id:        uuid.New().String(),
			CreatedAt: time.Now().UnixMilli(),
		},
	}
}

// OnStatus renders a status update message.
//
// Behavior by personality:
//   - PersonalityFull/Minimal: Starts or updates a spinner with the message.
//     The spinner runs until the first token arrives or stream ends.
//   - PersonalityMachine: Prints "STATUS: {message}\n" immediately.
//
// Thread Safety:
//
//	Protected by mutex. Safe to call concurrently with other methods.
//
// Parameters:
//   - ctx: Context for cancellation (currently unused, reserved for future)
//   - message: The status message to display (e.g., "Searching knowledge base...")
//
// Side Effects:
//   - Increments TotalEvents in result
//   - May start/update spinner (interactive modes)
//   - May print to writer (machine mode)
//
// Example status messages:
//   - "Connecting to model..."
//   - "Retrieving documents..."
//   - "Generating response..."
func (r *terminalStreamRenderer) OnStatus(ctx context.Context, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.finalized {
		return
	}

	r.result.TotalEvents++

	if r.personality == PersonalityMachine {
		fmt.Fprintf(r.writer, "STATUS: %s\n", message)
		return
	}

	// Start or update spinner
	if r.spinner == nil {
		r.spinner = NewSpinner(message)
		r.spinner.Start()
	} else {
		r.spinner.UpdateMessage(message)
	}
}

// OnToken renders a single token from the LLM response.
//
// Behavior by personality:
//   - PersonalityFull/Minimal: Prints the token immediately to the writer,
//     creating a streaming effect. Stops any running spinner on first token.
//   - PersonalityMachine: Buffers the token. All tokens are printed as a
//     single "ANSWER: {content}" line when OnDone is called.
//
// Thread Safety:
//
//	Protected by mutex. Safe to call concurrently, but tokens should arrive
//	in order for coherent output.
//
// Parameters:
//   - ctx: Context for cancellation (currently unused, reserved for future)
//   - token: The token text to render. May be a single character, word, or
//     partial word depending on the LLM's tokenization.
//
// Side Effects:
//   - Sets FirstTokenAt on first call (for time-to-first-token metrics)
//   - Stops spinner on first call (interactive modes)
//   - Prints newline after spinner (interactive modes)
//   - Increments TotalTokens and TotalEvents in result
//   - Appends to answer buffer
//   - May print to writer (interactive modes)
//
// Example:
//
//	// Tokens might arrive as: "The", " capital", " of", " France", " is", " Paris", "."
//	for _, token := range tokens {
//	    renderer.OnToken(ctx, token)
//	}
func (r *terminalStreamRenderer) OnToken(ctx context.Context, token string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.finalized {
		return
	}

	// Track first token timing
	if !r.hasWrittenToken {
		r.result.FirstTokenAt = time.Now().UnixMilli()
		r.hasWrittenToken = true

		// Stop spinner when first token arrives
		if r.spinner != nil {
			r.spinner.Stop()
			r.spinner = nil
			if r.personality != PersonalityMachine {
				fmt.Fprintln(r.writer) // New line after spinner
			}
		}
	}

	r.answerBuilder.WriteString(token)
	r.result.TotalTokens++
	r.result.TotalEvents++

	if r.personality == PersonalityMachine {
		// In machine mode, buffer until done
		return
	}

	// Print token immediately for streaming effect
	fmt.Fprint(r.writer, token)
}

// OnThinking renders thinking/reasoning tokens from Claude extended thinking.
//
// Thinking tokens represent the model's internal reasoning process. They are
// displayed differently from answer tokens to distinguish reasoning from output.
//
// Behavior by personality:
//   - PersonalityFull: Prints thinking in muted (gray) styling inline.
//     Stops any running spinner before printing.
//   - PersonalityMinimal: Prints thinking in muted styling inline.
//   - PersonalityMachine: Buffers thinking. Printed as "THINKING: {content}"
//     when OnDone is called.
//
// Thread Safety:
//
//	Protected by mutex. Safe to call concurrently.
//
// Parameters:
//   - ctx: Context for cancellation (currently unused, reserved for future)
//   - content: The thinking content to render. May contain the model's
//     reasoning, analysis, or intermediate thoughts.
//
// Side Effects:
//   - Stops spinner if running (interactive modes)
//   - Increments ThinkingTokens and TotalEvents in result
//   - Appends to thinking buffer
//   - May print to writer (interactive modes)
//
// Example:
//
//	// Thinking might be: "Let me analyze this question step by step..."
//	renderer.OnThinking(ctx, "Let me analyze this question step by step...")
func (r *terminalStreamRenderer) OnThinking(ctx context.Context, content string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.finalized {
		return
	}

	r.thinkingBuilder.WriteString(content)
	r.result.ThinkingTokens++
	r.result.TotalEvents++

	if r.personality == PersonalityMachine {
		// Buffer in machine mode
		return
	}

	// In interactive mode, show thinking inline (muted)
	// Stop spinner if running
	if r.spinner != nil {
		r.spinner.Stop()
		r.spinner = nil
		fmt.Fprintln(r.writer)
	}

	// Print thinking content in muted style
	fmt.Fprint(r.writer, Styles.Muted.Render(content))
}

// OnSources renders retrieved knowledge base sources inline.
//
// Sources are displayed as they arrive, providing immediate visibility into
// what documents the RAG system retrieved. This helps users understand the
// basis for the answer.
//
// Behavior by personality:
//   - PersonalityFull: Displays sources in a styled box with title and scores.
//     Stops any running spinner before displaying.
//   - PersonalityMinimal: Displays a numbered list of source names.
//   - PersonalityMachine: Prints each source as "SOURCE: {name} score={score}"
//     or "SOURCE: {name} distance={distance}".
//
// Thread Safety:
//
//	Protected by mutex. Safe to call concurrently.
//
// Parameters:
//   - ctx: Context for cancellation (currently unused, reserved for future)
//   - sources: The sources to render. Each SourceInfo contains the source
//     name and relevance score/distance.
//
// Side Effects:
//   - Appends sources to result.Sources
//   - Stops spinner if running (interactive modes)
//   - Increments TotalEvents in result
//   - Prints to writer immediately
//
// Example:
//
//	sources := []SourceInfo{
//	    {Source: "architecture.pdf", Score: 0.95},
//	    {Source: "api-docs.md", Score: 0.87},
//	}
//	renderer.OnSources(ctx, sources)
func (r *terminalStreamRenderer) OnSources(ctx context.Context, sources []SourceInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.finalized {
		return
	}

	r.result.Sources = append(r.result.Sources, sources...)
	r.result.TotalEvents++

	if len(sources) == 0 {
		return
	}

	if r.personality == PersonalityMachine {
		// Print sources immediately in machine format
		for _, src := range sources {
			versionInfo := formatVersionInfo(src.VersionNumber, src.IsCurrent)
			if src.Score != 0 {
				fmt.Fprintf(r.writer, "SOURCE: %s%s score=%.4f\n", src.Source, versionInfo, src.Score)
			} else if src.Distance != 0 {
				fmt.Fprintf(r.writer, "SOURCE: %s%s distance=%.4f\n", src.Source, versionInfo, src.Distance)
			} else {
				fmt.Fprintf(r.writer, "SOURCE: %s%s\n", src.Source, versionInfo)
			}
		}
		return
	}

	// Stop spinner if running to show sources
	if r.spinner != nil {
		r.spinner.Stop()
		r.spinner = nil
	}

	// In minimal mode, just list sources
	if r.personality == PersonalityMinimal {
		fmt.Fprintln(r.writer)
		fmt.Fprintln(r.writer, "Sources:")
		for i, src := range sources {
			versionInfo := formatVersionInfo(src.VersionNumber, src.IsCurrent)
			fmt.Fprintf(r.writer, "  %d. %s%s\n", i+1, src.Source, versionInfo)
		}
		fmt.Fprintln(r.writer)
		return
	}

	// Full personality - styled inline display
	fmt.Fprintln(r.writer)
	var content strings.Builder
	for i, src := range sources {
		// Build version info string
		versionInfo := formatVersionInfo(src.VersionNumber, src.IsCurrent)
		versionStyled := ""
		if versionInfo != "" {
			versionStyled = Styles.Muted.Render(versionInfo)
		}

		// Build score info string
		scoreInfo := ""
		if src.Score != 0 {
			scoreInfo = Styles.Muted.Render(fmt.Sprintf(" (%.2f)", src.Score))
		} else if src.Distance != 0 {
			scoreInfo = Styles.Muted.Render(fmt.Sprintf(" (%.2f)", src.Distance))
		}
		content.WriteString(fmt.Sprintf("%d. %s%s%s", i+1, src.Source, versionStyled, scoreInfo))
		if i < len(sources)-1 {
			content.WriteString("\n")
		}
	}
	boxStyle := Styles.InfoBox.Width(60)
	titleLine := Styles.Subtitle.Render("Retrieved Sources")
	fmt.Fprintln(r.writer, boxStyle.Render(titleLine+"\n"+content.String()))
	fmt.Fprintln(r.writer)
}

// OnDone signals successful stream completion.
//
// This method is called when the stream ends normally (not due to error).
// It finalizes output, flushes buffers, and records the session ID.
//
// Behavior by personality:
//   - PersonalityFull/Minimal: Stops any spinner, ensures output ends with
//     a newline for clean terminal state.
//   - PersonalityMachine: Prints buffered answer as "ANSWER: {content}",
//     buffered thinking as "THINKING: {content}", session as "SESSION: {id}",
//     and finally "DONE".
//
// Thread Safety:
//
//	Protected by mutex. Safe to call concurrently.
//
// Parameters:
//   - ctx: Context for cancellation (currently unused, reserved for future)
//   - sessionID: The session identifier for multi-turn conversations.
//     May be empty if session tracking is not used.
//
// Side Effects:
//   - Sets SessionID and CompletedAt in result
//   - Stops spinner if running
//   - Increments TotalEvents in result
//   - Prints to writer (especially in machine mode)
//
// After Calling:
//
//	Only Finalize() and Result() should be called. Further On* calls are ignored.
func (r *terminalStreamRenderer) OnDone(ctx context.Context, sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.finalized {
		return
	}

	r.result.SessionID = sessionID
	r.result.CompletedAt = time.Now().UnixMilli()
	r.result.TotalEvents++

	// Stop spinner if still running
	if r.spinner != nil {
		r.spinner.Stop()
		r.spinner = nil
	}

	if r.personality == PersonalityMachine {
		// Print buffered answer
		answer := r.answerBuilder.String()
		if answer != "" {
			fmt.Fprintf(r.writer, "ANSWER: %s\n", answer)
		}
		thinking := r.thinkingBuilder.String()
		if thinking != "" {
			fmt.Fprintf(r.writer, "THINKING: %s\n", thinking)
		}
		if sessionID != "" {
			fmt.Fprintf(r.writer, "SESSION: %s\n", sessionID)
		}
		fmt.Fprintln(r.writer, "DONE")
	} else {
		// Ensure we end with a newline
		answer := r.answerBuilder.String()
		if answer != "" && !strings.HasSuffix(answer, "\n") {
			fmt.Fprintln(r.writer)
		}
	}
}

// OnError renders an error that occurred during streaming.
//
// This method is called when the stream ends due to an error. It displays
// the error to the user and records it in the result.
//
// Behavior by personality:
//   - PersonalityFull: Displays error with error icon and red styling.
//   - PersonalityMinimal: Displays error with error icon.
//   - PersonalityMachine: Prints "ERROR: {message}".
//
// Thread Safety:
//
//	Protected by mutex. Safe to call concurrently.
//
// Parameters:
//   - ctx: Context for cancellation (currently unused, reserved for future)
//   - err: The error that occurred. Error message is stored in result.Error.
//
// Side Effects:
//   - Sets Error and CompletedAt in result
//   - Stops spinner if running
//   - Increments TotalEvents in result
//   - Prints error to writer
//
// After Calling:
//
//	Only Finalize() and Result() should be called. Further On* calls are ignored.
//
// Example:
//
//	if streamErr != nil {
//	    renderer.OnError(ctx, streamErr)
//	}
func (r *terminalStreamRenderer) OnError(ctx context.Context, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.finalized {
		return
	}

	r.result.Error = err.Error()
	r.result.CompletedAt = time.Now().UnixMilli()
	r.result.TotalEvents++

	// Stop spinner
	if r.spinner != nil {
		r.spinner.Stop()
		r.spinner = nil
	}

	if r.personality == PersonalityMachine {
		fmt.Fprintf(r.writer, "ERROR: %v\n", err)
	} else {
		fmt.Fprintf(r.writer, "\n%s %s\n",
			IconError.Render(),
			Styles.Error.Render(fmt.Sprintf("Stream error: %v", err)))
	}
}

// Finalize performs cleanup and marks the renderer as complete.
//
// This method MUST be called when streaming ends, regardless of whether
// it ended normally (OnDone) or with an error (OnError). It's safe to call
// multiple times; subsequent calls are no-ops.
//
// Actions performed:
//   - Stops any running spinner
//   - Finalizes the answer and thinking buffers into result
//   - Sets CompletedAt if not already set
//   - Marks the renderer as finalized (ignores further On* calls)
//
// Thread Safety:
//
//	Protected by mutex. Safe to call concurrently or multiple times.
//
// Typical Usage:
//
//	renderer := NewTerminalStreamRenderer(os.Stdout, personality)
//	defer renderer.Finalize() // Always call, even on panic
//
//	// ... process events ...
//
// Side Effects:
//   - Sets finalized flag to true
//   - Stops spinner if running
//   - Populates Answer and Thinking in result from builders
//   - Sets CompletedAt if zero
func (r *terminalStreamRenderer) Finalize() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.finalized {
		return
	}
	r.finalized = true

	// Stop spinner if still running
	if r.spinner != nil {
		r.spinner.Stop()
		r.spinner = nil
	}

	// Finalize result
	r.result.Answer = r.answerBuilder.String()
	r.result.Thinking = r.thinkingBuilder.String()
	if r.result.CompletedAt == 0 {
		r.result.CompletedAt = time.Now().UnixMilli()
	}
}

// Result returns the accumulated StreamResult.
//
// This method returns the result of the streaming operation, containing:
//   - Answer: Complete response text (all tokens concatenated)
//   - Thinking: Complete thinking text (if extended thinking was used)
//   - Sources: All retrieved sources
//   - SessionID: Session identifier for multi-turn conversations
//   - Error: Error message if stream failed
//   - Metrics: TotalTokens, ThinkingTokens, TotalEvents, timing data
//
// Thread Safety:
//
//	Protected by mutex. Safe to call concurrently. Returns a copy of the
//	result to prevent race conditions with ongoing rendering.
//
// Timing:
//
//	May be called before Finalize() to get partial results during streaming.
//	Call after Finalize() for the complete final result.
//
// Returns:
//
//	A pointer to a StreamResult containing all accumulated data. The returned
//	result is a copy; modifications do not affect the renderer's internal state.
//
// Example:
//
//	renderer.Finalize()
//	result := renderer.Result()
//	fmt.Printf("Answer: %s\n", result.Answer)
//	fmt.Printf("Tokens: %d\n", result.TotalTokens)
//	fmt.Printf("Duration: %v\n", result.Duration())
func (r *terminalStreamRenderer) Result() *StreamResult {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Copy result to avoid race conditions
	result := *r.result
	result.Answer = r.answerBuilder.String()
	result.Thinking = r.thinkingBuilder.String()
	return &result
}

// =============================================================================
// Buffer Stream Renderer (for testing)
// =============================================================================

// bufferStreamRenderer renders to an in-memory buffer for testing.
//
// This renderer captures all events without side effects, making it ideal
// for unit tests where you need to verify renderer behavior without
// terminal output.
//
// Features:
//   - No terminal output or spinners
//   - Captures all events in order
//   - Provides Events() method to inspect captured events
//   - Thread-safe for concurrent testing
//
// Fields:
//   - result: Accumulated result with metrics
//   - events: Slice of all captured events (in order)
//   - answerBuilder: Accumulates token content
//   - thinkingBuilder: Accumulates thinking content
//   - finalized: Prevents operations after Finalize()
type bufferStreamRenderer struct {
	result    *StreamResult
	events    []StreamEvent
	mu        sync.Mutex
	finalized bool

	answerBuilder   strings.Builder
	thinkingBuilder strings.Builder
}

// NewBufferStreamRenderer creates a renderer that buffers events to memory.
//
// This constructor creates a renderer for testing purposes. It captures all
// events without producing any output, allowing tests to verify event
// processing logic.
//
// Returns:
//
//	A StreamRenderer that captures events for later inspection. The returned
//	renderer has an Id and CreatedAt already set on its internal result.
//
// Example:
//
//	renderer := NewBufferStreamRenderer()
//	defer renderer.Finalize()
//
//	renderer.OnToken(ctx, "Hello")
//	renderer.OnToken(ctx, " world")
//	renderer.OnDone(ctx, "sess-123")
//
//	result := renderer.Result()
//	if result.Answer != "Hello world" {
//	    t.Error("unexpected answer")
//	}
//
//	// Inspect individual events
//	bufRenderer := renderer.(*bufferStreamRenderer)
//	events := bufRenderer.Events()
//	if len(events) != 3 {
//	    t.Errorf("expected 3 events, got %d", len(events))
//	}
func NewBufferStreamRenderer() StreamRenderer {
	return &bufferStreamRenderer{
		result: &StreamResult{
			Id:        uuid.New().String(),
			CreatedAt: time.Now().UnixMilli(),
		},
		events: make([]StreamEvent, 0),
	}
}

// OnStatus captures a status event to the buffer.
//
// Creates a status event and appends it to the events slice.
// No output is produced.
//
// Thread Safety:
//
//	Protected by mutex. Safe to call concurrently.
//
// Parameters:
//   - ctx: Context (unused in buffer renderer)
//   - message: The status message to capture
//
// Side Effects:
//   - Appends event to events slice
//   - Increments TotalEvents in result
func (r *bufferStreamRenderer) OnStatus(ctx context.Context, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.finalized {
		return
	}

	r.events = append(r.events, NewStatusEvent(message))
	r.result.TotalEvents++
}

// OnToken captures a token event to the buffer.
//
// Creates a token event, appends it to events, and accumulates the
// token content in the answer builder.
//
// Thread Safety:
//
//	Protected by mutex. Safe to call concurrently.
//
// Parameters:
//   - ctx: Context (unused in buffer renderer)
//   - token: The token content to capture
//
// Side Effects:
//   - Sets FirstTokenAt on first call
//   - Appends token to answer builder
//   - Appends event to events slice
//   - Increments TotalTokens and TotalEvents in result
func (r *bufferStreamRenderer) OnToken(ctx context.Context, token string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.finalized {
		return
	}

	if r.result.FirstTokenAt == 0 {
		r.result.FirstTokenAt = time.Now().UnixMilli()
	}

	r.answerBuilder.WriteString(token)
	r.events = append(r.events, NewTokenEvent(token))
	r.result.TotalTokens++
	r.result.TotalEvents++
}

// OnThinking captures a thinking event to the buffer.
//
// Creates a thinking event, appends it to events, and accumulates the
// thinking content in the thinking builder.
//
// Thread Safety:
//
//	Protected by mutex. Safe to call concurrently.
//
// Parameters:
//   - ctx: Context (unused in buffer renderer)
//   - content: The thinking content to capture
//
// Side Effects:
//   - Appends content to thinking builder
//   - Appends event to events slice
//   - Increments ThinkingTokens and TotalEvents in result
func (r *bufferStreamRenderer) OnThinking(ctx context.Context, content string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.finalized {
		return
	}

	r.thinkingBuilder.WriteString(content)
	r.events = append(r.events, NewThinkingEvent(content))
	r.result.ThinkingTokens++
	r.result.TotalEvents++
}

// OnSources captures a sources event to the buffer.
//
// Creates a sources event and appends it to events. Also appends sources
// to the result's Sources slice.
//
// Thread Safety:
//
//	Protected by mutex. Safe to call concurrently.
//
// Parameters:
//   - ctx: Context (unused in buffer renderer)
//   - sources: The sources to capture
//
// Side Effects:
//   - Appends sources to result.Sources
//   - Appends event to events slice
//   - Increments TotalEvents in result
func (r *bufferStreamRenderer) OnSources(ctx context.Context, sources []SourceInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.finalized {
		return
	}

	r.result.Sources = append(r.result.Sources, sources...)
	r.events = append(r.events, NewSourcesEvent(sources))
	r.result.TotalEvents++
}

// OnDone captures a done event to the buffer.
//
// Creates a done event and records stream completion.
//
// Thread Safety:
//
//	Protected by mutex. Safe to call concurrently.
//
// Parameters:
//   - ctx: Context (unused in buffer renderer)
//   - sessionID: The session identifier to capture
//
// Side Effects:
//   - Sets SessionID and CompletedAt in result
//   - Appends event to events slice
//   - Increments TotalEvents in result
func (r *bufferStreamRenderer) OnDone(ctx context.Context, sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.finalized {
		return
	}

	r.result.SessionID = sessionID
	r.result.CompletedAt = time.Now().UnixMilli()
	r.events = append(r.events, NewDoneEvent(sessionID))
	r.result.TotalEvents++
}

// OnError captures an error event to the buffer.
//
// Creates an error event and records the error.
//
// Thread Safety:
//
//	Protected by mutex. Safe to call concurrently.
//
// Parameters:
//   - ctx: Context (unused in buffer renderer)
//   - err: The error to capture
//
// Side Effects:
//   - Sets Error and CompletedAt in result
//   - Appends event to events slice
//   - Increments TotalEvents in result
func (r *bufferStreamRenderer) OnError(ctx context.Context, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.finalized {
		return
	}

	r.result.Error = err.Error()
	r.result.CompletedAt = time.Now().UnixMilli()
	r.events = append(r.events, NewErrorEvent(err.Error()))
	r.result.TotalEvents++
}

// Finalize marks the buffer renderer as complete.
//
// Finalizes the answer and thinking buffers into the result.
// Safe to call multiple times.
//
// Thread Safety:
//
//	Protected by mutex. Safe to call concurrently.
//
// Side Effects:
//   - Sets finalized flag to true
//   - Populates Answer and Thinking in result from builders
//   - Sets CompletedAt if zero
func (r *bufferStreamRenderer) Finalize() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.finalized {
		return
	}
	r.finalized = true

	r.result.Answer = r.answerBuilder.String()
	r.result.Thinking = r.thinkingBuilder.String()
	if r.result.CompletedAt == 0 {
		r.result.CompletedAt = time.Now().UnixMilli()
	}
}

// Result returns the accumulated StreamResult.
//
// Returns a copy of the result to prevent race conditions.
//
// Thread Safety:
//
//	Protected by mutex. Safe to call concurrently.
//
// Returns:
//
//	A pointer to a StreamResult containing all accumulated data.
func (r *bufferStreamRenderer) Result() *StreamResult {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := *r.result
	result.Answer = r.answerBuilder.String()
	result.Thinking = r.thinkingBuilder.String()
	return &result
}

// Events returns all captured events for testing inspection.
//
// This method is specific to bufferStreamRenderer and not part of the
// StreamRenderer interface. Cast the renderer to access it.
//
// Thread Safety:
//
//	Protected by mutex. Returns a copy to prevent race conditions.
//
// Returns:
//
//	A slice containing copies of all captured events in order.
//
// Example:
//
//	bufRenderer := renderer.(*bufferStreamRenderer)
//	events := bufRenderer.Events()
//	for i, event := range events {
//	    fmt.Printf("Event %d: %s\n", i, event.Type)
//	}
func (r *bufferStreamRenderer) Events() []StreamEvent {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Return a copy to avoid race conditions
	events := make([]StreamEvent, len(r.events))
	copy(events, r.events)
	return events
}

// =============================================================================
// Convenience Functions
// =============================================================================

// RenderStreamToResult is a convenience function that reads a stream and
// returns the aggregated result.
//
// This function combines StreamReader and internal buffering into a single
// call. Use for simple cases where you just need the final result without
// custom rendering.
//
// Parameters:
//   - ctx: Context for cancellation. When cancelled, reading stops.
//   - reader: StreamReader to use for parsing the stream format.
//   - source: io.Reader containing the stream data. Caller is responsible
//     for closing this reader.
//
// Returns:
//   - *StreamResult: The aggregated result containing answer, sources, etc.
//   - error: Non-nil if reading failed (parse error, context cancelled, etc.)
//
// Example:
//
//	reader := NewSSEStreamReader(NewSSEParser())
//	result, err := RenderStreamToResult(ctx, reader, httpResp.Body)
//	if err != nil {
//	    return err
//	}
//	fmt.Println(result.Answer)
func RenderStreamToResult(ctx context.Context, reader StreamReader, source io.Reader) (*StreamResult, error) {
	return reader.ReadAll(ctx, source)
}

// =============================================================================
// Compile-time Interface Checks
// =============================================================================

var _ StreamRenderer = (*terminalStreamRenderer)(nil)
var _ StreamRenderer = (*bufferStreamRenderer)(nil)
