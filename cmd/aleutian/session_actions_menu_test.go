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
	"bytes"
	"errors"
	"strings"
	"testing"
)

// =============================================================================
// Mock Implementations
// =============================================================================

// MockBrowserOpener records browser open calls for testing.
//
// # Description
//
// Captures all URLs passed to Open() and can be configured to return errors.
//
// # Fields
//
//   - OpenedURLs: Slice of all URLs passed to Open()
//   - ShouldError: If true, Open() returns an error
//   - ErrorMsg: The error message to return when ShouldError is true
type MockBrowserOpener struct {
	OpenedURLs  []string
	ShouldError bool
	ErrorMsg    string
}

// Open records the URL and optionally returns an error.
//
// # Description
//
// Mock implementation that records the URL and can simulate errors.
//
// # Inputs
//
//   - url: The URL to "open"
//
// # Outputs
//
//   - error: Returns error if ShouldError is true
//
// # Examples
//
//	mock := &MockBrowserOpener{}
//	err := mock.Open("http://example.com")
//	// mock.OpenedURLs now contains "http://example.com"
//
// # Limitations
//
//   - Does not actually open a browser
//
// # Assumptions
//
//   - None
func (m *MockBrowserOpener) Open(url string) error {
	m.OpenedURLs = append(m.OpenedURLs, url)
	if m.ShouldError {
		if m.ErrorMsg != "" {
			return errors.New(m.ErrorMsg)
		}
		return errors.New("mock browser error")
	}
	return nil
}

// =============================================================================
// DefaultBrowserOpener Tests
// =============================================================================

// TestDefaultBrowserOpener_ImplementsInterface verifies interface compliance.
//
// # Description
//
// Ensures DefaultBrowserOpener implements BrowserOpener interface.
func TestDefaultBrowserOpener_ImplementsInterface(t *testing.T) {
	var _ BrowserOpener = (*DefaultBrowserOpener)(nil)
}

// =============================================================================
// DefaultSessionActionsMenu Tests
// =============================================================================

// TestDefaultSessionActionsMenu_ImplementsInterface verifies interface compliance.
//
// # Description
//
// Ensures DefaultSessionActionsMenu implements SessionActionsMenu interface.
func TestDefaultSessionActionsMenu_ImplementsInterface(t *testing.T) {
	var _ SessionActionsMenu = (*DefaultSessionActionsMenu)(nil)
}

// TestNewDefaultSessionActionsMenu verifies constructor creates valid instance.
//
// # Description
//
// Tests that the constructor properly initializes all fields.
func TestNewDefaultSessionActionsMenu(t *testing.T) {
	input := strings.NewReader("")
	output := &bytes.Buffer{}

	menu := NewDefaultSessionActionsMenu(input, output)

	if menu == nil {
		t.Fatal("expected non-nil menu")
	}
	if menu.reader == nil {
		t.Error("expected non-nil reader")
	}
	if menu.writer == nil {
		t.Error("expected non-nil writer")
	}
	if menu.browserOpener == nil {
		t.Error("expected non-nil browserOpener")
	}
}

// TestNewSessionActionsMenuWithBrowserOpener verifies custom opener injection.
//
// # Description
//
// Tests that a custom browser opener can be injected.
func TestNewSessionActionsMenuWithBrowserOpener(t *testing.T) {
	input := strings.NewReader("")
	output := &bytes.Buffer{}
	mockOpener := &MockBrowserOpener{}

	menu := NewSessionActionsMenuWithBrowserOpener(input, output, mockOpener)

	if menu.browserOpener != mockOpener {
		t.Error("expected custom browser opener to be set")
	}
}

// TestDefaultSessionActionsMenu_Show_ExitOnOption4 verifies exit behavior.
//
// # Description
//
// Tests that selecting option 4 exits the menu loop.
func TestDefaultSessionActionsMenu_Show_ExitOnOption4(t *testing.T) {
	input := strings.NewReader("4\n")
	output := &bytes.Buffer{}
	mockOpener := &MockBrowserOpener{}

	menu := NewSessionActionsMenuWithBrowserOpener(input, output, mockOpener)
	menu.Show("test-session-id", "http://localhost:12210")

	// Should exit without opening any URLs
	if len(mockOpener.OpenedURLs) != 0 {
		t.Errorf("expected no URLs opened, got %d", len(mockOpener.OpenedURLs))
	}
}

