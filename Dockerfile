# Fight Club - Go Engine Dockerfile
# Multi-stage build for production deployment

# ============================================
# Stage 1: Build stage
# ============================================
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install git for go mod download (some deps might need it)
RUN apk add --no-cache git

# Copy go mod files first for better caching
COPY fight-club-go/go.mod fight-club-go/go.sum ./
RUN go mod download

# Copy source code
COPY fight-club-go/ .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o server ./cmd/server

# ============================================
# Stage 2: Production stage
# ============================================
FROM alpine:latest

# Install FFmpeg for streaming and ca-certificates for HTTPS
RUN apk add --no-cache ffmpeg ca-certificates

# Create non-root user
RUN addgroup -g 1001 -S appgroup && \
    adduser -S appuser -u 1001 -G appgroup

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/server /app/server

# Copy static files
COPY --from=builder /app/admin-panel /app/admin-panel
COPY --from=builder /app/assets /app/assets

# Change ownership
RUN chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 3000

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:3000/api/state || exit 1

# Start application
CMD ["/app/server"]
