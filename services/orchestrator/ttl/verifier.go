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
	"log/slog"
	"time"
)

// =============================================================================
// SEC-005: Deletion Verification
// =============================================================================

// ObjectExistsFunc is a function type for checking whether an object exists
// in Weaviate by class name and ID.
//
// # Description
//
// This function type decouples the verifier from the concrete Weaviate client,
// allowing unit tests to inject mock implementations without needing to mock
// the entire Weaviate client.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - className: Weaviate class name ("Document" or "Session").
//   - id: Weaviate object UUID.
//
// # Outputs
//
//   - bool: True if the object exists.
//   - error: Non-nil if the check itself fails (network error, etc).
type ObjectExistsFunc func(ctx context.Context, className, id string) (bool, error)

// deletionVerifier implements DeletionVerifier with configurable retry behavior.
//
// # Description
//
// Performs read-after-delete checks with retry logic to handle Weaviate
// replication lag. If the first check finds the object still exists, it
// waits retryDelay before trying again, up to maxRetries attempts.
//
// # Fields
//
//   - existsFunc: Function to check object existence (injectable for testing).
//   - retryDelay: Time to wait between retry attempts.
//   - maxRetries: Maximum number of verification attempts.
//
// # Thread Safety
//
// All methods are safe for concurrent use (no shared mutable state).
type deletionVerifier struct {
	existsFunc ObjectExistsFunc
	retryDelay time.Duration
	maxRetries int
}

// NewDeletionVerifier creates a verifier with configurable retry behavior.
//
// # Description
//
// Creates a verifier that performs read-after-delete checks using the provided
// ObjectExistsFunc. Retries with the configured delay between attempts.
//
// # Inputs
//
//   - existsFunc: Function to check if an object exists.
//   - retryDelay: Time to wait between retries. If 0, defaults to 100ms.
//   - maxRetries: Maximum verification attempts. If 0, defaults to 3.
//
// # Outputs
//
//   - DeletionVerifier: Ready to verify deletions.
//
// # Examples
//
//	verifier := NewDeletionVerifier(weaviateExistsFunc, 100*time.Millisecond, 3)
//	verified, err := verifier.VerifyDocumentDeleted(ctx, "abc-123")
//	if err != nil || !verified {
//	    // Handle verification failure
//	}
//
// # Limitations
//
//   - Adds latency to deletion operations (retryDelay × maxRetries in worst case).
//   - Cannot distinguish between "object never existed" and "object was deleted".
func NewDeletionVerifier(existsFunc ObjectExistsFunc, retryDelay time.Duration, maxRetries int) DeletionVerifier {
	if retryDelay == 0 {
		retryDelay = 100 * time.Millisecond
	}
	if maxRetries == 0 {
		maxRetries = 3
	}
	return &deletionVerifier{
		existsFunc: existsFunc,
		retryDelay: retryDelay,
		maxRetries: maxRetries,
	}
}

// VerifyDocumentDeleted confirms a document no longer exists in Weaviate.
//
// # Description
//
// Attempts to read the document by ID. If not found, deletion is confirmed.
// Retries with configured delay to handle replication lag.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - weaviateID: UUID of the document to verify.
//
// # Outputs
//
//   - bool: True if document is confirmed deleted (not found after all attempts).
//   - error: Non-nil if verification fails (object still exists after retries,
//     or context was cancelled).
//
// # Limitations
//
//   - Adds latency proportional to retryDelay × attempts needed.
//   - May report false negative if Weaviate is experiencing transient issues.
func (v *deletionVerifier) VerifyDocumentDeleted(ctx context.Context, weaviateID string) (bool, error) {
	return v.verifyDeleted(ctx, "Document", weaviateID)
}

// VerifySessionDeleted confirms a session no longer exists in Weaviate.
//
// # Description
//
// Attempts to read the session by ID. If not found, deletion is confirmed.
// Retries with configured delay to handle replication lag.
//
// # Inputs
//
//   - ctx: Context for cancellation and timeout.
//   - weaviateID: UUID of the session to verify.
//
// # Outputs
//
//   - bool: True if session is confirmed deleted (not found after all attempts).
//   - error: Non-nil if verification fails (object still exists after retries,
//     or context was cancelled).
func (v *deletionVerifier) VerifySessionDeleted(ctx context.Context, weaviateID string) (bool, error) {
	return v.verifyDeleted(ctx, "Session", weaviateID)
}

// verifyDeleted is the shared implementation for document and session verification.
//
// # Description
//
// Performs read-after-delete with retry logic. On each attempt:
// 1. Calls existsFunc to check if the object is still present
// 2. If not found (exists=false, err=nil), returns true (confirmed deleted)
// 3. If error occurs, logs and retries
// 4. If object still exists, waits retryDelay and retries
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - className: Weaviate class name ("Document" or "Session").
//   - weaviateID: UUID of the object to verify.
//
// # Outputs
//
//   - bool: True if confirmed deleted.
//   - error: Non-nil if object still exists after all retries or context cancelled.
func (v *deletionVerifier) verifyDeleted(ctx context.Context, className, weaviateID string) (bool, error) {
	var lastErr error

	for attempt := 0; attempt < v.maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-time.After(v.retryDelay):
			}
		}

		exists, err := v.existsFunc(ctx, className, weaviateID)
		if err != nil {
			lastErr = err
			slog.Debug("ttl.verifier: existence check error, retrying",
				"class", className,
				"weaviate_id", weaviateID,
				"attempt", attempt+1,
				"error", err,
			)
			continue
		}

		if !exists {
			return true, nil // Confirmed deleted
		}

		// Object still exists, will retry
		slog.Debug("ttl.verifier: object still exists, retrying",
			"class", className,
			"weaviate_id", weaviateID,
			"attempt", attempt+1,
		)
	}

	// All retries exhausted
	if lastErr != nil {
		return false, fmt.Errorf("verification failed for %s %s after %d attempts: %w",
			className, weaviateID, v.maxRetries, lastErr)
	}
	return false, fmt.Errorf("%s %s still exists after %d verification attempts",
		className, weaviateID, v.maxRetries)
}

// NewNoopDeletionVerifier creates a verifier that always confirms deletion.
//
// # Description
//
// Returns a no-op verifier that always reports success. Use this when
// deletion verification is not needed (e.g., testing, or when the caller
// handles verification separately).
//
// # Outputs
//
//   - DeletionVerifier: Always returns (true, nil) for all checks.
func NewNoopDeletionVerifier() DeletionVerifier {
	return &noopDeletionVerifier{}
}

// noopDeletionVerifier always confirms deletion without checking.
type noopDeletionVerifier struct{}

func (v *noopDeletionVerifier) VerifyDocumentDeleted(_ context.Context, _ string) (bool, error) {
	return true, nil
}

func (v *noopDeletionVerifier) VerifySessionDeleted(_ context.Context, _ string) (bool, error) {
	return true, nil
}
