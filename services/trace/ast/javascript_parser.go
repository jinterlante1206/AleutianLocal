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
	"github.com/smacker/go-tree-sitter/javascript"
)

// JavaScriptParser extracts symbols from JavaScript source code.
//
// Description:
//
//	JavaScriptParser uses tree-sitter to parse JavaScript source files and extract
//	structured symbol information. It supports all modern JavaScript features including
//	ES6+ modules, classes, async/await, generators, and private fields.
//
// Thread Safety:
//
//	JavaScriptParser is safe for concurrent use. Multiple goroutines can call Parse
//	simultaneously. Each Parse call creates its own tree-sitter parser instance.
//
// Example:
//
//	parser := NewJavaScriptParser()
//	result, err := parser.Parse(ctx, content, "app.js")
//	if err != nil {
//	    return fmt.Errorf("parse: %w", err)
//	}
//	for _, sym := range result.Symbols {
//	    fmt.Printf("%s: %s\n", sym.Kind, sym.Name)
//	}
type JavaScriptParser struct {
	options JavaScriptParserOptions
}

// JavaScriptParserOptions configures JavaScriptParser behavior.
type JavaScriptParserOptions struct {
	// MaxFileSize is the maximum file size in bytes to parse.
	// Files larger than this return ErrFileTooLarge.
	// Default: 10MB
	MaxFileSize int

	// IncludePrivate determines whether to include non-exported symbols.
	// Default: true
	IncludePrivate bool

	// ExtractBodies determines whether to include function body text.
	// Default: false (bodies are expensive and often not needed)
	ExtractBodies bool
}

// DefaultJavaScriptParserOptions returns the default options.
func DefaultJavaScriptParserOptions() JavaScriptParserOptions {
	return JavaScriptParserOptions{
		MaxFileSize:    10 * 1024 * 1024, // 10MB
		IncludePrivate: true,
		ExtractBodies:  false,
	}
}

// JavaScriptParserOption is a functional option for configuring JavaScriptParser.
type JavaScriptParserOption func(*JavaScriptParserOptions)

// WithJSMaxFileSize sets the maximum file size for parsing.
func WithJSMaxFileSize(size int) JavaScriptParserOption {
	return func(o *JavaScriptParserOptions) {
		o.MaxFileSize = size
	}
}

// WithJSIncludePrivate sets whether to include non-exported symbols.
func WithJSIncludePrivate(include bool) JavaScriptParserOption {
	return func(o *JavaScriptParserOptions) {
		o.IncludePrivate = include
	}
}

// WithJSExtractBodies sets whether to include function bodies.
func WithJSExtractBodies(extract bool) JavaScriptParserOption {
	return func(o *JavaScriptParserOptions) {
		o.ExtractBodies = extract
	}
}

// NewJavaScriptParser creates a new JavaScriptParser with the given options.
//
// Description:
//
//	Creates a parser configured for JavaScript source files. The parser can be
//	reused for multiple files and is safe for concurrent use.
//
// Example:
//
//	// Default options
//	parser := NewJavaScriptParser()
//
//	// With custom options
//	parser := NewJavaScriptParser(
//	    WithJSMaxFileSize(5 * 1024 * 1024),
//	    WithJSIncludePrivate(false),
//	)
func NewJavaScriptParser(opts ...JavaScriptParserOption) *JavaScriptParser {
	options := DefaultJavaScriptParserOptions()
	for _, opt := range opts {
		opt(&options)
	}
	return &JavaScriptParser{options: options}
}

// Language returns the language name for this parser.
func (p *JavaScriptParser) Language() string {
	return "javascript"
}

// Extensions returns the file extensions this parser handles.
func (p *JavaScriptParser) Extensions() []string {
	return []string{".js", ".mjs", ".cjs", ".jsx"}
}

