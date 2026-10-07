import { csrfRefusal } from "@/lib/bff/csrf";
import { apiClient, errorResponse } from "@/lib/bff/session";
import { apiError, sessionFromCode } from "@/lib/bff/sign-in";

/**
 * Signing in with a passkey: the browser's answer goes to the API, which
 * verifies it (user verified, on this site's origin, its challenge used
 * once) and answers a one-time code; the web app's server trades that
 * code for the web session, as every other sign-in does
 * (lib/bff/sign-in.ts). The page learns only that it worked.
 */
export async function POST(req: Request) {
  const refused = csrfRefusal(req);
  if (refused) return refused;

  let credential: Record<string, unknown> | undefined;
  try {
    const body = (await req.json()) as { credential?: unknown };
    if (body.credential && typeof body.credential === "object") {
      credential = body.credential as Record<string, unknown>;
    }
  } catch {
    // Answered below.
  }
  if (!credential) {
    return errorResponse(400, "invalid_request", "Missing credential.");
  }
  let code: string;
  try {
    code = (await apiClient(req).v2.account.passkeys.finishSignIn(credential))
      .code;
  } catch (error) {
    return apiError(error);
  }
  return sessionFromCode(req, code);
}
