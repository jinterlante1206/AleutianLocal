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
	"maps"
	"sync"
	"time"
)

// -----------------------------------------------------------------------------
// Snapshot Implementation
// -----------------------------------------------------------------------------

// snapshot implements the Snapshot interface with copy-on-write semantics.
//
// Thread Safety: Safe for concurrent use (immutable after creation).
type snapshot struct {
	generation int64
	createdAt  int64 // Unix milliseconds UTC

	// Index data - all maps are copied on snapshot creation for immutability
	proofData      map[string]ProofNumber
	constraintData map[string]Constraint
	similarityData map[string]map[string]float64 // node1 -> node2 -> distance
	dependencyData *dependencyGraph
	historyData    []HistoryEntry
	streamingData  *streamingStats
	clauseData     map[string]*Clause // CRS-04: learned clauses

	// GR-28: Graph query interface for activities
	graphQuery GraphQuery

	// GR-32: Graph-backed dependency index (preferred over dependencyData)
	graphBackedDepIndex *GraphBackedDependencyIndex
}

// newSnapshot creates a new immutable snapshot from current state.
//
// Description:
//
//	Creates deep copies of all index data to ensure immutability.
//	The source data can be modified after this call without affecting
//	the snapshot.
//
// Inputs:
//   - generation: The generation number for this snapshot.
//   - proofs: Proof number data to copy.
//   - constraints: Constraint data to copy.
//   - similarities: Similarity data to copy.
//   - deps: Dependency graph to copy.
//   - history: History entries to copy.
//   - streaming: Streaming stats to copy.
//   - clauses: Learned clause data to copy. (CRS-04)
//
// Outputs:
//   - *snapshot: The new immutable snapshot.
//
// Thread Safety: Safe for concurrent use.
func newSnapshot(
	generation int64,
	proofs map[string]ProofNumber,
	constraints map[string]Constraint,
	similarities map[string]map[string]float64,
	deps *dependencyGraph,
	history []HistoryEntry,
	streaming *streamingStats,
	clauses map[string]*Clause,
) *snapshot {
	s := &snapshot{
		generation: generation,
		createdAt:  time.Now().UnixMilli(),
		graphQuery: nil, // Set via setGraphQuery after creation
	}

	// Deep copy proof data
	if proofs != nil {
		s.proofData = maps.Clone(proofs)
	} else {
		s.proofData = make(map[string]ProofNumber)
	}

	// Deep copy constraint data
	if constraints != nil {
		s.constraintData = maps.Clone(constraints)
	} else {
		s.constraintData = make(map[string]Constraint)
	}

	// Deep copy similarity data (nested map)
	if similarities != nil {
		s.similarityData = make(map[string]map[string]float64, len(similarities))
		for k, v := range similarities {
			s.similarityData[k] = maps.Clone(v)
		}
	} else {
		s.similarityData = make(map[string]map[string]float64)
	}

	// Deep copy dependency graph
	if deps != nil {
		s.dependencyData = deps.clone()
	} else {
		s.dependencyData = newDependencyGraph()
	}

	// Deep copy history (including Metadata maps for true immutability)
	if history != nil {
		s.historyData = make([]HistoryEntry, len(history))
		for i, entry := range history {
			s.historyData[i] = entry // struct copy
			if entry.Metadata != nil {
				s.historyData[i].Metadata = maps.Clone(entry.Metadata)
			}
		}
	} else {
		s.historyData = make([]HistoryEntry, 0)
	}

	// Deep copy streaming stats
	if streaming != nil {
		s.streamingData = streaming.clone()
	} else {
		s.streamingData = newStreamingStats()
	}

	// Deep copy clause data (CRS-04)
	if clauses != nil {
		s.clauseData = make(map[string]*Clause, len(clauses))
		for k, v := range clauses {
			// Deep copy the clause struct
			clauseCopy := *v
			// Deep copy literals slice
			if v.Literals != nil {
				clauseCopy.Literals = make([]Literal, len(v.Literals))
				copy(clauseCopy.Literals, v.Literals)
			}
			s.clauseData[k] = &clauseCopy
		}
	} else {
		s.clauseData = make(map[string]*Clause)
	}

	return s
}

