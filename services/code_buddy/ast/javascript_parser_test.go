package ast

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestJavaScriptParser_Parse_EmptyFile(t *testing.T) {
	parser := NewJavaScriptParser()
	result, err := parser.Parse(context.Background(), []byte(""), "empty.js")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.Language != "javascript" {
		t.Errorf("expected language 'javascript', got %q", result.Language)
	}
	if result.FilePath != "empty.js" {
		t.Errorf("expected filePath 'empty.js', got %q", result.FilePath)
	}
	if result.Hash == "" {
		t.Error("expected hash to be set")
	}
}

func TestJavaScriptParser_Parse_FunctionDeclaration(t *testing.T) {
	parser := NewJavaScriptParser()
	content := `
function greet(name) {
    return "Hello, " + name;
}
`
	result, err := parser.Parse(context.Background(), []byte(content), "greet.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the function symbol
	var fn *Symbol
	for _, sym := range result.Symbols {
		if sym.Name == "greet" && sym.Kind == SymbolKindFunction {
			fn = sym
			break
		}
	}

	if fn == nil {
		t.Fatal("expected to find function 'greet'")
	}
	if fn.Language != "javascript" {
		t.Errorf("expected language 'javascript', got %q", fn.Language)
	}
	if !strings.Contains(fn.Signature, "greet(name)") {
		t.Errorf("expected signature to contain 'greet(name)', got %q", fn.Signature)
	}
}

func TestJavaScriptParser_Parse_AsyncFunction(t *testing.T) {
	parser := NewJavaScriptParser()
	content := `
async function fetchData(url) {
    const response = await fetch(url);
    return response.json();
}
`
	result, err := parser.Parse(context.Background(), []byte(content), "async.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var fn *Symbol
	for _, sym := range result.Symbols {
		if sym.Name == "fetchData" {
			fn = sym
			break
		}
	}

	if fn == nil {
		t.Fatal("expected to find function 'fetchData'")
	}
	if fn.Metadata == nil || !fn.Metadata.IsAsync {
		t.Error("expected function to be marked as async")
	}
	if !strings.Contains(fn.Signature, "async") {
		t.Errorf("expected signature to contain 'async', got %q", fn.Signature)
	}
}

func TestJavaScriptParser_Parse_GeneratorFunction(t *testing.T) {
	parser := NewJavaScriptParser()
	content := `
function* generateIds() {
    let id = 0;
    while (true) yield id++;
}
`
	result, err := parser.Parse(context.Background(), []byte(content), "generator.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var fn *Symbol
	for _, sym := range result.Symbols {
		if sym.Name == "generateIds" {
			fn = sym
			break
		}
	}

	if fn == nil {
		t.Fatal("expected to find function 'generateIds'")
	}
	if fn.Metadata == nil || !fn.Metadata.IsGenerator {
		t.Error("expected function to be marked as generator")
	}
}

func TestJavaScriptParser_Parse_ClassDeclaration(t *testing.T) {
	parser := NewJavaScriptParser()
	content := `
class UserService {
    constructor(db) {
        this.db = db;
    }

    getUser(id) {
        return this.db.find(id);
    }
}
`
	result, err := parser.Parse(context.Background(), []byte(content), "service.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var class *Symbol
	for _, sym := range result.Symbols {
		if sym.Name == "UserService" && sym.Kind == SymbolKindClass {
			class = sym
			break
		}
	}

	if class == nil {
		t.Fatal("expected to find class 'UserService'")
	}
	if len(class.Children) < 2 {
		t.Errorf("expected at least 2 children (constructor, getUser), got %d", len(class.Children))
	}

	// Check for constructor
	var constructor *Symbol
	for _, child := range class.Children {
		if child.Name == "constructor" {
			constructor = child
			break
		}
	}
	if constructor == nil {
		t.Error("expected to find constructor method")
	}
}

func TestJavaScriptParser_Parse_ClassExtends(t *testing.T) {
	parser := NewJavaScriptParser()
	content := `
class EventEmitter {}

class MyEmitter extends EventEmitter {
    emit(event) {
        console.log(event);
    }
}
`
	result, err := parser.Parse(context.Background(), []byte(content), "emitter.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var class *Symbol
	for _, sym := range result.Symbols {
		if sym.Name == "MyEmitter" {
			class = sym
			break
		}
	}

	if class == nil {
		t.Fatal("expected to find class 'MyEmitter'")
	}
	if class.Metadata == nil || class.Metadata.Extends != "EventEmitter" {
		t.Errorf("expected extends 'EventEmitter', got %v", class.Metadata)
	}
	if !strings.Contains(class.Signature, "extends EventEmitter") {
		t.Errorf("expected signature to contain 'extends EventEmitter', got %q", class.Signature)
	}
}

func TestJavaScriptParser_Parse_PrivateField(t *testing.T) {
	parser := NewJavaScriptParser()
	content := `
class Counter {
    #count = 0;
    publicValue = 1;

    increment() {
        this.#count++;
    }
}
`
	result, err := parser.Parse(context.Background(), []byte(content), "counter.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var class *Symbol
	for _, sym := range result.Symbols {
		if sym.Name == "Counter" {
			class = sym
			break
		}
	}

	if class == nil {
		t.Fatal("expected to find class 'Counter'")
	}

	var privateField, publicField *Symbol
	for _, child := range class.Children {
		if child.Name == "#count" {
			privateField = child
		}
		if child.Name == "publicValue" {
			publicField = child
		}
	}

	if privateField == nil {
		t.Error("expected to find private field '#count'")
	} else {
		if privateField.Exported {
			t.Error("expected private field to not be exported")
		}
		if privateField.Metadata == nil || privateField.Metadata.AccessModifier != "private" {
			t.Error("expected private field to have 'private' access modifier")
		}
	}

	if publicField == nil {
		t.Error("expected to find public field 'publicValue'")
	} else if !publicField.Exported {
		t.Error("expected public field to be exported")
	}
}

func TestJavaScriptParser_Parse_StaticMethod(t *testing.T) {
	parser := NewJavaScriptParser()
	content := `
class Factory {
    static create() {
        return new Factory();
    }
}
`
	result, err := parser.Parse(context.Background(), []byte(content), "factory.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var class *Symbol
	for _, sym := range result.Symbols {
		if sym.Name == "Factory" {
			class = sym
			break
		}
	}

	if class == nil {
		t.Fatal("expected to find class 'Factory'")
	}

	var staticMethod *Symbol
	for _, child := range class.Children {
		if child.Name == "create" {
			staticMethod = child
			break
		}
	}

	if staticMethod == nil {
		t.Fatal("expected to find static method 'create'")
	}
	if staticMethod.Metadata == nil || !staticMethod.Metadata.IsStatic {
		t.Error("expected method to be marked as static")
	}
	if !strings.Contains(staticMethod.Signature, "static") {
		t.Errorf("expected signature to contain 'static', got %q", staticMethod.Signature)
	}
}

func TestJavaScriptParser_Parse_ArrowFunction(t *testing.T) {
	parser := NewJavaScriptParser()
	content := `
const greet = (name) => {
    return "Hello, " + name;
};

const double = x => x * 2;

const asyncFetch = async (url) => {
    return fetch(url);
};
`
	result, err := parser.Parse(context.Background(), []byte(content), "arrows.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find arrow functions
	var greet, double, asyncFetch *Symbol
	for _, sym := range result.Symbols {
		switch sym.Name {
		case "greet":
			greet = sym
		case "double":
			double = sym
		case "asyncFetch":
			asyncFetch = sym
		}
	}

	if greet == nil {
		t.Error("expected to find 'greet' arrow function")
	} else if greet.Kind != SymbolKindFunction {
		t.Errorf("expected greet to be SymbolKindFunction, got %v", greet.Kind)
	}

	if double == nil {
		t.Error("expected to find 'double' arrow function")
	}

	if asyncFetch == nil {
		t.Error("expected to find 'asyncFetch' arrow function")
	} else if asyncFetch.Metadata == nil || !asyncFetch.Metadata.IsAsync {
		t.Error("expected asyncFetch to be marked as async")
	}
}

func TestJavaScriptParser_Parse_NamedImport(t *testing.T) {
	parser := NewJavaScriptParser()
	content := `import { useState, useEffect } from 'react';`

	result, err := parser.Parse(context.Background(), []byte(content), "app.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Imports) == 0 {
		t.Fatal("expected at least one import")
	}

	imp := result.Imports[0]
	if imp.Path != "react" {
		t.Errorf("expected path 'react', got %q", imp.Path)
	}
	if len(imp.Names) != 2 {
		t.Errorf("expected 2 named imports, got %d", len(imp.Names))
	}
	if !imp.IsModule {
		t.Error("expected IsModule to be true")
	}
}

func TestJavaScriptParser_Parse_DefaultImport(t *testing.T) {
	parser := NewJavaScriptParser()
	content := `import React from 'react';`

	result, err := parser.Parse(context.Background(), []byte(content), "app.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Imports) == 0 {
		t.Fatal("expected at least one import")
	}

	imp := result.Imports[0]
	if imp.Path != "react" {
		t.Errorf("expected path 'react', got %q", imp.Path)
	}
	if imp.Alias != "React" {
		t.Errorf("expected alias 'React', got %q", imp.Alias)
	}
	if !imp.IsDefault {
		t.Error("expected IsDefault to be true")
	}
}

func TestJavaScriptParser_Parse_NamespaceImport(t *testing.T) {
	parser := NewJavaScriptParser()
	content := `import * as utils from './utils.js';`

	result, err := parser.Parse(context.Background(), []byte(content), "app.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Imports) == 0 {
		t.Fatal("expected at least one import")
	}

	imp := result.Imports[0]
	if imp.Path != "./utils.js" {
		t.Errorf("expected path './utils.js', got %q", imp.Path)
	}
	if imp.Alias != "utils" {
		t.Errorf("expected alias 'utils', got %q", imp.Alias)
	}
	if !imp.IsNamespace {
		t.Error("expected IsNamespace to be true")
	}
}

func TestJavaScriptParser_Parse_CommonJSRequire(t *testing.T) {
	parser := NewJavaScriptParser()
	content := `const fs = require('fs');`

	result, err := parser.Parse(context.Background(), []byte(content), "app.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Imports) == 0 {
		t.Fatal("expected at least one import")
	}

	imp := result.Imports[0]
	if imp.Path != "fs" {
		t.Errorf("expected path 'fs', got %q", imp.Path)
	}
	if imp.Alias != "fs" {
		t.Errorf("expected alias 'fs', got %q", imp.Alias)
	}
	if !imp.IsCommonJS {
		t.Error("expected IsCommonJS to be true")
	}
}

func TestJavaScriptParser_Parse_ExportedFunction(t *testing.T) {
	parser := NewJavaScriptParser()
	content := `
export function greet(name) {
    return "Hello, " + name;
}
`
	result, err := parser.Parse(context.Background(), []byte(content), "greet.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var fn *Symbol
	for _, sym := range result.Symbols {
		if sym.Name == "greet" && sym.Kind == SymbolKindFunction {
			fn = sym
			break
		}
	}

	if fn == nil {
		t.Fatal("expected to find function 'greet'")
	}
	if !fn.Exported {
		t.Error("expected function to be exported")
	}
}

func TestJavaScriptParser_Parse_ExportedClass(t *testing.T) {
	parser := NewJavaScriptParser()
	content := `
export class UserService {
    getUser(id) {
        return null;
    }
}
`
	result, err := parser.Parse(context.Background(), []byte(content), "service.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var class *Symbol
	for _, sym := range result.Symbols {
		if sym.Name == "UserService" && sym.Kind == SymbolKindClass {
			class = sym
			break
		}
	}

	if class == nil {
		t.Fatal("expected to find class 'UserService'")
	}
	if !class.Exported {
		t.Error("expected class to be exported")
	}
}

func TestJavaScriptParser_Parse_ExportDefault(t *testing.T) {
	parser := NewJavaScriptParser()
	content := `
class UserService {}
export default UserService;
`
	result, err := parser.Parse(context.Background(), []byte(content), "service.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have both the class and the default export
	exportedCount := 0
	for _, sym := range result.Symbols {
		if sym.Name == "UserService" && sym.Exported {
			exportedCount++
		}
	}

	if exportedCount == 0 {
		t.Error("expected at least one exported UserService symbol")
	}
}

func TestJavaScriptParser_Parse_ExportConst(t *testing.T) {
	parser := NewJavaScriptParser()
	content := `export const DEFAULT_TIMEOUT = 5000;`

	result, err := parser.Parse(context.Background(), []byte(content), "config.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var constant *Symbol
	for _, sym := range result.Symbols {
		if sym.Name == "DEFAULT_TIMEOUT" {
			constant = sym
			break
		}
	}

	if constant == nil {
		t.Fatal("expected to find constant 'DEFAULT_TIMEOUT'")
	}
	if !constant.Exported {
		t.Error("expected constant to be exported")
	}
	if constant.Kind != SymbolKindConstant {
		t.Errorf("expected kind Constant, got %v", constant.Kind)
	}
}

func TestJavaScriptParser_Parse_JSDoc(t *testing.T) {
	parser := NewJavaScriptParser()
	content := `
/**
 * Greet a user by name.
 * @param {string} name - The user's name
 * @returns {string} The greeting
 */
export function greet(name) {
    return "Hello, " + name;
}
`
	result, err := parser.Parse(context.Background(), []byte(content), "greet.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var fn *Symbol
	for _, sym := range result.Symbols {
		if sym.Name == "greet" && sym.Kind == SymbolKindFunction {
			fn = sym
			break
		}
	}

	if fn == nil {
		t.Fatal("expected to find function 'greet'")
	}
	if fn.DocComment == "" {
		t.Error("expected DocComment to be populated")
	}
	if !strings.Contains(fn.DocComment, "@param") {
		t.Errorf("expected DocComment to contain @param, got %q", fn.DocComment)
	}
}

func TestJavaScriptParser_Parse_FileTooLarge(t *testing.T) {
	parser := NewJavaScriptParser(WithJSMaxFileSize(100))
	content := make([]byte, 200)
	for i := range content {
		content[i] = ' '
	}

	_, err := parser.Parse(context.Background(), content, "large.js")
	if err != ErrFileTooLarge {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestJavaScriptParser_Parse_InvalidUTF8(t *testing.T) {
	parser := NewJavaScriptParser()
	// Invalid UTF-8 byte sequence
	content := []byte{0xff, 0xfe, 0x00, 0x01}

	_, err := parser.Parse(context.Background(), content, "invalid.js")
	if err != ErrInvalidContent {
		t.Errorf("expected ErrInvalidContent, got %v", err)
	}
}

func TestJavaScriptParser_Parse_ContextCancellation(t *testing.T) {
	parser := NewJavaScriptParser()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := parser.Parse(ctx, []byte("function test() {}"), "test.js")
	if err == nil {
		t.Error("expected error due to cancelled context")
	}
}

func TestJavaScriptParser_Parse_Hash(t *testing.T) {
	parser := NewJavaScriptParser()
	content := []byte("const x = 1;")

	result1, _ := parser.Parse(context.Background(), content, "test.js")
	result2, _ := parser.Parse(context.Background(), content, "test.js")

	if result1.Hash == "" {
		t.Error("expected hash to be set")
	}
	if result1.Hash != result2.Hash {
		t.Error("expected same content to produce same hash")
	}

	// Different content should produce different hash
	result3, _ := parser.Parse(context.Background(), []byte("const y = 2;"), "test.js")
	if result1.Hash == result3.Hash {
		t.Error("expected different content to produce different hash")
	}
}

func TestJavaScriptParser_Parse_Concurrent(t *testing.T) {
	parser := NewJavaScriptParser()
	contents := []string{
		"function a() {}",
		"function b() {}",
		"function c() {}",
		"class X {}",
		"class Y {}",
	}

	var wg sync.WaitGroup
	errors := make(chan error, len(contents))

	for i, content := range contents {
		wg.Add(1)
		go func(idx int, c string) {
			defer wg.Done()
			_, err := parser.Parse(context.Background(), []byte(c), "test.js")
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

func TestJavaScriptParser_Parse_Timeout(t *testing.T) {
	parser := NewJavaScriptParser()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Small content that should parse quickly
	content := []byte("const x = 1;")

	// This might or might not timeout depending on timing
	// We're mainly testing that the timeout mechanism doesn't panic
	_, _ = parser.Parse(ctx, content, "test.js")
}

func TestJavaScriptParser_Language(t *testing.T) {
	parser := NewJavaScriptParser()
	if parser.Language() != "javascript" {
		t.Errorf("expected 'javascript', got %q", parser.Language())
	}
}

func TestJavaScriptParser_Extensions(t *testing.T) {
	parser := NewJavaScriptParser()
	extensions := parser.Extensions()

	expected := map[string]bool{".js": true, ".mjs": true, ".cjs": true, ".jsx": true}
	for _, ext := range extensions {
		if !expected[ext] {
			t.Errorf("unexpected extension: %q", ext)
		}
		delete(expected, ext)
	}
	for ext := range expected {
		t.Errorf("missing extension: %q", ext)
	}
}

func TestJavaScriptParser_Parse_ComprehensiveExample(t *testing.T) {
	parser := NewJavaScriptParser()
	content := `
/**
 * User service for managing users.
 * @module UserService
 */
import { EventEmitter } from 'events';
import config from './config.js';
const legacy = require('./legacy');

/**
 * User service class.
 * @class
 */
export class UserService extends EventEmitter {
    #privateCache = new Map();
    publicCount = 0;

    constructor(db) {
        super();
        this.db = db;
    }

    /**
     * Get user by ID.
     * @param {number} id - User ID
     * @returns {Promise<User>}
     */
    async getUser(id) {
        return this.db.findById(id);
    }

    static createInstance(db) {
        return new UserService(db);
    }

    *generateIds() {
        let id = 0;
        while (true) yield id++;
    }
}

export const DEFAULT_TIMEOUT = 5000;
const internalHelper = () => {};
export default UserService;
`
	result, err := parser.Parse(context.Background(), []byte(content), "user-service.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check imports
	if len(result.Imports) < 3 {
		t.Errorf("expected at least 3 imports, got %d", len(result.Imports))
	}

	// Check for UserService class
	var userService *Symbol
	for _, sym := range result.Symbols {
		if sym.Name == "UserService" && sym.Kind == SymbolKindClass {
			userService = sym
			break
		}
	}

	if userService == nil {
		t.Fatal("expected to find class 'UserService'")
	}
	if !userService.Exported {
		t.Error("expected UserService to be exported")
	}
	if userService.Metadata == nil || userService.Metadata.Extends != "EventEmitter" {
		t.Error("expected UserService to extend EventEmitter")
	}

	// Check class has expected children
	if len(userService.Children) < 5 {
		t.Errorf("expected at least 5 children, got %d", len(userService.Children))
	}

	// Find specific members
	memberNames := make(map[string]bool)
	for _, child := range userService.Children {
		memberNames[child.Name] = true
	}

	expectedMembers := []string{"#privateCache", "publicCount", "constructor", "getUser", "createInstance", "generateIds"}
	for _, name := range expectedMembers {
		if !memberNames[name] {
			t.Errorf("expected to find member %q", name)
		}
	}

	// Check async method
	for _, child := range userService.Children {
		if child.Name == "getUser" {
			if child.Metadata == nil || !child.Metadata.IsAsync {
				t.Error("expected getUser to be async")
			}
			if child.DocComment == "" {
				t.Error("expected getUser to have JSDoc comment")
			}
		}
		if child.Name == "generateIds" {
			if child.Metadata == nil || !child.Metadata.IsGenerator {
				t.Error("expected generateIds to be a generator")
			}
		}
		if child.Name == "createInstance" {
			if child.Metadata == nil || !child.Metadata.IsStatic {
				t.Error("expected createInstance to be static")
			}
		}
	}

	// Check DEFAULT_TIMEOUT
	var timeout *Symbol
	for _, sym := range result.Symbols {
		if sym.Name == "DEFAULT_TIMEOUT" {
			timeout = sym
			break
		}
	}
	if timeout == nil {
		t.Error("expected to find constant 'DEFAULT_TIMEOUT'")
	} else if !timeout.Exported {
		t.Error("expected DEFAULT_TIMEOUT to be exported")
	}

	// Check internalHelper is not exported
	for _, sym := range result.Symbols {
		if sym.Name == "internalHelper" {
			if sym.Exported {
				t.Error("expected internalHelper to not be exported")
			}
		}
	}
}
