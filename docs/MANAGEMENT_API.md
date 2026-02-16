# Management API

The management API runs on a separate port (default: 8081) and provides health checks, statistics, configuration inspection, and operational controls.

## Health Endpoints

```bash
# Liveness probe
curl http://localhost:8081/health
# {"status":"healthy"}

# Readiness probe
curl http://localhost:8081/ready
# {"status":"ready"}
```

Use these with Kubernetes liveness/readiness probes or any health-check system.

---

## Stats Endpoints

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

> **Note:** Stats collection is disabled by default for maximum performance. Enable it in your config or at runtime via the enable endpoint. See [CONFIGURATION.md](CONFIGURATION.md) for the `stats` section.

---

## Config Endpoints

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

## Prometheus Metrics

The `/metrics` endpoint exposes Prometheus-format metrics:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `redirector_requests_total` | Counter | method, status, rule_id | Total requests |
| `redirector_request_duration_seconds` | Histogram | method, status, rule_id | Request duration |
| `redirector_requests_in_flight` | Gauge | — | Current in-flight requests |
| `redirector_response_size_bytes` | Histogram | status | Response size |
| `redirector_config_reloads_total` | Counter | status | Config reload count (success/failure) |
| `redirector_config_rules_count` | Gauge | — | Current rule count |
| `redirector_config_last_reload_timestamp_seconds` | Gauge | — | Last reload timestamp |
| `redirector_config_load_duration_seconds` | Histogram | — | Config load time |
| `redirector_config_info` | Gauge | version, hash, source | Current config metadata |
| `redirector_rule_matches_total` | Counter | rule_id, match_type | Rule match count |
| `redirector_host_rejected_total` | Counter | — | Rejected unknown host requests |
| `redirector_rate_limited_total` | Counter | scope | Requests rejected by rate limiting |
| `redirector_build_info` | Gauge | version, commit, build_time, go_version | Build metadata |
| `redirector_uptime_seconds` | Gauge | — | Time since server started |
| `redirector_goroutines` | Gauge | — | Current goroutine count |
| `redirector_memory_alloc_bytes` | Gauge | — | Current memory allocation |
| `go_*` | Various | — | Standard Go runtime metrics |
| `process_*` | Various | — | Standard process metrics (CPU, FDs, memory) |

### redirector-sync Metrics

The syncer exposes metrics at `/metrics` on its webhook server port:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `redirector_sync_sync_total` | Counter | status | Total sync operations |
| `redirector_sync_sync_duration_seconds` | Histogram | status | Sync operation duration |
| `redirector_sync_last_sync_timestamp_seconds` | Gauge | — | Last sync attempt timestamp |
| `redirector_sync_last_sync_success` | Gauge | — | Whether last sync succeeded (1/0) |
| `redirector_sync_fetch_total` | Counter | source, status | Source fetch operations |
| `redirector_sync_fetch_duration_seconds` | Histogram | source | Source fetch duration |
| `redirector_sync_push_total` | Counter | target, status | Config push operations |
| `redirector_sync_push_duration_seconds` | Histogram | target | Config push duration |
| `redirector_sync_rules_fetched` | Gauge | — | Rules from last successful fetch |
| `redirector_sync_lint_errors_total` | Counter | source | Lint errors during sync |

### Webhook Response (redirector-sync)

When the syncer receives a webhook and triggers a sync, the response includes actionable details:

**Success:**
```json
{"status": "ok"}
```

**Lint failure (HTTP 422):**
```json
{
  "status": "lint_failed",
  "source": "github-primary",
  "message": "lint errors found in config from source github-primary (2 errors)",
  "issues": [
    {
      "severity": "error",
      "rule_id": "rule-a",
      "message": "Circular redirect detected: rule-a -> rule-b -> rule-a",
      "suggestion": "Remove one rule from the chain or change a destination to break the cycle"
    }
  ]
}
```

**Other failure (HTTP 500):** Plain text `"Sync failed"`.

Use the `issues` array in CI pipelines to provide specific feedback when config changes introduce problems like circular redirects, duplicate IDs, or overlapping patterns.

---

## Authentication

The management API supports authentication to protect sensitive endpoints. See [CONFIGURATION.md](CONFIGURATION.md) for the `auth` section.

Authentication methods:

- **API Keys** — Pass via `X-API-Key` header
- **JWT** — Pass via `Authorization: Bearer <token>` header
- **IP Allowlist** — Bypass auth for trusted IPs (e.g., localhost, internal networks)

---

## Debug Endpoints

```bash
# Version info
curl http://localhost:8081/version
# {"version":"1.2.0","build_time":"2025-01-15T10:00:00Z"}
```
