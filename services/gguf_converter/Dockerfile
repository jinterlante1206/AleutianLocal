# services/converter/Dockerfile

# --- Stage 1: Build llama.cpp ---
FROM ubuntu:24.04 as builder

# Install build dependencies
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        git \
        make \
        g++ \
        python3 \
        python3-pip \
        python3-venv \
    && rm -rf /var/lib/apt/lists/*

# Clone and compile llama.cpp
WORKDIR /app
RUN git clone https://github.com/ggerganov/llama.cpp.git
WORKDIR /app/llama.cpp
RUN make

# Install llama.cpp's python dependencies
RUN pip3 install -r requirements.txt


# --- Stage 2: Create the final server image ---
FROM python:3.11-slim

WORKDIR /app

# Install FastAPI server dependencies
COPY ./services/converter/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy the compiled llama.cpp and its dependencies from the builder stage
COPY --from=builder /app/llama.cpp /app/llama.cpp
COPY --from=builder /usr/local/lib/python3.12/dist-packages /usr/local/lib/python3.11/site-packages
COPY --from=builder /usr/lib/python3/dist-packages /usr/local/lib/python3.11/site-packages

# Copy our server code
COPY ./services/converter/server.py .

# Define the /models volume. This is CRITICAL.
VOLUME /models
EXPOSE 8000

# Run the server
CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8000"]