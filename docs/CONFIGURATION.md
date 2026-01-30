# Configuration Reference

Complete reference for all configuration options in The Redirector, redirector-sync, and redirector-tui.

All YAML values support environment variable expansion using `${VAR}` or `${VAR:-default}` syntax.

---

## Redirector Config (`config.yaml`)

### Root Fields

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| Version | string | `version` | `""` | Config version string (e.g., `"1.0"`) |
| Server | object | `server` | — | HTTP server settings |
| Defaults | object | `defaults` | — | Default values for redirects |
| Stats | object | `stats` | disabled | Statistics collection |
| Auth | object | `auth` | disabled | Management API authentication |
| Tracing | object | `tracing` | disabled | OpenTelemetry tracing |
| RateLimit | object | `rate_limit` | disabled | Rate limiting |
| Rules | array | `rules` | `[]` | Redirect/response rules |
| RulesInclude | array | `rules_include` | `[]` | External rule files to include (CSV or YAML) |

---

### Server (`server`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| Port | int | `port` | `8080` | Main redirect server port |
| ManagementPort | int | `management_port` | `8081` | Management API port |
| ReadTimeout | string | `read_timeout` | `""` | HTTP read timeout (e.g., `"5s"`) |
| WriteTimeout | string | `write_timeout` | `""` | HTTP write timeout (e.g., `"5s"`) |
| IdleTimeout | string | `idle_timeout` | `""` | HTTP idle timeout (e.g., `"120s"`) |
| MaxConnections | int | `max_connections` | `0` | Max concurrent connections (0 = unlimited) |

---

### Defaults (`defaults`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| StatusCode | int | `status_code` | `301` | Default HTTP status code for redirects |
| PreserveQuery | bool | `preserve_query` | `false` | Forward query string by default |
| Headers | map | `headers` | `null` | Default response headers added to every response |

---

### Stats (`stats`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| Enabled | bool | `enabled` | `false` | Enable stats collection |
| BufferSize | int | `buffer_size` | `1000` | Ring buffer size for live request view |
| SamplingRate | float | `sampling_rate` | `1.0` | Fraction of requests to record (`0.01` = 1%) |

---

### Auth (`auth`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| Enabled | bool | `enabled` | `false` | Enable management API authentication |
| APIKeys | array | `api_keys` | `[]` | API key entries |
| JWT | object | `jwt` | `null` | JWT authentication config |
| AllowIPs | array | `allow_ips` | `[]` | IP allowlist (bypass auth) |

#### API Keys (`auth.api_keys[]`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| Name | string | `name` | `""` | Descriptive name for the key |
| Key | string | `key` | `""` | The API key value (use `${ENV_VAR}`) |
| Permissions | array | `permissions` | `[]` | Granted permissions |

**Available permissions:** `read`, `write`, `reload`, `admin`, `stats:read`, `stats:write`

#### JWT (`auth.jwt`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| Enabled | bool | `enabled` | `false` | Enable JWT authentication |
| Secret | string | `secret` | `""` | HMAC secret (use `${ENV_VAR}`) |
| PublicKey | string | `public_key` | `""` | RSA/ECDSA public key (PEM-encoded) |
| Issuer | string | `issuer` | `""` | Expected `iss` claim |
| Audience | string | `audience` | `""` | Expected `aud` claim |

---

### Tracing (`tracing`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| Enabled | bool | `enabled` | `false` | Enable OpenTelemetry tracing |
| Endpoint | string | `endpoint` | `""` | OTLP endpoint (e.g., `"localhost:4317"`) |
| ServiceName | string | `service_name` | `"the-redirector"` | Service name in traces |
| Environment | string | `environment` | `""` | Deployment environment label |
| SamplingRate | float | `sampling_rate` | `1.0` | Trace sampling rate (0.0–1.0) |
| Insecure | bool | `insecure` | `true` | Use insecure connection (no TLS) |

---

