// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package compliance

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ComplianceReporter generates compliance reports and audit logs.
//
// # Description
//
// Maintains in-memory audit trails of blast radius analyses and generates
// reports suitable for compliance requirements (SOC2, ISO27001, etc.).
// Data is stored in-memory with optional JSON file persistence.
//
// NO external dependencies (no SQLite, no Redis).
//
// # Thread Safety
//
// Safe for concurrent use.
type ComplianceReporter struct {
	mu        sync.RWMutex
	events    []AuditEvent
	maxEvents int
	dataDir   string
}

// AuditEvent represents an auditable event.
type AuditEvent struct {
	// ID is the unique event identifier.
	ID string `json:"id"`

	// Timestamp is when the event occurred.
	Timestamp time.Time `json:"timestamp"`

	// EventType is the type of event.
	EventType AuditEventType `json:"event_type"`

	// Actor is who performed the action.
	Actor string `json:"actor"`

	// TargetSymbol is the symbol involved.
	TargetSymbol string `json:"target_symbol,omitempty"`

	// ProjectRoot is the project identifier.
	ProjectRoot string `json:"project_root,omitempty"`

	// Action is the action performed.
	Action string `json:"action"`

	// Outcome is the result of the action.
	Outcome string `json:"outcome"`

	// RiskLevel is the assessed risk level.
	RiskLevel string `json:"risk_level,omitempty"`

	// Details contains additional event details.
	Details map[string]interface{} `json:"details,omitempty"`

	// Metadata contains additional metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// AuditEventType categorizes audit events.
type AuditEventType string

const (
	AuditEventAnalysis     AuditEventType = "ANALYSIS"
	AuditEventRuleMatch    AuditEventType = "RULE_MATCH"
	AuditEventBlock        AuditEventType = "BLOCK"
	AuditEventApproval     AuditEventType = "APPROVAL"
	AuditEventOverride     AuditEventType = "OVERRIDE"
	AuditEventConfigChange AuditEventType = "CONFIG_CHANGE"
)

// ComplianceReport represents a compliance report.
type ComplianceReport struct {
	// GeneratedAt is when the report was generated.
	GeneratedAt time.Time `json:"generated_at"`

	// ReportPeriod is the period covered.
	ReportPeriod ReportPeriod `json:"report_period"`

	// Summary contains summary statistics.
	Summary ReportSummary `json:"summary"`

	// Events contains the audit events.
	Events []AuditEvent `json:"events"`

	// RuleViolations contains rule violations.
	RuleViolations []RuleViolation `json:"rule_violations"`

	// SecurityFindings contains security-related findings.
	SecurityFindings []SecurityFinding `json:"security_findings"`
}

// ReportPeriod defines the report time range.
type ReportPeriod struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// ReportSummary contains summary statistics.
type ReportSummary struct {
	TotalAnalyses         int            `json:"total_analyses"`
	TotalRuleMatches      int            `json:"total_rule_matches"`
	TotalBlocks           int            `json:"total_blocks"`
	TotalOverrides        int            `json:"total_overrides"`
	RiskDistribution      map[string]int `json:"risk_distribution"`
	TopRiskySymbols       []SymbolRisk   `json:"top_risky_symbols"`
	AverageCallerCount    float64        `json:"average_caller_count"`
	SecurityPathsAffected int            `json:"security_paths_affected"`
}

// SymbolRisk represents a symbol's risk summary.
type SymbolRisk struct {
	SymbolID   string `json:"symbol_id"`
	RiskLevel  string `json:"risk_level"`
	MatchCount int    `json:"match_count"`
}

// RuleViolation represents a rule violation.
type RuleViolation struct {
	Timestamp time.Time `json:"timestamp"`
	RuleName  string    `json:"rule_name"`
	SymbolID  string    `json:"symbol_id"`
	Actor     string    `json:"actor"`
	Severity  string    `json:"severity"`
	Outcome   string    `json:"outcome"`
}

// SecurityFinding represents a security-related finding.
type SecurityFinding struct {
	Timestamp    time.Time `json:"timestamp"`
	SymbolID     string    `json:"symbol_id"`
	SecurityPath string    `json:"security_path"`
	RiskLevel    string    `json:"risk_level"`
	Actor        string    `json:"actor"`
	Mitigated    bool      `json:"mitigated"`
}

// ReporterOptions configures the compliance reporter.
type ReporterOptions struct {
	// MaxEvents is the maximum number of events to keep in memory.
	// Default: 10000
	MaxEvents int

	// PersistPath is the optional path for JSON persistence.
	// If empty, data is memory-only.
	PersistPath string
}

// DefaultReporterOptions returns sensible defaults.
func DefaultReporterOptions() ReporterOptions {
	return ReporterOptions{
		MaxEvents: 10000,
	}
}

// NewComplianceReporter creates a new compliance reporter.
//
// # Inputs
//
//   - dataDir: Directory for optional JSON persistence. Empty for memory-only.
//
// # Outputs
//
//   - *ComplianceReporter: Ready-to-use reporter.
//   - error: Non-nil if loading persisted data failed.
func NewComplianceReporter(dataDir string) (*ComplianceReporter, error) {
	opts := DefaultReporterOptions()
	return NewComplianceReporterWithOptions(dataDir, &opts)
}

// NewComplianceReporterWithOptions creates a reporter with options.
func NewComplianceReporterWithOptions(dataDir string, opts *ReporterOptions) (*ComplianceReporter, error) {
	if opts == nil {
		defaults := DefaultReporterOptions()
		opts = &defaults
	}

	reporter := &ComplianceReporter{
		events:    make([]AuditEvent, 0, opts.MaxEvents),
		maxEvents: opts.MaxEvents,
		dataDir:   dataDir,
	}

	// Try to load persisted data
	if dataDir != "" {
		if err := reporter.loadPersisted(); err != nil {
			// Non-fatal, start fresh
			_ = err
		}
	}

	return reporter, nil
}

// loadPersisted loads data from JSON file.
func (c *ComplianceReporter) loadPersisted() error {
	if c.dataDir == "" {
		return nil
	}

	path := filepath.Join(c.dataDir, "audit_log.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return json.Unmarshal(data, &c.events)
}

// RecordEvent records an audit event.
//
// # Inputs
//
//   - event: The event to record.
//
// # Outputs
//
//   - error: Non-nil if recording failed.
func (c *ComplianceReporter) RecordEvent(event AuditEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Generate ID if not provided
	if event.ID == "" {
		event.ID = generateEventID()
	}

	// Ensure timestamp
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	c.events = append(c.events, event)

	// Trim if exceeds max
	if len(c.events) > c.maxEvents {
		// Keep most recent
		c.events = c.events[len(c.events)-c.maxEvents:]
	}

	return nil
}

// generateEventID generates a unique event ID.
func generateEventID() string {
	now := time.Now()
	return fmt.Sprintf("evt_%d_%d", now.UnixNano(), now.Nanosecond()%1000)
}

// QueryEvents retrieves audit events matching criteria.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - criteria: Query criteria.
//
// # Outputs
//
//   - []AuditEvent: Matching events.
//   - error: Non-nil on failure.
func (c *ComplianceReporter) QueryEvents(ctx context.Context, criteria QueryCriteria) ([]AuditEvent, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []AuditEvent

	for _, e := range c.events {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Apply filters
		if !criteria.StartTime.IsZero() && e.Timestamp.Before(criteria.StartTime) {
			continue
		}
		if !criteria.EndTime.IsZero() && e.Timestamp.After(criteria.EndTime) {
			continue
		}
		if criteria.EventType != "" && e.EventType != criteria.EventType {
			continue
		}
		if criteria.Actor != "" && e.Actor != criteria.Actor {
			continue
		}
		if criteria.SymbolID != "" && e.TargetSymbol != criteria.SymbolID {
			continue
		}

		result = append(result, e)
	}

	// Sort by timestamp descending
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.After(result[j].Timestamp)
	})

	// Apply limit
	if criteria.Limit > 0 && len(result) > criteria.Limit {
		result = result[:criteria.Limit]
	}

	return result, nil
}

