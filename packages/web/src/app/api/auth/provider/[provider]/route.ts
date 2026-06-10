import { NextResponse } from "next/server";
import { Memax } from "memax-sdk";
import { API_URL } from "@/lib/urls";

interface RouteContext {
  params: Promise<{ provider: string }>;
}

export async function GET(req: Request, context: RouteContext) {
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

  return NextResponse.redirect(
    new Memax({ apiUrl: API_URL }).auth.providerLoginURL(provider, redirectURI),
    { status: 307 },
  );
}
