"""
// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.
"""
import os
import re
import logging
import json
import time
from typing import Optional

import httpx
import ollama
from .base import BaseRAGPipeline
from datatypes.agent import AgentStepResponse, AgentStepRequest, AgentMessage

logger = logging.getLogger(__name__)

# Code Buddy HTTP service URL
CODE_BUDDY_URL = os.getenv("CODE_BUDDY_URL", "http://localhost:12210/v1/codebuddy")
CODE_BUDDY_GRAPH_ID = os.getenv("CODE_BUDDY_GRAPH_ID", "")

# Circuit breaker state for Code Buddy
_code_buddy_failures = 0
_code_buddy_circuit_open_until = 0.0
_CODE_BUDDY_FAILURE_THRESHOLD = 5
_CODE_BUDDY_RECOVERY_TIMEOUT = 60.0


def _validate_symbol_name(name: str) -> str:
    """Validate and sanitize symbol name."""
    if not name or len(name) > 200:
        raise ValueError("Symbol name must be 1-200 characters")
    # Allow typical identifier characters plus dots for qualified names
    if not re.match(r'^[a-zA-Z_][a-zA-Z0-9_./]*$', name):
        raise ValueError(f"Invalid symbol name format: {name}")
    return name


def _validate_file_path(path: str) -> str:
    """Validate and sanitize file path."""
    if not path or len(path) > 500:
        raise ValueError("File path must be 1-500 characters")
    # Prevent path traversal
    if ".." in path:
        raise ValueError("Path traversal not allowed")
    return path


def _is_circuit_open() -> bool:
    """Check if circuit breaker is open."""
    global _code_buddy_circuit_open_until
    if _code_buddy_circuit_open_until > 0 and time.time() < _code_buddy_circuit_open_until:
        return True
    return False


def _record_code_buddy_failure():
    """Record a Code Buddy failure for circuit breaker."""
    global _code_buddy_failures, _code_buddy_circuit_open_until
    _code_buddy_failures += 1
    if _code_buddy_failures >= _CODE_BUDDY_FAILURE_THRESHOLD:
        _code_buddy_circuit_open_until = time.time() + _CODE_BUDDY_RECOVERY_TIMEOUT
        logger.warning(f"Code Buddy circuit breaker opened for {_CODE_BUDDY_RECOVERY_TIMEOUT}s")


def _record_code_buddy_success():
    """Record a Code Buddy success, resetting circuit breaker."""
    global _code_buddy_failures, _code_buddy_circuit_open_until
    _code_buddy_failures = 0
    _code_buddy_circuit_open_until = 0.0


def _handle_code_buddy_error(response: httpx.Response, context: dict) -> dict:
    """Create agent-friendly error response."""
    status = response.status_code
    if status == 404:
        return {
            "error": f"Symbol '{context.get('name', 'unknown')}' not found",
            "suggestion": "Try using find_symbol to search for similar names"
        }
    elif status == 400:
        try:
            msg = response.json().get('error', 'unknown')
        except Exception:
            msg = 'unknown'
        return {
            "error": f"Invalid request: {msg}",
            "suggestion": "Check parameter format and try again"
        }
    elif status == 503:
        return {
            "error": "Code Buddy service temporarily unavailable",
            "suggestion": "Wait a moment and retry, or use read_file as fallback"
        }
    return {"error": f"Unexpected error: {status}"}


