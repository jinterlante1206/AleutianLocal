// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package handlers provides HTTP handlers for the orchestrator service.
//
// This file contains URL routing logic for external services, centralizing
// endpoint resolution and configuration. It handles environment variable
// resolution with backwards compatibility for deprecated variable names.
//
// Note: This does NOT handle model-to-container routing. That is handled
// by Sapheneia's orchestration layer. AleutianFOSS specifies the model
// in the request body, and Sapheneia routes to the appropriate container.
package handlers

import (
	"log/slog"
	"os"
)

// =============================================================================
// INTERFACES
// =============================================================================

// ServiceRouter defines the contract for service URL resolution.
//
// Description:
//
//	ServiceRouter provides a testable interface for resolving external
//	service URLs based on deployment configuration and environment variables.
//	This enables mocking in unit tests without needing real services.
//
// Implementations:
//   - DefaultServiceRouter (production - reads from environment)
//   - MockServiceRouter (testing - returns configured values)
//
// Example:
//
//	// Production usage
//	router := NewDefaultServiceRouter("standalone")
//	url := router.GetOrchestrationURL()
//
//	// Test usage
//	router := &MockServiceRouter{OrchestrationURL: "http://mock:8000"}
//	evaluator := NewEvaluatorWithRouter(router)
//
// Limitations:
//   - Does not validate URL format
//   - Does not test connectivity
//
// Assumptions:
//   - Environment variables are set before first call
//   - URLs do not have trailing slashes
type ServiceRouter interface {
	// GetOrchestrationURL returns the Sapheneia orchestration endpoint URL.
	// This is where inference requests are sent (POST /orchestration/v1/predict).
	GetOrchestrationURL() string

	// GetTradingURL returns the Sapheneia trading service endpoint URL.
	// This is where trading signals are sent (POST /trading/execute).
	GetTradingURL() string

	// GetInfluxDBURL returns the InfluxDB endpoint URL.
	// This is where time-series data is stored and queried.
	GetInfluxDBURL() string
}

// =============================================================================
// STRUCTS
// =============================================================================

// DefaultServiceRouter resolves service URLs from environment variables.
//
// Description:
//
//	DefaultServiceRouter implements ServiceRouter by reading URLs from
//	environment variables with sensible defaults. It supports both new
//	(preferred) and legacy (deprecated) environment variable names for
//	backwards compatibility.
//
// Fields:
//   - deploymentMode: "standalone" or "distributed" (affects defaults)
//
// Environment Variable Priority:
//
//	For each service, the router checks environment variables in order:
//	1. New preferred name (no warning)
//	2. Legacy deprecated name (logs warning)
//	3. Default value based on deployment mode
//
// Example:
//
//	router := NewDefaultServiceRouter("standalone")
//	orchURL := router.GetOrchestrationURL()
//	// Returns SAPHENEIA_ORCHESTRATION_URL if set,
//	// else ORCHESTRATOR_URL (with warning),
//	// else "http://localhost:12210"
//
// Limitations:
//   - Reads environment at call time (no caching)
//   - Deprecation warnings logged on every call (consider caching if noisy)
//
// Assumptions:
//   - Caller provides valid deployment mode
//   - Environment variables are set correctly
type DefaultServiceRouter struct {
	deploymentMode string
}

// =============================================================================
// CONSTRUCTORS
// =============================================================================

// NewDefaultServiceRouter creates a new DefaultServiceRouter.
//
// Description:
//
//	NewDefaultServiceRouter initializes the router with the given deployment
//	mode, which affects default URL resolution when environment variables
//	are not set.
//
// Inputs:
//   - deploymentMode: "standalone" (local dev) or "distributed" (Sapheneia cluster)
//
// Outputs:
//   - *DefaultServiceRouter: The configured router
//
// Example:
//
//	// For local development
//	router := NewDefaultServiceRouter("standalone")
//
//	// For connecting to Sapheneia cluster
//	router := NewDefaultServiceRouter("distributed")
//
// Limitations:
//   - Does not validate deployment mode value
//
// Assumptions:
//   - Valid deployment modes are "standalone" and "distributed"
//   - Invalid modes treated as "distributed" for defaults
func NewDefaultServiceRouter(deploymentMode string) *DefaultServiceRouter {
	return &DefaultServiceRouter{
		deploymentMode: deploymentMode,
	}
}

// =============================================================================
// METHODS
// =============================================================================

