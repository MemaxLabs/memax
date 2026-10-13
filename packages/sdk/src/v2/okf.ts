// A space's export (memax.export.v1, read by parseExport) as an Open
// Knowledge Format v0.2 bundle (github.com/GoogleCloudPlatform/
// open-knowledge-format, SPEC.md): one concept per memory, with OKF's own
// fields (`type`, `title`, `status`, `stale_after`, `generated`,
// `verified`, `sources`) and the rest of the record under one extension
// key, `memax`, which OKF consumers preserve; a tombstone per forgotten
// memory, without words; the Brief as the bundle-root `index.md`; and
// `log.md`, what happened to each memory, newest first, from the receipts
// (never their reasons). Frontmatter values are JSON, which every YAML
// parser reads, so the bundle is the same bytes for the same export.
//
// A projection, not a format: memax.export.v1 stays the verifiable copy
// (verifyExport), and this one is for any OKF reader.
import type {
  ExportedBrief,
  ExportedMemory,
  ExportedTombstone,
  ParsedExport,
} from "./export.js";

export const OKF_VERSION = "0.2";

/** OKF's lifecycle for a memory; null leaves it out (a rejected proposal). */
function statusOf(m: ExportedMemory): "draft" | "stable" | "deprecated" | null {
  switch (m.lifecycle) {
    case "proposed":
      return "draft";
    case "kept":
      return m.decision?.status === "superseded" ? "deprecated" : "stable";
    case "merged":
    case "faded":
      return "deprecated";
    default:
      return null;
  }
}

interface Receiptish {
  id: string;
  action: string;
  actor_kind: string;
  actor_id: string | null;
  agent: string | null;
  occurred_at: string;
  object_kind: string;
  object_ref: string | null;
}

/** An actor in OKF's forms: `human:<id>`, `<agent>`, `process:<id>`. */
function actorOf(r: Receiptish): string {
  switch (r.actor_kind) {
    case "person":
      return `human:${r.actor_id ?? "unknown"}`;
    case "agent":
      return r.agent || `agent:${r.actor_id ?? "unknown"}`;
    default:
      return `process:${r.actor_kind}`;
  }
}

/** The statement's first clause, at most 80 characters, on a word boundary. */
export function titleOf(statement: string): string {
  const s = statement.trim().replace(/\s+/g, " ");
  const end = s.search(/[.;:!?](\s|$)/);
  const clause = end > 0 ? s.slice(0, end) : s;
  if (clause.length <= 80) return clause;
  const cut = clause.slice(0, 80);
  const space = cut.lastIndexOf(" ");
  return `${(space > 40 ? cut.slice(0, space) : cut).trimEnd()}…`;
}

/**
 * JSON with U+2028 and U+2029 escaped, as Go's encoder (and so the
 * export) writes them: raw, some readers take them for line breaks.
 */
function json(v: unknown): string {
  return JSON.stringify(v)
    .replace(/\u2028/g, "\\u2028")
    .replace(/\u2029/g, "\\u2029");
}

/** `key: <json>` lines: YAML that is also JSON, in a fixed key order. */
function frontmatter(fields: Array<[string, unknown]>): string {
  const lines = fields
    .filter(([, v]) => v !== undefined)
    .map(([k, v]) => `${k}: ${json(v)}`);
  return `---\n${lines.join("\n")}\n---\n`;
}

/** A source as OKF lists it: `resource` (a URI, or `path#Lline`) and its ref. */
function sourceOf(s: Record<string, unknown>): Record<string, unknown> {
  const locator = (s.locator ?? {}) as { path?: string; line?: number };
  const resource =
    (typeof s.uri === "string" && s.uri) ||
    (locator.path
      ? `${locator.path}${locator.line ? `#L${locator.line}` : ""}`
      : (s.ref as string | undefined)) ||
    "";
  return {
    id: s.id,
    resource,
    ...(typeof s.ref === "string" ? { title: s.ref } : {}),
  };
}

function memoryFile(
  m: ExportedMemory,
  byId: Map<string, Receiptish>,
): string | null {
  const status = statusOf(m);
  if (!status) return null;
  const mine = (m.receipts ?? [])
    .map((stamp) => byId.get(stamp.id))
    .filter((r): r is Receiptish => !!r);
  const created = byId.get(m.created_receipt);
  // Kept, verified or edited by a person: human-reviewed; kept by an agent
  // with Write: machine-confirmed (OKF derives the tier from these).
  const verified = mine
    .filter(
      (r) =>
        r.action === "kept" ||
        r.action === "verified" ||
        (r.action === "edited" && r.actor_kind === "person"),
    )
    .map((r) => ({ by: actorOf(r), at: r.occurred_at }));
  const statement = m.body.trim();
  const { path: _path, body: _body, format: _format, ...rest } = m;
  void _path;
  void _body;
  void _format;
  const sources = Array.isArray(m.sources)
    ? (m.sources as Record<string, unknown>[]).map(sourceOf)
    : [];
  return (
    frontmatter([
      ["type", m.kind],
      ["title", titleOf(statement)],
      ["status", status],
      ["stale_after", m.stale_after ?? undefined],
      [
        "generated",
        created
          ? { by: actorOf(created), at: created.occurred_at }
          : { by: "process:memax", at: m.created_at },
      ],
      ["verified", verified.length > 0 ? verified : undefined],
      ["sources", sources.length > 0 ? sources : undefined],
      ["memax", { format: "memax.memory.v1", ...rest }],
    ]) + `\n${statement}\n`
  );
}

