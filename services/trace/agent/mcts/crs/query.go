// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package crs

import (
	"sort"
	"time"
)

// -----------------------------------------------------------------------------
// Query API
// -----------------------------------------------------------------------------

// QueryAPI provides cross-index query capabilities.
//
// Description:
//
//	QueryAPI enables queries that span multiple indexes, such as finding
//	proven nodes that satisfy certain constraints or nodes with high
//	similarity that are dependencies of each other.
//
// Thread Safety: Safe for concurrent use (operates on immutable snapshot).
type QueryAPI interface {
	// FindProvenNodes returns all nodes with PROVEN status.
	//
	// Outputs:
	//   - []string: Node IDs that are proven. Empty if none.
	FindProvenNodes() []string

	// FindDisprovenNodes returns all nodes with DISPROVEN status.
	//
	// Outputs:
	//   - []string: Node IDs that are disproven. Empty if none.
	FindDisprovenNodes() []string

	// FindUnexploredNodes returns nodes that haven't been fully explored.
	//
	// Description:
	//   A node is unexplored if it has proof status UNKNOWN or EXPANDED
	//   but not PROVEN or DISPROVEN.
	//
	// Outputs:
	//   - []string: Node IDs that are unexplored. Empty if none.
	FindUnexploredNodes() []string

	// FindByProofRange returns nodes with proof numbers in the given range.
	//
	// Inputs:
	//   - minProof: Minimum proof number (inclusive).
	//   - maxProof: Maximum proof number (inclusive).
	//
	// Outputs:
	//   - []string: Node IDs in range, sorted by proof number ascending.
	FindByProofRange(minProof, maxProof uint64) []string

	// FindConstrainedNodes returns nodes that have constraints on them.
	//
	// Inputs:
	//   - constraintType: Type of constraint to filter by. Use ConstraintTypeUnknown for all.
	//
	// Outputs:
	//   - []string: Node IDs with constraints. Empty if none.
	FindConstrainedNodes(constraintType ConstraintType) []string

	// FindViolatedConstraints returns constraints where at least one node is DISPROVEN.
	//
	// Outputs:
	//   - []Constraint: Constraints with disproven nodes. Empty if none.
	FindViolatedConstraints() []Constraint

	// FindSimilarWithProofStatus returns nodes similar to nodeID with the given status.
	//
	// Inputs:
	//   - nodeID: The node to find similar nodes for.
	//   - status: The proof status to filter by.
	//   - k: Maximum number of similar nodes to return.
	//
	// Outputs:
	//   - []SimilarityMatch: Similar nodes with matching status, sorted by distance.
	FindSimilarWithProofStatus(nodeID string, status ProofStatus, k int) []SimilarityMatch

	// FindDependencyChain returns the dependency chain from start to end.
	//
	// Description:
	//   Uses BFS to find the shortest path from start to end through dependencies.
	//
	// Inputs:
	//   - startNodeID: The starting node.
	//   - endNodeID: The target node.
	//
	// Outputs:
	//   - []string: Path from start to end (inclusive). Empty if no path exists.
	FindDependencyChain(startNodeID, endNodeID string) []string

	// FindAffectedByNode returns all nodes that would be affected if nodeID changed.
	//
	// Description:
	//   Computes the transitive closure of nodes that depend on nodeID.
	//
	// Inputs:
	//   - nodeID: The node to check impact for.
	//
	// Outputs:
	//   - []string: All nodes affected (transitively). Empty if none.
	FindAffectedByNode(nodeID string) []string

	// FindHotNodes returns the most frequently accessed nodes.
	//
	// Inputs:
	//   - n: Number of hot nodes to return.
	//
	// Outputs:
	//   - []NodeFrequency: Top N nodes by frequency.
	FindHotNodes(n int) []NodeFrequency

	// FindRecentDecisions returns recent history entries with optional filtering.
	//
	// Inputs:
	//   - n: Maximum number of entries.
	//   - source: Filter by source. Use SignalSourceUnknown for all.
	//
	// Outputs:
	//   - []HistoryEntry: Recent entries matching filter.
	FindRecentDecisions(n int, source SignalSource) []HistoryEntry

	// NodeStats returns comprehensive statistics about a node across all indexes.
	//
	// Inputs:
	//   - nodeID: The node to get stats for.
	//
	// Outputs:
	//   - *NodeStats: Statistics about the node. Nil if node not found anywhere.
	NodeStats(nodeID string) *NodeStats
}

