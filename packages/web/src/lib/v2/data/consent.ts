/**
 * OAuthConsent (plan 25 §5.15, epic 2.3): a person lets an outside agent
 * (an MCP client) connect to one of their spaces. The API is the OAuth
 * authority: GET /oauth/authorize/consent-request answers who is signed
 * in for the request, the person's spaces and what the agent will and
 * won't be able to do in each, decided by the server's policy. The page
 * posts the decision back as a plain form, so the browser follows the
 * API's redirect to the client's registered redirect_uri; nothing here
 * builds one. No words live here.
 */
import {
  MemaxError,
  type OAuthConsentHub,
  type OAuthConsentRequest,
} from "memax-sdk";
import { TARGET_READERS, type TargetKind } from "./targets";

/**
 * An ability the server says the agent has (can) or lacks (cannot) in a
 * space (packages/server/internal/handler/mcp_oauth_v2.go).
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
  /** The hub id the form sends. */
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
  /** read, propose or write (V2); null on V1. */
  autonomy: "read" | "propose" | "write" | null;
  can: ConsentAbility[];
  cannot: ConsentAbility[];
  /** Whether the server said what the agent can do here. */
  described: boolean;
}

export interface ConsentRequestView {
  requestId: string;
  /** The consent token: the form's CSRF token. */
  token: string;
  /** Where the form posts: the API's consent endpoint. */
  submitUrl: string;
  client: {
    /** As the client names itself; never trusted as anything but text. */
    name: string;
    /** The Ledger stamp's key (AGENTS), or the client's own slug. */
    agent: string;
    /** For a metadata-document client, the host Memax fetched it from. */
    host: string | null;
  };
  /** Who the request is signed in as. */
  person: string | null;
  /** V2 first, then project, team, personal; by name. */
  spaces: ConsentSpaceView[];
  /** The permissions the form sends: never above memax:propose. */
  permissions: string[];
  /** Seconds left when it was read. */
  expiresIn: number;
}

/** Why the request can't be shown. */
export type ConsentEnding =
  /** The link has no request in it. */
  | "missing"
  /** It lasted its 10 minutes. */
  | "expired"
  /** Answered already, or an old link (a token "Not you?" retired). */
  | "gone"
  /** It didn't load; trying again may work. */
  | "failed";

export class ConsentLoadError extends Error {
  constructor(readonly ending: ConsentEnding) {
    super(ending);
    this.name = "ConsentLoadError";
  }
}

export interface ConsentSource {
  load(input: {
    requestId: string;
    token: string;
    signal?: AbortSignal;
  }): Promise<ConsentRequestView>;
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
  switch (slug) {
    case "claude-ai":
      return "claude";
    default:
      return slug;
  }
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
  targets: NonNullable<OAuthConsentHub["targets"]>,
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
  hub: OAuthConsentHub,
  agent: string,
): ConsentSpaceView {
  const kind: ConsentSpaceKind =
    hub.space_kind ?? (hub.hub_type === "personal" ? "personal" : "team");
  const onV2 = hub.on_v2 === true;
  return {
    id: hub.id,
    name: hub.name,
    kind,
    onV2,
    disabled: hub.disabled,
    memories: onV2 ? (hub.kept_count ?? null) : hub.memory_count,
    people: hub.people_count ?? null,
    compiles: onV2 ? compiledFor(agent, hub.targets ?? []) : null,
    autonomy: onV2 ? (hub.autonomy ?? null) : null,
    can: abilities(hub.can),
    cannot: abilities(hub.cannot),
    described: hub.can !== undefined || hub.cannot !== undefined,
  };
}

export function toConsentRequest(r: OAuthConsentRequest): ConsentRequestView {
  const agent = stampAgent(r.agent_name);
  const spaces = r.hubs
    .map((hub) => toConsentSpace(hub, agent))
    .sort(
      (a, b) =>
        Number(b.onV2) - Number(a.onV2) ||
        KIND_ORDER[a.kind] - KIND_ORDER[b.kind] ||
        a.name.localeCompare(b.name),
    );
  const scope = (r.consent_scope ?? "memax:read memax:propose")
    .split(/\s+/)
    .filter((s) => s === "memax:read" || s === "memax:propose");
  return {
    requestId: r.session_id,
    token: r.csrf_token,
    submitUrl: r.submit_url,
    client: {
      name: cleanClientName(r.client_name),
      agent,
      host: r.client_host ?? null,
    },
    person: r.person?.name ?? null,
    spaces,
    permissions: scope,
    expiresIn: r.expires_in ?? 600,
  };
}

/** How the API's refusal reads on the page. */
export function consentEnding(err: unknown): ConsentEnding {
  if (err instanceof ConsentLoadError) return err.ending;
  if (!(err instanceof MemaxError)) return "failed";
  if (err.status === 410 || err.code === "consent_request_expired") {
    return "expired";
  }
  if (
    err.status === 404 ||
    err.code === "consent_request_not_found" ||
    err.code === "invalid_consent_token"
  ) {
    return "gone";
  }
  if (err.code === "missing_consent_request") return "missing";
  return "failed";
}
