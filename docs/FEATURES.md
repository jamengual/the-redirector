# Feature Specifications

This document details the feature requirements and design decisions for The Redirector.

## Core Philosophy

> **Frugal Core, Rich Plugins**
>
> The core redirector should be minimal and blazing fast. Advanced features
> should be implemented as optional plugins or separate utilities.

```
┌─────────────────────────────────────────────────────────────────────┐
│                         CORE (Minimal)                              │
│  • HTTP redirect handling (any status code)                         │
│  • Radix tree + regex routing                                       │
│  • Config loading (file-based)                                      │
│  • Prometheus metrics endpoint                                      │
│  • Health/ready endpoints                                           │
└─────────────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        ▼                     ▼                     ▼
┌───────────────┐    ┌───────────────┐    ┌───────────────┐
│ config-syncer │    │  redirector   │    │   redirector  │
│   (separate)  │    │     -tui      │    │    -lint      │
│               │    │               │    │               │
│ • GitHub/VCS  │    │ • Live logs   │    │ • Duplicates  │
│ • S3/Azure    │    │ • htop-style  │    │ • Conflicts   │
│ • Failover    │    │ • Stats view  │    │ • Regex perf  │
│ • Merging     │    │               │    │               │
└───────────────┘    └───────────────┘    └───────────────┘
```

---

## 1. Flexible Response Status Codes

The redirector should support **any HTTP status code**, not just redirects.

### Use Cases

| Status | Use Case |
|--------|----------|
| 301, 302, 307, 308 | Standard redirects |
| 404 | Block bots, hide deprecated endpoints |
| 410 | Gone - permanently removed content |
| 403 | Forbidden - block specific paths |
| 451 | Unavailable for legal reasons |
| 503 | Service unavailable (maintenance) |

### Configuration

```yaml
rules:
  # Standard redirect
  - match:
      path: /old-page
    response:
      status: 301
      location: https://example.com/new-page

  # Block bots with 404
  - match:
      type: regex
      pattern: ^/wp-admin
    response:
      status: 404
      body: "Not Found"

  # Maintenance mode
  - match:
      type: prefix
      path: /api/
      conditions:
        header: "X-Maintenance: true"
    response:
      status: 503
      headers:
        Retry-After: "3600"
      body: "Service temporarily unavailable"

  # GONE - permanently removed
  - match:
      path: /discontinued-product
    response:
      status: 410
      body: "This product has been discontinued"
```

### Implementation Note

Rename `Redirect` struct to `Response` to better reflect flexibility:

```go
type Response struct {
    Status   int               // Any HTTP status code
    Location string            // For redirects (3xx)
    Body     string            // Optional response body
    Headers  map[string]string // Custom headers
}
```

---

## 2. Header Injection

Support injecting custom headers in responses.

### Configuration

```yaml
defaults:
  headers:
    X-Powered-By: "the-redirector"
    X-Frame-Options: "DENY"
    X-Content-Type-Options: "nosniff"

rules:
  - match:
      path: /api/legacy
    response:
      status: 301
      location: https://api.example.com/v2
      headers:
        X-Deprecated: "true"
        X-Sunset-Date: "2025-01-01"
        Cache-Control: "no-cache"
```

### Security Headers

Common headers to support:

```yaml
# Security headers example
defaults:
  headers:
    Strict-Transport-Security: "max-age=31536000; includeSubDomains"
    X-Frame-Options: "SAMEORIGIN"
    X-Content-Type-Options: "nosniff"
    X-XSS-Protection: "1; mode=block"
    Referrer-Policy: "strict-origin-when-cross-origin"
```

---

## 3. Multi-File Configuration with YAML Merging

Support configuration spread across multiple files in a directory structure.

### Directory Structure

```
/etc/redirector/
├── config.yaml           # Main config (server settings)
├── defaults.yaml         # Default response settings
└── sites/
    ├── marketing/
    │   ├── campaigns.yaml
    │   └── landing-pages.yaml
    ├── api/
    │   ├── v1-redirects.yaml
    │   └── v2-redirects.yaml
    └── legacy/
        └── old-urls.yaml
```

