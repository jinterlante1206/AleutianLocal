// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Security limits.
const (
	// MaxToolCallsPerRequest limits tool calls per dispatch to prevent DoS.
	MaxToolCallsPerRequest = 20

	// MaxParamsSize limits parameter JSON size to prevent memory exhaustion.
	MaxParamsSize = 1 << 20 // 1MB

	// MaxOutputSize limits output size before truncation.
	MaxOutputSize = 1 << 22 // 4MB
)

// ApprovalFunc is called to check if a tool execution should proceed.
// Returns true if approved, false if declined.
// The error return is for approval system failures, not user declining.
type ApprovalFunc func(tool *ToolDefinition, params map[string]any) (bool, error)

// Dispatcher orchestrates tool execution with validation, approval, and formatting.
//
// Description:
//
//	The dispatcher is the high-level entry point for tool execution.
//	It coordinates between the parser (which extracts calls from LLM output),
//	the executor (which runs individual tools), and the formatter (which
//	prepares results for the LLM).
//
// Thread Safety: Dispatcher is safe for concurrent use.
type Dispatcher struct {
	registry  *Registry
	executor  *Executor
	formatter *Formatter
	recovery  *ErrorRecovery
	parser    *Parser
	approver  ApprovalFunc
	timeout   time.Duration
	mu        sync.RWMutex
}

// DispatcherOption configures the Dispatcher.
type DispatcherOption func(*Dispatcher)

// WithApprover sets the approval function for tools requiring user consent.
//
// Description:
//
//	The approval function is called for tools with SideEffects=true.
//	Return true to allow execution, false to decline.
//
// Inputs:
//
//	fn - The approval function. May be nil to disable approval checks.
func WithApprover(fn ApprovalFunc) DispatcherOption {
	return func(d *Dispatcher) {
		d.approver = fn
	}
}

// WithDispatchTimeout sets the default timeout for tool execution.
//
// Description:
//
//	Sets the maximum duration for a single tool execution.
//	Default is 30 seconds.
//
// Inputs:
//
//	timeout - The timeout duration. Zero or negative uses default.
func WithDispatchTimeout(timeout time.Duration) DispatcherOption {
	return func(d *Dispatcher) {
		d.timeout = timeout
	}
}

// WithDispatchFormatter sets a custom formatter.
//
// Description:
//
//	Overrides the default formatter used to format results for LLM consumption.
//
// Inputs:
//
//	f - The formatter. Must not be nil.
func WithDispatchFormatter(f *Formatter) DispatcherOption {
	return func(d *Dispatcher) {
		d.formatter = f
	}
}

// WithDispatchParser sets a custom parser.
//
// Description:
//
//	Overrides the default parser used to extract tool calls from LLM output.
//
// Inputs:
//
//	p - The parser. Must not be nil.
func WithDispatchParser(p *Parser) DispatcherOption {
	return func(d *Dispatcher) {
		d.parser = p
	}
}

// NewDispatcher creates a new tool dispatcher.
//
// Description:
//
//	Creates a dispatcher that coordinates tool parsing, execution, and formatting.
//	The registry and executor are required; other components use defaults if nil.
//
// Inputs:
//
//	registry - The tool registry. Must not be nil.
//	executor - The tool executor. Must not be nil.
//	opts - Configuration options
//
// Outputs:
//
//	*Dispatcher - The configured dispatcher. Returns nil if registry or executor is nil.
func NewDispatcher(registry *Registry, executor *Executor, opts ...DispatcherOption) *Dispatcher {
	if registry == nil || executor == nil {
		return nil
	}

	d := &Dispatcher{
		registry:  registry,
		executor:  executor,
		formatter: NewFormatter(),
		recovery:  NewErrorRecovery(),
		parser:    NewParser(),
		timeout:   30 * time.Second,
	}

	for _, opt := range opts {
		opt(d)
	}

	return d
}

// DispatchResult contains the outcome of a dispatch operation.
type DispatchResult struct {
	// Results contains individual tool execution results.
	Results []ExecutionResult `json:"results"`

	// FormattedOutput is the results formatted for LLM consumption.
	FormattedOutput string `json:"formatted_output"`

	// RemainingText is the LLM output text with tool calls removed.
	RemainingText string `json:"remaining_text"`

	// TotalDuration is the total time for all tool executions.
	TotalDuration time.Duration `json:"total_duration"`

	// FailedCount is the number of tools that failed.
	FailedCount int `json:"failed_count"`

	// ApprovedCount is the number of tools that required and received approval.
	ApprovedCount int `json:"approved_count"`

	// DeclinedCount is the number of tools that were declined by the approver.
	DeclinedCount int `json:"declined_count"`
}

