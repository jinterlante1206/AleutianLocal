// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package validate

import (
	"context"
	"fmt"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// ASTScanner scans code using AST analysis for dangerous patterns.
//
// Thread Safety: Individual Scan calls are safe for concurrent use.
// The scanner maintains no state between calls. Tree-sitter parsers
// are created per-call to avoid sharing issues.
type ASTScanner struct {
	goPatterns []DangerousPattern
	pyPatterns []DangerousPattern
	jsPatterns []DangerousPattern
}

// NewASTScanner creates a new AST-based scanner.
func NewASTScanner() *ASTScanner {
	return &ASTScanner{
		goPatterns: GoPatterns(),
		pyPatterns: PythonPatterns(),
		jsPatterns: JavaScriptPatterns(),
	}
}

// Scan scans source code for dangerous patterns using AST analysis.
//
// Description:
//
//	Parses the source code using tree-sitter and walks the AST
//	looking for function calls and patterns that match known
//	dangerous patterns. Uses AST analysis to avoid false positives
//	from patterns appearing in comments or strings.
//
// Inputs:
//
//	ctx - Context for cancellation
//	source - Source code bytes
//	language - Language identifier (go, python, javascript, typescript)
//	filePath - File path for error reporting
//
// Outputs:
//
//	[]ValidationWarning - Detected dangerous patterns
//	error - Non-nil if parsing fails
//
// Thread Safety: Safe for concurrent use. Parser created per-call.
func (s *ASTScanner) Scan(ctx context.Context, source []byte, language, filePath string) ([]ValidationWarning, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	parser := sitter.NewParser()
	defer parser.Close()

	var lang *sitter.Language
	var patterns []DangerousPattern

	switch language {
	case "go":
		lang = golang.GetLanguage()
		patterns = s.goPatterns
	case "python":
		lang = python.GetLanguage()
		patterns = s.pyPatterns
	case "javascript", "typescript":
		if language == "typescript" {
			lang = typescript.GetLanguage()
		} else {
			lang = javascript.GetLanguage()
		}
		patterns = s.jsPatterns
	default:
		// Unknown language, return empty
		return []ValidationWarning{}, nil
	}

	parser.SetLanguage(lang)

	tree, err := parser.ParseCtx(ctx, nil, source)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", language, err)
	}
	defer tree.Close()

	root := tree.RootNode()
	warnings := s.walkNode(root, source, patterns, filePath, language)

	return warnings, nil
}

// walkNode recursively walks the AST looking for patterns.
func (s *ASTScanner) walkNode(node *sitter.Node, source []byte, patterns []DangerousPattern, filePath, language string) []ValidationWarning {
	if node == nil {
		return nil
	}

	var warnings []ValidationWarning

	// Check current node against patterns
	if matched := s.matchNode(node, source, patterns, filePath, language); len(matched) > 0 {
		warnings = append(warnings, matched...)
	}

	// Recursively check children
	for i := uint32(0); i < node.ChildCount(); i++ {
		child := node.Child(int(i))
		childWarnings := s.walkNode(child, source, patterns, filePath, language)
		warnings = append(warnings, childWarnings...)
	}

	return warnings
}

// matchNode checks if a node matches any dangerous patterns.
func (s *ASTScanner) matchNode(node *sitter.Node, source []byte, patterns []DangerousPattern, filePath, language string) []ValidationWarning {
	var warnings []ValidationWarning

	nodeType := node.Type()

	for _, pattern := range patterns {
		// Check if node type matches
		if pattern.NodeType != "" && pattern.NodeType != nodeType {
			continue
		}

		// Extract function name from node
		funcName := s.extractFunctionName(node, source, language)
		if funcName == "" {
			continue
		}

		// Check if function name matches any in pattern
		if s.matchesFuncName(funcName, pattern.FuncNames) {
			line := int(node.StartPoint().Row) + 1 // 1-indexed
			warnings = append(warnings, ValidationWarning{
				Type:       pattern.WarnType,
				Pattern:    pattern.Name,
				File:       filePath,
				Line:       line,
				Severity:   pattern.Severity,
				Message:    pattern.Message,
				Suggestion: pattern.Suggestion,
				Blocking:   pattern.Blocking,
			})
		}
	}

	return warnings
}

// extractFunctionName extracts the function name from a call expression.
func (s *ASTScanner) extractFunctionName(node *sitter.Node, source []byte, language string) string {
	nodeType := node.Type()

	switch language {
	case "go":
		return s.extractGoFunctionName(node, source, nodeType)
	case "python":
		return s.extractPythonFunctionName(node, source, nodeType)
	case "javascript", "typescript":
		return s.extractJSFunctionName(node, source, nodeType)
	}

	return ""
}

