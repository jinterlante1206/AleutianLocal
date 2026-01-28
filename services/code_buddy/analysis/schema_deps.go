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
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

// SchemaDepTracker tracks database schema dependencies in code.
//
// # Description
//
// Detects SQL queries, ORM operations, and schema references in source code.
// Maps these to symbols to understand the database blast radius of changes.
//
// # Limitations
//
// SQL detection via regex has false-negative risk for:
//   - Dynamic SQL construction
//   - Heavily templated queries
//   - Non-standard SQL dialects
//
// # Thread Safety
//
// Safe for concurrent use after construction.
type SchemaDepTracker struct {
	projectRoot string
	codeGraph   *graph.Graph
	symbolIndex *index.SymbolIndex

	mu           sync.RWMutex
	dependencies []SchemaDependency
	tableUsages  map[string][]SchemaDependency // table -> usages
	symbolDeps   map[string][]SchemaDependency // symbol -> deps
}

// SchemaDependency represents a database dependency from code.
type SchemaDependency struct {
	// Table is the database table name.
	Table string `json:"table"`

	// Columns are specific columns referenced (if detectable).
	Columns []string `json:"columns,omitempty"`

	// Operation is the SQL operation type.
	Operation SQLOperation `json:"operation"`

	// SourceSymbol is the symbol containing this dependency.
	SourceSymbol string `json:"source_symbol"`

	// FilePath is the file containing the dependency.
	FilePath string `json:"file_path"`

	// Line is the line number of the dependency.
	Line int `json:"line"`

	// RawQuery is the detected SQL snippet (truncated for safety).
	RawQuery string `json:"raw_query,omitempty"`

	// ORM is the ORM framework detected (if any).
	ORM string `json:"orm,omitempty"`

	// Confidence is how confident we are in this detection (0-100).
	Confidence int `json:"confidence"`
}

// SQLOperation represents a type of SQL operation.
type SQLOperation string

const (
	SQLSelect SQLOperation = "SELECT"
	SQLInsert SQLOperation = "INSERT"
	SQLUpdate SQLOperation = "UPDATE"
	SQLDelete SQLOperation = "DELETE"
	SQLCreate SQLOperation = "CREATE"
	SQLAlter  SQLOperation = "ALTER"
	SQLDrop   SQLOperation = "DROP"
)

