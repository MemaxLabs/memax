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
         *     paused, resumed and disconnected.
         * @enum {string}
         */
        ReceiptAction: "proposed" | "kept" | "edited" | "rejected" | "merged" | "flagged" | "resolved" | "verified" | "faded" | "restored" | "forgot" | "moved" | "compiled" | "handed_off" | "answered" | "undid" | "connected" | "autonomy_changed" | "paused" | "resumed" | "disconnected";
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
         *     agent_paused. Refused changes to agents: person_must_manage (only a
         *     person changes what an agent may do), not_your_agent,
         *     autonomy_not_allowed, key_max_propose, autonomy_needs_web (raising
         *     an agent needs a person on the web). Refused or sent to Review:
         *     viewer, owners_keep, decision_needs_web. Sent to Review: api_key,
         *     external_source, contradicts_decision, edits_person_kept,
         *     autonomy_propose, integration, import, system_proposes, repository,
         *     person_proposed. Confirmation: confirm_in_agent.
         * @enum {string}
         */
        PolicyCode: "unknown_actor" | "unknown_action" | "secret_detected" | "not_member" | "read_only" | "key_read_only" | "key_cannot_review" | "key_cannot_forget" | "person_must_review" | "person_must_forget" | "forget_not_allowed" | "external_needs_review" | "proposal_in_review" | "agent_not_connected" | "agent_paused" | "person_must_manage" | "not_your_agent" | "autonomy_not_allowed" | "key_max_propose" | "autonomy_needs_web" | "viewer" | "owners_keep" | "decision_needs_web" | "api_key" | "external_source" | "contradicts_decision" | "edits_person_kept" | "autonomy_propose" | "integration" | "import" | "system_proposes" | "repository" | "person_proposed" | "confirm_in_agent";
        /** @enum {string} */
        ErrorCode: "invalid_request" | "idempotency_key_required" | "space_required" | "ambiguous_ref" | "unauthorized" | "refused" | "permission_denied" | "impersonation_read_only" | "surface_unverified" | "not_found" | "method_not_allowed" | "invalid_transition" | "edit_clash" | "idempotency_key_reused" | "precondition_required" | "rate_limited" | "internal_error" | "busy" | "unavailable";
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
        /** @description The body of keep and reject. Every field is optional. */
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
}
