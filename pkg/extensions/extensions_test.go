// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package extensions

import (
	"context"
	"testing"
	"time"
)

// ============================================================================
// ServiceOptions Tests
// ============================================================================

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	// Verify all fields are set to non-nil nop implementations
	if opts.AuthProvider == nil {
		t.Error("DefaultOptions().AuthProvider should not be nil")
	}
	if opts.AuthzProvider == nil {
		t.Error("DefaultOptions().AuthzProvider should not be nil")
	}
	if opts.AuditLogger == nil {
		t.Error("DefaultOptions().AuditLogger should not be nil")
	}
	if opts.MessageFilter == nil {
		t.Error("DefaultOptions().MessageFilter should not be nil")
	}

	// Verify they are the correct nop types
	if _, ok := opts.AuthProvider.(*NopAuthProvider); !ok {
		t.Error("DefaultOptions().AuthProvider should be *NopAuthProvider")
	}
	if _, ok := opts.AuthzProvider.(*NopAuthzProvider); !ok {
		t.Error("DefaultOptions().AuthzProvider should be *NopAuthzProvider")
	}
	if _, ok := opts.AuditLogger.(*NopAuditLogger); !ok {
		t.Error("DefaultOptions().AuditLogger should be *NopAuditLogger")
	}
	if _, ok := opts.MessageFilter.(*NopMessageFilter); !ok {
		t.Error("DefaultOptions().MessageFilter should be *NopMessageFilter")
	}
}

func TestServiceOptions_WithAuth(t *testing.T) {
	original := DefaultOptions()
	customAuth := &mockAuthProvider{userID: "custom-user"}

	// WithAuth should return a new options with the custom auth provider
	newOpts := original.WithAuth(customAuth)

	// New options should have the custom provider
	if newOpts.AuthProvider != customAuth {
		t.Error("WithAuth should set the custom AuthProvider")
	}

	// Original should be unchanged (immutable copy)
	if _, ok := original.AuthProvider.(*NopAuthProvider); !ok {
		t.Error("Original options should be unchanged after WithAuth")
	}

	// Other fields should be preserved
	if newOpts.AuthzProvider == nil {
		t.Error("WithAuth should preserve AuthzProvider")
	}
	if newOpts.AuditLogger == nil {
		t.Error("WithAuth should preserve AuditLogger")
	}
	if newOpts.MessageFilter == nil {
		t.Error("WithAuth should preserve MessageFilter")
	}
}

func TestServiceOptions_WithAuthz(t *testing.T) {
	original := DefaultOptions()
	customAuthz := &mockAuthzProvider{}

	newOpts := original.WithAuthz(customAuthz)

	if newOpts.AuthzProvider != customAuthz {
		t.Error("WithAuthz should set the custom AuthzProvider")
	}

	// Original should be unchanged
	if _, ok := original.AuthzProvider.(*NopAuthzProvider); !ok {
		t.Error("Original options should be unchanged after WithAuthz")
	}
}

func TestServiceOptions_WithAudit(t *testing.T) {
	original := DefaultOptions()
	customAudit := &mockAuditLogger{}

	newOpts := original.WithAudit(customAudit)

	if newOpts.AuditLogger != customAudit {
		t.Error("WithAudit should set the custom AuditLogger")
	}

	// Original should be unchanged
	if _, ok := original.AuditLogger.(*NopAuditLogger); !ok {
		t.Error("Original options should be unchanged after WithAudit")
	}
}

func TestServiceOptions_WithFilter(t *testing.T) {
	original := DefaultOptions()
	customFilter := &mockMessageFilter{}

	newOpts := original.WithFilter(customFilter)

	if newOpts.MessageFilter != customFilter {
		t.Error("WithFilter should set the custom MessageFilter")
	}

	// Original should be unchanged
	if _, ok := original.MessageFilter.(*NopMessageFilter); !ok {
		t.Error("Original options should be unchanged after WithFilter")
	}
}

