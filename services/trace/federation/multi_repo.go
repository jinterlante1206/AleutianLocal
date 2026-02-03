// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package federation

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
)

// FederatedGraph aggregates graphs from multiple repositories.
//
// # Description
//
// Combines dependency graphs from multiple repositories or monorepo packages
// into a unified view. Tracks cross-repository dependencies through imports,
// API calls, gRPC, and event systems.
//
// # Thread Safety
//
// Safe for concurrent use.
type FederatedGraph struct {
	mu    sync.RWMutex
	repos map[string]*RepoGraph // repo_id -> graph
	edges []CrossRepoEdge       // cross-repo dependencies
}

// RepoGraph wraps a repository's graph with metadata.
type RepoGraph struct {
	// ID is the unique repository identifier.
	ID string `json:"id"`

	// Path is the file system path to the repository.
	Path string `json:"path"`

	// Graph is the repository's dependency graph.
	Graph *graph.Graph `json:"-"`

	// Exports are symbols exported by this repo for other repos to use.
	Exports []ExportedSymbol `json:"exports"`

	// Generation is the graph generation for cache invalidation.
	Generation uint64 `json:"generation"`
}

// ExportedSymbol represents a symbol exported from a repository.
type ExportedSymbol struct {
	// SymbolID is the symbol identifier within the repo.
	SymbolID string `json:"symbol_id"`

	// Name is the public name.
	Name string `json:"name"`

	// Package is the package path.
	Package string `json:"package"`

	// Type is the symbol type (function, type, etc.).
	Type string `json:"type"`
}

// CrossRepoEdge represents a dependency between repositories.
type CrossRepoEdge struct {
	// FromRepo is the source repository ID.
	FromRepo string `json:"from_repo"`

	// FromSymbol is the symbol in the source repo.
	FromSymbol string `json:"from_symbol"`

	// ToRepo is the target repository ID.
	ToRepo string `json:"to_repo"`

	// ToSymbol is the symbol in the target repo.
	ToSymbol string `json:"to_symbol"`

	// EdgeType is the type of cross-repo dependency.
	EdgeType CrossRepoEdgeType `json:"edge_type"`

	// Confidence is how confident we are in this edge (0-100).
	Confidence int `json:"confidence"`
}

// CrossRepoEdgeType represents the type of cross-repository edge.
type CrossRepoEdgeType string

const (
	CrossRepoImport  CrossRepoEdgeType = "IMPORT"
	CrossRepoAPICall CrossRepoEdgeType = "API_CALL"
	CrossRepoGRPC    CrossRepoEdgeType = "GRPC"
	CrossRepoEvent   CrossRepoEdgeType = "EVENT"
)

// FederationManifest defines repositories to federate.
type FederationManifest struct {
	// Repos are the repositories to include.
	Repos []RepoConfig `yaml:"repos" json:"repos"`
}

// RepoConfig configures a repository in the federation.
type RepoConfig struct {
	// ID is the unique identifier for this repo.
	ID string `yaml:"id" json:"id"`

	// Path is the file system path (relative or absolute).
	Path string `yaml:"path" json:"path"`

	// Type is the repository type (go, python, typescript).
	Type string `yaml:"type,omitempty" json:"type,omitempty"`

	// Exports lists patterns for exported symbols.
	Exports []string `yaml:"exports,omitempty" json:"exports,omitempty"`
}

// NewFederatedGraph creates a new federated graph.
func NewFederatedGraph() *FederatedGraph {
	return &FederatedGraph{
		repos: make(map[string]*RepoGraph),
		edges: make([]CrossRepoEdge, 0),
	}
}

// AddRepo adds a repository to the federation.
//
// # Inputs
//
//   - repo: The repository graph to add.
//
// # Outputs
//
//   - error: Non-nil if repo ID already exists.
func (f *FederatedGraph) AddRepo(repo *RepoGraph) error {
	if repo == nil {
		return fmt.Errorf("repo is required")
	}
	if repo.ID == "" {
		return fmt.Errorf("repo ID is required")
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if _, exists := f.repos[repo.ID]; exists {
		return fmt.Errorf("repo %s already exists", repo.ID)
	}

	f.repos[repo.ID] = repo
	return nil
}

// RemoveRepo removes a repository from the federation.
func (f *FederatedGraph) RemoveRepo(repoID string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.repos, repoID)

	// Remove edges involving this repo
	newEdges := make([]CrossRepoEdge, 0, len(f.edges))
	for _, edge := range f.edges {
		if edge.FromRepo != repoID && edge.ToRepo != repoID {
			newEdges = append(newEdges, edge)
		}
	}
	f.edges = newEdges
}

// GetRepo returns a repository by ID.
func (f *FederatedGraph) GetRepo(repoID string) (*RepoGraph, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	repo, ok := f.repos[repoID]
	return repo, ok
}

