// Which spaces memax init imports into: the repository's project space
// (found, or created on the V2 record) and the person's Personal space for
// machine-local memory (plan 25 §7.3 step 5).
import { basename } from "node:path";
import { randomUUID } from "node:crypto";
import { MemaxError, type V2 } from "memax-sdk";
import { findLinkedRepo } from "../daemon/registry.js";
import { normalizeRepoUrl, readMemaxYmlConfig } from "../project-context.js";
import type { InitDeps, InitOptions } from "./types.js";

/** "acme/web" for any spelling of a repository's URL or "owner/name". */
export function repoKey(url: string): string {
  return normalizeRepoUrl(url).split("/").filter(Boolean).slice(-2).join("/");
}

export class InitError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "InitError";
  }
}

export interface ProjectSpace {
  space: V2.Space;
  created: boolean;
  /** How it was found: --space, .memax.yml, the link, the repository, a choice, or created. */
  source: "flag" | "memax_yml" | "linked" | "repository" | "chosen" | "created";
}

function find(spaces: V2.Space[], key: string): V2.Space | undefined {
  return spaces.find((s) => s.id === key || s.slug === key);
}

/** A space on V1 moves to V2 if it is empty; otherwise the person switches it in the app. */
async function onV2(d: InitDeps, sp: V2.Space): Promise<V2.Space> {
  if (sp.v2_enabled_at) return sp;
  try {
    return await d.memax.v2.spaces.switchToV2(sp.id, {
      idempotencyKey: randomUUID(),
      via: "cli",
    });
  } catch (err) {
    if (err instanceof MemaxError && err.code === "space_has_notes") {
      throw new InitError(
        `${sp.slug} is still on Memax V1, with ${(err.details?.notes as number | undefined) ?? "some"} memories. Switch it to V2 in the app first, or leave out --space to start a new project space.`,
      );
    }
    throw err;
  }
}

/** The repository's project space: named, linked, matched by its remote, chosen, or created. */
export async function projectSpace(
  d: InitDeps,
  o: InitOptions,
  root: string,
  remote: string | null,
  spaces: V2.Space[],
): Promise<ProjectSpace> {
  const named: Array<[string | undefined, ProjectSpace["source"]]> = [
    [o.space, "flag"],
    [readMemaxYmlConfig(root)?.space, "memax_yml"],
    [findLinkedRepo(d.paths, root)?.space_id, "linked"],
  ];
  for (const [key, source] of named) {
    if (!key) continue;
    const sp = find(spaces, key);
    if (!sp) {
      if (source === "flag")
        throw new InitError(
          `${key} isn't one of your spaces. Leave out --space to create one for this repository.`,
        );
      continue; // a stale .memax.yml or link: look further
    }
    if (sp.kind === "personal")
      throw new InitError(
        `${sp.slug} is your personal space. Choose a project space with --space.`,
      );
    return { space: await onV2(d, sp), created: false, source };
  }
  const repo = remote ? repoKey(remote) : null;
  if (repo) {
    const matches = spaces.filter(
      (s) => s.v2_enabled_at && s.repository && repoKey(s.repository) === repo,
    );
    if (matches.length === 1)
      return { space: matches[0], created: false, source: "repository" };
  }

  const name = repo ? repo.split("/")[1] : basename(root);
  const projects = spaces.filter(
    (s) =>
      s.kind === "project" &&
      s.v2_enabled_at &&
      (s.role === "owner" || s.role === "member"),
  );
  if (d.interactive && !o.yes && projects.length > 0) {
    d.out("");
    d.out(`  This repository has no space yet. Your project spaces:`);
    projects.forEach((s, i) => d.out(`    ${i + 1}. ${s.slug}`));
    const answer = await d.prompt.ask(
      `  Use one (its number), or press Enter to create ${name}: `,
    );
    const picked =
      projects[Number(answer) - 1] ??
      (answer ? find(projects, answer) : undefined);
    if (picked) return { space: picked, created: false, source: "chosen" };
    if (answer)
      throw new InitError(
        `No space ${answer}. Run memax init again and choose one, or press Enter for a new one.`,
      );
  } else if (!d.interactive && !o.yes) {
    throw new InitError(
      `This repository has no space yet. Run memax init --yes to create ${name}, or name one with --space.`,
    );
  }
  const space = await d.memax.v2.spaces.create(
    { name, ...(repo ? { repository: repo } : {}) },
    { idempotencyKey: `init-space:${repo ?? root}`, via: "cli" },
  );
  return { space, created: true, source: "created" };
}

export interface PersonalSpace {
  space: V2.Space | null;
  switched: boolean;
  /** Why machine-local memory stays where it is, when it does. */
  note?: string;
}

/** The person's Personal space, on V2 (an empty one is switched). */
export async function personalSpace(
  d: InitDeps,
  spaces: V2.Space[],
): Promise<PersonalSpace> {
  const sp = spaces.find((s) => s.kind === "personal" && s.role === "owner");
  if (!sp)
    return {
      space: null,
      switched: false,
      note: "You have no personal space yet, so machine-local memory stays on this machine.",
    };
  if (sp.v2_enabled_at) return { space: sp, switched: false };
  try {
    return { space: await onV2(d, sp), switched: true };
  } catch (err) {
    if (err instanceof InitError) {
      return {
        space: null,
        switched: false,
        note: `Your personal space is still on Memax V1, so machine-local memory stays on this machine. Switch it to V2 in the app, then run memax init again.`,
      };
    }
    throw err;
  }
}
