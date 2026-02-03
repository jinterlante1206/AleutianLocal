// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package ast

import (
	"context"
	"sync"
)

// Parser defines the contract for language-specific AST parsing.
//
// Description:
//
//	Parser implementations extract structured symbol information from source code.
//	Each implementation handles a specific programming language (Go, Python, TypeScript, etc.)
//	but produces output in the common ParseResult format defined in types.go.
//
//	The Parser interface is designed to be:
//	- Context-aware: Supports cancellation and timeouts via context.Context
//	- Language-agnostic: Common output format regardless of source language
//	- Error-tolerant: Partial results returned even when parse errors occur
//
// Inputs:
//
//	ctx      - Context for cancellation and timeout control. Implementations should
//	           check ctx.Done() during long-running operations.
//	content  - Raw source code bytes to parse. Must be valid UTF-8.
//	filePath - Path to the file being parsed (for error reporting and ID generation).
//	           Should be relative to project root for consistency.
//
// Outputs:
//
//	*ParseResult - Contains extracted symbols, imports, and metadata.
//	               May contain partial results even when errors occur.
//	error        - Non-nil if parsing failed completely. Partial failures
//	               are reported in ParseResult.Errors instead.
//
// Example:
//
//	parser := NewGoParser()
//	content, _ := os.ReadFile("main.go")
//	result, err := parser.Parse(ctx, content, "main.go")
//	if err != nil {
//	    return fmt.Errorf("parse failed: %w", err)
//	}
//	for _, symbol := range result.Symbols {
//	    fmt.Printf("%s: %s at line %d\n", symbol.Kind, symbol.Name, symbol.StartLine)
//	}
//
// Limitations:
//
//   - Does not resolve type information across files (single-file analysis only)
//   - May produce incomplete results for syntactically invalid code
//   - Language-specific edge cases may not be fully handled
//   - Does not perform semantic analysis (type checking, reference resolution)
//
// Assumptions:
//
//   - Content is valid UTF-8 encoded text
//   - FilePath uses forward slashes as path separator
//   - File extension matches the parser's supported extensions
//   - Caller handles concurrent access if sharing parser instances
type Parser interface {
	// Parse extracts symbols and metadata from source code.
	//
	// The method parses the provided content and returns a ParseResult containing
	// all extracted symbols, imports, and metadata. Parsing should be resilient
	// to syntax errors, returning partial results when possible.
	//
	// Parameters:
	//   - ctx: Context for cancellation. Long-running parses should check ctx.Done().
	//   - content: Raw source code bytes (must be valid UTF-8).
	//   - filePath: Path to the file (relative to project root, for ID generation).
	//
	// Returns:
	//   - *ParseResult: Extracted symbols and metadata. Never nil on success.
	//   - error: Non-nil only for complete parse failures (e.g., invalid UTF-8).
	//            Syntax errors are reported in ParseResult.Errors.
	//
	// Thread Safety:
	//   Implementations should be safe for concurrent use. Multiple goroutines
	//   may call Parse simultaneously with different content.
	Parse(ctx context.Context, content []byte, filePath string) (*ParseResult, error)

	// Language returns the canonical name of the language this parser handles.
	//
	// Returns a lowercase string identifying the language:
	//   - "go" for Go
	//   - "python" for Python
	//   - "typescript" for TypeScript
	//   - "javascript" for JavaScript
	//   - "html" for HTML
	//   - "css" for CSS
	//
	// This value is used for:
	//   - Setting ParseResult.Language and Symbol.Language
	//   - Parser selection based on file type
	//   - Logging and debugging
	Language() string

	// Extensions returns the file extensions this parser can handle.
	//
	// Returns a slice of extensions including the leading dot:
	//   - Go: [".go"]
	//   - Python: [".py", ".pyi"]
	//   - TypeScript: [".ts", ".tsx", ".mts", ".cts"]
	//   - JavaScript: [".js", ".jsx", ".mjs", ".cjs"]
	//   - HTML: [".html", ".htm"]
	//   - CSS: [".css"]
	//
	// Extensions are used by the parser registry to select the appropriate
	// parser for a given file. Extensions are case-sensitive and should
	// be lowercase.
	Extensions() []string
}

