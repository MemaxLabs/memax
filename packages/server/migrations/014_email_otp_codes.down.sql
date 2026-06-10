-- Revert 014: email_otp_codes

DROP INDEX IF EXISTS idx_email_otp_codes_expires;
DROP INDEX IF EXISTS idx_email_otp_codes_request_ip_created_at;
DROP INDEX IF EXISTS idx_email_otp_codes_email_created_at;
DROP INDEX IF EXISTS idx_email_otp_codes_active;
DROP TABLE IF EXISTS email_otp_codes;
