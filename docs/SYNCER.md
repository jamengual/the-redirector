# redirector-sync

Separate service for pulling configuration from multiple sources with failover, multi-team support, and integrated config linting.

## Multi-Team Configuration

Each team manages their own config source. The syncer merges them with conflict detection:

```yaml
# syncer.yaml
sync_interval: 60s

sources:
  # Marketing team - S3 bucket
  - name: marketing
    type: s3
    prefix: "marketing"     # Rules become: marketing/campaign, etc.
    priority: 10
    allowed_paths:          # Restrict which paths this team can define
      - "/promo/"
      - "/campaign/"
    config:
      bucket: marketing-team-config
      key: redirects/rules.yaml
      region: us-east-1

  # Engineering team - GitHub repo
  - name: engineering
    type: github
    prefix: "eng"           # Rules become: eng/docs-v2, etc.
    priority: 20
    allowed_paths:
      - "/docs/"
      - "/api/"
    config:
      owner: myorg
      repo: engineering-redirects
      path: config/rules.yaml
      ref: main

  # Platform team - highest priority, can override anything
  - name: platform
    type: file
    prefix: "platform"
    priority: 100           # Wins in conflicts
    config:
      path: /etc/redirector/platform-rules.yaml

# How to handle conflicts between teams
merge:
  conflict_resolution: error  # error, priority, or first
  require_prefix: true        # Force all sources to have prefixes

targets:
  - name: redirector-prod
    url: http://redirector:8081
    api_key: ${REDIRECTOR_API_KEY}
```

## Conflict Resolution Modes

| Mode | Behavior |
|------|----------|
| `error` | Fail sync if teams define rules for same path (safest) |
| `priority` | Higher priority team wins |
| `first` | First team to define the path wins |

---

## Simple Configuration (Single Source)

```yaml
# syncer.yaml
sync_interval: 5m

sources:
  - name: "s3-primary"
    type: s3
    priority: 100
    config:
      bucket: my-config-bucket
      key: config/redirector.yaml
      region: us-east-1

  - name: "local-fallback"
    type: file
    priority: 1
    config:
      path: /etc/redirector/config.yaml

targets:
  - name: redirector
    url: http://localhost:8081
```

---

## Running

```bash
# Continuous sync
./redirector-sync --config syncer.yaml

# One-shot (fetch once and exit)
./redirector-sync --config syncer.yaml --one-shot

# Dry run (fetch but don't write)
./redirector-sync --config syncer.yaml --dry-run
```

---

## Config Linting

Validate configuration and detect issues before deployment. Lint is integrated into `redirector-sync` and also runs automatically during sync (errors block sync, warnings are logged).

```bash
# Basic validation
./redirector-sync --lint --lint-config config.yaml

# JSON output for CI/CD
./redirector-sync --lint --lint-config config.yaml --lint-json

# Quiet mode (errors only)
./redirector-sync --lint --lint-config config.yaml --lint-quiet

# Lint by fetching from syncer sources
./redirector-sync --lint --config syncer.yaml
```

**Checks performed:**
- **Circular redirect detection** — cycles, self-loops, and regex/glob sample tracing
- Duplicate rule IDs
- Overlapping patterns (rules that match same paths)
- Greedy patterns without negative priority
- Regex performance issues (nested quantifiers, multiple `.*`)
- Unreachable rules (shadowed by higher-priority rules)
- Missing default status codes

Example output:
```
The Redirector - Config Linter
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Loaded 25 rules

✗ ERRORS (2)
─────────────────────────────────────────────────────────────────
  [rule-5] Duplicate rule ID 'homepage' (first seen at index 0)
  [rule-a] Circular redirect detected: rule-a -> rule-b -> rule-a
    → Remove one rule from the chain or change a destination to break the cycle

⚠ WARNINGS (2)
─────────────────────────────────────────────────────────────────
  [api-v1] Rule 'api-v1' may overlap with 'api-all': Prefix '/api/' is contained in '/api/v1/'
    → Consider setting different priorities to control matching order
  [catch-all] Greedy glob pattern '/**' will match many paths
    → Set a negative priority (e.g., -100) to ensure it's evaluated last

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Found: 2 errors, 2 warnings, 0 suggestions
```

### Multi-Team Conflict Detection

When your syncer config defines multiple sources, lint automatically detects conflicts between teams:

