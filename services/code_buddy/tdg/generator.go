// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package tdg

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"
)

// =============================================================================
// INTERFACES
// =============================================================================

// LLMClient defines the interface for LLM interactions.
type LLMClient interface {
	// Generate produces text from a prompt.
	Generate(ctx context.Context, prompt string) (string, error)

	// GenerateWithSystem produces text with a system prompt.
	GenerateWithSystem(ctx context.Context, system, prompt string) (string, error)
}

// ContextAssembler assembles code context for prompts.
type ContextAssembler interface {
	// AssembleContext gathers relevant code for a query.
	AssembleContext(ctx context.Context, query string, tokenBudget int) (string, error)
}

// =============================================================================
// TEST GENERATOR
// =============================================================================

// TestGenerator generates tests and fixes using an LLM.
//
// Thread Safety: Safe for concurrent use if LLMClient and ContextAssembler
// are safe for concurrent use.
type TestGenerator struct {
	llm       LLMClient
	context   ContextAssembler
	logger    *slog.Logger
	callCount atomic.Int64
}

// NewTestGenerator creates a new test generator.
//
// Inputs:
//
//	llm - LLM client for generation
//	context - Context assembler for code context
//	logger - Logger for structured logging
//
// Outputs:
//
//	*TestGenerator - Configured generator
func NewTestGenerator(llm LLMClient, context ContextAssembler, logger *slog.Logger) *TestGenerator {
	if logger == nil {
		logger = slog.Default()
	}
	return &TestGenerator{
		llm:     llm,
		context: context,
		logger:  logger,
	}
}

// CallCount returns the number of LLM calls made.
//
// Thread Safety: Safe for concurrent use.
func (g *TestGenerator) CallCount() int {
	return int(g.callCount.Load())
}

// ResetCallCount resets the call counter.
//
// Thread Safety: Safe for concurrent use.
func (g *TestGenerator) ResetCallCount() {
	g.callCount.Store(0)
}

// =============================================================================
// TEST GENERATION
// =============================================================================

