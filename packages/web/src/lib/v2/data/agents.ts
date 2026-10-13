/**
 * The agents domain of the V2 data layer (epic 1.8): the Agents table,
 * one agent's page, Connect an agent, and Settings › Agents and keys.
 * LedgerDataSource extends AgentsData (source.ts); the SDK source
 * implements it in agents-sdk.ts and the demo in agents-demo.ts.
 *
 * Names follow the /v2 contract (openapi/v2.yaml › AgentConnection) where
 * it has them. `null` means "not served yet": the UI shows "—" or leaves
 * the line out, never a zero it doesn't know. No words live here.
 */
import type { SpaceKind, SpaceSummary } from "./types";

/** Read writes nothing; Propose sends writes to Review; Write keeps them, still receipted. */
export type Autonomy = "read" | "propose" | "write";
export const AUTONOMY_LEVELS: readonly Autonomy[] = [
  "read",
  "propose",
  "write",
];

export type AgentState = "active" | "paused" | "disconnected";
export type AgentSurface = "cli" | "ide" | "cloud" | "chat";
export type CredentialKind = "api_key" | "oauth_grant";
/** What the person sees on a compiled file. */
export type TargetStatus = "synced" | "drifted" | "pending" | "off";

/** What an agent may do, as the Connection panel and Settings list it. */
export type Capability = "read" | "propose" | "write" | "ask";

/** A connection's autonomy in one space, and its use there. */
export interface AgentSpaceView {
  spaceId: string;
  slug: string;
  name: string;
  kind: SpaceKind;
  autonomy: Autonomy;
  /** Its reads (R-) of this space in the last 7 days; null when the source can't count them. */
  reads7d: number | null;
  writes7d: number;
}

/** The compiled file an agent reads, when compile targets are served. */
export interface AgentTarget {
  path: string;
  status: TargetStatus;
  /** Another agent that reads the same file ("OpenCode"), as a registry key. */
  sharedWith?: string;
}

/** One agent connection: an agent working for a person through one credential. */
export interface AgentConnectionView {
  /** The connection's id: the agent page's URL segment and every command's target. */
  id: string;
  /** Ledger registry key for the stamp ("claude-code", "gemini"), or the name for an unknown agent. */
  agent: string;
  /** The connection's display name ("Codex"). */
  name: string;
  surface: AgentSurface;
  state: AgentState;
  credential: { kind: CredentialKind; active: boolean };
  /** The most its credential allows: an API key proposes at most. */
  maxAutonomy: Autonomy;
  /** It works for the viewer. Only then can they pause, resume or disconnect it. */
  mine: boolean;
  /** The OAuth client's metadata document URL, when it has one. */
  clientId: string | null;
  /** The spaces it's connected to, personal space first. */
  spaces: AgentSpaceView[];
  /** Its reads (R-, one per space read) in the last 7 days; null when the source can't count them. */
  reads7d: number | null;
  writes7d: number;
  lastSeenAt: string | null;
  connectedAt: string;
  /** Who connected it: the viewer, another person, or Memax carrying a V1 credential over. */
  connectedBy: "you" | "person" | "memax" | null;
  /** What it may do where the source knows more than its autonomy (the demo's "ask"). */
  may?: Capability[];
  /**
   * PLACEHOLDER: compile targets are built on another branch. Undefined
   * means "not served", null means it reads over MCP only.
   */
  target?: AgentTarget | null;
}

/** A memory the agent wrote, as Recent writes shows it. */
export interface AgentWrite {
  receiptId: string;
  /** "M-0431". */
  ref: string;
  /** The memory's words; receipts never hold them, so this is read from the memory. Null once forgotten or unread. */
  statement: string | null;
  state:
    | "proposed"
    | "kept"
    | "merged"
    | "stale"
    | "faded"
    | "conflict"
    | "forgotten";
  /** Who the receipt in the rail names: the agent, or the person who kept it. */
  actor: { agent?: string; person?: string };
  /** The receipt's past-tense verb ("proposed", "kept"). */
  action: string;
  at: string;
  /** A line under the statement. */
  note?:
    | { kind: "contradicts"; ref: string }
    | { kind: "proposed-in"; agent: string; session: string };
}

