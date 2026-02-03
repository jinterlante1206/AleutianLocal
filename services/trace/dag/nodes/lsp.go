// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package nodes

import (
	"context"
	"fmt"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/dag"
	"github.com/AleutianAI/AleutianFOSS/services/trace/lsp"
)

// LSPSpawnNode spawns language servers for the requested languages.
//
// Description:
//
//	Uses the LSP Manager to spawn language servers for the specified
//	languages. Servers are spawned on-demand and cached for reuse.
//
// Inputs (from map[string]any):
//
//	"languages" ([]string): Languages to spawn servers for. Required.
//	    E.g., ["go", "python", "typescript"]
//
// Outputs:
//
//	*LSPSpawnOutput containing:
//	  - Spawned: Languages with successfully spawned servers
//	  - Failed: Languages that failed to spawn
//	  - Manager: The LSP manager for downstream use
//	  - Duration: Spawn time
//
// Thread Safety:
//
//	Safe for concurrent use.
type LSPSpawnNode struct {
	dag.BaseNode
	manager *lsp.Manager
}

// LSPSpawnOutput contains the result of spawning LSP servers.
type LSPSpawnOutput struct {
	// Spawned contains languages with active servers.
	Spawned []string

	// Failed contains languages that failed to spawn.
	Failed []LSPSpawnError

	// Manager is the LSP manager for downstream use.
	Manager *lsp.Manager

	// Operations is the LSP operations wrapper.
	Operations *lsp.Operations

	// Duration is the spawn time.
	Duration time.Duration
}

// LSPSpawnError represents a language server spawn failure.
type LSPSpawnError struct {
	Language string
	Error    string
}

// NewLSPSpawnNode creates a new LSP spawn node.
//
// Inputs:
//
//	manager - The LSP manager to use. Must not be nil.
//	deps - Names of nodes this node depends on.
//
// Outputs:
//
//	*LSPSpawnNode - The configured node.
func NewLSPSpawnNode(manager *lsp.Manager, deps []string) *LSPSpawnNode {
	return &LSPSpawnNode{
		BaseNode: dag.BaseNode{
			NodeName:         "LSP_SPAWN",
			NodeDependencies: deps,
			NodeTimeout:      1 * time.Minute,
			NodeRetryable:    true,
		},
		manager: manager,
	}
}

// Execute spawns the requested language servers.
//
// Description:
//
//	Spawns language servers for each requested language. Failures for
//	individual languages are reported but don't fail the entire operation.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	inputs - Map containing "languages".
//
// Outputs:
//
//	*LSPSpawnOutput - The spawn result.
//	error - Non-nil only if manager is nil.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (n *LSPSpawnNode) Execute(ctx context.Context, inputs map[string]any) (any, error) {
	if n.manager == nil {
		return nil, fmt.Errorf("%w: LSP manager", ErrNilDependency)
	}

	// Extract inputs
	languages, err := n.extractInputs(inputs)
	if err != nil {
		return nil, err
	}

	start := time.Now()

	spawned := make([]string, 0, len(languages))
	failed := make([]LSPSpawnError, 0)

	for _, lang := range languages {
		_, err := n.manager.GetOrSpawn(ctx, lang)
		if err != nil {
			failed = append(failed, LSPSpawnError{
				Language: lang,
				Error:    err.Error(),
			})
		} else {
			spawned = append(spawned, lang)
		}
	}

	return &LSPSpawnOutput{
		Spawned:    spawned,
		Failed:     failed,
		Manager:    n.manager,
		Operations: lsp.NewOperations(n.manager),
		Duration:   time.Since(start),
	}, nil
}

// extractInputs validates and extracts inputs from the map.
func (n *LSPSpawnNode) extractInputs(inputs map[string]any) ([]string, error) {
	langsRaw, ok := inputs["languages"]
	if !ok {
		return nil, fmt.Errorf("%w: languages", ErrMissingInput)
	}

	if langs, ok := langsRaw.([]string); ok {
		return langs, nil
	}

	// Handle []any
	if langsAny, ok := langsRaw.([]any); ok {
		langs := make([]string, len(langsAny))
		for i, l := range langsAny {
			s, ok := l.(string)
			if !ok {
				return nil, fmt.Errorf("%w: languages[%d] must be string", ErrInvalidInputType, i)
			}
			langs[i] = s
		}
		return langs, nil
	}

	return nil, fmt.Errorf("%w: languages must be []string", ErrInvalidInputType)
}

