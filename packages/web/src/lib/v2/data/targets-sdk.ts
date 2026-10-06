import type { V2 } from "memax-sdk";
import type { V2Client } from "./sdk-records";
import {
  isScoped,
  isShim,
  targetSlug,
  type CompileSummary,
  type CompiledFileView,
  type DriftChangeView,
  type DriftItemView,
  type TargetsSource,
  type TargetView,
} from "./targets";

/**
 * Compile targets through memax.v2.targets: the list (with each one's
 * latest run), the compiled content, the hand edits, Compile now,
 * settings (PATCH with If-Match) and resolving drift. Pure mappers are
 * exported for the tests.
 */

function unique(values: Iterable<string>): string[] {
  return [...new Set(values)];
}

export function compileOf(run: V2.CompileRun): CompileSummary {
  return {
    ref: run.ref,
    status: run.status,
    at: run.compiled_at,
    bytes: run.bytes,
    lines: run.lines,
    refs: run.refs,
    cites: unique(run.files.flatMap((f) => f.cites)),
    dropped: run.dropped_for_budget,
    files: run.files.flatMap((f) => (f.path ? [f.path] : [])),
  };
}

/** One target, given the space's others (for its URL segment and the canonical file it reads). */
export function targetOf(
  target: V2.Target,
  all: readonly V2.Target[],
): TargetView {
  const canonical = all.find((t) => t.kind === "agents_md");
  return {
    id: target.id,
    slug: targetSlug(target, all),
    kind: target.kind,
    label: target.label,
    path: target.path ?? null,
    reads:
      isShim(target.kind) || isScoped(target.kind)
        ? (canonical?.path ?? "AGENTS.md")
        : null,
    syncState: target.sync_state,
    delivery: target.delivery,
    settings: {
      include: target.settings.include,
      stale: target.settings.stale,
      sizeBudget: target.settings.size_budget,
    },
    version: target.version,
    openDrift: target.open_drift,
    lastCompile: target.last_compile ? compileOf(target.last_compile) : null,
  };
}

export function compiledFileOf(output: V2.PreviewOutput): CompiledFileView {
  return {
    path: output.path ?? null,
    label: output.path ?? output.label ?? "",
    content: output.content,
    bytes: output.bytes,
    lines: output.lines,
    refs: output.refs,
    cites: output.cites,
    dropped: output.dropped_for_budget,
  };
}

export function driftChangeOf(change: V2.DriftChange): DriftChangeView {
  switch (change.kind) {
    case "edit":
      return {
        kind: "edit",
        ref: change.ref ?? change.refs?.[0] ?? "",
        oldText: change.old_text ?? "",
        newText: change.new_text ?? "",
        oldLine: change.old_line ?? 0,
        newLine: change.new_line ?? 0,
      };
    case "new":
      return {
        kind: "new",
        text: change.text ?? "",
        line: change.line ?? 0,
        section: change.section ?? null,
      };
    case "remove":
      return {
        kind: "remove",
        ref: change.ref ?? change.refs?.[0] ?? "",
        oldText: change.old_text ?? "",
        oldLine: change.old_line ?? 0,
      };
  }
}

export function driftItemOf(item: V2.DriftItem): DriftItemView {
  const o = item.observation;
  return {
    observationId: o.id,
    path: o.path,
    observedAt: o.observed_at,
    commit: o.commit ?? null,
    compiled: item.compiled,
    compiledAt: item.base_compile?.compiled_at ?? null,
    observed: item.observed,
    changes: o.changeset.changes.map(driftChangeOf),
    hiddenCharacters: o.changeset.drift.hidden_characters,
  };
}

/**
 * The space's targets as views, or null when they don't load: the
 * screens that show them in passing (the status line, Review's card, a
 * memory's Reaches) still show without them.
 */
export async function targetsOrNull(
  client: V2Client,
  slug: string,
  signal?: AbortSignal,
): Promise<TargetView[] | null> {
  try {
    const { items } = await client.v2.targets.list(slug, { signal });
    return items.map((t) => targetOf(t, items));
  } catch {
    return null;
  }
}

/** A target as a command or read returned it, keeping what only the list knows (its URL segment, what it reads). */
function refreshed(target: V2.Target, before: TargetView): TargetView {
  return {
    ...targetOf(target, [target]),
    slug: before.slug,
    reads: before.reads,
  };
}

export function createSdkTargets(client: V2Client): TargetsSource {
  return {
    async list({ space, signal }) {
      const { items } = await client.v2.targets.list(space.slug, { signal });
      return items.map((t) => targetOf(t, items));
    },

    async preview({ target, signal }) {
      const preview = await client.v2.targets.preview(target.id, { signal });
      return {
        target: refreshed(preview.target, target),
        compile: preview.compile
          ? { ref: preview.compile.ref, at: preview.compile.compiled_at }
          : null,
        reads: preview.reads ?? target.reads,
        files: preview.files.map(compiledFileOf),
        copies: preview.copies.map(compiledFileOf),
      };
    },

    async drift({ target, signal }) {
      const drift = await client.v2.targets.drift(target.id, { signal });
      return {
        target: refreshed(drift.target, target),
        items: drift.items.map(driftItemOf),
      };
    },

    async compile({ target, idempotencyKey }) {
      const result = await client.v2.targets.compile(
        target.id,
        {},
        { idempotencyKey },
      );
      return refreshed(result.target, target);
    },

    async configure({ target, change, idempotencyKey }) {
      const body: V2.ConfigureTargetInput = {};
      if (change.settings) {
        const settings: V2.TargetSettingsInput = {};
        if (change.settings.include) settings.include = change.settings.include;
        if (change.settings.stale) settings.stale = change.settings.stale;
        if (change.settings.sizeBudget !== undefined) {
          settings.size_budget = change.settings.sizeBudget;
        }
        body.settings = settings;
      }
      if (change.delivery) body.delivery = change.delivery;
      if (change.enabled !== undefined) body.enabled = change.enabled;
      const result = await client.v2.targets.update(target.id, body, {
        idempotencyKey,
        ifMatch: target.version,
      });
      return refreshed(result.target, target);
    },

    async resolveDrift({ target, mode, idempotencyKey }) {
      const run =
        mode === "pull"
          ? client.v2.targets.pull
          : mode === "overwrite"
            ? client.v2.targets.overwrite
            : client.v2.targets.stop;
      const result = await run.call(
        client.v2.targets,
        target.id,
        {},
        { idempotencyKey },
      );
      const changes = result.observations.flatMap(
        (o) => o.resolution?.changes ?? [],
      );
      return {
        target: refreshed(result.target, target),
        proposals: result.proposals.map((m) => ({
          ref: m.ref,
          statement: m.statement,
        })),
        waiting: changes.filter((c) => c.outcome === "review").length,
        skipped: changes.filter((c) => c.outcome === "skipped").length,
      };
    },
  };
}
