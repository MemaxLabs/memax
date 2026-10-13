-- 064: v2_copilot_user
--
-- The personal layer's Copilot home file (personal-layer design §3.2,
-- PR 10). copilot_user is one Memax block in Copilot's user instructions,
-- ~/.copilot/copilot-instructions.md, or the same file under COPILOT_HOME.
-- Copilot CLI loads it in every session (copilot instruction list names
-- it), and VS Code reads it in its Copilot harness. Memax never writes a
-- file in ~/.copilot/instructions: a modular file there is the person's.
--
-- The kind check and the home-kind check admit copilot_user, and
-- v2.home_path_valid takes a copilot-instructions.md in a dot folder of the
-- home folder, never ~/.github's (a repository's file name) or ~/.memax's.
-- The Go mirror is ledger.checkHomePath (TestHomePathRulesMatchTheDatabase).

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
