package ast

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"
)

// PythonParserOption configures a PythonParser instance.
type PythonParserOption func(*PythonParser)

// WithPythonMaxFileSize sets the maximum file size the parser will accept.
//
// Parameters:
//   - bytes: Maximum file size in bytes. Must be positive.
//
// Example:
//
//	parser := NewPythonParser(WithPythonMaxFileSize(5 * 1024 * 1024)) // 5MB limit
func WithPythonMaxFileSize(bytes int64) PythonParserOption {
	return func(p *PythonParser) {
		if bytes > 0 {
			p.maxFileSize = bytes
		}
	}
}

// WithPythonParseOptions applies the given ParseOptions to the parser.
//
// Parameters:
//   - opts: ParseOptions to apply.
//
// Example:
//
//	parser := NewPythonParser(WithPythonParseOptions(ParseOptions{IncludePrivate: false}))
func WithPythonParseOptions(opts ParseOptions) PythonParserOption {
	return func(p *PythonParser) {
		p.parseOptions = opts
	}
}

// PythonParser implements the Parser interface for Python source code.
//
// Description:
//
//	PythonParser uses tree-sitter to parse Python source files and extract symbols.
//	It supports concurrent use from multiple goroutines - each Parse call
//	creates its own tree-sitter parser instance internally.
//
// Thread Safety:
//
//	PythonParser instances are safe for concurrent use. Multiple goroutines
//	may call Parse simultaneously on the same PythonParser instance.
//
// Example:
//
//	parser := NewPythonParser()
//	result, err := parser.Parse(ctx, []byte("def hello(): pass"), "main.py")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for _, sym := range result.Symbols {
//	    fmt.Printf("%s: %s\n", sym.Kind, sym.Name)
//	}
type PythonParser struct {
	maxFileSize  int64
	parseOptions ParseOptions
}

