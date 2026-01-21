// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package ux

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// ChatMode represents the chat operation mode
type ChatMode int

const (
	ChatModeRAG ChatMode = iota
	ChatModeDirect
)

// SessionStats aggregates metrics from a chat session for display.
//
// # Description
//
// SessionStats captures accumulated metrics across all exchanges in a
// chat session. It's designed to be displayed when the session ends,
// giving users visibility into their session's performance and usage.
//
// # Fields
//
//   - MessageCount: Number of user messages sent
//   - TotalTokens: Total tokens generated across all responses
//   - ThinkingTokens: Total thinking tokens (Claude extended thinking)
//   - SourcesUsed: Number of unique sources referenced
//   - Duration: Total session duration
//   - FirstResponseLatency: Time to first token of first response
//   - AverageResponseTime: Average time per response
type SessionStats struct {
	MessageCount         int
	TotalTokens          int
	ThinkingTokens       int
	SourcesUsed          int
	Duration             time.Duration
	FirstResponseLatency time.Duration
	AverageResponseTime  time.Duration
}

// SourceInfo represents a source citation from RAG retrieval.
//
// # Description
//
// SourceInfo captures metadata about a document retrieved during RAG
// processing. Each source has a unique ID and timestamp for database
// storage and audit trails.
//
// # Fields
//
//   - Id: Unique identifier for this source record (UUID v4).
//   - CreatedAt: Unix timestamp in milliseconds when source was retrieved.
//   - Source: Document name, path, or URL identifying the source.
//   - Distance: Vector distance (lower = more similar). Used by some pipelines.
//   - Score: Relevance score (higher = more relevant). Used by reranking pipelines.
//   - Hash: SHA-256 hash of source content for tamper detection.
//   - VersionNumber: Document version (1, 2, 3...). Nil for legacy docs.
//   - IsCurrent: True if this is the latest version. Nil for legacy docs.
type SourceInfo struct {
	Id            string  `json:"id,omitempty"`
	CreatedAt     int64   `json:"created_at,omitempty"`
	Source        string  `json:"source"`
	Distance      float64 `json:"distance,omitempty"`
	Score         float64 `json:"score,omitempty"`
	Hash          string  `json:"hash,omitempty"`
	VersionNumber *int    `json:"version_number,omitempty"`
	IsCurrent     *bool   `json:"is_current,omitempty"`
}

// ChatUI defines the interface for chat user interface operations.
// Implementations handle rendering chat elements to different outputs.
type ChatUI interface {
	// Header displays the chat session header with mode and configuration
	Header(mode ChatMode, pipeline, sessionID string)

	// Prompt returns the styled input prompt string
	Prompt() string

	// Response displays the assistant's response
	Response(answer string)

	// Sources displays the sources used in a RAG response
	Sources(sources []SourceInfo)

	// NoSources displays a message when no sources were found
	NoSources()

	// Error displays a chat error message
	Error(err error)

	// SessionResume displays session resume information
	SessionResume(sessionID string, turnCount int)

	// SessionEnd displays session end information
	SessionEnd(sessionID string)

	// SessionEndRich displays rich session end information with stats.
	//
	// This is the "maximalist" session end experience, showing:
	//   - Session ID with copy hint
	//   - Session statistics (messages, tokens, duration)
	//   - Commands for interacting with the session (resume, history)
	//
	// Use this instead of SessionEnd when you have accumulated stats.
	SessionEndRich(sessionID string, stats *SessionStats)
}

// terminalChatUI implements ChatUI for terminal output
type terminalChatUI struct {
	writer      io.Writer
	personality PersonalityLevel
}

// write is a helper that writes formatted output and handles errors.
// Errors are silently ignored as there's no meaningful recovery for terminal output.
func (u *terminalChatUI) write(format string, args ...interface{}) {
	if _, err := fmt.Fprintf(u.writer, format, args...); err != nil {
		// Terminal write errors are non-recoverable; silently ignore
		return
	}
}