// Generation returns the generation when this snapshot was created.
func (s *snapshot) Generation() int64 {
	return s.generation
}

// CreatedAt returns when this snapshot was created (Unix milliseconds UTC).
func (s *snapshot) CreatedAt() int64 {
	return s.createdAt
}

// ProofIndex returns the proof numbers index view.
func (s *snapshot) ProofIndex() ProofIndexView {
	return &proofIndexView{data: s.proofData}
}

// ConstraintIndex returns the constraint index view.
func (s *snapshot) ConstraintIndex() ConstraintIndexView {
	return &constraintIndexView{data: s.constraintData, clauses: s.clauseData}
}

// SimilarityIndex returns the similarity index view.
func (s *snapshot) SimilarityIndex() SimilarityIndexView {
	return &similarityIndexView{data: s.similarityData}
}

// DependencyIndex returns the dependency index view.
//
// Description:
//
//	Returns the graph-backed dependency index if available (GR-32), otherwise
//	falls back to the legacy dependencyGraph wrapper for backwards compatibility.
//
// Thread Safety: Safe for concurrent use (snapshot is immutable).
func (s *snapshot) DependencyIndex() DependencyIndexView {
	// GR-32: Prefer graph-backed index if available
	if s.graphBackedDepIndex != nil {
		return s.graphBackedDepIndex
	}
	// Fallback to legacy wrapper
	return &dependencyIndexView{graph: s.dependencyData}
}

// HistoryIndex returns the history index view.
func (s *snapshot) HistoryIndex() HistoryIndexView {
	return &historyIndexView{entries: s.historyData}
}

// StreamingIndex returns the streaming statistics index view.
func (s *snapshot) StreamingIndex() StreamingIndexView {
	return &streamingIndexView{stats: s.streamingData}
}

// GraphQuery returns read-only access to the code graph.
//
// Description:
//
//	Returns the graph query interface for activities to query the actual
//	code graph. Returns nil if graph is not available (e.g., during
//	initialization or if SetGraphProvider has not been called).
//
// Outputs:
//   - GraphQuery: The graph query interface, or nil if unavailable.
//
// Thread Safety: Safe for concurrent use (snapshot is immutable).
func (s *snapshot) GraphQuery() GraphQuery {
	return s.graphQuery
}

// setGraphQuery sets the graph query interface for this snapshot.
// Used internally by CRS when creating snapshots.
func (s *snapshot) setGraphQuery(gq GraphQuery) {
	s.graphQuery = gq
}

// setGraphBackedDepIndex sets the graph-backed dependency index for this snapshot.
// Used internally by CRS when creating snapshots (GR-32).
func (s *snapshot) setGraphBackedDepIndex(idx *GraphBackedDependencyIndex) {
	s.graphBackedDepIndex = idx
}

// -----------------------------------------------------------------------------
// Proof Index View
// -----------------------------------------------------------------------------

type proofIndexView struct {
	data map[string]ProofNumber
}

func (v *proofIndexView) Get(nodeID string) (ProofNumber, bool) {
	proof, ok := v.data[nodeID]
	return proof, ok
}

func (v *proofIndexView) All() map[string]ProofNumber {
	// Return a copy to maintain immutability
	return maps.Clone(v.data)
}

func (v *proofIndexView) Size() int {
	return len(v.data)
}

// -----------------------------------------------------------------------------
// Constraint Index View
// -----------------------------------------------------------------------------

type constraintIndexView struct {
	data    map[string]Constraint
	clauses map[string]*Clause // CRS-04: learned clauses
}

func (v *constraintIndexView) Get(constraintID string) (Constraint, bool) {
	c, ok := v.data[constraintID]
	return c, ok
}

func (v *constraintIndexView) FindByType(constraintType ConstraintType) []Constraint {
	var result []Constraint
	for _, c := range v.data {
		if c.Type == constraintType {
			result = append(result, c)
		}
	}
	return result
}

func (v *constraintIndexView) FindByNode(nodeID string) []Constraint {
	var result []Constraint
	for _, c := range v.data {
		for _, n := range c.Nodes {
			if n == nodeID {
				result = append(result, c)
				break
			}
		}
	}
	return result
}

