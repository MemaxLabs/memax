import { Memax, MemaxError } from "memax-sdk";
import { NextResponse } from "next/server";
import { API_URL } from "@/lib/urls";

export async function POST(req: Request) {
  const authorization = req.headers.get("authorization");
  if (!authorization) {
    return NextResponse.json(
      { error: { code: "unauthorized", message: "Missing authorization." } },
      { status: 401 },
    );
  }

  let body: { user_id?: string; email?: string };
  try {
    body = (await req.json()) as { user_id?: string; email?: string };
  } catch {
    return NextResponse.json(
      { error: { code: "invalid_request", message: "Invalid JSON." } },
      { status: 400 },
    );
  }

  try {
    const client = new Memax({
      apiUrl: API_URL,
      auth: async () => ({ Authorization: authorization }),
    });
    const result = await client.auth.impersonate({
      userId: body.user_id,
      email: body.email,
    });
    return NextResponse.json({ data: result });
  } catch (error) {
    if (error instanceof MemaxError) {
      return NextResponse.json(
        { error: { code: error.code, message: error.message } },
        { status: error.status },
      );
    }
    return NextResponse.json(
      { error: { code: "network_error", message: "Could not reach API." } },
      { status: 502 },
    );
  }
}
