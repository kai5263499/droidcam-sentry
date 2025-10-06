# Multi-stage build for DroidCam Sentry

# Stage 1: Build the Go application
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /build

# Copy go mod files
COPY backend/go.mod backend/go.sum ./backend/
WORKDIR /build/backend
RUN go mod download

# Copy source code
WORKDIR /build
COPY backend/ ./backend/
COPY frontend/ ./frontend/

# Install swag for Swagger docs generation
RUN go install github.com/swaggo/swag/cmd/swag@latest

# Generate Swagger docs
WORKDIR /build/backend
RUN ~/go/bin/swag init -g main.go --output docs

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /build/droidcam-sentry .

# Stage 2: Create minimal runtime image
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    ffmpeg \
    tzdata

# Create non-root user
RUN addgroup -g 1000 sentry && \
    adduser -D -u 1000 -G sentry sentry

# Create directories
RUN mkdir -p /app /data/recordings /data/logs /data/profiles && \
    chown -R sentry:sentry /app /data

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/droidcam-sentry /app/

# Copy frontend files
COPY --from=builder /build/frontend/ /app/frontend/

# Copy default config (will be overridden by mounted volume)
COPY backend/config.yaml /app/config.yaml.example

# Switch to non-root user
USER sentry

# Expose ports
EXPOSE 8080 6060

# Environment variables
ENV RECORDINGS_PATH=/data/recordings
ENV LOGS_PATH=/data/logs
ENV PROFILES_PATH=/data/profiles

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the application
ENTRYPOINT ["/app/droidcam-sentry"]
