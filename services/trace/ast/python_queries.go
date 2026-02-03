// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package ast

// Python Tree-sitter Node Types
//
// This file documents the tree-sitter node types used by PythonParser for symbol extraction.
// The parser uses direct node traversal rather than tree-sitter's query language for
// more precise control over symbol extraction.
//
// Reference: https://github.com/tree-sitter/tree-sitter-python/blob/master/src/grammar.json

// Node type constants for Python AST traversal.
//
// These constants define the tree-sitter node types that PythonParser uses to identify
// different Python language constructs. They match the node types defined in tree-sitter-python.
const (
	// Top-level nodes
	pyNodeModule = "module"

	// Import-related nodes
	pyNodeImportStatement     = "import_statement"
	pyNodeImportFromStatement = "import_from_statement"
	pyNodeDottedName          = "dotted_name"
	pyNodeAliasedImport       = "aliased_import"
	pyNodeRelativeImport      = "relative_import"
	pyNodeImportPrefix        = "import_prefix"
	pyNodeWildcardImport      = "wildcard_import"

	// Function-related nodes
	pyNodeFunctionDefinition      = "function_definition"
	pyNodeAsyncFunctionDefinition = "async_function_definition"
	pyNodeParameters              = "parameters"
	pyNodeTypedParameter          = "typed_parameter"
	pyNodeDefaultParameter        = "default_parameter"
	pyNodeTypedDefaultParameter   = "typed_default_parameter"
	pyNodeListSplatPattern        = "list_splat_pattern"
	pyNodeDictSplatPattern        = "dictionary_splat_pattern"

	// Class-related nodes
	pyNodeClassDefinition = "class_definition"
	pyNodeArgumentList    = "argument_list"
	pyNodeBlock           = "block"

	// Decorator-related nodes
	pyNodeDecoratedDefinition = "decorated_definition"
	pyNodeDecorator           = "decorator"

	// Variable/assignment nodes
	pyNodeExpressionStatement = "expression_statement"
	pyNodeAssignment          = "assignment"
	pyNodeAugmentedAssignment = "augmented_assignment"

	// Type annotation nodes
	pyNodeType = "type"

	// Identifier nodes
	pyNodeIdentifier = "identifier"
	pyNodeAttribute  = "attribute"

	// Literal nodes
	pyNodeString  = "string"
	pyNodeComment = "comment"

	// Expression nodes
	pyNodeCall = "call"
)

// PythonNodeTypes maps symbol kinds to the tree-sitter node types that produce them.
//
// This provides a reference for understanding how Python constructs map to symbols:
//
//	KindPackage   <- module (first docstring)
//	KindImport    <- import_statement, import_from_statement
//	KindFunction  <- function_definition, async_function_definition
//	KindMethod    <- function_definition (inside class body)
//	KindClass     <- class_definition
//	KindProperty  <- function_definition with @property decorator
//	KindField     <- assignment in class body
//	KindVariable  <- assignment at module level
//	KindConstant  <- assignment at module level with ALL_CAPS name
//	KindDecorator <- decorator
var PythonNodeTypes = map[SymbolKind][]string{
	SymbolKindPackage:   {pyNodeModule},
	SymbolKindImport:    {pyNodeImportStatement, pyNodeImportFromStatement},
	SymbolKindFunction:  {pyNodeFunctionDefinition, pyNodeAsyncFunctionDefinition},
	SymbolKindMethod:    {pyNodeFunctionDefinition}, // inside class
	SymbolKindClass:     {pyNodeClassDefinition},
	SymbolKindProperty:  {pyNodeFunctionDefinition}, // with @property
	SymbolKindField:     {pyNodeAssignment},         // inside class
	SymbolKindVariable:  {pyNodeAssignment},         // at module level
	SymbolKindConstant:  {pyNodeAssignment},         // module level, ALL_CAPS
	SymbolKindDecorator: {pyNodeDecorator},
}

// Python AST Structure Reference
//
// module
// ├── expression_statement (for docstrings)
// │   └── string
// ├── import_statement
// │   ├── dotted_name
// │   └── aliased_import
// │       ├── dotted_name
// │       └── identifier (alias)
// ├── import_from_statement
// │   ├── relative_import
// │   │   ├── import_prefix (dots)
// │   │   └── dotted_name (optional)
// │   ├── dotted_name
// │   ├── identifier+ (imported names)
// │   ├── aliased_import
// │   └── wildcard_import
// ├── function_definition
// │   ├── identifier (name)
// │   ├── parameters
// │   │   ├── identifier
// │   │   ├── typed_parameter
// │   │   │   ├── identifier
// │   │   │   └── type
// │   │   ├── default_parameter
// │   │   └── typed_default_parameter
// │   ├── type (return type)
// │   └── block
// │       └── expression_statement (docstring)
// ├── async_function_definition
// │   └── function_definition
// ├── class_definition
// │   ├── identifier (name)
// │   ├── argument_list (bases)
// │   └── block
// │       ├── expression_statement (docstring)
// │       ├── function_definition (methods)
// │       ├── decorated_definition
// │       └── expression_statement
// │           └── assignment (class variables)
// ├── decorated_definition
// │   ├── decorator+
// │   │   └── identifier or call or attribute
// │   └── (function_definition | class_definition)
// └── expression_statement
//     └── assignment (module-level variables)
//         ├── identifier (name)
//         ├── type (optional annotation)
//         └── expression (value)

