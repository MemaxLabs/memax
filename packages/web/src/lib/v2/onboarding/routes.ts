/**
 * Where the first session goes (plan §6.3, §7.3): the setup screens under
 * /setup and ReviewImport inside the space's Review, and where a person
 * lands after signing in.
 *
 *   /signin                 SignIn; signed in already, it lands (below)
 *   /signin/callback        finishes a sign-in, then `next` or lands
 *   /device?code=           CliAuth: confirming the CLI's device code
 *   /setup                  → /setup/import
 *   /setup/agents?space=    Connect: the agents and what each may do
 *   /setup/import?space=    FirstRun: run init, then follow its import
 *   /setup/cleanup?space=&import=   Cleanup: settle the disagreements
 *   /setup/done?space=      CompileDone: the files written and what's next
 *   /[space]/review?filter=import&import=   ReviewImport
 *
 * Landing after sign-in (afterSignIn): only a person the API shows the V2
 * UI (the V2 UI flag, internal/v2ui) lands in V2. For them, `next` when
 * the sign-in came from a page. Else, with no space on the V2 record yet,
 * someone who owns V1 work starts at its switch (the Today of a space
 * still on V1, which says what moves and offers FirstRun too), and anyone
 * else is new and starts at FirstRun. With a space on V2, one with an
 * import still in progress (a disagreement open or proposals it brought
 * still waiting, within the last two weeks) goes back to ReviewImport,
 * and anyone else opens a space's Today, a project space before
 * Personal. A person without the flag goes on to `next` only when it
 * opens for them (the CLI's device code, OAuthConsent, a V1 page), and
 * otherwise to V1's home.
 *
 * Only a space's owner switches it (policy.DecideSwitchSpace), so V1
 * spaces someone is only a member or viewer of don't make them a V1
 * person here: what they can do in V2 is start a space of their own,
 * which FirstRun is for, and its "Skip for now" opens a space still on
 * V1 (skipSpace).
 */
import type { WebUi } from "memax-sdk";
import { needsV2Opt, v1HomePath } from "@/lib/ui-gate";
import { importInProgress, type ImportView } from "../data/imports";
import type { ImportsSource } from "../data/imports";
import {
  holdsV1Work,
  type SwitchPreviewView,
  type SwitchSource,
} from "../data/switch";
import type { SpaceSummary } from "../data/types";
import { placeHref } from "../places";

export type SetupStep = "agents" | "import" | "cleanup" | "done";

export function setupHref(
  step: SetupStep,
  query: { space?: string | null; import?: string | null } = {},
): string {
  const params = new URLSearchParams();
  if (query.space) params.set("space", query.space);
  if (query.import) params.set("import", query.import);
  const qs = params.toString();
  return `/setup/${step}${qs ? `?${qs}` : ""}`;
}

export function reviewImportHref(space: string, importId?: string): string {
  const params = new URLSearchParams({ filter: "import" });
  if (importId) params.set("import", importId);
  return `/${encodeURIComponent(space)}/review?${params.toString()}`;
}

/**
 * The sign-in page, coming back to `next` (a same-origin path) after.
 * `again` signs in even with a session already (a fresh one issued to the
 * web app, which a change needing a person on the web asks for); without
 * it, a signed-in person goes straight on.
 */
export function signInHref(
  next?: string | null,
  { again = false }: { again?: boolean } = {},
): string {
  const params = new URLSearchParams();
  const safe = safeNext(next);
  if (safe) params.set("next", safe);
  if (again) params.set("again", "1");
  const query = params.toString();
  return query ? `/signin?${query}` : "/signin";
}

/**
 * A `next` that stays on this site: a path, never another origin, a
 * protocol-relative URL or a backslash trick. Null otherwise.
 */