// TestDefaultSessionActionsMenu_Show_ExitOnEmptyInput verifies empty input exits.
//
// # Description
//
// Tests that pressing enter without input exits the menu.
func TestDefaultSessionActionsMenu_Show_ExitOnEmptyInput(t *testing.T) {
	input := strings.NewReader("\n")
	output := &bytes.Buffer{}
	mockOpener := &MockBrowserOpener{}

	menu := NewSessionActionsMenuWithBrowserOpener(input, output, mockOpener)
	menu.Show("test-session-id", "http://localhost:12210")

	if len(mockOpener.OpenedURLs) != 0 {
		t.Errorf("expected no URLs opened, got %d", len(mockOpener.OpenedURLs))
	}
}

// TestDefaultSessionActionsMenu_Show_ExitOnEOF verifies EOF exits gracefully.
//
// # Description
//
// Tests that EOF on input stream exits the menu without error.
func TestDefaultSessionActionsMenu_Show_ExitOnEOF(t *testing.T) {
	input := strings.NewReader("") // Empty input = EOF immediately
	output := &bytes.Buffer{}
	mockOpener := &MockBrowserOpener{}

	menu := NewSessionActionsMenuWithBrowserOpener(input, output, mockOpener)
	menu.Show("test-session-id", "http://localhost:12210")

	// Should not panic or hang
	if len(mockOpener.OpenedURLs) != 0 {
		t.Errorf("expected no URLs opened, got %d", len(mockOpener.OpenedURLs))
	}
}

// TestDefaultSessionActionsMenu_Show_Option1_OpensHistory verifies history opening.
//
// # Description
//
// Tests that option 1 opens the session history URL (JSON response).
func TestDefaultSessionActionsMenu_Show_Option1_OpensHistory(t *testing.T) {
	input := strings.NewReader("1\n4\n") // Open history, then exit
	output := &bytes.Buffer{}
	mockOpener := &MockBrowserOpener{}

	menu := NewSessionActionsMenuWithBrowserOpener(input, output, mockOpener)
	menu.Show("my-session-123", "http://localhost:12210")

	if len(mockOpener.OpenedURLs) != 1 {
		t.Fatalf("expected 1 URL opened, got %d", len(mockOpener.OpenedURLs))
	}
	expectedURL := "http://localhost:12210/v1/sessions/my-session-123/history"
	if mockOpener.OpenedURLs[0] != expectedURL {
		t.Errorf("expected %s, got %s", expectedURL, mockOpener.OpenedURLs[0])
	}

	outputStr := output.String()
	if !strings.Contains(outputStr, "Opened in browser") {
		t.Error("expected success message in output")
	}
}

// TestDefaultSessionActionsMenu_Show_Option2_ShowsCurlCommands verifies curl output.
//
// # Description
//
// Tests that option 2 displays curl commands without opening browser.
func TestDefaultSessionActionsMenu_Show_Option2_ShowsCurlCommands(t *testing.T) {
	input := strings.NewReader("2\n4\n") // Show curl, then exit
	output := &bytes.Buffer{}
	mockOpener := &MockBrowserOpener{}

	menu := NewSessionActionsMenuWithBrowserOpener(input, output, mockOpener)
	menu.Show("test-session", "http://localhost:12210")

	// Should not open any URLs
	if len(mockOpener.OpenedURLs) != 0 {
		t.Errorf("expected no URLs opened, got %d", len(mockOpener.OpenedURLs))
	}

	outputStr := output.String()

	// Verify curl commands are shown
	if !strings.Contains(outputStr, "curl http://localhost:12210/v1/sessions/test-session/history") {
		t.Error("expected history curl command in output")
	}
	if !strings.Contains(outputStr, "curl -X POST http://localhost:12210/v1/sessions/test-session/verify") {
		t.Error("expected verify curl command in output")
	}
	if !strings.Contains(outputStr, "curl -X POST http://localhost:12127/v1/graphql") {
		t.Error("expected GraphQL curl command in output")
	}
}

