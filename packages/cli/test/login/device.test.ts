// memax login --device (and init's sign-in where no browser can open)
// against the fake server's device grant: the code is shown, the CLI
// polls at the server's pace, and every way it ends is said plainly.
import { Memax } from "memax-sdk";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  canOpenBrowser,
  deviceLoginFailure,
  signInWithDeviceCode,
  type DeviceLoginDeps,
} from "../../src/lib/device-login.js";
import { FakeV2 } from "../daemon/fake-v2.js";
import { installDevice, type DeviceState } from "./fake-device.js";

let fake: FakeV2;
let st: DeviceState;
let lines: string[];
let sleeps: number[];
let opened: string[];

beforeEach(async () => {
  fake = await new FakeV2().start();
  st = installDevice(fake);
  lines = [];
  sleeps = [];
  opened = [];
});
afterEach(async () => {
  await fake.stop();
});

/** The flow, on the real SDK, with a clock the test moves. */
function deps(
  onSleep: (polls: number) => void = () => {},
  more: Partial<DeviceLoginDeps> = {},
): DeviceLoginDeps {
  const memax = new Memax({ apiUrl: fake.url, maxRetries: 0 });
  return {
    auth: memax.auth,
    out: (l) => lines.push(l),
    now: () => st.now,
    sleep: async (ms) => {
      sleeps.push(ms);
      st.now += ms;
      onSleep(st.polls);
    },
    device: {
      clientVersion: "2.0.0",
      deviceName: "ziyang-mbp",
      deviceOs: "linux",
      space: "memax-v2",
    },
    ...more,
  };
}

describe("signing in with a device code", () => {
  it("shows the code, waits for the browser, and collects the session", async () => {
    const result = await signInWithDeviceCode(
      deps((polls) => polls === 2 && st.approve("WQRT-4821"), {
        open: (url) => opened.push(url),
      }),
    );
    expect(result).toEqual({
      ok: true,
      tokens: {
        access_token: "access-WQRT-4821",
        refresh_token: "refresh-WQRT-4821",
        expires_in: 3600,
      },
    });
    expect(lines).toContain("  Open memax.app/device and confirm this code:");
    expect(lines).toContain("      WQRT-4821");
    expect(lines.at(-1)).toMatch(/Waiting for you in the browser/);
    expect(opened).toEqual(["https://memax.app/device?code=WQRT-4821"]);
    // It told the server who is asking, with no credential.
    const ask = fake.calls("POST", /^\/oauth\/device_authorization$/)[0];
    expect(ask.body).toEqual({
      client_id: "memax-cli",
      client_version: "2.0.0",
      device_name: "ziyang-mbp",
      device_os: "linux",
      space: "memax-v2",
    });
    expect(ask.headers.authorization).toBeUndefined();
    // Every poll waited the interval, so the server never said slow_down.
    expect(sleeps.every((ms) => ms === 5_000)).toBe(true);
    expect(st.polls).toBe(3);
  });

  it("waits 5 s longer after slow_down, from then on", async () => {
    st.script.push({ status: 400, body: { error: "slow_down" } });
    const result = await signInWithDeviceCode(
      deps((polls) => polls === 3 && st.approve("WQRT-4821")),
    );
    expect(result.ok).toBe(true);
    expect(sleeps).toEqual([5_000, 10_000, 10_000, 10_000]);
  });

  it("keeps the pace through a failed poll", async () => {
    st.script.push({ status: 502, body: "<html>bad gateway" });
    const result = await signInWithDeviceCode(
      deps((polls) => polls === 1 && st.approve("WQRT-4821")),
    );
    expect(result.ok).toBe(true);
    expect(st.polls).toBe(2);
  });

  it("stops when the code is declined", async () => {
    const result = await signInWithDeviceCode(
      deps((polls) => polls === 1 && st.deny("WQRT-4821")),
    );
    expect(result).toEqual({ ok: false, reason: "denied" });
    expect(deviceLoginFailure("denied")).toMatch(/declined in the browser/);
  });

  it("gives up when the code runs out", async () => {
    const result = await signInWithDeviceCode(deps());
    expect(result).toEqual({ ok: false, reason: "expired" });
    // About 10 minutes of polls every 5 s, then no more.
    expect(st.polls).toBeGreaterThanOrEqual(110);
    expect(st.polls).toBeLessThanOrEqual(120);
    expect(deviceLoginFailure("expired")).toMatch(/ran out/);
  });

  it("says when the server no longer knows the code", async () => {
    st.script.push({ status: 400, body: { error: "invalid_grant" } });
    expect(await signInWithDeviceCode(deps())).toEqual({
      ok: false,
      reason: "invalid",
    });
  });

  it("stops when asked to", async () => {
    const stop = new AbortController();
    const result = await signInWithDeviceCode(
      deps(() => stop.abort(), { signal: stop.signal }),
    );
    expect(result).toEqual({ ok: false, reason: "aborted" });
  });
});

describe("whether a browser can open here", () => {
  it.each([
    ["darwin", {}, true],
    ["win32", {}, true],
    ["linux", { DISPLAY: ":0" }, true],
    ["linux", { WAYLAND_DISPLAY: "wayland-0" }, true],
    ["linux", {}, false],
    ["darwin", { SSH_CONNECTION: "203.0.113.4 52000 10.0.0.2 22" }, false],
    ["linux", { DISPLAY: ":10", SSH_TTY: "/dev/pts/1" }, false],
    ["darwin", { CI: "true" }, false],
  ] as const)("%s %j → %s", (platform, env, want) => {
    expect(canOpenBrowser({ platform, env })).toBe(want);
  });
});
