// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package orchestrator provides the core orchestrator service for AleutianLocal.
//
// This package contains the main Orchestrator type that coordinates all
// components of the service: HTTP routing, LLM clients, policy engine,
// vector database, and observability infrastructure.
//
// # Enterprise Integration
//
// The orchestrator supports dependency injection via extensions.ServiceOptions,
// enabling AleutianEnterprise to provide custom implementations of:
//   - AuthProvider: Custom authentication (JWT, API keys)
//   - AuthzProvider: Role-based access control
//   - AuditLogger: Compliance audit logging
//   - MessageFilter: PII detection and redaction
//
// # Usage
//
// Open source (uses no-op defaults):
//
//	cfg := orchestrator.Config{Port: 12210}
//	svc, err := orchestrator.New(cfg, nil)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	svc.Run()
//
// Enterprise (with custom implementations):
//
//	opts := &extensions.ServiceOptions{
//	    AuthProvider:  enterpriseAuth,
//	    AuditLogger:   enterpriseAudit,
//	}
//	svc, err := orchestrator.New(cfg, opts)
//
// # Import Path
//
// Enterprise imports this package as:
//
//	import "github.com/AleutianAI/AleutianFOSS/services/orchestrator"
package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/AleutianAI/AleutianFOSS/pkg/extensions"
	"github.com/AleutianAI/AleutianFOSS/services/llm"
	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/datatypes"
	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/observability"
	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/routes"
	"github.com/AleutianAI/AleutianFOSS/services/orchestrator/ttl"
	"github.com/AleutianAI/AleutianFOSS/services/policy_engine"
	"github.com/gin-gonic/gin"
	"github.com/weaviate/weaviate-go-client/v5/weaviate"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// =============================================================================
// Interface Definition
// =============================================================================

// Service defines the contract for the orchestrator service.
//
// # Description
//
// Service abstracts the orchestrator lifecycle, enabling testing and
// alternative implementations. The interface follows the minimal surface
// area principle - only essential lifecycle methods are exposed.
//
// # Thread Safety
//
// Implementations must be safe for concurrent use. Run() blocks and should
// only be called once per instance.
//
// # Limitations
//
//   - No graceful shutdown method yet (planned for future)
//   - Run() blocks until server error
//
// # Assumptions
//
//   - Service is fully initialized before Run() is called
//   - Run() is called at most once per Service instance
type Service interface {
	// Run starts the HTTP server and blocks until shutdown or error.
	//
	// # Description
	//
	// Starts the Gin HTTP server on the configured port. This method
	// blocks until the server stops (due to error or shutdown signal).
	//
	// # Inputs
	//
	// None (configuration provided at construction time).
	//
	// # Outputs
	//
	//   - error: Non-nil if server fails to start or encounters fatal error
	//
	// # Examples
	//
	//   if err := svc.Run(); err != nil {
	//       log.Fatalf("server error: %v", err)
	//   }
	//
	// # Limitations
	//
	//   - Blocks until server stops
	//   - No graceful shutdown support yet
	//
	// # Assumptions
	//
	//   - Service was successfully created via New()
	//   - Port is available and not in use
	Run() error

	// Router returns the underlying Gin engine for testing.
	//
	// # Description
	//
	// Provides access to the configured Gin router, primarily for
	// integration testing where direct HTTP calls are needed.
	//
	// # Outputs
	//
	//   - *gin.Engine: The configured router with all routes registered
	//
	// # Limitations
	//
	//   - Should not be used to modify routes after construction
	//
	// # Assumptions
	//
	//   - Caller will not modify the router
	Router() *gin.Engine
}

// =============================================================================
// Configuration
// =============================================================================

