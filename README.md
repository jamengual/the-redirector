# The Redirector

A high-performance, enterprise-grade URL redirect and response service built in Go.

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

## Overview

The Redirector handles URL redirects and custom responses at massive scale with sub-millisecond latency. Unlike general-purpose reverse proxies, it's purpose-built for redirect workloads with features designed for enterprise environments.

### Key Features

- **Blazing Fast**: Built on fasthttp with radix tree routing for < 1ms p99 latency
- **Flexible Matching**: Exact paths, prefixes, regex with capture groups, and glob wildcards
- **Any HTTP Response**: Not just redirects - return 404, 403, 503, or any status with custom bodies
- **Header Injection**: Add custom headers to any response
- **Multi-File Config**: Split rules across multiple YAML files for team organization
- **Live Monitoring**: htop-style TUI for real-time request debugging
- **Config Linting**: Detect duplicates, conflicts, and performance issues before deployment
- **Decoupled Architecture**: Separate config-syncer service for complex config management
- **Observable**: Stats endpoints, structured logging with rotation

## Components

| Binary | Description |
|--------|-------------|
| `redirector` | Main redirect server |
| `redirector-lint` | Config validator and analyzer |
| `redirector-tui` | Live monitoring dashboard |
| `config-syncer` | Multi-source config synchronization |

---

## Quick Start

### Build

```bash
# Build all binaries
go build ./...

# Or build individually
go build -o bin/redirector ./cmd/redirector
go build -o bin/redirector-lint ./cmd/redirector-lint
go build -o bin/redirector-tui ./cmd/redirector-tui
go build -o bin/config-syncer ./cmd/config-syncer
```

### Run

```bash
# Start the redirector
./bin/redirector -config config.yaml

# In another terminal, start the TUI monitor
./bin/redirector-tui -url http://localhost:8081

# Trigger immediate config reload
./bin/redirector sync
```

### Test

```bash
# Test a redirect
curl -I http://localhost:8080/old-home
# HTTP/1.1 301 Moved Permanently
# Location: https://example.com/
# X-Redirected-By: the-redirector
# X-Rule-ID: homepage-redirect
```

### CLI Commands

```bash
redirector                 # Start the server
redirector sync            # Trigger config reload
redirector version         # Show version
redirector help            # Show help
```

---

## Configuration

### Basic Structure

```yaml
version: "1.0"

server:
  port: 8080              # Redirect traffic port
  management_port: 8081   # Stats/health API port
  read_timeout: 5s
  write_timeout: 5s

defaults:
  status_code: 301
  preserve_query: true
  headers:
    X-Powered-By: "the-redirector"

# Optional: Enable live stats (disabled by default for performance)
stats:
  enabled: true
  buffer_size: 1000       # Ring buffer for live view
  sampling_rate: 1.0      # 1.0 = all, 0.01 = 1%

rules:
  - id: my-rule
    match:
      type: exact         # exact, prefix, regex, or glob
      path: /old-path
    redirect:
      to: https://example.com/new-path
      status: 301
```

---

## Rule Types

### Exact Match

Matches a specific path exactly.

```yaml
- id: homepage-redirect
  match:
    type: exact
    path: /old-home
  redirect:
    to: https://example.com/
    status: 301
```

### Prefix Match

Matches any path starting with the prefix. Use `preserve_path` to carry over the suffix.

```yaml
# /blog/hello-world -> https://blog.example.com/hello-world
- id: blog-redirect
  match:
    type: prefix
    path: /blog/
  redirect:
    to: https://blog.example.com/
    preserve_path: true
    status: 301
```

### Regex Match

Full regex support with capture groups (`$1`, `$2`, etc.).

```yaml
# /product/12345 -> https://shop.example.com/item/12345
- id: product-redirect
  match:
    type: regex
    pattern: ^/product/(\d+)$
  redirect:
    to: https://shop.example.com/item/$1
    status: 302

# Multiple captures
# /category/electronics/item/42 -> https://shop.example.com/electronics/42
- id: category-item
  match:
    type: regex
    pattern: ^/category/([^/]+)/item/(\d+)$
  redirect:
    to: https://shop.example.com/$1/$2
```

