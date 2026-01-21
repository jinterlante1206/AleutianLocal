// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package handlers

import (
	"os"
	"testing"
)

// =============================================================================
// TEST HELPERS
// =============================================================================

// clearRoutingEnvVars removes all routing-related environment variables.
//
// Description:
//
//	clearRoutingEnvVars ensures test isolation by removing all environment
//	variables that might affect routing behavior. Call this at the start
//	of each test to ensure a clean state.
//
// Inputs:
//
//	t: Test context for cleanup registration
//
// Outputs:
//
//	None
//
// Example:
//
//	func TestSomething(t *testing.T) {
//	    clearRoutingEnvVars(t)
//	    // test code...
//	}
//
// Limitations:
//   - Does not capture original values (uses t.Cleanup for restoration)
//
// Assumptions:
//   - Called at start of test before any env vars are set
func clearRoutingEnvVars(t *testing.T) {
	t.Helper()

	envVars := []string{
		"SAPHENEIA_ORCHESTRATION_URL",
		"ORCHESTRATOR_URL",
		"SAPHENEIA_TRADING_URL",
		"SAPHENEIA_TRADING_SERVICE_URL",
		"INFLUXDB_URL",
	}

	// Save original values and clear
	originalValues := make(map[string]string)
	for _, key := range envVars {
		if val, exists := os.LookupEnv(key); exists {
			originalValues[key] = val
		}
		os.Unsetenv(key)
	}

	// Restore original values on cleanup
	t.Cleanup(func() {
		for _, key := range envVars {
			os.Unsetenv(key)
			if val, exists := originalValues[key]; exists {
				os.Setenv(key, val)
			}
		}
	})
}

// =============================================================================
// DefaultServiceRouter TESTS - GetOrchestrationURL
// =============================================================================

// TestDefaultServiceRouter_GetOrchestrationURL_PreferredEnvVar tests that
// the preferred environment variable takes priority.
//
// Description:
//
//	When SAPHENEIA_ORCHESTRATION_URL is set, it should be returned
//	regardless of whether legacy env vars or defaults would apply.
//
// Inputs:
//
//	None (uses environment variables)
//
// Outputs:
//
//	URL from SAPHENEIA_ORCHESTRATION_URL
//
// Example:
//
//	SAPHENEIA_ORCHESTRATION_URL=http://custom:8000 -> "http://custom:8000"
//
// Limitations:
//
//	None
//
// Assumptions:
//   - Environment variables can be set/unset during test
func TestDefaultServiceRouter_GetOrchestrationURL_PreferredEnvVar(t *testing.T) {
	clearRoutingEnvVars(t)

	expected := "http://sapheneia-custom:9000"
	os.Setenv("SAPHENEIA_ORCHESTRATION_URL", expected)

	router := NewDefaultServiceRouter("standalone")
	actual := router.GetOrchestrationURL()

	if actual != expected {
		t.Errorf("GetOrchestrationURL() = %q, want %q", actual, expected)
	}
}

// TestDefaultServiceRouter_GetOrchestrationURL_LegacyEnvVar tests fallback
// to the deprecated ORCHESTRATOR_URL environment variable.
//
// Description:
//
//	When SAPHENEIA_ORCHESTRATION_URL is not set but ORCHESTRATOR_URL is,
//	the legacy value should be returned. This tests backwards compatibility.
//
// Inputs:
//
//	None (uses environment variables)
//
// Outputs:
//
//	URL from ORCHESTRATOR_URL (with deprecation warning logged)
//
// Example:
//
//	ORCHESTRATOR_URL=http://legacy:8000 -> "http://legacy:8000"
//
// Limitations:
//   - Does not verify deprecation warning is logged (would need log capture)
//
// Assumptions:
//   - Legacy env var support will be removed in v2.0
func TestDefaultServiceRouter_GetOrchestrationURL_LegacyEnvVar(t *testing.T) {
	clearRoutingEnvVars(t)

	expected := "http://legacy-orchestrator:7000"
	os.Setenv("ORCHESTRATOR_URL", expected)

	router := NewDefaultServiceRouter("standalone")
	actual := router.GetOrchestrationURL()

	if actual != expected {
		t.Errorf("GetOrchestrationURL() = %q, want %q", actual, expected)
	}
}

