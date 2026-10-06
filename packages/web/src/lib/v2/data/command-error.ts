import { MemaxError, refusalOf } from "memax-sdk";

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
  /** 409 `invalid_transition`: already decided, elsewhere or just now. */
  | { kind: "decided" }
  | { kind: "not-found" }
  | { kind: "rate-limited"; retryAfter: number | null }
  /** The request didn't reach the server, or the server had a moment. Safe to retry with the same key. */
  | { kind: "unreachable" }
  /** The source can't do this yet (no /v2 endpoint): a PLACEHOLDER. */
  | { kind: "unavailable" }
  /** Anything else (a bad request, a reused key). Retrying needs a new key. */
  | { kind: "unknown"; message: string | null };

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
  "busy",
  "unavailable",
  "invalid_response",
]);

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
    if (err.code === "invalid_transition") return { kind: "decided" };
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
  return failure.kind === "unreachable" || failure.kind === "rate-limited";
}
