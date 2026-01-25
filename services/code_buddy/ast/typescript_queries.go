package ast

// TypeScript Tree-sitter Node Types
//
// This file documents the tree-sitter node types used by TypeScriptParser for symbol extraction.
// The parser uses direct node traversal rather than tree-sitter's query language for
// more precise control over symbol extraction.
//
// Reference: https://github.com/tree-sitter/tree-sitter-typescript

// Node type constants for TypeScript AST traversal.
//
// These constants define the tree-sitter node types that TypeScriptParser uses to identify
// different TypeScript language constructs.
const (
	// Top-level nodes
	tsNodeProgram = "program"

	// Import-related nodes
	tsNodeImportStatement = "import_statement"
	tsNodeImportClause    = "import_clause"
	tsNodeNamespaceImport = "namespace_import"
	tsNodeNamedImports    = "named_imports"
	tsNodeImportSpecifier = "import_specifier"
	tsNodeString          = "string"
	tsNodeStringFragment  = "string_fragment"

	// Export-related nodes
	tsNodeExportStatement = "export_statement"
	tsNodeExportClause    = "export_clause"
	tsNodeExportSpecifier = "export_specifier"

	// Declaration nodes
	tsNodeFunctionDeclaration      = "function_declaration"
	tsNodeClassDeclaration         = "class_declaration"
	tsNodeAbstractClassDeclaration = "abstract_class_declaration"
	tsNodeInterfaceDeclaration     = "interface_declaration"
	tsNodeTypeAliasDeclaration     = "type_alias_declaration"
	tsNodeEnumDeclaration          = "enum_declaration"
	tsNodeLexicalDeclaration       = "lexical_declaration"
	tsNodeVariableDeclaration      = "variable_declaration"
	tsNodeVariableDeclarator       = "variable_declarator"

	// Class-related nodes
	tsNodeClassBody             = "class_body"
	tsNodeClassHeritage         = "class_heritage"
	tsNodeExtendsClause         = "extends_clause"
	tsNodeImplementsClause      = "implements_clause"
	tsNodeMethodDefinition      = "method_definition"
	tsNodePublicFieldDefinition = "public_field_definition"
	tsNodeAccessibilityModifier = "accessibility_modifier"

	// Interface-related nodes
	tsNodeInterfaceBody     = "interface_body"
	tsNodeObjectType        = "object_type"
	tsNodePropertySignature = "property_signature"
	tsNodeMethodSignature   = "method_signature"

	// Enum-related nodes
	tsNodeEnumBody       = "enum_body"
	tsNodeEnumAssignment = "enum_assignment"

	// Type-related nodes
	tsNodeTypeParameters   = "type_parameters"
	tsNodeTypeParameter    = "type_parameter"
	tsNodeTypeAnnotation   = "type_annotation"
	tsNodeTypeIdentifier   = "type_identifier"
	tsNodeGenericType      = "generic_type"
	tsNodeUnionType        = "union_type"
	tsNodeIntersectionType = "intersection_type"
	tsNodePredefinedType   = "predefined_type"

	// Function-related nodes
	tsNodeFormalParameters  = "formal_parameters"
	tsNodeRequiredParameter = "required_parameter"
	tsNodeOptionalParameter = "optional_parameter"
	tsNodeArrowFunction     = "arrow_function"

	// Decorator nodes
	tsNodeDecorator = "decorator"

	// Identifier nodes
	tsNodeIdentifier         = "identifier"
	tsNodePropertyIdentifier = "property_identifier"

	// Expression nodes
	tsNodeCallExpression = "call_expression"
	tsNodeNewExpression  = "new_expression"
	tsNodeArguments      = "arguments"

	// Statement nodes
	tsNodeStatementBlock  = "statement_block"
	tsNodeReturnStatement = "return_statement"

	// Comment nodes
	tsNodeComment = "comment"

	// Keywords
	tsNodeAsync    = "async"
	tsNodeStatic   = "static"
	tsNodeReadonly = "readonly"
	tsNodeAbstract = "abstract"
	tsNodeConst    = "const"
	tsNodeLet      = "let"
	tsNodeType     = "type"
	tsNodeDefault  = "default"
)

