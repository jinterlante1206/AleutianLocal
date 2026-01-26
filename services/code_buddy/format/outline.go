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
	"fmt"
	"io"
	"os"
	"strings"
)

// TreeChars defines characters for tree drawing.
type TreeChars struct {
	Branch   string // ├──
	LastItem string // └──
	Vertical string // │
	Arrow    string // →
	Warning  string // ⚠️
}

// UnicodeChars are the Unicode tree characters.
var UnicodeChars = TreeChars{
	Branch:   "├── ",
	LastItem: "└── ",
	Vertical: "│   ",
	Arrow:    "→",
	Warning:  "⚠️",
}

// ASCIIChars are the ASCII fallback characters.
var ASCIIChars = TreeChars{
	Branch:   "|-- ",
	LastItem: "`-- ",
	Vertical: "|   ",
	Arrow:    "->",
	Warning:  "[!]",
}

// OutlineFormatter formats results as tree-style outlines.
type OutlineFormatter struct {
	useUnicode bool
	chars      TreeChars
	maxDepth   int
}

// NewOutlineFormatter creates a new outline formatter.
// If unicode is true, uses Unicode box-drawing characters.
func NewOutlineFormatter(unicode bool) *OutlineFormatter {
	f := &OutlineFormatter{
		useUnicode: unicode,
		maxDepth:   10,
	}
	if unicode {
		f.chars = UnicodeChars
	} else {
		f.chars = ASCIIChars
	}
	return f
}

// NewOutlineFormatterAutoDetect creates an outline formatter with auto-detected charset.
func NewOutlineFormatterAutoDetect() *OutlineFormatter {
	useUnicode := true

	// Check TERM environment variable
	term := os.Getenv("TERM")
	if term == "dumb" || term == "" {
		useUnicode = false
	}

	// Check for Windows legacy terminal
	if os.Getenv("WT_SESSION") == "" && os.Getenv("TERM_PROGRAM") == "" {
		// Might be legacy Windows CMD
		if os.Getenv("COMSPEC") != "" {
			useUnicode = false
		}
	}

	return NewOutlineFormatter(useUnicode)
}

// SetASCIIMode sets whether to use ASCII characters.
func (f *OutlineFormatter) SetASCIIMode(ascii bool) {
	f.useUnicode = !ascii
	if f.useUnicode {
		f.chars = UnicodeChars
	} else {
		f.chars = ASCIIChars
	}
}

// Format converts the result to an outline string.
func (f *OutlineFormatter) Format(result interface{}) (string, error) {
	var sb strings.Builder
	if err := f.FormatStreaming(result, &sb); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// Name returns the format name.
func (f *OutlineFormatter) Name() FormatType {
	return FormatOutline
}

// TokenEstimate estimates the number of tokens.
func (f *OutlineFormatter) TokenEstimate(result interface{}, tokenizer ...string) int {
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

// IsReversible returns false - outline loses some structure.
func (f *OutlineFormatter) IsReversible() bool {
	return false
}

// SupportsStreaming returns true.
func (f *OutlineFormatter) SupportsStreaming() bool {
	return true
}

// FormatStreaming writes outline to a writer.
func (f *OutlineFormatter) FormatStreaming(result interface{}, w io.Writer) error {
	switch r := result.(type) {
	case *GraphResult:
		return f.formatGraphResult(r, w)
	case GraphResult:
		return f.formatGraphResult(&r, w)
	default:
		// Fall back to simple representation
		_, err := fmt.Fprintf(w, "%+v\n", result)
		return err
	}
}

// formatGraphResult formats a graph result as an outline.
func (f *OutlineFormatter) formatGraphResult(r *GraphResult, w io.Writer) error {
	// Header
	fmt.Fprintf(w, "Code Map for: %q\n", r.Query)
	fmt.Fprintln(w, strings.Repeat("═", 40))
	fmt.Fprintln(w)

	// Focus nodes (entry points)
	if len(r.FocusNodes) > 0 {
		fmt.Fprintln(w, "Entry Points:")
		for i, node := range r.FocusNodes {
			prefix := f.chars.Branch
			if i == len(r.FocusNodes)-1 {
				prefix = f.chars.LastItem
			}
			fmt.Fprintf(w, "  %s%s (%s)\n", prefix, node.Name, node.Location)
		}
		fmt.Fprintln(w)
	}

	// Key nodes with connections
	if len(r.KeyNodes) > 0 {
		fmt.Fprintln(w, "Dependency Graph:")
		for _, keyNode := range r.KeyNodes {
			f.formatKeyNode(&keyNode, w, "  ")
		}
		fmt.Fprintln(w)
	}

	// Risk assessment
	if r.Risk.Level != "" {
		fmt.Fprintf(w, "Risk Assessment: %s\n", strings.ToUpper(r.Risk.Level))
		fmt.Fprintf(w, "  - Direct impact: %d files\n", r.Risk.DirectImpact)
		fmt.Fprintf(w, "  - Indirect impact: %d files\n", r.Risk.IndirectImpact)
		for _, warning := range r.Risk.Warnings {
			fmt.Fprintf(w, "  %s %s\n", f.chars.Warning, warning)
		}
	}

	// Graph stats
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Graph: %d nodes, %d edges, depth %d\n",
		r.Graph.NodeCount, r.Graph.EdgeCount, r.Graph.Depth)

	return nil
}

// formatKeyNode formats a key node with its connections.
func (f *OutlineFormatter) formatKeyNode(node *KeyNode, w io.Writer, indent string) {
	// Node name with flag
	nodeLine := node.Name
	if node.Flag != "" {
		nodeLine += fmt.Sprintf(" [%s]", node.Flag)
	}
	if node.Warning != "" {
		nodeLine += fmt.Sprintf(" %s %s", f.chars.Warning, node.Warning)
	}
	fmt.Fprintf(w, "%s%s\n", indent, nodeLine)

	// Callers
	if node.Callers > 0 {
		fmt.Fprintf(w, "%s%sCalled by (%d):\n", indent, f.chars.Branch, node.Callers)
		f.formatConnections(node.Connections, "called_by", w, indent+f.chars.Vertical)
	}

	// Callees
	if node.Callees > 0 {
		fmt.Fprintf(w, "%s%sCalls (%d):\n", indent, f.chars.Branch, node.Callees)
		f.formatConnections(node.Connections, "calls", w, indent+f.chars.Vertical)
	}

	// Types used
	typeConns := f.filterConnections(node.Connections, "uses_type")
	if len(typeConns) > 0 {
		fmt.Fprintf(w, "%s%sUses types (%d):\n", indent, f.chars.LastItem, len(typeConns))
		f.formatConnections(node.Connections, "uses_type", w, indent+"    ")
	}
}

// formatConnections formats a list of connections.
func (f *OutlineFormatter) formatConnections(conns []Connection, connType string, w io.Writer, indent string) {
	filtered := f.filterConnections(conns, connType)
	for i, conn := range filtered {
		prefix := f.chars.Branch
		if i == len(filtered)-1 {
			prefix = f.chars.LastItem
		}
		fmt.Fprintf(w, "%s%s%s\n", indent, prefix, conn.TargetID)
	}
}

// filterConnections filters connections by type.
func (f *OutlineFormatter) filterConnections(conns []Connection, connType string) []Connection {
	var result []Connection
	for _, c := range conns {
		if c.Type == connType {
			result = append(result, c)
		}
	}
	return result
}
