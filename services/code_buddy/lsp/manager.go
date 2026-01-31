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
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"time"
)

// =============================================================================
// MANAGER CONFIG
// =============================================================================

// ManagerConfig configures the LSP manager.
type ManagerConfig struct {
	// IdleTimeout is how long a server can be idle before being shut down.
	// Set to 0 to disable idle shutdown.
	IdleTimeout time.Duration

	// StartupTimeout is the maximum time to wait for a server to start.
	StartupTimeout time.Duration

	// RequestTimeout is the default timeout for LSP requests.
	RequestTimeout time.Duration
}

// DefaultManagerConfig returns sensible defaults for the manager.
//
// Description:
//
//	Returns a configuration with:
//	  - IdleTimeout: 10 minutes
//	  - StartupTimeout: 30 seconds
//	  - RequestTimeout: 10 seconds
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		IdleTimeout:    10 * time.Minute,
		StartupTimeout: 30 * time.Second,
		RequestTimeout: 10 * time.Second,
	}
}

// =============================================================================
// MANAGER
// =============================================================================

// Manager manages LSP server instances for multiple languages.
//
// Description:
//
//	Provides lazy startup of language servers as needed, with idle
//	timeout and graceful shutdown. Each language has at most one
//	server instance per workspace.
//
// Thread Safety:
//
//	Safe for concurrent use.
type Manager struct {
	config   ManagerConfig
	rootPath string
	configs  *ConfigRegistry

	servers   map[string]*Server
	serversMu sync.RWMutex
	startMu   sync.Map // language → *sync.Mutex for startup serialization

	stopped  chan struct{}
	stopOnce sync.Once
}

// NewManager creates a new LSP manager.
//
// Description:
//
//	Creates a manager for the given workspace root. The manager will
//	lazily start language servers as needed and shut them down after
//	the idle timeout.
//
// Inputs:
//
//	rootPath - Absolute path to the workspace root
//	config - Manager configuration
//
// Outputs:
//
//	*Manager - The configured manager
func NewManager(rootPath string, config ManagerConfig) *Manager {
	return &Manager{
		config:   config,
		rootPath: rootPath,
		configs:  NewConfigRegistry(),
		servers:  make(map[string]*Server),
		stopped:  make(chan struct{}),
	}
}

// GetOrSpawn returns a server for the language, starting it if needed.
//
// Description:
//
//	Returns an existing server if one is running and ready, otherwise
//	starts a new server. Uses double-check locking to ensure only one
//	server is started per language even under concurrent requests.
//
// Inputs:
//
//	ctx - Context for cancellation and startup timeout
//	language - The language identifier (e.g., "go", "python")
//
// Outputs:
//
//	*Server - The ready server
//	error - Non-nil if the language is unsupported or server failed to start
//
// Errors:
//
//	ErrUnsupportedLanguage - No configuration for the language
//	ErrServerNotInstalled - Server binary not found
//	ErrInitializeFailed - Server initialization failed
//
// Thread Safety:
//
//	Safe for concurrent use.
func (m *Manager) GetOrSpawn(ctx context.Context, language string) (*Server, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}

	// Check if manager is stopped
	select {
	case <-m.stopped:
		return nil, fmt.Errorf("manager is stopped")
	default:
	}

	// Fast path: check if already running
	m.serversMu.RLock()
	server, ok := m.servers[language]
	m.serversMu.RUnlock()

	if ok && server.State() == ServerStateReady {
		return server, nil
	}

	// Get per-language startup lock
	lockI, _ := m.startMu.LoadOrStore(language, &sync.Mutex{})
	lock := lockI.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	// Double-check after acquiring lock
	m.serversMu.RLock()
	server, ok = m.servers[language]
	m.serversMu.RUnlock()

	if ok && server.State() == ServerStateReady {
		return server, nil
	}

	// Clean up dead server if exists
	if ok && server.State() == ServerStateStopped {
		m.serversMu.Lock()
		delete(m.servers, language)
		m.serversMu.Unlock()
	}

	// Get config for language
	config, ok := m.configs.Get(language)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedLanguage, language)
	}

	// Create and start new server
	server = NewServer(config, m.rootPath)

	// Apply startup timeout
	startCtx := ctx
	if m.config.StartupTimeout > 0 {
		var cancel context.CancelFunc
		startCtx, cancel = context.WithTimeout(ctx, m.config.StartupTimeout)
		defer cancel()
	}

	if err := server.Start(startCtx); err != nil {
		return nil, err
	}

	// Store the server
	m.serversMu.Lock()
	m.servers[language] = server
	m.serversMu.Unlock()

	return server, nil
}

// Get returns a server for the language if one is running.
//
// Description:
//
//	Returns the server if it exists and is ready, otherwise returns nil.
//	Does not start a new server.
//
// Inputs:
//
//	language - The language identifier
//
// Outputs:
//
//	*Server - The server if running and ready, nil otherwise
//
// Thread Safety:
//
//	Safe for concurrent use.
func (m *Manager) Get(language string) *Server {
	m.serversMu.RLock()
	defer m.serversMu.RUnlock()

	server, ok := m.servers[language]
	if ok && server.State() == ServerStateReady {
		return server
	}
	return nil
}

