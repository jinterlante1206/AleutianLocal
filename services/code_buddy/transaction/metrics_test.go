// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package transaction

import (
	"context"
	"testing"
	"time"
)

func TestRecordBegin(t *testing.T) {
	ctx := context.Background()

	t.Run("records success", func(t *testing.T) {
		SetMetricsEnabled(true)
		// Should not panic
		recordBegin(ctx, StrategyBranch, true)
	})

	t.Run("records failure", func(t *testing.T) {
		SetMetricsEnabled(true)
		// Should not panic
		recordBegin(ctx, StrategyStash, false)
	})

	t.Run("skips when disabled", func(t *testing.T) {
		SetMetricsEnabled(false)
		// Should not panic
		recordBegin(ctx, StrategyBranch, true)
		SetMetricsEnabled(true) // Restore
	})
}

func TestRecordCommit(t *testing.T) {
	ctx := context.Background()

	t.Run("records success", func(t *testing.T) {
		SetMetricsEnabled(true)
		// Should not panic
		recordCommit(ctx, 5*time.Second, 10, true)
	})

	t.Run("records failure", func(t *testing.T) {
		SetMetricsEnabled(true)
		// Should not panic
		recordCommit(ctx, 3*time.Second, 5, false)
	})

	t.Run("skips when disabled", func(t *testing.T) {
		SetMetricsEnabled(false)
		// Should not panic
		recordCommit(ctx, time.Second, 1, true)
		SetMetricsEnabled(true) // Restore
	})
}

func TestRecordRollback(t *testing.T) {
	ctx := context.Background()

	t.Run("records with user reason", func(t *testing.T) {
		SetMetricsEnabled(true)
		// Should not panic
		recordRollback(ctx, 2*time.Second, 3, "user requested")
	})

	t.Run("records with expired reason", func(t *testing.T) {
		SetMetricsEnabled(true)
		// Should not panic
		recordRollback(ctx, 30*time.Minute, 20, "transaction expired")
	})

	t.Run("records with manager close reason", func(t *testing.T) {
		SetMetricsEnabled(true)
		// Should not panic
		recordRollback(ctx, time.Minute, 5, "manager closed")
	})

	t.Run("skips when disabled", func(t *testing.T) {
		SetMetricsEnabled(false)
		// Should not panic
		recordRollback(ctx, time.Second, 1, "test")
		SetMetricsEnabled(true) // Restore
	})
}

func TestRecordExpired(t *testing.T) {
	ctx := context.Background()

	t.Run("records expiration", func(t *testing.T) {
		SetMetricsEnabled(true)
		// Should not panic
		recordExpired(ctx)
	})

	t.Run("skips when disabled", func(t *testing.T) {
		SetMetricsEnabled(false)
		// Should not panic
		recordExpired(ctx)
		SetMetricsEnabled(true) // Restore
	})
}

func TestRecordGitOp(t *testing.T) {
	ctx := context.Background()

	t.Run("records success", func(t *testing.T) {
		SetMetricsEnabled(true)
		// Should not panic
		recordGitOp(ctx, "create_branch", 50*time.Millisecond, nil)
	})

	t.Run("records failure", func(t *testing.T) {
		SetMetricsEnabled(true)
		// Should not panic
		recordGitOp(ctx, "reset_hard", 100*time.Millisecond, ErrNotGitRepository)
	})

	t.Run("skips when disabled", func(t *testing.T) {
		SetMetricsEnabled(false)
		// Should not panic
		recordGitOp(ctx, "commit", time.Millisecond, nil)
		SetMetricsEnabled(true) // Restore
	})
}

func TestIncDecActive(t *testing.T) {
	ctx := context.Background()

	t.Run("increments active gauge", func(t *testing.T) {
		SetMetricsEnabled(true)
		// Should not panic
		incActive(ctx)
	})

	t.Run("decrements active gauge", func(t *testing.T) {
		SetMetricsEnabled(true)
		// Should not panic
		decActive(ctx)
	})

	t.Run("skips when disabled", func(t *testing.T) {
		SetMetricsEnabled(false)
		// Should not panic
		incActive(ctx)
		decActive(ctx)
		SetMetricsEnabled(true) // Restore
	})
}

func TestNormalizeRollbackReason(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"transaction expired", "expired"},
		{"manager closed", "manager_close"},
		{"stale transaction cleanup", "cleanup"},
		{"user requested rollback", "user"},
		{"validation failed", "user"},
		{"", "user"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := normalizeRollbackReason(tc.input)
			if result != tc.expected {
				t.Errorf("normalizeRollbackReason(%q) = %q; want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestInitMetrics(t *testing.T) {
	// Test that initMetrics is idempotent (can be called multiple times)
	t.Run("idempotent initialization", func(t *testing.T) {
		err1 := initMetrics()
		err2 := initMetrics()

		// Both should return the same result
		if (err1 == nil) != (err2 == nil) {
			t.Errorf("initMetrics not idempotent: first=%v, second=%v", err1, err2)
		}
	})
}
