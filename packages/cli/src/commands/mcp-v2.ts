// The local stdio MCP server in spaces on the V2 record. Each space moves
// to V2 on its own (its /v2 Space carries v2_enabled_at); in the others
// the tools keep their V1 behaviour. Everything here goes through
// memax.v2 with X-Memax-Via: mcp, so the server's ledger and policy decide
// every write, exactly as for the remote MCP server.
//
// Differences from the remote server, because /v2 has no search endpoint
// yet and a local key can never keep:
//   - recall and search rank the space's newest kept memories (up to 200)
//     by the words they share with the query, locally;
//   - this process is one agent session, so read-after-write covers the
//     proposals this process made;
//   - nobody is asked to keep in the agent: an API key proposes, and the
//     person keeps in Review (the result links there).
import { createHash } from "node:crypto";
import { MemaxError, refusalOf, type V2 } from "memax-sdk";
import { getClient, usesAPIKey } from "../lib/client.js";
import { loadConfig } from "../lib/config.js";

export interface McpTextResult {
  [key: string]: unknown;
  content: { type: "text"; text: string }[];
  structuredContent?: Record<string, unknown>;
  isError?: boolean;
}

export function textResult(
  text: string,
  structured?: Record<string, unknown>,
): McpTextResult {
  return {
    content: [{ type: "text", text }],
    ...(structured ? { structuredContent: structured } : {}),
  };
}

export function errorResult(text: string): McpTextResult {
  return { content: [{ type: "text", text }], isError: true };
}

/** A result item, the shape of the remote server's MCPItem. */
export interface McpItem {
  id: string;
  ref?: string;
  record: "v1" | "v2";
  space_id?: string;
  space?: string;
  title?: string;
  text: string;
  summary?: string;
  section?: string;
  kind?: string;
  state?: string;
  stability?: string;
  score?: number;
  age?: string;
  source?: string;
  url?: string;
}

interface V2State {
  /** Spaces on the V2 record, by id. */
  spaces: Map<string, V2.Space>;
  /** The ones this credential may read. */
  readable: Set<string>;
  /** This agent's autonomy per space, for an API key. */
  autonomy: Map<string, string>;
  at: number;
}

const STATE_TTL_MS = 30_000;
let cached: V2State | undefined;

/** Which spaces are on V2, and which this credential reads (cached). */
export async function v2State(): Promise<V2State> {
  if (cached && Date.now() - cached.at < STATE_TTL_MS) return cached;
  const state: V2State = {
    spaces: new Map(),
    readable: new Set(),
    autonomy: new Map(),
    at: Date.now(),
  };
  const client = getClient();
  try {
    const { items } = await client.v2.spaces.list();
    for (const sp of items) {
      if (sp.v2_enabled_at) state.spaces.set(sp.id, sp);
    }
  } catch {
    // A server without /v2: everything is V1.
    cached = state;
    return state;
  }
  if (state.spaces.size > 0) {
    if (usesAPIKey()) {
      // An agent reads only the spaces it's connected to.
      const { items } = await client.v2.agents.list();
      for (const conn of items) {
        if (conn.state === "disconnected") continue;
        for (const sp of conn.spaces) {
          if (!state.spaces.has(sp.space_id)) continue;
          state.readable.add(sp.space_id);
          state.autonomy.set(
            sp.space_id,
            conn.state === "paused" ? "paused" : sp.autonomy,
          );
        }
      }
    } else {
      for (const id of state.spaces.keys()) state.readable.add(id);
    }
  }
  cached = state;
  return state;
}

/** The web app's origin, from the API's (or MEMAX_APP_URL). */
export function appBaseURL(): string {
  const env = process.env.MEMAX_APP_URL?.trim();
  if (env) return env.replace(/\/+$/, "");
  try {
    const api = new URL(loadConfig().api_url);
    if (api.hostname === "api.memax.app") return "https://memax.app";
    if (api.hostname === "staging-api.memaxlabs.com")
      return "https://staging-app.memaxlabs.com";
    if (api.hostname === "localhost" || api.hostname === "127.0.0.1")
      return "http://localhost:3000";
  } catch {
    // fall through
  }
  return "";
}

function spaceURL(sp: V2.Space, place: string): string {
  const base = appBaseURL();
  return base ? `${base}/${encodeURIComponent(sp.slug)}/${place}` : "";
}

