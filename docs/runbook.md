# Exam Node Runbook

## D-3 Provision

1. Provision VPS from snapshot or `scripts/provision.sh` (never by hand).
2. `docker compose up -d --wait` — wait for mysql healthy.
3. Register node in central: `POST /api/v1/exam-nodes` (admin) → save `token` once.
4. Set `CENTRAL_NODE_TOKEN` and `DEPLOYMENT_ID` in `.env`.
5. Verify NTP: `timedatectl status` or `ntpq -p` — offset < 2s, else `preflight` fails.

## D-1 Deploy

1. SuperAdmin/Admin selects multiple exams and deploys them to a node in central: `POST /api/v1/exams/{id}/node-deployment` per exam (loop in `DeployExams`, Task 6) or a bulk `DeployExams` call. Mapping exam→VPS is displayed in the dashboard.
2. On node: `go run ./cmd/bundleload --pull` pulls N bundles sequentially (one per deployment), verifies each checksum, and refreshes the running `examnode` cache when the process is already up. If `examnode` is stopped, the next startup rehydrates the same database snapshot.
3. `go run ./cmd/preflight` — must print `PASS` per bundle. If any `FAIL`, fix and rerun. Do not proceed to D-0 with a `FAIL`.
4. Verify: `curl -sf http://127.0.0.1:8080/livez` → 200, `curl -sf http://127.0.0.1:8080/readyz` → 200.
5. Access codes are paperless — handshake is automatic via `ListStudentExams` enrichment (Task 9). `GET /api/v1/exams/{id}/node-deployment/access-codes` remains for administrative/diagnostic use only.

## D-0 Exam day

- Students log in with `<EXAM6>-<PART6>` codes against the node (`POST /api/v1/auth/exam-login`).
- Harvest worker drains every 5m. Monitor: `docker compose logs -f examnode | grep harvest`.
- Sweeper finalizes abandoned attempts every 60s. Monitor: `grep sweeper`.

## If the node is unreachable mid-exam

1. **Do not destroy the box.** The 5-minute harvest means at most 5 minutes of submitted work is on the node but not yet in central. Binlog may still be recoverable.
2. In central, unseal the deployment so students can resume centrally:
   ```sql
   -- Find the deployment
   SELECT id, exam_id, status, reported_attempt_count FROM exam_node_deployments WHERE exam_id = '...';
   -- Clear delegation so StartAttempt/Autosave stop returning ErrExamDelegatedToNode
   UPDATE exams SET exam_node_deployment_id = NULL WHERE id = '...';
   -- Mark deployment aborted so harvest stops pushing
   UPDATE exam_node_deployments SET status = 'aborted' WHERE id = '...';
   ```
   After this, the exam is `published` and centrally writable again. Students resume on central — they lose at most 5m of autosave, not the whole session.
3. Re-point DNS / client `base_url` to central. The student's `GetAttempt` is the same path; only the host changes.
4. Keep the node VM running until `SELECT COUNT(*) FROM exam_attempts WHERE exam_id='...' AND harvested_at IS NULL` is 0 and every `harvest_log` row is acked. Then destroy.

## If harvest is failing

```bash
go run ./cmd/examharvest --force
# or
curl -X POST http://127.0.0.1:8080/internal/v1/harvest/force -H "Authorization: Bearer $CENTRAL_NODE_TOKEN"
```

Check central logs for `POST /api/v1/exam-nodes/deployments/{id}/attempts` — each attempt is acked individually, so one bad attempt does not cost the batch. Retrying is safe (upsert by primary key, decision 5).

## If seal reports missing attempts

Do not destroy the box. Investigate:

```sql
-- On node
SELECT COUNT(*) FROM attempts WHERE status IN ('submitted','auto_submitted');
SELECT COUNT(*) FROM attempts WHERE harvested_at IS NULL;
SELECT * FROM harvest_log ORDER BY pushed_at DESC LIMIT 20;

-- In central
SELECT COUNT(*) FROM exam_attempts WHERE exam_id = '...';
SELECT id, status FROM exam_attempts WHERE exam_id = '...' ORDER BY created_at DESC LIMIT 20;
```

If `node.attempts.harvested_at IS NULL` count equals `central.ReportedAttemptCount - CountFinishedAttemptsByExam`, a harvest batch was lost — rerun `examharvest --force`. If counts match but seal still refuses, check `exam_attempts.status = 'in_progress'` — an abandoned attempt may still be live; the sweeper will auto-submit it on the next tick.

## After seal

1. Teacher seals in central: `POST /api/v1/exams/{id}/node-deployment/seal` — must return 200. If it returns `ErrExamNodeNotSealable`, harvest is incomplete; do not retry seal, fix harvest first.
2. Verify: `GET /api/v1/exams/{id}/node-deployment` → `status: sealed`, `sealed_at` set, `exams.exam_node_deployment_id` is `NULL`.
3. Teacher grades essays, publishes results, syncs gradebook — all centrally, unchanged.
4. D+1: destroy the box. The bundle's answer keys are then gone.
