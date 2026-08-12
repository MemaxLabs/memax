/**
 * The canonical install + setup command shown wherever the web app tells
 * a user how to connect their agents.
 *
 * Installs globally instead of running through `npx`. `npx` looked
 * lighter — nothing left on the machine — but it breaks two things:
 *
 *   1. Hooks land on a slow path. `memax setup --hooks` bakes the
 *      resolved binary into each agent's hook config. Without a global
 *      install that resolution falls through to `npx -y memax-cli`, so
 *      every prompt re-resolves the package before the hook can answer —
 *      against a <500ms hook budget.
 *   2. `memax` never lands on PATH, so `memax agents sync`, `memax
 *      recall`, and the rest of the CLI are unreachable afterwards. The
 *      setup output says "Restart your agents" and the user is left with
 *      `command not found`.
 */
export const CLI_SETUP_CMD =
  "npm i -g memax-cli && memax login && memax setup --all";