// Dispatch parses and executes tool calls from LLM output.
//
// Description:
//
//	Parses the LLM output for tool calls, executes them sequentially,
//	and returns formatted results. This is the main entry point for
//	processing LLM output that may contain tool calls.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout
//	llmOutput - The raw LLM output text
//
// Outputs:
//
//	*DispatchResult - Execution results and formatted output
//	error - Non-nil if dispatch completely failed (partial failures in results)
//
// Thread Safety: This method is safe for concurrent use.
func (d *Dispatcher) Dispatch(ctx context.Context, llmOutput string) (*DispatchResult, error) {
	if ctx == nil {
		return nil, errors.New("ctx must not be nil")
	}

	// Parse tool calls from output
	calls, remaining, err := d.parser.Parse(llmOutput)
	if err != nil {
		return nil, fmt.Errorf("parsing tool calls: %w", err)
	}

	if len(calls) == 0 {
		return &DispatchResult{
			RemainingText: llmOutput,
		}, nil
	}

	// Execute the calls
	result, err := d.Execute(ctx, calls)
	if err != nil {
		return nil, err
	}

	result.RemainingText = remaining
	return result, nil
}

// Execute runs a set of parsed tool calls sequentially.
//
// Description:
//
//	Executes tool calls in order, collecting results. Execution continues
//	even if individual tools fail (partial success is allowed).
//
// Inputs:
//
//	ctx - Context for cancellation and timeout
//	calls - The tool calls to execute
//
// Outputs:
//
//	*DispatchResult - Execution results
//	error - Non-nil only for catastrophic failures
//
// Thread Safety: This method is safe for concurrent use.
func (d *Dispatcher) Execute(ctx context.Context, calls []ToolCall) (*DispatchResult, error) {
	if ctx == nil {
		return nil, errors.New("ctx must not be nil")
	}

	// Check limits
	if len(calls) > MaxToolCallsPerRequest {
		return nil, fmt.Errorf("too many tool calls: %d (max %d)", len(calls), MaxToolCallsPerRequest)
	}

	start := time.Now()
	result := &DispatchResult{
		Results: make([]ExecutionResult, 0, len(calls)),
	}

	for _, call := range calls {
		// Check for context cancellation
		if err := ctx.Err(); err != nil {
			break
		}

		execResult := d.executeSingle(ctx, call)
		result.Results = append(result.Results, execResult)

		if execResult.Result != nil && !execResult.Result.Success {
			result.FailedCount++
		}
		if execResult.Approved {
			result.ApprovedCount++
		}
		if execResult.Declined {
			result.DeclinedCount++
		}
	}

	result.TotalDuration = time.Since(start)
	result.FormattedOutput = d.formatter.Format(result.Results)

	return result, nil
}

// ExecuteParallel runs tool calls concurrently with bounded parallelism.
//
// Description:
//
//	Executes tool calls concurrently up to maxConcurrent at a time.
//	Results are returned in the same order as input calls.
//	This is useful when tools are independent and can run simultaneously.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout
//	calls - The tool calls to execute
//	maxConcurrent - Maximum concurrent executions (0 = NumCPU)
//
// Outputs:
//
//	*DispatchResult - Execution results in input order
//	error - Non-nil only for catastrophic failures
//
// Thread Safety: This method is safe for concurrent use.
func (d *Dispatcher) ExecuteParallel(ctx context.Context, calls []ToolCall, maxConcurrent int) (*DispatchResult, error) {
	if ctx == nil {
		return nil, errors.New("ctx must not be nil")
	}

	if len(calls) == 0 {
		return &DispatchResult{}, nil
	}

	// Check limits
	if len(calls) > MaxToolCallsPerRequest {
		return nil, fmt.Errorf("too many tool calls: %d (max %d)", len(calls), MaxToolCallsPerRequest)
	}

	if maxConcurrent <= 0 {
		maxConcurrent = runtime.NumCPU()
	}

	start := time.Now()

	// Pre-allocate results slice to maintain order
	results := make([]ExecutionResult, len(calls))
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrent)
	var failCount, approvedCount, declinedCount int32
	var countMu sync.Mutex

