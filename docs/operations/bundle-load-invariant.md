# Operational Deployment Invariant — Bundle Loading & Cache Publication

## The multi-process boundary

`LockExam` and the generation-token publication state live **inside one
`ContentService` process**. They coordinate concurrent writers *within* that
process only. They do NOT coordinate:

- a separate `cmd/bundleload` process against a running `examnode`, or
- two `bundleload` invocations targeting the same node database.

## Enforced invariant

```text
cmd/bundleload runs BEFORE examnode starts and NEVER concurrently with it.
```

- `examnode` rehydrates its content cache from persisted rows at startup, so
  bundles loaded by `bundleload` while the node was down are picked up on
  start — no live push needed.
- While `examnode` is serving students, bundle updates go exclusively through
  `POST /internal/v1/bundle` (central → node), where the per-exam lock,
  generation tokens, readiness gating, and rollback protocol apply in full.

## Why this is safe

| Scenario | Protection |
|---|---|
| `bundleload` while `examnode` is down | Safe: no live cache to invalidate; startup rehydrates from DB |
| `bundleload` while `examnode` is up | FORBIDDEN: `examnode`'s in-memory cache would go stale with no readiness gate to catch it |
| Concurrent internal pushes for one exam | Serialized by `LockExam`; generation tokens prevent stale publication |
| Failed load | Transaction rolls back; readiness stays closed until a successful retry |

## If live external loads are ever required

Before lifting the invariant, add cross-process coordination:

1. MySQL advisory lock (`GET_LOCK`) shared by both binaries around load +
   publish; and
2. a cache-invalidation notification to the running node (poll of the exam's
   `content_hash`, or an internal invalidate endpoint); or move all loading
   behind the running node's internal API.
