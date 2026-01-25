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
	"github.com/smacker/go-tree-sitter/dockerfile"
)

// DockerfileParser extracts symbols from Dockerfile source files.
//
// Description:
//
//	DockerfileParser uses tree-sitter to parse Dockerfiles and extract
//	structured symbol information including stages, ARG/ENV variables,
//	labels, exposed ports, and volumes.
//
// Thread Safety:
//
//	DockerfileParser is safe for concurrent use. Multiple goroutines can call Parse
//	simultaneously. Each Parse call creates its own tree-sitter parser instance.
//
// Example:
//
//	parser := NewDockerfileParser()
//	result, err := parser.Parse(ctx, content, "Dockerfile")
//	if err != nil {
//	    return fmt.Errorf("parse: %w", err)
//	}
//	for _, sym := range result.Symbols {
//	    fmt.Printf("%s: %s\n", sym.Kind, sym.Name)
//	}
type DockerfileParser struct {
	options DockerfileParserOptions
}

// DockerfileParserOptions configures DockerfileParser behavior.
type DockerfileParserOptions struct {
	// MaxFileSize is the maximum file size in bytes to parse.
	// Files larger than this return ErrFileTooLarge.
	// Default: 10MB
	MaxFileSize int
}

// DefaultDockerfileParserOptions returns the default options.
func DefaultDockerfileParserOptions() DockerfileParserOptions {
	return DockerfileParserOptions{
		MaxFileSize: 10 * 1024 * 1024, // 10MB
	}
}

// DockerfileParserOption is a functional option for configuring DockerfileParser.
type DockerfileParserOption func(*DockerfileParserOptions)

// WithDockerfileMaxFileSize sets the maximum file size for parsing.
func WithDockerfileMaxFileSize(size int) DockerfileParserOption {
	return func(o *DockerfileParserOptions) {
		o.MaxFileSize = size
	}
}

// NewDockerfileParser creates a new DockerfileParser with the given options.
//
// Description:
//
//	Creates a parser configured for Dockerfile files. The parser can be
//	reused for multiple files and is safe for concurrent use.
//
// Example:
//
//	// Default options
//	parser := NewDockerfileParser()
//
//	// With custom options
//	parser := NewDockerfileParser(
//	    WithDockerfileMaxFileSize(5 * 1024 * 1024),
//	)
func NewDockerfileParser(opts ...DockerfileParserOption) *DockerfileParser {
	options := DefaultDockerfileParserOptions()
	for _, opt := range opts {
		opt(&options)
	}
	return &DockerfileParser{
		options: options,
	}
}

// Language returns the language name for this parser.
func (p *DockerfileParser) Language() string {
	return "dockerfile"
}

// Extensions returns the file extensions this parser handles.
func (p *DockerfileParser) Extensions() []string {
	return []string{"Dockerfile", ".dockerfile"}
}

// Parse extracts symbols from Dockerfile source code.
//
// Description:
//
//	Parses the provided Dockerfile content using tree-sitter and extracts all
//	symbols including stages, variables, labels, ports, and volumes.
//
// Inputs:
//
//	ctx      - Context for cancellation. Checked before/after parsing.
//	content  - Raw Dockerfile source bytes. Must be valid UTF-8.
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
func (p *DockerfileParser) Parse(ctx context.Context, content []byte, filePath string) (*ParseResult, error) {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("dockerfile parse canceled before start: %w", err)
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
		Language:      "dockerfile",
		Hash:          hashStr,
		ParsedAtMilli: time.Now().UnixMilli(),
		Symbols:       make([]*Symbol, 0),
		Imports:       make([]Import, 0),
		Errors:        make([]string, 0),
	}

	// Parse with tree-sitter
	parser := sitter.NewParser()
	parser.SetLanguage(dockerfile.GetLanguage())

	tree, err := parser.ParseCtx(ctx, nil, content)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter parse failed: %w", err)
	}
	defer tree.Close()

	// Check context after parsing
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("dockerfile parse canceled after tree-sitter: %w", err)
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
func (p *DockerfileParser) extractSymbols(ctx context.Context, node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	if node == nil {
		return
	}

	// Check context periodically
	if ctx.Err() != nil {
		return
	}

	nodeType := node.Type()

	switch nodeType {
	case dockerfileNodeSourceFile:
		// Process all children
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(ctx, node.Child(i), content, filePath, result)
		}

	case dockerfileNodeFromInstruction:
		p.extractFromInstruction(node, content, filePath, result)

	case dockerfileNodeArgInstruction:
		p.extractArgInstruction(node, content, filePath, result)

	case dockerfileNodeEnvInstruction:
		p.extractEnvInstruction(node, content, filePath, result)

	case dockerfileNodeLabelInstruction:
		p.extractLabelInstruction(node, content, filePath, result)

	case dockerfileNodeExposeInstruction:
		p.extractExposeInstruction(node, content, filePath, result)

	case dockerfileNodeVolumeInstruction:
		p.extractVolumeInstruction(node, content, filePath, result)

	default:
		// Recurse into children for other node types
		for i := 0; i < int(node.ChildCount()); i++ {
			p.extractSymbols(ctx, node.Child(i), content, filePath, result)
		}
	}
}

