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
	"github.com/smacker/go-tree-sitter/css"
)

// CSSParser extracts symbols from CSS source code.
//
// Description:
//
//	CSSParser uses tree-sitter to parse CSS source files and extract
//	structured symbol information including class selectors, ID selectors,
//	CSS custom properties (variables), animations, and media queries.
//
// Thread Safety:
//
//	CSSParser is safe for concurrent use. Multiple goroutines can call Parse
//	simultaneously. Each Parse call creates its own tree-sitter parser instance.
//
// Example:
//
//	parser := NewCSSParser()
//	result, err := parser.Parse(ctx, content, "styles.css")
//	if err != nil {
//	    return fmt.Errorf("parse: %w", err)
//	}
//	for _, sym := range result.Symbols {
//	    fmt.Printf("%s: %s\n", sym.Kind, sym.Name)
//	}
type CSSParser struct {
	options CSSParserOptions
}

// CSSParserOptions configures CSSParser behavior.
type CSSParserOptions struct {
	// MaxFileSize is the maximum file size in bytes to parse.
	// Files larger than this return ErrFileTooLarge.
	// Default: 10MB
	MaxFileSize int

	// ExtractNestedClasses determines whether to extract classes inside media queries.
	// Default: true
	ExtractNestedClasses bool
}

// DefaultCSSParserOptions returns the default options.
func DefaultCSSParserOptions() CSSParserOptions {
	return CSSParserOptions{
		MaxFileSize:          10 * 1024 * 1024, // 10MB
		ExtractNestedClasses: true,
	}
}

// CSSParserOption is a functional option for configuring CSSParser.
type CSSParserOption func(*CSSParserOptions)

// WithCSSMaxFileSize sets the maximum file size for parsing.
func WithCSSMaxFileSize(size int) CSSParserOption {
	return func(o *CSSParserOptions) {
		o.MaxFileSize = size
	}
}

// WithCSSExtractNestedClasses sets whether to extract classes inside media queries.
func WithCSSExtractNestedClasses(extract bool) CSSParserOption {
	return func(o *CSSParserOptions) {
		o.ExtractNestedClasses = extract
	}
}

// NewCSSParser creates a new CSSParser with the given options.
//
// Description:
//
//	Creates a parser configured for CSS source files. The parser can be
//	reused for multiple files and is safe for concurrent use.
//
// Example:
//
//	// Default options
//	parser := NewCSSParser()
//
//	// With custom options
//	parser := NewCSSParser(
//	    WithCSSMaxFileSize(5 * 1024 * 1024),
//	)
func NewCSSParser(opts ...CSSParserOption) *CSSParser {
	options := DefaultCSSParserOptions()
	for _, opt := range opts {
		opt(&options)
	}
	return &CSSParser{options: options}
}

// Language returns the language name for this parser.
func (p *CSSParser) Language() string {
	return "css"
}

// Extensions returns the file extensions this parser handles.
func (p *CSSParser) Extensions() []string {
	return []string{".css"}
}

// Parse extracts symbols from CSS source code.
//
// Description:
//
//	Parses the provided CSS content using tree-sitter and extracts all
//	symbols including class selectors, ID selectors, CSS variables,
//	animations, and media queries.
//
// Inputs:
//
//	ctx      - Context for cancellation. Checked before and after parsing.
//	content  - Raw CSS source bytes. Must be valid UTF-8.
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
func (p *CSSParser) Parse(ctx context.Context, content []byte, filePath string) (*ParseResult, error) {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("css parse canceled before start: %w", err)
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
		Language:      "css",
		Hash:          hashStr,
		ParsedAtMilli: time.Now().UnixMilli(),
		Symbols:       make([]*Symbol, 0),
		Imports:       make([]Import, 0),
		Errors:        make([]string, 0),
	}

	// Parse with tree-sitter
	parser := sitter.NewParser()
	parser.SetLanguage(css.GetLanguage())

	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter parse failed: %w", err)
	}
	defer tree.Close()

	// Check context after parsing
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("css parse canceled after tree-sitter: %w", err)
	}

	// Track seen symbols to avoid duplicates
	seen := make(map[string]bool)

	// Extract symbols from AST
	rootNode := tree.RootNode()
	p.extractSymbols(rootNode, content, filePath, result, seen, "")

	// Validate result
	if err := result.Validate(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("validation error: %v", err))
	}

	return result, nil
}

// extractSymbols recursively extracts symbols from the AST.
func (p *CSSParser) extractSymbols(node *sitter.Node, content []byte, filePath string, result *ParseResult, seen map[string]bool, context string) {
	if node == nil {
		return
	}

	nodeType := node.Type()

	switch nodeType {
	case cssNodeStylesheet:
		// Process all children
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(node.Child(i), content, filePath, result, seen, context)
		}

	case cssNodeImportStatement:
		p.extractImport(node, content, filePath, result)

	case cssNodeRuleSet:
		p.extractRuleSet(node, content, filePath, result, seen, context)

	case cssNodeKeyframesStatement:
		p.extractKeyframes(node, content, filePath, result)

	case cssNodeMediaStatement:
		p.extractMedia(node, content, filePath, result, seen)

	default:
		// Recurse into children for other node types
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(node.Child(i), content, filePath, result, seen, context)
		}
	}
}

