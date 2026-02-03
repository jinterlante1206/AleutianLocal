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
	"sort"
	"strings"
)

// MermaidFormatter formats results as Mermaid graph diagrams.
type MermaidFormatter struct {
	maxNodes  int
	direction string // TD (top-down) or LR (left-right)
}

// NewMermaidFormatter creates a new Mermaid formatter.
// maxNodes limits the number of nodes for readability (default: 50).
func NewMermaidFormatter(maxNodes int) *MermaidFormatter {
	if maxNodes <= 0 {
		maxNodes = 50
	}
	return &MermaidFormatter{
		maxNodes:  maxNodes,
		direction: "TD",
	}
}

// SetDirection sets the graph direction (TD or LR).
func (f *MermaidFormatter) SetDirection(dir string) {
	if dir == "LR" || dir == "TD" {
		f.direction = dir
	}
}

// Format converts the result to a Mermaid diagram string.
func (f *MermaidFormatter) Format(result interface{}) (string, error) {
	var sb strings.Builder
	if err := f.FormatStreaming(result, &sb); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// Name returns the format name.
func (f *MermaidFormatter) Name() FormatType {
	return FormatMermaid
}

// TokenEstimate estimates the number of tokens.
func (f *MermaidFormatter) TokenEstimate(result interface{}, tokenizer ...string) int {
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

// IsReversible returns false - Mermaid is visual only.
func (f *MermaidFormatter) IsReversible() bool {
	return false
}

// SupportsStreaming returns false - needs full graph structure.
func (f *MermaidFormatter) SupportsStreaming() bool {
	return false
}

// FormatStreaming writes Mermaid diagram to a writer.
func (f *MermaidFormatter) FormatStreaming(result interface{}, w io.Writer) error {
	switch r := result.(type) {
	case *GraphResult:
		return f.formatGraphResult(r, w)
	case GraphResult:
		return f.formatGraphResult(&r, w)
	default:
		return errors.New("unsupported result type for mermaid format")
	}
}

// formatGraphResult formats a graph result as a Mermaid diagram.
func (f *MermaidFormatter) formatGraphResult(r *GraphResult, w io.Writer) error {
	fmt.Fprintln(w, "```mermaid")
	fmt.Fprintf(w, "graph %s\n", f.direction)

	// Collect all nodes and prioritize
	allNodes := f.collectNodes(r)
	prioritized := f.prioritizeNodes(allNodes, r.FocusNodes)

	// Track if we truncated
	truncated := len(allNodes) > f.maxNodes
	if truncated {
		fmt.Fprintf(w, "    %%%% Showing %d of %d nodes. Use format=json for complete data.\n",
			len(prioritized), len(allNodes))
	}

	// Group nodes by role
	callers := make([]mermaidNode, 0)
	targets := make([]mermaidNode, 0)
	dependencies := make([]mermaidNode, 0)

	// Build node ID set for edge filtering
	nodeIDs := make(map[string]bool)
	for _, n := range prioritized {
		nodeIDs[n.ID] = true
	}

	// Categorize nodes
	focusIDs := make(map[string]bool)
	for _, n := range r.FocusNodes {
		focusIDs[n.ID] = true
	}

	for _, n := range prioritized {
		if focusIDs[n.ID] {
			targets = append(targets, n)
		} else if n.Role == "caller" {
			callers = append(callers, n)
		} else {
			dependencies = append(dependencies, n)
		}
	}

	// Write subgraphs
	if len(callers) > 0 {
		fmt.Fprintln(w, "    subgraph Callers")
		for _, n := range callers {
			f.writeNode(w, n)
		}
		fmt.Fprintln(w, "    end")
		fmt.Fprintln(w)
	}

	if len(targets) > 0 {
		fmt.Fprintln(w, "    subgraph Target")
		for _, n := range targets {
			f.writeNode(w, n)
		}
		fmt.Fprintln(w, "    end")
		fmt.Fprintln(w)
	}

	if len(dependencies) > 0 {
		fmt.Fprintln(w, "    subgraph Dependencies")
		for _, n := range dependencies {
			f.writeNode(w, n)
		}
		fmt.Fprintln(w, "    end")
		fmt.Fprintln(w)
	}

	// Write edges (only between included nodes)
	for _, key := range r.KeyNodes {
		if !nodeIDs[key.ID] {
			continue
		}
		for _, conn := range key.Connections {
			if !nodeIDs[conn.TargetID] {
				continue
			}
			f.writeEdge(w, key.ID, conn.TargetID, conn.Type)
		}
	}

	// Write styles for warnings
	for _, n := range prioritized {
		if n.Warning != "" {
			fmt.Fprintf(w, "    style %s fill:#ff9,stroke:#f90\n", sanitizeID(n.ID))
		}
	}

	fmt.Fprintln(w, "```")
	return nil
}

// mermaidNode is an internal node representation for Mermaid.
type mermaidNode struct {
	ID          string
	Name        string
	Location    string
	Warning     string
	Flag        string
	Role        string // caller, target, dependency
	Connections int    // total connections for prioritization
	Distance    int    // distance from focus nodes
}

// collectNodes collects all nodes from a graph result.
func (f *MermaidFormatter) collectNodes(r *GraphResult) []mermaidNode {
	nodeMap := make(map[string]mermaidNode)

	// Add focus nodes
	for _, n := range r.FocusNodes {
		nodeMap[n.ID] = mermaidNode{
			ID:       n.ID,
			Name:     n.Name,
			Location: n.Location,
			Role:     "target",
			Distance: 0,
		}
	}

	// Add key nodes and their connections
	for _, k := range r.KeyNodes {
		if _, exists := nodeMap[k.ID]; !exists {
			nodeMap[k.ID] = mermaidNode{
				ID:          k.ID,
				Name:        k.Name,
				Location:    k.Location,
				Warning:     k.Warning,
				Flag:        k.Flag,
				Connections: k.Callers + k.Callees,
				Distance:    1,
			}
		}

		// Add connected nodes
		for _, conn := range k.Connections {
			if _, exists := nodeMap[conn.TargetID]; !exists {
				role := "dependency"
				if conn.Type == "called_by" {
					role = "caller"
				}
				nodeMap[conn.TargetID] = mermaidNode{
					ID:       conn.TargetID,
					Name:     conn.TargetID, // Use ID as name if not available
					Role:     role,
					Distance: 2,
				}
			}
		}
	}

	// Convert to slice
	nodes := make([]mermaidNode, 0, len(nodeMap))
	for _, n := range nodeMap {
		nodes = append(nodes, n)
	}

	return nodes
}

// prioritizeNodes selects the most important nodes up to maxNodes.
func (f *MermaidFormatter) prioritizeNodes(nodes []mermaidNode, focusNodes []Node) []mermaidNode {
	if len(nodes) <= f.maxNodes {
		return nodes
	}

	// Score each node for prioritization
	type scoredNode struct {
		node  mermaidNode
		score int
	}

	scored := make([]scoredNode, len(nodes))
	focusIDs := make(map[string]bool)
	for _, n := range focusNodes {
		focusIDs[n.ID] = true
	}

	for i, n := range nodes {
		score := 0

		// Target nodes always included (highest priority)
		if focusIDs[n.ID] {
			score += 10000
		}

		// Nodes with warnings get high priority
		if n.Warning != "" {
			score += 1000
		}

		// Nodes with flags (shared, entry) get priority
		if n.Flag != "" {
			score += 500
		}

		// More connections = higher priority
		score += n.Connections * 10

		// Closer to focus = higher priority
		score += (10 - n.Distance) * 50

		scored[i] = scoredNode{node: n, score: score}
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Take top maxNodes
	result := make([]mermaidNode, 0, f.maxNodes)
	for i := 0; i < f.maxNodes && i < len(scored); i++ {
		result = append(result, scored[i].node)
	}

	return result
}

// writeNode writes a node definition.
func (f *MermaidFormatter) writeNode(w io.Writer, n mermaidNode) {
	id := sanitizeID(n.ID)
	label := n.Name
	if n.Location != "" {
		label += "<br/>" + shortLocation(n.Location)
	}
	if n.Warning != "" {
		label += " ⚠️"
	}
	if n.Flag != "" {
		label += "<br/>" + strings.ToUpper(n.Flag)
	}

	fmt.Fprintf(w, "        %s[%s]\n", id, label)
}

// writeEdge writes an edge between nodes.
func (f *MermaidFormatter) writeEdge(w io.Writer, from, to, edgeType string) {
	fromID := sanitizeID(from)
	toID := sanitizeID(to)

	switch edgeType {
	case "calls":
		fmt.Fprintf(w, "    %s --> %s\n", fromID, toID)
	case "called_by":
		fmt.Fprintf(w, "    %s --> %s\n", toID, fromID)
	case "implements":
		fmt.Fprintf(w, "    %s -.-> %s\n", fromID, toID)
	case "uses_type":
		fmt.Fprintf(w, "    %s --> %s\n", fromID, toID)
	default:
		fmt.Fprintf(w, "    %s --> %s\n", fromID, toID)
	}
}

// sanitizeID makes an ID safe for Mermaid.
func sanitizeID(id string) string {
	// Replace characters that Mermaid doesn't like
	replacer := strings.NewReplacer(
		".", "_",
		"/", "_",
		"-", "_",
		":", "_",
		"*", "_",
		" ", "_",
		"(", "_",
		")", "_",
	)
	return replacer.Replace(id)
}

// shortLocation extracts a short location from a full path.
func shortLocation(loc string) string {
	// Take just filename:line
	parts := strings.Split(loc, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return loc
}
