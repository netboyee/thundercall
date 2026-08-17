SET @account_settings_drops = CONCAT_WS(', ',
  IF(
    EXISTS(
      SELECT 1
      FROM information_schema.columns
      WHERE table_schema = DATABASE()
        AND table_name = 'account_settings'
        AND column_name = 'sms_enabled'
    ),
    'DROP COLUMN sms_enabled',
    NULL
  ),
  IF(
    EXISTS(
      SELECT 1
      FROM information_schema.columns
      WHERE table_schema = DATABASE()
        AND table_name = 'account_settings'
        AND column_name = 'email_enabled'
    ),
    'DROP COLUMN email_enabled',
    NULL
  )
);
SET @account_settings_sql = IF(
  @account_settings_drops = '',
  'SELECT 1',
  CONCAT('ALTER TABLE account_settings ', @account_settings_drops)
);
PREPARE stmt FROM @account_settings_sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @users_settings_drops = CONCAT_WS(', ',
  IF(
    EXISTS(
      SELECT 1
      FROM information_schema.columns
      WHERE table_schema = DATABASE()
        AND table_name = 'users_settings'
        AND column_name = 'sms_enabled'
    ),
    'DROP COLUMN sms_enabled',
    NULL
  ),
  IF(
    EXISTS(
      SELECT 1
      FROM information_schema.columns
      WHERE table_schema = DATABASE()
        AND table_name = 'users_settings'
        AND column_name = 'email_enabled'
    ),
    'DROP COLUMN email_enabled',
    NULL
  )
);
SET @users_settings_sql = IF(
  @users_settings_drops = '',
  'SELECT 1',
  CONCAT('ALTER TABLE users_settings ', @users_settings_drops)
);
PREPARE stmt FROM @users_settings_sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @users_messages_drops = CONCAT_WS(', ',
  IF(
    EXISTS(
      SELECT 1
      FROM information_schema.columns
      WHERE table_schema = DATABASE()
        AND table_name = 'users_messages'
        AND column_name = 'sms_enabled'
    ),
    'DROP COLUMN sms_enabled',
    NULL
  ),
  IF(
    EXISTS(
      SELECT 1
      FROM information_schema.columns
      WHERE table_schema = DATABASE()
        AND table_name = 'users_messages'
        AND column_name = 'email_enabled'
    ),
    'DROP COLUMN email_enabled',
    NULL
  )
);
SET @users_messages_sql = IF(
  @users_messages_drops = '',
  'SELECT 1',
  CONCAT('ALTER TABLE users_messages ', @users_messages_drops)
);
PREPARE stmt FROM @users_messages_sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
