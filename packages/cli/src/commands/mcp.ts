import { Command } from "commander";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import type { RecalledMemory } from "memax-sdk";
import { getClient, setClientAgent } from "../lib/client.js";
import { getActiveHubID } from "../lib/config.js";
import {
  findHubMatch,
  getHubReference,
  PERSONAL_HUB_ALIAS,
} from "../lib/hubs.js";
import { MCP_INSTRUCTIONS, MCP_SERVER_NAME, servedTools } from "./mcp-tools.js";
import {
  errorResult,
  guardWrite,
  isDisplayRef,
  MAX_STATEMENT,
  type McpItem,
  type McpTextResult,
  textResult,
  v2Forget,
  v2Get,
  v2HubLines,
  v2List,
  v2Push,
  v2Recall,
  v2Space,
  v2State,
  v2Topics,
} from "./mcp-v2.js";

// The local stdio MCP server: the same tools as the remote one
// (mcp-tools.ts mirrors the remote catalogue; scripts/check-mcp-parity.mjs
// holds them equal). In spaces on the V2 record the tools go through
// memax.v2 (mcp-v2.ts); everywhere else they keep their V1 behaviour.

const __dirname = dirname(fileURLToPath(import.meta.url));

/** The CLI's version, reported as the server's. */
function cliVersion(): string {
  try {
    return (
      JSON.parse(
        readFileSync(join(__dirname, "..", "..", "package.json"), "utf-8"),
      ).version ?? "0.0.0"
    );
  } catch {
    return "0.0.0";
  }
}

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

function recalledItems(memories: RecalledMemory[]): McpItem[] {
  return memories.map((m) => ({
    id: m.id,
    record: "v1",
    ...(m.hub_id ? { space_id: m.hub_id } : {}),
    ...(m.hub_name ? { space: m.hub_name } : {}),
    title: m.title,
    text: m.chunk_content,
    ...(m.summary ? { summary: m.summary } : {}),
    kind: m.kind,
    stability: m.stability,
    ...(m.relevance_score ? { score: m.relevance_score } : {}),
    age: m.age,
    source: m.source,
  }));
}

function formatRecalled(memories: RecalledMemory[]): string {
  return memories
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
}

type Args = Record<string, unknown>;

function str(v: unknown): string | undefined {
  return typeof v === "string" && v.trim() !== "" ? v.trim() : undefined;
}

// --- Reads across both records ---

async function recallTool(
  args: Args,
  agentId: string,
  search: boolean,
): Promise<McpTextResult> {
  const query = str(args.query) ?? "";
  const limit =
    typeof args.limit === "number" && args.limit > 0
      ? args.limit
      : search
        ? 10
        : 10;
  const ref = str(args.hub_id) ?? str(args.space_id);
  if (search && !query) return errorResult("Say what to search for in query.");
  try {
    const state = await v2State();
    let target: string | undefined;
    if (ref) {
      try {
        target = await resolveHubReference(ref);
      } catch {
        // Invalid hub ref — fall back to the active hub (a ranking boost).
      }
    }
    const targetV2 = target ? state.spaces.get(target) : undefined;
    const readable = [...state.readable]
      .map((id) => state.spaces.get(id)!)
      .filter((sp) => !targetV2 || sp.id === targetV2.id);
    if (targetV2 && !state.readable.has(targetV2.id)) {
      return errorResult(
        `This agent isn't connected to ${targetV2.name}, so it can't read it. Connect it in Agents.`,
      );
    }
    const v2 =
      readable.length > 0
        ? await v2Recall(readable, query, limit, str(args.kind))
        : undefined;

    // V1 hubs: the V1 recall, without the V1 memories (notes) of spaces on V2.
    let v1: RecalledMemory[] = [];
    if (!targetV2 && query && !(search && str(args.kind))) {
      const result = await getClient().recall(query, {
        limit,
        topicId: str(args.topic_id),
        source: "mcp",
        workingDir: process.cwd(),
        projectContext: args.project_context as
          | Record<string, string>
          | undefined,
        hubId: target ?? getActiveHubID() ?? undefined,
      });
      v1 = (result.memories ?? []).filter(
        (m) => !m.hub_id || !state.spaces.has(m.hub_id),
      );
    }

    const results = [...(v2?.results ?? []), ...recalledItems(v1)];
    const structured: Record<string, unknown> = { results };
    if (!search && v2?.proposals.length) structured.proposals = v2.proposals;
    if (!search && v2?.digest.length) structured.digest = v2.digest;
    if (v2) structured.lexical_only = true;

    let text = v2?.text ?? "";
    if (v1.length) {
      if (text) text += "\n\nFrom spaces not on V2 yet:\n";
      text += formatRecalled(v1);
    }
    if (!text) {
      text =
        !query && v2
          ? "Nothing kept yet in the spaces this connection can read."
          : "No results found.";
    }
    void agentId;
    return textResult(text, structured);
  } catch (err) {
    return errorResult(
      `${search ? "Search" : "Recall"} failed: ${(err as Error).message}`,
    );
  }
}

