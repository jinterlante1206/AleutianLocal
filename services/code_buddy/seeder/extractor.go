// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package seeder

import (
	"context"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// MaxFilesPerModule limits how many files to parse per module.
const MaxFilesPerModule = 500

// ExtractDocs extracts documentation from a dependency's source files.
//
// Description:
//
//	Parses Go source files to extract documentation for exported symbols.
//	Uses the standard library's go/doc package for accurate extraction.
//
// Inputs:
//
//	ctx - Context for cancellation
//	dep - The dependency to extract docs from
//	dataSpace - Weaviate data space for isolation
//
// Outputs:
//
//	[]LibraryDoc - Extracted documentation entries
//	error - Non-nil if extraction fails completely
func ExtractDocs(ctx context.Context, dep Dependency, dataSpace string) ([]LibraryDoc, error) {
	if dep.LocalPath == "" {
		return nil, fmt.Errorf("dependency has no local path: %s", dep.ModulePath)
	}

	if dep.Language != "go" {
		return nil, ErrUnsupportedLanguage
	}

	var docs []LibraryDoc
	fileCount := 0

	// Walk the module directory
	err := filepath.WalkDir(dep.LocalPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		// Check context
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Skip directories that shouldn't be documented
		if d.IsDir() {
			name := d.Name()
			if name == "testdata" || name == "internal" || name == "vendor" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process .go files (skip tests)
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fileCount++
		if fileCount > MaxFilesPerModule {
			return filepath.SkipAll // Stop after limit
		}

		// Extract docs from this file
		fileDocs, err := extractDocsFromFile(path, dep, dataSpace)
		if err != nil {
			// Log but don't fail on individual file errors
			return nil
		}
		docs = append(docs, fileDocs...)

		return nil
	})

	if err != nil && err != filepath.SkipAll {
		return docs, fmt.Errorf("walking module %s: %w", dep.ModulePath, err)
	}

	return docs, nil
}

// extractDocsFromFile extracts documentation from a single Go file.
func extractDocsFromFile(path string, dep Dependency, dataSpace string) ([]LibraryDoc, error) {
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	// Get package name
	pkgName := astFile.Name.Name

	var docs []LibraryDoc

	// Create a doc.Package for proper doc extraction
	pkgDoc := doc.New(&ast.Package{
		Name:  pkgName,
		Files: map[string]*ast.File{path: astFile},
	}, dep.ModulePath, doc.AllDecls)

	// Extract type documentation
	for _, t := range pkgDoc.Types {
		if !token.IsExported(t.Name) {
			continue
		}

		symbolPath := pkgName + "." + t.Name
		docID := GenerateDocID(dep.ModulePath, dep.Version, symbolPath)

		docs = append(docs, LibraryDoc{
			DocID:      docID,
			Library:    dep.ModulePath,
			Version:    dep.Version,
			SymbolPath: symbolPath,
			SymbolKind: "type",
			Signature:  extractTypeSignature(t),
			DocContent: t.Doc,
			DataSpace:  dataSpace,
		})

		// Extract methods
		for _, m := range t.Methods {
			if !token.IsExported(m.Name) {
				continue
			}

			methodPath := symbolPath + "." + m.Name
			methodDocID := GenerateDocID(dep.ModulePath, dep.Version, methodPath)

			docs = append(docs, LibraryDoc{
				DocID:      methodDocID,
				Library:    dep.ModulePath,
				Version:    dep.Version,
				SymbolPath: methodPath,
				SymbolKind: "method",
				Signature:  extractFuncSignature(m),
				DocContent: m.Doc,
				DataSpace:  dataSpace,
			})
		}
	}

	// Extract function documentation
	for _, f := range pkgDoc.Funcs {
		if !token.IsExported(f.Name) {
			continue
		}

		symbolPath := pkgName + "." + f.Name
		docID := GenerateDocID(dep.ModulePath, dep.Version, symbolPath)

		docs = append(docs, LibraryDoc{
			DocID:      docID,
			Library:    dep.ModulePath,
			Version:    dep.Version,
			SymbolPath: symbolPath,
			SymbolKind: "function",
			Signature:  extractFuncSignature(f),
			DocContent: f.Doc,
			Example:    extractExample(f.Examples),
			DataSpace:  dataSpace,
		})
	}

	// Extract constant documentation
	for _, c := range pkgDoc.Consts {
		for _, name := range c.Names {
			if !token.IsExported(name) {
				continue
			}

			symbolPath := pkgName + "." + name
			docID := GenerateDocID(dep.ModulePath, dep.Version, symbolPath)

			docs = append(docs, LibraryDoc{
				DocID:      docID,
				Library:    dep.ModulePath,
				Version:    dep.Version,
				SymbolPath: symbolPath,
				SymbolKind: "constant",
				DocContent: c.Doc,
				DataSpace:  dataSpace,
			})
		}
	}

	// Extract variable documentation
	for _, v := range pkgDoc.Vars {
		for _, name := range v.Names {
			if !token.IsExported(name) {
				continue
			}

			symbolPath := pkgName + "." + name
			docID := GenerateDocID(dep.ModulePath, dep.Version, symbolPath)

			docs = append(docs, LibraryDoc{
				DocID:      docID,
				Library:    dep.ModulePath,
				Version:    dep.Version,
				SymbolPath: symbolPath,
				SymbolKind: "variable",
				DocContent: v.Doc,
				DataSpace:  dataSpace,
			})
		}
	}

	return docs, nil
}

// extractTypeSignature generates a signature string for a type.
func extractTypeSignature(t *doc.Type) string {
	if t.Decl == nil || len(t.Decl.Specs) == 0 {
		return "type " + t.Name
	}

	spec := t.Decl.Specs[0]
	if ts, ok := spec.(*ast.TypeSpec); ok {
		switch ts.Type.(type) {
		case *ast.StructType:
			return "type " + t.Name + " struct { ... }"
		case *ast.InterfaceType:
			return "type " + t.Name + " interface { ... }"
		default:
			return "type " + t.Name
		}
	}
	return "type " + t.Name
}

// extractFuncSignature generates a signature string for a function.
func extractFuncSignature(f *doc.Func) string {
	if f.Decl == nil {
		return "func " + f.Name + "()"
	}

	// Build signature from AST
	var sig strings.Builder
	sig.WriteString("func ")

	// Add receiver if present
	if f.Recv != "" {
		sig.WriteString("(")
		sig.WriteString(f.Recv)
		sig.WriteString(") ")
	}

	sig.WriteString(f.Name)
	sig.WriteString("(")

	// Add parameters
	if f.Decl.Type.Params != nil && len(f.Decl.Type.Params.List) > 0 {
		sig.WriteString("...")
	}

	sig.WriteString(")")

	// Add return type
	if f.Decl.Type.Results != nil && len(f.Decl.Type.Results.List) > 0 {
		sig.WriteString(" (...)")
	}

	return sig.String()
}

// extractExample extracts the first example from a list.
func extractExample(examples []*doc.Example) string {
	if len(examples) == 0 {
		return ""
	}

	// Return the first example's code
	ex := examples[0]
	if ex.Code != nil {
		return ex.Output // Use output as a simpler representation
	}
	return ""
}