// extractImport extracts an @import statement.
func (p *CSSParser) extractImport(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	path := ""
	mediaQuery := ""

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case cssNodeCallExpression:
			// url('...')
			path = p.extractURLValue(child, content)
		case cssNodeStringValue:
			// Direct string import
			path = p.extractStringValue(child, content)
		case cssNodePlainValue, "keyword_query":
			// Media query part (can be plain_value or keyword_query)
			if path != "" {
				mediaQuery = string(content[child.StartByte():child.EndByte()])
			}
		}
	}

	if path != "" {
		imp := Import{
			Path:         path,
			IsStylesheet: true,
			MediaQuery:   mediaQuery,
			Location: Location{
				FilePath:  filePath,
				StartLine: int(node.StartPoint().Row) + 1,
				EndLine:   int(node.EndPoint().Row) + 1,
				StartCol:  int(node.StartPoint().Column),
				EndCol:    int(node.EndPoint().Column),
			},
		}
		result.Imports = append(result.Imports, imp)

		// Also add as symbol
		sym := &Symbol{
			ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, path),
			Name:          path,
			Kind:          SymbolKindImport,
			FilePath:      filePath,
			StartLine:     int(node.StartPoint().Row) + 1,
			EndLine:       int(node.EndPoint().Row) + 1,
			StartCol:      int(node.StartPoint().Column),
			EndCol:        int(node.EndPoint().Column),
			Language:      "css",
			ParsedAtMilli: time.Now().UnixMilli(),
		}
		result.Symbols = append(result.Symbols, sym)
	}
}

// extractURLValue extracts the URL from a url() call expression.
func (p *CSSParser) extractURLValue(node *sitter.Node, content []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == cssNodeArguments {
			for j := 0; j < int(child.ChildCount()); j++ {
				gc := child.Child(j)
				if gc.Type() == cssNodeStringValue {
					return p.extractStringValue(gc, content)
				}
			}
		}
	}
	return ""
}

// extractStringValue extracts the content of a string value without quotes.
func (p *CSSParser) extractStringValue(node *sitter.Node, content []byte) string {
	text := string(content[node.StartByte():node.EndByte()])
	// Remove quotes
	if len(text) >= 2 && (text[0] == '"' || text[0] == '\'') {
		return text[1 : len(text)-1]
	}
	return text
}

// extractRuleSet extracts selectors and CSS variables from a rule set.
func (p *CSSParser) extractRuleSet(node *sitter.Node, content []byte, filePath string, result *ParseResult, seen map[string]bool, context string) {
	var selectorNode *sitter.Node
	var blockNode *sitter.Node

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case cssNodeSelectors:
			selectorNode = child
		case cssNodeBlock:
			blockNode = child
		}
	}

	// Extract selectors
	if selectorNode != nil {
		p.extractSelectors(selectorNode, content, filePath, result, seen, context)
	}

	// Extract CSS variables from block
	if blockNode != nil {
		p.extractCSSVariables(blockNode, content, filePath, result, seen)
	}
}

// extractSelectors extracts class and ID selectors.
func (p *CSSParser) extractSelectors(node *sitter.Node, content []byte, filePath string, result *ParseResult, seen map[string]bool, context string) {
	p.extractSelectorsRecursive(node, content, filePath, result, seen, context)
}

// extractSelectorsRecursive recursively extracts class and ID names from selectors.
func (p *CSSParser) extractSelectorsRecursive(node *sitter.Node, content []byte, filePath string, result *ParseResult, seen map[string]bool, context string) {
	if node == nil {
		return
	}

	switch node.Type() {
	case cssNodeClassSelector:
		// Extract class name
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == cssNodeClassName {
				name := string(content[child.StartByte():child.EndByte()])
				key := "class:" + name
				if !seen[key] {
					seen[key] = true
					sym := &Symbol{
						ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, "."+name),
						Name:          name,
						Kind:          SymbolKindCSSClass,
						FilePath:      filePath,
						StartLine:     int(node.StartPoint().Row) + 1,
						EndLine:       int(node.EndPoint().Row) + 1,
						StartCol:      int(node.StartPoint().Column),
						EndCol:        int(node.EndPoint().Column),
						Signature:     "." + name,
						Language:      "css",
						ParsedAtMilli: time.Now().UnixMilli(),
						Exported:      true,
					}
					if context != "" {
						sym.Metadata = &SymbolMetadata{
							CSSSelector: context,
						}
					}
					result.Symbols = append(result.Symbols, sym)
				}
			}
		}

	case cssNodeIDSelector:
		// Extract ID name
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == cssNodeIDName {
				name := string(content[child.StartByte():child.EndByte()])
				key := "id:" + name
				if !seen[key] {
					seen[key] = true
					sym := &Symbol{
						ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, "#"+name),
						Name:          name,
						Kind:          SymbolKindCSSID,
						FilePath:      filePath,
						StartLine:     int(node.StartPoint().Row) + 1,
						EndLine:       int(node.EndPoint().Row) + 1,
						StartCol:      int(node.StartPoint().Column),
						EndCol:        int(node.EndPoint().Column),
						Signature:     "#" + name,
						Language:      "css",
						ParsedAtMilli: time.Now().UnixMilli(),
						Exported:      true,
					}
					result.Symbols = append(result.Symbols, sym)
				}
			}
		}
	}

	// Recurse into children
	for i := 0; i < int(node.ChildCount()); i++ {
		p.extractSelectorsRecursive(node.Child(i), content, filePath, result, seen, context)
	}
}

