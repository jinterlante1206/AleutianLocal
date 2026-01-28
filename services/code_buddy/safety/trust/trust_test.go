// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package trust

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/safety"
)

// =============================================================================
// ZonePatterns Tests
// =============================================================================

func TestDefaultZonePatterns(t *testing.T) {
	t.Run("creates non-nil patterns", func(t *testing.T) {
		p := DefaultZonePatterns()
		if p == nil {
			t.Fatal("DefaultZonePatterns returned nil")
		}
		if p.PathPatterns == nil {
			t.Error("PathPatterns is nil")
		}
		if p.FunctionPatterns == nil {
			t.Error("FunctionPatterns is nil")
		}
		if p.ReceiverPatterns == nil {
			t.Error("ReceiverPatterns is nil")
		}
		if p.PackagePatterns == nil {
			t.Error("PackagePatterns is nil")
		}
	})

	t.Run("has patterns for all trust levels", func(t *testing.T) {
		p := DefaultZonePatterns()
		levels := []safety.TrustLevel{
			safety.TrustExternal,
			safety.TrustValidation,
			safety.TrustInternal,
			safety.TrustPrivileged,
		}

		for _, level := range levels {
			if len(p.PathPatterns[level]) == 0 {
				t.Errorf("no path patterns for level %v", level)
			}
			if len(p.FunctionPatterns[level]) == 0 {
				t.Errorf("no function patterns for level %v", level)
			}
		}
	})
}

func TestZonePatterns_MatchPath(t *testing.T) {
	p := DefaultZonePatterns()

	tests := []struct {
		name     string
		path     string
		expected safety.TrustLevel
		matched  bool
	}{
		{"handlers folder", "src/handlers/user.go", safety.TrustExternal, true},
		{"api folder", "pkg/api/v1/routes.go", safety.TrustExternal, true},
		{"middleware folder", "src/middleware/auth.go", safety.TrustValidation, true},
		{"validators folder", "pkg/validators/input.go", safety.TrustValidation, true},
		{"services folder", "src/services/user.go", safety.TrustInternal, true},
		{"domain folder", "pkg/domain/models.go", safety.TrustInternal, true},
		{"admin folder", "internal/admin/dashboard.go", safety.TrustPrivileged, true},
		{"unmatched path", "random/path/file.go", safety.TrustInternal, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, matched := p.MatchPath(tt.path)
			if matched != tt.matched {
				t.Errorf("MatchPath(%q) matched = %v, want %v", tt.path, matched, tt.matched)
			}
			if matched && level != tt.expected {
				t.Errorf("MatchPath(%q) level = %v, want %v", tt.path, level, tt.expected)
			}
		})
	}
}

func TestZonePatterns_MatchFunction(t *testing.T) {
	p := DefaultZonePatterns()

	tests := []struct {
		name     string
		funcName string
		expected safety.TrustLevel
		matched  bool
	}{
		{"HandleRequest", "HandleRequest", safety.TrustExternal, true},
		{"UserHandler", "UserHandler", safety.TrustExternal, true},
		{"ValidateInput", "ValidateInput", safety.TrustValidation, true},
		{"SanitizeHTML", "SanitizeHTML", safety.TrustValidation, true},
		{"CreateUser", "CreateUser", safety.TrustInternal, true},
		{"GetByID", "GetByID", safety.TrustInternal, true},
		{"AdminDelete", "AdminDelete", safety.TrustPrivileged, true},
		{"SystemConfig", "SystemConfig", safety.TrustPrivileged, true},
		{"randomFunc", "randomFunc", safety.TrustInternal, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, matched := p.MatchFunction(tt.funcName)
			if matched != tt.matched {
				t.Errorf("MatchFunction(%q) matched = %v, want %v", tt.funcName, matched, tt.matched)
			}
			if matched && level != tt.expected {
				t.Errorf("MatchFunction(%q) level = %v, want %v", tt.funcName, level, tt.expected)
			}
		})
	}
}

