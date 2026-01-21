// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package extensions

import (
	"context"
	"errors"
)

// ErrUnauthorized is returned when authentication or authorization fails.
// Enterprise implementations should wrap this error with additional context.
//
// Example:
//
//	if !validToken {
//	    return nil, fmt.Errorf("invalid token format: %w", extensions.ErrUnauthorized)
//	}
var ErrUnauthorized = errors.New("unauthorized")

// AuthInfo contains identity information returned after successful authentication.
//
// This struct is designed to be extensible via the Metadata field, allowing
// enterprise implementations to include additional claims without modifying
// the core type.
//
// Required fields (always populated):
//   - UserID: Unique identifier for the user
//
// Optional fields (may be empty):
//   - Email: User's email address
//   - Roles: List of roles/groups the user belongs to
//   - Metadata: Arbitrary key-value pairs for enterprise extensions
//
// Example:
//
//	info := &AuthInfo{
//	    UserID: "user-123",
//	    Email:  "user@example.com",
//	    Roles:  []string{"analyst", "viewer"},
//	    Metadata: NewMetadata().
//	        Set("department", "engineering").
//	        Set("mfa_verified", true),
//	}
type AuthInfo struct {
	// UserID is the unique identifier for the authenticated user.
	// This is the only required field and must never be empty.
	UserID string

	// Email is the user's email address.
	// May be empty if not provided by the auth provider.
	Email string

	// Roles contains the user's role memberships for authorization decisions.
	// Common roles: "admin", "analyst", "viewer", "auditor"
	Roles []string

	// Metadata holds additional claims from the identity provider.
	// Enterprise implementations can store provider-specific data here
	// without requiring changes to the core struct.
	//
	// Common metadata keys:
	//   - "groups": []string of group memberships
	//   - "department": organizational unit
	//   - "mfa_verified": whether MFA was used
	//   - "session_id": identity provider session ID
	//
	// Use NewMetadata() and type-safe accessors:
	//
	//   Metadata: NewMetadata().
	//       Set("department", "engineering").
	//       Set("mfa_verified", true),
	Metadata Metadata
}

