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

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
)

// SourcePattern defines a pattern for identifying data sources.
type SourcePattern struct {
	// Category is the source category (http_input, env_var, file_read, etc.).
	Category SourceCategory

	// FunctionName is a pattern for the function/method name.
	FunctionName string

	// Receiver is a pattern for the receiver type (for methods).
	Receiver string

	// Package is a pattern for the package containing the function.
	Package string

	// Signature is a pattern for the function signature.
	Signature string

	// Description explains what this source represents.
	Description string

	// Confidence is the base confidence level for matches (0.0-1.0).
	Confidence float64

	// compiled regex patterns
	nameRegex *regexp.Regexp
}

// SinkPattern defines a pattern for identifying data sinks.
type SinkPattern struct {
	// Category is the sink category (response, database, file, log, etc.).
	Category SinkCategory

	// FunctionName is a pattern for the function/method name.
	FunctionName string

	// Receiver is a pattern for the receiver type (for methods).
	Receiver string

	// Package is a pattern for the package containing the function.
	Package string

	// Signature is a pattern for the function signature.
	Signature string

	// Description explains what this sink represents.
	Description string

	// IsDangerous indicates if this sink could be a security risk with untrusted data.
	IsDangerous bool

	// Confidence is the base confidence level for matches (0.0-1.0).
	Confidence float64

	// compiled regex patterns
	nameRegex *regexp.Regexp
}

// SourceRegistry holds patterns for identifying data sources.
//
// Thread Safety:
//
//	SourceRegistry is safe for concurrent read access after initialization.
//	Do not modify patterns after construction.
type SourceRegistry struct {
	// patterns maps language to source patterns
	patterns map[string][]SourcePattern
}

// SinkRegistry holds patterns for identifying data sinks.
//
// Thread Safety:
//
//	SinkRegistry is safe for concurrent read access after initialization.
//	Do not modify patterns after construction.
type SinkRegistry struct {
	// patterns maps language to sink patterns
	patterns map[string][]SinkPattern
}

// NewSourceRegistry creates a new SourceRegistry with default patterns.
func NewSourceRegistry() *SourceRegistry {
	r := &SourceRegistry{
		patterns: make(map[string][]SourcePattern),
	}

	r.patterns["go"] = DefaultGoSourcePatterns()
	r.patterns["python"] = DefaultPythonSourcePatterns()
	r.patterns["typescript"] = DefaultTypeScriptSourcePatterns()
	r.patterns["javascript"] = DefaultTypeScriptSourcePatterns() // Same as TypeScript

	return r
}

// NewSinkRegistry creates a new SinkRegistry with default patterns.
func NewSinkRegistry() *SinkRegistry {
	r := &SinkRegistry{
		patterns: make(map[string][]SinkPattern),
	}

	r.patterns["go"] = DefaultGoSinkPatterns()
	r.patterns["python"] = DefaultPythonSinkPatterns()
	r.patterns["typescript"] = DefaultTypeScriptSinkPatterns()
	r.patterns["javascript"] = DefaultTypeScriptSinkPatterns() // Same as TypeScript

	return r
}

// GetPatterns returns source patterns for a language.
func (r *SourceRegistry) GetPatterns(language string) []SourcePattern {
	return r.patterns[strings.ToLower(language)]
}

// GetPatterns returns sink patterns for a language.
func (r *SinkRegistry) GetPatterns(language string) []SinkPattern {
	return r.patterns[strings.ToLower(language)]
}

// RegisterPatterns adds patterns for a language.
func (r *SourceRegistry) RegisterPatterns(language string, patterns []SourcePattern) {
	r.patterns[strings.ToLower(language)] = patterns
}

// RegisterPatterns adds patterns for a language.
func (r *SinkRegistry) RegisterPatterns(language string, patterns []SinkPattern) {
	r.patterns[strings.ToLower(language)] = patterns
}

// MatchSource checks if a symbol matches any source pattern.
func (r *SourceRegistry) MatchSource(sym *ast.Symbol) (*SourcePattern, bool) {
	if sym == nil {
		return nil, false
	}

	patterns := r.GetPatterns(sym.Language)
	for i := range patterns {
		if patterns[i].Match(sym) {
			return &patterns[i], true
		}
	}
	return nil, false
}

// MatchSink checks if a symbol matches any sink pattern.
func (r *SinkRegistry) MatchSink(sym *ast.Symbol) (*SinkPattern, bool) {
	if sym == nil {
		return nil, false
	}

	patterns := r.GetPatterns(sym.Language)
	for i := range patterns {
		if patterns[i].Match(sym) {
			return &patterns[i], true
		}
	}
	return nil, false
}

