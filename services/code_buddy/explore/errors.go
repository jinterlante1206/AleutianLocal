// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package explore

import "errors"

// Sentinel errors for the explore package.
var (
	// ErrSymbolNotFound indicates the requested symbol was not found.
	ErrSymbolNotFound = errors.New("symbol not found")

	// ErrFileNotFound indicates the requested file was not found.
	ErrFileNotFound = errors.New("file not found")

	// ErrPackageNotFound indicates the requested package was not found.
	ErrPackageNotFound = errors.New("package not found")

	// ErrInvalidInput indicates invalid input was provided.
	ErrInvalidInput = errors.New("invalid input")

	// ErrTraversalLimitReached indicates traversal exceeded configured limits.
	ErrTraversalLimitReached = errors.New("traversal limit reached")

	// ErrTokenBudgetExceeded indicates the token budget was exceeded.
	ErrTokenBudgetExceeded = errors.New("token budget exceeded")

	// ErrGraphNotReady indicates the graph is not ready for queries.
	ErrGraphNotReady = errors.New("graph not ready")

	// ErrContextCanceled indicates the operation was canceled.
	ErrContextCanceled = errors.New("context canceled")

	// ErrUnsupportedLanguage indicates the language is not supported.
	ErrUnsupportedLanguage = errors.New("unsupported language")

	// ErrNoEntryPoints indicates no entry points were found.
	ErrNoEntryPoints = errors.New("no entry points found")

	// ErrCacheMiss indicates the requested data is not in cache.
	ErrCacheMiss = errors.New("cache miss")
)
