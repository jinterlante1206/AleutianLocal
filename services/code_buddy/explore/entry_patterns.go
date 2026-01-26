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
	"regexp"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/ast"
)

// PatternMatcher defines criteria for matching entry points.
type PatternMatcher struct {
	// Name is a glob/regex pattern for the symbol name (e.g., "main", "Test*").
	Name string

	// Package is a glob/regex pattern for the package name.
	Package string

	// Signature is a pattern for the function signature.
	Signature string

	// Interface is the interface name that the type must implement.
	Interface string

	// Implements is similar to Interface but for service implementations.
	Implements string

	// Decorator is a decorator/annotation name (Python/TypeScript).
	Decorator string

	// BaseClass is a base class name (Python).
	BaseClass string

	// Type is a type pattern for struct fields or variables.
	Type string

	// Export indicates an exported symbol requirement.
	Export string

	// Framework identifies the framework if this pattern matches.
	Framework string

	// compiled patterns (lazily initialized)
	nameRegex      *regexp.Regexp
	packageRegex   *regexp.Regexp
	signatureRegex *regexp.Regexp
}

// EntryPointPatterns defines detection patterns per language.
type EntryPointPatterns struct {
	// Language identifies which language these patterns apply to.
	Language string

	// Main patterns for main entry points (main(), __main__).
	Main []PatternMatcher

	// Handler patterns for HTTP/REST handlers.
	Handler []PatternMatcher

	// Command patterns for CLI commands.
	Command []PatternMatcher

	// Test patterns for test functions.
	Test []PatternMatcher

	// Lambda patterns for serverless handlers.
	Lambda []PatternMatcher

	// GRPC patterns for gRPC service implementations.
	GRPC []PatternMatcher
}

// EntryPointRegistry holds patterns for all supported languages.
type EntryPointRegistry struct {
	patterns map[string]*EntryPointPatterns
}

// NewEntryPointRegistry creates a new registry with default patterns.
func NewEntryPointRegistry() *EntryPointRegistry {
	r := &EntryPointRegistry{
		patterns: make(map[string]*EntryPointPatterns),
	}

	// Register default patterns
	r.patterns["go"] = DefaultGoPatterns()
	r.patterns["python"] = DefaultPythonPatterns()
	r.patterns["typescript"] = DefaultTypeScriptPatterns()
	r.patterns["javascript"] = DefaultJavaScriptPatterns()

	return r
}

// GetPatterns returns patterns for a language.
func (r *EntryPointRegistry) GetPatterns(language string) (*EntryPointPatterns, bool) {
	p, ok := r.patterns[strings.ToLower(language)]
	return p, ok
}

// RegisterPatterns adds or replaces patterns for a language.
func (r *EntryPointRegistry) RegisterPatterns(language string, patterns *EntryPointPatterns) {
	r.patterns[strings.ToLower(language)] = patterns
}

// Languages returns all registered languages.
func (r *EntryPointRegistry) Languages() []string {
	langs := make([]string, 0, len(r.patterns))
	for lang := range r.patterns {
		langs = append(langs, lang)
	}
	return langs
}

// Match checks if a symbol matches the pattern.
//
// Thread Safety: This method is safe for concurrent use.
func (pm *PatternMatcher) Match(sym *ast.Symbol) bool {
	if sym == nil {
		return false
	}

	// Match name pattern
	if pm.Name != "" && !pm.matchName(sym.Name) {
		return false
	}

	// Match package pattern
	if pm.Package != "" && !pm.matchPackage(sym.Package) {
		return false
	}

	// Match signature pattern
	if pm.Signature != "" && !pm.matchSignature(sym.Signature) {
		return false
	}

	// Match decorator (stored in metadata)
	if pm.Decorator != "" && !pm.matchDecorator(sym) {
		return false
	}

	// Match base class (stored in metadata)
	if pm.BaseClass != "" && !pm.matchBaseClass(sym) {
		return false
	}

	// Match implements (stored in metadata)
	if pm.Implements != "" && !pm.matchImplements(sym) {
		return false
	}

	return true
}

