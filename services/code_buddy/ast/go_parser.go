package ast

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
)

// File size constants for security validation.
const (
	// DefaultMaxFileSize is the maximum file size the parser will accept (10MB).
	DefaultMaxFileSize = 10 * 1024 * 1024

	// WarnFileSize is the threshold at which a warning is logged (1MB).
	WarnFileSize = 1 * 1024 * 1024
)

// ErrFileTooLarge is returned when input content exceeds the maximum file size.
var ErrFileTooLarge = errors.New("file exceeds maximum size limit")

// GoParserOption configures a GoParser instance.
type GoParserOption func(*GoParser)

// WithMaxFileSize sets the maximum file size the parser will accept.
//
// Parameters:
//   - bytes: Maximum file size in bytes. Must be positive.
//
// Example:
//
//	parser := NewGoParser(WithMaxFileSize(5 * 1024 * 1024)) // 5MB limit
func WithMaxFileSize(bytes int64) GoParserOption {
	return func(p *GoParser) {
		if bytes > 0 {
			p.maxFileSize = bytes
		}
	}
}

// WithParseOptions applies the given ParseOptions to the parser.
//
// Parameters:
//   - opts: ParseOptions to apply.
//
// Example:
//
//	parser := NewGoParser(WithParseOptions(ParseOptions{IncludePrivate: false}))
func WithParseOptions(opts ParseOptions) GoParserOption {
	return func(p *GoParser) {
		p.parseOptions = opts
	}
}

// GoParser implements the Parser interface for Go source code.
//
// Description:
//
//	GoParser uses tree-sitter to parse Go source files and extract symbols.
//	It supports concurrent use from multiple goroutines - each Parse call
//	creates its own tree-sitter parser instance internally.
//
// Thread Safety:
//
//	GoParser instances are safe for concurrent use. Multiple goroutines
//	may call Parse simultaneously on the same GoParser instance.
//
// Example:
//
//	parser := NewGoParser()
//	result, err := parser.Parse(ctx, []byte("package main\n\nfunc main() {}"), "main.go")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for _, sym := range result.Symbols {
//	    fmt.Printf("%s: %s\n", sym.Kind, sym.Name)
//	}
type GoParser struct {
	maxFileSize  int64
	parseOptions ParseOptions
}

