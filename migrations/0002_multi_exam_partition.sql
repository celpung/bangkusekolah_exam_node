-- Task 17/18 review: partition items and attempts per exam so one VPS can
-- host multiple exams without cross-exam exposure. Idempotent.

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

CREATE INDEX IF NOT EXISTS idx_items_exam ON items (exam_id, section_sort_order, sort_order);
CREATE INDEX IF NOT EXISTS idx_attempts_exam ON attempts (exam_id, participant_id, attempt_no);