### Rate Limiting (`rate_limit`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| Enabled | bool | `enabled` | `false` | Enable rate limiting |
| GlobalRPS | float | `global_rps` | `10000` | Global requests per second |
| GlobalBurst | int | `global_burst` | `20000` | Global burst size |
| PerIPRPS | float | `per_ip_rps` | `100` | Per-IP requests per second |
| PerIPBurst | int | `per_ip_burst` | `200` | Per-IP burst size |
| PathLimits | array | `path_limits` | `[]` | Path-specific rate limits |
| TrustProxy | bool | `trust_proxy` | `false` | Trust `X-Forwarded-For` header |
| ExemptIPs | array | `exempt_ips` | `[]` | IPs that bypass rate limiting |
| CleanupInterval | duration | `cleanup_interval` | `5m` | Interval to remove stale IP limiters |
| IPTTL | duration | `ip_ttl` | `10m` | TTL for inactive IP limiters |

#### Path Limits (`rate_limit.path_limits[]`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| Path | string | `path` | `""` | Exact path or prefix (with `*`) |
| RPS | float | `rps` | `0` | Requests per second for this path |
| Burst | int | `burst` | `0` | Burst size for this path |

---

### Logging (`logging`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| Level | string | `level` | `"info"` | Minimum log level: `debug`, `info`, `warn`, `error` |
| Format | string | `format` | `"json"` | Log format: `json` or `console` |
| File | object | `file` | `null` | File output with rotation |

#### File (`logging.file`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| Path | string | `path` | `""` | Log file path |
| MaxSizeMB | int | `max_size_mb` | `100` | Rotate at this size (MB) |
| MaxBackups | int | `max_backups` | `5` | Max old log files to retain |
| MaxAgeDays | int | `max_age_days` | `30` | Delete rotated files after N days |
| Compress | bool | `compress` | `true` | Gzip compress rotated files |

---

### Rules (`rules[]`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| ID | string | `id` | auto-generated `"rule-N"` | Unique rule identifier |
| Match | object | `match` | — | Request matching criteria |
| Redirect | object | `redirect` | — | Response behavior |
| Priority | int | `priority` | `0` | Evaluation priority (higher first) |

#### Match (`rules[].match`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| Type | string | `type` | `"exact"` | Match type: `exact`, `prefix`, `regex`, `glob` |
| Path | string | `path` | `""` | Path to match (for `exact`/`prefix`) |
| Pattern | string | `pattern` | `""` | Pattern to match (for `regex`/`glob`) |
| Host | string | `host` | `""` | Hostname to match (enables host allowlist) |
| Conditions | map | `conditions` | `null` | Additional match conditions |

#### Redirect / Response (`rules[].redirect`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| Status | int | `status` | inherited from `defaults.status_code` | HTTP status code (100–599) |
| To | string | `to` | `""` | Redirect destination URL |
| Location | string | `location` | `""` | Alias for `to` |
| Body | string | `body` | `""` | Response body (for non-redirect responses) |
| PreservePath | bool | `preserve_path` | `false` | Append matched path suffix to destination |
| PreserveQuery | bool | `preserve_query` | inherited from `defaults.preserve_query` | Forward query string |
| Headers | map | `headers` | `null` | Custom response headers |

---

### Complete Redirector Example

