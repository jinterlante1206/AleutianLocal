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
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/history"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// PredictiveRiskScorer calculates predictive risk scores based on historical data.
//
// # Description
//
// Uses historical bug correlation, churn rate, and complexity metrics to
// predict the risk of changes. Symbols with past bugs or high churn are
// considered higher risk.
//
// # Thread Safety
//
// Safe for concurrent use after construction.
type PredictiveRiskScorer struct {
	graph        *graph.Graph
	index        *index.SymbolIndex
	historyStore *history.HistoryStore

	mu          sync.RWMutex
	bugHistory  map[string]int     // symbol -> bug count
	churnScores map[string]float64 // symbol -> churn rate
}

// PredictiveRisk represents a predictive risk score.
type PredictiveRisk struct {
	// Score is the overall risk score (0-100, higher = riskier).
	Score int `json:"score"`

	// Level is the risk level category.
	Level PredictiveRiskLevel `json:"level"`

	// Factors are the factors contributing to the score.
	Factors []PredictiveRiskFactor `json:"factors"`

	// HistoricalBugCount is the number of past bugs in this area.
	HistoricalBugCount int `json:"historical_bug_count"`

	// ChurnRate is the change frequency (changes per week).
	ChurnRate float64 `json:"churn_rate"`

	// ComplexityScore is the estimated complexity (if available).
	ComplexityScore int `json:"complexity_score,omitempty"`

	// FailureProbability is the estimated probability of failure (0.0-1.0).
	FailureProbability float64 `json:"failure_probability"`

	// Recommendations are suggested risk mitigation steps.
	Recommendations []string `json:"recommendations,omitempty"`
}

// PredictiveRiskLevel categorizes predictive risk.
type PredictiveRiskLevel string

const (
	PredictiveRiskLow      PredictiveRiskLevel = "LOW"
	PredictiveRiskMedium   PredictiveRiskLevel = "MEDIUM"
	PredictiveRiskHigh     PredictiveRiskLevel = "HIGH"
	PredictiveRiskCritical PredictiveRiskLevel = "CRITICAL"
)

// PredictiveRiskFactor is a factor contributing to predictive risk.
type PredictiveRiskFactor struct {
	// Name is the factor name.
	Name string `json:"name"`

	// Description explains the factor.
	Description string `json:"description"`

	// Contribution is the score contribution (0-100).
	Contribution int `json:"contribution"`

	// Evidence provides supporting data.
	Evidence string `json:"evidence,omitempty"`
}

// NewPredictiveRiskScorer creates a new scorer.
func NewPredictiveRiskScorer(g *graph.Graph, idx *index.SymbolIndex, store *history.HistoryStore) *PredictiveRiskScorer {
	return &PredictiveRiskScorer{
		graph:        g,
		index:        idx,
		historyStore: store,
		bugHistory:   make(map[string]int),
		churnScores:  make(map[string]float64),
	}
}