func TestServiceOptions_FluentChaining(t *testing.T) {
	// Test that all With* methods can be chained
	customAuth := &mockAuthProvider{userID: "chained-user"}
	customAuthz := &mockAuthzProvider{}
	customAudit := &mockAuditLogger{}
	customFilter := &mockMessageFilter{}

	opts := DefaultOptions().
		WithAuth(customAuth).
		WithAuthz(customAuthz).
		WithAudit(customAudit).
		WithFilter(customFilter)

	if opts.AuthProvider != customAuth {
		t.Error("Chained WithAuth should set AuthProvider")
	}
	if opts.AuthzProvider != customAuthz {
		t.Error("Chained WithAuthz should set AuthzProvider")
	}
	if opts.AuditLogger != customAudit {
		t.Error("Chained WithAudit should set AuditLogger")
	}
	if opts.MessageFilter != customFilter {
		t.Error("Chained WithFilter should set MessageFilter")
	}
}

// ============================================================================
// AuditEvent Tests
// ============================================================================

func TestAuditEvent_Fields(t *testing.T) {
	now := time.Now().UTC()
	metadata := map[string]any{
		"session_id": "sess-123",
		"model":      "claude-3",
	}

	event := AuditEvent{
		EventType:    "chat.message",
		Timestamp:    now,
		UserID:       "user-123",
		Action:       "send",
		ResourceType: "message",
		ResourceID:   "msg-456",
		Outcome:      "success",
		Metadata:     metadata,
	}

	if event.EventType != "chat.message" {
		t.Errorf("EventType = %q, want %q", event.EventType, "chat.message")
	}
	if event.Timestamp != now {
		t.Errorf("Timestamp = %v, want %v", event.Timestamp, now)
	}
	if event.UserID != "user-123" {
		t.Errorf("UserID = %q, want %q", event.UserID, "user-123")
	}
	if event.Action != "send" {
		t.Errorf("Action = %q, want %q", event.Action, "send")
	}
	if event.ResourceType != "message" {
		t.Errorf("ResourceType = %q, want %q", event.ResourceType, "message")
	}
	if event.ResourceID != "msg-456" {
		t.Errorf("ResourceID = %q, want %q", event.ResourceID, "msg-456")
	}
	if event.Outcome != "success" {
		t.Errorf("Outcome = %q, want %q", event.Outcome, "success")
	}
	if event.Metadata["session_id"] != "sess-123" {
		t.Errorf("Metadata[session_id] = %v, want %q", event.Metadata["session_id"], "sess-123")
	}
}

func TestAuditEvent_ZeroValue(t *testing.T) {
	var event AuditEvent

	// Zero values should be safe to use
	if event.EventType != "" {
		t.Errorf("Zero AuditEvent.EventType should be empty")
	}
	if !event.Timestamp.IsZero() {
		t.Errorf("Zero AuditEvent.Timestamp should be zero")
	}
	if event.Metadata != nil {
		t.Errorf("Zero AuditEvent.Metadata should be nil")
	}
}

// ============================================================================
// AuditFilter Tests
// ============================================================================

func TestAuditFilter_Fields(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	end := time.Now()

	filter := AuditFilter{
		EventTypes:   []string{"auth.login", "auth.logout"},
		UserID:       "user-123",
		StartTime:    start,
		EndTime:      end,
		ResourceType: "session",
		ResourceID:   "sess-456",
		Outcome:      "success",
		Limit:        100,
		Offset:       10,
	}

	if len(filter.EventTypes) != 2 {
		t.Errorf("EventTypes length = %d, want 2", len(filter.EventTypes))
	}
	if filter.EventTypes[0] != "auth.login" {
		t.Errorf("EventTypes[0] = %q, want %q", filter.EventTypes[0], "auth.login")
	}
	if filter.UserID != "user-123" {
		t.Errorf("UserID = %q, want %q", filter.UserID, "user-123")
	}
	if filter.StartTime != start {
		t.Errorf("StartTime = %v, want %v", filter.StartTime, start)
	}
	if filter.EndTime != end {
		t.Errorf("EndTime = %v, want %v", filter.EndTime, end)
	}
	if filter.ResourceType != "session" {
		t.Errorf("ResourceType = %q, want %q", filter.ResourceType, "session")
	}
	if filter.ResourceID != "sess-456" {
		t.Errorf("ResourceID = %q, want %q", filter.ResourceID, "sess-456")
	}
	if filter.Outcome != "success" {
		t.Errorf("Outcome = %q, want %q", filter.Outcome, "success")
	}
	if filter.Limit != 100 {
		t.Errorf("Limit = %d, want 100", filter.Limit)
	}
	if filter.Offset != 10 {
		t.Errorf("Offset = %d, want 10", filter.Offset)
	}
}