func TestZonePatterns_MatchReceiver(t *testing.T) {
	p := DefaultZonePatterns()

	tests := []struct {
		name     string
		receiver string
		expected safety.TrustLevel
		matched  bool
	}{
		{"UserHandler", "UserHandler", safety.TrustExternal, true},
		{"APIController", "APIController", safety.TrustExternal, true},
		{"InputValidator", "InputValidator", safety.TrustValidation, true},
		{"AuthMiddleware", "AuthMiddleware", safety.TrustValidation, true},
		{"UserService", "UserService", safety.TrustInternal, true},
		{"DataRepository", "DataRepository", safety.TrustInternal, true},
		{"AdminPanel", "AdminPanel", safety.TrustPrivileged, true},
		{"RandomType", "RandomType", safety.TrustInternal, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, matched := p.MatchReceiver(tt.receiver)
			if matched != tt.matched {
				t.Errorf("MatchReceiver(%q) matched = %v, want %v", tt.receiver, matched, tt.matched)
			}
			if matched && level != tt.expected {
				t.Errorf("MatchReceiver(%q) level = %v, want %v", tt.receiver, level, tt.expected)
			}
		})
	}
}

// =============================================================================
// CrossingRequirements Tests
// =============================================================================

func TestDefaultCrossingRequirements(t *testing.T) {
	t.Run("creates non-nil requirements", func(t *testing.T) {
		r := DefaultCrossingRequirements()
		if r == nil {
			t.Fatal("DefaultCrossingRequirements returned nil")
		}
		if r.Requirements == nil {
			t.Error("Requirements is nil")
		}
		if r.CWEMapping == nil {
			t.Error("CWEMapping is nil")
		}
		if r.SeverityMapping == nil {
			t.Error("SeverityMapping is nil")
		}
	})
}

func TestCrossingRequirements_GetRequirements(t *testing.T) {
	r := DefaultCrossingRequirements()

	tests := []struct {
		name     string
		from     safety.TrustLevel
		to       safety.TrustLevel
		hasReqs  bool
		minCount int
	}{
		{"external to internal", safety.TrustExternal, safety.TrustInternal, true, 1},
		{"external to privileged", safety.TrustExternal, safety.TrustPrivileged, true, 2},
		{"validation to privileged", safety.TrustValidation, safety.TrustPrivileged, true, 1},
		{"internal to privileged", safety.TrustInternal, safety.TrustPrivileged, true, 1},
		{"same level", safety.TrustInternal, safety.TrustInternal, false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqs := r.GetRequirements(tt.from, tt.to)
			if tt.hasReqs && len(reqs) < tt.minCount {
				t.Errorf("GetRequirements(%v, %v) got %d reqs, want at least %d",
					tt.from, tt.to, len(reqs), tt.minCount)
			}
			if !tt.hasReqs && len(reqs) > 0 {
				t.Errorf("GetRequirements(%v, %v) got %d reqs, want 0",
					tt.from, tt.to, len(reqs))
			}
		})
	}
}

func TestCrossingRequirements_GetCWE(t *testing.T) {
	r := DefaultCrossingRequirements()

	// External to internal should be CWE-20 (Input Validation)
	cwe := r.GetCWE(safety.TrustExternal, safety.TrustInternal)
	if cwe != "CWE-20" {
		t.Errorf("GetCWE(External, Internal) = %q, want CWE-20", cwe)
	}

	// External to privileged should be CWE-284 (Access Control)
	cwe = r.GetCWE(safety.TrustExternal, safety.TrustPrivileged)
	if cwe != "CWE-284" {
		t.Errorf("GetCWE(External, Privileged) = %q, want CWE-284", cwe)
	}
}

func TestCrossingRequirements_GetSeverity(t *testing.T) {
	r := DefaultCrossingRequirements()

	// External to privileged should be critical
	sev := r.GetSeverity(safety.TrustExternal, safety.TrustPrivileged)
	if sev != safety.SeverityCritical {
		t.Errorf("GetSeverity(External, Privileged) = %v, want CRITICAL", sev)
	}

	// External to internal should be medium
	sev = r.GetSeverity(safety.TrustExternal, safety.TrustInternal)
	if sev != safety.SeverityMedium {
		t.Errorf("GetSeverity(External, Internal) = %v, want MEDIUM", sev)
	}
}

