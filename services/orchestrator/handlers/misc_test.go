// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// Tests for miscellaneous handlers

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// =============================================================================
// HealthCheck Tests
// =============================================================================

func TestHealthCheck_ReturnsOK(t *testing.T) {
	router := gin.New()
	router.GET("/health", HealthCheck)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "ok", response["status"])
}

func TestHealthCheck_JSONContentType(t *testing.T) {
	router := gin.New()
	router.GET("/health", HealthCheck)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	router.ServeHTTP(w, req)

	contentType := w.Header().Get("Content-Type")
	assert.Contains(t, contentType, "application/json")
}

func TestHealthCheck_ResponseBody(t *testing.T) {
	router := gin.New()
	router.GET("/health", HealthCheck)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	router.ServeHTTP(w, req)

	body := w.Body.String()
	assert.Contains(t, body, "status")
	assert.Contains(t, body, "ok")
}
