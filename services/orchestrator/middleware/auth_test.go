// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jinterlante1206/AleutianLocal/pkg/extensions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Setup
// =============================================================================

func init() {
	gin.SetMode(gin.TestMode)
}

// mockAuthProvider is a configurable mock for testing.
type mockAuthProvider struct {
	authInfo *extensions.AuthInfo
	err      error
}

func (m *mockAuthProvider) Validate(_ context.Context, _ string) (*extensions.AuthInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.authInfo, nil
}

// =============================================================================
// extractBearerToken Tests
// =============================================================================

func TestExtractBearerToken_ValidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("Authorization", "Bearer abc123")

	token := extractBearerToken(c)

	assert.Equal(t, "abc123", token)
}

func TestExtractBearerToken_MissingHeader(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)

	token := extractBearerToken(c)

	assert.Empty(t, token)
}

func TestExtractBearerToken_InvalidFormat(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"no bearer prefix", "abc123"},
		{"basic auth", "Basic abc123"},
		{"empty bearer", "Bearer "},
		{"only bearer", "Bearer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/", nil)
			c.Request.Header.Set("Authorization", tt.header)

			token := extractBearerToken(c)

			assert.Empty(t, token)
		})
	}
}

func TestExtractBearerToken_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"lowercase", "bearer abc123"},
		{"uppercase", "BEARER abc123"},
		{"mixed case", "BeArEr abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/", nil)
			c.Request.Header.Set("Authorization", tt.header)

			token := extractBearerToken(c)

			assert.Equal(t, "abc123", token)
		})
	}
}

// =============================================================================
// AuthMiddleware Tests
// =============================================================================

func TestAuthMiddleware_Success(t *testing.T) {
	expectedAuthInfo := &extensions.AuthInfo{
		UserID: "user-123",
		Email:  "user@example.com",
		Roles:  []string{"admin"},
	}

	provider := &mockAuthProvider{authInfo: expectedAuthInfo}

	router := gin.New()
	router.Use(AuthMiddleware(provider))
	router.GET("/test", func(c *gin.Context) {
		authInfo := GetAuthInfo(c)
		require.NotNil(t, authInfo)
		c.JSON(http.StatusOK, gin.H{"user_id": authInfo.UserID})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_Unauthorized(t *testing.T) {
	provider := &mockAuthProvider{
		err: extensions.ErrUnauthorized,
	}

	router := gin.New()
	router.Use(AuthMiddleware(provider))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_ProviderError(t *testing.T) {
	provider := &mockAuthProvider{
		err: errors.New("network error"),
	}

	router := gin.New()
	router.Use(AuthMiddleware(provider))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_NopProvider(t *testing.T) {
	// NopAuthProvider should always succeed
	provider := &extensions.NopAuthProvider{}

	router := gin.New()
	router.Use(AuthMiddleware(provider))
	router.GET("/test", func(c *gin.Context) {
		authInfo := GetAuthInfo(c)
		require.NotNil(t, authInfo)
		assert.Equal(t, "local-user", authInfo.UserID)
		assert.Contains(t, authInfo.Roles, "admin")
		c.JSON(http.StatusOK, gin.H{"user_id": authInfo.UserID})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	// No Authorization header - NopAuthProvider doesn't need it
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// =============================================================================
// Context Helper Tests
// =============================================================================

func TestSetAndGetAuthInfo(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	expected := &extensions.AuthInfo{
		UserID: "test-user",
		Email:  "test@example.com",
		Roles:  []string{"viewer"},
	}

	SetAuthInfo(c, expected)
	actual := GetAuthInfo(c)

	require.NotNil(t, actual)
	assert.Equal(t, expected.UserID, actual.UserID)
	assert.Equal(t, expected.Email, actual.Email)
	assert.Equal(t, expected.Roles, actual.Roles)
}

func TestGetAuthInfo_NotSet(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	authInfo := GetAuthInfo(c)

	assert.Nil(t, authInfo)
}

func TestGetAuthInfo_WrongType(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(authInfoKey, "not an AuthInfo")

	authInfo := GetAuthInfo(c)

	assert.Nil(t, authInfo)
}
