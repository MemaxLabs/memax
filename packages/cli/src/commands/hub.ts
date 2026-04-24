import { Command } from "commander";
import chalk from "chalk";
import type { Hub, HubInvite, HubMember } from "memax-sdk";
import { getClient } from "../lib/client.js";
import { loadConfig, getActiveHubID, setActiveHubID } from "../lib/config.js";
import {
  getHubReference,
  PERSONAL_HUB_ALIAS,
  requireHub,
} from "../lib/hubs.js";

interface HubInviteCreateOptions {
  hub?: string;
  role?: string;
  format?: string;
}

interface HubInviteListOptions {
  hub?: string;
  format?: string;
  verbose?: boolean;
}

interface HubInviteMutationOptions {
  hub?: string;
  format?: string;
}

interface HubMembersOptions {
  format?: string;
}

export function inviteDisplayID(id: string, verbose = false): string {
  return verbose ? id : id.slice(0, 8);
}

export function deriveInviteURL(token: string, apiURL: string): string {
  const parsed = new URL(apiURL);
  if (parsed.hostname === "localhost" || parsed.hostname === "127.0.0.1") {
    return `http://localhost:3000/invite/${token}`;
  }
  if (parsed.hostname.startsWith("staging-api.")) {
    parsed.hostname = parsed.hostname.replace(/^staging-api\./, "staging-app.");
    parsed.pathname = `/invite/${token}`;
    parsed.search = "";
    parsed.hash = "";
    return parsed.toString();
  }
  if (parsed.hostname.startsWith("api.")) {
    parsed.hostname = parsed.hostname.replace(/^api\./, "");
    parsed.pathname = `/invite/${token}`;
    parsed.search = "";
    parsed.hash = "";
    return parsed.toString();
  }
  if (parsed.hostname.startsWith("api-")) {
    parsed.hostname = parsed.hostname.replace(/^api-/, "app-");
    parsed.pathname = `/invite/${token}`;
    parsed.search = "";
    parsed.hash = "";
    return parsed.toString();
  }
  parsed.pathname = `/invite/${token}`;
  parsed.search = "";
  parsed.hash = "";
  return parsed.toString();
}

export function extractInviteToken(value: string): string {
  const trimmed = value.trim();
  try {
    const parsed = new URL(trimmed);
    const parts = parsed.pathname.split("/").filter(Boolean);
    const inviteIndex = parts.indexOf("invite");
    if (inviteIndex >= 0 && parts[inviteIndex + 1]) {
      return parts[inviteIndex + 1];
    }
  } catch {
    // raw token/reference, fall through
  }
  return trimmed;
}

export function resolveInviteReference(
  invites: HubInvite[],
  ref: string,
): HubInvite {
  const normalized = ref.trim().toLowerCase();
  const exact = invites.find(
    (invite) => invite.id.toLowerCase() === normalized,
  );
  if (exact) return exact;

  const matches = invites.filter((invite) =>
    invite.id.toLowerCase().startsWith(normalized),
  );
  if (matches.length === 1) return matches[0];
  if (matches.length > 1) {
    throw new Error(
      `Invite ID prefix is ambiguous. Matches: ${matches
        .map((invite) => inviteDisplayID(invite.id, true))
        .join(", ")}`,
    );
  }
  throw new Error(
    "Invite not found. Run `memax hub invite list` to see outstanding invites.",
  );
}

async function requireInviteHub(ref: string | undefined): Promise<Hub> {
  const hubRef = ref ?? getActiveHubID();
  if (!hubRef) {
    throw new Error(
      "No active hub selected. Run `memax hub switch <slug>` or pass `--hub`.",
    );
  }
  const match = await requireHub(hubRef);
  return match.hub;
}