// SQL detection patterns (compiled once for performance).
var (
	// Basic SQL patterns
	sqlSelectPattern = regexp.MustCompile(`(?i)\bSELECT\s+.+?\s+FROM\s+["\x60]?(\w+)["\x60]?`)
	sqlInsertPattern = regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+["\x60]?(\w+)["\x60]?`)
	sqlUpdatePattern = regexp.MustCompile(`(?i)\bUPDATE\s+["\x60]?(\w+)["\x60]?\s+SET`)
	sqlDeletePattern = regexp.MustCompile(`(?i)\bDELETE\s+FROM\s+["\x60]?(\w+)["\x60]?`)
	sqlCreatePattern = regexp.MustCompile(`(?i)\bCREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?["\x60]?(\w+)["\x60]?`)
	sqlAlterPattern  = regexp.MustCompile(`(?i)\bALTER\s+TABLE\s+["\x60]?(\w+)["\x60]?`)
	sqlDropPattern   = regexp.MustCompile(`(?i)\bDROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?["\x60]?(\w+)["\x60]?`)

	// ORM patterns for Go
	gormFindPattern   = regexp.MustCompile(`\.(?:First|Find|Take|Last)\s*\(\s*&?(\w+)`)
	gormCreatePattern = regexp.MustCompile(`\.Create\s*\(\s*&?(\w+)`)
	gormUpdatePattern = regexp.MustCompile(`\.(?:Update|Updates|Save)\s*\(`)
	gormDeletePattern = regexp.MustCompile(`\.Delete\s*\(\s*&?(\w+)`)
	gormTablePattern  = regexp.MustCompile(`\.Table\s*\(\s*["'](\w+)["']\s*\)`)
	gormModelPattern  = regexp.MustCompile(`\.Model\s*\(\s*&?(\w+)`)

	// sqlx patterns
	sqlxQueryPattern = regexp.MustCompile(`\.(?:Query|Get|Select)x?\s*\([^,]+,\s*["'](.+?)["']`)
	sqlxExecPattern  = regexp.MustCompile(`\.(?:Exec|NamedExec)\s*\([^,]+,\s*["'](.+?)["']`)

	// ent patterns
	entQueryPattern  = regexp.MustCompile(`\.Query(\w+)\s*\(`)
	entCreatePattern = regexp.MustCompile(`\.Create(\w+)\s*\(`)
)

// NewSchemaDepTracker creates a new schema dependency tracker.
//
// # Inputs
//
//   - projectRoot: Root directory of the project.
//   - g: The dependency graph.
//   - idx: The symbol index.
//
// # Outputs
//
//   - *SchemaDepTracker: Ready-to-use tracker.
func NewSchemaDepTracker(projectRoot string, g *graph.Graph, idx *index.SymbolIndex) *SchemaDepTracker {
	return &SchemaDepTracker{
		projectRoot: projectRoot,
		codeGraph:   g,
		symbolIndex: idx,
		tableUsages: make(map[string][]SchemaDependency),
		symbolDeps:  make(map[string][]SchemaDependency),
	}
}

// Scan analyzes the codebase for database dependencies.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//
// # Outputs
//
//   - error: Non-nil on failure.
func (t *SchemaDepTracker) Scan(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Clear existing data
	t.dependencies = nil
	t.tableUsages = make(map[string][]SchemaDependency)
	t.symbolDeps = make(map[string][]SchemaDependency)

	// Walk source files
	err := filepath.WalkDir(t.projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip non-Go files and test files for now
		if d.IsDir() {
			// Skip common non-source directories
			name := d.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(path)
		if ext != ".go" {
			return nil
		}

		// Scan file for SQL patterns
		deps, err := t.scanFile(ctx, path)
		if err != nil {
			return nil // Continue on errors
		}

		t.dependencies = append(t.dependencies, deps...)
		for _, dep := range deps {
			t.tableUsages[dep.Table] = append(t.tableUsages[dep.Table], dep)
			t.symbolDeps[dep.SourceSymbol] = append(t.symbolDeps[dep.SourceSymbol], dep)
		}

		return nil
	})

	return err
}

// scanFile scans a single file for database dependencies.
func (t *SchemaDepTracker) scanFile(ctx context.Context, path string) ([]SchemaDependency, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var deps []SchemaDependency
	relPath, _ := filepath.Rel(t.projectRoot, path)

	scanner := bufio.NewScanner(f)
	lineNum := 0

	// Track current function for symbol mapping
	var currentSymbol string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		lineNum++
		line := scanner.Text()

		// Simple function detection (for symbol mapping)
		if strings.Contains(line, "func ") {
			currentSymbol = t.extractFunctionSymbol(line, relPath, lineNum)
		}

		// Check for SQL patterns
		deps = append(deps, t.detectSQLPatterns(line, relPath, lineNum, currentSymbol)...)

		// Check for ORM patterns
		deps = append(deps, t.detectORMPatterns(line, relPath, lineNum, currentSymbol)...)
	}

	return deps, scanner.Err()
}

// extractFunctionSymbol extracts a symbol ID from a function declaration.
func (t *SchemaDepTracker) extractFunctionSymbol(line, relPath string, lineNum int) string {
	// Simple extraction - look for "func Name(" or "func (r Receiver) Name("
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "func ") {
		return ""
	}

	// Remove "func "
	rest := strings.TrimPrefix(line, "func ")

	// Check for receiver
	if strings.HasPrefix(rest, "(") {
		// Skip receiver
		closeIdx := strings.Index(rest, ")")
		if closeIdx < 0 {
			return ""
		}
		rest = strings.TrimSpace(rest[closeIdx+1:])
	}

	// Get function name
	parenIdx := strings.Index(rest, "(")
	if parenIdx < 0 {
		return ""
	}
	funcName := strings.TrimSpace(rest[:parenIdx])

	// Build symbol ID
	return relPath + ":" + funcName
}

// detectSQLPatterns finds SQL queries in a line.
func (t *SchemaDepTracker) detectSQLPatterns(line, relPath string, lineNum int, symbol string) []SchemaDependency {
	var deps []SchemaDependency

	// Patterns with their operations
	patterns := []struct {
		pattern *regexp.Regexp
		op      SQLOperation
	}{
		{sqlSelectPattern, SQLSelect},
		{sqlInsertPattern, SQLInsert},
		{sqlUpdatePattern, SQLUpdate},
		{sqlDeletePattern, SQLDelete},
		{sqlCreatePattern, SQLCreate},
		{sqlAlterPattern, SQLAlter},
		{sqlDropPattern, SQLDrop},
	}

	for _, p := range patterns {
		matches := p.pattern.FindStringSubmatch(line)
		if len(matches) >= 2 {
			table := matches[1]
			// Skip common false positives
			if t.isLikelyFalsePositive(table) {
				continue
			}

			deps = append(deps, SchemaDependency{
				Table:        table,
				Operation:    p.op,
				SourceSymbol: symbol,
				FilePath:     relPath,
				Line:         lineNum,
				RawQuery:     truncateString(line, 100),
				Confidence:   80,
			})
		}
	}

	return deps
}

// detectORMPatterns finds ORM operations in a line.
func (t *SchemaDepTracker) detectORMPatterns(line, relPath string, lineNum int, symbol string) []SchemaDependency {
	var deps []SchemaDependency

	// GORM patterns
	if matches := gormTablePattern.FindStringSubmatch(line); len(matches) >= 2 {
		deps = append(deps, SchemaDependency{
			Table:        matches[1],
			Operation:    SQLSelect,
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			ORM:          "gorm",
			Confidence:   90,
		})
	}

	if matches := gormFindPattern.FindStringSubmatch(line); len(matches) >= 2 {
		modelName := matches[1]
		tableName := t.modelToTable(modelName)
		deps = append(deps, SchemaDependency{
			Table:        tableName,
			Operation:    SQLSelect,
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			ORM:          "gorm",
			Confidence:   70,
		})
	}

	if matches := gormCreatePattern.FindStringSubmatch(line); len(matches) >= 2 {
		modelName := matches[1]
		tableName := t.modelToTable(modelName)
		deps = append(deps, SchemaDependency{
			Table:        tableName,
			Operation:    SQLInsert,
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			ORM:          "gorm",
			Confidence:   70,
		})
	}

	if gormDeletePattern.MatchString(line) {
		if matches := gormDeletePattern.FindStringSubmatch(line); len(matches) >= 2 {
			modelName := matches[1]
			tableName := t.modelToTable(modelName)
			deps = append(deps, SchemaDependency{
				Table:        tableName,
				Operation:    SQLDelete,
				SourceSymbol: symbol,
				FilePath:     relPath,
				Line:         lineNum,
				ORM:          "gorm",
				Confidence:   70,
			})
		}
	}

	// sqlx patterns
	if matches := sqlxQueryPattern.FindStringSubmatch(line); len(matches) >= 2 {
		sqlSnippet := matches[1]
		// Try to extract table from SQL
		if tableDeps := t.detectSQLPatterns(sqlSnippet, relPath, lineNum, symbol); len(tableDeps) > 0 {
			for i := range tableDeps {
				tableDeps[i].ORM = "sqlx"
			}
			deps = append(deps, tableDeps...)
		}
	}

	// ent patterns
	if matches := entQueryPattern.FindStringSubmatch(line); len(matches) >= 2 {
		modelName := matches[1]
		tableName := t.modelToTable(modelName)
		deps = append(deps, SchemaDependency{
			Table:        tableName,
			Operation:    SQLSelect,
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			ORM:          "ent",
			Confidence:   85,
		})
	}

	if matches := entCreatePattern.FindStringSubmatch(line); len(matches) >= 2 {
		modelName := matches[1]
		tableName := t.modelToTable(modelName)
		deps = append(deps, SchemaDependency{
			Table:        tableName,
			Operation:    SQLInsert,
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			ORM:          "ent",
			Confidence:   85,
		})
	}

	return deps
}

// modelToTable converts a model name to a likely table name.
func (t *SchemaDepTracker) modelToTable(modelName string) string {
	// Common convention: snake_case plural
	// User -> users, OrderItem -> order_items
	result := toSnakeCase(modelName)

	// Simple pluralization
	if !strings.HasSuffix(result, "s") {
		result += "s"
	}

	return result
}

// toSnakeCase converts CamelCase to snake_case.
func toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				result.WriteByte('_')
			}
			result.WriteRune(r + 32) // lowercase
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// isLikelyFalsePositive checks for common false positive table names.
func (t *SchemaDepTracker) isLikelyFalsePositive(table string) bool {
	// Common false positives
	falsePositives := map[string]bool{
		"dual":        true, // Oracle dual
		"information": true,
		"schema":      true,
		"into":        true,
		"from":        true,
	}
	return falsePositives[strings.ToLower(table)]
}

// truncateString truncates a string to max length.
func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// FindTableUsages returns all code locations that use a table.
//
// # Inputs
//
//   - tableName: The table to search for.
//
// # Outputs
//
//   - []SchemaDependency: All usages of the table.
//   - error: Non-nil on failure.
func (t *SchemaDepTracker) FindTableUsages(tableName string) ([]SchemaDependency, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	deps := t.tableUsages[tableName]
	if deps == nil {
		return []SchemaDependency{}, nil
	}

	// Return a copy
	result := make([]SchemaDependency, len(deps))
	copy(result, deps)
	return result, nil
}

// FindSymbolDependencies returns all schema dependencies for a symbol.
//
// # Inputs
//
//   - symbolID: The symbol to get dependencies for.
//
// # Outputs
//
//   - []SchemaDependency: All schema dependencies.
//   - error: Non-nil on failure.
func (t *SchemaDepTracker) FindSymbolDependencies(symbolID string) ([]SchemaDependency, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	deps := t.symbolDeps[symbolID]
	if deps == nil {
		return []SchemaDependency{}, nil
	}

	// Return a copy
	result := make([]SchemaDependency, len(deps))
	copy(result, deps)
	return result, nil
}

// AllDependencies returns all detected schema dependencies.
func (t *SchemaDepTracker) AllDependencies() []SchemaDependency {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]SchemaDependency, len(t.dependencies))
	copy(result, t.dependencies)
	return result
}

// Tables returns all tables that have dependencies.
func (t *SchemaDepTracker) Tables() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	tables := make([]string, 0, len(t.tableUsages))
	for table := range t.tableUsages {
		tables = append(tables, table)
	}
	return tables
}

