from pydantic import BaseModel
from typing import List, Optional, Dict, Any

class ToolFunction(BaseModel):
    name: str
    arguments: str

class ToolCall(BaseModel):
    id: str
    type: str = "function"
    function: ToolFunction

class AgentMessage(BaseModel):
    role: str
    content: Optional[str] = None
    tool_call_id: Optional[str] = None
    tool_calls: Optional[List[ToolCall]] = None

class AgentStepRequest(BaseModel):
    query: str
    history: List[AgentMessage] = []

class AgentStepResponse(BaseModel):
    type: str  # "answer" or "tool_call"
    content: Optional[str] = None
    tool: Optional[str] = None
    args: Optional[Dict[str, Any]] = None
    tool_id: Optional[str] = None