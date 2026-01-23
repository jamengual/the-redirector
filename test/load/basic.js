/**
 * The Redirector - Basic Load Test
 *
 * Simple load test for quick performance checks.
 * Run with: k6 run test/load/basic.js
 *
 * Options:
 *   k6 run --vus 10 --duration 30s test/load/basic.js
 *   k6 run --env BASE_URL=http://redirector:8080 test/load/basic.js
 */

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Rate } from 'k6/metrics';

const latency = new Trend('redirect_latency', true);
const successRate = new Rate('success_rate');

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export const options = {
  vus: 10,
  duration: '30s',
  thresholds: {
    http_req_duration: ['p(95)<50', 'p(99)<100'],
    http_req_failed: ['rate<0.01'],
    success_rate: ['rate>0.99'],
  },
};

const testPaths = [
  '/exact/path/one',
  '/exact/path/two',
  '/blog/test-post',
  '/docs/v1/guide',
  '/product/12345',
  '/assets/js/app.js',
];

export default function() {
  const path = testPaths[Math.floor(Math.random() * testPaths.length)];
  const url = `${BASE_URL}${path}`;

  const res = http.get(url, { redirects: 0 });

  latency.add(res.timings.duration);

  const success = check(res, {
    'status is 3xx': (r) => r.status >= 300 && r.status < 400,
    'has location': (r) => r.headers['Location'] !== undefined,
  });

  successRate.add(success);

  sleep(0.01);
}
