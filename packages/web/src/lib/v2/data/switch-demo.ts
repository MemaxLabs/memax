import { CommandFailedError } from "./command-error";
import {
  DEMO_SWITCH_PREVIEW,
  DEMO_SWITCHED_AT,
  DEMO_V1_IMPORT_ID,
  DEMO_V1_SPACE,
} from "./demo-switch-data";
import type { SwitchSource, SwitchView } from "./switch";

/**
 * The demo's Switch to V2: acme-web is on V1 until the person switches
 * it in this tab's session. The switch runs "in the background" for one
 * read of the status, as the server's does when there's an import to
 * judge, then lands with the V1 import in Review; switching back puts
 * it on V1 again. Every other demo space is on V2 already.
 */

export interface DemoSwitch extends SwitchSource {
  /** Whether a space is on V2 in this session (the demo's spaces list reads it). */
  onV2(slug: string): boolean;
}

const DONE = {
  notes: 112,
  proposed: 7,
  targets: ["agents_md", "claude_md"],
  connected: 2,
  notified: 2,
};

export function createDemoSwitch({
  commandDelayMs = 0,
}: { commandDelayMs?: number } = {}): DemoSwitch {
  let state: SwitchView["state"] = "v1";
  /** Status reads left before a running switch lands. */
  let pending = 0;
  const delay = () =>
    new Promise<void>((r) => setTimeout(r, Math.max(0, commandDelayMs)));
  const view = (): SwitchView => ({
    state,
    step:
      state === "switched" || state === "off"
        ? "done"
        : state === "running"
          ? "candidates"
          : "space",
    preview: structuredClone(DEMO_SWITCH_PREVIEW),
    progress:
      state === "switched" || state === "off"
        ? structuredClone(DONE)
        : { notes: 0, proposed: 0, targets: [], connected: 0, notified: 0 },
    importId: state === "v1" ? null : DEMO_V1_IMPORT_ID,
    error: null,
    switchedAt: state === "v1" ? null : DEMO_SWITCHED_AT,
  });
  const ours = (slug: string) => slug === DEMO_V1_SPACE.slug;

  return {
    onV2: (slug) => !ours(slug) || state === "switched",
    async status({ space }) {
      if (!ours(space.slug)) {
        throw new CommandFailedError({ kind: "not-found" });
      }
      if (state === "running" && --pending <= 0) state = "switched";
      return view();
    },
    async toV2({ space }) {
      await delay();
      if (!ours(space.slug)) {
        throw new CommandFailedError({ kind: "not-found" });
      }
      if (state !== "switched") {
        state = "running";
        pending = 1;
      }
      return view();
    },
    async toV1({ space }) {
      await delay();
      if (!ours(space.slug)) {
        throw new CommandFailedError({ kind: "not-found" });
      }
      if (state === "switched") state = "off";
      return view();
    },
  };
}