### Merge Behavior

```yaml
# config.yaml (main)
config:
  rules_dir: /etc/redirector/sites
  merge_strategy: deep    # deep | override | append
  file_pattern: "**/*.yaml"
```

Rules are:
1. Loaded recursively from `rules_dir`
2. Merged in alphabetical order (by full path)
3. Later files can override earlier rules (same ID)
4. Conflicts logged as warnings

### Implementation

```go
// LoadDirectory loads all YAML files from a directory
func LoadDirectory(path string) (*Config, error) {
    var merged Config

    err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
        if !d.IsDir() && (strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml")) {
            cfg, err := Load(p)
            if err != nil {
                return err
            }
            merged = mergeConfigs(merged, cfg)
        }
        return nil
    })

    return &merged, err
}
```

---

## 4. Log Rotation

Built-in log rotation to prevent disk filling.

### Configuration

```yaml
logging:
  level: info
  format: json

  # File output with rotation
  file:
    path: /var/log/redirector/access.log
    max_size_mb: 100        # Rotate at 100MB (default)
    max_backups: 5          # Keep 5 rotated files
    max_age_days: 30        # Delete files older than 30 days
    compress: true          # Gzip rotated files
```

### Recommended Package

Use [`lumberjack`](https://github.com/natefinch/lumberjack) - well-maintained, widely used:

```go
import "gopkg.in/natefinch/lumberjack.v2"

logger := &lumberjack.Logger{
    Filename:   "/var/log/redirector/access.log",
    MaxSize:    100,  // MB
    MaxBackups: 5,
    MaxAge:     30,   // days
    Compress:   true,
}
```

---

## 5. Live CLI Dashboard (TUI)

htop-style terminal UI for real-time monitoring.

### Design (inspired by atmos/htop)

```
┌─ The Redirector v1.0.0 ────────────────────────────────────────────────────┐
│ Config: v2.3.1 (main@abc1234) | Updated: 2024-01-15 10:30:00 | Rules: 1,234│
├────────────────────────────────────────────────────────────────────────────┤
│ REQ/s: 45,231 │ p50: 0.1ms │ p99: 0.8ms │ Errors: 0.01% │ Mem: 45MB        │
├────────────────────────────────────────────────────────────────────────────┤
│                           LIVE REQUESTS (last 100)                         │
├────────────────────────────────────────────────────────────────────────────┤
│ TIME     │ STATUS │ LATENCY │ RULE         │ PATH                          │
│ 10:30:01 │ 301    │ 0.1ms   │ blog-redir   │ /blog/old-post → /posts/new   │
│ 10:30:01 │ 302    │ 0.2ms   │ api-v1       │ /api/v1/users → /api/v2/users │
│ 10:30:01 │ 404    │ 0.1ms   │ block-bots   │ /wp-admin                     │
│ 10:30:00 │ 301    │ 0.1ms   │ home-redir   │ /home → /                     │
│ ...                                                                        │
├────────────────────────────────────────────────────────────────────────────┤
│ [q] Quit  [f] Filter  [s] Sort  [p] Pause  [/] Search  [r] Refresh config  │
└────────────────────────────────────────────────────────────────────────────┘
```

### Architecture

The TUI connects to the redirector via a **streaming endpoint** (WebSocket or SSE):

```
┌─────────────┐         ┌─────────────┐
│ redirector  │◀───────▶│ redirector  │
│   (core)    │  Unix   │    -tui     │
│             │  Socket │             │
│ /stats/live │  or SSE │ Terminal UI │
└─────────────┘         └─────────────┘
```

### Performance Consideration

- TUI is a **separate binary** (`redirector-tui`)
- Core uses ring buffer for last N requests (configurable, default 1000)
- Sampling when under high load (e.g., log 1 in 100 at >50k req/s)
- Disabled by default, enabled via flag: `--enable-live-stats`

### Configuration

```yaml
# Enable live stats (disabled by default for performance)
stats:
  live:
    enabled: false          # Enable only when debugging
    buffer_size: 1000       # Keep last 1000 requests
    sampling_rate: 1.0      # 1.0 = all, 0.01 = 1% at high load
    socket_path: /tmp/redirector.sock
```

### Recommended Package

Use [`tview`](https://github.com/rivo/tview) or [`bubbletea`](https://github.com/charmbracelet/bubbletea):

```go
// Using bubbletea (modern, composable)
import tea "github.com/charmbracelet/bubbletea"
```

---

## 6. Stats Endpoint

Detailed statistics with filtering.

### Endpoints

```
GET /stats                    # Overall stats
GET /stats/rules              # Per-rule stats
GET /stats/rules/{id}         # Specific rule stats
GET /stats/rules?match=/api   # Filter by path pattern
GET /stats/live               # SSE stream for TUI
```

### Response Format

```json
{
  "uptime_seconds": 86400,
  "total_requests": 45231000,
  "requests_per_second": 523.5,
  "latency": {
    "p50_ms": 0.1,
    "p95_ms": 0.5,
    "p99_ms": 0.8,
    "p999_ms": 2.1
  },
  "status_codes": {
    "301": 40000000,
    "302": 5000000,
    "404": 231000
  },
  "top_rules": [
    {"id": "blog-redirect", "hits": 10000000, "avg_latency_ms": 0.1},
    {"id": "api-v1", "hits": 5000000, "avg_latency_ms": 0.2}
  ],
  "config": {
    "version": "v2.3.1",
    "source": "github:main@abc1234",
    "updated_at": "2024-01-15T10:30:00Z",
    "rules_count": 1234
  }
}
```

---

## 7. Health Endpoint with Config Version

Enhanced health check with configuration metadata.

### Response

```json
{
  "status": "healthy",
  "config": {
    "version": "v2.3.1",
    "source": "github",
    "ref": "main",
    "commit": "abc1234def5678",
    "updated_at": "2024-01-15T10:30:00Z",
    "rules_count": 1234,
    "files_loaded": [
      "/etc/redirector/sites/marketing/campaigns.yaml",
      "/etc/redirector/sites/api/v1-redirects.yaml"
    ]
  },
  "uptime_seconds": 86400,
  "memory_mb": 45,
  "goroutines": 12
}
```

---

## 8. Duplicate & Conflict Detection (Lint Tool)

Separate CLI tool for config validation.

### Usage

```bash
# Validate config files
redirector-lint /etc/redirector/

# Output
✓ Loaded 1,234 rules from 15 files

⚠ WARNINGS:
  - Rule 'catch-all' (line 45 in legacy.yaml) uses greedy pattern '/**'
    This will match before more specific rules. Consider using priority.

  - Rule 'api-legacy' overlaps with 'api-v1':
    Pattern '/api/*' matches same paths as '/api/v1/*'

  - Duplicate rule ID 'homepage' found in:
    - sites/marketing/landing.yaml:12
    - sites/legacy/old.yaml:34

✗ ERRORS:
  - Invalid regex in rule 'bad-pattern': unclosed group

SUGGESTIONS:
  - Rule 'product-regex' uses pattern '/product/(\d+)/.*'
    Consider using '/product/(\d+)(?:/.*)?$' for better performance
    (avoids backtracking on non-matching paths)
```

### Checks

1. **Duplicate IDs** - Same rule ID in multiple files
2. **Overlapping patterns** - Rules that match same paths
3. **Greedy patterns** - Patterns that may shadow other rules
4. **Invalid regex** - Syntax errors in patterns
5. **Performance suggestions** - Regex optimization hints
6. **Unreachable rules** - Rules that will never match

### Implementation

```go
// cmd/redirector-lint/main.go
type LintResult struct {
    Errors   []LintError
    Warnings []LintWarning
    Suggestions []Suggestion
}

func LintConfig(cfg *config.Config) *LintResult {
    result := &LintResult{}

    // Check for duplicates
    result.Errors = append(result.Errors, checkDuplicates(cfg)...)

    // Check for overlaps
    result.Warnings = append(result.Warnings, checkOverlaps(cfg)...)

    // Check regex performance
    result.Suggestions = append(result.Suggestions, analyzeRegexPerformance(cfg)...)

    return result
}
```

---

## 9. Multi-Source Failover & Aggregation

Support multiple configuration sources with failover and merging.

### Configuration

```yaml
# config-syncer.yaml
sources:
  # Primary source
  - name: github-primary
    type: github
    repository: my-org/redirect-config
    path: config/
    priority: 1              # Higher = preferred

  # Backup source (S3)
  - name: s3-backup
    type: s3
    bucket: redirect-config-backup
    key: config/
    priority: 2              # Used if GitHub fails

  # Additional rules (always merged)
  - name: local-overrides
    type: file
    path: /etc/redirector/local/
    merge: true              # Always merge, don't failover

# Failover behavior
failover:
  strategy: priority         # priority | round-robin | all
  health_check_interval: 30s
  failure_threshold: 3       # Failures before failover
  recovery_threshold: 2      # Successes before recovery
```

### Auth via Environment Variables

```yaml
# All auth can come from environment
sources:
  - type: s3
    bucket: ${S3_BUCKET}
    region: ${AWS_REGION}
    # Auth from env: AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY
    # Or IAM role (automatic in EKS/EC2)

  - type: azure_blob
    account: ${AZURE_STORAGE_ACCOUNT}
    container: ${AZURE_CONTAINER}
    # Auth from env: AZURE_STORAGE_KEY or AZURE_CLIENT_ID, etc.

  - type: github
    repository: ${GITHUB_REPO}
    # Auth from env: GITHUB_APP_ID, GITHUB_INSTALLATION_ID, GITHUB_PRIVATE_KEY
```

### Aggregation vs Failover

```yaml
# Aggregation: Merge configs from multiple sources
aggregation:
  enabled: true
  strategy: merge           # merge | override
  order:                    # Merge order (later overrides earlier)
    - github-primary
    - s3-regional
    - local-overrides

# Failover: Use backup when primary fails
failover:
  enabled: true
  sources:
    - github-primary        # Try first
    - s3-backup             # If GitHub fails
```

---

## 10. GitHubSource Pull Model

Config-syncer pulls to disk, then applies.

### Flow

```
┌──────────────────────────────────────────────────────────────────────────┐
│                           CONFIG-SYNCER                                  │
│                                                                          │
│  1. PULL          2. VALIDATE        3. APPLY          4. NOTIFY         │
│  ┌─────────┐      ┌─────────┐       ┌─────────┐       ┌─────────┐       │
│  │ GitHub  │─────▶│  Lint   │──────▶│  Write  │──────▶│  Push   │       │
│  │   API   │      │  Check  │       │ to Disk │       │ to API  │       │
│  └─────────┘      └─────────┘       └─────────┘       └─────────┘       │
│       │                │                 │                 │             │
│       ▼                ▼                 ▼                 ▼             │
│  /tmp/config/     Duplicates?       /etc/redirector/   POST /config     │
│  staging/         Conflicts?        config.yaml        to redirector    │
│                   Bad regex?                                             │
└──────────────────────────────────────────────────────────────────────────┘
```

### Configuration

```yaml
# config-syncer.yaml
sync:
  # Pull to staging directory first
  staging_dir: /tmp/redirector-staging

  # Final config location
  output_dir: /etc/redirector

  # Validation before applying
  validate:
    enabled: true
    lint_checks: true        # Run lint checks
    test_load: true          # Try loading config

  # Apply method
  apply:
    method: push_api         # push_api | file_only
    target_url: http://localhost:8081/api/v1/config
```

### Pull Interval

```yaml
sources:
  - type: github
    repository: my-org/config
    pull_interval: 60s       # Poll every 60 seconds

    # Or webhook-triggered (preferred)
    webhook:
      enabled: true
      secret: ${GITHUB_WEBHOOK_SECRET}
```

---

## 11. Regex Performance Analysis

Built into the lint tool, with optional AI suggestions.

### Static Analysis

```go
func analyzeRegexPerformance(pattern string) []Suggestion {
    var suggestions []Suggestion

    // Check for common performance issues
    if strings.Contains(pattern, ".*.*") {
        suggestions = append(suggestions, Suggestion{
            Type: "performance",
            Message: "Multiple .* in pattern causes exponential backtracking",
            Original: pattern,
            Suggested: "Use non-greedy .*? or be more specific",
        })
    }

    if strings.HasPrefix(pattern, ".*") {
        suggestions = append(suggestions, Suggestion{
            Type: "performance",
            Message: "Pattern starting with .* is slow (scans entire string)",
            Suggested: "Anchor with ^ or use more specific prefix",
        })
    }

    // Nested quantifiers
    if regexp.MustCompile(`\([^)]*[+*][^)]*\)[+*]`).MatchString(pattern) {
        suggestions = append(suggestions, Suggestion{
            Type: "critical",
            Message: "Nested quantifiers can cause catastrophic backtracking",
        })
    }

    return suggestions
}
```

