-- Task 19 review: partition items and attempts per exam so one VPS can host
-- multiple exams without cross-exam exposure. MySQL 5.7+/8.x compatible:
-- index existence is checked via information_schema.statistics because MySQL
-- does not support CREATE INDEX IF NOT EXISTS. Idempotent.

-- items: each bundle's item snapshots belong to exactly one exam.
SET @has_items_exam := (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'items' AND COLUMN_NAME = 'exam_id');
SET @sql := IF(@has_items_exam = 0,
  'ALTER TABLE items ADD COLUMN exam_id VARCHAR(36) NOT NULL DEFAULT '''' AFTER id',
  'SELECT ''items.exam_id exists''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- attempts: scope every attempt to the exam it was taken for.
SET @has_attempts_exam := (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'attempts' AND COLUMN_NAME = 'exam_id');
SET @sql := IF(@has_attempts_exam = 0,
  'ALTER TABLE attempts ADD COLUMN exam_id VARCHAR(36) NOT NULL DEFAULT '''' AFTER student_id',
  'SELECT ''attempts.exam_id exists''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- participants: scope every participant to its exam's bundle.
SET @has_participants_exam := (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'participants' AND COLUMN_NAME = 'exam_id');
SET @sql := IF(@has_participants_exam = 0,
  'ALTER TABLE participants ADD COLUMN exam_id VARCHAR(36) NOT NULL DEFAULT '''' AFTER student_id',
  'SELECT ''participants.exam_id exists''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- exams: load-time hash of the stored item/participant set, used by preflight.
SET @has_exams_content_hash := (SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'exams' AND COLUMN_NAME = 'content_hash');
SET @sql := IF(@has_exams_content_hash = 0,
  'ALTER TABLE exams ADD COLUMN content_hash VARCHAR(80) NOT NULL DEFAULT '''' AFTER bundle_checksum',
  'SELECT ''exams.content_hash exists''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

SET @has_idx_participants_exam := (SELECT COUNT(*) FROM information_schema.statistics
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'participants' AND INDEX_NAME = 'idx_participants_exam');
SET @sql := IF(@has_idx_participants_exam = 0,
  'ALTER TABLE participants ADD INDEX idx_participants_exam (exam_id)',
  'SELECT ''idx_participants_exam exists''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- idx_items_exam (exam_id, section_sort_order, sort_order)
SET @has_idx_items := (SELECT COUNT(*) FROM information_schema.statistics
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'items' AND INDEX_NAME = 'idx_items_exam');
SET @sql := IF(@has_idx_items = 0,
  'ALTER TABLE items ADD INDEX idx_items_exam (exam_id, section_sort_order, sort_order)',
  'SELECT ''idx_items_exam exists''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;

-- idx_attempts_exam (exam_id, participant_id, attempt_no)
SET @has_idx_attempts := (SELECT COUNT(*) FROM information_schema.statistics
  WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'attempts' AND INDEX_NAME = 'idx_attempts_exam');
SET @sql := IF(@has_idx_attempts = 0,
  'ALTER TABLE attempts ADD INDEX idx_attempts_exam (exam_id, participant_id, attempt_no)',
  'SELECT ''idx_attempts_exam exists''');
PREPARE stmt FROM @sql; EXECUTE stmt; DEALLOCATE PREPARE stmt;
