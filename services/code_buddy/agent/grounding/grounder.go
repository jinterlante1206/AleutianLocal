// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package grounding

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent"
)

// DefaultGrounder orchestrates multiple grounding checks.
//
// This is the main entry point for response validation. It coordinates
// all registered Checkers and aggregates their violations into a single Result.
//
// Thread Safety: Safe for concurrent use after construction.
type DefaultGrounder struct {
	config   Config
	checkers []Checker
}

// NewDefaultGrounder creates a new DefaultGrounder with the given checkers.
//
// Inputs:
//
//	config - Configuration for grounding behavior.
//	checkers - The checkers to run (executed in order).
//
// Outputs:
//
//	*DefaultGrounder - The configured grounder.
func NewDefaultGrounder(config Config, checkers ...Checker) *DefaultGrounder {
	return &DefaultGrounder{
		config:   config,
		checkers: checkers,
	}
}

// Validate implements Grounder.
//
// Description:
//
//	Runs all registered checkers against the response and aggregates
//	violations. Supports short-circuit on critical violations if configured.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout.
//	response - The LLM response content to validate.
//	assembledCtx - The context that was given to the LLM.
//
// Outputs:
//
//	*Result - The aggregated validation result.
//	error - Non-nil only if validation itself fails (not for violations).
//
// Thread Safety: Safe for concurrent use.
func (g *DefaultGrounder) Validate(ctx context.Context, response string, assembledCtx *agent.AssembledContext) (*Result, error) {
	if !g.config.Enabled {
		return &Result{
			Grounded:   true,
			Confidence: 1.0,
		}, nil
	}

	start := time.Now()

	// Create timeout context if configured
	if g.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.config.Timeout)
		defer cancel()
	}

	// Build check input from assembled context
	input, err := g.buildCheckInput(response, assembledCtx)
	if err != nil {
		return nil, fmt.Errorf("building check input: %w", err)
	}

	result := &Result{
		Grounded:   true,
		Confidence: 1.0,
	}

	// Run each checker
	for _, checker := range g.checkers {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		violations := checker.Check(ctx, input)
		result.ChecksRun++

		for _, v := range violations {
			result.AddViolation(v)

			// Short-circuit on critical if configured
			if g.config.ShortCircuitOnCritical && v.Severity == SeverityCritical {
				result.Grounded = false
				result.CheckDuration = time.Since(start)
				return result, nil
			}
		}
	}

	// Determine if grounded based on violations and confidence
	if result.CriticalCount > 0 {
		result.Grounded = false
	} else if result.Confidence < g.config.MinConfidence {
		result.Grounded = false
	} else if len(result.Violations) >= g.config.MaxViolationsBeforeReject {
		result.Grounded = false
	}

	result.CheckDuration = time.Since(start)
	return result, nil
}

// ShouldReject implements Grounder.
//
// Description:
//
//	Determines if a validation result warrants rejecting the response.
//
// Thread Safety: Safe for concurrent use.
func (g *DefaultGrounder) ShouldReject(result *Result) bool {
	if result == nil {
		return false
	}

	if g.config.RejectOnCritical && result.HasCritical() {
		return true
	}

	if len(result.Violations) >= g.config.MaxViolationsBeforeReject {
		return true
	}

	return !result.Grounded
}

// GenerateFootnote implements Grounder.
//
// Description:
//
//	Creates a warning footnote for responses with warnings but no critical
//	violations. Returns empty string if no footnote is needed.
//
// Thread Safety: Safe for concurrent use.
func (g *DefaultGrounder) GenerateFootnote(result *Result) string {
	if result == nil || !g.config.AddFootnoteOnWarning {
		return ""
	}

	// No footnote for critical violations (response should be rejected)
	if result.HasCritical() {
		return ""
	}

	// No footnote if no warnings
	if !result.HasWarnings() {
		return ""
	}

	var warnings []string
	for _, v := range result.Violations {
		if v.Severity == SeverityWarning {
			warnings = append(warnings, v.Message)
		}
	}

	if len(warnings) == 0 {
		return ""
	}

	return fmt.Sprintf("\n\n---\n⚠️ **Grounding warnings**: %s", strings.Join(warnings, "; "))
}

