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

	"go.opentelemetry.io/otel/trace"
)

// =============================================================================
// HLD-CRS Integration Helpers
// =============================================================================
//
// This file contains utility functions for HLD-CRS integration.
//
// IMPORTANT: CRS integration happens at the GRAPHANALYTICS LEVEL, not here.
// See docs/opensource/trace/graph_hld_crs_integration.md for architecture.
//
// HLD query functions (LCA, Distance, DecomposePath) are PURE FUNCTIONS with
// no CRS dependencies. GraphAnalytics provides CRS-aware wrapper methods:
// - LCAWithCRS()
// - DistanceWithCRS()
// - DecomposePathWithCRS()
// - BatchLCAWithCRS()
//
// =============================================================================

// GetCorrelationID extracts correlation ID from OpenTelemetry trace context.
//
// Description:
//
//	Returns the OTel trace ID as a correlation ID for linking logs and traces.
//	This provides automatic correlation without custom context keys.
//
// Inputs:
//   - ctx: Context with optional OTel span. Can be nil (returns empty string).
//
// Outputs:
//   - string: Trace ID in hex format, or empty string if no valid span context.
//
// Thread Safety: Safe for concurrent use.
//
// Example:
//
//	correlationID := GetCorrelationID(ctx)
//	logger.Info("query complete", slog.String("correlation_id", correlationID))
func GetCorrelationID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}

	// Extract trace ID from OTel span context
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		return span.SpanContext().TraceID().String()
	}

	return ""
}
