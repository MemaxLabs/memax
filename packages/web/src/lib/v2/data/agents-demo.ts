import {
  AgentCommandError,
  isRaise,
  type AgentConnectionView,
  type AgentDetailView,
  type AgentsData,
  type AgentSpaceView,
  type ApiKeyView,
} from "./agents";
import { DEMO_SPACES } from "./demo-dataset";

/**
 * The agents of the handoff's demo dataset: Agents.png's six rows,
 * AgentDetail.png's Codex, Keys.png's connections and keys, all at
 * Monday October 5 2026, 14:40 in Vancouver. Data, not copy.
 *
 * Commands change an in-memory copy (one per source), with the server's
 * rules where the demo can tell: an API key's agent proposes at most,
 * only the person an agent works for raises it, nothing changes on a
 * disconnected one. The demo person is on the web, so raising works.
 */

const at = (day: string, time: string) => `2026-${day}T${time}:00-07:00`;
const today = (time: string) => at("10-05", time);

const space = (slug: string) => {
  const s = DEMO_SPACES.find((d) => d.slug === slug)!;
  return { spaceId: s.id, slug: s.slug, name: s.name, kind: s.kind };
};

const inV2 = (
  autonomy: AgentSpaceView["autonomy"],
  reads7d: number,
  writes7d: number,
): AgentSpaceView => ({ ...space("memax-v2"), autonomy, reads7d, writes7d });

const id = (n: number) => `0192a7c0-0000-7000-8000-0000000000c${n}`;

/** The demo connections' ids by agent, for the demo's receipts. */
export const DEMO_AGENT_IDS: Readonly<Record<string, string>> = {
  "claude-code": id(1),
  codex: id(2),
  cursor: id(3),
  chatgpt: id(4),
  claude: id(5),
  gemini: id(6),
};

const oauth = { kind: "oauth_grant", active: true } as const;

/** Agents.png, in its order (oldest first). */
const AGENTS: readonly AgentConnectionView[] = [
  {
    id: id(1),
    agent: "claude-code",
    name: "Claude Code",
    surface: "cli",
    state: "active",
    credential: oauth,
    maxAutonomy: "write",
    mine: true,
    clientId: null,
    spaces: [inV2("write", 1204, 38)],
    reads7d: 1204,
    writes7d: 38,
    lastSeenAt: today("14:38"),
    connectedAt: at("08-14", "10:02"),
    connectedBy: "you",
    may: ["read", "write", "ask"],
    target: { path: "CLAUDE.md", status: "synced" },
  },
  {
    id: id(2),
    agent: "codex",
    name: "Codex",
    surface: "cloud",
    state: "active",
    credential: oauth,
    maxAutonomy: "write",
    mine: true,
    clientId: "https://chatgpt.com/codex/oauth-client.json",
    spaces: [inV2("propose", 512, 21)],
    reads7d: 512,
    writes7d: 21,
    lastSeenAt: today("14:26"),
    connectedAt: at("09-02", "09:30"),
    connectedBy: "you",
    may: ["read", "propose", "ask"],
    target: { path: "AGENTS.md", status: "synced", sharedWith: "opencode" },
  },
  {
    id: id(3),
    agent: "cursor",
    name: "Cursor",
    surface: "ide",
    state: "active",
    credential: oauth,
    maxAutonomy: "write",
    mine: true,
    clientId: null,
    spaces: [inV2("read", 388, 0)],
    reads7d: 388,
    writes7d: 0,
    lastSeenAt: today("13:40"),
    connectedAt: at("08-20", "15:11"),
    connectedBy: "you",
    may: ["read"],
    target: { path: ".cursor/rules/memax.mdc", status: "drifted" },
  },
  {
    id: id(4),
    agent: "chatgpt",
    name: "ChatGPT",
    surface: "chat",
    state: "active",
    credential: oauth,
    maxAutonomy: "write",
    mine: true,
    clientId: "https://chatgpt.com/connector/oauth-client.json",
    spaces: [
      inV2("propose", 64, 5),
      { ...space("personal"), autonomy: "propose", reads7d: 22, writes7d: 1 },
    ],
    reads7d: 86,
    writes7d: 6,
    lastSeenAt: today("11:40"),
    connectedAt: at("09-10", "12:00"),
    connectedBy: "you",
    may: ["read", "propose"],
    target: { path: "ChatGPT project", status: "synced" },
  },
  {
    id: id(5),
    agent: "claude",
    name: "Claude",
    surface: "chat",
    state: "active",
    credential: oauth,
    maxAutonomy: "write",
    mine: true,
    clientId: "https://claude.ai/oauth/claude-client.json",
    spaces: [
      { ...space("personal"), autonomy: "propose", reads7d: 30, writes7d: 1 },
      inV2("propose", 41, 2),
    ],
    reads7d: 71,
    writes7d: 3,
    lastSeenAt: at("10-04", "17:05"),
    connectedAt: at("09-18", "20:40"),
    connectedBy: "you",
    may: ["read", "propose"],
    target: null,
  },
  {
    id: id(6),
    agent: "gemini",
    name: "Gemini CLI",
    surface: "cli",
    state: "paused",
    credential: oauth,
    maxAutonomy: "write",
    mine: true,
    clientId: null,
    spaces: [inV2("read", 0, 0)],
    reads7d: 0,
    writes7d: 0,
    lastSeenAt: at("09-26", "11:15"),
    connectedAt: at("09-25", "16:20"),
    connectedBy: "you",
    may: ["read"],
    target: null,
  },
];

