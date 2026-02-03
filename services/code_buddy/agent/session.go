// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package agent

import (
	"fmt"
	"sync"
	"time"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/agent/mcts/crs"
	"github.com/google/uuid"
)

// MetricField represents a session metric field for type-safe increments.
type MetricField string

const (
	// MetricSteps is the total steps metric.
	MetricSteps MetricField = "steps"

	// MetricTokens is the total tokens metric.
	MetricTokens MetricField = "tokens"

	// MetricToolCalls is the tool calls metric.
	MetricToolCalls MetricField = "tool_calls"

	// MetricToolErrors is the tool errors metric.
	MetricToolErrors MetricField = "tool_errors"

	// MetricLLMCalls is the LLM calls metric.
	MetricLLMCalls MetricField = "llm_calls"

	// MetricCacheHits is the cache hits metric.
	MetricCacheHits MetricField = "cache_hits"

	// MetricGroundingRetries is the grounding validation retry count.
	MetricGroundingRetries MetricField = "grounding_retries"

	// MetricToolForcingRetries is the tool forcing retry count.
	MetricToolForcingRetries MetricField = "tool_forcing_retries"
)

// ValidContextEvictionPolicies contains valid eviction policy values.
var ValidContextEvictionPolicies = []string{"lru", "relevance", "hybrid"}

// ValidSafetyCheckScopes contains valid safety check scope values.
var ValidSafetyCheckScopes = []string{"changed_files", "blast_radius", "full"}

// ValidDegradationModes contains valid degradation mode values.
var ValidDegradationModes = []string{"fallback", "fail", "ask"}

// SessionConfig holds all tunable parameters for a session.
//
// Thread Safety:
//
//	SessionConfig is immutable after creation. Modifications require
//	creating a new config.
type SessionConfig struct {
	// MaxSteps is the maximum number of agent steps allowed.
	// Default: 50
	MaxSteps int `json:"max_steps"`

	// MaxTokensPerStep is the maximum tokens per LLM call.
	// Default: 4000
	MaxTokensPerStep int `json:"max_tokens_per_step"`

	// MaxTotalTokens is the total token budget for the session.
	// Default: 100000
	MaxTotalTokens int `json:"max_total_tokens"`

	// MaxToolCallsPerStep limits tool calls in a single step.
	// Default: 5
	MaxToolCallsPerStep int `json:"max_tool_calls_per_step"`

	// StepTimeout is the maximum duration for a single step.
	// Default: 30s
	StepTimeout time.Duration `json:"step_timeout"`

	// TotalTimeout is the maximum duration for the entire session.
	// Default: 10m
	TotalTimeout time.Duration `json:"total_timeout"`

	// InitialContextBudget is the token budget for initial context assembly.
	// Default: 8000
	InitialContextBudget int `json:"initial_context_budget"`

	// ContextEvictionPolicy determines how context is evicted when over budget.
	// Options: "lru", "relevance", "hybrid"
	// Default: "hybrid"
	ContextEvictionPolicy string `json:"context_eviction_policy"`

	// SummarizationThreshold is tokens before summarizing context.
	// Default: 50000
	SummarizationThreshold int `json:"summarization_threshold"`

	// RequireSafetyCheck enables safety checks before code changes.
	// Default: true
	RequireSafetyCheck bool `json:"require_safety_check"`

	// SafetyCheckScope determines what to analyze for safety.
	// Options: "changed_files", "blast_radius", "full"
	// Default: "blast_radius"
	SafetyCheckScope string `json:"safety_check_scope"`

	// BlockOnCritical blocks operations with critical security issues.
	// Default: true
	BlockOnCritical bool `json:"block_on_critical"`

	// EnabledToolSets specifies which tool categories are enabled.
	// Options: "exploration", "reasoning", "safety", "file"
	// Default: ["exploration", "reasoning", "safety", "file"]
	EnabledToolSets []string `json:"enabled_tool_sets"`

	// DisabledTools lists specific tools to disable.
	DisabledTools []string `json:"disabled_tools"`

	// ToolPriorities assigns priority weights to tools (higher = prefer).
	ToolPriorities map[string]int `json:"tool_priorities"`

	// ReflectionThreshold is steps before triggering reflection.
	// Default: 10
	ReflectionThreshold int `json:"reflection_threshold"`

	// ConfidenceThreshold triggers reflection below this confidence.
	// Default: 0.7
	ConfidenceThreshold float64 `json:"confidence_threshold"`

	// DegradationMode determines behavior when services unavailable.
	// Options: "fallback", "fail", "ask"
	// Default: "fallback"
	DegradationMode string `json:"degradation_mode"`

	// ToolRouterEnabled enables the micro-LLM tool router for faster tool selection.
	// When enabled, a fast model (like granite4:micro-h) pre-selects tools before
	// the main reasoning LLM, reducing latency.
	// Default: false (opt-in feature)
	ToolRouterEnabled bool `json:"tool_router_enabled"`

	// ToolRouterModel is the Ollama model to use for tool routing.
	// Should be a small, fast model like "granite4:micro-h" or "granite4:3b-h".
	// Default: "granite4:micro-h"
	ToolRouterModel string `json:"tool_router_model"`

	// ToolRouterTimeout is the maximum time for a routing decision.
	// If exceeded, falls back to main LLM tool selection.
	// Default: 500ms
	ToolRouterTimeout time.Duration `json:"tool_router_timeout"`

	// ToolRouterConfidence is the minimum confidence for accepting a routing decision.
	// Below this threshold, falls back to main LLM tool selection.
	// Default: 0.7
	ToolRouterConfidence float64 `json:"tool_router_confidence"`
}

