package ast

// Bash Tree-sitter Node Types
//
// This file documents the tree-sitter node types used by BashParser for symbol extraction.
// The parser uses direct node traversal rather than tree-sitter's query language for
// more precise control over symbol extraction.
//
// Reference: https://github.com/tree-sitter/tree-sitter-bash

// Node type constants for Bash AST traversal.
const (
	// Top-level nodes
	bashNodeProgram = "program"
	bashNodeComment = "comment"

	// Function nodes
	bashNodeFunctionDefinition = "function_definition"
	bashNodeCompoundStatement  = "compound_statement"
	bashNodeWord               = "word"

	// Variable nodes
	bashNodeVariableAssignment = "variable_assignment"
	bashNodeVariableName       = "variable_name"
	bashNodeDeclarationCommand = "declaration_command"

	// Declaration keywords
	bashNodeExport   = "export"
	bashNodeReadonly = "readonly"
	bashNodeLocal    = "local"
	bashNodeDeclare  = "declare"

	// Command nodes
	bashNodeCommand     = "command"
	bashNodeCommandName = "command_name"

	// String nodes
	bashNodeString        = "string"
	bashNodeRawString     = "raw_string"
	bashNodeStringContent = "string_content"
	bashNodeConcatenation = "concatenation"

	// Expression nodes
	bashNodeNumber = "number"

	// Error nodes
	bashNodeERROR = "ERROR"
)

// BashNodeTypes maps symbol kinds to the tree-sitter node types that produce them.
var BashNodeTypes = map[SymbolKind][]string{
	SymbolKindFunction: {bashNodeFunctionDefinition},
	SymbolKindVariable: {bashNodeVariableAssignment},
	SymbolKindConstant: {bashNodeDeclarationCommand}, // readonly variables
	SymbolKindAlias:    {bashNodeCommand},            // when command_name is "alias"
}

// Bash AST Structure Reference
//
// program
// ├── comment (# Comment text or shebang #!/bin/bash)
// │
// ├── variable_assignment
// │   ├── variable_name
// │   ├── =
// │   └── string/number/... (value)
// │
// ├── declaration_command
// │   ├── export/readonly/local/declare
// │   └── variable_assignment
// │       ├── variable_name
// │       ├── =
// │       └── string/number/... (value)
// │
// ├── function_definition
// │   ├── word (function_name)
// │   ├── (
// │   ├── )
// │   └── compound_statement
// │       ├── {
// │       ├── ... (function body)
// │       └── }
// │
// └── command
//     ├── command_name
//     │   └── word (command like "alias", "source", etc.)
//     └── concatenation (for alias: name='value')
//         ├── word (alias_name=)
//         └── raw_string ('alias_value')

// Bash Variable Scopes
//
// Variables in Bash have different scopes based on declaration:
// - No keyword: Global variable (SymbolKindVariable)
// - export: Exported environment variable (SymbolKindVariable, Exported=true)
// - readonly: Read-only constant (SymbolKindConstant)
// - local: Local to function (not extracted at script level)
// - declare: Various attributes (-r for readonly, -x for export, etc.)

// Bash Function Syntax
//
// Functions can be defined in two ways:
// 1. name() { body }
// 2. function name { body }
//
// Both are represented as function_definition in the AST.

// Bash Comments
//
// Comments start with # and extend to end of line.
// The shebang #!/bin/bash is also parsed as a comment.
// Comments preceding a function/variable can serve as documentation.

// Bash Aliases
//
// Aliases are defined using the alias command:
//   alias name='value'
//   alias name="value"
//
// The parser extracts both the alias name and its definition.