// LSPTypeCheckNode performs type checking using LSP hover.
//
// Description:
//
//	Uses the LSP hover operation to retrieve type information for
//	specified symbols. This provides accurate type resolution across
//	the project.
//
// Inputs (from map[string]any):
//
//	"operations" (*lsp.Operations): LSP operations from LSP_SPAWN. Required.
//	"symbols" ([]SymbolLocation): Symbols to type check. Required.
//
// Outputs:
//
//	*LSPTypeCheckOutput containing:
//	  - Results: Type information for each symbol
//	  - Errors: Symbols that failed type checking
//	  - Duration: Check time
//
// Thread Safety:
//
//	Safe for concurrent use.
type LSPTypeCheckNode struct {
	dag.BaseNode
}

// SymbolLocation identifies a symbol for type checking.
type SymbolLocation struct {
	FilePath string
	Line     int // 1-indexed
	Column   int // 0-indexed
}

// LSPTypeCheckOutput contains the result of type checking.
type LSPTypeCheckOutput struct {
	// Results contains type information for checked symbols.
	Results []TypeCheckResult

	// Errors contains symbols that failed type checking.
	Errors []TypeCheckError

	// Duration is the check time.
	Duration time.Duration
}

// TypeCheckResult contains type information for a symbol.
type TypeCheckResult struct {
	Symbol   SymbolLocation
	TypeInfo string
	Kind     string // "plaintext" or "markdown"
}

// TypeCheckError represents a type check failure.
type TypeCheckError struct {
	Symbol SymbolLocation
	Error  string
}

// NewLSPTypeCheckNode creates a new LSP type check node.
//
// Inputs:
//
//	deps - Names of nodes this node depends on.
//
// Outputs:
//
//	*LSPTypeCheckNode - The configured node.
func NewLSPTypeCheckNode(deps []string) *LSPTypeCheckNode {
	return &LSPTypeCheckNode{
		BaseNode: dag.BaseNode{
			NodeName:         "LSP_TYPE_CHECK",
			NodeDependencies: deps,
			NodeTimeout:      2 * time.Minute,
			NodeRetryable:    true,
		},
	}
}

// Execute performs type checking on the specified symbols.
//
// Description:
//
//	Uses LSP hover to retrieve type information for each symbol.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	inputs - Map containing "operations" and "symbols".
//
// Outputs:
//
//	*LSPTypeCheckOutput - The type check result.
//	error - Non-nil if operations is nil.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (n *LSPTypeCheckNode) Execute(ctx context.Context, inputs map[string]any) (any, error) {
	// Extract inputs
	ops, symbols, err := n.extractInputs(inputs)
	if err != nil {
		return nil, err
	}

	start := time.Now()

	results := make([]TypeCheckResult, 0, len(symbols))
	errors := make([]TypeCheckError, 0)

	for _, sym := range symbols {
		info, err := ops.Hover(ctx, sym.FilePath, sym.Line, sym.Column)
		if err != nil {
			errors = append(errors, TypeCheckError{
				Symbol: sym,
				Error:  err.Error(),
			})
			continue
		}

		if info != nil {
			results = append(results, TypeCheckResult{
				Symbol:   sym,
				TypeInfo: info.Content,
				Kind:     info.Kind,
			})
		}
	}

	return &LSPTypeCheckOutput{
		Results:  results,
		Errors:   errors,
		Duration: time.Since(start),
	}, nil
}

// extractInputs validates and extracts inputs from the map.
func (n *LSPTypeCheckNode) extractInputs(inputs map[string]any) (*lsp.Operations, []SymbolLocation, error) {
	// Extract operations
	opsRaw, ok := inputs["operations"]
	if !ok {
		// Try to get from LSP_SPAWN output
		if spawnOutput, ok := inputs["LSP_SPAWN"]; ok {
			if output, ok := spawnOutput.(*LSPSpawnOutput); ok && output.Operations != nil {
				opsRaw = output.Operations
			}
		}
		if opsRaw == nil {
			return nil, nil, fmt.Errorf("%w: operations", ErrMissingInput)
		}
	}

	ops, ok := opsRaw.(*lsp.Operations)
	if !ok {
		return nil, nil, fmt.Errorf("%w: operations must be *lsp.Operations", ErrInvalidInputType)
	}

	// Extract symbols
	symbolsRaw, ok := inputs["symbols"]
	if !ok {
		return ops, nil, nil // No symbols to check is OK
	}

	if symbols, ok := symbolsRaw.([]SymbolLocation); ok {
		return ops, symbols, nil
	}

	return nil, nil, fmt.Errorf("%w: symbols must be []SymbolLocation", ErrInvalidInputType)
}