// DefaultSessionConfig returns production-ready default configuration.
//
// Description:
//
//	Returns a SessionConfig with sensible defaults suitable for most
//	use cases. Callers can modify specific fields as needed.
//
// Outputs:
//
//	*SessionConfig - Configuration with default values
func DefaultSessionConfig() *SessionConfig {
	return &SessionConfig{
		MaxSteps:               50,
		MaxTokensPerStep:       4000,
		MaxTotalTokens:         100000,
		MaxToolCallsPerStep:    5,
		StepTimeout:            30 * time.Second,
		TotalTimeout:           10 * time.Minute,
		InitialContextBudget:   8000,
		ContextEvictionPolicy:  "hybrid",
		SummarizationThreshold: 50000,
		RequireSafetyCheck:     true,
		SafetyCheckScope:       "blast_radius",
		BlockOnCritical:        true,
		EnabledToolSets:        []string{"exploration", "reasoning", "safety", "file"},
		DisabledTools:          []string{},
		ToolPriorities:         make(map[string]int),
		ReflectionThreshold:    10,
		ConfidenceThreshold:    0.7,
		DegradationMode:        "fallback",
		// Tool Router defaults (enabled by default for faster tool selection)
		ToolRouterEnabled:    true,
		ToolRouterModel:      "granite4:micro-h",
		ToolRouterTimeout:    500 * time.Millisecond,
		ToolRouterConfidence: 0.7,
	}
}

// Validate checks that the configuration is valid.
//
// Description:
//
//	Validates all configuration fields including numeric limits and string enums.
//	Returns ErrInvalidSession with details if validation fails.
//
// Outputs:
//
//	error - Non-nil if configuration is invalid, contains validation details
func (c *SessionConfig) Validate() error {
	if c.MaxSteps <= 0 {
		return fmt.Errorf("%w: MaxSteps must be positive", ErrInvalidSession)
	}
	if c.MaxTokensPerStep <= 0 {
		return fmt.Errorf("%w: MaxTokensPerStep must be positive", ErrInvalidSession)
	}
	if c.MaxTotalTokens <= 0 {
		return fmt.Errorf("%w: MaxTotalTokens must be positive", ErrInvalidSession)
	}
	if c.StepTimeout <= 0 {
		return fmt.Errorf("%w: StepTimeout must be positive", ErrInvalidSession)
	}
	if c.TotalTimeout <= 0 {
		return fmt.Errorf("%w: TotalTimeout must be positive", ErrInvalidSession)
	}
	if c.InitialContextBudget <= 0 {
		return fmt.Errorf("%w: InitialContextBudget must be positive", ErrInvalidSession)
	}
	if c.ConfidenceThreshold < 0 || c.ConfidenceThreshold > 1 {
		return fmt.Errorf("%w: ConfidenceThreshold must be between 0 and 1", ErrInvalidSession)
	}

	// Validate string enums
	if c.ContextEvictionPolicy != "" && !isValidEnum(c.ContextEvictionPolicy, ValidContextEvictionPolicies) {
		return fmt.Errorf("%w: ContextEvictionPolicy must be one of %v", ErrInvalidSession, ValidContextEvictionPolicies)
	}
	if c.SafetyCheckScope != "" && !isValidEnum(c.SafetyCheckScope, ValidSafetyCheckScopes) {
		return fmt.Errorf("%w: SafetyCheckScope must be one of %v", ErrInvalidSession, ValidSafetyCheckScopes)
	}
	if c.DegradationMode != "" && !isValidEnum(c.DegradationMode, ValidDegradationModes) {
		return fmt.Errorf("%w: DegradationMode must be one of %v", ErrInvalidSession, ValidDegradationModes)
	}

	// Validate tool router configuration
	if c.ToolRouterEnabled {
		if c.ToolRouterModel == "" {
			return fmt.Errorf("%w: ToolRouterModel must be set when ToolRouterEnabled is true", ErrInvalidSession)
		}
		if c.ToolRouterTimeout <= 0 {
			return fmt.Errorf("%w: ToolRouterTimeout must be positive", ErrInvalidSession)
		}
		if c.ToolRouterConfidence < 0 || c.ToolRouterConfidence > 1 {
			return fmt.Errorf("%w: ToolRouterConfidence must be between 0 and 1", ErrInvalidSession)
		}
	}

	return nil
}

