## Template 1: Adding a Custom Python Service

This guide explains how to add your own containerized Python service (like a custom financial model, data parser, or utility) to the Aleutian stack.

**Goal:** Integrate a custom service that can be called by the Aleutian orchestrator or other services.

**Steps:**

1.  **Copy Template Files:**
    * Create a directory for your new service (e.g., `my_cool_service`).
    * Copy the template files into this directory:
        * `templates/python-service/Dockerfile`
        * `templates/python-service/requirements.txt`
        * `templates/python-service/server.py`

2.  **Implement Your Logic:**
    * Edit `my_cool_service/server.py` and add your specific Python code. Implement the logic within the `/run` endpoint or add new endpoints as needed.
    * Add any Python dependencies your code needs to `my_cool_service/requirements.txt`.

3.  **Configure `podman-compose.override.yml`:**
    * Create or edit the `podman-compose.override.yml` file in your Aleutian project root.
    * Add a service definition for your new service, based on the snippet below. **Remember to customize:**
        * `my-custom-service`: Rename this key to something descriptive (e.g., `finance-engine`).
        * `context`: Update the path to point to your service's code directory (e.g., `./my_cool_service`).
        * `container_name`: Give your container a unique name (e.g., `user-finance-engine`).
        * `ports`: Choose a unique *host* port (the first number, e.g., `12129`) if you need to access the service directly from your Mac. The container port (the second number, `8000`) should match what `server.py` uses.
        * `environment`: Add any environment variables your `server.py` needs. Use `${VAR_NAME:-default}` for optional variables.
        * `volumes` / `secrets`: Uncomment and configure if your service needs persistent data or access to secrets (like API keys). Make sure secrets are created with `podman secret create ...` first.

    ```yaml
    # --- podman-compose.override.yml ---
    version: '3.8' # Use a modern version

    services:
      my-custom-service: # <-- Rename this (e.g., finance-engine)
        build:
          context: ./path/to/your/service-code # <-- Update this path
          dockerfile: Dockerfile # Assumes Dockerfile is in the context directory
        container_name: user-custom-service # <-- Give it a unique name
        ports:
          - "12XYZ:8000" # <-- Choose a unique host port (12XYZ)
        environment:
          # Add environment variables needed by your server.py
          SOME_CONFIG_VALUE: ${SOME_CONFIG_VALUE:-default}
          # Example: API key (better to use secrets for sensitive values)
          # EXTERNAL_API_KEY: ${EXTERNAL_API_KEY} 
        # volumes: # Uncomment if you need to mount local data
        #   - ./my_service_data:/data 
        # secrets: # Uncomment if you need secrets
        #   - source: my_api_key_secret # Assumes 'podman secret create my_api_key_secret -' was run
        networks:
          - aleutian-network # Connects to the main Aleutian network

    # Define secrets used by your service (must exist in Podman)
    # secrets:
    #   my_api_key_secret:
    #     external: true

    # Define named volumes used by your service (if any)
    # volumes:
    #   my_service_data: {}

    # Declare the Aleutian network as external
    networks:
      aleutian-network:
        external: true

    ```

4.  **Integrate with Orchestrator (Optional):**
    * If you want the main Aleutian `orchestrator` to call your new service, you need to tell it where to find it.
    * Add an environment variable to the `orchestrator` service definition (usually in `podman-compose.override.yml` is cleanest):
        ```yaml
        # --- podman-compose.override.yml ---
        services:
          orchestrator:
            environment:
              # Use a descriptive name matching your service's purpose
              ALEUTIAN_CUSTOM_TOOL_FINANCE: http://user-finance-engine:8000 # Use container_name and internal port
              # Add others as needed
              # ALEUTIAN_CUSTOM_TOOL_PARSER: http://user-pdf-parser:8000
          # ... your custom service definition ...
        ```
    * Modify the `orchestrator`'s Go code (e.g., in a specific handler) to read this environment variable (`os.Getenv("ALEUTIAN_CUSTOM_TOOL_FINANCE")`) and make HTTP requests to that URL.

5.  **Deploy:**
    * Run `./aleutian stack start --build` (or `deploy --build`) to build and start your new service along with the core stack.
    * Check logs: `./aleutian stack logs user-custom-service`.

---

### Template Files (`templates/python-service/`)

**`Dockerfile`**
```dockerfile
# Use a slim base image
FROM python:3.11-slim

WORKDIR /app

# Install build tools if any dependencies need C extensions
# RUN apt-get update && apt-get install -y --no-install-recommends build-essential && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY server.py .

EXPOSE 8000

# Use uvicorn for running the FastAPI app
CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8000"]
```

### Sample server.py
```python
import os
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
import logging

logging.basicConfig(level=os.getenv("LOG_LEVEL", "INFO"))
logger = logging.getLogger(__name__)

app = FastAPI(title="My Custom Aleutian Service")

# Example request model
class RunRequest(BaseModel):
    input_data: str
    # Add other fields as needed

# Example response model
class RunResponse(BaseModel):
    output_data: str
    status: str

@app.post("/run", response_model=RunResponse)
async def run_custom_logic(request: RunRequest):
    """
    Placeholder endpoint for your custom logic.
    Receives data, processes it, and returns a result.
    """
    logger.info(f"Received request with data: {request.input_data[:50]}...") # Log truncated data
    try:
        # --- IMPLEMENT YOUR CUSTOM LOGIC HERE ---
        # Example: Call an external API, run a calculation, etc.
        processed_data = f"Processed: {request.input_data.upper()}"
        # --- END CUSTOM LOGIC ---

        logger.info("Processing successful.")
        return RunResponse(output_data=processed_data, status="success")

    except Exception as e:
        logger.error(f"Error during processing: {e}", exc_info=True)
        raise HTTPException(status_code=500, detail=f"Processing failed: {e}")

@app.get("/health")
def health_check():
    """Basic health check endpoint."""
    return {"status": "ok"}

# If running directly (python server.py), mainly for local testing without docker
if __name__ == "__main__":
    import uvicorn
    port = int(os.getenv("PORT", "8000"))
    uvicorn.run(app, host="0.0.0.0", port=port)
```

### Sample requirements.txt
```text
fastapi
uvicorn[standard]
# Add other dependencies here, e.g.:
# requests
# pandas
```