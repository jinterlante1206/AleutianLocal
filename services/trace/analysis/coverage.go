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
	"bufio"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// MaxCoverageFileSize is the maximum size of coverage files to parse.
const MaxCoverageFileSize = 50 * 1024 * 1024 // 50MB

// CoverageCorrelator parses coverage files and maps coverage to symbols.
//
// # Description
//
// Parses lcov and cobertura format coverage files and correlates the
// line coverage data with symbols from the index. Provides methods to
// query coverage for specific symbols and find untested code paths.
//
// # Thread Safety
//
// Safe for concurrent use after construction.
type CoverageCorrelator struct {
	coverageData map[string]*fileCoverage // file path -> coverage
	symbolIndex  *index.SymbolIndex
	mu           sync.RWMutex
	totalFiles   int
	totalLines   int
	coveredLines int
}

// fileCoverage tracks coverage for a single file.
type fileCoverage struct {
	FilePath       string
	LineCoverage   map[int]int // line number -> hit count
	BranchCoverage map[int]branchInfo
	FunctionHits   map[string]int // function name -> hit count
}

// branchInfo tracks branch coverage for a line.
type branchInfo struct {
	Total int
	Taken int
}

// CoverageInfo contains coverage data for a symbol.
type CoverageInfo struct {
	// SymbolID is the symbol this coverage applies to.
	SymbolID string `json:"symbol_id"`

	// LineCoverage is the percentage of lines covered (0.0-1.0).
	LineCoverage float64 `json:"line_coverage"`

	// BranchCoverage is the percentage of branches covered (0.0-1.0).
	BranchCoverage float64 `json:"branch_coverage"`

	// CoveredLines is the number of lines with coverage.
	CoveredLines int `json:"covered_lines"`

	// TotalLines is the total number of lines.
	TotalLines int `json:"total_lines"`

	// CoveredBy is the list of test functions that cover this symbol.
	CoveredBy []string `json:"covered_by,omitempty"`
}

// CoverageRisk indicates the coverage risk level.
type CoverageRisk string

const (
	// CoverageRiskHighUntested indicates most code is untested.
	CoverageRiskHighUntested CoverageRisk = "HIGH_UNTESTED"

	// CoverageRiskPartial indicates partial test coverage.
	CoverageRiskPartial CoverageRisk = "PARTIAL"

	// CoverageRiskCovered indicates good test coverage.
	CoverageRiskCovered CoverageRisk = "COVERED"
)

// NewCoverageCorrelator creates a new correlator from a coverage file.
//
// # Description
//
// Parses the coverage file and creates an internal representation for
// fast symbol lookup. Supports lcov (.info) and cobertura (.xml) formats.
//
// # Inputs
//
//   - coveragePath: Path to the coverage file. Must be < 50MB.
//   - idx: Symbol index for mapping lines to symbols.
//
// # Outputs
//
//   - *CoverageCorrelator: Ready-to-use correlator.
//   - error: Non-nil if parsing failed.
func NewCoverageCorrelator(coveragePath string, idx *index.SymbolIndex) (*CoverageCorrelator, error) {
	if coveragePath == "" {
		return nil, errors.New("coverage path is required")
	}
	if idx == nil {
		return nil, errors.New("symbol index is required")
	}

	// Check file size
	info, err := os.Stat(coveragePath)
	if err != nil {
		return nil, fmt.Errorf("stat coverage file: %w", err)
	}
	if info.Size() > MaxCoverageFileSize {
		return nil, fmt.Errorf("coverage file too large: %d bytes (max %d)", info.Size(), MaxCoverageFileSize)
	}

	c := &CoverageCorrelator{
		coverageData: make(map[string]*fileCoverage),
		symbolIndex:  idx,
	}

	// Detect format and parse
	ext := strings.ToLower(filepath.Ext(coveragePath))
	switch ext {
	case ".info":
		if err := c.parseLcov(coveragePath); err != nil {
			return nil, fmt.Errorf("parse lcov: %w", err)
		}
	case ".xml":
		if err := c.parseCobertura(coveragePath); err != nil {
			return nil, fmt.Errorf("parse cobertura: %w", err)
		}
	default:
		// Try to detect from content
		if err := c.parseAuto(coveragePath); err != nil {
			return nil, fmt.Errorf("parse coverage: %w", err)
		}
	}

	return c, nil
}

