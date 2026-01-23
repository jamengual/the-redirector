# Load Testing Specification

This document outlines the load testing strategy for The Redirector, including tools, scenarios, baselines, and continuous performance validation.

## Objectives

1. **Establish Performance Baselines**: Document expected throughput, latency, and resource usage
2. **Detect Regressions**: Integrate performance tests into CI/CD
3. **Capacity Planning**: Understand scaling characteristics
4. **Stress Testing**: Find breaking points and failure modes

## Testing Tools

### Primary: k6

k6 is our primary load testing tool for scripted, realistic scenarios.

**Why k6?**
- JavaScript scripting for complex test scenarios
- Built-in metrics and thresholds
- CI/CD friendly with exit codes
- Grafana integration for visualization
- Active community and documentation

**Installation:**
```bash
# macOS
brew install k6

# Docker
docker pull grafana/k6

# Linux
sudo gpg -k
sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update
sudo apt-get install k6
```

### Secondary: wrk

wrk is used for raw throughput testing to find maximum capacity.

**Why wrk?**
- Extremely efficient (written in C)
- Maximum throughput with minimal client overhead
- Lua scripting for custom requests

**Installation:**
```bash
# macOS
brew install wrk

# Linux (build from source)
git clone https://github.com/wg/wrk.git
cd wrk && make
```

### Tertiary: hey

hey is used for quick, simple load tests during development.

**Installation:**
```bash
go install github.com/rakyll/hey@latest
```

## Test Scenarios

### Scenario 1: Sustained Load (k6)

Tests system stability under normal production load.

```javascript
// test/load/sustained-load.js
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const errorRate = new Rate('errors');
const redirectLatency = new Trend('redirect_latency');

export const options = {
  stages: [
    { duration: '1m', target: 100 },   // Ramp up
    { duration: '10m', target: 100 },  // Sustained load
    { duration: '1m', target: 0 },     // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(99)<100'],  // 99% under 100ms
    errors: ['rate<0.01'],              // Error rate < 1%
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

const paths = [
  '/exact-match-1',
  '/exact-match-2',
  '/prefix/path/to/resource',
  '/regex/12345/details',
  '/glob/a/b/c/d',
];

export default function () {
  const path = paths[Math.floor(Math.random() * paths.length)];
  const res = http.get(`${BASE_URL}${path}`, {
    redirects: 0,  // Don't follow redirects
  });

  const isRedirect = res.status >= 300 && res.status < 400;

  check(res, {
    'is redirect': (r) => isRedirect,
    'has location header': (r) => r.headers['Location'] !== undefined,
  });

  errorRate.add(!isRedirect);
  redirectLatency.add(res.timings.duration);

  sleep(0.1);  // 10 requests per second per VU
}
```

### Scenario 2: Spike Test (k6)

Tests system behavior under sudden traffic spikes.

```javascript
// test/load/spike-test.js
import http from 'k6/http';
import { check } from 'k6';

export const options = {
  stages: [
    { duration: '1m', target: 100 },    // Normal load
    { duration: '10s', target: 1000 },  // Spike to 10x
    { duration: '1m', target: 1000 },   // Hold spike
    { duration: '10s', target: 100 },   // Return to normal
    { duration: '1m', target: 100 },    // Recovery period
  ],
  thresholds: {
    http_req_duration: ['p(95)<500'],   // 95% under 500ms during spike
    http_req_failed: ['rate<0.05'],     // Allow 5% errors during spike
  },
};

export default function () {
  const res = http.get('http://localhost:8080/spike-test-path', {
    redirects: 0,
  });

  check(res, {
    'status is redirect': (r) => r.status >= 300 && r.status < 400,
  });
}
```

### Scenario 3: Soak Test (k6)

Tests for memory leaks and degradation over extended periods.

