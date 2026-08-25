-- The exam node's whole schema. Seven tables, no foreign keys outside the node,
-- no soft deletes, no audit columns. Total table count stays 7 — multi-exam
-- per VPS adds rows, not tables. one active deployment per exam (M rows per VPS day) — One active deployment per exam (M rows per VPS day) in
-- `exams`/`exam_node_deployments`; participants are unique per
-- (exam_id, student_id) or per deployment — node is partitioned by
-- exam_id/deployment_id, not by node_id. Every table is single-writer by
-- design: the bundle loader writes exams/items/participants, the runtime writes
-- attempts/answers/integrity_events and the participant counters, and the
-- harvest worker writes harvest_log. Nothing is written by both the node and
-- central, which is what makes ingest an insert instead of a merge.

CREATE TABLE IF NOT EXISTS exams (
  id                      VARCHAR(36)  NOT NULL PRIMARY KEY,
  deployment_id           VARCHAR(36)  NOT NULL,
  title                   VARCHAR(255) NOT NULL,
  instruction             TEXT         NULL,
  starts_at               DATETIME     NOT NULL,
  ends_at                 DATETIME     NOT NULL,
  duration_minutes        INT          NOT NULL,
  max_attempts            INT          NOT NULL,
  shuffle_questions       TINYINT(1)   NOT NULL DEFAULT 0,
  shuffle_options         TINYINT(1)   NOT NULL DEFAULT 0,
  show_result_immediately TINYINT(1)   NOT NULL DEFAULT 0,
  passing_score           DECIMAL(5,2) NULL,
  result_selection_policy VARCHAR(30)  NOT NULL,
  max_score               DECIMAL(8,2) NOT NULL,
  has_manual_items        TINYINT(1)   NOT NULL DEFAULT 0,
  access_code_prefix      VARCHAR(10)  NOT NULL,
  bundle_checksum         VARCHAR(80)  NOT NULL,
  loaded_at               DATETIME     NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS items (
  id                       VARCHAR(36)  NOT NULL PRIMARY KEY,
  section_id               VARCHAR(36)  NOT NULL,
  section_title            VARCHAR(255) NOT NULL,
  section_sort_order       INT          NOT NULL DEFAULT 0,
  sort_order               INT          NOT NULL DEFAULT 0,
  question_type            VARCHAR(40)  NOT NULL,
  prompt_snapshot          TEXT         NOT NULL,
  options_snapshot_json    JSON         NULL,
  answer_key_snapshot_json JSON         NULL,
  rubric_criteria_json     JSON         NULL,
  points                   DECIMAL(8,2) NOT NULL,
  requires_manual_grading  TINYINT(1)   NOT NULL DEFAULT 0,
  KEY idx_items_order (section_sort_order, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS participants (
  id                VARCHAR(36)  NOT NULL PRIMARY KEY,
  student_id        VARCHAR(36)  NOT NULL,
  student_name      VARCHAR(255) NOT NULL,
  access_code       VARCHAR(20)  NOT NULL,
  attempt_count     INT          NOT NULL DEFAULT 0,
  latest_attempt_id VARCHAR(36)  NULL,
  UNIQUE KEY uniq_participants_access_code (access_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS attempts (
  id                VARCHAR(36)  NOT NULL PRIMARY KEY,
  participant_id    VARCHAR(36)  NOT NULL,
  student_id        VARCHAR(36)  NOT NULL,
  attempt_no        INT          NOT NULL,
  status            VARCHAR(30)  NOT NULL,
  started_at        DATETIME     NOT NULL,
  due_at            DATETIME     NOT NULL,
  submitted_at      DATETIME     NULL,
  auto_submitted_at DATETIME     NULL,
  score             DECIMAL(8,2) NULL,
  max_score         DECIMAL(8,2) NOT NULL,
  grading_status    VARCHAR(30)  NOT NULL,
  harvested_at      DATETIME     NULL,
  UNIQUE KEY uniq_attempts_participant_no (participant_id, attempt_no),
  KEY idx_attempts_sweep (status, due_at),
  KEY idx_attempts_harvest (status, harvested_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS answers (
  id             VARCHAR(36)  NOT NULL PRIMARY KEY,
  attempt_id     VARCHAR(36)  NOT NULL,
  item_id        VARCHAR(36)  NOT NULL,
  answer_json    JSON         NULL,
  answer_text    TEXT         NULL,
  score          DECIMAL(8,2) NULL,
  max_score      DECIMAL(8,2) NOT NULL,
  grading_status VARCHAR(30)  NOT NULL,
  last_saved_at  DATETIME     NOT NULL,
  client_seq     BIGINT       NOT NULL DEFAULT 0,
  UNIQUE KEY uniq_answers_attempt_item (attempt_id, item_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS integrity_events (
  id            VARCHAR(36) NOT NULL PRIMARY KEY,
  attempt_id    VARCHAR(36) NOT NULL,
  student_id    VARCHAR(36) NOT NULL,
  event_type    VARCHAR(40) NOT NULL,
  description   TEXT        NULL,
  metadata_json JSON        NULL,
  created_at    DATETIME    NOT NULL,
  KEY idx_integrity_attempt (attempt_id, event_type, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS harvest_log (
  id             BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
  attempt_id     VARCHAR(36) NOT NULL,
  pushed_at      DATETIME    NOT NULL,
  acked_at       DATETIME    NULL,
  attempts_count INT         NOT NULL DEFAULT 0,
  last_error     TEXT        NULL,
  KEY idx_harvest_attempt (attempt_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