def call_code_buddy(
    endpoint: str,
    params: Optional[dict] = None,
    body: Optional[dict] = None,
    context: Optional[dict] = None
) -> dict:
    """
    Call Code Buddy HTTP API synchronously with retry logic.

    Args:
        endpoint: API endpoint (e.g., 'callers', 'context')
        params: Query parameters for GET requests
        body: JSON body for POST requests
        context: Context for error messages

    Returns:
        API response as dict, or error dict on failure
    """
    if _is_circuit_open():
        return {
            "error": "Code Buddy temporarily disabled due to repeated failures",
            "suggestion": "Use read_file and list_files instead"
        }

    context = context or {}
    max_retries = 3
    base_delay = 1.0

    for attempt in range(max_retries):
        try:
            with httpx.Client(timeout=30.0) as client:
                url = f"{CODE_BUDDY_URL}/{endpoint}"
                if body:
                    response = client.post(url, json=body)
                else:
                    response = client.get(url, params=params)

                if response.status_code == 200:
                    _record_code_buddy_success()
                    return response.json()
                else:
                    _record_code_buddy_failure()
                    return _handle_code_buddy_error(response, context)

        except (httpx.TimeoutException, httpx.ConnectError) as e:
            _record_code_buddy_failure()
            if attempt < max_retries - 1:
                delay = base_delay * (2 ** attempt)
                logger.warning(f"Code Buddy call failed, retrying in {delay}s: {e}")
                time.sleep(delay)
            else:
                return {
                    "error": f"Code Buddy connection failed after {max_retries} attempts",
                    "suggestion": "Service may be down. Use read_file and list_files instead."
                }
        except Exception as e:
            _record_code_buddy_failure()
            return {"error": f"Unexpected error calling Code Buddy: {e}"}

