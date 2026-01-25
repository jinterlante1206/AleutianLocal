package ast

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	sitter "github.com/smacker/go-tree-sitter"
	tree_sitter_markdown "github.com/smacker/go-tree-sitter/markdown/tree-sitter-markdown"
)

// MarkdownParser extracts symbols from Markdown source files.
//
// Description:
//
//	MarkdownParser uses tree-sitter to parse Markdown files and extract
//	structured symbol information including headings, code blocks, links,
//	and lists.
//
// Thread Safety:
//
//	MarkdownParser is safe for concurrent use. Multiple goroutines can call Parse
//	simultaneously. Each Parse call creates its own tree-sitter parser instance.
//
// Example:
//
//	parser := NewMarkdownParser()
//	result, err := parser.Parse(ctx, content, "README.md")
//	if err != nil {
//	    return fmt.Errorf("parse: %w", err)
//	}
//	for _, sym := range result.Symbols {
//	    fmt.Printf("%s: %s\n", sym.Kind, sym.Name)
//	}
type MarkdownParser struct {
	options MarkdownParserOptions
}

// MarkdownParserOptions configures MarkdownParser behavior.
type MarkdownParserOptions struct {
	// MaxFileSize is the maximum file size in bytes to parse.
	// Files larger than this return ErrFileTooLarge.
	// Default: 10MB
	MaxFileSize int

	// MaxHeadingDepth is the maximum heading level to extract (1-6).
	// Default: 6 (extract all headings)
	MaxHeadingDepth int

	// ExtractCodeBlocks determines whether to extract fenced code blocks.
	// Default: true
	ExtractCodeBlocks bool

	// ExtractLinks determines whether to extract link reference definitions.
	// Default: true
	ExtractLinks bool

	// ExtractLists determines whether to extract lists.
	// Default: true
	ExtractLists bool
}

// DefaultMarkdownParserOptions returns the default options.
func DefaultMarkdownParserOptions() MarkdownParserOptions {
	return MarkdownParserOptions{
		MaxFileSize:       10 * 1024 * 1024, // 10MB
		MaxHeadingDepth:   6,
		ExtractCodeBlocks: true,
		ExtractLinks:      true,
		ExtractLists:      true,
	}
}

// MarkdownParserOption is a functional option for configuring MarkdownParser.
type MarkdownParserOption func(*MarkdownParserOptions)

// WithMarkdownMaxFileSize sets the maximum file size for parsing.
func WithMarkdownMaxFileSize(size int) MarkdownParserOption {
	return func(o *MarkdownParserOptions) {
		o.MaxFileSize = size
	}
}

// WithMarkdownMaxHeadingDepth sets the maximum heading depth to extract.
func WithMarkdownMaxHeadingDepth(depth int) MarkdownParserOption {
	return func(o *MarkdownParserOptions) {
		o.MaxHeadingDepth = depth
	}
}

// WithMarkdownExtractCodeBlocks sets whether to extract code blocks.
func WithMarkdownExtractCodeBlocks(extract bool) MarkdownParserOption {
	return func(o *MarkdownParserOptions) {
		o.ExtractCodeBlocks = extract
	}
}

// WithMarkdownExtractLinks sets whether to extract link definitions.
func WithMarkdownExtractLinks(extract bool) MarkdownParserOption {
	return func(o *MarkdownParserOptions) {
		o.ExtractLinks = extract
	}
}

// WithMarkdownExtractLists sets whether to extract lists.
func WithMarkdownExtractLists(extract bool) MarkdownParserOption {
	return func(o *MarkdownParserOptions) {
		o.ExtractLists = extract
	}
}

// NewMarkdownParser creates a new MarkdownParser with the given options.
//
// Description:
//
//	Creates a parser configured for Markdown files. The parser can be
//	reused for multiple files and is safe for concurrent use.
//
// Example:
//
//	// Default options
//	parser := NewMarkdownParser()
//
//	// With custom options
//	parser := NewMarkdownParser(
//	    WithMarkdownMaxFileSize(5 * 1024 * 1024),
//	    WithMarkdownMaxHeadingDepth(3),
//	)
func NewMarkdownParser(opts ...MarkdownParserOption) *MarkdownParser {
	options := DefaultMarkdownParserOptions()
	for _, opt := range opts {
		opt(&options)
	}
	return &MarkdownParser{
		options: options,
	}
}

