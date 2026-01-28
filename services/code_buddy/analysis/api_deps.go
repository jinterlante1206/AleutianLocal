// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package analysis

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/graph"
	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/index"
)

// APIDepTracker tracks external API dependencies in code.
//
// # Description
//
// Detects HTTP client calls, OpenAPI/Swagger references, GraphQL operations,
// and gRPC service calls. Maps these to symbols to understand the external
// API blast radius of changes.
//
// # Thread Safety
//
// Safe for concurrent use after construction.
type APIDepTracker struct {
	projectRoot string
	codeGraph   *graph.Graph
	symbolIndex *index.SymbolIndex

	mu             sync.RWMutex
	dependencies   []APIDependency
	endpointUsages map[string][]APIDependency // endpoint -> usages
	symbolDeps     map[string][]APIDependency // symbol -> deps
	contracts      []APIContract              // parsed contracts
}

// APIDependency represents an external API dependency from code.
type APIDependency struct {
	// Endpoint is the API endpoint (URL path or service method).
	Endpoint string `json:"endpoint"`

	// Method is the HTTP method (GET, POST, etc.) or RPC method.
	Method string `json:"method"`

	// Type is the API type (REST, GraphQL, gRPC).
	Type APIType `json:"type"`

	// SourceSymbol is the symbol containing this dependency.
	SourceSymbol string `json:"source_symbol"`

	// FilePath is the file containing the dependency.
	FilePath string `json:"file_path"`

	// Line is the line number of the dependency.
	Line int `json:"line"`

	// ServiceName is the external service name (if detectable).
	ServiceName string `json:"service_name,omitempty"`

	// Contract is the API contract reference (if any).
	Contract string `json:"contract,omitempty"`

	// Confidence is how confident we are in this detection (0-100).
	Confidence int `json:"confidence"`
}

// APIType represents the type of API.
type APIType string

const (
	APITypeREST    APIType = "REST"
	APITypeGraphQL APIType = "GRAPHQL"
	APITypeGRPC    APIType = "GRPC"
	APITypeWebhook APIType = "WEBHOOK"
)

// APIContract represents a parsed API contract file.
type APIContract struct {
	// Path is the file path of the contract.
	Path string `json:"path"`

	// Type is the contract type (openapi, graphql, protobuf).
	Type string `json:"type"`

	// Version is the API version (if specified).
	Version string `json:"version,omitempty"`

	// Endpoints are the defined endpoints.
	Endpoints []ContractEndpoint `json:"endpoints"`
}