// isValidEnum checks if a value is in the allowed list.
func isValidEnum(value string, allowed []string) bool {
	for _, v := range allowed {
		if value == v {
			return true
		}
	}
	return false
}

// Session represents an agent session with all state.
//
// Thread Safety:
//
//	Session uses internal synchronization for state access.
//	Multiple goroutines can safely read session state.
type Session struct {
	mu sync.RWMutex

	// ID is the unique session identifier.
	ID string `json:"id"`

	// ProjectRoot is the absolute path to the project.
	ProjectRoot string `json:"project_root"`

	// GraphID is the Code Buddy graph ID (set after init).
	GraphID string `json:"graph_id,omitempty"`

	// State is the current agent state.
	State AgentState `json:"state"`

	// Config holds the session configuration.
	Config *SessionConfig `json:"config"`

	// History records all execution steps.
	History []HistoryEntry `json:"history"`

	// Metrics tracks session metrics.
	Metrics *SessionMetrics `json:"metrics"`

	// CurrentContext holds the current assembled context.
	CurrentContext *AssembledContext `json:"-"`

	// LastQuery is the most recent user query.
	LastQuery string `json:"last_query,omitempty"`

	// LastIntent is the classified intent of the last query.
	LastIntent *QueryIntent `json:"last_intent,omitempty"`

	// CreatedAt is when the session was created.
	CreatedAt time.Time `json:"created_at"`

	// LastActiveAt is when the session was last active.
	LastActiveAt time.Time `json:"last_active_at"`

	// inProgress indicates if an operation is currently running.
	inProgress bool

	// CRS is the Code Reasoning State for MCTS-based reasoning.
	// Optional - nil when MCTS is not enabled.
	CRS crs.CRS `json:"-"`

	// traceRecorder captures reasoning steps for audit and debugging.
	// Optional - nil when trace recording is not enabled.
	traceRecorder *crs.TraceRecorder

	// crsSerializer converts CRS snapshots to exportable formats.
	// Lazily initialized when needed.
	crsSerializer *crs.Serializer

	// cachedSummary holds the cached reasoning summary.
	// Invalidated when CRS changes.
	cachedSummary *crs.ReasoningSummary

	// toolRouter is the micro-LLM tool router for fast tool selection.
	// Optional - nil when tool routing is not enabled.
	toolRouter ToolRouter

	// modelManager coordinates multiple Ollama models to prevent thrashing.
	// Shared between tool router and main LLM.
	modelManager ModelManager

	// recentToolErrors tracks tools that failed recently.
	// Fed back to the tool router to avoid suggesting the same tool.
	recentToolErrors []ToolRouterError
}

