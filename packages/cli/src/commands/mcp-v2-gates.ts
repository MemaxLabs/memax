// Decision gates in the local stdio MCP server, for spaces on the V2
// record. As on the remote server, memax_request_decision asks a G- gate
// through memax.v2 (X-Memax-Via: mcp) and returns its ID at once, and the
// ledger decides who may ask. Two differences, the same as for proposals
// (mcp-v2.ts): this process is one agent session, so recall tells it how
// the gates it asked ended, once each; and nobody is asked in the agent,
// so a person answers in Memax (or with `memax gate`).
import { createHash } from "node:crypto";
import { MemaxError, type V2 } from "memax-sdk";
import { getClient } from "../lib/client.js";
import {
  errorResult,
  reviewURL,
  textResult,
  type McpTextResult,
} from "./mcp-v2.js";

/** A gate in a result, the shape of the remote server's MCPGate. */
export interface McpGate {
  [key: string]: unknown;
  id: string;
  space_id: string;
  space?: string;
  question: string;
  status: V2.GateStatus;
  option?: number;
  answer?: string;
  memory?: string;
  expires_at?: string;
  url?: string;
  message?: string;
}

/** The gates this process asked and hasn't heard the end of: id → space id. */
const sessionGates = new Map<string, string>();

function gateItem(sp: V2.Space, g: V2.Gate): McpGate {
  const url = reviewURL(sp, g.ref);
  return {
    id: g.ref,
    space_id: sp.id,
    space: sp.name,
    question: g.question,
    status: g.status,
    ...(g.answer
      ? {
          option: g.answer.option,
          answer: g.answer.label,
          memory: g.answer.memory.ref,
        }
      : {}),
    expires_at: g.expires_at,
    ...(url ? { url } : {}),
  };
}

/** memax_request_decision's answer: the gate's ID and how it stands. */
function gateResult(sp: V2.Space, g: V2.Gate): McpTextResult {
  const item = gateItem(sp, g);
  let text: string;
  switch (g.status) {
    case "answered":
      text = `${g.ref} in ${sp.name} is answered: ${item.answer}. It is kept as ${item.memory}, a decision by the person.`;
      break;
    case "withdrawn":
      text = `${g.ref} in ${sp.name} was withdrawn, so no answer is coming.`;
      break;
    case "expired":
      text = `${g.ref} in ${sp.name} expired without an answer.`;
      break;
    default:
      text = `Asked in ${sp.name} as ${g.ref}.`;
      if (g.needs_web)
        text += ` Decisions in ${sp.name} need a person on the web, so it is answered there.`;
      if (item.url) text += ` A person answers it in Review: ${item.url}.`;
      text += ` The answer comes back in this connection's next memax_recall; it waits until ${g.expires_at}.`;
  }
  item.message = text;
  return textResult(text, item);
}

/** memax_request_decision in a space on V2: a gate, asked at once. */
export async function v2RequestDecision(
  sp: V2.Space,
  args: { question: string; options: string[]; context?: string },
): Promise<McpTextResult> {
  const options = args.options
    .map((label) => label.trim())
    .filter((label) => label !== "")
    .map((label) => ({ label }));
  const context = args.context?.trim();
  const input: V2.RequestDecisionInput = {
    question: args.question.trim(),
    options,
    ...(context ? { context } : {}),
  };
  // The same question asked again in this session is the same gate.
  const idempotencyKey =
    "mcp-gate:" +
    createHash("sha256")
      .update(JSON.stringify([sp.id, process.pid, input]))
      .digest("base64url");
  try {
    const res = await getClient().v2.gates.request(sp.id, input, {
      idempotencyKey,
      via: "mcp",
    });
    if (res.gate.status === "waiting") sessionGates.set(res.gate.id, sp.id);
    return gateResult(sp, res.gate);
  } catch (err) {
    return errorResult(
      err instanceof MemaxError
        ? err.message
        : `Decision request failed: ${(err as Error).message}`,
    );
  }
}

/**
 * How this session's gates in the spaces ended (each once), and for a
 * digest the ones still waiting: recall's `gates`, and its text.
 */
export async function v2GateNews(
  spaces: V2.Space[],
  digest: boolean,
): Promise<{ gates: McpGate[]; text: string }> {
  const byId = new Map(spaces.map((sp) => [sp.id, sp]));
  const ended: McpGate[] = [];
  const waiting: McpGate[] = [];
  for (const [id, spaceId] of [...sessionGates]) {
    const sp = byId.get(spaceId);
    if (!sp) continue;
    let g: V2.Gate;
    try {
      g = await getClient().v2.gates.get(id);
    } catch (err) {
      if (err instanceof MemaxError && err.status === 404)
        sessionGates.delete(id);
      continue; // the next recall tries again
    }
    if (g.status === "waiting") {
      if (digest) waiting.push(gateItem(sp, g));
      continue;
    }
    ended.push(gateItem(sp, g));
    sessionGates.delete(id);
  }
  const gates = [...ended, ...waiting];
  if (!gates.length) return { gates, text: "" };
  const lines = ["Decisions you asked for:"];
  for (const g of gates) {
    switch (g.status) {
      case "answered":
        lines.push(
          `- ${g.id} (${g.space}) answered: ${g.answer}. Kept as ${g.memory}.`,
        );
        break;
      case "withdrawn":
        lines.push(`- ${g.id} (${g.space}) was withdrawn by a person.`);
        break;
      case "expired":
        lines.push(`- ${g.id} (${g.space}) expired without an answer.`);
        break;
      default:
        lines.push(`- ${g.id} (${g.space}) is still waiting on a person.`);
    }
  }
  return { gates, text: lines.join("\n") };
}

/** Test hook: forget the gates this process asked. */
export function resetGatesForTest(): void {
  sessionGates.clear();
}
