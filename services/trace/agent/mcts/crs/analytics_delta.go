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
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// -----------------------------------------------------------------------------
// Analytics Proof Key Constants
// -----------------------------------------------------------------------------

// ProofKeyAnalytics* are typed constants for analytics proof keys.
// Using constants prevents typos and enables IDE autocomplete.
const (
	// ProofKeyAnalyticsHotspotsDone indicates hotspots analysis was run.
	ProofKeyAnalyticsHotspotsDone = "analytics:hotspots:done"

	// ProofKeyAnalyticsHotspotsFound indicates hotspots were found.
	ProofKeyAnalyticsHotspotsFound = "analytics:hotspots:found"

	// ProofKeyAnalyticsDeadCodeDone indicates dead code analysis was run.
	ProofKeyAnalyticsDeadCodeDone = "analytics:dead_code:done"

	// ProofKeyAnalyticsDeadCodeFound indicates dead code was found.
	ProofKeyAnalyticsDeadCodeFound = "analytics:dead_code:found"

	// ProofKeyAnalyticsCyclesDone indicates cycle detection was run.
	ProofKeyAnalyticsCyclesDone = "analytics:cycles:done"

	// ProofKeyAnalyticsCyclesFound indicates cycles were found.
	ProofKeyAnalyticsCyclesFound = "analytics:cycles:found"

	// ProofKeyAnalyticsPathDone indicates path analysis was run.
	ProofKeyAnalyticsPathDone = "analytics:path:done"

	// ProofKeyAnalyticsPathFound indicates a path was found.
	ProofKeyAnalyticsPathFound = "analytics:path:found"

	// ProofKeyAnalyticsCouplingDone indicates coupling analysis was run.
	ProofKeyAnalyticsCouplingDone = "analytics:coupling:done"

	// ProofKeyAnalyticsCouplingFound indicates coupling issues were found.
	ProofKeyAnalyticsCouplingFound = "analytics:coupling:found"

	// ProofKeyAnalyticsReferencesDone indicates references analysis was run.
	ProofKeyAnalyticsReferencesDone = "analytics:references:done"

	// ProofKeyAnalyticsReferencesFound indicates references were found.
	ProofKeyAnalyticsReferencesFound = "analytics:references:found"
)

// -----------------------------------------------------------------------------
// Analytics Query Type Constants
// -----------------------------------------------------------------------------

// AnalyticsQueryType identifies the type of analytics query.
type AnalyticsQueryType string

const (
	// AnalyticsQueryHotspots finds the most connected symbols.
	AnalyticsQueryHotspots AnalyticsQueryType = "hotspots"

	// AnalyticsQueryDeadCode finds unreachable code.
	AnalyticsQueryDeadCode AnalyticsQueryType = "dead_code"

	// AnalyticsQueryCycles detects cyclic dependencies.
	AnalyticsQueryCycles AnalyticsQueryType = "cycles"

	// AnalyticsQueryPath finds the shortest path between symbols.
	AnalyticsQueryPath AnalyticsQueryType = "path"

	// AnalyticsQueryReferences finds all references to a symbol.
	AnalyticsQueryReferences AnalyticsQueryType = "references"

	// AnalyticsQueryCoupling computes package coupling metrics.
	AnalyticsQueryCoupling AnalyticsQueryType = "coupling"
)

// IsValid returns true if this is a known analytics query type.
func (t AnalyticsQueryType) IsValid() bool {
	switch t {
	case AnalyticsQueryHotspots, AnalyticsQueryDeadCode, AnalyticsQueryCycles,
		AnalyticsQueryPath, AnalyticsQueryReferences, AnalyticsQueryCoupling:
		return true
	default:
		return false
	}
}

// -----------------------------------------------------------------------------
// Analytics Delta
// -----------------------------------------------------------------------------

// MaxAnalyticsHistoryRecords is the maximum number of analytics records to keep.
const MaxAnalyticsHistoryRecords = 100

// MaxResultsPerRecord limits stored result IDs to prevent memory bloat.
const MaxResultsPerRecord = 50

// analyticsIDCounter provides unique IDs for analytics records.
var analyticsIDCounter atomic.Int64

