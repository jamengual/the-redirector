# Implementation Plan: The Redirector

## Overview

This document outlines the phased implementation approach for building The Redirector, a high-performance vanity URL redirect service in Go. Each phase builds upon the previous, allowing for incremental delivery and validation.

---

## Phase 1: Core Foundation
**Goal**: Minimal viable redirect server with basic functionality
**Duration**: Foundation milestone
**Success Criteria**: Server handles 10k req/s with exact path matching

### Stage 1.1: Project Setup
**Goal**: Establish project structure and tooling
**Status**: Complete

**Tasks**:
- [x] Initialize Go module (`go mod init github.com/your-org/the-redirector`)
- [x] Set up directory structure:
  ```
  /cmd/redirector/      - Main application entry
  /internal/config/     - Configuration handling
  /internal/server/     - HTTP server
  /internal/router/     - Routing logic
  /internal/metrics/    - Prometheus metrics
  /pkg/redirect/        - Public redirect types
  /test/                - Integration tests
  /benchmarks/          - Performance tests
  ```
- [x] Configure linting (golangci-lint via Makefile)
- [x] Set up Makefile with common targets
- [x] Add .gitignore for Go projects
- [x] Create Dockerfile (multi-stage build)

**Tests**:
- [x] Project compiles with `go build ./...`
- [ ] Linter passes with `golangci-lint run`

### Stage 1.2: Basic HTTP Server
**Goal**: HTTP server with health endpoints
**Status**: Not Started

**Tasks**:
- [ ] Implement fasthttp server wrapper
- [ ] Add graceful shutdown handling
- [ ] Create `/health` endpoint (liveness)
- [ ] Create `/ready` endpoint (readiness)
- [ ] Add structured logging (zerolog or zap)
- [ ] Basic configuration via environment variables

**Tests**:
- Server starts and responds to health checks
- Graceful shutdown completes pending requests
- Logs output in JSON format

**Code Design Decision**:
Choose between `net/http` and `fasthttp`:
- `fasthttp`: Higher throughput, zero-alloc, but different API
- `net/http`: Standard library, broader middleware ecosystem

**Recommendation**: Start with `fasthttp` for maximum performance.

### Stage 1.3: Simple Redirect Logic
**Goal**: Basic exact-path redirects
**Status**: Not Started

**Tasks**:
- [ ] Define redirect rule struct:
  ```go
  type Rule struct {
      ID          string
      Path        string      // Exact path to match
      Destination string      // Redirect target URL
      StatusCode  int         // 301, 302, 307, 308
      Headers     map[string]string
  }
  ```
- [ ] Implement in-memory rule storage
- [ ] Create redirect handler
- [ ] Add response headers support
- [ ] Implement 301/302/307/308 redirects

**Tests**:
- Exact path `/foo` redirects to configured destination
- Correct status code returned
- Custom headers included in response
- Unknown paths return 404

### Stage 1.4: YAML Configuration
**Goal**: Load rules from YAML file
**Status**: Not Started

**Tasks**:
- [ ] Define configuration schema
- [ ] Implement YAML parser (gopkg.in/yaml.v3)
- [ ] Configuration validation
- [ ] Load on startup
- [ ] Error handling for invalid config

**Tests**:
- Valid YAML loads successfully
- Invalid YAML returns descriptive error
- Missing required fields detected
- Server starts with sample config

---

## Phase 2: Advanced Routing
**Goal**: Support regex, glob, and prefix matching
**Success Criteria**: Complex patterns work correctly, throughput > 50k req/s

### Stage 2.1: Radix Tree Router
**Goal**: Efficient prefix matching
**Status**: Not Started

**Tasks**:
- [ ] Integrate `github.com/fasthttp/router` or implement custom radix tree
- [ ] Support static path segments
- [ ] Support wildcard segments (`*`)
- [ ] Support catch-all segments (`**`)
- [ ] Implement prefix matching

**Tests**:
- `/api/*` matches `/api/anything`
- `/docs/**` matches `/docs/a/b/c`
- Prefix routes prioritized correctly
- Benchmark: < 100ns per lookup

### Stage 2.2: Regex Pattern Matching
**Goal**: Full regex support with capture groups
**Status**: Not Started

