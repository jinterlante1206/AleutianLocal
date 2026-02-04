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
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// -----------------------------------------------------------------------------
// Cycle Detection using Brent's Algorithm
// -----------------------------------------------------------------------------

// CycleDetector uses Brent's algorithm for online cycle detection.
//
// Description:
//
//	Maintains state between calls to detect cycles incrementally.
//	Each AddStep() call is O(1) amortized, enabling real-time detection.
//
//	Brent's algorithm uses the "teleporting tortoise" approach:
//	- Hare moves one step at a time
//	- Tortoise teleports to hare's position when power of 2 is reached
//	- Cycle detected when hare == tortoise
//
//	This is preferred over Floyd's tortoise-hare because:
//	- Fewer comparisons on average (24-36% fewer iterations)
//	- Same O(λ + μ) time complexity where λ=cycle length, μ=tail length
//	- O(1) space (vs O(V) for Tarjan SCC)
//
// Thread Safety: Safe for concurrent use.
type CycleDetector struct {
	mu sync.Mutex

	// State tracking (Brent's algorithm)
	power    int    // Current power of 2
	lambda   int    // Current cycle length estimate
	tortoise string // Slow pointer (state at power-of-2 boundary)
	hare     string // Fast pointer (current state)

	// History for cycle extraction
	stateSeq []string // Sequence of states seen (for cycle extraction)

	// Configuration
	maxHistory int // Maximum states to track (prevents unbounded memory)

	// Statistics
	stepsProcessed int       // Total steps processed
	cyclesDetected int       // Total cycles detected
	lastCycleTime  time.Time // When the last cycle was detected
}

// CycleDetectorConfig configures the cycle detector.
type CycleDetectorConfig struct {
	// MaxHistory is the maximum number of states to track for cycle extraction.
	// Larger values enable detecting longer cycles but use more memory.
	// Default: 1000
	MaxHistory int
}

// DefaultCycleDetectorConfig returns the default cycle detector configuration.
func DefaultCycleDetectorConfig() *CycleDetectorConfig {
	return &CycleDetectorConfig{
		MaxHistory: 1000,
	}
}

// NewCycleDetector creates a new cycle detector.
//
// Description:
//
//	Creates a CycleDetector configured for real-time cycle detection.
//	The detector maintains state between AddStep() calls to enable
//	online cycle detection with O(1) amortized time per step.
//
// Inputs:
//
//	config - Configuration options. Uses defaults if nil.
//
// Outputs:
//
//	*CycleDetector - The configured detector. Never nil.
func NewCycleDetector(config *CycleDetectorConfig) *CycleDetector {
	if config == nil {
		config = DefaultCycleDetectorConfig()
	}

	maxHistory := config.MaxHistory
	if maxHistory <= 0 {
		maxHistory = 1000
	}

	return &CycleDetector{
		power:      1,
		lambda:     1,
		stateSeq:   make([]string, 0, min(maxHistory, 100)), // Pre-allocate some capacity
		maxHistory: maxHistory,
	}
}

// CycleDetectionResult contains the result of a cycle detection check.
type CycleDetectionResult struct {
	// Detected is true if a cycle was found.
	Detected bool

	// Cycle contains the states in the detected cycle.
	// Empty if no cycle detected.
	Cycle []string

	// CycleLength is the length of the detected cycle.
	CycleLength int

	// TailLength is the number of states before the cycle starts.
	TailLength int

	// StateKey is the state key that triggered detection.
	StateKey string

	// Errors contains any non-fatal errors during detection.
	// A-02: Added to report partial failures.
	Errors []error
}