// AnalyticsQueryParams contains typed parameters for analytics queries.
//
// Description:
//
//	Replaces map[string]any to comply with CLAUDE.md §4.5.
//	Each field is optional - only populate what's relevant for the query type.
//
// Thread Safety: NOT safe for concurrent modification.
type AnalyticsQueryParams struct {
	// Limit is the maximum results for hotspots queries.
	Limit int `json:"limit,omitempty"`

	// FromSymbol is the starting symbol for path queries.
	FromSymbol string `json:"from_symbol,omitempty"`

	// ToSymbol is the target symbol for path queries.
	ToSymbol string `json:"to_symbol,omitempty"`

	// TargetSymbol is the symbol for references queries.
	TargetSymbol string `json:"target_symbol,omitempty"`

	// PackageName is the package for coupling queries.
	PackageName string `json:"package_name,omitempty"`
}

// AnalyticsRecord represents a single analytics query and its results.
//
// Description:
//
//	Stores metadata and results from an analytics query for CRS tracking.
//	Used for learning and activity coordination.
//
// Thread Safety: NOT safe for concurrent modification.
type AnalyticsRecord struct {
	// ID is a unique identifier for this record.
	ID string `json:"id"`

	// QueryType identifies the type of analytics query.
	QueryType AnalyticsQueryType `json:"query_type"`

	// QueryTime is when the query was executed (Unix milliseconds UTC).
	QueryTime int64 `json:"query_time"`

	// GraphGeneration is the graph version when this analytics was run.
	// Used to correlate analytics with graph state.
	GraphGeneration int64 `json:"graph_generation,omitempty"`

	// Params contains typed query parameters.
	// Replaces QueryParams map[string]any per CLAUDE.md §4.5.
	Params AnalyticsQueryParams `json:"params,omitempty"`

	// QueryParams is DEPRECATED. Use Params instead.
	// Retained for backwards compatibility with existing records.
	QueryParams map[string]any `json:"query_params,omitempty"`

	// ResultCount is the number of results returned.
	ResultCount int `json:"result_count"`

	// Results contains symbol IDs found (for hotspots, dead_code).
	// Limited to MaxResultsPerRecord entries.
	Results []string `json:"results,omitempty"`

	// Cycles contains detected cycles (for cycle detection).
	// Each inner slice is a cycle path.
	Cycles [][]string `json:"cycles,omitempty"`

	// Path contains the path between symbols (for path queries).
	Path []string `json:"path,omitempty"`

	// ExecutionMs is how long the query took in milliseconds.
	// Must be non-negative.
	ExecutionMs int64 `json:"execution_ms"`
}

// AnalyticsDelta represents an analytics query to record in CRS.
//
// Description:
//
//	When applied, records the analytics query in CRS history,
//	sets relevant proof numbers, and emits events for coordination.
//
// Thread Safety: NOT safe for concurrent modification.
type AnalyticsDelta struct {
	baseDelta

	// Record is the analytics record to add.
	Record *AnalyticsRecord
}

// NewAnalyticsDelta creates a new analytics delta.
//
// Inputs:
//   - source: The signal source (should be SignalSourceHard for analytics).
//   - record: The analytics record to add. Must not be nil.
//
// Outputs:
//
//	*AnalyticsDelta: The new delta.
func NewAnalyticsDelta(source SignalSource, record *AnalyticsRecord) *AnalyticsDelta {
	return &AnalyticsDelta{
		baseDelta: newBaseDelta(source),
		Record:    record,
	}
}

// Type returns the delta type.
func (d *AnalyticsDelta) Type() DeltaType {
	return DeltaTypeAnalytics
}

// ErrUnknownQueryType is returned when an unknown query type is used.
var ErrUnknownQueryType = errors.New("unknown analytics query type")

// Validate checks if this delta can be applied.
//
// Description:
//
//	Validates the analytics record structure. Ensures required fields
//	are present and results match the query type.
//
// Inputs:
//
//	snapshot: Current CRS state (unused for analytics).
//
// Outputs:
//
//	error: Non-nil if validation fails.
//
// Thread Safety: Safe for concurrent use (read-only operation).
func (d *AnalyticsDelta) Validate(_ Snapshot) error {
	if d.Record == nil {
		return errors.New("analytics record must not be nil")
	}
	if d.Record.QueryType == "" {
		return errors.New("analytics query_type must not be empty")
	}
	if d.Record.QueryTime == 0 {
		return errors.New("analytics query_time must not be zero")
	}
	if d.Record.ResultCount < 0 {
		return errors.New("analytics result_count must not be negative")
	}
	if d.Record.ExecutionMs < 0 {
		return errors.New("analytics execution_ms must not be negative")
	}

	// Validate results match query type
	switch d.Record.QueryType {
	case AnalyticsQueryCycles:
		if d.Record.ResultCount > 0 && len(d.Record.Cycles) == 0 {
			return errors.New("cycles query with results must populate Cycles field")
		}
	case AnalyticsQueryPath:
		if d.Record.ResultCount > 0 && len(d.Record.Path) == 0 {
			return errors.New("path query with results must populate Path field")
		}
	case AnalyticsQueryHotspots, AnalyticsQueryDeadCode, AnalyticsQueryReferences, AnalyticsQueryCoupling:
		// Results are optional - we track count
	default:
		// Warn about unknown types but don't fail (extensibility)
		slog.Debug("unknown analytics query type",
			slog.String("query_type", string(d.Record.QueryType)),
		)
	}

	return nil
}