// extractFromInstruction extracts stage information from FROM instruction.
func (p *DockerfileParser) extractFromInstruction(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	imageName := ""
	imageTag := ""
	stageName := ""

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case dockerfileNodeImageSpec:
			imageName, imageTag = p.extractImageSpec(child, content)
		case dockerfileNodeImageAlias:
			stageName = string(content[child.StartByte():child.EndByte()])
		}
	}

	// Build signature
	signature := "FROM " + imageName
	if imageTag != "" {
		signature += ":" + imageTag
	}
	if stageName != "" {
		signature += " AS " + stageName
	}

	// Create stage symbol if named
	if stageName != "" {
		sym := &Symbol{
			ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, stageName),
			Name:          stageName,
			Kind:          SymbolKindStage,
			FilePath:      filePath,
			StartLine:     int(node.StartPoint().Row) + 1,
			EndLine:       int(node.EndPoint().Row) + 1,
			StartCol:      int(node.StartPoint().Column),
			EndCol:        int(node.EndPoint().Column),
			Signature:     signature,
			Language:      "dockerfile",
			ParsedAtMilli: time.Now().UnixMilli(),
			Exported:      true,
		}
		result.Symbols = append(result.Symbols, sym)
	}

	// Record base image as import
	importPath := imageName
	if imageTag != "" {
		importPath += ":" + imageTag
	}
	imp := Import{
		Path: importPath,
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

// extractImageSpec extracts image name and tag from image_spec node.
func (p *DockerfileParser) extractImageSpec(node *sitter.Node, content []byte) (name, tag string) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case dockerfileNodeImageName:
			name = string(content[child.StartByte():child.EndByte()])
		case dockerfileNodeImageTag:
			// Tag includes the colon, so extract just the tag value
			tagText := string(content[child.StartByte():child.EndByte()])
			tag = strings.TrimPrefix(tagText, ":")
		}
	}
	return
}

// extractArgInstruction extracts ARG variables.
func (p *DockerfileParser) extractArgInstruction(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	argName := ""
	argValue := ""
	hasDefault := false

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case dockerfileNodeUnquotedString:
			if argName == "" {
				argName = string(content[child.StartByte():child.EndByte()])
			} else {
				argValue = string(content[child.StartByte():child.EndByte()])
				hasDefault = true
			}
		case "=":
			hasDefault = true
		}
	}

	if argName == "" {
		return
	}

	signature := "ARG " + argName
	if hasDefault && argValue != "" {
		signature += "=" + argValue
	}

	sym := &Symbol{
		ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, argName),
		Name:          argName,
		Kind:          SymbolKindArg,
		FilePath:      filePath,
		StartLine:     int(node.StartPoint().Row) + 1,
		EndLine:       int(node.EndPoint().Row) + 1,
		StartCol:      int(node.StartPoint().Column),
		EndCol:        int(node.EndPoint().Column),
		Signature:     signature,
		Language:      "dockerfile",
		ParsedAtMilli: time.Now().UnixMilli(),
		Exported:      true,
	}
	result.Symbols = append(result.Symbols, sym)
}

// extractEnvInstruction extracts ENV variables.
func (p *DockerfileParser) extractEnvInstruction(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == dockerfileNodeEnvPair {
			p.extractEnvPair(child, node, content, filePath, result)
		}
	}
}

// extractEnvPair extracts a single env pair.
func (p *DockerfileParser) extractEnvPair(pairNode, parentNode *sitter.Node, content []byte, filePath string, result *ParseResult) {
	envName := ""
	envValue := ""

	for i := 0; i < int(pairNode.ChildCount()); i++ {
		child := pairNode.Child(i)
		switch child.Type() {
		case dockerfileNodeUnquotedString:
			if envName == "" {
				envName = string(content[child.StartByte():child.EndByte()])
			} else {
				envValue = string(content[child.StartByte():child.EndByte()])
			}
		case dockerfileNodeDoubleQuotedString, dockerfileNodeSingleQuotedString:
			text := string(content[child.StartByte():child.EndByte()])
			// Remove quotes
			if len(text) >= 2 {
				envValue = text[1 : len(text)-1]
			}
		}
	}

	if envName == "" {
		return
	}

	signature := "ENV " + envName
	if envValue != "" {
		signature += "=" + envValue
	}

	sym := &Symbol{
		ID:            GenerateID(filePath, int(parentNode.StartPoint().Row)+1, envName),
		Name:          envName,
		Kind:          SymbolKindEnvVar,
		FilePath:      filePath,
		StartLine:     int(parentNode.StartPoint().Row) + 1,
		EndLine:       int(parentNode.EndPoint().Row) + 1,
		StartCol:      int(parentNode.StartPoint().Column),
		EndCol:        int(parentNode.EndPoint().Column),
		Signature:     signature,
		Language:      "dockerfile",
		ParsedAtMilli: time.Now().UnixMilli(),
		Exported:      true,
	}
	result.Symbols = append(result.Symbols, sym)
}