// AddStep processes a new state and returns cycle detection result.
//
// Description:
//
//	Uses Brent's cycle detection algorithm incrementally.
//	State is a normalized representation of the current decision.
//
//	Time Complexity: O(1) amortized per call.
//	Space Complexity: O(maxHistory) total.
//
// Inputs:
//
//	state - Normalized state string (e.g., "tool:list_packages:success:router").
//
// Outputs:
//
//	CycleDetectionResult - Contains detection status and cycle if found.
//
// Thread Safety: Safe for concurrent use.
func (d *CycleDetector) AddStep(state string) CycleDetectionResult {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.stepsProcessed++

	// Track state sequence for cycle extraction (bounded memory)
	if len(d.stateSeq) < d.maxHistory {
		d.stateSeq = append(d.stateSeq, state)
	} else {
		// Sliding window: remove oldest, add newest
		// This limits cycle detection to recent maxHistory states
		copy(d.stateSeq, d.stateSeq[1:])
		d.stateSeq[len(d.stateSeq)-1] = state
	}

	// Initialize on first step
	if d.tortoise == "" {
		d.tortoise = state
		d.hare = state
		return CycleDetectionResult{StateKey: state}
	}

	// Brent's algorithm: update hare (always moves)
	d.hare = state

	// Check for cycle: hare meets tortoise
	if d.hare == d.tortoise && d.stepsProcessed > 1 {
		// Cycle detected - extract it
		// I-01: Use extractCycleResult for accurate tail length calculation
		extracted := d.extractCycleLocked()
		d.cyclesDetected++
		d.lastCycleTime = time.Now()

		// I-01: Calculate tail length from the actual cycle start position
		tailLength := extracted.tailStart

		// Reset detector state for next potential cycle
		d.resetStateLocked()

		return CycleDetectionResult{
			Detected:    true,
			Cycle:       extracted.cycle,
			CycleLength: len(extracted.cycle),
			TailLength:  tailLength,
			StateKey:    state,
		}
	}

	// Update lambda (steps since last teleport)
	d.lambda++

	// Teleport tortoise when power of 2 reached
	if d.lambda == d.power {
		d.tortoise = d.hare
		d.power *= 2
		d.lambda = 0
	}

	return CycleDetectionResult{StateKey: state}
}

// extractCycleResult contains the extracted cycle and its position.
type extractCycleResult struct {
	cycle     []string
	tailStart int // Index where the cycle starts (tail length)
}

// extractCycleLocked extracts the cycle from the state sequence.
// Caller must hold d.mu.
// I-01: Returns both cycle and tail position for accurate TailLength calculation.
func (d *CycleDetector) extractCycleLocked() extractCycleResult {
	target := d.hare

	// Find cycle start by searching backwards for the repeated state
	for i := len(d.stateSeq) - 2; i >= 0; i-- {
		if d.stateSeq[i] == target {
			// Found cycle start - return the cycle
			cycle := make([]string, len(d.stateSeq)-i-1)
			copy(cycle, d.stateSeq[i:len(d.stateSeq)-1])
			return extractCycleResult{
				cycle:     cycle,
				tailStart: i,
			}
		}
	}

	// Fallback: single-state cycle (A -> A)
	return extractCycleResult{
		cycle:     []string{target},
		tailStart: len(d.stateSeq) - 1,
	}
}

// resetStateLocked resets the detector state for detecting the next cycle.
// Caller must hold d.mu.
func (d *CycleDetector) resetStateLocked() {
	d.power = 1
	d.lambda = 1
	d.tortoise = ""
	d.hare = ""
	// Keep stateSeq for debugging but mark position
}

// Reset clears all detector state.
//
// Description:
//
//	Use when starting a new session or when cycle has been handled.
//
// Thread Safety: Safe for concurrent use.
func (d *CycleDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.power = 1
	d.lambda = 1
	d.tortoise = ""
	d.hare = ""
	d.stateSeq = d.stateSeq[:0]
	d.stepsProcessed = 0
}

// Stats returns the detector statistics.
//
// Thread Safety: Safe for concurrent use.
func (d *CycleDetector) Stats() CycleDetectorStats {
	d.mu.Lock()
	defer d.mu.Unlock()

	return CycleDetectorStats{
		StepsProcessed: d.stepsProcessed,
		CyclesDetected: d.cyclesDetected,
		LastCycleTime:  d.lastCycleTime,
		HistorySize:    len(d.stateSeq),
		MaxHistory:     d.maxHistory,
	}
}

// CycleDetectorStats contains detector statistics.
type CycleDetectorStats struct {
	StepsProcessed int
	CyclesDetected int
	LastCycleTime  time.Time
	HistorySize    int
	MaxHistory     int
}

// -----------------------------------------------------------------------------
// State Key Generation
// -----------------------------------------------------------------------------

// GetStateKey generates a normalized state key for cycle detection.
//
// Description:
//
//	Creates a canonical state representation that ignores step numbers
//	but captures the semantically relevant state:
//	- Tool name (what tool was used/selected)
//	- Outcome (success/failure/forced)
//	- Actor (who made the decision)
//
//	This enables detecting semantic cycles like:
//	"router selects list_packages -> executes -> router selects list_packages"
//	regardless of step numbers or timestamps.
//
// Inputs:
//
//	step - The step record to generate a key for.
//
// Outputs:
//
//	string - Normalized state key (e.g., "tool:list_packages:success:router").
func GetStateKey(step StepRecord) string {
	// State = decision:tool:outcome:actor
	// This captures the semantic state while ignoring timing
	return fmt.Sprintf("%s:%s:%s:%s",
		step.Decision.String(),
		step.Tool,
		step.Outcome.String(),
		step.Actor.String(),
	)
}

