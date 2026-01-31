// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package patterns

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

// PatternDetector finds design patterns in code.
//
// # Description
//
// PatternDetector scans code using configurable matchers to find design
// patterns. It supports both structural detection (finding code that looks
// like a pattern) and idiomatic validation (checking if it's implemented
// correctly).
//
// # Thread Safety
//
// This type is safe for concurrent use.
type PatternDetector struct {
	graph    *graph.Graph
	index    *index.SymbolIndex
	matchers map[PatternType]*PatternMatcher
	mu       sync.RWMutex
}

// NewPatternDetector creates a new detector with default matchers.
//
// # Description
//
// Creates a detector that uses the provided graph and index with the
// standard set of pattern matchers.
//
// # Inputs
//
//   - g: Code graph. Must be frozen before DetectPatterns().
//   - idx: Symbol index for lookups.
//
// # Outputs
//
//   - *PatternDetector: Configured detector.
//
// # Example
//
//	detector := NewPatternDetector(graph, index)
//	patterns, err := detector.DetectPatterns(ctx, "pkg/service", nil)
func NewPatternDetector(g *graph.Graph, idx *index.SymbolIndex) *PatternDetector {
	return &PatternDetector{
		graph:    g,
		index:    idx,
		matchers: DefaultMatchers(),
	}
}

// RegisterMatcher adds a custom pattern matcher.
//
// # Description
//
// Registers a custom pattern matcher. If a matcher for the pattern type
// already exists, it is replaced.
//
// # Inputs
//
//   - matcher: The pattern matcher to register.
//
// # Example
//
//	detector.RegisterMatcher(&PatternMatcher{
//	    Name: "custom_pattern",
//	    StructuralCheck: func(...) []PatternCandidate { ... },
//	})
func (d *PatternDetector) RegisterMatcher(matcher *PatternMatcher) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.matchers[matcher.Name] = matcher
}

// DetectPatterns finds design patterns in the specified scope.
//
// # Description
//
// Scans the specified scope (package or file path prefix) for design
// patterns. Returns all detected patterns with confidence scores and
// idiomatic validation results.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - scope: Package or file path prefix to scan (empty = all).
//   - opts: Optional detection options.
//
// # Outputs
//
//   - []DetectedPattern: Found patterns with confidence and warnings.
//   - error: Non-nil on failure.
//
// # Example
//
//	// Detect all patterns in a package
//	patterns, err := detector.DetectPatterns(ctx, "pkg/auth", nil)
//
//	// Detect only factory patterns
//	patterns, err := detector.DetectPatterns(ctx, "pkg/auth", &DetectionOptions{
//	    Patterns: []PatternType{PatternFactory},
//	})
func (d *PatternDetector) DetectPatterns(
	ctx context.Context,
	scope string,
	opts *DetectionOptions,
) ([]DetectedPattern, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	ctx, span := startDetectSpan(ctx, scope)
	defer span.End()
	start := time.Now()

	if err := ctx.Err(); err != nil {
		setDetectSpanResult(span, 0, false)
		recordDetectMetrics(ctx, time.Since(start), 0, false)
		return nil, ErrContextCanceled
	}

	options := DetectionOptions{
		MinConfidence:       0.0,
		IncludeNonIdiomatic: true,
	}
	if opts != nil {
		options = *opts
	}

	d.mu.RLock()
	matchers := make([]*PatternMatcher, 0, len(d.matchers))
	for pt, m := range d.matchers {
		// Filter by requested patterns if specified
		if len(options.Patterns) > 0 {
			found := false
			for _, p := range options.Patterns {
				if p == pt {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		matchers = append(matchers, m)
	}
	d.mu.RUnlock()

	// Run matchers in parallel
	type matchResult struct {
		patterns []DetectedPattern
		err      error
	}
	results := make(chan matchResult, len(matchers))
	var wg sync.WaitGroup

	for _, matcher := range matchers {
		wg.Add(1)
		go func(m *PatternMatcher) {
			defer wg.Done()

			patterns := m.Match(ctx, d.graph, d.index, scope)
			results <- matchResult{patterns: patterns}
		}(matcher)
	}

	// Close results channel when all matchers complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect and filter results
	allPatterns := make([]DetectedPattern, 0)
	for result := range results {
		if result.err != nil {
			continue
		}

		for _, p := range result.patterns {
			// Filter by confidence
			if p.Confidence < options.MinConfidence {
				continue
			}
			// Filter non-idiomatic if requested
			if !options.IncludeNonIdiomatic && !p.Idiomatic {
				continue
			}
			allPatterns = append(allPatterns, p)
			recordPatternByType(ctx, string(p.Type))
		}
	}

	setDetectSpanResult(span, len(allPatterns), true)
	recordDetectMetrics(ctx, time.Since(start), len(allPatterns), true)

	return allPatterns, nil
}

// DetectPattern detects a specific pattern type.
//
// # Description
//
// Shorthand for detecting a single pattern type.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - scope: Package or file path prefix to scan.
//   - patternType: The specific pattern to look for.
//
// # Outputs
//
//   - []DetectedPattern: Found patterns.
//   - error: Non-nil on failure.
func (d *PatternDetector) DetectPattern(
	ctx context.Context,
	scope string,
	patternType PatternType,
) ([]DetectedPattern, error) {
	return d.DetectPatterns(ctx, scope, &DetectionOptions{
		Patterns: []PatternType{patternType},
	})
}

// GetMatcher returns a registered matcher by pattern type.
func (d *PatternDetector) GetMatcher(patternType PatternType) (*PatternMatcher, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	m, found := d.matchers[patternType]
	return m, found
}

// ListPatterns returns all registered pattern types.
func (d *PatternDetector) ListPatterns() []PatternType {
	d.mu.RLock()
	defer d.mu.RUnlock()

	patterns := make([]PatternType, 0, len(d.matchers))
	for pt := range d.matchers {
		patterns = append(patterns, pt)
	}
	return patterns
}

// Summary generates a summary of detected patterns.
func (d *PatternDetector) Summary(patterns []DetectedPattern) string {
	if len(patterns) == 0 {
		return "No patterns detected"
	}

	counts := make(map[PatternType]int)
	idiomaticCounts := make(map[PatternType]int)

	for _, p := range patterns {
		counts[p.Type]++
		if p.Idiomatic {
			idiomaticCounts[p.Type]++
		}
	}

	summary := fmt.Sprintf("Detected %d pattern(s):", len(patterns))
	for pt, count := range counts {
		idiomatic := idiomaticCounts[pt]
		summary += fmt.Sprintf("\n  %s: %d (%d idiomatic)", pt, count, idiomatic)
	}

	return summary
}