export interface AgentSessionView {
  ref: string;
  /** How the source labels it: a cloud task or a CLI session; plain when unknown. */
  kind?: "cloud" | "cli";
  /** The handoff it came from ("H-0093"). */
  handoff?: string;
  /** Still running. */
  live?: boolean;
  /** Its reads in that session (the last 30 days); null when the source can't count them. */
  reads: number | null;
  writes: number;
  lastAt: string;
}

/** What the agent did in the last 7 days. */
export interface AgentWeekView {
  reads: number | null;
  writes: number;
  proposals: number;
  kept: number;
  rejected: number;
  waiting: number;
  /** PLACEHOLDER (not in AgentWeek): questions it asked, and how many wait on the viewer. */
  questions: { asked: number; waiting: number } | null;
  /** PLACEHOLDER (not in AgentWeek): handoffs it received. */
  handoffsReceived: number | null;
  /** PLACEHOLDER (not in AgentWeek): writes from external content held for a person. */
  heldExternal: number | null;
}

/**
 * A cloud agent's latest counted session start (spec AgentStart): when,
 * the compiles its repository's files held, and the memories whose lines
 * it was told about. Refs only.
 */
export interface AgentStartView {
  at: string;
  spaceId: string;
  sessionRef: string | null;
  /** "C-0881": what the committed files held. */
  compiles: string[];
  /** The memories whose added or changed lines it was sent; empty when nothing was new. */
  told: string[];
}

export interface AgentDetailView {
  connection: AgentConnectionView;
  week: AgentWeekView;
  /** Newest first. */
  recentWrites: AgentWrite[];
  /** Most recent first. */
  sessions: AgentSessionView[];
  /** A cloud agent's latest start; null before its first; undefined for other agents. */
  lastStart?: AgentStartView | null;
}

/** The cloud agents Memax starts from a committed hook (cloud warm-start design §2). */
export const CLOUD_AGENTS = ["claude-code", "copilot", "codex"] as const;
export type CloudAgent = (typeof CLOUD_AGENTS)[number];

/** Codex Cloud runs no session-start hook yet: it starts from the committed AGENTS.md, with no key. */
export function hasCloudStart(agent: CloudAgent): boolean {
  return agent !== "codex";
}

/** A start key's secret, which the API answers once. */
export interface StartKeyView {
  secret: string;
  /** Its first characters ("mxk_7f2c"). */
  prefix: string;
  expiresAt: string;
}

/** A cloud agent's connection after it was made or given a new key, with the key once. */
export interface CloudAgentResultView {
  connection: AgentConnectionView;
  /** Null on a replay: the secret is never stored, so it's never answered twice. */
  key: StartKeyView | null;
}

/** Whether a connection is a cloud agent on a start key (it only starts sessions). */
export function isStartKeyAgent(agent: AgentConnectionView): boolean {
  return agent.surface === "cloud" && agent.credential.kind === "api_key";
}

/** An API key, as Settings › Agents and keys lists it. Keys read or propose, never keep or forget. */
export interface ApiKeyView {
  id: string;
  name: string;
  /** What the person may see of the key ("mxk_7f2c…"). */
  masked: string;
  /** The spaces it can reach, by id; null for every space of the person's. */
  spaceIds: string[] | null;
  may: "read" | "propose";
  createdAt: string;
  lastUsedAt: string | null;
}

/** Why a command on an agent didn't go through, normalised from the API's codes. */
export type AgentRefusal =
  /** Raising needs the person on the web (human_web), and Memax couldn't tell. */
  | "needs_web"
  /** An API key's agent proposes at most. */
  | "key_max_propose"
  /** Only the person it works for can raise it (owners can lower). */
  | "not_your_agent"
  /** Viewers can't raise, or Write needs someone who keeps in the space. */
  | "not_allowed"
  /** The person has connected as many kinds of agent as their plan has (D9). */
  | "agent_kind_limit"
  /** On a plan that doesn't bill seats only the owner lets an agent write. */
  | "seat_needed"
  /** Only a person changes what an agent may do. */
  | "person_must_manage"
  | "not_member"
  /** The web app's signature didn't verify. */
  | "surface_unverified"
  /** The agent is disconnected, or already in that state. */
  | "invalid_transition"
  | "not_found"
  /** Anything else: the network, the server. */
  | "failed";