// AssembledContext represents the current context window for the LLM.
type AssembledContext struct {
	// SystemPrompt is the system instructions.
	SystemPrompt string `json:"system_prompt"`

	// CodeContext contains code snippets.
	CodeContext []CodeEntry `json:"code_context"`

	// LibraryDocs contains library documentation.
	LibraryDocs []DocEntry `json:"library_docs"`

	// ToolResults contains recent tool outputs.
	ToolResults []ToolResult `json:"tool_results"`

	// ConversationHistory contains message history.
	ConversationHistory []Message `json:"conversation_history"`

	// TotalTokens is the current token count.
	TotalTokens int `json:"total_tokens"`

	// Relevance maps entry IDs to relevance scores.
	Relevance map[string]float64 `json:"relevance"`
}

// GetRelevance returns the relevance score for an entry ID.
// Returns 0.0 if the entry ID doesn't exist or if Relevance map is nil.
//
// Thread Safety: This method is NOT safe for concurrent use.
func (c *AssembledContext) GetRelevance(entryID string) float64 {
	if c.Relevance == nil {
		return 0.0
	}
	return c.Relevance[entryID]
}

// SetRelevance sets the relevance score for an entry ID.
// Initializes the Relevance map if nil (CT-001 fix).
//
// Thread Safety: This method is NOT safe for concurrent use.
func (c *AssembledContext) SetRelevance(entryID string, score float64) {
	if c.Relevance == nil {
		c.Relevance = make(map[string]float64)
	}
	c.Relevance[entryID] = score
}

// EnsureInitialized ensures all slice/map fields are non-nil.
// Call this after unmarshalling or constructing AssembledContext.
//
// Thread Safety: This method is NOT safe for concurrent use.
func (c *AssembledContext) EnsureInitialized() {
	if c.CodeContext == nil {
		c.CodeContext = []CodeEntry{}
	}
	if c.LibraryDocs == nil {
		c.LibraryDocs = []DocEntry{}
	}
	if c.ToolResults == nil {
		c.ToolResults = []ToolResult{}
	}
	if c.ConversationHistory == nil {
		c.ConversationHistory = []Message{}
	}
	if c.Relevance == nil {
		c.Relevance = make(map[string]float64)
	}
}

// CodeEntry represents a code snippet in context.
type CodeEntry struct {
	// ID is a unique identifier for this entry.
	ID string `json:"id"`

	// FilePath is the relative path to the file.
	FilePath string `json:"file_path"`

	// SymbolName is the symbol name if applicable.
	SymbolName string `json:"symbol_name,omitempty"`

	// Content is the code content.
	Content string `json:"content"`

	// Tokens is the estimated token count.
	Tokens int `json:"tokens"`

	// Relevance is the relevance score (0.0-1.0).
	Relevance float64 `json:"relevance"`

	// AddedAt is the step when this was added.
	AddedAt int `json:"added_at"`

	// Reason explains why this was included.
	Reason string `json:"reason"`
}

// DocEntry represents a library documentation entry.
type DocEntry struct {
	// ID is a unique identifier.
	ID string `json:"id"`

	// Library is the library name.
	Library string `json:"library"`

	// Symbol is the symbol path.
	Symbol string `json:"symbol"`

	// Content is the documentation content.
	Content string `json:"content"`

	// Tokens is the estimated token count.
	Tokens int `json:"tokens"`
}

// Message represents a conversation message.
type Message struct {
	// Role is "user", "assistant", or "system".
	Role string `json:"role"`

	// Content is the message content.
	Content string `json:"content"`

	// ToolCalls contains any tool calls in this message.
	ToolCalls []ToolInvocation `json:"tool_calls,omitempty"`
}

// NewSession creates a new agent session.
//
// Description:
//
//	Creates a session with the given project root and configuration.
//	The session starts in IDLE state with empty history.
//
// Inputs:
//
//	projectRoot - Absolute path to the project root
//	config - Session configuration (uses defaults if nil)
//
// Outputs:
//
//	*Session - The new session
//	error - Non-nil if configuration is invalid
//
// Example:
//
//	session, err := NewSession("/path/to/project", nil)
//	if err != nil {
//	    return fmt.Errorf("create session: %w", err)
//	}
func NewSession(projectRoot string, config *SessionConfig) (*Session, error) {
	// Validate projectRoot
	if projectRoot == "" {
		return nil, fmt.Errorf("%w: projectRoot must not be empty", ErrInvalidSession)
	}

	if config == nil {
		config = DefaultSessionConfig()
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	now := time.Now()
	return &Session{
		ID:            uuid.NewString(),
		ProjectRoot:   projectRoot,
		State:         StateIdle,
		Config:        config,
		History:       make([]HistoryEntry, 0),
		Metrics:       &SessionMetrics{},
		CreatedAt:     now,
		LastActiveAt:  now,
		traceRecorder: crs.NewTraceRecorder(crs.DefaultTraceConfig()),
	}, nil
}

// GetState returns the current session state.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) GetState() AgentState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State
}

