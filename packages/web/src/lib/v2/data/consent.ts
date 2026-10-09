/**
 * OAuthConsent (plan 25 §5.15, epic 2.3): a person lets an outside agent
 * (an MCP client) connect to one of their spaces. GET /oauth/authorize on
 * the API sends the browser here with the request's ID; the person signs
 * in on the web (any method) and their session opens and answers the
 * request through the web app's proxy. The first person to open a request
 * is bound to it. The server says, for each space, what the agent will and
 * won't be able to do there (from policy), and answers a decision with the
 * URL to send the browser to: the client's registered redirect_uri, which
 * nothing here builds. No words live here.
 */
import {
  MemaxError,
  type OAuthAutonomy,
  type OAuthRequest,
  type OAuthRequestSpace,
} from "memax-sdk";
import { acceptedRedirect } from "@/lib/oauth-redirects";
import { TARGET_READERS, type TargetKind } from "./targets";

/**
 * An ability the server says the agent has (can) or lacks (cannot) in a
 * space (packages/server/internal/handler/mcp_oauth_consent.go).
 */
export type ConsentAbility =
  | "read_brief"
  | "read_memories"
  | "propose"
  | "keep"
  | "add"
  | "gate"
  | "forget"
  | "other_spaces";

const ABILITIES: ReadonlySet<string> = new Set<ConsentAbility>([
  "read_brief",
  "read_memories",
  "propose",
  "keep",
  "add",
  "gate",
  "forget",
  "other_spaces",
]);

export type ConsentSpaceKind = "personal" | "project" | "team";

export interface ConsentSpaceView {
  /** The space the decision names. */
  id: string;
  name: string;
  kind: ConsentSpaceKind;
  /** On the V2 record; false for a space still on V1. */
  onV2: boolean;
  /** The person's role can't use what the agent asked for. */
  disabled: boolean;
  /** Kept memories (V2), or V1's memory count; null when unknown. */
  memories: number | null;
  people: number | null;
  /** The file this agent reads here, else the canonical one. */
  compiles: string | null;
  /** The level the agent is connected at (V2); null on V1. */
  autonomy: OAuthAutonomy | null;
  /** The most a person may later allow it, in Agents (V2); null on V1. */
  ceiling: OAuthAutonomy | null;
  can: ConsentAbility[];
  cannot: ConsentAbility[];
}

export interface ConsentRequestView {
  requestId: string;
  client: {
    /** As the client names itself; never trusted as anything but text. */
    name: string;
    /** The Ledger stamp's key (AGENTS), or the client's own slug. */
    agent: string;
    /** For a metadata-document client, the host Memax fetched it from. */
    host: string | null;
  };
  /** Who the request is bound to: the person signed in here. */
  person: string;
  /** V2 first, then project, team, personal; by name. */
  spaces: ConsentSpaceView[];
  /** Seconds left when it was read. */
  expiresIn: number;
}

/** Why the request can't be shown. */
export type ConsentEnding =
  /** The link has no request in it. */
  | "missing"
  /** It lasted its 10 minutes. */
  | "expired"
  /** Answered already, an old link, or someone else's. */
  | "gone"
  /** This session can't answer (not one the web app was issued). */
  | "refused"
  /** It didn't load; trying again may work. */
  | "failed"
  /** Answered, and the browser handed the answer to a native app. */
  | "handed";

export class ConsentLoadError extends Error {
  constructor(readonly ending: ConsentEnding) {
    super(ending);
    this.name = "ConsentLoadError";
  }
}

/** Why a decision didn't go through. */
export type ConsentRefusal =
  /** The space can't be connected from this account. */
  | "space"
  /** The request ended meanwhile (consentEnding says how). */
  | "ended"
  | "failed";

export class ConsentDecisionError extends Error {
  constructor(
    readonly refusal: ConsentRefusal,
    readonly ending: ConsentEnding | null = null,
  ) {
    super(refusal);
    this.name = "ConsentDecisionError";
  }
}

export interface ConsentSource {
  load(requestId: string): Promise<ConsentRequestView>;
  /** The URL to send the browser to: the client's redirect_uri. */
  decide(
    requestId: string,
    decision: { decision: "approve"; spaceId: string } | { decision: "deny" },
  ): Promise<string>;
  /** "Not you?": lets go of the request. */
  release(requestId: string): Promise<void>;
}

/** What a client named itself, for display: no control or bidi characters, one line. */
export function cleanClientName(raw: string): string {
  return raw
    .replace(/[\p{Cc}\p{Cf}\p{Zl}\p{Zp}]/gu, "")
    .replace(/\s+/g, " ")
    .trim();
}

