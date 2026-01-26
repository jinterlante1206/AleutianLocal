// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package explore

import (
	"context"
	"regexp"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

// ConfigPattern defines a pattern for identifying configuration access.
type ConfigPattern struct {
	// FunctionName is a pattern for the function/method name.
	FunctionName string

	// Receiver is a pattern for the receiver type (for methods).
	Receiver string

	// Package is a pattern for the package containing the function.
	Package string

	// Framework identifies the configuration framework.
	Framework string

	// Description explains what this pattern matches.
	Description string

	// compiled regex patterns
	nameRegex *regexp.Regexp
}

// ConfigFinder finds configuration usage in code.
//
// Thread Safety:
//
//	ConfigFinder is safe for concurrent use. It performs read-only
//	operations on the graph and index.
type ConfigFinder struct {
	graph    *graph.Graph
	index    *index.SymbolIndex
	patterns map[string][]ConfigPattern
}

// NewConfigFinder creates a new ConfigFinder.
//
// Description:
//
//	Creates a finder that can identify configuration access points
//	in code, tracking where configuration values are read.
//
// Inputs:
//
//	g - The code graph. Must be frozen.
//	idx - The symbol index.
//
// Outputs:
//
//	*ConfigFinder - The configured finder.
//
// Example:
//
//	finder := NewConfigFinder(graph, index)
//	usage, err := finder.FindConfigUsage(ctx, "DATABASE_URL")
func NewConfigFinder(g *graph.Graph, idx *index.SymbolIndex) *ConfigFinder {
	f := &ConfigFinder{
		graph:    g,
		index:    idx,
		patterns: make(map[string][]ConfigPattern),
	}

	// Register default patterns
	f.patterns["go"] = DefaultGoConfigPatterns()
	f.patterns["python"] = DefaultPythonConfigPatterns()
	f.patterns["typescript"] = DefaultTypeScriptConfigPatterns()
	f.patterns["javascript"] = DefaultTypeScriptConfigPatterns()

	return f
}

// FindConfigUsage finds all places where a configuration key is used.
//
// Description:
//
//	Searches the codebase for usages of the specified configuration key.
//	Supports patterns with wildcards (e.g., "DATABASE_*" matches
//	DATABASE_URL, DATABASE_HOST, etc.).
//
// Inputs:
//
//	ctx - Context for cancellation.
//	configKey - The configuration key or pattern to search for.
//
// Outputs:
//
//	*ConfigUsage - All usages of the configuration key.
//	error - Non-nil if the operation was canceled.
//
// Performance:
//
//	Target latency: < 500ms for typical codebases.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (f *ConfigFinder) FindConfigUsage(ctx context.Context, configKey string) (*ConfigUsage, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	usage := &ConfigUsage{
		ConfigKey: configKey,
		UsedIn:    make([]ConfigUse, 0),
	}

	// Build regex for config key matching
	var keyRegex *regexp.Regexp
	if strings.Contains(configKey, "*") {
		regexPattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(configKey), "\\*", ".*") + "$"
		var err error
		keyRegex, err = regexp.Compile(regexPattern)
		if err != nil {
			return nil, ErrInvalidInput
		}
	}

	// Iterate through all symbols looking for config access
	stats := f.index.Stats()
	_ = stats // Use stats for potential optimization

	// Search in functions that might access configuration
	for _, kind := range []ast.SymbolKind{
		ast.SymbolKindFunction,
		ast.SymbolKindMethod,
	} {
		symbols := f.index.GetByKind(kind)
		for _, sym := range symbols {
			if err := ctx.Err(); err != nil {
				return usage, ErrContextCanceled
			}

			// Check if this function is a config accessor
			if f.isConfigAccessor(sym) {
				// Check if the function references our config key
				if f.matchesConfigKey(sym, configKey, keyRegex) {
					usage.UsedIn = append(usage.UsedIn, ConfigUse{
						Function: sym.Name,
						FilePath: sym.FilePath,
						Line:     sym.StartLine,
						Context:  f.getConfigContext(sym),
					})
				}
			}
		}
	}

	return usage, nil
}