**Tasks**:
- [ ] Implement regex rule type
- [ ] Support PCRE-style patterns
- [ ] Capture group extraction ($1, $2, etc.)
- [ ] Substitution in destination URL
- [ ] Compile patterns on load (not per-request)
- [ ] Cache compiled regexes

**Approaches to Consider**:
1. **Go stdlib regexp**: Safe, no crashes, but slower (RE2 engine)
2. **github.com/dlclark/regexp2**: .NET-style regex, more features
3. **github.com/grafana/regexp**: Optimized Go regexp

**Recommendation**: Start with `regexp` stdlib, optimize later if needed.

**Tests**:
- Pattern `^/product/(\d+)$` captures product ID
- Substitution `/item/$1` works correctly
- Invalid regex returns config error
- Benchmark: < 1μs per regex match

### Stage 2.3: Glob Pattern Support
**Goal**: User-friendly glob patterns
**Status**: Not Started

**Tasks**:
- [ ] Implement glob to regex conversion
- [ ] Support patterns:
  - `*` - single segment wildcard
  - `**` - multi-segment wildcard
  - `?` - single character
  - `[abc]` - character class
- [ ] Path segment preservation logic

**Tests**:
- `/api/*/users` matches `/api/v1/users` and `/api/v2/users`
- `/docs/**` matches any depth under `/docs/`
- Glob patterns convert correctly to regex

### Stage 2.4: Rule Priority and Ordering
**Goal**: Deterministic rule matching
**Status**: Not Started

**Tasks**:
- [ ] Define matching priority:
  1. Exact match (highest)
  2. Prefix match (longer prefix wins)
  3. Regex match (order in config)
  4. Glob match (order in config)
- [ ] Implement priority sorting
- [ ] Add `priority` field for manual override
- [ ] First-match-wins semantics

**Tests**:
- Exact match takes precedence over regex
- Longer prefix beats shorter prefix
- Priority field overrides default order

---

## Phase 3: Configuration Management
**Goal**: Dynamic configuration without restarts
**Success Criteria**: Config reload < 100ms, zero dropped requests

### Stage 3.1: Configuration Abstraction
**Goal**: Pluggable configuration sources
**Status**: Not Started

**Tasks**:
- [ ] Define ConfigSource interface:
  ```go
  type ConfigSource interface {
      Load(ctx context.Context) (*Config, error)
      Watch(ctx context.Context) (<-chan *Config, error)
  }
  ```
- [ ] Implement FileConfigSource
- [ ] Support multiple file formats (YAML, JSON, TOML)
- [ ] Configuration merging from multiple sources

**Tests**:
- Interface contracts enforced
- File source loads correctly
- JSON and TOML parsers work

### Stage 3.2: Hot Reload
**Goal**: Zero-downtime configuration updates
**Status**: Not Started

**Tasks**:
- [ ] Implement atomic configuration swap
- [ ] File watcher for local files (fsnotify)
- [ ] Debounce rapid changes
- [ ] Validation before applying
- [ ] Rollback on invalid config
- [ ] Metrics for reload success/failure

**Design Decision**: Atomic swap vs incremental update
- **Atomic swap**: Simpler, consistent state, brief memory spike
- **Incremental**: Lower memory, complex state management

**Recommendation**: Atomic swap for simplicity and correctness.

**Tests**:
- Config changes apply without restart
- Invalid config rejected, old config retained
- No requests dropped during reload
- Metrics track reload events

### Stage 3.3: REST API for Configuration
**Goal**: Push configuration via HTTP API
**Status**: Not Started

**Tasks**:
- [ ] Implement management API server (separate port)
- [ ] POST /api/v1/config - full config replacement
- [ ] POST /api/v1/config/validate - validate without applying
- [ ] POST /api/v1/reload - trigger reload from sources
- [ ] GET /api/v1/config - retrieve current config
- [ ] Authentication (API key, JWT)
- [ ] Rate limiting on management endpoints

**Tests**:
- API updates config successfully
- Validation endpoint catches errors
- Authentication enforced
- Concurrent updates handled safely

