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

import (
	"testing"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
)

func TestPatternMatcher_Match_Name(t *testing.T) {
	t.Run("exact match", func(t *testing.T) {
		pm := PatternMatcher{Name: "main"}
		sym := &ast.Symbol{Name: "main", Language: "go"}
		if !pm.Match(sym) {
			t.Error("expected exact name match")
		}
	})

	t.Run("exact match fails for different name", func(t *testing.T) {
		pm := PatternMatcher{Name: "main"}
		sym := &ast.Symbol{Name: "notmain", Language: "go"}
		if pm.Match(sym) {
			t.Error("expected no match for different name")
		}
	})

	t.Run("glob pattern with suffix", func(t *testing.T) {
		pm := PatternMatcher{Name: "Test*"}
		testCases := []struct {
			name    string
			matches bool
		}{
			{"TestFoo", true},
			{"TestBar", true},
			{"Test", true},
			{"NotATest", false},
			{"test", false}, // Case-sensitive
		}

		for _, tc := range testCases {
			sym := &ast.Symbol{Name: tc.name, Language: "go"}
			if pm.Match(sym) != tc.matches {
				t.Errorf("Name %q: expected match=%v, got match=%v", tc.name, tc.matches, pm.Match(sym))
			}
		}
	})

	t.Run("glob pattern with prefix", func(t *testing.T) {
		pm := PatternMatcher{Name: "*Handler"}
		testCases := []struct {
			name    string
			matches bool
		}{
			{"UserHandler", true},
			{"Handler", true},
			{"handle", false},
		}

		for _, tc := range testCases {
			sym := &ast.Symbol{Name: tc.name, Language: "go"}
			if pm.Match(sym) != tc.matches {
				t.Errorf("Name %q: expected match=%v, got match=%v", tc.name, tc.matches, pm.Match(sym))
			}
		}
	})
}

func TestPatternMatcher_Match_Package(t *testing.T) {
	t.Run("exact package match", func(t *testing.T) {
		pm := PatternMatcher{Name: "main", Package: "main"}
		sym := &ast.Symbol{Name: "main", Package: "main", Language: "go"}
		if !pm.Match(sym) {
			t.Error("expected package match")
		}
	})

	t.Run("package mismatch", func(t *testing.T) {
		pm := PatternMatcher{Name: "main", Package: "main"}
		sym := &ast.Symbol{Name: "main", Package: "other", Language: "go"}
		if pm.Match(sym) {
			t.Error("expected no match for different package")
		}
	})

	t.Run("wildcard package", func(t *testing.T) {
		pm := PatternMatcher{Name: "init", Package: "*"}
		sym := &ast.Symbol{Name: "init", Package: "anypackage", Language: "go"}
		if !pm.Match(sym) {
			t.Error("expected wildcard package to match")
		}
	})
}

func TestPatternMatcher_Match_Signature(t *testing.T) {
	t.Run("signature contains", func(t *testing.T) {
		pm := PatternMatcher{Signature: "gin.Context"}
		sym := &ast.Symbol{
			Name:      "Handler",
			Signature: "func(c *gin.Context)",
			Language:  "go",
		}
		if !pm.Match(sym) {
			t.Error("expected signature match")
		}
	})

	t.Run("signature not found", func(t *testing.T) {
		pm := PatternMatcher{Signature: "echo.Context"}
		sym := &ast.Symbol{
			Name:      "Handler",
			Signature: "func(c *gin.Context)",
			Language:  "go",
		}
		if pm.Match(sym) {
			t.Error("expected no match for different signature")
		}
	})
}

func TestPatternMatcher_Match_Decorator(t *testing.T) {
	t.Run("decorator match", func(t *testing.T) {
		pm := PatternMatcher{Decorator: "@app.route"}
		sym := &ast.Symbol{
			Name:     "index",
			Language: "python",
			Metadata: &ast.SymbolMetadata{
				Decorators: []string{"@app.route", "@login_required"},
			},
		}
		if !pm.Match(sym) {
			t.Error("expected decorator match")
		}
	})

	t.Run("decorator not found", func(t *testing.T) {
		pm := PatternMatcher{Decorator: "@app.route"}
		sym := &ast.Symbol{
			Name:     "helper",
			Language: "python",
			Metadata: &ast.SymbolMetadata{
				Decorators: []string{"@staticmethod"},
			},
		}
		if pm.Match(sym) {
			t.Error("expected no match without decorator")
		}
	})

	t.Run("no metadata", func(t *testing.T) {
		pm := PatternMatcher{Decorator: "@app.route"}
		sym := &ast.Symbol{
			Name:     "helper",
			Language: "python",
		}
		if pm.Match(sym) {
			t.Error("expected no match without metadata")
		}
	})
}

func TestPatternMatcher_Match_BaseClass(t *testing.T) {
	t.Run("base class match", func(t *testing.T) {
		pm := PatternMatcher{BaseClass: "TestCase"}
		sym := &ast.Symbol{
			Name:     "MyTest",
			Kind:     ast.SymbolKindClass,
			Language: "python",
			Metadata: &ast.SymbolMetadata{
				Extends: "TestCase",
			},
		}
		if !pm.Match(sym) {
			t.Error("expected base class match")
		}
	})

	t.Run("base class with module prefix", func(t *testing.T) {
		pm := PatternMatcher{BaseClass: "TestCase"}
		sym := &ast.Symbol{
			Name:     "MyTest",
			Kind:     ast.SymbolKindClass,
			Language: "python",
			Metadata: &ast.SymbolMetadata{
				Extends: "unittest.TestCase",
			},
		}
		if !pm.Match(sym) {
			t.Error("expected base class match with prefix")
		}
	})

	t.Run("base class wildcard", func(t *testing.T) {
		pm := PatternMatcher{BaseClass: "*Servicer"}
		sym := &ast.Symbol{
			Name:     "MyServicer",
			Kind:     ast.SymbolKindClass,
			Language: "python",
			Metadata: &ast.SymbolMetadata{
				Extends: "UserServicer",
			},
		}
		if !pm.Match(sym) {
			t.Error("expected wildcard base class match")
		}
	})
}

