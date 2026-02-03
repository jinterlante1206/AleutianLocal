// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package trust_flow

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/explore"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
	"github.com/AleutianAI/AleutianFOSS/services/trace/safety"
)

// globalVulnCounter is a process-wide counter for generating unique vulnerability IDs.
// Using a global counter ensures IDs are unique across all tracer instances.
var globalVulnCounter uint64

// InputTracerImpl implements the safety.InputTracer interface.
//
// Description:
//
//	InputTracerImpl performs taint tracking from untrusted input sources
//	to sensitive sinks, detecting when unsanitized data reaches dangerous
//	operations. It builds on top of explore.DataFlowTracer with security
//	enhancements.
//
// Thread Safety:
//
//	InputTracerImpl is safe for concurrent use. It performs read-only
//	operations on the graph and index.
type InputTracerImpl struct {
	graph      *graph.Graph
	idx        *index.SymbolIndex
	sources    *explore.SourceRegistry
	sinks      *explore.SinkRegistry
	sanitizers *SanitizerRegistry
	dataTracer *explore.DataFlowTracer
}

// NewInputTracer creates a new InputTracerImpl.
//
// Description:
//
//	Creates an input tracer that can trace untrusted data through code,
//	identifying vulnerabilities where data reaches sinks unsanitized.
//
// Inputs:
//
//	g - The code graph. Must be frozen.
//	idx - The symbol index.
//
// Outputs:
//
//	*InputTracerImpl - The configured tracer.
//
// Example:
//
//	tracer := NewInputTracer(graph, index)
//	trace, err := tracer.TraceUserInput(ctx, "handlers.HandleLogin")
func NewInputTracer(g *graph.Graph, idx *index.SymbolIndex) *InputTracerImpl {
	return &InputTracerImpl{
		graph:      g,
		idx:        idx,
		sources:    explore.NewSourceRegistry(),
		sinks:      explore.NewSinkRegistry(),
		sanitizers: NewSanitizerRegistry(),
		dataTracer: explore.NewDataFlowTracer(g, idx),
	}
}

// NewInputTracerWithRegistries creates an InputTracerImpl with custom registries.
//
// Description:
//
//	Creates a tracer with custom registries for specialized analysis
//	or custom security rules.
func NewInputTracerWithRegistries(
	g *graph.Graph,
	idx *index.SymbolIndex,
	sources *explore.SourceRegistry,
	sinks *explore.SinkRegistry,
	sanitizers *SanitizerRegistry,
) *InputTracerImpl {
	return &InputTracerImpl{
		graph:      g,
		idx:        idx,
		sources:    sources,
		sinks:      sinks,
		sanitizers: sanitizers,
		dataTracer: explore.NewDataFlowTracerWithRegistries(g, idx, sources, sinks),
	}
}

