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

func TestSourceRegistry_MatchSource(t *testing.T) {
	registry := NewSourceRegistry()

	t.Run("matches HTTP source", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "FormValue",
			Receiver: "*http.Request",
			Language: "go",
		}

		pattern, ok := registry.MatchSource(sym)
		if !ok {
			t.Fatal("expected to match HTTP source")
		}
		if pattern.Category != SourceHTTP {
			t.Errorf("expected category %v, got %v", SourceHTTP, pattern.Category)
		}
	})

	t.Run("matches env source", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "Getenv",
			Package:  "os",
			Language: "go",
		}

		pattern, ok := registry.MatchSource(sym)
		if !ok {
			t.Fatal("expected to match env source")
		}
		if pattern.Category != SourceEnv {
			t.Errorf("expected category %v, got %v", SourceEnv, pattern.Category)
		}
	})

	t.Run("matches file source", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "ReadFile",
			Package:  "os",
			Language: "go",
		}

		pattern, ok := registry.MatchSource(sym)
		if !ok {
			t.Fatal("expected to match file source")
		}
		if pattern.Category != SourceFile {
			t.Errorf("expected category %v, got %v", SourceFile, pattern.Category)
		}
	})

	t.Run("no match for unrecognized symbol", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "CustomFunction",
			Package:  "custom",
			Language: "go",
		}

		_, ok := registry.MatchSource(sym)
		if ok {
			t.Error("expected no match for unrecognized symbol")
		}
	})

	t.Run("nil symbol returns no match", func(t *testing.T) {
		_, ok := registry.MatchSource(nil)
		if ok {
			t.Error("expected no match for nil symbol")
		}
	})
}

func TestSinkRegistry_MatchSink(t *testing.T) {
	registry := NewSinkRegistry()

	t.Run("matches response sink", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "Write",
			Receiver: "http.ResponseWriter",
			Language: "go",
		}

		pattern, ok := registry.MatchSink(sym)
		if !ok {
			t.Fatal("expected to match response sink")
		}
		if pattern.Category != SinkResponse {
			t.Errorf("expected category %v, got %v", SinkResponse, pattern.Category)
		}
	})

	t.Run("matches dangerous SQL sink", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:      "Exec",
			Package:   "database/sql",
			Signature: "func(string) error",
			Language:  "go",
		}

		pattern, ok := registry.MatchSink(sym)
		if !ok {
			t.Fatal("expected to match SQL sink")
		}
		if !pattern.IsDangerous {
			t.Error("expected SQL sink to be dangerous")
		}
	})

	t.Run("matches command execution sink", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "Command",
			Package:  "os/exec",
			Language: "go",
		}

		pattern, ok := registry.MatchSink(sym)
		if !ok {
			t.Fatal("expected to match command sink")
		}
		if pattern.Category != SinkCommand {
			t.Errorf("expected category %v, got %v", SinkCommand, pattern.Category)
		}
		if !pattern.IsDangerous {
			t.Error("expected command sink to be dangerous")
		}
	})

	t.Run("matches logging sink", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "Println",
			Package:  "log",
			Language: "go",
		}

		pattern, ok := registry.MatchSink(sym)
		if !ok {
			t.Fatal("expected to match log sink")
		}
		if pattern.Category != SinkLog {
			t.Errorf("expected category %v, got %v", SinkLog, pattern.Category)
		}
	})
}

func TestSinkRegistry_GetDangerousSinks(t *testing.T) {
	registry := NewSinkRegistry()

	t.Run("returns dangerous sinks for go", func(t *testing.T) {
		dangerous := registry.GetDangerousSinks("go")
		if len(dangerous) == 0 {
			t.Fatal("expected some dangerous sinks for go")
		}

		// All returned sinks should be dangerous
		for _, sink := range dangerous {
			if !sink.IsDangerous {
				t.Errorf("sink %s should be dangerous", sink.FunctionName)
			}
		}

		// Check for expected dangerous categories
		hasCommand := false
		hasSQL := false
		for _, sink := range dangerous {
			if sink.Category == SinkCommand {
				hasCommand = true
			}
			if sink.Category == SinkSQL {
				hasSQL = true
			}
		}
		if !hasCommand {
			t.Error("expected command execution sinks in dangerous list")
		}
		if !hasSQL {
			t.Error("expected SQL sinks in dangerous list")
		}
	})
}