// -----------------------------------------------------------------------------
// Query Result Types
// -----------------------------------------------------------------------------

// NodeFrequency pairs a node ID with its access frequency.
type NodeFrequency struct {
	NodeID    string
	Frequency uint64
}

// NodeStats contains comprehensive statistics about a node.
type NodeStats struct {
	// NodeID is the node identifier.
	NodeID string

	// Proof information (if exists).
	HasProof    bool
	ProofNumber ProofNumber

	// Constraint information.
	ConstraintCount int
	Constraints     []string // Constraint IDs

	// Similarity information.
	SimilarNodeCount int
	NearestNeighbor  string
	NearestDistance  float64

	// Dependency information.
	DependsOnCount  int
	DependedByCount int
	HasCycle        bool

	// History information.
	HistoryEntryCount int
	LastAction        string
	LastActionTime    string

	// Streaming information.
	AccessFrequency uint64
}

// -----------------------------------------------------------------------------
// Query Implementation
// -----------------------------------------------------------------------------

// queryImpl implements QueryAPI for a snapshot.
type queryImpl struct {
	snapshot Snapshot
}

// newQuery creates a new QueryAPI for a snapshot.
func newQuery(snapshot Snapshot) QueryAPI {
	return &queryImpl{snapshot: snapshot}
}

// FindProvenNodes returns all nodes with PROVEN status.
func (q *queryImpl) FindProvenNodes() []string {
	proofIndex := q.snapshot.ProofIndex()
	all := proofIndex.All()

	var result []string
	for nodeID, proof := range all {
		if proof.Status == ProofStatusProven {
			result = append(result, nodeID)
		}
	}
	sort.Strings(result)
	return result
}

// FindDisprovenNodes returns all nodes with DISPROVEN status.
func (q *queryImpl) FindDisprovenNodes() []string {
	proofIndex := q.snapshot.ProofIndex()
	all := proofIndex.All()

	var result []string
	for nodeID, proof := range all {
		if proof.Status == ProofStatusDisproven {
			result = append(result, nodeID)
		}
	}
	sort.Strings(result)
	return result
}

// FindUnexploredNodes returns nodes that haven't been fully explored.
func (q *queryImpl) FindUnexploredNodes() []string {
	proofIndex := q.snapshot.ProofIndex()
	all := proofIndex.All()

	var result []string
	for nodeID, proof := range all {
		if proof.Status == ProofStatusUnknown || proof.Status == ProofStatusExpanded {
			result = append(result, nodeID)
		}
	}
	sort.Strings(result)
	return result
}

// FindByProofRange returns nodes with proof numbers in the given range.
func (q *queryImpl) FindByProofRange(minProof, maxProof uint64) []string {
	proofIndex := q.snapshot.ProofIndex()
	all := proofIndex.All()

	type nodeProof struct {
		id    string
		proof uint64
	}
	var matches []nodeProof

	for nodeID, proof := range all {
		if proof.Proof >= minProof && proof.Proof <= maxProof {
			matches = append(matches, nodeProof{id: nodeID, proof: proof.Proof})
		}
	}

	// Sort by proof number ascending
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].proof < matches[j].proof
	})

	result := make([]string, len(matches))
	for i, m := range matches {
		result[i] = m.id
	}
	return result
}

