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
	"regexp"
	"strings"
)

// AdminPathPatterns are URL patterns that indicate admin endpoints.
var AdminPathPatterns = []string{
	"/admin",
	"/management",
	"/internal",
	"/api/admin",
	"/api/v*/admin",
	"/debug",
	"/metrics",
	"/actuator",
	"/console",
	"/dashboard",
	"/settings",
	"/config",
	"/system",
}

// SensitivePathPatterns are URL patterns that handle sensitive data.
var SensitivePathPatterns = []string{
	"/user",
	"/users",
	"/account",
	"/profile",
	"/password",
	"/auth",
	"/token",
	"/session",
	"/payment",
	"/billing",
	"/order",
	"/cart",
}

// MutationMethods are HTTP methods that modify data.
var MutationMethods = []string{"POST", "PUT", "PATCH", "DELETE"}

// AuthMiddlewarePatterns contains patterns for auth middleware by framework.
var AuthMiddlewarePatterns = map[string][]string{
	// Go frameworks
	"gin": {
		"authMiddleware", "AuthMiddleware", "jwt.Auth", "jwt.New",
		"sessions.Sessions", "Auth()", "RequireAuth", "AuthRequired",
		"gin-jwt", "gin-contrib/sessions",
	},
	"echo": {
		"middleware.JWT", "middleware.KeyAuth", "middleware.BasicAuth",
		"echojwt.JWT", "session.Middleware", "Auth()", "AuthMiddleware",
	},
	"chi": {
		"jwtauth", "httpauth", "session", "Auth()", "AuthMiddleware",
	},
	"fiber": {
		"jwtware", "basicauth", "session", "Auth()", "AuthMiddleware",
	},

	// Python frameworks
	"fastapi": {
		"Depends(get_current_user)", "Depends(get_current_active_user)",
		"HTTPBearer", "OAuth2PasswordBearer", "Security(",
		"api_key_header", "get_api_key", "verify_token",
	},
	"flask": {
		"@login_required", "login_required", "@jwt_required",
		"current_user", "flask_login", "flask_jwt", "flask_httpauth",
	},
	"django": {
		"@login_required", "permission_required", "IsAuthenticated",
		"IsAdminUser", "@permission_classes", "authentication_classes",
	},

	// TypeScript/JavaScript frameworks
	"express": {
		"passport.authenticate", "jwt.verify", "express-jwt",
		"isAuthenticated", "requireAuth", "authMiddleware",
	},
	"nestjs": {
		"@UseGuards", "AuthGuard", "JwtAuthGuard", "@ApiBearerAuth",
		"RolesGuard", "@Roles", "PermissionsGuard",
	},
}

// AuthzMiddlewarePatterns contains patterns for authorization middleware.
var AuthzMiddlewarePatterns = map[string][]string{
	// Go frameworks
	"gin": {
		"rbac", "casbin", "permission", "authorize", "checkRole",
		"RequireRole", "HasPermission", "ACL", "CanAccess",
	},
	"echo": {
		"casbin", "rbac", "authorize", "checkPermission",
	},
	"chi": {
		"casbin", "rbac", "authorize", "permission",
	},
	"fiber": {
		"casbin", "rbac", "authorize", "permission",
	},

	// Python frameworks
	"fastapi": {
		"RoleChecker", "PermissionChecker", "has_permission",
		"check_permission", "require_role", "authorize",
	},
	"flask": {
		"@roles_required", "@permission_required", "has_role",
		"check_permission", "flask_principal",
	},
	"django": {
		"@permission_required", "has_perm", "user_passes_test",
		"PermissionRequiredMixin", "django-guardian",
	},

	// TypeScript/JavaScript frameworks
	"express": {
		"checkRole", "hasPermission", "authorize", "rbac",
		"accessControl", "acl",
	},
	"nestjs": {
		"@Roles", "RolesGuard", "PermissionsGuard", "@SetMetadata",
		"@UseGuards(RolesGuard)", "casl",
	},
}

