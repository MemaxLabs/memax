// The /v2 resources: `memax.v2.spaces`, `.memories`, `.review`,
// `.receipts` and `.agents`. Thin, typed wrappers over the shared
// transport, so auth, the `{data}` envelope and MemaxError behave exactly
// as on /v1.
import { MemaxError } from "../errors.js";
import type { QueryValue, RequestFn } from "../transport.js";
import type {
  AgentCommandInput,
  AgentCommandResult,
  AgentDetail,
  AgentList,
  AutonomyInput,
  Brief,
  BriefResult,
  BriefVersionPage,
  ClientVia,
  CommandResult,
  CompileRunPage,
  ConfigureTargetInput,
  Conflict,
  CreateTargetInput,
  DeliveryInput,
  DeliveryResult,
  Drift,
  DriftResolutionResult,
  EditInput,
  MemoriesCommandResult,
  MemoryDetail,
  MemoryPage,
  ObservationInput,
  ObservationResult,
  PolicyDecision,
  ReceiptPage,
  RememberInput,
  ResolveConflictInput,
  ResolveDriftInput,
  ReviewInput,
  ReviewPage,
  ReviseBriefInput,
  Section,
  SpaceList,
  State,
  TargetList,
  TargetPreview,
  TargetResult,
  UndoInput,
} from "./types.js";

/** Options every command takes. */
export interface CommandOptions {
  /**
   * Required. Choose it once per intent (a uuid works) and send the same
   * key when you retry: the server then returns the original result and
   * writes nothing. A new command needs a new key.
   */
  idempotencyKey: string;
  /** The surface, for the receipt. Defaults to `api`. */
  via?: ClientVia;
  signal?: AbortSignal;
}

/** How a memory is addressed: a display ID needs its space. */
export interface MemoryRefOptions {
  /**
   * The space's id or slug. Required with a display ID (`M-0219`), which
   * is unique only within its tenant; optional with a memory id.
   */
  space?: string;
}

export interface ReviewOptions extends CommandOptions, MemoryRefOptions {
  /** The memory version you reviewed (its ETag); a newer version is 412. */
  ifMatch?: number;
}

export interface EditOptions extends CommandOptions, MemoryRefOptions {
  /** Required: the memory version you started from (its ETag). */
  ifMatch: number;
}

export interface PageOptions {
  cursor?: string;
  /** 1 to 200; defaults to 50. */
  limit?: number;
  signal?: AbortSignal;
}

export interface ListMemoriesOptions extends PageOptions {
  /** Only these displayed states. Without it, every state but rejected. */
  state?: State | State[];
  section?: Section | Section[];
}

export interface ListReceiptsOptions extends PageOptions {
  /** One memory's history, by display ID or id. */
  memory?: string;
}

export interface GetMemoryOptions extends MemoryRefOptions {
  signal?: AbortSignal;
}

export interface GetConflictOptions extends GetMemoryOptions {
  /** The other side, when the memory has more than one conflict. */
  with?: string;
}

function seg(value: string): string {
  return encodeURIComponent(value);
}

function commandHeaders(
  opts: CommandOptions,
  ifMatch?: number,
): Record<string, string> {
  const headers: Record<string, string> = {
    "Idempotency-Key": opts.idempotencyKey,
  };
  if (opts.via) headers["X-Memax-Via"] = opts.via;
  if (ifMatch !== undefined) headers["If-Match"] = `"${ifMatch}"`;
  return headers;
}

function pageQuery(opts: PageOptions | undefined): Record<string, QueryValue> {
  return { cursor: opts?.cursor, limit: opts?.limit };
}

function asList<T>(v: T | T[] | undefined): T[] | undefined {
  if (v === undefined) return undefined;
  return Array.isArray(v) ? v : [v];
}

export class V2SpacesResource {
  constructor(private readonly req: RequestFn) {}

  /** The spaces you belong to, with your role in each. */
  async list(opts?: { signal?: AbortSignal }): Promise<SpaceList> {
    return this.req("GET", "/v2/spaces", { signal: opts?.signal });
  }
}

export class V2MemoriesResource {
  constructor(private readonly req: RequestFn) {}

