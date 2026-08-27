// k6 load test for bangkusekolah_exam_node
// Run: k6 run --env BASE_URL=http://127.0.0.1:8080 --env EXAM_ID=exam-burst scripts/loadtest.js
//
// Each VU maps to exactly one seeded participant code (VU 1 -> 000001,
// VU 1000 -> 001000). The first iteration performs the complete student flow;
// later iterations are idle so MaxAttempts=1 is not violated.

import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '60s', target: 1000 },
    { duration: '90s', target: 1000 },
    { duration: '10s', target: 0 },
  ],
  thresholds: {
    http_req_duration: ['p(99)<500'],
    http_req_failed: ['rate<0.01'],
  },
};

export default function () {
  // The seeded fixture has MaxAttempts=1. Do not start another attempt when
  // k6 schedules a second iteration on an already-created VU.
  if (__ITER > 0) {
    sleep(1);
    return;
  }

  const baseUrl = __ENV.BASE_URL || 'http://127.0.0.1:8080';
  const examId = __ENV.EXAM_ID || 'exam-burst';
  const examPrefix = __ENV.EXAM_PREFIX || 'AAAAAA';
  const code = `${examPrefix}-${String(__VU).padStart(6, '0')}`;

  const loginRes = http.post(
    `${baseUrl}/api/v1/auth/exam-login`,
    JSON.stringify({ code }),
    { headers: { 'Content-Type': 'application/json' } },
  );
  check(loginRes, {
    'login 200': (r) => r.status === 200,
    'login token present': (r) => Boolean(r.json('data.token')),
  });
  if (loginRes.status !== 200) return;

  const token = loginRes.json('data.token');
  const authHeaders = {
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
  };

  const startRes = http.post(
    `${baseUrl}/api/v1/student/exams/${examId}/attempts`,
    null,
    authHeaders,
  );
  check(startRes, {
    'start 200': (r) => r.status === 200,
    'start attempt present': (r) => Boolean(r.json('data.ID')),
  });
  if (startRes.status !== 200) return;

  const attemptId = startRes.json('data.ID');

  for (let i = 0; i < 40; i++) {
    const itemId = `item-${String(i + 1).padStart(3, '0')}`;
    const saveRes = http.put(
      `${baseUrl}/api/v1/student/exam-attempts/${attemptId}/answers/${itemId}`,
      JSON.stringify({ answer_json: { answer: 'A' }, client_seq: i + 1 }),
      authHeaders,
    );
    check(saveRes, { 'autosave 200': (r) => r.status === 200 });
  }

  const submitRes = http.post(
    `${baseUrl}/api/v1/student/exam-attempts/${attemptId}/submit`,
    null,
    authHeaders,
  );
  check(submitRes, { 'submit 200': (r) => r.status === 200 });

  sleep(1);
}