// GetToolStateKey generates a tool-focused state key for cycle detection.
//
// Description:
//
//	Creates a simpler state key focused only on tool usage.
//	Use this for detecting tool repetition cycles specifically.
//
// Inputs:
//
//	step - The step record to generate a key for.
//
// Outputs:
//
//	string - Tool-focused state key (e.g., "list_packages:success").
func GetToolStateKey(step StepRecord) string {
	if step.Tool == "" {
		return fmt.Sprintf("no_tool:%s", step.Outcome.String())
	}
	return fmt.Sprintf("%s:%s", step.Tool, step.Outcome.String())
}

// -----------------------------------------------------------------------------
// Cycle Detection Integration with CRS
// -----------------------------------------------------------------------------

// CheckCycleOnStep checks for cycles after a step is recorded.
//
// Description:
//
//	Called after each step to detect cycles in real-time using Brent's algorithm.
//	If a cycle is detected, marks all cycle states as disproven in the proof index
//	and records a circuit breaker step.
//
//	This is the main integration point between cycle detection and CRS.
//
// Inputs:
//
//	ctx - Context for cancellation and tracing.
//	crsInstance - The CRS instance for recording and proof updates.
//	step - The step that was just recorded.
//	detector - The cycle detector for this session.
//
// Outputs:
//
//	CycleDetectionResult - The detection result.
//
// Thread Safety: Safe for concurrent use.
func CheckCycleOnStep(
	ctx context.Context,
	crsInstance CRS,
	step StepRecord,
	detector *CycleDetector,
) CycleDetectionResult {
	if ctx == nil || crsInstance == nil || detector == nil {
		return CycleDetectionResult{}
	}

	// Create span for tracing
	ctx, span := otel.Tracer("crs").Start(ctx, "crs.CheckCycleOnStep",
		trace.WithAttributes(
			attribute.String("session_id", step.SessionID),
			attribute.String("tool", step.Tool),
			attribute.String("decision", step.Decision.String()),
		),
	)
	defer span.End()

	// Generate state key and check for cycle
	stateKey := GetStateKey(step)
	result := detector.AddStep(stateKey)

	span.SetAttributes(
		attribute.String("state_key", stateKey),
		attribute.Bool("cycle_detected", result.Detected),
	)

	if !result.Detected {
		return result
	}

	// I-02: Check context cancellation before heavy operations
	if ctx.Err() != nil {
		result.Errors = append(result.Errors, ctx.Err())
		return result
	}

	// Cycle detected - mark all cycle nodes as disproven
	span.SetAttributes(
		attribute.Int("cycle_length", result.CycleLength),
		attribute.StringSlice("cycle_states", result.Cycle),
	)

	// I-03: Collect errors instead of silently ignoring them
	var errs []error

	for _, state := range result.Cycle {
		nodeID := fmt.Sprintf("session:%s:state:%s", step.SessionID, state)
		err := crsInstance.UpdateProofNumber(ctx, ProofUpdate{
			NodeID: nodeID,
			Type:   ProofUpdateTypeDisproven,
			Delta:  0,
			Reason: fmt.Sprintf("cycle_detected:%v", result.Cycle),
			Source: SignalSourceHard, // Cycles are definitive failures
		})
		if err != nil {
			span.RecordError(err)
			errs = append(errs, fmt.Errorf("update proof for %s: %w", nodeID, err))
		}
	}

	// A-01: Use a distinct step number suffix to avoid collision
	// Circuit breaker steps use negative numbers to distinguish from regular steps
	cbStepNumber := -(step.StepNumber + 1)

	// Record circuit breaker step for cycle detection
	cbStep := StepRecord{
		StepNumber: cbStepNumber,
		Timestamp:  time.Now(),
		SessionID:  step.SessionID,
		Actor:      ActorSystem,
		Decision:   DecisionCircuitBreaker,
		Outcome:    OutcomeForced,
		Reasoning:  fmt.Sprintf("Brent's algorithm detected cycle of length %d: %v", result.CycleLength, result.Cycle),
		Propagate:  true,
		Terminal:   false,
	}
	if err := crsInstance.RecordStep(ctx, cbStep); err != nil {
		span.RecordError(err)
		errs = append(errs, fmt.Errorf("record circuit breaker step: %w", err))
	}

	// I-03: Attach collected errors to result
	result.Errors = errs

	return result
}