export function reviewURL(sp: V2.Space, ref: string): string {
  const u = spaceURL(sp, "review");
  return u ? `${u}?ref=${encodeURIComponent(ref)}` : "";
}

function memoryURL(sp: V2.Space, ref: string): string {
  const u = spaceURL(sp, "memories");
  return u ? `${u}/${encodeURIComponent(ref)}` : "";
}

function item(sp: V2.Space, m: V2.Memory, score?: number): McpItem {
  return {
    id: m.id,
    ref: m.ref,
    record: "v2",
    space_id: sp.id,
    space: sp.name,
    text: m.statement,
    section: m.section,
    kind: m.kind,
    state: m.state,
    ...(score ? { score } : {}),
    url: memoryURL(sp, m.ref) || undefined,
  };
}

const SECTIONS = [
  "decisions",
  "conventions",
  "preferences",
  "open_question",
] as const;
const SECTION_LABELS: Record<string, string> = {
  decisions: "Decisions",
  conventions: "Conventions",
  preferences: "Preferences",
  open_question: "Open questions",
};

/** The memories this process proposed: read-after-write for this session. */
const sessionProposals = new Map<string, string>(); // ref → space id

function notConnected(sp: V2.Space): McpTextResult {
  const agents = spaceURL(sp, "agents");
  return errorResult(
    `This agent isn't connected to ${sp.name}, so it can't read it. Connect it in Agents${agents ? ` at ${agents}` : ""}.`,
  );
}

/** The V2 space a hub id names, or undefined for a V1 hub. */
export async function v2Space(hubId: string): Promise<V2.Space | undefined> {
  return (await v2State()).spaces.get(hubId);
}

/** Refuses a write an agent may not make in a space (Read, paused, not connected). */
export async function guardWrite(
  sp: V2.Space,
): Promise<McpTextResult | undefined> {
  const state = await v2State();
  if (!usesAPIKey()) return undefined;
  const level = state.autonomy.get(sp.id);
  if (level === undefined)
    return errorResult(
      `This agent isn't connected to ${sp.name}, so it can only read. Connect it in Agents.`,
    );
  if (level === "paused")
    return errorResult(
      "This agent is paused, so it can only read. Resume it in Agents.",
    );
  if (level === "read")
    return errorResult(
      `This agent is read-only in ${sp.name}. Change it in Agents.`,
    );
  return undefined;
}

// --- Writes ---

export interface PushArgs {
  content: string;
  hub_reason?: string;
  section?: string;
  session_ref?: string;
  sources?: { kind: string; ref: string; uri?: string }[];
}

export const MAX_STATEMENT = 2000;

/** memax_push in a space on V2: a proposal (or a keep at Write). */
export async function v2Push(
  sp: V2.Space,
  args: PushArgs,
): Promise<McpTextResult> {
  const statement = args.content.trim();
  const section = (args.section ?? "conventions") as V2.Section;
  if (!SECTIONS.includes(section as (typeof SECTIONS)[number]))
    return errorResult(
      "section must be decisions, conventions, preferences or open_question.",
    );
  const input: V2.RememberInput = {
    statement,
    section,
    ...(section === "decisions" ? { kind: "decision" as V2.MemoryKind } : {}),
    ...(args.sources?.length
      ? {
          sources: args.sources.map((s) => ({
            kind: s.kind as V2.SourceKind,
            ref: s.ref,
            ...(s.uri ? { uri: s.uri } : {}),
          })),
        }
      : {}),
    ...(args.hub_reason?.trim() ? { reason: args.hub_reason.trim() } : {}),
    ...(args.session_ref?.trim()
      ? { session_ref: args.session_ref.trim() }
      : {}),
  };
  // The same statement pushed again in this session is the same command.
  const idempotencyKey =
    "mcp-push:" +
    createHash("sha256")
      .update(JSON.stringify([sp.id, process.pid, input]))
      .digest("base64url");
  try {
    const res = await getClient().v2.memories.remember(sp.id, input, {
      idempotencyKey,
      via: "mcp",
    });
    const m = res.memory;
    if (res.outcome === "applied") {
      const text = `Kept ${m.ref} in ${sp.name}.`;
      return textResult(text, {
        status: "kept",
        id: m.ref,
        space_id: sp.id,
        message: text,
      });
    }
    sessionProposals.set(m.ref, sp.id);
    const review = reviewURL(sp, m.ref);
    let text = `Proposed ${m.ref} in ${sp.name}.`;
    if (res.policy.quarantine)
      text +=
        " It cites an outside source, so it is quarantined until a person keeps it on the web.";
    else if (res.policy.code === "decision_needs_web")
      text += ` Decisions in ${sp.name} need a person on the web.`;
    else text += " It waits in Review.";
    if (review) text += ` Keep it in Review: ${review}`;
    return textResult(text, {
      status: "proposed",
      id: m.ref,
      space_id: sp.id,
      ...(review ? { review_url: review } : {}),
      message: text,
    });
  } catch (err) {
    return errorResult(
      err instanceof MemaxError
        ? err.message
        : `Push failed: ${(err as Error).message}`,
    );
  }
}