// NewGoParser creates a new GoParser with the given options.
//
// Description:
//
//	Creates a GoParser configured with sensible defaults. Options can be
//	provided to customize behavior such as maximum file size.
//
// Inputs:
//   - opts: Optional configuration functions (WithMaxFileSize, WithParseOptions)
//
// Outputs:
//   - *GoParser: Configured parser instance, never nil
//
// Example:
//
//	// Default configuration
//	parser := NewGoParser()
//
//	// Custom max file size
//	parser := NewGoParser(WithMaxFileSize(5 * 1024 * 1024))
//
// Thread Safety:
//
//	The returned GoParser is safe for concurrent use.
func NewGoParser(opts ...GoParserOption) *GoParser {
	p := &GoParser{
		maxFileSize:  DefaultMaxFileSize,
		parseOptions: DefaultParseOptions(),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Parse extracts symbols from Go source code.
//
// Description:
//
//	Parse uses tree-sitter to parse the provided Go source code and extract
//	all symbols (functions, methods, types, interfaces, etc.) into a ParseResult.
//	The parser is error-tolerant and will return partial results for syntactically
//	invalid code.
//
// Inputs:
//   - ctx: Context for cancellation. Checked before and after parsing.
//     Note: Tree-sitter parsing itself cannot be interrupted mid-parse.
//   - content: Raw Go source code bytes. Must be valid UTF-8.
//   - filePath: Path to the file (for ID generation and error reporting).
//     Should be relative to project root using forward slashes.
//
// Outputs:
//   - *ParseResult: Extracted symbols and metadata. Never nil on success.
//     May contain partial results with errors for syntactically invalid code.
//   - error: Non-nil for complete failures:
//   - ErrFileTooLarge: Content exceeds maxFileSize
//   - ErrInvalidContent: Content is not valid UTF-8
//   - Context errors: Context was canceled or timed out
//
// Example:
//
//	result, err := parser.Parse(ctx, []byte("package main\n\nfunc Hello() {}"), "main.go")
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("Found %d symbols\n", len(result.Symbols))
//
// Limitations:
//   - Tree-sitter parsing is synchronous and cannot be interrupted mid-parse
//   - Very large files may take significant time to parse
//   - Some edge cases in Go syntax may not be fully handled
//
// Assumptions:
//   - Content is valid UTF-8 (validated internally)
//   - FilePath uses forward slashes as path separator
//   - FilePath does not contain path traversal sequences
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (p *GoParser) Parse(ctx context.Context, content []byte, filePath string) (*ParseResult, error) {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("parse canceled before start: %w", err)
	}

	// Validate file size
	if int64(len(content)) > p.maxFileSize {
		return nil, fmt.Errorf("%w: size %d exceeds limit %d", ErrFileTooLarge, len(content), p.maxFileSize)
	}

	// Log warning for large files
	if len(content) > WarnFileSize {
		slog.Warn("parsing large file",
			slog.String("file", filePath),
			slog.Int("size_bytes", len(content)))
	}

	// Validate UTF-8
	if !utf8.Valid(content) {
		return nil, fmt.Errorf("%w: content is not valid UTF-8", ErrInvalidContent)
	}

	// Compute hash before parsing (captures input)
	hash := sha256.Sum256(content)
	hashStr := hex.EncodeToString(hash[:])

	// Create tree-sitter parser (new instance per call for thread safety)
	parser := sitter.NewParser()
	parser.SetLanguage(golang.GetLanguage())

	// Parse the content
	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter parse failed: %w", err)
	}
	defer tree.Close()

	// Check context after parsing
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("parse canceled after tree-sitter: %w", err)
	}

	// Build result
	result := &ParseResult{
		FilePath:      filePath,
		Language:      "go",
		Hash:          hashStr,
		ParsedAtMilli: time.Now().UnixMilli(),
		Symbols:       make([]*Symbol, 0),
		Imports:       make([]Import, 0),
		Errors:        make([]string, 0),
	}

	// Extract symbols from the tree
	rootNode := tree.RootNode()
	if rootNode == nil {
		result.Errors = append(result.Errors, "tree-sitter returned nil root node")
		return result, nil
	}

	// Check for syntax errors in tree
	if rootNode.HasError() {
		result.Errors = append(result.Errors, "source contains syntax errors")
	}

	// Extract package name
	p.extractPackage(rootNode, content, filePath, result)

	// Extract imports
	p.extractImports(rootNode, content, filePath, result)

	// Extract functions
	p.extractFunctions(rootNode, content, filePath, result)

	// Extract methods
	p.extractMethods(rootNode, content, filePath, result)

	// Extract types (structs, interfaces, type aliases)
	p.extractTypes(rootNode, content, filePath, result)

	// Extract top-level variables and constants
	p.extractVariables(rootNode, content, filePath, result)
	p.extractConstants(rootNode, content, filePath, result)

	// Validate result before returning
	if err := result.Validate(); err != nil {
		return nil, fmt.Errorf("result validation failed: %w", err)
	}

	// Check context one final time
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("parse canceled after extraction: %w", err)
	}

	return result, nil
}

// Language returns the canonical language name for this parser.
//
// Returns:
//   - "go" for Go source files
func (p *GoParser) Language() string {
	return "go"
}

// Extensions returns the file extensions this parser handles.
//
// Returns:
//   - []string{".go"} for Go source files
func (p *GoParser) Extensions() []string {
	return []string{".go"}
}

