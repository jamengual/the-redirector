# Implementation Plan: The Redirector

## Overview

This document outlines the phased implementation approach for building The Redirector, a high-performance vanity URL redirect service in Go. Each phase builds upon the previous, allowing for incremental delivery and validation.

---

## Phase 1: Core Foundation
**Goal**: Minimal viable redirect server with basic functionality
**Status**: Complete ✓

### Stage 1.1: Project Setup
**Goal**: Establish project structure and tooling
**Status**: Complete ✓

**Tasks**:
- [x] Initialize Go module
- [x] Set up directory structure
- [x] Configure linting (golangci-lint via Makefile)
- [x] Set up Makefile with common targets
- [x] Add .gitignore for Go projects
- [x] Create Dockerfile (multi-stage build)

### Stage 1.2: Basic HTTP Server
**Goal**: HTTP server with health endpoints
**Status**: Complete ✓

**Implementation**: `internal/server/server.go`

**Tasks**:
- [x] Implement fasthttp server wrapper
- [x] Add graceful shutdown handling (SIGINT, SIGTERM, SIGHUP)
- [x] Create `/health` endpoint (liveness)
- [x] Create `/ready` endpoint (readiness)
- [x] Add structured logging (zerolog)
- [x] Basic configuration via environment variables
- [x] Separate management server on port 8081

### Stage 1.3: Simple Redirect Logic
**Goal**: Basic exact-path redirects
**Status**: Complete ✓

**Implementation**: `internal/router/router.go`

**Tasks**:
- [x] Define redirect rule struct with ID, Match, Redirect, Priority
- [x] Implement in-memory rule storage
- [x] Create redirect handler
- [x] Add response headers support
- [x] Implement 301/302/307/308 redirects
- [x] Support non-redirect responses (4xx, 5xx with body)
- [x] Query string preservation
- [x] Path suffix preservation for prefix matches

### Stage 1.4: YAML Configuration
**Goal**: Load rules from YAML file
**Status**: Complete ✓

**Implementation**: `internal/config/config.go`

**Tasks**:
- [x] Define configuration schema
- [x] Implement YAML parser (gopkg.in/yaml.v3)
- [x] Configuration validation
- [x] Load on startup
- [x] Error handling for invalid config
- [x] Directory loading with merge support

---

## Phase 2: Advanced Routing
**Goal**: Support regex, glob, and prefix matching
**Status**: Complete ✓

### Stage 2.1: Router Implementation
**Goal**: Efficient path matching
**Status**: Complete ✓

**Implementation**: `internal/router/router.go`

**Tasks**:
- [x] Exact match via hash map (O(1) lookup)
- [x] Prefix match sorted by length (longer wins)
- [x] Support static path segments
- [x] Support wildcard segments (`*`)
- [x] Support catch-all segments (`**`)
- [x] Host-based matching

### Stage 2.2: Regex Pattern Matching
**Goal**: Full regex support with capture groups
**Status**: Complete ✓

**Tasks**:
- [x] Implement regex rule type
- [x] Support PCRE-style patterns
- [x] Capture group extraction ($1, $2, etc.)
- [x] Substitution in destination URL
- [x] Compile patterns on load (not per-request)
- [x] Cache compiled regexes

### Stage 2.3: Glob Pattern Support
**Goal**: User-friendly glob patterns
**Status**: Complete ✓

**Tasks**:
- [x] Implement glob to regex conversion
- [x] Support patterns: `*`, `**`, `?`, `[abc]`
- [x] Path segment preservation logic

### Stage 2.4: Rule Priority and Ordering
**Goal**: Deterministic rule matching
**Status**: Complete ✓

**Tasks**:
- [x] Priority field for manual override
- [x] Specificity sorting (longer prefixes first)
- [x] First-match-wins semantics
- [x] Exact > Prefix > Regex/Glob matching order

---

## Phase 3: Configuration Management
**Goal**: Dynamic configuration without restarts
**Status**: Complete ✓

### Stage 3.1: Configuration Abstraction
**Goal**: Pluggable configuration sources
**Status**: Complete ✓

**Implementation**: `internal/providers/source.go`

