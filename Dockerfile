# The Redirector - Multi-stage Dockerfile
#
# Build: docker build -t the-redirector .
# Run:   docker run -p 8080:8080 -p 8081:8081 -v $(pwd)/config.yaml:/etc/redirector/config.yaml the-redirector

# ============================================
# Stage 1: Builder
# ============================================
FROM golang:1.21-alpine AS builder

# Build arguments
ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH=amd64

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build all binaries
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /bin/redirector \
    ./cmd/redirector

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /bin/redirector-lint \
    ./cmd/redirector-lint

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /bin/config-syncer \
    ./cmd/config-syncer

# Note: TUI is not included as it requires a terminal

# ============================================
# Stage 2: Runtime
# ============================================
FROM alpine:3.19

# Labels for container registry
LABEL org.opencontainers.image.title="The Redirector"
LABEL org.opencontainers.image.description="High-performance URL redirect and response service"
LABEL org.opencontainers.image.source="https://github.com/jamengual/the-redirector"
LABEL org.opencontainers.image.licenses="MIT"

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 redirector && \
    adduser -u 1000 -G redirector -s /bin/sh -D redirector

# Copy binaries from builder
COPY --from=builder /bin/redirector /usr/local/bin/
COPY --from=builder /bin/redirector-lint /usr/local/bin/
COPY --from=builder /bin/config-syncer /usr/local/bin/

# Create directories
RUN mkdir -p /etc/redirector /var/log/redirector && \
    chown -R redirector:redirector /etc/redirector /var/log/redirector

# Copy sample config (users should mount their own)
COPY --chown=redirector:redirector config.yaml /etc/redirector/config.yaml

# Switch to non-root user
USER redirector

# Expose ports
# 8080 - Redirect traffic
# 8081 - Management API (health, stats, reload)
EXPOSE 8080 8081

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q --spider http://localhost:8081/health || exit 1

# Default command
ENTRYPOINT ["redirector"]
CMD ["-config", "/etc/redirector/config.yaml"]