// Parse extracts symbols from JavaScript source code.
//
// Description:
//
//	Parses the provided JavaScript content using tree-sitter and extracts all
//	symbols including functions, classes, methods, fields, variables, and imports.
//
// Inputs:
//
//	ctx      - Context for cancellation. Checked before and after parsing.
//	content  - Raw JavaScript source bytes. Must be valid UTF-8.
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
func (p *JavaScriptParser) Parse(ctx context.Context, content []byte, filePath string) (*ParseResult, error) {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("javascript parse canceled before start: %w", err)
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
		Language:      "javascript",
		Hash:          hashStr,
		ParsedAtMilli: time.Now().UnixMilli(),
		Symbols:       make([]*Symbol, 0),
		Imports:       make([]Import, 0),
		Errors:        make([]string, 0),
	}

	// Parse with tree-sitter
	parser := sitter.NewParser()
	parser.SetLanguage(javascript.GetLanguage())

	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter parse failed: %w", err)
	}
	defer tree.Close()

	// Check context after parsing
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("javascript parse canceled after tree-sitter: %w", err)
	}

	// Extract symbols from AST
	rootNode := tree.RootNode()
	p.extractSymbols(rootNode, content, filePath, result, false)

	// Validate result
	if err := result.Validate(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("validation error: %v", err))
	}

	return result, nil
}

// extractSymbols recursively extracts symbols from the AST.
func (p *JavaScriptParser) extractSymbols(node *sitter.Node, content []byte, filePath string, result *ParseResult, exported bool) {
	if node == nil {
		return
	}

	nodeType := node.Type()

	switch nodeType {
	case jsNodeProgram:
		// Process all children
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(node.Child(i), content, filePath, result, false)
		}

	case jsNodeImportStatement:
		p.extractImport(node, content, filePath, result)

	case jsNodeExportStatement:
		p.extractExport(node, content, filePath, result)

	case jsNodeFunctionDeclaration, jsNodeGeneratorFunctionDecl:
		sym := p.extractFunction(node, content, filePath, exported)
		if sym != nil {
			if p.options.IncludePrivate || sym.Exported {
				result.Symbols = append(result.Symbols, sym)
			}
		}

	case jsNodeClassDeclaration:
		sym := p.extractClass(node, content, filePath, exported)
		if sym != nil {
			if p.options.IncludePrivate || sym.Exported {
				result.Symbols = append(result.Symbols, sym)
			}
		}

	case jsNodeLexicalDeclaration, jsNodeVariableDeclaration:
		// Check for CommonJS require() first
		p.extractCommonJSImport(node, content, filePath, result)
		// Then extract variables
		syms := p.extractVariables(node, content, filePath, exported)
		for _, sym := range syms {
			if p.options.IncludePrivate || sym.Exported {
				result.Symbols = append(result.Symbols, sym)
			}
		}

	default:
		// No special handling needed for other node types
		if false {
		}
	}
}

// extractImport extracts an import statement.
func (p *JavaScriptParser) extractImport(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	imp := &Import{
		IsModule: true,
		Location: Location{
			FilePath:  filePath,
			StartLine: int(node.StartPoint().Row) + 1,
			EndLine:   int(node.EndPoint().Row) + 1,
			StartCol:  int(node.StartPoint().Column),
			EndCol:    int(node.EndPoint().Column),
		},
	}

	// Find the module path (string node)
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == jsNodeString {
			imp.Path = p.extractStringContent(child, content)
		} else if child.Type() == jsNodeImportClause {
			p.extractImportClause(child, content, imp)
		}
	}

	if imp.Path != "" {
		result.Imports = append(result.Imports, *imp)

		// Also add as symbol
		sym := &Symbol{
			ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, imp.Path),
			Name:          imp.Path,
			Kind:          SymbolKindImport,
			FilePath:      filePath,
			StartLine:     int(node.StartPoint().Row) + 1,
			EndLine:       int(node.EndPoint().Row) + 1,
			StartCol:      int(node.StartPoint().Column),
			EndCol:        int(node.EndPoint().Column),
			Language:      "javascript",
			ParsedAtMilli: time.Now().UnixMilli(),
		}
		result.Symbols = append(result.Symbols, sym)
	}
}

// extractImportClause extracts the import clause details.
func (p *JavaScriptParser) extractImportClause(node *sitter.Node, content []byte, imp *Import) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case jsNodeIdentifier:
			// Default import
			imp.Alias = string(content[child.StartByte():child.EndByte()])
			imp.IsDefault = true
		case jsNodeNamespaceImport:
			// import * as foo
			for j := 0; j < int(child.ChildCount()); j++ {
				gc := child.Child(j)
				if gc.Type() == jsNodeIdentifier {
					imp.Alias = string(content[gc.StartByte():gc.EndByte()])
				}
			}
			imp.IsNamespace = true
		case jsNodeNamedImports:
			// import { foo, bar }
			for j := 0; j < int(child.ChildCount()); j++ {
				gc := child.Child(j)
				if gc.Type() == jsNodeImportSpecifier {
					name := p.extractImportSpecifierName(gc, content)
					if name != "" {
						imp.Names = append(imp.Names, name)
					}
				}
			}
		}
	}
}