// -----------------------------------------------------------------------------
// Post-Session Cycle Analysis (Tarjan SCC)
// -----------------------------------------------------------------------------

// CycleAnalysis contains post-session cycle analysis results.
type CycleAnalysis struct {
	// SessionID is the session that was analyzed.
	SessionID string `json:"session_id"`

	// TotalSCCs is the total number of strongly connected components.
	TotalSCCs int `json:"total_sccs"`

	// CyclicSCCs contains SCCs with more than one node (actual cycles).
	CyclicSCCs [][]string `json:"cyclic_sccs"`

	// LargestSCCSize is the size of the largest cyclic SCC.
	LargestSCCSize int `json:"largest_scc_size"`

	// AnalysisTime is when the analysis was performed.
	AnalysisTime time.Time `json:"analysis_time"`

	// AnalysisDuration is how long the analysis took.
	AnalysisDuration time.Duration `json:"analysis_duration_ns"`
}

// DecisionGraph represents a graph of decisions for cycle analysis.
type DecisionGraph struct {
	// Nodes are the node IDs (state keys).
	Nodes []string

	// Edges maps each node to its successors.
	Edges map[string][]string
}

// BuildDecisionGraph constructs a decision graph from step records.
//
// Description:
//
//	Builds a directed graph where:
//	- Nodes are state keys (from GetStateKey)
//	- Edges connect consecutive states
//
//	This graph can then be analyzed with Tarjan SCC for comprehensive
//	cycle detection.
//
// Inputs:
//
//	steps - The step records to build the graph from.
//
// Outputs:
//
//	*DecisionGraph - The decision graph. Never nil.
func BuildDecisionGraph(steps []StepRecord) *DecisionGraph {
	graph := &DecisionGraph{
		Nodes: make([]string, 0),
		Edges: make(map[string][]string),
	}

	if len(steps) == 0 {
		return graph
	}

	nodeSet := make(map[string]struct{})

	var prevState string
	for _, step := range steps {
		stateKey := GetStateKey(step)

		// Add node
		if _, exists := nodeSet[stateKey]; !exists {
			nodeSet[stateKey] = struct{}{}
			graph.Nodes = append(graph.Nodes, stateKey)
		}

		// Add edge from previous state
		if prevState != "" && prevState != stateKey {
			graph.Edges[prevState] = append(graph.Edges[prevState], stateKey)
		}

		prevState = stateKey
	}

	return graph
}

// -----------------------------------------------------------------------------
// Post-Session Analysis using Tarjan SCC
// -----------------------------------------------------------------------------

// AnalyzeSessionCycles performs comprehensive cycle analysis using Tarjan SCC.
//
// Description:
//
//	Called at session end or on-demand for debugging. Uses Tarjan's algorithm
//	to find ALL strongly connected components in the decision graph.
//
//	Unlike Brent's algorithm (which detects single cycles as they form),
//	Tarjan finds ALL cycles including:
//	- Multi-step cycles that Brent's might miss
//	- Complex SCCs with multiple entry/exit points
//	- Cycles that span multiple decision branches
//
//	This is more expensive than Brent's (O(V+E) vs O(1) per step) but provides
//	complete analysis for debugging and learning.
//
// When to call:
//
//   - Session end (for learning)
//   - On-demand debugging
//   - NOT in the hot path (too expensive)
//
// Inputs:
//
//	ctx - Context for cancellation.
//	crsInstance - The CRS instance to get step history from.
//	sessionID - The session to analyze.
//
// Outputs:
//
//	*CycleAnalysis - Analysis results. Never nil.
//	error - Non-nil on failure.
func AnalyzeSessionCycles(ctx context.Context, crsInstance CRS, sessionID string) (*CycleAnalysis, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if crsInstance == nil {
		return nil, fmt.Errorf("crsInstance must not be nil")
	}
	if sessionID == "" {
		return nil, fmt.Errorf("sessionID must not be empty")
	}

	startTime := time.Now()

	// Create span for tracing
	ctx, span := otel.Tracer("crs").Start(ctx, "crs.AnalyzeSessionCycles",
		trace.WithAttributes(
			attribute.String("session_id", sessionID),
		),
	)
	defer span.End()

	// Get step history from CRS
	steps := crsInstance.GetStepHistory(sessionID)
	if len(steps) == 0 {
		return &CycleAnalysis{
			SessionID:        sessionID,
			AnalysisTime:     startTime,
			AnalysisDuration: time.Since(startTime),
		}, nil
	}

	// Build decision graph
	graph := BuildDecisionGraph(steps)

	span.SetAttributes(
		attribute.Int("node_count", len(graph.Nodes)),
		attribute.Int("edge_count", len(graph.Edges)),
	)

	// R-02: Run Tarjan SCC with context for cancellation support
	sccs := tarjanSCCWithConfig(graph.Nodes, graph.Edges, &tarjanSCCConfig{
		MaxDepth: 10000,
		Ctx:      ctx,
	})

	// Build analysis result
	analysis := &CycleAnalysis{
		SessionID:        sessionID,
		TotalSCCs:        len(sccs),
		CyclicSCCs:       make([][]string, 0),
		LargestSCCSize:   0,
		AnalysisTime:     startTime,
		AnalysisDuration: time.Since(startTime),
	}

	for _, scc := range sccs {
		if len(scc) > 1 {
			// This SCC has multiple nodes - it's a cycle
			analysis.CyclicSCCs = append(analysis.CyclicSCCs, scc)
			if len(scc) > analysis.LargestSCCSize {
				analysis.LargestSCCSize = len(scc)
			}
		}
	}

	span.SetAttributes(
		attribute.Int("total_sccs", analysis.TotalSCCs),
		attribute.Int("cyclic_sccs", len(analysis.CyclicSCCs)),
		attribute.Int("largest_scc", analysis.LargestSCCSize),
	)

	return analysis, nil
}

