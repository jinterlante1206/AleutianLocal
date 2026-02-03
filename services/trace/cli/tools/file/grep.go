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
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/trace/cli/tools"
)

// GrepTool implements the Grep content search operation.
//
// Thread Safety: GrepTool is safe for concurrent use.
type GrepTool struct {
	config *Config
}

// NewGrepTool creates a new Grep tool with the given configuration.
func NewGrepTool(config *Config) *GrepTool {
	return &GrepTool{config: config}
}

// Name returns the tool name.
func (t *GrepTool) Name() string {
	return "Grep"
}

// Category returns the tool category.
func (t *GrepTool) Category() tools.ToolCategory {
	return tools.CategoryFile
}

// Definition returns the tool's parameter schema.
func (t *GrepTool) Definition() tools.ToolDefinition {
	return tools.ToolDefinition{
		Name:        "Grep",
		Description: "Search file contents using regex patterns. Returns matching lines with optional context. Use glob parameter to filter file types.",
		Parameters: map[string]tools.ParamDef{
			"pattern": {
				Type:        tools.ParamTypeString,
				Description: "Regex pattern to search for",
				Required:    true,
			},
			"path": {
				Type:        tools.ParamTypeString,
				Description: "File or directory to search. Defaults to working directory.",
				Required:    false,
			},
			"glob": {
				Type:        tools.ParamTypeString,
				Description: "File pattern filter (e.g., '*.go', '*.{ts,js}')",
				Required:    false,
			},
			"context_lines": {
				Type:        tools.ParamTypeInt,
				Description: fmt.Sprintf("Lines of context before and after match (max %d)", MaxContextLines),
				Required:    false,
				Default:     0,
			},
			"case_insensitive": {
				Type:        tools.ParamTypeBool,
				Description: "Enable case-insensitive matching",
				Required:    false,
				Default:     false,
			},
			"limit": {
				Type:        tools.ParamTypeInt,
				Description: fmt.Sprintf("Maximum matches to return. Defaults to %d, max %d.", DefaultGrepLimit, MaxGrepLimit),
				Required:    false,
				Default:     DefaultGrepLimit,
			},
		},
		Category:    tools.CategoryFile,
		Priority:    85,
		SideEffects: false,
		Timeout:     60 * time.Second,
		Examples: []tools.ToolExample{
			{
				Description: "Find function definitions",
				Parameters: map[string]any{
					"pattern": "func\\s+\\w+\\(",
					"glob":    "*.go",
				},
			},
			{
				Description: "Find TODO comments with context",
				Parameters: map[string]any{
					"pattern":       "TODO:",
					"context_lines": 2,
				},
			},
		},
	}
}

// Execute searches for content matching the pattern.
func (t *GrepTool) Execute(ctx context.Context, params map[string]any) (*tools.Result, error) {
	start := time.Now()

	// Parse parameters
	p := &GrepParams{}
	if pattern, ok := params["pattern"].(string); ok {
		p.Pattern = pattern
	}
	if path, ok := params["path"].(string); ok {
		p.Path = path
	}
	if glob, ok := params["glob"].(string); ok {
		p.Glob = glob
	}
	if contextLines, ok := getIntParam(params, "context_lines"); ok {
		p.ContextLines = contextLines
	}
	if caseInsensitive, ok := params["case_insensitive"].(bool); ok {
		p.CaseInsensitive = caseInsensitive
	}
	if limit, ok := getIntParam(params, "limit"); ok {
		p.Limit = limit
	}

	// Set defaults
	if p.Limit == 0 {
		p.Limit = DefaultGrepLimit
	}
	if p.Path == "" {
		p.Path = t.config.WorkingDir
	}

	// Validate
	if err := p.Validate(); err != nil {
		return &tools.Result{
			Success:  false,
			Error:    err.Error(),
			Duration: time.Since(start),
		}, nil
	}

	// Compile regex
	regexPattern := p.Pattern
	if p.CaseInsensitive {
		regexPattern = "(?i)" + regexPattern
	}
	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return &tools.Result{
			Success:  false,
			Error:    fmt.Sprintf("invalid regex pattern: %v", err),
			Duration: time.Since(start),
		}, nil
	}

	// Check if path is within allowed directories
	if !t.config.IsPathAllowed(p.Path) {
		return &tools.Result{
			Success:  false,
			Error:    "path is outside allowed directories",
			Duration: time.Since(start),
		}, nil
	}

	// Perform search
	matches, filesSearched, totalCount, err := t.grepFiles(ctx, p.Path, re, p.Glob, p.ContextLines, p.Limit)
	if err != nil {
		return &tools.Result{
			Success:  false,
			Error:    err.Error(),
			Duration: time.Since(start),
		}, nil
	}

	result := &GrepResult{
		Matches:       matches,
		Count:         len(matches),
		Truncated:     totalCount > p.Limit,
		FilesSearched: filesSearched,
		SearchPath:    p.Path,
	}

	// Format output as grep-style results
	var output strings.Builder
	for _, m := range matches {
		// Show context before
		for _, line := range m.ContextBefore {
			output.WriteString(fmt.Sprintf("%s-%d- %s\n", m.File, m.Line-len(m.ContextBefore), line))
		}
		// Show matching line
		output.WriteString(fmt.Sprintf("%s:%d: %s\n", m.File, m.Line, m.Content))
		// Show context after
		for i, line := range m.ContextAfter {
			output.WriteString(fmt.Sprintf("%s-%d- %s\n", m.File, m.Line+i+1, line))
		}
		if len(m.ContextBefore) > 0 || len(m.ContextAfter) > 0 {
			output.WriteString("--\n")
		}
	}

	jsonOutput, _ := json.Marshal(result)
	return &tools.Result{
		Success:    true,
		Output:     result,
		OutputText: output.String(),
		Duration:   time.Since(start),
		TokensUsed: estimateTokens(string(jsonOutput)),
	}, nil
}

