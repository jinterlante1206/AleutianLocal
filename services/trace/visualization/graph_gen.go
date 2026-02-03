// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package visualization

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/trace/analysis"
)

// OutputFormat specifies the visualization output format.
type OutputFormat string

const (
	FormatMermaid OutputFormat = "mermaid"
	FormatD3      OutputFormat = "d3"
	FormatSVG     OutputFormat = "svg"
	FormatDOT     OutputFormat = "dot"
)

// GraphGenerator generates visual representations of dependency graphs.
//
// # Description
//
// Creates visual output in various formats including Mermaid diagrams,
// D3.js JSON, and SVG. All rendering is done locally without external services.
//
// # Thread Safety
//
// Safe for concurrent use.
type GraphGenerator struct {
	options GraphOptions
}

// GraphOptions configures graph generation.
type GraphOptions struct {
	// MaxNodes limits the number of nodes in the output.
	// Default: 100
	MaxNodes int

	// MaxDepth limits the depth of transitive dependencies.
	// Default: 3
	MaxDepth int

	// ShowSecurityPaths highlights security-sensitive paths.
	// Default: true
	ShowSecurityPaths bool

	// GroupByPackage groups nodes by package.
	// Default: true
	GroupByPackage bool

	// Direction is the graph direction (TB, LR, BT, RL).
	// Default: "TB"
	Direction string
}

// DefaultGraphOptions returns sensible defaults.
func DefaultGraphOptions() GraphOptions {
	return GraphOptions{
		MaxNodes:          100,
		MaxDepth:          3,
		ShowSecurityPaths: true,
		GroupByPackage:    true,
		Direction:         "TB",
	}
}

// NewGraphGenerator creates a new graph generator.
func NewGraphGenerator(opts *GraphOptions) *GraphGenerator {
	if opts == nil {
		defaults := DefaultGraphOptions()
		opts = &defaults
	}
	return &GraphGenerator{options: *opts}
}

// Generate creates a visual representation of a blast radius.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - br: The blast radius to visualize.
//   - format: The output format.
//
// # Outputs
//
//   - string: The visualization in the requested format.
//   - error: Non-nil on failure.
func (g *GraphGenerator) Generate(ctx context.Context, br *analysis.EnhancedBlastRadius, format OutputFormat) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("context is required")
	}
	if br == nil {
		return "", fmt.Errorf("blast radius is required")
	}

	switch format {
	case FormatMermaid:
		return g.generateMermaid(br), nil
	case FormatD3:
		return g.generateD3JSON(br)
	case FormatSVG:
		return g.generateSVG(br), nil
	case FormatDOT:
		return g.generateDOT(br), nil
	default:
		return "", fmt.Errorf("unsupported format: %s", format)
	}
}

