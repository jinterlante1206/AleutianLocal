package ast

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCSSParser_Parse_EmptyFile(t *testing.T) {
	parser := NewCSSParser()
	result, err := parser.Parse(context.Background(), []byte(""), "empty.css")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.Language != "css" {
		t.Errorf("expected language 'css', got %q", result.Language)
	}
	if result.FilePath != "empty.css" {
		t.Errorf("expected filePath 'empty.css', got %q", result.FilePath)
	}
	if result.Hash == "" {
		t.Error("expected hash to be set")
	}
}

func TestCSSParser_Parse_ClassSelector(t *testing.T) {
	parser := NewCSSParser()
	content := `
.button {
    padding: 8px;
}
`
	result, err := parser.Parse(context.Background(), []byte(content), "styles.css")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var class *Symbol
	for _, sym := range result.Symbols {
		if sym.Name == "button" && sym.Kind == SymbolKindCSSClass {
			class = sym
			break
		}
	}

	if class == nil {
		t.Fatal("expected to find class 'button'")
	}
	if class.Signature != ".button" {
		t.Errorf("expected signature '.button', got %q", class.Signature)
	}
}

func TestCSSParser_Parse_IDSelector(t *testing.T) {
	parser := NewCSSParser()
	content := `
#main-header {
    background: blue;
}
`
	result, err := parser.Parse(context.Background(), []byte(content), "styles.css")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var id *Symbol
	for _, sym := range result.Symbols {
		if sym.Name == "main-header" && sym.Kind == SymbolKindCSSID {
			id = sym
			break
		}
	}

	if id == nil {
		t.Fatal("expected to find ID 'main-header'")
	}
	if id.Signature != "#main-header" {
		t.Errorf("expected signature '#main-header', got %q", id.Signature)
	}
}

func TestCSSParser_Parse_CSSVariable(t *testing.T) {
	parser := NewCSSParser()
	content := `
:root {
    --primary-color: #007bff;
    --spacing: 8px;
}
`
	result, err := parser.Parse(context.Background(), []byte(content), "styles.css")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var primary, spacing *Symbol
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindCSSVariable {
			switch sym.Name {
			case "--primary-color":
				primary = sym
			case "--spacing":
				spacing = sym
			}
		}
	}

	if primary == nil {
		t.Error("expected to find variable '--primary-color'")
	} else if !strings.Contains(primary.Signature, "--primary-color") {
		t.Errorf("expected signature to contain '--primary-color', got %q", primary.Signature)
	}

	if spacing == nil {
		t.Error("expected to find variable '--spacing'")
	}
}

func TestCSSParser_Parse_Keyframes(t *testing.T) {
	parser := NewCSSParser()
	content := `
@keyframes fadeIn {
    from { opacity: 0; }
    to { opacity: 1; }
}
`
	result, err := parser.Parse(context.Background(), []byte(content), "animations.css")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var animation *Symbol
	for _, sym := range result.Symbols {
		if sym.Name == "fadeIn" && sym.Kind == SymbolKindAnimation {
			animation = sym
			break
		}
	}

	if animation == nil {
		t.Fatal("expected to find animation 'fadeIn'")
	}
	if animation.Signature != "@keyframes fadeIn" {
		t.Errorf("expected signature '@keyframes fadeIn', got %q", animation.Signature)
	}
}

func TestCSSParser_Parse_MediaQuery(t *testing.T) {
	parser := NewCSSParser()
	content := `
@media (max-width: 768px) {
    .mobile-only {
        display: block;
    }
}
`
	result, err := parser.Parse(context.Background(), []byte(content), "responsive.css")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var mediaQuery *Symbol
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindMediaQuery {
			mediaQuery = sym
			break
		}
	}

	if mediaQuery == nil {
		t.Fatal("expected to find media query")
	}
	if !strings.Contains(mediaQuery.Name, "max-width") {
		t.Errorf("expected media query name to contain 'max-width', got %q", mediaQuery.Name)
	}

	// Check that nested class is also extracted
	var mobileClass *Symbol
	for _, sym := range result.Symbols {
		if sym.Name == "mobile-only" && sym.Kind == SymbolKindCSSClass {
			mobileClass = sym
			break
		}
	}

	if mobileClass == nil {
		t.Error("expected to find nested class 'mobile-only'")
	}
}