```yaml
# Every redirector config option with inline comments
version: "1.0"

server:
  port: 8080                    # Main traffic port
  management_port: 8081         # Management API port
  read_timeout: 5s
  write_timeout: 5s
  idle_timeout: 120s
  max_connections: 100000       # 0 = unlimited

defaults:
  status_code: 301              # Default redirect status
  preserve_query: true          # Forward query strings by default
  headers:
    X-Powered-By: "the-redirector"

stats:
  enabled: true
  buffer_size: 1000             # Ring buffer for live view
  sampling_rate: 1.0            # 1.0 = all requests, 0.01 = 1%

auth:
  enabled: true
  api_keys:
    - name: "ci-deploy"
      key: ${DEPLOY_API_KEY}
      permissions: [reload, read]
    - name: "admin"
      key: ${ADMIN_API_KEY}
      permissions: [admin]
  jwt:
    enabled: true
    secret: ${JWT_SECRET}
    issuer: "auth.example.com"
    audience: "redirector"
  allow_ips:
    - "127.0.0.1"
    - "10.0.0.0/8"

tracing:
  enabled: true
  endpoint: "localhost:4317"
  service_name: "the-redirector"
  environment: "production"
  sampling_rate: 0.1            # Sample 10% of requests
  insecure: false

rate_limit:
  enabled: true
  global_rps: 10000
  global_burst: 20000
  per_ip_rps: 100
  per_ip_burst: 200
  trust_proxy: true
  exempt_ips:
    - "10.0.0.0/8"
  path_limits:
    - path: "/api/*"
      rps: 50
      burst: 100

logging:
  level: info
  format: json
  file:
    path: /var/log/redirector/redirector.log
    max_size_mb: 100
    max_backups: 5
    max_age_days: 30
    compress: true

rules:
  # Exact match
  - id: homepage
    match:
      type: exact
      host: www.example.com
      path: /old-home
    redirect:
      to: https://example.com/
      status: 301

  # Prefix match with path preservation
  - id: blog-redirect
    match:
      type: prefix
      host: www.example.com
      path: /blog/
    redirect:
      to: https://blog.example.com/
      preserve_path: true
      status: 301

  # Regex with capture groups
  - id: product-redirect
    match:
      type: regex
      host: www.example.com
      pattern: ^/product/(\d+)$
    redirect:
      to: https://shop.example.com/item/$1
      status: 302

  # Glob match
  - id: docs-redirect
    match:
      type: glob
      host: www.example.com
      pattern: /docs/*/guide
    redirect:
      to: https://docs.example.com/
      preserve_path: true

  # Non-redirect response (404)
  - id: block-wp-admin
    match:
      type: prefix
      host: www.example.com
      path: /wp-admin
    redirect:
      status: 404
      body: "Not Found"

  # Maintenance mode
  - id: maintenance
    match:
      type: glob
      host: www.example.com
      pattern: /api/**
    redirect:
      status: 503
      body: "Service temporarily unavailable"
      headers:
        Retry-After: "3600"
    priority: 1000

  # Environment variable in destination
  - id: api-gateway
    match:
      type: prefix
      host: www.example.com
      path: /gateway/
    redirect:
      to: ${API_GATEWAY_URL:-https://gateway.example.com}/
      preserve_path: true

  # Low-priority fallback
  - id: fallback
    match:
      type: glob
      host: www.example.com
      pattern: /**
    redirect:
      to: https://example.com/not-found
      status: 302
    priority: -100

# Include external rule files
rules_include:
  - rules/marketing.csv
  - rules/legacy-urls.csv
  - rules/api-redirects.yaml
```

---

## Syncer Config (`syncer.yaml`)

### Root Fields

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| SyncInterval | duration | `sync_interval` | `60s` | How often to check for config updates |
| Sources | array | `sources` | `[]` | Configuration sources |
| Targets | array | `targets` | `[]` | Push targets (redirector instances) |
| Merge | object | `merge` | — | Multi-source merge behavior |
| Webhook | object | `webhook` | disabled | Webhook server for push-based updates |
| Retry | object | `retry` | — | Retry behavior on failed pushes |
| LogLevel | string | `log_level` | `"info"` | Log level: `debug`, `info`, `warn`, `error` |
| LogFormat | string | `log_format` | `""` | Log format: `json` or `console` |

---

### Sources (`sources[]`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| Name | string | `name` | auto-generated | Human-readable source name |
| Type | string | `type` | **(required)** | Source type (see below) |
| Prefix | string | `prefix` | `""` | Auto-prepended to all rule IDs from this source |
| Priority | int | `priority` | `0` | Merge order (higher = wins in conflicts) |
| AllowedPaths | array | `allowed_paths` | `[]` | Restrict which path prefixes this source can define |
| Config | map | `config` | `{}` | Source-specific configuration (see per-type sections) |

