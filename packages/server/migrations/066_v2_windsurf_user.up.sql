-- 066: v2_windsurf_user
--
-- The personal layer's Windsurf home file (personal-layer design §3.2,
-- PR 10). windsurf_user is one Memax block in Windsurf's global rules,
-- ~/.codeium/windsurf/memories/global_rules.md, which Cascade (Devin
-- Desktop, as Windsurf is called now) reads in every workspace, always on.
-- The file is limited to 6,000 characters with the person's own rules, so
-- its block defaults to a 4,000-byte budget (the ledger's
-- defaultSettingsAt) and the daemon writes nothing past the limit.
--
-- The kind check and the home-kind check admit windsurf_user, and
-- v2.home_path_valid takes exactly that one file. The Go mirror is
-- ledger.checkHomePath (TestHomePathRulesMatchTheDatabase).

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
           WHEN 'windsurf_user' THEN p = '.codeium/windsurf/memories/global_rules.md'
           ELSE false
           END
$$;

ALTER TABLE v2.targets DROP CONSTRAINT targets_kind_check;
ALTER TABLE v2.targets ADD CONSTRAINT targets_kind_check CHECK (kind IN
    ('agents_md', 'claude_md', 'cursor_mdc', 'chatgpt', 'gemini_md', 'copilot', 'windsurf', 'claude_rules',
     'claude_user', 'codex_user', 'gemini_user', 'copilot_user', 'windsurf_user'));

ALTER TABLE v2.targets DROP CONSTRAINT targets_root_kind_check;
ALTER TABLE v2.targets ADD CONSTRAINT targets_root_kind_check CHECK (
    CASE root
    WHEN 'home' THEN kind IN ('agents_md', 'claude_user', 'codex_user', 'gemini_user', 'copilot_user', 'windsurf_user')
                     AND delivery = 'local' AND v2.home_path_valid(kind, path)
    ELSE kind NOT IN ('claude_user', 'codex_user', 'gemini_user', 'copilot_user', 'windsurf_user')
    END);
