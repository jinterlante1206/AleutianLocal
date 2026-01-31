// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package lsp

// =============================================================================
// POSITION & RANGE TYPES
// =============================================================================

// Position represents a position in a text document.
// Line and character are 0-indexed per LSP specification.
type Position struct {
	// Line is the 0-indexed line number.
	Line int `json:"line"`

	// Character is the 0-indexed character offset within the line.
	Character int `json:"character"`
}

// Range represents a range in a text document.
type Range struct {
	// Start is the inclusive start position.
	Start Position `json:"start"`

	// End is the exclusive end position.
	End Position `json:"end"`
}

// Location represents a location in a document.
type Location struct {
	// URI is the document URI (file:// scheme).
	URI string `json:"uri"`

	// Range is the range within the document.
	Range Range `json:"range"`
}

// LocationLink represents a link between a source and target location.
type LocationLink struct {
	// OriginSelectionRange is the span in the source that was used.
	OriginSelectionRange *Range `json:"originSelectionRange,omitempty"`

	// TargetURI is the target document URI.
	TargetURI string `json:"targetUri"`

	// TargetRange is the full range of the target (for highlighting).
	TargetRange Range `json:"targetRange"`

	// TargetSelectionRange is the precise range to reveal.
	TargetSelectionRange Range `json:"targetSelectionRange"`
}

// =============================================================================
// DOCUMENT IDENTIFIERS
// =============================================================================

// TextDocumentIdentifier identifies a text document by URI.
type TextDocumentIdentifier struct {
	// URI is the document's URI.
	URI string `json:"uri"`
}

// TextDocumentItem represents a text document with its content.
type TextDocumentItem struct {
	// URI is the document's URI.
	URI string `json:"uri"`

	// LanguageID is the language identifier (e.g., "go", "python").
	LanguageID string `json:"languageId"`

	// Version is the version number of this document.
	Version int `json:"version"`

	// Text is the content of the document.
	Text string `json:"text"`
}

// VersionedTextDocumentIdentifier identifies a specific version of a document.
type VersionedTextDocumentIdentifier struct {
	TextDocumentIdentifier

	// Version is the version number. Null means the version is known.
	Version *int `json:"version"`
}

// =============================================================================
// REQUEST PARAMETER TYPES
// =============================================================================

// TextDocumentPositionParams identifies a position in a text document.
type TextDocumentPositionParams struct {
	// TextDocument is the document identifier.
	TextDocument TextDocumentIdentifier `json:"textDocument"`

	// Position is the position within the document.
	Position Position `json:"position"`
}

// ReferenceParams extends TextDocumentPositionParams for find references.
type ReferenceParams struct {
	TextDocumentPositionParams

	// Context contains additional context for the request.
	Context ReferenceContext `json:"context"`
}

// ReferenceContext contains options for find references requests.
type ReferenceContext struct {
	// IncludeDeclaration indicates whether to include the declaration.
	IncludeDeclaration bool `json:"includeDeclaration"`
}

// RenameParams contains rename request parameters.
type RenameParams struct {
	TextDocumentPositionParams

	// NewName is the new name to rename the symbol to.
	NewName string `json:"newName"`
}

// PrepareRenameParams contains prepare rename request parameters.
type PrepareRenameParams struct {
	TextDocumentPositionParams
}

// WorkspaceSymbolParams contains workspace symbol query parameters.
type WorkspaceSymbolParams struct {
	// Query is a non-empty query string to filter symbols.
	Query string `json:"query"`
}

// DidOpenTextDocumentParams contains params for textDocument/didOpen.
type DidOpenTextDocumentParams struct {
	// TextDocument is the document that was opened.
	TextDocument TextDocumentItem `json:"textDocument"`
}