// ContractEndpoint represents an endpoint defined in a contract.
type ContractEndpoint struct {
	Path        string   `json:"path"`
	Method      string   `json:"method,omitempty"`
	OperationID string   `json:"operation_id,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Parameters  []string `json:"parameters,omitempty"`
}

// HTTP client patterns
var (
	// Standard library http patterns
	httpNewRequestPattern = regexp.MustCompile(`http\.NewRequest(?:WithContext)?\s*\(\s*["'](\w+)["']\s*,\s*["']([^"']+)["']`)
	httpGetPattern        = regexp.MustCompile(`http\.Get\s*\(\s*["']([^"']+)["']`)
	httpPostPattern       = regexp.MustCompile(`http\.Post\s*\(\s*["']([^"']+)["']`)

	// Popular HTTP client libraries
	restyPattern = regexp.MustCompile(`\.(?:R|SetResult|SetBody)\(\)\.(?:Get|Post|Put|Delete|Patch)\s*\(\s*["']([^"']+)["']`)
	goreqPattern = regexp.MustCompile(`gorequest\.New\(\)\.(?:Get|Post|Put|Delete|Patch)\s*\(\s*["']([^"']+)["']`)
	reqPattern   = regexp.MustCompile(`req\.(?:Get|Post|Put|Delete|Patch)\s*\(\s*["']([^"']+)["']`)

	// URL patterns
	urlParsePattern = regexp.MustCompile(`url\.Parse\s*\(\s*["']([^"']+)["']`)

	// gRPC patterns
	grpcDialPattern    = regexp.MustCompile(`grpc\.Dial(?:Context)?\s*\(\s*(?:ctx\s*,\s*)?["']([^"']+)["']`)
	grpcClientPattern  = regexp.MustCompile(`New(\w+)Client\s*\(`)
	protoImportPattern = regexp.MustCompile(`"([^"]+\.pb)"`)

	// GraphQL patterns
	graphqlQueryPattern  = regexp.MustCompile(`(?:query|mutation|subscription)\s+(\w+)`)
	graphqlClientPattern = regexp.MustCompile(`graphql\.NewClient\s*\(\s*["']([^"']+)["']`)
)

// NewAPIDepTracker creates a new API dependency tracker.
//
// # Inputs
//
//   - projectRoot: Root directory of the project.
//   - g: The dependency graph.
//   - idx: The symbol index.
//
// # Outputs
//
//   - *APIDepTracker: Ready-to-use tracker.
func NewAPIDepTracker(projectRoot string, g *graph.Graph, idx *index.SymbolIndex) *APIDepTracker {
	return &APIDepTracker{
		projectRoot:    projectRoot,
		codeGraph:      g,
		symbolIndex:    idx,
		endpointUsages: make(map[string][]APIDependency),
		symbolDeps:     make(map[string][]APIDependency),
	}
}

// Scan analyzes the codebase for API dependencies.
//
// # Inputs
//
//   - ctx: Context for cancellation.
//
// # Outputs
//
//   - error: Non-nil on failure.
func (t *APIDepTracker) Scan(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Clear existing data
	t.dependencies = nil
	t.endpointUsages = make(map[string][]APIDependency)
	t.symbolDeps = make(map[string][]APIDependency)
	t.contracts = nil

	// First, scan for API contracts
	if err := t.scanContracts(ctx); err != nil {
		return err
	}

	// Then scan source files
	err := filepath.WalkDir(t.projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(path)
		if ext != ".go" {
			return nil
		}

		deps, err := t.scanFile(ctx, path)
		if err != nil {
			return nil
		}

		t.dependencies = append(t.dependencies, deps...)
		for _, dep := range deps {
			t.endpointUsages[dep.Endpoint] = append(t.endpointUsages[dep.Endpoint], dep)
			t.symbolDeps[dep.SourceSymbol] = append(t.symbolDeps[dep.SourceSymbol], dep)
		}

		return nil
	})

	return err
}

// scanContracts finds and parses API contract files.
func (t *APIDepTracker) scanContracts(ctx context.Context) error {
	// Look for OpenAPI/Swagger files
	openAPIFiles := []string{
		"openapi.yaml", "openapi.yml", "openapi.json",
		"swagger.yaml", "swagger.yml", "swagger.json",
		"api/openapi.yaml", "api/openapi.yml",
		"docs/openapi.yaml", "docs/swagger.yaml",
	}

	for _, filename := range openAPIFiles {
		path := filepath.Join(t.projectRoot, filename)
		if _, err := os.Stat(path); err == nil {
			contract, err := t.parseOpenAPI(path)
			if err == nil {
				t.contracts = append(t.contracts, contract)
			}
		}
	}

	// Look for GraphQL schema files
	graphQLFiles := []string{
		"schema.graphql", "schema.gql",
		"api/schema.graphql", "graphql/schema.graphql",
	}

	for _, filename := range graphQLFiles {
		path := filepath.Join(t.projectRoot, filename)
		if _, err := os.Stat(path); err == nil {
			contract, err := t.parseGraphQLSchema(path)
			if err == nil {
				t.contracts = append(t.contracts, contract)
			}
		}
	}

	// Look for proto files
	err := filepath.WalkDir(t.projectRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		if filepath.Ext(path) == ".proto" {
			contract, err := t.parseProto(path)
			if err == nil {
				t.contracts = append(t.contracts, contract)
			}
		}
		return nil
	})

	return err
}

// parseOpenAPI parses an OpenAPI/Swagger file.
func (t *APIDepTracker) parseOpenAPI(path string) (APIContract, error) {
	contract := APIContract{
		Path: path,
		Type: "openapi",
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return contract, err
	}

	// Simple JSON parsing (YAML would need a library)
	if strings.HasSuffix(path, ".json") {
		var spec struct {
			OpenAPI string `json:"openapi"`
			Info    struct {
				Version string `json:"version"`
			} `json:"info"`
			Paths map[string]map[string]struct {
				OperationID string `json:"operationId"`
				Summary     string `json:"summary"`
			} `json:"paths"`
		}

		if err := json.Unmarshal(data, &spec); err == nil {
			contract.Version = spec.Info.Version
			for pathStr, methods := range spec.Paths {
				for method, op := range methods {
					contract.Endpoints = append(contract.Endpoints, ContractEndpoint{
						Path:        pathStr,
						Method:      strings.ToUpper(method),
						OperationID: op.OperationID,
						Summary:     op.Summary,
					})
				}
			}
		}
	}

	return contract, nil
}

// parseGraphQLSchema parses a GraphQL schema file.
func (t *APIDepTracker) parseGraphQLSchema(path string) (APIContract, error) {
	contract := APIContract{
		Path: path,
		Type: "graphql",
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return contract, err
	}

	// Simple pattern matching for queries and mutations
	content := string(data)

	// Find Query type
	queryPattern := regexp.MustCompile(`type\s+Query\s*\{([^}]+)\}`)
	if matches := queryPattern.FindStringSubmatch(content); len(matches) > 1 {
		fieldPattern := regexp.MustCompile(`(\w+)\s*(?:\([^)]*\))?\s*:`)
		fields := fieldPattern.FindAllStringSubmatch(matches[1], -1)
		for _, field := range fields {
			if len(field) > 1 {
				contract.Endpoints = append(contract.Endpoints, ContractEndpoint{
					Path:   field[1],
					Method: "QUERY",
				})
			}
		}
	}

	// Find Mutation type
	mutationPattern := regexp.MustCompile(`type\s+Mutation\s*\{([^}]+)\}`)
	if matches := mutationPattern.FindStringSubmatch(content); len(matches) > 1 {
		fieldPattern := regexp.MustCompile(`(\w+)\s*(?:\([^)]*\))?\s*:`)
		fields := fieldPattern.FindAllStringSubmatch(matches[1], -1)
		for _, field := range fields {
			if len(field) > 1 {
				contract.Endpoints = append(contract.Endpoints, ContractEndpoint{
					Path:   field[1],
					Method: "MUTATION",
				})
			}
		}
	}

	return contract, nil
}

// parseProto parses a Protocol Buffer file.
func (t *APIDepTracker) parseProto(path string) (APIContract, error) {
	contract := APIContract{
		Path: path,
		Type: "protobuf",
	}

	f, err := os.Open(path)
	if err != nil {
		return contract, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var currentService string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Service declaration
		if strings.HasPrefix(line, "service ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				currentService = parts[1]
			}
		}

		// RPC method
		if strings.HasPrefix(line, "rpc ") && currentService != "" {
			rpcPattern := regexp.MustCompile(`rpc\s+(\w+)\s*\(`)
			if matches := rpcPattern.FindStringSubmatch(line); len(matches) > 1 {
				contract.Endpoints = append(contract.Endpoints, ContractEndpoint{
					Path:   currentService + "/" + matches[1],
					Method: "RPC",
				})
			}
		}
	}

	return contract, scanner.Err()
}