/**
 * memax_forget in a space on V2. An agent never forgets (rule 7): it asks,
 * and the request (receipted) waits for a person to forget the memory, or
 * keep it, on the web. There is no in-agent confirmation, as on the remote
 * server: Forget can't be undone. A person's own session is pointed to the
 * web, where they see everything it goes with.
 */
export async function v2Forget(
  sp: V2.Space,
  ref: string,
  args: { reason?: string; session_ref?: string } = {},
): Promise<McpTextResult> {
  const client = getClient();
  let m: V2.Memory;
  try {
    m = (await client.v2.memories.get(ref, { space: sp.id })).memory;
  } catch (err) {
    if (err instanceof MemaxError && err.status === 404)
      return errorResult(`Memory not found: ${ref}`);
    return errorResult(
      err instanceof MemaxError ? err.message : (err as Error).message,
    );
  }
  if (m.lifecycle === "forgotten")
    return textResult(
      `${m.ref} is already forgotten: its words are gone from Memax, every compiled file and every agent.`,
    );
  const where = memoryURL(sp, m.ref);
  if (!usesAPIKey())
    return textResult(
      `${m.ref} wasn't forgotten: forget it yourself on the web${where ? ` at ${where}` : ""}, where you see everything it goes with.`,
    );
  const reason = args.reason?.trim() ?? "";
  const day = new Date().toISOString().slice(0, 10);
  // Asking again the same day, for the same reason, is the same request.
  const idempotencyKey =
    "mcp-forget:" +
    createHash("sha256")
      .update([sp.id, m.id, reason, day].join("\0"))
      .digest("base64url");
  try {
    const res = await client.v2.memories.requestForget(
      m.id,
      {
        ...(reason ? { reason } : {}),
        ...(args.session_ref?.trim()
          ? { session_ref: args.session_ref.trim() }
          : {}),
      },
      { idempotencyKey, via: "mcp" },
    );
    let text =
      res.forget_request.status === "declined"
        ? `A person kept ${m.ref} when it was asked before`
        : `${m.ref} wasn't forgotten yet: an agent can't forget. It now waits for a person to forget it`;
    if (where) text += ` on the web: ${where}`;
    text += `. Until then it stays kept in ${sp.name}. Forgetting it removes it from Memax, every compiled file and every agent, and can't be undone.`;
    return textResult(text);
  } catch (err) {
    const refusal = refusalOf(err);
    if (refusal?.message) return errorResult(refusal.message);
    return errorResult(
      err instanceof MemaxError
        ? err.message
        : `Forget failed: ${(err as Error).message}`,
    );
  }
}

/** The _meta key of the notices a response carries (as the remote server's). */
export const META_NOTICES = "app.memax/notices";
const MAX_NOTICES = 20;

/** What the agent reads about a notice (the remote server's words). */
export function noticeMessage(n: V2.Notice, space: string): string {
  if (n.kind === "switched") {
    if (n.autonomy === "read")
      return `${space} moved to Memax V2, and you can only read there now: memax_push and memax_capture are refused. A person can change that in Agents.`;
    if (n.autonomy === "write")
      return `${space} moved to Memax V2. You keep your own work there; anything that touches a decision in force goes to a person in Review.`;
    return `${space} moved to Memax V2. What you save there is now proposed, and a person keeps it in Review; recall serves what people kept. A person can change what you may do in Agents.`;
  }
  if (n.kind === "space_forgotten")
    return `Everything in ${space} was forgotten. Drop anything you took from it, including what you saved in your own memory.`;
  return `Forgotten in ${space}: ${n.refs.join(", ")}. Drop anything you took from it, including what you saved in your own memory; it is gone from Memax and every compiled file.`;
}

