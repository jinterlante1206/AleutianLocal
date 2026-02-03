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
	"github.com/smacker/go-tree-sitter/bash"
)

// BashParser extracts symbols from Bash script source code.
//
// Description:
//
//	BashParser uses tree-sitter to parse Bash scripts and extract
//	structured symbol information including functions, variables,
//	exported variables, constants (readonly), and aliases.
//
// Thread Safety:
//
//	BashParser is safe for concurrent use. Multiple goroutines can call Parse
//	simultaneously. Each Parse call creates its own tree-sitter parser instance.
//
// Example:
//
//	parser := NewBashParser()
//	result, err := parser.Parse(ctx, content, "script.sh")
//	if err != nil {
//	    return fmt.Errorf("parse: %w", err)
//	}
//	for _, sym := range result.Symbols {
//	    fmt.Printf("%s: %s\n", sym.Kind, sym.Name)
//	}
type BashParser struct {
	options BashParserOptions
}

// BashParserOptions configures BashParser behavior.
type BashParserOptions struct {
	// MaxFileSize is the maximum file size in bytes to parse.
	// Files larger than this return ErrFileTooLarge.
	// Default: 10MB
	MaxFileSize int

	// ExtractAliases determines whether to extract alias definitions.
	// Default: true
	ExtractAliases bool
}

// DefaultBashParserOptions returns the default options.
func DefaultBashParserOptions() BashParserOptions {
	return BashParserOptions{
		MaxFileSize:    10 * 1024 * 1024, // 10MB
		ExtractAliases: true,
	}
}

// BashParserOption is a functional option for configuring BashParser.
type BashParserOption func(*BashParserOptions)

// WithBashMaxFileSize sets the maximum file size for parsing.
func WithBashMaxFileSize(size int) BashParserOption {
	return func(o *BashParserOptions) {
		o.MaxFileSize = size
	}
}

// WithBashExtractAliases sets whether to extract alias definitions.
func WithBashExtractAliases(extract bool) BashParserOption {
	return func(o *BashParserOptions) {
		o.ExtractAliases = extract
	}
}

// NewBashParser creates a new BashParser with the given options.
//
// Description:
//
//	Creates a parser configured for Bash script files. The parser can be
//	reused for multiple files and is safe for concurrent use.
//
// Example:
//
//	// Default options
//	parser := NewBashParser()
//
//	// With custom options
//	parser := NewBashParser(
//	    WithBashMaxFileSize(5 * 1024 * 1024),
//	    WithBashExtractAliases(false),
//	)
func NewBashParser(opts ...BashParserOption) *BashParser {
	options := DefaultBashParserOptions()
	for _, opt := range opts {
		opt(&options)
	}
	return &BashParser{
		options: options,
	}
}

// Language returns the language name for this parser.
func (p *BashParser) Language() string {
	return "bash"
}

// Extensions returns the file extensions this parser handles.
func (p *BashParser) Extensions() []string {
	return []string{".sh", ".bash", ".zsh"}
}

// Parse extracts symbols from Bash script source code.
//
// Description:
//
//	Parses the provided Bash content using tree-sitter and extracts all
//	symbols including functions, variables, constants, and aliases.
//
// Inputs:
//
//	ctx      - Context for cancellation. Checked before/after parsing.
//	content  - Raw Bash source bytes. Must be valid UTF-8.
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
func (p *BashParser) Parse(ctx context.Context, content []byte, filePath string) (*ParseResult, error) {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("bash parse canceled before start: %w", err)
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

	// Capture timestamp once for consistent timestamps across all symbols
	parsedAt := time.Now().UnixMilli()

	// Create result
	result := &ParseResult{
		FilePath:      filePath,
		Language:      "bash",
		Hash:          hashStr,
		ParsedAtMilli: parsedAt,
		Symbols:       make([]*Symbol, 0),
		Imports:       make([]Import, 0),
		Errors:        make([]string, 0),
	}

	// Parse with tree-sitter
	parser := sitter.NewParser()
	parser.SetLanguage(bash.GetLanguage())

	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter parse failed: %w", err)
	}
	defer tree.Close()

	// Check context after parsing
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("bash parse canceled after tree-sitter: %w", err)
	}

	// Track preceding comments for doc extraction
	var lastComment string
	var lastCommentLine int

	// Extract symbols from AST
	rootNode := tree.RootNode()
	p.extractSymbols(ctx, rootNode, content, filePath, result, &lastComment, &lastCommentLine, parsedAt)

	// Validate result
	if err := result.Validate(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("validation error: %v", err))
	}

	return result, nil
}