```javascript
// test/load/soak-test.js
import http from 'k6/http';
import { check } from 'k6';

export const options = {
  stages: [
    { duration: '5m', target: 50 },    // Ramp up
    { duration: '4h', target: 50 },    // Sustained load for 4 hours
    { duration: '5m', target: 0 },     // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(99)<100'],
    http_req_failed: ['rate<0.001'],   // Very low error rate
  },
};

export default function () {
  const res = http.get('http://localhost:8080/soak-test', {
    redirects: 0,
  });

  check(res, {
    'status is redirect': (r) => r.status >= 300 && r.status < 400,
  });
}
```

### Scenario 4: Maximum Throughput (wrk)

Find the maximum requests per second the system can handle.

```bash
# Maximum throughput test
wrk -t12 -c400 -d60s http://localhost:8080/exact-match

# With custom Lua script for varied paths
wrk -t12 -c400 -d60s -s test/load/varied-paths.lua http://localhost:8080
```

**Lua script for varied paths:**
```lua
-- test/load/varied-paths.lua
local paths = {
  "/exact-1",
  "/exact-2",
  "/prefix/a/b",
  "/regex/123",
}

request = function()
  local path = paths[math.random(#paths)]
  return wrk.format("GET", path)
end
```

### Scenario 5: Config Reload Under Load (k6)

Tests zero-downtime configuration updates.

```javascript
// test/load/config-reload.js
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter } from 'k6/metrics';

const configReloads = new Counter('config_reloads');
const reloadErrors = new Counter('reload_errors');

export const options = {
  stages: [
    { duration: '30s', target: 100 },
    { duration: '5m', target: 100 },
    { duration: '30s', target: 0 },
  ],
  thresholds: {
    http_req_failed: ['rate<0.001'],  // No errors during reload
  },
};

export function setup() {
  // Trigger config reloads periodically in background
  // This would be done via a separate process or test
}

export default function () {
  const res = http.get('http://localhost:8080/reload-test', {
    redirects: 0,
  });

  check(res, {
    'no errors during reload': (r) => r.status >= 300 && r.status < 400,
  });
}
```

### Scenario 6: Regex Performance (k6)

Tests performance impact of regex rules.

```javascript
// test/load/regex-performance.js
import http from 'k6/http';
import { Trend } from 'k6/metrics';

const regexLatency = new Trend('regex_match_latency');

export const options = {
  vus: 50,
  duration: '2m',
  thresholds: {
    regex_match_latency: ['p(99)<500'],  // Regex should still be fast
  },
};

const regexPaths = [
  '/product/12345/reviews',
  '/user/johndoe/profile',
  '/api/v1/resource/abc123',
  '/category/electronics/item/42',
];

export default function () {
  const path = regexPaths[Math.floor(Math.random() * regexPaths.length)];
  const res = http.get(`http://localhost:8080${path}`, {
    redirects: 0,
  });

  regexLatency.add(res.timings.duration);
}
```

## Performance Baselines

### Target Metrics

| Metric | Baseline | Target | Critical |
|--------|----------|--------|----------|
| Throughput (exact match) | 100k req/s | 150k req/s | 50k req/s |
| Throughput (prefix match) | 80k req/s | 120k req/s | 40k req/s |
| Throughput (regex match) | 50k req/s | 80k req/s | 25k req/s |
| Latency p50 | 50μs | 30μs | 100μs |
| Latency p99 | 200μs | 100μs | 1ms |
| Latency p99.9 | 1ms | 500μs | 5ms |
| Error rate | 0.001% | 0% | 0.1% |
| Memory (10k rules) | 50MB | 30MB | 100MB |
| Memory (100k rules) | 200MB | 150MB | 500MB |
| Config reload time | 50ms | 20ms | 200ms |

### Hardware Profiles

#### Small (Development)
- 2 vCPU, 4GB RAM
- Expected: 50k req/s

#### Medium (Production)
- 4 vCPU, 8GB RAM
- Expected: 100k req/s

#### Large (High Traffic)
- 8 vCPU, 16GB RAM
- Expected: 200k req/s

## CI/CD Integration

### GitHub Actions Workflow

```yaml
# .github/workflows/performance.yml
name: Performance Tests

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  load-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Build
        run: make build

      - name: Start server
        run: |
          ./redirector --config test/fixtures/load-test-config.yaml &
          sleep 5

      - name: Install k6
        run: |
          sudo gpg -k
          sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
          echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
          sudo apt-get update
          sudo apt-get install k6

      - name: Run load tests
        run: k6 run --out json=results.json test/load/sustained-load.js

      - name: Check thresholds
        run: |
          # k6 exits with non-zero if thresholds fail
          if [ $? -ne 0 ]; then
            echo "Performance regression detected!"
            exit 1
          fi

      - name: Upload results
        uses: actions/upload-artifact@v4
        with:
          name: load-test-results
          path: results.json
