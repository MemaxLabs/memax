import { API_URL } from "@/lib/urls";

// The CLI command a person copies from the app, for the API this build
// talks to and the CLI that goes with it. Pasted as it was drawn
// (`npx memax-cli init`), a staging page sent people to the released CLI
// and production's API, and the page waited for an init that never came.

/** The API the CLI uses when MEMAX_API_URL isn't set (packages/cli/src/lib/config.ts). */
export const CLI_DEFAULT_API = "https://api.memax.app";

/**
 * The npm dist-tag of the CLI this build goes with (NEXT_PUBLIC_CLI_TAG):
 * `alpha` on staging, which is built from main as the alpha CLI is; unset
 * in production, whose CLI is `latest`.
 */
const CLI_TAG = tagOf(process.env.NEXT_PUBLIC_CLI_TAG);

function tagOf(value: string | undefined): string | null {
  const tag = value?.trim();
  return tag && /^[a-z0-9][a-z0-9.-]*$/i.test(tag) ? tag : null;
}

/** What comes before a subcommand: `npx memax-cli`, aimed at this app. */
export function cliPrefix(
  api: string = API_URL,
  tag: string | null = CLI_TAG,
): string {
  const env = api === CLI_DEFAULT_API ? "" : `MEMAX_API_URL=${api} `;
  return `${env}npx memax-cli${tag ? `@${tag}` : ""}`;
}

/** A command for this app's CLI: `cliCommand("init --space acme-web")`. */
export function cliCommand(args: string): string {
  return `${cliPrefix()} ${args}`;
}
