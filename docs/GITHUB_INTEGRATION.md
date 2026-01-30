# GitHub Integration Guide

This guide covers setting up GitHub as a configuration source for The Redirector using best practices including GitHub App authentication and release-based deployments.

## Overview

The GitHub integration allows you to:
- Store redirect configurations in a Git repository
- Use releases for production deployments
- Use branches for staging/development
- Receive real-time updates via webhooks
- Audit all configuration changes through Git history

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                     GitHub Config Repository                        │
│                                                                     │
│   main branch ─────────────────────────────────────────────────────│
│     │                                                               │
│     ├─── config/                                                    │
│     │    ├── base.yaml          (shared config)                    │
│     │    ├── production.yaml    (production overrides)             │
│     │    └── staging.yaml       (staging overrides)                │
│     │                                                               │
│     └─── README.md                                                  │
│                                                                     │
│   Releases: v1.0.0, v1.1.0, v1.2.0 (production deploys)           │
│   Pre-releases: v1.3.0-rc.1 (staging deploys)                      │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              │ Webhook Events
                              │ (release.published, push)
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│                       redirector-sync                                │
│                                                                     │
│   ┌─────────────┐    ┌─────────────┐    ┌─────────────┐           │
│   │   GitHub    │───▶│  Validate   │───▶│    Push     │           │
│   │   Source    │    │   Config    │    │  to Nodes   │           │
│   └─────────────┘    └─────────────┘    └─────────────┘           │
│         │                                                          │
│         │ GitHub App Auth (JWT + Installation Token)               │
└─────────────────────────────────────────────────────────────────────┘
```

## Setting Up GitHub App Authentication

### Why GitHub App?

| Feature | Personal Token | GitHub App |
|---------|----------------|------------|
| Tied to user | Yes | No |
| Fine-grained permissions | Limited | Yes |
| Automatic token rotation | No | Yes |
| Audit trail | Limited | Full |
| Rate limits | Per user | Per app |
| Organization control | Limited | Full |

### Step 1: Create a GitHub App

1. Go to **Settings > Developer settings > GitHub Apps > New GitHub App**

2. Configure the app:
   ```
   App Name: redirector-sync
   Homepage URL: https://your-org.github.io/the-redirector
   Webhook URL: https://syncer.your-domain.com/webhook/github
   Webhook Secret: <generate-secure-secret>
   ```

3. Set permissions:
   ```yaml
   Repository permissions:
     Contents: Read-only          # Read config files
     Metadata: Read-only          # Required for API access

   Subscribe to events:
     - Push                        # Branch updates
     - Release                     # Release publications
     - Create                      # Tag creation
   ```

4. Generate a private key and save it securely

### Step 2: Install the App

1. Go to your GitHub App settings
2. Click **Install App**
3. Select the organization/account
4. Choose **Only select repositories**
5. Select your config repository

### Step 3: Note the IDs

After installation, note:
- **App ID**: Found on the App settings page
- **Installation ID**: Found in the URL after installing (e.g., `/installations/12345678`)

## Authentication Methods

### Method 1: Personal Access Token (PAT) - Simplest

Best for small teams or development environments.

```yaml
sources:
  - type: github
    repository: "my-org/redirect-config"
    path: "config/production.yaml"
    token: ${GITHUB_TOKEN}
```

### Method 2: GitHub App - Recommended for Production

Best for production. Tokens auto-rotate, permissions are fine-grained, and the App is not tied to a user.

```yaml
sources:
  - type: github
    repository: "my-org/redirect-config"
    path: "config/production.yaml"
    app:
      app_id: 123456
      installation_id: 12345678
      private_key_path: /secrets/github-app-key.pem
      # Or inline: private_key: ${GITHUB_APP_PRIVATE_KEY}
```

The provider handles the full GitHub App auth flow:
1. Loads the RSA private key (PKCS1 or PKCS8 PEM format)
2. Creates a JWT signed with RS256 (iss=AppID, 10min expiry)
3. Exchanges JWT for an installation access token via `POST /app/installations/{id}/access_tokens`
4. Caches the token and auto-refreshes 5 minutes before expiry

## Configuration

### redirector-sync Configuration

```yaml
# syncer.yaml
version: "1.0"

sources:
  - type: github
    repository: "my-org/redirect-config"
    path: "config/production.yaml"
    strategy: release
    environment: production

    # Auth option 1: PAT
    token: ${GITHUB_TOKEN}

    # Auth option 2: GitHub App
    app:
      app_id: 123456
      installation_id: 12345678
      private_key_path: /secrets/github-app-key.pem

    webhook_secret: ${GITHUB_WEBHOOK_SECRET}

targets:
  - url: http://redirector:8081
    auth:
      type: bearer
      token: ${REDIRECTOR_API_TOKEN}