// TestDefaultServiceRouter_GetOrchestrationURL_PreferredOverLegacy tests that
// the preferred env var takes precedence over the legacy one.
//
// Description:
//
//	When both SAPHENEIA_ORCHESTRATION_URL and ORCHESTRATOR_URL are set,
//	the preferred (new) variable should win.
//
// Inputs:
//
//	None (uses environment variables)
//
// Outputs:
//
//	URL from SAPHENEIA_ORCHESTRATION_URL (ignoring ORCHESTRATOR_URL)
//
// Example:
//
//	SAPHENEIA_ORCHESTRATION_URL=http://new:8000
//	ORCHESTRATOR_URL=http://old:8000
//	-> "http://new:8000"
//
// Limitations:
//
//	None
//
// Assumptions:
//   - Both env vars should not normally be set simultaneously
func TestDefaultServiceRouter_GetOrchestrationURL_PreferredOverLegacy(t *testing.T) {
	clearRoutingEnvVars(t)

	preferred := "http://preferred:8000"
	legacy := "http://legacy:8000"
	os.Setenv("SAPHENEIA_ORCHESTRATION_URL", preferred)
	os.Setenv("ORCHESTRATOR_URL", legacy)

	router := NewDefaultServiceRouter("standalone")
	actual := router.GetOrchestrationURL()

	if actual != preferred {
		t.Errorf("GetOrchestrationURL() = %q, want %q (preferred over legacy)", actual, preferred)
	}
}

// TestDefaultServiceRouter_GetOrchestrationURL_StandaloneDefault tests the
// default URL for standalone deployment mode.
//
// Description:
//
//	When no environment variables are set and deployment mode is "standalone",
//	the default localhost URL should be returned.
//
// Inputs:
//
//	deploymentMode: "standalone"
//
// Outputs:
//
//	"http://localhost:12210"
//
// Example:
//
//	router := NewDefaultServiceRouter("standalone")
//	url := router.GetOrchestrationURL() // "http://localhost:12210"
//
// Limitations:
//
//	None
//
// Assumptions:
//   - Port 12210 is the standard local Sapheneia orchestration port
func TestDefaultServiceRouter_GetOrchestrationURL_StandaloneDefault(t *testing.T) {
	clearRoutingEnvVars(t)

	router := NewDefaultServiceRouter("standalone")
	expected := "http://localhost:12210"
	actual := router.GetOrchestrationURL()

	if actual != expected {
		t.Errorf("GetOrchestrationURL() with standalone mode = %q, want %q", actual, expected)
	}
}

// TestDefaultServiceRouter_GetOrchestrationURL_DistributedDefault tests the
// default URL for distributed deployment mode.
//
// Description:
//
//	When no environment variables are set and deployment mode is "distributed",
//	the default Kubernetes service URL should be returned.
//
// Inputs:
//
//	deploymentMode: "distributed"
//
// Outputs:
//
//	"http://sapheneia-orchestration:8000"
//
// Example:
//
//	router := NewDefaultServiceRouter("distributed")
//	url := router.GetOrchestrationURL() // "http://sapheneia-orchestration:8000"
//
// Limitations:
//
//	None
//
// Assumptions:
//   - Kubernetes service name is "sapheneia-orchestration"
func TestDefaultServiceRouter_GetOrchestrationURL_DistributedDefault(t *testing.T) {
	clearRoutingEnvVars(t)

	router := NewDefaultServiceRouter("distributed")
	expected := "http://sapheneia-orchestration:8000"
	actual := router.GetOrchestrationURL()

	if actual != expected {
		t.Errorf("GetOrchestrationURL() with distributed mode = %q, want %q", actual, expected)
	}
}

// TestDefaultServiceRouter_GetOrchestrationURL_UnknownModeTreatedAsDistributed tests
// that unknown deployment modes fall back to distributed defaults.
//
// Description:
//
//	When an unrecognized deployment mode is provided, the router should
//	default to distributed behavior (safer for production).
//
// Inputs:
//
//	deploymentMode: "unknown-mode"
//
// Outputs:
//
//	"http://sapheneia-orchestration:8000" (distributed default)
//
// Example:
//
//	router := NewDefaultServiceRouter("typo-mode")
//	url := router.GetOrchestrationURL() // distributed default
//
// Limitations:
//   - No warning logged for unknown mode
//
// Assumptions:
//   - Unknown modes defaulting to distributed is safest behavior
func TestDefaultServiceRouter_GetOrchestrationURL_UnknownModeTreatedAsDistributed(t *testing.T) {
	clearRoutingEnvVars(t)

	router := NewDefaultServiceRouter("unknown-mode")
	expected := "http://sapheneia-orchestration:8000"
	actual := router.GetOrchestrationURL()

	if actual != expected {
		t.Errorf("GetOrchestrationURL() with unknown mode = %q, want %q (distributed default)", actual, expected)
	}
}