// extractCSSVariables extracts CSS custom properties from a block.
func (p *CSSParser) extractCSSVariables(node *sitter.Node, content []byte, filePath string, result *ParseResult, seen map[string]bool) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == cssNodeDeclaration {
			p.extractVariable(child, content, filePath, result, seen)
		}
	}
}

// extractVariable extracts a CSS custom property if the property name starts with --.
func (p *CSSParser) extractVariable(node *sitter.Node, content []byte, filePath string, result *ParseResult, seen map[string]bool) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == cssNodePropertyName {
			name := string(content[child.StartByte():child.EndByte()])
			if strings.HasPrefix(name, "--") {
				key := "var:" + name
				if !seen[key] {
					seen[key] = true
					// Get the full declaration text as signature
					sig := strings.TrimSpace(string(content[node.StartByte():node.EndByte()]))
					// Truncate if too long
					if len(sig) > 100 {
						sig = sig[:100] + "..."
					}
					sym := &Symbol{
						ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, name),
						Name:          name,
						Kind:          SymbolKindCSSVariable,
						FilePath:      filePath,
						StartLine:     int(node.StartPoint().Row) + 1,
						EndLine:       int(node.EndPoint().Row) + 1,
						StartCol:      int(node.StartPoint().Column),
						EndCol:        int(node.EndPoint().Column),
						Signature:     sig,
						Language:      "css",
						ParsedAtMilli: time.Now().UnixMilli(),
						Exported:      true,
					}
					result.Symbols = append(result.Symbols, sym)
				}
			}
		}
	}
}

// extractKeyframes extracts a @keyframes animation.
func (p *CSSParser) extractKeyframes(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	name := ""

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == cssNodeKeyframesName {
			name = string(content[child.StartByte():child.EndByte()])
		}
	}

	if name != "" {
		sym := &Symbol{
			ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, "@keyframes "+name),
			Name:          name,
			Kind:          SymbolKindAnimation,
			FilePath:      filePath,
			StartLine:     int(node.StartPoint().Row) + 1,
			EndLine:       int(node.EndPoint().Row) + 1,
			StartCol:      int(node.StartPoint().Column),
			EndCol:        int(node.EndPoint().Column),
			Signature:     "@keyframes " + name,
			Language:      "css",
			ParsedAtMilli: time.Now().UnixMilli(),
			Exported:      true,
		}
		result.Symbols = append(result.Symbols, sym)
	}
}

// extractMedia extracts a @media query and its nested rules.
func (p *CSSParser) extractMedia(node *sitter.Node, content []byte, filePath string, result *ParseResult, seen map[string]bool) {
	query := ""
	var blockNode *sitter.Node

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case cssNodeFeatureQuery:
			query = strings.TrimSpace(string(content[child.StartByte():child.EndByte()]))
		case cssNodeBlock:
			blockNode = child
		}
	}

	// Add media query as symbol
	if query != "" {
		sym := &Symbol{
			ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, "@media "+query),
			Name:          query,
			Kind:          SymbolKindMediaQuery,
			FilePath:      filePath,
			StartLine:     int(node.StartPoint().Row) + 1,
			EndLine:       int(node.EndPoint().Row) + 1,
			StartCol:      int(node.StartPoint().Column),
			EndCol:        int(node.EndPoint().Column),
			Signature:     "@media " + query,
			Language:      "css",
			ParsedAtMilli: time.Now().UnixMilli(),
			Exported:      true,
		}
		result.Symbols = append(result.Symbols, sym)
	}

	// Extract nested rules
	if blockNode != nil && p.options.ExtractNestedClasses {
		context := "@media " + query
		for i := 0; i < int(blockNode.ChildCount()); i++ {
			child := blockNode.Child(i)
			if child.Type() == cssNodeRuleSet {
				p.extractRuleSet(child, content, filePath, result, seen, context)
			}
		}
	}
}

// getPrecedingComment extracts a comment before a node.
func (p *CSSParser) getPrecedingComment(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}

	prev := node.PrevSibling()
	if prev != nil && prev.Type() == cssNodeComment {
		return string(content[prev.StartByte():prev.EndByte()])
	}

	return ""
}
