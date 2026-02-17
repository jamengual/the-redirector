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

### Stage 3.5: Compact Rule Formats
**Goal**: Simplified formats for managing large rule sets
**Status**: Complete ✓

**Implementation**: `internal/config/shortform.go`

**Tasks**:
- [x] Short-form YAML: `/old -> https://new.com [options]`
- [x] CSV format: `origin,destination,status,options`
- [x] Include directive: `rules_include: [file.csv, rules.yaml]`
- [x] Automatic pattern detection (exact, prefix, glob, regex)
- [x] Mixed format support (short-form + full YAML in same file)

---

## Phase 4: redirector-sync Service
**Goal**: Separate service for configuration management with pluggable sources and integrated linting
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

**Implementation**: `internal/providers/github.go`, `internal/providers/github_test.go`

**Tasks**:
- [x] Implement GitHubSource with GitHub App authentication
- [x] Support release-based deployments (production)
- [x] Support branch-based deployments (staging)
- [x] Support tag-based deployments
- [x] Webhook handler in redirector-sync for real-time updates
- [x] GitHub App JWT generation and installation token exchange (golang-jwt/jwt/v5)
- [x] PAT authentication support (GitHubPATAuth)
- [x] Private key loading from file or inline PEM (PKCS1/PKCS8)
- [x] Tag-based deployments with glob pattern matching (`tag_pattern` config)
- [x] Fixed getFileContent to use io.ReadAll instead of resp.ContentLength
- [x] Configurable baseURL for testability
- [x] Comprehensive test suite (github_test.go) including:
  - Constructor validation, auth types, strategies
  - JWT generation and installation token exchange (with httptest mock)
  - Token caching behavior
  - RSA private key parsing (PKCS1 and PKCS8)
  - Private key loading from file and inline PEM
  - Tag pattern matching (glob filtering in Fetch and HandleWebhook)
  - Validate, Fetch (release/branch/tag strategies), HandleWebhook
- [x] Integration test (test/integration/providers_test.go)
- [x] Fixed Accept header overwrite bug: `addAuthHeader()` was overwriting `Accept: application/vnd.github.raw` with `Accept: application/vnd.github+json`, causing GitHub Contents API to return JSON instead of raw file content
- [x] Accept header regression test (TestGitHubSource_getFileContent_AcceptHeader)
- [x] Debug logging (zerolog) for source creation, fetch flow, ref resolution, content parsing, and error diagnostics

### Stage 4.3: redirector-sync Service
**Goal**: Standalone service that coordinates config sources with integrated linting
**Status**: Complete ✓

**Implementation**: `cmd/redirector-sync/main.go`, `internal/syncer/syncer.go`

**Tasks**:
- [x] Create redirector-sync binary (renamed from config-syncer, absorbed redirector-lint)
- [x] Multi-source aggregation and merging
- [x] Push config to multiple redirector targets
- [x] Health checks for sources and targets
- [x] Webhook HTTP server for GitHub/GitLab events
- [x] Retry logic with exponential backoff
- [x] Refactored to use provider Registry (providerAdapter bridges Source -> ConfigSource)
- [x] Removed duplicate inline source implementations (fileSource, httpSource, githubSource)
- [x] Fixed `sourceConfigToMap` to map `ref` field to provider `environment` and default to `strategy: "branch"` when ref is set
- [x] Debug logging with secret redaction in source creation (`redactSecrets()` helper)

### Stage 4.4: GitLab Integration
**Goal**: GitLab repository support
**Status**: Complete ✓

**Implementation**: `internal/providers/gitlab.go`, `internal/providers/gitlab_test.go`

**Tasks**:
- [x] Implement GitLabSource
- [x] GitLab App or Project Token auth
- [x] OAuth2 authentication with automatic token refresh
- [x] Release and branch tracking
- [x] Tag-based deployments with glob pattern matching (`tag_pattern` config)
- [x] Webhook support (Release Hook, Push Hook, Tag Push Hook)
- [x] Comprehensive test suite (gitlab_test.go) including OAuth refresh tests

### Stage 4.5: Multi-Team Configuration
**Goal**: Support multiple teams with isolated config sources
**Status**: Complete ✓