### Stage 3.4: Environment Variable Injection
**Goal**: Dynamic values from environment
**Status**: Not Started

**Tasks**:
- [ ] Template syntax: `${ENV_VAR}` or `${ENV_VAR:-default}`
- [ ] Process templates on config load
- [ ] Support in destination URLs
- [ ] Support in header values
- [ ] Mask sensitive values in logs

**Tests**:
- Environment variables substituted
- Default values work
- Missing vars without default cause error
- Sensitive values masked in debug output

---

## Phase 4: Config-Syncer Service (Decoupled Architecture)
**Goal**: Separate service for configuration management with pluggable sources
**Success Criteria**: Config-Syncer pushes config to Redirector via API, supports VCS and cloud sources

### Stage 4.1: Pluggable Source Interface
**Goal**: Define extensible interface for community contributions
**Status**: Complete

**Tasks**:
- [x] Define Source interface with Fetch, Watch, Validate, Close
- [x] Create SourceRegistry for dynamic source registration
- [x] Implement FileSource as reference implementation
- [x] Document interface in CONTRIBUTING.md

**Tests**:
- Source interface contract documented
- FileSource loads and watches files
- Registry manages source factories

### Stage 4.2: GitHub Integration (VCS)
**Goal**: GitHub repository as configuration source with GitHub App auth
**Status**: In Progress

**Tasks**:
- [x] Implement GitHubSource with GitHub App authentication
- [x] Support release-based deployments (production)
- [x] Support branch-based deployments (staging)
- [x] Support tag-based deployments
- [ ] Implement webhook handler for real-time updates
- [ ] Add JWT generation for GitHub App auth
- [ ] Integration tests with GitHub API

**Design Decisions**:
- **GitHub App over PAT**: Better security, fine-grained permissions, audit trail
- **Release-based deploys**: Only publish releases triggers production updates
- **Branch mapping**: environment -> branch (production -> main, staging -> staging)

**Tests**:
- GitHub App token refresh works
- Release webhook triggers config fetch
- Branch tracking detects pushes
- Rate limiting handled gracefully

### Stage 4.3: Config-Syncer Service
**Goal**: Standalone service that coordinates config sources and pushes to redirectors
**Status**: Not Started

**Tasks**:
- [ ] Create config-syncer binary in cmd/config-syncer/
- [ ] Multi-source aggregation and merging
- [ ] Push config to multiple redirector targets
- [ ] Health checks for sources and targets
- [ ] Webhook HTTP server for GitHub/GitLab events
- [ ] Retry logic with exponential backoff

**Tests**:
- Syncer fetches from multiple sources
- Config pushed to all healthy targets
- Webhook events processed correctly
- Failed targets don't block others

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
**Success Criteria**: Reliable sync from S3, Parameter Store, etc.

### Stage 5.1: AWS S3 Integration
**Goal**: Load and watch configuration from S3
**Status**: Not Started

**Tasks**:
- [ ] Implement S3ConfigSource
- [ ] Support for S3 bucket + key configuration
- [ ] Polling-based updates (configurable interval)
- [ ] S3 event notifications via SQS (optional)
- [ ] IAM role authentication
- [ ] Error handling and retry logic

**Tests**:
- Config loads from S3 bucket
- Polling detects changes
- IAM authentication works
- Network errors handled gracefully

### Stage 5.2: AWS Parameter Store
**Goal**: Configuration from SSM Parameter Store
**Status**: Not Started

**Tasks**:
- [ ] Implement ParameterStoreConfigSource
- [ ] Support single parameter or parameter path
- [ ] SecureString decryption
- [ ] Hierarchical parameter organization
- [ ] Polling for updates

**Tests**:
- Parameters load correctly
- SecureString decrypted
- Hierarchical paths work
- Change detection functions

### Stage 5.3: AWS Secrets Manager
**Goal**: Sensitive configuration from Secrets Manager
**Status**: Not Started

**Tasks**:
- [ ] Implement SecretsManagerConfigSource
- [ ] Automatic secret rotation handling
- [ ] Version/stage selection
- [ ] Caching with TTL

**Tests**:
- Secrets retrieved successfully
- Rotation handled
- Cache reduces API calls

