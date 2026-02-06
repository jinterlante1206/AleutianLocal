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
	"sync"
	"testing"
	"time"
)

func TestDefaultManagerConfig(t *testing.T) {
	config := DefaultManagerConfig()

	if config.IdleTimeout != 10*time.Minute {
		t.Errorf("IdleTimeout = %v, want 10m", config.IdleTimeout)
	}
	if config.StartupTimeout != 30*time.Second {
		t.Errorf("StartupTimeout = %v, want 30s", config.StartupTimeout)
	}
	if config.RequestTimeout != 10*time.Second {
		t.Errorf("RequestTimeout = %v, want 10s", config.RequestTimeout)
	}
}

func TestNewManager(t *testing.T) {
	config := DefaultManagerConfig()
	mgr := NewManager("/tmp/test", config)

	if mgr.RootPath() != "/tmp/test" {
		t.Errorf("RootPath() = %q, want %q", mgr.RootPath(), "/tmp/test")
	}
	if mgr.Config().IdleTimeout != config.IdleTimeout {
		t.Error("Config() mismatch")
	}
	if mgr.Configs() == nil {
		t.Error("Configs() should not be nil")
	}
}

func TestManager_GetOrSpawn_RequiresContext(t *testing.T) {
	mgr := NewManager("/tmp/test", DefaultManagerConfig())
	defer mgr.ShutdownAll(context.Background())

	_, err := mgr.GetOrSpawn(nil, "go") //nolint:staticcheck
	if err == nil {
		t.Error("expected error for nil context")
	}
}

func TestManager_GetOrSpawn_UnsupportedLanguage(t *testing.T) {
	mgr := NewManager("/tmp/test", DefaultManagerConfig())
	defer mgr.ShutdownAll(context.Background())

	ctx := context.Background()
	_, err := mgr.GetOrSpawn(ctx, "unsupported-language-xyz")

	if err == nil {
		t.Error("expected error for unsupported language")
	}
}

func TestManager_Get_NotRunning(t *testing.T) {
	mgr := NewManager("/tmp/test", DefaultManagerConfig())
	defer mgr.ShutdownAll(context.Background())

	server := mgr.Get("go")
	if server != nil {
		t.Error("expected nil for non-running server")
	}
}

func TestManager_RunningServers_Empty(t *testing.T) {
	mgr := NewManager("/tmp/test", DefaultManagerConfig())
	defer mgr.ShutdownAll(context.Background())

	servers := mgr.RunningServers()
	if len(servers) != 0 {
		t.Errorf("RunningServers() = %v, want empty", servers)
	}
}

func TestManager_IsAvailable(t *testing.T) {
	mgr := NewManager("/tmp/test", DefaultManagerConfig())
	defer mgr.ShutdownAll(context.Background())

	// Check for unsupported language
	if mgr.IsAvailable("nonexistent-language") {
		t.Error("should not be available for nonexistent language")
	}

	// Go might or might not be available depending on system
	// Just make sure the method doesn't panic
	_ = mgr.IsAvailable("go")
}

func TestManager_ShutdownAll_Idempotent(t *testing.T) {
	mgr := NewManager("/tmp/test", DefaultManagerConfig())

	ctx := context.Background()
	err1 := mgr.ShutdownAll(ctx)
	err2 := mgr.ShutdownAll(ctx)

	if err1 != nil || err2 != nil {
		t.Errorf("ShutdownAll should be idempotent, got err1=%v err2=%v", err1, err2)
	}
}

func TestManager_ShutdownAll_PreventsNewServers(t *testing.T) {
	mgr := NewManager("/tmp/test", DefaultManagerConfig())

	ctx := context.Background()
	_ = mgr.ShutdownAll(ctx)

	_, err := mgr.GetOrSpawn(ctx, "go")
	if err == nil {
		t.Error("expected error after ShutdownAll")
	}
}

