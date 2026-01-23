#!/bin/bash
#
# The Redirector - Local Benchmark Runner
#
# Runs load tests with various resource constraints to establish performance baselines.
# Requires: k6, docker (optional, for resource limits)
#
# Usage:
#   ./scripts/benchmark.sh                    # Run all benchmarks
#   ./scripts/benchmark.sh smoke              # Run specific scenario
#   ./scripts/benchmark.sh --docker stress    # Run with Docker resource limits
#

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
RESULTS_DIR="${PROJECT_ROOT}/test/load/results"
CONFIG_FILE="${PROJECT_ROOT}/test/config/benchmark.yaml"
REDIRECTOR_BIN="${PROJECT_ROOT}/bin/redirector"

# Default settings
USE_DOCKER=false
SCENARIOS=("smoke" "load" "stress")
BASE_URL="http://localhost:8080"
MANAGEMENT_URL="http://localhost:8081"

# Resource profiles for Docker testing
declare -A CPU_LIMITS=(
    ["minimal"]="0.5"
    ["low"]="1"
    ["medium"]="2"
    ["high"]="4"
)

declare -A MEMORY_LIMITS=(
    ["minimal"]="64m"
    ["low"]="128m"
    ["medium"]="256m"
    ["high"]="512m"
)

print_header() {
    echo -e "${BLUE}"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  The Redirector - Performance Benchmark"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo -e "${NC}"
}

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_dependencies() {
    log_info "Checking dependencies..."

    if ! command -v k6 &> /dev/null; then
        log_error "k6 is not installed. Install it from https://k6.io/docs/getting-started/installation/"
        exit 1
    fi

    if [ "$USE_DOCKER" = true ] && ! command -v docker &> /dev/null; then
        log_error "Docker is required for resource-limited tests but is not installed"
        exit 1
    fi

    log_info "Dependencies OK"
}

build_redirector() {
    log_info "Building redirector..."

    cd "$PROJECT_ROOT"
    mkdir -p bin

    # Build with optimizations
    CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/redirector ./cmd/redirector

    if [ ! -f "$REDIRECTOR_BIN" ]; then
        log_error "Failed to build redirector"
        exit 1
    fi

    log_info "Build complete: $REDIRECTOR_BIN"
}

start_redirector() {
    local profile="${1:-native}"

    log_info "Starting redirector (profile: $profile)..."

    if [ "$USE_DOCKER" = true ] && [ "$profile" != "native" ]; then
        start_redirector_docker "$profile"
    else
        start_redirector_native
    fi

    # Wait for server to be ready
    local retries=30
    while [ $retries -gt 0 ]; do
        if curl -s "${MANAGEMENT_URL}/health" > /dev/null 2>&1; then
            log_info "Redirector is ready"
            return 0
        fi
        retries=$((retries - 1))
        sleep 0.5
    done

    log_error "Redirector failed to start"
    return 1
}

start_redirector_native() {
    # Kill any existing instance
    pkill -f "redirector.*benchmark.yaml" 2>/dev/null || true
    sleep 1

    "$REDIRECTOR_BIN" -config "$CONFIG_FILE" -log-level warn &
    REDIRECTOR_PID=$!

    # Store PID for cleanup
    echo $REDIRECTOR_PID > "${RESULTS_DIR}/.redirector.pid"
}

start_redirector_docker() {
    local profile="$1"
    local cpu_limit="${CPU_LIMITS[$profile]}"
    local mem_limit="${MEMORY_LIMITS[$profile]}"

    # Stop any existing container
    docker rm -f redirector-bench 2>/dev/null || true

    log_info "Starting with limits: CPU=$cpu_limit, Memory=$mem_limit"

    docker run -d \
        --name redirector-bench \
        --cpus="$cpu_limit" \
        --memory="$mem_limit" \
        -p 8080:8080 \
        -p 8081:8081 \
        -v "${CONFIG_FILE}:/etc/redirector/config.yaml:ro" \
        -v "${REDIRECTOR_BIN}:/usr/local/bin/redirector:ro" \
        --entrypoint /usr/local/bin/redirector \
        alpine:latest \
        -config /etc/redirector/config.yaml -log-level warn
}

