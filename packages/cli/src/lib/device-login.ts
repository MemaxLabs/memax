// Device-code sign-in (RFC 8628; plan 25 §5.15, §7.3 step 2): for a
// machine where no browser can open, such as over SSH, in a container or
// on a headless box. The CLI asks for a code, shows it with
// memax.app/device, and polls until the person confirms it on the web
// (CliAuth), declines it, or it runs out. The session it collects is the
// CLI's own (surface cli), the same as `memax login` in a browser gives.
import type {
  AuthTokenPair,
  DeviceSignIn,
  DeviceSignInOptions,
  DeviceSignInPoll,
} from "memax-sdk";

/** What the flow needs from the SDK's public (signed-out) client. */
export interface DeviceAuthClient {
  startDeviceSignIn(options?: DeviceSignInOptions): Promise<DeviceSignIn>;
  pollDeviceSignIn(
    deviceCode: string,
    options?: { signal?: AbortSignal },
  ): Promise<DeviceSignInPoll>;
}

export interface DeviceLoginDeps {
  auth: DeviceAuthClient;
  /** Prints one line. */
  out: (line: string) => void;
  sleep: (ms: number) => Promise<void>;
  /** Milliseconds, monotonic. */
  now: () => number;
  /** Opens a URL in a browser, when this machine has one; never throws. */
  open?: (url: string) => void;
  /** What the CLI says about itself on the confirmation page. */
  device: Omit<DeviceSignInOptions, "signal">;
  signal?: AbortSignal;
}

export type DeviceLoginResult =
  | { ok: true; tokens: AuthTokenPair }
  | { ok: false; reason: "denied" | "expired" | "invalid" | "aborted" };

/** How much longer slow_down asks for (RFC 8628 §3.5). */
const SLOW_DOWN_MS = 5_000;

/**
 * Signs in with a device code: prints the code and where to confirm it,
 * then polls until it ends. Network failures while polling are retried
 * at the same pace until the code runs out.
 */
export async function signInWithDeviceCode(
  d: DeviceLoginDeps,
): Promise<DeviceLoginResult> {
  const code = await d.auth.startDeviceSignIn({
    ...d.device,
    signal: d.signal,
  });
  const where = code.verificationUri.replace(/^https?:\/\//, "");
  d.out("");
  d.out(`  Open ${where} and confirm this code:`);
  d.out("");
  d.out(`      ${code.userCode}`);
  d.out("");
  if (d.open && code.verificationUriComplete) {
    d.open(code.verificationUriComplete);
  }
  d.out(
    `  Waiting for you in the browser… The code lasts ${Math.round(code.expiresIn / 60)} minutes.`,
  );

  const deadline = d.now() + code.expiresIn * 1000;
  let interval = Math.max(1, code.interval) * 1000;
  while (d.now() < deadline) {
    if (d.signal?.aborted) return { ok: false, reason: "aborted" };
    await d.sleep(interval);
    if (d.signal?.aborted) return { ok: false, reason: "aborted" };
    let poll: DeviceSignInPoll;
    try {
      poll = await d.auth.pollDeviceSignIn(code.deviceCode, {
        signal: d.signal,
      });
    } catch (err) {
      if (d.signal?.aborted) return { ok: false, reason: "aborted" };
      // The network, or the server between deploys: keep the pace.
      void err;
      continue;
    }
    switch (poll.status) {
      case "signed_in":
        return { ok: true, tokens: poll.tokens };
      case "pending":
        continue;
      case "slow_down":
        interval += SLOW_DOWN_MS;
        continue;
      case "denied":
      case "expired":
      case "invalid":
        return { ok: false, reason: poll.status };
    }
  }
  return { ok: false, reason: "expired" };
}

/** The sentence for a sign-in that didn't happen. */
export function deviceLoginFailure(
  reason: Exclude<DeviceLoginResult, { ok: true }>["reason"],
): string {
  switch (reason) {
    case "denied":
      return "The code was declined in the browser, so nothing was signed in. Run memax login again for a new code.";
    case "expired":
      return "The code ran out before it was confirmed. Run memax login again for a new one.";
    case "invalid":
      return "Memax didn't accept that code any more. Run memax login again for a new one.";
    case "aborted":
      return "Sign-in stopped.";
  }
}

export interface BrowserEnv {
  platform: NodeJS.Platform;
  env: Record<string, string | undefined>;
}

/**
 * Whether a browser can open for the person at this terminal. Over SSH a
 * browser would open on the remote machine, not in front of them; a Linux
 * box with no display has none; CI has no person.
 */
export function canOpenBrowser({ platform, env }: BrowserEnv): boolean {
  if (env.SSH_CONNECTION || env.SSH_CLIENT || env.SSH_TTY) return false;
  if (env.CI) return false;
  if (platform === "darwin" || platform === "win32") return true;
  return !!(env.DISPLAY || env.WAYLAND_DISPLAY || env.BROWSER);
}