### Glob Match

User-friendly wildcard patterns.

| Pattern | Matches |
|---------|---------|
| `*` | Single path segment |
| `**` | Any depth (zero or more segments) |
| `?` | Single character |

```yaml
# /docs/v1/guide, /docs/v2/guide, etc.
- id: docs-version
  match:
    type: glob
    pattern: /docs/*/guide
  redirect:
    to: https://docs.example.com/
    preserve_path: true

# /legacy/anything/at/any/depth
- id: legacy-catch-all
  match:
    type: glob
    pattern: /legacy/**
  redirect:
    to: https://new.example.com/
    preserve_path: true
```

---

## Non-Redirect Responses

Return any HTTP status code with optional body - not just redirects.

### Block Requests (404/403)

```yaml
# Block WordPress admin probes
- id: block-wp-admin
  match:
    type: prefix
    path: /wp-admin
  redirect:
    status: 404
    body: "Not Found"

# Block common attack paths
- id: block-bots
  match:
    type: regex
    pattern: ^/(\.env|\.git|phpinfo|wp-login)
  redirect:
    status: 403
    body: "Forbidden"
    headers:
      X-Blocked: "true"
```

### Maintenance Mode (503)

```yaml
- id: maintenance
  match:
    type: glob
    pattern: /api/**
  redirect:
    status: 503
    body: "Service temporarily unavailable"
    headers:
      Retry-After: "3600"
  priority: 1000  # High priority overrides other rules
```

### Gone (410)

```yaml
- id: discontinued-product
  match:
    type: exact
    path: /old-product
  redirect:
    status: 410
    body: "This product has been discontinued"
```

---

## Host-Based Routing

Match requests by hostname for domain migrations.

```yaml
- id: old-domain-redirect
  match:
    type: prefix
    host: old.example.com
    path: /
  redirect:
    to: https://new.example.com/
    preserve_path: true
```

---

## Environment Variables

Use `${VAR}` or `${VAR:-default}` syntax in any string value.

```yaml
- id: api-gateway
  match:
    type: prefix
    path: /gateway/
  redirect:
    to: ${API_GATEWAY_URL:-https://gateway.example.com}/
    preserve_path: true
```

---

## Priority and Ordering

Rules are evaluated in this order:

1. **Priority field** (higher numbers first)
2. **Match type specificity**: exact > prefix > regex > glob
3. **Path length** (longer paths first for prefix matches)
4. **Config file order** (for same priority)

```yaml
# Fallback rule - negative priority ensures it's evaluated last
- id: fallback
  match:
    type: glob
    pattern: /**
  redirect:
    to: https://example.com/not-found
    status: 302
  priority: -100
```

---

## Multi-File Configuration

Split rules across multiple YAML files for team organization. Files are loaded alphabetically and merged.

```
config/
├── 00-defaults.yaml      # Server settings, defaults
├── 10-api-redirects.yaml # API team rules
├── 20-blog-redirects.yaml # Content team rules
└── 30-legacy.yaml        # Migration rules
```

```bash
# Load entire directory
./redirector --config ./config/
```

---

## CLI Tools

### redirector-lint

Validate configuration and detect issues before deployment.

```bash
# Basic validation
./redirector-lint config.yaml

# JSON output for CI/CD
./redirector-lint --json config.yaml

# Quiet mode (errors only)
./redirector-lint --quiet config.yaml
```

**Checks performed:**
- Duplicate rule IDs
- Overlapping patterns (rules that match same paths)
- Greedy patterns without negative priority
- Regex performance issues (nested quantifiers, multiple `.*`)
- Unreachable rules (shadowed by higher-priority rules)
- Missing default status codes