// Language returns the language name for this parser.
func (p *MarkdownParser) Language() string {
	return "markdown"
}

// Extensions returns the file extensions this parser handles.
func (p *MarkdownParser) Extensions() []string {
	return []string{".md", ".markdown", ".mdown", ".mkd"}
}

// Parse extracts symbols from Markdown source code.
//
// Description:
//
//	Parses the provided Markdown content using tree-sitter and extracts all
//	symbols including headings, code blocks, links, and lists.
//
// Inputs:
//
//	ctx      - Context for cancellation. Checked before/after parsing.
//	content  - Raw Markdown source bytes. Must be valid UTF-8.
//	filePath - Path to the file (relative to project root, for ID generation).
//
// Outputs:
//
//	*ParseResult - Extracted symbols and metadata. Never nil on success.
//	error        - Non-nil only for complete failures (invalid UTF-8, too large).
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (p *MarkdownParser) Parse(ctx context.Context, content []byte, filePath string) (*ParseResult, error) {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("markdown parse canceled before start: %w", err)
	}

	// Validate file size
	if len(content) > p.options.MaxFileSize {
		return nil, ErrFileTooLarge
	}

	// Validate UTF-8
	if !utf8.Valid(content) {
		return nil, ErrInvalidContent
	}

	// Compute hash before parsing
	hash := sha256.Sum256(content)
	hashStr := hex.EncodeToString(hash[:])

	// Create result
	result := &ParseResult{
		FilePath:      filePath,
		Language:      "markdown",
		Hash:          hashStr,
		ParsedAtMilli: time.Now().UnixMilli(),
		Symbols:       make([]*Symbol, 0),
		Imports:       make([]Import, 0),
		Errors:        make([]string, 0),
	}

	// Parse with tree-sitter
	parser := sitter.NewParser()
	parser.SetLanguage(tree_sitter_markdown.GetLanguage())

	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter parse failed: %w", err)
	}
	defer tree.Close()

	// Check context after parsing
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("markdown parse canceled after tree-sitter: %w", err)
	}

	// Extract symbols from AST
	rootNode := tree.RootNode()
	p.extractSymbols(ctx, rootNode, content, filePath, result, nil)

	// Validate result
	if err := result.Validate(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("validation error: %v", err))
	}

	return result, nil
}

// extractSymbols recursively extracts symbols from the AST.
func (p *MarkdownParser) extractSymbols(ctx context.Context, node *sitter.Node, content []byte, filePath string, result *ParseResult, parentHeading *Symbol) {
	if node == nil {
		return
	}

	// Check context periodically
	if ctx.Err() != nil {
		return
	}

	nodeType := node.Type()

	switch nodeType {
	case markdownNodeDocument:
		// Process all children
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(ctx, node.Child(i), content, filePath, result, nil)
		}

	case markdownNodeSection:
		// Sections contain headings and their content
		var sectionHeading *Symbol
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == markdownNodeAtxHeading {
				sectionHeading = p.extractHeading(child, content, filePath, result, parentHeading)
			} else {
				p.extractSymbols(ctx, child, content, filePath, result, sectionHeading)
			}
		}

	case markdownNodeAtxHeading:
		p.extractHeading(node, content, filePath, result, parentHeading)

	case markdownNodeFencedCodeBlock:
		if p.options.ExtractCodeBlocks {
			p.extractCodeBlock(node, content, filePath, result)
		}

	case markdownNodeIndentedCodeBlock:
		if p.options.ExtractCodeBlocks {
			p.extractIndentedCodeBlock(node, content, filePath, result)
		}

	case markdownNodeList:
		if p.options.ExtractLists {
			p.extractList(node, content, filePath, result)
		}

	case markdownNodeLinkRefDef:
		if p.options.ExtractLinks {
			p.extractLinkReference(node, content, filePath, result)
		}

	default:
		// Recurse into children
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(ctx, node.Child(i), content, filePath, result, parentHeading)
		}
	}
}

