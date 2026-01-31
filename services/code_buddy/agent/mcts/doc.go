// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package mcts implements Monte Carlo Tree Search-inspired plan tree reasoning.
//
// This package provides MCTS-based exploration of code change plans, enabling
// the agent to explore multiple approaches before committing to a strategy.
//
// # Architecture
//
// The package consists of several key components:
//
//   - PlanTree: Root structure containing the exploration tree
//   - PlanNode: Individual node representing a plan step or alternative
//   - PlannedAction: Validated action to be executed (edit, create, delete)
//   - TreeBudget: Resource limits (nodes, depth, tokens, time, cost)
//   - MCTSController: Orchestrates the MCTS loop (select, expand, simulate, backpropagate)
//   - Simulator: Evaluates plan nodes via tiered simulation
//
// # MCTS Phases
//
// 1. SELECT: Pick promising node using UCB1 formula
// 2. EXPAND: Generate alternative approaches via LLM
// 3. SIMULATE: Evaluate node quality (syntax, lint, tests, blast radius)
// 4. BACKPROPAGATE: Update scores up the tree
//
// # Thread Safety
//
// All exported types are safe for concurrent use unless documented otherwise.
// PlanNode uses atomic operations for visit/score updates, and mutex for
// structural modifications.
//
// # Budget Limits
//
// TreeBudget enforces multiple limits:
//   - MaxNodes: Maximum nodes to explore
//   - MaxDepth: Maximum plan depth
//   - TimeLimit: Wall clock limit
//   - LLMCallLimit: Maximum LLM calls
//   - CostLimitUSD: Maximum LLM cost
//
// # Security
//
// PlannedAction validation provides:
//   - Path traversal protection
//   - Shell metacharacter detection
//   - UTF-8 validation
//   - Project boundary enforcement
//
// # Observability
//
// The package integrates with OpenTelemetry for:
//   - Distributed tracing of MCTS phases
//   - Prometheus metrics for cost/latency/success
//   - Structured logging with slog
package mcts
