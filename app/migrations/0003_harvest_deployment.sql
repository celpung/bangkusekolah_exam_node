-- Task 20 review: harvest_log gains deployment scoping and batch audit
-- columns. Idempotent, MySQL-compatible (information_schema guards).

SET @has_hl_deployment := (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'harvest_log' AND COLUMN_NAME = 'deployment_id');
SET @sql := IF(@has_hl_deployment = 0,
  'ALTER TABLE harvest_log ADD COLUMN deployment_id VARCHAR(36) NOT NULL DEFAULT '''' AFTER attempt_id',
  'SELECT ''harvest_log.deployment_id exists''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @has_idx_attempts_harvest := (SELECT COUNT(*) FROM information_schema.statistics
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'attempts' AND INDEX_NAME = 'idx_attempts_harvest');
SET @sql := IF(@has_idx_attempts_harvest = 0,
  'ALTER TABLE attempts ADD INDEX idx_attempts_harvest (status, harvested_at)',
  'SELECT ''idx_attempts_harvest exists''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