/** AgentDetail.png: Codex's week, writes and sessions. */
const CODEX_DETAIL: Omit<AgentDetailView, "connection"> = {
  week: {
    reads: 512,
    writes: 21,
    proposals: 21,
    kept: 14,
    rejected: 3,
    waiting: 4,
    questions: { asked: 1, waiting: 1 },
    handoffsReceived: 1,
    heldExternal: 2,
  },
  recentWrites: [
    {
      receiptId: "0192a7c0-0000-7000-8000-0000000000e1",
      ref: "M-0431",
      statement: "Deploy the v2 API to Fly.io in iad and ams.",
      state: "conflict",
      actor: { agent: "codex" },
      action: "proposed",
      at: today("14:26"),
      note: { kind: "contradicts", ref: "M-0174" },
    },
    {
      receiptId: "0192a7c0-0000-7000-8000-0000000000e2",
      ref: "M-0432",
      statement: "Pin shared dependency versions with the pnpm catalog.",
      state: "proposed",
      actor: { agent: "codex" },
      action: "proposed",
      at: today("14:18"),
    },
    {
      receiptId: "0192a7c0-0000-7000-8000-0000000000e3",
      ref: "M-0410",
      statement:
        "Workers must be idempotent: machines restart on every deploy.",
      state: "kept",
      actor: { person: "ZZ" },
      action: "kept",
      at: at("10-01", "16:44"),
      note: { kind: "proposed-in", agent: "codex", session: "2d4a" },
    },
  ],
  sessions: [
    {
      ref: "9f1c",
      kind: "cloud",
      handoff: "H-0093",
      live: true,
      reads: 38,
      writes: 2,
      lastAt: today("14:21"),
    },
    {
      ref: "2d4a",
      kind: "cli",
      reads: 120,
      writes: 6,
      lastAt: at("10-01", "16:30"),
    },
    {
      ref: "77ab",
      kind: "cloud",
      reads: 41,
      writes: 1,
      lastAt: at("09-29", "10:05"),
    },
    {
      ref: "5b0e",
      kind: "cli",
      reads: 96,
      writes: 4,
      lastAt: at("09-11", "13:52"),
    },
  ],
};

/** Keys.png's API keys. */
const API_KEYS: readonly ApiKeyView[] = [
  {
    id: "0192a7c0-0000-7000-8000-0000000000d1",
    name: "CI pipeline",
    masked: "mx_live_…7f2c",
    spaceIds: [space("memax-v2").spaceId],
    may: "read",
    createdAt: at("09-20", "09:00"),
    lastUsedAt: today("13:35"),
  },
  {
    id: "0192a7c0-0000-7000-8000-0000000000d2",
    name: "Staging support bot",
    masked: "mx_live_…1b9a",
    spaceIds: [space("memax-team").spaceId],
    may: "propose",
    createdAt: at("09-28", "14:00"),
    lastUsedAt: null,
  },
];

/** The file each agent kind reads in memax-v2 (the demo's compile targets). */
const NATIVE_FILES: Readonly<Record<string, string>> = {
  "claude-code": "CLAUDE.md",
  codex: "AGENTS.md",
  opencode: "AGENTS.md",
  copilot: "AGENTS.md",
  cursor: ".cursor/rules/memax.mdc",
  chatgpt: "ChatGPT project",
};

function detailFor(agent: AgentConnectionView): AgentDetailView {
  if (agent.agent === "codex") return { connection: agent, ...CODEX_DETAIL };
  return {
    connection: agent,
    week: {
      reads: agent.reads7d,
      writes: agent.writes7d,
      proposals: agent.writes7d,
      kept: 0,
      rejected: 0,
      waiting: 0,
      questions: null,
      handoffsReceived: null,
      heldExternal: null,
    },
    recentWrites: [],
    sessions: [],
  };
}

