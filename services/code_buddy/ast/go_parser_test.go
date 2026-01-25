package ast

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// Test source code samples (embedded, no file I/O).
const (
	testGoEmpty = ``

	testGoPackageOnly = `package example`

	testGoSimple = `package example

import (
	"context"
	"fmt"

	gin "github.com/gin-gonic/gin"
)

// Handler handles HTTP requests.
type Handler struct {
	db Database
}

// Database defines the data access interface.
type Database interface {
	Get(ctx context.Context, id string) (any, error)
}

// HandleGet handles GET requests.
func (h *Handler) HandleGet(ctx *gin.Context) {
	// implementation
}

// NewHandler creates a new Handler instance.
func NewHandler(db Database) *Handler {
	return &Handler{db: db}
}
`

	testGoFunction = `package example

// Add adds two integers.
func Add(a, b int) int {
	return a + b
}
`

	testGoMethod = `package example

type Calculator struct{}

// Add adds two integers.
func (c *Calculator) Add(a, b int) int {
	return a + b
}
`

	testGoInterface = `package example

// Reader defines read operations.
type Reader interface {
	Read(p []byte) (n int, err error)
	Close() error
}
`

	testGoStruct = `package example

// User represents a system user.
type User struct {
	ID        string
	Name      string
	Email     string
	createdAt int64
}
`

	testGoImports = `package example

import (
	"context"
	"fmt"
	"time"

	gin "github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo"
	_ "github.com/lib/pq"
)
`

	testGoSyntaxError = `package example

func Broken( {
	return
}

func Valid() string {
	return "hello"
}
`

	testGoVariablesConstants = `package example

var GlobalVar = "global"
var (
	MultiVar1 = "one"
	MultiVar2 = "two"
)

const MaxSize = 1024
const (
	StatusPending = "pending"
	StatusActive  = "active"
)
`

	testGoUnexported = `package example

type publicType struct{}
type privateType struct{}

func PublicFunc() {}
func privateFunc() {}

var PublicVar = 1
var privateVar = 2
`

	// Invalid UTF-8 bytes
	testInvalidUTF8 = "\xff\xfe"
)

func TestGoParser_Parse_EmptyFile(t *testing.T) {
	parser := NewGoParser()
	ctx := context.Background()

	result, err := parser.Parse(ctx, []byte(testGoEmpty), "empty.go")

	if err != nil {
		t.Fatalf("expected no error for empty file, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.FilePath != "empty.go" {
		t.Errorf("expected FilePath 'empty.go', got %q", result.FilePath)
	}
	if result.Language != "go" {
		t.Errorf("expected Language 'go', got %q", result.Language)
	}
	if result.Hash == "" {
		t.Error("expected non-empty hash")
	}
}

func TestGoParser_Parse_PackageOnly(t *testing.T) {
	parser := NewGoParser()
	ctx := context.Background()

	result, err := parser.Parse(ctx, []byte(testGoPackageOnly), "pkg.go")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have at least the package symbol
	packageSyms := filterByKind(result.Symbols, SymbolKindPackage)
	if len(packageSyms) != 1 {
		t.Errorf("expected 1 package symbol, got %d", len(packageSyms))
	}
	if len(packageSyms) > 0 && packageSyms[0].Name != "example" {
		t.Errorf("expected package name 'example', got %q", packageSyms[0].Name)
	}
}

func TestGoParser_Parse_Function(t *testing.T) {
	parser := NewGoParser()
	ctx := context.Background()

	result, err := parser.Parse(ctx, []byte(testGoFunction), "func.go")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	funcs := filterByKind(result.Symbols, SymbolKindFunction)
	if len(funcs) != 1 {
		t.Fatalf("expected 1 function, got %d", len(funcs))
	}

	fn := funcs[0]
	if fn.Name != "Add" {
		t.Errorf("expected function name 'Add', got %q", fn.Name)
	}
	if !fn.Exported {
		t.Error("expected function to be exported")
	}
	if fn.StartLine < 1 {
		t.Errorf("expected StartLine >= 1, got %d", fn.StartLine)
	}
	if fn.Signature == "" {
		t.Error("expected non-empty signature")
	}
}

func TestGoParser_Parse_Method(t *testing.T) {
	parser := NewGoParser()
	ctx := context.Background()

	result, err := parser.Parse(ctx, []byte(testGoMethod), "method.go")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	methods := filterByKind(result.Symbols, SymbolKindMethod)
	if len(methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(methods))
	}

	m := methods[0]
	if m.Name != "Add" {
		t.Errorf("expected method name 'Add', got %q", m.Name)
	}
	if !m.Exported {
		t.Error("expected method to be exported")
	}
	// Signature should contain receiver
	if !strings.Contains(m.Signature, "Calculator") {
		t.Errorf("expected signature to contain receiver, got %q", m.Signature)
	}
}

