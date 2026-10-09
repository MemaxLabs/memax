import type { V2 } from "memax-sdk";
import type {
  CarryReason,
  ForgetPreview,
  ForgetRequestLine,
  TombstoneStepLine,
  TombstoneView,
  UnreachableLine,
} from "./memories";
import { agentKey } from "./sdk-records";

/**
 * Forget's /v2 shapes as the web reads them (Tombstone.png, the States
 * board's inline confirmation, a memory's waiting requests). Pure, like
 * sdk-record.ts, so the rules are unit-tested.
 */

/** An agent on a tombstone: its registry key, or its name, or null. */
function agentOf(a: V2.TombstoneAgent | undefined): string | null {
  if (!a) return null;
  return agentKey(a.agent) ?? a.display_name ?? a.agent ?? null;
}

/** What a Forget would do, from GET /v2/memories/{ref}/forget-preview. */
export function previewOf(p: V2.ForgetPreview): ForgetPreview {
  const copies = p.files.filter((f) => f.delivery === "copy").length;
  return {
    version: p.version,
    carries: p.carries.map((c) => ({
      ref: c.ref,
      reason: c.reason as CarryReason,
    })),
    files: p.files.length - copies,
    copies,
    agents: p.agents,
    refusal: p.allowed
      ? null
      : {
          code: p.policy?.code ?? null,
          message: p.policy?.message ?? null,
        },
  };
}

/** A memory's waiting requests to forget it, newest first. */
export function forgetRequestsOf(
  requests: readonly V2.ForgetRequestRecord[] | undefined,
): ForgetRequestLine[] {
  return (requests ?? [])
    .filter((r) => r.status === "waiting")
    .sort((a, b) => Date.parse(b.requested_at) - Date.parse(a.requested_at))
    .map((r) => ({
      agent: agentOf(r.agent) ?? "",
      reason: r.reason?.trim() || null,
      at: r.requested_at,
    }));
}

function stepOf(s: V2.TombstoneStep, i: number): TombstoneStepLine {
  return {
    key: `${s.kind}-${i}`,
    kind: s.kind,
    status: s.status,
    reason: s.reason ?? null,
    at: s.at ?? null,
    target: s.target
      ? {
          label: s.target.label,
          kind: s.target.kind,
          delivery: s.target.delivery,
        }
      : null,
    compile: s.compile ?? null,
    agent: s.kind === "agent" ? agentOf(s.agent) : null,
    count: s.count ?? null,
  };
}

function unreachableOf(u: V2.UnreachableCopy): UnreachableLine {
  return {
    kind: u.kind,
    files: u.files ?? [],
    repositories: u.repositories ?? [],
    agents: u.agents ?? [],
    days: u.days ?? null,
    processors: (u.processors ?? []).map((p) => ({
      name: p.name,
      purpose: p.purpose,
      zeroRetention: p.zero_retention,
    })),
    targets: u.targets ?? [],
  };
}

/** A tombstone as Tombstone.png reads it. */
export function tombstoneOf(
  t: V2.Tombstone,
  viewerId: string | undefined,
): TombstoneView {
  return {
    ref: t.ref,
    at: t.forgotten_at,
    by:
      t.by.kind === "person"
        ? {
            kind: "person",
            self: Boolean(viewerId) && t.by.id === viewerId,
          }
        : t.by.kind === "memax"
          ? { kind: "memax" }
          : null,
    requestedBy: agentOf(t.requested_by),
    via: t.via,
    note: t.note?.trim() || null,
    keptAt: t.kept_at ?? null,
    readsBefore: t.reads_before,
    status: t.status,
    with: t.with,
    carried:
      t.carried && t.primary
        ? { reason: t.carried as CarryReason, primary: t.primary }
        : null,
    gone: {
      versions: t.gone.versions,
      sources: t.gone.sources,
      embeddings: t.gone.embeddings,
      files: t.gone.files,
    },
    agents: t.agents,
    steps: t.steps.map(stepOf),
    unreachable: t.unreachable.map(unreachableOf),
  };
}