func TestCrossingRequirements_RequiresValidation(t *testing.T) {
	r := DefaultCrossingRequirements()

	tests := []struct {
		from     safety.TrustLevel
		to       safety.TrustLevel
		required bool
	}{
		{safety.TrustExternal, safety.TrustInternal, true},
		{safety.TrustExternal, safety.TrustPrivileged, true},
		{safety.TrustInternal, safety.TrustPrivileged, true},
		{safety.TrustInternal, safety.TrustExternal, false},
		{safety.TrustPrivileged, safety.TrustInternal, false},
		{safety.TrustInternal, safety.TrustInternal, false},
	}

	for _, tt := range tests {
		name := TrustLevelName(tt.from) + "_to_" + TrustLevelName(tt.to)
		t.Run(name, func(t *testing.T) {
			got := r.RequiresValidation(tt.from, tt.to)
			if got != tt.required {
				t.Errorf("RequiresValidation(%v, %v) = %v, want %v",
					tt.from, tt.to, got, tt.required)
			}
		})
	}
}

// =============================================================================
// Helper Functions Tests
// =============================================================================

func TestTrustLevelName(t *testing.T) {
	tests := []struct {
		level    safety.TrustLevel
		expected string
	}{
		{safety.TrustExternal, "Untrusted"},
		{safety.TrustValidation, "Boundary"},
		{safety.TrustInternal, "Internal"},
		{safety.TrustPrivileged, "Privileged"},
		{safety.TrustLevel(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := TrustLevelName(tt.level)
			if got != tt.expected {
				t.Errorf("TrustLevelName(%v) = %q, want %q", tt.level, got, tt.expected)
			}
		})
	}
}

func TestGenerateZoneID(t *testing.T) {
	tests := []struct {
		level    safety.TrustLevel
		name     string
		expected string
	}{
		{safety.TrustExternal, "handlers", "untrusted_handlers"},
		{safety.TrustInternal, "services", "internal_services"},
		{safety.TrustPrivileged, "admin/users", "privileged_admin_users"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := GenerateZoneID(tt.level, tt.name)
			if got != tt.expected {
				t.Errorf("GenerateZoneID(%v, %q) = %q, want %q",
					tt.level, tt.name, got, tt.expected)
			}
		})
	}
}

func TestGenerateCrossingID(t *testing.T) {
	id := GenerateCrossingID("zone_a", "zone_b", "func.Name")
	expected := "zone_a_to_zone_b_at_func_Name"
	if id != expected {
		t.Errorf("GenerateCrossingID() = %q, want %q", id, expected)
	}
}

// =============================================================================
// ZoneDetector Tests
// =============================================================================

func TestNewZoneDetector(t *testing.T) {
	t.Run("creates with default patterns", func(t *testing.T) {
		zd := NewZoneDetector()
		if zd == nil {
			t.Fatal("NewZoneDetector returned nil")
		}
		if zd.patterns == nil {
			t.Error("patterns is nil")
		}
	})

	t.Run("creates with custom patterns", func(t *testing.T) {
		customPatterns := &ZonePatterns{}
		zd := NewZoneDetectorWithPatterns(customPatterns)
		if zd == nil {
			t.Fatal("NewZoneDetectorWithPatterns returned nil")
		}
		if zd.patterns != customPatterns {
			t.Error("patterns not set correctly")
		}
	})
}

func TestZoneDetector_DetectZones(t *testing.T) {
	g := graph.NewGraph("/test")

	// Add handler node (external/untrusted)
	g.AddNode(&ast.Symbol{
		ID:        "pkg.HandleRequest",
		Name:      "HandleRequest",
		Package:   "myapp/handlers",
		FilePath:  "handlers/user.go",
		StartLine: 10,
		Kind:      ast.SymbolKindFunction,
	})

	// Add service node (internal)
	g.AddNode(&ast.Symbol{
		ID:        "pkg.CreateUser",
		Name:      "CreateUser",
		Package:   "myapp/services",
		FilePath:  "services/user.go",
		StartLine: 20,
		Kind:      ast.SymbolKindFunction,
	})

	// Add edge from handler to service
	g.AddEdge("pkg.HandleRequest", "pkg.CreateUser", graph.EdgeTypeCalls, ast.Location{})
	g.Freeze()

	zd := NewZoneDetector()
	zones := zd.DetectZones(g, "")

	if len(zones) == 0 {
		t.Fatal("DetectZones returned no zones")
	}

	// Check that we have zones at different levels
	foundExternal := false
	foundInternal := false
	for _, z := range zones {
		if z.Level == safety.TrustExternal {
			foundExternal = true
		}
		if z.Level == safety.TrustInternal {
			foundInternal = true
		}
	}

	if !foundExternal {
		t.Error("expected to find external zone for handler")
	}
	if !foundInternal {
		t.Error("expected to find internal zone for service")
	}
}

