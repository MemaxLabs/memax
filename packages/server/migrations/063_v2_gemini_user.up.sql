-- 063: v2_gemini_user
--
-- The personal layer's Gemini home file (personal-layer design §3.2, PR 9).
-- gemini_user is one Memax block in ~/.gemini/AGENTS.md. Antigravity reads
-- that file in every session, and Gemini CLI reads it once its
-- context.fileName lists AGENTS.md (memax connect gemini --agents-md).
-- Antigravity reads ~/.gemini/GEMINI.md too, and Gemini's own memory tool
-- writes there, so Memax writes the AGENTS.md beside it, never a block in
-- GEMINI.md.
--
-- The kind check and the home-kind check admit gemini_user, and
-- v2.home_path_valid takes it at .gemini/AGENTS.md, or in a .gemini folder
-- deeper in the home folder (GEMINI_CLI_HOME moves Gemini's home). The Go
-- mirror is ledger.checkHomePath (TestHomePathRulesMatchTheDatabase).

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
           ELSE false
           END
$$;

ALTER TABLE v2.targets DROP CONSTRAINT targets_kind_check;
ALTER TABLE v2.targets ADD CONSTRAINT targets_kind_check CHECK (kind IN
    ('agents_md', 'claude_md', 'cursor_mdc', 'chatgpt', 'gemini_md', 'copilot', 'windsurf', 'claude_rules',
     'claude_user', 'codex_user', 'gemini_user'));

ALTER TABLE v2.targets DROP CONSTRAINT targets_root_kind_check;
ALTER TABLE v2.targets ADD CONSTRAINT targets_root_kind_check CHECK (
    CASE root
    WHEN 'home' THEN kind IN ('agents_md', 'claude_user', 'codex_user', 'gemini_user')
                     AND delivery = 'local' AND v2.home_path_valid(kind, path)
    ELSE kind NOT IN ('claude_user', 'codex_user', 'gemini_user')
    END);
