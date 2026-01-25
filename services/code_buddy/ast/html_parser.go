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
	"github.com/smacker/go-tree-sitter/html"
)

// HTMLParser extracts symbols from HTML source code.
//
// Description:
//
//	HTMLParser uses tree-sitter to parse HTML source files and extract
//	structured symbol information including elements with IDs, forms,
//	custom elements (web components), and script/stylesheet references.
//	It delegates inline <script> and <style> content to JavaScriptParser
//	and CSSParser respectively.
//
// Thread Safety:
//
//	HTMLParser is safe for concurrent use. Multiple goroutines can call Parse
//	simultaneously. Each Parse call creates its own tree-sitter parser instance.
//
// Example:
//
//	parser := NewHTMLParser()
//	result, err := parser.Parse(ctx, content, "index.html")
//	if err != nil {
//	    return fmt.Errorf("parse: %w", err)
//	}
//	for _, sym := range result.Symbols {
//	    fmt.Printf("%s: %s\n", sym.Kind, sym.Name)
//	}
type HTMLParser struct {
	options   HTMLParserOptions
	jsParser  *JavaScriptParser
	cssParser *CSSParser
}

// HTMLParserOptions configures HTMLParser behavior.
type HTMLParserOptions struct {
	// MaxFileSize is the maximum file size in bytes to parse.
	// Files larger than this return ErrFileTooLarge.
	// Default: 10MB
	MaxFileSize int

	// ParseInlineScripts determines whether to parse inline <script> content.
	// Default: true
	ParseInlineScripts bool

	// ParseInlineStyles determines whether to parse inline <style> content.
	// Default: true
	ParseInlineStyles bool
}

// DefaultHTMLParserOptions returns the default options.
func DefaultHTMLParserOptions() HTMLParserOptions {
	return HTMLParserOptions{
		MaxFileSize:        10 * 1024 * 1024, // 10MB
		ParseInlineScripts: true,
		ParseInlineStyles:  true,
	}
}

// HTMLParserOption is a functional option for configuring HTMLParser.
type HTMLParserOption func(*HTMLParserOptions)

// WithHTMLMaxFileSize sets the maximum file size for parsing.
func WithHTMLMaxFileSize(size int) HTMLParserOption {
	return func(o *HTMLParserOptions) {
		o.MaxFileSize = size
	}
}

// WithHTMLParseInlineScripts sets whether to parse inline scripts.
func WithHTMLParseInlineScripts(parse bool) HTMLParserOption {
	return func(o *HTMLParserOptions) {
		o.ParseInlineScripts = parse
	}
}

// WithHTMLParseInlineStyles sets whether to parse inline styles.
func WithHTMLParseInlineStyles(parse bool) HTMLParserOption {
	return func(o *HTMLParserOptions) {
		o.ParseInlineStyles = parse
	}
}

// NewHTMLParser creates a new HTMLParser with the given options.
//
// Description:
//
//	Creates a parser configured for HTML source files. The parser can be
//	reused for multiple files and is safe for concurrent use.
//
// Example:
//
//	// Default options
//	parser := NewHTMLParser()
//
//	// With custom options
//	parser := NewHTMLParser(
//	    WithHTMLMaxFileSize(5 * 1024 * 1024),
//	    WithHTMLParseInlineScripts(false),
//	)
func NewHTMLParser(opts ...HTMLParserOption) *HTMLParser {
	options := DefaultHTMLParserOptions()
	for _, opt := range opts {
		opt(&options)
	}
	return &HTMLParser{
		options:   options,
		jsParser:  NewJavaScriptParser(),
		cssParser: NewCSSParser(),
	}
}

// Language returns the language name for this parser.
func (p *HTMLParser) Language() string {
	return "html"
}

// Extensions returns the file extensions this parser handles.
func (p *HTMLParser) Extensions() []string {
	return []string{".html", ".htm"}
}

// Parse extracts symbols from HTML source code.
//
// Description:
//
//	Parses the provided HTML content using tree-sitter and extracts all
//	symbols including elements with IDs, forms, custom elements, and imports.
//	Inline <script> and <style> content is delegated to the respective parsers.
//
// Inputs:
//
//	ctx      - Context for cancellation. Checked before/after parsing and delegation.
//	content  - Raw HTML source bytes. Must be valid UTF-8.
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
func (p *HTMLParser) Parse(ctx context.Context, content []byte, filePath string) (*ParseResult, error) {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("html parse canceled before start: %w", err)
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
		Language:      "html",
		Hash:          hashStr,
		ParsedAtMilli: time.Now().UnixMilli(),
		Symbols:       make([]*Symbol, 0),
		Imports:       make([]Import, 0),
		Errors:        make([]string, 0),
	}

	// Parse with tree-sitter
	parser := sitter.NewParser()
	parser.SetLanguage(html.GetLanguage())

	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter parse failed: %w", err)
	}
	defer tree.Close()

	// Check context after parsing
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("html parse canceled after tree-sitter: %w", err)
	}

	// Extract symbols from AST
	rootNode := tree.RootNode()
	p.extractSymbols(ctx, rootNode, content, filePath, result)

	// Validate result
	if err := result.Validate(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("validation error: %v", err))
	}

	return result, nil
}

