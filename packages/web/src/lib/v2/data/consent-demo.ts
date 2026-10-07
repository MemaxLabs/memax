import type { OAuthConsentRequest } from "memax-sdk";
import {
  ConsentLoadError,
  toConsentRequest,
  type ConsentSource,
} from "./consent";

/**
 * The board's request (OAuthConsent.png), for dev fixtures: Codex asking
 * Ziyang, whose memax-v2 compiles AGENTS.md. Opened as
 * /oauth/authorize?request_id=demo where dev fixtures are on; any other
 * request goes to the API. Nothing it shows is posted anywhere.
 */
export const DEMO_CONSENT_REQUEST = "demo";

export const demoConsentRequest: OAuthConsentRequest = {
  session_id: DEMO_CONSENT_REQUEST,
  csrf_token: "demo",
  client_name: "Codex",
  agent_name: "codex",
  resource: "https://mcp.memax.app/mcp",
  submit_url: "",
  expires_at: "2026-10-05T14:12:00Z",
  hubs: [
    {
      id: "0192a7c0-0000-7000-8000-000000000002",
      name: "memax-v2",
      slug: "memax-v2",
      role: "owner",
      hub_type: "team",
      memory_count: 0,
      checked: true,
      disabled: false,
      capability_label: "",
      supported_permissions: [],
      space_kind: "project",
      on_v2: true,
      people_count: 1,
      kept_count: 214,
      targets: [
        { kind: "agents_md", path: "AGENTS.md" },
        { kind: "claude_md", path: "CLAUDE.md" },
      ],
      autonomy: "propose",
      can: ["read_brief", "propose", "gate"],
      cannot: ["keep", "forget", "other_spaces"],
    },
    {
      id: "0192a7c0-0000-7000-8000-000000000003",
      name: "Memax team",
      slug: "memax-team",
      role: "owner",
      hub_type: "team",
      memory_count: 0,
      checked: true,
      disabled: false,
      capability_label: "",
      supported_permissions: [],
      space_kind: "team",
      on_v2: true,
      people_count: 2,
      kept_count: 96,
      autonomy: "propose",
      can: ["read_brief", "propose", "gate"],
      cannot: ["keep", "forget", "other_spaces"],
    },
    {
      id: "0192a7c0-0000-7000-8000-000000000001",
      name: "Personal",
      slug: "personal",
      role: "owner",
      hub_type: "personal",
      memory_count: 0,
      checked: true,
      disabled: false,
      capability_label: "",
      supported_permissions: [],
      space_kind: "personal",
      on_v2: true,
      people_count: 1,
      kept_count: 61,
      autonomy: "propose",
      can: ["read_brief", "propose", "gate"],
      cannot: ["keep", "forget", "other_spaces"],
    },
  ],
  permissions: [],
  not_requested: [],
  person: { name: "Ziyang" },
  consent_scope: "memax:read memax:propose",
  expires_in: 600,
};

export const demoConsent: ConsentSource = {
  async load({ requestId }) {
    if (requestId !== DEMO_CONSENT_REQUEST) throw new ConsentLoadError("gone");
    return toConsentRequest(demoConsentRequest);
  },
};
