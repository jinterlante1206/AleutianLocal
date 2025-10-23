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

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        curl \
        build-essential \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Install FastAPI server dependencies
COPY ./services/gguf_converter/requirements.txt .
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir \
    -r requirements.txt \
    numpy \
    sentencepiece \
    gguf>=0.1.0a1 \
    mistral_common \
    transformers>=4.20.0 \
    torch \
    regex

# Copy the compiled llama.cpp and its dependencies from the builder stage
COPY --from=builder /app/llama.cpp /app/llama.cpp

# Copy our server code
COPY ./services/gguf_converter/server.py .

# Define the /models volume. This is CRITICAL.
VOLUME /models
EXPOSE 8000

# Run the server
CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8000"]