// NewPythonParser creates a new PythonParser with the given options.
//
// Description:
//
//	Creates a PythonParser configured with sensible defaults. Options can be
//	provided to customize behavior such as maximum file size.
//
// Inputs:
//   - opts: Optional configuration functions (WithPythonMaxFileSize, WithPythonParseOptions)
//
// Outputs:
//   - *PythonParser: Configured parser instance, never nil
//
// Example:
//
//	// Default configuration
//	parser := NewPythonParser()
//
//	// Custom max file size
//	parser := NewPythonParser(WithPythonMaxFileSize(5 * 1024 * 1024))
//
// Thread Safety:
//
//	The returned PythonParser is safe for concurrent use.
func NewPythonParser(opts ...PythonParserOption) *PythonParser {
	p := &PythonParser{
		maxFileSize:  DefaultMaxFileSize,
		parseOptions: DefaultParseOptions(),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Parse extracts symbols from Python source code.
//
// Description:
//
//	Parse uses tree-sitter to parse the provided Python source code and extract
//	all symbols (functions, classes, methods, etc.) into a ParseResult.
//	The parser is error-tolerant and will return partial results for syntactically
//	invalid code.
//
// Inputs:
//   - ctx: Context for cancellation. Checked before and after parsing.
//     Note: Tree-sitter parsing itself cannot be interrupted mid-parse.
//   - content: Raw Python source code bytes. Must be valid UTF-8.
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
//	result, err := parser.Parse(ctx, []byte("def hello(): pass"), "main.py")
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("Found %d symbols\n", len(result.Symbols))
//
// Limitations:
//   - Tree-sitter parsing is synchronous and cannot be interrupted mid-parse
//   - Very large files may take significant time to parse
//   - Some edge cases in Python syntax may not be fully handled
//
// Assumptions:
//   - Content is valid UTF-8 (validated internally)
//   - FilePath uses forward slashes as path separator
//   - FilePath does not contain path traversal sequences
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (p *PythonParser) Parse(ctx context.Context, content []byte, filePath string) (*ParseResult, error) {
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
	parser.SetLanguage(python.GetLanguage())

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
		Language:      "python",
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

	// Extract module docstring
	p.extractModuleDocstring(rootNode, content, filePath, result)

	// Extract imports
	p.extractImports(rootNode, content, filePath, result)

	// Extract classes and their methods
	p.extractClasses(rootNode, content, filePath, result)

	// Extract top-level functions
	p.extractFunctions(rootNode, content, filePath, result, nil)

	// Extract module-level variables
	p.extractModuleVariables(rootNode, content, filePath, result)

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
//   - "python" for Python source files
func (p *PythonParser) Language() string {
	return "python"
}

// Extensions returns the file extensions this parser handles.
//
// Returns:
//   - []string{".py", ".pyi"} for Python source and stub files
func (p *PythonParser) Extensions() []string {
	return []string{".py", ".pyi"}
}

// extractModuleDocstring extracts the module-level docstring if present.
func (p *PythonParser) extractModuleDocstring(root *sitter.Node, content []byte, filePath string, result *ParseResult) {
	// Module docstring is the first expression_statement with a string child
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child.Type() == "expression_statement" {
			// Check if first child is a string
			if child.ChildCount() > 0 {
				strNode := child.Child(0)
				if strNode.Type() == "string" {
					docstring := p.extractStringContent(strNode, content)

					sym := &Symbol{
						ID:         GenerateID(filePath, int(child.StartPoint().Row+1), "__module__"),
						Name:       "__module__",
						Kind:       SymbolKindPackage,
						FilePath:   filePath,
						Language:   "python",
						Exported:   true,
						DocComment: docstring,
						StartLine:  int(child.StartPoint().Row + 1),
						EndLine:    int(child.EndPoint().Row + 1),
						StartCol:   int(child.StartPoint().Column),
						EndCol:     int(child.EndPoint().Column),
					}
					result.Symbols = append(result.Symbols, sym)
					return
				}
			}
		}
		// Stop looking after non-string, non-comment, non-import first statement
		if child.Type() != "comment" && child.Type() != "import_statement" && child.Type() != "import_from_statement" {
			return
		}
	}
}

// extractImports extracts import statements from the AST.
func (p *PythonParser) extractImports(root *sitter.Node, content []byte, filePath string, result *ParseResult) {
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		switch child.Type() {
		case "import_statement":
			p.processImportStatement(child, content, filePath, result)
		case "import_from_statement":
			p.processImportFromStatement(child, content, filePath, result)
		}
	}
}

// processImportStatement handles 'import foo' or 'import foo as bar' style imports.
func (p *PythonParser) processImportStatement(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	// import_statement contains dotted_name or aliased_import children
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "dotted_name":
			path := string(content[child.StartByte():child.EndByte()])
			p.addImport(node, path, "", nil, false, false, filePath, result)
		case "aliased_import":
			var path, alias string
			for j := 0; j < int(child.ChildCount()); j++ {
				grandchild := child.Child(j)
				switch grandchild.Type() {
				case "dotted_name":
					path = string(content[grandchild.StartByte():grandchild.EndByte()])
				case "identifier":
					alias = string(content[grandchild.StartByte():grandchild.EndByte()])
				}
			}
			if path != "" {
				p.addImport(node, path, alias, nil, false, false, filePath, result)
			}
		}
	}
}

// processImportFromStatement handles 'from x import y' style imports.
func (p *PythonParser) processImportFromStatement(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	var modulePath string
	var names []string
	var isWildcard bool
	var isRelative bool
	var sawImport bool // Track when we've seen the "import" keyword

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "from":
			// Skip the "from" keyword
		case "import":
			// Mark that we've seen import - subsequent dotted_name/identifier are imported names
			sawImport = true
		case "relative_import":
			isRelative = true
			// relative_import contains import_prefix (dots) and optionally dotted_name
			var prefix string
			var name string
			for j := 0; j < int(child.ChildCount()); j++ {
				grandchild := child.Child(j)
				switch grandchild.Type() {
				case "import_prefix":
					prefix = string(content[grandchild.StartByte():grandchild.EndByte()])
				case "dotted_name":
					name = string(content[grandchild.StartByte():grandchild.EndByte()])
				}
			}
			modulePath = prefix + name
		case "dotted_name":
			nameStr := string(content[child.StartByte():child.EndByte()])
			if !sawImport {
				// Before "import" keyword - this is the module path
				modulePath = nameStr
			} else {
				// After "import" keyword - this is an imported name
				names = append(names, nameStr)
			}
		case "wildcard_import":
			isWildcard = true
		case "aliased_import":
			// from x import y as z
			var importName, alias string
			for j := 0; j < int(child.ChildCount()); j++ {
				grandchild := child.Child(j)
				switch grandchild.Type() {
				case "identifier":
					if importName == "" {
						importName = string(content[grandchild.StartByte():grandchild.EndByte()])
					} else {
						alias = string(content[grandchild.StartByte():grandchild.EndByte()])
					}
				case "dotted_name":
					if importName == "" {
						importName = string(content[grandchild.StartByte():grandchild.EndByte()])
					}
				}
			}
			if alias != "" {
				names = append(names, importName+" as "+alias)
			} else if importName != "" {
				names = append(names, importName)
			}
		case "identifier":
			if sawImport {
				names = append(names, string(content[child.StartByte():child.EndByte()]))
			}
		}
	}

	if modulePath != "" || isRelative {
		if modulePath == "" && isRelative {
			modulePath = "."
		}
		p.addImport(node, modulePath, "", names, isWildcard, isRelative, filePath, result)
	}
}

