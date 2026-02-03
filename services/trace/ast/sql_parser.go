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
	"github.com/smacker/go-tree-sitter/sql"
)

// SQLParser extracts symbols from SQL source code.
//
// Description:
//
//	SQLParser uses tree-sitter to parse SQL source files and extract
//	structured symbol information including tables, columns, views,
//	indexes, and constraints.
//
// Thread Safety:
//
//	SQLParser is safe for concurrent use. Multiple goroutines can call Parse
//	simultaneously. Each Parse call creates its own tree-sitter parser instance.
//
// Example:
//
//	parser := NewSQLParser()
//	result, err := parser.Parse(ctx, content, "schema.sql")
//	if err != nil {
//	    return fmt.Errorf("parse: %w", err)
//	}
//	for _, sym := range result.Symbols {
//	    fmt.Printf("%s: %s\n", sym.Kind, sym.Name)
//	}
type SQLParser struct {
	options SQLParserOptions
}

// SQLParserOptions configures SQLParser behavior.
type SQLParserOptions struct {
	// MaxFileSize is the maximum file size in bytes to parse.
	// Files larger than this return ErrFileTooLarge.
	// Default: 10MB
	MaxFileSize int

	// ExtractColumns determines whether to extract individual columns.
	// Default: true
	ExtractColumns bool
}

// DefaultSQLParserOptions returns the default options.
func DefaultSQLParserOptions() SQLParserOptions {
	return SQLParserOptions{
		MaxFileSize:    10 * 1024 * 1024, // 10MB
		ExtractColumns: true,
	}
}

// SQLParserOption is a functional option for configuring SQLParser.
type SQLParserOption func(*SQLParserOptions)

// WithSQLMaxFileSize sets the maximum file size for parsing.
func WithSQLMaxFileSize(size int) SQLParserOption {
	return func(o *SQLParserOptions) {
		o.MaxFileSize = size
	}
}

// WithSQLExtractColumns sets whether to extract individual columns.
func WithSQLExtractColumns(extract bool) SQLParserOption {
	return func(o *SQLParserOptions) {
		o.ExtractColumns = extract
	}
}

// NewSQLParser creates a new SQLParser with the given options.
//
// Description:
//
//	Creates a parser configured for SQL source files. The parser can be
//	reused for multiple files and is safe for concurrent use.
//
// Example:
//
//	// Default options
//	parser := NewSQLParser()
//
//	// With custom options
//	parser := NewSQLParser(
//	    WithSQLMaxFileSize(5 * 1024 * 1024),
//	    WithSQLExtractColumns(false),
//	)
func NewSQLParser(opts ...SQLParserOption) *SQLParser {
	options := DefaultSQLParserOptions()
	for _, opt := range opts {
		opt(&options)
	}
	return &SQLParser{
		options: options,
	}
}

// Language returns the language name for this parser.
func (p *SQLParser) Language() string {
	return "sql"
}

// Extensions returns the file extensions this parser handles.
func (p *SQLParser) Extensions() []string {
	return []string{".sql"}
}