// writeln is a helper that writes a line and handles errors.
func (u *terminalChatUI) writeln(args ...interface{}) {
	if _, err := fmt.Fprintln(u.writer, args...); err != nil {
		// Terminal write errors are non-recoverable; silently ignore
		return
	}
}

// NewChatUI creates a new terminal-based ChatUI
func NewChatUI() ChatUI {
	return &terminalChatUI{
		writer:      os.Stdout,
		personality: GetPersonality().Level,
	}
}

// NewChatUIWithWriter creates a ChatUI with a custom writer (for testing)
func NewChatUIWithWriter(w io.Writer, personality PersonalityLevel) ChatUI {
	return &terminalChatUI{
		writer:      w,
		personality: personality,
	}
}

// Header displays the chat session header
func (u *terminalChatUI) Header(mode ChatMode, pipeline, sessionID string) {
	if u.personality == PersonalityMachine {
		if mode == ChatModeRAG {
			u.write("CHAT_START: mode=rag pipeline=%s session=%s\n", pipeline, sessionID)
		} else {
			u.write("CHAT_START: mode=direct session=%s\n", sessionID)
		}
		return
	}

	if u.personality == PersonalityMinimal {
		if mode == ChatModeRAG {
			u.write("RAG Chat (pipeline: %s)\n", pipeline)
		} else {
			u.writeln("Direct Chat (no RAG)")
		}
		u.writeln("Type 'exit' to end.")
		return
	}

	// Full personality with box
	var content strings.Builder
	if mode == ChatModeRAG {
		content.WriteString(Styles.Highlight.Render("RAG-Enabled Chat"))
		content.WriteString("\n")
		content.WriteString(fmt.Sprintf("Pipeline: %s", Styles.Success.Render(pipeline)))
	} else {
		content.WriteString(Styles.Warning.Render("Direct LLM Chat"))
		content.WriteString("\n")
		content.WriteString(Styles.Muted.Render("(no knowledge base)"))
	}

	if sessionID != "" {
		content.WriteString("\n")
		content.WriteString(fmt.Sprintf("Session: %s", Styles.Muted.Render(sessionID)))
	}

	boxStyle := Styles.Box.Width(50)
	u.writeln(boxStyle.Render(content.String()))
	u.writeln()
	u.writeln(Styles.Muted.Render("Type 'exit' to end, '/help' for commands."))
	u.writeln()
}

// Prompt returns the styled input prompt string
func (u *terminalChatUI) Prompt() string {
	if u.personality == PersonalityMachine {
		return "> "
	}
	return Styles.Highlight.Render("> ")
}

// Response displays the assistant's response
func (u *terminalChatUI) Response(answer string) {
	if u.personality == PersonalityMachine {
		u.write("RESPONSE: %s\n", answer)
		return
	}
	u.writeln()
	u.writeln(answer)
}

// Sources displays the sources used in a RAG response
func (u *terminalChatUI) Sources(sources []SourceInfo) {
	if len(sources) == 0 {
		return
	}

	if u.personality == PersonalityMachine {
		for _, src := range sources {
			if src.Score != 0 {
				u.write("SOURCE: %s score=%.4f\n", src.Source, src.Score)
			} else if src.Distance != 0 {
				u.write("SOURCE: %s distance=%.4f\n", src.Source, src.Distance)
			} else {
				u.write("SOURCE: %s\n", src.Source)
			}
		}
		return
	}

	u.writeln()
	if u.personality == PersonalityMinimal {
		u.writeln("Sources:")
		for i, src := range sources {
			u.write("  %d. %s\n", i+1, src.Source)
		}
		return
	}

	// Full personality with styled box
	var content strings.Builder
	for i, src := range sources {
		scoreInfo := ""
		if src.Score != 0 {
			scoreInfo = Styles.Muted.Render(fmt.Sprintf(" (%.2f)", src.Score))
		} else if src.Distance != 0 {
			scoreInfo = Styles.Muted.Render(fmt.Sprintf(" (%.2f)", src.Distance))
		}
		content.WriteString(fmt.Sprintf("%d. %s%s", i+1, src.Source, scoreInfo))
		if i < len(sources)-1 {
			content.WriteString("\n")
		}
	}

	boxStyle := Styles.InfoBox.Width(60)
	titleLine := Styles.Subtitle.Render("Sources")
	u.writeln(boxStyle.Render(titleLine + "\n" + content.String()))
}

