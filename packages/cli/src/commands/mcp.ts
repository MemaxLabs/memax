import { Command } from "commander";
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import { getClient, setClientAgent } from "../lib/client.js";
import { getActiveHubID } from "../lib/config.js";
import {
  findHubMatch,
  getHubReference,
  PERSONAL_HUB_ALIAS,
} from "../lib/hubs.js";

const listMemoriesInputSchema = {
  type: "object" as const,
  properties: {
    limit: {
      type: "number",
      description: "Max results to return (default 20, max 50)",
    },
    cursor: {
      type: "string",
      description:
        "Pagination cursor from previous response (omit for first page)",
    },
    sort: {
      type: "string",
      description: "Sort by: newest (default) or relevant",
    },
    hub_id: {
      type: "string",
      description: 'Optional hub ID, slug, or "personal" to filter results.',
    },
    topic_id: {
      type: "string",
      description: "Optional topic ID (UUID) to filter results.",
    },
  },
};

function memoryClassification(memory: {
  kind?: string;
  stability?: string;
}): string {
  return [memory.kind, memory.stability].filter(Boolean).join("/");
}

function memberDisplayName(member: {
  user_name?: string;
  user_email?: string;
  user_id: string;
}): string {
  return member.user_name || member.user_email || member.user_id;
}

async function resolveHubReference(ref: string | undefined): Promise<string> {
  const hubs = await getClient().hubs.list();
  const hubRef = ref ?? getActiveHubID() ?? PERSONAL_HUB_ALIAS;
  const match = findHubMatch(hubs, hubRef);
  if (!match) {
    throw new Error(
      "Hub not found or not accessible. Use memax_hubs to list available hubs.",
    );
  }
  return match.hub.id;
}