// extractSymbols recursively extracts symbols from the AST.
func (p *HTMLParser) extractSymbols(ctx context.Context, node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	if node == nil {
		return
	}

	// Check context periodically
	if ctx.Err() != nil {
		return
	}

	nodeType := node.Type()

	switch nodeType {
	case htmlNodeDocument:
		// Process all children
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(ctx, node.Child(i), content, filePath, result)
		}

	case htmlNodeElement:
		p.extractElement(ctx, node, content, filePath, result)

	case htmlNodeScriptElement:
		p.extractScript(ctx, node, content, filePath, result)

	case htmlNodeStyleElement:
		p.extractStyle(ctx, node, content, filePath, result)

	default:
		// Recurse into children for other node types
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(ctx, node.Child(i), content, filePath, result)
		}
	}
}

// extractElement extracts an HTML element if it has relevant attributes.
func (p *HTMLParser) extractElement(ctx context.Context, node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	tagName := ""
	id := ""
	name := ""
	href := ""
	rel := ""

	// Get start tag
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == htmlNodeStartTag || child.Type() == htmlNodeSelfClosing {
			tagName, id, name, href, rel = p.extractTagInfo(child, content)
			break
		}
	}

	// Extract element with ID
	if id != "" {
		sym := &Symbol{
			ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, id),
			Name:          id,
			Kind:          SymbolKindElement,
			FilePath:      filePath,
			StartLine:     int(node.StartPoint().Row) + 1,
			EndLine:       int(node.EndPoint().Row) + 1,
			StartCol:      int(node.StartPoint().Column),
			EndCol:        int(node.EndPoint().Column),
			Signature:     fmt.Sprintf("<%s id=%q>", tagName, id),
			Language:      "html",
			ParsedAtMilli: time.Now().UnixMilli(),
			Exported:      true,
		}
		result.Symbols = append(result.Symbols, sym)
	}

	// Extract form with name
	if tagName == "form" && name != "" {
		sym := &Symbol{
			ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, "form:"+name),
			Name:          name,
			Kind:          SymbolKindForm,
			FilePath:      filePath,
			StartLine:     int(node.StartPoint().Row) + 1,
			EndLine:       int(node.EndPoint().Row) + 1,
			StartCol:      int(node.StartPoint().Column),
			EndCol:        int(node.EndPoint().Column),
			Signature:     fmt.Sprintf("<form name=%q>", name),
			Language:      "html",
			ParsedAtMilli: time.Now().UnixMilli(),
			Exported:      true,
		}
		result.Symbols = append(result.Symbols, sym)
	}

	// Extract custom element (web component)
	if strings.Contains(tagName, "-") && tagName != "" {
		sym := &Symbol{
			ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, tagName),
			Name:          tagName,
			Kind:          SymbolKindComponent,
			FilePath:      filePath,
			StartLine:     int(node.StartPoint().Row) + 1,
			EndLine:       int(node.EndPoint().Row) + 1,
			StartCol:      int(node.StartPoint().Column),
			EndCol:        int(node.EndPoint().Column),
			Signature:     fmt.Sprintf("<%s>", tagName),
			Language:      "html",
			ParsedAtMilli: time.Now().UnixMilli(),
			Exported:      true,
		}
		result.Symbols = append(result.Symbols, sym)
	}

	// Extract stylesheet link
	if tagName == "link" && rel == "stylesheet" && href != "" {
		imp := Import{
			Path:         href,
			IsStylesheet: true,
			Location: Location{
				FilePath:  filePath,
				StartLine: int(node.StartPoint().Row) + 1,
				EndLine:   int(node.EndPoint().Row) + 1,
				StartCol:  int(node.StartPoint().Column),
				EndCol:    int(node.EndPoint().Column),
			},
		}
		result.Imports = append(result.Imports, imp)

		sym := &Symbol{
			ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, href),
			Name:          href,
			Kind:          SymbolKindImport,
			FilePath:      filePath,
			StartLine:     int(node.StartPoint().Row) + 1,
			EndLine:       int(node.EndPoint().Row) + 1,
			StartCol:      int(node.StartPoint().Column),
			EndCol:        int(node.EndPoint().Column),
			Language:      "html",
			ParsedAtMilli: time.Now().UnixMilli(),
		}
		result.Symbols = append(result.Symbols, sym)
	}

	// Recurse into children
	for i := 0; i < int(node.ChildCount()); i++ {
		p.extractSymbols(ctx, node.Child(i), content, filePath, result)
	}
}

