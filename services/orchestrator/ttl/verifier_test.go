// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package ttl

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// =============================================================================
// SEC-005: Deletion Verification Tests
// =============================================================================

// TestDeletionVerifier_VerifyDocumentDeleted_NotFound tests that verification
// succeeds when the document is confirmed deleted (not found on first check).
func TestDeletionVerifier_VerifyDocumentDeleted_NotFound(t *testing.T) {
	existsFunc := func(_ context.Context, className, id string) (bool, error) {
		if className != "Document" {
			t.Errorf("Expected className 'Document', got %q", className)
		}
		return false, nil // Not found = confirmed deleted
	}

	verifier := NewDeletionVerifier(existsFunc, 10*time.Millisecond, 3)

	verified, err := verifier.VerifyDocumentDeleted(context.Background(), "test-uuid-123")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !verified {
		t.Error("Expected verified=true when document not found")
	}
}

// TestDeletionVerifier_VerifyDocumentDeleted_StillExists tests that verification
// fails when the document still exists after all retry attempts.
func TestDeletionVerifier_VerifyDocumentDeleted_StillExists(t *testing.T) {
	var attempts int32
	existsFunc := func(_ context.Context, _, _ string) (bool, error) {
		atomic.AddInt32(&attempts, 1)
		return true, nil // Object still exists
	}

	verifier := NewDeletionVerifier(existsFunc, 1*time.Millisecond, 3)

	verified, err := verifier.VerifyDocumentDeleted(context.Background(), "test-uuid-456")
	if err == nil {
		t.Fatal("Expected error when document still exists")
	}
	if verified {
		t.Error("Expected verified=false when document still exists")
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("Expected 3 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

// TestDeletionVerifier_VerifyDocumentDeleted_RetriesOnError tests that the
// verifier retries when the existence check returns an error, and succeeds
// when a subsequent attempt confirms deletion.
func TestDeletionVerifier_VerifyDocumentDeleted_RetriesOnError(t *testing.T) {
	var attempts int32
	existsFunc := func(_ context.Context, _, _ string) (bool, error) {
		attempt := atomic.AddInt32(&attempts, 1)
		if attempt <= 2 {
			return false, fmt.Errorf("network timeout")
		}
		return false, nil // Third attempt: confirmed deleted
	}

	verifier := NewDeletionVerifier(existsFunc, 1*time.Millisecond, 3)

	verified, err := verifier.VerifyDocumentDeleted(context.Background(), "test-uuid-789")
	if err != nil {
		t.Fatalf("Expected no error on eventual success, got: %v", err)
	}
	if !verified {
		t.Error("Expected verified=true after retries succeed")
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("Expected 3 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

// TestDeletionVerifier_VerifyDocumentDeleted_AllRetriesFail tests that the
// verifier returns an error when all retries fail with errors.
func TestDeletionVerifier_VerifyDocumentDeleted_AllRetriesFail(t *testing.T) {
	existsFunc := func(_ context.Context, _, _ string) (bool, error) {
		return false, fmt.Errorf("connection refused")
	}

	verifier := NewDeletionVerifier(existsFunc, 1*time.Millisecond, 3)

	verified, err := verifier.VerifyDocumentDeleted(context.Background(), "test-uuid-err")
	if err == nil {
		t.Fatal("Expected error when all retries fail")
	}
	if verified {
		t.Error("Expected verified=false when all retries fail")
	}
}

// TestDeletionVerifier_VerifySessionDeleted_NotFound tests that session
// verification succeeds when the session is confirmed deleted.
func TestDeletionVerifier_VerifySessionDeleted_NotFound(t *testing.T) {
	existsFunc := func(_ context.Context, className, id string) (bool, error) {
		if className != "Session" {
			t.Errorf("Expected className 'Session', got %q", className)
		}
		return false, nil
	}

	verifier := NewDeletionVerifier(existsFunc, 10*time.Millisecond, 3)

	verified, err := verifier.VerifySessionDeleted(context.Background(), "session-uuid-123")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !verified {
		t.Error("Expected verified=true when session not found")
	}
}

// TestDeletionVerifier_VerifySessionDeleted_StillExists tests that session
// verification fails when the session still exists after retries.
func TestDeletionVerifier_VerifySessionDeleted_StillExists(t *testing.T) {
	existsFunc := func(_ context.Context, _, _ string) (bool, error) {
		return true, nil
	}

	verifier := NewDeletionVerifier(existsFunc, 1*time.Millisecond, 2)

	verified, err := verifier.VerifySessionDeleted(context.Background(), "session-uuid-456")
	if err == nil {
		t.Fatal("Expected error when session still exists")
	}
	if verified {
		t.Error("Expected verified=false when session still exists")
	}
}

// TestDeletionVerifier_ContextCancellation tests that the verifier respects
// context cancellation between retry attempts.
func TestDeletionVerifier_ContextCancellation(t *testing.T) {
	var attempts int32
	existsFunc := func(_ context.Context, _, _ string) (bool, error) {
		atomic.AddInt32(&attempts, 1)
		return true, nil // Always exists, forcing retries
	}

	verifier := NewDeletionVerifier(existsFunc, 50*time.Millisecond, 5)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after first attempt has time to complete but before second retry delay
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	verified, err := verifier.VerifyDocumentDeleted(ctx, "test-uuid-cancel")
	if err == nil {
		t.Fatal("Expected error on context cancellation")
	}
	if verified {
		t.Error("Expected verified=false on context cancellation")
	}
	// Should have attempted at least once but not all 5
	a := atomic.LoadInt32(&attempts)
	if a == 0 {
		t.Error("Expected at least 1 attempt before cancellation")
	}
	if a >= 5 {
		t.Error("Expected fewer than 5 attempts due to cancellation")
	}
}

// TestDeletionVerifier_DefaultValues tests that zero values for retryDelay
// and maxRetries use sensible defaults.
func TestDeletionVerifier_DefaultValues(t *testing.T) {
	var attempts int32
	existsFunc := func(_ context.Context, _, _ string) (bool, error) {
		atomic.AddInt32(&attempts, 1)
		return true, nil
	}

	// Pass 0 for both - should default to 100ms delay and 3 retries
	verifier := NewDeletionVerifier(existsFunc, 0, 0)

	start := time.Now()
	_, _ = verifier.VerifyDocumentDeleted(context.Background(), "test-uuid-defaults")
	elapsed := time.Since(start)

	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("Expected 3 attempts (default), got %d", atomic.LoadInt32(&attempts))
	}

	// Should have waited ~200ms (2 delays between 3 attempts)
	if elapsed < 150*time.Millisecond {
		t.Errorf("Expected at least 150ms delay with default 100ms retry, got %v", elapsed)
	}
}

// TestDeletionVerifier_EventuallyDeleted tests that the verifier succeeds
// when the object disappears on the second attempt (simulating replication lag).
func TestDeletionVerifier_EventuallyDeleted(t *testing.T) {
	var attempts int32
	existsFunc := func(_ context.Context, _, _ string) (bool, error) {
		attempt := atomic.AddInt32(&attempts, 1)
		if attempt == 1 {
			return true, nil // First check: still exists (replication lag)
		}
		return false, nil // Second check: confirmed deleted
	}

	verifier := NewDeletionVerifier(existsFunc, 1*time.Millisecond, 3)

	verified, err := verifier.VerifyDocumentDeleted(context.Background(), "test-uuid-lag")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if !verified {
		t.Error("Expected verified=true after eventual deletion")
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("Expected 2 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

// TestNoopDeletionVerifier_AlwaysVerified tests that the no-op verifier
// always returns success.
func TestNoopDeletionVerifier_AlwaysVerified(t *testing.T) {
	verifier := NewNoopDeletionVerifier()

	verified, err := verifier.VerifyDocumentDeleted(context.Background(), "any-uuid")
	if err != nil {
		t.Fatalf("Noop verifier should not return error, got: %v", err)
	}
	if !verified {
		t.Error("Noop verifier should always return verified=true")
	}

	verified, err = verifier.VerifySessionDeleted(context.Background(), "any-session-uuid")
	if err != nil {
		t.Fatalf("Noop verifier should not return error for sessions, got: %v", err)
	}
	if !verified {
		t.Error("Noop verifier should always return verified=true for sessions")
	}
}

// TestDeletionVerifier_ConcurrentVerification tests that the verifier is
// safe for concurrent use.
func TestDeletionVerifier_ConcurrentVerification(t *testing.T) {
	existsFunc := func(_ context.Context, _, _ string) (bool, error) {
		return false, nil // Always deleted
	}

	verifier := NewDeletionVerifier(existsFunc, 1*time.Millisecond, 3)

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			verified, err := verifier.VerifyDocumentDeleted(
				context.Background(),
				fmt.Sprintf("uuid-%d", id),
			)
			if err != nil || !verified {
				t.Errorf("Concurrent verification failed for id %d", id)
			}
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