// scanFile scans a single file for API dependencies.
func (t *APIDepTracker) scanFile(ctx context.Context, path string) ([]APIDependency, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var deps []APIDependency
	relPath, _ := filepath.Rel(t.projectRoot, path)

	scanner := bufio.NewScanner(f)
	lineNum := 0
	var currentSymbol string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		lineNum++
		line := scanner.Text()

		// Track current function
		if strings.Contains(line, "func ") {
			currentSymbol = extractFunctionSymbolFromLine(line, relPath)
		}

		// Check for HTTP patterns
		deps = append(deps, t.detectHTTPPatterns(line, relPath, lineNum, currentSymbol)...)

		// Check for gRPC patterns
		deps = append(deps, t.detectGRPCPatterns(line, relPath, lineNum, currentSymbol)...)

		// Check for GraphQL patterns
		deps = append(deps, t.detectGraphQLPatterns(line, relPath, lineNum, currentSymbol)...)
	}

	return deps, scanner.Err()
}

// extractFunctionSymbolFromLine extracts function name from a line.
func extractFunctionSymbolFromLine(line, relPath string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "func ") {
		return ""
	}

	rest := strings.TrimPrefix(line, "func ")

	if strings.HasPrefix(rest, "(") {
		closeIdx := strings.Index(rest, ")")
		if closeIdx < 0 {
			return ""
		}
		rest = strings.TrimSpace(rest[closeIdx+1:])
	}

	parenIdx := strings.Index(rest, "(")
	if parenIdx < 0 {
		return ""
	}
	funcName := strings.TrimSpace(rest[:parenIdx])

	return relPath + ":" + funcName
}

