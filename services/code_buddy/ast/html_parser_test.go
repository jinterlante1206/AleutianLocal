package ast

import (
	"context"
	"strings"
	"testing"
	"time"
)

// filterSymbolsByKind is a test helper that filters symbols by their kind.
func filterSymbolsByKind(symbols []*Symbol, kind SymbolKind) []*Symbol {
	var result []*Symbol
	for _, sym := range symbols {
		if sym.Kind == kind {
			result = append(result, sym)
		}
	}
	return result
}

func TestHTMLParser_Language(t *testing.T) {
	parser := NewHTMLParser()
	if got := parser.Language(); got != "html" {
		t.Errorf("Language() = %q, want %q", got, "html")
	}
}

func TestHTMLParser_Extensions(t *testing.T) {
	parser := NewHTMLParser()
	exts := parser.Extensions()
	want := []string{".html", ".htm"}

	if len(exts) != len(want) {
		t.Fatalf("Extensions() returned %d items, want %d", len(exts), len(want))
	}

	for i, ext := range exts {
		if ext != want[i] {
			t.Errorf("Extensions()[%d] = %q, want %q", i, ext, want[i])
		}
	}
}

func TestHTMLParser_Parse_ElementWithID(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<!DOCTYPE html>
<html>
<body>
    <div id="main-content">Hello</div>
    <header id="site-header">
        <nav id="navigation">Links</nav>
    </header>
</body>
</html>`)

	result, err := parser.Parse(ctx, content, "index.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Find elements with IDs
	elements := filterSymbolsByKind(result.Symbols, SymbolKindElement)
	if len(elements) != 3 {
		t.Fatalf("got %d elements with IDs, want 3", len(elements))
	}

	ids := make(map[string]bool)
	for _, sym := range elements {
		ids[sym.Name] = true
	}

	wantIDs := []string{"main-content", "site-header", "navigation"}
	for _, id := range wantIDs {
		if !ids[id] {
			t.Errorf("missing element with id=%q", id)
		}
	}
}

func TestHTMLParser_Parse_FormWithName(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<form name="login-form" action="/login">
    <input type="text" name="username">
</form>
<form name="search-form" action="/search">
    <input type="search" name="q">
</form>`)

	result, err := parser.Parse(ctx, content, "forms.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	forms := filterSymbolsByKind(result.Symbols, SymbolKindForm)
	if len(forms) != 2 {
		t.Fatalf("got %d forms, want 2", len(forms))
	}

	names := make(map[string]bool)
	for _, sym := range forms {
		names[sym.Name] = true
		if !strings.Contains(sym.Signature, "form name=") {
			t.Errorf("form signature missing name: %s", sym.Signature)
		}
	}

	if !names["login-form"] {
		t.Error("missing form with name=login-form")
	}
	if !names["search-form"] {
		t.Error("missing form with name=search-form")
	}
}

func TestHTMLParser_Parse_CustomElement(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<html>
<body>
    <nav-menu></nav-menu>
    <user-profile id="profile"></user-profile>
    <my-custom-button></my-custom-button>
</body>
</html>`)

	result, err := parser.Parse(ctx, content, "components.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	components := filterSymbolsByKind(result.Symbols, SymbolKindComponent)
	if len(components) != 3 {
		t.Fatalf("got %d custom elements, want 3", len(components))
	}

	names := make(map[string]bool)
	for _, sym := range components {
		names[sym.Name] = true
	}

	wantNames := []string{"nav-menu", "user-profile", "my-custom-button"}
	for _, name := range wantNames {
		if !names[name] {
			t.Errorf("missing custom element %q", name)
		}
	}
}

func TestHTMLParser_Parse_ExternalScript(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<!DOCTYPE html>
<html>
<head>
    <script src="app.js"></script>
    <script src="vendor/react.js"></script>
</head>
</html>`)

	result, err := parser.Parse(ctx, content, "index.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Check imports
	if len(result.Imports) != 2 {
		t.Fatalf("got %d imports, want 2", len(result.Imports))
	}

	paths := make(map[string]bool)
	for _, imp := range result.Imports {
		paths[imp.Path] = true
		if !imp.IsScript {
			t.Errorf("import %q should have IsScript=true", imp.Path)
		}
	}

	if !paths["app.js"] {
		t.Error("missing import for app.js")
	}
	if !paths["vendor/react.js"] {
		t.Error("missing import for vendor/react.js")
	}
}