function createServer(agentId: string = ""): Server {
  const server = new Server(
    { name: "memax", version: "0.0.1" },
    { capabilities: { tools: {} } },
  );

  server.setRequestHandler(ListToolsRequestSchema, async () => ({
    tools: [
      {
        name: "memax_recall",
        description:
          "Search every hub the current token can access with a natural language query. " +
          "Returns relevant memories ranked by relevance. Use this when you need " +
          "background information about the project, team processes, or past decisions.",
        inputSchema: {
          type: "object" as const,
          properties: {
            query: {
              type: "string",
              description: "Natural language search query",
            },
            limit: {
              type: "number",
              description: "Max results to return (default 10)",
            },
            topic_id: {
              type: "string",
              description: "Restrict results to memories in this topic (UUID).",
            },
            hub_id: {
              type: "string",
              description:
                "Hub ID, slug, or 'personal'. Boosts ranking for this hub.",
            },
            project_context: {
              type: "object",
              description:
                "Current project context for relevance boosting. Keys: repo (git remote URL), project (short name).",
            },
          },
          required: ["query"],
        },
      },
      {
        name: "memax_push",
        description:
          "Save a piece of knowledge to the user's Memax knowledge base. " +
          "Memax classifies it automatically for retrieval.",
        inputSchema: {
          type: "object" as const,
          properties: {
            content: {
              type: "string",
              description: "The knowledge content to save",
            },
            title: {
              type: "string",
              description: "Optional title (auto-generated if omitted)",
            },
            hint: {
              type: "string",
              description:
                "Context hint to help AI process this memory (e.g. 'This is my resume', 'Meeting notes from product review'). Improves classification, summarization, and retrieval.",
            },
            tags: {
              type: "array",
              items: { type: "string" },
              description: "Tags for the memory",
            },
            initiation_type: {
              type: "string",
              description:
                "How this save was initiated: human_direct, human_requested_agent, agent_proactive, agent_automatic, import, or unknown.",
            },
            project_context: {
              type: "object",
              description:
                "Project context (auto-detected by CLI). Keys: repo (git remote URL), project (short name), branch.",
            },
            hub_id: {
              type: "string",
              description:
                "Target hub ID. Required when pushing into a team hub.",
            },
            hub_reason: {
              type: "string",
              description:
                "Why this belongs in the shared hub. Required when hub_id targets a team hub.",
            },
          },
          required: ["content"],
        },
      },
      {
        name: "memax_get",
        description:
          "Get the full content of a specific memory by ID. " +
          "Use this after memax_recall to read the complete content of a relevant memory.",
        inputSchema: {
          type: "object" as const,
          properties: {
            id: {
              type: "string",
              description:
                "The memory ID (from memax_recall or memax_list results)",
            },
          },
          required: ["id"],
        },
      },
      {
        name: "memax_list",
        description:
          "List memories with pagination. Use cursor from the previous response for the next page. Returns total count.",
        inputSchema: listMemoriesInputSchema,
      },
      {
        name: "memax_hubs",
        description:
          "List hubs the current user can access, including hub IDs, slugs, roles, and memory counts.",
        inputSchema: {
          type: "object" as const,
          properties: {},
        },
      },
      {
        name: "memax_hub_members",
        description:
          'List members for a hub. Requires hub_id (hub ID, slug, or "personal").',
        inputSchema: {
          type: "object" as const,
          properties: {
            hub_id: {
              type: "string",
              description: 'Hub ID, slug, or "personal".',
            },
          },
          required: ["hub_id"],
        },
      },
      {
        name: "memax_forget",
        description:
          "Delete a memory from the user's Memax knowledge base by ID. " +
          "Use this to remove outdated, incorrect, or duplicate memories.",
        inputSchema: {
          type: "object" as const,
          properties: {
            id: {
              type: "string",
              description:
                "The memory ID to delete (from memax_recall or memax_list results)",
            },
          },
          required: ["id"],
        },
      },
      {
        name: "memax_capture",
        description:
          "Capture key decisions, learnings, and context from this session. " +
          "Call at the end of a significant work session to save what was accomplished " +
          "and what should be remembered. Each fact is extracted and stored as a separate searchable memory.",
        inputSchema: {
          type: "object" as const,
          properties: {
            summary: {
              type: "string",
              description:
                "Brief summary of what was accomplished in this session",
            },
            decisions: {
              type: "array",
              items: { type: "string" },
              description:
                "Key decisions made (e.g., 'Chose PostgreSQL over MongoDB')",
            },
            learnings: {
              type: "array",
              items: { type: "string" },
              description:
                "Things learned (e.g., 'pg_trgm is language-agnostic')",
            },
          },
          required: ["summary"],
        },
      },
      {
        name: "memax_topics",
        description:
          "Browse the user's knowledge topics. Without topic_id: returns full topic tree with memory counts. With topic_id: returns memories in that topic.",
        inputSchema: {
          type: "object" as const,
          properties: {
            topic_id: {
              type: "string",
              description: "Topic ID to browse. Omit for full tree.",
            },
            hub_id: {
              type: "string",
              description: "Hub ID to scope topics.",
            },
          },
        },
      },
    ],
  }));

  server.setRequestHandler(CallToolRequestSchema, async (request) => {
    const { name, arguments: args } = request.params;

    switch (name) {
      case "memax_recall": {
        const typedArgs = args as {
          query: string;
          limit?: number;
          topic_id?: string;
          hub_id?: string;
          project_context?: Record<string, string>;
        };
        try {
          let hubId = getActiveHubID() || undefined;
          if (typedArgs.hub_id) {
            try {
              hubId = await resolveHubReference(typedArgs.hub_id);
            } catch {
              // Invalid hub ref — fall back to active hub (hub_id is a ranking boost, not a gate)
            }
          }
          const result = await getClient().recall(typedArgs.query, {
            limit: typedArgs.limit ?? 10,
            topicId: typedArgs.topic_id,
            source: "mcp",
            workingDir: process.cwd(),
            projectContext: typedArgs.project_context,
            hubId,
          });

          const memories = result.memories ?? [];
          if (memories.length === 0) {
            return {
              content: [{ type: "text" as const, text: "No results found." }],
            };
          }

          const formatted = memories
            .map((m, i) => {
              const score = (m.relevance_score * 100).toFixed(0);
              const heading = m.heading_chain ? ` — ${m.heading_chain}` : "";
              const parts = [
                `[${i + 1}] ${m.title} [${memoryClassification(m)}, ${score}%, ${m.age}] (id: ${m.id})${heading}`,
              ];
              if (m.summary) {
                parts.push(`Summary: ${m.summary}`);
              }
              parts.push(`Relevant excerpt:\n${m.chunk_content}`);
              return parts.join("\n");
            })
            .join("\n\n");

          return { content: [{ type: "text" as const, text: formatted }] };
        } catch (err) {
          return {
            content: [
              {
                type: "text" as const,
                text: `Recall failed: ${(err as Error).message}`,
              },
            ],
            isError: true,
          };
        }
      }

      case "memax_push": {
        const typedArgs = args as {
          content: string;
          title?: string;
          hint?: string;
          tags?: string[];
          initiation_type?: string;
          project_context?: Record<string, string>;
          hub_id?: string;
          hub_reason?: string;
        };
        try {
          const memory = await getClient().push(typedArgs.content, {
            title: typedArgs.title ?? "",
            hint: typedArgs.hint ?? "",
            tags: typedArgs.tags ?? [],
            source: "mcp",
            sourceAgent: agentId,
            initiationType:
              (typedArgs.initiation_type as
                | "human_direct"
                | "human_requested_agent"
                | "agent_proactive"
                | "agent_automatic"
                | "import"
                | "unknown"
                | undefined) ?? undefined,
            projectContext: typedArgs.project_context,
            hubId: typedArgs.hub_id,
            hubReason: typedArgs.hub_reason,
          });

          return {
            content: [
              {
                type: "text" as const,
                text: `Saved: ${memory.title} (id: ${memory.id})`,
              },
            ],
          };
        } catch (err) {
          return {
            content: [
              {
                type: "text" as const,
                text: `Push failed: ${(err as Error).message}`,
              },
            ],
            isError: true,
          };
        }
      }

      case "memax_get": {
        const typedArgs = args as { id: string };
        try {
          const client = getClient();
          const memory = await client.memories.get(typedArgs.id);
          // Agent calling memax_get is deliberate intent to read the
          // full memory — same contract as the web modal / detail page
          // and the remote Go MCP toolGet. Fire-and-forget so the
          // signal never blocks the response.
          void client.memories.trackAccessed(typedArgs.id).catch(() => {});

          const parts = [
            `# ${memory.title}`,
            `Classification: ${memoryClassification(memory)} | Source: ${memory.source} | Created: ${memory.created_at}`,
          ];
          if (memory.tags?.length > 0) {
            parts.push(`Tags: ${memory.tags.join(", ")}`);
          }
          if (memory.source_path) {
            parts.push(`Source: ${memory.source_path}`);
          }
          if (memory.summary) {
            parts.push(`\n## Summary\n${memory.summary}`);
          }
          parts.push(`\n## Content\n${memory.content}`);

          return {
            content: [{ type: "text" as const, text: parts.join("\n") }],
          };
        } catch (err) {
          return {
            content: [
              {
                type: "text" as const,
                text: `Get failed: ${(err as Error).message}`,
              },
            ],
            isError: true,
          };
        }
      }

      case "memax_list": {
        const typedArgs = args as {
          limit?: number;
          cursor?: string;
          sort?: string;
          hub_id?: string;
          topic_id?: string;
        };
        try {
          let hubId: string | undefined;
          if (typedArgs.hub_id) {
            hubId = await resolveHubReference(typedArgs.hub_id);
          }
          const res = await getClient().memories.list({
            limit: typedArgs.limit ?? 20,
            cursor: typedArgs.cursor,
            sort: typedArgs.sort as "newest" | "relevant" | undefined,
            hubId,
            topicId: typedArgs.topic_id,
          });

          const memories = res.memories ?? [];
          const total = res.total ?? 0;
          const nextCursor = res.next_cursor ?? "";
          const hasMore = res.has_more ?? false;

          if (memories.length === 0) {
            return {
              content: [
                {
                  type: "text" as const,
                  text: `No memories found. (${total} total in workspace)`,
                },
              ],
            };
          }

          let formatted = memories
            .map(
              (m) =>
                `- ${m.title} [${memoryClassification(m)}] — ${m.source} (id: ${m.id})`,
            )
            .join("\n");

          formatted += `\n\nShowing ${memories.length} of ${total} total.`;
          if (hasMore) {
            formatted += ` More available — pass cursor: "${nextCursor}" to get next page.`;
          }

          return { content: [{ type: "text" as const, text: formatted }] };
        } catch (err) {
          return {
            content: [
              {
                type: "text" as const,
                text: `List failed: ${(err as Error).message}`,
              },
            ],
            isError: true,
          };
        }
      }

      case "memax_hubs": {
        try {
          const hubs = await getClient().hubs.list();
          if (hubs.length === 0) {
            return {
              content: [{ type: "text" as const, text: "No hubs found." }],
            };
          }

          const activeHubID = getActiveHubID();
          const text = hubs
            .map(({ hub, role, memory_count }) => {
              const active = hub.id === activeHubID ? " active" : "";
              return `- **${hub.name}** (${hub.hub_type}, ${role}${active}) ref: ${getHubReference(hub)} id: ${hub.id} memories: ${memory_count}`;
            })
            .join("\n");

          return { content: [{ type: "text" as const, text }] };
        } catch (err) {
          return {
            content: [
              {
                type: "text" as const,
                text: `Hubs failed: ${(err as Error).message}`,
              },
            ],
            isError: true,
          };
        }
      }

      case "memax_hub_members": {
        const typedArgs = args as { hub_id?: string };
        try {
          const hubID = await resolveHubReference(typedArgs.hub_id);
          const result = await getClient().hubs.get(hubID);
          const members = result.members ?? [];
          let text = `## ${result.hub.name} members\n\n`;
          if (members.length === 0) {
            text += "No members found.";
          } else {
            text += members
              .map((member) => {
                const email = member.user_email
                  ? ` <${member.user_email}>`
                  : "";
                return `- **${memberDisplayName(member)}**${email} [${member.role}] joined: ${member.joined_at}`;
              })
              .join("\n");
          }
          return { content: [{ type: "text" as const, text }] };
        } catch (err) {
          return {
            content: [
              {
                type: "text" as const,
                text: `Hub members failed: ${(err as Error).message}`,
              },
            ],
            isError: true,
          };
        }
      }

      case "memax_forget": {
        const typedArgs = args as { id: string };
        if (!typedArgs.id) {
          return {
            content: [
              { type: "text" as const, text: "Memory ID is required." },
            ],
            isError: true,
          };
        }
        try {
          await getClient().memories.delete(typedArgs.id);
          return {
            content: [
              {
                type: "text" as const,
                text: `Forgotten: ${typedArgs.id}`,
              },
            ],
          };
        } catch (err) {
          return {
            content: [
              {
                type: "text" as const,
                text: `Forget failed: ${(err as Error).message}`,
              },
            ],
            isError: true,
          };
        }
      }

      case "memax_capture": {
        const typedArgs = args as {
          summary: string;
          decisions?: string[];
          learnings?: string[];
        };
        if (!typedArgs.summary) {
          return {
            content: [{ type: "text" as const, text: "Summary is required." }],
            isError: true,
          };
        }

        // Build structured content for the extraction pipeline
        let content = `## Session Summary\n${typedArgs.summary}\n`;
        if (typedArgs.decisions?.length) {
          content += `\n## Decisions Made\n${typedArgs.decisions.map((d) => `- ${d}`).join("\n")}\n`;
        }
        if (typedArgs.learnings?.length) {
          content += `\n## Learnings\n${typedArgs.learnings.map((l) => `- ${l}`).join("\n")}\n`;
        }

        try {
          const memory = await getClient().push(content, {
            title: `Session capture — ${new Date().toLocaleDateString()}`,
            contentType: "transcript",
            source: "mcp/capture",
            sourceAgent: agentId,
            initiationType: "agent_automatic",
          });
          return {
            content: [
              {
                type: "text" as const,
                text: `Session captured (id: ${memory.id}). Key facts will be extracted and stored as separate memories.`,
              },
            ],
          };
        } catch (err) {
          return {
            content: [
              {
                type: "text" as const,
                text: `Capture failed: ${(err as Error).message}`,
              },
            ],
            isError: true,
          };
        }
      }

      case "memax_topics": {
        const typedArgs = args as {
          topic_id?: string;
          hub_id?: string;
        };
        try {
          if (typedArgs.topic_id) {
            // Browse specific topic's memories
            const res = await getClient().topics.listMemories(
              typedArgs.topic_id,
            );
            const topic = await getClient().topics.get(typedArgs.topic_id);
            const memories = res.memories ?? [];
            let text = `## ${topic.name} (${memories.length} memories)\n\n`;
            for (const [i, m] of memories.entries()) {
              text += `${i + 1}. **${m.title}** [${memoryClassification(m)}] (id: ${m.id})\n`;
              if (m.summary) text += `   ${m.summary}\n`;
            }
            if (memories.length === 0)
              text += "No memories in this topic yet.\n";
            return { content: [{ type: "text" as const, text }] };
          }

          // Full topic tree
          const res = await getClient().topics.list(typedArgs.hub_id);
          const topics = res.topics ?? [];
          let text = "## Topics\n\n";
          if (topics.length === 0) {
            text +=
              "No topics yet. Push memories and run a dream cycle to auto-organize.\n";
          } else {
            for (const t of topics) {
              const indent = t.parent_id ? "  " : "";
              text += `${indent}- **${t.name}** (${t.memory_count} memories) [id: ${t.id}]\n`;
              if (t.description) text += `${indent}  ${t.description}\n`;
              for (const child of t.children) {
                text += `  - **${child.name}** (${child.memory_count} memories) [id: ${child.id}]\n`;
              }
            }
          }
          if (res.unassigned_count > 0) {
            text += `\n📥 **Inbox**: ${res.unassigned_count} unassigned memories\n`;
          }
          return { content: [{ type: "text" as const, text }] };
        } catch (err) {
          return {
            content: [
              {
                type: "text" as const,
                text: `Topics failed: ${(err as Error).message}`,
              },
            ],
            isError: true,
          };
        }
      }

      default:
        return {
          content: [{ type: "text" as const, text: `Unknown tool: ${name}` }],
          isError: true,
        };
    }
  });

  return server;
}

async function mcpServeCommand(options: { agent?: string }): Promise<void> {
  setClientAgent(options.agent);
  const server = createServer(options.agent ?? "");
  const transport = new StdioServerTransport();
  await server.connect(transport);

  // Keep the process alive — some agent launchers close stdin early
  // which makes Node think the event loop is empty and exit.
  await new Promise<void>((resolve) => {
    process.on("SIGINT", resolve);
    process.on("SIGTERM", resolve);
    // Also resolve if stdin closes (transport disconnected)
    process.stdin.on("end", resolve);
  });

  await server.close();
}

export function registerMcpCommand(program: Command): void {
  const mcp = program
    .command("mcp")
    .description("Model Context Protocol server for AI agents");

  mcp
    .command("serve")
    .description("Start MCP server on stdio")
    .option("--agent <name>", "Agent identity (e.g., claude-code, cursor)")
    .action(mcpServeCommand);
}