// generateMermaid creates a Mermaid flowchart diagram.
func (g *GraphGenerator) generateMermaid(br *analysis.EnhancedBlastRadius) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("flowchart %s\n", g.options.Direction))

	// Add target node with styling
	targetID := sanitizeMermaidID(br.Target)
	targetName := extractSymbolName(br.Target)
	sb.WriteString(fmt.Sprintf("    %s[[\"%s\"]]:::target\n", targetID, escapeMermaidLabel(targetName)))

	// Group callers by package if enabled
	if g.options.GroupByPackage {
		packages := g.groupByPackage(br.DirectCallers)
		for pkg, callers := range packages {
			if len(callers) > 0 {
				pkgID := sanitizeMermaidID("pkg_" + pkg)
				sb.WriteString(fmt.Sprintf("    subgraph %s[\"%s\"]\n", pkgID, escapeMermaidLabel(pkg)))

				for i, caller := range callers {
					if i >= g.options.MaxNodes/len(packages) {
						sb.WriteString(fmt.Sprintf("        %s_more[...%d more]\n", pkgID, len(callers)-i))
						break
					}

					callerID := sanitizeMermaidID(caller.ID)
					callerName := extractSymbolName(caller.ID)

					// Style based on risk
					style := g.getCallerStyle(caller, br)
					sb.WriteString(fmt.Sprintf("        %s[\"%s\"]%s\n", callerID, escapeMermaidLabel(callerName), style))
				}

				sb.WriteString("    end\n")
			}
		}
	} else {
		// Flat list of callers
		for i, caller := range br.DirectCallers {
			if i >= g.options.MaxNodes {
				sb.WriteString(fmt.Sprintf("    more[...%d more callers]\n", len(br.DirectCallers)-i))
				break
			}

			callerID := sanitizeMermaidID(caller.ID)
			callerName := extractSymbolName(caller.ID)
			style := g.getCallerStyle(caller, br)
			sb.WriteString(fmt.Sprintf("    %s[\"%s\"]%s\n", callerID, escapeMermaidLabel(callerName), style))
		}
	}

	// Add edges
	sb.WriteString("\n")
	for i, caller := range br.DirectCallers {
		if i >= g.options.MaxNodes {
			break
		}
		callerID := sanitizeMermaidID(caller.ID)
		sb.WriteString(fmt.Sprintf("    %s --> %s\n", callerID, targetID))
	}

	// Add styles
	sb.WriteString("\n")
	sb.WriteString("    classDef target fill:#ff6b6b,stroke:#333,stroke-width:2px,color:#fff\n")
	sb.WriteString("    classDef security fill:#ffd93d,stroke:#333,stroke-width:2px\n")
	sb.WriteString("    classDef high fill:#ff9f43,stroke:#333\n")
	sb.WriteString("    classDef low fill:#10ac84,stroke:#333\n")

	return sb.String()
}

// getCallerStyle returns Mermaid style class for a caller.
func (g *GraphGenerator) getCallerStyle(caller analysis.Caller, br *analysis.EnhancedBlastRadius) string {
	// Check if this caller is in a security path
	if g.options.ShowSecurityPaths {
		for _, sp := range br.SecurityPaths {
			if sp.IsSecuritySensitive && strings.Contains(caller.FilePath, sp.PathType) {
				return ":::security"
			}
		}
	}

	// Style by hops (distance from target)
	if caller.Hops == 1 {
		return ":::high"
	}
	return ":::low"
}

// groupByPackage groups callers by their package.
func (g *GraphGenerator) groupByPackage(callers []analysis.Caller) map[string][]analysis.Caller {
	packages := make(map[string][]analysis.Caller)
	for _, caller := range callers {
		pkg := extractPackage(caller.ID)
		packages[pkg] = append(packages[pkg], caller)
	}
	return packages
}