#### Source: File (`type: file`)

| Config Key | Type | Default | Description |
|------------|------|---------|-------------|
| `path` | string | **(required)** | Local filesystem path |
| `watch` | bool | `false` | Enable fsnotify file watching |

#### Source: S3 (`type: s3`)

| Config Key | Type | Default | Description |
|------------|------|---------|-------------|
| `bucket` | string | **(required)** | S3 bucket name |
| `key` | string | **(required)** | Object path in bucket |
| `region` | string | SDK default | AWS region |
| `role_arn` | string | `""` | Cross-account IAM role ARN |
| `endpoint` | string | `""` | S3-compatible endpoint (MinIO, LocalStack) |
| `poll_interval` | duration | `5m` | Change detection interval |

#### Source: Azure Blob (`type: azureblob`)

| Config Key | Type | Default | Description |
|------------|------|---------|-------------|
| `storage_account` | string | `""` | Azure storage account name |
| `container` | string | **(required)** | Blob container name |
| `blob_name` | string | **(required)** | Blob name |
| `connection_string` | string | `""` | Azure connection string |
| `account_key` | string | `""` | Storage account key |
| `sas_token` | string | `""` | Shared access signature token |
| `use_default_credential` | bool | `false` | Use Azure DefaultAzureCredential |
| `poll_interval` | duration | `5m` | Change detection interval |

#### Source: GCS (`type: gcs`)

| Config Key | Type | Default | Description |
|------------|------|---------|-------------|
| `bucket` | string | **(required)** | GCS bucket name |
| `object` | string | **(required)** | Object name |
| `project` | string | `""` | GCP project ID |
| `credentials_file` | string | `""` | Path to service account JSON |
| `credentials_json` | string | `""` | Inline JSON credentials |
| `endpoint` | string | `""` | Custom endpoint (emulator) |
| `poll_interval` | duration | `5m` | Change detection interval |

Environment: `STORAGE_EMULATOR_HOST` — if set, GCS uses the emulator endpoint.

#### Source: GitHub (`type: github`)

| Config Key | Type | Default | Description |
|------------|------|---------|-------------|
| `repository` | string | **(required)** | `"owner/repo"` format |
| `path` | string | `"config.yaml"` | Path to config file in repo |
| `token` | string | `""` | Personal Access Token |
| `base_url` | string | `"https://api.github.com"` | GitHub API URL (for GHES) |
| `strategy` | string | `"release"` | Deployment strategy: `release`, `tag`, `branch`, `commit` |
| `environment` | string | `"production"` | Maps to branch/release channel |
| `tag_pattern` | string | `""` | Glob for tag matching (e.g., `"v*"`) |
| `webhook_secret` | string | `""` | Webhook payload validation secret |

**GitHub App auth** (alternative to PAT):

| Config Key | Type | Default | Description |
|------------|------|---------|-------------|
| `app.app_id` | int | `0` | GitHub App ID |
| `app.installation_id` | int | `0` | Installation ID |
| `app.private_key_path` | string | `""` | Path to App's private key file |
| `app.private_key` | string | `""` | PEM-encoded private key inline |

See [GITHUB_INTEGRATION.md](GITHUB_INTEGRATION.md) for detailed setup.

#### Source: GitLab (`type: gitlab`)

| Config Key | Type | Default | Description |
|------------|------|---------|-------------|
| `project` | string | **(required)** | `"namespace/project"` format |
| `path` | string | `"config.yaml"` | Path to config file in repo |
| `base_url` | string | `"https://gitlab.com"` | GitLab URL (for self-hosted) |
| `token` | string | `""` | PAT/Project/Group access token |
| `token_type` | string | `"private-token"` | `"private-token"` or `"oauth"` |
| `strategy` | string | `"release"` | Deployment strategy: `release`, `tag`, `branch`, `commit` |
| `environment` | string | `"production"` | Maps to branch/release channel |
| `tag_pattern` | string | `""` | Glob for tag matching |
| `webhook_token` | string | `""` | Webhook validation token |

