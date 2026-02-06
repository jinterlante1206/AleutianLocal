// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package context

import (
	"context"
	"time"
)

// IntegrityChecker validates the hierarchical summary structure.
//
// Thread Safety: Safe for concurrent use.
type IntegrityChecker struct {
	cache     *SummaryCache
	hierarchy LanguageHierarchy
}

// NewIntegrityChecker creates a new integrity checker.
//
// Inputs:
//   - cache: The summary cache to check.
//   - hierarchy: The language hierarchy for validation.
//
// Outputs:
//   - *IntegrityChecker: A new checker instance.
func NewIntegrityChecker(cache *SummaryCache, hierarchy LanguageHierarchy) *IntegrityChecker {
	return &IntegrityChecker{
		cache:     cache,
		hierarchy: hierarchy,
	}
}

// IntegrityReport contains the results of an integrity check.
type IntegrityReport struct {
	// Valid is true if no integrity issues were found.
	Valid bool `json:"valid"`

	// OrphanedChildren are summaries whose parents don't exist.
	OrphanedChildren []string `json:"orphaned_children,omitempty"`

	// MissingChildren are parent-child references where the child doesn't exist.
	MissingChildren []MissingChild `json:"missing_children,omitempty"`

	// LevelMismatches are entities with incorrect hierarchy levels.
	LevelMismatches []LevelMismatch `json:"level_mismatches,omitempty"`

	// StaleEntries are summaries with outdated hashes.
	StaleEntries []StaleEntry `json:"stale_entries,omitempty"`

	// Timestamp is when this check was performed.
	Timestamp time.Time `json:"timestamp"`

	// Duration is how long the check took.
	Duration time.Duration `json:"duration"`

	// TotalChecked is the number of entries checked.
	TotalChecked int `json:"total_checked"`
}

// IssueCount returns the total number of issues found.
func (r *IntegrityReport) IssueCount() int {
	return len(r.OrphanedChildren) + len(r.MissingChildren) +
		len(r.LevelMismatches) + len(r.StaleEntries)
}

// MissingChild represents a parent referencing a non-existent child.
type MissingChild struct {
	ParentID string `json:"parent_id"`
	ChildID  string `json:"child_id"`
}

// LevelMismatch represents an entity with an incorrect hierarchy level.
type LevelMismatch struct {
	EntityID      string `json:"entity_id"`
	ExpectedLevel int    `json:"expected_level"`
	ActualLevel   int    `json:"actual_level"`
}

// StaleEntry represents a summary with an outdated hash.
type StaleEntry struct {
	ID          string `json:"id"`
	StoredHash  string `json:"stored_hash"`
	CurrentHash string `json:"current_hash"`
}

// Validate performs a comprehensive integrity check.
//
// Inputs:
//   - ctx: Context for cancellation.
//
// Outputs:
//   - *IntegrityReport: The validation report.
//   - error: Non-nil if the check couldn't be completed.
func (c *IntegrityChecker) Validate(ctx context.Context) (*IntegrityReport, error) {
	start := time.Now()
	report := &IntegrityReport{
		Valid:     true,
		Timestamp: start,
	}

	// Collect all summaries from cache
	allSummaries := c.collectAllSummaries()
	report.TotalChecked = len(allSummaries)

	// Build ID set for quick lookups
	idSet := make(map[string]bool)
	for _, s := range allSummaries {
		idSet[s.ID] = true
	}

	// Check each summary
	for _, summary := range allSummaries {
		// Check for context cancellation
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Check parent exists (except for project root)
		if summary.Level > 0 && summary.ParentID != "" {
			if !idSet[summary.ParentID] {
				report.OrphanedChildren = append(report.OrphanedChildren, summary.ID)
				report.Valid = false
			}
		}

		// Check children exist
		for _, childID := range summary.Children {
			if !idSet[childID] {
				report.MissingChildren = append(report.MissingChildren, MissingChild{
					ParentID: summary.ID,
					ChildID:  childID,
				})
				report.Valid = false
			}
		}

		// Check level is correct
		expectedLevel := c.hierarchy.EntityLevel(summary.ID)
		if summary.Level != expectedLevel {
			report.LevelMismatches = append(report.LevelMismatches, LevelMismatch{
				EntityID:      summary.ID,
				ExpectedLevel: expectedLevel,
				ActualLevel:   summary.Level,
			})
			report.Valid = false
		}
	}

	report.Duration = time.Since(start)
	return report, nil
}

