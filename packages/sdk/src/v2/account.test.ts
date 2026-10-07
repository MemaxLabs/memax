import { describe, expect, it, vi } from "vitest";
import { Memax, MemaxError, encodePasskeyAnswer } from "../index.js";
import type { PasskeyCheck, V2 } from "../index.js";

function jsonResponse(body: unknown, init?: ResponseInit): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
    ...init,
  });
}

type Call = {
  url: string;
  method?: string;
  headers: Record<string, string>;
  body?: string;
};

function client(
  responses: Response[],
  passkeyCheck?: (check: PasskeyCheck) => Promise<unknown | null>,
) {
  let i = 0;
  const fetchMock = vi.fn(async () =>
    responses[Math.min(i++, responses.length - 1)].clone(),
  );
  const memax = new Memax({
    apiUrl: "https://api.memax.app",
    fetch: fetchMock,
    maxRetries: 0,
    passkeyCheck,
  });
  const call = (n = 0): Call => {
    const [url, init] = fetchMock.mock.calls[n] as unknown as [
      string,
      RequestInit & { headers: Record<string, string> },
    ];
    return {
      url,
      method: init.method,
      headers: init.headers,
      body: init.body as string | undefined,
    };
  };
  return { memax, call, fetchMock };
}

const passkey: V2.Passkey = {
  id: "0199b0f3-2c4e-7000-8000-0000000000aa",
  name: "iCloud Keychain",
  provider: "iCloud Keychain",
  created_at: "2026-09-02T10:00:00Z",
  backup_eligible: true,
  synced: true,
  transports: ["internal", "hybrid"],
};

const account: V2.Account = {
  id: "0199b0f3-2c4e-7000-8000-000000000001",
  name: "Ziyang Zeng",
  email: "ziyang@example.com",
  initials: "ZZ",
  sign_in_methods: [
    { method: "github", connected: true, account: "ziyang@example.com" },
    { method: "google", connected: false },
    { method: "email", connected: true, account: "ziyang@example.com" },
  ],
  passkeys: [passkey],
  passkey_check: true,
  session: { surface: "web", signed_in_at: "2026-10-07T09:00:00Z" },
};

const challenge = {
  options: {
    challenge: "Y2hhbGxlbmdlLWZvci10aGUta2VlcA",
    timeout: 300000,
    rpId: "memax.app",
    allowCredentials: [{ type: "public-key", id: "Y3JlZA" }],
    userVerification: "required",
  },
  expires_at: "2026-10-07T10:05:00Z",
};

const needsPasskey = () =>
  jsonResponse(
    {
      error: {
        code: "needs_passkey",
        message:
          "You have a passkey, so keeping M-0219 asks for it. Confirm with your passkey on memax.app.",
        details: {
          policy: { effect: "refuse", code: "needs_passkey" },
          passkey: challenge,
        },
      },
    },
    { status: 403 },
  );

