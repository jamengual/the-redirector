# The Redirector

> A high-performance, enterprise-grade vanity URL redirect service built in Go

## Vision

The Redirector aims to be the definitive solution for managing URL redirects at scale. Born from real-world experience managing redirect infrastructure at large enterprises like Electronic Arts, this project addresses the pain points of existing solutions:

- **nginx limitations**: Complex configuration syntax, no dynamic reload without restart, difficult distributed config
- **AWS S3 redirects**: Limited to 50 routing rules, no regex support, HTTPS requires CloudFront
- **Lambda@Edge**: Cold start latency, JavaScript runtime overhead, complex deployment

## Research Summary: Existing Solutions

### Commercial & Open Source Redirect Services

| Solution | Pros | Cons |
|----------|------|------|
| **nginx** | Fast, battle-tested, PCRE regex | Static config, reload required, complex syntax |
| **Traefik** | Dynamic config, hot-reload, Docker-native | Heavy for simple redirects, resource intensive |
| **Caddy** | Auto TLS, simple config, Go-based | Less optimized for pure redirects |
| **AWS S3/CloudFront** | Serverless, global edge | 50 rule limit, no regex, HTTPS needs CloudFront |
| **Lambda@Edge** | Edge locations, flexible | Cold starts, JavaScript overhead, complex |
| **Envoy** | Advanced traffic management | Complex configuration, overkill for redirects |

### Key Features from Existing Solutions

#### Redirect Types
- **301 Permanent**: SEO-friendly, cached by browsers
- **302 Temporary**: Not cached, good for A/B testing
- **307 Temporary (preserve method)**: Maintains POST/PUT methods
- **308 Permanent (preserve method)**: Like 301 but preserves method

#### Pattern Matching (from nginx)
- Exact match: `= /path`
- Prefix match: `^~ /prefix`
- Regex match: `~ /pattern.*`
- Case-insensitive regex: `~* /pattern.*`

#### Configuration Sources (enterprise requirements)
- Local files (YAML, JSON, TOML)
- Environment variables
- AWS S3 buckets
- AWS Parameter Store
- AWS Secrets Manager
- Azure Blob Storage
- GCP Cloud Storage
- HashiCorp Consul/Vault
- etcd
- REST API push/pull

## Architecture Goals

### Performance Targets
- **Latency**: < 1ms p99 for redirect response
- **Throughput**: > 100,000 requests/second per instance
- **Memory**: < 100MB for 100,000 rules
- **Startup**: < 1 second cold start

### Scalability
- Horizontal scaling with stateless instances
- Distributed configuration synchronization
- No single point of failure
- Edge deployment ready (Kubernetes, Docker, bare metal)

### Reliability
- Zero-downtime configuration updates
- Graceful degradation on config source failure
- Health checks and readiness probes
- Comprehensive metrics and observability

## Feature Matrix

### Core Features (Phase 1)
- [ ] HTTP/HTTPS redirect handling
- [ ] 301/302/307/308 redirect types
- [ ] Exact path matching
- [ ] Prefix matching
- [ ] Basic regex patterns
- [ ] Custom response headers
- [ ] Health check endpoint
- [ ] Prometheus metrics

### Advanced Routing (Phase 2)
- [ ] Full regex with capture groups
- [ ] Glob patterns (`/**`, `/*/path/*`)
- [ ] Query string preservation/manipulation
- [ ] Host-based routing
- [ ] Path rewriting with substitution
- [ ] Conditional redirects (headers, user-agent)

### Configuration Management (Phase 3)
- [ ] YAML/JSON/TOML file configuration
- [ ] Environment variable injection
- [ ] Hot-reload without restart
- [ ] Configuration validation
- [ ] REST API for config push
- [ ] Webhook notifications

### Distributed Configuration (Phase 4)
- [ ] AWS S3 pull with polling/events
- [ ] AWS Parameter Store integration
- [ ] AWS Secrets Manager integration
- [ ] Azure Blob Storage
- [ ] GCP Cloud Storage
- [ ] HashiCorp Consul
- [ ] etcd support
- [ ] Configuration versioning