// Match checks if a symbol matches this source pattern.
func (p *SourcePattern) Match(sym *ast.Symbol) bool {
	if sym == nil {
		return false
	}

	// Match function name
	if p.FunctionName != "" && !matchPattern(p.FunctionName, sym.Name, &p.nameRegex) {
		return false
	}

	// Match receiver
	if p.Receiver != "" && !matchPattern(p.Receiver, sym.Receiver, nil) {
		return false
	}

	// Match package
	if p.Package != "" && !matchPattern(p.Package, sym.Package, nil) {
		return false
	}

	// Match signature
	if p.Signature != "" && !strings.Contains(sym.Signature, p.Signature) {
		return false
	}

	return true
}

// Match checks if a symbol matches this sink pattern.
func (p *SinkPattern) Match(sym *ast.Symbol) bool {
	if sym == nil {
		return false
	}

	// Match function name
	if p.FunctionName != "" && !matchPattern(p.FunctionName, sym.Name, &p.nameRegex) {
		return false
	}

	// Match receiver
	if p.Receiver != "" && !matchPattern(p.Receiver, sym.Receiver, nil) {
		return false
	}

	// Match package
	if p.Package != "" && !matchPattern(p.Package, sym.Package, nil) {
		return false
	}

	// Match signature
	if p.Signature != "" && !strings.Contains(sym.Signature, p.Signature) {
		return false
	}

	return true
}

// matchPattern matches a pattern against a value, supporting glob patterns.
func matchPattern(pattern, value string, compiledRegex **regexp.Regexp) bool {
	if pattern == "*" {
		return true
	}

	if strings.Contains(pattern, "*") {
		// Lazy compile regex
		var regex *regexp.Regexp
		if compiledRegex != nil && *compiledRegex != nil {
			regex = *compiledRegex
		} else {
			regexPattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), "\\*", ".*") + "$"
			var err error
			regex, err = regexp.Compile(regexPattern)
			if err != nil {
				return false
			}
			if compiledRegex != nil {
				*compiledRegex = regex
			}
		}
		return regex.MatchString(value)
	}

	return pattern == value
}

