// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package seeder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/weaviate/weaviate-go-client/v5/weaviate"
)

// SeederConfig configures the seeder.
type SeederConfig struct {
	// MaxConcurrent is the max concurrent doc extractions.
	MaxConcurrent int

	// Timeout is the overall seeding timeout.
	Timeout time.Duration

	// SkipIndirect skips indirect dependencies.
	SkipIndirect bool
}

// DefaultSeederConfig returns sensible defaults.
func DefaultSeederConfig() SeederConfig {
	return SeederConfig{
		MaxConcurrent: 4,
		Timeout:       5 * time.Minute,
		SkipIndirect:  true,
	}
}

// Seeder handles library documentation seeding.
type Seeder struct {
	client *weaviate.Client
	config SeederConfig
}

// NewSeeder creates a new library seeder.
//
// Description:
//
//	Creates a Seeder configured for library documentation extraction and indexing.
//
// Inputs:
//
//	client - Weaviate client. Must not be nil.
//	config - Seeder configuration.
//
// Outputs:
//
//	*Seeder - The configured seeder
//	error - Non-nil if client is nil
//
// Thread Safety: Seed() is safe for concurrent use. Multiple projects
// can be seeded concurrently as they use isolated data spaces.
func NewSeeder(client *weaviate.Client, config SeederConfig) (*Seeder, error) {
	if client == nil {
		return nil, ErrNilClient
	}
	return &Seeder{
		client: client,
		config: config,
	}, nil
}

// Seed extracts and indexes library documentation for a project.
//
// Description:
//
//	Discovers dependencies from the project, extracts documentation from
//	each, and indexes into Weaviate. Uses a deterministic data space
//	based on the project root hash for multi-tenant isolation.
//
// Inputs:
//
//	ctx - Context for cancellation
//	projectRoot - Absolute path to the project root
//	dataSpace - Optional override for data space (uses hash if empty)
//
// Outputs:
//
//	*SeedResult - Seeding statistics
//	error - Non-nil if seeding fails completely
func (s *Seeder) Seed(ctx context.Context, projectRoot, dataSpace string) (*SeedResult, error) {
	// Validate project root
	if err := ValidateProjectRoot(projectRoot); err != nil {
		return nil, fmt.Errorf("invalid project root: %w", err)
	}

	start := time.Now()
	slog.Info("Starting library seeding", "projectRoot", projectRoot)

	// Apply timeout
	if s.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.config.Timeout)
		defer cancel()
	}

	// Ensure schema exists
	if err := EnsureSchema(ctx, s.client); err != nil {
		return nil, fmt.Errorf("ensuring schema: %w", err)
	}

	// Use deterministic data space if not provided
	if dataSpace == "" {
		dataSpace = generateDataSpace(projectRoot)
	}

	result := &SeedResult{
		Errors: make([]string, 0),
	}

	// Resolve dependencies
	deps, err := ResolveDependencies(ctx, projectRoot)
	if err != nil {
		if err == ErrNoGoMod {
			slog.Info("No go.mod found, skipping seeding")
			return result, nil
		}
		return nil, fmt.Errorf("resolving dependencies: %w", err)
	}

	// Filter if configured
	if s.config.SkipIndirect {
		deps = FilterDirectDependencies(deps)
	}

	result.DependenciesFound = len(deps)
	slog.Info("Found dependencies", "count", len(deps))

	// Extract and index docs for each dependency
	for _, dep := range deps {
		if err := ctx.Err(); err != nil {
			break
		}

		if dep.LocalPath == "" {
			result.Errors = append(result.Errors,
				fmt.Sprintf("%s: not in local cache", dep.ModulePath))
			continue
		}

		slog.Info("Extracting docs", "module", dep.ModulePath, "version", dep.Version)

		docs, err := ExtractDocs(ctx, dep, dataSpace)
		if err != nil {
			result.Errors = append(result.Errors,
				fmt.Sprintf("%s: %v", dep.ModulePath, err))
			continue
		}

		if len(docs) == 0 {
			slog.Info("No docs extracted", "module", dep.ModulePath)
			continue
		}

		indexed, err := IndexDocs(ctx, s.client, docs)
		if err != nil {
			result.Errors = append(result.Errors,
				fmt.Sprintf("%s: indexing failed: %v", dep.ModulePath, err))
			continue
		}

		result.DocsIndexed += indexed
		slog.Info("Indexed docs", "module", dep.ModulePath, "count", indexed)
	}

	slog.Info("Seeding complete",
		"dependencies", result.DependenciesFound,
		"docs_indexed", result.DocsIndexed,
		"errors", len(result.Errors),
		"duration", time.Since(start))

	return result, nil
}

// generateDataSpace creates a deterministic data space from project root.
func generateDataSpace(projectRoot string) string {
	hash := sha256.Sum256([]byte(projectRoot))
	return "project-" + hex.EncodeToString(hash[:])[:12]
}
