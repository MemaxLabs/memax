import { Command } from "commander";
import { createServer } from "node:http";
import { hostname } from "node:os";
import { getActiveHubID, loadConfig, setActiveHubID } from "../lib/config.js";
import { getClient, getPublicClient, resetClient } from "../lib/client.js";
import { saveCredentials } from "../lib/credentials.js";
import {
  canOpenBrowser,
  deviceLoginFailure,
  noDeviceGrant,
  signInWithDeviceCode,
} from "../lib/device-login.js";
import { cliVersion } from "../lib/version.js";
import type { AuthProviderName } from "memax-sdk";

interface TokenPair {
  access_token: string;
  refresh_token: string;
  expires_in: number;
}

interface LoginOptions {
  provider?: string;
  /** Sign in with a code confirmed in any browser (RFC 8628). */
  device?: boolean;
  /** The space the CLI will use, shown on the confirmation page. */
  space?: string;
}

export async function loginCommand(options: LoginOptions = {}): Promise<void> {
  if (!(await signIn(options))) process.exit(1);
}

/**
 * Signs in with a code confirmed on the web app (memax.app/device, opened
 * here when a browser can), so the CLI is whoever the browser is signed
 * in as, however they sign in. `--provider` asks for that provider's page
 * instead, and a server with no device grant gets it too. `memax login`
 * and `memax init` both use it.
 */
export async function signIn(options: LoginOptions = {}): Promise<boolean> {
  if (options.provider && !options.device) return signInWithBrowser(options);
  return signInWithDevice(options);
}

/** Signs in with a device code and saves the credentials. */
export async function signInWithDevice(
  options: LoginOptions = {},
): Promise<boolean> {
  const browser = canOpenBrowser({
    platform: process.platform,
    env: process.env,
  });
  try {
    const result = await signInWithDeviceCode({
      auth: getPublicClient().auth,
      out: (l) => console.log(l),
      sleep: (ms) => new Promise((r) => setTimeout(r, ms)),
      now: () => performance.now(),
      open: browser ? openBrowser : undefined,
      device: {
        clientVersion: cliVersion(),
        deviceName: hostname(),
        deviceOs: process.platform,
        space: options.space,
      },
    });
    if (!result.ok) {
      console.error(`\n  ${deviceLoginFailure(result.reason)}\n`);
      return false;
    }
    await finishSignIn(result.tokens);
    return true;
  } catch (err) {
    if (noDeviceGrant(err)) {
      // An API from before the device grant: the provider's own page.
      if (browser) return signInWithBrowser(options);
      console.error(
        "  This Memax server can't sign in with a code. Run memax login on a machine with a browser, or set MEMAX_API_KEY.\n",
      );
      return false;
    }
    console.error(`  Login failed: ${(err as Error).message}\n`);
    return false;
  }
}

/** Saves a new session and picks the personal hub for V1 commands. */
async function finishSignIn(tokens: TokenPair): Promise<void> {
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
}

/** Opens a URL in the default browser; a failure leaves the printed link. */
function openBrowser(url: string): void {
  void import("node:child_process")
    .then(({ execFile }) => {
      const [cmd, args] =
        process.platform === "darwin"
          ? ["open", [url]]
          : process.platform === "win32"
            ? ["cmd", ["/c", "start", "", url]]
            : ["xdg-open", [url]];
      execFile(cmd, args, () => {});
    })
    .catch(() => {});
}

/**
 * Signs in on a provider's own page (OAuth with a local callback) and
 * saves the credentials; false, with the reason printed, when it didn't
 * work. Only for `--provider`, and for a server with no device grant.
 */
export async function signInWithBrowser(
  options: LoginOptions = {},
): Promise<boolean> {
  let provider: AuthProviderName;
  try {
    provider = normalizeProvider(options.provider);
  } catch (err) {
    console.error(`  Login failed: ${(err as Error).message}\n`);
    return false;
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
    await finishSignIn(tokens);
    return true;
  } catch (err) {
    console.error(`  Login failed: ${(err as Error).message}\n`);
    return false;
  }
}

/**
 * Signs this CLI's session out on the server (its refresh token stops
 * working at once) and clears the saved credentials. Offline, the
 * credentials are cleared anyway and the session ends when it expires, or
 * when you sign it out from Settings on memax.app.
 */
export async function logoutCommand(): Promise<void> {
  const { clearCredentials, loadCredentials } =
    await import("../lib/credentials.js");
  const creds = loadCredentials();
  let signedOut = false;
  const token = creds?.refresh_token || creds?.access_token;
  if (token) {
    try {
      await getPublicClient().auth.revoke(token);
      signedOut = true;
    } catch {
      // Unreachable or refused: clear locally all the same.
    }
  }
  clearCredentials();
  resetClient();
  if (token && !signedOut) {
    console.log(
      "  Logged out here. Memax couldn't be reached to end the session, so it lasts until it expires; sign it out in Settings on memax.app.\n",
    );
    return;
  }
  console.log(
    "  Logged out. The session is signed out and credentials cleared.\n",
  );
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
      "Sign in on GitHub's or Google's own page instead (github or google)",
    )
    .option(
      "--device",
      "Sign in with a code you confirm in a browser signed in to Memax (the default)",
    )
    .action(loginCommand);
  program
    .command("logout")
    .description("Sign this session out and clear saved credentials")
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