```

### Environment Variables

```bash
# For PAT auth
export GITHUB_TOKEN="ghp_xxxxxxxxxxxxxxxxxxxx"

# For GitHub App auth - private key as PEM string
export GITHUB_APP_PRIVATE_KEY="$(cat private-key.pem)"

# Webhook secret for validating payloads
export GITHUB_WEBHOOK_SECRET="your-webhook-secret"

# API token for pushing to redirector
export REDIRECTOR_API_TOKEN="your-api-token"
```

## Deployment Strategies

### Strategy 1: Release-Based (Recommended for Production)

Only deploy when a GitHub Release is published.

```yaml
sources:
  - type: github
    repository: "my-org/redirect-config"
    path: "config/production.yaml"
    strategy: release
    environment: production  # Only non-prerelease releases
```

**Workflow:**
1. Merge PRs to `main`
2. Test changes in staging
3. Create a new release (e.g., `v1.2.0`)
4. Webhook triggers config deployment
5. All redirector instances update atomically

### Strategy 2: Branch-Based (For Staging/Development)

Deploy on every push to a branch.

```yaml
sources:
  - type: github
    repository: "my-org/redirect-config"
    path: "config/staging.yaml"
    strategy: branch
    environment: staging  # Maps to 'staging' branch
```

**Environment to Branch Mapping:**
| Environment | Branch |
|-------------|--------|
| production | main |
| staging | staging |
| development | develop |

### Strategy 3: Tag-Based with Pattern Matching

Deploy when a tag matching a glob pattern is created.

```yaml
sources:
  - type: github
    repository: "my-org/redirect-config"
    path: "config/production.yaml"
    strategy: tag
    tag_pattern: "v*"           # Only deploy on tags starting with "v"
```

**Pattern examples:**
| Pattern | Matches | Doesn't Match |
|---------|---------|---------------|
| `v*` | `v1.0.0`, `v2.0-rc1` | `release-1.0`, `latest` |
| `config-*` | `config-1.0`, `config-prod` | `v1.0`, `release` |
| `release-[0-9]*` | `release-1`, `release-23` | `release-beta` |

Without `tag_pattern`, the latest tag (by date) is always used. With a pattern, only tags matching the glob are considered.

### Strategy 4: Pre-release (Staging with Releases)

Use GitHub pre-releases for staging.

```yaml
sources:
  - type: github
    repository: "my-org/redirect-config"
    path: "config/staging.yaml"
    strategy: release
    environment: staging  # Pre-releases only
```

## Config Repository Structure

### Recommended Structure

```
redirect-config/
├── README.md
├── CODEOWNERS                    # Require reviews
├── .github/
│   └── workflows/
│       └── validate.yaml         # CI validation
│
├── config/
│   ├── base.yaml                 # Shared rules
│   ├── production.yaml           # Production overrides
│   └── staging.yaml              # Staging overrides
│
└── schemas/
    └── config-schema.json        # JSON Schema for validation
```

### CODEOWNERS

```
# .github/CODEOWNERS
# Require platform team review for all config changes
* @my-org/platform-team

# Production requires additional approval
config/production.yaml @my-org/platform-leads
```

### CI Validation Workflow

```yaml
# .github/workflows/validate.yaml
name: Validate Config

