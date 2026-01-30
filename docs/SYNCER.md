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

| Source | Type | Description |
|--------|------|-------------|
| File | `file` | Local filesystem (YAML, JSON) |
| HTTP/HTTPS | `http` | HTTP endpoint with bearer/basic auth, ETag caching |
| AWS S3 | `s3` | S3 bucket with IAM/cross-account support |
| AWS Parameter Store | `parameterstore` | SSM parameters (single or hierarchy) |
| AWS Secrets Manager | `secretsmanager` | Secrets with rotation support |
| Azure Blob Storage | `azureblob` | Azure Storage with SAS/DefaultCredential |
| GCP Cloud Storage | `gcs` | GCS with Application Default Credentials |
| GitHub | `github` | GitHub repos (PAT or GitHub App auth, release/branch/tag strategies, tag pattern glob matching) |
| GitLab | `gitlab` | GitLab repos (PAT/OAuth2, releases/branches/tags with pattern matching) |
| HashiCorp Consul | `consul` | Consul KV with native watch |
| etcd | `etcd` | etcd KV with native watch |

### AWS Parameter Store

```yaml
sources:
  - name: aws-params
    type: parameterstore
    config:
      path: /myapp/redirector/config  # Single parameter
      # Or hierarchy: /myapp/redirector/  (trailing slash)
      region: us-east-1
      with_decryption: true  # For SecureString
      poll_interval: 1m
```

### AWS Secrets Manager

```yaml
sources:
  - name: aws-secrets
    type: secretsmanager
    config:
      secret_id: myapp/redirector-config
      region: us-east-1
      version_stage: AWSCURRENT  # Or AWSPREVIOUS
      cache_ttl: 5m
```

### Azure Blob Storage

```yaml
sources:
  - name: azure-blob
    type: azureblob
    config:
      storage_account: mystorageaccount
      container: configs
      blob_name: redirector.yaml
      # Auth options (pick one):
      connection_string: ${AZURE_STORAGE_CONNECTION_STRING}
      # Or: account_key, sas_token, use_default_credential
```

### GCP Cloud Storage

```yaml
sources:
  - name: gcs
    type: gcs
    config:
      bucket: my-config-bucket
      object: redirector/config.yaml
      # Auth: Uses Application Default Credentials by default
      # Or: credentials_file: /path/to/service-account.json
```

### HTTP/HTTPS Endpoint

```yaml
sources:
  - name: http-config
    type: http
    config:
      url: https://config-server.internal/redirector/config.yaml
      bearer_token: ${CONFIG_SERVER_TOKEN}
      # Or basic auth:
      # basic_auth:
      #   username: admin
      #   password: ${CONFIG_PASSWORD}
      timeout: 10s
      poll_interval: 30s
      headers:
        X-Custom-Header: "my-value"
```

The HTTP provider supports ETag-based caching. If the server returns an `ETag` header, subsequent requests include `If-None-Match` to avoid re-downloading unchanged configurations.

### GitLab

```yaml
sources:
  - name: gitlab-config
    type: gitlab
    config:
      project: mygroup/myproject
      path: config/redirector.yaml
      base_url: https://gitlab.com  # Or self-hosted
      strategy: release  # release, branch, tag, commit
      environment: production
      token: ${GITLAB_TOKEN}
```

### Consul

```yaml
sources:
  - name: consul-kv
    type: consul
    config:
      key: redirector/config
      address: consul.service.consul:8500
      datacenter: dc1
      token: ${CONSUL_TOKEN}  # ACL token (optional)
```

### etcd

```yaml
sources:
  - name: etcd-kv
    type: etcd
    config:
      key: /redirector/config
      endpoints:
        - etcd1:2379
        - etcd2:2379
        - etcd3:2379
      username: root
      password: ${ETCD_PASSWORD}
```

---

For full configuration reference of all syncer fields, see [CONFIGURATION.md](CONFIGURATION.md).