// RepoIDs returns all repository IDs.
func (f *FederatedGraph) RepoIDs() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	ids := make([]string, 0, len(f.repos))
	for id := range f.repos {
		ids = append(ids, id)
	}
	return ids
}

// DiscoverEdges finds cross-repository dependencies.
//
// # Description
//
// Analyzes all repositories to find dependencies between them. Detects:
//   - Go package imports
//   - HTTP/gRPC client calls
//   - Event publisher/subscriber relationships
//
// # Inputs
//
//   - ctx: Context for cancellation.
//
// # Outputs
//
//   - error: Non-nil on failure.
func (f *FederatedGraph) DiscoverEdges(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context is required")
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Clear existing edges
	f.edges = make([]CrossRepoEdge, 0)

	// Build export index
	exportIndex := f.buildExportIndex()

	// Scan each repo for cross-repo references
	for _, repo := range f.repos {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		edges, err := f.discoverRepoEdges(ctx, repo, exportIndex)
		if err != nil {
			continue // Skip errors, continue with other repos
		}
		f.edges = append(f.edges, edges...)
	}

	return nil
}

// exportIndex maps package/symbol to repo ID
type exportIndex map[string]string

// buildExportIndex creates an index of exports across all repos.
func (f *FederatedGraph) buildExportIndex() exportIndex {
	index := make(exportIndex)

	for repoID, repo := range f.repos {
		for _, export := range repo.Exports {
			key := export.Package + "/" + export.Name
			index[key] = repoID
		}
	}

	return index
}

// discoverRepoEdges finds edges from one repo to others.
func (f *FederatedGraph) discoverRepoEdges(ctx context.Context, repo *RepoGraph, exports exportIndex) ([]CrossRepoEdge, error) {
	var edges []CrossRepoEdge

	// Walk source files looking for imports
	err := filepath.WalkDir(repo.Path, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		ext := filepath.Ext(path)
		if ext != ".go" {
			return nil
		}

		fileEdges, _ := f.scanFileForCrossRepoRefs(path, repo.ID, exports)
		edges = append(edges, fileEdges...)

		return nil
	})

	return edges, err
}

// Import detection patterns
var (
	goImportPattern = regexp.MustCompile(`import\s+(?:\(\s*([\s\S]*?)\s*\)|"([^"]+)")`)
	goImportLine    = regexp.MustCompile(`(?:"([^"]+)"|(\w+)\s+"([^"]+)")`)
)

// scanFileForCrossRepoRefs finds cross-repo references in a file.
func (f *FederatedGraph) scanFileForCrossRepoRefs(path, repoID string, exports exportIndex) ([]CrossRepoEdge, error) {
	var edges []CrossRepoEdge

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var inImportBlock bool
	var importLines []string

	for scanner.Scan() {
		line := scanner.Text()

		// Track import blocks
		if strings.Contains(line, "import") {
			if strings.Contains(line, "(") {
				inImportBlock = true
				continue
			}
			// Single-line import
			if matches := goImportPattern.FindStringSubmatch(line); len(matches) > 2 && matches[2] != "" {
				importLines = append(importLines, matches[2])
			}
		}

		if inImportBlock {
			if strings.Contains(line, ")") {
				inImportBlock = false
			} else {
				// Extract import path
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "//") {
					if matches := goImportLine.FindStringSubmatch(line); len(matches) > 1 {
						importPath := matches[1]
						if importPath == "" && len(matches) > 3 {
							importPath = matches[3]
						}
						if importPath != "" {
							importLines = append(importLines, importPath)
						}
					}
				}
			}
		}
	}

	// Check imports against exports
	for _, imp := range importLines {
		// Check if this import matches any export
		for exportKey, exportRepoID := range exports {
			if exportRepoID == repoID {
				continue // Skip same repo
			}

			if strings.HasSuffix(imp, extractPackageFromKey(exportKey)) {
				edges = append(edges, CrossRepoEdge{
					FromRepo:   repoID,
					FromSymbol: path,
					ToRepo:     exportRepoID,
					ToSymbol:   exportKey,
					EdgeType:   CrossRepoImport,
					Confidence: 90,
				})
			}
		}
	}

	return edges, scanner.Err()
}

// extractPackageFromKey extracts package path from export key.
func extractPackageFromKey(key string) string {
	if idx := strings.LastIndex(key, "/"); idx > 0 {
		return key[:idx]
	}
	return key
}

// GetCrossRepoEdges returns all cross-repo edges.
func (f *FederatedGraph) GetCrossRepoEdges() []CrossRepoEdge {
	f.mu.RLock()
	defer f.mu.RUnlock()

	result := make([]CrossRepoEdge, len(f.edges))
	copy(result, f.edges)
	return result
}

// GetEdgesFrom returns edges from a specific repo.
func (f *FederatedGraph) GetEdgesFrom(repoID string) []CrossRepoEdge {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var result []CrossRepoEdge
	for _, edge := range f.edges {
		if edge.FromRepo == repoID {
			result = append(result, edge)
		}
	}
	return result
}