func TestAuditFilter_ZeroValue(t *testing.T) {
	var filter AuditFilter

	// Zero values should represent "no filter" for each field
	if filter.EventTypes != nil {
		t.Errorf("Zero AuditFilter.EventTypes should be nil")
	}
	if filter.UserID != "" {
		t.Errorf("Zero AuditFilter.UserID should be empty")
	}
	if !filter.StartTime.IsZero() {
		t.Errorf("Zero AuditFilter.StartTime should be zero")
	}
	if filter.Limit != 0 {
		t.Errorf("Zero AuditFilter.Limit should be 0")
	}
}

// ============================================================================
// NopAuditLogger Tests
// ============================================================================

func TestNopAuditLogger_Log(t *testing.T) {
	logger := &NopAuditLogger{}
	ctx := context.Background()

	event := AuditEvent{
		EventType: "test.event",
		UserID:    "test-user",
		Action:    "test",
		Outcome:   "success",
	}

	err := logger.Log(ctx, event)
	if err != nil {
		t.Errorf("NopAuditLogger.Log() returned error: %v", err)
	}
}

func TestNopAuditLogger_Log_EmptyEvent(t *testing.T) {
	logger := &NopAuditLogger{}
	ctx := context.Background()

	// Even an empty event should succeed
	err := logger.Log(ctx, AuditEvent{})
	if err != nil {
		t.Errorf("NopAuditLogger.Log() with empty event returned error: %v", err)
	}
}

func TestNopAuditLogger_Query(t *testing.T) {
	logger := &NopAuditLogger{}
	ctx := context.Background()

	filter := AuditFilter{
		EventTypes: []string{"any.event"},
		UserID:     "any-user",
	}

	events, err := logger.Query(ctx, filter)
	if err != nil {
		t.Errorf("NopAuditLogger.Query() returned error: %v", err)
	}
	if events == nil {
		t.Error("NopAuditLogger.Query() returned nil, want empty slice")
	}
	if len(events) != 0 {
		t.Errorf("NopAuditLogger.Query() returned %d events, want 0", len(events))
	}
}

