// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package safety

import (
	"context"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
)

// InputTracer traces untrusted input through code to find vulnerabilities.
//
// Description:
//
//	InputTracer performs taint analysis, tracking where untrusted data
//	flows and whether it's sanitized before reaching sensitive sinks.
//	This is the core interface for CB-23's trust flow analysis.
//
// Thread Safety:
//
//	Implementations must be safe for concurrent use.
type InputTracer interface {
	// TraceUserInput traces data flow from an input source.
	//
	// Description:
	//   Follows data from the source through function calls, tracking
	//   taint state at each step. Reports vulnerabilities when untrusted
	//   data reaches sensitive sinks without sanitization.
	//
	// Inputs:
	//   ctx - Context for cancellation and timeout.
	//   sourceID - The symbol ID of the input source to trace from.
	//   opts - Optional configuration (max depth, sink filters, etc.).
	//
	// Outputs:
	//   *InputTrace - The trace result with path, sinks, and vulnerabilities.
	//   error - Non-nil if source not found or operation canceled.
	TraceUserInput(ctx context.Context, sourceID string, opts ...TraceOption) (*InputTrace, error)
}

// SecurityScanner scans code for security vulnerabilities.
//
// Description:
//
//	SecurityScanner performs SAST-lite scanning, detecting common
//	vulnerability patterns like SQL injection, XSS, command injection,
//	and more. It integrates with trust flow analysis for higher confidence.
//
// Thread Safety:
//
//	Implementations must be safe for concurrent use.
type SecurityScanner interface {
	// ScanForSecurityIssues scans a scope for vulnerabilities.
	//
	// Description:
	//   Scans the specified scope (package, file, or symbol) for security
	//   issues using pattern matching and trust flow analysis.
	//
	// Inputs:
	//   ctx - Context for cancellation and timeout.
	//   scope - The scope to scan (package path, file path, or symbol ID).
	//   opts - Optional configuration (min severity, min confidence, etc.).
	//
	// Outputs:
	//   *ScanResult - The scan result with issues found.
	//   error - Non-nil if scope not found or operation canceled.
	ScanForSecurityIssues(ctx context.Context, scope string, opts ...ScanOption) (*ScanResult, error)
}

// ErrorAuditor audits error handling for security issues.
//
// Description:
//
//	ErrorAuditor detects fail-open patterns, information leaks, and
//	improper error handling that could lead to security bypasses.
//
// Thread Safety:
//
//	Implementations must be safe for concurrent use.
type ErrorAuditor interface {
	// AuditErrorHandling audits error handling in a scope.
	//
	// Description:
	//   Analyzes error handling patterns to detect fail-open conditions,
	//   information leaks (stack traces, internal paths), and swallowed errors.
	//
	// Inputs:
	//   ctx - Context for cancellation and timeout.
	//   scope - The scope to audit (package path or file path).
	//   opts - Optional configuration (focus area, etc.).
	//
	// Outputs:
	//   *ErrorAudit - The audit result with issues found.
	//   error - Non-nil if scope not found or operation canceled.
	AuditErrorHandling(ctx context.Context, scope string, opts ...AuditOption) (*ErrorAudit, error)
}

// SecretFinder finds hardcoded secrets in code.
//
// Description:
//
//	SecretFinder detects API keys, passwords, private keys, and other
//	credentials that are hardcoded in source code.
//
// Thread Safety:
//
//	Implementations must be safe for concurrent use.
type SecretFinder interface {
	// FindHardcodedSecrets finds secrets in a scope.
	//
	// Description:
	//   Scans for common secret patterns: API keys, passwords, private
	//   keys, connection strings, and cloud credentials.
	//
	// Inputs:
	//   ctx - Context for cancellation and timeout.
	//   scope - The scope to scan (package path or file path).
	//
	// Outputs:
	//   []HardcodedSecret - All secrets found (masked in output).
	//   error - Non-nil if scope not found or operation canceled.
	FindHardcodedSecrets(ctx context.Context, scope string) ([]HardcodedSecret, error)
}