export function createDemoAgents(): AgentsData {
  // One copy per source, so commands change what the screens read.
  let agents: AgentConnectionView[] = AGENTS.map((a) => ({ ...a }));
  let keys: ApiKeyView[] = [...API_KEYS];
  let nextKey = 1;

  const find = (agentId: string) => {
    const agent = agents.find((a) => a.id === agentId);
    if (!agent) throw new AgentCommandError("not_found");
    if (agent.state === "disconnected") {
      throw new AgentCommandError("invalid_transition");
    }
    return agent;
  };
  const update = (next: AgentConnectionView) => {
    agents = agents.map((a) => (a.id === next.id ? next : a));
    return next;
  };
  const inSpace = (slug: string) =>
    agents.filter(
      (a) =>
        a.state !== "disconnected" && a.spaces.some((s) => s.slug === slug),
    );
  const mineOnly = () => agents.filter((a) => a.state !== "disconnected");

  return {
    agentsPeek: {
      spaceAgents: (slug) => inSpace(slug),
      agent: (agentId) => {
        const agent = agents.find((a) => a.id === agentId);
        return agent ? detailFor(agent) : undefined;
      },
      myAgents: () => mineOnly(),
      apiKeys: () => [...keys],
    },
    spaceAgents: async (s) => inSpace(s.slug),
    myAgents: async () => mineOnly(),
    agent: async (agentId) => {
      const agent = agents.find((a) => a.id === agentId);
      return agent ? detailFor(agent) : null;
    },
    async setAutonomy({ agent: agentId, space: s, autonomy }) {
      const agent = find(agentId);
      const current = agent.spaces.find((x) => x.slug === s.slug);
      const from = current?.autonomy ?? "read";
      if (isRaise(from, autonomy)) {
        if (!agent.mine) throw new AgentCommandError("not_your_agent");
        if (autonomy === "write" && agent.credential.kind === "api_key") {
          throw new AgentCommandError("key_max_propose");
        }
      }
      const spaces = current
        ? agent.spaces.map((x) => (x.slug === s.slug ? { ...x, autonomy } : x))
        : [
            ...agent.spaces,
            {
              spaceId: s.id,
              slug: s.slug,
              name: s.name,
              kind: s.kind,
              autonomy,
              reads7d: 0,
              writes7d: 0,
            },
          ];
      // The demo's `may` is the board's; past a change, it follows the level.
      return update({ ...agent, spaces, may: undefined });
    },
    async pauseAgent({ agent: agentId }) {
      const agent = find(agentId);
      if (agent.state === "paused") {
        throw new AgentCommandError("invalid_transition");
      }
      return update({ ...agent, state: "paused" });
    },
    async resumeAgent({ agent: agentId }) {
      const agent = find(agentId);
      if (agent.state !== "paused") {
        throw new AgentCommandError("invalid_transition");
      }
      return update({ ...agent, state: "active" });
    },
    async disconnectAgent({ agent: agentId }) {
      const agent = find(agentId);
      return update({
        ...agent,
        state: "disconnected",
        credential: { ...agent.credential, active: false },
      });
    },
    newAgentAutonomy: () => "propose",
    targetFor: (s, key) => {
      if (s.slug !== "memax-v2") return undefined;
      // ConnectAgent.png: OpenCode reads the AGENTS.md Codex reads.
      const file = NATIVE_FILES[key];
      if (!file) return null;
      const reader = agents.find(
        (a) => a.target?.path === file && a.agent !== key,
      );
      const target = agents.find((a) => a.target?.path === file)?.target;
      return {
        path: file,
        status: target?.status ?? "synced",
        sharedWith: reader?.agent,
      };
    },
    apiKeys: async () => [...keys],
    async createApiKey({ name, may, space: s }) {
      const n = nextKey++;
      const secret = `mx_live_demo${String(n).padStart(4, "0")}`;
      const key: ApiKeyView = {
        id: `0192a7c0-0000-7000-8000-0000000001d${n}`,
        name,
        masked: `mx_live_…${secret.slice(-4)}`,
        spaceIds: [s.id],
        may,
        createdAt: today("14:40"),
        lastUsedAt: null,
      };
      keys = [...keys, key];
      return { key, secret };
    },
    async revokeApiKey(keyId) {
      keys = keys.filter((k) => k.id !== keyId);
    },
  };
}