// DefaultGoSourcePatterns returns default data source patterns for Go.
func DefaultGoSourcePatterns() []SourcePattern {
	return []SourcePattern{
		// HTTP request data
		{Category: SourceHTTP, FunctionName: "Body", Receiver: "*http.Request", Description: "HTTP request body", Confidence: 0.95},
		{Category: SourceHTTP, FunctionName: "FormValue", Receiver: "*http.Request", Description: "HTTP form value", Confidence: 0.95},
		{Category: SourceHTTP, FunctionName: "PostFormValue", Receiver: "*http.Request", Description: "HTTP POST form value", Confidence: 0.95},
		{Category: SourceHTTP, FunctionName: "FormFile", Receiver: "*http.Request", Description: "HTTP uploaded file", Confidence: 0.95},
		{Category: SourceHTTP, FunctionName: "URL", Receiver: "*http.Request", Description: "HTTP request URL", Confidence: 0.9},
		{Category: SourceHTTP, FunctionName: "Header", Receiver: "*http.Request", Description: "HTTP request headers", Confidence: 0.9},
		{Category: SourceHTTP, FunctionName: "Cookies", Receiver: "*http.Request", Description: "HTTP cookies", Confidence: 0.9},
		{Category: SourceHTTP, FunctionName: "Cookie", Receiver: "*http.Request", Description: "HTTP cookie", Confidence: 0.9},
		{Category: SourceHTTP, FunctionName: "Query", Signature: "gin.Context", Description: "Gin query parameter", Confidence: 0.95},
		{Category: SourceHTTP, FunctionName: "Param", Signature: "gin.Context", Description: "Gin URL parameter", Confidence: 0.95},
		{Category: SourceHTTP, FunctionName: "PostForm", Signature: "gin.Context", Description: "Gin POST form", Confidence: 0.95},
		{Category: SourceHTTP, FunctionName: "Bind*", Signature: "gin.Context", Description: "Gin request binding", Confidence: 0.95},
		{Category: SourceHTTP, FunctionName: "QueryParam", Signature: "echo.Context", Description: "Echo query parameter", Confidence: 0.95},
		{Category: SourceHTTP, FunctionName: "FormValue", Signature: "echo.Context", Description: "Echo form value", Confidence: 0.95},

		// Environment variables
		{Category: SourceEnv, FunctionName: "Getenv", Package: "os", Description: "Environment variable read", Confidence: 0.95},
		{Category: SourceEnv, FunctionName: "LookupEnv", Package: "os", Description: "Environment variable lookup", Confidence: 0.95},
		{Category: SourceEnv, FunctionName: "Environ", Package: "os", Description: "All environment variables", Confidence: 0.9},
		{Category: SourceEnv, FunctionName: "Get*", Package: "*viper*", Description: "Viper config get", Confidence: 0.9},

		// File reads
		{Category: SourceFile, FunctionName: "ReadFile", Package: "os", Description: "File read", Confidence: 0.9},
		{Category: SourceFile, FunctionName: "ReadFile", Package: "ioutil", Description: "File read (deprecated)", Confidence: 0.9},
		{Category: SourceFile, FunctionName: "Open", Package: "os", Description: "File open", Confidence: 0.85},
		{Category: SourceFile, FunctionName: "Read", Receiver: "*os.File", Description: "File read", Confidence: 0.9},
		{Category: SourceFile, FunctionName: "ReadAll", Package: "io", Description: "Read all from reader", Confidence: 0.85},

		// CLI arguments
		{Category: SourceCLI, FunctionName: "Args", Package: "os", Description: "Command line arguments", Confidence: 0.95},
		{Category: SourceCLI, FunctionName: "Arg", Package: "flag", Description: "Flag argument", Confidence: 0.95},
		{Category: SourceCLI, FunctionName: "*Var", Package: "flag", Description: "Flag variable", Confidence: 0.9},

		// Database results
		{Category: SourceDB, FunctionName: "Scan", Receiver: "*sql.Row*", Description: "Database row scan", Confidence: 0.85},
		{Category: SourceDB, FunctionName: "Scan", Receiver: "*sql.Rows", Description: "Database rows scan", Confidence: 0.85},
		{Category: SourceDB, FunctionName: "Query*", Package: "*sql*", Description: "Database query", Confidence: 0.8},

		// WebSocket
		{Category: SourceWebSocket, FunctionName: "Read*", Receiver: "*websocket.Conn", Description: "WebSocket read", Confidence: 0.9},
		{Category: SourceWebSocket, FunctionName: "ReadMessage", Receiver: "*websocket.Conn", Description: "WebSocket message", Confidence: 0.95},
	}
}

