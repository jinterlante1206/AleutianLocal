// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package mcts

import "errors"

// Sentinel errors for the mcts package.
var (
	// Budget errors
	ErrBudgetExhausted      = errors.New("mcts budget exhausted")
	ErrTimeLimitExceeded    = errors.New("mcts time limit exceeded")
	ErrNodeLimitExceeded    = errors.New("mcts node limit exceeded")
	ErrDepthLimitExceeded   = errors.New("mcts depth limit exceeded")
	ErrLLMCallLimitExceeded = errors.New("mcts LLM call limit exceeded")
	ErrCostLimitExceeded    = errors.New("mcts cost limit exceeded")

	// Validation errors
	ErrPathOutsideBoundary = errors.New("file path is outside project boundary")
	ErrInvalidActionType   = errors.New("invalid action type")
	ErrInvalidUTF8         = errors.New("code diff contains invalid UTF-8")
	ErrUnsafePath          = errors.New("file path contains unsafe characters")
	ErrEmptyDescription    = errors.New("action description is empty")
	ErrPathTooLong         = errors.New("file path exceeds maximum length")
	ErrCodeDiffTooLarge    = errors.New("code diff exceeds maximum size")
	ErrActionNotValidated  = errors.New("action used without validation")

	// Circuit breaker errors
	ErrCircuitOpen = errors.New("circuit breaker is open")

	// Tree errors
	ErrNoValidPath        = errors.New("no valid path found in tree")
	ErrTreeNotInitialized = errors.New("plan tree not initialized")
	ErrNodeAbandoned      = errors.New("node has been abandoned")
)
