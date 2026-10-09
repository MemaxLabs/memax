// The OAuth server's device grant (RFC 8628) on the fake /v2 server, with
// the real server's rules (internal/deviceauth): a code lives 10 minutes,
// polling faster than the interval is slow_down (and 5 s more from then
// on), a confirmed code yields one session, and a person confirms or
// declines it (here, the test). Time is the test's clock.
import type { FakeV2 } from "../daemon/fake-v2.js";

export interface DeviceState {
  /** The test's clock, in ms. */
  now: number;
  codes: Map<
    string,
    {
      userCode: string;
      status: "pending" | "approved" | "denied" | "consumed";
      interval: number;
      lastPoll: number | null;
      expiresAt: number;
      form: Record<string, string>;
    }
  >;
  /** Answers to give before the real ones, e.g. a 503 between deploys. */
  script: Array<{ status: number; body: unknown }>;
  approve(userCode: string): void;
  deny(userCode: string): void;
  polls: number;
}

export function installDevice(fake: FakeV2, interval = 5): DeviceState {
  let n = 0;
  const st: DeviceState = {
    now: 0,
    codes: new Map(),
    script: [],
    polls: 0,
    approve(userCode) {
      for (const c of st.codes.values())
        if (c.userCode === userCode) c.status = "approved";
    },
    deny(userCode) {
      for (const c of st.codes.values())
        if (c.userCode === userCode) c.status = "denied";
    },
  };
  const err = (error: string) => ({ status: 400, body: { error } });
  fake.oauth = (path, form) => {
    if (path === "/oauth/device_authorization") {
      if (form.get("client_id") !== "memax-cli")
        return { status: 401, body: { error: "invalid_client" } };
      const deviceCode = `device-code-${++n}`.padEnd(43, "x");
      const userCode = `WQRT-482${n}`;
      st.codes.set(deviceCode, {
        userCode,
        status: "pending",
        interval,
        lastPoll: null,
        expiresAt: st.now + 600_000,
        form: Object.fromEntries(form),
      });
      return {
        status: 200,
        body: {
          device_code: deviceCode,
          user_code: userCode,
          verification_uri: "https://memax.app/device",
          verification_uri_complete: `https://memax.app/device?code=${userCode}`,
          expires_in: 600,
          interval,
        },
      };
    }
    if (path !== "/oauth/token") return null;
    st.polls++;
    const scripted = st.script.shift();
    if (scripted) return scripted;
    if (
      form.get("grant_type") !== "urn:ietf:params:oauth:grant-type:device_code"
    )
      return err("unsupported_grant_type");
    const c = st.codes.get(form.get("device_code") ?? "");
    if (!c || c.status === "consumed") return err("invalid_grant");
    if (c.status === "denied") return err("access_denied");
    if (st.now >= c.expiresAt) return err("expired_token");
    if (c.status === "approved") {
      c.status = "consumed";
      return {
        status: 200,
        body: {
          access_token: `access-${c.userCode}`,
          refresh_token: `refresh-${c.userCode}`,
          token_type: "Bearer",
          expires_in: 3600,
        },
      };
    }
    const tooSoon =
      c.lastPoll !== null && st.now - c.lastPoll < c.interval * 1000;
    c.lastPoll = st.now;
    if (tooSoon) {
      c.interval += 5;
      return err("slow_down");
    }
    return err("authorization_pending");
  };
  return st;
}
