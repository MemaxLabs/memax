import { NextResponse } from "next/server";
import { Memax } from "memax-sdk";
import { API_URL } from "@/lib/urls";

interface RouteContext {
  params: Promise<{ provider: string }>;
}

export async function GET(req: Request, context: RouteContext) {
  const authorization = req.headers.get("authorization");
  if (!authorization) {
    return NextResponse.json(
      {
        error: {
          code: "unauthorized",
          message: "Missing authorization header.",
        },
      },
      { status: 401 },
    );
  }

  const { provider } = await context.params;
  if (provider !== "github" && provider !== "google") {
    return NextResponse.json(
      {
        error: {
          code: "invalid_provider",
          message: "Unsupported provider.",
        },
      },
      { status: 400 },
    );
  }

  const url = new URL(req.url);
  const redirectURI = url.searchParams.get("redirect_uri");
  if (!redirectURI) {
    return NextResponse.json(
      {
        error: {
          code: "invalid_request",
          message: "Missing redirect_uri.",
        },
      },
      { status: 400 },
    );
  }

  const apiURL = new Memax({ apiUrl: API_URL }).auth.linkProviderURL(
    provider,
    redirectURI,
  );

  let response: Response;
  try {
    response = await fetch(apiURL, {
      headers: { Authorization: authorization },
      redirect: "manual",
      cache: "no-store",
    });
  } catch {
    return NextResponse.json(
      {
        error: {
          code: "network_error",
          message: "Could not reach memax API.",
        },
      },
      { status: 502 },
    );
  }

  const location = response.headers.get("location");
  if (
    response.status >= 300 &&
    response.status < 400 &&
    typeof location === "string" &&
    location.length > 0
  ) {
    return NextResponse.json({ data: { url: location } });
  }

  let payload: unknown = null;
  try {
    payload = await response.json();
  } catch {
    // Fall through to a normalized error response below.
  }

  if (payload && typeof payload === "object") {
    return NextResponse.json(payload, { status: response.status });
  }

  return NextResponse.json(
    {
      error: {
        code: "link_start_failed",
        message: "Could not start provider sign-in.",
      },
    },
    { status: response.status || 502 },
  );
}
