// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package reason

import (
	"context"
	"strings"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
)

// SignatureParser parses function signatures using tree-sitter.
//
// Description:
//
//	SignatureParser provides accurate, language-aware parsing of function
//	and method signatures. It uses tree-sitter for robust parsing rather
//	than error-prone regex patterns.
//
// Thread Safety:
//
//	SignatureParser is safe for concurrent use. Each ParseSignature call
//	creates its own parser instance internally.
type SignatureParser struct {
	mu sync.RWMutex

	// Cached tree-sitter languages
	goLang *sitter.Language
	pyLang *sitter.Language
	tsLang *sitter.Language
}

// NewSignatureParser creates a new SignatureParser.
//
// Description:
//
//	Creates a parser that can handle Go, Python, and TypeScript signatures.
//	Languages are lazily initialized on first use.
//
// Outputs:
//
//	*SignatureParser - A new signature parser.
func NewSignatureParser() *SignatureParser {
	return &SignatureParser{}
}

// ParseSignature parses a function signature string.
//
// Description:
//
//	Parses the given signature string using tree-sitter for the specified
//	language. Returns structured information about the signature including
//	parameters, return types, and type parameters.
//
// Inputs:
//
//	sig - The signature string to parse.
//	lang - The language ("go", "python", "typescript").
//
// Outputs:
//
//	*ParsedSignature - The parsed signature structure.
//	error - Non-nil if parsing fails or language is unsupported.
//
// Example:
//
//	parser := NewSignatureParser()
//	sig, err := parser.ParseSignature(
//	    "func(ctx context.Context, id string) (*User, error)",
//	    "go",
//	)
//
// Limitations:
//
//   - Requires valid syntax for the target language
//   - May not handle all edge cases for complex signatures
//   - Does not resolve type aliases or imports
func (p *SignatureParser) ParseSignature(sig string, lang string) (*ParsedSignature, error) {
	if sig == "" {
		return nil, ErrInvalidInput
	}

	switch lang {
	case "go":
		return p.parseGoSignature(sig)
	case "python":
		return p.parsePythonSignature(sig)
	case "typescript", "javascript":
		return p.parseTypeScriptSignature(sig)
	default:
		return nil, ErrUnsupportedLanguage
	}
}

