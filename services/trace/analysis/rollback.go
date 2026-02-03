// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package analysis

import (
	"context"
	"regexp"
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// RollbackRiskAssessor assesses the risk and difficulty of rolling back changes.
//
// # Description
//
// Analyzes changes to identify irreversible or hard-to-rollback modifications
// such as database schema changes, API removals, and data migrations.
//
// # Thread Safety
//
// Safe for concurrent use after construction.
type RollbackRiskAssessor struct {
	graph *graph.Graph
	index *index.SymbolIndex
}

// RollbackRisk represents the risk assessment for rolling back a change.
type RollbackRisk struct {
	// Level is the overall risk level.
	Level RollbackRiskLevel `json:"level"`

	// Score is a numeric score (0-100, higher = harder to rollback).
	Score int `json:"score"`

	// IrreversibleChanges are changes that cannot be undone.
	IrreversibleChanges []IrreversibleChange `json:"irreversible_changes,omitempty"`

	// HighRiskFactors are factors contributing to rollback difficulty.
	HighRiskFactors []RiskFactor `json:"high_risk_factors,omitempty"`

	// MitigationSteps are suggested steps to enable safe rollback.
	MitigationSteps []string `json:"mitigation_steps,omitempty"`

	// RequiresDataMigration indicates if data migration is needed.
	RequiresDataMigration bool `json:"requires_data_migration"`

	// RequiresAPIVersioning indicates if API versioning is needed.
	RequiresAPIVersioning bool `json:"requires_api_versioning"`

	// EstimatedRecoveryComplexity estimates rollback complexity.
	EstimatedRecoveryComplexity string `json:"estimated_recovery_complexity"`
}

// RollbackRiskLevel represents the risk level for rollback.
type RollbackRiskLevel string

const (
	RollbackRiskLow      RollbackRiskLevel = "LOW"
	RollbackRiskMedium   RollbackRiskLevel = "MEDIUM"
	RollbackRiskHigh     RollbackRiskLevel = "HIGH"
	RollbackRiskCritical RollbackRiskLevel = "CRITICAL"
)

// IrreversibleChange represents a change that cannot be easily undone.
type IrreversibleChange struct {
	// Type is the type of irreversible change.
	Type IrreversibleChangeType `json:"type"`

	// Description describes what was changed.
	Description string `json:"description"`

	// Location is where the change occurs.
	Location string `json:"location"`

	// Impact describes the impact of this change.
	Impact string `json:"impact"`
}

// IrreversibleChangeType represents types of irreversible changes.
type IrreversibleChangeType string

const (
	IrreversibleColumnDrop   IrreversibleChangeType = "COLUMN_DROP"
	IrreversibleTableDrop    IrreversibleChangeType = "TABLE_DROP"
	IrreversibleDataDelete   IrreversibleChangeType = "DATA_DELETE"
	IrreversibleAPIRemoval   IrreversibleChangeType = "API_REMOVAL"
	IrreversibleFieldRemoval IrreversibleChangeType = "FIELD_REMOVAL"
	IrreversibleTypeChange   IrreversibleChangeType = "TYPE_CHANGE"
)

// RiskFactor represents a factor contributing to rollback risk.
type RiskFactor struct {
	// Factor is the risk factor name.
	Factor string `json:"factor"`

	// Description explains the risk.
	Description string `json:"description"`

	// Weight is the contribution to the overall score (0-100).
	Weight int `json:"weight"`
}

// NewRollbackRiskAssessor creates a new assessor.
func NewRollbackRiskAssessor(g *graph.Graph, idx *index.SymbolIndex) *RollbackRiskAssessor {
	return &RollbackRiskAssessor{
		graph: g,
		index: idx,
	}
}

// AssessChange assesses the rollback risk for a change.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - changeType: Type of change (e.g., "function_removed", "field_added").
//   - symbolID: The symbol being changed.
//   - details: Additional details about the change.
//
// # Outputs
//
//   - *RollbackRisk: The risk assessment.
//   - error: Non-nil on failure.
func (r *RollbackRiskAssessor) AssessChange(
	ctx context.Context,
	changeType string,
	symbolID string,
	details map[string]string,
) (*RollbackRisk, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	risk := &RollbackRisk{
		Level:                       RollbackRiskLow,
		Score:                       0,
		IrreversibleChanges:         make([]IrreversibleChange, 0),
		HighRiskFactors:             make([]RiskFactor, 0),
		MitigationSteps:             make([]string, 0),
		EstimatedRecoveryComplexity: "SIMPLE",
	}

	// Assess based on change type
	switch changeType {
	case "function_removed", "method_removed":
		r.assessFunctionRemoval(risk, symbolID)
	case "field_removed", "column_removed":
		r.assessFieldRemoval(risk, symbolID, details)
	case "type_changed":
		r.assessTypeChange(risk, symbolID, details)
	case "api_removed", "endpoint_removed":
		r.assessAPIRemoval(risk, symbolID)
	case "schema_altered":
		r.assessSchemaChange(risk, symbolID, details)
	}

	// Check for caller count risk
	r.assessCallerRisk(risk, symbolID)

	// Calculate final score and level
	r.calculateFinalScore(risk)

	return risk, nil
}

// assessFunctionRemoval assesses risk of removing a function.
func (r *RollbackRiskAssessor) assessFunctionRemoval(risk *RollbackRisk, symbolID string) {
	var callerCount int
	if node, found := r.graph.GetNode(symbolID); found {
		callerCount = len(node.Incoming)
	}

	if callerCount > 0 {
		risk.IrreversibleChanges = append(risk.IrreversibleChanges, IrreversibleChange{
			Type:        IrreversibleAPIRemoval,
			Description: "Function removed with existing callers",
			Location:    symbolID,
			Impact:      "Callers will fail to compile/run",
		})

		risk.HighRiskFactors = append(risk.HighRiskFactors, RiskFactor{
			Factor:      "active_callers",
			Description: "Function has active callers that depend on it",
			Weight:      30,
		})

		risk.MitigationSteps = append(risk.MitigationSteps,
			"Deprecate function before removal",
			"Provide migration path for callers",
			"Use feature flag to gradually disable",
		)
	}

	// Check if exported
	node, ok := r.graph.GetNode(symbolID)
	if ok && node.Symbol != nil && isExported(node.Symbol.Name) {
		risk.HighRiskFactors = append(risk.HighRiskFactors, RiskFactor{
			Factor:      "exported_symbol",
			Description: "Removing an exported symbol may break external consumers",
			Weight:      20,
		})
		risk.RequiresAPIVersioning = true
	}
}

// assessFieldRemoval assesses risk of removing a struct field.
func (r *RollbackRiskAssessor) assessFieldRemoval(risk *RollbackRisk, symbolID string, details map[string]string) {
	risk.IrreversibleChanges = append(risk.IrreversibleChanges, IrreversibleChange{
		Type:        IrreversibleFieldRemoval,
		Description: "Struct field removed",
		Location:    symbolID,
		Impact:      "Data associated with this field may be lost",
	})

	// Check if this might be a database column
	if details != nil {
		if _, ok := details["table"]; ok {
			risk.IrreversibleChanges = append(risk.IrreversibleChanges, IrreversibleChange{
				Type:        IrreversibleColumnDrop,
				Description: "Database column drop detected",
				Location:    symbolID,
				Impact:      "Data in this column will be permanently lost",
			})

			risk.HighRiskFactors = append(risk.HighRiskFactors, RiskFactor{
				Factor:      "data_loss",
				Description: "Database column removal causes permanent data loss",
				Weight:      50,
			})

			risk.RequiresDataMigration = true

			risk.MitigationSteps = append(risk.MitigationSteps,
				"Backup data before migration",
				"Create reversible migration script",
				"Add column back if rollback needed",
			)
		}
	}

	// Check for serialization tags
	risk.MitigationSteps = append(risk.MitigationSteps,
		"Ensure serialized data compatibility",
		"Consider soft-delete (nullable) instead of removal",
	)
}

// assessTypeChange assesses risk of changing a type.
func (r *RollbackRiskAssessor) assessTypeChange(risk *RollbackRisk, symbolID string, details map[string]string) {
	risk.IrreversibleChanges = append(risk.IrreversibleChanges, IrreversibleChange{
		Type:        IrreversibleTypeChange,
		Description: "Type signature changed",
		Location:    symbolID,
		Impact:      "All usages need to be updated",
	})

	risk.HighRiskFactors = append(risk.HighRiskFactors, RiskFactor{
		Factor:      "breaking_change",
		Description: "Type change is a breaking change for all consumers",
		Weight:      25,
	})

	// Check old and new types for severity
	if details != nil {
		oldType := details["old_type"]
		newType := details["new_type"]

		// Narrowing changes are harder to rollback
		if isNarrowingChange(oldType, newType) {
			risk.HighRiskFactors = append(risk.HighRiskFactors, RiskFactor{
				Factor:      "narrowing_change",
				Description: "Type narrowing may cause data truncation",
				Weight:      30,
			})
		}
	}

	risk.MitigationSteps = append(risk.MitigationSteps,
		"Add type conversion functions",
		"Maintain backward compatibility layer",
		"Version the API",
	)
}

// assessAPIRemoval assesses risk of removing an API endpoint.
func (r *RollbackRiskAssessor) assessAPIRemoval(risk *RollbackRisk, symbolID string) {
	risk.IrreversibleChanges = append(risk.IrreversibleChanges, IrreversibleChange{
		Type:        IrreversibleAPIRemoval,
		Description: "API endpoint removed",
		Location:    symbolID,
		Impact:      "External clients will receive 404 errors",
	})

	risk.HighRiskFactors = append(risk.HighRiskFactors, RiskFactor{
		Factor:      "external_api",
		Description: "API changes affect external consumers",
		Weight:      40,
	})

	risk.RequiresAPIVersioning = true

	risk.MitigationSteps = append(risk.MitigationSteps,
		"Deprecate endpoint before removal",
		"Maintain old version alongside new",
		"Communicate changes to API consumers",
		"Add sunset header to deprecated endpoints",
	)
}

// assessSchemaChange assesses risk of database schema changes.
func (r *RollbackRiskAssessor) assessSchemaChange(risk *RollbackRisk, symbolID string, details map[string]string) {
	if details == nil {
		return
	}

	operation := details["operation"]
	switch operation {
	case "DROP TABLE":
		risk.IrreversibleChanges = append(risk.IrreversibleChanges, IrreversibleChange{
			Type:        IrreversibleTableDrop,
			Description: "Table dropped",
			Location:    symbolID,
			Impact:      "All data in table permanently lost",
		})
		risk.HighRiskFactors = append(risk.HighRiskFactors, RiskFactor{
			Factor:      "table_drop",
			Description: "Dropping table causes permanent data loss",
			Weight:      60,
		})

	case "DROP COLUMN":
		risk.IrreversibleChanges = append(risk.IrreversibleChanges, IrreversibleChange{
			Type:        IrreversibleColumnDrop,
			Description: "Column dropped",
			Location:    symbolID,
			Impact:      "Data in column permanently lost",
		})
		risk.HighRiskFactors = append(risk.HighRiskFactors, RiskFactor{
			Factor:      "column_drop",
			Description: "Dropping column causes data loss",
			Weight:      50,
		})

	case "ALTER COLUMN":
		risk.HighRiskFactors = append(risk.HighRiskFactors, RiskFactor{
			Factor:      "column_alter",
			Description: "Column type change may cause data truncation",
			Weight:      30,
		})
	}

	risk.RequiresDataMigration = true

	risk.MitigationSteps = append(risk.MitigationSteps,
		"Create backup before migration",
		"Write reversible migration scripts",
		"Test migration on staging first",
		"Plan for data recovery if needed",
	)
}

// assessCallerRisk adds risk factors based on number of callers.
func (r *RollbackRiskAssessor) assessCallerRisk(risk *RollbackRisk, symbolID string) {
	var callerCount int
	if node, found := r.graph.GetNode(symbolID); found {
		callerCount = len(node.Incoming)
	}

	if callerCount > 50 {
		risk.HighRiskFactors = append(risk.HighRiskFactors, RiskFactor{
			Factor:      "high_caller_count",
			Description: "Many callers depend on this symbol",
			Weight:      20,
		})
		risk.EstimatedRecoveryComplexity = "COMPLEX"
	} else if callerCount > 20 {
		risk.HighRiskFactors = append(risk.HighRiskFactors, RiskFactor{
			Factor:      "moderate_caller_count",
			Description: "Moderate number of callers",
			Weight:      10,
		})
		risk.EstimatedRecoveryComplexity = "MODERATE"
	}
}

// calculateFinalScore calculates the final risk score and level.
func (r *RollbackRiskAssessor) calculateFinalScore(risk *RollbackRisk) {
	// Sum up risk factor weights
	score := 0
	for _, factor := range risk.HighRiskFactors {
		score += factor.Weight
	}

	// Add weight for irreversible changes
	score += len(risk.IrreversibleChanges) * 15

	// Cap at 100
	if score > 100 {
		score = 100
	}
	risk.Score = score

	// Determine level
	switch {
	case score >= 70:
		risk.Level = RollbackRiskCritical
		risk.EstimatedRecoveryComplexity = "VERY_COMPLEX"
	case score >= 50:
		risk.Level = RollbackRiskHigh
		if risk.EstimatedRecoveryComplexity != "VERY_COMPLEX" {
			risk.EstimatedRecoveryComplexity = "COMPLEX"
		}
	case score >= 25:
		risk.Level = RollbackRiskMedium
		if risk.EstimatedRecoveryComplexity == "SIMPLE" {
			risk.EstimatedRecoveryComplexity = "MODERATE"
		}
	default:
		risk.Level = RollbackRiskLow
	}
}

// isNarrowingChange checks if a type change is narrowing (e.g., int64 -> int32).
func isNarrowingChange(oldType, newType string) bool {
	narrowingPairs := map[string][]string{
		"int64":   {"int32", "int16", "int8", "int"},
		"float64": {"float32"},
		"string":  {},
	}

	if narrowing, ok := narrowingPairs[oldType]; ok {
		for _, t := range narrowing {
			if t == newType {
				return true
			}
		}
	}
	return false
}

// RollbackEnricher implements Enricher for rollback risk assessment.
type RollbackEnricher struct {
	assessor *RollbackRiskAssessor
	mu       sync.RWMutex
}

// NewRollbackEnricher creates a rollback risk enricher.
func NewRollbackEnricher(g *graph.Graph, idx *index.SymbolIndex) *RollbackEnricher {
	return &RollbackEnricher{
		assessor: NewRollbackRiskAssessor(g, idx),
	}
}

// Name returns the enricher name.
func (e *RollbackEnricher) Name() string {
	return "rollback_risk"
}

// Priority returns execution priority.
func (e *RollbackEnricher) Priority() int {
	return 3
}

// Enrich adds rollback risk information to the blast radius.
func (e *RollbackEnricher) Enrich(ctx context.Context, target *EnrichmentTarget, result *EnhancedBlastRadius) error {
	if ctx == nil {
		return ErrNilContext
	}

	// Default change type - could be extended to detect from symbol kind
	changeType := "function_modified"
	if target.Symbol != nil {
		switch target.Symbol.Kind {
		case ast.SymbolKindFunction, ast.SymbolKindMethod:
			changeType = "function_modified"
		case ast.SymbolKindStruct:
			changeType = "field_modified"
		case ast.SymbolKindInterface:
			changeType = "interface_modified"
		}
	}

	details := make(map[string]string)
	risk, err := e.assessor.AssessChange(ctx, changeType, target.SymbolID, details)
	if err != nil {
		return err
	}

	result.RollbackRisk = risk
	return nil
}

// AnalyzeDiff analyzes a diff for rollback risks.
func (r *RollbackRiskAssessor) AnalyzeDiff(ctx context.Context, diff string) (*RollbackRisk, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	risk := &RollbackRisk{
		Level:                       RollbackRiskLow,
		Score:                       0,
		IrreversibleChanges:         make([]IrreversibleChange, 0),
		HighRiskFactors:             make([]RiskFactor, 0),
		MitigationSteps:             make([]string, 0),
		EstimatedRecoveryComplexity: "SIMPLE",
	}

	// Look for dangerous patterns in diff
	dangerousPatterns := []struct {
		pattern *regexp.Regexp
		change  IrreversibleChangeType
		desc    string
		weight  int
	}{
		{
			regexp.MustCompile(`(?i)DROP\s+TABLE`),
			IrreversibleTableDrop,
			"DROP TABLE detected in diff",
			60,
		},
		{
			regexp.MustCompile(`(?i)DROP\s+COLUMN`),
			IrreversibleColumnDrop,
			"DROP COLUMN detected in diff",
			50,
		},
		{
			regexp.MustCompile(`(?i)DELETE\s+FROM\s+\w+\s*;`),
			IrreversibleDataDelete,
			"Mass DELETE detected in diff",
			40,
		},
		{
			regexp.MustCompile(`(?i)TRUNCATE\s+TABLE`),
			IrreversibleDataDelete,
			"TRUNCATE TABLE detected in diff",
			55,
		},
	}

	for _, dp := range dangerousPatterns {
		if dp.pattern.MatchString(diff) {
			risk.IrreversibleChanges = append(risk.IrreversibleChanges, IrreversibleChange{
				Type:        dp.change,
				Description: dp.desc,
				Location:    "diff",
				Impact:      "May cause permanent data loss",
			})
			risk.HighRiskFactors = append(risk.HighRiskFactors, RiskFactor{
				Factor:      string(dp.change),
				Description: dp.desc,
				Weight:      dp.weight,
			})
		}
	}

	// Check for removed functions/methods
	removedFuncPattern := regexp.MustCompile(`(?m)^-\s*func\s+`)
	if removedFuncPattern.MatchString(diff) {
		risk.HighRiskFactors = append(risk.HighRiskFactors, RiskFactor{
			Factor:      "removed_function",
			Description: "Function removed in diff",
			Weight:      20,
		})
	}

	// Check for removed exported symbols
	removedExportedPattern := regexp.MustCompile(`(?m)^-\s*func\s+[A-Z]|^-\s*type\s+[A-Z]`)
	if removedExportedPattern.MatchString(diff) {
		risk.HighRiskFactors = append(risk.HighRiskFactors, RiskFactor{
			Factor:      "removed_export",
			Description: "Exported symbol removed",
			Weight:      30,
		})
		risk.RequiresAPIVersioning = true
	}

	r.calculateFinalScore(risk)
	return risk, nil
}
