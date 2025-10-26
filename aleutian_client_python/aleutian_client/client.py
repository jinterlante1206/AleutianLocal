import httpx
from .models import (
    RAGRequest, RAGResponse,
    DirectChatRequest, DirectChatResponse, Message,
    DocumentRequest, DocumentResponse, SessionListResponse, SessionInfo, DeleteSessionResponse
)
from .exceptions import AleutianConnectionError, AleutianApiError
from typing import List, Optional


class AleutianClient:
    def __init__(self, host: str = "http://localhost", port: int = 12210):
        """
        Initializes the client to connect to an Aleutian orchestrator.

        Args:
            host: The hostname of the orchestrator (e.g., http://localhost)
            port: The port the orchestrator is listening on (default 12210)
        """
        self.base_url = f"{host}:{port}/v1"
        self._client = httpx.Client(base_url=self.base_url, timeout=300.0)  # 5 min timeout

    def health_check(self) -> dict:
        """Checks the health of the orchestrator."""
        try:
            response = self._client.get("/health")
            response.raise_for_status()  # Raise HTTPError for 4xx/5xx
            return response.json()
        except httpx.RequestError as e:
            raise AleutianConnectionError(f"Connection failed: {e}") from e

    def ask(self, query: str, pipeline: str = "reranking", no_rag: bool = False,
            session_id: str = None) -> RAGResponse:
        """
        Asks a question to the RAG system (maps to /v1/rag).

        Args:
            query: The user's question.
            pipeline: The RAG pipeline to use (e.g., "reranking", "standard").
            no_rag: Set to True to skip RAG and ask the LLM directly.
            session_id: An optional session ID.

        Returns:
            A RAGResponse object with the answer and sources.
        """
        request_data = RAGRequest(query=query, pipeline=pipeline, no_rag=no_rag,
                                  session_id=session_id)

        try:
            response = self._client.post("/rag", json=request_data.model_dump())

            if response.status_code != 200:
                # Try to parse the error detail from the server
                try:
                    error_detail = response.json().get('details', response.text)
                except Exception:
                    error_detail = response.text
                raise AleutianApiError(
                    f"API returned status {response.status_code}: {error_detail}")

            return RAGResponse(**response.json())

        except httpx.RequestError as e:
            raise AleutianConnectionError(f"Connection to /v1/rag failed: {e}") from e

    def chat(self, messages: list[Message]) -> DirectChatResponse:
        """
        Sends a list of messages to the direct chat endpoint (maps to /v1/chat/direct).

        Args:
            messages: A list of Message objects (e.g., [Message(role="user", content="Hi")])

        Returns:
            A DirectChatResponse object with the assistant's answer.
        """
        request_data = DirectChatRequest(messages=messages)
        try:
            response = self._client.post("/chat/direct", json=request_data.model_dump())
            if response.status_code != 200:
                try:
                    error_detail = response.json().get('error', response.text)
                except Exception:
                    error_detail = response.text
                raise AleutianApiError(
                    f"API returned status {response.status_code}: {error_detail}")

            return DirectChatResponse(**response.json())

        except httpx.RequestError as e:
            raise AleutianConnectionError(f"Connection to /v1/chat/direct failed: {e}") from e

    def populate_document(self, content: str, source: str,
                          version: Optional[str] = None) -> DocumentResponse:
        """
        Populates a single document into Weaviate (maps to POST /v1/documents).
        Note: This sends raw content. For file-path based ingestion, use the CLI.

        Args:
            content: The text content of the document.
            source: A source identifier (e.g., file path, URL).
            version: (Optional) A version string for data tracking.

        Returns:
            A DocumentResponse object indicating success or skip.
        """
        # Note: The Go handler 'documents.go' doesn't seem to use a 'version' field yet.
        # We send it, but it might be ignored until the handler is updated.
        request_data = DocumentRequest(content=content, source=source, version=version)

        try:
            # Note: The 'documents.go' handler expects 'content' and 'source' at the top level
            # Let's match the Go struct 'CreateDocumentRequest'
            response = self._client.post("/v1/documents",
                                         json=request_data.model_dump(exclude_none=True))
            self._handle_error(response, "/v1/documents")
            return DocumentResponse(**response.json())
        except httpx.RequestError as e:
            raise AleutianConnectionError(f"Connection to /v1/documents failed: {e}") from e

    def list_sessions(self) -> List[SessionInfo]:
        """
        Lists all available conversation sessions (maps to GET /v1/sessions).

        Returns:
            A list of SessionInfo objects.
        """
        try:
            response = self._client.get("/v1/sessions")
            self._handle_error(response, "/v1/sessions")

            # Parse the nested GraphQL-like response
            parsed_response = SessionListResponse(**response.json())
            if parsed_response.data and "Session" in parsed_response.data.Get:
                return parsed_response.data.Get["Session"]
            return []  # Return empty list if no data or "Session" key

        except httpx.RequestError as e:
            raise AleutianConnectionError(f"Connection to /v1/sessions failed: {e}") from e

    def delete_session(self, session_id: str) -> DeleteSessionResponse:
        """
        Deletes a specific session and its related conversations (maps to DELETE /v1/sessions/{session_id}).

        Args:
            session_id: The ID of the session to delete.

        Returns:
            A DeleteSessionResponse object confirming deletion.
        """
        endpoint = f"/v1/sessions/{session_id}"
        try:
            response = self._client.delete(endpoint)
            self._handle_error(response, endpoint)
            return DeleteSessionResponse(**response.json())
        except httpx.RequestError as e:
            raise AleutianConnectionError(f"Connection to {endpoint} failed: {e}") from e

    def close(self):
        """
        Safe-closes the underlying HTTP client.
        Call this when you are done with the client instance.
        """
        if self._client and not self._client.is_closed:
            self._client.close()

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()