// extractSymbols recursively extracts symbols from the AST.
func (p *BashParser) extractSymbols(ctx context.Context, node *sitter.Node, content []byte, filePath string, result *ParseResult, lastComment *string, lastCommentLine *int, parsedAt int64) {
	if node == nil {
		return
	}

	// Check context periodically
	if ctx.Err() != nil {
		return
	}

	nodeType := node.Type()

	switch nodeType {
	case bashNodeProgram:
		// Process all children
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(ctx, node.Child(i), content, filePath, result, lastComment, lastCommentLine, parsedAt)
		}

	case bashNodeComment:
		// Track comments for doc extraction
		text := string(content[node.StartByte():node.EndByte()])
		// Skip shebang
		if !strings.HasPrefix(text, "#!") {
			// Remove leading # and trim
			text = strings.TrimPrefix(text, "#")
			text = strings.TrimSpace(text)
			*lastComment = text
			*lastCommentLine = int(node.EndPoint().Row) + 1
		}

	case bashNodeFunctionDefinition:
		p.extractFunction(node, content, filePath, result, *lastComment, *lastCommentLine, parsedAt)
		*lastComment = ""

	case bashNodeVariableAssignment:
		p.extractVariable(node, content, filePath, result, false, false, *lastComment, *lastCommentLine, parsedAt)
		*lastComment = ""

	case bashNodeDeclarationCommand:
		p.extractDeclaration(node, content, filePath, result, *lastComment, *lastCommentLine, parsedAt)
		*lastComment = ""

	case bashNodeCommand:
		// Check for alias command
		if p.options.ExtractAliases {
			p.extractAlias(node, content, filePath, result, parsedAt)
		}
		// Check for source/. command (imports)
		p.extractSource(node, content, filePath, result)

	default:
		// Recurse into children for other node types
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(ctx, node.Child(i), content, filePath, result, lastComment, lastCommentLine, parsedAt)
		}
	}
}

// extractFunction extracts a function definition.
func (p *BashParser) extractFunction(node *sitter.Node, content []byte, filePath string, result *ParseResult, lastComment string, lastCommentLine int, parsedAt int64) {
	funcName := ""

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == bashNodeWord {
			funcName = string(content[child.StartByte():child.EndByte()])
			break
		}
	}

	if funcName == "" {
		return
	}

	// Check if comment is immediately preceding (within 2 lines to allow blank lines)
	docComment := ""
	lineGap := int(node.StartPoint().Row) - lastCommentLine
	if lastComment != "" && lineGap >= 0 && lineGap <= 1 {
		docComment = lastComment
	}

	sym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, funcName),
		Name:          funcName,
		Kind:          SymbolKindFunction,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     funcName + "()",
		DocComment:    docComment,
		Language:      "bash",
		ParsedAtMilli: parsedAt,
		Exported:      true, // Bash functions are globally accessible
	}
	result.Symbols = append(result.Symbols, sym)
}

// extractVariable extracts a variable assignment.
func (p *BashParser) extractVariable(node *sitter.Node, content []byte, filePath string, result *ParseResult, isExported, isReadonly bool, lastComment string, lastCommentLine int, parsedAt int64) {
	varName := ""
	varValue := ""

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case bashNodeVariableName:
			varName = string(content[child.StartByte():child.EndByte()])
		case bashNodeString, bashNodeRawString, bashNodeNumber, bashNodeConcatenation, bashNodeWord:
			varValue = string(content[child.StartByte():child.EndByte()])
		}
	}

	if varName == "" {
		return
	}

	// Determine symbol kind
	kind := SymbolKindVariable
	if isReadonly {
		kind = SymbolKindConstant
	}

	// Build signature
	signature := varName
	if varValue != "" {
		// Truncate long values
		displayValue := varValue
		if len(displayValue) > 50 {
			displayValue = displayValue[:50] + "..."
		}
		signature += "=" + displayValue
	}

	// Check if comment is immediately preceding (within 2 lines to allow blank lines)
	docComment := ""
	lineGap := int(node.StartPoint().Row) - lastCommentLine
	if lastComment != "" && lineGap >= 0 && lineGap <= 1 {
		docComment = lastComment
	}

	sym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, varName),
		Name:          varName,
		Kind:          kind,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     signature,
		DocComment:    docComment,
		Language:      "bash",
		ParsedAtMilli: parsedAt,
		Exported:      isExported,
	}
	result.Symbols = append(result.Symbols, sym)
}

