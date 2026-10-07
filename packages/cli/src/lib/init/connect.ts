// Step 3 of memax init: connect the agents found on this machine (plan 25
// §7.3). An agent connects over OAuth, the way memax setup --mcp sets it
// up: init writes its MCP settings, and the agent's own sign-in (on its
// first use) creates its connection, at Propose (Cursor and Gemini CLI at
// Read). An agent already connected is connected to the new spaces here,
// at the same levels; from the CLI a level can only be lowered or kept,
// never raised (raising needs a person on the web).
import { randomUUID } from "node:crypto";
import type { Memax, V2 } from "memax-sdk";
import { detectAgents, type AgentEntry, type DetectedAgent } from "./agents.js";
import type { InitDeps, InitReport } from "./types.js";

export interface ConnectResult {
  detected: DetectedAgent[];
  rows: InitReport["agents"];
}

const RANK: Record<V2.Autonomy, number> = { read: 0, propose: 1, write: 2 };

/**
 * Connects an agent's existing connection to each space it isn't in yet:
 * at the level it starts at, or lower if it is lower elsewhere (never
 * raised from the CLI). Returns its level in the first space, and whether
 * anything changed. Shared by memax init and memax connect.
 */
export async function connectToSpaces(
  memax: Memax,
  conn: V2.AgentConnection,
  start: V2.Autonomy,
  spaces: V2.Space[],
): Promise<{ autonomy: V2.Autonomy | null; added: number }> {
  let autonomy: V2.Autonomy | null = null;
  let added = 0;
  for (const sp of spaces) {
    const there = conn.spaces.find((s) => s.space_id === sp.id);
    if (there) {
      autonomy ??= there.autonomy;
      continue;
    }
    const elsewhere = conn.spaces
      .map((s) => s.autonomy)
      .sort((x, y) => RANK[x] - RANK[y])[0];
    const level =
      elsewhere && RANK[elsewhere] < RANK[start] ? elsewhere : start;
    try {
      await memax.v2.agents.setAutonomy(
        conn.id,
        sp.id,
        { autonomy: level },
        { idempotencyKey: randomUUID(), via: "cli" },
      );
      autonomy ??= level;
      added++;
    } catch {
      // A space whose default is lower than this level: it connects there on its next sign-in.
    }
  }
  return { autonomy, added };
}

/** Finds the agents, writes their MCP settings (when allowed), and connects existing connections to the spaces. */
export async function connectAgents(
  d: InitDeps,
  root: string | null,
  spaces: V2.Space[],
  /** Asked once, with the agents whose MCP settings don't name Memax yet. */
  allow: (need: AgentEntry[]) => Promise<boolean>,
): Promise<ConnectResult> {
  const detected = detectAgents({
    home: d.home,
    root,
    path: d.env.path,
    platform: d.env.platform,
  });
  let connections: V2.AgentConnection[] = [];
  try {
    connections = (await d.memax.v2.agents.list()).items;
  } catch {
    // Connections are a convenience here; MCP settings still work.
  }
  const need = detected
    .filter((x) => x.found && x.agent.setupId && !d.hasMcp(x.agent))
    .map((x) => x.agent);
  const write = need.length > 0 && (await allow(need));
  const rows: InitReport["agents"] = [];
  for (const det of detected) {
    const a = det.agent;
    if (!det.found && !a.connector) continue;
    let mcp = a.connector ? "connector" : "not written";
    if (det.found && a.setupId && !need.includes(a)) mcp = "present";
    else if (det.found && a.setupId && write) {
      const res = await d.writeMcp(a);
      mcp = typeof res === "string" ? res : `failed: ${res.error}`;
    }
    const conn = connections.find(
      (c) => c.agent === a.kind && c.state === "active",
    );
    const autonomy = conn
      ? (await connectToSpaces(d.memax, conn, a.start, spaces)).autonomy
      : null;
    rows.push({
      kind: a.kind,
      name: a.name,
      where: det.found ? det.evidence : "",
      found: det.found,
      mcp,
      autonomy: autonomy ?? (det.found ? a.start : null),
    });
  }
  return { detected, rows };
}
