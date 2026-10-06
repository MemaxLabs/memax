import { MemaxError, refusalOf } from "memax-sdk";
import { GATE_ENDS, type GateEnd } from "./gates";

/**
 * Why a command (keep, reject, edit) didn't go through, normalised from
 * whatever the source threw, so Review and a memory's page can say what
 * happened and what to do (States board: "say what happened, what was
 * kept and what to do"). The words are the catalogue's
 * (records-copy.ts), keyed by these kinds and the policy code.
 */
export type CommandFailure =
  /** 403 `refused`: policy said no. `code` is spec PolicyCode. */
  | { kind: "refused"; code: string | null; message: string | null }
  /** 412 `edit_clash`: the memory changed since the person read it. */
  | { kind: "clash"; currentVersion: number | null }
  /**
   * 409 `invalid_transition`: already decided, elsewhere or just now. A
   * decision gate that ended says how (`details.status`).
   */
  | { kind: "decided"; status?: GateEnd }
  | { kind: "not-found" }
  | { kind: "rate-limited"; retryAfter: number | null }
  /**
   * 503: Memax is still checking a proposal that touches a decision in
   * force (`judge_pending`: Keep before the judge, for at most 30 s), or
   * another change holds the memory (`busy`). Either way, retry after
   * `retryAfter` seconds with the same key.
   */
  | {
      kind: "busy";
      retryAfter: number | null;
      ref: string | null;
      /** `judge_pending` rather than another change holding it. */
      judge: boolean;
    }
  /**
   * 409 `in_conflict`: the judge flagged the proposal as contradicting a
   * decision in force (`with`), so it is settled, not kept.
   */
  | { kind: "in-conflict"; with: string | null }
  /** 409 `undo_refused`: why an undo can't go through, and what's in its way (`ref`). */
  | { kind: "undo-refused"; reason: UndoRefusal; ref: string | null }
  /** The request didn't reach the server, or the server had a moment. Safe to retry with the same key. */
  | { kind: "unreachable" }
  /** The source can't do this yet (no /v2 endpoint): a PLACEHOLDER. */
  | { kind: "unavailable" }
  /** Anything else (a bad request, a reused key). Retrying needs a new key. */
  | { kind: "unknown"; message: string | null };

/** Why an undo was refused (spec UndoRefusal). */
export type UndoRefusal =
  | "window_passed"
  | "already_undone"
  | "not_undoable"
  | "later_changes";

const UNDO_REFUSALS = new Set<string>([
  "window_passed",
  "already_undone",
  "not_undoable",
  "later_changes",
]);

/** Thrown by sources that aren't the SDK (the demo, placeholders). */
export class CommandFailedError extends Error {
  constructor(readonly failure: CommandFailure) {
    super(`command failed: ${failure.kind}`);
    this.name = "CommandFailedError";
  }
}

const UNREACHABLE = new Set([
  "network_error",
  "internal_error",
  "unavailable",
  "invalid_response",
]);

function detail(err: MemaxError, key: string): unknown {
  return err.details?.[key];
}

export function toFailure(err: unknown): CommandFailure {
  if (err instanceof CommandFailedError) return err.failure;
  if (err instanceof MemaxError) {
    if (err.code === "refused") {
      const policy = refusalOf(err);
      return {
        kind: "refused",
        code: policy?.code ?? null,
        message: policy?.message ?? err.message ?? null,
      };
    }
    if (err.code === "edit_clash" || err.status === 412) {
      const current = err.details?.current_version;
      return {
        kind: "clash",
        currentVersion: typeof current === "number" ? current : null,
      };
    }
    if (err.code === "undo_refused") {
      const reason = detail(err, "reason");
      const ref = detail(err, "ref");
      return {
        kind: "undo-refused",
        reason:
          typeof reason === "string" && UNDO_REFUSALS.has(reason)
            ? (reason as UndoRefusal)
            : "not_undoable",
        ref: typeof ref === "string" && ref ? ref : null,
      };
    }
    if (err.code === "busy" || err.code === "judge_pending") {
      const seconds = detail(err, "retry_after");
      const ref = detail(err, "ref");
      return {
        kind: "busy",
        retryAfter:
          err.retryAfterSeconds ??
          (typeof seconds === "number" ? seconds : null),
        ref: typeof ref === "string" && ref ? ref : null,
        judge: err.code === "judge_pending",
      };
    }
    if (err.code === "in_conflict") {
      const ref = detail(err, "ref");
      return {
        kind: "in-conflict",
        with: typeof ref === "string" && ref ? ref : null,
      };
    }
    if (err.code === "invalid_transition") {
      const status = detail(err, "status");
      return GATE_ENDS.includes(status as GateEnd)
        ? { kind: "decided", status: status as GateEnd }
        : { kind: "decided" };
    }
    if (err.code === "not_found" || err.status === 404) {
      return { kind: "not-found" };
    }
    if (err.isRateLimited) {
      return {
        kind: "rate-limited",
        retryAfter: err.retryAfterSeconds ?? null,
      };
    }
    if (UNREACHABLE.has(err.code) || err.status === 0 || err.status >= 500) {
      return { kind: "unreachable" };
    }
    return { kind: "unknown", message: err.message || null };
  }
  if (err instanceof TypeError) return { kind: "unreachable" };
  return {
    kind: "unknown",
    message: err instanceof Error ? err.message : null,
  };
}

/**
 * Whether trying again is the same command: the same idempotency key
 * is reused, so the server applies it at most once.
 */
export function isRetryable(failure: CommandFailure): boolean {
  return (
    failure.kind === "unreachable" ||
    failure.kind === "rate-limited" ||
    failure.kind === "busy"
  );
}
