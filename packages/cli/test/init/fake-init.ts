// What memax init needs from /v2, on top of the daemon tests' fake server
// (test/daemon/fake-v2.ts): creating and switching spaces, imports (with
// the server's folds, its "already in the record" check and a scripted
// conflict check), settling, bulk keep, memories, the Brief, targets and
// agent connections. The rules follow internal/ledger closely enough for
// the CLI's flow: every import is a proposal, and the bulk set is what
// the server would mark `bulk`.
import { randomUUID } from "node:crypto";
import type { V2 } from "memax-sdk";
import type { FakeV2, RouteResult } from "../daemon/fake-v2.js";

const now = () => new Date().toISOString();

/** The server's normalised statement (textsig.Normalize, near enough). */
export function norm(s: string): string {
  return s
    .toLowerCase()
    .replace(/\s+/g, " ")
    .trim()
    .replace(/[.!;: ]+$/, "");
}

export interface InitState {
  memories: Map<string, V2.Memory>;
  imports: Map<string, { view: V2.ImportView }>;
  briefs: Map<string, V2.Brief>;
  /** Groups the conflict check finds, by statements' words; default none. */
  conflicts: (
    statements: V2.Memory[],
  ) => Array<{ subject: string; members: string[]; suggestion?: string }>;
  /** Polls before the judge is done with a new import. */
  judgeAfter: number;
  connected: Array<{ agent: string; space: string; autonomy: string }>;
  settled: Array<{ n: number; choice: string }>;
  kept: string[];
  /** V1 memories a space still on V1 holds, by slug (the switch preview). */
  v1Notes: Record<string, number>;
}

function fail(
  status: number,
  error: string,
  message: string,
  details?: unknown,
): RouteResult {
  return { status, error, message, details };
}