### Enterprise Features (Phase 5)
- [ ] Multi-tenant support
- [ ] Role-based access control
- [ ] Audit logging
- [ ] Rate limiting
- [ ] Circuit breaker patterns
- [ ] A/B testing support
- [ ] Analytics and reporting
- [ ] Admin dashboard UI

## Technical Decisions

### Why Go?
- **Performance**: Near-C performance with memory safety
- **Concurrency**: Goroutines handle massive concurrent connections
- **Deployment**: Single binary, no runtime dependencies
- **Ecosystem**: Excellent HTTP libraries (fasthttp, net/http)
- **Cloud Native**: First-class Kubernetes support

### Why fasthttp?
- 10x faster than net/http for simple handlers
- Zero allocation routing with radix trees
- Production proven at 17M+ requests/day
- Perfect for redirect workloads (simple request/response)

### Router Strategy: Radix Tree
- O(k) lookup where k is path length
- Memory efficient for common prefixes
- Supports dynamic path parameters
- Zero garbage during matching

## Configuration Format

```yaml
# config.yaml
version: "1.0"
defaults:
  status_code: 301
  preserve_query: true
  headers:
    X-Redirected-By: "the-redirector"

rules:
  # Exact match
  - match:
      type: exact
      path: /old-page
    redirect:
      to: https://example.com/new-page
      status: 301

  # Prefix match with path preservation
  - match:
      type: prefix
      path: /blog/
    redirect:
      to: https://newblog.example.com/
      preserve_path: true

  # Regex with capture groups
  - match:
      type: regex
      pattern: ^/products/(\d+)/reviews$
    redirect:
      to: https://shop.example.com/item/$1/feedback
      status: 302

  # Glob pattern
  - match:
      type: glob
      pattern: /api/v1/**
    redirect:
      to: https://api.example.com/v2/
      preserve_path: true

  # Conditional redirect
  - match:
      type: exact
      path: /download
      conditions:
        user_agent_contains: "Mobile"
    redirect:
      to: https://m.example.com/download

  # Host-based redirect
  - match:
      type: prefix
      host: old.example.com
      path: /
    redirect:
      to: https://new.example.com/
```

## API Specification

### Management API

```
GET  /api/v1/health          - Health check
GET  /api/v1/ready           - Readiness probe
GET  /api/v1/metrics         - Prometheus metrics
GET  /api/v1/config          - Current configuration
POST /api/v1/config          - Update configuration
POST /api/v1/config/validate - Validate configuration
POST /api/v1/reload          - Trigger config reload
GET  /api/v1/rules           - List all rules
GET  /api/v1/rules/{id}      - Get specific rule
POST /api/v1/rules           - Add rule
PUT  /api/v1/rules/{id}      - Update rule
DELETE /api/v1/rules/{id}    - Delete rule
```

## Competitive Advantages

1. **Purpose-Built**: Optimized specifically for redirects, not general proxying
2. **Dynamic Config**: Hot-reload from multiple sources without restart
3. **Enterprise Ready**: S3, Parameter Store, Secrets Manager integration
4. **Developer Friendly**: Simple YAML config, REST API, comprehensive docs
5. **Observable**: Built-in Prometheus metrics, structured logging
6. **Fast**: fasthttp + radix tree = sub-millisecond latency
7. **Portable**: Single binary, Docker-ready, Kubernetes-native

## Success Metrics

- Latency p99 < 1ms at 100k rules
- Throughput > 100k req/s on modest hardware (4 CPU, 8GB RAM)
- Zero-downtime config updates
- Memory usage linear with rule count
- Config reload < 100ms for 100k rules

## References

### Performance Benchmarks (Prior Art)
- fasthttp: 10x faster than net/http
- Radix tree routing: O(k) vs O(n) for linear search
- Go HTTP servers: comparable to nginx for simple workloads

### Similar Projects
- [traefik](https://traefik.io/) - Cloud native edge router
- [caddy](https://caddyserver.com/) - Modern web server with auto HTTPS
- [envoy](https://www.envoyproxy.io/) - Cloud native proxy
- [fasthttp/router](https://github.com/fasthttp/router) - Radix tree router
