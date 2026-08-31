-- Bind an in-progress attempt to the mobile app installation that opened it.
-- Existing attempts remain nullable so an upgrade does not invalidate
-- historical attempts.

SET @has_device_id := (
  SELECT COUNT(*) FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'attempts'
    AND COLUMN_NAME = 'device_id'
);
SET @add_device_id := IF(
  @has_device_id = 0,
  'ALTER TABLE attempts ADD COLUMN device_id VARCHAR(128) NULL AFTER exam_id',
  'SELECT 1'
);
PREPARE stmt FROM @add_device_id;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @has_device_index := (
  SELECT COUNT(*) FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'attempts'
    AND INDEX_NAME = 'idx_attempts_device_id'
);
SET @add_device_index := IF(
  @has_device_index = 0,
  'ALTER TABLE attempts ADD INDEX idx_attempts_device_id (device_id)',
  'SELECT 1'
);
PREPARE stmt FROM @add_device_index;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