Example output:
```
The Redirector - Config Linter
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Loaded 25 rules

✗ ERRORS (1)
─────────────────────────────────
  [rule-5] Duplicate rule ID 'homepage' (first seen at index 0)

⚠ WARNINGS (2)
─────────────────────────────────
  [api-v1] Rule 'api-v1' may overlap with 'api-all': Prefix '/api/' is contained in '/api/v1/'
    → Consider setting different priorities to control matching order
  [catch-all] Greedy glob pattern '/**' will match many paths
    → Set a negative priority (e.g., -100) to ensure it's evaluated last

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Found: 1 errors, 2 warnings, 0 suggestions
```

### redirector-tui

Live monitoring dashboard with htop-style interface.

```bash
# Connect to local management API
./redirector-tui

# Connect to remote server
./redirector-tui --url http://redirector.internal:8081
```

**Features:**
- Real-time request stream
- Sort by time, status, latency, path, or rule (keys: 1-5)
- Filter requests (press `f`)
- Pause/resume (press `p` or space)
- Summary stats: uptime, requests/sec, errors, error rate
- Latency histogram

**Keyboard shortcuts:**
| Key | Action |
|-----|--------|
| `q` | Quit |
| `p` / `space` | Pause/resume |
| `f` / `/` | Filter mode |
| `c` | Clear filter |
| `r` | Force refresh |
| `1-5` | Sort by column |
| `↑/k`, `↓/j` | Navigate |
| `?` | Help |

---

## Management API

The management API runs on a separate port (default: 8081).

### Health Endpoints

```bash
# Liveness probe
curl http://localhost:8081/health
# {"status":"healthy"}

# Readiness probe
curl http://localhost:8081/ready
# {"status":"ready"}
```

### Stats Endpoints

```bash
# Summary statistics
curl http://localhost:8081/stats
# {
#   "uptime_seconds": 3600,
#   "total_requests": 150000,
#   "total_errors": 42,
#   "requests_per_second": 41.67,
#   "error_rate": 0.00028,
#   "status_counts": {"301": 149000, "404": 42, ...},
#   "latency_buckets": {"<100us": 120000, "<500us": 25000, ...},
#   "top_rules": [{"id": "api-redirect", "hits": 50000, ...}]
# }

# Live request stream (for TUI)
curl http://localhost:8081/stats/live?limit=100

# Stats for specific rule
curl http://localhost:8081/stats/rule/my-rule-id

# Enable/disable stats collection
curl -X POST http://localhost:8081/stats/enable
curl -X POST http://localhost:8081/stats/disable

# Reset stats
curl -X POST http://localhost:8081/stats/reset
```

### Config Endpoints

```bash
# Get current config info
curl http://localhost:8081/api/v1/config
# {"version":"1.0","rules_count":25}

# Get rules count
curl http://localhost:8081/api/v1/rules
# {"count":25}

# Trigger config reload (same as `redirector sync`)
curl -X POST http://localhost:8081/api/v1/reload
# {"status":"reloaded","rules_count":25,"duration_ms":15}
```

---

## Config-Syncer

Separate service for pulling configuration from multiple sources with failover.

### Configuration

```yaml
# syncer.yaml
sync_interval: 5m

sources:
  # Primary: S3
  - name: "s3-primary"
    type: s3
    priority: 100
    enabled: true
    s3:
      bucket: my-config-bucket
      key: config/redirector.yaml
      region: us-east-1

  # Fallback: Local file
  - name: "local-fallback"
    type: file
    priority: 1
    enabled: true
    file:
      path: /etc/redirector/config.yaml

output:
  type: file
  file:
    path: /var/lib/redirector/config.yaml
    atomic: true
```

### Run

```bash
# Continuous sync
./config-syncer --config syncer.yaml

# One-shot (fetch once and exit)
./config-syncer --config syncer.yaml --one-shot

# Dry run (fetch but don't write)
./config-syncer --config syncer.yaml --dry-run
```

