package ast

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDockerfileParser_Language(t *testing.T) {
	parser := NewDockerfileParser()
	if got := parser.Language(); got != "dockerfile" {
		t.Errorf("Language() = %q, want %q", got, "dockerfile")
	}
}

func TestDockerfileParser_Extensions(t *testing.T) {
	parser := NewDockerfileParser()
	exts := parser.Extensions()
	want := []string{"Dockerfile", ".dockerfile"}

	if len(exts) != len(want) {
		t.Fatalf("Extensions() returned %d items, want %d", len(exts), len(want))
	}

	for i, ext := range exts {
		if ext != want[i] {
			t.Errorf("Extensions()[%d] = %q, want %q", i, ext, want[i])
		}
	}
}

func TestDockerfileParser_Parse_FromStage(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	content := []byte(`FROM golang:1.21-alpine AS builder
RUN go build

FROM alpine:3.18 AS production
COPY --from=builder /app/main /main
`)

	result, err := parser.Parse(ctx, content, "Dockerfile")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	stages := filterDockerSymbolsByKind(result.Symbols, SymbolKindStage)
	if len(stages) != 2 {
		t.Fatalf("got %d stages, want 2", len(stages))
	}

	names := make(map[string]bool)
	for _, s := range stages {
		names[s.Name] = true
	}

	if !names["builder"] {
		t.Error("missing stage 'builder'")
	}
	if !names["production"] {
		t.Error("missing stage 'production'")
	}
}

func TestDockerfileParser_Parse_FromImport(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	content := []byte(`FROM golang:1.21-alpine
FROM alpine:3.18
`)

	result, err := parser.Parse(ctx, content, "Dockerfile")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(result.Imports) != 2 {
		t.Fatalf("got %d imports, want 2", len(result.Imports))
	}

	paths := make(map[string]bool)
	for _, imp := range result.Imports {
		paths[imp.Path] = true
	}

	if !paths["golang:1.21-alpine"] {
		t.Error("missing import for golang:1.21-alpine")
	}
	if !paths["alpine:3.18"] {
		t.Error("missing import for alpine:3.18")
	}
}

func TestDockerfileParser_Parse_Arg(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	content := []byte(`FROM alpine
ARG VERSION=1.0.0
ARG BUILD_DATE
`)

	result, err := parser.Parse(ctx, content, "Dockerfile")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	args := filterDockerSymbolsByKind(result.Symbols, SymbolKindArg)
	if len(args) != 2 {
		t.Fatalf("got %d ARGs, want 2", len(args))
	}

	names := make(map[string]bool)
	for _, a := range args {
		names[a.Name] = true
		if a.Name == "VERSION" && !strings.Contains(a.Signature, "1.0.0") {
			t.Errorf("VERSION should have default value in signature: %s", a.Signature)
		}
	}

	if !names["VERSION"] {
		t.Error("missing ARG VERSION")
	}
	if !names["BUILD_DATE"] {
		t.Error("missing ARG BUILD_DATE")
	}
}

func TestDockerfileParser_Parse_Env(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	content := []byte(`FROM alpine
ENV GO111MODULE=on
ENV CGO_ENABLED=0
`)

	result, err := parser.Parse(ctx, content, "Dockerfile")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	envs := filterDockerSymbolsByKind(result.Symbols, SymbolKindEnvVar)
	if len(envs) != 2 {
		t.Fatalf("got %d ENVs, want 2", len(envs))
	}

	names := make(map[string]bool)
	for _, e := range envs {
		names[e.Name] = true
	}

	if !names["GO111MODULE"] {
		t.Error("missing ENV GO111MODULE")
	}
	if !names["CGO_ENABLED"] {
		t.Error("missing ENV CGO_ENABLED")
	}
}

func TestDockerfileParser_Parse_Label(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	content := []byte(`FROM alpine
LABEL maintainer="dev@example.com"
LABEL version="1.0"
`)

	result, err := parser.Parse(ctx, content, "Dockerfile")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	labels := filterDockerSymbolsByKind(result.Symbols, SymbolKindLabel)
	if len(labels) != 2 {
		t.Fatalf("got %d LABELs, want 2", len(labels))
	}

	names := make(map[string]bool)
	for _, l := range labels {
		names[l.Name] = true
	}

	if !names["maintainer"] {
		t.Error("missing LABEL maintainer")
	}
	if !names["version"] {
		t.Error("missing LABEL version")
	}
}

