# Performance Testing Guide

This document describes how to run performance tests for The Redirector and interpret the results.

## Overview

The Redirector is designed for high-throughput, low-latency redirect workloads. Our performance testing infrastructure helps you:

1. Establish baseline performance metrics
2. Test behavior under different resource constraints
3. Identify bottlenecks and breaking points
4. Validate performance doesn't regress with changes

## Prerequisites

### Local Testing

- **Go 1.21+** - For building the redirector
- **k6** - Load testing tool
  ```bash
  # macOS
  brew install k6

  # Linux
  sudo gpg -k
  sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
  echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
  sudo apt-get update
  sudo apt-get install k6

  # Windows
  choco install k6
  ```
- **Docker** (optional) - For resource-limited testing

## Running Tests

### Quick Start

```bash
# Run basic smoke test
./scripts/benchmark.sh --basic

# Run smoke, load, and stress tests
./scripts/benchmark.sh

# Run specific scenario
./scripts/benchmark.sh smoke
./scripts/benchmark.sh load
./scripts/benchmark.sh stress
```

### With Resource Limits (Docker)

Test behavior under constrained resources:

```bash
# Run with minimal resources (0.5 CPU, 64MB RAM)
./scripts/benchmark.sh --docker --profile minimal smoke

# Run across all resource profiles
./scripts/benchmark.sh --docker --all-profiles smoke load
```

### Manual k6 Commands

For fine-grained control:

```bash
# Start redirector with benchmark config
./bin/redirector -config test/config/benchmark.yaml &

# Run basic test
k6 run test/load/basic.js

# Run specific scenario
k6 run --env SCENARIO=stress test/load/scenarios.js

# Custom VUs and duration
k6 run --vus 100 --duration 1m test/load/basic.js
```

## Test Scenarios

| Scenario | Description | VUs | Duration |
|----------|-------------|-----|----------|
| `smoke` | Quick sanity check | 1 | 10s |
| `load` | Sustained load test | 0→50→0 | 3 min |
| `stress` | Find breaking point | 0→500→0 | 7 min |
| `spike` | Sudden traffic burst | 10→500→10 | ~1.5 min |
| `soak` | Long-running stability | 50 | 10 min |
| `exact_only` | Best-case (exact matches) | 1000 rps | 1 min |
| `regex_only` | Worst-case (regex matches) | 1000 rps | 1 min |
| `max_throughput` | Maximum throughput test | 10000 rps | 1 min |

## Resource Profiles

When using Docker for resource-limited testing:

| Profile | CPU | Memory | Use Case |
|---------|-----|--------|----------|
| `minimal` | 0.5 cores | 64 MB | Edge/IoT deployments |
| `low` | 1 core | 128 MB | Small containers |
| `medium` | 2 cores | 256 MB | Standard deployment |
| `high` | 4 cores | 512 MB | High-traffic deployment |

## Performance Baselines

Expected performance characteristics on typical hardware:

### Latency Targets

| Metric | Target | Notes |
|--------|--------|-------|
| p50 latency | < 100µs | Exact match lookups |
| p95 latency | < 500µs | Mixed workload |
| p99 latency | < 1ms | Under normal load |

### Throughput Targets

| Configuration | Expected RPS | Notes |
|---------------|--------------|-------|
| Single core | 50,000+ | Exact matches |
| Single core | 20,000+ | Mixed workload |
| Single core | 10,000+ | Regex-heavy |
| 4 cores | 150,000+ | Mixed workload |

### Memory Usage

| Rules Count | Memory Usage |
|-------------|--------------|
| 100 rules | ~20 MB |
| 1,000 rules | ~50 MB |
| 10,000 rules | ~150 MB |

## Interpreting Results

### Key Metrics

1. **http_req_duration** - Request latency
   - p95 < 100ms (threshold)
   - p99 < 500ms (threshold)

2. **http_req_failed** - Error rate
   - Should be < 1%

3. **http_reqs** - Total requests
   - Higher is better for throughput tests

4. **redirect_latency** - Custom metric for redirect processing time

5. **success_rate** - Percentage of successful redirects

### Reading k6 Output

```
     ✓ status is 3xx
     ✓ has location

     checks.........................: 100.00% ✓ 10000   ✗ 0
     data_received..................: 1.5 MB  50 kB/s
     data_sent......................: 800 kB  27 kB/s
     http_req_blocked...............: avg=1.2µs   min=0s      med=1µs     max=1.2ms   p(90)=2µs    p(95)=2µs
     http_req_connecting............: avg=0s      min=0s      med=0s      max=0s      p(90)=0s     p(95)=0s
     http_req_duration..............: avg=245µs   min=50µs    med=180µs   max=15ms    p(90)=400µs  p(95)=600µs
       { expected_response:true }...: avg=245µs   min=50µs    med=180µs   max=15ms    p(90)=400µs  p(95)=600µs
     http_req_failed................: 0.00%   ✓ 0       ✗ 10000
     http_req_rate..................: 333.33/s
```

Key things to look for:
- **p(95) and p(99)** - Tail latencies
- **http_req_failed** - Should be 0% or very low
- **checks** - All should pass

### Threshold Violations

If thresholds fail, k6 exits with code 99. Common causes:

1. **High p95/p99 latency**
   - Check for regex complexity
   - Verify no GC pressure
   - Check for resource contention

2. **High error rate**
   - Server may be overwhelmed
   - Check for connection limits
   - Verify health endpoint responds

## GitHub Actions Integration

### Automatic Testing

- **Pull Requests**: Smoke test runs automatically on PRs touching performance-critical code
- **Weekly**: Full benchmark suite runs every Monday at 6 AM UTC

### Manual Trigger

1. Go to **Actions** → **Performance Benchmark**
2. Click **Run workflow**
3. Select scenario and resource profile
4. Results are uploaded as artifacts

### Viewing Results

1. Go to the workflow run
2. Download artifacts for each profile
3. Review `summary_*.md` files for overview
4. Check `*.log` files for detailed k6 output

## Tuning for Performance

### Server Configuration

```yaml
server:
  read_timeout: 5s
  write_timeout: 5s
  idle_timeout: 120s
  max_connections: 100000
```

### Stats Impact

Disable stats for maximum performance:

```yaml
stats:
  enabled: false
```

Or use sampling for high-traffic:

```yaml
stats:
  enabled: true
  sampling_rate: 0.01  # 1% sampling
```

### Rule Ordering

1. Put high-traffic exact matches first
2. Avoid catch-all patterns without negative priority
3. Keep regex patterns simple when possible

## Troubleshooting

### Low Throughput

1. Check if stats collection is enabled
2. Verify no debug logging
3. Check for complex regex patterns
4. Ensure adequate file descriptors: `ulimit -n 65535`

### High Latency Spikes

1. Check for GC pauses: add `-gcflags="-m"` to build
2. Monitor memory with pprof
3. Check for lock contention

### Connection Errors

1. Increase `max_connections` in config
2. Increase system limits:
   ```bash
   sysctl -w net.core.somaxconn=65535
   sysctl -w net.ipv4.tcp_max_syn_backlog=65535
   ```

## Contributing

When submitting performance-related changes:

1. Run full benchmark suite before and after
2. Include comparison in PR description
3. Document any expected performance changes
4. Add regression tests for critical paths