// QueryCriteria specifies audit event query criteria.
type QueryCriteria struct {
	StartTime time.Time
	EndTime   time.Time
	EventType AuditEventType
	Actor     string
	SymbolID  string
	Limit     int
}

// GenerateReport generates a compliance report.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - period: The report period.
//
// # Outputs
//
//   - *ComplianceReport: The generated report.
//   - error: Non-nil on failure.
func (c *ComplianceReporter) GenerateReport(ctx context.Context, period ReportPeriod) (*ComplianceReport, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}

	report := &ComplianceReport{
		GeneratedAt:      time.Now(),
		ReportPeriod:     period,
		RuleViolations:   make([]RuleViolation, 0),
		SecurityFindings: make([]SecurityFinding, 0),
	}

	// Get all events in period
	events, err := c.QueryEvents(ctx, QueryCriteria{
		StartTime: period.Start,
		EndTime:   period.End,
	})
	if err != nil {
		return nil, err
	}

	report.Events = events

	// Calculate summary
	report.Summary = c.calculateSummary(events)

	// Extract rule violations
	for _, e := range events {
		if e.EventType == AuditEventRuleMatch || e.EventType == AuditEventBlock {
			violation := RuleViolation{
				Timestamp: e.Timestamp,
				SymbolID:  e.TargetSymbol,
				Actor:     e.Actor,
				Outcome:   e.Outcome,
			}
			if e.Details != nil {
				if rn, ok := e.Details["rule_name"].(string); ok {
					violation.RuleName = rn
				}
				if sev, ok := e.Details["severity"].(string); ok {
					violation.Severity = sev
				}
			}
			report.RuleViolations = append(report.RuleViolations, violation)
		}
	}

	// Extract security findings
	for _, e := range events {
		if e.Details != nil {
			if sp, ok := e.Details["security_path"].(string); ok && sp != "" {
				finding := SecurityFinding{
					Timestamp:    e.Timestamp,
					SymbolID:     e.TargetSymbol,
					SecurityPath: sp,
					RiskLevel:    e.RiskLevel,
					Actor:        e.Actor,
					Mitigated:    e.Outcome == "APPROVED" || e.Outcome == "MITIGATED",
				}
				report.SecurityFindings = append(report.SecurityFindings, finding)
			}
		}
	}

	return report, nil
}