func (v *constraintIndexView) All() map[string]Constraint {
	return maps.Clone(v.data)
}

func (v *constraintIndexView) Size() int {
	return len(v.data)
}

// --- Clause Methods (CRS-04) ---

// GetClause returns a learned clause by ID.
func (v *constraintIndexView) GetClause(clauseID string) (*Clause, bool) {
	clause, ok := v.clauses[clauseID]
	if !ok {
		return nil, false
	}
	// Return a copy to maintain immutability
	clauseCopy := *clause
	if clause.Literals != nil {
		clauseCopy.Literals = make([]Literal, len(clause.Literals))
		copy(clauseCopy.Literals, clause.Literals)
	}
	return &clauseCopy, true
}

// AllClauses returns all learned clauses.
func (v *constraintIndexView) AllClauses() map[string]*Clause {
	// Return a copy to maintain immutability
	result := make(map[string]*Clause, len(v.clauses))
	for k, clause := range v.clauses {
		clauseCopy := *clause
		if clause.Literals != nil {
			clauseCopy.Literals = make([]Literal, len(clause.Literals))
			copy(clauseCopy.Literals, clause.Literals)
		}
		result[k] = &clauseCopy
	}
	return result
}

// ClauseCount returns the number of learned clauses.
func (v *constraintIndexView) ClauseCount() int {
	return len(v.clauses)
}

// CheckAssignment checks if an assignment violates any learned clauses.
func (v *constraintIndexView) CheckAssignment(assignment map[string]bool) ClauseCheckResult {
	for _, clause := range v.clauses {
		if clause.IsViolated(assignment) {
			return ClauseCheckResult{
				Conflict:       true,
				ViolatedClause: clause,
				Reason:         "assignment violates learned clause: " + clause.String(),
			}
		}
	}
	return ClauseCheckResult{
		Conflict: false,
	}
}

// -----------------------------------------------------------------------------
// Similarity Index View
// -----------------------------------------------------------------------------

type similarityIndexView struct {
	data map[string]map[string]float64
}

func (v *similarityIndexView) Distance(node1, node2 string) (float64, bool) {
	if inner, ok := v.data[node1]; ok {
		if dist, ok := inner[node2]; ok {
			return dist, true
		}
	}
	// Try reverse lookup (similarity is symmetric)
	if inner, ok := v.data[node2]; ok {
		if dist, ok := inner[node1]; ok {
			return dist, true
		}
	}
	return 0, false
}

func (v *similarityIndexView) NearestNeighbors(nodeID string, k int) []SimilarityMatch {
	if k <= 0 {
		return nil
	}

	// Collect all distances from this node
	var matches []SimilarityMatch

	if inner, ok := v.data[nodeID]; ok {
		for otherID, dist := range inner {
			matches = append(matches, SimilarityMatch{NodeID: otherID, Distance: dist})
		}
	}

	// Also check reverse relationships
	for otherID, inner := range v.data {
		if otherID == nodeID {
			continue
		}
		if dist, ok := inner[nodeID]; ok {
			// Check if we already have this
			found := false
			for _, m := range matches {
				if m.NodeID == otherID {
					found = true
					break
				}
			}
			if !found {
				matches = append(matches, SimilarityMatch{NodeID: otherID, Distance: dist})
			}
		}
	}

	// Sort by distance (simple insertion sort for small k)
	for i := 1; i < len(matches); i++ {
		j := i
		for j > 0 && matches[j-1].Distance > matches[j].Distance {
			matches[j-1], matches[j] = matches[j], matches[j-1]
			j--
		}
	}

	// Return top k
	if len(matches) > k {
		matches = matches[:k]
	}
	return matches
}

func (v *similarityIndexView) Size() int {
	count := 0
	for _, inner := range v.data {
		count += len(inner)
	}
	return count
}