```bash
# Fetches all sources from syncer.yaml, lints each, detects cross-source conflicts
./redirector-sync --lint --config syncer.yaml
```

The syncer config already encodes source names, prefixes, and priorities — no extra arguments needed.

Example output:
```
The Redirector - Multi-Team Config Linter
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Sources: 3 | Total Rules: 45
  • marketing: 15 rules
  • engineering: 20 rules
  • platform: 10 rules

⚠ TEAM CONFLICTS (2)
─────────────────────────────────────────
These rules from different teams may conflict at runtime:

  1. Multiple teams define rules for the same path '/api/v1/users'
     Path: /api/v1/users
     Teams: engineering vs platform
     Rules: eng/api-users, platform/api-override
     Type: exact

  2. Prefix rules overlap: '/docs/' and '/docs/v1/' may match the same paths
     Path: /docs/ vs /docs/v1/
     Teams: marketing vs engineering
     Rules: marketing/docs-redirect, eng/docs-v1
     Type: overlap

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Found: 2 conflicts, 0 errors, 1 warnings

Recommendation: Teams should coordinate on conflicting paths or use
different path prefixes to avoid runtime conflicts.
```

### Circular Redirect Detection

The linter detects redirect loops that would cause infinite request cycles. This is one of the most common issues with large redirect rule sets, especially on content sites where teams independently manage rules.

#### How It Works

The linter builds a **directed graph** from all redirect rules and runs **depth-first search (DFS)** cycle detection:

```
 ┌──────────┐     ┌──────────┐     ┌──────────┐
 │  rule-a   │────▶│  rule-b   │────▶│  rule-c   │
 │  /page-a  │     │  /page-b  │     │  /page-c  │
 │  → /page-b│     │  → /page-c│     │  → /page-a│ ◀── cycle!
 └──────────┘     └──────────┘     └──────────┘
       ▲                                  │
       └──────────────────────────────────┘
```

Each rule is an edge: from the path it matches to the path it redirects to. When the graph has a cycle, the linter traces the full chain and reports it as an error.

#### What It Detects

| Type | Severity | Example | Description |
|------|----------|---------|-------------|
| **Direct cycle** | Error | `/a → /b → /a` | Two rules redirecting to each other |
| **Transitive cycle** | Error | `/a → /b → /c → /a` | Chain of 3+ rules forming a loop |
| **Prefix cross-cycle** | Error | `/foo/ → /bar/`, `/bar/ → /foo/` | Prefix rules bouncing between each other |
| **Cross-host cycle** | Error | `example.com/p1 → example.com/p2 → example.com/p1` | Cycle via absolute URLs pointing back to local hosts |
| **Prefix self-loop** | Error | `/old/ → /old/new/` with `preserve_path` | A prefix rule that redirects back into its own match space, causing expanding paths: `/old/x → /old/new/x → /old/new/new/x → ...` |
| **Regex self-match** | Warning | `^/api/v1/(.*) → /api/v1/v2/$1` | Regex destination still matches the same rule (best-effort, sample-based) |
| **Glob loop** | Warning | `/docs/* → /docs/archive/sample` | Glob destination falls within the same glob match pattern (best-effort) |

#### Example: Direct Cycle

Config with a circular redirect:
```yaml
rules:
  - id: old-home
    match:
      type: exact
      path: /old-home
    redirect:
      to: /new-home
      status: 301

  - id: new-home
    match:
      type: exact
      path: /new-home
    redirect:
      to: /old-home    # Oops — this creates a loop!
      status: 301
```

Lint output:
```
The Redirector - Config Linter
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Loaded 2 rules

✗ ERRORS (1)
─────────────────────────────────────────────────────────────────
  [old-home] Circular redirect detected: old-home -> new-home -> old-home
    → Remove one rule from the chain or change a destination to break the cycle

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Found: 1 errors, 0 warnings, 0 suggestions
```

#### Example: Prefix Self-Loop with preserve_path

This is a subtle bug — a prefix rule with `preserve_path: true` redirecting to a destination that starts with the same prefix:

```yaml
rules:
  - id: migrate-docs
    match:
      type: prefix
      path: /docs/
    redirect:
      to: /docs/v2/
      preserve_path: true
      status: 301
```

What happens at runtime: `/docs/api` → `/docs/v2/api` → `/docs/v2/v2/api` → `/docs/v2/v2/v2/api` → ... (infinite expanding path).