// parseLcov parses lcov format coverage files.
func (c *CoverageCorrelator) parseLcov(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Increase buffer for long lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var current *fileCoverage

	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, "SF:"):
			// Source file
			filePath := strings.TrimPrefix(line, "SF:")
			current = &fileCoverage{
				FilePath:       filePath,
				LineCoverage:   make(map[int]int),
				BranchCoverage: make(map[int]branchInfo),
				FunctionHits:   make(map[string]int),
			}
			c.coverageData[filePath] = current
			c.totalFiles++

		case strings.HasPrefix(line, "DA:"):
			// Line coverage: DA:line_number,hit_count
			if current == nil {
				continue
			}
			parts := strings.Split(strings.TrimPrefix(line, "DA:"), ",")
			if len(parts) >= 2 {
				lineNum, _ := strconv.Atoi(parts[0])
				hits, _ := strconv.Atoi(parts[1])
				current.LineCoverage[lineNum] = hits
				c.totalLines++
				if hits > 0 {
					c.coveredLines++
				}
			}

		case strings.HasPrefix(line, "BRDA:"):
			// Branch coverage: BRDA:line,block,branch,taken
			if current == nil {
				continue
			}
			parts := strings.Split(strings.TrimPrefix(line, "BRDA:"), ",")
			if len(parts) >= 4 {
				lineNum, _ := strconv.Atoi(parts[0])
				taken := 0
				if parts[3] != "-" {
					taken, _ = strconv.Atoi(parts[3])
				}
				bi := current.BranchCoverage[lineNum]
				bi.Total++
				if taken > 0 {
					bi.Taken++
				}
				current.BranchCoverage[lineNum] = bi
			}

		case strings.HasPrefix(line, "FN:"):
			// Function: FN:line,function_name
			// We track this but FNDA has the hit count

		case strings.HasPrefix(line, "FNDA:"):
			// Function hit count: FNDA:hit_count,function_name
			if current == nil {
				continue
			}
			parts := strings.Split(strings.TrimPrefix(line, "FNDA:"), ",")
			if len(parts) >= 2 {
				hits, _ := strconv.Atoi(parts[0])
				funcName := parts[1]
				current.FunctionHits[funcName] = hits
			}

		case line == "end_of_record":
			current = nil
		}
	}

	return scanner.Err()
}

// parseCobertura parses cobertura XML format coverage files.
func (c *CoverageCorrelator) parseCobertura(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Use a limited reader to enforce size limit
	limited := io.LimitReader(f, MaxCoverageFileSize)

	decoder := xml.NewDecoder(limited)

	// Cobertura structure
	type Line struct {
		Number            int    `xml:"number,attr"`
		Hits              int    `xml:"hits,attr"`
		Branch            bool   `xml:"branch,attr"`
		ConditionCoverage string `xml:"condition-coverage,attr"`
	}

	type Method struct {
		Name string `xml:"name,attr"`
		Line int    `xml:"line,attr"`
		Hits int    `xml:"hits,attr"`
	}

	type Class struct {
		Name     string   `xml:"name,attr"`
		Filename string   `xml:"filename,attr"`
		Lines    []Line   `xml:"lines>line"`
		Methods  []Method `xml:"methods>method"`
	}

	type Package struct {
		Name    string  `xml:"name,attr"`
		Classes []Class `xml:"classes>class"`
	}

	type Coverage struct {
		Packages []Package `xml:"packages>package"`
	}

	var cov Coverage
	if err := decoder.Decode(&cov); err != nil {
		return fmt.Errorf("decode cobertura XML: %w", err)
	}

	for _, pkg := range cov.Packages {
		for _, cls := range pkg.Classes {
			fc := &fileCoverage{
				FilePath:       cls.Filename,
				LineCoverage:   make(map[int]int),
				BranchCoverage: make(map[int]branchInfo),
				FunctionHits:   make(map[string]int),
			}

			for _, line := range cls.Lines {
				fc.LineCoverage[line.Number] = line.Hits
				c.totalLines++
				if line.Hits > 0 {
					c.coveredLines++
				}

				if line.Branch && line.ConditionCoverage != "" {
					// Parse "50% (1/2)" format
					bi := parseBranchCoverage(line.ConditionCoverage)
					fc.BranchCoverage[line.Number] = bi
				}
			}

			for _, method := range cls.Methods {
				fc.FunctionHits[method.Name] = method.Hits
			}

			c.coverageData[cls.Filename] = fc
			c.totalFiles++
		}
	}

	return nil
}

