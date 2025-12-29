# Build stage - use Go 1.25+ to match go.mod requirements
FROM --platform=linux/arm64 golang:1.25-alpine AS builder

WORKDIR /src

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY services/data_fetcher/ ./services/data_fetcher/

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /data-fetcher ./services/data_fetcher

# Runtime stage
FROM --platform=linux/arm64 alpine:latest

RUN apk add --no-cache ca-certificates curl

WORKDIR /app

COPY --from=builder /data-fetcher ./data-fetcher

EXPOSE 8001

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8001/health || exit 1

CMD ["/app/data-fetcher"]