  /**
   * Remember a statement in a space. Policy decides the outcome: a member
   * or owner keeps it (`applied`); a viewer, an agent or an API key
   * proposes it (`proposed`). A refusal throws a MemaxError with code
   * `refused`; see {@link refusalOf}.
   */
  async remember(
    space: string,
    input: RememberInput,
    opts: CommandOptions,
  ): Promise<CommandResult> {
    return this.req("POST", `/v2/spaces/${seg(space)}/memories`, {
      body: input,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }

  /** A page of the space's memories, newest first. */
  async list(space: string, opts?: ListMemoriesOptions): Promise<MemoryPage> {
    return this.req("GET", `/v2/spaces/${seg(space)}/memories`, {
      query: {
        ...pageQuery(opts),
        state: asList(opts?.state),
        section: asList(opts?.section),
      },
      signal: opts?.signal,
    });
  }

  /** One memory with its sources, versions and latest receipts. */
  async get(ref: string, opts?: GetMemoryOptions): Promise<MemoryDetail> {
    return this.req("GET", `/v2/memories/${seg(ref)}`, {
      query: { space: opts?.space },
      signal: opts?.signal,
    });
  }

  /** Keep a proposal. Only a person who is a member or owner can. */
  async keep(
    ref: string,
    input: ReviewInput,
    opts: ReviewOptions,
  ): Promise<CommandResult> {
    return this.command(ref, "keep", input, opts, opts.ifMatch);
  }

  /** Reject a proposal. `input.reason` goes into the receipt. */
  async reject(
    ref: string,
    input: ReviewInput,
    opts: ReviewOptions,
  ): Promise<CommandResult> {
    return this.command(ref, "reject", input, opts, opts.ifMatch);
  }

  /**
   * Write a new version of the statement. `opts.ifMatch` is the version
   * you started from; if the memory changed since, this throws a
   * MemaxError with code `edit_clash` (412).
   */
  async edit(
    ref: string,
    input: EditInput,
    opts: EditOptions,
  ): Promise<CommandResult> {
    return this.command(ref, "edit", input, opts, opts.ifMatch);
  }

  /**
   * Both sides of one of this memory's conflicts, their latest receipts,
   * and the four answers with what each does and whether you may take it
   * (ReviewConflict).
   */
  async conflict(ref: string, opts?: GetConflictOptions): Promise<Conflict> {
    return this.req("GET", `/v2/memories/${seg(ref)}/conflict`, {
      query: { space: opts?.space, with: opts?.with },
      signal: opts?.signal,
    });
  }

  /**
   * Settle a conflict. Relative to this memory: `keep_this`, `keep_other`,
   * `keep_both` (with narrower `statement` / `other_statement`) or
   * `leave_open`. Only a person who may keep can; an agent or API key gets
   * a MemaxError `refused` (see {@link refusalOf}).
   */
  async resolveConflict(
    ref: string,
    input: ResolveConflictInput,
    opts: ReviewOptions,
  ): Promise<MemoriesCommandResult> {
    return this.req("POST", `/v2/memories/${seg(ref)}:resolve-conflict`, {
      query: { space: opts.space },
      body: input,
      extraHeaders: commandHeaders(opts, opts.ifMatch),
      signal: opts.signal,
    });
  }

  private async command(
    ref: string,
    verb: "keep" | "reject" | "edit",
    input: ReviewInput | EditInput,
    opts: CommandOptions & MemoryRefOptions,
    ifMatch: number | undefined,
  ): Promise<CommandResult> {
    return this.req("POST", `/v2/memories/${seg(ref)}:${verb}`, {
      query: { space: opts.space },
      body: input,
      extraHeaders: commandHeaders(opts, ifMatch),
      signal: opts.signal,
    });
  }
}

export class V2ReviewResource {
  constructor(private readonly req: RequestFn) {}

  /**
   * Review's queue: proposals, then kept memories in conflict, then stale
   * ones, oldest first within each group. `total` counts the whole queue.
   */
  async list(space: string, opts?: PageOptions): Promise<ReviewPage> {
    return this.req("GET", `/v2/spaces/${seg(space)}/review`, {
      query: pageQuery(opts),
      signal: opts?.signal,
    });
  }
}

export class V2ReceiptsResource {
  constructor(private readonly req: RequestFn) {}

  /** The space's Activity, newest first. Receipts never hold memory text. */
  async list(space: string, opts?: ListReceiptsOptions): Promise<ReceiptPage> {
    return this.req("GET", `/v2/spaces/${seg(space)}/receipts`, {
      query: { ...pageQuery(opts), memory: opts?.memory },
      signal: opts?.signal,
    });
  }

  /**
   * Undo the command that wrote this receipt (any of its receipts):
   * Review's ⌘Z. A person undoes their own keep, reject, edit or conflict
   * resolution within 10 minutes, and any person who may keep undoes one
   * of the judge's folds. A refusal throws a MemaxError `undo_refused`
   * whose `details.reason` is window_passed, later_changes,
   * already_undone or not_undoable.
   */
  async undo(
    receipt: string,
    input: UndoInput,
    opts: CommandOptions,
  ): Promise<MemoriesCommandResult> {
    return this.req("POST", `/v2/receipts/${seg(receipt)}:undo`, {
      body: input,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }
}

export class V2AgentsResource {
  constructor(private readonly req: RequestFn) {}

  /**
   * Your agent connections, each with its autonomy in every space of yours
   * it is connected to. With an agent's own credential, only that agent.
   */
  async list(opts?: { signal?: AbortSignal }): Promise<AgentList> {
    return this.req("GET", "/v2/agents", { signal: opts?.signal });
  }

  /** Every agent connected to a space, whoever it works for. */
  async listInSpace(
    space: string,
    opts?: { signal?: AbortSignal },
  ): Promise<AgentList> {
    return this.req("GET", `/v2/spaces/${seg(space)}/agents`, {
      signal: opts?.signal,
    });
  }

  /** One agent with its week, latest writes and latest sessions. */
  async get(
    agent: string,
    opts?: { signal?: AbortSignal },
  ): Promise<AgentDetail> {
    return this.req("GET", `/v2/agents/${seg(agent)}`, {
      signal: opts?.signal,
    });
  }

  /**
   * Set what an agent may do in a space, connecting it there if it isn't
   * yet. Raising needs a person on the web app: elsewhere it throws a
   * MemaxError `refused` with policy code `autonomy_needs_web` (see
   * {@link refusalOf}). Lowering works from anywhere.
   */
  async setAutonomy(
    agent: string,
    space: string,
    input: AutonomyInput,
    opts: CommandOptions,
  ): Promise<AgentCommandResult> {
    return this.req("PATCH", `/v2/agents/${seg(agent)}/spaces/${seg(space)}`, {
      body: input,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }

  /** Stop an agent writing anywhere until it is resumed. It still reads. */
  async pause(
    agent: string,
    input: AgentCommandInput,
    opts: CommandOptions,
  ): Promise<AgentCommandResult> {
    return this.command(agent, "pause", input, opts);
  }

  /** Let a paused agent write again. Needs a person on the web app. */
  async resume(
    agent: string,
    input: AgentCommandInput,
    opts: CommandOptions,
  ): Promise<AgentCommandResult> {
    return this.command(agent, "resume", input, opts);
  }

  /**
   * End the connection for good. Its API key or OAuth grant is revoked in
   * the same step, so the agent stops working at once.
   */
  async disconnect(
    agent: string,
    input: AgentCommandInput,
    opts: CommandOptions,
  ): Promise<AgentCommandResult> {
    return this.command(agent, "disconnect", input, opts);
  }

  private async command(
    agent: string,
    verb: "pause" | "resume" | "disconnect",
    input: AgentCommandInput,
    opts: CommandOptions,
  ): Promise<AgentCommandResult> {
    return this.req("POST", `/v2/agents/${seg(agent)}:${verb}`, {
      body: input,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }
}

export interface VersionedCommandOptions extends CommandOptions {
  /**
   * The version you started from (the Brief's or the target's ETag). A
   * Brief revision needs it once the space has a Brief; a newer version
   * throws a MemaxError `edit_clash` (412).
   */
  ifMatch?: number;
}

export class V2BriefsResource {
  constructor(private readonly req: RequestFn) {}

  /** The space's current Brief version. Throws `not_found` when it has none. */
  async get(space: string, opts?: { signal?: AbortSignal }): Promise<Brief> {
    return this.req("GET", `/v2/spaces/${seg(space)}/brief`, {
      signal: opts?.signal,
    });
  }

  /**
   * Write a new version of the Brief. Memory items must be kept memories
   * of the space, and prose must cite at least one. Every target of the
   * space recompiles.
   */
  async revise(
    space: string,
    input: ReviseBriefInput,
    opts: VersionedCommandOptions,
  ): Promise<BriefResult> {
    return this.req("POST", `/v2/spaces/${seg(space)}/brief`, {
      body: input,
      extraHeaders: commandHeaders(opts, opts.ifMatch),
      signal: opts.signal,
    });
  }

  /** Every version of the Brief, newest first, with who wrote it and why. */
  async versions(space: string, opts?: PageOptions): Promise<BriefVersionPage> {
    return this.req("GET", `/v2/spaces/${seg(space)}/brief/versions`, {
      query: pageQuery(opts),
      signal: opts?.signal,
    });
  }
}

export class V2TargetsResource {
  constructor(private readonly req: RequestFn) {}

  /** Where the space compiles to, each target with its sync state. */
  async list(
    space: string,
    opts?: { signal?: AbortSignal },
  ): Promise<TargetList> {
    return this.req("GET", `/v2/spaces/${seg(space)}/targets`, {
      signal: opts?.signal,
    });
  }

  /** Add a target (the kind's defaults fill the rest) and compile it. */
  async create(
    space: string,
    input: CreateTargetInput,
    opts: CommandOptions,
  ): Promise<TargetResult> {
    return this.req("POST", `/v2/spaces/${seg(space)}/targets`, {
      body: input,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }

  /** Change a target's path, settings or delivery, or stop or restart it. */
  async update(
    target: string,
    input: ConfigureTargetInput,
    opts: VersionedCommandOptions,
  ): Promise<TargetResult> {
    return this.req("PATCH", `/v2/targets/${seg(target)}`, {
      body: input,
      extraHeaders: commandHeaders(opts, opts.ifMatch),
      signal: opts.signal,
    });
  }

  /** Compile now. The compile is queued: the target answers `compiling`. */
  async compile(
    target: string,
    input: ReviewInput,
    opts: CommandOptions,
  ): Promise<TargetResult> {
    return this.req("POST", `/v2/targets/${seg(target)}:compile`, {
      body: input,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }

  /** The latest compiled content, with the run's metadata. */
  async preview(
    target: string,
    opts?: { signal?: AbortSignal },
  ): Promise<TargetPreview> {
    return this.req("GET", `/v2/targets/${seg(target)}/preview`, {
      signal: opts?.signal,
    });
  }

  /** The target's compile runs, newest first. */
  async runs(target: string, opts?: PageOptions): Promise<CompileRunPage> {
    return this.req("GET", `/v2/targets/${seg(target)}/runs`, {
      query: pageQuery(opts),
      signal: opts?.signal,
    });
  }

  /**
   * Report a target's file as it is on disk (the daemon). `drifted` says
   * whether it was a hand edit, now waiting for a person.
   */
  async observe(
    target: string,
    input: ObservationInput,
    opts: CommandOptions,
  ): Promise<ObservationResult> {
    return this.req("POST", `/v2/targets/${seg(target)}/observations`, {
      body: input,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }

  /** Acknowledge that a compile run's output is on disk. */
  async deliver(
    target: string,
    input: DeliveryInput,
    opts: CommandOptions,
  ): Promise<DeliveryResult> {
    return this.req("POST", `/v2/targets/${seg(target)}/deliveries`, {
      body: input,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }

  /** The target's open hand edits: both sides of each, and the changes. */
  async drift(target: string, opts?: { signal?: AbortSignal }): Promise<Drift> {
    return this.req("GET", `/v2/targets/${seg(target)}/drift`, {
      signal: opts?.signal,
    });
  }

  /** Turn the hand edits into proposals; removed lines wait for a person. */
  async pull(
    target: string,
    input: ResolveDriftInput,
    opts: CommandOptions,
  ): Promise<DriftResolutionResult> {
    return this.resolve(target, "pull", input, opts);
  }

  /** Write the compiled file over the hand edits. */
  async overwrite(
    target: string,
    input: ResolveDriftInput,
    opts: CommandOptions,
  ): Promise<DriftResolutionResult> {
    return this.resolve(target, "overwrite", input, opts);
  }

  /** Stop compiling the target; the file stays as it is. */
  async stop(
    target: string,
    input: ResolveDriftInput,
    opts: CommandOptions,
  ): Promise<DriftResolutionResult> {
    return this.resolve(target, "stop", input, opts);
  }

  private async resolve(
    target: string,
    mode: "pull" | "overwrite" | "stop",
    input: ResolveDriftInput,
    opts: CommandOptions,
  ): Promise<DriftResolutionResult> {
    return this.req("POST", `/v2/targets/${seg(target)}/drift:${mode}`, {
      body: input,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }
}

/** `memax.v2`: the V2 record. */
export class V2Resource {
  readonly spaces: V2SpacesResource;
  readonly memories: V2MemoriesResource;
  readonly review: V2ReviewResource;
  readonly receipts: V2ReceiptsResource;
  readonly agents: V2AgentsResource;
  readonly briefs: V2BriefsResource;
  readonly targets: V2TargetsResource;

  constructor(req: RequestFn) {
    this.spaces = new V2SpacesResource(req);
    this.memories = new V2MemoriesResource(req);
    this.review = new V2ReviewResource(req);
    this.receipts = new V2ReceiptsResource(req);
    this.agents = new V2AgentsResource(req);
    this.briefs = new V2BriefsResource(req);
    this.targets = new V2TargetsResource(req);
  }
}

/**
 * The policy decision behind a refused command (403 `refused`), or
 * undefined for any other error. Localise by `code`; `message` is the
 * English fallback.
 */
export function refusalOf(err: unknown): PolicyDecision | undefined {
  if (!(err instanceof MemaxError) || err.code !== "refused") return undefined;
  const policy = err.details?.policy;
  if (policy && typeof policy === "object" && "effect" in policy) {
    return policy as PolicyDecision;
  }
  return undefined;
}