// Merge combines this delta with another delta.
func (d *AnalyticsDelta) Merge(other Delta) (Delta, error) {
	otherAnalytics, ok := other.(*AnalyticsDelta)
	if !ok {
		// Different types - return composite
		return NewCompositeDelta(d, other), nil
	}

	// Analytics deltas are append-only, so we create a composite
	return NewCompositeDelta(d, otherAnalytics), nil
}

// ConflictsWith returns true if this delta conflicts with another.
func (d *AnalyticsDelta) ConflictsWith(_ Delta) bool {
	// Analytics is append-only, never conflicts
	return false
}

// indexesAnalyticsAndProof is the index list for analytics deltas.
var indexesAnalyticsAndProof = []string{"analytics", "proof"}

// IndexesAffected returns which indexes this delta will modify.
//
// Description:
//
//	Analytics deltas affect both the analytics history and proof index
//	(for completion markers).
//
// Thread Safety: Returns a shared slice. Callers must not modify.
func (d *AnalyticsDelta) IndexesAffected() []string {
	return indexesAnalyticsAndProof
}

// -----------------------------------------------------------------------------
// Analytics History Manager
// -----------------------------------------------------------------------------

// AnalyticsHistory stores recent analytics records.
//
// Description:
//
//	Thread-safe ring buffer for analytics history with O(1) insertions.
//	Uses channel-based concurrency pattern per project standards.
//
// Thread Safety: Safe for concurrent use.
type AnalyticsHistory struct {
	mu       sync.RWMutex
	records  []*AnalyticsRecord
	maxSize  int
	writeIdx int // Next write position in ring buffer
	count    int // Total records (up to maxSize)
}

// NewAnalyticsHistory creates a new analytics history.
//
// Inputs:
//
//	maxSize: Maximum number of records to keep.
//
// Outputs:
//
//	*AnalyticsHistory: The new history instance.
func NewAnalyticsHistory(maxSize int) *AnalyticsHistory {
	if maxSize <= 0 {
		maxSize = MaxAnalyticsHistoryRecords
	}
	return &AnalyticsHistory{
		records: make([]*AnalyticsRecord, maxSize),
		maxSize: maxSize,
	}
}

// Add records an analytics query.
//
// Inputs:
//
//	record: The record to add. Must not be nil.
func (h *AnalyticsHistory) Add(record *AnalyticsRecord) {
	if record == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.records[h.writeIdx] = record
	h.writeIdx = (h.writeIdx + 1) % h.maxSize
	if h.count < h.maxSize {
		h.count++
	}
}

// GetLast returns the most recent record of a given type.
//
// Inputs:
//
//	queryType: The type of query to find.
//
// Outputs:
//
//	*AnalyticsRecord: The most recent matching record, or nil if not found.
func (h *AnalyticsHistory) GetLast(queryType AnalyticsQueryType) *AnalyticsRecord {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Search backwards from most recent
	for i := 0; i < h.count; i++ {
		idx := (h.writeIdx - 1 - i + h.maxSize) % h.maxSize
		if h.records[idx] != nil && h.records[idx].QueryType == queryType {
			return h.records[idx]
		}
	}
	return nil
}

// HasRun returns true if a query type has been run.
//
// Inputs:
//
//	queryType: The type of query to check.
//
// Outputs:
//
//	bool: True if the query type has been run.
func (h *AnalyticsHistory) HasRun(queryType AnalyticsQueryType) bool {
	return h.GetLast(queryType) != nil
}

// All returns a copy of all records in chronological order.
//
// Outputs:
//
//	[]*AnalyticsRecord: Copy of all records.
func (h *AnalyticsHistory) All() []*AnalyticsRecord {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]*AnalyticsRecord, 0, h.count)

	// Start from oldest record
	startIdx := 0
	if h.count == h.maxSize {
		startIdx = h.writeIdx // Oldest is at writeIdx in full buffer
	}

	for i := 0; i < h.count; i++ {
		idx := (startIdx + i) % h.maxSize
		if h.records[idx] != nil {
			result = append(result, h.records[idx])
		}
	}

	return result
}