// GenerateReproducerTest creates a test that should FAIL on current code.
//
// Description:
//
//	Uses the LLM to generate a test that reproduces the described bug.
//	The test should fail on the current code and pass after the fix.
//
// Inputs:
//
//	ctx - Context for cancellation
//	req - The TDG request with bug description
//
// Outputs:
//
//	*TestCase - The generated test
//	error - Non-nil on generation failure
func (g *TestGenerator) GenerateReproducerTest(ctx context.Context, req *Request) (*TestCase, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	start := time.Now()
	g.logger.Debug("Generating reproducer test",
		slog.String("language", req.Language),
		slog.Int("desc_length", len(req.BugDescription)),
	)

	// Assemble code context
	codeContext := ""
	if g.context != nil {
		var err error
		codeContext, err = g.context.AssembleContext(ctx, req.BugDescription, 4000)
		if err != nil {
			g.logger.Warn("Failed to assemble context",
				slog.String("error", err.Error()),
			)
			// Continue without context
		}
	}

	// Build prompt
	prompt := g.buildTestGenerationPrompt(req, codeContext)

	// Generate via LLM
	g.callCount.Add(1)
	response, err := g.llm.GenerateWithSystem(ctx, tdgSystemPrompt, prompt)
	if err != nil {
		g.logger.Error("LLM generation failed",
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("%w: %v", ErrLLMGenerationFailed, err)
	}

	// Parse response to extract test
	tc, err := g.parseTestFromResponse(response, req.Language)
	if err != nil {
		g.logger.Error("Failed to parse test from response",
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	// Set package path based on target file or project
	if req.TargetFile != "" {
		tc.PackagePath = strings.TrimSuffix(req.TargetFile, ".go")
		tc.PackagePath = strings.TrimSuffix(tc.PackagePath, ".py")
		tc.PackagePath = strings.TrimSuffix(tc.PackagePath, ".ts")
	}

	g.logger.Info("Generated reproducer test",
		slog.String("name", tc.Name),
		slog.String("file", tc.FilePath),
		slog.Duration("duration", time.Since(start)),
	)

	return tc, nil
}

// RefineTest regenerates a test after it passed when it should have failed.
//
// Description:
//
//	Provides feedback to the LLM that the test didn't reproduce the bug
//	and asks for a new test that actually fails.
//
// Inputs:
//
//	ctx - Context for cancellation
//	req - The original request
//	previousTest - The test that incorrectly passed
//	testOutput - The output showing the test passed
//
// Outputs:
//
//	*TestCase - A new test
//	error - Non-nil on generation failure
func (g *TestGenerator) RefineTest(ctx context.Context, req *Request, previousTest *TestCase, testOutput string) (*TestCase, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	start := time.Now()
	g.logger.Debug("Refining test",
		slog.String("previous_name", previousTest.Name),
	)

	prompt := g.buildTestRefinementPrompt(req, previousTest, testOutput)

	g.callCount.Add(1)
	response, err := g.llm.GenerateWithSystem(ctx, tdgSystemPrompt, prompt)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLLMGenerationFailed, err)
	}

	tc, err := g.parseTestFromResponse(response, req.Language)
	if err != nil {
		return nil, err
	}

	g.logger.Info("Refined test",
		slog.String("name", tc.Name),
		slog.Duration("duration", time.Since(start)),
	)

	return tc, nil
}

// =============================================================================
// FIX GENERATION
// =============================================================================

// GenerateFix creates a patch to fix the bug.
//
// Description:
//
//	Uses the LLM to generate a code fix based on the reproducer test
//	and its failure output.
//
// Inputs:
//
//	ctx - Context for cancellation
//	req - The original request
//	tc - The reproducer test
//	testOutput - Output from running the failing test
//
// Outputs:
//
//	*Patch - The generated fix
//	error - Non-nil on generation failure
func (g *TestGenerator) GenerateFix(ctx context.Context, req *Request, tc *TestCase, testOutput string) (*Patch, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	start := time.Now()
	g.logger.Debug("Generating fix",
		slog.String("test_name", tc.Name),
		slog.Int("output_length", len(testOutput)),
	)

	// Assemble code context focused on the bug
	codeContext := ""
	if g.context != nil {
		var err error
		codeContext, err = g.context.AssembleContext(ctx, req.BugDescription, 4000)
		if err != nil {
			g.logger.Warn("Failed to assemble context",
				slog.String("error", err.Error()),
			)
		}
	}

	prompt := g.buildFixGenerationPrompt(req, tc, testOutput, codeContext)

	g.callCount.Add(1)
	response, err := g.llm.GenerateWithSystem(ctx, tdgSystemPrompt, prompt)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLLMGenerationFailed, err)
	}

	patch, err := g.parsePatchFromResponse(response)
	if err != nil {
		g.logger.Error("Failed to parse patch from response",
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	g.logger.Info("Generated fix",
		slog.String("file", patch.FilePath),
		slog.Int("new_size", len(patch.NewContent)),
		slog.Duration("duration", time.Since(start)),
	)

	return patch, nil
}

// RefineFix regenerates a fix after the test still failed.
//
// Description:
//
//	Provides feedback that the fix didn't work and asks for a new fix.
//
// Inputs:
//
//	ctx - Context for cancellation
//	req - The original request
//	tc - The reproducer test
//	previousPatch - The fix that didn't work
//	testOutput - Output showing the test still fails
//
// Outputs:
//
//	*Patch - A new fix
//	error - Non-nil on generation failure
func (g *TestGenerator) RefineFix(ctx context.Context, req *Request, tc *TestCase, previousPatch *Patch, testOutput string) (*Patch, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	start := time.Now()
	g.logger.Debug("Refining fix",
		slog.String("previous_file", previousPatch.FilePath),
	)

	prompt := g.buildFixRefinementPrompt(req, tc, previousPatch, testOutput)

	g.callCount.Add(1)
	response, err := g.llm.GenerateWithSystem(ctx, tdgSystemPrompt, prompt)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLLMGenerationFailed, err)
	}

	patch, err := g.parsePatchFromResponse(response)
	if err != nil {
		return nil, err
	}

	g.logger.Info("Refined fix",
		slog.String("file", patch.FilePath),
		slog.Duration("duration", time.Since(start)),
	)

	return patch, nil
}

