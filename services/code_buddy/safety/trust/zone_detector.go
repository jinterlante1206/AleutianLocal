// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package trust

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/safety"
)

// ZoneDetector automatically identifies trust zones in a codebase.
//
// Description:
//
//	ZoneDetector analyzes code structure to identify regions with
//	different trust levels. It uses path patterns, function names,
//	and receiver types to classify code into zones.
//
// Thread Safety:
//
//	ZoneDetector is safe for concurrent use after initialization.
type ZoneDetector struct {
	patterns *ZonePatterns
}

// NewZoneDetector creates a new ZoneDetector with default patterns.
func NewZoneDetector() *ZoneDetector {
	return &ZoneDetector{
		patterns: DefaultZonePatterns(),
	}
}

// NewZoneDetectorWithPatterns creates a ZoneDetector with custom patterns.
func NewZoneDetectorWithPatterns(patterns *ZonePatterns) *ZoneDetector {
	return &ZoneDetector{
		patterns: patterns,
	}
}

// DetectZones analyzes a graph and identifies trust zones.
//
// Description:
//
//	Scans all nodes in the graph and groups them into trust zones
//	based on file paths, function names, and receiver types.
//
// Inputs:
//
//	g - The code graph to analyze. Must be frozen.
//	scope - Package or path prefix to limit analysis.
//
// Outputs:
//
//	[]safety.TrustZone - The detected trust zones.
func (d *ZoneDetector) DetectZones(g *graph.Graph, scope string) []safety.TrustZone {
	// Collect nodes by zone
	zoneNodes := make(map[safety.TrustLevel]map[string][]*graph.Node)
	for level := safety.TrustExternal; level <= safety.TrustPrivileged; level++ {
		zoneNodes[level] = make(map[string][]*graph.Node)
	}

	// Process nodes in parallel
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Collect nodes from iterator into slice for batching
	var allNodes []*graph.Node
	for _, node := range g.Nodes() {
		allNodes = append(allNodes, node)
	}

	batchSize := 100
	for i := 0; i < len(allNodes); i += batchSize {
		end := i + batchSize
		if end > len(allNodes) {
			end = len(allNodes)
		}

		wg.Add(1)
		go func(batch []*graph.Node) {
			defer wg.Done()

			localZones := make(map[safety.TrustLevel]map[string][]*graph.Node)
			for level := safety.TrustExternal; level <= safety.TrustPrivileged; level++ {
				localZones[level] = make(map[string][]*graph.Node)
			}

			for _, node := range batch {
				if node.Symbol == nil {
					continue
				}

				// Check scope
				if scope != "" {
					if !strings.HasPrefix(node.Symbol.FilePath, scope) &&
						!strings.HasPrefix(node.Symbol.Package, scope) {
						continue
					}
				}

				// Determine zone
				level, zoneName := d.classifyNode(node)

				localZones[level][zoneName] = append(localZones[level][zoneName], node)
			}

			// Merge into global
			mu.Lock()
			for level, zoneMap := range localZones {
				for zoneName, nodes := range zoneMap {
					zoneNodes[level][zoneName] = append(zoneNodes[level][zoneName], nodes...)
				}
			}
			mu.Unlock()
		}(allNodes[i:end])
	}
	wg.Wait()

	// Build zone objects
	var zones []safety.TrustZone

	for level := safety.TrustExternal; level <= safety.TrustPrivileged; level++ {
		for zoneName, nodes := range zoneNodes[level] {
			if len(nodes) == 0 {
				continue
			}

			zone := d.buildZone(g, level, zoneName, nodes)
			zones = append(zones, zone)
		}
	}

	// Sort zones by level (untrusted first), then by name
	sort.Slice(zones, func(i, j int) bool {
		if zones[i].Level != zones[j].Level {
			return zones[i].Level < zones[j].Level
		}
		return zones[i].Name < zones[j].Name
	})

	return zones
}

// classifyNode determines the trust level and zone name for a node.
func (d *ZoneDetector) classifyNode(node *graph.Node) (safety.TrustLevel, string) {
	if node.Symbol == nil {
		return safety.TrustInternal, "unknown"
	}

	// Priority: 1. Path patterns, 2. Function patterns, 3. Receiver patterns

	// Check path patterns first
	if level, matched := d.patterns.MatchPath(node.Symbol.FilePath); matched {
		zoneName := d.extractZoneName(node.Symbol.FilePath, level)
		return level, zoneName
	}

	// Check function patterns
	if level, matched := d.patterns.MatchFunction(node.Symbol.Name); matched {
		zoneName := d.extractZoneNameFromPackage(node.Symbol.Package, level)
		return level, zoneName
	}

	// Check receiver patterns
	if node.Symbol.Receiver != "" {
		if level, matched := d.patterns.MatchReceiver(node.Symbol.Receiver); matched {
			zoneName := d.extractZoneNameFromPackage(node.Symbol.Package, level)
			return level, zoneName
		}
	}

	// Default: classify by package
	zoneName := d.extractZoneNameFromPackage(node.Symbol.Package, safety.TrustInternal)
	return safety.TrustInternal, zoneName
}

