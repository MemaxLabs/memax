// A small /v2 server for the daemon's tests: the spaces, targets, runs,
// previews, deliveries and observations of packages/server/openapi/v2.yaml,
// with the ledger's rules for baselines, drift and sync states. Hashes and
// managed blocks come from the real compiler, so the CLI's own copies are
// checked against it on every exchange.
import { randomUUID } from "node:crypto";
import {
  createServer,
  type IncomingMessage,
  type Server,
  type ServerResponse,
} from "node:http";
import type { AddressInfo } from "node:net";
import type { V2 } from "memax-sdk";
import {
  driftHash,
  extractManagedBlock,
  parseBack,
  sha256Hex,
  upsertManagedBlock,
} from "../../../compiler/src/index.js";

export interface Logged {
  method: string;
  path: string;
  headers: Record<string, string | string[] | undefined>;
  body?: unknown;
  status: number;
}

interface Run {
  run: V2.CompileRun;
  content: Map<string, string>;
}

interface Entry {
  target: V2.Target;
  runs: Run[];
  observations: V2.Observation[];
  /** Content of each delivered or accepted file, by path. */
  baseline: Map<string, string>;
}

const now = () => new Date().toISOString();
const DEFAULT_PATH: Partial<Record<V2.TargetKind, string>> = {
  agents_md: "AGENTS.md",
  claude_md: "CLAUDE.md",
  gemini_md: "GEMINI.md",
  cursor_mdc: ".cursor/rules",
};

/** The server's ManifestSHA256. */
export function manifest(entries: Record<string, string>): string {
  const keys = Object.keys(entries).sort();
  if (keys.length === 1) return entries[keys[0]];
  return sha256Hex(keys.map((k) => `${k}\u0000${entries[k]}\n`).join(""));
}

export type RouteResult =
  | { status: number; data: unknown }
  | { status: number; error: string; message: string; details?: unknown };

export type Route = (
  method: string,
  path: string,
  url: URL,
  body: Record<string, unknown> | undefined,
) => RouteResult | null;

export class FakeV2 {
  readonly log: Logged[] = [];
  readonly spaces: V2.Space[] = [];
  readonly targets = new Map<string, Entry>();
  readonly agents: V2.AgentConnection[] = [];
  /**
   * More routes, tried before the built-in ones (memax init's tests add
   * spaces, imports, bulk review and the Brief: test/init/fake-init.ts).
   */
  readonly extra: Route[] = [];
  /**
   * The OAuth server's form endpoints (/oauth/…), answered in OAuth's own
   * JSON before any credential is checked (test/login/fake-device.ts).
   */
  oauth?: (
    path: string,
    form: URLSearchParams,
    headers: IncomingMessage["headers"],
  ) => {
    status: number;
    body: unknown;
    headers?: Record<string, string>;
  } | null;
  reviewTotal = 0;
  keptCount = 0;
  /** Receipts written, as the server writes them (compile runs write none). */
  receipts = 0;
  private server: Server | null = null;
  private seq = 880;
  private keys = new Map<string, { status: number; body: unknown }>();
  private faults: Array<{
    match: RegExp;
    status: number;
    code: string;
    times: number;
  }> = [];
  url = "";

  async start(): Promise<this> {
    this.server = createServer((req, res) => void this.handle(req, res));
    await new Promise<void>((r) => this.server!.listen(0, "127.0.0.1", r));
    this.url = `http://127.0.0.1:${(this.server.address() as AddressInfo).port}`;
    return this;
  }

  async stop(): Promise<void> {
    await new Promise<void>((r) => this.server?.close(() => r()));
  }

  /** Requests whose method and path match. */
  calls(method: string, path: RegExp): Logged[] {
    return this.log.filter((l) => l.method === method && path.test(l.path));
  }

  /** The next `times` requests matching `path` fail with `status`. */
  fail(path: RegExp, status: number, code = "busy", times = 1): void {
    this.faults.push({ match: path, status, code, times });
  }

  addSpace(slug: string, name = slug): V2.Space {
    const space: V2.Space = {
      id: randomUUID(),
      tenant_id: randomUUID(),
      slug,
      name,
      kind: "project",
      role: "owner",
    };
    this.spaces.push(space);
    return space;
  }

