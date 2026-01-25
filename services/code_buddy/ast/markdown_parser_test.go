package ast

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMarkdownParser_Parse_SimpleHeadings(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte(`# Heading 1

## Heading 2

### Heading 3

#### Heading 4

##### Heading 5

###### Heading 6
`)

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	headings := filterSymbolsByKind(result.Symbols, SymbolKindHeading)
	if len(headings) != 6 {
		t.Errorf("expected 6 headings, got %d", len(headings))
	}

	// Verify heading levels
	for _, h := range headings {
		if h.Metadata == nil {
			t.Errorf("heading %s missing metadata", h.Name)
			continue
		}
		expectedLevel := 0
		switch h.Name {
		case "Heading 1":
			expectedLevel = 1
		case "Heading 2":
			expectedLevel = 2
		case "Heading 3":
			expectedLevel = 3
		case "Heading 4":
			expectedLevel = 4
		case "Heading 5":
			expectedLevel = 5
		case "Heading 6":
			expectedLevel = 6
		}
		if h.Metadata.HeadingLevel != expectedLevel {
			t.Errorf("heading %s: expected level %d, got %d", h.Name, expectedLevel, h.Metadata.HeadingLevel)
		}
	}
}

func TestMarkdownParser_Parse_HeadingSignature(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte(`## Important Section`)

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	headings := filterSymbolsByKind(result.Symbols, SymbolKindHeading)
	if len(headings) != 1 {
		t.Fatalf("expected 1 heading, got %d", len(headings))
	}

	h := headings[0]
	expectedSig := "## Important Section"
	if h.Signature != expectedSig {
		t.Errorf("expected signature %q, got %q", expectedSig, h.Signature)
	}
}

func TestMarkdownParser_Parse_FencedCodeBlock(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte("# Code Example\n\n```go\nfunc main() {\n    fmt.Println(\"Hello\")\n}\n```\n")

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	codeBlocks := filterSymbolsByKind(result.Symbols, SymbolKindCodeBlock)
	if len(codeBlocks) != 1 {
		t.Fatalf("expected 1 code block, got %d", len(codeBlocks))
	}

	cb := codeBlocks[0]
	if cb.Name != "go_block" {
		t.Errorf("expected name 'go_block', got %q", cb.Name)
	}
	if cb.Metadata == nil || cb.Metadata.CodeLanguage != "go" {
		t.Errorf("expected code language 'go', got %v", cb.Metadata)
	}
	if !strings.HasPrefix(cb.Signature, "```go") {
		t.Errorf("expected signature to start with '```go', got %q", cb.Signature)
	}
}

func TestMarkdownParser_Parse_MultipleCodeBlocks(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte(`# Examples

` + "```python\nprint('hello')\n```" + `

` + "```javascript\nconsole.log('world');\n```" + `

` + "```\nplain code\n```" + `
`)

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	codeBlocks := filterSymbolsByKind(result.Symbols, SymbolKindCodeBlock)
	if len(codeBlocks) != 3 {
		t.Fatalf("expected 3 code blocks, got %d", len(codeBlocks))
	}

	// Check languages
	languages := make(map[string]bool)
	for _, cb := range codeBlocks {
		if cb.Metadata != nil && cb.Metadata.CodeLanguage != "" {
			languages[cb.Metadata.CodeLanguage] = true
		}
	}
	if !languages["python"] {
		t.Error("missing python code block")
	}
	if !languages["javascript"] {
		t.Error("missing javascript code block")
	}
}

func TestMarkdownParser_Parse_UnorderedList(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte(`# Tasks

- First item
- Second item
- Third item
`)

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lists := filterSymbolsByKind(result.Symbols, SymbolKindList)
	if len(lists) != 1 {
		t.Fatalf("expected 1 list, got %d", len(lists))
	}

	l := lists[0]
	if l.Metadata == nil {
		t.Fatal("list missing metadata")
	}
	if l.Metadata.ListType != "unordered" {
		t.Errorf("expected list type 'unordered', got %q", l.Metadata.ListType)
	}
	if l.Metadata.ListItems != 3 {
		t.Errorf("expected 3 items, got %d", l.Metadata.ListItems)
	}
	if !strings.Contains(l.Signature, "First item") {
		t.Errorf("expected signature to contain first item, got %q", l.Signature)
	}
}