// extractZoneName extracts a meaningful zone name from a file path.
func (d *ZoneDetector) extractZoneName(path string, level safety.TrustLevel) string {
	// Get directory containing the file
	dir := filepath.Dir(path)

	// Try to find the most specific matching segment
	segments := strings.Split(dir, string(filepath.Separator))

	// Look for a segment matching the level patterns
	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		if seg == "" || seg == "." {
			continue
		}

		// Check if this segment matches the expected level
		testPath := seg + "/"
		if matchedLevel, matched := d.patterns.MatchPath(testPath); matched && matchedLevel == level {
			// Use the segment and potentially one more for context
			if i > 0 && segments[i-1] != "" && segments[i-1] != "." {
				return segments[i-1] + "_" + seg
			}
			return seg
		}
	}

	// Fallback: use last two directory segments
	if len(segments) >= 2 {
		last := segments[len(segments)-1]
		prev := segments[len(segments)-2]
		if last != "" && prev != "" {
			return prev + "_" + last
		}
	}

	if len(segments) >= 1 && segments[len(segments)-1] != "" {
		return segments[len(segments)-1]
	}

	return "default"
}

// extractZoneNameFromPackage extracts a zone name from a package path.
func (d *ZoneDetector) extractZoneNameFromPackage(pkg string, level safety.TrustLevel) string {
	if pkg == "" {
		return "default"
	}

	// Split package path
	parts := strings.Split(pkg, "/")
	if len(parts) == 0 {
		return "default"
	}

	// Use last 1-2 parts
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "_" + parts[len(parts)-1]
	}
	return parts[len(parts)-1]
}

// buildZone creates a TrustZone from a set of nodes.
func (d *ZoneDetector) buildZone(g *graph.Graph, level safety.TrustLevel, name string, nodes []*graph.Node) safety.TrustZone {
	zone := safety.TrustZone{
		ID:          GenerateZoneID(level, name),
		Name:        name,
		Level:       level,
		EntryPoints: make([]string, 0),
		ExitPoints:  make([]string, 0),
		Files:       make([]string, 0),
	}

	filesMap := make(map[string]bool)
	entryPointsMap := make(map[string]bool)
	exitPointsMap := make(map[string]bool)

	for _, node := range nodes {
		if node.Symbol == nil {
			continue
		}

		// Collect files
		if node.Symbol.FilePath != "" {
			filesMap[node.Symbol.FilePath] = true
		}

		// Identify entry points (functions that receive external calls)
		if len(node.Incoming) > 0 {
			// Check if any incoming edge is from a different zone
			for _, edge := range node.Incoming {
				fromNode, exists := g.GetNode(edge.FromID)
				if !exists || fromNode.Symbol == nil {
					continue
				}

				fromLevel, _ := d.classifyNode(fromNode)
				if fromLevel != level {
					// This is an entry point
					entryPointsMap[node.ID] = true
					break
				}
			}
		}

		// Identify exit points (functions that call into other zones)
		if len(node.Outgoing) > 0 {
			for _, edge := range node.Outgoing {
				toNode, exists := g.GetNode(edge.ToID)
				if !exists || toNode.Symbol == nil {
					continue
				}

				toLevel, _ := d.classifyNode(toNode)
				if toLevel != level {
					// This is an exit point
					exitPointsMap[node.ID] = true
					break
				}
			}
		}
	}

	// Convert maps to slices
	for file := range filesMap {
		zone.Files = append(zone.Files, file)
	}
	for ep := range entryPointsMap {
		zone.EntryPoints = append(zone.EntryPoints, ep)
	}
	for ep := range exitPointsMap {
		zone.ExitPoints = append(zone.ExitPoints, ep)
	}

	// Sort for consistent output
	sort.Strings(zone.Files)
	sort.Strings(zone.EntryPoints)
	sort.Strings(zone.ExitPoints)

	return zone
}

// FindZoneForNode returns the zone containing a specific node.
func (d *ZoneDetector) FindZoneForNode(node *graph.Node, zones []safety.TrustZone) *safety.TrustZone {
	if node == nil || node.Symbol == nil {
		return nil
	}

	level, zoneName := d.classifyNode(node)
	zoneID := GenerateZoneID(level, zoneName)

	for i := range zones {
		if zones[i].ID == zoneID {
			return &zones[i]
		}
	}

	return nil
}

// GetZoneByID finds a zone by its ID.
func GetZoneByID(zones []safety.TrustZone, id string) *safety.TrustZone {
	for i := range zones {
		if zones[i].ID == id {
			return &zones[i]
		}
	}
	return nil
}