export class AgentCommandError extends Error {
  constructor(
    readonly refusal: AgentRefusal,
    message?: string,
  ) {
    super(message ?? refusal);
    this.name = "AgentCommandError";
  }
}

export interface AgentCommand {
  /** The connection's id. */
  agent: string;
  /** One per intent; the same key on a retry (spec: Idempotency-Key). */
  idempotencyKey: string;
}

/** The agents part of LedgerDataSource. */
export interface AgentsData {
  /** Data on hand for the first render, so screenshots never catch a loading frame. The demo has it. */
  readonly agentsPeek?: {
    spaceAgents(slug: string): AgentConnectionView[] | undefined;
    agent(id: string): AgentDetailView | undefined;
    myAgents(): AgentConnectionView[];
    apiKeys(): ApiKeyView[];
  };
  /** Every agent connected to the space, whoever it works for, oldest first. */
  spaceAgents(
    space: SpaceSummary,
    signal?: AbortSignal,
  ): Promise<AgentConnectionView[]>;
  /** The viewer's own agent connections, across their spaces. */
  myAgents(signal?: AbortSignal): Promise<AgentConnectionView[]>;
  /** One agent with its week, recent writes and sessions. Null when it isn't in the viewer's spaces. */
  agent(id: string, signal?: AbortSignal): Promise<AgentDetailView | null>;
  /**
   * Sets what the agent may do in a space. Throws AgentCommandError: a
   * raise without a person on the web is `needs_web`.
   */
  setAutonomy(
    input: AgentCommand & { space: SpaceSummary; autonomy: Autonomy },
  ): Promise<AgentConnectionView>;
  pauseAgent(input: AgentCommand): Promise<AgentConnectionView>;
  /** Resuming is raising: it needs the person on the web. */
  resumeAgent(input: AgentCommand): Promise<AgentConnectionView>;
  /** Ends the connection and revokes its credential in the same step. */
  disconnectAgent(input: AgentCommand): Promise<AgentConnectionView>;
  /**
   * Connects a cloud agent to one project or team space at Read, with a
   * start key that only starts its sessions there. The secret comes back
   * once. Throws AgentCommandError.
   */
  connectCloudAgent(input: {
    agent: CloudAgent;
    space: SpaceSummary;
    idempotencyKey: string;
  }): Promise<CloudAgentResultView>;
  /** A new start key under the same connection; the old one stops at once. */
  rekeyAgent(input: AgentCommand): Promise<CloudAgentResultView>;
  /** The autonomy a new agent starts at in the space (its rules). */
  newAgentAutonomy(space: SpaceSummary): Autonomy;
  /**
   * The compiled file an agent of this kind (a registry key) would read
   * in the space, for Connect an agent. PLACEHOLDER: undefined until
   * compile targets are served; null when it reads over MCP only.
   */
  targetFor(space: SpaceSummary, agent: string): AgentTarget | null | undefined;
  apiKeys(signal?: AbortSignal): Promise<ApiKeyView[]>;
  /** Creates a key for one space. The secret is returned once. */
  createApiKey(input: {
    name: string;
    may: "read" | "propose";
    space: SpaceSummary;
  }): Promise<{ key: ApiKeyView; secret: string }>;
  revokeApiKey(id: string): Promise<void>;
}

/** Whether moving from one level to another gives the agent more to do. */
export function isRaise(from: Autonomy, to: Autonomy): boolean {
  return AUTONOMY_LEVELS.indexOf(to) > AUTONOMY_LEVELS.indexOf(from);
}

/** The connection's level in a space, or undefined when it isn't connected there. */
export function autonomyIn(
  agent: AgentConnectionView,
  slug: string,
): AgentSpaceView | undefined {
  return agent.spaces.find((s) => s.slug === slug);
}

/** What it may do in a space, as capability words: the source's own list, or its autonomy. */
export function capabilities(
  agent: AgentConnectionView,
  autonomy: Autonomy,
): Capability[] {
  if (agent.may) return agent.may;
  return autonomy === "read" ? ["read"] : ["read", autonomy];
}
