package ast

// Go Tree-sitter Node Types
//
// This file documents the tree-sitter node types used by GoParser for symbol extraction.
// The parser uses direct node traversal rather than tree-sitter's query language for
// more precise control over symbol extraction.
//
// Reference: https://github.com/tree-sitter/tree-sitter-go/blob/master/src/grammar.json

// Node type constants for Go AST traversal.
//
// These constants define the tree-sitter node types that GoParser uses to identify
// different Go language constructs. They match the node types defined in tree-sitter-go.
const (
	// Top-level declarations
	nodePackageClause       = "package_clause"
	nodeImportDeclaration   = "import_declaration"
	nodeFunctionDeclaration = "function_declaration"
	nodeMethodDeclaration   = "method_declaration"
	nodeTypeDeclaration     = "type_declaration"
	nodeVarDeclaration      = "var_declaration"
	nodeConstDeclaration    = "const_declaration"

	// Import-related nodes
	nodeImportSpec     = "import_spec"
	nodeImportSpecList = "import_spec_list"

	// Type-related nodes
	nodeTypeSpec         = "type_spec"
	nodeStructType       = "struct_type"
	nodeInterfaceType    = "interface_type"
	nodeFieldDeclaration = "field_declaration"
	nodeFieldDeclList    = "field_declaration_list"
	nodeMethodSpec       = "method_spec"
	nodeMethodSpecList   = "method_spec_list"

	// Variable-related nodes
	nodeVarSpec       = "var_spec"
	nodeVarSpecList   = "var_spec_list"
	nodeConstSpec     = "const_spec"
	nodeConstSpecList = "const_spec_list"

	// Identifier nodes
	nodeIdentifier        = "identifier"
	nodeFieldIdentifier   = "field_identifier"
	nodePackageIdentifier = "package_identifier"
	nodeTypeIdentifier    = "type_identifier"
	nodeBlankIdentifier   = "blank_identifier"

	// Type nodes
	nodePointerType   = "pointer_type"
	nodeSliceType     = "slice_type"
	nodeMapType       = "map_type"
	nodeChannelType   = "channel_type"
	nodeQualifiedType = "qualified_type"
	nodeFunctionType  = "function_type"

	// Other nodes
	nodeParameterList            = "parameter_list"
	nodeComment                  = "comment"
	nodeInterpretedStringLiteral = "interpreted_string_literal"
	nodeDot                      = "dot"
)

// GoNodeTypes maps symbol kinds to the tree-sitter node types that produce them.
//
// This provides a reference for understanding how Go constructs map to symbols:
//
//	KindPackage   <- package_clause/package_identifier
//	KindImport    <- import_declaration/import_spec
//	KindFunction  <- function_declaration
//	KindMethod    <- method_declaration, method_spec (in interfaces)
//	KindStruct    <- type_declaration/type_spec/struct_type
//	KindInterface <- type_declaration/type_spec/interface_type
//	KindType      <- type_declaration/type_spec (aliases)
//	KindField     <- field_declaration (in structs)
//	KindVariable  <- var_declaration/var_spec
//	KindConstant  <- const_declaration/const_spec
var GoNodeTypes = map[SymbolKind][]string{
	SymbolKindPackage:   {nodePackageClause},
	SymbolKindImport:    {nodeImportDeclaration, nodeImportSpec},
	SymbolKindFunction:  {nodeFunctionDeclaration},
	SymbolKindMethod:    {nodeMethodDeclaration, nodeMethodSpec},
	SymbolKindStruct:    {nodeTypeDeclaration, nodeStructType},
	SymbolKindInterface: {nodeTypeDeclaration, nodeInterfaceType},
	SymbolKindType:      {nodeTypeDeclaration, nodeTypeSpec},
	SymbolKindField:     {nodeFieldDeclaration},
	SymbolKindVariable:  {nodeVarDeclaration, nodeVarSpec},
	SymbolKindConstant:  {nodeConstDeclaration, nodeConstSpec},
}

// Example tree-sitter query patterns for reference.
//
// These patterns could be used with tree-sitter's query language if we decide
// to switch from direct traversal. They're included here for documentation.
//
// Note: The current implementation uses direct node traversal because:
// 1. It provides more precise control over what we extract
// 2. It's easier to debug and modify
// 3. It handles edge cases more gracefully
//
// Function query:
//
//	(function_declaration
//	  name: (identifier) @name
//	  parameters: (parameter_list) @params
//	  result: (_)? @result) @func
//
// Method query:
//
//	(method_declaration
//	  receiver: (parameter_list) @receiver
//	  name: (field_identifier) @name
//	  parameters: (parameter_list) @params
//	  result: (_)? @result) @method
//
// Type query:
//
//	(type_declaration
//	  (type_spec
//	    name: (type_identifier) @name
//	    type: (_) @type)) @typedef
//
// Import query:
//
//	(import_declaration
//	  (import_spec_list
//	    (import_spec
//	      name: (package_identifier)? @alias
//	      path: (interpreted_string_literal) @path)))
const (
	// QueryFunctions is a tree-sitter query pattern for function declarations.
	QueryFunctions = `
(function_declaration
  name: (identifier) @name
  parameters: (parameter_list) @params
  result: (_)? @result) @func
`

	// QueryMethods is a tree-sitter query pattern for method declarations.
	QueryMethods = `
(method_declaration
  receiver: (parameter_list) @receiver
  name: (field_identifier) @name
  parameters: (parameter_list) @params
  result: (_)? @result) @method
`

	// QueryTypes is a tree-sitter query pattern for type declarations.
	QueryTypes = `
(type_declaration
  (type_spec
    name: (type_identifier) @name
    type: (_) @type)) @typedef
`

	// QueryImports is a tree-sitter query pattern for import declarations.
	QueryImports = `
(import_declaration
  (import_spec_list
    (import_spec
      name: (package_identifier)? @alias
      path: (interpreted_string_literal) @path)))
`
)