// calculateSummary calculates report summary statistics.
func (c *ComplianceReporter) calculateSummary(events []AuditEvent) ReportSummary {
	summary := ReportSummary{
		RiskDistribution: make(map[string]int),
	}

	symbolRisks := make(map[string]*SymbolRisk)
	var totalCallers float64
	var callerCount int

	for _, e := range events {
		switch e.EventType {
		case AuditEventAnalysis:
			summary.TotalAnalyses++
		case AuditEventRuleMatch:
			summary.TotalRuleMatches++
		case AuditEventBlock:
			summary.TotalBlocks++
		case AuditEventOverride:
			summary.TotalOverrides++
		}

		if e.RiskLevel != "" {
			summary.RiskDistribution[e.RiskLevel]++
		}

		// Track symbol risks
		if e.TargetSymbol != "" {
			if sr, ok := symbolRisks[e.TargetSymbol]; ok {
				sr.MatchCount++
				if riskGreater(e.RiskLevel, sr.RiskLevel) {
					sr.RiskLevel = e.RiskLevel
				}
			} else {
				symbolRisks[e.TargetSymbol] = &SymbolRisk{
					SymbolID:   e.TargetSymbol,
					RiskLevel:  e.RiskLevel,
					MatchCount: 1,
				}
			}
		}

		// Track caller counts
		if e.Details != nil {
			if cc, ok := e.Details["caller_count"].(float64); ok {
				totalCallers += cc
				callerCount++
			}
		}

		// Track security paths
		if e.Details != nil {
			if _, ok := e.Details["security_path"]; ok {
				summary.SecurityPathsAffected++
			}
		}
	}

	// Calculate average caller count
	if callerCount > 0 {
		summary.AverageCallerCount = totalCallers / float64(callerCount)
	}

	// Get top risky symbols
	for _, sr := range symbolRisks {
		summary.TopRiskySymbols = append(summary.TopRiskySymbols, *sr)
	}

	// Sort by match count descending
	sort.Slice(summary.TopRiskySymbols, func(i, j int) bool {
		return summary.TopRiskySymbols[i].MatchCount > summary.TopRiskySymbols[j].MatchCount
	})

	// Limit to top 10
	if len(summary.TopRiskySymbols) > 10 {
		summary.TopRiskySymbols = summary.TopRiskySymbols[:10]
	}

	return summary
}

// riskGreater returns true if a > b in risk level.
func riskGreater(a, b string) bool {
	order := map[string]int{
		"LOW":      0,
		"MEDIUM":   1,
		"HIGH":     2,
		"CRITICAL": 3,
	}
	return order[a] > order[b]
}