// matchName checks if the symbol name matches the pattern.
func (pm *PatternMatcher) matchName(name string) bool {
	// Handle glob patterns
	pattern := pm.Name
	if strings.Contains(pattern, "*") {
		// Convert glob to regex
		if pm.nameRegex == nil {
			regexPattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), "\\*", ".*") + "$"
			pm.nameRegex, _ = regexp.Compile(regexPattern)
		}
		if pm.nameRegex != nil {
			return pm.nameRegex.MatchString(name)
		}
		return false
	}
	return name == pattern
}

// matchPackage checks if the symbol package matches the pattern.
func (pm *PatternMatcher) matchPackage(pkg string) bool {
	pattern := pm.Package
	if pattern == "*" {
		return true
	}
	if strings.Contains(pattern, "*") {
		if pm.packageRegex == nil {
			regexPattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), "\\*", ".*") + "$"
			pm.packageRegex, _ = regexp.Compile(regexPattern)
		}
		if pm.packageRegex != nil {
			return pm.packageRegex.MatchString(pkg)
		}
		return false
	}
	return pkg == pattern
}

// matchSignature checks if the symbol signature matches the pattern.
func (pm *PatternMatcher) matchSignature(sig string) bool {
	pattern := pm.Signature
	if strings.Contains(pattern, "*") {
		if pm.signatureRegex == nil {
			regexPattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), "\\*", ".*") + "$"
			pm.signatureRegex, _ = regexp.Compile(regexPattern)
		}
		if pm.signatureRegex != nil {
			return pm.signatureRegex.MatchString(sig)
		}
		return false
	}
	// For signature matching, use contains for flexibility
	return strings.Contains(sig, pattern)
}

// matchDecorator checks if the symbol has a matching decorator.
func (pm *PatternMatcher) matchDecorator(sym *ast.Symbol) bool {
	if sym.Metadata == nil || len(sym.Metadata.Decorators) == 0 {
		return false
	}
	pattern := pm.Decorator
	for _, dec := range sym.Metadata.Decorators {
		// Check exact match or pattern match
		if dec == pattern || strings.HasPrefix(dec, pattern) {
			return true
		}
		// Handle decorator patterns like "@app.route*"
		if strings.Contains(pattern, "*") {
			regexPattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), "\\*", ".*") + "$"
			if matched, _ := regexp.MatchString(regexPattern, dec); matched {
				return true
			}
		}
	}
	return false
}

// matchBaseClass checks if the symbol extends a matching base class.
func (pm *PatternMatcher) matchBaseClass(sym *ast.Symbol) bool {
	if sym.Metadata == nil || sym.Metadata.Extends == "" {
		return false
	}
	pattern := pm.BaseClass
	extends := sym.Metadata.Extends

	if strings.Contains(pattern, "*") {
		regexPattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), "\\*", ".*") + "$"
		if matched, _ := regexp.MatchString(regexPattern, extends); matched {
			return true
		}
		return false
	}
	return extends == pattern || strings.HasSuffix(extends, "."+pattern)
}

// matchImplements checks if the symbol implements a matching interface.
func (pm *PatternMatcher) matchImplements(sym *ast.Symbol) bool {
	if sym.Metadata == nil || len(sym.Metadata.Implements) == 0 {
		return false
	}
	pattern := pm.Implements
	for _, impl := range sym.Metadata.Implements {
		if strings.Contains(pattern, "*") {
			regexPattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), "\\*", ".*") + "$"
			if matched, _ := regexp.MatchString(regexPattern, impl); matched {
				return true
			}
		} else if impl == pattern || strings.HasSuffix(impl, pattern) {
			return true
		}
	}
	return false
}