// AllPairs returns all similarity pairs for export.
//
// Description:
//
//	Returns a deep copy of the similarity matrix. The returned map
//	is safe to modify without affecting the snapshot.
//
// Outputs:
//   - map[string]map[string]float64: Deep copy of all pairs. Never nil.
//
// Thread Safety: Returns deep copy; safe for concurrent use.
func (v *similarityIndexView) AllPairs() map[string]map[string]float64 {
	result := make(map[string]map[string]float64, len(v.data))
	for fromID, inner := range v.data {
		result[fromID] = make(map[string]float64, len(inner))
		for toID, similarity := range inner {
			result[fromID][toID] = similarity
		}
	}
	return result
}

// AllPairsFiltered returns similarity pairs in one direction only for efficient export.
//
// Description:
//
//	Returns pairs where fromID < toID to avoid duplicates. More memory-efficient
//	than AllPairs() since similarity is symmetric and export only needs one direction.
//
// Inputs:
//   - maxPairs: Maximum pairs to return. -1 for unlimited.
//
// Outputs:
//   - []SimilarityPairData: Pairs in one direction. Never nil.
//   - bool: True if truncated due to limit.
//
// Thread Safety: Safe for concurrent use.
func (v *similarityIndexView) AllPairsFiltered(maxPairs int) ([]SimilarityPairData, bool) {
	// Pre-count for allocation (only one direction)
	count := 0
	for fromID, inner := range v.data {
		for toID := range inner {
			if fromID < toID {
				count++
			}
		}
	}

	// Apply limit to allocation
	allocSize := count
	if maxPairs > 0 && allocSize > maxPairs {
		allocSize = maxPairs
	}

	result := make([]SimilarityPairData, 0, allocSize)
	truncated := false
	pairCount := 0

	for fromID, inner := range v.data {
		for toID, similarity := range inner {
			if fromID < toID {
				// Check truncation limit
				if maxPairs > 0 && pairCount >= maxPairs {
					truncated = true
					break
				}

				result = append(result, SimilarityPairData{
					FromID:     fromID,
					ToID:       toID,
					Similarity: similarity,
				})
				pairCount++
			}
		}
		if truncated {
			break
		}
	}

	return result, truncated
}

// -----------------------------------------------------------------------------
// Dependency Graph (Internal)
// -----------------------------------------------------------------------------

type dependencyGraph struct {
	// forward: node -> nodes it depends on
	forward map[string]map[string]struct{}
	// reverse: node -> nodes that depend on it
	reverse map[string]map[string]struct{}
}

func newDependencyGraph() *dependencyGraph {
	return &dependencyGraph{
		forward: make(map[string]map[string]struct{}),
		reverse: make(map[string]map[string]struct{}),
	}
}

func (g *dependencyGraph) clone() *dependencyGraph {
	c := &dependencyGraph{
		forward: make(map[string]map[string]struct{}, len(g.forward)),
		reverse: make(map[string]map[string]struct{}, len(g.reverse)),
	}
	for k, v := range g.forward {
		c.forward[k] = make(map[string]struct{}, len(v))
		for kk := range v {
			c.forward[k][kk] = struct{}{}
		}
	}
	for k, v := range g.reverse {
		c.reverse[k] = make(map[string]struct{}, len(v))
		for kk := range v {
			c.reverse[k][kk] = struct{}{}
		}
	}
	return c
}

func (g *dependencyGraph) addEdge(from, to string) {
	if g.forward[from] == nil {
		g.forward[from] = make(map[string]struct{})
	}
	g.forward[from][to] = struct{}{}

	if g.reverse[to] == nil {
		g.reverse[to] = make(map[string]struct{})
	}
	g.reverse[to][from] = struct{}{}
}

func (g *dependencyGraph) dependsOn(nodeID string) []string {
	deps := g.forward[nodeID]
	if deps == nil {
		return nil
	}
	result := make([]string, 0, len(deps))
	for d := range deps {
		result = append(result, d)
	}
	return result
}

func (g *dependencyGraph) dependedBy(nodeID string) []string {
	deps := g.reverse[nodeID]
	if deps == nil {
		return nil
	}
	result := make([]string, 0, len(deps))
	for d := range deps {
		result = append(result, d)
	}
	return result
}

func (g *dependencyGraph) hasCycle(nodeID string) bool {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	return g.hasCycleUtil(nodeID, visited, recStack)
}