function normalizeInviteRole(
  role: string | undefined,
): "admin" | "contributor" | "viewer" {
  if (!role) return "contributor";
  const normalized = role.trim().toLowerCase();
  if (normalized === "member") return "contributor";
  if (normalized === "reader") return "viewer";
  if (!["admin", "contributor", "viewer"].includes(normalized)) {
    throw new Error("Role must be one of: admin, contributor, viewer");
  }
  return normalized as "admin" | "contributor" | "viewer";
}

function validateFormat(format: string | undefined): "text" | "json" {
  const resolved = (format ?? "text").toLowerCase();
  if (resolved !== "text" && resolved !== "json") {
    throw new Error(`Unsupported --format value: ${format}`);
  }
  return resolved;
}

function printInvite(invite: HubInvite, verbose = false): void {
  console.log(
    `${chalk.bold(inviteDisplayID(invite.id, verbose))} ${chalk.gray(`[${invite.role}]`)}`,
  );
  console.log(
    chalk.gray(
      `  created: ${new Date(invite.created_at).toISOString()} · expires: ${new Date(invite.expires_at).toISOString()}`,
    ),
  );
}

function memberDisplayName(member: HubMember): string {
  return member.user_name || member.user_email || member.user_id;
}

function printMember(member: HubMember): void {
  const email = member.user_email ? chalk.gray(` <${member.user_email}>`) : "";
  console.log(
    `  ${chalk.bold(memberDisplayName(member))}${email} ${chalk.dim(`[${member.role}]`)}`,
  );
  console.log(
    chalk.gray(`    joined: ${new Date(member.joined_at).toISOString()}`),
  );
}

export async function hubListCommand(options: {
  format?: string;
}): Promise<void> {
  try {
    const client = getClient();
    const hubs = await client.hubs.list();

    if (!hubs || hubs.length === 0) {
      if (options.format === "json") {
        console.log("[]");
      } else {
        console.log(chalk.dim("  No hubs found. Run: memax hub create <name>"));
      }
      return;
    }

    if (options.format === "json") {
      console.log(JSON.stringify(hubs, null, 2));
      return;
    }

    const activeHubID = getActiveHubID();

    console.log();
    for (const { hub, role } of hubs) {
      const isActive = hub.id === activeHubID;
      const marker = isActive ? chalk.green("● ") : "  ";
      const typeTag =
        hub.hub_type === "personal"
          ? chalk.dim("(personal)")
          : chalk.cyan("(team)");
      const roleTag = chalk.dim(`[${role}]`);
      console.log(
        `${marker}${chalk.bold(hub.name)} ${typeTag} ${roleTag}  ${chalk.dim(getHubReference(hub))}  ${chalk.dim(hub.id)}`,
      );
    }
    console.log();
  } catch (err) {
    console.error(chalk.red((err as Error).message || "Failed to list hubs"));
    process.exit(1);
  }
}

export async function hubCreateCommand(name: string): Promise<void> {
  try {
    const hub = await getClient().hubs.create(name);
    console.log(chalk.green(`\n  Created hub: ${hub.name} (${hub.slug})\n`));
    console.log(chalk.dim(`  ID: ${hub.id}`));
    console.log(
      chalk.dim(
        `  Set read context: memax hub switch ${getHubReference(hub)}\n`,
      ),
    );
  } catch (err) {
    console.error(chalk.red((err as Error).message || "Failed to create hub"));
    process.exit(1);
  }
}

export async function hubSwitchCommand(idOrSlug: string): Promise<void> {
  try {
    const match = await requireHub(idOrSlug);
    setActiveHubID(match.hub.id);
    const switchedRef =
      match.hub.hub_type === "personal" ? PERSONAL_HUB_ALIAS : match.hub.slug;
    console.log(
      chalk.green(
        `\n  Switched read context to: ${match.hub.name} (${switchedRef})\n`,
      ),
    );
  } catch (err) {
    console.error(chalk.red((err as Error).message || "Failed to switch hub"));
    process.exit(1);
  }
}

