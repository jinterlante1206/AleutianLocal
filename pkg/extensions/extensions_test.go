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

	"github.com/stretchr/testify/assert"
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
	if opts.DataClassifier == nil {
		t.Error("DefaultOptions().DataClassifier should not be nil")
	}
	if opts.RequestAuditor == nil {
		t.Error("DefaultOptions().RequestAuditor should not be nil")
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
	if _, ok := opts.DataClassifier.(*NopDataClassifier); !ok {
		t.Error("DefaultOptions().DataClassifier should be *NopDataClassifier")
	}
	if _, ok := opts.RequestAuditor.(*NopRequestAuditor); !ok {
		t.Error("DefaultOptions().RequestAuditor should be *NopRequestAuditor")
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
	metadata := NewMetadata().
		Set("session_id", "sess-123").
		Set("model", "claude-3")

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
	metadata := NewMetadata().
		Set("department", "engineering").
		Set("mfa_verified", true)

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

func (p *mockAuthProvider) Validate(_ context.Context, _ string) (*AuthInfo, error) {
	return &AuthInfo{UserID: p.userID}, nil
}

// mockAuthzProvider is a test implementation of AuthzProvider
type mockAuthzProvider struct{}

func (p *mockAuthzProvider) Authorize(_ context.Context, _ AuthzRequest) error {
	return nil
}

// mockAuditLogger is a test implementation of AuditLogger
type mockAuditLogger struct {
	events []AuditEvent
}

func (l *mockAuditLogger) Log(_ context.Context, event AuditEvent) error {
	l.events = append(l.events, event)
	return nil
}

func (l *mockAuditLogger) Query(_ context.Context, _ AuditFilter) ([]AuditEvent, error) {
	return l.events, nil
}

func (l *mockAuditLogger) Flush(_ context.Context) error {
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

// ============================================================================
// ServiceOptions With* Tests for New Interfaces
// ============================================================================

func TestServiceOptions_WithClassifier(t *testing.T) {
	original := DefaultOptions()
	customClassifier := &mockDataClassifier{}

	newOpts := original.WithClassifier(customClassifier)

	if newOpts.DataClassifier != customClassifier {
		t.Error("WithClassifier should set the custom DataClassifier")
	}

	// Original should be unchanged
	if _, ok := original.DataClassifier.(*NopDataClassifier); !ok {
		t.Error("Original options should be unchanged after WithClassifier")
	}
}

func TestServiceOptions_WithRequestAuditor(t *testing.T) {
	original := DefaultOptions()
	customAuditor := &mockRequestAuditor{}

	newOpts := original.WithRequestAuditor(customAuditor)

	if newOpts.RequestAuditor != customAuditor {
		t.Error("WithRequestAuditor should set the custom RequestAuditor")
	}

	// Original should be unchanged
	if _, ok := original.RequestAuditor.(*NopRequestAuditor); !ok {
		t.Error("Original options should be unchanged after WithRequestAuditor")
	}
}

func TestServiceOptions_FluentChainingWithAllOptions(t *testing.T) {
	// Test that all With* methods can be chained including new ones
	customAuth := &mockAuthProvider{userID: "chained-user"}
	customAuthz := &mockAuthzProvider{}
	customAudit := &mockAuditLogger{}
	customFilter := &mockMessageFilter{}
	customClassifier := &mockDataClassifier{}
	customAuditor := &mockRequestAuditor{}

	opts := DefaultOptions().
		WithAuth(customAuth).
		WithAuthz(customAuthz).
		WithAudit(customAudit).
		WithFilter(customFilter).
		WithClassifier(customClassifier).
		WithRequestAuditor(customAuditor)

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
	if opts.DataClassifier != customClassifier {
		t.Error("Chained WithClassifier should set DataClassifier")
	}
	if opts.RequestAuditor != customAuditor {
		t.Error("Chained WithRequestAuditor should set RequestAuditor")
	}
}

// ============================================================================
// NopDataClassifier Tests
// ============================================================================

func TestNopDataClassifier_Classify(t *testing.T) {
	classifier := &NopDataClassifier{}
	ctx := context.Background()

	tests := []struct {
		name    string
		content string
	}{
		{"regular text", "Hello, how are you?"},
		{"text with SSN", "My SSN is 123-45-6789"},
		{"text with API key", "API key: sk-1234567890abcdef"},
		{"empty content", ""},
		{"unicode content", "こんにちは世界 🌍"},
		{"very long content", string(make([]byte, 10000))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := classifier.Classify(ctx, tt.content)
			if err != nil {
				t.Errorf("Classify() returned error: %v", err)
			}
			if result == nil {
				t.Fatal("Classify() returned nil result")
			}
			if result.HighestLevel != ClassificationPublic {
				t.Errorf("HighestLevel = %q, want %q", result.HighestLevel, ClassificationPublic)
			}
			if !result.IsClean {
				t.Error("IsClean should be true for NopDataClassifier")
			}
			if result.Findings != nil {
				t.Error("Findings should be nil for NopDataClassifier")
			}
		})
	}
}

func TestNopDataClassifier_ClassifyBatch(t *testing.T) {
	classifier := &NopDataClassifier{}
	ctx := context.Background()

	contents := []string{
		"Normal text",
		"SSN: 123-45-6789",
		"API key: sk-secret",
		"",
	}

	results, err := classifier.ClassifyBatch(ctx, contents)
	if err != nil {
		t.Errorf("ClassifyBatch() returned error: %v", err)
	}
	if len(results) != len(contents) {
		t.Errorf("ClassifyBatch() returned %d results, want %d", len(results), len(contents))
	}

	for i, result := range results {
		if result == nil {
			t.Errorf("Result[%d] is nil", i)
			continue
		}
		if result.HighestLevel != ClassificationPublic {
			t.Errorf("Result[%d].HighestLevel = %q, want %q", i, result.HighestLevel, ClassificationPublic)
		}
		if !result.IsClean {
			t.Errorf("Result[%d].IsClean should be true", i)
		}
	}
}

func TestNopDataClassifier_ClassifyBatch_EmptySlice(t *testing.T) {
	classifier := &NopDataClassifier{}
	ctx := context.Background()

	results, err := classifier.ClassifyBatch(ctx, []string{})
	if err != nil {
		t.Errorf("ClassifyBatch() with empty slice returned error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("ClassifyBatch() returned %d results, want 0", len(results))
	}
}

func TestNopDataClassifier_WithCanceledContext(t *testing.T) {
	classifier := &NopDataClassifier{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := classifier.Classify(ctx, "test")
	if err != nil {
		t.Errorf("Classify with canceled context returned error: %v", err)
	}
	if result == nil || !result.IsClean {
		t.Error("Classify should return clean result even with canceled context")
	}

	results, err := classifier.ClassifyBatch(ctx, []string{"a", "b"})
	if err != nil {
		t.Errorf("ClassifyBatch with canceled context returned error: %v", err)
	}
	if len(results) != 2 {
		t.Error("ClassifyBatch should return results even with canceled context")
	}
}

func TestNopDataClassifier_InterfaceCompliance(t *testing.T) {
	var _ DataClassifier = (*NopDataClassifier)(nil)
	var _ DataClassifier = &NopDataClassifier{}
}

// ============================================================================
// Classification Types Tests
// ============================================================================

func TestDataClassification_Constants(t *testing.T) {
	// Verify constants have expected values
	assert.Equal(t, DataClassification("PUBLIC"), ClassificationPublic)
	assert.Equal(t, DataClassification("CONFIDENTIAL"), ClassificationConfidential)
	assert.Equal(t, DataClassification("PII"), ClassificationPII)
	assert.Equal(t, DataClassification("SECRET"), ClassificationSecret)
}

func TestClassificationResult_ZeroValue(t *testing.T) {
	var result ClassificationResult

	if result.HighestLevel != "" {
		t.Errorf("Zero ClassificationResult.HighestLevel should be empty")
	}
	if result.IsClean {
		t.Error("Zero ClassificationResult.IsClean should be false")
	}
	if result.Findings != nil {
		t.Error("Zero ClassificationResult.Findings should be nil")
	}
}

func TestClassificationFinding_Fields(t *testing.T) {
	finding := ClassificationFinding{
		Classification: ClassificationPII,
		Type:           "email",
		Location:       "line 5, characters 10-30",
		Pattern:        "email_regex",
		Snippet:        "user@exa...",
	}

	if finding.Classification != ClassificationPII {
		t.Errorf("Classification = %q, want %q", finding.Classification, ClassificationPII)
	}
	if finding.Type != "email" {
		t.Errorf("Type = %q, want %q", finding.Type, "email")
	}
	if finding.Location != "line 5, characters 10-30" {
		t.Errorf("Location = %q, want expected value", finding.Location)
	}
}

// ============================================================================
// HTTPHeaders Tests
// ============================================================================

func TestHTTPHeaders_GetSet(t *testing.T) {
	headers := HTTPHeaders{}

	// Set a value
	headers.Set("Content-Type", "application/json")

	// Get the value
	if got := headers.Get("Content-Type"); got != "application/json" {
		t.Errorf("Get(Content-Type) = %q, want %q", got, "application/json")
	}

	// Get non-existent key
	if got := headers.Get("Non-Existent"); got != "" {
		t.Errorf("Get(Non-Existent) = %q, want empty string", got)
	}
}

func TestHTTPHeaders_Literal(t *testing.T) {
	headers := HTTPHeaders{
		"Content-Type":  "application/json",
		"Authorization": "Bearer token",
	}

	if headers.Get("Content-Type") != "application/json" {
		t.Error("Literal initialization failed")
	}
	if headers.Get("Authorization") != "Bearer token" {
		t.Error("Literal initialization failed for Authorization")
	}
}

// ============================================================================
// AuditableRequest Tests
// ============================================================================

func TestAuditableRequest_Fields(t *testing.T) {
	now := time.Now().UTC()
	body := []byte(`{"message": "hello"}`)
	headers := HTTPHeaders{"Content-Type": "application/json"}

	req := &AuditableRequest{
		Method:    "POST",
		Path:      "/v1/chat/direct",
		Headers:   headers,
		Body:      body,
		UserID:    "user-123",
		SessionID: "sess-456",
		RequestID: "req-789",
		Timestamp: now,
	}

	if req.Method != "POST" {
		t.Errorf("Method = %q, want %q", req.Method, "POST")
	}
	if req.Path != "/v1/chat/direct" {
		t.Errorf("Path = %q, want %q", req.Path, "/v1/chat/direct")
	}
	if req.Headers.Get("Content-Type") != "application/json" {
		t.Error("Headers not set correctly")
	}
	if string(req.Body) != `{"message": "hello"}` {
		t.Errorf("Body = %q, want expected value", string(req.Body))
	}
	if req.UserID != "user-123" {
		t.Errorf("UserID = %q, want %q", req.UserID, "user-123")
	}
	if req.SessionID != "sess-456" {
		t.Errorf("SessionID = %q, want %q", req.SessionID, "sess-456")
	}
	if req.RequestID != "req-789" {
		t.Errorf("RequestID = %q, want %q", req.RequestID, "req-789")
	}
	if !req.Timestamp.Equal(now) {
		t.Errorf("Timestamp = %v, want %v", req.Timestamp, now)
	}
}

func TestAuditableRequest_ZeroValue(t *testing.T) {
	var req AuditableRequest

	if req.Method != "" {
		t.Error("Zero value Method should be empty")
	}
	if req.Body != nil {
		t.Error("Zero value Body should be nil")
	}
	if req.Headers != nil {
		t.Error("Zero value Headers should be nil")
	}
}

// ============================================================================
// AuditableResponse Tests
// ============================================================================

func TestAuditableResponse_Fields(t *testing.T) {
	now := time.Now().UTC()
	body := []byte(`{"answer": "world"}`)
	headers := HTTPHeaders{"Content-Type": "application/json"}

	resp := &AuditableResponse{
		StatusCode: 200,
		Headers:    headers,
		Body:       body,
		Timestamp:  now,
	}

	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, 200)
	}
	if resp.Headers.Get("Content-Type") != "application/json" {
		t.Error("Headers not set correctly")
	}
	if string(resp.Body) != `{"answer": "world"}` {
		t.Errorf("Body = %q, want expected value", string(resp.Body))
	}
	if !resp.Timestamp.Equal(now) {
		t.Errorf("Timestamp = %v, want %v", resp.Timestamp, now)
	}
}

func TestAuditableResponse_ZeroValue(t *testing.T) {
	var resp AuditableResponse

	if resp.StatusCode != 0 {
		t.Error("Zero value StatusCode should be 0")
	}
	if resp.Body != nil {
		t.Error("Zero value Body should be nil")
	}
}

// ============================================================================
// NopRequestAuditor Capture Tests
// ============================================================================

func TestNopRequestAuditor_CaptureRequest(t *testing.T) {
	auditor := &NopRequestAuditor{}
	ctx := context.Background()

	req := &AuditableRequest{
		Method:    "POST",
		Path:      "/v1/chat/direct",
		Body:      []byte(`{"test": true}`),
		UserID:    "user-123",
		RequestID: "req-456",
		Timestamp: time.Now().UTC(),
	}

	auditID, err := auditor.CaptureRequest(ctx, req)
	if err != nil {
		t.Errorf("CaptureRequest() returned error: %v", err)
	}
	if auditID != "" {
		t.Errorf("CaptureRequest() auditID = %q, want empty string for Nop", auditID)
	}

	// Should accept nil request too (defensive)
	auditID, err = auditor.CaptureRequest(ctx, nil)
	if err != nil {
		t.Errorf("CaptureRequest(nil) returned error: %v", err)
	}
	if auditID != "" {
		t.Errorf("CaptureRequest(nil) auditID = %q, want empty string", auditID)
	}
}

func TestNopRequestAuditor_CaptureResponse(t *testing.T) {
	auditor := &NopRequestAuditor{}
	ctx := context.Background()

	resp := &AuditableResponse{
		StatusCode: 200,
		Body:       []byte(`{"answer": "hello"}`),
		Timestamp:  time.Now().UTC(),
	}

	err := auditor.CaptureResponse(ctx, "any-audit-id", resp)
	if err != nil {
		t.Errorf("CaptureResponse() returned error: %v", err)
	}

	// Should accept empty auditID
	err = auditor.CaptureResponse(ctx, "", resp)
	if err != nil {
		t.Errorf("CaptureResponse(empty auditID) returned error: %v", err)
	}

	// Should accept nil response too (defensive)
	err = auditor.CaptureResponse(ctx, "", nil)
	if err != nil {
		t.Errorf("CaptureResponse(nil) returned error: %v", err)
	}
}

func TestNopRequestAuditor_CaptureRoundTrip(t *testing.T) {
	auditor := &NopRequestAuditor{}
	ctx := context.Background()

	// Simulate a full request/response capture flow
	req := &AuditableRequest{
		Method:    "POST",
		Path:      "/v1/chat/direct",
		Body:      []byte(`{"question": "what is 2+2?"}`),
		UserID:    "user-123",
		RequestID: "req-789",
		Timestamp: time.Now().UTC(),
	}

	auditID, err := auditor.CaptureRequest(ctx, req)
	if err != nil {
		t.Fatalf("CaptureRequest() failed: %v", err)
	}

	// Simulate processing...

	resp := &AuditableResponse{
		StatusCode: 200,
		Body:       []byte(`{"answer": "4"}`),
		Timestamp:  time.Now().UTC(),
	}

	err = auditor.CaptureResponse(ctx, auditID, resp)
	if err != nil {
		t.Fatalf("CaptureResponse() failed: %v", err)
	}
}

func TestNopRequestAuditor_CaptureWithCanceledContext(t *testing.T) {
	auditor := &NopRequestAuditor{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	req := &AuditableRequest{
		Method: "POST",
		Path:   "/test",
	}

	// Should still succeed (Nop ignores context)
	auditID, err := auditor.CaptureRequest(ctx, req)
	if err != nil {
		t.Errorf("CaptureRequest() with canceled ctx returned error: %v", err)
	}
	if auditID != "" {
		t.Error("CaptureRequest() should return empty auditID")
	}

	resp := &AuditableResponse{StatusCode: 200}
	err = auditor.CaptureResponse(ctx, "", resp)
	if err != nil {
		t.Errorf("CaptureResponse() with canceled ctx returned error: %v", err)
	}
}

// ============================================================================
// NopRequestAuditor Hash Chain Tests
// ============================================================================

func TestNopRequestAuditor_RecordEntry(t *testing.T) {
	auditor := &NopRequestAuditor{}
	ctx := context.Background()

	entry := HashChainEntry{
		SessionID:   "sess-123",
		SequenceNum: 1,
		ContentHash: "abc123",
		ChainHash:   "def456",
		ContentType: "conversation_turn",
		Timestamp:   time.Now().UTC(),
	}

	err := auditor.RecordEntry(ctx, entry)
	if err != nil {
		t.Errorf("RecordEntry() returned error: %v", err)
	}

	// Should accept empty entry too
	err = auditor.RecordEntry(ctx, HashChainEntry{})
	if err != nil {
		t.Errorf("RecordEntry() with empty entry returned error: %v", err)
	}
}

func TestNopRequestAuditor_GetLastEntry(t *testing.T) {
	auditor := &NopRequestAuditor{}
	ctx := context.Background()

	entry, err := auditor.GetLastEntry(ctx, "any-session")
	if err != nil {
		t.Errorf("GetLastEntry() returned error: %v", err)
	}
	if entry != nil {
		t.Error("GetLastEntry() should return nil for NopRequestAuditor")
	}
}

func TestNopRequestAuditor_VerifyChain(t *testing.T) {
	auditor := &NopRequestAuditor{}
	ctx := context.Background()

	result, err := auditor.VerifyChain(ctx, "any-session")
	if err != nil {
		t.Errorf("VerifyChain() returned error: %v", err)
	}
	if result == nil {
		t.Fatal("VerifyChain() returned nil result")
	}
	if !result.IsValid {
		t.Error("IsValid should be true for NopRequestAuditor")
	}
	if result.TotalEntries != 0 {
		t.Errorf("TotalEntries = %d, want 0", result.TotalEntries)
	}
	if result.Message == "" {
		t.Error("Message should be set")
	}
}

func TestNopRequestAuditor_GetChainLength(t *testing.T) {
	auditor := &NopRequestAuditor{}
	ctx := context.Background()

	length, err := auditor.GetChainLength(ctx, "any-session")
	if err != nil {
		t.Errorf("GetChainLength() returned error: %v", err)
	}
	if length != 0 {
		t.Errorf("GetChainLength() = %d, want 0", length)
	}
}

func TestNopRequestAuditor_WithCanceledContext(t *testing.T) {
	auditor := &NopRequestAuditor{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// All methods should succeed even with canceled context
	err := auditor.RecordEntry(ctx, HashChainEntry{})
	if err != nil {
		t.Errorf("RecordEntry with canceled context returned error: %v", err)
	}

	entry, err := auditor.GetLastEntry(ctx, "session")
	if err != nil {
		t.Errorf("GetLastEntry with canceled context returned error: %v", err)
	}
	if entry != nil {
		t.Error("GetLastEntry should return nil")
	}

	result, err := auditor.VerifyChain(ctx, "session")
	if err != nil {
		t.Errorf("VerifyChain with canceled context returned error: %v", err)
	}
	if result == nil || !result.IsValid {
		t.Error("VerifyChain should return valid result")
	}

	length, err := auditor.GetChainLength(ctx, "session")
	if err != nil {
		t.Errorf("GetChainLength with canceled context returned error: %v", err)
	}
	if length != 0 {
		t.Error("GetChainLength should return 0")
	}
}

func TestNopRequestAuditor_InterfaceCompliance(t *testing.T) {
	var _ RequestAuditor = (*NopRequestAuditor)(nil)
	var _ RequestAuditor = &NopRequestAuditor{}
}

// ============================================================================
// HashChainEntry Tests
// ============================================================================

func TestHashChainEntry_Fields(t *testing.T) {
	now := time.Now().UTC()
	metadata := NewMetadata().
		Set("user_id", "user-123").
		Set("request_id", "req-456")

	entry := HashChainEntry{
		SessionID:    "sess-123",
		SequenceNum:  5,
		ContentHash:  "abc123",
		PreviousHash: "def456",
		ChainHash:    "ghi789",
		Timestamp:    now,
		ContentType:  "conversation_turn",
		Metadata:     metadata,
	}

	if entry.SessionID != "sess-123" {
		t.Errorf("SessionID = %q, want %q", entry.SessionID, "sess-123")
	}
	if entry.SequenceNum != 5 {
		t.Errorf("SequenceNum = %d, want 5", entry.SequenceNum)
	}
	if entry.ContentHash != "abc123" {
		t.Errorf("ContentHash = %q, want %q", entry.ContentHash, "abc123")
	}
	if entry.PreviousHash != "def456" {
		t.Errorf("PreviousHash = %q, want %q", entry.PreviousHash, "def456")
	}
	if entry.ChainHash != "ghi789" {
		t.Errorf("ChainHash = %q, want %q", entry.ChainHash, "ghi789")
	}
	if entry.Timestamp != now {
		t.Errorf("Timestamp = %v, want %v", entry.Timestamp, now)
	}
	if entry.ContentType != "conversation_turn" {
		t.Errorf("ContentType = %q, want %q", entry.ContentType, "conversation_turn")
	}
	if entry.Metadata["user_id"] != "user-123" {
		t.Errorf("Metadata[user_id] = %v, want %q", entry.Metadata["user_id"], "user-123")
	}
}

func TestHashChainEntry_ZeroValue(t *testing.T) {
	var entry HashChainEntry

	if entry.SessionID != "" {
		t.Errorf("Zero HashChainEntry.SessionID should be empty")
	}
	if entry.SequenceNum != 0 {
		t.Errorf("Zero HashChainEntry.SequenceNum should be 0")
	}
	if !entry.Timestamp.IsZero() {
		t.Errorf("Zero HashChainEntry.Timestamp should be zero")
	}
	if entry.Metadata != nil {
		t.Errorf("Zero HashChainEntry.Metadata should be nil")
	}
}

func TestChainVerificationResult_ZeroValue(t *testing.T) {
	var result ChainVerificationResult

	if result.IsValid {
		t.Error("Zero ChainVerificationResult.IsValid should be false")
	}
	if result.TotalEntries != 0 {
		t.Errorf("Zero ChainVerificationResult.TotalEntries should be 0")
	}
	if result.BreakPoint != 0 {
		t.Errorf("Zero ChainVerificationResult.BreakPoint should be 0")
	}
}

// ============================================================================
// Concurrent Usage Tests for New Interfaces
// ============================================================================

func TestNopImplementations_ConcurrentSafety_NewInterfaces(t *testing.T) {
	classifier := &NopDataClassifier{}
	auditor := &NopRequestAuditor{}

	ctx := context.Background()
	const goroutines = 100

	done := make(chan bool, goroutines*2)

	// Test concurrent DataClassifier operations
	for i := 0; i < goroutines; i++ {
		go func() {
			_, _ = classifier.Classify(ctx, "test content")
			_, _ = classifier.ClassifyBatch(ctx, []string{"a", "b", "c"})
			done <- true
		}()
	}

	// Test concurrent RequestAuditor operations
	for i := 0; i < goroutines; i++ {
		go func() {
			_ = auditor.RecordEntry(ctx, HashChainEntry{})
			_, _ = auditor.GetLastEntry(ctx, "session")
			_, _ = auditor.VerifyChain(ctx, "session")
			_, _ = auditor.GetChainLength(ctx, "session")
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < goroutines*2; i++ {
		<-done
	}
}

// ============================================================================
// Mock implementations for new interfaces
// ============================================================================

// mockDataClassifier is a test implementation of DataClassifier
type mockDataClassifier struct{}

func (c *mockDataClassifier) Classify(_ context.Context, _ string) (*ClassificationResult, error) {
	return &ClassificationResult{
		HighestLevel: ClassificationPublic,
		IsClean:      true,
	}, nil
}

func (c *mockDataClassifier) ClassifyBatch(_ context.Context, contents []string) ([]*ClassificationResult, error) {
	results := make([]*ClassificationResult, len(contents))
	for i := range contents {
		results[i] = &ClassificationResult{
			HighestLevel: ClassificationPublic,
			IsClean:      true,
		}
	}
	return results, nil
}

// mockRequestAuditor is a test implementation of RequestAuditor
type mockRequestAuditor struct{}

func (a *mockRequestAuditor) CaptureRequest(_ context.Context, _ *AuditableRequest) (string, error) {
	return "mock-audit-id", nil
}

func (a *mockRequestAuditor) CaptureResponse(_ context.Context, _ string, _ *AuditableResponse) error {
	return nil
}

func (a *mockRequestAuditor) RecordEntry(_ context.Context, _ HashChainEntry) error {
	return nil
}

func (a *mockRequestAuditor) GetLastEntry(_ context.Context, _ string) (*HashChainEntry, error) {
	return nil, nil
}

func (a *mockRequestAuditor) VerifyChain(_ context.Context, _ string) (*ChainVerificationResult, error) {
	return &ChainVerificationResult{IsValid: true}, nil
}

func (a *mockRequestAuditor) GetChainLength(_ context.Context, _ string) (int, error) {
	return 0, nil
}

// ============================================================================
// Metadata Type Tests
// ============================================================================

func TestNewMetadata(t *testing.T) {
	meta := NewMetadata()
	if meta == nil {
		t.Fatal("NewMetadata() returned nil")
	}
	if len(meta) != 0 {
		t.Errorf("NewMetadata() returned non-empty map, len = %d", len(meta))
	}
}

func TestMetadata_Set(t *testing.T) {
	meta := NewMetadata().
		Set("key1", "value1").
		Set("key2", 42).
		Set("key3", true)

	if meta["key1"] != "value1" {
		t.Errorf("key1 = %v, want %q", meta["key1"], "value1")
	}
	if meta["key2"] != 42 {
		t.Errorf("key2 = %v, want %d", meta["key2"], 42)
	}
	if meta["key3"] != true {
		t.Errorf("key3 = %v, want %v", meta["key3"], true)
	}
}

func TestMetadata_Get(t *testing.T) {
	meta := NewMetadata().Set("key", "value")

	value, ok := meta.Get("key")
	if !ok {
		t.Error("Get(key) should return true for existing key")
	}
	if value != "value" {
		t.Errorf("Get(key) = %v, want %q", value, "value")
	}

	_, ok = meta.Get("nonexistent")
	if ok {
		t.Error("Get(nonexistent) should return false")
	}
}

func TestMetadata_GetString(t *testing.T) {
	meta := NewMetadata().
		Set("string", "hello").
		Set("int", 42)

	str, ok := meta.GetString("string")
	if !ok {
		t.Error("GetString(string) should return true")
	}
	if str != "hello" {
		t.Errorf("GetString(string) = %q, want %q", str, "hello")
	}

	_, ok = meta.GetString("int")
	if ok {
		t.Error("GetString(int) should return false for non-string")
	}

	_, ok = meta.GetString("nonexistent")
	if ok {
		t.Error("GetString(nonexistent) should return false")
	}
}

func TestMetadata_GetInt(t *testing.T) {
	meta := NewMetadata().
		Set("int", 42).
		Set("string", "hello")

	i, ok := meta.GetInt("int")
	if !ok {
		t.Error("GetInt(int) should return true")
	}
	if i != 42 {
		t.Errorf("GetInt(int) = %d, want %d", i, 42)
	}

	_, ok = meta.GetInt("string")
	if ok {
		t.Error("GetInt(string) should return false for non-int")
	}
}

func TestMetadata_GetInt64(t *testing.T) {
	meta := NewMetadata().Set("int64", int64(9223372036854775807))

	i, ok := meta.GetInt64("int64")
	if !ok {
		t.Error("GetInt64(int64) should return true")
	}
	if i != 9223372036854775807 {
		t.Errorf("GetInt64(int64) = %d, want %d", i, int64(9223372036854775807))
	}
}

func TestMetadata_GetFloat64(t *testing.T) {
	meta := NewMetadata().Set("float", 3.14159)

	f, ok := meta.GetFloat64("float")
	if !ok {
		t.Error("GetFloat64(float) should return true")
	}
	if f != 3.14159 {
		t.Errorf("GetFloat64(float) = %f, want %f", f, 3.14159)
	}
}

func TestMetadata_GetBool(t *testing.T) {
	meta := NewMetadata().
		Set("true", true).
		Set("false", false)

	b, ok := meta.GetBool("true")
	if !ok {
		t.Error("GetBool(true) should return true")
	}
	if !b {
		t.Error("GetBool(true) = false, want true")
	}

	b, ok = meta.GetBool("false")
	if !ok {
		t.Error("GetBool(false) should return true")
	}
	if b {
		t.Error("GetBool(false) = true, want false")
	}
}

func TestMetadata_GetTime(t *testing.T) {
	now := time.Now().UTC()
	meta := NewMetadata().Set("time", now)

	tm, ok := meta.GetTime("time")
	if !ok {
		t.Error("GetTime(time) should return true")
	}
	if !tm.Equal(now) {
		t.Errorf("GetTime(time) = %v, want %v", tm, now)
	}
}

func TestMetadata_Has(t *testing.T) {
	meta := NewMetadata().Set("key", "value")

	if !meta.Has("key") {
		t.Error("Has(key) should return true")
	}
	if meta.Has("nonexistent") {
		t.Error("Has(nonexistent) should return false")
	}
}

func TestMetadata_Delete(t *testing.T) {
	meta := NewMetadata().Set("key", "value")

	// Delete returns the same instance for chaining
	result := meta.Delete("key")
	// Verify chaining works by using the result
	result.Set("new_key", "new_value")
	if !meta.Has("new_key") {
		t.Error("Delete should return same instance for chaining")
	}
	if meta.Has("key") {
		t.Error("Delete should remove the key")
	}

	// Deleting nonexistent key should not panic
	meta.Delete("nonexistent")
}

func TestMetadata_Clone(t *testing.T) {
	original := NewMetadata().
		Set("key1", "value1").
		Set("key2", 42)

	clone := original.Clone()

	// Clone should have same values
	if clone["key1"] != "value1" {
		t.Error("Clone should copy values")
	}

	// Modifying clone should not affect original
	clone.Set("key1", "modified")
	if original["key1"] != "value1" {
		t.Error("Modifying clone should not affect original")
	}

	// Modifying original should not affect clone
	original.Set("key3", "new")
	if clone.Has("key3") {
		t.Error("Modifying original should not affect clone")
	}
}

func TestMetadata_Merge(t *testing.T) {
	meta1 := NewMetadata().Set("key1", "value1")
	meta2 := NewMetadata().Set("key2", "value2")

	// Merge returns the same instance for chaining
	result := meta1.Merge(meta2)
	// Verify chaining works by using the result
	result.Set("key3", "value3")
	if !meta1.Has("key3") {
		t.Error("Merge should return same instance for chaining")
	}

	if !meta1.Has("key1") || !meta1.Has("key2") {
		t.Error("Merge should add keys from other")
	}

	// Merge with nil should not panic
	meta1.Merge(nil)
}

func TestMetadata_Merge_Overwrite(t *testing.T) {
	meta1 := NewMetadata().Set("key", "original")
	meta2 := NewMetadata().Set("key", "overwritten")

	meta1.Merge(meta2)
	if meta1["key"] != "overwritten" {
		t.Error("Merge should overwrite existing keys")
	}
}

func TestMetadata_Keys(t *testing.T) {
	meta := NewMetadata().
		Set("a", 1).
		Set("b", 2).
		Set("c", 3)

	keys := meta.Keys()
	if len(keys) != 3 {
		t.Errorf("Keys() returned %d keys, want 3", len(keys))
	}

	// Check all keys are present (order not guaranteed)
	keyMap := make(map[string]bool)
	for _, k := range keys {
		keyMap[k] = true
	}
	if !keyMap["a"] || !keyMap["b"] || !keyMap["c"] {
		t.Error("Keys() should return all keys")
	}
}

func TestMetadata_Len(t *testing.T) {
	meta := NewMetadata()
	if meta.Len() != 0 {
		t.Errorf("Empty Metadata.Len() = %d, want 0", meta.Len())
	}

	meta.Set("key", "value")
	if meta.Len() != 1 {
		t.Errorf("Metadata.Len() = %d, want 1", meta.Len())
	}
}

func TestMetadata_ZeroValue(t *testing.T) {
	var meta Metadata

	// Zero value should be nil
	assert.Nil(t, meta, "Zero Metadata should be nil")

	// Methods should not panic on nil (except Set which modifies)
	assert.False(t, meta.Has("key"), "Nil Metadata.Has should return false")
	_, ok := meta.Get("key")
	assert.False(t, ok, "Nil Metadata.Get should return false")
	assert.Equal(t, 0, meta.Len(), "Nil Metadata.Len should return 0")
}