# 1. Generic Tool Definitions (JSON Schema is standard across providers)
TOOLS = [
    # === Existing Tools ===
    {
        "type": "function",
        "function": {
            "name": "list_files",
            "description": "List files in a directory.",
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string",
                             "description": "Path relative to project root (default: .)"}
                },
                "required": ["path"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "read_file",
            "description": "Read contents of a file.",
            "parameters": {
                "type": "object",
                "properties": {
                    "path": {"type": "string", "description": "Path to the file"}
                },
                "required": ["path"]
            }
        }
    },
    # === New Code Buddy Tools ===
    {
        "type": "function",
        "function": {
            "name": "get_context",
            "description": "Get assembled context (code + types + docs) for a query. "
                          "PREFER this over multiple find_* calls for complex queries - "
                          "it's more efficient and provides better results.",
            "parameters": {
                "type": "object",
                "properties": {
                    "query": {"type": "string", "description": "Search query or task description"},
                    "token_budget": {"type": "integer", "default": 8000,
                                     "description": "Maximum tokens to return"}
                },
                "required": ["query"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "find_symbol",
            "description": "Find where a function, type, or variable is defined.",
            "parameters": {
                "type": "object",
                "properties": {
                    "name": {"type": "string", "description": "Name of the symbol to find"},
                    "kind": {"type": "string", "enum": ["function", "type", "variable", "any"],
                            "default": "any", "description": "Type of symbol to search for"}
                },
                "required": ["name"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "find_callers",
            "description": "Find all functions that call a given function. "
                          "Use get_context instead if you need broader context.",
            "parameters": {
                "type": "object",
                "properties": {
                    "function_name": {"type": "string", "description": "Name of the function"},
                    "limit": {"type": "integer", "default": 50, "maximum": 200,
                             "description": "Maximum number of results"}
                },
                "required": ["function_name"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "find_callees",
            "description": "Find all functions called by a given function.",
            "parameters": {
                "type": "object",
                "properties": {
                    "function_name": {"type": "string", "description": "Name of the function"},
                    "limit": {"type": "integer", "default": 50, "maximum": 200,
                             "description": "Maximum number of results"}
                },
                "required": ["function_name"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "find_implementations",
            "description": "Find all types that implement an interface.",
            "parameters": {
                "type": "object",
                "properties": {
                    "interface_name": {"type": "string", "description": "Name of the interface"},
                    "limit": {"type": "integer", "default": 50, "maximum": 100,
                             "description": "Maximum number of results"}
                },
                "required": ["interface_name"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "find_references",
            "description": "Find all locations where a symbol is used.",
            "parameters": {
                "type": "object",
                "properties": {
                    "symbol_name": {"type": "string", "description": "Name of the symbol"},
                    "limit": {"type": "integer", "default": 100, "maximum": 500,
                             "description": "Maximum number of results"}
                },
                "required": ["symbol_name"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "get_type_info",
            "description": "Get full type definition including fields and methods.",
            "parameters": {
                "type": "object",
                "properties": {
                    "type_name": {"type": "string", "description": "Name of the type"}
                },
                "required": ["type_name"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "get_imports",
            "description": "Get all imports for a file.",
            "parameters": {
                "type": "object",
                "properties": {
                    "file_path": {"type": "string", "description": "Path to the file"}
                },
                "required": ["file_path"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "get_dependency_tree",
            "description": "Get import/dependency tree for a file.",
            "parameters": {
                "type": "object",
                "properties": {
                    "file_path": {"type": "string", "description": "Path to the file"},
                    "depth": {"type": "integer", "default": 2,
                             "description": "How deep to traverse dependencies"}
                },
                "required": ["file_path"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "search_library_docs",
            "description": "Search documentation for imported libraries.",
            "parameters": {
                "type": "object",
                "properties": {
                    "query": {"type": "string", "description": "Search query"},
                    "library": {"type": "string", "default": "",
                               "description": "Optional library name to filter by"}
                },
                "required": ["query"]
            }
        }
    },
    # === Synthetic Memory Tools ===
    {
        "type": "function",
        "function": {
            "name": "retrieve_memory",
            "description": "Retrieve learned constraints and patterns relevant to the current task. "
                          "ALWAYS check before making changes to code to avoid repeating mistakes.",
            "parameters": {
                "type": "object",
                "properties": {
                    "query": {"type": "string",
                             "description": "What you're trying to do or understand"},
                    "scope": {"type": "string", "default": "",
                             "description": "File path or glob pattern to focus the search"}
                },
                "required": ["query"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "store_memory",
            "description": "Store a learned constraint or pattern for future sessions. "
                          "Use when you discover something important about the codebase that "
                          "should be remembered for future interactions.",
            "parameters": {
                "type": "object",
                "properties": {
                    "content": {"type": "string",
                               "description": "The rule, constraint, or pattern to remember"},
                    "memory_type": {
                        "type": "string",
                        "enum": ["constraint", "pattern", "convention",
                                "bug_pattern", "optimization", "security"],
                        "description": "Type of knowledge being stored"
                    },
                    "scope": {"type": "string",
                             "description": "What files/paths this applies to (e.g., 'services/*', '*')"},
                    "confidence": {"type": "number", "default": 0.7, "minimum": 0.0, "maximum": 1.0,
                                  "description": "How confident you are (0.0-1.0)"}
                },
                "required": ["content", "memory_type", "scope"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "validate_memory",
            "description": "Mark a memory as validated/confirmed useful, boosting its confidence.",
            "parameters": {
                "type": "object",
                "properties": {
                    "memory_id": {"type": "string", "description": "ID of the memory to validate"}
                },
                "required": ["memory_id"]
            }
        }
    },
    {
        "type": "function",
        "function": {
            "name": "contradict_memory",
            "description": "Mark a memory as contradicted/incorrect, reducing its confidence or deleting it.",
            "parameters": {
                "type": "object",
                "properties": {
                    "memory_id": {"type": "string", "description": "ID of the memory to contradict"},
                    "reason": {"type": "string", "description": "Why this memory is incorrect"}
                },
                "required": ["memory_id", "reason"]
            }
        }
    }
]


class AgentPipeline(BaseRAGPipeline):
    def __init__(self, weaviate_client, config):
        super().__init__(weaviate_client, config)
        self.agent_model = os.getenv("OLLAMA_MODEL", "gemma3:27b")
        self.agent_backend = os.getenv("AGENT_BACKEND", "ollama").lower()
        self.project_root = "/app/codebase"

        # Initialize Clients based on Backend
        if self.agent_backend == "ollama":
            self.ollama_client = ollama.Client(host=config.get("llm_service_url"))
        elif self.agent_backend == "anthropic":
            from anthropic import Anthropic  # Lazy import
            self.anthropic_client = Anthropic(api_key=self.anthropic_api_key)

        logger.info(
            f"Agent initialized with backend: {self.agent_backend} model: {self.agent_model}")

    async def run_step(self, request: AgentStepRequest) -> AgentStepResponse:
        """
        Executes ONE step of the agent loop.
        Stateless: Takes history -> Calls LLM -> Returns Instruction.
        """
        # 1. Convert incoming Pydantic history to LLM-specific dicts
        messages = self._convert_history_to_llm_format(request.history)

        # If history is empty, add the system prompt/initial query context
        if not request.history:
            messages.append(
                {"role": "user", "content": f"Trace the codebase to answer: {request.query}"})

        # 2. Call the LLM
        try:
            response_data = await self._call_model_agnostic(messages)
        except Exception as e:
            logger.error(f"Agent LLM error: {e}", exc_info=True)
            return AgentStepResponse(type="answer", content=f"Critical Agent Error: {e}")

        # 3. Decision Logic
        if response_data.get('tool_calls'):
            tool = response_data['tool_calls'][0]  # Handle first tool

            # Parse args safely
            args = tool['args']
            if isinstance(args, str):
                try:
                    args = json.loads(args)
                except json.JSONDecodeError:
                    pass  # Keep as string if not JSON

            return AgentStepResponse(
                type="tool_call",
                tool=tool['name'],
                args=args,
                tool_id=tool['id']
            )
        else:
            return AgentStepResponse(type="answer", content=response_data['content'])

    def _convert_history_to_llm_format(self, history: list[AgentMessage]) -> list[dict]:
        """Translates the generic history back into the backend-specific format."""
        llm_messages = []

        for msg in history:
            if self.agent_backend == "ollama":
                # Ollama format is simpler
                m = {"role": msg.role, "content": msg.content}
                if msg.tool_calls:
                    # Reconstruct Ollama tool calls structure if needed
                    # Note: Ollama might handle history differently, but basic role/content works for context
                    pass
                llm_messages.append(m)

            elif self.agent_backend == "anthropic":
                if msg.role == "user":
                    if msg.content:
                        llm_messages.append({"role": "user", "content": msg.content})
                    # Handle tool results (which come as role='tool' in our generic format but 'user' in Anthropic)
                    if msg.role == "tool":
                        llm_messages.append({
                            "role": "user",
                            "content": [{
                                "type": "tool_result",
                                "tool_use_id": msg.tool_call_id,
                                "content": msg.content
                            }]
                        })
                elif msg.role == "assistant":
                    content_block = []
                    if msg.content:
                        content_block.append({"type": "text", "text": msg.content})
                    if msg.tool_calls:
                        for tc in msg.tool_calls:
                            content_block.append({
                                "type": "tool_use",
                                "id": tc.id,
                                "name": tc.function.name,
                                "input": json.loads(tc.function.arguments)
                            })
                    llm_messages.append({"role": "assistant", "content": content_block})

        return llm_messages

    async def _call_model_agnostic(self, messages):
        """Routes to the correct provider and normalizes the output"""
        if self.agent_backend == "ollama":
            response = self.ollama_client.chat(
                model=self.agent_model,
                messages=messages,
                tools=TOOLS
            )
            msg = response['message']

            # Normalize Ollama's format to our internal format
            tool_calls = []
            if msg.get('tool_calls'):
                for tc in msg['tool_calls']:
                    tool_calls.append({
                        "id": "call_null",  # Ollama doesn't use IDs, but Anthropic does
                        "name": tc['function']['name'],
                        "args": tc['function']['arguments']
                    })

            return {
                "content": msg.get('content', ''),
                "tool_calls": tool_calls,
                "raw": msg
            }

        elif self.agent_backend == "anthropic":
            # Anthropic requires system prompt to be top-level
            system = "You are a helpful coding agent."
            filtered_msgs = [m for m in messages if m['role'] != 'system']

            response = self.anthropic_client.messages.create(
                model=self.agent_model,  # e.g. claude-3-7-sonnet-20250219
                max_tokens=4096,
                system=system,
                messages=filtered_msgs,
                tools=TOOLS
            )

            content_text = ""
            tool_calls = []

            for block in response.content:
                if block.type == 'text':
                    content_text += block.text
                elif block.type == 'tool_use':
                    tool_calls.append({
                        "id": block.id,
                        "name": block.name,
                        "args": block.input
                    })

            return {
                "content": content_text,
                "tool_calls": tool_calls
            }

        raise ValueError(f"Unsupported Agent Backend: {self.agent_backend}")

    def _append_assistant_message(self, messages, response_data):
        """Helper to append the correct format to history based on backend"""
        if self.agent_backend == "ollama":
            messages.append(response_data['raw'])
        elif self.agent_backend == "anthropic":
            messages.append({
                "role": "assistant",
                "content": [
                               {"type": "text", "text": response_data['content']}
                           ] + [
                               {"type": "tool_use", "id": tc['id'], "name": tc['name'],
                                "input": tc['args']}
                               for tc in response_data['tool_calls']
                           ]
            })

    def _append_tool_result(self, messages, tool_id, fn_name, result):
        """Helper to append tool results in the format the backend expects"""
        if self.agent_backend == "ollama":
            messages.append({
                "role": "tool",
                "content": result,
                # Ollama infers the link, simpler format
            })
        elif self.agent_backend == "anthropic":
            messages.append({
                "role": "user",
                "content": [
                    {
                        "type": "tool_result",
                        "tool_use_id": tool_id,
                        "content": result
                    }
                ]
            })

    def _execute_tool(self, name, args):
        """Execute a tool and return the result."""
        try:
            # === File System Tools ===
            if name in ("list_files", "read_file"):
                rel_path = args.get("path", ".")
                safe_path = os.path.normpath(os.path.join(self.project_root, rel_path))
                if not safe_path.startswith(self.project_root):
                    return "Error: Access denied (Path traversal attempt)"

                if name == "list_files":
                    if os.path.isdir(safe_path):
                        items = os.listdir(safe_path)
                        return json.dumps([f for f in items if not f.startswith('.')])
                    return json.dumps({"error": "Not a directory"})

                elif name == "read_file":
                    if os.path.exists(safe_path):
                        with open(safe_path, 'r') as f:
                            return f.read()
                    return json.dumps({"error": "File not found"})

            # === Code Buddy Tools ===
            elif name == "get_context":
                query = args.get("query", "")
                budget = args.get("token_budget", 8000)
                result = call_code_buddy("context", body={
                    "graph_id": CODE_BUDDY_GRAPH_ID,
                    "query": query,
                    "token_budget": budget
                }, context={"name": query})
                return json.dumps(result)

            elif name == "find_symbol":
                symbol_name = _validate_symbol_name(args.get("name", ""))
                result = call_code_buddy("symbol/" + symbol_name, params={
                    "graph_id": CODE_BUDDY_GRAPH_ID
                }, context={"name": symbol_name})
                return json.dumps(result)

            elif name == "find_callers":
                func_name = _validate_symbol_name(args.get("function_name", ""))
                limit = min(args.get("limit", 50), 200)
                result = call_code_buddy("callers", params={
                    "graph_id": CODE_BUDDY_GRAPH_ID,
                    "function": func_name,
                    "limit": limit
                }, context={"name": func_name})
                return json.dumps(result)

            elif name == "find_callees":
                func_name = _validate_symbol_name(args.get("function_name", ""))
                limit = min(args.get("limit", 50), 200)
                # Note: This would need a /callees endpoint in Code Buddy
                # For now, use context assembler which includes callees
                result = call_code_buddy("context", body={
                    "graph_id": CODE_BUDDY_GRAPH_ID,
                    "query": f"what does {func_name} call",
                    "token_budget": 4000
                }, context={"name": func_name})
                return json.dumps(result)

            elif name == "find_implementations":
                iface_name = _validate_symbol_name(args.get("interface_name", ""))
                limit = min(args.get("limit", 50), 100)
                result = call_code_buddy("implementations", params={
                    "graph_id": CODE_BUDDY_GRAPH_ID,
                    "interface": iface_name,
                    "limit": limit
                }, context={"name": iface_name})
                return json.dumps(result)

            elif name == "find_references":
                symbol_name = _validate_symbol_name(args.get("symbol_name", ""))
                limit = min(args.get("limit", 100), 500)
                # Use context assembler for references
                result = call_code_buddy("context", body={
                    "graph_id": CODE_BUDDY_GRAPH_ID,
                    "query": f"references to {symbol_name}",
                    "token_budget": 4000
                }, context={"name": symbol_name})
                return json.dumps(result)

            elif name == "get_type_info":
                type_name = _validate_symbol_name(args.get("type_name", ""))
                result = call_code_buddy("symbol/" + type_name, params={
                    "graph_id": CODE_BUDDY_GRAPH_ID
                }, context={"name": type_name})
                return json.dumps(result)

            elif name == "get_imports":
                file_path = _validate_file_path(args.get("file_path", ""))
                # Use context to get imports for file
                result = call_code_buddy("context", body={
                    "graph_id": CODE_BUDDY_GRAPH_ID,
                    "query": f"imports in {file_path}",
                    "token_budget": 2000
                }, context={"name": file_path})
                return json.dumps(result)

            elif name == "get_dependency_tree":
                file_path = _validate_file_path(args.get("file_path", ""))
                depth = min(args.get("depth", 2), 5)
                # Use context for dependency tree
                result = call_code_buddy("context", body={
                    "graph_id": CODE_BUDDY_GRAPH_ID,
                    "query": f"dependency tree for {file_path} depth {depth}",
                    "token_budget": 4000
                }, context={"name": file_path})
                return json.dumps(result)

            elif name == "search_library_docs":
                query = args.get("query", "")
                library = args.get("library", "")
                search_query = f"{library} {query}" if library else query
                result = call_code_buddy("context", body={
                    "graph_id": CODE_BUDDY_GRAPH_ID,
                    "query": search_query,
                    "token_budget": 4000,
                    "include_library_docs": True
                }, context={"name": query})
                return json.dumps(result)

            # === Synthetic Memory Tools ===
            elif name == "retrieve_memory":
                query = args.get("query", "")
                scope = args.get("scope", "")
                if not query:
                    return json.dumps({"error": "Query is required"})
                result = call_code_buddy("memories/retrieve", body={
                    "query": query,
                    "scope": scope,
                    "limit": 10
                }, context={"name": query})
                return json.dumps(result)

            elif name == "store_memory":
                content = args.get("content", "")
                memory_type = args.get("memory_type", "")
                scope = args.get("scope", "")
                confidence = args.get("confidence", 0.7)

                if not content:
                    return json.dumps({"error": "Content is required"})
                if not memory_type:
                    return json.dumps({"error": "Memory type is required"})
                if not scope:
                    return json.dumps({"error": "Scope is required"})

                # Validate memory type
                valid_types = ["constraint", "pattern", "convention",
                              "bug_pattern", "optimization", "security"]
                if memory_type not in valid_types:
                    return json.dumps({
                        "error": f"Invalid memory type. Must be one of: {', '.join(valid_types)}"
                    })

                # Validate confidence
                if confidence < 0.0 or confidence > 1.0:
                    return json.dumps({"error": "Confidence must be between 0.0 and 1.0"})

                result = call_code_buddy("memories", body={
                    "content": content,
                    "memory_type": memory_type,
                    "scope": scope,
                    "confidence": confidence,
                    "source": "agent_discovery"
                }, context={"name": content[:50]})
                return json.dumps(result)

            elif name == "validate_memory":
                memory_id = args.get("memory_id", "")
                if not memory_id:
                    return json.dumps({"error": "Memory ID is required"})
                result = call_code_buddy(f"memories/{memory_id}/validate", body={},
                                        context={"name": memory_id})
                return json.dumps(result)

            elif name == "contradict_memory":
                memory_id = args.get("memory_id", "")
                reason = args.get("reason", "")
                if not memory_id:
                    return json.dumps({"error": "Memory ID is required"})
                if not reason:
                    return json.dumps({"error": "Reason is required"})
                result = call_code_buddy(f"memories/{memory_id}/contradict", body={
                    "reason": reason
                }, context={"name": memory_id})
                return json.dumps(result)

            else:
                return json.dumps({"error": f"Unknown tool: {name}"})

        except ValueError as e:
            return json.dumps({"error": str(e), "suggestion": "Check parameter format"})
        except Exception as e:
            logger.error(f"Tool execution error: {e}", exc_info=True)
            return json.dumps({"error": f"System Error: {e}"})