// ValidateWithHashes performs validation including hash checks.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - hashProvider: Function to get current hash for an entity.
//
// Outputs:
//   - *IntegrityReport: The validation report.
//   - error: Non-nil if the check couldn't be completed.
func (c *IntegrityChecker) ValidateWithHashes(
	ctx context.Context,
	hashProvider func(entityID string) (string, error),
) (*IntegrityReport, error) {
	// First do basic validation
	report, err := c.Validate(ctx)
	if err != nil {
		return nil, err
	}

	// Then check hashes
	allSummaries := c.collectAllSummaries()

	for _, summary := range allSummaries {
		if ctx.Err() != nil {
			return report, ctx.Err()
		}

		currentHash, err := hashProvider(summary.ID)
		if err != nil {
			continue // Skip if we can't get hash
		}

		if summary.Hash != currentHash {
			report.StaleEntries = append(report.StaleEntries, StaleEntry{
				ID:          summary.ID,
				StoredHash:  summary.Hash,
				CurrentHash: currentHash,
			})
			report.Valid = false
		}
	}

	report.Duration = time.Since(report.Timestamp)
	return report, nil
}

// collectAllSummaries gathers all summaries from cache.
func (c *IntegrityChecker) collectAllSummaries() []*Summary {
	result := make([]*Summary, 0)

	for level := 0; level < 4; level++ {
		summaries := c.cache.GetByLevel(level)
		result = append(result, summaries...)
	}

	return result
}

// Repair attempts to fix integrity issues.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - report: The integrity report with issues to fix.
//
// Outputs:
//   - *RepairResult: Summary of repairs made.
//   - error: Non-nil if repair couldn't be completed.
func (c *IntegrityChecker) Repair(ctx context.Context, report *IntegrityReport) (*RepairResult, error) {
	result := &RepairResult{}

	// Remove orphaned entries (summaries with missing parents)
	for _, orphanID := range report.OrphanedChildren {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		c.cache.Delete(orphanID)
		result.OrphansRemoved++
	}

	// Invalidate stale entries
	for _, stale := range report.StaleEntries {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		c.cache.Invalidate(stale.ID)
		result.StaleInvalidated++
	}

	// Fix level mismatches by updating the summary
	for _, mismatch := range report.LevelMismatches {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		summary, ok := c.cache.Get(mismatch.EntityID)
		if !ok {
			summary, ok, _ = c.cache.GetStale(mismatch.EntityID)
		}
		if ok {
			summary.Level = mismatch.ExpectedLevel
			summary.UpdatedAt = time.Now().UnixMilli()
			c.cache.Set(summary)
			result.LevelsFixed++
		}
	}

	return result, nil
}

// RepairResult contains the summary of repairs made.
type RepairResult struct {
	// OrphansRemoved is the number of orphaned entries removed.
	OrphansRemoved int `json:"orphans_removed"`

	// StaleInvalidated is the number of stale entries invalidated.
	StaleInvalidated int `json:"stale_invalidated"`

	// LevelsFixed is the number of level mismatches corrected.
	LevelsFixed int `json:"levels_fixed"`

	// ChildrenRegenerated is the number of missing children regenerated.
	// Note: This requires a summarizer, which is not available here.
	ChildrenRegenerated int `json:"children_regenerated"`
}

// TotalRepairs returns the total number of repairs made.
func (r *RepairResult) TotalRepairs() int {
	return r.OrphansRemoved + r.StaleInvalidated + r.LevelsFixed + r.ChildrenRegenerated
}