// TraceUserInput traces data flow from an input source and identifies vulnerabilities.
//
// Description:
//
//	Performs BFS traversal from the input source, tracking taint state
//	at each step. When untrusted data reaches a dangerous sink without
//	being sanitized, a vulnerability is reported.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout.
//	sourceID - The symbol ID of the input source to trace from.
//	opts - Optional configuration (max depth, sink filters, etc.).
//
// Outputs:
//
//	*safety.InputTrace - The trace result with path, sinks, and vulnerabilities.
//	error - Non-nil if source not found or operation canceled.
//
// Errors:
//
//	safety.ErrSymbolNotFound - Source symbol not found.
//	safety.ErrGraphNotReady - Graph is not frozen.
//	safety.ErrContextCanceled - Context was canceled.
//
// Performance:
//
//	Target latency: < 200ms for single source, max 10 hops.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (t *InputTracerImpl) TraceUserInput(
	ctx context.Context,
	sourceID string,
	opts ...safety.TraceOption,
) (*safety.InputTrace, error) {
	start := time.Now()

	if ctx == nil {
		return nil, safety.ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, safety.ErrContextCanceled
	}

	if !t.graph.IsFrozen() {
		return nil, safety.ErrGraphNotReady
	}

	// Apply options
	config := safety.DefaultTraceConfig()
	config.ApplyOptions(opts...)

	// Set up context with timeout
	if config.Limits.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Limits.Timeout)
		defer cancel()
	}

	// Verify source node exists and is a source
	sourceNode, exists := t.graph.GetNode(sourceID)
	if !exists {
		return nil, safety.ErrSymbolNotFound
	}

	// Build trace result
	result := &safety.InputTrace{
		Path:            make([]safety.TraceStep, 0),
		Sinks:           make([]safety.Sink, 0),
		Sanitizers:      make([]safety.Sanitizer, 0),
		Vulnerabilities: make([]safety.Vulnerability, 0),
		Limitations: []string{
			"Function-level precision only; does not track variable assignments",
			"May miss flows through interface calls or dynamic dispatch",
			"Does not track data through closures or callbacks",
		},
		Confidence: 1.0,
	}

	// Classify the source
	if sourceNode.Symbol != nil {
		sourcePat, isSource := t.sources.MatchSource(sourceNode.Symbol)
		if isSource {
			result.Source = safety.InputSource{
				ID:          sourceNode.ID,
				Name:        sourceNode.Symbol.Name,
				Category:    string(sourcePat.Category),
				Location:    fmt.Sprintf("%s:%d", sourceNode.Symbol.FilePath, sourceNode.Symbol.StartLine),
				Description: sourcePat.Description,
			}
		} else {
			// If not a recognized source, still trace from it
			result.Source = safety.InputSource{
				ID:       sourceNode.ID,
				Name:     sourceNode.Symbol.Name,
				Category: "unknown",
				Location: fmt.Sprintf("%s:%d", sourceNode.Symbol.FilePath, sourceNode.Symbol.StartLine),
			}
		}
	}

	// BFS traversal with taint tracking
	visited := make(map[string]bool)
	type queueItem struct {
		nodeID    string
		depth     int
		taint     safety.DataTaint
		taintedBy string
	}
	queue := []queueItem{{sourceID, 0, safety.TaintUntrusted, sourceID}}
	visited[sourceID] = true
	nodesVisited := 0
	truncated := false

	// Track which sinks are vulnerable (reached by untrusted data unsanitized)
	vulnerableSinks := make(map[string]*safety.Vulnerability)

	for len(queue) > 0 {
		// Check context periodically
		if nodesVisited%100 == 0 {
			if err := ctx.Err(); err != nil {
				result.Limitations = append(result.Limitations, "Traversal stopped due to timeout or cancellation")
				result.Duration = time.Since(start)
				return result, nil // Return partial results
			}
		}

		// Check node limit
		if nodesVisited >= config.MaxNodes {
			truncated = true
			break
		}

		item := queue[0]
		queue = queue[1:]
		nodesVisited++

		// Check depth limit
		if item.depth >= config.MaxDepth {
			continue
		}

		node, exists := t.graph.GetNode(item.nodeID)
		if !exists {
			continue
		}

		// Add to path
		if node.Symbol != nil {
			result.Path = append(result.Path, safety.TraceStep{
				SymbolID:  item.nodeID,
				Name:      node.Symbol.Name,
				Location:  fmt.Sprintf("%s:%d", node.Symbol.FilePath, node.Symbol.StartLine),
				Taint:     item.taint,
				TaintedBy: item.taintedBy,
			})
		}

		// Follow outgoing CALLS edges
		for _, edge := range node.Outgoing {
			if edge.Type != graph.EdgeTypeCalls {
				continue
			}

			if visited[edge.ToID] {
				continue
			}
			visited[edge.ToID] = true

			targetNode, exists := t.graph.GetNode(edge.ToID)
			if !exists {
				continue
			}

			// Determine taint state for target
			targetTaint := item.taint
			taintedBy := item.taintedBy

			// Check if target is a sanitizer
			if targetNode.Symbol != nil {
				if sanitizerPat, isSanitizer := t.sanitizers.MatchSanitizer(targetNode.Symbol); isSanitizer {
					result.Sanitizers = append(result.Sanitizers, safety.Sanitizer{
						ID:           targetNode.ID,
						Name:         targetNode.Symbol.Name,
						Location:     fmt.Sprintf("%s:%d", targetNode.Symbol.FilePath, targetNode.Symbol.StartLine),
						MakesSafeFor: sanitizerPat.MakesSafeFor,
						IsComplete:   sanitizerPat.IsComplete(),
						Notes:        sanitizerPat.Description,
					})

					// If sanitizer is complete, mark data as clean
					if sanitizerPat.IsComplete() {
						targetTaint = safety.TaintClean
					} else {
						// Partial sanitizer - mark as mixed
						targetTaint = safety.TaintMixed
					}
				}
			}

			// Check if target is a dangerous sink
			if targetNode.Symbol != nil {
				if sinkPat, isSink := t.sinks.MatchSink(targetNode.Symbol); isSink && sinkPat.IsDangerous {
					// Filter by requested categories
					if len(config.SinkCategories) > 0 {
						found := false
						for _, cat := range config.SinkCategories {
							if string(sinkPat.Category) == cat {
								found = true
								break
							}
						}
						if !found {
							continue
						}
					}

					sink := safety.Sink{
						ID:          targetNode.ID,
						Name:        targetNode.Symbol.Name,
						Category:    string(sinkPat.Category),
						Location:    fmt.Sprintf("%s:%d", targetNode.Symbol.FilePath, targetNode.Symbol.StartLine),
						IsDangerous: true,
						CWE:         CWEMapping[string(sinkPat.Category)],
					}
					result.Sinks = append(result.Sinks, sink)

					// If data reaching sink is untrusted or mixed, it's a vulnerability
					if item.taint == safety.TaintUntrusted || item.taint == safety.TaintMixed {
						// Check if sanitizer for this category was applied
						sanitized := false
						for _, san := range result.Sanitizers {
							for _, cat := range san.MakesSafeFor {
								if cat == string(sinkPat.Category) && san.IsComplete {
									sanitized = true
									break
								}
							}
						}

						if !sanitized {
							vuln := t.createVulnerability(
								targetNode,
								sinkPat,
								item.taint == safety.TaintUntrusted,
							)
							vulnerableSinks[targetNode.ID] = vuln
						}
					}
				}
			}

			// Add to queue for further traversal
			queue = append(queue, queueItem{edge.ToID, item.depth + 1, targetTaint, taintedBy})
		}
	}

	// Add vulnerabilities to result
	for _, vuln := range vulnerableSinks {
		result.Vulnerabilities = append(result.Vulnerabilities, *vuln)
	}

	if truncated {
		result.Limitations = append(result.Limitations,
			fmt.Sprintf("Traversal truncated at %d nodes", config.MaxNodes))
		result.Confidence *= 0.8
	}

	result.Duration = time.Since(start)
	return result, nil
}