### Common Optimizations

| Bad Pattern | Better Pattern | Why |
|-------------|----------------|-----|
| `.*foo.*` | `foo` with prefix match | Avoid scanning full string |
| `/api/.*/users` | `/api/[^/]+/users` | `[^/]+` is more specific |
| `(a+)+` | `a+` | Nested quantifiers = exponential |
| `^.*$` | Match all | Regex is overkill |
| `/product/(\d+)/.*` | `/product/(\d+)(?:/.*)?$` | Anchor prevents backtracking |

---

## 12. Recommended Go Packages

Core packages (well-maintained, widely used):

| Purpose | Package | Stars | Notes |
|---------|---------|-------|-------|
| HTTP Server | `github.com/valyala/fasthttp` | 21k+ | 10x faster than net/http |
| Router | `github.com/fasthttp/router` | 1k+ | Radix tree, zero-alloc |
| Logging | `github.com/rs/zerolog` | 10k+ | Zero-alloc JSON logging |
| Log Rotation | `gopkg.in/natefinch/lumberjack.v2` | 4k+ | Size-based rotation |
| YAML | `gopkg.in/yaml.v3` | stdlib-quality | Official YAML parser |
| Regex | `regexp` (stdlib) | - | RE2 engine, safe |
| TUI | `github.com/charmbracelet/bubbletea` | 25k+ | Modern, composable |
| CLI | `github.com/spf13/cobra` | 36k+ | Standard for Go CLIs |
| Config | `github.com/spf13/viper` | 26k+ | Multi-source config |
| Metrics | `github.com/prometheus/client_golang` | 5k+ | Standard for Go |
| AWS SDK | `github.com/aws/aws-sdk-go-v2` | 2k+ | Official AWS SDK v2 |
| Azure SDK | `github.com/Azure/azure-sdk-for-go` | 1k+ | Official Azure SDK |
| Testing | `github.com/stretchr/testify` | 22k+ | Assertions + mocks |