// addImport adds an import to the result (both Import struct and Symbol).
func (p *PythonParser) addImport(node *sitter.Node, path, alias string, names []string, isWildcard, isRelative bool, filePath string, result *ParseResult) {
	startLine := int(node.StartPoint().Row + 1)
	endLine := int(node.EndPoint().Row + 1)
	startCol := int(node.StartPoint().Column)
	endCol := int(node.EndPoint().Column)

	imp := Import{
		Path:       path,
		Alias:      alias,
		Names:      names,
		IsWildcard: isWildcard,
		IsRelative: isRelative,
		Location: Location{
			FilePath:  filePath,
			StartLine: startLine,
			EndLine:   endLine,
			StartCol:  startCol,
			EndCol:    endCol,
		},
	}
	result.Imports = append(result.Imports, imp)

	// Also add as symbol
	sym := &Symbol{
		ID:        GenerateID(filePath, startLine, path),
		Name:      path,
		Kind:      SymbolKindImport,
		FilePath:  filePath,
		Language:  "python",
		Exported:  true,
		StartLine: startLine,
		EndLine:   endLine,
		StartCol:  startCol,
		EndCol:    endCol,
	}
	result.Symbols = append(result.Symbols, sym)
}

// extractClasses extracts class definitions from the AST.
func (p *PythonParser) extractClasses(root *sitter.Node, content []byte, filePath string, result *ParseResult) {
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		switch child.Type() {
		case "class_definition":
			p.processClass(child, content, filePath, result, nil)
		case "decorated_definition":
			p.processDecoratedDefinition(child, content, filePath, result, nil)
		}
	}
}

// processClass extracts a class definition.
func (p *PythonParser) processClass(node *sitter.Node, content []byte, filePath string, result *ParseResult, decorators []string) *Symbol {
	var name string
	var bases []string
	var bodyNode *sitter.Node
	var docstring string

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "identifier":
			name = string(content[child.StartByte():child.EndByte()])
		case "argument_list":
			// Extract base classes
			for j := 0; j < int(child.ChildCount()); j++ {
				arg := child.Child(j)
				if arg.Type() == "identifier" || arg.Type() == "attribute" {
					bases = append(bases, string(content[arg.StartByte():arg.EndByte()]))
				}
			}
		case "block":
			bodyNode = child
		}
	}

	if name == "" {
		return nil
	}

	exported := p.isExported(name)
	if !p.parseOptions.IncludePrivate && !exported {
		return nil
	}

	// Extract docstring from body
	if bodyNode != nil {
		docstring = p.extractDocstring(bodyNode, content)
	}

	sym := &Symbol{
		ID:         GenerateID(filePath, int(node.StartPoint().Row+1), name),
		Name:       name,
		Kind:       SymbolKindClass,
		FilePath:   filePath,
		Language:   "python",
		Exported:   exported,
		DocComment: docstring,
		StartLine:  int(node.StartPoint().Row + 1),
		EndLine:    int(node.EndPoint().Row + 1),
		StartCol:   int(node.StartPoint().Column),
		EndCol:     int(node.EndPoint().Column),
		Children:   make([]*Symbol, 0),
	}

	// Add metadata for decorators and bases
	if len(decorators) > 0 || len(bases) > 0 {
		sym.Metadata = &SymbolMetadata{
			Decorators: decorators,
		}
		if len(bases) > 0 {
			sym.Metadata.Extends = bases[0]
			if len(bases) > 1 {
				sym.Metadata.Implements = bases[1:]
			}
		}
	}

	// Extract methods and class variables from body
	if bodyNode != nil {
		p.extractClassMembers(bodyNode, content, filePath, sym)
	}

	result.Symbols = append(result.Symbols, sym)
	return sym
}