/**
 * A name cut to `max` characters with an ellipsis, for buttons and lines
 * that can't wrap: at a word break when one falls in the second half.
 */
export function shortName(name: string, max = 32): string {
  const chars = Array.from(name);
  if (chars.length <= max) return name;
  const cut = chars.slice(0, max - 1).join("");
  const space = cut.lastIndexOf(" ");
  const head = space >= cut.length / 2 ? cut.slice(0, space) : cut;
  return `${head.trimEnd()}…`;
}

/** The V1 agent slug as the Ledger's stamp registry knows it. */
export function stampAgent(slug: string): string {
  return slug === "claude-ai" ? "claude" : slug;
}

const FILE_LABELS: Record<string, string> = {
  agents_md: "AGENTS.md",
  claude_md: "CLAUDE.md",
  gemini_md: "GEMINI.md",
};

/**
 * The file a space compiles that this agent reads natively (its own kind
 * before the canonical AGENTS.md, as TARGET_READERS has them), else the
 * canonical file, else the first. ChatGPT's copy-out text is no file.
 */
export function compiledFor(
  agent: string,
  targets: NonNullable<OAuthRequestSpace["targets"]>,
): string | null {
  const files = targets.filter((t) => t.kind !== "chatgpt");
  const mine = (Object.keys(TARGET_READERS) as TargetKind[])
    .filter((kind) => TARGET_READERS[kind].includes(agent))
    .sort((a, b) => Number(a === "agents_md") - Number(b === "agents_md"));
  const pick =
    mine
      .map((kind) => files.find((t) => t.kind === kind))
      .find((t) => t !== undefined) ??
    files.find((t) => t.kind === "agents_md") ??
    files[0];
  if (!pick) return null;
  return pick.path || FILE_LABELS[pick.kind] || null;
}

function abilities(values: string[] | undefined): ConsentAbility[] {
  return (values ?? []).filter((v): v is ConsentAbility => ABILITIES.has(v));
}

const KIND_ORDER: Record<ConsentSpaceKind, number> = {
  project: 0,
  team: 1,
  personal: 2,
};

export function toConsentSpace(
  space: OAuthRequestSpace,
  agent: string,
): ConsentSpaceView {
  return {
    id: space.id,
    name: space.name,
    kind: space.kind,
    onV2: space.on_v2,
    disabled: space.disabled,
    memories: space.memories ?? null,
    people: space.people,
    compiles: space.on_v2 ? compiledFor(agent, space.targets ?? []) : null,
    autonomy: space.on_v2 ? (space.autonomy ?? null) : null,
    ceiling: space.on_v2 ? (space.ceiling ?? null) : null,
    can: abilities(space.can),
    cannot: abilities(space.cannot),
  };
}

export function toConsentRequest(r: OAuthRequest): ConsentRequestView {
  const agent = stampAgent(r.agent_name);
  const spaces = r.spaces
    .map((space) => toConsentSpace(space, agent))
    .sort(
      (a, b) =>
        Number(b.onV2) - Number(a.onV2) ||
        KIND_ORDER[a.kind] - KIND_ORDER[b.kind] ||
        a.name.localeCompare(b.name),
    );
  return {
    requestId: r.request_id,
    client: {
      name: cleanClientName(r.client_name),
      agent,
      host: r.client_host ?? null,
    },
    person: r.person.name,
    spaces,
    expiresIn: r.expires_in,
  };
}

/** How the API's refusal of a request reads on the page. */
export function consentEnding(err: unknown): ConsentEnding {
  if (err instanceof ConsentLoadError) return err.ending;
  if (!(err instanceof MemaxError)) return "failed";
  if (err.status === 410 || err.code === "consent_request_expired") {
    return "expired";
  }
  if (err.status === 404 || err.code === "consent_request_not_found") {
    return "gone";
  }
  if (err.code === "consent_by_person_on_web") return "refused";
  return "failed";
}

/** How the API's refusal of a decision reads on the page. */
export function consentRefusal(err: unknown): ConsentDecisionError {
  if (err instanceof ConsentDecisionError) return err;
  if (err instanceof MemaxError && err.code === "consent_space") {
    return new ConsentDecisionError("space");
  }
  const ending = consentEnding(err);
  return ending === "failed"
    ? new ConsentDecisionError("failed")
    : new ConsentDecisionError("ended", ending);
}

/**
 * Whether to follow the URL the server answered: it built it from the
 * client's registered redirect_uri, and the page follows it only when it
 * passes the same rules the server registered it under (https, loopback
 * http, a native app's private scheme; never javascript:, data: and the
 * like). A browser hands a native app's scheme to the app.
 */
export function followable(url: string): boolean {
  return acceptedRedirect(url);
}