// DefaultGoSinkPatterns returns default data sink patterns for Go.
func DefaultGoSinkPatterns() []SinkPattern {
	return []SinkPattern{
		// HTTP response
		{Category: SinkResponse, FunctionName: "Write", Receiver: "http.ResponseWriter", Description: "HTTP response write", IsDangerous: false, Confidence: 0.95},
		{Category: SinkResponse, FunctionName: "WriteHeader", Receiver: "http.ResponseWriter", Description: "HTTP header write", IsDangerous: false, Confidence: 0.9},
		{Category: SinkResponse, FunctionName: "JSON", Signature: "gin.Context", Description: "Gin JSON response", IsDangerous: false, Confidence: 0.95},
		{Category: SinkResponse, FunctionName: "String", Signature: "gin.Context", Description: "Gin string response", IsDangerous: false, Confidence: 0.95},
		{Category: SinkResponse, FunctionName: "HTML", Signature: "gin.Context", Description: "Gin HTML response", IsDangerous: true, Confidence: 0.95},

		// Database writes
		{Category: SinkDatabase, FunctionName: "Exec", Package: "*sql*", Description: "SQL execution", IsDangerous: true, Confidence: 0.95},
		{Category: SinkDatabase, FunctionName: "Query", Package: "*sql*", Description: "SQL query (may write)", IsDangerous: true, Confidence: 0.85},
		{Category: SinkDatabase, FunctionName: "Prepare", Package: "*sql*", Description: "SQL prepare", IsDangerous: false, Confidence: 0.7},

		// SQL injection sinks (dangerous)
		{Category: SinkSQL, FunctionName: "Exec", Signature: "string", Description: "Raw SQL execution", IsDangerous: true, Confidence: 0.95},
		{Category: SinkSQL, FunctionName: "Query", Signature: "string", Description: "Raw SQL query", IsDangerous: true, Confidence: 0.95},
		{Category: SinkSQL, FunctionName: "Raw", Package: "*gorm*", Description: "GORM raw query", IsDangerous: true, Confidence: 0.95},

		// File writes
		{Category: SinkFile, FunctionName: "WriteFile", Package: "os", Description: "File write", IsDangerous: false, Confidence: 0.95},
		{Category: SinkFile, FunctionName: "Write", Receiver: "*os.File", Description: "File write", IsDangerous: false, Confidence: 0.95},
		{Category: SinkFile, FunctionName: "WriteString", Receiver: "*os.File", Description: "File string write", IsDangerous: false, Confidence: 0.95},

		// Logging
		{Category: SinkLog, FunctionName: "Print*", Package: "log", Description: "Log print", IsDangerous: false, Confidence: 0.9},
		{Category: SinkLog, FunctionName: "Fatal*", Package: "log", Description: "Log fatal", IsDangerous: false, Confidence: 0.9},
		{Category: SinkLog, FunctionName: "Info*", Package: "*slog*", Description: "Structured log info", IsDangerous: false, Confidence: 0.9},
		{Category: SinkLog, FunctionName: "Error*", Package: "*slog*", Description: "Structured log error", IsDangerous: false, Confidence: 0.9},

		// Network
		{Category: SinkNetwork, FunctionName: "Write", Receiver: "net.Conn", Description: "Network write", IsDangerous: false, Confidence: 0.9},
		{Category: SinkNetwork, FunctionName: "Post", Package: "net/http", Description: "HTTP POST", IsDangerous: false, Confidence: 0.85},
		{Category: SinkNetwork, FunctionName: "Get", Package: "net/http", Description: "HTTP GET", IsDangerous: false, Confidence: 0.7},

		// Command execution (dangerous)
		{Category: SinkCommand, FunctionName: "Command", Package: "os/exec", Description: "Command execution", IsDangerous: true, Confidence: 0.95},
		{Category: SinkCommand, FunctionName: "Run", Receiver: "*exec.Cmd", Description: "Command run", IsDangerous: true, Confidence: 0.95},
		{Category: SinkCommand, FunctionName: "Start", Receiver: "*exec.Cmd", Description: "Command start", IsDangerous: true, Confidence: 0.95},
		{Category: SinkCommand, FunctionName: "Output", Receiver: "*exec.Cmd", Description: "Command output", IsDangerous: true, Confidence: 0.95},
	}
}

// DefaultPythonSourcePatterns returns default data source patterns for Python.
func DefaultPythonSourcePatterns() []SourcePattern {
	return []SourcePattern{
		// HTTP request data (Flask/FastAPI/Django)
		{Category: SourceHTTP, FunctionName: "get", Receiver: "request.form", Description: "Flask form data", Confidence: 0.95},
		{Category: SourceHTTP, FunctionName: "get", Receiver: "request.args", Description: "Flask query args", Confidence: 0.95},
		{Category: SourceHTTP, FunctionName: "get", Receiver: "request.json", Description: "Flask JSON body", Confidence: 0.95},
		{Category: SourceHTTP, FunctionName: "body", Receiver: "Request", Description: "FastAPI request body", Confidence: 0.95},
		{Category: SourceHTTP, FunctionName: "GET", Receiver: "request", Description: "Django GET data", Confidence: 0.95},
		{Category: SourceHTTP, FunctionName: "POST", Receiver: "request", Description: "Django POST data", Confidence: 0.95},

		// Environment variables
		{Category: SourceEnv, FunctionName: "getenv", Package: "os", Description: "Environment variable", Confidence: 0.95},
		{Category: SourceEnv, FunctionName: "get", Receiver: "os.environ", Description: "Environment dict", Confidence: 0.95},

		// File reads
		{Category: SourceFile, FunctionName: "read", Receiver: "*file*", Description: "File read", Confidence: 0.9},
		{Category: SourceFile, FunctionName: "readline", Receiver: "*file*", Description: "File readline", Confidence: 0.9},
		{Category: SourceFile, FunctionName: "readlines", Receiver: "*file*", Description: "File readlines", Confidence: 0.9},
		{Category: SourceFile, FunctionName: "read_text", Package: "pathlib", Description: "Pathlib read", Confidence: 0.9},

		// CLI arguments
		{Category: SourceCLI, FunctionName: "argv", Package: "sys", Description: "Command line args", Confidence: 0.95},
		{Category: SourceCLI, FunctionName: "parse_args", Package: "argparse", Description: "Parsed arguments", Confidence: 0.9},

		// Database
		{Category: SourceDB, FunctionName: "fetchone", Receiver: "*cursor*", Description: "DB fetch one", Confidence: 0.85},
		{Category: SourceDB, FunctionName: "fetchall", Receiver: "*cursor*", Description: "DB fetch all", Confidence: 0.85},
		{Category: SourceDB, FunctionName: "fetchmany", Receiver: "*cursor*", Description: "DB fetch many", Confidence: 0.85},
	}
}

