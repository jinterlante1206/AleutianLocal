// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package reason

import (
	"context"
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

func setupValidatorTestIndex() *index.SymbolIndex {
	idx := index.NewSymbolIndex()

	// Add some types to the index
	userType := &ast.Symbol{
		ID:       "types/user.go:10:User",
		Name:     "User",
		Kind:     ast.SymbolKindStruct,
		FilePath: "types/user.go",
		Package:  "types",
		Language: "go",
	}

	requestType := &ast.Symbol{
		ID:       "types/request.go:5:Request",
		Name:     "Request",
		Kind:     ast.SymbolKindStruct,
		FilePath: "types/request.go",
		Package:  "types",
		Language: "go",
	}

	idx.Add(userType)
	idx.Add(requestType)

	return idx
}

func TestChangeValidator_ValidateChange(t *testing.T) {
	idx := setupValidatorTestIndex()
	validator := NewChangeValidator(idx)
	ctx := context.Background()

	t.Run("valid go code passes syntax check", func(t *testing.T) {
		content := `package main

import "fmt"

func main() {
	fmt.Println("Hello, world!")
}
`
		validation, err := validator.ValidateChange(ctx, "main.go", content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !validation.SyntaxValid {
			t.Errorf("expected syntax to be valid, errors: %v", validation.Errors)
		}
		if validation.Scope != "syntactic" {
			t.Errorf("expected scope 'syntactic', got %q", validation.Scope)
		}
	})

	t.Run("invalid go code fails syntax check", func(t *testing.T) {
		content := `package main

func main() {
	// Missing closing brace
	fmt.Println("Hello"
}
`
		validation, err := validator.ValidateChange(ctx, "main.go", content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if validation.SyntaxValid {
			t.Error("expected syntax to be invalid")
		}
		if len(validation.Errors) == 0 {
			t.Error("expected syntax errors")
		}
	})

	t.Run("valid python code passes syntax check", func(t *testing.T) {
		content := `def hello():
    print("Hello, world!")

if __name__ == "__main__":
    hello()
`
		validation, err := validator.ValidateChange(ctx, "main.py", content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !validation.SyntaxValid {
			t.Errorf("expected syntax to be valid, errors: %v", validation.Errors)
		}
	})

	t.Run("invalid python code fails syntax check", func(t *testing.T) {
		content := `def hello(
    # Missing closing paren
    print("Hello")
`
		validation, err := validator.ValidateChange(ctx, "main.py", content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if validation.SyntaxValid {
			t.Error("expected syntax to be invalid")
		}
	})

	t.Run("valid typescript code passes syntax check", func(t *testing.T) {
		content := `function hello(): void {
    console.log("Hello, world!");
}

hello();
`
		validation, err := validator.ValidateChange(ctx, "main.ts", content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !validation.SyntaxValid {
			t.Errorf("expected syntax to be valid, errors: %v", validation.Errors)
		}
	})

	t.Run("has confidence score", func(t *testing.T) {
		content := `package main

func main() {}
`
		validation, err := validator.ValidateChange(ctx, "main.go", content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if validation.Confidence <= 0 || validation.Confidence > 1 {
			t.Errorf("confidence should be between 0 and 1, got %f", validation.Confidence)
		}
	})
}

func TestChangeValidator_Errors(t *testing.T) {
	validator := NewChangeValidator(nil)
	ctx := context.Background()

	t.Run("nil context", func(t *testing.T) {
		_, err := validator.ValidateChange(nil, "main.go", "content")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("empty file path", func(t *testing.T) {
		_, err := validator.ValidateChange(ctx, "", "content")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("empty content", func(t *testing.T) {
		_, err := validator.ValidateChange(ctx, "main.go", "")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

		_, err := validator.ValidateChange(cancelCtx, "main.go", "content")
		if err != ErrContextCanceled {
			t.Errorf("expected ErrContextCanceled, got %v", err)
		}
	})
}

func TestChangeValidator_UnknownLanguage(t *testing.T) {
	validator := NewChangeValidator(nil)
	ctx := context.Background()

	validation, err := validator.ValidateChange(ctx, "file.unknown", "some content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have warning about unknown language
	hasWarning := false
	for _, w := range validation.Warnings {
		if w.Type == "unknown_language" {
			hasWarning = true
			break
		}
	}
	if !hasWarning {
		t.Error("expected unknown_language warning")
	}

	// Confidence should be reduced
	if validation.Confidence >= 0.6 {
		t.Errorf("expected reduced confidence for unknown language, got %f", validation.Confidence)
	}
}

func TestChangeValidator_ImportValidation(t *testing.T) {
	validator := NewChangeValidator(nil)
	ctx := context.Background()

	t.Run("warns about suspicious imports", func(t *testing.T) {
		content := `package main

import "../../../dangerous/path"

func main() {}
`
		validation, err := validator.ValidateChange(ctx, "main.go", content)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		hasSuspiciousWarning := false
		for _, w := range validation.Warnings {
			if w.Type == "import" {
				hasSuspiciousWarning = true
				break
			}
		}
		if !hasSuspiciousWarning {
			t.Error("expected import warning for path traversal")
		}
	})
}

func TestDetectLanguage(t *testing.T) {
	validator := NewChangeValidator(nil)

	tests := []struct {
		filePath string
		expected string
	}{
		{"main.go", "go"},
		{"test.py", "python"},
		{"types.pyi", "python"},
		{"app.ts", "typescript"},
		{"app.tsx", "typescript"},
		{"app.js", "javascript"},
		{"app.jsx", "javascript"},
		{"file.unknown", ""},
		{"MAIN.GO", "go"}, // Case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.filePath, func(t *testing.T) {
			result := validator.detectLanguage(tt.filePath)
			if result != tt.expected {
				t.Errorf("detectLanguage(%q) = %q, want %q",
					tt.filePath, result, tt.expected)
			}
		})
	}
}

func TestIsBuiltinType(t *testing.T) {
	tests := []struct {
		typeName string
		lang     string
		expected bool
	}{
		// Go builtins
		{"int", "go", true},
		{"string", "go", true},
		{"bool", "go", true},
		{"error", "go", true},
		{"any", "go", true},
		{"CustomType", "go", false},

		// Python builtins
		{"str", "python", true},
		{"int", "python", true},
		{"dict", "python", true},
		{"None", "python", true},
		{"CustomClass", "python", false},

		// TypeScript builtins
		{"string", "typescript", true},
		{"number", "typescript", true},
		{"boolean", "typescript", true},
		{"any", "typescript", true},
		{"CustomInterface", "typescript", false},

		// Unknown language
		{"string", "rust", false},
	}

	for _, tt := range tests {
		name := tt.typeName + "/" + tt.lang
		t.Run(name, func(t *testing.T) {
			result := isBuiltinType(tt.typeName, tt.lang)
			if result != tt.expected {
				t.Errorf("isBuiltinType(%q, %q) = %v, want %v",
					tt.typeName, tt.lang, result, tt.expected)
			}
		})
	}
}

func TestIsGoStdLib(t *testing.T) {
	tests := []struct {
		pkg      string
		expected bool
	}{
		{"fmt", true},
		{"context", true},
		{"net", true},
		{"http", true},
		{"testing", true},
		{"mypackage", false},
		{"github.com/user/pkg", false},
	}

	for _, tt := range tests {
		t.Run(tt.pkg, func(t *testing.T) {
			result := isGoStdLib(tt.pkg)
			if result != tt.expected {
				t.Errorf("isGoStdLib(%q) = %v, want %v",
					tt.pkg, result, tt.expected)
			}
		})
	}
}

func TestIsTypeSymbol(t *testing.T) {
	tests := []struct {
		symbol   *ast.Symbol
		expected bool
	}{
		{
			symbol:   &ast.Symbol{Kind: ast.SymbolKindStruct},
			expected: true,
		},
		{
			symbol:   &ast.Symbol{Kind: ast.SymbolKindInterface},
			expected: true,
		},
		{
			symbol:   &ast.Symbol{Kind: ast.SymbolKindType},
			expected: true,
		},
		{
			symbol:   &ast.Symbol{Kind: ast.SymbolKindClass},
			expected: true,
		},
		{
			symbol:   &ast.Symbol{Kind: ast.SymbolKindEnum},
			expected: true,
		},
		{
			symbol:   &ast.Symbol{Kind: ast.SymbolKindFunction},
			expected: false,
		},
		{
			symbol:   &ast.Symbol{Kind: ast.SymbolKindMethod},
			expected: false,
		},
		{
			symbol:   &ast.Symbol{Kind: ast.SymbolKindVariable},
			expected: false,
		},
		{
			symbol:   nil,
			expected: false,
		},
	}

	for i, tt := range tests {
		name := "nil"
		if tt.symbol != nil {
			name = tt.symbol.Kind.String()
		}
		t.Run(name, func(t *testing.T) {
			result := isTypeSymbol(tests[i].symbol)
			if result != tests[i].expected {
				t.Errorf("isTypeSymbol() = %v, want %v", result, tests[i].expected)
			}
		})
	}
}

func TestChangeValidator_Concurrency(t *testing.T) {
	validator := NewChangeValidator(nil)
	ctx := context.Background()

	// Validate concurrently to test thread safety
	done := make(chan bool, 10)
	content := `package main

func main() {}
`

	for i := 0; i < 10; i++ {
		go func() {
			_, err := validator.ValidateChange(ctx, "main.go", content)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestExtractImports(t *testing.T) {
	validator := NewChangeValidator(nil)

	t.Run("go imports", func(t *testing.T) {
		content := `package main

import (
	"fmt"
	"net/http"
)

func main() {}
`
		imports := validator.extractImports(content, "go")
		if len(imports) != 2 {
			t.Errorf("expected 2 imports, got %d", len(imports))
		}

		paths := make(map[string]bool)
		for _, imp := range imports {
			paths[imp.Path] = true
		}

		if !paths["fmt"] {
			t.Error("expected 'fmt' import")
		}
		if !paths["net/http"] {
			t.Error("expected 'net/http' import")
		}
	})

	t.Run("python imports", func(t *testing.T) {
		content := `import os
from typing import Dict
`
		imports := validator.extractImports(content, "python")
		if len(imports) != 2 {
			t.Errorf("expected 2 imports, got %d", len(imports))
		}
	})

	t.Run("typescript imports", func(t *testing.T) {
		content := `import { Component } from "react";
const fs = require("fs");
`
		imports := validator.extractImports(content, "typescript")
		if len(imports) != 2 {
			t.Errorf("expected 2 imports, got %d", len(imports))
		}
	})
}