Lint output:
```
✗ ERRORS (1)
─────────────────────────────────────────────────────────────────
  [migrate-docs] Prefix rule 'migrate-docs' with preserve_path creates a self-loop:
    match '/docs/' redirects to '/docs/v2/' which is within the same prefix
    → Change the destination to a path outside the match prefix, or disable preserve_path
```

**Fix:** Change the destination to a path outside the match prefix:
```yaml
  - id: migrate-docs
    match:
      type: prefix
      path: /docs/
    redirect:
      to: /documentation/v2/    # Outside /docs/ — no loop
      preserve_path: true
      status: 301
```

#### Example: Regex Warning (Best-Effort)

Regex rules can't always be statically analyzed (regex intersection is undecidable), so the linter generates sample paths and traces them through the rules:

```yaml
rules:
  - id: api-rewrite
    match:
      type: regex
      pattern: "^/api/v1/(.*)"
    redirect:
      to: /api/v1/v2/$1     # Destination still matches ^/api/v1/(.*)!
      status: 302
```

Lint output:
```
⚠ WARNINGS (1)
─────────────────────────────────────────────────────────────────
  [api-rewrite] Potential circular redirect: rule 'api-rewrite' destination '/api/v1/v2/$1'
    may match the same rule (sample path: '/api/v1/sample' -> '/api/v1/v2/sample')
    → Verify the destination does not fall within the rule's match pattern
```

#### Example: JSON Output for CI

```bash
./redirector-sync --lint --lint-config config.yaml --lint-json
```

```json
{
  "issues": [
    {
      "severity": "error",
      "rule_id": "old-home",
      "message": "Circular redirect detected: old-home -> new-home -> old-home",
      "suggestion": "Remove one rule from the chain or change a destination to break the cycle"
    },
    {
      "severity": "warning",
      "rule_id": "api-rewrite",
      "message": "Potential circular redirect: rule 'api-rewrite' destination '/api/v1/v2/$1' may match the same rule (sample path: '/api/v1/sample' -> '/api/v1/v2/sample')",
      "suggestion": "Verify the destination does not fall within the rule's match pattern"
    }
  ],
  "rules_count": 5,
  "files_count": 0
}
```

Use this in CI to gate deployments:
```bash
# In your config repo's CI pipeline
if ! ./redirector-sync --lint --lint-config config.yaml; then
  echo "Config validation failed — check for circular redirects"
  exit 1
fi
```

#### Webhook Response

When the syncer receives a webhook and the fetched config has lint errors, the response includes the full issue list (instead of a generic "Sync failed"):

```
POST /webhook → HTTP 422 Unprocessable Entity
```
```json
{
  "status": "lint_failed",
  "source": "github-primary",
  "message": "lint errors found in config from source github-primary (1 errors)",
  "issues": [
    {
      "severity": "error",
      "rule_id": "old-home",
      "message": "Circular redirect detected: old-home -> new-home -> old-home",
      "suggestion": "Remove one rule from the chain or change a destination to break the cycle"
    }
  ]
}
```

This lets CI pipelines that trigger syncs via webhook inspect the response and provide specific feedback to the user.

#### Detection Limitations

| Rule Type | Detection | Notes |
|-----------|-----------|-------|
| Exact | Deterministic | Full cycle detection via graph analysis |
| Prefix | Deterministic | Includes `preserve_path` self-loop detection |
| Regex | Best-effort (samples) | Generates representative paths and traces them; reported as warnings |
| Glob | Best-effort (samples) | Same sample-based approach as regex; reported as warnings |
| External URLs | Skipped | Destinations pointing to hosts not in `match.host` are assumed external |

---

## Debug Logging

Enable debug output to diagnose integration issues:

```yaml
# syncer.yaml
log_level: debug   # trace, debug, info (default), warn, error, fatal

sources:
  - name: "github-config"
    type: github
    # ...
```

Debug logging shows source creation details (with secrets redacted), fetch flow with resolved refs, API URLs, content sizes, and content previews on parse failures.

---

## Available Configuration Sources