func TestDockerfileParser_Parse_Expose(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	content := []byte(`FROM alpine
EXPOSE 8080
EXPOSE 8443/tcp
`)

	result, err := parser.Parse(ctx, content, "Dockerfile")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	ports := filterDockerSymbolsByKind(result.Symbols, SymbolKindPort)
	if len(ports) != 2 {
		t.Fatalf("got %d ports, want 2", len(ports))
	}

	portNames := make(map[string]bool)
	for _, p := range ports {
		portNames[p.Name] = true
	}

	if !portNames["8080"] {
		t.Error("missing port 8080")
	}
}

func TestDockerfileParser_Parse_Volume(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	content := []byte(`FROM alpine
VOLUME ["/data", "/config"]
`)

	result, err := parser.Parse(ctx, content, "Dockerfile")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	volumes := filterDockerSymbolsByKind(result.Symbols, SymbolKindVolume)
	if len(volumes) != 2 {
		t.Fatalf("got %d volumes, want 2", len(volumes))
	}

	volNames := make(map[string]bool)
	for _, v := range volumes {
		volNames[v.Name] = true
	}

	if !volNames["/data"] {
		t.Error("missing volume /data")
	}
	if !volNames["/config"] {
		t.Error("missing volume /config")
	}
}

func TestDockerfileParser_Parse_EmptyContent(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	result, err := parser.Parse(ctx, []byte(""), "Dockerfile")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(result.Symbols) != 0 {
		t.Errorf("got %d symbols, want 0", len(result.Symbols))
	}
	if result.Language != "dockerfile" {
		t.Errorf("Language = %q, want %q", result.Language, "dockerfile")
	}
}

func TestDockerfileParser_Parse_CommentsOnly(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	content := []byte(`# This is a comment
# Another comment
`)

	result, err := parser.Parse(ctx, content, "Dockerfile")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(result.Symbols) != 0 {
		t.Errorf("got %d symbols, want 0", len(result.Symbols))
	}
}

func TestDockerfileParser_Parse_ContextCancellation(t *testing.T) {
	parser := NewDockerfileParser()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	content := []byte(`FROM alpine`)

	_, err := parser.Parse(ctx, content, "Dockerfile")
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Errorf("error should mention cancellation: %v", err)
	}
}

