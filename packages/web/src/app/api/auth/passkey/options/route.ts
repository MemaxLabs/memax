import { csrfRefusal } from "@/lib/bff/csrf";
import { apiClient } from "@/lib/bff/session";
import { apiError } from "@/lib/bff/sign-in";

/**
 * Starting a passkey sign-in: the web app's server asks the API for a
 * challenge naming nobody (any passkey for this site), and the page hands
 * its options to navigator.credentials.get.
 */
export async function POST(req: Request) {
  const refused = csrfRefusal(req);
  if (refused) return refused;
  try {
    const challenge = await apiClient(req).v2.account.passkeys.startSignIn();
    return Response.json(
      { data: challenge },
      { headers: { "cache-control": "no-store" } },
    );
  } catch (error) {
    return apiError(error);
  }
}