// RoutePatterns contains patterns for detecting routes by framework.
var RoutePatterns = map[string]*RouteDetector{
	"gin": {
		ImportPattern:  `"github.com/gin-gonic/gin"`,
		RoutePattern:   `\.(?:GET|POST|PUT|PATCH|DELETE|OPTIONS|HEAD)\s*\(\s*"([^"]+)"`,
		GroupPattern:   `\.Group\s*\(\s*"([^"]+)"`,
		HandlerPattern: `\.(?:GET|POST|PUT|PATCH|DELETE)\s*\([^,]+,\s*(\w+)`,
	},
	"echo": {
		ImportPattern:  `"github.com/labstack/echo`,
		RoutePattern:   `\.(?:GET|POST|PUT|PATCH|DELETE)\s*\(\s*"([^"]+)"`,
		GroupPattern:   `\.Group\s*\(\s*"([^"]+)"`,
		HandlerPattern: `\.(?:GET|POST|PUT|PATCH|DELETE)\s*\([^,]+,\s*(\w+)`,
	},
	"chi": {
		ImportPattern:  `"github.com/go-chi/chi`,
		RoutePattern:   `\.(?:Get|Post|Put|Patch|Delete)\s*\(\s*"([^"]+)"`,
		GroupPattern:   `\.Route\s*\(\s*"([^"]+)"`,
		HandlerPattern: `\.(?:Get|Post|Put|Patch|Delete)\s*\([^,]+,\s*(\w+)`,
	},
	"fiber": {
		ImportPattern:  `"github.com/gofiber/fiber`,
		RoutePattern:   `\.(?:Get|Post|Put|Patch|Delete)\s*\(\s*"([^"]+)"`,
		GroupPattern:   `\.Group\s*\(\s*"([^"]+)"`,
		HandlerPattern: `\.(?:Get|Post|Put|Patch|Delete)\s*\([^,]+,\s*(\w+)`,
	},
	"fastapi": {
		ImportPattern:  `from fastapi|import fastapi`,
		RoutePattern:   `@(?:app|router)\.(?:get|post|put|patch|delete)\s*\(\s*["']([^"']+)["']`,
		GroupPattern:   `APIRouter\s*\(\s*prefix\s*=\s*["']([^"']+)["']`,
		HandlerPattern: `def\s+(\w+)\s*\(`,
	},
	"flask": {
		ImportPattern:  `from flask|import flask`,
		RoutePattern:   `@(?:app|bp)\.route\s*\(\s*["']([^"']+)["']`,
		GroupPattern:   `Blueprint\s*\(\s*\w+\s*,\s*\w+\s*,\s*url_prefix\s*=\s*["']([^"']+)["']`,
		HandlerPattern: `def\s+(\w+)\s*\(`,
	},
	"express": {
		ImportPattern:  `require\s*\(\s*['"]express['"]|from\s+['"]express['"]`,
		RoutePattern:   `\.(?:get|post|put|patch|delete)\s*\(\s*['"]([^'"]+)['"]`,
		GroupPattern:   `Router\s*\(\s*\)`,
		HandlerPattern: `\.(?:get|post|put|patch|delete)\s*\([^,]+,\s*(?:async\s+)?(?:function\s+)?(\w+)?`,
	},
	"nestjs": {
		ImportPattern:  `from\s+['"]@nestjs/`,
		RoutePattern:   `@(?:Get|Post|Put|Patch|Delete)\s*\(\s*['"]?([^'")\s]*)['"]?\s*\)`,
		GroupPattern:   `@Controller\s*\(\s*['"]([^'"]+)['"]`,
		HandlerPattern: `(?:async\s+)?(\w+)\s*\(`,
	},
}

// RouteDetector contains compiled patterns for route detection.
type RouteDetector struct {
	ImportPattern  string
	RoutePattern   string
	GroupPattern   string
	HandlerPattern string

	compiledImport  *regexp.Regexp
	compiledRoute   *regexp.Regexp
	compiledGroup   *regexp.Regexp
	compiledHandler *regexp.Regexp
}