// parseBranchCoverage parses cobertura condition-coverage attribute.
func parseBranchCoverage(s string) branchInfo {
	// Format: "50% (1/2)"
	var bi branchInfo
	parts := strings.Split(s, " ")
	if len(parts) >= 2 {
		// Parse (taken/total)
		inner := strings.Trim(parts[1], "()")
		nums := strings.Split(inner, "/")
		if len(nums) >= 2 {
			bi.Taken, _ = strconv.Atoi(nums[0])
			bi.Total, _ = strconv.Atoi(nums[1])
		}
	}
	return bi
}

// parseAuto tries to detect format from file content.
func (c *CoverageCorrelator) parseAuto(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Read first few bytes to detect format
	header := make([]byte, 100)
	n, err := f.Read(header)
	if err != nil && err != io.EOF {
		return err
	}
	header = header[:n]

	headerStr := string(header)

	// Check for XML
	if strings.Contains(headerStr, "<?xml") || strings.Contains(headerStr, "<coverage") {
		return c.parseCobertura(path)
	}

	// Assume lcov
	return c.parseLcov(path)
}

// GetCoverage returns coverage info for a specific symbol.
//
// # Inputs
//
//   - symbolID: The symbol to get coverage for.
//
// # Outputs
//
//   - *CoverageInfo: Coverage data for the symbol.
//   - error: Non-nil if symbol not found or no coverage data.
func (c *CoverageCorrelator) GetCoverage(symbolID string) (*CoverageInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Look up symbol in index
	sym, ok := c.symbolIndex.GetByID(symbolID)
	if !ok {
		return nil, fmt.Errorf("symbol not found: %s", symbolID)
	}

	// Find coverage for this file
	fc, ok := c.coverageData[sym.FilePath]
	if !ok {
		// Try with different path formats
		fc = c.findCoverageForFile(sym.FilePath)
		if fc == nil {
			return &CoverageInfo{
				SymbolID:     symbolID,
				LineCoverage: 0,
			}, nil
		}
	}

	// Calculate coverage for symbol's line range
	startLine := sym.StartLine
	endLine := sym.EndLine
	if endLine == 0 {
		endLine = startLine
	}

	covered := 0
	total := 0
	branchCovered := 0
	branchTotal := 0

	for line := startLine; line <= endLine; line++ {
		if hits, ok := fc.LineCoverage[line]; ok {
			total++
			if hits > 0 {
				covered++
			}
		}

		if bi, ok := fc.BranchCoverage[line]; ok {
			branchTotal += bi.Total
			branchCovered += bi.Taken
		}
	}

	info := &CoverageInfo{
		SymbolID:     symbolID,
		CoveredLines: covered,
		TotalLines:   total,
	}

	if total > 0 {
		info.LineCoverage = float64(covered) / float64(total)
	}
	if branchTotal > 0 {
		info.BranchCoverage = float64(branchCovered) / float64(branchTotal)
	}

	return info, nil
}

// findCoverageForFile tries different path formats to find coverage.
func (c *CoverageCorrelator) findCoverageForFile(filePath string) *fileCoverage {
	// Try exact match first
	if fc, ok := c.coverageData[filePath]; ok {
		return fc
	}

	// Try basename
	base := filepath.Base(filePath)
	for path, fc := range c.coverageData {
		if filepath.Base(path) == base {
			return fc
		}
	}

	// Try suffix match
	for path, fc := range c.coverageData {
		if strings.HasSuffix(path, filePath) || strings.HasSuffix(filePath, path) {
			return fc
		}
	}

	return nil
}

