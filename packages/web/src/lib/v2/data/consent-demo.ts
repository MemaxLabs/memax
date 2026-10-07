import type { OAuthRequest, OAuthRequestSpace } from "memax-sdk";
import {
  ConsentLoadError,
  toConsentRequest,
  type ConsentSource,
} from "./consent";

/**
 * The board's request (OAuthConsent.png), for dev fixtures: Codex asking
 * Ziyang, whose memax-v2 compiles AGENTS.md. Opened as
 * /oauth/authorize?request=demo where dev fixtures are on, with no session;
 * any other request goes to the API. Answering it goes nowhere.
 */
export const DEMO_CONSENT_REQUEST = "demo";

const atPropose: Pick<
  OAuthRequestSpace,
  "on_v2" | "role" | "disabled" | "autonomy" | "ceiling" | "can" | "cannot"
> = {
  on_v2: true,
  role: "owner",
  disabled: false,
  autonomy: "propose",
  ceiling: "write",
  can: ["read_brief", "propose", "gate"],
  cannot: ["keep", "forget", "other_spaces"],
};

export const demoConsentRequest: OAuthRequest = {
  request_id: DEMO_CONSENT_REQUEST,
  client_name: "Codex",
  agent_name: "codex",
  resource: "https://mcp.memax.app/mcp",
  scope: "memax:read memax:write",
  expires_at: "2026-10-05T14:12:00Z",
  expires_in: 600,
  person: { name: "Ziyang" },
  spaces: [
    {
      ...atPropose,
      id: "0192a7c0-0000-7000-8000-000000000002",
      name: "memax-v2",
      slug: "memax-v2",
      kind: "project",
      people: 1,
      memories: 214,
      targets: [
        { kind: "agents_md", path: "AGENTS.md" },
        { kind: "claude_md", path: "CLAUDE.md" },
      ],
    },
    {
      ...atPropose,
      id: "0192a7c0-0000-7000-8000-000000000003",
      name: "Memax team",
      slug: "memax-team",
      kind: "team",
      people: 2,
      memories: 96,
    },
    {
      ...atPropose,
      id: "0192a7c0-0000-7000-8000-000000000001",
      name: "Personal",
      slug: "personal",
      kind: "personal",
      people: 1,
      memories: 61,
    },
  ],
};

export const demoConsent: ConsentSource = {
  async load(requestId) {
    if (requestId !== DEMO_CONSENT_REQUEST) throw new ConsentLoadError("gone");
    return toConsentRequest(demoConsentRequest);
  },
  // The demo answers nothing: the page stays as it is, waiting.
  decide: () => new Promise<string>(() => undefined),
  release: () => new Promise<void>(() => undefined),
};