// buildCheckInput constructs the CheckInput from AssembledContext.
func (g *DefaultGrounder) buildCheckInput(response string, assembledCtx *agent.AssembledContext) (*CheckInput, error) {
	if assembledCtx == nil {
		return &CheckInput{
			Response: response,
		}, nil
	}

	// Truncate response if configured
	scanResponse := response
	if g.config.MaxResponseScanLength > 0 && len(response) > g.config.MaxResponseScanLength {
		scanResponse = response[:g.config.MaxResponseScanLength]
	}

	// Detect primary language from file extensions in context
	projectLang := g.detectProjectLanguage(assembledCtx)

	evidenceIndex := g.buildEvidenceIndex(assembledCtx)

	// Extract user question for semantic drift checking
	userQuestion := ExtractUserQuestion(assembledCtx)

	input := &CheckInput{
		Response:      scanResponse,
		UserQuestion:  userQuestion,
		ProjectLang:   projectLang,
		CodeContext:   assembledCtx.CodeContext,
		ToolResults:   convertToolResults(assembledCtx.ToolResults),
		EvidenceIndex: evidenceIndex,
		KnownFiles:    evidenceIndex.Files, // Also set KnownFiles for phantom file checker
	}

	return input, nil
}

// detectProjectLanguage determines the primary language from context files.
func (g *DefaultGrounder) detectProjectLanguage(assembledCtx *agent.AssembledContext) string {
	if assembledCtx == nil {
		return ""
	}

	// Count occurrences of each language
	langCounts := make(map[string]int)

	for _, entry := range assembledCtx.CodeContext {
		lang := DetectLanguageFromPath(entry.FilePath)
		if lang != "" {
			langCounts[lang]++
		}
	}

	// Find the most common language
	maxCount := 0
	primaryLang := ""
	for lang, count := range langCounts {
		if count > maxCount {
			maxCount = count
			primaryLang = lang
		}
	}

	return primaryLang
}

// buildEvidenceIndex creates an EvidenceIndex from the AssembledContext.
//
// This index tracks exactly what was shown to the LLM so we can verify
// that claims in the response are grounded in actual evidence.
func (g *DefaultGrounder) buildEvidenceIndex(assembledCtx *agent.AssembledContext) *EvidenceIndex {
	if assembledCtx == nil {
		return NewEvidenceIndex()
	}

	idx := NewEvidenceIndex()
	var contentBuilder strings.Builder

	// Index code context
	for _, entry := range assembledCtx.CodeContext {
		normalizedPath := normalizePath(entry.FilePath)

		// Add file paths
		idx.Files[entry.FilePath] = true
		idx.Files[normalizedPath] = true
		idx.FileBasenames[filepath.Base(entry.FilePath)] = true

		// Store content for line validation (use normalized path as canonical key)
		// Only store once to save memory; lookups should normalize paths first
		idx.FileContents[normalizedPath] = entry.Content

		// Count lines for line number validation
		// Line count = number of newlines + 1 (last line may not end with newline)
		lineCount := strings.Count(entry.Content, "\n") + 1
		if entry.Content == "" {
			lineCount = 0 // Empty file has 0 lines
		}
		idx.FileLines[normalizedPath] = lineCount

		// Add symbols
		if entry.SymbolName != "" {
			idx.Symbols[entry.SymbolName] = true
		}

		// Extract symbols from content (basic extraction)
		extractSymbols(entry.Content, idx.Symbols)

		// Extract detailed symbol information for attribute validation
		symbolDetails := extractSymbolDetails(entry.Content, normalizedPath)
		for _, sym := range symbolDetails {
			idx.Symbols[sym.Name] = true
			idx.SymbolDetails[sym.Name] = append(idx.SymbolDetails[sym.Name], sym)
		}

		// Extract imports for relationship validation
		imports := extractImports(entry.Content, entry.FilePath)
		if len(imports) > 0 {
			idx.Imports[normalizedPath] = imports
		}

		// Extract function calls for relationship validation
		calls := extractFunctionCalls(entry.Content, entry.FilePath)
		for funcName, callees := range calls {
			idx.CallsWithin[funcName] = callees
		}

		// Detect language from extension
		lang := DetectLanguageFromPath(entry.FilePath)
		if lang != "" {
			idx.Languages[lang] = true
		}

		// Accumulate raw content
		contentBuilder.WriteString(entry.Content)
		contentBuilder.WriteString("\n")
	}

	// Index tool results
	for _, result := range assembledCtx.ToolResults {
		contentBuilder.WriteString(result.Output)
		contentBuilder.WriteString("\n")

		// Extract file paths from tool output
		extractFilePathsFromText(result.Output, idx.Files, idx.FileBasenames)
	}

	idx.RawContent = contentBuilder.String()

	return idx
}