  addTarget(
    space: V2.Space,
    kind: V2.TargetKind,
    opts: { path?: string; user_owned?: boolean; delivery?: V2.Delivery } = {},
  ): V2.Target {
    const path = opts.path ?? DEFAULT_PATH[kind];
    const t: V2.Target = {
      id: randomUUID(),
      space_id: space.id,
      tenant_id: space.tenant_id,
      kind,
      path,
      label: kind === "chatgpt" ? "ChatGPT project" : (path ?? kind),
      settings: {
        include: "kept_and_open",
        stale: "mark",
        size_budget: 32768,
        ...(opts.user_owned ? { user_owned: true } : {}),
      },
      delivery: opts.delivery ?? (kind === "chatgpt" ? "copy" : "local"),
      sync_state: "compiling",
      version: 1,
      dirty_gen: 1,
      compiled_gen: 0,
      dirty_at: now(),
      open_drift: 0,
      created_receipt_id: randomUUID(),
      last_receipt_id: randomUUID(),
      created_at: now(),
      updated_at: now(),
    };
    this.targets.set(t.id, {
      target: t,
      runs: [],
      observations: [],
      baseline: new Map(),
    });
    return t;
  }

  entry(id: string): Entry {
    const e = this.targets.get(id);
    if (!e) throw new Error(`no target ${id}`);
    return e;
  }

  /**
   * Records a run. `files` maps paths to content; for a user-owned shim,
   * to the managed block's inner lines, applied to the baseline as the
   * server's compile does.
   */
  compile(id: string, files: Record<string, string | string[]>): V2.CompileRun {
    const e = this.entry(id);
    const t = e.target;
    const content = new Map<string, string>();
    for (const [path, f] of Object.entries(files)) {
      content.set(
        path,
        Array.isArray(f)
          ? upsertManagedBlock(e.baseline.get(path) ?? "", f)
          : f,
      );
    }
    const outputs: V2.CompiledOutput[] = [...content].map(([path, c]) => ({
      path,
      sha256: sha256Hex(c),
      drift_sha256: driftHash(c),
      bytes: Buffer.byteLength(c),
      lines: c.split("\n").length - 1,
      refs: [],
      cites: [],
      dropped_for_budget: [],
      ...(Array.isArray(files[path]) ? { user_owned: true } : {}),
    }));
    const drift = manifest(
      Object.fromEntries(outputs.map((o) => [o.path!, o.drift_sha256])),
    );
    t.dirty_gen = Math.max(t.dirty_gen, t.compiled_gen + 1);
    const run: V2.CompileRun = {
      id: randomUUID(),
      ref: `C-0${++this.seq}`,
      target_id: t.id,
      space_id: t.space_id,
      brief: "B-0043",
      brief_version: 1,
      generation: t.dirty_gen,
      status: t.delivery === "local" ? "compiled" : "delivered",
      input_sha256: sha256Hex(String(this.seq)),
      output_sha256: drift,
      drift_sha256: drift,
      bytes: outputs.reduce((n, o) => n + o.bytes, 0),
      lines: outputs.reduce((n, o) => n + o.lines, 0),
      refs: [],
      dropped_for_budget: [],
      files: outputs,
      warnings: [],
      enqueued_at: now(),
      started_at: now(),
      compiled_at: now(),
      receipt_id: randomUUID(),
    };
    e.runs.unshift({ run, content });
    t.compiled_gen = t.dirty_gen;
    t.last_compile = run;
    if (t.sync_state !== "off" && t.sync_state !== "drifted")
      t.sync_state = this.settled(e);
    return run;
  }

  /** A failed run: the preview keeps showing the last good one. */
  failCompile(id: string): void {
    const e = this.entry(id);
    const t = e.target;
    const run = {
      ...e.runs[0].run,
      id: randomUUID(),
      ref: `C-0${++this.seq}`,
      status: "failed" as const,
      error: "compile failed",
      files: [],
    };
    e.runs.unshift({ run, content: new Map() });
    t.last_compile = run;
  }

  /** Marks the target changed but not yet compiled (a Keep). */
  dirty(id: string): void {
    this.receipts++;
    const t = this.entry(id).target;
    t.dirty_gen++;
    if (t.sync_state !== "off" && t.sync_state !== "drifted")
      t.sync_state = "compiling";
  }

  /** A pull's proposals wait in Review: the server holds the file. */
  hold(id: string, proposals: string[]): void {
    const t = this.entry(id).target;
    const f = t.delivered?.files.find((x) => x.observation);
    if (!f || !f.observation) throw new Error("hold: no accepted edit to hold");
    f.held = true;
    t.holds = [
      { observation: f.observation, path: f.path, proposals, since: now() },
    ];
    t.sync_state = "held";
    this.receipts++;
  }

