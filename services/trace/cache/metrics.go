// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package cache

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Package-level tracer and meter for cache operations.
var (
	tracer = otel.Tracer("aleutian.cache")
	meter  = otel.Meter("aleutian.cache")
)

// Metrics for cache operations.
var (
	cacheHits       metric.Int64Counter
	cacheMisses     metric.Int64Counter
	cacheEvictions  metric.Int64Counter
	cacheGetLatency metric.Float64Histogram
	cacheBuildTotal metric.Int64Counter

	metricsOnce sync.Once
	metricsErr  error
)

// initMetrics initializes the metrics. Safe to call multiple times.
func initMetrics() error {
	metricsOnce.Do(func() {
		var err error

		cacheHits, err = meter.Int64Counter(
			"cache_hits_total",
			metric.WithDescription("Total number of cache hits"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		cacheMisses, err = meter.Int64Counter(
			"cache_misses_total",
			metric.WithDescription("Total number of cache misses"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		cacheEvictions, err = meter.Int64Counter(
			"cache_evictions_total",
			metric.WithDescription("Total number of cache evictions"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		cacheGetLatency, err = meter.Float64Histogram(
			"cache_get_duration_seconds",
			metric.WithDescription("Duration of cache get operations"),
			metric.WithUnit("s"),
		)
		if err != nil {
			metricsErr = err
			return
		}

		cacheBuildTotal, err = meter.Int64Counter(
			"cache_build_total",
			metric.WithDescription("Total number of cache builds"),
		)
		if err != nil {
			metricsErr = err
			return
		}
	})
	return metricsErr
}

// recordCacheHit records a cache hit metric.
func recordCacheHit(ctx context.Context) {
	if err := initMetrics(); err != nil {
		return
	}
	cacheHits.Add(ctx, 1)
}

// recordCacheMiss records a cache miss metric.
func recordCacheMiss(ctx context.Context) {
	if err := initMetrics(); err != nil {
		return
	}
	cacheMisses.Add(ctx, 1)
}

// recordCacheEviction records a cache eviction metric.
func recordCacheEviction(ctx context.Context) {
	if err := initMetrics(); err != nil {
		return
	}
	cacheEvictions.Add(ctx, 1)
}

// recordCacheGetLatency records the latency of a cache get operation.
func recordCacheGetLatency(ctx context.Context, duration time.Duration, hit bool) {
	if err := initMetrics(); err != nil {
		return
	}
	cacheGetLatency.Record(ctx, duration.Seconds(),
		metric.WithAttributes(attribute.Bool("hit", hit)),
	)
}

// startCacheSpan creates a span for a cache operation.
func startCacheSpan(ctx context.Context, operation, projectRoot string) (context.Context, trace.Span) {
	return tracer.Start(ctx, "GraphCache."+operation,
		trace.WithAttributes(
			attribute.String("cache.operation", operation),
			attribute.String("cache.project_root", projectRoot),
		),
	)
}

// setCacheSpanResult sets the result attributes on a cache span.
func setCacheSpanResult(span trace.Span, hit bool) {
	span.SetAttributes(attribute.Bool("cache.hit", hit))
}
