import { Memax } from "memax-sdk";
import { withCookies } from "@/lib/bff/cookies";
import { csrfRefusal } from "@/lib/bff/csrf";
import { ensureAccess, errorResponse, readSession } from "@/lib/bff/session";
import { API_URL } from "@/lib/urls";

interface RouteContext {
  params: Promise<{ provider: string }>;
}

/**
 * Starts linking GitHub or Google to the signed-in account: asks the API,
 * with the session's access token from its cookie, where to send the
 * browser, and answers that URL.
 */
export async function GET(req: Request, context: RouteContext) {
  const refused = csrfRefusal(req);
  if (refused) return refused;

  const { provider } = await context.params;
  if (provider !== "github" && provider !== "google") {
    return errorResponse(400, "invalid_provider", "Unsupported provider.");
  }

  const url = new URL(req.url);
  const redirectURI = url.searchParams.get("redirect_uri");
  if (!redirectURI) {
    return errorResponse(400, "invalid_request", "Missing redirect_uri.");
  }

  const session = readSession(req);
  if ((await ensureAccess(session, req)) !== "ok" || !session.access) {
    return withCookies(
      errorResponse(401, "unauthorized", "Sign in to link an account."),
      session.setCookies,
    );
  }

  const apiURL = new Memax({ apiUrl: API_URL }).auth.linkProviderURL(
    provider,
    redirectURI,
  );

  let response: Response;
  try {
    response = await fetch(apiURL, {
      headers: { Authorization: `Bearer ${session.access}` },
      redirect: "manual",
      cache: "no-store",
    });
  } catch {
    return withCookies(
      errorResponse(502, "network_error", "Could not reach memax API."),
      session.setCookies,
    );
  }

  const location = response.headers.get("location");
  if (
    response.status >= 300 &&
    response.status < 400 &&
    typeof location === "string" &&
    location.length > 0
  ) {
    return withCookies(
      Response.json({ data: { url: location } }),
      session.setCookies,
    );
  }

  let payload: unknown = null;
  try {
    payload = await response.json();
  } catch {
    // Fall through to a normalized error response below.
  }

  if (payload && typeof payload === "object") {
    return withCookies(
      Response.json(payload, { status: response.status }),
      session.setCookies,
    );
  }

  return withCookies(
    errorResponse(
      response.status || 502,
      "link_start_failed",
      "Could not start provider sign-in.",
    ),
    session.setCookies,
  );
}