// extractGoFunctionName extracts function name from Go AST nodes.
func (s *ASTScanner) extractGoFunctionName(node *sitter.Node, source []byte, nodeType string) string {
	switch nodeType {
	case "call_expression":
		// Get the function part of the call
		if funcNode := node.ChildByFieldName("function"); funcNode != nil {
			return string(source[funcNode.StartByte():funcNode.EndByte()])
		}
	case "comment":
		// Check for directive comments like //go:linkname
		content := string(source[node.StartByte():node.EndByte()])
		if strings.HasPrefix(content, "//go:linkname") {
			return "//go:linkname"
		}
	}
	return ""
}

// extractPythonFunctionName extracts function name from Python AST nodes.
func (s *ASTScanner) extractPythonFunctionName(node *sitter.Node, source []byte, nodeType string) string {
	if nodeType != "call" {
		return ""
	}

	// Get the function part of the call
	if funcNode := node.ChildByFieldName("function"); funcNode != nil {
		funcType := funcNode.Type()

		switch funcType {
		case "identifier":
			// Simple function call: eval()
			return string(source[funcNode.StartByte():funcNode.EndByte()])
		case "attribute":
			// Method call: os.system(), subprocess.call()
			return string(source[funcNode.StartByte():funcNode.EndByte()])
		}
	}

	return ""
}

// extractJSFunctionName extracts function name from JS/TS AST nodes.
func (s *ASTScanner) extractJSFunctionName(node *sitter.Node, source []byte, nodeType string) string {
	switch nodeType {
	case "call_expression":
		// Get the function part
		if funcNode := node.ChildByFieldName("function"); funcNode != nil {
			return string(source[funcNode.StartByte():funcNode.EndByte()])
		}
	case "new_expression":
		// new Function()
		if consNode := node.ChildByFieldName("constructor"); consNode != nil {
			return string(source[consNode.StartByte():consNode.EndByte()])
		}
	case "assignment_expression":
		// Check for innerHTML assignment
		if leftNode := node.ChildByFieldName("left"); leftNode != nil {
			leftStr := string(source[leftNode.StartByte():leftNode.EndByte()])
			if strings.HasSuffix(leftStr, "innerHTML") || strings.HasSuffix(leftStr, "outerHTML") {
				return strings.TrimPrefix(leftStr, ".")
			}
		}
	case "member_expression":
		// Check for __proto__ access
		content := string(source[node.StartByte():node.EndByte()])
		if strings.Contains(content, "__proto__") || strings.Contains(content, "constructor.prototype") {
			return "__proto__"
		}
	}

	return ""
}

// matchesFuncName checks if a function name matches any in the pattern list.
func (s *ASTScanner) matchesFuncName(funcName string, patterns []string) bool {
	for _, p := range patterns {
		// Exact match
		if funcName == p {
			return true
		}
		// Suffix match (e.g., "os.system" matches pattern "system")
		if strings.HasSuffix(funcName, "."+p) {
			return true
		}
		// Prefix match (e.g., "exec.Command" matches if funcName contains "exec.Command")
		if strings.Contains(funcName, p) {
			return true
		}
	}
	return false
}

// ScanForSQLInjection checks for SQL injection patterns.
// This requires more context than simple pattern matching.
func (s *ASTScanner) ScanForSQLInjection(ctx context.Context, source []byte, language, filePath string) []ValidationWarning {
	if ctx.Err() != nil {
		return nil
	}

	var warnings []ValidationWarning

	// Look for SQL query string concatenation
	// This is a heuristic check - not AST-based
	lines := strings.Split(string(source), "\n")
	for i, line := range lines {
		// Check for common SQL patterns with string concat
		if s.containsSQLConcat(line) {
			warnings = append(warnings, ValidationWarning{
				Type:       WarnTypeSQLInjection,
				Pattern:    "SQL string concatenation",
				File:       filePath,
				Line:       i + 1,
				Severity:   SeverityHigh,
				Message:    "Potential SQL injection: Query built with string concatenation",
				Suggestion: "Use parameterized queries with placeholders (?, $1, :name)",
				Blocking:   false,
			})
		}
	}

	return warnings
}

// containsSQLConcat checks if a line contains SQL concatenation patterns.
func (s *ASTScanner) containsSQLConcat(line string) bool {
	lower := strings.ToLower(line)

	// Must contain SQL keyword
	hasSQLKeyword := strings.Contains(lower, "select ") ||
		strings.Contains(lower, "insert ") ||
		strings.Contains(lower, "update ") ||
		strings.Contains(lower, "delete ") ||
		strings.Contains(lower, "from ") ||
		strings.Contains(lower, "where ")

	if !hasSQLKeyword {
		return false
	}

	// Must contain string concatenation
	hasConcat := strings.Contains(line, "+") ||
		strings.Contains(line, "fmt.Sprintf") ||
		strings.Contains(line, "f\"") || strings.Contains(line, "f'") ||
		strings.Contains(line, "${") ||
		strings.Contains(line, "%s") || strings.Contains(line, "%v")

	return hasConcat
}