#### Source: HTTP (`type: http`)

| Config Key | Type | Default | Description |
|------------|------|---------|-------------|
| `url` | string | **(required)** | HTTP/HTTPS endpoint |
| `headers` | map | `null` | Custom request headers |
| `timeout` | duration | `30s` | HTTP request timeout |
| `bearer_token` | string | `""` | Bearer token for auth |
| `basic_auth.username` | string | `""` | Basic auth username |
| `basic_auth.password` | string | `""` | Basic auth password |
| `poll_interval` | duration | `30s` | Change detection interval |

Supports ETag-based caching automatically.

#### Source: Parameter Store (`type: parameterstore`)

| Config Key | Type | Default | Description |
|------------|------|---------|-------------|
| `path` | string | **(required)** | SSM parameter path (trailing `/` for hierarchy) |
| `region` | string | SDK default | AWS region |
| `role_arn` | string | `""` | IAM role ARN for assume role |
| `with_decryption` | bool | `true` | Decrypt SecureString parameters |
| `recursive` | bool | `false` | Recursively fetch parameters |
| `poll_interval` | duration | `1m` | Change detection interval |

#### Source: Secrets Manager (`type: secretsmanager`)

| Config Key | Type | Default | Description |
|------------|------|---------|-------------|
| `secret_id` | string | **(required)** | Secret name or ARN |
| `region` | string | SDK default | AWS region |
| `role_arn` | string | `""` | IAM role ARN for assume role |
| `version_id` | string | `""` | Specific version ID |
| `version_stage` | string | `""` | Version stage (e.g., `AWSCURRENT`) |
| `cache_ttl` | duration | `5m` | Cache time-to-live |
| `poll_interval` | duration | `5m` | Change detection interval |

#### Source: Consul (`type: consul`)

| Config Key | Type | Default | Description |
|------------|------|---------|-------------|
| `key` | string | **(required)** | KV path |
| `address` | string | `"127.0.0.1:8500"` | Consul HTTP address |
| `datacenter` | string | agent default | Datacenter to query |
| `token` | string | `""` | ACL token |
| `namespace` | string | `""` | Consul Enterprise namespace |
| `partition` | string | `""` | Consul Enterprise partition |
| `wait_time` | duration | `5m` | Blocking query wait time |

**TLS** (`tls` sub-object):

| Config Key | Type | Default | Description |
|------------|------|---------|-------------|
| `tls.address` | string | `""` | SNI/verification address |
| `tls.ca_file` | string | `""` | CA certificate path |
| `tls.ca_path` | string | `""` | CA certificate directory |
| `tls.cert_file` | string | `""` | Client certificate |
| `tls.key_file` | string | `""` | Client key |
| `tls.insecure_skip_verify` | bool | `false` | Skip TLS verification |

#### Source: etcd (`type: etcd`)

| Config Key | Type | Default | Description |
|------------|------|---------|-------------|
| `key` | string | **(required)** | KV key path |
| `endpoints` | array | `["127.0.0.1:2379"]` | etcd server addresses |
| `username` | string | `""` | Authentication username |
| `password` | string | `""` | Authentication password |
| `dial_timeout` | duration | `5s` | Connection timeout |
| `request_timeout` | duration | `10s` | Individual request timeout |

**TLS** (`tls` sub-object):

| Config Key | Type | Default | Description |
|------------|------|---------|-------------|
| `tls.cert_file` | string | `""` | Client certificate |
| `tls.key_file` | string | `""` | Client key |
| `tls.ca_file` | string | `""` | Trusted CA certificate |
| `tls.insecure_skip_verify` | bool | `false` | Skip TLS verification |

---