// =============================================================================
// PROMPTS
// =============================================================================

const tdgSystemPrompt = `You are in TEST-DRIVEN GENERATION mode.

RULES:
1. FIRST write a test that reproduces the bug
2. The test MUST fail on current code (I will verify)
3. THEN write the fix
4. The test MUST pass after your fix (I will verify)
5. No other tests may break

If you can't write a reproducer test, explain why.
If your test passes when it shouldn't, I'll ask you to try again.
If your fix breaks other tests, you must fix those too.

RESPONSE FORMAT for tests:
` + "```" + `TEST_FILE: path/to/test_file.go
` + "```" + `
` + "```" + `go
// test code here
` + "```" + `

RESPONSE FORMAT for fixes:
` + "```" + `FIX_FILE: path/to/file.go
` + "```" + `
` + "```" + `go
// complete file content with fix
` + "```" + ``

func (g *TestGenerator) buildTestGenerationPrompt(req *Request, codeContext string) string {
	var sb strings.Builder

	sb.WriteString("Write a test that reproduces this bug:\n\n")
	sb.WriteString(req.BugDescription)
	sb.WriteString("\n\n")

	if codeContext != "" {
		sb.WriteString("Relevant code context:\n")
		sb.WriteString(codeContext)
		sb.WriteString("\n\n")
	}

	sb.WriteString("Requirements:\n")
	sb.WriteString("- Test should FAIL on current code (reproduces the bug)\n")
	sb.WriteString("- Test should PASS after the correct fix\n")
	sb.WriteString(fmt.Sprintf("- Use standard %s testing framework\n", req.Language))
	sb.WriteString("- Name the test descriptively (e.g., TestFunctionName_Scenario_ExpectedBehavior)\n")
	sb.WriteString("- Include clear arrange/act/assert sections\n")

	return sb.String()
}

func (g *TestGenerator) buildTestRefinementPrompt(req *Request, previousTest *TestCase, testOutput string) string {
	var sb strings.Builder

	sb.WriteString("Your previous test PASSED when it should have FAILED.\n\n")
	sb.WriteString("Previous test:\n")
	sb.WriteString(previousTest.Content)
	sb.WriteString("\n\nTest output (unexpectedly passed):\n")
	sb.WriteString(testOutput)
	sb.WriteString("\n\nBug description:\n")
	sb.WriteString(req.BugDescription)
	sb.WriteString("\n\nPlease write a NEW test that actually reproduces the bug.\n")
	sb.WriteString("The test must FAIL on the current code.\n")

	return sb.String()
}

func (g *TestGenerator) buildFixGenerationPrompt(req *Request, tc *TestCase, testOutput, codeContext string) string {
	var sb strings.Builder

	sb.WriteString("Your reproducer test is confirmed to fail:\n\n")
	sb.WriteString("Test:\n")
	sb.WriteString(tc.Content)
	sb.WriteString("\n\nTest output (failure):\n")
	sb.WriteString(testOutput)
	sb.WriteString("\n\n")

	if codeContext != "" {
		sb.WriteString("Code context:\n")
		sb.WriteString(codeContext)
		sb.WriteString("\n\n")
	}

	sb.WriteString("Bug description:\n")
	sb.WriteString(req.BugDescription)
	sb.WriteString("\n\nNow write the fix. The test must pass after your changes.\n")
	sb.WriteString("Provide the complete file content, not just the diff.\n")

	return sb.String()
}

func (g *TestGenerator) buildFixRefinementPrompt(req *Request, tc *TestCase, previousPatch *Patch, testOutput string) string {
	var sb strings.Builder

	sb.WriteString("Your previous fix did NOT work. The test still fails.\n\n")
	sb.WriteString("Previous fix (file: " + previousPatch.FilePath + "):\n")
	sb.WriteString(previousPatch.NewContent)
	sb.WriteString("\n\nTest that still fails:\n")
	sb.WriteString(tc.Content)
	sb.WriteString("\n\nTest output:\n")
	sb.WriteString(testOutput)
	sb.WriteString("\n\nPlease provide a corrected fix.\n")

	return sb.String()
}

