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
	"github.com/smacker/go-tree-sitter/yaml"
)

// YAMLParser extracts symbols from YAML source files.
//
// Description:
//
//	YAMLParser uses tree-sitter to parse YAML files and extract
//	structured symbol information including top-level keys, nested keys,
//	anchors, and document boundaries.
//
// Thread Safety:
//
//	YAMLParser is safe for concurrent use. Multiple goroutines can call Parse
//	simultaneously. Each Parse call creates its own tree-sitter parser instance.
//
// Example:
//
//	parser := NewYAMLParser()
//	result, err := parser.Parse(ctx, content, "config.yaml")
//	if err != nil {
//	    return fmt.Errorf("parse: %w", err)
//	}
//	for _, sym := range result.Symbols {
//	    fmt.Printf("%s: %s\n", sym.Kind, sym.Name)
//	}
type YAMLParser struct {
	options YAMLParserOptions
}

// YAMLParserOptions configures YAMLParser behavior.
type YAMLParserOptions struct {
	// MaxFileSize is the maximum file size in bytes to parse.
	// Files larger than this return ErrFileTooLarge.
	// Default: 10MB
	MaxFileSize int

	// MaxKeyDepth is the maximum nesting depth for key extraction.
	// 0 = top-level only, 1 = one level deep, etc.
	// Default: 3
	MaxKeyDepth int

	// ExtractAnchors determines whether to extract anchor definitions.
	// Default: true
	ExtractAnchors bool
}

// DefaultYAMLParserOptions returns the default options.
func DefaultYAMLParserOptions() YAMLParserOptions {
	return YAMLParserOptions{
		MaxFileSize:    10 * 1024 * 1024, // 10MB
		MaxKeyDepth:    3,
		ExtractAnchors: true,
	}
}

// YAMLParserOption is a functional option for configuring YAMLParser.
type YAMLParserOption func(*YAMLParserOptions)

// WithYAMLMaxFileSize sets the maximum file size for parsing.
func WithYAMLMaxFileSize(size int) YAMLParserOption {
	return func(o *YAMLParserOptions) {
		o.MaxFileSize = size
	}
}

// WithYAMLMaxKeyDepth sets the maximum key depth for extraction.
func WithYAMLMaxKeyDepth(depth int) YAMLParserOption {
	return func(o *YAMLParserOptions) {
		o.MaxKeyDepth = depth
	}
}

// WithYAMLExtractAnchors sets whether to extract anchor definitions.
func WithYAMLExtractAnchors(extract bool) YAMLParserOption {
	return func(o *YAMLParserOptions) {
		o.ExtractAnchors = extract
	}
}

// NewYAMLParser creates a new YAMLParser with the given options.
//
// Description:
//
//	Creates a parser configured for YAML files. The parser can be
//	reused for multiple files and is safe for concurrent use.
//
// Example:
//
//	// Default options
//	parser := NewYAMLParser()
//
//	// With custom options
//	parser := NewYAMLParser(
//	    WithYAMLMaxFileSize(5 * 1024 * 1024),
//	    WithYAMLMaxKeyDepth(5),
//	)
func NewYAMLParser(opts ...YAMLParserOption) *YAMLParser {
	options := DefaultYAMLParserOptions()
	for _, opt := range opts {
		opt(&options)
	}
	return &YAMLParser{
		options: options,
	}
}

// Language returns the language name for this parser.
func (p *YAMLParser) Language() string {
	return "yaml"
}

// Extensions returns the file extensions this parser handles.
func (p *YAMLParser) Extensions() []string {
	return []string{".yaml", ".yml"}
}

// Parse extracts symbols from YAML source code.
//
// Description:
//
//	Parses the provided YAML content using tree-sitter and extracts all
//	symbols including keys, anchors, and document boundaries.
//
// Inputs:
//
//	ctx      - Context for cancellation. Checked before/after parsing.
//	content  - Raw YAML source bytes. Must be valid UTF-8.
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
func (p *YAMLParser) Parse(ctx context.Context, content []byte, filePath string) (*ParseResult, error) {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("yaml parse canceled before start: %w", err)
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
		Language:      "yaml",
		Hash:          hashStr,
		ParsedAtMilli: time.Now().UnixMilli(),
		Symbols:       make([]*Symbol, 0),
		Imports:       make([]Import, 0),
		Errors:        make([]string, 0),
	}

	// Parse with tree-sitter
	parser := sitter.NewParser()
	parser.SetLanguage(yaml.GetLanguage())

	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter parse failed: %w", err)
	}
	defer tree.Close()

	// Check context after parsing
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("yaml parse canceled after tree-sitter: %w", err)
	}

	// Extract symbols from AST
	rootNode := tree.RootNode()
	docIndex := 0
	p.extractSymbols(ctx, rootNode, content, filePath, result, "", 0, &docIndex)

	// Validate result
	if err := result.Validate(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("validation error: %v", err))
	}

	return result, nil
}

