// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package verify

import (
	"testing"
)

func TestDetermineRebuildStrategy(t *testing.T) {
	tests := []struct {
		name       string
		staleCount int
		totalFiles int
		want       RebuildStrategy
	}{
		// Zero stale
		{
			name:       "0 stale files",
			staleCount: 0,
			totalFiles: 100,
			want:       StrategyNone,
		},

		// Inline silent (1-3)
		{
			name:       "1 stale file",
			staleCount: 1,
			totalFiles: 100,
			want:       StrategyInlineSilent,
		},
		{
			name:       "2 stale files",
			staleCount: 2,
			totalFiles: 100,
			want:       StrategyInlineSilent,
		},
		{
			name:       "3 stale files",
			staleCount: 3,
			totalFiles: 100,
			want:       StrategyInlineSilent,
		},

		// Inline with status (4-10)
		{
			name:       "4 stale files",
			staleCount: 4,
			totalFiles: 100,
			want:       StrategyInlineWithStatus,
		},
		{
			name:       "10 stale files",
			staleCount: 10,
			totalFiles: 100,
			want:       StrategyInlineWithStatus,
		},

		// Background partial (11-50 or >20%)
		{
			name:       "11 stale files",
			staleCount: 11,
			totalFiles: 100,
			want:       StrategyBackgroundPartial,
		},
		{
			name:       "50 stale files",
			staleCount: 50,
			totalFiles: 100,
			want:       StrategyBackgroundPartial,
		},
		{
			name:       ">20% stale (25 of 100)",
			staleCount: 25,
			totalFiles: 100,
			want:       StrategyBackgroundPartial,
		},

		// Full rebuild (>50%)
		{
			name:       ">50% stale (51 of 100)",
			staleCount: 51,
			totalFiles: 100,
			want:       StrategyFullRebuild,
		},
		{
			name:       "75% stale",
			staleCount: 75,
			totalFiles: 100,
			want:       StrategyFullRebuild,
		},
		{
			name:       "100% stale",
			staleCount: 100,
			totalFiles: 100,
			want:       StrategyFullRebuild,
		},

		// Edge cases
		{
			name:       "0 total files with stale",
			staleCount: 5,
			totalFiles: 0,
			want:       StrategyInlineSilent,
		},
		{
			name:       "0 total files, 0 stale",
			staleCount: 0,
			totalFiles: 0,
			want:       StrategyNone,
		},
		{
			name:       "small project (5 files), 3 stale",
			staleCount: 3,
			totalFiles: 5,
			want:       StrategyFullRebuild, // 60% stale
		},
		{
			name:       "small project (10 files), 3 stale",
			staleCount: 3,
			totalFiles: 10,
			want:       StrategyInlineSilent, // Only 30%, but count-based is 3
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineRebuildStrategy(tt.staleCount, tt.totalFiles)
			if got != tt.want {
				t.Errorf("DetermineRebuildStrategy(%d, %d) = %v, want %v",
					tt.staleCount, tt.totalFiles, got, tt.want)
			}
		})
	}
}

func TestDetermineRebuildStrategyFromResult(t *testing.T) {
	t.Run("nil result returns StrategyNone", func(t *testing.T) {
		got := DetermineRebuildStrategyFromResult(nil, 100)
		if got != StrategyNone {
			t.Errorf("got %v, want StrategyNone", got)
		}
	})

	t.Run("result with stale files", func(t *testing.T) {
		result := &VerifyResult{
			StaleFiles:   []string{"a.go", "b.go"},
			DeletedFiles: []string{"c.go"},
		}

		got := DetermineRebuildStrategyFromResult(result, 100)
		if got != StrategyInlineSilent {
			t.Errorf("got %v, want StrategyInlineSilent", got)
		}
	})

	t.Run("result with deleted files only", func(t *testing.T) {
		result := &VerifyResult{
			DeletedFiles: []string{"a.go"},
		}

		got := DetermineRebuildStrategyFromResult(result, 100)
		if got != StrategyInlineSilent {
			t.Errorf("got %v, want StrategyInlineSilent", got)
		}
	})
}

func TestRebuildStrategy_String(t *testing.T) {
	tests := []struct {
		strategy RebuildStrategy
		want     string
	}{
		{StrategyNone, "none"},
		{StrategyInlineSilent, "inline_silent"},
		{StrategyInlineWithStatus, "inline_with_status"},
		{StrategyBackgroundPartial, "background_partial"},
		{StrategyFullRebuild, "full_rebuild"},
		{RebuildStrategy(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.strategy.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStrategyDescription(t *testing.T) {
	tests := []struct {
		strategy RebuildStrategy
		wantLen  int // Just check it's non-empty
	}{
		{StrategyNone, 10},
		{StrategyInlineSilent, 10},
		{StrategyInlineWithStatus, 10},
		{StrategyBackgroundPartial, 10},
		{StrategyFullRebuild, 10},
	}

	for _, tt := range tests {
		t.Run(tt.strategy.String(), func(t *testing.T) {
			got := StrategyDescription(tt.strategy)
			if len(got) < tt.wantLen {
				t.Errorf("StrategyDescription(%v) = %q, want len >= %d",
					tt.strategy, got, tt.wantLen)
			}
		})
	}
}

func TestIsInline(t *testing.T) {
	tests := []struct {
		strategy RebuildStrategy
		want     bool
	}{
		{StrategyNone, false},
		{StrategyInlineSilent, true},
		{StrategyInlineWithStatus, true},
		{StrategyBackgroundPartial, false},
		{StrategyFullRebuild, false},
	}

	for _, tt := range tests {
		t.Run(tt.strategy.String(), func(t *testing.T) {
			got := IsInline(tt.strategy)
			if got != tt.want {
				t.Errorf("IsInline(%v) = %v, want %v", tt.strategy, got, tt.want)
			}
		})
	}
}

func TestNeedsProgress(t *testing.T) {
	tests := []struct {
		strategy RebuildStrategy
		want     bool
	}{
		{StrategyNone, false},
		{StrategyInlineSilent, false},
		{StrategyInlineWithStatus, true},
		{StrategyBackgroundPartial, true},
		{StrategyFullRebuild, true},
	}

	for _, tt := range tests {
		t.Run(tt.strategy.String(), func(t *testing.T) {
			got := NeedsProgress(tt.strategy)
			if got != tt.want {
				t.Errorf("NeedsProgress(%v) = %v, want %v", tt.strategy, got, tt.want)
			}
		})
	}
}
