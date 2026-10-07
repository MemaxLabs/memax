// =============================================================================
// CLI API client — configures memax-sdk with CLI-specific auth
//
// All CLI commands should use `getClient()` instead of raw fetch.
// Auth flow: MEMAX_API_KEY env var → stored credentials (with auto-refresh).
// =============================================================================

import { Memax } from "memax-sdk";
import chalk from "chalk";
import { loadConfig } from "./config.js";
import {
  loadCredentials,
  saveCredentials,
  isTokenExpired,
  getLocalAgentKey,
} from "./credentials.js";
import { cliVersion } from "./version.js";

/**
 * Names the CLI to the API, so your sessions list says "memax CLI 0.9.0"
 * rather than an anonymous client.
 */
export function cliUserAgent(): string {
  return `memax-cli/${cliVersion()} (${process.platform})`;
}

let instance: Memax | null = null;
let publicInstance: Memax | null = null;
const seenWarnings = new Set<string>();
let scopedAgentID = "";
let fetchImpl: typeof globalThis.fetch | undefined;

/** Get the shared SDK client instance (lazily created) */
export function getClient(): Memax {
  if (!instance) {
    const config = loadConfig();
    instance = new Memax({
      apiUrl: config.api_url,
      auth: cliAuthProvider,
      onWarning: printApiWarning,
      fetch: fetchImpl,
      headers: { "User-Agent": cliUserAgent() },
    });
  }
  return instance;
}

/** Get an unauthenticated SDK client for auth bootstrap/refresh flows */
export function getPublicClient(): Memax {
  if (!publicInstance) {
    const config = loadConfig();
    publicInstance = new Memax({
      apiUrl: config.api_url,
      fetch: fetchImpl,
      headers: { "User-Agent": cliUserAgent() },
    });
  }
  return publicInstance;
}

/**
 * Marks a warning as said already: a command that explains it in its own
 * words (memax agents sync, for space_on_v2) calls this first.
 */
export function quietWarning(warning: string): void {
  seenWarnings.add(warning);
}

/** Reset the cached client (useful after login/logout) */
export function resetClient(): void {
  instance = null;
  publicInstance = null;
  seenWarnings.clear();
}

/**
 * Sends every request through `f` from here on, token refreshes included
 * (the daemon's lighter transport, lib/daemon/http.ts).
 */
export function setClientFetch(f: typeof globalThis.fetch): void {
  fetchImpl = f;
  resetClient();
}

export function setClientAgent(agentID?: string): void {
  scopedAgentID = agentID?.trim() ?? "";
  resetClient();
}

export async function getAuthHeaders(): Promise<Record<string, string>> {
  return cliAuthProvider();
}

/**
 * Whether requests authenticate with an API key (MEMAX_API_KEY, or the
 * agent-scoped local key `memax setup` created). On the V2 record every
 * API key is an agent, which reads only the spaces it's connected to.
 */
export function usesAPIKey(): boolean {
  if (process.env.MEMAX_API_KEY) return true;
  return scopedAgentID !== "" && getLocalAgentKey(scopedAgentID) !== undefined;
}

/**
 * CLI auth provider — resolves authorization headers.
 *
 * Priority:
 * 1. MEMAX_API_KEY env var (CI/CD, non-interactive)
 * 2. Stored credentials with automatic token refresh
 */
async function cliAuthProvider(): Promise<Record<string, string>> {
  // 1. Env var takes priority
  const envKey = process.env.MEMAX_API_KEY;
  if (envKey) {
    return { Authorization: `Bearer ${envKey}` };
  }

  // 2. Agent-scoped local key for local MCP/hooks/capture flows
  if (scopedAgentID) {
    const agentKey = getLocalAgentKey(scopedAgentID);
    if (agentKey) {
      return { Authorization: `Bearer ${agentKey}` };
    }
  }

  // 3. Stored user credentials
  const creds = loadCredentials();
  if (!creds?.access_token) return {};

  // Auto-refresh if expired. The refresh token rotates: the answer carries
  // the session's next one and the one sent is retired, so it must be
  // stored. Processes sharing this file (the daemon, MCP servers, hooks)
  // that refresh together all get the same next token from the server.
  if (isTokenExpired() && creds.refresh_token) {
    try {
      const tokens = await getPublicClient().auth.refresh(creds.refresh_token);
      if (tokens.access_token) {
        saveCredentials({
          access_token: tokens.access_token,
          refresh_token: tokens.refresh_token || creds.refresh_token,
          expires_at: Date.now() + tokens.expires_in * 1000,
        });
        return { Authorization: `Bearer ${tokens.access_token}` };
      }
    } catch {
      // Refresh failed (offline, or the session was signed out) — fall
      // through to the stale token; the API's 401 says to log in again.
    }
  }

  return { Authorization: `Bearer ${creds.access_token}` };
}

function printApiWarning(warning: string): void {
  if (!warning || seenWarnings.has(warning)) {
    return;
  }
  seenWarnings.add(warning);
  if (warning === "space_on_v2") {
    console.error(
      chalk.yellow(
        "  This space is on V2: what you push here is kept as a note, and Dream proposes from it for Review.",
      ),
    );
    return;
  }
  if (warning === "agent_identity_claim_rejected") {
    console.error(
      chalk.yellow(
        "  Warning: agent attribution was rejected for this write; the memory was saved as you instead.",
      ),
    );
    return;
  }
  console.error(chalk.yellow(`  Warning: ${warning}`));
}