// SetState updates the session state.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) SetState(state AgentState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = state
	s.LastActiveAt = time.Now()
}

// GetGraphID returns the graph ID if set.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) GetGraphID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.GraphID
}

// SetGraphID sets the graph ID.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) SetGraphID(graphID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.GraphID = graphID
	s.LastActiveAt = time.Now()
}

// AddHistoryEntry appends a history entry.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) AddHistoryEntry(entry HistoryEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry.Step = len(s.History)
	entry.State = s.State
	entry.Timestamp = time.Now()
	s.History = append(s.History, entry)
	s.LastActiveAt = time.Now()
}

// GetHistory returns a copy of the history.
//
// Description:
//
//	Returns a shallow copy of the history slice. The slice itself is copied
//	but HistoryEntry structs are value types so modifications to the returned
//	slice won't affect the session's internal history.
//
// Outputs:
//
//	[]HistoryEntry - Copy of the session history
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) GetHistory() []HistoryEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	history := make([]HistoryEntry, len(s.History))
	copy(history, s.History)
	return history
}

// IncrementMetric increments a session metric.
//
// Description:
//
//	Increments the specified metric by the given value. Use the
//	MetricField constants (MetricSteps, MetricTokens, etc.) for type safety.
//
// Inputs:
//
//	field - The metric field to increment (use MetricField constants)
//	value - The amount to add
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) IncrementMetric(field MetricField, value int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch field {
	case MetricSteps:
		s.Metrics.TotalSteps += value
	case MetricTokens:
		s.Metrics.TotalTokens += value
	case MetricToolCalls:
		s.Metrics.ToolCalls += value
	case MetricToolErrors:
		s.Metrics.ToolErrors += value
	case MetricLLMCalls:
		s.Metrics.LLMCalls += value
	case MetricCacheHits:
		s.Metrics.CacheHits += value
	case MetricGroundingRetries:
		s.Metrics.GroundingRetries += value
	case MetricToolForcingRetries:
		s.Metrics.ToolForcingRetries += value
	}
	s.LastActiveAt = time.Now()
}

// SetDegradedMode sets the degraded mode flag.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) SetDegradedMode(degraded bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Metrics.DegradedMode = degraded
	s.LastActiveAt = time.Now()
}

// TryAcquire attempts to acquire the session for an operation.
//
// Returns false if another operation is in progress.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) TryAcquire() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inProgress {
		return false
	}
	s.inProgress = true
	s.LastActiveAt = time.Now()
	return true
}

// Release releases the session after an operation.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inProgress = false
	s.LastActiveAt = time.Now()
}

// IsTerminated returns true if the session is in a terminal state.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) IsTerminated() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.State.IsTerminal()
}

// GetCurrentContext returns the current assembled context.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) GetCurrentContext() *AssembledContext {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.CurrentContext
}

// SetCurrentContext updates the current assembled context.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) SetCurrentContext(ctx *AssembledContext) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CurrentContext = ctx
	s.LastActiveAt = time.Now()
}

// ToSessionState converts to an external SessionState.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) ToSessionState() *SessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &SessionState{
		ID:           s.ID,
		ProjectRoot:  s.ProjectRoot,
		GraphID:      s.GraphID,
		State:        s.State,
		StepCount:    s.Metrics.TotalSteps,
		TokensUsed:   s.Metrics.TotalTokens,
		CreatedAt:    s.CreatedAt,
		LastActiveAt: s.LastActiveAt,
		DegradedMode: s.Metrics.DegradedMode,
	}
}

// GetClarificationPrompt returns the clarification prompt if set.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) GetClarificationPrompt() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.History) == 0 {
		return ""
	}
	// Return the last history entry's clarification prompt if any
	lastEntry := s.History[len(s.History)-1]
	return lastEntry.ClarificationPrompt
}

