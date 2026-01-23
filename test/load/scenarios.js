/**
 * The Redirector - Load Test Scenarios
 *
 * Run with: k6 run test/load/scenarios.js
 *
 * Environment variables:
 *   - BASE_URL: Target URL (default: http://localhost:8080)
 *   - SCENARIO: Which scenario to run (default: mixed)
 */

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// Custom metrics
const redirectSuccess = new Rate('redirect_success');
const redirectLatency = new Trend('redirect_latency', true);
const requestsPerRule = new Counter('requests_per_rule');

// Configuration
const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const SCENARIO = __ENV.SCENARIO || 'mixed';

// Test data for different match types
const exactPaths = [
  '/exact/path/one',
  '/exact/path/two',
  '/exact/path/three',
  '/api/v1/users',
  '/api/v1/products',
];

const prefixPaths = [
  '/blog/hello-world',
  '/blog/2024/post-title',
  '/docs/v1/getting-started',
  '/docs/v2/api-reference',
  '/api/legacy/users/123',
  '/old-site/about',
];

const regexPaths = [
  '/product/12345',
  '/product/99999',
  '/user/johndoe/profile',
  '/user/test-user/profile',
  '/category/electronics/item/42',
  '/article/2024/01/hello-world',
  '/download/a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4',
];

const globPaths = [
  '/assets/js/app.js',
  '/assets/css/style.css',
  '/assets/images/logo.png',
  '/images/photo.jpg',
  '/static/v1/bundle.js',
];

const blockPaths = [
  '/wp-admin',
  '/wp-admin/index.php',
  '/.env',
];

const notFoundPaths = [
  '/random/nonexistent/path',
  '/this-does-not-exist',
];

// Scenario configurations
export const options = {
  scenarios: {
    // Quick smoke test
    smoke: {
      executor: 'constant-vus',
      vus: 1,
      duration: '10s',
      exec: 'mixedWorkload',
      startTime: '0s',
    },

    // Sustained load test
    load: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 50 },   // Ramp up
        { duration: '2m', target: 50 },    // Steady state
        { duration: '30s', target: 0 },    // Ramp down
      ],
      exec: 'mixedWorkload',
      startTime: '0s',
    },

    // Stress test - find breaking point
    stress: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '1m', target: 100 },
        { duration: '1m', target: 200 },
        { duration: '1m', target: 300 },
        { duration: '1m', target: 400 },
        { duration: '2m', target: 500 },
        { duration: '1m', target: 0 },
      ],
      exec: 'mixedWorkload',
      startTime: '0s',
    },

    // Spike test
    spike: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '10s', target: 10 },   // Normal load
        { duration: '5s', target: 500 },   // Spike!
        { duration: '30s', target: 500 },  // Stay at spike
        { duration: '10s', target: 10 },   // Recovery
        { duration: '30s', target: 10 },   // Steady
      ],
      exec: 'mixedWorkload',
      startTime: '0s',
    },

    // Soak test - long duration
    soak: {
      executor: 'constant-vus',
      vus: 50,
      duration: '10m',
      exec: 'mixedWorkload',
      startTime: '0s',
    },

    // Exact match only (best case)
    exact_only: {
      executor: 'constant-rate',
      rate: 1000,
      timeUnit: '1s',
      duration: '1m',
      preAllocatedVUs: 50,
      exec: 'exactMatchOnly',
      startTime: '0s',
    },

    // Regex match only (worst case)
    regex_only: {
      executor: 'constant-rate',
      rate: 1000,
      timeUnit: '1s',
      duration: '1m',
      preAllocatedVUs: 50,
      exec: 'regexMatchOnly',
      startTime: '0s',
    },

    // Max throughput test
    max_throughput: {
      executor: 'constant-rate',
      rate: 10000,
      timeUnit: '1s',
      duration: '1m',
      preAllocatedVUs: 200,
      maxVUs: 500,
      exec: 'mixedWorkload',
      startTime: '0s',
    },
  },

  thresholds: {
    http_req_duration: ['p(95)<100', 'p(99)<500'],  // 95% < 100ms, 99% < 500ms
    http_req_failed: ['rate<0.01'],                  // Less than 1% errors
    redirect_success: ['rate>0.95'],                 // 95% successful redirects
  },
};

// Select which scenario to run based on environment variable
export function setup() {
  console.log(`Running scenario: ${SCENARIO}`);
  console.log(`Target: ${BASE_URL}`);

  // Verify server is up
  const res = http.get(`${BASE_URL.replace(':8080', ':8081')}/health`);
  if (res.status !== 200) {
    throw new Error(`Server health check failed: ${res.status}`);
  }

  return { scenario: SCENARIO };
}

// Helper function to make request and record metrics
function makeRequest(path, expectedStatus = 301) {
  const url = `${BASE_URL}${path}`;
  const start = Date.now();

  const res = http.get(url, {
    redirects: 0,  // Don't follow redirects
    tags: { path_type: getPathType(path) },
  });

  const duration = Date.now() - start;
  redirectLatency.add(duration);

  const success = res.status === expectedStatus ||
                  (expectedStatus === 301 && res.status >= 300 && res.status < 400);

  redirectSuccess.add(success);

  check(res, {
    'status is redirect or expected': (r) => success,
    'has location header': (r) => r.status < 400 ? r.headers['Location'] !== undefined : true,
    'has rule id header': (r) => r.headers['X-Rule-Id'] !== undefined || r.headers['X-Rule-ID'] !== undefined,
  });

  return res;
}

function getPathType(path) {
  if (exactPaths.includes(path)) return 'exact';
  if (prefixPaths.some(p => path.startsWith(p.split('/').slice(0, 3).join('/')))) return 'prefix';
  if (path.match(/\/product\/\d+/) || path.match(/\/user\/[^/]+\/profile/)) return 'regex';
  if (path.startsWith('/assets/') || path.startsWith('/images/') || path.startsWith('/static/')) return 'glob';
  if (path.startsWith('/wp-admin') || path === '/.env') return 'block';
  return 'other';
}

function randomChoice(arr) {
  return arr[Math.floor(Math.random() * arr.length)];
}

// Workload functions
export function mixedWorkload() {
  // Weighted distribution: 40% exact, 30% prefix, 15% regex, 10% glob, 5% block
  const rand = Math.random();

  if (rand < 0.40) {
    makeRequest(randomChoice(exactPaths));
  } else if (rand < 0.70) {
    makeRequest(randomChoice(prefixPaths));
  } else if (rand < 0.85) {
    makeRequest(randomChoice(regexPaths));
  } else if (rand < 0.95) {
    makeRequest(randomChoice(globPaths));
  } else {
    makeRequest(randomChoice(blockPaths), 404);
  }

  sleep(0.01); // Small sleep to prevent overwhelming
}

export function exactMatchOnly() {
  makeRequest(randomChoice(exactPaths));
}

export function regexMatchOnly() {
  makeRequest(randomChoice(regexPaths));
}

export function prefixMatchOnly() {
  makeRequest(randomChoice(prefixPaths));
}

export function globMatchOnly() {
  makeRequest(randomChoice(globPaths));
}

// Default function (used when no scenario specified)
export default function() {
  mixedWorkload();
}

// Teardown - print summary
export function teardown(data) {
  console.log(`\nTest completed for scenario: ${data.scenario}`);
}