// extractPackage extracts the package declaration from the AST.
func (p *GoParser) extractPackage(root *sitter.Node, content []byte, filePath string, result *ParseResult) {
	// Find package_clause node
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child.Type() == "package_clause" {
			// Get package name
			for j := 0; j < int(child.ChildCount()); j++ {
				nameNode := child.Child(j)
				if nameNode.Type() == "package_identifier" {
					name := string(content[nameNode.StartByte():nameNode.EndByte()])

					sym := &Symbol{
						ID:        GenerateID(filePath, int(nameNode.StartPoint().Row+1), name),
						Name:      name,
						Kind:      SymbolKindPackage,
						FilePath:  filePath,
						Language:  "go",
						Exported:  true,
						StartLine: int(nameNode.StartPoint().Row + 1),
						EndLine:   int(nameNode.EndPoint().Row + 1),
						StartCol:  int(nameNode.StartPoint().Column + 1),
						EndCol:    int(nameNode.EndPoint().Column + 1),
					}

					// Look for preceding comment
					sym.DocComment = p.getPrecedingComment(root, child, content)

					result.Symbols = append(result.Symbols, sym)
					return
				}
			}
		}
	}
}

// extractImports extracts import declarations from the AST.
func (p *GoParser) extractImports(root *sitter.Node, content []byte, filePath string, result *ParseResult) {
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child.Type() == "import_declaration" {
			p.processImportDecl(child, content, filePath, result)
		}
	}
}

// processImportDecl processes a single import declaration (which may contain multiple imports).
func (p *GoParser) processImportDecl(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)

		switch child.Type() {
		case "import_spec":
			p.processImportSpec(child, content, filePath, result)
		case "import_spec_list":
			// Grouped imports: import ( ... )
			for j := 0; j < int(child.ChildCount()); j++ {
				spec := child.Child(j)
				if spec.Type() == "import_spec" {
					p.processImportSpec(spec, content, filePath, result)
				}
			}
		}
	}
}

// processImportSpec extracts a single import specification.
func (p *GoParser) processImportSpec(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	var alias string
	var path string

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "package_identifier", "blank_identifier", "dot":
			alias = string(content[child.StartByte():child.EndByte()])
		case "interpreted_string_literal":
			// Remove quotes from path
			raw := string(content[child.StartByte():child.EndByte()])
			path = strings.Trim(raw, "\"")
		}
	}

	if path == "" {
		return
	}

	startLine := int(node.StartPoint().Row + 1)
	endLine := int(node.EndPoint().Row + 1)
	startCol := int(node.StartPoint().Column + 1)
	endCol := int(node.EndPoint().Column + 1)

	imp := Import{
		Path:  path,
		Alias: alias,
		Location: Location{
			FilePath:  filePath,
			StartLine: startLine,
			EndLine:   endLine,
			StartCol:  startCol,
			EndCol:    endCol,
		},
	}
	result.Imports = append(result.Imports, imp)

	// Also add as symbol for AST completeness
	sym := &Symbol{
		ID:        GenerateID(filePath, startLine, path),
		Name:      path,
		Kind:      SymbolKindImport,
		FilePath:  filePath,
		Language:  "go",
		Exported:  true,
		StartLine: startLine,
		EndLine:   endLine,
		StartCol:  startCol,
		EndCol:    endCol,
	}
	result.Symbols = append(result.Symbols, sym)
}

// extractFunctions extracts function declarations from the AST.
func (p *GoParser) extractFunctions(root *sitter.Node, content []byte, filePath string, result *ParseResult) {
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child.Type() == "function_declaration" {
			p.processFunctionDecl(child, content, filePath, result, root)
		}
	}
}

