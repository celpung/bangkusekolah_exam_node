# Review Phase B Task 23 — `bangkusekolah_exam_node`

**Branch:** `kevin`
**HEAD:** `67586ac`
**Live remote:** `origin/kevin` = `67586ac`
**Reviewer:** Ned / Senior Programmer
**Verdict:** **REQUEST CHANGES**

## Summary

The Go load-test package, k6 file, and wrapper are present and the load-tag package compiles. The claimed load evidence is not yet valid: the Go test is a direct service/repository test that can silently skip and exit 0, has no test-database safety/cleanup, and does not measure request p99; the k6 script targets endpoints absent from the current node router and generates codes that do not correspond to the 1000 seeded participants. Docker/runtime execution and an actual 1000-student run were not independently verified.

## Blocking findings

### BLOCKER-1 — k6 scenario cannot exercise the current node HTTP surface

**Locations:**

```text
scripts/loadtest.js:35-65
app/adapter/delivery/router/router.go:31-47
```

The k6 script calls:

```text
POST /api/v1/auth/exam-login
POST /api/v1/student/exams/{examId}/attempts
PUT  /api/v1/student/attempts/{attemptId}/items/{itemId}
POST /api/v1/student/attempts/{attemptId}/submit
```

The current node router registers neither the auth/start endpoints nor those attempt paths. It currently exposes content and:

```text
GET  /api/v1/student/exam-attempts/{attemptId}
PUT  /api/v1/student/exam-attempts/{attemptId}/answers/{itemId}
POST /api/v1/student/exam-attempts/{attemptId}/submit
```

`/api/v1/auth/exam-login` and `POST /api/v1/student/exams/{examId}/attempts` are not wired. The k6 test therefore cannot produce the claimed successful login/start/autosave/submit flow against the committed node binary.

### Required fix

Align the k6 script with the actual approved HTTP contract and wire missing endpoints if they are required. Add a route-level smoke test that runs the exact URLs used by k6 against the assembled router and verifies login, start, 40 autosaves, and submit.

### BLOCKER-2 — k6 access-code generation is invalid for the seeded 1000 participants

**Location:**

```text
scripts/loadtest.js:28-33
```

The Go fixture creates:

```text
AAAAAA-000001 ... AAAAAA-001000
```

The k6 formula is:

```javascript
AAAAAA-${String(__VU * 1000 + __ITER).padStart(6, '0')}
```

Observed values include:

```text
VU 1, iteration 0    -> AAAAAA-001000
VU 2, iteration 0    -> AAAAAA-002000  (not seeded)
VU 999, iteration 0  -> AAAAAA-999000  (not seeded)
VU 1000, iteration 0 -> AAAAAA-1000000 (7 digits, invalid shape)
```

The comments mention CSV support, but the script never reads `CODE_CSV`. In addition, the staged VU function can run multiple iterations; subsequent iterations can exceed the participant range and `MaxAttempts: 1` makes repeated attempts invalid.

### Required fix

Use a real participant-code data source (for example k6 `SharedArray` + CSV) and run exactly one iteration per seeded participant, or deterministically map VU 1..1000 to suffix 000001..001000 and prevent extra iterations. Add a preflight check that every generated code exists in the test fixture.

### BLOCKER-3 — Go burst test can mutate an arbitrary database and leaves data behind

**Location:**

```text
tests/load/student_burst_test.go:27-50
```

The test calls `config.Load()` and `provider.Connect(cfg)` but does not:

```text
validate the server-reported database name ends with _test
require TEST_DB_DSN explicitly
run migrations on a fresh test database
clean up the synthetic exam/participants/attempts/answers afterward
```

It then calls `bundleSvc.LoadBundle`, followed by 1000 starts, 40,000 autosaves, and 1000 submits. A misconfigured `DB_DSN` can therefore receive destructive/large test writes. The wrapper claims `TEST_DB_DSN or DB_*`, but this test does not read `TEST_DB_DSN`; `config.Load()` reads only `DB_DSN`.

### Required fix

Use a dedicated load-test setup that:

- requires an explicit test DSN or derives one with an enforced `_test` suffix;
- verifies `SELECT DATABASE()` from the server;
- runs migrations before `LoadBundle`;
- cleans all synthetic data/database in a checked-error teardown;
- refuses production/staging names;
- does not print credentials.

## HIGH findings

### HIGH-4 — Default wrapper/test silently reports a skipped load test as success

**Locations:**

```text
tests/load/student_burst_test.go:27-37
scripts/loadtest.sh:11-16
```

With no configuration, the exact command succeeds as:

```text
--- SKIP: TestBurst (no node config: DB_DSN is required)
PASS
wrapper_go_exit=0
```

This is acceptable only for an explicitly optional developer test, not as evidence for the final checkpoint. The wrapper must distinguish `SKIPPED` from `PASS`, and CI/checkpoint automation must fail or clearly mark the load gate unverified when no real test database was used.