// =============================================================================
// DefaultServiceRouter TESTS - GetTradingURL
// =============================================================================

// TestDefaultServiceRouter_GetTradingURL_PreferredEnvVar tests that
// the preferred environment variable takes priority for trading service.
//
// Description:
//
//	When SAPHENEIA_TRADING_URL is set, it should be returned.
//
// Inputs:
//
//	None (uses environment variables)
//
// Outputs:
//
//	URL from SAPHENEIA_TRADING_URL
//
// Example:
//
//	SAPHENEIA_TRADING_URL=http://trading:9000 -> "http://trading:9000"
//
// Limitations:
//
//	None
//
// Assumptions:
//   - Environment variables can be set/unset during test
func TestDefaultServiceRouter_GetTradingURL_PreferredEnvVar(t *testing.T) {
	clearRoutingEnvVars(t)

	expected := "http://sapheneia-trading:9000"
	os.Setenv("SAPHENEIA_TRADING_URL", expected)

	router := NewDefaultServiceRouter("standalone")
	actual := router.GetTradingURL()

	if actual != expected {
		t.Errorf("GetTradingURL() = %q, want %q", actual, expected)
	}
}

// TestDefaultServiceRouter_GetTradingURL_LegacyEnvVar tests fallback
// to the deprecated SAPHENEIA_TRADING_SERVICE_URL environment variable.
//
// Description:
//
//	When SAPHENEIA_TRADING_URL is not set but SAPHENEIA_TRADING_SERVICE_URL
//	is, the legacy value should be returned.
//
// Inputs:
//
//	None (uses environment variables)
//
// Outputs:
//
//	URL from SAPHENEIA_TRADING_SERVICE_URL (with deprecation warning logged)
//
// Example:
//
//	SAPHENEIA_TRADING_SERVICE_URL=http://legacy:8000 -> "http://legacy:8000"
//
// Limitations:
//   - Does not verify deprecation warning is logged
//
// Assumptions:
//   - Legacy env var support will be removed in v2.0
func TestDefaultServiceRouter_GetTradingURL_LegacyEnvVar(t *testing.T) {
	clearRoutingEnvVars(t)

	expected := "http://legacy-trading:7000"
	os.Setenv("SAPHENEIA_TRADING_SERVICE_URL", expected)

	router := NewDefaultServiceRouter("standalone")
	actual := router.GetTradingURL()

	if actual != expected {
		t.Errorf("GetTradingURL() = %q, want %q", actual, expected)
	}
}

// TestDefaultServiceRouter_GetTradingURL_PreferredOverLegacy tests that
// the preferred env var takes precedence over the legacy one.
//
// Description:
//
//	When both SAPHENEIA_TRADING_URL and SAPHENEIA_TRADING_SERVICE_URL are set,
//	the preferred (new) variable should win.
//
// Inputs:
//
//	None (uses environment variables)
//
// Outputs:
//
//	URL from SAPHENEIA_TRADING_URL (ignoring legacy)
//
// Example:
//
//	SAPHENEIA_TRADING_URL=http://new:8000
//	SAPHENEIA_TRADING_SERVICE_URL=http://old:8000
//	-> "http://new:8000"
//
// Limitations:
//
//	None
//
// Assumptions:
//   - Both env vars should not normally be set simultaneously
func TestDefaultServiceRouter_GetTradingURL_PreferredOverLegacy(t *testing.T) {
	clearRoutingEnvVars(t)

	preferred := "http://preferred-trading:8000"
	legacy := "http://legacy-trading:8000"
	os.Setenv("SAPHENEIA_TRADING_URL", preferred)
	os.Setenv("SAPHENEIA_TRADING_SERVICE_URL", legacy)

	router := NewDefaultServiceRouter("standalone")
	actual := router.GetTradingURL()

	if actual != preferred {
		t.Errorf("GetTradingURL() = %q, want %q (preferred over legacy)", actual, preferred)
	}
}