// Config holds orchestrator configuration options.
//
// # Description
//
// Config centralizes all configuration for the orchestrator service.
// Values can be populated from environment variables, config files,
// or programmatically for testing.
//
// # Required Fields
//
// None - all fields have sensible defaults.
//
// # Optional Fields
//
// All fields are optional with defaults applied by New().
//
// # Examples
//
//	// Minimal config (uses all defaults)
//	cfg := Config{}
//
//	// Custom port and LLM backend
//	cfg := Config{
//	    Port:       8080,
//	    LLMBackend: "claude",
//	}
//
//	// Full configuration
//	cfg := Config{
//	    Port:            12210,
//	    LLMBackend:      "openai",
//	    WeaviateURL:     "http://localhost:8080",
//	    OTelEndpoint:    "localhost:4317",
//	    EnableMetrics:   true,
//	}
type Config struct {
	// Port is the HTTP server port. Default: 12210
	Port int

	// LLMBackend specifies the LLM provider.
	// Valid values: "local", "openai", "ollama", "claude", "anthropic"
	// Default: "local"
	LLMBackend string

	// WeaviateURL is the Weaviate vector database URL.
	// If empty, vector DB features are disabled.
	// Example: "http://localhost:8080"
	WeaviateURL string

	// OTelEndpoint is the OpenTelemetry collector endpoint.
	// Default: "aleutian-otel-collector:4317"
	OTelEndpoint string

	// EnableMetrics enables Prometheus metrics endpoint.
	// Default: true
	EnableMetrics bool

	// GinMode sets the Gin framework mode.
	// Valid values: "debug", "release", "test"
	// Default: uses GIN_MODE env var or "debug"
	GinMode string

	// TTLCleanupInterval is how often the TTL scheduler runs cleanup.
	// Default: 1 hour
	TTLCleanupInterval time.Duration

	// TTLLogPath is the path to the TTL audit log file.
	// Default: "./logs/ttl_cleanup.log"
	TTLLogPath string

	// TTLEnabled enables the background TTL cleanup scheduler.
	// Default: true (when Weaviate is configured)
	TTLEnabled bool
}

// =============================================================================
// Implementation
// =============================================================================

// service implements Service for production use.
//
// # Description
//
// service is the main implementation that coordinates:
//   - HTTP routing via Gin
//   - LLM client management
//   - Policy engine for data classification
//   - Optional Weaviate integration
//   - OpenTelemetry tracing
//   - Prometheus metrics
//
// # Fields
//
//   - config: Service configuration
//   - opts: Extension options for enterprise features
//   - router: Gin HTTP engine
//   - llmClient: LLM provider client
//   - policyEngine: Data classification engine
//   - weaviateClient: Vector database client (may be nil)
//   - tracerCleanup: Function to shutdown tracer on exit
//
// # Thread Safety
//
// Thread-safe after construction. All fields are read-only after New() returns.
//
// # Limitations
//
//   - No hot-reload of configuration
//   - Single LLM backend per instance
//
// # Assumptions
//
//   - All external services (LLM, Weaviate, OTel) are reachable if configured
type service struct {
	config         Config
	opts           extensions.ServiceOptions
	router         *gin.Engine
	llmClient      llm.LLMClient
	policyEngine   *policy_engine.PolicyEngine
	weaviateClient *weaviate.Client
	tracerCleanup  func(context.Context)
	ttlScheduler   ttl.TTLScheduler
	ttlLogger      ttl.TTLLogger
}

// =============================================================================
// Constructor
// =============================================================================

