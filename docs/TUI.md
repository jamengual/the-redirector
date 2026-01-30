# redirector-tui

Live monitoring dashboard with an htop-style interface for real-time request debugging.

![TUI Demo](tui-demo.gif)

*Generated with [VHS](https://github.com/charmbracelet/vhs). Regenerate: `vhs docs/tui-demo.tape`*

## Quick Start

```bash
# Connect to local management API
./redirector-tui

# Connect to remote server
./redirector-tui --url http://redirector.internal:8081

# With redirector-sync for multi-team conflict view
./redirector-tui --url http://redirector:8081 --syncer-url http://redirector-sync:8082
```

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-url` | `http://localhost:8081` | Management API base URL |
| `-syncer-url` | (none) | Config syncer status URL for multi-team view |

---

## Features

- Real-time request stream
- Sort by time, status, latency, path, or rule (keys: 1-5)
- Filter requests (press `f`)
- Pause/resume (press `p` or space)
- Summary stats: uptime, requests/sec, errors, error rate
- Latency histogram
- **Config view** with multi-team conflict status (press `Tab`)

---

## Views

| View | Description |
|------|-------------|
| Traffic | Live request stream with stats |
| Config | Multi-team merge status and conflicts |

---

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `q` | Quit |
| `Tab` | Switch view (Traffic/Config) |
| `p` / `space` | Pause/resume |
| `f` / `/` | Filter mode |
| `c` | Clear filter |
| `r` | Force refresh |
| `1-5` | Sort by column |
| `↑/k`, `↓/j` | Navigate |
| `?` | Help |

---

## Requirements

The TUI connects to the redirector's management API, so the redirector must be running with stats enabled:

```yaml
stats:
  enabled: true
  buffer_size: 1000
  sampling_rate: 1.0
```

See [CONFIGURATION.md](CONFIGURATION.md) for details on the `stats` section.