// SchemaDepEnricher implements Enricher for schema dependency tracking.
type SchemaDepEnricher struct {
	tracker *SchemaDepTracker
	mu      sync.RWMutex
	scanned bool
}

// NewSchemaDepEnricher creates an enricher for schema dependencies.
func NewSchemaDepEnricher(projectRoot string, g *graph.Graph, idx *index.SymbolIndex) *SchemaDepEnricher {
	return &SchemaDepEnricher{
		tracker: NewSchemaDepTracker(projectRoot, g, idx),
	}
}

// Name returns the enricher name.
func (e *SchemaDepEnricher) Name() string {
	return "schema_deps"
}

// Priority returns execution priority.
func (e *SchemaDepEnricher) Priority() int {
	return 2
}

// Enrich adds schema dependency information to the blast radius.
func (e *SchemaDepEnricher) Enrich(ctx context.Context, target *EnrichmentTarget, result *EnhancedBlastRadius) error {
	if ctx == nil {
		return ErrNilContext
	}

	// Ensure we've scanned
	e.mu.Lock()
	if !e.scanned {
		if err := e.tracker.Scan(ctx); err != nil {
			e.mu.Unlock()
			return err
		}
		e.scanned = true
	}
	e.mu.Unlock()

	// Get dependencies for target symbol
	deps, _ := e.tracker.FindSymbolDependencies(target.SymbolID)
	if len(deps) > 0 {
		result.SchemaDependencies = deps
	}

	// Also check callers for schema dependencies
	for _, caller := range result.DirectCallers {
		callerDeps, _ := e.tracker.FindSymbolDependencies(caller.ID)
		result.SchemaDependencies = append(result.SchemaDependencies, callerDeps...)
	}

	return nil
}

// Invalidate forces a rescan on next enrichment.
func (e *SchemaDepEnricher) Invalidate() {
	e.mu.Lock()
	e.scanned = false
	e.mu.Unlock()
}
