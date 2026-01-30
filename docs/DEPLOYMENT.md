# Deployment

## Docker

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o redirector ./cmd/redirector

FROM alpine:latest
COPY --from=builder /app/redirector /usr/local/bin/
COPY config.yaml /etc/redirector/
EXPOSE 8080 8081
CMD ["redirector", "--config", "/etc/redirector/config.yaml"]
```

```bash
docker build -t the-redirector .
docker run -p 8080:8080 -p 8081:8081 the-redirector
```

### Multi-Binary Image

To include all three binaries:

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o redirector ./cmd/redirector && \
    go build -o redirector-sync ./cmd/redirector-sync && \
    go build -o redirector-tui ./cmd/redirector-tui

FROM alpine:latest
COPY --from=builder /app/redirector /usr/local/bin/
COPY --from=builder /app/redirector-sync /usr/local/bin/
COPY --from=builder /app/redirector-tui /usr/local/bin/
COPY config.yaml /etc/redirector/
EXPOSE 8080 8081
CMD ["redirector", "--config", "/etc/redirector/config.yaml"]
```

---

## Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redirector
spec:
  replicas: 3
  selector:
    matchLabels:
      app: redirector
  template:
    metadata:
      labels:
        app: redirector
    spec:
      containers:
      - name: redirector
        image: ghcr.io/your-org/the-redirector:latest
        ports:
        - containerPort: 8080
          name: http
        - containerPort: 8081
          name: management
        livenessProbe:
          httpGet:
            path: /health
            port: management
          initialDelaySeconds: 5
        readinessProbe:
          httpGet:
            path: /ready
            port: management
        resources:
          requests:
            cpu: 100m
            memory: 64Mi
          limits:
            cpu: 1000m
            memory: 256Mi
        volumeMounts:
        - name: config
          mountPath: /etc/redirector
      volumes:
      - name: config
        configMap:
          name: redirector-config
```

### Service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: redirector
spec:
  selector:
    app: redirector
  ports:
  - name: http
    port: 80
    targetPort: http
  - name: management
    port: 8081
    targetPort: management
```

### ConfigMap

```bash
kubectl create configmap redirector-config --from-file=config.yaml
```

---

## Performance Tuning

### Server Settings

```yaml
server:
  port: 8080
  management_port: 8081
  read_timeout: 5s
  write_timeout: 5s
  idle_timeout: 120s
  max_connections: 100000
```

### Stats Impact

Stats collection is **disabled by default** for maximum performance. When enabled:

- Atomic counters add ~10ns per request
- Ring buffer adds ~50ns per request (with sampling, less)
- Use `sampling_rate: 0.01` for 1% sampling on high-traffic deployments

```yaml
stats:
  enabled: true
  buffer_size: 1000
  sampling_rate: 0.01  # Sample 1% of requests
```

### Logging

Structured JSON logging with automatic rotation:

```yaml
logging:
  level: info          # debug, info, warn, error
  format: json         # json or console
  file:
    path: /var/log/redirector/redirector.log
    max_size_mb: 100   # Rotate at 100MB
    max_backups: 5     # Keep 5 old files
    max_age_days: 30   # Delete after 30 days
    compress: true     # Gzip rotated files
```

### Signal Handling

| Signal | Action |
|--------|--------|
| `SIGHUP` | Trigger config reload |
| `SIGINT` / `SIGTERM` | Graceful shutdown |

---

For full configuration reference, see [CONFIGURATION.md](CONFIGURATION.md).
For load testing methodology and results, see [LOAD_TESTING.md](LOAD_TESTING.md) and [PERFORMANCE.md](PERFORMANCE.md).