func TestSourceRegistry_Languages(t *testing.T) {
	registry := NewSourceRegistry()
	langs := registry.Languages()

	if len(langs) < 4 {
		t.Errorf("expected at least 4 languages, got %d", len(langs))
	}

	// Check for expected languages
	langSet := make(map[string]bool)
	for _, lang := range langs {
		langSet[lang] = true
	}

	for _, expected := range []string{"go", "python", "typescript", "javascript"} {
		if !langSet[expected] {
			t.Errorf("expected language %s to be registered", expected)
		}
	}
}

func TestSourcePattern_Match(t *testing.T) {
	t.Run("matches by function name", func(t *testing.T) {
		pattern := SourcePattern{FunctionName: "ReadFile"}
		sym := &ast.Symbol{Name: "ReadFile", Language: "go"}

		if !pattern.Match(sym) {
			t.Error("expected pattern to match")
		}
	})

	t.Run("matches by receiver", func(t *testing.T) {
		pattern := SourcePattern{FunctionName: "Read", Receiver: "*os.File"}
		sym := &ast.Symbol{Name: "Read", Receiver: "*os.File", Language: "go"}

		if !pattern.Match(sym) {
			t.Error("expected pattern to match")
		}
	})

	t.Run("matches by package", func(t *testing.T) {
		pattern := SourcePattern{FunctionName: "Getenv", Package: "os"}
		sym := &ast.Symbol{Name: "Getenv", Package: "os", Language: "go"}

		if !pattern.Match(sym) {
			t.Error("expected pattern to match")
		}
	})

	t.Run("matches with wildcard in name", func(t *testing.T) {
		pattern := SourcePattern{FunctionName: "Get*"}
		sym := &ast.Symbol{Name: "GetString", Language: "go"}

		if !pattern.Match(sym) {
			t.Error("expected wildcard pattern to match")
		}
	})

	t.Run("does not match different name", func(t *testing.T) {
		pattern := SourcePattern{FunctionName: "ReadFile"}
		sym := &ast.Symbol{Name: "WriteFile", Language: "go"}

		if pattern.Match(sym) {
			t.Error("expected pattern not to match different name")
		}
	})
}

func TestSinkPattern_Match(t *testing.T) {
	t.Run("matches by signature", func(t *testing.T) {
		pattern := SinkPattern{FunctionName: "Exec", Signature: "string"}
		sym := &ast.Symbol{
			Name:      "Exec",
			Signature: "func(query string) error",
			Language:  "go",
		}

		if !pattern.Match(sym) {
			t.Error("expected pattern to match signature")
		}
	})

	t.Run("matches with wildcard in package", func(t *testing.T) {
		pattern := SinkPattern{FunctionName: "Run", Package: "*exec*"}
		sym := &ast.Symbol{Name: "Run", Package: "os/exec", Language: "go"}

		if !pattern.Match(sym) {
			t.Error("expected wildcard package pattern to match")
		}
	})
}

func TestPythonSourcePatterns(t *testing.T) {
	registry := NewSourceRegistry()

	t.Run("matches Flask request data", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "get",
			Receiver: "request.form",
			Language: "python",
		}

		pattern, ok := registry.MatchSource(sym)
		if !ok {
			t.Fatal("expected to match Flask source")
		}
		if pattern.Category != SourceHTTP {
			t.Errorf("expected category %v, got %v", SourceHTTP, pattern.Category)
		}
	})

	t.Run("matches Python env access", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "getenv",
			Package:  "os",
			Language: "python",
		}

		pattern, ok := registry.MatchSource(sym)
		if !ok {
			t.Fatal("expected to match Python env source")
		}
		if pattern.Category != SourceEnv {
			t.Errorf("expected category %v, got %v", SourceEnv, pattern.Category)
		}
	})
}

