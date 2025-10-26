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