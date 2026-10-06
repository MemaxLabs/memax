/**
 * Decision gates (G-, epic 1.11; HANDOFF "Decision gate"): a question an
 * agent asked a person in a space, with two to four options. A person
 * answers with one option, and the answer is kept, in the same command,
 * as a decision that person authored; the agent hears how it ended on
 * its next read. Part of LedgerDataSource (source.ts) as `source.gates`;
 * gates-sdk.ts (memax.v2.gates) and gates-demo.ts implement it.
 *
 * Review lists the waiting ones first and answers them; Today lists them
 * in "Waiting on you"; Activity links their receipts here. No words live
 * here: the catalogue builds every sentence from these values.
 */
import type { Actor } from "./records";
import type { SpaceSummary } from "./types";

/** Spec GateStatus. `expired` is never stored: a waiting gate past its `expiresAt` reads as expired. */
export type GateStatus = "waiting" | "answered" | "withdrawn" | "expired";
/** How a gate ended. */
export type GateEnd = Exclude<GateStatus, "waiting">;

export const GATE_ENDS: readonly GateEnd[] = [
  "answered",
  "withdrawn",
  "expired",
];

export interface GateOptionView {
  label: string;
  /** What choosing it means, in the agent's words. */
  detail: string | null;
}

/** How a person answered, and the decision it became. */
export interface GateAnswerView {
  /** The chosen option, counting from 0. */
  option: number;
  label: string;
  /** The kept decision's display ID ("M-0447"). */
  memory: string;
  by: Actor | null;
  at: string;
}

export interface GateView {
  id: string;
  /** Display ID ("G-0012"); commands address it with the space. */
  ref: string;
  /** The gate's version: the If-Match of an answer or a withdrawal. It changes only when the gate ends. */
  version: number;
  question: string;
  context: string | null;
  options: GateOptionView[];
  /** As the server last said. Read it through gateStatusAt, which knows the clock. */
  status: GateStatus;
  /** The agent that asked: a Ledger registry key ("codex"). */
  agent: string;
  /** The session it asked from ("9f1c"). */
  session: string | null;
  askedAt: string;
  expiresAt: string;
  /**
   * D15: decisions in this space need a person on the web, so only the
   * web app answers it (the CLI and in-agent answers are refused).
   */
  needsWeb: boolean;
  answer: GateAnswerView | null;
  /** Who took the question back: the agent that asked, or a person. */
  withdrawn: { by: Actor | null; at: string } | null;
}

/** What an answer returns: the gate, ended, and the kept decision it became. */
export interface GateAnswerResult {
  gate: GateView;
  /** The decision's display ID and version. */
  memory: { ref: string; version: number };
  /** Compiled files rewritten, when the source knows. */
  recompiled: number | null;
}

/**
 * Whether this session answers as a person on the web (assurance
 * human_web, D15), as far as the frame can tell before anything is sent:
 * `true` when the session was issued to the web app (the token's
 * `surface` claim), `false` when it wasn't (a CLI login, a session from
 * before migration 030), `null` when the frame can't tell. Only the API
 * decides; this lets Review say so before the person tries.
 */
export type WebSession = boolean | null;

export interface GatesSource {
  /** The demo has every space's gates on hand, so screenshots never catch a loading frame. */
  peekWaiting?(slug: string): GateView[] | undefined;
  /** The space's waiting gates, the soonest to expire first. */
  waiting(input: {
    space: SpaceSummary;
    signal?: AbortSignal;
  }): Promise<GateView[]>;
  /** One gate however it ended, by display ID or id; null when the space has none by that ref. */
  get(input: {
    space: SpaceSummary;
    ref: string;
    signal?: AbortSignal;
  }): Promise<GateView | null>;
  /**
   * Answers with one option (from 0), with the gate's version as If-Match.
   * Refusals throw CommandFailure-shaped errors (command-error.ts): 403
   * `refused` (`decision_needs_web` under D15, `viewer`, `owners_keep`),
   * and 409 `invalid_transition` with how it ended (`decided`, `status`).
   */
  answer(input: {
    space: SpaceSummary;
    gate: GateView;
    option: number;
    idempotencyKey: string;
  }): Promise<GateAnswerResult>;
  /** Takes the question back. The agent hears it on its next read. */
  withdraw(input: {
    space: SpaceSummary;
    gate: GateView;
    idempotencyKey: string;
  }): Promise<GateView>;
  /** What the frame knows about this session's assurance (see WebSession). */
  webSession(): WebSession;
}

/** A gate's status at `now`: a waiting gate past its expiry has expired. */
export function gateStatusAt(gate: GateView, now: Date): GateStatus {
  if (gate.status !== "waiting") return gate.status;
  return Date.parse(gate.expiresAt) <= now.getTime() ? "expired" : "waiting";
}

/** Review's order for gates: the soonest to expire first, then the oldest asked. */
export function byExpiry(a: GateView, b: GateView): number {
  return (
    Date.parse(a.expiresAt) - Date.parse(b.expiresAt) ||
    Date.parse(a.askedAt) - Date.parse(b.askedAt)
  );
}

/** The ones still waiting at `now`, in Review's order (byExpiry). */
export function waitingGates(
  gates: readonly GateView[],
  now: Date,
): GateView[] {
  return gates.filter((g) => gateStatusAt(g, now) === "waiting").sort(byExpiry);
}

/** The agents waiting on these questions (registry keys), once each, in order. */
export function askingAgents(gates: readonly GateView[]): string[] {
  return [...new Set(gates.map((g) => g.agent))];
}

/** A gate's display ID, as the API addresses one (spec GateRef): "G-0012". */
export function isGateRef(ref: string): boolean {
  return /^G-\d{1,18}$/.test(ref);
}

/** Whether an answer can be sent from here: needs the web, and the frame knows this session isn't. */
export function answerNeedsWebHere(gate: GateView, web: WebSession): boolean {
  return gate.needsWeb && web === false;
}
