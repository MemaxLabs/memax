import type { PasskeyCheck } from "memax-sdk";

/**
 * The passkey re-check's bridge between the SDK client (lib/memax-client)
 * and the page. When the API refuses a decision with 403 `needs_passkey`,
 * the SDK calls `answerPasskeyCheck` with the challenge; the app frame has
 * registered a handler that asks the person (a dialog whose button is the
 * user gesture WebAuthn wants, then the browser's prompt) and answers the
 * credential's JSON, or null when they decline. The SDK then sends the
 * very same request again with the answer. With no handler (V1's pages,
 * the sign-in screen), the call fails with `needs_passkey` as before.
 *
 * Checks queue: one dialog at a time, in the order they came.
 */

export type PasskeyAsker = (check: PasskeyCheck) => Promise<unknown | null>;

let asker: PasskeyAsker | null = null;
let queue: Promise<unknown> = Promise.resolve();

/** Registers the page's way of asking; returns how to take it back. */
export function setPasskeyAsker(next: PasskeyAsker): () => void {
  asker = next;
  return () => {
    if (asker === next) asker = null;
  };
}

/** The SDK's `passkeyCheck`: asks the registered handler, one at a time. */
export function answerPasskeyCheck(check: PasskeyCheck): Promise<unknown> {
  const run = queue.then(() => (asker ? asker(check) : null));
  queue = run.catch(() => null);
  return run;
}

/**
 * What the page hears about a change that needed a person and went
 * through without a passkey (`policy.suggest: "passkey"`): the frame
 * suggests adding one.
 */
export const PASSKEY_SUGGESTED_EVENT = "memax:passkey-suggested";

/** Whether an API answer's body suggests a passkey. */
export function suggestsPasskey(body: unknown): boolean {
  const data = (body as { data?: { policy?: { suggest?: unknown } } } | null)
    ?.data;
  return data?.policy?.suggest === "passkey";
}