// FindAllConfigAccess finds all configuration access points in a file.
//
// Description:
//
//	Scans all symbols in a file and identifies configuration access points.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	filePath - The relative path to the file.
//
// Outputs:
//
//	[]ConfigUse - All configuration access points in the file.
//	error - Non-nil if the file is not found or operation was canceled.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (f *ConfigFinder) FindAllConfigAccess(ctx context.Context, filePath string) ([]ConfigUse, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	symbols := f.index.GetByFile(filePath)
	if len(symbols) == 0 {
		return nil, ErrFileNotFound
	}

	accesses := make([]ConfigUse, 0)
	for _, sym := range symbols {
		if err := ctx.Err(); err != nil {
			return accesses, ErrContextCanceled
		}

		if f.isConfigAccessor(sym) {
			accesses = append(accesses, ConfigUse{
				Function: sym.Name,
				FilePath: sym.FilePath,
				Line:     sym.StartLine,
				Context:  f.getConfigContext(sym),
			})
		}
	}

	return accesses, nil
}

// FindConfigByFramework finds configuration access for a specific framework.
//
// Description:
//
//	Finds all configuration access points that use a specific framework
//	(e.g., "viper", "envconfig", "dotenv").
//
// Inputs:
//
//	ctx - Context for cancellation.
//	framework - The framework name to search for.
//
// Outputs:
//
//	[]ConfigUse - All configuration access points for the framework.
//	error - Non-nil if the operation was canceled.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (f *ConfigFinder) FindConfigByFramework(ctx context.Context, framework string) ([]ConfigUse, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	accesses := make([]ConfigUse, 0)
	frameworkLower := strings.ToLower(framework)

	for _, kind := range []ast.SymbolKind{
		ast.SymbolKindFunction,
		ast.SymbolKindMethod,
	} {
		symbols := f.index.GetByKind(kind)
		for _, sym := range symbols {
			if err := ctx.Err(); err != nil {
				return accesses, ErrContextCanceled
			}

			// Check if this symbol uses the specified framework
			patterns := f.patterns[strings.ToLower(sym.Language)]
			for _, pattern := range patterns {
				if strings.ToLower(pattern.Framework) == frameworkLower {
					if f.matchesPattern(sym, &pattern) {
						accesses = append(accesses, ConfigUse{
							Function: sym.Name,
							FilePath: sym.FilePath,
							Line:     sym.StartLine,
							Context:  pattern.Framework + ": " + pattern.Description,
						})
						break
					}
				}
			}
		}
	}

	return accesses, nil
}

// FindEnvironmentVariables finds all environment variable accesses.
//
// Description:
//
//	Specifically finds accesses to environment variables through
//	os.Getenv, os.environ, process.env, etc.
//
// Inputs:
//
//	ctx - Context for cancellation.
//
// Outputs:
//
//	[]ConfigUse - All environment variable access points.
//	error - Non-nil if the operation was canceled.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (f *ConfigFinder) FindEnvironmentVariables(ctx context.Context) ([]ConfigUse, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	envAccesses := make([]ConfigUse, 0)

	// Env var access patterns
	envPatterns := map[string][]string{
		"go":         {"Getenv", "LookupEnv", "Environ"},
		"python":     {"getenv", "environ"},
		"typescript": {"env"},
		"javascript": {"env"},
	}

	for _, kind := range []ast.SymbolKind{
		ast.SymbolKindFunction,
		ast.SymbolKindMethod,
	} {
		symbols := f.index.GetByKind(kind)
		for _, sym := range symbols {
			if err := ctx.Err(); err != nil {
				return envAccesses, ErrContextCanceled
			}

			lang := strings.ToLower(sym.Language)
			patterns, ok := envPatterns[lang]
			if !ok {
				continue
			}

			for _, pattern := range patterns {
				if sym.Name == pattern || strings.Contains(sym.Name, pattern) {
					envAccesses = append(envAccesses, ConfigUse{
						Function: sym.Name,
						FilePath: sym.FilePath,
						Line:     sym.StartLine,
						Context:  "Environment variable access",
					})
					break
				}
			}
		}
	}

	return envAccesses, nil
}

// isConfigAccessor checks if a symbol is a configuration accessor.
func (f *ConfigFinder) isConfigAccessor(sym *ast.Symbol) bool {
	if sym == nil {
		return false
	}

	patterns := f.patterns[strings.ToLower(sym.Language)]
	for i := range patterns {
		if f.matchesPattern(sym, &patterns[i]) {
			return true
		}
	}

	return false
}

