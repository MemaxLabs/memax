import { Command } from "commander";
import { createServer } from "node:http";
import { getActiveHubID, loadConfig, setActiveHubID } from "../lib/config.js";
import { getClient, getPublicClient, resetClient } from "../lib/client.js";
import { saveCredentials } from "../lib/credentials.js";
import type { AuthProviderName } from "memax-sdk";

interface TokenPair {
  access_token: string;
  refresh_token: string;
  expires_in: number;
}

interface LoginOptions {
  provider?: string;
}

export async function loginCommand(options: LoginOptions = {}): Promise<void> {
  let provider: AuthProviderName;
  try {
    provider = normalizeProvider(options.provider);
  } catch (err) {
    console.error(`  Login failed: ${(err as Error).message}\n`);
    process.exit(1);
  }

  // Start a temporary local server to receive the OAuth callback
  const port = await findFreePort();
  const callbackUrl = `http://localhost:${port}/callback`;

  const tokenPromise = new Promise<TokenPair>((resolve, reject) => {
    const server = createServer(async (req, res) => {
      const url = new URL(req.url!, `http://localhost:${port}`);

      if (url.pathname === "/callback") {
        const code = url.searchParams.get("code");

        if (code) {
          // Exchange the one-time code for tokens via POST
          try {
            const tokens = await getPublicClient().auth.exchangeCode(code);

            if (tokens.access_token) {
              res.writeHead(200, { "Content-Type": "text/html" });
              res.end(`
                  <html><body style="font-family: system-ui; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0;">
                    <div style="text-align: center;">
                      <h2 style="font-weight: 600;">Logged in to Memax</h2>
                      <p style="color: #64748b;">You can close this tab and return to your terminal.</p>
                    </div>
                  </body></html>
                `);
              resolve(tokens);
            } else {
              res.writeHead(400, { "Content-Type": "text/html" });
              res.end(
                "<html><body><h2>Login failed</h2><p>Token exchange failed.</p></body></html>",
              );
              reject(new Error("Token exchange failed."));
            }
          } catch (err) {
            res.writeHead(500, { "Content-Type": "text/html" });
            res.end(
              "<html><body><h2>Login failed</h2><p>Could not reach Memax API.</p></body></html>",
            );
            reject(new Error("Could not reach Memax API for token exchange."));
          }
        } else {
          res.writeHead(400, { "Content-Type": "text/html" });
          res.end(
            "<html><body><h2>Login failed</h2><p>No authorization code received.</p></body></html>",
          );
          reject(new Error("No authorization code received from callback."));
        }

        // Close the server and force-kill connections so the process exits
        setTimeout(() => {
          server.close();
          server.closeAllConnections();
        }, 500);
      }
    });

    server.listen(port);

    // Timeout after 2 minutes — use unref() so it doesn't keep the process alive
    const timeout = setTimeout(() => {
      server.close();
      server.closeAllConnections();
      reject(
        new Error("Login timed out — no callback received within 2 minutes."),
      );
    }, 120_000);
    timeout.unref();
  });

  // Build the OAuth URL with our local callback as the redirect
  const authUrl = getPublicClient().auth.providerLoginURL(
    provider,
    callbackUrl,
  );
  const providerLabel = provider === "google" ? "Google" : "GitHub";

  console.log(`\n  Opening browser for ${providerLabel} login...\n`);
  console.log(`  If the browser doesn't open, visit:\n  ${authUrl}\n`);

  // Try to open the browser
  try {
    const { exec } = await import("node:child_process");
    const cmd =
      process.platform === "darwin"
        ? `open "${authUrl}"`
        : process.platform === "win32"
          ? `start "${authUrl}"`
          : `xdg-open "${authUrl}"`;
    exec(cmd);
  } catch {
    // Browser open failed — user can copy the URL
  }

  try {
    const tokens = await tokenPromise;
    saveCredentials({
      access_token: tokens.access_token,
      refresh_token: tokens.refresh_token,
      expires_at: Date.now() + tokens.expires_in * 1000,
    });
    resetClient();

    // Auto-set personal hub so commands work without `memax hub switch`
    try {
      const hubs = await getClient().hubs.list();
      const personal = hubs.find((h) => h.hub.hub_type === "personal");
      if (personal) {
        setActiveHubID(personal.hub.id);
      }
    } catch {
      // Non-fatal — user can manually run `memax hub switch personal`
    }

    console.log(
      "  Logged in successfully. Credentials saved to ~/.memax/credentials.json\n",
    );
  } catch (err) {
    console.error(`  Login failed: ${(err as Error).message}\n`);
    process.exit(1);
  }
}

export async function logoutCommand(): Promise<void> {
  const { clearCredentials } = await import("../lib/credentials.js");
  clearCredentials();
  console.log("  Logged out. Credentials cleared.\n");
}

export async function whoamiCommand(): Promise<void> {
  const { loadCredentials } = await import("../lib/credentials.js");
  const { getClient } = await import("../lib/client.js");
  const chalk = (await import("chalk")).default;

  const creds = loadCredentials();
  if (!creds?.access_token) {
    console.log("  Not logged in. Run: memax login\n");
    return;
  }

  try {
    const me = await getClient().auth.me();
    const u = me.user;

    console.log();
    console.log(
      `  ${chalk.bold(u.display_name || u.name)} ${chalk.dim(`(${u.email})`)}`,
    );
    console.log(`  Plan: ${chalk.cyan(u.personal_plan_id || u.plan)}`);

    // Active read hub (client-local)
    const activeHubID = getActiveHubID();
    if (me.hubs && me.hubs.length > 0 && activeHubID) {
      const active = me.hubs.find((h) => h.hub.id === activeHubID);
      if (active) {
        const typeTag =
          active.hub.hub_type === "personal" ? "" : chalk.dim(" (team)");
        console.log(`  Read hub:  ${active.hub.name}${typeTag}`);
      }
    }

    // Usage this period
    if (
      me.usage &&
      (me.usage.push_count > 0 ||
        me.usage.recall_count > 0 ||
        me.usage.ask_count > 0)
    ) {
      console.log(
        `  Usage: ${me.usage.push_count} pushes, ${me.usage.recall_count} recalls, ${me.usage.ask_count} asks`,
      );
    }

    console.log();
  } catch {
    console.log("  Session expired or invalid. Run: memax login\n");
  }
}

export function registerLoginCommands(program: Command): void {
  program
    .command("login")
    .description("Log in to Memax")
    .option(
      "--provider <name>",
      "OAuth provider to use: github or google (default: github)",
    )
    .action(loginCommand);
  program
    .command("logout")
    .description("Clear saved credentials")
    .action(logoutCommand);
  program
    .command("whoami")
    .description("Show current user")
    .action(whoamiCommand);
}

function normalizeProvider(value?: string): AuthProviderName {
  if (!value) {
    return "github";
  }
  const normalized = value.trim().toLowerCase();
  if (normalized === "github" || normalized === "google") {
    return normalized;
  }
  throw new Error("Unsupported login provider. Use: github or google.");
}

async function findFreePort(): Promise<number> {
  return new Promise((resolve) => {
    const server = createServer();
    server.listen(0, () => {
      const addr = server.address();
      const port = typeof addr === "object" && addr ? addr.port : 0;
      server.close(() => resolve(port));
    });
  });
}
