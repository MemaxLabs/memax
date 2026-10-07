/**
 * Device sign-in (plan §5.15, RFC 8628): a person confirming, on the web,
 * the code the memax CLI shows on a machine where no browser can open
 * (CliAuth, /device). Part of LedgerDataSource (source.ts) as
 * `source.devices`; devices-sdk.ts (memax.v2.devices) and devices-demo.ts
 * implement it. No words live here.
 */

/** pending, approved (the CLI hasn't collected it yet), signed_in, denied or expired. */
export type DeviceState =
  | "pending"
  | "approved"
  | "signed_in"
  | "denied"
  | "expired";

/** A device asking to sign in. All but `address` is what it says about itself. */
export interface DeviceRequestView {
  /** As people read it: "WQRT-4821". */
  userCode: string;
  state: DeviceState;
  clientId: string;
  /** "2.0.0". */
  clientVersion: string | null;
  /** "ziyang-mbp". */
  deviceName: string | null;
  /** macOS, Linux, Windows, FreeBSD or other. */
  deviceOs: string | null;
  /** The space slug it will use. */
  space: string | null;
  /** Where the request came from, as Memax saw it. */
  address: string | null;
  requestedAt: string;
  expiresAt: string;
  decidedAt: string | null;
  signedInAt: string | null;
}

/** Why a confirmation didn't go through, normalised from the API. */
export type DeviceRefusal =
  /** No waiting code by that name (or someone else's). */
  | "not_found"
  /** Confirming needs the session issued to the web app, and the proxy's signature. */
  | "needs_web"
  /** Agents and keys never sign a device in. */
  | "by_person"
  /** Too many codes that don't exist; wait `retryAfter` seconds. */
  | "rate_limited"
  /** Already decided the other way, or expired (`state`). */
  | "decided"
  | "unavailable"
  | "failed";

export class DeviceCommandError extends Error {
  constructor(
    readonly refusal: DeviceRefusal,
    readonly detail: { state?: DeviceState; retryAfter?: number } = {},
  ) {
    super(refusal);
    this.name = "DeviceCommandError";
  }
}

export interface DevicesSource {
  /** The demo answers at once, so screenshots never catch a loading frame. */
  peek?(userCode: string): DeviceRequestView | null | undefined;
  /** What a waiting code says about the device; throws DeviceCommandError. */
  lookup(input: {
    userCode: string;
    signal?: AbortSignal;
  }): Promise<DeviceRequestView>;
  /** Signs the device in as the viewer; throws DeviceCommandError. */
  approve(input: {
    userCode: string;
    idempotencyKey: string;
  }): Promise<DeviceRequestView>;
  /** Declines the code; nothing is signed in. */
  deny(input: {
    userCode: string;
    idempotencyKey: string;
  }): Promise<DeviceRequestView>;
}

/** A code as the person might type it, normalised for display: "wqrt 4821" → "WQRT-4821". */
export function normalizeUserCode(raw: string): string | null {
  const chars = raw
    .toUpperCase()
    .replace(/[\s\-–—]/g, "")
    .split("");
  if (chars.length !== 8) return null;
  const digits = chars
    .slice(4)
    .map((c) => (c === "O" ? "0" : c === "I" || c === "L" ? "1" : c));
  const letters = chars.slice(0, 4);
  if (!letters.every((c) => /[BCDFGHJKLMNPQRSTVWXZ]/.test(c))) return null;
  if (!digits.every((c) => /[0-9]/.test(c))) return null;
  return `${letters.join("")}-${digits.join("")}`;
}
