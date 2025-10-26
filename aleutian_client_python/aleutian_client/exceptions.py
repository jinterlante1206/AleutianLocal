# aleutian_client_python/exceptions.py
class AleutianError(Exception):
    """Base exception for the Aleutian client."""
    pass

class AleutianConnectionError(AleutianError):
    """Raised when the client cannot connect to the server."""
    pass

class AleutianApiError(AleutianError):
    """Raised when the API returns an error status code (4xx or 5xx)."""
    pass