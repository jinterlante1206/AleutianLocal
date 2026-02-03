// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// MockClient is a mock LLM client for testing.
//
// Thread Safety:
//
//	MockClient is safe for concurrent use.
type MockClient struct {
	mu sync.RWMutex

	// name is the provider name.
	name string

	// model is the model name.
	model string

	// responses are queued responses to return.
	responses []*Response

	// defaultResponse is returned when no queued responses remain.
	defaultResponse *Response

	// calls records all calls made to Complete.
	calls []CompletionCall

	// responseFunc allows dynamic response generation.
	responseFunc func(*Request) (*Response, error)

	// delay adds artificial latency to responses.
	delay time.Duration

	// errorToReturn causes Complete to return this error.
	errorToReturn error
}

// CompletionCall records a call to Complete.
type CompletionCall struct {
	Request   *Request
	Timestamp time.Time
}

// NewMockClient creates a new mock LLM client.
func NewMockClient() *MockClient {
	return &MockClient{
		name:  "mock",
		model: "mock-model",
		defaultResponse: &Response{
			Content:      "Mock response",
			StopReason:   "end",
			TokensUsed:   100,
			InputTokens:  50,
			OutputTokens: 50,
		},
		calls: make([]CompletionCall, 0),
	}
}

// WithName sets the provider name.
func (c *MockClient) WithName(name string) *MockClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.name = name
	return c
}

// WithModel sets the model name.
func (c *MockClient) WithModel(model string) *MockClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.model = model
	return c
}

// WithDelay adds artificial latency.
func (c *MockClient) WithDelay(d time.Duration) *MockClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.delay = d
	return c
}

// WithError configures the client to return an error.
func (c *MockClient) WithError(err error) *MockClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errorToReturn = err
	return c
}

// WithResponseFunc sets a dynamic response function.
func (c *MockClient) WithResponseFunc(f func(*Request) (*Response, error)) *MockClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responseFunc = f
	return c
}

// QueueResponse adds a response to the queue.
func (c *MockClient) QueueResponse(response *Response) *MockClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responses = append(c.responses, response)
	return c
}

// QueueToolCall queues a response that invokes a tool.
func (c *MockClient) QueueToolCall(toolName string, arguments map[string]any) *MockClient {
	argsJSON, _ := json.Marshal(arguments)

	response := &Response{
		Content:    "",
		StopReason: "tool_use",
		ToolCalls: []ToolCall{{
			ID:        fmt.Sprintf("call_%d", len(c.responses)),
			Name:      toolName,
			Arguments: string(argsJSON),
		}},
		TokensUsed:   100,
		InputTokens:  50,
		OutputTokens: 50,
	}

	return c.QueueResponse(response)
}

// QueueFinalResponse queues a final response with no tool calls.
func (c *MockClient) QueueFinalResponse(content string) *MockClient {
	return c.QueueResponse(&Response{
		Content:      content,
		StopReason:   "end",
		TokensUsed:   100 + len(content)/4,
		InputTokens:  50,
		OutputTokens: 50 + len(content)/4,
	})
}

// SetDefaultResponse sets the response to return when queue is empty.
func (c *MockClient) SetDefaultResponse(response *Response) *MockClient {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.defaultResponse = response
	return c
}

// Complete implements the Client interface.
func (c *MockClient) Complete(ctx context.Context, request *Request) (*Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Record the call
	c.calls = append(c.calls, CompletionCall{
		Request:   request,
		Timestamp: time.Now(),
	})

	// Apply delay
	if c.delay > 0 {
		time.Sleep(c.delay)
	}

	// Check for context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Return error if configured
	if c.errorToReturn != nil {
		return nil, c.errorToReturn
	}

	// Use response function if configured
	if c.responseFunc != nil {
		return c.responseFunc(request)
	}

	// Return queued response if available
	if len(c.responses) > 0 {
		response := c.responses[0]
		c.responses = c.responses[1:]
		response.Duration = c.delay
		response.Model = c.model
		return response, nil
	}

	// Return default response
	response := *c.defaultResponse
	response.Duration = c.delay
	response.Model = c.model
	return &response, nil
}

// Name implements the Client interface.
func (c *MockClient) Name() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.name
}

// Model implements the Client interface.
func (c *MockClient) Model() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.model
}

// GetCalls returns all recorded calls.
func (c *MockClient) GetCalls() []CompletionCall {
	c.mu.RLock()
	defer c.mu.RUnlock()

	calls := make([]CompletionCall, len(c.calls))
	copy(calls, c.calls)
	return calls
}

// CallCount returns the number of calls made.
func (c *MockClient) CallCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.calls)
}

// LastRequest returns the most recent request.
func (c *MockClient) LastRequest() *Request {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.calls) == 0 {
		return nil
	}
	return c.calls[len(c.calls)-1].Request
}

// Reset clears all state.
func (c *MockClient) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.responses = nil
	c.calls = make([]CompletionCall, 0)
	c.errorToReturn = nil
	c.responseFunc = nil
	c.delay = 0
}

// Verify ensures all queued responses were consumed.
func (c *MockClient) Verify() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.responses) > 0 {
		return fmt.Errorf("mock: %d queued responses not consumed", len(c.responses))
	}
	return nil
}

// ExpectCall returns an error if the expected number of calls wasn't made.
func (c *MockClient) ExpectCall(count int) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.calls) != count {
		return fmt.Errorf("mock: expected %d calls, got %d", count, len(c.calls))
	}
	return nil
}
