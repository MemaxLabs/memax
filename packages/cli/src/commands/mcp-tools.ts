// The local stdio MCP server's tool catalogue: the agent profile of the
// remote server (packages/server/internal/handler/mcp_tools.json), tool for
// tool. scripts/check-mcp-parity.mjs fails lint when names, titles,
// descriptions, input or output schemas, annotations or the instructions
// differ. Descriptions say what a tool does, not how an agent should behave.
//
// Erasable TypeScript only (no enums, no imports), so the parity script can
// import this file with Node's type stripping.

/** The memory item every read tool's output schema uses ("#item"). */
export const MCP_ITEM_SCHEMA: Record<string, unknown> = {
  type: "object",
  properties: {
    id: {
      type: "string",
      description: "The memory's UUID.",
    },
    ref: {
      type: "string",
      description:
        "The display ID of a memory on the V2 record, such as M-0219.",
    },
    record: {
      type: "string",
      enum: ["v1", "v2"],
      description:
        "v2 for a statement on the V2 record; v1 for a saved memory in a space that hasn't switched.",
    },
    space_id: {
      type: "string",
    },
    space: {
      type: "string",
      description: "The space's name.",
    },
    title: {
      type: "string",
    },
    text: {
      type: "string",
      description: "The statement (v2) or the most relevant excerpt (v1).",
    },
    summary: {
      type: "string",
    },
    section: {
      type: "string",
      description: "decisions, conventions, preferences or open_question.",
    },
    kind: {
      type: "string",
    },
    state: {
      type: "string",
      description: "kept, proposed, stale or conflict (v2).",
    },
    stability: {
      type: "string",
    },
    score: {
      type: "number",
    },
    age: {
      type: "string",
    },
    source: {
      type: "string",
    },
    url: {
      type: "string",
      description: "Where a person can open it in Memax.",
    },
  },
  required: ["id", "record", "text"],
};

export const MCP_SERVER_NAME = "memax";

export const MCP_INSTRUCTIONS =
  "Memax holds the memories a person and their team keep across agents. memax_recall and memax_search read them; memax_get opens one memory with its receipts and sources; memax_push proposes a memory (in spaces on the V2 record a person keeps it, in the agent or in Review); memax_capture saves session notes; memax_forget asks a person to forget a memory; memax_request_decision puts a question in front of the person. memax_list, memax_hubs, memax_hub_members and memax_topics browse spaces.";

export interface McpToolDefinition {
  name: string;
  title: string;
  description: string;
  inputSchema: {
    type: "object";
    properties: Record<string, unknown>;
    required?: string[];
  };
  outputSchema?: Record<string, unknown>;
  annotations: {
    title: string;
    readOnlyHint: boolean;
    destructiveHint?: boolean;
    idempotentHint: boolean;
    openWorldHint: boolean;
  };
}