/**
 * Forget's notices (rule 7: every agent that read it is told on its next
 * read). An API key's waiting notices ride on its next response, whatever
 * the tool, as on the remote server: a paragraph in the text,
 * _meta["app.memax/notices"], and recall's (and search's) `notices`. They
 * are acknowledged once the response is built, so each is told once.
 */
export async function withNotices(
  res: McpTextResult,
  tool: string,
): Promise<McpTextResult> {
  if (!usesAPIKey()) return res;
  let state: V2State;
  let notices: V2.Notice[];
  try {
    state = await v2State();
    if (state.spaces.size === 0) return res;
    notices = (await getClient().v2.notices.list()).notices.slice(
      0,
      MAX_NOTICES,
    );
  } catch {
    return res; // Telling the agent never fails its call.
  }
  if (!notices.length) return res;
  const meta = notices.map((n) => {
    const space = state.spaces.get(n.space_id)?.name ?? n.space ?? "a space";
    return {
      kind: n.kind,
      space_id: n.space_id,
      space,
      refs: n.refs,
      message: noticeMessage(n, space),
    };
  });
  const text = meta.map((m) => m.message).join("\n");
  const content = [...res.content];
  if (content.length)
    content[0] = { ...content[0], text: `${content[0].text}\n\n${text}` };
  else content.push({ type: "text", text });
  const out: McpTextResult = {
    ...res,
    content,
    _meta: {
      ...((res._meta as Record<string, unknown> | undefined) ?? {}),
      [META_NOTICES]: meta,
    },
  };
  if (
    (tool === "memax_recall" || tool === "memax_search") &&
    out.structuredContent
  ) {
    const existing = Array.isArray(out.structuredContent.notices)
      ? (out.structuredContent.notices as unknown[])
      : [];
    out.structuredContent = {
      ...out.structuredContent,
      notices: [
        ...existing,
        ...meta.map(({ kind, message, space_id, refs }) => ({
          kind,
          message,
          space_id,
          refs,
        })),
      ],
    };
  }
  const ids = notices.map((n) => n.id);
  await getClient()
    .v2.notices.ack(
      { ids },
      {
        idempotencyKey:
          "mcp-notices:" +
          createHash("sha256").update(ids.join(",")).digest("base64url"),
        via: "mcp",
      },
    )
    .catch(() => undefined); // told again next time, rather than never
  return out;
}

// --- Reads ---

async function keptIn(sp: V2.Space, limit = 200): Promise<V2.Memory[]> {
  const page = await getClient().v2.memories.list(sp.id, {
    state: ["kept", "stale", "conflict"],
    limit,
  });
  return page.items.filter((m) => m.lifecycle === "kept");
}

function terms(text: string): string[] {
  return (text.toLowerCase().match(/[\p{L}\p{N}]+/gu) ?? []).filter(
    (w) => w.length > 1,
  );
}

/** Ranks statements by the query words (and prefixes) they contain. */
export function rankLocally(
  query: string,
  memories: { statement: string }[],
): number[] {
  const q = [...new Set(terms(query))];
  return memories.map((m) => {
    const words = terms(m.statement);
    let score = 0;
    for (const t of q) {
      if (words.includes(t)) score += 1;
      else if (words.some((w) => w.startsWith(t) || t.startsWith(w)))
        score += 0.5;
    }
    return q.length ? score / q.length : 0;
  });
}

export interface V2ReadPart {
  results: McpItem[];
  proposals: McpItem[];
  digest: Record<string, unknown>[];
  text: string;
}

const MAX_COMPILED = 32 * 1024;

/**
 * A space's latest compiled file: the target delivered over MCP, else
 * AGENTS.md, else the ChatGPT copy-out (as the remote server picks it).
 */
interface CompiledDigest {
  [key: string]: unknown;
  ref: string;
  target: string;
  compiled_at: string;
  content: string;
  truncated?: boolean;
}

