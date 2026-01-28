// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package history

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// TrendingAnalyzer analyzes blast radius trends over time.
//
// # Description
//
// Calculates growth rates, detects rapid increases, and identifies
// symbols with concerning trends.
//
// # Thread Safety
//
// Safe for concurrent use.
type TrendingAnalyzer struct {
	store *HistoryStore
}

// SymbolTrend represents the trend for a symbol.
type SymbolTrend struct {
	// SymbolID is the symbol identifier.
	SymbolID string `json:"symbol_id"`

	// CurrentCallers is the most recent caller count.
	CurrentCallers int `json:"current_callers"`

	// PreviousCallers is the caller count from the comparison period.
	PreviousCallers int `json:"previous_callers"`

	// GrowthRate is the percentage change.
	GrowthRate float64 `json:"growth_rate"`

	// Direction indicates trend direction.
	Direction TrendDirection `json:"direction"`

	// IsRapidGrowth indicates if growth exceeds threshold.
	IsRapidGrowth bool `json:"is_rapid_growth"`

	// DataPoints is the number of data points analyzed.
	DataPoints int `json:"data_points"`

	// FirstSeen is when this symbol was first tracked.
	FirstSeen time.Time `json:"first_seen"`

	// LastSeen is the most recent data point.
	LastSeen time.Time `json:"last_seen"`
}

// TrendDirection indicates the direction of a trend.
type TrendDirection string

const (
	TrendUp     TrendDirection = "UP"
	TrendDown   TrendDirection = "DOWN"
	TrendStable TrendDirection = "STABLE"
)

// TrendAlert represents an alert for concerning trends.
type TrendAlert struct {
	// SymbolID is the symbol with the alert.
	SymbolID string `json:"symbol_id"`

	// AlertType is the type of alert.
	AlertType TrendAlertType `json:"alert_type"`

	// Message describes the alert.
	Message string `json:"message"`

	// Severity is the alert severity.
	Severity string `json:"severity"`

	// Trend is the underlying trend data.
	Trend SymbolTrend `json:"trend"`

	// Timestamp is when the alert was generated.
	Timestamp time.Time `json:"timestamp"`
}

// TrendAlertType represents the type of trend alert.
type TrendAlertType string

const (
	AlertRapidGrowth     TrendAlertType = "RAPID_GROWTH"
	AlertHighCallerCount TrendAlertType = "HIGH_CALLER_COUNT"
	AlertNewHotPath      TrendAlertType = "NEW_HOT_PATH"
)

// TrendingOptions configures trend analysis.
type TrendingOptions struct {
	// ComparisonPeriod is how far back to look for comparison.
	// Default: 7 days
	ComparisonPeriod time.Duration

	// RapidGrowthThreshold is the percentage that triggers alerts.
	// Default: 50%
	RapidGrowthThreshold float64

	// HighCallerThreshold triggers alerts for high caller counts.
	// Default: 50
	HighCallerThreshold int

	// MinDataPoints is the minimum data points for trend calculation.
	// Default: 3
	MinDataPoints int
}

// DefaultTrendingOptions returns sensible defaults.
func DefaultTrendingOptions() TrendingOptions {
	return TrendingOptions{
		ComparisonPeriod:     7 * 24 * time.Hour,
		RapidGrowthThreshold: 50.0,
		HighCallerThreshold:  50,
		MinDataPoints:        3,
	}
}

// NewTrendingAnalyzer creates a new trending analyzer.
func NewTrendingAnalyzer(store *HistoryStore) *TrendingAnalyzer {
	return &TrendingAnalyzer{
		store: store,
	}
}