**Implementation**: `internal/syncer/syncer.go`

**Tasks**:
- [x] Per-source prefixes (rules become `team/rule-id`)
- [x] Per-source allowed paths (restrict what paths a team can define)
- [x] Priority-based conflict resolution
- [x] Merge report with conflict tracking
- [x] Path conflict detection between sources
- [x] Merge warnings and audit trail

---

## Phase 5: Cloud Provider Configuration
**Goal**: Enterprise-grade configuration from cloud services
**Status**: Complete ✓

### Stage 4.5: HTTP Provider
**Goal**: HTTP/HTTPS endpoint as configuration source
**Status**: Complete ✓

**Implementation**: `internal/providers/http.go`, `internal/providers/http_test.go`

**Tasks**:
- [x] Implement HTTPSource with Registry registration ("http")
- [x] Bearer token and Basic auth support
- [x] Custom headers support
- [x] ETag-based caching (If-None-Match / 304 Not Modified)
- [x] Configurable timeout and poll interval
- [x] Validate via HEAD request
- [x] Comprehensive test suite (http_test.go) - 11 tests

### Stage 4.6: File Provider Tests
**Goal**: Unit tests for local file configuration source
**Status**: Complete ✓

**Implementation**: `internal/providers/file_test.go`

**Tasks**:
- [x] TestNewFileSource (constructor validation, poll interval)
- [x] TestFileSource_Name, _SupportsWatch, _Registry
- [x] TestFileSource_Validate (valid file, non-existent, directory)
- [x] TestFileSource_Fetch (valid file, non-existent)
- [x] TestFileSource_Close (including double-close safety, with watcher)
- [x] TestFileSource_DefaultPollInterval

---

## Phase 5: Cloud Provider Configuration
**Goal**: Enterprise-grade configuration from cloud services
**Status**: Complete ✓

### Stage 5.1: AWS S3 Integration
**Goal**: Load and watch configuration from S3
**Status**: Complete ✓

**Implementation**: `internal/providers/s3.go`, `internal/providers/s3_test.go`

**Tasks**:
- [x] Implement S3ConfigSource
- [x] Support for S3 bucket + key configuration
- [x] Polling-based updates (configurable interval)
- [x] S3 event notifications support
- [x] IAM role authentication
- [x] Cross-account access via role ARN
- [x] S3-compatible endpoints (MinIO)
- [x] Registry registration (init() with NewS3SourceFromMap)
- [x] SupportsWatch() method
- [x] Test suite (s3_test.go)

### Stage 5.2: AWS Parameter Store
**Goal**: Configuration from SSM Parameter Store
**Status**: Complete ✓

**Implementation**: `internal/providers/parameterstore.go`

**Tasks**:
- [x] Implement ParameterStoreConfigSource
- [x] Support single parameter or parameter path
- [x] SecureString decryption
- [x] Hierarchical parameter organization
- [x] Polling for updates

### Stage 5.3: AWS Secrets Manager
**Goal**: Sensitive configuration from Secrets Manager
**Status**: Complete ✓

**Implementation**: `internal/providers/secretsmanager.go`

**Tasks**:
- [x] Implement SecretsManagerConfigSource
- [x] Automatic secret rotation handling
- [x] Version/stage selection
- [x] Caching with TTL

### Stage 5.4: Additional Cloud Providers
**Goal**: Multi-cloud support
**Status**: Complete ✓

**Implementation**:
- `internal/providers/azureblob.go`
- `internal/providers/gcs.go`
- `internal/providers/consul.go`
- `internal/providers/etcd.go`

**Tasks**:
- [x] Azure Blob Storage integration
- [x] GCP Cloud Storage integration
- [x] HashiCorp Consul integration
- [x] etcd integration

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

**Implementation**: `cmd/redirector-sync/main.go` (lint mode), `internal/lint/lint.go`

**Tasks**:
- [x] Configuration validation
- [x] Rule conflict detection
- [x] JSON and colored output modes
- [x] CI-friendly exit codes
- [x] Multi-team conflict detection (automatic with multi-source syncer config)
- [x] Cross-source overlap detection
- [x] Per-source issue reporting