// TestDefaultServiceRouter_GetTradingURL_Default tests the default trading URL.
//
// Description:
//
//	When no environment variables are set, the default localhost URL
//	should be returned (regardless of deployment mode for trading).
//
// Inputs:
//
//	None
//
// Outputs:
//
//	"http://localhost:12132"
//
// Example:
//
//	router := NewDefaultServiceRouter("standalone")
//	url := router.GetTradingURL() // "http://localhost:12132"
//
// Limitations:
//   - Trading service always defaults to localhost (no distributed default)
//
// Assumptions:
//   - Port 12132 is the standard local Sapheneia trading port
func TestDefaultServiceRouter_GetTradingURL_Default(t *testing.T) {
	clearRoutingEnvVars(t)

	router := NewDefaultServiceRouter("standalone")
	expected := "http://localhost:12132"
	actual := router.GetTradingURL()

	if actual != expected {
		t.Errorf("GetTradingURL() = %q, want %q", actual, expected)
	}
}

// =============================================================================
// DefaultServiceRouter TESTS - GetInfluxDBURL
// =============================================================================

// TestDefaultServiceRouter_GetInfluxDBURL_EnvVar tests that
// the INFLUXDB_URL environment variable is used when set.
//
// Description:
//
//	When INFLUXDB_URL is set, it should be returned.
//
// Inputs:
//
//	None (uses environment variables)
//
// Outputs:
//
//	URL from INFLUXDB_URL
//
// Example:
//
//	INFLUXDB_URL=http://influx:8086 -> "http://influx:8086"
//
// Limitations:
//
//	None
//
// Assumptions:
//   - Environment variables can be set/unset during test
func TestDefaultServiceRouter_GetInfluxDBURL_EnvVar(t *testing.T) {
	clearRoutingEnvVars(t)

	expected := "http://influxdb-custom:8086"
	os.Setenv("INFLUXDB_URL", expected)

	router := NewDefaultServiceRouter("standalone")
	actual := router.GetInfluxDBURL()

	if actual != expected {
		t.Errorf("GetInfluxDBURL() = %q, want %q", actual, expected)
	}
}

// TestDefaultServiceRouter_GetInfluxDBURL_Default tests the default InfluxDB URL.
//
// Description:
//
//	When INFLUXDB_URL is not set, the default localhost URL should be returned.
//
// Inputs:
//
//	None
//
// Outputs:
//
//	"http://localhost:12130"
//
// Example:
//
//	router := NewDefaultServiceRouter("standalone")
//	url := router.GetInfluxDBURL() // "http://localhost:12130"
//
// Limitations:
//
//	None
//
// Assumptions:
//   - Port 12130 is the standard local InfluxDB port
func TestDefaultServiceRouter_GetInfluxDBURL_Default(t *testing.T) {
	clearRoutingEnvVars(t)

	router := NewDefaultServiceRouter("standalone")
	expected := "http://localhost:12130"
	actual := router.GetInfluxDBURL()

	if actual != expected {
		t.Errorf("GetInfluxDBURL() = %q, want %q", actual, expected)
	}
}

// =============================================================================
// MockServiceRouter TESTS
// =============================================================================

// TestMockServiceRouter_GetOrchestrationURL tests that MockServiceRouter
// returns the configured OrchestrationURL.
//
// Description:
//
//	MockServiceRouter should return exactly what is configured in its
//	OrchestrationURL field, enabling predictable test behavior.
//
// Inputs:
//
//	OrchestrationURL field value
//
// Outputs:
//
//	The configured URL string
//
// Example:
//
//	mock := &MockServiceRouter{OrchestrationURL: "http://test:8000"}
//	url := mock.GetOrchestrationURL() // "http://test:8000"
//
// Limitations:
//   - Returns empty string if field not set
//
// Assumptions:
//   - Used only in test code
func TestMockServiceRouter_GetOrchestrationURL(t *testing.T) {
	expected := "http://mock-orchestration:8000"
	mock := &MockServiceRouter{
		OrchestrationURL: expected,
	}

	actual := mock.GetOrchestrationURL()
	if actual != expected {
		t.Errorf("MockServiceRouter.GetOrchestrationURL() = %q, want %q", actual, expected)
	}
}