function tombstoneFile(t: ExportedTombstone): string {
  const {
    path: _path,
    body: _body,
    format: _format,
    ...rest
  } = t as ExportedTombstone & { body?: string };
  void _path;
  void _body;
  void _format;
  return (
    frontmatter([
      ["type", "tombstone"],
      ["title", `${t.ref}, forgotten`],
      ["status", "deprecated"],
      ["memax", { format: "memax.tombstone.v1", ...rest }],
    ]) + "\nForgotten. Its words are gone everywhere Memax could reach.\n"
  );
}

/** The bundle-root index: OKF's version, then the current Brief's sections. */
function indexFile(
  brief: ExportedBrief | undefined,
  slug: string,
  memories: ExportedMemory[],
): string {
  const byRef = new Map(memories.map((m) => [m.ref, m]));
  const link = (ref: string) => {
    const m = byRef.get(ref);
    return m ? `- [${titleOf(m.body)}](memories/${ref}.md)` : null;
  };
  const out: string[] = [`# ${brief?.title ?? slug}`, ""];
  if (brief?.summary) out.push(brief.summary, "");
  const placed = new Set<string>();
  for (const s of brief?.sections ?? []) {
    const lines: string[] = [];
    for (const it of s.items) {
      if (it.forgotten) continue;
      if (it.ref) {
        const l = link(it.ref);
        if (l) {
          lines.push(l);
          placed.add(it.ref);
        }
      } else if (it.text) lines.push(`- ${it.text}`);
    }
    if (lines.length > 0) out.push(`## ${s.heading}`, "", ...lines, "");
  }
  const rest = (lifecycle: string) =>
    memories
      .filter((m) => m.lifecycle === lifecycle && !placed.has(m.ref))
      .map((m) => link(m.ref)!)
      .filter(Boolean);
  const kept = rest("kept");
  if (kept.length > 0) out.push("## Not in the Brief", "", ...kept, "");
  const waiting = rest("proposed");
  if (waiting.length > 0) out.push("## Waiting for review", "", ...waiting, "");
  return `${frontmatter([["okf_version", OKF_VERSION]])}\n${out.join("\n").trimEnd()}\n`;
}

/** What happened to the space's memories, by day, newest first. No words. */
function logFile(receipts: Receiptish[]): string {
  const days = new Map<string, string[]>();
  for (const r of [...receipts].reverse()) {
    if (r.object_kind !== "memory" && r.object_kind !== "brief") continue;
    const day = r.occurred_at.slice(0, 10);
    const line = `- ${r.action} ${r.object_ref ?? r.object_kind} · ${actorOf(r)}`;
    days.set(day, [...(days.get(day) ?? []), line]);
  }
  const out: string[] = ["# Log", ""];
  for (const [day, lines] of [...days].sort((a, b) => b[0].localeCompare(a[0])))
    out.push(`## ${day}`, "", ...lines, "");
  return `${out.join("\n").trimEnd()}\n`;
}

/**
 * The export as an OKF v0.2 bundle: file paths (below the space's
 * folder) to their contents, sorted.
 */
export function toOkf(parsed: ParsedExport, slug = ""): Map<string, string> {
  const receipts = parsed.receipts as unknown as Receiptish[];
  const byId = new Map(receipts.map((r) => [r.id, r]));
  const out = new Map<string, string>();
  const current = parsed.briefs.find((b) => b.current) ?? parsed.briefs.at(-1);
  const shown = parsed.memories.filter((m) => statusOf(m) !== null);
  out.set("index.md", indexFile(current, slug, shown));
  out.set("log.md", logFile(receipts));
  for (const m of parsed.memories) {
    const file = memoryFile(m, byId);
    if (file) out.set(`memories/${m.ref}.md`, file);
  }
  for (const t of parsed.tombstones)
    out.set(`tombstones/${t.ref}.md`, tombstoneFile(t));
  return new Map([...out].sort((a, b) => a[0].localeCompare(b[0])));
}
