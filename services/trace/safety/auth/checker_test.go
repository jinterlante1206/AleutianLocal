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
	"testing"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/ast"
	"github.com/AleutianAI/AleutianFOSS/services/trace/graph"
	"github.com/AleutianAI/AleutianFOSS/services/trace/index"
	"github.com/AleutianAI/AleutianFOSS/services/trace/safety"
)

func createTestGraph() (*graph.Graph, *index.SymbolIndex) {
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	handler := &ast.Symbol{
		ID:        "handlers.Router",
		Name:      "Router",
		Kind:      ast.SymbolKindFunction,
		Language:  "go",
		FilePath:  "handlers/router.go",
		Package:   "handlers",
		StartLine: 10,
	}

	g.AddNode(handler)
	idx.Add(handler)
	g.Freeze()

	return g, idx
}

// --- Pattern Tests ---

func TestIsAdminPath(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"/admin", true},
		{"/api/admin/users", true},
		{"/management", true},
		{"/internal/config", true},
		{"/debug/pprof", true},
		{"/metrics", true},
		{"/api/users", false},
		{"/products", false},
		{"/", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := IsAdminPath(tt.path)
			if result != tt.expected {
				t.Errorf("IsAdminPath(%q) = %v, expected %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestIsSensitivePath(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"/users", true},
		{"/api/user/profile", true},
		{"/account", true},
		{"/password/reset", true},
		{"/auth/login", true},
		{"/payment/checkout", true},
		{"/products", false},
		{"/static/js", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := IsSensitivePath(tt.path)
			if result != tt.expected {
				t.Errorf("IsSensitivePath(%q) = %v, expected %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestIsMutationMethod(t *testing.T) {
	tests := []struct {
		method   string
		expected bool
	}{
		{"POST", true},
		{"PUT", true},
		{"PATCH", true},
		{"DELETE", true},
		{"GET", false},
		{"HEAD", false},
		{"OPTIONS", false},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			result := IsMutationMethod(tt.method)
			if result != tt.expected {
				t.Errorf("IsMutationMethod(%q) = %v, expected %v", tt.method, result, tt.expected)
			}
		})
	}
}

func TestRouteDetector_DetectFramework(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		framework string
		expected  bool
	}{
		{
			name:      "Gin framework",
			content:   `import "github.com/gin-gonic/gin"`,
			framework: "gin",
			expected:  true,
		},
		{
			name:      "Echo framework",
			content:   `import "github.com/labstack/echo/v4"`,
			framework: "echo",
			expected:  true,
		},
		{
			name:      "FastAPI framework",
			content:   `from fastapi import FastAPI`,
			framework: "fastapi",
			expected:  true,
		},
		{
			name:      "Flask framework",
			content:   `from flask import Flask`,
			framework: "flask",
			expected:  true,
		},
		{
			name:      "Express framework",
			content:   `const express = require('express')`,
			framework: "express",
			expected:  true,
		},
		{
			name:      "NestJS framework",
			content:   `import { Controller } from '@nestjs/common'`,
			framework: "nestjs",
			expected:  true,
		},
		{
			name:      "No framework",
			content:   `package main`,
			framework: "gin",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := RoutePatterns[tt.framework]
			result := detector.DetectFramework(tt.content)
			if result != tt.expected {
				t.Errorf("DetectFramework() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestRouteDetector_FindRoutes_Gin(t *testing.T) {
	content := `
package main

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()
	r.GET("/users", getUsers)
	r.POST("/users", createUser)
	r.GET("/admin/dashboard", adminDashboard)
	r.DELETE("/users/:id", deleteUser)
}
`
	detector := RoutePatterns["gin"]
	routes := detector.FindRoutes(content)

	if len(routes) != 4 {
		t.Errorf("Expected 4 routes, got %d", len(routes))
	}

	// Check first route
	if routes[0].Path != "/users" {
		t.Errorf("Expected path /users, got %s", routes[0].Path)
	}
	if routes[0].Method != "GET" {
		t.Errorf("Expected method GET, got %s", routes[0].Method)
	}
}

func TestRouteDetector_FindRoutes_FastAPI(t *testing.T) {
	content := `
from fastapi import FastAPI

app = FastAPI()

@app.get("/users")
def get_users():
    return []

@app.post("/users")
def create_user(user: User):
    return user

@app.delete("/admin/users/{id}")
def delete_user(id: int):
    pass
`
	detector := RoutePatterns["fastapi"]
	routes := detector.FindRoutes(content)

	if len(routes) != 3 {
		t.Errorf("Expected 3 routes, got %d", len(routes))
	}

	// Check routes
	expectedPaths := []string{"/users", "/users", "/admin/users/{id}"}
	for i, route := range routes {
		if route.Path != expectedPaths[i] {
			t.Errorf("Route %d: expected path %s, got %s", i, expectedPaths[i], route.Path)
		}
	}
}

// --- Checker Tests ---

func TestAuthChecker_CheckAuthEnforcement_EmptyScope(t *testing.T) {
	g, idx := createTestGraph()
	checker := NewAuthChecker(g, idx)

	ctx := context.Background()
	_, err := checker.CheckAuthEnforcement(ctx, "")

	if err != safety.ErrInvalidInput {
		t.Errorf("Expected ErrInvalidInput, got %v", err)
	}
}

func TestAuthChecker_CheckAuthEnforcement_ContextCanceled(t *testing.T) {
	g, idx := createTestGraph()
	checker := NewAuthChecker(g, idx)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := checker.CheckAuthEnforcement(ctx, "handlers")

	if err != safety.ErrContextCanceled {
		t.Errorf("Expected ErrContextCanceled, got %v", err)
	}
}

func TestAuthChecker_CheckAuthEnforcement_DetectsMissingAuth(t *testing.T) {
	g, idx := createTestGraph()
	checker := NewAuthChecker(g, idx)

	// Set file content with unprotected admin endpoint
	checker.SetFileContent("handlers/router.go", `
package handlers

import "github.com/gin-gonic/gin"

func SetupRoutes(r *gin.Engine) {
	// No auth middleware!
	r.GET("/admin/users", getAdminUsers)
	r.POST("/admin/settings", updateSettings)
}
`)

	ctx := context.Background()
	result, err := checker.CheckAuthEnforcement(ctx, "handlers")

	if err != nil {
		t.Fatalf("CheckAuthEnforcement failed: %v", err)
	}

	if result.Framework != "gin" {
		t.Errorf("Expected framework gin, got %s", result.Framework)
	}

	if result.MissingAuth == 0 {
		t.Error("Expected to detect missing auth on admin endpoints")
	}

	// Check that admin endpoints are flagged as critical
	for _, ep := range result.Endpoints {
		if ep.IsAdminEndpoint && !ep.HasAuthentication {
			if ep.Risk != safety.SeverityCritical {
				t.Errorf("Expected CRITICAL risk for unprotected admin endpoint, got %s", ep.Risk)
			}
		}
	}
}

func TestAuthChecker_CheckAuthEnforcement_DetectsAuthMiddleware(t *testing.T) {
	g, idx := createTestGraph()
	checker := NewAuthChecker(g, idx)

	// Set file content with auth middleware
	checker.SetFileContent("handlers/router.go", `
package handlers

import "github.com/gin-gonic/gin"

func SetupRoutes(r *gin.Engine) {
	// Global auth middleware
	r.Use(authMiddleware())

	r.GET("/users", getUsers)
	r.POST("/users", createUser)
}
`)

	ctx := context.Background()
	result, err := checker.CheckAuthEnforcement(ctx, "handlers")

	if err != nil {
		t.Fatalf("CheckAuthEnforcement failed: %v", err)
	}

	// Should detect that endpoints have auth
	for _, ep := range result.Endpoints {
		if !ep.HasAuthentication {
			t.Errorf("Expected endpoint %s to have authentication", ep.Path)
		}
	}

	if result.MissingAuth > 0 {
		t.Errorf("Expected no missing auth, got %d", result.MissingAuth)
	}
}

func TestAuthChecker_CheckAuthEnforcement_FastAPI(t *testing.T) {
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	sym := &ast.Symbol{
		ID:        "app.main",
		Name:      "main",
		Kind:      ast.SymbolKindFunction,
		Language:  "python",
		FilePath:  "app/main.py",
		Package:   "app",
		StartLine: 1,
	}
	g.AddNode(sym)
	idx.Add(sym)
	g.Freeze()

	checker := NewAuthChecker(g, idx)

	// Set file content with FastAPI
	checker.SetFileContent("app/main.py", `
from fastapi import FastAPI, Depends
from app.auth import get_current_user

app = FastAPI()

@app.get("/users")
def get_users(user = Depends(get_current_user)):
    return []

@app.post("/admin/settings")
def update_settings():
    # No auth!
    pass
`)

	ctx := context.Background()
	result, err := checker.CheckAuthEnforcement(ctx, "app",
		safety.WithFrameworkHint("fastapi"))

	if err != nil {
		t.Fatalf("CheckAuthEnforcement failed: %v", err)
	}

	if result.Framework != "fastapi" {
		t.Errorf("Expected framework fastapi, got %s", result.Framework)
	}

	// Should detect the unprotected admin endpoint
	foundUnprotectedAdmin := false
	for _, ep := range result.Endpoints {
		if ep.Path == "/admin/settings" && !ep.HasAuthentication {
			foundUnprotectedAdmin = true
			break
		}
	}

	if !foundUnprotectedAdmin {
		t.Error("Expected to detect unprotected admin endpoint")
	}
}

func TestAuthChecker_CheckAuthEnforcement_AuthorizationCheck(t *testing.T) {
	g, idx := createTestGraph()
	checker := NewAuthChecker(g, idx)

	// Set file content with auth but no authz
	checker.SetFileContent("handlers/router.go", `
package handlers

import "github.com/gin-gonic/gin"

func SetupRoutes(r *gin.Engine) {
	r.Use(authMiddleware())

	// Auth but no authorization on admin endpoint
	r.DELETE("/admin/users/:id", deleteUser)
}
`)

	ctx := context.Background()
	result, err := checker.CheckAuthEnforcement(ctx, "handlers",
		safety.WithAuthCheckType("authorization"))

	if err != nil {
		t.Fatalf("CheckAuthEnforcement failed: %v", err)
	}

	// Should detect missing authorization
	if result.MissingAuthz == 0 {
		t.Error("Expected to detect missing authorization on admin DELETE endpoint")
	}
}

func TestAuthChecker_CheckAuthEnforcement_Suggestions(t *testing.T) {
	g, idx := createTestGraph()
	checker := NewAuthChecker(g, idx)

	checker.SetFileContent("handlers/router.go", `
package handlers

import "github.com/gin-gonic/gin"

func SetupRoutes(r *gin.Engine) {
	r.GET("/admin/users", getAdminUsers)
}
`)

	ctx := context.Background()
	result, err := checker.CheckAuthEnforcement(ctx, "handlers")

	if err != nil {
		t.Fatalf("CheckAuthEnforcement failed: %v", err)
	}

	// Should have suggestions
	if len(result.Suggestions) == 0 {
		t.Error("Expected suggestions for missing auth")
	}

	// Should have framework-specific suggestion
	foundFrameworkSuggestion := false
	for _, s := range result.Suggestions {
		if len(s) > 0 {
			foundFrameworkSuggestion = true
			break
		}
	}

	if !foundFrameworkSuggestion {
		t.Error("Expected framework-specific suggestion")
	}
}

func TestAuthChecker_CheckAuthEnforcement_Performance(t *testing.T) {
	g, idx := createTestGraph()
	checker := NewAuthChecker(g, idx)

	checker.SetFileContent("handlers/router.go", `
package handlers

import "github.com/gin-gonic/gin"

func SetupRoutes(r *gin.Engine) {
	r.GET("/users", getUsers)
}
`)

	ctx := context.Background()
	start := time.Now()

	_, err := checker.CheckAuthEnforcement(ctx, "handlers")

	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("CheckAuthEnforcement failed: %v", err)
	}

	// Target: < 500ms
	if elapsed > 500*time.Millisecond {
		t.Errorf("CheckAuthEnforcement took %v, expected < 500ms", elapsed)
	}
}

func TestAuthChecker_CheckAuthEnforcement_NestJS(t *testing.T) {
	g := graph.NewGraph("/test")
	idx := index.NewSymbolIndex()

	sym := &ast.Symbol{
		ID:        "users.controller",
		Name:      "UsersController",
		Kind:      ast.SymbolKindClass,
		Language:  "typescript",
		FilePath:  "src/users/users.controller.ts",
		Package:   "users",
		StartLine: 1,
	}
	g.AddNode(sym)
	idx.Add(sym)
	g.Freeze()

	checker := NewAuthChecker(g, idx)

	checker.SetFileContent("src/users/users.controller.ts", `
import { Controller, Get, UseGuards } from '@nestjs/common';
import { JwtAuthGuard } from '../auth/jwt-auth.guard';

@Controller('users')
export class UsersController {
  @UseGuards(JwtAuthGuard)
  @Get()
  findAll() {
    return [];
  }

  @Get('admin')
  // No guard!
  adminEndpoint() {
    return 'admin';
  }
}
`)

	ctx := context.Background()
	result, err := checker.CheckAuthEnforcement(ctx, "users",
		safety.WithFrameworkHint("nestjs"))

	if err != nil {
		t.Fatalf("CheckAuthEnforcement failed: %v", err)
	}

	if result.Framework != "nestjs" {
		t.Errorf("Expected framework nestjs, got %s", result.Framework)
	}

	// Should detect routes
	if len(result.Endpoints) == 0 {
		t.Error("Expected to detect NestJS routes")
	}
}
