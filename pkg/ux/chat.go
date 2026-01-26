// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

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

// HeaderConfig contains configuration for displaying the chat header.
//
// # Description
//
// HeaderConfig groups all optional parameters for the chat header display.
// This allows extending the header with new fields without breaking existing
// callers of the Header() method.
//
// # Fields
//
//   - Mode: Required. RAG or Direct chat mode.
//   - Pipeline: RAG pipeline name (e.g., "verified", "reranking"). Empty for direct mode.
//   - SessionID: Session identifier for resume. May be empty for new sessions.
//   - TTL: Session TTL as configured (e.g., "5m", "24h"). Empty if no TTL.
//   - DataSpace: Dataspace being queried (e.g., "wheat", "work"). Empty for all docs.
//   - DataSpaceStats: Optional aggregated stats for the dataspace.
type HeaderConfig struct {
	Mode           ChatMode
	Pipeline       string
	SessionID      string
	TTL            string
	DataSpace      string
	DataSpaceStats *DataSpaceStats // Optional stats from orchestrator
}

// DataSpaceStats contains aggregated metrics for a dataspace.
//
// # Description
//
// DataSpaceStats captures aggregate information about documents within
// a dataspace. This is fetched from the orchestrator and displayed
// in the chat header.
//
// # Fields
//
//   - DocumentCount: Number of documents in the dataspace
//   - LastUpdatedAt: Unix milliseconds of most recent document ingestion
type DataSpaceStats struct {
	DocumentCount int   `json:"document_count"`
	LastUpdatedAt int64 `json:"last_updated_at"` // Unix ms timestamp
}

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
	// Header displays the chat session header with mode and configuration.
	// Deprecated: Use HeaderWithConfig for new code.
	Header(mode ChatMode, pipeline, sessionID string)

	// HeaderWithConfig displays the chat session header with full configuration.
	// This method supports displaying TTL, dataspace, and other metadata.
	HeaderWithConfig(config HeaderConfig)

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

	// ShowAutoCorrection displays a notification that a typo was auto-corrected.
	//
	// # Description
	//
	// Called when a high-confidence typo correction is applied automatically.
	// Informs the user what was corrected so they can undo if needed.
	//
	// # Inputs
	//
	//   - original: The original (misspelled) word
	//   - corrected: The corrected word that will be used
	ShowAutoCorrection(original, corrected string)

	// ShowCorrectionSuggestion displays a suggestion for a possible typo.
	//
	// # Description
	//
	// Called for lower-confidence corrections that require user confirmation.
	// The user can retype their query if the suggestion is correct.
	//
	// # Inputs
	//
	//   - original: The original (possibly misspelled) word
	//   - suggested: The suggested correction
	ShowCorrectionSuggestion(original, suggested string)
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

// Header displays the chat session header.
// Deprecated: Use HeaderWithConfig for new code with TTL/dataspace support.
func (u *terminalChatUI) Header(mode ChatMode, pipeline, sessionID string) {
	u.HeaderWithConfig(HeaderConfig{
		Mode:      mode,
		Pipeline:  pipeline,
		SessionID: sessionID,
	})
}

// HeaderWithConfig displays the chat session header with full configuration.
//
// # Description
//
// Renders the chat header box with mode, pipeline, and optional metadata
// including TTL and dataspace. Adapts output based on personality level.
//
// # Inputs
//
//   - config: HeaderConfig with mode, pipeline, sessionID, TTL, dataspace
//
// # Outputs
//
// None. Writes directly to the configured writer.
func (u *terminalChatUI) HeaderWithConfig(config HeaderConfig) {
	if u.personality == PersonalityMachine {
		u.headerMachine(config)
		return
	}

	if u.personality == PersonalityMinimal {
		u.headerMinimal(config)
		return
	}

	u.headerFull(config)
}

// headerMachine renders the header in machine-readable format.
func (u *terminalChatUI) headerMachine(config HeaderConfig) {
	if config.Mode == ChatModeRAG {
		parts := []string{fmt.Sprintf("mode=rag pipeline=%s", config.Pipeline)}
		if config.SessionID != "" {
			parts = append(parts, fmt.Sprintf("session=%s", config.SessionID))
		}
		if config.DataSpace != "" {
			parts = append(parts, fmt.Sprintf("dataspace=%s", config.DataSpace))
		}
		if config.DataSpaceStats != nil {
			parts = append(parts, fmt.Sprintf("doc_count=%d", config.DataSpaceStats.DocumentCount))
			if config.DataSpaceStats.LastUpdatedAt > 0 {
				parts = append(parts, fmt.Sprintf("last_updated=%d", config.DataSpaceStats.LastUpdatedAt))
			}
		}
		if config.TTL != "" {
			parts = append(parts, fmt.Sprintf("ttl=%s", config.TTL))
		}
		u.write("CHAT_START: %s\n", strings.Join(parts, " "))
	} else {
		parts := []string{"mode=direct"}
		if config.SessionID != "" {
			parts = append(parts, fmt.Sprintf("session=%s", config.SessionID))
		}
		u.write("CHAT_START: %s\n", strings.Join(parts, " "))
	}
}

// headerMinimal renders the header in minimal format.
func (u *terminalChatUI) headerMinimal(config HeaderConfig) {
	if config.Mode == ChatModeRAG {
		u.write("RAG Chat (pipeline: %s)\n", config.Pipeline)
		if config.DataSpace != "" {
			if config.DataSpaceStats != nil {
				u.write("Dataspace: %s (%d docs)\n", config.DataSpace, config.DataSpaceStats.DocumentCount)
			} else {
				u.write("Dataspace: %s\n", config.DataSpace)
			}
		}
		if config.TTL != "" {
			u.write("TTL: %s\n", config.TTL)
		}
	} else {
		u.writeln("Direct Chat (no RAG)")
	}
	u.writeln("Type 'exit' to end.")
}