// GetOrchestrationURL returns the orchestration service URL.
//
// Description:
//
//	GetOrchestrationURL resolves the URL for the Sapheneia orchestration
//	endpoint. This is where inference requests are sent. The orchestration
//	service handles model routing internally - AleutianFOSS just specifies
//	the model name in the request body.
//
// Inputs:
//
//	None (uses receiver state and environment)
//
// Outputs:
//   - string: The fully-qualified URL (e.g., "http://localhost:12210")
//
// Environment Variables (checked in order):
//  1. SAPHENEIA_ORCHESTRATION_URL (new, preferred)
//  2. ORCHESTRATOR_URL (legacy, deprecated - logs warning)
//  3. Default based on deployment mode:
//     - standalone: "http://localhost:12210"
//     - distributed: "http://sapheneia-orchestration:8000"
//
// Example:
//
//	router := NewDefaultServiceRouter("standalone")
//	url := router.GetOrchestrationURL()
//	// With no env vars: "http://localhost:12210"
//	// With SAPHENEIA_ORCHESTRATION_URL=http://custom:8000: "http://custom:8000"
//
// Limitations:
//   - Does not validate URL format
//   - Does not test connectivity
//   - Logs warning on every call if using deprecated env var
//
// Assumptions:
//   - URLs do not have trailing slashes
//   - Default ports are correct for local development
func (r *DefaultServiceRouter) GetOrchestrationURL() string {
	// Priority 1: New environment variable (preferred)
	if url := os.Getenv("SAPHENEIA_ORCHESTRATION_URL"); url != "" {
		return url
	}

	// Priority 2: Legacy environment variable (deprecated, with warning)
	if url := os.Getenv("ORCHESTRATOR_URL"); url != "" {
		slog.Warn("ORCHESTRATOR_URL is deprecated and will be removed in v2.0, use SAPHENEIA_ORCHESTRATION_URL instead")
		return url
	}

	// Priority 3: Default based on deployment mode
	if r.deploymentMode == "standalone" {
		return "http://localhost:12210"
	}
	return "http://sapheneia-orchestration:8000"
}

// GetTradingURL returns the trading service URL.
//
// Description:
//
//	GetTradingURL resolves the URL for the Sapheneia trading service.
//	This is where trading signal requests are sent to get buy/sell/hold
//	decisions based on forecasts.
//
// Inputs:
//
//	None (uses environment)
//
// Outputs:
//   - string: The fully-qualified URL (e.g., "http://localhost:12132")
//
// Environment Variables (checked in order):
//  1. SAPHENEIA_TRADING_URL (new, preferred)
//  2. SAPHENEIA_TRADING_SERVICE_URL (legacy, deprecated - logs warning)
//  3. Default: "http://localhost:12132"
//
// Example:
//
//	url := router.GetTradingURL()
//	// With no env vars: "http://localhost:12132"
//
// Limitations:
//   - Does not validate URL format
//   - Logs warning on every call if using deprecated env var
//
// Assumptions:
//   - Trading service is always available at resolved URL
func (r *DefaultServiceRouter) GetTradingURL() string {
	// Priority 1: New environment variable (preferred)
	if url := os.Getenv("SAPHENEIA_TRADING_URL"); url != "" {
		return url
	}

	// Priority 2: Legacy environment variable (deprecated, with warning)
	if url := os.Getenv("SAPHENEIA_TRADING_SERVICE_URL"); url != "" {
		slog.Warn("SAPHENEIA_TRADING_SERVICE_URL is deprecated and will be removed in v2.0, use SAPHENEIA_TRADING_URL instead")
		return url
	}

	// Priority 3: Default
	return "http://localhost:12132"
}

// GetInfluxDBURL returns the InfluxDB URL.
//
// Description:
//
//	GetInfluxDBURL resolves the URL for the InfluxDB time-series database.
//	This is where historical price data is stored and queried for
//	backtesting scenarios.
//
// Inputs:
//
//	None (uses environment)
//
// Outputs:
//   - string: The fully-qualified URL (e.g., "http://localhost:12130")
//
// Environment Variables:
//  1. INFLUXDB_URL
//  2. Default: "http://localhost:12130"
//
// Example:
//
//	url := router.GetInfluxDBURL()
//	// With no env vars: "http://localhost:12130"
//
// Limitations:
//   - Does not validate connectivity
//
// Assumptions:
//   - InfluxDB is running and accessible at resolved URL
func (r *DefaultServiceRouter) GetInfluxDBURL() string {
	if url := os.Getenv("INFLUXDB_URL"); url != "" {
		return url
	}
	return "http://localhost:12130"
}

// =============================================================================
// MOCK IMPLEMENTATION (for testing)
// =============================================================================

// MockServiceRouter is a test double for ServiceRouter.
//
// Description:
//
//	MockServiceRouter implements ServiceRouter with configurable URLs
//	for use in unit tests. This allows testing evaluator logic without
//	needing real external services.
//
// Fields:
//   - OrchestrationURL: URL to return from GetOrchestrationURL()
//   - TradingURL: URL to return from GetTradingURL()
//   - InfluxDBURL: URL to return from GetInfluxDBURL()
//
// Example:
//
//	mock := &MockServiceRouter{
//	    OrchestrationURL: server.URL,
//	    TradingURL:       tradingServer.URL,
//	    InfluxDBURL:      influxServer.URL,
//	}
//	evaluator := NewEvaluatorWithRouter(mock)
//
// Limitations:
//   - Returns empty string if field not set
//
// Assumptions:
//   - Used only in test code
type MockServiceRouter struct {
	OrchestrationURL string
	TradingURL       string
	InfluxDBURL      string
}

// GetOrchestrationURL returns the configured orchestration URL.
func (m *MockServiceRouter) GetOrchestrationURL() string {
	return m.OrchestrationURL
}

// GetTradingURL returns the configured trading URL.
func (m *MockServiceRouter) GetTradingURL() string {
	return m.TradingURL
}

// GetInfluxDBURL returns the configured InfluxDB URL.
func (m *MockServiceRouter) GetInfluxDBURL() string {
	return m.InfluxDBURL
}

// =============================================================================
// TYPE ASSERTION COMPILE CHECKS
// =============================================================================

var _ ServiceRouter = (*DefaultServiceRouter)(nil)
var _ ServiceRouter = (*MockServiceRouter)(nil)