// AnalyzeSymbol calculates the trend for a specific symbol.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - symbolID: The symbol to analyze.
//   - opts: Optional configuration (nil uses defaults).
//
// # Outputs
//
//   - *SymbolTrend: The calculated trend.
//   - error: Non-nil on failure.
func (t *TrendingAnalyzer) AnalyzeSymbol(ctx context.Context, symbolID string, opts *TrendingOptions) (*SymbolTrend, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}

	if opts == nil {
		defaults := DefaultTrendingOptions()
		opts = &defaults
	}

	// Get events for this symbol
	now := time.Now()
	events, err := t.store.Query(symbolID, now.Add(-opts.ComparisonPeriod*2), now)
	if err != nil {
		return nil, err
	}

	if len(events) < opts.MinDataPoints {
		return &SymbolTrend{
			SymbolID:   symbolID,
			DataPoints: len(events),
			Direction:  TrendStable,
		}, nil
	}

	// Sort by timestamp
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	trend := &SymbolTrend{
		SymbolID:   symbolID,
		DataPoints: len(events),
		FirstSeen:  events[0].Timestamp,
		LastSeen:   events[len(events)-1].Timestamp,
	}

	// Get current (most recent) caller count
	trend.CurrentCallers = events[len(events)-1].DirectCallers

	// Find comparison point (from comparison period ago)
	comparisonCutoff := now.Add(-opts.ComparisonPeriod)
	for _, e := range events {
		if e.Timestamp.Before(comparisonCutoff) {
			trend.PreviousCallers = e.DirectCallers
		} else {
			break
		}
	}

	// If no comparison point, use first event
	if trend.PreviousCallers == 0 && len(events) > 1 {
		trend.PreviousCallers = events[0].DirectCallers
	}

	// Calculate growth rate
	if trend.PreviousCallers > 0 {
		trend.GrowthRate = float64(trend.CurrentCallers-trend.PreviousCallers) / float64(trend.PreviousCallers) * 100
	} else if trend.CurrentCallers > 0 {
		trend.GrowthRate = 100.0 // New symbol, 100% growth
	}

	// Determine direction
	if trend.GrowthRate > 5 {
		trend.Direction = TrendUp
	} else if trend.GrowthRate < -5 {
		trend.Direction = TrendDown
	} else {
		trend.Direction = TrendStable
	}

	// Check for rapid growth
	trend.IsRapidGrowth = trend.GrowthRate > opts.RapidGrowthThreshold

	return trend, nil
}

// GetTrendingSymbols returns symbols with significant trends.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - limit: Maximum number of symbols to return.
//   - opts: Optional configuration.
//
// # Outputs
//
//   - []SymbolTrend: Symbols sorted by growth rate (descending).
//   - error: Non-nil on failure.
func (t *TrendingAnalyzer) GetTrendingSymbols(ctx context.Context, limit int, opts *TrendingOptions) ([]SymbolTrend, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}

	if opts == nil {
		defaults := DefaultTrendingOptions()
		opts = &defaults
	}

	// Get all recent events
	events, err := t.store.QueryRecent(10000) // Reasonable upper bound
	if err != nil {
		return nil, err
	}

	// Group by symbol
	symbolEvents := make(map[string][]HistoryEvent)
	for _, e := range events {
		symbolEvents[e.SymbolID] = append(symbolEvents[e.SymbolID], e)
	}

	// Calculate trends for each symbol
	var trends []SymbolTrend
	for symbolID := range symbolEvents {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		trend, err := t.AnalyzeSymbol(ctx, symbolID, opts)
		if err != nil || trend.DataPoints < opts.MinDataPoints {
			continue
		}

		trends = append(trends, *trend)
	}

	// Sort by growth rate
	sort.Slice(trends, func(i, j int) bool {
		return trends[i].GrowthRate > trends[j].GrowthRate
	})

	// Limit results
	if len(trends) > limit {
		trends = trends[:limit]
	}

	return trends, nil
}

// GenerateAlerts generates alerts for concerning trends.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - opts: Optional configuration.
//
// # Outputs
//
//   - []TrendAlert: Generated alerts.
//   - error: Non-nil on failure.
func (t *TrendingAnalyzer) GenerateAlerts(ctx context.Context, opts *TrendingOptions) ([]TrendAlert, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}

	if opts == nil {
		defaults := DefaultTrendingOptions()
		opts = &defaults
	}

	trends, err := t.GetTrendingSymbols(ctx, 1000, opts)
	if err != nil {
		return nil, err
	}

	var alerts []TrendAlert
	now := time.Now()

	for _, trend := range trends {
		// Rapid growth alert
		if trend.IsRapidGrowth {
			alerts = append(alerts, TrendAlert{
				SymbolID:  trend.SymbolID,
				AlertType: AlertRapidGrowth,
				Message: fmt.Sprintf("Symbol %s has grown %.1f%% in the last %s",
					trend.SymbolID, trend.GrowthRate, opts.ComparisonPeriod),
				Severity:  "HIGH",
				Trend:     trend,
				Timestamp: now,
			})
		}

		// High caller count alert
		if trend.CurrentCallers >= opts.HighCallerThreshold {
			alerts = append(alerts, TrendAlert{
				SymbolID:  trend.SymbolID,
				AlertType: AlertHighCallerCount,
				Message: fmt.Sprintf("Symbol %s has %d callers (threshold: %d)",
					trend.SymbolID, trend.CurrentCallers, opts.HighCallerThreshold),
				Severity:  "MEDIUM",
				Trend:     trend,
				Timestamp: now,
			})
		}

		// New hot path (new symbol with significant callers)
		isNew := now.Sub(trend.FirstSeen) < opts.ComparisonPeriod
		if isNew && trend.CurrentCallers >= 10 {
			alerts = append(alerts, TrendAlert{
				SymbolID:  trend.SymbolID,
				AlertType: AlertNewHotPath,
				Message: fmt.Sprintf("New symbol %s already has %d callers",
					trend.SymbolID, trend.CurrentCallers),
				Severity:  "LOW",
				Trend:     trend,
				Timestamp: now,
			})
		}
	}

	// Sort alerts by severity
	severityOrder := map[string]int{"HIGH": 0, "MEDIUM": 1, "LOW": 2}
	sort.Slice(alerts, func(i, j int) bool {
		return severityOrder[alerts[i].Severity] < severityOrder[alerts[j].Severity]
	})

	return alerts, nil
}