---

## Architecture Summary

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              CORE BINARY                                    │
│                           (redirector)                                      │
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                        MINIMAL CORE                                  │   │
│  │  • fasthttp server                                                   │   │
│  │  • Radix tree + regex router                                        │   │
│  │  • YAML config loading (single file or directory)                   │   │
│  │  • Any HTTP status response                                          │   │
│  │  • Header injection                                                  │   │
│  │  • /health, /ready, /metrics                                        │   │
│  │  • Log rotation (lumberjack)                                        │   │
│  │  • Optional: /stats/live (ring buffer for TUI)                      │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘

┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐
│  config-syncer   │  │  redirector-tui  │  │  redirector-lint │
│    (separate)    │  │    (separate)    │  │    (separate)    │
│                  │  │                  │  │                  │
│ • GitHub/GitLab  │  │ • htop-style UI  │  │ • Duplicate check│
│ • S3/Azure/GCP   │  │ • Live requests  │  │ • Conflict detect│
│ • Pull to disk   │  │ • Stats view     │  │ • Regex analysis │
│ • Validate       │  │ • Connects via   │  │ • Suggestions    │
│ • Multi-source   │  │   socket/SSE     │  │                  │
│ • Failover       │  │                  │  │                  │
└──────────────────┘  └──────────────────┘  └──────────────────┘
```