**Tasks**:
- [x] Define ConfigSource interface (Fetch, Watch, Validate, Close)
- [x] Implement FileSource (`internal/providers/file.go`)
- [x] Support multiple file formats (YAML, JSON)
- [x] Configuration merging from multiple sources

### Stage 3.2: Hot Reload
**Goal**: Zero-downtime configuration updates
**Status**: Complete ✓

**Implementation**: `internal/watcher/watcher.go`

**Tasks**:
- [x] Implement atomic configuration swap
- [x] File watcher for local files (fsnotify)
- [x] Debounce rapid changes (default 500ms)
- [x] Validation before applying
- [x] Rollback on invalid config
- [x] Metrics for reload success/failure
- [x] SIGHUP signal handling for manual reload

### Stage 3.3: REST API for Configuration
**Goal**: Push configuration via HTTP API
**Status**: Complete ✓

**Implementation**: `internal/server/server.go`

**Tasks**:
- [x] Management API server (separate port 8081)
- [x] POST /api/v1/reload - trigger reload
- [x] GET /api/v1/config - retrieve current config
- [x] GET /api/v1/rules - list rules
- [x] POST /api/v1/rollback - config rollback
- [x] GET /api/v1/versions - version history
- [x] Authentication (API key, JWT)
- [x] Rate limiting on management endpoints

### Stage 3.4: Environment Variable Injection
**Goal**: Dynamic values from environment
**Status**: Complete ✓

**Tasks**:
- [x] Template syntax: `${ENV_VAR}` or `${ENV_VAR:-default}`
- [x] Process templates on config load
- [x] Support in destination URLs
- [x] Support in header values

---

## Phase 4: Config-Syncer Service
**Goal**: Separate service for configuration management with pluggable sources
**Status**: Complete ✓

### Stage 4.1: Pluggable Source Interface
**Goal**: Define extensible interface for community contributions
**Status**: Complete ✓

**Implementation**: `internal/providers/source.go`

**Tasks**:
- [x] Define Source interface with Fetch, Watch, Validate, Close
- [x] Create SourceRegistry for dynamic source registration
- [x] Implement FileSource as reference implementation

### Stage 4.2: GitHub Integration
**Goal**: GitHub repository as configuration source
**Status**: Complete ✓

**Implementation**: `internal/providers/github.go`

**Tasks**:
- [x] Implement GitHubSource with GitHub App authentication
- [x] Support release-based deployments (production)
- [x] Support branch-based deployments (staging)
- [x] Support tag-based deployments
- [x] Webhook handler in config-syncer for real-time updates

### Stage 4.3: Config-Syncer Service
**Goal**: Standalone service that coordinates config sources
**Status**: Complete ✓

**Implementation**: `cmd/config-syncer/main.go`, `internal/syncer/syncer.go`

**Tasks**:
- [x] Create config-syncer binary
- [x] Multi-source aggregation and merging
- [x] Push config to multiple redirector targets
- [x] Health checks for sources and targets
- [x] Webhook HTTP server for GitHub/GitLab events
- [x] Retry logic with exponential backoff

### Stage 4.4: GitLab Integration
**Goal**: GitLab repository support
**Status**: Not Started

**Tasks**:
- [ ] Implement GitLabSource
- [ ] GitLab App or Project Token auth
- [ ] Release and branch tracking
- [ ] Webhook support

---

## Phase 5: Cloud Provider Configuration
**Goal**: Enterprise-grade configuration from cloud services
**Status**: Partially Complete

### Stage 5.1: AWS S3 Integration
**Goal**: Load and watch configuration from S3
**Status**: Complete ✓

**Implementation**: `internal/providers/s3.go`

**Tasks**:
- [x] Implement S3ConfigSource
- [x] Support for S3 bucket + key configuration
- [x] Polling-based updates (configurable interval)
- [x] S3 event notifications support
- [x] IAM role authentication
- [x] Cross-account access via role ARN
- [x] S3-compatible endpoints (MinIO)

### Stage 5.2: AWS Parameter Store
**Goal**: Configuration from SSM Parameter Store
**Status**: Not Started

