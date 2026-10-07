-- Revert 050: v2_notification_settings. A person's choices go with it;
-- the morning email's stays in v2.dream_settings, untouched.

DROP TRIGGER IF EXISTS dream_settings_email_off_moves_version ON v2.dream_settings;
DROP TRIGGER IF EXISTS dream_settings_email_moves_version ON v2.dream_settings;
DROP FUNCTION IF EXISTS v2.dream_email_moves_notification_version();
DROP TABLE IF EXISTS v2.notification_settings;