func TestCSSParser_Parse_Import(t *testing.T) {
	parser := NewCSSParser()
	content := `
@import url('reset.css');
@import 'variables.css';
`
	result, err := parser.Parse(context.Background(), []byte(content), "main.css")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Imports) < 2 {
		t.Fatalf("expected at least 2 imports, got %d", len(result.Imports))
	}

	// Check first import (url())
	found1 := false
	found2 := false
	for _, imp := range result.Imports {
		if imp.Path == "reset.css" {
			found1 = true
			if !imp.IsStylesheet {
				t.Error("expected IsStylesheet to be true")
			}
		}
		if imp.Path == "variables.css" {
			found2 = true
		}
	}

	if !found1 {
		t.Error("expected to find import 'reset.css'")
	}
	if !found2 {
		t.Error("expected to find import 'variables.css'")
	}
}

func TestCSSParser_Parse_ImportWithMedia(t *testing.T) {
	parser := NewCSSParser()
	content := `@import 'print.css' print;`

	result, err := parser.Parse(context.Background(), []byte(content), "main.css")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Imports) == 0 {
		t.Fatal("expected at least one import")
	}

	imp := result.Imports[0]
	if imp.Path != "print.css" {
		t.Errorf("expected path 'print.css', got %q", imp.Path)
	}
	if imp.MediaQuery != "print" {
		t.Errorf("expected media query 'print', got %q", imp.MediaQuery)
	}
}

func TestCSSParser_Parse_MultipleClasses(t *testing.T) {
	parser := NewCSSParser()
	content := `
.button { padding: 8px; }
.button-primary { background: blue; }
.button-secondary { background: gray; }
.card { border: 1px solid; }
.card-header { font-weight: bold; }
`
	result, err := parser.Parse(context.Background(), []byte(content), "components.css")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedClasses := []string{"button", "button-primary", "button-secondary", "card", "card-header"}
	foundClasses := make(map[string]bool)

	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindCSSClass {
			foundClasses[sym.Name] = true
		}
	}

	for _, expected := range expectedClasses {
		if !foundClasses[expected] {
			t.Errorf("expected to find class %q", expected)
		}
	}
}

func TestCSSParser_Parse_ComplexSelectors(t *testing.T) {
	parser := NewCSSParser()
	content := `
.container .inner { padding: 8px; }
.nav > .item { margin: 4px; }
.btn.active { color: blue; }
`
	result, err := parser.Parse(context.Background(), []byte(content), "complex.css")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should extract all class names from complex selectors
	expectedClasses := []string{"container", "inner", "nav", "item", "btn", "active"}
	foundClasses := make(map[string]bool)

	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindCSSClass {
			foundClasses[sym.Name] = true
		}
	}

	for _, expected := range expectedClasses {
		if !foundClasses[expected] {
			t.Errorf("expected to find class %q", expected)
		}
	}
}