// NoSources displays a message when no sources were found
func (u *terminalChatUI) NoSources() {
	if u.personality == PersonalityMachine {
		u.writeln("SOURCES: none")
		return
	}
	if u.personality != PersonalityMinimal {
		u.writeln(Styles.Muted.Render("(No sources from knowledge base)"))
	}
}

// Error displays a chat error message
func (u *terminalChatUI) Error(err error) {
	if u.personality == PersonalityMachine {
		u.write("CHAT_ERROR: %v\n", err)
		return
	}
	u.write("%s %s\n", IconError.Render(), Styles.Error.Render(fmt.Sprintf("Chat error: %v", err)))
}

// SessionResume displays session resume information
func (u *terminalChatUI) SessionResume(sessionID string, turnCount int) {
	if u.personality == PersonalityMachine {
		u.write("SESSION_RESUME: session=%s turns=%d\n", sessionID, turnCount)
		return
	}
	u.write("%s %s\n", IconSuccess.Render(),
		Styles.Success.Render(fmt.Sprintf("Resumed session %s (%d previous turns)", sessionID, turnCount)))
}

// SessionEnd displays session end information.
//
// # Description
//
// Displays a simple goodbye message with the session ID. For a richer
// experience with statistics and next steps, use SessionEndRich instead.
//
// # Inputs
//
//   - sessionID: The session identifier. May be empty for anonymous sessions.
//
// # Outputs
//
// None. Writes directly to the configured writer.
//
// # Examples
//
//	ui.SessionEnd("sess-abc123")
//	// Output (full personality):
//	// Session: sess-abc123
//	// Goodbye!
//
// # Limitations
//
//   - Does not display session statistics
//   - Does not show resume commands
//
// # Assumptions
//
//   - Writer is available and writable
func (u *terminalChatUI) SessionEnd(sessionID string) {
	if u.personality == PersonalityMachine {
		u.write("CHAT_END: session=%s\n", sessionID)
		return
	}
	if sessionID != "" {
		u.writeln(Styles.Muted.Render(fmt.Sprintf("Session: %s", sessionID)))
	}
	u.writeln("Goodbye!")
}

// SessionEndRich displays rich session end information with statistics.
//
// # Description
//
// Displays a comprehensive session summary including:
//   - Session ID with visual prominence
//   - Session statistics (messages, tokens, sources, duration)
//   - Performance metrics (time to first response)
//   - Commands for resuming the session later
//
// This is the "maximalist" session end experience, designed to give
// users full visibility into their session and clear next steps.
//
// # Inputs
//
//   - sessionID: The session identifier. May be empty for anonymous sessions.
//   - stats: Session statistics. If nil, falls back to SessionEnd behavior.
//
// # Outputs
//
// None. Writes directly to the configured writer.
//
// # Examples
//
//	stats := &SessionStats{
//	    MessageCount: 5,
//	    TotalTokens:  1234,
//	    Duration:     2 * time.Minute,
//	}
//	ui.SessionEndRich("sess-abc123", stats)
//
// # Limitations
//
//   - Box rendering requires terminal width of at least 60 characters
//   - Emoji icons may not render on all terminals
//
// # Assumptions
//
//   - Writer is available and writable
//   - Terminal supports ANSI colors (for full personality)
func (u *terminalChatUI) SessionEndRich(sessionID string, stats *SessionStats) {
	// Fall back to simple end if no stats
	if stats == nil {
		u.SessionEnd(sessionID)
		return
	}

	if u.personality == PersonalityMachine {
		u.sessionEndRichMachine(sessionID, stats)
		return
	}

	if u.personality == PersonalityMinimal {
		u.sessionEndRichMinimal(sessionID, stats)
		return
	}

	u.sessionEndRichFull(sessionID, stats)
}

