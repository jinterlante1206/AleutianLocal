package ast

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSQLParser_Language(t *testing.T) {
	parser := NewSQLParser()
	if got := parser.Language(); got != "sql" {
		t.Errorf("Language() = %q, want %q", got, "sql")
	}
}

func TestSQLParser_Extensions(t *testing.T) {
	parser := NewSQLParser()
	exts := parser.Extensions()
	want := []string{".sql"}

	if len(exts) != len(want) {
		t.Fatalf("Extensions() returned %d items, want %d", len(exts), len(want))
	}

	for i, ext := range exts {
		if ext != want[i] {
			t.Errorf("Extensions()[%d] = %q, want %q", i, ext, want[i])
		}
	}
}

func TestSQLParser_Parse_CreateTable(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	content := []byte(`
CREATE TABLE users (
    id INT PRIMARY KEY,
    username VARCHAR(255) NOT NULL,
    email VARCHAR(255) UNIQUE
);
`)

	result, err := parser.Parse(ctx, content, "schema.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Find table symbol
	var table *Symbol
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindTable {
			table = sym
			break
		}
	}

	if table == nil {
		t.Fatal("expected to find table symbol")
	}
	if table.Name != "users" {
		t.Errorf("table name = %q, want %q", table.Name, "users")
	}
	if !strings.Contains(table.Signature, "CREATE TABLE users") {
		t.Errorf("table signature should contain 'CREATE TABLE users': %s", table.Signature)
	}
}