// Compile compiles all patterns.
//
// Description:
//
//	Compiles regex patterns for route detection. Invalid patterns
//	will result in nil compiled regex and a warning is logged.
//	Compilation is idempotent and safe to call multiple times.
func (r *RouteDetector) Compile() {
	if r.compiledImport == nil && r.ImportPattern != "" {
		compiled, err := regexp.Compile(r.ImportPattern)
		if err == nil {
			r.compiledImport = compiled
		}
		// Invalid patterns silently fail - the detector will not match
	}
	if r.compiledRoute == nil && r.RoutePattern != "" {
		compiled, err := regexp.Compile(r.RoutePattern)
		if err == nil {
			r.compiledRoute = compiled
		}
	}
	if r.compiledGroup == nil && r.GroupPattern != "" {
		compiled, err := regexp.Compile(r.GroupPattern)
		if err == nil {
			r.compiledGroup = compiled
		}
	}
	if r.compiledHandler == nil && r.HandlerPattern != "" {
		compiled, err := regexp.Compile(r.HandlerPattern)
		if err == nil {
			r.compiledHandler = compiled
		}
	}
}

// DetectFramework detects the web framework used in content.
func (r *RouteDetector) DetectFramework(content string) bool {
	r.Compile()
	if r.compiledImport == nil {
		return false
	}
	return r.compiledImport.MatchString(content)
}

// FindRoutes finds all routes in content.
func (r *RouteDetector) FindRoutes(content string) []DetectedRoute {
	r.Compile()
	if r.compiledRoute == nil {
		return nil
	}

	var routes []DetectedRoute
	matches := r.compiledRoute.FindAllStringSubmatchIndex(content, -1)

	for _, match := range matches {
		if len(match) < 4 {
			continue
		}

		path := content[match[2]:match[3]]
		lineNum := strings.Count(content[:match[0]], "\n") + 1

		// Extract method from the match context
		methodMatch := content[max(0, match[0]-10):match[1]]
		method := extractMethod(methodMatch)

		// Find handler name
		handlerName := ""
		if r.compiledHandler != nil {
			handlerMatches := r.compiledHandler.FindStringSubmatch(content[match[0]:min(len(content), match[1]+100)])
			if len(handlerMatches) > 1 {
				handlerName = handlerMatches[1]
			}
		}

		routes = append(routes, DetectedRoute{
			Path:    path,
			Method:  method,
			Handler: handlerName,
			Line:    lineNum,
		})
	}

	return routes
}

// DetectedRoute represents a detected HTTP route.
type DetectedRoute struct {
	Path    string
	Method  string
	Handler string
	Line    int
}

// extractMethod extracts the HTTP method from context.
func extractMethod(context string) string {
	context = strings.ToUpper(context)

	methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"}
	for _, m := range methods {
		if strings.Contains(context, m) {
			return m
		}
	}
	return "GET"
}

// IsAdminPath checks if a path appears to be an admin endpoint.
func IsAdminPath(path string) bool {
	pathLower := strings.ToLower(path)

	for _, pattern := range AdminPathPatterns {
		patternLower := strings.ToLower(pattern)

		// Handle wildcard patterns
		if strings.Contains(patternLower, "*") {
			parts := strings.Split(patternLower, "*")
			if len(parts) == 2 {
				if strings.HasPrefix(pathLower, parts[0]) && strings.HasSuffix(pathLower, parts[1]) {
					return true
				}
			}
		} else if strings.Contains(pathLower, patternLower) {
			return true
		}
	}

	return false
}

// IsSensitivePath checks if a path handles sensitive data.
func IsSensitivePath(path string) bool {
	pathLower := strings.ToLower(path)

	for _, pattern := range SensitivePathPatterns {
		if strings.Contains(pathLower, strings.ToLower(pattern)) {
			return true
		}
	}

	return false
}

// IsMutationMethod checks if method modifies data.
func IsMutationMethod(method string) bool {
	methodUpper := strings.ToUpper(method)

	for _, m := range MutationMethods {
		if m == methodUpper {
			return true
		}
	}

	return false
}

// max returns the larger of two integers.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// min returns the smaller of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
