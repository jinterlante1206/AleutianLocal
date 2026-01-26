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
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
)

// =============================================================================
// Interface Definition
// =============================================================================

// SessionActionsMenu defines the contract for displaying interactive session action menus.
//
// # Description
//
// SessionActionsMenu provides a user-friendly interface for performing common
// session-related actions like opening URLs in a browser or viewing curl commands.
// The interface allows for different implementations (terminal, GUI, mock for testing).
//
// # Thread Safety
//
// Implementations must be safe for sequential use but are not required to be
// concurrent-safe as menus are typically used in a single-threaded context.
//
// # Limitations
//
//   - Designed for interactive terminal sessions
//   - Browser opening requires platform-specific commands
//
// # Assumptions
//
//   - Input/output streams are valid and connected
//   - User has a default browser configured (for browser actions)
type SessionActionsMenu interface {
	// Show displays the interactive menu and handles user input.
	//
	// # Description
	//
	// Presents a numbered menu of actions and processes user selections
	// until the user chooses to exit. Actions include opening URLs in
	// the browser and displaying curl commands.
	//
	// # Inputs
	//
	//   - sessionID: The session ID for building URLs
	//   - baseURL: The orchestrator base URL
	//
	// # Outputs
	//
	// None. Interacts via configured input/output streams.
	//
	// # Examples
	//
	//   menu := NewDefaultSessionActionsMenu(os.Stdin, os.Stdout)
	//   menu.Show("abc-123", "http://localhost:12210")
	//
	// # Limitations
	//
	//   - Blocks until user selects exit option or EOF
	//
	// # Assumptions
	//
	//   - Input stream provides line-terminated user input
	Show(sessionID, baseURL string)
}

// BrowserOpener defines the contract for opening URLs in a browser.
//
// # Description
//
// Abstracts browser opening to enable testing without actually launching browsers.
//
// # Inputs
//
//   - url: The URL to open
//
// # Outputs
//
//   - error: Non-nil if browser could not be opened
type BrowserOpener interface {
	Open(url string) error
}

// =============================================================================
// Struct Definition
// =============================================================================

// DefaultSessionActionsMenu provides an interactive terminal menu for session actions.
//
// # Description
//
// Implements SessionActionsMenu using standard input/output streams and
// platform-specific browser opening commands.
//
// # Fields
//
//   - reader: Buffered reader for user input
//   - writer: Writer for menu output
//   - browserOpener: Component for opening URLs in browser
//
// # Thread Safety
//
// Not thread-safe. Designed for sequential use in a single terminal session.
//
// # Limitations
//
//   - Requires terminal input (stdin)
//   - Browser opening is platform-dependent
//
// # Assumptions
//
//   - Input/output streams remain valid for the lifetime of the menu
type DefaultSessionActionsMenu struct {
	reader        *bufio.Reader
	writer        io.Writer
	browserOpener BrowserOpener
}

// DefaultBrowserOpener opens URLs using platform-specific commands.
//
// # Description
//
// Uses the appropriate system command to open URLs:
//   - macOS: open
//   - Linux: xdg-open
//   - Windows: cmd /c start
//
// # Thread Safety
//
// Safe for concurrent use.
type DefaultBrowserOpener struct{}

// =============================================================================
// Constructor Functions
// =============================================================================

// NewDefaultSessionActionsMenu creates a new DefaultSessionActionsMenu with the given I/O streams.
//
// # Description
//
// Creates a menu instance configured to read from the provided input stream
// and write to the provided output stream. Uses the default browser opener.
//
// # Inputs
//
//   - input: Reader for user input (typically os.Stdin)
//   - output: Writer for menu output (typically os.Stdout)
//
// # Outputs
//
//   - *DefaultSessionActionsMenu: Configured menu instance
//
// # Examples
//
//	menu := NewDefaultSessionActionsMenu(os.Stdin, os.Stdout)
//
// # Limitations
//
//   - Does not validate that streams are connected to a terminal
//
// # Assumptions
//
//   - Input and output streams are valid and open
func NewDefaultSessionActionsMenu(input io.Reader, output io.Writer) *DefaultSessionActionsMenu {
	return &DefaultSessionActionsMenu{
		reader:        bufio.NewReader(input),
		writer:        output,
		browserOpener: &DefaultBrowserOpener{},
	}
}