func TestMarkdownParser_Parse_OrderedList(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte(`# Steps

1. Step one
2. Step two
3. Step three
4. Step four
`)

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lists := filterSymbolsByKind(result.Symbols, SymbolKindList)
	if len(lists) != 1 {
		t.Fatalf("expected 1 list, got %d", len(lists))
	}

	l := lists[0]
	if l.Metadata == nil {
		t.Fatal("list missing metadata")
	}
	if l.Metadata.ListType != "ordered" {
		t.Errorf("expected list type 'ordered', got %q", l.Metadata.ListType)
	}
	if l.Metadata.ListItems != 4 {
		t.Errorf("expected 4 items, got %d", l.Metadata.ListItems)
	}
}

func TestMarkdownParser_Parse_LinkReference(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte(`# Links

[example]: https://example.com "Example Site"
[github]: https://github.com
`)

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	links := filterSymbolsByKind(result.Symbols, SymbolKindLink)
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}

	// Find example link
	var exampleLink *Symbol
	for _, l := range links {
		if l.Name == "example" {
			exampleLink = l
			break
		}
	}

	if exampleLink == nil {
		t.Fatal("missing 'example' link")
	}
	if exampleLink.Metadata == nil {
		t.Fatal("example link missing metadata")
	}
	if exampleLink.Metadata.LinkURL != "https://example.com" {
		t.Errorf("expected URL 'https://example.com', got %q", exampleLink.Metadata.LinkURL)
	}
	if exampleLink.Metadata.LinkTitle != "Example Site" {
		t.Errorf("expected title 'Example Site', got %q", exampleLink.Metadata.LinkTitle)
	}
}

