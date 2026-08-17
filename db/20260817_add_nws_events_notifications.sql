CREATE TABLE IF NOT EXISTS nws_events (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  event_key VARCHAR(128) NOT NULL,
  product_class VARCHAR(8) NOT NULL,
  office_id VARCHAR(16) NOT NULL,
  phenomenon VARCHAR(8) NOT NULL,
  significance VARCHAR(8) NOT NULL,
  etn VARCHAR(16) NOT NULL,
  event_year SMALLINT NOT NULL,
  last_action VARCHAR(16) NOT NULL,
  begins_at TIMESTAMP NULL DEFAULT NULL,
  ends_at TIMESTAMP NULL DEFAULT NULL,
  first_issued_at TIMESTAMP NULL DEFAULT NULL,
  last_issued_at TIMESTAMP NULL DEFAULT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_nws_events_event_key (event_key),
  KEY idx_nws_events_natural_key (product_class, office_id, phenomenon, significance, etn, event_year)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

SET @messages_add_nws_event_id = (
  SELECT COUNT(*)
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'messages'
    AND column_name = 'nws_event_id'
);
SET @messages_add_nws_event_id_sql = IF(
  @messages_add_nws_event_id = 0,
  'ALTER TABLE messages ADD COLUMN nws_event_id BIGINT UNSIGNED NULL AFTER source_message_id',
  'SELECT 1'
);
PREPARE stmt FROM @messages_add_nws_event_id_sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @messages_add_primary_vtec_raw = (
  SELECT COUNT(*)
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'messages'
    AND column_name = 'primary_vtec_raw'
);
SET @messages_add_primary_vtec_raw_sql = IF(
  @messages_add_primary_vtec_raw = 0,
  'ALTER TABLE messages ADD COLUMN primary_vtec_raw VARCHAR(128) NULL AFTER nws_zones_json',
  'SELECT 1'
);
PREPARE stmt FROM @messages_add_primary_vtec_raw_sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @messages_add_vtec_action = (
  SELECT COUNT(*)
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'messages'
    AND column_name = 'vtec_action'
);
SET @messages_add_vtec_action_sql = IF(
  @messages_add_vtec_action = 0,
  'ALTER TABLE messages ADD COLUMN vtec_action VARCHAR(16) NULL AFTER primary_vtec_raw',
  'SELECT 1'
);
PREPARE stmt FROM @messages_add_vtec_action_sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @messages_add_event_index = (
  SELECT COUNT(*)
  FROM information_schema.statistics
  WHERE table_schema = DATABASE()
    AND table_name = 'messages'
    AND index_name = 'idx_messages_nws_event_id'
);
SET @messages_add_event_index_sql = IF(
  @messages_add_event_index = 0,
  'ALTER TABLE messages ADD KEY idx_messages_nws_event_id (nws_event_id)',
  'SELECT 1'
);
PREPARE stmt FROM @messages_add_event_index_sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @messages_add_event_fk = (
  SELECT COUNT(*)
  FROM information_schema.table_constraints
  WHERE constraint_schema = DATABASE()
    AND table_name = 'messages'
    AND constraint_name = 'fk_messages_nws_event_id'
    AND constraint_type = 'FOREIGN KEY'
);
SET @messages_add_event_fk_sql = IF(
  @messages_add_event_fk = 0,
  'ALTER TABLE messages ADD CONSTRAINT fk_messages_nws_event_id FOREIGN KEY (nws_event_id) REFERENCES nws_events (id) ON DELETE SET NULL',
  'SELECT 1'
);
PREPARE stmt FROM @messages_add_event_fk_sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

CREATE TABLE IF NOT EXISTS notifications (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  nws_event_id BIGINT UNSIGNED NOT NULL,
  user_id BIGINT UNSIGNED NOT NULL,
  channel VARCHAR(16) NOT NULL,
  first_message_id BIGINT UNSIGNED NOT NULL,
  last_message_id BIGINT UNSIGNED NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'queued',
  first_attempted_at TIMESTAMP NULL DEFAULT NULL,
  sent_at TIMESTAMP NULL DEFAULT NULL,
  delivered_at TIMESTAMP NULL DEFAULT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_notifications_event_user_channel (nws_event_id, user_id, channel),
  KEY idx_notifications_user_channel_status (user_id, channel, status),
  KEY idx_notifications_first_message_id (first_message_id),
  KEY idx_notifications_last_message_id (last_message_id),
  CONSTRAINT fk_notifications_nws_event_id
    FOREIGN KEY (nws_event_id) REFERENCES nws_events (id)
    ON DELETE CASCADE,
  CONSTRAINT fk_notifications_user_id
    FOREIGN KEY (user_id) REFERENCES users (id)
    ON DELETE CASCADE,
  CONSTRAINT fk_notifications_first_message_id
    FOREIGN KEY (first_message_id) REFERENCES messages (id)
    ON DELETE CASCADE,
  CONSTRAINT fk_notifications_last_message_id
    FOREIGN KEY (last_message_id) REFERENCES messages (id)
    ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

SET @delivery_attempts_add_notification_id = (
  SELECT COUNT(*)
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'delivery_attempts'
    AND column_name = 'notification_id'
);
SET @delivery_attempts_add_notification_id_sql = IF(
  @delivery_attempts_add_notification_id = 0,
  'ALTER TABLE delivery_attempts ADD COLUMN notification_id BIGINT UNSIGNED NULL AFTER users_message_id',
  'SELECT 1'
);
PREPARE stmt FROM @delivery_attempts_add_notification_id_sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @delivery_attempts_add_attempt_number = (
  SELECT COUNT(*)
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'delivery_attempts'
    AND column_name = 'attempt_number'
);
SET @delivery_attempts_add_attempt_number_sql = IF(
  @delivery_attempts_add_attempt_number = 0,
  'ALTER TABLE delivery_attempts ADD COLUMN attempt_number INT NOT NULL DEFAULT 1 AFTER channel',
  'SELECT 1'
);
PREPARE stmt FROM @delivery_attempts_add_attempt_number_sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @drop_old_attempt_unique = (
  SELECT COUNT(*)
  FROM information_schema.statistics
  WHERE table_schema = DATABASE()
    AND table_name = 'delivery_attempts'
    AND index_name = 'uq_delivery_attempts_users_message_channel'
);
SET @drop_old_attempt_unique_sql = IF(
  @drop_old_attempt_unique = 1,
  'ALTER TABLE delivery_attempts DROP INDEX uq_delivery_attempts_users_message_channel',
  'SELECT 1'
);
PREPARE stmt FROM @drop_old_attempt_unique_sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_user_attempt_unique = (
  SELECT COUNT(*)
  FROM information_schema.statistics
  WHERE table_schema = DATABASE()
    AND table_name = 'delivery_attempts'
    AND index_name = 'uq_delivery_attempts_users_message_channel_attempt'
);
SET @add_user_attempt_unique_sql = IF(
  @add_user_attempt_unique = 0,
  'ALTER TABLE delivery_attempts ADD UNIQUE KEY uq_delivery_attempts_users_message_channel_attempt (users_message_id, channel, attempt_number)',
  'SELECT 1'
);
PREPARE stmt FROM @add_user_attempt_unique_sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_notification_attempt_unique = (
  SELECT COUNT(*)
  FROM information_schema.statistics
  WHERE table_schema = DATABASE()
    AND table_name = 'delivery_attempts'
    AND index_name = 'uq_delivery_attempts_notification_attempt'
);
SET @add_notification_attempt_unique_sql = IF(
  @add_notification_attempt_unique = 0,
  'ALTER TABLE delivery_attempts ADD UNIQUE KEY uq_delivery_attempts_notification_attempt (notification_id, attempt_number)',
  'SELECT 1'
);
PREPARE stmt FROM @add_notification_attempt_unique_sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_notification_attempt_index = (
  SELECT COUNT(*)
  FROM information_schema.statistics
  WHERE table_schema = DATABASE()
    AND table_name = 'delivery_attempts'
    AND index_name = 'idx_delivery_attempts_notification_id'
);
SET @add_notification_attempt_index_sql = IF(
  @add_notification_attempt_index = 0,
  'ALTER TABLE delivery_attempts ADD KEY idx_delivery_attempts_notification_id (notification_id)',
  'SELECT 1'
);
PREPARE stmt FROM @add_notification_attempt_index_sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_notification_attempt_fk = (
  SELECT COUNT(*)
  FROM information_schema.table_constraints
  WHERE constraint_schema = DATABASE()
    AND table_name = 'delivery_attempts'
    AND constraint_name = 'fk_delivery_attempts_notification_id'
    AND constraint_type = 'FOREIGN KEY'
);
SET @add_notification_attempt_fk_sql = IF(
  @add_notification_attempt_fk = 0,
  'ALTER TABLE delivery_attempts ADD CONSTRAINT fk_delivery_attempts_notification_id FOREIGN KEY (notification_id) REFERENCES notifications (id) ON DELETE CASCADE',
  'SELECT 1'
);
PREPARE stmt FROM @add_notification_attempt_fk_sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
