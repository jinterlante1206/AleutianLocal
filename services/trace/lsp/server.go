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
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"
)

// =============================================================================
// SERVER STATE
// =============================================================================

// ServerState represents the lifecycle state of an LSP server.
type ServerState int

const (
	// ServerStateUninitialized is the initial state before Start is called.
	ServerStateUninitialized ServerState = iota

	// ServerStateStarting means the server process is starting.
	ServerStateStarting

	// ServerStateReady means the server is initialized and ready for requests.
	ServerStateReady

	// ServerStateStopping means the server is shutting down.
	ServerStateStopping

	// ServerStateStopped means the server has terminated.
	ServerStateStopped
)

// String returns a human-readable state name.
func (s ServerState) String() string {
	names := []string{"uninitialized", "starting", "ready", "stopping", "stopped"}
	if int(s) < len(names) {
		return names[s]
	}
	return "unknown"
}

// =============================================================================
// SERVER
// =============================================================================

// Server represents a running LSP server process.
//
// Description:
//
//	Manages the lifecycle of an LSP server process, including starting,
//	initializing, and shutting down. Provides methods for sending requests
//	and notifications to the server.
//
// Thread Safety:
//
//	Safe for concurrent use after Start() returns successfully.
type Server struct {
	config   LanguageConfig
	rootPath string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	protocol     *Protocol
	capabilities ServerCapabilities

	state   ServerState
	stateMu sync.RWMutex

	ctx      context.Context
	cancel   context.CancelFunc
	readDone chan struct{}

	lastUsed   time.Time
	lastUsedMu sync.Mutex
}

// NewServer creates a new server instance (not started).
//
// Description:
//
//	Creates a server instance configured for the given language.
//	The server is not started; call Start to begin the process.
//
// Inputs:
//
//	config - Language configuration for the server
//	rootPath - Absolute path to the workspace root
//
// Outputs:
//
//	*Server - The configured (but not started) server
func NewServer(config LanguageConfig, rootPath string) *Server {
	return &Server{
		config:   config,
		rootPath: rootPath,
		state:    ServerStateUninitialized,
		readDone: make(chan struct{}),
		lastUsed: time.Now(),
	}
}

// Start starts the LSP server process and initializes it.
//
// Description:
//
//	Starts the server process, establishes communication, and performs
//	the LSP initialize handshake. On success, the server is ready to
//	receive requests.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout
//
// Outputs:
//
//	error - Non-nil if the server failed to start or initialize
//
// Errors:
//
//	ErrServerNotInstalled - Server binary not found
//	ErrServerAlreadyStarted - Start called on a non-uninitialized server
//	ErrInitializeFailed - LSP initialize handshake failed
//
// Thread Safety:
//
//	Safe for concurrent use, but only the first caller will start the server.
func (s *Server) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("ctx must not be nil")
	}

	s.stateMu.Lock()
	if s.state != ServerStateUninitialized {
		s.stateMu.Unlock()
		return ErrServerAlreadyStarted
	}
	s.state = ServerStateStarting
	s.stateMu.Unlock()

	// Check binary exists
	path, err := exec.LookPath(s.config.Command)
	if err != nil {
		s.setState(ServerStateStopped)
		slog.Warn("LSP server not installed",
			slog.String("language", s.config.Language),
			slog.String("command", s.config.Command),
		)
		return fmt.Errorf("%w: %s", ErrServerNotInstalled, s.config.Command)
	}

	slog.Info("Starting LSP server",
		slog.String("language", s.config.Language),
		slog.String("command", path),
		slog.String("root_path", s.rootPath),
	)

	// Create server context (independent of caller's context)
	s.ctx, s.cancel = context.WithCancel(context.Background())

	// Create command
	s.cmd = exec.CommandContext(s.ctx, path, s.config.Args...)
	s.cmd.Dir = s.rootPath

	// Setup pipes
	s.stdin, err = s.cmd.StdinPipe()
	if err != nil {
		s.cleanup()
		return fmt.Errorf("stdin pipe: %w", err)
	}

	s.stdout, err = s.cmd.StdoutPipe()
	if err != nil {
		s.cleanup()
		return fmt.Errorf("stdout pipe: %w", err)
	}

	// Start process
	if err := s.cmd.Start(); err != nil {
		s.cleanup()
		return fmt.Errorf("start process: %w", err)
	}

	// Setup protocol
	s.protocol = NewProtocol(s.stdout, s.stdin)

	// Start read loop in background
	go func() {
		defer close(s.readDone)
		_ = s.protocol.ReadLoop(s.ctx)
	}()

	// Perform initialize handshake
	if err := s.initialize(ctx); err != nil {
		s.Shutdown(ctx)
		return fmt.Errorf("%w: %v", ErrInitializeFailed, err)
	}

	s.setState(ServerStateReady)
	s.touchLastUsed()

	slog.Info("LSP server ready",
		slog.String("language", s.config.Language),
		slog.Bool("definition", s.capabilities.HasDefinitionProvider()),
		slog.Bool("references", s.capabilities.HasReferencesProvider()),
		slog.Bool("hover", s.capabilities.HasHoverProvider()),
		slog.Bool("rename", s.capabilities.HasRenameProvider()),
	)

	return nil
}

