import { NextResponse, type NextRequest } from "next/server";
import { devRoutesEnabled } from "@/lib/dev-routes";
import { parseUiToggle, UI_COOKIE, UI_COOKIE_V2 } from "@/lib/ui-gate";

// Dev-only UI toggle. /dev/ui?v=2 sets memax_ui=v2 and opens the Ledger
// specimen; /dev/ui?v=1 clears it and goes back to V1. Either takes
// &next=/path. 404 in production builds unless NEXT_PUBLIC_DEV_FIXTURES=1,
// like the /dev pages.
//
// It lives outside both route groups: it belongs to neither UI and
// renders no layout.

export const dynamic = "force-dynamic";

const THIRTY_DAYS_SECONDS = 60 * 60 * 24 * 30;

export function GET(request: NextRequest) {
  if (!devRoutesEnabled()) {
    return new NextResponse(null, { status: 404 });
  }

  const toggle = parseUiToggle(request.nextUrl.searchParams);
  if (!toggle) {
    return new NextResponse(
      "Use /dev/ui?v=2 to open the Ledger UI, or /dev/ui?v=1 to go back to V1. Add &next=/path to choose where to land.\n",
      { status: 400, headers: { "content-type": "text/plain; charset=utf-8" } },
    );
  }

  const response = NextResponse.redirect(
    new URL(toggle.next, request.nextUrl.origin),
  );
  response.headers.set("cache-control", "no-store");
  if (toggle.ui === "v2") {
    response.cookies.set(UI_COOKIE, UI_COOKIE_V2, {
      path: "/",
      sameSite: "lax",
      httpOnly: true,
      secure: request.nextUrl.protocol === "https:",
      maxAge: THIRTY_DAYS_SECONDS,
    });
  } else {
    response.cookies.set(UI_COOKIE, "", { path: "/", maxAge: 0 });
  }
  return response;
}
