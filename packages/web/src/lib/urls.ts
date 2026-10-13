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

/** API server origin — e.g. `https://api.memax.app` or `http://localhost:8080`. */
export const API_URL = resolvePublicUrl(
  process.env.NEXT_PUBLIC_API_URL,
  PROD_API_URL,
);

/**
 * The remote MCP server agents add by hand: this build's API, so a staging
 * page names staging's server. mcp.memax.app is planned as its alias
 * (plan 25 E7) but isn't served yet, so nothing may name it until it is.
 */
export const MCP_URL = `${API_URL}/mcp`;

/** Web app origin — e.g. `https://memax.app`. Used for share links, OAuth redirects. */
export const APP_URL = resolvePublicUrl(
  process.env.NEXT_PUBLIC_APP_URL,
  PROD_APP_URL,
);

/** Developer docs origin — e.g. `https://docs.memax.app`. Used for footer links, changelog. */
export const DOCS_URL = resolvePublicUrl(
  process.env.NEXT_PUBLIC_DOCS_URL,
  PROD_DOCS_URL,
);