// processFunctionDecl extracts a single function declaration.
func (p *GoParser) processFunctionDecl(node *sitter.Node, content []byte, filePath string, result *ParseResult, root *sitter.Node) {
	var name string
	var signature string
	var params string
	var returns string

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "identifier":
			name = string(content[child.StartByte():child.EndByte()])
		case "parameter_list":
			// First parameter_list is params, subsequent ones are return types
			plist := string(content[child.StartByte():child.EndByte()])
			if params == "" {
				params = plist
			} else {
				returns = plist
			}
		case "type_identifier", "pointer_type", "slice_type", "map_type", "channel_type", "qualified_type", "interface_type", "struct_type", "function_type":
			returns = string(content[child.StartByte():child.EndByte()])
		}
	}

	if name == "" {
		return
	}

	// Build signature
	signature = fmt.Sprintf("func %s%s", name, params)
	if returns != "" {
		signature += " " + returns
	}

	exported := len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'

	// Filter unexported if configured
	if !p.parseOptions.IncludePrivate && !exported {
		return
	}

	sym := &Symbol{
		ID:        GenerateID(filePath, int(node.StartPoint().Row+1), name),
		Name:      name,
		Kind:      SymbolKindFunction,
		FilePath:  filePath,
		Language:  "go",
		Exported:  exported,
		Signature: signature,
		StartLine: int(node.StartPoint().Row + 1),
		EndLine:   int(node.EndPoint().Row + 1),
		StartCol:  int(node.StartPoint().Column + 1),
		EndCol:    int(node.EndPoint().Column + 1),
	}

	// Get doc comment
	sym.DocComment = p.getPrecedingComment(root, node, content)

	result.Symbols = append(result.Symbols, sym)
}

// extractMethods extracts method declarations from the AST.
func (p *GoParser) extractMethods(root *sitter.Node, content []byte, filePath string, result *ParseResult) {
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child.Type() == "method_declaration" {
			p.processMethodDecl(child, content, filePath, result, root)
		}
	}
}

// processMethodDecl extracts a single method declaration.
func (p *GoParser) processMethodDecl(node *sitter.Node, content []byte, filePath string, result *ParseResult, root *sitter.Node) {
	var name string
	var receiver string
	var params string
	var returns string

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "parameter_list":
			// First parameter_list is receiver, second is params
			plist := string(content[child.StartByte():child.EndByte()])
			if receiver == "" {
				receiver = plist
			} else if params == "" {
				params = plist
			} else {
				// Third parameter_list is returns
				returns = plist
			}
		case "field_identifier":
			name = string(content[child.StartByte():child.EndByte()])
		case "type_identifier", "pointer_type", "slice_type", "map_type", "channel_type", "qualified_type":
			returns = string(content[child.StartByte():child.EndByte()])
		}
	}

	if name == "" {
		return
	}

	// Build signature
	signature := fmt.Sprintf("func %s %s%s", receiver, name, params)
	if returns != "" {
		signature += " " + returns
	}

	exported := len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'

	// Filter unexported if configured
	if !p.parseOptions.IncludePrivate && !exported {
		return
	}

	sym := &Symbol{
		ID:        GenerateID(filePath, int(node.StartPoint().Row+1), name),
		Name:      name,
		Kind:      SymbolKindMethod,
		FilePath:  filePath,
		Language:  "go",
		Exported:  exported,
		Signature: signature,
		StartLine: int(node.StartPoint().Row + 1),
		EndLine:   int(node.EndPoint().Row + 1),
		StartCol:  int(node.StartPoint().Column + 1),
		EndCol:    int(node.EndPoint().Column + 1),
	}

	// Get doc comment
	sym.DocComment = p.getPrecedingComment(root, node, content)

	result.Symbols = append(result.Symbols, sym)
}

// extractTypes extracts type declarations (struct, interface, alias) from the AST.
func (p *GoParser) extractTypes(root *sitter.Node, content []byte, filePath string, result *ParseResult) {
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child.Type() == "type_declaration" {
			p.processTypeDecl(child, content, filePath, result, root)
		}
	}
}

// processTypeDecl extracts type declarations.
func (p *GoParser) processTypeDecl(node *sitter.Node, content []byte, filePath string, result *ParseResult, root *sitter.Node) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "type_spec" {
			p.processTypeSpec(child, content, filePath, result, root, node)
		}
	}
}

