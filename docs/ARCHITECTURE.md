# Architecture: Decoupled Services Design

## Overview

The Redirector follows a **decoupled services architecture** that separates concerns between fast request handling and configuration management.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              EXTERNAL SOURCES                                │
├─────────────┬─────────────┬─────────────┬─────────────┬────────────────────┤
│   GitHub    │    GitLab   │     S3      │ Param Store │    Consul/etcd     │
│   (VCS)     │    (VCS)    │   (Cloud)   │   (Cloud)   │      (K/V)         │
└──────┬──────┴──────┬──────┴──────┬──────┴──────┬──────┴─────────┬──────────┘
       │             │             │             │                │
       └─────────────┴─────────────┴──────┬──────┴────────────────┘
                                          │
                                          ▼
                        ┌─────────────────────────────────────┐
                        │       CONFIG-SYNCER SERVICE         │
                        │                                     │
                        │  • Pulls from multiple sources      │
                        │  • Validates configuration          │
                        │  • Transforms/merges configs        │
                        │  • Pushes to redirector via API     │
                        │  • Handles webhooks (GitHub, etc)   │
                        │  • Manages versioning/rollback      │
                        └───────────────┬─────────────────────┘
                                        │
                                        │ Config Push (REST API)
                                        │ POST /api/v1/config
                                        ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                           REDIRECTOR SERVICE                                 │
│                                                                             │
│   ┌─────────────┐    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐ │
│   │   Ingress   │───▶│   Router    │───▶│  Redirect   │───▶│  Response   │ │
│   │  (fasthttp) │    │ (radix tree)│    │   Handler   │    │  (3xx)      │ │
│   └─────────────┘    └─────────────┘    └─────────────┘    └─────────────┘ │
│                                                                             │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                     In-Memory Rule Store                             │   │
│   │   • Atomic swap on config update                                    │   │
│   │   • Zero-copy reads during request handling                          │   │
│   │   • No external dependencies                                         │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
│   ┌─────────────┐    ┌─────────────┐    ┌─────────────┐                    │
│   │   Metrics   │    │   Health    │    │  Management │                    │
│   │ (Prometheus)│    │   Checks    │    │     API     │                    │
│   └─────────────┘    └─────────────┘    └─────────────┘                    │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Two-Service Model

### 1. Redirector Service

**Responsibility**: Handle HTTP redirects as fast as possible.

**Characteristics**:
- Single purpose: receive request → match rule → return redirect
- Zero external dependencies during request handling
- Stateless (config is pushed, not pulled)
- Horizontally scalable
- Sub-millisecond latency target

**API**:
```
# Request handling (port 8080)
GET /* -> 301/302/307/308 redirect or 404

# Management (port 8081)
GET  /health              -> Liveness check
GET  /ready               -> Readiness check
GET  /metrics             -> Prometheus metrics
POST /api/v1/config       -> Receive new config (from syncer)
GET  /api/v1/config       -> Return current config hash/version
POST /api/v1/config/validate -> Validate config without applying
```

### 2. Config-Syncer Service

**Responsibility**: Fetch, validate, and push configuration from various sources.

**Characteristics**:
- Handles all external dependencies (VCS, cloud storage, etc.)
- Can run as sidecar or separate service
- Supports multiple sync strategies (polling, webhooks, event-driven)
- Manages configuration versioning and rollback
- Single point of config management

**Supported Sources**:

| Source | Pull Method | Real-time Updates |
|--------|-------------|-------------------|
| GitHub/GitLab | Poll / Webhook | Yes (webhook) |
| AWS S3 | Poll / S3 Events | Yes (SQS/SNS) |
| AWS Parameter Store | Poll | No |
| AWS Secrets Manager | Poll | No |
| Azure Blob Storage | Poll | Yes (Event Grid) |
| GCP Cloud Storage | Poll / Pub/Sub | Yes (Pub/Sub) |
| HashiCorp Consul | Watch | Yes (watch) |
| etcd | Watch | Yes (watch) |
| HTTP/REST endpoint | Poll | Yes (webhook) |
| Local file | fsnotify | Yes |

## Communication Protocol

### Config Push API

```http
POST /api/v1/config HTTP/1.1
Host: redirector:8081
Content-Type: application/yaml
X-Config-Version: v1.2.3
X-Config-Source: github:main:abc123
Authorization: Bearer <token>

version: "1.0"
rules:
  - id: rule-1
    match:
      path: /foo
    redirect:
      to: https://example.com/bar
```

**Response**:
```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "status": "applied",
  "version": "v1.2.3",
  "rules_count": 150,
  "previous_version": "v1.2.2",
  "applied_at": "2024-01-15T10:30:00Z"
}
```

### Validation API

```http
POST /api/v1/config/validate HTTP/1.1
Content-Type: application/yaml

<config content>
```

**Response**:
```http
HTTP/1.1 200 OK
Content-Type: application/json

{
  "valid": true,
  "rules_count": 150,
  "warnings": [
    "Rule 'legacy-catch-all' has very low priority (-100)"
  ]
}
```

## Config-Syncer Design

### Core Components