// generateD3JSON creates D3.js compatible JSON.
func (g *GraphGenerator) generateD3JSON(br *analysis.EnhancedBlastRadius) (string, error) {
	type D3Node struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Package  string `json:"package"`
		Group    int    `json:"group"`
		IsTarget bool   `json:"isTarget"`
		Security bool   `json:"security,omitempty"`
		Distance int    `json:"distance,omitempty"`
	}

	type D3Link struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Value  int    `json:"value"`
	}

	type D3Graph struct {
		Nodes []D3Node `json:"nodes"`
		Links []D3Link `json:"links"`
	}

	graph := D3Graph{
		Nodes: make([]D3Node, 0),
		Links: make([]D3Link, 0),
	}

	// Add target node
	graph.Nodes = append(graph.Nodes, D3Node{
		ID:       br.Target,
		Name:     extractSymbolName(br.Target),
		Package:  extractPackage(br.Target),
		Group:    0,
		IsTarget: true,
	})

	// Check if any security paths are active
	hasSecurityPath := len(br.SecurityPaths) > 0 && br.SecurityPaths[0].IsSecuritySensitive

	// Add caller nodes
	for i, caller := range br.DirectCallers {
		if i >= g.options.MaxNodes {
			break
		}

		graph.Nodes = append(graph.Nodes, D3Node{
			ID:       caller.ID,
			Name:     extractSymbolName(caller.ID),
			Package:  extractPackage(caller.ID),
			Group:    caller.Hops,
			Security: hasSecurityPath,
			Distance: caller.Hops,
		})

		graph.Links = append(graph.Links, D3Link{
			Source: caller.ID,
			Target: br.Target,
			Value:  1,
		})
	}

	data, err := json.MarshalIndent(graph, "", "  ")
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// generateSVG creates an SVG image of the graph.
func (g *GraphGenerator) generateSVG(br *analysis.EnhancedBlastRadius) string {
	// Simple SVG generation without external libraries
	var sb strings.Builder

	// SVG header
	width := 800
	height := 600
	sb.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d">`, width, height))
	sb.WriteString("\n")

	// Styles
	sb.WriteString(`<style>
    .node { cursor: pointer; }
    .node-target { fill: #ff6b6b; stroke: #333; stroke-width: 2px; }
    .node-caller { fill: #74b9ff; stroke: #333; stroke-width: 1px; }
    .node-security { fill: #ffd93d; stroke: #333; stroke-width: 2px; }
    .link { stroke: #999; stroke-opacity: 0.6; fill: none; }
    .label { font-family: Arial, sans-serif; font-size: 12px; fill: #333; }
  </style>
`)

	// Calculate positions
	centerX := width / 2
	centerY := height / 2
	targetRadius := 30

	// Draw target node at center
	targetName := extractSymbolName(br.Target)
	sb.WriteString(fmt.Sprintf(`  <circle class="node node-target" cx="%d" cy="%d" r="%d"/>`, centerX, centerY, targetRadius))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf(`  <text class="label" x="%d" y="%d" text-anchor="middle" dy=".3em">%s</text>`,
		centerX, centerY, html.EscapeString(truncateLabel(targetName, 15))))
	sb.WriteString("\n")

	// Draw callers in a circle around target
	numCallers := len(br.DirectCallers)
	if numCallers > g.options.MaxNodes {
		numCallers = g.options.MaxNodes
	}

	radius := 200.0
	callerRadius := 20

	// Check if any security paths are active
	hasSecurityPath := len(br.SecurityPaths) > 0 && br.SecurityPaths[0].IsSecuritySensitive

	for i := 0; i < numCallers; i++ {
		caller := br.DirectCallers[i]
		angle := float64(i) * (6.28318 / float64(numCallers))
		x := centerX + int(radius*cos(angle))
		y := centerY + int(radius*sin(angle))

		// Draw link
		sb.WriteString(fmt.Sprintf(`  <line class="link" x1="%d" y1="%d" x2="%d" y2="%d"/>`, x, y, centerX, centerY))
		sb.WriteString("\n")

		// Draw caller node
		nodeClass := "node-caller"
		if hasSecurityPath {
			nodeClass = "node-security"
		}
		sb.WriteString(fmt.Sprintf(`  <circle class="node %s" cx="%d" cy="%d" r="%d"/>`, nodeClass, x, y, callerRadius))
		sb.WriteString("\n")

		// Draw label
		callerName := extractSymbolName(caller.ID)
		sb.WriteString(fmt.Sprintf(`  <text class="label" x="%d" y="%d" text-anchor="middle" dy=".3em">%s</text>`,
			x, y+callerRadius+15, html.EscapeString(truncateLabel(callerName, 10))))
		sb.WriteString("\n")
	}

	// Overflow indicator
	if len(br.DirectCallers) > g.options.MaxNodes {
		sb.WriteString(fmt.Sprintf(`  <text class="label" x="%d" y="%d" text-anchor="middle">+%d more</text>`,
			centerX, height-20, len(br.DirectCallers)-g.options.MaxNodes))
		sb.WriteString("\n")
	}

	sb.WriteString("</svg>")
	return sb.String()
}

