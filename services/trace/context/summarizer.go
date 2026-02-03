// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package context

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// SummarizerConfig configures the summarizer behavior.
type SummarizerConfig struct {
	// RetryConfig configures retry behavior for LLM calls.
	RetryConfig RetryConfig `json:"retry_config"`

	// ConcurrencyConfig configures parallel processing.
	ConcurrencyConfig ConcurrencyConfig `json:"concurrency_config"`

	// CostLimits configures cost thresholds.
	CostLimits CostLimits `json:"cost_limits"`

	// ValidateOutputs enables LLM output validation.
	ValidateOutputs bool `json:"validate_outputs"`

	// FallbackToPartial enables partial summary fallback on LLM failure.
	FallbackToPartial bool `json:"fallback_to_partial"`
}

// DefaultSummarizerConfig returns sensible defaults.
func DefaultSummarizerConfig() SummarizerConfig {
	return SummarizerConfig{
		RetryConfig:       DefaultRetryConfig(),
		ConcurrencyConfig: DefaultConcurrencyConfig(),
		CostLimits:        DefaultCostLimits(),
		ValidateOutputs:   true,
		FallbackToPartial: true,
	}
}

// Summarizer generates hierarchical summaries using an LLM.
//
// Thread Safety: Safe for concurrent use.
type Summarizer struct {
	llm            LLMClient
	cache          *SummaryCache
	validator      *SummaryValidator
	circuitBreaker *CircuitBreaker
	hierarchy      LanguageHierarchy
	costEstimator  *CostEstimator
	config         SummarizerConfig
	workerPool     *WorkerPool
}

// NewSummarizer creates a new summarizer.
//
// Inputs:
//   - llm: The LLM client for generating summaries.
//   - cache: The summary cache for storing results.
//   - hierarchy: The language hierarchy.
//   - config: Configuration options.
//
// Outputs:
//   - *Summarizer: A new summarizer instance.
func NewSummarizer(
	llm LLMClient,
	cache *SummaryCache,
	hierarchy LanguageHierarchy,
	config SummarizerConfig,
) *Summarizer {
	return &Summarizer{
		llm:            llm,
		cache:          cache,
		validator:      NewSummaryValidator(hierarchy),
		circuitBreaker: NewCircuitBreaker(DefaultCircuitBreakerConfig()),
		hierarchy:      hierarchy,
		costEstimator:  NewCostEstimator(DefaultCostConfig(), config.CostLimits),
		config:         config,
		workerPool:     LLMWorkerPool(config.ConcurrencyConfig),
	}
}

// SymbolInfo provides symbol information for summary generation.
type SymbolInfo struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"` // function, type, method, etc.
	Signature string `json:"signature,omitempty"`
	DocString string `json:"doc_string,omitempty"`
}

// PackageInfo provides package information for summary generation.
type PackageInfo struct {
	Path      string       `json:"path"`
	Name      string       `json:"name"`
	Symbols   []SymbolInfo `json:"symbols"`
	Imports   []string     `json:"imports,omitempty"`
	FileCount int          `json:"file_count"`
}

// FileInfo provides file information for summary generation.
type FileInfo struct {
	Path        string       `json:"path"`
	Package     string       `json:"package"`
	Symbols     []SymbolInfo `json:"symbols"`
	LineCount   int          `json:"line_count"`
	ContentHash string       `json:"content_hash"`
}

// ProjectInfo provides project information for summary generation.
type ProjectInfo struct {
	Root     string   `json:"root"`
	Language string   `json:"language"`
	Packages []string `json:"packages"`
}