func TestNopAuditLogger_Query_EmptyFilter(t *testing.T) {
	logger := &NopAuditLogger{}
	ctx := context.Background()

	events, err := logger.Query(ctx, AuditFilter{})
	if err != nil {
		t.Errorf("NopAuditLogger.Query() with empty filter returned error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("NopAuditLogger.Query() returned %d events, want 0", len(events))
	}
}

func TestNopAuditLogger_Flush(t *testing.T) {
	logger := &NopAuditLogger{}
	ctx := context.Background()

	err := logger.Flush(ctx)
	if err != nil {
		t.Errorf("NopAuditLogger.Flush() returned error: %v", err)
	}
}

func TestNopAuditLogger_WithCanceledContext(t *testing.T) {
	logger := &NopAuditLogger{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// NopAuditLogger should succeed even with canceled context
	// since it doesn't actually do any work
	err := logger.Log(ctx, AuditEvent{EventType: "test"})
	if err != nil {
		t.Errorf("NopAuditLogger.Log() with canceled context returned error: %v", err)
	}

	events, err := logger.Query(ctx, AuditFilter{})
	if err != nil {
		t.Errorf("NopAuditLogger.Query() with canceled context returned error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("Expected empty events, got %d", len(events))
	}

	err = logger.Flush(ctx)
	if err != nil {
		t.Errorf("NopAuditLogger.Flush() with canceled context returned error: %v", err)
	}
}

func TestNopAuditLogger_InterfaceCompliance(t *testing.T) {
	// Compile-time check is in the source file, but this verifies at runtime
	var _ AuditLogger = (*NopAuditLogger)(nil)
	var _ AuditLogger = &NopAuditLogger{}
}

// ============================================================================
// AuthInfo Tests
// ============================================================================

func TestAuthInfo_Fields(t *testing.T) {
	metadata := map[string]any{
		"department":   "engineering",
		"mfa_verified": true,
	}

	info := &AuthInfo{
		UserID:   "user-123",
		Email:    "user@example.com",
		Roles:    []string{"admin", "analyst"},
		Metadata: metadata,
	}

	if info.UserID != "user-123" {
		t.Errorf("UserID = %q, want %q", info.UserID, "user-123")
	}
	if info.Email != "user@example.com" {
		t.Errorf("Email = %q, want %q", info.Email, "user@example.com")
	}
	if len(info.Roles) != 2 {
		t.Errorf("Roles length = %d, want 2", len(info.Roles))
	}
	if info.Metadata["department"] != "engineering" {
		t.Errorf("Metadata[department] = %v, want %q", info.Metadata["department"], "engineering")
	}
}

func TestAuthInfo_HasRole(t *testing.T) {
	tests := []struct {
		name     string
		roles    []string
		checkFor string
		want     bool
	}{
		{
			name:     "has matching role",
			roles:    []string{"admin", "analyst", "viewer"},
			checkFor: "analyst",
			want:     true,
		},
		{
			name:     "has first role",
			roles:    []string{"admin", "analyst"},
			checkFor: "admin",
			want:     true,
		},
		{
			name:     "has last role",
			roles:    []string{"admin", "analyst", "viewer"},
			checkFor: "viewer",
			want:     true,
		},
		{
			name:     "no matching role",
			roles:    []string{"admin", "analyst"},
			checkFor: "superuser",
			want:     false,
		},
		{
			name:     "empty roles",
			roles:    []string{},
			checkFor: "admin",
			want:     false,
		},
		{
			name:     "nil roles",
			roles:    nil,
			checkFor: "admin",
			want:     false,
		},
		{
			name:     "single role match",
			roles:    []string{"admin"},
			checkFor: "admin",
			want:     true,
		},
		{
			name:     "single role no match",
			roles:    []string{"viewer"},
			checkFor: "admin",
			want:     false,
		},
		{
			name:     "case sensitive",
			roles:    []string{"Admin"},
			checkFor: "admin",
			want:     false,
		},
		{
			name:     "empty string role",
			roles:    []string{"", "admin"},
			checkFor: "",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &AuthInfo{
				UserID: "test-user",
				Roles:  tt.roles,
			}
			got := info.HasRole(tt.checkFor)
			if got != tt.want {
				t.Errorf("HasRole(%q) = %v, want %v", tt.checkFor, got, tt.want)
			}
		})
	}
}

func TestAuthInfo_ZeroValue(t *testing.T) {
	var info AuthInfo

	if info.UserID != "" {
		t.Errorf("Zero AuthInfo.UserID should be empty")
	}
	if info.Email != "" {
		t.Errorf("Zero AuthInfo.Email should be empty")
	}
	if info.Roles != nil {
		t.Errorf("Zero AuthInfo.Roles should be nil")
	}
	if info.HasRole("any") {
		t.Error("Zero AuthInfo.HasRole should return false")
	}
}

// ============================================================================
// AuthzRequest Tests
// ============================================================================

func TestAuthzRequest_Fields(t *testing.T) {
	user := &AuthInfo{UserID: "user-123", Roles: []string{"admin"}}

	req := AuthzRequest{
		User:         user,
		Action:       "delete",
		ResourceType: "evaluation",
		ResourceID:   "eval-456",
	}

	if req.User != user {
		t.Error("AuthzRequest.User should be the assigned user")
	}
	if req.Action != "delete" {
		t.Errorf("Action = %q, want %q", req.Action, "delete")
	}
	if req.ResourceType != "evaluation" {
		t.Errorf("ResourceType = %q, want %q", req.ResourceType, "evaluation")
	}
	if req.ResourceID != "eval-456" {
		t.Errorf("ResourceID = %q, want %q", req.ResourceID, "eval-456")
	}
}

func TestAuthzRequest_ZeroValue(t *testing.T) {
	var req AuthzRequest

	if req.User != nil {
		t.Errorf("Zero AuthzRequest.User should be nil")
	}
	if req.Action != "" {
		t.Errorf("Zero AuthzRequest.Action should be empty")
	}
	if req.ResourceType != "" {
		t.Errorf("Zero AuthzRequest.ResourceType should be empty")
	}
	if req.ResourceID != "" {
		t.Errorf("Zero AuthzRequest.ResourceID should be empty")
	}
}

// ============================================================================
// NopAuthProvider Tests
// ============================================================================

func TestNopAuthProvider_Validate(t *testing.T) {
	provider := &NopAuthProvider{}
	ctx := context.Background()

	tests := []struct {
		name  string
		token string
	}{
		{"valid JWT-like token", "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0"},
		{"API key", "ak_live_1234567890"},
		{"session token", "sess_abc123"},
		{"empty token", ""},
		{"whitespace token", "   "},
		{"special characters", "token-with-special!@#$%^&*()"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := provider.Validate(ctx, tt.token)
			if err != nil {
				t.Errorf("Validate(%q) returned error: %v", tt.token, err)
			}
			if info == nil {
				t.Fatalf("Validate(%q) returned nil AuthInfo", tt.token)
			}
			if info.UserID != "local-user" {
				t.Errorf("UserID = %q, want %q", info.UserID, "local-user")
			}
			if info.Email != "" {
				t.Errorf("Email = %q, want empty", info.Email)
			}
			if len(info.Roles) != 1 || info.Roles[0] != "admin" {
				t.Errorf("Roles = %v, want [admin]", info.Roles)
			}
		})
	}
}

func TestNopAuthProvider_Validate_ReturnedAuthInfoHasAdminRole(t *testing.T) {
	provider := &NopAuthProvider{}
	ctx := context.Background()

	info, _ := provider.Validate(ctx, "any-token")

	if !info.HasRole("admin") {
		t.Error("NopAuthProvider should return AuthInfo with admin role")
	}
}

func TestNopAuthProvider_WithCanceledContext(t *testing.T) {
	provider := &NopAuthProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	info, err := provider.Validate(ctx, "token")
	if err != nil {
		t.Errorf("NopAuthProvider.Validate() with canceled context returned error: %v", err)
	}
	if info == nil {
		t.Error("NopAuthProvider.Validate() with canceled context returned nil")
	}
}

func TestNopAuthProvider_InterfaceCompliance(t *testing.T) {
	var _ AuthProvider = (*NopAuthProvider)(nil)
	var _ AuthProvider = &NopAuthProvider{}
}

// ============================================================================
// NopAuthzProvider Tests
// ============================================================================

func TestNopAuthzProvider_Authorize(t *testing.T) {
	provider := &NopAuthzProvider{}
	ctx := context.Background()

	tests := []struct {
		name string
		req  AuthzRequest
	}{
		{
			name: "delete everything",
			req: AuthzRequest{
				User:         &AuthInfo{UserID: "anyone"},
				Action:       "delete",
				ResourceType: "everything",
				ResourceID:   "*",
			},
		},
		{
			name: "read sensitive data",
			req: AuthzRequest{
				User:         &AuthInfo{UserID: "hacker"},
				Action:       "read",
				ResourceType: "secrets",
				ResourceID:   "database-password",
			},
		},
		{
			name: "nil user",
			req: AuthzRequest{
				User:         nil,
				Action:       "create",
				ResourceType: "admin",
			},
		},
		{
			name: "empty request",
			req:  AuthzRequest{},
		},
		{
			name: "user without roles",
			req: AuthzRequest{
				User:         &AuthInfo{UserID: "noroles", Roles: nil},
				Action:       "admin",
				ResourceType: "system",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := provider.Authorize(ctx, tt.req)
			if err != nil {
				t.Errorf("Authorize() returned error: %v", err)
			}
		})
	}
}

func TestNopAuthzProvider_WithCanceledContext(t *testing.T) {
	provider := &NopAuthzProvider{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := provider.Authorize(ctx, AuthzRequest{})
	if err != nil {
		t.Errorf("NopAuthzProvider.Authorize() with canceled context returned error: %v", err)
	}
}

func TestNopAuthzProvider_InterfaceCompliance(t *testing.T) {
	var _ AuthzProvider = (*NopAuthzProvider)(nil)
	var _ AuthzProvider = &NopAuthzProvider{}
}

// ============================================================================
// Error Variables Tests
// ============================================================================

func TestErrUnauthorized(t *testing.T) {
	if ErrUnauthorized == nil {
		t.Fatal("ErrUnauthorized should not be nil")
	}
	if ErrUnauthorized.Error() != "unauthorized" {
		t.Errorf("ErrUnauthorized.Error() = %q, want %q", ErrUnauthorized.Error(), "unauthorized")
	}
}

func TestErrMessageBlocked(t *testing.T) {
	if ErrMessageBlocked == nil {
		t.Fatal("ErrMessageBlocked should not be nil")
	}
	if ErrMessageBlocked.Error() != "message blocked by filter" {
		t.Errorf("ErrMessageBlocked.Error() = %q, want %q", ErrMessageBlocked.Error(), "message blocked by filter")
	}
}

// ============================================================================
// FilterResult Tests
// ============================================================================

func TestFilterResult_Fields(t *testing.T) {
	detections := []Detection{
		{Type: "ssn", Location: "chars 10-21", Action: "redacted"},
	}

	result := FilterResult{
		Original:    "My SSN is 123-45-6789",
		Filtered:    "My SSN is [REDACTED]",
		WasModified: true,
		WasBlocked:  false,
		BlockReason: "",
		Detections:  detections,
	}

	if result.Original != "My SSN is 123-45-6789" {
		t.Errorf("Original = %q, want %q", result.Original, "My SSN is 123-45-6789")
	}
	if result.Filtered != "My SSN is [REDACTED]" {
		t.Errorf("Filtered = %q, want %q", result.Filtered, "My SSN is [REDACTED]")
	}
	if !result.WasModified {
		t.Error("WasModified should be true")
	}
	if result.WasBlocked {
		t.Error("WasBlocked should be false")
	}
	if len(result.Detections) != 1 {
		t.Errorf("Detections length = %d, want 1", len(result.Detections))
	}
}

func TestFilterResult_Blocked(t *testing.T) {
	result := FilterResult{
		Original:    "harmful content",
		Filtered:    "",
		WasModified: true,
		WasBlocked:  true,
		BlockReason: "policy violation: harmful content detected",
		Detections:  []Detection{{Type: "harmful", Action: "blocked"}},
	}

	if !result.WasBlocked {
		t.Error("WasBlocked should be true")
	}
	if result.BlockReason == "" {
		t.Error("BlockReason should be set when WasBlocked is true")
	}
}

func TestFilterResult_ZeroValue(t *testing.T) {
	var result FilterResult

	if result.Original != "" {
		t.Errorf("Zero FilterResult.Original should be empty")
	}
	if result.Filtered != "" {
		t.Errorf("Zero FilterResult.Filtered should be empty")
	}
	if result.WasModified {
		t.Error("Zero FilterResult.WasModified should be false")
	}
	if result.WasBlocked {
		t.Error("Zero FilterResult.WasBlocked should be false")
	}
	if result.Detections != nil {
		t.Error("Zero FilterResult.Detections should be nil")
	}
}

// ============================================================================
// Detection Tests
// ============================================================================

func TestDetection_Fields(t *testing.T) {
	detection := Detection{
		Type:        "credit_card",
		Location:    "characters 45-64",
		Action:      "redacted",
		Original:    "4111-1111-1111-1111",
		Replacement: "[CARD REDACTED]",
	}

	if detection.Type != "credit_card" {
		t.Errorf("Type = %q, want %q", detection.Type, "credit_card")
	}
	if detection.Location != "characters 45-64" {
		t.Errorf("Location = %q, want %q", detection.Location, "characters 45-64")
	}
	if detection.Action != "redacted" {
		t.Errorf("Action = %q, want %q", detection.Action, "redacted")
	}
	if detection.Original != "4111-1111-1111-1111" {
		t.Errorf("Original = %q, want %q", detection.Original, "4111-1111-1111-1111")
	}
	if detection.Replacement != "[CARD REDACTED]" {
		t.Errorf("Replacement = %q, want %q", detection.Replacement, "[CARD REDACTED]")
	}
}

func TestDetection_ZeroValue(t *testing.T) {
	var detection Detection

	if detection.Type != "" {
		t.Errorf("Zero Detection.Type should be empty")
	}
	if detection.Location != "" {
		t.Errorf("Zero Detection.Location should be empty")
	}
	if detection.Action != "" {
		t.Errorf("Zero Detection.Action should be empty")
	}
	if detection.Original != "" {
		t.Errorf("Zero Detection.Original should be empty")
	}
	if detection.Replacement != "" {
		t.Errorf("Zero Detection.Replacement should be empty")
	}
}

// ============================================================================
// NopMessageFilter Tests
// ============================================================================

func TestNopMessageFilter_FilterInput(t *testing.T) {
	filter := &NopMessageFilter{}
	ctx := context.Background()

	tests := []struct {
		name    string
		message string
	}{
		{"regular message", "Hello, how are you?"},
		{"message with SSN", "My SSN is 123-45-6789"},
		{"message with credit card", "Card: 4111-1111-1111-1111"},
		{"empty message", ""},
		{"whitespace only", "   \t\n  "},
		{"unicode message", "こんにちは世界 🌍"},
		{"very long message", string(make([]byte, 10000))},
		{"message with special chars", "<script>alert('xss')</script>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := filter.FilterInput(ctx, tt.message)
			if err != nil {
				t.Errorf("FilterInput() returned error: %v", err)
			}
			if result == nil {
				t.Fatal("FilterInput() returned nil result")
			}
			if result.Original != tt.message {
				t.Errorf("Original = %q, want %q", result.Original, tt.message)
			}
			if result.Filtered != tt.message {
				t.Errorf("Filtered = %q, want %q", result.Filtered, tt.message)
			}
			if result.WasModified {
				t.Error("WasModified should be false for NopMessageFilter")
			}
			if result.WasBlocked {
				t.Error("WasBlocked should be false for NopMessageFilter")
			}
			if result.Detections != nil {
				t.Error("Detections should be nil for NopMessageFilter")
			}
		})
	}
}

func TestNopMessageFilter_FilterOutput(t *testing.T) {
	filter := &NopMessageFilter{}
	ctx := context.Background()

	tests := []struct {
		name    string
		message string
	}{
		{"regular response", "I'm doing well, thank you!"},
		{"response with API key", "Here's your key: sk-1234567890"},
		{"empty response", ""},
		{"markdown response", "# Title\n\n**Bold** and *italic*"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := filter.FilterOutput(ctx, tt.message)
			if err != nil {
				t.Errorf("FilterOutput() returned error: %v", err)
			}
			if result == nil {
				t.Fatal("FilterOutput() returned nil result")
			}
			if result.Original != tt.message {
				t.Errorf("Original = %q, want %q", result.Original, tt.message)
			}
			if result.Filtered != tt.message {
				t.Errorf("Filtered = %q, want %q", result.Filtered, tt.message)
			}
			if result.WasModified {
				t.Error("WasModified should be false")
			}
			if result.WasBlocked {
				t.Error("WasBlocked should be false")
			}
		})
	}
}