// DefaultPythonSinkPatterns returns default data sink patterns for Python.
func DefaultPythonSinkPatterns() []SinkPattern {
	return []SinkPattern{
		// HTTP response
		{Category: SinkResponse, FunctionName: "jsonify", Package: "flask", Description: "Flask JSON response", IsDangerous: false, Confidence: 0.95},
		{Category: SinkResponse, FunctionName: "render_template", Package: "flask", Description: "Flask template render", IsDangerous: true, Confidence: 0.95},
		{Category: SinkResponse, FunctionName: "HttpResponse", Package: "django.http", Description: "Django response", IsDangerous: false, Confidence: 0.95},

		// Database
		{Category: SinkDatabase, FunctionName: "execute", Receiver: "*cursor*", Description: "SQL execute", IsDangerous: true, Confidence: 0.95},
		{Category: SinkDatabase, FunctionName: "executemany", Receiver: "*cursor*", Description: "SQL execute many", IsDangerous: true, Confidence: 0.95},

		// SQL (dangerous)
		{Category: SinkSQL, FunctionName: "execute", Signature: "string", Description: "Raw SQL", IsDangerous: true, Confidence: 0.95},
		{Category: SinkSQL, FunctionName: "raw", Package: "django.db", Description: "Django raw SQL", IsDangerous: true, Confidence: 0.95},

		// File
		{Category: SinkFile, FunctionName: "write", Receiver: "*file*", Description: "File write", IsDangerous: false, Confidence: 0.95},
		{Category: SinkFile, FunctionName: "write_text", Package: "pathlib", Description: "Pathlib write", IsDangerous: false, Confidence: 0.95},

		// Logging
		{Category: SinkLog, FunctionName: "info", Package: "logging", Description: "Log info", IsDangerous: false, Confidence: 0.9},
		{Category: SinkLog, FunctionName: "warning", Package: "logging", Description: "Log warning", IsDangerous: false, Confidence: 0.9},
		{Category: SinkLog, FunctionName: "error", Package: "logging", Description: "Log error", IsDangerous: false, Confidence: 0.9},
		{Category: SinkLog, FunctionName: "print", Description: "Print output", IsDangerous: false, Confidence: 0.8},

		// Command execution (dangerous)
		{Category: SinkCommand, FunctionName: "run", Package: "subprocess", Description: "Subprocess run", IsDangerous: true, Confidence: 0.95},
		{Category: SinkCommand, FunctionName: "call", Package: "subprocess", Description: "Subprocess call", IsDangerous: true, Confidence: 0.95},
		{Category: SinkCommand, FunctionName: "Popen", Package: "subprocess", Description: "Subprocess Popen", IsDangerous: true, Confidence: 0.95},
		{Category: SinkCommand, FunctionName: "system", Package: "os", Description: "OS system", IsDangerous: true, Confidence: 0.95},
		{Category: SinkCommand, FunctionName: "popen", Package: "os", Description: "OS popen", IsDangerous: true, Confidence: 0.95},

		// Eval (dangerous)
		{Category: SinkCommand, FunctionName: "eval", Description: "Python eval", IsDangerous: true, Confidence: 0.99},
		{Category: SinkCommand, FunctionName: "exec", Description: "Python exec", IsDangerous: true, Confidence: 0.99},
	}
}

