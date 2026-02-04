# Frontend build stage
FROM node:20-alpine AS frontend-builder

WORKDIR /app/web

# Copy frontend package files
COPY web/package*.json ./

# Install dependencies
RUN npm ci

# Copy frontend source
COPY web/ ./

# Build frontend
RUN npm run build

# Backend build stage
FROM golang:1.25-alpine AS backend-builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Copy built frontend from frontend-builder
COPY --from=frontend-builder /app/static ./static

# Build arguments for version info
ARG VERSION=dev
ARG COMMIT=unknown

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /lemuria \
    ./cmd/lemuria

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 lemuria && \
    adduser -D -u 1000 -G lemuria lemuria

WORKDIR /app

# Copy binary from builder
COPY --from=backend-builder /lemuria /app/lemuria

# Copy static files from frontend builder
COPY --from=frontend-builder /app/static /app/static

# Set ownership
RUN chown -R lemuria:lemuria /app

# Use non-root user
USER lemuria

# Environment variables
ENV STATIC_DIR=/app/static

# Expose default port
EXPOSE 4141

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:4141/health || exit 1

# Run the server
ENTRYPOINT ["/app/lemuria"]
CMD ["-config", "/app/config/lemuria.yaml"]