// =============================================================================
// RESPONSE PARSING
// =============================================================================

func (g *TestGenerator) parseTestFromResponse(response, language string) (*TestCase, error) {
	// Extract file path from TEST_FILE marker
	filePath := extractMarkedPath(response, "TEST_FILE:")
	if filePath == "" {
		// Try to infer from language
		filePath = inferTestFilePath(language)
	}

	// Extract code block
	content := extractCodeBlock(response, language)
	if content == "" {
		return nil, fmt.Errorf("%w: no code block found in response", ErrInvalidTestCase)
	}

	// Extract test name from content
	name := extractTestName(content, language)
	if name == "" {
		name = "TestReproducer"
	}

	return &TestCase{
		Name:     name,
		FilePath: filePath,
		Content:  content,
		Language: language,
		TestType: "unit",
	}, nil
}

func (g *TestGenerator) parsePatchFromResponse(response string) (*Patch, error) {
	// Extract file path from FIX_FILE marker
	filePath := extractMarkedPath(response, "FIX_FILE:")
	if filePath == "" {
		return nil, fmt.Errorf("%w: no FIX_FILE marker found", ErrInvalidPatch)
	}

	// Extract code block (try to detect language from file extension)
	lang := LanguageForFile(filePath)
	if lang == "" {
		lang = "go" // Default
	}
	content := extractCodeBlock(response, lang)
	if content == "" {
		return nil, fmt.Errorf("%w: no code block found in response", ErrInvalidPatch)
	}

	return &Patch{
		FilePath:   filePath,
		NewContent: content,
	}, nil
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

func extractMarkedPath(response, marker string) string {
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, marker) {
			path := strings.TrimPrefix(line, marker)
			return strings.TrimSpace(path)
		}
		// Also check inside code blocks
		if strings.Contains(line, marker) {
			parts := strings.Split(line, marker)
			if len(parts) > 1 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func extractCodeBlock(response, language string) string {
	// Look for ```language or just ```
	markers := []string{
		"```" + language,
		"```" + strings.ToLower(language),
		"```",
	}

	for _, marker := range markers {
		start := strings.Index(response, marker)
		if start == -1 {
			continue
		}

		// Find the closing ```
		start += len(marker)
		// Skip to next line if marker had content after it
		if idx := strings.Index(response[start:], "\n"); idx != -1 {
			start += idx + 1
		}

		end := strings.Index(response[start:], "```")
		if end == -1 {
			continue
		}

		content := strings.TrimSpace(response[start : start+end])
		if content != "" {
			return content
		}
	}

	return ""
}

func extractTestName(content, language string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch language {
		case "go":
			if strings.HasPrefix(line, "func Test") {
				// Extract function name
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					name := parts[1]
					if idx := strings.Index(name, "("); idx != -1 {
						name = name[:idx]
					}
					return name
				}
			}
		case "python":
			if strings.HasPrefix(line, "def test_") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					name := parts[1]
					if idx := strings.Index(name, "("); idx != -1 {
						name = name[:idx]
					}
					return name
				}
			}
		case "typescript", "javascript":
			if strings.Contains(line, "test(") || strings.Contains(line, "it(") {
				// Extract test description
				if idx := strings.Index(line, "'"); idx != -1 {
					end := strings.Index(line[idx+1:], "'")
					if end != -1 {
						return line[idx+1 : idx+1+end]
					}
				}
				if idx := strings.Index(line, "\""); idx != -1 {
					end := strings.Index(line[idx+1:], "\"")
					if end != -1 {
						return line[idx+1 : idx+1+end]
					}
				}
			}
		}
	}
	return ""
}

func inferTestFilePath(language string) string {
	switch language {
	case "go":
		return "reproducer_test.go"
	case "python":
		return "test_reproducer.py"
	case "typescript":
		return "reproducer.test.ts"
	case "javascript":
		return "reproducer.test.js"
	default:
		return "reproducer_test"
	}
}