func TestZoneDetector_FindZoneForNode(t *testing.T) {
	g := graph.NewGraph("/test")

	g.AddNode(&ast.Symbol{
		ID:        "pkg.HandleRequest",
		Name:      "HandleRequest",
		Package:   "myapp/handlers",
		FilePath:  "handlers/user.go",
		StartLine: 10,
		Kind:      ast.SymbolKindFunction,
	})
	g.Freeze()

	zd := NewZoneDetector()
	zones := zd.DetectZones(g, "")

	node, _ := g.GetNode("pkg.HandleRequest")
	zone := zd.FindZoneForNode(node, zones)

	if zone == nil {
		t.Fatal("FindZoneForNode returned nil")
	}
	if zone.Level != safety.TrustExternal {
		t.Errorf("zone level = %v, want TrustExternal", zone.Level)
	}
}

// =============================================================================
// CrossingDetector Tests
// =============================================================================

func TestNewCrossingDetector(t *testing.T) {
	cd := NewCrossingDetector()
	if cd == nil {
		t.Fatal("NewCrossingDetector returned nil")
	}
	if cd.zoneDetector == nil {
		t.Error("zoneDetector is nil")
	}
	if cd.requirements == nil {
		t.Error("requirements is nil")
	}
}

func TestCrossingDetector_DetectCrossings(t *testing.T) {
	g := graph.NewGraph("/test")

	// Handler (external)
	g.AddNode(&ast.Symbol{
		ID:        "handler.Request",
		Name:      "HandleRequest",
		Package:   "myapp/handlers",
		FilePath:  "handlers/api.go",
		StartLine: 10,
		Kind:      ast.SymbolKindFunction,
	})

	// Service (internal)
	g.AddNode(&ast.Symbol{
		ID:        "service.Create",
		Name:      "CreateUser",
		Package:   "myapp/services",
		FilePath:  "services/user.go",
		StartLine: 20,
		Kind:      ast.SymbolKindFunction,
	})

	// Edge crossing from external to internal
	g.AddEdge("handler.Request", "service.Create", graph.EdgeTypeCalls, ast.Location{})
	g.Freeze()

	zd := NewZoneDetector()
	zones := zd.DetectZones(g, "")

	cd := NewCrossingDetector()
	ctx := context.Background()

	crossings, err := cd.DetectCrossings(ctx, g, zones, "")
	if err != nil {
		t.Fatalf("DetectCrossings error: %v", err)
	}

	if len(crossings) == 0 {
		t.Fatal("expected to detect crossings")
	}
}

func TestCrossingDetector_FindViolations(t *testing.T) {
	// Create a crossing without validation
	fromZone := &safety.TrustZone{
		ID:    "untrusted_handlers",
		Name:  "handlers",
		Level: safety.TrustExternal,
	}
	toZone := &safety.TrustZone{
		ID:    "internal_services",
		Name:  "services",
		Level: safety.TrustInternal,
	}

	crossings := []safety.BoundaryCrossing{
		{
			ID:            "test_crossing",
			From:          fromZone,
			To:            toZone,
			CrossingAt:    "handlers/api.go:10",
			HasValidation: false,
		},
	}

	cd := NewCrossingDetector()
	violations := cd.FindViolations(crossings)

	if len(violations) == 0 {
		t.Fatal("expected to find violations for unvalidated crossing")
	}

	v := violations[0]
	if v.Severity != safety.SeverityMedium {
		t.Errorf("violation severity = %v, want MEDIUM", v.Severity)
	}
	if v.CWE != "CWE-20" {
		t.Errorf("violation CWE = %q, want CWE-20", v.CWE)
	}
}