// Example tree-sitter query patterns for reference.
//
// These patterns could be used with tree-sitter's query language if we decide
// to switch from direct traversal. They're included here for documentation.
//
// Note: The current implementation uses direct node traversal because:
// 1. It provides more precise control over what we extract
// 2. It's easier to debug and modify
// 3. It handles edge cases more gracefully
const (
	// QueryPythonFunctions is a tree-sitter query pattern for function declarations.
	QueryPythonFunctions = `
(function_definition
  name: (identifier) @name
  parameters: (parameters) @params
  return_type: (type)? @return_type) @func
`

	// QueryPythonAsyncFunctions is a tree-sitter query pattern for async functions.
	QueryPythonAsyncFunctions = `
(async_function_definition
  (function_definition
    name: (identifier) @name
    parameters: (parameters) @params
    return_type: (type)? @return_type)) @async_func
`

	// QueryPythonClasses is a tree-sitter query pattern for class definitions.
	QueryPythonClasses = `
(class_definition
  name: (identifier) @name
  superclasses: (argument_list)? @bases
  body: (block) @body) @class
`

	// QueryPythonImports is a tree-sitter query pattern for import statements.
	QueryPythonImports = `
(import_statement
  name: (dotted_name) @module) @import

(import_from_statement
  module_name: [
    (dotted_name) @module
    (relative_import) @relative
  ]) @from_import
`

	// QueryPythonDecorators is a tree-sitter query pattern for decorators.
	QueryPythonDecorators = `
(decorated_definition
  (decorator
    [
      (identifier) @decorator_name
      (call function: (identifier) @decorator_name)
      (attribute) @decorator_name
    ])*
  definition: [
    (function_definition) @func
    (class_definition) @class
  ])
`

	// QueryPythonVariables is a tree-sitter query pattern for variable assignments.
	QueryPythonVariables = `
(expression_statement
  (assignment
    left: (identifier) @name
    type: (type)? @type
    right: (_) @value)) @var
`
)

// Python Visibility Rules
//
// Python uses naming conventions to indicate visibility:
//
// 1. Public names: No underscore prefix
//    Example: name, MyClass, my_function
//    Exported: true
//
// 2. Protected names: Single underscore prefix
//    Example: _name, _MyClass, _my_function
//    Convention: Internal use, not part of public API
//    Exported: false
//
// 3. Private names: Double underscore prefix (name mangling)
//    Example: __name, __my_function
//    Note: Python mangles these names to _ClassName__name
//    Exported: false
//
// 4. Dunder names: Double underscore prefix AND suffix
//    Example: __init__, __str__, __name__
//    Note: These are special Python methods/attributes
//    Exported: true (they are part of Python's protocol)
//
// 5. __all__ variable:
//    When present, only names in __all__ are truly public when using "from x import *"
//    We extract all symbols but note that __all__ determines import visibility

// Type Hint Formats
//
// Python type hints can appear in several forms:
//
// 1. Simple types:
//    def foo(x: int) -> str:
//
// 2. Generic types:
//    def foo(x: List[int]) -> Dict[str, int]:
//
// 3. Optional types (pre-3.10):
//    def foo(x: Optional[str]) -> None:
//
// 4. Union types (3.10+):
//    def foo(x: str | None) -> int | None:
//
// 5. Complex nested types:
//    def foo(x: Callable[[int, str], bool]) -> None:
//
// The parser preserves type hints as-is in the Signature field
// and extracts the return type to Metadata.ReturnType

// Decorator Patterns
//
// Common decorators and their meanings:
//
// @property           -> Converts method to property (getter)
// @name.setter        -> Property setter
// @name.deleter       -> Property deleter
// @staticmethod       -> Method doesn't receive implicit first argument
// @classmethod        -> Method receives class as first argument
// @abstractmethod     -> Method must be overridden in subclass
// @dataclass          -> Class is a dataclass (auto-generates __init__, etc.)
// @functools.cache    -> Memoization decorator
// @functools.wraps    -> Preserves function metadata in decorators
//
// The parser extracts decorator names (without arguments) to Metadata.Decorators
