// k6 load test for bangkusekolah_exam_node
// Run: k6 run --env BASE_URL=http://127.0.0.1:8080 --env EXAM_ID=exam-1 scripts/loadtest.js
//
// This script hits the real HTTP server (cmd/examnode) so it also proves
// the middleware.Throttle(400) + SetMaxOpenConns(50) combination under
// real TCP. For service-layer burst testing, use:
//   go test -tags=load -run TestBurst -count=1 -v

import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '60s', target: 1000 },  // ramp up to 1000 VUs
    { duration: '90s', target: 1000 },  // hold at 1000
    { duration: '10s', target: 0 },     // ramp down
  ],
  thresholds: {
    http_req_duration: ['p(99)<500'],    // p99 < 500ms
    http_req_failed: ['rate<0.01'],      // < 1% errors
  },
};

export default function () {
  const baseUrl = __ENV.BASE_URL || 'http://127.0.0.1:8080';
  const examId = __ENV.EXAM_ID || 'exam-1';

  // Each VU gets a unique access code from a CSV or generated.
  // For CSV-based: k6 run --env CODE_CSV=participants.csv
  // For generated: use VU ID to construct code.
  const vuId = __VU;
  const iterId = __ITER;
  const code = `AAAAAA-${String(vuId * 1000 + iterId).padStart(6, '0')}`;

  // 1. Login
  const loginRes = http.post(`${baseUrl}/api/v1/auth/exam-login`,
    JSON.stringify({ code }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  check(loginRes, { 'login 200': (r) => r.status === 200 });
  if (loginRes.status !== 200) return;

  const token = loginRes.json('token');
  const authHeaders = { headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' } };

  // 2. Start attempt
  const startRes = http.post(`${baseUrl}/api/v1/student/exams/${examId}/attempts`, null, authHeaders);
  check(startRes, { 'start 200': (r) => r.status === 200 });
  if (startRes.status !== 200) return;

  const attemptId = startRes.json('id');

  // 3. Autosave 40 items
  for (let i = 0; i < 40; i++) {
    const itemId = `item-${String(i + 1).padStart(3, '0')}`;
    const saveRes = http.put(`${baseUrl}/api/v1/student/attempts/${attemptId}/items/${itemId}`,
      JSON.stringify({ answer: 'A', client_seq: i + 1 }),
      authHeaders
    );
    check(saveRes, { 'autosave 200': (r) => r.status === 200 });
  }

  // 4. Submit
  const submitRes = http.post(`${baseUrl}/api/v1/student/attempts/${attemptId}/submit`, null, authHeaders);
  check(submitRes, { 'submit 200': (r) => r.status === 200 });

  sleep(1);
}