func TestGoParser_Parse_Interface(t *testing.T) {
	parser := NewGoParser()
	ctx := context.Background()

	result, err := parser.Parse(ctx, []byte(testGoInterface), "interface.go")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	interfaces := filterByKind(result.Symbols, SymbolKindInterface)
	if len(interfaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(interfaces))
	}

	iface := interfaces[0]
	if iface.Name != "Reader" {
		t.Errorf("expected interface name 'Reader', got %q", iface.Name)
	}
	if !iface.Exported {
		t.Error("expected interface to be exported")
	}
	if iface.DocComment == "" {
		t.Error("expected non-empty doc comment")
	}

	// Should have method children
	if len(iface.Children) != 2 {
		t.Errorf("expected 2 interface methods, got %d", len(iface.Children))
	}
}

func TestGoParser_Parse_Struct(t *testing.T) {
	parser := NewGoParser()
	ctx := context.Background()

	result, err := parser.Parse(ctx, []byte(testGoStruct), "struct.go")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	structs := filterByKind(result.Symbols, SymbolKindStruct)
	if len(structs) != 1 {
		t.Fatalf("expected 1 struct, got %d", len(structs))
	}

	s := structs[0]
	if s.Name != "User" {
		t.Errorf("expected struct name 'User', got %q", s.Name)
	}
	if !s.Exported {
		t.Error("expected struct to be exported")
	}

	// Should have field children (4 total, but createdAt is unexported)
	// With default options (IncludePrivate: true), should have 4
	if len(s.Children) != 4 {
		t.Errorf("expected 4 struct fields, got %d", len(s.Children))
	}

	// Check field names
	fieldNames := make([]string, len(s.Children))
	for i, c := range s.Children {
		fieldNames[i] = c.Name
	}
	expectedFields := []string{"ID", "Name", "Email", "createdAt"}
	for _, expected := range expectedFields {
		found := false
		for _, name := range fieldNames {
			if name == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected field %q not found in %v", expected, fieldNames)
		}
	}
}

func TestGoParser_Parse_Imports(t *testing.T) {
	parser := NewGoParser()
	ctx := context.Background()

	result, err := parser.Parse(ctx, []byte(testGoImports), "imports.go")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 6 imports
	if len(result.Imports) != 6 {
		t.Errorf("expected 6 imports, got %d", len(result.Imports))
	}

	// Check for aliased import
	foundAlias := false
	for _, imp := range result.Imports {
		if imp.Path == "github.com/gin-gonic/gin" && imp.Alias == "gin" {
			foundAlias = true
			break
		}
	}
	if !foundAlias {
		t.Error("expected to find gin alias import")
	}

	// Check for dot import
	foundDot := false
	for _, imp := range result.Imports {
		if imp.Alias == "." {
			foundDot = true
			break
		}
	}
	if !foundDot {
		t.Error("expected to find dot import")
	}

	// Check for blank import
	foundBlank := false
	for _, imp := range result.Imports {
		if imp.Alias == "_" {
			foundBlank = true
			break
		}
	}
	if !foundBlank {
		t.Error("expected to find blank import")
	}
}