func TestSQLParser_Parse_CreateTableWithColumns(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	content := []byte(`
CREATE TABLE products (
    id INT PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    price DECIMAL(10,2)
);
`)

	result, err := parser.Parse(ctx, content, "schema.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Count symbols by kind
	tables := 0
	columns := 0
	for _, sym := range result.Symbols {
		switch sym.Kind {
		case SymbolKindTable:
			tables++
		case SymbolKindColumn:
			columns++
		}
	}

	if tables != 1 {
		t.Errorf("got %d tables, want 1", tables)
	}
	if columns != 3 {
		t.Errorf("got %d columns, want 3", columns)
	}
}

func TestSQLParser_Parse_ColumnConstraints(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	content := []byte(`
CREATE TABLE test (
    id INT PRIMARY KEY,
    email VARCHAR(255) UNIQUE,
    name VARCHAR(100) NOT NULL
);
`)

	result, err := parser.Parse(ctx, content, "schema.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Check column constraints
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindColumn {
			switch sym.Name {
			case "id":
				if !strings.Contains(sym.Signature, "PRIMARY KEY") {
					t.Errorf("id column signature should contain PRIMARY KEY: %s", sym.Signature)
				}
			case "email":
				if !strings.Contains(sym.Signature, "UNIQUE") {
					t.Errorf("email column signature should contain UNIQUE: %s", sym.Signature)
				}
			case "name":
				if !strings.Contains(sym.Signature, "NOT NULL") {
					t.Errorf("name column signature should contain NOT NULL: %s", sym.Signature)
				}
			}
		}
	}
}

func TestSQLParser_Parse_ColumnMetadata(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	content := []byte(`
CREATE TABLE users (
    id INT PRIMARY KEY
);
`)

	result, err := parser.Parse(ctx, content, "schema.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Find id column
	var column *Symbol
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindColumn && sym.Name == "id" {
			column = sym
			break
		}
	}

	if column == nil {
		t.Fatal("expected to find id column")
	}
	if column.Metadata == nil {
		t.Fatal("expected column to have metadata")
	}
	if column.Metadata.ParentName != "users" {
		t.Errorf("column ParentName = %q, want %q", column.Metadata.ParentName, "users")
	}
}

func TestSQLParser_Parse_CreateIndex(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	content := []byte(`
CREATE INDEX idx_users_email ON users(email);
`)

	result, err := parser.Parse(ctx, content, "schema.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	var index *Symbol
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindIndex {
			index = sym
			break
		}
	}

	if index == nil {
		t.Fatal("expected to find index symbol")
	}
	if index.Name != "idx_users_email" {
		t.Errorf("index name = %q, want %q", index.Name, "idx_users_email")
	}
	if !strings.Contains(index.Signature, "ON users") {
		t.Errorf("index signature should contain 'ON users': %s", index.Signature)
	}
}

func TestSQLParser_Parse_CreateUniqueIndex(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	content := []byte(`
CREATE UNIQUE INDEX idx_email ON users(email);
`)

	result, err := parser.Parse(ctx, content, "schema.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	var index *Symbol
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindIndex {
			index = sym
			break
		}
	}

	if index == nil {
		t.Fatal("expected to find index symbol")
	}
	if !strings.Contains(index.Signature, "UNIQUE") {
		t.Errorf("index signature should contain 'UNIQUE': %s", index.Signature)
	}
}

func TestSQLParser_Parse_CreateView(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	content := []byte(`
CREATE VIEW active_users AS
SELECT * FROM users WHERE active = true;
`)

	result, err := parser.Parse(ctx, content, "schema.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	var view *Symbol
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindView {
			view = sym
			break
		}
	}

	if view == nil {
		t.Fatal("expected to find view symbol")
	}
	if view.Name != "active_users" {
		t.Errorf("view name = %q, want %q", view.Name, "active_users")
	}
	if !strings.Contains(view.Signature, "CREATE VIEW active_users") {
		t.Errorf("view signature should contain view name: %s", view.Signature)
	}
}

func TestSQLParser_Parse_MultipleStatements(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	content := []byte(`
CREATE TABLE users (
    id INT PRIMARY KEY,
    name VARCHAR(100)
);

CREATE TABLE orders (
    id INT PRIMARY KEY,
    user_id INT
);

CREATE INDEX idx_orders_user ON orders(user_id);

CREATE VIEW user_orders AS
SELECT u.name, o.id FROM users u JOIN orders o ON u.id = o.user_id;
`)

	result, err := parser.Parse(ctx, content, "schema.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Count symbols by kind
	tables := 0
	columns := 0
	indexes := 0
	views := 0
	for _, sym := range result.Symbols {
		switch sym.Kind {
		case SymbolKindTable:
			tables++
		case SymbolKindColumn:
			columns++
		case SymbolKindIndex:
			indexes++
		case SymbolKindView:
			views++
		}
	}

	if tables != 2 {
		t.Errorf("got %d tables, want 2", tables)
	}
	if columns != 4 {
		t.Errorf("got %d columns, want 4", columns)
	}
	if indexes != 1 {
		t.Errorf("got %d indexes, want 1", indexes)
	}
	if views != 1 {
		t.Errorf("got %d views, want 1", views)
	}
}

func TestSQLParser_Parse_EmptyContent(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	result, err := parser.Parse(ctx, []byte(""), "empty.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(result.Symbols) != 0 {
		t.Errorf("got %d symbols, want 0", len(result.Symbols))
	}
	if result.Language != "sql" {
		t.Errorf("Language = %q, want %q", result.Language, "sql")
	}
}

func TestSQLParser_Parse_CommentsOnly(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	content := []byte(`
-- This is a comment
-- Another comment
`)

	result, err := parser.Parse(ctx, content, "comments.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Comments are not extracted as symbols
	if len(result.Symbols) != 0 {
		t.Errorf("got %d symbols, want 0 (comments not extracted)", len(result.Symbols))
	}
}

func TestSQLParser_Parse_ContextCancellation(t *testing.T) {
	parser := NewSQLParser()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	content := []byte(`CREATE TABLE test (id INT);`)

	_, err := parser.Parse(ctx, content, "test.sql")
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Errorf("error should mention cancellation: %v", err)
	}
}

func TestSQLParser_Parse_FileTooLarge(t *testing.T) {
	parser := NewSQLParser(WithSQLMaxFileSize(100))
	ctx := context.Background()

	content := make([]byte, 200)
	for i := range content {
		content[i] = '-'
	}

	_, err := parser.Parse(ctx, content, "large.sql")
	if err != ErrFileTooLarge {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestSQLParser_Parse_InvalidUTF8(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	content := []byte{0xff, 0xfe, 0x00, 0x01}

	_, err := parser.Parse(ctx, content, "invalid.sql")
	if err != ErrInvalidContent {
		t.Errorf("expected ErrInvalidContent, got %v", err)
	}
}

func TestSQLParser_Parse_HashComputation(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	content := []byte(`CREATE TABLE test (id INT);`)

	result, err := parser.Parse(ctx, content, "test.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if result.Hash == "" {
		t.Error("Hash should not be empty")
	}
	if len(result.Hash) != 64 {
		t.Errorf("Hash length = %d, want 64 (SHA-256 hex)", len(result.Hash))
	}
}

func TestSQLParser_Parse_TimestampSet(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	before := time.Now().UnixMilli()
	result, err := parser.Parse(ctx, []byte(`SELECT 1;`), "test.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	after := time.Now().UnixMilli()

	if result.ParsedAtMilli < before || result.ParsedAtMilli > after {
		t.Errorf("ParsedAtMilli = %d, want between %d and %d", result.ParsedAtMilli, before, after)
	}
}

func TestSQLParser_Parse_LineNumbers(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	content := []byte(`-- Comment
-- Another comment
CREATE TABLE users (
    id INT PRIMARY KEY
);
`)

	result, err := parser.Parse(ctx, content, "test.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	var table *Symbol
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindTable {
			table = sym
			break
		}
	}

	if table == nil {
		t.Fatal("expected to find table")
	}
	if table.StartLine != 3 {
		t.Errorf("table StartLine = %d, want 3", table.StartLine)
	}
}

func TestSQLParser_Parse_SymbolExported(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	content := []byte(`CREATE TABLE public_table (id INT);`)

	result, err := parser.Parse(ctx, content, "test.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	for _, sym := range result.Symbols {
		if !sym.Exported {
			t.Errorf("symbol %q should be exported", sym.Name)
		}
	}
}

func TestSQLParser_Parse_FilePath(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	content := []byte(`CREATE TABLE test (id INT);`)
	filePath := "migrations/001_init.sql"

	result, err := parser.Parse(ctx, content, filePath)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if result.FilePath != filePath {
		t.Errorf("FilePath = %q, want %q", result.FilePath, filePath)
	}

	for _, sym := range result.Symbols {
		if sym.FilePath != filePath {
			t.Errorf("symbol FilePath = %q, want %q", sym.FilePath, filePath)
		}
	}
}

func TestSQLParser_Parse_SymbolLanguage(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	content := []byte(`CREATE TABLE test (id INT);`)

	result, err := parser.Parse(ctx, content, "test.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	for _, sym := range result.Symbols {
		if sym.Language != "sql" {
			t.Errorf("symbol Language = %q, want %q", sym.Language, "sql")
		}
	}
}

func TestSQLParser_ConcurrentParsing(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	contents := [][]byte{
		[]byte(`CREATE TABLE t1 (id INT);`),
		[]byte(`CREATE TABLE t2 (id INT);`),
		[]byte(`CREATE TABLE t3 (id INT);`),
		[]byte(`CREATE INDEX idx ON t1(id);`),
		[]byte(`CREATE VIEW v1 AS SELECT 1;`),
	}

	results := make(chan error, len(contents))

	for i, content := range contents {
		go func(idx int, c []byte) {
			_, err := parser.Parse(ctx, c, "concurrent.sql")
			results <- err
		}(i, content)
	}

	for range contents {
		if err := <-results; err != nil {
			t.Errorf("concurrent parse error: %v", err)
		}
	}
}

func TestSQLParser_Parse_ExtractColumnsDisabled(t *testing.T) {
	parser := NewSQLParser(WithSQLExtractColumns(false))
	ctx := context.Background()

	content := []byte(`
CREATE TABLE users (
    id INT PRIMARY KEY,
    name VARCHAR(100)
);
`)

	result, err := parser.Parse(ctx, content, "schema.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Should have table but no columns
	tables := 0
	columns := 0
	for _, sym := range result.Symbols {
		switch sym.Kind {
		case SymbolKindTable:
			tables++
		case SymbolKindColumn:
			columns++
		}
	}

	if tables != 1 {
		t.Errorf("got %d tables, want 1", tables)
	}
	if columns != 0 {
		t.Errorf("got %d columns, want 0 (extraction disabled)", columns)
	}
}

func TestSQLParser_Parse_IndexWithMultipleColumns(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	content := []byte(`
CREATE INDEX idx_user_order ON orders(user_id, created_at);
`)

	result, err := parser.Parse(ctx, content, "schema.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	var index *Symbol
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindIndex {
			index = sym
			break
		}
	}

	if index == nil {
		t.Fatal("expected to find index symbol")
	}
	// The signature should include both columns
	if !strings.Contains(index.Signature, "user_id") || !strings.Contains(index.Signature, "created_at") {
		t.Errorf("index signature should contain both columns: %s", index.Signature)
	}
}

func TestSQLParser_Parse_IndexTableMetadata(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	content := []byte(`
CREATE INDEX idx_email ON users(email);
`)

	result, err := parser.Parse(ctx, content, "schema.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	var index *Symbol
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindIndex {
			index = sym
			break
		}
	}

	if index == nil {
		t.Fatal("expected to find index symbol")
	}
	if index.Metadata == nil {
		t.Fatal("expected index to have metadata")
	}
	if index.Metadata.ParentName != "users" {
		t.Errorf("index ParentName = %q, want %q", index.Metadata.ParentName, "users")
	}
}

func TestSQLParser_DefaultOptions(t *testing.T) {
	opts := DefaultSQLParserOptions()

	if opts.MaxFileSize != 10*1024*1024 {
		t.Errorf("MaxFileSize = %d, want %d", opts.MaxFileSize, 10*1024*1024)
	}
	if !opts.ExtractColumns {
		t.Error("ExtractColumns should be true by default")
	}
}

func TestSQLParser_Parse_DataTypes(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	content := []byte(`
CREATE TABLE datatypes (
    int_col INT,
    varchar_col VARCHAR(255),
    decimal_col DECIMAL(10,2),
    timestamp_col TIMESTAMP,
    date_col DATE,
    bool_col BOOL,
    text_col TEXT
);
`)

	result, err := parser.Parse(ctx, content, "schema.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Verify each column has data type in signature
	columnSignatures := make(map[string]string)
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindColumn {
			columnSignatures[sym.Name] = sym.Signature
		}
	}

	tests := []struct {
		column   string
		dataType string
	}{
		{"int_col", "INT"},
		{"varchar_col", "VARCHAR"},
		{"decimal_col", "DECIMAL"},
		{"timestamp_col", "TIMESTAMP"},
		{"date_col", "DATE"},
		{"bool_col", "BOOL"},
		{"text_col", "TEXT"},
	}

	for _, tt := range tests {
		sig, ok := columnSignatures[tt.column]
		if !ok {
			t.Errorf("missing column %s", tt.column)
			continue
		}
		if !strings.Contains(strings.ToUpper(sig), tt.dataType) {
			t.Errorf("column %s signature should contain %s: %s", tt.column, tt.dataType, sig)
		}
	}
}

func TestSQLParser_Parse_ViewQueryTruncation(t *testing.T) {
	parser := NewSQLParser()
	ctx := context.Background()

	// Create a view with a long query
	longQuery := "SELECT " + strings.Repeat("column_name, ", 50) + "id FROM very_long_table_name WHERE condition = true"
	content := []byte("CREATE VIEW long_view AS " + longQuery + ";")

	result, err := parser.Parse(ctx, content, "schema.sql")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	var view *Symbol
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindView {
			view = sym
			break
		}
	}

	if view == nil {
		t.Fatal("expected to find view symbol")
	}
	// Signature should be truncated for readability
	if len(view.Signature) > 200 {
		t.Errorf("view signature too long (%d chars), should be truncated", len(view.Signature))
	}
}