// generateDOT creates a Graphviz DOT format graph.
func (g *GraphGenerator) generateDOT(br *analysis.EnhancedBlastRadius) string {
	var sb strings.Builder

	sb.WriteString("digraph BlastRadius {\n")
	sb.WriteString("    rankdir=TB;\n")
	sb.WriteString("    node [shape=box, style=filled];\n")
	sb.WriteString("\n")

	// Target node
	targetID := sanitizeDOTID(br.Target)
	targetName := extractSymbolName(br.Target)
	sb.WriteString(fmt.Sprintf("    %s [label=\"%s\", fillcolor=\"#ff6b6b\", fontcolor=\"white\"];\n",
		targetID, escapeDOTLabel(targetName)))

	// Check if any security paths are active
	hasSecurityPath := len(br.SecurityPaths) > 0 && br.SecurityPaths[0].IsSecuritySensitive

	// Caller nodes
	for i, caller := range br.DirectCallers {
		if i >= g.options.MaxNodes {
			sb.WriteString(fmt.Sprintf("    overflow [label=\"+%d more\", shape=plaintext];\n", len(br.DirectCallers)-i))
			break
		}

		callerID := sanitizeDOTID(caller.ID)
		callerName := extractSymbolName(caller.ID)

		color := "#74b9ff"
		if hasSecurityPath {
			color = "#ffd93d"
		}

		sb.WriteString(fmt.Sprintf("    %s [label=\"%s\", fillcolor=\"%s\"];\n",
			callerID, escapeDOTLabel(callerName), color))
	}

	sb.WriteString("\n")

	// Edges
	for i, caller := range br.DirectCallers {
		if i >= g.options.MaxNodes {
			break
		}
		callerID := sanitizeDOTID(caller.ID)
		sb.WriteString(fmt.Sprintf("    %s -> %s;\n", callerID, targetID))
	}

	sb.WriteString("}\n")
	return sb.String()
}

// Helper functions

func sanitizeMermaidID(s string) string {
	// Replace special characters
	replacer := strings.NewReplacer(
		":", "_",
		"/", "_",
		".", "_",
		"-", "_",
		" ", "_",
		"(", "",
		")", "",
		"*", "ptr_",
	)
	result := replacer.Replace(s)
	// Ensure starts with letter
	if len(result) > 0 && (result[0] >= '0' && result[0] <= '9') {
		result = "n" + result
	}
	return result
}

func sanitizeDOTID(s string) string {
	// DOT IDs can be quoted
	return fmt.Sprintf("\"%s\"", strings.ReplaceAll(s, "\"", "\\\""))
}

func escapeMermaidLabel(s string) string {
	replacer := strings.NewReplacer(
		"\"", "#quot;",
		"<", "&lt;",
		">", "&gt;",
	)
	return replacer.Replace(s)
}

func escapeDOTLabel(s string) string {
	replacer := strings.NewReplacer(
		"\"", "\\\"",
		"\n", "\\n",
	)
	return replacer.Replace(s)
}

func extractSymbolName(symbolID string) string {
	// Symbol ID format: path/to/file.go:FunctionName
	if idx := strings.LastIndex(symbolID, ":"); idx > 0 {
		return symbolID[idx+1:]
	}
	return symbolID
}

func extractPackage(symbolID string) string {
	// Extract package from path
	if idx := strings.LastIndex(symbolID, ":"); idx > 0 {
		path := symbolID[:idx]
		if slashIdx := strings.LastIndex(path, "/"); slashIdx > 0 {
			return path[slashIdx+1:]
		}
		return path
	}
	return "unknown"
}