// GetSymbolTimeline returns caller counts over time for a symbol.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - symbolID: The symbol to get timeline for.
//   - since: Start of the timeline.
//   - bucketSize: Size of each time bucket.
//
// # Outputs
//
//   - []TimelinePoint: Points on the timeline.
//   - error: Non-nil on failure.
func (t *TrendingAnalyzer) GetSymbolTimeline(ctx context.Context, symbolID string, since time.Time, bucketSize time.Duration) ([]TimelinePoint, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}

	events, err := t.store.Query(symbolID, since, time.Now())
	if err != nil {
		return nil, err
	}

	if len(events) == 0 {
		return nil, nil
	}

	// Sort by timestamp
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	// Bucket events
	buckets := make(map[int64][]HistoryEvent)
	for _, e := range events {
		bucket := e.Timestamp.UnixMilli() / bucketSize.Milliseconds() * bucketSize.Milliseconds()
		buckets[bucket] = append(buckets[bucket], e)
	}

	// Create timeline points
	var timeline []TimelinePoint
	for bucket, events := range buckets {
		// Average caller count in bucket
		var total int
		for _, e := range events {
			total += e.DirectCallers
		}
		avg := total / len(events)

		timeline = append(timeline, TimelinePoint{
			Timestamp:  time.UnixMilli(bucket),
			Callers:    avg,
			DataPoints: len(events),
		})
	}

	// Sort by timestamp
	sort.Slice(timeline, func(i, j int) bool {
		return timeline[i].Timestamp.Before(timeline[j].Timestamp)
	})

	return timeline, nil
}

// TimelinePoint represents a point on the timeline.
type TimelinePoint struct {
	// Timestamp is the bucket start time.
	Timestamp time.Time `json:"timestamp"`

	// Callers is the average caller count.
	Callers int `json:"callers"`

	// DataPoints is the number of events in this bucket.
	DataPoints int `json:"data_points"`
}

// CompareSymbols compares trends between two symbols.
func (t *TrendingAnalyzer) CompareSymbols(ctx context.Context, symbolA, symbolB string, opts *TrendingOptions) (*SymbolComparison, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}

	trendA, err := t.AnalyzeSymbol(ctx, symbolA, opts)
	if err != nil {
		return nil, fmt.Errorf("analyze %s: %w", symbolA, err)
	}

	trendB, err := t.AnalyzeSymbol(ctx, symbolB, opts)
	if err != nil {
		return nil, fmt.Errorf("analyze %s: %w", symbolB, err)
	}

	return &SymbolComparison{
		SymbolA:          trendA,
		SymbolB:          trendB,
		CallerDifference: trendA.CurrentCallers - trendB.CurrentCallers,
		GrowthDifference: trendA.GrowthRate - trendB.GrowthRate,
	}, nil
}

// SymbolComparison compares two symbol trends.
type SymbolComparison struct {
	// SymbolA is the first symbol's trend.
	SymbolA *SymbolTrend `json:"symbol_a"`

	// SymbolB is the second symbol's trend.
	SymbolB *SymbolTrend `json:"symbol_b"`

	// CallerDifference is A's callers minus B's callers.
	CallerDifference int `json:"caller_difference"`

	// GrowthDifference is A's growth rate minus B's growth rate.
	GrowthDifference float64 `json:"growth_difference"`
}
