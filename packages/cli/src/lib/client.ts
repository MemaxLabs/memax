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

let instance: Memax | null = null;
let publicInstance: Memax | null = null;
const seenWarnings = new Set<string>();
let scopedAgentID = "";

/** Get the shared SDK client instance (lazily created) */
export function getClient(): Memax {
  if (!instance) {
    const config = loadConfig();
    instance = new Memax({
      apiUrl: config.api_url,
      auth: cliAuthProvider,
      onWarning: printApiWarning,
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
    });
  }
  return publicInstance;
}

/** Reset the cached client (useful after login/logout) */
export function resetClient(): void {
  instance = null;
  publicInstance = null;
  seenWarnings.clear();
}

export function setClientAgent(agentID?: string): void {
  scopedAgentID = agentID?.trim() ?? "";
  resetClient();
}

export async function getAuthHeaders(): Promise<Record<string, string>> {
  return cliAuthProvider();
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

  // Auto-refresh if expired
  if (isTokenExpired() && creds.refresh_token) {
    try {
      const tokens = await getPublicClient().auth.refresh(creds.refresh_token);
      if (tokens.access_token) {
        saveCredentials({
          access_token: tokens.access_token,
          refresh_token: tokens.refresh_token,
          expires_at: Date.now() + tokens.expires_in * 1000,
        });
        return { Authorization: `Bearer ${tokens.access_token}` };
      }
    } catch {
      // Refresh failed — fall through to stale token
    }
  }

  return { Authorization: `Bearer ${creds.access_token}` };
}

function printApiWarning(warning: string): void {
  if (!warning || seenWarnings.has(warning)) {
    return;
  }
  seenWarnings.add(warning);
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