// DefaultTypeScriptSourcePatterns returns default data source patterns for TypeScript/JavaScript.
func DefaultTypeScriptSourcePatterns() []SourcePattern {
	return []SourcePattern{
		// HTTP request data
		{Category: SourceHTTP, FunctionName: "body", Receiver: "req", Description: "Express request body", Confidence: 0.95},
		{Category: SourceHTTP, FunctionName: "params", Receiver: "req", Description: "Express URL params", Confidence: 0.95},
		{Category: SourceHTTP, FunctionName: "query", Receiver: "req", Description: "Express query params", Confidence: 0.95},
		{Category: SourceHTTP, FunctionName: "headers", Receiver: "req", Description: "Express headers", Confidence: 0.9},
		{Category: SourceHTTP, FunctionName: "cookies", Receiver: "req", Description: "Express cookies", Confidence: 0.9},

		// Environment variables
		{Category: SourceEnv, FunctionName: "env", Receiver: "process", Description: "Process env", Confidence: 0.95},

		// File reads
		{Category: SourceFile, FunctionName: "readFile*", Package: "fs", Description: "FS read file", Confidence: 0.9},
		{Category: SourceFile, FunctionName: "read*", Package: "fs/promises", Description: "FS promises read", Confidence: 0.9},

		// CLI arguments
		{Category: SourceCLI, FunctionName: "argv", Receiver: "process", Description: "Process argv", Confidence: 0.95},

		// Database
		{Category: SourceDB, FunctionName: "find*", Package: "*mongo*", Description: "MongoDB find", Confidence: 0.85},
		{Category: SourceDB, FunctionName: "query", Package: "*pg*", Description: "PostgreSQL query", Confidence: 0.85},
	}
}

// DefaultTypeScriptSinkPatterns returns default data sink patterns for TypeScript/JavaScript.
func DefaultTypeScriptSinkPatterns() []SinkPattern {
	return []SinkPattern{
		// HTTP response
		{Category: SinkResponse, FunctionName: "send", Receiver: "res", Description: "Express send", IsDangerous: false, Confidence: 0.95},
		{Category: SinkResponse, FunctionName: "json", Receiver: "res", Description: "Express JSON", IsDangerous: false, Confidence: 0.95},
		{Category: SinkResponse, FunctionName: "render", Receiver: "res", Description: "Express render", IsDangerous: true, Confidence: 0.95},

		// Database
		{Category: SinkDatabase, FunctionName: "insert*", Package: "*mongo*", Description: "MongoDB insert", IsDangerous: false, Confidence: 0.9},
		{Category: SinkDatabase, FunctionName: "update*", Package: "*mongo*", Description: "MongoDB update", IsDangerous: false, Confidence: 0.9},
		{Category: SinkDatabase, FunctionName: "query", Package: "*pg*", Description: "PostgreSQL query", IsDangerous: true, Confidence: 0.9},

		// SQL (dangerous)
		{Category: SinkSQL, FunctionName: "query", Signature: "string", Description: "Raw SQL query", IsDangerous: true, Confidence: 0.95},

		// File
		{Category: SinkFile, FunctionName: "writeFile*", Package: "fs", Description: "FS write file", IsDangerous: false, Confidence: 0.95},
		{Category: SinkFile, FunctionName: "write*", Package: "fs/promises", Description: "FS promises write", IsDangerous: false, Confidence: 0.95},

		// Logging
		{Category: SinkLog, FunctionName: "log", Receiver: "console", Description: "Console log", IsDangerous: false, Confidence: 0.9},
		{Category: SinkLog, FunctionName: "error", Receiver: "console", Description: "Console error", IsDangerous: false, Confidence: 0.9},

		// Command execution (dangerous)
		{Category: SinkCommand, FunctionName: "exec", Package: "child_process", Description: "Child process exec", IsDangerous: true, Confidence: 0.95},
		{Category: SinkCommand, FunctionName: "spawn", Package: "child_process", Description: "Child process spawn", IsDangerous: true, Confidence: 0.95},
		{Category: SinkCommand, FunctionName: "execSync", Package: "child_process", Description: "Child process execSync", IsDangerous: true, Confidence: 0.95},

		// Eval (dangerous)
		{Category: SinkCommand, FunctionName: "eval", Description: "JavaScript eval", IsDangerous: true, Confidence: 0.99},
		{Category: SinkCommand, FunctionName: "Function", Description: "Function constructor", IsDangerous: true, Confidence: 0.95},
	}
}

// GetDangerousSinks returns all sink patterns that are marked as dangerous.
func (r *SinkRegistry) GetDangerousSinks(language string) []SinkPattern {
	patterns := r.GetPatterns(language)
	dangerous := make([]SinkPattern, 0)
	for _, p := range patterns {
		if p.IsDangerous {
			dangerous = append(dangerous, p)
		}
	}
	return dangerous
}

// Languages returns all registered languages.
func (r *SourceRegistry) Languages() []string {
	langs := make([]string, 0, len(r.patterns))
	for lang := range r.patterns {
		langs = append(langs, lang)
	}
	return langs
}

// Languages returns all registered languages.
func (r *SinkRegistry) Languages() []string {
	langs := make([]string, 0, len(r.patterns))
	for lang := range r.patterns {
		langs = append(langs, lang)
	}
	return langs
}