  /** The pull's proposals are decided: the latest run is due over the edit. */
  lift(id: string): void {
    const t = this.entry(id).target;
    for (const f of t.delivered?.files ?? []) if (f.observation) f.held = false;
    delete t.holds;
    t.sync_state = "pending_delivery";
    this.receipts++;
  }

  setState(id: string, state: V2.SyncState): void {
    const t = this.entry(id).target;
    t.sync_state = state;
    t.version++;
  }

  private lastGood(e: Entry): Run | undefined {
    return e.runs.find((r) => r.run.status !== "failed");
  }

  private settled(e: Entry): V2.SyncState {
    const t = e.target;
    if (t.sync_state === "off" || t.sync_state === "drifted")
      return t.sync_state;
    if (t.compiled_gen < t.dirty_gen) return "compiling";
    if (t.delivery !== "local" && t.delivery !== "pr") return "in_sync";
    return t.delivered?.compile_id === this.lastGood(e)?.run.id
      ? "in_sync"
      : "pending_delivery";
  }

  // --------------------------------------------------------------- HTTP

  private async handle(
    req: IncomingMessage,
    res: ServerResponse,
  ): Promise<void> {
    const chunks: Buffer[] = [];
    for await (const c of req) chunks.push(c as Buffer);
    const raw = Buffer.concat(chunks).toString("utf8");
    const url = new URL(req.url ?? "/", this.url);
    const path = decodeURIComponent(url.pathname);
    // The OAuth server's forms (device sign-in), before any credential.
    if (path.startsWith("/oauth/") && this.oauth) {
      const form = new URLSearchParams(raw);
      const out = this.oauth(path, form, req.headers);
      this.log.push({
        method: req.method ?? "",
        path,
        headers: req.headers,
        body: Object.fromEntries(form),
        status: out?.status ?? 404,
      });
      res.writeHead(out?.status ?? 404, {
        "Content-Type": "application/json",
        "Cache-Control": "no-store",
        ...(out?.headers ?? {}),
      });
      res.end(JSON.stringify(out?.body ?? { error: "not_found" }));
      return;
    }
    const body = raw ? (JSON.parse(raw) as Record<string, unknown>) : undefined;
    const send = (
      status: number,
      payload: unknown,
      headers: Record<string, string> = {},
    ) => {
      this.log.push({
        method: req.method ?? "",
        path: path + url.search,
        headers: req.headers,
        body,
        status,
      });
      res.writeHead(status, { "Content-Type": "application/json", ...headers });
      res.end(JSON.stringify(payload));
    };
    const fail = (status: number, code: string, message: string) =>
      send(status, { error: { code, message } });

    if (!req.headers.authorization)
      return fail(401, "unauthorized", "Sign in.");
    const fault = this.faults.find((f) => f.match.test(path) && f.times > 0);
    if (fault) {
      fault.times--;
      return fail(fault.status, fault.code, "Injected.");
    }
    const key = req.headers["idempotency-key"] as string | undefined;
    if (req.method === "POST" || req.method === "PATCH") {
      if (!key)
        return fail(
          400,
          "idempotency_key_required",
          "Send an Idempotency-Key.",
        );
      const prior = this.keys.get(key);
      if (prior)
        return send(prior.status, prior.body, {
          "Idempotent-Replayed": "true",
        });
    }
    const remember = (status: number, data: unknown) => {
      if (key) this.keys.set(key, { status, body: { data } });
      send(status, { data });
    };

    try {
      const out = this.route(req.method ?? "GET", path, url, body);
      if (!out) return fail(404, "not_found", "No such thing in your spaces.");
      if ("error" in out)
        return send(out.status, {
          error: {
            code: out.error,
            message: out.message,
            details: out.details,
          },
        });
      return req.method === "GET"
        ? send(out.status, { data: out.data })
        : remember(out.status, out.data);
    } catch (err) {
      return fail(500, "internal_error", (err as Error).message);
    }
  }

  private space(key: string): V2.Space | undefined {
    return this.spaces.find((s) => s.id === key || s.slug === key);
  }