// Parse extracts symbols from SQL source code.
//
// Description:
//
//	Parses the provided SQL content using tree-sitter and extracts all
//	symbols including tables, columns, views, and indexes.
//
// Inputs:
//
//	ctx      - Context for cancellation. Checked before/after parsing.
//	content  - Raw SQL source bytes. Must be valid UTF-8.
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
func (p *SQLParser) Parse(ctx context.Context, content []byte, filePath string) (*ParseResult, error) {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sql parse canceled before start: %w", err)
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
		Language:      "sql",
		Hash:          hashStr,
		ParsedAtMilli: time.Now().UnixMilli(),
		Symbols:       make([]*Symbol, 0),
		Imports:       make([]Import, 0),
		Errors:        make([]string, 0),
	}

	// Parse with tree-sitter
	parser := sitter.NewParser()
	parser.SetLanguage(sql.GetLanguage())

	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter parse failed: %w", err)
	}
	defer tree.Close()

	// Check context after parsing
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("sql parse canceled after tree-sitter: %w", err)
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
func (p *SQLParser) extractSymbols(ctx context.Context, node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	if node == nil {
		return
	}

	// Check context periodically
	if ctx.Err() != nil {
		return
	}

	nodeType := node.Type()

	switch nodeType {
	case sqlNodeProgram:
		// Process all children
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(ctx, node.Child(i), content, filePath, result)
		}

	case sqlNodeStatement:
		// Process statement children
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(ctx, node.Child(i), content, filePath, result)
		}

	case sqlNodeCreateTable:
		p.extractCreateTable(ctx, node, content, filePath, result)

	case sqlNodeCreateIndex:
		p.extractCreateIndex(node, content, filePath, result)

	case sqlNodeCreateView:
		p.extractCreateView(node, content, filePath, result)

	case sqlNodeComment:
		// SQL comments are not extracted as symbols - skip them

	default:
		// Recurse into children for other node types
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(ctx, node.Child(i), content, filePath, result)
		}
	}
}

// extractCreateTable extracts table and column symbols from CREATE TABLE.
func (p *SQLParser) extractCreateTable(ctx context.Context, node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	tableName := ""
	var columnDefs *sitter.Node

	// Find table name and column definitions
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case sqlNodeObjectReference:
			tableName = p.extractIdentifier(child, content)
		case sqlNodeColumnDefinitions:
			columnDefs = child
		}
	}

	if tableName == "" {
		return
	}

	// Create table symbol
	tableSym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, tableName),
		Name:          tableName,
		Kind:          SymbolKindTable,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     fmt.Sprintf("CREATE TABLE %s", tableName),
		Language:      "sql",
		ParsedAtMilli: time.Now().UnixMilli(),
		Exported:      true,
	}
	result.Symbols = append(result.Symbols, tableSym)

	// Extract columns if enabled
	if p.options.ExtractColumns && columnDefs != nil {
		p.extractColumns(ctx, columnDefs, content, filePath, tableName, result)
	}
}

// extractColumns extracts column definitions from column_definitions node.
func (p *SQLParser) extractColumns(ctx context.Context, node *sitter.Node, content []byte, filePath, tableName string, result *ParseResult) {
	for i := 0; i < int(node.ChildCount()); i++ {
		if ctx.Err() != nil {
			return
		}

		child := node.Child(i)
		if child.Type() == sqlNodeColumnDefinition {
			p.extractColumn(child, content, filePath, tableName, result)
		}
	}
}

// extractColumn extracts a single column definition.
func (p *SQLParser) extractColumn(node *sitter.Node, content []byte, filePath, tableName string, result *ParseResult) {
	columnName := ""
	dataType := ""
	isPrimaryKey := false
	isUnique := false
	isNotNull := false

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case sqlNodeIdentifier:
			if columnName == "" {
				columnName = string(content[child.StartByte():child.EndByte()])
			}
		case sqlNodeInt, sqlNodeVarchar, sqlNodeDecimal, sqlNodeTimestamp, sqlNodeDate, sqlNodeBool, sqlNodeText, sqlNodeBlob:
			dataType = p.extractDataType(child, content)
		case sqlNodeKeywordPrimary:
			isPrimaryKey = true
		case sqlNodeKeywordUnique:
			isUnique = true
		case sqlNodeKeywordNot:
			// Check if next is NULL
			if i+1 < int(node.ChildCount()) && node.Child(i+1).Type() == sqlNodeKeywordNull {
				isNotNull = true
			}
		}
	}

	if columnName == "" {
		return
	}

	// Build signature
	signature := columnName
	if dataType != "" {
		signature += " " + dataType
	}
	if isPrimaryKey {
		signature += " PRIMARY KEY"
	}
	if isUnique && !isPrimaryKey {
		signature += " UNIQUE"
	}
	if isNotNull {
		signature += " NOT NULL"
	}

	// Build metadata
	metadata := &SymbolMetadata{
		ParentName: tableName,
	}
	if isPrimaryKey {
		metadata.SQLConstraints = []string{"PRIMARY KEY"}
	} else if isUnique {
		metadata.SQLConstraints = []string{"UNIQUE"}
	}
	if isNotNull {
		metadata.SQLConstraints = append(metadata.SQLConstraints, "NOT NULL")
	}

	sym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, tableName+"."+columnName),
		Name:          columnName,
		Kind:          SymbolKindColumn,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     signature,
		Language:      "sql",
		ParsedAtMilli: time.Now().UnixMilli(),
		Exported:      true,
		Metadata:      metadata,
	}
	result.Symbols = append(result.Symbols, sym)
}

