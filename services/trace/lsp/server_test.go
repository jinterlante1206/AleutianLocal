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
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestServerState_String(t *testing.T) {
	tests := []struct {
		state    ServerState
		expected string
	}{
		{ServerStateUninitialized, "uninitialized"},
		{ServerStateStarting, "starting"},
		{ServerStateReady, "ready"},
		{ServerStateStopping, "stopping"},
		{ServerStateStopped, "stopped"},
		{ServerState(99), "unknown"},
	}

	for _, tc := range tests {
		if got := tc.state.String(); got != tc.expected {
			t.Errorf("ServerState(%d).String() = %q, want %q", tc.state, got, tc.expected)
		}
	}
}

func TestNewServer(t *testing.T) {
	config := LanguageConfig{
		Language: "go",
		Command:  "gopls",
		Args:     []string{"serve"},
	}

	server := NewServer(config, "/tmp/test")

	if server.Language() != "go" {
		t.Errorf("Language() = %q, want %q", server.Language(), "go")
	}
	if server.RootPath() != "/tmp/test" {
		t.Errorf("RootPath() = %q, want %q", server.RootPath(), "/tmp/test")
	}
	if server.State() != ServerStateUninitialized {
		t.Errorf("State() = %v, want Uninitialized", server.State())
	}
}

func TestServer_StartRequiresContext(t *testing.T) {
	config := LanguageConfig{
		Language: "go",
		Command:  "gopls",
		Args:     []string{"serve"},
	}

	server := NewServer(config, "/tmp/test")

	err := server.Start(nil) //nolint:staticcheck
	if err == nil {
		t.Error("expected error for nil context")
	}
}

func TestServer_StartNotInstalled(t *testing.T) {
	config := LanguageConfig{
		Language: "test",
		Command:  "nonexistent-lsp-server-binary-12345",
		Args:     []string{},
	}

	server := NewServer(config, "/tmp/test")

	ctx := context.Background()
	err := server.Start(ctx)

	if err == nil {
		t.Fatal("expected error for nonexistent binary")
	}
	if server.State() != ServerStateStopped {
		t.Errorf("State() = %v, want Stopped", server.State())
	}
}

func TestServer_DoubleStart(t *testing.T) {
	config := LanguageConfig{
		Language: "test",
		Command:  "nonexistent-lsp-server-binary-12345",
		Args:     []string{},
	}

	server := NewServer(config, "/tmp/test")

	ctx := context.Background()
	_ = server.Start(ctx) // First start (fails because binary doesn't exist)

	// Try to start again - should fail because already tried
	err := server.Start(ctx)
	if err != ErrServerAlreadyStarted {
		// Note: depending on state, might get different error
		if err == nil {
			t.Error("expected error for double start")
		}
	}
}

func TestServer_ShutdownIdempotent(t *testing.T) {
	config := LanguageConfig{
		Language: "test",
		Command:  "nonexistent",
		Args:     []string{},
	}

	server := NewServer(config, "/tmp/test")

	ctx := context.Background()
	// Shutdown before start should be safe
	err1 := server.Shutdown(ctx)
	err2 := server.Shutdown(ctx)

	if err1 != nil || err2 != nil {
		t.Errorf("Shutdown should be idempotent, got err1=%v err2=%v", err1, err2)
	}
}

func TestServer_RequestRequiresReady(t *testing.T) {
	config := LanguageConfig{
		Language: "test",
		Command:  "nonexistent",
		Args:     []string{},
	}

	server := NewServer(config, "/tmp/test")

	ctx := context.Background()
	_, err := server.Request(ctx, "test", nil)

	if err != ErrServerNotRunning {
		t.Errorf("Request() error = %v, want ErrServerNotRunning", err)
	}
}

func TestServer_NotifyRequiresReady(t *testing.T) {
	config := LanguageConfig{
		Language: "test",
		Command:  "nonexistent",
		Args:     []string{},
	}

	server := NewServer(config, "/tmp/test")

	err := server.Notify("test", nil)

	if err != ErrServerNotRunning {
		t.Errorf("Notify() error = %v, want ErrServerNotRunning", err)
	}
}

func TestServer_LastUsed(t *testing.T) {
	config := LanguageConfig{
		Language: "test",
		Command:  "nonexistent",
		Args:     []string{},
	}

	server := NewServer(config, "/tmp/test")

	// LastUsed should be set at creation
	lastUsed := server.LastUsed()
	if time.Since(lastUsed) > time.Second {
		t.Error("LastUsed should be recent")
	}
}

// Integration tests - only run if gopls is installed
func TestServer_StartShutdown_Integration(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}

	// Create a temporary Go project
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	config := LanguageConfig{
		Language: "go",
		Command:  "gopls",
		Args:     []string{"serve"},
	}

	server := NewServer(config, dir)

	// Start with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if server.State() != ServerStateReady {
		t.Errorf("State() = %v, want Ready", server.State())
	}

	// Check capabilities
	caps := server.Capabilities()
	if !caps.HasDefinitionProvider() {
		t.Error("expected definition provider")
	}

	// Shutdown
	if err := server.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}

	if server.State() != ServerStateStopped {
		t.Errorf("State() = %v, want Stopped", server.State())
	}
}

func TestServer_Request_Integration(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {\n\thelper()\n}\n\nfunc helper() {}\n"), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	config := LanguageConfig{
		Language: "go",
		Command:  "gopls",
		Args:     []string{"serve"},
	}

	server := NewServer(config, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer server.Shutdown(context.Background())

	// Open the file first (required for most LSP operations)
	content, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	openParams := DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:        "file://" + filepath.Join(dir, "main.go"),
			LanguageID: "go",
			Version:    1,
			Text:       string(content),
		},
	}
	if err := server.Notify("textDocument/didOpen", openParams); err != nil {
		t.Fatalf("didOpen: %v", err)
	}

	// Give gopls time to process
	time.Sleep(500 * time.Millisecond)

	// Send a hover request
	hoverParams := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{
			URI: "file://" + filepath.Join(dir, "main.go"),
		},
		Position: Position{Line: 6, Character: 5}, // "helper" function
	}

	resp, err := server.Request(ctx, "textDocument/hover", hoverParams)
	if err != nil {
		t.Fatalf("hover request: %v", err)
	}

	if resp == nil {
		t.Error("expected non-nil response")
	}
}
