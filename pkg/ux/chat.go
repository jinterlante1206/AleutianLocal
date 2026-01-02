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
)

// ChatMode represents the chat operation mode
type ChatMode int

const (
	ChatModeRAG ChatMode = iota
	ChatModeDirect
)

// SourceInfo represents a source citation from RAG
type SourceInfo struct {
	Source   string
	Distance float64
	Score    float64
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
}

// terminalChatUI implements ChatUI for terminal output
type terminalChatUI struct {
	writer      io.Writer
	personality PersonalityLevel
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
			fmt.Fprintf(u.writer, "CHAT_START: mode=rag pipeline=%s session=%s\n", pipeline, sessionID)
		} else {
			fmt.Fprintf(u.writer, "CHAT_START: mode=direct session=%s\n", sessionID)
		}
		return
	}

	if u.personality == PersonalityMinimal {
		if mode == ChatModeRAG {
			fmt.Fprintf(u.writer, "RAG Chat (pipeline: %s)\n", pipeline)
		} else {
			fmt.Fprintln(u.writer, "Direct Chat (no RAG)")
		}
		fmt.Fprintln(u.writer, "Type 'exit' to end.")
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
	fmt.Fprintln(u.writer, boxStyle.Render(content.String()))
	fmt.Fprintln(u.writer)
	fmt.Fprintln(u.writer, Styles.Muted.Render("Type 'exit' to end, '/help' for commands."))
	fmt.Fprintln(u.writer)
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
		fmt.Fprintf(u.writer, "RESPONSE: %s\n", answer)
		return
	}
	fmt.Fprintln(u.writer)
	fmt.Fprintln(u.writer, answer)
}

// Sources displays the sources used in a RAG response
func (u *terminalChatUI) Sources(sources []SourceInfo) {
	if len(sources) == 0 {
		return
	}

	if u.personality == PersonalityMachine {
		for _, src := range sources {
			if src.Score != 0 {
				fmt.Fprintf(u.writer, "SOURCE: %s score=%.4f\n", src.Source, src.Score)
			} else if src.Distance != 0 {
				fmt.Fprintf(u.writer, "SOURCE: %s distance=%.4f\n", src.Source, src.Distance)
			} else {
				fmt.Fprintf(u.writer, "SOURCE: %s\n", src.Source)
			}
		}
		return
	}

	fmt.Fprintln(u.writer)
	if u.personality == PersonalityMinimal {
		fmt.Fprintln(u.writer, "Sources:")
		for i, src := range sources {
			fmt.Fprintf(u.writer, "  %d. %s\n", i+1, src.Source)
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
	fmt.Fprintln(u.writer, boxStyle.Render(titleLine+"\n"+content.String()))
}

// NoSources displays a message when no sources were found
func (u *terminalChatUI) NoSources() {
	if u.personality == PersonalityMachine {
		fmt.Fprintln(u.writer, "SOURCES: none")
		return
	}
	if u.personality != PersonalityMinimal {
		fmt.Fprintln(u.writer, Styles.Muted.Render("(No sources from knowledge base)"))
	}
}

// Error displays a chat error message
func (u *terminalChatUI) Error(err error) {
	if u.personality == PersonalityMachine {
		fmt.Fprintf(u.writer, "CHAT_ERROR: %v\n", err)
		return
	}
	fmt.Fprintf(u.writer, "%s %s\n", IconError.Render(), Styles.Error.Render(fmt.Sprintf("Chat error: %v", err)))
}

// SessionResume displays session resume information
func (u *terminalChatUI) SessionResume(sessionID string, turnCount int) {
	if u.personality == PersonalityMachine {
		fmt.Fprintf(u.writer, "SESSION_RESUME: session=%s turns=%d\n", sessionID, turnCount)
		return
	}
	fmt.Fprintf(u.writer, "%s %s\n", IconSuccess.Render(),
		Styles.Success.Render(fmt.Sprintf("Resumed session %s (%d previous turns)", sessionID, turnCount)))
}

// SessionEnd displays session end information
func (u *terminalChatUI) SessionEnd(sessionID string) {
	if u.personality == PersonalityMachine {
		fmt.Fprintf(u.writer, "CHAT_END: session=%s\n", sessionID)
		return
	}
	if sessionID != "" {
		fmt.Fprintln(u.writer, Styles.Muted.Render(fmt.Sprintf("Session: %s", sessionID)))
	}
	fmt.Fprintln(u.writer, "Goodbye!")
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
