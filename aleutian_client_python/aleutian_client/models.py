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

from pydantic import BaseModel, Field
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

# Timeseries Forecasting Model datatypes
class TimeseriesForecastRequest(BaseModel):
    """
    Payload to match a timeseries forecast
    """
    name: str
    context_period_size: int
    forecast_period_size: int
    data_version: Optional[str] = "v1.0"

class TimeseriesForecastResponse(BaseModel):
    """
    Response payload you get back from the timeseries forecast endpoint
    """
    name: str
    forecast: List[float] = Field(default_factory=list)
    message: str

class DataFetchRequest(BaseModel):
    tickers: List[str]
    start_date: str  # "YYYY-MM-DD"
    interval: str = "1d" # "1m", "1h", "1d"

class DataFetchResponse(BaseModel):
    status: str
    message: str
    details: Dict[str, str]