on:
  pull_request:
    paths:
      - 'config/**'

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install redirector CLI
        run: |
          curl -L https://github.com/your-org/the-redirector/releases/latest/download/redirector-cli-linux-amd64 -o /usr/local/bin/redirector-cli
          chmod +x /usr/local/bin/redirector-cli

      - name: Validate configs
        run: |
          for config in config/*.yaml; do
            echo "Validating $config..."
            redirector-cli validate "$config"
          done

      - name: Check for conflicts
        run: |
          # Ensure no duplicate rule IDs across configs
          redirector-cli check-conflicts config/
```

## Webhook Setup

### Webhook Handler

The redirector-sync exposes a webhook endpoint:

```
POST /webhook/github
Headers:
  X-GitHub-Event: release
  X-Hub-Signature-256: sha256=...
  X-GitHub-Delivery: <uuid>
```

### Verifying Webhooks

```go
// Validate webhook signature
func validateWebhook(payload []byte, signature, secret string) bool {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(payload)
    expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
    return hmac.Equal([]byte(signature), []byte(expected))
}
```

### Supported Events

| Event | Action | Strategy | Result |
|-------|--------|----------|--------|
| `release` | `published` | release | Deploy new release |
| `push` | - | branch | Deploy branch update (only if matching env branch) |
| `create` | ref_type=tag | tag | Deploy new tag (filtered by `tag_pattern` if set) |

When `tag_pattern` is configured, only tags matching the glob pattern trigger deployment. Non-matching tag creation events are silently ignored.

## Release Workflow

### Creating a Release

```bash
# 1. Ensure main is up to date
git checkout main
git pull origin main

# 2. Create a tag
git tag -a v1.2.0 -m "Release v1.2.0: Add mobile redirect rules"

# 3. Push the tag
git push origin v1.2.0

# 4. Create release on GitHub (or use gh CLI)
gh release create v1.2.0 \
  --title "v1.2.0" \
  --notes "## Changes
- Added mobile redirect rules
- Fixed /api/v1 prefix matching"
```

### Automated Release Notes

```yaml
# .github/release.yml
changelog:
  categories:
    - title: New Rules
      labels:
        - new-rule
    - title: Modified Rules
      labels:
        - rule-change
    - title: Removed Rules
      labels:
        - rule-removal
```

## Rollback

### Using Releases

```bash
# Rollback to previous release
gh release delete v1.2.0  # Delete bad release
# Previous release (v1.1.0) will be detected as latest
```

### Using redirector-sync API

```bash
# List available versions
curl http://syncer:8080/api/v1/versions

# Rollback to specific version
curl -X POST http://syncer:8080/api/v1/rollback \
  -H "Content-Type: application/json" \
  -d '{"version": "v1.1.0"}'
```

### Using Git

```bash
# Revert the config change
git revert HEAD
git push origin main

# Create a new release with the revert
gh release create v1.2.1 --notes "Rollback: Revert changes from v1.2.0"
```

## Monitoring

### Metrics

```prometheus
# Config sync status
redirector_config_sync_total{source="github",status="success"}
redirector_config_sync_total{source="github",status="error"}

# Config version
redirector_config_version{source="github",version="v1.2.0"}

# Webhook events
redirector_webhook_received_total{source="github",event="release"}
redirector_webhook_processed_total{source="github",event="release",status="success"}
```

### Alerts

```yaml
# Alertmanager rules
groups:
  - name: redirector-config
    rules:
      - alert: ConfigSyncFailed
        expr: increase(redirector_config_sync_total{status="error"}[5m]) > 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Config sync from GitHub failed"

      - alert: WebhookDeliveryFailed
        expr: increase(redirector_webhook_received_total[5m]) == 0
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: "No GitHub webhooks received in 15 minutes"
```

## Troubleshooting

### Enable Debug Logging

The redirector-sync and GitHub provider support debug-level logging via zerolog. Add `log_level: debug` to your syncer config:

```yaml
# syncer.yaml
log_level: debug   # trace, debug, info (default), warn, error, fatal

sources:
  - name: "github-config"
    type: github
    # ...
```

Debug output includes:
- **Source creation**: config map (with secrets redacted), auth type, strategy, environment
- **Fetch flow**: strategy resolution, ref lookup, file URL, content size
- **Parse diagnostics**: on failure, logs a preview of the fetched content (first 200 chars)
- **Push retries**: target name, attempt number, backoff delay

### Common Errors

| Error | Cause | Fix |
|-------|-------|-----|
| `no suitable release found for environment production` | Default strategy is `release` but no GitHub Releases exist | Set `strategy: branch` and `ref: <branch-name>` in your syncer config |
| `parsing config: validating config: no rules defined` | Fetched content is not valid redirect config YAML | Enable debug logging to see content preview; check your config file has a `rules:` section |
| `getting auth token: PAT token is empty` | Token env var not set or empty | Verify `${GITHUB_TOKEN}` is exported in your environment |
| `GitHub API returned 404` | Repository, path, or ref not found | Check owner/repo, file path, and branch/tag name |
| `GitHub API returned 401` | Invalid or expired token | Regenerate PAT or check GitHub App private key |

### redirector-sync `ref` Field

When using the redirector-sync YAML format, the `ref` field maps to the provider's `environment` parameter. If `ref` is set without an explicit `strategy`, the syncer defaults to `strategy: "branch"`:

```yaml
sources:
  - name: "my-config"
    type: github
    github:
      owner: "my-org"
      repo: "my-config-repo"
      path: "config.yaml"
      ref: "main"              # Maps to environment, defaults to branch strategy
      token: "${GITHUB_TOKEN}"
```

This is equivalent to setting `strategy: branch` and `environment: main` in the provider config directly.

## Security Best Practices

1. **Use GitHub App** - Never use personal access tokens in production
2. **Minimal permissions** - Only request `contents:read` and `metadata:read`
3. **Webhook secrets** - Always validate webhook signatures
4. **Private key security** - Store in secrets manager, not in code
5. **Branch protection** - Require reviews for production config
6. **CODEOWNERS** - Enforce team ownership of config files
7. **Audit logging** - Git history provides full audit trail