// convertToolResults converts agent.ToolResult to grounding.ToolResult.
func convertToolResults(agentResults []agent.ToolResult) []ToolResult {
	results := make([]ToolResult, len(agentResults))
	for i, r := range agentResults {
		results[i] = ToolResult{
			InvocationID: r.InvocationID,
			Output:       r.Output,
		}
	}
	return results
}

// DetectLanguageFromPath returns the language based on file extension.
//
// Inputs:
//
//	filePath - The file path to analyze.
//
// Outputs:
//
//	string - The detected language (e.g., "go", "python"), or empty if unknown.
func DetectLanguageFromPath(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".jsx":
		return "javascript"
	case ".tsx":
		return "typescript"
	case ".java":
		return "java"
	case ".rs":
		return "rust"
	case ".c", ".h":
		return "c"
	case ".cpp", ".hpp", ".cc":
		return "cpp"
	default:
		return ""
	}
}

// extractSymbols performs basic symbol extraction from code.
// This is a simplified extraction; more sophisticated analysis uses AST parsing.
func extractSymbols(content string, symbols map[string]bool) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Go function definitions
		if strings.HasPrefix(line, "func ") {
			// Extract function name
			rest := strings.TrimPrefix(line, "func ")
			// Handle method receivers: func (r *Receiver) Name(
			if strings.HasPrefix(rest, "(") {
				if idx := strings.Index(rest, ")"); idx != -1 {
					rest = strings.TrimSpace(rest[idx+1:])
				}
			}
			if idx := strings.Index(rest, "("); idx != -1 {
				name := strings.TrimSpace(rest[:idx])
				if name != "" {
					symbols[name] = true
				}
			}
		}

		// Go type definitions
		if strings.HasPrefix(line, "type ") {
			rest := strings.TrimPrefix(line, "type ")
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				symbols[fields[0]] = true
			}
		}

		// Python function definitions
		if strings.HasPrefix(line, "def ") {
			rest := strings.TrimPrefix(line, "def ")
			if idx := strings.Index(rest, "("); idx != -1 {
				name := strings.TrimSpace(rest[:idx])
				if name != "" {
					symbols[name] = true
				}
			}
		}

		// Python class definitions
		if strings.HasPrefix(line, "class ") {
			rest := strings.TrimPrefix(line, "class ")
			if idx := strings.Index(rest, "("); idx != -1 {
				name := strings.TrimSpace(rest[:idx])
				if name != "" {
					symbols[name] = true
				}
			} else if idx := strings.Index(rest, ":"); idx != -1 {
				name := strings.TrimSpace(rest[:idx])
				if name != "" {
					symbols[name] = true
				}
			}
		}
	}
}

// extractFilePathsFromText extracts file paths mentioned in text.
func extractFilePathsFromText(text string, files map[string]bool, basenames map[string]bool) {
	// Look for common file path patterns
	words := strings.Fields(text)
	for _, word := range words {
		// Clean up word
		word = strings.Trim(word, "\"'`()[]{},:;")

		// Check if it looks like a file path
		if strings.Contains(word, "/") || strings.Contains(word, "\\") {
			// Has path separator
			if hasCodeExtension(word) {
				files[word] = true
				files[normalizePath(word)] = true
				basenames[filepath.Base(word)] = true
			}
		} else if hasCodeExtension(word) {
			// Just a filename with code extension
			basenames[word] = true
		}
	}
}

// hasCodeExtension checks if a path has a recognized code file extension.
func hasCodeExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".py", ".js", ".ts", ".jsx", ".tsx", ".java", ".rs",
		".c", ".cpp", ".h", ".hpp", ".cc", ".md", ".yaml", ".yml", ".json":
		return true
	default:
		return false
	}
}