| Source | Type | Auth Options | Description |
|--------|------|--------------|-------------|
| File | `file` | — | Local filesystem (YAML, JSON) |
| AWS S3 | `s3` | IAM, cross-account role | S3 bucket with IAM/cross-account support |
| Azure Blob Storage | `azureblob` | Connection string, account key, SAS, Managed Identity | Azure Storage containers |
| GCP Cloud Storage | `gcs` | Application Default Credentials, service account | GCS buckets |
| GitHub | `github` | PAT, GitHub App (JWT) | Repos with release/branch/tag/commit strategies |
| GitLab | `gitlab` | PAT, OAuth2 | Repos with release/branch/tag/commit strategies |
| HTTP/HTTPS | `http` | Bearer token, basic auth | Any HTTP endpoint with ETag caching |
| AWS Parameter Store | `parameterstore` | IAM, cross-account role | SSM parameters (single or hierarchy) |
| AWS Secrets Manager | `secretsmanager` | IAM, cross-account role | Secrets with rotation and version staging |
| HashiCorp Consul | `consul` | ACL token, mTLS | Consul KV with blocking queries |
| etcd | `etcd` | Username/password, mTLS | etcd KV with native watch |

---

### File

Read config from the local filesystem. Simplest source — good for local development or as a fallback.

```yaml
sources:
  - name: local-config
    type: file
    config:
      path: /etc/redirector/config.yaml
```

---

### AWS S3

Read config from an S3 bucket. Supports cross-account access via IAM role assumption and S3-compatible endpoints (MinIO, LocalStack).

```yaml
sources:
  # Standard S3
  - name: s3-primary
    type: s3
    config:
      bucket: my-config-bucket
      key: config/redirector.yaml
      region: us-east-1
      poll_interval: 5m

  # Cross-account access
  - name: s3-cross-account
    type: s3
    config:
      bucket: other-team-bucket
      key: redirects/rules.yaml
      region: eu-west-1
      role_arn: arn:aws:iam::123456789012:role/redirector-reader
      poll_interval: 5m

  # S3-compatible (MinIO)
  - name: minio
    type: s3
    config:
      bucket: configs
      key: redirector.yaml
      region: us-east-1
      endpoint: http://minio.internal:9000
```

---

### Azure Blob Storage

Read config from Azure Blob Storage. Four authentication methods available.

```yaml
sources:
  # Connection string auth
  - name: azure-connstr
    type: azureblob
    config:
      storage_account: mystorageaccount
      container: configs
      blob_name: redirector.yaml
      connection_string: ${AZURE_STORAGE_CONNECTION_STRING}
      poll_interval: 5m

  # Account key auth
  - name: azure-key
    type: azureblob
    config:
      storage_account: mystorageaccount
      container: configs
      blob_name: redirector.yaml
      account_key: ${AZURE_STORAGE_KEY}

  # SAS token auth
  - name: azure-sas
    type: azureblob
    config:
      storage_account: mystorageaccount
      container: configs
      blob_name: redirector.yaml
      sas_token: ${AZURE_SAS_TOKEN}

  # Managed Identity (Azure VMs, AKS, App Service)
  - name: azure-managed
    type: azureblob
    config:
      storage_account: mystorageaccount
      container: configs
      blob_name: redirector.yaml
      use_default_credential: true
```

---

### GCP Cloud Storage

Read config from a GCS bucket. Uses Application Default Credentials by default.

```yaml
sources:
  # Application Default Credentials (GKE, Cloud Run, etc.)
  - name: gcs-default
    type: gcs
    config:
      bucket: my-config-bucket
      object: redirector/config.yaml
      project: my-gcp-project
      poll_interval: 5m

  # Service account key file
  - name: gcs-sa
    type: gcs
    config:
      bucket: my-config-bucket
      object: redirector/config.yaml
      credentials_file: /etc/redirector/gcp-sa.json

  # Inline credentials (use env var for the JSON)
  - name: gcs-inline
    type: gcs
    config:
      bucket: my-config-bucket
      object: redirector/config.yaml
      credentials_json: ${GCP_CREDENTIALS_JSON}

  # Storage emulator (dev/test)
  - name: gcs-emulator
    type: gcs
    config:
      bucket: test-bucket
      object: config.yaml
      endpoint: http://localhost:4443
```

The GCS provider also respects the `STORAGE_EMULATOR_HOST` environment variable.

---

### GitHub

Read config from a GitHub repository. Supports PAT and GitHub App authentication, with four deployment strategies.

