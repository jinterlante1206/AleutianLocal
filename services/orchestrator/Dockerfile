# Use a multi-stage build for a small, secure final image.

# --- Build Stage ---
FROM --platform=linux/arm64 golang:1.25-alpine AS builder

# Set the working directory to the project root inside the container
WORKDIR /src

# Copy the Go module files first to leverage Docker layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code for the entire project
COPY services/ ./services/
COPY pkg/ ./pkg/

# Build the orchestrator binary, pointing to its package path from the root
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /app/orchestrator ./services/orchestrator
# --- Final Stage ---
FROM --platform=linux/arm64 alpine:latest
WORKDIR /app

RUN apk update && \
    apk add --no-cache \
        git \
        curl \
        python3 \
        py3-pip

RUN pip3 install --no-cache-dir --break-system-packages "huggingface_hub[cli]>=0.26.0"
RUN hf version

# Copy only the compiled binary from the builder stage
COPY --from=builder /app/orchestrator .

# ADD THIS LINE to copy the needed policy file
# Ensure the target directory structure exists
COPY --from=builder /src/services/orchestrator/ui /app/ui

# The CMD should start the server
CMD ["/app/orchestrator"]