// extractSymbolDetails extracts detailed symbol information from Go code.
// This provides richer data than extractSymbols for attribute validation.
//
// Inputs:
//
//	content - The source code content.
//	filePath - The file path for this code.
//
// Outputs:
//
//	[]SymbolInfo - Detailed symbol information extracted from the code.
func extractSymbolDetails(content string, filePath string) []SymbolInfo {
	var symbols []SymbolInfo
	lines := strings.Split(content, "\n")

	for lineNum, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// Go function/method definitions
		if strings.HasPrefix(trimmedLine, "func ") {
			if sym := parseGoFunctionSignature(trimmedLine, filePath, lineNum+1); sym != nil {
				symbols = append(symbols, *sym)
			}
		}

		// Go type definitions
		if strings.HasPrefix(trimmedLine, "type ") {
			if sym := parseGoTypeDefinition(lines, lineNum, filePath); sym != nil {
				symbols = append(symbols, *sym)
			}
		}
	}

	return symbols
}

// parseGoFunctionSignature parses a Go function/method signature.
// Example: "func (r *Config) Parse(ctx context.Context, data []byte) (*Result, error) {"
func parseGoFunctionSignature(line string, filePath string, lineNum int) *SymbolInfo {
	rest := strings.TrimPrefix(line, "func ")

	sym := &SymbolInfo{
		File: filePath,
		Line: lineNum,
		Kind: "function",
	}

	// Handle method receivers: func (r *Receiver) Name(
	if strings.HasPrefix(rest, "(") {
		closeIdx := strings.Index(rest, ")")
		if closeIdx == -1 {
			return nil
		}
		receiverPart := rest[1:closeIdx]
		sym.Receiver = extractReceiverType(receiverPart)
		sym.Kind = "method"
		rest = strings.TrimSpace(rest[closeIdx+1:])
	}

	// Extract function name
	parenIdx := strings.Index(rest, "(")
	if parenIdx == -1 {
		return nil
	}
	sym.Name = strings.TrimSpace(rest[:parenIdx])
	if sym.Name == "" {
		return nil
	}
	rest = rest[parenIdx:]

	// Extract parameters
	sym.Parameters = extractFunctionParams(rest)

	// Extract return types
	sym.ReturnTypes = extractReturnTypes(rest)

	return sym
}

// extractReceiverType extracts the type from a receiver declaration.
// Example: "r *Config" -> "*Config"
func extractReceiverType(receiver string) string {
	parts := strings.Fields(receiver)
	if len(parts) == 0 {
		return ""
	}
	// Last part is the type
	return parts[len(parts)-1]
}

// extractFunctionParams extracts parameter types from a function signature.
// Example: "(ctx context.Context, data []byte)" -> ["context.Context", "[]byte"]
func extractFunctionParams(sig string) []string {
	if !strings.HasPrefix(sig, "(") {
		return nil
	}

	// Find the matching closing paren for params
	depth := 0
	closeIdx := -1
	for i, ch := range sig {
		if ch == '(' {
			depth++
		} else if ch == ')' {
			depth--
			if depth == 0 {
				closeIdx = i
				break
			}
		}
	}
	if closeIdx == -1 {
		return nil
	}

	paramPart := sig[1:closeIdx]
	if paramPart == "" {
		return nil
	}

	// Parse comma-separated parameters
	var params []string
	parts := splitByComma(paramPart)
	for _, part := range parts {
		// Each part is like "ctx context.Context" or "data []byte"
		// We want the type, which is the last space-separated token
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) == 0 {
			continue
		}
		// Last field is the type
		params = append(params, fields[len(fields)-1])
	}

	return params
}

// extractReturnTypes extracts return types from a function signature.
// Example: "(...) (*Result, error) {" -> ["*Result", "error"]
// Example: "(...) error {" -> ["error"]
func extractReturnTypes(sig string) []string {
	// Find the params section end
	depth := 0
	paramsEndIdx := -1
	for i, ch := range sig {
		if ch == '(' {
			depth++
		} else if ch == ')' {
			depth--
			if depth == 0 {
				paramsEndIdx = i
				break
			}
		}
	}
	if paramsEndIdx == -1 {
		return nil
	}

	rest := strings.TrimSpace(sig[paramsEndIdx+1:])
	if rest == "" || rest == "{" {
		return nil
	}

	// Remove trailing { if present
	rest = strings.TrimSuffix(strings.TrimSpace(rest), "{")
	rest = strings.TrimSpace(rest)

	if rest == "" {
		return nil
	}

	// Check if it's a tuple return: (T1, T2)
	if strings.HasPrefix(rest, "(") {
		closeIdx := strings.LastIndex(rest, ")")
		if closeIdx > 0 {
			returnPart := rest[1:closeIdx]
			return splitByComma(returnPart)
		}
	}

	// Single return type
	return []string{rest}
}

