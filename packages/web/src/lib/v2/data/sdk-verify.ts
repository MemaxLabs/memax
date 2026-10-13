import { renameSuggestion, type V2 } from "memax-sdk";
import type {
  StaleCause,
  StaleWhy,
  VerifyAnswer,
  VerifyResult,
} from "./memories";
import { conditionsOf } from "./sdk-record";
import { actorOf } from "./sdk-records";

/**
 * Verify through memax.v2: why a memory is stale (GET
 * /v2/memories/{ref}/stale) and a person's answer (POST :verify). Pure
 * mappers, so the rules are unit-tested.
 */

/** The report's causes the screen can say; one it doesn't know yet is left out. */
function causeOf(cause: V2.StaleCause): StaleCause[] {
  switch (cause.kind) {
    case "date":
      return [{ kind: "date", staleAfter: cause.stale_after }];
    case "check": {
      const [condition] = conditionsOf([cause.condition]);
      if (!condition || condition.kind === "date") return [];
      return [
        {
          kind: "check",
          condition,
          change: cause.evidence?.change ?? "removed",
          path: cause.evidence?.path ?? null,
          renamedTo: cause.evidence?.renamed_to ?? null,
          constraint: cause.evidence?.constraint ?? null,
          branch: cause.branch,
          commit: cause.commit,
          checkedAt: cause.checked_at ?? null,
        },
      ];
    }
    default:
      return [];
  }
}

export function staleWhyOf(
  report: V2.StaleReport,
  viewerId: string | undefined,
): StaleWhy {
  return {
    ref: report.ref,
    version: report.version,
    statement: report.statement,
    kind: report.kind,
    stale: report.stale,
    since: report.flagged?.occurred_at ?? null,
    flaggedBy: report.flagged ? actorOf(report.flagged, viewerId) : null,
    kept: report.kept
      ? {
          by: actorOf(report.kept, viewerId),
          at: report.kept.occurred_at,
          action: report.kept.action === "verified" ? "verified" : "kept",
        }
      : null,
    readsSince: report.reads_since,
    causes: report.causes.flatMap(causeOf),
    // A rename's new words need no model: the old path replaced by the new
    // one (the SDK's renameSuggestion, as memax verify offers it). The
    // judge's suggestions come later (stale design §3.2).
    suggestion: renameSuggestionOf(report),
  };
}

/** The first renamed cause's new words, from Memax, citing its commit. */
function renameSuggestionOf(report: V2.StaleReport): StaleWhy["suggestion"] {
  if (!report.stale) return null;
  for (const c of report.causes) {
    if (c.kind !== "check" || c.evidence?.change !== "renamed") continue;
    const from =
      c.evidence.path ??
      (c.condition.kind === "path_exists"
        ? (c.condition.path ?? "")
        : "path" in c.condition
          ? c.condition.path
          : "");
    const statement = renameSuggestion(
      report.statement,
      from,
      c.evidence.renamed_to ?? "",
    );
    if (statement)
      return { statement, by: { kind: "memax" }, from: c.commit.slice(0, 7) };
  }
  return null;
}

export function verifyResultOf(
  answer: VerifyAnswer,
  result: V2.VerifyResult,
): VerifyResult {
  return {
    ref: result.memory.ref,
    answer,
    version: result.memory.version,
    // Still true and an update are undoable by any of their receipts; a
    // Forget never is.
    receipt:
      answer === "forget"
        ? null
        : (result.receipts.find((r) => r.action === "verified")?.id ?? null),
  };
}

/** The body of :verify for an answer. */
export function verifyBody(input: {
  answer: VerifyAnswer;
  statement?: string;
  carries?: string[];
  note?: string;
}): V2.VerifyInput {
  if (input.answer === "forget") {
    const body: V2.VerifyInput = {
      answer: "forget",
      carries: input.carries ?? [],
    };
    if (input.note?.trim()) body.note = input.note.trim();
    return body;
  }
  if (input.answer === "update") {
    return { answer: "update", statement: input.statement ?? "" };
  }
  return { answer: "still_true" };
}