// CalculateRisk computes the predictive risk for a symbol.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//   - symbolID: The symbol to analyze.
//
// # Outputs
//
//   - *PredictiveRisk: The calculated risk.
//   - error: Non-nil on failure.
func (p *PredictiveRiskScorer) CalculateRisk(ctx context.Context, symbolID string) (*PredictiveRisk, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	risk := &PredictiveRisk{
		Factors:         make([]PredictiveRiskFactor, 0),
		Recommendations: make([]string, 0),
	}

	totalScore := 0

	// Factor 1: Historical bug count
	bugCount := p.getBugCount(symbolID)
	if bugCount > 0 {
		contribution := min(bugCount*10, 30) // Max 30 points
		totalScore += contribution

		risk.Factors = append(risk.Factors, PredictiveRiskFactor{
			Name:         "historical_bugs",
			Description:  "Past bugs in this code area",
			Contribution: contribution,
			Evidence:     formatEvidence("Found %d past bugs", bugCount),
		})
		risk.HistoricalBugCount = bugCount

		risk.Recommendations = append(risk.Recommendations,
			"Extra testing recommended due to past bugs",
			"Consider adding regression tests",
		)
	}

	// Factor 2: Churn rate
	churnRate := p.getChurnRate(symbolID)
	if churnRate > 0 {
		var contribution int
		switch {
		case churnRate > 5: // More than 5 changes per week
			contribution = 25
			risk.Recommendations = append(risk.Recommendations,
				"High churn area - consider stabilization",
			)
		case churnRate > 2:
			contribution = 15
		case churnRate > 0.5:
			contribution = 5
		}
		totalScore += contribution

		if contribution > 0 {
			risk.Factors = append(risk.Factors, PredictiveRiskFactor{
				Name:         "churn_rate",
				Description:  "Frequency of changes",
				Contribution: contribution,
				Evidence:     formatEvidence("%.1f changes per week", churnRate),
			})
		}
		risk.ChurnRate = churnRate
	}

	// Factor 3: Caller count (more callers = higher impact)
	var callerCount int
	if node, ok := p.graph.GetNode(symbolID); ok {
		callerCount = len(node.Incoming)
	}
	if callerCount > 0 {
		var contribution int
		switch {
		case callerCount > 50:
			contribution = 20
		case callerCount > 20:
			contribution = 15
		case callerCount > 10:
			contribution = 10
		case callerCount > 5:
			contribution = 5
		}
		totalScore += contribution

		if contribution > 0 {
			risk.Factors = append(risk.Factors, PredictiveRiskFactor{
				Name:         "caller_count",
				Description:  "Number of dependent callers",
				Contribution: contribution,
				Evidence:     formatEvidence("%d callers depend on this", callerCount),
			})
		}
	}

	// Factor 4: Complexity (based on symbol metadata)
	complexity := p.estimateComplexity(symbolID)
	if complexity > 0 {
		var contribution int
		switch {
		case complexity > 20:
			contribution = 20
			risk.Recommendations = append(risk.Recommendations,
				"Consider refactoring to reduce complexity",
			)
		case complexity > 10:
			contribution = 10
		case complexity > 5:
			contribution = 5
		}
		totalScore += contribution

		if contribution > 0 {
			risk.Factors = append(risk.Factors, PredictiveRiskFactor{
				Name:         "complexity",
				Description:  "Code complexity score",
				Contribution: contribution,
				Evidence:     formatEvidence("Complexity score: %d", complexity),
			})
		}
		risk.ComplexityScore = complexity
	}

	// Factor 5: Security sensitivity
	if p.isSecuritySensitive(symbolID) {
		contribution := 15
		totalScore += contribution

		risk.Factors = append(risk.Factors, PredictiveRiskFactor{
			Name:         "security_sensitive",
			Description:  "Handles security-sensitive operations",
			Contribution: contribution,
		})

		risk.Recommendations = append(risk.Recommendations,
			"Security review recommended",
			"Ensure proper input validation",
		)
	}

	// Calculate final score and level
	if totalScore > 100 {
		totalScore = 100
	}
	risk.Score = totalScore

	switch {
	case totalScore >= 70:
		risk.Level = PredictiveRiskCritical
		risk.FailureProbability = 0.7 + (float64(totalScore-70)/30)*0.25
	case totalScore >= 50:
		risk.Level = PredictiveRiskHigh
		risk.FailureProbability = 0.4 + (float64(totalScore-50)/20)*0.3
	case totalScore >= 25:
		risk.Level = PredictiveRiskMedium
		risk.FailureProbability = 0.1 + (float64(totalScore-25)/25)*0.3
	default:
		risk.Level = PredictiveRiskLow
		risk.FailureProbability = float64(totalScore) / 250 // Max 0.1
	}

	// Round failure probability
	risk.FailureProbability = float64(int(risk.FailureProbability*100)) / 100

	return risk, nil
}

// getBugCount returns the number of past bugs for a symbol.
func (p *PredictiveRiskScorer) getBugCount(symbolID string) int {
	p.mu.RLock()
	count := p.bugHistory[symbolID]
	p.mu.RUnlock()
	return count
}