export function installInit(fake: FakeV2): InitState {
  const st: InitState = {
    memories: new Map(),
    imports: new Map(),
    briefs: new Map(),
    conflicts: () => [],
    judgeAfter: 1,
    connected: [],
    settled: [],
    kept: [],
    v1Notes: {},
  };
  let seq = 0;
  const space = (key: string) =>
    fake.spaces.find((s) => s.id === key || s.slug === key);
  const polls = new Map<string, number>();

  const sourcesOf = (it: V2.ImportItemInput): V2.Source[] =>
    (it.sources ?? []).map((s) => ({
      id: randomUUID(),
      kind: s.kind,
      ref: s.ref,
      locator: (s.locator as Record<string, unknown>) ?? {},
      external: s.kind === "url" || !!s.external || s.trust === "external",
      trust: s.kind === "url" ? "external" : (s.trust ?? "repository"),
      created_at: now(),
    }));
  const memory = (sp: V2.Space, it: V2.ImportItemInput): V2.Memory => {
    const trusts = (it.sources ?? []).map((s) =>
      s.kind === "url" || s.external ? "external" : (s.trust ?? "repository"),
    );
    const order: V2.Trust[] = [
      "external",
      "repository",
      "agent_own_work",
      "person",
    ];
    const trust =
      trusts.sort((a, b) => order.indexOf(a) - order.indexOf(b))[0] ??
      "repository";
    const m: V2.Memory = {
      id: randomUUID(),
      ref: `M-${String(++seq).padStart(4, "0")}`,
      space_id: sp.id,
      tenant_id: sp.tenant_id,
      statement: it.statement,
      section: it.section,
      kind: it.kind ?? "fact",
      state: "proposed",
      lifecycle: "proposed",
      flags: [],
      trust,
      version: 1,
      conditions: [],
      scope: (it.scope as Record<string, unknown>) ?? {},
      created_receipt_id: randomUUID(),
      last_receipt_id: randomUUID(),
      created_at: now(),
      updated_at: now(),
      sources: sourcesOf(it),
      judge: { state: "working", version: 1 },
    };
    st.memories.set(m.id, m);
    return m;
  };

  /** The server's bulk rule (imports_read.go). */
  const refresh = (v: V2.ImportView) => {
    const checking = v.import.check.state === "pending";
    const open = new Map<string, number>();
    for (const c of v.conflicts)
      if (c.state === "open") for (const m of c.members) open.set(m.id, c.n);
    for (const im of v.memories) {
      const m = (im.memory = st.memories.get(im.memory.id)!);
      const hidden = v.items
        .filter((i) => i.memory?.id === m.id)
        .reduce((n, i) => n + i.hidden_characters, 0);
      im.conflict = open.get(m.id);
      im.held =
        m.lifecycle !== "proposed"
          ? "decided"
          : m.flags.includes("conflict") || open.has(m.id)
            ? "conflict"
            : m.trust === "external"
              ? "quarantined"
              : checking || m.judge?.state === "working"
                ? "checking"
                : hidden > 0
                  ? "hidden_characters"
                  : undefined;
      if (!im.held) delete im.held;
      if (!im.conflict) delete im.conflict;
      im.bulk = !im.held;
    }
    const proposals = v.memories.filter((m) => m.outcome === "proposed");
    v.progress = {
      proposals: proposals.length,
      working: proposals.filter(
        (m) =>
          m.memory.lifecycle === "proposed" &&
          m.memory.judge?.state === "working",
      ).length,
      judged: proposals.filter((m) => m.memory.judge?.state !== "working")
        .length,
      failed: 0,
      ready:
        !checking &&
        proposals.every((m) => m.memory.judge?.state !== "working"),
    };
  };

  /** The judge and the conflict check, after `judgeAfter` polls. */
  const judge = (v: V2.ImportView) => {
    const n = (polls.get(v.import.id) ?? 0) + 1;
    polls.set(v.import.id, n);
    if (n < st.judgeAfter || v.import.check.state !== "pending") return;
    const pending = v.memories
      .filter((m) => m.outcome === "proposed")
      .map((m) => st.memories.get(m.memory.id)!);
    for (const m of pending)
      m.judge = {
        state: "judged",
        version: m.version,
        verdict: "none",
        outcome: "none",
        stage: "none",
      };
    const groups =
      pending.length >= 2
        ? st.conflicts(pending.filter((m) => m.lifecycle === "proposed"))
        : [];
    groups.forEach((g, i) => {
      const members = g.members
        .map((ref) => pending.find((m) => m.ref === ref)!)
        .filter(Boolean);
      for (const m of members) {
        m.flags = ["conflict"];
        m.state = "conflict";
      }
      v.conflicts.push({
        id: randomUUID(),
        n: i + 1,
        subject: g.subject,
        ...(g.suggestion ? { suggestion: g.suggestion } : {}),
        members: members.map((m) => ({ id: m.id, ref: m.ref })),
        state: "open",
        created_receipt_id: randomUUID(),
        last_receipt_id: randomUUID(),
        created_at: now(),
      });
    });
    v.import.counts.conflicts = v.conflicts.length;
    v.import.check = {
      state: pending.length >= 2 ? "checked" : "skipped",
      checked_at: now(),
    };
  };

  // The Switch to V2 as the server answers it: a space's V1 memories are
  // st.v1Notes[slug] (none by default).
  const switchView = (sp: V2.Space): V2.SpaceSwitch => {
    const total = st.v1Notes[sp.slug] ?? 0;
    return {
      space: sp,
      state: sp.v2_enabled_at ? "switched" : "v1",
      step: sp.v2_enabled_at ? "done" : "space",
      preview: {
        kind: sp.kind,
        kinds: [sp.kind],
        members: [],
        notes: {
          total,
          person: total,
          agent: 0,
          candidates: total,
          fold: 0,
          kept: 0,
          long: 0,
          secret: 0,
          archived: 0,
          format: 0,
          external: 0,
          seeds: 0,
        },
        personas: 0,
        configs: [],
        targets: [],
        agents: [],
        gates: 0,
        dream_runs: 0,
        empty: total === 0,
      },
      progress: {
        notes: 0,
        personas: 0,
        configs: 0,
        targets: [],
        proposed: 0,
        folded: 0,
        existing: 0,
        refused: 0,
        imports: [],
        connected: 0,
        already_connected: 0,
        notified: 0,
        gates_moved: 0,
        gates_left: 0,
      },
      attempts: 0,
      background: false,
    };
  };

  const keep = (m: V2.Memory): string | null => {
    if (m.lifecycle !== "proposed") return "invalid_transition";
    if (m.flags.includes("conflict")) return "in_conflict";
    m.lifecycle = "kept";
    m.state = "kept";
    st.kept.push(m.ref);
    return null;
  };

  fake.extra.push((method, path, url, body): RouteResult | null => {
    let m: RegExpMatchArray | null;
    if (method === "POST" && path === "/v2/spaces") {
      const b = body as V2.CreateSpaceInput;
      let slug = b.name.toLowerCase().replace(/[^a-z0-9]+/g, "-");
      if (fake.spaces.some((s) => s.slug === slug)) slug += "-2";
      const sp = fake.addSpace(slug, b.name);
      sp.repository = b.repository;
      sp.v2_enabled_at = now();
      return { status: 201, data: sp };
    }
    if (
      method === "GET" &&
      (m = path.match(/^\/v2\/spaces\/([^/]+)\/switch$/))
    ) {
      const sp = space(m[1]);
      if (!sp) return null;
      return { status: 200, data: switchView(sp) };
    }
    if (
      method === "POST" &&
      (m = path.match(/^\/v2\/spaces\/([^/]+):switch$/))
    ) {
      const sp = space(m[1]);
      if (!sp) return null;
      sp.v2_enabled_at ??= now();
      return { status: 200, data: switchView(sp) };
    }
    if (
      (m = path.match(
        /^\/v2\/spaces\/([^/:]+)\/imports(?:\/([^/]+)(?:\/conflicts\/(\d+):settle)?)?$/,
      ))
    ) {
      const sp = space(m[1]);
      if (!sp) return null;
      if (method === "POST" && !m[2])
        return createImport(sp, body as V2.ImportInput);
      if (method === "GET" && !m[2]) {
        const items = [...st.imports.values()]
          .filter((i) => i.view.import.space_id === sp.id)
          .map((i) => i.view.import)
          .reverse();
        return { status: 200, data: { items, has_more: false } };
      }
      const imp = st.imports.get(m[2]);
      if (!imp || imp.view.import.space_id !== sp.id) return null;
      if (method === "GET" && !m[3]) {
        judge(imp.view);
        refresh(imp.view);
        return { status: 200, data: imp.view };
      }
      if (method === "POST" && m[3])
        return settle(
          imp.view,
          Number(m[3]),
          body as V2.SettleImportConflictInput,
        );
      return null;
    }
    if (
      method === "POST" &&
      (m = path.match(/^\/v2\/spaces\/([^/]+)\/memories:(keep|reject)$/))
    ) {
      const b = body as V2.BulkReviewInput;
      const items = b.items.map((it) => {
        const mem = [...st.memories.values()].find(
          (x) => x.id === it.memory || x.ref === it.memory,
        );
        if (!mem)
          return {
            memory: it.memory,
            outcome: "failed",
            error: { code: "not_found", message: "No such memory." },
          };
        const err =
          m![2] === "keep"
            ? keep(mem)
            : mem.lifecycle === "proposed"
              ? ((mem.lifecycle = "rejected"), null)
              : "invalid_transition";
        return err
          ? {
              memory: it.memory,
              ref: mem.ref,
              outcome: "failed",
              error: { code: err, message: err },
            }
          : {
              memory: it.memory,
              ref: mem.ref,
              outcome: "applied",
              state: mem.lifecycle,
              version: mem.version,
            };
      });
      return {
        status: 200,
        data: {
          items,
          applied: items.filter((i) => i.outcome === "applied").length,
          refused: 0,
          failed: items.filter((i) => i.outcome === "failed").length,
        },
      };
    }
    if (
      method === "GET" &&
      (m = path.match(/^\/v2\/spaces\/([^/]+)\/memories$/)) &&
      url.searchParams.get("state") === "kept"
    ) {
      const sp = space(m[1]);
      if (!sp) return null;
      const items = [...st.memories.values()]
        .filter((x) => x.space_id === sp.id && x.lifecycle === "kept")
        .reverse();
      return { status: 200, data: { items, has_more: false } };
    }
    if ((m = path.match(/^\/v2\/spaces\/([^/]+)\/brief$/))) {
      const sp = space(m[1]);
      if (!sp) return null;
      if (method === "GET") {
        const b = st.briefs.get(sp.id);
        return b
          ? { status: 200, data: b }
          : fail(404, "not_found", "No Brief yet.");
      }
      const input = body as V2.ReviseBriefInput;
      const brief: V2.Brief = {
        id: randomUUID(),
        version_id: randomUUID(),
        ref: "B-0001",
        version: 1,
        space_id: sp.id,
        tenant_id: sp.tenant_id,
        title: input.title,
        sections: input.sections.map((s) => ({
          key: s.key,
          heading: s.heading,
          items: s.items ?? [],
        })),
        facts: input.sections.reduce((n, s) => n + (s.items?.length ?? 0), 0),
        current: true,
        receipt_id: randomUUID(),
        created_at: now(),
      };
      st.briefs.set(sp.id, brief);
      return {
        status: 201,
        data: {
          outcome: "applied",
          policy: { effect: "apply" },
          brief,
          receipts: [{}],
        },
      };
    }
    if (
      method === "POST" &&
      (m = path.match(/^\/v2\/spaces\/([^/]+)\/targets$/))
    ) {
      const sp = space(m[1]);
      if (!sp) return null;
      const input = body as V2.CreateTargetInput;
      const t = fake.addTarget(sp, input.kind, {
        user_owned: !!input.settings?.user_owned,
      });
      return {
        status: 201,
        data: {
          outcome: "applied",
          policy: { effect: "apply" },
          target: t,
          receipts: [],
        },
      };
    }
    if (
      method === "PATCH" &&
      (m = path.match(/^\/v2\/agents\/([^/]+)\/spaces\/([^/]+)$/))
    ) {
      const a = fake.agents.find((x) => x.id === m![1]);
      const sp = space(m[2]);
      if (!a || !sp) return null;
      const level = (body as V2.AutonomyInput).autonomy;
      a.spaces.push({
        space_id: sp.id,
        slug: sp.slug,
        name: sp.name,
        kind: sp.kind,
        autonomy: level,
        reads_7d: 0,
        writes_7d: 0,
        updated_at: now(),
      });
      st.connected.push({ agent: a.agent, space: sp.slug, autonomy: level });
      return {
        status: 200,
        data: {
          outcome: "applied",
          policy: { effect: "apply" },
          connection: a,
          receipts: [],
        },
      };
    }
    if (method === "GET" && path === "/v2/agents")
      return { status: 200, data: { items: fake.agents } };
    return null;
  });

  function createImport(sp: V2.Space, input: V2.ImportInput): RouteResult {
    const id = randomUUID();
    const view: V2.ImportView = {
      import: {
        id,
        space_id: sp.id,
        tenant_id: sp.tenant_id,
        actor_kind: "person",
        files: input.files ?? [],
        skipped: input.skipped ?? [],
        counts: {
          items: input.items.length,
          proposed: 0,
          folded: 0,
          existing: 0,
          refused: 0,
          conflicts: 0,
        },
        check: { state: "pending" },
        origin: "init",
        created_at: now(),
      },
      items: [],
      memories: [],
      conflicts: [],
      progress: {
        proposals: 0,
        working: 0,
        judged: 0,
        failed: 0,
        ready: false,
      },
    };
    const results: V2.ImportItemResult[] = [];
    const leads = new Map<string, { key: string; mem: V2.Memory }>();
    input.items.forEach((it, position) => {
      const k = norm(it.statement);
      const lead = leads.get(k);
      const existing = [...st.memories.values()].find(
        (x) =>
          x.space_id === sp.id &&
          x.lifecycle !== "forgotten" &&
          norm(x.statement) === k,
      );
      const add = (
        outcome: V2.ImportOutcome,
        mem: V2.Memory,
        folded?: string,
      ) => {
        results.push({
          key: it.key,
          position,
          ref: it.ref ?? it.sources?.[0]?.ref ?? "",
          outcome,
          memory: { id: mem.id, ref: mem.ref },
          ...(outcome === "existing" ? { lifecycle: mem.lifecycle } : {}),
          ...(folded ? { folded_into: folded } : {}),
        });
        view.items.push({
          position,
          key: it.key,
          ref: it.ref ?? "",
          location: it.location,
          outcome,
          memory: { id: mem.id, ref: mem.ref },
          hidden_characters: it.hidden_characters ?? 0,
        });
        if (!view.memories.some((x) => x.memory.id === mem.id))
          view.memories.push({
            memory: mem,
            outcome: outcome === "existing" ? "existing" : "proposed",
            items: 0,
            bulk: false,
          });
        view.memories.find((x) => x.memory.id === mem.id)!.items++;
        view.import.counts[outcome]++;
      };
      if (lead) {
        lead.mem.sources?.push(...sourcesOf(it));
        add("folded", lead.mem, lead.key);
      } else if (existing) add("existing", existing);
      else {
        const mem = memory(sp, it);
        leads.set(k, { key: it.key, mem });
        add("proposed", mem);
      }
    });
    view.import.uploaded_at = now();
    st.imports.set(id, { view });
    return { status: 201, data: { import: view.import, items: results } };
  }

  function settle(
    v: V2.ImportView,
    n: number,
    input: V2.SettleImportConflictInput,
  ): RouteResult {
    const c = v.conflicts.find((x) => x.n === n);
    if (!c) return fail(404, "not_found", "No such conflict.");
    if (c.state !== "open")
      return fail(409, "invalid_transition", "Already settled.");
    const members = c.members.map((p) => st.memories.get(p.id)!);
    for (const m of members) {
      m.flags = [];
      m.state = m.lifecycle;
    }
    const keepIt = (m: V2.Memory) => {
      m.lifecycle = "kept";
      m.state = "kept";
    };
    if (input.choice === "keep_one") {
      for (const m of members) {
        if (m.ref === input.keep) keepIt(m);
        else m.lifecycle = m.state = "rejected";
      }
    } else if (input.choice === "keep_all") members.forEach(keepIt);
    else if (input.choice === "leave_open")
      members.forEach((m) => (keepIt(m), (m.section = "open_question")));
    c.state = "settled";
    c.choice = input.choice;
    c.settled_at = now();
    st.settled.push({ n, choice: input.choice });
    return {
      status: 200,
      data: {
        outcome: "applied",
        policy: { effect: "apply" },
        conflict: c,
        memories: members,
        receipts: [{}],
      },
    };
  }

  return st;
}
