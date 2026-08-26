# AGENTS.md — Bangkusekolah Exam Node

## 1. Scope

This repository runs the student sitting flow and nothing else (design decision 1).
It serves `Login`, `ListStudentExams`, `StartAttempt`, `GetAttempt`, `AutosaveAnswer`,
`SubmitAttempt`, `RecordIntegrityEvent`, `GetStudentResult`. It does NOT serve
authoring, question bank, participant management, teacher grading, analytics,
gradebook, notifications, or audit log.

## 2. Writing Code Rules

Copy verbatim from `bangkusekolah_service/AGENTS.md` §2:

1. DRY — reuse before duplicating. DRY is the highest-priority rule.
2. Single Responsibility — one clear responsibility per function.
3. KISS — simplest solution that correctly solves the problem.
4. Clear Naming — names communicate purpose without comments.
5. YAGNI — do not implement what is not currently required.
6. Meaningful Errors — clear, specific, handled appropriately.
7. Avoid Magic Values — named constants, not unexplained literals.
8. Comments — only where non-obvious, especially why.

### The one sanctioned DRY exception

`app/domain/grading` — `GradeObjectiveAnswer` and its four helpers
`normalizedScalar`, `stringSlice`, `sameOrderedOrSet`, `normalizedMap` are copied
from `bangkusekolah_service/app/service/exam_service.go` and pinned by
a byte-identical `testdata/grading/vectors.json` asserted in both repositories.
The exception is scoped to those five functions and nothing else.

## 3. Architecture

Hexagonal, mirroring central but smaller (~3000–4000 lines):

```
cmd/examnode/        HTTP server
cmd/bundleload/      pull + load bundle from central
cmd/preflight/       go/no-go checklist
cmd/examharvest/     forced push (also runs in-process on a ticker)
app/domain/entity/         runtime entities only
app/domain/grading/        ← the copied kernel (decision 3)
app/domain/error/
app/port/{inbound,outbound}
app/service/               attempt_service, sweeper_service, harvest_service, bundle_service
app/adapter/delivery/{handler,router,middleware,dto}
app/adapter/persistence/{model,mapper,repository}
app/adapter/central/       client to central
app/adapter/security/      JWT, node-local signing key, ID generator
testdata/grading/vectors.json   ← byte-identical to central's copy
migrations/  docker-compose.yml  AGENTS.md  docs/runbook.md
```

Domain never imports adapters. Services never import `app/adapter/...`.
Repositories never return GORM models. Handlers never query the database.
Multi-step writes go through `TxManager.Atomic`. Every repository method calls
`repo_helper.GetDB(ctx, r.db)` so transactions propagate.

## 4. Verification

After every task:

```bash
go test ./... && go vet ./... && go build ./...
gofmt -w <modified-files>
```

The shared fixture checksum must be identical in both repos:

```bash
sha256sum ../bangkusekolah_service/testdata/grading/vectors.json testdata/grading/vectors.json
```

## 5. Key decisions

- Scope is the sitting flow only (decision 1) — every table is single-writer.
- Thin node with its own 7-table schema, no FKs, no soft deletes (decision 2).
- Grading kernel is copied, pinned by golden vectors (decision 3).
- Central re-derives every score (decision 4) — node's scores are inputs, never authority.
- Node UUIDs adopted verbatim, no mapping table (decision 5).
- Delegation is a nullable FK, not a new ExamStatus (decision 8).
- Access codes `<EXAM6>-<PART6>` in Crockford base32 (decision 9).
- Mixed exams: essays return manual_required with subtotal (decision 10).
- file_upload rejected at deploy (decision 11) — also defense-in-depth in bundle load.
- 5-minute harvest, no warm standby (decision 12).
- Node registration is admin-only (decision 13).
- No server-side shuffling (decision 14) — store and pass through.
- Content and answers are separate endpoints, ETag + gzip (decision 15).
