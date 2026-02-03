// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package nodes

import (
	"context"
	"fmt"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/cache"
	"github.com/AleutianAI/AleutianFOSS/services/trace/dag"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/manifest"
)

// LoadCacheNode loads or builds a cached code graph.
//
// Description:
//
//	Attempts to load a cached graph for the project. If not cached or
//	stale, uses the provided build function to create a new graph and
//	caches it for future use.
//
// Inputs (from map[string]any):
//
//	"project_root" (string): Absolute path to project root. Required.
//	"force_rebuild" (bool): If true, ignores cache and rebuilds. Optional.
//
// Outputs:
//
//	*LoadCacheOutput containing:
//	  - Graph: The loaded or built graph
//	  - Manifest: The project manifest
//	  - FromCache: Whether the graph was loaded from cache
//	  - Duration: Load/build time
//
// Thread Safety:
//
//	Safe for concurrent use.
type LoadCacheNode struct {
	dag.BaseNode
	graphCache *cache.GraphCache
	buildFunc  cache.BuildFunc
}

// LoadCacheOutput contains the result of loading/building a cache.
type LoadCacheOutput struct {
	// Graph is the loaded or built code graph.
	Graph *graph.Graph

	// Manifest is the project manifest.
	Manifest *manifest.Manifest

	// GraphID is the unique identifier for this cached graph.
	GraphID string

	// FromCache indicates whether the graph was loaded from cache.
	FromCache bool

	// WasStale indicates the cache entry was stale.
	WasStale bool

	// Duration is the load/build time.
	Duration time.Duration
}

// NewLoadCacheNode creates a new load cache node.
//
// Inputs:
//
//	graphCache - The graph cache to use. Must not be nil.
//	buildFunc - Function to build graph if not cached. Must not be nil.
//	deps - Names of nodes this node depends on.
//
// Outputs:
//
//	*LoadCacheNode - The configured node.
func NewLoadCacheNode(
	graphCache *cache.GraphCache,
	buildFunc cache.BuildFunc,
	deps []string,
) *LoadCacheNode {
	return &LoadCacheNode{
		BaseNode: dag.BaseNode{
			NodeName:         "LOAD_CACHE",
			NodeDependencies: deps,
			NodeTimeout:      2 * time.Minute,
			NodeRetryable:    true,
		},
		graphCache: graphCache,
		buildFunc:  buildFunc,
	}
}

// Execute loads or builds the cached graph.
//
// Description:
//
//	First attempts to load from cache. If not found or force_rebuild is
//	set, uses the build function to create a new graph. The result is
//	cached for future use.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	inputs - Map containing "project_root" and optionally "force_rebuild".
//
// Outputs:
//
//	*LoadCacheOutput - The load result.
//	error - Non-nil if loading and building both fail.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (n *LoadCacheNode) Execute(ctx context.Context, inputs map[string]any) (any, error) {
	if n.graphCache == nil {
		return nil, fmt.Errorf("%w: graph cache", ErrNilDependency)
	}
	if n.buildFunc == nil {
		return nil, fmt.Errorf("%w: build function", ErrNilDependency)
	}

	// Extract inputs
	projectRoot, forceRebuild, err := n.extractInputs(inputs)
	if err != nil {
		return nil, err
	}

	start := time.Now()

	// Force rebuild if requested
	if forceRebuild {
		n.graphCache.ForceInvalidate(projectRoot)
	}

	// Try to get from cache or build
	entry, release, err := n.graphCache.GetOrBuild(ctx, projectRoot, n.buildFunc)
	if err != nil {
		return nil, fmt.Errorf("get or build graph: %w", err)
	}

	// Note: We don't call release() here because the graph needs to remain
	// available for downstream nodes. The caller (pipeline executor) is
	// responsible for cleanup.

	fromCache := entry.BuiltAtMilli < time.Now().Add(-time.Second).UnixMilli()

	_ = release // Suppress unused warning - release is intentionally not called here

	return &LoadCacheOutput{
		Graph:     entry.Graph,
		Manifest:  entry.Manifest,
		GraphID:   entry.GraphID,
		FromCache: fromCache,
		WasStale:  false,
		Duration:  time.Since(start),
	}, nil
}

// extractInputs validates and extracts inputs from the map.
func (n *LoadCacheNode) extractInputs(inputs map[string]any) (string, bool, error) {
	// Extract project root
	rootRaw, ok := inputs["project_root"]
	if !ok {
		rootRaw, ok = inputs["root"]
		if !ok {
			return "", false, fmt.Errorf("%w: project_root", ErrMissingInput)
		}
	}

	projectRoot, ok := rootRaw.(string)
	if !ok {
		return "", false, fmt.Errorf("%w: project_root must be string", ErrInvalidInputType)
	}

	// Extract optional force_rebuild
	forceRebuild := false
	if forceRaw, ok := inputs["force_rebuild"]; ok {
		if force, ok := forceRaw.(bool); ok {
			forceRebuild = force
		}
	}

	return projectRoot, forceRebuild, nil
}