export async function hubMembersCommand(
  idOrSlug: string | undefined,
  options: HubMembersOptions,
): Promise<void> {
  try {
    const format = validateFormat(options.format);
    const hubRef = idOrSlug ?? getActiveHubID() ?? PERSONAL_HUB_ALIAS;
    const match = await requireHub(hubRef);
    const result = await getClient().hubs.get(match.hub.id);
    const members = result.members ?? [];

    if (format === "json") {
      process.stdout.write(
        `${JSON.stringify(
          { hub: result.hub, role: result.role, members },
          null,
          2,
        )}\n`,
      );
      return;
    }

    console.log(chalk.blue(`\n  Members for ${result.hub.name}\n`));
    if (members.length === 0) {
      console.log(chalk.dim("  No members found."));
      return;
    }
    for (const member of members) {
      printMember(member);
      console.log();
    }
  } catch (err) {
    console.error(
      chalk.red((err as Error).message || "Failed to list hub members"),
    );
    process.exit(1);
  }
}

export async function hubInviteCreateCommand(
  options: HubInviteCreateOptions,
): Promise<void> {
  try {
    const format = validateFormat(options.format);
    const hub = await requireInviteHub(options.hub);
    const role = normalizeInviteRole(options.role);
    const invite = await getClient().hubs.createInvite(hub.id, { role });
    const inviteURL =
      invite.invite_url ?? deriveInviteURL(invite.token, loadConfig().api_url);

    if (format === "json") {
      process.stdout.write(
        `${JSON.stringify({ invite, invite_url: inviteURL }, null, 2)}\n`,
      );
      return;
    }

    console.log(
      chalk.green(`\n  Invite created for ${chalk.bold(hub.name)}\n`),
    );
    console.log(chalk.gray(`  id: ${invite.id}`));
    console.log(chalk.gray(`  role: ${invite.role}`));
    console.log(
      chalk.gray(`  expires: ${new Date(invite.expires_at).toISOString()}`),
    );
    console.log(chalk.gray(`  link: ${inviteURL}\n`));
  } catch (err) {
    console.error(
      chalk.red((err as Error).message || "Failed to create hub invite"),
    );
    process.exit(1);
  }
}

export async function hubInviteListCommand(
  options: HubInviteListOptions,
): Promise<void> {
  try {
    const format = validateFormat(options.format);
    const hub = await requireInviteHub(options.hub);
    const invites = await getClient().hubs.listInvites(hub.id);

    if (format === "json") {
      process.stdout.write(`${JSON.stringify(invites, null, 2)}\n`);
      return;
    }

    if (invites.length === 0) {
      console.log(chalk.dim(`  No outstanding invites for ${hub.name}.`));
      return;
    }

    console.log(chalk.blue(`\n  Outstanding invites for ${hub.name}\n`));
    for (const invite of invites) {
      printInvite(invite, options.verbose);
      console.log();
    }
  } catch (err) {
    console.error(
      chalk.red((err as Error).message || "Failed to list hub invites"),
    );
    process.exit(1);
  }
}

export async function hubInviteRevokeCommand(
  inviteRef: string,
  options: HubInviteMutationOptions,
): Promise<void> {
  try {
    const format = validateFormat(options.format);
    const hub = await requireInviteHub(options.hub);
    const invites = await getClient().hubs.listInvites(hub.id);
    const invite = resolveInviteReference(invites, inviteRef);
    const result = await getClient().hubs.revokeInvite(hub.id, invite.id);

    if (format === "json") {
      process.stdout.write(`${JSON.stringify(result, null, 2)}\n`);
      return;
    }

    console.log(
      chalk.green(
        `\n  Revoked invite ${inviteDisplayID(invite.id)} for ${hub.name}\n`,
      ),
    );
  } catch (err) {
    console.error(
      chalk.red((err as Error).message || "Failed to revoke hub invite"),
    );
    process.exit(1);
  }
}

