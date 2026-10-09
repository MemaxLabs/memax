import { redirect } from "next/navigation";

// V1's consent page is retired: every MCP authorization, V1 people's
// included, is answered on the Ledger page (OAuthConsent, /oauth/authorize),
// where the web session says who the person is and one space is chosen per
// connection. A deliberate change for V1 people too, made when sign-in for
// authorize moved to the web (plan 25 §5.15). The proxy sends this path on
// first (lib/ui-gate.ts); this page does the same if it is ever reached,
// with its query, so a link from before the move still opens the request.
export default async function V1OAuthConsentPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(await searchParams)) {
    for (const v of Array.isArray(value) ? value : value ? [value] : []) {
      query.append(key, v);
    }
  }
  const qs = query.toString();
  redirect(`/oauth/authorize${qs ? `?${qs}` : ""}`);
}
