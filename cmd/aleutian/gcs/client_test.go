// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.

package gcs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// NewClient Tests
// ============================================================================

func TestNewClient_NonExistentSAKeyPath(t *testing.T) {
	ctx := context.Background()

	_, err := NewClient(ctx, "test-project", "test-bucket", "/nonexistent/path/to/key.json")
	if err == nil {
		t.Fatal("NewClient with non-existent SA key should return error")
	}
	if !strings.Contains(err.Error(), "service account key not found") {
		t.Errorf("Error should mention SA key not found, got: %v", err)
	}
	if !strings.Contains(err.Error(), "/nonexistent/path/to/key.json") {
		t.Errorf("Error should contain the path, got: %v", err)
	}
}

func TestNewClient_EmptyPath(t *testing.T) {
	ctx := context.Background()

	_, err := NewClient(ctx, "test-project", "test-bucket", "")
	if err == nil {
		t.Fatal("NewClient with empty SA key path should return error")
	}
}

func TestNewClient_InvalidCredentialsFile(t *testing.T) {
	ctx := context.Background()

	// Create a temporary file with invalid JSON
	tmpDir := t.TempDir()
	invalidKeyPath := filepath.Join(tmpDir, "invalid_key.json")
	err := os.WriteFile(invalidKeyPath, []byte("not valid json"), 0644)
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	_, err = NewClient(ctx, "test-project", "test-bucket", invalidKeyPath)
	if err == nil {
		t.Fatal("NewClient with invalid credentials file should return error")
	}
	if !strings.Contains(err.Error(), "failed to create GCS storage client") {
		t.Errorf("Error should mention failed to create client, got: %v", err)
	}
}

func TestNewClient_DirectoryInsteadOfFile(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Try to use a directory as the credentials file
	_, err := NewClient(ctx, "test-project", "test-bucket", tmpDir)
	if err == nil {
		t.Fatal("NewClient with directory as SA key should return error")
	}
}

// ============================================================================
// UploadFile Tests (error paths that don't require GCS connection)
// ============================================================================

func TestClient_UploadFile_NonExistentLocalFile(t *testing.T) {
	// Create a client struct directly without a real storage client
	// This tests the local file validation before any GCS operations
	client := &Client{
		storageClient: nil, // Will fail if we try to use it
		ProjectId:     "test-project",
		BucketName:    "test-bucket",
	}

	ctx := context.Background()
	err := client.UploadFile(ctx, "/nonexistent/file/path.txt", "dest/path.txt")
	if err == nil {
		t.Fatal("UploadFile with non-existent local file should return error")
	}
	if !strings.Contains(err.Error(), "failed to open the local file") {
		t.Errorf("Error should mention failed to open file, got: %v", err)
	}
	if !strings.Contains(err.Error(), "/nonexistent/file/path.txt") {
		t.Errorf("Error should contain the path, got: %v", err)
	}
}

func TestClient_UploadFile_DirectoryInsteadOfFile(t *testing.T) {
	// Note: This test is skipped because on some systems (macOS),
	// opening a directory for reading succeeds, and the test would
	// require a real storage client to proceed past the file open.
	// The important error paths (file not found, empty path) are
	// tested in other test cases.
	t.Skip("Skipped: behavior is platform-dependent and requires real GCS client")
}

func TestClient_UploadFile_EmptyPath(t *testing.T) {
	client := &Client{
		storageClient: nil,
		ProjectId:     "test-project",
		BucketName:    "test-bucket",
	}

	ctx := context.Background()
	err := client.UploadFile(ctx, "", "dest/path.txt")
	if err == nil {
		t.Fatal("UploadFile with empty local path should return error")
	}
}

// ============================================================================
// UploadDir Tests (error paths)
// ============================================================================

func TestClient_UploadDir_NonExistentDirectory(t *testing.T) {
	client := &Client{
		storageClient: nil,
		ProjectId:     "test-project",
		BucketName:    "test-bucket",
	}

	ctx := context.Background()
	err := client.UploadDir(ctx, "/nonexistent/directory/path", "dest/prefix")
	if err == nil {
		t.Fatal("UploadDir with non-existent directory should return error")
	}
}

func TestClient_UploadDir_EmptyPath(t *testing.T) {
	client := &Client{
		storageClient: nil,
		ProjectId:     "test-project",
		BucketName:    "test-bucket",
	}

	ctx := context.Background()
	err := client.UploadDir(ctx, "", "dest/prefix")
	if err == nil {
		t.Fatal("UploadDir with empty path should return error")
	}
}

func TestClient_UploadDir_FileInsteadOfDirectory(t *testing.T) {
	// Note: This test is skipped because filepath.Walk on a file will
	// visit it as a non-directory and try to call UploadFile, which
	// would require a real storage client.
	// The important error paths (dir not found, empty path) are
	// tested in other test cases.
	t.Skip("Skipped: requires real GCS client to test file upload within Walk")
}