// AuthChecker checks authentication and authorization enforcement.
//
// Description:
//
//	AuthChecker detects endpoints missing authentication or authorization
//	middleware. It supports multiple web frameworks (Gin, Echo, FastAPI, etc.).
//
// Thread Safety:
//
//	Implementations must be safe for concurrent use.
type AuthChecker interface {
	// CheckAuthEnforcement checks auth on endpoints.
	//
	// Description:
	//   Analyzes HTTP handlers and routes to verify they have proper
	//   authentication and authorization middleware.
	//
	// Inputs:
	//   ctx - Context for cancellation and timeout.
	//   scope - The scope to check (package path).
	//   opts - Optional configuration (framework hint, check type).
	//
	// Outputs:
	//   *AuthCheck - The check result with endpoints and issues.
	//   error - Non-nil if scope not found or operation canceled.
	CheckAuthEnforcement(ctx context.Context, scope string, opts ...AuthCheckOption) (*AuthCheck, error)
}

// TrustBoundaryAnalyzer analyzes trust boundaries and zone crossings.
//
// Description:
//
//	TrustBoundaryAnalyzer implements a formal trust zone model, detecting
//	where untrusted data crosses into trusted zones without validation.
//	This is Aleutian's unique security differentiator.
//
// Thread Safety:
//
//	Implementations must be safe for concurrent use.
type TrustBoundaryAnalyzer interface {
	// AnalyzeTrustBoundary analyzes trust zones and crossings.
	//
	// Description:
	//   Automatically detects trust zones in the codebase and identifies
	//   boundary crossings where validation may be missing.
	//
	// Inputs:
	//   ctx - Context for cancellation and timeout.
	//   scope - The scope to analyze (package path).
	//   opts - Optional configuration (show zones, etc.).
	//
	// Outputs:
	//   *TrustBoundary - The analysis result with zones, crossings, violations.
	//   error - Non-nil if scope not found or operation canceled.
	AnalyzeTrustBoundary(ctx context.Context, scope string, opts ...BoundaryOption) (*TrustBoundary, error)
}

// RemediationVerifier verifies that security fixes work.
//
// Description:
//
//	RemediationVerifier re-runs analysis on patched code to verify that
//	a security fix actually resolves the issue and doesn't introduce new ones.
//
// Thread Safety:
//
//	Implementations must be safe for concurrent use.
type RemediationVerifier interface {
	// VerifyRemediation verifies a security fix.
	//
	// Description:
	//   Re-runs the specific check that found the issue on the new code
	//   to verify the vulnerability is fixed.
	//
	// Inputs:
	//   ctx - Context for cancellation and timeout.
	//   issueID - The ID of the security issue being fixed.
	//   newCode - The patched code to verify.
	//
	// Outputs:
	//   *VerificationResult - Whether the fix worked.
	//   error - Non-nil if issue not found or operation canceled.
	VerifyRemediation(ctx context.Context, issueID string, newCode string) (*VerificationResult, error)
}

// FrameworkAnalyzer understands framework-specific middleware patterns.
//
// Description:
//
//	FrameworkAnalyzer enables framework-aware security analysis for
//	web frameworks like Gin, Echo, Chi, FastAPI, Flask, NestJS, etc.
//
// Thread Safety:
//
//	Implementations must be safe for concurrent use.
type FrameworkAnalyzer interface {
	// Name returns the framework name.
	Name() string

	// DetectRoutes finds HTTP routes in the scope.
	DetectRoutes(g *graph.Graph, scope string) ([]Route, error)

	// DetectMiddleware finds middleware on a route.
	DetectMiddleware(route Route) ([]Middleware, error)

	// IsAuthMiddleware checks if middleware provides authentication.
	// Returns (isAuth, methodName).
	IsAuthMiddleware(m Middleware) (bool, string)

	// IsAuthzMiddleware checks if middleware provides authorization.
	// Returns (isAuthz, methodName).
	IsAuthzMiddleware(m Middleware) (bool, string)
}