func (g *dependencyGraph) hasCycleUtil(nodeID string, visited, recStack map[string]bool) bool {
	visited[nodeID] = true
	recStack[nodeID] = true

	for dep := range g.forward[nodeID] {
		if !visited[dep] {
			if g.hasCycleUtil(dep, visited, recStack) {
				return true
			}
		} else if recStack[dep] {
			return true
		}
	}

	recStack[nodeID] = false
	return false
}

func (g *dependencyGraph) edgeCount() int {
	count := 0
	for _, deps := range g.forward {
		count += len(deps)
	}
	return count
}

// -----------------------------------------------------------------------------
// Dependency Index View
// -----------------------------------------------------------------------------

type dependencyIndexView struct {
	graph *dependencyGraph
}

func (v *dependencyIndexView) DependsOn(nodeID string) []string {
	return v.graph.dependsOn(nodeID)
}

func (v *dependencyIndexView) DependedBy(nodeID string) []string {
	return v.graph.dependedBy(nodeID)
}

func (v *dependencyIndexView) HasCycle(nodeID string) bool {
	return v.graph.hasCycle(nodeID)
}

func (v *dependencyIndexView) Size() int {
	return v.graph.edgeCount()
}

// AllEdges returns all dependency edges for export.
//
// Description:
//
//	Returns a deep copy of the forward edge map. The returned map
//	is safe to modify without affecting the snapshot.
//
// Outputs:
//   - map[string][]string: Deep copy of forward edges. Never nil.
//
// Thread Safety: Returns deep copy; safe for concurrent use.
func (v *dependencyIndexView) AllEdges() map[string][]string {
	result := make(map[string][]string, len(v.graph.forward))
	for fromID, toSet := range v.graph.forward {
		toIDs := make([]string, 0, len(toSet))
		for toID := range toSet {
			toIDs = append(toIDs, toID)
		}
		result[fromID] = toIDs
	}
	return result
}

// IsGraphBacked returns false for the legacy dependency graph implementation.
func (v *dependencyIndexView) IsGraphBacked() bool {
	return false
}

// -----------------------------------------------------------------------------
// History Index View
// -----------------------------------------------------------------------------

type historyIndexView struct {
	entries []HistoryEntry
}

func (v *historyIndexView) Trace(nodeID string) []HistoryEntry {
	var result []HistoryEntry
	for _, e := range v.entries {
		if e.NodeID == nodeID {
			result = append(result, e)
		}
	}
	return result
}

func (v *historyIndexView) Recent(n int) []HistoryEntry {
	if n <= 0 {
		return nil
	}
	if n >= len(v.entries) {
		// Return copy to maintain immutability
		result := make([]HistoryEntry, len(v.entries))
		copy(result, v.entries)
		return result
	}
	// Return the last n entries
	start := len(v.entries) - n
	result := make([]HistoryEntry, n)
	copy(result, v.entries[start:])
	return result
}

func (v *historyIndexView) Size() int {
	return len(v.entries)
}

// -----------------------------------------------------------------------------
// Streaming Stats (Internal)
// -----------------------------------------------------------------------------

type streamingStats struct {
	mu          sync.RWMutex
	frequencies map[string]uint64
	cardinality uint64
}

func newStreamingStats() *streamingStats {
	return &streamingStats{
		frequencies: make(map[string]uint64),
		cardinality: 0,
	}
}

func (s *streamingStats) clone() *streamingStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := &streamingStats{
		frequencies: maps.Clone(s.frequencies),
		cardinality: s.cardinality,
	}
	return c
}

func (s *streamingStats) estimate(item string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.frequencies[item]
}

func (s *streamingStats) getCardinality() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cardinality
}

func (s *streamingStats) size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Approximate memory: map overhead + entries
	return len(s.frequencies) * 32 // rough estimate
}

// -----------------------------------------------------------------------------
// Streaming Index View
// -----------------------------------------------------------------------------

type streamingIndexView struct {
	stats *streamingStats
}

func (v *streamingIndexView) Estimate(item string) uint64 {
	return v.stats.estimate(item)
}

func (v *streamingIndexView) Cardinality() uint64 {
	return v.stats.getCardinality()
}

func (v *streamingIndexView) Size() int {
	return v.stats.size()
}