func TestGoParser_Parse_SyntaxError(t *testing.T) {
	parser := NewGoParser()
	ctx := context.Background()

	result, err := parser.Parse(ctx, []byte(testGoSyntaxError), "broken.go")

	// Should NOT return error - should return partial result with errors
	if err != nil {
		t.Fatalf("expected no error for syntax errors (should be in result.Errors), got: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Should have errors reported
	if len(result.Errors) == 0 {
		t.Error("expected errors to be reported in result.Errors")
	}

	// Should still extract the valid function
	funcs := filterByKind(result.Symbols, SymbolKindFunction)
	validFound := false
	for _, fn := range funcs {
		if fn.Name == "Valid" {
			validFound = true
			break
		}
	}
	if !validFound {
		t.Error("expected to find 'Valid' function despite syntax errors")
	}
}

func TestGoParser_Parse_VariablesConstants(t *testing.T) {
	parser := NewGoParser()
	ctx := context.Background()

	result, err := parser.Parse(ctx, []byte(testGoVariablesConstants), "vars.go")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	vars := filterByKind(result.Symbols, SymbolKindVariable)
	if len(vars) != 3 {
		t.Errorf("expected 3 variables, got %d", len(vars))
	}

	consts := filterByKind(result.Symbols, SymbolKindConstant)
	if len(consts) != 3 {
		t.Errorf("expected 3 constants, got %d", len(consts))
	}
}

func TestGoParser_Parse_ContextCancellation(t *testing.T) {
	parser := NewGoParser()

	// Create already-canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := parser.Parse(ctx, []byte(testGoSimple), "test.go")

	if err == nil {
		t.Fatal("expected error for canceled context")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Errorf("expected 'canceled' in error, got: %v", err)
	}
}

func TestGoParser_Parse_FileTooLarge(t *testing.T) {
	// Create parser with small max size
	parser := NewGoParser(WithMaxFileSize(100))
	ctx := context.Background()

	// testGoSimple is larger than 100 bytes
	_, err := parser.Parse(ctx, []byte(testGoSimple), "large.go")

	if err == nil {
		t.Fatal("expected error for file too large")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("expected 'exceeds' in error, got: %v", err)
	}
}

func TestGoParser_Parse_InvalidUTF8(t *testing.T) {
	parser := NewGoParser()
	ctx := context.Background()

	_, err := parser.Parse(ctx, []byte(testInvalidUTF8), "invalid.go")

	if err == nil {
		t.Fatal("expected error for invalid UTF-8")
	}
	if !strings.Contains(err.Error(), "UTF-8") {
		t.Errorf("expected 'UTF-8' in error, got: %v", err)
	}
}

func TestGoParser_Parse_HashDeterministic(t *testing.T) {
	parser := NewGoParser()
	ctx := context.Background()

	content := []byte(testGoSimple)

	result1, err := parser.Parse(ctx, content, "test.go")
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}

	result2, err := parser.Parse(ctx, content, "test.go")
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}

	if result1.Hash != result2.Hash {
		t.Errorf("hash not deterministic: %q != %q", result1.Hash, result2.Hash)
	}

	// Hash should be 64 hex characters (SHA256)
	if len(result1.Hash) != 64 {
		t.Errorf("expected 64-char hex hash, got %d chars", len(result1.Hash))
	}
}

func TestGoParser_Parse_HashNonEmpty(t *testing.T) {
	parser := NewGoParser()
	ctx := context.Background()

	result, err := parser.Parse(ctx, []byte(testGoSimple), "test.go")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if result.Hash == "" {
		t.Error("expected non-empty hash")
	}

	// Verify it's valid hex
	for _, c := range result.Hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("hash contains non-hex character: %c", c)
			break
		}
	}
}

func TestGoParser_Parse_ResultValidates(t *testing.T) {
	parser := NewGoParser()
	ctx := context.Background()

	result, err := parser.Parse(ctx, []byte(testGoSimple), "test.go")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Result should already be validated, but double-check
	if err := result.Validate(); err != nil {
		t.Errorf("result failed validation: %v", err)
	}

	// All symbols should validate
	for _, sym := range result.Symbols {
		if err := sym.Validate(); err != nil {
			t.Errorf("symbol %q failed validation: %v", sym.Name, err)
		}
	}
}