// --- Option Types ---

// TraceOption configures InputTracer.TraceUserInput.
type TraceOption func(*TraceConfig)

// TraceConfig holds configuration for input tracing.
type TraceConfig struct {
	MaxDepth       int
	MaxNodes       int
	SinkCategories []string
	Limits         ResourceLimits
}

// DefaultTraceConfig returns sensible defaults for tracing.
func DefaultTraceConfig() *TraceConfig {
	return &TraceConfig{
		MaxDepth:       10,
		MaxNodes:       1000,
		SinkCategories: nil,
		Limits:         DefaultResourceLimits(),
	}
}

// ApplyOptions applies a list of options to the config.
func (c *TraceConfig) ApplyOptions(opts ...TraceOption) {
	for _, opt := range opts {
		opt(c)
	}
}

// WithMaxDepth sets the maximum trace depth.
func WithMaxDepth(depth int) TraceOption {
	return func(c *TraceConfig) {
		c.MaxDepth = depth
	}
}

// WithMaxNodes sets the maximum nodes to visit.
func WithMaxNodes(nodes int) TraceOption {
	return func(c *TraceConfig) {
		c.MaxNodes = nodes
	}
}

// WithSinkCategories filters to specific sink types.
func WithSinkCategories(categories ...string) TraceOption {
	return func(c *TraceConfig) {
		c.SinkCategories = categories
	}
}

// WithResourceLimits sets resource constraints.
func WithResourceLimits(limits ResourceLimits) TraceOption {
	return func(c *TraceConfig) {
		c.Limits = limits
	}
}

// ScanOption configures SecurityScanner.ScanForSecurityIssues.
type ScanOption func(*ScanConfig)

// ScanConfig holds configuration for security scanning.
type ScanConfig struct {
	MinSeverity   Severity
	MinConfidence float64
	Parallelism   int
	Limits        ResourceLimits
}

// DefaultScanConfig returns sensible defaults for scanning.
func DefaultScanConfig() *ScanConfig {
	return &ScanConfig{
		MinSeverity:   SeverityMedium,
		MinConfidence: 0.5,
		Parallelism:   4,
		Limits:        DefaultResourceLimits(),
	}
}

// ApplyOptions applies a list of options to the config.
func (c *ScanConfig) ApplyOptions(opts ...ScanOption) {
	for _, opt := range opts {
		opt(c)
	}
}

// WithMinSeverity sets the minimum severity to report.
func WithMinSeverity(s Severity) ScanOption {
	return func(c *ScanConfig) {
		c.MinSeverity = s
	}
}

// WithMinConfidence sets the minimum confidence to report.
func WithMinConfidence(conf float64) ScanOption {
	return func(c *ScanConfig) {
		c.MinConfidence = conf
	}
}

// WithParallelism sets the number of parallel workers.
func WithParallelism(p int) ScanOption {
	return func(c *ScanConfig) {
		c.Parallelism = p
	}
}

// AuditOption configures ErrorAuditor.AuditErrorHandling.
type AuditOption func(*AuditConfig)

// AuditConfig holds configuration for error auditing.
type AuditConfig struct {
	Focus  string // "all", "fail_open", "info_leak"
	Limits ResourceLimits
}

// DefaultAuditConfig returns sensible defaults for auditing.
func DefaultAuditConfig() *AuditConfig {
	return &AuditConfig{
		Focus:  "all",
		Limits: DefaultResourceLimits(),
	}
}

// ApplyOptions applies a list of options to the config.
func (c *AuditConfig) ApplyOptions(opts ...AuditOption) {
	for _, opt := range opts {
		opt(c)
	}
}

// WithAuditFocus sets the focus area for auditing.
func WithAuditFocus(focus string) AuditOption {
	return func(c *AuditConfig) {
		c.Focus = focus
	}
}