execLoop:
	for i, call := range calls {
		// Check for context cancellation before starting
		if ctx.Err() != nil {
			break execLoop
		}

		// Acquire semaphore
		select {
		case <-ctx.Done():
			break execLoop
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(idx int, c ToolCall) {
			defer wg.Done()
			defer func() { <-sem }()

			execResult := d.executeSingle(ctx, c)
			results[idx] = execResult

			countMu.Lock()
			if execResult.Result != nil && !execResult.Result.Success {
				failCount++
			}
			if execResult.Approved {
				approvedCount++
			}
			if execResult.Declined {
				declinedCount++
			}
			countMu.Unlock()
		}(i, call)
	}

	wg.Wait()

	result := &DispatchResult{
		Results:       results,
		TotalDuration: time.Since(start),
		FailedCount:   int(failCount),
		ApprovedCount: int(approvedCount),
		DeclinedCount: int(declinedCount),
	}
	result.FormattedOutput = d.formatter.Format(results)

	return result, nil
}

// executeSingle handles a single tool call.
func (d *Dispatcher) executeSingle(ctx context.Context, call ToolCall) ExecutionResult {
	result := ExecutionResult{
		Call: call,
	}

	// Get tool definition
	tool, ok := d.registry.Get(call.Name)
	if !ok {
		result.Result = &Result{
			Success: false,
			Error:   fmt.Sprintf("unknown tool: %s", call.Name),
		}
		return result
	}

	// Parse parameters
	params, err := call.ParamsMap()
	if err != nil {
		result.Result = &Result{
			Success: false,
			Error:   err.Error(),
		}
		return result
	}

	// Check parameter size
	if len(call.Params) > MaxParamsSize {
		result.Result = &Result{
			Success: false,
			Error:   fmt.Sprintf("parameters too large: %d bytes (max %d)", len(call.Params), MaxParamsSize),
		}
		return result
	}

	// Check approval if required
	def := tool.Definition()
	approver := d.getApprover()
	if def.SideEffects && approver != nil {
		approved, err := approver(&def, params)
		if err != nil {
			result.Result = &Result{
				Success: false,
				Error:   fmt.Sprintf("approval check failed: %v", err),
			}
			return result
		}
		if !approved {
			result.Result = &Result{
				Success: false,
				Error:   "user declined",
			}
			result.Declined = true
			return result
		}
		result.Approved = true
	}

	// Create invocation
	invocation := &Invocation{
		ID:         call.ID,
		ToolName:   call.Name,
		Parameters: params,
	}

	// Execute with timeout
	execCtx := ctx
	if d.timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, d.timeout)
		defer cancel()
	}

	execResult, err := d.executor.Execute(execCtx, invocation)
	if err != nil {
		result.Result = &Result{
			Success: false,
			Error:   err.Error(),
		}

		// Add recovery suggestion
		suggestion := d.recovery.SuggestFix(err, call)
		if suggestion != "" {
			result.Result.Error += "\n\nSuggestion: " + suggestion
		}

		return result
	}

	result.Result = execResult
	return result
}

// DispatchWithRetry executes tool calls with retry logic for transient failures.
//
// Description:
//
//	Wraps Execute with retry logic for tools that fail with retryable errors.
//	Uses exponential backoff between retries.
//
// Inputs:
//
//	ctx - Context for cancellation and timeout
//	calls - The tool calls to execute
//	maxRetries - Maximum retries per tool (0 = no retries)
//
// Outputs:
//
//	*DispatchResult - Final execution results
//	error - Non-nil only for catastrophic failures
//
// Thread Safety: This method is safe for concurrent use.
func (d *Dispatcher) DispatchWithRetry(ctx context.Context, calls []ToolCall, maxRetries int) (*DispatchResult, error) {
	if ctx == nil {
		return nil, errors.New("ctx must not be nil")
	}

	if maxRetries < 0 {
		maxRetries = 0
	}

	start := time.Now()
	result := &DispatchResult{
		Results: make([]ExecutionResult, 0, len(calls)),
	}

	for _, call := range calls {
		if ctx.Err() != nil {
			break
		}

		var execResult ExecutionResult
		for attempt := 0; attempt <= maxRetries; attempt++ {
			execResult = d.executeSingle(ctx, call)

			// Check if we should retry
			if execResult.Result == nil || execResult.Result.Success {
				break
			}

			// Check if error is retryable
			analysis := d.recovery.Analyze(errors.New(execResult.Result.Error), call)
			if analysis == nil || !analysis.Retryable {
				break
			}

			// Exponential backoff
			if attempt < maxRetries {
				backoff := time.Duration(1<<uint(attempt)) * 100 * time.Millisecond
				select {
				case <-ctx.Done():
					break
				case <-time.After(backoff):
				}
			}
		}

		result.Results = append(result.Results, execResult)
		if execResult.Result != nil && !execResult.Result.Success {
			result.FailedCount++
		}
		if execResult.Approved {
			result.ApprovedCount++
		}
		if execResult.Declined {
			result.DeclinedCount++
		}
	}

	result.TotalDuration = time.Since(start)
	result.FormattedOutput = d.formatter.Format(result.Results)

	return result, nil
}