// NewSessionActionsMenuWithBrowserOpener creates a menu with a custom browser opener.
//
// # Description
//
// Allows injection of a custom browser opener for testing or alternative
// browser launching strategies.
//
// # Inputs
//
//   - input: Reader for user input
//   - output: Writer for menu output
//   - opener: Custom browser opener implementation
//
// # Outputs
//
//   - *DefaultSessionActionsMenu: Configured menu instance
//
// # Examples
//
//	mockOpener := &MockBrowserOpener{}
//	menu := NewSessionActionsMenuWithBrowserOpener(os.Stdin, os.Stdout, mockOpener)
//
// # Limitations
//
//   - None
//
// # Assumptions
//
//   - All parameters are non-nil
func NewSessionActionsMenuWithBrowserOpener(input io.Reader, output io.Writer, opener BrowserOpener) *DefaultSessionActionsMenu {
	return &DefaultSessionActionsMenu{
		reader:        bufio.NewReader(input),
		writer:        output,
		browserOpener: opener,
	}
}

// =============================================================================
// Interface Implementation - DefaultBrowserOpener
// =============================================================================

// Open opens the given URL in the system's default browser.
//
// # Description
//
// Uses platform-specific commands to launch the default browser:
//   - macOS: Executes "open <url>"
//   - Linux: Executes "xdg-open <url>"
//   - Windows: Executes "cmd /c start <url>"
//
// The command is started asynchronously (does not wait for browser to close).
//
// # Inputs
//
//   - url: The URL to open in the browser
//
// # Outputs
//
//   - error: Non-nil if the browser command failed to start
//
// # Examples
//
//	opener := &DefaultBrowserOpener{}
//	err := opener.Open("http://localhost:8080/graphql")
//	if err != nil {
//	    log.Printf("Could not open browser: %v", err)
//	}
//
// # Limitations
//
//   - Requires the appropriate system command to be available
//   - On Linux, xdg-open must be installed (standard on most desktop distros)
//   - Does not verify URL validity
//
// # Assumptions
//
//   - User has a default browser configured
//   - System commands (open/xdg-open/cmd) are in PATH
func (o *DefaultBrowserOpener) Open(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return cmd.Start()
}

// =============================================================================
// Interface Implementation - DefaultSessionActionsMenu
// =============================================================================

// Show displays the interactive menu and handles user input.
//
// # Description
//
// Presents a numbered menu with the following options:
//   - [1] Open GraphQL Console in browser
//   - [2] Open Session History in browser
//   - [3] Show all curl commands
//   - [4] Done
//
// The menu loops until the user selects option 4 or EOF is reached.
//
// # Inputs
//
//   - sessionID: The session ID for building URLs
//   - baseURL: The orchestrator base URL (e.g., "http://localhost:12210")
//
// # Outputs
//
// None. All output goes to the configured writer.
//
// # Examples
//
//	menu := NewDefaultSessionActionsMenu(os.Stdin, os.Stdout)
//	menu.Show("abc-123-def", "http://localhost:12210")
//
// # Limitations
//
//   - Blocks until user exits or input stream closes
//   - Invalid input results in error message and re-prompt
//
// # Assumptions
//
//   - Weaviate GraphQL console is at localhost:12127
//   - Session history endpoint follows /v1/sessions/{id}/history pattern
func (m *DefaultSessionActionsMenu) Show(sessionID, baseURL string) {
	graphqlURL := "http://localhost:12127/v1/graphql"
	historyURL := fmt.Sprintf("%s/v1/sessions/%s/history", baseURL, sessionID)

	for {
		m.printMenuHeader()
		m.printMenuOptions()

		choice, err := m.readUserChoice()
		if err != nil {
			// EOF or error - exit menu
			m.writeLine("")
			return
		}

		m.writeLine("")

		if m.handleChoice(choice, sessionID, baseURL, graphqlURL, historyURL) {
			return
		}
	}
}