// AuthCheckOption configures AuthChecker.CheckAuthEnforcement.
type AuthCheckOption func(*AuthCheckConfig)

// AuthCheckConfig holds configuration for auth checking.
type AuthCheckConfig struct {
	Framework string // Framework hint (auto-detected if empty)
	CheckType string // "both", "authentication", "authorization"
	Limits    ResourceLimits
}

// DefaultAuthCheckConfig returns sensible defaults for auth checking.
func DefaultAuthCheckConfig() *AuthCheckConfig {
	return &AuthCheckConfig{
		Framework: "", // Auto-detect
		CheckType: "both",
		Limits:    DefaultResourceLimits(),
	}
}

// ApplyOptions applies a list of options to the config.
func (c *AuthCheckConfig) ApplyOptions(opts ...AuthCheckOption) {
	for _, opt := range opts {
		opt(c)
	}
}

// WithFrameworkHint provides a framework name hint.
func WithFrameworkHint(fw string) AuthCheckOption {
	return func(c *AuthCheckConfig) {
		c.Framework = fw
	}
}

// WithAuthCheckType sets what to check.
func WithAuthCheckType(checkType string) AuthCheckOption {
	return func(c *AuthCheckConfig) {
		c.CheckType = checkType
	}
}

// BoundaryOption configures TrustBoundaryAnalyzer.AnalyzeTrustBoundary.
type BoundaryOption func(*boundaryConfig)

type boundaryConfig struct {
	ShowZones bool
	Limits    ResourceLimits
}

// WithShowZones includes full zone map in output.
func WithShowZones(show bool) BoundaryOption {
	return func(c *boundaryConfig) {
		c.ShowZones = show
	}
}

// --- Result Types ---

// InputTrace is the result of tracing user input through code.
type InputTrace struct {
	// Source is the input source that was traced.
	Source InputSource `json:"source"`

	// Path is the sequence of steps from source to sinks.
	Path []TraceStep `json:"path"`

	// Sinks are the sensitive sinks reached by the input.
	Sinks []Sink `json:"sinks"`

	// Sanitizers are the sanitization points found along the path.
	Sanitizers []Sanitizer `json:"sanitizers"`

	// Vulnerabilities are confirmed security issues.
	Vulnerabilities []Vulnerability `json:"vulnerabilities,omitempty"`

	// Confidence is the overall confidence in this trace (0.0-1.0).
	Confidence float64 `json:"confidence"`

	// Limitations lists what couldn't be analyzed.
	Limitations []string `json:"limitations,omitempty"`

	// PartialFailures lists analysis gaps.
	PartialFailures []PartialFailure `json:"partial_failures,omitempty"`

	// Duration is how long the trace took.
	Duration time.Duration `json:"duration"`
}

// InputSource describes where untrusted input enters.
type InputSource struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"` // http, env, file, cli, etc.
	Location    string `json:"location"`
	Description string `json:"description,omitempty"`
}

// TraceStep is one step in the data flow path.
type TraceStep struct {
	SymbolID  string    `json:"symbol_id"`
	Name      string    `json:"name"`
	Location  string    `json:"location"`
	Taint     DataTaint `json:"taint"`
	TaintedBy string    `json:"tainted_by,omitempty"` // Source of taint
}

// Sink describes a sensitive operation that receives data.
type Sink struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"` // sql, command, xss, path, ssrf, etc.
	Location    string `json:"location"`
	IsDangerous bool   `json:"is_dangerous"`
	CWE         string `json:"cwe,omitempty"`
}

// Sanitizer describes a function that makes data safe.
type Sanitizer struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Location     string   `json:"location"`
	MakesSafeFor []string `json:"makes_safe_for"` // Sink categories this sanitizes
	IsComplete   bool     `json:"is_complete"`    // Fully sanitizes or needs more?
	Notes        string   `json:"notes,omitempty"`
}