// extractSymbols recursively extracts symbols from the AST.
func (p *YAMLParser) extractSymbols(ctx context.Context, node *sitter.Node, content []byte, filePath string, result *ParseResult, keyPath string, depth int, docIndex *int) {
	if node == nil {
		return
	}

	// Check context periodically
	if ctx.Err() != nil {
		return
	}

	nodeType := node.Type()

	switch nodeType {
	case yamlNodeStream:
		// Process all children (documents)
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(ctx, node.Child(i), content, filePath, result, "", 0, docIndex)
		}

	case yamlNodeDocument:
		// Track document if there are multiple
		*docIndex++
		if *docIndex > 1 {
			sym := &Symbol{
				ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, fmt.Sprintf("document_%d", *docIndex)),
				Name:          fmt.Sprintf("document_%d", *docIndex),
				Kind:          SymbolKindDocument,
				FilePath:      filePath,
				StartLine:     int(node.StartPoint().Row) + 1,
				EndLine:       int(node.EndPoint().Row) + 1,
				StartCol:      int(node.StartPoint().Column),
				EndCol:        int(node.EndPoint().Column),
				Signature:     fmt.Sprintf("--- # Document %d", *docIndex),
				Language:      "yaml",
				ParsedAtMilli: time.Now().UnixMilli(),
				Exported:      true,
			}
			result.Symbols = append(result.Symbols, sym)
		}
		// Process document content
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(ctx, node.Child(i), content, filePath, result, "", 0, docIndex)
		}

	case yamlNodeBlockNode, yamlNodeFlowNode:
		// Just recurse
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(ctx, node.Child(i), content, filePath, result, keyPath, depth, docIndex)
		}

	case yamlNodeBlockMapping, yamlNodeFlowMapping:
		// Process mapping pairs
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(ctx, node.Child(i), content, filePath, result, keyPath, depth, docIndex)
		}

	case yamlNodeBlockMappingPair, yamlNodeFlowPair:
		p.extractKeyValuePair(ctx, node, content, filePath, result, keyPath, depth, docIndex)

	case yamlNodeAnchor:
		if p.options.ExtractAnchors {
			p.extractAnchor(node, content, filePath, result)
		}

	default:
		// Recurse into children
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(ctx, node.Child(i), content, filePath, result, keyPath, depth, docIndex)
		}
	}
}

// extractKeyValuePair extracts a key-value pair symbol.
func (p *YAMLParser) extractKeyValuePair(ctx context.Context, node *sitter.Node, content []byte, filePath string, result *ParseResult, parentPath string, depth int, docIndex *int) {
	// Skip if too deep
	if depth > p.options.MaxKeyDepth {
		return
	}

	var keyNode *sitter.Node
	var valueNode *sitter.Node

	// Find key and value nodes
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		childType := child.Type()

		if childType == yamlNodeFlowNode || childType == yamlNodeBlockNode {
			// First flow_node is typically the key
			if keyNode == nil {
				keyNode = child
			} else {
				valueNode = child
			}
		}
	}

	if keyNode == nil {
		return
	}

	// Extract key name
	keyName := p.extractScalarValue(keyNode, content)
	if keyName == "" {
		return
	}

	// Build full key path
	fullPath := keyName
	if parentPath != "" {
		fullPath = parentPath + "." + keyName
	}

	// Build signature with value preview
	valuePreview := ""
	if valueNode != nil {
		valuePreview = p.extractScalarValue(valueNode, content)
		if valuePreview == "" {
			// Check for nested mapping or sequence
			if p.hasNestedStructure(valueNode) {
				valuePreview = "..."
			}
		}
	}

	signature := keyName
	if valuePreview != "" {
		displayValue := valuePreview
		if len(displayValue) > 50 {
			displayValue = displayValue[:50] + "..."
		}
		signature += ": " + displayValue
	}

	sym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, fullPath),
		Name:          keyName,
		Kind:          SymbolKindKey,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     signature,
		Language:      "yaml",
		ParsedAtMilli: time.Now().UnixMilli(),
		Exported:      true,
	}

	// Add parent path to metadata if nested
	if parentPath != "" {
		sym.Metadata = &SymbolMetadata{
			ParentName: parentPath,
		}
	}

	result.Symbols = append(result.Symbols, sym)

	// Recurse into nested structure
	if valueNode != nil {
		for i := 0; i < int(valueNode.ChildCount()); i++ {
			p.extractSymbols(ctx, valueNode.Child(i), content, filePath, result, fullPath, depth+1, docIndex)
		}
	}
}