// ============================================================================
// Client Fields Tests
// ============================================================================

func TestClient_Fields(t *testing.T) {
	client := &Client{
		storageClient: nil,
		ProjectId:     "my-project-123",
		BucketName:    "my-bucket-456",
	}

	if client.ProjectId != "my-project-123" {
		t.Errorf("ProjectId = %q, want %q", client.ProjectId, "my-project-123")
	}
	if client.BucketName != "my-bucket-456" {
		t.Errorf("BucketName = %q, want %q", client.BucketName, "my-bucket-456")
	}
}

// ============================================================================
// Context Handling Tests
// ============================================================================

func TestNewClient_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Even with canceled context, the SA key check happens first
	_, err := NewClient(ctx, "test-project", "test-bucket", "/nonexistent/key.json")
	if err == nil {
		t.Fatal("Should still return error for non-existent key")
	}
	// The error should be about the key file, not context cancellation
	if !strings.Contains(err.Error(), "service account key not found") {
		t.Errorf("Expected SA key error, got: %v", err)
	}
}

func TestClient_UploadFile_CanceledContext(t *testing.T) {
	client := &Client{
		storageClient: nil,
		ProjectId:     "test-project",
		BucketName:    "test-bucket",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// The file open happens before context is checked
	err := client.UploadFile(ctx, "/nonexistent/file.txt", "dest/path.txt")
	if err == nil {
		t.Fatal("Should return error for non-existent file")
	}
}

// ============================================================================
// Integration Tests (require real GCS credentials)
// These tests are skipped by default but document how to test with real GCS
// ============================================================================

func TestNewClient_Integration(t *testing.T) {
	// Skip unless explicitly running integration tests
	keyPath := os.Getenv("GCS_TEST_SA_KEY_PATH")
	projectID := os.Getenv("GCS_TEST_PROJECT_ID")
	bucketName := os.Getenv("GCS_TEST_BUCKET_NAME")

	if keyPath == "" || projectID == "" || bucketName == "" {
		t.Skip("Skipping integration test: GCS_TEST_SA_KEY_PATH, GCS_TEST_PROJECT_ID, and GCS_TEST_BUCKET_NAME not set")
	}

	ctx := context.Background()
	client, err := NewClient(ctx, projectID, bucketName, keyPath)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if client == nil {
		t.Fatal("NewClient returned nil client")
	}
	if client.ProjectId != projectID {
		t.Errorf("ProjectId = %q, want %q", client.ProjectId, projectID)
	}
	if client.BucketName != bucketName {
		t.Errorf("BucketName = %q, want %q", client.BucketName, bucketName)
	}
}

func TestClient_UploadFile_Integration(t *testing.T) {
	keyPath := os.Getenv("GCS_TEST_SA_KEY_PATH")
	projectID := os.Getenv("GCS_TEST_PROJECT_ID")
	bucketName := os.Getenv("GCS_TEST_BUCKET_NAME")

	if keyPath == "" || projectID == "" || bucketName == "" {
		t.Skip("Skipping integration test: GCS_TEST_SA_KEY_PATH, GCS_TEST_PROJECT_ID, and GCS_TEST_BUCKET_NAME not set")
	}

	ctx := context.Background()
	client, err := NewClient(ctx, projectID, bucketName, keyPath)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	// Create a temp file to upload
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test_upload.txt")
	err = os.WriteFile(testFile, []byte("test content for upload"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	err = client.UploadFile(ctx, testFile, "test/integration_test_upload.txt")
	if err != nil {
		t.Errorf("UploadFile failed: %v", err)
	}
}

func TestClient_UploadDir_Integration(t *testing.T) {
	keyPath := os.Getenv("GCS_TEST_SA_KEY_PATH")
	projectID := os.Getenv("GCS_TEST_PROJECT_ID")
	bucketName := os.Getenv("GCS_TEST_BUCKET_NAME")

	if keyPath == "" || projectID == "" || bucketName == "" {
		t.Skip("Skipping integration test: GCS_TEST_SA_KEY_PATH, GCS_TEST_PROJECT_ID, and GCS_TEST_BUCKET_NAME not set")
	}

	ctx := context.Background()
	client, err := NewClient(ctx, projectID, bucketName, keyPath)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	// Create a temp directory with files
	tmpDir := t.TempDir()
	err = os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content 1"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file 1: %v", err)
	}
	err = os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content 2"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file 2: %v", err)
	}

	err = client.UploadDir(ctx, tmpDir, "test/integration_dir_upload")
	if err != nil {
		t.Errorf("UploadDir failed: %v", err)
	}
}
