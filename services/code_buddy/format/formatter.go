// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package format

import (
	"errors"
	"fmt"
)

// ErrFormatNotSupported is returned when a format type is not supported.
var ErrFormatNotSupported = errors.New("format not supported")

// ErrResultTooLarge is returned when a result exceeds format capabilities.
var ErrResultTooLarge = errors.New("result exceeds format capabilities")

// FormatRegistry manages format specifications and capabilities.
type FormatRegistry struct {
	formatters   map[FormatType]Formatter
	capabilities map[FormatType]FormatCapability
}

// NewFormatRegistry creates a new format registry with default formatters.
func NewFormatRegistry() *FormatRegistry {
	r := &FormatRegistry{
		formatters:   make(map[FormatType]Formatter),
		capabilities: make(map[FormatType]FormatCapability),
	}

	// Register default formatters
	r.Register(FormatJSON, NewJSONFormatter())
	r.Register(FormatOutline, NewOutlineFormatter(true))
	r.Register(FormatCompact, NewCompactFormatter())
	r.Register(FormatMermaid, NewMermaidFormatter(50))
	r.Register(FormatMarkdown, NewMarkdownFormatter())

	// Set capabilities
	r.capabilities = map[FormatType]FormatCapability{
		FormatJSON:     {MaxNodes: 0, MaxRows: 0, MaxTokens: 0, SupportsStreaming: true},
		FormatOutline:  {MaxNodes: 500, MaxRows: 0, MaxTokens: 10000, SupportsStreaming: true},
		FormatCompact:  {MaxNodes: 0, MaxRows: 0, MaxTokens: 50000, SupportsStreaming: false},
		FormatMermaid:  {MaxNodes: 100, MaxRows: 0, MaxTokens: 5000, SupportsStreaming: false},
		FormatMarkdown: {MaxNodes: 0, MaxRows: 100, MaxTokens: 8000, SupportsStreaming: true},
	}

	return r
}

// Register registers a formatter for a format type.
func (r *FormatRegistry) Register(formatType FormatType, formatter Formatter) {
	r.formatters[formatType] = formatter
}

// GetFormatter returns the formatter for the given type.
func (r *FormatRegistry) GetFormatter(formatType FormatType) (Formatter, error) {
	f, ok := r.formatters[formatType]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrFormatNotSupported, formatType)
	}
	return f, nil
}

// ValidateCapability checks if a result can be formatted with the given format.
func (r *FormatRegistry) ValidateCapability(result interface{}, formatType FormatType) error {
	cap, ok := r.capabilities[formatType]
	if !ok {
		return fmt.Errorf("%w: %s", ErrFormatNotSupported, formatType)
	}

	stats := AnalyzeResult(result)

	if cap.MaxNodes > 0 && stats.NodeCount > cap.MaxNodes {
		return fmt.Errorf("%w: result has %d nodes, but %s format supports max %d. Use format=json or format=compact",
			ErrResultTooLarge, stats.NodeCount, formatType, cap.MaxNodes)
	}

	if cap.MaxRows > 0 && stats.RowCount > cap.MaxRows {
		return fmt.Errorf("%w: result has %d rows, but %s format supports max %d. Use format=json",
			ErrResultTooLarge, stats.RowCount, formatType, cap.MaxRows)
	}

	return nil
}

// Format formats a result with the specified format type.
func (r *FormatRegistry) Format(result interface{}, formatType FormatType) (string, error) {
	f, err := r.GetFormatter(formatType)
	if err != nil {
		return "", err
	}

	if err := r.ValidateCapability(result, formatType); err != nil {
		return "", err
	}

	return f.Format(result)
}

// AutoSelectFormat selects the best format based on result size and token budget.
func (r *FormatRegistry) AutoSelectFormat(result interface{}, tokenBudget int) FormatType {
	stats := AnalyzeResult(result)

	// If small result, use full JSON
	if stats.NodeCount <= 20 {
		return FormatJSON
	}

	// If medium result, use outline
	if stats.NodeCount <= 100 {
		return FormatOutline
	}

	// If large but within token budget, use compact
	if tokenBudget > 0 {
		// Estimate compact JSON tokens (roughly 50% of full JSON)
		estimatedCompactTokens := stats.NodeCount * 20 // rough estimate
		if estimatedCompactTokens < tokenBudget {
			return FormatCompact
		}
	}

	// Default to markdown summary for very large results
	return FormatMarkdown
}

// GetCapability returns the capability for a format type.
func (r *FormatRegistry) GetCapability(formatType FormatType) (FormatCapability, bool) {
	cap, ok := r.capabilities[formatType]
	return cap, ok
}

// ListFormats returns all supported format types.
func (r *FormatRegistry) ListFormats() []FormatType {
	formats := make([]FormatType, 0, len(r.formatters))
	for f := range r.formatters {
		formats = append(formats, f)
	}
	return formats
}

// GetMetadata returns metadata for a format type.
func (r *FormatRegistry) GetMetadata(formatType FormatType) (FormatMetadata, error) {
	f, err := r.GetFormatter(formatType)
	if err != nil {
		return FormatMetadata{}, err
	}

	note := ""
	if !f.IsReversible() {
		note = "Use format=json for full fidelity"
	}

	return FormatMetadata{
		Type:       formatType,
		Version:    FormatVersion,
		Reversible: f.IsReversible(),
		Note:       note,
	}, nil
}

// DefaultRegistry is the default format registry.
var DefaultRegistry = NewFormatRegistry()

// GetFormatter returns a formatter from the default registry.
func GetFormatter(formatType FormatType) (Formatter, error) {
	return DefaultRegistry.GetFormatter(formatType)
}

// Format formats a result using the default registry.
func Format(result interface{}, formatType FormatType) (string, error) {
	return DefaultRegistry.Format(result, formatType)
}

// AutoSelectFormat selects a format using the default registry.
func AutoSelectFormat(result interface{}, tokenBudget int) FormatType {
	return DefaultRegistry.AutoSelectFormat(result, tokenBudget)
}