func TestHTMLParser_Parse_ModuleScript(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<script type="module" src="main.mjs"></script>
<script src="legacy.js"></script>`)

	result, err := parser.Parse(ctx, content, "index.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(result.Imports) != 2 {
		t.Fatalf("got %d imports, want 2", len(result.Imports))
	}

	var moduleImport, legacyImport *Import
	for i := range result.Imports {
		if result.Imports[i].Path == "main.mjs" {
			moduleImport = &result.Imports[i]
		} else if result.Imports[i].Path == "legacy.js" {
			legacyImport = &result.Imports[i]
		}
	}

	if moduleImport == nil {
		t.Fatal("missing module import")
	}
	if !moduleImport.IsModule {
		t.Error("main.mjs should have IsModule=true")
	}

	if legacyImport == nil {
		t.Fatal("missing legacy import")
	}
	if legacyImport.IsModule {
		t.Error("legacy.js should have IsModule=false")
	}
}

func TestHTMLParser_Parse_Stylesheet(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<!DOCTYPE html>
<html>
<head>
    <link rel="stylesheet" href="styles.css">
    <link rel="stylesheet" href="vendor/bootstrap.css">
    <link rel="icon" href="favicon.ico">
</head>
</html>`)

	result, err := parser.Parse(ctx, content, "index.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Should only import stylesheets, not favicon
	stylesheets := make([]Import, 0)
	for _, imp := range result.Imports {
		if imp.IsStylesheet {
			stylesheets = append(stylesheets, imp)
		}
	}

	if len(stylesheets) != 2 {
		t.Fatalf("got %d stylesheets, want 2", len(stylesheets))
	}

	paths := make(map[string]bool)
	for _, ss := range stylesheets {
		paths[ss.Path] = true
	}

	if !paths["styles.css"] {
		t.Error("missing stylesheet styles.css")
	}
	if !paths["vendor/bootstrap.css"] {
		t.Error("missing stylesheet vendor/bootstrap.css")
	}
}

func TestHTMLParser_Parse_InlineScript(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<!DOCTYPE html>
<html>
<body>
    <script>
        function init() {
            console.log('Hello');
        }
        const config = { debug: true };
    </script>
</body>
</html>`)

	result, err := parser.Parse(ctx, content, "index.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Should find function and constant from inline script
	funcs := filterSymbolsByKind(result.Symbols, SymbolKindFunction)
	consts := filterSymbolsByKind(result.Symbols, SymbolKindConstant)

	if len(funcs) != 1 {
		t.Errorf("got %d functions, want 1", len(funcs))
	} else if funcs[0].Name != "init" {
		t.Errorf("function name = %q, want %q", funcs[0].Name, "init")
	}

	if len(consts) != 1 {
		t.Errorf("got %d constants, want 1", len(consts))
	} else if consts[0].Name != "config" {
		t.Errorf("constant name = %q, want %q", consts[0].Name, "config")
	}
}

func TestHTMLParser_Parse_InlineStyle(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<!DOCTYPE html>
<html>
<head>
    <style>
        .container {
            max-width: 1200px;
        }
        #header {
            background: blue;
        }
    </style>
</head>
</html>`)

	result, err := parser.Parse(ctx, content, "index.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Should find CSS class and ID selectors from inline style
	// CSS classes use SymbolKindCSSClass, IDs use SymbolKindCSSID
	classes := filterSymbolsByKind(result.Symbols, SymbolKindCSSClass)
	ids := filterSymbolsByKind(result.Symbols, SymbolKindCSSID)

	if len(classes) != 1 {
		t.Errorf("got %d CSS classes, want 1", len(classes))
	} else if classes[0].Name != "container" {
		t.Errorf("class name = %q, want %q", classes[0].Name, "container")
	}

	if len(ids) != 1 {
		t.Errorf("got %d CSS ID selectors, want 1", len(ids))
	} else if ids[0].Name != "header" {
		t.Errorf("ID selector name = %q, want %q", ids[0].Name, "header")
	}
}