---

## Logging

Structured JSON logging with automatic rotation.

```yaml
# In your main config or via CLI flags
logging:
  level: info          # debug, info, warn, error
  format: json         # json or console
  file:
    path: /var/log/redirector/redirector.log
    max_size_mb: 100   # Rotate at 100MB
    max_backups: 5     # Keep 5 old files
    max_age_days: 30   # Delete after 30 days
    compress: true     # Gzip rotated files
```

---

## Performance Tuning

### Server Settings

```yaml
server:
  port: 8080
  management_port: 8081
  read_timeout: 5s
  write_timeout: 5s
  idle_timeout: 120s
  max_connections: 100000
```

### Stats Impact

Stats collection is **disabled by default** for maximum performance. When enabled:

- Atomic counters add ~10ns per request
- Ring buffer adds ~50ns per request (with sampling, less)
- Use `sampling_rate: 0.01` for 1% sampling on high-traffic deployments

```yaml
stats:
  enabled: true
  buffer_size: 1000
  sampling_rate: 0.01  # Sample 1% of requests
```

---

## Docker

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o redirector ./cmd/redirector

FROM alpine:latest
COPY --from=builder /app/redirector /usr/local/bin/
COPY config.yaml /etc/redirector/
EXPOSE 8080 8081
CMD ["redirector", "--config", "/etc/redirector/config.yaml"]
```

```bash
docker build -t the-redirector .
docker run -p 8080:8080 -p 8081:8081 the-redirector
```

---

## Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redirector
spec:
  replicas: 3
  selector:
    matchLabels:
      app: redirector
  template:
    metadata:
      labels:
        app: redirector
    spec:
      containers:
      - name: redirector
        image: ghcr.io/your-org/the-redirector:latest
        ports:
        - containerPort: 8080
          name: http
        - containerPort: 8081
          name: management
        livenessProbe:
          httpGet:
            path: /health
            port: management
          initialDelaySeconds: 5
        readinessProbe:
          httpGet:
            path: /ready
            port: management
        resources:
          requests:
            cpu: 100m
            memory: 64Mi
          limits:
            cpu: 1000m
            memory: 256Mi
        volumeMounts:
        - name: config
          mountPath: /etc/redirector
      volumes:
      - name: config
        configMap:
          name: redirector-config
```

---

## Project Structure

```
the-redirector/
├── cmd/
│   ├── redirector/          # Main server
│   ├── redirector-lint/     # Config linter
│   ├── redirector-tui/      # Live monitoring TUI
│   └── config-syncer/       # Config sync service
├── internal/
│   ├── config/              # YAML parsing, validation
│   ├── router/              # Radix tree + regex routing
│   ├── server/              # fasthttp server
│   ├── stats/               # Ring buffer stats
│   ├── lint/                # Linting rules
│   ├── logging/             # Log rotation
│   └── providers/           # Config sources (file, GitHub, etc.)
├── pkg/redirect/            # Public types
├── config.yaml              # Sample configuration
├── syncer.yaml              # Sample syncer configuration
└── docs/                    # Additional documentation
```

---

## Development

```bash
# Run tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Build all binaries
go build ./...

# Format code
go fmt ./...

# Vet code
go vet ./...
```

---

## Roadmap

See [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md) for detailed status.

**Upcoming:**
- Prometheus metrics endpoint
- Hot reload with fsnotify
- Full AWS S3/Parameter Store integration
- OpenTelemetry tracing
- Load testing infrastructure

---

## License

MIT License - see [LICENSE](LICENSE) for details.

---

## Acknowledgments

- [fasthttp](https://github.com/valyala/fasthttp) - High-performance HTTP
- [zerolog](https://github.com/rs/zerolog) - Zero-allocation logging
- [lumberjack](https://github.com/natefinch/lumberjack) - Log rotation
- [bubbletea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [lipgloss](https://github.com/charmbracelet/lipgloss) - TUI styling