// matchesPattern checks if a symbol matches a config pattern.
func (f *ConfigFinder) matchesPattern(sym *ast.Symbol, pattern *ConfigPattern) bool {
	if sym == nil {
		return false
	}

	// Match function name
	if pattern.FunctionName != "" {
		if strings.Contains(pattern.FunctionName, "*") {
			if pattern.nameRegex == nil {
				regexPattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern.FunctionName), "\\*", ".*") + "$"
				pattern.nameRegex, _ = regexp.Compile(regexPattern)
			}
			if pattern.nameRegex != nil && !pattern.nameRegex.MatchString(sym.Name) {
				return false
			}
		} else if sym.Name != pattern.FunctionName {
			return false
		}
	}

	// Match receiver
	if pattern.Receiver != "" && !strings.Contains(sym.Receiver, pattern.Receiver) {
		return false
	}

	// Match package
	if pattern.Package != "" {
		if strings.Contains(pattern.Package, "*") {
			regexPattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern.Package), "\\*", ".*") + "$"
			if matched, _ := regexp.MatchString(regexPattern, sym.Package); !matched {
				return false
			}
		} else if !strings.Contains(sym.Package, pattern.Package) {
			return false
		}
	}

	return true
}

// matchesConfigKey checks if a symbol references a config key.
func (f *ConfigFinder) matchesConfigKey(sym *ast.Symbol, configKey string, keyRegex *regexp.Regexp) bool {
	if sym == nil {
		return false
	}

	// For now, we do a simple name/signature check
	// A more sophisticated implementation would analyze the AST
	searchIn := sym.Name + " " + sym.Signature

	if keyRegex != nil {
		return keyRegex.MatchString(searchIn) || strings.Contains(strings.ToUpper(searchIn), strings.ToUpper(configKey))
	}

	return strings.Contains(strings.ToUpper(searchIn), strings.ToUpper(configKey))
}

// getConfigContext returns context about a config accessor.
func (f *ConfigFinder) getConfigContext(sym *ast.Symbol) string {
	if sym == nil {
		return ""
	}

	patterns := f.patterns[strings.ToLower(sym.Language)]
	for i := range patterns {
		if f.matchesPattern(sym, &patterns[i]) {
			return patterns[i].Framework + ": " + patterns[i].Description
		}
	}

	return "Configuration access"
}

// GetPatterns returns config patterns for a language.
func (f *ConfigFinder) GetPatterns(language string) []ConfigPattern {
	return f.patterns[strings.ToLower(language)]
}

// RegisterPatterns adds config patterns for a language.
func (f *ConfigFinder) RegisterPatterns(language string, patterns []ConfigPattern) {
	f.patterns[strings.ToLower(language)] = patterns
}

// DefaultGoConfigPatterns returns default configuration patterns for Go.
func DefaultGoConfigPatterns() []ConfigPattern {
	return []ConfigPattern{
		// Standard library
		{FunctionName: "Getenv", Package: "os", Framework: "stdlib", Description: "Environment variable access"},
		{FunctionName: "LookupEnv", Package: "os", Framework: "stdlib", Description: "Environment variable lookup"},
		{FunctionName: "Environ", Package: "os", Framework: "stdlib", Description: "All environment variables"},

		// Viper
		{FunctionName: "Get", Package: "*viper*", Framework: "viper", Description: "Get config value"},
		{FunctionName: "GetString", Package: "*viper*", Framework: "viper", Description: "Get string config"},
		{FunctionName: "GetInt", Package: "*viper*", Framework: "viper", Description: "Get int config"},
		{FunctionName: "GetBool", Package: "*viper*", Framework: "viper", Description: "Get bool config"},
		{FunctionName: "GetFloat64", Package: "*viper*", Framework: "viper", Description: "Get float64 config"},
		{FunctionName: "GetDuration", Package: "*viper*", Framework: "viper", Description: "Get duration config"},
		{FunctionName: "GetStringSlice", Package: "*viper*", Framework: "viper", Description: "Get string slice config"},
		{FunctionName: "GetStringMap", Package: "*viper*", Framework: "viper", Description: "Get string map config"},
		{FunctionName: "BindEnv", Package: "*viper*", Framework: "viper", Description: "Bind environment variable"},
		{FunctionName: "SetDefault", Package: "*viper*", Framework: "viper", Description: "Set default value"},

		// envconfig
		{FunctionName: "Process", Package: "*envconfig*", Framework: "envconfig", Description: "Process env config"},

		// godotenv
		{FunctionName: "Load", Package: "*godotenv*", Framework: "godotenv", Description: "Load .env file"},

		// flag package
		{FunctionName: "String", Package: "flag", Framework: "flag", Description: "String flag"},
		{FunctionName: "Int", Package: "flag", Framework: "flag", Description: "Int flag"},
		{FunctionName: "Bool", Package: "flag", Framework: "flag", Description: "Bool flag"},
		{FunctionName: "Float64", Package: "flag", Framework: "flag", Description: "Float64 flag"},
		{FunctionName: "Duration", Package: "flag", Framework: "flag", Description: "Duration flag"},
		{FunctionName: "Var", Package: "flag", Framework: "flag", Description: "Custom flag"},
		{FunctionName: "Parse", Package: "flag", Framework: "flag", Description: "Parse flags"},
	}
}