// New creates a new orchestrator Service with the given configuration.
//
// # Description
//
// New initializes all orchestrator components:
//  1. Applies default configuration for missing values
//  2. Initializes OpenTelemetry tracing
//  3. Initializes Prometheus metrics
//  4. Creates LLM client based on backend type
//  5. Creates Weaviate client if URL provided
//  6. Initializes policy engine
//  7. Sets up HTTP routes with extension options
//
// If opts is nil, DefaultOptions() is used (no-op implementations).
//
// # Inputs
//
//   - cfg: Service configuration. Zero values use defaults.
//   - opts: Extension options for enterprise features. May be nil.
//
// # Outputs
//
//   - Service: Ready-to-run orchestrator service
//   - error: Non-nil if initialization fails
//
// # Examples
//
//	// Open source usage (no-op extensions)
//	cfg := Config{Port: 12210, LLMBackend: "ollama"}
//	svc, err := New(cfg, nil)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	log.Fatal(svc.Run())
//
//	// Enterprise usage (custom extensions)
//	opts := &extensions.ServiceOptions{
//	    AuthProvider: myAuthProvider,
//	    AuditLogger:  myAuditLogger,
//	}
//	svc, err := New(cfg, opts)
//
// # Limitations
//
//   - LLM client creation may fail if provider is unreachable
//   - Weaviate connection is optional but schema check runs if connected
//
// # Assumptions
//
//   - Environment variables are set for LLM providers (API keys, URLs)
//   - Network is available for external service connections
func New(cfg Config, opts *extensions.ServiceOptions) (Service, error) {
	s := &service{
		config: applyConfigDefaults(cfg),
	}

	// Apply extension options (use defaults if nil)
	if opts != nil {
		s.opts = *opts
	} else {
		s.opts = extensions.DefaultOptions()
	}

	// Initialize OpenTelemetry tracer
	cleanup, err := s.initTracer()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize tracer: %w", err)
	}
	s.tracerCleanup = cleanup

	// Initialize Prometheus metrics
	if s.config.EnableMetrics {
		observability.InitMetrics()
		slog.Info("Initialized Prometheus metrics for streaming")
	}

	// Initialize Weaviate client (optional)
	if err := s.initWeaviate(); err != nil {
		slog.Warn("Weaviate initialization failed, running in lightweight mode",
			"error", err)
		// Not fatal - continue without Weaviate
	}

	// Initialize TTL scheduler (only if Weaviate is available)
	if s.weaviateClient != nil && s.config.TTLEnabled {
		if err := s.initTTLScheduler(); err != nil {
			slog.Warn("TTL scheduler initialization failed",
				"error", err)
			// Not fatal - continue without TTL cleanup
		}
	}

	// Initialize policy engine
	s.policyEngine, err = policy_engine.NewPolicyEngine()
	if err != nil {
		s.cleanup()
		return nil, fmt.Errorf("failed to initialize policy engine: %w", err)
	}

	// Initialize LLM client
	if err := s.initLLMClient(); err != nil {
		s.cleanup()
		return nil, fmt.Errorf("failed to initialize LLM client: %w", err)
	}

	// Setup HTTP router
	s.initRouter()

	return s, nil
}

// =============================================================================
// Service Interface Methods
// =============================================================================

// Run starts the HTTP server and blocks until shutdown or error.
//
// # Description
//
// Starts the Gin HTTP server on the configured port. This method
// blocks until the server stops due to error or shutdown signal.
//
// # Inputs
//
// None.
//
// # Outputs
//
//   - error: Non-nil if server fails to start or encounters fatal error
//
// # Examples
//
//	if err := svc.Run(); err != nil {
//	    log.Fatalf("server error: %v", err)
//	}
//
// # Limitations
//
//   - Blocks until server stops
//   - Cleanup is automatic on return
//
// # Assumptions
//
//   - Service was successfully created via New()
//   - Port is available
func (s *service) Run() error {
	defer s.cleanup()

	addr := fmt.Sprintf(":%d", s.config.Port)
	slog.Info("Starting orchestrator server", "port", s.config.Port)

	return s.router.Run(addr)
}

// Router returns the underlying Gin engine for testing.
//
// # Description
//
// Provides access to the configured Gin router for integration testing.
//
// # Outputs
//
//   - *gin.Engine: The configured router
//
// # Limitations
//
//   - Should not be used to modify routes after construction
//
// # Assumptions
//
//   - Caller will not modify the router
func (s *service) Router() *gin.Engine {
	return s.router
}

// =============================================================================
// Private Initialization Methods
// =============================================================================