// TestMockServiceRouter_GetTradingURL tests that MockServiceRouter
// returns the configured TradingURL.
//
// Description:
//
//	MockServiceRouter should return exactly what is configured in its
//	TradingURL field.
//
// Inputs:
//
//	TradingURL field value
//
// Outputs:
//
//	The configured URL string
//
// Example:
//
//	mock := &MockServiceRouter{TradingURL: "http://test:9000"}
//	url := mock.GetTradingURL() // "http://test:9000"
//
// Limitations:
//   - Returns empty string if field not set
//
// Assumptions:
//   - Used only in test code
func TestMockServiceRouter_GetTradingURL(t *testing.T) {
	expected := "http://mock-trading:9000"
	mock := &MockServiceRouter{
		TradingURL: expected,
	}

	actual := mock.GetTradingURL()
	if actual != expected {
		t.Errorf("MockServiceRouter.GetTradingURL() = %q, want %q", actual, expected)
	}
}

// TestMockServiceRouter_GetInfluxDBURL tests that MockServiceRouter
// returns the configured InfluxDBURL.
//
// Description:
//
//	MockServiceRouter should return exactly what is configured in its
//	InfluxDBURL field.
//
// Inputs:
//
//	InfluxDBURL field value
//
// Outputs:
//
//	The configured URL string
//
// Example:
//
//	mock := &MockServiceRouter{InfluxDBURL: "http://test:8086"}
//	url := mock.GetInfluxDBURL() // "http://test:8086"
//
// Limitations:
//   - Returns empty string if field not set
//
// Assumptions:
//   - Used only in test code
func TestMockServiceRouter_GetInfluxDBURL(t *testing.T) {
	expected := "http://mock-influxdb:8086"
	mock := &MockServiceRouter{
		InfluxDBURL: expected,
	}

	actual := mock.GetInfluxDBURL()
	if actual != expected {
		t.Errorf("MockServiceRouter.GetInfluxDBURL() = %q, want %q", actual, expected)
	}
}

// TestMockServiceRouter_EmptyFields tests that MockServiceRouter returns
// empty strings when fields are not configured.
//
// Description:
//
//	When MockServiceRouter is created with default (zero) values,
//	all URL methods should return empty strings.
//
// Inputs:
//
//	None (zero-value struct)
//
// Outputs:
//
//	Empty strings for all URL methods
//
// Example:
//
//	mock := &MockServiceRouter{}
//	url := mock.GetOrchestrationURL() // ""
//
// Limitations:
//
//	None
//
// Assumptions:
//   - Callers handle empty string appropriately
func TestMockServiceRouter_EmptyFields(t *testing.T) {
	mock := &MockServiceRouter{}

	if url := mock.GetOrchestrationURL(); url != "" {
		t.Errorf("Empty MockServiceRouter.GetOrchestrationURL() = %q, want empty string", url)
	}
	if url := mock.GetTradingURL(); url != "" {
		t.Errorf("Empty MockServiceRouter.GetTradingURL() = %q, want empty string", url)
	}
	if url := mock.GetInfluxDBURL(); url != "" {
		t.Errorf("Empty MockServiceRouter.GetInfluxDBURL() = %q, want empty string", url)
	}
}

// =============================================================================
// INTERFACE COMPLIANCE TESTS
// =============================================================================

// TestServiceRouter_InterfaceCompliance verifies that both implementations
// satisfy the ServiceRouter interface at compile time.
//
// Description:
//
//	This test ensures that DefaultServiceRouter and MockServiceRouter
//	both implement the ServiceRouter interface. The actual type assertion
//	checks are in routing.go, but this test uses the interface to verify
//	polymorphic behavior works correctly.
//
// Inputs:
//
//	Various ServiceRouter implementations
//
// Outputs:
//
//	None (compile-time verification)
//
// Example:
//
//	var router ServiceRouter = &DefaultServiceRouter{}
//	var router ServiceRouter = &MockServiceRouter{}
//
// Limitations:
//
//	None
//
// Assumptions:
//   - Interface is stable
func TestServiceRouter_InterfaceCompliance(t *testing.T) {
	clearRoutingEnvVars(t)

	// Test that both implementations can be used through the interface
	implementations := []ServiceRouter{
		NewDefaultServiceRouter("standalone"),
		&MockServiceRouter{
			OrchestrationURL: "http://mock:8000",
			TradingURL:       "http://mock:9000",
			InfluxDBURL:      "http://mock:8086",
		},
	}

	for i, router := range implementations {
		// Just verify we can call all interface methods without panic
		_ = router.GetOrchestrationURL()
		_ = router.GetTradingURL()
		_ = router.GetInfluxDBURL()

		t.Logf("Implementation %d passed interface compliance", i)
	}
}