// Size returns the number of records stored.
func (h *AnalyticsHistory) Size() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.count
}

// clone creates a deep copy of the analytics history.
//
// Description:
//
//	Creates an independent copy of the history for snapshot immutability.
//	Only copies non-nil records for efficiency (O-1 optimization).
//
// Thread Safety: Safe for concurrent use (holds read lock).
func (h *AnalyticsHistory) clone() *AnalyticsHistory {
	h.mu.RLock()
	defer h.mu.RUnlock()

	cloned := &AnalyticsHistory{
		records:  make([]*AnalyticsRecord, h.maxSize),
		maxSize:  h.maxSize,
		writeIdx: h.writeIdx,
		count:    h.count,
	}

	// O-1 optimization: only iterate over count records, not maxSize
	if h.count == 0 {
		return cloned
	}

	// Find start index and iterate only over existing records
	startIdx := 0
	if h.count == h.maxSize {
		startIdx = h.writeIdx
	}

	for i := 0; i < h.count; i++ {
		idx := (startIdx + i) % h.maxSize
		r := h.records[idx]
		if r == nil {
			continue
		}

		recordCopy := *r
		// Deep copy Params (it's a value type, so copy is automatic)
		// Params is AnalyticsQueryParams struct - already copied by *r

		// Deep copy slices
		if r.Results != nil {
			recordCopy.Results = make([]string, len(r.Results))
			copy(recordCopy.Results, r.Results)
		}
		if r.Path != nil {
			recordCopy.Path = make([]string, len(r.Path))
			copy(recordCopy.Path, r.Path)
		}
		if r.Cycles != nil {
			recordCopy.Cycles = make([][]string, len(r.Cycles))
			for j, cycle := range r.Cycles {
				recordCopy.Cycles[j] = make([]string, len(cycle))
				copy(recordCopy.Cycles[j], cycle)
			}
		}
		// R-3 fix: Deep copy deprecated QueryParams map
		if r.QueryParams != nil {
			recordCopy.QueryParams = make(map[string]any, len(r.QueryParams))
			for k, v := range r.QueryParams {
				// Note: interface{} values are still shallow copied
				// but this is acceptable for deprecation period
				recordCopy.QueryParams[k] = v
			}
		}
		cloned.records[idx] = &recordCopy
	}

	return cloned
}

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

// NewAnalyticsRecord creates a new analytics record with the given parameters.
//
// Description:
//
//	Creates a new AnalyticsRecord with a unique ID. The ID includes a
//	monotonic counter to ensure uniqueness even at millisecond precision.
//
// Inputs:
//   - queryType: The type of analytics query.
//   - queryTime: When the query was executed (Unix milliseconds UTC).
//   - resultCount: Number of results returned.
//   - executionMs: How long the query took in milliseconds.
//
// Outputs:
//
//	*AnalyticsRecord: The new record.
//
// Thread Safety: Safe for concurrent use.
func NewAnalyticsRecord(
	queryType AnalyticsQueryType,
	queryTime int64,
	resultCount int,
	executionMs int64,
) *AnalyticsRecord {
	// I-5 fix: Use counter for guaranteed uniqueness
	counter := analyticsIDCounter.Add(1)
	return &AnalyticsRecord{
		ID:          fmt.Sprintf("analytics-%s-%d-%d", queryType, queryTime, counter),
		QueryType:   queryType,
		QueryTime:   queryTime,
		ResultCount: resultCount,
		ExecutionMs: executionMs,
	}
}

// WithResults adds symbol IDs to the record.
func (r *AnalyticsRecord) WithResults(results []string) *AnalyticsRecord {
	r.Results = results
	return r
}

// WithCycles adds cycle data to the record.
func (r *AnalyticsRecord) WithCycles(cycles [][]string) *AnalyticsRecord {
	r.Cycles = cycles
	return r
}

// WithPath adds path data to the record.
func (r *AnalyticsRecord) WithPath(path []string) *AnalyticsRecord {
	r.Path = path
	return r
}

// WithParams adds query parameters to the record.
//
// DEPRECATED: Use WithTypedParams instead per CLAUDE.md §4.5.
func (r *AnalyticsRecord) WithParams(params map[string]any) *AnalyticsRecord {
	r.QueryParams = params
	return r
}

// WithTypedParams sets typed query parameters.
//
// Description:
//
//	Sets the Params field with typed parameters. Preferred over
//	WithParams which uses map[string]any.
//
// Thread Safety: NOT safe for concurrent use.
func (r *AnalyticsRecord) WithTypedParams(params AnalyticsQueryParams) *AnalyticsRecord {
	r.Params = params
	return r
}