func truncateLabel(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// Simple math functions to avoid importing math package for just these
func cos(x float64) float64 {
	// Taylor series approximation for cos
	x = normalizeAngle(x)
	result := 1.0
	term := 1.0
	for i := 1; i <= 10; i++ {
		term *= -x * x / float64((2*i-1)*(2*i))
		result += term
	}
	return result
}

func sin(x float64) float64 {
	// Taylor series approximation for sin
	x = normalizeAngle(x)
	result := x
	term := x
	for i := 1; i <= 10; i++ {
		term *= -x * x / float64((2*i)*(2*i+1))
		result += term
	}
	return result
}

func normalizeAngle(x float64) float64 {
	const twoPi = 6.283185307179586
	for x > twoPi {
		x -= twoPi
	}
	for x < 0 {
		x += twoPi
	}
	return x
}

// GraphHTMLTemplate returns an HTML template for interactive D3 visualization.
func GraphHTMLTemplate(d3JSON string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Blast Radius Visualization</title>
  <script src="https://d3js.org/d3.v7.min.js"></script>
  <style>
    body { margin: 0; font-family: Arial, sans-serif; }
    svg { width: 100%%; height: 100vh; }
    .node circle { stroke: #333; stroke-width: 1.5px; }
    .node text { font-size: 12px; }
    .link { stroke: #999; stroke-opacity: 0.6; }
    .node.target circle { fill: #ff6b6b; }
    .node.security circle { fill: #ffd93d; }
    .tooltip {
      position: absolute;
      background: #333;
      color: #fff;
      padding: 8px;
      border-radius: 4px;
      font-size: 12px;
    }
  </style>
</head>
<body>
  <svg></svg>
  <script>
    const data = %s;

    const width = window.innerWidth;
    const height = window.innerHeight;

    const svg = d3.select("svg");

    const simulation = d3.forceSimulation(data.nodes)
      .force("link", d3.forceLink(data.links).id(d => d.id).distance(100))
      .force("charge", d3.forceManyBody().strength(-200))
      .force("center", d3.forceCenter(width / 2, height / 2));

    const link = svg.append("g")
      .selectAll("line")
      .data(data.links)
      .join("line")
      .attr("class", "link");

    const node = svg.append("g")
      .selectAll("g")
      .data(data.nodes)
      .join("g")
      .attr("class", d => "node" + (d.isTarget ? " target" : "") + (d.security ? " security" : ""))
      .call(d3.drag()
        .on("start", dragstarted)
        .on("drag", dragged)
        .on("end", dragended));

    node.append("circle")
      .attr("r", d => d.isTarget ? 20 : 10)
      .attr("fill", d => {
        if (d.isTarget) return "#ff6b6b";
        if (d.security) return "#ffd93d";
        return d3.schemeCategory10[d.group %% 10];
      });

    node.append("text")
      .attr("dx", 15)
      .attr("dy", 4)
      .text(d => d.name);

    simulation.on("tick", () => {
      link
        .attr("x1", d => d.source.x)
        .attr("y1", d => d.source.y)
        .attr("x2", d => d.target.x)
        .attr("y2", d => d.target.y);

      node.attr("transform", d => "translate(" + d.x + "," + d.y + ")");
    });

    function dragstarted(event) {
      if (!event.active) simulation.alphaTarget(0.3).restart();
      event.subject.fx = event.subject.x;
      event.subject.fy = event.subject.y;
    }

    function dragged(event) {
      event.subject.fx = event.x;
      event.subject.fy = event.y;
    }

    function dragended(event) {
      if (!event.active) simulation.alphaTarget(0);
      event.subject.fx = null;
      event.subject.fy = null;
    }
  </script>
</body>
</html>`, d3JSON)
}

// InteractiveVisualization generates a complete interactive HTML visualization.
func (g *GraphGenerator) InteractiveVisualization(ctx context.Context, br *analysis.EnhancedBlastRadius) (string, error) {
	d3JSON, err := g.Generate(ctx, br, FormatD3)
	if err != nil {
		return "", err
	}
	return GraphHTMLTemplate(d3JSON), nil
}

// MultiBlastRadiusVisualization creates a visualization for multiple blast radii.
func (g *GraphGenerator) MultiBlastRadiusVisualization(ctx context.Context, results []*analysis.EnhancedBlastRadius, format OutputFormat) (string, error) {
	if len(results) == 0 {
		return "", fmt.Errorf("no blast radius results provided")
	}

	// For mermaid, combine into a single diagram
	if format == FormatMermaid {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("flowchart %s\n", g.options.Direction))

		// Deduplicate nodes across all results
		nodeSet := make(map[string]bool)
		edgeSet := make(map[string]bool)

		for _, br := range results {
			// Add target
			targetID := sanitizeMermaidID(br.Target)
			targetName := extractSymbolName(br.Target)
			if !nodeSet[targetID] {
				sb.WriteString(fmt.Sprintf("    %s[[\"%s\"]]:::target\n", targetID, escapeMermaidLabel(targetName)))
				nodeSet[targetID] = true
			}

			// Add callers
			maxPerResult := g.options.MaxNodes / len(results)
			for i, caller := range br.DirectCallers {
				if i >= maxPerResult {
					break
				}

				callerID := sanitizeMermaidID(caller.ID)
				callerName := extractSymbolName(caller.ID)
				if !nodeSet[callerID] {
					style := g.getCallerStyle(caller, br)
					sb.WriteString(fmt.Sprintf("    %s[\"%s\"]%s\n", callerID, escapeMermaidLabel(callerName), style))
					nodeSet[callerID] = true
				}

				edgeKey := callerID + "->" + targetID
				if !edgeSet[edgeKey] {
					sb.WriteString(fmt.Sprintf("    %s --> %s\n", callerID, targetID))
					edgeSet[edgeKey] = true
				}
			}
		}

		sb.WriteString("\n")
		sb.WriteString("    classDef target fill:#ff6b6b,stroke:#333,stroke-width:2px,color:#fff\n")
		sb.WriteString("    classDef security fill:#ffd93d,stroke:#333,stroke-width:2px\n")
		sb.WriteString("    classDef high fill:#ff9f43,stroke:#333\n")
		sb.WriteString("    classDef low fill:#10ac84,stroke:#333\n")

		return sb.String(), nil
	}

	// For other formats, just use the first result
	return g.Generate(ctx, results[0], format)
}

// PackageHierarchyVisualization creates a package-level visualization.
func (g *GraphGenerator) PackageHierarchyVisualization(ctx context.Context, br *analysis.EnhancedBlastRadius) (string, error) {
	var sb strings.Builder
	sb.WriteString("flowchart TB\n")

	// Group by package
	packages := g.groupByPackage(br.DirectCallers)

	// Sort packages
	pkgNames := make([]string, 0, len(packages))
	for pkg := range packages {
		pkgNames = append(pkgNames, pkg)
	}
	sort.Strings(pkgNames)

	targetPkg := extractPackage(br.Target)
	targetPkgID := sanitizeMermaidID("pkg_" + targetPkg)
	sb.WriteString(fmt.Sprintf("    %s[[\"%s\\n(target)\"]]:::target\n", targetPkgID, escapeMermaidLabel(targetPkg)))

	for _, pkg := range pkgNames {
		if pkg == targetPkg {
			continue
		}
		callers := packages[pkg]
		pkgID := sanitizeMermaidID("pkg_" + pkg)
		sb.WriteString(fmt.Sprintf("    %s[\"%s\\n(%d callers)\"]:::caller\n",
			pkgID, escapeMermaidLabel(pkg), len(callers)))
		sb.WriteString(fmt.Sprintf("    %s --> %s\n", pkgID, targetPkgID))
	}

	sb.WriteString("\n")
	sb.WriteString("    classDef target fill:#ff6b6b,stroke:#333,stroke-width:2px,color:#fff\n")
	sb.WriteString("    classDef caller fill:#74b9ff,stroke:#333\n")

	return sb.String(), nil
}
