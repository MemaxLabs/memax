import type { V2 } from "memax-sdk";
import type { MachinesSource, MachineView } from "./machines";
import type { V2Client } from "./sdk-records";

/**
 * A personal space's machines through memax.v2.machines: the list, a
 * rename (PATCH with If-Match on the machine's version) and an unlink
 * (Idempotency-Key). Only the space's owner, signed in, reads or changes
 * them; anyone else is refused (`machines_by_person`, `machines_personal`).
 */

export function machineOf(machine: V2.Machine): MachineView {
  return {
    id: machine.id,
    label: machine.label,
    name: machine.name,
    platform: machine.platform,
    linked: machine.linked,
    linkedAt: machine.linked_at,
    unlinkedAt: machine.unlinked_at ?? null,
    lastSeenAt: machine.last_seen_at ?? null,
    sessionId: machine.session_id ?? null,
    version: machine.version,
  };
}

/**
 * The space's machines, or null when they don't load (a space that isn't
 * personal is refused): the screens that show them in passing (the status
 * line, a file's state) still show without them.
 */
export async function machinesOrNull(
  client: V2Client,
  slug: string,
  signal?: AbortSignal,
): Promise<MachineView[] | null> {
  try {
    const { items } = await client.v2.machines.list(slug, { signal });
    return items.map(machineOf);
  } catch {
    return null;
  }
}

export function createSdkMachines(client: V2Client): MachinesSource {
  return {
    async list({ space, signal }) {
      const { items } = await client.v2.machines.list(space.slug, { signal });
      return items.map(machineOf);
    },

    async rename({ machine, name, idempotencyKey }) {
      const result = await client.v2.machines.rename(
        machine.id,
        { name },
        { idempotencyKey, ifMatch: machine.version },
      );
      return machineOf(result.machine);
    },

    async unlink({ machine, idempotencyKey }) {
      const result = await client.v2.machines.unlink(
        machine.id,
        {},
        { idempotencyKey },
      );
      return machineOf(result.machine);
    },
  };
}
