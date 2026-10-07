import type { Memax, V2 } from "memax-sdk";
import { registryKey } from "./agents-sdk";
import {
  fileOfRef,
  type BulkOutcome,
  type ImportConflictView,
  type ImportFileView,
  type ImportMemoryView,
  type ImportSummary,
  type ImportView,
  type ImportsSource,
  type SettleChoice,
} from "./imports";

/**
 * Imports over memax.v2 (imports.ts): the list and one import in full,
 * settling a disagreement, and keeping or rejecting in bulk. Mapping
 * only: every rule (what can be kept in bulk, how a settlement keeps and
 * rejects) is the server's.
 */

type ImportsClient = Pick<Memax, "v2">;

function agentOf(raw: string | undefined): string | null {
  return raw ? registryKey(raw) : null;
}

export function toImportFile(f: V2.ImportFile): ImportFileView {
  return {
    path: f.path,
    kind: f.kind,
    agent: agentOf(f.agent),
    location: f.location,
    statements: f.statements,
    skipped: f.skipped,
    hiddenCharacters: f.hidden_characters,
  };
}

export function toImportSummary(imp: V2.Import): ImportSummary {
  return {
    id: imp.id,
    spaceId: imp.space_id,
    client: imp.client ?? null,
    createdAt: imp.created_at,
    files: (imp.files ?? []).map(toImportFile),
    skipped: (imp.skipped ?? []).map((s) => ({
      ref: s.ref,
      reason: s.reason,
      detail: s.detail ?? null,
    })),
    counts: { ...imp.counts },
    check: imp.check.state,
    origin: imp.origin,
  };
}

/** The host an outside source was read on ("modelcontextprotocol.io"). */
function externalOf(memory: V2.Memory): string | null {
  const src = memory.sources?.find((s) => s.external);
  if (!src) return null;
  try {
    return src.uri ? new URL(src.uri).host : src.ref;
  } catch {
    return src.ref;
  }
}

export function toImportView(v: V2.ImportView): ImportView {
  const summary = toImportSummary(v.import);
  const agentOfFile = new Map(summary.files.map((f) => [f.path, f.agent]));
  // The statements each memory stands for, in the import's order.
  const refsOf = new Map<string, string[]>();
  for (const item of v.items) {
    if (!item.memory) continue;
    const list = refsOf.get(item.memory.id) ?? [];
    if (!list.includes(item.ref)) list.push(item.ref);
    refsOf.set(item.memory.id, list);
  }
  const byId = new Map<string, V2.Memory>();
  const memories: ImportMemoryView[] = v.memories.map((m) => {
    byId.set(m.memory.id, m.memory);
    const refs = refsOf.get(m.memory.id) ?? [];
    const files = [...new Set(refs.map(fileOfRef))];
    return {
      id: m.memory.id,
      ref: m.memory.ref,
      version: m.memory.version,
      statement: m.memory.statement,
      state: m.memory.state,
      outcome: m.outcome === "existing" ? "existing" : "proposed",
      items: m.items,
      refs,
      files,
      agent: files.map((f) => agentOfFile.get(f) ?? null).find(Boolean) ?? null,
      bulk: m.bulk,
      held: m.held ?? null,
      conflict: m.conflict ?? null,
      external: externalOf(m.memory),
      trust: m.memory.trust ?? null,
    };
  });
  return {
    summary,
    memories,
    conflicts: v.conflicts.map((c) =>
      toImportConflict(c, byId, refsOf, agentOfFile),
    ),
    progress: { ...v.progress },
  };
}

/** A disagreement, with its members' words and where each was said. */
export function toImportConflict(
  c: V2.ImportConflict,
  byId: ReadonlyMap<string, V2.Memory>,
  refsOf: ReadonlyMap<string, string[]> = new Map(),
  agentOfFile: ReadonlyMap<string, string | null> = new Map(),
): ImportConflictView {
  return {
    id: c.id,
    n: c.n,
    subject: c.subject ?? null,
    rationale: c.rationale ?? null,
    suggestion: c.suggestion ?? null,
    members: c.members.map((p) => {
      const memory = byId.get(p.id);
      const refs = refsOf.get(p.id) ?? [];
      return {
        id: p.id,
        ref: p.ref,
        statement: memory?.statement ?? "",
        refs,
        agent:
          refs
            .map((r) => agentOfFile.get(fileOfRef(r)) ?? null)
            .find(Boolean) ?? null,
      };
    }),
    state: c.state,
    choice: c.choice ?? null,
    chosen: c.chosen?.ref ?? null,
  };
}

function toBulkOutcome(r: V2.BulkReviewResult): BulkOutcome {
  const out: BulkOutcome = { applied: [], refused: [], failed: [] };
  for (const item of r.items) {
    const ref = item.ref ?? item.memory;
    if (item.outcome === "applied") out.applied.push(ref);
    else if (item.outcome === "refused")
      out.refused.push({ ref, code: item.policy?.code ?? null });
    else out.failed.push({ ref, code: item.error?.code ?? null });
  }
  return out;
}

function settleInput(c: SettleChoice): V2.SettleImportConflictInput {
  switch (c.choice) {
    case "keep_one":
      return { choice: "keep_one", keep: c.keep };
    case "keep_suggestion":
      return c.statement
        ? { choice: "keep_suggestion", statement: c.statement }
        : { choice: "keep_suggestion" };
    default:
      return { choice: c.choice };
  }
}

export function createSdkImports(client: ImportsClient): ImportsSource {
  return {
    async list({ space, limit = 10, signal }) {
      const page = await client.v2.imports.list(space.slug, { limit, signal });
      return page.items.map(toImportSummary);
    },
    async get({ space, id, signal }) {
      try {
        return toImportView(
          await client.v2.imports.get(space.slug, id, { signal }),
        );
      } catch (err) {
        if ((err as { status?: number }).status === 404) return null;
        throw err;
      }
    },
    async settle({ space, importId, n, choice, idempotencyKey }) {
      const res = await client.v2.imports.settle(
        space.slug,
        importId,
        n,
        settleInput(choice),
        { idempotencyKey },
      );
      return toImportConflict(
        res.conflict,
        new Map(res.memories.map((m) => [m.id, m])),
      );
    },
    async keep({ space, items, idempotencyKey }) {
      return toBulkOutcome(
        await client.v2.memories.keepMany(
          space.slug,
          { items: items.map((i) => ({ memory: i.ref, version: i.version })) },
          { idempotencyKey },
        ),
      );
    },
    async reject({ space, items, idempotencyKey }) {
      return toBulkOutcome(
        await client.v2.memories.rejectMany(
          space.slug,
          { items: items.map((i) => ({ memory: i.ref, version: i.version })) },
          { idempotencyKey },
        ),
      );
    },
  };
}