// TestServiceRouter_PolymorphicUsage verifies that code can use the interface
// without knowing the underlying implementation.
//
// Description:
//
//	This test simulates how production code would use ServiceRouter
//	polymorphically, accepting any implementation.
//
// Inputs:
//
//	ServiceRouter interface
//
// Outputs:
//
//	URLs from the implementation
//
// Example:
//
//	func DoWork(router ServiceRouter) string {
//	    return router.GetOrchestrationURL()
//	}
//
// Limitations:
//
//	None
//
// Assumptions:
//   - All implementations behave correctly
func TestServiceRouter_PolymorphicUsage(t *testing.T) {
	clearRoutingEnvVars(t)

	// Helper function that accepts the interface
	getURLs := func(router ServiceRouter) (string, string, string) {
		return router.GetOrchestrationURL(),
			router.GetTradingURL(),
			router.GetInfluxDBURL()
	}

	t.Run("DefaultServiceRouter", func(t *testing.T) {
		router := NewDefaultServiceRouter("standalone")
		orch, trading, influx := getURLs(router)

		if orch != "http://localhost:12210" {
			t.Errorf("Orchestration URL = %q, want default", orch)
		}
		if trading != "http://localhost:12132" {
			t.Errorf("Trading URL = %q, want default", trading)
		}
		if influx != "http://localhost:12130" {
			t.Errorf("InfluxDB URL = %q, want default", influx)
		}
	})

	t.Run("MockServiceRouter", func(t *testing.T) {
		router := &MockServiceRouter{
			OrchestrationURL: "http://test-orch:8000",
			TradingURL:       "http://test-trading:9000",
			InfluxDBURL:      "http://test-influx:8086",
		}
		orch, trading, influx := getURLs(router)

		if orch != "http://test-orch:8000" {
			t.Errorf("Orchestration URL = %q, want mock value", orch)
		}
		if trading != "http://test-trading:9000" {
			t.Errorf("Trading URL = %q, want mock value", trading)
		}
		if influx != "http://test-influx:8086" {
			t.Errorf("InfluxDB URL = %q, want mock value", influx)
		}
	})
}

// =============================================================================
// CONSTRUCTOR TESTS
// =============================================================================

// TestNewDefaultServiceRouter_PreservesDeploymentMode tests that the
// constructor correctly stores the deployment mode.
//
// Description:
//
//	NewDefaultServiceRouter should store the provided deployment mode
//	for later use in URL resolution.
//
// Inputs:
//
//	deploymentMode: string
//
// Outputs:
//
//	*DefaultServiceRouter with mode set
//
// Example:
//
//	router := NewDefaultServiceRouter("standalone")
//	// router.deploymentMode == "standalone"
//
// Limitations:
//   - deploymentMode field is not exported (tested indirectly)
//
// Assumptions:
//   - Valid modes are "standalone" and "distributed"
func TestNewDefaultServiceRouter_PreservesDeploymentMode(t *testing.T) {
	clearRoutingEnvVars(t)

	testCases := []struct {
		name            string
		mode            string
		expectedOrchURL string
	}{
		{
			name:            "standalone mode",
			mode:            "standalone",
			expectedOrchURL: "http://localhost:12210",
		},
		{
			name:            "distributed mode",
			mode:            "distributed",
			expectedOrchURL: "http://sapheneia-orchestration:8000",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := NewDefaultServiceRouter(tc.mode)
			actual := router.GetOrchestrationURL()

			if actual != tc.expectedOrchURL {
				t.Errorf("NewDefaultServiceRouter(%q).GetOrchestrationURL() = %q, want %q",
					tc.mode, actual, tc.expectedOrchURL)
			}
		})
	}
}
