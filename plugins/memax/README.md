# Memax for Claude Code

Memax keeps what you and your team have decided, and gives it to every agent you use. People keep memories; agents read them and propose new ones, and a person reviews each proposal before it counts. Memax compiles what's kept into the files your agents already read (`AGENTS.md`, and a `CLAUDE.md` that imports it).

This plugin brings Memax into Claude Code, Claude and Cowork in one install.

| Part                   | What it does                                                                                                                                                                                                                                        |
| ---------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **MCP server**         | The remote Memax server, `https://api.memax.app/mcp`: recall, search and propose. Claude Code signs you in with OAuth on memax.app the first time (`/mcp` shows it), whichever way you sign in to Memax.                                            |
| **Session-start hook** | When the [Memax CLI](https://www.npmjs.com/package/memax-cli) is installed, each session starts with what changed since your last one in that repository. It reads local files only and never touches the network. Without the CLI it does nothing. |
| **`memax` skill**      | How to work with Memax: recall before a task, propose what you learn instead of keeping it, leave decisions waiting for a person to the person, and respect Review.                                                                                 |

## Install

In a Claude Code session:

```text
/plugin marketplace add MemaxLabs/memax
/plugin install memax@memax
```

Or from your shell:

```bash
claude plugin marketplace add MemaxLabs/memax
claude plugin install memax@memax
```

Then run `/mcp` and sign in to Memax. You need a Memax account, and during the alpha accounts are by invitation: ask for one at [memax.app](https://memax.app).

For the session-start hook, install the CLI and set your repository up once:

```bash
npx memax-cli init
```

`memax init` finds your agents' files, imports what they say for review, connects your agents and compiles your first `AGENTS.md`.

## What an agent can do

An agent connects at **Propose** by default: it reads your kept memories and proposes new ones, and nothing it proposes counts until a person keeps it. You can change what each agent may do in Memax's Agents page. Raising an agent needs you, signed in on the web.

## Privacy

- The hook reads only files on your machine: the Memax CLI's local cache and the compiled files in your repository. It opens no network connection.
- The MCP server receives what the agent sends it: your queries and its proposals. Memax's [Security page](https://docs.memax.app/concepts/security) lists every service that sees your memories and what each keeps.
- Forgetting a memory in Memax removes it from every compiled file and every agent's next session, and tells you which copies Memax can't reach.

## Uninstall

```text
/plugin uninstall memax@memax
```

Your memories stay in Memax. To take Memax out of a repository too, run `memax unlink` there.

## Links

- Documentation: [docs.memax.app/integrations/claude-code-plugin](https://docs.memax.app/integrations/claude-code-plugin)
- Issues: [github.com/MemaxLabs/memax/issues](https://github.com/MemaxLabs/memax/issues)
- Support: support@memax.app

## License

Apache-2.0. See [LICENSE](LICENSE).
