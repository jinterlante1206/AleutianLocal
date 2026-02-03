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

// Strategy thresholds (configurable via environment variables).
const (
	// InlineSilentThreshold is the max stale files for silent inline rebuild.
	InlineSilentThreshold = 3

	// InlineWithStatusThreshold is the max stale files for inline with status.
	InlineWithStatusThreshold = 10

	// BackgroundPartialThreshold is the max stale files before considering percentage.
	BackgroundPartialThreshold = 50

	// FullRebuildRatio is the percentage of stale files triggering full rebuild.
	FullRebuildRatio = 0.5

	// BackgroundPartialRatio is the percentage triggering background partial rebuild.
	BackgroundPartialRatio = 0.2
)

// DetermineRebuildStrategy determines the appropriate rebuild strategy
// based on the number of stale files and total files in the graph.
//
// Description:
//
//	Analyzes the verification result to determine how to handle stale files.
//	The strategy balances user experience (quick responses) with accuracy
//	(fresh data).
//
// Inputs:
//
//	staleCount - Number of stale or deleted files.
//	totalFiles - Total number of files in the manifest/graph.
//
// Outputs:
//
//	RebuildStrategy - The recommended strategy for handling staleness.
//
// Strategy Rules:
//
//	0 stale          → StrategyNone (no action needed)
//	1-3 stale        → StrategyInlineSilent (rebuild silently, ~20-50ms)
//	4-10 stale       → StrategyInlineWithStatus (show "updating...")
//	11-50 stale      → StrategyBackgroundPartial (async rebuild)
//	>20% stale       → StrategyBackgroundPartial (async rebuild)
//	>50% stale       → StrategyFullRebuild (complete rebuild)
//
// Example:
//
//	strategy := DetermineRebuildStrategy(2, 100)  // StrategyInlineSilent
//	strategy := DetermineRebuildStrategy(15, 100) // StrategyBackgroundPartial
//	strategy := DetermineRebuildStrategy(60, 100) // StrategyFullRebuild
func DetermineRebuildStrategy(staleCount, totalFiles int) RebuildStrategy {
	if staleCount == 0 {
		return StrategyNone
	}

	// Handle edge case of empty or very small project
	if totalFiles == 0 {
		if staleCount > 0 {
			return StrategyInlineSilent
		}
		return StrategyNone
	}

	// Check percentage-based thresholds first
	ratio := float64(staleCount) / float64(totalFiles)

	// >50% stale → full rebuild
	if ratio > FullRebuildRatio {
		return StrategyFullRebuild
	}

	// Check count-based thresholds
	if staleCount <= InlineSilentThreshold {
		return StrategyInlineSilent
	}

	if staleCount <= InlineWithStatusThreshold {
		return StrategyInlineWithStatus
	}

	// >20% stale OR >10 files → background partial
	if ratio > BackgroundPartialRatio || staleCount > InlineWithStatusThreshold {
		return StrategyBackgroundPartial
	}

	return StrategyInlineWithStatus
}

// DetermineRebuildStrategyFromResult is a convenience function that
// determines strategy from a VerifyResult.
//
// Description:
//
//	Extracts stale count from the VerifyResult and determines the
//	appropriate rebuild strategy.
//
// Inputs:
//
//	result - The verification result.
//	totalFiles - Total number of files in the manifest/graph.
//
// Outputs:
//
//	RebuildStrategy - The recommended strategy.
func DetermineRebuildStrategyFromResult(result *VerifyResult, totalFiles int) RebuildStrategy {
	if result == nil {
		return StrategyNone
	}
	return DetermineRebuildStrategy(result.StaleCount(), totalFiles)
}

// StrategyDescription returns a user-friendly description of the strategy.
//
// Inputs:
//
//	strategy - The rebuild strategy.
//
// Outputs:
//
//	string - Human-readable description.
func StrategyDescription(strategy RebuildStrategy) string {
	switch strategy {
	case StrategyNone:
		return "No rebuild needed"
	case StrategyInlineSilent:
		return "Quick inline rebuild (silent)"
	case StrategyInlineWithStatus:
		return "Inline rebuild with status updates"
	case StrategyBackgroundPartial:
		return "Background partial rebuild"
	case StrategyFullRebuild:
		return "Full rebuild from scratch"
	default:
		return "Unknown strategy"
	}
}

// IsInline returns true if the strategy can be performed inline.
//
// Inputs:
//
//	strategy - The rebuild strategy.
//
// Outputs:
//
//	bool - True if inline execution is appropriate.
func IsInline(strategy RebuildStrategy) bool {
	return strategy == StrategyInlineSilent || strategy == StrategyInlineWithStatus
}

// NeedsProgress returns true if the strategy should show progress.
//
// Inputs:
//
//	strategy - The rebuild strategy.
//
// Outputs:
//
//	bool - True if progress updates should be shown.
func NeedsProgress(strategy RebuildStrategy) bool {
	return strategy == StrategyInlineWithStatus ||
		strategy == StrategyBackgroundPartial ||
		strategy == StrategyFullRebuild
}