// GetMetrics returns a copy of the session metrics.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) GetMetrics() SessionMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Metrics == nil {
		return SessionMetrics{}
	}
	return *s.Metrics
}

// GetMetric returns the value of a specific metric field.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) GetMetric(field MetricField) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Metrics == nil {
		return 0
	}
	switch field {
	case MetricSteps:
		return s.Metrics.TotalSteps
	case MetricTokens:
		return s.Metrics.TotalTokens
	case MetricToolCalls:
		return s.Metrics.ToolCalls
	case MetricToolErrors:
		return s.Metrics.ToolErrors
	case MetricLLMCalls:
		return s.Metrics.LLMCalls
	case MetricCacheHits:
		return s.Metrics.CacheHits
	case MetricGroundingRetries:
		return s.Metrics.GroundingRetries
	case MetricToolForcingRetries:
		return s.Metrics.ToolForcingRetries
	default:
		return 0
	}
}

// GetProjectRoot returns the project root path.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) GetProjectRoot() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ProjectRoot
}

// -----------------------------------------------------------------------------
// CRS Methods
// -----------------------------------------------------------------------------

// SetCRS sets the Code Reasoning State for this session.
//
// Description:
//
//	Associates a CRS instance with this session for MCTS-based reasoning.
//	Also initializes the CRS serializer for later export operations.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) SetCRS(crsInstance crs.CRS) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CRS = crsInstance
	s.cachedSummary = nil
	if s.crsSerializer == nil {
		s.crsSerializer = crs.NewSerializer(nil)
	}
}

// GetCRS returns the CRS instance for this session.
//
// Outputs:
//
//	crs.CRS - The CRS instance, or nil if not set.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) GetCRS() crs.CRS {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.CRS
}

// HasCRS returns true if CRS is enabled for this session.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) HasCRS() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.CRS != nil
}

// SetTraceRecorder sets the trace recorder for this session.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) SetTraceRecorder(recorder *crs.TraceRecorder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.traceRecorder = recorder
}

// GetTraceRecorder returns the trace recorder for this session.
//
// Outputs:
//
//	*crs.TraceRecorder - The trace recorder, or nil if not set.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) GetTraceRecorder() *crs.TraceRecorder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.traceRecorder
}

// GetReasoningSummary returns the reasoning summary for this session.
//
// Description:
//
//	Computes high-level metrics about the reasoning process from CRS state.
//	Returns nil if CRS is not enabled. Summary is cached until CRS changes.
//
// Outputs:
//
//	*ReasoningSummary - Summary metrics, or nil if CRS not enabled.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) GetReasoningSummary() *ReasoningSummary {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.CRS == nil {
		return nil
	}

	// Return cached summary if available
	if s.cachedSummary != nil {
		return convertCRSSummary(s.cachedSummary)
	}

	// Initialize serializer if needed
	if s.crsSerializer == nil {
		s.crsSerializer = crs.NewSerializer(nil)
	}

	// Compute summary from current snapshot
	snapshot := s.CRS.Snapshot()
	summary := s.crsSerializer.ComputeSummary(snapshot)
	s.cachedSummary = &summary

	return convertCRSSummary(&summary)
}

// InvalidateSummaryCache clears the cached reasoning summary.
//
// Description:
//
//	Call this after applying deltas to CRS to ensure fresh summary
//	is computed on next GetReasoningSummary call.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) InvalidateSummaryCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cachedSummary = nil
}

// GetReasoningTrace returns the reasoning trace for this session.
//
// Description:
//
//	Returns the step-by-step reasoning trace if a trace recorder is set.
//	Returns nil if trace recording is not enabled.
//
// Outputs:
//
//	*crs.ReasoningTrace - The reasoning trace, or nil if not enabled.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) GetReasoningTrace() *crs.ReasoningTrace {
	s.mu.RLock()
	recorder := s.traceRecorder
	sessionID := s.ID
	s.mu.RUnlock()

	if recorder == nil {
		return nil
	}

	trace := recorder.Export(sessionID)
	return &trace
}