// ParserRegistry manages parser instances by language and file extension.
//
// Description:
//
//	ParserRegistry provides a central lookup mechanism for finding the appropriate
//	parser for a given file or language. It supports registration of multiple
//	parsers and lookup by language name or file extension.
//
// Thread Safety:
//
//	ParserRegistry is fully thread-safe. All methods can be called concurrently
//	from multiple goroutines. Registration uses write locks, lookups use read locks.
type ParserRegistry struct {
	mu sync.RWMutex

	// byLanguage maps language names to parser instances.
	byLanguage map[string]Parser

	// byExtension maps file extensions to parser instances.
	byExtension map[string]Parser
}

// NewParserRegistry creates a new empty ParserRegistry.
func NewParserRegistry() *ParserRegistry {
	return &ParserRegistry{
		byLanguage:  make(map[string]Parser),
		byExtension: make(map[string]Parser),
	}
}

// Register adds a parser to the registry.
//
// The parser is registered under its Language() name and all its Extensions().
// If a language or extension is already registered, it will be overwritten.
//
// Thread Safety: This method is safe for concurrent use.
//
// Parameters:
//   - parser: The parser to register. Must not be nil.
//
// Example:
//
//	registry := NewParserRegistry()
//	registry.Register(NewGoParser())
//	registry.Register(NewPythonParser())
func (r *ParserRegistry) Register(parser Parser) {
	if parser == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.byLanguage[parser.Language()] = parser

	for _, ext := range parser.Extensions() {
		r.byExtension[ext] = parser
	}
}

// GetByLanguage returns the parser for the given language name.
//
// Thread Safety: This method is safe for concurrent use.
//
// Parameters:
//   - language: The language name (e.g., "go", "python"). Case-sensitive.
//
// Returns:
//   - Parser: The registered parser, or nil if not found.
//   - bool: True if a parser was found.
func (r *ParserRegistry) GetByLanguage(language string) (Parser, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	parser, ok := r.byLanguage[language]
	return parser, ok
}

// GetByExtension returns the parser for the given file extension.
//
// Thread Safety: This method is safe for concurrent use.
//
// Parameters:
//   - ext: The file extension including the dot (e.g., ".go", ".py"). Case-sensitive.
//
// Returns:
//   - Parser: The registered parser, or nil if not found.
//   - bool: True if a parser was found.
func (r *ParserRegistry) GetByExtension(ext string) (Parser, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	parser, ok := r.byExtension[ext]
	return parser, ok
}

// Languages returns a list of all registered language names.
//
// Thread Safety: This method is safe for concurrent use.
func (r *ParserRegistry) Languages() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	languages := make([]string, 0, len(r.byLanguage))
	for lang := range r.byLanguage {
		languages = append(languages, lang)
	}
	return languages
}

// Extensions returns a list of all registered file extensions.
//
// Thread Safety: This method is safe for concurrent use.
func (r *ParserRegistry) Extensions() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	extensions := make([]string, 0, len(r.byExtension))
	for ext := range r.byExtension {
		extensions = append(extensions, ext)
	}
	return extensions
}

// ParseOptions configures parser behavior.
//
// Parsers may accept ParseOptions to customize their behavior.
// Not all options are supported by all parsers.
type ParseOptions struct {
	// IncludeComments determines whether to extract comments as symbols.
	// Default: false (comments extracted as DocComment on associated symbols only)
	IncludeComments bool

	// IncludePrivate determines whether to include non-exported symbols.
	// Default: true (include all symbols)
	IncludePrivate bool

	// MaxDepth limits the nesting depth for child symbol extraction.
	// 0 means no limit. Default: 0
	MaxDepth int

	// ExtractBodies determines whether to include function/method body text.
	// Default: false (bodies are expensive and often not needed)
	ExtractBodies bool
}

// DefaultParseOptions returns the default parse options.
func DefaultParseOptions() ParseOptions {
	return ParseOptions{
		IncludeComments: false,
		IncludePrivate:  true,
		MaxDepth:        0,
		ExtractBodies:   false,
	}
}
