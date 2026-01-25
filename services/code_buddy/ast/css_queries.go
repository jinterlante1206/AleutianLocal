package ast

// CSS Tree-sitter Node Types
//
// This file documents the tree-sitter node types used by CSSParser for symbol extraction.
// The parser uses direct node traversal rather than tree-sitter's query language for
// more precise control over symbol extraction.
//
// Reference: https://github.com/tree-sitter/tree-sitter-css

// Node type constants for CSS AST traversal.
const (
	// Top-level nodes
	cssNodeStylesheet = "stylesheet"

	// Import-related nodes
	cssNodeImportStatement = "import_statement"

	// Rule nodes
	cssNodeRuleSet     = "rule_set"
	cssNodeSelectors   = "selectors"
	cssNodeBlock       = "block"
	cssNodeDeclaration = "declaration"

	// Selector types
	cssNodeClassSelector           = "class_selector"
	cssNodeIDSelector              = "id_selector"
	cssNodeElementSelector         = "tag_name"
	cssNodePseudoClassSelector     = "pseudo_class_selector"
	cssNodePseudoElementSelector   = "pseudo_element_selector"
	cssNodeAttributeSelector       = "attribute_selector"
	cssNodeDescendantSelector      = "descendant_selector"
	cssNodeChildSelector           = "child_selector"
	cssNodeSiblingSelector         = "sibling_selector"
	cssNodeAdjacentSiblingSelector = "adjacent_sibling_selector"
	cssNodeUniversalSelector       = "universal_selector"

	// Selector name nodes
	cssNodeClassName    = "class_name"
	cssNodeIDName       = "id_name"
	cssNodePropertyName = "property_name"

	// At-rules
	cssNodeKeyframesStatement = "keyframes_statement"
	cssNodeKeyframesName      = "keyframes_name"
	cssNodeKeyframeBlockList  = "keyframe_block_list"
	cssNodeKeyframeBlock      = "keyframe_block"
	cssNodeMediaStatement     = "media_statement"
	cssNodeFeatureQuery       = "feature_query"
	cssNodeFeatureName        = "feature_name"
	cssNodeSupportsStatement  = "supports_statement"
	cssNodeNamespaceStatement = "namespace_statement"
	cssNodeCharsetStatement   = "charset_statement"
	cssNodeFontFaceStatement  = "font_face_statement"

	// Values
	cssNodeStringValue    = "string_value"
	cssNodePlainValue     = "plain_value"
	cssNodeColorValue     = "color_value"
	cssNodeIntegerValue   = "integer_value"
	cssNodeFloatValue     = "float_value"
	cssNodeCallExpression = "call_expression"
	cssNodeFunctionName   = "function_name"
	cssNodeArguments      = "arguments"
	cssNodeUnit           = "unit"

	// Comments
	cssNodeComment = "comment"
)

// CSSNodeTypes maps symbol kinds to the tree-sitter node types that produce them.
var CSSNodeTypes = map[SymbolKind][]string{
	SymbolKindCSSClass:    {cssNodeClassSelector},
	SymbolKindCSSID:       {cssNodeIDSelector},
	SymbolKindCSSVariable: {cssNodeDeclaration}, // when property_name starts with --
	SymbolKindAnimation:   {cssNodeKeyframesStatement},
	SymbolKindMediaQuery:  {cssNodeMediaStatement},
	SymbolKindImport:      {cssNodeImportStatement},
}

// CSS AST Structure Reference
//
// stylesheet
// ├── comment
// │
// ├── import_statement
// │   ├── @import
// │   ├── call_expression (url('...'))
// │   │   ├── function_name = url
// │   │   └── arguments
// │   │       └── string_value
// │   └── ;
// │
// ├── rule_set
// │   ├── selectors
// │   │   ├── class_selector
// │   │   │   ├── .
// │   │   │   └── class_name
// │   │   ├── id_selector
// │   │   │   ├── #
// │   │   │   └── id_name
// │   │   └── ... (other selector types)
// │   └── block
// │       ├── {
// │       ├── declaration
// │       │   ├── property_name
// │       │   ├── :
// │       │   ├── <value>
// │       │   └── ;
// │       └── }
// │
// ├── keyframes_statement
// │   ├── @keyframes
// │   ├── keyframes_name
// │   └── keyframe_block_list
// │       ├── {
// │       ├── keyframe_block
// │       │   ├── from | to | percentage
// │       │   └── block
// │       └── }
// │
// └── media_statement
//     ├── @media
//     ├── feature_query
//     │   ├── (
//     │   ├── feature_name
//     │   ├── :
//     │   ├── <value>
//     │   └── )
//     └── block
//         └── rule_set+

// CSS Selector Types
//
// CSS supports many selector types:
//
// 1. Class selectors: .button, .card-header
// 2. ID selectors: #header, #main-content
// 3. Element selectors: div, span, p
// 4. Pseudo-class selectors: :hover, :focus, :nth-child()
// 5. Pseudo-element selectors: ::before, ::after
// 6. Attribute selectors: [type="text"], [data-active]
// 7. Combinators: div > p, .a .b, .a + .b, .a ~ .b
// 8. Universal selector: *
//
// The parser extracts class and ID names from selectors.

// CSS Custom Properties
//
// CSS custom properties (variables) are declared with -- prefix:
//
// :root {
//     --primary-color: #007bff;
//     --spacing-unit: 8px;
// }
//
// They are used with var():
//
// .button {
//     background: var(--primary-color);
//     padding: var(--spacing-unit);
// }
//
// The parser extracts custom properties as SymbolKindCSSVariable.

// CSS At-Rules
//
// Common at-rules:
//
// @import - import external stylesheets
// @media - conditional styles based on media features
// @keyframes - define animations
// @font-face - define custom fonts
// @supports - feature queries
// @namespace - XML namespace
// @charset - character encoding
// @page - print page styles
// @layer - cascade layers (CSS Layers)
//
// The parser extracts @import, @media, and @keyframes.

// CSS Comments
//
// CSS uses /* */ style comments only (no // single-line comments).
// Comments can span multiple lines and appear almost anywhere.
//
// /* This is a CSS comment */