func TestDockerfileParser_Parse_FileTooLarge(t *testing.T) {
	parser := NewDockerfileParser(WithDockerfileMaxFileSize(100))
	ctx := context.Background()

	content := make([]byte, 200)
	for i := range content {
		content[i] = '#'
	}

	_, err := parser.Parse(ctx, content, "Dockerfile")
	if err != ErrFileTooLarge {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestDockerfileParser_Parse_InvalidUTF8(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	content := []byte{0xff, 0xfe, 0x00, 0x01}

	_, err := parser.Parse(ctx, content, "Dockerfile")
	if err != ErrInvalidContent {
		t.Errorf("expected ErrInvalidContent, got %v", err)
	}
}

func TestDockerfileParser_Parse_HashComputation(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	content := []byte(`FROM alpine`)

	result, err := parser.Parse(ctx, content, "Dockerfile")
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

func TestDockerfileParser_Parse_TimestampSet(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	before := time.Now().UnixMilli()
	result, err := parser.Parse(ctx, []byte(`FROM alpine`), "Dockerfile")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	after := time.Now().UnixMilli()

	if result.ParsedAtMilli < before || result.ParsedAtMilli > after {
		t.Errorf("ParsedAtMilli = %d, want between %d and %d", result.ParsedAtMilli, before, after)
	}
}

func TestDockerfileParser_Parse_LineNumbers(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	content := []byte(`# Comment
FROM alpine AS builder
ARG VERSION
`)

	result, err := parser.Parse(ctx, content, "Dockerfile")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	stages := filterDockerSymbolsByKind(result.Symbols, SymbolKindStage)
	if len(stages) != 1 {
		t.Fatalf("got %d stages, want 1", len(stages))
	}

	if stages[0].StartLine != 2 {
		t.Errorf("stage StartLine = %d, want 2", stages[0].StartLine)
	}

	args := filterDockerSymbolsByKind(result.Symbols, SymbolKindArg)
	if len(args) != 1 {
		t.Fatalf("got %d args, want 1", len(args))
	}

	if args[0].StartLine != 3 {
		t.Errorf("arg StartLine = %d, want 3", args[0].StartLine)
	}
}

func TestDockerfileParser_Parse_SymbolLanguage(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	content := []byte(`FROM alpine AS builder
ARG VERSION
`)

	result, err := parser.Parse(ctx, content, "Dockerfile")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	for _, sym := range result.Symbols {
		if sym.Language != "dockerfile" {
			t.Errorf("symbol %s Language = %q, want %q", sym.Name, sym.Language, "dockerfile")
		}
	}
}

func TestDockerfileParser_Parse_FilePath(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	content := []byte(`FROM alpine`)
	filePath := "docker/Dockerfile.prod"

	result, err := parser.Parse(ctx, content, filePath)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if result.FilePath != filePath {
		t.Errorf("FilePath = %q, want %q", result.FilePath, filePath)
	}
}

func TestDockerfileParser_ConcurrentParsing(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	contents := [][]byte{
		[]byte(`FROM alpine`),
		[]byte(`FROM golang:1.21`),
		[]byte(`FROM node:18`),
		[]byte(`FROM python:3.11`),
		[]byte(`FROM rust:1.70`),
	}

	results := make(chan error, len(contents))

	for i, content := range contents {
		go func(idx int, c []byte) {
			_, err := parser.Parse(ctx, c, "Dockerfile")
			results <- err
		}(i, content)
	}

	for range contents {
		if err := <-results; err != nil {
			t.Errorf("concurrent parse error: %v", err)
		}
	}
}

func TestDockerfileParser_Parse_SymbolExported(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	content := []byte(`FROM alpine AS builder
ARG VERSION
ENV APP_ENV=production
`)

	result, err := parser.Parse(ctx, content, "Dockerfile")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	for _, sym := range result.Symbols {
		if !sym.Exported {
			t.Errorf("symbol %q should be exported", sym.Name)
		}
	}
}

func TestDockerfileParser_DefaultOptions(t *testing.T) {
	opts := DefaultDockerfileParserOptions()

	if opts.MaxFileSize != 10*1024*1024 {
		t.Errorf("MaxFileSize = %d, want %d", opts.MaxFileSize, 10*1024*1024)
	}
}

func TestDockerfileParser_Parse_ComplexDockerfile(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	content := []byte(`# Build stage
FROM golang:1.21-alpine AS builder

ARG VERSION=1.0.0
ARG BUILD_DATE

ENV GO111MODULE=on
ENV CGO_ENABLED=0

WORKDIR /app
COPY . .
RUN go build -o main .

# Production stage
FROM alpine:3.18 AS production

LABEL maintainer="dev@example.com"
LABEL version="1.0"

ENV APP_ENV=production

EXPOSE 8080
EXPOSE 8443/tcp

VOLUME ["/data", "/config"]

COPY --from=builder /app/main /main
ENTRYPOINT ["/main"]
`)

	result, err := parser.Parse(ctx, content, "Dockerfile")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	stages := filterDockerSymbolsByKind(result.Symbols, SymbolKindStage)
	args := filterDockerSymbolsByKind(result.Symbols, SymbolKindArg)
	envs := filterDockerSymbolsByKind(result.Symbols, SymbolKindEnvVar)
	labels := filterDockerSymbolsByKind(result.Symbols, SymbolKindLabel)
	ports := filterDockerSymbolsByKind(result.Symbols, SymbolKindPort)
	volumes := filterDockerSymbolsByKind(result.Symbols, SymbolKindVolume)

	if len(stages) != 2 {
		t.Errorf("got %d stages, want 2", len(stages))
	}
	if len(args) != 2 {
		t.Errorf("got %d args, want 2", len(args))
	}
	if len(envs) != 3 {
		t.Errorf("got %d envs, want 3", len(envs))
	}
	if len(labels) != 2 {
		t.Errorf("got %d labels, want 2", len(labels))
	}
	if len(ports) != 2 {
		t.Errorf("got %d ports, want 2", len(ports))
	}
	if len(volumes) != 2 {
		t.Errorf("got %d volumes, want 2", len(volumes))
	}

	// Verify imports
	if len(result.Imports) != 2 {
		t.Errorf("got %d imports, want 2", len(result.Imports))
	}
}

func TestDockerfileParser_Parse_StageSignature(t *testing.T) {
	parser := NewDockerfileParser()
	ctx := context.Background()

	content := []byte(`FROM golang:1.21-alpine AS builder`)

	result, err := parser.Parse(ctx, content, "Dockerfile")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	stages := filterDockerSymbolsByKind(result.Symbols, SymbolKindStage)
	if len(stages) != 1 {
		t.Fatalf("got %d stages, want 1", len(stages))
	}

	want := "FROM golang:1.21-alpine AS builder"
	if stages[0].Signature != want {
		t.Errorf("Signature = %q, want %q", stages[0].Signature, want)
	}
}

// Helper function for filtering symbols
func filterDockerSymbolsByKind(symbols []*Symbol, kind SymbolKind) []*Symbol {
	var result []*Symbol
	for _, sym := range symbols {
		if sym.Kind == kind {
			result = append(result, sym)
		}
	}
	return result
}