// printMenuHeader writes the menu header to output.
//
// # Description
//
// Outputs the styled header for the quick actions menu.
//
// # Inputs
//
// None.
//
// # Outputs
//
// None. Writes to configured output stream.
//
// # Examples
//
//	m.printMenuHeader()
//	// Output:
//	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
//	// QUICK ACTIONS
//	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
//
// # Limitations
//
//   - Fixed-width formatting assumes terminal width >= 65 chars
//
// # Assumptions
//
//   - Output stream is writable
func (m *DefaultSessionActionsMenu) printMenuHeader() {
	divider := "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	m.writeLine(divider)
	m.writeLine("QUICK ACTIONS")
	m.writeLine(divider)
	m.writeLine("")
}

// printMenuOptions writes the numbered menu options to output.
//
// # Description
//
// Outputs the available menu choices with numbers for selection.
//
// # Inputs
//
// None.
//
// # Outputs
//
// None. Writes to configured output stream.
//
// # Examples
//
//	m.printMenuOptions()
//	// Output:
//	//   [1] Open Session History (JSON)
//	//   [2] Show all curl commands
//	//   [3] Show GraphQL query
//	//   [4] Done
//
// # Limitations
//
//   - Menu options are fixed
//
// # Assumptions
//
//   - Output stream is writable
func (m *DefaultSessionActionsMenu) printMenuOptions() {
	m.writeLine("  [1] Open Session History (JSON) in browser")
	m.writeLine("  [2] Show all curl commands")
	m.writeLine("  [3] Show GraphQL query (copy-paste ready)")
	m.writeLine("  [4] Done")
	m.writeLine("")
	m.write("  Select option [1-4]: ")
}

// readUserChoice reads and returns the user's menu selection.
//
// # Description
//
// Reads a line from input, trims whitespace, and returns the choice.
//
// # Inputs
//
// None. Reads from configured input stream.
//
// # Outputs
//
//   - string: The trimmed user input
//   - error: Non-nil if reading failed (EOF, I/O error)
//
// # Examples
//
//	choice, err := m.readUserChoice()
//	if err != nil {
//	    return // EOF
//	}
//	if choice == "1" { ... }
//
// # Limitations
//
//   - Blocks until newline or EOF
//
// # Assumptions
//
//   - Input stream provides line-terminated input
func (m *DefaultSessionActionsMenu) readUserChoice() (string, error) {
	input, err := m.reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(input), nil
}

// handleChoice processes the user's menu selection.
//
// # Description
//
// Executes the action corresponding to the user's choice:
//   - "1": Opens session history in browser (JSON response)
//   - "2": Displays curl commands
//   - "3": Shows GraphQL query (copy-paste ready)
//   - "4" or "": Exits menu
//   - Other: Shows error message
//
// # Inputs
//
//   - choice: The user's selection (1-4)
//   - sessionID: Session ID for URL construction
//   - baseURL: Orchestrator base URL
//   - graphqlURL: GraphQL console URL
//   - historyURL: Session history URL
//
// # Outputs
//
//   - bool: True if menu should exit, false to continue
//
// # Examples
//
//	shouldExit := m.handleChoice("1", sessionID, baseURL, graphqlURL, historyURL)
//	if shouldExit {
//	    return
//	}
//
// # Limitations
//
//   - Only handles choices 1-4
//
// # Assumptions
//
//   - All URL parameters are valid
func (m *DefaultSessionActionsMenu) handleChoice(choice, sessionID, baseURL, graphqlURL, historyURL string) bool {
	switch choice {
	case "1":
		m.openURLInBrowser(historyURL, "Session History")
		return false
	case "2":
		m.showCurlCommands(sessionID, baseURL, graphqlURL, historyURL)
		return false
	case "3":
		m.showGraphQLQuery(sessionID, graphqlURL)
		return false
	case "4", "":
		return true
	default:
		m.writeLine("  Invalid option. Please select 1-4.")
		m.writeLine("")
		return false
	}
}