// FindConstrainedNodes returns nodes that have constraints on them.
func (q *queryImpl) FindConstrainedNodes(constraintType ConstraintType) []string {
	constraintIndex := q.snapshot.ConstraintIndex()

	var constraints []Constraint
	if constraintType == ConstraintTypeUnknown {
		all := constraintIndex.All()
		for _, c := range all {
			constraints = append(constraints, c)
		}
	} else {
		constraints = constraintIndex.FindByType(constraintType)
	}

	nodeSet := make(map[string]struct{})
	for _, c := range constraints {
		for _, nodeID := range c.Nodes {
			nodeSet[nodeID] = struct{}{}
		}
	}

	result := make([]string, 0, len(nodeSet))
	for nodeID := range nodeSet {
		result = append(result, nodeID)
	}
	sort.Strings(result)
	return result
}

// FindViolatedConstraints returns constraints where at least one node is DISPROVEN.
func (q *queryImpl) FindViolatedConstraints() []Constraint {
	constraintIndex := q.snapshot.ConstraintIndex()
	proofIndex := q.snapshot.ProofIndex()

	all := constraintIndex.All()
	var result []Constraint

	for _, c := range all {
		for _, nodeID := range c.Nodes {
			if proof, exists := proofIndex.Get(nodeID); exists {
				if proof.Status == ProofStatusDisproven {
					result = append(result, c)
					break
				}
			}
		}
	}

	return result
}

// FindSimilarWithProofStatus returns nodes similar to nodeID with the given status.
func (q *queryImpl) FindSimilarWithProofStatus(nodeID string, status ProofStatus, k int) []SimilarityMatch {
	if k <= 0 {
		return nil
	}

	similarityIndex := q.snapshot.SimilarityIndex()
	proofIndex := q.snapshot.ProofIndex()

	// Get more neighbors than requested, then filter
	neighbors := similarityIndex.NearestNeighbors(nodeID, k*3)

	var result []SimilarityMatch
	for _, match := range neighbors {
		if proof, exists := proofIndex.Get(match.NodeID); exists {
			if proof.Status == status {
				result = append(result, match)
				if len(result) >= k {
					break
				}
			}
		}
	}

	return result
}

// FindDependencyChain returns the dependency chain from start to end.
func (q *queryImpl) FindDependencyChain(startNodeID, endNodeID string) []string {
	if startNodeID == endNodeID {
		return []string{startNodeID}
	}

	depIndex := q.snapshot.DependencyIndex()

	// BFS to find shortest path
	visited := make(map[string]bool)
	parent := make(map[string]string)
	queue := []string{startNodeID}
	visited[startNodeID] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// Check forward dependencies
		deps := depIndex.DependsOn(current)
		for _, dep := range deps {
			if !visited[dep] {
				visited[dep] = true
				parent[dep] = current
				if dep == endNodeID {
					// Reconstruct path
					path := []string{dep}
					for p := parent[dep]; p != ""; p = parent[p] {
						path = append([]string{p}, path...)
					}
					return path
				}
				queue = append(queue, dep)
			}
		}
	}

	return nil // No path found
}

// FindAffectedByNode returns all nodes that would be affected if nodeID changed.
func (q *queryImpl) FindAffectedByNode(nodeID string) []string {
	depIndex := q.snapshot.DependencyIndex()

	// Compute transitive closure of reverse dependencies
	visited := make(map[string]bool)
	var result []string

	var dfs func(string)
	dfs = func(current string) {
		dependents := depIndex.DependedBy(current)
		for _, dep := range dependents {
			if !visited[dep] {
				visited[dep] = true
				result = append(result, dep)
				dfs(dep)
			}
		}
	}

	dfs(nodeID)
	sort.Strings(result)
	return result
}

