import { API_URL } from "@/lib/urls";
import { CLI_DEFAULT_API, cliCommand } from "@/lib/v2/cli";
import type { CloudAgent } from "@/lib/v2/data/agents";

// What a cloud agent's repository commits so its sandbox starts warm
// (cloud warm-start design §5), as `memax connect --cloud` writes it
// (packages/cli/src/lib/connect/cloud.ts): the hook file names the
// variable, never the key. Data, not copy.

/** The cloud start's package, pinned exactly (npm versions are immutable). */
export const START_PACKAGE = "@memaxlabs/start@1.0.0";

/** The committed hook file an agent's cloud sandbox runs. */
export interface CloudHookFile {
  /** Repository-relative. */
  path: string;
  /** The whole file, as Memax writes it when there is none. */
  json: string;
}

export function cloudHookFile(agent: CloudAgent): CloudHookFile | null {
  switch (agent) {
    case "claude-code":
      return {
        path: ".claude/settings.json",
        json: pretty({
          hooks: {
            SessionStart: [
              {
                matcher: "startup|resume|compact",
                hooks: [
                  {
                    type: "command",
                    command: `[ "$CLAUDE_CODE_REMOTE" = true ] && npx -y ${START_PACKAGE} --agent claude-code || true`,
                    timeout: 20,
                  },
                ],
              },
            ],
          },
        }),
      };
    case "copilot":
      return {
        path: ".github/hooks/memax.json",
        json: pretty({
          version: 1,
          hooks: {
            sessionStart: [
              {
                type: "command",
                bash: `[ -n "$MEMAX_START_KEY" ] && npx -y ${START_PACKAGE} --agent copilot || true`,
                timeoutSec: 20,
              },
            ],
          },
        }),
      };
    default:
      return null;
  }
}

function pretty(value: unknown): string {
  return `${JSON.stringify(value, null, 2)}\n`;
}

/** The CLI command that writes the hook file and prints a key, run in the repository. */
export function connectCloudCommand(agent: CloudAgent, space: string): string {
  return cliCommand(`connect --cloud ${agent} --space ${space}`);
}

/** The API host the sandbox must reach. */
export function apiHost(api: string = API_URL): string {
  try {
    return new URL(api).host;
  } catch {
    return new URL(CLI_DEFAULT_API).host;
  }
}

/** The API's address when it isn't production's: the sandbox needs MEMAX_API_URL then. */
export function nonDefaultApi(api: string = API_URL): string | null {
  return api === CLI_DEFAULT_API ? null : api;
}