// detectHTTPPatterns finds HTTP client calls in a line.
func (t *APIDepTracker) detectHTTPPatterns(line, relPath string, lineNum int, symbol string) []APIDependency {
	var deps []APIDependency

	// http.NewRequest
	if matches := httpNewRequestPattern.FindStringSubmatch(line); len(matches) >= 3 {
		deps = append(deps, APIDependency{
			Endpoint:     matches[2],
			Method:       matches[1],
			Type:         APITypeREST,
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			Confidence:   90,
		})
	}

	// http.Get
	if matches := httpGetPattern.FindStringSubmatch(line); len(matches) >= 2 {
		deps = append(deps, APIDependency{
			Endpoint:     matches[1],
			Method:       "GET",
			Type:         APITypeREST,
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			Confidence:   90,
		})
	}

	// http.Post
	if matches := httpPostPattern.FindStringSubmatch(line); len(matches) >= 2 {
		deps = append(deps, APIDependency{
			Endpoint:     matches[1],
			Method:       "POST",
			Type:         APITypeREST,
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			Confidence:   90,
		})
	}

	// Resty, gorequest, etc.
	for _, pattern := range []*regexp.Regexp{restyPattern, goreqPattern, reqPattern} {
		if matches := pattern.FindStringSubmatch(line); len(matches) >= 2 {
			method := "GET"
			if strings.Contains(line, ".Post(") {
				method = "POST"
			} else if strings.Contains(line, ".Put(") {
				method = "PUT"
			} else if strings.Contains(line, ".Delete(") {
				method = "DELETE"
			} else if strings.Contains(line, ".Patch(") {
				method = "PATCH"
			}

			deps = append(deps, APIDependency{
				Endpoint:     matches[1],
				Method:       method,
				Type:         APITypeREST,
				SourceSymbol: symbol,
				FilePath:     relPath,
				Line:         lineNum,
				Confidence:   85,
			})
		}
	}

	return deps
}

// detectGRPCPatterns finds gRPC client calls in a line.
func (t *APIDepTracker) detectGRPCPatterns(line, relPath string, lineNum int, symbol string) []APIDependency {
	var deps []APIDependency

	// grpc.Dial
	if matches := grpcDialPattern.FindStringSubmatch(line); len(matches) >= 2 {
		deps = append(deps, APIDependency{
			Endpoint:     matches[1],
			Method:       "DIAL",
			Type:         APITypeGRPC,
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			Confidence:   95,
		})
	}

	// NewXxxClient
	if matches := grpcClientPattern.FindStringSubmatch(line); len(matches) >= 2 {
		deps = append(deps, APIDependency{
			Endpoint:     matches[1],
			Method:       "CLIENT",
			Type:         APITypeGRPC,
			ServiceName:  matches[1],
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			Confidence:   85,
		})
	}

	return deps
}