// getGoLang returns the Go tree-sitter language, initializing if needed.
func (p *SignatureParser) getGoLang() *sitter.Language {
	p.mu.RLock()
	if p.goLang != nil {
		lang := p.goLang
		p.mu.RUnlock()
		return lang
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.goLang == nil {
		p.goLang = golang.GetLanguage()
	}
	return p.goLang
}

// getPythonLang returns the Python tree-sitter language, initializing if needed.
func (p *SignatureParser) getPythonLang() *sitter.Language {
	p.mu.RLock()
	if p.pyLang != nil {
		lang := p.pyLang
		p.mu.RUnlock()
		return lang
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pyLang == nil {
		p.pyLang = python.GetLanguage()
	}
	return p.pyLang
}

// getTypeScriptLang returns the TypeScript tree-sitter language, initializing if needed.
func (p *SignatureParser) getTypeScriptLang() *sitter.Language {
	p.mu.RLock()
	if p.tsLang != nil {
		lang := p.tsLang
		p.mu.RUnlock()
		return lang
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tsLang == nil {
		p.tsLang = typescript.GetLanguage()
	}
	return p.tsLang
}

// parseGoSignature parses a Go function signature.
func (p *SignatureParser) parseGoSignature(sig string) (*ParsedSignature, error) {
	// Wrap in a minimal Go file to make it parseable
	// Handle both "func(...)" and "func Name(...)" formats
	var code string
	if strings.HasPrefix(sig, "func ") && !strings.HasPrefix(sig, "func(") {
		// Named function: "func Name(...)"
		code = "package p\n" + sig + " {}"
	} else if strings.HasPrefix(sig, "func(") {
		// Anonymous function type: "func(...)"
		code = "package p\nvar x " + sig
	} else {
		// Try to parse as-is with wrapper
		code = "package p\n" + sig + " {}"
	}

	parser := sitter.NewParser()
	parser.SetLanguage(p.getGoLang())

	tree, err := parser.ParseCtx(context.Background(), nil, []byte(code))
	if err != nil {
		return nil, ErrParseFailure
	}
	defer tree.Close()

	root := tree.RootNode()
	if root.HasError() {
		// Try alternate parse strategies
		return p.parseGoSignatureFallback(sig)
	}

	result := &ParsedSignature{
		Language:   "go",
		Parameters: make([]ParameterInfo, 0),
		Returns:    make([]TypeInfo, 0),
	}

	// Find the function declaration or type
	p.extractGoSignatureFromNode(root, []byte(code), result)

	return result, nil
}

// parseGoSignatureFallback handles edge cases in Go signature parsing.
func (p *SignatureParser) parseGoSignatureFallback(sig string) (*ParsedSignature, error) {
	result := &ParsedSignature{
		Language:   "go",
		Parameters: make([]ParameterInfo, 0),
		Returns:    make([]TypeInfo, 0),
	}

	// Simple fallback: extract basic info from the signature string
	// This handles cases where tree-sitter can't parse the fragment

	// Extract function name if present
	if strings.HasPrefix(sig, "func ") {
		rest := sig[5:]
		if idx := strings.Index(rest, "("); idx > 0 {
			namePart := rest[:idx]
			// Check for receiver
			if strings.HasPrefix(namePart, "(") {
				// Method with receiver
				endReceiver := strings.Index(namePart, ")")
				if endReceiver > 0 {
					receiverStr := namePart[1:endReceiver]
					result.Receiver = parseGoTypeString(strings.TrimSpace(receiverStr))
					result.Name = strings.TrimSpace(namePart[endReceiver+1:])
				}
			} else {
				result.Name = strings.TrimSpace(namePart)
			}
		}
	}

	// Extract parameters
	if start := strings.Index(sig, "("); start >= 0 {
		depth := 0
		paramStart := start + 1
		for i := start; i < len(sig); i++ {
			switch sig[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					// Found the end of parameters
					paramStr := sig[paramStart:i]
					result.Parameters = parseGoParameters(paramStr)

					// Look for return types after the closing paren
					rest := strings.TrimSpace(sig[i+1:])
					if len(rest) > 0 {
						result.Returns = parseGoReturns(rest)
					}
					return result, nil
				}
			}
		}
	}

	return result, nil
}

// extractGoSignatureFromNode extracts signature info from a tree-sitter node.
func (p *SignatureParser) extractGoSignatureFromNode(node *sitter.Node, code []byte, result *ParsedSignature) {
	nodeType := node.Type()

	switch nodeType {
	case "function_declaration":
		// Extract name
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			result.Name = string(code[nameNode.StartByte():nameNode.EndByte()])
		}
		// Extract parameters
		if params := node.ChildByFieldName("parameters"); params != nil {
			result.Parameters = p.extractGoParameters(params, code)
		}
		// Extract return type
		if retNode := node.ChildByFieldName("result"); retNode != nil {
			result.Returns = p.extractGoReturnTypes(retNode, code)
		}
		// Extract type parameters (generics)
		if typeParams := node.ChildByFieldName("type_parameters"); typeParams != nil {
			result.TypeParams = p.extractGoTypeParams(typeParams, code)
		}
		return

	case "method_declaration":
		// Extract receiver
		if receiver := node.ChildByFieldName("receiver"); receiver != nil {
			result.Receiver = p.extractGoReceiver(receiver, code)
		}
		// Extract name
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			result.Name = string(code[nameNode.StartByte():nameNode.EndByte()])
		}
		// Extract parameters
		if params := node.ChildByFieldName("parameters"); params != nil {
			result.Parameters = p.extractGoParameters(params, code)
		}
		// Extract return type
		if retNode := node.ChildByFieldName("result"); retNode != nil {
			result.Returns = p.extractGoReturnTypes(retNode, code)
		}
		return

	case "function_type":
		// Anonymous function type
		if params := node.ChildByFieldName("parameters"); params != nil {
			result.Parameters = p.extractGoParameters(params, code)
		}
		if retNode := node.ChildByFieldName("result"); retNode != nil {
			result.Returns = p.extractGoReturnTypes(retNode, code)
		}
		return
	}

	// Recurse into children
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		p.extractGoSignatureFromNode(child, code, result)
	}
}

