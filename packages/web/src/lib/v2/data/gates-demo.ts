import { MemaxError } from "memax-sdk";
import { ZZ, at } from "./demo-review-data";
import {
  gateStatusAt,
  waitingGates,
  type GateView,
  type GatesSource,
  type WebSession,
} from "./gates";

/**
 * The demo's decision gates: Ledger.png's "Open questions" in the Memax
 * team space, Claude Code's question about the ChatGPT tool names and
 * Codex's deploy-target question (as Handoff.png and MobileGate.png draw
 * it). A team space's decisions need a person on the web (D15), so both
 * carry `needsWeb`. Answers and withdrawals made in this browser session
 * apply on top, replay by idempotency key and refuse like the server:
 * 403 `decision_needs_web` when the session isn't the web app's, and 409
 * with `details.status` once a gate has ended. Data, not copy.
 *
 * memax-v2's boards (Main, Review, Activity) name Codex's question from
 * H-0093 without listing it, so that space has no gate here and keeps
 * reproducing them.
 */

const TEAM = "memax-team";

export const DEMO_GATES: Readonly<Record<string, readonly GateView[]>> = {
  [TEAM]: [
    {
      id: "0192a7c0-0000-7000-8000-0000000c0011",
      ref: "G-0011",
      version: 1,
      question: "Keep the ChatGPT tool names, or align them with the core set?",
      context:
        "The ChatGPT profile serves seven aliases of the core MCP tools. Aligning them means one set of names everywhere, and a connector update for ChatGPT.",
      options: [
        {
          label: "Keep the ChatGPT names",
          detail: "The seven aliases stay; nothing changes for ChatGPT.",
        },
        {
          label: "Align with the core set",
          detail: "One set of tool names everywhere; the connector is updated.",
        },
        {
          label: "Decide later",
          detail: "Claude Code keeps the aliases until you answer.",
        },
      ],
      status: "waiting",
      agent: "claude-code",
      session: "3e1a",
      askedAt: at("13:40"),
      // Claude Code asked to wait a day.
      expiresAt: at("13:40", "2026-10-06"),
      needsWeb: true,
      answer: null,
      withdrawn: null,
    },
    {
      id: "0192a7c0-0000-7000-8000-0000000c0012",
      ref: "G-0012",
      version: 1,
      question: "Which deploy target should the v2 API use?",
      context:
        "Codex found a kept note and its own proposal that disagree: M-0174 (Railway, kept by Jiahao) and M-0431 (Fly.io, proposed by Codex).",
      options: [
        {
          label: "Fly.io, iad and ams",
          detail: "Matches where the current API and workers run.",
        },
        {
          label: "Railway",
          detail: "Jiahao’s note from Sep 18: simpler preview environments.",
        },
        {
          label: "Decide later",
          detail: "Codex keeps both configs behind a flag until you answer.",
        },
      ],
      status: "waiting",
      agent: "codex",
      session: "9f1c",
      askedAt: at("14:38"),
      // The default: seven days.
      expiresAt: at("14:38", "2026-10-12"),
      needsWeb: true,
      answer: null,
      withdrawn: null,
    },
  ],
};

function sleep(ms: number) {
  return new Promise<void>((resolve) => setTimeout(resolve, ms));
}

/** The decision an answer keeps: the question, then the chosen option (ledger gateStatement). */
export function answerStatement(question: string, label: string): string {
  const q = question.trim();
  if (/[?.!:]$/.test(q)) return `${q} ${label}`;
  if (/[？。！：]$/.test(q)) return `${q}${label}`;
  return `${q}: ${label}`;
}

