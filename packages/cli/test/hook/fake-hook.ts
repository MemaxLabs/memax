// What the warm-start cache and the compile-load reports need from /v2,
// on top of the daemon tests' fake server: a space's waiting gates, its
// tombstones, and POST /v2/spaces/{space}/compile-loads, which records
// each load once per Idempotency-Key (the fake replays keys itself).
import { randomUUID } from "node:crypto";
import type { V2 } from "memax-sdk";
import type { FakeV2 } from "../daemon/fake-v2.js";

export interface HookState {
  gates: Map<string, V2.Gate[]>;
  tombstones: Map<string, V2.Tombstone[]>;
  loads: Array<{ space: string; body: V2.CompileLoadInput }>;
  /** Status to answer loads with (201 by default). */
  loadStatus: number;
}

export function installHookRoutes(fake: FakeV2): HookState {
  const st: HookState = {
    gates: new Map(),
    tombstones: new Map(),
    loads: [],
    loadStatus: 201,
  };
  const space = (key: string) =>
    fake.spaces.find((s) => s.id === key || s.slug === key);
  fake.extra.push((method, path, url, body) => {
    let m: RegExpMatchArray | null;
    if (
      method === "GET" &&
      (m = path.match(/^\/v2\/spaces\/([^/]+)\/(gates|tombstones)$/))
    ) {
      const sp = space(m[1]);
      if (!sp) return null;
      if (m[2] === "gates") {
        const status = url.searchParams.getAll("status");
        const items = (st.gates.get(sp.id) ?? []).filter(
          (g) => status.length === 0 || status.includes(g.status),
        );
        return { status: 200, data: { items, has_more: false } };
      }
      return {
        status: 200,
        data: { tombstones: st.tombstones.get(sp.id) ?? [], has_more: false },
      };
    }
    if (
      method === "POST" &&
      (m = path.match(/^\/v2\/spaces\/([^/]+)\/compile-loads$/))
    ) {
      const sp = space(m[1]);
      if (!sp) return null;
      if (st.loadStatus !== 201)
        return {
          status: st.loadStatus,
          error: st.loadStatus === 403 ? "refused" : "unavailable",
          message: "Injected.",
        };
      const input = body as V2.CompileLoadInput;
      st.loads.push({ space: sp.id, body: input });
      return {
        status: 201,
        data: {
          read: {
            id: randomUUID(),
            ref: `R-${String(st.loads.length).padStart(4, "0")}`,
            space_id: sp.id,
            reader_kind: "person",
            person_id: randomUUID(),
            agent: input.agent,
            kind: "compile_load",
            via: "cli",
            compile: input.compile,
            memories: 0,
            memory_refs: [],
            read_at: input.loaded_at ?? new Date().toISOString(),
            recorded_at: new Date().toISOString(),
          },
        },
      };
    }
    return null;
  });
  return st;
}

export function gateFixture(
  space: V2.Space,
  ref: string,
  question: string,
  expires: Date,
): V2.Gate {
  const now = new Date().toISOString();
  return {
    id: randomUUID(),
    ref,
    space_id: space.id,
    tenant_id: space.tenant_id,
    question,
    options: [],
    status: "waiting",
    expires_at: expires.toISOString(),
    asked_by: randomUUID(),
    agent: "codex",
    needs_web: false,
    version: 1,
    created_receipt_id: randomUUID(),
    last_receipt_id: randomUUID(),
    created_at: now,
    updated_at: now,
  } as unknown as V2.Gate;
}

export function tombstoneFixture(
  space: V2.Space,
  ref: string,
  at: Date,
  withRefs: string[] = [],
): V2.Tombstone {
  return {
    id: randomUUID(),
    op_id: randomUUID(),
    ref,
    kind: "memory",
    object_id: randomUUID(),
    space_id: space.id,
    tenant_id: space.tenant_id,
    with: withRefs,
    note: "the person's own words, never cached",
    by: { kind: "person" },
    via: "web",
    receipt_id: randomUUID(),
    forgotten_at: at.toISOString(),
    reads_before: 0,
    gone: {},
    agents: 0,
    status: "done",
    steps: [],
    unreachable: [],
  } as unknown as V2.Tombstone;
}