// openURLInBrowser opens a URL and displays status.
//
// # Description
//
// Attempts to open the URL in the default browser and displays
// success or failure message to the user.
//
// # Inputs
//
//   - url: The URL to open
//   - description: Human-readable description for status messages
//
// # Outputs
//
// None. Writes status to configured output stream.
//
// # Examples
//
//	m.openURLInBrowser("http://localhost:8080", "GraphQL Console")
//	// Output on success: "  Opening GraphQL Console..."
//	//                    "  ✓ Opened in browser"
//	// Output on failure: "  Opening GraphQL Console..."
//	//                    "  ⚠️  Could not open browser: <error>"
//	//                    "  📋  URL: http://localhost:8080"
//
// # Limitations
//
//   - Success message doesn't verify browser actually opened URL
//
// # Assumptions
//
//   - browserOpener is configured and functional
func (m *DefaultSessionActionsMenu) openURLInBrowser(url, description string) {
	m.writef("  Opening %s...\n", description)
	if err := m.browserOpener.Open(url); err != nil {
		m.writef("  ⚠️  Could not open browser: %v\n", err)
		m.writef("  📋  URL: %s\n", url)
	} else {
		m.writeLine("  ✓ Opened in browser")
	}
	m.writeLine("")
}

// showCurlCommands displays curl commands for session operations.
//
// # Description
//
// Prints formatted curl commands for common session operations:
//   - Get session history
//   - Verify session integrity
//   - Query GraphQL (example)
//
// # Inputs
//
//   - sessionID: Session ID for URL construction
//   - baseURL: Orchestrator base URL
//   - graphqlURL: GraphQL console URL
//   - historyURL: Session history URL
//
// # Outputs
//
// None. Writes commands to configured output stream.
//
// # Examples
//
//	m.showCurlCommands("abc-123", "http://localhost:12210", graphqlURL, historyURL)
//	// Output:
//	//   Curl Commands:
//	//
//	//   # Get session history
//	//   curl http://localhost:12210/v1/sessions/abc-123/history
//	//   ...
//
// # Limitations
//
//   - Commands are formatted for bash-compatible shells
//
// # Assumptions
//
//   - All URL parameters are valid
func (m *DefaultSessionActionsMenu) showCurlCommands(sessionID, baseURL, graphqlURL, historyURL string) {
	m.writeLine("  Curl Commands:")
	m.writeLine("")
	m.writeLine("  # Get session history")
	m.writef("  curl %s\n", historyURL)
	m.writeLine("")
	m.writeLine("  # Verify session integrity")
	m.writef("  curl -X POST %s/v1/sessions/%s/verify\n", baseURL, sessionID)
	m.writeLine("")
	m.writeLine("  # Query GraphQL (example)")
	m.writef("  curl -X POST %s -H 'Content-Type: application/json' -d '{\"query\": \"{ Get { Session { session_id } } }\"}'\n", graphqlURL)
	m.writeLine("")
}