// initialize performs the LSP initialize handshake.
func (s *Server) initialize(ctx context.Context) error {
	params := InitializeParams{
		ProcessID: os.Getpid(),
		RootURI:   "file://" + s.rootPath,
		RootPath:  s.rootPath,
		Capabilities: ClientCapabilities{
			TextDocument: TextDocumentClientCapabilities{
				Synchronization: &TextDocumentSyncClientCapabilities{
					DidSave: true,
				},
				Definition: &DefinitionCapabilities{},
				References: &ReferencesCapabilities{},
				Hover: &HoverCapabilities{
					ContentFormat: []string{"markdown", "plaintext"},
				},
				Rename: &RenameCapabilities{
					PrepareSupport: true,
				},
			},
			Workspace: WorkspaceClientCapabilities{
				ApplyEdit: true,
				WorkspaceEdit: &WorkspaceEditClientCapabilities{
					DocumentChanges: true,
				},
				Symbol: &WorkspaceSymbolClientCapabilities{},
			},
		},
		WorkspaceFolders: []WorkspaceFolder{
			{
				URI:  "file://" + s.rootPath,
				Name: "workspace",
			},
		},
	}

	// Add initialization options if configured
	if s.config.InitializationOptions != nil {
		params.InitializationOptions = s.config.InitializationOptions
	}

	resp, err := s.protocol.SendRequest(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("initialize request: %w", err)
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("parse initialize result: %w", err)
	}

	s.capabilities = result.Capabilities

	// Send initialized notification
	if err := s.protocol.SendNotification("initialized", struct{}{}); err != nil {
		return fmt.Errorf("initialized notification: %w", err)
	}

	return nil
}

// Shutdown gracefully shuts down the server.
//
// Description:
//
//	Sends shutdown and exit messages to the server, then waits for the
//	process to terminate. If the server doesn't respond, it is killed.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout
//
// Outputs:
//
//	error - Non-nil if shutdown encountered errors (server is still stopped)
//
// Thread Safety:
//
//	Safe for concurrent use. Multiple calls are idempotent.
func (s *Server) Shutdown(ctx context.Context) error {
	s.stateMu.Lock()
	if s.state == ServerStateStopped || s.state == ServerStateStopping {
		s.stateMu.Unlock()
		return nil
	}
	s.state = ServerStateStopping
	s.stateMu.Unlock()

	slog.Info("Shutting down LSP server",
		slog.String("language", s.config.Language),
	)

	defer s.cleanup()

	// Try graceful shutdown
	if s.protocol != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		// Send shutdown request (ignoring errors)
		_, _ = s.protocol.SendRequest(shutdownCtx, "shutdown", nil)

		// Send exit notification
		_ = s.protocol.SendNotification("exit", nil)

		// Mark protocol as closed
		s.protocol.Close()
	}

	// Close stdin to signal EOF to server
	if s.stdin != nil {
		_ = s.stdin.Close()
	}

	// Wait for process with timeout
	if s.cmd != nil && s.cmd.Process != nil {
		done := make(chan error, 1)
		go func() { done <- s.cmd.Wait() }()

		select {
		case <-time.After(5 * time.Second):
			// Force kill
			_ = s.cmd.Process.Kill()
			<-done
		case <-done:
		}
	}

	// Wait for read loop to finish
	if s.cancel != nil {
		s.cancel()
	}

	select {
	case <-s.readDone:
	case <-time.After(time.Second):
	}

	return nil
}

// cleanup releases resources and sets state to stopped.
func (s *Server) cleanup() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.stdout != nil {
		_ = s.stdout.Close()
	}
	s.setState(ServerStateStopped)
}

// =============================================================================
// ACCESSORS
// =============================================================================

// State returns the current server state.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (s *Server) State() ServerState {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.state
}

// Language returns the language this server handles.
func (s *Server) Language() string {
	return s.config.Language
}

// RootPath returns the workspace root path.
func (s *Server) RootPath() string {
	return s.rootPath
}

// Capabilities returns the server's capabilities.
//
// Description:
//
//	Returns the capabilities reported by the server during initialization.
//	Returns zero value if the server hasn't been initialized.
func (s *Server) Capabilities() ServerCapabilities {
	return s.capabilities
}

// LastUsed returns when the server was last used.
//
// Thread Safety:
//
//	Safe for concurrent use.
func (s *Server) LastUsed() time.Time {
	s.lastUsedMu.Lock()
	defer s.lastUsedMu.Unlock()
	return s.lastUsed
}

// =============================================================================
// REQUEST METHODS
// =============================================================================

// Request sends an LSP request and waits for the response.
//
// Description:
//
//	Sends a request to the server and blocks until a response is received
//	or the context is cancelled. Updates the last-used timestamp.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout
//	method - The LSP method to invoke
//	params - Method parameters
//
// Outputs:
//
//	*Response - The server's response
//	error - Non-nil if server not ready, send failed, or timeout
//
// Thread Safety:
//
//	Safe for concurrent use.
func (s *Server) Request(ctx context.Context, method string, params interface{}) (*Response, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}
	if s.State() != ServerStateReady {
		return nil, ErrServerNotRunning
	}
	s.touchLastUsed()
	return s.protocol.SendRequest(ctx, method, params)
}

// Notify sends an LSP notification.
//
// Description:
//
//	Sends a notification to the server. Notifications do not expect a
//	response. Updates the last-used timestamp.
//
// Inputs:
//
//	method - The LSP method to invoke
//	params - Method parameters
//
// Outputs:
//
//	error - Non-nil if server not ready or send failed
//
// Thread Safety:
//
//	Safe for concurrent use.
func (s *Server) Notify(method string, params interface{}) error {
	if s.State() != ServerStateReady {
		return ErrServerNotRunning
	}
	s.touchLastUsed()
	return s.protocol.SendNotification(method, params)
}

// =============================================================================
// INTERNAL HELPERS
// =============================================================================

func (s *Server) setState(state ServerState) {
	s.stateMu.Lock()
	s.state = state
	s.stateMu.Unlock()
}

func (s *Server) touchLastUsed() {
	s.lastUsedMu.Lock()
	s.lastUsed = time.Now()
	s.lastUsedMu.Unlock()
}
