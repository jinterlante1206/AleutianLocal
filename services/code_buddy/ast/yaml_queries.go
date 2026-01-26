package ast

// YAML Tree-sitter Node Types
//
// This file documents the tree-sitter node types used by YAMLParser for symbol extraction.
// The parser uses direct node traversal rather than tree-sitter's query language for
// more precise control over symbol extraction.
//
// Reference: https://github.com/ikatyang/tree-sitter-yaml

// Node type constants for YAML AST traversal.
const (
	// Top-level nodes
	yamlNodeStream   = "stream"
	yamlNodeDocument = "document"
	yamlNodeComment  = "comment"

	// Mapping nodes
	yamlNodeBlockMapping     = "block_mapping"
	yamlNodeBlockMappingPair = "block_mapping_pair"
	yamlNodeFlowMapping      = "flow_mapping"
	yamlNodeFlowPair         = "flow_pair"

	// Sequence nodes
	yamlNodeBlockSequence     = "block_sequence"
	yamlNodeBlockSequenceItem = "block_sequence_item"
	yamlNodeFlowSequence      = "flow_sequence"

	// Node wrapper types
	yamlNodeBlockNode = "block_node"
	yamlNodeFlowNode  = "flow_node"

	// Scalar nodes
	yamlNodePlainScalar       = "plain_scalar"
	yamlNodeStringScalar      = "string_scalar"
	yamlNodeIntegerScalar     = "integer_scalar"
	yamlNodeFloatScalar       = "float_scalar"
	yamlNodeBooleanScalar     = "boolean_scalar"
	yamlNodeNullScalar        = "null_scalar"
	yamlNodeDoubleQuoteScalar = "double_quote_scalar"
	yamlNodeSingleQuoteScalar = "single_quote_scalar"

	// Anchor/Alias nodes
	yamlNodeAnchor     = "anchor"
	yamlNodeAnchorName = "anchor_name"
	yamlNodeAlias      = "alias"
	yamlNodeAliasName  = "alias_name"

	// Tag nodes
	yamlNodeTag     = "tag"
	yamlNodeTagName = "tag_name"

	// Error nodes
	yamlNodeERROR = "ERROR"
)

// YAMLNodeTypes maps symbol kinds to the tree-sitter node types that produce them.
var YAMLNodeTypes = map[SymbolKind][]string{
	SymbolKindKey:      {yamlNodeBlockMappingPair, yamlNodeFlowPair},
	SymbolKindAnchor:   {yamlNodeAnchor},
	SymbolKindDocument: {yamlNodeDocument},
}

// YAML AST Structure Reference
//
// stream
// ├── comment (# Comment)
// │
// ├── document
// │   └── block_node
// │       └── block_mapping
// │           ├── block_mapping_pair
// │           │   ├── flow_node
// │           │   │   └── plain_scalar
// │           │   │       └── string_scalar (key)
// │           │   ├── :
// │           │   └── flow_node
// │           │       └── plain_scalar
// │           │           └── string_scalar (value)
// │           │
// │           └── block_mapping_pair (nested)
// │               ├── flow_node
// │               │   └── plain_scalar
// │               │       └── string_scalar (key)
// │               ├── :
// │               └── block_node
// │                   └── block_mapping
// │                       └── ... (nested pairs)
// │
// ├── --- (document separator)
// │
// └── document (second document)
//     └── ...

// YAML Key Extraction
//
// Keys are extracted from block_mapping_pair and flow_pair nodes.
// The parser extracts:
// - Top-level keys (depth 0)
// - Nested keys up to a configurable depth
// - Key path for nested keys (e.g., "database.host")

// YAML Anchors and Aliases
//
// Anchors define reusable content:
//   defaults: &defaults
//     timeout: 30
//
// Aliases reference anchored content:
//   production:
//     <<: *defaults
//
// The parser extracts both for reference tracking.

// YAML Multi-Document
//
// Multiple YAML documents in a single file are separated by ---
// Each document is tracked as a separate symbol for organization.

// YAML Value Types
//
// Values are typed based on scalar node type:
// - string_scalar: String values
// - integer_scalar: Integer values
// - float_scalar: Float values
// - boolean_scalar: true/false
// - null_scalar: null/~
// - double_quote_scalar/single_quote_scalar: Quoted strings