// Vulnerability is a confirmed security issue.
type Vulnerability struct {
	ID             string         `json:"id"`
	Type           string         `json:"type"` // sql_injection, xss, etc.
	CWE            string         `json:"cwe"`
	Severity       Severity       `json:"severity"`
	Confidence     float64        `json:"confidence"`
	Exploitability Exploitability `json:"exploitability"`
	Location       string         `json:"location"`
	Line           int            `json:"line"`
	Code           string         `json:"code,omitempty"`
	Description    string         `json:"description"`
	Remediation    string         `json:"remediation"`
	DataFlowProven bool           `json:"data_flow_proven"`
}

// ScanResult is the result of a security scan.
type ScanResult struct {
	Scope           string           `json:"scope"`
	Issues          []SecurityIssue  `json:"issues"`
	Summary         ScanSummary      `json:"summary"`
	PartialFailures []PartialFailure `json:"partial_failures,omitempty"`
	Confidence      float64          `json:"confidence"`
	CoveragePercent float64          `json:"coverage_percent"`
	Duration        time.Duration    `json:"duration"`
}

// SecurityIssue is a detected security vulnerability.
type SecurityIssue struct {
	ID              string   `json:"id"`
	Type            string   `json:"type"`
	Severity        Severity `json:"severity"`
	Confidence      float64  `json:"confidence"`
	Location        string   `json:"location"`
	Line            int      `json:"line"`
	Code            string   `json:"code,omitempty"`
	Description     string   `json:"description"`
	Remediation     string   `json:"remediation"`
	CWE             string   `json:"cwe"`
	CVSS            float64  `json:"cvss,omitempty"`
	DataFlowProven  bool     `json:"data_flow_proven"`
	Suppressed      bool     `json:"suppressed"`
	SuppressionNote string   `json:"suppression_note,omitempty"`
}

// ScanSummary summarizes scan results.
type ScanSummary struct {
	TotalIssues  int `json:"total_issues"`
	Critical     int `json:"critical"`
	High         int `json:"high"`
	Medium       int `json:"medium"`
	Low          int `json:"low"`
	Suppressed   int `json:"suppressed"`
	FilesScanned int `json:"files_scanned"`
}

// ErrorAudit is the result of error handling audit.
type ErrorAudit struct {
	Scope           string           `json:"scope"`
	Issues          []ErrorIssue     `json:"issues"`
	Summary         ErrorSummary     `json:"summary"`
	PartialFailures []PartialFailure `json:"partial_failures,omitempty"`
	Duration        time.Duration    `json:"duration"`
}

// ErrorIssue is a detected error handling problem.
type ErrorIssue struct {
	Type       string   `json:"type"` // fail_open, leak_info, swallow, expose_stack
	Severity   Severity `json:"severity"`
	Location   string   `json:"location"`
	Line       int      `json:"line"`
	Code       string   `json:"code,omitempty"`
	Context    string   `json:"context"`
	Risk       string   `json:"risk"`
	Suggestion string   `json:"suggestion"`
	CWE        string   `json:"cwe,omitempty"`
}

// ErrorSummary summarizes error audit results.
type ErrorSummary struct {
	TotalErrors     int `json:"total_errors"`
	Handled         int `json:"handled"`
	Swallowed       int `json:"swallowed"`
	InfoLeaks       int `json:"info_leaks"`
	FailOpenPaths   int `json:"fail_open_paths"`
	FailClosedPaths int `json:"fail_closed_paths"`
}

// HardcodedSecret is a detected secret in code.
type HardcodedSecret struct {
	Type     string   `json:"type"` // api_key, password, private_key, etc.
	Location string   `json:"location"`
	Line     int      `json:"line"`
	Context  string   `json:"context"` // Masked code around secret
	Severity Severity `json:"severity"`
}

