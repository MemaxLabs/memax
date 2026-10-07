/**
 * Changing what an agent may do (plan §5.6, Phase 0 log "agent
 * connections"): the rules the control shows before anything is sent,
 * and the state machine behind it. Pure, so both are unit-tested.
 *
 * The server decides (policy.DecideConnection); the client only avoids
 * offering what it knows will be refused, and says why:
 * - lowering is always allowed for your own agent, and an owner may
 *   lower anyone's agent in the space;
 * - raising needs the person on the web (human_web), which only the
 *   server can tell, so a raise waits for its answer and rolls back on
 *   a refusal;
 * - a viewer can only lower their own agent;
 * - an API key's agent proposes at most (`max_autonomy`), and a
 *   credential without write access only reads.
 */
import {
  AUTONOMY_LEVELS,
  isRaise,
  type AgentConnectionView,
  type Autonomy,
} from "../data/agents";
import type { SpaceRole } from "../data/types";

/** Why a level can't be chosen; the screens word it (t.ledger.agents.unavailable). */
export type Unavailable =
  | "disconnected"
  | "revoked"
  | "keyMaxPropose"
  | "readOnlyCredential"
  | "notYoursRaise"
  | "notYours"
  | "viewer";

/** The levels the viewer can't choose for this agent in a space, each with why. */
export function unavailableLevels(
  agent: AgentConnectionView,
  current: Autonomy,
  role: SpaceRole,
): Partial<Record<Autonomy, Unavailable>> {
  const out: Partial<Record<Autonomy, Unavailable>> = {};
  const every = (reason: Unavailable, keepCurrent = false) => {
    for (const level of AUTONOMY_LEVELS) {
      if (keepCurrent && level === current) continue;
      out[level] = reason;
    }
    return out;
  };
  if (agent.state === "disconnected") return every("disconnected");
  if (!agent.credential.active) return every("revoked");
  if (!agent.mine) {
    if (role !== "owner") return every("notYours", true);
    for (const level of AUTONOMY_LEVELS) {
      if (isRaise(current, level)) out[level] = "notYoursRaise";
    }
    return out;
  }
  for (const level of AUTONOMY_LEVELS) {
    if (!isRaise(current, level)) continue;
    if (isRaise(agent.maxAutonomy, level)) {
      out[level] =
        agent.credential.kind === "api_key"
          ? "keyMaxPropose"
          : "readOnlyCredential";
    } else if (role === "viewer") {
      out[level] = "viewer";
    }
  }
  return out;
}

export interface AutonomyState {
  /** The level the server last confirmed. */
  confirmed: Autonomy;
  /** What the control shows: the person's latest choice. */
  shown: Autonomy;
  /** The command on its way, if any. */
  inflight: { to: Autonomy; key: string; kind: "raise" | "lower" } | null;
}

export type AutonomyEvent =
  /** The person picked a level (a click, or an arrow key). */
  | { type: "choose"; to: Autonomy }
  /** Send the latest choice, if it differs and nothing is on its way. */
  | { type: "send"; key: string }
  | { type: "settled"; key: string; autonomy: Autonomy }
  /** Refused or failed: back to what the server has. */
  | { type: "rolledBack"; key: string }
  /** The server's data changed underneath (a refetch). */
  | { type: "sync"; confirmed: Autonomy };

export function initialAutonomy(confirmed: Autonomy): AutonomyState {
  return { confirmed, shown: confirmed, inflight: null };
}

/**
 * One command at a time, latest choice wins. Arrowing from Read to
 * Write passes Propose; the caller sends only once the choices settle
 * (a short pause), and a choice made while a command is on its way is
 * sent after it. A refusal drops back to the confirmed level, dropping
 * any later choice with it, so the control never shows a level the
 * server hasn't accepted.
 */
export function autonomyReducer(
  state: AutonomyState,
  event: AutonomyEvent,
): AutonomyState {
  switch (event.type) {
    case "choose":
      return event.to === state.shown ? state : { ...state, shown: event.to };
    case "send":
      if (state.inflight || state.shown === state.confirmed) return state;
      return {
        ...state,
        inflight: {
          to: state.shown,
          key: event.key,
          kind: isRaise(state.confirmed, state.shown) ? "raise" : "lower",
        },
      };
    case "settled":
      if (state.inflight?.key !== event.key) return state;
      return {
        confirmed: event.autonomy,
        shown: state.shown === state.inflight.to ? event.autonomy : state.shown,
        inflight: null,
      };
    case "rolledBack":
      if (state.inflight?.key !== event.key) return state;
      return { ...state, shown: state.confirmed, inflight: null };
    case "sync":
      if (state.inflight || event.confirmed === state.confirmed) return state;
      return {
        confirmed: event.confirmed,
        shown: state.shown === state.confirmed ? event.confirmed : state.shown,
        inflight: null,
      };
  }
}

/** Whether a choice is waiting to be sent. */
export function hasUnsent(state: AutonomyState): boolean {
  return !state.inflight && state.shown !== state.confirmed;
}

/** A raise is waiting on the server: the control says it's busy, and nothing is assumed. */
export function isRaising(state: AutonomyState): boolean {
  return (
    state.inflight?.kind === "raise" ||
    (!state.inflight && isRaise(state.confirmed, state.shown))
  );
}