// extractGoParameters extracts parameters from a parameter_list node.
func (p *SignatureParser) extractGoParameters(node *sitter.Node, code []byte) []ParameterInfo {
	params := make([]ParameterInfo, 0)

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "parameter_declaration" {
			param := ParameterInfo{}

			// Extract name(s) and type
			for j := 0; j < int(child.ChildCount()); j++ {
				paramChild := child.Child(j)
				switch paramChild.Type() {
				case "identifier":
					param.Name = string(code[paramChild.StartByte():paramChild.EndByte()])
				case "variadic_parameter_declaration":
					param.Type.IsVariadic = true
					// Get the type from the variadic declaration
					if typeNode := paramChild.ChildByFieldName("type"); typeNode != nil {
						param.Type = p.extractGoType(typeNode, code)
						param.Type.IsVariadic = true
					}
				default:
					// Assume it's a type
					if param.Type.Name == "" {
						param.Type = p.extractGoType(paramChild, code)
					}
				}
			}

			if param.Type.Name != "" || param.Name != "" {
				params = append(params, param)
			}
		} else if child.Type() == "variadic_parameter_declaration" {
			param := ParameterInfo{}
			param.Type.IsVariadic = true

			if nameNode := child.ChildByFieldName("name"); nameNode != nil {
				param.Name = string(code[nameNode.StartByte():nameNode.EndByte()])
			}
			if typeNode := child.ChildByFieldName("type"); typeNode != nil {
				param.Type = p.extractGoType(typeNode, code)
				param.Type.IsVariadic = true
			}
			params = append(params, param)
		}
	}

	return params
}

// extractGoReturnTypes extracts return types from a result node.
func (p *SignatureParser) extractGoReturnTypes(node *sitter.Node, code []byte) []TypeInfo {
	returns := make([]TypeInfo, 0)

	nodeType := node.Type()

	if nodeType == "parameter_list" {
		// Multiple returns: (type1, type2)
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type() == "parameter_declaration" {
				// Named return or just type
				for j := 0; j < int(child.ChildCount()); j++ {
					paramChild := child.Child(j)
					if paramChild.Type() != "identifier" && paramChild.Type() != "," {
						returns = append(returns, p.extractGoType(paramChild, code))
					}
				}
			}
		}
	} else {
		// Single return type
		returns = append(returns, p.extractGoType(node, code))
	}

	return returns
}

// extractGoType extracts TypeInfo from a type node.
func (p *SignatureParser) extractGoType(node *sitter.Node, code []byte) TypeInfo {
	typeInfo := TypeInfo{}
	nodeType := node.Type()

	switch nodeType {
	case "pointer_type":
		typeInfo.IsPointer = true
		if child := node.Child(0); child != nil {
			inner := p.extractGoType(child, code)
			typeInfo.Name = "*" + inner.Name
			typeInfo.Package = inner.Package
		}

	case "slice_type":
		typeInfo.IsSlice = true
		if elemNode := node.ChildByFieldName("element"); elemNode != nil {
			typeInfo.ElementType = new(TypeInfo)
			*typeInfo.ElementType = p.extractGoType(elemNode, code)
		}
		typeInfo.Name = string(code[node.StartByte():node.EndByte()])

	case "map_type":
		typeInfo.IsMap = true
		if keyNode := node.ChildByFieldName("key"); keyNode != nil {
			typeInfo.KeyType = new(TypeInfo)
			*typeInfo.KeyType = p.extractGoType(keyNode, code)
		}
		if valueNode := node.ChildByFieldName("value"); valueNode != nil {
			typeInfo.ElementType = new(TypeInfo)
			*typeInfo.ElementType = p.extractGoType(valueNode, code)
		}
		typeInfo.Name = string(code[node.StartByte():node.EndByte()])

	case "channel_type":
		typeInfo.IsChannel = true
		typeInfo.Name = string(code[node.StartByte():node.EndByte()])

	case "qualified_type":
		// Package-qualified type: pkg.Type
		typeInfo.Name = string(code[node.StartByte():node.EndByte()])
		if pkgNode := node.ChildByFieldName("package"); pkgNode != nil {
			typeInfo.Package = string(code[pkgNode.StartByte():pkgNode.EndByte()])
		}

	case "generic_type":
		// Generic type: Type[T, U]
		typeInfo.Name = string(code[node.StartByte():node.EndByte()])
		if argsNode := node.ChildByFieldName("type_arguments"); argsNode != nil {
			for i := 0; i < int(argsNode.ChildCount()); i++ {
				child := argsNode.Child(i)
				if child.Type() != "," && child.Type() != "[" && child.Type() != "]" {
					typeInfo.TypeParams = append(typeInfo.TypeParams,
						string(code[child.StartByte():child.EndByte()]))
				}
			}
		}

	default:
		// Simple type name
		typeInfo.Name = string(code[node.StartByte():node.EndByte()])
	}

	return typeInfo
}

