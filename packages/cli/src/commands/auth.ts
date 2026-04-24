import { Command } from "commander";
import chalk from "chalk";
import { getClient } from "../lib/client.js";
import { resolveHubID } from "../lib/hubs.js";

export async function createKeyCommand(
  name: string,
  opts: {
    expires?: string;
    hub?: string[];
    agent?: string;
    grant?: string[];
    readOnly?: boolean;
    trustLevel?: "public" | "standard" | "elevated" | "admin";
  },
): Promise<void> {
  const expiresInDays = opts.expires ? parseInt(opts.expires, 10) : 0;

  try {
    const hubRefs = opts.hub ?? [];
    const hubIds: string[] = [];
    for (const hubRef of hubRefs) {
      const hubId = await resolveHubID(hubRef);
      if (!hubId) {
        throw new Error(
          `Hub not found or not accessible: ${hubRef}. Run \`memax hub list\` to see available hubs.`,
        );
      }
      hubIds.push(hubId);
    }
    if (opts.readOnly && opts.grant && opts.grant.length > 0) {
      throw new Error("Use either --read-only or --grant, not both.");
    }
    const scopes = opts.readOnly ? ["read"] : opts.grant;
    if (hubIds.length > 0 && scopes?.some(isAccountLevelGrant)) {
      throw new Error(
        "Hub-scoped keys cannot include account-level grants like agent-sync, config, settings, or account deletion.",
      );
    }
    const result = await getClient().auth.createKey({
      name,
      hubId: hubIds[0],
      hubIds: hubIds.length > 1 ? hubIds : undefined,
      agentName: opts.agent || undefined,
      expiresInDays: expiresInDays || undefined,
      scopes,
      trustLevel: opts.trustLevel,
    });

    console.log("\n  API key created successfully.\n");
    console.log(`  Name:       ${result.name}`);
    console.log(`  Key:        ${result.key}`);
    const isHubScoped = result.hub_scope_mode === "hub_allowlist";
    console.log(
      `  Scope:      ${isHubScoped ? chalk.cyan(result.scope) : chalk.dim("all accessible hubs")}`,
    );
    if (result.default_permissions && result.default_permissions.length > 0) {
      console.log(
        `  Grants:     ${chalk.gray(result.default_permissions.join(", "))}`,
      );
    }
    if (result.trust_level) {
      console.log(`  Trust:      ${chalk.gray(result.trust_level)}`);
    }
    if (opts.agent) {
      console.log(`  Agent:      ${chalk.magenta(opts.agent)}`);
    }
    if (result.expires_at) {
      console.log(
        `  Expires:    ${new Date(result.expires_at).toLocaleDateString()}`,
      );
    } else {
      console.log(`  Expires:    never`);
    }
    console.log(
      `\n  ${chalk.yellow("Save this key now — it cannot be retrieved again.")}\n`,
    );
    console.log("  Usage:");
    console.log(`    export MEMAX_API_KEY=${result.key}\n`);
  } catch (err) {
    console.error(`  Failed to create API key: ${(err as Error).message}\n`);
    process.exit(1);
  }
}

export async function listKeysCommand(): Promise<void> {
  try {
    const keys = await getClient().auth.listKeys();

    if (keys.length === 0) {
      console.log(
        "\n  No API keys. Create one with: memax auth create-key <name>\n",
      );
      return;
    }

    console.log("\n  API Keys:\n");
    for (const key of keys) {
      const expires = key.expires_at
        ? new Date(key.expires_at).toLocaleDateString()
        : "never";
      const lastUsed = key.last_used
        ? new Date(key.last_used).toLocaleDateString()
        : "never";
      const scope = key.hub_id
        ? chalk.cyan(`hub:${key.scope}`)
        : chalk.dim("all accessible hubs");
      const agent = key.agent_name
        ? chalk.magenta(` [${key.agent_name}]`)
        : key.standalone
          ? chalk.gray(" [standalone]")
          : chalk.yellow(" [unassigned]");
      console.log(`  ${key.prefix}...  ${key.name}  ${scope}${agent}`);
      console.log(
        `    ID: ${key.id}  Expires: ${expires}  Last used: ${lastUsed}`,
      );
      if (key.default_permissions && key.default_permissions.length > 0) {
        console.log(`    Grants: ${key.default_permissions.join(", ")}`);
      }
    }
    console.log();
  } catch (err) {
    console.error(`  Failed to list API keys: ${(err as Error).message}\n`);
    process.exit(1);
  }
}

export async function revokeKeyCommand(id: string): Promise<void> {
  // `memax auth revoke-key` is idempotent: re-running against an
  // already-revoked key exits 0 with an explicit "already revoked"
  // line so scripts and CI cleanup flows don't break on the race.
  // Only a genuine store failure (revoke_failed) or an unexpected
  // network error exits non-zero.
  try {
    const result = await getClient().auth.revokeKey(id);
    if (result.revoked) {
      console.log("\n  API key revoked.\n");
      return;
    }
    const skip = result.skipped[0];
    const reason = skip?.reason;
    if (reason === "not_found") {
      console.log("\n  API key not found (already revoked).\n");
      return;
    }
    console.error(`  Failed to revoke API key: ${reason ?? "unknown"}\n`);
    process.exit(1);
  } catch (err) {
    console.error(`  Failed to revoke API key: ${(err as Error).message}\n`);
    process.exit(1);
  }
}

export function registerAuthCommand(program: Command): void {
  const authCmd = program
    .command("auth")
    .description("Manage authentication and API keys");

  authCmd
    .command("create-key <name>")
    .description("Create an API key for CI/CD or non-interactive use")
    .option("--expires <days>", "Expire after N days (default: never)")
    .option(
      "--hub <id>",
      "Scope to a specific hub. May be repeated.",
      collect,
      [],
    )
    .option(
      "--agent <slug>",
      "Associate with an agent (e.g. claude-code, cursor)",
    )
    .option(
      "--grant <name>",
      "Permission bundle or permission. May be repeated.",
      collect,
      [],
    )
    .option("--read-only", "Shortcut for --grant read")
    .option(
      "--trust-level <level>",
      "Grant trust level: public, standard, elevated, admin",
    )
    .action(createKeyCommand);

  authCmd
    .command("list-keys")
    .description("List your API keys")
    .action(listKeysCommand);

  authCmd
    .command("revoke-key <id>")
    .description("Revoke an API key")
    .action(revokeKeyCommand);
}

function collect(value: string, previous: string[]): string[] {
  previous.push(value);
  return previous;
}

function isAccountLevelGrant(grant: string): boolean {
  return (
    grant === "agent-sync" ||
    grant.startsWith("config:") ||
    grant.startsWith("agent_session:") ||
    grant.startsWith("settings:") ||
    grant.startsWith("account:")
  );
}