stop_redirector() {
    log_info "Stopping redirector..."

    if [ "$USE_DOCKER" = true ]; then
        docker rm -f redirector-bench 2>/dev/null || true
    else
        if [ -f "${RESULTS_DIR}/.redirector.pid" ]; then
            kill "$(cat "${RESULTS_DIR}/.redirector.pid")" 2>/dev/null || true
            rm -f "${RESULTS_DIR}/.redirector.pid"
        fi
        pkill -f "redirector.*benchmark.yaml" 2>/dev/null || true
    fi
}

run_k6_test() {
    local scenario="$1"
    local profile="${2:-native}"
    local timestamp
    timestamp=$(date +%Y%m%d_%H%M%S)
    local output_file="${RESULTS_DIR}/${scenario}_${profile}_${timestamp}.json"

    log_info "Running scenario: $scenario (profile: $profile)"

    # Run k6 with the specific scenario
    k6 run \
        --out json="$output_file" \
        --env BASE_URL="$BASE_URL" \
        --env SCENARIO="$scenario" \
        --tag profile="$profile" \
        --tag scenario="$scenario" \
        "${PROJECT_ROOT}/test/load/scenarios.js" \
        2>&1 | tee "${RESULTS_DIR}/${scenario}_${profile}_${timestamp}.log"

    local exit_code=${PIPESTATUS[0]}

    if [ $exit_code -eq 0 ]; then
        log_info "Scenario $scenario completed successfully"
    else
        log_warn "Scenario $scenario completed with threshold violations (exit code: $exit_code)"
    fi

    return $exit_code
}

run_basic_test() {
    local profile="${1:-native}"
    local timestamp
    timestamp=$(date +%Y%m%d_%H%M%S)
    local output_file="${RESULTS_DIR}/basic_${profile}_${timestamp}.json"

    log_info "Running basic test (profile: $profile)"

    k6 run \
        --out json="$output_file" \
        --env BASE_URL="$BASE_URL" \
        --tag profile="$profile" \
        "${PROJECT_ROOT}/test/load/basic.js" \
        2>&1 | tee "${RESULTS_DIR}/basic_${profile}_${timestamp}.log"
}