// GeneratePackageSummary generates a summary for a package.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - info: Package information.
//
// Outputs:
//   - *Summary: The generated summary.
//   - error: Non-nil if generation fails and no fallback available.
func (s *Summarizer) GeneratePackageSummary(ctx context.Context, info *PackageInfo) (*Summary, error) {
	// Check cache first
	if cached, ok := s.cache.Get(info.Path); ok {
		return cached, nil
	}

	// Check circuit breaker
	if !s.circuitBreaker.Allow() {
		return s.handleLLMUnavailable(ctx, info.Path, LevelPackage, info)
	}

	// Build prompt
	prompt := s.buildPackagePrompt(info)

	// Call LLM with retry
	var response *LLMResponse
	_, err := RetryWithCircuitBreaker(ctx, s.circuitBreaker, s.config.RetryConfig, func(ctx context.Context, attempt int) error {
		var callErr error
		response, callErr = s.llm.Complete(ctx, prompt,
			WithLevelTokenLimits(LevelPackage),
			WithTemperature(DefaultLLMTemperature),
		)
		return callErr
	})

	if err != nil {
		return s.handleLLMUnavailable(ctx, info.Path, LevelPackage, info)
	}

	// Parse and validate response
	summary := s.parsePackageResponse(info.Path, response)

	if s.config.ValidateOutputs {
		sourceInfo := &SourceInfo{
			Symbols: make([]string, len(info.Symbols)),
		}
		for i, sym := range info.Symbols {
			sourceInfo.Symbols[i] = sym.Name
		}

		result := s.validator.Validate(summary, sourceInfo)
		if !result.Valid {
			// Log validation issues but still use the summary
			summary.Partial = true
		}
	}

	// Record cost
	s.costEstimator.RecordUsage(response.InputTokens, response.OutputTokens)

	// Cache the result
	summary.UpdatedAt = time.Now()
	s.cache.Set(summary)

	return summary, nil
}

// GenerateFileSummary generates a summary for a file.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - info: File information.
//
// Outputs:
//   - *Summary: The generated summary.
//   - error: Non-nil if generation fails and no fallback available.
func (s *Summarizer) GenerateFileSummary(ctx context.Context, info *FileInfo) (*Summary, error) {
	// Check cache first
	if cached, ok := s.cache.Get(info.Path); ok {
		if !cached.IsStale(info.ContentHash) {
			return cached, nil
		}
	}

	// Check circuit breaker
	if !s.circuitBreaker.Allow() {
		return s.handleFileUnavailable(ctx, info)
	}

	// Build prompt
	prompt := s.buildFilePrompt(info)

	// Call LLM with retry
	var response *LLMResponse
	_, err := RetryWithCircuitBreaker(ctx, s.circuitBreaker, s.config.RetryConfig, func(ctx context.Context, attempt int) error {
		var callErr error
		response, callErr = s.llm.Complete(ctx, prompt,
			WithLevelTokenLimits(LevelFile),
			WithTemperature(DefaultLLMTemperature),
		)
		return callErr
	})

	if err != nil {
		return s.handleFileUnavailable(ctx, info)
	}

	// Parse response
	summary := s.parseFileResponse(info, response)

	// Record cost
	s.costEstimator.RecordUsage(response.InputTokens, response.OutputTokens)

	// Cache the result
	s.cache.Set(summary)

	return summary, nil
}

// GenerateProjectSummary generates a summary for the entire project.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - info: Project information.
//
// Outputs:
//   - *Summary: The generated summary.
//   - error: Non-nil if generation fails.
func (s *Summarizer) GenerateProjectSummary(ctx context.Context, info *ProjectInfo) (*Summary, error) {
	// Check cache first
	if cached, ok := s.cache.Get(info.Root); ok {
		return cached, nil
	}

	// Check circuit breaker
	if !s.circuitBreaker.Allow() {
		return s.generatePartialProjectSummary(info), nil
	}

	// Get package summaries first
	packageSummaries := s.cache.GetByLevel(1)

	// Build prompt with package context
	prompt := s.buildProjectPrompt(info, packageSummaries)

	// Call LLM
	var response *LLMResponse
	_, err := RetryWithCircuitBreaker(ctx, s.circuitBreaker, s.config.RetryConfig, func(ctx context.Context, attempt int) error {
		var callErr error
		response, callErr = s.llm.Complete(ctx, prompt,
			WithLevelTokenLimits(LevelProject),
			WithTemperature(DefaultLLMTemperature),
		)
		return callErr
	})

	if err != nil {
		return s.generatePartialProjectSummary(info), nil
	}

	// Parse response
	summary := s.parseProjectResponse(info, response)

	// Record cost
	s.costEstimator.RecordUsage(response.InputTokens, response.OutputTokens)

	// Cache
	s.cache.Set(summary)

	return summary, nil
}