// sessionEndRichMachine renders session end in machine-readable format.
//
// # Description
//
// Outputs session end information in a structured KEY=value format
// suitable for parsing by scripts and automation tools.
//
// # Inputs
//
//   - sessionID: The session identifier.
//   - stats: Session statistics to display.
//
// # Outputs
//
// None. Writes to the configured writer in format:
// CHAT_END: session=<id> messages=<n> tokens=<n> duration=<d>
//
// # Limitations
//
//   - Does not include all statistics (only core metrics)
//
// # Assumptions
//
//   - Stats is non-nil (caller validates)
func (u *terminalChatUI) sessionEndRichMachine(sessionID string, stats *SessionStats) {
	u.write("CHAT_END: session=%s messages=%d tokens=%d duration=%s\n",
		sessionID, stats.MessageCount, stats.TotalTokens, stats.Duration.Round(time.Millisecond))
}

// sessionEndRichMinimal renders session end in minimal format.
//
// # Description
//
// Outputs session end information with basic formatting, suitable for
// terminals with limited capability or users who prefer concise output.
//
// # Inputs
//
//   - sessionID: The session identifier.
//   - stats: Session statistics to display.
//
// # Outputs
//
// None. Writes summary line and goodbye to the configured writer.
//
// # Limitations
//
//   - No box styling or icons
//   - No resume command hint
//
// # Assumptions
//
//   - Stats is non-nil (caller validates)
func (u *terminalChatUI) sessionEndRichMinimal(sessionID string, stats *SessionStats) {
	u.writeln()
	if sessionID != "" {
		u.write("Session: %s\n", sessionID)
	}
	u.write("Messages: %d | Tokens: %d | Duration: %s\n",
		stats.MessageCount, stats.TotalTokens, formatDuration(stats.Duration))
	u.writeln("Goodbye!")
}

// sessionEndRichFull renders session end with full styling.
//
// # Description
//
// Outputs a comprehensive, styled session summary in a bordered box.
// Includes all available statistics and hints for continuing the session.
//
// # Inputs
//
//   - sessionID: The session identifier.
//   - stats: Session statistics to display.
//
// # Outputs
//
// None. Writes styled box with:
//   - Session Summary header with ID
//   - Statistics section with icons
//   - Continue Later section with resume command
//   - Goodbye message
//
// # Limitations
//
//   - Requires terminal width >= 60 characters for proper rendering
//   - Icons require Unicode support
//
// # Assumptions
//
//   - Stats is non-nil (caller validates)
//   - Terminal supports ANSI color codes
func (u *terminalChatUI) sessionEndRichFull(sessionID string, stats *SessionStats) {
	u.writeln()

	var content strings.Builder

	// Session section
	content.WriteString(Styles.Subtitle.Render("Session Summary"))
	content.WriteString("\n\n")

	// Session ID with visual prominence
	if sessionID != "" {
		content.WriteString(fmt.Sprintf("  %s  %s\n",
			Styles.Muted.Render("ID:"),
			Styles.Highlight.Render(sessionID)))
	}

	// Stats section
	content.WriteString("\n")
	content.WriteString(Styles.Subtitle.Render("Statistics"))
	content.WriteString("\n\n")

	// Core metrics with icons
	content.WriteString(fmt.Sprintf("  %s  %d messages exchanged\n",
		IconChat.Render(), stats.MessageCount))
	content.WriteString(fmt.Sprintf("  %s  %d tokens generated\n",
		IconInfo.Render(), stats.TotalTokens))

	// Thinking tokens (conditional)
	if stats.ThinkingTokens > 0 {
		content.WriteString(fmt.Sprintf("  %s  %d thinking tokens\n",
			Styles.Muted.Render("🧠"), stats.ThinkingTokens))
	}

	// Sources (conditional)
	if stats.SourcesUsed > 0 {
		content.WriteString(fmt.Sprintf("  %s  %d sources referenced\n",
			IconDocument.Render(), stats.SourcesUsed))
	}

	// Duration
	content.WriteString(fmt.Sprintf("  %s  %s session duration\n",
		IconTime.Render(), formatDuration(stats.Duration)))

	// Performance metrics (conditional)
	if stats.FirstResponseLatency > 0 {
		content.WriteString(fmt.Sprintf("  %s  %s time to first response\n",
			Styles.Muted.Render("⚡"), formatDuration(stats.FirstResponseLatency)))
	}

	// Next steps section (only if session ID available)
	if sessionID != "" {
		content.WriteString("\n")
		content.WriteString(Styles.Subtitle.Render("Continue Later"))
		content.WriteString("\n\n")
		content.WriteString(fmt.Sprintf("  %s\n",
			Styles.Muted.Render("Resume this session:")))
		content.WriteString(fmt.Sprintf("  %s\n",
			Styles.Success.Render(fmt.Sprintf("./aleutian chat --resume %s", sessionID))))
	}

	// Render the styled box
	// Width 68 accommodates the resume command (25 chars + 36 char UUID + padding)
	boxStyle := Styles.Box.Width(68)
	u.writeln(boxStyle.Render(content.String()))
	u.writeln()
	u.writeln(Styles.Highlight.Render("Goodbye! 👋"))
}

