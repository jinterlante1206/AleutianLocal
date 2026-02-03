// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package dag

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// DefaultNodeTimeout is the default timeout for nodes that don't specify one.
const DefaultNodeTimeout = 30 * time.Second

// BaseNode provides a partial implementation of the Node interface.
//
// Description:
//
//	BaseNode implements the common parts of Node (name, dependencies, timeout,
//	retryable). Embed this in concrete node implementations and override Execute.
//
// Example:
//
//	type MyNode struct {
//	    dag.BaseNode
//	    // custom fields
//	}
//
//	func NewMyNode() *MyNode {
//	    return &MyNode{
//	        BaseNode: dag.BaseNode{
//	            NodeName:         "MY_NODE",
//	            NodeDependencies: []string{"OTHER_NODE"},
//	            NodeTimeout:      10 * time.Second,
//	        },
//	    }
//	}
//
//	func (n *MyNode) Execute(ctx context.Context, inputs map[string]any) (any, error) {
//	    // implementation
//	}
type BaseNode struct {
	NodeName         string
	NodeDependencies []string
	NodeTimeout      time.Duration
	NodeRetryable    bool
}

// Name returns the node's unique identifier.
func (n *BaseNode) Name() string {
	return n.NodeName
}

// Dependencies returns the names of nodes that must complete first.
func (n *BaseNode) Dependencies() []string {
	if n.NodeDependencies == nil {
		return []string{}
	}
	return n.NodeDependencies
}

// Timeout returns the maximum execution time for this node.
func (n *BaseNode) Timeout() time.Duration {
	if n.NodeTimeout == 0 {
		return DefaultNodeTimeout
	}
	return n.NodeTimeout
}

// Retryable returns whether this node can be retried on failure.
func (n *BaseNode) Retryable() bool {
	return n.NodeRetryable
}

// Execute returns an error if called directly.
// Concrete implementations must override this method.
func (n *BaseNode) Execute(_ context.Context, _ map[string]any) (any, error) {
	return nil, fmt.Errorf("%w: BaseNode.Execute must be overridden by concrete implementation", ErrInvalidInput)
}

// Builder constructs a DAG with validation.
//
// Description:
//
//	Builder provides a fluent API for constructing DAGs. It validates that
//	all dependencies exist and that no cycles are present.
//
// Thread Safety:
//
//	Builder is NOT safe for concurrent use. Build the DAG in a single goroutine.
//
// Example:
//
//	dag, err := dag.NewBuilder("my-pipeline").
//	    AddNode(parseNode).
//	    AddNode(graphNode).
//	    Build()
type Builder struct {
	name   string
	nodes  map[string]Node
	edges  []Edge
	errors []error
}

// NewBuilder creates a new DAG builder.
//
// Inputs:
//
//	name - The name for the DAG (used in logging/metrics).
//
// Outputs:
//
//	*Builder - The builder instance.
func NewBuilder(name string) *Builder {
	return &Builder{
		name:   name,
		nodes:  make(map[string]Node),
		edges:  make([]Edge, 0),
		errors: make([]error, 0),
	}
}

// AddNode adds a node to the DAG.
//
// Description:
//
//	Adds a node and automatically creates edges from its declared dependencies.
//	If a node with the same name already exists, an error is recorded.
//
// Inputs:
//
//	node - The node to add. Must not be nil.
//
// Outputs:
//
//	*Builder - The builder for chaining.
func (b *Builder) AddNode(node Node) *Builder {
	if node == nil {
		b.errors = append(b.errors, ErrNilNode)
		return b
	}

	name := node.Name()
	if _, exists := b.nodes[name]; exists {
		b.errors = append(b.errors, &NodeError{NodeName: name, Err: ErrDuplicateNode})
		return b
	}

	b.nodes[name] = node

	// Create edges from dependencies
	for _, dep := range node.Dependencies() {
		b.edges = append(b.edges, Edge{From: dep, To: name})
	}

	return b
}