// DefaultPythonConfigPatterns returns default configuration patterns for Python.
func DefaultPythonConfigPatterns() []ConfigPattern {
	return []ConfigPattern{
		// Standard library
		{FunctionName: "getenv", Package: "os", Framework: "stdlib", Description: "Environment variable access"},
		{FunctionName: "environ", Package: "os", Framework: "stdlib", Description: "Environment dict"},

		// python-dotenv
		{FunctionName: "load_dotenv", Package: "*dotenv*", Framework: "dotenv", Description: "Load .env file"},
		{FunctionName: "dotenv_values", Package: "*dotenv*", Framework: "dotenv", Description: "Get dotenv values"},

		// pydantic settings
		{FunctionName: "BaseSettings", Package: "*pydantic*", Framework: "pydantic", Description: "Pydantic settings"},

		// configparser
		{FunctionName: "ConfigParser", Package: "configparser", Framework: "configparser", Description: "Config parser"},
		{FunctionName: "get", Receiver: "ConfigParser", Framework: "configparser", Description: "Get config value"},
		{FunctionName: "getint", Receiver: "ConfigParser", Framework: "configparser", Description: "Get int config"},
		{FunctionName: "getfloat", Receiver: "ConfigParser", Framework: "configparser", Description: "Get float config"},
		{FunctionName: "getboolean", Receiver: "ConfigParser", Framework: "configparser", Description: "Get bool config"},

		// django settings
		{FunctionName: "settings", Package: "*django*", Framework: "django", Description: "Django settings"},

		// dynaconf
		{FunctionName: "Dynaconf", Package: "*dynaconf*", Framework: "dynaconf", Description: "Dynaconf settings"},
	}
}

// DefaultTypeScriptConfigPatterns returns default configuration patterns for TypeScript/JavaScript.
func DefaultTypeScriptConfigPatterns() []ConfigPattern {
	return []ConfigPattern{
		// process.env
		{FunctionName: "env", Receiver: "process", Framework: "node", Description: "Environment variable access"},

		// dotenv
		{FunctionName: "config", Package: "*dotenv*", Framework: "dotenv", Description: "Load .env file"},

		// config packages
		{FunctionName: "get", Package: "*config*", Framework: "config", Description: "Get config value"},
		{FunctionName: "has", Package: "*config*", Framework: "config", Description: "Check config exists"},

		// convict
		{FunctionName: "convict", Package: "*convict*", Framework: "convict", Description: "Convict config"},
		{FunctionName: "get", Receiver: "convict", Framework: "convict", Description: "Get convict value"},

		// nestjs config
		{FunctionName: "ConfigService", Package: "*@nestjs*", Framework: "nestjs", Description: "NestJS config service"},
		{FunctionName: "get", Receiver: "ConfigService", Framework: "nestjs", Description: "Get NestJS config"},

		// Next.js env
		{FunctionName: "env", Receiver: "process", Framework: "nextjs", Description: "Next.js env access"},
	}
}

// TraceConfigUsage traces where a config value flows after being read.
//
// Description:
//
//	Starting from a config access point, traces where the value flows
//	through the codebase using call graph analysis.
//
// Inputs:
//
//	ctx - Context for cancellation.
//	symbolID - The symbol ID of the config accessor.
//	opts - Optional configuration (MaxNodes, MaxHops).
//
// Outputs:
//
//	*DataFlow - The flow of the config value through the code.
//	error - Non-nil if the symbol is not found or operation was canceled.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (f *ConfigFinder) TraceConfigUsage(ctx context.Context, symbolID string, opts ...ExploreOption) (*DataFlow, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, ErrContextCanceled
	}

	// Create a data flow tracer and trace from this symbol
	tracer := NewDataFlowTracer(f.graph, f.index)
	return tracer.TraceDataFlow(ctx, symbolID, opts...)
}
