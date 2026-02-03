// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package auth

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
	"github.com/AleutianAI/AleutianFOSS/services/trace/safety"
)

// AuthCheckerImpl implements the safety.AuthChecker interface.
//
// Description:
//
//	AuthCheckerImpl detects endpoints missing authentication or authorization
//	middleware. It supports multiple web frameworks including Gin, Echo,
//	FastAPI, Flask, NestJS, and Express.
//
// Thread Safety:
//
//	AuthCheckerImpl is safe for concurrent use after initialization.
type AuthCheckerImpl struct {
	graph     *graph.Graph
	idx       *index.SymbolIndex
	detectors map[string]*RouteDetector

	// File content cache
	fileCache   map[string]string
	fileCacheMu sync.RWMutex
}

// NewAuthChecker creates a new auth checker.
//
// Description:
//
//	Creates a checker with support for multiple web frameworks.
//
// Inputs:
//
//	g - The code graph.
//	idx - The symbol index.
//
// Outputs:
//
//	*AuthCheckerImpl - The configured checker.
func NewAuthChecker(g *graph.Graph, idx *index.SymbolIndex) *AuthCheckerImpl {
	return &AuthCheckerImpl{
		graph:     g,
		idx:       idx,
		detectors: RoutePatterns,
		fileCache: make(map[string]string),
	}
}

// CheckAuthEnforcement checks auth on endpoints.
//
// Description:
//
//	Analyzes HTTP handlers and routes to verify they have proper
//	authentication and authorization middleware. Detects:
//	  - Endpoints without authentication
//	  - Endpoints without authorization (especially admin/mutation)
//	  - Admin endpoints without any protection
//
// Inputs:
//
//	ctx - Context for cancellation and timeout.
//	scope - The scope to check (package path).
//	opts - Optional configuration (framework hint, check type).
//
// Outputs:
//
//	*safety.AuthCheck - The check result with endpoints and issues.
//	error - Non-nil if scope not found or operation canceled.
//
// Errors:
//
//	safety.ErrInvalidInput - Scope is empty.
//	safety.ErrContextCanceled - Context was canceled.
//
// Thread Safety:
//
//	This method is safe for concurrent use.
func (c *AuthCheckerImpl) CheckAuthEnforcement(
	ctx context.Context,
	scope string,
	opts ...safety.AuthCheckOption,
) (*safety.AuthCheck, error) {
	start := time.Now()

	if ctx == nil {
		return nil, safety.ErrInvalidInput
	}

	if err := ctx.Err(); err != nil {
		return nil, safety.ErrContextCanceled
	}

	if scope == "" {
		return nil, safety.ErrInvalidInput
	}

	// Apply options
	config := safety.DefaultAuthCheckConfig()
	config.ApplyOptions(opts...)

	// Find files in scope
	files := c.findFilesInScope(scope)
	if len(files) == 0 {
		return &safety.AuthCheck{
			Scope:     scope,
			Endpoints: []safety.EndpointAuth{},
			Duration:  time.Since(start),
		}, nil
	}

	// Detect framework and collect routes
	detectedFramework := config.Framework
	var allEndpoints []safety.EndpointAuth
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Bound concurrency to avoid memory pressure on large codebases
	const maxConcurrency = 10
	semaphore := make(chan struct{}, maxConcurrency)

	for filePath := range files {
		if ctx.Err() != nil {
			break
		}

		wg.Add(1)
		semaphore <- struct{}{} // Acquire semaphore

		go func(fp string) {
			defer wg.Done()
			defer func() { <-semaphore }() // Release semaphore

			content := c.getFileContent(fp)
			if content == "" {
				return
			}

			// Detect framework
			framework := detectedFramework
			if framework == "" {
				framework = c.detectFramework(content)
			}

			if framework == "" {
				return
			}

			// Find routes
			detector, ok := c.detectors[framework]
			if !ok {
				return
			}

			routes := detector.FindRoutes(content)
			if len(routes) == 0 {
				return
			}

			// Pre-split content into lines once for efficiency
			contentLines := strings.Split(content, "\n")

			// Analyze routes in parallel if there are many
			if len(routes) > 3 {
				var routeWg sync.WaitGroup
				endpoints := make([]safety.EndpointAuth, len(routes))

				for i, route := range routes {
					routeWg.Add(1)
					go func(idx int, r DetectedRoute) {
						defer routeWg.Done()
						endpoints[idx] = c.analyzeRouteWithLines(r, content, contentLines, framework, config.CheckType)
					}(i, route)
				}
				routeWg.Wait()

				mu.Lock()
				allEndpoints = append(allEndpoints, endpoints...)
				if detectedFramework == "" {
					detectedFramework = framework
				}
				mu.Unlock()
			} else {
				// Sequential for small number of routes (lower overhead)
				for _, route := range routes {
					endpoint := c.analyzeRouteWithLines(route, content, contentLines, framework, config.CheckType)

					mu.Lock()
					allEndpoints = append(allEndpoints, endpoint)
					if detectedFramework == "" {
						detectedFramework = framework
					}
					mu.Unlock()
				}
			}
		}(filePath)
	}

	wg.Wait()

	// Build result
	result := &safety.AuthCheck{
		Scope:     scope,
		Framework: detectedFramework,
		Endpoints: allEndpoints,
		Duration:  time.Since(start),
	}

	// Count issues
	for _, ep := range allEndpoints {
		if !ep.HasAuthentication && (config.CheckType == "both" || config.CheckType == "authentication") {
			result.MissingAuth++
		}
		if !ep.HasAuthorization && (config.CheckType == "both" || config.CheckType == "authorization") {
			// Only count as missing authz if it's a mutation or admin endpoint
			if IsMutationMethod(ep.Method) || ep.IsAdminEndpoint {
				result.MissingAuthz++
			}
		}
	}

	// Generate suggestions
	result.Suggestions = c.generateSuggestions(result, detectedFramework)

	// Add framework details
	if detectedFramework != "" {
		result.FrameworkDetails = &safety.FrameworkInfo{
			Name:       detectedFramework,
			Confidence: 0.9,
			Indicators: []string{fmt.Sprintf("Detected via import/route patterns")},
		}
	}

	return result, nil
}

