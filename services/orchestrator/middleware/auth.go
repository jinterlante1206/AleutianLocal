// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

// Package middleware provides HTTP middleware for the orchestrator service.
//
// This package contains middleware for authentication, authorization,
// and request processing. It integrates with the extensions package
// to support enterprise features.
//
// # Authentication Flow
//
// The auth middleware extracts a bearer token from the Authorization header,
// validates it using the configured AuthProvider, and stores the resulting
// AuthInfo in the Gin context for downstream handlers.
//
//	Request
//	   │
//	   ▼
//	AuthMiddleware
//	   │
//	   ├─► Extract token from "Authorization: Bearer <token>"
//	   │
//	   ├─► provider.Validate(ctx, token)
//	   │
//	   └─► Store AuthInfo in context
//	           │
//	           ▼
//	       Handler (retrieves via GetAuthInfo)
//
// # Open Source Behavior
//
// When using NopAuthProvider (default), all requests are authenticated
// as "local-user" with admin privileges. This enables the CLI to function
// without any authentication infrastructure.
//
// # Enterprise Behavior
//
// Enterprise implementations validate tokens against identity providers
// (Okta, Auth0, Azure AD) and return real user identity information.
package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/AleutianAI/AleutianFOSS/pkg/extensions"
	"github.com/gin-gonic/gin"
)

// =============================================================================
// Context Keys
// =============================================================================

// authInfoKey is the context key for storing AuthInfo.
// Using a typed key prevents collisions with other context values.
const authInfoKey = "aleutian_auth_info"

// =============================================================================
// Context Helpers
// =============================================================================

// SetAuthInfo stores the authenticated user info in the Gin context.
//
// # Description
//
// Called by AuthMiddleware after successful authentication.
// The stored AuthInfo can be retrieved by handlers via GetAuthInfo.
//
// # Inputs
//
//   - c: Gin context. Must not be nil.
//   - info: Authenticated user information. May be nil.
//
// # Outputs
//
// None.
//
// # Examples
//
//	// In middleware after successful auth
//	authInfo, _ := provider.Validate(ctx, token)
//	SetAuthInfo(c, authInfo)
//
// # Limitations
//
//   - Only valid for current request (context is request-scoped)
//   - Overwrites any previously set auth info
//
// # Assumptions
//
//   - Gin context is valid and not nil
//   - Called within request lifecycle
//
// # Thread Safety
//
// Safe to call concurrently (Gin context is request-scoped).
func SetAuthInfo(c *gin.Context, info *extensions.AuthInfo) {
	c.Set(authInfoKey, info)
}

// GetAuthInfo retrieves the authenticated user info from the Gin context.
//
// # Description
//
// Called by handlers to access the authenticated user's identity.
// Returns nil if no AuthInfo is present (request not authenticated).
//
// # Inputs
//
//   - c: Gin context. Must not be nil.
//
// # Outputs
//
//   - *extensions.AuthInfo: User info, or nil if not authenticated
//
// # Examples
//
//	func (h *handler) HandleRequest(c *gin.Context) {
//	    authInfo := middleware.GetAuthInfo(c)
//	    if authInfo == nil {
//	        c.JSON(401, gin.H{"error": "not authenticated"})
//	        return
//	    }
//	    // Use authInfo.UserID, authInfo.Roles, etc.
//	}
//
// # Limitations
//
//   - Returns nil if SetAuthInfo was not called or called with nil
//   - Returns nil if stored value is wrong type (defensive)
//
// # Assumptions
//
//   - Gin context is valid and not nil
//   - AuthMiddleware has already processed the request
//
// # Thread Safety
//
// Safe to call concurrently (Gin context is request-scoped).
func GetAuthInfo(c *gin.Context) *extensions.AuthInfo {
	if info, exists := c.Get(authInfoKey); exists {
		if authInfo, ok := info.(*extensions.AuthInfo); ok {
			return authInfo
		}
	}
	return nil
}

// =============================================================================
// Auth Middleware
// =============================================================================

// AuthMiddleware creates a Gin middleware that authenticates requests.
//
// # Description
//
// Extracts the bearer token from the Authorization header, validates it
// using the provided AuthProvider, and stores the resulting AuthInfo
// in the context for downstream handlers.
//
// # Token Extraction
//
// The middleware expects tokens in the Authorization header:
//
//	Authorization: Bearer <token>
//
// If the header is missing or malformed, the token passed to Validate
// will be empty string. NopAuthProvider accepts this and returns local-user.
//
// # Inputs
//
//   - provider: AuthProvider to validate tokens. Must not be nil.
//
// # Outputs
//
//   - gin.HandlerFunc: Middleware function ready for use with Gin
//
// # Examples
//
//	// Apply to route group
//	v1 := router.Group("/v1")
//	v1.Use(middleware.AuthMiddleware(opts.AuthProvider))
//
//	// Apply to single route
//	router.GET("/protected", middleware.AuthMiddleware(provider), handler)
//
// # Limitations
//
//   - Only supports Bearer token authentication
//   - Does not support multiple authentication schemes
//   - Does not cache validation results (validates every request)
//
// # Assumptions
//
//   - Provider is non-nil and ready for use
//   - Provider.Validate is safe for concurrent calls
//   - ErrUnauthorized is used for auth failures (other errors treated as failures too)
//
// # Thread Safety
//
// Thread-safe. The returned middleware can be used concurrently.
func AuthMiddleware(provider extensions.AuthProvider) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract bearer token from Authorization header
		token := extractBearerToken(c)

		// Validate token using the provider
		authInfo, err := provider.Validate(c.Request.Context(), token)
		if err != nil {
			// Check if it's an authorization error
			if errors.Is(err, extensions.ErrUnauthorized) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": "unauthorized",
				})
				return
			}
			// Other errors (provider failures, network issues, etc.)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "authentication failed",
			})
			return
		}

		// Store auth info in context for handlers
		SetAuthInfo(c, authInfo)

		// Continue to next handler
		c.Next()
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

// extractBearerToken extracts the token from the Authorization header.
//
// # Description
//
// Parses the Authorization header expecting format: "Bearer <token>"
// Returns empty string if header is missing or malformed.
// The "Bearer" prefix is case-insensitive per RFC 7235.
//
// # Inputs
//
//   - c: Gin context. Must not be nil.
//
// # Outputs
//
//   - string: The extracted token, or empty string if not found
//
// # Examples
//
//	// Header: "Authorization: Bearer abc123"
//	token := extractBearerToken(c)
//	// token == "abc123"
//
//	// Header: "Authorization: bearer ABC123" (case insensitive)
//	token := extractBearerToken(c)
//	// token == "ABC123"
//
//	// Header missing or malformed
//	token := extractBearerToken(c)
//	// token == ""
//
// # Limitations
//
//   - Only extracts Bearer tokens, not Basic or other schemes
//   - Token whitespace is trimmed
//
// # Assumptions
//
//   - Gin context is valid and has an HTTP request
//   - Token does not contain leading/trailing whitespace that is significant
func extractBearerToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return ""
	}

	// Expected format: "Bearer <token>"
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}

	return strings.TrimSpace(parts[1])
}