// extractDataType extracts the data type string from a type node.
func (p *SQLParser) extractDataType(node *sitter.Node, content []byte) string {
	return string(content[node.StartByte():node.EndByte()])
}

// extractCreateIndex extracts index symbols from CREATE INDEX.
func (p *SQLParser) extractCreateIndex(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	indexName := ""
	tableName := ""
	isUnique := false
	var columns []string

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case sqlNodeKeywordUnique:
			isUnique = true
		case sqlNodeIdentifier:
			if indexName == "" {
				indexName = string(content[child.StartByte():child.EndByte()])
			}
		case sqlNodeObjectReference:
			tableName = p.extractIdentifier(child, content)
		case sqlNodeIndexFields:
			columns = p.extractIndexColumns(child, content)
		}
	}

	if indexName == "" {
		return
	}

	// Build signature
	signature := "CREATE "
	if isUnique {
		signature += "UNIQUE "
	}
	signature += "INDEX " + indexName
	if tableName != "" {
		signature += " ON " + tableName
	}
	if len(columns) > 0 {
		signature += "(" + strings.Join(columns, ", ") + ")"
	}

	sym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, indexName),
		Name:          indexName,
		Kind:          SymbolKindIndex,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     signature,
		Language:      "sql",
		ParsedAtMilli: time.Now().UnixMilli(),
		Exported:      true,
		Metadata: &SymbolMetadata{
			ParentName: tableName,
		},
	}
	result.Symbols = append(result.Symbols, sym)
}

// extractIndexColumns extracts column names from index_fields node.
func (p *SQLParser) extractIndexColumns(node *sitter.Node, content []byte) []string {
	var columns []string
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == sqlNodeField {
			for j := 0; j < int(child.ChildCount()); j++ {
				gc := child.Child(j)
				if gc.Type() == sqlNodeIdentifier {
					columns = append(columns, string(content[gc.StartByte():gc.EndByte()]))
				}
			}
		}
	}
	return columns
}

// extractCreateView extracts view symbols from CREATE VIEW.
func (p *SQLParser) extractCreateView(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	viewName := ""
	var queryNode *sitter.Node

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case sqlNodeObjectReference:
			if viewName == "" {
				viewName = p.extractIdentifier(child, content)
			}
		case sqlNodeCreateQuery:
			queryNode = child
		}
	}

	if viewName == "" {
		return
	}

	// Extract query definition for signature (truncate if too long)
	queryDef := ""
	if queryNode != nil {
		queryDef = string(content[queryNode.StartByte():queryNode.EndByte()])
		// Normalize whitespace
		queryDef = strings.Join(strings.Fields(queryDef), " ")
		if len(queryDef) > 100 {
			queryDef = queryDef[:100] + "..."
		}
	}

	signature := "CREATE VIEW " + viewName
	if queryDef != "" {
		signature += " AS " + queryDef
	}

	sym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, viewName),
		Name:          viewName,
		Kind:          SymbolKindView,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     signature,
		Language:      "sql",
		ParsedAtMilli: time.Now().UnixMilli(),
		Exported:      true,
	}
	result.Symbols = append(result.Symbols, sym)
}

// extractIdentifier extracts an identifier from an object_reference node.
func (p *SQLParser) extractIdentifier(node *sitter.Node, content []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == sqlNodeIdentifier {
			return string(content[child.StartByte():child.EndByte()])
		}
	}
	return ""
}