```

### Local Performance Testing

```bash
# Quick sanity check
make load-test-quick

# Full performance suite
make load-test-full

# Specific scenario
k6 run test/load/sustained-load.js

# With custom options
k6 run --vus 200 --duration 5m test/load/sustained-load.js

# Export to InfluxDB + Grafana
k6 run --out influxdb=http://localhost:8086/k6 test/load/sustained-load.js
```

## Monitoring During Tests

### Key Metrics to Monitor

1. **Application Metrics**
   - Request rate
   - Error rate
   - Latency percentiles
   - Active connections

2. **System Metrics**
   - CPU usage
   - Memory usage
   - Goroutine count
   - GC pause time

3. **Network Metrics**
   - Bytes in/out
   - Connection errors
   - TCP states

### Grafana Dashboard

Create a Grafana dashboard with panels for:

1. **Request Rate** (requests/second)
2. **Latency Heatmap** (p50, p95, p99)
3. **Error Rate** (%)
4. **Memory Usage** (MB)
5. **CPU Usage** (%)
6. **Goroutines** (count)
7. **Config Reloads** (count, duration)

## Troubleshooting Performance Issues

### High Latency

1. Check regex complexity
2. Profile with `go tool pprof`
3. Review GC activity
4. Check connection pool exhaustion

### Low Throughput

1. Check for goroutine leaks
2. Review connection limits
3. Profile CPU usage
4. Check network saturation

### Memory Growth

1. Check for unclosed resources
2. Profile heap allocations
3. Review config reload memory
4. Check connection buffer sizes

## Benchmarking Commands

### Go Benchmarks

```bash
# Run all benchmarks
go test -bench=. ./...

# Run specific benchmark with memory stats
go test -bench=BenchmarkExactMatch -benchmem ./internal/router/

# Run benchmarks multiple times for stability
go test -bench=. -count=5 ./...

# CPU profiling
go test -bench=. -cpuprofile=cpu.prof ./internal/router/
go tool pprof cpu.prof

# Memory profiling
go test -bench=. -memprofile=mem.prof ./internal/router/
go tool pprof mem.prof
```

### Quick Load Tests (hey)

```bash
# Quick throughput test
hey -n 10000 -c 100 http://localhost:8080/test

# With request rate limiting
hey -n 10000 -c 100 -q 1000 http://localhost:8080/test

# Extended duration
hey -z 60s -c 100 http://localhost:8080/test
```

### Maximum Throughput (wrk)

```bash
# Find max throughput
wrk -t12 -c400 -d30s http://localhost:8080/test

# With latency stats
wrk -t12 -c400 -d30s --latency http://localhost:8080/test
```

## Reporting

### Performance Report Template

```markdown
# Performance Test Report

## Test Configuration
- Date: YYYY-MM-DD
- Version: vX.Y.Z
- Hardware: [specs]
- Config: [number of rules]

## Results Summary

| Metric | Result | Baseline | Status |
|--------|--------|----------|--------|
| Throughput | X req/s | Y req/s | PASS/FAIL |
| p99 Latency | Xms | Yms | PASS/FAIL |
| Error Rate | X% | Y% | PASS/FAIL |
| Memory | XMB | YMB | PASS/FAIL |

## Detailed Results
[Include k6 output, graphs, etc.]

## Recommendations
[Any performance improvements identified]
```
