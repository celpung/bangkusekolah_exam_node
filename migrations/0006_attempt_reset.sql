-- Additive state used to reject stale harvest traffic after an attempt reset.
SET @has_reset_generation := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'attempts'
    AND COLUMN_NAME = 'reset_generation'
);
SET @add_reset_generation := IF(
  @has_reset_generation = 0,
  'ALTER TABLE attempts ADD COLUMN reset_generation BIGINT NOT NULL DEFAULT 0 AFTER grading_status',
  'SELECT 1'
);
PREPARE stmt FROM @add_reset_generation;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

CREATE TABLE IF NOT EXISTS attempt_reset_operations (
  request_id VARCHAR(100) NOT NULL PRIMARY KEY,
  operation VARCHAR(20) NOT NULL,
  command_hash CHAR(64) NOT NULL,
  status VARCHAR(30) NOT NULL,
  code VARCHAR(60) NOT NULL DEFAULT '',
  message VARCHAR(255) NOT NULL DEFAULT '',
  node_id VARCHAR(100) NOT NULL,
  deployment_id VARCHAR(36) NOT NULL,
  exam_id VARCHAR(36) NOT NULL,
  student_id VARCHAR(36) NOT NULL,
  participant_id VARCHAR(36) NOT NULL,
  attempt_id VARCHAR(36) NOT NULL,
  expected_generation BIGINT NOT NULL DEFAULT 0,
  target_generation BIGINT NOT NULL DEFAULT 0,
  expected_status VARCHAR(30) NOT NULL,
  expected_due_at DATETIME NULL,
  expected_submitted_at DATETIME NULL,
  expected_auto_submitted_at DATETIME NULL,
  deadline DATETIME NOT NULL,
  attempt_no INT NOT NULL DEFAULT 0,
  attempt_status VARCHAR(30) NOT NULL DEFAULT '',
  started_at DATETIME NULL,
  due_at DATETIME NULL,
  submitted_at DATETIME NULL,
  auto_submitted_at DATETIME NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  KEY idx_attempt_reset_operations_status (status),
  KEY idx_attempt_reset_operations_target (deployment_id, attempt_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