// detectGraphQLPatterns finds GraphQL operations in a line.
func (t *APIDepTracker) detectGraphQLPatterns(line, relPath string, lineNum int, symbol string) []APIDependency {
	var deps []APIDependency

	// GraphQL client
	if matches := graphqlClientPattern.FindStringSubmatch(line); len(matches) >= 2 {
		deps = append(deps, APIDependency{
			Endpoint:     matches[1],
			Method:       "CLIENT",
			Type:         APITypeGraphQL,
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			Confidence:   90,
		})
	}

	// Query/mutation in string literal
	if matches := graphqlQueryPattern.FindStringSubmatch(line); len(matches) >= 2 {
		method := "QUERY"
		if strings.Contains(line, "mutation") {
			method = "MUTATION"
		} else if strings.Contains(line, "subscription") {
			method = "SUBSCRIPTION"
		}

		deps = append(deps, APIDependency{
			Endpoint:     matches[1],
			Method:       method,
			Type:         APITypeGraphQL,
			SourceSymbol: symbol,
			FilePath:     relPath,
			Line:         lineNum,
			Confidence:   80,
		})
	}

	return deps
}

// FindEndpointUsages returns all code locations that call an endpoint.
func (t *APIDepTracker) FindEndpointUsages(endpoint string) ([]APIDependency, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	deps := t.endpointUsages[endpoint]
	if deps == nil {
		return []APIDependency{}, nil
	}

	result := make([]APIDependency, len(deps))
	copy(result, deps)
	return result, nil
}

// FindSymbolDependencies returns all API dependencies for a symbol.
func (t *APIDepTracker) FindSymbolDependencies(symbolID string) ([]APIDependency, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	deps := t.symbolDeps[symbolID]
	if deps == nil {
		return []APIDependency{}, nil
	}

	result := make([]APIDependency, len(deps))
	copy(result, deps)
	return result, nil
}

// AllDependencies returns all detected API dependencies.
func (t *APIDepTracker) AllDependencies() []APIDependency {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]APIDependency, len(t.dependencies))
	copy(result, t.dependencies)
	return result
}

// Contracts returns all parsed API contracts.
func (t *APIDepTracker) Contracts() []APIContract {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]APIContract, len(t.contracts))
	copy(result, t.contracts)
	return result
}

// APIDepEnricher implements Enricher for API dependency tracking.
type APIDepEnricher struct {
	tracker *APIDepTracker
	mu      sync.RWMutex
	scanned bool
}

// NewAPIDepEnricher creates an enricher for API dependencies.
func NewAPIDepEnricher(projectRoot string, g *graph.Graph, idx *index.SymbolIndex) *APIDepEnricher {
	return &APIDepEnricher{
		tracker: NewAPIDepTracker(projectRoot, g, idx),
	}
}

// Name returns the enricher name.
func (e *APIDepEnricher) Name() string {
	return "api_deps"
}

// Priority returns execution priority.
func (e *APIDepEnricher) Priority() int {
	return 2
}

// Enrich adds API dependency information to the blast radius.
func (e *APIDepEnricher) Enrich(ctx context.Context, target *EnrichmentTarget, result *EnhancedBlastRadius) error {
	if ctx == nil {
		return ErrNilContext
	}

	e.mu.Lock()
	if !e.scanned {
		if err := e.tracker.Scan(ctx); err != nil {
			e.mu.Unlock()
			return err
		}
		e.scanned = true
	}
	e.mu.Unlock()

	// Get dependencies for target symbol
	deps, _ := e.tracker.FindSymbolDependencies(target.SymbolID)
	if len(deps) > 0 {
		result.APIDependencies = deps
	}

	// Also check callers for API dependencies
	for _, caller := range result.DirectCallers {
		callerDeps, _ := e.tracker.FindSymbolDependencies(caller.ID)
		result.APIDependencies = append(result.APIDependencies, callerDeps...)
	}

	return nil
}

// Invalidate forces a rescan on next enrichment.
func (e *APIDepEnricher) Invalidate() {
	e.mu.Lock()
	e.scanned = false
	e.mu.Unlock()
}