### Stage 5.4: Additional Cloud Providers
**Goal**: Multi-cloud support
**Status**: Not Started

**Tasks**:
- [ ] Azure Blob Storage integration
- [ ] GCP Cloud Storage integration
- [ ] HashiCorp Consul integration
- [ ] etcd integration

**Tests**:
- Each provider loads config correctly
- Authentication works for each provider
- Error handling consistent

### Stage 5.5: Configuration Versioning
**Goal**: Track and audit configuration changes
**Status**: Not Started

**Tasks**:
- [ ] Version tracking for each config load
- [ ] Config diff on reload
- [ ] Audit log of changes
- [ ] Rollback to previous version
- [ ] Config history retention

**Tests**:
- Versions increment on change
- Diff accurately shows changes
- Rollback restores previous config
- History accessible via API

---

## Phase 6: Observability and Operations
**Goal**: Production-ready monitoring and debugging
**Success Criteria**: Full visibility into system behavior

### Stage 6.1: Prometheus Metrics
**Goal**: Comprehensive metrics
**Status**: Not Started

**Tasks**:
- [ ] Request metrics:
  - `redirector_requests_total{status,path,rule_id}`
  - `redirector_request_duration_seconds`
  - `redirector_request_size_bytes`
- [ ] Config metrics:
  - `redirector_config_reload_total{status}`
  - `redirector_config_rules_count`
  - `redirector_config_last_reload_timestamp`
- [ ] System metrics:
  - `redirector_goroutines`
  - `redirector_memory_bytes`
- [ ] Implement /metrics endpoint

**Tests**:
- Metrics endpoint returns Prometheus format
- Counters increment correctly
- Histograms capture latency distribution

### Stage 6.2: Structured Logging
**Goal**: JSON logging with context
**Status**: Not Started

**Tasks**:
- [ ] Configure log levels (debug, info, warn, error)
- [ ] Request logging with:
  - Request ID
  - Source IP
  - Path
  - Matched rule
  - Response status
  - Duration
- [ ] Sensitive data masking
- [ ] Log sampling for high-volume

**Tests**:
- Logs output valid JSON
- All fields present
- Sensitive data masked
- Log level filtering works

### Stage 6.3: Distributed Tracing
**Goal**: Request tracing support
**Status**: Not Started

**Tasks**:
- [ ] OpenTelemetry integration
- [ ] Trace context propagation
- [ ] Span creation for key operations
- [ ] Export to Jaeger/Zipkin/OTLP

**Tests**:
- Traces exported correctly
- Context propagated through handlers
- Spans include relevant metadata

### Stage 6.4: Health and Diagnostics
**Goal**: Operational endpoints
**Status**: Not Started

**Tasks**:
- [ ] Detailed /health with checks:
  - Config source connectivity
  - Memory usage
  - Goroutine count
- [ ] /debug/pprof endpoints (optional, gated)
- [ ] /debug/config dump (masked secrets)
- [ ] /debug/rules - list all active rules

**Tests**:
- Health check reports accurate status
- Profiling endpoints work
- Config dump masks sensitive values

---

## Phase 7: Performance and Load Testing
**Goal**: Establish and maintain performance baselines
**Success Criteria**: Documented performance characteristics

### Stage 7.1: Benchmark Suite
**Goal**: Micro-benchmarks for critical paths
**Status**: Not Started

**Tasks**:
- [ ] Benchmark exact path lookup
- [ ] Benchmark prefix matching
- [ ] Benchmark regex matching
- [ ] Benchmark config reload
- [ ] Memory allocation profiling
- [ ] CPU profiling

**Tests**:
- Benchmarks run with `go test -bench`
- Results recorded in benchmark history
- No performance regressions

### Stage 7.2: Load Testing Infrastructure
**Goal**: Realistic load testing capability
**Status**: Not Started

**Tools to Evaluate**:
1. **k6**: JavaScript scripting, CI/CD friendly, good reports
2. **wrk**: Maximum throughput testing, Lua scripting
3. **hey**: Simple HTTP load testing, quick checks
4. **go-wrk**: Go-native, similar to wrk

**Recommendation**: Use k6 for scripted scenarios, wrk for max throughput.

