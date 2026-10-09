import type { V2 } from "memax-sdk";
import type {
  ConflictChange,
  ConflictData,
  ConflictOption,
  ConflictOptionKind,
  ConflictSide,
} from "./review";
import { actorOf, sessionOf } from "./sdk-records";

/**
 * GET /v2/memories/{ref}/conflict as ReviewConflict reads it. Pure, so
 * the mapping rules are unit-tested. The server answers relative to the
 * memory in the path; the screen always puts the decision in force on
 * the left (`kept`) and the flagged side on the right (`proposal`), so
 * keep_this / keep_other become `proposal` / `kept` by which side the
 * path named.
 *
 * The judge writes a question, a label per answer and a suggested answer
 * when it found the conflict; a conflict found otherwise (an import, Dream)
 * has none, and the screen words them from the catalogue and preselects
 * nothing. What the server doesn't serve: compiled files and the agents
 * told (PLACEHOLDER: compile runs aren't served to Review yet).
 */

/** The board's order: 1 the proposal, 2 as kept, 3 both, 4 open. */
const ORDER: ConflictOptionKind[] = ["proposal", "kept", "both", "open"];

/** The answer the screen means, as the server's choice for `ref`. */
export function choiceFor(option: ConflictOptionKind): V2.ConflictChoice {
  switch (option) {
    case "proposal":
      return "keep_this";
    case "kept":
      return "keep_other";
    case "both":
      return "keep_both";
    case "open":
      return "leave_open";
  }
}

function kindOf(
  choice: V2.ConflictChoice,
  thisIsFlagged: boolean,
): ConflictOptionKind {
  switch (choice) {
    case "keep_this":
      return thisIsFlagged ? "proposal" : "kept";
    case "keep_other":
      return thisIsFlagged ? "kept" : "proposal";
    case "keep_both":
      return "both";
    case "leave_open":
      return "open";
  }
}

function sideOf(
  memory: V2.Memory,
  receipts: readonly V2.Receipt[],
  role: "kept" | "proposal",
  viewerId: string | undefined,
): ConflictSide {
  // Newest first (spec); the receipt that made it what it is: its Keep
  // for the decision in force, its proposal for the flagged side.
  const own = receipts.filter((r) => r.object_id === memory.id);
  const made =
    role === "kept"
      ? own.find((r) => r.action === "kept")
      : (own.find((r) => r.id === memory.created_receipt_id) ??
        own.find((r) => r.action === "proposed"));
  const receipt = made ?? own[0];
  const sources = memory.sources ?? [];
  const file = sources.find((s) => s.kind === "file" || s.kind === "pr");
  return {
    ref: memory.ref,
    version: memory.version,
    statement: memory.statement,
    by: actorOf(receipt, viewerId),
    at: receipt?.occurred_at ?? memory.created_at,
    why: memory.decision?.why?.trim() || null,
    source: role === "kept" ? (sources[0]?.ref ?? null) : null,
    // PLACEHOLDER: the conflict carries no reach (files and reads); the
    // kept side's reads are on its own GET /v2/memories/{ref}.
    reaches: null,
    evidence:
      role === "proposal" && file
        ? { code: file.ref, changedAt: file.created_at }
        : null,
    session: role === "proposal" ? sessionOf(receipt) : null,
  };
}

function optionOf(
  option: V2.ConflictOption,
  kind: ConflictOptionKind,
  kept: V2.Memory,
  flagged: V2.Memory,
): ConflictOption {
  return {
    kind,
    label: option.label?.trim() || null,
    detail: null,
    effects: option.effects.map((e) => ({
      ref: e.ref,
      change: e.change as ConflictChange,
    })),
    allowed: option.allowed,
    refusal: option.allowed
      ? null
      : {
          code: option.policy?.code ?? null,
          message: option.policy?.message ?? null,
        },
    decision:
      kind === "proposal"
        ? flagged.statement
        : kind === "kept"
          ? kept.statement
          : "",
    narrowed:
      kind === "both"
        ? { proposal: flagged.statement, kept: kept.statement }
        : null,
  };
}

export function conflictOf(
  conflict: V2.Conflict,
  viewerId: string | undefined,
): ConflictData {
  const thisIsFlagged = conflict.memory.ref === conflict.flagged_ref;
  const flagged = thisIsFlagged ? conflict.memory : conflict.other;
  const kept = thisIsFlagged ? conflict.other : conflict.memory;
  const options = conflict.options
    .map((o) => optionOf(o, kindOf(o.choice, thisIsFlagged), kept, flagged))
    .sort((a, b) => ORDER.indexOf(a.kind) - ORDER.indexOf(b.kind));
  // The judge's suggestion is preselected, when the person may take it.
  const suggestedKind = conflict.suggested
    ? kindOf(conflict.suggested, thisIsFlagged)
    : null;
  const suggested = options.findIndex(
    (o) => o.kind === suggestedKind && o.allowed,
  );
  return {
    question: conflict.question?.trim() || null,
    area: kept.decision?.area?.trim() || flagged.decision?.area?.trim() || null,
    kept: sideOf(kept, conflict.receipts, "kept", viewerId),
    proposal: sideOf(flagged, conflict.receipts, "proposal", viewerId),
    options,
    suggested: suggested < 0 ? null : suggested,
    // PLACEHOLDER: compile runs and the agents told aren't served yet.
    recompiles: null,
    tells: [],
  };
}