/** The tools, with output schemas still naming the item as {"$ref": "#item"}. */
export const MCP_TOOLS: McpToolDefinition[] = [
  {
    name: "memax_recall",
    title: "Recall memories",
    description:
      "Returns the memories relevant to a query from the spaces this connection can read. In spaces on the V2 record it returns kept memories, plus this session's own pending proposals marked proposed; without a query it returns a digest of each space (its latest compiled file, or its top kept memories by section, and what changed since this connection was last seen). In other spaces it returns ranked excerpts of saved memories.",
    inputSchema: {
      type: "object",
      properties: {
        query: {
          type: "string",
          description:
            "What to look for, in natural language. Omit it for each space's digest.",
        },
        limit: {
          type: "number",
          description: "Maximum number of results.",
        },
        topic_id: {
          type: "string",
          description: "Restrict results to memories in this topic (UUID).",
        },
        hub_id: {
          type: "string",
          description:
            "Hub or space ID, slug, or 'personal'. Its results rank first; a space on the V2 record is read alone.",
        },
        space_id: {
          type: "string",
          description: "Same as hub_id.",
        },
        project_context: {
          type: "object",
          description:
            "Current project context for relevance. Keys: repo (git remote URL), project (short name).",
        },
        session_ref: {
          type: "string",
          description:
            "The agent session this call belongs to. Proposals pushed with the same session_ref are included, marked proposed.",
        },
      },
    },
    outputSchema: {
      type: "object",
      properties: {
        results: {
          type: "array",
          items: {
            $ref: "#item",
          },
        },
        proposals: {
          type: "array",
          description: "This session's own proposals, still waiting in Review.",
          items: {
            $ref: "#item",
          },
        },
        digest: {
          type: "array",
          description: "Without a query: one entry per space on the V2 record.",
          items: {
            type: "object",
            properties: {
              space_id: {
                type: "string",
              },
              space: {
                type: "string",
              },
              sections: {
                type: "array",
                items: {
                  type: "object",
                  properties: {
                    section: {
                      type: "string",
                    },
                    memories: {
                      type: "array",
                      items: {
                        $ref: "#item",
                      },
                    },
                  },
                  required: ["section", "memories"],
                },
              },
              changed_since: {
                type: "string",
              },
              changed: {
                type: "integer",
              },
              waiting_in_review: {
                type: "integer",
              },
              review_url: {
                type: "string",
              },
              compiled: {
                type: "object",
                description:
                  "The space's latest compiled file (AGENTS.md, or the target delivered over MCP); sections are empty when it is present.",
                properties: {
                  ref: {
                    type: "string",
                    description: "The compile run, such as C-0881.",
                  },
                  target: {
                    type: "string",
                  },
                  compiled_at: {
                    type: "string",
                  },
                  content: {
                    type: "string",
                  },
                  truncated: {
                    type: "boolean",
                  },
                },
                required: ["ref", "target", "compiled_at", "content"],
              },
            },
            required: ["space_id", "space", "sections"],
          },
        },
        notices: {
          type: "array",
          items: {
            type: "object",
            properties: {
              kind: {
                type: "string",
              },
              message: {
                type: "string",
              },
            },
            required: ["kind", "message"],
          },
        },
        partial: {
          type: "boolean",
          description:
            "Some spaces were left out because they didn't answer in time.",
        },
        lexical_only: {
          type: "boolean",
          description:
            "Results from spaces on the V2 record were matched by words, not by meaning.",
        },
      },
      required: ["results"],
    },
    annotations: {
      title: "Recall memories",
      readOnlyHint: true,
      idempotentHint: true,
      openWorldHint: false,
    },
  },
  {
    name: "memax_search",
    title: "Search memories",
    description:
      "Searches kept memories and decisions in the spaces this connection can read and returns the matches with their display IDs. In spaces that haven't switched to the V2 record it searches saved memories.",
    inputSchema: {
      type: "object",
      properties: {
        query: {
          type: "string",
          description: "Words or a question to search for.",
        },
        kind: {
          type: "string",
          enum: ["fact", "decision"],
          description: "Only memories of this kind.",
        },
        space_id: {
          type: "string",
          description:
            "Space ID, slug, or 'personal'. Omit it to search every space this connection can read.",
        },
        limit: {
          type: "number",
          description: "Maximum number of results (default 10).",
        },
      },
      required: ["query"],
    },
    outputSchema: {
      type: "object",
      properties: {
        results: {
          type: "array",
          items: {
            $ref: "#item",
          },
        },
        partial: {
          type: "boolean",
        },
        lexical_only: {
          type: "boolean",
        },
      },
      required: ["results"],
    },
    annotations: {
      title: "Search memories",
      readOnlyHint: true,
      idempotentHint: true,
      openWorldHint: false,
    },
  },
  {
    name: "memax_push",
    title: "Propose a memory",
    description:
      "Saves one piece of knowledge to a space. In a space on the V2 record it becomes a proposal that a person keeps: when the client supports elicitation the person is asked in the agent (keep, edit, or leave it as a proposal), otherwise it waits in Review; an agent connected at Write keeps its own first-party statements. In other spaces Memax saves and classifies it.",
    inputSchema: {
      type: "object",
      properties: {
        content: {
          type: "string",
          description:
            "The knowledge to save. In a space on the V2 record, one statement of at most 2000 characters.",
        },
        title: {
          type: "string",
          description: "Optional title (generated if omitted).",
        },
        hint: {
          type: "string",
          description:
            "Context that helps Memax process it, such as 'This is my resume' or 'Meeting notes from product review'.",
        },
        tags: {
          type: "array",
          items: {
            type: "string",
          },
          description: "Tags for the memory.",
        },
        initiation_type: {
          type: "string",
          description:
            "How this save was initiated: human_requested_agent, agent_proactive, agent_automatic, import, or unknown. A tool call is never human_direct; that value is ignored.",
        },
        project_context: {
          type: "object",
          description:
            "Project context. Keys: repo (git remote URL), project (short name), branch.",
        },
        hub_id: {
          type: "string",
          description:
            "Target hub or space ID, slug, or 'personal'. Required for a team hub.",
        },
        space_id: {
          type: "string",
          description: "Same as hub_id.",
        },
        hub_reason: {
          type: "string",
          description:
            "Why this belongs in the shared hub. Required for a team hub that hasn't switched to the V2 record; on the V2 record it becomes the receipt's reason.",
        },
        section: {
          type: "string",
          enum: ["decisions", "conventions", "preferences", "open_question"],
          description:
            "Where it belongs in a space on the V2 record (default conventions). A memory in decisions is recorded as a decision.",
        },
        sources: {
          type: "array",
          description:
            "Where the statement comes from. Content from a URL, an email or an issue is external and always waits in Review.",
          items: {
            type: "object",
            properties: {
              kind: {
                type: "string",
                enum: [
                  "session",
                  "pr",
                  "file",
                  "url",
                  "issue",
                  "email",
                  "note",
                ],
              },
              ref: {
                type: "string",
                description:
                  "What a person sees, such as 'PR #212' or 'go.mod:14'.",
              },
              uri: {
                type: "string",
                description: "A URL or repository path, if any.",
              },
            },
            required: ["kind", "ref"],
          },
        },
        session_ref: {
          type: "string",
          description:
            "The agent session this call belongs to. Recalls with the same session_ref include the proposal before it is kept.",
        },
      },
      required: ["content"],
    },
    outputSchema: {
      type: "object",
      properties: {
        status: {
          type: "string",
          enum: ["saved", "kept", "proposed", "note"],
          description:
            "saved (a space that hasn't switched), kept, proposed (waiting in Review) or note.",
        },
        id: {
          type: "string",
          description: "M-0219 on the V2 record; a UUID otherwise.",
        },
        space_id: {
          type: "string",
        },
        review_url: {
          type: "string",
          description: "Where a person keeps the proposal.",
        },
        assurance: {
          type: "string",
          description: "For a keep confirmed in the agent: client_attested.",
        },
        message: {
          type: "string",
        },
      },
      required: ["status", "id"],
    },
    annotations: {
      title: "Propose a memory",
      readOnlyHint: false,
      destructiveHint: false,
      idempotentHint: false,
      openWorldHint: false,
    },
  },
  {
    name: "memax_get",
    title: "Open a memory",
    description:
      "Returns one memory in full by its display ID (M-0219, with its space) or its UUID. On the V2 record it includes the receipts and sources.",
    inputSchema: {
      type: "object",
      properties: {
        id: {
          type: "string",
          description: "A display ID such as M-0219, or a memory UUID.",
        },
        space_id: {
          type: "string",
          description:
            "The space of a display ID (ID, slug, or 'personal'). Optional with a UUID.",
        },
      },
      required: ["id"],
    },
    outputSchema: {
      type: "object",
      properties: {
        memory: {
          type: "object",
          properties: {
            id: {
              type: "string",
            },
            ref: {
              type: "string",
            },
            record: {
              type: "string",
              enum: ["v1", "v2"],
            },
            space_id: {
              type: "string",
            },
            space: {
              type: "string",
            },
            title: {
              type: "string",
            },
            text: {
              type: "string",
            },
            summary: {
              type: "string",
            },
            section: {
              type: "string",
            },
            kind: {
              type: "string",
            },
            state: {
              type: "string",
            },
            stability: {
              type: "string",
            },
            trust: {
              type: "string",
            },
            version: {
              type: "integer",
            },
            source: {
              type: "string",
            },
            tags: {
              type: "array",
              items: {
                type: "string",
              },
            },
            created_at: {
              type: "string",
            },
            url: {
              type: "string",
            },
            sources: {
              type: "array",
              items: {
                type: "object",
                properties: {
                  kind: {
                    type: "string",
                  },
                  ref: {
                    type: "string",
                  },
                  uri: {
                    type: "string",
                  },
                  external: {
                    type: "boolean",
                  },
                  trust: {
                    type: "string",
                  },
                },
                required: ["kind", "ref"],
              },
            },
            receipts: {
              type: "array",
              items: {
                type: "object",
                properties: {
                  action: {
                    type: "string",
                  },
                  actor_kind: {
                    type: "string",
                  },
                  agent: {
                    type: "string",
                  },
                  via: {
                    type: "string",
                  },
                  assurance: {
                    type: "string",
                  },
                  reason: {
                    type: "string",
                  },
                  at: {
                    type: "string",
                  },
                },
                required: ["action", "actor_kind", "via", "at"],
              },
            },
          },
          required: ["id", "record", "text"],
        },
      },
      required: ["memory"],
    },
    annotations: {
      title: "Open a memory",
      readOnlyHint: true,
      idempotentHint: true,
      openWorldHint: false,
    },
  },
  {
    name: "memax_list",
    title: "List memories",
    description:
      "Lists memories newest first, a page at a time; pass the cursor from the previous page for the next one. In a space on the V2 record it lists kept memories, and topic_id may name a section.",
    inputSchema: {
      type: "object",
      properties: {
        limit: {
          type: "number",
          description: "Max results (default 20, max 50).",
        },
        cursor: {
          type: "string",
          description: "Pagination cursor from the previous page.",
        },
        sort: {
          type: "string",
          description: "newest (default) or relevant.",
        },
        hub_id: {
          type: "string",
          description: "Hub or space ID, slug, or 'personal' to list.",
        },
        space_id: {
          type: "string",
          description: "Same as hub_id.",
        },
        topic_id: {
          type: "string",
          description:
            "Topic ID (UUID) to filter by; in a space on the V2 record, a section (decisions, conventions, preferences or open_question).",
        },
      },
    },
    outputSchema: {
      type: "object",
      properties: {
        memories: {
          type: "array",
          items: {
            $ref: "#item",
          },
        },
        next_cursor: {
          type: "string",
        },
        has_more: {
          type: "boolean",
        },
        total: {
          type: "integer",
        },
      },
      required: ["memories"],
    },
    annotations: {
      title: "List memories",
      readOnlyHint: true,
      idempotentHint: true,
      openWorldHint: false,
    },
  },
  {
    name: "memax_hubs",
    title: "List spaces",
    description:
      "Lists the hubs (spaces) this connection can read, with their IDs, slugs, roles and memory counts, and for spaces on the V2 record this agent's autonomy there.",
    inputSchema: {
      type: "object",
      properties: {},
    },
    annotations: {
      title: "List spaces",
      readOnlyHint: true,
      idempotentHint: true,
      openWorldHint: false,
    },
  },
  {
    name: "memax_hub_members",
    title: "List space members",
    description: "Lists the members of a hub (space) and their roles.",
    inputSchema: {
      type: "object",
      properties: {
        hub_id: {
          type: "string",
          description: "Hub or space ID, slug, or 'personal'.",
        },
        space_id: {
          type: "string",
          description: "Same as hub_id.",
        },
      },
    },
    annotations: {
      title: "List space members",
      readOnlyHint: true,
      idempotentHint: true,
      openWorldHint: false,
    },
  },
  {
    name: "memax_forget",
    title: "Forget a memory",
    description:
      "Forgets a memory by ID. In a space on the V2 record an agent can't forget: this asks a person, who confirms on the web, and the result says where. In other spaces it deletes the memory.",
    inputSchema: {
      type: "object",
      properties: {
        id: {
          type: "string",
          description: "A display ID such as M-0219, or a memory UUID.",
        },
        space_id: {
          type: "string",
          description: "The space of a display ID. Optional with a UUID.",
        },
      },
      required: ["id"],
    },
    annotations: {
      title: "Forget a memory",
      readOnlyHint: false,
      destructiveHint: true,
      idempotentHint: true,
      openWorldHint: false,
    },
  },
  {
    name: "memax_capture",
    title: "Capture session notes",
    description:
      "Saves a summary of a work session, with its decisions and learnings, as notes. In a space on the V2 record notes are raw material that Dream folds into proposals for Review; elsewhere each fact is extracted into its own memory.",
    inputSchema: {
      type: "object",
      properties: {
        summary: {
          type: "string",
          description: "What the session accomplished.",
        },
        decisions: {
          type: "array",
          items: {
            type: "string",
          },
          description:
            "Decisions made, such as 'Chose PostgreSQL over MongoDB'.",
        },
        learnings: {
          type: "array",
          items: {
            type: "string",
          },
          description:
            "Things learned, such as 'pg_trgm is language-agnostic'.",
        },
      },
      required: ["summary"],
    },
    annotations: {
      title: "Capture session notes",
      readOnlyHint: false,
      destructiveHint: false,
      idempotentHint: false,
      openWorldHint: false,
    },
  },
  {
    name: "memax_request_decision",
    title: "Ask a person to decide",
    description:
      "Puts a decision in front of the person: a question with 2 to 4 options, shown in Memax with a notification. It returns at once with the request's ID; the person's answer is saved as a memory that later recalls return.",
    inputSchema: {
      type: "object",
      properties: {
        question: {
          type: "string",
          description: "The decision, as one user-facing question.",
        },
        options: {
          type: "array",
          items: {
            type: "string",
          },
          description: "2 to 4 mutually exclusive choices, as short labels.",
        },
        context: {
          type: "string",
          description:
            "Why it matters: tradeoffs, constraints, and a recommendation if there is one.",
        },
      },
      required: ["question", "options"],
    },
    annotations: {
      title: "Ask a person to decide",
      readOnlyHint: false,
      destructiveHint: false,
      idempotentHint: false,
      openWorldHint: false,
    },
  },
  {
    name: "memax_topics",
    title: "Browse topics",
    description:
      "Without topic_id, returns the topic tree with memory counts; with topic_id, the memories in that topic. In a space on the V2 record the topics are its sections (decisions, conventions, preferences, open_question) and their kept memories.",
    inputSchema: {
      type: "object",
      properties: {
        topic_id: {
          type: "string",
          description:
            "Topic ID, or a section on the V2 record. Omit it for the whole tree.",
        },
        hub_id: {
          type: "string",
          description: "Hub or space ID, slug, or 'personal'.",
        },
        space_id: {
          type: "string",
          description: "Same as hub_id.",
        },
      },
    },
    annotations: {
      title: "Browse topics",
      readOnlyHint: true,
      idempotentHint: true,
      openWorldHint: false,
    },
  },
];

/** expandItemRefs replaces {"$ref": "#item"} with the item schema, as the remote server serves it. */
export function expandItemRefs(schema: unknown): unknown {
  if (Array.isArray(schema)) return schema.map(expandItemRefs);
  if (schema && typeof schema === "object") {
    const entries = Object.entries(schema as Record<string, unknown>);
    if (
      entries.length === 1 &&
      entries[0][0] === "$ref" &&
      entries[0][1] === "#item"
    ) {
      return structuredClone(MCP_ITEM_SCHEMA);
    }
    return Object.fromEntries(entries.map(([k, v]) => [k, expandItemRefs(v)]));
  }
  return schema;
}

/** The tools as served: output schemas self-contained. */
export function servedTools(): McpToolDefinition[] {
  return MCP_TOOLS.map((t) => ({
    ...t,
    ...(t.outputSchema
      ? {
          outputSchema: expandItemRefs(t.outputSchema) as Record<
            string,
            unknown
          >,
        }
      : {}),
  }));
}