// extractClassMembers extracts methods and class variables from a class body.
func (p *PythonParser) extractClassMembers(body *sitter.Node, content []byte, filePath string, classSym *Symbol) {
	for i := 0; i < int(body.ChildCount()); i++ {
		child := body.Child(i)
		switch child.Type() {
		case "function_definition":
			if method := p.processMethod(child, content, filePath, nil, classSym.Name); method != nil {
				classSym.Children = append(classSym.Children, method)
			}
		case "decorated_definition":
			if method := p.processDecoratedMethod(child, content, filePath, classSym.Name); method != nil {
				classSym.Children = append(classSym.Children, method)
			}
		case "expression_statement":
			// Class-level variable assignments
			if child.ChildCount() > 0 {
				assign := child.Child(0)
				if assign.Type() == "assignment" || assign.Type() == "augmented_assignment" {
					if field := p.processClassVariable(assign, content, filePath); field != nil {
						classSym.Children = append(classSym.Children, field)
					}
				}
			}
		}
	}
}

// processClassVariable extracts a class variable (field).
func (p *PythonParser) processClassVariable(node *sitter.Node, content []byte, filePath string) *Symbol {
	var name string
	var typeStr string

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "identifier":
			if name == "" {
				name = string(content[child.StartByte():child.EndByte()])
			}
		case "type":
			typeStr = string(content[child.StartByte():child.EndByte()])
		}
	}

	if name == "" {
		return nil
	}

	exported := p.isExported(name)
	if !p.parseOptions.IncludePrivate && !exported {
		return nil
	}

	return &Symbol{
		ID:        GenerateID(filePath, int(node.StartPoint().Row+1), name),
		Name:      name,
		Kind:      SymbolKindField,
		FilePath:  filePath,
		Language:  "python",
		Exported:  exported,
		Signature: typeStr,
		StartLine: int(node.StartPoint().Row + 1),
		EndLine:   int(node.EndPoint().Row + 1),
		StartCol:  int(node.StartPoint().Column),
		EndCol:    int(node.EndPoint().Column),
	}
}

// processMethod extracts a method from a class.
func (p *PythonParser) processMethod(node *sitter.Node, content []byte, filePath string, decorators []string, className string) *Symbol {
	return p.processFunction(node, content, filePath, decorators, className)
}

// processDecoratedMethod extracts a decorated method.
func (p *PythonParser) processDecoratedMethod(node *sitter.Node, content []byte, filePath string, className string) *Symbol {
	decorators := p.extractDecorators(node, content)

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "function_definition" {
			return p.processMethod(child, content, filePath, decorators, className)
		}
	}

	return nil
}

// extractFunctions extracts top-level function definitions.
func (p *PythonParser) extractFunctions(root *sitter.Node, content []byte, filePath string, result *ParseResult, parent *Symbol) {
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		switch child.Type() {
		case "function_definition":
			if fn := p.processFunction(child, content, filePath, nil, ""); fn != nil {
				// Extract nested functions
				p.extractNestedFunctions(child, content, filePath, fn)
				result.Symbols = append(result.Symbols, fn)
			}
		case "decorated_definition":
			// Check if it's a function (not a class)
			for j := 0; j < int(child.ChildCount()); j++ {
				grandchild := child.Child(j)
				if grandchild.Type() == "function_definition" {
					decorators := p.extractDecorators(child, content)
					if fn := p.processFunction(grandchild, content, filePath, decorators, ""); fn != nil {
						// Extract nested functions
						p.extractNestedFunctions(grandchild, content, filePath, fn)
						result.Symbols = append(result.Symbols, fn)
					}
					break
				}
			}
		}
	}
}

