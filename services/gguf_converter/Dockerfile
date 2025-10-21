# services/converter/Dockerfile
ARG VERSION=latest
ARG BUILD_TIMESTAMP=""
# --- Stage 1: Build llama.cpp ---
FROM ubuntu:24.04 as builder

# Install build dependencies
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        git \
        cmake \
        libcurl4-openssl-dev \
        build-essential \
        python3 \
        python3-pip \
        python3-venv \
    && rm -rf /var/lib/apt/lists/*

# Clone and compile llama.cpp
WORKDIR /app
RUN git clone https://github.com/ggerganov/llama.cpp.git
WORKDIR /app/llama.cpp

RUN mkdir build
WORKDIR /app/llama.cpp/build
RUN cmake ..
RUN cmake --build . --config Release

# Install llama.cpp's python dependencies
WORKDIR /app/llama.cpp
RUN python3 -m venv venv
RUN ./venv/bin/pip install -r requirements.txt


# --- Stage 2: Create the final server image ---
FROM python:3.11-slim
ARG VERSION
ARG BUILD_TIMESTAMP

WORKDIR /app

# Add in the version number for the build.
COPY VERSION.txt /tmp/VERSION.txt
RUN export APP_VERSION=$(cat /tmp/VERSION.txt | tr -d '[:space:]') && \
    echo "Using Version: $APP_VERSION" && \
    echo "Build Timestamp: $BUILD_TIMESTAMP"
LABEL maintainer="jinterlante@aleutian.ai"
LABEL app.version=$VERSION
LABEL build.timestamp=$BUILD_TIMESTAMP

# Install FastAPI server dependencies
COPY ./services/gguf_converter/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy the compiled llama.cpp and its dependencies from the builder stage
COPY --from=builder /app/llama.cpp /app/llama.cpp
COPY --from=builder /app/llama.cpp/venv/lib/python3.12/site-packages /usr/local/lib/python3.11/site-packages

# Copy our server code
COPY ./services/gguf_converter/server.py .

# Define the /models volume. This is CRITICAL.
VOLUME /models
EXPOSE 8000

# Run the server
CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8000"]