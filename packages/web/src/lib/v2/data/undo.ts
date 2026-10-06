/**
 * Undo (Review's ⌘Z, the decision toasts' Undo, and Undo on one of the
 * judge's folds). The server addresses an undo by receipt, because a
 * resolution or a fold changes two memories and a receipt names exactly
 * one command (spec: POST /v2/receipts/{receipt}:undo). Part of
 * LedgerDataSource (source.ts); sdk-source.ts and the demo
 * (demo-records.ts) implement it.
 *
 * What can be undone, and for how long (server internal/ledger/undo.go):
 * a person's own Keep, Reject, Edit (with or without Keep) and conflict
 * resolution for 10 minutes; one of the judge's folds, by anyone who may
 * keep, for 14 days. Forget never. A refusal throws: 409 `undo_refused`
 * (CommandFailure `undo-refused`, with the reason) or 403 `refused` with
 * policy code `undo_by_decider`.
 */
import type { SpaceSummary } from "./types";

/** The command an undo reverses. `fold` is the judge's; the rest are a person's own. */
export type UndoCommand = "keep" | "reject" | "edit" | "resolve" | "fold";

/** How long the server lets each be undone. */
export const UNDO_WINDOW_MS = 10 * 60 * 1000;
export const FOLD_UNDO_WINDOW_MS = 14 * 24 * 60 * 60 * 1000;

export function undoWindowMs(command: UndoCommand): number {
  return command === "fold" ? FOLD_UNDO_WINDOW_MS : UNDO_WINDOW_MS;
}

export interface UndoResult {
  /** Every memory the undo put back, by display ID, the one named first. */
  refs: string[];
}

export interface UndoSource {
  undo(input: {
    space: SpaceSummary;
    /** Any of the command's receipts. */
    receipt: string;
    /** One per undo, reused across its retries. */
    idempotencyKey: string;
  }): Promise<UndoResult>;
}