**Tasks**:
- [ ] k6 test scripts for:
  - Sustained load testing
  - Spike testing
  - Soak testing (long duration)
  - Stress testing (find breaking point)
- [ ] wrk scripts for throughput baseline
- [ ] Grafana dashboard for test results
- [ ] CI integration for regression testing

### Stage 7.3: Performance Targets
**Goal**: Define and validate performance SLAs
**Status**: Not Started

**Targets**:
| Metric | Target | Measurement |
|--------|--------|-------------|
| Latency p50 | < 100μs | wrk at 10k req/s |
| Latency p99 | < 1ms | wrk at 10k req/s |
| Throughput | > 100k req/s | wrk, single instance |
| Memory | < 100MB | 100k rules loaded |
| Startup | < 1s | Cold start to ready |
| Config reload | < 100ms | 100k rules |

**Tasks**:
- [ ] Establish baseline measurements
- [ ] Document hardware requirements
- [ ] Create scaling guidelines
- [ ] Performance tuning guide

---

## Phase 8: Enterprise Features
**Goal**: Features for large-scale deployments
**Success Criteria**: Multi-tenant, secure, auditable

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
**Status**: Not Started

**Tasks**:
- [ ] API authentication (JWT, API keys)
- [ ] Role-based access control
- [ ] Audit logging for all changes
- [ ] IP allowlisting for management API

### Stage 8.3: Rate Limiting
**Goal**: Protect against abuse
**Status**: Not Started

**Tasks**:
- [ ] Per-path rate limits
- [ ] Per-IP rate limits
- [ ] Configurable limits in rules
- [ ] Rate limit headers (X-RateLimit-*)

### Stage 8.4: Admin Dashboard (Optional)
**Goal**: Visual management interface
**Status**: Not Started

**Tasks**:
- [ ] Web UI for rule management
- [ ] Real-time metrics visualization
- [ ] Configuration editor with validation
- [ ] User management

---

## Recommended Technology Stack

### Core
- **Language**: Go 1.21+
- **HTTP Server**: `github.com/valyala/fasthttp`
- **Router**: `github.com/fasthttp/router` (radix tree)

### Configuration
- **YAML**: `gopkg.in/yaml.v3`
- **JSON**: `encoding/json` (stdlib)
- **TOML**: `github.com/pelletier/go-toml/v2`
- **File watching**: `github.com/fsnotify/fsnotify`

### AWS Integration
- **SDK**: `github.com/aws/aws-sdk-go-v2`

### Observability
- **Metrics**: `github.com/prometheus/client_golang`
- **Logging**: `github.com/rs/zerolog` or `go.uber.org/zap`
- **Tracing**: `go.opentelemetry.io/otel`

### Testing
- **Unit tests**: `testing` (stdlib)
- **Assertions**: `github.com/stretchr/testify`
- **Load testing**: k6, wrk
- **Mocking**: `github.com/golang/mock`

### Build & Deploy
- **Container**: Docker multi-stage build
- **CI/CD**: GitHub Actions
- **Helm chart**: For Kubernetes deployment

---

## Risk Assessment

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| fasthttp API changes | Low | Medium | Pin version, abstract interface |
| Regex performance | Medium | High | Compile on load, benchmark |
| Memory with large configs | Medium | Medium | Profile, streaming load |
| Cloud provider SDK changes | Low | Low | Abstract provider interface |
| Configuration race conditions | Medium | High | Atomic swap, mutex protection |

---

## Decision Log

| Date | Decision | Rationale | Alternatives Considered |
|------|----------|-----------|------------------------|
| TBD | Use fasthttp | Max performance for simple workload | net/http, fiber |
| TBD | Radix tree router | O(k) lookup, memory efficient | hash map, linear search |
| TBD | Atomic config swap | Simpler, consistent state | Incremental updates |
| TBD | YAML as primary config | Human readable, comments | JSON, TOML, HCL |

---

## Getting Started

Once ready to begin implementation:

1. Review this plan and adjust timelines
2. Start with Phase 1, Stage 1.1
3. Mark stages as complete in this document
4. Write tests before implementation
5. Benchmark after each stage
6. Document decisions as made