func TestMarkdownParser_Parse_ComplexDocument(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte(`# Project README

A description of the project.

## Installation

` + "```bash\nnpm install my-package\n```" + `

## Usage

` + "```javascript\nconst pkg = require('my-package');\n```" + `

### API Reference

- Method 1
- Method 2
- Method 3

## Contributing

1. Fork the repo
2. Make changes
3. Submit PR

[docs]: https://docs.example.com "Documentation"
[repo]: https://github.com/user/repo
`)

	result, err := parser.Parse(context.Background(), content, "README.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Count symbols by kind
	headings := filterSymbolsByKind(result.Symbols, SymbolKindHeading)
	codeBlocks := filterSymbolsByKind(result.Symbols, SymbolKindCodeBlock)
	lists := filterSymbolsByKind(result.Symbols, SymbolKindList)
	links := filterSymbolsByKind(result.Symbols, SymbolKindLink)

	if len(headings) != 5 {
		t.Errorf("expected 5 headings, got %d", len(headings))
	}
	if len(codeBlocks) != 2 {
		t.Errorf("expected 2 code blocks, got %d", len(codeBlocks))
	}
	if len(lists) != 2 {
		t.Errorf("expected 2 lists, got %d", len(lists))
	}
	if len(links) != 2 {
		t.Errorf("expected 2 links, got %d", len(links))
	}
}

func TestMarkdownParser_Parse_NestedSections(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte(`# Chapter 1

## Section 1.1

Content here.

### Subsection 1.1.1

More content.

## Section 1.2

Different content.
`)

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	headings := filterSymbolsByKind(result.Symbols, SymbolKindHeading)
	if len(headings) != 4 {
		t.Fatalf("expected 4 headings, got %d", len(headings))
	}

	// Verify parent relationships
	subsection := findSymbolByName(headings, "Subsection 1.1.1")
	if subsection == nil {
		t.Fatal("missing subsection")
	}
	if subsection.Metadata == nil || subsection.Metadata.ParentName != "Section 1.1" {
		parent := ""
		if subsection.Metadata != nil {
			parent = subsection.Metadata.ParentName
		}
		t.Errorf("expected parent 'Section 1.1', got %q", parent)
	}
}

func TestMarkdownParser_Parse_EmptyContent(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte("")

	result, err := parser.Parse(context.Background(), content, "empty.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Symbols) != 0 {
		t.Errorf("expected 0 symbols, got %d", len(result.Symbols))
	}
}

func TestMarkdownParser_Parse_HeadingsOnly(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte(`# One
## Two
### Three
`)

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	headings := filterSymbolsByKind(result.Symbols, SymbolKindHeading)
	if len(headings) != 3 {
		t.Errorf("expected 3 headings, got %d", len(headings))
	}
}

func TestMarkdownParser_Parse_MaxHeadingDepth(t *testing.T) {
	parser := NewMarkdownParser(WithMarkdownMaxHeadingDepth(2))
	content := []byte(`# H1
## H2
### H3
#### H4
`)

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	headings := filterSymbolsByKind(result.Symbols, SymbolKindHeading)
	// Should only get H1 and H2
	if len(headings) != 2 {
		t.Errorf("expected 2 headings (max depth 2), got %d", len(headings))
	}
}

func TestMarkdownParser_Parse_DisableCodeBlocks(t *testing.T) {
	parser := NewMarkdownParser(WithMarkdownExtractCodeBlocks(false))
	content := []byte("# Title\n\n```go\ncode\n```\n")

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	codeBlocks := filterSymbolsByKind(result.Symbols, SymbolKindCodeBlock)
	if len(codeBlocks) != 0 {
		t.Errorf("expected 0 code blocks (disabled), got %d", len(codeBlocks))
	}
}

func TestMarkdownParser_Parse_DisableLists(t *testing.T) {
	parser := NewMarkdownParser(WithMarkdownExtractLists(false))
	content := []byte(`# Title

- item 1
- item 2
`)

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lists := filterSymbolsByKind(result.Symbols, SymbolKindList)
	if len(lists) != 0 {
		t.Errorf("expected 0 lists (disabled), got %d", len(lists))
	}
}

func TestMarkdownParser_Parse_DisableLinks(t *testing.T) {
	parser := NewMarkdownParser(WithMarkdownExtractLinks(false))
	content := []byte(`# Title

[link]: https://example.com
`)

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	links := filterSymbolsByKind(result.Symbols, SymbolKindLink)
	if len(links) != 0 {
		t.Errorf("expected 0 links (disabled), got %d", len(links))
	}
}

func TestMarkdownParser_Parse_FileTooLarge(t *testing.T) {
	parser := NewMarkdownParser(WithMarkdownMaxFileSize(100))
	content := make([]byte, 200) // Larger than max
	for i := range content {
		content[i] = 'a'
	}

	_, err := parser.Parse(context.Background(), content, "large.md")
	if err != ErrFileTooLarge {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestMarkdownParser_Parse_InvalidUTF8(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte{0xff, 0xfe, 0x00, 0x01}

	_, err := parser.Parse(context.Background(), content, "invalid.md")
	if err != ErrInvalidContent {
		t.Errorf("expected ErrInvalidContent, got %v", err)
	}
}

func TestMarkdownParser_Parse_ContextCancellation(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte("# Heading")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := parser.Parse(ctx, content, "test.md")
	if err == nil {
		t.Error("expected error for canceled context")
	}
}

func TestMarkdownParser_Parse_LineNumbers(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte(`# First Heading

Content

## Second Heading
`)

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	headings := filterSymbolsByKind(result.Symbols, SymbolKindHeading)
	if len(headings) != 2 {
		t.Fatalf("expected 2 headings, got %d", len(headings))
	}

	first := findSymbolByName(headings, "First Heading")
	if first == nil || first.StartLine != 1 {
		line := 0
		if first != nil {
			line = first.StartLine
		}
		t.Errorf("expected First Heading on line 1, got %d", line)
	}

	second := findSymbolByName(headings, "Second Heading")
	if second == nil || second.StartLine != 5 {
		line := 0
		if second != nil {
			line = second.StartLine
		}
		t.Errorf("expected Second Heading on line 5, got %d", line)
	}
}

func TestMarkdownParser_Parse_HashComputed(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte("# Test")

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Hash == "" {
		t.Error("expected hash to be computed")
	}
	if len(result.Hash) != 64 { // SHA-256 hex length
		t.Errorf("expected 64 char hash, got %d", len(result.Hash))
	}
}

func TestMarkdownParser_Parse_ParsedAtSet(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte("# Test")

	before := time.Now().UnixMilli()
	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := time.Now().UnixMilli()

	if result.ParsedAtMilli < before || result.ParsedAtMilli > after {
		t.Errorf("ParsedAtMilli %d not between %d and %d", result.ParsedAtMilli, before, after)
	}
}

func TestMarkdownParser_Language(t *testing.T) {
	parser := NewMarkdownParser()
	if parser.Language() != "markdown" {
		t.Errorf("expected language 'markdown', got %q", parser.Language())
	}
}

func TestMarkdownParser_Extensions(t *testing.T) {
	parser := NewMarkdownParser()
	exts := parser.Extensions()

	expected := []string{".md", ".markdown", ".mdown", ".mkd"}
	if len(exts) != len(expected) {
		t.Errorf("expected %d extensions, got %d", len(expected), len(exts))
	}

	for _, e := range expected {
		found := false
		for _, ext := range exts {
			if ext == e {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing extension %q", e)
		}
	}
}

func TestMarkdownParser_Parse_CodeBlockWithoutLanguage(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte("# Code\n\n```\nplain code\n```\n")

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	codeBlocks := filterSymbolsByKind(result.Symbols, SymbolKindCodeBlock)
	if len(codeBlocks) != 1 {
		t.Fatalf("expected 1 code block, got %d", len(codeBlocks))
	}

	cb := codeBlocks[0]
	if cb.Name != "code_block" {
		t.Errorf("expected name 'code_block' for unlabeled block, got %q", cb.Name)
	}
}

func TestMarkdownParser_Parse_MultipleLists(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte(`# Lists

- Unordered item 1
- Unordered item 2

1. Ordered item 1
2. Ordered item 2

- Another unordered
- List here
`)

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lists := filterSymbolsByKind(result.Symbols, SymbolKindList)
	if len(lists) != 3 {
		t.Errorf("expected 3 lists, got %d", len(lists))
	}

	orderedCount := 0
	unorderedCount := 0
	for _, l := range lists {
		if l.Metadata != nil {
			if l.Metadata.ListType == "ordered" {
				orderedCount++
			} else if l.Metadata.ListType == "unordered" {
				unorderedCount++
			}
		}
	}

	if orderedCount != 1 {
		t.Errorf("expected 1 ordered list, got %d", orderedCount)
	}
	if unorderedCount != 2 {
		t.Errorf("expected 2 unordered lists, got %d", unorderedCount)
	}
}

func TestMarkdownParser_Parse_SymbolIDs(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte("# My Heading")

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	headings := filterSymbolsByKind(result.Symbols, SymbolKindHeading)
	if len(headings) != 1 {
		t.Fatalf("expected 1 heading, got %d", len(headings))
	}

	h := headings[0]
	expectedID := "test.md:1:My Heading"
	if h.ID != expectedID {
		t.Errorf("expected ID %q, got %q", expectedID, h.ID)
	}
}

func TestMarkdownParser_Parse_ExportedSymbols(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte(`# Heading

- item

[link]: https://example.com
`)

	result, err := parser.Parse(context.Background(), content, "test.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, sym := range result.Symbols {
		if !sym.Exported {
			t.Errorf("symbol %s should be exported", sym.Name)
		}
	}
}

func TestMarkdownParser_Concurrency(t *testing.T) {
	parser := NewMarkdownParser()
	content := []byte(`# Test

- item 1
- item 2

` + "```go\ncode\n```" + `
`)

	const goroutines = 10
	errors := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			_, err := parser.Parse(context.Background(), content, "test.md")
			errors <- err
		}()
	}

	for i := 0; i < goroutines; i++ {
		if err := <-errors; err != nil {
			t.Errorf("concurrent parse error: %v", err)
		}
	}
}

// Helper functions

func findSymbolByName(symbols []*Symbol, name string) *Symbol {
	for _, s := range symbols {
		if s.Name == name {
			return s
		}
	}
	return nil
}