// extractImportSpecifierName extracts the name from an import specifier.
func (p *JavaScriptParser) extractImportSpecifierName(node *sitter.Node, content []byte) string {
	// import { foo } or import { foo as bar }
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == jsNodeIdentifier {
			return string(content[child.StartByte():child.EndByte()])
		}
	}
	return ""
}

// extractStringContent extracts the string content without quotes.
func (p *JavaScriptParser) extractStringContent(node *sitter.Node, content []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == jsNodeStringFragment {
			return string(content[child.StartByte():child.EndByte()])
		}
	}
	// Fallback: remove quotes manually
	text := string(content[node.StartByte():node.EndByte()])
	if len(text) >= 2 {
		return text[1 : len(text)-1]
	}
	return text
}

// extractExport extracts an export statement.
func (p *JavaScriptParser) extractExport(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	isDefault := false

	// Check for default keyword
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == jsNodeDefault {
			isDefault = true
			break
		}
	}

	// Get preceding comment for the export
	docComment := p.getPrecedingComment(node, content)

	// Process the declaration inside the export
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case jsNodeFunctionDeclaration, jsNodeGeneratorFunctionDecl:
			sym := p.extractFunction(child, content, filePath, true)
			if sym != nil {
				sym.Exported = true
				if isDefault {
					sym.Metadata = ensureMetadata(sym.Metadata)
					// Mark as default export in metadata or name
				}
				if docComment != "" && sym.DocComment == "" {
					sym.DocComment = docComment
				}
				if p.options.IncludePrivate || sym.Exported {
					result.Symbols = append(result.Symbols, sym)
				}
			}

		case jsNodeClassDeclaration:
			sym := p.extractClass(child, content, filePath, true)
			if sym != nil {
				sym.Exported = true
				if docComment != "" && sym.DocComment == "" {
					sym.DocComment = docComment
				}
				if p.options.IncludePrivate || sym.Exported {
					result.Symbols = append(result.Symbols, sym)
				}
			}

		case jsNodeLexicalDeclaration, jsNodeVariableDeclaration:
			syms := p.extractVariables(child, content, filePath, true)
			for _, sym := range syms {
				sym.Exported = true
				if docComment != "" && sym.DocComment == "" {
					sym.DocComment = docComment
				}
				if p.options.IncludePrivate || sym.Exported {
					result.Symbols = append(result.Symbols, sym)
				}
			}

		case jsNodeIdentifier:
			// export default identifier
			if isDefault {
				name := string(content[child.StartByte():child.EndByte()])
				sym := &Symbol{
					ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, name),
					Name:          name,
					Kind:          SymbolKindVariable,
					FilePath:      filePath,
					StartLine:     int(node.StartPoint().Row) + 1,
					EndLine:       int(node.EndPoint().Row) + 1,
					StartCol:      int(node.StartPoint().Column),
					EndCol:        int(node.EndPoint().Column),
					Exported:      true,
					Language:      "javascript",
					ParsedAtMilli: time.Now().UnixMilli(),
					DocComment:    docComment,
				}
				result.Symbols = append(result.Symbols, sym)
			}

		case jsNodeExportClause:
			// export { foo, bar }
			p.extractExportClause(child, content, filePath, result)
		}
	}
}

// extractExportClause extracts named exports from export clause.
func (p *JavaScriptParser) extractExportClause(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == jsNodeExportSpecifier {
			name := ""
			for j := 0; j < int(child.ChildCount()); j++ {
				gc := child.Child(j)
				if gc.Type() == jsNodeIdentifier {
					name = string(content[gc.StartByte():gc.EndByte()])
					break
				}
			}
			if name != "" {
				sym := &Symbol{
					ID:            GenerateID(filePath, int(child.StartPoint().Row)+1, name),
					Name:          name,
					Kind:          SymbolKindVariable,
					FilePath:      filePath,
					StartLine:     int(child.StartPoint().Row) + 1,
					EndLine:       int(child.EndPoint().Row) + 1,
					StartCol:      int(child.StartPoint().Column),
					EndCol:        int(child.EndPoint().Column),
					Exported:      true,
					Language:      "javascript",
					ParsedAtMilli: time.Now().UnixMilli(),
				}
				result.Symbols = append(result.Symbols, sym)
			}
		}
	}
}

