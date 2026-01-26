package ast

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestYAMLParser_Language(t *testing.T) {
	parser := NewYAMLParser()
	if got := parser.Language(); got != "yaml" {
		t.Errorf("Language() = %q, want %q", got, "yaml")
	}
}

func TestYAMLParser_Extensions(t *testing.T) {
	parser := NewYAMLParser()
	exts := parser.Extensions()
	want := []string{".yaml", ".yml"}

	if len(exts) != len(want) {
		t.Fatalf("Extensions() returned %d items, want %d", len(exts), len(want))
	}

	for i, ext := range exts {
		if ext != want[i] {
			t.Errorf("Extensions()[%d] = %q, want %q", i, ext, want[i])
		}
	}
}

func TestYAMLParser_Parse_TopLevelKeys(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	content := []byte(`name: my-app
version: "1.0.0"
debug: true
`)

	result, err := parser.Parse(ctx, content, "config.yaml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	keys := filterYAMLSymbolsByKind(result.Symbols, SymbolKindKey)
	if len(keys) != 3 {
		t.Fatalf("got %d keys, want 3", len(keys))
	}

	names := make(map[string]bool)
	for _, k := range keys {
		names[k.Name] = true
	}

	wantKeys := []string{"name", "version", "debug"}
	for _, key := range wantKeys {
		if !names[key] {
			t.Errorf("missing key %q", key)
		}
	}
}

func TestYAMLParser_Parse_NestedKeys(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	content := []byte(`database:
  host: localhost
  port: 5432
`)

	result, err := parser.Parse(ctx, content, "config.yaml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	keys := filterYAMLSymbolsByKind(result.Symbols, SymbolKindKey)
	// Should have: database, host, port
	if len(keys) < 3 {
		t.Fatalf("got %d keys, want at least 3", len(keys))
	}

	// Check nested keys have parent metadata
	for _, k := range keys {
		if k.Name == "host" || k.Name == "port" {
			if k.Metadata == nil || k.Metadata.ParentName != "database" {
				t.Errorf("key %q should have ParentName=database", k.Name)
			}
		}
	}
}

func TestYAMLParser_Parse_DeeplyNestedKeys(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	content := []byte(`level1:
  level2:
    level3:
      value: deep
`)

	result, err := parser.Parse(ctx, content, "config.yaml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// With default MaxKeyDepth=3, should extract level1, level2, level3, value
	keys := filterYAMLSymbolsByKind(result.Symbols, SymbolKindKey)
	if len(keys) < 4 {
		t.Fatalf("got %d keys, want at least 4", len(keys))
	}

	names := make(map[string]bool)
	for _, k := range keys {
		names[k.Name] = true
	}

	wantKeys := []string{"level1", "level2", "level3", "value"}
	for _, key := range wantKeys {
		if !names[key] {
			t.Errorf("missing key %q", key)
		}
	}
}

func TestYAMLParser_Parse_MaxKeyDepth(t *testing.T) {
	parser := NewYAMLParser(WithYAMLMaxKeyDepth(1))
	ctx := context.Background()

	content := []byte(`level1:
  level2:
    level3:
      level4: deep
`)

	result, err := parser.Parse(ctx, content, "config.yaml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// With MaxKeyDepth=1, should only extract level1, level2
	keys := filterYAMLSymbolsByKind(result.Symbols, SymbolKindKey)

	names := make(map[string]bool)
	for _, k := range keys {
		names[k.Name] = true
	}

	if names["level3"] || names["level4"] {
		t.Error("should not extract keys beyond depth 1")
	}
}

func TestYAMLParser_Parse_Anchor(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	content := []byte(`defaults: &defaults
  timeout: 30
  retries: 3

production:
  <<: *defaults
  timeout: 60
`)

	result, err := parser.Parse(ctx, content, "config.yaml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	anchors := filterYAMLSymbolsByKind(result.Symbols, SymbolKindAnchor)
	if len(anchors) != 1 {
		t.Fatalf("got %d anchors, want 1", len(anchors))
	}

	if anchors[0].Name != "defaults" {
		t.Errorf("anchor name = %q, want %q", anchors[0].Name, "defaults")
	}
	if !strings.Contains(anchors[0].Signature, "&defaults") {
		t.Errorf("anchor signature should contain '&defaults': %s", anchors[0].Signature)
	}
}

func TestYAMLParser_Parse_AnchorDisabled(t *testing.T) {
	parser := NewYAMLParser(WithYAMLExtractAnchors(false))
	ctx := context.Background()

	content := []byte(`defaults: &defaults
  timeout: 30
`)

	result, err := parser.Parse(ctx, content, "config.yaml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	anchors := filterYAMLSymbolsByKind(result.Symbols, SymbolKindAnchor)
	if len(anchors) != 0 {
		t.Errorf("got %d anchors, want 0 (extraction disabled)", len(anchors))
	}
}

func TestYAMLParser_Parse_MultiDocument(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	content := []byte(`name: first
---
name: second
---
name: third
`)

	result, err := parser.Parse(ctx, content, "multi.yaml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// First document doesn't get a Document symbol, only 2nd and 3rd
	docs := filterYAMLSymbolsByKind(result.Symbols, SymbolKindDocument)
	if len(docs) != 2 {
		t.Fatalf("got %d document symbols, want 2", len(docs))
	}
}

func TestYAMLParser_Parse_EmptyContent(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	result, err := parser.Parse(ctx, []byte(""), "empty.yaml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(result.Symbols) != 0 {
		t.Errorf("got %d symbols, want 0", len(result.Symbols))
	}
	if result.Language != "yaml" {
		t.Errorf("Language = %q, want %q", result.Language, "yaml")
	}
}

func TestYAMLParser_Parse_CommentsOnly(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	content := []byte(`# This is a comment
# Another comment
`)

	result, err := parser.Parse(ctx, content, "comments.yaml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Comments are not extracted as symbols
	if len(result.Symbols) != 0 {
		t.Errorf("got %d symbols, want 0", len(result.Symbols))
	}
}

func TestYAMLParser_Parse_ContextCancellation(t *testing.T) {
	parser := NewYAMLParser()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	content := []byte(`name: test`)

	_, err := parser.Parse(ctx, content, "test.yaml")
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Errorf("error should mention cancellation: %v", err)
	}
}

func TestYAMLParser_Parse_FileTooLarge(t *testing.T) {
	parser := NewYAMLParser(WithYAMLMaxFileSize(100))
	ctx := context.Background()

	content := make([]byte, 200)
	for i := range content {
		content[i] = '#'
	}

	_, err := parser.Parse(ctx, content, "large.yaml")
	if err != ErrFileTooLarge {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestYAMLParser_Parse_InvalidUTF8(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	content := []byte{0xff, 0xfe, 0x00, 0x01}

	_, err := parser.Parse(ctx, content, "invalid.yaml")
	if err != ErrInvalidContent {
		t.Errorf("expected ErrInvalidContent, got %v", err)
	}
}

func TestYAMLParser_Parse_HashComputation(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	content := []byte(`name: test`)

	result, err := parser.Parse(ctx, content, "test.yaml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if result.Hash == "" {
		t.Error("Hash should not be empty")
	}
	if len(result.Hash) != 64 {
		t.Errorf("Hash length = %d, want 64 (SHA-256 hex)", len(result.Hash))
	}
}

func TestYAMLParser_Parse_TimestampSet(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	before := time.Now().UnixMilli()
	result, err := parser.Parse(ctx, []byte(`key: value`), "test.yaml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	after := time.Now().UnixMilli()

	if result.ParsedAtMilli < before || result.ParsedAtMilli > after {
		t.Errorf("ParsedAtMilli = %d, want between %d and %d", result.ParsedAtMilli, before, after)
	}
}

func TestYAMLParser_Parse_LineNumbers(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	content := []byte(`# Comment
# Another comment
name: value
`)

	result, err := parser.Parse(ctx, content, "test.yaml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	keys := filterYAMLSymbolsByKind(result.Symbols, SymbolKindKey)
	if len(keys) != 1 {
		t.Fatalf("got %d keys, want 1", len(keys))
	}

	if keys[0].StartLine != 3 {
		t.Errorf("key StartLine = %d, want 3", keys[0].StartLine)
	}
}

func TestYAMLParser_Parse_SymbolLanguage(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	content := []byte(`name: test`)

	result, err := parser.Parse(ctx, content, "test.yaml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	for _, sym := range result.Symbols {
		if sym.Language != "yaml" {
			t.Errorf("symbol %s Language = %q, want %q", sym.Name, sym.Language, "yaml")
		}
	}
}

func TestYAMLParser_Parse_FilePath(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	content := []byte(`name: test`)
	filePath := "config/app.yaml"

	result, err := parser.Parse(ctx, content, filePath)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if result.FilePath != filePath {
		t.Errorf("FilePath = %q, want %q", result.FilePath, filePath)
	}

	for _, sym := range result.Symbols {
		if sym.FilePath != filePath {
			t.Errorf("symbol FilePath = %q, want %q", sym.FilePath, filePath)
		}
	}
}

func TestYAMLParser_ConcurrentParsing(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	contents := [][]byte{
		[]byte(`name: test1`),
		[]byte(`name: test2`),
		[]byte(`name: test3`),
		[]byte(`a: 1\nb: 2`),
		[]byte(`key: value`),
	}

	results := make(chan error, len(contents))

	for i, content := range contents {
		go func(idx int, c []byte) {
			_, err := parser.Parse(ctx, c, "concurrent.yaml")
			results <- err
		}(i, content)
	}

	for range contents {
		if err := <-results; err != nil {
			t.Errorf("concurrent parse error: %v", err)
		}
	}
}

func TestYAMLParser_Parse_KeySignature(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	content := []byte(`name: my-app
count: 42
`)

	result, err := parser.Parse(ctx, content, "test.yaml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindKey {
			if !strings.Contains(sym.Signature, ":") {
				t.Errorf("key %s signature should contain ':': %s", sym.Name, sym.Signature)
			}
		}
	}
}

func TestYAMLParser_Parse_NestedObjectSignature(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	content := []byte(`database:
  host: localhost
`)

	result, err := parser.Parse(ctx, content, "test.yaml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Find the database key
	for _, sym := range result.Symbols {
		if sym.Name == "database" && sym.Kind == SymbolKindKey {
			// Should have "..." in signature for nested object
			if !strings.Contains(sym.Signature, "...") {
				t.Errorf("nested key signature should contain '...': %s", sym.Signature)
			}
		}
	}
}

func TestYAMLParser_Parse_QuotedValues(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	content := []byte(`single: 'single quoted'
double: "double quoted"
plain: plain value
`)

	result, err := parser.Parse(ctx, content, "test.yaml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	keys := filterYAMLSymbolsByKind(result.Symbols, SymbolKindKey)
	if len(keys) != 3 {
		t.Fatalf("got %d keys, want 3", len(keys))
	}
}

func TestYAMLParser_Parse_SymbolExported(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	content := []byte(`name: test`)

	result, err := parser.Parse(ctx, content, "test.yaml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	for _, sym := range result.Symbols {
		if !sym.Exported {
			t.Errorf("symbol %q should be exported", sym.Name)
		}
	}
}

func TestYAMLParser_DefaultOptions(t *testing.T) {
	opts := DefaultYAMLParserOptions()

	if opts.MaxFileSize != 10*1024*1024 {
		t.Errorf("MaxFileSize = %d, want %d", opts.MaxFileSize, 10*1024*1024)
	}
	if opts.MaxKeyDepth != 3 {
		t.Errorf("MaxKeyDepth = %d, want 3", opts.MaxKeyDepth)
	}
	if !opts.ExtractAnchors {
		t.Error("ExtractAnchors should be true by default")
	}
}

func TestYAMLParser_Parse_ComplexDocument(t *testing.T) {
	parser := NewYAMLParser()
	ctx := context.Background()

	content := []byte(`# Application Config
name: my-app
version: "1.0.0"

database: &db_config
  host: localhost
  port: 5432
  credentials:
    username: admin
    password: secret

server:
  port: 8080
  endpoints:
    - /api/v1
    - /api/v2

production:
  <<: *db_config
  host: prod.example.com
`)

	result, err := parser.Parse(ctx, content, "config.yaml")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	keys := filterYAMLSymbolsByKind(result.Symbols, SymbolKindKey)
	anchors := filterYAMLSymbolsByKind(result.Symbols, SymbolKindAnchor)

	// Should have many keys at various levels
	if len(keys) < 10 {
		t.Errorf("got %d keys, want at least 10", len(keys))
	}

	// Should have one anchor
	if len(anchors) != 1 {
		t.Errorf("got %d anchors, want 1", len(anchors))
	}

	// Verify some specific keys exist
	keyNames := make(map[string]bool)
	for _, k := range keys {
		keyNames[k.Name] = true
	}

	expectedKeys := []string{"name", "version", "database", "host", "port", "server", "production"}
	for _, expected := range expectedKeys {
		if !keyNames[expected] {
			t.Errorf("missing key %q", expected)
		}
	}
}

// Helper function for filtering symbols
func filterYAMLSymbolsByKind(symbols []*Symbol, kind SymbolKind) []*Symbol {
	var result []*Symbol
	for _, sym := range symbols {
		if sym.Kind == kind {
			result = append(result, sym)
		}
	}
	return result
}