func TestNopMessageFilter_FilterContext(t *testing.T) {
	filter := &NopMessageFilter{}
	ctx := context.Background()

	tests := []struct {
		name       string
		contextMsg string
	}{
		{"system prompt", "You are a helpful assistant."},
		{"RAG context", "Document content: This is retrieved information."},
		{"empty context", ""},
		{"context with sensitive data", "Database password: secret123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := filter.FilterContext(ctx, tt.contextMsg)
			if err != nil {
				t.Errorf("FilterContext() returned error: %v", err)
			}
			if result == nil {
				t.Fatal("FilterContext() returned nil result")
			}
			if result.Original != tt.contextMsg {
				t.Errorf("Original = %q, want %q", result.Original, tt.contextMsg)
			}
			if result.Filtered != tt.contextMsg {
				t.Errorf("Filtered = %q, want %q", result.Filtered, tt.contextMsg)
			}
			if result.WasModified {
				t.Error("WasModified should be false")
			}
			if result.WasBlocked {
				t.Error("WasBlocked should be false")
			}
		})
	}
}

func TestNopMessageFilter_WithCanceledContext(t *testing.T) {
	filter := &NopMessageFilter{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// All methods should succeed even with canceled context
	result, err := filter.FilterInput(ctx, "test")
	if err != nil {
		t.Errorf("FilterInput with canceled context returned error: %v", err)
	}
	if result.Filtered != "test" {
		t.Error("FilterInput should return unchanged message")
	}

	result, err = filter.FilterOutput(ctx, "test")
	if err != nil {
		t.Errorf("FilterOutput with canceled context returned error: %v", err)
	}
	if result.Filtered != "test" {
		t.Error("FilterOutput should return unchanged message")
	}

	result, err = filter.FilterContext(ctx, "test")
	if err != nil {
		t.Errorf("FilterContext with canceled context returned error: %v", err)
	}
	if result.Filtered != "test" {
		t.Error("FilterContext should return unchanged message")
	}
}

func TestNopMessageFilter_InterfaceCompliance(t *testing.T) {
	var _ MessageFilter = (*NopMessageFilter)(nil)
	var _ MessageFilter = &NopMessageFilter{}
}

// ============================================================================
// Concurrent Usage Tests
// ============================================================================

func TestNopImplementations_ConcurrentSafety(t *testing.T) {
	// All nop implementations should be safe for concurrent use
	authProvider := &NopAuthProvider{}
	authzProvider := &NopAuthzProvider{}
	auditLogger := &NopAuditLogger{}
	messageFilter := &NopMessageFilter{}

	ctx := context.Background()
	const goroutines = 100

	done := make(chan bool, goroutines*4)

	// Test concurrent AuthProvider.Validate
	for i := 0; i < goroutines; i++ {
		go func() {
			_, _ = authProvider.Validate(ctx, "token")
			done <- true
		}()
	}

	// Test concurrent AuthzProvider.Authorize
	for i := 0; i < goroutines; i++ {
		go func() {
			_ = authzProvider.Authorize(ctx, AuthzRequest{})
			done <- true
		}()
	}

	// Test concurrent AuditLogger operations
	for i := 0; i < goroutines; i++ {
		go func() {
			_ = auditLogger.Log(ctx, AuditEvent{})
			_, _ = auditLogger.Query(ctx, AuditFilter{})
			_ = auditLogger.Flush(ctx)
			done <- true
		}()
	}

	// Test concurrent MessageFilter operations
	for i := 0; i < goroutines; i++ {
		go func() {
			_, _ = messageFilter.FilterInput(ctx, "test")
			_, _ = messageFilter.FilterOutput(ctx, "test")
			_, _ = messageFilter.FilterContext(ctx, "test")
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < goroutines*4; i++ {
		<-done
	}
}

// ============================================================================
// Mock implementations for testing
// ============================================================================

// mockAuthProvider is a test implementation of AuthProvider
type mockAuthProvider struct {
	userID string
}

func (p *mockAuthProvider) Validate(ctx context.Context, token string) (*AuthInfo, error) {
	return &AuthInfo{UserID: p.userID}, nil
}

// mockAuthzProvider is a test implementation of AuthzProvider
type mockAuthzProvider struct{}

func (p *mockAuthzProvider) Authorize(ctx context.Context, req AuthzRequest) error {
	return nil
}

// mockAuditLogger is a test implementation of AuditLogger
type mockAuditLogger struct {
	events []AuditEvent
}

func (l *mockAuditLogger) Log(ctx context.Context, event AuditEvent) error {
	l.events = append(l.events, event)
	return nil
}

func (l *mockAuditLogger) Query(ctx context.Context, filter AuditFilter) ([]AuditEvent, error) {
	return l.events, nil
}

func (l *mockAuditLogger) Flush(ctx context.Context) error {
	return nil
}

// mockMessageFilter is a test implementation of MessageFilter
type mockMessageFilter struct{}

func (f *mockMessageFilter) FilterInput(ctx context.Context, message string) (*FilterResult, error) {
	return &FilterResult{Original: message, Filtered: message}, nil
}

func (f *mockMessageFilter) FilterOutput(ctx context.Context, message string) (*FilterResult, error) {
	return &FilterResult{Original: message, Filtered: message}, nil
}

func (f *mockMessageFilter) FilterContext(ctx context.Context, contextMsg string) (*FilterResult, error) {
	return &FilterResult{Original: contextMsg, Filtered: contextMsg}, nil
}
