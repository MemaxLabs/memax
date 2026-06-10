-- 014: email_otp_codes
--
-- Email OTP login (third sign-in path alongside GitHub + Google OAuth).
-- A request stores a hashed 6-digit code keyed by canonicalized email;
-- the verify endpoint scans the most recent active row, increments
-- attempts on a miss, marks consumed on a hit. The same row carries
-- the carry-through client_redirect + invite_token so the verify call
-- enters loginOrCreateUser with the same context an OAuth callback
-- would. Indexes optimize for the three hot paths: most-recent active
-- code lookup, per-email/per-IP rate-limit windows, and the expired
-- code reaper.
CREATE TABLE email_otp_codes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT        NOT NULL,
    code_hash       TEXT        NOT NULL,
    purpose         TEXT        NOT NULL DEFAULT 'login',
    attempts        INTEGER     NOT NULL DEFAULT 0,
    max_attempts    INTEGER     NOT NULL DEFAULT 5,
    client_redirect TEXT        NOT NULL DEFAULT '',
    invite_token    TEXT        NOT NULL DEFAULT '',
    request_ip      TEXT        NOT NULL DEFAULT '',
    user_agent      TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ NOT NULL,
    consumed_at     TIMESTAMPTZ
);

-- Active code lookup: verify picks the most-recent unconsumed row for
-- an email and checks expiry in code. Partial index keeps it tiny.
CREATE INDEX idx_email_otp_codes_active
    ON email_otp_codes (email, created_at DESC)
    WHERE consumed_at IS NULL;

-- Per-email rate-limit window (e.g. 3 requests / hour).
CREATE INDEX idx_email_otp_codes_email_created_at
    ON email_otp_codes (email, created_at DESC);

-- Per-IP rate-limit window (e.g. 10 requests / hour). Partial: the
-- empty-string default is a placeholder for missing IPs and would just
-- bloat the index.
CREATE INDEX idx_email_otp_codes_request_ip_created_at
    ON email_otp_codes (request_ip, created_at DESC)
    WHERE request_ip <> '';

-- Reaper: DELETE WHERE expires_at < now() runs on a cron.
CREATE INDEX idx_email_otp_codes_expires
    ON email_otp_codes (expires_at);
