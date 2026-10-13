import { readFileSync, writeFileSync, mkdirSync, existsSync } from "node:fs";
import { join } from "node:path";
import { getConfigDir } from "./config.js";

const CRED_FILE = join(getConfigDir(), "credentials.json");

interface Credentials {
  access_token: string;
  refresh_token: string;
  expires_at?: number; // unix timestamp (ms)
  local_agent_keys?: Record<string, string>;
}

export function loadCredentials(): Credentials | null {
  if (!existsSync(CRED_FILE)) return null;
  try {
    const creds = JSON.parse(readFileSync(CRED_FILE, "utf-8")) as Credentials;
    if (!creds.access_token && !creds.local_agent_keys) return null;
    return creds;
  } catch {
    return null;
  }
}

export function saveCredentials(creds: Credentials): void {
  mkdirSync(getConfigDir(), { recursive: true });
  const existing = loadCredentials();
  const next: Credentials = {
    ...existing,
    ...creds,
    local_agent_keys: creds.local_agent_keys ?? existing?.local_agent_keys,
  };
  writeFileSync(CRED_FILE, JSON.stringify(next, null, 2) + "\n", {
    mode: 0o600,
  });
}

/**
 * The person an access token names (its JWT `sub`), read without checking
 * the signature: only to tell whether two sessions are the same person's.
 * Null for anything that isn't such a token (an API key).
 */
export function tokenSubject(token: string | undefined): string | null {
  const body = token?.split(".")[1];
  if (!body) return null;
  try {
    const claims = JSON.parse(
      Buffer.from(body, "base64url").toString("utf8"),
    ) as { sub?: unknown };
    return typeof claims.sub === "string" && claims.sub !== ""
      ? claims.sub
      : null;
  } catch {
    return null;
  }
}

/** The person this machine is signed in as, from the saved session. */
export function currentPerson(): string | null {
  return tokenSubject(loadCredentials()?.access_token);
}

export function isTokenExpired(): boolean {
  const creds = loadCredentials();
  if (!creds?.expires_at) return false; // no expiry info → assume valid
  // Treat as expired 5 minutes early to avoid edge cases
  return Date.now() >= creds.expires_at - 5 * 60 * 1000;
}

export function clearCredentials(): void {
  if (existsSync(CRED_FILE)) {
    writeFileSync(CRED_FILE, "{}\n", { mode: 0o600 });
  }
}

export function getLocalAgentKey(agentID: string): string | undefined {
  const creds = loadCredentials();
  return creds?.local_agent_keys?.[agentID];
}

export function saveLocalAgentKey(agentID: string, apiKey: string): void {
  const creds = loadCredentials() ?? {
    access_token: "",
    refresh_token: "",
  };
  const localAgentKeys = { ...(creds.local_agent_keys ?? {}) };
  localAgentKeys[agentID] = apiKey;
  saveCredentials({
    ...creds,
    local_agent_keys: localAgentKeys,
  });
}