// extractGoReceiver extracts receiver info from a parameter_list node.
func (p *SignatureParser) extractGoReceiver(node *sitter.Node, code []byte) *TypeInfo {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "parameter_declaration" {
			for j := 0; j < int(child.ChildCount()); j++ {
				paramChild := child.Child(j)
				if paramChild.Type() != "identifier" && paramChild.Type() != "," {
					typeInfo := p.extractGoType(paramChild, code)
					return &typeInfo
				}
			}
		}
	}
	return nil
}

// extractGoTypeParams extracts type parameters from a type_parameter_list node.
func (p *SignatureParser) extractGoTypeParams(node *sitter.Node, code []byte) []TypeParamInfo {
	typeParams := make([]TypeParamInfo, 0)

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "type_parameter_declaration" {
			tp := TypeParamInfo{}
			if nameNode := child.ChildByFieldName("name"); nameNode != nil {
				tp.Name = string(code[nameNode.StartByte():nameNode.EndByte()])
			}
			if constraintNode := child.ChildByFieldName("constraint"); constraintNode != nil {
				tp.Constraint = string(code[constraintNode.StartByte():constraintNode.EndByte()])
			}
			if tp.Name != "" {
				typeParams = append(typeParams, tp)
			}
		}
	}

	return typeParams
}

// parsePythonSignature parses a Python function signature.
func (p *SignatureParser) parsePythonSignature(sig string) (*ParsedSignature, error) {
	// Wrap in minimal Python to make it parseable
	code := sig + ":\n    pass"

	parser := sitter.NewParser()
	parser.SetLanguage(p.getPythonLang())

	tree, err := parser.ParseCtx(context.Background(), nil, []byte(code))
	if err != nil {
		return nil, ErrParseFailure
	}
	defer tree.Close()

	root := tree.RootNode()
	result := &ParsedSignature{
		Language:   "python",
		Parameters: make([]ParameterInfo, 0),
		Returns:    make([]TypeInfo, 0),
	}

	p.extractPythonSignatureFromNode(root, []byte(code), result)

	return result, nil
}

// extractPythonSignatureFromNode extracts signature info from a Python AST node.
func (p *SignatureParser) extractPythonSignatureFromNode(node *sitter.Node, code []byte, result *ParsedSignature) {
	nodeType := node.Type()

	if nodeType == "function_definition" {
		// Extract name
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			result.Name = string(code[nameNode.StartByte():nameNode.EndByte()])
		}

		// Extract parameters
		if params := node.ChildByFieldName("parameters"); params != nil {
			result.Parameters = p.extractPythonParameters(params, code)
		}

		// Extract return type annotation
		if retNode := node.ChildByFieldName("return_type"); retNode != nil {
			retType := TypeInfo{
				Name: string(code[retNode.StartByte():retNode.EndByte()]),
			}
			result.Returns = append(result.Returns, retType)
		}

		return
	}

	// Recurse into children
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		p.extractPythonSignatureFromNode(child, code, result)
	}
}