describe("memax.v2.account", () => {
  it("reads and renames your account", async () => {
    const { memax, call } = client([
      jsonResponse({ data: account }),
      jsonResponse({ data: { ...account, name: "Z. Zeng" } }),
    ]);
    const got = await memax.v2.account.get();
    expect(got.passkey_check).toBe(true);
    expect(call(0).url).toBe("https://api.memax.app/v2/me/account");
    const renamed = await memax.v2.account.update(
      { name: "Z. Zeng" },
      { idempotencyKey: "k-1" },
    );
    expect(renamed.name).toBe("Z. Zeng");
    expect(call(1).method).toBe("PATCH");
    expect(call(1).headers["Idempotency-Key"]).toBe("k-1");
    expect(JSON.parse(call(1).body!)).toEqual({ name: "Z. Zeng" });
  });

  it("adds, renames and removes passkeys", async () => {
    const { memax, call } = client([
      jsonResponse({
        data: {
          options: {
            rp: { id: "memax.app", name: "Memax" },
            user: {
              id: "dXNlcg",
              name: "ziyang@example.com",
              displayName: "Ziyang Zeng",
            },
            challenge: "Y2g",
            pubKeyCredParams: [{ type: "public-key", alg: -7 }],
            timeout: 300000,
            excludeCredentials: [],
            authenticatorSelection: {
              residentKey: "required",
              requireResidentKey: true,
              userVerification: "required",
            },
            attestation: "none",
          },
          expires_at: "2026-10-07T10:05:00Z",
        },
      }),
      jsonResponse({ data: passkey }, { status: 201 }),
      jsonResponse({ data: { ...passkey, name: "Laptop" } }),
      jsonResponse({ data: passkey }),
    ]);
    const reg = await memax.v2.account.passkeys.startRegistration({
      idempotencyKey: "k-reg",
    });
    expect(reg.options.authenticatorSelection.userVerification).toBe(
      "required",
    );
    expect(call(0).url).toBe(
      "https://api.memax.app/v2/me/passkey-registrations",
    );
    const credential = { id: "abc", type: "public-key", response: {} };
    await memax.v2.account.passkeys.add(credential, "Laptop", {
      idempotencyKey: "k-add",
    });
    expect(call(1).url).toBe("https://api.memax.app/v2/me/passkeys");
    expect(JSON.parse(call(1).body!)).toEqual({ credential, name: "Laptop" });
    await memax.v2.account.passkeys.rename(passkey.id, "Laptop", {
      idempotencyKey: "k-ren",
    });
    expect(call(2).method).toBe("PATCH");
    expect(call(2).url).toBe(
      `https://api.memax.app/v2/me/passkeys/${passkey.id}`,
    );
    await memax.v2.account.passkeys.remove(passkey.id, {
      idempotencyKey: "k-rm",
    });
    expect(call(3).url).toBe(
      `https://api.memax.app/v2/me/passkeys/${passkey.id}:remove`,
    );
  });

  it("signs in with a passkey (the web app's server's calls)", async () => {
    const { memax, call } = client([
      jsonResponse({ data: challenge }),
      jsonResponse({ data: { code: "one-time", expires_in: 60 } }),
    ]);
    const begin = await memax.v2.account.passkeys.startSignIn();
    expect(begin.options.allowCredentials).toHaveLength(1);
    expect(call(0).url).toBe("https://api.memax.app/v2/passkey-sign-ins");
    const done = await memax.v2.account.passkeys.finishSignIn({ id: "x" });
    expect(done.code).toBe("one-time");
    expect(call(1).url).toBe(
      "https://api.memax.app/v2/passkey-sign-ins:finish",
    );
    expect(call(1).headers["Idempotency-Key"]).toBeUndefined();
  });

  it("connects and disconnects sign-in methods, and forgets the account", async () => {
    const { memax, call } = client([
      jsonResponse({ data: { url: "https://accounts.google.com/o/oauth2" } }),
      jsonResponse({ data: account }),
      jsonResponse({
        data: {
          spaces: 2,
          agents: 1,
          team_spaces_kept: 1,
          sessions: 3,
          passkeys: 1,
        },
      }),
    ]);
    const { url } = await memax.v2.account.connectSignIn(
      "google",
      "https://memax.app/settings/account",
      { idempotencyKey: "k-c" },
    );
    expect(url).toContain("accounts.google.com");
    expect(call(0).url).toBe(
      "https://api.memax.app/v2/me/sign-in-methods/google:connect",
    );
    expect(JSON.parse(call(0).body!)).toEqual({
      redirect_uri: "https://memax.app/settings/account",
    });
    await memax.v2.account.disconnectSignIn("github", {
      idempotencyKey: "k-d",
    });
    expect(call(1).url).toBe(
      "https://api.memax.app/v2/me/sign-in-methods/github:disconnect",
    );
    const forgot = await memax.v2.account.forget("ziyang@example.com", {
      idempotencyKey: "k-f",
    });
    expect(forgot.spaces).toBe(2);
    expect(JSON.parse(call(2).body!)).toEqual({
      confirm: "ziyang@example.com",
    });
  });
});

