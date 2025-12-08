ARG VERSION=latest
ARG BUILD_TIMESTAMP=""

# --- Stage 1: Build llama.cpp (Heavy C++ Compilation) ---
# This stage typically caches well unless you change the git repo URL or apt packages.
FROM ubuntu:24.04 as builder

# 1. Install build tools
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

# 2. Clone and Compile
# Tip: To make this even faster/safer, you could pin a specific commit hash here
# using 'git checkout <hash>' to prevent daily updates from breaking cache.
WORKDIR /app
RUN git clone https://github.com/ggerganov/llama.cpp.git
WORKDIR /app/llama.cpp

RUN mkdir build
WORKDIR /app/llama.cpp/build
RUN cmake ..
RUN cmake --build . --config Release


# --- Stage 2: Create the final server image (Python) ---
FROM python:3.11-slim
ARG VERSION
ARG BUILD_TIMESTAMP

WORKDIR /app

# 3. System Dependencies (Cached until apt packages change)
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        curl \
        build-essential \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# 4. Python Dependencies (Cached until requirements.txt changes)
# We do this EARLY so it doesn't run when you just change server code or version.
COPY ./services/gguf_converter/requirements.txt .
RUN pip install --no-cache-dir \
    -r requirements.txt \
    numpy \
    sentencepiece \
    gguf>=0.1.0a1 \
    mistral_common \
    transformers>=4.20.0 \
    torch \
    regex

# 5. Copy Compiled Binaries (Cached until Stage 1 changes)
COPY --from=builder /app/llama.cpp /app/llama.cpp

# 6. Copy App Code (Cached until server.py changes)
COPY ./services/gguf_converter/server.py .

# 7. Metadata & Versioning (Volatile - Keep at bottom!)
# Moving this to the end ensures changing the version doesn't trigger a pip reinstall.
COPY VERSION.txt /tmp/VERSION.txt
RUN export APP_VERSION=$(cat /tmp/VERSION.txt | tr -d '[:space:]') && \
    echo "Using Version: $APP_VERSION" && \
    echo "Build Timestamp: $BUILD_TIMESTAMP"

LABEL maintainer="jinterlante@aleutian.ai"
LABEL app.version=$VERSION
LABEL build.timestamp=$BUILD_TIMESTAMP

# 8. Runtime Config
VOLUME /models
EXPOSE 8000

CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8000"]