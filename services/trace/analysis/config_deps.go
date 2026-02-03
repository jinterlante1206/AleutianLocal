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

	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
)

// ConfigDepTracker tracks configuration dependencies in code.
//
// # Description
//
// Detects environment variable usage, Viper config references, and struct
// tag-based configuration. Maps these to symbols to understand which code
// depends on which configuration values.
//
// # Thread Safety
//
// Safe for concurrent use after construction.
type ConfigDepTracker struct {
	projectRoot string
	codeGraph   *graph.Graph
	symbolIndex *index.SymbolIndex

	mu           sync.RWMutex
	dependencies []ConfigDependency
	configUsages map[string][]ConfigDependency // config key -> usages
	symbolDeps   map[string][]ConfigDependency // symbol -> deps
}

// ConfigDependency represents a configuration dependency from code.
type ConfigDependency struct {
	// Key is the configuration key name.
	Key string `json:"key"`

	// Type is the type of configuration (env, viper, flag, file).
	Type ConfigType `json:"type"`

	// DefaultValue is the default value (if detectable).
	DefaultValue string `json:"default_value,omitempty"`

	// Required indicates if this config is required.
	Required bool `json:"required"`

	// SourceSymbol is the symbol containing this dependency.
	SourceSymbol string `json:"source_symbol"`

	// FilePath is the file containing the dependency.
	FilePath string `json:"file_path"`

	// Line is the line number of the dependency.
	Line int `json:"line"`

	// Confidence is how confident we are in this detection (0-100).
	Confidence int `json:"confidence"`
}

// ConfigType represents the type of configuration source.
type ConfigType string

const (
	ConfigTypeEnv    ConfigType = "ENV"
	ConfigTypeViper  ConfigType = "VIPER"
	ConfigTypeFlag   ConfigType = "FLAG"
	ConfigTypeFile   ConfigType = "FILE"
	ConfigTypeStruct ConfigType = "STRUCT_TAG"
)

// Config detection patterns
var (
	// os.Getenv patterns
	osGetenvPattern    = regexp.MustCompile(`os\.Getenv\s*\(\s*["']([^"']+)["']\s*\)`)
	osLookupEnvPattern = regexp.MustCompile(`os\.LookupEnv\s*\(\s*["']([^"']+)["']\s*\)`)

	// Viper patterns
	viperGetPattern        = regexp.MustCompile(`viper\.(?:Get|GetString|GetInt|GetBool|GetFloat64|GetDuration|GetStringSlice)\s*\(\s*["']([^"']+)["']\s*\)`)
	viperSetDefaultPattern = regexp.MustCompile(`viper\.SetDefault\s*\(\s*["']([^"']+)["']\s*,\s*(.+?)\s*\)`)
	viperBindEnvPattern    = regexp.MustCompile(`viper\.BindEnv\s*\(\s*["']([^"']+)["']`)

	// Flag patterns
	flagStringPattern = regexp.MustCompile(`flag\.(?:String|Int|Bool|Float64|Duration)\s*\(\s*["']([^"']+)["']`)
	pflagPattern      = regexp.MustCompile(`pflag\.(?:String|Int|Bool|Float64|Duration)(?:P)?\s*\(\s*["']([^"']+)["']`)

	// Struct tag patterns for config
	envTagPattern      = regexp.MustCompile(`env:"([^"]+)"`)
	configTagPattern   = regexp.MustCompile(`(?:mapstructure|yaml|json|toml):"([^"]+)"`)
	defaultTagPattern  = regexp.MustCompile(`default:"([^"]+)"`)
	requiredTagPattern = regexp.MustCompile(`required:"true"`)

	// Envconfig patterns
	envconfigPattern = regexp.MustCompile(`envconfig\.Process\s*\(\s*["']([^"']+)["']`)
)

// NewConfigDepTracker creates a new configuration dependency tracker.
//
// # Inputs
//
//   - projectRoot: Root directory of the project.
//   - g: The dependency graph.
//   - idx: The symbol index.
//
// # Outputs
//
//   - *ConfigDepTracker: Ready-to-use tracker.
func NewConfigDepTracker(projectRoot string, g *graph.Graph, idx *index.SymbolIndex) *ConfigDepTracker {
	return &ConfigDepTracker{
		projectRoot:  projectRoot,
		codeGraph:    g,
		symbolIndex:  idx,
		configUsages: make(map[string][]ConfigDependency),
		symbolDeps:   make(map[string][]ConfigDependency),
	}
}

// Scan analyzes the codebase for configuration dependencies.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//
// # Outputs
//
//   - error: Non-nil on failure.
func (t *ConfigDepTracker) Scan(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Clear existing data
	t.dependencies = nil
	t.configUsages = make(map[string][]ConfigDependency)
	t.symbolDeps = make(map[string][]ConfigDependency)

	// Walk source files
	err := filepath.WalkDir(t.projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if d.IsDir() {
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

		deps, err := t.scanFile(ctx, path)
		if err != nil {
			return nil
		}

		t.dependencies = append(t.dependencies, deps...)
		for _, dep := range deps {
			t.configUsages[dep.Key] = append(t.configUsages[dep.Key], dep)
			t.symbolDeps[dep.SourceSymbol] = append(t.symbolDeps[dep.SourceSymbol], dep)
		}

		return nil
	})

	return err
}

