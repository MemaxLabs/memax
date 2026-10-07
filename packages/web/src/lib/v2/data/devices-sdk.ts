import { MemaxError, refusalOf, type Memax, type V2 } from "memax-sdk";
import {
  DeviceCommandError,
  type DeviceRefusal,
  type DeviceRequestView,
  type DevicesSource,
  type DeviceState,
} from "./devices";

/** Device sign-in over memax.v2.devices (devices.ts). */

type DevicesClient = Pick<Memax, "v2">;

export function toDeviceRequest(d: V2.DeviceAuthorization): DeviceRequestView {
  return {
    userCode: d.user_code,
    state: d.state,
    clientId: d.client_id,
    clientVersion: d.client_version ?? null,
    deviceName: d.device_name ?? null,
    deviceOs: d.device_os ?? null,
    space: d.space ?? null,
    address: d.address ?? null,
    requestedAt: d.requested_at,
    expiresAt: d.expires_at,
    decidedAt: d.decided_at ?? null,
    signedInAt: d.signed_in_at ?? null,
  };
}

export function toDeviceError(err: unknown): DeviceCommandError {
  if (err instanceof DeviceCommandError) return err;
  if (!(err instanceof MemaxError)) return new DeviceCommandError("failed");
  let refusal: DeviceRefusal = "failed";
  const detail: { state?: DeviceState; retryAfter?: number } = {};
  if (err.code === "refused") {
    const code = refusalOf(err)?.code;
    refusal = code === "device_by_person" ? "by_person" : "needs_web";
  } else if (err.code === "surface_unverified") {
    refusal = "needs_web";
  } else if (err.code === "not_found" || err.status === 404) {
    refusal = "not_found";
  } else if (err.isRateLimited) {
    refusal = "rate_limited";
    if (err.retryAfterSeconds) detail.retryAfter = err.retryAfterSeconds;
  } else if (err.code === "invalid_transition") {
    refusal = "decided";
    const state = err.details?.state;
    if (typeof state === "string") detail.state = state as DeviceState;
  } else if (err.code === "unavailable") {
    refusal = "unavailable";
  }
  return new DeviceCommandError(refusal, detail);
}

export function createSdkDevices(client: DevicesClient): DevicesSource {
  const wrap = async (
    run: () => Promise<V2.DeviceAuthorization>,
  ): Promise<DeviceRequestView> => {
    try {
      return toDeviceRequest(await run());
    } catch (err) {
      throw toDeviceError(err);
    }
  };
  return {
    lookup: ({ userCode, signal }) =>
      wrap(() => client.v2.devices.lookup(userCode, { signal })),
    approve: ({ userCode, idempotencyKey }) =>
      wrap(() => client.v2.devices.approve(userCode, { idempotencyKey })),
    deny: ({ userCode, idempotencyKey }) =>
      wrap(() => client.v2.devices.deny(userCode, { idempotencyKey })),
  };
}