// extractPythonParameters extracts parameters from Python parameters node.
func (p *SignatureParser) extractPythonParameters(node *sitter.Node, code []byte) []ParameterInfo {
	params := make([]ParameterInfo, 0)

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		childType := child.Type()

		switch childType {
		case "identifier":
			// Simple parameter
			params = append(params, ParameterInfo{
				Name: string(code[child.StartByte():child.EndByte()]),
			})

		case "typed_parameter":
			param := ParameterInfo{}
			if nameNode := child.ChildByFieldName("name"); nameNode != nil {
				param.Name = string(code[nameNode.StartByte():nameNode.EndByte()])
			}
			if typeNode := child.ChildByFieldName("type"); typeNode != nil {
				param.Type.Name = string(code[typeNode.StartByte():typeNode.EndByte()])
			}
			params = append(params, param)

		case "default_parameter":
			param := ParameterInfo{Optional: true}
			if nameNode := child.ChildByFieldName("name"); nameNode != nil {
				param.Name = string(code[nameNode.StartByte():nameNode.EndByte()])
			}
			if valueNode := child.ChildByFieldName("value"); valueNode != nil {
				param.Default = string(code[valueNode.StartByte():valueNode.EndByte()])
			}
			params = append(params, param)

		case "typed_default_parameter":
			param := ParameterInfo{Optional: true}
			if nameNode := child.ChildByFieldName("name"); nameNode != nil {
				param.Name = string(code[nameNode.StartByte():nameNode.EndByte()])
			}
			if typeNode := child.ChildByFieldName("type"); typeNode != nil {
				param.Type.Name = string(code[typeNode.StartByte():typeNode.EndByte()])
			}
			if valueNode := child.ChildByFieldName("value"); valueNode != nil {
				param.Default = string(code[valueNode.StartByte():valueNode.EndByte()])
			}
			params = append(params, param)

		case "list_splat_pattern", "dictionary_splat_pattern":
			param := ParameterInfo{
				Type: TypeInfo{IsVariadic: true},
			}
			// Get the name from the child
			for j := 0; j < int(child.ChildCount()); j++ {
				subChild := child.Child(j)
				if subChild.Type() == "identifier" {
					param.Name = string(code[subChild.StartByte():subChild.EndByte()])
					break
				}
			}
			params = append(params, param)
		}
	}

	return params
}

// parseTypeScriptSignature parses a TypeScript/JavaScript function signature.
func (p *SignatureParser) parseTypeScriptSignature(sig string) (*ParsedSignature, error) {
	// Wrap in minimal TypeScript to make it parseable
	var code string
	if strings.HasPrefix(sig, "function") {
		code = sig + " {}"
	} else {
		// Arrow function or method signature
		code = "const x = " + sig + " => {}"
	}

	parser := sitter.NewParser()
	parser.SetLanguage(p.getTypeScriptLang())

	tree, err := parser.ParseCtx(context.Background(), nil, []byte(code))
	if err != nil {
		return nil, ErrParseFailure
	}
	defer tree.Close()

	root := tree.RootNode()
	result := &ParsedSignature{
		Language:   "typescript",
		Parameters: make([]ParameterInfo, 0),
		Returns:    make([]TypeInfo, 0),
	}

	p.extractTypeScriptSignatureFromNode(root, []byte(code), result)

	return result, nil
}

// extractTypeScriptSignatureFromNode extracts signature info from a TypeScript AST node.
func (p *SignatureParser) extractTypeScriptSignatureFromNode(node *sitter.Node, code []byte, result *ParsedSignature) {
	nodeType := node.Type()

	switch nodeType {
	case "function_declaration", "function":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			result.Name = string(code[nameNode.StartByte():nameNode.EndByte()])
		}
		if params := node.ChildByFieldName("parameters"); params != nil {
			result.Parameters = p.extractTypeScriptParameters(params, code)
		}
		if retNode := node.ChildByFieldName("return_type"); retNode != nil {
			result.Returns = append(result.Returns, TypeInfo{
				Name: string(code[retNode.StartByte():retNode.EndByte()]),
			})
		}
		return

	case "arrow_function":
		if params := node.ChildByFieldName("parameters"); params != nil {
			result.Parameters = p.extractTypeScriptParameters(params, code)
		} else if param := node.ChildByFieldName("parameter"); param != nil {
			// Single unparenthesized parameter
			result.Parameters = append(result.Parameters, ParameterInfo{
				Name: string(code[param.StartByte():param.EndByte()]),
			})
		}
		if retNode := node.ChildByFieldName("return_type"); retNode != nil {
			result.Returns = append(result.Returns, TypeInfo{
				Name: string(code[retNode.StartByte():retNode.EndByte()]),
			})
		}
		return

	case "method_definition":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			result.Name = string(code[nameNode.StartByte():nameNode.EndByte()])
		}
		if params := node.ChildByFieldName("parameters"); params != nil {
			result.Parameters = p.extractTypeScriptParameters(params, code)
		}
		return
	}

	// Recurse into children
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		p.extractTypeScriptSignatureFromNode(child, code, result)
	}
}

