/**
 * One idempotency key per user action, reused across its retries (spec:
 * Idempotency-Key). An intent names the action exactly: the command,
 * the memory, the version it started from and what it sends. Pressing
 * Keep again after a dropped connection is the same intent, so the
 * server applies it at most once; editing the words or deciding on a
 * newer version is a new intent with a new key.
 *
 * Settle an intent when it has an answer (success, or a refusal that a
 * retry would only repeat); keep it while a retry is still the same
 * command (the request may or may not have landed).
 */
export class IntentKeys {
  private readonly keys = new Map<string, string>();

  constructor(
    private readonly newKey: () => string = () => crypto.randomUUID(),
  ) {}

  /** The key for this intent: the same one until it settles. */
  keyFor(intent: string): string {
    let key = this.keys.get(intent);
    if (!key) {
      key = this.newKey();
      this.keys.set(intent, key);
    }
    return key;
  }

  settle(intent: string): void {
    this.keys.delete(intent);
  }
}

/** A stable intent id from its parts; the payload is folded into a short hash. */
export function intentOf(
  command: string,
  ref: string,
  version: number,
  ...payload: (string | undefined)[]
): string {
  let hash = 5381;
  for (const ch of payload.map((p) => p ?? "").join("\u0000")) {
    hash = ((hash << 5) + hash + ch.charCodeAt(0)) | 0;
  }
  return `${command}:${ref}:${version}:${(hash >>> 0).toString(36)}`;
}
