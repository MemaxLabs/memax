// SPDX-License-Identifier: Apache-2.0
//
// Generated from packages/server/openapi/v2.yaml by
// `pnpm --filter memax-sdk gen:v2`. Do not edit: change the spec and
// regenerate. `pnpm lint` fails when this file is stale.

export interface paths {
    "/v2/spaces": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List your spaces
         * @description Every space you belong to, with your role in it. Not paginated.
         */
        get: operations["listSpaces"];
        put?: never;
        /**
         * Create a project space
         * @description Creates a project space on the V2 record, owned by you: `memax init`
         *     does this for a repository that has no space yet. Without a `slug`,
         *     the name's slug is used, with a number added when it is taken; a
         *     slug you name must be free (409 `slug_taken`). Only a signed-in
         *     person creates spaces, and here only project spaces: your personal
         *     space exists already, and team spaces come with Team. A space isn't
         *     part of its own record, so creating one writes no receipt; its
         *     Activity starts with its first change. The same `Idempotency-Key`
         *     finds the space the first call created.
         */
        post: operations["createSpace"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/switch": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        /**
         * Where a space's Switch to V2 stands, with a preview
         * @description Where the space's Switch to V2 stands (`state`: `v1`, `running`,
         *     `switched`, `failed` or `off` once switched back), what each step
         *     did, and a fresh `preview` of what switching moves: the dry run.
         *     The preview is read from V1 and changes nothing: who the members are
         *     and the roles they keep (owner → owner, admin → member who can
         *     forget, contributor → member, viewer → viewer); how many V1 memories
         *     become notes, and of those, how many of a person's own are offered
         *     for bulk keep (`notes.candidates`), how many an agent wrote and
         *     Dream folds into proposals (`notes.fold`), and how many stay notes
         *     only; personas and agent files that become notes, and the compile
         *     targets the agent files stand for; the agents connected at Propose;
         *     decisions waiting on V1's board; V1 Dream runs, kept as read-only
         *     history; and the V1 plan, grandfathered. Any member may read it.
         */
        get: operations["getSpaceSwitch"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}:switch": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Switch a space to V2, or back to V1
         * @description Moves a space to the V2 record, so agents are served from it over
         *     MCP and its V1 content is ready for Review (plan 25 §10). Nothing is
         *     lost and no V1 row changes: every V1 memory becomes a note (N-),
         *     searchable by its owner and never compiled; a person's own short V1
         *     memories go up as one import (`import_id`, origin `v1`) through the
         *     import conflict check, so Review's "From V1" shows disagreements
         *     first and keeps the rest in one go; what agents wrote waits for
         *     Dream, which folds it into proposals; personas and agent files
         *     become notes, and the agent files' compile targets are added, with
         *     V1's two-way config sync off for those files while the space is on
         *     V2; the members' API keys and OAuth grants are connected at Propose,
         *     and each connected agent is told on its next MCP response; a
         *     decision waiting on V1's board moves to the record. Read the preview
         *     first: `GET /v2/spaces/{space}/switch`.
         *
         *     A space with nothing to import switches within the request (200).
         *     Otherwise the switch continues in the background (202): read it
         *     again until `state` is `switched`. A step that fails leaves it
         *     `failed` at that step; sending this again resumes it there. While it
         *     runs, or once it has switched, sending it again changes nothing.
         *     `kind` lets a V1 team hub switch as a project space, until it has a
         *     V2 record (409 `space_kind` otherwise); `repository` sets the
         *     repository it compiles for.
         *
         *     `to: v1` switches it back: every surface serves it as V1 again, and
         *     its V1 rows, which the switch never changed, answer as before. Its
         *     V2 record stays, for a later switch, which moves only what V1 gained
         *     meanwhile. Only the space's owner may, signed in.
         */
        post: operations["switchSpace"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/v1-dream-runs": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        /**
         * V1's Dream runs, as read-only history
         * @description The space's Dream runs from V1, newest first: when each ran and what
         *     it counted. Read-only edition history: V1's reports aren't served,
         *     and V1's actions can't be undone (plan 25 §10).
         */
        get: operations["listV1DreamRuns"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/notes": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        /**
         * Search a space's notes
         * @description Notes (N-) are a space's raw material: its V1 memories, personas and
         *     agent files, kept when it switched to V2, and what agents capture
         *     since. They are never compiled and never served as kept context.
         *     Notes are their owner's: you see the notes you wrote and, in a space
         *     you own, every note in it. With `q`, the best matches first (by V1's
         *     own index of the words); without, the newest.
         */
        get: operations["searchNotes"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/notes/{note}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
                /** @description A note's display ID (N-0042) or id. */
                note: components["parameters"]["NotePath"];
            };
            cookie?: never;
        };
        /**
         * Read a note
         * @description A note you may read (yours, or any in a space you own), by its N- ref or id.
         */
        get: operations["getNote"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/notes/{note}/forget-preview": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
                /** @description A note's display ID (N-0042) or id. */
                note: components["parameters"]["NotePath"];
            };
            cookie?: never;
        };
        /**
         * What forgetting a note would do
         * @description The memories that carry the note's words and go with it, and whether you may forget it.
         */
        get: operations["previewForgetNote"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/notes/{note}:forget": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
                /** @description A note's display ID (N-0042) or id. */
                note: components["parameters"]["NotePath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Forget a note everywhere
         * @description Forgets a note (rule 7). In one transaction its words leave the V1
         *     row that holds them, as V1's own delete takes it (a memory with its
         *     chunks, attachments and topic links; a persona; an agent file), with
         *     what V1 derived from it that names it (board cards citing it,
         *     notifications about it, Dream's reasons, the activity summary of its
         *     title). Memories carrying its words go with it: the proposal the
         *     switch made of it and Dream's proposals citing it. Name them in
         *     `carries`; otherwise the answer is 409 `forget_carries` with
         *     `details.carries`, and nothing changes. A tombstone stays, its
         *     attachments' stored objects are deleted, and every agent connected
         *     to the space is told on its next read. It can't be undone. Only a
         *     person who may forget in the space forgets (owners, by default).
         */
        post: operations["forgetNote"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}:export": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Export a space's whole record
         * @description The space's whole record as a zip archive in the export format
         *     (`memax.export.v1`; see the docs' Export page), one folder named by
         *     the space's slug: every memory as Markdown with YAML frontmatter
         *     (its statement as the body; state, sources with their `file:line`,
         *     versions, decision fields, conditions, links and receipts in the
         *     frontmatter), a tombstone without words for every forgotten memory,
         *     every Brief version as it reads, the decision gates, the targets
         *     with their latest compile, the agents, read counts, every receipt in
         *     chain order with its canonical fields (`receipts.jsonl`), every
         *     signed checkpoint with the public keys (`checkpoints.json`), and a
         *     manifest with every file's SHA-256 (`export.json`).
         *     `memax verify-export` (or the SDK's `verifyExport`) checks it.
         *
         *     Any person who may read the space exports it, on every plan; agents
         *     and API keys are refused with policy `export_by_person`. An export
         *     changes nothing, and is counted by one receipt (`exported`, on the
         *     space, by you), written before the record is read, so the export
         *     lists it. It records no reads. A retry with the same
         *     `Idempotency-Key` writes no new receipt and, if nothing else changed
         *     meanwhile, returns the same bytes. Exports are rate-limited per
         *     person (429 `rate_limited`, with `Retry-After`).
         *
         *     The archive streams as it is read, from one consistent snapshot of
         *     the record. A failure after the first byte ends the response early,
         *     so the archive is incomplete and can't be opened; export again.
         */
        post: operations["exportSpace"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/memories": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        /**
         * List memories
         * @description The space's memories, newest first. Without `state`, every state
         *     except `rejected` is listed. Sources are not included; read one
         *     memory for its sources.
         */
        get: operations["listMemories"];
        put?: never;
        /**
         * Remember a statement
         * @description Writes one statement to the space. Policy decides what happens: a
         *     member or owner keeps it (`outcome: applied`); a viewer, an agent or
         *     an API key proposes it (`outcome: proposed`), and it waits in Review.
         *     An agent's statement that cites an external source is always
         *     proposed and quarantined (`policy.quarantine`). A statement that
         *     contains a credential is refused.
         */
        post: operations["rememberMemory"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/memories:near-duplicates": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Find what a draft repeats
         * @description Remember's near-duplicate check: the kept memories and pending
         *     proposals of the space that a draft statement repeats, best first,
         *     so a person can keep an agent's proposal instead of writing the
         *     same thing twice. `exact` is the same words (case, spacing and
         *     punctuation aside); `near` is at least `floor` similar by embedding
         *     (cosine). Superseded decisions are left out.
         *
         *     No model runs on this path. The draft is embedded (within about
         *     120 ms) and compared exactly with the space's stored embeddings,
         *     so it answers in under 150 ms. When embeddings are off on this
         *     server, or the draft's embedding misses its deadline, only exact
         *     repeats are checked and `semantic` is `false`.
         *
         *     It only reads, so any credential that reads the space may call it.
         *     It is a `POST` so the draft travels in the body, never in a URL or
         *     an access log, and it takes no `Idempotency-Key`. Clients call it
         *     while a person types (debounced), and it is rate-limited per
         *     caller: 429 `rate_limited` with `Retry-After`.
         */
        post: operations["findNearDuplicates"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/memories:keep": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Keep proposals in bulk
         * @description Keeps several proposals of the space, each as its own Keep: its own
         *     receipt, by Keep's rules, so one that can't be kept (in conflict,
         *     quarantined outside the web, a decision where decisions need a
         *     person on the web, not judged yet) is reported and the rest are
         *     kept. ReviewImport's "Keep 30" and `memax init`'s "keep the ones
         *     that agree" send the proposals the person chose. Each item's
         *     `version` is the version the person saw, as `If-Match` is for one
         *     Keep. Retrying with the same `Idempotency-Key` keeps nothing twice.
         *     Every target recompiles once the keeps land.
         */
        post: operations["keepMemories"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/memories:reject": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Reject proposals in bulk
         * @description Rejects several proposals of the space, each as its own Reject with
         *     its own receipt, by Reject's rules; one that can't be rejected is
         *     reported and the rest are. Retrying with the same `Idempotency-Key`
         *     rejects nothing twice.
         */
        post: operations["rejectMemories"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/ask": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Ask the space a question
         * @description ⌘K Ask: a short answer to a person's question from the space's kept
         *     memories, streamed as server-sent events, every sentence cited to
         *     the memories it rests on.
         *
         *     The search is the one recall and search use: the space's kept
         *     memories and decisions in force, never a superseded decision,
         *     never another space's memory. Quarantined memories (trust
         *     `external`) are left out of answers; search and Memories still
         *     show them. The answer tier's model is given the best matches and
         *     answers only from them, in at most three sentences. A citation of
         *     anything it wasn't given is removed before it is sent (`dropped`
         *     counts them). When the memories don't answer the question, or none
         *     matched, the stream ends with outcome `not_covered` and no words;
         *     an answer left with no citation ends `unsupported`, and clients
         *     don't show it as an answer. When answers are off on the server,
         *     the stream is the matching memories alone (`answering: false`,
         *     outcome `sources_only`).
         *
         *     The stream, in order: one `sources` event; then `delta` (words,
         *     citations removed) and `cite` events interleaved as the answer
         *     arrives (`cite.n` numbers the cited memories in the order they're
         *     first cited); then `done`, or `error` if the model failed partway.
         *     Cancelling the request (closing the connection) stops the model.
         *
         *     Only a signed-in person who belongs to the space may ask; an agent
         *     or API key is refused with policy `ask_by_person` (agents read the
         *     record over MCP). It only reads: it writes no record row and no
         *     receipt, and takes no `Idempotency-Key`. Each answer the model
         *     writes counts toward the person's asks this month, and past the
         *     plan's limit the request is refused with policy `ask_limit`
         *     (`details.limit`, `details.current`, `details.reset_at`). Asks that
         *     never reach the model (nothing matched, answers off) don't count.
         *     To keep an answer, remember it with the cited memories as sources
         *     of kind `memory`.
         */
        post: operations["askSpace"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/review": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        /**
         * List what needs a person
         * @description Review's queue: every proposal, then kept memories flagged as a
         *     conflict, then kept memories flagged as stale. Within each group the
         *     oldest comes first, so the queue drains in the order things arrived.
         *     `total` counts the whole queue, not just this page.
         */
        get: operations["listReview"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/receipts": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        /**
         * List receipts
         * @description The space's Activity: every receipt, newest first. Receipts name the
         *     actor, the action and the object; they never hold the memory's words.
         *     Pass `memory` for one memory's history.
         */
        get: operations["listReceipts"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/checkpoints": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        /**
         * List receipt checkpoints
         * @description The space's sealed receipt chain, newest checkpoint first: each
         *     checkpoint's range of receipts (positions in the chain, from 1), the
         *     chain hash before and after it, the Merkle root of its receipts and
         *     its signature. `seal` says how far the chain is sealed ("sealed
         *     through receipt 1,284 at 14:02"), how many receipts wait to be sealed
         *     and when the chain was last verified; `keys` are the public keys
         *     checkpoints are signed with, current and retired.
         */
        get: operations["listCheckpoints"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/reads": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        /**
         * List reads
         * @description What agents read in the space, newest first: one read (`R-`) per
         *     recall, search, get, list or digest that returned something here, and
         *     one per session-start compile load. `reads_7d` counts the last 7
         *     days. Reads hold memory refs and compile refs, never words.
         */
        get: operations["listReads"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/tombstones": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        /**
         * List what was forgotten
         * @description The space's tombstones, newest first: each forgotten memory (and
         *     each Forget of the whole space), who asked, when, what went with it,
         *     and whether its propagation is done. Read one with
         *     `GET /v2/memories/{ref}/tombstone` for its steps.
         */
        get: operations["listTombstones"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/compile-loads": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Report a compile an agent loaded
         * @description A session-start hook (or the daemon) reports that its agent loaded a
         *     compiled file natively: the compile run (`C-`) it found in the file.
         *     It counts as a read of every fact in that compile, and it is how
         *     Memax knows a file's loads are observed (a fact in a file whose loads
         *     nobody reports never fades). Reading is enough: an agent connected to
         *     the space reports its own loads (a paused one too); an agent that
         *     isn't connected here is refused with 403 `refused`
         *     (`agent_not_connected`), so a credential can't count reads in a space
         *     it can't read. A person's session (the CLI the hook runs under) names
         *     the agent. Report a load within a day of it (`loaded_at`); the same
         *     `Idempotency-Key` records it once.
         */
        post: operations["recordCompileLoad"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/imports": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        /**
         * List imports
         * @description The space's imports, newest first, each with its counts.
         */
        get: operations["listImports"];
        put?: never;
        /**
         * Import statements from agent files
         * @description Uploads what agents already know, read from their files (`memax
         *     init`): statements, each with its source `file:line`, plus what the
         *     client kept on the machine and why (a ref and a rule, never the
         *     words). Each statement is written as its own proposal, with its own
         *     receipt (`via: import`), and is always a proposal, whoever sends it:
         *     nothing an import brings is kept until a person keeps it.
         *
         *     Statements of the upload that repeat each other (the same words, or
         *     a near-verbatim repeat) become one proposal citing every file that
         *     says it (`folded`). A statement the space already has, in any state
         *     but forgotten, is skipped (`existing`), so running `memax init` again
         *     proposes only what is new. A statement policy refuses (a credential
         *     the client missed) is `refused`. A repository file's statements are
         *     held to the repository class whatever the request says; lines only
         *     on another branch arrive `external` and are quarantined.
         *
         *     The judge then looks at each proposal, and the import's conflict
         *     check groups the proposals that disagree (`conflicts` on the
         *     import), within seconds; read the import to follow it.
         *
         *     A retry with the same `Idempotency-Key` resumes the same import and
         *     writes nothing twice. At most 500 statements; split more into
         *     several imports.
         */
        post: operations["createImport"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/imports/{import}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
                /** @description The import's id. */
                import: components["parameters"]["ImportPath"];
            };
            cookie?: never;
        };
        /**
         * Read an import
         * @description One import in full: its files, what the client kept on the machine,
         *     what became of every statement, the memories they became (with the
         *     judge's verdicts) and the disagreements found (Cleanup, ReviewImport).
         *     A proposal can be kept in bulk (`bulk`) when it waits in Review, the
         *     judge and the conflict check have looked at it and found nothing,
         *     it cites no outside source and its line had no hidden characters;
         *     `held` says why one can't. `progress.ready` is set once nothing is
         *     being judged and the conflict check is done.
         */
        get: operations["getImport"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/imports/{import}/conflicts/{n}:settle": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
                /** @description The import's id. */
                import: components["parameters"]["ImportPath"];
                /** @description The disagreement's number within the import (its `n`, from 1). */
                n: components["parameters"]["ConflictNumber"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Settle an import's disagreement
         * @description Settles one of an import's disagreements, once, as a group:
         *     `keep_one` keeps `keep` and rejects the rest; `keep_all` keeps them
         *     all (they don't disagree after all); `leave_open` keeps them as open
         *     questions, so agents read that it isn't decided; `keep_suggestion`
         *     keeps one new statement (the check's `suggestion`, or your
         *     `statement`), citing every member's sources, and rejects the members.
         *     The group's flags and links end, and every change has its receipt.
         *     It follows Keep's rules: a person who may keep, on the web where the
         *     space needs one for decisions, and on the web to keep a quarantined
         *     statement. A member that also contradicts a decision in force can't
         *     be kept until that is settled (409 `in_conflict`). The members of an
         *     open import disagreement can't be settled two at a time with
         *     `:resolve-conflict`.
         */
        post: operations["settleImportConflict"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/memories/{ref}": {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        /**
         * Read one memory
         * @description One memory with its sources, every version of its statement (newest
         *     first) and its latest receipts. `receipts.next_cursor` continues on
         *     `GET /v2/spaces/{space}/receipts?memory={id}`.
         */
        get: operations["getMemory"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/memories/{ref}:keep": {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Keep a proposal
         * @description Keeps a proposal. Only a person who is a member or owner can keep
         *     (per the space's rules); agents and API keys are refused. Send
         *     `If-Match` with the version you reviewed. A proposal the judge has
         *     flagged as a conflict can't be kept until it is settled: 409
         *     `in_conflict`, whose `details.ref` is the decision in force in the
         *     way (settle it with `:resolve-conflict`). One that touches a
         *     decision in force can't be kept before the judge has looked at it
         *     (about 5 s, at most 30 s): until then Keep answers 503
         *     `judge_pending` with `Retry-After`, and the same Keep, with the same
         *     `Idempotency-Key`, goes through once it has. (503 `busy` is another
         *     change holding the memory.)
         */
        post: operations["keepMemory"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/memories/{ref}:edit": {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Edit a memory
         * @description Writes a new version of the statement, and optionally moves it to
         *     another section. `If-Match` is required. When policy sends the edit
         *     to Review (an agent editing what a person kept, for example), the
         *     result is a new proposal that supersedes the memory, and the memory
         *     itself is unchanged. `keep: true` keeps a proposal after editing it,
         *     except when a person's new words touch a decision in force and the
         *     judge hasn't seen them (rule 11): then the edit is saved as the
         *     proposal's new version, judged like any proposal's, and not kept.
         *     The result is `outcome: proposed` with policy code `judge_pending`
         *     and the new `version`; keep it with `:keep` on that version, which
         *     answers 503 `judge_pending` with `Retry-After` until the judge has
         *     looked, and then keeps it (or 409 `in_conflict` if the judge flagged
         *     it). This 200 sets no `Retry-After`: the judge may answer within
         *     milliseconds, and the Keep's 503 says exactly how long to wait. The
         *     saved edit is undoable as an edit. A flagged proposal can't be kept
         *     this way either (409 `in_conflict`).
         */
        post: operations["editMemory"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/memories/{ref}:reject": {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Reject a proposal
         * @description Rejects a proposal. Only a person who is a member or owner can
         *     reject. `reason` goes into the receipt.
         */
        post: operations["rejectMemory"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/memories/{ref}/conflict": {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        /**
         * Compare a conflict
         * @description ReviewConflict: both sides of one of this memory's conflicts (the
         *     flagged memory and the decision in force it contradicts), each with
         *     its sources, links and the judge's verdict; their latest receipts;
         *     and the four answers with what each does and whether you may take
         *     it. When the judge found the conflict, it also wrote a short
         *     `question`, a `label` per answer and, when the sources settle it, a
         *     `suggested` answer. Pass `with` when the memory has more than one
         *     conflict. A memory with no conflict is 409 `invalid_transition`.
         */
        get: operations["getConflict"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/memories/{ref}:resolve-conflict": {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Settle a conflict
         * @description One answer must win (rule 11). Relative to this memory:
         *     `keep_this` keeps it and the other side gives way, `keep_other` the
         *     reverse (a proposal that gives way is rejected; a kept decision is
         *     superseded and stops compiling; a kept fact fades), `keep_both`
         *     keeps both, usually with narrower words (`statement`,
         *     `other_statement`), and `leave_open` makes it an open question,
         *     setting a decision in force on either side to open. The flag is
         *     cleared, every change has its receipt, and the whole resolution can
         *     be undone. Only a person who may keep can settle a conflict, on the
         *     web where the space needs one for decisions.
         *
         *     Rule 11 for `keep_both`: narrower words that touch a decision in
         *     force other than the two sides wait for the judge. The resolution
         *     is then saved, not applied: a proposal's words become its new
         *     version (in `memories`), a kept memory's become a draft (its
         *     `drafted` receipt), out of force until the resolution applies, and
         *     the conflict stays open. The answer is 200 `outcome: proposed` with
         *     policy code `judge_pending`, and no `Retry-After` (the judge may
         *     answer within milliseconds). Send the same resolution again, with a
         *     new `Idempotency-Key` and the version in `memories`: it answers 503
         *     `judge_pending` with `Retry-After` until the judge has looked (about
         *     5 s, at most 30 s; after that it goes ahead, so a judge that's down
         *     never blocks it), then applies, or answers 409 `in_conflict` when
         *     the judge found the words contradict a decision in force
         *     (`details.ref`). Words that touch no other decision apply at once.
         *     `keep_this`, `keep_other` and `leave_open` keep a proposal's words as
         *     they stand, and wait the same way: words the judge hasn't seen yet
         *     (an edit, or narrower words a `keep_both` saved) that touch another
         *     decision in force answer 503 `judge_pending` until it has, then
         *     apply or answer 409 `in_conflict`. Keeping a side that is flagged
         *     against another decision too is 409 `in_conflict` naming it: settle
         *     that first.
         */
        post: operations["resolveConflict"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/memories/{ref}:forget": {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Forget a memory everywhere
         * @description Forgets a memory (rule 7). In one transaction its words leave the
         *     record: every version's statement, its sources' quotes and
         *     locators, its decision fields, its search entry and embeddings, the
         *     reasons on its receipts, the judge's words about it on both sides of
         *     every pair, a gate's question when it is the decision the gate
         *     became, and every Brief line that cited it (the Brief gets a new
         *     version). A tombstone stays. Every target that held it recompiles,
         *     the stored copies of older compiles are re-rendered without it, the
         *     caches are purged and every agent that read it (or is connected to
         *     the space) is told on its next response; the tombstone's steps say
         *     how far that got. It can't be undone.
         *
         *     Only a person who may keep and forget in the space forgets (owners,
         *     by default; see the space's rules). A decision in a space whose
         *     decisions need a person on the web needs the web (D15). Agents and
         *     API keys are refused: an agent asks with `:request-forget`.
         *
         *     `If-Match` is required: the version you saw. Memories that carry its
         *     words go with it (proposals folded into it or that would change it,
         *     and memories citing it as a source, such as a kept Ask answer).
         *     Name them in `carries`; when the list doesn't match, the answer is
         *     409 `forget_carries` with `details.carries`, and nothing changes.
         */
        post: operations["forgetMemory"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/memories/{ref}:request-forget": {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Ask a person to forget a memory
         * @description An agent's memax_forget on the V2 record: it forgets nothing and
         *     records a request (receipted, with `reason`) that a person forgets
         *     the memory, or keeps it, on the web. Asking again while one waits is
         *     the same request. Agents connected at Propose or Write, and API keys
         *     that propose, may ask; people forget it themselves (policy
         *     `forget_by_person`).
         */
        post: operations["requestForget"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/memories/{ref}:decline-forget": {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Keep a memory an agent asked to forget
         * @description A person who may forget it keeps it instead: every waiting request
         *     to forget it is declined, with a `forget_declined` receipt. 409
         *     `invalid_transition` when nobody asked.
         */
        post: operations["declineForget"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/memories/{ref}/forget-preview": {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        /**
         * What a Forget would do
         * @description Read before anyone confirms a Forget: the memories that would go with
         *     it (send their refs as `carries`), the compiled files and copy-outs
         *     that hold it and would be rewritten, how many agents would be told
         *     (`readers` of them read it; the rest are connected to the space),
         *     the version to send as `If-Match`, and whether you may (`allowed`,
         *     with `policy` when not). It changes nothing. 409 `invalid_transition`
         *     when it is forgotten already.
         */
        get: operations["previewForget"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/memories/{ref}/tombstone": {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        /**
         * Read a forgotten memory's tombstone
         * @description What Forget did: who asked and when, what went with it, each step of
         *     how it was forgotten (removed from Memax, each file rewritten, each
         *     agent told) with where it stands now, and the copies Memax can't
         *     reach (`unreachable`: git history of committed files, agents' own
         *     memories, backups and their window, the model providers that saw
         *     the words, files with a hand edit Memax won't write over, copies a
         *     person pasted elsewhere). Never words. 404 when it isn't forgotten.
         */
        get: operations["getTombstone"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/receipts/{receipt}:undo": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description A receipt's id. */
                receipt: components["parameters"]["ReceiptPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Undo a decision
         * @description Undoes the command that wrote this receipt (any of its receipts):
         *     Review's ⌘Z. A person undoes their own keep, reject, edit or
         *     conflict resolution within 10 minutes, and their own Remember
         *     (undoing it withdraws the memory: it ends rejected, and leaves the
         *     compiled files); any person who may keep undoes one of the judge's
         *     folds within 14 days. Each memory gets an
         *     `undid` receipt whose `source` names the receipt undone
         *     (`{"kind": "receipt", "ref": <id>}`), and targets recompile when the
         *     kept set changes. Refused with 409 `undo_refused` when the window
         *     has passed (`window_passed`), it was already undone
         *     (`already_undone`), a later change depends on it (`later_changes`,
         *     `details.ref` names what is in the way), or the receipt's command
         *     can't be undone (`not_undoable`). Forget can never be undone, and
         *     neither can the judge's `returned` (a Write-level agent's write put
         *     back in Review for contradicting a decision in force): settle that
         *     conflict instead.
         */
        post: operations["undoReceipt"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/agents": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * List your agents
         * @description Your agent connections, oldest first: each one's autonomy in every
         *     space of yours it is connected to, when it was last seen, and its
         *     writes in the last 7 days. Disconnected agents are left out. With an
         *     agent's own credential, only that agent is listed. Not paginated.
         */
        get: operations["listAgents"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/agents": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        /**
         * List the agents in a space
         * @description Every agent connected to the space, whoever it works for, oldest
         *     first, with its autonomy there and its writes there in the last 7
         *     days. Of other people's agents you see only the spaces you share.
         *     Disconnected agents are left out. Not paginated.
         */
        get: operations["listSpaceAgents"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/agents/{agent}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The agent connection's id. */
                agent: components["parameters"]["AgentPath"];
            };
            cookie?: never;
        };
        /**
         * Read one agent
         * @description One agent connection with what it did this week, its latest writes
         *     (receipts, which never hold the words) and its latest sessions, in
         *     your spaces. Disconnected agents can still be read.
         */
        get: operations["getAgent"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/agents/{agent}/spaces/{space}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The agent connection's id. */
                agent: components["parameters"]["AgentPath"];
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        /**
         * Set what an agent may do in a space
         * @description Sets the agent's autonomy in the space, connecting it there if it
         *     isn't yet. Only a person changes what an agent may do, never an
         *     agent. Lowering is always allowed for your own agent, and a space's
         *     owners may lower anyone's agent there. Raising needs you on the web
         *     (assurance `human_web`), so an agent driving the CLI with your login
         *     can't raise itself; connecting at no more than the space's default
         *     (Propose unless its rules say otherwise) is the exception. Write
         *     needs you to be able to keep in the space, and an API key's agent
         *     proposes at most. The same level again writes nothing.
         */
        patch: operations["setAgentAutonomy"];
        trace?: never;
    };
    "/v2/agents/{agent}:pause": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The agent connection's id. */
                agent: components["parameters"]["AgentPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Pause an agent
         * @description Stops the agent writing anywhere until it is resumed; it can still
         *     read. Only the person it works for can pause it. The receipt goes to
         *     every space it is connected to.
         */
        post: operations["pauseAgent"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/agents/{agent}:resume": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The agent connection's id. */
                agent: components["parameters"]["AgentPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Resume a paused agent
         * @description Lets a paused agent write again, at the autonomy it had. Resuming is
         *     raising, so it needs you on the web (assurance `human_web`): an agent
         *     can't resume itself.
         */
        post: operations["resumeAgent"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/agents/{agent}:disconnect": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The agent connection's id. */
                agent: components["parameters"]["AgentPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Disconnect an agent
         * @description Ends the connection for good and revokes its credential (the API key
         *     or OAuth grant) in the same transaction, so the agent stops working
         *     at once. Only the person it works for can disconnect it. To use the
         *     agent again, connect it again.
         */
        post: operations["disconnectAgent"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/brief": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        /**
         * Read the Brief
         * @description The space's current Brief version, with the receipt of whoever wrote
         *     it. A memory item is a display ID; render its statement from the
         *     memory itself. 404 when the space has no Brief yet.
         */
        get: operations["getBrief"];
        put?: never;
        /**
         * Revise the Brief
         * @description Writes a new version (`B-`) of the space's Brief: ordered sections of
         *     memory items and cited prose. Every memory item must be a kept
         *     memory of the space; every prose line must cite at least one kept
         *     memory, and nothing forgotten or rejected. Send `If-Match` with the
         *     version you started from; it is required once the space has a
         *     Brief. People who may keep revise the Brief; agents can't. Every
         *     target of the space recompiles.
         */
        post: operations["reviseBrief"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/brief/versions": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        /**
         * List the Brief's versions
         * @description Every version of the space's Brief, newest first, each with the
         *     receipt of whoever wrote it and why (BriefHistory). Restore one with
         *     `:restore`.
         */
        get: operations["listBriefVersions"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/brief/versions/{n}:restore": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
                /** @description The Brief version's number (its `version`, from 1). */
                n: components["parameters"]["BriefVersionNumber"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Restore an older version of the Brief
         * @description Writes version `n` back as a new version (`B-`) of the Brief, with a
         *     `revised` receipt whose `source` is the version restored
         *     (`{kind: brief, ref: B-0040}`). The record has moved on since, so
         *     it keeps only what can still stand, and `dropped` says what it left
         *     out: a memory line whose memory isn't kept any more, a line of prose
         *     whose words were forgotten, that cites a forgotten memory or that no
         *     longer cites a kept one, and (kind `cite`) a citation of a rejected
         *     memory taken off a line that stays. A line of prose that cites
         *     nothing is refused (400), as Dream's own changes are. Send `If-Match`
         *     with the version in force you started from; it is required. The
         *     version in force can't be restored. Whoever may revise the Brief may
         *     restore it, and every target of the space recompiles. A retry with
         *     the same `Idempotency-Key` returns the same version, and works
         *     `dropped` out again from version `n`.
         */
        post: operations["restoreBriefVersion"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/targets": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        /**
         * List where the space compiles to
         * @description The space's targets (AGENTS.md, the CLAUDE.md shim, scoped Cursor
         *     rules, the ChatGPT copy-out, …), each with its sync state, its latest
         *     compile run and what is on disk. Not paginated.
         */
        get: operations["listTargets"];
        put?: never;
        /**
         * Add a target
         * @description Adds a compile target to the space, with the kind's defaults for
         *     whatever you leave out (path, settings, delivery), and compiles it.
         *     The space needs a Brief first. People who may keep add targets.
         */
        post: operations["createTarget"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/targets/{target}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        /**
         * Change a target
         * @description Changes a target's path, settings or delivery, or stops and restarts
         *     compiling it (`enabled`), and recompiles it. Send `If-Match` with
         *     the target's version to be sure you change what you saw. A change
         *     that changes nothing writes nothing and has no receipts.
         */
        patch: operations["configureTarget"];
        trace?: never;
    };
    "/v2/targets/{target}:compile": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Compile a target now
         * @description Asks for a fresh compile of the target (Compile now). It is queued,
         *     so the answer is 202 with the target compiling; follow
         *     `last_compile` on the target, or the runs. A stopped target is 409.
         */
        post: operations["compileTarget"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/targets/{target}/preview": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        /**
         * Read what a target compiled to
         * @description The latest good compile's content (TargetPreview): every file it
         *     writes, or the copy-out text, with the run's metadata. Before the
         *     first compile, `compile` is absent and there are no files. 503 when
         *     the server has no compile service.
         */
        get: operations["getTargetPreview"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/targets/{target}/runs": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        /**
         * List a target's compile runs
         * @description Every compile run (`C-`) of the target, newest first, failed ones included.
         */
        get: operations["listCompileRuns"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/targets/{target}/observations": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Report a target's file as seen
         * @description A device (the local daemon) reports a file the target writes, as it
         *     is on disk. When it matches what Memax delivered (line endings and a
         *     BOM aside), nothing is recorded: 200 with `drifted: false`. Otherwise
         *     it is a hand edit: the content is kept, the compiler reads it back
         *     as changes, and the target is drifted until a person pulls,
         *     overwrites or stops it: 201 with the observation.
         */
        post: operations["recordObservation"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/targets/{target}/deliveries": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Acknowledge a delivery
         * @description A device acknowledges it wrote a compile run's output: `sha256` is
         *     the run's drift hash, computed from what was written (for one file,
         *     the compiler's drift_sha256 of that file; for several, the sha256
         *     of `<path> NUL <drift_sha256> LF` lines sorted by path). A target is
         *     in sync once its latest run is delivered. Acknowledging the same run
         *     again, or an older one, changes nothing. While a pull holds one of
         *     the run's files (the target is `held`), nothing may be written over
         *     it: 409.
         */
        post: operations["recordDelivery"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/targets/{target}/drift": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        /**
         * Read a target's hand edits
         * @description Each open hand edit of the target (DriftResolve): the file as Memax
         *     wrote it, the file as it is now, and what the edit says, change by
         *     change. No open edits is an empty list. 503 when the server has no
         *     compile service.
         */
        get: operations["getDrift"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/targets/{target}/drift:pull": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Pull a hand edit back as proposals
         * @description Turns each change of the target's open hand edits into a proposal,
         *     written by the person on the device (or by the repository, as
         *     external, when nobody is known), with a `file:line` source: a changed
         *     cited line proposes an edit of that memory, a new line a new memory.
         *     A removed cited line waits in the resolution for a person to forget
         *     or exclude the memory; it is never forgotten automatically. Any
         *     person in the space may pull.
         *
         *     The edited file stays as it is until its proposals are decided:
         *     while any of them is still proposed, the target is `held` and
         *     nothing is delivered over the file (see `holds` on the target).
         *     Once each is kept or rejected, the latest compile is delivered over
         *     it: kept lines come back compiled, rejected ones go. A pull that
         *     writes no proposal holds nothing; the file stays until the next
         *     compile delivers over it.
         */
        post: operations["pullDrift"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/targets/{target}/drift:overwrite": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Overwrite a hand edit
         * @description Lets Memax write the compiled file over the target's open hand
         *     edits: the target recompiles and is delivered again. The edit stays
         *     in the observation, so a copy remains in Activity. People who may
         *     keep overwrite.
         */
        post: operations["overwriteDrift"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/targets/{target}/drift:stop": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Stop compiling a hand-edited target
         * @description Stops compiling the target (`off`) and closes its open hand edits;
         *     the file stays as it is, and agents keep reading the space over MCP.
         *     Turn it back on with `enabled: true`. People who may keep stop
         *     targets.
         */
        post: operations["stopDrift"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/gates": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        /**
         * List decision gates
         * @description The space's decision gates, newest first. Without `status`, every
         *     status is listed. `waiting` are the ones an agent is waiting on (a
         *     waiting gate past its `expires_at` is `expired`).
         */
        get: operations["listGates"];
        put?: never;
        /**
         * Ask a person to decide
         * @description An agent asks a question with two to four options, and the gate waits
         *     for a person's answer. Only an agent connected to the space at
         *     Propose or Write asks (an API key may: it proposes); a read-only or
         *     paused one only reads, and a person remembers the decision instead
         *     (403 `refused`). One agent has at most three decisions waiting in a
         *     space (`gate_limit`). A question, its context or an option that
         *     contains a credential is refused.
         */
        post: operations["requestDecision"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/gates/{ref}": {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A gate's display ID (G-0012, with `?space=`) or its id. */
                ref: components["parameters"]["GateRefPath"];
            };
            cookie?: never;
        };
        /**
         * Read one decision gate
         * @description The question, its options, and how it ended, if it has.
         */
        get: operations["getGate"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/gates/{ref}:answer": {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A gate's display ID (G-0012, with `?space=`) or its id. */
                ref: components["parameters"]["GateRefPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Answer a decision gate
         * @description A person answers with one of the gate's options, and the answer is
         *     kept, in the same command, as a decision they authored: `memory` in
         *     the result, with a `kept` receipt, beside the gate's `answered`
         *     receipt. It follows Keep's rules for a decision: only members and
         *     owners (per the space's rules) answer, never an agent or an API key,
         *     and where the space's decisions need a person on the web
         *     (`gate.needs_web`) only a request from the web app answers; anything
         *     else is refused (`decision_needs_web`) and the gate keeps waiting. A
         *     gate answered, withdrawn or expired already is 409
         *     `invalid_transition`, with `details.status`.
         */
        post: operations["answerGate"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/gates/{ref}:withdraw": {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A gate's display ID (G-0012, with `?space=`) or its id. */
                ref: components["parameters"]["GateRefPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Withdraw a decision gate
         * @description Takes a waiting gate's question back. The agent that asked may (and
         *     then it knows already), and so may the person it works for and
         *     anyone who could answer it (`not_your_gate` otherwise). A gate that
         *     isn't waiting is 409 `invalid_transition`, with `details.status`.
         */
        post: operations["withdrawGate"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/dream/editions": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        /**
         * List Dream's editions
         * @description The space's Dream editions (`D-`), newest first, each with what it
         *     read and the counts of what it did, and when the next one is due
         *     (`schedule`, in the owner's local night). An edition is published
         *     only when the space had something new since the last one: no input,
         *     no run. A person's `X-Timezone` is how Dream learns their local
         *     night, unless they set one in the Dream settings.
         */
        get: operations["listEditions"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/dream/editions/{edition}": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
                /** @description An edition's display ID (D-0214), its number (214), its id, or `latest`. */
                edition: components["parameters"]["EditionPath"];
            };
            cookie?: never;
        };
        /**
         * Read one edition
         * @description One edition (`D-0214`, `214` or `latest`) with every action it took
         *     and what it found that needs a person (`surfaced`). Each action
         *     carries the memory it is about as that memory is now, its words read
         *     live: one forgotten since shows as forgotten, with no words.
         */
        get: operations["getEdition"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/dream/editions/{edition}/actions": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
                /** @description An edition's display ID (D-0214), its number (214), its id, or `latest`. */
                edition: components["parameters"]["EditionPath"];
            };
            cookie?: never;
        };
        /**
         * List an edition's actions
         * @description An edition's actions in the order it took them, of one kind or all.
         */
        get: operations["listDreamActions"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/dream/editions/{edition}:undo": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
                /** @description An edition's display ID (D-0214), its number (214), its id, or `latest`. */
                edition: components["parameters"]["EditionPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Undo an edition's actions of one kind
         * @description Undoes every undoable action of the edition of one kind (the
         *     edition's "Undo both" and "Restore all"), each as its own command
         *     with its own receipts, exactly as `POST /v2/dream/actions/{action}:undo`
         *     would. What can't be undone is listed in `refused` with Undo's
         *     reason; the rest goes ahead.
         */
        post: operations["undoEdition"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/spaces/{space}/dream:run": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Run Dream now
         * @description Asks Dream to run on the space now, outside its night. Only the
         *     space's owner may (`dream_run_by_owner`), a few times a day on Pro
         *     and once a week on Free (429 `rate_limited`, with `Retry-After`).
         *     The run is queued; it publishes an edition only if the space has
         *     something new since the last one.
         */
        post: operations["runDream"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/dream/actions/{action}:undo": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description One of Dream's actions, by id. */
                action: components["parameters"]["DreamActionPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Undo one of Dream's actions
         * @description Applies the action's inverse as a ledger command: the state before
         *     the action comes back exactly (folded notes unlinked, a proposal
         *     withdrawn, a folded duplicate back in Review, a conflict or stale
         *     flag cleared, a faded memory kept again, the Brief version Dream
         *     replaced written back). Any person who may keep undoes it, within 30
         *     days. Each memory (or the Brief) gets an `undid` receipt whose
         *     `source` names the action's receipt. Refused with 409
         *     `undo_refused` when it was already undone, the window has passed, a
         *     memory it touched was forgotten, or a later change depends on it.
         */
        post: operations["undoDreamAction"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/memories/{ref}:restore": {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Restore a faded memory
         * @description Keeps a faded memory again (Dream fades what nobody read in 60 days,
         *     and never deletes it). It follows Keep's rules, and the memory
         *     compiles again. Anything that isn't faded is 409
         *     `invalid_transition`.
         */
        post: operations["restoreMemory"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/dream/settings": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Your Dream settings
         * @description Your time zone (Dream runs in your local night; `default` means
         *     Memax doesn't know it yet and uses UTC) and whether the morning
         *     edition comes by email.
         */
        get: operations["getDreamSettings"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        /**
         * Change your Dream settings
         * @description Sets your time zone (an IANA name, such as America/Vancouver) or
         *     turns the morning email on or off. People only; your spaces' next
         *     nights move to your zone at the next sweep.
         */
        patch: operations["updateDreamSettings"];
        trace?: never;
    };
    "/v2/dream/email:unsubscribe": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Turn off the morning email
         * @description One-click unsubscribe (RFC 8058) from the morning edition: the token
         *     from the email is the credential, so it needs no sign-in. It answers
         *     the same whether or not the token matched.
         */
        post: operations["unsubscribeDreamEmail"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/notices": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Your agent's notices
         * @description For an agent's own credential: what it hasn't been told yet, oldest
         *     first: each memory forgotten since it read it (or in a space it is
         *     connected to), and each space forgotten whole. The remote MCP server
         *     puts them in the agent's next response; a local server (memax mcp
         *     serve) reads them here and acknowledges them with `:ack` once it has
         *     told the agent. A person's session has none.
         */
        get: operations["listNotices"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/notices:ack": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Mark notices told
         * @description Marks the agent's notices told, each once (a notice already told,
         *     or someone else's, is skipped). Telling an agent is bookkeeping, not
         *     a change to the record: it writes no receipt, and acknowledging
         *     again changes nothing.
         */
        post: operations["ackNotices"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/device-authorizations:lookup": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Read a device's sign-in code
         * @description What a waiting device code says about the device asking (CliAuth):
         *     the client and its version, the machine's name and system, the
         *     space it will use, and the address its request came from. All of
         *     it but the address is what the device says about itself. A code
         *     you decided reads as it ended; anyone else's is 404, as a code that
         *     doesn't exist is, and naming codes that don't exist too often is
         *     429 `rate_limited`. It is a `POST` so the code stays out of URLs
         *     and access logs, and it takes no `Idempotency-Key`. Only a person
         *     on a session reads one (`device_by_person`).
         */
        post: operations["lookupDeviceAuthorization"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/device-authorizations:approve": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Sign a device in
         * @description Confirms a waiting code: the device's next poll of the token
         *     endpoint collects a session for you, once. The session is the
         *     CLI's (`surface` cli), so what the device does with it is
         *     `client_attested`, never `human_web`. Only a person on the web app
         *     confirms a code (`device_needs_web` otherwise; `device_by_person`
         *     for agents and keys), so an agent holding your CLI login can't sign
         *     more machines in. Confirming a code you already confirmed answers
         *     it as it is; a code you declined or that expired is 409
         *     `invalid_transition` with `details.state`.
         */
        post: operations["approveDeviceAuthorization"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/device-authorizations:deny": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Decline a device's code
         * @description Declines a waiting code ("It doesn't match"): the device hears
         *     `access_denied` and nothing is signed in. The same rules as
         *     confirming apply: a person on the web app, once; a code you
         *     confirmed, or that expired, is 409 `invalid_transition`.
         */
        post: operations["denyDeviceAuthorization"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/sessions": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        /**
         * Your sessions
         * @description Everywhere you are signed in, the most recently used first: the web
         *     app, the memax CLI (a browser login or the email code), a device
         *     code, and each MCP client you authorized. Each says what signed in,
         *     the client, when it signed in and was last used, the address and
         *     city it was last seen from when Memax knows them, when it ends, and
         *     whether it is the session this request comes from (`current`).
         *     Sessions that ended (signed out, revoked, expired, or an MCP client
         *     whose authorization was withdrawn) are not listed. Only a person on
         *     a session lists them (`session_by_person`).
         */
        get: operations["listSessions"];
        put?: never;
        post?: never;
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/sessions/{session}:revoke": {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The session's id. */
                session: components["parameters"]["SessionPath"];
            };
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Sign a session out
         * @description Ends one of your sessions. Its refresh token stops working at once;
         *     the access token it last received works until it expires, within
         *     the hour. An MCP client whose session you end must be authorized
         *     again; its agent connection stays. Signing out the session this
         *     request comes from works from anywhere you are signed in; any other
         *     needs you on the web app (`session_needs_web`), so an agent holding
         *     your CLI login can't sign you out elsewhere. A session that isn't
         *     yours, or has already ended, is 404.
         */
        post: operations["revokeSession"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
    "/v2/sessions:revoke-others": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * Sign out everywhere else
         * @description Ends every session of yours but the one this request comes from:
         *     other browsers, the CLI on every machine, devices, MCP clients. Each
         *     one's refresh token stops working at once, and its last access
         *     token within the hour. It needs you on the web app
         *     (`session_needs_web`). A request whose session Memax can't tell (a
         *     sign-in from before sessions were named) is 409
         *     `invalid_transition`: sign in again, then sign the others out.
         */
        post: operations["revokeOtherSessions"];
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        /** Format: uuid */
        Id: string;
        /** Format: date-time */
        Timestamp: string;
        /** @description A space's id or slug. */
        SpaceKey: string;
        /** @description A display ID (M-0219) or a memory id. */
        MemoryRef: string;
        /** @description A memory's display ID, unique within its tenant. */
        DisplayRef: string;
        /** @description A memory's (M-) or a note's (N-) display ID, unique within its tenant. */
        RecordRef: string;
        /** @description A note's display ID, unique within its tenant. */
        NoteRef: string;
        /** @description A note's display ID (N-0042, any case and padding) or its id. */
        NoteKey: string;
        /** @description A memory version as an entity tag, `"3"`. A bare `3` is accepted too. */
        VersionTag: string;
        /** @description Opaque. Pass it back as `cursor`. */
        Cursor: string;
        /** @description Why. It goes into the receipt, and is redacted if the memory is forgotten. */
        Reason: string;
        /** @description The agent session the command came from, if any. */
        SessionRef: string;
        /**
         * @description Where a memory sits in the Brief.
         * @enum {string}
         */
        Section: "decisions" | "conventions" | "preferences" | "open_question";
        /** @enum {string} */
        MemoryKind: "fact" | "decision";
        /**
         * @description The one state people see, derived from `lifecycle` and `flags`:
         *     forgotten > conflict > stale > proposed > merged > faded > kept.
         *     `rejected` is internal and carries no mark.
         * @enum {string}
         */
        State: "proposed" | "kept" | "merged" | "stale" | "conflict" | "faded" | "forgotten" | "rejected";
        /** @enum {string} */
        Lifecycle: "proposed" | "kept" | "merged" | "faded" | "forgotten" | "rejected";
        /** @enum {string} */
        Flag: "conflict" | "stale";
        /**
         * @description The class of a source, and of a memory (the lowest of its sources),
         *     highest first. `external` is quarantined.
         * @enum {string}
         */
        Trust: "person" | "agent_own_work" | "repository" | "external";
        /**
         * @description What a source points at. `memory` cites a kept memory in the same
         *     space by its display ID (a kept Ask answer cites the memories it
         *     came from); its trust is the cited memory's, whatever the request
         *     says.
         * @enum {string}
         */
        SourceKind: "session" | "pr" | "file" | "url" | "issue" | "email" | "note" | "import" | "memory";
        /** @enum {string} */
        SpaceKind: "personal" | "project" | "team";
        /** @enum {string} */
        Role: "owner" | "member" | "viewer";
        /**
         * @description What an agent may do in a space. `read` writes nothing; `propose`
         *     sends its writes to Review; `write` keeps them, still with a
         *     receipt (and still proposes what cites an outside source).
         * @enum {string}
         */
        Autonomy: "read" | "propose" | "write";
        /**
         * @description Which agent a connection is. `other` is any agent Memax doesn't know by name.
         * @enum {string}
         */
        AgentKind: "claude-code" | "codex" | "cursor" | "chatgpt" | "claude" | "gemini-cli" | "copilot" | "opencode" | "windsurf" | "other";
        /**
         * @description Where the agent runs.
         * @enum {string}
         */
        AgentSurface: "cli" | "ide" | "cloud" | "chat";
        /**
         * @description `active`; `paused` (it only reads until resumed); `disconnected`
         *     (for good, its credential revoked).
         * @enum {string}
         */
        AgentState: "active" | "paused" | "disconnected";
        /** @enum {string} */
        CredentialKind: "api_key" | "oauth_grant";
        /** @enum {string} */
        ActorKind: "person" | "agent" | "dream" | "memax" | "repository";
        /**
         * @description The surface a change came through.
         * @enum {string}
         */
        Via: "web" | "cli" | "mcp" | "review" | "api" | "email" | "slack" | "github" | "linear" | "import" | "system";
        /**
         * @description For keeps, and a person's changes to an agent connection:
         *     `human_web` when Memax verified the person was on the web app,
         *     `client_attested` when a client (the CLI, an agent) says a person did.
         * @enum {string}
         */
        Assurance: "human_web" | "client_attested";
        /**
         * @description The past-tense verb a receipt records. An agent connection's
         *     receipts (`object_kind: agent`) are connected, autonomy_changed,
         *     paused, resumed and disconnected. A Brief's (`brief`) are revised; a
         *     target's (`target`) configured, requested, observed, pulled,
         *     overwritten and stopped; a compile run's (`compile`) compiled and
         *     delivered. The judge's (by Memax) are merged (a fold), linked (an
         *     update or explicit change), flagged (a conflict), returned (a
         *     Write-level agent's write it found contradicting a decision in
         *     force, put back in Review as a conflict; `source` is that decision)
         *     and judged (nothing to do); settling a conflict writes resolved,
         *     kept, rejected, edited, superseded or faded, and drafted (narrower
         *     words for a kept memory, held out of force until the judge has seen
         *     them); Undo writes undid. A decision gate's (`gate`) are asked (by
         *     the agent), answered (by a person; the kept decision it became has
         *     its own `kept` receipt) and withdrawn.
         *     Forget writes forgot on each forgotten memory (and on a gate whose
         *     decision it was, and on a space forgotten whole), resolved on a
         *     memory whose only conflict was with it, and purged on a target or a
         *     Brief whose drift evidence or older versions lost its words; an
         *     agent's memax_forget writes forget_requested, and a person keeping
         *     the memory instead writes forget_declined. An export of the space is one
         *     exported receipt on the space (`object_kind: space`). Dream
         *     (`actor_kind: dream`, via system) writes published on its edition
         *     (`dream`, D-), folded (notes folded into a memory as lineage), proposed,
         *     merged (a duplicate proposal), flagged (a conflict or a stale fact),
         *     faded and revised (the Brief), each citing the edition (`source: {kind:
         *     dream, ref: D-0214}`); undoing one writes undid; restoring a faded
         *     memory writes restored. Switching to V2 writes noted on the space (its
         *     V1 content numbered as notes) and switched once it is on the V2
         *     record; switching back writes switched_back. A note's Forget writes
         *     forgot on the note (`object_kind: note`).
         * @enum {string}
         */
        ReceiptAction: "proposed" | "kept" | "edited" | "rejected" | "merged" | "flagged" | "resolved" | "verified" | "faded" | "restored" | "forgot" | "moved" | "compiled" | "handed_off" | "answered" | "undid" | "connected" | "autonomy_changed" | "paused" | "resumed" | "disconnected" | "revised" | "configured" | "requested" | "delivered" | "observed" | "pulled" | "overwritten" | "stopped" | "judged" | "linked" | "superseded" | "asked" | "withdrawn" | "returned" | "drafted" | "purged" | "forget_requested" | "forget_declined" | "published" | "folded" | "exported" | "noted" | "switched" | "switched_back";
        /** @enum {string} */
        ObjectKind: "memory" | "note" | "brief" | "target" | "compile" | "handoff" | "gate" | "dream" | "agent" | "space";
        /**
         * @description What a command did. `applied`: it took effect as asked. `proposed`:
         *     it went to Review. `needs_confirmation`: it went to Review, and the
         *     agent should ask the person to keep it.
         * @enum {string}
         */
        Outcome: "applied" | "proposed" | "needs_confirmation";
        /** @enum {string} */
        PolicyEffect: "apply" | "propose" | "confirm" | "refuse";
        /**
         * @description Why policy decided what it did. Stable; clients localise by it.
         *     Refusals: unknown_actor, unknown_action, secret_detected, not_member,
         *     read_only, key_read_only, key_cannot_review, key_cannot_forget,
         *     person_must_review, person_must_forget, forget_not_allowed,
         *     external_needs_review, proposal_in_review, agent_not_connected (the
         *     agent has no connection, or none to this space, so it only reads),
         *     agent_paused, brief_by_person (agents propose; people edit the
         *     Brief), targets_by_person, compile_by_memax, judge_by_memax,
         *     undo_by_decider (only the person who decided can undo it). Refused
         *     decision gates: gate_by_agent (agents ask; people decide directly),
         *     person_must_answer (agents ask; people answer), gate_limit (the agent
         *     already has 3 decisions waiting in the space), not_your_gate (only the
         *     agent that asked, the person it works for, or someone who can answer
         *     withdraws a question). Refused forget requests: forget_by_person
         *     (people forget it themselves, or ask an owner). Refused asks: ask_by_person (agents read over
         *     MCP; Ask answers people), ask_limit (the plan's asks this month are
         *     used up; `details.limit` and `details.resets_at` say how many and
         *     when they start again). A refused export: export_by_person (only a
         *     signed-in person exports a space). Refused changes to
         *     agents: person_must_manage (only a person changes what an agent may
         *     do), not_your_agent, autonomy_not_allowed, key_max_propose,
         *     autonomy_needs_web (raising an agent needs a person on the web).
         *     Refused or sent to Review:
         *     viewer, owners_keep, decision_needs_web. Sent to Review: api_key,
         *     external_source, contradicts_decision, touches_decision (a
         *     Write-level agent's write that touches a decision in force waits for
         *     the judge and a person), edits_person_kept,
         *     autonomy_propose, integration, import, system_proposes, repository,
         *     person_proposed. Saved, not kept: judge_pending (a person's edit,
         *     then keep, whose new words touch a decision in force: the edit is
         *     the proposal's new version, and Keep waits for the judge; and a
         *     "keep both" whose narrower words touch another decision in force:
         *     they are saved, and the same resolution waits for the judge).
         *     Confirmation: confirm_in_agent.
         *     Refused changes to spaces: space_by_person (only a signed-in person
         *     creates or switches a space), space_kind (only project spaces are
         *     created here), space_limit (the most project spaces one person owns
         *     during the alpha), switch_by_owner (only the owner switches a space).
         *     Refused device codes: device_by_person (only a person signs a device
         *     in), device_needs_web (confirming or declining a code needs a person
         *     on the web app). Refused sessions: session_by_person (only a person
         *     lists or signs out their sessions), session_needs_web (signing out a
         *     session other than this one needs a person on the web app).
         * @enum {string}
         */
        PolicyCode: "unknown_actor" | "unknown_action" | "secret_detected" | "not_member" | "read_only" | "key_read_only" | "key_cannot_review" | "key_cannot_forget" | "person_must_review" | "person_must_forget" | "forget_not_allowed" | "external_needs_review" | "proposal_in_review" | "agent_not_connected" | "agent_paused" | "person_must_manage" | "not_your_agent" | "autonomy_not_allowed" | "key_max_propose" | "autonomy_needs_web" | "brief_by_person" | "targets_by_person" | "compile_by_memax" | "judge_by_memax" | "undo_by_decider" | "gate_by_agent" | "person_must_answer" | "gate_limit" | "not_your_gate" | "forget_by_person" | "ask_by_person" | "ask_limit" | "export_by_person" | "viewer" | "owners_keep" | "decision_needs_web" | "api_key" | "external_source" | "contradicts_decision" | "touches_decision" | "edits_person_kept" | "autonomy_propose" | "integration" | "import" | "system_proposes" | "repository" | "person_proposed" | "judge_pending" | "confirm_in_agent" | "space_by_person" | "space_kind" | "space_limit" | "switch_by_owner" | "dream_by_dream" | "dream_run_by_owner" | "device_by_person" | "device_needs_web" | "session_by_person" | "session_needs_web";
        /** @enum {string} */
        ErrorCode: "invalid_request" | "idempotency_key_required" | "space_required" | "ambiguous_ref" | "unauthorized" | "refused" | "permission_denied" | "impersonation_read_only" | "surface_unverified" | "not_found" | "method_not_allowed" | "invalid_transition" | "in_conflict" | "undo_refused" | "edit_clash" | "idempotency_key_reused" | "precondition_required" | "rate_limited" | "internal_error" | "busy" | "judge_pending" | "unavailable" | "forget_carries" | "slug_taken" | "space_kind";
        /**
         * @description A typed edge between two memories. merged_into: folded into another
         *     memory (a duplicate, or a repeat of a rejection). supersedes: replaces
         *     (or, as a proposal, would replace) another. conflicts_with:
         *     contradicts a decision in force. closes: settles another.
         * @enum {string}
         */
        LinkKind: "merged_into" | "supersedes" | "conflicts_with" | "closes";
        /**
         * @description `out`: from this memory to the other; `in`: from the other to this one.
         * @enum {string}
         */
        LinkDirection: "out" | "in";
        /**
         * @description How the judge found a memory relates to another; `none` when it compared nothing.
         * @enum {string}
         */
        Relation: "duplicate" | "updates" | "extends" | "contradicts" | "unrelated" | "none";
        /**
         * @description What decided the verdict: `exact` (the same words), `near` (a
         *     near-verbatim repeat), `reproposal` (a repeat of a rejection),
         *     `llm` (the model), `none` (nothing to compare, or no model).
         * @enum {string}
         */
        JudgeStage: "exact" | "near" | "reproposal" | "llm" | "none";
        /**
         * @description What the verdict did: folded, suppressed (folded into a rejection),
         *     linked (an update), superseding (an explicit change of a decision in
         *     force, which keeping it supersedes), flagged (a conflict), none,
         *     failed (no usable answer from the model; nothing flagged) or skipped
         *     (what it pointed at changed first).
         * @enum {string}
         */
        VerdictOutcome: "folded" | "suppressed" | "linked" | "superseding" | "flagged" | "none" | "failed" | "skipped";
        /**
         * @description `working`: not judged yet (Review's neutral mark). `judged`. `failed`: reviewed as usual.
         * @enum {string}
         */
        JudgeState: "working" | "judged" | "failed";
        /**
         * @description The judge's model tier whose answer counted.
         * @enum {string}
         */
        ModelTier: "primary" | "fallback" | "strong";
        /**
         * @description ReviewConflict's answers, relative to the memory in the path.
         * @enum {string}
         */
        ConflictChoice: "keep_this" | "keep_other" | "keep_both" | "leave_open";
        /**
         * @description What an answer does to one side.
         * @enum {string}
         */
        ConflictChange: "kept" | "rejected" | "superseded" | "faded" | "open" | "stays";
        /**
         * @description Why an undo was refused.
         * @enum {string}
         */
        UndoRefusal: "window_passed" | "later_changes" | "already_undone" | "not_undoable";
        /** @description A decision gate's display ID (G-0012) or its id. */
        GateRef: string;
        /** @description A decision gate's display ID, unique within its tenant. */
        GateDisplayRef: string;
        /**
         * @description `waiting` for a person; `answered` (kept as a decision); `withdrawn`
         *     (the question was taken back); `expired` (it waited past
         *     `expires_at`, and can't be answered).
         * @enum {string}
         */
        GateStatus: "waiting" | "answered" | "withdrawn" | "expired";
        /** @description A Brief version's display ID, unique within its tenant. */
        BriefRef: string;
        /** @description A compile run's display ID, unique within its tenant. */
        CompileRef: string;
        /** @description A memory's display ID; any case and padding (`m-219` is M-0219). */
        MemoryDisplayRef: string;
        /** @description The compiler's section key: decisions, conventions, preferences, open, overview, or your own. */
        SectionKey: string;
        /** @description A path relative to the repository root, with no `..` and no leading `/`. */
        RepoPath: string;
        /** @description A lowercase hex sha256. */
        Sha256: string;
        /**
         * @description The compiler's adapter. agents_md is the canonical AGENTS.md;
         *     claude_md and gemini_md are shims that import it; cursor_mdc,
         *     copilot, windsurf and claude_rules hold path-scoped facts only;
         *     chatgpt is copied out.
         * @enum {string}
         */
        TargetKind: "agents_md" | "claude_md" | "cursor_mdc" | "chatgpt" | "gemini_md" | "copilot" | "windsurf" | "claude_rules";
        /**
         * @description How the output reaches its readers: `local` (the daemon writes it),
         *     `pr` (a pull request), `mcp` (agents read it over MCP) or `copy` (a
         *     person copies it out; ChatGPT only).
         * @enum {string}
         */
        Delivery: "local" | "pr" | "mcp" | "copy";
        /**
         * @description `compiling`: a change hasn't compiled yet. `pending_delivery`: the
         *     latest compile isn't on disk yet. `in_sync`: it is (or, for MCP and
         *     copy-out, it compiled). `drifted`: a file was edited by hand.
         *     `held`: a hand edit came back by a pull, and its proposals wait in
         *     Review; the file stays as it is until they are kept or rejected.
         *     `off`: compiling is stopped.
         * @enum {string}
         */
        SyncState: "in_sync" | "compiling" | "pending_delivery" | "drifted" | "held" | "off";
        /** @enum {string} */
        IncludeMode: "kept_only" | "kept_and_open";
        /** @enum {string} */
        StaleMode: "mark" | "omit";
        /** @enum {string} */
        ScopedMode: "inline" | "omit";
        /** @enum {string} */
        CompileStatus: "compiled" | "delivered" | "failed";
        /** @enum {string} */
        ObservationStatus: "open" | "pulled" | "overwritten" | "stopped" | "dismissed";
        /** @enum {string} */
        ObserverKind: "device" | "github";
        /** @enum {string} */
        DriftMode: "pull" | "overwrite" | "stop";
        /** @enum {string} */
        ChangeKind: "edit" | "new" | "remove";
        /**
         * @description `proposed`: a proposal was written. `review`: a removed line waits
         *     for a person to forget or exclude its memory. `skipped`: nothing
         *     was written (`reason` says why).
         * @enum {string}
         */
        ChangeOutcome: "proposed" | "review" | "skipped";
        Space: {
            id: components["schemas"]["Id"];
            /** @description Who owns the space's record. Display IDs are unique per tenant. */
            tenant_id: components["schemas"]["Id"];
            slug: string;
            name: string;
            kind: components["schemas"]["SpaceKind"];
            role: components["schemas"]["Role"];
            /** @description The repository the space compiles for, if any. */
            repository?: string;
            /**
             * Format: date-time
             * @description When the space switched to the V2 record. Absent while it is on V1: there, agents' writes go to the V1 memory API, and V1 behaviour applies to every surface.
             */
            v2_enabled_at?: string;
        };
        DecisionOption: {
            label: string;
            detail?: string;
        };
        /** @description The extra fields of a memory of kind `decision`. */
        Decision: {
            why?: string;
            options?: components["schemas"]["DecisionOption"][];
            consequences?: string;
            area?: string;
            /** @enum {string} */
            status?: "in_force" | "superseded" | "open";
        };
        /** @description Where a memory came from. */
        Source: {
            id: components["schemas"]["Id"];
            kind: components["schemas"]["SourceKind"];
            /** @description What people see, e.g. "PR */
            ref: string;
            /** @description The machine locator, e.g. a URL or a repository path. */
            uri?: string;
            /** @description Structured position data, e.g. {path, line, commit}. */
            locator: {
                [key: string]: unknown;
            };
            /** @description Third-party content, quarantined to Review. */
            external: boolean;
            trust: components["schemas"]["Trust"];
            /** @description The supporting excerpt. Purged when the memory is forgotten. */
            quote?: string;
            content_hash?: string;
            created_at: components["schemas"]["Timestamp"];
        };
        /** @description One statement and its state. */
        Memory: {
            id: components["schemas"]["Id"];
            ref: components["schemas"]["DisplayRef"];
            space_id: components["schemas"]["Id"];
            tenant_id: components["schemas"]["Id"];
            /** @description The current version's words. Empty once forgotten. */
            statement: string;
            section: components["schemas"]["Section"];
            kind: components["schemas"]["MemoryKind"];
            state: components["schemas"]["State"];
            lifecycle: components["schemas"]["Lifecycle"];
            flags: components["schemas"]["Flag"][];
            trust: components["schemas"]["Trust"];
            /** @description The statement's version; also the `ETag`. */
            version: number;
            stale_after?: components["schemas"]["Timestamp"];
            /** @description "Stays true while" conditions. */
            conditions: unknown[];
            decision?: components["schemas"]["Decision"];
            /** @description Where it applies, e.g. {"paths": ["packages/web/**"]}. */
            scope: {
                [key: string]: unknown;
            };
            valid_from?: components["schemas"]["Timestamp"];
            valid_to?: components["schemas"]["Timestamp"];
            created_receipt_id: components["schemas"]["Id"];
            last_receipt_id: components["schemas"]["Id"];
            created_at: components["schemas"]["Timestamp"];
            updated_at: components["schemas"]["Timestamp"];
            /** @description Present when one memory is read and it cites sources. */
            sources?: components["schemas"]["Source"][];
            /** @description Its active links, both ways. Absent when it has none. */
            links?: components["schemas"]["Link"][];
            /**
             * @description The memory this one would replace (it has a `supersedes` link to
             *     it), with its current words, so Review can show the pair as a
             *     diff ("Updates M-0156").
             */
            updates?: components["schemas"]["LinkedMemory"];
            /**
             * @description The judge's verdict on the current version; for a proposal not
             *     judged yet, `state: working`. Absent for memories the judge
             *     doesn't look at (a person's own keep).
             */
            judge?: components["schemas"]["JudgeInfo"];
        };
        /** @description An active link, seen from one memory. */
        Link: {
            id: components["schemas"]["Id"];
            kind: components["schemas"]["LinkKind"];
            direction: components["schemas"]["LinkDirection"];
            /** @description The other memory. */
            memory_id: components["schemas"]["Id"];
            ref: components["schemas"]["DisplayRef"];
            /** @description The receipt that made the link. */
            receipt_id: components["schemas"]["Id"];
            created_at: components["schemas"]["Timestamp"];
        };
        /** @description The other side of a link, with its current words. */
        LinkedMemory: {
            id: components["schemas"]["Id"];
            ref: components["schemas"]["DisplayRef"];
            version: number;
            statement: string;
            lifecycle: components["schemas"]["Lifecycle"];
        };
        MemoryPointer: {
            id: components["schemas"]["Id"];
            ref: components["schemas"]["DisplayRef"];
        };
        /** @description The judge's verdict on a memory's current version. */
        JudgeInfo: {
            state: components["schemas"]["JudgeState"];
            /** @description The version judged (or waiting to be). */
            version: number;
            verdict?: components["schemas"]["Relation"];
            outcome?: components["schemas"]["VerdictOutcome"];
            stage?: components["schemas"]["JudgeStage"];
            /** @description The memory the verdict is about. */
            related?: components["schemas"]["MemoryPointer"];
            confidence?: number;
            /** @description One line from the model, in English. Purged if either memory is forgotten. */
            rationale?: string;
            /** @description The model's merged wording, for Review to offer. */
            merged_statement?: string;
            tier?: components["schemas"]["ModelTier"];
            judged_at?: components["schemas"]["Timestamp"];
        };
        /** @description One version of a memory's statement. */
        MemoryVersion: {
            version: number;
            /** @description Empty once the memory is forgotten. */
            statement: string;
            /** @description The receipt that wrote this version. */
            receipt_id: components["schemas"]["Id"];
            created_at: components["schemas"]["Timestamp"];
        };
        /** @description Where a change came from. A pointer, never a quote. */
        ReceiptSource: {
            kind: string;
            ref: string;
        };
        /** @description One entry of the append-only log. Never holds memory text. */
        Receipt: {
            id: components["schemas"]["Id"];
            /**
             * Format: int64
             * @description Position in the log.
             */
            seq: number;
            tenant_id: components["schemas"]["Id"];
            space_id: components["schemas"]["Id"];
            object_kind: components["schemas"]["ObjectKind"];
            object_id: components["schemas"]["Id"];
            object_ref: string;
            action: components["schemas"]["ReceiptAction"];
            actor_kind: components["schemas"]["ActorKind"];
            /** @description The person, or the agent's credential. Absent for Dream, Memax and the repository. */
            actor_id?: components["schemas"]["Id"];
            /** @description The agent the change came through, e.g. claude-code. */
            agent?: string;
            via: components["schemas"]["Via"];
            assurance?: components["schemas"]["Assurance"];
            session_ref?: string;
            source?: components["schemas"]["ReceiptSource"];
            /** @description Why, if given. Redacted when the memory is forgotten. */
            reason?: string;
            /** @description When it happened on the client. */
            occurred_at: components["schemas"]["Timestamp"];
            /** @description When the server recorded it. */
            recorded_at: components["schemas"]["Timestamp"];
            stream_id: components["schemas"]["Id"];
            stream_version: number;
        };
        /** @description Why a command had the outcome it did. */
        PolicyDecision: {
            effect: components["schemas"]["PolicyEffect"];
            code?: components["schemas"]["PolicyCode"];
            /** @description An English sentence; localise by `code`. */
            message?: string;
            /** @description External content, held with the ochre notice. */
            quarantine?: boolean;
        };
        CommandResult: {
            outcome: components["schemas"]["Outcome"];
            policy: components["schemas"]["PolicyDecision"];
            /**
             * @description The memory's current projection. For an edit sent to Review, the
             *     new proposal.
             */
            memory: components["schemas"]["Memory"];
            /** @description The receipts the command wrote, oldest first. */
            receipts: components["schemas"]["Receipt"][];
        };
        /** @description A command that changed several memories (settling a conflict, an undo). */
        MemoriesCommandResult: {
            outcome: components["schemas"]["Outcome"];
            policy: components["schemas"]["PolicyDecision"];
            /** @description The memory the command named (for an undo, the first it restored). */
            memory: components["schemas"]["Memory"];
            /** @description Every memory the command changed, that one first. */
            memories: components["schemas"]["Memory"][];
            /** @description The receipts the command wrote, oldest first. */
            receipts: components["schemas"]["Receipt"][];
        };
        ConflictEffect: {
            ref: components["schemas"]["DisplayRef"];
            change: components["schemas"]["ConflictChange"];
        };
        ConflictOption: {
            choice: components["schemas"]["ConflictChoice"];
            /** @description What it does to each side, this memory first. */
            effects: components["schemas"]["ConflictEffect"][];
            /** @description Whether you may take it. */
            allowed: boolean;
            /** @description Why you may not, when policy says so. */
            policy?: components["schemas"]["PolicyDecision"];
            /**
             * @description The judge's short label for this answer ("Fly.io everywhere"),
             *     when it wrote one. One line, in the language of the memories.
             */
            label?: string;
        };
        /** @description Both sides of a conflict, for ReviewConflict. */
        Conflict: {
            /** @description This side, with its sources, links and verdict. */
            memory: components["schemas"]["Memory"];
            other: components["schemas"]["Memory"];
            /** @description The side carrying the conflict flag. */
            flagged_ref: components["schemas"]["DisplayRef"];
            /** @description The decision in force it contradicts. */
            decision_ref: components["schemas"]["DisplayRef"];
            /** @description The conflicts_with link, seen from this side. */
            link: components["schemas"]["Link"];
            /** @description Both sides' latest receipts, newest first. */
            receipts: components["schemas"]["Receipt"][];
            options: components["schemas"]["ConflictOption"][];
            /**
             * @description The judge's short question for settling it ("Fly.io or Railway
             *     for the v2 API?"), when it wrote one (the verdict that flagged
             *     it). One line, in the language of the memories. Forgetting
             *     either side takes it out.
             */
            question?: string;
            /**
             * @description The answer the judge suggests, relative to this memory, when the
             *     flagged memory's sources settle it. A suggestion: a person
             *     decides.
             */
            suggested?: components["schemas"]["ConflictChoice"];
        };
        MemoryDetail: {
            memory: components["schemas"]["Memory"];
            /** @description Every version of the statement, newest first. */
            versions: components["schemas"]["MemoryVersion"][];
            receipts: components["schemas"]["ReceiptPage"];
            /** @description How agents read it. Absent when the counts couldn't be read. */
            reads?: components["schemas"]["MemoryReads"];
            /** @description Agents' requests to forget it, waiting for a person (forget it, or keep it with `:decline-forget`). */
            forget_requests?: components["schemas"]["ForgetRequestRecord"][];
        };
        /**
         * @description How a memory has been read: directly (recall, search, get, list) and
         *     in every compile that contained it (a session-start digest, a
         *     reported load), over the last 13 months.
         */
        MemoryReads: {
            /** @description Reads of it in the last 13 months. */
            reads: number;
            /** @description Reads of it in the last 7 days. */
            reads_7d: number;
            /** @description Its latest read; absent if it was never read. */
            last_read_at?: components["schemas"]["Timestamp"];
            /** @description How many agents read it ("reaches 5 agents"). */
            agents: number;
            /** @description Who read it, most recent first (at most 20). */
            readers: components["schemas"]["MemoryReader"][];
            /**
             * @description It is in a compiled file whose loads Memax can't see (agents read
             *     the file natively and no hook reports the loads), so it may be
             *     read more than counted, and it never fades.
             */
            unobserved_target: boolean;
        };
        /** @description One reader of a memory. */
        MemoryReader: {
            reader_kind: components["schemas"]["ReaderKind"];
            /** @description The agent connection, for an agent. */
            connection_id?: components["schemas"]["Id"];
            /** @description The person, or the person the agent works for. */
            person_id: components["schemas"]["Id"];
            agent?: components["schemas"]["AgentKind"];
            /** @description The agent connection's name, when you can see the connection. */
            display_name?: string;
            reads: number;
            reads_7d: number;
            last_read_at: components["schemas"]["Timestamp"];
        };
        /**
         * @description A zip archive in the export format `memax.export.v1`: one folder,
         *     named by the space's slug, holding README.md, export.json,
         *     memories/, tombstones/, brief/, decisions.md, gates.json,
         *     targets.json, agents.json, reads.json, receipts.jsonl and
         *     checkpoints.json. The same record gives the same bytes.
         */
        ExportArchive: string;
        /**
         * @description One signed checkpoint of a space's receipt chain. Its signature is
         *     Ed25519 over the checkpoint statement (receiptchain format 1: the
         *     magic `memax.checkpoint.v1`, then the length-prefixed space_id,
         *     tenant_id, number, position_from, position_to, first and last
         *     receipt ids, last_seq, prev_sha256, chain_sha256, merkle_root,
         *     sealed_at in microseconds and key_id).
         */
        Checkpoint: {
            id: components["schemas"]["Id"];
            space_id: components["schemas"]["Id"];
            tenant_id: components["schemas"]["Id"];
            /** @description 1, 2, 3 … in the space. */
            number: number;
            /** @description The first receipt's position in the space's chain, from 1. */
            position_from: number;
            position_to: number;
            receipts: number;
            first_receipt_id: components["schemas"]["Id"];
            last_receipt_id: components["schemas"]["Id"];
            /** @description The last receipt's `seq`. */
            last_seq: number;
            /** @description The chain hash before the range (the space's genesis hash for the first checkpoint). */
            prev_sha256: components["schemas"]["Sha256"];
            /** @description The chain hash after the range. */
            chain_sha256: components["schemas"]["Sha256"];
            /** @description The RFC 6962 Merkle root of the range's receipt leaves. */
            merkle_root: components["schemas"]["Sha256"];
            /**
             * @description The receipts' canonical encoding version.
             * @enum {integer}
             */
            format: 1;
            /** @description False when the server had no signing key; the checkpoint still chains. */
            signed: boolean;
            /** @description The signing key, as in `keys`. */
            key_id?: string;
            /** @description The Ed25519 signature, base64. */
            signature?: string;
            sealed_at: components["schemas"]["Timestamp"];
            /** @description When its copy reached object storage; absent until then. */
            stored_at?: components["schemas"]["Timestamp"];
        };
        /** @description How far a space's receipt chain is sealed and verified. */
        SealStatus: {
            /** @description Receipts sealed so far. */
            sealed_receipts: number;
            /** @description The last sealed receipt's `seq`. */
            sealed_through_seq?: number;
            sealed_through_receipt_id?: components["schemas"]["Id"];
            /** @description The chain hash after the last sealed receipt. */
            head_sha256?: components["schemas"]["Sha256"];
            checkpoints: number;
            sealed_at?: components["schemas"]["Timestamp"];
            /** @description Receipts written but not sealed yet (counted up to 10,000). */
            unsealed: number;
            /** @description When the chain was last verified from its first receipt. */
            verified_at?: components["schemas"]["Timestamp"];
            verified_receipts?: number;
            /** @description What the last verification found wrong; 0 means the chain verified. */
            verify_problems?: number;
        };
        /** @description A public key checkpoints are signed with. */
        SigningKey: {
            key_id: string;
            /** @enum {string} */
            algorithm: "ed25519";
            /** @description The raw 32-byte public key, base64. */
            public_key: string;
        };
        CheckpointPage: {
            items: components["schemas"]["Checkpoint"][];
            has_more: boolean;
            next_cursor?: components["schemas"]["Cursor"];
            seal: components["schemas"]["SealStatus"];
            keys: components["schemas"]["SigningKey"][];
        };
        /**
         * @description `agent` (an agent connection) or `person` (a person's CLI reporting their agent's session start).
         * @enum {string}
         */
        ReaderKind: "agent" | "person";
        /**
         * @description What a read went through: `recall` (with a query), `search`, `get`
         *     (one memory), `list` (a page of kept memories), `digest` (recall
         *     without a query, at session start), or `compile_load` (a hook
         *     reported the compile its agent loaded natively).
         * @enum {string}
         */
        ReadKind: "recall" | "search" | "get" | "list" | "digest" | "compile_load";
        /**
         * @description The surface a read came through.
         * @enum {string}
         */
        ReadVia: "mcp" | "api" | "cli";
        /** @description A read's display ID, unique within its tenant. */
        ReadRef: string;
        /** @description One read of one space (R-). It never holds memory text or the query. */
        Read: {
            id: components["schemas"]["Id"];
            ref: components["schemas"]["ReadRef"];
            space_id: components["schemas"]["Id"];
            reader_kind: components["schemas"]["ReaderKind"];
            /** @description The agent connection, for an agent. */
            connection_id?: components["schemas"]["Id"];
            /** @description The person, or the person the agent works for. */
            person_id: components["schemas"]["Id"];
            agent?: components["schemas"]["AgentKind"];
            kind: components["schemas"]["ReadKind"];
            via: components["schemas"]["ReadVia"];
            session_ref?: components["schemas"]["SessionRef"];
            /** @description The compile run read, for a compiled digest or a compile load. */
            compile?: components["schemas"]["CompileRef"];
            /** @description How many memories it covered; for a compile, every fact in it. */
            memories: number;
            /** @description The memories it returned directly, in display order (at most 200). */
            memory_refs: components["schemas"]["DisplayRef"][];
            read_at: components["schemas"]["Timestamp"];
            recorded_at: components["schemas"]["Timestamp"];
        };
        ReadPage: {
            items: components["schemas"]["Read"][];
            has_more: boolean;
            next_cursor?: components["schemas"]["Cursor"];
            /** @description The space's reads in the last 7 days. */
            reads_7d: number;
        };
        CompileLoadRequest: {
            /** @description The compile run the agent loaded, by display ID (C-0012) or id, from the compiled file's header. */
            compile: string;
            /** @description The agent whose session loaded it. Ignored for an agent's own credential, which is always its agent. */
            agent?: components["schemas"]["AgentKind"];
            session_ref?: components["schemas"]["SessionRef"];
            /** @description When the session loaded it; defaults to now, and must be within the last day. */
            loaded_at?: components["schemas"]["Timestamp"];
        };
        CompileLoadResult: {
            read: components["schemas"]["Read"];
        };
        SpaceList: {
            items: components["schemas"]["Space"][];
        };
        MemoryPage: {
            items: components["schemas"]["Memory"][];
            has_more: boolean;
            next_cursor?: components["schemas"]["Cursor"];
        };
        ReviewPage: {
            items: components["schemas"]["Memory"][];
            has_more: boolean;
            next_cursor?: components["schemas"]["Cursor"];
            /** @description How many memories are waiting, across every page. */
            total: number;
        };
        ReceiptPage: {
            items: components["schemas"]["Receipt"][];
            has_more: boolean;
            next_cursor?: components["schemas"]["Cursor"];
        };
        /** @description The credential a connection is bound to. */
        AgentCredential: {
            kind: components["schemas"]["CredentialKind"];
            /** @description The API key's or OAuth grant's id. */
            id: components["schemas"]["Id"];
            /** @description False once the credential is revoked, expired or deleted. */
            active: boolean;
        };
        /** @description A connection's autonomy in one space, and its use there. */
        AgentSpace: {
            space_id: components["schemas"]["Id"];
            slug: string;
            name: string;
            kind: components["schemas"]["SpaceKind"];
            autonomy: components["schemas"]["Autonomy"];
            /** @description Its reads (R-) of this space in the last 7 days. */
            reads_7d: number;
            /** @description Memories it proposed, kept or edited in the last 7 days. */
            writes_7d: number;
            /** @description When its autonomy here last changed. */
            updated_at: components["schemas"]["Timestamp"];
        };
        /**
         * @description An agent working for a person through one credential: the identity
         *     its receipts name, and its autonomy in each space.
         */
        AgentConnection: {
            /** @description The connection's id; receipts of its writes carry it as `actor_id`. */
            id: components["schemas"]["Id"];
            /** @description The person it works for. */
            person_id: components["schemas"]["Id"];
            agent: components["schemas"]["AgentKind"];
            display_name: string;
            surface: components["schemas"]["AgentSurface"];
            credential: components["schemas"]["AgentCredential"];
            /**
             * @description The most its credential allows: an API key proposes at most, and
             *     a credential without write access (or one no longer active) only
             *     reads.
             */
            max_autonomy: components["schemas"]["Autonomy"];
            /** @description The OAuth client's Client ID Metadata Document URL, when it has one. */
            client_id?: string;
            state: components["schemas"]["AgentState"];
            /**
             * @description The spaces of yours it is connected to, personal space first. In a
             *     space it isn't connected to, it only reads.
             */
            spaces: components["schemas"]["AgentSpace"][];
            /** @description Its reads (R-, one per space read) in the last 7 days, in those spaces. */
            reads_7d: number;
            /** @description Writes in the last 7 days, in those spaces. */
            writes_7d: number;
            /** @description When it last used Memax (updated at most once a minute). */
            last_seen_at?: components["schemas"]["Timestamp"];
            /** @description Who connected it (`memax` for a V1 credential carried over), when you can see that receipt. */
            connected_by_kind?: components["schemas"]["ActorKind"];
            /** @description The person who connected it. */
            connected_by?: components["schemas"]["Id"];
            disconnected_at?: components["schemas"]["Timestamp"];
            created_receipt_id: components["schemas"]["Id"];
            last_receipt_id: components["schemas"]["Id"];
            /** @description When it was connected. */
            created_at: components["schemas"]["Timestamp"];
            updated_at: components["schemas"]["Timestamp"];
        };
        AgentList: {
            items: components["schemas"]["AgentConnection"][];
        };
        /**
         * @description What the agent did in the last 7 days, in your spaces: its reads, its
         *     writes, and what became of the memories it wrote.
         */
        AgentWeek: {
            /** @description Its reads (R-, one per space read). */
            reads: number;
            /** @description Memories it proposed, kept or edited. */
            writes: number;
            /** @description Memories it wrote that went to Review. */
            proposals: number;
            /** @description Memories it wrote that are kept now. */
            kept: number;
            rejected: number;
            /** @description Memories it wrote that are still waiting in Review. */
            waiting: number;
        };
        /** @description One of the agent's sessions, from the session on its receipts and on its reads of the last 30 days. */
        AgentSession: {
            session_ref: components["schemas"]["SessionRef"];
            /** @description Its reads in that session, in the last 30 days. */
            reads: number;
            writes: number;
            last_at: components["schemas"]["Timestamp"];
        };
        AgentDetail: {
            agent: components["schemas"]["AgentConnection"];
            this_week: components["schemas"]["AgentWeek"];
            /** @description Its latest receipts on memories, newest first (at most 10). Read a memory for its words. */
            recent_writes: components["schemas"]["Receipt"][];
            /** @description Its latest sessions, most recent first (at most 10). */
            sessions: components["schemas"]["AgentSession"][];
        };
        AgentCommandResult: {
            outcome: components["schemas"]["Outcome"];
            policy: components["schemas"]["PolicyDecision"];
            /** @description The connection after the change. */
            agent: components["schemas"]["AgentConnection"];
            /**
             * @description The receipts the command wrote, oldest first: one per space the
             *     change applies to, or none when there was nothing to change.
             */
            receipts: components["schemas"]["Receipt"][];
        };
        /** @description A memory item (`ref`) or a line of connective prose that cites what it rests on (`text`, `cites`). */
        BriefItem: {
            ref?: components["schemas"]["DisplayRef"];
            text?: string;
            cites?: components["schemas"]["DisplayRef"][];
            /** @description In an older version, prose whose words Forget took out because it cited a forgotten memory; only its citations stay. */
            forgotten?: boolean;
        };
        BriefSection: {
            key: components["schemas"]["SectionKey"];
            heading: string;
            items: components["schemas"]["BriefItem"][];
        };
        /** @description One version (`B-`) of a space's Brief. */
        Brief: {
            /** @description The Brief's id, the same for every version. */
            id: components["schemas"]["Id"];
            version_id: components["schemas"]["Id"];
            ref: components["schemas"]["BriefRef"];
            /** @description The version number; also the `ETag`. */
            version: number;
            space_id: components["schemas"]["Id"];
            tenant_id: components["schemas"]["Id"];
            /** @description The version it was revised from. */
            parent_version?: number;
            title: string;
            summary?: string;
            sections: components["schemas"]["BriefSection"][];
            /** @description How many memories the version places or cites. */
            facts: number;
            /** @description Set on the version in force. */
            current: boolean;
            receipt_id: components["schemas"]["Id"];
            /** @description Who wrote this version, and why. */
            receipt?: components["schemas"]["Receipt"];
            created_at: components["schemas"]["Timestamp"];
        };
        BriefResult: {
            outcome: components["schemas"]["Outcome"];
            policy: components["schemas"]["PolicyDecision"];
            brief: components["schemas"]["Brief"];
            receipts: components["schemas"]["Receipt"][];
        };
        /**
         * @description Something a restore left out of the version it restored, and why.
         *     Names memories by display ID, never by their words.
         */
        BriefDrop: {
            /** @description The key of the section it was in. */
            section: components["schemas"]["SectionKey"];
            /** @description The line in the restored version, a memory's ref or `P:<section>:<index>` for prose. */
            item: string;
            /**
             * @description A memory line, a line of prose, or a citation taken off a line of prose that stays.
             * @enum {string}
             */
            kind: "memory" | "prose" | "cite";
            /**
             * @description The memories that made it go: the memory line's own, the
             *     forgotten ones a line of prose cited (or, when none was
             *     forgotten, every memory it cited), or the memory a citation
             *     named.
             */
            refs: components["schemas"]["DisplayRef"][];
            /**
             * @description `forgotten` (the memory, or the line's words, were forgotten) or
             *     `not_kept` (it isn't kept any more: faded, folded, rejected).
             * @enum {string}
             */
            reason: "forgotten" | "not_kept";
        };
        RestoreBriefResult: {
            outcome: components["schemas"]["Outcome"];
            policy: components["schemas"]["PolicyDecision"];
            /** @description The new version. */
            brief: components["schemas"]["Brief"];
            receipts: components["schemas"]["Receipt"][];
            /** @description What the restore left out, in the order of the version it restored. */
            dropped: components["schemas"]["BriefDrop"][];
        };
        BriefVersionPage: {
            items: components["schemas"]["Brief"][];
            has_more: boolean;
            next_cursor?: components["schemas"]["Cursor"];
        };
        /** @description How a target is written. */
        TargetSettings: {
            include: components["schemas"]["IncludeMode"];
            stale: components["schemas"]["StaleMode"];
            /** @description Bytes per file; the tool's own limit caps it. */
            size_budget: number;
            /** @description agents_md and chatgpt only. */
            scoped?: components["schemas"]["ScopedMode"];
            /** @description Shims only. The person owns the file; Memax manages one marked block in it. */
            user_owned?: boolean;
        };
        DeliveredFile: {
            path: components["schemas"]["RepoPath"];
            /** @description The file's drift hash as Memax believes it is on disk. */
            sha256: components["schemas"]["Sha256"];
            /** @description Set when the baseline is a hand edit Memax accepted (pulled or overwritten). */
            observation?: components["schemas"]["Id"];
            /**
             * @description Set with `observation`. True while the proposals of the pull that
             *     accepted this edit wait in Review: nothing is written over the
             *     file until they are kept or rejected.
             */
            held?: boolean;
        };
        /** @description A file a pull holds, and the proposals it waits for. */
        TargetHold: {
            /** @description The hand edit the pull accepted. */
            observation: components["schemas"]["Id"];
            path: components["schemas"]["RepoPath"];
            /** @description The pull's proposals that are still proposed. */
            proposals: components["schemas"]["DisplayRef"][];
            /** @description When the pull was made. */
            since: components["schemas"]["Timestamp"];
        };
        /** @description What Memax believes is on disk. */
        Delivered: {
            compile_id?: components["schemas"]["Id"];
            /** @description The run whose output is on disk, if Memax wrote it. */
            compile?: components["schemas"]["CompileRef"];
            sha256: components["schemas"]["Sha256"];
            files: components["schemas"]["DeliveredFile"][];
            at?: components["schemas"]["Timestamp"];
        };
        /** @description One output of a compile run: a file (`path`) or copy-out text (`label`). */
        CompiledOutput: {
            path?: components["schemas"]["RepoPath"];
            label?: string;
            sha256: components["schemas"]["Sha256"];
            drift_sha256: components["schemas"]["Sha256"];
            bytes: number;
            lines: number;
            /** @description Memories whose statements it contains. */
            refs: components["schemas"]["DisplayRef"][];
            /** @description Every memory it cites. */
            cites: components["schemas"]["DisplayRef"][];
            /** @description Memories that didn't fit the size budget; they stay live over MCP. */
            dropped_for_budget: components["schemas"]["DisplayRef"][];
            user_owned?: boolean;
        };
        CompileWarning: {
            /** @description The compiler's warning code, or `not_compiled` for what the server left out. */
            code: string;
            message: string;
            path?: string;
            ref?: string;
        };
        /** @description One compile (`C-`) of one target. */
        CompileRun: {
            id: components["schemas"]["Id"];
            ref: components["schemas"]["CompileRef"];
            target_id: components["schemas"]["Id"];
            space_id: components["schemas"]["Id"];
            brief: components["schemas"]["BriefRef"];
            brief_version: number;
            /**
             * Format: int64
             * @description The target generation it compiled.
             */
            generation: number;
            status: components["schemas"]["CompileStatus"];
            input_sha256: components["schemas"]["Sha256"];
            output_sha256?: components["schemas"]["Sha256"];
            /** @description What a delivery is acknowledged against. */
            drift_sha256?: components["schemas"]["Sha256"];
            bytes: number;
            lines: number;
            refs: components["schemas"]["DisplayRef"][];
            dropped_for_budget: components["schemas"]["DisplayRef"][];
            files: components["schemas"]["CompiledOutput"][];
            warnings: components["schemas"]["CompileWarning"][];
            /** @description Why a failed run failed. Names fields, never memory text. */
            error?: string;
            /** @description When the change it compiled was made. */
            enqueued_at: components["schemas"]["Timestamp"];
            started_at: components["schemas"]["Timestamp"];
            compiled_at: components["schemas"]["Timestamp"];
            delivered_at?: components["schemas"]["Timestamp"];
            receipt_id: components["schemas"]["Id"];
        };
        /** @description A compiled file (or copy-out) of a space, and where it stands. */
        Target: {
            id: components["schemas"]["Id"];
            space_id: components["schemas"]["Id"];
            tenant_id: components["schemas"]["Id"];
            kind: components["schemas"]["TargetKind"];
            /** @description A file, or a rules directory for scoped kinds. Absent for ChatGPT. */
            path?: components["schemas"]["RepoPath"];
            /** @description What people see, e.g. `AGENTS.md` or `ChatGPT project`. */
            label: string;
            settings: components["schemas"]["TargetSettings"];
            delivery: components["schemas"]["Delivery"];
            sync_state: components["schemas"]["SyncState"];
            /** @description The target's version; also the `ETag`. */
            version: number;
            /**
             * Format: int64
             * @description Bumped by every change that affects the output.
             */
            dirty_gen: number;
            /**
             * Format: int64
             * @description The generation the latest compile covers.
             */
            compiled_gen: number;
            dirty_at: components["schemas"]["Timestamp"];
            /** @description The latest run, whatever its status. */
            last_compile?: components["schemas"]["CompileRun"];
            delivered?: components["schemas"]["Delivered"];
            /** @description Files with a hand edit waiting to be resolved. */
            open_drift: number;
            /** @description The files a pull holds (the target is `held`); absent when none. */
            holds?: components["schemas"]["TargetHold"][];
            created_receipt_id: components["schemas"]["Id"];
            last_receipt_id: components["schemas"]["Id"];
            created_at: components["schemas"]["Timestamp"];
            updated_at: components["schemas"]["Timestamp"];
        };
        TargetList: {
            items: components["schemas"]["Target"][];
        };
        TargetResult: {
            outcome: components["schemas"]["Outcome"];
            policy: components["schemas"]["PolicyDecision"];
            target: components["schemas"]["Target"];
            /** @description The receipts the command wrote; none when it changed nothing. */
            receipts: components["schemas"]["Receipt"][];
        };
        /** @description One compiled output with its content. */
        PreviewOutput: {
            path?: components["schemas"]["RepoPath"];
            label?: string;
            content: string;
            sha256: components["schemas"]["Sha256"];
            drift_sha256: components["schemas"]["Sha256"];
            bytes: number;
            lines: number;
            refs: components["schemas"]["DisplayRef"][];
            cites: components["schemas"]["DisplayRef"][];
            dropped_for_budget: components["schemas"]["DisplayRef"][];
            user_owned?: boolean;
        };
        TargetPreview: {
            target: components["schemas"]["Target"];
            /** @description The latest good run, whose content this is. Absent before the first compile. */
            compile?: components["schemas"]["CompileRun"];
            /** @description The canonical file this tool also reads, e.g. `AGENTS.md` for Cursor. */
            reads?: string;
            files: components["schemas"]["PreviewOutput"][];
            /** @description Copy-out text (ChatGPT project instructions). */
            copies: components["schemas"]["PreviewOutput"][];
        };
        CompileRunPage: {
            items: components["schemas"]["CompileRun"][];
            has_more: boolean;
            next_cursor?: components["schemas"]["Cursor"];
        };
        /**
         * @description One change the compiler read back from a hand edit: `edit` (a cited
         *     line's words changed: `ref`, `refs`, `old_text`, `new_text`,
         *     `old_line`, `new_line`), `new` (a line with no matching cite:
         *     `text`, `line`, `section`, `paths`, `cites`) or `remove` (a cited
         *     line is gone: `ref`, `refs`, `old_text`, `old_line`).
         */
        DriftChange: {
            kind: components["schemas"]["ChangeKind"];
            ref?: string;
            refs?: string[];
            old_text?: string;
            new_text?: string;
            old_line?: number;
            new_line?: number;
            text?: string;
            line?: number;
            /** @description The heading it was added under. */
            section?: string | null;
            paths?: string[];
            cites?: string[];
        };
        DriftInfo: {
            changed: boolean;
            header_edited: boolean;
            frontmatter_edited: boolean;
            layout_edited: boolean;
            /**
             * @description For a file the person owns, the state of Memax's block.
             * @enum {string|null}
             */
            managed_block: "intact" | "edited" | "removed" | null;
            /** @description Hidden characters in the edited file; they never reach a proposal. */
            hidden_characters: number;
        };
        ChangeSet: {
            changes: components["schemas"]["DriftChange"][];
            drift: components["schemas"]["DriftInfo"];
        };
        /** @description What resolving a hand edit did with one change. */
        ChangeResult: {
            kind: components["schemas"]["ChangeKind"];
            line: number;
            /** @description The memory the change is about (edits and removals). */
            ref?: string;
            outcome: components["schemas"]["ChangeOutcome"];
            /** @description The proposal written for it. */
            proposal?: components["schemas"]["DisplayRef"];
            /** @description Why it was skipped (a policy code, or too_long, empty, invalid_text). */
            reason?: string;
        };
        DriftResolutionRecord: {
            mode?: components["schemas"]["DriftMode"];
            receipt_id: components["schemas"]["Id"];
            changes?: components["schemas"]["ChangeResult"][];
            /** @description A newer observation of the same file that dismissed this one. */
            superseded_by?: components["schemas"]["Id"];
        };
        /** @description A compiled file seen changed outside Memax. */
        Observation: {
            id: components["schemas"]["Id"];
            target_id: components["schemas"]["Id"];
            space_id: components["schemas"]["Id"];
            path: components["schemas"]["RepoPath"];
            observed_sha256: components["schemas"]["Sha256"];
            observer_kind: components["schemas"]["ObserverKind"];
            observer_id: string;
            commit?: string;
            bytes: number;
            base_compile_id?: components["schemas"]["Id"];
            /** @description The run the edit was compared against, when Memax wrote the baseline. */
            base_compile?: components["schemas"]["CompileRef"];
            base_sha256?: components["schemas"]["Sha256"];
            changeset: components["schemas"]["ChangeSet"];
            status: components["schemas"]["ObservationStatus"];
            resolution?: components["schemas"]["DriftResolutionRecord"];
            observed_at: components["schemas"]["Timestamp"];
            resolved_at?: components["schemas"]["Timestamp"];
            receipt_id: components["schemas"]["Id"];
        };
        ObservationResult: {
            /** @description Whether the file was a hand edit (and recorded). */
            drifted: boolean;
            target: components["schemas"]["Target"];
            observation?: components["schemas"]["Observation"];
            receipts: components["schemas"]["Receipt"][];
        };
        DeliveryResult: {
            target: components["schemas"]["Target"];
            compile: components["schemas"]["CompileRun"];
            /** @description Empty when the delivery was already known. */
            receipts: components["schemas"]["Receipt"][];
        };
        DriftItem: {
            observation: components["schemas"]["Observation"];
            /** @description The file as Memax last delivered (or accepted) it; empty if it never wrote one. */
            compiled: string;
            base_compile?: components["schemas"]["CompileRun"];
            /** @description The file as it is now. */
            observed: string;
        };
        Drift: {
            target: components["schemas"]["Target"];
            items: components["schemas"]["DriftItem"][];
        };
        DriftResolutionResult: {
            outcome: components["schemas"]["Outcome"];
            policy: components["schemas"]["PolicyDecision"];
            target: components["schemas"]["Target"];
            /** @description The hand edits resolved. */
            observations: components["schemas"]["Observation"][];
            /** @description The proposals a pull wrote, in file order. */
            proposals: components["schemas"]["Memory"][];
            receipts: components["schemas"]["Receipt"][];
        };
        GateOption: {
            label: string;
            /** @description What choosing it means. */
            detail?: string;
        };
        /** @description How a person answered, and the decision it became. */
        GateAnswer: {
            /** @description The chosen option, counting from 1. */
            option: number;
            label: string;
            /** @description The kept decision the answer became. */
            memory: components["schemas"]["MemoryPointer"];
            /** @description The person who answered. */
            answered_by: components["schemas"]["Id"];
            answered_at: components["schemas"]["Timestamp"];
            assurance: components["schemas"]["Assurance"];
        };
        /** @description Who took the question back, and when. */
        GateWithdrawal: {
            /**
             * @description `agent`: the one that asked. `person`: the person it works for, or someone who could answer.
             * @enum {string}
             */
            by_kind: "agent" | "person";
            /** @description The agent connection or the person. */
            by: components["schemas"]["Id"];
            at: components["schemas"]["Timestamp"];
        };
        /** @description A decision gate (G-): a question an agent asked a person. */
        Gate: {
            id: components["schemas"]["Id"];
            ref: components["schemas"]["GateDisplayRef"];
            space_id: components["schemas"]["Id"];
            tenant_id: components["schemas"]["Id"];
            /** @description One question, in the asking agent's words. */
            question: string;
            /** @description Why it matters, from the agent. */
            context?: string;
            options: components["schemas"]["GateOption"][];
            status: components["schemas"]["GateStatus"];
            expires_at: components["schemas"]["Timestamp"];
            /** @description The agent connection that asked. */
            asked_by: components["schemas"]["Id"];
            /** @description The agent that asked, e.g. codex. */
            agent?: string;
            /** @description The agent session it asked from. */
            session_ref?: string;
            /** @description Answering needs a person on the web: the space's rule for decisions (team spaces by default). The CLI and in-agent answers are refused. */
            needs_web: boolean;
            answer?: components["schemas"]["GateAnswer"];
            withdrawn?: components["schemas"]["GateWithdrawal"];
            /** @description When the agent that asked was told how it ended. */
            delivered_at?: components["schemas"]["Timestamp"];
            /** @description Changes when the gate ends. Send it back as `If-Match`. */
            version: number;
            created_receipt_id: components["schemas"]["Id"];
            last_receipt_id: components["schemas"]["Id"];
            created_at: components["schemas"]["Timestamp"];
            updated_at: components["schemas"]["Timestamp"];
        };
        GatePage: {
            items: components["schemas"]["Gate"][];
            has_more: boolean;
            next_cursor?: components["schemas"]["Cursor"];
        };
        GateResult: {
            outcome: components["schemas"]["Outcome"];
            policy: components["schemas"]["PolicyDecision"];
            gate: components["schemas"]["Gate"];
            /** @description For an answer, the kept decision it became. */
            memory?: components["schemas"]["Memory"];
            /** @description The receipts the command wrote, oldest first. */
            receipts: components["schemas"]["Receipt"][];
        };
        /** @description A source a new memory cites. */
        SourceInput: {
            kind: components["schemas"]["SourceKind"];
            /** @description What people see, e.g. "PR */
            ref: string;
            uri?: string;
            locator?: {
                [key: string]: unknown;
            };
            /**
             * @description Third-party content. URL, email and issue sources are external
             *     whatever this says.
             */
            external?: boolean;
            /**
             * @description The source's class as you assert it: `person` for your own
             *     words (your global instructions file), `agent_own_work` for an
             *     agent's notes. Memax holds it to what you could write yourself
             *     (an agent's own work at most, for an agent); without it, a file
             *     or PR is `repository` and an import `external`. An import holds
             *     a repository file's statements to `repository`, whatever this
             *     says.
             */
            trust?: components["schemas"]["Trust"];
            /** @description The supporting excerpt. */
            quote?: string;
            content_hash?: string;
        };
        /** @description A draft statement to check before remembering it. */
        NearDuplicatesRequest: {
            /** @description The draft, as the person has typed it so far. */
            statement: string;
            /** @description The most repeats to return (default 3). */
            limit?: number;
        };
        /** @description What a draft repeats. */
        NearDuplicates: {
            /** @description Best first. Exact repeats come before near ones. */
            items: components["schemas"]["NearDuplicate"][];
            /**
             * @description The draft was compared by meaning. `false` when embeddings are
             *     off on this server or the draft's embedding missed its deadline:
             *     only exact repeats were checked.
             */
            semantic: boolean;
            /** @description The least similarity a near repeat needed. */
            floor: number;
        };
        /** @description A memory a draft repeats. */
        NearDuplicate: {
            /** @description A kept memory or a pending proposal (its `lifecycle` says which). */
            memory: components["schemas"]["Memory"];
            /** @description Cosine similarity of the draft and the memory; 1 for an exact repeat. */
            similarity: number;
            match: components["schemas"]["DuplicateMatch"];
            /** @description The receipt that wrote the memory, so a client can say who proposed or kept it, through which agent, and when. */
            created: components["schemas"]["Receipt"];
        };
        /**
         * @description `exact`: the same words. `near`: the same thing by meaning.
         * @enum {string}
         */
        DuplicateMatch: "exact" | "near";
        RememberRequest: {
            /** @description One fact, in your words. */
            statement: string;
            section: components["schemas"]["Section"];
            /** @description Defaults to fact. */
            kind?: components["schemas"]["MemoryKind"];
            /** @description Only for kind decision. */
            decision?: components["schemas"]["Decision"];
            sources?: components["schemas"]["SourceInput"][];
            /** @description When it should be verified again. */
            stale_after?: components["schemas"]["Timestamp"];
            /** @description "Stays true while" conditions. */
            conditions?: unknown[];
            /** @description Where it applies, e.g. {"paths": ["packages/web/**"]}. */
            scope?: {
                [key: string]: unknown;
            };
            valid_from?: components["schemas"]["Timestamp"];
            valid_to?: components["schemas"]["Timestamp"];
            reason?: components["schemas"]["Reason"];
            /** @description When it happened on the client; offline queues keep the original time. */
            occurred_at?: components["schemas"]["Timestamp"];
            session_ref?: components["schemas"]["SessionRef"];
        };
        RequestDecisionRequest: {
            /** @description The decision, as one question. */
            question: string;
            /** @description Why it matters; the tradeoffs, and a recommendation if there is one. */
            context?: string;
            /** @description Two to four different answers, in order. */
            options: components["schemas"]["GateOption"][];
            /** @description When the agent stops waiting. Defaults to 7 days from now; between 5 minutes and 30 days. */
            expires_at?: components["schemas"]["Timestamp"];
            reason?: components["schemas"]["Reason"];
            /** @description When it happened on the client; offline queues keep the original time. */
            occurred_at?: components["schemas"]["Timestamp"];
            session_ref?: components["schemas"]["SessionRef"];
        };
        AnswerGateRequest: {
            /** @description The chosen option, counting from 1. */
            option: number;
            reason?: components["schemas"]["Reason"];
            /** @description When it happened on the client; offline queues keep the original time. */
            occurred_at?: components["schemas"]["Timestamp"];
            session_ref?: components["schemas"]["SessionRef"];
        };
        /** @description The body of keep, reject, compile and withdrawing a gate. Every field is optional. */
        ReviewRequest: {
            reason?: components["schemas"]["Reason"];
            /** @description When it happened on the client; offline queues keep the original time. */
            occurred_at?: components["schemas"]["Timestamp"];
            session_ref?: components["schemas"]["SessionRef"];
        };
        ResolveConflictRequest: {
            choice: components["schemas"]["ConflictChoice"];
            /** @description The other side, by display ID or id, when this memory has more than one conflict. */
            other?: components["schemas"]["MemoryRef"];
            /** @description keep_both only. Narrower words for this memory. */
            statement?: string;
            /** @description keep_both only. Narrower words for the other side. */
            other_statement?: string;
            reason?: components["schemas"]["Reason"];
            /** @description When it happened on the client; offline queues keep the original time. */
            occurred_at?: components["schemas"]["Timestamp"];
            session_ref?: components["schemas"]["SessionRef"];
        };
        AutonomyRequest: {
            autonomy: components["schemas"]["Autonomy"];
            reason?: components["schemas"]["Reason"];
            /** @description When it happened on the client; offline queues keep the original time. */
            occurred_at?: components["schemas"]["Timestamp"];
        };
        /** @description The body of pause, resume and disconnect. Every field is optional. */
        AgentCommandRequest: {
            reason?: components["schemas"]["Reason"];
            /** @description When it happened on the client; offline queues keep the original time. */
            occurred_at?: components["schemas"]["Timestamp"];
        };
        EditRequest: {
            /** @description The new words. */
            statement: string;
            /** @description Moves the memory; omit to leave it where it is. */
            section?: components["schemas"]["Section"];
            /** @description Also keep a proposal after editing it (Review's edit and keep). */
            keep?: boolean;
            reason?: components["schemas"]["Reason"];
            /** @description When it happened on the client; offline queues keep the original time. */
            occurred_at?: components["schemas"]["Timestamp"];
            session_ref?: components["schemas"]["SessionRef"];
        };
        /** @description A memory item (`ref`) or a line of prose that cites at least one kept memory (`text`, `cites`). */
        BriefItemInput: {
            ref?: components["schemas"]["MemoryDisplayRef"];
            text?: string;
            cites?: components["schemas"]["MemoryDisplayRef"][];
        };
        BriefSectionInput: {
            key: components["schemas"]["SectionKey"];
            heading: string;
            items?: components["schemas"]["BriefItemInput"][];
        };
        ReviseBriefRequest: {
            title: string;
            /** @description One line under the title. */
            summary?: string;
            sections: components["schemas"]["BriefSectionInput"][];
            reason?: components["schemas"]["Reason"];
            /** @description When it happened on the client; offline queues keep the original time. */
            occurred_at?: components["schemas"]["Timestamp"];
            session_ref?: components["schemas"]["SessionRef"];
        };
        /** @description Settings to change; what you leave out stays. */
        TargetSettingsInput: {
            include?: components["schemas"]["IncludeMode"];
            stale?: components["schemas"]["StaleMode"];
            size_budget?: number;
            scoped?: components["schemas"]["ScopedMode"];
            user_owned?: boolean;
        };
        CreateTargetRequest: {
            kind: components["schemas"]["TargetKind"];
            /** @description Defaults to the kind's own (AGENTS.md, CLAUDE.md, .cursor/rules, …). ChatGPT takes none. */
            path?: components["schemas"]["RepoPath"];
            settings?: components["schemas"]["TargetSettingsInput"];
            /** @description Defaults to local for files and copy for ChatGPT. */
            delivery?: components["schemas"]["Delivery"];
            reason?: components["schemas"]["Reason"];
            occurred_at?: components["schemas"]["Timestamp"];
            session_ref?: components["schemas"]["SessionRef"];
        };
        ConfigureTargetRequest: {
            path?: components["schemas"]["RepoPath"];
            settings?: components["schemas"]["TargetSettingsInput"];
            delivery?: components["schemas"]["Delivery"];
            /** @description false stops compiling the target; true turns it back on. */
            enabled?: boolean;
            reason?: components["schemas"]["Reason"];
            occurred_at?: components["schemas"]["Timestamp"];
            session_ref?: components["schemas"]["SessionRef"];
        };
        ObservationRequest: {
            /** @description The file, relative to the repository root. */
            path: components["schemas"]["RepoPath"];
            /** @description The file's content as it is on disk. */
            content: string;
            /** @description The device that saw it, for the record. */
            device_id?: string;
            /** @description The commit the file is at, if the device knows it. */
            commit?: string;
            reason?: components["schemas"]["Reason"];
            /** @description When the device saw it. */
            occurred_at?: components["schemas"]["Timestamp"];
            session_ref?: components["schemas"]["SessionRef"];
        };
        DeliveryRequest: {
            /** @description The run you wrote, by display ID (C-0881) or id. */
            compile: string;
            /** @description The run's drift hash, computed from what you wrote. */
            sha256: components["schemas"]["Sha256"];
            reason?: components["schemas"]["Reason"];
            /** @description When it was written. */
            occurred_at?: components["schemas"]["Timestamp"];
            session_ref?: components["schemas"]["SessionRef"];
        };
        /** @description Every field is optional. */
        ResolveDriftRequest: {
            /** @description Resolve only this file's edit; without it, every open one of the target. */
            observation?: components["schemas"]["Id"];
            reason?: components["schemas"]["Reason"];
            occurred_at?: components["schemas"]["Timestamp"];
            session_ref?: components["schemas"]["SessionRef"];
        };
        /** @description A person's question. */
        AskRequest: {
            /** @description The question, as the person typed it. */
            question: string;
        };
        /**
         * @description One event of an answer's stream: `event` is the server-sent event's
         *     name and `data` its JSON payload. The stream is one `sources`, then
         *     `delta` and `cite` in answer order, then `done` (or `error`).
         */
        AskEvent: components["schemas"]["AskSourcesEvent"] | components["schemas"]["AskDeltaEvent"] | components["schemas"]["AskCiteEvent"] | components["schemas"]["AskDoneEvent"] | components["schemas"]["AskErrorEvent"];
        AskSourcesEvent: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            event: "sources";
            data: components["schemas"]["AskSources"];
        };
        AskDeltaEvent: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            event: "delta";
            data: components["schemas"]["AskDelta"];
        };
        AskCiteEvent: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            event: "cite";
            data: components["schemas"]["AskCite"];
        };
        AskDoneEvent: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            event: "done";
            data: components["schemas"]["AskDone"];
        };
        AskErrorEvent: {
            /**
             * @description discriminator enum property added by openapi-typescript
             * @enum {string}
             */
            event: "error";
            data: components["schemas"]["AskFailure"];
        };
        /** @description The kept memories the answer may cite, best match first. Sent once, before any words. */
        AskSources: {
            sources: components["schemas"]["AskSource"][];
            /** @description `false` when answers are off on this server: `done` follows at once, with outcome `sources_only`. */
            answering: boolean;
            /** @description The search matched words, not meaning (embeddings off, or the query's embedding was late). */
            lexical_only: boolean;
        };
        /** @description One kept memory an answer may cite. */
        AskSource: {
            id: components["schemas"]["Id"];
            ref: components["schemas"]["DisplayRef"];
            statement: string;
            section: components["schemas"]["Section"];
            kind: components["schemas"]["MemoryKind"];
            state: components["schemas"]["State"];
            trust: components["schemas"]["Trust"];
            version: number;
            /** @description How it came to read as it does, for the source's receipt line ("kept by ZZ · Oct 2"). */
            receipt?: components["schemas"]["Receipt"];
        };
        /** @description The answer's next words, citations removed. */
        AskDelta: {
            text: string;
        };
        /** @description A citation at this point of the answer, of one of the sources. */
        AskCite: {
            /** @description The cited memory's number, in the order memories are first cited, from 1. */
            n: number;
            ref: components["schemas"]["DisplayRef"];
        };
        /**
         * @description How an answer ended. `answered`: words with at least one citation.
         *     `not_covered`: nothing kept matched, or the memories don't answer
         *     it. `unsupported`: the model answered, but no citation named a
         *     memory it was given; don't show it as an answer. `sources_only`:
         *     answers are off; the sources are the answer.
         * @enum {string}
         */
        AskOutcome: "answered" | "not_covered" | "unsupported" | "sources_only";
        /** @description The end of an answer. */
        AskDone: {
            outcome: components["schemas"]["AskOutcome"];
            /** @description The memories cited, in `n` order. */
            cited: components["schemas"]["DisplayRef"][];
            /** @description Citations removed because they named a memory the model wasn't given. */
            dropped: number;
            usage: components["schemas"]["AskUsage"];
        };
        /** @description What the answer used, and how long it took. */
        AskUsage: {
            /** @description The answer tier's model, when one answered. */
            model?: string;
            input_tokens: number;
            output_tokens: number;
            retrieval_ms: number;
            /** @description From the request to the answer's first words, when any came. */
            first_token_ms?: number;
            total_ms: number;
        };
        /**
         * @description `answer_failed`: the model failed or ran out of time partway; what streamed so far stays, and the person can ask again.
         * @enum {string}
         */
        AskFailureCode: "answer_failed";
        /** @description Why an answer stopped partway. */
        AskFailure: {
            code: components["schemas"]["AskFailureCode"];
            /** @description What happened and what to do, in English. */
            message: string;
        };
        /** @description The body of `:forget`. Every field is optional. */
        ForgetRequest: {
            /**
             * @description Your own note on why. It stays on the tombstone (it is not
             *     forgotten), so don't repeat the words.
             */
            note?: string;
            /** @description The display IDs that go with it, exactly as `forget_carries` listed them. */
            carries?: components["schemas"]["MemoryDisplayRef"][];
            /** @description When it happened on the client. */
            occurred_at?: components["schemas"]["Timestamp"];
            session_ref?: components["schemas"]["SessionRef"];
        };
        /**
         * @description Why a memory goes with another one's Forget. folded: a proposal the
         *     judge folded into it (a copy of its words). updates: a proposal that
         *     would change it. cites: built from it (a kept Ask answer citing it).
         *     space: everything in the space was forgotten.
         * @enum {string}
         */
        CarryReason: "folded" | "updates" | "cites" | "space";
        /** @description A memory that goes with a Forget. */
        ForgetCarry: {
            id: components["schemas"]["Id"];
            ref: components["schemas"]["DisplayRef"];
            reason: components["schemas"]["CarryReason"];
            /** @description The memory (or note) it goes with. */
            with: components["schemas"]["RecordRef"];
            lifecycle: components["schemas"]["Lifecycle"];
            kind: components["schemas"]["MemoryKind"];
        };
        ForgetResult: {
            outcome: components["schemas"]["Outcome"];
            policy: components["schemas"]["PolicyDecision"];
            /** @description The forgotten memory (no words, state forgotten). */
            memory: components["schemas"]["Memory"];
            /** @description The receipts the Forget wrote, oldest first. */
            receipts: components["schemas"]["Receipt"][];
            tombstone: components["schemas"]["Tombstone"];
            /** @description The memories forgotten with it. */
            memories: components["schemas"]["Memory"][];
        };
        /** @description What a Forget of a memory would do, before anyone confirms it. */
        ForgetPreview: {
            ref: components["schemas"]["DisplayRef"];
            /** @description The memory's version; send it as `If-Match`. */
            version: number;
            /** @description The memories that go with it; send their refs as `carries`. */
            carries: components["schemas"]["ForgetCarry"][];
            /** @description The compiled files and copy-outs that hold it, which would be rewritten. */
            files: components["schemas"]["TombstoneTarget"][];
            /** @description The agents that would be told on their next read. */
            agents: number;
            /** @description Of those agents, how many read it. */
            readers: number;
            /** @description Whether you may forget it (and everything that goes with it). */
            allowed: boolean;
            /** @description Why you may not, when policy says so. */
            policy?: components["schemas"]["PolicyDecision"];
        };
        /**
         * @description `memory`; `note` for a note (N-); or `space` for a Forget of everything in a space.
         * @enum {string}
         */
        TombstoneKind: "memory" | "space" | "note";
        /**
         * @description `propagating` until every copy has an answer (the SLO is a minute); then `done`.
         * @enum {string}
         */
        TombstoneStatus: "propagating" | "done";
        /**
         * @description asked: who asked. removed: the words left Memax. target: a compiled
         *     file (or copy-out) rewritten. artifacts: the stored copies of older
         *     compiles and hand edits re-rendered. caches: the caches purged.
         *     ledger: the forget ledger's copy (ids only) written. agent: an agent
         *     told on its next read.
         * @enum {string}
         */
        StepKind: "asked" | "removed" | "target" | "artifacts" | "caches" | "ledger" | "agent";
        /** @enum {string} */
        StepStatus: "done" | "waiting" | "held" | "failed" | "unreachable";
        /**
         * @description hand_edit: the file has a hand edit, which Memax never writes over.
         *     stopped: compiling the target was stopped. copy: copied out (ChatGPT);
         *     paste it again. delivery: compiled, waiting for the daemon (or a pull
         *     request) to write it. compiling: not compiled yet. paused: the agent
         *     is paused, and is told before it reads anything else. disconnected:
         *     the agent can't read any more. next_read: told on its next read.
         * @enum {string}
         */
        StepReason: "hand_edit" | "stopped" | "copy" | "delivery" | "compiling" | "paused" | "disconnected" | "next_read";
        /**
         * @description A copy Memax can't reach. git_history: earlier commits of the
         *     compiled files. agent_memory: what agents saved in their own memory.
         *     backups: database backups, kept for `days` and treated as beyond use
         *     (the forget ledger is re-applied after any restore). llm: model
         *     providers that processed the words. hand_edits: files with a hand
         *     edit Memax won't write over. copies: copy-outs a person pasted
         *     elsewhere.
         * @enum {string}
         */
        UnreachableKind: "git_history" | "agent_memory" | "backups" | "llm" | "hand_edits" | "copies";
        TombstoneActor: {
            /**
             * @description `person`, or `memax` when the forget ledger re-applied it after a restore.
             * @enum {string}
             */
            kind: "person" | "memax";
            id?: components["schemas"]["Id"];
        };
        TombstoneAgent: {
            connection_id: components["schemas"]["Id"];
            agent?: components["schemas"]["AgentKind"];
            display_name?: string;
            state?: components["schemas"]["AgentState"];
        };
        TombstoneTarget: {
            id: components["schemas"]["Id"];
            kind: components["schemas"]["TargetKind"];
            label: string;
            delivery: components["schemas"]["Delivery"];
        };
        /** @description What Forget took out of Memax. */
        TombstoneGone: {
            versions: number;
            sources: number;
            embeddings: number;
            verdicts: number;
            /** @description Verdicts a model gave (the judge's provider saw the words). */
            model_verdicts: number;
            gates: number;
            /** @description Compiled files that held it. */
            files: number;
            /** @description Memories forgotten (a space's Forget). */
            memories?: number;
        };
        TombstoneStep: {
            kind: components["schemas"]["StepKind"];
            status: components["schemas"]["StepStatus"];
            at?: components["schemas"]["Timestamp"];
            target?: components["schemas"]["TombstoneTarget"];
            /** @description The compile run that rewrote the target. */
            compile?: components["schemas"]["CompileRef"];
            agent?: components["schemas"]["TombstoneAgent"];
            reason?: components["schemas"]["StepReason"];
            /** @description How many stored copies were re-rendered. */
            count?: number;
            /** @description The agent read it (else it is connected to the space). */
            read_it?: boolean;
        };
        Processor: {
            name: string;
            /** @enum {string} */
            purpose: "embeddings" | "judge" | "ask";
            zero_retention: boolean;
        };
        UnreachableCopy: {
            kind: components["schemas"]["UnreachableKind"];
            repositories?: string[];
            files?: string[];
            agents?: string[];
            days?: number;
            processors?: components["schemas"]["Processor"][];
            targets?: string[];
        };
        /** @description What Forget did to one memory (or a whole space). Never words. */
        Tombstone: {
            id: components["schemas"]["Id"];
            /** @description The Forget it belongs to (the primary tombstone's id). */
            op_id: components["schemas"]["Id"];
            /** @description The forgotten memory's or note's display ID, or `space`. */
            ref: string;
            kind: components["schemas"]["TombstoneKind"];
            object_id: components["schemas"]["Id"];
            space_id: components["schemas"]["Id"];
            tenant_id: components["schemas"]["Id"];
            carried?: components["schemas"]["CarryReason"];
            /** @description The memory it went with, when carried. */
            primary?: string;
            /** @description The other display IDs forgotten in the same Forget. */
            with: string[];
            /** @description The person's note. */
            note?: string;
            by: components["schemas"]["TombstoneActor"];
            /** @description The agent whose request led to it. */
            requested_by?: components["schemas"]["TombstoneAgent"];
            via: components["schemas"]["Via"];
            receipt_id: components["schemas"]["Id"];
            forgotten_at: components["schemas"]["Timestamp"];
            kept_at?: components["schemas"]["Timestamp"];
            /** @description Reads before it was forgotten, directly and in compiles that held it. */
            reads_before: number;
            gone: components["schemas"]["TombstoneGone"];
            /** @description How many agents are told, each once on its next read. */
            agents: number;
            status: components["schemas"]["TombstoneStatus"];
            completed_at?: components["schemas"]["Timestamp"];
            /** @description From the Forget to the last step's answer. */
            duration_ms?: number;
            /** @description When the forget ledger re-applied it after a restore. */
            reapplied_at?: components["schemas"]["Timestamp"];
            /** @description How it was forgotten, in order. Empty in a list. */
            steps: components["schemas"]["TombstoneStep"][];
            /** @description The copies Memax can't reach. Empty in a list. */
            unreachable: components["schemas"]["UnreachableCopy"][];
        };
        TombstonePage: {
            tombstones: components["schemas"]["Tombstone"][];
            next_cursor?: components["schemas"]["Cursor"];
            has_more: boolean;
        };
        /** @enum {string} */
        ForgetRequestStatus: "waiting" | "forgotten" | "declined";
        /** @description An agent's request that a person forget a memory. */
        ForgetRequestRecord: {
            id: components["schemas"]["Id"];
            memory_id: components["schemas"]["Id"];
            ref: components["schemas"]["DisplayRef"];
            space_id: components["schemas"]["Id"];
            agent: components["schemas"]["TombstoneAgent"];
            session_ref?: string;
            /** @description The agent's reason, from the request's receipt (redacted if the memory is forgotten). */
            reason?: string;
            status: components["schemas"]["ForgetRequestStatus"];
            decided_by?: components["schemas"]["Id"];
            decided_at?: components["schemas"]["Timestamp"];
            receipt_id: components["schemas"]["Id"];
            requested_at: components["schemas"]["Timestamp"];
        };
        ForgetRequestResult: {
            outcome: components["schemas"]["Outcome"];
            policy: components["schemas"]["PolicyDecision"];
            memory: components["schemas"]["Memory"];
            /** @description The request's receipt; empty when it was already waiting. */
            receipts: components["schemas"]["Receipt"][];
            forget_request: components["schemas"]["ForgetRequestRecord"];
        };
        /**
         * @description forgotten: memories forgotten since the agent read them (or in a
         *     space it is connected to). space_forgotten: everything in a space.
         *     switched: a space it is connected to moved to the V2 record, and
         *     `autonomy` is what it may do there now.
         * @enum {string}
         */
        NoticeKind: "forgotten" | "space_forgotten" | "switched";
        /** @description Something an agent is told once, on its next read. */
        Notice: {
            id: components["schemas"]["Id"];
            space_id: components["schemas"]["Id"];
            /** @description The Forget (its tombstone); absent for `switched`. */
            op_id?: components["schemas"]["Id"];
            /** @description What the agent may do in the space now (`switched`). */
            autonomy?: components["schemas"]["Autonomy"];
            kind: components["schemas"]["NoticeKind"];
            /** @description The forgotten display IDs; empty when a whole space was forgotten. */
            refs: string[];
            /** @description The agent read it (else it is connected to the space). */
            read_it: boolean;
            at: components["schemas"]["Timestamp"];
            /** @description The space's name, when the agent can still read it. */
            space?: string;
        };
        NoticeList: {
            notices: components["schemas"]["Notice"][];
        };
        AckNoticesRequest: {
            ids: components["schemas"]["Id"][];
        };
        AckNoticesResult: {
            /** @description How many were marked told now. */
            acknowledged: number;
        };
        CreateSpaceRequest: {
            name: string;
            /** @description Lowercase letters, digits and hyphens, 4 to 50 characters. Without one, Memax picks one from the name. */
            slug?: string;
            /**
             * @description Only project spaces are created here; the default.
             * @enum {string}
             */
            kind?: "project";
            /** @description The repository the space compiles for, "owner/name". */
            repository?: string;
        };
        /**
         * @description Where a file an import read lives: `repository` (shared with
         *     everyone who clones it) or `home` (on the person's machine only);
         *     `v1` for a space's own V1 memory, imported when it switched to V2
         *     (clients don't send it).
         * @enum {string}
         */
        ImportLocation: "repository" | "home" | "v1";
        /**
         * @description What became of one statement: `proposed` (a new proposal),
         *     `folded` (it repeats another statement of the upload, whose proposal
         *     cites it too), `existing` (the space already has it) or `refused`
         *     (policy refused it; see `policy`).
         * @enum {string}
         */
        ImportOutcome: "proposed" | "folded" | "existing" | "refused";
        /**
         * @description Why the client kept a statement on the machine: `secret` (it holds
         *     a credential), `too_long` (longer than a statement can be) or
         *     `limit` (the file has more statements than one import takes).
         * @enum {string}
         */
        ImportSkipReason: "secret" | "too_long" | "limit";
        /**
         * @description How far the import's conflict check got: `pending`, `checked`,
         *     `no_model` (no model is configured: only the judge's repeats check
         *     ran), `failed` (the model gave no answer; nothing was flagged) or
         *     `skipped` (fewer than two proposals to compare).
         * @enum {string}
         */
        ImportCheckState: "pending" | "checked" | "no_model" | "failed" | "skipped";
        /**
         * @description Why a proposal can't be kept in bulk: `decided` (it isn't waiting in
         *     Review any more), `conflict` (it disagrees with another statement or
         *     a decision in force), `quarantined` (it cites an outside source),
         *     `checking` (the judge or the conflict check hasn't finished),
         *     `unchecked` (the judge couldn't reach a verdict), `stale`, or
         *     `hidden_characters` (its line had hidden characters: read it first).
         * @enum {string}
         */
        ImportHeld: "decided" | "conflict" | "quarantined" | "checking" | "hidden_characters" | "stale" | "unchecked";
        /**
         * @description How a disagreement is settled: `keep_one`, `keep_all`, `leave_open`
         *     or `keep_suggestion`.
         * @enum {string}
         */
        ImportChoice: "keep_one" | "keep_all" | "leave_open" | "keep_suggestion";
        /** @description The client's name for a file's role, e.g. claude_md, agents_md, cursor_rule, claude_memory, codex_memory. */
        ImportFileKind: string;
        /** @description One file an import read. Never its contents. */
        ImportFile: {
            /** @description What people see, e.g. "CLAUDE.md" or "~/.codex/memories/notes.md". */
            path: string;
            kind: components["schemas"]["ImportFileKind"];
            /** @description The agent the file belongs to, e.g. claude-code. */
            agent?: string;
            location: components["schemas"]["ImportLocation"];
            /** @description The class the file's statements were sent at. */
            trust?: components["schemas"]["Trust"];
            /** @description The statements found in it. */
            statements: number;
            /** @description The statements the client kept on the machine. */
            skipped: number;
            /** @description The hidden characters the client removed from it. */
            hidden_characters: number;
        };
        /** @description A statement the client didn't send, and why. A ref and a rule, never its words. */
        ImportSkip: {
            /** @description Where it is, e.g. "CLAUDE.md:31". */
            ref: string;
            reason: components["schemas"]["ImportSkipReason"];
            /** @description The rule that matched, e.g. "GitHub token". Never the matched text. */
            detail?: string;
        };
        /** @description One statement of an import. */
        ImportItemInput: {
            /** @description Unique within the request; the item's result carries it back. */
            key: string;
            /** @description Where it came from, e.g. "CLAUDE.md:12". Defaults to its first source's ref. */
            ref?: string;
            location: components["schemas"]["ImportLocation"];
            /** @description The hidden characters the client removed from it. */
            hidden_characters?: number;
            statement: string;
            section: components["schemas"]["Section"];
            kind?: components["schemas"]["MemoryKind"];
            decision?: components["schemas"]["Decision"];
            sources?: components["schemas"]["SourceInput"][];
            /** @description Where it applies, e.g. {"paths": ["**\/*.test.ts"]} from a scoped rule. */
            scope?: {
                [key: string]: unknown;
            };
        };
        ImportRequest: {
            /** @description The uploader, e.g. "memax-cli 0.3.0". */
            client?: string;
            files?: components["schemas"]["ImportFile"][];
            skipped?: components["schemas"]["ImportSkip"][];
            items: components["schemas"]["ImportItemInput"][];
            /** @description When the files were read. */
            occurred_at?: components["schemas"]["Timestamp"];
            /** @description The run that read them, e.g. "init-7f3a"; every statement's receipt carries it. */
            session_ref?: components["schemas"]["SessionRef"];
        };
        /** @description An import's statements by outcome, and its disagreements. */
        ImportCounts: {
            items: number;
            proposed: number;
            folded: number;
            existing: number;
            refused: number;
            conflicts: number;
        };
        ImportCheck: {
            state: components["schemas"]["ImportCheckState"];
            checked_at?: components["schemas"]["Timestamp"];
            tier?: components["schemas"]["ModelTier"];
            model?: string;
        };
        /** @description One upload of statements read from agent files, or a space's own V1 memories when it switched. */
        Import: {
            id: components["schemas"]["Id"];
            space_id: components["schemas"]["Id"];
            tenant_id: components["schemas"]["Id"];
            actor_kind: components["schemas"]["ActorKind"];
            /** @description The person, or the agent connection, that uploaded it. */
            actor_id?: components["schemas"]["Id"];
            agent?: string;
            client?: string;
            files: components["schemas"]["ImportFile"][];
            skipped: components["schemas"]["ImportSkip"][];
            counts: components["schemas"]["ImportCounts"];
            /** @description When every statement was written. */
            uploaded_at?: components["schemas"]["Timestamp"];
            check: components["schemas"]["ImportCheck"];
            /**
             * @description `init`, statements `memax init` read from agent files; or `v1`, the
             *     person's own V1 memories, offered for bulk keep when the space
             *     switched to V2 (Review's "From V1"; each item's ref is its note).
             * @enum {string}
             */
            origin: "init" | "v1";
            created_at: components["schemas"]["Timestamp"];
        };
        /** @description What became of one statement of the request. */
        ImportItemResult: {
            key: string;
            /** @description Its index in the request. */
            position: number;
            ref: string;
            outcome: components["schemas"]["ImportOutcome"];
            /** @description The proposal it became (folded, the one it folded into), or the memory the space already had. */
            memory?: components["schemas"]["MemoryPointer"];
            /** @description That memory's lifecycle, for an existing one. */
            lifecycle?: components["schemas"]["Lifecycle"];
            /** @description The key of the item it folded into. */
            folded_into?: string;
            /** @description Why it was refused. */
            policy?: components["schemas"]["PolicyDecision"];
        };
        ImportResult: {
            import: components["schemas"]["Import"];
            items: components["schemas"]["ImportItemResult"][];
        };
        /** @description What became of one statement, as the import shows it. */
        ImportItem: {
            position: number;
            key: string;
            ref: string;
            location: components["schemas"]["ImportLocation"];
            outcome: components["schemas"]["ImportOutcome"];
            memory?: components["schemas"]["MemoryPointer"];
            /** @description The position of the item it folded into. */
            folded_into?: number;
            /** @description The policy code it was refused with. */
            code?: string;
            hidden_characters: number;
        };
        /** @description A memory an import proposed or found, and whether it can be kept in bulk. */
        ImportMemory: {
            memory: components["schemas"]["Memory"];
            /** @description proposed (this import proposed it) or existing. */
            outcome: components["schemas"]["ImportOutcome"];
            /** @description The import's statements it stands for, folded repeats included ("Three files agree"). */
            items: number;
            /** @description It can be kept in bulk with the ones that agree. */
            bulk: boolean;
            held?: components["schemas"]["ImportHeld"];
            /** @description The import disagreement it is in, by its `n`. */
            conflict?: number;
        };
        /** @description A disagreement among an import's proposals (Test command, 3 files disagree). */
        ImportConflict: {
            id: components["schemas"]["Id"];
            /** @description Its number within the import ("1 of 3"). */
            n: number;
            /** @description What they disagree about, in the model's words. */
            subject?: string;
            /** @description How they disagree, in the model's words. */
            rationale?: string;
            /** @description One statement that says what holds for all of them, when the model found one. */
            suggestion?: string;
            confidence?: number;
            /** @description The proposals that disagree, in the import's order. */
            members: components["schemas"]["MemoryPointer"][];
            /** @enum {string} */
            state: "open" | "settled";
            choice?: components["schemas"]["ImportChoice"];
            /** @description The memory that holds (keep_one, keep_suggestion). */
            chosen?: components["schemas"]["MemoryPointer"];
            created_receipt_id: components["schemas"]["Id"];
            last_receipt_id: components["schemas"]["Id"];
            created_at: components["schemas"]["Timestamp"];
            settled_at?: components["schemas"]["Timestamp"];
        };
        /** @description How far the judge got with the import's proposals. */
        ImportProgress: {
            proposals: number;
            working: number;
            judged: number;
            failed: number;
            /** @description Nothing is being judged and the conflict check is done. */
            ready: boolean;
        };
        ImportView: {
            import: components["schemas"]["Import"];
            items: components["schemas"]["ImportItem"][];
            /** @description The memories the items became or matched, once each, in the order the items first name them. */
            memories: components["schemas"]["ImportMemory"][];
            conflicts: components["schemas"]["ImportConflict"][];
            progress: components["schemas"]["ImportProgress"];
        };
        ImportPage: {
            items: components["schemas"]["Import"][];
            has_more: boolean;
            next_cursor?: components["schemas"]["Cursor"];
        };
        SettleImportConflictRequest: {
            choice: components["schemas"]["ImportChoice"];
            /** @description keep_one only. The member to keep. */
            keep?: components["schemas"]["MemoryRef"];
            /** @description keep_suggestion only. The words to keep; without them, the check's suggestion. */
            statement?: string;
            reason?: components["schemas"]["Reason"];
            /** @description When it happened on the client; offline queues keep the original time. */
            occurred_at?: components["schemas"]["Timestamp"];
            session_ref?: components["schemas"]["SessionRef"];
        };
        ImportConflictResult: {
            outcome: components["schemas"]["Outcome"];
            policy: components["schemas"]["PolicyDecision"];
            conflict: components["schemas"]["ImportConflict"];
            /** @description Every memory it changed, the members first. */
            memories: components["schemas"]["Memory"][];
            /** @description The receipts it wrote, oldest first. */
            receipts: components["schemas"]["Receipt"][];
        };
        BulkReviewItemInput: {
            memory: components["schemas"]["MemoryRef"];
            /** @description The version the person saw (its ETag); a newer one is reported as `edit_clash`. */
            version?: number;
        };
        BulkReviewRequest: {
            items: components["schemas"]["BulkReviewItemInput"][];
            reason?: components["schemas"]["Reason"];
            /** @description When it happened on the client; offline queues keep the original time. */
            occurred_at?: components["schemas"]["Timestamp"];
            session_ref?: components["schemas"]["SessionRef"];
        };
        /**
         * @description `applied` (kept, or rejected), `refused` (policy; see `policy`) or `failed` (see `error`).
         * @enum {string}
         */
        BulkOutcome: "applied" | "refused" | "failed";
        BulkReviewItem: {
            /** @description The memory as the request named it. */
            memory: string;
            ref?: components["schemas"]["DisplayRef"];
            outcome: components["schemas"]["BulkOutcome"];
            /** @description Its state now. */
            state?: components["schemas"]["State"];
            version?: number;
            policy?: components["schemas"]["PolicyDecision"];
            /** @description Why it failed, as the one-memory command would have answered. */
            error?: components["schemas"]["Error"];
        };
        BulkReviewResult: {
            items: components["schemas"]["BulkReviewItem"][];
            applied: number;
            refused: number;
            failed: number;
        };
        /**
         * @description v1: on V1, never switched. running: switching (in the background).
         *     switched: on the V2 record. failed: a step failed; send `:switch`
         *     again to resume it. off: switched back to V1.
         * @enum {string}
         */
        SwitchState: "v1" | "running" | "switched" | "failed" | "off";
        /**
         * @description The step a switch stands at (the next to run), or `done`.
         * @enum {string}
         */
        SwitchStep: "space" | "notes" | "personas" | "configs" | "candidates" | "agents" | "gates" | "switch" | "done";
        /** @description The body of `:switch`. Every field is optional. */
        SwitchSpaceRequest: {
            /**
             * @description `v2` (the default) switches the space to the V2 record; `v1` switches it back.
             * @enum {string}
             */
            to?: "v2" | "v1";
            /**
             * @description The kind it switches as. A V1 team hub may switch as a project space while it has no V2 record.
             * @enum {string}
             */
            kind?: "project" | "team";
            /** @description The repository it compiles for ("owner/name"); empty clears it. */
            repository?: string;
        };
        /** @description A person in the space, with their V1 role and the V2 role it maps to. */
        SwitchMember: {
            person_id: components["schemas"]["Id"];
            name: string;
            /** @description owner, admin, contributor or viewer. */
            v1_role: string;
            role: components["schemas"]["Role"];
            /** @description An admin in V1 is a member who can forget. */
            can_forget: boolean;
        };
        /** @description What the space's V1 memories become. */
        SwitchNotes: {
            /** @description The V1 memories that become notes. */
            total: number;
            /** @description Of those, the ones people wrote. */
            person: number;
            /** @description Of those, the ones agents wrote. */
            agent: number;
            /** @description A person's own, short enough to be one statement, offered for bulk keep. */
            candidates: number;
            /** @description For Dream to fold into proposals (agents', and documents longer than one statement). */
            fold: number;
            /** @description Notes only, never proposed (archived in V1, or holding a credential). */
            kept: number;
            /** @description A person's notes longer than one statement. */
            long: number;
            /** @description Notes holding a credential. */
            secret: number;
            /** @description Notes archived in V1. */
            archived: number;
            /** @description A person's notes that are files or pages, not typed text. */
            format: number;
            /** @description Notes from the web or an email (quarantined when proposed). */
            external: number;
            /** @description V1's onboarding memories, left out. */
            seeds: number;
        };
        /** @description A V1 agent file that belongs to the space. */
        SwitchConfig: {
            path: string;
            agent: string;
            /** @description V1's sync scope (global, profile:<name> or project:<url>). */
            scope: string;
            /** @description The compile targets it stands for. */
            targets: components["schemas"]["TargetKind"][];
        };
        /** @description A V1 API key or OAuth grant the switch connects to the space (or found connected). */
        SwitchAgent: {
            credential: components["schemas"]["CredentialKind"];
            name: string;
            agent: components["schemas"]["AgentKind"];
            /** @description The person it works for. */
            person_id: components["schemas"]["Id"];
            /** @description What it may do in the space after the switch. */
            autonomy: components["schemas"]["Autonomy"];
            /** @description It was connected to the space already, and keeps its level. */
            connected: boolean;
        };
        /** @description What switching the space moves, read fresh from V1. Nothing changes. */
        SwitchPreview: {
            kind: components["schemas"]["SpaceKind"];
            /** @description The kinds it may switch as. */
            kinds: components["schemas"]["SpaceKind"][];
            repository?: string;
            /** @description The repository most of its V1 memories came from. */
            suggested_repository?: string;
            members: components["schemas"]["SwitchMember"][];
            notes: components["schemas"]["SwitchNotes"];
            /** @description Personas that become notes (the personal space). */
            personas: number;
            configs: components["schemas"]["SwitchConfig"][];
            /** @description The compile targets the agent files stand for. */
            targets: components["schemas"]["TargetKind"][];
            agents: components["schemas"]["SwitchAgent"][];
            /** @description Decisions waiting on V1's board, which move to the record when their agent is connected. */
            gates: number;
            /** @description V1 Dream runs, kept as read-only history. */
            dream_runs: number;
            /** @description The V1 plan, grandfathered. */
            plan?: string;
            /** @description Nothing to import, so it switches within the request. */
            empty: boolean;
        };
        /** @description What the switch's steps did so far. */
        SwitchProgress: {
            /** @description V1 memories numbered as notes. */
            notes: number;
            personas: number;
            /** @description Agent files numbered as notes. */
            configs: number;
            /** @description Compile targets added. */
            targets: components["schemas"]["TargetKind"][];
            /** @description A person's own V1 memories proposed for bulk keep. */
            proposed: number;
            /** @description Repeats among them, folded into one proposal. */
            folded: number;
            /** @description Already in the record. */
            existing: number;
            refused: number;
            /** @description The V1 imports (at most 500 statements each). */
            imports: components["schemas"]["Id"][];
            /** @description Agents connected to the space. */
            connected: number;
            already_connected: number;
            /** @description Agents told on their next MCP response. */
            notified: number;
            gates_moved: number;
            /** @description Decisions left on V1's board (their agent isn't connected). */
            gates_left: number;
        };
        /** @description Where a space's Switch to V2 stands. */
        SpaceSwitch: {
            space: components["schemas"]["Space"];
            state: components["schemas"]["SwitchState"];
            step: components["schemas"]["SwitchStep"];
            preview: components["schemas"]["SwitchPreview"];
            progress: components["schemas"]["SwitchProgress"];
            /** @description The V1 import, for Review's "From V1". */
            import_id?: components["schemas"]["Id"];
            /** @description Why a step failed (a code). */
            error?: string;
            attempts: number;
            started_at?: components["schemas"]["Timestamp"];
            switched_at?: components["schemas"]["Timestamp"];
            switched_back_at?: components["schemas"]["Timestamp"];
            /** @description It continues in the background. */
            background: boolean;
        };
        /**
         * @description Where a note's words live until cutover. memory, a V1 memory; persona, a V1 persona; agent_config, a synced agent file.
         * @enum {string}
         */
        NoteOrigin: "memory" | "persona" | "agent_config";
        /**
         * @description What the switch did with it. candidate: a person's own V1 memory,
         *     offered for bulk keep. fold: for Dream to fold into proposals. note:
         *     it stays a note, never proposed.
         * @enum {string}
         */
        NoteDisposition: "candidate" | "fold" | "note";
        /**
         * @description Why a note isn't a bulk-keep candidate. long: longer than one
         *     statement. secret: it holds a credential. archived: archived in V1.
         *     format: a file or a page, not typed text. external: from the web or
         *     an email.
         * @enum {string}
         */
        NoteHold: "long" | "secret" | "archived" | "format" | "external";
        /** @description A note (N-), raw material that is never compiled. */
        Note: {
            id: components["schemas"]["Id"];
            ref?: components["schemas"]["NoteRef"];
            space_id: components["schemas"]["Id"];
            /** @description Whose it is in V1 (the person, or the person the agent that wrote it worked for). */
            owner_id: components["schemas"]["Id"];
            origin: components["schemas"]["NoteOrigin"];
            title: string;
            /** @description The start of its words (280 characters). */
            excerpt: string;
            /**
             * @description person or agent.
             * @enum {string}
             */
            author_kind: "person" | "agent";
            agent?: string;
            /** @description V1's state (active, processing, archived). */
            state: string;
            disposition: components["schemas"]["NoteDisposition"];
            hold?: components["schemas"]["NoteHold"];
            trust?: components["schemas"]["Trust"];
            /** @description Where it came from (a source path, a persona's or agent file's path). */
            path?: string;
            /** @description Its length in characters. */
            length: number;
            /** @description How well it matches the search. */
            score?: number;
            created_at: components["schemas"]["Timestamp"];
            updated_at: components["schemas"]["Timestamp"];
        };
        NotePage: {
            items: components["schemas"]["Note"][];
        };
        /** @description The body of a note's `:forget`. Every field is optional. */
        ForgetNoteRequest: {
            /** @description Your own note on why. It stays on the tombstone, so don't repeat the words. */
            note?: string;
            /** @description The memories that go with it, exactly as `forget_carries` listed them. */
            carries?: components["schemas"]["MemoryDisplayRef"][];
        };
        NoteForgetResult: {
            outcome: components["schemas"]["Outcome"];
            policy: components["schemas"]["PolicyDecision"];
            receipts: components["schemas"]["Receipt"][];
            tombstone: components["schemas"]["Tombstone"];
            /** @description The memories forgotten with it. */
            memories: components["schemas"]["Memory"][];
        };
        /** @description What forgetting a note would do, before anyone confirms it. */
        NoteForgetPreview: {
            ref?: components["schemas"]["NoteRef"];
            carries: components["schemas"]["ForgetCarry"][];
            allowed: boolean;
            policy?: components["schemas"]["PolicyDecision"];
        };
        /** @description A Dream run from V1 (read-only history; no undo). */
        V1DreamRun: {
            id: components["schemas"]["Id"];
            status: string;
            mode: string;
            started_at: components["schemas"]["Timestamp"];
            finished_at?: components["schemas"]["Timestamp"];
            scanned: number;
            merged: number;
            contradictions: number;
            archived: number;
            organized: number;
            restructured: number;
            actions: number;
        };
        V1DreamRunList: {
            items: components["schemas"]["V1DreamRun"][];
        };
        Error: {
            code: components["schemas"]["ErrorCode"];
            /** @description What went wrong and what to do, in English. */
            message: string;
            details?: components["schemas"]["ErrorDetails"];
        };
        /** @description Machine-readable context; which keys appear depends on `code`. */
        ErrorDetails: {
            /** @description The field that is wrong (`invalid_request`). */
            field?: string;
            /** @description The policy decision (`refused`). */
            policy?: components["schemas"]["PolicyDecision"];
            /** @description The memory's or gate's display ID (`edit_clash`, `invalid_transition`, `judge_pending`), the decision in force a flagged proposal contradicts (`in_conflict`), or what is in an undo's way (`undo_refused`). */
            ref?: string;
            /** @description The gate's status now (`invalid_transition` on a gate). */
            status?: components["schemas"]["GateStatus"];
            /** @description Why an undo was refused (`undo_refused`). */
            reason?: components["schemas"]["UndoRefusal"];
            /** @description The version you sent (`edit_clash`). */
            expected_version?: number;
            /** @description The memory's version now (`edit_clash`). */
            current_version?: number;
            /** @description Seconds to wait (`rate_limited`, `busy`, `judge_pending`). */
            retry_after?: number;
            /** @description The rate limit (`rate_limited`), or the plan's asks a month (`refused` with policy `ask_limit`). */
            limit?: number;
            /** @description Requests counted so far (`rate_limited`), or asks this month (`ask_limit`). */
            current?: number;
            /** @description When the window resets (`rate_limited`), or when asks start again (`ask_limit`, the 1st, UTC). */
            reset_at?: components["schemas"]["Timestamp"];
            /** @description The memories that go with a Forget (`forget_carries`). */
            carries?: components["schemas"]["ForgetCarry"][];
            /** @description How a device's code ended (`invalid_transition` on a device code). */
            state?: components["schemas"]["DeviceAuthorizationState"];
        };
        /**
         * @description What one of an edition's actions did. fold: notes became lineage of
         *     a kept memory (its words unchanged). propose: a new fact from notes,
         *     waiting in Review. dedupe: a proposal that repeats one waiting was
         *     folded into it. conflict: a memory that contradicts a kept one was
         *     flagged. stale: a memory past its date was flagged. fade: a memory
         *     nobody read in 60 days faded (restorable). brief: a few small, cited
         *     changes to the Brief.
         * @enum {string}
         */
        DreamActionKind: "fold" | "propose" | "dedupe" | "conflict" | "stale" | "fade" | "brief";
        /** @enum {string} */
        DreamTrigger: "schedule" | "manual";
        /** @description An edition's display ID, unique per tenant. */
        EditionRef: string;
        /** @description A note's display ID, unique per tenant. */
        NoteDisplayRef: string;
        NoteAuthorCount: {
            /**
             * @description Who wrote the notes. `chat` is a note captured in a chat app.
             * @enum {string}
             */
            kind: "agent" | "person" | "chat";
            /** @description The agent's registry key, when an agent wrote them. */
            agent?: string;
            count: number;
        };
        /** @description The edition's actions by kind (those undone included). */
        DreamCounts: {
            fold: number;
            propose: number;
            dedupe: number;
            conflict: number;
            stale: number;
            fade: number;
            brief: number;
        };
        DreamBriefChange: {
            ref?: components["schemas"]["BriefRef"];
            version: number;
            /** @description How many small changes it made. */
            ops: number;
        };
        DreamUndone: {
            receipt_id: components["schemas"]["Id"];
            /** @description The person who undid it. */
            by?: components["schemas"]["Id"];
            at: components["schemas"]["Timestamp"];
        };
        /** @description One of an edition's actions, with the memory it is about as it is now. */
        DreamAction: {
            id: components["schemas"]["Id"];
            edition_id: components["schemas"]["Id"];
            edition_ref: components["schemas"]["EditionRef"];
            /** @description Its place in the edition, from 1. */
            n: number;
            kind: components["schemas"]["DreamActionKind"];
            /** @description The memory it changed or created (every kind but brief). */
            memory?: components["schemas"]["Memory"];
            /** @description That memory's version when Dream acted. */
            version?: number;
            /** @description The proposal a duplicate folded into (dedupe), or the other side (conflict). */
            related?: components["schemas"]["Memory"];
            /** @description The notes it rests on (fold, propose). */
            note_refs: components["schemas"]["NoteDisplayRef"][];
            brief?: components["schemas"]["DreamBriefChange"];
            receipt_ids: components["schemas"]["Id"][];
            undone?: components["schemas"]["DreamUndone"];
            /** @description Whether Undo may still apply; a later change can still refuse it. */
            undoable: boolean;
            created_at: components["schemas"]["Timestamp"];
        };
        /** @description Something the edition lists because it needs a person, though Dream didn't do it. */
        DreamSurfaced: {
            /** @enum {string} */
            kind: "conflict";
            memory: components["schemas"]["Memory"];
            with?: components["schemas"]["Memory"];
        };
        /** @description One edition (D-): what Dream read and what it did. */
        DreamEdition: {
            id: components["schemas"]["Id"];
            ref: components["schemas"]["EditionRef"];
            /** @description The edition's number ("No. 214"). */
            n: number;
            space_id: components["schemas"]["Id"];
            /** @description The night (or the run-now moment) it answers. */
            slot: components["schemas"]["Timestamp"];
            trigger: components["schemas"]["DreamTrigger"];
            /** @description The person who asked it to run now. */
            requested_by?: components["schemas"]["Id"];
            /** @description It read record changes after this (the previous edition's `until`). */
            since?: components["schemas"]["Timestamp"];
            until: components["schemas"]["Timestamp"];
            started_at: components["schemas"]["Timestamp"];
            finished_at: components["schemas"]["Timestamp"];
            /** @description How long the run took. */
            seconds: number;
            notes_read: number;
            notes_by: components["schemas"]["NoteAuthorCount"][];
            /** @description The notes it read, oldest first. */
            note_refs: components["schemas"]["NoteDisplayRef"][];
            /** @description The memories the notes became, folded into or proposed. */
            fact_refs: components["schemas"]["MemoryDisplayRef"][];
            counts: components["schemas"]["DreamCounts"];
            /** @description How many of its actions a person undid. */
            undone: number;
            /** @description Conflicts and stale facts it flagged or found that still wait on a person. */
            needs_you: number;
            /** @description Its `published` receipt. */
            receipt_id: components["schemas"]["Id"];
            /** @description Every action, in order (when one edition is read). */
            actions?: components["schemas"]["DreamAction"][];
            /** @description What it found that needs a person (when one edition is read). */
            surfaced?: components["schemas"]["DreamSurfaced"][];
        };
        DreamSchedule: {
            /** @enum {string} */
            cadence: "nightly" | "weekly";
            /** @description The owner's zone the next night is in ("UTC" until Memax knows it). */
            time_zone: string;
            next_at: components["schemas"]["Timestamp"];
        };
        DreamEditionPage: {
            items: components["schemas"]["DreamEdition"][];
            has_more: boolean;
            next_cursor?: components["schemas"]["Cursor"];
            schedule?: components["schemas"]["DreamSchedule"];
        };
        DreamActionPage: {
            items: components["schemas"]["DreamAction"][];
            has_more: boolean;
            next_cursor?: components["schemas"]["Cursor"];
        };
        DreamUndoResult: {
            outcome: components["schemas"]["Outcome"];
            policy: components["schemas"]["PolicyDecision"];
            action: components["schemas"]["DreamAction"];
            /** @description Every memory the undo changed. */
            memories: components["schemas"]["Memory"][];
            receipts: components["schemas"]["Receipt"][];
        };
        UndoEditionRequest: {
            kind: components["schemas"]["DreamActionKind"];
            reason?: components["schemas"]["Reason"];
        };
        DreamUndoRefusal: {
            action_id: components["schemas"]["Id"];
            /** @description Undo's reason (`UndoRefusal`), or `refused` when policy refused. */
            reason: string;
            message: string;
            ref?: string;
        };
        UndoEditionResult: {
            undone: components["schemas"]["DreamAction"][];
            refused: components["schemas"]["DreamUndoRefusal"][];
        };
        DreamRun: {
            queued: boolean;
            slot: components["schemas"]["Timestamp"];
            trigger: components["schemas"]["DreamTrigger"];
        };
        DreamSettings: {
            time_zone: string;
            /**
             * @description `default`: Memax doesn't know your zone yet (UTC). `observed`: from your app's clock. `set`: by you.
             * @enum {string}
             */
            time_zone_source: "default" | "observed" | "set";
            morning_email: boolean;
        };
        DreamSettingsRequest: {
            time_zone?: string;
            morning_email?: boolean;
        };
        UnsubscribeResult: {
            unsubscribed: boolean;
        };
        DreamEditionPageEnvelope: {
            data: components["schemas"]["DreamEditionPage"];
        };
        DreamEditionEnvelope: {
            data: components["schemas"]["DreamEdition"];
        };
        DreamActionPageEnvelope: {
            data: components["schemas"]["DreamActionPage"];
        };
        DreamUndoResultEnvelope: {
            data: components["schemas"]["DreamUndoResult"];
        };
        UndoEditionResultEnvelope: {
            data: components["schemas"]["UndoEditionResult"];
        };
        DreamRunEnvelope: {
            data: components["schemas"]["DreamRun"];
        };
        DreamSettingsEnvelope: {
            data: components["schemas"]["DreamSettings"];
        };
        UnsubscribeResultEnvelope: {
            data: components["schemas"]["UnsubscribeResult"];
        };
        ErrorEnvelope: {
            error: components["schemas"]["Error"];
        };
        ForgetResultEnvelope: {
            data: components["schemas"]["ForgetResult"];
        };
        TombstoneEnvelope: {
            data: components["schemas"]["Tombstone"];
        };
        ForgetPreviewEnvelope: {
            data: components["schemas"]["ForgetPreview"];
        };
        TombstonePageEnvelope: {
            data: components["schemas"]["TombstonePage"];
        };
        ForgetRequestResultEnvelope: {
            data: components["schemas"]["ForgetRequestResult"];
        };
        NoticeListEnvelope: {
            data: components["schemas"]["NoticeList"];
        };
        AckNoticesResultEnvelope: {
            data: components["schemas"]["AckNoticesResult"];
        };
        SpaceSwitchEnvelope: {
            data: components["schemas"]["SpaceSwitch"];
        };
        NotePageEnvelope: {
            data: components["schemas"]["NotePage"];
        };
        NoteEnvelope: {
            data: components["schemas"]["Note"];
        };
        NoteForgetResultEnvelope: {
            data: components["schemas"]["NoteForgetResult"];
        };
        NoteForgetPreviewEnvelope: {
            data: components["schemas"]["NoteForgetPreview"];
        };
        V1DreamRunListEnvelope: {
            data: components["schemas"]["V1DreamRunList"];
        };
        SpaceEnvelope: {
            data: components["schemas"]["Space"];
        };
        ImportResultEnvelope: {
            data: components["schemas"]["ImportResult"];
        };
        ImportViewEnvelope: {
            data: components["schemas"]["ImportView"];
        };
        ImportPageEnvelope: {
            data: components["schemas"]["ImportPage"];
        };
        ImportConflictResultEnvelope: {
            data: components["schemas"]["ImportConflictResult"];
        };
        BulkReviewResultEnvelope: {
            data: components["schemas"]["BulkReviewResult"];
        };
        SpaceListEnvelope: {
            data: components["schemas"]["SpaceList"];
        };
        MemoryPageEnvelope: {
            data: components["schemas"]["MemoryPage"];
        };
        ReviewPageEnvelope: {
            data: components["schemas"]["ReviewPage"];
        };
        ReceiptPageEnvelope: {
            data: components["schemas"]["ReceiptPage"];
        };
        MemoryDetailEnvelope: {
            data: components["schemas"]["MemoryDetail"];
        };
        CommandResultEnvelope: {
            data: components["schemas"]["CommandResult"];
        };
        NearDuplicatesEnvelope: {
            data: components["schemas"]["NearDuplicates"];
        };
        MemoriesCommandResultEnvelope: {
            data: components["schemas"]["MemoriesCommandResult"];
        };
        ConflictEnvelope: {
            data: components["schemas"]["Conflict"];
        };
        AgentListEnvelope: {
            data: components["schemas"]["AgentList"];
        };
        AgentDetailEnvelope: {
            data: components["schemas"]["AgentDetail"];
        };
        AgentCommandResultEnvelope: {
            data: components["schemas"]["AgentCommandResult"];
        };
        BriefEnvelope: {
            data: components["schemas"]["Brief"];
        };
        BriefResultEnvelope: {
            data: components["schemas"]["BriefResult"];
        };
        RestoreBriefResultEnvelope: {
            data: components["schemas"]["RestoreBriefResult"];
        };
        BriefVersionPageEnvelope: {
            data: components["schemas"]["BriefVersionPage"];
        };
        TargetListEnvelope: {
            data: components["schemas"]["TargetList"];
        };
        TargetResultEnvelope: {
            data: components["schemas"]["TargetResult"];
        };
        TargetPreviewEnvelope: {
            data: components["schemas"]["TargetPreview"];
        };
        CompileRunPageEnvelope: {
            data: components["schemas"]["CompileRunPage"];
        };
        ObservationResultEnvelope: {
            data: components["schemas"]["ObservationResult"];
        };
        DeliveryResultEnvelope: {
            data: components["schemas"]["DeliveryResult"];
        };
        DriftEnvelope: {
            data: components["schemas"]["Drift"];
        };
        DriftResolutionEnvelope: {
            data: components["schemas"]["DriftResolutionResult"];
        };
        GatePageEnvelope: {
            data: components["schemas"]["GatePage"];
        };
        GateEnvelope: {
            data: components["schemas"]["Gate"];
        };
        GateResultEnvelope: {
            data: components["schemas"]["GateResult"];
        };
        ReadPageEnvelope: {
            data: components["schemas"]["ReadPage"];
        };
        CompileLoadResultEnvelope: {
            data: components["schemas"]["CompileLoadResult"];
        };
        CheckpointPageEnvelope: {
            data: components["schemas"]["CheckpointPage"];
        };
        DeviceCodeRequest: {
            /** @description The code the terminal shows, e.g. "WQRT-4821". Case, spaces and the dash don't matter. */
            user_code: string;
        };
        /**
         * @description Where a device's code stands: `pending` (waiting for a person),
         *     `approved` (confirmed; the device hasn't collected its session
         *     yet), `signed_in` (the device collected it), `denied` or `expired`
         *     (its 10 minutes are up).
         * @enum {string}
         */
        DeviceAuthorizationState: "pending" | "approved" | "signed_in" | "denied" | "expired";
        /**
         * @description A device asking to sign in with a code. Everything about the device
         *     but `address` is what it says about itself.
         */
        DeviceAuthorization: {
            /** @description The code, as people read it ("WQRT-4821"). */
            user_code: string;
            state: components["schemas"]["DeviceAuthorizationState"];
            /** @description The client asking; only the memax CLI (`memax-cli`) signs in this way. */
            client_id: string;
            /** @description The client's version, e.g. "2.0.0". */
            client_version?: string;
            /** @description The machine's name, e.g. "ziyang-mbp". */
            device_name?: string;
            /**
             * @description The machine's system.
             * @enum {string}
             */
            device_os?: "macOS" | "Linux" | "Windows" | "FreeBSD" | "other";
            /** @description The space the CLI will use, by slug; it can switch in the terminal. */
            space?: string;
            /** @description The address the request came from, as Memax saw it. */
            address?: string;
            requested_at: components["schemas"]["Timestamp"];
            expires_at: components["schemas"]["Timestamp"];
            /** @description When you confirmed or declined it. */
            decided_at?: components["schemas"]["Timestamp"];
            /** @description When the device collected its session. */
            signed_in_at?: components["schemas"]["Timestamp"];
        };
        DeviceAuthorizationEnvelope: {
            data: components["schemas"]["DeviceAuthorization"];
        };
        /**
         * @description What signed in. `web`: the web app. `cli`: the memax CLI, through a
         *     browser login or the email code. `device`: the memax CLI with a
         *     device code you confirmed on the web. `mcp`: an MCP client you
         *     authorized. Only a `web` session, through the web app, can keep as
         *     a person on the web (`human_web`).
         * @enum {string}
         */
        SessionSurface: "web" | "cli" | "device" | "mcp";
        /** @description One place you are signed in. */
        Session: {
            id: components["schemas"]["Id"];
            surface: components["schemas"]["SessionSurface"];
            /**
             * @description The client, in words: the browser and system for the web app
             *     ("Chrome on macOS"), the CLI and its version, or, for a device,
             *     what it said about itself ("memax CLI 2.0.0 on ziyang-mbp
             *     (macOS)"), and an MCP client's registered name.
             */
            client: string;
            /** @description The agent an MCP session is for, e.g. `claude-ai`. */
            agent?: string;
            /** @description The address it was last seen from, when Memax knows it. */
            address?: string;
            /** @description The city that address is in, when the edge says. */
            city?: string;
            signed_in_at: components["schemas"]["Timestamp"];
            /** @description When it was last used, within a few minutes. */
            last_used_at: components["schemas"]["Timestamp"];
            /** @description When it ends unless it is signed out first; refreshing doesn't extend it. */
            expires_at: components["schemas"]["Timestamp"];
            /** @description When it was signed out (only in the answer to signing it out). */
            revoked_at?: components["schemas"]["Timestamp"];
            /** @description Whether this request comes from this session. */
            current: boolean;
        };
        SessionList: {
            items: components["schemas"]["Session"][];
        };
        SessionListEnvelope: {
            data: components["schemas"]["SessionList"];
        };
        SessionEnvelope: {
            data: components["schemas"]["Session"];
        };
        SessionsRevoked: {
            /** @description How many sessions were signed out. */
            revoked: number;
        };
        SessionsRevokedEnvelope: {
            data: components["schemas"]["SessionsRevoked"];
        };
    };
    responses: {
        /** @description The command was applied, or sent to Review. */
        CommandResult: {
            headers: {
                ETag: components["headers"]["ETag"];
                "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["CommandResultEnvelope"];
            };
        };
        /** @description The command was applied; `memories` are every memory it changed. */
        MemoriesCommandResult: {
            headers: {
                ETag: components["headers"]["ETag"];
                "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["MemoriesCommandResultEnvelope"];
            };
        };
        /** @description `undo_refused`: see `details.reason`, and `details.ref` for what is in the way. */
        UndoRefused: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorEnvelope"];
            };
        };
        /** @description The change to the agent was applied (or there was nothing to change). */
        AgentCommandResult: {
            headers: {
                "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["AgentCommandResultEnvelope"];
            };
        };
        /**
         * @description `invalid_request`, `idempotency_key_required`, `space_required` or
         *     `ambiguous_ref`.
         */
        BadRequest: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorEnvelope"];
            };
        };
        /** @description `unauthorized`: sign in again or check the API key. */
        Unauthorized: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorEnvelope"];
            };
        };
        /**
         * @description `refused` (policy; see `details.policy`), `permission_denied`,
         *     `impersonation_read_only` or `surface_unverified`.
         */
        Forbidden: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorEnvelope"];
            };
        };
        /** @description `not_found`: no such space or memory in your spaces. */
        NotFound: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorEnvelope"];
            };
        };
        /** @description `invalid_transition`: the memory's, agent's or gate's state doesn't allow this command (keeping a kept memory, pausing a paused agent, anything on a disconnected one, answering a gate that was answered, withdrawn or expired). `in_conflict` (Keep, and edit then keep): the judge flagged the proposal as contradicting a decision in force; `details.ref` is that decision, and the conflict is settled with `:resolve-conflict`. On `:resolve-conflict`, `in_conflict` names a decision in force that is in the way of the answer: another conflict of the side it would keep, or one the judge found narrower words contradict. */
        InvalidTransition: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorEnvelope"];
            };
        };
        /** @description `forget_carries`: other memories carry its words and go with it; `details.carries` lists them, and the Forget goes through once `carries` names exactly those. `invalid_transition`: it is forgotten already. */
        ForgetConflict: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorEnvelope"];
            };
        };
        /** @description `edit_clash`: the memory changed since you read it. Reload and try again. */
        EditClash: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorEnvelope"];
            };
        };
        /** @description `idempotency_key_reused`: send a new key for a new command. */
        IdempotencyKeyReused: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorEnvelope"];
            };
        };
        /** @description `precondition_required`: send `If-Match` with the version you started from. */
        PreconditionRequired: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorEnvelope"];
            };
        };
        /** @description `rate_limited`: wait for `Retry-After` seconds. */
        RateLimited: {
            headers: {
                "Retry-After": components["headers"]["RetryAfter"];
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorEnvelope"];
            };
        };
        /** @description `internal_error`. */
        InternalError: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorEnvelope"];
            };
        };
        /**
         * @description `busy` (another change holds the memory; `Retry-After` is set),
         *     `judge_pending` (Keep: the judge hasn't looked at these words yet,
         *     on a proposal that touches a decision in force; `Retry-After` is set
         *     and `details.ref` names the memory; the same Keep goes through once
         *     it has, or after at most 30 s) or `unavailable` (the record is not
         *     configured on this server).
         */
        Unavailable: {
            headers: {
                /** @description Seconds to wait before retrying; set for `busy` and `judge_pending`. */
                "Retry-After"?: string;
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorEnvelope"];
            };
        };
        /** @description `slug_taken` (the slug belongs to another space) or `space_kind` (the space can't switch as that kind: only a V1 team hub with no V2 record yet may become a project space). */
        SpaceConflict: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ErrorEnvelope"];
            };
        };
        /** @description The target after the command. */
        TargetResult: {
            headers: {
                ETag: components["headers"]["VersionETag"];
                "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["TargetResultEnvelope"];
            };
        };
        /** @description The load, recorded as a read (or already recorded under this key). */
        CompileLoadRecorded: {
            headers: {
                "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["CompileLoadResultEnvelope"];
            };
        };
        /** @description The gate after the command (and, for an answer, the decision it became). */
        GateResult: {
            headers: {
                ETag: components["headers"]["VersionETag"];
                "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["GateResultEnvelope"];
            };
        };
        /** @description The hand edits, resolved. */
        DriftResolution: {
            headers: {
                "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["DriftResolutionEnvelope"];
            };
        };
    };
    parameters: {
        /** @description The space's id or slug. */
        SpacePath: components["schemas"]["SpaceKey"];
        /**
         * @description The space a display ID belongs to, by id or slug. Required with a
         *     display ID; optional with a memory id, where it must match.
         */
        SpaceContext: components["schemas"]["SpaceKey"];
        /** @description A note's display ID (N-0042) or id. */
        NotePath: components["schemas"]["NoteKey"];
        /** @description A display ID (M-0219, with `?space=`) or a memory id. */
        RefPath: components["schemas"]["MemoryRef"];
        /** @description The agent connection's id. */
        AgentPath: components["schemas"]["Id"];
        /** @description The session's id. */
        SessionPath: components["schemas"]["Id"];
        /** @description The `next_cursor` of the previous page. */
        Cursor: string;
        /** @description Page size. Larger values are capped at 200. */
        Limit: number;
        /**
         * @description A key you choose for this command, such as a uuid. Send the same key
         *     when you retry; send a new key for a new command.
         */
        IdempotencyKey: string;
        /** @description The `ETag` (memory version) you started from, e.g. `"3"`. */
        IfMatch: components["schemas"]["VersionTag"];
        /** @description The `ETag` (memory version) you reviewed, e.g. `"3"`. */
        IfMatchOptional: components["schemas"]["VersionTag"];
        /**
         * @description The surface the command came through, for its receipt. Defaults to
         *     `api`. Every value here records a client-attested change; Memax
         *     records a change as made by a person on the web (`via: web`) only
         *     when the web app's proxy signed the request for a session issued to
         *     the web app.
         */
        Via: "api" | "cli" | "mcp";
        /** @description A receipt's id. */
        ReceiptPath: components["schemas"]["Id"];
        /** @description An edition's display ID (D-0214), its number (214), its id, or `latest`. */
        EditionPath: string;
        /** @description One of Dream's actions, by id. */
        DreamActionPath: components["schemas"]["Id"];
        /**
         * @description Your app's IANA time zone (the SDK sends it). Dream records it as
         *     your zone, so it runs in your local night, unless you set one.
         */
        TimeZoneHeader: string;
        /** @description The import's id. */
        ImportPath: components["schemas"]["Id"];
        /** @description The disagreement's number within the import (its `n`, from 1). */
        ConflictNumber: string;
        /** @description The Brief version's number (its `version`, from 1). */
        BriefVersionNumber: string;
        /** @description A gate's display ID (G-0012, with `?space=`) or its id. */
        GateRefPath: components["schemas"]["GateRef"];
        /**
         * @description The gate's `ETag` (its version) you read, e.g. `"1"`. A gate's
         *     version changes only when it ends: one that ended since is 409
         *     `invalid_transition` (with `details.status`), and a version that
         *     isn't the waiting gate's is 412 `edit_clash`.
         */
        IfMatchGate: components["schemas"]["VersionTag"];
        /** @description The target's id. */
        TargetPath: components["schemas"]["Id"];
        /**
         * @description The `ETag` (the Brief's or the target's version) you started from,
         *     e.g. `"3"`. Revising a Brief requires it once the space has one.
         */
        IfMatchVersion: components["schemas"]["VersionTag"];
    };
    requestBodies: never;
    headers: {
        /** @description The memory's version, quoted. Send it back as `If-Match`. */
        ETag: components["schemas"]["VersionTag"];
        /** @description The memory's URL, by id. */
        Location: string;
        /** @description Set to `true` when the Idempotency-Key was already applied and nothing new was written. */
        IdempotentReplayed: "true";
        /** @description Seconds to wait before retrying. */
        RetryAfter: string;
        /** @description The object's version, quoted. Send it back as `If-Match`. */
        VersionETag: components["schemas"]["VersionTag"];
        /** @description The target's URL, by id. */
        TargetLocation: string;
        /** @description The gate's URL, by id. */
        GateLocation: string;
        /** @description The import's URL. */
        ImportURL: string;
    };
    pathItems: never;
}
export type $defs = Record<string, never>;
export interface operations {
    listSpaces: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Your spaces. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SpaceListEnvelope"];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    createSpace: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["CreateSpaceRequest"];
            };
        };
        responses: {
            /** @description The space, with you as its owner. */
            201: {
                headers: {
                    "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SpaceEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            409: components["responses"]["SpaceConflict"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    getSpaceSwitch: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The switch, with its preview. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SpaceSwitchEnvelope"];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    switchSpace: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["SwitchSpaceRequest"];
            };
        };
        responses: {
            /** @description Switched (or switched back) within the request, or it already had. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SpaceSwitchEnvelope"];
                };
            };
            /** @description The switch continues in the background; read it with `GET /v2/spaces/{space}/switch`. */
            202: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SpaceSwitchEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["SpaceConflict"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    listV1DreamRuns: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The runs. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["V1DreamRunListEnvelope"];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    searchNotes: {
        parameters: {
            query?: {
                /** @description Words to search for. */
                q?: string;
                /** @description Page size. Larger values are capped at 200. */
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The notes. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["NotePageEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    getNote: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
                /** @description A note's display ID (N-0042) or id. */
                note: components["parameters"]["NotePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The note. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["NoteEnvelope"];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    previewForgetNote: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
                /** @description A note's display ID (N-0042) or id. */
                note: components["parameters"]["NotePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The preview. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["NoteForgetPreviewEnvelope"];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    forgetNote: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
                /** @description A note's display ID (N-0042) or id. */
                note: components["parameters"]["NotePath"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["ForgetNoteRequest"];
            };
        };
        responses: {
            /** @description Forgotten; the result has the tombstone. */
            200: {
                headers: {
                    "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["NoteForgetResultEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["ForgetConflict"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    exportSpace: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The export, as a zip archive. */
            200: {
                headers: {
                    /** @description `attachment; filename="memax-<slug>-<yyyy-mm-dd>.zip"`, dated as of the newest receipt. */
                    "Content-Disposition": string;
                    /** @description The id of the export's own `exported` receipt (also in `export.json`). */
                    "X-Memax-Export-Receipt": string;
                    "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                    [name: string]: unknown;
                };
                content: {
                    "application/zip": components["schemas"]["ExportArchive"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    listMemories: {
        parameters: {
            query?: {
                /** @description Only these displayed states. Repeat for more than one. */
                state?: components["schemas"]["State"][];
                /** @description Only these sections. Repeat for more than one. */
                section?: components["schemas"]["Section"][];
                /** @description The `next_cursor` of the previous page. */
                cursor?: components["parameters"]["Cursor"];
                /** @description Page size. Larger values are capped at 200. */
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description One page of memories. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["MemoryPageEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    rememberMemory: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["RememberRequest"];
            };
        };
        responses: {
            /** @description The memory was written, kept or proposed. */
            201: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    Location: components["headers"]["Location"];
                    "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CommandResultEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    findNearDuplicates: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["NearDuplicatesRequest"];
            };
        };
        responses: {
            /** @description What the draft repeats, best first; none is an empty list. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["NearDuplicatesEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    keepMemories: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["BulkReviewRequest"];
            };
        };
        responses: {
            /** @description What happened to each proposal, in request order. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BulkReviewResultEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    rejectMemories: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["BulkReviewRequest"];
            };
        };
        responses: {
            /** @description What happened to each proposal, in request order. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BulkReviewResultEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    askSpace: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["AskRequest"];
            };
        };
        responses: {
            /** @description The answer, as a stream of AskEvent events, sent with `Cache-Control: no-cache`. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "text/event-stream": components["schemas"]["AskEvent"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    listReview: {
        parameters: {
            query?: {
                /** @description The `next_cursor` of the previous page. */
                cursor?: components["parameters"]["Cursor"];
                /** @description Page size. Larger values are capped at 200. */
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description One page of the queue. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ReviewPageEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    listReceipts: {
        parameters: {
            query?: {
                /** @description Only this memory's receipts, by display ID (M-0219) or id. */
                memory?: components["schemas"]["MemoryRef"];
                /** @description The `next_cursor` of the previous page. */
                cursor?: components["parameters"]["Cursor"];
                /** @description Page size. Larger values are capped at 200. */
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description One page of receipts. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ReceiptPageEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    listCheckpoints: {
        parameters: {
            query?: {
                /** @description The `next_cursor` of the previous page. */
                cursor?: components["parameters"]["Cursor"];
                /** @description Page size. Larger values are capped at 200. */
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description One page of checkpoints. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CheckpointPageEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    listReads: {
        parameters: {
            query?: {
                /** @description The `next_cursor` of the previous page. */
                cursor?: components["parameters"]["Cursor"];
                /** @description Page size. Larger values are capped at 200. */
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description One page of reads. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ReadPageEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    listTombstones: {
        parameters: {
            query?: {
                /** @description The `next_cursor` of the previous page. */
                cursor?: components["parameters"]["Cursor"];
                /** @description Page size. Larger values are capped at 200. */
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description One page of tombstones. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TombstonePageEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    recordCompileLoad: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["CompileLoadRequest"];
            };
        };
        responses: {
            201: components["responses"]["CompileLoadRecorded"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    listImports: {
        parameters: {
            query?: {
                /** @description The `next_cursor` of the previous page. */
                cursor?: components["parameters"]["Cursor"];
                /** @description Page size. Larger values are capped at 200. */
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description One page of imports. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ImportPageEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    createImport: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ImportRequest"];
            };
        };
        responses: {
            /** @description The import, and what became of each statement. */
            201: {
                headers: {
                    Location: components["headers"]["ImportURL"];
                    "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ImportResultEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    getImport: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
                /** @description The import's id. */
                import: components["parameters"]["ImportPath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The import. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ImportViewEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    settleImportConflict: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
                /** @description The import's id. */
                import: components["parameters"]["ImportPath"];
                /** @description The disagreement's number within the import (its `n`, from 1). */
                n: components["parameters"]["ConflictNumber"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["SettleImportConflictRequest"];
            };
        };
        responses: {
            /** @description The disagreement, settled, and every memory it changed. */
            200: {
                headers: {
                    "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ImportConflictResultEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    getMemory: {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The memory and its history. */
            200: {
                headers: {
                    ETag: components["headers"]["ETag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["MemoryDetailEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    keepMemory: {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /** @description The `ETag` (memory version) you reviewed, e.g. `"3"`. */
                "If-Match"?: components["parameters"]["IfMatchOptional"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["ReviewRequest"];
            };
        };
        responses: {
            200: components["responses"]["CommandResult"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            412: components["responses"]["EditClash"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    editMemory: {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /** @description The `ETag` (memory version) you started from, e.g. `"3"`. */
                "If-Match": components["parameters"]["IfMatch"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["EditRequest"];
            };
        };
        responses: {
            200: components["responses"]["CommandResult"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            412: components["responses"]["EditClash"];
            422: components["responses"]["IdempotencyKeyReused"];
            428: components["responses"]["PreconditionRequired"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    rejectMemory: {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /** @description The `ETag` (memory version) you reviewed, e.g. `"3"`. */
                "If-Match"?: components["parameters"]["IfMatchOptional"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["ReviewRequest"];
            };
        };
        responses: {
            200: components["responses"]["CommandResult"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            412: components["responses"]["EditClash"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    getConflict: {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
                /** @description The other side, by display ID or id, when there is more than one. */
                with?: components["schemas"]["MemoryRef"];
            };
            header?: never;
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The conflict. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ConflictEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    resolveConflict: {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /** @description The `ETag` (memory version) you reviewed, e.g. `"3"`. */
                "If-Match"?: components["parameters"]["IfMatchOptional"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ResolveConflictRequest"];
            };
        };
        responses: {
            200: components["responses"]["MemoriesCommandResult"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            412: components["responses"]["EditClash"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    forgetMemory: {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /** @description The `ETag` (memory version) you started from, e.g. `"3"`. */
                "If-Match": components["parameters"]["IfMatch"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["ForgetRequest"];
            };
        };
        responses: {
            /** @description Forgotten; the result has the tombstone. */
            200: {
                headers: {
                    "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ForgetResultEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["ForgetConflict"];
            412: components["responses"]["EditClash"];
            422: components["responses"]["IdempotencyKeyReused"];
            428: components["responses"]["PreconditionRequired"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    requestForget: {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["ReviewRequest"];
            };
        };
        responses: {
            /** @description The request, waiting for a person. */
            200: {
                headers: {
                    "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ForgetRequestResultEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    declineForget: {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["ReviewRequest"];
            };
        };
        responses: {
            200: components["responses"]["CommandResult"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    previewForget: {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description What it would do. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ForgetPreviewEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    getTombstone: {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The tombstone. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TombstoneEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    undoReceipt: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description A receipt's id. */
                receipt: components["parameters"]["ReceiptPath"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["ReviewRequest"];
            };
        };
        responses: {
            200: components["responses"]["MemoriesCommandResult"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["UndoRefused"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    listAgents: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Your agents. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AgentListEnvelope"];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    listSpaceAgents: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The space's agents. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AgentListEnvelope"];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    getAgent: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The agent connection's id. */
                agent: components["parameters"]["AgentPath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The agent. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AgentDetailEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    setAgentAutonomy: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The agent connection's id. */
                agent: components["parameters"]["AgentPath"];
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["AutonomyRequest"];
            };
        };
        responses: {
            200: components["responses"]["AgentCommandResult"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    pauseAgent: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The agent connection's id. */
                agent: components["parameters"]["AgentPath"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["AgentCommandRequest"];
            };
        };
        responses: {
            200: components["responses"]["AgentCommandResult"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    resumeAgent: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The agent connection's id. */
                agent: components["parameters"]["AgentPath"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["AgentCommandRequest"];
            };
        };
        responses: {
            200: components["responses"]["AgentCommandResult"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    disconnectAgent: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The agent connection's id. */
                agent: components["parameters"]["AgentPath"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["AgentCommandRequest"];
            };
        };
        responses: {
            200: components["responses"]["AgentCommandResult"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    getBrief: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The current version. */
            200: {
                headers: {
                    ETag: components["headers"]["VersionETag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BriefEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    reviseBrief: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The `ETag` (the Brief's or the target's version) you started from,
                 *     e.g. `"3"`. Revising a Brief requires it once the space has one.
                 */
                "If-Match"?: components["parameters"]["IfMatchVersion"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ReviseBriefRequest"];
            };
        };
        responses: {
            /** @description The new version. */
            201: {
                headers: {
                    ETag: components["headers"]["VersionETag"];
                    "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BriefResultEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            412: components["responses"]["EditClash"];
            422: components["responses"]["IdempotencyKeyReused"];
            428: components["responses"]["PreconditionRequired"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    listBriefVersions: {
        parameters: {
            query?: {
                /** @description The `next_cursor` of the previous page. */
                cursor?: components["parameters"]["Cursor"];
                /** @description Page size. Larger values are capped at 200. */
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description One page of versions. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["BriefVersionPageEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    restoreBriefVersion: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The `ETag` (the Brief's or the target's version) you started from,
                 *     e.g. `"3"`. Revising a Brief requires it once the space has one.
                 */
                "If-Match"?: components["parameters"]["IfMatchVersion"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
                /** @description The Brief version's number (its `version`, from 1). */
                n: components["parameters"]["BriefVersionNumber"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["ReviewRequest"];
            };
        };
        responses: {
            /** @description The new version, and what the restore left out. */
            201: {
                headers: {
                    ETag: components["headers"]["VersionETag"];
                    "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["RestoreBriefResultEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            412: components["responses"]["EditClash"];
            422: components["responses"]["IdempotencyKeyReused"];
            428: components["responses"]["PreconditionRequired"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    listTargets: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The targets. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TargetListEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    createTarget: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["CreateTargetRequest"];
            };
        };
        responses: {
            /** @description The target, compiling. */
            201: {
                headers: {
                    ETag: components["headers"]["VersionETag"];
                    Location: components["headers"]["TargetLocation"];
                    "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TargetResultEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    configureTarget: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The `ETag` (the Brief's or the target's version) you started from,
                 *     e.g. `"3"`. Revising a Brief requires it once the space has one.
                 */
                "If-Match"?: components["parameters"]["IfMatchVersion"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ConfigureTargetRequest"];
            };
        };
        responses: {
            200: components["responses"]["TargetResult"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            412: components["responses"]["EditClash"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    compileTarget: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["ReviewRequest"];
            };
        };
        responses: {
            202: components["responses"]["TargetResult"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    getTargetPreview: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The compiled content. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["TargetPreviewEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    listCompileRuns: {
        parameters: {
            query?: {
                /** @description The `next_cursor` of the previous page. */
                cursor?: components["parameters"]["Cursor"];
                /** @description Page size. Larger values are capped at 200. */
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description One page of runs. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["CompileRunPageEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    recordObservation: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["ObservationRequest"];
            };
        };
        responses: {
            /** @description The file matches what Memax delivered. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ObservationResultEnvelope"];
                };
            };
            /** @description A hand edit, recorded. */
            201: {
                headers: {
                    "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["ObservationResultEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    recordDelivery: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["DeliveryRequest"];
            };
        };
        responses: {
            /** @description The delivery, recorded (or already known). */
            200: {
                headers: {
                    "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DeliveryResultEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    getDrift: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The open hand edits. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DriftEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    pullDrift: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["ResolveDriftRequest"];
            };
        };
        responses: {
            200: components["responses"]["DriftResolution"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    overwriteDrift: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["ResolveDriftRequest"];
            };
        };
        responses: {
            200: components["responses"]["DriftResolution"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    stopDrift: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The target's id. */
                target: components["parameters"]["TargetPath"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["ResolveDriftRequest"];
            };
        };
        responses: {
            200: components["responses"]["DriftResolution"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    listGates: {
        parameters: {
            query?: {
                /** @description Only these statuses. Repeat for more than one. */
                status?: components["schemas"]["GateStatus"][];
                /** @description The `next_cursor` of the previous page. */
                cursor?: components["parameters"]["Cursor"];
                /** @description Page size. Larger values are capped at 200. */
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description One page of gates. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["GatePageEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    requestDecision: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["RequestDecisionRequest"];
            };
        };
        responses: {
            /** @description The gate is waiting. */
            201: {
                headers: {
                    ETag: components["headers"]["VersionETag"];
                    Location: components["headers"]["GateLocation"];
                    "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["GateResultEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    getGate: {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header?: never;
            path: {
                /** @description A gate's display ID (G-0012, with `?space=`) or its id. */
                ref: components["parameters"]["GateRefPath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The gate. */
            200: {
                headers: {
                    ETag: components["headers"]["VersionETag"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["GateEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    answerGate: {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The gate's `ETag` (its version) you read, e.g. `"1"`. A gate's
                 *     version changes only when it ends: one that ended since is 409
                 *     `invalid_transition` (with `details.status`), and a version that
                 *     isn't the waiting gate's is 412 `edit_clash`.
                 */
                "If-Match"?: components["parameters"]["IfMatchGate"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description A gate's display ID (G-0012, with `?space=`) or its id. */
                ref: components["parameters"]["GateRefPath"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["AnswerGateRequest"];
            };
        };
        responses: {
            200: components["responses"]["GateResult"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            412: components["responses"]["EditClash"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    withdrawGate: {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The gate's `ETag` (its version) you read, e.g. `"1"`. A gate's
                 *     version changes only when it ends: one that ended since is 409
                 *     `invalid_transition` (with `details.status`), and a version that
                 *     isn't the waiting gate's is 412 `edit_clash`.
                 */
                "If-Match"?: components["parameters"]["IfMatchGate"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description A gate's display ID (G-0012, with `?space=`) or its id. */
                ref: components["parameters"]["GateRefPath"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["ReviewRequest"];
            };
        };
        responses: {
            200: components["responses"]["GateResult"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            412: components["responses"]["EditClash"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    listEditions: {
        parameters: {
            query?: {
                /** @description The `next_cursor` of the previous page. */
                cursor?: components["parameters"]["Cursor"];
                /** @description Page size. Larger values are capped at 200. */
                limit?: components["parameters"]["Limit"];
            };
            header?: {
                /**
                 * @description Your app's IANA time zone (the SDK sends it). Dream records it as
                 *     your zone, so it runs in your local night, unless you set one.
                 */
                "X-Timezone"?: components["parameters"]["TimeZoneHeader"];
            };
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description One page of editions. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DreamEditionPageEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    getEdition: {
        parameters: {
            query?: never;
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
                /** @description An edition's display ID (D-0214), its number (214), its id, or `latest`. */
                edition: components["parameters"]["EditionPath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The edition. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DreamEditionEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    listDreamActions: {
        parameters: {
            query?: {
                kind?: components["schemas"]["DreamActionKind"];
                /** @description The `next_cursor` of the previous page. */
                cursor?: components["parameters"]["Cursor"];
                /** @description Page size. Larger values are capped at 200. */
                limit?: components["parameters"]["Limit"];
            };
            header?: never;
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
                /** @description An edition's display ID (D-0214), its number (214), its id, or `latest`. */
                edition: components["parameters"]["EditionPath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description One page of actions. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DreamActionPageEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    undoEdition: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
                /** @description An edition's display ID (D-0214), its number (214), its id, or `latest`. */
                edition: components["parameters"]["EditionPath"];
            };
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["UndoEditionRequest"];
            };
        };
        responses: {
            /** @description What was undone, and what wasn't. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["UndoEditionResultEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    runDream: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description The space's id or slug. */
                space: components["parameters"]["SpacePath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The run is queued. */
            202: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DreamRunEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    undoDreamAction: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description One of Dream's actions, by id. */
                action: components["parameters"]["DreamActionPath"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["ReviewRequest"];
            };
        };
        responses: {
            /** @description The action is undone. */
            200: {
                headers: {
                    "Idempotent-Replayed": components["headers"]["IdempotentReplayed"];
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DreamUndoResultEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["UndoRefused"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    restoreMemory: {
        parameters: {
            query?: {
                /**
                 * @description The space a display ID belongs to, by id or slug. Required with a
                 *     display ID; optional with a memory id, where it must match.
                 */
                space?: components["parameters"]["SpaceContext"];
            };
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
                /** @description The `ETag` (memory version) you reviewed, e.g. `"3"`. */
                "If-Match"?: components["parameters"]["IfMatchOptional"];
                /**
                 * @description The surface the command came through, for its receipt. Defaults to
                 *     `api`. Every value here records a client-attested change; Memax
                 *     records a change as made by a person on the web (`via: web`) only
                 *     when the web app's proxy signed the request for a session issued to
                 *     the web app.
                 */
                "X-Memax-Via"?: components["parameters"]["Via"];
            };
            path: {
                /** @description A display ID (M-0219, with `?space=`) or a memory id. */
                ref: components["parameters"]["RefPath"];
            };
            cookie?: never;
        };
        requestBody?: {
            content: {
                "application/json": components["schemas"]["ReviewRequest"];
            };
        };
        responses: {
            200: components["responses"]["CommandResult"];
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            412: components["responses"]["EditClash"];
            422: components["responses"]["IdempotencyKeyReused"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    getDreamSettings: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Your settings. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DreamSettingsEnvelope"];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    updateDreamSettings: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["DreamSettingsRequest"];
            };
        };
        responses: {
            /** @description Your settings. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DreamSettingsEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    unsubscribeDreamEmail: {
        parameters: {
            query: {
                token: string;
            };
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The morning email is off, if the token was one. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["UnsubscribeResultEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    listNotices: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The notices waiting. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["NoticeListEnvelope"];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    ackNotices: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["AckNoticesRequest"];
            };
        };
        responses: {
            /** @description How many were marked. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["AckNoticesResultEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    lookupDeviceAuthorization: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["DeviceCodeRequest"];
            };
        };
        responses: {
            /** @description The code and the device asking. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DeviceAuthorizationEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    approveDeviceAuthorization: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["DeviceCodeRequest"];
            };
        };
        responses: {
            /** @description The code, confirmed. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DeviceAuthorizationEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    denyDeviceAuthorization: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
            };
            path?: never;
            cookie?: never;
        };
        requestBody: {
            content: {
                "application/json": components["schemas"]["DeviceCodeRequest"];
            };
        };
        responses: {
            /** @description The code, declined. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["DeviceAuthorizationEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            409: components["responses"]["InvalidTransition"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    listSessions: {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description Your live sessions. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SessionListEnvelope"];
                };
            };
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    revokeSession: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
            };
            path: {
                /** @description The session's id. */
                session: components["parameters"]["SessionPath"];
            };
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description The session, ended. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SessionEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            404: components["responses"]["NotFound"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
    revokeOtherSessions: {
        parameters: {
            query?: never;
            header: {
                /**
                 * @description A key you choose for this command, such as a uuid. Send the same key
                 *     when you retry; send a new key for a new command.
                 */
                "Idempotency-Key": components["parameters"]["IdempotencyKey"];
            };
            path?: never;
            cookie?: never;
        };
        requestBody?: never;
        responses: {
            /** @description How many sessions ended. */
            200: {
                headers: {
                    [name: string]: unknown;
                };
                content: {
                    "application/json": components["schemas"]["SessionsRevokedEnvelope"];
                };
            };
            400: components["responses"]["BadRequest"];
            401: components["responses"]["Unauthorized"];
            403: components["responses"]["Forbidden"];
            409: components["responses"]["InvalidTransition"];
            429: components["responses"]["RateLimited"];
            500: components["responses"]["InternalError"];
            503: components["responses"]["Unavailable"];
        };
    };
}