// extractHeading extracts a heading symbol.
func (p *MarkdownParser) extractHeading(node *sitter.Node, content []byte, filePath string, result *ParseResult, parentHeading *Symbol) *Symbol {
	level := 0
	headingText := ""

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case markdownNodeH1Marker:
			level = 1
		case markdownNodeH2Marker:
			level = 2
		case markdownNodeH3Marker:
			level = 3
		case markdownNodeH4Marker:
			level = 4
		case markdownNodeH5Marker:
			level = 5
		case markdownNodeH6Marker:
			level = 6
		case markdownNodeInline:
			headingText = strings.TrimSpace(string(content[child.StartByte():child.EndByte()]))
		}
	}

	// Skip if beyond max depth
	if level > p.options.MaxHeadingDepth {
		return nil
	}

	if headingText == "" {
		return nil
	}

	// Build signature with level indicator
	signature := strings.Repeat("#", level) + " " + headingText

	sym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, headingText),
		Name:          headingText,
		Kind:          SymbolKindHeading,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     signature,
		Language:      "markdown",
		ParsedAtMilli: time.Now().UnixMilli(),
		Exported:      true,
		Metadata: &SymbolMetadata{
			HeadingLevel: level,
		},
	}

	// Set parent heading relationship
	if parentHeading != nil {
		sym.Metadata.ParentName = parentHeading.Name
	}

	result.Symbols = append(result.Symbols, sym)
	return sym
}

// extractCodeBlock extracts a fenced code block symbol.
func (p *MarkdownParser) extractCodeBlock(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	language := ""
	codeContent := ""

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case markdownNodeInfoString:
			// Get language from info_string > language
			for j := 0; j < int(child.ChildCount()); j++ {
				langChild := child.Child(j)
				if langChild.Type() == markdownNodeLanguage {
					language = string(content[langChild.StartByte():langChild.EndByte()])
					break
				}
			}
		case markdownNodeCodeFenceContent:
			codeContent = string(content[child.StartByte():child.EndByte()])
		}
	}

	// Build name and signature
	name := "code_block"
	if language != "" {
		name = language + "_block"
	}

	// Preview first line of code
	preview := ""
	if codeContent != "" {
		lines := strings.SplitN(codeContent, "\n", 2)
		if len(lines) > 0 {
			preview = strings.TrimSpace(lines[0])
			if len(preview) > 40 {
				preview = preview[:40] + "..."
			}
		}
	}

	signature := "```"
	if language != "" {
		signature += language
	}
	if preview != "" {
		signature += " // " + preview
	}

	sym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, name),
		Name:          name,
		Kind:          SymbolKindCodeBlock,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     signature,
		Language:      "markdown",
		ParsedAtMilli: time.Now().UnixMilli(),
		Exported:      true,
	}

	if language != "" {
		sym.Metadata = &SymbolMetadata{
			CodeLanguage: language,
		}
	}

	result.Symbols = append(result.Symbols, sym)
}

// extractIndentedCodeBlock extracts an indented code block.
func (p *MarkdownParser) extractIndentedCodeBlock(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	codeContent := strings.TrimSpace(string(content[node.StartByte():node.EndByte()]))

	// Preview first line
	preview := ""
	lines := strings.SplitN(codeContent, "\n", 2)
	if len(lines) > 0 {
		preview = strings.TrimSpace(lines[0])
		if len(preview) > 40 {
			preview = preview[:40] + "..."
		}
	}

	signature := "(indented code block)"
	if preview != "" {
		signature += " // " + preview
	}

	sym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, "indented_code"),
		Name:          "indented_code",
		Kind:          SymbolKindCodeBlock,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     signature,
		Language:      "markdown",
		ParsedAtMilli: time.Now().UnixMilli(),
		Exported:      true,
	}

	result.Symbols = append(result.Symbols, sym)
}