func TestCSSParser_Parse_FileTooLarge(t *testing.T) {
	parser := NewCSSParser(WithCSSMaxFileSize(100))
	content := make([]byte, 200)
	for i := range content {
		content[i] = ' '
	}

	_, err := parser.Parse(context.Background(), content, "large.css")
	if err != ErrFileTooLarge {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestCSSParser_Parse_InvalidUTF8(t *testing.T) {
	parser := NewCSSParser()
	content := []byte{0xff, 0xfe, 0x00, 0x01}

	_, err := parser.Parse(context.Background(), content, "invalid.css")
	if err != ErrInvalidContent {
		t.Errorf("expected ErrInvalidContent, got %v", err)
	}
}

func TestCSSParser_Parse_ContextCancellation(t *testing.T) {
	parser := NewCSSParser()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := parser.Parse(ctx, []byte(".test {}"), "test.css")
	if err == nil {
		t.Error("expected error due to cancelled context")
	}
}

func TestCSSParser_Parse_Hash(t *testing.T) {
	parser := NewCSSParser()
	content := []byte(".test { color: red; }")

	result1, _ := parser.Parse(context.Background(), content, "test.css")
	result2, _ := parser.Parse(context.Background(), content, "test.css")

	if result1.Hash == "" {
		t.Error("expected hash to be set")
	}
	if result1.Hash != result2.Hash {
		t.Error("expected same content to produce same hash")
	}

	result3, _ := parser.Parse(context.Background(), []byte(".other {}"), "test.css")
	if result1.Hash == result3.Hash {
		t.Error("expected different content to produce different hash")
	}
}

func TestCSSParser_Parse_Concurrent(t *testing.T) {
	parser := NewCSSParser()
	contents := []string{
		".a { color: red; }",
		".b { color: blue; }",
		".c { color: green; }",
		"#x { background: white; }",
		"#y { background: black; }",
	}

	var wg sync.WaitGroup
	errors := make(chan error, len(contents))

	for i, content := range contents {
		wg.Add(1)
		go func(idx int, c string) {
			defer wg.Done()
			_, err := parser.Parse(context.Background(), []byte(c), "test.css")
			if err != nil {
				errors <- err
			}
		}(i, content)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent parse error: %v", err)
	}
}

func TestCSSParser_Parse_Timeout(t *testing.T) {
	parser := NewCSSParser()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	content := []byte(".test { color: red; }")
	_, _ = parser.Parse(ctx, content, "test.css")
}

func TestCSSParser_Language(t *testing.T) {
	parser := NewCSSParser()
	if parser.Language() != "css" {
		t.Errorf("expected 'css', got %q", parser.Language())
	}
}

func TestCSSParser_Extensions(t *testing.T) {
	parser := NewCSSParser()
	extensions := parser.Extensions()

	if len(extensions) != 1 || extensions[0] != ".css" {
		t.Errorf("expected ['.css'], got %v", extensions)
	}
}

func TestCSSParser_Parse_ComprehensiveExample(t *testing.T) {
	parser := NewCSSParser()
	content := `/* Main styles */
@import url('reset.css');
@import 'variables.css';
@import 'print.css' print;

:root {
    --primary-color: #007bff;
    --secondary-color: #6c757d;
    --spacing-unit: 8px;
}

#main-header {
    background: var(--primary-color);
}

#main-footer {
    background: var(--secondary-color);
}

.button {
    padding: var(--spacing-unit);
}

.button-primary {
    background: var(--primary-color);
}

.card {
    border: 1px solid #ddd;
}

@keyframes fadeIn {
    from { opacity: 0; }
    to { opacity: 1; }
}

@keyframes slideIn {
    from { transform: translateX(-100%); }
    to { transform: translateX(0); }
}

@media (max-width: 768px) {
    .mobile-only {
        display: block;
    }
    .desktop-only {
        display: none;
    }
}

@media (prefers-color-scheme: dark) {
    :root {
        --primary-color: #4dabf7;
    }
}
`
	result, err := parser.Parse(context.Background(), []byte(content), "main.css")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check imports
	if len(result.Imports) < 3 {
		t.Errorf("expected at least 3 imports, got %d", len(result.Imports))
	}

	// Check CSS variables
	varCount := 0
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindCSSVariable {
			varCount++
		}
	}
	if varCount < 3 {
		t.Errorf("expected at least 3 CSS variables, got %d", varCount)
	}

	// Check ID selectors
	idCount := 0
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindCSSID {
			idCount++
		}
	}
	if idCount < 2 {
		t.Errorf("expected at least 2 ID selectors, got %d", idCount)
	}

	// Check class selectors
	classCount := 0
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindCSSClass {
			classCount++
		}
	}
	if classCount < 5 {
		t.Errorf("expected at least 5 class selectors, got %d", classCount)
	}

	// Check animations
	animCount := 0
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindAnimation {
			animCount++
		}
	}
	if animCount < 2 {
		t.Errorf("expected at least 2 animations, got %d", animCount)
	}

	// Check media queries
	mediaCount := 0
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindMediaQuery {
			mediaCount++
		}
	}
	if mediaCount < 2 {
		t.Errorf("expected at least 2 media queries, got %d", mediaCount)
	}
}
