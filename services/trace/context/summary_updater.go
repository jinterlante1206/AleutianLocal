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

// SummaryUpdater handles incremental summary updates when code changes.
//
// Thread Safety: Safe for concurrent use.
type SummaryUpdater struct {
	summarizer       *Summarizer
	cache            *SummaryCache
	integrityChecker *IntegrityChecker
	hierarchy        LanguageHierarchy
}

// NewSummaryUpdater creates a new updater.
//
// Inputs:
//   - summarizer: The summarizer for generating new summaries.
//   - cache: The summary cache.
//   - hierarchy: The language hierarchy.
//
// Outputs:
//   - *SummaryUpdater: A new updater instance.
func NewSummaryUpdater(
	summarizer *Summarizer,
	cache *SummaryCache,
	hierarchy LanguageHierarchy,
) *SummaryUpdater {
	return &SummaryUpdater{
		summarizer:       summarizer,
		cache:            cache,
		integrityChecker: NewIntegrityChecker(cache, hierarchy),
		hierarchy:        hierarchy,
	}
}

// ChangeSet describes a set of code changes.
type ChangeSet struct {
	// AddedFiles are new files added to the project.
	AddedFiles []FileInfo `json:"added_files"`

	// ModifiedFiles are files that were changed.
	ModifiedFiles []FileInfo `json:"modified_files"`

	// DeletedFiles are file paths that were removed.
	DeletedFiles []string `json:"deleted_files"`

	// Timestamp is when the changes were detected.
	Timestamp time.Time `json:"timestamp"`
}

// IsEmpty returns true if there are no changes.
func (c *ChangeSet) IsEmpty() bool {
	return len(c.AddedFiles) == 0 && len(c.ModifiedFiles) == 0 && len(c.DeletedFiles) == 0
}

// TotalChanges returns the total number of changes.
func (c *ChangeSet) TotalChanges() int {
	return len(c.AddedFiles) + len(c.ModifiedFiles) + len(c.DeletedFiles)
}

// UpdateResult contains results from updating summaries.
type UpdateResult struct {
	// FilesUpdated is the number of file summaries updated.
	FilesUpdated int `json:"files_updated"`

	// FilesDeleted is the number of file summaries deleted.
	FilesDeleted int `json:"files_deleted"`

	// PackagesUpdated is the number of package summaries updated.
	PackagesUpdated int `json:"packages_updated"`

	// ProjectUpdated indicates if the project summary was updated.
	ProjectUpdated bool `json:"project_updated"`

	// Duration is how long the update took.
	Duration time.Duration `json:"duration"`

	// Errors contains any errors encountered.
	Errors []string `json:"errors,omitempty"`
}