```yaml
sources:
  # PAT auth, release strategy (default)
  - name: github-releases
    type: github
    config:
      repository: myorg/redirector-config
      path: config/rules.yaml
      token: ${GITHUB_TOKEN}
      strategy: release           # Deploy from GitHub Releases
      environment: production

  # PAT auth, branch strategy
  - name: github-branch
    type: github
    config:
      repository: myorg/redirector-config
      path: config/rules.yaml
      token: ${GITHUB_TOKEN}
      strategy: branch
      environment: main           # Track the 'main' branch

  # PAT auth, tag strategy with pattern
  - name: github-tags
    type: github
    config:
      repository: myorg/redirector-config
      path: config/rules.yaml
      token: ${GITHUB_TOKEN}
      strategy: tag
      tag_pattern: "v*"           # Only tags matching v*

  # GitHub App auth (recommended for orgs)
  - name: github-app
    type: github
    config:
      repository: myorg/redirector-config
      path: config/rules.yaml
      strategy: release
      app:
        app_id: 12345
        installation_id: 67890
        private_key_path: /etc/redirector/github-app.pem
        # Or inline: private_key: ${GITHUB_APP_PRIVATE_KEY}

  # GitHub Enterprise Server
  - name: github-enterprise
    type: github
    config:
      repository: myorg/redirector-config
      path: config/rules.yaml
      token: ${GITHUB_TOKEN}
      base_url: https://github.corp.example.com/api/v3
      strategy: branch
      environment: main

  # Webhook-triggered sync (instant updates on push)
  - name: github-webhook
    type: github
    config:
      repository: myorg/redirector-config
      path: config/rules.yaml
      token: ${GITHUB_TOKEN}
      strategy: branch
      environment: main
      webhook_secret: ${GITHUB_WEBHOOK_SECRET}
```

**Deployment strategies:**

| Strategy | Behavior |
|----------|----------|
| `release` | Only deploy from published GitHub Releases (default, safest) |
| `tag` | Deploy from git tags matching `tag_pattern` glob |
| `branch` | Track a branch (set via `environment`) |
| `commit` | Pin to a specific commit SHA |

See [GITHUB_INTEGRATION.md](GITHUB_INTEGRATION.md) for detailed GitHub App setup.

---

### GitLab

Read config from a GitLab repository. Supports PAT and OAuth2 authentication.

```yaml
sources:
  # PAT auth, release strategy
  - name: gitlab-releases
    type: gitlab
    config:
      project: mygroup/redirector-config
      path: config/rules.yaml
      token: ${GITLAB_TOKEN}
      token_type: private-token   # default
      strategy: release
      environment: production

  # OAuth2 auth with auto token refresh
  - name: gitlab-oauth
    type: gitlab
    config:
      project: mygroup/redirector-config
      path: config/rules.yaml
      token: ${GITLAB_OAUTH_TOKEN}
      token_type: oauth
      strategy: branch
      environment: main

  # Tag strategy with pattern matching
  - name: gitlab-tags
    type: gitlab
    config:
      project: mygroup/redirector-config
      path: config/rules.yaml
      token: ${GITLAB_TOKEN}
      strategy: tag
      tag_pattern: "release-*"

  # Self-hosted GitLab
  - name: gitlab-self-hosted
    type: gitlab
    config:
      project: mygroup/redirector-config
      path: config/rules.yaml
      base_url: https://gitlab.corp.example.com
      token: ${GITLAB_TOKEN}
      strategy: release

  # Webhook-triggered sync
  - name: gitlab-webhook
    type: gitlab
    config:
      project: mygroup/redirector-config
      path: config/rules.yaml
      token: ${GITLAB_TOKEN}
      strategy: branch
      environment: main
      webhook_token: ${GITLAB_WEBHOOK_TOKEN}
```

---

### HTTP/HTTPS Endpoint

Read config from any HTTP endpoint. Supports bearer token and basic auth. Automatically uses ETag-based caching — if the server returns an `ETag` header, subsequent requests include `If-None-Match` to avoid re-downloading unchanged configurations.

```yaml
sources:
  # Bearer token auth
  - name: http-bearer
    type: http
    config:
      url: https://config-server.internal/redirector/config.yaml
      bearer_token: ${CONFIG_SERVER_TOKEN}
      timeout: 10s
      poll_interval: 30s
      headers:
        X-Custom-Header: "my-value"
        Accept: "application/yaml"

  # Basic auth
  - name: http-basic
    type: http
    config:
      url: https://config-server.internal/redirector/config.yaml
      basic_auth:
        username: admin
        password: ${CONFIG_PASSWORD}
      timeout: 10s
      poll_interval: 1m

  # No auth (internal network)
  - name: http-internal
    type: http
    config:
      url: http://config-service.internal:8080/redirector.yaml
      timeout: 5s
      poll_interval: 30s
```