**Tasks**:
- [ ] Implement ParameterStoreConfigSource
- [ ] Support single parameter or parameter path
- [ ] SecureString decryption
- [ ] Hierarchical parameter organization
- [ ] Polling for updates

### Stage 5.3: AWS Secrets Manager
**Goal**: Sensitive configuration from Secrets Manager
**Status**: Not Started

**Tasks**:
- [ ] Implement SecretsManagerConfigSource
- [ ] Automatic secret rotation handling
- [ ] Version/stage selection
- [ ] Caching with TTL

### Stage 5.4: Additional Cloud Providers
**Goal**: Multi-cloud support
**Status**: Not Started

**Tasks**:
- [ ] Azure Blob Storage integration
- [ ] GCP Cloud Storage integration
- [ ] HashiCorp Consul integration
- [ ] etcd integration

### Stage 5.5: Configuration Versioning
**Goal**: Track and audit configuration changes
**Status**: Complete ✓

**Implementation**: `internal/versioning/`

**Tasks**:
- [x] Version tracking for each config load (SHA256 hash)
- [x] Config diff on reload
- [x] Audit log of changes
- [x] Rollback to previous version
- [x] Config history retention (ring buffer)

---

## Phase 6: Observability and Operations
**Goal**: Production-ready monitoring and debugging
**Status**: Complete ✓

### Stage 6.1: Prometheus Metrics
**Goal**: Comprehensive metrics
**Status**: Complete ✓

**Implementation**: `internal/metrics/metrics.go`

**Tasks**:
- [x] Request metrics: `redirector_requests_total`, `redirector_request_duration_seconds`
- [x] Config metrics: `redirector_config_reload_total`, `redirector_config_rules_count`
- [x] System metrics: goroutines, memory
- [x] Implement /metrics endpoint

### Stage 6.2: Structured Logging
**Goal**: JSON logging with context
**Status**: Complete ✓

**Implementation**: `internal/logging/logging.go`

**Tasks**:
- [x] Configure log levels (debug, info, warn, error)
- [x] JSON output format via zerolog
- [x] File rotation support with lumberjack
- [x] Request logging with context (path, status, duration)

### Stage 6.3: Distributed Tracing
**Goal**: Request tracing support
**Status**: Complete ✓

**Implementation**: `internal/tracing/tracing.go`

**Tasks**:
- [x] OpenTelemetry integration
- [x] Trace context propagation
- [x] Span creation for key operations
- [x] Export to Jaeger/Zipkin/OTLP
- [x] Configurable sampling rate

### Stage 6.4: Health and Diagnostics
**Goal**: Operational endpoints
**Status**: Complete ✓

**Tasks**:
- [x] `/health` endpoint (liveness)
- [x] `/ready` endpoint (readiness)
- [x] `/debug/pprof` endpoints for profiling
- [x] `/debug/vars` for runtime variables

---

## Phase 7: Performance and Load Testing
**Goal**: Establish and maintain performance baselines
**Status**: Complete ✓

### Stage 7.1: Benchmark Suite
**Goal**: Micro-benchmarks for critical paths
**Status**: Complete ✓

**Implementation**: `internal/router/router_test.go`

**Tasks**:
- [x] Benchmark exact path lookup
- [x] Benchmark prefix matching
- [x] Benchmark regex matching
- [x] Benchmark glob matching
- [x] Benchmark config reload
- [x] Memory allocation profiling

### Stage 7.2: Load Testing Infrastructure
**Goal**: Realistic load testing capability
**Status**: Complete ✓

**Implementation**: `test/load/`

**Tasks**:
- [x] k6 test scripts for smoke, load, stress, spike scenarios
- [x] CI integration for regression testing
- [x] Go benchmark regression detection with benchstat

### Stage 7.3: Performance Targets
**Goal**: Define and validate performance SLAs
**Status**: Complete ✓

**Targets**:
| Metric | Target | Status |
|--------|--------|--------|
| Latency p50 | < 100μs | ✓ |
| Latency p99 | < 1ms | ✓ |
| Throughput | > 100k req/s | ✓ |
| Memory | < 100MB | ✓ |
| Startup | < 1s | ✓ |
| Config reload | < 100ms | ✓ |

---