// formatDuration formats a duration for human-readable display.
//
// # Description
//
// Converts a time.Duration to a human-friendly string representation.
// Adapts the format based on the magnitude of the duration.
//
// # Inputs
//
//   - d: The duration to format.
//
// # Outputs
//
//   - string: Formatted duration string.
//
// # Examples
//
//	formatDuration(500*time.Millisecond) // "500ms"
//	formatDuration(5*time.Second)        // "5.0s"
//	formatDuration(90*time.Second)       // "1m 30s"
//	formatDuration(2*time.Hour)          // "2h 0m"
//
// # Limitations
//
//   - Does not handle durations longer than 24 hours specially
//
// # Assumptions
//
//   - Duration is non-negative
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		if secs == 0 {
			return fmt.Sprintf("%dm", mins)
		}
		return fmt.Sprintf("%dm %ds", mins, secs)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", hours, mins)
}

// Convenience functions that use the default ChatUI (for backward compatibility)

var defaultChatUI ChatUI

func getDefaultChatUI() ChatUI {
	if defaultChatUI == nil {
		defaultChatUI = NewChatUI()
	}
	return defaultChatUI
}

// ChatHeader prints the chat session header (convenience function)
func ChatHeader(mode ChatMode, pipeline string, sessionID string) {
	getDefaultChatUI().Header(mode, pipeline, sessionID)
}

// ChatSources prints the sources used in a RAG response (convenience function)
func ChatSources(sources []SourceInfo) {
	getDefaultChatUI().Sources(sources)
}

// ChatPrompt returns the styled prompt string (convenience function)
func ChatPrompt() string {
	return getDefaultChatUI().Prompt()
}

// ChatResponse prints the assistant's response (convenience function)
func ChatResponse(answer string) {
	getDefaultChatUI().Response(answer)
}

// ChatError prints a chat error (convenience function)
func ChatError(err error) {
	getDefaultChatUI().Error(err)
}

// ChatSessionResume prints session resume info (convenience function)
func ChatSessionResume(sessionID string, turnCount int) {
	getDefaultChatUI().SessionResume(sessionID, turnCount)
}

// ChatSessionEnd prints session end info (convenience function)
func ChatSessionEnd(sessionID string) {
	getDefaultChatUI().SessionEnd(sessionID)
}

// ChatNoSources prints a message when no sources were found (convenience function)
func ChatNoSources() {
	getDefaultChatUI().NoSources()
}
