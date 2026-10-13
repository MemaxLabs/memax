-- 056: v2_machines
--
-- The personal layer (personal-layer design §5.1): a Personal space
-- compiles to files under the person's home folder on every machine they
-- link, one set of targets per machine.
--
--   machines   a computer linked to a personal space: its CLI device, its
--              name and platform, the session that linked it, and whether
--              it is still linked
--   targets    gain root (repo | home) and, for home targets, machine_id.
--              A home target is a file in that machine's home folder, so
--              the baseline, holds, the one open observation per file,
--              Overwrite, Stop compiling and Forget's propagation steps are
--              all per machine with no change to how they work.
--
-- # Machines
--
-- A machine is space-scoped (RLS on app.space_ids, ENABLEd and FORCEd) and
-- lives only in a personal space, as do home targets: a constraint trigger
-- refuses either anywhere else, and the ledger checks first. device_key is
-- the CLI's device id, unique per space, so linking the same computer
-- again reuses its row. seq numbers machines per space for receipts'
-- object_ref ("machine-2"): a host name is personal data and never goes in
-- a receipt. memax_v2 gets no DELETE; v2.retire_space takes a retired
-- space's machines with its targets.
--
-- # What needs no receipt
--
-- machines.last_seen_at (and updated_at): when the machine's daemon last
-- read its targets, bookkeeping like targets.dirty_gen. Any other change
-- needs a receipt about the machine in the same transaction.
--
-- # Home paths
--
-- v2.home_path_valid(kind, path) is a per-kind allowlist of files under a
-- home folder: a path in a dot folder (never a project folder in ~), named
-- for the agent's own file. It is a sanity check; the daemon writes only
-- the file the person approved on that machine. The kind check admits
-- the home kinds the compiler has (claude_user, codex_user); each later
-- adapter (Gemini, Copilot, Windsurf) adds its kind and its paths here in
-- its own migration, as ledger.TargetKinds and the compiler gain it.
--
-- # Receipt vocabulary
--
-- receipts_object_kind_check gains machine; receipts_action_check gains
-- unlinked (linked was already there, for the judge's links). The lists
-- keep everything 028 through 048 admit.

ALTER TABLE v2.receipts DROP CONSTRAINT receipts_object_kind_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_object_kind_check CHECK (object_kind IN
    ('memory', 'note', 'brief', 'target', 'compile', 'handoff', 'gate', 'dream', 'agent', 'space', 'machine'));

ALTER TABLE v2.receipts DROP CONSTRAINT receipts_action_check;
ALTER TABLE v2.receipts ADD CONSTRAINT receipts_action_check CHECK (action IN
    ('proposed', 'kept', 'edited', 'rejected', 'merged', 'flagged', 'resolved', 'verified',
     'faded', 'restored', 'forgot', 'moved', 'compiled', 'handed_off', 'answered', 'undid',
     'connected', 'autonomy_changed', 'paused', 'resumed', 'disconnected',
     'revised', 'configured', 'requested', 'delivered', 'observed', 'pulled', 'overwritten', 'stopped',
     'judged', 'linked', 'superseded',
     'asked', 'withdrawn',
     'returned', 'drafted',
     'purged', 'forget_requested', 'forget_declined',
     'exported',
     'published', 'folded',
     'noted', 'switched', 'switched_back',
     'unlinked'));

-- ---------------------------------------------------------------------
-- Home paths (mirrored in Go by ledger.checkHomePath, parity-tested)
-- ---------------------------------------------------------------------

CREATE FUNCTION v2.home_path_valid(k text, p text) RETURNS boolean
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$
    SELECT v2.repo_path_valid(p)
       AND left(split_part(p, '/', 1), 1) = '.'
       AND CASE k
           WHEN 'agents_md' THEN p = '.memax/personal/AGENTS.md'
           WHEN 'claude_user' THEN split_part(p, '/', 1) <> '.memax'
                AND (p ~ '/rules/memax-personal\.md$' OR p ~ '/CLAUDE\.md$')
           WHEN 'codex_user' THEN split_part(p, '/', 1) <> '.memax'
                AND p ~ '/AGENTS(\.override)?\.md$'
           ELSE false
           END
$$;

COMMENT ON FUNCTION v2.home_path_valid(text, text) IS
    'Whether a home target''s path is a file its kind writes: in a dot folder of the home folder, named for the agent''s own file.';

-- ---------------------------------------------------------------------
-- Machines
-- ---------------------------------------------------------------------

CREATE TABLE v2.machines (
    id                 uuid PRIMARY KEY,                    -- uuidv7, from Go; also the receipts' stream
    tenant_id          uuid NOT NULL,
    space_id           uuid NOT NULL,
    seq                integer NOT NULL,                    -- machine-<seq>, per space: receipts' object_ref
    device_key         uuid NOT NULL,                       -- the CLI's device id (getOrCreateDeviceID)
    name               text NOT NULL,                       -- the short host name by default; personal data, never in a receipt
    platform           text NOT NULL,                       -- darwin, linux, …
    session_id         uuid,                                -- the CLI session that linked it last
    linked_at          timestamptz NOT NULL,
    unlinked_at        timestamptz,
    last_seen_at       timestamptz,                         -- bookkeeping: its daemon's last read
    stream_version     integer NOT NULL DEFAULT 1,
    created_receipt_id uuid NOT NULL REFERENCES v2.receipts (id),
    last_receipt_id    uuid NOT NULL REFERENCES v2.receipts (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT machines_space_fkey FOREIGN KEY (space_id, tenant_id) REFERENCES public.hubs (id, tenant_id) ON DELETE CASCADE,
    CONSTRAINT machines_id_space_key UNIQUE (id, space_id),
    CONSTRAINT machines_device_key UNIQUE (space_id, device_key),
    CONSTRAINT machines_seq_key UNIQUE (space_id, seq),
    CONSTRAINT machines_seq_check CHECK (seq >= 1 AND stream_version >= 1),
    CONSTRAINT machines_name_check CHECK (
        btrim(name) = name AND char_length(name) BETWEEN 1 AND 80 AND name !~ '[[:cntrl:]]'),
    CONSTRAINT machines_platform_check CHECK (platform ~ '^[a-z0-9][a-z0-9_-]{0,31}$'),
    CONSTRAINT machines_unlinked_check CHECK (unlinked_at IS NULL OR unlinked_at >= linked_at - interval '1 minute')
);

CREATE INDEX machines_space_idx ON v2.machines (space_id);

COMMENT ON TABLE v2.machines IS
    'A computer linked to a personal space: its home targets are that machine''s files. Its name is personal data, never in a receipt.';

-- ---------------------------------------------------------------------
-- Targets: a root, and a machine for home targets
-- ---------------------------------------------------------------------

ALTER TABLE v2.targets
    ADD COLUMN root text NOT NULL DEFAULT 'repo',
    ADD COLUMN machine_id uuid,
    ADD CONSTRAINT targets_root_check CHECK (root IN ('repo', 'home')),
    ADD CONSTRAINT targets_machine_check CHECK ((root = 'home') = (machine_id IS NOT NULL)),
    ADD CONSTRAINT targets_machine_fkey FOREIGN KEY (machine_id, space_id) REFERENCES v2.machines (id, space_id);

ALTER TABLE v2.targets DROP CONSTRAINT targets_kind_check;
ALTER TABLE v2.targets ADD CONSTRAINT targets_kind_check CHECK (kind IN
    ('agents_md', 'claude_md', 'cursor_mdc', 'chatgpt', 'gemini_md', 'copilot', 'windsurf', 'claude_rules',
     'claude_user', 'codex_user'));

-- The home kinds write only at home; at home, only they and the canonical
-- copy (agents_md) compile, and Memax's daemon writes them (local).
ALTER TABLE v2.targets ADD CONSTRAINT targets_root_kind_check CHECK (
    CASE root
    WHEN 'home' THEN kind IN ('agents_md', 'claude_user', 'codex_user')
                     AND delivery = 'local' AND v2.home_path_valid(kind, path)
    ELSE kind NOT IN ('claude_user', 'codex_user')
    END);

ALTER TABLE v2.targets DROP CONSTRAINT targets_path_key;
ALTER TABLE v2.targets ADD CONSTRAINT targets_path_key UNIQUE NULLS NOT DISTINCT (space_id, root, machine_id, kind, path);

CREATE INDEX targets_machine_idx ON v2.targets (machine_id) WHERE machine_id IS NOT NULL;

-- ---------------------------------------------------------------------
-- Only in a personal space (the ledger checks first)
-- ---------------------------------------------------------------------

CREATE FUNCTION v2.require_personal_space() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    -- Nested: a machine's row has no root, and plpgsql resolves every field
    -- an expression names.
    IF TG_TABLE_NAME = 'targets' THEN
        IF NEW.root <> 'home' THEN
            RETURN NULL;
        END IF;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM v2.spaces s WHERE s.id = NEW.space_id AND s.kind = 'personal') THEN
        RAISE EXCEPTION 'v2.%: machines and home targets live only in a personal space (space %)', TG_TABLE_NAME, NEW.space_id
            USING ERRCODE = 'MXH01';
    END IF;
    RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER machines_personal_space
    AFTER INSERT ON v2.machines
    DEFERRABLE INITIALLY IMMEDIATE
    FOR EACH ROW EXECUTE FUNCTION v2.require_personal_space();

CREATE CONSTRAINT TRIGGER targets_personal_space
    AFTER INSERT ON v2.targets
    DEFERRABLE INITIALLY IMMEDIATE
    FOR EACH ROW EXECUTE FUNCTION v2.require_personal_space();

-- ---------------------------------------------------------------------
-- Receipt enforcement (rule 1)
-- ---------------------------------------------------------------------

-- Its own function, like 047's v2.require_dream_receipt, so the receipt
-- check of the other record tables is untouched.
CREATE FUNCTION v2.require_machine_receipt() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    n v2.machines;
    o v2.machines;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        -- The daemon's last read is bookkeeping.
        n := NEW; o := OLD;
        n.last_seen_at := NULL; o.last_seen_at := NULL;
        n.updated_at := NULL; o.updated_at := NULL;
        IF n IS NOT DISTINCT FROM o THEN
            RETURN NULL;
        END IF;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts r
         WHERE r.id = NEW.last_receipt_id
           AND r.txid = pg_current_xact_id()
           AND r.space_id = NEW.space_id
           AND r.object_id = NEW.id
    ) THEN
        RAISE EXCEPTION 'v2.machines: % without a receipt written in the same transaction', TG_OP
            USING ERRCODE = 'MXR01',
                  HINT = 'Every change to the V2 record goes through internal/ledger, which writes the receipt.';
    END IF;
    RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER machines_require_receipt
    AFTER INSERT OR UPDATE ON v2.machines
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION v2.require_machine_receipt();