// AuthCheck is the result of auth enforcement check.
type AuthCheck struct {
	Scope            string           `json:"scope"`
	Framework        string           `json:"framework"`
	Endpoints        []EndpointAuth   `json:"endpoints"`
	MissingAuth      int              `json:"missing_auth"`
	MissingAuthz     int              `json:"missing_authz"`
	Suggestions      []string         `json:"suggestions,omitempty"`
	PartialFailures  []PartialFailure `json:"partial_failures,omitempty"`
	FrameworkDetails *FrameworkInfo   `json:"framework_details,omitempty"`
	Duration         time.Duration    `json:"duration"`
}

// EndpointAuth describes auth status of an endpoint.
type EndpointAuth struct {
	Name              string   `json:"name"`
	Type              string   `json:"type"` // http, grpc, graphql, websocket
	Path              string   `json:"path"`
	Method            string   `json:"method"`
	Framework         string   `json:"framework"`
	HasAuthentication bool     `json:"has_authentication"`
	AuthMethod        string   `json:"auth_method,omitempty"`
	HasAuthorization  bool     `json:"has_authorization"`
	AuthzMethod       string   `json:"authz_method,omitempty"`
	Risk              Severity `json:"risk,omitempty"`
	IsAdminEndpoint   bool     `json:"is_admin_endpoint"`
	HandlesData       bool     `json:"handles_data"`
}

// FrameworkInfo describes detected framework details.
type FrameworkInfo struct {
	Name       string   `json:"name"`
	Confidence float64  `json:"confidence"`
	Indicators []string `json:"indicators"`
	Version    string   `json:"version,omitempty"`
}

// Route describes an HTTP route.
type Route struct {
	Path       string   `json:"path"`
	Method     string   `json:"method"`
	Handler    string   `json:"handler"`
	Location   string   `json:"location"`
	Middleware []string `json:"middleware,omitempty"`
}

// Middleware describes a middleware function.
type Middleware struct {
	Name     string `json:"name"`
	Location string `json:"location"`
	Type     string `json:"type,omitempty"` // auth, authz, logging, etc.
}

// TrustBoundary is the result of trust boundary analysis.
type TrustBoundary struct {
	Scope           string              `json:"scope"`
	Zones           []TrustZone         `json:"zones,omitempty"`
	Crossings       []BoundaryCrossing  `json:"crossings"`
	Violations      []BoundaryViolation `json:"violations"`
	Recommendations []string            `json:"recommendations,omitempty"`
	PartialFailures []PartialFailure    `json:"partial_failures,omitempty"`
	Duration        time.Duration       `json:"duration"`
}

// TrustZone represents a region of code with uniform trust level.
type TrustZone struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Level       TrustLevel `json:"level"`
	EntryPoints []string   `json:"entry_points"`
	ExitPoints  []string   `json:"exit_points"`
	Files       []string   `json:"files"`
}

// BoundaryCrossing represents data moving between zones.
type BoundaryCrossing struct {
	ID            string     `json:"id"`
	From          *TrustZone `json:"from"`
	To            *TrustZone `json:"to"`
	CrossingAt    string     `json:"crossing_at"`
	DataPath      []string   `json:"data_path"`
	HasValidation bool       `json:"has_validation"`
	ValidationFn  string     `json:"validation_fn,omitempty"`
}

// BoundaryViolation represents an unsafe crossing.
type BoundaryViolation struct {
	Crossing    *BoundaryCrossing `json:"crossing"`
	Severity    Severity          `json:"severity"`
	MissingStep string            `json:"missing_step"`
	CWE         string            `json:"cwe"`
	Remediation string            `json:"remediation"`
}

// VerificationResult is the result of remediation verification.
type VerificationResult struct {
	IssueID       string          `json:"issue_id"`
	OriginalIssue *SecurityIssue  `json:"original_issue"`
	IsFixed       bool            `json:"is_fixed"`
	StillPresent  bool            `json:"still_present"`
	NewIssues     []SecurityIssue `json:"new_issues,omitempty"`
	Explanation   string          `json:"explanation"`
}