export function safeNext(next: string | null | undefined): string | null {
  if (!next) return null;
  if (!next.startsWith("/") || next.startsWith("//") || next.includes("\\")) {
    return null;
  }
  // Signing in again from the sign-in pages would loop.
  if (/^\/signin(?:[/?#]|$)/.test(next)) return null;
  return next;
}

/** Where a sign-in goes on, before any V2 data is read. */
export type AfterSignIn =
  /** A same-origin path the person may open. */
  | { kind: "next"; href: string }
  /** The person's V2 landing (resolveLanding). */
  | { kind: "landing" }
  /** V1's home: the person doesn't see V2. */
  | { kind: "v1"; href: string };

/**
 * Where a sign-in goes on, from `next` and the person's web UI (the API's
 * `ui`, as the sign-in or the profile answered it; unknown counts as V1,
 * so V2 stays hidden when in doubt).
 */
export function afterSignIn(
  next: string | null | undefined,
  ui: WebUi | null | undefined,
): AfterSignIn {
  const safe = safeNext(next);
  if (ui === "v2")
    return safe ? { kind: "next", href: safe } : { kind: "landing" };
  if (safe && !needsV2Opt(safe)) return { kind: "next", href: safe };
  return { kind: "v1", href: v1HomePath(true) };
}

export type Landing =
  | { kind: "first-run" }
  /** A space still on V1, whose Today is its switch to V2. */
  | { kind: "switch"; space: string }
  | { kind: "review-import"; space: string; importId: string }
  | { kind: "today"; space: string };

export function landingHref(landing: Landing): string {
  switch (landing.kind) {
    case "first-run":
      return setupHref("import");
    case "review-import":
      return reviewImportHref(landing.space, landing.importId);
    case "switch":
    case "today":
      return placeHref(landing.space, "today");
  }
}

/** How long an import still counts as the one a person comes back to. */
export const IMPORT_RETURN_WINDOW_MS = 14 * 24 * 60 * 60 * 1000;

/** The spaces on the V2 record (a source that doesn't say means all are). */
export function v2Spaces(spaces: readonly SpaceSummary[]): SpaceSummary[] {
  return spaces.filter((s) => s.onV2 !== false);
}

/** The space a returning person opens: a project space before Personal. */
export function homeSpace(spaces: readonly SpaceSummary[]): SpaceSummary {
  return (
    spaces.find((s) => s.kind === "project") ??
    spaces.find((s) => s.kind === "team") ??
    spaces[0]!
  );
}

/**
 * The spaces still on V1 that the person owns, so theirs to switch (only
 * an owner switches one): a project or team space before Personal, each
 * in switcher order.
 */
export function spacesToSwitch(
  spaces: readonly SpaceSummary[],
): SpaceSummary[] {
  const owned = spaces.filter((s) => s.onV2 === false && s.role === "owner");
  return [
    ...owned.filter((s) => s.kind !== "personal"),
    ...owned.filter((s) => s.kind === "personal"),
  ];
}

/**
 * Where FirstRun's "Skip for now" goes before init has made a space: one
 * still on V1 that the person owns, whose Today is the switch to V2, else
 * any of theirs; null when they have none.
 */
export function skipSpace(
  spaces: readonly SpaceSummary[],
): SpaceSummary | null {
  const onV1 = spaces.filter((s) => s.onV2 === false);
  return spacesToSwitch(spaces)[0] ?? onV1[0] ?? spaces[0] ?? null;
}

/**
 * The spaces whose switch preview decides the landing: with nothing on
 * V2 and no project or team space of theirs on V1 (V1 made those only
 * when someone asked), the personal space they own. Every account starts
 * with one on V1, empty, so only what it holds tells a V1 person from a
 * new one. Empty otherwise: nothing else needs reading.
 */
export function switchesToRead(
  spaces: readonly SpaceSummary[],
): SpaceSummary[] {
  if (v2Spaces(spaces).length > 0) return [];
  const own = spacesToSwitch(spaces);
  return own.some((s) => s.kind !== "personal") ? [] : own;
}

/** What the landing is decided from (resolveLanding reads it). */
export interface LandingRead {
  spaces: readonly SpaceSummary[];
  /** The newest import of each space on V2, as read: null when it has none. */
  imports: ReadonlyArray<{ space: SpaceSummary; view: ImportView | null }>;
  /**
   * What switching each space switchesToRead names would move, as read:
   * null when the preview didn't load.
   */
  switches?: ReadonlyArray<{
    space: SpaceSummary;
    preview: SwitchPreviewView | null;
  }>;
}

/**
 * The space still on V1 a person with nothing on V2 starts at, to switch
 * it: the first of theirs (spacesToSwitch) that is a project or team
 * space, or their personal space when it holds V1 work (holdsV1Work). A
 * preview that didn't load counts as holding none: FirstRun's "Skip for
 * now" still opens it.
 */
function spaceToSwitch(read: LandingRead): SpaceSummary | null {
  for (const space of spacesToSwitch(read.spaces)) {
    if (space.kind !== "personal") return space;
    const preview = read.switches?.find((x) => x.space.id === space.id);
    if (preview?.preview && holdsV1Work(preview.preview)) return space;
  }
  return null;
}

/**
 * Decides the landing from the spaces, the newest import of each space on
 * V2 and, with none on V2, what switching their personal space would
 * move. Pure, so the rules are tested.
 */
export function decideLanding(read: LandingRead, now: Date): Landing {
  const onV2 = v2Spaces(read.spaces);
  if (onV2.length === 0) {
    const v1 = spaceToSwitch(read);
    return v1 ? { kind: "switch", space: v1.slug } : { kind: "first-run" };
  }
  const recent = read.imports
    .filter(
      (x): x is { space: SpaceSummary; view: ImportView } =>
        x.view !== null &&
        now.getTime() - new Date(x.view.summary.createdAt).getTime() <=
          IMPORT_RETURN_WINDOW_MS,
    )
    .sort(
      (a, b) =>
        new Date(b.view.summary.createdAt).getTime() -
        new Date(a.view.summary.createdAt).getTime(),
    );
  const open = recent.find((x) => importInProgress(x.view));
  if (open) {
    return {
      kind: "review-import",
      space: open.space.slug,
      importId: open.view.summary.id,
    };
  }
  return { kind: "today", space: homeSpace(onV2).slug };
}

/** What `resolveLanding` reads: the source's spaces, imports and switches. */
export interface LandingSource {
  spaces(signal?: AbortSignal): Promise<SpaceSummary[]>;
  imports: Pick<ImportsSource, "list" | "get">;
  switch: Pick<SwitchSource, "status">;
  now(): Date;
}

/**
 * Reads what the landing needs (the spaces, each V2 space's newest import
 * in full, and with nothing on V2 the switch preview switchesToRead
 * names) and decides. A space whose imports don't load counts as having
 * none: landing on Today is never wrong.
 */
export async function resolveLanding(
  source: LandingSource,
  signal?: AbortSignal,
): Promise<Landing> {
  const spaces = await source.spaces(signal);
  const [imports, switches] = await Promise.all([
    Promise.all(
      v2Spaces(spaces).map((space) => newestImport(source, space, signal)),
    ),
    Promise.all(
      switchesToRead(spaces).map((space) =>
        switchPreview(source, space, signal),
      ),
    ),
  ]);
  return decideLanding({ spaces, imports, switches }, source.now());
}

/** A space's newest import in full; none when it has none or they don't load. */
async function newestImport(
  source: LandingSource,
  space: SpaceSummary,
  signal?: AbortSignal,
): Promise<{ space: SpaceSummary; view: ImportView | null }> {
  try {
    const [latest] = await source.imports.list({ space, limit: 1, signal });
    const view = latest
      ? await source.imports.get({ space, id: latest.id, signal })
      : null;
    return { space, view };
  } catch {
    return { space, view: null };
  }
}

/** What switching a space would move; null when the preview doesn't load. */
async function switchPreview(
  source: LandingSource,
  space: SpaceSummary,
  signal?: AbortSignal,
): Promise<{ space: SpaceSummary; preview: SwitchPreviewView | null }> {
  try {
    const { preview } = await source.switch.status({ space, signal });
    return { space, preview };
  } catch {
    return { space, preview: null };
  }
}
