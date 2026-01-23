// sustained-load.js - k6 load test for The Redirector
// Run with: k6 run test/load/sustained-load.js

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// Custom metrics
const errorRate = new Rate('errors');
const redirectLatency = new Trend('redirect_latency');
const redirects = new Counter('successful_redirects');

// Test configuration
export const options = {
  stages: [
    { duration: '30s', target: 50 },   // Ramp up to 50 VUs
    { duration: '2m', target: 50 },    // Sustained load
    { duration: '30s', target: 100 },  // Ramp up to 100 VUs
    { duration: '2m', target: 100 },   // Higher sustained load
    { duration: '30s', target: 0 },    // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<100', 'p(99)<200'],  // 95% under 100ms, 99% under 200ms
    errors: ['rate<0.01'],                           // Error rate < 1%
    redirect_latency: ['p(99)<150'],                 // Custom latency metric
  },
};

// Test URLs - mix of different match types
const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

const testPaths = [
  // Exact matches
  '/old-home',
  '/api/v1/legacy',
  '/landing',

  // Prefix matches
  '/blog/hello-world',
  '/blog/2024/01/new-post',
  '/api/v1/users/123',

  // Regex matches
  '/product/12345',
  '/product/67890',
  '/user/johndoe/profile',
  '/user/janedoe/profile',
  '/category/electronics/item/42',

  // Glob matches
  '/docs/v1/guide',
  '/docs/v2/guide',
  '/legacy/deep/nested/path',
];

export default function () {
  // Select a random path
  const path = testPaths[Math.floor(Math.random() * testPaths.length)];
  const url = `${BASE_URL}${path}`;

  // Make request without following redirects
  const res = http.get(url, {
    redirects: 0,  // Don't follow redirects
    tags: { path: path },
  });

  // Check if response is a redirect (3xx)
  const isRedirect = res.status >= 300 && res.status < 400;
  const hasLocation = res.headers['Location'] !== undefined;

  // Validate response
  const passed = check(res, {
    'is redirect': (r) => isRedirect,
    'has Location header': (r) => hasLocation,
    'has X-Redirected-By header': (r) => r.headers['X-Redirected-By'] === 'the-redirector',
    'response time OK': (r) => r.timings.duration < 100,
  });

  // Update custom metrics
  errorRate.add(!passed);
  redirectLatency.add(res.timings.duration);

  if (isRedirect) {
    redirects.add(1);
  }

  // Small sleep to simulate realistic traffic patterns
  sleep(0.05);  // 50ms = ~20 requests per second per VU
}

// Setup function - runs once before the test
export function setup() {
  console.log(`Starting load test against ${__ENV.BASE_URL || 'http://localhost:8080'}`);

  // Verify server is responding
  const res = http.get(`${__ENV.BASE_URL || 'http://localhost:8080'}/old-home`, {
    redirects: 0,
  });

  if (res.status < 300 || res.status >= 400) {
    console.warn(`Warning: Initial request returned ${res.status}, expected 3xx`);
  }

  return { startTime: new Date().toISOString() };
}

// Teardown function - runs once after the test
export function teardown(data) {
  console.log(`Load test completed. Started at: ${data.startTime}`);
}