// tarjanSCCConfig configures Tarjan SCC behavior.
type tarjanSCCConfig struct {
	// MaxDepth limits recursion depth to prevent stack overflow.
	// R-01: Default 10000 should handle most real-world graphs.
	MaxDepth int

	// Context for cancellation checking.
	// R-02: Checked periodically during iteration.
	Ctx context.Context
}

// defaultTarjanConfig returns default Tarjan configuration.
func defaultTarjanConfig() *tarjanSCCConfig {
	return &tarjanSCCConfig{
		MaxDepth: 10000,
		Ctx:      context.Background(),
	}
}

// tarjanSCC is a simplified Tarjan SCC implementation for post-session analysis.
//
// Description:
//
//	Inline implementation to avoid circular dependencies with algorithms/graph.
//	For production use with large graphs, prefer the full TarjanSCC implementation
//	in services/trace/agent/mcts/algorithms/graph/tarjan_scc.go.
//
//	R-01: Includes depth limit to prevent stack overflow.
//	R-02: Checks context cancellation periodically.
func tarjanSCC(nodes []string, edges map[string][]string) [][]string {
	return tarjanSCCWithConfig(nodes, edges, defaultTarjanConfig())
}

// tarjanSCCWithConfig runs Tarjan SCC with configuration options.
func tarjanSCCWithConfig(nodes []string, edges map[string][]string, config *tarjanSCCConfig) [][]string {
	if config == nil {
		config = defaultTarjanConfig()
	}

	index := 0
	stack := make([]string, 0, len(nodes))
	onStack := make(map[string]bool, len(nodes))
	indices := make(map[string]int, len(nodes))
	lowlinks := make(map[string]int, len(nodes))
	sccs := make([][]string, 0)
	depth := 0
	checkInterval := 100 // Check cancellation every N operations

	var strongConnect func(v string) bool
	strongConnect = func(v string) bool {
		// R-01: Check depth limit
		depth++
		if depth > config.MaxDepth {
			return false // Abort - graph too deep
		}
		defer func() { depth-- }()

		// R-02: Periodic cancellation check
		if index%checkInterval == 0 && config.Ctx != nil && config.Ctx.Err() != nil {
			return false
		}

		indices[v] = index
		lowlinks[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range edges[v] {
			if _, visited := indices[w]; !visited {
				if !strongConnect(w) {
					return false // Propagate abort
				}
				if lowlinks[w] < lowlinks[v] {
					lowlinks[v] = lowlinks[w]
				}
			} else if onStack[w] {
				if indices[w] < lowlinks[v] {
					lowlinks[v] = indices[w]
				}
			}
		}

		if lowlinks[v] == indices[v] {
			scc := make([]string, 0)
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			sccs = append(sccs, scc)
		}
		return true
	}

	for _, v := range nodes {
		// R-02: Check cancellation between top-level iterations
		if config.Ctx != nil && config.Ctx.Err() != nil {
			break
		}
		if _, visited := indices[v]; !visited {
			if !strongConnect(v) {
				break // Abort on depth limit or cancellation
			}
		}
	}

	return sccs
}
