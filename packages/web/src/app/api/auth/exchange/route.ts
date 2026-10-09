import { csrfRefusal } from "@/lib/bff/csrf";
import { errorResponse } from "@/lib/bff/session";
import { sessionFromCode } from "@/lib/bff/sign-in";

/**
 * Signing in: the one-time code the API redirected to the web app (after
 * GitHub, Google or the email code) is traded here, by the web app's
 * server, for the session (lib/bff/sign-in.ts). Its tokens go into
 * HttpOnly cookies and never reach the page.
 */
export async function POST(req: Request) {
  const refused = csrfRefusal(req);
  if (refused) return refused;

  let code: string | undefined;
  try {
    code = ((await req.json()) as { code?: string }).code;
  } catch {
    // Answered below.
  }
  if (!code) return errorResponse(400, "invalid_request", "Missing code.");
  return sessionFromCode(req, code);
}
