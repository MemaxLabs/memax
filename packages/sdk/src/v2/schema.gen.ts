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
        post?: never;
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
         *     `If-Match` with the version you reviewed.
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
         *     itself is unchanged. `keep: true` keeps a proposal after editing it.
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
         *     receipt of whoever wrote it and why (BriefHistory). Restore one by
         *     revising the Brief with its sections.
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
         *     again, or an older one, changes nothing.
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
         *     or exclude the memory; it is never forgotten automatically. The file
         *     stays as it is until the next compile delivers over it. Any person
         *     in the space may pull.
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
        /** @enum {string} */
        SourceKind: "session" | "pr" | "file" | "url" | "issue" | "email" | "note" | "import";
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
         *     delivered.
         * @enum {string}
         */
        ReceiptAction: "proposed" | "kept" | "edited" | "rejected" | "merged" | "flagged" | "resolved" | "verified" | "faded" | "restored" | "forgot" | "moved" | "compiled" | "handed_off" | "answered" | "undid" | "connected" | "autonomy_changed" | "paused" | "resumed" | "disconnected" | "revised" | "configured" | "requested" | "delivered" | "observed" | "pulled" | "overwritten" | "stopped";
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
         *     Brief), targets_by_person, compile_by_memax. Refused changes to
         *     agents: person_must_manage (only a person changes what an agent may
         *     do), not_your_agent, autonomy_not_allowed, key_max_propose,
         *     autonomy_needs_web (raising an agent needs a person on the web).
         *     Refused or sent to Review:
         *     viewer, owners_keep, decision_needs_web. Sent to Review: api_key,
         *     external_source, contradicts_decision, edits_person_kept,
         *     autonomy_propose, integration, import, system_proposes, repository,
         *     person_proposed. Confirmation: confirm_in_agent.
         * @enum {string}
         */
        PolicyCode: "unknown_actor" | "unknown_action" | "secret_detected" | "not_member" | "read_only" | "key_read_only" | "key_cannot_review" | "key_cannot_forget" | "person_must_review" | "person_must_forget" | "forget_not_allowed" | "external_needs_review" | "proposal_in_review" | "agent_not_connected" | "agent_paused" | "person_must_manage" | "not_your_agent" | "autonomy_not_allowed" | "key_max_propose" | "autonomy_needs_web" | "brief_by_person" | "targets_by_person" | "compile_by_memax" | "viewer" | "owners_keep" | "decision_needs_web" | "api_key" | "external_source" | "contradicts_decision" | "edits_person_kept" | "autonomy_propose" | "integration" | "import" | "system_proposes" | "repository" | "person_proposed" | "confirm_in_agent";
        /** @enum {string} */
        ErrorCode: "invalid_request" | "idempotency_key_required" | "space_required" | "ambiguous_ref" | "unauthorized" | "refused" | "permission_denied" | "impersonation_read_only" | "surface_unverified" | "not_found" | "method_not_allowed" | "invalid_transition" | "edit_clash" | "idempotency_key_reused" | "precondition_required" | "rate_limited" | "internal_error" | "busy" | "unavailable";
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
         *     `off`: compiling is stopped.
         * @enum {string}
         */
        SyncState: "in_sync" | "compiling" | "pending_delivery" | "drifted" | "off";
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
        MemoryDetail: {
            memory: components["schemas"]["Memory"];
            /** @description Every version of the statement, newest first. */
            versions: components["schemas"]["MemoryVersion"][];
            receipts: components["schemas"]["ReceiptPage"];
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
            /** @description Reads in the last 7 days. 0 until reads are recorded. */
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
            /** @description Reads in the last 7 days, in those spaces. 0 until reads are recorded. */
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
         * @description What the agent did in the last 7 days, in your spaces: its writes,
         *     and what became of the memories it wrote.
         */
        AgentWeek: {
            /** @description 0 until reads are recorded. */
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
        /** @description One of the agent's sessions, from the session on its receipts. */
        AgentSession: {
            session_ref: components["schemas"]["SessionRef"];
            /** @description 0 until reads are recorded. */
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
            /** @description The supporting excerpt. */
            quote?: string;
            content_hash?: string;
        };
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
        /** @description The body of keep, reject and compile. Every field is optional. */
        ReviewRequest: {
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
            /** @description The memory's display ID (`edit_clash`, `invalid_transition`). */
            ref?: string;
            /** @description The version you sent (`edit_clash`). */
            expected_version?: number;
            /** @description The memory's version now (`edit_clash`). */
            current_version?: number;
            /** @description Seconds to wait (`rate_limited`, `busy`). */
            retry_after?: number;
            /** @description The rate limit (`rate_limited`). */
            limit?: number;
            /** @description Requests counted so far (`rate_limited`). */
            current?: number;
            /** @description When the window resets (`rate_limited`). */
            reset_at?: components["schemas"]["Timestamp"];
        };
        ErrorEnvelope: {
            error: components["schemas"]["Error"];
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
        /** @description `invalid_transition`: the memory's or agent's state doesn't allow this command (keeping a kept memory, pausing a paused agent, anything on a disconnected one). */
        InvalidTransition: {
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
         * @description `busy` (another change holds the memory; `Retry-After` is set) or
         *     `unavailable` (the record is not configured on this server).
         */
        Unavailable: {
            headers: {
                /** @description Seconds to wait before retrying; set for `busy`. */
                "Retry-After"?: string;
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
        /** @description A display ID (M-0219, with `?space=`) or a memory id. */
        RefPath: components["schemas"]["MemoryRef"];
        /** @description The agent connection's id. */
        AgentPath: components["schemas"]["Id"];
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
}