// DidCloseTextDocumentParams contains params for textDocument/didClose.
type DidCloseTextDocumentParams struct {
	// TextDocument is the document that was closed.
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// DidChangeTextDocumentParams contains params for textDocument/didChange.
type DidChangeTextDocumentParams struct {
	// TextDocument is the document that changed.
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`

	// ContentChanges is the list of changes.
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// TextDocumentContentChangeEvent describes a content change event.
type TextDocumentContentChangeEvent struct {
	// Range is the range that got replaced. Omit for full document sync.
	Range *Range `json:"range,omitempty"`

	// RangeLength is the length of the range that got replaced (deprecated).
	RangeLength *int `json:"rangeLength,omitempty"`

	// Text is the new text for the range or full document.
	Text string `json:"text"`
}

// =============================================================================
// RESPONSE TYPES
// =============================================================================

// HoverResult contains hover information.
type HoverResult struct {
	// Contents is the hover content.
	Contents MarkupContent `json:"contents"`

	// Range is the range this hover applies to.
	Range *Range `json:"range,omitempty"`
}

// MarkupContent represents documentation content.
type MarkupContent struct {
	// Kind is the type of markup: "plaintext" or "markdown".
	Kind string `json:"kind"`

	// Value is the actual content.
	Value string `json:"value"`
}

// WorkspaceEdit represents changes to many resources.
type WorkspaceEdit struct {
	// Changes is a map from URI to list of text edits.
	Changes map[string][]TextEdit `json:"changes,omitempty"`

	// DocumentChanges are versioned document edits (preferred over Changes).
	DocumentChanges []TextDocumentEdit `json:"documentChanges,omitempty"`
}

// TextEdit represents a single text change.
type TextEdit struct {
	// Range is the range to replace.
	Range Range `json:"range"`

	// NewText is the replacement text.
	NewText string `json:"newText"`
}

// TextDocumentEdit describes edits to a specific document version.
type TextDocumentEdit struct {
	// TextDocument identifies the document.
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`

	// Edits is the list of edits.
	Edits []TextEdit `json:"edits"`
}

// SymbolInformation represents information about a symbol.
type SymbolInformation struct {
	// Name is the symbol's name.
	Name string `json:"name"`

	// Kind is the symbol kind (function, class, etc.).
	Kind SymbolKind `json:"kind"`

	// Tags are additional attributes (deprecated, etc.).
	Tags []SymbolTag `json:"tags,omitempty"`

	// Location is where the symbol is defined.
	Location Location `json:"location"`

	// ContainerName is the name of the containing symbol.
	ContainerName string `json:"containerName,omitempty"`
}

// SymbolKind represents the kind of a symbol.
type SymbolKind int

// Symbol kinds as defined by the LSP specification.
const (
	SymbolKindFile          SymbolKind = 1
	SymbolKindModule        SymbolKind = 2
	SymbolKindNamespace     SymbolKind = 3
	SymbolKindPackage       SymbolKind = 4
	SymbolKindClass         SymbolKind = 5
	SymbolKindMethod        SymbolKind = 6
	SymbolKindProperty      SymbolKind = 7
	SymbolKindField         SymbolKind = 8
	SymbolKindConstructor   SymbolKind = 9
	SymbolKindEnum          SymbolKind = 10
	SymbolKindInterface     SymbolKind = 11
	SymbolKindFunction      SymbolKind = 12
	SymbolKindVariable      SymbolKind = 13
	SymbolKindConstant      SymbolKind = 14
	SymbolKindString        SymbolKind = 15
	SymbolKindNumber        SymbolKind = 16
	SymbolKindBoolean       SymbolKind = 17
	SymbolKindArray         SymbolKind = 18
	SymbolKindObject        SymbolKind = 19
	SymbolKindKey           SymbolKind = 20
	SymbolKindNull          SymbolKind = 21
	SymbolKindEnumMember    SymbolKind = 22
	SymbolKindStruct        SymbolKind = 23
	SymbolKindEvent         SymbolKind = 24
	SymbolKindOperator      SymbolKind = 25
	SymbolKindTypeParameter SymbolKind = 26
)

// SymbolTag represents additional symbol attributes.
type SymbolTag int

// Symbol tags as defined by the LSP specification.
const (
	SymbolTagDeprecated SymbolTag = 1
)

// PrepareRenameResult contains the result of a prepare rename request.
type PrepareRenameResult struct {
	// Range is the range of the string to rename.
	Range Range `json:"range"`

	// Placeholder is the text of the string to rename.
	Placeholder string `json:"placeholder"`
}

// =============================================================================
// INITIALIZE TYPES
// =============================================================================

// InitializeParams contains initialization parameters.
type InitializeParams struct {
	// ProcessID is the process ID of the parent process.
	ProcessID int `json:"processId"`

	// RootURI is the root URI of the workspace (preferred over rootPath).
	RootURI string `json:"rootUri"`

	// RootPath is the root path of the workspace (deprecated).
	RootPath string `json:"rootPath,omitempty"`

	// Capabilities describes what the client supports.
	Capabilities ClientCapabilities `json:"capabilities"`

	// InitializationOptions are custom initialization options.
	InitializationOptions interface{} `json:"initializationOptions,omitempty"`

	// Trace sets the initial trace setting.
	Trace string `json:"trace,omitempty"`

	// WorkspaceFolders are the workspace folders if supported.
	WorkspaceFolders []WorkspaceFolder `json:"workspaceFolders,omitempty"`
}

// WorkspaceFolder represents a workspace folder.
type WorkspaceFolder struct {
	// URI is the folder URI.
	URI string `json:"uri"`

	// Name is the name of the folder.
	Name string `json:"name"`
}

// ClientCapabilities describes what the client supports.
type ClientCapabilities struct {
	// TextDocument describes text document capabilities.
	TextDocument TextDocumentClientCapabilities `json:"textDocument,omitempty"`

	// Workspace describes workspace capabilities.
	Workspace WorkspaceClientCapabilities `json:"workspace,omitempty"`
}

// TextDocumentClientCapabilities describes text document capabilities.
type TextDocumentClientCapabilities struct {
	// Synchronization describes document sync capabilities.
	Synchronization *TextDocumentSyncClientCapabilities `json:"synchronization,omitempty"`

	// Definition describes go-to-definition support.
	Definition *DefinitionCapabilities `json:"definition,omitempty"`

	// References describes find-references support.
	References *ReferencesCapabilities `json:"references,omitempty"`

	// Hover describes hover support.
	Hover *HoverCapabilities `json:"hover,omitempty"`

	// Rename describes rename support.
	Rename *RenameCapabilities `json:"rename,omitempty"`
}

// TextDocumentSyncClientCapabilities describes sync capabilities.
type TextDocumentSyncClientCapabilities struct {
	// DynamicRegistration indicates dynamic registration is supported.
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`

	// WillSave indicates willSave notifications are supported.
	WillSave bool `json:"willSave,omitempty"`

	// WillSaveWaitUntil indicates willSaveWaitUntil requests are supported.
	WillSaveWaitUntil bool `json:"willSaveWaitUntil,omitempty"`

	// DidSave indicates didSave notifications are supported.
	DidSave bool `json:"didSave,omitempty"`
}

// WorkspaceClientCapabilities describes workspace capabilities.
type WorkspaceClientCapabilities struct {
	// ApplyEdit indicates applyEdit requests are supported.
	ApplyEdit bool `json:"applyEdit,omitempty"`

	// WorkspaceEdit describes workspace edit capabilities.
	WorkspaceEdit *WorkspaceEditClientCapabilities `json:"workspaceEdit,omitempty"`

	// Symbol describes workspace symbol capabilities.
	Symbol *WorkspaceSymbolClientCapabilities `json:"symbol,omitempty"`
}

// WorkspaceEditClientCapabilities describes workspace edit capabilities.
type WorkspaceEditClientCapabilities struct {
	// DocumentChanges indicates documentChanges are supported.
	DocumentChanges bool `json:"documentChanges,omitempty"`
}

// WorkspaceSymbolClientCapabilities describes workspace symbol capabilities.
type WorkspaceSymbolClientCapabilities struct {
	// DynamicRegistration indicates dynamic registration is supported.
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

// DefinitionCapabilities describes go-to-definition support.
type DefinitionCapabilities struct {
	// DynamicRegistration indicates dynamic registration is supported.
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`

	// LinkSupport indicates LocationLink support.
	LinkSupport bool `json:"linkSupport,omitempty"`
}

// ReferencesCapabilities describes find-references support.
type ReferencesCapabilities struct {
	// DynamicRegistration indicates dynamic registration is supported.
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

// HoverCapabilities describes hover support.
type HoverCapabilities struct {
	// DynamicRegistration indicates dynamic registration is supported.
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`

	// ContentFormat describes supported content formats.
	ContentFormat []string `json:"contentFormat,omitempty"`
}

// RenameCapabilities describes rename support.
type RenameCapabilities struct {
	// DynamicRegistration indicates dynamic registration is supported.
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`

	// PrepareSupport indicates prepareRename is supported.
	PrepareSupport bool `json:"prepareSupport,omitempty"`
}

// InitializeResult contains the server's response to initialize.
type InitializeResult struct {
	// Capabilities describes what the server supports.
	Capabilities ServerCapabilities `json:"capabilities"`

	// ServerInfo contains optional server information.
	ServerInfo *ServerInfo `json:"serverInfo,omitempty"`
}

// ServerInfo contains information about the server.
type ServerInfo struct {
	// Name is the server's name.
	Name string `json:"name"`

	// Version is the server's version.
	Version string `json:"version,omitempty"`
}

// ServerCapabilities describes what the server supports.
type ServerCapabilities struct {
	// TextDocumentSync describes how documents are synced.
	TextDocumentSync interface{} `json:"textDocumentSync,omitempty"`

	// DefinitionProvider indicates textDocument/definition is supported.
	DefinitionProvider interface{} `json:"definitionProvider,omitempty"`

	// ReferencesProvider indicates textDocument/references is supported.
	ReferencesProvider interface{} `json:"referencesProvider,omitempty"`

	// HoverProvider indicates textDocument/hover is supported.
	HoverProvider interface{} `json:"hoverProvider,omitempty"`

	// RenameProvider indicates textDocument/rename is supported.
	RenameProvider interface{} `json:"renameProvider,omitempty"`

	// WorkspaceSymbolProvider indicates workspace/symbol is supported.
	WorkspaceSymbolProvider interface{} `json:"workspaceSymbolProvider,omitempty"`
}

// HasDefinitionProvider returns true if definition is supported.
func (c *ServerCapabilities) HasDefinitionProvider() bool {
	return c.DefinitionProvider != nil && c.DefinitionProvider != false
}

// HasReferencesProvider returns true if references is supported.
func (c *ServerCapabilities) HasReferencesProvider() bool {
	return c.ReferencesProvider != nil && c.ReferencesProvider != false
}

// HasHoverProvider returns true if hover is supported.
func (c *ServerCapabilities) HasHoverProvider() bool {
	return c.HoverProvider != nil && c.HoverProvider != false
}

// HasRenameProvider returns true if rename is supported.
func (c *ServerCapabilities) HasRenameProvider() bool {
	return c.RenameProvider != nil && c.RenameProvider != false
}

// HasWorkspaceSymbolProvider returns true if workspace/symbol is supported.
func (c *ServerCapabilities) HasWorkspaceSymbolProvider() bool {
	return c.WorkspaceSymbolProvider != nil && c.WorkspaceSymbolProvider != false
}
