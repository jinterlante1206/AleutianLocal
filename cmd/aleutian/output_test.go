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
	"encoding/json"
	"testing"
	"time"
)

// TestPolicyVerifyResultJSON tests that PolicyVerifyResult serializes correctly.
func TestPolicyVerifyResultJSON(t *testing.T) {
	result := PolicyVerifyResult{
		Valid:    true,
		Hash:     "sha256:abc123",
		ByteSize: 1234,
		Version:  "1.0",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal PolicyVerifyResult: %v", err)
	}

	var decoded PolicyVerifyResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal PolicyVerifyResult: %v", err)
	}

	if decoded.Valid != result.Valid {
		t.Errorf("Valid = %v, want %v", decoded.Valid, result.Valid)
	}
	if decoded.Hash != result.Hash {
		t.Errorf("Hash = %s, want %s", decoded.Hash, result.Hash)
	}
	if decoded.ByteSize != result.ByteSize {
		t.Errorf("ByteSize = %d, want %d", decoded.ByteSize, result.ByteSize)
	}
}

// TestPolicyTestResultJSON tests that PolicyTestResult serializes correctly.
func TestPolicyTestResultJSON(t *testing.T) {
	result := PolicyTestResult{
		Input: "test input",
		Matches: []PolicyTestMatch{
			{
				Rule:           "secrets/api-key",
				Severity:       "CRITICAL",
				Match:          "AKIA...",
				Classification: "Secret",
				Confidence:     "High",
				LineNumber:     1,
			},
		},
		Matched: true,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal PolicyTestResult: %v", err)
	}

	var decoded PolicyTestResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal PolicyTestResult: %v", err)
	}

	if decoded.Input != result.Input {
		t.Errorf("Input = %s, want %s", decoded.Input, result.Input)
	}
	if decoded.Matched != result.Matched {
		t.Errorf("Matched = %v, want %v", decoded.Matched, result.Matched)
	}
	if len(decoded.Matches) != len(result.Matches) {
		t.Errorf("Matches len = %d, want %d", len(decoded.Matches), len(result.Matches))
	}
	if decoded.Matches[0].Rule != result.Matches[0].Rule {
		t.Errorf("Matches[0].Rule = %s, want %s", decoded.Matches[0].Rule, result.Matches[0].Rule)
	}
}

// TestSessionListResultJSON tests that SessionListResult serializes correctly.
func TestSessionListResultJSON(t *testing.T) {
	result := SessionListResult{
		Sessions: []SessionSummary{
			{
				ID:        "sess-abc123",
				Summary:   "Test session",
				CreatedAt: "2026-01-24T10:00:00Z",
			},
		},
		Count: 1,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal SessionListResult: %v", err)
	}

	var decoded SessionListResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal SessionListResult: %v", err)
	}

	if decoded.Count != result.Count {
		t.Errorf("Count = %d, want %d", decoded.Count, result.Count)
	}
	if len(decoded.Sessions) != len(result.Sessions) {
		t.Errorf("Sessions len = %d, want %d", len(decoded.Sessions), len(result.Sessions))
	}
	if decoded.Sessions[0].ID != result.Sessions[0].ID {
		t.Errorf("Sessions[0].ID = %s, want %s", decoded.Sessions[0].ID, result.Sessions[0].ID)
	}
}

// TestWeaviateSummaryResultJSON tests that WeaviateSummaryResult serializes correctly.
func TestWeaviateSummaryResultJSON(t *testing.T) {
	result := WeaviateSummaryResult{
		Classes: []WeaviateClassInfo{
			{Name: "Document", Count: 1234},
			{Name: "Chunk", Count: 5678},
		},
		TotalObjects:  6912,
		SchemaVersion: "1.0",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal WeaviateSummaryResult: %v", err)
	}

	var decoded WeaviateSummaryResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal WeaviateSummaryResult: %v", err)
	}

	if decoded.TotalObjects != result.TotalObjects {
		t.Errorf("TotalObjects = %d, want %d", decoded.TotalObjects, result.TotalObjects)
	}
	if len(decoded.Classes) != len(result.Classes) {
		t.Errorf("Classes len = %d, want %d", len(decoded.Classes), len(result.Classes))
	}
}

// TestCommandResultJSON tests that CommandResult serializes correctly.
func TestCommandResultJSON(t *testing.T) {
	result := CommandResult{
		APIVersion: "1.0",
		Command:    "test",
		Timestamp:  time.Now(),
		DurationMs: 100,
		Success:    true,
		Data:       map[string]string{"key": "value"},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal CommandResult: %v", err)
	}

	var decoded CommandResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal CommandResult: %v", err)
	}

	if decoded.APIVersion != result.APIVersion {
		t.Errorf("APIVersion = %s, want %s", decoded.APIVersion, result.APIVersion)
	}
	if decoded.Success != result.Success {
		t.Errorf("Success = %v, want %v", decoded.Success, result.Success)
	}
}

// TestOutputResult_Success tests OutputResult with no error and no findings.
func TestOutputResult_Success(t *testing.T) {
	cfg := OutputConfig{JSON: false, Quiet: true}
	start := time.Now()
	data := map[string]string{"test": "value"}

	exitCode := OutputResult(cfg, "test", start, data, false, nil)

	if exitCode != CLIExitSuccess {
		t.Errorf("Exit code = %d, want %d", exitCode, CLIExitSuccess)
	}
}

// TestOutputResult_Findings tests OutputResult with findings.
func TestOutputResult_Findings(t *testing.T) {
	cfg := OutputConfig{JSON: false, Quiet: true}
	start := time.Now()
	data := map[string]string{"test": "value"}

	exitCode := OutputResult(cfg, "test", start, data, true, nil)

	if exitCode != CLIExitFindings {
		t.Errorf("Exit code = %d, want %d", exitCode, CLIExitFindings)
	}
}

// TestOutputResult_Error tests OutputResult with error.
func TestOutputResult_Error(t *testing.T) {
	cfg := OutputConfig{JSON: false, Quiet: true}
	start := time.Now()

	exitCode := OutputResult(cfg, "test", start, nil, false, bytes.ErrTooLarge)

	if exitCode != CLIExitError {
		t.Errorf("Exit code = %d, want %d", exitCode, CLIExitError)
	}
}

// TestExitCodeConstants tests exit code constant values.
func TestExitCodeConstants(t *testing.T) {
	if CLIExitSuccess != 0 {
		t.Errorf("CLIExitSuccess = %d, want 0", CLIExitSuccess)
	}
	if CLIExitFindings != 1 {
		t.Errorf("CLIExitFindings = %d, want 1", CLIExitFindings)
	}
	if CLIExitError != 2 {
		t.Errorf("CLIExitError = %d, want 2", CLIExitError)
	}
}
