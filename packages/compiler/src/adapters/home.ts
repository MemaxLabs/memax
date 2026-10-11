/**
 * Home adapters: a personal space's record in the user-level file one agent
 * loads in every session of every project (personal-layer design §3.2, §4).
 *
 * A repository's files import AGENTS.md; these don't. Codex has no imports,
 * and a missing canonical would break every importer, so each file carries
 * its lines inline and stands on its own. Each holds:
 *
 * - shared facts (no `scope.agents`) and this agent's own facts;
 * - never a path-scoped fact: home files load in every project, so a fact
 *   tied to a repository stays live over MCP (the API leaves out facts with
 *   a repository scope before they reach the compiler);
 * - the Brief's prose only when every memory it cites is one of those.
 *
 * Owner `memax` writes the whole file (`~/.claude/rules/memax-personal.md`).
 * Owner `block` manages one marked block in the person's own file
 * (`~/.codex/AGENTS.md`), as a user-owned shim does. The API compiles that
 * block against `current: ""`, or the block alone, because the person's
 * other lines never leave their machine; the daemon there splices it in.
 *
 * The default budget is 6 KiB, about 1,500 tokens, since these files load
 * in every session of every agent on every machine the person links.
 */
import { compose, headerComment } from "../document.js";
import { extractManagedBlock, upsertManagedBlock } from "../managed-block.js";
import type { Fact, Model } from "../model.js";
import { parseFile } from "../parse.js";
import { select } from "../select.js";
import type { Adapter, Limits, Owner } from "./adapter.js";

/** The default budget of every file a personal space compiles to. */
export const HOME_BUDGET = 6 * 1024;

/**
 * Said once per file: tools disagree on whether user or project files win,
 * and projects are where the decisions in force live.
 */
export const PERSONAL_INTRO_LINE =
  "The person you work with keeps these in Memax. Where a project's own files disagree, follow the project.";

/** The one-line tail: what didn't fit, and everything else, is a call away. */
export const PERSONAL_LIVE_CONTEXT =
  "The rest of what they keep is live over the memax MCP server: memax_recall, memax_search, memax_get.";

/**
 * The facts a personal file carries: never path-scoped, and either shared
 * or for `agent`. The canonical (`agent` null) carries shared facts only.
 */
export function personalFacts(agent: string | null): (fact: Fact) => boolean {
  return (f) =>
    f.paths.length === 0 &&
    (f.agents.length === 0 || (agent !== null && f.agents.includes(agent)));
}

/** Brief prose that rests only on facts the file carries. */
export function proseOn(
  model: Model,
  carries: (fact: Fact) => boolean,
): (cites: string[]) => boolean {
  return (cites) =>
    cites.every((ref) => {
      const fact = model.facts.get(ref);
      return fact !== undefined && carries(fact);
    });
}

/** The head every personal file starts with: header, title, summary, intro. */
export function personalHead(model: Model): string[] {
  const { title, summary } = model.brief;
  return [
    headerComment(model),
    `# ${title}`,
    "",
    ...(summary ? [summary] : []),
    PERSONAL_INTRO_LINE,
  ];
}

export interface HomeSpec {
  kind: Adapter["kind"];
  tool: string;
  /** The agent ID whose agent-only memories land here. */
  agent: string;
  /** Relative to the home folder. */
  defaultPath: string;
  pathHint: string;
  acceptsPath(path: string): boolean;
  /** Who owns the file at an accepted path. */
  owner(path: string): Owner;
  limits: Limits;
}

export function home(spec: HomeSpec): Adapter {
  return {
    kind: spec.kind,
    version: 1,
    role: "home",
    tool: spec.tool,
    defaultPath: spec.defaultPath,
    pathHint: spec.pathHint,
    acceptsPath: spec.acceptsPath,
    roots: ["home"],
    owner: spec.owner,
    spaceKinds: ["personal"],
    defaultBudget: () => HOME_BUDGET,
    limits: spec.limits,
    reads: () => null,
    parse: parseFile,

    render(model, target, limit) {
      const carries = personalFacts(spec.agent);
      const selection = select(model, target, {
        accepts: carries,
        prose: proseOn(model, carries),
      });
      const doc = compose(
        selection,
        {
          style: "markdown",
          head: personalHead(model),
          tail: [PERSONAL_LIVE_CONTEXT],
        },
        limit,
      );
      if (!target.userOwned) {
        return [
          {
            path: target.path,
            content: doc.content,
            driftText: doc.content,
            userOwned: false,
            doc,
          },
        ];
      }
      const content = upsertManagedBlock(
        target.current,
        doc.content.slice(0, -1).split("\n"),
      );
      return [
        {
          path: target.path,
          content,
          driftText: extractManagedBlock(content) ?? content,
          userOwned: true,
          doc,
        },
      ];
    },
  };
}