// UpdateChangedSummaries updates summaries for changed files.
//
// This method:
// 1. Deletes summaries for removed files
// 2. Regenerates summaries for modified files
// 3. Generates summaries for new files
// 4. Propagates changes up to package and project summaries
//
// Inputs:
//   - ctx: Context for cancellation.
//   - changes: The set of changes to process.
//
// Outputs:
//   - *UpdateResult: Summary of updates made.
//   - error: Non-nil if updates completely failed.
func (u *SummaryUpdater) UpdateChangedSummaries(ctx context.Context, changes *ChangeSet) (*UpdateResult, error) {
	start := time.Now()
	result := &UpdateResult{}

	if changes.IsEmpty() {
		result.Duration = time.Since(start)
		return result, nil
	}

	// Track affected packages for propagation
	affectedPackages := make(map[string]bool)

	// 1. Delete summaries for removed files
	for _, filePath := range changes.DeletedFiles {
		u.cache.Delete(filePath)
		result.FilesDeleted++

		// Mark package as affected
		pkgPath, err := u.hierarchy.ParentOf(filePath)
		if err == nil && pkgPath != "" {
			affectedPackages[pkgPath] = true
		}
	}

	// 2. Process modified files
	for _, fileInfo := range changes.ModifiedFiles {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		// Invalidate old summary
		u.cache.InvalidateIfStale(fileInfo.Path, fileInfo.ContentHash)

		// Generate new summary
		_, err := u.summarizer.GenerateFileSummary(ctx, &fileInfo)
		if err != nil {
			result.Errors = append(result.Errors, "file "+fileInfo.Path+": "+err.Error())
		} else {
			result.FilesUpdated++
		}

		// Mark package as affected
		pkgPath, err := u.hierarchy.ParentOf(fileInfo.Path)
		if err == nil && pkgPath != "" {
			affectedPackages[pkgPath] = true
		}
	}

	// 3. Process added files
	for _, fileInfo := range changes.AddedFiles {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		// Generate new summary
		_, err := u.summarizer.GenerateFileSummary(ctx, &fileInfo)
		if err != nil {
			result.Errors = append(result.Errors, "file "+fileInfo.Path+": "+err.Error())
		} else {
			result.FilesUpdated++
		}

		// Mark package as affected
		pkgPath, err := u.hierarchy.ParentOf(fileInfo.Path)
		if err == nil && pkgPath != "" {
			affectedPackages[pkgPath] = true
		}
	}

	// 4. Propagate changes to affected packages
	for pkgPath := range affectedPackages {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		// Invalidate and regenerate package summary
		u.cache.Invalidate(pkgPath)

		// Note: We'd need PackageInfo to regenerate, which would come from the graph
		// For now, just invalidate so it's regenerated on next access
		result.PackagesUpdated++
	}

	// 5. If any packages changed, invalidate project summary
	if result.PackagesUpdated > 0 {
		u.cache.Invalidate("")
		result.ProjectUpdated = true
	}

	result.Duration = time.Since(start)
	return result, nil
}

// RefreshStale regenerates all stale summaries.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - hashProvider: Function to get current hash for an entity.
//
// Outputs:
//   - *UpdateResult: Summary of updates made.
//   - error: Non-nil if refresh fails.
func (u *SummaryUpdater) RefreshStale(
	ctx context.Context,
	hashProvider func(entityID string) (string, error),
) (*UpdateResult, error) {
	// First, run integrity check to find stale entries
	report, err := u.integrityChecker.ValidateWithHashes(ctx, hashProvider)
	if err != nil {
		return nil, err
	}

	result := &UpdateResult{}
	start := time.Now()

	// Invalidate all stale entries
	for _, stale := range report.StaleEntries {
		u.cache.Invalidate(stale.ID)
	}

	// Note: Actual regeneration would happen on next access
	// or could be triggered explicitly with file/package info

	result.FilesUpdated = len(report.StaleEntries)
	result.Duration = time.Since(start)

	return result, nil
}

// ValidateAndRepair validates cache integrity and repairs issues.
//
// Inputs:
//   - ctx: Context for cancellation.
//
// Outputs:
//   - *IntegrityReport: The validation report.
//   - *RepairResult: Results of repairs made.
//   - error: Non-nil if validation/repair fails.
func (u *SummaryUpdater) ValidateAndRepair(ctx context.Context) (*IntegrityReport, *RepairResult, error) {
	report, err := u.integrityChecker.Validate(ctx)
	if err != nil {
		return nil, nil, err
	}

	if report.Valid {
		return report, &RepairResult{}, nil
	}

	repairResult, err := u.integrityChecker.Repair(ctx, report)
	if err != nil {
		return report, repairResult, err
	}

	return report, repairResult, nil
}

// WarmCache pre-generates summaries for important entities.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - packages: Package information to pre-warm.
//   - progress: Optional progress callback.
//
// Outputs:
//   - *GenerationResult: Results of the warm operation.
func (u *SummaryUpdater) WarmCache(
	ctx context.Context,
	packages []*PackageInfo,
	progress ProgressCallback,
) (*GenerationResult, error) {
	return u.summarizer.GenerateAllPackageSummaries(ctx, packages, progress)
}
