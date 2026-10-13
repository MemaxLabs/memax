-- Revert 066: v2_windsurf_user
--
-- Windsurf's home files go with their runs and drift evidence (the daemon on
-- each machine keeps what it wrote), then 064's checks and path rule come
-- back (065 left them alone). Receipts are append-only, so the targets'
-- receipts stay.

SET CONSTRAINTS v2.targets_last_compile_fkey, v2.targets_delivered_compile_fkey DEFERRED;
DELETE FROM v2.target_observations o USING v2.targets t WHERE o.target_id = t.id AND t.kind = 'windsurf_user';
DELETE FROM v2.compile_runs r USING v2.targets t WHERE r.target_id = t.id AND t.kind = 'windsurf_user';
DELETE FROM v2.targets WHERE kind = 'windsurf_user';
SET CONSTRAINTS v2.targets_last_compile_fkey, v2.targets_delivered_compile_fkey IMMEDIATE;

ALTER TABLE v2.targets DROP CONSTRAINT targets_kind_check;
ALTER TABLE v2.targets ADD CONSTRAINT targets_kind_check CHECK (kind IN
    ('agents_md', 'claude_md', 'cursor_mdc', 'chatgpt', 'gemini_md', 'copilot', 'windsurf', 'claude_rules',
     'claude_user', 'codex_user', 'gemini_user', 'copilot_user'));

ALTER TABLE v2.targets DROP CONSTRAINT targets_root_kind_check;
ALTER TABLE v2.targets ADD CONSTRAINT targets_root_kind_check CHECK (
    CASE root
    WHEN 'home' THEN kind IN ('agents_md', 'claude_user', 'codex_user', 'gemini_user', 'copilot_user')
                     AND delivery = 'local' AND v2.home_path_valid(kind, path)
    ELSE kind NOT IN ('claude_user', 'codex_user', 'gemini_user', 'copilot_user')
    END);

CREATE OR REPLACE FUNCTION v2.home_path_valid(k text, p text) RETURNS boolean
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
           WHEN 'gemini_user' THEN split_part(p, '/', 1) <> '.memax'
                AND p ~ '(^|/)\.gemini/AGENTS\.md$'
           WHEN 'copilot_user' THEN split_part(p, '/', 1) NOT IN ('.memax', '.github')
                AND p ~ '/copilot-instructions\.md$'
           ELSE false
           END
$$;