// parseGoTypeDefinition parses a Go type definition.
// Returns SymbolInfo with fields (for structs) or methods (for interfaces).
func parseGoTypeDefinition(lines []string, startLine int, filePath string) *SymbolInfo {
	if startLine >= len(lines) {
		return nil
	}

	line := strings.TrimSpace(lines[startLine])
	rest := strings.TrimPrefix(line, "type ")

	// Get type name
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		return nil
	}

	sym := &SymbolInfo{
		Name: fields[0],
		File: filePath,
		Line: startLine + 1,
	}

	// Determine kind and extract details
	if strings.Contains(rest, "struct") {
		sym.Kind = "struct"
		sym.Fields = extractStructFields(lines, startLine)
	} else if strings.Contains(rest, "interface") {
		sym.Kind = "interface"
		sym.Methods = extractInterfaceMethods(lines, startLine)
	} else {
		sym.Kind = "type"
	}

	return sym
}

// extractStructFields extracts field names from a struct definition.
func extractStructFields(lines []string, startLine int) []string {
	var fields []string
	inStruct := false

	for i := startLine; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		if strings.Contains(line, "struct") && strings.Contains(line, "{") {
			inStruct = true
			// Check if struct is defined on one line
			if strings.Contains(line, "}") {
				// One-liner struct, extract fields
				braceStart := strings.Index(line, "{")
				braceEnd := strings.Index(line, "}")
				if braceStart != -1 && braceEnd > braceStart {
					fieldPart := line[braceStart+1 : braceEnd]
					return parseFieldPart(fieldPart)
				}
				return nil
			}
			continue
		}

		if inStruct {
			if line == "}" {
				break
			}

			// Skip comments and embedded types
			if strings.HasPrefix(line, "//") || line == "" {
				continue
			}

			// Extract field name (first word before type)
			parts := strings.Fields(line)
			if len(parts) >= 2 && !strings.HasPrefix(parts[0], "*") {
				// Regular field: Name Type
				fieldName := parts[0]
				if isValidIdentifier(fieldName) {
					fields = append(fields, fieldName)
				}
			} else if len(parts) == 1 {
				// Could be embedded type
				continue
			}
		}
	}

	return fields
}

// extractInterfaceMethods extracts method names from an interface definition.
func extractInterfaceMethods(lines []string, startLine int) []string {
	var methods []string
	inInterface := false

	for i := startLine; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		if strings.Contains(line, "interface") && strings.Contains(line, "{") {
			inInterface = true
			continue
		}

		if inInterface {
			if line == "}" {
				break
			}

			// Skip comments
			if strings.HasPrefix(line, "//") || line == "" {
				continue
			}

			// Extract method name (word before opening paren)
			parenIdx := strings.Index(line, "(")
			if parenIdx > 0 {
				methodName := strings.TrimSpace(line[:parenIdx])
				if isValidIdentifier(methodName) {
					methods = append(methods, methodName)
				}
			}
		}
	}

	return methods
}

// splitByComma splits a string by commas, respecting nested brackets.
func splitByComma(s string) []string {
	var parts []string
	var current strings.Builder
	depth := 0

	for _, ch := range s {
		switch ch {
		case '(', '[', '{':
			depth++
			current.WriteRune(ch)
		case ')', ']', '}':
			depth--
			current.WriteRune(ch)
		case ',':
			if depth == 0 {
				if str := strings.TrimSpace(current.String()); str != "" {
					parts = append(parts, str)
				}
				current.Reset()
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}

	if str := strings.TrimSpace(current.String()); str != "" {
		parts = append(parts, str)
	}

	return parts
}

// parseFieldPart parses inline struct fields like "Name string; Value int"
func parseFieldPart(fieldPart string) []string {
	var fields []string
	parts := strings.Split(fieldPart, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tokens := strings.Fields(part)
		if len(tokens) >= 2 && isValidIdentifier(tokens[0]) {
			fields = append(fields, tokens[0])
		}
	}
	return fields
}

// isValidIdentifier checks if a string is a valid Go identifier.
func isValidIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, ch := range s {
		if i == 0 {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_') {
				return false
			}
		} else {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_') {
				return false
			}
		}
	}
	return true
}