// HasRole checks if the user has a specific role.
//
// This is a convenience method for authorization checks:
//
//	if !authInfo.HasRole("admin") {
//	    return ErrUnauthorized
//	}
func (a *AuthInfo) HasRole(role string) bool {
	for _, r := range a.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// AuthProvider validates authentication tokens and returns user identity.
//
// Implementations must be safe for concurrent use by multiple goroutines.
//
// # Open Source Behavior
//
// The default NopAuthProvider always returns a valid "local-user" with admin
// privileges. This allows the local CLI to function without any authentication
// infrastructure.
//
// # Enterprise Implementation
//
// Enterprise versions implement this interface to validate tokens against
// identity providers like Okta, Auth0, or Azure AD.
//
// Example enterprise implementation:
//
//	type OktaAuthProvider struct {
//	    client *okta.Client
//	}
//
//	func (p *OktaAuthProvider) Validate(ctx context.Context, token string) (*AuthInfo, error) {
//	    claims, err := p.client.VerifyToken(ctx, token)
//	    if err != nil {
//	        return nil, fmt.Errorf("okta validation failed: %w", ErrUnauthorized)
//	    }
//	    return &AuthInfo{
//	        UserID: claims.Subject,
//	        Email:  claims.Email,
//	        Roles:  claims.Groups,
//	    }, nil
//	}
type AuthProvider interface {
	// Validate checks if the token is valid and returns the user's identity.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control
	//   - token: The authentication token (JWT, session ID, API key, etc.)
	//
	// Returns:
	//   - *AuthInfo: User identity information if valid
	//   - error: ErrUnauthorized (or wrapped) if invalid, other errors for failures
	//
	// The token format is implementation-specific:
	//   - JWT: "eyJhbGciOiJSUzI1NiIs..."
	//   - API Key: "ak_live_..."
	//   - Session: "sess_..."
	Validate(ctx context.Context, token string) (*AuthInfo, error)
}

// AuthzRequest describes an authorization check request.
//
// This struct follows the common pattern of (subject, action, resource)
// for access control decisions.
//
// Example:
//
//	req := AuthzRequest{
//	    User:         authInfo,
//	    Action:       "read",
//	    ResourceType: "evaluation",
//	    ResourceID:   "eval-456",
//	}
//	err := authzProvider.Authorize(ctx, req)
type AuthzRequest struct {
	// User is the authenticated user making the request.
	// This comes from AuthProvider.Validate().
	User *AuthInfo

	// Action is the operation being attempted.
	// Common actions: "create", "read", "update", "delete", "execute"
	Action string

	// ResourceType is the category of resource being accessed.
	// Examples: "evaluation", "model", "session", "report"
	ResourceType string

	// ResourceID is the specific resource instance (optional).
	// If empty, the check is for the resource type in general.
	// Examples: "eval-123", "model-456"
	ResourceID string
}

// AuthzProvider checks if a user is authorized to perform an action.
//
// Implementations must be safe for concurrent use by multiple goroutines.
//
// # Open Source Behavior
//
// The default NopAuthzProvider always allows all actions. This is appropriate
// for single-user local deployments where access control isn't needed.
//
// # Enterprise Implementation
//
// Enterprise versions implement RBAC, ABAC, or policy-based access control.
//
// Example enterprise implementation:
//
//	type RBACProvider struct {
//	    policies *PolicyEngine
//	}
//
//	func (p *RBACProvider) Authorize(ctx context.Context, req AuthzRequest) error {
//	    allowed := p.policies.Check(req.User.Roles, req.Action, req.ResourceType)
//	    if !allowed {
//	        return fmt.Errorf("user %s cannot %s %s: %w",
//	            req.User.UserID, req.Action, req.ResourceType, ErrUnauthorized)
//	    }
//	    return nil
//	}
type AuthzProvider interface {
	// Authorize checks if the user is permitted to perform the action.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control
	//   - req: The authorization request describing user, action, and resource
	//
	// Returns:
	//   - nil: Action is authorized
	//   - error: ErrUnauthorized (or wrapped) if denied
	Authorize(ctx context.Context, req AuthzRequest) error
}

// NopAuthProvider is the default authentication provider for open source.
//
// It always returns a valid local user with admin privileges, enabling
// the CLI to function without any authentication infrastructure.
//
// Thread-safe: This implementation has no mutable state.
//
// Example:
//
//	provider := &NopAuthProvider{}
//	info, err := provider.Validate(ctx, "any-token")
//	// info.UserID == "local-user"
//	// info.Roles == []string{"admin"}
//	// err == nil
type NopAuthProvider struct{}

// Validate always returns a valid local user with admin privileges.
//
// The token parameter is ignored - any value (including empty string)
// results in successful authentication. This is intentional for local
// single-user deployments.
func (p *NopAuthProvider) Validate(_ context.Context, _ string) (*AuthInfo, error) {
	return &AuthInfo{
		UserID: "local-user",
		Email:  "",
		Roles:  []string{"admin"},
	}, nil
}

// NopAuthzProvider is the default authorization provider for open source.
//
// It always allows all actions, enabling the CLI to function without
// any access control infrastructure.
//
// Thread-safe: This implementation has no mutable state.
//
// Example:
//
//	provider := &NopAuthzProvider{}
//	err := provider.Authorize(ctx, AuthzRequest{
//	    User:         &AuthInfo{UserID: "anyone"},
//	    Action:       "delete",
//	    ResourceType: "everything",
//	})
//	// err == nil (always allowed)
type NopAuthzProvider struct{}

// Authorize always returns nil, allowing all actions.
//
// The request parameter is ignored - all actions are permitted.
// This is intentional for local single-user deployments where
// access control isn't needed.
func (p *NopAuthzProvider) Authorize(_ context.Context, _ AuthzRequest) error {
	return nil
}

// Compile-time interface compliance checks.
// These ensure NopAuthProvider and NopAuthzProvider implement their interfaces.
var (
	_ AuthProvider  = (*NopAuthProvider)(nil)
	_ AuthzProvider = (*NopAuthzProvider)(nil)
)