// scanFile scans a single file for configuration dependencies.
func (t *ConfigDepTracker) scanFile(ctx context.Context, path string) ([]ConfigDependency, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var deps []ConfigDependency
	relPath, _ := filepath.Rel(t.projectRoot, path)

	scanner := bufio.NewScanner(f)
	lineNum := 0
	var currentSymbol string
	var inStructDef bool
	var structName string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		lineNum++
		line := scanner.Text()

		// Track current function/struct
		if strings.Contains(line, "func ") {
			currentSymbol = extractConfigSymbol(line, relPath)
			inStructDef = false
		}

		// Track struct definitions for tag scanning
		if strings.Contains(line, "type ") && strings.Contains(line, "struct") {
			structName = extractStructName(line)
			inStructDef = true
			currentSymbol = relPath + ":" + structName
		}

		if strings.Contains(line, "}") && !strings.Contains(line, "{") {
			inStructDef = false
		}

		// Detect env var usage
		deps = append(deps, t.detectEnvPatterns(line, relPath, lineNum, currentSymbol)...)

		// Detect viper usage
		deps = append(deps, t.detectViperPatterns(line, relPath, lineNum, currentSymbol)...)

		// Detect flag usage
		deps = append(deps, t.detectFlagPatterns(line, relPath, lineNum, currentSymbol)...)

		// Detect struct tag configs
		if inStructDef {
			deps = append(deps, t.detectStructTagPatterns(line, relPath, lineNum, currentSymbol)...)
		}
	}

	return deps, scanner.Err()
}

// extractConfigSymbol extracts a symbol ID from a function declaration.
func extractConfigSymbol(line, relPath string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "func ") {
		return ""
	}

	rest := strings.TrimPrefix(line, "func ")

	if strings.HasPrefix(rest, "(") {
		closeIdx := strings.Index(rest, ")")
		if closeIdx < 0 {
			return ""
		}
		rest = strings.TrimSpace(rest[closeIdx+1:])
	}

	parenIdx := strings.Index(rest, "(")
	if parenIdx < 0 {
		return ""
	}
	funcName := strings.TrimSpace(rest[:parenIdx])

	return relPath + ":" + funcName
}

// extractStructName extracts struct name from a type declaration.
func extractStructName(line string) string {
	// type Foo struct { or type Foo struct{
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "type ") {
		return ""
	}

	rest := strings.TrimPrefix(line, "type ")
	spaceIdx := strings.Index(rest, " ")
	if spaceIdx < 0 {
		return ""
	}

	return strings.TrimSpace(rest[:spaceIdx])
}

// detectEnvPatterns finds environment variable usage.
func (t *ConfigDepTracker) detectEnvPatterns(line, relPath string, lineNum int, symbol string) []ConfigDependency {
	var deps []ConfigDependency

	// os.Getenv
	if matches := osGetenvPattern.FindStringSubmatch(line); len(matches) >= 2 {
		deps = append(deps, ConfigDependency{
			Key:          matches[1],
			Type:         ConfigTypeEnv,
			Required:     !strings.Contains(line, "LookupEnv"), // Getenv implies required
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			Confidence:   95,
		})
	}

	// os.LookupEnv
	if matches := osLookupEnvPattern.FindStringSubmatch(line); len(matches) >= 2 {
		deps = append(deps, ConfigDependency{
			Key:          matches[1],
			Type:         ConfigTypeEnv,
			Required:     false, // LookupEnv returns ok, so not required
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			Confidence:   95,
		})
	}

	return deps
}

// detectViperPatterns finds Viper configuration usage.
func (t *ConfigDepTracker) detectViperPatterns(line, relPath string, lineNum int, symbol string) []ConfigDependency {
	var deps []ConfigDependency

	// viper.Get*
	if matches := viperGetPattern.FindStringSubmatch(line); len(matches) >= 2 {
		deps = append(deps, ConfigDependency{
			Key:          matches[1],
			Type:         ConfigTypeViper,
			Required:     true, // Get implies required
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			Confidence:   90,
		})
	}

	// viper.SetDefault
	if matches := viperSetDefaultPattern.FindStringSubmatch(line); len(matches) >= 3 {
		deps = append(deps, ConfigDependency{
			Key:          matches[1],
			Type:         ConfigTypeViper,
			DefaultValue: strings.TrimSpace(matches[2]),
			Required:     false,
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			Confidence:   90,
		})
	}

	// viper.BindEnv
	if matches := viperBindEnvPattern.FindStringSubmatch(line); len(matches) >= 2 {
		deps = append(deps, ConfigDependency{
			Key:          matches[1],
			Type:         ConfigTypeViper,
			Required:     false,
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			Confidence:   85,
		})
	}

	return deps
}