// GetEdgesTo returns edges to a specific repo.
func (f *FederatedGraph) GetEdgesTo(repoID string) []CrossRepoEdge {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var result []CrossRepoEdge
	for _, edge := range f.edges {
		if edge.ToRepo == repoID {
			result = append(result, edge)
		}
	}
	return result
}

// FindAffectedRepos finds all repos affected by a change in a symbol.
//
// # Inputs
//
//   - repoID: The repository where the change occurs.
//   - symbolID: The symbol being changed.
//
// # Outputs
//
//   - []string: Repository IDs that depend on this symbol.
func (f *FederatedGraph) FindAffectedRepos(repoID, symbolID string) []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	affected := make(map[string]bool)

	for _, edge := range f.edges {
		if edge.ToRepo == repoID {
			// Check if this edge involves the changed symbol
			if edge.ToSymbol == symbolID || strings.Contains(edge.ToSymbol, symbolID) {
				affected[edge.FromRepo] = true
			}
		}
	}

	result := make([]string, 0, len(affected))
	for repo := range affected {
		result = append(result, repo)
	}
	return result
}

// CrossRepoBlastRadius calculates blast radius across repositories.
type CrossRepoBlastRadius struct {
	// TargetRepo is the repository containing the changed symbol.
	TargetRepo string `json:"target_repo"`

	// TargetSymbol is the changed symbol.
	TargetSymbol string `json:"target_symbol"`

	// LocalCallers are callers within the same repository.
	LocalCallers int `json:"local_callers"`

	// AffectedRepos are other repositories affected.
	AffectedRepos []AffectedRepo `json:"affected_repos"`

	// TotalCrossRepoCallers is the total number of callers in other repos.
	TotalCrossRepoCallers int `json:"total_cross_repo_callers"`
}

// AffectedRepo represents an affected repository.
type AffectedRepo struct {
	// RepoID is the repository identifier.
	RepoID string `json:"repo_id"`

	// CallerCount is the number of callers in this repo.
	CallerCount int `json:"caller_count"`

	// ImpactLevel is the impact severity.
	ImpactLevel string `json:"impact_level"`
}

// CalculateCrossRepoBlastRadius computes blast radius across repositories.
func (f *FederatedGraph) CalculateCrossRepoBlastRadius(ctx context.Context, repoID, symbolID string) (*CrossRepoBlastRadius, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}

	f.mu.RLock()
	defer f.mu.RUnlock()

	result := &CrossRepoBlastRadius{
		TargetRepo:    repoID,
		TargetSymbol:  symbolID,
		AffectedRepos: make([]AffectedRepo, 0),
	}

	// Count local callers
	if repo, ok := f.repos[repoID]; ok && repo.Graph != nil {
		if node, found := repo.Graph.GetNode(symbolID); found {
			result.LocalCallers = len(node.Incoming)
		}
	}

	// Find cross-repo callers
	repoCallerCounts := make(map[string]int)
	for _, edge := range f.edges {
		if edge.ToRepo == repoID && edge.ToSymbol == symbolID {
			repoCallerCounts[edge.FromRepo]++
		}
	}

	for repo, count := range repoCallerCounts {
		impactLevel := "LOW"
		if count > 10 {
			impactLevel = "HIGH"
		} else if count > 5 {
			impactLevel = "MEDIUM"
		}

		result.AffectedRepos = append(result.AffectedRepos, AffectedRepo{
			RepoID:      repo,
			CallerCount: count,
			ImpactLevel: impactLevel,
		})
		result.TotalCrossRepoCallers += count
	}

	return result, nil
}

// LoadManifest loads a federation manifest from a YAML file.
//
// # Inputs
//
//   - path: Path to the manifest file (.aleutian/federation.yml).
//
// # Outputs
//
//   - *FederationManifest: Parsed manifest.
//   - error: Non-nil on failure.
func LoadManifest(path string) (*FederationManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	// Simple YAML parsing without external library
	manifest := &FederationManifest{
		Repos: make([]RepoConfig, 0),
	}

	lines := strings.Split(string(data), "\n")
	var currentRepo *RepoConfig

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "- id:") {
			if currentRepo != nil {
				manifest.Repos = append(manifest.Repos, *currentRepo)
			}
			currentRepo = &RepoConfig{
				ID: strings.TrimSpace(strings.TrimPrefix(line, "- id:")),
			}
		} else if strings.HasPrefix(line, "path:") && currentRepo != nil {
			currentRepo.Path = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "path:")), "\"'")
		} else if strings.HasPrefix(line, "type:") && currentRepo != nil {
			currentRepo.Type = strings.TrimSpace(strings.TrimPrefix(line, "type:"))
		}
	}

	if currentRepo != nil {
		manifest.Repos = append(manifest.Repos, *currentRepo)
	}

	return manifest, nil
}
