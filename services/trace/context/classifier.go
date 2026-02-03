// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package context

import (
	"regexp"
	"strings"
	"unicode"
)

// QueryType represents the classification of a user query.
type QueryType int

const (
	// QueryTypeOverview indicates a high-level overview query.
	// Example: "What does this codebase do?"
	// Strategy: Use Level 0-1 summaries only, skip drill-down.
	QueryTypeOverview QueryType = iota

	// QueryTypeConceptual indicates a conceptual/architectural query.
	// Example: "How does authentication work?"
	// Strategy: Full hierarchical retrieval with drill-down.
	QueryTypeConceptual

	// QueryTypeSpecific indicates a specific symbol lookup query.
	// Example: "Where is ValidateToken defined?"
	// Strategy: Direct symbol lookup, skip hierarchy.
	QueryTypeSpecific

	// QueryTypeLocational indicates a path-based lookup query.
	// Example: "Show me the auth package"
	// Strategy: Direct path lookup, expand children.
	QueryTypeLocational
)

// String returns the human-readable name for the query type.
func (t QueryType) String() string {
	switch t {
	case QueryTypeOverview:
		return "overview"
	case QueryTypeConceptual:
		return "conceptual"
	case QueryTypeSpecific:
		return "specific"
	case QueryTypeLocational:
		return "locational"
	default:
		return "unknown"
	}
}

// QueryClassification contains the result of query classification.
type QueryClassification struct {
	// Type is the classified query type.
	Type QueryType

	// Confidence is the classification confidence (0.0-1.0).
	Confidence float64

	// Signals lists which patterns matched.
	Signals []string

	// ExtractedTerms contains any specific terms extracted
	// (e.g., symbol names, paths).
	ExtractedTerms []string
}

// QueryClassifier classifies queries to determine retrieval strategy.
//
// Thread Safety: Implementations must be safe for concurrent use.
type QueryClassifier interface {
	// Classify analyzes a query and returns its classification.
	//
	// Inputs:
	//   - query: The user query string.
	//
	// Outputs:
	//   - *QueryClassification: The classification result.
	Classify(query string) *QueryClassification
}

// PatternClassifier implements QueryClassifier using pattern matching.
//
// Thread Safety: Safe for concurrent use (stateless after init).
type PatternClassifier struct {
	overviewPatterns   []classifierPattern
	conceptualPatterns []classifierPattern
	specificPatterns   []classifierPattern
	locationalPatterns []classifierPattern
	camelCaseRegex     *regexp.Regexp
	pathRegex          *regexp.Regexp
}

// classifierPattern represents a pattern for classification.
type classifierPattern struct {
	pattern string
	weight  float64
	regex   *regexp.Regexp
}

// NewPatternClassifier creates a new pattern-based classifier.
func NewPatternClassifier() *PatternClassifier {
	c := &PatternClassifier{
		camelCaseRegex: regexp.MustCompile(`[A-Z][a-z]+[A-Z][a-zA-Z]*`),
		pathRegex:      regexp.MustCompile(`(?i)(?:in\s+)?(?:the\s+)?([a-z_][a-z0-9_]*(?:/[a-z_][a-z0-9_]*)+)(?:\s|$|\.)`),
	}
	c.initPatterns()
	return c
}