// SanitizeParams removes or masks sensitive fields before logging.
//
// Description:
//
//	Sanitizes parameters to prevent credential leaks in logs.
//	Must be called before any structured logging of tool parameters.
//
// Inputs:
//
//	toolName - The tool name
//	params - The raw parameter JSON
//
// Outputs:
//
//	json.RawMessage - Sanitized parameters safe for logging
//
// Thread Safety: This function is safe for concurrent use.
func SanitizeParams(toolName string, params json.RawMessage) json.RawMessage {
	sensitiveFields := map[string][]string{
		"bash_execute": {"command"},     // May contain secrets in env vars
		"write_file":   {"content"},     // May contain credentials
		"edit_file":    {"new_content"}, // May contain credentials
	}

	fields, ok := sensitiveFields[toolName]
	if !ok {
		return params
	}

	var data map[string]any
	if err := json.Unmarshal(params, &data); err != nil {
		return json.RawMessage(`{"error": "sanitization failed"}`)
	}

	for _, field := range fields {
		if _, exists := data[field]; exists {
			data[field] = "[REDACTED]"
		}
	}

	sanitized, err := json.Marshal(data)
	if err != nil {
		return json.RawMessage(`{"error": "sanitization failed"}`)
	}
	return sanitized
}

// GenerateToolsPrompt generates a system prompt section describing available tools.
//
// Description:
//
//	Creates a formatted description of all available tools for inclusion
//	in the LLM system prompt.
//
// Inputs:
//
//	enabledCategories - Categories to include (empty = all)
//	disabledTools - Specific tools to exclude
//
// Outputs:
//
//	string - Formatted tool descriptions
//
// Thread Safety: This method is safe for concurrent use.
func (d *Dispatcher) GenerateToolsPrompt(enabledCategories []string, disabledTools []string) string {
	definitions := d.registry.GetDefinitionsFiltered(enabledCategories, disabledTools)
	return d.formatter.FormatToolList(definitions)
}

// GetAvailableToolNames returns the names of tools that can be executed.
//
// Description:
//
//	Returns tool names filtered by category and excluding disabled tools.
//	Results are sorted alphabetically.
//
// Inputs:
//
//	enabledCategories - Categories to include (empty = all)
//	disabledTools - Specific tools to exclude
//
// Outputs:
//
//	[]string - Sorted list of available tool names
//
// Thread Safety: This method is safe for concurrent use.
func (d *Dispatcher) GetAvailableToolNames(enabledCategories []string, disabledTools []string) []string {
	tools := d.registry.GetEnabled(enabledCategories, disabledTools)
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name()
	}
	sort.Strings(names)
	return names
}

// SetApprover sets or replaces the approval function.
//
// Thread Safety: This method is safe for concurrent use.
func (d *Dispatcher) SetApprover(fn ApprovalFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.approver = fn
}

// getApprover returns the current approval function.
//
// Thread Safety: This method is safe for concurrent use.
func (d *Dispatcher) getApprover() ApprovalFunc {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.approver
}

// GetParser returns the dispatcher's parser.
//
// Outputs:
//
//	*Parser - The parser instance
//
// Thread Safety: This method is safe for concurrent use.
func (d *Dispatcher) GetParser() *Parser {
	return d.parser
}

