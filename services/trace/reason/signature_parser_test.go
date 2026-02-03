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
	"testing"
)

func TestSignatureParser_ParseGoSignature(t *testing.T) {
	parser := NewSignatureParser()

	t.Run("simple function", func(t *testing.T) {
		sig, err := parser.ParseSignature("func Add(a, b int) int", "go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sig.Name != "Add" {
			t.Errorf("expected name Add, got %s", sig.Name)
		}
		if sig.Language != "go" {
			t.Errorf("expected language go, got %s", sig.Language)
		}
	})

	t.Run("function with context", func(t *testing.T) {
		sig, err := parser.ParseSignature(
			"func Process(ctx context.Context, data []byte) error", "go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sig.Parameters) < 2 {
			t.Errorf("expected at least 2 parameters, got %d", len(sig.Parameters))
		}
		if len(sig.Returns) == 0 {
			t.Error("expected return type")
		}
	})

	t.Run("method with receiver", func(t *testing.T) {
		sig, err := parser.ParseSignature(
			"func (s *Service) HandleRequest(req *Request) (*Response, error)", "go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sig.Receiver == nil {
			t.Error("expected receiver to be set")
		} else if !sig.Receiver.IsPointer {
			t.Error("expected receiver to be pointer type")
		}
		if len(sig.Returns) != 2 {
			t.Errorf("expected 2 returns, got %d", len(sig.Returns))
		}
	})

	t.Run("function type literal", func(t *testing.T) {
		sig, err := parser.ParseSignature(
			"func(string) bool", "go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sig.Parameters) != 1 {
			t.Errorf("expected 1 parameter, got %d", len(sig.Parameters))
		}
		if len(sig.Returns) != 1 {
			t.Errorf("expected 1 return, got %d", len(sig.Returns))
		}
	})

	t.Run("variadic function", func(t *testing.T) {
		sig, err := parser.ParseSignature(
			"func Printf(format string, args ...interface{})", "go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sig.Parameters) < 2 {
			t.Errorf("expected at least 2 parameters, got %d", len(sig.Parameters))
		}
		// Check if any parameter is variadic
		hasVariadic := false
		for _, p := range sig.Parameters {
			if p.Type.IsVariadic {
				hasVariadic = true
				break
			}
		}
		if !hasVariadic {
			t.Error("expected variadic parameter")
		}
	})

	t.Run("no return type", func(t *testing.T) {
		sig, err := parser.ParseSignature("func Init()", "go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sig.Returns) != 0 {
			t.Errorf("expected 0 returns, got %d", len(sig.Returns))
		}
	})
}

func TestSignatureParser_ParsePythonSignature(t *testing.T) {
	parser := NewSignatureParser()

	t.Run("simple function", func(t *testing.T) {
		sig, err := parser.ParseSignature("def add(a, b)", "python")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sig.Name != "add" {
			t.Errorf("expected name add, got %s", sig.Name)
		}
		if len(sig.Parameters) != 2 {
			t.Errorf("expected 2 parameters, got %d", len(sig.Parameters))
		}
	})

	t.Run("function with type hints", func(t *testing.T) {
		sig, err := parser.ParseSignature(
			"def process(data: bytes) -> str", "python")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sig.Parameters) != 1 {
			t.Errorf("expected 1 parameter, got %d", len(sig.Parameters))
		}
		if len(sig.Returns) == 0 {
			t.Error("expected return type")
		}
	})

	t.Run("function with defaults", func(t *testing.T) {
		sig, err := parser.ParseSignature(
			"def connect(host: str, port: int = 8080)", "python")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sig.Parameters) != 2 {
			t.Errorf("expected 2 parameters, got %d", len(sig.Parameters))
		}
		// Check if port has default
		hasOptional := false
		for _, p := range sig.Parameters {
			if p.Optional {
				hasOptional = true
				break
			}
		}
		if !hasOptional {
			t.Error("expected optional parameter")
		}
	})
}

func TestSignatureParser_ParseTypeScriptSignature(t *testing.T) {
	parser := NewSignatureParser()

	t.Run("simple function", func(t *testing.T) {
		sig, err := parser.ParseSignature(
			"function add(a: number, b: number): number", "typescript")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sig.Name != "add" {
			t.Errorf("expected name add, got %s", sig.Name)
		}
	})

	t.Run("arrow function", func(t *testing.T) {
		sig, err := parser.ParseSignature(
			"(x: string) => boolean", "typescript")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sig.Parameters) != 1 {
			t.Errorf("expected 1 parameter, got %d", len(sig.Parameters))
		}
	})
}