// applyConfigDefaults fills in missing configuration values.
//
// # Description
//
// Applies sensible defaults for any zero-valued configuration fields.
//
// # Inputs
//
//   - cfg: User-provided configuration
//
// # Outputs
//
//   - Config: Configuration with defaults applied
func applyConfigDefaults(cfg Config) Config {
	if cfg.Port == 0 {
		cfg.Port = 12210
	}
	if cfg.LLMBackend == "" {
		cfg.LLMBackend = "local"
	}
	if cfg.OTelEndpoint == "" {
		cfg.OTelEndpoint = "aleutian-otel-collector:4317"
	}
	// EnableMetrics defaults to true (zero value is false, so we need explicit check)
	// We'll handle this by always enabling unless explicitly disabled via a setter
	cfg.EnableMetrics = true

	// TTL defaults
	if cfg.TTLCleanupInterval == 0 {
		cfg.TTLCleanupInterval = 1 * time.Hour
	}
	if cfg.TTLLogPath == "" {
		cfg.TTLLogPath = "./logs/ttl_cleanup.log"
	}
	// TTLEnabled defaults to true (will only run if Weaviate is configured)
	cfg.TTLEnabled = true

	return cfg
}

// initTracer initializes OpenTelemetry distributed tracing.
//
// # Description
//
// Sets up OTLP trace exporter to send spans to the configured collector.
//
// # Outputs
//
//   - func(context.Context): Cleanup function to call on shutdown
//   - error: Non-nil if tracer setup fails
//
// # Limitations
//
//   - Uses insecure gRPC connection (appropriate for internal networks)
//
// # Assumptions
//
//   - OTel collector is reachable at configured endpoint
func (s *service) initTracer() (func(context.Context), error) {
	ctx := context.Background()

	conn, err := grpc.NewClient(s.config.OTelEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection: %w", err)
	}

	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceNameKey.String("orchestrator-service")))
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	bsp := sdktrace.NewBatchSpanProcessor(traceExporter)
	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp))

	otel.SetTracerProvider(traceProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))

	cleanup := func(ctx context.Context) {
		ctx, cancel := context.WithTimeout(ctx, time.Second*5)
		defer cancel()
		if err := traceExporter.Shutdown(ctx); err != nil {
			slog.Error("failed to shutdown OTLP exporter", "error", err)
		}
	}

	return cleanup, nil
}

// initWeaviate initializes the Weaviate vector database client.
//
// # Description
//
// Creates a Weaviate client if WeaviateURL is configured.
// Validates the URL format and ensures schema is created.
//
// # Outputs
//
//   - error: Non-nil if Weaviate initialization fails
//
// # Limitations
//
//   - Returns nil error if WeaviateURL is empty (optional dependency)
//
// # Assumptions
//
//   - Weaviate server is running and accessible
func (s *service) initWeaviate() error {
	weaviateURL := strings.Trim(s.config.WeaviateURL, "\"' ")

	if weaviateURL == "" || !strings.Contains(weaviateURL, "http") {
		slog.Info("Weaviate URL not configured, running in lightweight mode")
		return nil
	}

	parsedURL, err := url.Parse(weaviateURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return fmt.Errorf("invalid Weaviate URL: %s", weaviateURL)
	}

	clientConf := weaviate.Config{
		Host:   parsedURL.Host,
		Scheme: parsedURL.Scheme,
	}

	s.weaviateClient, err = weaviate.NewClient(clientConf)
	if err != nil {
		return fmt.Errorf("failed to create Weaviate client: %w", err)
	}

	datatypes.EnsureWeaviateSchema(s.weaviateClient)
	slog.Info("Weaviate client initialized", "url", weaviateURL)

	return nil
}

