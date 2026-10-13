import { LANDING_HREF } from "@/lib/ui-gate";
import { DOCS_URL } from "@/lib/urls";

/**
 * Where the public site links. In-app pages are paths (the Landing's
 * moves with LANDING_AT_ROOT); everything else is a full URL. V1's
 * Privacy page is in the other root layout, so it is a plain link.
 */
export const LINKS = {
  landing: LANDING_HREF,
  pricing: "/pricing",
  security: "/security",
  docs: "/docs",
  signIn: "/signin",
  privacy: "/privacy",
  github: "https://github.com/MemaxLabs/memax",
  changelog: "https://github.com/MemaxLabs/memax/releases",
  fullDocs: DOCS_URL,
} as const;

/** A page on the docs site (docs.memax.app), by its path there. */
export function docsPage(path: string): string {
  return `${DOCS_URL}${path}`;
}

/**
 * Where people write to us. Both addresses are the ones the published
 * Terms and Privacy policy already give (security@ "within two business
 * days"; team@ for everything else).
 */
export const CONTACT = {
  security: "security@memaxlabs.com",
  team: "team@memaxlabs.com",
} as const;

/**
 * The footer's notice: a legal mark and the company's name, the same in
 * every language (like the names in settings-copy.ts NAMES).
 */
export const COPYRIGHT = "© 2026 MemaxLabs";

/** The section the Landing's "How it works" jumps to. */
export const HOW_IT_WORKS_ID = "how-it-works";