// processTypeSpec extracts a single type specification.
func (p *GoParser) processTypeSpec(node *sitter.Node, content []byte, filePath string, result *ParseResult, root *sitter.Node, parentDecl *sitter.Node) {
	var name string
	var kind SymbolKind
	var typeNode *sitter.Node

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "type_identifier":
			name = string(content[child.StartByte():child.EndByte()])
		case "struct_type":
			kind = SymbolKindStruct
			typeNode = child
		case "interface_type":
			kind = SymbolKindInterface
			typeNode = child
		default:
			if kind == 0 && name != "" {
				kind = SymbolKindType // Type alias
			}
		}
	}

	if name == "" {
		return
	}

	if kind == 0 {
		kind = SymbolKindType
	}

	exported := len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'

	// Filter unexported if configured
	if !p.parseOptions.IncludePrivate && !exported {
		return
	}

	sym := &Symbol{
		ID:        GenerateID(filePath, int(node.StartPoint().Row+1), name),
		Name:      name,
		Kind:      kind,
		FilePath:  filePath,
		Language:  "go",
		Exported:  exported,
		StartLine: int(node.StartPoint().Row + 1),
		EndLine:   int(node.EndPoint().Row + 1),
		StartCol:  int(node.StartPoint().Column + 1),
		EndCol:    int(node.EndPoint().Column + 1),
	}

	// Get doc comment (from type_declaration, not type_spec)
	sym.DocComment = p.getPrecedingComment(root, parentDecl, content)

	// Extract children (fields for struct, methods for interface)
	if typeNode != nil {
		sym.Children = p.extractTypeChildren(typeNode, content, filePath, kind)
	}

	result.Symbols = append(result.Symbols, sym)
}

// extractTypeChildren extracts fields from struct or method signatures from interface.
func (p *GoParser) extractTypeChildren(node *sitter.Node, content []byte, filePath string, parentKind SymbolKind) []*Symbol {
	children := make([]*Symbol, 0)

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "field_declaration_list":
			// Struct fields
			for j := 0; j < int(child.ChildCount()); j++ {
				field := child.Child(j)
				if field.Type() == "field_declaration" {
					children = append(children, p.extractField(field, content, filePath)...)
				}
			}
		case "method_elem":
			// Interface method (tree-sitter uses method_elem, not method_spec)
			if sym := p.extractMethodSpec(child, content, filePath); sym != nil {
				children = append(children, sym)
			}
		}
	}

	return children
}

// extractField extracts struct fields.
func (p *GoParser) extractField(node *sitter.Node, content []byte, filePath string) []*Symbol {
	fields := make([]*Symbol, 0)
	var fieldType string
	var names []string

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "field_identifier":
			names = append(names, string(content[child.StartByte():child.EndByte()]))
		default:
			// Assume other nodes are the type
			if len(names) > 0 && fieldType == "" {
				fieldType = string(content[child.StartByte():child.EndByte()])
			}
		}
	}

	for _, name := range names {
		exported := len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'

		if !p.parseOptions.IncludePrivate && !exported {
			continue
		}

		sym := &Symbol{
			ID:        GenerateID(filePath, int(node.StartPoint().Row+1), name),
			Name:      name,
			Kind:      SymbolKindField,
			FilePath:  filePath,
			Language:  "go",
			Exported:  exported,
			Signature: fieldType,
			StartLine: int(node.StartPoint().Row + 1),
			EndLine:   int(node.EndPoint().Row + 1),
			StartCol:  int(node.StartPoint().Column + 1),
			EndCol:    int(node.EndPoint().Column + 1),
		}
		fields = append(fields, sym)
	}

	return fields
}

// extractMethodSpec extracts interface method specifications.
func (p *GoParser) extractMethodSpec(node *sitter.Node, content []byte, filePath string) *Symbol {
	var name string
	var params string
	var returns string

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "field_identifier":
			name = string(content[child.StartByte():child.EndByte()])
		case "parameter_list":
			if params == "" {
				params = string(content[child.StartByte():child.EndByte()])
			} else {
				returns = string(content[child.StartByte():child.EndByte()])
			}
		case "type_identifier", "pointer_type", "slice_type", "map_type":
			returns = string(content[child.StartByte():child.EndByte()])
		}
	}

	if name == "" {
		return nil
	}

	signature := fmt.Sprintf("%s%s", name, params)
	if returns != "" {
		signature += " " + returns
	}

	exported := len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'

	if !p.parseOptions.IncludePrivate && !exported {
		return nil
	}

	return &Symbol{
		ID:        GenerateID(filePath, int(node.StartPoint().Row+1), name),
		Name:      name,
		Kind:      SymbolKindMethod,
		FilePath:  filePath,
		Language:  "go",
		Exported:  exported,
		Signature: signature,
		StartLine: int(node.StartPoint().Row + 1),
		EndLine:   int(node.EndPoint().Row + 1),
		StartCol:  int(node.StartPoint().Column + 1),
		EndCol:    int(node.EndPoint().Column + 1),
	}
}