-- ---------------------------------------------------------------------
-- Row-level security (rule 13)
-- ---------------------------------------------------------------------

ALTER TABLE v2.machines ENABLE ROW LEVEL SECURITY;
ALTER TABLE v2.machines FORCE ROW LEVEL SECURITY;
CREATE POLICY machines_space ON v2.machines
    USING (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]))
    WITH CHECK (space_id = ANY ((SELECT v2.current_space_ids())::uuid[]));

-- ---------------------------------------------------------------------
-- Retiring a space takes its machines with its targets
-- ---------------------------------------------------------------------

-- 044's v2.retire_space, plus the machines after the targets: a machine's
-- name is personal data, so nothing of it outlives a retired space.
CREATE OR REPLACE FUNCTION v2.retire_space(p_space uuid) RETURNS void
    LANGUAGE plpgsql SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
BEGIN
    IF NOT (p_space = ANY ((SELECT v2.current_space_ids())::uuid[])) THEN
        RAISE EXCEPTION 'space %: not in this transaction''s scope', p_space USING ERRCODE = 'MXR02';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM v2.receipts r
         WHERE r.object_id = p_space AND r.object_kind = 'space' AND r.action = 'forgot'
           AND r.txid = pg_current_xact_id() AND r.space_id = p_space
    ) THEN
        RAISE EXCEPTION 'space %: a space is retired only by a Forget of the whole space in the same transaction', p_space
            USING ERRCODE = 'MXR02';
    END IF;
    IF EXISTS (SELECT 1 FROM v2.memories m WHERE m.space_id = p_space AND m.lifecycle <> 'forgotten') THEN
        RAISE EXCEPTION 'space %: forget every memory before retiring it', p_space USING ERRCODE = 'MXR02';
    END IF;
    SET CONSTRAINTS v2.targets_last_compile_fkey, v2.targets_delivered_compile_fkey DEFERRED;
    -- memax init's uploads (043): bookkeeping that refers to the hub and
    -- the memories, so it goes first.
    DELETE FROM v2.import_conflicts WHERE space_id = p_space;
    DELETE FROM v2.import_items WHERE space_id = p_space;
    DELETE FROM v2.imports WHERE space_id = p_space;
    DELETE FROM v2.memory_embeddings WHERE space_id = p_space;
    DELETE FROM v2.judge_verdicts WHERE space_id = p_space;
    DELETE FROM v2.memory_links WHERE space_id = p_space;
    DELETE FROM v2.memory_sources WHERE space_id = p_space;
    DELETE FROM v2.sources WHERE space_id = p_space;
    DELETE FROM v2.forget_requests WHERE space_id = p_space;
    DELETE FROM v2.decision_gates WHERE space_id = p_space;
    DELETE FROM v2.undo_entries WHERE space_id = p_space;
    DELETE FROM v2.target_observations WHERE space_id = p_space;
    DELETE FROM v2.compile_runs WHERE space_id = p_space;
    DELETE FROM v2.targets WHERE space_id = p_space;
    DELETE FROM v2.machines WHERE space_id = p_space;
    DELETE FROM v2.brief_versions WHERE space_id = p_space;
    DELETE FROM v2.briefs WHERE space_id = p_space;
    DELETE FROM v2.memory_versions WHERE space_id = p_space;
    DELETE FROM v2.memories WHERE space_id = p_space;
    DELETE FROM v2.command_keys WHERE space_id = p_space;
    DELETE FROM v2.agent_connection_spaces WHERE space_id = p_space;
    DELETE FROM v2.reads WHERE space_id = p_space;
    DELETE FROM v2.read_rollups WHERE space_id = p_space;
    UPDATE v2.space_ledgers SET retired_at = now() WHERE space_id = p_space;
END $$;
REVOKE ALL ON FUNCTION v2.retire_space(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION v2.retire_space(uuid) TO memax_v2;

-- ---------------------------------------------------------------------
-- Grants: the least memax_v2 needs. No DELETE; a machine's identity (id,
-- tenant, space, seq, device, the creating receipt) is not updatable, nor
-- is a target's root or machine.
-- ---------------------------------------------------------------------

GRANT SELECT, INSERT ON v2.machines TO memax_v2;
GRANT UPDATE (name, platform, session_id, linked_at, unlinked_at, last_seen_at, stream_version, last_receipt_id, updated_at)
    ON v2.machines TO memax_v2;
GRANT EXECUTE ON FUNCTION v2.home_path_valid(text, text) TO memax_v2;