// extractFunction extracts a function declaration.
func (p *JavaScriptParser) extractFunction(node *sitter.Node, content []byte, filePath string, exported bool) *Symbol {
	name := ""
	isAsync := false
	isGenerator := false
	var params []string
	docComment := p.getPrecedingComment(node, content)

	// Check node type for generator
	if node.Type() == jsNodeGeneratorFunctionDecl {
		isGenerator = true
	}

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case jsNodeIdentifier:
			name = string(content[child.StartByte():child.EndByte()])
		case jsNodeAsync:
			isAsync = true
		case jsNodeFormalParameters:
			params = p.extractParameters(child, content)
		case "*":
			isGenerator = true
		}
	}

	if name == "" {
		return nil
	}

	// Build signature
	signature := "function"
	if isAsync {
		signature = "async function"
	}
	if isGenerator {
		signature += "*"
	}
	signature += " " + name + "(" + strings.Join(params, ", ") + ")"

	sym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, name),
		Name:          name,
		Kind:          SymbolKindFunction,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     signature,
		DocComment:    docComment,
		Exported:      exported,
		Language:      "javascript",
		ParsedAtMilli: time.Now().UnixMilli(),
	}

	if isAsync || isGenerator {
		sym.Metadata = &SymbolMetadata{
			IsAsync:     isAsync,
			IsGenerator: isGenerator,
		}
	}

	return sym
}

// extractClass extracts a class declaration.
func (p *JavaScriptParser) extractClass(node *sitter.Node, content []byte, filePath string, exported bool) *Symbol {
	name := ""
	var extends string
	var children []*Symbol
	docComment := p.getPrecedingComment(node, content)

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case jsNodeIdentifier:
			name = string(content[child.StartByte():child.EndByte()])
		case jsNodeClassHeritage:
			extends = p.extractClassHeritage(child, content)
		case jsNodeClassBody:
			children = p.extractClassBody(child, content, filePath, name)
		}
	}

	if name == "" {
		return nil
	}

	signature := "class " + name
	if extends != "" {
		signature += " extends " + extends
	}

	sym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, name),
		Name:          name,
		Kind:          SymbolKindClass,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     signature,
		DocComment:    docComment,
		Exported:      exported,
		Language:      "javascript",
		ParsedAtMilli: time.Now().UnixMilli(),
		Children:      children,
	}

	if extends != "" {
		sym.Metadata = &SymbolMetadata{
			Extends: extends,
		}
	}

	return sym
}

// extractClassHeritage extracts the extends clause from class heritage.
func (p *JavaScriptParser) extractClassHeritage(node *sitter.Node, content []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == jsNodeIdentifier {
			return string(content[child.StartByte():child.EndByte()])
		}
	}
	return ""
}

// extractClassBody extracts members from a class body.
func (p *JavaScriptParser) extractClassBody(node *sitter.Node, content []byte, filePath string, className string) []*Symbol {
	var members []*Symbol

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case jsNodeMethodDefinition:
			mem := p.extractMethod(child, content, filePath, className)
			if mem != nil {
				members = append(members, mem)
			}
		case jsNodeFieldDefinition:
			mem := p.extractField(child, content, filePath, className)
			if mem != nil {
				members = append(members, mem)
			}
		}
	}

	return members
}

// extractMethod extracts a method definition from a class.
func (p *JavaScriptParser) extractMethod(node *sitter.Node, content []byte, filePath string, className string) *Symbol {
	name := ""
	isAsync := false
	isStatic := false
	isGenerator := false
	isPrivate := false
	var params []string
	docComment := p.getPrecedingComment(node, content)

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case jsNodePropertyIdentifier:
			name = string(content[child.StartByte():child.EndByte()])
		case jsNodePrivatePropertyIdent:
			name = string(content[child.StartByte():child.EndByte()])
			isPrivate = true
		case jsNodeAsync:
			isAsync = true
		case jsNodeStatic:
			isStatic = true
		case "*":
			isGenerator = true
		case jsNodeFormalParameters:
			params = p.extractParameters(child, content)
		}
	}

	if name == "" {
		return nil
	}

	// Build signature
	sig := ""
	if isStatic {
		sig += "static "
	}
	if isAsync {
		sig += "async "
	}
	sig += name
	if isGenerator {
		sig += "*"
	}
	sig += "(" + strings.Join(params, ", ") + ")"

	sym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, className+"."+name),
		Name:          name,
		Kind:          SymbolKindMethod,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     sig,
		DocComment:    docComment,
		Receiver:      className,
		Exported:      !isPrivate,
		Language:      "javascript",
		ParsedAtMilli: time.Now().UnixMilli(),
	}

	if isAsync || isGenerator || isStatic || isPrivate {
		sym.Metadata = &SymbolMetadata{
			IsAsync:     isAsync,
			IsGenerator: isGenerator,
			IsStatic:    isStatic,
		}
		if isPrivate {
			sym.Metadata.AccessModifier = "private"
		}
	}

	return sym
}