// Build validates and constructs the DAG.
//
// Description:
//
//	Validates that all dependencies exist and no cycles are present.
//	Returns an error if validation fails.
//
// Outputs:
//
//	*DAG - The constructed DAG.
//	error - Non-nil if validation fails.
func (b *Builder) Build() (*DAG, error) {
	// Check for accumulated errors
	if len(b.errors) > 0 {
		return nil, b.errors[0]
	}

	if len(b.nodes) == 0 {
		return nil, ErrInvalidInput
	}

	// Validate dependencies exist
	for _, edge := range b.edges {
		if _, exists := b.nodes[edge.From]; !exists {
			return nil, &NodeError{NodeName: edge.To, Err: ErrNodeNotFound}
		}
	}

	// Build adjacency list
	adjList := make(map[string][]string)
	for name := range b.nodes {
		adjList[name] = b.nodes[name].Dependencies()
	}

	// Check for cycles using DFS
	if err := b.detectCycles(adjList); err != nil {
		return nil, err
	}

	// Find terminal node (no outgoing edges)
	terminal := b.findTerminal()

	return &DAG{
		name:     b.name,
		nodes:    b.nodes,
		edges:    b.edges,
		adjList:  adjList,
		terminal: terminal,
	}, nil
}

// detectCycles uses DFS to detect cycles in the graph.
func (b *Builder) detectCycles(adjList map[string][]string) error {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	path := make([]string, 0)

	var dfs func(node string) error
	dfs = func(node string) error {
		visited[node] = true
		recStack[node] = true
		path = append(path, node)

		for _, dep := range adjList[node] {
			if !visited[dep] {
				if err := dfs(dep); err != nil {
					return err
				}
			} else if recStack[dep] {
				// Found cycle - find where it starts
				cycleStart := -1
				for i, n := range path {
					if n == dep {
						cycleStart = i
						break
					}
				}
				cyclePath := append(path[cycleStart:], dep)
				return NewCycleError(cyclePath)
			}
		}

		path = path[:len(path)-1]
		recStack[node] = false
		return nil
	}

	for name := range b.nodes {
		if !visited[name] {
			if err := dfs(name); err != nil {
				return err
			}
		}
	}

	return nil
}

// findTerminal finds the node with no dependents (terminal node).
// If multiple terminals exist, returns the lexicographically first one for determinism.
func (b *Builder) findTerminal() string {
	// A node is a dependency of another = has outgoing edge
	// We want node with no outgoing edges = no one depends on it
	hasDependent := make(map[string]bool)
	for _, edge := range b.edges {
		hasDependent[edge.From] = true
	}

	// Collect all terminal nodes
	var terminals []string
	for name := range b.nodes {
		if !hasDependent[name] {
			terminals = append(terminals, name)
		}
	}

	if len(terminals) == 0 {
		return ""
	}

	// Sort for deterministic selection (lexicographically first)
	sort.Strings(terminals)
	return terminals[0]
}

// FuncNode wraps a function as a Node for simple cases.
//
// Description:
//
//	FuncNode allows creating nodes from simple functions without
//	defining a full struct.
//
// Example:
//
//	node := dag.NewFuncNode("MY_NODE", []string{"DEP"}, func(ctx, inputs) (any, error) {
//	    return "result", nil
//	})
type FuncNode struct {
	BaseNode
	fn func(context.Context, map[string]any) (any, error)
}

// NewFuncNode creates a node from a function.
//
// Inputs:
//
//	name - The node name.
//	deps - Dependency node names.
//	fn - The function to execute.
//
// Outputs:
//
//	*FuncNode - The function node.
func NewFuncNode(
	name string,
	deps []string,
	fn func(context.Context, map[string]any) (any, error),
) *FuncNode {
	return &FuncNode{
		BaseNode: BaseNode{
			NodeName:         name,
			NodeDependencies: deps,
		},
		fn: fn,
	}
}

// Execute runs the wrapped function.
func (n *FuncNode) Execute(ctx context.Context, inputs map[string]any) (any, error) {
	if n.fn == nil {
		return nil, ErrInvalidInput
	}
	return n.fn(ctx, inputs)
}

// WithTimeout sets the timeout for a FuncNode.
func (n *FuncNode) WithTimeout(d time.Duration) *FuncNode {
	n.NodeTimeout = d
	return n
}

// WithRetryable sets whether the FuncNode is retryable.
func (n *FuncNode) WithRetryable(retryable bool) *FuncNode {
	n.NodeRetryable = retryable
	return n
}