// extractTagInfo extracts tag information from a start_tag or self_closing_tag.
func (p *HTMLParser) extractTagInfo(node *sitter.Node, content []byte) (tagName, id, name, href, rel string) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case htmlNodeTagName:
			tagName = string(content[child.StartByte():child.EndByte()])
		case htmlNodeAttribute:
			attrName, attrValue := p.extractAttribute(child, content)
			switch attrName {
			case "id":
				id = attrValue
			case "name":
				name = attrValue
			case "href":
				href = attrValue
			case "rel":
				rel = attrValue
			}
		}
	}
	return
}

// extractAttribute extracts attribute name and value.
func (p *HTMLParser) extractAttribute(node *sitter.Node, content []byte) (name, value string) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case htmlNodeAttributeName:
			name = string(content[child.StartByte():child.EndByte()])
		case htmlNodeQuotedAttributeValue:
			// Get the actual value inside quotes
			for j := 0; j < int(child.ChildCount()); j++ {
				gc := child.Child(j)
				if gc.Type() == htmlNodeAttributeValue {
					value = string(content[gc.StartByte():gc.EndByte()])
				}
			}
		case htmlNodeAttributeValue:
			value = string(content[child.StartByte():child.EndByte()])
		}
	}
	return
}

// extractScript extracts script elements and delegates inline content.
func (p *HTMLParser) extractScript(ctx context.Context, node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	src := ""
	isModule := false
	var rawTextNode *sitter.Node

	// Get start tag info
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case htmlNodeStartTag:
			src, isModule = p.extractScriptTagInfo(child, content)
		case htmlNodeRawText:
			rawTextNode = child
		}
	}

	// External script reference
	if src != "" {
		imp := Import{
			Path:     src,
			IsScript: true,
			IsModule: isModule,
			Location: Location{
				FilePath:  filePath,
				StartLine: int(node.StartPoint().Row) + 1,
				EndLine:   int(node.EndPoint().Row) + 1,
				StartCol:  int(node.StartPoint().Column),
				EndCol:    int(node.EndPoint().Column),
			},
		}
		result.Imports = append(result.Imports, imp)

		sym := &Symbol{
			ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, src),
			Name:          src,
			Kind:          SymbolKindImport,
			FilePath:      filePath,
			StartLine:     int(node.StartPoint().Row) + 1,
			EndLine:       int(node.EndPoint().Row) + 1,
			StartCol:      int(node.StartPoint().Column),
			EndCol:        int(node.EndPoint().Column),
			Language:      "html",
			ParsedAtMilli: time.Now().UnixMilli(),
		}
		result.Symbols = append(result.Symbols, sym)
	}

	// Inline script - delegate to JavaScript parser
	if rawTextNode != nil && src == "" && p.options.ParseInlineScripts {
		// Check context before delegation
		if ctx.Err() != nil {
			return
		}

		scriptContent := content[rawTextNode.StartByte():rawTextNode.EndByte()]
		if len(strings.TrimSpace(string(scriptContent))) > 0 {
			jsResult, err := p.jsParser.Parse(ctx, scriptContent, filePath+":<script>")
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("inline script parse error: %v", err))
			} else {
				// Merge JavaScript symbols into result
				for _, sym := range jsResult.Symbols {
					// Adjust line numbers relative to script position
					sym.StartLine += int(rawTextNode.StartPoint().Row)
					sym.EndLine += int(rawTextNode.StartPoint().Row)
					result.Symbols = append(result.Symbols, sym)
				}
				result.Errors = append(result.Errors, jsResult.Errors...)
			}
		}
	}
}

// extractScriptTagInfo extracts src and type from a script start tag.
func (p *HTMLParser) extractScriptTagInfo(node *sitter.Node, content []byte) (src string, isModule bool) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == htmlNodeAttribute {
			attrName, attrValue := p.extractAttribute(child, content)
			switch attrName {
			case "src":
				src = attrValue
			case "type":
				isModule = attrValue == "module"
			}
		}
	}
	return
}

// extractStyle extracts style elements and delegates content.
func (p *HTMLParser) extractStyle(ctx context.Context, node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	var rawTextNode *sitter.Node

	// Find raw text content
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == htmlNodeRawText {
			rawTextNode = child
			break
		}
	}

	// Inline style - delegate to CSS parser
	if rawTextNode != nil && p.options.ParseInlineStyles {
		// Check context before delegation
		if ctx.Err() != nil {
			return
		}

		styleContent := content[rawTextNode.StartByte():rawTextNode.EndByte()]
		if len(strings.TrimSpace(string(styleContent))) > 0 {
			cssResult, err := p.cssParser.Parse(ctx, styleContent, filePath+":<style>")
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("inline style parse error: %v", err))
			} else {
				// Merge CSS symbols into result
				for _, sym := range cssResult.Symbols {
					// Adjust line numbers relative to style position
					sym.StartLine += int(rawTextNode.StartPoint().Row)
					sym.EndLine += int(rawTextNode.StartPoint().Row)
					result.Symbols = append(result.Symbols, sym)
				}
				result.Errors = append(result.Errors, cssResult.Errors...)
			}
		}
	}
}
