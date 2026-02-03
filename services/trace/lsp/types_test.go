// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package lsp

import (
	"encoding/json"
	"testing"
)

func TestPosition_MarshalJSON(t *testing.T) {
	t.Run("roundtrip", func(t *testing.T) {
		p := Position{Line: 10, Character: 5}
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var p2 Position
		if err := json.Unmarshal(data, &p2); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if p != p2 {
			t.Errorf("roundtrip failed: got %+v, want %+v", p2, p)
		}
	})

	t.Run("zero values", func(t *testing.T) {
		p := Position{Line: 0, Character: 0}
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		expected := `{"line":0,"character":0}`
		if string(data) != expected {
			t.Errorf("got %s, want %s", string(data), expected)
		}
	})
}

func TestRange_MarshalJSON(t *testing.T) {
	r := Range{
		Start: Position{Line: 10, Character: 0},
		End:   Position{Line: 10, Character: 20},
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var r2 Range
	if err := json.Unmarshal(data, &r2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if r.Start != r2.Start || r.End != r2.End {
		t.Errorf("roundtrip failed: got %+v, want %+v", r2, r)
	}
}

func TestLocation_MarshalJSON(t *testing.T) {
	loc := Location{
		URI: "file:///path/to/file.go",
		Range: Range{
			Start: Position{Line: 10, Character: 0},
			End:   Position{Line: 10, Character: 20},
		},
	}

	data, err := json.Marshal(loc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var loc2 Location
	if err := json.Unmarshal(data, &loc2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if loc.URI != loc2.URI {
		t.Errorf("URI mismatch: got %s, want %s", loc2.URI, loc.URI)
	}
	if loc.Range.Start != loc2.Range.Start {
		t.Errorf("Range.Start mismatch: got %+v, want %+v", loc2.Range.Start, loc.Range.Start)
	}
}

func TestTextDocumentPositionParams_MarshalJSON(t *testing.T) {
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///test.go"},
		Position:     Position{Line: 5, Character: 10},
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var params2 TextDocumentPositionParams
	if err := json.Unmarshal(data, &params2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if params.TextDocument.URI != params2.TextDocument.URI {
		t.Errorf("URI mismatch: got %s, want %s", params2.TextDocument.URI, params.TextDocument.URI)
	}
	if params.Position != params2.Position {
		t.Errorf("Position mismatch: got %+v, want %+v", params2.Position, params.Position)
	}
}

func TestReferenceParams_MarshalJSON(t *testing.T) {
	params := ReferenceParams{
		TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///test.go"},
			Position:     Position{Line: 5, Character: 10},
		},
		Context: ReferenceContext{IncludeDeclaration: true},
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var params2 ReferenceParams
	if err := json.Unmarshal(data, &params2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !params2.Context.IncludeDeclaration {
		t.Error("IncludeDeclaration should be true")
	}
}

func TestRenameParams_MarshalJSON(t *testing.T) {
	params := RenameParams{
		TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///test.go"},
			Position:     Position{Line: 5, Character: 10},
		},
		NewName: "newFunctionName",
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var params2 RenameParams
	if err := json.Unmarshal(data, &params2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if params2.NewName != "newFunctionName" {
		t.Errorf("NewName mismatch: got %s, want newFunctionName", params2.NewName)
	}
}

func TestHoverResult_MarshalJSON(t *testing.T) {
	hover := HoverResult{
		Contents: MarkupContent{
			Kind:  "markdown",
			Value: "```go\nfunc foo() string\n```",
		},
		Range: &Range{
			Start: Position{Line: 5, Character: 0},
			End:   Position{Line: 5, Character: 3},
		},
	}

	data, err := json.Marshal(hover)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var hover2 HoverResult
	if err := json.Unmarshal(data, &hover2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if hover2.Contents.Kind != "markdown" {
		t.Errorf("Contents.Kind mismatch: got %s, want markdown", hover2.Contents.Kind)
	}
	if hover2.Range == nil {
		t.Error("Range should not be nil")
	}
}

func TestWorkspaceEdit_MarshalJSON(t *testing.T) {
	edit := WorkspaceEdit{
		Changes: map[string][]TextEdit{
			"file:///test.go": {
				{
					Range:   Range{Start: Position{0, 0}, End: Position{0, 5}},
					NewText: "hello",
				},
			},
		},
	}

	data, err := json.Marshal(edit)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var edit2 WorkspaceEdit
	if err := json.Unmarshal(data, &edit2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(edit2.Changes) != 1 {
		t.Errorf("Changes length mismatch: got %d, want 1", len(edit2.Changes))
	}
	if edits, ok := edit2.Changes["file:///test.go"]; !ok || len(edits) != 1 {
		t.Error("expected one edit for file:///test.go")
	}
}

func TestSymbolInformation_MarshalJSON(t *testing.T) {
	sym := SymbolInformation{
		Name: "TestFunction",
		Kind: SymbolKindFunction,
		Location: Location{
			URI: "file:///test.go",
			Range: Range{
				Start: Position{Line: 10, Character: 0},
				End:   Position{Line: 15, Character: 1},
			},
		},
		ContainerName: "main",
	}

	data, err := json.Marshal(sym)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var sym2 SymbolInformation
	if err := json.Unmarshal(data, &sym2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if sym2.Name != "TestFunction" {
		t.Errorf("Name mismatch: got %s, want TestFunction", sym2.Name)
	}
	if sym2.Kind != SymbolKindFunction {
		t.Errorf("Kind mismatch: got %d, want %d", sym2.Kind, SymbolKindFunction)
	}
}

func TestInitializeParams_MarshalJSON(t *testing.T) {
	params := InitializeParams{
		ProcessID: 12345,
		RootURI:   "file:///workspace",
		Capabilities: ClientCapabilities{
			TextDocument: TextDocumentClientCapabilities{
				Definition: &DefinitionCapabilities{},
				Hover: &HoverCapabilities{
					ContentFormat: []string{"markdown", "plaintext"},
				},
			},
		},
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var params2 InitializeParams
	if err := json.Unmarshal(data, &params2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if params2.ProcessID != 12345 {
		t.Errorf("ProcessID mismatch: got %d, want 12345", params2.ProcessID)
	}
	if params2.RootURI != "file:///workspace" {
		t.Errorf("RootURI mismatch: got %s, want file:///workspace", params2.RootURI)
	}
}

func TestServerCapabilities_HasProviders(t *testing.T) {
	tests := []struct {
		name     string
		caps     ServerCapabilities
		checkDef bool
		checkRef bool
		checkHov bool
		checkRen bool
	}{
		{
			name:     "all true",
			caps:     ServerCapabilities{DefinitionProvider: true, ReferencesProvider: true, HoverProvider: true, RenameProvider: true},
			checkDef: true, checkRef: true, checkHov: true, checkRen: true,
		},
		{
			name:     "all false",
			caps:     ServerCapabilities{DefinitionProvider: false, ReferencesProvider: false},
			checkDef: false, checkRef: false, checkHov: false, checkRen: false,
		},
		{
			name:     "nil values",
			caps:     ServerCapabilities{},
			checkDef: false, checkRef: false, checkHov: false, checkRen: false,
		},
		{
			name:     "object values",
			caps:     ServerCapabilities{DefinitionProvider: map[string]interface{}{}},
			checkDef: true, checkRef: false, checkHov: false, checkRen: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.caps.HasDefinitionProvider(); got != tc.checkDef {
				t.Errorf("HasDefinitionProvider() = %v, want %v", got, tc.checkDef)
			}
			if got := tc.caps.HasReferencesProvider(); got != tc.checkRef {
				t.Errorf("HasReferencesProvider() = %v, want %v", got, tc.checkRef)
			}
			if got := tc.caps.HasHoverProvider(); got != tc.checkHov {
				t.Errorf("HasHoverProvider() = %v, want %v", got, tc.checkHov)
			}
			if got := tc.caps.HasRenameProvider(); got != tc.checkRen {
				t.Errorf("HasRenameProvider() = %v, want %v", got, tc.checkRen)
			}
		})
	}
}
