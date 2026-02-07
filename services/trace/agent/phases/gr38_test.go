// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package phases

import (
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/agent"
	"github.com/AleutianAI/AleutianFOSS/services/trace/agent/mcts/crs"
)

// TestGR38_DuplicateToolCallPrevention tests that hard forcing is skipped
// when the tool has already been called in the session.
func TestGR38_DuplicateToolCallPrevention(t *testing.T) {
	t.Run("tool already called prevents hard forcing", func(t *testing.T) {
		// Create a session with existing tool history
		session, err := agent.NewSession("/test/project", nil)
		if err != nil {
			t.Fatalf("Failed to create session: %v", err)
		}

		// Simulate a previous tool call by adding to trace
		session.RecordTraceStep(crs.TraceStep{
			Timestamp: time.Now(),
			Action:    "tool_call",
			Tool:      "find_entry_points",
		})

		// Build tool history (this is what the fix checks)
		history := buildToolHistoryFromSession(session)

		// Check that the tool appears in history
		found := false
		for _, entry := range history {
			if entry.Tool == "find_entry_points" {
				found = true
				break
			}
		}

		if !found {
			t.Error("find_entry_points should be in tool history after being recorded")
		}
	})

	t.Run("empty history allows hard forcing", func(t *testing.T) {
		session, err := agent.NewSession("/test/project", nil)
		if err != nil {
			t.Fatalf("Failed to create session: %v", err)
		}

		// No tool calls recorded
		history := buildToolHistoryFromSession(session)

		if len(history) != 0 {
			t.Errorf("Expected empty history, got %d entries", len(history))
		}
	})

	t.Run("different tool allows hard forcing", func(t *testing.T) {
		session, err := agent.NewSession("/test/project", nil)
		if err != nil {
			t.Fatalf("Failed to create session: %v", err)
		}

		// Record a call to a different tool
		session.RecordTraceStep(crs.TraceStep{
			Timestamp: time.Now(),
			Action:    "tool_call",
			Tool:      "find_symbol",
		})

		history := buildToolHistoryFromSession(session)

		// Check that find_entry_points is NOT in history
		found := false
		for _, entry := range history {
			if entry.Tool == "find_entry_points" {
				found = true
				break
			}
		}

		if found {
			t.Error("find_entry_points should NOT be in history when only find_symbol was called")
		}
	})

	t.Run("forced calls are also counted", func(t *testing.T) {
		session, err := agent.NewSession("/test/project", nil)
		if err != nil {
			t.Fatalf("Failed to create session: %v", err)
		}

		// Record a forced tool call (CB-31d format)
		session.RecordTraceStep(crs.TraceStep{
			Timestamp: time.Now(),
			Action:    "tool_call_forced",
			Tool:      "find_entry_points",
		})

		history := buildToolHistoryFromSession(session)

		// Both tool_call and tool_call_forced should be counted
		found := false
		for _, entry := range history {
			if entry.Tool == "find_entry_points" {
				found = true
				break
			}
		}

		if !found {
			t.Error("tool_call_forced should be counted in tool history (CB-31d fix)")
		}
	})
}

// TestGR38_ToolHistoryLimiting tests that tool history is limited to prevent memory growth.
func TestGR38_ToolHistoryLimiting(t *testing.T) {
	session, err := agent.NewSession("/test/project", nil)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Record many tool calls
	for i := 0; i < maxToolHistoryEntries+10; i++ {
		session.RecordTraceStep(crs.TraceStep{
			Timestamp: time.Now(),
			Action:    "tool_call",
			Tool:      "test_tool",
		})
	}

	history := buildToolHistoryFromSession(session)

	if len(history) > maxToolHistoryEntries {
		t.Errorf("History should be limited to %d, got %d", maxToolHistoryEntries, len(history))
	}
}