async function latestCompiled(
  sp: V2.Space,
): Promise<CompiledDigest | undefined> {
  const client = getClient();
  const { items } = await client.v2.targets.list(sp.id);
  const live = items.filter((t) => t.sync_state !== "off");
  const target =
    live.find((t) => t.delivery === "mcp") ??
    live.find((t) => t.kind === "agents_md") ??
    live.find((t) => t.kind === "chatgpt");
  if (!target) return undefined;
  const preview = await client.v2.targets.preview(target.id);
  const output = preview.files[0] ?? preview.copies[0];
  if (!preview.compile || !output) return undefined;
  const truncated = output.content.length > MAX_COMPILED;
  return {
    ref: preview.compile.ref,
    target: target.label,
    compiled_at: preview.compile.compiled_at,
    content: truncated ? output.content.slice(0, MAX_COMPILED) : output.content,
    ...(truncated ? { truncated: true } : {}),
  };
}

/**
 * memax_search's include_notes: the notes (V1 memories of spaces on the V2
 * record) the person the key works for may read, as the remote server
 * returns them.
 */
export async function v2Notes(
  spaces: V2.Space[],
  query: string,
  limit: number,
): Promise<{ results: Record<string, unknown>[]; text: string }> {
  const client = getClient();
  const results: Record<string, unknown>[] = [];
  for (const sp of spaces) {
    const page = await client.v2.notes.search(sp.id, { q: query, limit });
    for (const n of page.items) {
      results.push({
        id: n.id,
        ...(n.ref ? { ref: n.ref } : {}),
        record: "note",
        space_id: n.space_id,
        space: sp.name,
        title: n.title,
        text: n.excerpt.trim() || n.title,
        ...(n.path ? { source: n.path } : {}),
        ...(n.score ? { score: n.score } : {}),
        state: "note",
      });
    }
  }
  results.splice(limit);
  if (results.length === 0) return { results, text: "" };
  const lines = ["Notes (V1 memories, never kept context):"];
  results.forEach((it, i) =>
    lines.push(
      `[${i + 1}] ${String(it.ref ?? it.id)} (${String(it.space)}) ${String(it.text)}`,
    ),
  );
  return { results, text: lines.join("\n") };
}

/** The V2 part of a recall or search over the readable spaces. */
export async function v2Recall(
  spaces: V2.Space[],
  query: string,
  limit: number,
  kind?: string,
): Promise<V2ReadPart> {
  const part: V2ReadPart = { results: [], proposals: [], digest: [], text: "" };
  const lines: string[] = [];
  const client = getClient();
  if (query.trim() === "") {
    for (const sp of spaces) {
      const waiting = (await client.v2.review.list(sp.id, { limit: 1 })).total;
      // The space's latest compile, when it has one, is the digest.
      const compiled = await latestCompiled(sp).catch(() => undefined);
      if (compiled) {
        part.digest.push({
          space_id: sp.id,
          space: sp.name,
          sections: [],
          ...(waiting ? { waiting_in_review: waiting } : {}),
          compiled,
        });
        lines.push(`## ${sp.name}`);
        if (waiting) lines.push(`${waiting} waiting in Review`);
        lines.push(
          `Compiled ${compiled.ref} · ${compiled.target} · ${compiled.compiled_at}`,
          "",
          compiled.content.trim(),
          "",
        );
        continue;
      }
      const kept = await keptIn(sp);
      const sections = SECTIONS.map((section) => ({
        section,
        memories: kept
          .filter((m) => m.section === section)
          .slice(0, 8)
          .map((m) => item(sp, m)),
      })).filter((s) => s.memories.length > 0);
      part.digest.push({
        space_id: sp.id,
        space: sp.name,
        sections,
        ...(waiting ? { waiting_in_review: waiting } : {}),
      });
      lines.push(`## ${sp.name}`);
      if (waiting) lines.push(`${waiting} waiting in Review`);
      for (const s of sections) {
        lines.push(`### ${SECTION_LABELS[s.section]}`);
        for (const m of s.memories) lines.push(`- ${m.ref} ${m.text}`);
      }
      lines.push("");
    }
  } else {
    const scored: { sp: V2.Space; m: V2.Memory; score: number }[] = [];
    for (const sp of spaces) {
      const kept = (await keptIn(sp)).filter((m) => !kind || m.kind === kind);
      const scores = rankLocally(query, kept);
      kept.forEach((m, i) => {
        if (scores[i] > 0) scored.push({ sp, m, score: scores[i] });
      });
    }
    scored.sort((a, b) => b.score - a.score);
    part.results = scored.slice(0, limit).map((s) => item(s.sp, s.m, s.score));
    if (part.results.length) {
      lines.push("Kept:");
      part.results.forEach((it, i) =>
        lines.push(
          `[${i + 1}] ${it.ref} (${it.space} · ${it.section}, ${it.state}) ${it.text}`,
        ),
      );
      lines.push("");
    }
  }
  // This process is one agent session: its own proposals still waiting.
  const mine = [...sessionProposals.entries()].filter(([, spaceId]) =>
    spaces.some((sp) => sp.id === spaceId),
  );
  if (mine.length && kind === undefined) {
    for (const sp of spaces) {
      const page = await client.v2.memories.list(sp.id, {
        state: "proposed",
        limit: 50,
      });
      for (const m of page.items) {
        if (sessionProposals.get(m.ref) === sp.id)
          part.proposals.push(item(sp, m));
      }
    }
    if (part.proposals.length) {
      lines.push("Proposed in this session, waiting in Review:");
      part.proposals.forEach((it, i) =>
        lines.push(`[${i + 1}] ${it.ref} (${it.space}) ${it.text}`),
      );
    }
  }
  part.text = lines.join("\n").trim();
  return part;
}