// GenerationResult contains results from batch summary generation.
type GenerationResult struct {
	Summaries    []*Summary    `json:"summaries"`
	SuccessCount int           `json:"success_count"`
	FailureCount int           `json:"failure_count"`
	PartialCount int           `json:"partial_count"`
	TotalTokens  int           `json:"total_tokens"`
	TotalCost    float64       `json:"total_cost"`
	Duration     time.Duration `json:"duration"`
	Errors       []string      `json:"errors,omitempty"`
}

// GenerateAllPackageSummaries generates summaries for all packages.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - packages: Package information for all packages.
//   - progress: Optional callback for progress updates.
//
// Outputs:
//   - *GenerationResult: Results of the batch operation.
//   - error: Non-nil if completely failed.
func (s *Summarizer) GenerateAllPackageSummaries(
	ctx context.Context,
	packages []*PackageInfo,
	progress ProgressCallback,
) (*GenerationResult, error) {
	start := time.Now()

	// Estimate costs and check limits
	estimate := s.costEstimator.EstimateForLevel(LevelPackage, len(packages))
	if err := s.costEstimator.CheckLimits(&CostEstimate{
		PackageCount:          len(packages),
		EstimatedInputTokens:  estimate.InputTokens,
		EstimatedOutputTokens: estimate.OutputTokens,
		EstimatedTotalTokens:  estimate.InputTokens + estimate.OutputTokens,
		EstimatedCostUSD:      estimate.CostUSD,
	}); err != nil {
		return nil, err
	}

	// Build work items
	items := make([]WorkItem, len(packages))
	summaries := make([]*Summary, len(packages))

	for i, pkg := range packages {
		i, pkg := i, pkg // capture
		items[i] = WorkItem{
			ID: pkg.Path,
			Work: func(ctx context.Context) error {
				summary, err := s.GeneratePackageSummary(ctx, pkg)
				if summary != nil {
					summaries[i] = summary
				}
				return err
			},
		}
	}

	// Process batch
	batchResult := s.workerPool.ProcessBatch(ctx, items, progress)

	// Compile results
	result := &GenerationResult{
		Summaries: make([]*Summary, 0, len(packages)),
		Duration:  time.Since(start),
	}

	for _, summary := range summaries {
		if summary != nil {
			result.Summaries = append(result.Summaries, summary)
			if summary.Partial {
				result.PartialCount++
			} else {
				result.SuccessCount++
			}
			result.TotalTokens += summary.TokensUsed
		}
	}

	result.FailureCount = batchResult.FailureCount

	for _, r := range batchResult.Results {
		if r.Error != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", r.ID, r.Error))
		}
	}

	_, _, result.TotalCost = s.costEstimator.GetUsage()

	return result, nil
}

// EstimateCost estimates the cost for generating summaries.
//
// Inputs:
//   - ctx: Context for cancellation.
//   - projectCount: Number of projects.
//   - packageCount: Number of packages.
//   - fileCount: Number of files.
//
// Outputs:
//   - *CostEstimate: The cost estimate.
func (s *Summarizer) EstimateCost(ctx context.Context, projectCount, packageCount, fileCount int) *CostEstimate {
	return s.costEstimator.EstimateForProject(projectCount, packageCount, fileCount)
}

// handleLLMUnavailable handles the case when LLM is unavailable.
func (s *Summarizer) handleLLMUnavailable(ctx context.Context, id string, level HierarchyLevel, info *PackageInfo) (*Summary, error) {
	if !s.config.FallbackToPartial {
		return nil, ErrLLMUnavailable
	}

	// Try stale cache
	if cached, ok, _ := s.cache.GetStale(id); ok {
		return cached, nil
	}

	// Generate partial summary
	return s.generatePartialPackageSummary(info), nil
}

// handleFileUnavailable handles the case when LLM is unavailable for file summary.
func (s *Summarizer) handleFileUnavailable(ctx context.Context, info *FileInfo) (*Summary, error) {
	if !s.config.FallbackToPartial {
		return nil, ErrLLMUnavailable
	}

	// Try stale cache
	if cached, ok, _ := s.cache.GetStale(info.Path); ok {
		return cached, nil
	}

	// Generate partial summary
	return s.generatePartialFileSummary(info), nil
}

