import { CommandFailedError } from "./command-error";
import { DEMO_MACHINES } from "./demo-machines-data";
import {
  isOnline,
  validMachineName,
  type MachinesSource,
  type MachineView,
} from "./machines";

/**
 * The demo's machines (demo-machines-data.ts) behind MachinesSource, with
 * this browser session's renames and unlinks applied. A rename checks the
 * version (If-Match) like the server; an unlink stops every file of the
 * machine in the same step (`stopMachine`), and a machine unlinked
 * already changes nothing. Commands replay by idempotency key.
 */

function sleep(ms: number) {
  return new Promise<void>((resolve) => setTimeout(resolve, ms));
}

export interface DemoMachines extends MachinesSource {
  /** Whether the machine's daemon writes now: online and linked. */
  online(slug: string, machineId: string): boolean;
}

export function createDemoMachines({
  now,
  commandDelayMs = 240,
  stopMachine,
}: {
  now: () => Date;
  commandDelayMs?: number;
  /** Stops every file of the machine (targets-demo.ts). */
  stopMachine: (slug: string, machineId: string) => void;
}): DemoMachines {
  const lists = new Map<string, MachineView[]>(
    Object.entries(DEMO_MACHINES).map(([slug, list]) => [slug, [...list]]),
  );
  const replays = new Map<string, MachineView>();
  const listOf = (slug: string) => lists.get(slug) ?? [];
  const find = (slug: string, id: string) => {
    const found = listOf(slug).find((m) => m.id === id);
    if (!found) throw new CommandFailedError({ kind: "not-found" });
    return found;
  };
  const put = (slug: string, next: MachineView) => {
    lists.set(
      slug,
      listOf(slug).map((m) => (m.id === next.id ? next : m)),
    );
    return next;
  };

  async function command(
    key: string,
    run: () => MachineView,
  ): Promise<MachineView> {
    if (commandDelayMs > 0) await sleep(commandDelayMs);
    const replay = replays.get(key);
    if (replay) return replay;
    const result = run();
    replays.set(key, result);
    return result;
  }

  return {
    online(slug, machineId) {
      const machine = listOf(slug).find((m) => m.id === machineId);
      return machine ? isOnline(machine, now()) : true;
    },
    peekList: (slug) => lists.get(slug),
    async list({ space }) {
      // Only a personal space is written to machines; the server refuses others.
      if (space.kind !== "personal") {
        throw new CommandFailedError({
          kind: "refused",
          code: "machines_personal",
          message: null,
        });
      }
      return listOf(space.slug);
    },
    rename({ space, machine, name, idempotencyKey }) {
      return command(idempotencyKey, () => {
        const live = find(space.slug, machine.id);
        if (live.version !== machine.version) {
          throw new CommandFailedError({
            kind: "clash",
            currentVersion: live.version,
          });
        }
        const next = validMachineName(name);
        if (next === null) {
          throw new CommandFailedError({ kind: "unknown", message: null });
        }
        // The same name writes nothing.
        if (next === live.name) return live;
        return put(space.slug, {
          ...live,
          name: next,
          version: live.version + 1,
        });
      });
    },
    unlink({ space, machine, idempotencyKey }) {
      return command(idempotencyKey, () => {
        const live = find(space.slug, machine.id);
        if (!live.linked) return live;
        stopMachine(space.slug, live.id);
        return put(space.slug, {
          ...live,
          linked: false,
          unlinkedAt: now().toISOString(),
          version: live.version + 1,
        });
      });
    },
  };
}