```go
// Source interface for all config sources
type Source interface {
    // Name returns the source identifier
    Name() string

    // Fetch retrieves the current configuration
    Fetch(ctx context.Context) (*Config, error)

    // Watch returns a channel that receives updates
    Watch(ctx context.Context) (<-chan *Config, error)

    // SupportsWatch indicates if real-time updates are available
    SupportsWatch() bool
}

// Syncer coordinates config fetching and pushing
type Syncer struct {
    sources    []Source
    targets    []string       // Redirector endpoints
    validator  *Validator
    versioner  *Versioner

    pollInterval time.Duration
    retryPolicy  RetryPolicy
}
```

### Source Implementations

#### GitHub Source

```go
type GitHubSource struct {
    owner      string
    repo       string
    path       string
    branch     string
    token      string

    client     *github.Client
    lastSHA    string
}

// Supports both polling and webhook
func (g *GitHubSource) Watch(ctx context.Context) (<-chan *Config, error) {
    // Option 1: Poll for changes
    // Option 2: Listen for webhook events
}
```

#### S3 Source

```go
type S3Source struct {
    bucket     string
    key        string
    region     string

    client     *s3.Client
    lastETag   string
}

// Supports polling and S3 event notifications
func (s *S3Source) Watch(ctx context.Context) (<-chan *Config, error) {
    // Option 1: Poll with ETag check
    // Option 2: SQS queue for S3 events
}
```

### Configuration Example

```yaml
# config-syncer.yaml
version: "1.0"

# Where to push config
targets:
  - url: http://redirector-1:8081
    weight: 1
  - url: http://redirector-2:8081
    weight: 1
  - url: http://redirector-3:8081
    weight: 1

# Authentication for targets
auth:
  type: bearer
  token: ${SYNCER_AUTH_TOKEN}

# Config sources (merged in order, later sources override)
sources:
  # Base config from GitHub
  - type: github
    owner: my-org
    repo: redirect-config
    path: config/base.yaml
    branch: main
    webhook_secret: ${GITHUB_WEBHOOK_SECRET}

  # Environment-specific overrides from S3
  - type: s3
    bucket: my-config-bucket
    key: redirects/${ENVIRONMENT}/overrides.yaml
    region: us-east-1
    poll_interval: 30s

  # Secrets from AWS Secrets Manager
  - type: secrets_manager
    secret_name: redirect-api-keys
    region: us-east-1

# Sync settings
sync:
  poll_interval: 60s
  retry_attempts: 3
  retry_delay: 5s

# Validation rules
validation:
  max_rules: 100000
  require_https_destinations: true
  allowed_status_codes: [301, 302, 307, 308]

# Rollback settings
rollback:
  enabled: true
  keep_versions: 10
  auto_rollback_on_error: true
```

## Deployment Patterns

### Pattern 1: Sidecar (Kubernetes)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redirector
spec:
  template:
    spec:
      containers:
        # Main redirector container
        - name: redirector
          image: the-redirector:latest
          ports:
            - containerPort: 8080
            - containerPort: 8081

        # Config syncer sidecar
        - name: config-syncer
          image: the-redirector-syncer:latest
          env:
            - name: TARGET_URL
              value: "http://localhost:8081"
            - name: GITHUB_TOKEN
              valueFrom:
                secretKeyRef:
                  name: github-credentials
                  key: token
```

### Pattern 2: Separate Service

```yaml
# Dedicated config-syncer deployment
apiVersion: apps/v1
kind: Deployment
metadata:
  name: config-syncer
spec:
  replicas: 2  # HA for config management
  template:
    spec:
      containers:
        - name: config-syncer
          image: the-redirector-syncer:latest
          env:
            - name: TARGETS
              value: "http://redirector.default.svc:8081"
```

### Pattern 3: GitOps (ArgoCD/Flux)

The config-syncer can integrate with GitOps workflows:

1. Config changes pushed to Git
2. GitHub webhook triggers config-syncer
3. Config-syncer validates and pushes to all redirectors
4. ArgoCD/Flux tracks syncer deployment

## Benefits of Decoupled Architecture

### For Redirector Service

1. **Maximum Performance**: No I/O during request handling
2. **Predictable Latency**: No network calls to external services
3. **Simple Testing**: Mock config push, test redirect logic
4. **Easy Scaling**: Stateless, scale horizontally without coordination
5. **Minimal Attack Surface**: No credentials for external services

### For Config-Syncer Service

1. **Single Responsibility**: Only handles config synchronization
2. **Flexible Sources**: Easy to add new source types
3. **Centralized Validation**: One place for all config validation
4. **Audit Trail**: Track all config changes and versions
5. **Independent Updates**: Update syncer without touching redirectors

### For Operations

1. **Clear Boundaries**: Easy to debug issues
2. **Independent Deployments**: Update each service separately
3. **Flexible Scaling**: Scale based on actual needs
4. **Better Security**: Limit credential exposure
5. **Easier Testing**: Test each service in isolation

## Failure Modes

### Config-Syncer Failure

- Redirectors continue serving with last known config
- Alert on sync failures
- Manual config push as fallback

### Redirector Failure

- Load balancer routes around failed instances
- Config-syncer retries push to recovered instances
- New instances receive config on startup

### Source Unavailable

- Syncer uses cached config
- Alert on source failures
- Configurable fallback sources

## Future Enhancements

1. **Multi-Region Sync**: Coordinate config across regions
2. **Canary Deployments**: Roll out config changes gradually
3. **A/B Testing**: Route config variants to subset of instances
4. **Config Diffing**: Show changes before applying
5. **Slack/Teams Integration**: Notify on config changes
6. **Web UI**: Visual config management
