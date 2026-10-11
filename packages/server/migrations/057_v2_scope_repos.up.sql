-- 057: v2_scope_repos
--
-- The repository scope for personal facts (personal-layer design §3.3).
-- A memory's scope (v2.memories.scope, jsonb) gains `repos`: the
-- repositories it applies in, as "owner/name" keys (ledger.RepoKey).
-- `memax init` sets it on what a Personal space imports from files that
-- belong to one project (that repository's Claude Code auto memory and
-- CLAUDE.local.md), and a home target (056) leaves such memories out of
-- the files it compiles: they would otherwise load in every project. They
-- stay live over MCP.
--
-- The scope is already a jsonb object (memories_scope_check), so `repos`
-- needs no column. internal/ledger normalises and bounds it on every write
-- (NewMemory.validate, ScopeMemory), the one write path; Forget already
-- clears the whole scope.
--
-- # Receipt vocabulary
--
-- receipts_action_check gains scoped: a memory's scope changed (ScopeMemory:
-- a person widening a memory to everywhere on its page, or Memax's
-- backfill, cmd/v2-backfill-repo-scope, setting the repositories of what
-- an earlier init imported without them). The receipt holds no words; its
-- reason names the repositories. The list keeps every verb 028 through 056
-- admit; a branch that adds verbs in parallel merges by taking the union
-- of both lists (TestReceiptEnumsMatchTheSchema holds the last migration
-- to the spec's ReceiptAction enum).

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
     'unlinked',
     'scoped'));