// GetUntestedBlastRadius returns symbols in the blast radius that lack coverage.
//
// # Inputs
//
//   - br: The blast radius to analyze.
//
// # Outputs
//
//   - []string: Symbol IDs that are untested.
func (c *CoverageCorrelator) GetUntestedBlastRadius(br *BlastRadius) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var untested []string

	for _, caller := range br.DirectCallers {
		info, err := c.GetCoverage(caller.ID)
		if err != nil || info.LineCoverage < 0.5 {
			untested = append(untested, caller.ID)
		}
	}

	return untested
}

// GetUntestedEnhancedBlastRadius returns untested symbols from enhanced result.
func (c *CoverageCorrelator) GetUntestedEnhancedBlastRadius(br *EnhancedBlastRadius) []Caller {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var untested []Caller

	for _, caller := range br.DirectCallers {
		info, err := c.GetCoverage(caller.ID)
		if err != nil || info.LineCoverage < 0.5 {
			untested = append(untested, caller)
		}
	}

	return untested
}

// CalculateCoverageRisk determines the coverage risk level.
func (c *CoverageCorrelator) CalculateCoverageRisk(br *EnhancedBlastRadius) CoverageRisk {
	if len(br.DirectCallers) == 0 {
		return CoverageRiskCovered
	}

	untested := c.GetUntestedEnhancedBlastRadius(br)
	untestedRatio := float64(len(untested)) / float64(len(br.DirectCallers))

	switch {
	case untestedRatio > 0.5:
		return CoverageRiskHighUntested
	case untestedRatio > 0.0:
		return CoverageRiskPartial
	default:
		return CoverageRiskCovered
	}
}

// Stats returns coverage statistics.
type CoverageStats struct {
	TotalFiles   int     `json:"total_files"`
	TotalLines   int     `json:"total_lines"`
	CoveredLines int     `json:"covered_lines"`
	Coverage     float64 `json:"coverage"`
}

// Stats returns overall coverage statistics.
func (c *CoverageCorrelator) Stats() CoverageStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	coverage := 0.0
	if c.totalLines > 0 {
		coverage = float64(c.coveredLines) / float64(c.totalLines)
	}

	return CoverageStats{
		TotalFiles:   c.totalFiles,
		TotalLines:   c.totalLines,
		CoveredLines: c.coveredLines,
		Coverage:     coverage,
	}
}

// CoverageEnricher implements Enricher for test coverage correlation.
type CoverageEnricher struct {
	correlator *CoverageCorrelator
	mu         sync.RWMutex
}

// NewCoverageEnricher creates an enricher for coverage correlation.
func NewCoverageEnricher(coveragePath string, idx *index.SymbolIndex) (*CoverageEnricher, error) {
	correlator, err := NewCoverageCorrelator(coveragePath, idx)
	if err != nil {
		return nil, err
	}
	return &CoverageEnricher{
		correlator: correlator,
	}, nil
}

// Name returns the enricher name.
func (e *CoverageEnricher) Name() string {
	return "coverage"
}

// Priority returns execution priority.
func (e *CoverageEnricher) Priority() int {
	return 2
}

// Enrich adds coverage information to the blast radius.
func (e *CoverageEnricher) Enrich(ctx context.Context, target *EnrichmentTarget, result *EnhancedBlastRadius) error {
	if ctx == nil {
		return ErrNilContext
	}

	// Get coverage for target symbol
	info, _ := e.correlator.GetCoverage(target.SymbolID)
	if info != nil {
		result.Coverage = info
	}

	// Find untested callers
	result.UntestedCallers = e.correlator.GetUntestedEnhancedBlastRadius(result)

	// Calculate coverage risk
	result.CoverageRisk = string(e.correlator.CalculateCoverageRisk(result))

	return nil
}