// analyzeRoute analyzes a single route for auth enforcement.
// Deprecated: Use analyzeRouteWithLines for better performance.
func (c *AuthCheckerImpl) analyzeRoute(
	route DetectedRoute,
	content string,
	framework string,
	checkType string,
) safety.EndpointAuth {
	// Split lines once and delegate
	contentLines := strings.Split(content, "\n")
	return c.analyzeRouteWithLines(route, content, contentLines, framework, checkType)
}

// analyzeRouteWithLines analyzes a single route for auth enforcement.
// Uses pre-split lines for efficiency when analyzing multiple routes.
func (c *AuthCheckerImpl) analyzeRouteWithLines(
	route DetectedRoute,
	content string,
	contentLines []string,
	framework string,
	checkType string,
) safety.EndpointAuth {
	endpoint := safety.EndpointAuth{
		Name:            route.Handler,
		Type:            "http",
		Path:            route.Path,
		Method:          route.Method,
		Framework:       framework,
		IsAdminEndpoint: IsAdminPath(route.Path),
		HandlesData:     IsMutationMethod(route.Method) || IsSensitivePath(route.Path),
	}

	// Check for authentication middleware
	if checkType == "both" || checkType == "authentication" {
		hasAuth, authMethod := c.hasAuthMiddlewareWithLines(content, contentLines, route, framework)
		endpoint.HasAuthentication = hasAuth
		endpoint.AuthMethod = authMethod
	}

	// Check for authorization middleware
	if checkType == "both" || checkType == "authorization" {
		hasAuthz, authzMethod := c.hasAuthzMiddlewareWithLines(content, contentLines, route, framework)
		endpoint.HasAuthorization = hasAuthz
		endpoint.AuthzMethod = authzMethod
	}

	// Determine risk
	endpoint.Risk = c.assessRisk(endpoint)

	return endpoint
}

// hasAuthMiddleware checks if a route has authentication middleware.
// Deprecated: Use hasAuthMiddlewareWithLines for better performance.
func (c *AuthCheckerImpl) hasAuthMiddleware(content string, route DetectedRoute, framework string) (bool, string) {
	contentLines := strings.Split(content, "\n")
	return c.hasAuthMiddlewareWithLines(content, contentLines, route, framework)
}

