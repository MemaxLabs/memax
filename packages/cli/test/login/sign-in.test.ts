// How memax login and memax init sign in: with a code confirmed on the web
// app, so the CLI is whoever the browser is signed in as (GitHub, Google,
// an email code or a passkey alike). A provider's own page only knows its
// provider: a person who signs in with Google got the account of their
// GitHub login instead (staging, Oct 8).
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { MemaxError } from "memax-sdk";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { noDeviceGrant } from "../../src/lib/device-login.js";
import { FakeV2 } from "../daemon/fake-v2.js";
import { installDevice, type DeviceState } from "./fake-device.js";

// What the CLI opens in a browser, instead of opening it.
const opened = vi.hoisted(() => [] as string[]);
vi.mock("node:child_process", async (actual) => ({
  ...(await actual<typeof import("node:child_process")>()),
  execFile: (_cmd: string, args: string[], cb?: () => void) => {
    opened.push(args.at(-1) ?? "");
    cb?.();
  },
}));

let fake: FakeV2;
let home: string;
let out: string[];

beforeEach(async () => {
  fake = await new FakeV2().start();
  home = mkdtempSync(join(tmpdir(), "memax-sign-in-"));
  out = [];
  vi.stubEnv("HOME", home);
  vi.stubEnv("USERPROFILE", home);
  vi.stubEnv("MEMAX_API_URL", fake.url);
  vi.stubEnv("MEMAX_API_KEY", "");
  // A desktop with a browser, unless a test says otherwise.
  vi.stubEnv("CI", "");
  vi.stubEnv("SSH_CONNECTION", "");
  vi.stubEnv("SSH_CLIENT", "");
  vi.stubEnv("SSH_TTY", "");
  vi.stubEnv("DISPLAY", ":0");
  opened.length = 0;
  vi.spyOn(console, "log").mockImplementation((l) => void out.push(String(l)));
  vi.spyOn(console, "error").mockImplementation(
    (l) => void out.push(String(l)),
  );
  // The CLI's config folder and client are fixed when their modules load.
  vi.resetModules();
});
afterEach(async () => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  await fake.stop();
  rmSync(home, { recursive: true, force: true });
});

const login = () => import("../../src/commands/login.js");

/** Confirms the first code on the web as soon as the CLI shows it. */
function confirmOnTheWeb(st: DeviceState): void {
  const t = setInterval(() => {
    const code = [...st.codes.values()][0];
    if (code) {
      st.approve(code.userCode);
      clearInterval(t);
    }
  }, 10);
}

describe("signing in", () => {
  it("opens the web app's page for a code, not a provider's, where a browser can", async () => {
    const st = installDevice(fake, 1);
    confirmOnTheWeb(st);
    const { signIn } = await login();
    expect(await signIn({})).toBe(true);
    expect(opened).toEqual(["https://memax.app/device?code=WQRT-4821"]);
    expect(out).toContain("  Open memax.app/device and confirm this code:");
    expect(fake.calls("POST", /^\/oauth\/device_authorization$/)).toHaveLength(
      1,
    );
    const saved = JSON.parse(
      readFileSync(join(home, ".memax", "credentials.json"), "utf8"),
    );
    expect(saved).toMatchObject({
      access_token: "access-WQRT-4821",
      refresh_token: "refresh-WQRT-4821",
    });
  }, 20_000);

  it("says what to do on a server with no device grant and no browser here", async () => {
    // An API from before the device grant answers 404; where a browser can
    // open, the CLI goes to the provider's page instead.
    vi.stubEnv("CI", "1");
    fake.oauth = () => null;
    const { signIn } = await login();
    expect(await signIn({})).toBe(false);
    expect(out.join("\n")).toMatch(
      /can't sign in with a code\. Run memax login on a machine with a browser, or set MEMAX_API_KEY/,
    );
  });

  it("tells a server without the grant from a refused sign-in", () => {
    expect(noDeviceGrant(new MemaxError("gone", "not_found", 404))).toBe(true);
    expect(noDeviceGrant(new MemaxError("no", "invalid_client", 401))).toBe(
      false,
    );
    expect(noDeviceGrant(new MemaxError("slow", "rate_limited", 429))).toBe(
      false,
    );
    expect(noDeviceGrant(new Error("offline"))).toBe(false);
  });
});