export function isDisplayRef(id: string): boolean {
  return /^M-\d+$/i.test(id.trim());
}

/** memax_get on V2: the memory with its receipts and sources. */
export async function v2Get(
  ref: string,
  sp: V2.Space | undefined,
): Promise<McpTextResult | undefined> {
  const state = await v2State();
  if (sp && !state.readable.has(sp.id)) return notConnected(sp);
  let detail: V2.MemoryDetail;
  try {
    detail = await getClient().v2.memories.get(ref, {
      ...(sp ? { space: sp.id } : {}),
    });
  } catch (err) {
    if (err instanceof MemaxError && err.status === 404) return undefined;
    return errorResult(
      err instanceof MemaxError ? err.message : (err as Error).message,
    );
  }
  const m = detail.memory;
  const space = state.spaces.get(m.space_id);
  if (!space) return undefined; // a memory in a space not on V2
  if (!state.readable.has(space.id)) return notConnected(space);
  if (m.lifecycle === "rejected")
    return errorResult(`Memory not found: ${ref}`);
  if (m.lifecycle === "proposed") {
    // Rule 11: a write kept at once that the judge then found contradicting
    // a decision in force is back in Review (the remote server says the same).
    const returned = m.flags.includes("conflict")
      ? [...detail.receipts.items]
          .sort((a, b) => b.seq - a.seq)
          .find((r) => r.action === "returned")
      : undefined;
    if (returned) {
      const what = returned.source?.ref
        ? `${returned.source.ref}, a decision in force`
        : "a decision in force";
      return errorResult(
        `${m.ref} is back in Review in ${space.name}: it was kept at once, then Memax found it contradicts ${what}. It isn't kept now, so don't act on it; a person keeps it or settles the conflict.`,
      );
    }
    return errorResult(
      `${m.ref} is a proposal waiting in Review in ${space.name}; it can be read once a person keeps it.`,
    );
  }
  const lines = [
    `# ${m.ref} · ${space.name}`,
    `State: ${m.state} | Section: ${m.section} | Kind: ${m.kind} | Trust: ${m.trust} | Version: ${m.version}`,
    "",
    m.lifecycle === "forgotten"
      ? "This memory was forgotten: its words are gone, and only its receipts remain."
      : m.statement,
  ];
  const sources = (m.sources ?? []).map((s) => ({
    kind: s.kind,
    ref: s.ref,
    ...(s.uri ? { uri: s.uri } : {}),
    ...(s.external ? { external: true } : {}),
    trust: s.trust,
  }));
  if (sources.length) {
    lines.push("", "## Sources");
    for (const s of sources)
      lines.push(
        `- ${s.kind}: ${s.ref}${s.uri ? ` (${s.uri})` : ""}${s.external ? " [external]" : ""}`,
      );
  }
  const receipts = [...detail.receipts.items].reverse().map((r) => ({
    action: r.action,
    actor_kind: r.actor_kind,
    ...(r.agent ? { agent: r.agent } : {}),
    via: r.via,
    ...(r.assurance ? { assurance: r.assurance } : {}),
    ...(r.reason ? { reason: r.reason } : {}),
    at: r.occurred_at,
  }));
  if (receipts.length) {
    lines.push("", "## Receipts");
    for (const r of receipts)
      lines.push(
        `- ${r.at} ${r.action} by ${r.actor_kind}${r.agent ? ` via ${r.agent}` : ""} (${r.via}${r.assurance ? `, ${r.assurance}` : ""})${r.reason ? `: ${r.reason}` : ""}`,
      );
  }
  return textResult(lines.join("\n").trim(), {
    memory: {
      id: m.id,
      ref: m.ref,
      record: "v2",
      space_id: space.id,
      space: space.name,
      text: m.statement,
      section: m.section,
      kind: m.kind,
      state: m.state,
      trust: m.trust,
      version: m.version,
      created_at: m.created_at,
      ...(memoryURL(space, m.ref) ? { url: memoryURL(space, m.ref) } : {}),
      sources,
      receipts,
    },
  });
}