### Stats Collector
**Status**: Complete ✓

**Implementation**: `internal/stats/stats.go`

**Tasks**:
- [x] Real-time request statistics
- [x] Per-rule statistics
- [x] Sampling support for high-volume
- [x] Live stream endpoint

### TUI Dashboard
**Status**: Complete ✓

**Implementation**: `cmd/redirector-tui/main.go`

**Tasks**:
- [x] Live traffic view with sorting and filtering
- [x] Config status view with merge report
- [x] Multi-team conflict visualization
- [x] View switching (Tab key)
- [x] Syncer status integration (`--syncer-url`)

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

## Phase 9: Circular Redirect Detection & Mitigation
**Goal**: Detect redirect loops at config-time and protect against them at runtime
**Status**: Not Started

### Stage 9.1: Static Cycle Detection (Lint Check)
**Goal**: Detect circular redirect chains at lint/config-load time
**Status**: Complete ✓
**Success Criteria**: Lint catches direct cycles (A→B→A), transitive cycles (A→B→C→A), and self-loops for exact and prefix rules. Regex/glob cycles reported as warnings.

**Implementation**: `internal/lint/lint.go` — new `checkCircularRedirects()` method

**Approach**: Build a directed graph from redirect rules and detect cycles with DFS (three-color marking: white→gray→black). Each rule is an edge from its match path to its redirect destination.

**Edge Construction by Match Type**:
- **Exact**: Edge from `rule.Match.Path` → parsed path of `rule.Redirect.GetLocation()`
- **Prefix with PreservePath**: Edge from `rule.Match.Path` → parsed path of `rule.Redirect.GetLocation()` + check if destination falls within the prefix's match space (self-loop detection)
- **Regex/Glob**: Generate 3-5 sample paths using heuristics (e.g., replace `(\d+)` with `123`, `(.*)` with `test`), trace each sample through all rules. Report as `SeverityWarning` (best-effort, not provably correct)

**Graph Node Normalization**:
- Strip scheme and host from destinations that point back to the same service (detect via `match.host` or relative URLs)
- Normalize trailing slashes for comparison
- Only consider redirect rules (3xx status), skip non-redirect responses

**Cycle Detection Algorithm**:
1. Build adjacency list from redirect rules
2. DFS with three colors: unvisited, in-progress, done
3. When an in-progress node is revisited → cycle found
4. Track the full chain for error messages (e.g., "Circular redirect: rule-A → rule-B → rule-C → rule-A")

**Tasks**:
- [ ] Add `checkCircularRedirects()` method to `Linter`
- [ ] Implement `buildRedirectGraph()` helper — returns adjacency list of rule ID → destination rule IDs
- [ ] Implement `extractDestinationPath()` — parse redirect URL, normalize, return local path (or "" if external)
- [ ] Implement `findCycles()` — DFS cycle detection returning chains
- [ ] Handle `PreservePath` prefix self-loops (destination prefix matches source prefix)
- [ ] Best-effort regex/glob sample tracing (generate sample URLs, trace through rules)
- [ ] Register check in `Lint()` method
- [ ] Write tests: direct cycle, transitive cycle, self-loop, prefix self-loop, no-cycle (clean config), cross-host cycle, external destination (no cycle), regex sample-based warning
- [ ] Multi-source cycle detection: add `checkCrossSourceCycles()` to `MultiSourceLinter`

**Tests**: `internal/lint/lint_test.go`

### Stage 9.2: Runtime Loop Protection
**Goal**: Break redirect loops at request time via hop counter header
**Status**: Not Started
**Success Criteria**: Requests that bounce through the redirector more than N times (default: 10) receive 508 Loop Detected instead of another redirect. Zero performance impact on non-looping requests (single header read).

**Implementation**: `internal/server/server.go` — modify `handleRedirect()`

**Approach**:
1. On incoming request, read `X-Redirect-Count` header (integer, default 0)
2. If count >= max (configurable, default 10), return **508 Loop Detected** with diagnostic body
3. If count < max, set `X-Redirect-Count: count+1` on the redirect response
4. Add `redirector_loop_detected_total` counter metric