generate_summary() {
    local profile="$1"

    log_info "Generating summary for profile: $profile"

    # Find the most recent results for this profile
    local summary_file="${RESULTS_DIR}/summary_${profile}.md"

    cat > "$summary_file" << EOF
# Performance Summary: ${profile}

Generated: $(date -u +"%Y-%m-%d %H:%M:%S UTC")

## System Information

- Profile: ${profile}
EOF

    if [ "$USE_DOCKER" = true ] && [ "$profile" != "native" ]; then
        cat >> "$summary_file" << EOF
- CPU Limit: ${CPU_LIMITS[$profile]} cores
- Memory Limit: ${MEMORY_LIMITS[$profile]}
EOF
    else
        cat >> "$summary_file" << EOF
- CPU: $(sysctl -n hw.ncpu 2>/dev/null || nproc 2>/dev/null || echo "unknown") cores
- Memory: $(sysctl -n hw.memsize 2>/dev/null | awk '{print $0/1024/1024/1024 " GB"}' || free -h 2>/dev/null | awk '/^Mem:/{print $2}' || echo "unknown")
EOF
    fi

    cat >> "$summary_file" << EOF

## Results

| Scenario | Requests/sec | p95 Latency | p99 Latency | Error Rate |
|----------|-------------|-------------|-------------|------------|
EOF

    # Parse results from log files (simplified - k6 outputs to stdout)
    for log_file in "${RESULTS_DIR}"/*_${profile}_*.log; do
        if [ -f "$log_file" ]; then
            local scenario
            scenario=$(basename "$log_file" | cut -d'_' -f1)

            # Extract metrics from k6 output
            local rps p95 p99 error_rate
            rps=$(grep "http_reqs" "$log_file" | tail -1 | awk '{print $2}' || echo "N/A")
            p95=$(grep "http_req_duration.*p(95)" "$log_file" | awk -F'=' '{print $2}' | awk '{print $1}' || echo "N/A")
            p99=$(grep "http_req_duration.*p(99)" "$log_file" | awk -F'=' '{print $2}' | awk '{print $1}' || echo "N/A")
            error_rate=$(grep "http_req_failed" "$log_file" | awk '{print $2}' || echo "N/A")

            echo "| $scenario | $rps | $p95 | $p99 | $error_rate |" >> "$summary_file"
        fi
    done

    log_info "Summary written to: $summary_file"
}

cleanup() {
    log_info "Cleaning up..."
    stop_redirector
}

usage() {
    cat << EOF
Usage: $(basename "$0") [OPTIONS] [SCENARIOS...]

Options:
    --docker          Run with Docker resource limits
    --profile PROF    Resource profile: minimal, low, medium, high (requires --docker)
    --basic           Run only the basic test
    --all-profiles    Run tests across all resource profiles (requires --docker)
    --help            Show this help message

Scenarios:
    smoke             Quick smoke test (10s, 1 VU)
    load              Sustained load test (3 min, up to 50 VUs)
    stress            Stress test to find breaking point
    spike             Spike test (sudden traffic burst)
    soak              Long-running stability test (10 min)
    exact_only        Test only exact matches (best case)
    regex_only        Test only regex matches (worst case)
    max_throughput    Maximum throughput test

Examples:
    $(basename "$0")                        # Run smoke, load, stress tests
    $(basename "$0") smoke                  # Run only smoke test
    $(basename "$0") --docker --profile low stress
    $(basename "$0") --docker --all-profiles smoke load
EOF
}

main() {
    local profile="native"
    local run_basic=false
    local all_profiles=false
    local scenarios_to_run=()

    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --docker)
                USE_DOCKER=true
                shift
                ;;
            --profile)
                profile="$2"
                shift 2
                ;;
            --basic)
                run_basic=true
                shift
                ;;
            --all-profiles)
                all_profiles=true
                shift
                ;;
            --help|-h)
                usage
                exit 0
                ;;
            *)
                scenarios_to_run+=("$1")
                shift
                ;;
        esac
    done

    # Use default scenarios if none specified
    if [ ${#scenarios_to_run[@]} -eq 0 ]; then
        scenarios_to_run=("${SCENARIOS[@]}")
    fi

    print_header

    # Setup
    trap cleanup EXIT
    mkdir -p "$RESULTS_DIR"
    check_dependencies
    build_redirector

    if [ "$all_profiles" = true ] && [ "$USE_DOCKER" = true ]; then
        # Run across all profiles
        for prof in "${!CPU_LIMITS[@]}"; do
            log_info "━━━ Testing profile: $prof ━━━"
            start_redirector "$prof"

            if [ "$run_basic" = true ]; then
                run_basic_test "$prof"
            else
                for scenario in "${scenarios_to_run[@]}"; do
                    run_k6_test "$scenario" "$prof" || true
                done
            fi

            generate_summary "$prof"
            stop_redirector
            sleep 2
        done
    else
        # Run with single profile
        start_redirector "$profile"

        if [ "$run_basic" = true ]; then
            run_basic_test "$profile"
        else
            for scenario in "${scenarios_to_run[@]}"; do
                run_k6_test "$scenario" "$profile" || true
            done
        fi

        generate_summary "$profile"
    fi

    log_info "Benchmark complete! Results in: $RESULTS_DIR"
}

main "$@"
