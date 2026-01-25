package ast

// SQL Tree-sitter Node Types
//
// This file documents the tree-sitter node types used by SQLParser for symbol extraction.
// The parser uses direct node traversal rather than tree-sitter's query language for
// more precise control over symbol extraction.
//
// Reference: https://github.com/m-novikov/tree-sitter-sql

// Node type constants for SQL AST traversal.
const (
	// Top-level nodes
	sqlNodeProgram   = "program"
	sqlNodeStatement = "statement"
	sqlNodeComment   = "comment"

	// DDL Statement nodes
	sqlNodeCreateTable = "create_table"
	sqlNodeCreateIndex = "create_index"
	sqlNodeCreateView  = "create_view"
	sqlNodeAlterTable  = "alter_table"
	sqlNodeDropTable   = "drop_table"
	sqlNodeDropIndex   = "drop_index"
	sqlNodeDropView    = "drop_view"

	// Table structure nodes
	sqlNodeColumnDefinitions = "column_definitions"
	sqlNodeColumnDefinition  = "column_definition"
	sqlNodeConstraints       = "constraints"
	sqlNodeConstraint        = "constraint"
	sqlNodeAddColumn         = "add_column"

	// Index nodes
	sqlNodeIndexFields = "index_fields"
	sqlNodeField       = "field"

	// View nodes
	sqlNodeCreateQuery = "create_query"

	// Common nodes
	sqlNodeObjectReference = "object_reference"
	sqlNodeIdentifier      = "identifier"
	sqlNodeKeywordUnique   = "keyword_unique"
	sqlNodeKeywordPrimary  = "keyword_primary"
	sqlNodeKeywordKey      = "keyword_key"
	sqlNodeKeywordNot      = "keyword_not"
	sqlNodeKeywordNull     = "keyword_null"
	sqlNodeKeywordForeign  = "keyword_foreign"
	sqlNodeKeywordCreate   = "keyword_create"
	sqlNodeKeywordTable    = "keyword_table"
	sqlNodeKeywordView     = "keyword_view"
	sqlNodeKeywordIndex    = "keyword_index"

	// Data type nodes
	sqlNodeInt       = "int"
	sqlNodeVarchar   = "varchar"
	sqlNodeDecimal   = "decimal"
	sqlNodeTimestamp = "timestamp"
	sqlNodeDate      = "date"
	sqlNodeBool      = "bool"
	sqlNodeText      = "text"
	sqlNodeBlob      = "blob"
	sqlNodeLiteral   = "literal"

	// Error nodes
	sqlNodeERROR = "ERROR"
)

// SQLNodeTypes maps symbol kinds to the tree-sitter node types that produce them.
var SQLNodeTypes = map[SymbolKind][]string{
	SymbolKindTable:  {sqlNodeCreateTable},
	SymbolKindColumn: {sqlNodeColumnDefinition},
	SymbolKindView:   {sqlNodeCreateView},
	SymbolKindIndex:  {sqlNodeCreateIndex},
}

// SQL AST Structure Reference
//
// program
// ├── comment (-- Comment text)
// │
// ├── statement
// │   └── create_table
// │       ├── keyword_create
// │       ├── keyword_table
// │       ├── object_reference
// │       │   └── identifier (table_name)
// │       └── column_definitions
// │           ├── (
// │           ├── column_definition
// │           │   ├── identifier (column_name)
// │           │   ├── int/varchar/decimal/... (data_type)
// │           │   ├── keyword_primary? keyword_key?
// │           │   ├── keyword_not? keyword_null?
// │           │   └── keyword_unique?
// │           ├── ,
// │           ├── column_definition
// │           │   └── ...
// │           └── )
// │
// ├── statement
// │   └── create_index
// │       ├── keyword_create
// │       ├── keyword_unique? (for UNIQUE INDEX)
// │       ├── keyword_index
// │       ├── identifier (index_name)
// │       ├── keyword_on
// │       ├── object_reference
// │       │   └── identifier (table_name)
// │       └── index_fields
// │           ├── (
// │           ├── field
// │           │   └── identifier (column_name)
// │           ├── ,
// │           ├── field
// │           │   └── identifier (column_name)
// │           └── )
// │
// ├── statement
// │   └── create_view
// │       ├── keyword_create
// │       ├── keyword_view
// │       ├── object_reference
// │       │   └── identifier (view_name)
// │       ├── keyword_as
// │       └── create_query
// │           └── select (view definition)
// │
// └── statement
//     └── alter_table
//         ├── keyword_alter
//         ├── keyword_table
//         ├── object_reference
//         │   └── identifier (table_name)
//         └── add_column
//             ├── keyword_add
//             ├── keyword_column
//             └── column_definition
//                 └── ...

// SQL Constraint Types
//
// The parser extracts constraint information from column definitions:
// - PRIMARY KEY: keyword_primary + keyword_key
// - UNIQUE: keyword_unique
// - NOT NULL: keyword_not + keyword_null
// - FOREIGN KEY: keyword_foreign + keyword_key (in constraints node)
//
// Standalone constraints are also detected in the constraints node.

// SQL Data Types
//
// Common data types in column definitions:
// - int: INTEGER, INT, BIGINT, SMALLINT
// - varchar: VARCHAR(n), CHAR(n)
// - decimal: DECIMAL(p,s), NUMERIC(p,s)
// - timestamp: TIMESTAMP, DATETIME
// - date: DATE
// - bool: BOOLEAN, BOOL
// - text: TEXT
// - blob: BLOB, BYTEA
//
// The parser extracts the data type for signature generation.

// SQL Identifier Quoting
//
// SQL identifiers may be quoted:
// - Double quotes: "table_name"
// - Backticks: `table_name`
// - Square brackets: [table_name]
//
// The parser handles these by extracting the identifier text directly.
