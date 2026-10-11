import { DEMO_NOW } from "./demo-dataset";
import type { MachineView } from "./machines";

/**
 * The demo's Personal space is written to two machines (personal-layer
 * design §8): Ziyang's laptop, online now, the one the CLI session in
 * Settings › Account runs on; and a Linux desktop at home, last online
 * on Friday night, so the files there are a compile behind. Data, not
 * copy.
 */

const minutesAgo = (n: number) =>
  new Date(Date.parse(DEMO_NOW) - n * 60_000).toISOString();

export const DEMO_MACHINE_IDS = {
  laptop: "0192a7c0-0000-7000-8000-0000000000e1",
  desktop: "0192a7c0-0000-7000-8000-0000000000e2",
} as const;

export const DEMO_MACHINES: Readonly<Record<string, readonly MachineView[]>> = {
  personal: [
    {
      id: DEMO_MACHINE_IDS.laptop,
      label: "machine-1",
      name: "ziyang-mbp",
      platform: "darwin",
      linked: true,
      // Linked again this morning, when `memax init` ran on it.
      linkedAt: "2026-10-05T08:58:00-07:00",
      unlinkedAt: null,
      lastSeenAt: minutesAgo(2),
      // Settings › Account's "memax CLI on ziyang-mbp" (account-demo.ts).
      sessionId: "0192a7c0-0000-7000-8000-0000000005e2",
      version: 1,
    },
    {
      id: DEMO_MACHINE_IDS.desktop,
      label: "machine-2",
      name: "home-linux",
      platform: "linux",
      linked: true,
      linkedAt: "2026-09-30T20:15:00-07:00",
      unlinkedAt: null,
      lastSeenAt: "2026-10-02T21:10:00-07:00",
      sessionId: "0192a7c0-0000-7000-8000-0000000005e4",
      version: 2,
    },
  ],
};