  private route(
    method: string,
    path: string,
    url: URL,
    body: Record<string, unknown> | undefined,
  ): RouteResult | null {
    for (const r of this.extra) {
      const out = r(method, path, url, body);
      if (out) return out;
    }
    let m: RegExpMatchArray | null;
    if (method === "GET" && path === "/v2/spaces")
      return { status: 200, data: { items: this.spaces } };
    if (
      (m = path.match(
        /^\/v2\/spaces\/([^/]+)\/(targets|review|memories|agents|receipts)$/,
      ))
    ) {
      const sp = this.space(m[1]);
      if (!sp) return null;
      if (m[2] === "targets") {
        const items = [...this.targets.values()]
          .filter((e) => e.target.space_id === sp.id)
          .map((e) => e.target);
        return { status: 200, data: { items } };
      }
      if (m[2] === "review")
        return {
          status: 200,
          data: { items: [], has_more: false, total: this.reviewTotal },
        };
      if (m[2] === "receipts")
        return {
          status: 200,
          data: {
            items: this.receipts ? [{ id: `r-${this.receipts}` }] : [],
            has_more: false,
          },
        };
      if (m[2] === "agents")
        return { status: 200, data: { items: this.agents } };
      // Kept memories, paged by offset (the CLI only counts them).
      const from = Number(url.searchParams.get("cursor") ?? 0);
      const limit = Number(url.searchParams.get("limit") ?? 50);
      const n = Math.max(0, Math.min(limit, this.keptCount - from));
      const more = from + n < this.keptCount;
      return {
        status: 200,
        data: {
          items: Array.from({ length: n }, () => ({})),
          has_more: more,
          ...(more ? { next_cursor: String(from + n) } : {}),
        },
      };
    }
    if (
      !(m = path.match(
        /^\/v2\/targets\/([^/:]+)(?::(compile)|\/(preview|runs|deliveries|observations|drift(?::(?:pull|overwrite|stop))?))?$/,
      ))
    )
      return null;
    const e = this.targets.get(m[1]);
    if (!e) return null;
    const t = e.target;
    const op = m[2] ?? m[3] ?? "";
    switch (`${method} ${op}`) {
      case "PATCH ": {
        this.receipts++;
        const input = body as V2.ConfigureTargetInput;
        t.settings = { ...t.settings, ...input.settings };
        if (input.enabled === false) t.sync_state = "off";
        if (input.enabled === true && t.sync_state === "off") {
          t.sync_state = "compiling";
          this.dirty(t.id);
        }
        t.version++;
        return {
          status: 200,
          data: {
            outcome: "applied",
            policy: policy(),
            target: t,
            receipts: [],
          },
        };
      }
      case "GET preview": {
        const good = this.lastGood(e);
        const files = good
          ? good.run.files.map((f) => ({
              ...f,
              content: good.content.get(f.path!)!,
            }))
          : [];
        return {
          status: 200,
          data: {
            target: t,
            ...(good ? { compile: good.run } : {}),
            files,
            copies: [],
          },
        };
      }
      case "GET runs": {
        const limit = Number(url.searchParams.get("limit") ?? 50);
        return {
          status: 200,
          data: {
            items: e.runs.slice(0, limit).map((r) => r.run),
            has_more: e.runs.length > limit,
          },
        };
      }
      case "POST deliveries":
        return this.deliver(e, body as V2.DeliveryInput);
      case "POST observations":
        return this.observe(e, body as V2.ObservationInput);
      case "POST compile":
        if (t.sync_state === "off")
          return {
            status: 409,
            error: "invalid_transition",
            message: "The target is off.",
          };
        this.dirty(t.id);
        t.version++;
        return {
          status: 202,
          data: {
            outcome: "applied",
            policy: policy(),
            target: t,
            receipts: [],
          },
        };
      case "POST drift:pull":
      case "POST drift:overwrite":
      case "POST drift:stop":
        return this.resolve(e, op.slice("drift:".length) as V2.DriftMode);
    }
    return null;
  }

  private deliver(e: Entry, input: V2.DeliveryInput) {
    const t = e.target;
    const r = e.runs.find(
      (x) => x.run.ref === input.compile || x.run.id === input.compile,
    );
    if (!r) return null;
    if (r.run.status === "failed")
      return {
        status: 400,
        error: "invalid_request",
        message: "That run failed.",
      };
    if (
      (t.holds ?? []).some((h) => r.run.files.some((f) => f.path === h.path))
    ) {
      return {
        status: 409,
        error: "invalid_transition",
        message: "The file is held until its proposals are decided.",
      };
    }
    if (input.sha256 !== r.run.drift_sha256) {
      return {
        status: 400,
        error: "invalid_request",
        message: `sha256 doesn't match ${r.run.ref}`,
      };
    }
    const already =
      t.delivered?.compile_id === r.run.id &&
      t.delivered.sha256 === r.run.drift_sha256;
    const older =
      t.delivered?.compile_id &&
      e.runs.findIndex((x) => x.run.id === t.delivered!.compile_id) <
        e.runs.indexOf(r);
    if (!already && !older) {
      this.receipts++;
      r.run.status = "delivered";
      r.run.delivered_at = now();
      t.delivered = {
        compile_id: r.run.id,
        compile: r.run.ref,
        sha256: r.run.drift_sha256!,
        files: r.run.files.map((f) => ({
          path: f.path!,
          sha256: f.drift_sha256,
        })),
        at: now(),
      };
      e.baseline = new Map(r.content);
      t.sync_state = this.settled(e);
    }
    return { status: 200, data: { target: t, compile: r.run, receipts: [] } };
  }