func TestManager_Shutdown_NotRunning(t *testing.T) {
	mgr := NewManager("/tmp/test", DefaultManagerConfig())
	defer mgr.ShutdownAll(context.Background())

	ctx := context.Background()
	err := mgr.Shutdown(ctx, "go")

	if err != nil {
		t.Errorf("Shutdown for non-running server should succeed, got %v", err)
	}
}

// Integration tests
func TestManager_GetOrSpawn_Integration(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}

	// Create temporary Go project
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	mgr := NewManager(dir, DefaultManagerConfig())
	defer mgr.ShutdownAll(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	srv1, err := mgr.GetOrSpawn(ctx, "go")
	if err != nil {
		t.Fatalf("GetOrSpawn: %v", err)
	}

	if srv1 == nil {
		t.Fatal("expected non-nil server")
	}

	if srv1.State() != ServerStateReady {
		t.Errorf("State() = %v, want Ready", srv1.State())
	}

	// Second call should return same server
	srv2, err := mgr.GetOrSpawn(ctx, "go")
	if err != nil {
		t.Fatalf("GetOrSpawn 2: %v", err)
	}

	if srv1 != srv2 {
		t.Error("expected same server instance")
	}

	// Check Get also returns the same server
	srv3 := mgr.Get("go")
	if srv3 != srv1 {
		t.Error("Get should return same server")
	}

	// Check RunningServers
	running := mgr.RunningServers()
	if len(running) != 1 || running[0] != "go" {
		t.Errorf("RunningServers() = %v, want [go]", running)
	}
}

func TestManager_GetOrSpawn_Concurrent_Integration(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	mgr := NewManager(dir, DefaultManagerConfig())
	defer mgr.ShutdownAll(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Launch multiple goroutines trying to get the same server
	var wg sync.WaitGroup
	servers := make(chan *Server, 10)
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv, err := mgr.GetOrSpawn(ctx, "go")
			if err != nil {
				errors <- err
				return
			}
			servers <- srv
		}()
	}

	wg.Wait()
	close(servers)
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("GetOrSpawn error: %v", err)
	}

	// All should be same server
	var first *Server
	for srv := range servers {
		if first == nil {
			first = srv
		} else if srv != first {
			t.Error("got different servers from concurrent calls")
		}
	}
}