// Shutdown shuts down a specific language server.
//
// Description:
//
//	Gracefully shuts down the server for the given language.
//	No-op if no server is running for the language.
//
// Inputs:
//
//	ctx - Context for shutdown timeout
//	language - The language identifier
//
// Outputs:
//
//	error - Non-nil if shutdown encountered errors
//
// Thread Safety:
//
//	Safe for concurrent use.
func (m *Manager) Shutdown(ctx context.Context, language string) error {
	m.serversMu.Lock()
	server, ok := m.servers[language]
	if ok {
		delete(m.servers, language)
	}
	m.serversMu.Unlock()

	if !ok {
		return nil
	}

	return server.Shutdown(ctx)
}

// ShutdownAll shuts down all servers and stops the manager.
//
// Description:
//
//	Gracefully shuts down all running servers. After this call,
//	GetOrSpawn will return an error.
//
// Inputs:
//
//	ctx - Context for shutdown timeout
//
// Outputs:
//
//	error - Non-nil if any shutdown encountered errors (last error returned)
//
// Thread Safety:
//
//	Safe for concurrent use. Multiple calls are idempotent.
func (m *Manager) ShutdownAll(ctx context.Context) error {
	// Mark manager as stopped
	m.stopOnce.Do(func() {
		close(m.stopped)
	})

	// Get all servers
	m.serversMu.Lock()
	servers := make(map[string]*Server)
	for lang, srv := range m.servers {
		servers[lang] = srv
	}
	m.servers = make(map[string]*Server)
	m.serversMu.Unlock()

	// Shut down all servers
	var lastErr error
	for _, server := range servers {
		if err := server.Shutdown(ctx); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// IsAvailable checks if an LSP server is available for a language.
//
// Description:
//
//	Checks if the language is supported and the server binary is installed.
//	Does not start the server.
//
// Inputs:
//
//	language - The language identifier
//
// Outputs:
//
//	bool - True if the language is supported and server is installed
//
// Thread Safety:
//
//	Safe for concurrent use.
func (m *Manager) IsAvailable(language string) bool {
	config, ok := m.configs.Get(language)
	if !ok {
		return false
	}
	_, err := exec.LookPath(config.Command)
	return err == nil
}

// RunningServers returns languages with running servers.
//
// Description:
//
//	Returns a list of language identifiers for servers that are
//	currently in the ready state.
//
// Outputs:
//
//	[]string - Language identifiers
//
// Thread Safety:
//
//	Safe for concurrent use.
func (m *Manager) RunningServers() []string {
	m.serversMu.RLock()
	defer m.serversMu.RUnlock()

	langs := make([]string, 0, len(m.servers))
	for lang, srv := range m.servers {
		if srv.State() == ServerStateReady {
			langs = append(langs, lang)
		}
	}
	return langs
}

// Config returns the manager configuration.
func (m *Manager) Config() ManagerConfig {
	return m.config
}

// RootPath returns the workspace root path.
func (m *Manager) RootPath() string {
	return m.rootPath
}

// Configs returns the language configuration registry.
//
// Description:
//
//	Returns the registry so callers can register custom language
//	configurations.
//
// Thread Safety:
//
//	The returned registry is safe for concurrent use.
func (m *Manager) Configs() *ConfigRegistry {
	return m.configs
}

// =============================================================================
// IDLE MONITOR
// =============================================================================

// StartIdleMonitor starts the idle server cleanup goroutine.
//
// Description:
//
//	Starts a background goroutine that periodically checks for idle
//	servers and shuts them down. The check interval is half the idle
//	timeout. Does nothing if IdleTimeout is 0.
//
// Thread Safety:
//
//	Safe for concurrent use. Multiple calls start multiple monitors
//	(not recommended).
func (m *Manager) StartIdleMonitor() {
	if m.config.IdleTimeout <= 0 {
		return
	}

	go func() {
		// Check at half the idle timeout interval
		interval := m.config.IdleTimeout / 2
		if interval < time.Second {
			interval = time.Second
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-m.stopped:
				return
			case <-ticker.C:
				m.shutdownIdle()
			}
		}
	}()
}

// shutdownIdle shuts down servers that have been idle too long.
func (m *Manager) shutdownIdle() {
	m.serversMu.RLock()
	var toShutdown []string
	for lang, srv := range m.servers {
		if srv.State() == ServerStateReady && time.Since(srv.LastUsed()) > m.config.IdleTimeout {
			toShutdown = append(toShutdown, lang)
		}
	}
	m.serversMu.RUnlock()

	ctx := context.Background()
	for _, lang := range toShutdown {
		slog.Info("Shutting down idle LSP server",
			slog.String("language", lang),
			slog.Duration("idle_timeout", m.config.IdleTimeout),
		)
		_ = m.Shutdown(ctx, lang)
	}
}