// extractDeclaration extracts declaration commands (export, readonly, local, declare).
func (p *BashParser) extractDeclaration(node *sitter.Node, content []byte, filePath string, result *ParseResult, lastComment string, lastCommentLine int, parsedAt int64) {
	isExported := false
	isReadonly := false
	isLocal := false

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case bashNodeExport:
			isExported = true
		case bashNodeReadonly:
			isReadonly = true
		case bashNodeLocal:
			isLocal = true
		case bashNodeVariableAssignment:
			// Don't extract local variables (they're function-scoped)
			if !isLocal {
				p.extractVariable(child, content, filePath, result, isExported, isReadonly, lastComment, lastCommentLine, parsedAt)
			}
		}
	}
}

// extractAlias extracts alias definitions from alias commands.
func (p *BashParser) extractAlias(node *sitter.Node, content []byte, filePath string, result *ParseResult, parsedAt int64) {
	// Check if this is an alias command
	isAlias := false
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == bashNodeCommandName {
			for j := 0; j < int(child.ChildCount()); j++ {
				gc := child.Child(j)
				if gc.Type() == bashNodeWord {
					if string(content[gc.StartByte():gc.EndByte()]) == "alias" {
						isAlias = true
					}
				}
			}
		}
	}

	if !isAlias {
		return
	}

	// Extract alias definition
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == bashNodeConcatenation || child.Type() == bashNodeWord {
			text := string(content[child.StartByte():child.EndByte()])
			// Parse alias=value or alias='value'
			if strings.Contains(text, "=") {
				parts := strings.SplitN(text, "=", 2)
				aliasName := parts[0]
				aliasValue := ""
				if len(parts) > 1 {
					aliasValue = parts[1]
				}

				sym := &Symbol{
					ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, aliasName),
					Name:          aliasName,
					Kind:          SymbolKindAlias,
					FilePath:      filePath,
					StartLine:     int(node.StartPoint().Row) + 1,
					EndLine:       int(node.EndPoint().Row) + 1,
					StartCol:      int(node.StartPoint().Column),
					EndCol:        int(node.EndPoint().Column),
					Signature:     fmt.Sprintf("alias %s=%s", aliasName, aliasValue),
					Language:      "bash",
					ParsedAtMilli: parsedAt,
					Exported:      false,
				}
				result.Symbols = append(result.Symbols, sym)
			}
		}
	}
}

// extractSource extracts source/. imports.
func (p *BashParser) extractSource(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	// Check if this is a source or . command
	isSource := false
	sourcePath := ""

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == bashNodeCommandName {
			for j := 0; j < int(child.ChildCount()); j++ {
				gc := child.Child(j)
				if gc.Type() == bashNodeWord {
					cmd := string(content[gc.StartByte():gc.EndByte()])
					if cmd == "source" || cmd == "." {
						isSource = true
					}
				}
			}
		} else if isSource && (child.Type() == bashNodeWord || child.Type() == bashNodeString) {
			sourcePath = string(content[child.StartByte():child.EndByte()])
			// Remove quotes if present
			sourcePath = strings.Trim(sourcePath, "\"'")
		}
	}

	if !isSource || sourcePath == "" {
		return
	}

	// SECURITY WARNING: The extracted sourcePath is NOT sanitized and may contain
	// path traversal sequences (e.g., '../') or be an absolute path. Downstream
	// consumers MUST validate and sanitize this path before using it to access
	// the filesystem to prevent path traversal vulnerabilities.
	imp := Import{
		Path:     sourcePath,
		IsScript: true,
		Location: Location{
			FilePath:  filePath,
			StartLine: int(node.StartPoint().Row) + 1,
			EndLine:   int(node.EndPoint().Row) + 1,
			StartCol:  int(node.StartPoint().Column),
			EndCol:    int(node.EndPoint().Column),
		},
	}
	result.Imports = append(result.Imports, imp)
}