describe("the passkey re-check", () => {
  const assertion = {
    id: "Y3JlZA",
    rawId: "Y3JlZA",
    type: "public-key",
    response: {
      clientDataJSON: "e30",
      authenticatorData: "AA",
      signature: "AA",
    },
    clientExtensionResults: {},
  };

  it("answers needs_passkey and sends the very same request again", async () => {
    const kept = jsonResponse({
      data: {
        outcome: "applied",
        policy: { effect: "apply" },
        memory: null,
        receipts: [],
      },
    });
    const seen: PasskeyCheck[] = [];
    const { memax, call, fetchMock } = client(
      [needsPasskey(), kept],
      async (check) => {
        seen.push(check);
        return assertion;
      },
    );
    await memax.v2.memories.keep(
      "M-0219",
      {},
      {
        space: "memax-v2",
        idempotencyKey: "k-keep",
        ifMatch: 2,
      },
    );
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(seen).toHaveLength(1);
    expect(seen[0].options.challenge).toBe(challenge.options.challenge);
    expect(seen[0].expiresAt).toBe(challenge.expires_at);
    expect(seen[0].message).toContain("M-0219");
    const [first, second] = [call(0), call(1)];
    expect(second.url).toBe(first.url);
    expect(second.method).toBe(first.method);
    expect(second.body).toBe(first.body);
    expect(second.headers["Idempotency-Key"]).toBe("k-keep");
    expect(second.headers["If-Match"]).toBe('"2"');
    expect(first.headers["X-Memax-Passkey"]).toBeUndefined();
    expect(second.headers["X-Memax-Passkey"]).toBe(
      encodePasskeyAnswer(assertion),
    );
    expect(
      JSON.parse(
        atob(
          second.headers["X-Memax-Passkey"]
            .replace(/-/g, "+")
            .replace(/_/g, "/"),
        ),
      ),
    ).toEqual(assertion);
  });

  it("throws needs_passkey when the person declines, or no handler is set", async () => {
    const declined = client([needsPasskey()], async () => null);
    await expect(
      declined.memax.v2.memories.keep(
        "M-0219",
        {},
        {
          space: "memax-v2",
          idempotencyKey: "k",
        },
      ),
    ).rejects.toMatchObject({ code: "needs_passkey", status: 403 });
    expect(declined.fetchMock).toHaveBeenCalledTimes(1);

    const none = client([needsPasskey()]);
    const err = await none.memax.v2.memories
      .keep("M-0219", {}, { space: "memax-v2", idempotencyKey: "k" })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(MemaxError);
    expect((err as MemaxError).details?.passkey).toBeDefined();
  });

  it("asks once: a second needs_passkey is thrown", async () => {
    const handler = vi.fn(async () => assertion);
    const { memax, fetchMock } = client(
      [needsPasskey(), needsPasskey()],
      handler,
    );
    await expect(
      memax.v2.memories.keep(
        "M-0219",
        {},
        {
          space: "memax-v2",
          idempotencyKey: "k",
        },
      ),
    ).rejects.toMatchObject({ code: "needs_passkey" });
    expect(handler).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("passes passkey_invalid through", async () => {
    const { memax } = client(
      [
        needsPasskey(),
        jsonResponse(
          {
            error: {
              code: "passkey_invalid",
              message: "That passkey check took too long. Try again.",
              details: { passkey_failure: "expired" },
            },
          },
          { status: 403 },
        ),
      ],
      async () => assertion,
    );
    await expect(
      memax.v2.memories.keep(
        "M-0219",
        {},
        {
          space: "memax-v2",
          idempotencyKey: "k",
        },
      ),
    ).rejects.toMatchObject({
      code: "passkey_invalid",
      details: { passkey_failure: "expired" },
    });
  });
});
