import { NextResponse } from "next/server";
import { API_URL } from "@/lib/urls";

function buildUpstreamURL(req: Request, path: string[]): string {
  const url = new URL(req.url);
  const upstream = new URL(`${API_URL}/${path.join("/")}`);
  upstream.search = url.search;
  return upstream.toString();
}

function buildUpstreamHeaders(req: Request): Headers {
  const headers = new Headers();
  const authorization = req.headers.get("authorization");
  const contentType = req.headers.get("content-type");
  const accept = req.headers.get("accept");
  const hubID = req.headers.get("x-hub-id");
  const timezone = req.headers.get("x-timezone");

  if (authorization) headers.set("authorization", authorization);
  if (contentType) headers.set("content-type", contentType);
  if (accept) headers.set("accept", accept);
  if (hubID) headers.set("x-hub-id", hubID);
  if (timezone) headers.set("x-timezone", timezone);

  return headers;
}

async function proxy(req: Request, path: string[]): Promise<Response> {
  try {
    const upstream = await fetch(buildUpstreamURL(req, path), {
      method: req.method,
      headers: buildUpstreamHeaders(req),
      body:
        req.method === "GET" || req.method === "HEAD"
          ? undefined
          : await req.text(),
      cache: "no-store",
    });

    const headers = new Headers();
    const contentType = upstream.headers.get("content-type");
    const cacheControl = upstream.headers.get("cache-control");
    const contentDisposition = upstream.headers.get("content-disposition");
    const memaxWarning = upstream.headers.get("x-memax-warning");

    if (contentType) headers.set("content-type", contentType);
    if (cacheControl) headers.set("cache-control", cacheControl);
    if (contentDisposition) {
      headers.set("content-disposition", contentDisposition);
    }
    if (memaxWarning) headers.set("x-memax-warning", memaxWarning);

    return new Response(upstream.body, {
      status: upstream.status,
      headers,
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
}

export async function GET(
  req: Request,
  context: { params: Promise<{ path: string[] }> },
) {
  const { path } = await context.params;
  return proxy(req, path);
}

export async function POST(
  req: Request,
  context: { params: Promise<{ path: string[] }> },
) {
  const { path } = await context.params;
  return proxy(req, path);
}

export async function PATCH(
  req: Request,
  context: { params: Promise<{ path: string[] }> },
) {
  const { path } = await context.params;
  return proxy(req, path);
}

export async function DELETE(
  req: Request,
  context: { params: Promise<{ path: string[] }> },
) {
  const { path } = await context.params;
  return proxy(req, path);
}