// TypeScriptNodeTypes maps symbol kinds to the tree-sitter node types that produce them.
//
// This provides a reference for understanding how TypeScript constructs map to symbols:
//
//	KindImport    <- import_statement
//	KindFunction  <- function_declaration, arrow_function (named)
//	KindClass     <- class_declaration, abstract_class_declaration
//	KindInterface <- interface_declaration
//	KindType      <- type_alias_declaration
//	KindEnum      <- enum_declaration
//	KindEnumMember<- enum_assignment, property_identifier (in enum)
//	KindMethod    <- method_definition, method_signature
//	KindField     <- public_field_definition, property_signature
//	KindVariable  <- lexical_declaration (let), variable_declaration
//	KindConstant  <- lexical_declaration (const)
var TypeScriptNodeTypes = map[SymbolKind][]string{
	SymbolKindImport:     {tsNodeImportStatement},
	SymbolKindFunction:   {tsNodeFunctionDeclaration, tsNodeArrowFunction},
	SymbolKindClass:      {tsNodeClassDeclaration, tsNodeAbstractClassDeclaration},
	SymbolKindInterface:  {tsNodeInterfaceDeclaration},
	SymbolKindType:       {tsNodeTypeAliasDeclaration},
	SymbolKindEnum:       {tsNodeEnumDeclaration},
	SymbolKindEnumMember: {tsNodeEnumAssignment},
	SymbolKindMethod:     {tsNodeMethodDefinition, tsNodeMethodSignature},
	SymbolKindField:      {tsNodePublicFieldDefinition, tsNodePropertySignature},
	SymbolKindVariable:   {tsNodeLexicalDeclaration, tsNodeVariableDeclaration},
	SymbolKindConstant:   {tsNodeLexicalDeclaration}, // when using const
}

// TypeScript AST Structure Reference
//
// program
// ├── import_statement
// │   ├── import
// │   ├── type?                        // for type-only imports
// │   ├── import_clause
// │   │   ├── identifier               // default import
// │   │   ├── namespace_import         // * as foo
// │   │   │   ├── *
// │   │   │   ├── as
// │   │   │   └── identifier
// │   │   └── named_imports            // { foo, bar }
// │   │       └── import_specifier+
// │   │           ├── identifier       // imported name
// │   │           └── identifier?      // local alias
// │   ├── from
// │   └── string                       // module path
// │       └── string_fragment
// │
// ├── export_statement
// │   ├── decorator*                   // decorators before export
// │   ├── export
// │   ├── default?
// │   └── <declaration>                // function, class, interface, etc.
// │
// ├── function_declaration
// │   ├── async?
// │   ├── function
// │   ├── identifier                   // name
// │   ├── type_parameters?             // <T, U>
// │   │   └── type_parameter+
// │   │       ├── type_identifier
// │   │       └── constraint?          // extends Foo
// │   ├── formal_parameters            // (a, b)
// │   │   └── required_parameter | optional_parameter
// │   │       ├── identifier
// │   │       ├── ?                    // for optional
// │   │       └── type_annotation?
// │   ├── type_annotation?             // return type
// │   └── statement_block
// │
// ├── class_declaration
// │   ├── class
// │   ├── type_identifier              // name
// │   ├── type_parameters?
// │   ├── class_heritage?
// │   │   ├── extends_clause
// │   │   │   └── type_identifier | generic_type
// │   │   └── implements_clause
// │   │       └── type_identifier+
// │   └── class_body
// │       ├── method_definition
// │       │   ├── accessibility_modifier?  // public, private, protected
// │       │   ├── static?
// │       │   ├── async?
// │       │   ├── readonly?
// │       │   ├── property_identifier   // name
// │       │   ├── type_parameters?
// │       │   ├── formal_parameters
// │       │   ├── type_annotation?
// │       │   └── statement_block
// │       └── public_field_definition
// │           ├── accessibility_modifier?
// │           ├── static?
// │           ├── readonly?
// │           ├── property_identifier   // name
// │           ├── type_annotation?
// │           └── initializer?
// │
// ├── interface_declaration
// │   ├── interface
// │   ├── type_identifier              // name
// │   ├── type_parameters?
// │   └── interface_body | object_type
// │       ├── property_signature
// │       │   ├── readonly?
// │       │   ├── property_identifier
// │       │   ├── ??                   // optional marker
// │       │   └── type_annotation
// │       └── method_signature
// │           ├── property_identifier
// │           ├── type_parameters?
// │           ├── formal_parameters
// │           └── type_annotation?
// │
// ├── type_alias_declaration
// │   ├── type
// │   ├── type_identifier              // name
// │   ├── type_parameters?
// │   ├── =
// │   └── <type>                       // the type definition
// │
// ├── enum_declaration
// │   ├── enum
// │   ├── identifier                   // name
// │   └── enum_body
// │       └── enum_assignment*
// │           ├── property_identifier  // member name
// │           ├── =
// │           └── <value>
// │
// └── lexical_declaration
//     ├── const | let
//     └── variable_declarator
//         ├── identifier               // name
//         ├── type_annotation?
//         └── initializer?
//             └── arrow_function | ...