// detectFlagPatterns finds command-line flag usage.
func (t *ConfigDepTracker) detectFlagPatterns(line, relPath string, lineNum int, symbol string) []ConfigDependency {
	var deps []ConfigDependency

	// flag.String/Int/Bool/etc
	if matches := flagStringPattern.FindStringSubmatch(line); len(matches) >= 2 {
		deps = append(deps, ConfigDependency{
			Key:          matches[1],
			Type:         ConfigTypeFlag,
			Required:     false,
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			Confidence:   95,
		})
	}

	// pflag
	if matches := pflagPattern.FindStringSubmatch(line); len(matches) >= 2 {
		deps = append(deps, ConfigDependency{
			Key:          matches[1],
			Type:         ConfigTypeFlag,
			Required:     false,
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			Confidence:   95,
		})
	}

	return deps
}

// detectStructTagPatterns finds configuration from struct tags.
func (t *ConfigDepTracker) detectStructTagPatterns(line, relPath string, lineNum int, symbol string) []ConfigDependency {
	var deps []ConfigDependency

	// Look for field with tags
	if !strings.Contains(line, "`") {
		return deps
	}

	// env tag
	if matches := envTagPattern.FindStringSubmatch(line); len(matches) >= 2 {
		envKey := matches[1]
		// Handle multiple values (e.g., env:"KEY,required")
		parts := strings.Split(envKey, ",")
		key := parts[0]

		required := requiredTagPattern.MatchString(line)

		var defaultVal string
		if defMatches := defaultTagPattern.FindStringSubmatch(line); len(defMatches) >= 2 {
			defaultVal = defMatches[1]
		}

		deps = append(deps, ConfigDependency{
			Key:          key,
			Type:         ConfigTypeStruct,
			DefaultValue: defaultVal,
			Required:     required,
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			Confidence:   90,
		})
	}

	// mapstructure/yaml/json/toml tags for config files
	if matches := configTagPattern.FindStringSubmatch(line); len(matches) >= 2 {
		key := matches[1]
		// Skip - or omitempty-only values
		if key == "-" || key == "omitempty" {
			return deps
		}
		// Handle "name,omitempty"
		parts := strings.Split(key, ",")
		key = parts[0]
		if key == "" || key == "-" {
			return deps
		}

		deps = append(deps, ConfigDependency{
			Key:          key,
			Type:         ConfigTypeFile,
			Required:     false,
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			Confidence:   75, // Lower confidence as these might not be config
		})
	}

	return deps
}

// FindConfigUsages returns all code locations that use a config key.
func (t *ConfigDepTracker) FindConfigUsages(key string) ([]ConfigDependency, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	deps := t.configUsages[key]
	if deps == nil {
		return []ConfigDependency{}, nil
	}

	result := make([]ConfigDependency, len(deps))
	copy(result, deps)
	return result, nil
}

// FindSymbolDependencies returns all config dependencies for a symbol.
func (t *ConfigDepTracker) FindSymbolDependencies(symbolID string) ([]ConfigDependency, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	deps := t.symbolDeps[symbolID]
	if deps == nil {
		return []ConfigDependency{}, nil
	}

	result := make([]ConfigDependency, len(deps))
	copy(result, deps)
	return result, nil
}

// AllDependencies returns all detected config dependencies.
func (t *ConfigDepTracker) AllDependencies() []ConfigDependency {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]ConfigDependency, len(t.dependencies))
	copy(result, t.dependencies)
	return result
}

// ConfigKeys returns all config keys that have dependencies.
func (t *ConfigDepTracker) ConfigKeys() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	keys := make([]string, 0, len(t.configUsages))
	for key := range t.configUsages {
		keys = append(keys, key)
	}
	return keys
}

// ConfigDepEnricher implements Enricher for config dependency tracking.
type ConfigDepEnricher struct {
	tracker *ConfigDepTracker
	mu      sync.RWMutex
	scanned bool
}

// NewConfigDepEnricher creates an enricher for config dependencies.
func NewConfigDepEnricher(projectRoot string, g *graph.Graph, idx *index.SymbolIndex) *ConfigDepEnricher {
	return &ConfigDepEnricher{
		tracker: NewConfigDepTracker(projectRoot, g, idx),
	}
}

// Name returns the enricher name.
func (e *ConfigDepEnricher) Name() string {
	return "config_deps"
}

// Priority returns execution priority.
func (e *ConfigDepEnricher) Priority() int {
	return 2
}

// Enrich adds config dependency information to the blast radius.
func (e *ConfigDepEnricher) Enrich(ctx context.Context, target *EnrichmentTarget, result *EnhancedBlastRadius) error {
	if ctx == nil {
		return ErrNilContext
	}

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
		result.ConfigDependencies = deps
	}

	// Also check callers for config dependencies
	for _, caller := range result.DirectCallers {
		callerDeps, _ := e.tracker.FindSymbolDependencies(caller.ID)
		result.ConfigDependencies = append(result.ConfigDependencies, callerDeps...)
	}

	return nil
}

// Invalidate forces a rescan on next enrichment.
func (e *ConfigDepEnricher) Invalidate() {
	e.mu.Lock()
	e.scanned = false
	e.mu.Unlock()
}
