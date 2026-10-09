-- 024: worker_heartbeats
--
-- One row per running worker process. workerapp/heartbeat.go upserts
-- it every 15s and deletes it on graceful shutdown; the admin ops
-- pulse (store.getOpsWorkers) reads it to count live worker machines.
--
-- This used to live in River's own `river_client` table, which River
-- never wrote itself. River v0.40 (its schema migration 007) drops
-- `river_client` and `river_client_queue` as unused, so the heartbeat
-- moves to a table the app owns. Rows are not copied over: app
-- migrations run before River's on a fresh database (river_client
-- doesn't exist yet), and new workers re-register within one tick.
--
-- UNLOGGED like river_client was: rows are rewritten every 15s, so
-- losing them on a crash or compute restart is harmless and saves WAL.
CREATE UNLOGGED TABLE worker_heartbeats (
    id text PRIMARY KEY,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT worker_heartbeats_id_length CHECK (char_length(id) > 0 AND char_length(id) < 128)
);
