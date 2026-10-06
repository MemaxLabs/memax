import { MemaxError, type V2 } from "memax-sdk";
import { registryKey } from "./agents-sdk";
import {
  waitingGates,
  type GateView,
  type GatesSource,
  type WebSession,
} from "./gates";
import type { Actor } from "./records";
import type { V2Client } from "./sdk-records";

/**
 * Decision gates through memax.v2.gates: the space's waiting gates
 * (GET /v2/spaces/{space}/gates?status=waiting), one gate by its G- ref,
 * and answer and withdraw with If-Match on the gate's version and the
 * caller's idempotency key. What /v2 doesn't serve yet is said so:
 * compile runs (an answer's `recompiled` is null) and other people's
 * names (a receipt carries only their id).
 */

/** At most three wait per agent and space (policy MaxWaitingGates), so one page holds them all. */
export const GATES_PAGE = 200;

function isNotFound(err: unknown): boolean {
  return (
    err instanceof MemaxError &&
    (err.status === 404 || err.code === "not_found")
  );
}

/** "9f1c" from "9f1c" or "session 9f1c". */
function sessionOf(ref: string | undefined): string | null {
  const trimmed = ref?.trim();
  return trimmed ? trimmed.replace(/^session\s+/i, "") : null;
}

function text(value: string | undefined): string | null {
  return value?.trim() ? value : null;
}

function person(id: string, viewerId: string | undefined): Actor {
  return { kind: "person", self: Boolean(viewerId) && id === viewerId };
}

/** One /v2 gate as the screens read it. */
export function gateOf(g: V2.Gate, viewerId: string | undefined): GateView {
  const agent = registryKey(g.agent ?? "other");
  return {
    id: g.id,
    ref: g.ref,
    version: g.version,
    question: g.question,
    context: text(g.context),
    options: g.options.map((o) => ({ label: o.label, detail: text(o.detail) })),
    status: g.status,
    agent,
    session: sessionOf(g.session_ref),
    askedAt: g.created_at,
    expiresAt: g.expires_at,
    needsWeb: g.needs_web,
    answer: g.answer
      ? {
          option: g.answer.option - 1,
          label: g.answer.label,
          memory: g.answer.memory.ref,
          by: person(g.answer.answered_by, viewerId),
          at: g.answer.answered_at,
        }
      : null,
    withdrawn: g.withdrawn
      ? {
          by:
            g.withdrawn.by_kind === "agent"
              ? { kind: "agent", agent }
              : person(g.withdrawn.by, viewerId),
          at: g.withdrawn.at,
        }
      : null,
  };
}

export function createSdkGates({
  client,
  viewerId,
  webSession,
}: {
  client: V2Client;
  viewerId: () => string | undefined;
  webSession: () => WebSession;
}): GatesSource {
  return {
    async waiting({ space, signal }) {
      const page = await client.v2.gates.list(space.slug, {
        status: "waiting",
        limit: GATES_PAGE,
        signal,
      });
      // The server reads expiry with its clock; this keeps the order.
      return waitingGates(
        page.items.map((g) => gateOf(g, viewerId())),
        new Date(),
      );
    },
    async get({ space, ref, signal }) {
      try {
        const gate = await client.v2.gates.get(ref, {
          space: space.slug,
          signal,
        });
        return gateOf(gate, viewerId());
      } catch (err) {
        if (isNotFound(err)) return null;
        throw err;
      }
    },
    // No X-Memax-Via: the server records an answer as a person's on the
    // web only when it can tell (the signed proxy), not because a client
    // says so.
    async answer({ space, gate, option, idempotencyKey }) {
      const result = await client.v2.gates.answer(
        gate.ref,
        { option: option + 1 },
        { space: space.slug, idempotencyKey, ifMatch: gate.version },
      );
      const answered = gateOf(result.gate, viewerId());
      const memory = result.memory
        ? { ref: result.memory.ref, version: result.memory.version }
        : { ref: answered.answer?.memory ?? "", version: 1 };
      // PLACEHOLDER: compile runs aren't served to the frame yet.
      return { gate: answered, memory, recompiled: null };
    },
    async withdraw({ space, gate, idempotencyKey }) {
      const result = await client.v2.gates.withdraw(
        gate.ref,
        {},
        { space: space.slug, idempotencyKey, ifMatch: gate.version },
      );
      return gateOf(result.gate, viewerId());
    },
    webSession,
  };
}