// hasAuthMiddlewareWithLines checks if a route has authentication middleware.
// Uses pre-split lines for efficiency when checking multiple routes.
func (c *AuthCheckerImpl) hasAuthMiddlewareWithLines(content string, contentLines []string, route DetectedRoute, framework string) (bool, string) {
	patterns, ok := AuthMiddlewarePatterns[framework]
	if !ok {
		return false, ""
	}

	// For decorator-based frameworks (FastAPI, Flask, NestJS), only check near the route
	// These frameworks use per-endpoint decorators, not global middleware
	isDecoratorBased := framework == "fastapi" || framework == "flask" || framework == "nestjs"

	if !isDecoratorBased {
		// Check global middleware (at top of file) for middleware-based frameworks
		globalArea := content[:min(len(content), 500)]

		for _, pattern := range patterns {
			if strings.Contains(globalArea, pattern) {
				return true, pattern
			}
		}
	}

	// Get the lines around the route for checking
	// For decorator-based frameworks, check only 2 lines before and 3 lines after
	// (decorators appear before, auth params appear in the function signature)
	linesBefore := 2
	linesAfter := 3
	if !isDecoratorBased {
		linesBefore = 5
		linesAfter = 5
	}

	routeArea := getLinesAroundRouteFromSlice(contentLines, route.Line, linesBefore, linesAfter)

	for _, pattern := range patterns {
		if strings.Contains(routeArea, pattern) {
			return true, pattern
		}
	}

	return false, ""
}

// getLinesAroundRoute extracts lines around a route definition.
// Deprecated: Use getLinesAroundRouteFromSlice for better performance.
func getLinesAroundRoute(content string, routeLine, linesBefore, linesAfter int) string {
	lines := strings.Split(content, "\n")
	return getLinesAroundRouteFromSlice(lines, routeLine, linesBefore, linesAfter)
}