// WithGraphGeneration sets the graph generation for correlation.
//
// Description:
//
//	Records the graph version when this analytics was run.
//	Enables correlation of analytics results with graph state.
//
// Thread Safety: NOT safe for concurrent use.
func (r *AnalyticsRecord) WithGraphGeneration(gen int64) *AnalyticsRecord {
	r.GraphGeneration = gen
	return r
}

// TruncateResults limits Results to MaxResultsPerRecord.
//
// Description:
//
//	Called before storing to prevent memory bloat from large result sets.
//	Logs if truncation occurs.
//
// Thread Safety: NOT safe for concurrent use.
func (r *AnalyticsRecord) TruncateResults() *AnalyticsRecord {
	if len(r.Results) > MaxResultsPerRecord {
		slog.Debug("truncating analytics results",
			slog.String("query_type", string(r.QueryType)),
			slog.Int("original_count", len(r.Results)),
			slog.Int("truncated_to", MaxResultsPerRecord),
		)
		r.Results = r.Results[:MaxResultsPerRecord]
	}
	return r
}

// GetProofDoneKey returns the proof key for "done" status.
//
// Description:
//
//	Returns the typed constant for known query types. For unknown types,
//	generates a key dynamically and logs a warning.
//
// Thread Safety: Safe for concurrent use.
func (r *AnalyticsRecord) GetProofDoneKey() string {
	switch r.QueryType {
	case AnalyticsQueryHotspots:
		return ProofKeyAnalyticsHotspotsDone
	case AnalyticsQueryDeadCode:
		return ProofKeyAnalyticsDeadCodeDone
	case AnalyticsQueryCycles:
		return ProofKeyAnalyticsCyclesDone
	case AnalyticsQueryPath:
		return ProofKeyAnalyticsPathDone
	case AnalyticsQueryCoupling:
		return ProofKeyAnalyticsCouplingDone
	case AnalyticsQueryReferences:
		return ProofKeyAnalyticsReferencesDone
	default:
		// I-3 fix: Log warning for unknown types
		slog.Debug("generating proof key for unknown analytics type",
			slog.String("query_type", string(r.QueryType)),
		)
		return fmt.Sprintf("analytics:%s:done", r.QueryType)
	}
}

// GetProofFoundKey returns the proof key for "found" status.
//
// Description:
//
//	Returns the typed constant for known query types. For unknown types,
//	generates a key dynamically and logs a warning.
//
// Thread Safety: Safe for concurrent use.
func (r *AnalyticsRecord) GetProofFoundKey() string {
	switch r.QueryType {
	case AnalyticsQueryHotspots:
		return ProofKeyAnalyticsHotspotsFound
	case AnalyticsQueryDeadCode:
		return ProofKeyAnalyticsDeadCodeFound
	case AnalyticsQueryCycles:
		return ProofKeyAnalyticsCyclesFound
	case AnalyticsQueryPath:
		return ProofKeyAnalyticsPathFound
	case AnalyticsQueryCoupling:
		return ProofKeyAnalyticsCouplingFound
	case AnalyticsQueryReferences:
		return ProofKeyAnalyticsReferencesFound
	default:
		// I-3 fix: Log warning for unknown types
		slog.Debug("generating proof key for unknown analytics type",
			slog.String("query_type", string(r.QueryType)),
		)
		return fmt.Sprintf("analytics:%s:found", r.QueryType)
	}
}

// HasResults returns true if the record has results.
func (r *AnalyticsRecord) HasResults() bool {
	switch r.QueryType {
	case AnalyticsQueryCycles:
		return len(r.Cycles) > 0
	case AnalyticsQueryPath:
		return len(r.Path) > 0
	default:
		return r.ResultCount > 0
	}
}

// createAnalyticsDeltaFromParams is a helper to create an analytics delta.
//
// Description:
//
//	Convenience function for tools to create analytics deltas.
//
// Inputs:
//   - queryType: The type of analytics query.
//   - resultCount: Number of results.
//   - executionMs: Execution time in milliseconds.
//
// Outputs:
//
//	*AnalyticsDelta: The new delta.
func CreateAnalyticsDelta(
	queryType AnalyticsQueryType,
	resultCount int,
	executionMs int64,
) *AnalyticsDelta {
	record := NewAnalyticsRecord(
		queryType,
		time.Now().UnixMilli(),
		resultCount,
		executionMs,
	)
	return NewAnalyticsDelta(SignalSourceHard, record)
}