export async function hubInviteRegenerateCommand(
  inviteRef: string,
  options: HubInviteMutationOptions,
): Promise<void> {
  try {
    const format = validateFormat(options.format);
    const hub = await requireInviteHub(options.hub);
    const invites = await getClient().hubs.listInvites(hub.id);
    const invite = resolveInviteReference(invites, inviteRef);
    const replacement = await getClient().hubs.regenerateInvite(
      hub.id,
      invite.id,
    );
    const inviteURL =
      replacement.invite_url ??
      deriveInviteURL(replacement.token, loadConfig().api_url);

    if (format === "json") {
      process.stdout.write(
        `${JSON.stringify({ invite: replacement, invite_url: inviteURL }, null, 2)}\n`,
      );
      return;
    }

    console.log(
      chalk.green(
        `\n  Regenerated invite ${inviteDisplayID(invite.id)} for ${hub.name}\n`,
      ),
    );
    console.log(chalk.gray(`  new id: ${replacement.id}`));
    console.log(chalk.gray(`  role: ${replacement.role}`));
    console.log(
      chalk.gray(
        `  expires: ${new Date(replacement.expires_at).toISOString()}`,
      ),
    );
    console.log(chalk.gray(`  link: ${inviteURL}\n`));
  } catch (err) {
    console.error(
      chalk.red((err as Error).message || "Failed to regenerate hub invite"),
    );
    process.exit(1);
  }
}

export async function hubInviteAcceptCommand(
  tokenOrURL: string,
): Promise<void> {
  try {
    const token = extractInviteToken(tokenOrURL);
    const result = await getClient().invites.accept(token);
    console.log(
      chalk.green(`\n  Joined hub: ${result.hub.name} as ${result.role}\n`),
    );
  } catch (err) {
    console.error(
      chalk.red((err as Error).message || "Failed to accept invite"),
    );
    process.exit(1);
  }
}

export function registerHubCommands(program: Command): void {
  const hubCmd = program.command("hub").description("Manage hubs (workspaces)");

  hubCmd
    .command("list")
    .description("List your hubs")
    .option("--format <format>", "Output format: text, json", "text")
    .action(hubListCommand);

  hubCmd
    .command("create <name>")
    .description("Create a new team hub")
    .action(hubCreateCommand);

  hubCmd
    .command("switch <id-or-slug>")
    .description(
      'Switch your active read hub (use "personal" for your personal hub)',
    )
    .action(hubSwitchCommand);

  hubCmd
    .command("members [id-or-slug]")
    .description("List members for the current or selected hub")
    .option("--format <format>", "Output format: text, json", "text")
    .action(hubMembersCommand);

  const hubInviteCmd = hubCmd
    .command("invite")
    .description("Manage hub invites");

  hubInviteCmd
    .command("create")
    .description(
      "Create a shareable invite link for the current or selected hub",
    )
    .option("--hub <slug>", "Scope to a hub")
    .option(
      "--role <role>",
      "Invite role: admin, contributor, viewer",
      "contributor",
    )
    .option("--format <format>", "Output format: text, json", "text")
    .action(hubInviteCreateCommand);

  hubInviteCmd
    .command("list")
    .description("List outstanding invites for the current or selected hub")
    .option("--hub <slug>", "Scope to a hub")
    .option("--verbose", "Show full invite IDs")
    .option("--format <format>", "Output format: text, json", "text")
    .action(hubInviteListCommand);

  hubInviteCmd
    .command("revoke <invite-id-or-prefix>")
    .description("Revoke an outstanding invite")
    .option("--hub <slug>", "Scope to a hub")
    .option("--format <format>", "Output format: text, json", "text")
    .action(hubInviteRevokeCommand);

  hubInviteCmd
    .command("regenerate <invite-id-or-prefix>")
    .description("Revoke and replace an outstanding invite")
    .option("--hub <slug>", "Scope to a hub")
    .option("--format <format>", "Output format: text, json", "text")
    .action(hubInviteRegenerateCommand);

  hubInviteCmd
    .command("accept <token-or-url>")
    .description("Accept a hub invite from a token or full invite URL")
    .action(hubInviteAcceptCommand);
}
