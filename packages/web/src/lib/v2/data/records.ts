/**
 * What Review, Memories and a memory's page share: who did something,
 * what they did, the compiled files a memory reaches, and what a
 * decision returns. Names follow the /v2 contract (openapi/v2.yaml)
 * where it has them; no words live here (the i18n catalogue builds
 * every sentence from these values).
 */
import type { TargetView } from "./targets";

/** Who a receipt names. */
export type Actor =
  /** `agent` is a Ledger registry key ("claude-code", "codex"). */
  | { kind: "agent"; agent: string }
  | { kind: "dream" }
  /**
   * A person. `self` is the signed-in viewer. Receipts don't carry
   * names yet, so another person has initials and a name only where
   * the source knows them (the demo).
   */
  | { kind: "person"; self: boolean; initials?: string; name?: string }
  | { kind: "memax" }
  | { kind: "repository" };

/** A receipt's verb (spec ReceiptAction, the memory-related ones). */
export type RecordAction =
  | "proposed"
  | "kept"
  | "edited"
  | "rejected"
  | "merged"
  | "flagged"
  | "resolved"
  | "verified"
  | "faded"
  | "restored"
  | "forgot"
  | "moved"
  | "compiled"
  | "handed_off"
  | "answered"
  | "undid";

/** A memory's displayed state (spec State, without the internal `rejected`). */
export type DisplayState =
  | "proposed"
  | "kept"
  | "merged"
  | "stale"
  | "conflict"
  | "faded"
  | "forgotten";

/** A receipt as a row's rail shows it: who, what, when. */
export interface RailReceipt {
  by: Actor | null;
  action: RecordAction;
  at: string;
}

/**
 * A compiled file: "Keeping recompiles" in Review, "Reaches" on a
 * memory. The compile targets' own view (targets.ts), so every screen
 * words a target's state the same way.
 */
export type TargetLine = TargetView;

/** What keep, reject and edit return. */
export interface DecisionResult {
  /**
   * The memory the command wrote: the same one, or, for an edit policy
   * sent to Review, the new proposal that supersedes it.
   */
  ref: string;
  outcome: "kept" | "rejected" | "edited" | "proposed";
  /** The memory's version after the command (its next If-Match). */
  version: number | null;
  /** Compiled files rewritten, when the source knows. */
  recompiled: number | null;
  /**
   * The command's receipt, which Undo addresses (spec: POST
   * /v2/receipts/{receipt}:undo, any of the command's receipts). Null or
   * absent when the command can't be undone.
   */
  receipt?: string | null;
  /**
   * Edit, then keep, saved but not kept (policy `judge_pending`): the new
   * words touch a decision in force, so they wait for the judge. `version`
   * is the saved one; a plain Keep of it finishes the decision.
   */
  judgePending?: boolean;
}