func TestHTMLParser_Parse_InlineScriptDisabled(t *testing.T) {
	parser := NewHTMLParser(WithHTMLParseInlineScripts(false))
	ctx := context.Background()

	content := []byte(`<script>
function test() {}
</script>`)

	result, err := parser.Parse(ctx, content, "index.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	funcs := filterSymbolsByKind(result.Symbols, SymbolKindFunction)
	if len(funcs) != 0 {
		t.Errorf("got %d functions, want 0 (inline scripts disabled)", len(funcs))
	}
}

func TestHTMLParser_Parse_InlineStyleDisabled(t *testing.T) {
	parser := NewHTMLParser(WithHTMLParseInlineStyles(false))
	ctx := context.Background()

	content := []byte(`<style>
.test { color: red; }
</style>`)

	result, err := parser.Parse(ctx, content, "index.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	classes := filterSymbolsByKind(result.Symbols, SymbolKindCSSClass)
	if len(classes) != 0 {
		t.Errorf("got %d CSS classes, want 0 (inline styles disabled)", len(classes))
	}
}

func TestHTMLParser_Parse_EmptyContent(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	result, err := parser.Parse(ctx, []byte(""), "empty.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(result.Symbols) != 0 {
		t.Errorf("got %d symbols, want 0", len(result.Symbols))
	}
	if result.Language != "html" {
		t.Errorf("Language = %q, want %q", result.Language, "html")
	}
}

func TestHTMLParser_Parse_MinimalHTML(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<!DOCTYPE html><html></html>`)

	result, err := parser.Parse(ctx, content, "minimal.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// No elements with IDs, no imports
	if len(result.Symbols) != 0 {
		t.Errorf("got %d symbols, want 0", len(result.Symbols))
	}
	if len(result.Imports) != 0 {
		t.Errorf("got %d imports, want 0", len(result.Imports))
	}
}

func TestHTMLParser_Parse_ContextCancellation(t *testing.T) {
	parser := NewHTMLParser()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	content := []byte(`<div id="test">Content</div>`)

	_, err := parser.Parse(ctx, content, "test.html")
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Errorf("error should mention cancellation: %v", err)
	}
}

func TestHTMLParser_Parse_FileTooLarge(t *testing.T) {
	parser := NewHTMLParser(WithHTMLMaxFileSize(100))
	ctx := context.Background()

	content := make([]byte, 200)
	for i := range content {
		content[i] = 'a'
	}

	_, err := parser.Parse(ctx, content, "large.html")
	if err != ErrFileTooLarge {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestHTMLParser_Parse_InvalidUTF8(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte{0xff, 0xfe, 0x00, 0x01}

	_, err := parser.Parse(ctx, content, "invalid.html")
	if err != ErrInvalidContent {
		t.Errorf("expected ErrInvalidContent, got %v", err)
	}
}

func TestHTMLParser_Parse_HashComputation(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<html><body>Test</body></html>`)

	result, err := parser.Parse(ctx, content, "test.html")
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

func TestHTMLParser_Parse_TimestampSet(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	before := time.Now().UnixMilli()
	result, err := parser.Parse(ctx, []byte(`<html></html>`), "test.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	after := time.Now().UnixMilli()

	if result.ParsedAtMilli < before || result.ParsedAtMilli > after {
		t.Errorf("ParsedAtMilli = %d, want between %d and %d", result.ParsedAtMilli, before, after)
	}
}

func TestHTMLParser_Parse_LineNumbers(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<!DOCTYPE html>
<html>
<body>
    <div id="first">Line 4</div>
    <div id="second">Line 5</div>
</body>
</html>`)

	result, err := parser.Parse(ctx, content, "test.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	elements := filterSymbolsByKind(result.Symbols, SymbolKindElement)
	if len(elements) != 2 {
		t.Fatalf("got %d elements, want 2", len(elements))
	}

	for _, sym := range elements {
		if sym.Name == "first" && sym.StartLine != 4 {
			t.Errorf("first element StartLine = %d, want 4", sym.StartLine)
		}
		if sym.Name == "second" && sym.StartLine != 5 {
			t.Errorf("second element StartLine = %d, want 5", sym.StartLine)
		}
	}
}

func TestHTMLParser_Parse_SelfClosingTags(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<html>
<head>
    <link rel="stylesheet" href="style.css"/>
    <meta charset="utf-8"/>
</head>
<body>
    <input id="name" type="text"/>
    <img id="logo" src="logo.png"/>
</body>
</html>`)

	result, err := parser.Parse(ctx, content, "test.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Should find elements with IDs even if self-closing
	elements := filterSymbolsByKind(result.Symbols, SymbolKindElement)
	names := make(map[string]bool)
	for _, sym := range elements {
		names[sym.Name] = true
	}

	if !names["name"] {
		t.Error("missing element with id=name")
	}
	if !names["logo"] {
		t.Error("missing element with id=logo")
	}

	// Should find stylesheet import
	if len(result.Imports) < 1 {
		t.Error("should find stylesheet import")
	}
}

func TestHTMLParser_Parse_NestedElements(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<div id="outer">
    <section id="middle">
        <article id="inner">
            <p id="deepest">Content</p>
        </article>
    </section>
</div>`)

	result, err := parser.Parse(ctx, content, "nested.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	elements := filterSymbolsByKind(result.Symbols, SymbolKindElement)
	if len(elements) != 4 {
		t.Fatalf("got %d elements, want 4", len(elements))
	}

	ids := make(map[string]bool)
	for _, sym := range elements {
		ids[sym.Name] = true
	}

	wantIDs := []string{"outer", "middle", "inner", "deepest"}
	for _, id := range wantIDs {
		if !ids[id] {
			t.Errorf("missing nested element with id=%q", id)
		}
	}
}

func TestHTMLParser_Parse_ElementSignature(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<div id="container">Content</div>`)

	result, err := parser.Parse(ctx, content, "test.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	elements := filterSymbolsByKind(result.Symbols, SymbolKindElement)
	if len(elements) != 1 {
		t.Fatalf("got %d elements, want 1", len(elements))
	}

	want := `<div id="container">`
	if elements[0].Signature != want {
		t.Errorf("Signature = %q, want %q", elements[0].Signature, want)
	}
}

func TestHTMLParser_Parse_CustomElementSignature(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<user-card></user-card>`)

	result, err := parser.Parse(ctx, content, "test.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	components := filterSymbolsByKind(result.Symbols, SymbolKindComponent)
	if len(components) != 1 {
		t.Fatalf("got %d components, want 1", len(components))
	}

	want := "<user-card>"
	if components[0].Signature != want {
		t.Errorf("Signature = %q, want %q", components[0].Signature, want)
	}
}

func TestHTMLParser_Parse_MultipleScriptsAndStyles(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<!DOCTYPE html>
<html>
<head>
    <script src="vendor.js"></script>
    <link rel="stylesheet" href="reset.css">
    <style>
        .header { display: flex; }
    </style>
</head>
<body>
    <script>
        function main() {}
    </script>
    <script src="app.js"></script>
    <style>
        .footer { margin-top: 20px; }
    </style>
</body>
</html>`)

	result, err := parser.Parse(ctx, content, "index.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Should have 3 imports (2 scripts + 1 stylesheet)
	scripts := 0
	stylesheets := 0
	for _, imp := range result.Imports {
		if imp.IsScript {
			scripts++
		}
		if imp.IsStylesheet {
			stylesheets++
		}
	}

	if scripts != 2 {
		t.Errorf("got %d script imports, want 2", scripts)
	}
	if stylesheets != 1 {
		t.Errorf("got %d stylesheet imports, want 1", stylesheets)
	}

	// Should find function from inline script
	funcs := filterSymbolsByKind(result.Symbols, SymbolKindFunction)
	if len(funcs) != 1 {
		t.Errorf("got %d functions, want 1", len(funcs))
	}

	// Should find CSS classes from both inline styles
	classes := filterSymbolsByKind(result.Symbols, SymbolKindCSSClass)
	if len(classes) != 2 {
		t.Errorf("got %d CSS classes, want 2", len(classes))
	}
}

func TestHTMLParser_Parse_ComplexDocument(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Complex Document</title>
    <link rel="stylesheet" href="styles/main.css">
    <script type="module" src="js/app.mjs"></script>
</head>
<body>
    <header id="site-header">
        <nav-menu class="primary"></nav-menu>
        <search-box id="global-search"></search-box>
    </header>

    <main id="content">
        <section id="hero">
            <h1>Welcome</h1>
        </section>

        <form name="contact" id="contact-form" action="/submit">
            <input id="email" type="email" name="email">
            <user-input></user-input>
            <button type="submit">Send</button>
        </form>
    </main>

    <footer id="site-footer">
        <social-links></social-links>
    </footer>

    <script>
        class App {
            constructor() {}
        }
    </script>

    <style>
        #site-header { position: sticky; }
        .container { max-width: 1200px; }
    </style>
</body>
</html>`)

	result, err := parser.Parse(ctx, content, "complex.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Count different symbol types
	elements := len(filterSymbolsByKind(result.Symbols, SymbolKindElement))
	forms := len(filterSymbolsByKind(result.Symbols, SymbolKindForm))
	components := len(filterSymbolsByKind(result.Symbols, SymbolKindComponent))
	cssClasses := len(filterSymbolsByKind(result.Symbols, SymbolKindCSSClass))

	// Elements with IDs: site-header, global-search, content, hero, contact-form, email, site-footer
	if elements < 7 {
		t.Errorf("got %d elements with IDs, want at least 7", elements)
	}

	// Form: contact
	if forms != 1 {
		t.Errorf("got %d forms, want 1", forms)
	}

	// Custom elements: nav-menu, search-box, user-input, social-links
	if components != 4 {
		t.Errorf("got %d custom elements, want 4", components)
	}

	// JavaScript class: App
	jsClasses := 0
	for _, sym := range result.Symbols {
		if sym.Kind == SymbolKindClass && strings.Contains(sym.FilePath, "<script>") {
			jsClasses++
		}
	}
	if jsClasses != 1 {
		t.Errorf("got %d JS classes, want 1", jsClasses)
	}

	// CSS: .container (class)
	if cssClasses < 1 {
		t.Errorf("got %d CSS classes, want at least 1", cssClasses)
	}

	// Imports: main.css (stylesheet) + app.mjs (module script)
	if len(result.Imports) != 2 {
		t.Errorf("got %d imports, want 2", len(result.Imports))
	}
}

func TestHTMLParser_Parse_EmptyInlineScript(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<script>   </script>`)

	result, err := parser.Parse(ctx, content, "test.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Should not produce errors for empty script
	if len(result.Errors) > 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
}

func TestHTMLParser_Parse_EmptyInlineStyle(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<style>   </style>`)

	result, err := parser.Parse(ctx, content, "test.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Should not produce errors for empty style
	if len(result.Errors) > 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
}

func TestHTMLParser_Parse_ImportLocations(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<html>
<head>
    <script src="app.js"></script>
</head>
</html>`)

	result, err := parser.Parse(ctx, content, "test.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(result.Imports) != 1 {
		t.Fatalf("got %d imports, want 1", len(result.Imports))
	}

	imp := result.Imports[0]
	if imp.Location.FilePath != "test.html" {
		t.Errorf("Location.FilePath = %q, want %q", imp.Location.FilePath, "test.html")
	}
	if imp.Location.StartLine != 3 {
		t.Errorf("Location.StartLine = %d, want 3", imp.Location.StartLine)
	}
}

func TestHTMLParser_ConcurrentParsing(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	contents := [][]byte{
		[]byte(`<div id="test1"></div>`),
		[]byte(`<div id="test2"></div>`),
		[]byte(`<div id="test3"></div>`),
		[]byte(`<nav-menu></nav-menu>`),
		[]byte(`<script src="app.js"></script>`),
	}

	results := make(chan error, len(contents))

	for i, content := range contents {
		go func(idx int, c []byte) {
			_, err := parser.Parse(ctx, c, "concurrent.html")
			results <- err
		}(i, content)
	}

	for range contents {
		if err := <-results; err != nil {
			t.Errorf("concurrent parse error: %v", err)
		}
	}
}

func TestHTMLParser_Parse_SymbolExported(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<div id="public-element"></div>
<form name="public-form"></form>
<custom-elem></custom-elem>`)

	result, err := parser.Parse(ctx, content, "test.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	for _, sym := range result.Symbols {
		// HTML elements, forms, and components should be exported
		if sym.Kind == SymbolKindElement || sym.Kind == SymbolKindForm || sym.Kind == SymbolKindComponent {
			if !sym.Exported {
				t.Errorf("symbol %q should be exported", sym.Name)
			}
		}
	}
}

func TestHTMLParser_Parse_FilePath(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<div id="test"></div>`)
	filePath := "src/pages/index.html"

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

func TestHTMLParser_Parse_InlineScriptSymbolFilePath(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<script>
function test() {}
</script>`)

	result, err := parser.Parse(ctx, content, "app.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	funcs := filterSymbolsByKind(result.Symbols, SymbolKindFunction)
	if len(funcs) != 1 {
		t.Fatalf("got %d functions, want 1", len(funcs))
	}

	// Inline script symbols should have the special path marker
	if !strings.Contains(funcs[0].FilePath, "<script>") {
		t.Errorf("inline script symbol FilePath should contain <script>: %q", funcs[0].FilePath)
	}
}

func TestHTMLParser_Parse_InlineStyleSymbolFilePath(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<style>
.test { color: red; }
</style>`)

	result, err := parser.Parse(ctx, content, "app.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	classes := filterSymbolsByKind(result.Symbols, SymbolKindCSSClass)
	if len(classes) != 1 {
		t.Fatalf("got %d CSS classes, want 1", len(classes))
	}

	// Inline style symbols should have the special path marker
	if !strings.Contains(classes[0].FilePath, "<style>") {
		t.Errorf("inline style symbol FilePath should contain <style>: %q", classes[0].FilePath)
	}
}

func TestHTMLParser_DefaultOptions(t *testing.T) {
	opts := DefaultHTMLParserOptions()

	if opts.MaxFileSize != 10*1024*1024 {
		t.Errorf("MaxFileSize = %d, want %d", opts.MaxFileSize, 10*1024*1024)
	}
	if !opts.ParseInlineScripts {
		t.Error("ParseInlineScripts should be true by default")
	}
	if !opts.ParseInlineStyles {
		t.Error("ParseInlineStyles should be true by default")
	}
}

func TestHTMLParser_WithHTMLMaxFileSize(t *testing.T) {
	parser := NewHTMLParser(WithHTMLMaxFileSize(1000))

	if parser.options.MaxFileSize != 1000 {
		t.Errorf("MaxFileSize = %d, want 1000", parser.options.MaxFileSize)
	}
}

func TestHTMLParser_Parse_SymbolLanguage(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<div id="test"></div>`)

	result, err := parser.Parse(ctx, content, "test.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	elements := filterSymbolsByKind(result.Symbols, SymbolKindElement)
	if len(elements) != 1 {
		t.Fatalf("got %d elements, want 1", len(elements))
	}

	if elements[0].Language != "html" {
		t.Errorf("Language = %q, want %q", elements[0].Language, "html")
	}
}

func TestHTMLParser_Parse_InlineScriptSymbolLanguage(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<script>
function test() {}
</script>`)

	result, err := parser.Parse(ctx, content, "test.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	funcs := filterSymbolsByKind(result.Symbols, SymbolKindFunction)
	if len(funcs) != 1 {
		t.Fatalf("got %d functions, want 1", len(funcs))
	}

	// JavaScript symbols should have javascript language
	if funcs[0].Language != "javascript" {
		t.Errorf("Language = %q, want %q", funcs[0].Language, "javascript")
	}
}

func TestHTMLParser_Parse_InlineStyleSymbolLanguage(t *testing.T) {
	parser := NewHTMLParser()
	ctx := context.Background()

	content := []byte(`<style>
.test { color: red; }
</style>`)

	result, err := parser.Parse(ctx, content, "test.html")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	classes := filterSymbolsByKind(result.Symbols, SymbolKindCSSClass)
	if len(classes) != 1 {
		t.Fatalf("got %d CSS classes, want 1", len(classes))
	}

	// CSS symbols should have css language
	if classes[0].Language != "css" {
		t.Errorf("Language = %q, want %q", classes[0].Language, "css")
	}
}
