package ast

// HTML Tree-sitter Node Types
//
// This file documents the tree-sitter node types used by HTMLParser for symbol extraction.
// The parser uses direct node traversal rather than tree-sitter's query language for
// more precise control over symbol extraction.
//
// Reference: https://github.com/tree-sitter/tree-sitter-html

// Node type constants for HTML AST traversal.
const (
	// Top-level nodes
	htmlNodeDocument = "document"
	htmlNodeDoctype  = "doctype"

	// Element nodes
	htmlNodeElement     = "element"
	htmlNodeStartTag    = "start_tag"
	htmlNodeEndTag      = "end_tag"
	htmlNodeSelfClosing = "self_closing_tag"
	htmlNodeTagName     = "tag_name"
	htmlNodeText        = "text"

	// Special element nodes
	htmlNodeScriptElement = "script_element"
	htmlNodeStyleElement  = "style_element"
	htmlNodeRawText       = "raw_text"

	// Attribute nodes
	htmlNodeAttribute            = "attribute"
	htmlNodeAttributeName        = "attribute_name"
	htmlNodeAttributeValue       = "attribute_value"
	htmlNodeQuotedAttributeValue = "quoted_attribute_value"

	// Comment nodes
	htmlNodeComment = "comment"

	// Error nodes
	htmlNodeERROR = "ERROR"
)

// HTMLNodeTypes maps symbol kinds to the tree-sitter node types that produce them.
var HTMLNodeTypes = map[SymbolKind][]string{
	SymbolKindElement:   {htmlNodeElement},
	SymbolKindForm:      {htmlNodeElement},                        // when tag_name == "form"
	SymbolKindComponent: {htmlNodeElement},                        // when tag_name contains hyphen
	SymbolKindImport:    {htmlNodeScriptElement, htmlNodeElement}, // script src, link href
}

// HTML AST Structure Reference
//
// document
// ├── doctype
// │   ├── <!
// │   ├── DOCTYPE
// │   └── >
// │
// ├── element
// │   ├── start_tag
// │   │   ├── <
// │   │   ├── tag_name
// │   │   ├── attribute*
// │   │   │   ├── attribute_name
// │   │   │   ├── =
// │   │   │   └── quoted_attribute_value
// │   │   │       ├── "
// │   │   │       ├── attribute_value
// │   │   │       └── "
// │   │   └── >
// │   ├── text | element*
// │   └── end_tag
// │       ├── </
// │       ├── tag_name
// │       └── >
// │
// ├── script_element
// │   ├── start_tag
// │   │   ├── <
// │   │   ├── tag_name (= "script")
// │   │   ├── attribute* (src, type, etc.)
// │   │   └── >
// │   ├── raw_text (JavaScript content)
// │   └── end_tag
// │
// └── style_element
//     ├── start_tag
//     │   ├── <
//     │   ├── tag_name (= "style")
//     │   └── >
//     ├── raw_text (CSS content)
//     └── end_tag

// HTML Attribute Names of Interest
//
// Elements with these attributes are extracted as symbols:
// - id: Any element with an id attribute → SymbolKindElement
// - name: Form elements with name attribute → SymbolKindForm (for <form>)
// - src: Script elements with src → Import
// - href: Link elements with href (stylesheets) → Import
// - data-*: Custom data attributes (may indicate components)

// HTML Custom Elements (Web Components)
//
// Elements with hyphenated tag names are treated as custom elements:
// - <nav-menu> → SymbolKindComponent
// - <user-profile> → SymbolKindComponent
// - <my-app> → SymbolKindComponent
//
// This follows the Web Components naming convention.

// HTML Script Types
//
// Script elements can have different types:
// - <script> → Classic script (default)
// - <script type="module"> → ES module
// - <script type="text/javascript"> → Classic script
// - <script type="application/json"> → JSON data (not executable)
//
// The parser extracts the type attribute to distinguish modules.

// HTML Framework Detection Hints
//
// Certain attributes indicate framework usage:
// - v-*, @*, : → Vue.js
// - *ngIf, *ngFor, [binding], (event) → Angular
// - {#if}, {#each}, on:event → Svelte
// - class:name, style:prop → Svelte
//
// This is informational only; the parser doesn't fully parse framework syntax.