// extractImports extracts import statements from file content.
// Supports Go and Python import syntax.
//
// Inputs:
//
//	content - The source code content.
//	filePath - The file path for language detection.
//
// Outputs:
//
//	[]ImportInfo - List of imports found in the file.
func extractImports(content string, filePath string) []ImportInfo {
	lang := DetectLanguageFromPath(filePath)

	switch lang {
	case "go":
		return extractGoImports(content)
	case "python":
		return extractPythonImports(content)
	default:
		return nil
	}
}

// extractGoImports extracts import statements from Go code.
func extractGoImports(content string) []ImportInfo {
	var imports []ImportInfo
	lines := strings.Split(content, "\n")
	inImportBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Start of import block
		if strings.HasPrefix(trimmed, "import (") {
			inImportBlock = true
			continue
		}

		// End of import block
		if inImportBlock && trimmed == ")" {
			inImportBlock = false
			continue
		}

		// Single-line import
		if strings.HasPrefix(trimmed, "import ") && !strings.Contains(trimmed, "(") {
			rest := strings.TrimPrefix(trimmed, "import ")
			if imp := parseGoImportLine(rest); imp != nil {
				imports = append(imports, *imp)
			}
			continue
		}

		// Inside import block
		if inImportBlock && trimmed != "" && !strings.HasPrefix(trimmed, "//") {
			if imp := parseGoImportLine(trimmed); imp != nil {
				imports = append(imports, *imp)
			}
		}
	}

	return imports
}

// parseGoImportLine parses a single Go import line.
// Handles: "pkg/path", alias "pkg/path", . "pkg/path", _ "pkg/path"
func parseGoImportLine(line string) *ImportInfo {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	// Remove inline comments
	if idx := strings.Index(line, "//"); idx != -1 {
		line = strings.TrimSpace(line[:idx])
	}

	var alias, path string

	// Check for aliased import: alias "path"
	parts := strings.Fields(line)
	if len(parts) == 2 {
		alias = parts[0]
		path = strings.Trim(parts[1], "\"")
	} else if len(parts) == 1 {
		path = strings.Trim(parts[0], "\"")
		// Extract package name from path as alias
		alias = extractPackageName(path)
	} else {
		return nil
	}

	if path == "" {
		return nil
	}

	return &ImportInfo{
		Path:  path,
		Alias: alias,
	}
}

// extractPackageName extracts the package name from an import path.
// "github.com/pkg/errors" -> "errors"
func extractPackageName(path string) string {
	if idx := strings.LastIndex(path, "/"); idx != -1 {
		return path[idx+1:]
	}
	return path
}

// extractPythonImports extracts import statements from Python code.
func extractPythonImports(content string) []ImportInfo {
	var imports []ImportInfo
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip comments
		if strings.HasPrefix(trimmed, "#") {
			continue
		}

		// "import module" or "import module as alias"
		if strings.HasPrefix(trimmed, "import ") && !strings.HasPrefix(trimmed, "import (") {
			rest := strings.TrimPrefix(trimmed, "import ")
			// Handle "import a, b, c"
			for _, part := range strings.Split(rest, ",") {
				part = strings.TrimSpace(part)
				// Handle "module as alias"
				if strings.Contains(part, " as ") {
					subParts := strings.Split(part, " as ")
					if len(subParts) == 2 {
						imports = append(imports, ImportInfo{
							Path:  strings.TrimSpace(subParts[0]),
							Alias: strings.TrimSpace(subParts[1]),
						})
					}
				} else if part != "" {
					imports = append(imports, ImportInfo{
						Path:  part,
						Alias: part,
					})
				}
			}
			continue
		}

		// "from module import ..."
		if strings.HasPrefix(trimmed, "from ") {
			rest := strings.TrimPrefix(trimmed, "from ")
			if idx := strings.Index(rest, " import "); idx != -1 {
				module := strings.TrimSpace(rest[:idx])
				if module != "" {
					imports = append(imports, ImportInfo{
						Path:  module,
						Alias: module,
					})
				}
			}
		}
	}

	return imports
}