func TestCrossingDetector_NoViolationWhenValidated(t *testing.T) {
	fromZone := &safety.TrustZone{
		ID:    "untrusted_handlers",
		Name:  "handlers",
		Level: safety.TrustExternal,
	}
	toZone := &safety.TrustZone{
		ID:    "internal_services",
		Name:  "services",
		Level: safety.TrustInternal,
	}

	crossings := []safety.BoundaryCrossing{
		{
			ID:            "validated_crossing",
			From:          fromZone,
			To:            toZone,
			CrossingAt:    "handlers/api.go:10",
			HasValidation: true,
			ValidationFn:  "ValidateInput",
		},
	}

	cd := NewCrossingDetector()
	violations := cd.FindViolations(crossings)

	if len(violations) != 0 {
		t.Errorf("expected no violations for validated crossing, got %d", len(violations))
	}
}

// =============================================================================
// Analyzer Tests
// =============================================================================

func TestNewAnalyzer(t *testing.T) {
	g := graph.NewGraph("/test")
	g.Freeze()
	idx := index.NewSymbolIndex()

	a := NewAnalyzer(g, idx)
	if a == nil {
		t.Fatal("NewAnalyzer returned nil")
	}
	if a.graph != g {
		t.Error("graph not set")
	}
	if a.zoneDetector == nil {
		t.Error("zoneDetector is nil")
	}
	if a.crossingDetector == nil {
		t.Error("crossingDetector is nil")
	}
}