// DefaultGoPatterns returns default entry point patterns for Go.
func DefaultGoPatterns() *EntryPointPatterns {
	return &EntryPointPatterns{
		Language: "go",
		Main: []PatternMatcher{
			{Name: "main", Package: "main", Framework: "stdlib"},
		},
		Handler: []PatternMatcher{
			// Standard library http.Handler interface
			{Name: "ServeHTTP", Signature: "http.ResponseWriter, *http.Request", Framework: "net/http"},
			// Standard library handler functions
			{Signature: "func(http.ResponseWriter, *http.Request)", Framework: "net/http"},
			// Gin framework
			{Signature: "func(*gin.Context)", Framework: "gin"},
			{Name: "Handle*", Signature: "*gin.Context", Framework: "gin"},
			// Echo framework
			{Signature: "func(echo.Context) error", Framework: "echo"},
			// Fiber framework
			{Signature: "func(*fiber.Ctx) error", Framework: "fiber"},
			// Chi framework (uses standard http handlers)
			{Signature: "func(w http.ResponseWriter, r *http.Request)", Framework: "chi"},
			// Gorilla Mux (uses standard http handlers)
			{Signature: "func(w http.ResponseWriter, r *http.Request)", Framework: "gorilla"},
		},
		Command: []PatternMatcher{
			// Cobra commands
			{Type: "*cobra.Command", Framework: "cobra"},
			{Name: "Run", Signature: "*cobra.Command, []string", Framework: "cobra"},
			{Name: "RunE", Signature: "*cobra.Command, []string", Framework: "cobra"},
			// Init functions (package initialization)
			{Name: "init", Package: "*", Framework: "stdlib"},
			// urfave/cli
			{Signature: "*cli.Context", Framework: "urfave/cli"},
		},
		Test: []PatternMatcher{
			{Name: "Test*", Signature: "testing.T", Framework: "testing"},
			{Name: "Benchmark*", Signature: "testing.B", Framework: "testing"},
			{Name: "Fuzz*", Signature: "testing.F", Framework: "testing"},
			{Name: "Example*", Framework: "testing"},
		},
		Lambda: []PatternMatcher{
			// AWS Lambda handlers
			{Signature: "func(context.Context, events.APIGatewayProxyRequest)", Framework: "aws-lambda"},
			{Signature: "func(context.Context, events.SQSEvent)", Framework: "aws-lambda"},
			{Signature: "func(context.Context, events.SNSEvent)", Framework: "aws-lambda"},
			{Signature: "func(context.Context, events.S3Event)", Framework: "aws-lambda"},
			{Signature: "func(context.Context, events.DynamoDBEvent)", Framework: "aws-lambda"},
			// GCP Cloud Functions
			{Signature: "func(http.ResponseWriter, *http.Request)", Framework: "gcp-functions"},
			// Generic lambda pattern
			{Name: "Handler", Signature: "context.Context", Framework: "lambda"},
		},
		GRPC: []PatternMatcher{
			// gRPC server implementations typically end with "Server"
			{Implements: "*Server", Framework: "grpc"},
			// Register server functions
			{Name: "Register*Server", Framework: "grpc"},
		},
	}
}

// DefaultPythonPatterns returns default entry point patterns for Python.
func DefaultPythonPatterns() *EntryPointPatterns {
	return &EntryPointPatterns{
		Language: "python",
		Main: []PatternMatcher{
			// __main__ block handling
			{Name: "__main__", Framework: "stdlib"},
			// Module-level main function
			{Name: "main", Framework: "stdlib"},
		},
		Handler: []PatternMatcher{
			// Flask routes
			{Decorator: "@app.route", Framework: "flask"},
			{Decorator: "@blueprint.route", Framework: "flask"},
			// FastAPI routes
			{Decorator: "@app.get", Framework: "fastapi"},
			{Decorator: "@app.post", Framework: "fastapi"},
			{Decorator: "@app.put", Framework: "fastapi"},
			{Decorator: "@app.delete", Framework: "fastapi"},
			{Decorator: "@app.patch", Framework: "fastapi"},
			{Decorator: "@router.get", Framework: "fastapi"},
			{Decorator: "@router.post", Framework: "fastapi"},
			{Decorator: "@router.put", Framework: "fastapi"},
			{Decorator: "@router.delete", Framework: "fastapi"},
			// Django views
			{Decorator: "@api_view", Framework: "django-rest"},
			{BaseClass: "View", Framework: "django"},
			{BaseClass: "APIView", Framework: "django-rest"},
			{BaseClass: "ViewSet", Framework: "django-rest"},
			{BaseClass: "ModelViewSet", Framework: "django-rest"},
			// Tornado
			{BaseClass: "RequestHandler", Framework: "tornado"},
			// aiohttp
			{Decorator: "@routes.get", Framework: "aiohttp"},
			{Decorator: "@routes.post", Framework: "aiohttp"},
		},
		Command: []PatternMatcher{
			// Click CLI
			{Decorator: "@click.command", Framework: "click"},
			{Decorator: "@click.group", Framework: "click"},
			// argparse (function named main with ArgumentParser usage)
			{Name: "main", Framework: "argparse"},
			// Django management commands
			{BaseClass: "BaseCommand", Framework: "django"},
			// Typer CLI
			{Decorator: "@app.command", Framework: "typer"},
		},
		Test: []PatternMatcher{
			// pytest
			{Name: "test_*", Framework: "pytest"},
			// unittest
			{BaseClass: "TestCase", Framework: "unittest"},
			{Name: "test_*", BaseClass: "TestCase", Framework: "unittest"},
		},
		Lambda: []PatternMatcher{
			// AWS Lambda handlers
			{Name: "lambda_handler", Framework: "aws-lambda"},
			{Name: "handler", Framework: "aws-lambda"},
			// GCP Cloud Functions
			{Decorator: "@functions_framework.http", Framework: "gcp-functions"},
			// Azure Functions
			{Decorator: "@app.function_name", Framework: "azure-functions"},
		},
		GRPC: []PatternMatcher{
			// gRPC servicers
			{BaseClass: "*Servicer", Framework: "grpc"},
			{Name: "*Servicer", Framework: "grpc"},
		},
	}
}

