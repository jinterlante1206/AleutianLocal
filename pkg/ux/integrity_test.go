// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package ux

import (
	"testing"
)

// =============================================================================
// SHA256HashComputer Tests
// =============================================================================

func TestSHA256HashComputer_ComputeContentHash(t *testing.T) {
	computer := NewSHA256HashComputer()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "empty string",
			content: "",
			want:    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:    "hello world",
			content: "Hello, World!",
			want:    "dffd6021bb2bd5b0af676290809ec3a53191dd81c7f70a4b28688a362182986f",
		},
		{
			name:    "simple text",
			content: "test",
			want:    "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computer.ComputeContentHash(tt.content)
			if got != tt.want {
				t.Errorf("ComputeContentHash(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestSHA256HashComputer_ComputeEventHash(t *testing.T) {
	computer := NewSHA256HashComputer()

	tests := []struct {
		name      string
		content   string
		createdAt int64
		prevHash  string
	}{
		{
			name:      "first event with empty prevHash",
			content:   "Hello",
			createdAt: 1735657200000,
			prevHash:  "",
		},
		{
			name:      "subsequent event with prevHash",
			content:   "World",
			createdAt: 1735657200001,
			prevHash:  "abc123def456abc123def456abc123def456abc123def456abc123def456abc1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computer.ComputeEventHash(tt.content, tt.createdAt, tt.prevHash)

			// Verify hash is 64 characters (256 bits as hex)
			if len(got) != 64 {
				t.Errorf("ComputeEventHash() returned hash of length %d, want 64", len(got))
			}

			// Verify hash is consistent
			got2 := computer.ComputeEventHash(tt.content, tt.createdAt, tt.prevHash)
			if got != got2 {
				t.Error("ComputeEventHash() should return consistent results")
			}
		})
	}
}

func TestSHA256HashComputer_DifferentInputsDifferentHashes(t *testing.T) {
	computer := NewSHA256HashComputer()

	hash1 := computer.ComputeEventHash("Hello", 1735657200000, "")
	hash2 := computer.ComputeEventHash("World", 1735657200000, "")
	hash3 := computer.ComputeEventHash("Hello", 1735657200001, "")
	hash4 := computer.ComputeEventHash("Hello", 1735657200000, "prev")

	hashes := []string{hash1, hash2, hash3, hash4}
	seen := make(map[string]bool)

	for _, h := range hashes {
		if seen[h] {
			t.Error("different inputs should produce different hashes")
		}
		seen[h] = true
	}
}

// =============================================================================
// FullChainVerifier Tests
// =============================================================================

func TestFullChainVerifier_Verify_EmptyChain(t *testing.T) {
	verifier := NewFullChainVerifier()
	events := []StreamEvent{}

	result := verifier.Verify(events)

	if !result.Valid {
		t.Error("empty chain should be valid")
	}
	if result.ChainLength != 0 {
		t.Errorf("ChainLength = %d, want 0", result.ChainLength)
	}
}

func TestFullChainVerifier_Verify_ValidChainWithRecompute(t *testing.T) {
	verifier := NewFullChainVerifier()
	computer := NewSHA256HashComputer()

	// Build a valid chain with correctly computed hashes
	event1 := StreamEvent{
		Content:   "Hello",
		CreatedAt: 1735657200000,
		PrevHash:  "",
	}
	event1.Hash = computer.ComputeEventHash(event1.Content, event1.CreatedAt, event1.PrevHash)

	event2 := StreamEvent{
		Content:   "World",
		CreatedAt: 1735657200001,
		PrevHash:  event1.Hash,
	}
	event2.Hash = computer.ComputeEventHash(event2.Content, event2.CreatedAt, event2.PrevHash)

	events := []StreamEvent{event1, event2}

	result := verifier.Verify(events)

	if !result.Valid {
		t.Errorf("valid chain with correct hashes should pass: %s", result.ErrorMessage)
	}
	if result.FinalHash != event2.Hash {
		t.Errorf("FinalHash = %q, want %q", result.FinalHash, event2.Hash)
	}
}

func TestFullChainVerifier_Verify_ModifiedContent(t *testing.T) {
	verifier := NewFullChainVerifier()
	computer := NewSHA256HashComputer()

	// Build a chain and then modify content
	event1 := StreamEvent{
		Content:   "Original",
		CreatedAt: 1735657200000,
		PrevHash:  "",
	}
	event1.Hash = computer.ComputeEventHash(event1.Content, event1.CreatedAt, event1.PrevHash)

	// Modify content but keep the old hash
	event1.Content = "Modified"

	events := []StreamEvent{event1}

	result := verifier.Verify(events)

	if result.Valid {
		t.Error("modified content should fail verification")
	}
	if result.InvalidEventIndex != 0 {
		t.Errorf("InvalidEventIndex = %d, want 0", result.InvalidEventIndex)
	}
	if result.ErrorMessage == "" {
		t.Error("ErrorMessage should indicate content modification")
	}
}

func TestFullChainVerifier_Verify_BrokenChainLink(t *testing.T) {
	verifier := NewFullChainVerifier()
	computer := NewSHA256HashComputer()

	event1 := StreamEvent{
		Content:   "Hello",
		CreatedAt: 1735657200000,
		PrevHash:  "",
	}
	event1.Hash = computer.ComputeEventHash(event1.Content, event1.CreatedAt, event1.PrevHash)

	// Create event2 with wrong PrevHash
	event2 := StreamEvent{
		Content:   "World",
		CreatedAt: 1735657200001,
		PrevHash:  "wronghash",
	}
	event2.Hash = computer.ComputeEventHash(event2.Content, event2.CreatedAt, event2.PrevHash)

	events := []StreamEvent{event1, event2}

	result := verifier.Verify(events)

	if result.Valid {
		t.Error("broken chain link should fail verification")
	}
	if result.InvalidEventIndex != 1 {
		t.Errorf("InvalidEventIndex = %d, want 1", result.InvalidEventIndex)
	}
}

// =============================================================================
// IntegrityInfo Tests
// =============================================================================

func TestNewIntegrityInfo(t *testing.T) {
	result := &StreamResult{
		ChainHash:   "chain123",
		ContentHash: "content456",
		TotalEvents: 47,
	}

	info := NewIntegrityInfo(result, true)

	if info.ChainHash != "chain123" {
		t.Errorf("ChainHash = %q, want %q", info.ChainHash, "chain123")
	}
	if info.ContentHash != "content456" {
		t.Errorf("ContentHash = %q, want %q", info.ContentHash, "content456")
	}
	if info.ChainLength != 47 {
		t.Errorf("ChainLength = %d, want 47", info.ChainLength)
	}
	if !info.IntegrityVerified {
		t.Error("IntegrityVerified should be true")
	}
	if info.VerifiedAt == 0 {
		t.Error("VerifiedAt should be set")
	}
	if info.TurnHashes == nil {
		t.Error("TurnHashes should be initialized")
	}
	if info.SourceHashes == nil {
		t.Error("SourceHashes should be initialized")
	}
}

func TestNewIntegrityInfoFromVerification(t *testing.T) {
	verification := &ChainVerificationResult{
		Valid:       true,
		ChainLength: 25,
		FinalHash:   "finalhash123",
	}

	info := NewIntegrityInfoFromVerification(verification)

	if info.ChainHash != "finalhash123" {
		t.Errorf("ChainHash = %q, want %q", info.ChainHash, "finalhash123")
	}
	if info.ChainLength != 25 {
		t.Errorf("ChainLength = %d, want 25", info.ChainLength)
	}
	if !info.IntegrityVerified {
		t.Error("IntegrityVerified should be true")
	}
}

func TestNewIntegrityInfoFromVerification_Failed(t *testing.T) {
	verification := &ChainVerificationResult{
		Valid:        false,
		ChainLength:  10,
		ErrorMessage: "chain broken at event 5",
	}

	info := NewIntegrityInfoFromVerification(verification)

	if info.IntegrityVerified {
		t.Error("IntegrityVerified should be false")
	}
	if info.VerificationError != "chain broken at event 5" {
		t.Errorf("VerificationError = %q, want %q", info.VerificationError, "chain broken at event 5")
	}
}

func TestIntegrityInfo_AddTurnHash(t *testing.T) {
	info := &IntegrityInfo{
		TurnHashes: make(map[int]string),
	}

	info.AddTurnHash(1, "What is 2+2?", "2+2 equals 4.")

	hash, ok := info.GetTurnHash(1)
	if !ok {
		t.Error("turn hash should exist")
	}
	if hash == "" {
		t.Error("turn hash should not be empty")
	}
	if len(hash) != 64 {
		t.Errorf("turn hash length = %d, want 64", len(hash))
	}
}

func TestIntegrityInfo_AddSourceHash(t *testing.T) {
	info := &IntegrityInfo{
		SourceHashes: make(map[string]string),
	}

	info.AddSourceHash("document.pdf", "The content of the document")

	hash, ok := info.GetSourceHash("document.pdf")
	if !ok {
		t.Error("source hash should exist")
	}
	if hash == "" {
		t.Error("source hash should not be empty")
	}
	if len(hash) != 64 {
		t.Errorf("source hash length = %d, want 64", len(hash))
	}
}

func TestIntegrityInfo_GetTurnHash_NotFound(t *testing.T) {
	info := &IntegrityInfo{
		TurnHashes: make(map[int]string),
	}

	_, ok := info.GetTurnHash(99)
	if ok {
		t.Error("non-existent turn should return false")
	}
}

func TestIntegrityInfo_GetSourceHash_NotFound(t *testing.T) {
	info := &IntegrityInfo{
		SourceHashes: make(map[string]string),
	}

	_, ok := info.GetSourceHash("nonexistent.pdf")
	if ok {
		t.Error("non-existent source should return false")
	}
}

func TestIntegrityInfo_FormatForDisplay_Verified(t *testing.T) {
	info := &IntegrityInfo{
		ChainHash:         "a3f2c8d9e1b4f7a6c5d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0",
		ChainLength:       47,
		IntegrityVerified: true,
	}

	display := info.FormatForDisplay()

	if display == "" {
		t.Error("display should not be empty")
	}
	// Should contain verification status
	if !containsSubstring(display, "Verified") {
		t.Error("display should contain 'Verified'")
	}
	// Should contain chain length
	if !containsSubstring(display, "47") {
		t.Error("display should contain chain length")
	}
}

func TestIntegrityInfo_FormatForDisplay_Failed(t *testing.T) {
	info := &IntegrityInfo{
		ChainHash:         "abc123",
		ChainLength:       10,
		IntegrityVerified: false,
	}

	display := info.FormatForDisplay()

	if !containsSubstring(display, "FAILED") {
		t.Error("display should contain 'FAILED'")
	}
}

func TestIntegrityInfo_FormatForDisplay_EmptyHash(t *testing.T) {
	info := &IntegrityInfo{
		ChainHash:         "",
		ChainLength:       0,
		IntegrityVerified: true,
	}

	display := info.FormatForDisplay()

	if !containsSubstring(display, "N/A") {
		t.Error("display should contain 'N/A' for empty hash")
	}
}

// =============================================================================
// truncateHash Tests
// =============================================================================

func TestTruncateHash_ShortHash(t *testing.T) {
	short := "abc123"
	result := truncateHash(short)

	if result != short {
		t.Errorf("short hash should not be truncated: got %q, want %q", result, short)
	}
}

func TestTruncateHash_LongHash(t *testing.T) {
	long := "a3f2c8d9e1b4f7a6c5d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0"
	result := truncateHash(long)

	if len(result) >= len(long) {
		t.Error("long hash should be truncated")
	}
	// Should start with first 8 chars
	if result[:8] != "a3f2c8d9" {
		t.Errorf("truncated hash should start with 'a3f2c8d9', got %q", result[:8])
	}
	// Should end with last 4 chars
	if result[len(result)-4:] != "a9b0" {
		t.Errorf("truncated hash should end with 'a9b0', got %q", result[len(result)-4:])
	}
	// Should contain ellipsis
	if !containsSubstring(result, "...") {
		t.Error("truncated hash should contain '...'")
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstringHelper(s, substr))
}

func containsSubstringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