// extractScalarValue extracts the string value from a scalar node.
func (p *YAMLParser) extractScalarValue(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}

	nodeType := node.Type()

	switch nodeType {
	case yamlNodePlainScalar:
		// Get the inner scalar
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			switch child.Type() {
			case yamlNodeStringScalar, yamlNodeIntegerScalar, yamlNodeFloatScalar, yamlNodeBooleanScalar, yamlNodeNullScalar:
				return string(content[child.StartByte():child.EndByte()])
			}
		}
	case yamlNodeDoubleQuoteScalar, yamlNodeSingleQuoteScalar:
		// Return the full quoted string
		text := string(content[node.StartByte():node.EndByte()])
		// Remove quotes
		if len(text) >= 2 {
			return text[1 : len(text)-1]
		}
		return text
	case yamlNodeStringScalar, yamlNodeIntegerScalar, yamlNodeFloatScalar, yamlNodeBooleanScalar, yamlNodeNullScalar:
		return string(content[node.StartByte():node.EndByte()])
	case yamlNodeFlowNode, yamlNodeBlockNode:
		// Recurse into wrapper nodes
		for i := 0; i < int(node.ChildCount()); i++ {
			if v := p.extractScalarValue(node.Child(i), content); v != "" {
				return v
			}
		}
	}

	return ""
}

// hasNestedStructure checks if a node contains a nested mapping or sequence.
func (p *YAMLParser) hasNestedStructure(node *sitter.Node) bool {
	if node == nil {
		return false
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		childType := child.Type()
		if childType == yamlNodeBlockMapping || childType == yamlNodeFlowMapping ||
			childType == yamlNodeBlockSequence || childType == yamlNodeFlowSequence {
			return true
		}
		if p.hasNestedStructure(child) {
			return true
		}
	}
	return false
}

// extractAnchor extracts an anchor definition.
func (p *YAMLParser) extractAnchor(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	anchorName := ""

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == yamlNodeAnchorName {
			anchorName = string(content[child.StartByte():child.EndByte()])
			break
		}
	}

	if anchorName == "" {
		return
	}

	sym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, "&"+anchorName),
		Name:          anchorName,
		Kind:          SymbolKindAnchor,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     "&" + anchorName,
		Language:      "yaml",
		ParsedAtMilli: time.Now().UnixMilli(),
		Exported:      true,
	}
	result.Symbols = append(result.Symbols, sym)
}

// extractValueType determines the YAML value type from a node.
func (p *YAMLParser) extractValueType(node *sitter.Node) string {
	if node == nil {
		return ""
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case yamlNodeIntegerScalar:
			return "integer"
		case yamlNodeFloatScalar:
			return "float"
		case yamlNodeBooleanScalar:
			return "boolean"
		case yamlNodeNullScalar:
			return "null"
		case yamlNodeStringScalar, yamlNodeDoubleQuoteScalar, yamlNodeSingleQuoteScalar:
			return "string"
		case yamlNodeBlockSequence, yamlNodeFlowSequence:
			return "array"
		case yamlNodeBlockMapping, yamlNodeFlowMapping:
			return "object"
		case yamlNodePlainScalar, yamlNodeFlowNode, yamlNodeBlockNode:
			if t := p.extractValueType(child); t != "" {
				return t
			}
		}
	}
	return ""
}

// countKeys returns the number of Key symbols extracted.
func (p *YAMLParser) countKeys(result *ParseResult) int {
	count := 0
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindKey {
			count++
		}
	}
	return count
}

// getTopLevelKeys returns only the top-level key names.
func (p *YAMLParser) getTopLevelKeys(result *ParseResult) []string {
	var keys []string
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindKey && (sym.Metadata == nil || sym.Metadata.ParentName == "") {
			keys = append(keys, sym.Name)
		}
	}
	return keys
}

// Helper to check if a key name contains dots (nested path).
func hasNestedPath(name string) bool {
	return strings.Contains(name, ".")
}