  private observe(e: Entry, input: V2.ObservationInput) {
    const t = e.target;
    const sha = driftHash(input.content);
    const base = t.delivered?.files.find((f) => f.path === input.path);
    const latest = this.lastGood(e)?.run.files.find(
      (f) => f.path === input.path,
    );
    if (
      t.sync_state === "off" ||
      base?.sha256 === sha ||
      latest?.drift_sha256 === sha
    ) {
      return { status: 200, data: { drifted: false, target: t, receipts: [] } };
    }
    for (const o of e.observations)
      if (o.path === input.path && o.status === "open") o.status = "dismissed";
    const last = e.baseline.get(input.path) ?? "";
    const changeset = parseBack(last, input.content) as unknown as V2.ChangeSet;
    const o: V2.Observation = {
      id: randomUUID(),
      target_id: t.id,
      space_id: t.space_id,
      path: input.path,
      observed_sha256: sha,
      observer_kind: "device",
      observer_id: input.device_id ?? "",
      bytes: Buffer.byteLength(input.content),
      changeset,
      status: "open",
      observed_at: now(),
      receipt_id: randomUUID(),
      ...(base ? { base_sha256: base.sha256 } : {}),
    };
    e.observations.push(o);
    this.receipts++;
    (o as V2.Observation & { content?: string }).content = input.content;
    t.sync_state = "drifted";
    t.open_drift = e.observations.filter((x) => x.status === "open").length;
    t.version++;
    return {
      status: 201,
      data: {
        drifted: true,
        target: t,
        observation: stripContent(o),
        receipts: [],
      },
    };
  }

  private resolve(e: Entry, mode: V2.DriftMode) {
    const t = e.target;
    const open = e.observations.filter((o) => o.status === "open");
    if (open.length === 0)
      return {
        status: 409,
        error: "invalid_transition",
        message: "No hand edit.",
      };
    const files = [...(t.delivered?.files ?? [])];
    for (const o of open) {
      o.status =
        mode === "pull"
          ? "pulled"
          : mode === "overwrite"
            ? "overwritten"
            : "stopped";
      if (mode !== "stop") {
        const i = files.findIndex((f) => f.path === o.path);
        // As the server shows it: `held` with every accepted edit (this
        // fake's pulls write no proposals; hold() makes one wait).
        const f = {
          path: o.path,
          sha256: o.observed_sha256,
          observation: o.id,
          held: false,
        };
        if (i >= 0) files[i] = f;
        else files.push(f);
        e.baseline.set(
          o.path,
          (o as V2.Observation & { content?: string }).content ?? "",
        );
      }
    }
    t.open_drift = 0;
    this.receipts++;
    t.version++;
    if (mode === "stop") {
      t.sync_state = "off";
    } else {
      const keep = mode === "pull" ? t.delivered?.compile_id : undefined;
      const ref = mode === "pull" ? t.delivered?.compile : undefined;
      t.delivered = {
        ...(keep ? { compile_id: keep, compile: ref } : {}),
        sha256: manifest(
          Object.fromEntries(files.map((f) => [f.path, f.sha256])),
        ),
        files,
      };
      t.sync_state = "in_sync";
      t.sync_state = this.settled(e);
    }
    return {
      status: 200,
      data: {
        outcome: "applied",
        policy: policy(),
        target: t,
        observations: open.map(stripContent),
        proposals: [],
        receipts: [{}],
      },
    };
  }
}

function stripContent(o: V2.Observation): V2.Observation {
  const { content: _content, ...rest } = o as V2.Observation & {
    content?: string;
  };
  return rest;
}

function policy(): V2.PolicyDecision {
  return {
    effect: "allow",
    code: "member_keeps",
    message: "",
  } as unknown as V2.PolicyDecision;
}

/** The managed block of a file, for assertions. */
export function blockOf(content: string): string | null {
  return extractManagedBlock(content);
}