// extractList extracts a list symbol.
func (p *MarkdownParser) extractList(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	// Determine list type (ordered vs unordered)
	listType := "unordered"
	itemCount := 0

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == markdownNodeListItem {
			itemCount++
			// Check first item's marker to determine type
			if itemCount == 1 {
				for j := 0; j < int(child.ChildCount()); j++ {
					marker := child.Child(j)
					switch marker.Type() {
					case markdownNodeListMarkerDot, markdownNodeListMarkerParen:
						listType = "ordered"
					}
				}
			}
		}
	}

	// Build signature
	var signature string
	if listType == "ordered" {
		signature = fmt.Sprintf("1. ... (%d items)", itemCount)
	} else {
		signature = fmt.Sprintf("- ... (%d items)", itemCount)
	}

	// Extract first item text for context
	firstItemText := p.extractFirstListItemText(node, content)
	if firstItemText != "" {
		if len(firstItemText) > 30 {
			firstItemText = firstItemText[:30] + "..."
		}
		if listType == "ordered" {
			signature = fmt.Sprintf("1. %s (%d items)", firstItemText, itemCount)
		} else {
			signature = fmt.Sprintf("- %s (%d items)", firstItemText, itemCount)
		}
	}

	sym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, "list"),
		Name:          listType + "_list",
		Kind:          SymbolKindList,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     signature,
		Language:      "markdown",
		ParsedAtMilli: time.Now().UnixMilli(),
		Exported:      true,
		Metadata: &SymbolMetadata{
			ListType:  listType,
			ListItems: itemCount,
		},
	}

	result.Symbols = append(result.Symbols, sym)
}

// extractFirstListItemText extracts the text of the first list item.
func (p *MarkdownParser) extractFirstListItemText(node *sitter.Node, content []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == markdownNodeListItem {
			// Find paragraph > inline within list item
			for j := 0; j < int(child.ChildCount()); j++ {
				itemChild := child.Child(j)
				if itemChild.Type() == markdownNodeParagraph {
					for k := 0; k < int(itemChild.ChildCount()); k++ {
						paraChild := itemChild.Child(k)
						if paraChild.Type() == markdownNodeInline {
							return strings.TrimSpace(string(content[paraChild.StartByte():paraChild.EndByte()]))
						}
					}
				}
			}
			break
		}
	}
	return ""
}

// extractLinkReference extracts a link reference definition.
func (p *MarkdownParser) extractLinkReference(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	label := ""
	destination := ""
	title := ""

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case markdownNodeLinkLabel:
			// Extract label text (remove brackets)
			labelText := string(content[child.StartByte():child.EndByte()])
			labelText = strings.TrimPrefix(labelText, "[")
			labelText = strings.TrimSuffix(labelText, "]")
			label = strings.TrimSpace(labelText)
		case markdownNodeLinkDest:
			destination = string(content[child.StartByte():child.EndByte()])
		case markdownNodeLinkTitle:
			titleText := string(content[child.StartByte():child.EndByte()])
			// Remove quotes
			titleText = strings.Trim(titleText, "\"'")
			title = titleText
		}
	}

	if label == "" {
		return
	}

	// Build signature
	signature := fmt.Sprintf("[%s]: %s", label, destination)
	if title != "" {
		signature += fmt.Sprintf(" \"%s\"", title)
	}

	sym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, label),
		Name:          label,
		Kind:          SymbolKindLink,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     signature,
		Language:      "markdown",
		ParsedAtMilli: time.Now().UnixMilli(),
		Exported:      true,
		Metadata: &SymbolMetadata{
			LinkURL:   destination,
			LinkTitle: title,
		},
	}

	result.Symbols = append(result.Symbols, sym)
}

// countHeadings returns the number of heading symbols.
func (p *MarkdownParser) countHeadings(result *ParseResult) int {
	count := 0
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindHeading {
			count++
		}
	}
	return count
}

// countCodeBlocks returns the number of code block symbols.
func (p *MarkdownParser) countCodeBlocks(result *ParseResult) int {
	count := 0
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindCodeBlock {
			count++
		}
	}
	return count
}
