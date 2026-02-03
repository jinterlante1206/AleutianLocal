package ast

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBashParser_Language(t *testing.T) {
	parser := NewBashParser()
	if got := parser.Language(); got != "bash" {
		t.Errorf("Language() = %q, want %q", got, "bash")
	}
}

func TestBashParser_Extensions(t *testing.T) {
	parser := NewBashParser()
	exts := parser.Extensions()
	want := []string{".sh", ".bash", ".zsh"}

	if len(exts) != len(want) {
		t.Fatalf("Extensions() returned %d items, want %d", len(exts), len(want))
	}

	for i, ext := range exts {
		if ext != want[i] {
			t.Errorf("Extensions()[%d] = %q, want %q", i, ext, want[i])
		}
	}
}

func TestBashParser_Parse_Function(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	content := []byte(`#!/bin/bash

check_status() {
    echo "checking"
}

deploy() {
    echo "deploying"
}
`)

	result, err := parser.Parse(ctx, content, "script.sh")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	funcs := filterBashSymbolsByKind(result.Symbols, SymbolKindFunction)
	if len(funcs) != 2 {
		t.Fatalf("got %d functions, want 2", len(funcs))
	}

	names := make(map[string]bool)
	for _, f := range funcs {
		names[f.Name] = true
		if !strings.Contains(f.Signature, "()") {
			t.Errorf("function %s signature should contain '()': %s", f.Name, f.Signature)
		}
	}

	if !names["check_status"] {
		t.Error("missing function check_status")
	}
	if !names["deploy"] {
		t.Error("missing function deploy")
	}
}

func TestBashParser_Parse_Variable(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	content := []byte(`#!/bin/bash

DB_HOST="localhost"
DB_PORT=5432
CONFIG_PATH=/etc/app
`)

	result, err := parser.Parse(ctx, content, "script.sh")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	vars := filterBashSymbolsByKind(result.Symbols, SymbolKindVariable)
	if len(vars) != 3 {
		t.Fatalf("got %d variables, want 3", len(vars))
	}

	names := make(map[string]bool)
	for _, v := range vars {
		names[v.Name] = true
	}

	if !names["DB_HOST"] {
		t.Error("missing variable DB_HOST")
	}
	if !names["DB_PORT"] {
		t.Error("missing variable DB_PORT")
	}
	if !names["CONFIG_PATH"] {
		t.Error("missing variable CONFIG_PATH")
	}
}

func TestBashParser_Parse_ExportedVariable(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	content := []byte(`#!/bin/bash

export APP_ENV="production"
export PATH="/usr/local/bin:$PATH"
`)

	result, err := parser.Parse(ctx, content, "script.sh")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	vars := filterBashSymbolsByKind(result.Symbols, SymbolKindVariable)
	if len(vars) != 2 {
		t.Fatalf("got %d variables, want 2", len(vars))
	}

	for _, v := range vars {
		if !v.Exported {
			t.Errorf("variable %s should be exported", v.Name)
		}
	}
}

func TestBashParser_Parse_ReadonlyVariable(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	content := []byte(`#!/bin/bash

readonly CONFIG_PATH="/etc/app/config"
readonly VERSION="1.0.0"
`)

	result, err := parser.Parse(ctx, content, "script.sh")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	consts := filterBashSymbolsByKind(result.Symbols, SymbolKindConstant)
	if len(consts) != 2 {
		t.Fatalf("got %d constants, want 2", len(consts))
	}

	names := make(map[string]bool)
	for _, c := range consts {
		names[c.Name] = true
	}

	if !names["CONFIG_PATH"] {
		t.Error("missing constant CONFIG_PATH")
	}
	if !names["VERSION"] {
		t.Error("missing constant VERSION")
	}
}

func TestBashParser_Parse_Alias(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	content := []byte(`#!/bin/bash

alias ll='ls -la'
alias gst='git status'
`)

	result, err := parser.Parse(ctx, content, "script.sh")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	aliases := filterBashSymbolsByKind(result.Symbols, SymbolKindAlias)
	if len(aliases) != 2 {
		t.Fatalf("got %d aliases, want 2", len(aliases))
	}

	names := make(map[string]bool)
	for _, a := range aliases {
		names[a.Name] = true
		if !strings.HasPrefix(a.Signature, "alias ") {
			t.Errorf("alias signature should start with 'alias ': %s", a.Signature)
		}
	}

	if !names["ll"] {
		t.Error("missing alias ll")
	}
	if !names["gst"] {
		t.Error("missing alias gst")
	}
}

func TestBashParser_Parse_AliasDisabled(t *testing.T) {
	parser := NewBashParser(WithBashExtractAliases(false))
	ctx := context.Background()

	content := []byte(`#!/bin/bash

alias ll='ls -la'
`)

	result, err := parser.Parse(ctx, content, "script.sh")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	aliases := filterBashSymbolsByKind(result.Symbols, SymbolKindAlias)
	if len(aliases) != 0 {
		t.Errorf("got %d aliases, want 0 (extraction disabled)", len(aliases))
	}
}

