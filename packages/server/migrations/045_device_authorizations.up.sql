-- 045: device_authorizations
--
-- The device authorization grant (RFC 8628) for `memax login` and
-- `npx memax-cli init` where no browser can open on the machine: over
-- SSH, in a container, on a headless box (plan 25 §5.15, §7.3 step 2).
--
--   1. The CLI asks for a code (POST /oauth/device_authorization) and
--      shows the person a short user code ("WQRT-4821") and memax.app/device.
--   2. The person, signed in on the web, checks that the code matches and
--      confirms it (CliAuth, /v2/device-authorizations:approve). Only a
--      person on the web app may (human_web), never an agent or the CLI.
--   3. The CLI polls POST /oauth/token with the device code and, once
--      confirmed, gets a person's CLI session (surface 'cli', so its
--      writes are client-attested, never human_web), once.
--
-- Neither code is stored. device_code_hash is the SHA-256 of the 256-bit
-- device code; user_code_hash is an HMAC of the normalised user code
-- keyed by the server's secret, because a user code is short (about 30
-- bits) and a plain hash of one could be reversed from a copy of this
-- table. A row holds no token: the session is written to `sessions` when
-- the CLI collects it.
--
-- `status` is what happened to the code: pending (waiting for a person),
-- approved or denied (by user_id), consumed (the CLI collected its
-- session; a code yields one session). Expiry is read from expires_at,
-- never stored. The CLI's own words about itself (device name, OS,
-- version, the space it will use) are shown to the person as what the
-- device says, beside the address its request came from.
CREATE TABLE device_authorizations (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    device_code_hash  TEXT        NOT NULL UNIQUE,
    user_code_hash    TEXT        NOT NULL,
    client_id         TEXT        NOT NULL,
    client_version    TEXT        NOT NULL DEFAULT '',
    device_name       TEXT        NOT NULL DEFAULT '',
    device_os         TEXT        NOT NULL DEFAULT '',
    space_slug        TEXT        NOT NULL DEFAULT '',
    request_ip        TEXT        NOT NULL DEFAULT '',
    user_agent        TEXT        NOT NULL DEFAULT '',
    status            TEXT        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'denied', 'consumed')),
    user_id           UUID        REFERENCES users(id) ON DELETE CASCADE,
    interval_seconds  INTEGER     NOT NULL DEFAULT 5 CHECK (interval_seconds BETWEEN 1 AND 300),
    last_polled_at    TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at        TIMESTAMPTZ NOT NULL,
    decided_at        TIMESTAMPTZ,
    consumed_at       TIMESTAMPTZ,
    -- A person decides every code that isn't pending, and only a confirmed
    -- code is collected.
    CONSTRAINT device_authorizations_decided CHECK (
        (status = 'pending') = (user_id IS NULL AND decided_at IS NULL)
    ),
    CONSTRAINT device_authorizations_consumed CHECK (
        (status = 'consumed') = (consumed_at IS NOT NULL)
    )
);

-- A user code names one waiting request at a time; a new request that
-- draws a waiting one's code draws again.
CREATE UNIQUE INDEX idx_device_authorizations_pending_user_code
    ON device_authorizations (user_code_hash)
    WHERE status = 'pending';

-- The confirmation page finds a code's latest request, whatever its status.
CREATE INDEX idx_device_authorizations_user_code
    ON device_authorizations (user_code_hash, created_at DESC);

-- Per-address rate-limit window on new requests.
CREATE INDEX idx_device_authorizations_ip_created
    ON device_authorizations (request_ip, created_at DESC)
    WHERE request_ip <> '';

-- The reaper deletes requests a day after they expire.
CREATE INDEX idx_device_authorizations_expires
    ON device_authorizations (expires_at);
