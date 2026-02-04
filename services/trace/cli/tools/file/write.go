// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package file

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/cli/tools"
)

// WriteTool implements the Write file operation.
//
// Thread Safety: WriteTool is safe for concurrent use.
type WriteTool struct {
	config *Config
}

// NewWriteTool creates a new Write tool with the given configuration.
func NewWriteTool(config *Config) *WriteTool {
	return &WriteTool{config: config}
}

// Name returns the tool name.
func (t *WriteTool) Name() string {
	return "Write"
}

// Category returns the tool category.
func (t *WriteTool) Category() tools.ToolCategory {
	return tools.CategoryFile
}

// Definition returns the tool's parameter schema.
func (t *WriteTool) Definition() tools.ToolDefinition {
	return tools.ToolDefinition{
		Name:        "Write",
		Description: "Create a new file or overwrite an existing file. Uses atomic writes for safety. Requires approval before writing.",
		Parameters: map[string]tools.ParamDef{
			"file_path": {
				Type:        tools.ParamTypeString,
				Description: "Path for the file to write. Can be absolute or relative to the project root.",
				Required:    true,
			},
			"content": {
				Type:        tools.ParamTypeString,
				Description: "File content to write",
				Required:    true,
			},
		},
		Category:    tools.CategoryFile,
		Priority:    95,
		SideEffects: true, // Modifies filesystem
		Timeout:     30 * time.Second,
		Examples: []tools.ToolExample{
			{
				Description: "Create a new Go file",
				Parameters: map[string]any{
					"file_path": "/path/to/new_file.go",
					"content":   "package main\n\nfunc main() {\n}\n",
				},
			},
		},
	}
}

// Execute writes content to a file using atomic write.
func (t *WriteTool) Execute(ctx context.Context, params map[string]any) (*tools.Result, error) {
	start := time.Now()

	// Parse parameters
	p := &WriteParams{}
	if filePath, ok := params["file_path"].(string); ok {
		p.FilePath = filePath
	}
	if content, ok := params["content"].(string); ok {
		p.Content = content
	}

	// Resolve relative paths to absolute using working directory
	if p.FilePath != "" && !filepath.IsAbs(p.FilePath) {
		p.FilePath = filepath.Join(t.config.WorkingDir, p.FilePath)
	}

	// Validate
	if err := p.Validate(); err != nil {
		return &tools.Result{
			Success:  false,
			Error:    err.Error(),
			Duration: time.Since(start),
		}, nil
	}

	// Check if sensitive path
	if IsSensitivePath(p.FilePath) {
		return &tools.Result{
			Success:  false,
			Error:    "cannot write to sensitive path",
			Duration: time.Since(start),
		}, nil
	}

	// Check if path is within allowed directories
	if !t.config.IsPathAllowed(p.FilePath) {
		return &tools.Result{
			Success:  false,
			Error:    "path is outside allowed directories",
			Duration: time.Since(start),
		}, nil
	}

	// Check if file exists (for Created flag)
	_, err := os.Stat(p.FilePath)
	isNew := os.IsNotExist(err)

	// Create parent directories if needed
	dir := filepath.Dir(p.FilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return &tools.Result{
			Success:  false,
			Error:    fmt.Sprintf("failed to create parent directories: %v", err),
			Duration: time.Since(start),
		}, nil
	}

	// Perform atomic write
	if err := atomicWriteFile(p.FilePath, []byte(p.Content), 0644); err != nil {
		return &tools.Result{
			Success:  false,
			Error:    fmt.Sprintf("failed to write file: %v", err),
			Duration: time.Since(start),
		}, nil
	}

	result := &WriteResult{
		Success:      true,
		BytesWritten: int64(len(p.Content)),
		Path:         p.FilePath,
		Created:      isNew,
	}

	return &tools.Result{
		Success:       true,
		Output:        result,
		OutputText:    fmt.Sprintf("Wrote %d bytes to %s", result.BytesWritten, result.Path),
		Duration:      time.Since(start),
		ModifiedFiles: []string{p.FilePath},
	}, nil
}

// atomicWriteFile writes content to a file atomically using rename.
//
// This ensures that the file is either fully written or not modified at all,
// preventing partial writes on crashes or errors.
func atomicWriteFile(path string, content []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	// Create temp file in same directory (ensures same filesystem for rename)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()

	// Clean up temp file on any error
	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	// Write content
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("writing content: %w", err)
	}

	// Sync to disk
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("syncing to disk: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}

	// Set permissions before rename
	if err := os.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("setting permissions: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming temp file: %w", err)
	}

	success = true
	return nil
}