func TestBashParser_Parse_SourceImport(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	content := []byte(`#!/bin/bash

source /etc/profile
. ~/.bashrc
source "$CONFIG_DIR/settings.sh"
`)

	result, err := parser.Parse(ctx, content, "script.sh")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(result.Imports) < 2 {
		t.Fatalf("got %d imports, want at least 2", len(result.Imports))
	}

	paths := make(map[string]bool)
	for _, imp := range result.Imports {
		paths[imp.Path] = true
		if !imp.IsScript {
			t.Errorf("import %q should have IsScript=true", imp.Path)
		}
	}

	if !paths["/etc/profile"] {
		t.Error("missing import for /etc/profile")
	}
	if !paths["~/.bashrc"] {
		t.Error("missing import for ~/.bashrc")
	}
}

func TestBashParser_Parse_EmptyContent(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	result, err := parser.Parse(ctx, []byte(""), "empty.sh")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(result.Symbols) != 0 {
		t.Errorf("got %d symbols, want 0", len(result.Symbols))
	}
	if result.Language != "bash" {
		t.Errorf("Language = %q, want %q", result.Language, "bash")
	}
}

func TestBashParser_Parse_ShebangOnly(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	content := []byte(`#!/bin/bash
`)

	result, err := parser.Parse(ctx, content, "shebang.sh")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(result.Symbols) != 0 {
		t.Errorf("got %d symbols, want 0", len(result.Symbols))
	}
}

func TestBashParser_Parse_CommentsOnly(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	content := []byte(`#!/bin/bash
# This is a comment
# Another comment
`)

	result, err := parser.Parse(ctx, content, "comments.sh")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Comments are not extracted as symbols
	if len(result.Symbols) != 0 {
		t.Errorf("got %d symbols, want 0", len(result.Symbols))
	}
}

func TestBashParser_Parse_ContextCancellation(t *testing.T) {
	parser := NewBashParser()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	content := []byte(`#!/bin/bash
echo "test"
`)

	_, err := parser.Parse(ctx, content, "test.sh")
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Errorf("error should mention cancellation: %v", err)
	}
}