// GetFormatter returns the dispatcher's formatter.
//
// Outputs:
//
//	*Formatter - The formatter instance
//
// Thread Safety: This method is safe for concurrent use.
func (d *Dispatcher) GetFormatter() *Formatter {
	return d.formatter
}

// GetRecovery returns the dispatcher's error recovery helper.
//
// Outputs:
//
//	*ErrorRecovery - The error recovery helper
//
// Thread Safety: This method is safe for concurrent use.
func (d *Dispatcher) GetRecovery() *ErrorRecovery {
	return d.recovery
}

// ToolCallFromInvocation converts an Invocation to a ToolCall.
//
// Description:
//
//	Converts the internal Invocation type to a ToolCall suitable for
//	use with the dispatcher.
//
// Inputs:
//
//	inv - The invocation to convert. Must not be nil.
//
// Outputs:
//
//	ToolCall - The converted tool call
//	error - Non-nil if parameter marshaling fails
//
// Thread Safety: This function is safe for concurrent use.
func ToolCallFromInvocation(inv *Invocation) (ToolCall, error) {
	params, err := json.Marshal(inv.Parameters)
	if err != nil {
		return ToolCall{}, fmt.Errorf("marshaling parameters: %w", err)
	}

	return ToolCall{
		ID:     inv.ID,
		Name:   inv.ToolName,
		Params: params,
	}, nil
}

// FilterToolCalls filters tool calls by name prefix or category.
//
// Description:
//
//	Useful for routing different types of tool calls to different handlers.
//
// Inputs:
//
//	calls - Tool calls to filter
//	filter - Function that returns true for calls to keep
//
// Outputs:
//
//	[]ToolCall - Filtered tool calls (may be empty)
//
// Thread Safety: This function is safe for concurrent use.
func FilterToolCalls(calls []ToolCall, filter func(ToolCall) bool) []ToolCall {
	result := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		if filter(call) {
			result = append(result, call)
		}
	}
	return result
}

// GroupToolCallsByCategory groups tool calls by their tool's category.
//
// Description:
//
//	Groups tool calls by the category of the tool they invoke.
//	Calls for unknown tools are silently skipped.
//
// Inputs:
//
//	calls - Tool calls to group
//
// Outputs:
//
//	map[ToolCategory][]ToolCall - Grouped tool calls
//
// Thread Safety: This method is safe for concurrent use.
func (d *Dispatcher) GroupToolCallsByCategory(calls []ToolCall) map[ToolCategory][]ToolCall {
	result := make(map[ToolCategory][]ToolCall)

	for _, call := range calls {
		tool, ok := d.registry.Get(call.Name)
		if !ok {
			continue
		}
		category := tool.Category()
		result[category] = append(result[category], call)
	}

	return result
}

// ToolExists checks if a tool is registered.
//
// Inputs:
//
//	name - The tool name to check
//
// Outputs:
//
//	bool - True if the tool exists
//
// Thread Safety: This method is safe for concurrent use.
func (d *Dispatcher) ToolExists(name string) bool {
	_, ok := d.registry.Get(name)
	return ok
}

// MergeResults combines multiple DispatchResults into one.
//
// Description:
//
//	Merges results from multiple dispatch operations. Useful when
//	executing tool calls in batches or from multiple sources.
//	RemainingText takes the first non-empty value found.
//
// Inputs:
//
//	results - The results to merge. Nil results are skipped.
//
// Outputs:
//
//	*DispatchResult - Combined results
//
// Thread Safety: This function is safe for concurrent use.
func MergeResults(results ...*DispatchResult) *DispatchResult {
	merged := &DispatchResult{
		Results: make([]ExecutionResult, 0),
	}

	var formattedParts []string

	for _, r := range results {
		if r == nil {
			continue
		}
		merged.Results = append(merged.Results, r.Results...)
		merged.TotalDuration += r.TotalDuration
		merged.FailedCount += r.FailedCount
		merged.ApprovedCount += r.ApprovedCount
		merged.DeclinedCount += r.DeclinedCount

		if r.FormattedOutput != "" {
			formattedParts = append(formattedParts, r.FormattedOutput)
		}
		if r.RemainingText != "" && merged.RemainingText == "" {
			merged.RemainingText = r.RemainingText
		}
	}

	merged.FormattedOutput = strings.Join(formattedParts, "\n\n")
	return merged
}
