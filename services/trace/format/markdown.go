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
	"io"
	"strings"
)

// MarkdownFormatter formats results as Markdown tables and lists.
type MarkdownFormatter struct {
	maxRows int
}

// NewMarkdownFormatter creates a new Markdown formatter.
func NewMarkdownFormatter() *MarkdownFormatter {
	return &MarkdownFormatter{maxRows: 100}
}

// SetMaxRows sets the maximum number of table rows.
func (f *MarkdownFormatter) SetMaxRows(max int) {
	f.maxRows = max
}

// Format converts the result to a Markdown string.
func (f *MarkdownFormatter) Format(result interface{}) (string, error) {
	var sb strings.Builder
	if err := f.FormatStreaming(result, &sb); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// Name returns the format name.
func (f *MarkdownFormatter) Name() FormatType {
	return FormatMarkdown
}

// TokenEstimate estimates the number of tokens.
func (f *MarkdownFormatter) TokenEstimate(result interface{}, tokenizer ...string) int {
	output, err := f.Format(result)
	if err != nil {
		return 0
	}

	ratio := TokenRatio["default"]
	if len(tokenizer) > 0 {
		if r, ok := TokenRatio[tokenizer[0]]; ok {
			ratio = r
		}
	}

	return int(float64(len(output)) / ratio)
}

// IsReversible returns false - Markdown loses structure.
func (f *MarkdownFormatter) IsReversible() bool {
	return false
}

// SupportsStreaming returns true.
func (f *MarkdownFormatter) SupportsStreaming() bool {
	return true
}

// FormatStreaming writes Markdown to a writer.
func (f *MarkdownFormatter) FormatStreaming(result interface{}, w io.Writer) error {
	switch r := result.(type) {
	case *GraphResult:
		return f.formatGraphResult(r, w)
	case GraphResult:
		return f.formatGraphResult(&r, w)
	default:
		return errors.New("unsupported result type for markdown format")
	}
}

// formatGraphResult formats a graph result as Markdown.
func (f *MarkdownFormatter) formatGraphResult(r *GraphResult, w io.Writer) error {
	// Header
	fmt.Fprintf(w, "## Code Map: %s\n\n", r.Query)

	// Focus nodes as list
	if len(r.FocusNodes) > 0 {
		fmt.Fprintln(w, "### Entry Points")
		fmt.Fprintln(w)
		for _, n := range r.FocusNodes {
			fmt.Fprintf(w, "- **%s** (`%s`)\n", n.Name, n.Location)
		}
		fmt.Fprintln(w)
	}

	// Key nodes as table
	if len(r.KeyNodes) > 0 {
		fmt.Fprintln(w, "### Symbol Summary")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "| Symbol | Location | Callers | Callees | Status |")
		fmt.Fprintln(w, "|--------|----------|---------|---------|--------|")

		rows := r.KeyNodes
		truncated := false
		if len(rows) > f.maxRows {
			rows = rows[:f.maxRows]
			truncated = true
		}

		for _, k := range rows {
			status := ""
			if k.Warning != "" {
				status = "⚠️ " + k.Warning
			} else if k.Flag != "" {
				status = strings.ToUpper(k.Flag)
			} else {
				status = "-"
			}

			location := shortLocation(k.Location)
			fmt.Fprintf(w, "| %s | %s | %d | %d | %s |\n",
				k.Name, location, k.Callers, k.Callees, status)
		}
		fmt.Fprintln(w)

		if truncated {
			fmt.Fprintf(w, "*Showing %d of %d symbols. Use format=json for complete data.*\n\n",
				f.maxRows, len(r.KeyNodes))
		}
	}

	// Risk assessment
	if r.Risk.Level != "" {
		fmt.Fprintln(w, "### Risk Assessment")
		fmt.Fprintln(w)

		riskEmoji := map[string]string{
			"low":    "🟢",
			"medium": "🟡",
			"high":   "🔴",
		}
		emoji := riskEmoji[strings.ToLower(r.Risk.Level)]
		if emoji == "" {
			emoji = "⚪"
		}

		fmt.Fprintf(w, "**Risk Level:** %s %s\n\n", emoji, strings.ToUpper(r.Risk.Level))
		fmt.Fprintf(w, "- **Direct impact:** %d files\n", r.Risk.DirectImpact)
		fmt.Fprintf(w, "- **Indirect impact:** %d files\n", r.Risk.IndirectImpact)
		fmt.Fprintln(w)

		if len(r.Risk.Warnings) > 0 {
			fmt.Fprintln(w, "**Warnings:**")
			for _, warn := range r.Risk.Warnings {
				fmt.Fprintf(w, "- ⚠️ %s\n", warn)
			}
			fmt.Fprintln(w)
		}
	}

	// Graph stats
	fmt.Fprintln(w, "### Graph Statistics")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "- **Nodes:** %d\n", r.Graph.NodeCount)
	fmt.Fprintf(w, "- **Edges:** %d\n", r.Graph.EdgeCount)
	fmt.Fprintf(w, "- **Max depth:** %d\n", r.Graph.Depth)

	return nil
}