// createVulnerability creates a vulnerability from a dangerous sink.
func (t *InputTracerImpl) createVulnerability(
	node *graph.Node,
	sinkPat *explore.SinkPattern,
	dataFlowProven bool,
) *safety.Vulnerability {
	// Use global counter to ensure unique IDs across all tracer instances
	id := atomic.AddUint64(&globalVulnCounter, 1)

	cwe := CWEMapping[string(sinkPat.Category)]
	severity := safety.Severity(SeverityBySinkCategory[string(sinkPat.Category)])
	if severity == "" {
		severity = safety.SeverityHigh
	}

	confidence := sinkPat.Confidence
	if dataFlowProven {
		confidence = min(confidence+0.1, 0.99)
	}

	exploitability := safety.ExploitabilityUnknown
	if dataFlowProven {
		exploitability = safety.ExploitabilityYes
	}

	return &safety.Vulnerability{
		ID:             fmt.Sprintf("VULN-%d", id),
		Type:           string(sinkPat.Category),
		CWE:            cwe,
		Severity:       severity,
		Confidence:     confidence,
		Exploitability: exploitability,
		Location:       fmt.Sprintf("%s:%d", node.Symbol.FilePath, node.Symbol.StartLine),
		Line:           node.Symbol.StartLine,
		Description:    fmt.Sprintf("Untrusted input reaches %s sink without sanitization", sinkPat.Category),
		Remediation:    getRemediation(string(sinkPat.Category)),
		DataFlowProven: dataFlowProven,
	}
}

// getRemediation returns remediation advice for a vulnerability category.
func getRemediation(category string) string {
	switch category {
	case "sql":
		return "Use parameterized queries or prepared statements instead of string concatenation"
	case "command":
		return "Avoid shell=True; use subprocess with argument list; validate and sanitize all inputs"
	case "xss":
		return "Use context-aware output encoding; prefer template auto-escaping; sanitize HTML with allowlist"
	case "path":
		return "Use filepath.Base for filenames; validate paths stay within base directory; use SecureJoin"
	case "ssrf":
		return "Validate URLs against allowlist of permitted hosts; reject internal IPs and localhost"
	case "deserialize":
		return "Avoid deserializing untrusted data; use JSON instead of pickle/gob; validate schema"
	case "log":
		return "Sanitize user input before logging; use structured logging with proper field types"
	default:
		return "Validate and sanitize all untrusted input before use"
	}
}

// min returns the smaller of two float64 values.
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