// extractFunctionCalls extracts function calls from code.
// Returns a map of function name -> list of functions it calls.
//
// Inputs:
//
//	content - The source code content.
//	filePath - The file path for language detection.
//
// Outputs:
//
//	map[string][]string - Map of caller function to called functions.
func extractFunctionCalls(content string, filePath string) map[string][]string {
	lang := DetectLanguageFromPath(filePath)

	switch lang {
	case "go":
		return extractGoFunctionCalls(content)
	default:
		// Only Go supported for now
		return nil
	}
}

// extractGoFunctionCalls extracts function calls from Go code.
func extractGoFunctionCalls(content string) map[string][]string {
	result := make(map[string][]string)
	lines := strings.Split(content, "\n")

	var currentFunc string
	braceDepth := 0
	inFunc := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect function start
		if strings.HasPrefix(trimmed, "func ") {
			funcName := extractFuncNameFromLine(trimmed)
			if funcName != "" {
				currentFunc = funcName
				inFunc = true
				braceDepth = 0
			}
		}

		// Track brace depth to know when we exit a function
		if inFunc {
			braceDepth += strings.Count(line, "{")
			braceDepth -= strings.Count(line, "}")

			if braceDepth <= 0 && currentFunc != "" {
				// End of function
				inFunc = false
				currentFunc = ""
				continue
			}

			// Extract function calls from this line
			if currentFunc != "" {
				calls := extractCallsFromLine(trimmed)
				if len(calls) > 0 {
					result[currentFunc] = appendUnique(result[currentFunc], calls...)
				}
			}
		}
	}

	return result
}

// extractFuncNameFromLine extracts the function name from a func declaration line.
func extractFuncNameFromLine(line string) string {
	rest := strings.TrimPrefix(line, "func ")

	// Handle method receivers: func (r *Receiver) Name(
	if strings.HasPrefix(rest, "(") {
		if idx := strings.Index(rest, ")"); idx != -1 {
			rest = strings.TrimSpace(rest[idx+1:])
		}
	}

	// Extract name before (
	if idx := strings.Index(rest, "("); idx != -1 {
		return strings.TrimSpace(rest[:idx])
	}
	return ""
}

// extractCallsFromLine extracts function calls from a line of code.
func extractCallsFromLine(line string) []string {
	var calls []string

	// Skip comments
	if strings.HasPrefix(strings.TrimSpace(line), "//") {
		return nil
	}

	// Simple pattern: identifier followed by (
	// This catches: foo(), pkg.Foo(), obj.Method()
	words := strings.FieldsFunc(line, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '.')
	})

	for _, word := range words {
		// Skip keywords and builtins
		if isGoKeyword(word) || isGoBuiltin(word) {
			continue
		}

		// Check if this word is followed by ( in the original line
		idx := strings.Index(line, word)
		if idx == -1 {
			continue
		}

		afterWord := line[idx+len(word):]
		afterWord = strings.TrimSpace(afterWord)

		if strings.HasPrefix(afterWord, "(") {
			// This looks like a function call
			// Extract just the function name (last part after .)
			if dotIdx := strings.LastIndex(word, "."); dotIdx != -1 {
				funcName := word[dotIdx+1:]
				if funcName != "" && !isGoKeyword(funcName) && !isGoBuiltin(funcName) {
					calls = append(calls, funcName)
				}
			} else if word != "" && !isGoKeyword(word) && !isGoBuiltin(word) {
				calls = append(calls, word)
			}
		}
	}

	return calls
}

// isGoBuiltin returns true if the word is a Go built-in function.
// These are excluded from call tracking as they're not user-defined calls.
func isGoBuiltin(word string) bool {
	builtins := map[string]bool{
		"make": true, "new": true, "len": true, "cap": true, "append": true,
		"copy": true, "delete": true, "close": true, "panic": true, "recover": true,
		"print": true, "println": true, "complex": true, "real": true, "imag": true,
	}
	return builtins[word]
}

// appendUnique appends items to a slice, avoiding duplicates.
func appendUnique(slice []string, items ...string) []string {
	seen := make(map[string]bool)
	for _, s := range slice {
		seen[s] = true
	}
	for _, item := range items {
		if !seen[item] {
			slice = append(slice, item)
			seen[item] = true
		}
	}
	return slice
}
