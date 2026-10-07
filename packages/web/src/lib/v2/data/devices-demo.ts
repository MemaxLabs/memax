import {
  DeviceCommandError,
  normalizeUserCode,
  type DeviceRequestView,
  type DevicesSource,
} from "./devices";

/**
 * The demo's device sign-in (CliAuth.png): ziyang-mbp asked with
 * WQRT-4821 twelve seconds before the demo's clock. Confirming it
 * signs the demo CLI in a moment later; any other code is unknown.
 */
export const DEMO_DEVICE_CODE = "WQRT-4821";

export function createDemoDevices({
  now,
  commandDelayMs = 0,
  signInAfterMs = 600,
}: {
  now: () => Date;
  commandDelayMs?: number;
  /** How long after a confirmation the demo CLI collects its session. */
  signInAfterMs?: number;
}): DevicesSource {
  const asked = new Date(now().getTime() - 12_000);
  let request: DeviceRequestView = {
    userCode: DEMO_DEVICE_CODE,
    state: "pending",
    clientId: "memax-cli",
    clientVersion: "2.0.0",
    deviceName: "ziyang-mbp",
    deviceOs: "macOS",
    space: "memax-v2",
    address: "203.0.113.4",
    requestedAt: asked.toISOString(),
    expiresAt: new Date(asked.getTime() + 10 * 60_000).toISOString(),
    decidedAt: null,
    signedInAt: null,
  };
  let approvedAt: number | null = null;
  const delay = () =>
    new Promise<void>((r) => setTimeout(r, Math.max(0, commandDelayMs)));
  const current = (): DeviceRequestView => {
    if (
      request.state === "approved" &&
      approvedAt !== null &&
      Date.now() - approvedAt >= signInAfterMs
    ) {
      request = {
        ...request,
        state: "signed_in",
        signedInAt: request.decidedAt,
      };
    }
    return { ...request };
  };
  const find = (code: string) => {
    if (normalizeUserCode(code) !== DEMO_DEVICE_CODE) {
      throw new DeviceCommandError("not_found");
    }
    return current();
  };
  const decide = async (code: string, to: "approved" | "denied") => {
    await delay();
    const r = find(code);
    const same =
      r.state === to || (to === "approved" && r.state === "signed_in");
    if (same) return r;
    if (r.state !== "pending") {
      throw new DeviceCommandError("decided", { state: r.state });
    }
    request = { ...r, state: to, decidedAt: now().toISOString() };
    if (to === "approved") approvedAt = Date.now();
    return { ...request };
  };
  return {
    peek: (code) => {
      try {
        return find(code);
      } catch {
        return null;
      }
    },
    lookup: async ({ userCode }) => find(userCode),
    approve: ({ userCode }) => decide(userCode, "approved"),
    deny: ({ userCode }) => decide(userCode, "denied"),
  };
}