func TestManager_Shutdown_Integration(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	mgr := NewManager(dir, DefaultManagerConfig())
	defer mgr.ShutdownAll(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Start server
	srv, err := mgr.GetOrSpawn(ctx, "go")
	if err != nil {
		t.Fatalf("GetOrSpawn: %v", err)
	}

	// Shut it down
	if err := mgr.Shutdown(ctx, "go"); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// Server should be stopped
	if srv.State() != ServerStateStopped {
		t.Errorf("State() = %v, want Stopped", srv.State())
	}

	// Get should return nil
	if mgr.Get("go") != nil {
		t.Error("Get should return nil after shutdown")
	}

	// GetOrSpawn should start a new server
	srv2, err := mgr.GetOrSpawn(ctx, "go")
	if err != nil {
		t.Fatalf("GetOrSpawn 2: %v", err)
	}
	if srv2 == srv {
		t.Error("should get new server after shutdown")
	}
}

func TestManager_IdleMonitor(t *testing.T) {
	// Use a very short idle timeout for testing
	config := ManagerConfig{
		IdleTimeout:    100 * time.Millisecond,
		StartupTimeout: 30 * time.Second,
		RequestTimeout: 10 * time.Second,
	}

	mgr := NewManager("/tmp/test", config)
	defer mgr.ShutdownAll(context.Background())

	// Start idle monitor
	mgr.StartIdleMonitor()

	// Without any servers running, this should just not crash
	time.Sleep(300 * time.Millisecond)
}

func TestManager_Configs_Registration(t *testing.T) {
	mgr := NewManager("/tmp/test", DefaultManagerConfig())
	defer mgr.ShutdownAll(context.Background())

	// Register a custom language
	mgr.Configs().Register(LanguageConfig{
		Language:   "custom",
		Command:    "custom-lsp",
		Args:       []string{"--stdio"},
		Extensions: []string{".custom"},
	})

	// Check it was registered
	config, ok := mgr.Configs().Get("custom")
	if !ok {
		t.Error("custom language should be registered")
	}
	if config.Command != "custom-lsp" {
		t.Errorf("Command = %q, want custom-lsp", config.Command)
	}
}

// =============================================================================
// ReleaseFile/ReopenFile Tests (Windows atomic write compatibility)
// =============================================================================

func TestManager_ReleaseFile_RequiresContext(t *testing.T) {
	mgr := NewManager("/tmp/test", DefaultManagerConfig())
	defer mgr.ShutdownAll(context.Background())

	err := mgr.ReleaseFile(nil, "/tmp/test.go") //nolint:staticcheck
	if err == nil {
		t.Error("expected error for nil context")
	}
}

func TestManager_ReopenFile_RequiresContext(t *testing.T) {
	mgr := NewManager("/tmp/test", DefaultManagerConfig())
	defer mgr.ShutdownAll(context.Background())

	err := mgr.ReopenFile(nil, "/tmp/test.go", "package main", "go") //nolint:staticcheck
	if err == nil {
		t.Error("expected error for nil context")
	}
}

func TestManager_ReleaseFile_NoServers(t *testing.T) {
	mgr := NewManager("/tmp/test", DefaultManagerConfig())
	defer mgr.ShutdownAll(context.Background())

	ctx := context.Background()
	err := mgr.ReleaseFile(ctx, "/tmp/test.go")

	// Should succeed even with no servers running
	if err != nil {
		t.Errorf("ReleaseFile should succeed with no servers, got %v", err)
	}
}

func TestManager_ReopenFile_NoServers(t *testing.T) {
	mgr := NewManager("/tmp/test", DefaultManagerConfig())
	defer mgr.ShutdownAll(context.Background())

	ctx := context.Background()
	err := mgr.ReopenFile(ctx, "/tmp/test.go", "package main", "go")

	// Should succeed even with no servers running
	if err != nil {
		t.Errorf("ReopenFile should succeed with no servers, got %v", err)
	}
}

func TestManager_ReleaseFile_AfterShutdown(t *testing.T) {
	mgr := NewManager("/tmp/test", DefaultManagerConfig())

	// Shutdown first
	ctx := context.Background()
	_ = mgr.ShutdownAll(ctx)

	// Should still succeed (no-op after shutdown)
	err := mgr.ReleaseFile(ctx, "/tmp/test.go")
	if err != nil {
		t.Errorf("ReleaseFile should succeed after shutdown, got %v", err)
	}
}

func TestManager_ReopenFile_AfterShutdown(t *testing.T) {
	mgr := NewManager("/tmp/test", DefaultManagerConfig())

	// Shutdown first
	ctx := context.Background()
	_ = mgr.ShutdownAll(ctx)

	// Should still succeed (no-op after shutdown)
	err := mgr.ReopenFile(ctx, "/tmp/test.go", "package main", "go")
	if err != nil {
		t.Errorf("ReopenFile should succeed after shutdown, got %v", err)
	}
}

func TestManager_ReleaseFile_Integration(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}

	// Create temporary Go project
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(mainFile, []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	mgr := NewManager(dir, DefaultManagerConfig())
	defer mgr.ShutdownAll(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Start the server
	_, err := mgr.GetOrSpawn(ctx, "go")
	if err != nil {
		t.Fatalf("GetOrSpawn: %v", err)
	}

	// Release the file (should not error)
	err = mgr.ReleaseFile(ctx, mainFile)
	if err != nil {
		t.Errorf("ReleaseFile: %v", err)
	}

	// Reopen the file with new content
	newContent := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"
	err = mgr.ReopenFile(ctx, mainFile, newContent, "go")
	if err != nil {
		t.Errorf("ReopenFile: %v", err)
	}
}
