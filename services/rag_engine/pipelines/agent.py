import os
import logging
import json
import ollama
from .base import BaseRAGPipeline
from datatypes.agent import AgentStepResponse, AgentStepRequest, AgentMessage

logger = logging.getLogger(__name__)

# 1. Generic Tool Definitions (JSON Schema is standard across providers)
TOOLS = [
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

    # ... (Keep _execute_tool implementation same as before) ...
    def _execute_tool(self, name, args):
        try:
            rel_path = args.get("path", ".")
            safe_path = os.path.normpath(os.path.join(self.project_root, rel_path))
            if not safe_path.startswith(self.project_root):
                return "Error: Access denied (Path traversal attempt)"

            if name == "list_files":
                if os.path.isdir(safe_path):
                    items = os.listdir(safe_path)
                    return [f for f in items if not f.startswith('.')]
                return "Error: Not a directory"

            elif name == "read_file":
                if os.path.exists(safe_path):
                    with open(safe_path, 'r') as f:
                        return f.read()
                return "Error: File not found"

        except Exception as e:
            return f"System Error: {e}"