func TestPatternMatcher_Match_Implements(t *testing.T) {
	t.Run("implements match", func(t *testing.T) {
		pm := PatternMatcher{Implements: "*Server"}
		sym := &ast.Symbol{
			Name:     "MyServer",
			Kind:     ast.SymbolKindStruct,
			Language: "go",
			Metadata: &ast.SymbolMetadata{
				Implements: []string{"UserServer", "io.Closer"},
			},
		}
		if !pm.Match(sym) {
			t.Error("expected implements match")
		}
	})
}

func TestEntryPointRegistry(t *testing.T) {
	t.Run("get default patterns", func(t *testing.T) {
		r := NewEntryPointRegistry()

		langs := []string{"go", "python", "typescript", "javascript"}
		for _, lang := range langs {
			patterns, ok := r.GetPatterns(lang)
			if !ok {
				t.Errorf("expected patterns for %s", lang)
			}
			if patterns.Language != lang {
				t.Errorf("expected language %s, got %s", lang, patterns.Language)
			}
		}
	})

	t.Run("unknown language", func(t *testing.T) {
		r := NewEntryPointRegistry()
		_, ok := r.GetPatterns("ruby")
		if ok {
			t.Error("expected no patterns for ruby")
		}
	})

	t.Run("register custom patterns", func(t *testing.T) {
		r := NewEntryPointRegistry()
		custom := &EntryPointPatterns{
			Language: "rust",
			Main:     []PatternMatcher{{Name: "main"}},
		}
		r.RegisterPatterns("rust", custom)

		patterns, ok := r.GetPatterns("rust")
		if !ok {
			t.Error("expected patterns for rust")
		}
		if len(patterns.Main) != 1 {
			t.Error("expected 1 main pattern")
		}
	})

	t.Run("list languages", func(t *testing.T) {
		r := NewEntryPointRegistry()
		langs := r.Languages()
		if len(langs) < 4 {
			t.Errorf("expected at least 4 languages, got %d", len(langs))
		}
	})
}

func TestDefaultGoPatterns(t *testing.T) {
	patterns := DefaultGoPatterns()

	t.Run("has main pattern", func(t *testing.T) {
		if len(patterns.Main) == 0 {
			t.Error("expected main patterns")
		}
		// Main pattern should match main() in package main
		sym := &ast.Symbol{Name: "main", Package: "main", Language: "go"}
		matched := false
		for _, p := range patterns.Main {
			if p.Match(sym) {
				matched = true
				break
			}
		}
		if !matched {
			t.Error("expected main pattern to match")
		}
	})

	t.Run("has http handler patterns", func(t *testing.T) {
		if len(patterns.Handler) == 0 {
			t.Error("expected handler patterns")
		}
	})

	t.Run("has test patterns", func(t *testing.T) {
		if len(patterns.Test) == 0 {
			t.Error("expected test patterns")
		}
		// Test pattern should match Test* functions with testing.T in signature
		sym := &ast.Symbol{
			Name:      "TestMyFunction",
			Signature: "func(t *testing.T)",
			Language:  "go",
		}
		matched := false
		for _, p := range patterns.Test {
			if p.Match(sym) {
				matched = true
				break
			}
		}
		if !matched {
			t.Error("expected test pattern to match")
		}
	})
}

func TestDefaultPythonPatterns(t *testing.T) {
	patterns := DefaultPythonPatterns()

	t.Run("has flask handler patterns", func(t *testing.T) {
		found := false
		for _, p := range patterns.Handler {
			if p.Framework == "flask" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected flask handler patterns")
		}
	})

	t.Run("has fastapi handler patterns", func(t *testing.T) {
		found := false
		for _, p := range patterns.Handler {
			if p.Framework == "fastapi" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected fastapi handler patterns")
		}
	})

	t.Run("has pytest patterns", func(t *testing.T) {
		found := false
		for _, p := range patterns.Test {
			if p.Framework == "pytest" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected pytest patterns")
		}
	})
}

func TestDefaultTypeScriptPatterns(t *testing.T) {
	patterns := DefaultTypeScriptPatterns()

	t.Run("has nestjs handler patterns", func(t *testing.T) {
		found := false
		for _, p := range patterns.Handler {
			if p.Framework == "nestjs" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected nestjs handler patterns")
		}
	})

	t.Run("has nextjs handler patterns", func(t *testing.T) {
		found := false
		for _, p := range patterns.Handler {
			if p.Framework == "nextjs" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected nextjs handler patterns")
		}
	})

	t.Run("has jest test patterns", func(t *testing.T) {
		found := false
		for _, p := range patterns.Test {
			if p.Framework == "jest" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected jest test patterns")
		}
	})
}

func TestNilSymbol(t *testing.T) {
	pm := PatternMatcher{Name: "main"}
	if pm.Match(nil) {
		t.Error("expected no match for nil symbol")
	}
}
