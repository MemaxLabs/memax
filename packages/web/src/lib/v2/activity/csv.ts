import type { ActivityActor, ActivityEntry } from "../data/activity";

/**
 * Activity as CSV (RFC 4180), built in the browser from the receipts
 * the page has loaded. The columns are the receipt's own fields, named
 * as the API names them, so the file is data rather than prose and
 * reads the same in any locale. A receipt never holds a memory's words,
 * so neither does the file. Reads (R-) aren't receipts and stay out.
 *
 * PLACEHOLDER: a full export (every receipt, with the sealer's signed
 * checkpoints, plan §5.3) needs a server endpoint. /v2 serves the
 * checkpoints (Activity shows how far they reach); the export that
 * carries them and `memax verify-export` aren't built.
 */

export const CSV_COLUMNS = [
  "occurred_at",
  "actor_kind",
  "actor",
  "action",
  "object_kind",
  "object_ref",
  "via",
  "assurance",
  "session",
  "source",
  "reason",
  "receipt_id",
] as const;

function actorFields(actor: ActivityActor): [string, string] {
  switch (actor.kind) {
    case "you":
      return ["person", actor.initials];
    case "person":
      return ["person", actor.initials ?? actor.name ?? ""];
    case "agent":
      return ["agent", actor.agent];
    default:
      return [actor.kind, ""];
  }
}

/** One cell: quoted when it has a comma, quote or line break; formulas defused. */
export function csvCell(value: string): string {
  // A leading =, +, -, @ (or a tab or carriage return) makes spreadsheets
  // run the cell as a formula; a leading apostrophe keeps it text.
  const safe = /^[=+\-@\t\r]/.test(value) ? `'${value}` : value;
  return /[",\r\n]/.test(safe) ? `"${safe.replace(/"/g, '""')}"` : safe;
}

export function activityRow(entry: ActivityEntry): string[] {
  const [actorKind, actor] = actorFields(entry.actor);
  return [
    entry.at,
    actorKind,
    actor,
    entry.action,
    entry.object.kind,
    entry.object.ref,
    entry.rawVia ?? "",
    entry.assurance ?? "",
    entry.session ?? "",
    entry.source ? `${entry.source.kind}:${entry.source.ref}` : "",
    entry.reason ?? "",
    entry.id,
  ];
}

/** The whole file: a byte-order mark (so spreadsheets read UTF-8), a header, CRLF lines. */
export function activityCsv(entries: readonly ActivityEntry[]): string {
  const lines = [
    CSV_COLUMNS.join(","),
    ...entries.map((e) => activityRow(e).map(csvCell).join(",")),
  ];
  return `﻿${lines.join("\r\n")}\r\n`;
}

/** "memax-v2-activity-2026-10-05.csv". */
export function activityCsvName(slug: string, now: Date): string {
  const day = now.toISOString().slice(0, 10);
  return `${slug}-activity-${day}.csv`;
}