// extractLabelInstruction extracts LABEL metadata.
func (p *DockerfileParser) extractLabelInstruction(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == dockerfileNodeLabelPair {
			p.extractLabelPair(child, node, content, filePath, result)
		}
	}
}

// extractLabelPair extracts a single label pair.
func (p *DockerfileParser) extractLabelPair(pairNode, parentNode *sitter.Node, content []byte, filePath string, result *ParseResult) {
	labelKey := ""
	labelValue := ""

	for i := 0; i < int(pairNode.ChildCount()); i++ {
		child := pairNode.Child(i)
		switch child.Type() {
		case dockerfileNodeUnquotedString:
			if labelKey == "" {
				labelKey = string(content[child.StartByte():child.EndByte()])
			} else {
				labelValue = string(content[child.StartByte():child.EndByte()])
			}
		case dockerfileNodeDoubleQuotedString, dockerfileNodeSingleQuotedString:
			text := string(content[child.StartByte():child.EndByte()])
			// Remove quotes
			if len(text) >= 2 {
				labelValue = text[1 : len(text)-1]
			}
		}
	}

	if labelKey == "" {
		return
	}

	signature := "LABEL " + labelKey
	if labelValue != "" {
		signature += "=" + `"` + labelValue + `"`
	}

	sym := &Symbol{
		ID:            GenerateID(filePath, int(parentNode.StartPoint().Row)+1, labelKey),
		Name:          labelKey,
		Kind:          SymbolKindLabel,
		FilePath:      filePath,
		StartLine:     int(parentNode.StartPoint().Row) + 1,
		EndLine:       int(parentNode.EndPoint().Row) + 1,
		StartCol:      int(parentNode.StartPoint().Column),
		EndCol:        int(parentNode.EndPoint().Column),
		Signature:     signature,
		Language:      "dockerfile",
		ParsedAtMilli: time.Now().UnixMilli(),
		Exported:      true,
	}
	result.Symbols = append(result.Symbols, sym)
}

// extractExposeInstruction extracts EXPOSE port definitions.
func (p *DockerfileParser) extractExposeInstruction(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == dockerfileNodeExposePort {
			portText := string(content[child.StartByte():child.EndByte()])

			sym := &Symbol{
				ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, portText),
				Name:          portText,
				Kind:          SymbolKindPort,
				FilePath:      filePath,
				StartLine:     int(node.StartPoint().Row) + 1,
				EndLine:       int(node.EndPoint().Row) + 1,
				StartCol:      int(node.StartPoint().Column),
				EndCol:        int(node.EndPoint().Column),
				Signature:     "EXPOSE " + portText,
				Language:      "dockerfile",
				ParsedAtMilli: time.Now().UnixMilli(),
				Exported:      true,
			}
			result.Symbols = append(result.Symbols, sym)
		}
	}
}

// extractVolumeInstruction extracts VOLUME mount points.
func (p *DockerfileParser) extractVolumeInstruction(node *sitter.Node, content []byte, filePath string, result *ParseResult) {
	var volumes []string

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		switch child.Type() {
		case dockerfileNodePath:
			volumes = append(volumes, string(content[child.StartByte():child.EndByte()]))
		case dockerfileNodeJsonStringArray:
			// Parse JSON array
			for j := 0; j < int(child.ChildCount()); j++ {
				gc := child.Child(j)
				if gc.Type() == dockerfileNodeJsonString {
					text := string(content[gc.StartByte():gc.EndByte()])
					// Remove quotes
					if len(text) >= 2 {
						volumes = append(volumes, text[1:len(text)-1])
					}
				}
			}
		}
	}

	for _, vol := range volumes {
		sym := &Symbol{
			ID:            GenerateID(filePath, int(node.StartPoint().Row)+1, vol),
			Name:          vol,
			Kind:          SymbolKindVolume,
			FilePath:      filePath,
			StartLine:     int(node.StartPoint().Row) + 1,
			EndLine:       int(node.EndPoint().Row) + 1,
			StartCol:      int(node.StartPoint().Column),
			EndCol:        int(node.EndPoint().Column),
			Signature:     "VOLUME " + vol,
			Language:      "dockerfile",
			ParsedAtMilli: time.Now().UnixMilli(),
			Exported:      true,
		}
		result.Symbols = append(result.Symbols, sym)
	}
}
