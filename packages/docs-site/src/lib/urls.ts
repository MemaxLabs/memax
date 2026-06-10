const PROD_API_URL = "https://api.memax.app";
const PROD_APP_URL = "https://memax.app";
const PROD_DOCS_URL = "https://docs.memax.app";

function resolvePublicUrl(
  envValue: string | undefined,
  prodDefault: string,
): string {
  const trimmed = envValue?.trim();
  if (!trimmed) return prodDefault;
  return trimmed.replace(/\/+$/, "");
}

/** API server origin — referenced in code examples that embed live URLs. */
export const API_URL = resolvePublicUrl(
  process.env.NEXT_PUBLIC_API_URL,
  PROD_API_URL,
);

/** Main web app origin — used for "Open memax" / "Sign in" links from docs. */
export const APP_URL = resolvePublicUrl(
  process.env.NEXT_PUBLIC_APP_URL,
  PROD_APP_URL,
);

/**
 * Docs site's own origin — used for `metadataBase` and OG tags. Even
 * though we're already running on this origin, making it env-driven
 * lets preview deployments render the right canonical + OG URLs.
 */
export const DOCS_URL = resolvePublicUrl(
  process.env.NEXT_PUBLIC_DOCS_URL,
  PROD_DOCS_URL,
);
