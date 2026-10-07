/**
 * "Switch to V2" (plan §10, epic 2.8): a space still on V1 moves to the
 * V2 record. Its V1 memories become notes (N-), nothing lost; the ones the
 * person wrote, one statement each, wait in Review as one import (origin
 * `v1`, ReviewImport "From V1"); what agents wrote goes to Dream; its
 * agent files become compile targets; its agents connect at Propose. No
 * V1 row changes, so switching back gives V1 exactly as it was. Part of
 * LedgerDataSource (source.ts) as `source.switch`; switch-sdk.ts
 * (memax.v2.spaces) and switch-demo.ts implement it.
 *
 * No words live here: the page words everything from the catalogue
 * (today-en.ts `switch`, today-zh.ts).
 */
import type { SpaceKind, SpaceRole, SpaceSummary } from "./types";

/** v1: never switched. running: switching in the background. off: switched back. */
export type SwitchState = "v1" | "running" | "switched" | "failed" | "off";

/** The step a switch stands at (the next to run), or done (spec SwitchStep). */
export type SwitchStep =
  | "space"
  | "notes"
  | "personas"
  | "configs"
  | "candidates"
  | "agents"
  | "gates"
  | "switch"
  | "done";

/** What the space's V1 memories become (spec SwitchNotes). */
export interface SwitchNotesView {
  total: number;
  /** A person's own, one statement each: offered to keep in one go. */
  candidates: number;
  /** What agents wrote, and longer notes: Dream folds them into proposals. */
  fold: number;
  /** Notes only, never proposed. */
  kept: number;
  archived: number;
  secret: number;
}

export interface SwitchMemberView {
  name: string;
  /** owner, admin, contributor or viewer. */
  v1Role: string;
  role: SpaceRole;
  canForget: boolean;
}

export interface SwitchAgentView {
  name: string;
  /** A registry key ("claude-code"). */
  agent: string;
  autonomy: "read" | "propose" | "write";
  /** Connected already: it keeps its level. */
  connected: boolean;
}

/** What switching moves, read fresh from V1. Nothing changes. */
export interface SwitchPreviewView {
  kind: SpaceKind;
  /** The kinds it may switch as: a V1 team hub may become a project space. */
  kinds: SpaceKind[];
  repository: string | null;
  suggestedRepository: string | null;
  members: SwitchMemberView[];
  notes: SwitchNotesView;
  personas: number;
  /** Agent files whose two-way sync stops. */
  configs: number;
  /** The compile targets they stand for (spec TargetKind). */
  targets: string[];
  agents: SwitchAgentView[];
  /** Decisions waiting on V1's board. */
  gates: number;
  /** V1 Dream runs, kept as read-only history. */
  dreamRuns: number;
  plan: string | null;
}

/** What the switch's steps did so far. */
export interface SwitchProgressView {
  notes: number;
  /** A person's own V1 memories proposed for keeping in one go. */
  proposed: number;
  targets: string[];
  connected: number;
  notified: number;
}

/** Where a space's switch stands. */
export interface SwitchView {
  state: SwitchState;
  step: SwitchStep;
  preview: SwitchPreviewView;
  progress: SwitchProgressView;
  /** The V1 import, for ReviewImport "From V1". */
  importId: string | null;
  /** Why a step failed (a code). */
  error: string | null;
  switchedAt: string | null;
}

export interface SwitchSource {
  /** Where the space's switch stands, with a fresh preview. */
  status(input: {
    space: SpaceSummary;
    signal?: AbortSignal;
  }): Promise<SwitchView>;
  /**
   * Switches the space (its owner only; resumes a failed switch). While
   * it runs in the background the state is `running`: read `status`
   * again. Refusals throw CommandFailure-shaped errors (command-error.ts).
   */
  toV2(input: {
    space: SpaceSummary;
    /** A V1 team hub may switch as a project space. */
    kind?: "project" | "team";
    idempotencyKey: string;
  }): Promise<SwitchView>;
  /** Switches it back to V1: every surface serves it as before. */
  toV1(input: {
    space: SpaceSummary;
    idempotencyKey: string;
  }): Promise<SwitchView>;
}

/** Whether the page should keep reading the status: the switch is running. */
export function switchRunning(view: SwitchView | undefined): boolean {
  return view?.state === "running";
}