// --- The server ---

function createServer(agentId: string = ""): Server {
  const server = new Server(
    { name: MCP_SERVER_NAME, version: cliVersion() },
    { capabilities: { tools: {} }, instructions: MCP_INSTRUCTIONS },
  );

  server.setRequestHandler(ListToolsRequestSchema, async () => ({
    tools: servedTools(),
  }));

  server.setRequestHandler(CallToolRequestSchema, async (request) => {
    const { name } = request.params;
    const args = (request.params.arguments ?? {}) as Args;

    switch (name) {
      case "memax_recall":
        return recallTool(args, agentId, false);

      case "memax_search":
        return recallTool(args, agentId, true);

      case "memax_push": {
        const typedArgs = args as {
          content: string;
          title?: string;
          hint?: string;
          tags?: string[];
          initiation_type?: string;
          project_context?: Record<string, string>;
          hub_id?: string;
          space_id?: string;
          hub_reason?: string;
          section?: string;
          session_ref?: string;
          sources?: { kind: string; ref: string; uri?: string }[];
        };
        const hubRef = typedArgs.hub_id ?? typedArgs.space_id;
        try {
          const target = await resolveHubReference(hubRef).catch(
            () => undefined,
          );
          const sp = target ? await v2Space(target) : undefined;
          if (sp) {
            const denied = await guardWrite(sp);
            if (denied) return denied;
            if (
              typedArgs.content?.trim() &&
              [...typedArgs.content.trim()].length <= MAX_STATEMENT
            ) {
              return v2Push(sp, typedArgs);
            }
            // Longer than one statement: a note, for Dream to fold.
          }
          const memory = await getClient().push(typedArgs.content, {
            title: typedArgs.title ?? "",
            hint: typedArgs.hint ?? "",
            tags: typedArgs.tags ?? [],
            source: "mcp",
            sourceAgent: agentId,
            // A tool call is never human_direct (same rule as the Go MCP
            // server drops server-side); keep the wire honest here too.
            initiationType:
              typedArgs.initiation_type === "human_direct"
                ? undefined
                : ((typedArgs.initiation_type as
                    | "human_requested_agent"
                    | "agent_proactive"
                    | "agent_automatic"
                    | "import"
                    | "unknown"
                    | undefined) ?? undefined),
            projectContext: typedArgs.project_context,
            hubId: hubRef,
            hubReason: typedArgs.hub_reason,
          });
          if (sp) {
            const message = `Saved as a note in ${sp.name}: a memory is one statement of at most ${MAX_STATEMENT} characters. Dream folds notes into proposals for Review.`;
            return textResult(
              `Saved as a note (id: ${memory.id}). ${message}`,
              { status: "note", id: memory.id, space_id: sp.id, message },
            );
          }
          return textResult(`Saved: ${memory.title} (id: ${memory.id})`, {
            status: "saved",
            id: memory.id,
            ...(memory.hub_id ? { space_id: memory.hub_id } : {}),
          });
        } catch (err) {
          return errorResult(`Push failed: ${(err as Error).message}`);
        }
      }

      case "memax_get": {
        const id = str(args.id) ?? "";
        try {
          const spaceRef = str(args.space_id);
          const spaceId = spaceRef
            ? await resolveHubReference(spaceRef)
            : undefined;
          const sp = spaceId ? await v2Space(spaceId) : undefined;
          if (isDisplayRef(id) || sp || (await v2State()).spaces.size > 0) {
            const res = await v2Get(id, sp);
            if (res) return res;
            if (isDisplayRef(id)) return errorResult(`Memory not found: ${id}`);
          }
          const client = getClient();
          const memory = await client.memories.get(id);
          if (memory.hub_id && (await v2Space(memory.hub_id))) {
            return errorResult(
              `${id} is a note in a space on the V2 record. Notes are raw material for Dream, not context; search kept memories with memax_search.`,
            );
          }
          // Agent calling memax_get is deliberate intent to read the
          // full memory — same contract as the web modal / detail page
          // and the remote Go MCP toolGet. Fire-and-forget so the
          // signal never blocks the response.
          void client.memories.trackAccessed(id).catch(() => {});

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

          return textResult(parts.join("\n"), {
            memory: {
              id: memory.id,
              record: "v1",
              ...(memory.hub_id ? { space_id: memory.hub_id } : {}),
              title: memory.title,
              text: memory.content,
              ...(memory.summary ? { summary: memory.summary } : {}),
              kind: memory.kind,
              stability: memory.stability,
              source: memory.source,
              tags: memory.tags ?? [],
              created_at: memory.created_at,
            },
          });
        } catch (err) {
          return errorResult(`Get failed: ${(err as Error).message}`);
        }
      }

      case "memax_list": {
        const typedArgs = args as {
          limit?: number;
          cursor?: string;
          sort?: string;
          hub_id?: string;
          space_id?: string;
          topic_id?: string;
        };
        try {
          let hubId: string | undefined;
          const ref = typedArgs.hub_id ?? typedArgs.space_id;
          if (ref) {
            hubId = await resolveHubReference(ref);
            const sp = await v2Space(hubId);
            if (sp) return v2List(sp, typedArgs);
          }
          const res = await getClient().memories.list({
            limit: typedArgs.limit ?? 20,
            cursor: typedArgs.cursor,
            sort: typedArgs.sort as "newest" | "relevant" | undefined,
            hubId,
            topicId: typedArgs.topic_id,
          });

          const state = await v2State();
          const memories = (res.memories ?? []).filter(
            (m) => !m.hub_id || !state.spaces.has(m.hub_id),
          );
          const total = res.total ?? 0;
          const nextCursor = res.next_cursor ?? "";
          const hasMore = res.has_more ?? false;
          const structured = {
            memories: memories.map((m) => ({
              id: m.id,
              record: "v1",
              ...(m.hub_id ? { space_id: m.hub_id } : {}),
              title: m.title,
              text: m.title,
              kind: m.kind,
              stability: m.stability,
              source: m.source,
            })),
            ...(nextCursor ? { next_cursor: nextCursor } : {}),
            ...(hasMore ? { has_more: true } : {}),
            ...(total ? { total } : {}),
          };

          if (memories.length === 0) {
            return textResult(
              `No memories found. (${total} total in workspace)`,
              structured,
            );
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
          return textResult(formatted, structured);
        } catch (err) {
          return errorResult(`List failed: ${(err as Error).message}`);
        }
      }

      case "memax_hubs": {
        try {
          const hubs = await getClient().hubs.list();
          const state = await v2State();
          const activeHubID = getActiveHubID();
          const lines = hubs
            .filter(({ hub }) => !state.spaces.has(hub.id))
            .map(({ hub, role, memory_count }) => {
              const active = hub.id === activeHubID ? " active" : "";
              return `- **${hub.name}** (${hub.hub_type}, ${role}${active}) ref: ${getHubReference(hub)} id: ${hub.id} memories: ${memory_count}`;
            });
          lines.push(...(await v2HubLines(hubs, activeHubID)));
          if (lines.length === 0) return textResult("No hubs found.");
          return textResult(lines.join("\n"));
        } catch (err) {
          return errorResult(`Hubs failed: ${(err as Error).message}`);
        }
      }

      case "memax_hub_members": {
        const typedArgs = args as { hub_id?: string; space_id?: string };
        try {
          const hubID = await resolveHubReference(
            typedArgs.hub_id ?? typedArgs.space_id,
          );
          const state = await v2State();
          const sp = state.spaces.get(hubID);
          if (sp && !state.readable.has(sp.id)) {
            return errorResult(
              `This agent isn't connected to ${sp.name}, so it can't read it. Connect it in Agents.`,
            );
          }
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
          return textResult(text);
        } catch (err) {
          return errorResult(`Hub members failed: ${(err as Error).message}`);
        }
      }

      case "memax_forget": {
        const id = str(args.id);
        if (!id) {
          return errorResult("Memory ID is required.");
        }
        try {
          const spaceRef = str(args.space_id);
          const spaceId = spaceRef
            ? await resolveHubReference(spaceRef)
            : undefined;
          const sp = spaceId ? await v2Space(spaceId) : undefined;
          if (isDisplayRef(id) || sp) {
            if (!sp)
              return errorResult(
                `Pass space_id with ${id}: display IDs live in a space.`,
              );
            return v2Forget(sp, id.toUpperCase());
          }
          const state = await v2State();
          if (state.spaces.size > 0) {
            const found = await v2Get(id, undefined);
            if (found && !found.isError) {
              const memory = (found.structuredContent?.memory ?? {}) as {
                ref?: string;
                space_id?: string;
              };
              const space = memory.space_id
                ? state.spaces.get(memory.space_id)
                : undefined;
              if (space && memory.ref) return v2Forget(space, memory.ref);
            }
            const v1 = await getClient()
              .memories.get(id)
              .catch(() => undefined);
            if (v1?.hub_id && state.spaces.has(v1.hub_id)) {
              return errorResult(
                `${id} is a note in a space on the V2 record, and an agent can't forget there. Ask the person to forget it on the web.`,
              );
            }
          }
          await getClient().memories.delete(id);
          return textResult(`Forgotten: ${id}`);
        } catch (err) {
          return errorResult(`Forget failed: ${(err as Error).message}`);
        }
      }

      case "memax_capture": {
        const typedArgs = args as {
          summary: string;
          decisions?: string[];
          learnings?: string[];
        };
        if (!typedArgs.summary) {
          return errorResult("Summary is required.");
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
          const target = await resolveHubReference(undefined).catch(
            () => undefined,
          );
          const sp = target ? await v2Space(target) : undefined;
          if (sp) {
            const denied = await guardWrite(sp);
            if (denied) return denied;
          }
          const memory = await getClient().push(content, {
            title: `Session capture — ${new Date().toLocaleDateString()}`,
            contentType: "transcript",
            source: "mcp/capture",
            sourceAgent: agentId,
            initiationType: "agent_automatic",
          });
          if (sp) {
            return textResult(
              `Session captured (id: ${memory.id}). Saved as notes in ${sp.name}: Dream folds them into proposals for Review, and nothing is kept until a person keeps it.`,
            );
          }
          return textResult(
            `Session captured (id: ${memory.id}). Key facts will be extracted and stored as separate memories.`,
          );
        } catch (err) {
          return errorResult(`Capture failed: ${(err as Error).message}`);
        }
      }

      case "memax_request_decision": {
        const typedArgs = args as {
          question: string;
          options?: string[];
          context?: string;
        };
        if (!typedArgs.question || (typedArgs.options?.length ?? 0) < 2) {
          return errorResult(
            "memax_request_decision requires 'question' and 2-4 'options'.",
          );
        }
        try {
          // Resolve to a real hub UUID: the REST path is
          // /v1/hubs/{id}/… and the server's membership check can't
          // read an alias like "personal".
          const hubId = await resolveHubReference(undefined);
          const sp = await v2Space(hubId);
          if (sp) {
            const denied = await guardWrite(sp);
            if (denied) return denied;
          }
          // On the V2 record too, this is a board decision card until
          // decision gates (G-) land (epic 1.11).
          const { slot } = await getClient().boards.requestDecision(hubId, {
            question: typedArgs.question,
            options: typedArgs.options ?? [],
            context: typedArgs.context,
            source_agent: agentId,
          });
          return textResult(
            `Decision card created (id: ${slot.slot_key}). The user has been pinged; their choice will be saved to memory. Recall with keywords from your question later to read it. Continue with other work now.`,
          );
        } catch (err) {
          return errorResult(
            `Decision request failed: ${(err as Error).message}`,
          );
        }
      }

      case "memax_topics": {
        const typedArgs = args as {
          topic_id?: string;
          hub_id?: string;
          space_id?: string;
        };
        try {
          const ref = typedArgs.hub_id ?? typedArgs.space_id;
          const hubId = ref
            ? await resolveHubReference(ref)
            : await resolveHubReference(undefined).catch(() => undefined);
          const sp = hubId ? await v2Space(hubId) : undefined;
          if (sp) return v2Topics(sp, typedArgs.topic_id);

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
            return textResult(text);
          }

          // Full topic tree
          const res = await getClient().topics.list(ref);
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
          return textResult(text);
        } catch (err) {
          return errorResult(`Topics failed: ${(err as Error).message}`);
        }
      }

      default:
        return errorResult(`Unknown tool: ${name}`);
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

export { createServer as createMcpServerForTest };