// initPatterns initializes the classification patterns.
func (c *PatternClassifier) initPatterns() {
	// Overview patterns - high-level questions about the codebase
	c.overviewPatterns = []classifierPattern{
		{pattern: `(?i)^what does\s`, weight: 1.0},
		{pattern: `(?i)^what is this`, weight: 1.0},
		{pattern: `(?i)overview`, weight: 0.9},
		{pattern: `(?i)explain this`, weight: 0.8},
		{pattern: `(?i)describe\s+(this\s+)?(codebase|project|repo)`, weight: 1.0},
		{pattern: `(?i)summarize`, weight: 0.8},
		{pattern: `(?i)high[- ]?level`, weight: 0.7},
		{pattern: `(?i)general\s+structure`, weight: 0.9},
		{pattern: `(?i)what('s| is) (the )?(purpose|goal)`, weight: 0.9},
	}

	// Conceptual patterns - understanding how things work
	c.conceptualPatterns = []classifierPattern{
		{pattern: `(?i)^how does`, weight: 1.0},
		{pattern: `(?i)^how do`, weight: 0.9},
		{pattern: `(?i)^why does`, weight: 0.9},
		{pattern: `(?i)^why do`, weight: 0.8},
		{pattern: `(?i)^when does`, weight: 0.8},
		{pattern: `(?i)architecture`, weight: 0.9},
		{pattern: `(?i)flow`, weight: 0.7},
		{pattern: `(?i)process`, weight: 0.6},
		{pattern: `(?i)mechanism`, weight: 0.8},
		{pattern: `(?i)work(s|ing)?(\s+together)?`, weight: 0.6},
		{pattern: `(?i)interact(s|ion)?`, weight: 0.7},
		{pattern: `(?i)connect(s|ed|ion)?`, weight: 0.6},
		{pattern: `(?i)relationship`, weight: 0.7},
		{pattern: `(?i)design`, weight: 0.6},
	}

	// Specific patterns - looking for specific symbols
	c.specificPatterns = []classifierPattern{
		{pattern: `(?i)^where is`, weight: 1.0},
		{pattern: `(?i)^find\s`, weight: 0.9},
		{pattern: `(?i)^locate\s`, weight: 0.9},
		{pattern: `(?i)definition\s+of`, weight: 1.0},
		{pattern: `(?i)defined(\?|$)`, weight: 0.8},
		{pattern: `(?i)^what is\s+[A-Z]`, weight: 0.7}, // "What is ValidateToken"
		{pattern: `(?i)signature`, weight: 0.7},
		{pattern: `(?i)implement(s|ed|ation)?\s+of`, weight: 0.8},
		{pattern: `(?i)declaration`, weight: 0.8},
	}

	// Locational patterns - path-based queries
	c.locationalPatterns = []classifierPattern{
		{pattern: `(?i)^show me`, weight: 1.0},
		{pattern: `(?i)^open\s`, weight: 0.9},
		{pattern: `(?i)^look at`, weight: 0.8},
		{pattern: `(?i)^go to`, weight: 0.8},
		{pattern: `(?i)the\s+\w+\s+package`, weight: 0.9},
		{pattern: `(?i)the\s+\w+\s+file`, weight: 0.9},
		{pattern: `(?i)the\s+\w+\s+module`, weight: 0.9},
		{pattern: `(?i)in\s+[a-z_]+(/[a-z_]+)+`, weight: 1.0},
		{pattern: `(?i)inside\s+`, weight: 0.7},
		{pattern: `(?i)^list\s`, weight: 0.7},
	}

	// Compile all patterns
	for i := range c.overviewPatterns {
		c.overviewPatterns[i].regex = regexp.MustCompile(c.overviewPatterns[i].pattern)
	}
	for i := range c.conceptualPatterns {
		c.conceptualPatterns[i].regex = regexp.MustCompile(c.conceptualPatterns[i].pattern)
	}
	for i := range c.specificPatterns {
		c.specificPatterns[i].regex = regexp.MustCompile(c.specificPatterns[i].pattern)
	}
	for i := range c.locationalPatterns {
		c.locationalPatterns[i].regex = regexp.MustCompile(c.locationalPatterns[i].pattern)
	}
}