// DefaultTypeScriptPatterns returns default entry point patterns for TypeScript.
func DefaultTypeScriptPatterns() *EntryPointPatterns {
	return &EntryPointPatterns{
		Language: "typescript",
		Main: []PatternMatcher{
			// Entry point scripts typically export default or named exports
			{Name: "main", Framework: "node"},
		},
		Handler: []PatternMatcher{
			// NestJS controllers
			{Decorator: "@Controller", Framework: "nestjs"},
			{Decorator: "@Get", Framework: "nestjs"},
			{Decorator: "@Post", Framework: "nestjs"},
			{Decorator: "@Put", Framework: "nestjs"},
			{Decorator: "@Delete", Framework: "nestjs"},
			{Decorator: "@Patch", Framework: "nestjs"},
			// Express-like handlers
			{Signature: "(req, res)", Framework: "express"},
			{Signature: "(req: Request, res: Response)", Framework: "express"},
			// Next.js API routes
			{Export: "default", Type: "NextApiHandler", Framework: "nextjs"},
			{Name: "getServerSideProps", Framework: "nextjs"},
			{Name: "getStaticProps", Framework: "nextjs"},
			{Name: "getStaticPaths", Framework: "nextjs"},
			// Hono framework
			{Signature: "(c: Context)", Framework: "hono"},
		},
		Command: []PatternMatcher{
			// Commander.js
			{Signature: ".command(", Framework: "commander"},
			// yargs
			{Signature: ".command(", Framework: "yargs"},
		},
		Test: []PatternMatcher{
			// Jest / Mocha / Vitest
			{Name: "it", Framework: "jest"},
			{Name: "test", Framework: "jest"},
			{Name: "describe", Framework: "jest"},
			{Name: "beforeEach", Framework: "jest"},
			{Name: "afterEach", Framework: "jest"},
			{Name: "beforeAll", Framework: "jest"},
			{Name: "afterAll", Framework: "jest"},
		},
		Lambda: []PatternMatcher{
			// AWS Lambda handlers
			{Export: "handler", Signature: "(event, context)", Framework: "aws-lambda"},
			{Name: "handler", Signature: "APIGatewayProxyEvent", Framework: "aws-lambda"},
			// Vercel serverless functions
			{Export: "default", Type: "VercelRequest", Framework: "vercel"},
		},
		GRPC: []PatternMatcher{
			// gRPC implementations
			{Implements: "*Server", Framework: "grpc"},
		},
	}
}

// DefaultJavaScriptPatterns returns default entry point patterns for JavaScript.
func DefaultJavaScriptPatterns() *EntryPointPatterns {
	// JavaScript patterns are similar to TypeScript
	ts := DefaultTypeScriptPatterns()
	ts.Language = "javascript"
	return ts
}
