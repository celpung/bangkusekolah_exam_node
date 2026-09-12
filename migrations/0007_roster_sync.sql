-- Durable incremental roster state. This migration is additive and must not
-- remove existing participants, attempts, answers, or integrity events.

SET @has_exams_roster_revision := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'exams' AND COLUMN_NAME = 'roster_revision'
);
SET @add_exams_roster_revision := IF(
  @has_exams_roster_revision = 0,
  'ALTER TABLE exams ADD COLUMN roster_revision BIGINT NOT NULL DEFAULT 0 AFTER content_hash',
  'SELECT 1'
);
PREPARE stmt_exams_roster_revision FROM @add_exams_roster_revision;
EXECUTE stmt_exams_roster_revision;
DEALLOCATE PREPARE stmt_exams_roster_revision;

SET @has_participants_roster_revision := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'participants' AND COLUMN_NAME = 'roster_revision'
);
SET @add_participants_roster_revision := IF(
  @has_participants_roster_revision = 0,
  'ALTER TABLE participants ADD COLUMN roster_revision BIGINT NOT NULL DEFAULT 0 AFTER access_code',
  'SELECT 1'
);
PREPARE stmt_participants_roster_revision FROM @add_participants_roster_revision;
EXECUTE stmt_participants_roster_revision;
DEALLOCATE PREPARE stmt_participants_roster_revision;

-- Let duplicate legacy students fail the index creation. The migration must
-- not delete or silently rewrite data to make the constraint pass.
SET @has_participant_identity_index := (
  SELECT COUNT(*) FROM information_schema.statistics
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'participants'
    AND INDEX_NAME = 'uniq_participants_exam_student'
);
SET @add_participant_identity_index := IF(
  @has_participant_identity_index = 0,
  'ALTER TABLE participants ADD UNIQUE KEY uniq_participants_exam_student (exam_id, student_id)',
  'SELECT 1'
);
PREPARE stmt_participant_identity_index FROM @add_participant_identity_index;
EXECUTE stmt_participant_identity_index;
DEALLOCATE PREPARE stmt_participant_identity_index;

CREATE TABLE IF NOT EXISTS roster_event_receipts (
  event_id       VARCHAR(36) NOT NULL PRIMARY KEY,
  deployment_id  VARCHAR(36) NOT NULL,
  exam_id        VARCHAR(36) NOT NULL,
  revision       BIGINT      NOT NULL,
  participant_id VARCHAR(36) NOT NULL,
  payload_hash   CHAR(64)    NOT NULL,
  status         VARCHAR(20) NOT NULL,
  outcome_code   VARCHAR(60) NOT NULL DEFAULT '',
  created_at     DATETIME    NOT NULL,
  updated_at     DATETIME    NOT NULL,
  UNIQUE KEY uniq_roster_receipt_deployment_revision (deployment_id, revision),
  KEY idx_roster_receipts_deployment (deployment_id),
  KEY idx_roster_receipts_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