// ExportCSV exports audit events to CSV.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - criteria: Query criteria.
//   - outputPath: Path for the CSV file.
//
// # Outputs
//
//   - error: Non-nil on failure.
func (c *ComplianceReporter) ExportCSV(ctx context.Context, criteria QueryCriteria, outputPath string) error {
	events, err := c.QueryEvents(ctx, criteria)
	if err != nil {
		return err
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Header
	header := []string{
		"ID", "Timestamp", "Event Type", "Actor", "Target Symbol",
		"Project", "Action", "Outcome", "Risk Level",
	}
	writer.Write(header)

	// Data
	for _, e := range events {
		row := []string{
			e.ID,
			e.Timestamp.Format(time.RFC3339),
			string(e.EventType),
			e.Actor,
			e.TargetSymbol,
			e.ProjectRoot,
			e.Action,
			e.Outcome,
			e.RiskLevel,
		}
		writer.Write(row)
	}

	return nil
}

// ExportJSON exports a compliance report to JSON.
//
// # Inputs
//
//   - report: The report to export.
//   - outputPath: Path for the JSON file.
//
// # Outputs
//
//   - error: Non-nil on failure.
func (c *ComplianceReporter) ExportJSON(report *ComplianceReport, outputPath string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}

	return os.WriteFile(outputPath, data, 0644)
}

// Persist saves data to JSON file (if dataDir was provided).
func (c *ComplianceReporter) Persist() error {
	if c.dataDir == "" {
		return nil
	}

	c.mu.RLock()
	events := make([]AuditEvent, len(c.events))
	copy(events, c.events)
	c.mu.RUnlock()

	// Ensure directory exists
	if err := os.MkdirAll(c.dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	data, err := json.Marshal(events)
	if err != nil {
		return fmt.Errorf("marshal events: %w", err)
	}

	path := filepath.Join(c.dataDir, "audit_log.json")
	return os.WriteFile(path, data, 0644)
}

// Close persists data and cleans up.
func (c *ComplianceReporter) Close() error {
	return c.Persist()
}

// RecordAnalysis records an analysis event.
func (c *ComplianceReporter) RecordAnalysis(actor, symbolID, projectRoot, riskLevel string, callerCount int) error {
	return c.RecordEvent(AuditEvent{
		EventType:    AuditEventAnalysis,
		Actor:        actor,
		TargetSymbol: symbolID,
		ProjectRoot:  projectRoot,
		Action:       "ANALYZE_BLAST_RADIUS",
		Outcome:      "COMPLETED",
		RiskLevel:    riskLevel,
		Details: map[string]interface{}{
			"caller_count": callerCount,
		},
	})
}

// RecordRuleMatch records a rule match event.
func (c *ComplianceReporter) RecordRuleMatch(actor, symbolID, ruleName, severity, action string) error {
	return c.RecordEvent(AuditEvent{
		EventType:    AuditEventRuleMatch,
		Actor:        actor,
		TargetSymbol: symbolID,
		Action:       "RULE_EVALUATED",
		Outcome:      action,
		RiskLevel:    severity,
		Details: map[string]interface{}{
			"rule_name": ruleName,
			"severity":  severity,
		},
	})
}

// RecordBlock records a block event.
func (c *ComplianceReporter) RecordBlock(actor, symbolID, reason string) error {
	return c.RecordEvent(AuditEvent{
		EventType:    AuditEventBlock,
		Actor:        actor,
		TargetSymbol: symbolID,
		Action:       "CHANGE_BLOCKED",
		Outcome:      "BLOCKED",
		RiskLevel:    "CRITICAL",
		Details: map[string]interface{}{
			"reason": reason,
		},
	})
}

// RecordOverride records an override event.
func (c *ComplianceReporter) RecordOverride(actor, symbolID, reason, approver string) error {
	return c.RecordEvent(AuditEvent{
		EventType:    AuditEventOverride,
		Actor:        actor,
		TargetSymbol: symbolID,
		Action:       "RULE_OVERRIDE",
		Outcome:      "APPROVED",
		Details: map[string]interface{}{
			"reason":   reason,
			"approver": approver,
		},
	})
}

// EventCount returns the current number of events.
func (c *ComplianceReporter) EventCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.events)
}

// Clear removes all events.
func (c *ComplianceReporter) Clear() {
	c.mu.Lock()
	c.events = make([]AuditEvent, 0, c.maxEvents)
	c.mu.Unlock()
}
