/**
 * The machines a person's Personal space is written to (personal-layer
 * design §5, §8): each computer they ran `memax link --home` on, with its
 * own home files (home-files.ts groups them by file). Part of
 * LedgerDataSource as `source.machines`; machines-sdk.ts
 * (memax.v2.machines) and machines-demo.ts implement it.
 *
 * Names follow the /v2 contract (openapi/v2.yaml › Machine). A machine's
 * name is personal data: receipts call it by its label ("machine-2"), and
 * people see the name, with the label only where two share a name. No
 * words live here.
 */
import type { SpaceSummary } from "./types";

export interface MachineView {
  id: string;
  /** What receipts call it ("machine-2"), numbered per space; never its name. */
  label: string;
  /** What the person calls it ("work-mbp"); the short host name by default. */
  name: string;
  /** "darwin", "linux", … as the CLI reported it. */
  platform: string;
  /** Linked now; an unlinked machine's files are stopped. */
  linked: boolean;
  linkedAt: string;
  unlinkedAt: string | null;
  /** When its daemon last read its files (to five minutes); null before it has. */
  lastSeenAt: string | null;
  /** The CLI session that linked it last, so Signed in on can say which session writes Personal. */
  sessionId: string | null;
  /** The If-Match of a rename. */
  version: number;
}

export interface MachineCommand {
  space: SpaceSummary;
  machine: MachineView;
  /** One per intent; the same key on a retry (spec: Idempotency-Key). */
  idempotencyKey: string;
}

export interface MachinesSource {
  /** The demo's data, on hand for the first render. */
  peekList?(slug: string): MachineView[] | undefined;
  /** Every machine linked to the space, linked now or not, by number. */
  list(input: {
    space: SpaceSummary;
    signal?: AbortSignal;
  }): Promise<MachineView[]>;
  /** PATCH with If-Match on the machine's version. A clash throws (command-error.ts). */
  rename(input: MachineCommand & { name: string }): Promise<MachineView>;
  /**
   * Takes the space off the machine: every file of it stops, and its
   * daemon removes what Memax wrote there, leaving what was edited by hand.
   */
  unlink(input: MachineCommand): Promise<MachineView>;
}

/** The longest name a machine may have (spec RenameMachineRequest). */
export const MACHINE_NAME_MAX = 80;

/**
 * A daemon notes its machine as seen at most every five minutes while it
 * polls, so a machine seen within three of those is online.
 */
export const ONLINE_WITHIN_MS = 15 * 60_000;

/** Whether the machine's daemon has read its files lately. */
export function isOnline(machine: MachineView, now: Date): boolean {
  if (!machine.linked || !machine.lastSeenAt) return false;
  return now.getTime() - Date.parse(machine.lastSeenAt) <= ONLINE_WITHIN_MS;
}

/** The label's number, for ordering: machine-2 before machine-10. */
function labelNumber(label: string): number {
  const n = Number(label.replace(/^machine-/, ""));
  return Number.isFinite(n) ? n : Number.MAX_SAFE_INTEGER;
}

/** Linked machines first, each group by number (the order they were linked in). */
export function orderMachines<T extends { label: string; linked?: boolean }>(
  machines: readonly T[],
): T[] {
  return [...machines].sort(
    (a, b) =>
      Number(b.linked ?? true) - Number(a.linked ?? true) ||
      labelNumber(a.label) - labelNumber(b.label),
  );
}

/**
 * What a person calls a machine: its name, and its label after it only
 * where another of their machines has the same name ("mbp (machine-2)").
 * `all` is every machine the screen could name, so two of the same name
 * never read alike.
 */
export function machineName(
  machine: { id: string; label: string; name: string },
  all: readonly { id: string; label: string; name: string }[],
): string {
  const key = machine.name.trim().toLowerCase();
  const shared = all.some(
    (m) => m.id !== machine.id && m.name.trim().toLowerCase() === key,
  );
  return shared ? `${machine.name} (${machine.label})` : machine.name;
}

/** A name a rename may send: trimmed, 1 to 80 characters. Null when it isn't one. */
export function validMachineName(draft: string): string | null {
  const name = draft.trim();
  if (name.length === 0 || [...name].length > MACHINE_NAME_MAX) return null;
  return name;
}

/** The machine most recently linked: the one `memax init` just linked, on the first run. */
export function latestLinked(
  machines: readonly MachineView[],
): MachineView | null {
  return (
    machines
      .filter((m) => m.linked)
      .sort((a, b) => Date.parse(b.linkedAt) - Date.parse(a.linkedAt))[0] ??
    null
  );
}
