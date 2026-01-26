package ast

// Markdown Tree-sitter Node Types
//
// This file documents the tree-sitter node types used by MarkdownParser for symbol extraction.
// The parser uses direct node traversal rather than tree-sitter's query language for
// more precise control over symbol extraction.
//
// Reference: https://github.com/tree-sitter-grammars/tree-sitter-markdown

// Node type constants for Markdown AST traversal.
const (
	// Top-level nodes
	markdownNodeDocument = "document"
	markdownNodeSection  = "section"

	// Heading nodes
	markdownNodeAtxHeading = "atx_heading"
	markdownNodeH1Marker   = "atx_h1_marker"
	markdownNodeH2Marker   = "atx_h2_marker"
	markdownNodeH3Marker   = "atx_h3_marker"
	markdownNodeH4Marker   = "atx_h4_marker"
	markdownNodeH5Marker   = "atx_h5_marker"
	markdownNodeH6Marker   = "atx_h6_marker"
	markdownNodeSetextH1   = "setext_h1_underline"
	markdownNodeSetextH2   = "setext_h2_underline"
	markdownNodeInline     = "inline"

	// Code block nodes
	markdownNodeFencedCodeBlock   = "fenced_code_block"
	markdownNodeCodeBlockDelim    = "fenced_code_block_delimiter"
	markdownNodeInfoString        = "info_string"
	markdownNodeLanguage          = "language"
	markdownNodeCodeFenceContent  = "code_fence_content"
	markdownNodeIndentedCodeBlock = "indented_code_block"

	// List nodes
	markdownNodeList              = "list"
	markdownNodeListItem          = "list_item"
	markdownNodeListMarkerMinus   = "list_marker_minus"
	markdownNodeListMarkerPlus    = "list_marker_plus"
	markdownNodeListMarkerStar    = "list_marker_star"
	markdownNodeListMarkerDot     = "list_marker_dot"
	markdownNodeListMarkerParen   = "list_marker_parenthesis"
	markdownNodeTaskListMarker    = "task_list_marker_checked"
	markdownNodeTaskListUnchecked = "task_list_marker_unchecked"

	// Block elements
	markdownNodeParagraph     = "paragraph"
	markdownNodeBlockQuote    = "block_quote"
	markdownNodeThematicBreak = "thematic_break"

	// Link/reference nodes
	markdownNodeLinkRefDef = "link_reference_definition"
	markdownNodeLinkLabel  = "link_label"
	markdownNodeLinkDest   = "link_destination"
	markdownNodeLinkTitle  = "link_title"

	// Table nodes (if supported by the grammar)
	markdownNodeTable       = "pipe_table"
	markdownNodeTableHeader = "pipe_table_header"
	markdownNodeTableRow    = "pipe_table_row"
	markdownNodeTableCell   = "pipe_table_cell"

	// HTML nodes
	markdownNodeHTMLBlock = "html_block"

	// Error nodes
	markdownNodeERROR = "ERROR"
)

// MarkdownNodeTypes maps symbol kinds to the tree-sitter node types that produce them.
var MarkdownNodeTypes = map[SymbolKind][]string{
	SymbolKindHeading:   {markdownNodeAtxHeading},
	SymbolKindCodeBlock: {markdownNodeFencedCodeBlock, markdownNodeIndentedCodeBlock},
	SymbolKindList:      {markdownNodeList},
	SymbolKindLink:      {markdownNodeLinkRefDef},
}

// Markdown AST Structure Reference
//
// document
// └── section
//     ├── atx_heading
//     │   ├── atx_h1_marker (# / ## / ### / etc.)
//     │   └── inline (heading text)
//     │
//     ├── paragraph
//     │   └── inline (text content)
//     │
//     ├── fenced_code_block
//     │   ├── fenced_code_block_delimiter (```)
//     │   ├── info_string
//     │   │   └── language (e.g., "go", "python")
//     │   ├── code_fence_content (the code)
//     │   └── fenced_code_block_delimiter (```)
//     │
//     ├── list
//     │   ├── list_item
//     │   │   ├── list_marker_minus/plus/star/dot (- / + / * / 1.)
//     │   │   └── paragraph
//     │   │       └── inline (item text)
//     │   └── ...more list_items
//     │
//     ├── block_quote
//     │   ├── block_quote_marker (>)
//     │   └── paragraph
//     │       └── inline (quoted text)
//     │
//     ├── link_reference_definition
//     │   ├── link_label ([ref-name])
//     │   ├── link_destination (URL)
//     │   └── link_title (optional "title")
//     │
//     └── thematic_break (---)

// Heading Levels
//
// ATX-style headings use markers:
// - atx_h1_marker: #
// - atx_h2_marker: ##
// - atx_h3_marker: ###
// - atx_h4_marker: ####
// - atx_h5_marker: #####
// - atx_h6_marker: ######
//
// Setext-style headings use underlines:
// - setext_h1_underline: ===
// - setext_h2_underline: ---

// Code Block Languages
//
// Fenced code blocks can have an info string specifying the language:
// ```go
// code here
// ```
//
// The language is extracted from the info_string > language node.

// List Types
//
// Unordered lists use markers:
// - list_marker_minus: -
// - list_marker_plus: +
// - list_marker_star: *
//
// Ordered lists use numbered markers:
// - list_marker_dot: 1.
// - list_marker_parenthesis: 1)
//
// Task lists have additional markers:
// - task_list_marker_checked: [x]
// - task_list_marker_unchecked: [ ]