func TestGoParser_Parse_Concurrent(t *testing.T) {
	parser := NewGoParser()
	ctx := context.Background()

	sources := []string{
		testGoFunction,
		testGoMethod,
		testGoStruct,
		testGoInterface,
		testGoImports,
	}

	var wg sync.WaitGroup
	errors := make(chan error, len(sources)*10)

	// Run 10 iterations of each source concurrently
	for i := 0; i < 10; i++ {
		for j, src := range sources {
			wg.Add(1)
			go func(idx int, source string) {
				defer wg.Done()

				result, err := parser.Parse(ctx, []byte(source), "test.go")
				if err != nil {
					errors <- err
					return
				}
				if result == nil {
					errors <- context.DeadlineExceeded // dummy error
					return
				}
			}(j, src)
		}
	}

	wg.Wait()
	close(errors)

	var errs []error
	for err := range errors {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		t.Errorf("concurrent parsing had %d errors: %v", len(errs), errs)
	}
}

func TestGoParser_Parse_FilterUnexported(t *testing.T) {
	// Create parser that excludes unexported symbols
	parser := NewGoParser(WithParseOptions(ParseOptions{
		IncludePrivate: false,
	}))
	ctx := context.Background()

	result, err := parser.Parse(ctx, []byte(testGoUnexported), "unexported.go")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Should only have exported symbols
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindPackage || sym.Kind == SymbolKindImport {
			continue // These are always "exported"
		}
		if !sym.Exported {
			t.Errorf("unexported symbol %q should be filtered out", sym.Name)
		}
	}

	// Should have PublicFunc but not privateFunc
	funcs := filterByKind(result.Symbols, SymbolKindFunction)
	for _, fn := range funcs {
		if fn.Name == "privateFunc" {
			t.Error("privateFunc should be filtered out")
		}
	}
}

func TestGoParser_Parse_ComplexFile(t *testing.T) {
	parser := NewGoParser()
	ctx := context.Background()

	result, err := parser.Parse(ctx, []byte(testGoSimple), "complex.go")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Verify we extracted expected symbols
	packages := filterByKind(result.Symbols, SymbolKindPackage)
	if len(packages) != 1 {
		t.Errorf("expected 1 package, got %d", len(packages))
	}

	structs := filterByKind(result.Symbols, SymbolKindStruct)
	if len(structs) != 1 {
		t.Errorf("expected 1 struct (Handler), got %d", len(structs))
	}

	interfaces := filterByKind(result.Symbols, SymbolKindInterface)
	if len(interfaces) != 1 {
		t.Errorf("expected 1 interface (Database), got %d", len(interfaces))
	}

	functions := filterByKind(result.Symbols, SymbolKindFunction)
	if len(functions) != 1 {
		t.Errorf("expected 1 function (NewHandler), got %d", len(functions))
	}

	methods := filterByKind(result.Symbols, SymbolKindMethod)
	if len(methods) >= 1 {
		// HandleGet method plus Database.Get interface method
		foundHandleGet := false
		for _, m := range methods {
			if m.Name == "HandleGet" {
				foundHandleGet = true
				break
			}
		}
		if !foundHandleGet {
			t.Error("expected to find HandleGet method")
		}
	}

	// Should have imports
	if len(result.Imports) < 3 {
		t.Errorf("expected at least 3 imports, got %d", len(result.Imports))
	}
}

func TestGoParser_Language(t *testing.T) {
	parser := NewGoParser()

	if parser.Language() != "go" {
		t.Errorf("expected language 'go', got %q", parser.Language())
	}
}

func TestGoParser_Extensions(t *testing.T) {
	parser := NewGoParser()

	exts := parser.Extensions()
	if len(exts) != 1 {
		t.Errorf("expected 1 extension, got %d", len(exts))
	}
	if exts[0] != ".go" {
		t.Errorf("expected extension '.go', got %q", exts[0])
	}
}

func TestGoParser_InterfaceCompliance(t *testing.T) {
	// Compile-time check that GoParser implements Parser
	var _ Parser = (*GoParser)(nil)
}

func TestGoParser_Parse_WithTimeout(t *testing.T) {
	parser := NewGoParser()

	// Create context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Give time for context to expire
	time.Sleep(1 * time.Millisecond)

	_, err := parser.Parse(ctx, []byte(testGoSimple), "test.go")

	if err == nil {
		t.Fatal("expected error for expired context")
	}
}

// Helper function to filter symbols by kind.
func filterByKind(symbols []*Symbol, kind SymbolKind) []*Symbol {
	result := make([]*Symbol, 0)
	for _, s := range symbols {
		if s.Kind == kind {
			result = append(result, s)
		}
	}
	return result
}
