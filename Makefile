.PHONY: all build build-all test bench lint clean docker run help integration-test integration-up integration-down

# Variables
BINARY_NAME=redirector
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

all: lint test build

## Build

build: ## Build the redirector binary
	$(GOBUILD) $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/redirector

build-all: ## Build all binaries (redirector, redirector-lint, redirector-tui, config-syncer)
	$(GOBUILD) $(LDFLAGS) -o bin/redirector ./cmd/redirector
	$(GOBUILD) $(LDFLAGS) -o bin/redirector-lint ./cmd/redirector-lint
	$(GOBUILD) $(LDFLAGS) -o bin/redirector-tui ./cmd/redirector-tui
	$(GOBUILD) $(LDFLAGS) -o bin/config-syncer ./cmd/config-syncer

build-linux: ## Build all binaries for Linux
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o bin/redirector-linux-amd64 ./cmd/redirector
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o bin/redirector-lint-linux-amd64 ./cmd/redirector-lint
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o bin/config-syncer-linux-amd64 ./cmd/config-syncer

## Testing

test: ## Run unit tests
	$(GOTEST) -v -race -cover ./...

test-short: ## Run short tests only
	$(GOTEST) -v -short ./...

test-coverage: ## Run tests with coverage report
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## Integration Testing

integration-up: ## Start integration test infrastructure
	docker compose -f test/integration/docker compose.yml up -d
	@echo "Waiting for services to be ready..."
	@sleep 15
	@chmod +x test/integration/setup.sh
	./test/integration/setup.sh

integration-down: ## Stop integration test infrastructure
	docker compose -f test/integration/docker compose.yml down -v

integration-test: integration-up ## Run integration tests (starts infrastructure if needed)
	AWS_ACCESS_KEY_ID=test \
	AWS_SECRET_ACCESS_KEY=test \
	AWS_DEFAULT_REGION=us-east-1 \
	AWS_ENDPOINT_URL=http://localhost:4566 \
	STORAGE_EMULATOR_HOST=http://localhost:4443 \
	$(GOTEST) -tags=integration -v ./test/integration/...
	@$(MAKE) integration-down

## Benchmarking

bench: ## Run benchmarks
	$(GOTEST) -bench=. -benchmem ./...

bench-router: ## Run router benchmarks
	$(GOTEST) -bench=. -benchmem ./internal/router/

## Load Testing

load-test-quick: ## Run quick load test (30 seconds)
	@if command -v k6 > /dev/null; then \
		k6 run --vus 50 --duration 30s test/load/sustained-load.js; \
	else \
		echo "k6 not installed. Install with: brew install k6"; \
	fi

load-test-full: ## Run full load test suite
	@if command -v k6 > /dev/null; then \
		k6 run test/load/sustained-load.js; \
	else \
		echo "k6 not installed. Install with: brew install k6"; \
	fi

load-test-wrk: ## Run wrk throughput test
	@if command -v wrk > /dev/null; then \
		wrk -t12 -c400 -d30s http://localhost:8080/test; \
	else \
		echo "wrk not installed. Install with: brew install wrk"; \
	fi

## Quality

lint: ## Run linter
	@if command -v golangci-lint > /dev/null; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Install with: brew install golangci-lint"; \
	fi

fmt: ## Format code
	$(GOCMD) fmt ./...

vet: ## Run go vet
	$(GOCMD) vet ./...

## Dependencies

deps: ## Download dependencies
	$(GOMOD) download

tidy: ## Tidy dependencies
	$(GOMOD) tidy

## Docker

docker-build: ## Build Docker image
	docker build -t the-redirector:$(VERSION) .

docker-run: ## Run Docker container
	docker run -p 8080:8080 -p 8081:8081 -v $(PWD)/config.yaml:/config.yaml the-redirector:$(VERSION)

## Development

run: build ## Build and run locally
	./bin/$(BINARY_NAME) --config config.yaml

run-dev: ## Run with go run (faster iteration)
	$(GOCMD) run ./cmd/redirector --config config.yaml --log-level debug

## Cleanup

clean: ## Clean build artifacts
	rm -rf bin/
	rm -f coverage.out coverage.html

## Help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
