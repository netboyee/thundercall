CREATE DATABASE IF NOT EXISTS thundercall
  CHARACTER SET utf8mb4
  COLLATE utf8mb4_unicode_ci;

USE thundercall;

CREATE TABLE IF NOT EXISTS accounts (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  name VARCHAR(255) NOT NULL,
  active TINYINT(1) NOT NULL DEFAULT 1,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS users (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  account_id BIGINT UNSIGNED NOT NULL,
  external_id VARCHAR(128) NULL,
  first_name VARCHAR(120) NULL,
  last_name VARCHAR(120) NULL,
  display_name VARCHAR(255) NULL,
  title VARCHAR(255) NULL,
  active TINYINT(1) NOT NULL DEFAULT 1,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_users_account_id (account_id),
  CONSTRAINT fk_users_account_id
    FOREIGN KEY (account_id) REFERENCES accounts (id)
    ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS user_contact_methods (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id BIGINT UNSIGNED NOT NULL,
  channel VARCHAR(16) NOT NULL,
  destination VARCHAR(320) NOT NULL,
  is_primary TINYINT(1) NOT NULL DEFAULT 0,
  is_verified TINYINT(1) NOT NULL DEFAULT 0,
  active TINYINT(1) NOT NULL DEFAULT 1,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_user_contact_methods (user_id, channel, destination),
  KEY idx_user_contact_methods_channel (channel),
  CONSTRAINT fk_user_contact_methods_user_id
    FOREIGN KEY (user_id) REFERENCES users (id)
    ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS locations (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  account_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(255) NOT NULL,
  address_line_1 VARCHAR(255) NULL,
  address_line_2 VARCHAR(255) NULL,
  city VARCHAR(120) NULL,
  state_code VARCHAR(16) NULL,
  postal_code VARCHAR(32) NULL,
  county_fips VARCHAR(32) NULL,
  nws_zone VARCHAR(32) NULL,
  latitude DECIMAL(10,7) NULL,
  longitude DECIMAL(10,7) NULL,
  coverage_geometry GEOMETRY SRID 4326 NULL,
  is_thundercall_enabled TINYINT(1) NOT NULL DEFAULT 1,
  active TINYINT(1) NOT NULL DEFAULT 1,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_locations_account_id (account_id),
  KEY idx_locations_county_fips (county_fips),
  KEY idx_locations_nws_zone (nws_zone),
  CONSTRAINT fk_locations_account_id
    FOREIGN KEY (account_id) REFERENCES accounts (id)
    ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS users_locations (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id BIGINT UNSIGNED NOT NULL,
  location_id BIGINT UNSIGNED NOT NULL,
  subscription_type VARCHAR(32) NOT NULL DEFAULT 'direct',
  is_primary TINYINT(1) NOT NULL DEFAULT 0,
  is_thundercall_enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_users_locations (user_id, location_id, subscription_type),
  KEY idx_users_locations_location_id (location_id),
  CONSTRAINT fk_users_locations_user_id
    FOREIGN KEY (user_id) REFERENCES users (id)
    ON DELETE CASCADE,
  CONSTRAINT fk_users_locations_location_id
    FOREIGN KEY (location_id) REFERENCES locations (id)
    ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS account_settings (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  account_id BIGINT UNSIGNED NOT NULL,
  message_type_code VARCHAR(64) NOT NULL,
  voice_enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_account_settings (account_id, message_type_code),
  CONSTRAINT fk_account_settings_account_id
    FOREIGN KEY (account_id) REFERENCES accounts (id)
    ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS users_settings (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id BIGINT UNSIGNED NOT NULL,
  message_type_code VARCHAR(64) NOT NULL,
  voice_enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_users_settings (user_id, message_type_code),
  CONSTRAINT fk_users_settings_user_id
    FOREIGN KEY (user_id) REFERENCES users (id)
    ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS api_users (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  account_id BIGINT UNSIGNED NOT NULL,
  email VARCHAR(255) NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  display_name VARCHAR(255) NULL,
  active TINYINT(1) NOT NULL DEFAULT 1,
  last_login_at TIMESTAMP NULL DEFAULT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_api_users_email (email),
  KEY idx_api_users_account_id (account_id),
  CONSTRAINT fk_api_users_account_id
    FOREIGN KEY (account_id) REFERENCES accounts (id)
    ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS api_sessions (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  api_user_id BIGINT UNSIGNED NOT NULL,
  token_hash CHAR(64) NOT NULL,
  expires_at TIMESTAMP NOT NULL,
  revoked_at TIMESTAMP NULL DEFAULT NULL,
  last_seen_at TIMESTAMP NULL DEFAULT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_api_sessions_token_hash (token_hash),
  KEY idx_api_sessions_user_id (api_user_id),
  KEY idx_api_sessions_expires (expires_at, revoked_at),
  CONSTRAINT fk_api_sessions_api_user_id
    FOREIGN KEY (api_user_id) REFERENCES api_users (id)
    ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS source_messages (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  source VARCHAR(64) NOT NULL,
  external_id VARCHAR(128) NOT NULL,
  wmo_code VARCHAR(32) NULL,
  wfo_code VARCHAR(16) NULL,
  awips_id VARCHAR(32) NULL,
  product_category VARCHAR(16) NULL,
  issued_at TIMESTAMP NULL DEFAULT NULL,
  raw_payload LONGTEXT NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'received',
  parse_error TEXT NULL,
  received_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  parsed_at TIMESTAMP NULL DEFAULT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_source_messages_source_external_id (source, external_id),
  KEY idx_source_messages_source_received_at (source, received_at),
  KEY idx_source_messages_status_received_at (status, received_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

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

CREATE TABLE IF NOT EXISTS messages (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  account_id BIGINT UNSIGNED NULL,
  source_message_id BIGINT UNSIGNED NULL,
  nws_event_id BIGINT UNSIGNED NULL,
  external_message_id VARCHAR(128) NULL,
  source_segment_index INT NULL,
  fingerprint CHAR(64) NOT NULL,
  source VARCHAR(64) NOT NULL,
  event_code VARCHAR(32) NOT NULL,
  message_type VARCHAR(255) NOT NULL,
  alert_type_code VARCHAR(64) NOT NULL,
  title VARCHAR(255) NULL,
  body LONGTEXT NOT NULL,
  coordinate VARCHAR(255) NULL,
  polygon_wkt LONGTEXT NULL,
  fips_codes_json JSON NULL,
  nws_zones_json JSON NULL,
  primary_vtec_raw VARCHAR(128) NULL,
  vtec_action VARCHAR(16) NULL,
  original_payload LONGTEXT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'accepted',
  issued_at TIMESTAMP NULL DEFAULT NULL,
  received_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  processed_at TIMESTAMP NULL DEFAULT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_messages_account_id (account_id),
  KEY idx_messages_source_message_id (source_message_id),
  KEY idx_messages_nws_event_id (nws_event_id),
  KEY idx_messages_fingerprint_received_at (fingerprint, received_at),
  KEY idx_messages_status_received_at (status, received_at),
  KEY idx_messages_event_code (event_code),
  UNIQUE KEY uq_messages_source_segment (source_message_id, source_segment_index),
  CONSTRAINT fk_messages_account_id
    FOREIGN KEY (account_id) REFERENCES accounts (id)
    ON DELETE SET NULL,
  CONSTRAINT fk_messages_source_message_id
    FOREIGN KEY (source_message_id) REFERENCES source_messages (id)
    ON DELETE SET NULL,
  CONSTRAINT fk_messages_nws_event_id
    FOREIGN KEY (nws_event_id) REFERENCES nws_events (id)
    ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

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

CREATE TABLE IF NOT EXISTS users_messages (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  message_id BIGINT UNSIGNED NOT NULL,
  user_id BIGINT UNSIGNED NOT NULL,
  matched_location_id BIGINT UNSIGNED NULL,
  resolution_reason VARCHAR(128) NOT NULL DEFAULT 'location_match',
  voice_enabled TINYINT(1) NOT NULL DEFAULT 0,
  status VARCHAR(32) NOT NULL DEFAULT 'queued',
  queued_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  delivered_at TIMESTAMP NULL DEFAULT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_users_messages (message_id, user_id),
  KEY idx_users_messages_user_id (user_id),
  KEY idx_users_messages_status (status),
  CONSTRAINT fk_users_messages_message_id
    FOREIGN KEY (message_id) REFERENCES messages (id)
    ON DELETE CASCADE,
  CONSTRAINT fk_users_messages_user_id
    FOREIGN KEY (user_id) REFERENCES users (id)
    ON DELETE CASCADE,
  CONSTRAINT fk_users_messages_location_id
    FOREIGN KEY (matched_location_id) REFERENCES locations (id)
    ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS delivery_attempts (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  users_message_id BIGINT UNSIGNED NOT NULL,
  notification_id BIGINT UNSIGNED NULL,
  channel VARCHAR(16) NOT NULL,
  attempt_number INT NOT NULL DEFAULT 1,
  destination VARCHAR(320) NOT NULL,
  provider VARCHAR(64) NULL,
  provider_message_id VARCHAR(255) NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'queued',
  error_message TEXT NULL,
  requested_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  sent_at TIMESTAMP NULL DEFAULT NULL,
  delivered_at TIMESTAMP NULL DEFAULT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_delivery_attempts_users_message_channel_attempt (users_message_id, channel, attempt_number),
  UNIQUE KEY uq_delivery_attempts_notification_attempt (notification_id, attempt_number),
  KEY idx_delivery_attempts_users_message_id (users_message_id),
  KEY idx_delivery_attempts_notification_id (notification_id),
  KEY idx_delivery_attempts_channel_status (channel, status),
  CONSTRAINT fk_delivery_attempts_users_message_id
    FOREIGN KEY (users_message_id) REFERENCES users_messages (id)
    ON DELETE CASCADE,
  CONSTRAINT fk_delivery_attempts_notification_id
    FOREIGN KEY (notification_id) REFERENCES notifications (id)
    ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS outbox_events (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  aggregate_type VARCHAR(64) NOT NULL,
  aggregate_id BIGINT UNSIGNED NOT NULL,
  event_type VARCHAR(64) NOT NULL,
  stream_key VARCHAR(128) NOT NULL,
  payload_json JSON NOT NULL,
  published_at TIMESTAMP NULL DEFAULT NULL,
  attempt_count INT NOT NULL DEFAULT 0,
  last_error TEXT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_outbox_events_publish (published_at, created_at),
  KEY idx_outbox_events_aggregate (aggregate_type, aggregate_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