func TestBashParser_Parse_FileTooLarge(t *testing.T) {
	parser := NewBashParser(WithBashMaxFileSize(100))
	ctx := context.Background()

	content := make([]byte, 200)
	for i := range content {
		content[i] = '#'
	}

	_, err := parser.Parse(ctx, content, "large.sh")
	if err != ErrFileTooLarge {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestBashParser_Parse_InvalidUTF8(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	content := []byte{0xff, 0xfe, 0x00, 0x01}

	_, err := parser.Parse(ctx, content, "invalid.sh")
	if err != ErrInvalidContent {
		t.Errorf("expected ErrInvalidContent, got %v", err)
	}
}

func TestBashParser_Parse_HashComputation(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	content := []byte(`#!/bin/bash
echo "hello"
`)

	result, err := parser.Parse(ctx, content, "test.sh")
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

func TestBashParser_Parse_TimestampSet(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	before := time.Now().UnixMilli()
	result, err := parser.Parse(ctx, []byte(`echo "test"`), "test.sh")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	after := time.Now().UnixMilli()

	if result.ParsedAtMilli < before || result.ParsedAtMilli > after {
		t.Errorf("ParsedAtMilli = %d, want between %d and %d", result.ParsedAtMilli, before, after)
	}
}

func TestBashParser_Parse_LineNumbers(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	content := []byte(`#!/bin/bash
# Comment
# Another comment
my_func() {
    echo "hello"
}
`)

	result, err := parser.Parse(ctx, content, "test.sh")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	funcs := filterBashSymbolsByKind(result.Symbols, SymbolKindFunction)
	if len(funcs) != 1 {
		t.Fatalf("got %d functions, want 1", len(funcs))
	}

	if funcs[0].StartLine != 4 {
		t.Errorf("function StartLine = %d, want 4", funcs[0].StartLine)
	}
}

func TestBashParser_Parse_SymbolLanguage(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	content := []byte(`#!/bin/bash
MY_VAR="value"
my_func() { echo "test"; }
`)

	result, err := parser.Parse(ctx, content, "test.sh")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	for _, sym := range result.Symbols {
		if sym.Language != "bash" {
			t.Errorf("symbol %s Language = %q, want %q", sym.Name, sym.Language, "bash")
		}
	}
}

func TestBashParser_Parse_FilePath(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	content := []byte(`#!/bin/bash
MY_VAR="value"
`)
	filePath := "scripts/deploy.sh"

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

func TestBashParser_ConcurrentParsing(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	contents := [][]byte{
		[]byte(`MY_VAR1="value1"`),
		[]byte(`MY_VAR2="value2"`),
		[]byte(`func1() { echo "1"; }`),
		[]byte(`func2() { echo "2"; }`),
		[]byte(`alias ll='ls -la'`),
	}

	results := make(chan error, len(contents))

	for i, content := range contents {
		go func(idx int, c []byte) {
			_, err := parser.Parse(ctx, c, "concurrent.sh")
			results <- err
		}(i, content)
	}

	for range contents {
		if err := <-results; err != nil {
			t.Errorf("concurrent parse error: %v", err)
		}
	}
}

func TestBashParser_Parse_VariableSignature(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	content := []byte(`#!/bin/bash
DB_HOST="localhost"
DB_PORT=5432
`)

	result, err := parser.Parse(ctx, content, "test.sh")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindVariable {
			if !strings.Contains(sym.Signature, "=") {
				t.Errorf("variable %s signature should contain '=': %s", sym.Name, sym.Signature)
			}
		}
	}
}

func TestBashParser_Parse_LocalVariablesSkipped(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	content := []byte(`#!/bin/bash
GLOBAL_VAR="global"

my_func() {
    local local_var="local"
    echo "$local_var"
}
`)

	result, err := parser.Parse(ctx, content, "test.sh")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Should have global var and function, but not local var
	vars := filterBashSymbolsByKind(result.Symbols, SymbolKindVariable)
	if len(vars) != 1 {
		t.Errorf("got %d variables, want 1 (local vars skipped)", len(vars))
	}
	if len(vars) > 0 && vars[0].Name != "GLOBAL_VAR" {
		t.Errorf("expected GLOBAL_VAR, got %s", vars[0].Name)
	}
}

func TestBashParser_Parse_FunctionExported(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	content := []byte(`#!/bin/bash
my_func() {
    echo "test"
}
`)

	result, err := parser.Parse(ctx, content, "test.sh")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	funcs := filterBashSymbolsByKind(result.Symbols, SymbolKindFunction)
	if len(funcs) != 1 {
		t.Fatalf("got %d functions, want 1", len(funcs))
	}

	if !funcs[0].Exported {
		t.Error("function should be exported (bash functions are globally accessible)")
	}
}

func TestBashParser_Parse_MixedContent(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	content := []byte(`#!/bin/bash

# Configuration
export APP_ENV="production"
readonly VERSION="1.0.0"

# Helper function
log() {
    echo "[$(date)] $1"
}

# Alias
alias ll='ls -la'

# Main script
source /etc/profile
log "Starting..."
`)

	result, err := parser.Parse(ctx, content, "script.sh")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	vars := filterBashSymbolsByKind(result.Symbols, SymbolKindVariable)
	consts := filterBashSymbolsByKind(result.Symbols, SymbolKindConstant)
	funcs := filterBashSymbolsByKind(result.Symbols, SymbolKindFunction)
	aliases := filterBashSymbolsByKind(result.Symbols, SymbolKindAlias)

	if len(vars) != 1 {
		t.Errorf("got %d variables, want 1 (APP_ENV)", len(vars))
	}
	if len(consts) != 1 {
		t.Errorf("got %d constants, want 1 (VERSION)", len(consts))
	}
	if len(funcs) != 1 {
		t.Errorf("got %d functions, want 1 (log)", len(funcs))
	}
	if len(aliases) != 1 {
		t.Errorf("got %d aliases, want 1 (ll)", len(aliases))
	}
	if len(result.Imports) != 1 {
		t.Errorf("got %d imports, want 1", len(result.Imports))
	}
}

func TestBashParser_DefaultOptions(t *testing.T) {
	opts := DefaultBashParserOptions()

	if opts.MaxFileSize != 10*1024*1024 {
		t.Errorf("MaxFileSize = %d, want %d", opts.MaxFileSize, 10*1024*1024)
	}
	if !opts.ExtractAliases {
		t.Error("ExtractAliases should be true by default")
	}
}

func TestBashParser_Parse_LongVariableValueTruncation(t *testing.T) {
	parser := NewBashParser()
	ctx := context.Background()

	longValue := strings.Repeat("x", 100)
	content := []byte(`#!/bin/bash
LONG_VAR="` + longValue + `"
`)

	result, err := parser.Parse(ctx, content, "test.sh")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	vars := filterBashSymbolsByKind(result.Symbols, SymbolKindVariable)
	if len(vars) != 1 {
		t.Fatalf("got %d variables, want 1", len(vars))
	}

	// Signature should be truncated
	if len(vars[0].Signature) > 70 {
		t.Errorf("signature too long (%d chars), should be truncated", len(vars[0].Signature))
	}
}

// Helper function for filtering symbols
func filterBashSymbolsByKind(symbols []*Symbol, kind SymbolKind) []*Symbol {
	var result []*Symbol
	for _, sym := range symbols {
		if sym.Kind == kind {
			result = append(result, sym)
		}
	}
	return result
}