// RecordTraceStep records a reasoning trace step.
//
// Description:
//
//	Records a step in the reasoning trace for audit and debugging.
//	This is a convenience wrapper around the TraceRecorder.
//	If trace recording is not enabled, this is a no-op.
//
// Inputs:
//
//	step - The trace step to record.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) RecordTraceStep(step crs.TraceStep) {
	s.mu.RLock()
	recorder := s.traceRecorder
	s.mu.RUnlock()

	if recorder == nil {
		return
	}

	recorder.RecordStep(step)
}

// GetCRSExport returns the full CRS state export.
//
// Description:
//
//	Exports all six CRS indexes and summary metrics in JSON-serializable format.
//	Returns nil if CRS is not enabled.
//
// Outputs:
//
//	*crs.CRSExport - Full CRS export, or nil if CRS not enabled.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) GetCRSExport() *crs.CRSExport {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.CRS == nil {
		return nil
	}

	// Initialize serializer if needed
	if s.crsSerializer == nil {
		s.crsSerializer = crs.NewSerializer(nil)
	}

	snapshot := s.CRS.Snapshot()
	export := s.crsSerializer.Export(snapshot, s.ID)
	return export
}

// convertCRSSummary converts crs.ReasoningSummary to agent.ReasoningSummary.
func convertCRSSummary(src *crs.ReasoningSummary) *ReasoningSummary {
	if src == nil {
		return nil
	}
	return &ReasoningSummary{
		NodesExplored:      src.NodesExplored,
		NodesProven:        src.NodesProven,
		NodesDisproven:     src.NodesDisproven,
		NodesUnknown:       src.NodesUnknown,
		ConstraintsApplied: src.ConstraintsApplied,
		ExplorationDepth:   src.ExplorationDepth,
		ConfidenceScore:    src.ConfidenceScore,
	}
}

// -----------------------------------------------------------------------------
// Tool Router Methods
// -----------------------------------------------------------------------------

// SetToolRouter sets the tool router for this session.
//
// Description:
//
//	Associates a ToolRouter instance with this session for fast tool selection.
//	Should be called after session creation if tool routing is enabled.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) SetToolRouter(router ToolRouter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolRouter = router
}

// GetToolRouter returns the tool router for this session.
//
// Outputs:
//
//	ToolRouter - The tool router, or nil if not enabled.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) GetToolRouter() ToolRouter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.toolRouter
}

// HasToolRouter returns true if tool routing is enabled for this session.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) HasToolRouter() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.toolRouter != nil
}

// SetModelManager sets the model manager for this session.
//
// Description:
//
//	Associates a ModelManager instance with this session for multi-model
//	coordination. Used to prevent model thrashing when using tool router
//	with main LLM.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) SetModelManager(manager ModelManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modelManager = manager
}

// GetModelManager returns the model manager for this session.
//
// Outputs:
//
//	ModelManager - The model manager, or nil if not set.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) GetModelManager() ModelManager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.modelManager
}

// IsToolRouterEnabled returns true if tool routing is configured and enabled.
//
// Description:
//
//	Checks both the configuration flag and whether a router has been set.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) IsToolRouterEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Config.ToolRouterEnabled && s.toolRouter != nil
}

// RecordToolError records a tool failure for router feedback.
//
// Description:
//
//	Records that a tool failed with an error. This is fed back to the
//	tool router so it can avoid suggesting the same tool again.
//	Only keeps the last 5 errors to avoid unbounded growth.
//
// Inputs:
//
//	tool - The tool name that failed.
//	errMsg - The error message.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) RecordToolError(tool, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := ToolRouterError{
		Tool:      tool,
		Error:     errMsg,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	s.recentToolErrors = append(s.recentToolErrors, err)

	// Keep only the last 5 errors
	if len(s.recentToolErrors) > 5 {
		s.recentToolErrors = s.recentToolErrors[len(s.recentToolErrors)-5:]
	}
}

// GetRecentToolErrors returns recent tool failures for router feedback.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) GetRecentToolErrors() []ToolRouterError {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.recentToolErrors) == 0 {
		return nil
	}

	// Return a copy to avoid race conditions
	result := make([]ToolRouterError, len(s.recentToolErrors))
	copy(result, s.recentToolErrors)
	return result
}

// ClearToolErrors clears all recorded tool errors.
//
// Description:
//
//	Call this when errors have been addressed or a new query starts.
//
// Thread Safety: This method is safe for concurrent use.
func (s *Session) ClearToolErrors() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recentToolErrors = nil
}