// extractTypeScriptParameters extracts parameters from TypeScript formal_parameters node.
func (p *SignatureParser) extractTypeScriptParameters(node *sitter.Node, code []byte) []ParameterInfo {
	params := make([]ParameterInfo, 0)

	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		childType := child.Type()

		switch childType {
		case "required_parameter", "optional_parameter":
			param := ParameterInfo{}
			if childType == "optional_parameter" {
				param.Optional = true
			}

			if patternNode := child.ChildByFieldName("pattern"); patternNode != nil {
				param.Name = string(code[patternNode.StartByte():patternNode.EndByte()])
			}
			if typeNode := child.ChildByFieldName("type"); typeNode != nil {
				param.Type.Name = string(code[typeNode.StartByte():typeNode.EndByte()])
			}
			if valueNode := child.ChildByFieldName("value"); valueNode != nil {
				param.Default = string(code[valueNode.StartByte():valueNode.EndByte()])
				param.Optional = true
			}
			params = append(params, param)

		case "rest_pattern":
			param := ParameterInfo{
				Type: TypeInfo{IsVariadic: true},
			}
			// Get name from child
			for j := 0; j < int(child.ChildCount()); j++ {
				subChild := child.Child(j)
				if subChild.Type() == "identifier" {
					param.Name = string(code[subChild.StartByte():subChild.EndByte()])
					break
				}
			}
			params = append(params, param)
		}
	}

	return params
}

// Helper functions for fallback parsing

func parseGoTypeString(s string) *TypeInfo {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	info := &TypeInfo{}

	// Handle pointer
	if strings.HasPrefix(s, "*") {
		info.IsPointer = true
		s = s[1:]
	}

	// Handle slice
	if strings.HasPrefix(s, "[]") {
		info.IsSlice = true
		s = s[2:]
	}

	// Handle map
	if strings.HasPrefix(s, "map[") {
		info.IsMap = true
	}

	info.Name = s
	return info
}

func parseGoParameters(paramStr string) []ParameterInfo {
	params := make([]ParameterInfo, 0)
	if paramStr == "" {
		return params
	}

	// Split by comma, being careful about nested types
	depth := 0
	current := ""
	for _, r := range paramStr {
		switch r {
		case '(', '[', '{':
			depth++
			current += string(r)
		case ')', ']', '}':
			depth--
			current += string(r)
		case ',':
			if depth == 0 {
				if p := parseGoSingleParam(strings.TrimSpace(current)); p != nil {
					params = append(params, *p)
				}
				current = ""
			} else {
				current += string(r)
			}
		default:
			current += string(r)
		}
	}
	if current != "" {
		if p := parseGoSingleParam(strings.TrimSpace(current)); p != nil {
			params = append(params, *p)
		}
	}

	return params
}

func parseGoSingleParam(s string) *ParameterInfo {
	if s == "" {
		return nil
	}

	param := &ParameterInfo{}

	// Handle variadic
	if strings.HasPrefix(s, "...") {
		param.Type.IsVariadic = true
		s = s[3:]
	}

	// Split into name and type
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return nil
	}

	if len(parts) == 1 {
		// Just a type
		param.Type.Name = parts[0]
	} else {
		// Name and type
		param.Name = parts[0]
		param.Type.Name = strings.Join(parts[1:], " ")
	}

	return param
}

func parseGoReturns(returnStr string) []TypeInfo {
	returns := make([]TypeInfo, 0)

	returnStr = strings.TrimSpace(returnStr)
	if returnStr == "" {
		return returns
	}

	// Check for parenthesized returns
	if strings.HasPrefix(returnStr, "(") && strings.HasSuffix(returnStr, ")") {
		returnStr = returnStr[1 : len(returnStr)-1]
		// Parse multiple returns
		parts := strings.Split(returnStr, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			// Skip named return variable names
			fields := strings.Fields(part)
			if len(fields) > 1 {
				part = fields[len(fields)-1]
			}
			if part != "" {
				returns = append(returns, TypeInfo{Name: part})
			}
		}
	} else {
		// Single return type
		returns = append(returns, TypeInfo{Name: returnStr})
	}

	return returns
}

