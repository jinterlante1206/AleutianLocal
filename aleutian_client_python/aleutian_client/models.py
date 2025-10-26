from pydantic import BaseModel
from typing import List, Optional, Dict, Any

# --- Models for /v1/chat/direct ---
class Message(BaseModel):
    role: str
    content: str

class DirectChatRequest(BaseModel):
    messages: List[Message]

class DirectChatResponse(BaseModel):
    answer: str

# --- Models for /v1/rag ---
class RAGRequest(BaseModel):
    query: str
    session_id: Optional[str] = None
    pipeline: str
    no_rag: bool

class SourceInfo(BaseModel):
    source: str
    distance: Optional[float] = None
    score: Optional[float] = None

class RAGResponse(BaseModel):
    answer: str
    session_id: str
    sources: Optional[List[SourceInfo]] = []

# --- Models for /v1/sessions ---
class SessionInfo(BaseModel):
    session_id: str
    summary: str
    timestamp: int

# --- Models for POST /v1/documents ---
class DocumentRequest(BaseModel):
    content: str
    source: str
    version: Optional[str] = None # For future data versioning

class DocumentResponse(BaseModel):
    status: str
    source: str
    id: Optional[str] = None
    message: Optional[str] = None

# --- Models for GET /v1/sessions ---
class WeaviateGraphQLResponse(BaseModel):
    # This matches the nested structure: {"Get": {"Session": [...]}}
    Get: Dict[str, List[SessionInfo]]

class SessionListResponse(BaseModel):
    data: Optional[WeaviateGraphQLResponse] = None
    errors: Optional[List[Any]] = None

# --- Models for DELETE /v1/sessions/{session_id} ---
class DeleteSessionResponse(BaseModel):
    status: str
    deleted_session_id: str