// generatePartialPackageSummary generates a metadata-only summary.
func (s *Summarizer) generatePartialPackageSummary(info *PackageInfo) *Summary {
	var content strings.Builder
	keywords := make([]string, 0)

	content.WriteString(fmt.Sprintf("Package: %s\n", info.Name))

	// Group symbols by type
	types := make([]string, 0)
	functions := make([]string, 0)

	for _, sym := range info.Symbols {
		switch sym.Kind {
		case "type", "struct", "interface":
			types = append(types, sym.Name)
			keywords = append(keywords, sym.Name)
		case "function", "method":
			functions = append(functions, sym.Name)
		}
	}

	if len(types) > 0 {
		content.WriteString("Types: ")
		content.WriteString(strings.Join(types, ", "))
		content.WriteString("\n")
	}

	if len(functions) > 0 {
		content.WriteString("Functions: ")
		content.WriteString(strings.Join(functions[:min(10, len(functions))], ", "))
		if len(functions) > 10 {
			content.WriteString(fmt.Sprintf(" (+%d more)", len(functions)-10))
		}
		content.WriteString("\n")
	}

	return &Summary{
		ID:        info.Path,
		Level:     1,
		Content:   content.String(),
		Keywords:  keywords,
		Partial:   true,
		Language:  s.hierarchy.Language(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// generatePartialFileSummary generates a metadata-only file summary.
func (s *Summarizer) generatePartialFileSummary(info *FileInfo) *Summary {
	var content strings.Builder
	keywords := make([]string, 0)

	content.WriteString(fmt.Sprintf("File: %s (in package %s)\n", info.Path, info.Package))
	content.WriteString(fmt.Sprintf("Contains %d symbols, %d lines\n", len(info.Symbols), info.LineCount))

	for _, sym := range info.Symbols {
		keywords = append(keywords, sym.Name)
	}

	parentID, _ := s.hierarchy.ParentOf(info.Path)

	return &Summary{
		ID:        info.Path,
		Level:     2,
		Content:   content.String(),
		Keywords:  keywords,
		ParentID:  parentID,
		Hash:      info.ContentHash,
		Partial:   true,
		Language:  s.hierarchy.Language(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// generatePartialProjectSummary generates a metadata-only project summary.
func (s *Summarizer) generatePartialProjectSummary(info *ProjectInfo) *Summary {
	var content strings.Builder

	content.WriteString(fmt.Sprintf("Project: %s\n", info.Root))
	content.WriteString(fmt.Sprintf("Language: %s\n", info.Language))
	content.WriteString(fmt.Sprintf("Contains %d packages\n", len(info.Packages)))

	return &Summary{
		ID:        "",
		Level:     0,
		Content:   content.String(),
		Keywords:  []string{info.Language},
		Children:  info.Packages,
		Partial:   true,
		Language:  info.Language,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// Prompt builders

func (s *Summarizer) buildPackagePrompt(info *PackageInfo) string {
	var sb strings.Builder

	sb.WriteString("Summarize this Go package for a code search index.\n\n")
	sb.WriteString(fmt.Sprintf("Package: %s\n\n", info.Path))

	// Types
	sb.WriteString("Types defined:\n")
	for _, sym := range info.Symbols {
		if sym.Kind == "type" || sym.Kind == "struct" || sym.Kind == "interface" {
			sb.WriteString(fmt.Sprintf("- %s (%s)\n", sym.Name, sym.Kind))
		}
	}

	// Functions
	sb.WriteString("\nFunctions defined:\n")
	for _, sym := range info.Symbols {
		if sym.Kind == "function" || sym.Kind == "method" {
			sb.WriteString(fmt.Sprintf("- %s\n", sym.Name))
		}
	}

	// Imports
	if len(info.Imports) > 0 {
		sb.WriteString("\nImports:\n")
		for _, imp := range info.Imports[:min(10, len(info.Imports))] {
			sb.WriteString(fmt.Sprintf("- %s\n", imp))
		}
	}

	sb.WriteString("\nGenerate a 2-3 sentence summary covering:\n")
	sb.WriteString("1. What this package does\n")
	sb.WriteString("2. Key types/interfaces\n")
	sb.WriteString("3. Key functions\n\n")
	sb.WriteString("Also extract 5-10 keywords for search matching.\n")
	sb.WriteString("Format: SUMMARY: <your summary>\\nKEYWORDS: word1, word2, word3...")

	return sb.String()
}

func (s *Summarizer) buildFilePrompt(info *FileInfo) string {
	var sb strings.Builder

	sb.WriteString("Summarize this source file for a code search index.\n\n")
	sb.WriteString(fmt.Sprintf("File: %s\n", info.Path))
	sb.WriteString(fmt.Sprintf("Package: %s\n\n", info.Package))

	sb.WriteString("Contents:\n")
	for _, sym := range info.Symbols {
		sb.WriteString(fmt.Sprintf("- %s (%s)\n", sym.Name, sym.Kind))
	}

	sb.WriteString("\nGenerate a 1-2 sentence summary covering:\n")
	sb.WriteString("1. Purpose of this file\n")
	sb.WriteString("2. Main symbols it defines\n\n")
	sb.WriteString("Also extract 3-5 keywords.\n")
	sb.WriteString("Format: SUMMARY: <your summary>\\nKEYWORDS: word1, word2, word3...")

	return sb.String()
}

func (s *Summarizer) buildProjectPrompt(info *ProjectInfo, packageSummaries []*Summary) string {
	var sb strings.Builder

	sb.WriteString("Summarize this codebase for a high-level overview.\n\n")
	sb.WriteString(fmt.Sprintf("Project root: %s\n", info.Root))
	sb.WriteString(fmt.Sprintf("Language: %s\n", info.Language))
	sb.WriteString(fmt.Sprintf("Packages: %d\n\n", len(info.Packages)))

	sb.WriteString("Package summaries:\n")
	for _, pkg := range packageSummaries[:min(20, len(packageSummaries))] {
		sb.WriteString(fmt.Sprintf("- %s: %s\n", pkg.ID, truncate(pkg.Content, 100)))
	}

	sb.WriteString("\nGenerate a 3-5 sentence overview covering:\n")
	sb.WriteString("1. What this project does\n")
	sb.WriteString("2. Main components/packages\n")
	sb.WriteString("3. Architecture/structure\n\n")
	sb.WriteString("Format: SUMMARY: <your summary>\\nKEYWORDS: word1, word2...")

	return sb.String()
}

// Response parsers

func (s *Summarizer) parsePackageResponse(pkgPath string, response *LLMResponse) *Summary {
	content, keywords := parsePromptResponse(response.Content)

	parentID, _ := s.hierarchy.ParentOf(pkgPath)

	return &Summary{
		ID:         pkgPath,
		Level:      1,
		Content:    content,
		Keywords:   keywords,
		ParentID:   parentID,
		TokensUsed: response.TokensUsed,
		Language:   s.hierarchy.Language(),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

func (s *Summarizer) parseFileResponse(info *FileInfo, response *LLMResponse) *Summary {
	content, keywords := parsePromptResponse(response.Content)

	parentID, _ := s.hierarchy.ParentOf(info.Path)

	return &Summary{
		ID:         info.Path,
		Level:      2,
		Content:    content,
		Keywords:   keywords,
		ParentID:   parentID,
		Hash:       info.ContentHash,
		TokensUsed: response.TokensUsed,
		Language:   s.hierarchy.Language(),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

func (s *Summarizer) parseProjectResponse(info *ProjectInfo, response *LLMResponse) *Summary {
	content, keywords := parsePromptResponse(response.Content)

	return &Summary{
		ID:         "",
		Level:      0,
		Content:    content,
		Keywords:   keywords,
		Children:   info.Packages,
		TokensUsed: response.TokensUsed,
		Language:   info.Language,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

// parsePromptResponse extracts content and keywords from LLM response.
func parsePromptResponse(response string) (content string, keywords []string) {
	lines := strings.Split(response, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(strings.ToUpper(line), "SUMMARY:") {
			content = strings.TrimSpace(strings.TrimPrefix(line, "SUMMARY:"))
			content = strings.TrimPrefix(content, "summary:")
		} else if strings.HasPrefix(strings.ToUpper(line), "KEYWORDS:") {
			kwStr := strings.TrimSpace(strings.TrimPrefix(line, "KEYWORDS:"))
			kwStr = strings.TrimPrefix(kwStr, "keywords:")
			for _, kw := range strings.Split(kwStr, ",") {
				kw = strings.TrimSpace(kw)
				if kw != "" {
					keywords = append(keywords, kw)
				}
			}
		}
	}

	// If no structured response, use the whole thing as content
	if content == "" {
		content = response
	}

	return content, keywords
}

// Helper functions

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