## Phase 8: Enterprise Features
**Goal**: Features for large-scale deployments
**Status**: Mostly Complete

### Stage 8.1: Multi-Tenancy
**Goal**: Isolated tenant configurations
**Status**: Not Started

**Tasks**:
- [ ] Tenant identification (header, subdomain)
- [ ] Per-tenant rule sets
- [ ] Tenant-level metrics
- [ ] Resource limits per tenant

### Stage 8.2: Access Control
**Goal**: Fine-grained permissions
**Status**: Complete ✓

**Implementation**: `internal/auth/`

**Tasks**:
- [x] API authentication (JWT, API keys)
- [x] Role-based access control (admin, reload, read, stats:read, stats:write)
- [x] Audit logging for all changes
- [x] IP allowlisting for management API (secure by default)

### Stage 8.3: Rate Limiting
**Goal**: Protect against abuse
**Status**: Complete ✓

**Implementation**: `internal/ratelimit/ratelimit.go`

**Tasks**:
- [x] Global rate limits
- [x] Per-IP rate limits
- [x] Per-path rate limits
- [x] Configurable limits in config
- [x] Rate limit headers (X-RateLimit-*)
- [x] Exempt IPs configuration
- [x] Auto-cleanup of stale limiters

### Stage 8.4: TUI Dashboard
**Goal**: Terminal-based management interface
**Status**: Complete ✓

**Implementation**: `cmd/redirector-tui/main.go`

**Tasks**:
- [x] Real-time metrics visualization
- [x] Rule list and status
- [x] Configuration management
- [x] Built with charmbracelet/bubbletea

---

## Additional Tools

### Configuration Linter
**Status**: Complete ✓

**Implementation**: `cmd/redirector-lint/main.go`

**Tasks**:
- [x] Configuration validation
- [x] Rule conflict detection
- [x] JSON and colored output modes
- [x] CI-friendly exit codes

### Stats Collector
**Status**: Complete ✓

**Implementation**: `internal/stats/stats.go`

**Tasks**:
- [x] Real-time request statistics
- [x] Per-rule statistics
- [x] Sampling support for high-volume
- [x] Live stream endpoint

---

## Deployment

### Helm Chart
**Status**: Complete ✓

**Implementation**: `charts/the-redirector/`

**Tasks**:
- [x] Deployment template
- [x] Service and Ingress
- [x] ConfigMap generation
- [x] HPA and PDB
- [x] ServiceMonitor for Prometheus
- [x] Security contexts (non-root, read-only fs)

### CI/CD
**Status**: Complete ✓

**Implementation**: `.github/workflows/`

**Tasks**:
- [x] Build and test workflow
- [x] Performance benchmark workflow
- [x] Benchmark regression detection
- [x] PR comments with results

---

## Remaining Work

### Priority 1 (Recommended)
- [ ] GitLab Integration (Phase 4.4)
- [ ] AWS Parameter Store (Phase 5.2)

### Priority 2 (Nice to Have)
- [ ] AWS Secrets Manager (Phase 5.3)
- [ ] Multi-cloud providers (Phase 5.4)
- [ ] Multi-Tenancy (Phase 8.1)

---

## Technology Stack

### Core
- **Language**: Go 1.21+
- **HTTP Server**: `github.com/valyala/fasthttp`
- **Router**: Custom implementation with radix-like efficiency

### Configuration
- **YAML**: `gopkg.in/yaml.v3`
- **File watching**: `github.com/fsnotify/fsnotify`

### AWS Integration
- **SDK**: `github.com/aws/aws-sdk-go-v2`

### Observability
- **Metrics**: `github.com/prometheus/client_golang`
- **Logging**: `github.com/rs/zerolog`
- **Tracing**: `go.opentelemetry.io/otel`

### Security
- **JWT**: `github.com/golang-jwt/jwt/v5`
- **Rate Limiting**: `golang.org/x/time/rate`

### Testing
- **Unit tests**: `testing` (stdlib)
- **Assertions**: `github.com/stretchr/testify`
- **Load testing**: k6

### Build & Deploy
- **Container**: Docker multi-stage build
- **CI/CD**: GitHub Actions
- **Kubernetes**: Helm chart
