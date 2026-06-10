-- Revert 008: drop_agent_session_tables
--
-- Recreates the four agent-session-sync tables with their original baseline
-- shape. Data is NOT recoverable — this exists so a rollback restores the
-- schema enough that the previous binary can come back online (it would
-- start writing fresh rows). Indexes/foreign keys mirror what migration
-- 001_baseline_v1 originally produced.

CREATE TABLE public.agent_sessions (
    id uuid NOT NULL,
    owner_id uuid NOT NULL,
    agent text NOT NULL,
    file_path text NOT NULL,
    scope text DEFAULT 'global'::text NOT NULL,
    session_type text DEFAULT 'session'::text NOT NULL,
    filename text NOT NULL,
    content_type text NOT NULL,
    size_bytes bigint DEFAULT 0 NOT NULL,
    content_hash text NOT NULL,
    object_key text NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.agent_sessions
    ADD CONSTRAINT agent_sessions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.agent_sessions
    ADD CONSTRAINT agent_sessions_owner_id_agent_file_path_scope_key UNIQUE (owner_id, agent, file_path, scope);

ALTER TABLE ONLY public.agent_sessions
    ADD CONSTRAINT agent_sessions_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE INDEX idx_agent_sessions_owner ON public.agent_sessions USING btree (owner_id);
CREATE INDEX idx_agent_sessions_owner_agent ON public.agent_sessions USING btree (owner_id, agent);
CREATE INDEX idx_agent_sessions_owner_scope ON public.agent_sessions USING btree (owner_id, scope);

CREATE TABLE public.agent_session_sync_states (
    owner_id uuid NOT NULL,
    device_id text NOT NULL,
    agent text NOT NULL,
    file_path text NOT NULL,
    scope text NOT NULL,
    local_path text,
    last_seen_version integer NOT NULL,
    last_seen_hash text NOT NULL,
    suppressed boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.agent_session_sync_states
    ADD CONSTRAINT agent_session_sync_states_pkey PRIMARY KEY (owner_id, device_id, agent, file_path, scope);

ALTER TABLE ONLY public.agent_session_sync_states
    ADD CONSTRAINT agent_session_sync_states_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE INDEX idx_agent_session_sync_states_owner_device ON public.agent_session_sync_states USING btree (owner_id, device_id);

CREATE TABLE public.agent_session_tombstones (
    owner_id uuid NOT NULL,
    agent text NOT NULL,
    file_path text NOT NULL,
    scope text NOT NULL,
    version integer NOT NULL,
    deleted_at timestamp with time zone DEFAULT now() NOT NULL,
    session_type text,
    filename text,
    content_type text,
    size_bytes bigint,
    content_hash text,
    object_key text,
    content_expires_at timestamp with time zone
);

ALTER TABLE ONLY public.agent_session_tombstones
    ADD CONSTRAINT agent_session_tombstones_pkey PRIMARY KEY (owner_id, agent, file_path, scope);

ALTER TABLE ONLY public.agent_session_tombstones
    ADD CONSTRAINT agent_session_tombstones_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE INDEX idx_agent_session_tombstones_owner ON public.agent_session_tombstones USING btree (owner_id);

CREATE TABLE public.agent_session_snapshots (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    session_id uuid,
    owner_id uuid NOT NULL,
    agent text NOT NULL,
    file_path text NOT NULL,
    scope text DEFAULT 'global'::text NOT NULL,
    device_id text NOT NULL,
    device_label text,
    content_hash text NOT NULL,
    object_key text NOT NULL,
    version_at integer NOT NULL,
    reason text DEFAULT 'divergence'::text NOT NULL,
    session_type text DEFAULT 'session'::text NOT NULL,
    filename text DEFAULT ''::text NOT NULL,
    content_type text DEFAULT 'application/octet-stream'::text NOT NULL,
    size_bytes bigint,
    content_expires_at timestamp with time zone,
    purged_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.agent_session_snapshots
    ADD CONSTRAINT agent_session_snapshots_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.agent_session_snapshots
    ADD CONSTRAINT agent_session_snapshots_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.agent_session_snapshots
    ADD CONSTRAINT agent_session_snapshots_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.agent_sessions(id) ON DELETE SET NULL;

CREATE INDEX idx_session_snapshots_expiry ON public.agent_session_snapshots USING btree (content_expires_at) WHERE ((purged_at IS NULL) AND (content_expires_at IS NOT NULL));
CREATE INDEX idx_session_snapshots_owner ON public.agent_session_snapshots USING btree (owner_id, created_at DESC);
CREATE INDEX idx_session_snapshots_session ON public.agent_session_snapshots USING btree (session_id) WHERE (session_id IS NOT NULL);