### Targets (`targets[]`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| Name | string | `name` | `""` | Friendly identifier |
| URL | string | `url` | **(required)** | Redirector management API endpoint |
| APIKey | string | `api_key` | `""` | Authentication key |
| Timeout | duration | `timeout` | `10s` | Request timeout |

---

### Merge (`merge`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| ConflictResolution | string | `conflict_resolution` | `"error"` | `"error"`, `"priority"`, or `"first"` |
| ValidateBeforePush | bool | `validate_before_push` | `false` | Validate merged config before push |
| RequirePrefix | bool | `require_prefix` | `false` | Require all sources to have a prefix |

---

### Webhook (`webhook`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| Enabled | bool | `enabled` | `false` | Enable webhook server |
| Port | int | `port` | `0` | Webhook server port |
| Secret | string | `secret` | `""` | Webhook validation secret |

---

### Retry (`retry`)

| Field | Type | YAML Key | Default | Description |
|-------|------|----------|---------|-------------|
| Attempts | int | `attempts` | `3` | Max retry attempts |
| Delay | duration | `delay` | `1s` | Initial retry delay (exponential backoff) |

---

### Complete Syncer Example

```yaml
# Every syncer config option with inline comments
sync_interval: 60s
log_level: info                   # debug, info, warn, error
log_format: json                  # json or console

sources:
  # Local file with fsnotify watching
  - name: local-fallback
    type: file
    prefix: "local"
    priority: 1
    config:
      path: /etc/redirector/config.yaml
      watch: true

  # AWS S3
  - name: s3-primary
    type: s3
    prefix: "s3"
    priority: 100
    config:
      bucket: my-config-bucket
      key: config/redirector.yaml
      region: us-east-1
      role_arn: arn:aws:iam::123456789:role/redirector-config
      poll_interval: 5m

  # Azure Blob
  - name: azure-backup
    type: azureblob
    prefix: "azure"
    priority: 50
    config:
      storage_account: mystorageaccount
      container: configs
      blob_name: redirector.yaml
      connection_string: ${AZURE_STORAGE_CONNECTION_STRING}

  # GCS
  - name: gcs-config
    type: gcs
    prefix: "gcs"
    priority: 50
    config:
      bucket: my-config-bucket
      object: redirector/config.yaml
      credentials_file: /path/to/service-account.json

  # GitHub with PAT
  - name: github-eng
    type: github
    prefix: "eng"
    priority: 20
    allowed_paths:
      - "/docs/"
      - "/api/"
    config:
      repository: myorg/engineering-redirects
      path: config/rules.yaml
      token: ${GITHUB_TOKEN}
      strategy: release
      environment: production
      tag_pattern: "v*"

  # GitHub with App auth
  - name: github-platform
    type: github
    prefix: "platform"
    priority: 100
    config:
      repository: myorg/platform-config
      path: redirects.yaml
      strategy: branch
      environment: main
      app:
        app_id: 12345
        installation_id: 67890
        private_key_path: /etc/redirector/github-app.pem

  # GitLab
  - name: gitlab-marketing
    type: gitlab
    prefix: "marketing"
    priority: 10
    allowed_paths:
      - "/promo/"
      - "/campaign/"
    config:
      project: mygroup/marketing-redirects
      path: config/redirector.yaml
      base_url: https://gitlab.example.com
      token: ${GITLAB_TOKEN}
      token_type: private-token
      strategy: release

  # HTTP endpoint
  - name: http-config
    type: http
    prefix: "http"
    priority: 30
    config:
      url: https://config-server.internal/redirector/config.yaml
      bearer_token: ${CONFIG_SERVER_TOKEN}
      timeout: 10s
      poll_interval: 30s
      headers:
        X-Custom-Header: "my-value"

  # AWS Parameter Store
  - name: aws-params
    type: parameterstore
    prefix: "ssm"
    priority: 40
    config:
      path: /myapp/redirector/config
      region: us-east-1
      with_decryption: true
      recursive: false
      poll_interval: 1m

  # AWS Secrets Manager
  - name: aws-secrets
    type: secretsmanager
    prefix: "secrets"
    priority: 45
    config:
      secret_id: myapp/redirector-config
      region: us-east-1
      version_stage: AWSCURRENT
      cache_ttl: 5m

  # Consul
  - name: consul-kv
    type: consul
    prefix: "consul"
    priority: 60
    config:
      key: redirector/config
      address: consul.service.consul:8500
      datacenter: dc1
      token: ${CONSUL_TOKEN}
      namespace: ""
      wait_time: 5m
      tls:
        ca_file: /etc/ssl/consul-ca.pem
        cert_file: /etc/ssl/consul-cert.pem
        key_file: /etc/ssl/consul-key.pem

  # etcd
  - name: etcd-kv
    type: etcd
    prefix: "etcd"
    priority: 70
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
      tls:
        cert_file: /etc/ssl/etcd-cert.pem
        key_file: /etc/ssl/etcd-key.pem
        ca_file: /etc/ssl/etcd-ca.pem

merge:
  conflict_resolution: priority   # error, priority, first
  validate_before_push: true
  require_prefix: true

targets:
  - name: redirector-prod-1
    url: http://redirector-1:8081
    api_key: ${REDIRECTOR_API_KEY}
    timeout: 10s
  - name: redirector-prod-2
    url: http://redirector-2:8081
    api_key: ${REDIRECTOR_API_KEY}
    timeout: 10s

webhook:
  enabled: true
  port: 9090
  secret: ${WEBHOOK_SECRET}

retry:
  attempts: 3
  delay: 1s
```

