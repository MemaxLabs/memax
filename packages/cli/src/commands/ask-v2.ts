// memax ask "<question>" in a space on the V2 record (plan 25 §7.2, the
// Cli board): a cited answer from the space's kept memories, streamed,
// with the cited memories' receipts under it:
//
//   River runs on the Postgres we already operate, so a job commits
//   with the rows it touches. [1] Temporal was dropped. [2]
//   [1] M-0219 kept · Oct 2   [2] M-0230 kept by CC · Aug 21
//
// Spaces still on V1 keep V1's ask (ask.ts decides which).
import chalk from "chalk";
import { MemaxError, refusalOf, type Memax, type V2 } from "memax-sdk";
import { agentMark, apiFailureMessage, clock } from "./v2-output.js";

export interface AskV2Deps {
  memax: Pick<Memax, "v2">;
  /** Writes without a newline (the answer streams). */
  write: (s: string) => void;
  err: (line: string) => void;
  /** A terminal gets colour and the receipts line; a pipe gets plain text. */
  tty: boolean;
  now?: Date;
  signal?: AbortSignal;
}

export interface AskV2Options {
  format?: string;
}

/** What one answer came to, for --format json. */
export interface AskV2Result {
  space: string;
  question: string;
  outcome: V2.AskOutcome | "failed";
  answer: string;
  citations: { n: number; ref: string; statement: string }[];
  sources: V2.AskSource[];
  usage?: V2.AskUsage;
  error?: string;
}

/** "kept · Oct 2", "kept by CC · Aug 21", "merged by Dream · Aug 21". */
export function receiptPhrase(s: V2.AskSource, now = new Date()): string {
  const rc = s.receipt;
  if (!rc) return "kept";
  const verb =
    rc.action === "merged" || rc.action === "verified" ? rc.action : "kept";
  let by = "";
  switch (rc.actor_kind) {
    case "agent":
      by = ` by ${agentMark((rc.agent ?? "other") as V2.AgentKind, rc.agent ?? "")}`;
      break;
    case "dream":
      by = " by Dream";
      break;
    case "memax":
      by = " by Memax";
      break;
  }
  const when = clock(rc.occurred_at, now);
  return `${verb}${by}${when ? ` · ${when}` : ""}`;
}

/** Streams one answer and returns the exit code. */
export async function askV2(
  space: V2.Space,
  question: string,
  d: AskV2Deps,
  o: AskV2Options = {},
): Promise<number> {
  const json = o.format === "json";
  const out = (s: string) => {
    if (!json) d.write(s);
  };
  const result: AskV2Result = {
    space: space.slug,
    question,
    outcome: "failed",
    answer: "",
    citations: [],
    sources: [],
  };
  const byRef = new Map<string, V2.AskSource>();
  let wrote = false;
  try {
    for await (const ev of d.memax.v2.ask(space.id, question, {
      signal: d.signal,
    })) {
      switch (ev.event) {
        case "sources":
          result.sources = ev.data.sources;
          for (const s of ev.data.sources) byRef.set(s.ref, s);
          break;
        case "delta":
          result.answer += ev.data.text;
          out(ev.data.text);
          wrote = true;
          break;
        case "cite": {
          const { n, ref } = ev.data;
          result.answer += ` [${n}]`;
          out(d.tty ? chalk.gray(` [${n}]`) : ` [${n}]`);
          if (!result.citations.some((c) => c.n === n)) {
            result.citations.push({
              n,
              ref,
              statement: byRef.get(ref)?.statement ?? "",
            });
          }
          break;
        }
        case "done":
          result.outcome = ev.data.outcome;
          result.usage = ev.data.usage;
          break;
        case "error":
          result.error = ev.data.message;
          break;
      }
    }
  } catch (err) {
    if (d.signal?.aborted) {
      if (wrote) out("\n");
      return 130;
    }
    const refused = refusalOf(err);
    const message =
      refused?.message ??
      (err instanceof MemaxError ? apiFailureMessage(err) : String(err));
    if (json) {
      result.error = message;
      d.write(JSON.stringify(result, null, 2) + "\n");
    } else {
      if (wrote) out("\n");
      d.err(d.tty ? chalk.red(`  ${message}`) : message);
    }
    return 1;
  }

  if (json) {
    d.write(JSON.stringify(result, null, 2) + "\n");
    return result.outcome === "failed" ? 1 : 0;
  }
  if (wrote) out("\n");
  const gray = (s: string) => (d.tty ? chalk.gray(s) : s);
  switch (result.outcome) {
    case "answered": {
      const line = result.citations
        .map((c) => {
          const s = byRef.get(c.ref);
          return `[${c.n}] ${c.ref} ${s ? receiptPhrase(s, d.now) : ""}`.trimEnd();
        })
        .join("   ");
      if (line) out(gray(line) + "\n");
      return 0;
    }
    case "unsupported":
      d.err(
        (d.tty ? chalk.yellow : (s: string) => s)(
          `  That answer cites nothing kept in ${space.name}, so it isn't one. Nothing kept answers it yet.`,
        ),
      );
      return 0;
    case "not_covered":
      out(
        gray(
          `Nothing kept in ${space.name} answers that yet. Remember it with memax remember.`,
        ) + "\n",
      );
      return 0;
    case "sources_only":
      out(
        gray("Answers are off on this server. Kept memories that match:") +
          "\n",
      );
      result.sources.forEach((s, i) => {
        out(`[${i + 1}] ${s.statement}\n`);
        out(gray(`    ${s.ref} ${receiptPhrase(s, d.now)}`) + "\n");
      });
      if (result.sources.length === 0) out(gray("None.") + "\n");
      return 0;
    default:
      d.err(
        d.tty
          ? chalk.red(
              `  ${result.error ?? "The answer stopped partway. Ask again."}`,
            )
          : (result.error ?? "The answer stopped partway. Ask again."),
      );
      return 1;
  }
}