// Classify classifies a query based on pattern matching.
func (c *PatternClassifier) Classify(query string) *QueryClassification {
	query = strings.TrimSpace(query)
	if query == "" {
		return &QueryClassification{
			Type:       QueryTypeConceptual,
			Confidence: 0.0,
		}
	}

	result := &QueryClassification{
		Signals:        make([]string, 0),
		ExtractedTerms: make([]string, 0),
	}

	scores := map[QueryType]float64{
		QueryTypeOverview:   0,
		QueryTypeConceptual: 0,
		QueryTypeSpecific:   0,
		QueryTypeLocational: 0,
	}

	// Check overview patterns
	for _, p := range c.overviewPatterns {
		if p.regex.MatchString(query) {
			scores[QueryTypeOverview] += p.weight
			result.Signals = append(result.Signals, "overview:"+p.pattern)
		}
	}

	// Check conceptual patterns
	for _, p := range c.conceptualPatterns {
		if p.regex.MatchString(query) {
			scores[QueryTypeConceptual] += p.weight
			result.Signals = append(result.Signals, "conceptual:"+p.pattern)
		}
	}

	// Check specific patterns
	for _, p := range c.specificPatterns {
		if p.regex.MatchString(query) {
			scores[QueryTypeSpecific] += p.weight
			result.Signals = append(result.Signals, "specific:"+p.pattern)
		}
	}

	// Check locational patterns
	for _, p := range c.locationalPatterns {
		if p.regex.MatchString(query) {
			scores[QueryTypeLocational] += p.weight
			result.Signals = append(result.Signals, "locational:"+p.pattern)
		}
	}

	// Check for CamelCase symbols (strong indicator of specific query)
	if matches := c.camelCaseRegex.FindAllString(query, -1); len(matches) > 0 {
		scores[QueryTypeSpecific] += 1.5
		result.Signals = append(result.Signals, "camelCase")
		result.ExtractedTerms = append(result.ExtractedTerms, matches...)
	}

	// Check for snake_case function names
	if snakeCase := c.findSnakeCaseSymbols(query); len(snakeCase) > 0 {
		scores[QueryTypeSpecific] += 1.0
		result.Signals = append(result.Signals, "snake_case")
		result.ExtractedTerms = append(result.ExtractedTerms, snakeCase...)
	}

	// Check for path patterns
	if matches := c.pathRegex.FindAllStringSubmatch(query, -1); len(matches) > 0 {
		scores[QueryTypeLocational] += 1.5
		result.Signals = append(result.Signals, "path")
		for _, m := range matches {
			if len(m) > 1 {
				result.ExtractedTerms = append(result.ExtractedTerms, m[1])
			}
		}
	}

	// Find the highest scoring type
	var maxScore float64
	var maxType QueryType = QueryTypeConceptual // default

	for t, score := range scores {
		if score > maxScore {
			maxScore = score
			maxType = t
		}
	}

	result.Type = maxType

	// Calculate confidence based on score magnitude and differentiation
	totalScore := scores[QueryTypeOverview] + scores[QueryTypeConceptual] +
		scores[QueryTypeSpecific] + scores[QueryTypeLocational]

	if totalScore > 0 {
		// Confidence is based on how much the winner leads
		result.Confidence = maxScore / totalScore
		// Boost confidence if there's a clear winner
		if maxScore >= 1.5 {
			result.Confidence = minFloat(result.Confidence*1.2, 1.0)
		}
	} else {
		// No patterns matched - default to conceptual with low confidence
		result.Confidence = 0.3
	}

	return result
}

// findSnakeCaseSymbols finds snake_case function-like names in the query.
func (c *PatternClassifier) findSnakeCaseSymbols(query string) []string {
	// Look for patterns like "validate_token" or "handle_request"
	pattern := regexp.MustCompile(`\b[a-z]+_[a-z_]+\b`)
	matches := pattern.FindAllString(query, -1)

	// Filter out common words
	commonPhrases := map[string]bool{
		"of_the": true, "in_the": true, "to_the": true,
		"is_a": true, "is_the": true, "at_the": true,
	}

	result := make([]string, 0)
	for _, m := range matches {
		if !commonPhrases[m] {
			result = append(result, m)
		}
	}
	return result
}

// minFloat returns the minimum of two float64 values.
func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// ContainsCamelCase returns true if the string contains CamelCase words.
func ContainsCamelCase(s string) bool {
	for i := 0; i < len(s)-1; i++ {
		if unicode.IsLower(rune(s[i])) && unicode.IsUpper(rune(s[i+1])) {
			return true
		}
	}
	return false
}

// DefaultQueryClassifier is the default query classifier.
var DefaultQueryClassifier = NewPatternClassifier()

// ClassifyQuery is a convenience function to classify a query.
func ClassifyQuery(query string) *QueryClassification {
	return DefaultQueryClassifier.Classify(query)
}