---

## CLI Flags

### `redirector`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-config` | string | `"config.yaml"` | Path to config file or directory |
| `-log-level` | string | `"info"` | Log level: `debug`, `info`, `warn`, `error` |
| `-watch` | bool | `true` | Watch config for hot-reload via fsnotify |
| `-watch-debounce` | duration | `500ms` | Debounce duration for config reload |
| `-allow-empty` | bool | `false` | Start with empty config directory (useful with redirector-sync) |
| `-version` | bool | `false` | Show version and exit |

**Subcommands:**

| Command | Description |
|---------|-------------|
| `redirector sync` | Trigger config reload via management API |
| `redirector version` | Show version |
| `redirector help` | Show help |

**`redirector sync` flags:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-url` | string | `"http://localhost:8081"` | Management API URL |
| `-timeout` | duration | `10s` | Request timeout |

### `redirector-sync`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-config` | string | `"syncer.yaml"` | Path to syncer configuration |
| `-one-shot` | bool | `false` | Run once and exit |
| `-dry-run` | bool | `false` | Fetch config but don't write output |
| `-lint` | bool | `false` | Lint mode: validate and exit |
| `-lint-json` | bool | `false` | Output lint results as JSON |
| `-lint-quiet` | bool | `false` | Only show lint errors (no warnings) |
| `-lint-config` | string | `""` | Path to config file to lint directly |

### `redirector-tui`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-url` | string | `"http://localhost:8081"` | Management API base URL |
| `-syncer-url` | string | `""` | Config syncer status URL for multi-team view |

---

## Build-Time Variables

Set via `-ldflags` during `go build`:

| Variable | Default | Description |
|----------|---------|-------------|
| `version` | `"dev"` | Application version string |
| `buildTime` | `"unknown"` | Build timestamp |

Example:
```bash
go build -ldflags "-X main.version=1.2.0 -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" ./cmd/redirector
```

---

## Environment Variables

| Variable | Where Used | Description |
|----------|-----------|-------------|
| `${VAR}` / `${VAR:-default}` in YAML | All config files | Expanded before YAML parsing via `os.ExpandEnv()` |
| `STORAGE_EMULATOR_HOST` | GCS provider | If set, GCS uses the storage emulator endpoint |

---

## Signal Handling

| Signal | Action |
|--------|--------|
| `SIGHUP` | Trigger manual config reload (server) |
| `SIGINT` / `SIGTERM` | Graceful shutdown (server and syncer) |
