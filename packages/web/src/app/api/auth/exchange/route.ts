import { Memax, MemaxError } from "memax-sdk";
import { NextResponse } from "next/server";
import { API_URL } from "@/lib/urls";

const client = new Memax({ apiUrl: API_URL });

export async function POST(req: Request) {
  let body: { code?: string };
  try {
    body = (await req.json()) as { code?: string };
  } catch {
    return NextResponse.json(
      {
        error: {
          code: "invalid_request",
          message: "Missing code.",
        },
      },
      { status: 400 },
    );
  }

  if (!body.code) {
    return NextResponse.json(
      {
        error: {
          code: "invalid_request",
          message: "Missing code.",
        },
      },
      { status: 400 },
    );
  }

  try {
    const tokens = await client.auth.exchangeCode(body.code);
    return NextResponse.json({ data: tokens });
  } catch (error) {
    if (error instanceof MemaxError) {
      return NextResponse.json(
        {
          error: {
            code: error.code,
            message: error.message,
          },
        },
        { status: error.status },
      );
    }
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