func TestTypeScriptSourcePatterns(t *testing.T) {
	registry := NewSourceRegistry()

	t.Run("matches Express request body", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "body",
			Receiver: "req",
			Language: "typescript",
		}

		pattern, ok := registry.MatchSource(sym)
		if !ok {
			t.Fatal("expected to match Express source")
		}
		if pattern.Category != SourceHTTP {
			t.Errorf("expected category %v, got %v", SourceHTTP, pattern.Category)
		}
	})

	t.Run("matches process.env", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "env",
			Receiver: "process",
			Language: "typescript",
		}

		pattern, ok := registry.MatchSource(sym)
		if !ok {
			t.Fatal("expected to match process.env source")
		}
		if pattern.Category != SourceEnv {
			t.Errorf("expected category %v, got %v", SourceEnv, pattern.Category)
		}
	})
}

func TestPythonSinkPatterns(t *testing.T) {
	registry := NewSinkRegistry()

	t.Run("matches Python eval as dangerous", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "eval",
			Language: "python",
		}

		pattern, ok := registry.MatchSink(sym)
		if !ok {
			t.Fatal("expected to match Python eval sink")
		}
		if !pattern.IsDangerous {
			t.Error("expected eval to be dangerous")
		}
		if pattern.Confidence < 0.9 {
			t.Errorf("expected high confidence for eval, got %f", pattern.Confidence)
		}
	})

	t.Run("matches subprocess as dangerous", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "run",
			Package:  "subprocess",
			Language: "python",
		}

		pattern, ok := registry.MatchSink(sym)
		if !ok {
			t.Fatal("expected to match subprocess sink")
		}
		if pattern.Category != SinkCommand {
			t.Errorf("expected category %v, got %v", SinkCommand, pattern.Category)
		}
		if !pattern.IsDangerous {
			t.Error("expected subprocess to be dangerous")
		}
	})
}

func TestTypeScriptSinkPatterns(t *testing.T) {
	registry := NewSinkRegistry()

	t.Run("matches JavaScript eval as dangerous", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "eval",
			Language: "javascript",
		}

		pattern, ok := registry.MatchSink(sym)
		if !ok {
			t.Fatal("expected to match JavaScript eval sink")
		}
		if !pattern.IsDangerous {
			t.Error("expected eval to be dangerous")
		}
	})

	t.Run("matches child_process exec as dangerous", func(t *testing.T) {
		sym := &ast.Symbol{
			Name:     "exec",
			Package:  "child_process",
			Language: "typescript",
		}

		pattern, ok := registry.MatchSink(sym)
		if !ok {
			t.Fatal("expected to match child_process sink")
		}
		if pattern.Category != SinkCommand {
			t.Errorf("expected category %v, got %v", SinkCommand, pattern.Category)
		}
		if !pattern.IsDangerous {
			t.Error("expected child_process to be dangerous")
		}
	})
}

func TestSourceRegistry_RegisterPatterns(t *testing.T) {
	registry := NewSourceRegistry()

	customPatterns := []SourcePattern{
		{FunctionName: "customSource", Category: SourceHTTP, Confidence: 0.9},
	}
	registry.RegisterPatterns("custom", customPatterns)

	patterns := registry.GetPatterns("custom")
	if len(patterns) != 1 {
		t.Errorf("expected 1 pattern, got %d", len(patterns))
	}
	if patterns[0].FunctionName != "customSource" {
		t.Errorf("expected customSource, got %s", patterns[0].FunctionName)
	}
}

func TestSinkRegistry_RegisterPatterns(t *testing.T) {
	registry := NewSinkRegistry()

	customPatterns := []SinkPattern{
		{FunctionName: "customSink", Category: SinkDatabase, IsDangerous: true, Confidence: 0.9},
	}
	registry.RegisterPatterns("custom", customPatterns)

	patterns := registry.GetPatterns("custom")
	if len(patterns) != 1 {
		t.Errorf("expected 1 pattern, got %d", len(patterns))
	}
	if patterns[0].FunctionName != "customSink" {
		t.Errorf("expected customSink, got %s", patterns[0].FunctionName)
	}
}
