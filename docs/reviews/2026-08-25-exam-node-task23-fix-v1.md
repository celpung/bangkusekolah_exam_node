# Task 23 Blocker Fix — `bangkusekolah_exam_node`

**Repository:** `bangkusekolah_exam_node`
**Branch:** `kevin`
**Base reviewed:** `67586ac`
**Status:** Local fixes implemented and verified; not committed or pushed.

## Fixed blockers

### HTTP flow now matches the k6 scenario

Added and wired:

```text
POST /api/v1/auth/exam-login
POST /api/v1/student/exams/{examId}/attempts
```

The first endpoint uses `AuthService.Login` to issue the node JWT. The second endpoint uses the JWT participant identity and verifies that the path exam matches the JWT exam before calling `AttemptService.StartAttempt`.

Existing k6 paths were corrected to the node router contract:

```text
PUT  /api/v1/student/exam-attempts/{attemptId}/answers/{itemId}
POST /api/v1/student/exam-attempts/{attemptId}/submit
```

The script now reads the response envelope correctly:

```text
login: data.token
start: data.ID
```

### k6 participant mapping fixed

The script now maps:

```text
VU 1    -> AAAAAA-000001
VU 1000 -> AAAAAA-001000
```

It performs only the first iteration for each VU, preventing duplicate `MaxAttempts=1` starts.

### Go load test database safety fixed

`tests/load/student_burst_test.go` now:

- requires `TEST_DB_DSN` explicitly;
- sets the test DSN as the node config DSN without printing it;
- verifies `SELECT DATABASE()` and requires a name ending in `_test`;
- runs embedded node migrations before seeding;
- cleans synthetic rows before and after the run with checked errors;
- checks submitted-attempt, attempt, answer, and duplicate-identity counts;
- preserves database errors when locating active attempts;
- uses the correct `answer` key in the synthetic objective item.

The wrapper now:

- provides `--help`/`-h` without side effects;
- fails with exit 2 if `TEST_DB_DSN` is absent;
- no longer treats a missing DB as a successful skipped load gate.

## Tests added

`app/adapter/delivery/handler/auth_start_handler_test.go` verifies:

```text
login returns token envelope
start uses JWT participant identity
foreign exam path is rejected
```

## Verification

```text
go test ./...                         PASS
go vet ./...                          PASS
go build ./...                        PASS
go test -race ./...                  PASS
go test -tags=load ./... -run '^$'   PASS (compile-only)
gofmt -d modified Go files          PASS
bash -n scripts/loadtest.sh          PASS
node --check scripts/loadtest.js     PASS
git diff --check                    PASS
```

Explicit safety behavior:

```text
./scripts/loadtest.sh --help          PASS
./scripts/loadtest.sh go (no DSN)     exit 2, explicit failure
```

## Evidence not available in this environment

```text
actual 1000-student MySQL burst: not run — no TEST_DB_DSN
actual k6 HTTP run:             not run — k6 unavailable
Docker image/runtime smoke:      not run — Docker daemon/Compose unavailable
```

No load result or p99 pass is claimed without those environments.

## Changed files

```text
app/adapter/delivery/handler/auth_handler.go                 added
app/adapter/delivery/handler/auth_start_handler_test.go      added
app/adapter/delivery/handler/attempt_handler.go              modified
app/adapter/delivery/router/router.go                        modified
cmd/examnode/main.go                                         modified
scripts/loadtest.js                                           modified
scripts/loadtest.sh                                           modified
tests/load/student_burst_test.go                             modified
```

## Scope

Only the Task 23 blocking paths were fixed. The changes remain local on `kevin`; no commit, push, merge, PR, checkout, or other branch modification was performed.
