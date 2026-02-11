// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package tools provides CLI tools for graph queries.
//
// Tool implementations have been refactored into individual files:
//   - tool_find_callers.go: find_callers tool
//   - tool_find_callees.go: find_callees tool
//   - tool_find_implementations.go: find_implementations tool
//   - tool_find_symbol.go: find_symbol tool
//   - tool_get_call_chain.go: get_call_chain tool
//   - tool_find_references.go: find_references tool
//   - tool_find_hotspots.go: find_hotspots tool
//   - tool_find_dead_code.go: find_dead_code tool
//   - tool_find_cycles.go: find_cycles tool
//   - tool_find_path.go: find_path tool
//   - tool_find_entry_points.go: find_entry_points tool
//   - tool_find_important.go: find_important tool
//   - tool_find_communities.go: find_communities tool
//   - tool_find_similar_code.go: find_similar_code tool
//   - tool_find_config_usage.go: find_config_usage tool
//
// Shared helpers are in tool_helpers.go.
package tools