// FindHotNodes returns the most frequently accessed nodes.
func (q *queryImpl) FindHotNodes(n int) []NodeFrequency {
	if n <= 0 {
		return nil
	}

	// Get all nodes from proof index and their frequencies from streaming
	proofIndex := q.snapshot.ProofIndex()
	streamingIndex := q.snapshot.StreamingIndex()

	all := proofIndex.All()
	frequencies := make([]NodeFrequency, 0, len(all))

	for nodeID := range all {
		freq := streamingIndex.Estimate(nodeID)
		if freq > 0 {
			frequencies = append(frequencies, NodeFrequency{
				NodeID:    nodeID,
				Frequency: freq,
			})
		}
	}

	// Sort by frequency descending
	sort.Slice(frequencies, func(i, j int) bool {
		return frequencies[i].Frequency > frequencies[j].Frequency
	})

	if len(frequencies) > n {
		frequencies = frequencies[:n]
	}

	return frequencies
}

// FindRecentDecisions returns recent history entries with optional filtering.
func (q *queryImpl) FindRecentDecisions(n int, source SignalSource) []HistoryEntry {
	if n <= 0 {
		return nil
	}

	historyIndex := q.snapshot.HistoryIndex()

	// Get more entries than needed if filtering
	fetchN := n
	if source != SignalSourceUnknown {
		fetchN = n * 3 // Fetch more to account for filtering
	}

	recent := historyIndex.Recent(fetchN)

	if source == SignalSourceUnknown {
		if len(recent) > n {
			return recent[:n]
		}
		return recent
	}

	// Filter by source
	var result []HistoryEntry
	for _, entry := range recent {
		if entry.Source == source {
			result = append(result, entry)
			if len(result) >= n {
				break
			}
		}
	}

	return result
}

// NodeStats returns comprehensive statistics about a node across all indexes.
func (q *queryImpl) NodeStats(nodeID string) *NodeStats {
	stats := &NodeStats{
		NodeID: nodeID,
	}

	// Proof information
	proofIndex := q.snapshot.ProofIndex()
	if proof, exists := proofIndex.Get(nodeID); exists {
		stats.HasProof = true
		stats.ProofNumber = proof
	}

	// Constraint information
	constraintIndex := q.snapshot.ConstraintIndex()
	constraints := constraintIndex.FindByNode(nodeID)
	stats.ConstraintCount = len(constraints)
	for _, c := range constraints {
		stats.Constraints = append(stats.Constraints, c.ID)
	}

	// Similarity information
	similarityIndex := q.snapshot.SimilarityIndex()
	neighbors := similarityIndex.NearestNeighbors(nodeID, 10)
	stats.SimilarNodeCount = len(neighbors)
	if len(neighbors) > 0 {
		stats.NearestNeighbor = neighbors[0].NodeID
		stats.NearestDistance = neighbors[0].Distance
	}

	// Dependency information
	depIndex := q.snapshot.DependencyIndex()
	stats.DependsOnCount = len(depIndex.DependsOn(nodeID))
	stats.DependedByCount = len(depIndex.DependedBy(nodeID))
	stats.HasCycle = depIndex.HasCycle(nodeID)

	// History information
	historyIndex := q.snapshot.HistoryIndex()
	trace := historyIndex.Trace(nodeID)
	stats.HistoryEntryCount = len(trace)
	if len(trace) > 0 {
		lastEntry := trace[len(trace)-1]
		stats.LastAction = lastEntry.Action
		stats.LastActionTime = time.UnixMilli(lastEntry.Timestamp).UTC().Format("2006-01-02T15:04:05Z")
	}

	// Streaming information
	streamingIndex := q.snapshot.StreamingIndex()
	stats.AccessFrequency = streamingIndex.Estimate(nodeID)

	// Only return stats if node exists in at least one index
	if !stats.HasProof && stats.ConstraintCount == 0 && stats.SimilarNodeCount == 0 &&
		stats.DependsOnCount == 0 && stats.DependedByCount == 0 &&
		stats.HistoryEntryCount == 0 && stats.AccessFrequency == 0 {
		return nil
	}

	return stats
}

// -----------------------------------------------------------------------------
// Snapshot Query Method
// -----------------------------------------------------------------------------

// Query returns the query API for cross-index queries.
func (s *snapshot) Query() QueryAPI {
	return newQuery(s)
}