// extractNestedFunctions extracts nested function definitions.
func (p *PythonParser) extractNestedFunctions(funcNode *sitter.Node, content []byte, filePath string, parentFn *Symbol) {
	// Find the block node
	for i := 0; i < int(funcNode.ChildCount()); i++ {
		child := funcNode.Child(i)
		if child.Type() == "block" {
			for j := 0; j < int(child.ChildCount()); j++ {
				stmt := child.Child(j)
				if stmt.Type() == "function_definition" {
					if nested := p.processFunction(stmt, content, filePath, nil, ""); nested != nil {
						parentFn.Children = append(parentFn.Children, nested)
					}
				} else if stmt.Type() == "decorated_definition" {
					decorators := p.extractDecorators(stmt, content)
					for k := 0; k < int(stmt.ChildCount()); k++ {
						def := stmt.Child(k)
						if def.Type() == "function_definition" {
							if nested := p.processFunction(def, content, filePath, decorators, ""); nested != nil {
								parentFn.Children = append(parentFn.Children, nested)
							}
							break
						}
					}
				}
			}
			break
		}
	}
}

// processFunction extracts a function definition.
func (p *PythonParser) processFunction(node *sitter.Node, content []byte, filePath string, decorators []string, className string) *Symbol {
	var name string
	var params string
	var returnType string
	var docstring string
	var isAsync bool

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "async":
			// async keyword is a child of function_definition in tree-sitter-python
			isAsync = true
		case "identifier":
			name = string(content[child.StartByte():child.EndByte()])
		case "parameters":
			params = string(content[child.StartByte():child.EndByte()])
		case "type":
			returnType = string(content[child.StartByte():child.EndByte()])
		case "block":
			docstring = p.extractDocstring(child, content)
		}
	}

	if name == "" {
		return nil
	}

	exported := p.isExported(name)
	if !p.parseOptions.IncludePrivate && !exported {
		return nil
	}

	// Determine kind based on decorators and context
	kind := SymbolKindFunction
	isStatic := false
	if className != "" {
		kind = SymbolKindMethod
	}

	// Check for special decorators
	for _, dec := range decorators {
		switch dec {
		case "property":
			kind = SymbolKindProperty
		case "staticmethod":
			isStatic = true
		case "classmethod":
			isStatic = true
		}
	}

	// Build signature
	var signature string
	if isAsync {
		signature = fmt.Sprintf("async def %s%s", name, params)
	} else {
		signature = fmt.Sprintf("def %s%s", name, params)
	}
	if returnType != "" {
		signature += " -> " + returnType
	}

	sym := &Symbol{
		ID:         GenerateID(filePath, int(node.StartPoint().Row+1), name),
		Name:       name,
		Kind:       kind,
		FilePath:   filePath,
		Language:   "python",
		Exported:   exported,
		Signature:  signature,
		DocComment: docstring,
		StartLine:  int(node.StartPoint().Row + 1),
		EndLine:    int(node.EndPoint().Row + 1),
		StartCol:   int(node.StartPoint().Column),
		EndCol:     int(node.EndPoint().Column),
	}

	// Add metadata
	if len(decorators) > 0 || returnType != "" || isAsync || isStatic {
		sym.Metadata = &SymbolMetadata{
			Decorators: decorators,
			ReturnType: returnType,
			IsAsync:    isAsync,
			IsStatic:   isStatic,
		}
	}

	return sym
}

// processDecoratedDefinition handles decorated classes and functions at module level.
func (p *PythonParser) processDecoratedDefinition(node *sitter.Node, content []byte, filePath string, result *ParseResult, parent *Symbol) {
	decorators := p.extractDecorators(node, content)

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "class_definition":
			p.processClass(child, content, filePath, result, decorators)
		case "function_definition":
			// Handled in extractFunctions
		}
	}
}

// extractDecorators extracts decorator names from a decorated_definition.
func (p *PythonParser) extractDecorators(node *sitter.Node, content []byte) []string {
	decorators := make([]string, 0)

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "decorator" {
			// Get the decorator name (identifier or attribute)
			for j := 0; j < int(child.ChildCount()); j++ {
				grandchild := child.Child(j)
				switch grandchild.Type() {
				case "identifier":
					decorators = append(decorators, string(content[grandchild.StartByte():grandchild.EndByte()]))
				case "attribute":
					decorators = append(decorators, string(content[grandchild.StartByte():grandchild.EndByte()]))
				case "call":
					// Decorator with arguments: @foo(x)
					for k := 0; k < int(grandchild.ChildCount()); k++ {
						ggchild := grandchild.Child(k)
						if ggchild.Type() == "identifier" || ggchild.Type() == "attribute" {
							decorators = append(decorators, string(content[ggchild.StartByte():ggchild.EndByte()]))
							break
						}
					}
				}
			}
		}
	}

	return decorators
}

