import { Memax, MemaxError } from "memax-sdk";
import { NextResponse } from "next/server";
import { API_URL } from "@/lib/urls";

export async function GET(req: Request) {
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

  try {
    const client = new Memax({
      apiUrl: API_URL,
      auth: async () => ({
        Authorization: authorization,
      }),
    });
    const profile = await client.auth.me();
    return NextResponse.json({ data: profile });
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