// extractVariables extracts top-level variable declarations.
func (p *GoParser) extractVariables(root *sitter.Node, content []byte, filePath string, result *ParseResult) {
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child.Type() == "var_declaration" {
			p.processVarDecl(child, content, filePath, result, root, SymbolKindVariable)
		}
	}
}

// extractConstants extracts top-level constant declarations.
func (p *GoParser) extractConstants(root *sitter.Node, content []byte, filePath string, result *ParseResult) {
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child.Type() == "const_declaration" {
			p.processVarDecl(child, content, filePath, result, root, SymbolKindConstant)
		}
	}
}

// processVarDecl processes variable or constant declarations.
func (p *GoParser) processVarDecl(node *sitter.Node, content []byte, filePath string, result *ParseResult, root *sitter.Node, kind SymbolKind) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "var_spec", "const_spec":
			p.processVarSpec(child, content, filePath, result, root, kind, node)
		case "var_spec_list", "const_spec_list":
			for j := 0; j < int(child.ChildCount()); j++ {
				spec := child.Child(j)
				if spec.Type() == "var_spec" || spec.Type() == "const_spec" {
					p.processVarSpec(spec, content, filePath, result, root, kind, node)
				}
			}
		}
	}
}

// processVarSpec processes a single variable or constant specification.
func (p *GoParser) processVarSpec(node *sitter.Node, content []byte, filePath string, result *ParseResult, root *sitter.Node, kind SymbolKind, parentDecl *sitter.Node) {
	var names []string
	var typeStr string

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "identifier":
			names = append(names, string(content[child.StartByte():child.EndByte()]))
		case "type_identifier", "pointer_type", "slice_type", "map_type", "channel_type", "qualified_type":
			typeStr = string(content[child.StartByte():child.EndByte()])
		}
	}

	for _, name := range names {
		exported := len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'

		if !p.parseOptions.IncludePrivate && !exported {
			continue
		}

		sym := &Symbol{
			ID:        GenerateID(filePath, int(node.StartPoint().Row+1), name),
			Name:      name,
			Kind:      kind,
			FilePath:  filePath,
			Language:  "go",
			Exported:  exported,
			Signature: typeStr,
			StartLine: int(node.StartPoint().Row + 1),
			EndLine:   int(node.EndPoint().Row + 1),
			StartCol:  int(node.StartPoint().Column + 1),
			EndCol:    int(node.EndPoint().Column + 1),
		}

		// Get doc comment
		sym.DocComment = p.getPrecedingComment(root, parentDecl, content)

		result.Symbols = append(result.Symbols, sym)
	}
}

// getPrecedingComment extracts the doc comment immediately before a node.
func (p *GoParser) getPrecedingComment(root *sitter.Node, node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}

	nodeStartLine := int(node.StartPoint().Row)

	// Search siblings for comment immediately preceding this node
	for i := 0; i < int(root.ChildCount()); i++ {
		sibling := root.Child(i)
		if sibling.Type() == "comment" {
			commentEndLine := int(sibling.EndPoint().Row)
			// Comment must be on line immediately before node
			if commentEndLine == nodeStartLine-1 || commentEndLine == nodeStartLine {
				return strings.TrimSpace(string(content[sibling.StartByte():sibling.EndByte()]))
			}
		}
	}

	return ""
}

// Compile-time interface compliance check.
var _ Parser = (*GoParser)(nil)
