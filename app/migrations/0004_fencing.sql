-- Persisted node-side abort fencing marker.
-- Idempotent for both fresh and legacy node databases.

SET @has_exams_fenced := (
  SELECT COUNT(*)
  FROM information_schema.columns
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'exams'
    AND COLUMN_NAME = 'fenced_at'
);
SET @add_exams_fenced := IF(
  @has_exams_fenced = 0,
  'ALTER TABLE exams ADD COLUMN fenced_at DATETIME NULL AFTER content_hash',
  'SELECT ''exams.fenced_at exists'''
);
PREPARE stmt_exams_fenced FROM @add_exams_fenced;
EXECUTE stmt_exams_fenced;
DEALLOCATE PREPARE stmt_exams_fenced;
