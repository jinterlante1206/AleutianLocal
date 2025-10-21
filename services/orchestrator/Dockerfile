# Use a multi-stage build for a small, secure final image.

# --- Build Stage ---
FROM golang:1.25-alpine AS builder

# Set the working directory to the project root inside the container
WORKDIR /src

# Copy the Go module files first to leverage Docker layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code for the entire project
COPY services/ ./services/
COPY internal/ ./internal/

# Build the orchestrator binary, pointing to its package path from the root
RUN CGO_ENABLED=0 go build -o /app/orchestrator ./services/orchestrator

# --- Final Stage ---
FROM alpine:latest
WORKDIR /app

# Copy only the compiled binary from the builder stage
COPY --from=builder /app/orchestrator .


# The CMD should start the server
CMD ["/app/orchestrator"]