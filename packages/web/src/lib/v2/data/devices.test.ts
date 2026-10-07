import { MemaxError } from "memax-sdk";
import { describe, expect, it, vi } from "vitest";
import { createDemoDevices, DEMO_DEVICE_CODE } from "./devices-demo";
import { DeviceCommandError, normalizeUserCode } from "./devices";
import {
  createSdkDevices,
  toDeviceError,
  toDeviceRequest,
} from "./devices-sdk";

describe("a device's code", () => {
  it.each([
    ["WQRT-4821", "WQRT-4821"],
    ["wqrt 4821", "WQRT-4821"],
    ["WQRT4821", "WQRT-4821"],
    ["wqrt-48Z1", null],
    ["WQRT-O82I", "WQRT-0821"],
    ["AQRT-4821", null],
    ["WQRT-482", null],
    ["", null],
  ])("reads %j as %j", (raw, want) => {
    expect(normalizeUserCode(raw)).toBe(want);
  });
});

describe("the devices mapper", () => {
  it("maps the spec's DeviceAuthorization", () => {
    expect(
      toDeviceRequest({
        user_code: "WQRT-4821",
        state: "pending",
        client_id: "memax-cli",
        client_version: "2.0.0",
        device_name: "ziyang-mbp",
        device_os: "macOS",
        space: "memax-v2",
        address: "203.0.113.4",
        requested_at: "2026-10-05T21:39:48Z",
        expires_at: "2026-10-05T21:49:48Z",
      }),
    ).toEqual({
      userCode: "WQRT-4821",
      state: "pending",
      clientId: "memax-cli",
      clientVersion: "2.0.0",
      deviceName: "ziyang-mbp",
      deviceOs: "macOS",
      space: "memax-v2",
      address: "203.0.113.4",
      requestedAt: "2026-10-05T21:39:48Z",
      expiresAt: "2026-10-05T21:49:48Z",
      decidedAt: null,
      signedInAt: null,
    });
  });

  it.each([
    [
      new MemaxError("x", "refused", 403, {
        policy: { effect: "refuse", code: "device_needs_web" },
      }),
      "needs_web",
    ],
    [
      new MemaxError("x", "refused", 403, {
        policy: { effect: "refuse", code: "device_by_person" },
      }),
      "by_person",
    ],
    [new MemaxError("x", "surface_unverified", 403), "needs_web"],
    [new MemaxError("x", "not_found", 404), "not_found"],
    [new MemaxError("x", "rate_limited", 429, undefined, 120), "rate_limited"],
    [
      new MemaxError("x", "invalid_transition", 409, { state: "expired" }),
      "decided",
    ],
    [new MemaxError("x", "unavailable", 503), "unavailable"],
    [new TypeError("fetch failed"), "failed"],
  ])("normalises %s", (err, refusal) => {
    expect(toDeviceError(err).refusal).toBe(refusal);
  });

  it("keeps how a decided code ended and how long to wait", () => {
    expect(
      toDeviceError(
        new MemaxError("x", "invalid_transition", 409, { state: "denied" }),
      ).detail,
    ).toEqual({ state: "denied" });
    expect(
      toDeviceError(new MemaxError("x", "rate_limited", 429, undefined, 300))
        .detail,
    ).toEqual({
      retryAfter: 300,
    });
  });

  it("calls memax.v2.devices with the person's key", async () => {
    const approve = vi.fn(async () => ({
      user_code: "WQRT-4821",
      state: "approved",
      client_id: "memax-cli",
      requested_at: "2026-10-05T21:39:48Z",
      expires_at: "2026-10-05T21:49:48Z",
    }));
    const lookup = vi.fn(async () => {
      throw new MemaxError("x", "not_found", 404);
    });
    const devices = createSdkDevices({
      v2: { devices: { approve, lookup } },
    } as never);
    expect(
      (await devices.approve({ userCode: "WQRT-4821", idempotencyKey: "k" }))
        .state,
    ).toBe("approved");
    expect(approve).toHaveBeenCalledWith("WQRT-4821", { idempotencyKey: "k" });
    await expect(
      devices.lookup({ userCode: "BCDF-0000" }),
    ).rejects.toBeInstanceOf(DeviceCommandError);
  });
});

describe("the demo's device", () => {
  it("signs the CLI in a moment after the person confirms", async () => {
    const devices = createDemoDevices({
      now: () => new Date("2026-10-05T14:40:00-07:00"),
      signInAfterMs: 0,
    });
    expect((await devices.lookup({ userCode: DEMO_DEVICE_CODE })).state).toBe(
      "pending",
    );
    expect(
      (await devices.approve({ userCode: "wqrt4821", idempotencyKey: "k" }))
        .state,
    ).toBe("approved");
    expect((await devices.lookup({ userCode: DEMO_DEVICE_CODE })).state).toBe(
      "signed_in",
    );
    await expect(
      devices.deny({ userCode: DEMO_DEVICE_CODE, idempotencyKey: "k2" }),
    ).rejects.toMatchObject({
      refusal: "decided",
    });
    await expect(
      devices.lookup({ userCode: "BCDF-0000" }),
    ).rejects.toMatchObject({
      refusal: "not_found",
    });
  });
});