// headerFull renders the header with full styling.
func (u *terminalChatUI) headerFull(config HeaderConfig) {
	var content strings.Builder
	if config.Mode == ChatModeRAG {
		content.WriteString(Styles.Highlight.Render("RAG-Enabled Chat"))
		content.WriteString("\n")
		content.WriteString(fmt.Sprintf("Pipeline: %s", Styles.Success.Render(config.Pipeline)))

		// Add dataspace with optional stats
		if config.DataSpace != "" {
			content.WriteString("\n")
			if config.DataSpaceStats != nil {
				// Format: "Dataspace: wheat (142 docs, updated 2h ago)"
				statsInfo := fmt.Sprintf("%d docs", config.DataSpaceStats.DocumentCount)
				if config.DataSpaceStats.LastUpdatedAt > 0 {
					relTime := formatRelativeTime(config.DataSpaceStats.LastUpdatedAt)
					statsInfo = fmt.Sprintf("%s, updated %s", statsInfo, relTime)
				}
				content.WriteString(fmt.Sprintf("Dataspace: %s %s",
					Styles.Success.Render(config.DataSpace),
					Styles.Muted.Render(fmt.Sprintf("(%s)", statsInfo))))
			} else {
				content.WriteString(fmt.Sprintf("Dataspace: %s", Styles.Success.Render(config.DataSpace)))
			}
		}

		// Add TTL on same line as dataspace if both present, otherwise new line
		if config.TTL != "" {
			if config.DataSpace != "" {
				content.WriteString(" | ")
			} else {
				content.WriteString("\n")
			}
			content.WriteString(fmt.Sprintf("TTL: %s", Styles.Success.Render(config.TTL)))
		}
	} else {
		content.WriteString(Styles.Warning.Render("Direct LLM Chat"))
		content.WriteString("\n")
		content.WriteString(Styles.Muted.Render("(no knowledge base)"))
	}

	if config.SessionID != "" {
		content.WriteString("\n")
		content.WriteString(fmt.Sprintf("Session: %s", Styles.Muted.Render(config.SessionID)))
	}

	boxStyle := Styles.Box.Width(60)
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

// formatRelativeTime converts a Unix milliseconds timestamp to a relative time string.
//
// # Description
//
// Converts a timestamp to a human-friendly relative time like "2h ago",
// "3 days ago", etc. Adapts the unit based on the time difference.
//
// # Inputs
//
//   - unixMs: Unix timestamp in milliseconds
//
// # Outputs
//
//   - string: Relative time string (e.g., "2h ago", "3 days ago")
//
// # Examples
//
//	formatRelativeTime(time.Now().Add(-2*time.Hour).UnixMilli()) // "2h ago"
//	formatRelativeTime(time.Now().Add(-3*24*time.Hour).UnixMilli()) // "3 days ago"
//
// # Limitations
//
//   - Returns "just now" for times within the last minute
//   - Does not handle future times specially
//
// # Assumptions
//
//   - Timestamp is in milliseconds (not seconds)
func formatRelativeTime(unixMs int64) string {
	if unixMs == 0 {
		return "unknown"
	}

	t := time.UnixMilli(unixMs)
	diff := time.Since(t)

	if diff < time.Minute {
		return "just now"
	}
	if diff < time.Hour {
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", mins)
	}
	if diff < 24*time.Hour {
		hours := int(diff.Hours())
		if hours == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", hours)
	}
	if diff < 7*24*time.Hour {
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
	if diff < 30*24*time.Hour {
		weeks := int(diff.Hours() / (24 * 7))
		if weeks == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	}

	// For older times, show the date
	return t.Format("Jan 2, 2006")
}

// ShowAutoCorrection displays a notification that a typo was auto-corrected.
//
// # Description
//
// Called when a high-confidence typo correction is applied automatically.
// Output format varies by personality mode:
//   - Machine: AUTOCORRECT: original -> corrected
//   - Minimal: [corrected "original" to "corrected"]
//   - Full: Styled message with muted styling
//
// # Inputs
//
//   - original: The original (misspelled) word
//   - corrected: The corrected word that will be used
//
// # Outputs
//
// None. Writes to the configured writer.
func (u *terminalChatUI) ShowAutoCorrection(original, corrected string) {
	switch u.personality {
	case PersonalityMachine:
		u.write("AUTOCORRECT: %s -> %s\n", original, corrected)
	case PersonalityMinimal:
		u.write("[corrected \"%s\" to \"%s\"]\n", original, corrected)
	default:
		u.writeln(Styles.Muted.Render(fmt.Sprintf("(corrected \"%s\" → \"%s\")", original, corrected)))
	}
}

// ShowCorrectionSuggestion displays a suggestion for a possible typo.
//
// # Description
//
// Called for lower-confidence corrections that the user might want to consider.
// Does not auto-correct; just informs the user of a possible typo.
// Output format varies by personality mode.
//
// # Inputs
//
//   - original: The original (possibly misspelled) word
//   - suggested: The suggested correction
//
// # Outputs
//
// None. Writes to the configured writer.
func (u *terminalChatUI) ShowCorrectionSuggestion(original, suggested string) {
	switch u.personality {
	case PersonalityMachine:
		u.write("SUGGEST: %s -> %s\n", original, suggested)
	case PersonalityMinimal:
		u.write("Did you mean \"%s\"? (you typed \"%s\")\n", suggested, original)
	default:
		u.writeln(Styles.Warning.Render(fmt.Sprintf("Did you mean \"%s\"?", suggested)) +
			Styles.Muted.Render(fmt.Sprintf(" (you typed \"%s\")", original)))
	}
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
