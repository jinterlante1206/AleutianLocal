// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package lint provides integration with external linters for code validation.
//
// The lint package executes established linters (golangci-lint, ruff, eslint)
// as a validation layer for LLM-generated code. This provides:
//
//   - 500+ rules from community-maintained linters
//   - Fast execution (10-200ms depending on linter)
//   - Accurate detection of common LLM mistakes (unused vars, unchecked errors)
//   - Graceful degradation when linters are not installed
//
// # Architecture
//
// The package integrates into the validation pipeline:
//
//	Patch → Size Check → Syntax Check → LINTER CHECK → Pattern Scan → Result
//
// Linters run on the full file after applying a patch, catching issues that
// regex patterns would miss (type errors, import cycles, etc.).
//
// # Supported Linters
//
//	| Language   | Linter         | Command             |
//	|------------|----------------|---------------------|
//	| Go         | golangci-lint  | golangci-lint run   |
//	| Python     | Ruff           | ruff check          |
//	| TypeScript | ESLint         | eslint              |
//	| JavaScript | ESLint         | eslint              |
//
// # Severity Mapping
//
// Each linter's output is mapped to a standard severity:
//
//	| Linter Severity | Our Severity | Action      |
//	|-----------------|--------------|-------------|
//	| error           | Error        | Block patch |
//	| warning         | Warning      | Allow, warn |
//	| info/style      | Info         | Log only    |
//
// # Usage
//
//	runner := lint.NewLintRunner()
//
//	// Lint a file
//	result, err := runner.Lint(ctx, "path/to/file.go")
//	if err != nil {
//	    // Linter failed or unavailable
//	}
//	if !result.Valid {
//	    // File has blocking issues
//	}
//
//	// Lint content directly
//	result, err := runner.LintContent(ctx, []byte("package main..."), "go")
//
// # Thread Safety
//
// All exported types are safe for concurrent use.
package lint