// TestDefaultSessionActionsMenu_Show_Option3_ShowsGraphQLQuery verifies GraphQL query output.
//
// # Description
//
// Tests that option 3 displays GraphQL query without opening browser.
func TestDefaultSessionActionsMenu_Show_Option3_ShowsGraphQLQuery(t *testing.T) {
	input := strings.NewReader("3\n4\n") // Show GraphQL query, then exit
	output := &bytes.Buffer{}
	mockOpener := &MockBrowserOpener{}

	menu := NewSessionActionsMenuWithBrowserOpener(input, output, mockOpener)
	menu.Show("test-session", "http://localhost:12210")

	// Should not open any URLs
	if len(mockOpener.OpenedURLs) != 0 {
		t.Errorf("expected no URLs opened, got %d", len(mockOpener.OpenedURLs))
	}

	outputStr := output.String()

	// Verify GraphQL query elements are shown
	if !strings.Contains(outputStr, "GraphQL Query") {
		t.Error("expected GraphQL Query header in output")
	}
	if !strings.Contains(outputStr, "Conversation") {
		t.Error("expected Conversation class in query")
	}
	if !strings.Contains(outputStr, "test-session") {
		t.Error("expected session ID in query")
	}
	if !strings.Contains(outputStr, "question") {
		t.Error("expected question field in query")
	}
	if !strings.Contains(outputStr, "answer") {
		t.Error("expected answer field in query")
	}
}

// TestDefaultSessionActionsMenu_Show_InvalidOption_ShowsError verifies error handling.
//
// # Description
//
// Tests that invalid options display an error message and re-prompt.
func TestDefaultSessionActionsMenu_Show_InvalidOption_ShowsError(t *testing.T) {
	input := strings.NewReader("x\n5\ninvalid\n4\n") // Invalid options, then exit
	output := &bytes.Buffer{}
	mockOpener := &MockBrowserOpener{}

	menu := NewSessionActionsMenuWithBrowserOpener(input, output, mockOpener)
	menu.Show("test-session", "http://localhost:12210")

	outputStr := output.String()

	// Should show error message for invalid options
	errorCount := strings.Count(outputStr, "Invalid option")
	if errorCount != 3 {
		t.Errorf("expected 3 invalid option messages, got %d", errorCount)
	}

	// Should not open any URLs
	if len(mockOpener.OpenedURLs) != 0 {
		t.Errorf("expected no URLs opened, got %d", len(mockOpener.OpenedURLs))
	}
}

// TestDefaultSessionActionsMenu_Show_BrowserError_ShowsFallback verifies error fallback.
//
// # Description
//
// Tests that browser open errors display the URL as fallback.
func TestDefaultSessionActionsMenu_Show_BrowserError_ShowsFallback(t *testing.T) {
	input := strings.NewReader("1\n4\n") // Try to open history, then exit
	output := &bytes.Buffer{}
	mockOpener := &MockBrowserOpener{
		ShouldError: true,
		ErrorMsg:    "browser not found",
	}

	menu := NewSessionActionsMenuWithBrowserOpener(input, output, mockOpener)
	menu.Show("test-session", "http://localhost:12210")

	outputStr := output.String()

	// Should show error message
	if !strings.Contains(outputStr, "Could not open browser") {
		t.Error("expected error message in output")
	}

	// Should show history URL as fallback
	if !strings.Contains(outputStr, "http://localhost:12210/v1/sessions/test-session/history") {
		t.Error("expected URL fallback in output")
	}
}