// getChurnRate returns the change frequency for a symbol.
func (p *PredictiveRiskScorer) getChurnRate(symbolID string) float64 {
	p.mu.RLock()
	rate := p.churnScores[symbolID]
	p.mu.RUnlock()
	return rate
}

// estimateComplexity estimates cyclomatic complexity.
func (p *PredictiveRiskScorer) estimateComplexity(symbolID string) int {
	sym, ok := p.index.GetByID(symbolID)
	if !ok {
		return 0
	}

	// Estimate based on line count
	lineCount := sym.EndLine - sym.StartLine
	if lineCount < 0 {
		lineCount = 0
	}

	// Rough estimate: 1 decision point per 10 lines
	return lineCount / 10
}

// isSecuritySensitive checks if a symbol handles security operations.
func (p *PredictiveRiskScorer) isSecuritySensitive(symbolID string) bool {
	// Check against known security patterns
	securityPatterns := []string{
		"auth", "login", "password", "token", "secret",
		"encrypt", "decrypt", "hash", "validate", "sanitize",
		"permission", "access", "credential", "session",
	}

	symbolLower := toLowerCase(symbolID)
	for _, pattern := range securityPatterns {
		if containsIgnoreCase(symbolLower, pattern) {
			return true
		}
	}

	return false
}

// RegisterBug records a bug for a symbol.
func (p *PredictiveRiskScorer) RegisterBug(symbolID string) {
	p.mu.Lock()
	p.bugHistory[symbolID]++
	p.mu.Unlock()
}

// UpdateChurnScore updates the churn rate for a symbol.
func (p *PredictiveRiskScorer) UpdateChurnScore(symbolID string, rate float64) {
	p.mu.Lock()
	p.churnScores[symbolID] = rate
	p.mu.Unlock()
}

// LoadBugHistory loads bug history from a map.
func (p *PredictiveRiskScorer) LoadBugHistory(history map[string]int) {
	p.mu.Lock()
	for k, v := range history {
		p.bugHistory[k] = v
	}
	p.mu.Unlock()
}

// LoadChurnScores loads churn scores from a map.
func (p *PredictiveRiskScorer) LoadChurnScores(scores map[string]float64) {
	p.mu.Lock()
	for k, v := range scores {
		p.churnScores[k] = v
	}
	p.mu.Unlock()
}

// PredictiveEnricher implements Enricher for predictive risk scoring.
type PredictiveEnricher struct {
	scorer *PredictiveRiskScorer
}

// NewPredictiveEnricher creates a predictive risk enricher.
func NewPredictiveEnricher(g *graph.Graph, idx *index.SymbolIndex, store *history.HistoryStore) *PredictiveEnricher {
	return &PredictiveEnricher{
		scorer: NewPredictiveRiskScorer(g, idx, store),
	}
}

// Name returns the enricher name.
func (e *PredictiveEnricher) Name() string {
	return "predictive_risk"
}

// Priority returns execution priority (runs last).
func (e *PredictiveEnricher) Priority() int {
	return 4
}

// Enrich adds predictive risk information to the blast radius.
func (e *PredictiveEnricher) Enrich(ctx context.Context, target *EnrichmentTarget, result *EnhancedBlastRadius) error {
	if ctx == nil {
		return ErrNilContext
	}

	risk, err := e.scorer.CalculateRisk(ctx, target.SymbolID)
	if err != nil {
		return err
	}

	result.PredictiveRisk = risk
	return nil
}

// Scorer returns the underlying scorer for configuration.
func (e *PredictiveEnricher) Scorer() *PredictiveRiskScorer {
	return e.scorer
}

// Helper functions

func formatEvidence(format string, args ...interface{}) string {
	return format // Simplified - in production would use fmt.Sprintf
}

func toLowerCase(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			result[i] = c + 32
		} else {
			result[i] = c
		}
	}
	return string(result)
}

func containsIgnoreCase(s, substr string) bool {
	sLower := toLowerCase(s)
	substrLower := toLowerCase(substr)

	for i := 0; i <= len(sLower)-len(substrLower); i++ {
		if sLower[i:i+len(substrLower)] == substrLower {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