**Why X-Redirect-Count**:
- Only works when the redirector redirects back to itself (the most dangerous case)
- Zero cost on first-hop requests (just a header read)
- RFC 8586 CDN-Loop is designed for multi-CDN chains — this is simpler and fits the single-service case

**Configuration**:
```yaml
server:
  max_redirect_hops: 10  # default, 0 = disabled
```

**Tasks**:
- [ ] Add `MaxRedirectHops` field to `ServerConfig` (default: 10)
- [ ] Read `X-Redirect-Count` header in `handleRedirect()`, before rule matching
- [ ] If count >= max: return 508, increment metric, log warning with request path and chain length
- [ ] If redirect: set `X-Redirect-Count: count+1` on response
- [ ] Add `LoopDetectedTotal` counter to `Metrics` struct
- [ ] Wire metric in server
- [ ] Write tests: no header (first hop), header at max (508 response), header below max (incremented), disabled (max=0 bypasses check), non-redirect response (no header added)
- [ ] Update docs: MANAGEMENT_API.md (new metric), CONFIGURATION.md (new field), README.md (mention in DDoS section)

**Tests**: `internal/server/server_test.go`

### Stage 9.3: Lint CLI Output for Cycles
**Goal**: Clear, actionable lint output for circular redirect findings
**Status**: Not Started
**Success Criteria**: `redirector-sync --lint` shows circular redirect chains with visual arrows and suggested fixes

**Tasks**:
- [ ] Format cycle chains as: `⟳ Circular redirect detected: rule-A → rule-B → rule-C → rule-A`
- [ ] Include rule IDs, match paths, and destinations in the cycle report
- [ ] Suggest fixes: "Remove one rule from the chain or change the destination to break the cycle"
- [ ] JSON output includes cycle details for CI integration

**Tests**: `internal/lint/lint_test.go` (output formatting)

---

## Remaining Work

### Priority 1 (Recommended)
- [x] GitLab Integration (Phase 4.4) ✓
- [x] AWS Parameter Store (Phase 5.2) ✓

### Priority 2 (Nice to Have)
- [x] AWS Secrets Manager (Phase 5.3) ✓
- [x] Multi-cloud providers (Phase 5.4) ✓
- [ ] Multi-Tenancy (Phase 8.1)
- [ ] Circular Redirect Detection (Phase 9)

---

## Test Coverage Summary

All providers have comprehensive unit tests. Below is the test file mapping:

| Provider | Source File | Test File | Tests |
|----------|------------|-----------|-------|
| File | `file.go` | `file_test.go` | 12 tests (constructor, name, watch, registry, validate, fetch, close) |
| HTTP | `http.go` | `http_test.go` | 11 tests (constructor, name, watch, registry, validate, fetch with auth variants) |
| GitHub | `github.go` | `github_test.go` | 26+ tests (constructor, auth types, JWT, token caching, PEM parsing, tag patterns, strategies, webhooks, Accept header regression) |
| GitLab | `gitlab.go` | `gitlab_test.go` | 20+ tests (constructor, auth types, OAuth refresh, tag patterns, strategies, webhooks) |
| S3 | `s3.go` | `s3_test.go` | 8 tests (constructor, name, watch, registry, poll interval, close) |

Integration tests (`test/integration/providers_test.go`) cover: S3, Parameter Store, Secrets Manager, Azure Blob, GCS, Consul, etcd, and GitHub (httptest mock).

---

## Technology Stack

### Core
- **Language**: Go 1.25+
- **HTTP Server**: `github.com/valyala/fasthttp`
- **Router**: Custom implementation with radix-like efficiency

### Configuration
- **YAML**: `gopkg.in/yaml.v3`
- **File watching**: `github.com/fsnotify/fsnotify`

### Cloud Integration
- **AWS SDK**: `github.com/aws/aws-sdk-go-v2` (S3, SSM, Secrets Manager)
- **Azure SDK**: `github.com/Azure/azure-sdk-for-go` (Blob Storage)
- **GCP SDK**: `cloud.google.com/go/storage`
- **HashiCorp Consul**: `github.com/hashicorp/consul/api`
- **etcd**: `go.etcd.io/etcd/client/v3`

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