// TestDefaultSessionActionsMenu_Show_MultipleActions verifies sequential actions.
//
// # Description
//
// Tests that multiple actions can be performed before exiting.
func TestDefaultSessionActionsMenu_Show_MultipleActions(t *testing.T) {
	input := strings.NewReader("1\n2\n3\n4\n") // History, Curl, GraphQL query, Exit
	output := &bytes.Buffer{}
	mockOpener := &MockBrowserOpener{}

	menu := NewSessionActionsMenuWithBrowserOpener(input, output, mockOpener)
	menu.Show("multi-test", "http://localhost:12210")

	// Should have opened 1 URL (only option 1 opens browser now)
	if len(mockOpener.OpenedURLs) != 1 {
		t.Fatalf("expected 1 URL opened, got %d", len(mockOpener.OpenedURLs))
	}

	// Should be history URL
	expectedHistory := "http://localhost:12210/v1/sessions/multi-test/history"
	if mockOpener.OpenedURLs[0] != expectedHistory {
		t.Errorf("expected history URL, got %s", mockOpener.OpenedURLs[0])
	}

	outputStr := output.String()
	// Should also have curl commands and GraphQL query
	if !strings.Contains(outputStr, "Curl Commands") {
		t.Error("expected curl commands in output")
	}
	if !strings.Contains(outputStr, "GraphQL Query") {
		t.Error("expected GraphQL query in output")
	}
}

// TestDefaultSessionActionsMenu_PrintMenuHeader verifies header output.
//
// # Description
//
// Tests that the menu header is formatted correctly.
func TestDefaultSessionActionsMenu_PrintMenuHeader(t *testing.T) {
	input := strings.NewReader("")
	output := &bytes.Buffer{}

	menu := NewDefaultSessionActionsMenu(input, output)
	menu.printMenuHeader()

	outputStr := output.String()
	if !strings.Contains(outputStr, "QUICK ACTIONS") {
		t.Error("expected 'QUICK ACTIONS' in header")
	}
	if !strings.Contains(outputStr, "━") {
		t.Error("expected divider characters in header")
	}
}

// TestDefaultSessionActionsMenu_PrintMenuOptions verifies options output.
//
// # Description
//
// Tests that all menu options are displayed.
func TestDefaultSessionActionsMenu_PrintMenuOptions(t *testing.T) {
	input := strings.NewReader("")
	output := &bytes.Buffer{}

	menu := NewDefaultSessionActionsMenu(input, output)
	menu.printMenuOptions()

	outputStr := output.String()

	expectedOptions := []string{
		"[1] Open Session History",
		"[2] Show all curl commands",
		"[3] Show GraphQL query",
		"[4] Done",
		"Select option [1-4]",
	}

	for _, expected := range expectedOptions {
		if !strings.Contains(outputStr, expected) {
			t.Errorf("expected '%s' in options output", expected)
		}
	}
}

// TestDefaultSessionActionsMenu_ShowCurlCommands verifies curl command output.
//
// # Description
//
// Tests that curl commands include all required elements.
func TestDefaultSessionActionsMenu_ShowCurlCommands(t *testing.T) {
	input := strings.NewReader("")
	output := &bytes.Buffer{}

	menu := NewDefaultSessionActionsMenu(input, output)
	menu.showCurlCommands(
		"abc-123",
		"http://localhost:12210",
		"http://localhost:12127/v1/graphql",
		"http://localhost:12210/v1/sessions/abc-123/history",
	)

	outputStr := output.String()

	// Check for session ID in commands
	if !strings.Contains(outputStr, "abc-123") {
		t.Error("expected session ID in curl commands")
	}

	// Check for required endpoints
	if !strings.Contains(outputStr, "/history") {
		t.Error("expected history endpoint in curl commands")
	}
	if !strings.Contains(outputStr, "/verify") {
		t.Error("expected verify endpoint in curl commands")
	}
	if !strings.Contains(outputStr, "Content-Type: application/json") {
		t.Error("expected Content-Type header in GraphQL command")
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

// BenchmarkDefaultSessionActionsMenu_Show benchmarks menu rendering.
//
// # Description
//
// Measures the performance of menu display and option handling.
func BenchmarkDefaultSessionActionsMenu_Show(b *testing.B) {
	for i := 0; i < b.N; i++ {
		input := strings.NewReader("4\n") // Immediate exit
		output := &bytes.Buffer{}
		mockOpener := &MockBrowserOpener{}

		menu := NewSessionActionsMenuWithBrowserOpener(input, output, mockOpener)
		menu.Show("bench-session", "http://localhost:12210")
	}
}