export function createDemoGates({
  now,
  commandDelayMs = 240,
  webSession = true,
  nextRef,
  kept,
  recompiled,
}: {
  now: () => Date;
  commandDelayMs?: number;
  /** The demo plays the web app; tests turn it off to see D15's notice. */
  webSession?: WebSession;
  /** The next memory display ID (shared with Remember). */
  nextRef: () => string;
  /** The decision an answer kept (citing the gate), so Memories and the Brief show it. */
  kept: (
    slug: string,
    memory: { ref: string; statement: string; gate: string },
  ) => void;
  /** Files a Keep recompiles in the space. */
  recompiled: (slug: string) => number | null;
}): GatesSource & {
  /** Answers given this session, for the overview's kept count. */
  answeredCount(slug: string): number;
} {
  /** Gates that ended this session, by space and ref. */
  const ended = new Map<string, GateView>();
  const replays = new Map<string, unknown>();
  const key = (slug: string, ref: string) => `${slug}/${ref}`;
  const stamp = () => now().toISOString();

  function current(slug: string, ref: string): GateView | null {
    const base = DEMO_GATES[slug]?.find((g) => g.ref === ref || g.id === ref);
    if (!base) return null;
    return ended.get(key(slug, base.ref)) ?? base;
  }

  function all(slug: string): GateView[] {
    return (DEMO_GATES[slug] ?? []).map(
      (g) => ended.get(key(slug, g.ref)) ?? g,
    );
  }

  /** Runs a command once per key, after the demo's delay, like the server's replay. */
  async function command<T>(idempotencyKey: string, run: () => T): Promise<T> {
    if (replays.has(idempotencyKey)) {
      await sleep(commandDelayMs);
      return replays.get(idempotencyKey) as T;
    }
    await sleep(commandDelayMs);
    const result = run();
    replays.set(idempotencyKey, result);
    return result;
  }

  /** The server's checks before a gate command: it exists, it waits, the version matches. */
  function open(slug: string, gate: GateView): GateView {
    const found = current(slug, gate.ref);
    if (!found) {
      throw new MemaxError("Nothing by that reference.", "not_found", 404);
    }
    const status = gateStatusAt(found, now());
    if (status !== "waiting") {
      throw new MemaxError(
        `${found.ref} isn't waiting any more.`,
        "invalid_transition",
        409,
        { ref: found.ref, status },
      );
    }
    if (found.version !== gate.version) {
      throw new MemaxError(`${found.ref} changed.`, "edit_clash", 412, {
        ref: found.ref,
        expected_version: gate.version,
        current_version: found.version,
      });
    }
    return found;
  }

  return {
    peekWaiting: (slug) => waitingGates(all(slug), now()),
    async waiting({ space }) {
      return waitingGates(all(space.slug), now());
    },
    async get({ space, ref }) {
      return current(space.slug, ref);
    },
    answer({ space, gate, option, idempotencyKey }) {
      return command(idempotencyKey, () => {
        const found = open(space.slug, gate);
        // D15: a team space's decisions are answered only on the web.
        if (found.needsWeb && webSession === false) {
          const message = `Decisions in ${space.name} need a person on the web. Answer ${found.ref} in Review at memax.app.`;
          throw new MemaxError(message, "refused", 403, {
            policy: { effect: "refuse", code: "decision_needs_web", message },
          });
        }
        const choice = found.options[option];
        if (!choice) {
          throw new MemaxError(
            "Choose one of its options.",
            "invalid_request",
            400,
            {
              field: "option",
            },
          );
        }
        const ref = nextRef();
        kept(space.slug, {
          ref,
          statement: answerStatement(found.question, choice.label),
          gate: found.ref,
        });
        const answered: GateView = {
          ...found,
          status: "answered",
          version: found.version + 1,
          answer: {
            option,
            label: choice.label,
            memory: ref,
            by: ZZ,
            at: stamp(),
          },
        };
        ended.set(key(space.slug, found.ref), answered);
        return {
          gate: answered,
          memory: { ref, version: 1 },
          recompiled: recompiled(space.slug),
        };
      });
    },
    withdraw({ space, gate, idempotencyKey }) {
      return command(idempotencyKey, () => {
        const found = open(space.slug, gate);
        const withdrawn: GateView = {
          ...found,
          status: "withdrawn",
          version: found.version + 1,
          withdrawn: { by: ZZ, at: stamp() },
        };
        ended.set(key(space.slug, found.ref), withdrawn);
        return withdrawn;
      });
    },
    webSession: () => webSession,
    answeredCount: (slug) =>
      all(slug).filter((g) => g.status === "answered").length,
  };
}
