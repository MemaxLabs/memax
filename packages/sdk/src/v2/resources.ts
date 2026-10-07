// The /v2 resources: `memax.v2.spaces`, `.memories`, `.review`, `.imports`,
// `.receipts`, `.reads`, `.agents`, `.briefs`, `.targets`, `.gates`,
// `.notices`, `.devices`, `.sessions`, `.dream`, `.notes` and `.settings`. Thin, typed
// wrappers over the shared
// transport, so auth, the `{data}` envelope and MemaxError behave exactly
// as on /v1.
import { MemaxError } from "../errors.js";
import type { OpenFn, QueryValue, RequestFn } from "../transport.js";
import { readEventStream } from "./sse.js";
import type {
  AgentCommandInput,
  AskEvent,
  AskInput,
  AgentCommandResult,
  AgentDetail,
  AgentList,
  AutonomyInput,
  CheckpointPage,
  Brief,
  BriefResult,
  BriefVersionPage,
  BulkReviewInput,
  BulkReviewResult,
  ClientVia,
  CommandResult,
  CompileLoadInput,
  CompileLoadResult,
  CompileRunPage,
  ConfigureTargetInput,
  Conflict,
  CreateSpaceInput,
  CreateTargetInput,
  DeliveryInput,
  DeliveryResult,
  DeviceAuthorization,
  DeviceCodeInput,
  DreamAction,
  DreamActionKind,
  DreamActionPage,
  DreamEdition,
  DreamEditionPage,
  DreamRun,
  DreamSettings,
  DreamSettingsInput,
  NotificationSettings,
  NotificationSettingsInput,
  Security,
  DreamUndoResult,
  UndoEditionInput,
  UndoEditionResult,
  Drift,
  DriftResolutionResult,
  EditInput,
  AckNoticesInput,
  AckNoticesResult,
  ForgetCarry,
  ForgetInput,
  ForgetPreview,
  ForgetRequestResult,
  ForgetResult,
  NoticeList,
  Tombstone,
  TombstonePage,
  AnswerGateInput,
  Gate,
  GatePage,
  GateResult,
  GateStatus,
  ImportConflictResult,
  ImportInput,
  ImportPage,
  ImportResult,
  ImportView,
  MemoriesCommandResult,
  MemoryDetail,
  MemoryPage,
  NearDuplicates,
  NearDuplicatesInput,
  ObservationInput,
  ObservationResult,
  PolicyDecision,
  ReadPage,
  ReceiptPage,
  RememberInput,
  RequestDecisionInput,
  ResolveConflictInput,
  ResolveDriftInput,
  ReviewInput,
  ReviewPage,
  ReviseBriefInput,
  Section,
  Session,
  SessionList,
  SessionsRevoked,
  SettleImportConflictInput,
  Space,
  SpaceList,
  SpaceSwitch,
  SwitchSpaceInput,
  Note,
  NotePage,
  NoteForgetPreview,
  NoteForgetResult,
  ForgetNoteInput,
  V1DreamRunList,
  State,
  TargetList,
  TargetPreview,
  TargetResult,
  UndoInput,
  WithdrawGateInput,
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

/** An export of a space, as {@link V2SpacesResource.export} downloads it. */
export interface SpaceExport {
  /** The zip archive (memax.export.v1): one folder, named by the space's slug. */
  bytes: Uint8Array;
  /** The download's name: `memax-<slug>-<yyyy-mm-dd>.zip`. */
  filename: string;
  /** The export's own `exported` receipt. */
  receipt: string;
  /** A retry with the same idempotency key: nothing new was written. */
  replayed: boolean;
}

export class V2SpacesResource {
  constructor(
    private readonly req: RequestFn,
    private readonly open?: OpenFn,
  ) {}

  /**
   * Export the space's whole record as a zip archive (memax.export.v1):
   * every memory as Markdown with frontmatter, tombstones without words,
   * the Brief's versions, gates, targets, agents, read counts, every
   * receipt in chain order and the signed checkpoints, with a manifest of
   * every file's SHA-256. Check it with {@link verifyExport}. Any person
   * who may read the space exports it; agents and API keys get a
   * MemaxError `refused` (policy `export_by_person`). Each export is one
   * `exported` receipt; a retry with the same idempotency key writes none.
   * Rate-limited per person (`rate_limited`, with `retryAfter`).
   */
  async export(space: string, opts: CommandOptions): Promise<SpaceExport> {
    if (!this.open) {
      throw new MemaxError(
        "This client can't download an export.",
        "invalid_request",
        0,
      );
    }
    const res = await this.open("POST", `/v2/spaces/${seg(space)}:export`, {
      extraHeaders: { ...commandHeaders(opts), Accept: "application/zip" },
      signal: opts.signal,
    });
    let bytes: Uint8Array;
    try {
      bytes = new Uint8Array(await res.arrayBuffer());
    } catch (err) {
      if (opts.signal?.aborted) throw err;
      // The server ends a failed export early: retry with the same key.
      throw new MemaxError(
        "The export stopped partway. Export again with the same idempotency key.",
        "network_error",
        0,
      );
    }
    const disposition = res.headers.get("Content-Disposition") ?? "";
    const named = /filename="([^"]+)"/.exec(disposition)?.[1];
    return {
      bytes,
      filename: named ?? `memax-${space}.zip`,
      receipt: res.headers.get("X-Memax-Export-Receipt") ?? "",
      replayed: res.headers.get("Idempotent-Replayed") === "true",
    };
  }

  /** The spaces you belong to, with your role in each. */
  async list(opts?: { signal?: AbortSignal }): Promise<SpaceList> {
    return this.req("GET", "/v2/spaces", { signal: opts?.signal });
  }

  /**
   * Create a project space on the V2 record, owned by you (`memax init`
   * for a repository with no space yet). Without a `slug`, Memax picks one
   * from the name; a slug you name that is taken throws a MemaxError
   * `slug_taken` (409). Only a signed-in person creates spaces. The same
   * idempotency key finds the space the first call created.
   */
  async create(input: CreateSpaceInput, opts: CommandOptions): Promise<Space> {
    return this.req("POST", "/v2/spaces", {
      body: input,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }

  /**
   * Where the space's Switch to V2 stands, with a fresh preview of what
   * switching moves (the dry run; it changes nothing): the members and the
   * roles they keep, how many V1 memories become notes and how many of a
   * person's own are offered for bulk keep, the agent files, the agents
   * connected at Propose, waiting decisions, V1 Dream runs and the plan.
   */
  async switchStatus(
    space: string,
    opts?: { signal?: AbortSignal },
  ): Promise<SpaceSwitch> {
    return this.req("GET", `/v2/spaces/${seg(space)}/switch`, {
      signal: opts?.signal,
    });
  }

  /**
   * Switch a space to the V2 record (plan 25 §10). Nothing is lost and no
   * V1 row changes: V1 memories become notes, a person's own short ones go
   * up as one import for bulk keep ("From V1"), agents' wait for Dream,
   * the members' agents are connected at Propose and told. A space with
   * nothing to import switches in the call; otherwise `background` is set
   * and `state` is `running`: read it again with {@link switchStatus}. A
   * `failed` switch resumes when asked again. `kind: "project"` lets a V1
   * team hub switch as a project space while it has no V2 record (a
   * MemaxError `space_kind` otherwise). Only the space's owner may.
   */
  async switchToV2(
    space: string,
    opts: CommandOptions & { kind?: "project" | "team"; repository?: string },
  ): Promise<SpaceSwitch> {
    const body: SwitchSpaceInput = { to: "v2" };
    if (opts.kind) body.kind = opts.kind;
    if (opts.repository !== undefined) body.repository = opts.repository;
    return this.req("POST", `/v2/spaces/${seg(space)}:switch`, {
      body,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }

  /**
   * Switch a space back to V1: every surface serves it as V1 again, and
   * its V1 rows answer exactly as before. Its V2 record stays, for a later
   * switch. Only the space's owner may.
   */
  async switchToV1(space: string, opts: CommandOptions): Promise<SpaceSwitch> {
    return this.req("POST", `/v2/spaces/${seg(space)}:switch`, {
      body: { to: "v1" },
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }

  /** The space's V1 Dream runs, newest first: read-only history. */
  async v1DreamRuns(
    space: string,
    opts?: { signal?: AbortSignal },
  ): Promise<V1DreamRunList> {
    return this.req("GET", `/v2/spaces/${seg(space)}/v1-dream-runs`, {
      signal: opts?.signal,
    });
  }
}

/**
 * `memax.v2.notes`: a space's notes (N-), its raw material (V1 memories,
 * personas and agent files, and what agents capture). Notes are never
 * compiled. You see the notes you wrote and, in a space you own, all of
 * them.
 */
export class V2NotesResource {
  constructor(private readonly req: RequestFn) {}

  /** Search a space's notes (the newest, without `q`). */
  async search(
    space: string,
    query?: { q?: string; limit?: number },
    opts?: { signal?: AbortSignal },
  ): Promise<NotePage> {
    return this.req("GET", `/v2/spaces/${seg(space)}/notes`, {
      query: { q: query?.q, limit: query?.limit },
      signal: opts?.signal,
    });
  }

  /** One note, by its ref (N-0042) or id. */
  async get(
    space: string,
    note: string,
    opts?: { signal?: AbortSignal },
  ): Promise<Note> {
    return this.req("GET", `/v2/spaces/${seg(space)}/notes/${seg(note)}`, {
      signal: opts?.signal,
    });
  }

  /** What forgetting a note would take with it, and whether you may. */
  async previewForget(
    space: string,
    note: string,
    opts?: { signal?: AbortSignal },
  ): Promise<NoteForgetPreview> {
    return this.req(
      "GET",
      `/v2/spaces/${seg(space)}/notes/${seg(note)}/forget-preview`,
      { signal: opts?.signal },
    );
  }

  /**
   * Forget a note everywhere (rule 7): its V1 row goes, with what V1
   * derived from it, and the memories carrying its words (name them in
   * `carries`, as `forget_carries` listed them). It can't be undone.
   */
  async forget(
    space: string,
    note: string,
    input: ForgetNoteInput,
    opts: CommandOptions,
  ): Promise<NoteForgetResult> {
    return this.req(
      "POST",
      `/v2/spaces/${seg(space)}/notes/${seg(note)}:forget`,
      { body: input, extraHeaders: commandHeaders(opts), signal: opts.signal },
    );
  }
}

export class V2ImportsResource {
  constructor(private readonly req: RequestFn) {}

  /**
   * Upload statements read from agent files (`memax init`). Each becomes
   * its own proposal (`via: import`) with its `file:line` sources; repeats
   * within the upload fold into one, and what the space already has is
   * skipped. Nothing is kept. Retrying with the same idempotency key
   * resumes the same import. At most 500 statements per import.
   */
  async create(
    space: string,
    input: ImportInput,
    opts: CommandOptions,
  ): Promise<ImportResult> {
    return this.req("POST", `/v2/spaces/${seg(space)}/imports`, {
      body: input,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }

  /** The space's imports, newest first. */
  async list(space: string, opts?: PageOptions): Promise<ImportPage> {
    return this.req("GET", `/v2/spaces/${seg(space)}/imports`, {
      query: pageQuery(opts),
      signal: opts?.signal,
    });
  }

  /**
   * One import in full: what became of each statement, the memories they
   * became with the judge's verdicts, the disagreements found, and which
   * proposals can be kept in bulk. Poll it until `progress.ready`.
   */
  async get(
    space: string,
    importId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<ImportView> {
    return this.req(
      "GET",
      `/v2/spaces/${seg(space)}/imports/${seg(importId)}`,
      { signal: opts?.signal },
    );
  }

  /**
   * Settle one of an import's disagreements, once, as a group:
   * `keep_one` (with `keep`), `keep_all`, `leave_open` or
   * `keep_suggestion`. It follows Keep's rules; a refusal throws a
   * MemaxError `refused` (see {@link refusalOf}).
   */
  async settle(
    space: string,
    importId: string,
    n: number,
    input: SettleImportConflictInput,
    opts: CommandOptions,
  ): Promise<ImportConflictResult> {
    return this.req(
      "POST",
      `/v2/spaces/${seg(space)}/imports/${seg(importId)}/conflicts/${n}:settle`,
      { body: input, extraHeaders: commandHeaders(opts), signal: opts.signal },
    );
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

  /**
   * Remember's near-duplicate check: the kept memories and pending
   * proposals of the space that a draft repeats, best first (`exact`, the
   * same words; `near`, the same thing by meaning). It only reads, so it
   * takes no idempotency key; call it debounced while a person types (it
   * is rate-limited per caller, `rate_limited` with `retryAfter`). When
   * the server has no embeddings, or the draft's embedding was late, only
   * exact repeats are checked and `semantic` is false.
   */
  async nearDuplicates(
    space: string,
    input: NearDuplicatesInput,
    opts?: { signal?: AbortSignal },
  ): Promise<NearDuplicates> {
    return this.req(
      "POST",
      `/v2/spaces/${seg(space)}/memories:near-duplicates`,
      { body: input, signal: opts?.signal },
    );
  }

  /**
   * Keep several proposals, each as its own Keep with its own receipt.
   * One that can't be kept is reported in its item (`refused`, with the
   * policy, or `failed`, with the error) and the rest are kept. Send each
   * item's `version` as seen. The same idempotency key keeps nothing twice.
   */
  async keepMany(
    space: string,
    input: BulkReviewInput,
    opts: CommandOptions,
  ): Promise<BulkReviewResult> {
    return this.req("POST", `/v2/spaces/${seg(space)}/memories:keep`, {
      body: input,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }

  /** Reject several proposals, each as its own Reject. */
  async rejectMany(
    space: string,
    input: BulkReviewInput,
    opts: CommandOptions,
  ): Promise<BulkReviewResult> {
    return this.req("POST", `/v2/spaces/${seg(space)}/memories:reject`, {
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

  /**
   * Keep a proposal. Only a person who is a member or owner can. Before
   * the judge has looked at a proposal that touches a decision in force,
   * this throws a MemaxError `judge_pending` (503, `retryAfterSeconds`
   * set): send the same Keep, with the same key, after that. A proposal
   * the judge flagged throws `in_conflict` (409; `details.ref` is the
   * decision in force): settle it with {@link resolveConflict}.
   */
  async keep(
    ref: string,
    input: ReviewInput,
    opts: ReviewOptions,
  ): Promise<CommandResult> {
    return this.command(ref, "keep", input, opts, opts.ifMatch);
  }

  /**
   * Restore a faded memory: Dream fades what nobody read in 60 days, and
   * never deletes it. It follows Keep's rules, and the memory compiles
   * again.
   */
  async restore(
    ref: string,
    input: ReviewInput,
    opts: ReviewOptions,
  ): Promise<CommandResult> {
    return this.command(ref, "restore", input, opts, opts.ifMatch);
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
   * MemaxError with code `edit_clash` (412). With `keep: true`, new words
   * that touch a decision in force are saved but not kept until the judge
   * has looked: the result is `outcome: "proposed"` with policy code
   * `judge_pending`, and {@link keep} on the returned version finishes it.
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

  /**
   * What a Forget of the memory would do, before anyone confirms it: the
   * memories that go with it (send their refs as `carries`), the compiled
   * files that hold it, how many agents would be told, the version to
   * send as `ifMatch`, and whether you may (`allowed`, with `policy`). It
   * changes nothing.
   */
  async previewForget(
    ref: string,
    opts?: GetMemoryOptions,
  ): Promise<ForgetPreview> {
    return this.req("GET", `/v2/memories/${seg(ref)}/forget-preview`, {
      query: { space: opts?.space },
      signal: opts?.signal,
    });
  }

  /**
   * Forget a memory everywhere (rule 7): its words leave the record in one
   * step, every target recompiles without it, and every agent that read it
   * is told. It can't be undone. Only a person who may forget in the space
   * can; agents ask with {@link requestForget}. `opts.ifMatch` is the
   * version you saw. Memories that carry its words (proposals folded into
   * it, memories citing it) go with it: name them in `input.carries`. When
   * the list doesn't match, this throws a MemaxError `forget_carries`
   * (409) and nothing changes; {@link forgetCarriesOf} reads the list.
   */
  async forget(
    ref: string,
    input: ForgetInput,
    opts: EditOptions,
  ): Promise<ForgetResult> {
    return this.req("POST", `/v2/memories/${seg(ref)}:forget`, {
      query: { space: opts.space },
      body: input,
      extraHeaders: commandHeaders(opts, opts.ifMatch),
      signal: opts.signal,
    });
  }

  /**
   * An agent asks a person to forget a memory (memax_forget): it records a
   * request, with `input.reason`, that a person confirms or declines on
   * the web. It forgets nothing. A person forgets with {@link forget}.
   */
  async requestForget(
    ref: string,
    input: ReviewInput,
    opts: CommandOptions & MemoryRefOptions,
  ): Promise<ForgetRequestResult> {
    return this.req("POST", `/v2/memories/${seg(ref)}:request-forget`, {
      query: { space: opts.space },
      body: input,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }

  /**
   * Keep a memory an agent asked to forget: every waiting request is
   * declined. Throws `invalid_transition` (409) when nobody asked.
   */
  async declineForget(
    ref: string,
    input: ReviewInput,
    opts: CommandOptions & MemoryRefOptions,
  ): Promise<CommandResult> {
    return this.req("POST", `/v2/memories/${seg(ref)}:decline-forget`, {
      query: { space: opts.space },
      body: input,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }

  /**
   * A forgotten memory's tombstone: who forgot it and when, what went with
   * it, each step of the Forget with where it stands, and the copies Memax
   * can't reach. Never words. Throws `not_found` when it isn't forgotten.
   */
  async tombstone(ref: string, opts?: GetMemoryOptions): Promise<Tombstone> {
    return this.req("GET", `/v2/memories/${seg(ref)}/tombstone`, {
      query: { space: opts?.space },
      signal: opts?.signal,
    });
  }

  /** What was forgotten in a space, newest first (without the steps). */
  async tombstones(space: string, opts?: PageOptions): Promise<TombstonePage> {
    return this.req("GET", `/v2/spaces/${seg(space)}/tombstones`, {
      query: pageQuery(opts),
      signal: opts?.signal,
    });
  }

  private async command(
    ref: string,
    verb: "keep" | "reject" | "edit" | "restore",
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
   * The space's sealed receipt chain, newest checkpoint first, with how
   * far it is sealed and verified (`seal`) and the public keys checkpoints
   * are signed with. Check an export against them with
   * {@link verifyReceiptChain}, pinning the keys you trust.
   */
  async checkpoints(
    space: string,
    opts?: PageOptions,
  ): Promise<CheckpointPage> {
    return this.req("GET", `/v2/spaces/${seg(space)}/checkpoints`, {
      query: pageQuery(opts),
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

export class V2ReadsResource {
  constructor(private readonly req: RequestFn) {}

  /**
   * What agents read in the space (R-), newest first, with `reads_7d`.
   * Reads are not receipts: they hold memory and compile refs, never words.
   */
  async list(space: string, opts?: PageOptions): Promise<ReadPage> {
    return this.req("GET", `/v2/spaces/${seg(space)}/reads`, {
      query: pageQuery(opts),
      signal: opts?.signal,
    });
  }

  /**
   * Report the compile (C-) an agent loaded natively at session start: a
   * read of every fact in it, and how Memax knows the file's loads are
   * seen. An agent's credential reports its own loads in spaces it is
   * connected to; a person's session names the agent (`input.agent`).
   * Report within a day of the load; the same key records it once.
   */
  async recordCompileLoad(
    space: string,
    input: CompileLoadInput,
    opts: CommandOptions,
  ): Promise<CompileLoadResult> {
    return this.req("POST", `/v2/spaces/${seg(space)}/compile-loads`, {
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

export interface ListGatesOptions extends PageOptions {
  /** Only these statuses. Without it, every gate. */
  status?: GateStatus | GateStatus[];
}

/** How a gate is addressed: a display ID (`G-0012`) needs its space. */
export interface GateRefOptions {
  /** The space's id or slug. Required with a display ID; optional with a gate id. */
  space?: string;
}

export interface GateCommandOptions extends CommandOptions, GateRefOptions {
  /**
   * The gate's version you read (its ETag). A gate that ended since throws
   * `invalid_transition` (409); any other mismatch, `edit_clash` (412).
   */
  ifMatch?: number;
}

export class V2GatesResource {
  constructor(private readonly req: RequestFn) {}

  /** The space's decision gates, newest first. */
  async list(space: string, opts?: ListGatesOptions): Promise<GatePage> {
    return this.req("GET", `/v2/spaces/${seg(space)}/gates`, {
      query: { ...pageQuery(opts), status: asList(opts?.status) },
      signal: opts?.signal,
    });
  }

  /**
   * Ask a person to decide (an agent that may propose). The gate waits for
   * an answer until `expires_at`. A refusal (a person asking, a read-only
   * agent, three already waiting) throws a MemaxError `refused`.
   */
  async request(
    space: string,
    input: RequestDecisionInput,
    opts: CommandOptions,
  ): Promise<GateResult> {
    return this.req("POST", `/v2/spaces/${seg(space)}/gates`, {
      body: input,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }

  /** One gate, by display ID (with `space`) or id. */
  async get(
    ref: string,
    opts?: GateRefOptions & { signal?: AbortSignal },
  ): Promise<Gate> {
    return this.req("GET", `/v2/gates/${seg(ref)}`, {
      query: { space: opts?.space },
      signal: opts?.signal,
    });
  }

  /**
   * Answer with one option (from 1). The answer is kept as a decision you
   * authored: `memory` in the result. Where the space's decisions need a
   * person on the web (`gate.needs_web`), only the web app can answer; a
   * gate that ended already throws `invalid_transition` (409).
   */
  async answer(
    ref: string,
    input: AnswerGateInput,
    opts: GateCommandOptions,
  ): Promise<GateResult> {
    return this.command(ref, "answer", input, opts);
  }

  /** Take a waiting gate's question back. */
  async withdraw(
    ref: string,
    input: WithdrawGateInput,
    opts: GateCommandOptions,
  ): Promise<GateResult> {
    return this.command(ref, "withdraw", input, opts);
  }

  private async command(
    ref: string,
    verb: "answer" | "withdraw",
    input: AnswerGateInput | WithdrawGateInput,
    opts: GateCommandOptions,
  ): Promise<GateResult> {
    return this.req("POST", `/v2/gates/${seg(ref)}:${verb}`, {
      query: { space: opts.space },
      body: input,
      extraHeaders: commandHeaders(opts, opts.ifMatch),
      signal: opts.signal,
    });
  }
}

/** Options for confirming or declining a device's code. */
export interface DeviceCommandOptions {
  /** One per intent, the same on a retry (see {@link CommandOptions}). */
  idempotencyKey: string;
  signal?: AbortSignal;
}

/**
 * `memax.v2.devices`: a person confirming, on the web, the code the memax
 * CLI shows on a machine with no browser (CliAuth). The CLI's side of the
 * flow is `memax.auth.startDeviceSignIn` and `pollDeviceSignIn` (RFC 8628).
 */
export class V2DevicesResource {
  constructor(private readonly req: RequestFn) {}

  /**
   * What a waiting code says about the device asking. A code that doesn't
   * exist, or that someone else decided, throws `not_found`; naming too many
   * that don't exist throws `rate_limited`.
   */
  async lookup(
    userCode: string,
    opts?: { signal?: AbortSignal },
  ): Promise<DeviceAuthorization> {
    const body: DeviceCodeInput = { user_code: userCode };
    return this.req("POST", "/v2/device-authorizations:lookup", {
      body,
      signal: opts?.signal,
    });
  }

  /**
   * Sign the device in as you: its next poll collects a CLI session, once.
   * Only a person on the web app may (`refused` with `device_needs_web`
   * otherwise). A code you declined or that expired throws
   * `invalid_transition` with `details.state`.
   */
  async approve(
    userCode: string,
    opts: DeviceCommandOptions,
  ): Promise<DeviceAuthorization> {
    return this.command("approve", userCode, opts);
  }

  /** Decline the code ("It doesn't match"): nothing is signed in. */
  async deny(
    userCode: string,
    opts: DeviceCommandOptions,
  ): Promise<DeviceAuthorization> {
    return this.command("deny", userCode, opts);
  }

  private async command(
    verb: "approve" | "deny",
    userCode: string,
    opts: DeviceCommandOptions,
  ): Promise<DeviceAuthorization> {
    const body: DeviceCodeInput = { user_code: userCode };
    return this.req("POST", `/v2/device-authorizations:${verb}`, {
      body,
      extraHeaders: { "Idempotency-Key": opts.idempotencyKey },
      signal: opts.signal,
    });
  }
}

/** Options for signing a session out. */
export interface SessionCommandOptions {
  /** One per intent, the same on a retry (see {@link CommandOptions}). */
  idempotencyKey: string;
  signal?: AbortSignal;
}

/**
 * `memax.v2.sessions`: everywhere you are signed in (the web app, the CLI,
 * devices, MCP clients), and signing any of it out. Signing a session out
 * ends its refresh token at once, and its last access token within the
 * hour. A client signs its own session out with `memax.auth.revoke`.
 */
export class V2SessionsResource {
  constructor(private readonly req: RequestFn) {}

  /** Your live sessions, the most recently used first; `current` marks this one. */
  async list(opts?: { signal?: AbortSignal }): Promise<SessionList> {
    return this.req("GET", "/v2/sessions", { signal: opts?.signal });
  }

  /**
   * Sign one session out. The session you call from may sign itself out
   * anywhere; any other needs you on the web app (`refused` with
   * `session_needs_web`). A session that isn't yours, or already ended,
   * throws `not_found`.
   */
  async revoke(id: string, opts: SessionCommandOptions): Promise<Session> {
    return this.req("POST", `/v2/sessions/${seg(id)}:revoke`, {
      extraHeaders: { "Idempotency-Key": opts.idempotencyKey },
      signal: opts.signal,
    });
  }

  /**
   * Sign out everywhere but this session (web app only,
   * `session_needs_web`). A sign-in from before sessions were named throws
   * `invalid_transition`: sign in again first.
   */
  async revokeOthers(opts: SessionCommandOptions): Promise<SessionsRevoked> {
    return this.req("POST", "/v2/sessions:revoke-others", {
      extraHeaders: { "Idempotency-Key": opts.idempotencyKey },
      signal: opts.signal,
    });
  }
}

export class V2NoticesResource {
  constructor(private readonly req: RequestFn) {}

  /**
   * For an agent's own credential: what it hasn't been told yet, oldest
   * first (memories forgotten since it read them, spaces forgotten whole).
   * Tell the agent, then {@link ack} them. A person's session has none.
   */
  async list(opts?: { signal?: AbortSignal }): Promise<NoticeList> {
    return this.req("GET", "/v2/notices", { signal: opts?.signal });
  }

  /**
   * Mark notices told, each once. It writes no receipt; acknowledging
   * again changes nothing.
   */
  async ack(
    input: AckNoticesInput,
    opts: CommandOptions,
  ): Promise<AckNoticesResult> {
    return this.req("POST", "/v2/notices:ack", {
      body: input,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }
}

/** Options for listing an edition's actions. */
export interface ListDreamActionsOptions extends PageOptions {
  /** Only actions of this kind. */
  kind?: DreamActionKind;
}

/**
 * `memax.v2.dream`: Dream's editions, the overnight upkeep of a space done
 * in the open. Each edition (`D-`) lists what Dream read and every small
 * action it took, each with receipts that cite the edition, and any person
 * who may keep undoes any of them for 30 days.
 */
export class V2DreamResource {
  constructor(private readonly req: RequestFn) {}

  /**
   * The space's editions, newest first, and when the next is due
   * (`schedule`). The SDK sends the client's time zone, which is how Dream
   * learns the person's local night.
   */
  async editions(space: string, opts?: PageOptions): Promise<DreamEditionPage> {
    return this.req("GET", `/v2/spaces/${seg(space)}/dream/editions`, {
      query: pageQuery(opts),
      signal: opts?.signal,
    });
  }

  /**
   * One edition by display ID (`D-0214`), number (`214`), id or `latest`,
   * with its actions and what it found that needs a person. A space with no
   * edition yet throws `not_found` for `latest`.
   */
  async edition(
    space: string,
    edition: string,
    opts?: { signal?: AbortSignal },
  ): Promise<DreamEdition> {
    return this.req(
      "GET",
      `/v2/spaces/${seg(space)}/dream/editions/${seg(edition)}`,
      { signal: opts?.signal },
    );
  }

  /** An edition's actions, in order, of one kind or all. */
  async actions(
    space: string,
    edition: string,
    opts?: ListDreamActionsOptions,
  ): Promise<DreamActionPage> {
    return this.req(
      "GET",
      `/v2/spaces/${seg(space)}/dream/editions/${seg(edition)}/actions`,
      { query: { ...pageQuery(opts), kind: opts?.kind }, signal: opts?.signal },
    );
  }

  /**
   * Undo one of Dream's actions: the state before it comes back exactly.
   * A refusal (already undone, too old, a later change in the way, the
   * memory forgotten) throws a MemaxError `undo_refused` with
   * `details.reason`.
   */
  async undo(
    action: string,
    input: UndoInput,
    opts: CommandOptions,
  ): Promise<DreamUndoResult> {
    return this.req("POST", `/v2/dream/actions/${seg(action)}:undo`, {
      body: input,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }

  /**
   * Undo every undoable action of one kind in an edition ("Undo both",
   * "Restore all"), each as its own command. What can't be undone is in
   * `refused`; the rest goes ahead. Reuse the key on a retry.
   */
  async undoAll(
    space: string,
    edition: string,
    input: UndoEditionInput,
    opts: CommandOptions,
  ): Promise<UndoEditionResult> {
    return this.req(
      "POST",
      `/v2/spaces/${seg(space)}/dream/editions/${seg(edition)}:undo`,
      { body: input, extraHeaders: commandHeaders(opts), signal: opts.signal },
    );
  }

  /**
   * Ask Dream to run on the space now (its owner only; a few times a day
   * on Pro). It runs only if the space has something new since the last
   * edition. Too soon throws `rate_limited` with `retryAfter`.
   */
  async run(space: string, opts: CommandOptions): Promise<DreamRun> {
    return this.req("POST", `/v2/spaces/${seg(space)}/dream:run`, {
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }

  /** Your time zone (Dream runs in your night) and the morning email. */
  async settings(opts?: { signal?: AbortSignal }): Promise<DreamSettings> {
    return this.req("GET", "/v2/dream/settings", { signal: opts?.signal });
  }

  /** Set your time zone, or turn the morning email on or off. */
  async updateSettings(
    input: DreamSettingsInput,
    opts: CommandOptions,
  ): Promise<DreamSettings> {
    return this.req("PATCH", "/v2/dream/settings", {
      body: input,
      extraHeaders: commandHeaders(opts),
      signal: opts.signal,
    });
  }

  /**
   * Turn the morning email off with the token from its unsubscribe link.
   * It needs no sign-in, and answers the same whether or not the token
   * matched.
   */
  async unsubscribe(
    token: string,
    opts?: { signal?: AbortSignal },
  ): Promise<{ unsubscribed: boolean }> {
    return this.req("POST", "/v2/dream/email:unsubscribe", {
      query: { token },
      signal: opts?.signal,
    });
  }
}

/** Options for changing your settings. */
export interface SettingsEditOptions {
  /** Required. Reuse it when you retry the same change. */
  idempotencyKey: string;
  /** Required: the settings' version you read (`version`, also the ETag). */
  ifMatch: number;
  signal?: AbortSignal;
}

/** `memax.v2.settings`: your notification settings, and how this Memax keeps your data. */
export class V2SettingsResource {
  constructor(private readonly req: RequestFn) {}

  /**
   * For each event, whether it reaches you by email (`email_sent` says
   * whether Memax sends that email today), your quiet hours in your time
   * zone, and the Review reminder's wait. The morning edition's email is
   * Dream's setting, the same one its unsubscribe link turns off.
   */
  async notifications(opts?: {
    signal?: AbortSignal;
  }): Promise<NotificationSettings> {
    return this.req("GET", "/v2/me/notifications", { signal: opts?.signal });
  }

  /**
   * Change what you send; the rest stays. A version that's no longer
   * current (the unsubscribe link, another tab) throws `edit_clash`
   * rather than undoing that change.
   */
  async updateNotifications(
    input: NotificationSettingsInput,
    opts: SettingsEditOptions,
  ): Promise<NotificationSettings> {
    return this.req("PATCH", "/v2/me/notifications", {
      body: input,
      extraHeaders: commandHeaders(opts, opts.ifMatch),
      signal: opts.signal,
    });
  }

  /**
   * What this Memax says about itself: what a Keep from this session
   * counts as, where data lives, every outside service that sees a
   * memory's words and what it keeps, and how long backups keep what
   * Forget removed. A space's seals are `receipts.checkpoints`.
   */
  async security(opts?: { signal?: AbortSignal }): Promise<Security> {
    return this.req("GET", "/v2/security", { signal: opts?.signal });
  }
}

/** Whether a Dream action can still be undone, as the server last said. */
export function undoableAction(a: DreamAction): boolean {
  return a.undoable && !a.undone;
}

/** `memax.v2`: the V2 record. */
export class V2Resource {
  readonly spaces: V2SpacesResource;
  readonly memories: V2MemoriesResource;
  readonly review: V2ReviewResource;
  readonly receipts: V2ReceiptsResource;
  readonly reads: V2ReadsResource;
  readonly agents: V2AgentsResource;
  readonly briefs: V2BriefsResource;
  readonly targets: V2TargetsResource;
  readonly gates: V2GatesResource;
  readonly notices: V2NoticesResource;
  readonly imports: V2ImportsResource;
  readonly devices: V2DevicesResource;
  readonly sessions: V2SessionsResource;
  readonly notes: V2NotesResource;
  readonly dream: V2DreamResource;
  readonly settings: V2SettingsResource;
  private readonly openStream?: OpenFn;

  constructor(req: RequestFn, open?: OpenFn) {
    this.spaces = new V2SpacesResource(req, open);
    this.memories = new V2MemoriesResource(req);
    this.review = new V2ReviewResource(req);
    this.receipts = new V2ReceiptsResource(req);
    this.reads = new V2ReadsResource(req);
    this.agents = new V2AgentsResource(req);
    this.briefs = new V2BriefsResource(req);
    this.targets = new V2TargetsResource(req);
    this.gates = new V2GatesResource(req);
    this.notices = new V2NoticesResource(req);
    this.imports = new V2ImportsResource(req);
    this.devices = new V2DevicesResource(req);
    this.sessions = new V2SessionsResource(req);
    this.notes = new V2NotesResource(req);
    this.dream = new V2DreamResource(req);
    this.settings = new V2SettingsResource(req);
    this.openStream = open;
  }

  /**
   * Ask a space a question: a short answer from its kept memories,
   * streamed, every sentence cited. Iterate the events as they arrive:
   *
   * ```ts
   * for await (const ev of memax.v2.ask("memax-v2", "Why River?", { signal })) {
   *   if (ev.event === "delta") process.stdout.write(ev.data.text);
   * }
   * ```
   *
   * The stream is one `sources` event (the memories the answer may cite),
   * then `delta` (words) and `cite` (`n`, `ref`) in answer order, then
   * `done` with the `outcome`, or `error` if the model failed partway.
   * Show `not_covered` and `unsupported` as "nothing kept answers that",
   * never as an answer. A refusal before the stream starts throws a
   * MemaxError (`refused` with policy `ask_by_person` or `ask_limit`; see
   * {@link refusalOf}). Aborting `signal`, or leaving the loop early,
   * closes the connection, and the server stops the model. To keep an
   * answer, `memories.remember` it with the cited refs as sources of kind
   * `memory`. Only a signed-in person may ask; API keys can't.
   */
  async *ask(
    space: string,
    question: string,
    opts?: AskOptions,
  ): AsyncGenerator<AskEvent> {
    if (!this.openStream) {
      throw new MemaxError(
        "This client can't open event streams.",
        "invalid_request",
        0,
      );
    }
    const body: AskInput = { question };
    const res = await this.openStream("POST", `/v2/spaces/${seg(space)}/ask`, {
      body,
      signal: opts?.signal,
    });
    if (!res.body) return;
    for await (const ev of readEventStream(res.body, opts?.signal)) {
      const parsed = askEventOf(ev.event, ev.data);
      if (parsed) yield parsed;
    }
    if (opts?.signal?.aborted) throw abortError(opts.signal);
  }
}

/** Options for {@link V2Resource.ask}. */
export interface AskOptions {
  /** Abort to stop the answer; the server stops its model too. */
  signal?: AbortSignal;
}

const ASK_EVENTS = new Set(["sources", "delta", "cite", "done", "error"]);

/**
 * An Ask event from a server-sent event, or undefined for one this SDK
 * doesn't know (a newer server's), which callers skip.
 */
export function askEventOf(event: string, data: string): AskEvent | undefined {
  if (!ASK_EVENTS.has(event)) return undefined;
  let parsed: unknown;
  try {
    parsed = JSON.parse(data);
  } catch {
    throw new MemaxError(
      `The answer's ${event} event isn't JSON.`,
      "invalid_response",
      200,
    );
  }
  return { event, data: parsed } as AskEvent;
}

function abortError(signal: AbortSignal): Error {
  const reason = (signal as { reason?: unknown }).reason;
  if (reason instanceof Error && reason.name === "AbortError") return reason;
  const err =
    typeof DOMException !== "undefined"
      ? new DOMException("Aborted", "AbortError")
      : Object.assign(new Error("Aborted"), { name: "AbortError" });
  return err;
}

/**
 * The memories that go with a Forget, from its 409 `forget_carries`, or
 * undefined for any other error. Show them, then forget again with their
 * refs in `carries`.
 */
export function forgetCarriesOf(err: unknown): ForgetCarry[] | undefined {
  if (!(err instanceof MemaxError) || err.code !== "forget_carries") {
    return undefined;
  }
  const carries = err.details?.carries;
  return Array.isArray(carries) ? (carries as ForgetCarry[]) : undefined;
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
