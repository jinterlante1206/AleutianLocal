// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package graph

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Package-level tracer and meter for graph operations.
var (
	tracer = otel.Tracer("aleutian.graph")
	meter  = otel.Meter("aleutian.graph")
)

// Metrics for graph building operations.
var (
	buildLatency            metric.Float64Histogram
	buildTotal              metric.Int64Counter
	nodesCreated            metric.Int64Histogram
	edgesCreated            metric.Int64Histogram
	queryLatency            metric.Float64Histogram
	interfaceEdgesCreated   metric.Int64Counter // GR-40: Interface implementation edges
	interfaceMatchesChecked metric.Int64Counter // GR-40: Type-interface pairs checked
	callEdgesResolved       metric.Int64Counter // GR-41: Call edges resolved to symbols
	callEdgesUnresolved     metric.Int64Counter // GR-41: Call edges to placeholders
	callSitesExtracted      metric.Int64Counter // GR-41: Total call sites extracted
	importEdgesCreated      metric.Int64Counter // GR-41c: Import edges created
	importEdgesFailed       metric.Int64Counter // GR-41c: Import edges failed

	metricsOnce sync.Once
	metricsErr  error
)

// initMetrics initializes the metrics. Safe to call multiple times.
func initMetrics() error {
	metricsOnce.Do(func() {
		var err error

		buildLatency, err = meter.Float64Histogram(
			"graph_build_duration_seconds",
			metric.WithDescription("Duration of graph build operations"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		buildTotal, err = meter.Int64Counter(
			"graph_build_total",
			metric.WithDescription("Total number of graph build operations"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		nodesCreated, err = meter.Int64Histogram(
			"graph_nodes_created",
			metric.WithDescription("Number of nodes created per build"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		edgesCreated, err = meter.Int64Histogram(
			"graph_edges_created",
			metric.WithDescription("Number of edges created per build"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		queryLatency, err = meter.Float64Histogram(
			"graph_query_duration_seconds",
			metric.WithDescription("Duration of graph query operations"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// GR-40: Interface implementation detection metrics
		interfaceEdgesCreated, err = meter.Int64Counter(
			"graph_interface_edges_created_total",
			metric.WithDescription("Total EdgeTypeImplements edges created via method-set matching"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		interfaceMatchesChecked, err = meter.Int64Counter(
			"graph_interface_matches_checked_total",
			metric.WithDescription("Total type-interface pairs checked for implementation"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// GR-41: Call edge extraction metrics
		callEdgesResolved, err = meter.Int64Counter(
			"graph_call_edges_resolved_total",
			metric.WithDescription("Total EdgeTypeCalls edges resolved to existing symbols"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		callEdgesUnresolved, err = meter.Int64Counter(
			"graph_call_edges_unresolved_total",
			metric.WithDescription("Total EdgeTypeCalls edges to placeholder nodes"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		callSitesExtracted, err = meter.Int64Counter(
			"graph_call_sites_extracted_total",
			metric.WithDescription("Total call sites extracted from function bodies"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		// GR-41c: Import edge metrics
		importEdgesCreated, err = meter.Int64Counter(
			"graph_import_edges_created_total",
			metric.WithDescription("Total EdgeTypeImports edges created"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		importEdgesFailed, err = meter.Int64Counter(
			"graph_import_edges_failed_total",
			metric.WithDescription("Total EdgeTypeImports edges that failed to create"),
		)
		if err != nil {
			metricsErr = err
			return
		}
	})
	return metricsErr
}

// recordBuildMetrics records metrics for a build operation.
func recordBuildMetrics(ctx context.Context, duration time.Duration, nodeCount, edgeCount int, success bool) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(attribute.Bool("success", success))

	buildLatency.Record(ctx, duration.Seconds(), attrs)
	buildTotal.Add(ctx, 1, attrs)

	if success {
		nodesCreated.Record(ctx, int64(nodeCount))
		edgesCreated.Record(ctx, int64(edgeCount))
	}
}

// recordQueryMetrics records metrics for a query operation.
func recordQueryMetrics(ctx context.Context, queryType string, duration time.Duration, resultCount int) {
	if err := initMetrics(); err != nil {
		return
	}

	queryLatency.Record(ctx, duration.Seconds(),
		metric.WithAttributes(attribute.String("query_type", queryType)),
	)
}

// startBuildSpan creates a span for a build operation.
func startBuildSpan(ctx context.Context, fileCount int) (context.Context, trace.Span) {
	return tracer.Start(ctx, "GraphBuilder.Build",
		trace.WithAttributes(
			attribute.Int("graph.file_count", fileCount),
		),
	)
}

// setBuildSpanResult sets the result attributes on a build span.
func setBuildSpanResult(span trace.Span, nodeCount, edgeCount int, incomplete bool) {
	span.SetAttributes(
		attribute.Int("graph.node_count", nodeCount),
		attribute.Int("graph.edge_count", edgeCount),
		attribute.Bool("graph.incomplete", incomplete),
	)
}

// startQuerySpan creates a span for a query operation.
func startQuerySpan(ctx context.Context, queryType, symbolID string) (context.Context, trace.Span) {
	return tracer.Start(ctx, "Graph."+queryType,
		trace.WithAttributes(
			attribute.String("graph.query_type", queryType),
			attribute.String("graph.symbol_id", symbolID),
		),
	)
}

// recordInterfaceDetectionMetrics records metrics for GR-40/GR-40a interface detection.
//
// Description:
//
//	Records the number of EdgeTypeImplements edges created via method-set matching
//	and the number of type-interface pairs checked. Supports both Go interfaces (GR-40)
//	and Python Protocols (GR-40a) via the language parameter.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - language: Language of the interfaces ("go" or "python").
//   - edgesCreated: Number of EdgeTypeImplements edges created.
//   - matchesChecked: Number of type-interface pairs checked.
//
// Thread Safety: Safe for concurrent use.
func recordInterfaceDetectionMetrics(ctx context.Context, edgesCreated, matchesChecked int) {
	if err := initMetrics(); err != nil {
		return
	}

	interfaceEdgesCreated.Add(ctx, int64(edgesCreated))
	interfaceMatchesChecked.Add(ctx, int64(matchesChecked))
}

// recordInterfaceDetectionMetricsWithLanguage records metrics with language dimension.
//
// Description:
//
//	GR-40a: Same as recordInterfaceDetectionMetrics but includes language label
//	to distinguish Go interface detection from Python Protocol detection.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - language: Language of the interfaces ("go" or "python").
//   - edgesCreated: Number of EdgeTypeImplements edges created.
//   - matchesChecked: Number of type-interface pairs checked.
//
// Thread Safety: Safe for concurrent use.
func recordInterfaceDetectionMetricsWithLanguage(ctx context.Context, language string, edgesCreated, matchesChecked int) {
	if err := initMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(attribute.String("language", language))
	interfaceEdgesCreated.Add(ctx, int64(edgesCreated), attrs)
	interfaceMatchesChecked.Add(ctx, int64(matchesChecked), attrs)
}

// recordCallEdgeMetrics records metrics for GR-41 call edge extraction.
//
// Description:
//
//	Records the number of call edges resolved to existing symbols,
//	call edges to placeholder nodes, and total call sites extracted.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - resolved: Number of call edges resolved to existing symbols.
//   - unresolved: Number of call edges to placeholder nodes.
//   - extracted: Total call sites extracted from function bodies.
//
// Thread Safety: Safe for concurrent use.
func recordCallEdgeMetrics(ctx context.Context, resolved, unresolved, extracted int) {
	if err := initMetrics(); err != nil {
		return
	}

	callEdgesResolved.Add(ctx, int64(resolved))
	callEdgesUnresolved.Add(ctx, int64(unresolved))
	callSitesExtracted.Add(ctx, int64(extracted))
}

// recordImportEdgeMetrics records metrics for GR-41c import edge extraction.
//
// Description:
//
//	Records the number of import edges created and failed.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - created: Number of import edges successfully created.
//   - failed: Number of import edges that failed to create.
//
// Thread Safety: Safe for concurrent use.
func recordImportEdgeMetrics(ctx context.Context, created, failed int) {
	if err := initMetrics(); err != nil {
		return
	}

	importEdgesCreated.Add(ctx, int64(created))
	importEdgesFailed.Add(ctx, int64(failed))
}

// HLD-specific metrics (GR-19a)
var (
	hldConstructionDuration metric.Float64Histogram
	hldNodeCount            metric.Float64Histogram
	hldValidationErrors     metric.Int64Counter
	hldHeavyPathCount       metric.Float64Histogram
	hldLightEdgeCount       metric.Float64Histogram
	hldMaxDepth             metric.Float64Histogram

	hldMetricsOnce sync.Once
	hldMetricsErr  error
)

// initHLDMetrics initializes HLD-specific metrics. Safe to call multiple times.
func initHLDMetrics() error {
	hldMetricsOnce.Do(func() {
		var err error

		hldConstructionDuration, err = meter.Float64Histogram(
			"trace_hld_construction_duration_seconds",
			metric.WithDescription("Time to construct HLD structure"),
			metric.WithUnit("s"),
		)
		if err != nil {
			hldMetricsErr = err
			return
		}

		hldNodeCount, err = meter.Float64Histogram(
			"trace_hld_node_count",
			metric.WithDescription("Number of nodes in HLD tree"),
		)
		if err != nil {
			hldMetricsErr = err
			return
		}

		hldValidationErrors, err = meter.Int64Counter(
			"trace_hld_validation_errors_total",
			metric.WithDescription("Total HLD validation errors"),
		)
		if err != nil {
			hldMetricsErr = err
			return
		}

		hldHeavyPathCount, err = meter.Float64Histogram(
			"trace_hld_heavy_path_count",
			metric.WithDescription("Number of heavy paths in HLD decomposition"),
		)
		if err != nil {
			hldMetricsErr = err
			return
		}

		hldLightEdgeCount, err = meter.Float64Histogram(
			"trace_hld_light_edge_count",
			metric.WithDescription("Number of light edges in HLD decomposition"),
		)
		if err != nil {
			hldMetricsErr = err
			return
		}

		hldMaxDepth, err = meter.Float64Histogram(
			"trace_hld_max_depth",
			metric.WithDescription("Maximum depth of tree in HLD decomposition"),
		)
		if err != nil {
			hldMetricsErr = err
			return
		}
	})
	return hldMetricsErr
}

// recordHLDMetrics records metrics for HLD construction.
//
// Description:
//
//	Records HLD construction duration, node count, heavy path count,
//	light edge count, and max depth. Called after successful HLD construction.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - stats: HLD statistics from Stats() method.
//
// Thread Safety: Safe for concurrent use.
func recordHLDMetrics(ctx context.Context, stats HLDStats) {
	if err := initHLDMetrics(); err != nil {
		return
	}

	hldConstructionDuration.Record(ctx, stats.ConstructionTime.Seconds())
	hldNodeCount.Record(ctx, float64(stats.NodeCount))
	hldHeavyPathCount.Record(ctx, float64(stats.HeavyPathCount))
	hldLightEdgeCount.Record(ctx, float64(stats.LightEdgeCount))
	hldMaxDepth.Record(ctx, float64(stats.MaxPathLength))
}

// recordHLDValidationError increments the HLD validation error counter.
func recordHLDValidationError(ctx context.Context) {
	if err := initHLDMetrics(); err != nil {
		return
	}
	hldValidationErrors.Add(ctx, 1)
}

// Segment Tree metrics (GR-19b)
var (
	segTreeBuildDuration       metric.Float64Histogram
	segTreeQueryDuration       metric.Float64Histogram
	segTreeUpdateDuration      metric.Float64Histogram
	segTreeRangeUpdateDuration metric.Float64Histogram
	segTreeSize                metric.Float64Histogram
	segTreeValidationErrors    metric.Int64Counter
	segTreeLazyPushCount       metric.Int64Counter
	segTreeQueryNodesVisited   metric.Float64Histogram

	segTreeMetricsOnce sync.Once
	segTreeMetricsErr  error
)

// initSegTreeMetrics initializes segment tree metrics. Safe to call multiple times.
func initSegTreeMetrics() error {
	segTreeMetricsOnce.Do(func() {
		var err error

		segTreeBuildDuration, err = meter.Float64Histogram(
			"trace_segment_tree_build_duration_seconds",
			metric.WithDescription("Time to build segment tree"),
			metric.WithUnit("s"),
		)
		if err != nil {
			segTreeMetricsErr = err
			return
		}

		segTreeQueryDuration, err = meter.Float64Histogram(
			"trace_segment_tree_query_duration_seconds",
			metric.WithDescription("Time to query segment tree"),
			metric.WithUnit("s"),
		)
		if err != nil {
			segTreeMetricsErr = err
			return
		}

		segTreeUpdateDuration, err = meter.Float64Histogram(
			"trace_segment_tree_update_duration_seconds",
			metric.WithDescription("Time to update single element in segment tree"),
			metric.WithUnit("s"),
		)
		if err != nil {
			segTreeMetricsErr = err
			return
		}

		segTreeRangeUpdateDuration, err = meter.Float64Histogram(
			"trace_segment_tree_range_update_duration_seconds",
			metric.WithDescription("Time to perform range update in segment tree"),
			metric.WithUnit("s"),
		)
		if err != nil {
			segTreeMetricsErr = err
			return
		}

		segTreeSize, err = meter.Float64Histogram(
			"trace_segment_tree_size",
			metric.WithDescription("Segment tree array size"),
		)
		if err != nil {
			segTreeMetricsErr = err
			return
		}

		segTreeValidationErrors, err = meter.Int64Counter(
			"trace_segment_tree_validation_errors_total",
			metric.WithDescription("Total segment tree validation errors"),
		)
		if err != nil {
			segTreeMetricsErr = err
			return
		}

		segTreeLazyPushCount, err = meter.Int64Counter(
			"trace_segment_tree_lazy_push_total",
			metric.WithDescription("Total lazy propagation push operations"),
		)
		if err != nil {
			segTreeMetricsErr = err
			return
		}

		segTreeQueryNodesVisited, err = meter.Float64Histogram(
			"trace_segment_tree_query_nodes_visited",
			metric.WithDescription("Number of tree nodes visited during query"),
		)
		if err != nil {
			segTreeMetricsErr = err
			return
		}
	})
	return segTreeMetricsErr
}

// recordSegTreeMetrics records metrics for segment tree construction.
//
// Description:
//
//	Records segment tree build duration, size, and other statistics.
//	Called after successful segment tree construction.
//
// Inputs:
//   - ctx: Context for metric recording.
//   - stats: Segment tree statistics from Stats() method.
//
// Thread Safety: Safe for concurrent use.
func recordSegTreeMetrics(ctx context.Context, stats SegmentTreeStats) {
	if err := initSegTreeMetrics(); err != nil {
		return
	}

	attrs := metric.WithAttributes(
		attribute.String("agg_func", stats.AggFunc.String()),
	)

	segTreeBuildDuration.Record(ctx, stats.BuildTime.Seconds(), attrs)
	segTreeSize.Record(ctx, float64(stats.Size), attrs)
}

// recordSegTreeValidationError increments the segment tree validation error counter.
func recordSegTreeValidationError(ctx context.Context) {
	if err := initSegTreeMetrics(); err != nil {
		return
	}
	segTreeValidationErrors.Add(ctx, 1)
}

// recordSegTreeQueryDuration records segment tree query duration.
func recordSegTreeQueryDuration(ctx context.Context, duration time.Duration, aggFunc AggregateFunc) {
	if err := initSegTreeMetrics(); err != nil {
		return
	}
	segTreeQueryDuration.Record(ctx, duration.Seconds(),
		metric.WithAttributes(attribute.String("agg_func", aggFunc.String())))
}

// recordSegTreeUpdateDuration records segment tree update duration.
func recordSegTreeUpdateDuration(ctx context.Context, duration time.Duration, aggFunc AggregateFunc) {
	if err := initSegTreeMetrics(); err != nil {
		return
	}
	segTreeUpdateDuration.Record(ctx, duration.Seconds(),
		metric.WithAttributes(attribute.String("agg_func", aggFunc.String())))
}

// recordSegTreeRangeUpdateDuration records segment tree range update duration.
func recordSegTreeRangeUpdateDuration(ctx context.Context, duration time.Duration, aggFunc AggregateFunc) {
	if err := initSegTreeMetrics(); err != nil {
		return
	}
	segTreeRangeUpdateDuration.Record(ctx, duration.Seconds(),
		metric.WithAttributes(attribute.String("agg_func", aggFunc.String())))
}