// grepFiles searches for pattern in files.
func (t *GrepTool) grepFiles(ctx context.Context, root string, re *regexp.Regexp, globPattern string, contextLines, limit int) ([]GrepMatch, int, int, error) {
	var matches []GrepMatch
	filesSearched := 0
	totalMatches := 0

	// Build exclusion set
	exclusions := make(map[string]bool)
	for _, ex := range DefaultExclusions {
		exclusions[ex] = true
	}

	// Expand glob patterns for matching
	var filePatterns []string
	if globPattern != "" {
		filePatterns = expandBraces(globPattern)
	}

	// Check if root is a file
	info, err := os.Stat(root)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("accessing path: %w", err)
	}

	if !info.IsDir() {
		// Search single file
		fileMatches, err := t.searchFile(ctx, root, re, contextLines)
		if err != nil {
			return nil, 0, 0, err
		}
		if len(fileMatches) > limit {
			return fileMatches[:limit], 1, len(fileMatches), nil
		}
		return fileMatches, 1, len(fileMatches), nil
	}

	// Walk directory
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil // Skip errors
		}

		// Skip excluded directories
		if d.IsDir() {
			if exclusions[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		// Check glob pattern
		if len(filePatterns) > 0 {
			matched := false
			for _, p := range filePatterns {
				if m, _ := filepath.Match(p, d.Name()); m {
					matched = true
					break
				}
			}
			if !matched {
				return nil
			}
		}

		// Skip binary files based on extension
		if isBinaryExtension(filepath.Ext(path)) {
			return nil
		}

		filesSearched++

		// Search file
		fileMatches, err := t.searchFile(ctx, path, re, contextLines)
		if err != nil {
			return nil // Skip file errors
		}

		totalMatches += len(fileMatches)

		// Collect matches up to limit
		for _, m := range fileMatches {
			if len(matches) >= limit {
				return filepath.SkipAll
			}
			matches = append(matches, m)
		}

		return nil
	})

	if err != nil && err != context.Canceled && err != filepath.SkipAll {
		return nil, filesSearched, totalMatches, fmt.Errorf("walking directory: %w", err)
	}

	return matches, filesSearched, totalMatches, nil
}

// searchFile searches for pattern matches in a single file.
func (t *GrepTool) searchFile(ctx context.Context, path string, re *regexp.Regexp, contextLines int) ([]GrepMatch, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var matches []GrepMatch
	var lineBuffer []string // Rolling buffer for context
	lineNum := 0

	scanner := bufio.NewScanner(file)
	// Increase buffer for long lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, MaxLineLengthChars*4)

	// Pending matches waiting for "after" context
	type pendingMatch struct {
		match       GrepMatch
		afterNeeded int
	}
	var pending []pendingMatch

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		lineNum++
		line := scanner.Text()

		// Truncate long lines
		if len(line) > MaxLineLengthChars {
			line = line[:MaxLineLengthChars] + "..."
		}

		// Add "after" context to pending matches
		for i := range pending {
			if pending[i].afterNeeded > 0 {
				pending[i].match.ContextAfter = append(pending[i].match.ContextAfter, line)
				pending[i].afterNeeded--
			}
		}

		// Move completed pending matches to results
		newPending := pending[:0]
		for _, p := range pending {
			if p.afterNeeded == 0 {
				matches = append(matches, p.match)
			} else {
				newPending = append(newPending, p)
			}
		}
		pending = newPending

		// Check for match
		if re.MatchString(line) {
			match := GrepMatch{
				File:    path,
				Line:    lineNum,
				Content: line,
			}

			// Add "before" context from buffer
			if contextLines > 0 && len(lineBuffer) > 0 {
				start := len(lineBuffer) - contextLines
				if start < 0 {
					start = 0
				}
				match.ContextBefore = make([]string, len(lineBuffer)-start)
				copy(match.ContextBefore, lineBuffer[start:])
			}

			if contextLines > 0 {
				pending = append(pending, pendingMatch{match: match, afterNeeded: contextLines})
			} else {
				matches = append(matches, match)
			}
		}

		// Update line buffer for context
		if contextLines > 0 {
			lineBuffer = append(lineBuffer, line)
			if len(lineBuffer) > contextLines {
				lineBuffer = lineBuffer[1:]
			}
		}
	}

	// Add remaining pending matches
	for _, p := range pending {
		matches = append(matches, p.match)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	return matches, nil
}

// isBinaryExtension returns true for known binary file extensions.
func isBinaryExtension(ext string) bool {
	binaryExts := map[string]bool{
		".exe": true, ".dll": true, ".so": true, ".dylib": true,
		".bin": true, ".obj": true, ".o": true, ".a": true,
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
		".ico": true, ".bmp": true, ".tiff": true, ".webp": true,
		".pdf": true, ".doc": true, ".docx": true, ".xls": true,
		".xlsx": true, ".ppt": true, ".pptx": true,
		".zip": true, ".tar": true, ".gz": true, ".bz2": true,
		".7z": true, ".rar": true, ".jar": true, ".war": true,
		".mp3": true, ".mp4": true, ".avi": true, ".mov": true,
		".wasm": true, ".pyc": true, ".pyo": true,
		".ttf": true, ".otf": true, ".woff": true, ".woff2": true,
		".db": true, ".sqlite": true, ".sqlite3": true,
	}
	return binaryExts[strings.ToLower(ext)]
}