// extractModuleVariables extracts top-level variable assignments.
func (p *PythonParser) extractModuleVariables(root *sitter.Node, content []byte, filePath string, result *ParseResult) {
	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child.Type() == "expression_statement" {
			if child.ChildCount() > 0 {
				expr := child.Child(0)
				if expr.Type() == "assignment" {
					if variable := p.processModuleVariable(expr, content, filePath); variable != nil {
						result.Symbols = append(result.Symbols, variable)
					}
				}
			}
		}
	}
}

// processModuleVariable extracts a module-level variable.
func (p *PythonParser) processModuleVariable(node *sitter.Node, content []byte, filePath string) *Symbol {
	var name string
	var typeStr string

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case "identifier":
			if name == "" {
				name = string(content[child.StartByte():child.EndByte()])
			}
		case "type":
			typeStr = string(content[child.StartByte():child.EndByte()])
		}
	}

	if name == "" {
		return nil
	}

	// Skip internal variables like __all__, __version__, etc. for symbol extraction
	// but don't skip them entirely - they're useful
	exported := p.isExported(name)
	if !p.parseOptions.IncludePrivate && !exported {
		return nil
	}

	// Determine kind: CONSTANT if all caps, otherwise variable
	kind := SymbolKindVariable
	if isAllCaps(name) {
		kind = SymbolKindConstant
	}

	return &Symbol{
		ID:        GenerateID(filePath, int(node.StartPoint().Row+1), name),
		Name:      name,
		Kind:      kind,
		FilePath:  filePath,
		Language:  "python",
		Exported:  exported,
		Signature: typeStr,
		StartLine: int(node.StartPoint().Row + 1),
		EndLine:   int(node.EndPoint().Row + 1),
		StartCol:  int(node.StartPoint().Column),
		EndCol:    int(node.EndPoint().Column),
	}
}

// extractDocstring extracts the docstring from a block node.
func (p *PythonParser) extractDocstring(block *sitter.Node, content []byte) string {
	// First statement in block might be docstring
	if block.ChildCount() > 0 {
		first := block.Child(0)
		if first.Type() == "expression_statement" && first.ChildCount() > 0 {
			strNode := first.Child(0)
			if strNode.Type() == "string" {
				return p.extractStringContent(strNode, content)
			}
		}
	}
	return ""
}

// extractStringContent extracts the content from a string node, removing quotes.
func (p *PythonParser) extractStringContent(node *sitter.Node, content []byte) string {
	raw := string(content[node.StartByte():node.EndByte()])

	// Handle triple-quoted strings
	if strings.HasPrefix(raw, `"""`) || strings.HasPrefix(raw, `'''`) {
		return strings.Trim(raw, `"'`)
	}

	// Handle single-quoted strings
	return strings.Trim(raw, `"'`)
}

// isExported determines if a Python name is exported.
//
// Python visibility rules:
//   - Names starting with _ (single underscore) are conventionally private
//   - Names starting with __ but not ending with __ are name-mangled (private)
//   - Dunder names (__init__, __str__, etc.) are special/public
//   - All other names are public
func (p *PythonParser) isExported(name string) bool {
	if name == "" {
		return false
	}

	// Dunder methods (__xxx__) are exported
	if strings.HasPrefix(name, "__") && strings.HasSuffix(name, "__") {
		return true
	}

	// Name-mangled (__xxx) is not exported
	if strings.HasPrefix(name, "__") {
		return false
	}

	// Single underscore prefix is not exported
	if strings.HasPrefix(name, "_") {
		return false
	}

	return true
}

// isAllCaps returns true if the name is all uppercase (with underscores allowed).
func isAllCaps(name string) bool {
	for _, r := range name {
		if r != '_' && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return len(name) > 0
}

// Compile-time interface compliance check.
var _ Parser = (*PythonParser)(nil)