// initLLMClient initializes the LLM provider client.
//
// # Description
//
// Creates the appropriate LLM client based on the configured backend type.
//
// # Outputs
//
//   - error: Non-nil if LLM client creation fails
//
// # Limitations
//
//   - Only supports: local, openai, ollama, claude/anthropic
//
// # Assumptions
//
//   - Required environment variables are set for the chosen provider
func (s *service) initLLMClient() error {
	var err error

	switch s.config.LLMBackend {
	case "local":
		s.llmClient, err = llm.NewLocalLlamaCppClient()
		slog.Info("Using Local Llama.cpp LLM backend")
	case "openai":
		s.llmClient, err = llm.NewOpenAIClient()
		slog.Info("Using OpenAI LLM backend")
	case "ollama":
		s.llmClient, err = llm.NewOllamaClient()
		slog.Info("Using Ollama LLM backend")
	case "claude", "anthropic":
		s.llmClient, err = llm.NewAnthropicClient()
		slog.Info("Using Anthropic (Claude) LLM backend")
	default:
		slog.Warn("Unknown LLM backend, defaulting to local", "backend", s.config.LLMBackend)
		s.llmClient, err = llm.NewLocalLlamaCppClient()
	}

	return err
}

// initRouter sets up the Gin HTTP router with all routes.
//
// # Description
//
// Creates the Gin engine, applies middleware, and registers all routes.
// Routes are configured based on available dependencies (e.g., Weaviate).
// ServiceOptions are passed through to enable enterprise extensions.
//
// # Limitations
//
//   - Routes are fixed after initialization
//
// # Assumptions
//
//   - All dependencies (LLM, policy engine) are initialized
func (s *service) initRouter() {
	s.router = gin.Default()
	s.router.Use(otelgin.Middleware("orchestrator-service"))

	routes.SetupRoutes(s.router, s.weaviateClient, s.llmClient, s.policyEngine, s.opts)
}

// cleanup releases all resources held by the service.
//
// # Description
//
// Called when Run() exits or on initialization failure.
// Shuts down tracer, TTL scheduler, and any other cleanup tasks.
func (s *service) cleanup() {
	// Stop TTL scheduler
	if s.ttlScheduler != nil {
		if err := s.ttlScheduler.Stop(); err != nil {
			slog.Warn("TTL scheduler stop error", "error", err)
		}
	}

	// Close TTL logger
	if s.ttlLogger != nil {
		if err := s.ttlLogger.Close(); err != nil {
			slog.Warn("TTL logger close error", "error", err)
		}
	}

	// Shutdown tracer
	if s.tracerCleanup != nil {
		s.tracerCleanup(context.Background())
	}
}

// initTTLScheduler initializes the background TTL cleanup scheduler.
//
// # Description
//
// Creates the TTL service, logger, and scheduler components. Starts the
// scheduler as a background goroutine that periodically cleans up expired
// documents and sessions.
//
// # Outputs
//
//   - error: Non-nil if scheduler initialization fails
//
// # Limitations
//
//   - Requires Weaviate client to be initialized
//   - Log directory must be writable
//
// # Assumptions
//
//   - Weaviate client is available (checked by caller)
//   - TTLEnabled is true (checked by caller)
func (s *service) initTTLScheduler() error {
	// Create TTL service backed by Weaviate
	ttlService := ttl.NewTTLService(s.weaviateClient)

	// Create TTL audit logger
	logger, err := ttl.NewTTLLogger(s.config.TTLLogPath)
	if err != nil {
		slog.Warn("Failed to create TTL audit logger, continuing without audit log",
			"log_path", s.config.TTLLogPath,
			"error", err)
		// Continue without audit logger - slog will still capture logs
	} else {
		s.ttlLogger = logger
	}

	// Create scheduler configuration
	schedulerConfig := ttl.DefaultSchedulerConfig()
	schedulerConfig.Interval = s.config.TTLCleanupInterval

	// Create and start scheduler
	s.ttlScheduler = ttl.NewTTLScheduler(ttlService, s.ttlLogger, schedulerConfig)

	// Start scheduler in background
	ctx := context.Background()
	if err := s.ttlScheduler.Start(ctx); err != nil {
		return fmt.Errorf("failed to start TTL scheduler: %w", err)
	}

	slog.Info("TTL cleanup scheduler started",
		"interval", s.config.TTLCleanupInterval.String(),
		"log_path", s.config.TTLLogPath,
	)

	return nil
}

// =============================================================================
// Compile-time Interface Compliance
// =============================================================================

var _ Service = (*service)(nil)