// CompareSignatures compares two signatures and returns breaking changes.
//
// Description:
//
//	Compares a current signature against a proposed signature and identifies
//	any breaking changes that would affect callers.
//
// Inputs:
//
//	current - The current signature.
//	proposed - The proposed new signature.
//
// Outputs:
//
//	[]BreakingChange - List of breaking changes detected.
//
// Thread Safety:
//
//	This function is safe for concurrent use.
func CompareSignatures(current, proposed *ParsedSignature) []BreakingChange {
	changes := make([]BreakingChange, 0)

	if current == nil || proposed == nil {
		return changes
	}

	// Check parameter count
	if len(proposed.Parameters) > len(current.Parameters) {
		// New required parameters added
		newParams := proposed.Parameters[len(current.Parameters):]
		for _, param := range newParams {
			if !param.Optional {
				changes = append(changes, BreakingChange{
					Type:        BreakingChangeSignature,
					Description: "New required parameter added: " + param.Name,
					Severity:    SeverityHigh,
					AutoFixable: false,
				})
			}
		}
	}

	// Check parameter order/types
	minLen := len(current.Parameters)
	if len(proposed.Parameters) < minLen {
		minLen = len(proposed.Parameters)
		// Parameters removed
		for i := minLen; i < len(current.Parameters); i++ {
			changes = append(changes, BreakingChange{
				Type:        BreakingChangeSignature,
				Description: "Parameter removed: " + current.Parameters[i].Name,
				Severity:    SeverityHigh,
				AutoFixable: false,
			})
		}
	}

	for i := 0; i < minLen; i++ {
		curr := current.Parameters[i]
		prop := proposed.Parameters[i]

		// Check type change
		if !typesEqual(curr.Type, prop.Type) {
			changes = append(changes, BreakingChange{
				Type: BreakingChangeSignature,
				Description: "Parameter type changed: " + curr.Name +
					" from " + curr.Type.Name + " to " + prop.Type.Name,
				Severity:    SeverityHigh,
				AutoFixable: false,
			})
		}
	}

	// Check return type changes
	if len(current.Returns) != len(proposed.Returns) {
		changes = append(changes, BreakingChange{
			Type:        BreakingChangeReturn,
			Description: "Return type count changed",
			Severity:    SeverityHigh,
			AutoFixable: false,
		})
	} else {
		for i := 0; i < len(current.Returns); i++ {
			if !typesEqual(current.Returns[i], proposed.Returns[i]) {
				changes = append(changes, BreakingChange{
					Type: BreakingChangeReturn,
					Description: "Return type changed from " +
						current.Returns[i].Name + " to " + proposed.Returns[i].Name,
					Severity:    SeverityHigh,
					AutoFixable: false,
				})
			}
		}
	}

	// Check receiver change (method → function or vice versa)
	if (current.Receiver == nil) != (proposed.Receiver == nil) {
		changes = append(changes, BreakingChange{
			Type:        BreakingChangeSignature,
			Description: "Method/function conversion",
			Severity:    SeverityCritical,
			AutoFixable: false,
		})
	} else if current.Receiver != nil && proposed.Receiver != nil {
		if !typesEqual(*current.Receiver, *proposed.Receiver) {
			changes = append(changes, BreakingChange{
				Type: BreakingChangeSignature,
				Description: "Receiver type changed from " +
					current.Receiver.Name + " to " + proposed.Receiver.Name,
				Severity:    SeverityHigh,
				AutoFixable: false,
			})
		}
	}

	return changes
}

// typesEqual compares two TypeInfo for equality.
func typesEqual(a, b TypeInfo) bool {
	if a.Name != b.Name {
		return false
	}
	if a.IsPointer != b.IsPointer {
		return false
	}
	if a.IsSlice != b.IsSlice {
		return false
	}
	if a.IsMap != b.IsMap {
		return false
	}
	if a.IsChannel != b.IsChannel {
		return false
	}
	if a.IsVariadic != b.IsVariadic {
		return false
	}
	return true
}