func TestSignatureParser_Errors(t *testing.T) {
	parser := NewSignatureParser()

	t.Run("empty signature", func(t *testing.T) {
		_, err := parser.ParseSignature("", "go")
		if err != ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("unsupported language", func(t *testing.T) {
		_, err := parser.ParseSignature("func foo()", "cobol")
		if err != ErrUnsupportedLanguage {
			t.Errorf("expected ErrUnsupportedLanguage, got %v", err)
		}
	})
}

func TestCompareSignatures(t *testing.T) {
	t.Run("identical signatures", func(t *testing.T) {
		current := &ParsedSignature{
			Name:       "Foo",
			Parameters: []ParameterInfo{{Name: "x", Type: TypeInfo{Name: "int"}}},
			Returns:    []TypeInfo{{Name: "error"}},
		}
		proposed := &ParsedSignature{
			Name:       "Foo",
			Parameters: []ParameterInfo{{Name: "x", Type: TypeInfo{Name: "int"}}},
			Returns:    []TypeInfo{{Name: "error"}},
		}

		changes := CompareSignatures(current, proposed)
		if len(changes) != 0 {
			t.Errorf("expected no changes, got %d", len(changes))
		}
	})

	t.Run("added required parameter", func(t *testing.T) {
		current := &ParsedSignature{
			Parameters: []ParameterInfo{{Name: "x", Type: TypeInfo{Name: "int"}}},
		}
		proposed := &ParsedSignature{
			Parameters: []ParameterInfo{
				{Name: "x", Type: TypeInfo{Name: "int"}},
				{Name: "y", Type: TypeInfo{Name: "string"}},
			},
		}

		changes := CompareSignatures(current, proposed)
		if len(changes) == 0 {
			t.Error("expected breaking change for added parameter")
		}
	})

	t.Run("removed parameter", func(t *testing.T) {
		current := &ParsedSignature{
			Parameters: []ParameterInfo{
				{Name: "x", Type: TypeInfo{Name: "int"}},
				{Name: "y", Type: TypeInfo{Name: "string"}},
			},
		}
		proposed := &ParsedSignature{
			Parameters: []ParameterInfo{{Name: "x", Type: TypeInfo{Name: "int"}}},
		}

		changes := CompareSignatures(current, proposed)
		if len(changes) == 0 {
			t.Error("expected breaking change for removed parameter")
		}
	})

	t.Run("changed parameter type", func(t *testing.T) {
		current := &ParsedSignature{
			Parameters: []ParameterInfo{{Name: "x", Type: TypeInfo{Name: "int"}}},
		}
		proposed := &ParsedSignature{
			Parameters: []ParameterInfo{{Name: "x", Type: TypeInfo{Name: "string"}}},
		}

		changes := CompareSignatures(current, proposed)
		if len(changes) == 0 {
			t.Error("expected breaking change for type change")
		}
	})

	t.Run("changed return type", func(t *testing.T) {
		current := &ParsedSignature{
			Returns: []TypeInfo{{Name: "int"}},
		}
		proposed := &ParsedSignature{
			Returns: []TypeInfo{{Name: "string"}},
		}

		changes := CompareSignatures(current, proposed)
		if len(changes) == 0 {
			t.Error("expected breaking change for return type change")
		}
	})

	t.Run("method to function conversion", func(t *testing.T) {
		current := &ParsedSignature{
			Receiver: &TypeInfo{Name: "*Service"},
		}
		proposed := &ParsedSignature{
			Receiver: nil,
		}

		changes := CompareSignatures(current, proposed)
		if len(changes) == 0 {
			t.Error("expected breaking change for method→function conversion")
		}
		// Should be critical severity
		hasCritical := false
		for _, c := range changes {
			if c.Severity == SeverityCritical {
				hasCritical = true
				break
			}
		}
		if !hasCritical {
			t.Error("expected critical severity for method→function conversion")
		}
	})

	t.Run("nil inputs", func(t *testing.T) {
		changes := CompareSignatures(nil, nil)
		if len(changes) != 0 {
			t.Error("expected no changes for nil inputs")
		}

		changes = CompareSignatures(&ParsedSignature{}, nil)
		if len(changes) != 0 {
			t.Error("expected no changes when proposed is nil")
		}
	})
}

func TestTypesEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        TypeInfo
		b        TypeInfo
		expected bool
	}{
		{
			name:     "identical simple types",
			a:        TypeInfo{Name: "int"},
			b:        TypeInfo{Name: "int"},
			expected: true,
		},
		{
			name:     "different names",
			a:        TypeInfo{Name: "int"},
			b:        TypeInfo{Name: "string"},
			expected: false,
		},
		{
			name:     "pointer vs non-pointer",
			a:        TypeInfo{Name: "T", IsPointer: true},
			b:        TypeInfo{Name: "T", IsPointer: false},
			expected: false,
		},
		{
			name:     "slice vs non-slice",
			a:        TypeInfo{Name: "[]T", IsSlice: true},
			b:        TypeInfo{Name: "[]T", IsSlice: false},
			expected: false,
		},
		{
			name:     "variadic difference",
			a:        TypeInfo{Name: "T", IsVariadic: true},
			b:        TypeInfo{Name: "T", IsVariadic: false},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := typesEqual(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("typesEqual(%v, %v) = %v, want %v",
					tt.a, tt.b, result, tt.expected)
			}
		})
	}
}