// getLinesAroundRouteFromSlice extracts lines around a route definition using pre-split lines.
func getLinesAroundRouteFromSlice(lines []string, routeLine, linesBefore, linesAfter int) string {
	// Validate routeLine (1-indexed)
	if routeLine <= 0 {
		if len(lines) > 0 {
			return strings.Join(lines[:min(len(lines), linesAfter)], "\n")
		}
		return ""
	}

	startLine := max(0, routeLine-1-linesBefore) // routeLine is 1-indexed
	endLine := min(len(lines), routeLine+linesAfter)

	if startLine >= len(lines) {
		startLine = 0
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	if startLine >= endLine {
		// Fallback: return all lines joined
		return strings.Join(lines, "\n")
	}

	return strings.Join(lines[startLine:endLine], "\n")
}

// hasAuthzMiddleware checks if a route has authorization middleware.
// Deprecated: Use hasAuthzMiddlewareWithLines for better performance.
func (c *AuthCheckerImpl) hasAuthzMiddleware(content string, route DetectedRoute, framework string) (bool, string) {
	contentLines := strings.Split(content, "\n")
	return c.hasAuthzMiddlewareWithLines(content, contentLines, route, framework)
}

// hasAuthzMiddlewareWithLines checks if a route has authorization middleware.
// Uses pre-split lines for efficiency when checking multiple routes.
func (c *AuthCheckerImpl) hasAuthzMiddlewareWithLines(content string, contentLines []string, route DetectedRoute, framework string) (bool, string) {
	patterns, ok := AuthzMiddlewarePatterns[framework]
	if !ok {
		return false, ""
	}

	// For decorator-based frameworks, only check near the route
	isDecoratorBased := framework == "fastapi" || framework == "flask" || framework == "nestjs"

	if !isDecoratorBased {
		// Check global middleware
		globalArea := content[:min(len(content), 500)]

		for _, pattern := range patterns {
			if strings.Contains(globalArea, pattern) {
				return true, pattern
			}
		}
	}

	// Get the lines around the route for checking
	linesBefore := 2
	linesAfter := 3
	if !isDecoratorBased {
		linesBefore = 5
		linesAfter = 5
	}

	routeArea := getLinesAroundRouteFromSlice(contentLines, route.Line, linesBefore, linesAfter)

	for _, pattern := range patterns {
		if strings.Contains(routeArea, pattern) {
			return true, pattern
		}
	}

	return false, ""
}

// assessRisk determines the risk level for an endpoint.
func (c *AuthCheckerImpl) assessRisk(endpoint safety.EndpointAuth) safety.Severity {
	// Critical: Admin endpoint without auth
	if endpoint.IsAdminEndpoint && !endpoint.HasAuthentication {
		return safety.SeverityCritical
	}

	// High: Mutation endpoint without auth
	if IsMutationMethod(endpoint.Method) && !endpoint.HasAuthentication {
		return safety.SeverityHigh
	}

	// High: Admin endpoint without authz
	if endpoint.IsAdminEndpoint && endpoint.HasAuthentication && !endpoint.HasAuthorization {
		return safety.SeverityHigh
	}

	// Medium: Mutation endpoint without authz
	if IsMutationMethod(endpoint.Method) && endpoint.HasAuthentication && !endpoint.HasAuthorization {
		return safety.SeverityMedium
	}

	// Medium: Sensitive path without auth
	if IsSensitivePath(endpoint.Path) && !endpoint.HasAuthentication {
		return safety.SeverityMedium
	}

	// Low: GET endpoint without auth (might be intentional)
	if !endpoint.HasAuthentication {
		return safety.SeverityLow
	}

	return ""
}

// detectFramework detects the web framework in content.
func (c *AuthCheckerImpl) detectFramework(content string) string {
	for framework, detector := range c.detectors {
		if detector.DetectFramework(content) {
			return framework
		}
	}
	return ""
}

// generateSuggestions generates improvement suggestions.
func (c *AuthCheckerImpl) generateSuggestions(result *safety.AuthCheck, framework string) []string {
	var suggestions []string

	if result.MissingAuth > 0 {
		switch framework {
		case "gin":
			suggestions = append(suggestions, "Add authentication middleware: router.Use(authMiddleware())")
		case "echo":
			suggestions = append(suggestions, "Add JWT middleware: e.Use(middleware.JWT([]byte(secret)))")
		case "fastapi":
			suggestions = append(suggestions, "Add dependency: Depends(get_current_user)")
		case "flask":
			suggestions = append(suggestions, "Add decorator: @login_required")
		case "express":
			suggestions = append(suggestions, "Add passport: passport.authenticate('jwt', {session: false})")
		case "nestjs":
			suggestions = append(suggestions, "Add guard: @UseGuards(JwtAuthGuard)")
		default:
			suggestions = append(suggestions, "Add authentication middleware to protect endpoints")
		}
	}

	if result.MissingAuthz > 0 {
		suggestions = append(suggestions, "Consider adding role-based access control (RBAC) for mutation endpoints")
		suggestions = append(suggestions, "Admin endpoints should verify user has admin role before processing")
	}

	// Count critical issues
	criticalCount := 0
	for _, ep := range result.Endpoints {
		if ep.Risk == safety.SeverityCritical {
			criticalCount++
		}
	}

	if criticalCount > 0 {
		suggestions = append(suggestions,
			fmt.Sprintf("CRITICAL: %d endpoint(s) have admin paths without authentication - fix immediately", criticalCount))
	}

	return suggestions
}

// findFilesInScope finds all files in a scope.
func (c *AuthCheckerImpl) findFilesInScope(scope string) map[string]bool {
	filesMap := make(map[string]bool)

	for _, node := range c.graph.Nodes() {
		if node.Symbol == nil || node.Symbol.FilePath == "" {
			continue
		}

		// Match by package
		if node.Symbol.Package == scope {
			filesMap[node.Symbol.FilePath] = true
			continue
		}

		// Match by file path prefix
		if strings.HasPrefix(node.Symbol.FilePath, scope) {
			filesMap[node.Symbol.FilePath] = true
			continue
		}

		// Match by package prefix
		if strings.HasPrefix(node.Symbol.Package, scope) {
			filesMap[node.Symbol.FilePath] = true
			continue
		}
	}

	return filesMap
}

// getFileContent retrieves file content from cache.
func (c *AuthCheckerImpl) getFileContent(filePath string) string {
	c.fileCacheMu.RLock()
	content, ok := c.fileCache[filePath]
	c.fileCacheMu.RUnlock()

	if ok {
		return content
	}

	return ""
}

// SetFileContent sets file content for checking.
func (c *AuthCheckerImpl) SetFileContent(filePath, content string) {
	c.fileCacheMu.Lock()
	c.fileCache[filePath] = content
	c.fileCacheMu.Unlock()
}

// ClearFileCache clears the file content cache.
func (c *AuthCheckerImpl) ClearFileCache() {
	c.fileCacheMu.Lock()
	c.fileCache = make(map[string]string)
	c.fileCacheMu.Unlock()
}
