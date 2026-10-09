import type { Locale } from "@/i18n";
import { interpolate } from "@/i18n/interpolate";
import type { Translations } from "@/i18n/locales/en";
import { count, joinList } from "./copy";
import type { SwitchPreviewView } from "./data/switch";

/**
 * The words of "Switch to V2" (Today of a space still on V1): one row per
 * thing that moves, from the preview's counts, each left out when there's
 * nothing of it. The catalogue is today-en.ts `switch` (today-zh.ts).
 */

export type SwitchCopy = Translations["ledger"]["today"]["switch"];

export interface SwitchRow {
  key: string;
  label: string;
  text: string;
}

function targetLabel(s: SwitchCopy, kind: string): string {
  return (s.targets as Record<string, string>)[kind] ?? kind;
}

export function switchRows(
  s: SwitchCopy,
  pv: SwitchPreviewView,
  locale: Locale,
): SwitchRow[] {
  const n = pv.notes;
  const rows: SwitchRow[] = [
    {
      key: "notes",
      label: s.notes,
      text:
        n.total === 0
          ? s.notesNone
          : count(s.notesTextOne, s.notesText, n.total),
    },
  ];
  if (n.candidates > 0)
    rows.push({
      key: "review",
      label: s.review,
      text: count(s.reviewTextOne, s.reviewText, n.candidates),
    });
  if (n.fold > 0)
    rows.push({
      key: "dream",
      label: s.dream,
      text: count(s.dreamTextOne, s.dreamText, n.fold),
    });
  if (n.kept > 0) {
    const why = [
      n.archived > 0 ? interpolate(s.archived, { n: n.archived }) : "",
      n.secret > 0 ? count(s.secretOne, s.secret, n.secret) : "",
    ].filter(Boolean);
    rows.push({
      key: "notes-only",
      label: s.notesOnly,
      text: interpolate(s.notesOnlyText, {
        n: n.kept,
        why: joinList(why, locale),
      }),
    });
  }
  if (pv.personas > 0)
    rows.push({
      key: "personas",
      label: s.personas,
      text: count(s.personasTextOne, s.personasText, pv.personas),
    });
  rows.push({
    key: "people",
    label: s.people,
    text:
      pv.members.length <= 1
        ? s.peopleOne
        : interpolate(s.peopleText, { n: pv.members.length }),
  });
  const joining = pv.agents.filter((a) => !a.connected);
  if (joining.length > 0)
    rows.push({
      key: "agents",
      label: s.agents,
      text: interpolate(s.agentsText, {
        list: joinList(
          joining.map((a) =>
            interpolate(s.agentAt, {
              name: a.name,
              level: s.levels[a.autonomy],
            }),
          ),
          locale,
        ),
      }),
    });
  if (pv.targets.length > 0)
    rows.push({
      key: "files",
      label: s.files,
      text: interpolate(s.filesText, {
        files: joinList(
          pv.targets.map((t) => targetLabel(s, t)),
          locale,
        ),
      }),
    });
  if (pv.gates > 0)
    rows.push({
      key: "decisions",
      label: s.decisions,
      text: count(s.decisionsTextOne, s.decisionsText, pv.gates),
    });
  if (pv.dreamRuns > 0)
    rows.push({
      key: "history",
      label: s.history,
      text: count(s.historyTextOne, s.historyText, pv.dreamRuns),
    });
  if (pv.plan)
    rows.push({
      key: "plan",
      label: s.plan,
      text: interpolate(s.planText, { plan: pv.plan }),
    });
  return rows;
}