// extractField extracts a field definition from a class.
func (p *JavaScriptParser) extractField(node *sitter.Node, content []byte, filePath string, className string) *Symbol {
	name := ""
	isStatic := false
	isPrivate := false
	docComment := p.getPrecedingComment(node, content)

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case jsNodePropertyIdentifier:
			name = string(content[child.StartByte():child.EndByte()])
		case jsNodePrivatePropertyIdent:
			name = string(content[child.StartByte():child.EndByte()])
			isPrivate = true
		case jsNodeStatic:
			isStatic = true
		}
	}

	if name == "" {
		return nil
	}

	sig := ""
	if isStatic {
		sig += "static "
	}
	sig += name

	sym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, className+"."+name),
		Name:          name,
		Kind:          SymbolKindField,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     sig,
		DocComment:    docComment,
		Receiver:      className,
		Exported:      !isPrivate,
		Language:      "javascript",
		ParsedAtMilli: time.Now().UnixMilli(),
	}

	if isStatic || isPrivate {
		sym.Metadata = &SymbolMetadata{
			IsStatic: isStatic,
		}
		if isPrivate {
			sym.Metadata.AccessModifier = "private"
		}
	}

	return sym
}

// extractVariables extracts variable declarations.
func (p *JavaScriptParser) extractVariables(node *sitter.Node, content []byte, filePath string, exported bool) []*Symbol {
	var symbols []*Symbol
	isConst := false
	docComment := p.getPrecedingComment(node, content)

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case jsNodeConst:
			isConst = true
		case jsNodeVariableDeclarator:
			sym := p.extractVariableDeclarator(child, content, filePath, exported, isConst, docComment)
			if sym != nil {
				symbols = append(symbols, sym)
			}
		}
	}

	return symbols
}

// extractVariableDeclarator extracts a single variable declarator.
func (p *JavaScriptParser) extractVariableDeclarator(node *sitter.Node, content []byte, filePath string, exported bool, isConst bool, docComment string) *Symbol {
	name := ""
	isArrowFunction := false
	isAsync := false
	var params []string

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case jsNodeIdentifier:
			if name == "" { // First identifier is the variable name
				name = string(content[child.StartByte():child.EndByte()])
			}
		case jsNodeArrowFunction:
			isArrowFunction = true
			// Extract arrow function details
			for j := 0; j < int(child.ChildCount()); j++ {
				gc := child.Child(j)
				switch gc.Type() {
				case jsNodeAsync:
					isAsync = true
				case jsNodeFormalParameters:
					params = p.extractParameters(gc, content)
				case jsNodeIdentifier:
					// Single parameter without parens
					params = []string{string(content[gc.StartByte():gc.EndByte()])}
				}
			}
		}
	}

	if name == "" {
		return nil
	}

	kind := SymbolKindVariable
	if isConst {
		kind = SymbolKindConstant
	}
	if isArrowFunction {
		kind = SymbolKindFunction
	}

	sig := ""
	if isConst {
		sig = "const "
	} else {
		sig = "let "
	}
	sig += name
	if isArrowFunction {
		if isAsync {
			sig = "const " + name + " = async (" + strings.Join(params, ", ") + ") => {}"
		} else {
			sig = "const " + name + " = (" + strings.Join(params, ", ") + ") => {}"
		}
	}

	sym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, name),
		Name:          name,
		Kind:          kind,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     sig,
		DocComment:    docComment,
		Exported:      exported,
		Language:      "javascript",
		ParsedAtMilli: time.Now().UnixMilli(),
	}

	if isArrowFunction && isAsync {
		sym.Metadata = &SymbolMetadata{
			IsAsync: true,
		}
	}

	return sym
}