/** memax_list on a space on V2: its kept memories, newest first. */
export async function v2List(
  sp: V2.Space,
  args: { limit?: number; cursor?: string; topic_id?: string },
): Promise<McpTextResult> {
  if (!(await v2State()).readable.has(sp.id)) return notConnected(sp);
  if (
    args.topic_id &&
    !SECTIONS.includes(args.topic_id as (typeof SECTIONS)[number])
  )
    return errorResult(
      "In a space on the V2 record, topic_id is a section: decisions, conventions, preferences or open_question.",
    );
  const page = await getClient().v2.memories.list(sp.id, {
    state: ["kept", "stale", "conflict"],
    ...(args.topic_id ? { section: args.topic_id as V2.Section } : {}),
    cursor: args.cursor,
    limit: Math.min(args.limit ?? 20, 50),
  });
  const kept = page.items.filter((m) => m.lifecycle === "kept");
  const memories = kept.map((m) => item(sp, m));
  const structured = {
    memories,
    ...(page.next_cursor ? { next_cursor: page.next_cursor } : {}),
    ...(page.has_more ? { has_more: true } : {}),
  };
  if (!kept.length)
    return textResult(`No kept memories in ${sp.name} yet.`, structured);
  let text = kept
    .map((m) => `- ${m.ref} [${m.section}/${m.state}] ${m.statement}`)
    .join("\n");
  text += `\n\nShowing ${kept.length} from ${sp.name}.`;
  if (page.has_more)
    text += ` More available — pass cursor: "${page.next_cursor}" for next page.`;
  return textResult(text, structured);
}

/** memax_topics on a space on V2: its sections. */
export async function v2Topics(
  sp: V2.Space,
  topicId: string | undefined,
): Promise<McpTextResult> {
  if (topicId) return v2List(sp, { topic_id: topicId, limit: 20 });
  if (!(await v2State()).readable.has(sp.id)) return notConnected(sp);
  const kept = await keptIn(sp);
  const lines = [`## Sections of ${sp.name}`, ""];
  for (const section of SECTIONS) {
    const n = kept.filter((m) => m.section === section).length;
    lines.push(`- **${SECTION_LABELS[section]}** (${n} kept) [id: ${section}]`);
  }
  return textResult(lines.join("\n"));
}

/** The lines memax_hubs adds for the spaces on V2 this agent reads. */
export async function v2HubLines(
  hubs: {
    hub: { id: string; name: string; hub_type: string; slug: string };
    role: string;
  }[],
  activeHubID: string | undefined,
): Promise<string[]> {
  const state = await v2State();
  const lines: string[] = [];
  for (const { hub, role } of hubs) {
    if (!state.readable.has(hub.id)) continue;
    const kept = (await keptIn(state.spaces.get(hub.id)!)).length;
    const ref = hub.hub_type === "personal" ? "personal" : hub.slug;
    const active = hub.id === activeHubID ? " active" : "";
    const level = state.autonomy.get(hub.id);
    lines.push(
      `- **${hub.name}** (${hub.hub_type}, ${role}${active}) ref: ${ref} id: ${hub.id} memories: ${kept} kept · on V2${level ? ` · this agent: ${level}` : ""}`,
    );
  }
  return lines;
}

/** Test hook: forget the cached V2 state and this session's proposals. */
export function resetV2StateForTest(): void {
  cached = undefined;
  sessionProposals.clear();
}