// showGraphQLQuery displays a copy-paste ready GraphQL query for the session.
//
// # Description
//
// Outputs a formatted GraphQL query that can be pasted into any GraphQL
// client (Insomnia, Postman, graphql-playground, etc.) to query session data.
// Also shows the curl command for CLI usage.
//
// # Inputs
//
//   - sessionID: Session ID for the query filter
//   - graphqlURL: GraphQL endpoint URL
//
// # Outputs
//
// None. Writes query to configured output stream.
//
// # Examples
//
//	m.showGraphQLQuery("abc-123", "http://localhost:12127/v1/graphql")
//	// Output:
//	//   GraphQL Query (copy into your GraphQL client):
//	//
//	//   POST http://localhost:12127/v1/graphql
//	//
//	//   {
//	//     Get {
//	//       Conversation(where: {...}) {
//	//         ...
//	//       }
//	//     }
//	//   }
//
// # Limitations
//
//   - Query is specific to Weaviate schema
//
// # Assumptions
//
//   - Weaviate Conversation class exists with expected fields
func (m *DefaultSessionActionsMenu) showGraphQLQuery(sessionID, graphqlURL string) {
	m.writeLine("  GraphQL Query (copy into your GraphQL client):")
	m.writeLine("")
	m.writef("  Endpoint: %s\n", graphqlURL)
	m.writeLine("  Method:   POST")
	m.writeLine("  Headers:  Content-Type: application/json")
	m.writeLine("")
	m.writeLine("  Query:")
	m.writeLine("  ┌─────────────────────────────────────────────────────────────")
	m.writeLine("  │ {")
	m.writeLine("  │   Get {")
	m.writef("  │     Conversation(where: {path: [\"session_id\"], operator: Equal, valueString: \"%s\"}) {\n", sessionID)
	m.writeLine("  │       question")
	m.writeLine("  │       answer")
	m.writeLine("  │       timestamp")
	m.writeLine("  │       turn_number")
	m.writeLine("  │     }")
	m.writeLine("  │   }")
	m.writeLine("  │ }")
	m.writeLine("  └─────────────────────────────────────────────────────────────")
	m.writeLine("")
	m.writeLine("  Curl command:")
	m.writef("  curl -X POST %s -H 'Content-Type: application/json' -d '{\"query\": \"{ Get { Conversation(where: {path: [\\\"session_id\\\"], operator: Equal, valueString: \\\"%s\\\"}) { question answer timestamp } } }\"}'\n", graphqlURL, sessionID)
	m.writeLine("")
}

// =============================================================================
// Helper Methods
// =============================================================================

// writeLine writes a line with newline to the output stream.
//
// # Description
//
// Convenience method for writing a complete line to output.
//
// # Inputs
//
//   - s: The string to write
//
// # Outputs
//
// None.
//
// # Limitations
//
//   - Logs write errors but does not return them
//
// # Assumptions
//
//   - Output stream is writable
func (m *DefaultSessionActionsMenu) writeLine(s string) {
	if _, err := fmt.Fprintln(m.writer, s); err != nil {
		slog.Warn("failed to write line", "error", err)
	}
}

// write writes a string to the output stream without newline.
//
// # Description
//
// Convenience method for writing without trailing newline.
//
// # Inputs
//
//   - s: The string to write
//
// # Outputs
//
// None.
//
// # Limitations
//
//   - Logs write errors but does not return them
//
// # Assumptions
//
//   - Output stream is writable
func (m *DefaultSessionActionsMenu) write(s string) {
	if _, err := fmt.Fprint(m.writer, s); err != nil {
		slog.Warn("failed to write", "error", err)
	}
}

// writef writes a formatted string to the output stream.
//
// # Description
//
// Convenience method for formatted output.
//
// # Inputs
//
//   - format: Printf-style format string
//   - args: Format arguments
//
// # Outputs
//
// None.
//
// # Limitations
//
//   - Logs write errors but does not return them
//
// # Assumptions
//
//   - Output stream is writable
func (m *DefaultSessionActionsMenu) writef(format string, args ...any) {
	if _, err := fmt.Fprintf(m.writer, format, args...); err != nil {
		slog.Warn("failed to write formatted output", "error", err)
	}
}

// =============================================================================
// Compile-time Interface Assertions
// =============================================================================

var _ SessionActionsMenu = (*DefaultSessionActionsMenu)(nil)
var _ BrowserOpener = (*DefaultBrowserOpener)(nil)