// extractParameters extracts parameter names from formal_parameters.
func (p *JavaScriptParser) extractParameters(node *sitter.Node, content []byte) []string {
	var params []string

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case jsNodeIdentifier:
			params = append(params, string(content[child.StartByte():child.EndByte()]))
		case jsNodeRestPattern:
			// ...args
			for j := 0; j < int(child.ChildCount()); j++ {
				gc := child.Child(j)
				if gc.Type() == jsNodeIdentifier {
					params = append(params, "..."+string(content[gc.StartByte():gc.EndByte()]))
				}
			}
		case jsNodeAssignmentExpression:
			// param = defaultValue
			for j := 0; j < int(child.ChildCount()); j++ {
				gc := child.Child(j)
				if gc.Type() == jsNodeIdentifier {
					params = append(params, string(content[gc.StartByte():gc.EndByte()]))
					break
				}
			}
		}
	}

	return params
}

// extractCommonJSImport extracts CommonJS require() imports.
func (p *JavaScriptParser) extractCommonJSImport(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	// Look for: const foo = require('bar')
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == jsNodeVariableDeclarator {
			varName := ""
			requirePath := ""

			for j := 0; j < int(child.ChildCount()); j++ {
				gc := child.Child(j)
				switch gc.Type() {
				case jsNodeIdentifier:
					varName = string(content[gc.StartByte():gc.EndByte()])
				case jsNodeCallExpression:
					// Check if it's require()
					for k := 0; k < int(gc.ChildCount()); k++ {
						ggc := gc.Child(k)
						if ggc.Type() == jsNodeIdentifier && string(content[ggc.StartByte():ggc.EndByte()]) == "require" {
							// Found require, get the argument
							for l := 0; l < int(gc.ChildCount()); l++ {
								arg := gc.Child(l)
								if arg.Type() == jsNodeArguments {
									for m := 0; m < int(arg.ChildCount()); m++ {
										argChild := arg.Child(m)
										if argChild.Type() == jsNodeString {
											requirePath = p.extractStringContent(argChild, content)
										}
									}
								}
							}
						}
					}
				}
			}

			if requirePath != "" && varName != "" {
				imp := &Import{
					Path:       requirePath,
					Alias:      varName,
					IsCommonJS: true,
					Location: Location{
						FilePath:  filePath,
						StartLine: int(node.StartPoint().Row) + 1,
						EndLine:   int(node.EndPoint().Row) + 1,
						StartCol:  int(node.StartPoint().Column),
						EndCol:    int(node.EndPoint().Column),
					},
				}
				result.Imports = append(result.Imports, *imp)

				// Also add as symbol
				sym := &Symbol{
					ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, requirePath),
					Name:          requirePath,
					Kind:          SymbolKindImport,
					FilePath:      filePath,
					StartLine:     int(node.StartPoint().Row) + 1,
					EndLine:       int(node.EndPoint().Row) + 1,
					StartCol:      int(node.StartPoint().Column),
					EndCol:        int(node.EndPoint().Column),
					Language:      "javascript",
					ParsedAtMilli: time.Now().UnixMilli(),
				}
				result.Symbols = append(result.Symbols, sym)
			}
		}
	}
}

// getPrecedingComment extracts JSDoc or comment before a node.
func (p *JavaScriptParser) getPrecedingComment(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}

	// Look for comment node immediately before this one
	prev := node.PrevSibling()
	if prev != nil && prev.Type() == jsNodeComment {
		comment := string(content[prev.StartByte():prev.EndByte()])
		// Check if it's a JSDoc comment
		if strings.HasPrefix(comment, "/**") {
			return comment
		}
	}

	// If this node is inside an export_statement, check parent's previous sibling
	parent := node.Parent()
	if parent != nil && parent.Type() == jsNodeExportStatement {
		parentPrev := parent.PrevSibling()
		if parentPrev != nil && parentPrev.Type() == jsNodeComment {
			comment := string(content[parentPrev.StartByte():parentPrev.EndByte()])
			if strings.HasPrefix(comment, "/**") {
				return comment
			}
		}
	}

	return ""
}

// ensureMetadata ensures the metadata object exists.
func ensureMetadata(m *SymbolMetadata) *SymbolMetadata {
	if m == nil {
		return &SymbolMetadata{}
	}
	return m
}
