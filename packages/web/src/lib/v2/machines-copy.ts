/**
 * The words for a Personal space's machines and home files (personal-
 * layer design §8): a home file's status across its machines ("In sync
 * on 2 machines", "On 1 of 2 machines · home-linux last online 3 days
 * ago"), one machine's copy, when a machine was last online, what each
 * machine is. Pure, catalogue in and string out, so both locales are
 * unit-tested.
 */
import { interpolate } from "@/i18n/interpolate";
import type { Locale } from "@/i18n";
import type { Translations } from "@/i18n/locales/en";
import { editedWhen, sameDay, type BriefCopy } from "./brief-copy";
import { count, formatShortDate, joinList } from "./copy";
import {
  homeAgent,
  type HomeFileGroup,
  type HomeFileStatus,
  type MachineFileState,
} from "./data/home-files";
import type { MachineView } from "./data/machines";
import type { DriftItemView, TargetView } from "./data/targets";

export type MachinesCopy = Translations["ledger"]["machines"];

/** The StateMark a status wears (Ledger's names; lib/ stays free of the Ledger). */
export type MarkState = "kept" | "proposed" | "working" | "off";

interface When {
  now: Date;
  timeZone: string;
  locale: Locale;
}

/** "macOS", "Linux"; an unknown platform as the CLI reported it. */
export function platformName(m: MachinesCopy, platform: string): string {
  return (m.platform as Record<string, string>)[platform] ?? platform;
}

/**
 * When a machine's daemon last read its files: "online now" within the
 * quarter hour, then minutes, hours, yesterday, days, and a date after a
 * week. "not online yet" before it ever has.
 */
export function seenText(
  m: MachinesCopy,
  lastSeenAt: string | null,
  { now, timeZone, locale }: When,
  online: boolean,
): string {
  const s = m.seen;
  if (online) return s.now;
  if (!lastSeenAt) return s.never;
  const then = new Date(lastSeenAt);
  const minutes = Math.max(
    1,
    Math.round((now.getTime() - then.getTime()) / 60_000),
  );
  if (minutes < 60) return interpolate(s.minutes, { n: minutes });
  if (sameDay(then, now, timeZone)) {
    return interpolate(s.hours, { n: Math.round(minutes / 60) });
  }
  const yesterday = new Date(now.getTime() - 24 * 60 * 60_000);
  if (sameDay(then, yesterday, timeZone)) return s.yesterday;
  const days = Math.max(2, Math.round(minutes / (24 * 60)));
  if (days < 7) return interpolate(s.days, { n: days });
  return interpolate(s.on, {
    date: formatShortDate(then, timeZone, locale),
  });
}

/** What a home file is for: its agent's name, or "Canonical copy". */
export function homeToolName(
  m: MachinesCopy,
  group: Pick<HomeFileGroup, "agent">,
  agentName: (key: string) => string,
): string {
  return group.agent ? agentName(group.agent) : m.canonical;
}

export interface StatusText {
  /** The state mark's word: "In sync on 2 machines". */
  label: string;
  /** What the status leaves out, on its own line: "home-linux last online 3 days ago". */
  detail: string | null;
  state: MarkState;
}

/**
 * A home file's status across its machines, as a list row reads it. A
 * file behind on some machine says which, and why when it knows: the
 * machine hasn't been online (with when it last was), or its daemon
 * hasn't written it yet.
 */
export function homeStatusText(
  m: MachinesCopy,
  status: HomeFileStatus,
  when: When,
): StatusText {
  const s = m.status;
  switch (status.kind) {
    case "in_sync":
      return {
        label: status.machine
          ? interpolate(s.inSyncOn, { machine: status.machine })
          : interpolate(s.inSync, { n: status.machines }),
        detail: null,
        state: "kept",
      };
    case "drifted":
      return {
        label:
          status.machines.length === 1
            ? interpolate(s.drifted, { machine: status.machines[0]! })
            : interpolate(s.driftedMany, { n: status.machines.length }),
        detail: null,
        state: "proposed",
      };
    case "held":
      return {
        label:
          status.machines.length === 1
            ? interpolate(s.held, { machine: status.machines[0]! })
            : interpolate(s.heldMany, { n: status.machines.length }),
        detail: null,
        state: "proposed",
      };
    case "compiling":
      return { label: s.compiling, detail: null, state: "working" };
    case "off":
      return { label: s.off, detail: null, state: "off" };
    case "behind": {
      const why = status.offline
        ? interpolate(s.machineSeen, {
            machine: status.offline.machine,
            seen: seenText(m, status.offline.since, when, false),
          })
        : status.waiting
          ? interpolate(s.waitingFor, { machine: status.waiting })
          : null;
      // On its one machine, the reason is the status.
      if (status.of === 1 && why) {
        return { label: why, detail: null, state: "working" };
      }
      return {
        label: interpolate(s.behind, {
          reached: status.reached,
          of: status.of,
        }),
        detail: why,
        state: "working",
      };
    }
  }
}

/** One machine's copy of a file, as the switcher and a machine's row read it. */
export function machineStateText(
  m: MachinesCopy,
  state: MachineFileState,
): { label: string; state: MarkState } {
  const w = m.state;
  switch (state.kind) {
    case "in_sync":
      return { label: w.in_sync, state: "kept" };
    case "compiling":
      return { label: w.compiling, state: "working" };
    case "waiting":
      return { label: w.waiting, state: "working" };
    case "offline":
      return { label: w.offline, state: "working" };
    case "drifted":
      return { label: w.drifted, state: "proposed" };
    case "held":
      return { label: w.held, state: "proposed" };
    case "stopped":
      return { label: w.stopped, state: "off" };
    case "unlinked":
      return { label: w.unlinked, state: "off" };
  }
}

/** A machine's second line: "macOS · Claude Code and Codex", or that no agent's file is on it. */
export function machineMeta(
  m: MachinesCopy,
  machine: Pick<MachineView, "platform">,
  agents: readonly string[],
  agentName: (key: string) => string,
  locale: Locale,
): string {
  const who = agents.length
    ? joinList(agents.map(agentName), locale)
    : m.panel.noFiles;
  return `${platformName(m, machine.platform)} · ${who}`;
}

/** "1 linked", "2 linked". */
export function linkedText(m: MachinesCopy, n: number): string {
  return count(m.panel.linkedOne, m.panel.linked, n);
}

/**
 * The Brief's drift notice for one machine's home file: "Codex's copy on
 * work-mbp has one local edit. Someone changed ~/.codex/AGENTS.md by hand
 * on work-mbp today at 11:20. Memax never overwrites a hand edit without
 * asking."
 */
export function homeDriftNotice(
  m: MachinesCopy,
  b: BriefCopy,
  target: TargetView,
  machine: string,
  item: DriftItemView | undefined,
  agentName: (key: string) => string,
  when: When,
): { title: string; body: string } {
  const d = m.drift;
  const agent = homeAgent(target.kind);
  const edits = Math.max(1, target.openDrift);
  const file = target.label;
  const title = agent
    ? count(d.notice, d.noticeMany, edits, { agent: agentName(agent), machine })
    : count(d.noticeFile, d.noticeFileMany, edits, { file, machine });
  const how = item
    ? interpolate(d.noticeBody, {
        file,
        machine,
        when: editedWhen(b, item.observedAt, when),
      })
    : null;
  return {
    title,
    body: [how, b.drift.never]
      .filter(Boolean)
      .join(when.locale === "zh" ? "" : " "),
  };
}
