/**
 * A new space's name and repository (NewSpace): what the API takes, and
 * the slug it will pick, mirrored from internal/ledger/spaces.go so the
 * demo makes the same URLs and the form can check a repository before it
 * asks.
 */
import { isSpaceSlug } from "@/lib/ui-gate";

/** The API's longest space name. */
export const MAX_SPACE_NAME = 80;
/** The API's longest repository. */
export const MAX_SPACE_REPOSITORY = 200;

/** "owner/name" (a GitLab group's "group/sub/name" too), nothing else. */
const REPOSITORY = /^[\w.-]+(?:\/[\w.-]+)+$/;

/** Whether `repo` reads as owner/name (empty is fine: the repository is optional). */
export function isRepository(repo: string): boolean {
  const r = repo.trim();
  return r === "" || REPOSITORY.test(r);
}

/** A repository's name, which a space for it is usually called ("memax-web"). */
export function nameOfRepository(repo: string): string {
  const r = repo.trim();
  return REPOSITORY.test(r) ? r.slice(r.lastIndexOf("/") + 1) : "";
}

/**
 * The slug the API makes of a name: lower case, letters and digits,
 * hyphens between words, at most 44 characters; a short one gets
 * "-space" (ledger.slugify).
 */
export function slugOfName(name: string): string {
  let out = "";
  let dash = false;
  for (const ch of name.toLowerCase()) {
    if (/[a-z0-9]/.test(ch)) {
      if (dash && out) out += "-";
      dash = false;
      out += ch;
    } else {
      dash = true;
    }
  }
  if (out.length > 44) out = out.slice(0, 44).replace(/-+$/, "");
  if (out.length < 4) out = `${out}-space`.replace(/^-/, "");
  return out;
}

/** The first free slug for a name, as the API picks it: the name's, else with -2 to -9. */
export function freeSlug(name: string, taken: (slug: string) => boolean) {
  const base = slugOfName(name);
  const candidates = [
    base,
    ...Array.from({ length: 8 }, (_, i) => `${base}-${i + 2}`),
  ];
  return (
    candidates.find((slug) => isSpaceSlug(slug) && !taken(slug)) ??
    `${base}-${Date.now().toString(36).slice(-6)}`
  );
}