### HIGH-5 — Go burst test does not measure p99 latency or HTTP behavior

**Locations:**

```text
tests/load/student_burst_test.go:63-156
```

The test calls `AttemptService` directly, not the HTTP server. It records phase elapsed time and only checks total duration `<90s`; it does not record per-request latency or compute p99. A total under 90 seconds does not imply p99 `<500ms`.

The k6 test is the only HTTP/p99 candidate, but it is currently unrunnable against the node routes and was not executed. Report the checkpoint as load evidence missing until a real HTTP run produces:

```text
http_req_duration p99 < 500ms
http_req_failed rate < 1%
HTTP endpoint success assertions
```

### HIGH-6 — Load model does not represent the documented 10-second autosave cadence

**Location:**

```text
tests/load/student_burst_test.go:87-118
```

All 1000 goroutines perform 40 autosaves as fast as possible. There is no pacing, so the test creates a 40,000-request burst rather than the documented approximately 70 rps steady-state traffic. This can be a useful stress spike, but it must be labelled separately and complemented by a paced scenario.

### HIGH-7 — Errors are ignored while locating active attempts

**Locations:**

```text
tests/load/student_burst_test.go:96-100
 tests/load/student_burst_test.go:130-134
```

Both autosave and submit phases use:

```go
att, _ := repo.FindActiveAttemptByParticipant(...)
```

A database error is converted into `att == nil` and a generic counter increment. Preserve/log the underlying error in a bounded way and fail the test with the actual database failure category.

### HIGH-8 — No cleanup means repeated runs are not isolated/idempotent

**Location:**

```text
tests/load/student_burst_test.go:42-50
```

The test uses stable IDs (`exam-burst`, `part-0001`, etc.) but never deletes the generated rows. A second run depends on `LoadBundle` replacement behavior and leaves attempts/answers behind. Add a checked teardown and verify row counts before/after.

### HIGH-9 — Final checkpoint claims are not independently demonstrated

**Location:**

```text
commit message for 67586ac
```

The commit claims both repositories' build/vet/test gates and a checkpoint, but this commit changes only the node repository. In this review I independently ran the node gates, checksum, load-tag compile, wrapper, and syntax checks. I did not claim a fresh service checkpoint run, a real load run, or a Docker runtime run.

## Medium findings

- `scripts/loadtest.sh` has no `--help` path and its documented `k6 staging` form passes the literal string `staging` as `BASE_URL` rather than a URL.
- The Go load test's `Checksum: "burst-test-checksum"` is a synthetic value; ensure `LoadBundle` either accepts this intentionally or compute the real checksum to cover the actual bundle integrity contract.
- The Go test uses `time.Now()` for the bundle window and does not pin/clean the clock-dependent state, which can make reruns near boundary times less deterministic.
- The load test does not assert final database invariants (1000 graded/submitted attempts, 40,000 answers, no duplicate attempts, no orphan rows) after the burst.

## Verified

### Live source

```text
local HEAD:   67586ac
origin/kevin: 67586ac
branch:       kevin
worktree:     clean before this review report
```

### Normal Go gates

```text
go build ./...        PASS
go vet ./...          PASS
go test ./...         PASS
go test -race ./...  PASS
git diff --check     PASS
```

### Load-tag gate

```text
go test -tags=load ./... -run '^$' -count=0  PASS (compile-only)
go test -tags=load ./tests/load -run '^TestBurst$' with empty env  PASS as SKIP
```

The second result is not a load-test pass; it skipped because `DB_DSN` was absent.

### Wrapper/syntax checks

```text
bash -n scripts/loadtest.sh  PASS
node --check scripts/loadtest.js  PASS
```

`k6` is not installed in this environment, so the wrapper's k6 path returned:

```text
k6: command not found
exit 127
```

### Shared grading fixture

```text
node vectors.json checksum:    45d8661588390c4c7eb3d2146c4b3a7bbbb5527277d29a0e7fce48cab7dd1ad4
service vectors.json checksum: 45d8661588390c4c7eb3d2146c4b3a7bbbb5527277d29a0e7fce48cab7dd1ad4
```

### Runtime evidence gap

```text
docker compose: unavailable
docker daemon:  unavailable
actual 1000-student run: not executed
actual k6 HTTP run:       not executed
```

## Scope

This review covers Task 23 commit `67586ac` in `bangkusekolah_exam_node` branch `kevin`. It does not approve the previously documented Task 22 runtime endpoint gaps. No source, branch, commit, PR, or remote ref was changed; only this review report was written.

## Final verdict

```text
Task 23: REQUEST CHANGES
```

The load-test code is present and compiles, but the k6 path cannot hit the current node API, its generated credentials do not represent the seeded students, and the Go path can silently skip while mutating an unguarded database. Fix the endpoint/data mapping, enforce disposable DB isolation/cleanup, and provide a real HTTP/load execution before calling the Phase B checkpoint complete.