---

### AWS Parameter Store

Read config from AWS Systems Manager Parameter Store. Supports single parameters and hierarchies, with optional decryption for SecureString parameters.

```yaml
sources:
  # Single parameter
  - name: ssm-single
    type: parameterstore
    config:
      path: /myapp/redirector/config
      region: us-east-1
      with_decryption: true       # Decrypt SecureString (default: true)
      poll_interval: 1m

  # Parameter hierarchy (trailing slash)
  - name: ssm-hierarchy
    type: parameterstore
    config:
      path: /myapp/redirector/    # Trailing slash = fetch all children
      region: us-east-1
      recursive: true             # Include nested paths
      with_decryption: true
      poll_interval: 1m

  # Cross-account access
  - name: ssm-cross-account
    type: parameterstore
    config:
      path: /shared/redirector/config
      region: us-east-1
      role_arn: arn:aws:iam::123456789012:role/ssm-reader
```

---

### AWS Secrets Manager

Read config from AWS Secrets Manager. Supports version staging and caching for rotation-aware deployments.

```yaml
sources:
  # Current version (default)
  - name: secrets-current
    type: secretsmanager
    config:
      secret_id: myapp/redirector-config
      region: us-east-1
      version_stage: AWSCURRENT   # Or AWSPREVIOUS for rollback
      cache_ttl: 5m               # Cache to reduce API calls
      poll_interval: 5m

  # Specific version (pinned deployment)
  - name: secrets-pinned
    type: secretsmanager
    config:
      secret_id: myapp/redirector-config
      region: us-east-1
      version_id: "abc123-def456"

  # Cross-account access
  - name: secrets-cross-account
    type: secretsmanager
    config:
      secret_id: arn:aws:secretsmanager:us-east-1:123456789012:secret:redirector-config
      region: us-east-1
      role_arn: arn:aws:iam::123456789012:role/secrets-reader
```

---

### HashiCorp Consul

Read config from Consul KV. Uses blocking queries for near-instant change detection without polling.

```yaml
sources:
  # Basic setup
  - name: consul-basic
    type: consul
    config:
      key: redirector/config
      address: consul.service.consul:8500
      datacenter: dc1
      token: ${CONSUL_TOKEN}      # ACL token
      wait_time: 5m               # Blocking query timeout

  # Consul Enterprise (namespace + partition)
  - name: consul-enterprise
    type: consul
    config:
      key: redirector/config
      address: consul.corp.example.com:8500
      datacenter: us-east-1
      token: ${CONSUL_TOKEN}
      namespace: production
      partition: platform

  # mTLS authentication
  - name: consul-mtls
    type: consul
    config:
      key: redirector/config
      address: consul.service.consul:8501
      token: ${CONSUL_TOKEN}
      tls:
        ca_file: /etc/ssl/consul/ca.pem
        cert_file: /etc/ssl/consul/client-cert.pem
        key_file: /etc/ssl/consul/client-key.pem
```

---

### etcd

Read config from etcd KV. Uses native watch for real-time change detection.

```yaml
sources:
  # Basic setup
  - name: etcd-basic
    type: etcd
    config:
      key: /redirector/config
      endpoints:
        - etcd1:2379
        - etcd2:2379
        - etcd3:2379
      username: root
      password: ${ETCD_PASSWORD}
      dial_timeout: 5s
      request_timeout: 10s

  # Single node (dev/test)
  - name: etcd-dev
    type: etcd
    config:
      key: /redirector/config
      endpoints:
        - localhost:2379

  # mTLS authentication
  - name: etcd-mtls
    type: etcd
    config:
      key: /redirector/config
      endpoints:
        - etcd1.corp.example.com:2379
        - etcd2.corp.example.com:2379
        - etcd3.corp.example.com:2379
      tls:
        cert_file: /etc/ssl/etcd/client-cert.pem
        key_file: /etc/ssl/etcd/client-key.pem
        ca_file: /etc/ssl/etcd/ca.pem
```

---

For full configuration reference of all syncer fields, see [CONFIGURATION.md](CONFIGURATION.md).
