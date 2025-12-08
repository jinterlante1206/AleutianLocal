package datatypes

// AgentStepRequest is the payload the CLI sends to the Orchestrator/Python container.
// It includes the full history so that the container remains stateless.
type AgentStepRequest struct {
	Query   string         `json:"query"`
	History []AgentMessage `json:"history"`
}

// AgentMessage represents a single turn in the conversation history.
// This mirrors the standard OpenAI/Anthropic message format.
type AgentMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallId string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall represents the LLM's request to execute a function.
type ToolCall struct {
	Id       string       `json:"id"`
	Type     string       `json:"type"` // usually "function"
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` //JSON string of args e.g. '{"path": "main.go"}'
}

// AgentStepResponse is the decision the Agent makes.
// It tells the CLI to either "stop and print" (Type="answer") or "do work" (Type="tool_call").
type AgentStepResponse struct {
	Type    string `json:"type"`              // "answer" or "tool_call"
	Content string `json:"content,omitempty"` // The final answer (if Type="answer")

	// Fields populated if Type="tool_call"
	ToolName string                 `json:"tool,omitempty"`
	ToolArgs map[string]interface{} `json:"args,omitempty"`
	ToolID   string                 `json:"tool_id,omitempty"` // Must be sent back in the next request
}