// Example tree-sitter query patterns for reference.
//
// These patterns could be used with tree-sitter's query language if we decide
// to switch from direct traversal.
const (
	// QueryTSFunctions is a tree-sitter query pattern for function declarations.
	QueryTSFunctions = `
(function_declaration
  name: (identifier) @name
  type_parameters: (type_parameters)? @type_params
  parameters: (formal_parameters) @params
  return_type: (type_annotation)? @return_type) @func
`

	// QueryTSClasses is a tree-sitter query pattern for class declarations.
	QueryTSClasses = `
(class_declaration
  name: (type_identifier) @name
  type_parameters: (type_parameters)? @type_params
  (class_heritage)? @heritage
  body: (class_body) @body) @class
`

	// QueryTSInterfaces is a tree-sitter query pattern for interface declarations.
	QueryTSInterfaces = `
(interface_declaration
  name: (type_identifier) @name
  type_parameters: (type_parameters)? @type_params
  body: [
    (interface_body) @body
    (object_type) @body
  ]) @interface
`

	// QueryTSTypes is a tree-sitter query pattern for type alias declarations.
	QueryTSTypes = `
(type_alias_declaration
  name: (type_identifier) @name
  type_parameters: (type_parameters)? @type_params
  value: (_) @value) @type_alias
`

	// QueryTSEnums is a tree-sitter query pattern for enum declarations.
	QueryTSEnums = `
(enum_declaration
  name: (identifier) @name
  body: (enum_body) @body) @enum
`

	// QueryTSImports is a tree-sitter query pattern for import statements.
	QueryTSImports = `
(import_statement
  (import_clause
    [
      (identifier) @default
      (namespace_import (identifier) @namespace)
      (named_imports (import_specifier (identifier) @named)+)
    ])
  source: (string) @source) @import
`

	// QueryTSExports is a tree-sitter query pattern for export statements.
	QueryTSExports = `
(export_statement
  (decorator)* @decorators
  declaration: (_) @declaration) @export
`
)

// TypeScript Import Types
//
// TypeScript/ES6 supports several import forms:
//
// 1. Named imports:
//    import { foo, bar } from 'module'
//    import { foo as f } from 'module'
//
// 2. Default imports:
//    import foo from 'module'
//
// 3. Namespace imports:
//    import * as foo from 'module'
//
// 4. Type-only imports (TypeScript specific):
//    import type { Foo } from 'module'
//    import { type Foo, bar } from 'module'
//
// 5. Side-effect imports:
//    import 'module'
//
// 6. CommonJS (legacy):
//    const foo = require('module')
//
// The parser captures:
// - IsDefault: true for default imports
// - IsNamespace: true for namespace imports
// - IsTypeOnly: true for type-only imports
// - IsCommonJS: true for require() calls
// - IsModule: true for ES module imports

// TypeScript Export Types
//
// TypeScript supports several export forms:
//
// 1. Named exports:
//    export { foo, bar }
//    export { foo as f }
//
// 2. Default exports:
//    export default foo
//    export default class Foo {}
//
// 3. Declaration exports:
//    export function foo() {}
//    export class Foo {}
//    export interface IFoo {}
//    export type Foo = string
//    export enum Foo {}
//    export const foo = 1
//
// 4. Re-exports:
//    export { foo } from 'module'
//    export * from 'module'
//
// 5. Type-only exports:
//    export type { Foo }
//
// The parser marks exported symbols with Exported: true

// TypeScript Access Modifiers
//
// Class members can have access modifiers:
//
// - public: accessible from anywhere (default)
// - private: only accessible within the class
// - protected: accessible within class and subclasses
// - readonly: cannot be modified after initialization
// - static: belongs to class, not instance
// - abstract: must be implemented by subclass
//
// The parser captures these in Metadata.AccessModifier and related flags.

// TypeScript Decorator Patterns
//
// Decorators can be applied to:
// - Classes: @Component class Foo {}
// - Methods: @Log method() {}
// - Properties: @Input() property: string
// - Parameters: method(@Inject() param) {}
//
// Common Angular decorators:
// - @Component, @Directive, @Injectable, @Pipe
// - @Input, @Output, @ViewChild
// - @NgModule
//
// Common NestJS decorators:
// - @Controller, @Injectable, @Module
// - @Get, @Post, @Put, @Delete
// - @Body, @Param, @Query
//
// The parser extracts decorator names (without arguments) to Metadata.Decorators.

// TypeScript Generics
//
// Generic type parameters can appear on:
// - Functions: function foo<T>(x: T): T {}
// - Classes: class Foo<T> {}
// - Interfaces: interface IFoo<T> {}
// - Type aliases: type Foo<T> = T | null
// - Methods: method<T>(x: T): T {}
//
// Constraints can be specified:
// - <T extends Base>
// - <T extends Base, U extends T>
// - <T = DefaultType>
//
// The parser captures type parameters in Metadata.TypeParameters.