func TestAnalyzer_AnalyzeTrustBoundary(t *testing.T) {
	t.Run("returns error for nil context", func(t *testing.T) {
		g := graph.NewGraph("/test")
		g.Freeze()
		idx := index.NewSymbolIndex()
		a := NewAnalyzer(g, idx)

		_, err := a.AnalyzeTrustBoundary(nil, "")
		if err != safety.ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("returns error for unfrozen graph", func(t *testing.T) {
		g := graph.NewGraph("/test")
		// Note: not frozen
		idx := index.NewSymbolIndex()
		a := NewAnalyzer(g, idx)

		ctx := context.Background()
		_, err := a.AnalyzeTrustBoundary(ctx, "")
		if err != safety.ErrGraphNotReady {
			t.Errorf("expected ErrGraphNotReady, got %v", err)
		}
	})

	t.Run("returns result for valid input", func(t *testing.T) {
		g := graph.NewGraph("/test")
		g.AddNode(&ast.Symbol{
			ID:        "test.Handler",
			Name:      "HandleRequest",
			Package:   "myapp/handlers",
			FilePath:  "handlers/api.go",
			StartLine: 10,
			Kind:      ast.SymbolKindFunction,
		})
		g.Freeze()
		idx := index.NewSymbolIndex()

		a := NewAnalyzer(g, idx)
		ctx := context.Background()

		result, err := a.AnalyzeTrustBoundary(ctx, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("result is nil")
		}
		if result.Duration == 0 {
			t.Error("duration not set")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		g := graph.NewGraph("/test")
		g.Freeze()
		idx := index.NewSymbolIndex()
		a := NewAnalyzer(g, idx)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := a.AnalyzeTrustBoundary(ctx, "")
		if err != safety.ErrContextCanceled {
			t.Errorf("expected ErrContextCanceled, got %v", err)
		}
	})
}

func TestAnalyzer_GeneratesRecommendations(t *testing.T) {
	g := graph.NewGraph("/test")

	// Add handler with crossing to privileged zone
	g.AddNode(&ast.Symbol{
		ID:        "handler.Admin",
		Name:      "HandleAdminRequest",
		Package:   "myapp/handlers",
		FilePath:  "handlers/admin.go",
		StartLine: 10,
		Kind:      ast.SymbolKindFunction,
	})

	g.AddNode(&ast.Symbol{
		ID:        "admin.Delete",
		Name:      "AdminDeleteUser",
		Package:   "myapp/admin",
		FilePath:  "admin/users.go",
		StartLine: 20,
		Kind:      ast.SymbolKindFunction,
	})

	g.AddEdge("handler.Admin", "admin.Delete", graph.EdgeTypeCalls, ast.Location{})
	g.Freeze()

	idx := index.NewSymbolIndex()
	a := NewAnalyzer(g, idx)

	ctx := context.Background()
	result, err := a.AnalyzeTrustBoundary(ctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Recommendations) == 0 {
		t.Error("expected recommendations to be generated")
	}
}

// =============================================================================
// Verifier Tests
// =============================================================================

func TestNewVerifier(t *testing.T) {
	v := NewVerifier(nil)
	if v == nil {
		t.Fatal("NewVerifier returned nil")
	}
	if v.issueStore == nil {
		t.Error("issueStore is nil")
	}
}

func TestVerifier_RegisterIssue(t *testing.T) {
	v := NewVerifier(nil)

	issue := &safety.SecurityIssue{
		ID:   "ISSUE-1",
		Type: "sql_injection",
	}
	v.RegisterIssue(issue)

	got, found := v.GetIssueStore().Get("ISSUE-1")
	if !found {
		t.Fatal("issue not found after registration")
	}
	if got.Type != "sql_injection" {
		t.Errorf("issue type = %q, want sql_injection", got.Type)
	}
}

func TestVerifier_VerifyRemediation(t *testing.T) {
	t.Run("returns error for nil context", func(t *testing.T) {
		v := NewVerifier(nil)
		_, err := v.VerifyRemediation(nil, "ISSUE-1", "code")
		if err != safety.ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("returns error for empty issueID", func(t *testing.T) {
		v := NewVerifier(nil)
		ctx := context.Background()
		_, err := v.VerifyRemediation(ctx, "", "code")
		if err != safety.ErrInvalidInput {
			t.Errorf("expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("returns error for unknown issue", func(t *testing.T) {
		v := NewVerifier(nil)
		ctx := context.Background()
		_, err := v.VerifyRemediation(ctx, "UNKNOWN", "code")
		if err != safety.ErrNoVulnerabilityFound {
			t.Errorf("expected ErrNoVulnerabilityFound, got %v", err)
		}
	})

	t.Run("verifies SQL injection fix", func(t *testing.T) {
		v := NewVerifier(nil)
		v.RegisterIssue(&safety.SecurityIssue{
			ID:   "SQL-1",
			Type: "sql_injection",
		})

		ctx := context.Background()
		fixedCode := `db.Query("SELECT * FROM users WHERE id = ?", userID)`

		result, err := v.VerifyRemediation(ctx, "SQL-1", fixedCode)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsFixed {
			t.Error("expected IsFixed = true for parameterized query")
		}
	})

	t.Run("detects unfixed SQL injection", func(t *testing.T) {
		v := NewVerifier(nil)
		v.RegisterIssue(&safety.SecurityIssue{
			ID:   "SQL-2",
			Type: "sql_injection",
		})

		ctx := context.Background()
		unfixedCode := `db.Query("SELECT * FROM users WHERE id = " + userID)`

		result, err := v.VerifyRemediation(ctx, "SQL-2", unfixedCode)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsFixed {
			t.Error("expected IsFixed = false for concatenated query")
		}
	})

	t.Run("verifies command injection fix", func(t *testing.T) {
		v := NewVerifier(nil)
		v.RegisterIssue(&safety.SecurityIssue{
			ID:   "CMD-1",
			Type: "command_injection",
		})

		ctx := context.Background()
		fixedCode := `exec.Command("ls", []string{"-la", dir})`

		result, err := v.VerifyRemediation(ctx, "CMD-1", fixedCode)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsFixed {
			t.Error("expected IsFixed = true for arg list command")
		}
	})

	t.Run("detects unfixed command injection", func(t *testing.T) {
		v := NewVerifier(nil)
		v.RegisterIssue(&safety.SecurityIssue{
			ID:   "CMD-2",
			Type: "command_injection",
		})

		ctx := context.Background()
		unfixedCode := `subprocess.run(cmd, shell=True)`

		result, err := v.VerifyRemediation(ctx, "CMD-2", unfixedCode)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.IsFixed {
			t.Error("expected IsFixed = false for shell=True")
		}
	})

	t.Run("verifies XSS fix", func(t *testing.T) {
		v := NewVerifier(nil)
		v.RegisterIssue(&safety.SecurityIssue{
			ID:   "XSS-1",
			Type: "xss",
		})

		ctx := context.Background()
		fixedCode := `html.EscapeString(userInput)`

		result, err := v.VerifyRemediation(ctx, "XSS-1", fixedCode)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsFixed {
			t.Error("expected IsFixed = true for escaped output")
		}
	})

	t.Run("verifies path traversal fix", func(t *testing.T) {
		v := NewVerifier(nil)
		v.RegisterIssue(&safety.SecurityIssue{
			ID:   "PATH-1",
			Type: "path_traversal",
		})

		ctx := context.Background()
		fixedCode := `filepath.Base(filename)`

		result, err := v.VerifyRemediation(ctx, "PATH-1", fixedCode)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsFixed {
			t.Error("expected IsFixed = true for validated path")
		}
	})

	t.Run("verifies boundary fix", func(t *testing.T) {
		v := NewVerifier(nil)
		v.RegisterIssue(&safety.SecurityIssue{
			ID:   "BOUNDARY-1",
			Type: "trust_boundary_violation",
		})

		ctx := context.Background()
		fixedCode := `
			func HandleRequest(w http.ResponseWriter, r *http.Request) {
				if err := ValidateInput(input); err != nil {
					return
				}
				// proceed with validated input
			}
		`

		result, err := v.VerifyRemediation(ctx, "BOUNDARY-1", fixedCode)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.IsFixed {
			t.Error("expected IsFixed = true for validated boundary")
		}
	})
}

func TestVerifier_ContextCancellation(t *testing.T) {
	v := NewVerifier(nil)
	v.RegisterIssue(&safety.SecurityIssue{
		ID:   "TEST-1",
		Type: "sql_injection",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := v.VerifyRemediation(ctx, "TEST-1", "code")
	if err != safety.ErrContextCanceled {
		t.Errorf("expected ErrContextCanceled, got %v", err)
	}
}

// =============================================================================
// IssueStore Tests
// =============================================================================

func TestIssueStore(t *testing.T) {
	t.Run("register and get", func(t *testing.T) {
		store := NewIssueStore()
		issue := &safety.SecurityIssue{ID: "TEST-1", Type: "test"}
		store.Register(issue)

		got, found := store.Get("TEST-1")
		if !found {
			t.Fatal("issue not found")
		}
		if got.Type != "test" {
			t.Errorf("type = %q, want test", got.Type)
		}
	})

	t.Run("remove", func(t *testing.T) {
		store := NewIssueStore()
		store.Register(&safety.SecurityIssue{ID: "TEST-1"})
		store.Remove("TEST-1")

		_, found := store.Get("TEST-1")
		if found {
			t.Error("issue should not be found after removal")
		}
	})

	t.Run("ignores nil issue", func(t *testing.T) {
		store := NewIssueStore()
		store.Register(nil) // Should not panic
	})

	t.Run("ignores empty ID", func(t *testing.T) {
		store := NewIssueStore()
		store.Register(&safety.SecurityIssue{ID: ""})
		_, found := store.Get("")
		if found {
			t.Error("should not store issue with empty ID")
		}
	})
}

// =============================================================================
// Concurrency Tests
// =============================================================================

func TestZoneDetector_Concurrent(t *testing.T) {
	g := graph.NewGraph("/test")

	// Add many nodes
	for i := 0; i < 100; i++ {
		g.AddNode(&ast.Symbol{
			ID:        fmt.Sprintf("node.%d", i),
			Name:      fmt.Sprintf("Func%d", i),
			Package:   "myapp/pkg",
			FilePath:  "pkg/file.go",
			StartLine: i,
			Kind:      ast.SymbolKindFunction,
		})
	}
	g.Freeze()

	zd := NewZoneDetector()

	// Run concurrent zone detection
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			zones := zd.DetectZones(g, "")
			if zones == nil {
				t.Error("zones is nil")
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for concurrent zone detection")
		}
	}
}

func TestIssueStore_Concurrent(t *testing.T) {
	store := NewIssueStore()

	// Concurrent writes
	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func(id int) {
			store.Register(&safety.SecurityIssue{
				ID:   fmt.Sprintf("ISSUE-%d", id),
				Type: "test",
			})
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		go func(id int) {
			store.Get(fmt.Sprintf("ISSUE-%d", id))
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for concurrent reads")
		}
	}
}
